package council

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/council/runner"
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

// deadSeats is the room a real machine has: one vendor missing, one installed
// behind a shim council refuses to drive.
func deadSeats() State {
	st := room()
	st.Columns[1].Avail = AvailNotInstalled
	st.Columns[1].Note = "not found on PATH (looked for codex)"
	st.Columns[1].Sandbox = SandboxClaim{}
	st.Columns[2].Avail = AvailUnusable
	st.Columns[2].Note = "resolves to a shell shim (agy.cmd) and takes its prompt as an argument; set TELLTALE_COUNCIL_AGY_BIN to the real executable"
	st.Columns[2].Sandbox = SandboxClaim{}
	return st
}

// TestUnavailableColumnsSayWhichFailure is the honest-degradation case: "not
// installed" and "installed but not drivable" are different facts and must not
// render alike.
//
// Asserted with --vendor all, which is where the full cards live now. By
// default those seats fold out of the grid — see TestDeadSeatsFoldOut — and the
// distinction this test guards moves to the notice line, where
// TestTheCollapsedSeatSaysWhichFailure keeps it.
func TestUnavailableColumnsSayWhichFailure(t *testing.T) {
	st := room()
	st.Seats = Seats{All: true}
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
	// The distinction has to be carried by a WORD somewhere visible, never by
	// the absence of motion — a spinner that is not spinning and a column that
	// has nothing to say look identical in a screenshot, and identical to a user
	// glancing at three of them.
	//
	// It is checked in two places on purpose, because §9.14 moved which one does
	// the work. The BODY no longer recites the mechanism; what it says is that
	// the seat is working and what to expect. The HEADER is what names the state,
	// and it is drawn on every frame, above the scroll, in both glyph sets — so
	// it is the carrier that cannot be scrolled away from.
	if !strings.Contains(a, "arrives whole") {
		t.Error("the waiting column does not say what to expect")
	}
	if !strings.Contains(a, "waiting") {
		t.Error("no word on the frame names the waiting state")
	}
	if strings.Contains(b, "waiting") {
		t.Error("a streaming column claims to be waiting")
	}
	// And the vendor-internals vocabulary is gone from the reading area. This is
	// the assertion that keeps the explanation on the help page rather than
	// creeping back into the body of every waiting turn.
	if strings.Contains(a, "incremental") {
		t.Error("the waiting body is explaining council's plumbing again")
	}
	golden(t, "waiting-vs-streaming", a)
}

// TestWaitingOnYouIsNotStreaming is the same claim about a seat stopped on the
// OPERATOR, and it now guards both halves of it.
//
// TestWaitingIsNotStreaming above guards the word for a seat with nothing to
// show. This guards the word AND the figure for a seat with a stopped process
// behind it, because the two arrived one at a time. §9.45 corrected the number —
// the header stopped charging a person's reading time to the vendor — and left
// `⋮ streaming` sitting over it, which is a corrected figure under a false word.
// The amendment finishes it: the header says `needs you`, in the strip's own
// vocabulary, and carries no clock at all.
//
// Two states that must not render alike, exactly as above: one seat blocked on
// the operator, one seat with the identical wall clock and no card at all. And a
// third, because the split has to SURVIVE the answer — the same seat with the
// card gone states the vendor's twelve seconds again.
func TestWaitingOnYouIsNotStreaming(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 5, 0, 0, time.UTC)

	blocked := room()
	blocked.Turn, blocked.Now = 1, now
	blocked.Columns[0].Phase, blocked.Columns[0].TurnN = PhaseStreaming, 1
	blocked.Columns[0].Prompt = "write the file"
	blocked.Columns[0].Started = now.Add(-5 * time.Minute)
	blocked.Gates = []PendingGate{{
		Vendor: model.VendorClaude, RequestID: "r1", Text: "Write: a.txt",
		StoppedAt: now.Add(-4*time.Minute - 48*time.Second),
	}}

	working := room()
	working.Turn, working.Now = 1, now
	working.Columns[0].Phase, working.Columns[0].TurnN = PhaseStreaming, 1
	working.Columns[0].Prompt = "write the file"
	working.Columns[0].Started = now.Add(-5 * time.Minute)
	working.Columns[0].Body = "Considering the tradeoffs."

	a, b := render(blocked), render(working)
	if a == b {
		t.Fatal("a seat stopped on the operator renders identically to one that is working")
	}
	// The seat that really has been streaming for five minutes keeps its five
	// minutes, and keeps the word for them too.
	if !strings.Contains(b, "streaming 5m0s") {
		t.Error("a seat nobody blocked lost its own five minutes")
	}
	// The blocked one gives up both. The word is the room's own — the same phrase
	// the card two rows under it spells `waiting on you` and the strip spells
	// `NEEDS YOU` — and no clock follows it, because neither figure is time this
	// seat spent in this state.
	if strings.Contains(a, "streaming") {
		t.Error("a stopped seat still claims output is arriving")
	}
	if !strings.Contains(a, needsYouWord) {
		t.Errorf("no word on the frame says the seat is stopped on the operator:\n%s", a)
	}
	if strings.Contains(a, needsYouWord+" 12s") || strings.Contains(a, needsYouWord+" 4m48s") {
		t.Error("the state word grew a clock that is not time spent in that state")
	}
	// The operator's own figure is where §9.45 put it, on the turn's separator,
	// and it is still counting.
	if !strings.Contains(a, "you 4m48s") {
		t.Error("no figure on the frame says how long the room waited on the operator")
	}
	// And the working seat says nothing about the operator — absent, not zero,
	// which is the distinction zero-vs-absent.txt exists for.
	if strings.Contains(b, "you ") {
		t.Error("a seat that was never gated grew an operator figure")
	}

	// Answered: the card goes, the stretch is filed on the column, and the
	// vendor's own twelve seconds come back to the header. This is the assertion
	// that keeps the amendment from being a way to hide the split — the figure is
	// deferred while the seat is stopped, never dropped.
	// The columns are copied rather than shared: a State value carries a slice,
	// so writing through the copy would edit the frame `a` was rendered from and
	// leave the assertions above describing a State that no longer exists.
	answered := blocked
	answered.Gates = nil
	answered.Columns = append([]Column(nil), blocked.Columns...)
	answered.Columns[0].GateWait = runner.Span{
		D: 4*time.Minute + 48*time.Second, Measured: true,
	}
	if got := render(answered); !strings.Contains(got, "streaming 12s") {
		t.Errorf("the answered seat does not state the vendor's own time:\n%s", got)
	}
}

// TestEveryGranularityIsExplained mirrors TestEveryBadgeIsExplained, one badge
// column over.
//
// §9.13 gave the sandbox words a legend and left the granularity word beside
// them undefined, which was survivable only because the waiting card was
// reciting the whole explanation in the body of every waiting turn. §9.14 took
// that out of the reading area, which makes the legend owed rather than nice —
// so this fails the build when a Granularity value can render on a column with
// nothing on the help page to say what it means.
func TestEveryGranularityIsExplained(t *testing.T) {
	gloss := helpGranGloss()
	for _, gr := range []Granularity{GranUnknown, GranTokens, GranEvents, GranFinalOnly} {
		if strings.TrimSpace(gloss[gr]) == "" {
			t.Errorf("granularity %v renders %q on a column and is explained nowhere", gr, gr.String())
		}
	}
	// GranUnknown is the one a reader cannot decode by reading the header,
	// because its whole point is that the header says nothing there.
	if !strings.Contains(gloss[GranUnknown], "never been established") {
		t.Errorf("the no-word case does not say why the column is blank: %q", gloss[GranUnknown])
	}

	// And it has to reach the screen, or it is the same failure §9.13 found in
	// SandboxClaim.Detail: written, tested, and rendered by nothing.
	st := room()
	st.Help = HelpPostures
	st.Height = 60 // tall enough to reach below the 24-row fold
	// Flattened, because what is asserted is that the sentence REACHED the
	// screen, not where it happened to break. These glosses are wrapped prose in
	// a panel whose body width is derived (framePad + helpIndent), so pinning a
	// phrase to one rendered line makes this test fail on any change to the
	// panel's margins — which is exactly what it did when the per-seat detail
	// was moved to hang under its own label.
	if !strings.Contains(flatten(render(st)), "sends nothing at all until its turn is done") {
		t.Error("the granularity gloss renders nowhere — the same gap §9.13 found in Detail")
	}
	// A seat whose granularity was never established still gets its sentence.
	st.Columns[0].Gran = GranUnknown
	if !strings.Contains(flatten(render(st)), "never been established") {
		t.Error("a seat with no granularity word is left with nothing to explain the blank")
	}
}

