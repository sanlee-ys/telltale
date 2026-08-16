package pi

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sanlee-ys/telltale/internal/jsonl"
	"github.com/sanlee-ys/telltale/internal/model"
)

const (
	fixtureRoot = "testdata/sessions"
	healthyID   = "00000000-aaaa-4bbb-8ccc-000000000001"
	headerOnly  = "00000000-aaaa-4bbb-8ccc-000000000002"
	secretID    = "00000000-aaaa-4bbb-8ccc-000000000003"
	acmeID      = "00000000-aaaa-4bbb-8ccc-000000000004"

	plantedKey    = "AKIAIOSFODNN7EXAMPLE"
	authMarker    = "SYNTHETIC-AUTH-JSON-MUST-NEVER-BE-READ"
	healthyName   = "Example App\u2028Session"
	healthyCwd    = `C:\src\code\example-app`
	healthyFile   = "2026-08-11T15-02-43-599Z_00000000-aaaa-4bbb-8ccc-000000000001.jsonl"
	healthySubdir = "--C--src-code-example-app--"
)

func discover(t *testing.T, root string) []model.SessionRef {
	t.Helper()
	refs, err := NewWithRoot(root).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	return refs
}

func read(t *testing.T, root, id string) *model.Session {
	t.Helper()
	a := NewWithRoot(root)
	for _, ref := range discover(t, root) {
		if ref.ID != id {
			continue
		}
		s, err := a.Read(context.Background(), ref)
		if err != nil {
			t.Fatalf("Read(%s): %v", id, err)
		}
		return s
	}
	t.Fatalf("Discover never returned session %s", id)
	return nil
}

func extra(s *model.Session, label string) (string, bool) {
	for _, e := range s.Extras {
		if e.Label == label {
			return e.Value, true
		}
	}
	return "", false
}

func displayed(s *model.Session) []string {
	var out []string
	if s.Name != nil {
		out = append(out, *s.Name)
	}
	if s.WorkspaceDir != nil {
		out = append(out, *s.WorkspaceDir)
	}
	if id, ok := s.Model.Name(); ok {
		out = append(out, id)
	}
	out = append(out, s.Diagnostics...)
	for _, e := range s.Extras {
		out = append(out, e.Label, e.Value)
	}
	return out
}

func TestFixtureBytesArePreserved(t *testing.T) {
	p := filepath.Join(fixtureRoot, healthySubdir, healthyFile)
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
	if len(b) == 0 || b[len(b)-1] == '\n' {
		t.Error("fixture gained a trailing newline; the torn final record is now a complete one")
	}
}

func TestDiscoverFindsSynthesizedSessions(t *testing.T) {
	refs := discover(t, fixtureRoot)
	want := []string{healthyID, headerOnly, secretID, acmeID}
	if len(refs) != len(want) {
		var got []string
		for _, r := range refs {
			got = append(got, r.ID)
		}
		t.Fatalf("Discover returned %v, want exactly %v", got, want)
	}
	for i, id := range want {
		if refs[i].ID != id {
			t.Errorf("ref %d is %s, want %s", i, refs[i].ID, id)
		}
		if refs[i].Vendor != Vendor {
			t.Errorf("ref %d carries vendor %q", i, refs[i].Vendor)
		}
		if refs[i].LastActivity == nil {
			t.Errorf("ref %d has no mtime hint", i)
		}
	}
	for _, ref := range refs {
		base := filepath.Base(ref.Locator)
		switch base {
		case "auth.json", "notes.md", "foo.jsonl":
			t.Errorf("Discover returned %q, which is not a session", ref.Locator)
		}
		if strings.Contains(ref.Locator, string(filepath.Separator)+"nested"+string(filepath.Separator)) {
			t.Errorf("Discover walked into a nested directory: %s", ref.Locator)
		}
	}
}

