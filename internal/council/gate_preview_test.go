package council

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The gate card now shows the edit it is asking about — but only when the
// vendor's own permission payload carried both halves of it (§9.41). These
// tests are the two sides of that sentence, and the second one is the one that
// matters: a card that renders NOTHING for a payload without a before is the
// honesty property, and it is the easier of the two to break by accident.

// editingRoom is gatedRoom with a payload that carries both halves, which is
// what Claude Code's Edit tool measured to send (see runner.Gate's capture).
//
// The fixture's indentation is SPACES, deliberately, even though the code it
// imitates is Go and would arrive with tabs. queueGate runs both halves through
// the same sanitize every other vendor string goes through, and that collapses
// a tab to one space before it can reach a fixed-width grid — so a golden built
// from a tab would pin a line the live path cannot produce.
func editingRoom() State {
	st := gatedRoom()
	st.Columns[0].Acts[1] = Act{ID: "t1", Text: "Edit: internal/council/gate.go"}
	st.Gates = []PendingGate{{
		Vendor: model.VendorClaude, RequestID: "r1", ToolUseID: "t1",
		Text: "Edit: internal/council/gate.go",
		Old:  "func gateCard() {\n return nil\n}",
		New:  "func gateCard() []string {\n return lines\n}",
	}}
	return st
}

func TestGatePreview(t *testing.T) {
	golden(t, "gate-preview", render(editingRoom()))
}

// TestGatePreviewASCII. The `-`/`+` prefixes are the whole signal and the
// colour only seconds it, so the reduced glyph set must show the same edit —
// the goldens render PlainStyles, which is what makes this a proof rather than
// an approximation.
func TestGatePreviewASCII(t *testing.T) {
	st := editingRoom()
	st.ASCII = true
	golden(t, "gate-preview-ascii", Render(st, PlainStyles(), GlyphsFor(true)))
}

// TestAPayloadWithoutABeforeShowsNoPreview is the rule this feature exists to
// keep. A Write carries `content` and no old half — measured at Claude Code
// 2.1.226 in the same session as the Edit capture — so the card names the call
// and stops. Reconstructing the before from the file on disk would be the room
// displaying something no vendor told it (§4a.1).
//
// It compares against `gate-card`, the EXISTING golden, rather than adding one
// of its own — and that reuse is the assertion. A new file would have been
// byte-identical to that one, and "identical to the frame before this feature
// existed" is a stronger claim than any fresh copy of it: it fails the moment a
// payload with no halves starts drawing anything at all.
func TestAPayloadWithoutABeforeShowsNoPreview(t *testing.T) {
	st := gatedRoom() // Write: its PendingGate carries Text and nothing else.
	got := render(st)
	for _, mark := range []string{"  - ", "  + "} {
		if strings.Contains(got, mark) {
			t.Errorf("a payload with no before/after drew a preview line %q", mark)
		}
	}
	if !strings.Contains(got, "waiting on you") {
		t.Fatal("the card itself disappeared along with the preview")
	}
	golden(t, "gate-card", got)
}

// TestIdenticalHalvesDrawNothing. HasPreview's whole test is that the halves
// DIFFER, which folds the three "nothing to show" cases into one answer — no
// payload halves, an edit that changes nothing, and (from queueGate) an edit
// whose only difference was a redacted secret. A line drawn against an
// identical line would be the card claiming a change it cannot point at.
func TestIdenticalHalvesDrawNothing(t *testing.T) {
	st := editingRoom()
	st.Gates[0].Old = "same line"
	st.Gates[0].New = "same line"
	if st.Gates[0].HasPreview() {
		t.Fatal("an edit that changes nothing claims it has a preview")
	}
	if got := render(st); strings.Contains(got, "  - same line") {
		t.Error("an edit whose halves are identical drew a diff of a line against itself")
	}
}

// TestADeletionIsAllRemovals. An empty new half beside a non-empty old one is a
// legal, measured deletion: the pair was carried and the halves differ. The
// preview is all `-` lines and NO added-side count — "0 more added lines not
// shown" would be the card filling a slot rather than answering a question.
func TestADeletionIsAllRemovals(t *testing.T) {
	st := editingRoom()
	st.Gates[0].Old = "the line that goes away"
	st.Gates[0].New = ""
	got := render(st)
	if !strings.Contains(got, "- the line that goes away") {
		t.Error("a deletion did not render its removed line")
	}
	if strings.Contains(got, "  + ") {
		t.Error("a deletion invented an added line")
	}
	if strings.Contains(got, "added line") {
		t.Error("a deletion counted an added side that does not exist")
	}
}

