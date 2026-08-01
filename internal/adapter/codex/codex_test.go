package codex

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

const (
	healthyID  = "00000000-bbbb-4ccc-8ddd-000000000002"
	apiKeyID   = "00000000-bbbb-4ccc-8ddd-000000000003"
	subAgentID = "00000000-bbbb-4ccc-8ddd-000000000004"
)

func testAdapter(t *testing.T) *Adapter {
	t.Helper()
	return NewWithRoot("testdata")
}

func refByID(t *testing.T, refs []model.SessionRef, id string) model.SessionRef {
	t.Helper()
	for _, r := range refs {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no ref with id %q in %d refs", id, len(refs))
	return model.SessionRef{}
}

func TestFixtureBytesArePreserved(t *testing.T) {
	p := filepath.Join("testdata", "sessions", "2026", "08", "01",
		"rollout-2026-08-01T09-12-33-"+healthyID+".jsonl")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte{0xE2, 0x80, 0xA8}) {
		t.Error("fixture lost its raw U+2028; the framing test can no longer fail")
	}
	if bytes.Contains(b, []byte{'\r'}) {
		t.Error("fixture has CRLF line endings; framing is defined on 0x0A alone")
	}
	if b[len(b)-1] == '\n' {
		t.Error("fixture gained a trailing newline; the torn final record is now a complete one")
	}
}

func TestCapabilitiesInvertAgainstClaude(t *testing.T) {
	caps := testAdapter(t).Capabilities()
	if err := caps.Validate(); err != nil {
		t.Fatal(err)
	}
	want := map[model.Field]model.Capability{
		model.FieldModel:        model.CapReported,
		model.FieldWorkspace:    model.CapReported,
		model.FieldLastActivity: model.CapReported,
		// Codex ships rate limits on the disk seam where Claude ships none.
		model.FieldQuota: model.CapReported,
		// Codex ships the denominator, so a percentage is computable — by us,
		// which makes it derived and marks it as an estimate in the HUD.
		model.FieldContextPercent: model.CapDerived,
		// No session title, no dollars, no liveness signal in the format.
		model.FieldName:     model.CapNone,
		model.FieldCost:     model.CapNone,
		model.FieldLiveness: model.CapNone,
	}
	for f, w := range want {
		if got := caps.Capability(f); got != w {
			t.Errorf("capability(%s) = %s, want %s", f, got, w)
		}
	}
}

func TestDiscoverSkipsCompressedLockAndTmpNeighbours(t *testing.T) {
	refs, err := testAdapter(t).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(refs))
	for _, r := range refs {
		ids = append(ids, r.ID)
	}
	sort.Strings(ids)

	want := []string{healthyID, apiKeyID, subAgentID}
	sort.Strings(want)
	if len(ids) != len(want) {
		t.Fatalf("discovered %v, want %v — the .jsonl.zst and rollout-compression.lock neighbours are not sessions", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("discovered %v, want %v", ids, want)
		}
	}
}

func TestDiscoverReportsVendorAbsent(t *testing.T) {
	// The state of the dev machine: no ~/.codex at all.
	_, err := NewWithRoot(filepath.Join("testdata", "no-such-codex-home")).Discover(context.Background())
	if !errors.Is(err, model.ErrVendorAbsent) {
		t.Fatalf("Discover on a missing CODEX_HOME returned %v, want ErrVendorAbsent", err)
	}
}

func TestReadHealthySession(t *testing.T) {
	a := testAdapter(t)
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s, err := a.Read(context.Background(), refByID(t, refs, healthyID))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(a.Capabilities()); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// The model lives on turn_context, not on session_meta.
	if got, _ := s.Model.Name(); got != "gpt-fixture-codex" {
		t.Errorf("model = %q", got)
	}
	if got, ok := s.WorkspaceName(); !ok || got != "example-app" {
		t.Errorf("workspace = %q ok=%v", got, ok)
	}

	// Derived from last_token_usage.total_tokens over model_context_window:
	// 189888 / 272000. Deliberately NOT the vendor's baseline-normalized
	// figure, which is a different statistic.
	if s.ContextPercent == nil {
		t.Fatal("context_pct absent; Codex ships the denominator")
	}
	want := 189888.0 / 272000.0 * 100
	if math.Abs(float64(*s.ContextPercent)-want) > 1e-9 {
		t.Errorf("context_pct = %v, want %v", float64(*s.ContextPercent), want)
	}
	if !s.Derived.Has(model.FieldContextPercent) {
		t.Error("context_pct must be marked derived so the HUD renders an estimate marker")
	}

	// The last token_count carried primary at 88.4% and secondary null: the
	// second window is ABSENT from the slice, not present at zero.
	if len(s.Quota) != 1 {
		t.Fatalf("quota windows = %d (%v), want only the primary window", len(s.Quota), s.Quota)
	}
	w := s.Quota[0]
	if w.ID != "primary" || w.Label != "5h" {
		t.Errorf("window = %+v, want id=primary label=5h (from window_minutes 300)", w)
	}
	if w.UsedPercent == nil || float64(*w.UsedPercent) != 88.4 {
		t.Errorf("used_percent = %v, want 88.4", w.UsedPercent)
	}
	if w.ResetsAt == nil {
		t.Error("resets_at absent")
	}

	// Not in the format at all.
	if s.Cost != nil {
		t.Errorf("cost = %v, want nil", *s.Cost)
	}
	if s.Name != nil {
		t.Errorf("name = %q, want nil (Codex has no session title)", *s.Name)
	}
	if s.LivenessHint != nil {
		t.Errorf("liveness hint = %v, want nil", *s.LivenessHint)
	}
}

