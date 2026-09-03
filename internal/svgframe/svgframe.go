// Package svgframe turns a rendered terminal frame into an SVG picture of that
// frame. It is how README.md's hero images are produced, and the reason they
// cannot drift from the product again.
//
// The images used to be hand-drawn mockups. By the time PR #125 landed, the
// council picture was showing three focus marks at once — the ambiguous-focus
// bug §9.12 exists to have fixed — a room with no seat keys, no focus rail and
// no posture word, and a "Cost: $0.0034" line reading a dollar figure the
// codebase deliberately refuses to derive (ADR-001, design.md §4a.1). A picture
// a human maintains is a claim with no gate on it, and this one had gone stale
// in all three directions at once. So the picture is now the frame: the same
// State the golden pins, rendered through the same Render, with only the
// terminal's own chrome drawn around it.
//
// # Why this package is stdlib-only, and why it takes bytes rather than a State
//
// It takes an already-rendered string carrying ANSI SGR, not a council.State or
// a hud.State. That keeps one converter serving both surfaces without either
// package's types leaking into the other, and it keeps this package free of
// lipgloss — which matters because the alternative sites both fail: internal/theme
// must stay stdlib-only so the statusline binary never links the Charm stack
// (ADR-002), and a converter living inside internal/council could not draw the
// HUD.
//
// # Why the palettes are hex here and stay ANSI indices in internal/theme
//
// theme.go argues, correctly, that a hex triple would be telltale asserting a
// colour over the scheme the user already chose. An SVG has no terminal to ask.
// Pinning a scheme is therefore a property of the PICTURE, not of the palette,
// and it lives here rather than being smuggled into theme. Both schemes are
// real, shipped Windows Terminal schemes — Campbell and Tango Light — which is
// the same "Windows is the primary target" reasoning as ADR-002 rather than a
// hue somebody liked. Which light scheme was not a free choice; see Light.
package svgframe

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// The type metrics every emitted picture uses.
//
// Advance is the character cell width, and it is NOT measured from the font:
// which font resolves on a reader's machine is unknown, and box-drawing runes
// routinely fall through the stack to whatever has them. Every run is emitted
// with an explicit textLength of its cell count times this number, so the
// columns line up under a font this package never saw. 0.6em is the advance of
// every font in the stack below; the textLength is what makes being slightly
// wrong about that harmless.
//
// The size is picked so that every cell origin is a whole number of pixels:
// 0.6em of 15px is exactly 9. At 14px the advance is 8.4, every cell after the
// first starts on a subpixel, and a row of `━` picks up a lighter seam at each
// join — a horizontal rule that arrives on the page as a dotted one. That was
// measured on the emitted file, not reasoned about.
const (
	FontSize   = 15.0
	Advance    = 9.0
	LineHeight = 18.0

	padX      = 21.0
	padY      = 15.0
	headerH   = 36.0
	cornerRad = 10.0

	// FontStack is local-only on purpose: an image GitHub renders must fetch
	// nothing, so there is no @font-face and no remote reference anywhere in
	// the output.
	FontStack = "'JetBrains Mono','Fira Code','Cascadia Mono',Consolas,'DejaVu Sans Mono',monospace"
)

// Palette is one terminal colour scheme, resolved to hex.
//
// ANSI holds indices 0–15 so that internal/theme's indices — and council's seat
// hues, which are indices 4, 5, 6 and 12 (§9.28) — resolve to the same relative
// colours a terminal would give them.
type Palette struct {
	Name       string
	Background string
	Foreground string
	Rule       string // the panel border and the header separator
	Header     string // the caption strip behind the command name
	ANSI       [16]string
}

// Dark is Windows Terminal's Campbell, its default scheme.
func Dark() Palette {
	return Palette{
		Name:       "dark",
		Background: "#0c0c0c",
		Foreground: "#cccccc",
		Rule:       "#3a3a3a",
		Header:     "#1b1b1b",
		ANSI: [16]string{
			"#0c0c0c", "#c50f1f", "#13a10e", "#c19c00",
			"#0037da", "#881798", "#3a96dd", "#cccccc",
			"#767676", "#e74856", "#16c60c", "#f9f1a5",
			"#3b78ff", "#b4009e", "#61d6d6", "#f2f2f2",
		},
	}
}