// TestALongEditSaysWhatItDidNotShow. The card is chrome and cannot grow without
// eating the reply underneath it, so each half is bounded — and a bound that
// did not say what it dropped would be silent clipping, which is the ambiguity
// §4a.1 forbids and which the overflow markers already refuse elsewhere.
//
// The counts are PER HALF on purpose: one total would let a long removal spend
// the whole budget and take the additions with it, with no line admitting the
// additions had ever been there.
func TestALongEditSaysWhatItDidNotShow(t *testing.T) {
	st := editingRoom()
	st.Gates[0].Old = "old 1\nold 2\nold 3\nold 4\nold 5"
	st.Gates[0].New = "new 1\nnew 2\nnew 3\nnew 4"
	got := render(st)
	for _, want := range []string{"2 more removed lines not shown", "1 more added line not shown"} {
		if !strings.Contains(got, want) {
			t.Errorf("the card never says %q", want)
		}
	}
	if strings.Contains(got, "old 4") || strings.Contains(got, "new 4") {
		t.Error("the card rendered past its own bound")
	}
	golden(t, "gate-preview-truncated", got)
}

// TestThePreviewCountsPayloadLinesNotShownLines.
//
// The number in "N more removed lines not shown" is a claim about the payload,
// so it has to be the difference between what arrived and what fits — not a
// constant, and not a count of anything the renderer produced. A half of
// exactly gatePreviewHalfLines shows everything and counts nothing; one line
// more counts exactly one.
func TestThePreviewCountsPayloadLinesNotShownLines(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines int
		want  string
	}{
		{"one under the bound", gatePreviewHalfLines - 1, ""},
		{"exactly the bound", gatePreviewHalfLines, ""},
		{"one over", gatePreviewHalfLines + 1, "1 more removed line not shown"},
		{"four over", gatePreviewHalfLines + 4, "4 more removed lines not shown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b []string
			for i := 0; i < tc.lines; i++ {
				b = append(b, "line "+itoa(i))
			}
			st := editingRoom()
			st.Gates[0].Old = strings.Join(b, "\n")
			st.Gates[0].New = "one replacement line"
			got := render(st)
			if tc.want == "" {
				if strings.Contains(got, "not shown") {
					t.Errorf("%d lines fit inside the bound and still drew a count", tc.lines)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("the card never says %q", tc.want)
			}
		})
	}
}

// TestThePreviewSurvivesEveryWidth sweeps the card carrying an edit across the
// same widths and glyph sets TestGateCardNeverExceedsTheWidth sweeps the bare
// card across — including a line far longer than any column, which is what
// forces the truncate-then-style order (fit alone would clip a styled string
// through an escape, §9.5).
func TestThePreviewSurvivesEveryWidth(t *testing.T) {
	for _, w := range []int{60, 72, 80, 95, 96, 100, 120, 160, 201} {
		for _, ascii := range []bool{false, true} {
			for _, expanded := range []bool{false, true} {
				st := editingRoom()
				st.Width, st.Height = w, 24
				st.Expanded = expanded
				st.Gates[0].Old = strings.Repeat("averylongunbrokenidentifier", 8)
				st.Gates[0].New = strings.Repeat("adifferentlongidentifier", 8)
				out := Render(st, PlainStyles(), GlyphsFor(ascii))
				for i, line := range strings.Split(out, "\n") {
					if got := lipgloss.Width(line); got > w {
						t.Errorf("w=%d ascii=%v expanded=%v: line %d is %d cells: %q",
							w, ascii, expanded, i, got, line)
					}
				}
				if n := len(strings.Split(out, "\n")); n > 24 {
					t.Errorf("w=%d ascii=%v expanded=%v: frame is %d lines, terminal is 24",
						w, ascii, expanded, n)
				}
			}
		}
	}
}

// TestThePreviewKeepsTheKeysOnTheCard. The two keys that unblock a vendor are
// the reason the card exists; a preview that pushed them out of the chrome
// would have spent the user's attention on the evidence and taken away the
// verdict.
func TestThePreviewKeepsTheKeysOnTheCard(t *testing.T) {
	st := editingRoom()
	st.Gates[0].Old = strings.Repeat("removed line\n", 40)
	st.Gates[0].New = strings.Repeat("added line\n", 40)
	got := render(st)
	if !strings.Contains(got, "y approve") || !strings.Contains(got, "n deny") {
		t.Error("a long edit displaced the gate keys from the card")
	}
}

