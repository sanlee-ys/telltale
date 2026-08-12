// telltale — an honest gauge for your coding agents.
//
// One binary, eight modes (decisions/002, decisions/008):
//
//	telltale statusline   read a vendor statusline JSON payload on stdin, print one
//	                      line (Claude Code, or Antigravity CLI via its documented
//	                      product marker — ADR-004)
//	telltale hud          cross-vendor watch-mode TUI
//	telltale council      dispatch room: one brief to several vendor CLIs at once
//	telltale hook <v>     vendor hook relay: a per-turn payload on stdin, token
//	                      counts to ~/.telltale/usage/, nothing on stdout
//	telltale otel <v>     vendor telemetry collector: a loopback OTLP listener the
//	                      vendor's own exporter pushes to, token counts to
//	                      ~/.telltale/usage/ (design.md §7.16a)
//	telltale events       fleet event sink: a loopback server hook emitters POST
//	                      to, a durable log under ~/.telltale/events/, and a
//	                      WebSocket stream per insert (design.md §7.21)
//	telltale snapshot     the fleet's current gauge state as one JSON document,
//	                      for a reader that is a program (design.md §7.22)
//	telltale doctor       launch-time preflight: which vendor binaries are here,
//	                      what version each reports, and what was never checked
//
// The two GAUGES — statusline and hud — share the normalized session model and
// internal/theme's numbers, and nothing else. Neither calls the network or
// sends anything to a running agent; their writes are the statusline's quota
// relay (internal/quotacache, design.md §7.15) and the hook relay's token
// counts (internal/usagecache, §7.16) — numbers-only, under ~/.telltale/,
// never ahead of the thing the vendor is waiting on. council is the
// deliberate exception
// (ADR-008): it spawns vendor CLIs, states each one's read-only posture on
// screen, and shares no keybinding with the HUD. It reuses internal/theme and
// nothing else from the gauges, so that seam is unchanged.
//
// The single binary links the TUI framework, but the statusline path never
// initializes it — only package init runs, and the statusline latency budget is
// re-benchmarked whenever deps change (the binary is spawned fresh on every
// prompt, so init cost is statusline cost).
//
// doctor is the only mode that RUNS a vendor without having been asked for a
// turn, and it is bounded to `<binary> --version`: no model, no session, no
// quota, no credential, no network, and nothing written anywhere (design.md
// §9.42, internal/doctor's package doc).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	agyadapter "github.com/sanlee-ys/telltale/internal/adapter/antigravity"
	"github.com/sanlee-ys/telltale/internal/adapter/claudecode"
	"github.com/sanlee-ys/telltale/internal/adapter/codex"
	"github.com/sanlee-ys/telltale/internal/adapter/cursor"
	"github.com/sanlee-ys/telltale/internal/adapter/gemini"
	grokadapter "github.com/sanlee-ys/telltale/internal/adapter/grok"
	"github.com/sanlee-ys/telltale/internal/antigravity"
	"github.com/sanlee-ys/telltale/internal/claude"
	"github.com/sanlee-ys/telltale/internal/council"
	"github.com/sanlee-ys/telltale/internal/cursorhook"
	"github.com/sanlee-ys/telltale/internal/doctor"
	"github.com/sanlee-ys/telltale/internal/eventsink"
	"github.com/sanlee-ys/telltale/internal/grokotel"
	"github.com/sanlee-ys/telltale/internal/hud"
	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/quotacache"
	"github.com/sanlee-ys/telltale/internal/snapshot"
	"github.com/sanlee-ys/telltale/internal/statusline"
	"github.com/sanlee-ys/telltale/internal/usagecache"
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
	case "hook":
		runHook(os.Args[2:])
	case "otel":
		if err := runOtel(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "telltale otel:", err)
			os.Exit(1)
		}
	case "events":
		if err := runEvents(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "telltale events:", err)
			os.Exit(1)
		}
	case "snapshot":
		if err := runSnapshot(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "telltale snapshot:", err)
			os.Exit(1)
		}
	case "doctor":
		if err := runDoctor(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "telltale doctor:", err)
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
		relayQuota(string(model.VendorAntigravity), quotacache.FromAntigravity(in.Quota, time.Now()))
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
	relayQuota(string(model.VendorClaude), quotacache.FromClaude(in.RateLimits, time.Now()))
}

