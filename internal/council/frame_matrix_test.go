package council

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The frame matrix sweeps every room shape this package can render across every
// terminal it claims to support, and asserts the one property the whole surface
// rests on: the frame fits.
//
// A torn frame is the honest-gauge rule failing at the layer below the numbers.
// A gauge that cannot tell absent from zero is the bug this project exists to
// prevent; a line that runs past the terminal and wraps takes the whole row it
// wrapped into with it, so a reader is looking at a frame whose alignment is a
// lie — every column after the tear is one row out of step with its header.
//
// TestGateCardNeverExceedsTheWidth already made this assertion, and made it for
// one state. That is the shape of the problem: the states nobody thought to
// stress are exactly the ones that tear, because the ones under suspicion got a
// test the day they were suspected. This sweeps the lot.
//
// UNBREAKABLE TOKENS ARE THE POINT. Prose wraps at spaces and hides the bug;
// what tears a frame is a path, a URL, a git sha or a stack frame with nowhere
// to break, which is most of what a vendor actually emits.

// matrixWidths spans the narrow floor, the awkward middles where a four-way
// split leaves a remainder, and a terminal wider than anyone runs.
var matrixWidths = []int{60, 72, 80, 96, 100, 120, 160, 201}

// matrixHeights are the short frame the rest of the corpus pins, a common
// window, and the tall one that started the ceiling program.
var matrixHeights = []int{24, 40, 60}

// unbreakable is a token with nowhere to wrap. Sized past any single column so
// it has to be truncated rather than merely folded.
const unbreakable = "C:\\Users\\dev\\code\\telltale\\internal\\council\\testdata\\golden\\a-file-with-a-very-long-name.txt"

func matrixRooms() map[string]func() State {
	return map[string]func() State{
		"idle": func() State { return room() },

		"streaming-long": func() State {
			st := room()
			st.Columns[0].Phase = PhaseStreaming
			st.Columns[0].Body = unbreakable + " " + strings.Repeat("word ", 40) + unbreakable
			st.Columns[1].Phase = PhaseWaiting
			return st
		},

		"every-phase": func() State {
			st := room()
			st.Columns[0].Phase = PhaseDone
			st.Columns[0].Body = strings.Repeat(unbreakable+"\n", 6)
			st.Columns[1].Phase = PhaseFailed
			st.Columns[2].Phase = PhaseCancelled
			return st
		},

		"unavailable": func() State {
			st := room()
			st.Columns[1].Avail = AvailNotInstalled
			st.Columns[2].Avail = AvailUnusable
			return st
		},

		"notice-long": func() State {
			st := room()
			st.Notice = "reattached from " + unbreakable + " — turn 109 was the last, 3/3 seats restored"
			return st
		},

		"draft-long": func() State {
			st := room()
			st.Mode = ModeComposing
			st.Draft = unbreakable + " " + unbreakable
			return st
		},

		"gated-long": func() State {
			st := room()
			st.Gates = []PendingGate{{
				Vendor: model.VendorClaude, RequestID: "r1", ToolUseID: "t1",
				Text: "Bash: " + unbreakable,
			}}
			return st
		},
	}
}

// TestMatrixRoomsAreNotDegenerate guards the sweep against passing vacuously.
//
// A state builder that quietly fails to set the thing it names — a gate that
// needs a posture it was not given, a mode that needs a draft — still renders,
// still fits, and still reports ok. The sweep would then be asserting the
// width of the idle room eight times under seven names. So each room has to
// differ from idle at a reference geometry before its result means anything.
func TestMatrixRoomsAreNotDegenerate(t *testing.T) {
	base := room()
	base.Width, base.Height = 120, 24
	idle := Render(base, PlainStyles(), GlyphsFor(false))

	for name, build := range matrixRooms() {
		if name == "idle" {
			continue
		}
		st := build()
		st.Width, st.Height = 120, 24
		if Render(st, PlainStyles(), GlyphsFor(false)) == idle {
			t.Errorf("%s renders identically to idle — the builder is not setting what it names, "+
				"so TestFrameNeverTears is measuring the idle room under another name", name)
		}
	}
}

// TestFrameNeverTears is the sweep.
//
// Two assertions, and the second is not redundant with the first: a frame can
// fit every line and still hand the terminal more rows than it has, which
// scrolls the top of the room off screen and takes the header with it.
func TestFrameNeverTears(t *testing.T) {
	for name, build := range matrixRooms() {
		for _, w := range matrixWidths {
			for _, h := range matrixHeights {
				for _, ascii := range []bool{false, true} {
					for _, expanded := range []bool{false, true} {
						st := build()
						st.Width, st.Height = w, h
						st.Expanded = expanded
						out := Render(st, PlainStyles(), GlyphsFor(ascii))

						lines := strings.Split(out, "\n")
						for i, line := range lines {
							if got := lipgloss.Width(line); got > w {
								t.Errorf("%s w=%d h=%d ascii=%v expanded=%v: line %d is %d cells: %q",
									name, w, h, ascii, expanded, i, got, line)
							}
						}
						if len(lines) > h {
							t.Errorf("%s w=%d h=%d ascii=%v expanded=%v: frame is %d lines, terminal is %d",
								name, w, h, ascii, expanded, len(lines), h)
						}
					}
				}
			}
		}
	}
}
