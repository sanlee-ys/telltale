package council

import "strings"

// Glyphs is council's visual alphabet. Like the HUD's, it is a value rather
// than constants because glyph mode is a switch independent of colour: --ascii
// replaces characters, NO_COLOR strips styling, and a legacy console can fail
// at one and not the other.
//
// The set is deliberately a subset of the HUD's, reusing the same characters
// for the same meanings — Sep still separates zones, Ellipsis still means
// truncated, Caret still marks the insertion point — so the two surfaces read
// as one product.
type Glyphs struct {
	Sep      string // column separator
	Rule     string // horizontal rule
	Ellipsis string // truncation
	Caret    string // insertion point in the prompt
	Warn     string // notice and unavailable-card prefix
	Focus    string // marks the focused column / selected tab
	Prompt   string // prompt line prefix
	Up       string // "there is more above" marker
	Down     string // "there is more below" marker
	Act      string // prefixes a tool call / command in the activity trace

	// Range joins the ends of a span of turn numbers — "turns 2–7" (§9.19).
	//
	// Punctuation rather than a mark, which is why it may be the hyphen in the
	// reduced set even though "-" is already the ASCII rule and the ASCII
	// spinner's first frame. Those two are marks: they stand alone in a slot and
	// are read as a symbol. This one only ever appears wedged between two digits,
	// where nothing else in this room can be, so there is no slot for it to
	// collide in.
	Range string

	// Idle is the one mark the phase vocabulary needed that nothing else in this
	// package already carried: a seat that has not been asked anything yet.
	//
	// Every other phase is spelled with a mark this room already owns — the
	// spinner for a turn in flight, ActOK for one that finished, ActFail for one
	// that broke, Warn for one that did not complete normally — so the phase
	// glyphs are a REUSE of meanings rather than a second alphabet. This one has
	// no existing owner, and it is deliberately the HUD's own "weakest state"
	// dot with the HUD's own ASCII form (design.md §7.5), because the two
	// surfaces are meant to read as one product.
	Idle string

	// The three outcome marks that follow a trace entry. There is deliberately
	// no mark for a call still pending: an unresolved entry renders bare,
	// because a mark for "nothing is known yet" would be a claim.
	//
	// ActUnknown is not a degraded ActOK and must never share its glyph. "the
	// vendor said this step ended" and "the vendor said this step worked" are
	// different facts, and §4a.1's rule is that different facts do not render
	// alike — the same reason a dropped column and an em dash are kept apart.
	ActOK      string // the vendor reported the call succeeded
	ActFail    string // the vendor reported the call failed
	ActUnknown string // the call ended and the vendor said nothing about how

	Spinner []string

	// ASCII reports whether this is the reduced set.
	ASCII bool
}

// UnicodeGlyphs is the reference set, rendered against Windows Terminal.
//
// Same known limitation as the HUD's (design.md §7.10): these are all
// East-Asian-Ambiguous width, Windows Terminal renders ambiguous as narrow, and
// a terminal configured otherwise will shear the layout. --ascii is the escape
// hatch.
func UnicodeGlyphs() Glyphs {
	return Glyphs{
		Sep:      "│", // │
		Rule:     "─", // ─
		Ellipsis: "…", // …
		Caret:    "_",
		Warn:     "⚠", // ⚠
		Focus:    "▸", // ▸
		Prompt:   "›", // ›
		Up:       "↑", // ↑
		Down:     "↓", // ↓
		Act:      "⚙", // ⚙
		Range:    "–", // en dash, the typographic joiner for a numeric span
		Idle:     "○", // ○
		// ✓ / ✗ / ? — the third is an ordinary question mark on purpose. It is
		// the one character that reads as "not known" to everybody without a
		// legend, and unlike a middle dot or an em dash it cannot be mistaken
		// for decoration or for a missing value.
		ActOK:      "✓", // ✓
		ActFail:    "✗", // ✗
		ActUnknown: "?",
		Spinner: []string{
			"⠋", "⠙", "⠹", "⠸", "⠼",
			"⠴", "⠦", "⠧", "⠇", "⠏",
		},
	}
}

// ASCIIGlyphs is for legacy consoles, non-UTF-8 code pages and piped output.
func ASCIIGlyphs() Glyphs {
	return Glyphs{
		Sep:      "|",
		Rule:     "-",
		Ellipsis: ">",
		Caret:    "_",
		Warn:     "!",
		// Focus cannot be ">": that is already the ASCII ellipsis here, and a
		// mark that also means "truncated" is not a mark. Same reasoning, and
		// the same answer, as the HUD's cursor.
		Focus:  "]",
		Prompt: ":",
		Up:     "^",
		Down:   "v",
		// "*" for a step the vendor took. Not "#" (the HUD's ASCII gauge fill)
		// and not ">" (the ellipsis here), because a mark that already means
		// something else is not a mark.
		Act: "*",
		// The plain hyphen, between two digits only — see Range's doc comment
		// for why that is not the collision the rest of this block avoids.
		Range: "-",
		// "." for a seat nothing has been asked of. Unclaimed here, and it is
		// exactly what the HUD's ASCII table already maps "○" to (design.md
		// §7.5) — so the two surfaces spell the same state the same way in both
		// glyph modes rather than only in the pretty one.
		Idle: ".",
		// The outcome marks have to dodge everything already spoken for here:
		// "*" is Act, ">" the ellipsis, "]" focus, "!" the warning prefix, "^"
		// and "v" the overflow markers, "|" the separator, "-" the rule and the
		// first spinner frame, "/" and "\" the others, ":" the prompt, "_" the
		// caret, and "#" is the HUD's gauge fill. That leaves "+" and "x",
		// which carry the tick/cross meaning in every ASCII checklist anyone
		// has ever read, and "?" which is unclaimed and means the same thing in
		// both sets.
		ActOK:      "+",
		ActFail:    "x",
		ActUnknown: "?",
		Spinner:    []string{"-", "\\", "|", "/"},
	}
}

func asciiGlyphs() Glyphs {
	g := ASCIIGlyphs()
	g.ASCII = true
	return g
}

// GlyphsFor picks a set. ascii is an explicit switch, never inferred from the
// colour profile.
func GlyphsFor(ascii bool) Glyphs {
	if ascii {
		return asciiGlyphs()
	}
	return UnicodeGlyphs()
}

// sanitize makes vendor-authored text safe to place in a fixed grid.
//
// Council's exposure here is worse than the HUD's, not better: the HUD renders
// session names, while council renders whatever three language models decided
// to emit, on a stream, into a fixed-width column. U+2028/U+2029, tabs and
// stray carriage returns would tear the layout apart, so they collapse to a
// space and other control characters are dropped. Newlines SURVIVE — they are
// the vendor's paragraphing and wrap() honours them.
func sanitize(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		// Written as escapes on purpose: these are invisible in an editor, and
		// this is the one place their identity has to be exact.
		case r == '\u2028' || r == '\u2029' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		case r == '\n':
			b.WriteByte('\n')
		case r < 0x20 || r == 0x7f:
			// dropped: an unprintable byte has no width the grid can budget
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// sanitizeKeepingSpace is sanitize for text the user is TYPING, minus the
// newline case: a draft prompt is one logical line on the footer regardless of
// what a paste contained.
//
// It does not trim. Trimming would make the string on screen disagree with the
// string about to be dispatched, which is the same silent divergence the HUD's
// find query refuses (design.md §7.14).
func sanitizeKeepingSpace(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\u2028' || r == '\u2029' || r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
