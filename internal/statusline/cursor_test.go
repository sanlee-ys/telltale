package statusline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/cursorstatus"
)

// This vendor's payload carries no clock-bearing field at all — no quota, no
// reset — so Now only exists to keep Render deterministic.
var cursorTestNow = time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC)

func loadCursor(t *testing.T, name string) *cursorstatus.StatuslineInput {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	in, err := cursorstatus.Parse(f)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return in
}

func renderCursorPlain(t *testing.T, fixture string) string {
	t.Helper()
	return RenderCursor(loadCursor(t, fixture), Options{NoColor: true, Now: cursorTestNow})
}

func TestCursorFullRender(t *testing.T) {
	got := renderCursorPlain(t, "cursor-full.json")
	want := "Composer 2.5 Fast │ ctx 34.5% │ autorun │ ⌥my-feature │ example-app"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

// THE test for this vendor. Its payload ships two numbers the CLI computes from
// a third and names as though they were read: `remaining_percentage` (100 −
// used) and `total_input_tokens` (used% × window size, which the vendor's own
// skill doc calls "derived from used_percentage"). Rendering either one would be
// the ADR-001 violation print mode's `inputTokens` already cost this repo once.
//
// The fixture carries both, populated and plausible. Neither may appear —
// and the guard is structural rather than conditional: they are not fields on
// internal/cursorstatus.StatuslineInput, so encoding/json drops them at the
// parse and there is nothing for a later render change to reach for.
func TestCursorDerivedFieldsNeverRender(t *testing.T) {
	got := renderCursorPlain(t, "cursor-full.json")
	// 65.5 is remaining_percentage; 68900 / 68.9k is total_input_tokens.
	for _, banned := range []string{"65.5", "68900", "68.9k", "200000", "200k"} {
		if strings.Contains(got, banned) {
			t.Fatalf("a CLI-computed value reached the line: %q in %q", banned, got)
		}
	}
	if !strings.Contains(got, "34.5%") {
		t.Fatalf("the one vendor-sourced reading must render: %q", got)
	}
}

// The shape a real session hands over before its first API call — measured, and
// the reason the context segment cannot be written to assume a number. Every
// context_window key arrives null. That is a read this gauge could not get, not
// a measured zero, so the segment hides entirely rather than drawing `ctx 0%`.
func TestCursorNullContextHidesSegment(t *testing.T) {
	got := renderCursorPlain(t, "cursor-nocontext.json")
	if strings.Contains(got, "ctx") {
		t.Fatalf("an unread context must hide, never render as zero: %q", got)
	}
	if !strings.Contains(got, "Composer 2.5 Fast") || !strings.Contains(got, "example-app") {
		t.Fatalf("the rest of the line must still render: %q", got)
	}
}

// Zero and absent stay distinguishable on the one field that can carry a
// reading: a measured 0 is data and renders.
func TestCursorZeroContextRenders(t *testing.T) {
	in := loadCursor(t, "cursor-nocontext.json")
	zero := 0.0
	in.ContextWindow = &cursorstatus.ContextWindow{UsedPercentage: &zero}
	got := RenderCursor(in, Options{NoColor: true, Now: cursorTestNow})
	if !strings.Contains(got, "ctx 0%") {
		t.Fatalf("a zero context reading is data and must render: %q", got)
	}
}

// autorun off and autorun absent both render nothing; only on spends a segment.
// See cursorAutorunSegment for why that asymmetry is not the zero-vs-absent rule
// being bent — this is a posture with a default, not a gauge reading.
func TestCursorAutorunOnlyRendersWhenOn(t *testing.T) {
	if got := renderCursorPlain(t, "cursor-nocontext.json"); strings.Contains(got, "autorun") {
		t.Fatalf("autorun:false must render nothing: %q", got)
	}
	if got := renderCursorPlain(t, "cursor-minimal.json"); strings.Contains(got, "autorun") {
		t.Fatalf("an absent autorun must render nothing: %q", got)
	}
	if got := renderCursorPlain(t, "cursor-full.json"); !strings.Contains(got, "autorun") {
		t.Fatalf("autorun:true must render: %q", got)
	}
}

// A payload with nothing renderable but a model id still renders the model, and
// nothing else — including when display_name is present but empty.
func TestCursorMinimal(t *testing.T) {
	if got := renderCursorPlain(t, "cursor-minimal.json"); got != "composer-2.5" {
		t.Fatalf("minimal render: got %q", got)
	}
}

// The BOM is defensive, and this pins that it is actually defended. The
// statusline payload was MEASURED without one; the same vendor's hook payload
// has one and it broke a plain parse (design.md §7.16).
func TestCursorBOMIsStripped(t *testing.T) {
	payload := "\xef\xbb\xbf" + `{"model":{"id":"composer-2.5"},"cwd":"/fake/code/example-app"}`
	in, err := cursorstatus.Parse(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("a BOM-prefixed payload must parse: %v", err)
	}
	got := RenderCursor(in, Options{NoColor: true, Now: cursorTestNow})
	if !strings.Contains(got, "composer-2.5") || !strings.Contains(got, "example-app") {
		t.Fatalf("BOM-prefixed payload did not render: %q", got)
	}
}

// The same single-JSON-value framing note as the other two vendors: a raw
// U+2028 inside a string value must parse and render without tearing anything.
func TestCursorU2028InPayloadStrings(t *testing.T) {
	payload := "{\"model\":{\"id\":\"line\u2028sep\"}}"
	if !strings.Contains(payload, "\u2028") {
		t.Fatal("test payload lost its U+2028")
	}
	in, err := cursorstatus.Parse(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("U+2028 in a string value broke the parse: %v", err)
	}
	got := RenderCursor(in, Options{NoColor: true, Now: cursorTestNow})
	if !strings.Contains(got, "line") || !strings.Contains(got, "sep") {
		t.Fatalf("payload with U+2028 did not render: %q", got)
	}
}

// Threshold coloring holds on this path too: the ctx segment shares the pct
// helper, so green under warn, yellow from 60, red from 85.
func TestCursorThresholdColors(t *testing.T) {
	cases := []struct {
		ctx         float64
		code, label string
	}{
		{8, "\x1b[32m", "green under warn"},
		{72, "\x1b[33m", "yellow from 60"},
		{91, "\x1b[31m", "red from 85"},
	}
	for _, c := range cases {
		in := loadCursor(t, "cursor-minimal.json")
		ctx := c.ctx
		in.ContextWindow = &cursorstatus.ContextWindow{UsedPercentage: &ctx}
		got := RenderCursor(in, Options{Now: cursorTestNow})
		if !strings.Contains(got, c.code) {
			t.Errorf("%s: expected %q in %q", c.label, c.code, got)
		}
	}
}

// Same purpose as BenchmarkRender: parse+render cost isolated from process
// spawn, proving the third vendor stays inside the millisecond budget. The
// vendor's 2000ms timeout is its kill deadline, not this path's allowance —
// the binary is respawned on a 300ms debounce.
func BenchmarkRenderCursor(b *testing.B) {
	data, err := os.ReadFile(filepath.Join("testdata", "cursor-full.json"))
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		in, err := cursorstatus.Parse(strings.NewReader(string(data)))
		if err != nil {
			b.Fatal(err)
		}
		RenderCursor(in, Options{Now: cursorTestNow})
	}
}