// flatten collapses a rendered frame to one whitespace-normalized line. Only
// safe on full-width surfaces (the help panel), where no two columns sit side by
// side for it to run together. Test-only.
func flatten(frame string) string {
	return strings.Join(strings.Fields(frame), " ")
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
	// Derived, not left zero: the program sets Route on every keystroke, so a
	// golden with an unset Route would pin a frame the room cannot produce.
	st.Route, _ = ParseRoute(st.Draft)
	golden(t, "compose", render(st))
}

// TestEmptyComposeQuietsTheFooter pins the screenshot cut: an empty draft in
// compose mode keeps routing + enter + scroll/tab, and does not spend the mode
// line on ^j/^r or a long placeholder that repeats what routing already says.
func TestEmptyComposeQuietsTheFooter(t *testing.T) {
	st := room()
	st.Mode = ModeComposing
	st.Draft = ""
	got := render(st)

	if !strings.Contains(got, "type a brief") {
		t.Error("empty compose lost the short placeholder")
	}
	if strings.Contains(got, "goes to everyone; @claude") {
		t.Error("empty compose still carries the long placeholder that repeated the routing")
	}
	if !strings.Contains(got, "enter dispatch") {
		t.Error("empty compose dropped enter — the key the mode exists for")
	}
	if !strings.Contains(got, "→ claude") {
		t.Error("empty compose dropped routing")
	}
	if strings.Contains(got, "^j newline") || strings.Contains(got, "^r rebut") {
		t.Error("empty compose still names ^j/^r before there is a draft to extend or rebut with")
	}

	st.Draft = "hello"
	st.Route, _ = ParseRoute(st.Draft)
	full := render(st)
	if !strings.Contains(full, "^j newline") || !strings.Contains(full, "^r rebut") {
		t.Error("a non-empty draft does not restore ^j/^r on the mode line")
	}
	golden(t, "compose-empty", got)
}

// TestBottomAnchorPutsBodyNearComposer pins the ruled tall-window contract:
// when body content is shorter than avail, spare blanks sit between chrome and
// transcript so the last body line is adjacent to the lower rule (composer).
func TestBottomAnchorPutsBodyNearComposer(t *testing.T) {
	st := room()
	st.Width, st.Height = 120, 60
	st.Columns[0].Body = "short reply for bottom-anchor"
	st.Columns[0].Phase = PhaseDone
	rows := strings.Split(render(st), "\n")
	g := GlyphsFor(false)
	bodyStart, bodyEnd := -1, -1
	for i, r := range rows {
		if !frameEdge(r, st.Width, g) {
			continue
		}
		if bodyStart < 0 {
			bodyStart = i + 1
			continue
		}
		bodyEnd = i
		break
	}
	if bodyStart < 0 || bodyEnd <= bodyStart {
		t.Fatalf("could not find body between rules (%d rows)", len(rows))
	}
	body := rows[bodyStart:bodyEnd]
	lastContent := -1
	for i, r := range body {
		if strings.Contains(r, "short reply for bottom-anchor") {
			lastContent = i
		}
	}
	if lastContent < 0 {
		t.Fatal("reply text missing from body")
	}
	// Last content row must be the final body row (adjacent to lower rule /
	// composer), not mid-column under a top-anchored pad.
	if lastContent != len(body)-1 {
		t.Errorf("bottom-anchor failed: reply at body row %d of %d (want last)", lastContent, len(body))
	}
	// Mid-column pad above the reply must be sep-free (no void spears).
	blankPadWithSep := 0
	for _, r := range body[:lastContent] {
		if strings.TrimSpace(r) != "" {
			continue
		}
		if strings.Contains(r, g.Sep) {
			blankPadWithSep++
		}
	}
	if blankPadWithSep != 0 {
		t.Errorf("blank pad rows still carry │ (%d) — void spears returned", blankPadWithSep)
	}
}

// TestRailsStopThroughEmptyBody pins Phase 2: a tall idle frame must not draw
// │ through every empty body row. Content rows keep the separator; the void
// between and below content is gutter-width spaces only.
func TestRailsStopThroughEmptyBody(t *testing.T) {
	st := room()
	st.Width, st.Height = 120, 60
	rows := strings.Split(render(st), "\n")
	g := GlyphsFor(false)
	// Skip header/rules/footer: body is between the two full-width rules.
	// Column chrome also repeats g.Rule under seat names; only a line that is
	// entirely the rule glyph at frame width is a room rule.
	bodyStart, bodyEnd := -1, -1
	for i, r := range rows {
		if !frameEdge(r, st.Width, g) {
			continue
		}
		if bodyStart < 0 {
			bodyStart = i + 1
			continue
		}
		bodyEnd = i
		break
	}
	if bodyStart < 0 || bodyEnd <= bodyStart {
		t.Fatalf("could not find body between rules in a 120x60 idle frame (%d rows)", len(rows))
	}
	withSep, withoutSep := 0, 0
	for _, r := range rows[bodyStart:bodyEnd] {
		if strings.Contains(r, g.Sep) {
			withSep++
		} else {
			withoutSep++
		}
	}
	if withSep == 0 {
		t.Error("no column separators in the occupied region — rails were removed entirely")
	}
	if withoutSep == 0 {
		t.Error("every body row still carries │ — rails were not truncated at content height")
	}
	if withoutSep < withSep {
		t.Errorf("expected most of a tall idle body to be sep-free; with=%d without=%d", withSep, withoutSep)
	}
}

// fullWidthRule is true when a line is one of the room's two frame edges — every
// cell is g.RuleHeavy at st.Width — as opposed to the shorter, lighter rules
// drawn on a seat's header, a turn separator or a card.
//
// It reads the HEAVY glyph since §9.26. That is not a rename: it is the reason
// the heavy weight was worth adding. The frame's edge and a column header's
// leader used to be the same character, so "is this line the frame" was a
// question about width and this helper had to say so out loud; now it is a
// question about which glyph, and the width check is belt and braces.
// frameEdge reports a line that BOUNDS the room's reading area.
//
// It used to be one predicate — a full-bleed heavy rule — because the frame was
// two of them. §9.44 replaced the lower one with the composer's box, so the
// reading area now runs from the header's rule to that box's top border, and a
// test hunting for "the body between the rules" hunts for one of each.
func frameEdge(line string, width int, g Glyphs) bool {
	return fullWidthRule(line, width, g) ||
		strings.HasPrefix(strings.TrimSpace(line), g.BoxTL)
}

func fullWidthRule(line string, width int, g Glyphs) bool {
	rs := []rune(line)
	if len(rs) != width {
		return false
	}
	for _, r := range rs {
		if string(r) != g.RuleHeavy {
			return false
		}
	}
	return true
}

func TestHelp(t *testing.T) {
	st := room()
	st.Help = HelpKeys
	golden(t, "help", render(st))
}

// postureRoom is room() with every seat carrying the claim it REALLY ships,
// read from sandboxFor, rather than the short stand-ins the layout fixture
// uses. The posture page renders that prose verbatim, so a golden built on the
// fixture would review a sentence no user ever sees. Windows, because that is
// the reference machine and the OS whose claims have moved the most — the
// codex seat wore `unsandboxed` here until the 2026-08-29 re-measurement.
func postureRoom() State {
	st := room()
	for i := range st.Columns {
		st.Columns[i].Sandbox = sandboxFor(st.Columns[i].Vendor, true)
	}
	st.Help = HelpPostures
	st.Height = 44
	return st
}

// TestHelpPostures renders page two at a terminal tall enough to reach the
// per-seat half. The golden is deliberately NOT 24 rows: the legend fits the
// minimum room and this room's own measured claims sit below the fold, so a
// 24-row golden would review the half nobody had to write carefully and skip
// the paragraphs that are the whole reason the page exists.
func TestHelpPostures(t *testing.T) {
	golden(t, "help-postures", render(postureRoom()))
}