// relayQuota is the statusline's one write: the quota reading it just
// rendered, relayed for the HUD (internal/quotacache package doc; design.md
// §7.15). It runs AFTER the line is on stdout so its cost can never delay the
// render, and it is best-effort by rule — a gauge must never fail its render
// over its cache, so the error goes nowhere. Payloads without quota (API-key
// logins) relay nothing; the previous reading, if any, ages out on the read
// side rather than being erased by an absence.
func relayQuota(vendor string, windows []quotacache.Window) {
	if len(windows) == 0 {
		return
	}
	dir, err := quotacache.Dir()
	if err != nil {
		return
	}
	_ = quotacache.Write(dir, vendor, windows, time.Now())
}

// runHook is the vendor-hook relay: a payload on stdin, a number on disk, and
// nothing on stdout.
//
// It is a third mode rather than a flag on statusline because it is a
// different seam with a different owner. `telltale statusline` is invoked by a
// host that wants a LINE back and will print whatever it gets; a hook is
// invoked by a vendor mid-turn and its stdout is parsed by that vendor as a
// hook result. So this path prints nothing at all on success — the one place
// in the binary where silence is the contract.
//
// Exit code is 0 on every path, including a broken payload. A hook that exits
// non-zero is a hook that can colour a vendor's turn with an error the user
// did not cause, and telltale's cache is never worth that; the reason goes to
// stderr for `--debug`-style inspection and the turn continues.
// runOtel starts the vendor telemetry collector (design.md §7.16a). Unlike
// the hook relay it is a foreground server: it holds a loopback socket open
// and the VENDOR's exporter pushes to it, so the gauges' no-network-calls
// contract (§4a.5) is untouched — they only ever read the file this writes.
func runOtel(args []string) error {
	if len(args) == 0 {
		return errors.New("want a vendor (grok)")
	}
	if args[0] != "grok" {
		return errors.New("unknown vendor " + args[0] + " (want grok)")
	}
	fs := flag.NewFlagSet("telltale otel grok", flag.ContinueOnError)
	addr := fs.String("addr", grokotel.DefaultAddr, "listen address (loopback only)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	dir, err := usagecache.Dir()
	if err != nil {
		return err
	}
	srv := grokotel.New(dir, func(format string, a ...any) {
		fmt.Printf(format+"\n", a...)
	})
	return srv.Run(*addr)
}

// runEvents starts the fleet event sink (design.md §7.21): a loopback HTTP
// server hook emitters POST to, a durable JSONL log under
// ~/.telltale/events/, and a WebSocket stream that rebroadcasts each insert.
// Like `otel`, it is a foreground server the operator runs on purpose; the
// gauges never read or write its files.
func runEvents(args []string) error {
	fs := flag.NewFlagSet("telltale events", flag.ContinueOnError)
	addr := fs.String("addr", eventsink.DefaultAddr, "listen address (loopback only)")
	retain := fs.Int("retain", 30, "days of events to keep; the sweep runs at startup and then hourly")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *retain <= 0 {
		return errors.New("--retain wants a positive number of days")
	}
	dir, err := eventsink.Dir()
	if err != nil {
		return err
	}
	store, err := eventsink.Open(dir, time.Duration(*retain)*24*time.Hour, nil)
	if err != nil {
		return err
	}
	srv := eventsink.NewServer(store, func(format string, a ...any) {
		fmt.Printf(format+"\n", a...)
	})
	return srv.Run(*addr, time.Hour)
}

func runHook(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "telltale hook: want a vendor (cursor)")
		return
	}
	switch args[0] {
	case "cursor":
		runCursorHook()
	default:
		fmt.Fprintln(os.Stderr, "telltale hook: unknown vendor "+args[0]+" (want cursor)")
	}
}

// runCursorHook reads one afterAgentResponse payload and folds its token
// counts into ~/.telltale/usage/cursor.json (design.md §7.16).
//
// The payload it is handed carries the model's reply text and the user's email
// address alongside the numbers; internal/cursorhook is what makes that
// impossible to keep, and it is called before anything here touches disk.
func runCursorHook() {
	turn, err := cursorhook.Parse(os.Stdin)
	if err != nil {
		// ErrEmpty is not a failure: the hook fired and the vendor had no
		// usage to report. Both cases write nothing and both exit clean; only
		// the message differs, and only on stderr.
		fmt.Fprintln(os.Stderr, "telltale hook cursor:", err)
		return
	}
	delta, ok := usagecache.FromCursorTurn(turn)
	if !ok {
		fmt.Fprintln(os.Stderr, "telltale hook cursor: incomplete token counts; turn not counted")
		return
	}
	dir, err := usagecache.Dir()
	if err != nil {
		return
	}
	if err := usagecache.Add(dir, string(model.VendorCursor), delta, time.Now()); err != nil {
		fmt.Fprintln(os.Stderr, "telltale hook cursor:", err)
	}
}

