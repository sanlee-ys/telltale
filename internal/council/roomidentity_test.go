package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/svgframe"
)

// The 2026-09-03 room-identity graft, asserted rather than eyeballed.
//
// docs/room-identity.md is the argument; this file is the part of it a build can
// check. Three claims, and every one of them was made by an audit of a rendered
// picture rather than by reading the code:
//
//  1. the identity survives at a projector width — every ink clears the floor
//     its job needs against the ground the room is actually drawn on;
//  2. the posture rail ENHANCES and never DEFINES — strip it and the room is
//     byte for byte what it was, which is why the 118 layout goldens are the
//     proof of it;
//  3. a lane, a verdict and an affordance are all carried by words and marks, so
//     `--ascii` and NO_COLOR lose a grouping and never a fact.

// projectorFloors is the contrast every value in this palette has to clear, and
// WHY it has to clear that number rather than a nicer one.
//
// 4.5:1 is WCAG AA for body text and 3:1 is the floor for a non-text UI
// component; a rule and a track are the second kind and prose is the first. The
// audit's finding was that three values were under those floors at 180x50 — "four
// unfocused columns recede too far; their prose and hairlines will disappear at
// the back" — so this is that finding turned into a gate.
//
// The RAIL is deliberately exempt and asserted from the other side (see
// TestTheRailIsLegibleOnBothGrounds): a ground a reader can READ would be a ground
// competing with the ink printed on it.
func TestTheInkScaleClearsTheProjectorFloor(t *testing.T) {
	for _, ground := range []struct {
		name string
		bg   string
		pal  Palette
	}{
		{"night", svgframe.Dark().Background, NightPalette()},
		{"paper", svgframe.Light().Background, PaperPalette()},
	} {
		for _, tok := range []struct {
			name  string
			hex   string
			floor float64
		}{
			{"Measured", ground.pal.Measured, 4.5},
			{"Muted", ground.pal.Muted, 4.5},
			{"Dim", ground.pal.Dim, 4.5},
			{"Identity", ground.pal.Identity, 4.5},
			{"Withdrawn", ground.pal.Withdrawn, 4.5},
			{"Broke", ground.pal.Broke, 4.5},
			{"RuleInk", ground.pal.RuleInk, 3},
			{"Hair", ground.pal.Hair, 3},
		} {
			if c := svgframe.Contrast(tok.hex, ground.bg); c < tok.floor {
				t.Errorf("%s: %s is %.2f:1 on %s, below the %.1f:1 floor its job needs",
					ground.name, tok.name, c, ground.bg, tok.floor)
			}
		}
		// The scale is ordered, or it is not a scale. Dim is the reading area of
		// a column the keys do not move and it has to stay BELOW the chrome
		// around it, or the demotion stops meaning anything.
		if svgframe.Contrast(ground.pal.Dim, ground.bg) >= svgframe.Contrast(ground.pal.Muted, ground.bg) {
			t.Errorf("%s: Dim is not below Muted; an unfocused column no longer recedes", ground.name)
		}
	}
}

// zoomHeadroom is the contrast the two RULE inks carry ABOVE the 3:1 floor a
// non-text component needs, so that a one-pixel mark still clears that floor
// after a Zoom share has been resampled onto a laptop.
//
// Where 3.75 comes from, and it is measured rather than chosen. The 2026-09-04
// pass rendered the room at the owner's own pixel size, scaled it the way a
// viewer's client does, and read the arrived pixels back. A one-pixel horizontal
// rule left the owner's screen at 3.1:1 and arrived at 2.3:1 under a 0.75
// resample: the mark keeps about 0.72 of its contrast above 1. An ink that has
// to arrive at 3:1 must therefore leave at 1 + 2/0.72 = 3.8:1, and 3.75 is that
// number with the rounding room a floor needs. docs/room-identity.md's last
// section carries the measurement and the frames it was read from.
//
// It applies to Hair and RuleInk and to nothing else. Prose is many pixels thick
// and loses almost nothing to a resample, so widening this to the text inks
// would be raising a floor that no measurement asked for.
const zoomHeadroom = 3.75