// TestQuestionMarkCyclesAndAlwaysLeaves. `?` stopped being a toggle when the
// panel grew a second page, and the property that must survive that is the one
// the panel depends on: it is the only documented way out, so no number of
// presses may strand a reader on a page.
func TestQuestionMarkCyclesAndAlwaysLeaves(t *testing.T) {
	h := HelpClosed
	for _, want := range []HelpPage{HelpKeys, HelpPostures, HelpClosed, HelpKeys} {
		if h = h.next(); h != want {
			t.Fatalf("? cycled to %v, want %v", h, want)
		}
	}
}

// TestEveryBadgeIsExplained. A badge word with nothing on the legend page to
// say what it means is the exact state this page was added to fix — a two-word
// safety claim on screen and no reachable explanation of it — and it is the
// state a sixth posture level would silently restore.
func TestEveryBadgeIsExplained(t *testing.T) {
	explained := map[SandboxLevel]bool{}
	for _, e := range helpBadgeGloss() {
		if explained[e.level] {
			t.Errorf("%v is glossed twice", e.level)
		}
		explained[e.level] = true
		if len(e.gloss) == 0 {
			t.Errorf("%v has an empty gloss", e.level)
		}
	}
	for l := SandboxUnknown; l <= SandboxGated; l++ {
		b := SandboxClaim{Level: l}.Badge()
		if b == "" {
			// SandboxUnknown renders no badge, so there is nothing to explain.
			continue
		}
		if !explained[l] {
			t.Errorf("the badge %q renders on a column and the help page does not "+
				"say what it means", b)
		}
	}
}

// TestThePostureLegendDoesNotSoftenAnyClaim. This page is a gloss on the badge
// words, never a replacement for them (ADR-008 §3 and its amendments). The two
// words that mean "this seat can change your files" have to keep meaning that
// in plain English, and the weakest badge has to keep admitting it is weak —
// a legend that made either sit more comfortably would be the blanket claim
// this whole vocabulary exists to refuse, re-entering through the help panel.
func TestThePostureLegendDoesNotSoftenAnyClaim(t *testing.T) {
	byLevel := map[SandboxLevel]string{}
	for _, e := range helpBadgeGloss() {
		byLevel[e.level] = strings.ToLower(strings.Join(e.gloss, " "))
	}

	for _, tc := range []struct {
		level SandboxLevel
		want  []string
	}{
		{SandboxNone, []string{"nothing restricts", "measured", "change your files"}},
		{SandboxRequested, []string{"never observed"}},
		{SandboxTools, []string{"absent"}},
		{SandboxWrite, []string{"edit and run"}},
		{SandboxGated, []string{"asks first"}},
	} {
		for _, w := range tc.want {
			if !strings.Contains(byLevel[tc.level], strings.ToLower(w)) {
				t.Errorf("the gloss for %q dropped %q: %q",
					SandboxClaim{Level: tc.level}.Badge(), w, byLevel[tc.level])
			}
		}
	}

	// No gloss may claim a posture is safe, restricted or read-only. The badges
	// break the `ro:` prefix on purpose; a legend that put the word back would
	// undo that in the one place a reader goes to have it explained.
	for l, g := range byLevel {
		if l == SandboxNone || l == SandboxWrite || l == SandboxGated {
			for _, forbidden := range []string{"read-only", "safe", "cannot write"} {
				if strings.Contains(g, forbidden) {
					t.Errorf("the gloss for %q says %q: %q",
						SandboxClaim{Level: l}.Badge(), forbidden, g)
				}
			}
		}
	}
}

// TestThePosturePageRendersEachSeatsOwnClaim. SandboxClaim.Detail was written,
// tested and quoted into ADR-008 for several amendments while rendering
// NOWHERE — the field's own comment said it was "shown in the degraded/help
// text" and no surface read it. §9.2's rule is that a claim you cannot see is
// not a claim, and the argument behind a claim is under the same rule.
func TestThePosturePageRendersEachSeatsOwnClaim(t *testing.T) {
	st := postureRoom()
	// Taller than the golden's 44 rows, and the reason is an accident this
	// test used to pass on. The page does not scroll: what does not fit is
	// cut behind `↓ N more below`, and at 44 rows the third seat's paragraph
	// sits below the fold. Until 2026-09-02 the assertion still held because
	// the LEGEND repeats Antigravity's old opening clause ("treat this column
	// as able to change your files"), so the seat's own claim was never on
	// screen and the test could not tell. The claim now opens with the
	// seat's shape words (§9.54), which the legend does not carry, and the
	// property under test — the field REACHES the screen — is asserted on a
	// terminal tall enough to hold all of it.
	st.Height = 80
	got := render(st)

	for _, c := range st.Columns {
		// The detail is wrapped, so the whole sentence is not on one line. Its
		// opening clause is enough to prove the field reached the screen.
		head := strings.SplitN(c.Sandbox.Detail, " ", 5)
		if len(head) < 5 {
			t.Fatalf("%s has a suspiciously short posture detail: %q", c.Label, c.Sandbox.Detail)
		}
		if !strings.Contains(got, strings.Join(head[:4], " ")) {
			t.Errorf("%s's own posture claim is not on the page:\n%s", c.Label, got)
		}
	}
}

// TestHelpFitsTheSmallestRoom is the panel's hard budget, asserted rather than
// counted by hand.
//
// The `?` line is the only documented way back OUT of this panel and `q` is the
// only way out of the room, so both have to survive the shortest terminal the
// room will draw in at all. That is not the 120x24 the golden renders: the
// collapsed-seat notice costs a row and the narrow tier's tab bar costs another,
// and a machine with a seat that will not run is the ordinary machine here —
// Cursor, permanently, on the reference box. Every geometry below was a real
// room in which the last two lines of the help had silently fallen off the
// bottom, which is how a panel that promises to list the keys ends up hiding
// the two that matter most.
func TestHelpFitsTheSmallestRoom(t *testing.T) {
	// A fourth seat that is not installed: it collapses out of the grid, which
	// is what puts the notice row on screen. This is the reference machine's
	// actual room.
	withDeadSeat := func() State {
		st := room()
		st.Columns = append(st.Columns, Column{
			Vendor: model.VendorCursor, Label: "Cursor",
			Avail: AvailNotInstalled, Note: "not found on PATH",
		})
		return st
	}

	for _, tc := range []struct {
		name       string
		st         func() State
		w          int
		wantNotice bool
		wantTabs   bool
	}{
		{name: "wide", st: room, w: 120},
		{name: "wide+notice", st: withDeadSeat, w: 120, wantNotice: true},
		{name: "tabs+notice", st: withDeadSeat, w: 80, wantNotice: true, wantTabs: true},
	} {
		st := tc.st()
		st.Width, st.Height = tc.w, MinHeight+14 // 24 rows
		// Assert the fixture really produces the geometry it is named for, so a
		// future change that stops collapsing seats cannot turn this into three
		// copies of the easy case.
		if got := collapsedNotice(st, GlyphsFor(false)) != ""; got != tc.wantNotice {
			t.Fatalf("%s: notice row %v, want %v", tc.name, got, tc.wantNotice)
		}
		if got := layoutFor(st, GlyphsFor(false)).Tabs; got != tc.wantTabs {
			t.Fatalf("%s: tab bar %v, want %v", tc.name, got, tc.wantTabs)
		}

		// BOTH pages spend the same budget and both have to survive it. The
		// second page is the one carrying a safety vocabulary, so a page whose
		// closing lines fell off the bottom would leave a reader looking at
		// "unsandboxed" with the sentence about what actually contains the room
		// scrolled away — and no visible way back to the keys.
		for _, pg := range []struct {
			page HelpPage
			want []string
		}{
			{HelpKeys, []string{"ctrl+c / q", "?            next page"}},
			{HelpPostures, []string{"unsandboxed", "WORKSPACE above", "?            close"}},
		} {
			st.Help = pg.page
			got := render(st)
			for _, want := range pg.want {
				if !strings.Contains(got, want) {
					t.Errorf("%s (%dx%d) page %v: the help panel dropped %q\n%s",
						tc.name, st.Width, st.Height, pg.page, want, got)
				}
			}
		}
	}
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
		"help":          func() State { st := room(); st.Help = HelpKeys; return st },
		"help-postures": func() State { st := room(); st.Help = HelpPostures; return st },
		// A multi-row composer over a room that is also mid-transcript: the two
		// new variable-height surfaces competing for the same frame.
		"talking": func() State {
			st := talking()
			st.Mode = ModeComposing
			st.Draft = "and\nnow\na\nfollow-up\nspanning\nseveral rows"
			return st
		},
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
		// The trace is the newest way to overflow a column: an entry carries a
		// clipped-but-still-long command, an outcome mark appended after it,
		// and an indented failure detail that is itself wrapped. The mark is
		// what makes this worth its own case — it is added AFTER the text, so
		// an entry that exactly filled the column would spill by two cells.
		"trace": func() State {
			st := room()
			st.Turn = 1
			st.Columns[0].Phase = PhaseDone
			st.Columns[0].Acts = []Act{
				{Text: "Bash: " + strings.Repeat("go test ./internal/council ", 4), Status: runner.ActOK},
				{
					Text:   "Bash: " + strings.Repeat("verylongtokenwithnobreaks", 5),
					Status: runner.ActFailed,
					Detail: strings.Repeat("unbreakabletokenofdoom", 6),
				},
				{Text: "tool", Status: runner.ActUnknown},
				{Text: strings.Repeat("pending", 12)},
			}
			st.Columns[0].Body = "Summary."
			return st
		},
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
	states := map[string]func() State{
		"empty": room,
		// The two surfaces that can now take rows away from the body. Both have
		// to lose that argument before the frame does.
		"composing": func() State {
			st := room()
			st.Mode = ModeComposing
			st.Draft = "one\ntwo\nthree\nfour\nfive\nsix\nseven"
			return st
		},
		"collapsed": deadSeats,
	}
	for name, mk := range states {
		for _, h := range []int{10, 12, 16, 24, 40} {
			for _, w := range []int{60, 80, 120} {
				st := mk()
				st.Width, st.Height = w, h
				n := len(strings.Split(Render(st, PlainStyles(), GlyphsFor(false)), "\n"))
				if n > h {
					t.Errorf("%s w=%d h=%d: frame is %d lines, terminal is %d", name, w, h, n, h)
				}
			}
		}
	}
}