// Light is Windows Terminal's Tango Light.
//
// Chosen over the other light schemes it ships for one measured reason: index 7
// is the gauge track on a light background (theme.ColorTrackLite), and in One
// Half Light and Solarized Light index 7 IS the background — #fafafa on #fafafa,
// #eee8d5 on #fdf6e3. The track would have rendered invisible, which is not a
// cosmetic loss: an empty track and no track at all are the two halves of
// zero-vs-absent, the one regression this repo exists to prevent (ADR-001), and
// a light picture that dropped the track would have collapsed them where the
// terminal keeps them apart. Tango Light's index 7 is #d3d7cf on white, quiet
// and present, which is what a track is meant to be. TestNoSpentColourVanishes
// is the gate.
func Light() Palette {
	return Palette{
		Name:       "light",
		Background: "#ffffff",
		Foreground: "#000000",
		Rule:       "#c9ccc6",
		Header:     "#f1f2ef",
		ANSI: [16]string{
			"#000000", "#cc0000", "#4e9a06", "#c4a000",
			"#3465a4", "#75507b", "#06989a", "#d3d7cf",
			"#555753", "#ef2929", "#8ae234", "#fce94f",
			"#729fcf", "#ad7fa8", "#34e2e2", "#eeeeec",
		},
	}
}

// Spent lists the palette indices telltale actually renders with: internal/theme's
// identity hue, its severity ramp and its two gauge tracks, plus the four seat
// hues council spends (§9.28). Indices outside this set are in a Palette because
// a scheme has sixteen of them, not because anything draws with them — index 0
// is a scheme's own background, and asserting anything about its legibility
// would be asserting the wrong thing.
var Spent = []int{
	1, 2, 3, // theme.ColorCrit / ColorOK / ColorWarn
	4, 5, 6, 12, // council's seat hues; 6 is also theme.ColorIdentity
	7, 8, // theme.ColorTrackLite / ColorTrackDark
}

// Contrast is the WCAG contrast ratio between a palette entry and its
// background, on the 1–21 scale.
//
// It exists for one assertion — that nothing this product draws with vanishes
// into the paper — and it is honest about being no more than that. A ratio of
// 1.4 is not readable body text; it is the difference between a gauge track a
// reader can see and one that is not there. Legibility of the coloured VALUES
// is carried where this UI always carries it: by the word beside them.
func Contrast(hex, background string) float64 {
	a, b := luminance(hex), luminance(background)
	if a < b {
		a, b = b, a
	}
	return (a + 0.05) / (b + 0.05)
}

func luminance(hex string) float64 {
	v, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	if err != nil {
		return 0
	}
	ch := func(shift uint) float64 {
		c := float64((v>>shift)&0xff) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(16) + 0.7152*ch(8) + 0.0722*ch(0)
}

// faintOpacity is how a terminal's SGR 2 is drawn. Faint is de-emphasis in this
// UI — chrome, rules, labels, and a column the keys do not move (§9.27) — so it
// is drawn as less ink rather than as a different hue, which is what a terminal
// does and what keeps the distinction from colliding with a severity.
const faintOpacity = "0.55"

// Frame is one picture: a caption naming the command that produced it, and the
// frame that command rendered.
//
// Lines carry ANSI SGR. Anything the picture shows must have come through them,
// which is the whole point: Caption is a command name, and there is nowhere in
// this struct to put a number.
type Frame struct {
	// Caption is the command whose output Lines is. It names how to reproduce
	// the picture; it is not a value read from anywhere.
	Caption string
	// Alt is the picture's accessible description.
	Alt   string
	Lines []string
}

// Render emits the picture.
//
// It fails rather than guessing: an SGR parameter this package does not
// understand is an error, not a dropped attribute. A style added to council or
// the HUD later must show up as a build failure here rather than silently
// vanishing from the most public picture of the product.
//
// The panel is sized to the widest line rather than to a width the caller
// declares. Neither renderer pads every line to the terminal's full width —
// council's rules run the whole way and the HUD's do not, and the HUD's pad row
// is empty rather than blank-filled — so a declared width would be a number
// this package would then have to be right about.
func Render(f Frame, p Palette) ([]byte, error) {
	if len(f.Lines) == 0 {
		return nil, fmt.Errorf("svgframe: no lines")
	}

	rows := make([][]run, 0, len(f.Lines))
	width := 0
	for i, line := range f.Lines {
		rs, cells, err := parse(line)
		if err != nil {
			return nil, fmt.Errorf("svgframe: line %d: %w", i+1, err)
		}
		if cells > width {
			width = cells
		}
		rows = append(rows, rs)
	}

	w := 2*padX + float64(width)*Advance
	h := headerH + 2*padY + float64(len(rows))*LineHeight

	var b strings.Builder
	fmt.Fprintf(&b, "<!-- Generated from the render, not drawn by hand. Do not edit:\n"+
		"     re-emit it with `go test ./internal/council ./internal/hud -update`.\n"+
		"     See internal/svgframe for why this picture cannot drift. -->\n")
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%s" height="%s" viewBox="0 0 %s %s" role="img" aria-labelledby="t">`+"\n",
		num(w), num(h), num(w), num(h))
	fmt.Fprintf(&b, "  <title id=\"t\">%s</title>\n", esc(f.Alt))

	fmt.Fprintf(&b, `  <rect x="0" y="0" width="%s" height="%s" rx="%s" fill="%s" stroke="%s"/>`+"\n",
		num(w), num(h), num(cornerRad), p.Background, p.Rule)
	// The caption strip is clipped to the panel's own rounded top by drawing it
	// with the same radius and then squaring off its bottom edge.
	fmt.Fprintf(&b, `  <path d="M0 %s V%s a%s %s 0 0 1 %s -%s H%s a%s %s 0 0 1 %s %s V%s Z" fill="%s"/>`+"\n",
		num(headerH), num(cornerRad), num(cornerRad), num(cornerRad), num(cornerRad), num(cornerRad),
		num(w-cornerRad), num(cornerRad), num(cornerRad), num(cornerRad), num(cornerRad), num(headerH),
		p.Header)
	fmt.Fprintf(&b, `  <line x1="0" y1="%s" x2="%s" y2="%s" stroke="%s"/>`+"\n",
		num(headerH), num(w), num(headerH), p.Rule)
	fmt.Fprintf(&b, `  <text x="%s" y="%s" font-family="%s" font-size="%s" fill="%s" fill-opacity="%s" xml:space="preserve">%s</text>`+"\n",
		num(padX), num(headerH/2+FontSize*0.36), FontStack, num(FontSize-1), p.Foreground, faintOpacity, esc(f.Caption))

	top := headerH + padY
	for i, rs := range rows {
		// The baseline sits three quarters down the line box, which is where a
		// terminal puts it and what keeps descenders clear of the row below.
		y := top + float64(i)*LineHeight + LineHeight*0.75
		fmt.Fprintf(&b, `  <text y="%s" font-family="%s" font-size="%s" xml:space="preserve">`+"\n",
			num(y), FontStack, num(FontSize))
		for _, r := range rs {
			b.WriteString("    ")
			b.WriteString(r.svg(p))
			b.WriteString("\n")
		}
		b.WriteString("  </text>\n")
	}
	b.WriteString("</svg>\n")
	return []byte(b.String()), nil
}

