package council

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// TestTheBadgeWordsLeaveWholeOrNotAtAll is §9.11 on the badge row between the
// strip floor and the width where every word fits. stripBadges ruled at
// fourteen cells that a clipped state word is not a word, and that a clipped
// one which is also a PREFIX of another word in the vocabulary is worse than
// damage: `fina` is not `final only`. Above the floor the row used to shed
// only when a containment badge was on it, so `  ro:requested  final only`
// (26 cells) reached the caller's fit() in a 25-cell column and came back as
// `ro:requested  final onl`. Three goldens carried that cut and passed.
//
// Swept over every width from the strip floor to 80, over every shape the
// row takes: the six postures with each granularity word, a seat tree, a
// shared tree with and without a reason, and a replayed room. At each width
// the row is inside w, fit() only pads it, and every word on it is a word the
// room has, whole. The vocabulary is enumerated rather than derived, so a new
// word that clips would fail here by not being on the list.
func TestTheBadgeWordsLeaveWholeOrNotAtAll(t *testing.T) {
	postures := []SandboxLevel{SandboxTools, SandboxEnforced, SandboxRequested, SandboxWrite, SandboxGated, SandboxNone}
	grans := []Granularity{GranTokens, GranEvents, GranFinalOnly}
	contains := []ContainClaim{
		{},
		{Level: ContainSeatTree, Branch: "seat/claude"},
		{Level: ContainShared},
		{Level: ContainShared, Why: "not a git repo"},
	}

	type shape struct {
		name string
		st   State
		col  Column
	}
	var shapes []shape
	for _, p := range postures {
		for _, gr := range grans {
			for _, cn := range contains {
				for _, replay := range []bool{false, true} {
					st := room()
					st.Replay = replay
					c := st.Columns[0]
					c.Sandbox = SandboxClaim{Level: p}
					c.Gran = gr
					c.Containment = cn
					name := c.Sandbox.Badge() + "/" + gr.String() + "/" + cn.Badge(false)
					if replay {
						name = "replay " + name
					}
					shapes = append(shapes, shape{name, st, c})
				}
			}
		}
	}

	for _, ascii := range []bool{false, true} {
		g := GlyphsFor(ascii)
		// Every word the row may carry, whole. The containment badge has a
		// short form the reason sheds to, and a warning mark leads the
		// shared tree when a reason exists (badgeRow).
		words := map[string]bool{"REPLAY": true}
		for _, p := range postures {
			words[SandboxClaim{Level: p}.Badge()] = true
		}
		for _, gr := range grans {
			words[gr.String()] = true
		}
		for _, cn := range contains {
			if b := cn.Badge(ascii); b != "" {
				words[b] = true
				words[g.Warn+" "+b] = true
			}
		}
		words[g.Warn+" "+ContainClaim{Level: ContainShared}.Badge(ascii)] = true

		for _, sh := range shapes {
			st := sh.st
			st.ASCII = ascii
			for w := stripWidth + 1; w <= 80; w++ {
				row := badgeRow(st, sh.col, w, PlainStyles(), g)
				if lipgloss.Width(row) > w {
					t.Errorf("ascii=%v %s w=%d: the badge row is %d cells, so fit() would clip it: %q",
						ascii, sh.name, w, lipgloss.Width(row), row)
				}
				if strings.TrimRight(fit(row, w), " ") != strings.TrimRight(row, " ") {
					t.Errorf("ascii=%v %s w=%d: fit() changed the badge row: %q -> %q",
						ascii, sh.name, w, row, fit(row, w))
				}
				if row == "" {
					continue
				}
				if !strings.HasPrefix(row, "  ") {
					t.Errorf("ascii=%v %s w=%d: the row is not indented under the seat name: %q",
						ascii, sh.name, w, row)
				}
				for _, word := range strings.Split(strings.TrimPrefix(row, "  "), "  ") {
					if !words[word] {
						t.Errorf("ascii=%v %s w=%d: %q is not a word this room has: %q",
							ascii, sh.name, w, word, row)
					}
				}
				if st.Replay && !strings.HasPrefix(row, "  REPLAY") {
					t.Errorf("ascii=%v %s w=%d: a replayed row does not lead with REPLAY: %q",
						ascii, sh.name, w, row)
				}
			}
			// Not vacuous: at the reference width every shape has room for
			// every one of its words.
			st.Replay = sh.st.Replay
			wide := badgeRow(st, sh.col, 80, PlainStyles(), g)
			for _, want := range []string{sh.col.Sandbox.Badge(), sh.col.Gran.String(), sh.col.Containment.Badge(ascii)} {
				if !strings.Contains(wide, want) {
					t.Errorf("ascii=%v %s: %q is missing at 80 cells: %q", ascii, sh.name, want, wide)
				}
			}
		}
	}
}