// TestTheRuleInksCarryTheZoomHeadroom is the 2026-09-04 finding turned into a
// gate, the way TestTheInkScaleClearsTheProjectorFloor is the 2026-09-03 one.
//
// The two tests assert different things and both are needed. The projector test
// says an ink clears its floor at the reader's eye. This says the two THIN marks
// leave with enough contrast that the floor is still met after the share is
// resampled — which is the demo's real viewing condition, and the one the
// audit's "the projector test is still open" was pointing at.
func TestTheRuleInksCarryTheZoomHeadroom(t *testing.T) {
	for _, ground := range []struct {
		name string
		bg   string
		pal  Palette
	}{
		{"night", svgframe.Dark().Background, NightPalette()},
		{"paper", svgframe.Light().Background, PaperPalette()},
	} {
		for _, tok := range []struct{ name, hex string }{
			{"Hair", ground.pal.Hair}, {"RuleInk", ground.pal.RuleInk},
		} {
			if c := svgframe.Contrast(tok.hex, ground.bg); c < zoomHeadroom {
				t.Errorf("%s: %s is %.2f:1 on %s, below the %.2f:1 a rule needs to survive a Zoom share",
					ground.name, tok.name, c, ground.bg, zoomHeadroom)
			}
		}
		// The two rule weights differ by INK as well as by stroke, and raising
		// both is how that stayed true. A reader at a projector width cannot
		// resolve a stroke; they can resolve which line is darker.
		hair := svgframe.Contrast(ground.pal.Hair, ground.bg)
		rule := svgframe.Contrast(ground.pal.RuleInk, ground.bg)
		if rule <= hair {
			t.Errorf("%s: RuleInk is %.2f:1 and Hair is %.2f:1; the two rule weights no longer differ by ink",
				ground.name, rule, hair)
		}
	}
	// Night keeps the ordered scale the identity is built on, and the raise
	// spent the gap between Dim and the ink rule. Assert what is left, so a
	// later raise cannot invert it by accident.
	n := NightPalette()
	bg := svgframe.Dark().Background
	for _, pair := range []struct{ above, below string }{
		{"Muted", "Dim"}, {"Dim", "RuleInk"}, {"RuleInk", "Hair"},
	} {
		hex := map[string]string{
			"Muted": n.Muted, "Dim": n.Dim, "RuleInk": n.RuleInk, "Hair": n.Hair,
		}
		a, b := svgframe.Contrast(hex[pair.above], bg), svgframe.Contrast(hex[pair.below], bg)
		if a <= b {
			t.Errorf("night: %s (%.2f:1) is not above %s (%.2f:1); the ink scale is out of order",
				pair.above, a, pair.below, b)
		}
	}
}

// TestTheRailIsLegibleOnBothGrounds pins the posture rail's own two properties,
// which pull against each other.
//
// It has to be VISIBLE — a ledger nobody sees is not the frame's governing
// object — and it has to be QUIET, because every ink the badge row spends is
// printed on it and a saturated band would eat the accents two rows down. So the
// band itself is asserted to be barely there against the terminal's ground, and
// every ink that lands on it is asserted to clear WCAG AA against the BAND
// rather than against the terminal.
func TestTheRailIsLegibleOnBothGrounds(t *testing.T) {
	for _, ground := range []struct {
		name string
		bg   string
		pal  Palette
	}{
		{"night", svgframe.Dark().Background, NightPalette()},
		{"paper", svgframe.Light().Background, PaperPalette()},
	} {
		rail := ground.pal.Rail
		if rail == "" {
			t.Fatalf("%s: the palette has no rail ground", ground.name)
		}
		// Seen, not read. Above 1.1:1 it is a band; much above 2:1 it would be a
		// second surface laid over the room.
		if c := svgframe.Contrast(rail, ground.bg); c < 1.1 || c > 2 {
			t.Errorf("%s: the rail is %.2f:1 against the terminal's ground, outside [1.1, 2]", ground.name, c)
		}
		// Everything the badge row can print, printed on it.
		for _, tok := range []struct{ name, hex string }{
			{"Measured", ground.pal.Measured},
			{"Muted", ground.pal.Muted},
			{"Withdrawn", ground.pal.Withdrawn},
			{"Broke", ground.pal.Broke},
			{"Identity", ground.pal.Identity},
		} {
			if c := svgframe.Contrast(tok.hex, rail); c < 4.5 {
				t.Errorf("%s: %s is %.2f:1 ON THE RAIL, below WCAG AA — the band is eating a claim",
					ground.name, tok.name, c)
			}
		}
		// The rules that CROSS the rail are UI components, not text.
		for _, tok := range []struct{ name, hex string }{
			{"Hair", ground.pal.Hair}, {"RuleInk", ground.pal.RuleInk},
		} {
			if c := svgframe.Contrast(tok.hex, rail); c < 1.5 {
				t.Errorf("%s: %s is %.2f:1 on the rail; a separator crossing the ledger vanished",
					ground.name, tok.name, c)
			}
		}
	}
}

