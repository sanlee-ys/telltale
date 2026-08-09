// Package theme holds the numbers and names both render surfaces share:
// severity thresholds, the 4-bit ANSI palette indices, and the value
// formatters (docs/design.md §7.5).
//
// It is deliberately stdlib-only and deliberately holds no Style type. The
// statusline path (ADR-002) must never link a TUI framework, so this package
// carries thresholds and format helpers while each surface maps the palette
// indices itself: internal/statusline to raw escape codes, internal/hud to
// lipgloss.Style values. One source of truth for the numbers, zero coupling of
// the statusline binary to the Charm stack.
//
// Honest-gauge consequence, and the reason the formatters live here rather
// than in each renderer: percentages floor rather than round. A usage gauge
// that rounds 99.96 up to "100%" claims a window is exhausted when the vendor
// said it was not.
package theme

import (
	"fmt"
	"math"
	"time"
)

// Severity thresholds for any percentage gauge (docs/design.md §2, §7.5):
// green below WarnPct, yellow from WarnPct, red from CritPct.
const (
	WarnPct = 60.0
	CritPct = 85.0
)

// Severity is the band a percentage falls in. It is a value, not a color:
// each surface maps it to its own styling primitive.
type Severity uint8

const (
	SevOK Severity = iota
	SevWarn
	SevCrit
)

func (s Severity) String() string {
	switch s {
	case SevWarn:
		return "warn"
	case SevCrit:
		return "crit"
	default:
		return "ok"
	}
}

// SeverityFor bands a percentage on the 0–100 scale.
func SeverityFor(p float64) Severity {
	switch {
	case p >= CritPct:
		return SevCrit
	case p >= WarnPct:
		return SevWarn
	default:
		return SevOK
	}
}

// ANSI 4-bit palette indices. The terminal's own palette is used rather than
// hex truecolor so telltale inherits whatever theme the user already chose and
// looks native in Windows Terminal's default scheme and in a light scheme
// without a second palette.
//
// Hue owns exactly one meaning: cyan is identity, the green/yellow/red ramp is
// severity, faint is de-emphasis. Nothing else gets a color — in particular
// the HUD's liveness dot encodes state by glyph and intensity, never by hue,
// because green already means "under 60%".
const (
	ColorIdentity  = "6" // cyan: model name, vendor tag
	ColorOK        = "2" // green
	ColorWarn      = "3" // yellow
	ColorCrit      = "1" // red
	ColorTrackLite = "7" // gauge track on a light background
	ColorTrackDark = "8" // gauge track on a dark background
)

// ColorFor maps a severity to its ANSI palette index.
func ColorFor(s Severity) string {
	switch s {
	case SevCrit:
		return ColorCrit
	case SevWarn:
		return ColorWarn
	default:
		return ColorOK
	}
}

// Percent renders a percentage on the 0–100 scale in at most five columns:
// "84.2%", "41%", "100%", "0.4%".
//
// It floors to one decimal and never rounds up — a gauge must not overstate
// usage. Whole values drop the decimal, which is what keeps the field inside
// five columns ("99.9%" is the widest possible output).
func Percent(p float64) string {
	if p < 0 {
		p = 0
	}
	f := math.Floor(p*10) / 10
	if f == math.Trunc(f) {
		return fmt.Sprintf("%d%%", int64(f))
	}
	return fmt.Sprintf("%.1f%%", f)
}

// Cost renders a USD amount in at most seven columns: cents below $1000,
// whole dollars at or above it ("$999.99", "$1234"). Cents stop carrying
// information long before the field would overflow.
func Cost(usd float64) string {
	if usd < 0 {
		usd = 0
	}
	if usd >= 1000 {
		return fmt.Sprintf("$%d", int64(usd))
	}
	return fmt.Sprintf("$%.2f", usd)
}

// Tokens renders a token count compactly: "940", "48.0k", "1.9M", "2.4B".
//
// It floors at every step and never rounds up, the same rule Percent follows
// and for the same reason: this number is what a machine SPENT, and a display
// that rounds 47,950 up to "48.0k" has invented fifty tokens nobody was
// billed for. Flooring can only ever understate, which is the direction an
// honest gauge is allowed to be wrong in.
//
// "B" rather than "G" above a billion because the value is a count, not a
// byte size — the SI prefix would be borrowing a vocabulary that means
// something else here. Four columns is the common case ("1.9M"), six the
// widest ("999.9k").
func Tokens(n int64) string {
	if n < 0 {
		n = 0
	}
	switch {
	case n < 1_000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		// Integer division IS the floor: n/100 is hundreds, and dividing that
		// by ten in float can only render a tenth already reached.
		return trimTenth(float64(n/100)/10, "k")
	case n < 1_000_000_000:
		return trimTenth(float64(n/100_000)/10, "M")
	default:
		return trimTenth(float64(n/100_000_000)/10, "B")
	}
}

// trimTenth drops a trailing ".0" so a whole magnitude reads as "2M" rather
// than "2.0M" — the same shape Percent uses for whole percentages, and one
// fewer column in a header line that is already fighting for them.
func trimTenth(v float64, suffix string) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%d%s", int64(v), suffix)
	}
	return fmt.Sprintf("%.1f%s", v, suffix)
}

// Age renders how long ago something happened, in at most four columns:
// "12s", "47m", "2h", "3d". Sub-hour precision is where a session monitor's
// value is; finer precision above that would just be a second thing ticking.
//
// A negative duration (a file mtime ahead of the local clock) is not rendered
// here — the caller must treat that as an unreadable value and render absence,
// because "0s" would claim the session was active this instant.
func Age(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		days := int(d.Hours()) / 24
		if days > 999 {
			days = 999
		}
		return fmt.Sprintf("%dd", days)
	}
}

// Countdown renders time remaining until a quota window resets: "2h13m",
// "47m", "5d02h". The days branch matters: a seven-day window is 168 hours,
// and "168h00m" is both wider and less legible than "6d23h".
func Countdown(d time.Duration) string {
	// Checked before rounding: rounding first would turn 30 s of remaining
	// window into "1m", which is a minute the vendor never gave us.
	if d < time.Minute {
		return "<1m"
	}
	d = d.Round(time.Minute)
	switch {
	case d >= 24*time.Hour:
		days := int(d.Hours()) / 24
		hours := int(d.Hours()) % 24
		return fmt.Sprintf("%dd%02dh", days, hours)
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return "<1m"
	}
}

// WindowLabel names a quota window from its length in minutes: "5h", "7d",
// "45m". Budgeted for the statusline at four cells or fewer.
//
// Callers pass a length only when the vendor reported one. A window whose
// length is unknown gets a positional label from the adapter instead — naming
// it "5h" on a guess would be a duration claim we cannot source.
func WindowLabel(minutes int64) string {
	switch {
	case minutes <= 0:
		return ""
	case minutes < 60:
		return fmt.Sprintf("%dm", minutes)
	case minutes < 24*60:
		return fmt.Sprintf("%dh", minutes/60)
	default:
		return fmt.Sprintf("%dd", minutes/(24*60))
	}
}
