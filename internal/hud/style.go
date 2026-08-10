package hud

import (
	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
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

// vendorHue is one vendor's own ANSI palette index, and it is the SECOND half
// of council's ratified hue exception rather than a new decision (San,
// 2026-08-09; design.md §7.17, extending §9.28).
//
// **What the exception is for.** Council spends one hue per SEAT, on the
// argument that "which of several agents is speaking" is a concept
// internal/theme has no token for. The fleet usage view is the one HUD surface
// with the same shape: six vendor blocks stacked in one column, each a heading
// with a paragraph under it, so POSITION answers nothing about which vendor a
// reader is looking at — which is the exact condition under which a hue earns
// its place. The grid does not qualify and does not get this: a row's vendor is
// already answered by the two-letter tag in a fixed column.
//
// **Why it lives here and not in internal/theme, and the stdlib rule is NOT the
// reason.** These are plain strings and would compile in theme perfectly well;
// citing ADR-002 here would send the next reader to fix the wrong thing. The
// reason is theme's OWN contract — one hue, one meaning, across every surface
// that imports it — and internal/statusline has no vendor blocks. A per-vendor
// hue promoted to theme would be a token that means nothing on the surface with
// the tightest line budget in the product, which is how a shared palette stops
// being shared. That is council's argument verbatim, and the fact that TWO
// packages now hold the same map is the honest cost of keeping theme's contract
// intact; TestVendorHuesMatchCouncilsSeats is what stops the two copies from
// teaching different colours for one vendor.
//
// **The assignments are council's, matched by literal value** — claude 5,
// codex 6, agy 4, cursor 12, grok 14 — because a reader who learned in the room
// that magenta is Claude must not meet a second colour for Claude one keypress
// away. Everything council argued about them carries over unchanged: 4-bit
// indices so the terminal resolves them against the scheme the user already
// chose (a hex triple would be telltale asserting a colour over the user's own,
// and would need an isDark fork); the severity family 1/2/3 and its bright
// twins 9/10/11 off limits, because a vendor wearing red on a surface whose
// percentages are red at 85% would read as an account in trouble; the chrome
// family 0/7/8/15 off limits because it is the gauge track and the terminal's
// own fore/background. That leaves 4, 5, 6, 12, 13, 14 and this spends five of
// them — the same five, with 13 the last free index in the fleet.
//
// **The honest weakness, inherited too.** agy is 4 and grok 14 are one hue at
// two intensities away from cursor 12 and codex 6 respectively, and some
// terminal schemes render each pair close together. Council carries that on the
// two-letter tags beside every seat name; here the vendor's full NAME is the
// thing the hue is tinting, spelled out in the heading, so the distinction is
// carried by more than council had.
//
// Gemini takes the identity hue as a documented fallback rather than a hue of
// its own, exactly as it does in the room: a vendor with no hue decision behind
// it renders as it always did rather than as something broken.
func vendorHue(v model.VendorID) string {
	switch v {
	case model.VendorClaude:
		return "5" // magenta
	case model.VendorCodex:
		return "6" // cyan — theme's identity hue, kept by the vendor that had it
	case model.VendorAntigravity:
		return "4" // blue
	case model.VendorCursor:
		return "12" // bright blue
	case model.VendorGrok:
		return "14" // bright cyan
	default:
		// Gemini and anything added since.
		return theme.ColorIdentity
	}
}

// VendorIdentity is Identity retinted to one vendor's own hue. It is spent on
// exactly one thing: the vendor NAME in a usage-block heading (§7.17).
//
// Not the seam sentence beside it, which is chrome and — past quotaAgeWarn — a
// warning that must not be recoloured. Not the gauges, percentages or
// countdowns, which severity owns. Not the spend line, the models census, the
// grid rows or the header's quota block: on all of those the vendor is either
// already answered by position or is not the question being asked.
//
// Derived from the base style rather than built beside it, so PlainStyles stays
// the identity set BY CONSTRUCTION — see retint. The one caller renders a body
// that is never dimmed (Render's `case st.Usage` says why), so there is no path
// on which this re-brightens a de-emphasized frame.
func (s Styles) VendorIdentity(v model.VendorID) lipgloss.Style {
	return s.retint(s.Identity, v)
}

// retint is the one place a vendor hue reaches a style, and the Plain guard is
// what keeps every layout golden blind to this feature. A second literal
// constructor would have to remember the guard, and would forget it the first
// time it grew an attribute.
func (s Styles) retint(st lipgloss.Style, v model.VendorID) lipgloss.Style {
	if s.Plain {
		return st
	}
	return st.Foreground(lipgloss.Color(vendorHue(v)))
}

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