// TestPlainStylesPaintsNoRail is the golden contract for the whole graft, and it
// is the reason 118 layout goldens did not move for it.
//
// Every treatment this identity adds is a colour, a weight or a ground, and
// PlainStyles renders all three as the identity function BY CONSTRUCTION rather
// than by inspection: onBand, bandFill and onRail short-circuit on Plain, and
// fitOn is handed an identity style there. It is also the accessibility rule the
// LEDGER did not lift, checked mechanically: NO_COLOR resolves to exactly this
// set, so a room with no rail is a room that has lost no claim.
func TestPlainStylesPaintsNoRail(t *testing.T) {
	p := PlainStyles()
	if p.RailGround != "" {
		t.Errorf("PlainStyles carries a rail ground %q", p.RailGround)
	}
	for _, s := range []string{"", "ro:enforced", "  padded  "} {
		if got := p.onBand(p.Muted).Render(s); got != s {
			t.Errorf("onBand is not a no-op under PlainStyles: %q -> %q", s, got)
		}
		if got := p.bandFill().Render(s); got != s {
			t.Errorf("bandFill is not a no-op under PlainStyles: %q -> %q", s, got)
		}
		if got := p.onRail().Muted.Render(s); got != s {
			t.Errorf("onRail is not a no-op under PlainStyles: %q -> %q", s, got)
		}
		if got := fitOn(s, len(s), p.bandFill()); got != fit(s, len(s)) {
			t.Errorf("fitOn diverges from fit under PlainStyles: %q -> %q", s, got)
		}
	}
	// The lane and the track are on the same contract.
	for _, st := range []struct {
		name  string
		style interface{ Render(...string) string }
	}{{"Lane", p.Lane()}, {"Track", p.Track()}} {
		if got := st.style.Render("x"); got != "x" {
			t.Errorf("PlainStyles().%s is not a no-op: %q", st.name, got)
		}
	}
	// And the coloured set really does paint one, or the test above is vacuous.
	if NewStyles(true).RailGround == "" {
		t.Error("the coloured set has no rail ground; the assertions above prove nothing")
	}
}

