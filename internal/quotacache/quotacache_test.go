package quotacache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/antigravity"
	"github.com/sanlee-ys/telltale/internal/claude"
	"github.com/sanlee-ys/telltale/internal/model"
)

var pinned = time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

func fp(v float64) *float64 { return &v }

func tp(t time.Time) *time.Time { return &t }

func TestWriteThenReadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	reset := pinned.Add(2 * time.Hour)
	err := Write(dir, "claude", []Window{
		{ID: "five_hour", Label: "5h", UsedPercent: fp(42), ResetsAt: tp(reset)},
		{ID: "seven_day", Label: "7d", UsedPercent: fp(6)},
	}, pinned)
	if err != nil {
		t.Fatal(err)
	}

	got := ReadAll(dir, pinned.Add(time.Minute))
	if len(got) != 1 {
		t.Fatalf("ReadAll = %v", got)
	}
	a := got[0]
	if a.Vendor != model.VendorClaude || !a.WrittenAt.Equal(pinned) {
		t.Errorf("account = %+v", a)
	}
	if len(a.Windows) != 2 || a.Windows[0].Label != "5h" || float64(*a.Windows[0].UsedPercent) != 42 {
		t.Errorf("windows = %+v", a.Windows)
	}
	if a.Windows[0].ResetsAt == nil || !a.Windows[0].ResetsAt.Equal(reset) {
		t.Errorf("reset did not round-trip: %+v", a.Windows[0])
	}
}

// A window whose reset has passed describes a window that no longer exists:
// its percentage is not stale, it is false, and rendering it would be the
// relay presenting last night's exhausted window as this morning's.
func TestAnExpiredWindowIsDroppedAtRead(t *testing.T) {
	dir := t.TempDir()
	err := Write(dir, "claude", []Window{
		{ID: "five_hour", Label: "5h", UsedPercent: fp(100), ResetsAt: tp(pinned.Add(30 * time.Minute))},
		{ID: "seven_day", Label: "7d", UsedPercent: fp(6), ResetsAt: tp(pinned.Add(72 * time.Hour))},
	}, pinned)
	if err != nil {
		t.Fatal(err)
	}

	got := ReadAll(dir, pinned.Add(time.Hour))
	if len(got) != 1 || len(got[0].Windows) != 1 || got[0].Windows[0].ID != "seven_day" {
		t.Fatalf("the expired five_hour window survived: %+v", got)
	}
	// Once every window has expired the whole entry is gone, not an empty block.
	if got := ReadAll(dir, pinned.Add(80*time.Hour)); len(got) != 0 {
		t.Fatalf("an entry with no live windows rendered anyway: %+v", got)
	}
}

func TestStaleAndSkewedEntriesAreDropped(t *testing.T) {
	dir := t.TempDir()
	// No reset times, so only the age rules govern.
	if err := Write(dir, "claude", []Window{{ID: "seven_day", Label: "7d", UsedPercent: fp(6)}}, pinned); err != nil {
		t.Fatal(err)
	}
	if got := ReadAll(dir, pinned.Add(maxAge+time.Minute)); len(got) != 0 {
		t.Fatalf("a reading older than maxAge rendered: %+v", got)
	}
	// A future timestamp beyond skew tolerance means a clock we cannot reason
	// about — mirror of the adapters' future-skew guard.
	if got := ReadAll(dir, pinned.Add(-futureSkew-time.Minute)); len(got) != 0 {
		t.Fatalf("a future-stamped reading rendered: %+v", got)
	}
	if got := ReadAll(dir, pinned.Add(-time.Minute)); len(got) != 1 {
		t.Fatalf("jitter within futureSkew should be tolerated: %+v", got)
	}
}

// The cache is numbers only, never content — the room.json standard. The
// serialized form is asserted field by field so a future Entry field that
// could carry session text has to fight this test to ship.
func TestTheCacheCarriesKeysAndNumbersOnly(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, "claude", []Window{{ID: "five_hour", Label: "5h", UsedPercent: fp(42)}}, pinned); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	for k := range top {
		switch k {
		case "vendor", "written_at", "windows":
		default:
			t.Errorf("unexpected top-level cache field %q", k)
		}
	}
	var windows []map[string]json.RawMessage
	if err := json.Unmarshal(top["windows"], &windows); err != nil {
		t.Fatal(err)
	}
	for _, w := range windows {
		for k := range w {
			switch k {
			case "id", "label", "used_percent", "resets_at":
			default:
				t.Errorf("unexpected window cache field %q", k)
			}
		}
	}
}

