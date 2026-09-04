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

// TestTheTokenCountsAreParsedAndNeverRendered is the render half of §7.16b's
// ruling, and it guards a temptation rather than a bug.
//
// full.json now carries the real 2.1.233 token block, so the fields are
// sitting in the parsed struct one call away from the renderer. They must stay
// off the line. Every number in that block describes ONE API call — the last
// one — and `total_input_tokens` is the context window's occupancy, not a
// spend; putting any of it on a statusline beside a cost figure would read as
// "what this session has used", which is a claim the payload does not make.
//
// The counts are asserted individually rather than by diffing the whole line,
// because TestFullRender already pins the line exactly. This one says WHY the
// line may not grow, so the next person to add a segment reads the reason
// instead of just a failing string compare.
func TestTheTokenCountsAreParsedAndNeverRendered(t *testing.T) {
	in := load(t, "full.json")
	// Guard the premise: if the fixture ever loses the block, this test would
	// pass while asserting nothing.
	if in.ContextWindow == nil || in.ContextWindow.CurrentUsage == nil ||
		in.ContextWindow.TotalInputTokens == nil {
		t.Fatal("full.json no longer carries the 2.1.233 token block; this test would assert nothing")
	}

	got := renderPlain(t, "full.json")
	for _, n := range []string{"16000", "16.0k", "12000", "2800", "1200", "512"} {
		if strings.Contains(got, n) {
			t.Errorf("a per-API-call token count reached the statusline (%q in %q). "+
				"§7.16b: these are one call's numbers and total_input_tokens is an "+
				"occupancy level, so neither a spend nor a total may be built from them", n, got)
		}
	}
	// The prompt correlation id is likewise parsed and unrendered; it names a
	// prompt, and a statusline that printed it would be leaking an internal id
	// into a line the user reads at a glance.
	if strings.Contains(got, "cafe") {
		t.Errorf("prompt_id reached the statusline: %q", got)
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

// TestCacheSegmentRendersTheReportedRatio pins the segment's place on the line
// and its one arithmetic.
//
// `cache 91%` sits directly after `ctx 8%` on purpose: the two answer one
// question together — how full the window is, and how much of what fills it the
// vendor served from cache. The 91 is hit_ratio (0.91) times 100 and nothing
// else; the fixture's own cache_read_input_tokens (12000) does NOT produce it,
// which is the difference between reading a number and inventing one.
func TestCacheSegmentRendersTheReportedRatio(t *testing.T) {
	got := renderPlain(t, "prompt-cache.json")
	want := "Opus │ ctx 8% │ cache 91% │ $0.01 │ 5h 23.5% ↻ 2h13m │ 7d 41.2% ↻ 5d02h │ myproject"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

// TestTheCacheSegmentReadsTheRatioAndDerivesNothing guards the temptation the
// whole §7.16c ruling exists to refuse.
//
// prompt-cache.json carries BOTH the vendor's computed hit_ratio and the raw
// per-call counts it would take to compute one here. Only the reported quotient
// may reach the line. A ratio built from current_usage would be arithmetic
// telltale invented (ADR-001, §4a.1), and it would also be the WRONG number:
// current_usage describes one API call, while hit_ratio is taken over every
// main-conversation request of the session.
func TestTheCacheSegmentReadsTheRatioAndDerivesNothing(t *testing.T) {
	in := load(t, "prompt-cache.json")
	if in.PromptCache == nil || in.PromptCache.HitRatio == nil ||
		in.ContextWindow == nil || in.ContextWindow.CurrentUsage == nil {
		t.Fatal("prompt-cache.json no longer carries both the ratio and the raw counts; this test would assert nothing")
	}
	u := in.ContextWindow.CurrentUsage
	// The per-call ratio the fixture's counts would yield: 12000/(1200+2800+12000)
	// = 0.75. It differs from the reported 0.91 by construction, so a renderer
	// that silently switched sources fails here instead of drifting quietly.
	perCall := float64(*u.CacheReadInputTokens) /
		float64(*u.InputTokens+*u.CacheCreationInputTokens+*u.CacheReadInputTokens)
	if perCall == *in.PromptCache.HitRatio {
		t.Fatal("the fixture's per-call ratio now equals the reported one; " +
			"this test can no longer tell a derivation from a reading")
	}

	got := renderPlain(t, "prompt-cache.json")
	for _, n := range []string{"75%", "352000", "310200", "45000", "14", "2800", "12000"} {
		if strings.Contains(got, n) {
			t.Errorf("a number the cache segment may not source reached the line (%q in %q). "+
				"§7.16c: hit_ratio is the ONE reported figure here; the counts, the write "+
				"totals and the request count are parsed or dropped, never rendered", n, got)
		}
	}
}

// TestTheCacheSegmentHidesOnEveryAbsence covers the three ways the number is
// not there, and none of them may render as zero.
//
// The last case is the one worth stating: a session whose provider reports no
// cache tokens has a real prompt_cache block with a real request count and a
// null ratio. `cache 0%` there would claim a measurement nobody took. A ratio
// of 0 WITH caching observed is a different thing entirely — it is a reading,
// and it renders.
func TestTheCacheSegmentHidesOnEveryAbsence(t *testing.T) {
	yes, no := true, false
	zero, ratio := 0.0, 0.91
	for _, tc := range []struct {
		name string
		pc   *claude.PromptCache
		want string
	}{
		{"no block at all (pre-2.1.251, or before the first response)", nil, ""},
		{"block present, ratio still null", &claude.PromptCache{CachingObserved: &yes}, ""},
		{"provider reports no cache tokens", &claude.PromptCache{CachingObserved: &no, HitRatio: &ratio}, ""},
		{"a measured zero is a reading", &claude.PromptCache{CachingObserved: &yes, HitRatio: &zero}, "cache 0%"},
		{"a measured ratio renders", &claude.PromptCache{CachingObserved: &yes, HitRatio: &ratio}, "cache 91%"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := &claude.StatuslineInput{
				Model:       claude.Model{DisplayName: "Opus"},
				PromptCache: tc.pc,
			}
			got := Render(in, Options{NoColor: true, Now: testNow})
			if tc.want == "" {
				if strings.Contains(got, "cache") {
					t.Errorf("segment rendered on an absent source: %q", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("got %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

// TestTheCacheRatioCarriesNoThresholdColor is a rule, not a preference.
//
// Every other percentage on this line is a consumption, so pct() paints high
// values red. A cache hit ratio inverts that — 91% is the healthy end — and
// nothing here has measured the ratio at which a cache becomes bad. So the
// value renders unpainted, and the word `cache` carries the distinction, which
// is what this UI asks of every distinction anyway.
func TestTheCacheRatioCarriesNoThresholdColor(t *testing.T) {
	yes := true
	for _, r := range []float64{0.02, 0.65, 0.91} {
		ratio := r
		in := &claude.StatuslineInput{
			Model:       claude.Model{DisplayName: "Opus"},
			PromptCache: &claude.PromptCache{CachingObserved: &yes, HitRatio: &ratio},
		}
		got := Render(in, Options{Now: testNow})
		i := strings.Index(got, "cache ")
		if i < 0 {
			t.Fatalf("no cache segment at ratio %v: %q", r, got)
		}
		if strings.ContainsAny(got[i:], "\x1b") {
			t.Errorf("the cache ratio carries styling at %v (%q); pct()'s scale reads "+
				"high-is-bad and would paint a well-cached session red", r, got[i:])
		}
	}
}