// TestARacerWithNoRankGetsAnEmptyTrack is §4a.1 on the race board.
//
// A lane's fill is the racer's host-observed finishing position and NOTHING
// else. A racer the host has not ranked has no such position, so it gets a track
// at full length with nothing claimed — never a short one, because a short track
// is a finishing position the room made up. The two states must not collapse,
// which is the same law zero-vs-absent states one surface over.
func TestARacerWithNoRankGetsAnEmptyTrack(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		g := GlyphsFor(ascii)
		sty := PlainStyles()
		st := room()
		st.ASCII = ascii

		running := strings.Join(runningLaneLines(st, 60, sty, g), "\n")
		if !strings.Contains(running, "no rank yet") {
			t.Errorf("ascii=%v: an unranked racer does not say so: %q", ascii, running)
		}
		if strings.Contains(running, g.Fill) {
			t.Errorf("ascii=%v: an unranked racer's track is filled: %q", ascii, running)
		}
		if !strings.Contains(running, strings.Repeat(g.Rule, laneCells)) {
			t.Errorf("ascii=%v: an unranked racer has no track at all: %q", ascii, running)
		}

		// A racer that DID finish is on the board, so its lane is never empty —
		// last of three still claims a cell.
		last := strings.Join(laneLines(3, 3, "3rd of 3 · done · 20s", 60, sty, g), "\n")
		if !strings.Contains(last, g.Fill) {
			t.Errorf("ascii=%v: the last finisher's lane is empty: %q", ascii, last)
		}
		first := strings.Join(laneLines(1, 3, "1st of 3 · done · 9s", 60, sty, g), "\n")
		if strings.Count(first, g.Fill) <= strings.Count(last, g.Fill) {
			t.Errorf("ascii=%v: first does not out-fill last: %q vs %q", ascii, first, last)
		}
		if strings.Count(first, g.Fill) != laneCells {
			t.Errorf("ascii=%v: the winner's track is not full: %q", ascii, first)
		}
		// The words never leave, at any width. The TRACK sheds first.
		narrow := strings.Join(laneLines(1, 3, "1st of 3 · done · 9s", 22, sty, g), "\n")
		if !strings.Contains(narrow, "1st of 3") {
			t.Errorf("ascii=%v: a narrow lane dropped the words: %q", ascii, narrow)
		}
		if strings.Contains(narrow, strings.Repeat(g.Fill, 2)) {
			t.Errorf("ascii=%v: the track did not shed before the words: %q", ascii, narrow)
		}
	}
}

// TestEveryVerdictWearsItsOwnMark. The mark is a second signal over a sentence
// that already said PASS, FAIL or unavailable — but two verdicts arriving at one
// mark would be worse than no mark at all, because a reader who has learnt the
// alphabet would be reading a wrong answer rather than no answer.
func TestEveryVerdictWearsItsOwnMark(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		g := GlyphsFor(ascii)
		st := room()
		seen := map[string]string{}
		for _, tc := range []struct {
			name string
			ck   ArenaCheck
			word string
		}{
			{"pass", ArenaCheck{Cmd: "go test ./...", Exited: true, Code: 0}, "check PASS"},
			{"fail", ArenaCheck{Cmd: "go test ./...", Exited: true, Code: 2}, "check FAIL"},
			{"unavailable", ArenaCheck{Cmd: "go test ./...", Err: "exec: \"go\""}, "check unavailable"},
			{"running", ArenaCheck{Cmd: "go test ./...", Running: true}, "check running"},
		} {
			ck := tc.ck
			mark := verdictMark(&ck, st, g)
			if prev, dup := seen[mark]; dup {
				t.Errorf("ascii=%v: %s and %s share the mark %q", ascii, tc.name, prev, mark)
			}
			seen[mark] = tc.name

			line := strings.Join(verdictLines(&ck, st, 70, PlainStyles(), g), "\n")
			if !strings.HasPrefix(line, mark+" ") {
				t.Errorf("ascii=%v: %s does not lead with its mark: %q", ascii, tc.name, line)
			}
			// The WORDING is what --ascii and NO_COLOR read, and it is §9.48's
			// own, unchanged.
			if !strings.Contains(line, tc.word) {
				t.Errorf("ascii=%v: %s lost its wording: %q", ascii, tc.name, line)
			}
		}
	}
}

// TestTheArenaRulePrintsTheAdoptCommand. `/adopt` is what a race is FOR, and the
// room drew the branch, the diff and the verdict without ever drawing the one
// command that acts on all three.
//
// It is text, so it survives --ascii and NO_COLOR untouched — and it SHEDS
// rather than clips when the rule cannot hold both it and the branch, because a
// half-printed command is a command that does not work when typed.
func TestTheArenaRulePrintsTheAdoptCommand(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		g := GlyphsFor(ascii)
		wide := stripANSI(arenaRule("arena/t6/claude", adoptAffordance(), 60, PlainStyles(), g))
		if !strings.Contains(wide, "arena arena/t6/claude") {
			t.Errorf("ascii=%v: the rule lost its branch: %q", ascii, wide)
		}
		if !strings.HasSuffix(strings.TrimRight(wide, " "), "/adopt") {
			t.Errorf("ascii=%v: the rule does not end with the adopt command: %q", ascii, wide)
		}
		narrow := stripANSI(arenaRule("arena/t6/antigravity", adoptAffordance(), 20, PlainStyles(), g))
		if strings.Contains(narrow, "/adop") && !strings.Contains(narrow, "/adopt") {
			t.Errorf("ascii=%v: the adopt command was clipped rather than shed: %q", ascii, narrow)
		}
	}
}