// TestSettlingSeatSaysItIsStillExiting.
//
// A seat that has answered but whose process has not exited renders `done` — and
// has to say the second half out loud. Between those two moments (4.06s and
// 4.25s measured on codex-cli 0.147.0, 7.94s in §9.33) the room has no spinner
// and every column reads finished, while the composer is still locked and `q` is
// still refused. Without a word there, the room looks wedged.
//
// Asserted on the WORD, in both glyph sets: this is the signal that has to
// survive --ascii and NO_COLOR, because a reader on either is exactly the reader
// who cannot tell a quiet room from a stuck one (§7.1 rule 2).
func TestSettlingSeatSaysItIsStillExiting(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		g := GlyphsFor(ascii)
		c := Column{
			Vendor: model.VendorCodex, Label: "Codex", Avail: AvailInstalled,
			Phase: PhaseDone, Elapsed: 4 * time.Second, Settling: true,
		}
		head, clock, tail := columnStatus(State{}, c, g)
		got := head + clock + tail
		if !strings.Contains(got, "done") {
			t.Errorf("ascii=%v: status = %q, want the settled phase word", ascii, got)
		}
		if !strings.Contains(got, "exiting") {
			t.Errorf("ascii=%v: status = %q — a settling seat does not say its process is still going", ascii, got)
		}
		// After the clock, never inside it: the figure is the time to the ANSWER,
		// and a reader must not take the linger for part of it.
		if strings.Index(got, "exiting") < strings.Index(got, "4s") {
			t.Errorf("ascii=%v: status = %q — the linger word cut in front of the turn's own figure", ascii, got)
		}

		c.Settling = false
		qh, qc, qt := columnStatus(State{}, c, g)
		if quiet := qh + qc + qt; strings.Contains(quiet, "exiting") {
			t.Errorf("ascii=%v: a retired seat still claims to be exiting: %q", ascii, quiet)
		}
	}
}

