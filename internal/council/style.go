package council

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/theme"
)

// Styles is council's palette. It maps internal/theme's ANSI indices to
// lipgloss values, the same way internal/hud does and for the same reason:
// internal/theme stays stdlib-only so the statusline binary never links a TUI
// framework (ADR-002).
//
// Council adds no hues of its own, with ONE ratified exception. A dispatch room
// that invented a sixth colour would drift from the visual language the
// statusline and HUD share, and that rule stands for every concept this package
// has. The exception is the SEAT, which is the one concept council has that the
// other two surfaces do not — see seatHue, which carries the ratification, the
// closed list of places it is spent, and the weakness it is honest about
// (§9.28). It is an exception to a rule, not a repeal of it: nothing else in
// this package may add a hue, and the seat hue itself is spent only where seat
// names are what the eye is sorting.
//
// What it does add is WEIGHT and CONTRAST, both attributes rather than hues and
// therefore costing the palette nothing. The room had exactly one typographic
// level — everything was either default or faint — so a seat's name, a posture
// claim, a key you can press and a line of vendor prose all arrived at the eye
// with the same emphasis. Bold is how the two lines that are always true of a
// column (who it is, what it may do) separate from the several hundred lines
// that are only true this turn; Dim is how the column the keys move separates
// from the three the reader is not in (§9.27).
type Styles struct {
	Text     lipgloss.Style // vendor output, prompt text, the keys in the mode line
	Muted    lipgloss.Style // chrome, rules, labels, de-emphasis
	Identity lipgloss.Style // vendor names, focused tab
	SevOK    lipgloss.Style // a column that finished cleanly
	SevWarn  lipgloss.Style // a column that is unavailable or cancelled
	SevCrit  lipgloss.Style // a column that failed

	// Strong is Identity at full weight: the room's own name, each seat's name,
	// and the user's brief echoed back inside a column. Everything it marks is
	// an ANCHOR — the thing a reader is looking for when they scan the frame —
	// rather than a value that changed this second.
	Strong lipgloss.Style
	// Alert is SevWarn at full weight, and it is spent on exactly two things: a
	// posture badge saying this seat can change your files, and the title line
	// of a card explaining why a seat is not working. Both are claims a hurried
	// reader must not skim past, and both already carry their meaning in words
	// — the weight only makes the word findable.
	Alert lipgloss.Style

	// Dim is Text one contrast step down, and it is spent on exactly one thing:
	// the reading area of a column the keys do NOT move (§9.27).
	//
	// The room's focus signals — `▸`, the seat name's weight, and now the thick
	// rail — all say which column is addressed. None of them says anything about
	// the other three, so four columns of prose arrived at one intensity and the
	// eye had no reason to land anywhere. This is crush's Focused/Blurred pair
	// applied to prose rather than to a border: the addressed column keeps full
	// contrast and its neighbours recede.
	//
	// It is Text + Faint, which is Muted's own attribute, and that collapse is
	// ACCEPTED rather than overlooked. Inside an unfocused column the prose and
	// the chrome do end up at one intensity — but every distinction between them
	// is carried by shape first: a turn separator is a labelled rule, a trace
	// entry opens with ⚙, a skip line opens with ○, a note with ⚠. Weight was
	// always the second signal there (§7.1 rule 2), so what is lost is the second
	// signal on a column the reader is not reading. Widening this to the focused
	// column's chrome, or to any card, would spend it where the second signal is
	// still doing work.
	//
	// PlainStyles renders it as the identity function, so no layout golden sees
	// it and the distinction it makes is one `▌` and `▸` already carry whole
	// under --ascii and NO_COLOR.
	Dim lipgloss.Style

	// Blurred marks a derived set for a column the keys do not move — see
	// forSeat. It exists so the prose builders can ask ONE question (`sty.Body()`)
	// rather than each taking a seatFocus they would each have to interpret.
	Blurred bool

	// Plain reports that every style is a no-op, for layout goldens.
	Plain bool
}

