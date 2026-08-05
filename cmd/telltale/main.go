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
	dir := fs.String("cd", "", "move the room's workspace for this launch (default: where the saved room was, or cwd) — /cd inside the room does the same")
	seats := fs.String("vendor", "", "who is in the room: a comma list (claude,codex,agy,cursor) or all; default seats every vendor that can be driven")
	ascii := fs.Bool("ascii", false, "draw with ASCII only (legacy consoles, non-UTF-8 code pages)")
	noTitle := fs.Bool("no-title", false, "do not set the terminal window title")
	// The room writes by default, and --read is the opt-out. Posture used to be
	// an opt-IN flag, which was right while nothing could ask before a write and
	// "this room can write" therefore meant "this room writes without you". The
	// gated seat's approval card retired that reading: the thing the flag was
	// guarding is now guarded per call, and all the flag still did was make a
	// room you opened to get work done unable to do any until you remembered a
	// word. Same move already made for the workspace, which stopped being an
	// invocation input for the same reason.
	read := fs.Bool("read", false, "open a deliberation-only room: seats answer, and none of them may touch the workspace")
	// Accepted and ignored, like --resume above it. It was in muscle memory and
	// in notes; failing on it would be a worse answer than doing what it always
	// meant, which is now the default.
	fs.Bool("write", false, "accepted and ignored — the room writes by default now; --read is the opposite")
	auto := fs.Bool("auto", false, "let the gated seat approve its own tool calls instead of asking you")
	brief := fs.String("brief", "", "file of shared operating context handed to every vendor on its first turn (or TELLTALE_COUNCIL_BRIEF)")
	resume := fs.Bool("resume", false, "reattach to the saved room (this is the default; the flag is kept for muscle memory)")
	fresh := fs.Bool("fresh", false, "start a new room instead of reattaching to the saved one")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Parsed here, where the flag lives, and rejected before the alternate
	// screen is entered — the same discipline --brief and --resume follow: a
	// misspelled vendor name must be a line on stderr, not a card behind a TUI
	// the user has to quit to read.
	room, err := council.ParseSeats(*seats)
	if err != nil {
		return err
	}

	return council.Run(council.Options{
		Dir:       *dir,
		Seats:     room,
		ASCII:     *ascii || os.Getenv("TELLTALE_ASCII") != "",
		NoTitle:   *noTitle,
		Write:     !*read,
		Auto:      *auto,
		BriefPath: *brief,
		Resume:    *resume,
		Fresh:     *fresh,
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

telltale council is ONE persistent room. Run it with no arguments: it reopens
the saved room, reattaches every vendor's own session, and continues the
conversation. Change what repo it works in from INSIDE the room — type
/cd <dir> in the composer — never with a flag. Seats move on their next turn.

telltale council flags:
  --cd <dir>                  move the room's workspace at launch (default:
                              where the saved room was, or cwd). The daily path
                              never needs it; /cd inside the room does the same
  --fresh                     start a new room instead of reattaching. If a
                              saved room exists it is named once before the
                              first dispatch replaces it
  --vendor <list>             who is in the room: a comma list of claude, codex,
                              agy and cursor, or "all". By default a seat that
                              is not installed — or is installed and cannot be
                              driven — folds out of the grid, so the seats that
                              answer get the width, and one line under the
                              header names what was folded and why. "all" keeps
                              every seat on screen; a list seats exactly those
                              and dispatches to nobody else. Different from an
                              @mention, which routes one turn.
  --brief <file>              shared operating context handed to every vendor on
                              its first turn — who you are, what the lanes are,
                              whatever convention they would otherwise each guess
                              at separately. Also TELLTALE_COUNCIL_BRIEF. The file
                              is read from disk and never stored by telltale.
  --read                      open a room that can only talk. Seats answer and
                              compare; none of them may edit a file or run a
                              command. The opposite is the DEFAULT: a plain
                              council writes, because a room you opened to get
                              work done should be able to do it without you
                              remembering a word first.
                              What guards the default is the approval card, not
                              the flag. The seat that can be asked (claude) asks
                              first: every tool call that changes anything
                              raises a card, y approves and n denies, and
                              nothing runs until you answer. The other three are
                              batch CLIs with no channel to ask on, so they act
                              unasked and their columns say so — which is why
                              the workspace is the real containment. Point --cd
                              at a git worktree if that matters.
                              --write is still accepted and does nothing.
  --auto                      let the gated seat approve its own tool calls
                              instead of asking. Reach for it when you are not
                              watching; it is the one setting that leaves
                              nothing in the room asking permission for anything.
  --resume                    reattach to the saved room. This is the DEFAULT —
                              the flag is kept for muscle memory and does the
                              same thing. The turn counter continues and each
                              vendor picks up its OWN session; a seat whose
                              thread the vendor no longer has says so and
                              starts fresh. Not a posture: --read is never
                              restored from the file, it is retyped or it is
                              not in effect.
  --ascii                     draw with ASCII only (also TELLTALE_ASCII=1)
  --no-title                  leave the terminal window title alone

statusline and hud read vendor files and never write, never call the network,
and never send anything to a running agent. council is the deliberate
exception: it spawns vendor CLIs, and each column states its own posture on
screen. It is also the only mode that writes
anything at all — one room file, ~/.telltale/council/room.json, holding the
session ids reattaching needs and no transcript, output or brief content.`)
}