// run is a maximal stretch of cells sharing one style.
type run struct {
	col   int // starting cell column
	cells int
	text  string
	sty   sgr
}

func (r run) svg(p Palette) string {
	var attrs strings.Builder
	fmt.Fprintf(&attrs, `x="%s" textLength="%s" lengthAdjust="spacingAndGlyphs"`,
		num(padX+float64(r.col)*Advance), num(float64(r.cells)*Advance))

	fill := p.Foreground
	switch {
	case r.sty.hex != "":
		// A surface that named its own ink. The palette is not consulted: the
		// point of a hex triple is that it is the same colour on every scheme,
		// which is exactly why a full-screen surface may spend one and a
		// statusline may not (internal/council/style.go).
		fill = r.sty.hex
	case r.sty.fg >= 0:
		fill = p.ANSI[r.sty.fg]
	}
	fmt.Fprintf(&attrs, ` fill="%s"`, fill)
	if r.sty.faint {
		fmt.Fprintf(&attrs, ` fill-opacity="%s"`, faintOpacity)
	}
	if r.sty.bold {
		// Weight, not a brighter hue. Council spends weight as a signal in its
		// own right — a seat's name, a posture badge that says a seat may change
		// your files (style.go's Alert) — so rendering bold as weight is the
		// faithful reading; brightening it instead would move a claim into the
		// severity ramp.
		attrs.WriteString(` font-weight="bold"`)
	}
	return "<tspan " + attrs.String() + ">" + esc(r.text) + "</tspan>"
}

// sgr is the subset of SGR state a telltale frame can carry: a 4-bit
// foreground, weight, and intensity. Backgrounds, reverse video, underline and
// italics are not in it because neither renderer emits them — and parse fails
// loudly if one ever starts.
type sgr struct {
	fg int // -1: the terminal's default foreground
	// hex is a truecolor foreground the surface named for itself, "#rrggbb".
	// When it is set it wins over fg — see the run.svg switch for why the
	// palette is not consulted.
	hex   string
	bold  bool
	faint bool
}

func reset() sgr { return sgr{fg: -1} }