// TestSettlingKeepsTheFooterOfferingCancel.
//
// The footer chooses `ctrl+c cancel` or `q quit`, and it used to ask Busy().
// Busy() is derived from column phases, so a seat that settles ahead of its
// process takes it false while the turn is still live — and `q` is refused
// outright while a turn is live. A footer naming a key that answers with a
// notice is §7.8's surprise, so the test is on the predicate the footer actually
// reads.
func TestSettlingKeepsTheFooterOfferingCancel(t *testing.T) {
	st := State{Columns: []Column{{
		Vendor: model.VendorCodex, Avail: AvailInstalled,
		Phase: PhaseDone, Settling: true,
	}}}
	if st.Busy() {
		t.Error("Busy() is true for a seat that has answered; the spinner would run over a finished column")
	}
	if !st.Settling() {
		t.Error("Settling() missed a settling column")
	}
	if !st.InFlight() {
		t.Error("InFlight() went false while a process was still alive — the footer would offer `q`")
	}

	st.Columns[0].Settling = false
	if st.InFlight() {
		t.Error("InFlight() stayed true after every seat retired")
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

// TestArenaDiffColors is TestPhaseColors' shape pointed at the patch view:
// one existing token per line class, asserted with the coloured set, while the
// plain set renders the exact bytes the goldens already pin. The header cases
// are the ones that earn the test — `+++` and `---` open with the change
// markers' own prefixes, and a classifier that read them second would paint a
// file header as an edit.
func TestArenaDiffColors(t *testing.T) {
	sty := NewStyles(true)
	cases := []struct {
		line  string
		style lipgloss.Style
		what  string
	}{
		{"+inserted line", sty.SevOK, "an added line"},
		{"-deleted line", sty.SevCrit, "a removed line"},
		{"@@ -1,4 +1,6 @@", sty.Muted, "a hunk header"},
		{"diff --git a/x.go b/x.go", sty.Muted, "a file header"},
		{"index 0123abc..456def0 100644", sty.Muted, "an index header"},
		{"+++ b/x.go", sty.Muted, "a +++ file header"},
		{"--- a/x.go", sty.Muted, "a --- file header"},
		{" unchanged context", sty.Text, "a context line"},
	}
	for _, c := range cases {
		got := sty.ForDiffLine(c.line).Render(c.line)
		want := c.style.Render(c.line)
		if got != want {
			t.Errorf("%s renders %q, want %q", c.what, got, want)
		}
	}
	// The header-before-marker rule, stated as its own assertion: a `+++`
	// header wearing the addition's green is the patch claiming an edit it
	// never made, and likewise `---` and the removal's red.
	if sty.ForDiffLine("+++ b/x.go").Render("x") == sty.SevOK.Render("x") {
		t.Error("a +++ file header is styled as an addition")
	}
	if sty.ForDiffLine("--- a/x.go").Render("x") == sty.SevCrit.Render("x") {
		t.Error("a --- file header is styled as a removal")
	}

	// The render path actually spends the tokens: the patch view assembled
	// with the coloured set carries each class's escape on the whole line.
	st := room()
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Arena = &ArenaResult{
		Stat: " x.go | 2 +-",
		Diff: "diff --git a/x.go b/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n ctx\n",
	}
	st.Columns[0].ArenaShowDiff = true
	lines, _ := columnLines(st, st.Columns[0], 38, sty, GlyphsFor(false))
	joined := strings.Join(lines, "\n")
	for _, want := range []struct{ styled, what string }{
		{sty.SevOK.Render("+new"), "the added line"},
		{sty.SevCrit.Render("-old"), "the removed line"},
		{sty.Muted.Render("@@ -1 +1 @@"), "the hunk header"},
		{sty.Muted.Render("+++ b/x.go"), "the +++ header"},
	} {
		if !strings.Contains(joined, want.styled) {
			t.Errorf("%s does not carry its token's escape in the rendered column", want.what)
		}
	}
	if strings.Contains(joined, sty.SevOK.Render("+++ b/x.go")) {
		t.Error("the rendered column styles a +++ header as an addition")
	}

	// Colour stayed a second signal: PlainStyles renders every class as its
	// own bytes — the same property the untouched goldens assert frame-wide —
	// and that identity is also the whole of the --ascii and NO_COLOR story,
	// because those paths neutralize the style set and never see an escape.
	plain := PlainStyles()
	for _, c := range cases {
		if got := plain.ForDiffLine(c.line).Render(c.line); got != c.line {
			t.Errorf("PlainStyles alters %s: %q", c.what, got)
		}
	}
	pl, _ := columnLines(st, st.Columns[0], 38, plain, GlyphsFor(false))
	pj := strings.Join(pl, "\n")
	if strings.Contains(pj, "\x1b[") {
		t.Error("PlainStyles patch view emits an ANSI escape; goldens would see colour")
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
	// The pair dispatch() writes together: the note, and the flag saying it is
	// about a turn this seat sat out rather than about the turn it last took.
	st.Columns[1].TurnN = 1
	st.Columns[1].Note = "not addressed in turn 2"
	st.Columns[1].Skipped = true
	st.Columns[2].Phase = PhaseWaiting

	got := render(st)
	// The COLUMN no longer says it and the ROOM line does, once, naming the seat
	// (from the LEDGER lane, roomline.go). One dispatch described three times was
	// the finding; a room of four idle seats printed the same sentence four times
	// about one turn.
	if strings.Contains(got, "not addressed in turn 2") {
		t.Error("a column still prints a fact about the whole dispatch")
	}
	if !strings.Contains(got, "sat turn 2 out: "+st.Columns[1].Label) {
		t.Errorf("the room line does not name the seat the turn left out\n%s", got)
	}
	golden(t, "unaddressed-column", got)
}

// longBody is a reply too tall for any column at the test geometry.
func longBody(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = "line " + strconv.Itoa(i+1) + " of the reply"
	}
	return strings.Join(lines, "\n")
}

// TestOverflowAnnouncesItself is the point of this whole feature. Before it, a
// reply taller than the column was silently truncated — indistinguishable from
// a vendor that simply stopped talking, which is the exact ambiguity §4a.1
// forbids. Scrolling is the affordance; SAYING there is more is the honesty.
func TestOverflowAnnouncesItself(t *testing.T) {
	st := room()
	st.Turn = 1
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Body = longBody(60)
	st.Columns[0].Follow = false
	st.Columns[0].Scroll = 0

	got := render(st)
	if !strings.Contains(got, "more below") {
		t.Error("a clipped reply does not say there is more below it")
	}
	golden(t, "scroll-top", got)

	// Scrolled into the middle: both directions must be announced.
	st.Columns[0].Scroll = 20
	mid := render(st)
	if !strings.Contains(mid, "more above") || !strings.Contains(mid, "more below") {
		t.Error("a mid-scroll column does not announce both directions")
	}
	golden(t, "scroll-middle", mid)
}

// TestTheOverflowMarkerNamesItsKeys. The count said content was hidden and
// nothing about how to reach it, which is how a room with scrollback, page keys
// and a full-width expand got reported as having no way to scroll.
func TestTheOverflowMarkerNamesItsKeys(t *testing.T) {
	st := room()
	st.Turn = 1
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Body = longBody(60)
	st.Columns[0].Follow = false
	st.Columns[0].Scroll = 20
	st.Columns[1].Phase = PhaseDone
	st.Columns[1].Body = longBody(60)
	st.Columns[1].Follow = false
	st.Columns[1].Scroll = 20

	got := render(st)
	// Exactly once, and matched joined to the count so the mode line's own copy
	// of the same keys is not what satisfies this. The keys address the focused
	// column, so a hint on the seat beside it would be a claim about a column
	// those keys do not move — and twice within one column (above AND below) is
	// noise on top of the number the marker exists to carry.
	if n := strings.Count(got, "more above  │  ↑↓ scroll"); n != 1 {
		t.Errorf("the hinted marker appears %d times, want once — on the focused column's first marker\n%s", n, got)
	}
	if strings.Contains(got, "more below  │  ↑↓ scroll") {
		t.Errorf("the hint is repeated on the same column's second marker\n%s", got)
	}
	// The count is never traded for the hint.
	if !strings.Contains(got, "more above") || !strings.Contains(got, "more below") {
		t.Error("the hint displaced the overflow count")
	}

	// In compose mode the hint drops `f`, which is the letter f there.
	st.Mode = ModeComposing
	if strings.Contains(render(st), "f expand") {
		t.Error("the overflow marker advertises f while composing, where f is text")
	}

	// A column too narrow for both keeps the count and drops the hint entirely.
	narrow := st
	narrow.Mode = ModeViewing
	narrow.Width = 96
	if n := strings.Count(render(narrow), "more above"); n == 0 {
		t.Error("the narrow room lost its overflow count")
	}
}

// TestAnUnfocusedColumnNamesTheKeyThatReachesIt is the second scroll report, and
// it is not the same bug as §9.10's.
//
//	"scrolling works for your window. i tried scrolling up/down in agy and
//	cursor. could not."
//
// Both halves are true. The keys move the focused column, they always did, and
// three columns with content hidden each said "↑ 36 more above" in exactly the
// same words — one of them naming a key and two of them naming nothing. Pressing
// ↑ while looking at the third seat moves the first, which from where the user
// is sitting is a scroll key that does not work.
//
// So the marker on a column the keys do NOT move names the key that would move
// them there. Same rule as the focused column's hint — a marker states the key
// for THIS column and never a neighbour's — applied to the case left blank.
func TestAnUnfocusedColumnNamesTheKeyThatReachesIt(t *testing.T) {
	st := room()
	st.Turn = 1
	for i := range st.Columns {
		st.Columns[i].Phase = PhaseDone
		st.Columns[i].Body = longBody(60)
		st.Columns[i].Follow = false
		st.Columns[i].Scroll = 20
	}

	got := render(st)
	// The frame this concern exists to produce, pinned whole: three columns each
	// holding something back, one of them saying how to read it and two saying
	// how to get there.
	golden(t, "scroll-unfocused", got)
	// One focused column, so one scroll hint and two tab hints — never the
	// scroll keys on a seat they do not reach.
	if n := strings.Count(got, "more above  "+UnicodeGlyphs().Sep+"  "+UnicodeGlyphs().Up+UnicodeGlyphs().Down+" scroll"); n != 1 {
		t.Errorf("the scroll hint appears %d times, want once — on the focused column\n%s", n, got)
	}
	if n := strings.Count(got, "more above  "+UnicodeGlyphs().Sep+"  tab to focus"); n != 2 {
		t.Errorf("the tab hint appears %d times, want one per unfocused column\n%s", n, got)
	}
	// The count still outranks the hint, in both forms.
	if n := strings.Count(got, "more above"); n != 3 {
		t.Errorf("a column traded its overflow count for a hint: %d counts on screen\n%s", n, got)
	}

	// It is honest in compose too, which is why it needs no mode-awareness: tab
	// moves focus there as well (§9.10), unlike `f`.
	st.Mode = ModeComposing
	if !strings.Contains(render(st), "tab to focus") {
		t.Error("the tab hint vanished in compose mode, where tab still moves focus")
	}

	// A room with one seat on screen has nothing to tab to, and says nothing.
	one := deadSeats()
	one.Turn = 1
	one.Columns[0].Phase = PhaseDone
	one.Columns[0].Body = longBody(60)
	one.Columns[0].Follow = false
	one.Columns[0].Scroll = 20
	if strings.Contains(render(one), "tab to focus") {
		t.Error("a one-seat room offers tab, which reaches nothing there")
	}
}

// TestFollowShowsTheTail: a streaming column pins to the newest output, so the
// interesting line during a turn is the one arriving.
func TestFollowShowsTheTail(t *testing.T) {
	st := room()
	st.Turn = 1
	st.Columns[0].Phase = PhaseStreaming
	st.Columns[0].Body = longBody(60)
	st.Columns[0].Follow = true

	got := render(st)
	if !strings.Contains(got, "line 60 of the reply") {
		t.Error("a following column is not showing the newest line")
	}
	if strings.Contains(got, "more below") {
		t.Error("a following column claims there is content below the tail")
	}
	if !strings.Contains(got, "more above") {
		t.Error("a following column does not say it has scrolled past earlier content")
	}
}

// TestShortRepliesGetNoScrollFurniture: the markers cost a line each, so they
// must only appear when they are true.
func TestShortRepliesGetNoScrollFurniture(t *testing.T) {
	st := room()
	st.Turn = 1
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Body = "short answer"
	got := render(st)
	if strings.Contains(got, "more above") || strings.Contains(got, "more below") {
		t.Error("a reply that fits still drew scroll markers")
	}
}

// TestExpandedGivesOneColumnTheWholeWidth. Three columns compare at a glance;
// one column is for reading.
func TestExpandedGivesOneColumnTheWholeWidth(t *testing.T) {
	st := room()
	st.Turn = 1
	st.Expanded = true
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Body = "Resume beats re-sending the transcript, because input grows quadratically against metered quotas and the blind-round guarantee stops being structural."

	got := render(st)
	if strings.Contains(got, "Antigravity  ") && strings.Count(got, "ro:tools") > 1 {
		t.Error("expanded mode still drew several columns")
	}
	golden(t, "expanded", got)

	// Expansion outranks width: even a wide terminal shows one column.
	if lay := resolveLayout(200, 40, 3, true); lay.Tier != TierTabs || lay.Cols != 1 {
		t.Errorf("expanded at width 200 resolved to %v with %d cols, want one column", lay.Tier, lay.Cols)
	}
}

// TestScrollNeverExceedsTheTerminal re-runs the width sweep with tall content
// and a scrolled column, because the markers are new lines assembled at render
// time and are exactly the kind of thing that overflows a narrow column.
func TestScrollNeverExceedsTheTerminal(t *testing.T) {
	for _, w := range []int{60, 72, 96, 120, 200} {
		for _, ascii := range []bool{false, true} {
			for _, expanded := range []bool{false, true} {
				st := room()
				st.Width, st.Height = w, 24
				st.Expanded = expanded
				st.Turn = 1
				for i := range st.Columns {
					st.Columns[i].Phase = PhaseDone
					st.Columns[i].Body = longBody(80)
					st.Columns[i].Follow = false
					st.Columns[i].Scroll = 15
				}
				out := Render(st, PlainStyles(), GlyphsFor(ascii))
				for i, line := range strings.Split(out, "\n") {
					if n := lipgloss.Width(line); n > w {
						t.Errorf("w=%d ascii=%v expanded=%v: line %d is %d cells",
							w, ascii, expanded, i, n)
					}
				}
			}
		}
	}
}

// TestWaitingColumnShowsItsClock is the answer to "why is that one taking so
// long". Two of the three vendors are final-only, so a blank column plus a
// spinner is the COMMON case, and without a clock it reads as broken rather
// than slow.
func TestWaitingColumnShowsItsClock(t *testing.T) {
	base := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	st := room()
	st.Turn = 1
	st.Now = base.Add(73 * time.Second)
	st.Columns[2].Phase = PhaseWaiting
	st.Columns[2].Started = base

	// 73 seconds reads as 1m13s. Past a minute, minutes are what a person
	// actually wants — "73s" makes you do arithmetic to answer "is this stuck".
	got := render(st)
	if !strings.Contains(got, "1m13s") {
		t.Error("a waiting column does not say how long it has been waiting")
	}
	golden(t, "waiting-clock", got)
}

// TestFinishedColumnKeepsItsTime: the asymmetry between a streaming vendor and
// a final-only one is only legible if the finished column still says what it
// cost in wall time.
func TestFinishedColumnKeepsItsTime(t *testing.T) {
	st := room()
	st.Turn = 1
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Body = "OK"
	st.Columns[0].Elapsed = 4 * time.Second
	st.Columns[1].Phase = PhaseDone
	st.Columns[1].Body = "OK"
	st.Columns[1].Elapsed = 96 * time.Second

	got := render(st)
	if !strings.Contains(got, "4s") {
		t.Error("the fast column lost its timing")
	}
	if !strings.Contains(got, "1m36s") {
		t.Error("the slow column lost its timing, or does not render minutes")
	}
}

// TestIdleColumnsHaveNoClock: a column that never ran has no duration, and
// rendering "0s" would be a measurement it never made.
func TestIdleColumnsHaveNoClock(t *testing.T) {
	if got := render(room()); strings.Contains(got, "0s") {
		t.Error("an idle column rendered a duration it never measured")
	}
}

// TestElapsedIsPureOverState guards the contract the goldens rest on: the
// duration comes from State.Now, never from the clock, so two renders of one
// State are identical no matter how much time passes between them.
func TestElapsedIsPureOverState(t *testing.T) {
	st := room()
	st.Turn = 1
	st.Now = time.Date(2026, 8, 4, 12, 1, 0, 0, time.UTC)
	st.Columns[0].Phase = PhaseStreaming
	st.Columns[0].Started = st.Now.Add(-30 * time.Second)

	first := render(st)
	time.Sleep(15 * time.Millisecond)
	if render(st) != first {
		t.Fatal("Render read a clock; the elapsed counter must come from State.Now")
	}
	if !strings.Contains(first, "30s") {
		t.Errorf("elapsed not derived from State.Now")
	}
}

// TestWriteModeIsLoudAndPersistent. Widening what three agents may do to a
// working tree is session state, so its marker is chrome rather than a notice:
// a notice scrolls away and a badge can be missed while reading a column.
func TestWriteModeIsLoudAndPersistent(t *testing.T) {
	st := room()
	st.Write = true
	for i := range st.Columns {
		st.Columns[i].Sandbox = SandboxClaim{Level: SandboxWrite, Detail: "started with --write"}
	}

	got := render(st)
	if !strings.Contains(got, "WRITE") {
		t.Error("write mode is not marked in the header")
	}
	if strings.Contains(got, "ro:") {
		t.Error("a write-mode room still advertises a read-only posture somewhere")
	}
	if strings.Count(got, "WRITES") != 3 {
		t.Error("not every column carries the write badge; grading them would imply a safety difference that does not exist")
	}
	golden(t, "write-mode", got)
}

// TestReadModeSaysNothingAboutWriting is the other direction: the loud marker
// must not leak into a room that cannot write.
//
// It also pins that such a room NAMES itself. Write is the default now, so read
// is the exception, and an unmarked header would leave the reader deciding
// between "this room cannot write" and "the badge did not render" — the same
// reason "no brief" is spelled out a few lines from it in the header.
func TestReadModeSaysNothingAboutWriting(t *testing.T) {
	got := render(room())
	if strings.Contains(got, "WRITE") {
		t.Error("a read room claims write mode")
	}
	if !strings.Contains(got, "READ") {
		t.Error("a read room does not say so; absence of a badge is not a claim")
	}
}

// TestFlowHopIsNamedWhileAChainDrivesTheRoom.
//
// A chain dispatches its own next hop, which makes it the only thing here that
// sends a brief the user did not type to a seat the user did not pick. With
// three of four columns idle, that is indistinguishable from the room acting on
// its own unless the header says whose turn it is and how far along.
func TestFlowHopIsNamedWhileAChainDrivesTheRoom(t *testing.T) {
	st := room()
	st.Turn = 3
	st.FlowHop, st.FlowSteps = 2, 4
	st.FlowVendor = st.Columns[0].Vendor

	got := render(st)
	if !strings.Contains(got, "hop 2/4") {
		t.Error("the header does not say which hop the chain is on")
	}
	if !strings.Contains(got, "@"+string(st.Columns[0].Vendor)) {
		t.Error("the header does not name the seat holding the hop")
	}

	// And absent for an ordinary turn: a marker that outlived its chain would
	// report an orchestration over a room that is merely waiting.
	plain := room()
	plain.Turn = 3
	if strings.Contains(render(plain), "hop ") {
		t.Error("a room with no chain reports a hop")
	}
}

// activityRoom is the room the `activity` golden pins: one seat that has
// finished a turn with a trace behind it, two that have not been asked
// anything. It deliberately sits on room()'s three-column fixture so the
// activity golden does not cascade every time the public hero adds a seat.
//
// The public pictures use heroRoom() instead — same activity story, five seats.
func activityRoom() State {
	st := room()
	st.Turn = 1
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Acts = []Act{
		{Text: "Glob"}, {Text: "Read"}, {Text: "Bash: go test ./..."},
	}
	st.Columns[0].Body = "Tests pass."
	return st
}

// heroRoom is the five-seat room the public council pictures pin.
//
// room() stays three columns on purpose: hundreds of goldens and layout
// assertions are built on that fixture, and expanding it would re-pin the
// whole suite for a change that only the public hero needs. This fixture is
// the dedicated source for images/telltale-council-{dark,light}.svg — one State,
// one Render, blank scrollback dropped in heroFrame, dual dark/light SVGs.
//
// Width is 160 so five primary columns stay above minColumn (24): at 120 the
// layout drops five seats to tabs, which is not the product picture. Seating
// order matches addressableVendors. Exactly one focus mark (Claude, Focus 0).
// Sandbox and granularity claims mirror the measured per-vendor postures the
// live room draws — no invented cost, no invented context.
func heroRoom() State {
	st := NewState()
	st.Width, st.Height = 160, 24
	st.Workspace = "/home/dev/code/telltale"
	st.Home = "/home/dev"
	st.Mode = ModeViewing
	st.Turn = 1
	st.Focus = 0
	st.Columns = []Column{
		{
			Vendor: model.VendorClaude, Label: "Claude Code",
			Avail:   AvailInstalled,
			Sandbox: SandboxClaim{Level: SandboxTools, Detail: "tool allowlist"},
			Gran:    GranTokens, Phase: PhaseDone,
			Acts: []Act{
				{Text: "Glob"}, {Text: "Read"}, {Text: "Bash: go test ./..."},
			},
			Body: "Tests pass.",
		},
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
		{
			Vendor: model.VendorCursor, Label: "Cursor",
			Avail:   AvailInstalled,
			Sandbox: SandboxClaim{Level: SandboxRequested, Detail: "ACP plan mode, one trial held"},
			Gran:    GranTokens, Phase: PhaseIdle,
		},
		{
			Vendor: model.VendorGrok, Label: "Grok",
			Avail:   AvailInstalled,
			Sandbox: SandboxClaim{Level: SandboxNone, Detail: "measured not to restrict writes"},
			Gran:    GranTokens, Phase: PhaseIdle,
		},
	}
	return st
}

// TestActivityTraceIsNotProse is the core distinction this feature rests on.
// Body is what a vendor SAID; Acts is what it DID. Concatenating them would let
// a tool name read as part of an answer — the same category error as rendering
// a quoted reply as the vendor's own words.
func TestActivityTraceIsNotProse(t *testing.T) {
	st := activityRoom()

	got := render(st)
	for _, want := range []string{"Glob", "Read", "go test", "Tests pass."} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	// The marker is a glyph, not colour: this product's rule is that every
	// distinction survives --ascii and a monochrome terminal.
	if !strings.Contains(got, "⚙ Glob") {
		t.Error("activity is not visually marked as activity")
	}
	if strings.Contains(got, "GlobTests pass.") || strings.Contains(got, "Glob Tests pass.") {
		t.Error("activity ran together with prose")
	}
	golden(t, "activity", got)
}

// TestActingColumnDoesNotClaimSilence: the waiting card says "no incremental
// output", which would flatly contradict a trace rendered directly above it.
func TestActingColumnDoesNotClaimSilence(t *testing.T) {
	st := room()
	st.Turn = 1
	st.Columns[2].Phase = PhaseWaiting
	st.Columns[2].Acts = []Act{{Text: "list_dir: C:\\ws"}, {Text: "run_command: go test ./..."}}

	got := render(st)
	if strings.Contains(got, "the reply arrives whole") {
		t.Error("a column showing a live trace still claims nothing arrives before the end")
	}
	if !strings.Contains(got, "list_dir") {
		t.Error("the trace is missing from a waiting column — the case it matters most for")
	}
}

// TestASCIIActivityHasItsOwnMarker guards the fallback: "*" rather than a
// glyph a legacy console renders as a tofu box.
func TestASCIIActivityHasItsOwnMarker(t *testing.T) {
	st := room()
	st.Turn = 1
	st.Columns[0].Phase = PhaseStreaming
	st.Columns[0].Acts = []Act{{Text: "Glob"}}
	got := Render(st, PlainStyles(), GlyphsFor(true))
	if !strings.Contains(got, "* Glob") {
		t.Error("ascii mode lost the activity marker")
	}
}

// TestActOutcomesRenderDistinctly is the honest-gauge rule on the trace itself.
//
// Four states, four different renders. The one that matters most is Unknown
// against OK: "the vendor said this step ended" and "the vendor said this step
// worked" are different facts, and if they ever render alike the room is
// claiming a result it does not have — which for Antigravity is EVERY step, not
// an edge case.
func TestActOutcomesRenderDistinctly(t *testing.T) {
	mk := func(s runner.ActStatus) string {
		st := room()
		st.Turn = 1
		st.Expanded = true // one column at full width, so nothing wraps
		st.Columns[0].Phase = PhaseDone
		st.Columns[0].Acts = []Act{{Text: "Bash: go test", Status: s}}
		st.Columns[0].Body = "Done."
		return render(st)
	}

	seen := map[string]runner.ActStatus{}
	for _, s := range []runner.ActStatus{
		runner.ActPending, runner.ActOK, runner.ActFailed, runner.ActUnknown,
	} {
		got := mk(s)
		if prev, dup := seen[got]; dup {
			t.Fatalf("status %v renders identically to %v", s, prev)
		}
		seen[got] = s
	}

	// And the marks are the ones the glyph set names, positioned after the
	// command rather than in front of it.
	g := UnicodeGlyphs()
	if !strings.Contains(mk(runner.ActOK), "Bash: go test "+g.ActOK) {
		t.Error("a successful call is not marked with the OK glyph")
	}
	if !strings.Contains(mk(runner.ActFailed), "Bash: go test "+g.ActFail) {
		t.Error("a failed call is not marked with the failure glyph")
	}
	if !strings.Contains(mk(runner.ActUnknown), "Bash: go test "+g.ActUnknown) {
		t.Error("an unresolved-outcome call is not marked as unknown")
	}
	// Pending renders BARE. A mark for "nothing is known yet" would be a claim.
	pending := mk(runner.ActPending)
	for _, mark := range []string{g.ActOK, g.ActFail, g.ActUnknown} {
		if strings.Contains(pending, "Bash: go test "+mark) {
			t.Errorf("a call still in flight was marked %q", mark)
		}
	}
}

// TestActOutcomeMarksSurviveASCII: every distinction in this product has to
// survive --ascii and a monochrome terminal, so the marks are glyphs first and
// colour second. The ASCII set also has to dodge every character already spoken
// for — "*" is Act, ">" the ellipsis, "]" focus, "#" the HUD's gauge fill.
func TestActOutcomeMarksSurviveASCII(t *testing.T) {
	st := room()
	st.Turn = 1
	st.ASCII = true
	st.Expanded = true
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Acts = []Act{
		{Text: "Bash: go build", Status: runner.ActOK},
		{Text: "Bash: go test", Status: runner.ActFailed},
		{Text: "tool", Status: runner.ActUnknown},
	}
	got := Render(st, PlainStyles(), GlyphsFor(true))

	a := ASCIIGlyphs()
	for _, want := range []string{
		"* Bash: go build " + a.ActOK,
		"* Bash: go test " + a.ActFail,
		"* tool " + a.ActUnknown,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ascii mode is missing %q", want)
		}
	}
	// Distinct from each other AND from the marks already in use.
	marks := map[string]string{"ok": a.ActOK, "fail": a.ActFail, "unknown": a.ActUnknown}
	taken := map[string]string{
		"act": a.Act, "ellipsis": a.Ellipsis, "focus": a.Focus, "warn": a.Warn,
		"sep": a.Sep, "rule": a.Rule, "prompt": a.Prompt, "caret": a.Caret,
		"up": a.Up, "down": a.Down, "idle": a.Idle,
	}
	for name, m := range marks {
		for other, t2 := range taken {
			if m == t2 {
				t.Errorf("the ascii %s mark %q is already the %s glyph; a mark that means two things is not a mark", name, m, other)
			}
		}
	}
	if a.ActOK == a.ActFail || a.ActOK == a.ActUnknown || a.ActFail == a.ActUnknown {
		t.Error("two outcome marks collide in ascii mode")
	}
}

// TestFailureDetailIsTheVendorsOwnWords: a failed call may carry the vendor's
// first line about why. It renders indented under the call it belongs to, so it
// cannot be read as a separate step — and only a FAILURE gets one, because
// pasting every successful command's stdout into a 37-cell column would bury
// the answer the room exists to compare.
func TestFailureDetailIsTheVendorsOwnWords(t *testing.T) {
	st := room()
	st.Turn = 1
	st.Expanded = true
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Acts = []Act{
		{Text: "Bash: go test ./...", Status: runner.ActFailed, Detail: "FAIL github.com/x/y"},
		{Text: "Bash: go vet ./...", Status: runner.ActOK, Detail: "this should never render"},
	}
	got := render(st)
	if !strings.Contains(got, "FAIL github.com/x/y") {
		t.Error("the failure detail is missing")
	}
	if strings.Contains(got, "this should never render") {
		t.Error("a successful call rendered its output; the trace is a record of what was done, not a log")
	}
}

// TestActOutcomeColors is the style half of the split, asserted with the
// coloured set rather than the plain one. Colour is the SECOND signal — the
// glyph test above is the first — but a failed step should still read as one at
// a glance, and an unknown one must not read as an alarm.
func TestActOutcomeColors(t *testing.T) {
	sty := NewStyles(true)
	g := UnicodeGlyphs()
	cases := []struct {
		status runner.ActStatus
		style  lipgloss.Style
	}{
		{runner.ActOK, sty.SevOK},
		{runner.ActFailed, sty.SevCrit},
		// Not a severity. Not knowing how a step went is not an alarm, and
		// colouring it as one trains the eye to ignore the real ones.
		{runner.ActUnknown, sty.Muted},
	}
	for _, c := range cases {
		mark, got := actMark(c.status, sty, g)
		if mark == "" {
			t.Errorf("status %v has no mark", c.status)
		}
		if got.Render(mark) != c.style.Render(mark) {
			t.Errorf("status %v renders %q, want %q", c.status, got.Render(mark), c.style.Render(mark))
		}
	}
	// A styled body line must survive the fixed-width cell without its escape
	// sequence being cut — the §9.5 trap, which goldens are blind to because
	// they render with PlainStyles.
	st := room()
	st.Turn = 1
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Acts = []Act{{Text: "Bash: " + strings.Repeat("go test ", 12), Status: runner.ActFailed}}
	out := Render(st, sty, g)
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > st.Width {
			t.Errorf("styled line %d is %d cells, terminal is %d", i, w, st.Width)
		}
	}
}