// NewStyles builds the coloured set. isDark is accepted for symmetry with the
// HUD and for the adaptive token council will need when it grows a gauge; no
// current token consumes it, so no golden depends on the answer.
func NewStyles(isDark bool) Styles {
	_ = isDark
	return Styles{
		Text:     lipgloss.NewStyle(),
		Muted:    lipgloss.NewStyle().Faint(true),
		Identity: lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorIdentity)),
		SevOK:    lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorOK)),
		SevWarn:  lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorWarn)),
		SevCrit:  lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorCrit)),
		Strong:   lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorIdentity)).Bold(true),
		Alert:    lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorWarn)).Bold(true),
		Dim:      lipgloss.NewStyle().Faint(true),
	}
}

// PlainStyles is the identity set: every Render returns its input unchanged, so
// layout goldens compare byte for byte without depending on the CI terminal's
// colour profile.
func PlainStyles() Styles {
	s := lipgloss.NewStyle()
	return Styles{
		Text: s, Muted: s, Identity: s,
		SevOK: s, SevWarn: s, SevCrit: s,
		// Strong and Alert are identity here too, which is the whole reason
		// weight is safe to introduce: it changes no cell's width and no line's
		// content, so every layout golden is blind to it and every distinction
		// it makes is one the words and glyphs already carried.
		Strong: s, Alert: s,
		// Dim likewise: an unfocused column's prose is the same bytes at the same
		// width, which is what makes contrast safe to spend as a signal at all.
		Dim:   s,
		Plain: true,
	}
}

// Rule is the style for horizontal rules.
func (s Styles) Rule() lipgloss.Style { return s.Muted }

// forSeat derives the set one column renders with: unchanged for the seat the
// keys move, one contrast step down for every other (§9.27).
//
// A derived SET rather than a seatFocus threaded through columnLines, because
// the demotion has to reach builders nested three deep and every one of them
// already takes a Styles. Passing the focus alongside would mean each of them
// deciding for itself what "unfocused" means, which is exactly how a rule this
// narrow gets widened by accident.
func (s Styles) forSeat(f seatFocus) Styles {
	if f.hasKeys() {
		return s
	}
	s.Blurred = true
	return s
}

// Body is the style a column's READING AREA renders in: the vendor's reply, the
// live stand-in for one that has not arrived yet, and the line standing in for a
// seat that has not been asked anything.
//
// Not the chrome around it (already Muted). Not the user's echoed brief, which
// stays Strong in every column — what a seat was ASKED is the thing a reader
// scrolls looking for, and it is the user's own words rather than a vendor's.
// Not a note or a card: those carry failure and posture claims, and §9.2's rule
// is that a claim does not get quieter because the reader is looking elsewhere.
func (s Styles) Body() lipgloss.Style {
	if s.Blurred {
		return s.Dim
	}
	return s.Text
}

// bold adds weight without breaking the identity set.
//
// PlainStyles has to stay a true no-op — every layout golden compares bytes —
// and lipgloss will happily emit a bold escape from an otherwise empty style,
// so weight is applied through here rather than by calling Bold in the
// renderer.
func (s Styles) bold(st lipgloss.Style) lipgloss.Style {
	if s.Plain {
		return st
	}
	return st.Bold(true)
}

// ForPhase returns the style a column's status word renders in.
//
// Idle, waiting and streaming are all Muted: a column in flight is not a
// severity, and colouring it would spend the alphabet's loudest signal on the
// most common state.
func (s Styles) ForPhase(p Phase) lipgloss.Style {
	switch p {
	case PhaseDone:
		return s.SevOK
	case PhaseFailed:
		return s.SevCrit
	case PhaseCancelled:
		return s.SevWarn
	default:
		return s.Muted
	}
}

