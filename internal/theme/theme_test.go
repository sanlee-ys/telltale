package theme

import (
	"testing"
	"time"
)

func TestPercentFloorsNeverRoundsUp(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0%"},
		{0.4, "0.4%"},
		{5, "5%"},
		{41, "41%"},
		{84.2, "84.2%"},
		{92.6, "92.6%"},
		{99.9, "99.9%"},
		{100, "100%"},
		// The reason this helper exists: %.1f would render 100.0% here and
		// claim an exhausted window the vendor never reported.
		{99.96, "99.9%"},
		{99.99999, "99.9%"},
	}
	for _, c := range cases {
		if got := Percent(c.in); got != c.want {
			t.Errorf("Percent(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPercentFitsFiveColumns(t *testing.T) {
	for p := 0.0; p <= 100.0; p += 0.05 {
		if n := len([]rune(Percent(p))); n > 5 {
			t.Fatalf("Percent(%v) = %q is %d columns, budget is 5", p, Percent(p), n)
		}
	}
}

func TestTokensFloorsAndNeverOverstates(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{999, "999"},
		{1000, "1k"},
		{1203, "1.2k"},
		{48012, "48k"},
		{48099, "48k"},
		// The flooring rule, stated as a case: 47,999 is not 48k. A gauge that
		// rounds a spend figure up has invented tokens nobody was billed for.
		{47999, "47.9k"},
		{999999, "999.9k"},
		{1_000_000, "1M"},
		{1_904_221, "1.9M"},
		{1_999_999, "1.9M"},
		{999_999_999, "999.9M"},
		{1_000_000_000, "1B"},
		{2_450_000_000, "2.4B"},
		// Negative is not a small count, it is a broken one. Callers reject it
		// upstream; this is the belt-and-braces floor.
		{-5, "0"},
	}
	for _, c := range cases {
		if got := Tokens(c.in); got != c.want {
			t.Errorf("Tokens(%d) = %q, want %q", c.in, got, c.want)
		}
		if n := len([]rune(Tokens(c.in))); n > 6 {
			t.Errorf("Tokens(%d) = %q exceeds the 6-column budget", c.in, Tokens(c.in))
		}
	}
}

func TestCost(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "$0.00"},
		{0.18, "$0.18"},
		{2.41, "$2.41"},
		{340.5, "$340.50"},
		{999.99, "$999.99"},
		{1000, "$1000"},
		{1234.56, "$1234"},
	}
	for _, c := range cases {
		if got := Cost(c.in); got != c.want {
			t.Errorf("Cost(%v) = %q, want %q", c.in, got, c.want)
		}
		if n := len([]rune(Cost(c.in))); n > 7 {
			t.Errorf("Cost(%v) = %q exceeds the 7-column budget", c.in, Cost(c.in))
		}
	}
}

func TestAge(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{3 * time.Second, "3s"},
		{12 * time.Second, "12s"},
		{48 * time.Second, "48s"},
		{59900 * time.Millisecond, "59s"},
		{90 * time.Second, "1m"},
		{4 * time.Minute, "4m"},
		{22 * time.Minute, "22m"},
		{119 * time.Minute, "1h"},
		{2 * time.Hour, "2h"},
		{3 * 24 * time.Hour, "3d"},
	}
	for _, c := range cases {
		if got := Age(c.in); got != c.want {
			t.Errorf("Age(%v) = %q, want %q", c.in, got, c.want)
		}
		if n := len([]rune(Age(c.in))); n > 4 {
			t.Errorf("Age(%v) = %q exceeds the 4-column budget", c.in, Age(c.in))
		}
	}
}

func TestCountdownHasDaysBranch(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{2*time.Hour + 13*time.Minute, "2h13m"},
		{47 * time.Minute, "47m"},
		// The case the statusline's local shortDur gets wrong: a seven-day
		// window is 120+ hours and must not render as "120h00m".
		{5*24*time.Hour + 2*time.Hour, "5d02h"},
		{7 * 24 * time.Hour, "7d00h"},
		{30 * time.Second, "<1m"},
		{0, "<1m"},
		{-time.Hour, "<1m"},
	}
	for _, c := range cases {
		if got := Countdown(c.in); got != c.want {
			t.Errorf("Countdown(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSeverityBands(t *testing.T) {
	cases := []struct {
		in   float64
		want Severity
	}{
		{0, SevOK},
		{59.9, SevOK},
		{60, SevWarn},
		{84.9, SevWarn},
		{85, SevCrit},
		{100, SevCrit},
	}
	for _, c := range cases {
		if got := SeverityFor(c.in); got != c.want {
			t.Errorf("SeverityFor(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestWindowLabel(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{300, "5h"},   // Codex primary
		{10080, "7d"}, // Codex secondary
		{45, "45m"},
		{60, "1h"},
		{1440, "1d"},
		{0, ""}, // unknown length: caller must supply a positional label
		{-1, ""},
	}
	for _, c := range cases {
		got := WindowLabel(c.in)
		if got != c.want {
			t.Errorf("WindowLabel(%d) = %q, want %q", c.in, got, c.want)
		}
		if n := len([]rune(got)); n > 4 {
			t.Errorf("WindowLabel(%d) = %q exceeds the 4-cell budget", c.in, got)
		}
	}
}