func TestReadMapsHeaderModelActivityAndTokens(t *testing.T) {
	s := read(t, fixtureRoot, healthyID)
	if s.ID != healthyID {
		t.Errorf("id = %q, want the header id", s.ID)
	}
	if s.Name == nil || *s.Name != healthyName {
		t.Errorf("name = %q, want the last session_info.name with raw U+2028", ptrStr(s.Name))
	}
	if id, _ := s.Model.Name(); id != "grok-4.5" {
		t.Errorf("model = %q, want the last assistant message.model", id)
	}
	if s.WorkspaceDir == nil || *s.WorkspaceDir != healthyCwd {
		t.Errorf("workspace = %v, want header cwd", ptrStr(s.WorkspaceDir))
	}
	if s.LastActivity == nil {
		t.Fatal("last_activity absent")
	}
	wantTS := time.Date(2026, 8, 11, 15, 4, 10, 0, time.UTC)
	if !s.LastActivity.Equal(wantTS) && s.LastActivity.Before(wantTS) {
		// mtime may be newer than the fixture timestamps after checkout.
		// The vendor timestamp must still be a lower bound.
		t.Errorf("last_activity = %v, want at least the newest entry %v", s.LastActivity, wantTS)
	}
	if s.Tokens == nil || s.Tokens.Input != 6818 || s.Tokens.Output != 24 {
		t.Errorf("tokens = %+v, want last assistant usage input/output with no cache math", s.Tokens)
	}
	if !s.Degraded.Empty() {
		t.Errorf("a healthy session degraded %s", s.Degraded)
	}
	if err := s.Validate(NewWithRoot(fixtureRoot).Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestCostIsExtraNotField(t *testing.T) {
	caps := NewWithRoot(fixtureRoot).Capabilities()
	if caps.Capability(model.FieldCost) != model.CapNone {
		t.Error("cost is declared; the store holds no session total")
	}
	s := read(t, fixtureRoot, healthyID)
	if s.Cost != nil {
		t.Errorf("cost = %v, want nil", *s.Cost)
	}
	if s.Has(model.FieldCost) {
		t.Error("session reports a cost value")
	}
	if v, ok := extra(s, "message cost"); !ok || v != "$0.0138" {
		t.Errorf("message cost extra = %q/%v, want the last assistant usage.cost.total", v, ok)
	}
}

func TestContextQuotaLivenessSubagentsStayAbsent(t *testing.T) {
	caps := NewWithRoot(fixtureRoot).Capabilities()
	if err := caps.Validate(); err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	for _, f := range []model.Field{
		model.FieldContextPercent, model.FieldCost, model.FieldQuota,
		model.FieldLiveness, model.FieldSubagents,
	} {
		if caps.Capability(f) != model.CapNone {
			t.Errorf("%s is %s, want none", f, caps.Capability(f))
		}
	}
	for _, f := range []model.Field{
		model.FieldName, model.FieldModel, model.FieldWorkspace, model.FieldLastActivity,
	} {
		if caps.Capability(f) != model.CapReported {
			t.Errorf("%s is %s, want reported", f, caps.Capability(f))
		}
	}
	if !caps.Derived.Empty() {
		t.Errorf("derived set is %s; nothing here is computed", caps.Derived)
	}

	s := read(t, fixtureRoot, healthyID)
	if s.ContextPercent != nil {
		t.Errorf("context_pct = %v, want nil", *s.ContextPercent)
	}
	if len(s.Quota) != 0 {
		t.Errorf("quota = %v, want none", s.Quota)
	}
	if s.LivenessHint != nil {
		t.Errorf("liveness hint = %v, want nil", *s.LivenessHint)
	}
	if s.Subagents != nil {
		t.Errorf("subagents = %v, want nil", *s.Subagents)
	}
}

func TestHeaderOnlySessionStillLists(t *testing.T) {
	s := read(t, fixtureRoot, headerOnly)
	if s.ID != headerOnly {
		t.Errorf("id = %q", s.ID)
	}
	if s.WorkspaceDir == nil || *s.WorkspaceDir != healthyCwd {
		t.Errorf("workspace = %v, want header cwd", ptrStr(s.WorkspaceDir))
	}
	if s.Name == nil || *s.Name != "example-app" {
		t.Errorf("name = %v, want the workspace basename fallback", ptrStr(s.Name))
	}
	if s.Model != nil {
		t.Errorf("model = %v, want absent with no model_change", s.Model)
	}
	if s.Tokens != nil {
		t.Errorf("tokens = %+v, want absent with no assistant usage", s.Tokens)
	}
	if s.Degraded.Has(model.FieldName) || s.Degraded.Has(model.FieldWorkspace) {
		t.Errorf("degraded %s: a header-only file is a session, not a failed read", s.Degraded)
	}
	if err := s.Validate(NewWithRoot(fixtureRoot).Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestAcmeModelChangeIsProviderSlashID(t *testing.T) {
	s := read(t, fixtureRoot, acmeID)
	if id, _ := s.Model.Name(); id != "xai/grok-4.5" {
		t.Errorf("model = %q, want last model_change provider/modelId", id)
	}
	if s.Name == nil || *s.Name != "acme-api" {
		t.Errorf("name = %v, want workspace basename", ptrStr(s.Name))
	}
}

func TestPlantedSecretDoesNotLeak(t *testing.T) {
	s := read(t, fixtureRoot, secretID)
	for _, v := range displayed(s) {
		if strings.Contains(v, plantedKey) {
			t.Errorf("planted key reached a Session value: %q", v)
		}
	}
	if s.Name != nil && strings.Contains(*s.Name, plantedKey) {
		t.Error("planted key reached Name")
	}
}

func TestNeverReadsAuthJSON(t *testing.T) {
	for _, p := range []string{
		filepath.Join("testdata", "auth.json"),
		filepath.Join(fixtureRoot, "auth.json"),
	} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("testdata is missing %s: %v", p, err)
		}
		if !bytes.Contains(b, []byte(authMarker)) {
			t.Fatalf("%s lost its planted marker", p)
		}
	}
	a := NewWithRoot(fixtureRoot)
	refs := discover(t, fixtureRoot)
	for _, ref := range refs {
		if strings.Contains(strings.ToLower(ref.Locator), "auth.json") {
			t.Errorf("Discover named auth.json: %s", ref.Locator)
		}
		s, err := a.Read(context.Background(), ref)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		for _, v := range displayed(s) {
			if strings.Contains(v, authMarker) || strings.Contains(v, "auth.json") {
				t.Errorf("auth.json reached a Session value: %q", v)
			}
		}
	}
}

func TestJSONLWithU2028StaysOneRecord(t *testing.T) {
	p := filepath.Join(fixtureRoot, healthySubdir, healthyFile)
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var recs [][]byte
	if err := jsonl.Scan(f, func(rec []byte) error {
		recs = append(recs, append([]byte(nil), rec...))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	found := false
	sep := []byte{0xE2, 0x80, 0xA8}
	for _, rec := range recs {
		if !bytes.Contains(rec, sep) {
			continue
		}
		found = true
		if bytes.Count(rec, []byte{'\n'}) != 1 {
			t.Errorf("U+2028 record has %d 0x0A bytes; a Unicode line split tore it", bytes.Count(rec, []byte{'\n'}))
		}
		if !utf8.Valid(rec) {
			t.Error("U+2028 record is not valid UTF-8")
		}
	}
	if !found {
		t.Fatal("no complete record carried U+2028")
	}

	s := read(t, fixtureRoot, healthyID)
	if s.Name == nil || !strings.ContainsRune(*s.Name, '\u2028') {
		t.Errorf("name = %q, want the session_info value after the separator", ptrStr(s.Name))
	}
	if s.Name != nil && !strings.HasSuffix(*s.Name, "Session") {
		t.Errorf("name = %q, the half after U+2028 was lost", *s.Name)
	}
}

func TestNewWithRootMissingIsVendorAbsent(t *testing.T) {
	_, err := NewWithRoot(filepath.Join(t.TempDir(), "no-such-store")).Discover(context.Background())
	if !errors.Is(err, model.ErrVendorAbsent) {
		t.Errorf("Discover = %v, want ErrVendorAbsent", err)
	}
	if _, err := (&Adapter{}).Discover(context.Background()); !errors.Is(err, model.ErrVendorAbsent) {
		t.Errorf("rootless Discover = %v, want ErrVendorAbsent", err)
	}
}

func TestNewResolvesSessionRoots(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", "")
	t.Setenv("PI_CODING_AGENT_DIR", "")
	got := New().Root()
	if !strings.HasSuffix(filepath.ToSlash(got), ".pi/agent/sessions") {
		t.Errorf("default Root = %q, want .../.pi/agent/sessions", got)
	}

	agentHome := filepath.Join(t.TempDir(), "agent-home")
	t.Setenv("PI_CODING_AGENT_DIR", agentHome)
	got = New().Root()
	want := filepath.Join(agentHome, "sessions")
	if got != want {
		t.Errorf("PI_CODING_AGENT_DIR Root = %q, want %q", got, want)
	}

	sessionRoot := filepath.Join(t.TempDir(), "sessions-root")
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", sessionRoot)
	got = New().Root()
	if got != sessionRoot {
		t.Errorf("PI_CODING_AGENT_SESSION_DIR Root = %q, want %q", got, sessionRoot)
	}
}

func TestReadOfAVanishedSessionIsGone(t *testing.T) {
	_, err := NewWithRoot(fixtureRoot).Read(context.Background(), model.SessionRef{
		Vendor:  Vendor,
		ID:      "00000000-aaaa-4bbb-8ccc-00000000ffff",
		Locator: filepath.Join(fixtureRoot, "nowhere", "gone.jsonl"),
	})
	if !errors.Is(err, model.ErrSessionGone) {
		t.Errorf("Read = %v, want ErrSessionGone", err)
	}
}

func TestNoLivenessHintIsEverSet(t *testing.T) {
	for _, ref := range discover(t, fixtureRoot) {
		s, err := NewWithRoot(fixtureRoot).Read(context.Background(), ref)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if s.LivenessHint != nil {
			t.Errorf("session %s carries a liveness hint (%s)", ref.ID, *s.LivenessHint)
		}
	}
}

func TestActivityFoldTakesTheFresherSignal(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, healthySubdir)
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(fixtureRoot, healthySubdir, healthyFile)
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(sessDir, healthyFile)
	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	stamped := time.Date(2026, 8, 11, 15, 4, 10, 0, time.UTC)
	older := stamped.Add(-30 * time.Minute)
	if err := os.Chtimes(dst, older, older); err != nil {
		t.Fatal(err)
	}
	s := read(t, dir, healthyID)
	if s.LastActivity == nil || !s.LastActivity.Equal(stamped) {
		t.Errorf("last_activity = %v, want the newest entry timestamp %v", s.LastActivity, stamped)
	}

	newer := stamped.Add(90 * time.Second).Truncate(time.Second)
	if err := os.Chtimes(dst, newer, newer); err != nil {
		t.Fatal(err)
	}
	s = read(t, dir, healthyID)
	if s.LastActivity == nil || !s.LastActivity.Truncate(time.Second).Equal(newer) {
		t.Errorf("last_activity = %v, want the file mtime %v", s.LastActivity, newer)
	}
}

func TestFutureSkewDegradesLastActivity(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "--C--src-code-skew--")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	line := `{"type":"session","version":3,"id":"00000000-aaaa-4bbb-8ccc-000000000099","timestamp":"` + future + `","cwd":"C:\\src\\code\\skew"}` + "\n"
	dst := filepath.Join(sessDir, "2026-08-11T15-00-00-000Z_00000000-aaaa-4bbb-8ccc-000000000099.jsonl")
	if err := os.WriteFile(dst, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	ahead := time.Now().Add(time.Hour)
	if err := os.Chtimes(dst, ahead, ahead); err != nil {
		t.Fatal(err)
	}
	s := read(t, dir, "00000000-aaaa-4bbb-8ccc-000000000099")
	if s.LastActivity != nil {
		t.Errorf("last_activity = %v, want absent when both signals sit ahead of the clock", s.LastActivity)
	}
	if !s.Degraded.Has(model.FieldLastActivity) {
		t.Error("future-skew did not degrade last_activity")
	}
}

func ptrStr(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}
