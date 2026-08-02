package statusline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/claude"
)

// The clock is pinned so countdown segments are deterministic.
// full.json's five_hour resets_at is 1754074800 (2026-08-01 19:00:00 UTC);
// pinning "now" 2h13m earlier asserts the countdown arithmetic exactly.
var testNow = time.Unix(1754074800, 0).Add(-2*time.Hour - 13*time.Minute)

func load(t *testing.T, name string) *claude.StatuslineInput {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	in, err := claude.Parse(f)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return in
}

func renderPlain(t *testing.T, fixture string) string {
	t.Helper()
	return Render(load(t, fixture), Options{NoColor: true, Now: testNow})
}

func TestFullRender(t *testing.T) {
	got := renderPlain(t, "full.json")
	// The 7d countdown is exact now that the formatter is shared
	// theme.Countdown (the old local shortDur had no days branch and rendered
	// "122h13m"). Glyph and digits are separated by a space — glued, ↻ reads
	// as one garbled token in ambiguous-width fonts (dogfood, 2026-08-02).
	want := "Opus │ ctx 8% │ $0.01 │ 5h 23.5% ↻ 2h13m │ 7d 41.2% ↻ 5d02h │ myproject"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

// The load-bearing honest-gauge test: API-key logins have NO rate_limits.
// The quota segments must be absent — not "5h 0%".
func TestAPIKeyHidesQuotaSegments(t *testing.T) {
	got := renderPlain(t, "apikey.json")
	if strings.Contains(got, "5h") || strings.Contains(got, "7d") {
		t.Fatalf("quota segments must hide when rate_limits absent; got %q", got)
	}
	if !strings.Contains(got, "ctx 62.4%") {
		t.Fatalf("context segment missing: %q", got)
	}
	if strings.Contains(got, "0%%") {
		t.Fatalf("absent data rendered as zero: %q", got)
	}
}

// Each window is independently absent (documented) — five_hour present
// without seven_day must render only five_hour; a window without resets_at
// renders its percentage without a countdown.
func TestPartialWindow(t *testing.T) {
	got := renderPlain(t, "partial-window.json")
	if !strings.Contains(got, "5h 88%") {
		t.Fatalf("five_hour missing: %q", got)
	}
	if strings.Contains(got, "7d") {
		t.Fatalf("absent seven_day must not render: %q", got)
	}
	if strings.Contains(got, "↻") {
		t.Fatalf("countdown must not render without resets_at: %q", got)
	}
}

// A payload with nothing but a model still renders the model, and nothing else.
func TestMinimal(t *testing.T) {
	got := renderPlain(t, "minimal.json")
	if got != "claude-opus-5" {
		t.Fatalf("minimal render: got %q", got)
	}
}

func TestDirSegment(t *testing.T) {
	got := renderPlain(t, "full.json")
	if !strings.Contains(got, "myproject") {
		t.Fatalf("dir segment missing: %q", got)
	}
	// minimal.json has no cwd/workspace — dir segment must hide.
	if got := renderPlain(t, "minimal.json"); strings.Contains(got, "│") {
		t.Fatalf("minimal must render model only: %q", got)
	}
}

func TestWorktreeSegment(t *testing.T) {
	got := renderPlain(t, "worktree.json")
	if !strings.Contains(got, "⌥my-feature") {
		t.Fatalf("worktree segment missing: %q", got)
	}
}

// Threshold coloring: colored output must use green under 60, yellow from 60,
// red from 85 (docs/design.md §2).
func TestThresholdColors(t *testing.T) {
	cases := []struct {
		fixture string
		code    string
		label   string
	}{
		{"full.json", "\x1b[32m", "green under warn (ctx 8%)"},
		{"apikey.json", "\x1b[33m", "yellow from 60 (ctx 62.4%)"},
		{"partial-window.json", "\x1b[31m", "red from 85 (ctx 91%)"},
	}
	for _, c := range cases {
		got := Render(load(t, c.fixture), Options{Now: testNow})
		if !strings.Contains(got, c.code) {
			t.Errorf("%s: expected %q in %q", c.label, c.code, got)
		}
	}
}

// BenchmarkRender isolates parse+render cost from process-spawn cost. On
// Windows, spawning any exe costs ~15-30ms (OS floor); this benchmark proves
// telltale's own work is negligible against the 300ms debounce budget.
func BenchmarkRender(b *testing.B) {
	data, err := os.ReadFile(filepath.Join("testdata", "full.json"))
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		in, err := claude.Parse(strings.NewReader(string(data)))
		if err != nil {
			b.Fatal(err)
		}
		Render(in, Options{Now: testNow})
	}
}

// Unknown fields in the payload must not break parsing (vendor adds fields
// between versions).
func TestUnknownFieldsIgnored(t *testing.T) {
	in, err := claude.Parse(strings.NewReader(
		`{"model":{"id":"m","display_name":"M"},"future_field":{"x":1}}`))
	if err != nil {
		t.Fatalf("unknown fields must be ignored: %v", err)
	}
	if got := Render(in, Options{NoColor: true, Now: testNow}); got != "M" {
		t.Fatalf("got %q", got)
	}
}
