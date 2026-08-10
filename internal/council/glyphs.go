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

	// RuleHeavy is the room's second rule weight, and it is spent on exactly
	// two lines: the full-bleed rule under the header, and the turn separator at
	// the top of a turn page.
	//
	// It was three. §9.44 took the lower full-bleed rule away — the composer is a
	// bordered box now and closes the frame by shape rather than by ink — so the
	// weight is scarcer than §9.26 left it and says something narrower: the chrome
	// stops here and the seats begin.
	//
	// A room that draws every horizontal line at one weight has no way to say
	// which of them is the OUTLINE. §9.11 gave this surface one rule glyph and
	// then asked it to be four different things — the frame's own edge, a column
	// header's leader, a turn separator in a transcript, a seat's heading on a
	// page — so a reader scanning for "where does the room end and the content
	// start" got the same ink as a reader scanning for "where does turn 3 begin".
	// Two weights make that one distinction and stop there; a third would be a
	// hierarchy nobody can hold in their head.
	//
	// It is a WEIGHT rather than a hue, so it costs the palette nothing and
	// survives NO_COLOR intact — and the ASCII partner is a different character
	// rather than a fallback to the light rule, so --ascii keeps the distinction
	// too. `=` is the one unclaimed mark left in the reduced set: the block below
	// enumerates what every other candidate already means here.
	RuleHeavy string

	// FocusRail is the gutter cell to the LEFT of the focused column, drawn
	// instead of Sep — the vertical answer to RuleHeavy's horizontal one.
	//
	// It is a SECOND carrier of a fact the room already states (§9.27). Focus is
	// spelled by the `▸` before the seat's name and by that name's weight, and
	// both of those live on one row at the top of a column; a reader scrolling a
	// transcript forty rows down had nothing on screen saying which column the
	// keys move. The rail is the only mark on this surface that is as tall as the
	// thing it describes, which is why it is worth a glyph.
	//
	// It rides §9.23's band exactly: the thick rail spans the rows the thin one
	// would and no others, so focus never asserts a grid over emptiness either.
	//
	// Same East-Asian-Ambiguous width caveat as every other glyph here (§7.10) —
	// U+258C measures one cell in Windows Terminal, the reference renderer, and a
	// terminal configured to render ambiguous as wide shears this row the way it
	// already shears the rules.
	FocusRail string

	// The four corners of the composer's box (§9.44).
	//
	// Only four glyphs are new here: the box's horizontal runs are Rule and its
	// sides are Sep, because a box side IS a vertical rule between what is inside
	// and what is outside — the same job Sep does between two columns — and
	// minting a second vertical would be two marks for one meaning.
	//
	// The corners are the one thing this room had no character for. They are also
	// the whole reason the box does not need the HEAVY weight the rule it replaced
	// carried: closure used to be spelled by ink, and it is now spelled by SHAPE —
	// four corners and two sides that meet — so the light weight is not a demotion
	// but the signal moving to a carrier that survives NO_COLOR and --ascii intact
	// (§9.26, amended).
	//
	// The ASCII partner is `+` in all four slots, and it is the same character
	// ActOK already owns. That is Range's slot argument, not a collision: `+` here
	// only ever appears at the two ends of a run of `-`, where an outcome mark
	// cannot be — outcome marks follow a trace entry inside a column, never at the
	// end of a border. One `+` for all four corners because ASCII has no cornering
	// characters at all, and a box drawn with `/` and `\` reads as a diagram rather
	// than as a frame.
	BoxTL, BoxTR, BoxBL, BoxBR string

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
		Sep:  "│", // │
		Rule: "─", // ─
		// U+2501, the box-drawing set's own heavy form of U+2500. The pair is
		// the whole reason this is two glyphs rather than a colour: a terminal
		// that can draw one draws the other, and they are legibly the same line
		// at two weights rather than two different marks.
		RuleHeavy: "━", // ━
		// U+258C LEFT HALF BLOCK. The heaviest one-cell vertical mark that is
		// still a RULE rather than a symbol — it reads as `│` with more ink,
		// which is the whole claim the rail makes.
		FocusRail: "▌", // ▌
		// U+256D..U+2570, the rounded forms. Rounded rather than square because
		// the box is the one element on screen the user ACTS in, and the softer
		// corner is what tells the eye it is an input rather than another panel.
		BoxTL:    "╭", // ╭
		BoxTR:    "╮", // ╮
		BoxBL:    "╰", // ╰
		BoxBR:    "╯", // ╯
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
		Sep:  "|",
		Rule: "-",
		// The heavy rule has to dodge the same list the outcome marks dodged, and
		// by the time this glyph was needed that list had grown: "-" is the light
		// rule, the Range joiner and the first spinner frame, "|" the separator,
		// ">" the ellipsis, "]" focus, "!" the warning prefix, "^"/"v" the
		// overflow markers, "*" Act, "." Idle, ":" the prompt, "_" the caret,
		// "+"/"x"/"?" the outcome marks, "/" and "\" the remaining spinner
		// frames, and "#" is the HUD's gauge fill. "=" is unclaimed, and it is
		// also the only unclaimed character that reads as a DOUBLED "-" rather
		// than as a different symbol — which is the one property a second rule
		// weight needs. TestTheHeavyRuleHasAnUnclaimedASCIIPartner holds it.
		RuleHeavy: "=",
		// "[" for the focused column's rail, and "#" — the obvious candidate —
		// is refused for the same reason ActOK refused it: it is the HUD's ascii
		// gauge fill, and one product means one vocabulary. Of what is left, "["
		// is the squarest vertical stroke in the set, so it reads as a thickened
		// "|" rather than as a different symbol; it faces the column it marks;
		// and its mirror "]" is already this room's ascii focus mark, so the two
		// spell ONE meaning in two slots instead of two meanings.
		//
		// The `[` and `]` in the mode line are key NAMES in the footer's prose,
		// never marks in the grid — the same slot argument Range's doc makes for
		// the hyphen, and the reason this is reuse rather than a collision.
		FocusRail: "[",
		// See BoxTL's doc for why one character does all four corners, and why
		// sharing ActOK's `+` is a slot reuse rather than a collision.
		BoxTL:    "+",
		BoxTR:    "+",
		BoxBL:    "+",
		BoxBR:    "+",
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
// newline case: a newline arriving inside a KeyPressMsg's Text is line-ending
// noise from the decoder, not paragraphing, so it flattens to a space.
//
// A bracketed paste never comes through here — it arrives whole as a
// tea.PasteMsg and goes through sanitizePaste (paste.go), which KEEPS its
// newlines, because there they are the operator's structure. This filter is
// the typed path, and — in a terminal with no bracketed paste, where a paste
// replays as keystrokes — the honest floor for that replay's text chunks.
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
