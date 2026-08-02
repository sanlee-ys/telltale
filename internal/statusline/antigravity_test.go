package statusline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/antigravity"
)

// agy countdowns come from the vendor's relative reset_in_seconds, so no
// clock pinning is needed for them; the pinned Now only guards the absolute
// reset_time fallback.
var agyTestNow = time.Date(2026, 8, 2, 20, 0, 0, 0, time.UTC)

func loadAgy(t *testing.T, name string) *antigravity.StatuslineInput {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	in, err := antigravity.Parse(f)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return in
}

func renderAgyPlain(t *testing.T, fixture string) string {
	t.Helper()
	return RenderAntigravity(loadAgy(t, fixture), Options{NoColor: true, Now: agyTestNow})
}

func TestAgyFullRender(t *testing.T) {
	got := renderAgyPlain(t, "agy-full.json")
	// Bucket order is sorted ("3p-weekly" before "gemini-weekly"); used% is
	// the unit conversion (1-remaining)*100; the countdown is the vendor's
	// own reset_in_seconds; the branch comes from the payload's vcs object
	// with the dirty star; the state word is the vendor-reported agent_state.
	want := "Gemini 3.6 Flash (High) │ ctx 3.1% │ 3p-weekly 0% ↻ 6d23h │ gemini-weekly 25% ↻ 6d23h │ working │ main* │ example-app"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

// A pending tool confirmation outranks the state word: "waiting on you" is
// the fact the user needs, and tool_use would bury it.
func TestAgyConfirmationOutranksState(t *testing.T) {
	got := renderAgyPlain(t, "agy-confirm.json")
	if !strings.Contains(got, "confirm?") {
		t.Fatalf("confirmation-pending marker missing: %q", got)
	}
	if strings.Contains(got, "tool") {
		t.Fatalf("state word must yield to the confirmation marker: %q", got)
	}
}

// The zero-vs-absent pair, statusline edition: used_percentage of 0 is a
// reading and renders "ctx 0%"; an absent quota map hides every bucket.
func TestAgyZeroContextRendersAndAbsentQuotaHides(t *testing.T) {
	got := renderAgyPlain(t, "agy-no-quota.json")
	if !strings.Contains(got, "ctx 0%") {
		t.Fatalf("a zero context reading is data and must render: %q", got)
	}
	if strings.Contains(got, "weekly") || strings.Contains(got, "↻") {
		t.Fatalf("absent quota must hide the bucket segments: %q", got)
	}
	if !strings.Contains(got, "idle") {
		t.Fatalf("idle state missing: %q", got)
	}
}

// A payload with nothing but a model still renders the model, and nothing else.
func TestAgyMinimal(t *testing.T) {
	if got := renderAgyPlain(t, "agy-minimal.json"); got != "gemini-3-flash" {
		t.Fatalf("minimal render: got %q", got)
	}
}

// The vendor's state vocabulary may grow; an unknown state renders verbatim
// rather than vanishing — it is still the vendor's truth.
func TestAgyUnknownStateRendersVerbatim(t *testing.T) {
	in, err := antigravity.Parse(strings.NewReader(
		`{"product":"antigravity","model":{"id":"m"},"agent_state":"compacting"}`))
	if err != nil {
		t.Fatal(err)
	}
	got := RenderAntigravity(in, Options{NoColor: true, Now: agyTestNow})
	if !strings.Contains(got, "compacting") {
		t.Fatalf("unknown state must render verbatim: %q", got)
	}
}

// A bucket without remaining_fraction hides entirely: never 0%, never 100%.
func TestAgyBucketWithoutRemainingHides(t *testing.T) {
	in, err := antigravity.Parse(strings.NewReader(
		`{"product":"antigravity","model":{"id":"m"},"quota":{"gemini-weekly":{"reset_in_seconds":600}}}`))
	if err != nil {
		t.Fatal(err)
	}
	got := RenderAntigravity(in, Options{NoColor: true, Now: agyTestNow})
	if strings.Contains(got, "weekly") {
		t.Fatalf("a bucket without a reading must hide: %q", got)
	}
}

// The absolute reset_time fallback: a bucket with reset_time but no
// reset_in_seconds still gets its countdown, computed against Options.Now.
func TestAgyResetTimeFallback(t *testing.T) {
	in, err := antigravity.Parse(strings.NewReader(
		`{"product":"antigravity","model":{"id":"m"},"quota":{"gemini-weekly":{"remaining_fraction":0.5,"reset_time":"2026-08-02T22:13:00Z"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	got := RenderAntigravity(in, Options{NoColor: true, Now: agyTestNow})
	if !strings.Contains(got, "gemini-weekly 50% ↻ 2h13m") {
		t.Fatalf("reset_time fallback countdown missing: %q", got)
	}
}

// The single-JSON-value framing note holds for this vendor too: a raw U+2028
// inside a string value must parse and render without tearing anything. The
// character below is the real thing, produced by the Go escape.
func TestAgyU2028InPayloadStrings(t *testing.T) {
	payload := "{\"product\":\"antigravity\",\"model\":{\"id\":\"line\u2028sep\"}}"
	if !strings.Contains(payload, "\u2028") {
		t.Fatal("test payload lost its U+2028")
	}
	in, err := antigravity.Parse(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("U+2028 in a string value broke the parse: %v", err)
	}
	got := RenderAntigravity(in, Options{NoColor: true, Now: agyTestNow})
	if !strings.Contains(got, "line") || !strings.Contains(got, "sep") {
		t.Fatalf("payload with U+2028 did not render: %q", got)
	}
}

// The routing marker the cmd layer depends on: the vendor stamps this exact
// value on every observed payload (live capture, 2026-08-02).
func TestAgyProductConstant(t *testing.T) {
	if antigravity.Product != "antigravity" {
		t.Fatalf("Product = %q", antigravity.Product)
	}
	in := loadAgy(t, "agy-full.json")
	if in.Product != antigravity.Product {
		t.Fatalf("fixture product = %q", in.Product)
	}
}