// The Codex analogue of Claude's API-key-login fixture: token counts present,
// rate_limits null. Quota must be absent, never rendered as a zeroed window.
func TestNullRateLimitsYieldNoQuotaWindows(t *testing.T) {
	a := testAdapter(t)
	refs, _ := a.Discover(context.Background())
	s, err := a.Read(context.Background(), refByID(t, refs, apiKeyID))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(a.Capabilities()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(s.Quota) != 0 {
		t.Fatalf("quota = %v, want none — rate_limits was null", s.Quota)
	}
	if s.Has(model.FieldQuota) {
		t.Error("quota reported present with no windows")
	}
	// Context still resolves: the two data are independent.
	if s.ContextPercent == nil {
		t.Error("context_pct absent though info was present")
	}
}

func TestSubAgentThreadIsNotASession(t *testing.T) {
	a := testAdapter(t)
	refs, _ := a.Discover(context.Background())
	_, err := a.Read(context.Background(), refByID(t, refs, subAgentID))
	if !errors.Is(err, ErrSubAgentThread) {
		t.Fatalf("Read of a sub-agent rollout returned %v, want ErrSubAgentThread", err)
	}
}

func TestTornTailChangesNothing(t *testing.T) {
	a := testAdapter(t)
	refs, _ := a.Discover(context.Background())
	ref := refByID(t, refs, healthyID)

	full, err := a.Read(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(ref.Locator)
	if err != nil {
		t.Fatal(err)
	}
	cut := raw[:bytes.LastIndexByte(raw, '\n')+1]

	dir := t.TempDir()
	day := filepath.Join(dir, "sessions", "2026", "08", "01")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "rollout-2026-08-01T09-12-33-" + healthyID + ".jsonl"
	if err := os.WriteFile(filepath.Join(day, name), cut, 0o644); err != nil {
		t.Fatal(err)
	}
	b := NewWithRoot(dir)
	brefs, _ := b.Discover(context.Background())
	trimmed, err := b.Read(context.Background(), refByID(t, brefs, healthyID))
	if err != nil {
		t.Fatal(err)
	}

	if full.Present() != trimmed.Present() {
		t.Fatalf("torn tail changed which fields are present: %s vs %s", full.Present(), trimmed.Present())
	}
	if (full.ContextPercent == nil) != (trimmed.ContextPercent == nil) ||
		(full.ContextPercent != nil && *full.ContextPercent != *trimmed.ContextPercent) {
		t.Fatal("torn tail changed the context reading")
	}
	if len(full.Quota) != len(trimmed.Quota) {
		t.Fatal("torn tail changed the quota windows")
	}
	if len(full.Diagnostics) != 0 {
		t.Errorf("a torn tail is not a parse failure, but produced diagnostics: %v", full.Diagnostics)
	}
}

func TestReadReportsSessionGone(t *testing.T) {
	_, err := testAdapter(t).Read(context.Background(), model.SessionRef{
		Vendor:  Vendor,
		ID:      healthyID,
		Locator: filepath.Join("testdata", "sessions", "1999", "01", "01", "rollout-nope.jsonl"),
	})
	if !errors.Is(err, model.ErrSessionGone) {
		t.Fatalf("Read of a vanished rollout returned %v, want ErrSessionGone", err)
	}
}

func TestRolloutFilenameFilter(t *testing.T) {
	ok := map[string]string{
		"rollout-2026-08-01T09-12-33-00000000-bbbb-4ccc-8ddd-000000000002.jsonl": "00000000-bbbb-4ccc-8ddd-000000000002",
	}
	bad := []string{
		"rollout-2026-07-20T08-00-00-00000000-bbbb-4ccc-8ddd-000000000005.jsonl.zst",
		"rollout-compression.lock",
		"rollout-2026-08-01T09-12-33-00000000-bbbb-4ccc-8ddd-000000000002.jsonl.tmp",
		"notes.jsonl",
		"rollout-nope.jsonl",
	}
	for name, want := range ok {
		got, accepted := sessionIDFromFile(name)
		if !accepted || got != want {
			t.Errorf("sessionIDFromFile(%q) = %q,%v want %q,true", name, got, accepted, want)
		}
	}
	for _, name := range bad {
		if id, accepted := sessionIDFromFile(name); accepted {
			t.Errorf("%q accepted as session %q, want rejected", name, id)
		}
	}
}