// TestMixedTraceGolden is the whole feature in one frame: a column whose steps
// succeeded, failed with a reason, and ended without saying how — beside a
// column still waiting on one.
func TestMixedTraceGolden(t *testing.T) {
	st := room()
	st.Turn = 2
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Acts = []Act{
		{ID: "toolu_a", Text: "Glob: **/*.go", Status: runner.ActOK},
		{ID: "toolu_b", Text: "Bash: go test ./...", Status: runner.ActFailed, Detail: "FAIL council 0.3s"},
		{ID: "toolu_c", Text: "Read: view.go", Status: runner.ActOK},
	}
	st.Columns[0].Body = "One test fails."
	st.Columns[1].Phase = PhaseStreaming
	st.Columns[1].Acts = []Act{
		{ID: "item_1", Text: "go build ./...", Status: runner.ActPending},
	}
	st.Columns[2].Phase = PhaseWaiting
	// The agy column, as the adapter now builds it: real tool names, and the
	// per-step failure agy DOES report. It used to read `tool ?` / `checkpoint ?`
	// — a gear icon per plumbing message and one indistinguishable entry per real
	// call — which is what this golden was quietly holding in place.
	st.Columns[2].Acts = []Act{
		{ID: "step-3", Text: "list_dir: C:\\ws", Status: runner.ActUnknown},
		{ID: "step-8", Text: "run_command: pwsh -Command \"Get-ChildItem\"", Status: runner.ActFailed,
			Detail: "granting access to C:\\: Access is denied."},
	}
	golden(t, "activity-outcomes", render(st))
}

