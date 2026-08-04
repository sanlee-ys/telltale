package council

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

var update = flag.Bool("update", false, "rewrite golden files")

// golden compares a rendered frame against testdata/golden/<name>.txt.
//
// Every golden renders with PlainStyles, the identity set, so the bytes do not
// depend on the CI terminal's colour profile — the same split internal/hud
// uses (design.md §7.9). Colour is asserted separately, in TestPhaseColors.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".txt")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run: go test ./internal/council -update)", err)
	}
	if got != string(want) {
		t.Errorf("frame differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

// room is a fully seated council with deterministic content. Tests construct
// State directly: no terminal, no program loop, no vendor process.
func room() State {
	st := NewState()
	st.Width, st.Height = 120, 24
	st.Workspace = "/home/dev/code/telltale"
	st.Home = "/home/dev"
	st.Mode = ModeViewing
	st.Columns = []Column{
		{
			Vendor: model.VendorClaude, Label: "Claude Code",
			Avail:   AvailInstalled,
			Sandbox: SandboxClaim{Level: SandboxTools, Detail: "tool allowlist"},
			Gran:    GranTokens, Phase: PhaseIdle,
		},
		// The fixture mirrors what the live vendors actually measured to, so the
		// goldens show the room a user really sees rather than an optimistic one.
		{
			Vendor: model.VendorCodex, Label: "Codex",
			Avail:   AvailInstalled,
			Sandbox: SandboxClaim{Level: SandboxRequested, Detail: "degrades to a spawn failure on windows"},
			Gran:    GranFinalOnly, Phase: PhaseIdle,
		},
		{
			Vendor: model.VendorAntigravity, Label: "Antigravity",
			Avail:   AvailInstalled,
			Sandbox: SandboxClaim{Level: SandboxNone, Detail: "measured not to restrict writes"},
			Gran:    GranFinalOnly, Phase: PhaseIdle,
		},
	}
	return st
}

func render(st State) string { return Render(st, PlainStyles(), GlyphsFor(false)) }

func TestEmptyRoomWide(t *testing.T) {
	golden(t, "empty-wide", render(room()))
}

func TestEmptyRoomTabs(t *testing.T) {
	st := room()
	st.Width = 80
	golden(t, "empty-tabs", render(st))
}

func TestFloor(t *testing.T) {
	st := room()
	st.Width, st.Height = 40, 20
	golden(t, "floor-width", render(st))

	st = room()
	st.Width, st.Height = 120, 4
	golden(t, "floor-height", render(st))
}

// TestUnavailableColumnsSayWhichFailure is the honest-degradation case: "not
// installed" and "installed but not drivable" are different facts and must not
// render alike.
func TestUnavailableColumnsSayWhichFailure(t *testing.T) {
	st := room()
	st.Columns[1].Avail = AvailNotInstalled
	st.Columns[1].Note = "not found on PATH (looked for codex)"
	st.Columns[1].Sandbox = SandboxClaim{}
	st.Columns[2].Avail = AvailUnusable
	st.Columns[2].Note = "resolves to a shell shim (agy.cmd) and takes its prompt as an argument; set TELLTALE_COUNCIL_AGY_BIN to the real executable"
	st.Columns[2].Sandbox = SandboxClaim{}
	got := render(st)
	golden(t, "unavailable", got)

	if !strings.Contains(got, "not found on PATH") {
		t.Error("the not-installed column does not say it was not found")
	}
	if !strings.Contains(got, "shell shim") {
		t.Error("the unusable column does not say why it is unusable")
	}
}

// TestWaitingIsNotStreaming is the core honesty assertion for this surface.
//
// A column whose vendor reports nothing until it finishes must SAY so. If
// PhaseWaiting ever renders identically to PhaseStreaming, the room is claiming
// live output it does not have — the same failure as a gauge that cannot tell
// "no data" from "zero" (README, the honest-gauge rule).
func TestWaitingIsNotStreaming(t *testing.T) {
	waiting := room()
	waiting.Turn = 1
	waiting.Columns[0].Phase = PhaseWaiting
	waiting.Columns[0].Gran = GranFinalOnly

	streaming := room()
	streaming.Turn = 1
	streaming.Columns[0].Phase = PhaseStreaming
	streaming.Columns[0].Body = "Considering the tradeoffs."

	a, b := render(waiting), render(streaming)
	if a == b {
		t.Fatal("a waiting column renders identically to a streaming one")
	}
	// A single word, because the sentence is wrapped into a 37-cell column and
	// any longer phrase would be split across lines by the renderer under test.
	if !strings.Contains(a, "incremental") {
		t.Error("the waiting column does not explain that this vendor cannot stream")
	}
	golden(t, "waiting-vs-streaming", a)
}

// TestSandboxBadgesAreNeverBlanket guards ADR-008's correction: the UI must
// state a posture per vendor, and must never render an unqualified "read-only".
func TestSandboxBadgesAreNeverBlanket(t *testing.T) {
	got := render(room())
	for _, want := range []string{"ro:tools", "ro:requested", "unsandboxed"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing per-vendor sandbox badge %q", want)
		}
	}
	// Three vendors, three DIFFERENT badges. If they ever converge on one
	// string the room has started making a blanket claim again, which is the
	// specific thing ADR-008 was amended twice to stop.
	if strings.Count(got, "ro:tools") != 1 || strings.Count(got, "unsandboxed") != 1 {
		t.Error("the badges are no longer per-vendor")
	}
	// The bare claim, with no mechanism named, is the thing that was wrong in
	// the first draft of the ADR. It must not come back.
	//
	// "unsandboxed" is stripped before the check rather than added to the
	// allowed list: it is the exact OPPOSITE claim, and a substring search that
	// cannot tell the two apart would fail the honest badge for containing the
	// dishonest one.
	bare := strings.ReplaceAll(got, "unsandboxed", "")
	for _, banned := range []string{"read-only sandbox", "sandboxed", "ro:enforced"} {
		if strings.Contains(bare, banned) {
			t.Errorf("frame makes an unearned blanket claim: %q", banned)
		}
	}
}

func TestMixedPhases(t *testing.T) {
	st := room()
	st.Turn = 2
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Body = "Use the native resume path. Re-sending the transcript grows input quadratically."
	st.Columns[1].Phase = PhaseFailed
	st.Columns[1].Note = "exit 1: not signed in — run `codex login`"
	st.Columns[2].Phase = PhaseCancelled
	st.Columns[2].Body = "Partly agree, though the"
	st.Columns[2].Note = "cancelled — output above is partial"
	golden(t, "mixed-phases", render(st))
}

func TestComposeMode(t *testing.T) {
	st := room()
	st.Mode = ModeComposing
	st.Draft = "should council resume sessions or re-send the transcript?"
	golden(t, "compose", render(st))
}

func TestHelp(t *testing.T) {
	st := room()
	st.Help = true
	golden(t, "help", render(st))
}

func TestASCII(t *testing.T) {
	st := room()
	st.ASCII = true
	st.Turn = 1
	st.Columns[0].Phase = PhaseStreaming
	st.Columns[0].Body = "Reading the adapter interface."
	golden(t, "ascii", Render(st, PlainStyles(), GlyphsFor(true)))
}

// TestNoLineExceedsTheTerminalWidth sweeps widths across every mode. This is the
// rule that outranks aesthetics: a line one cell too long wraps and shears the
// whole grid.
func TestNoLineExceedsTheTerminalWidth(t *testing.T) {
	widths := []int{60, 72, 80, 95, 96, 100, 120, 160, 201}
	states := map[string]func() State{
		"empty": room,
		"compose": func() State {
			st := room()
			st.Mode = ModeComposing
			st.Draft = strings.Repeat("brief ", 40)
			return st
		},
		"help": func() State { st := room(); st.Help = true; return st },
		"busy": func() State {
			st := room()
			st.Turn = 1
			st.Columns[0].Phase = PhaseStreaming
			st.Columns[0].Body = strings.Repeat("a long streamed reply with no short words anywhere ", 6)
			st.Columns[1].Phase = PhaseWaiting
			st.Columns[2].Phase = PhaseFailed
			st.Columns[2].Note = "exit 127: " + strings.Repeat("verylongtokenwithnobreaks", 4)
			return st
		},
		"unavailable": func() State {
			st := room()
			for i := range st.Columns {
				st.Columns[i].Avail = AvailNotInstalled
				st.Columns[i].Note = strings.Repeat("not found on PATH ", 5)
			}
			return st
		},
		"notice": func() State { st := room(); st.Notice = strings.Repeat("a notice that runs long ", 8); return st },
	}

	for name, mk := range states {
		for _, w := range widths {
			for _, ascii := range []bool{false, true} {
				st := mk()
				st.Width, st.Height = w, 24
				out := Render(st, PlainStyles(), GlyphsFor(ascii))
				for i, line := range strings.Split(out, "\n") {
					if got := lipgloss.Width(line); got > w {
						t.Errorf("%s w=%d ascii=%v: line %d is %d cells (max %d): %q",
							name, w, ascii, i, got, w, line)
					}
				}
			}
		}
	}
}

// TestFrameHeightFitsTheTerminal keeps the room from scrolling its own chrome
// off the top, which would take the header and the mode line with it.
func TestFrameHeightFitsTheTerminal(t *testing.T) {
	for _, h := range []int{10, 12, 16, 24, 40} {
		for _, w := range []int{60, 80, 120} {
			st := room()
			st.Width, st.Height = w, h
			n := len(strings.Split(Render(st, PlainStyles(), GlyphsFor(false)), "\n"))
			if n > h {
				t.Errorf("w=%d h=%d: frame is %d lines, terminal is %d", w, h, n, h)
			}
		}
	}
}

// TestPhaseColors is the style half of the split: one escape code per terminal
// phase, asserted with the coloured set rather than the plain one.
func TestPhaseColors(t *testing.T) {
	sty := NewStyles(true)
	cases := []struct {
		phase Phase
		style lipgloss.Style
	}{
		{PhaseDone, sty.SevOK},
		{PhaseFailed, sty.SevCrit},
		{PhaseCancelled, sty.SevWarn},
	}
	for _, c := range cases {
		got := sty.ForPhase(c.phase).Render("x")
		want := c.style.Render("x")
		if got != want {
			t.Errorf("phase %v renders %q, want %q", c.phase, got, want)
		}
	}
	// In-flight phases are deliberately NOT a severity.
	for _, p := range []Phase{PhaseIdle, PhaseWaiting, PhaseStreaming} {
		if sty.ForPhase(p).Render("x") != sty.Muted.Render("x") {
			t.Errorf("phase %v is coloured as a severity; in-flight is not a severity", p)
		}
	}
}

// TestRenderIsPure guards the contract the goldens depend on: two renders of
// the same State are byte-identical, so nothing in the render path reads a
// clock, the filesystem or the environment.
func TestRenderIsPure(t *testing.T) {
	st := room()
	st.Turn = 3
	st.Columns[0].Phase = PhaseStreaming
	st.Columns[0].Body = "partial"
	if render(st) != render(st) {
		t.Fatal("Render is not pure over State")
	}
}

// TestSanitizeKeepsParagraphsAndKillsLineBreakers: vendor output is arbitrary
// model text, and a U+2028 in a fixed-width column tears the grid apart.
func TestSanitizeKeepsParagraphsAndKillsLineBreakers(t *testing.T) {
	// Written as escapes on purpose: these are invisible in an editor, and
	// this is the one place their identity has to be exact.
	in := "one\u2028two\u2029three\tfour\rfive\x00six\nseven"
	got := sanitize(in)
	for _, bad := range []string{"\u2028", "\u2029", "\t", "\r", "\x00"} {
		if strings.Contains(got, bad) {
			t.Errorf("sanitize kept %q", bad)
		}
	}
	if !strings.Contains(got, "\n") {
		t.Error("sanitize dropped the newline; paragraphing is the vendor's, not ours")
	}
	// The draft variant flattens newlines too: the prompt is one logical line.
	if strings.Contains(sanitizeKeepingSpace(in), "\n") {
		t.Error("sanitizeKeepingSpace kept a newline in a single-line draft")
	}
}

// TestReportedCostRendersAndAbsentCostDoesNot is the honest-gauge rule on the
// one number council can show. A vendor that reported zero and a vendor that
// reported nothing must not render alike — and council never derives a cost
// from token counts, so "nothing" is a real and common state.
func TestReportedCostRendersAndAbsentCostDoesNot(t *testing.T) {
	zero := 0.0
	real := 0.0123

	absent := room()
	if strings.Contains(render(absent), "$") {
		t.Error("a column with no reported cost rendered a dollar figure")
	}

	reportedZero := room()
	reportedZero.Columns[0].CostUSD = &zero
	if !strings.Contains(render(reportedZero), "$0.0000") {
		t.Error("a vendor that reported zero did not render $0.0000")
	}

	reported := room()
	reported.Turn = 1
	reported.Columns[0].Phase = PhaseDone
	reported.Columns[0].Body = "Resume beats re-sending."
	reported.Columns[0].CostUSD = &real
	got := render(reported)
	if !strings.Contains(got, "$0.0123") {
		t.Error("a reported cost was not rendered")
	}
	golden(t, "reported-cost", got)
}

// TestAddressedTurnGolden shows the routed compose state: two vendors named,
// the footer saying so.
func TestAddressedTurnGolden(t *testing.T) {
	st := room()
	st.Mode = ModeComposing
	st.Draft = "@codex @agy is the resume flag right?"
	st.Route, _ = ParseRoute(st.Draft)
	golden(t, "compose-addressed", render(st))
}

// TestUnaddressedColumnSaysSo: a vendor left out of a turn keeps its previous
// reply on screen, because that is still the last thing it said — but it must
// not read as a third opinion on the new brief.
func TestUnaddressedColumnSaysSo(t *testing.T) {
	st := room()
	st.Turn = 2
	st.Columns[0].Phase = PhaseStreaming
	st.Columns[0].Body = "Looking at the resume path now."
	st.Columns[1].Phase = PhaseDone
	st.Columns[1].Body = "An older answer from turn 1."
	st.Columns[1].Note = "not addressed in turn 2"
	st.Columns[2].Phase = PhaseWaiting

	got := render(st)
	if !strings.Contains(got, "not addressed in turn 2") {
		t.Error("a column left out of the turn does not say so")
	}
	golden(t, "unaddressed-column", got)
}