// ForSandbox returns the style a column's posture badge renders in.
//
// §9.2 makes the badge a safety claim and then leaves it looking like every
// other word on a faint line: on a room where one seat is `ro:tools` and the
// one beside it can edit your working tree, both whispered at the same volume.
// The badge that says a seat may CHANGE things now carries weight and the
// warning hue; the ones that name a read-only mechanism stay chrome.
//
// This does not weaken the rule that colour is redundant — it strengthens it.
// The distinction is still carried entirely by the word (`unsandboxed` and
// `WRITES` break the `ro:` prefix on purpose), which is why the badges survive
// --ascii and NO_COLOR exactly as they did. Weight and hue only make the word
// findable in a frame with four columns of prose in it.
func (s Styles) ForSandbox(l SandboxLevel) lipgloss.Style {
	switch l {
	case SandboxWrite, SandboxNone:
		return s.Alert
	case SandboxGated:
		// Bold, but not a severity. A gated seat is the room working: it may do
		// everything SandboxWrite allows and has to be told yes first, so
		// colouring it like the ungated ones would teach the eye to skip the
		// difference the gate exists to make.
		return s.bold(s.Text)
	default:
		return s.Muted
	}
}

// seatHue is one seat's own ANSI palette index — the RATIFIED exception to
// "council adds no hues of its own" (San, 2026-08-07; design.md §9.28).
//
// **Why council and not internal/theme.** The stdlib-only rule (ADR-002) is not
// the reason and citing it here would send the next reader to fix the wrong
// thing: these are plain strings and would compile in theme just fine. The
// reason is theme's OWN contract — one hue, one meaning, across every surface —
// and `internal/statusline` and `internal/hud` have no seats. A per-vendor hue
// promoted to theme would be a token that means nothing on two of the three
// surfaces that import it, which is how a shared palette stops being shared.
//
// **Why 4-bit indices.** theme.go's own argument, unchanged: the terminal
// resolves an index against the scheme the user already chose, so council looks
// native in Windows Terminal's default and in a light scheme without a second
// palette and without an isDark fork. A hex triple would be council asserting a
// colour over the user's own.
//
// **What is OFF LIMITS, and it is not a guideline.** The severity family — 1/2/3
// and their bright twins 9/10/11 — belongs to the green/yellow/red ramp, and a
// seat that happened to wear red would be a seat that reads as failed. The
// chrome family — 0/7/8/15 — is the gauge track and the terminal's own
// foreground/background. That leaves 4, 5, 6, 12, 13, 14, and this function
// spends four of them. TestNoSeatHueIsASeverity fails the build if that ever
// stops being true.
//
// **The honest weakness.** agy is 4 and cursor is 12: one hue at two
// intensities, which is a distinction a reader can miss and which some terminal
// schemes render closer together than others. That is acceptable HERE and only
// here, because the two-letter tags §9.25 made permanent — `AG` and `CU` — carry
// the real distinction on every surface a seat name appears on, and the hue is
// the second signal it is supposed to be. If a fifth vendor arrives wanting
// blue, the tag is what still works and the hue is what has to be argued for.
//
// **The fifth vendor arrived, and here is the argument it owed.** Grok is 14,
// bright cyan — the twin of Codex's 6, so the weakness above is now TWO pairs
// rather than one. That is forced rather than chosen: after 4/5/6/12 the legal
// set holds only 13 and 14, and both are twins of a seat already at the table.
// There was no unpaired hue to spend, so the only real decision was WHICH seat
// to pair with, and it went to Codex for a reason about how the room is
// actually used. Silence routes to Claude alone (mentions.go, defaultRoute), so
// the Claude column is the one on screen in nearly every room; keeping magenta
// unshared protects the seat a reader sees most from the one confusion this
// palette can still make. Codex's cyan is spent instead, and `CX` versus `GR`
// is what carries the distinction when a scheme renders 6 and 14 close.
//
// What this exhausts is worth stating plainly for whoever adds the sixth: the
// legal set is now FULL. 13 is the last free index, and after it a new seat
// cannot have a hue of its own without either taking a severity — which would
// make a seat read as failed — or abandoning 4-bit indices, which would mean
// council asserting a colour over the user's own scheme. The tag is what
// scales; the hue was always going to run out.
func seatHue(v model.VendorID) string {
	switch v {
	case model.VendorClaude:
		return "5" // magenta
	case model.VendorCodex:
		return "6" // cyan — theme's identity hue, kept for the seat that had it
	case model.VendorAntigravity:
		return "4" // blue
	case model.VendorCursor:
		return "12" // bright blue
	case model.VendorGrok:
		return "14" // bright cyan
	default:
		// Gemini and anything added since. A seat with no hue of its own renders
		// in the identity hue every seat name used to have, which is a seat that
		// looks exactly as it did rather than a seat that looks broken.
		return theme.ColorIdentity
	}
}

