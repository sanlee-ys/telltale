package svgframe

import (
	"strings"
	"testing"
)

// The pictures this package emits are the most public thing the project has, so
// the properties asserted here are the ones a reader would be misled by if they
// were wrong: that every character of the frame survives, that the columns are
// pinned rather than left to a font, and that a style this package does not
// understand stops the build instead of disappearing.

// TestEachRunPinsItsOwnColumn. A monospace font-family is a request, not a
// guarantee: box-drawing runes routinely fall through the stack to whatever has
// them, and one substituted glyph at a different advance would shear every
// column to its right. Each run therefore states its own x and its own
// textLength, so alignment does not depend on the font that resolves.
func TestEachRunPinsItsOwnColumn(t *testing.T) {
	out := mustRender(t, Frame{Caption: "c", Alt: "a", Lines: []string{
		"ab\x1b[36mcd\x1b[m",
	}}, Dark())

	for _, want := range []string{
		`<tspan x="21" textLength="18"`, // "ab" at column 0
		`<tspan x="39" textLength="18"`, // "cd" at column 2
		`lengthAdjust="spacingAndGlyphs"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// A picture GitHub renders must fetch nothing: no @font-face, no external
// stylesheet, no remote image. This is a property of the output, not of the
// author's intentions, so it is asserted on the bytes.
func TestOutputIsSelfContained(t *testing.T) {
	out := mustRender(t, Frame{Caption: "telltale hud", Alt: "a", Lines: []string{"x"}}, Light())
	// The SVG namespace is a name, not an address — nothing resolves it. Every
	// other URL in the output would be a fetch.
	body := strings.ReplaceAll(out, `xmlns="http://www.w3.org/2000/svg"`, "")
	for _, forbidden := range []string{"http://", "https://", "@font-face", "<image", "url("} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the picture reaches outside itself: %q", forbidden)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "<!--") || !strings.Contains(out, "<svg ") {
		t.Error("the picture does not open with the generated-file notice and an svg root")
	}
}

// Markup in a frame is text, not markup. Nothing telltale renders contains an
// ampersand today; a vendor's reply quoted into a council column could.
func TestFrameTextIsEscaped(t *testing.T) {
	out := mustRender(t, Frame{Caption: "a & b", Alt: "<alt>", Lines: []string{"go build ./... && echo <ok>"}}, Dark())
	if !strings.Contains(out, "go build ./... &amp;&amp; echo &lt;ok&gt;") {
		t.Errorf("frame text was not escaped:\n%s", out)
	}
	if !strings.Contains(out, "&lt;alt&gt;") || !strings.Contains(out, "a &amp; b") {
		t.Errorf("the title or the caption was not escaped:\n%s", out)
	}
}

// The four-bit indices are what internal/theme spends and what council's seat
// hues spend (§9.28), so each has to land on its scheme's own colour rather
// than on a hue this package picked.
func TestIndicesResolveThroughTheScheme(t *testing.T) {
	dark, light := Dark(), Light()
	cases := []struct {
		sgr string
		idx int
	}{
		{"\x1b[32m", 2},  // theme.ColorOK
		{"\x1b[36m", 6},  // theme.ColorIdentity
		{"\x1b[94m", 12}, // council's cursor seat hue, the bright-fg form
		{"\x1b[38;5;5m", 5},
	}
	for _, c := range cases {
		for _, p := range []Palette{dark, light} {
			out := mustRender(t, Frame{Caption: "c", Alt: "a", Lines: []string{c.sgr + "x\x1b[m"}}, p)
			if !strings.Contains(out, `fill="`+p.ANSI[c.idx]+`"`) {
				t.Errorf("%s in the %s scheme did not resolve to index %d (%s)", c.sgr, p.Name, c.idx, p.ANSI[c.idx])
			}
		}
	}
}

// Nothing this product draws with may vanish into the paper.
//
// The failure this catches is real and was live: the first light scheme tried
// here was One Half Light, whose index 7 is #fafafa — its own background. Index
// 7 is theme.ColorTrackLite, the gauge track, so the light picture rendered a
// row at 0% context as a row with nothing in the CONTEXT cell, which is exactly
// what a row with no context source renders as. Zero and absent, collapsed into
// one cell, in the most public picture of a product whose first honesty rule is
// that they are different states.
//
// 1.4 is a floor, not a legibility standard — see Contrast.
func TestNoSpentColourVanishes(t *testing.T) {
	const floor = 1.4
	for _, p := range []Palette{Dark(), Light()} {
		for _, i := range Spent {
			if c := Contrast(p.ANSI[i], p.Background); c < floor {
				t.Errorf("%s: index %d (%s) is %.2f:1 against the background (%s); a colour this product spends must clear %.1f:1",
					p.Name, i, p.ANSI[i], c, p.Background, floor)
			}
		}
		if c := Contrast(p.Foreground, p.Background); c < 4.5 {
			t.Errorf("%s: the default foreground is %.2f:1 against the background, below WCAG AA for body text", p.Name, c)
		}
	}
}

// Weight and intensity are signals in their own right in this UI — council
// spends bold on a seat's name and on a posture badge saying a seat may change
// your files, and faint on the columns the keys do not move (§9.27). A picture
// that dropped either would be dropping the distinction, not the decoration.
func TestWeightAndIntensitySurvive(t *testing.T) {
	out := mustRender(t, Frame{Caption: "c", Alt: "a", Lines: []string{
		"\x1b[1;33mWRITES\x1b[m \x1b[2mchrome\x1b[m plain",
	}}, Dark())
	if !strings.Contains(out, `font-weight="bold"`) {
		t.Error("bold was dropped")
	}
	if !strings.Contains(out, `fill-opacity="`+faintOpacity+`">chrome`) {
		t.Errorf("faint was dropped:\n%s", out)
	}
	if strings.Contains(out, `font-weight="bold">chrome`) {
		t.Error("faint text was drawn bold")
	}
}

// The loud failure. A surface that grows a style this package cannot draw must
// break the build here — the alternative is a hero image that quietly stops
// showing a distinction the terminal makes, which is the exact failure mode the
// generated pictures exist to end.
func TestUnknownStyleIsAnError(t *testing.T) {
	for _, line := range []string{
		"\x1b[7mreverse\x1b[m",          // reverse video
		"\x1b[4munderline\x1b[m",        // underline
		"\x1b[41mbackground\x1b[m",      // a background colour
		"\x1b[38;5;200mxterm256\x1b[m",  // outside the four-bit palette
		"\x1b[38;2;255;0mshort\x1b[m",   // truecolor with a channel missing
		"\x1b[38;2;255;0;300mbig\x1b[m", // a channel outside a byte
		"\x1b[1Kbad",                    // a CSI sequence that is not SGR at all
	} {
		if _, err := Render(Frame{Caption: "c", Alt: "a", Lines: []string{line}}, Dark()); err == nil {
			t.Errorf("%q rendered without complaint; it must not", line)
		}
	}
}

// TestTruecolorIsDrawnVerbatim. A truecolor run is accepted now, and the triple
// reaches the picture unchanged rather than being snapped to a palette entry.
//
// It stopped being an error when internal/council took its own ink set
// (style.go's MONOGRAPH palette): a full-screen room that inherited eight
// primaries from whatever scheme was loaded could not have an identity of its
// own. The picture's job is unchanged — show what the terminal shows — and what
// a terminal shows for 38;2 is that exact colour on every scheme, so there is
// nothing to resolve.
func TestTruecolorIsDrawnVerbatim(t *testing.T) {
	out := render(t, "\x1b[38;2;255;190;119mcopper\x1b[m plain")
	if !strings.Contains(out, `fill="#ffbe77"`) {
		t.Errorf("the truecolor run did not carry its own triple:\n%s", out)
	}
	if !strings.Contains(out, `fill="`+Dark().Foreground+`">`+" plain") {
		t.Errorf("the run after the reset did not return to the scheme's foreground:\n%s", out)
	}
	// Weight travels with it: council spends bold on a measured value, and a
	// picture that dropped it would lose the room's loudest typographic signal.
	bold := render(t, "\x1b[1;38;2;236;228;213m$0.0123\x1b[m")
	if !strings.Contains(bold, `font-weight="bold"`) || !strings.Contains(bold, `fill="#ece4d5"`) {
		t.Errorf("a bold truecolor run lost weight or ink:\n%s", bold)
	}
}

// render is Render on one line, failing the test rather than returning an error.
func render(t *testing.T, line string) string {
	t.Helper()
	got, err := Render(Frame{Caption: "c", Alt: "a", Lines: []string{line}}, Dark())
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

// Cell counting is what every x coordinate is derived from, and a rune counted
// twice would shear the row. Trailing spaces count: they are what a padded
// frame is made of.
func TestCellsAreCountedNotBytes(t *testing.T) {
	// Box drawing, an eighth-block, the focus rail and the caret — multi-byte,
	// single-cell, and the runes the pictures are mostly made of.
	runs, cells, err := parse("▌ ▸ ━█▎ ")
	if err != nil {
		t.Fatal(err)
	}
	if cells != 8 {
		t.Errorf("counted %d cells, want 8", cells)
	}
	if len(runs) != 1 || runs[0].col != 0 {
		t.Errorf("an unstyled line is one run at column 0, got %+v", runs)
	}
}

func mustRender(t *testing.T, f Frame, p Palette) string {
	t.Helper()
	out, err := Render(f, p)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