// TestAGatedRoomAsksOneQuestionAtFullWeight.
//
// A room with three blocked seats drew three gate cards, every one of them
// opening at Alert, and the 2026-09-03 audit read the result as "too many
// simultaneous yellow warnings flatten the hierarchy; the actionable gate line is
// not singular enough." The keys answer exactly ONE call — the oldest, the one
// the footer names — so that is the one card whose title takes weight.
//
// Nothing is taken away from the others: same ⚠, same words, same warning ink,
// one step of weight quieter. Under PlainStyles all three are identical, which is
// what says the singularity is an emphasis and not a claim.
func TestAGatedRoomAsksOneQuestionAtFullWeight(t *testing.T) {
	sty := NewStyles(true)
	g := UnicodeGlyphs()
	q := []PendingGate{{Vendor: "claude", Text: "Write: internal/council/gate.go"}}

	next := strings.Join(gateCardLines(q, "", true, 40, sty, g), "\n")
	later := strings.Join(gateCardLines(q, "", false, 40, sty, g), "\n")
	if next == later {
		t.Error("the card the keys answer renders like the ones they do not")
	}
	// The TITLE line, which is where the weight lives; the card wraps at this
	// width and the continuation carries the same style.
	if !strings.HasPrefix(next, sty.Alert.Render(g.Warn+" waiting on you: Write:")) {
		t.Errorf("the answerable card is not at full weight:\n%s", next)
	}
	if strings.HasPrefix(later, sty.Alert.Render(g.Warn+" waiting on you: Write:")) {
		t.Errorf("a card the keys do not answer is still at full weight:\n%s", later)
	}
	// The quieter card is still a claim, still marked, still worded the same.
	if stripANSI(next) != stripANSI(later) {
		t.Errorf("a quieter gate card says something different:\n%s\n---\n%s",
			stripANSI(next), stripANSI(later))
	}
	if !strings.Contains(stripANSI(later), g.Warn+" waiting on you:") {
		t.Errorf("a quieter gate card lost its mark:\n%s", stripANSI(later))
	}
	// And under PlainStyles the two are one row of bytes, which is why no golden
	// moved for this.
	plain := PlainStyles()
	if strings.Join(gateCardLines(q, "", true, 40, plain, g), "\n") !=
		strings.Join(gateCardLines(q, "", false, 40, plain, g), "\n") {
		t.Error("the singular gate card is visible to PlainStyles")
	}
}

// TestThePosturesPageIsAnEvidenceLadder. The legend is the room's vocabulary for
// the one thing no competitor draws, and it was printed in an order that was no
// order: tools, enforced, requested, none, write, gated.
//
// What is asserted is the ORDER and the completeness, not the prose. A badge that
// exists with nothing to say what it means is already caught by
// TestEveryBadgeIsExplained; this catches a badge that is explained in the wrong
// place on the ladder, which is the failure that makes the page a list again.
func TestThePosturesPageIsAnEvidenceLadder(t *testing.T) {
	want := []SandboxLevel{
		SandboxEnforced, SandboxTools, SandboxRequested,
		SandboxGated, SandboxWrite, SandboxNone,
	}
	got := helpBadgeGloss()
	if len(got) != len(want) {
		t.Fatalf("the legend has %d rungs, want %d", len(got), len(want))
	}
	for i, e := range got {
		if e.level != want[i] {
			t.Errorf("rung %d is %v, want %v — the ladder is out of evidence order", i, e.level, want[i])
		}
	}
	// The page still says which way the ladder runs, and still refuses a
	// room-wide claim.
	page := strings.Join(helpPostures(room(), layoutFor(room(), UnicodeGlyphs()), PlainStyles(), UnicodeGlyphs()), "\n")
	for _, want := range []string{"Best evidence first", "not the room", "WORKSPACE above"} {
		if !strings.Contains(page, want) {
			t.Errorf("the postures page dropped %q", want)
		}
	}
}
