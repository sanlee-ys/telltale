// telltale — an honest gauge for your coding agents.
//
// One binary, three modes (decisions/002, decisions/008):
//
//	telltale statusline   read a vendor statusline JSON payload on stdin, print one
//	                      line (Claude Code, or Antigravity CLI via its documented
//	                      product marker — ADR-004)
//	telltale hud          cross-vendor watch-mode TUI
//	telltale council      dispatch room: one brief to several vendor CLIs at once
//
// The two GAUGES — statusline and hud — share the normalized session model and
// internal/theme's numbers, and nothing else. Neither writes, calls the network
// or sends anything to a running agent. council is the deliberate exception
// (ADR-008): it spawns vendor CLIs, states each one's read-only posture on
// screen, and shares no keybinding with the HUD. It reuses internal/theme and
// nothing else from the gauges, so that seam is unchanged.
//
// The single binary links the TUI framework, but the statusline path never
// initializes it — only package init runs, and the statusline latency budget is
// re-benchmarked whenever deps change (the binary is spawned fresh on every
// prompt, so init cost is statusline cost).
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	agyadapter "github.com/sanlee-ys/telltale/internal/adapter/antigravity"
	"github.com/sanlee-ys/telltale/internal/adapter/claudecode"
	"github.com/sanlee-ys/telltale/internal/adapter/codex"
	"github.com/sanlee-ys/telltale/internal/adapter/cursor"
	"github.com/sanlee-ys/telltale/internal/adapter/gemini"
	"github.com/sanlee-ys/telltale/internal/antigravity"
	"github.com/sanlee-ys/telltale/internal/claude"
	"github.com/sanlee-ys/telltale/internal/council"
	"github.com/sanlee-ys/telltale/internal/hud"
	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/statusline"
)

var version = "dev" // set via -ldflags at release time

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "statusline":
		runStatusline()
	case "hud":
		if err := runHUD(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "telltale hud:", err)
			os.Exit(1)
		}
	case "council":
		if err := runCouncil(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "telltale council:", err)
			os.Exit(1)
		}
	case "version", "--version", "-v":
		fmt.Println("telltale", version)
	default:
		usage()
		os.Exit(2)
	}
}