// TestThePreviewIsChromeNotBody, the gate card's own property applied to the
// part of it that can be several lines long: a vendor is STOPPED behind this,
// and during a turn every column follows its own tail, so a preview in the body
// would be pushed off by the output of the very call it is asking about.
func TestThePreviewIsChromeNotBody(t *testing.T) {
	st := editingRoom()
	st.Columns[0].Body = strings.Repeat("a long streamed reply that pushes everything up ", 40)
	st.Columns[0].Follow = true
	if got := render(st); !strings.Contains(got, "- func gateCard() {") {
		t.Error("the preview scrolled away under the vendor's own output")
	}
}

// TestThePreviewCostsScrollCeiling. Chrome costs body lines, and MaxScroll
// derives the ceiling from the chrome it actually renders rather than from a
// constant — so a card that grew a preview must move the ceiling with it, or
// the tail scrolls past the end of the content and shows blank cells where the
// newest output should be.
func TestThePreviewCostsScrollCeiling(t *testing.T) {
	st := editingRoom()
	st.Columns[0].Body = strings.Repeat("line of the reply\n", 40)

	with := MaxScroll(st, 0)
	st.Gates[0].Old, st.Gates[0].New = "", ""
	without := MaxScroll(st, 0)
	if with <= without {
		t.Errorf("scroll ceiling with a preview = %d, without = %d; the preview must cost body lines",
			with, without)
	}
}

// TestAddedAndRemovedAreDifferentInColourToo — word first, colour second, but
// the two colours must not collide either, or a reader relying on the hue and a
// reader relying on the prefix would be told different things. The card spends
// ForDiffLine, the same classifier §9.37's raw patch lines go through, which is
// how council adds no hue for this.
func TestAddedAndRemovedAreDifferentInColourToo(t *testing.T) {
	sty := NewStyles(true)
	if sty.ForDiffLine("- gone").Render("x") == sty.ForDiffLine("+ new").Render("x") {
		t.Fatal("a removed line and an added line are styled identically")
	}
	if got := PlainStyles().ForDiffLine("- gone").Render("- gone"); got != "- gone" {
		t.Errorf("PlainStyles is not the identity set for a preview line: %q", got)
	}
}

// TestContentThatLooksLikeAPatchHeaderIsStillContent.
//
// ForDiffLine matches `---`, `+++` and `@@` as headers before it matches the
// change markers — the right rule for §9.37's raw patches, and a trap here: an
// edit that removes a line whose own text begins with `--` would compose to
// `---…` and be painted as chrome, so a removal would render as if it were not
// one. The space after the mark is what makes that unreachable, and this is the
// assertion that keeps it there.
//
// It checks the STYLE rather than the bytes, because under PlainStyles every
// branch is the identity function and a golden could not see this at all — the
// same blindness §9.5's padRight trap turns on.
func TestContentThatLooksLikeAPatchHeaderIsStillContent(t *testing.T) {
	sty := NewStyles(true)
	for _, tc := range []struct {
		mark, text string
		want       lipgloss.Style
	}{
		{"-", "--- a/file.go", sty.SevCrit},
		{"-", "--pretty=oneline", sty.SevCrit},
		{"+", "+++ b/file.go", sty.SevOK},
		{"+", "++i;", sty.SevOK},
		{"-", "@@ -1,3 +1,4 @@", sty.SevCrit},
		{"-", "", sty.SevCrit},
	} {
		t.Run(tc.mark+tc.text, func(t *testing.T) {
			body := strings.TrimRight(tc.mark+" "+tc.text, " ")
			if got := sty.ForDiffLine(body).Render("x"); got != tc.want.Render("x") {
				t.Errorf("%q is styled as chrome or as the wrong side of the edit", body)
			}
		})
	}
}

// TestTheCursorSeatRendersNoPreview. The ACP permission request carries a title
// and a kind and neither half of an edit (§9.36's capture), so its PendingGate
// leaves both halves empty and the card says only what was measured. Stated as
// a test because "this vendor shows less" is exactly the kind of claim that
// rots quietly when someone later fills a field to make a surface look uniform.
func TestTheCursorSeatRendersNoPreview(t *testing.T) {
	p := PendingGate{Vendor: model.VendorCursor, RequestID: "acp-perm-1",
		ToolUseID: "call-1", Text: "execute: `mkdir zzz`"}
	if p.HasPreview() {
		t.Fatal("a gate with no measured halves claims it has a preview")
	}
	if lines := gatePreview(p, 40, PlainStyles(), GlyphsFor(false)); lines != nil {
		t.Errorf("gatePreview drew %d lines for a payload that carried none: %q", len(lines), lines)
	}
}