// parse splits one rendered line into styled runs and reports its cell width.
//
// Cell width is counted in runes. Every rune telltale renders — the box drawing,
// the eighth-blocks in a gauge, `▌ ▸ ⚙ ✓ ○ —` — is a single cell in the width
// tables both renderers lay out against, so a rune count and a cell count are
// the same number here. A rune that was not would break the emitted picture's
// alignment, and the equal-width check in Render is where that would surface.
func parse(line string) ([]run, int, error) {
	st := reset()
	var (
		runs []run
		cur  strings.Builder
		col  int
		n    int // cells in cur
	)
	flush := func() {
		if n == 0 {
			return
		}
		runs = append(runs, run{col: col, cells: n, text: cur.String(), sty: st})
		col += n
		n = 0
		cur.Reset()
	}

	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		if rs[i] != 0x1b {
			cur.WriteRune(rs[i])
			n++
			continue
		}
		if i+1 >= len(rs) || rs[i+1] != '[' {
			return nil, 0, fmt.Errorf("escape that is not a CSI sequence")
		}
		j := i + 2
		for j < len(rs) && rs[j] != 'm' {
			if rs[j] != ';' && (rs[j] < '0' || rs[j] > '9') {
				return nil, 0, fmt.Errorf("CSI sequence that is not SGR (ends %q)", string(rs[j]))
			}
			j++
		}
		if j >= len(rs) {
			return nil, 0, fmt.Errorf("unterminated SGR sequence")
		}
		next, err := apply(st, string(rs[i+2:j]))
		if err != nil {
			return nil, 0, err
		}
		if next != st {
			flush()
			st = next
		}
		i = j
	}
	flush()
	return runs, col, nil
}

// apply folds one SGR parameter string into the running style.
func apply(st sgr, params string) (sgr, error) {
	if params == "" {
		return reset(), nil
	}
	fields := strings.Split(params, ";")
	for i := 0; i < len(fields); i++ {
		p, err := strconv.Atoi(fields[i])
		if err != nil {
			return st, fmt.Errorf("SGR parameter %q is not a number", fields[i])
		}
		switch {
		case p == 0:
			st = reset()
		case p == 1:
			st.bold = true
		case p == 2:
			st.faint = true
		case p == 22:
			st.bold, st.faint = false, false
		case p >= 30 && p <= 37:
			st.fg = p - 30
		case p == 39:
			st.hex = ""
			st.fg = -1
		case p >= 90 && p <= 97:
			st.fg = p - 90 + 8
		case p == 38:
			// The extended-colour forms, and BOTH are accepted now.
			//
			// 38;5;n stays limited to the 16 indices telltale's palette names: a
			// 256-colour value would be a surface reaching for a shade the
			// scheme has no say over, without the decision that a hex triple
			// forces somebody to write down.
			//
			// 38;2;r;g;b is the surface having made that decision. It arrived
			// when internal/council took its own ink set (style.go's MONOGRAPH
			// palette): a full-screen room that inherited eight primaries from
			// whatever scheme was loaded could not have an identity of its own.
			// The picture draws the triple verbatim — a truecolor run is by
			// definition the same colour under every scheme, so there is nothing
			// here to resolve against a palette.
			if i+1 >= len(fields) {
				return st, fmt.Errorf("SGR 38 in a form this palette cannot resolve: %q", params)
			}
			switch fields[i+1] {
			case "5":
				if i+2 >= len(fields) {
					return st, fmt.Errorf("SGR 38;5 with no index: %q", params)
				}
				idx, err := strconv.Atoi(fields[i+2])
				if err != nil || idx < 0 || idx > 15 {
					return st, fmt.Errorf("SGR 38;5;%s is outside the 4-bit palette", fields[i+2])
				}
				st.fg, st.hex = idx, ""
				i += 2
			case "2":
				if i+4 >= len(fields) {
					return st, fmt.Errorf("SGR 38;2 with fewer than three channels: %q", params)
				}
				ch := [3]int{}
				for k := 0; k < 3; k++ {
					v, err := strconv.Atoi(fields[i+2+k])
					if err != nil || v < 0 || v > 255 {
						return st, fmt.Errorf("SGR 38;2 channel %q is not a byte", fields[i+2+k])
					}
					ch[k] = v
				}
				st.fg, st.hex = -1, fmt.Sprintf("#%02x%02x%02x", ch[0], ch[1], ch[2])
				i += 4
			default:
				return st, fmt.Errorf("SGR 38 in a form this palette cannot resolve: %q", params)
			}
		default:
			return st, fmt.Errorf("SGR parameter %d is not one this package draws", p)
		}
	}
	return st, nil
}

// num formats a coordinate to two decimals with the trailing zeros stripped.
//
// Rounded rather than printed at full precision because the cell advance is not
// representable in binary: an unrounded x lands on "95.60000000000001", which
// is fifteen digits of noise in a file whose whole point is that a human reads
// its diff. Two decimals is a fortieth of a pixel.
func num(f float64) string {
	s := strconv.FormatFloat(math.Round(f*100)/100, 'f', -1, 64)
	return s
}

func esc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