func runStatusline() {
	// One statusline command serves two vendors. Routing is the documented
	// `product` field, an affirmative marker: agy stamps "antigravity" on
	// every payload and Claude Code's payload has no product field at all.
	// Stdin is read once; both parsers see the same bytes.
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "telltale: bad statusline input:", err)
		os.Exit(0)
	}
	// A probe failure is bad input, full stop — it must take the clean-exit
	// path. Falling through to the Claude parser would be worse than nothing:
	// claude.Parse uses a streaming decoder that reads only the FIRST JSON
	// value, so a broken buffer that starts with a valid agy payload would
	// render a plausible Claude-shaped line with quota, state and branch
	// silently dropped (review finding, 2026-08-02).
	var probe struct {
		Product string `json:"product"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		fmt.Fprintln(os.Stderr, "telltale: bad statusline input:", err)
		os.Exit(0)
	}

	noColor := os.Getenv("NO_COLOR") != ""
	if probe.Product == antigravity.Product {
		in, err := antigravity.Parse(bytes.NewReader(raw))
		if err != nil {
			fmt.Fprintln(os.Stderr, "telltale: bad statusline input:", err)
			os.Exit(0)
		}
		fmt.Println(statusline.RenderAntigravity(in, statusline.Options{NoColor: noColor}))
		return
	}
	in, err := claude.Parse(bytes.NewReader(raw))
	if err != nil {
		// A gauge must never crash the host UI: on bad input, render nothing
		// and exit clean. The error goes to stderr for `/statusline` debugging.
		fmt.Fprintln(os.Stderr, "telltale: bad statusline input:", err)
		os.Exit(0)
	}
	fmt.Println(statusline.Render(in, statusline.Options{NoColor: noColor}))
}

func runHUD(args []string) error {
	fs := flag.NewFlagSet("telltale hud", flag.ContinueOnError)
	vendor := fs.String("vendor", "all", "vendor filter at startup: all, claude, codex, gemini, agy, cursor")
	ascii := fs.Bool("ascii", false, "draw with ASCII only (legacy consoles, non-UTF-8 code pages)")
	noTitle := fs.Bool("no-title", false, "do not set the terminal window title")
	if err := fs.Parse(args); err != nil {
		return err
	}

	filter, err := parseFilter(*vendor)
	if err != nil {
		return err
	}

	// ASCII mode is a switch independent of colour. NO_COLOR is not handled
	// here at all: colorprofile caps the profile and Bubble Tea downsamples
	// internally, so there is one mechanism, the standard one, and no
	// --no-color flag of our own.
	useASCII := *ascii || os.Getenv("TELLTALE_ASCII") != ""

	return hud.Run(hud.Options{
		// Every vendor is always registered. An adapter whose vendor is not
		// installed reports ErrVendorAbsent and vanishes from the HUD; there
		// is nothing to configure and nothing to fail.
		Adapters: []model.Adapter{
			claudecode.New(), codex.New(), gemini.New(),
			agyadapter.New(), cursor.New(),
		},
		Filter:  filter,
		ASCII:   useASCII,
		NoTitle: *noTitle,
	})
}

// runCouncil starts the dispatch room.
//
// Council is the one mode that is not a gauge: it spawns vendor CLIs rather
// than reading their files. It shares no keybinding with the HUD and is not
// reachable from it — the only way in is typing this subcommand (ADR-008).
func runCouncil(args []string) error {
	fs := flag.NewFlagSet("telltale council", flag.ContinueOnError)
	dir := fs.String("cd", "", "workspace directory to dispatch turns against (default: cwd)")
	ascii := fs.Bool("ascii", false, "draw with ASCII only (legacy consoles, non-UTF-8 code pages)")
	noTitle := fs.Bool("no-title", false, "do not set the terminal window title")
	if err := fs.Parse(args); err != nil {
		return err
	}

	return council.Run(council.Options{
		Dir:     *dir,
		ASCII:   *ascii || os.Getenv("TELLTALE_ASCII") != "",
		NoTitle: *noTitle,
	})
}

func parseFilter(s string) (hud.Filter, error) {
	switch s {
	case "", "all":
		return hud.FilterAll, nil
	case "claude":
		return hud.FilterClaude, nil
	case "codex":
		return hud.FilterCodex, nil
	case "gemini":
		return hud.FilterGemini, nil
	case "agy", "antigravity":
		// Both spellings: `agy` is the vendor id the footer and the header
		// count print, and `antigravity` is what the product is called
		// everywhere else in this repo. Rejecting the name a reader just saw
		// in the README would be a flag being clever.
		return hud.FilterAntigravity, nil
	case "cursor", "composer":
		// `composer` is what Cursor calls the agent pane; `cursor` is what the
		// footer and the header count print. Both are accepted for the same
		// reason both agy spellings are.
		return hud.FilterCursor, nil
	default:
		return hud.FilterAll, errors.New("unknown --vendor " + s + " (want all, claude, codex, gemini, agy or cursor)")
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `telltale — an honest gauge for your coding agents

usage:
  telltale statusline    (wire into Claude Code settings.json statusLine command)
  telltale hud           cross-vendor session HUD
  telltale council       dispatch room: one brief, several agents, side by side
  telltale version

telltale hud flags:
  --vendor all|claude|codex|gemini|agy|cursor   start with a vendor filter applied
  --ascii                     draw with ASCII only (also TELLTALE_ASCII=1)
  --no-title                  leave the terminal window title alone

telltale council flags:
  --cd <dir>                  workspace to dispatch turns against (default: cwd)
  --ascii                     draw with ASCII only (also TELLTALE_ASCII=1)
  --no-title                  leave the terminal window title alone

statusline and hud read vendor files and never write, never call the network,
and never send anything to a running agent. council is the deliberate
exception: it spawns vendor CLIs, and each column states its own read-only
posture on screen (decisions/008).`)
}
