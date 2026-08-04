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
type Styles struct {
	Text     lipgloss.Style // vendor output, prompt text
	Muted    lipgloss.Style // chrome, rules, labels, de-emphasis
	Identity lipgloss.Style // vendor names, focused tab
	SevOK    lipgloss.Style // a column that finished cleanly
	SevWarn  lipgloss.Style // a column that is unavailable or cancelled
	SevCrit  lipgloss.Style // a column that failed

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
		Plain: true,
	}
}

// Rule is the style for horizontal rules.
func (s Styles) Rule() lipgloss.Style { return s.Muted }

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

// ForAvailability returns the style an unavailable column's card renders in.
func (s Styles) ForAvailability(a Availability) lipgloss.Style {
	if a == AvailInstalled {
		return s.Text
	}
	return s.SevWarn
}
