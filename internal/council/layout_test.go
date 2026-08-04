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
			total := 2 + (lay.Cols-1)*(1+2*gutter) // pads + separators and gutters
			for i := 0; i < lay.Cols; i++ {
				total += lay.ColWidth + lay.extraFor(i)
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