// runDoctor is the launch-time preflight (design.md §9.42).
//
// A fifth mode rather than a council flag, and the reason is the same one that
// made `hook` its own mode: what it prints goes somewhere else. Council's
// output is a full-screen room, so a preflight rendered inside it would be
// unreadable at exactly the moment it is wanted — before the room opens, and
// often piped into a file or pasted into an issue. This path never enters the
// alternate screen and never touches the TUI.
//
// It is the one place in this binary that runs a vendor. What that buys and
// what it costs is argued in internal/doctor's package doc: `--version` parses
// argv, prints a string and exits, so nothing here starts a turn, spends quota,
// reads a credential, writes under ~/.telltale, or calls the network.
func runDoctor(args []string) error {
	fs := flag.NewFlagSet("telltale doctor", flag.ContinueOnError)
	width := fs.Int("width", 0, "wrap column for the report (default 80)")
	// Every probe gets its own deadline rather than one budget for the run: a
	// wedged seat must cost its own timeout and not the report. The default is
	// generous because the slow case is measured — a cold vendor CLI takes
	// seconds to start on this fleet (design.md §9.33) and a bundled node loads
	// for ~1.2s before it parses a flag — and because the cost of being wrong
	// is asymmetric: too short renders a FAILED that is really this flag's
	// fault, which is the one kind of dishonest cell this mode exists to avoid.
	timeout := fs.Duration("timeout", 15*time.Second, "how long each vendor gets to answer --version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rep := doctor.Run(council.DoctorSeats(), doctor.ExecProbe(*timeout))
	fmt.Print(doctor.Render(rep, doctor.Options{Width: *width}))
	// Exit 0 whatever the report says. A failed CHECK is this command working —
	// it looked, and it is telling you. Exiting non-zero on a missing vendor
	// would make "I have four of the five seats installed" indistinguishable
	// from "doctor itself broke", and would put a red cross in any CI that ever
	// ran it for information.
	return nil
}

// allAdapters is the registered adapter set, in one place because two read
// modes now consume it. The HUD and the snapshot must never disagree about
// which vendors exist — a snapshot missing a vendor the HUD draws would be a
// silent omission in the surface a program trusts.
func allAdapters() []model.Adapter {
	return []model.Adapter{
		claudecode.New(), codex.New(), gemini.New(),
		agyadapter.New(), cursor.New(), grokadapter.New(),
	}
}

// runSnapshot is the machine-readable read mode (design.md §7.22): one scan,
// one JSON document on stdout, exit 0.
//
// A separate mode rather than a flag on `hud`, for the reason `doctor` is one:
// what it prints goes somewhere else. The HUD owns the alternate screen and
// runs until you quit it; this reads once, writes to a pipe, and returns. It
// enters no TUI and renders no gauge, so no v1 gate surface is on this path.
//
// It is a READ, and it takes the gauges' contract with it: it scans vendor
// stores and the account relay, calls no network, reads no credential, and
// writes nothing at all — not even the statusline's quota relay, because this
// mode renders no quota of its own to relay.
func runSnapshot(args []string) error {
	fs := flag.NewFlagSet("telltale snapshot", flag.ContinueOnError)
	vendor := fs.String("vendor", "all", "report one vendor only: all, claude, codex, gemini, agy, cursor, grok")
	compact := fs.Bool("compact", false, "print the document on one line instead of indented")
	// One deadline for the run rather than per vendor, because there is no
	// frame to keep drawing: a scan that cannot finish reports what it has and
	// says so in scan_error, which is a truthful document rather than a hang.
	timeout := fs.Duration("timeout", 10*time.Second, "how long the scan gets before it reports what it has")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// A positional argument is almost always a flag someone forgot the dashes
	// on. Failing here with the correction beats printing a document that
	// silently ignored what was typed.
	if fs.NArg() > 0 {
		return errors.New("unexpected argument " + fs.Arg(0) + " (this mode takes flags only: --vendor, --compact, --timeout)")
	}
	if *timeout <= 0 {
		return errors.New("--timeout wants a positive duration")
	}

	filter, err := parseFilter(*vendor)
	if err != nil {
		return err
	}
	adapters := allAdapters()
	if v, ok := filter.VendorID(); ok {
		adapters = nil
		for _, a := range allAdapters() {
			if a.Vendor() == v {
				adapters = append(adapters, a)
			}
		}
		// A filter naming a vendor no adapter serves would print an empty
		// document that looks like an idle fleet. The seat roster and the
		// adapter roster are allowed to differ (model.VendorGrok was a seat
		// before it was an adapter), so this is a real case and not a typo.
		if len(adapters) == 0 {
			return errors.New("--vendor " + string(v) + " has no HUD adapter, so there is nothing to snapshot")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	snap := hud.Scan(ctx, adapters, time.Now())
	// The account relay rides the scan here exactly as it does in the HUD
	// (design.md §7.15): read after the scan, off any render path, and a
	// missing directory contributes nothing rather than failing the document.
	if dir, err := quotacache.Dir(); err == nil {
		snap.Account = quotacache.ReadAll(dir, snap.At)
	}
	out, err := snapshot.Encode(snapshot.Build(snap, model.DefaultLivenessThresholds), *compact)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(out)
	return err
}

func runHUD(args []string) error {
	fs := flag.NewFlagSet("telltale hud", flag.ContinueOnError)
	vendor := fs.String("vendor", "all", "vendor filter at startup: all, claude, codex, gemini, agy, cursor, grok")
	// The env var is the flag's DEFAULT, not a second mechanism: a typed
	// --hide always wins, including --hide "" to see everything for one launch
	// without unsetting the variable. TELLTALE_ASCII set the precedent for a
	// per-machine standing preference living in the environment.
	hide := fs.String("hide", os.Getenv("TELLTALE_HUD_HIDE"), "comma list of vendors the HUD leaves out entirely (default $TELLTALE_HUD_HIDE); the footer states the hide")
	ascii := fs.Bool("ascii", false, "draw with ASCII only (legacy consoles, non-UTF-8 code pages)")
	noTitle := fs.Bool("no-title", false, "do not set the terminal window title")
	if err := fs.Parse(args); err != nil {
		return err
	}

	filter, err := parseFilter(*vendor)
	if err != nil {
		return err
	}
	hidden, err := parseHide(*hide)
	if err != nil {
		return err
	}
	// A startup filter naming a hidden vendor is a contradiction, and failing
	// loudly here beats opening on an empty grid whose footer says both
	// "filter gemini" and "hidden gemini".
	if v, ok := filter.VendorID(); ok {
		for _, h := range hidden {
			if h == v {
				return errors.New("--vendor " + string(v) + " is on the hide list (--hide / TELLTALE_HUD_HIDE); drop one of the two")
			}
		}
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
		Adapters: allAdapters(),
		Filter:   filter,
		Hide:     hidden,
		ASCII:    useASCII,
		NoTitle:  *noTitle,
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
	seats := fs.String("vendor", "", "who is in the room: a comma list ("+strings.Join(council.SeatNames(), ",")+") or all (default: who the saved room seated, or every vendor that can be driven) — a typed list overrides the saved roster and is what the room saves from then on")
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
	// Accepted and ignored, like --resume below it. It was in muscle memory and
	// in notes; failing on it would be a worse answer than doing what it always
	// meant, which is now the default.
	fs.Bool("write", false, "accepted and ignored — the room writes by default now; --read is the opposite")
	auto := fs.Bool("auto", false, "let the gated seat approve its own tool calls instead of asking you")
	brief := fs.String("brief", "", "file of shared operating context handed to every vendor on its first turn (or TELLTALE_COUNCIL_BRIEF)")
	resume := fs.Bool("resume", false, "reattach to the saved room (this is the default; the flag is kept for muscle memory)")
	fresh := fs.Bool("fresh", false, "start a new room instead of reattaching to the saved one")
	trace := fs.String("trace", "", "append each turn's measured clock — spawn, wait, stream — to this file")
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
		TracePath: *trace,
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
	case "grok":
		// One spelling, because grok's binary name and its product name are the
		// same word (model.VendorGrok). There is no second thing to accept.
		return hud.FilterGrok, nil
	default:
		return hud.FilterAll, errors.New("unknown --vendor " + s + " (want all, claude, codex, gemini, agy, cursor or grok)")
	}
}

// parseHide reads the `--hide` comma list into vendor ids (§7.20). It rides
// parseFilter for the vocabulary — both agy spellings, `composer` for cursor —
// so the two flags can never disagree about what a vendor is called, and it
// rejects `all`: a HUD told to hide every vendor is a request to not run it.
// The result is deduplicated and sorted so the footer's wording is stable no
// matter how the list was typed.
func parseHide(s string) ([]model.VendorID, error) {
	if s == "" {
		return nil, nil
	}
	seen := map[model.VendorID]bool{}
	var out []model.VendorID
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		f, err := parseFilter(part)
		if err != nil {
			return nil, errors.New("unknown --hide vendor " + part + " (want claude, codex, gemini, agy, cursor or grok)")
		}
		v, ok := f.VendorID()
		if !ok {
			return nil, errors.New("--hide all would hide every vendor; run nothing instead")
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// usageText is the long help.
//
// It is a package-level const rather than a literal inside usage() so
// TestUsageNamesEverySeat can read it. The council seat roster appears here as
// hand-wrapped prose at a fixed column, which is the one place in this repo the
// roster cannot simply be interpolated from council.SeatNames() without the
// interpolation fighting the wrapping — so the words stay written out and a
// test pins them against SeatNames instead. This block is exactly where that
// drift happened: grok became the fifth seat (§9.39) and the help went on
// naming four, telling a reader a supported vendor did not exist.
const usageText = `telltale — an honest gauge for your coding agents

usage:
  telltale statusline    (wire into Claude Code settings.json statusLine command)
  telltale hud           cross-vendor session HUD
  telltale council       dispatch room: one brief, several agents, side by side
  telltale hook cursor   (wire into ~/.cursor/hooks.json as an afterAgentResponse
                         command hook) read one turn's token counts on stdin,
                         add them to this machine's running total, print nothing
  telltale otel grok     receive grok's external OpenTelemetry export on
                         loopback and add each api request's token counts to
                         this machine's running total. The push is grok's:
                         enable it with [telemetry] otel_enabled = true and
                         otel_logs_exporter = "otlp" in ~/.grok/config.toml,
                         then leave this running while grok runs
  telltale events        fleet event sink: receive one hook event per POST on
                         loopback, append it to a durable log under
                         ~/.telltale/events/, and rebroadcast it to every
                         connected WebSocket client. Any process that can pipe
                         JSON is a source — wire tools/emit-event.py as a hook
                         command with --source-app <name> as the one per-repo
                         edit. Nothing renders these events yet; the sink runs
                         dark until something connects to /stream
  telltale snapshot      read the fleet once and print its state as JSON on
                         stdout: a per-vendor block and a pre-computed fleet
                         rollup, so an agent gets its answer from one command
                         and one parse. Absent is null and a measured zero is
                         0; a derived value is named in "estimated" and a
                         field the vendor can never source is named in
                         "unsupported". Numbers and keys only — no session
                         name, workspace, transcript or reply text
  telltale doctor        launch-time preflight: which vendor binaries are on
                         this machine, where each was found, what version it
                         reports — and, said out loud rather than left blank,
                         what was never checked. Auth and network are always
                         "not checked": nothing here probes a login or calls
                         the network, and a preflight that implied otherwise
                         would be trusted on the one day it was wrong
  telltale version

telltale hud flags:
  --vendor all|claude|codex|gemini|agy|cursor|grok
                              start with a vendor filter applied
  --hide <list>               comma list of vendors the HUD leaves out entirely
                              (default $TELLTALE_HUD_HIDE; --hide "" overrides
                              the variable for one launch). The footer states
                              the hide, and the v cycle skips those vendors
  --ascii                     draw with ASCII only (also TELLTALE_ASCII=1)
  --no-title                  leave the terminal window title alone

telltale snapshot flags:
  --vendor all|claude|codex|gemini|agy|cursor|grok
                              report one vendor only (default all)
  --compact                   print the document on one line instead of
                              indented
  --timeout <dur>             how long the scan gets before it reports what it
                              has (default 10s). A scan that runs out says so
                              in scan_error rather than hanging or lying

telltale doctor flags:
  --timeout <dur>             how long each vendor gets to answer --version
                              (default 15s). One deadline per seat, not one for
                              the run: a wedged vendor costs its own timeout and
                              reports a failed check, never the whole report
  --width <n>                 wrap column for the report (default 80)
It prints words and no colour, so it reads the same in a terminal, in a pipe and
in a pasted issue; --ascii and NO_COLOR have nothing to switch off.

telltale otel grok flags:
  --addr <host:port>          listen address (default 127.0.0.1:4318, the OTLP
                              http default). Loopback only; any other host is
                              refused at startup

telltale events flags:
  --addr <host:port>          listen address (default 127.0.0.1:4519).
                              Loopback only; any other host is refused at
                              startup
  --retain <days>             days of events to keep (default 30). The sweep
                              runs at startup and then hourly; a day file is
                              deleted only when its whole day is past the
                              window

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
                              agy, cursor and grok, or "all" (default: who the
                              saved room seated, or every seat that can be
                              driven). A typed list overrides the saved roster
                              and is the room the next save records. A seat that
                              is not installed — or is installed and cannot be
                              driven — folds out of the grid, so the seats
                              that answer get the width, and one line under
                              the header names what was folded and why. "all"
                              keeps every seat on screen; a list seats exactly
                              those and dispatches to nobody else. Different
                              from an @mention, which routes one turn.
                              /seat <list> does the same from INSIDE the room,
                              and /seat all puts everyone back. An unseated seat
                              keeps its thread AND its process — it stops being
                              drawn and stops being dispatched to, nothing more,
                              so coming back cannot fail. Sitting a seat out is
                              cheaper still: one nobody addresses is already
                              quiet and already free.
  --brief <file>            shared operating context handed to every vendor on
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
                              The posture also moves from INSIDE the room: /read
                              makes it read-only at once, /write asks y/n before
                              letting it write again. The flag is for the room
                              you want at the door; those are for the one you
                              change your mind about. Seats move on their next
                              turn.
                              --write is still accepted and does nothing.
  --auto                      let the gated seat approve its own tool calls
                              instead of asking. Reach for it when you are not
                              watching; it is the one setting that leaves
                              nothing in the room asking permission for
                              anything.
                              The a key does the same from inside the room, and
                              it is on the approval CARD rather than in the
                              composer because that is where you form the
                              preference: y approves, n denies, a approves this
                              call and every one after it. a alone in view mode
                              turns the asking back on, and while it is off the
                              footer carries a cell that says so.
  --resume                    reattach to the saved room. This is the DEFAULT —
                              the flag is kept for muscle memory and does the
                              same thing. The turn counter continues and each
                              vendor picks up its OWN session; a seat whose
                              thread the vendor no longer has says so and
                              starts fresh. Not a posture: --read is never
                              restored from the file, it is retyped or it is
                              not in effect.
  --trace <file>              append one line per seat per turn recording where
                              that turn's time went: spawn (launching the vendor
                              process), wait (launch, or the moment the turn was
                              handed to a process already running, until the
                              first line comes back) and stream (first line to
                              last). A segment that did not happen prints "-",
                              never 0 — a seat whose process outlives the turn
                              spawns on its first turn and on no other.
                              Off by default, and it adds nothing to the room:
                              this answers "which phase took the 44 seconds",
                              which is worth a file and not worth four more
                              numbers on every column. The file is appended to,
                              so runs accumulate.
  --ascii                     draw with ASCII only (also TELLTALE_ASCII=1)
  --no-title                  leave the terminal window title alone

statusline and hud read vendor files, never call the network, and never send
anything to a running agent. The state the gauges write lives under
~/.telltale/ and is keys and numbers only: the statusline relays the quota it
just rendered (quota/<vendor>.json) so the hud can attribute account quota per
vendor, the cursor hook adds each turn's token counts to a running total
(usage/<vendor>.json) so the hud can say what this machine spent, and council
keeps one room file (council/room.json) holding the session ids reattaching
needs — no transcript, output, prompt or brief content in any of them. council
is the deliberate exception to reading only: it spawns vendor CLIs, and each
column states its own posture on screen.

A fourth store carries content, and it is named as an exception rather than
counted with the three. The event sink (telltale events) keeps each hook
payload VERBATIM under ~/.telltale/events/ — content, not keys and numbers.
What contains it is scope, not redaction: the sink is its own foreground mode
that you start, its server binds loopback only, and no gauge reads or renders
those files. The keys-and-numbers rule above still binds every store the
gauges themselves write.

Tokens spent are NOT quota. Cursor exposes no account limit without a network
call, so the hud shows what was consumed and never a percentage of anything.`

func usage() {
	fmt.Fprintln(os.Stderr, usageText)
}