// TestAFailureReasonIsACardNotRawWreckage.
//
// A trace entry's detail is whatever the vendor wrote on stderr, which on
// Windows is routinely a path longer than the column and sometimes several lines
// of it. Both of those used to reach the renderer intact: sanitize deliberately
// preserves newlines (a prose reply's paragraphs are content), so a multi-line
// stderr blob arrived as ragged fragments at random widths, and nothing bounded
// how many rows one failed call could take from the answers the room exists to
// compare.
func TestAFailureReasonIsACardNotRawWreckage(t *testing.T) {
	g := GlyphsFor(false)
	long := "error executing cascade step: CORTEX_STEP_TYPE_RUN_COMMAND:\n" +
		`granting access to C:\Users\dev\code\telltale\internal\council: Access is denied.` + "\n" +
		"see the vendor's own troubleshooting page for the full list of causes and remedies"

	lines := actDetail(long, 37, g)
	if len(lines) > actDetailMaxRows {
		t.Errorf("one failed call spent %d rows of a column, cap is %d", len(lines), actDetailMaxRows)
	}
	for i, l := range lines {
		if lipgloss.Width(l) > 37 {
			t.Errorf("detail line %d is %d cells, column is 37", i, lipgloss.Width(l))
		}
		// Four, not two: the line above a detail is the hanging tail of the
		// command it explains, and both at the same indent is a distinction
		// carried by colour alone.
		if !strings.HasPrefix(l, "    ") {
			t.Errorf("detail line %d does not hang under its entry: %q", i, l)
		}
		if strings.ContainsAny(l, "\n\r\t") {
			t.Errorf("detail line %d still carries raw whitespace: %q", i, l)
		}
	}
	// A clipped detail must never be able to read as a complete one.
	if !strings.HasSuffix(lines[len(lines)-1], g.Ellipsis) {
		t.Errorf("a clipped reason does not say it was clipped: %q", lines[len(lines)-1])
	}

	// Same clip under --ascii, where the ellipsis is ">" — the one cell in this
	// entry that a hardcoded "…" would have broken.
	a := GlyphsFor(true)
	al := actDetail(long, 37, a)
	if !strings.HasSuffix(al[len(al)-1], a.Ellipsis) {
		t.Errorf("the ascii clip mark is missing: %q", al[len(al)-1])
	}
	if strings.Contains(al[len(al)-1], "…") {
		t.Errorf("the unicode ellipsis survived --ascii: %q", al[len(al)-1])
	}

	// A reason that fits is left exactly alone: the clip is a ceiling, not a
	// format, and marking an unclipped line would be the opposite lie.
	short := actDetail("FAIL council 0.3s", 37, g)
	if len(short) != 1 || strings.Contains(short[0], g.Ellipsis) {
		t.Errorf("a short reason was clipped anyway: %q", short)
	}
}

