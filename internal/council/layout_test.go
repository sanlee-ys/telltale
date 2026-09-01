package council

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestTierIsAPureFunctionOfWidth(t *testing.T) {
	cases := []struct {
		width, cols int
		want        Tier
	}{
		{59, 3, TierFloor},
		{60, 3, TierTabs},
		{95, 3, TierTabs},
		{96, 3, TierColumns},
		{200, 3, TierColumns},
		// One seated vendor is never three columns, however wide the terminal.
		{200, 1, TierTabs},
		{200, 0, TierTabs},
	}
	for _, c := range cases {
		if got := tierFor(c.width, c.cols, false); got != c.want {
			t.Errorf("tierFor(%d, %d) = %v, want %v", c.width, c.cols, got, c.want)
		}
	}
}

// TestColumnsExactlyFillTheWidth is the arithmetic the side-by-side join
// depends on. If the columns plus their separators do not add up to the
// terminal width, every body row is either short (ragged edge) or long (wraps,
// and the grid shears).
func TestColumnsExactlyFillTheWidth(t *testing.T) {
	for w := columnsBreak; w <= 220; w++ {
		for n := 2; n <= 4; n++ {
			lay := resolveLayout(w, 24, n, false)
			if lay.Tier != TierColumns {
				continue
			}
			total := 2*framePad + (lay.Cols-1)*(1+2*gutter) // pads + separators and gutters
			for i := 0; i < lay.Cols; i++ {
				total += lay.widthAt(i)
			}
			if total != w {
				t.Fatalf("w=%d n=%d: columns total %d cells, want %d", w, n, total, w)
			}
		}
	}
}

// TestColumnsFallBackToTabsRatherThanShredding: three unreadably narrow
// columns are worse than one readable one, so the tier drops instead.
func TestColumnsFallBackToTabsRatherThanShredding(t *testing.T) {
	for w := MinWidth; w <= 300; w++ {
		lay := resolveLayout(w, 24, 3, false)
		if lay.Tier == TierColumns && lay.ColWidth < minColumn {
			t.Fatalf("w=%d: seated 3 columns at %d cells each, below the %d floor",
				w, lay.ColWidth, minColumn)
		}
	}
}

func TestBodyRowsNeverGoNegative(t *testing.T) {
	for h := 0; h <= 40; h++ {
		for _, w := range []int{60, 96, 120} {
			if got := resolveLayout(w, h, 3, false).Body; got < 1 {
				t.Errorf("w=%d h=%d: Body = %d", w, h, got)
			}
		}
	}
}

// TestWrapNeverExceedsTheWidth is a property test over the shapes vendor output
// actually takes: ordinary prose, an unbreakable token longer than the column,
// CJK (two cells per rune), and existing paragraphing.
func TestWrapNeverExceedsTheWidth(t *testing.T) {
	inputs := []string{
		"the quick brown fox jumps over the lazy dog",
		strings.Repeat("x", 200),
		"short " + strings.Repeat("unbreakabletoken", 12) + " tail",
		"https://example.com/a/very/long/path/that/will/not/break/on/spaces/at/all",
		"日本語のテキストはセル幅が二倍になるので折り返し計算が違います",
		"para one\n\npara two with more words in it\nand a third line",
		"",
		"   leading and trailing   ",
	}
	// From 2: at w == 1 a double-width rune cannot be represented, which is
	// wrap's documented exception and is unreachable in a real frame (minColumn
	// is 24). The frame-level sweep covers what actually ships.
	for _, w := range []int{2, 3, 8, 20, 24, 37, 60} {
		for _, in := range inputs {
			for _, line := range wrap(in, w) {
				if got := lipgloss.Width(line); got > w {
					t.Errorf("wrap(w=%d): line is %d cells: %q", w, got, line)
				}
			}
		}
	}
}

// TestWrapKeepsEveryRune: wrapping may re-break lines, but it must not eat
// content. A renderer that silently drops the tail of a reply is exactly the
// failure this product exists to refuse.
func TestWrapKeepsEveryRune(t *testing.T) {
	inputs := []string{
		"the quick brown fox jumps over the lazy dog",
		strings.Repeat("x", 137),
		"short " + strings.Repeat("unbreakabletoken", 5) + " tail",
		"日本語のテキスト",
	}
	for _, w := range []int{3, 8, 20, 60} {
		for _, in := range inputs {
			got := strings.ReplaceAll(strings.Join(wrap(in, w), ""), " ", "")
			want := strings.ReplaceAll(in, " ", "")
			if got != want {
				t.Errorf("wrap(%q, %d) lost content:\n got %q\nwant %q", in, w, got, want)
			}
		}
	}
}

func TestTruncateAndFitMeasureDisplayWidth(t *testing.T) {
	g := GlyphsFor(false)
	// A CJK string is 2 cells per rune: a len()-based implementation would pass
	// a naive test and shear a real terminal.
	if got := lipgloss.Width(padRight("日本語", 10, g)); got != 10 {
		t.Errorf("padRight CJK = %d cells, want 10", got)
	}
	if got := lipgloss.Width(truncate("日本語テキスト", 5, g.Ellipsis)); got > 5 {
		t.Errorf("truncate CJK = %d cells, want <= 5", got)
	}
	if got := lipgloss.Width(fit("日本語テキスト", 6)); got != 6 {
		t.Errorf("fit CJK = %d cells, want 6", got)
	}
}

