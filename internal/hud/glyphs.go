package hud

import "strings"

// Glyphs is the HUD's visual alphabet. It exists as a value rather than as
// constants because glyph mode is a switch independent of colour: NO_COLOR
// strips styling, --ascii replaces characters, and the two are unrelated
// failures (a legacy console may render colour fine and box glyphs as mojibake).
//
// Every distinction in the HUD is carried by a glyph or a number first and by
// colour only second, which is what makes both degradations correct by
// construction rather than by a second code path.
type Glyphs struct {
	// DotLive/DotIdle/DotStale encode liveness. There is deliberately no
	// DotUnknown: unknown liveness renders as blank, because "stale" is a
	// claim and "we have no activity signal" is not (design.md §4a.4).
	DotLive  string
	DotIdle  string
	DotStale string

	// Fill and Eighths draw the gauge; Track is the unfilled remainder. A
	// full-height fill over a mid-height rule reads as a level above a
	// baseline; a shaded track reads as texture and fights the text.
	Fill    string
	Eighths []string
	Track   string

	Sep      string // zone separator
	Absent   string // the "no value" marker
	Ellipsis string // truncation
	Reset    string // quota countdown prefix
	Warn     string // footer notice prefix

	// RuleHeavy is the second rule weight, and it exists for the same reason
	// council's does (§9.26): one weight asked to be both "where the frame ends"
	// and "where a section starts" is a hierarchy a reader has to reconstruct
	// from indentation. `Track` stays the interior weight — it draws the frame's
	// own full-bleed rules and every gauge track — and RuleHeavy is spent on
	// exactly one line in the whole HUD, the fleet usage view's title (§7.17).
	//
	// It is a CHARACTER before it is a style, so `--ascii` gets `=` rather than
	// falling back to `-`: the hierarchy has to survive on the terminals least
	// able to infer it, and `NO_COLOR` never touched it because weight of this
	// kind is a glyph rather than an attribute. `=` is the one unclaimed mark
	// left in the reduced set and the only unclaimed one that reads as a
	// DOUBLED `-` rather than as a different symbol — the one property a second
	// rule weight needs. TestTheHeavyRuleHasAnUnclaimedASCIIPartner enumerates
	// the claimed set so the next glyph cannot be added without meeting it. The
	// same pair council chose, deliberately: §7.1 principle 5 is that these are
	// one product, and a second heavy-rule character would be a second alphabet.
	RuleHeavy string

	// Cursor marks the selected row. It lives in the row's leading pad column,
	// which was already blank, so selection costs the grid nothing.
	Cursor string
	// Fork prefixes the sub-agent chip. A count, not a gauge — the row says
	// "this session is fanning out", and the number says how wide.
	Fork string
	// Mid separates two facts inside one cell (the burn forecast's clock time
	// from its sampling basis). Lighter than Sep, which separates zones.
	Mid string
	// Caret marks the insertion point in the find query. Static, never
	// blinking: §7.1 rule 4 budgets exactly one moving cell on this screen and
	// it is not this one.
	Caret string

	Spinner []string

	// ASCII reports whether this is the reduced set, so callers can pick a
	// different affordance where a straight substitution does not work.
	ASCII bool
}

// UnicodeGlyphs is the reference set, rendered against Windows Terminal.
//
// Known limitation (design.md §7.10): every glyph here is East-Asian-Ambiguous
// width. Windows Terminal renders ambiguous as narrow, which is what the grid
// assumes; a terminal configured to render them double-width will shear the
// layout, and ASCIIGlyphs is the escape hatch.
func UnicodeGlyphs() Glyphs {
	return Glyphs{
		DotLive:  "●", // ●
		DotIdle:  "◐", // ◐
		DotStale: "○", // ○
		Fill:     "█", // █
		Eighths: []string{
			"▏", "▎", "▍", "▌",
			"▋", "▊", "▉",
		},
		Track:     "─", // ─
		RuleHeavy: "━", // ━
		Sep:       "│", // │
		Absent:    "—", // —
		Ellipsis:  "…", // …
		Reset:     "↻", // ↻
		Warn:      "⚠", // ⚠
		Cursor:    "▸", // ▸
		Fork:      "⑂", // ⑂
		Mid:       "·", // ·
		Caret:     "_",
		Spinner: []string{
			"⠋", "⠙", "⠹", "⠸", "⠼",
			"⠴", "⠦", "⠧", "⠇", "⠏",
		},
	}
}

// ASCIIGlyphs is for legacy consoles, non-UTF-8 code pages, and piped output.
//
// The gauge loses its partials: "#" has no eighth-width relatives, so fill
// resolution drops to one whole cell. That is a real loss of precision in the
// bar, and it is acceptable only because the number beside the bar carries the
// precision anyway — the bar carries the glance.
func ASCIIGlyphs() Glyphs {
	return Glyphs{
		DotLive:   "*",
		DotIdle:   "o",
		DotStale:  ".",
		Fill:      "#",
		Eighths:   nil,
		Track:     "-",
		RuleHeavy: "=",
		Sep:       "|",
		Absent:    "n/a",
		Ellipsis:  ">",
		Reset:     "~",
		Warn:      "!",
		// The cursor cannot be ">" here: ">" is already the ASCII ellipsis, and
		// a mark that also means "truncated" is not a mark. "*" is taken by
		// DotLive, so the selection uses the one bracket shape nothing else in
		// the ASCII set uses.
		Cursor:  "]",
		Fork:    "Y",
		Mid:     "-",
		Caret:   "_",
		Spinner: []string{"-", "\\", "|", "/"},
	}
}

// Worktree renders a worktree mark. It is a method rather than a glyph because
// the ASCII form wraps the name rather than prefixing it.
//
// v1 note: no adapter can source a worktree name from disk — the field exists
// only on the statusline's stdin payload, which the HUD does not consume — so
// nothing calls this yet. It is kept because the mark is part of the shared
// visual language with the statusline (design.md §7.1 principle 5).
func (g Glyphs) Worktree(name string) string {
	if name == "" {
		return ""
	}
	if g.ASCII {
		return "(" + name + ")"
	}
	return "⌥" + name // ⌥
}

func asciiGlyphs() Glyphs {
	g := ASCIIGlyphs()
	g.ASCII = true
	return g
}

// GlyphsFor picks a set. ascii is an explicit switch (--ascii, TELLTALE_ASCII,
// or a non-terminal output target), never inferred from the colour profile.
func GlyphsFor(ascii bool) Glyphs {
	if ascii {
		return asciiGlyphs()
	}
	return UnicodeGlyphs()
}

// sanitize makes model-authored text safe to place in a fixed grid.
//
// Session names come from the model and may contain U+2028, U+2029, tabs or
// newlines (design.md §4a.2 is explicit that renderers must not assume one
// line). Any of those in a row would tear the layout apart at render time, so
// they collapse to a space here and other control characters are dropped.
func sanitize(s string) string { return strings.TrimSpace(sanitizeKeepingSpace(s)) }

// sanitizeKeepingSpace is sanitize without the trim, for text the user is
// TYPING.
//
// Trimming a query would make the string on screen disagree with the string
// being matched: type a trailing space and the footer would show "acme" while
// the filter hides every row, because nothing contains "acme ". Small, and
// exactly the class of silent divergence this product exists to refuse.
func sanitizeKeepingSpace(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		// U+2028/U+2029 are written as escapes on purpose: they are invisible
		// in an editor, and this is the one place their identity must be exact.
		case r == '\u2028' || r == '\u2029' || r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f:
			// dropped: an unprintable byte has no width the grid can budget
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