// TestAWrappedTraceEntryHangsUnderItsOwnMark.
//
// §9.11 gave every card in a column one grammar — a title with its body hanging
// under it — and the trace entry was the one that never got it. A `run_command`
// carrying a Windows path wraps in a 37-cell column, and its continuation used
// to start hard against the column edge: a wrapped command read as a second,
// nameless entry, and the outcome mark landed on a line with nothing on it to
// say what it was the outcome of.
func TestAWrappedTraceEntryHangsUnderItsOwnMark(t *testing.T) {
	g := GlyphsFor(false)
	a := Act{Text: `run_command: pwsh -Command "Get-ChildItem C:\Users\dev\code"`, Status: runner.ActFailed}
	lines := actLines(a, 37, PlainStyles(), g)
	if len(lines) < 2 {
		t.Fatal("the entry under test did not wrap")
	}
	if !strings.HasPrefix(lines[0], g.Act+" ") {
		t.Errorf("the entry lost its mark: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "  ") {
		t.Errorf("a wrapped command does not hang under its entry: %q", lines[1])
	}
	// The outcome still lands on the entry's last line rather than being
	// stranded: it is what the eye is looking for once the command is read.
	if !strings.Contains(lines[1], g.ActFail) {
		t.Errorf("the outcome mark is not on the entry's last line: %q", lines[1])
	}
}