// SeatIdentity is Identity retinted to this seat's own hue, and SeatStrong is
// Strong retinted the same way.
//
// Derived from the base styles rather than built beside them, so PlainStyles
// stays the identity set BY CONSTRUCTION: `s.Identity` is the empty style there,
// and retinting is skipped outright, so no golden can see any of this. A second
// pair of literal constructors would have to remember that, and would forget it
// the first time one of them grew a second attribute.
func (s Styles) SeatIdentity(v model.VendorID) lipgloss.Style {
	return s.retint(s.Identity, v)
}

// SeatStrong is Strong at this seat's hue — the seat name at weight, where seat
// names are what the eye is sorting.
func (s Styles) SeatStrong(v model.VendorID) lipgloss.Style {
	return s.retint(s.Strong, v)
}

// retint is the one place a seat hue reaches a style, and the Plain guard is
// what keeps every layout golden blind to §9.28.
func (s Styles) retint(st lipgloss.Style, v model.VendorID) lipgloss.Style {
	if s.Plain {
		return st
	}
	return st.Foreground(lipgloss.Color(seatHue(v)))
}

// ForDiffLine returns the style one raw patch line renders in — the "later,
// separate change" §9.37's `d` amendment left on the table, landed entirely
// inside the palette the room already has. Added lines wear SevOK, removed
// lines SevCrit, headers the Muted chrome style; nothing here is a new hue,
// and nothing here is a first signal. The `+`/`-` prefixes carry the whole
// distinction on their own — they are what survives --ascii and NO_COLOR, and
// under PlainStyles every branch of this switch is the identity style, which
// is why no layout golden can see this function exist.
//
// Classification reads the RAW line, and the headers are matched FIRST, in
// this order, because three of them are prefix-shadowed by the change
// markers: `+++ b/file` opens with the addition's own `+`, `--- a/file` with
// the removal's `-`, and a classifier that checked `+` before `+++` would
// paint a file header as an inserted line — a header wearing green is the
// patch claiming a change it never made. `index ` keeps its trailing space on
// purpose: a body line of prose can begin with the word "index", and only
// git's own `index 1234..5678 100644` form carries the space-delimited shape.
//
// SevOK/SevCrit here are severity tokens spent on non-severity facts, and
// that reuse is deliberate rather than sloppy: green-for-added and
// red-for-removed is the one colour convention every diff reader already
// owns, it is the same green/red every terminal diff pager maps to these same
// ANSI indices, and minting a second green would be exactly the sixth-colour
// drift the package comment forbids. The context lines stay Text — a line the
// patch did not touch is the baseline the coloured lines read against.
func (s Styles) ForDiffLine(line string) lipgloss.Style {
	switch {
	case strings.HasPrefix(line, "+++"),
		strings.HasPrefix(line, "---"),
		strings.HasPrefix(line, "diff --git"),
		strings.HasPrefix(line, "index "),
		strings.HasPrefix(line, "@@"):
		return s.Muted
	case strings.HasPrefix(line, "+"):
		return s.SevOK
	case strings.HasPrefix(line, "-"):
		return s.SevCrit
	default:
		return s.Text
	}
}

// ForAvailability returns the style an unavailable column's card renders in.
func (s Styles) ForAvailability(a Availability) lipgloss.Style {
	if a == AvailInstalled {
		return s.Text
	}
	return s.SevWarn
}