// Nothing to say, nothing written: an entry of empty windows would assert
// "I measured nothing" — which is what not writing says for free — and a
// malformed sibling file must not take the readable ones down with it.
func TestEmptyWritesAndMalformedFilesAreNonEvents(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, "claude", nil, pinned); err != nil {
		t.Fatal(err)
	}
	if err := Write(dir, "claude", []Window{{ID: "x", Label: "x"}}, pinned); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("an empty write left a file: %v", entries)
	}

	if err := os.WriteFile(filepath.Join(dir, "junk.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(dir, "agy", []Window{{ID: "gemini-weekly", Label: "gemini-weekly", UsedPercent: fp(38)}}, pinned); err != nil {
		t.Fatal(err)
	}
	got := ReadAll(dir, pinned)
	if len(got) != 1 || got[0].Vendor != model.VendorAntigravity {
		t.Fatalf("a malformed sibling changed the result: %+v", got)
	}
}

func TestReadAllOrdersByVendor(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []string{"claude", "agy"} {
		if err := Write(dir, v, []Window{{ID: "w", Label: "w", UsedPercent: fp(1)}}, pinned); err != nil {
			t.Fatal(err)
		}
	}
	got := ReadAll(dir, pinned)
	if len(got) != 2 || got[0].Vendor != model.VendorAntigravity || got[1].Vendor != model.VendorClaude {
		t.Fatalf("order = %+v", got)
	}
}

func TestFromClaudeMatchesTheStatuslineVocabulary(t *testing.T) {
	resets := pinned.Add(2 * time.Hour).Unix()
	rl := &claude.RateLimits{
		FiveHour: &claude.Window{UsedPercentage: fp(42), ResetsAt: &resets},
		SevenDay: &claude.Window{UsedPercentage: fp(6)},
	}
	got := FromClaude(rl, pinned)
	if len(got) != 2 || got[0].ID != "five_hour" || got[0].Label != "5h" ||
		got[1].ID != "seven_day" || got[1].Label != "7d" {
		t.Fatalf("FromClaude = %+v", got)
	}
	if got[0].ResetsAt == nil || !got[0].ResetsAt.Equal(time.Unix(resets, 0)) {
		t.Errorf("reset instant = %+v", got[0].ResetsAt)
	}
	// An API-key login has no rate_limits at all; nil must convert to nothing
	// rather than a zeroed window (the statusline's own hide-never-zero rule).
	if got := FromClaude(nil, pinned); got != nil {
		t.Errorf("FromClaude(nil) = %+v", got)
	}
}

func TestFromAntigravityRelaysBucketsVerbatim(t *testing.T) {
	sec := int64(3 * 3600)
	quota := map[string]*antigravity.QuotaBucket{
		"gemini-weekly": {RemainingFraction: fp(0.62), ResetInSeconds: &sec},
		"3p-weekly":     {RemainingFraction: nil}, // no reading: hides, never 0%
	}
	got := FromAntigravity(quota, pinned)
	if len(got) != 1 || got[0].ID != "gemini-weekly" || got[0].Label != "gemini-weekly" {
		t.Fatalf("FromAntigravity = %+v", got)
	}
	if used := *got[0].UsedPercent; used < 37.9 || used > 38.1 {
		t.Errorf("used = %v, want (1-remaining)*100", used)
	}
	if got[0].ResetsAt == nil || !got[0].ResetsAt.Equal(pinned.Add(3*time.Hour)) {
		t.Errorf("reset = %+v", got[0].ResetsAt)
	}
}

// The write is atomic: a reader never sees half an entry. The mechanism
// (temp + rename) is asserted by its observable consequence — no temp files
// left behind, and the target readable immediately after Write returns.
func TestWriteLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, "claude", []Window{{ID: "w", Label: "w", UsedPercent: fp(1)}}, pinned); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
	if len(entries) != 1 || entries[0].Name() != "claude.json" {
		t.Errorf("dir = %v", entries)
	}
}