// TestTheBadgeRowShedsInStripBadgesOrder pins the ladder at the widths where
// each rung gives way, on the longest row the room draws: a seat with the
// longest posture word in the shared tree with a reason, final only, in a
// replay. The granularity word leaves first, then the reason, then the
// containment badge, then the posture badge; REPLAY is what is left, and it
// is what the strip keeps below the floor. The posture badge only ever leaves
// beside REPLAY: alone, every badge the room spells fits above the strip.
func TestTheBadgeRowShedsInStripBadgesOrder(t *testing.T) {
	st := room()
	st.Replay = true
	c := st.Columns[0]
	c.Sandbox = SandboxClaim{Level: SandboxRequested}
	c.Gran = GranFinalOnly
	c.Containment = ContainClaim{Level: ContainShared, Why: "not a git repo"}

	full := "  REPLAY  ro:requested  ⚠ shared tree · not a git repo  final only"
	if got := lipgloss.Width(full); got != 66 {
		t.Fatalf("the full row measures %d cells, and the rungs below were counted at 66", got)
	}
	for _, tc := range []struct {
		w    int
		want string
	}{
		// One cell under each rung's floor sheds exactly one word.
		{65, "  REPLAY  ro:requested  ⚠ shared tree · not a git repo"},
		{53, "  REPLAY  ro:requested  ⚠ shared tree"},
		{36, "  REPLAY  ro:requested"},
		{21, "  REPLAY"},
		// At each rung's exact floor the word stays: the ladder sheds at the
		// width where a word stops fitting whole, not earlier.
		{66, full},
		{54, "  REPLAY  ro:requested  ⚠ shared tree · not a git repo"},
		{37, "  REPLAY  ro:requested  ⚠ shared tree"},
		{22, "  REPLAY  ro:requested"},
	} {
		got := badgeRow(st, c, tc.w, PlainStyles(), UnicodeGlyphs())
		if got != tc.want {
			t.Errorf("w=%d: %q, want %q", tc.w, got, tc.want)
		}
	}
}

// TestNarrowColumnsShedWholeWords is the rule through Render, on the room
// where it was measured: four seats at 120 columns give each 25 cells.
// `unsandboxed  final only` is 25 with its indent and stays whole;
// `ro:requested  final only` is 26 and the seat draws `ro:requested` alone,
// where it used to draw `ro:requested  final onl`; the writing seat in the
// shared tree sheds its granularity word and keeps `⚠ shared tree`, the
// claim §9.55 exists to make visible.
func TestNarrowColumnsShedWholeWords(t *testing.T) {
	st := room()
	st.Turn = 1
	st.Write = true
	st.Columns = append(st.Columns, Column{
		Vendor: model.VendorCursor, Label: "Cursor",
		Avail:       AvailInstalled,
		Sandbox:     SandboxClaim{Level: SandboxWrite, Detail: "started with --write"},
		Containment: ContainClaim{Level: ContainShared, Why: "not a git repo"},
		Gran:        GranTokens, Phase: PhaseDone,
		Body: "Docs written.",
	})

	got := render(st)
	for _, want := range []string{"  ro:requested  ", "  unsandboxed  final only  ", "  WRITES  ⚠ shared tree  "} {
		if !strings.Contains(got, want) {
			t.Errorf("the room does not draw %q:\n%s", want, got)
		}
	}
	for _, cut := range []string{"final onl ", "fina ", "share ", "ro:requested  final", "not a git r"} {
		if strings.Contains(got, cut) {
			t.Errorf("a badge word was clipped to %q:\n%s", cut, got)
		}
	}
	golden(t, "badges-shed-whole-words", got)
}
