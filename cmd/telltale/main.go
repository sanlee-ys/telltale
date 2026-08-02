// telltale — an honest gauge for your coding agents.
//
// One binary, two modes (decisions/002):
//
//	telltale statusline   read Claude Code's statusline JSON on stdin, print one line
//	telltale hud          cross-vendor watch-mode TUI
//
// The two paths share the normalized session model and internal/theme's
// numbers, and nothing else. The single binary links the TUI framework, but
// the statusline path never initializes it — only package init runs, and the
// statusline latency budget is re-benchmarked whenever deps change (the
// binary is spawned fresh on every prompt, so init cost is statusline cost).
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/sanlee-ys/telltale/internal/adapter/claudecode"
	"github.com/sanlee-ys/telltale/internal/adapter/codex"
	"github.com/sanlee-ys/telltale/internal/adapter/gemini"
	"github.com/sanlee-ys/telltale/internal/antigravity"
	"github.com/sanlee-ys/telltale/internal/claude"
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
	var probe struct {
		Product string `json:"product"`
	}
	_ = json.Unmarshal(raw, &probe) // a probe failure falls through to the Claude parser's own error

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
	vendor := fs.String("vendor", "all", "vendor filter at startup: all, claude, codex, gemini")
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
		Adapters: []model.Adapter{claudecode.New(), codex.New(), gemini.New()},
		Filter:   filter,
		ASCII:    useASCII,
		NoTitle:  *noTitle,
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
	default:
		return hud.FilterAll, errors.New("unknown --vendor " + s + " (want all, claude, codex or gemini)")
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `telltale — an honest gauge for your coding agents

usage:
  telltale statusline    (wire into Claude Code settings.json statusLine command)
  telltale hud           cross-vendor session HUD
  telltale version

telltale hud flags:
  --vendor all|claude|codex|gemini   start with a vendor filter applied
  --ascii                     draw with ASCII only (also TELLTALE_ASCII=1)
  --no-title                  leave the terminal window title alone`)
}
