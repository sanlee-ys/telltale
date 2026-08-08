package council

import (
	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/theme"
)

// Styles is council's palette. It maps internal/theme's ANSI indices to
// lipgloss values, the same way internal/hud does and for the same reason:
// internal/theme stays stdlib-only so the statusline binary never links a TUI
// framework (ADR-002).
//
// Council adds no hues of its own. A dispatch room that invented a sixth colour
// would drift from the visual language the statusline and HUD share.
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

// ForAvailability returns the style an unavailable column's card renders in.
func (s Styles) ForAvailability(a Availability) lipgloss.Style {
	if a == AvailInstalled {
		return s.Text
	}
	return s.SevWarn
}
