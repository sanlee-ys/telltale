package hud

import (
	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/theme"
)

// Styles is the HUD's whole palette: four hues, one attribute, and the
// terminal's default foreground. It maps internal/theme's ANSI indices to
// lipgloss values, which is the only place the Charm stack meets the shared
// numbers — internal/theme itself stays stdlib-only so the statusline binary
// never links a TUI framework (ADR-002).
//
// Semantic aliases exist so intent is greppable rather than inferred: Absent
// and Rule are Muted; Notice is SevWarn.
type Styles struct {
	Text     lipgloss.Style // primary values
	Muted    lipgloss.Style // chrome, labels, rules, de-emphasis
	Identity lipgloss.Style // model name, vendor tag
	SevOK    lipgloss.Style // value < 60
	SevWarn  lipgloss.Style // value >= 60
	SevCrit  lipgloss.Style // value >= 85
	Track    lipgloss.Style // unfilled gauge cells

	// Plain reports that every style is a no-op. Layout goldens render with a
	// plain set so they compare byte for byte without depending on the CI
	// terminal's colour profile.
	Plain bool
}

// NewStyles builds the coloured set.
//
// isDark comes from the terminal's answer to an OSC background-colour query.
// Lipgloss v2 removed AdaptiveColor and the global renderer, so adaptation is
// explicit and happens exactly here. Only Track consumes it, and no layout
// depends on it, so golden layout tests are unaffected by which branch is
// taken. Terminals that never answer leave the default: assume dark.
func NewStyles(isDark bool) Styles {
	ld := lipgloss.LightDark(isDark)
	return Styles{
		Text:     lipgloss.NewStyle(),
		Muted:    lipgloss.NewStyle().Faint(true),
		Identity: lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorIdentity)),
		SevOK:    lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorOK)),
		SevWarn:  lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorWarn)),
		SevCrit:  lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ColorCrit)),
		Track: lipgloss.NewStyle().Foreground(
			ld(lipgloss.Color(theme.ColorTrackLite), lipgloss.Color(theme.ColorTrackDark)),
		),
	}
}

// PlainStyles is the identity set: every Render returns its input unchanged.
//
// This is not a "no colour" mode for users — NO_COLOR is handled by
// colorprofile downsampling inside Bubble Tea, with no telltale code path
// involved. It exists so tests can assert layout without a terminal.
func PlainStyles() Styles {
	s := lipgloss.NewStyle()
	return Styles{
		Text: s, Muted: s, Identity: s,
		SevOK: s, SevWarn: s, SevCrit: s, Track: s,
		Plain: true,
	}
}

// Sev returns the style for a percentage's severity band.
func (s Styles) Sev(p float64) lipgloss.Style {
	switch theme.SeverityFor(p) {
	case theme.SevCrit:
		return s.SevCrit
	case theme.SevWarn:
		return s.SevWarn
	default:
		return s.SevOK
	}
}

// Absent is the style for the em dash. Aliased rather than used directly so
// that "this cell has no value" is greppable.
func (s Styles) Absent() lipgloss.Style { return s.Muted }

// Rule is the style for horizontal rules.
func (s Styles) Rule() lipgloss.Style { return s.Muted }

// Dim returns a de-emphasized variant of a style, used to render the whole row
// area when the scan has gone stale. Retained values stay on screen because
// they were true at the age displayed beside them; nothing renders at full
// intensity while the measurement is known to be old.
func (s Styles) Dim(dim bool) Styles {
	if !dim || s.Plain {
		return s
	}
	faint := lipgloss.NewStyle().Faint(true)
	return Styles{
		Text: faint, Muted: faint, Identity: faint,
		SevOK: faint, SevWarn: faint, SevCrit: faint, Track: faint,
	}
}
