package gemini

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

	"github.com/sanlee-ys/telltale/internal/model"
)

// Refs are keyed by filename stem (the vendor embeds only the id's first 8
// characters in the filename); the full session ids live inside the files.
const (
	healthyRef       = "session-2026-08-02T10-00-00000000"
	tornRef          = "session-2026-08-02T11-30-00000000"
	healthySessionID = "00000000-cccc-4ddd-8eee-000000000001"
)

func testAdapter(t *testing.T) *Adapter {
	t.Helper()
	return NewWithRoot(filepath.Join("testdata", "tmp"))
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

// The fixtures are the framing rule's proof. If either byte pattern is lost to
// a checkout rewrite, the tests below stop testing anything, so they are
// asserted directly rather than assumed (.gitattributes is the other half).
func TestFixtureBytesArePreserved(t *testing.T) {
	p := filepath.Join("testdata", "tmp", "example-app-1234", "chats", healthyRef+".jsonl")
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

func TestCapabilitiesDeclareOnlyWhatIsOnDisk(t *testing.T) {
	caps := testAdapter(t).Capabilities()
	if err := caps.Validate(); err != nil {
		t.Fatal(err)
	}
	want := map[model.Field]model.Capability{
		model.FieldName:         model.CapReported, // metadata summary
		model.FieldModel:        model.CapReported, // per-message model id
		model.FieldWorkspace:    model.CapReported, // vendor's projects.json entry, read verbatim
		model.FieldLastActivity: model.CapReported,
		// Verified against chatRecordingService.ts / storage.ts at v0.53.1:
		// no context window size, no cost, no quota reaches disk. The CLI's own
		// context percentage divides by a static table compiled into its source,
		// which is the assumed denominator decisions/001 forbids.
		model.FieldContextPercent: model.CapNone,
		model.FieldCost:           model.CapNone,
		model.FieldQuota:          model.CapNone,
		// "A process exists" is not liveness (design.md §4a.4).
		model.FieldLiveness: model.CapNone,
		// The nest under chats/<parent-id>/ IS on disk, but the number is not:
		// files are counted exactly and the recency boundary is the inference.
		model.FieldSubagents: model.CapDerived,
	}
	for f, w := range want {
		if got := caps.Capability(f); got != w {
			t.Errorf("capability(%s) = %s, want %s", f, got, w)
		}
	}
}

func TestDiscoverIsFixedDepthAndJSONLOnly(t *testing.T) {
	refs, err := testAdapter(t).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(refs))
	for _, r := range refs {
		ids = append(ids, r.ID)
	}
	sort.Strings(ids)

	want := []string{healthyRef, tornRef}
	if len(ids) != len(want) {
		t.Fatalf("discovered %v, want exactly %v — walking into chats/ subdirectories picks up the subagent nest, and an extension-blind filter picks up the legacy .json recording", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("discovered %v, want %v", ids, want)
		}
	}
	for _, r := range refs {
		if r.Vendor != Vendor {
			t.Errorf("ref %s has vendor %q", r.ID, r.Vendor)
		}
		if r.LastActivity == nil {
			t.Errorf("ref %s has no mtime hint", r.ID)
		}
	}
}

func TestDiscoverReportsVendorAbsent(t *testing.T) {
	a := NewWithRoot(filepath.Join("testdata", "no-such-directory"))
	_, err := a.Discover(context.Background())
	if !errors.Is(err, model.ErrVendorAbsent) {
		t.Fatalf("Discover on a missing root returned %v, want ErrVendorAbsent", err)
	}
}

func TestReadHealthySession(t *testing.T) {
	a := testAdapter(t)
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s, err := a.Read(context.Background(), refByID(t, refs, healthyRef))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(a.Capabilities()); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// The upsert is the vendor's write pattern: the same message id is
	// re-appended when tokens settle, so a linear last-wins pass must land on
	// the re-appended record's model, not the first occurrence's.
	if got, _ := s.Model.Name(); got != "gemini-3-pro" {
		t.Errorf("model = %q, want gemini-3-pro — the re-appended (upserted) record wins", got)
	}
	if s.Name == nil || *s.Name != "FIXTURE example session" {
		t.Errorf("name = %v, want the $set summary", s.Name)
	}
	// workspace comes from the vendor's registry, keyed by the project dir
	// slug — the record itself carries only an opaque hash.
	if got, ok := s.WorkspaceName(); !ok || got != "example-app" {
		t.Errorf("workspace = %q ok=%v, want example-app via projects.json", got, ok)
	}
	if s.LastActivity == nil {
		t.Error("last_activity absent; mtime is always available")
	}
	// tokens.input is promptTokenCount — what was sent — and cached is a
	// subset of it, not an addition.
	if got := extra(s, "ctx tokens"); got != "215k" {
		t.Errorf("ctx tokens = %q, want 215k (input alone; cached is a subset)", got)
	}

	// Everything the vendor does not put on disk stays nil. This is the row
	// that renders as em dashes in the HUD and must never render as zero.
	if s.ContextPercent != nil {
		t.Errorf("context_pct = %v, want nil (no window size on disk)", *s.ContextPercent)
	}
	if s.Cost != nil {
		t.Errorf("cost = %v, want nil (nothing on disk is priced)", *s.Cost)
	}
	if len(s.Quota) != 0 {
		t.Errorf("quota = %v, want none (rate limiting is runtime-only)", s.Quota)
	}
	if s.LivenessHint != nil {
		t.Errorf("liveness hint = %v, want nil", *s.LivenessHint)
	}
	if len(s.Diagnostics) != 0 {
		t.Errorf("healthy session produced diagnostics: %v — the $rewindTo marker and the torn tail are known shapes, not parse failures", s.Diagnostics)
	}
}

// A torn tail must be invisible: the same file with those bytes removed
// renders identically. This is the §7.7 hud-torn-tail assertion at the
// adapter level.
func TestTornTailChangesNothing(t *testing.T) {
	a := testAdapter(t)
	refs, _ := a.Discover(context.Background())
	ref := refByID(t, refs, healthyRef)

	full, err := a.Read(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(ref.Locator)
	if err != nil {
		t.Fatal(err)
	}
	cut := raw[:bytes.LastIndexByte(raw, '\n')+1]
	root, chats := tempTree(t)
	if err := os.WriteFile(filepath.Join(chats, healthyRef+".jsonl"), cut, 0o644); err != nil {
		t.Fatal(err)
	}
	b := NewWithRoot(root)
	brefs, _ := b.Discover(context.Background())
	trimmed, err := b.Read(context.Background(), refByID(t, brefs, healthyRef))
	if err != nil {
		t.Fatal(err)
	}

	if !sameDisplayFields(full, trimmed) {
		t.Fatalf("torn tail changed the render:\nwith    %s\nwithout %s", describe(full), describe(trimmed))
	}
	if len(full.Diagnostics) != 0 {
		t.Errorf("a torn tail is not a parse failure, but produced diagnostics: %v", full.Diagnostics)
	}
}

// A session whose ONLY record is torn still lists: it was discovered by
// filename, and every record-sourced field is absent rather than invented.
// workspace is the exception by design — it comes from the file's LOCATION
// plus the registry, not from any record, so a torn file still names its
// project. The subagent nest, keyed by the full session id that never parsed,
// is honestly unknowable: degraded, not zero.
func TestTornOnlyRecordStillListsWithRecordFieldsAbsent(t *testing.T) {
	a := testAdapter(t)
	refs, _ := a.Discover(context.Background())
	s, err := a.Read(context.Background(), refByID(t, refs, tornRef))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(a.Capabilities()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if s.Has(model.FieldModel) {
		t.Error("model parsed out of a torn record")
	}
	if s.Has(model.FieldName) {
		t.Error("name parsed out of a torn record")
	}
	if got, ok := s.WorkspaceName(); !ok || got != "example-app" {
		t.Errorf("workspace = %q ok=%v; the registry lookup does not depend on the record", got, ok)
	}
	if !s.Has(model.FieldLastActivity) {
		t.Error("last_activity should still come from the file's mtime")
	}
	if s.Subagents != nil {
		t.Errorf("subagents = %d, want nil — the nest is keyed by a session id we never read, so \"0\" would be a claim", *s.Subagents)
	}
	if !s.Degraded.Has(model.FieldSubagents) {
		t.Error("unresolvable nest must mark subagents degraded")
	}
}

func TestReadReportsSessionGone(t *testing.T) {
	a := testAdapter(t)
	_, err := a.Read(context.Background(), model.SessionRef{
		Vendor:  Vendor,
		ID:      healthyRef,
		Locator: filepath.Join("testdata", "tmp", "example-app-1234", "chats", "gone.jsonl"),
	})
	if !errors.Is(err, model.ErrSessionGone) {
		t.Fatalf("Read of a vanished file returned %v, want ErrSessionGone — the writer deletes non-resumable sessions on exit, so this path is NORMAL", err)
	}
}

// The structural exclusion has a read-time backstop: a chat file whose
// metadata says kind=subagent is not a session, wherever it was found.
func TestSubagentTranscriptRejectedAtRead(t *testing.T) {
	root, chats := tempTree(t)
	rec := `{"sessionId":"00000000-cccc-4ddd-8eee-000000000013","projectHash":"00000000000000000000000000000000000000000000000000000000000000c1","startTime":"2026-08-02T10:00:00.000Z","lastUpdated":"2026-08-02T10:00:00.000Z","kind":"subagent","directories":["c:\\users\\dev\\code\\example-app"]}` + "\n"
	if err := os.WriteFile(filepath.Join(chats, "inline-sub.jsonl"), []byte(rec), 0o644); err != nil {
		t.Fatal(err)
	}
	a := NewWithRoot(root)
	refs, _ := a.Discover(context.Background())
	_, err := a.Read(context.Background(), refByID(t, refs, "inline-sub"))
	if !errors.Is(err, ErrSubagentTranscript) {
		t.Fatalf("Read returned %v, want ErrSubagentTranscript", err)
	}
}

// Clock skew: a file whose mtime is ahead of the observation clock has no
// readable age. It degrades to absent, which renders "—" — never "0s".
func TestFutureMtimeDegradesRatherThanClampingToZero(t *testing.T) {
	root, chats := tempTree(t)
	p := filepath.Join(chats, healthyRef+".jsonl")
	rec := `{"sessionId":"` + healthySessionID + `","projectHash":"00000000000000000000000000000000000000000000000000000000000000c1"}` + "\n"
	if err := os.WriteFile(p, []byte(rec), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(p, future, future); err != nil {
		t.Fatal(err)
	}

	a := NewWithRoot(root)
	refs, _ := a.Discover(context.Background())
	s, err := a.Read(context.Background(), refByID(t, refs, healthyRef))
	if err != nil {
		t.Fatal(err)
	}
	if s.LastActivity != nil {
		t.Fatalf("last_activity = %v, want nil for a future mtime with no record timestamps", *s.LastActivity)
	}
	if !s.Degraded.Has(model.FieldLastActivity) {
		t.Error("a future mtime should mark last_activity degraded")
	}
	if err := s.Validate(a.Capabilities()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// §6 Q8 for this vendor: the $set lastUpdated timestamp — the writer's own
// freshness signal — must outvote a stale mtime, and a future one must not.
func TestLastActivityUsesNewestRecordTimestampOverStaleMtime(t *testing.T) {
	root, chats := tempTree(t)
	now := time.Now().UTC()
	fresh := now.Add(-30 * time.Second)
	p := filepath.Join(chats, healthyRef+".jsonl")
	rec := `{"sessionId":"` + healthySessionID + `","projectHash":"00000000000000000000000000000000000000000000000000000000000000c1"}` + "\n" +
		`{"$set":{"lastUpdated":"` + fresh.Format(time.RFC3339Nano) + `"}}` + "\n"
	if err := os.WriteFile(p, []byte(rec), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := now.Add(-10 * time.Minute)
	if err := os.Chtimes(p, stale, stale); err != nil {
		t.Fatal(err)
	}
	s := readOne(t, NewWithRoot(root), healthyRef)
	if s.LastActivity == nil {
		t.Fatal("last_activity absent")
	}
	if s.LastActivity.Before(fresh.Add(-time.Second)) {
		t.Errorf("last_activity = %v, want the $set lastUpdated (~%v) to outvote the stale mtime (%v)",
			s.LastActivity, fresh, stale)
	}

	// Future record timestamp: excluded, mtime stands.
	future := now.Add(time.Hour)
	rec2 := `{"sessionId":"` + healthySessionID + `","projectHash":"00000000000000000000000000000000000000000000000000000000000000c1"}` + "\n" +
		`{"$set":{"lastUpdated":"` + future.Format(time.RFC3339Nano) + `"}}` + "\n"
	if err := os.WriteFile(p, []byte(rec2), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, stale, stale); err != nil {
		t.Fatal(err)
	}
	s = readOne(t, NewWithRoot(root), healthyRef)
	if s.LastActivity == nil {
		t.Fatal("last_activity absent")
	}
	if s.LastActivity.After(stale.Add(time.Second)) {
		t.Errorf("last_activity = %v; a FUTURE record timestamp must not outvote the mtime (%v)", s.LastActivity, stale)
	}
}

// ------------------------------------------------------- sub-agent counting

// fanoutTree builds a synthesized project tree whose nest mtimes are set
// explicitly: checkout rewrites fixture mtimes, so a repo fixture can never
// pin a recency boundary.
func fanoutTree(t *testing.T, ages ...time.Duration) *Adapter {
	t.Helper()
	root, chats := tempTree(t)
	rec := `{"sessionId":"` + healthySessionID + `","projectHash":"00000000000000000000000000000000000000000000000000000000000000c1"}` + "\n"
	if err := os.WriteFile(filepath.Join(chats, healthyRef+".jsonl"), []byte(rec), 0o644); err != nil {
		t.Fatal(err)
	}
	nest := filepath.Join(chats, healthySessionID)
	if err := os.MkdirAll(nest, 0o755); err != nil {
		t.Fatal(err)
	}
	// A non-transcript neighbour: counting it would inflate the chip.
	if err := os.WriteFile(filepath.Join(nest, "note.partial.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i, age := range ages {
		p := filepath.Join(nest, "sub-"+string(rune('a'+i))+".jsonl")
		if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		mod := now.Add(-age)
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	return NewWithRoot(root)
}

func readOne(t *testing.T, a *Adapter, id string) *model.Session {
	t.Helper()
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s, err := a.Read(context.Background(), refByID(t, refs, id))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(a.Capabilities()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return s
}

// The chip counts a fan-out in progress, not a fan-out that ever happened.
func TestSubagentCountIsRecentOnly(t *testing.T) {
	a := fanoutTree(t, time.Minute, 10*time.Minute, 2*time.Hour, -3*time.Hour)
	s := readOne(t, a, healthyRef)
	if s.Subagents == nil {
		t.Fatal("subagents absent; the nest exists and is readable")
	}
	if *s.Subagents != 2 {
		t.Errorf("counted %d recent sub-agents, want 2 (1m and 10m inside the %s horizon; 2h is out, an mtime 3h in the FUTURE is not a readable time, and note.partial.json is not a transcript)",
			*s.Subagents, subagentHorizon)
	}
	if !s.Derived.Has(model.FieldSubagents) {
		t.Error("the count is computed from a recency boundary and must be marked derived")
	}
}

// A session that never fanned out has a MEASURED zero: we looked where the
// nest would be and there was nothing there.
func TestSubagentCountIsZeroWithoutANest(t *testing.T) {
	a := fanoutTree(t) // no nested transcripts written, but the dir exists
	s := readOne(t, a, healthyRef)
	if s.Subagents == nil {
		t.Fatal("subagents absent; an empty (or absent) nest is a countable zero")
	}
	if *s.Subagents != 0 {
		t.Errorf("counted %d, want 0", *s.Subagents)
	}
}

// The repo fixture pins the FILTER, not the boundary: its mtimes are whatever
// checkout wrote. Two .jsonl transcripts plus one non-transcript neighbour,
// so the count can never exceed two however fresh the clone is.
func TestSubagentCountNeverExceedsTheTranscriptsPresent(t *testing.T) {
	s := readOne(t, testAdapter(t), healthyRef)
	if s.Subagents == nil {
		t.Fatal("subagents absent for a session whose nest exists")
	}
	if *s.Subagents > 2 {
		t.Errorf("counted %d, want at most 2 — note.partial.json is not a transcript", *s.Subagents)
	}
}

// ------------------------------------------------------- workspace registry

// A registry that exists but cannot be parsed is a broken read, not a missing
// fact: the field degrades instead of silently vanishing.
func TestWorkspaceDegradesOnUnparseableRegistry(t *testing.T) {
	root, chats := tempTree(t)
	rec := `{"sessionId":"` + healthySessionID + `","projectHash":"00000000000000000000000000000000000000000000000000000000000000c1"}` + "\n"
	if err := os.WriteFile(filepath.Join(chats, healthyRef+".jsonl"), []byte(rec), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(root), registryFile), []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := readOne(t, NewWithRoot(root), healthyRef)
	if s.WorkspaceDir != nil {
		t.Errorf("workspace = %q, want nil from a corrupt registry", *s.WorkspaceDir)
	}
	if !s.Degraded.Has(model.FieldWorkspace) {
		t.Error("a corrupt registry must mark workspace degraded")
	}
	found := false
	for _, d := range s.Diagnostics {
		if strings.Contains(d, "project registry unparseable") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing diagnostic; got %v", s.Diagnostics)
	}
}

// A missing registry, or a slug with no entry, is plain absence: the registry
// self-heals from .project_root markers and can lag a fresh project.
func TestWorkspaceAbsentWithoutRegistryEntry(t *testing.T) {
	root, chats := tempTree(t)
	rec := `{"sessionId":"` + healthySessionID + `","projectHash":"00000000000000000000000000000000000000000000000000000000000000c1"}` + "\n"
	if err := os.WriteFile(filepath.Join(chats, healthyRef+".jsonl"), []byte(rec), 0o644); err != nil {
		t.Fatal(err)
	}
	// tempTree copies the registry to mirror the real ~/.gemini shape; this
	// test is about the shape WITHOUT one.
	if err := os.Remove(filepath.Join(filepath.Dir(root), registryFile)); err != nil {
		t.Fatal(err)
	}
	s := readOne(t, NewWithRoot(root), healthyRef)
	if s.WorkspaceDir != nil {
		t.Errorf("workspace = %q, want nil with no registry present", *s.WorkspaceDir)
	}
	if s.Degraded.Has(model.FieldWorkspace) {
		t.Error("absence is not degradation: no registry, no claim, no mark")
	}
}

// ------------------------------------------------------- helpers

// tempTree builds <tmp>/tmp/example-app-1234/chats and copies the repo
// registry beside the root, mirroring ~/.gemini's real shape.
func tempTree(t *testing.T) (root, chats string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, "tmp")
	chats = filepath.Join(root, "example-app-1234", "chats")
	if err := os.MkdirAll(chats, 0o755); err != nil {
		t.Fatal(err)
	}
	reg, err := os.ReadFile(filepath.Join("testdata", registryFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, registryFile), reg, 0o644); err != nil {
		t.Fatal(err)
	}
	return root, chats
}

func extra(s *model.Session, label string) string {
	for _, e := range s.Extras {
		if e.Label == label {
			return e.Value
		}
	}
	return ""
}

func sameDisplayFields(a, b *model.Session) bool {
	if a.Present() != b.Present() {
		return false
	}
	an, _ := a.Model.Name()
	bn, _ := b.Model.Name()
	if an != bn {
		return false
	}
	aw, _ := a.WorkspaceName()
	bw, _ := b.WorkspaceName()
	if aw != bw {
		return false
	}
	if (a.Name == nil) != (b.Name == nil) {
		return false
	}
	if a.Name != nil && *a.Name != *b.Name {
		return false
	}
	return a.Degraded == b.Degraded
}

func describe(s *model.Session) string {
	m, _ := s.Model.Name()
	w, _ := s.WorkspaceName()
	return "present=" + s.Present().String() + " model=" + m + " workspace=" + w + " degraded=" + s.Degraded.String()
}