// TestFitIsANSIAware is the regression guard for the bug padRight would have
// had here: it truncates rune by rune, so on pre-styled text it would cut
// through an escape sequence and count escape bytes as content. Goldens render
// with PlainStyles and would never catch it.
func TestFitIsANSIAware(t *testing.T) {
	sty := NewStyles(true)
	styled := sty.Identity.Render("Claude") + " " + sty.SevOK.Render("done")
	out := fit(styled, 20)
	if got := lipgloss.Width(out); got != 20 {
		t.Errorf("fit on styled text = %d cells, want 20", got)
	}
	if !strings.Contains(out, "Claude") {
		t.Error("fit dropped visible content from styled text")
	}
}

func TestElideLeftKeepsTheTail(t *testing.T) {
	got := elideLeft("/home/dev/code/telltale/internal/council", 20, "…")
	if lipgloss.Width(got) > 20 {
		t.Errorf("elideLeft = %d cells, want <= 20", lipgloss.Width(got))
	}
	if !strings.HasSuffix(got, "council") {
		t.Errorf("elideLeft dropped the informative tail: %q", got)
	}
}

// TestPaneWidthsHoldTheirFloors is §9.51's half of the arithmetic
// TestColumnsExactlyFillTheWidth pins for the unbiased frame.
//
// Two invariants, over every width the columns tier draws at, every pane count
// it draws, and every bias one press of a resize key can write. They are the two
// ways a pane splitter breaks a grid: a row that does not add up shears the
// frame, and a pane under stripColumn cannot say the two things a strip exists
// to say (§9.18).
//
// The bias is swept BEYOND what the keys will write, deliberately. Render is
// pure over State, so State is an input this package does not control: a test
// types one out by hand, and a stale bias survives a seat folding out of the
// grid. An invariant that held only because paneResize was careful is one the
// goldens could break by accident.
func TestPaneWidthsHoldTheirFloors(t *testing.T) {
	for w := columnsBreak; w <= 220; w++ {
		for n := 2; n <= 4; n++ {
			for _, step := range []int{-4 * paneStep, -paneStep, 0, paneStep, 4 * paneStep, 999} {
				for at := 0; at < n; at++ {
					bias := make([]int, n)
					bias[at] = step
					bias[(at+1)%n] = -step
					lay := resolveLayoutIn(layoutInput{
						Width: w, Height: 24, Cols: n, Bias: bias,
					})
					if lay.Tier != TierColumns {
						continue
					}
					total := 2*framePad + (lay.Cols-1)*(1+2*gutter)
					for i := 0; i < lay.Cols; i++ {
						got := lay.widthAt(i)
						if got < stripColumn {
							t.Fatalf("w=%d n=%d bias=%v: pane %d is %d cells, below the %d floor",
								w, n, bias, i, got, stripColumn)
						}
						total += got
					}
					if total != w {
						t.Fatalf("w=%d n=%d bias=%v: panes total %d cells, want %d",
							w, n, bias, total, w)
					}
				}
			}
		}
	}
}

// TestAnUnbiasedFrameIsUntouched. The pane arithmetic is applied OVER a finished
// apportionment rather than mixed into the division, specifically so the room as
// it was before §9.51 renders as it did — which is what makes ~89 goldens taken
// before this feature still correct claims about the frame.
func TestAnUnbiasedFrameIsUntouched(t *testing.T) {
	for w := columnsBreak; w <= 220; w++ {
		for n := 2; n <= 4; n++ {
			for _, bias := range [][]int{nil, make([]int, n), {}} {
				in := layoutInput{Width: w, Height: 24, Cols: n}
				want := resolveLayoutIn(in)
				in.Bias = bias
				got := resolveLayoutIn(in)
				if got.ColWidth != want.ColWidth || len(got.ColWidths) != len(want.ColWidths) {
					t.Fatalf("w=%d n=%d bias=%v: %+v, want %+v", w, n, bias, got, want)
				}
				for i := 0; i < want.Cols; i++ {
					if got.widthAt(i) != want.widthAt(i) {
						t.Fatalf("w=%d n=%d bias=%v: pane %d is %d cells, want %d",
							w, n, bias, i, got.widthAt(i), want.widthAt(i))
					}
				}
			}
		}
	}
}

// TestNormalizeBiasSumsToZero. A bias that does not sum to zero is a row that
// overflows the terminal or leaves a ragged edge, and the keys are not the only
// thing that can write one: a seat folding out of the grid takes half of a pair
// with it.
func TestNormalizeBiasSumsToZero(t *testing.T) {
	for _, in := range [][]int{
		{5}, {5, 0}, {5, 5, 5}, {-7, 2}, {100, -1, -1}, {0, 0, 3},
	} {
		b := append([]int(nil), in...)
		if !normalizeBias(b) {
			t.Fatalf("%v: reported no bias", in)
		}
		sum := 0
		for _, v := range b {
			sum += v
		}
		if sum != 0 {
			t.Errorf("normalizeBias(%v) = %v, sums to %d", in, b, sum)
		}
	}
	// An empty or all-zero bias reports false, which is what routes the frame
	// back down the untouched path above.
	for _, in := range [][]int{nil, {}, {0}, {0, 0, 0}} {
		if normalizeBias(append([]int(nil), in...)) {
			t.Errorf("normalizeBias(%v) claimed a bias", in)
		}
	}
}
