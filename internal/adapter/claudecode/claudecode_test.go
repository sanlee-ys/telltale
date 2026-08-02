package claudecode

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

const (
	healthyID = "00000000-aaaa-4bbb-8ccc-000000000001"
	tornID    = "00000000-aaaa-4bbb-8ccc-000000000002"
)

func testAdapter(t *testing.T) *Adapter {
	t.Helper()
	return NewWithRoot(filepath.Join("testdata", "projects"))
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
	p := filepath.Join("testdata", "projects", "C--Users-dev-code-example-app", healthyID+".jsonl")
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
		model.FieldName:         model.CapReported,
		model.FieldModel:        model.CapReported,
		model.FieldWorkspace:    model.CapReported,
		model.FieldLastActivity: model.CapReported,
		// Grepped across the live corpus: zero matches for a context window
		// size, a cost, or a rate limit anywhere in the transcripts. Declaring
		// these would promise a source that does not exist.
		model.FieldContextPercent: model.CapNone,
		model.FieldCost:           model.CapNone,
		model.FieldQuota:          model.CapNone,
		// "A process exists" is not liveness (design.md §4a.4).
		model.FieldLiveness: model.CapNone,
		// The sidecar tree IS on disk, but the number is not: the files are
		// counted exactly and the recency boundary is the inference, so the
		// field is derived and the HUD marks it.
		model.FieldSubagents: model.CapDerived,
	}
	for f, w := range want {
		if got := caps.Capability(f); got != w {
			t.Errorf("capability(%s) = %s, want %s", f, got, w)
		}
	}
}

func TestDiscoverIsNonRecursiveAndUUIDFiltered(t *testing.T) {
	refs, err := testAdapter(t).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(refs))
	for _, r := range refs {
		ids = append(ids, r.ID)
	}
	sort.Strings(ids)

	want := []string{healthyID, tornID}
	if len(ids) != len(want) {
		t.Fatalf("discovered %v, want exactly %v — a recursive walk picks up the subagents/ sidecar, and an extension-only filter picks up .memory-sync-manifest.json", ids, want)
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
	s, err := a.Read(context.Background(), refByID(t, refs, healthyID))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(a.Capabilities()); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if got, _ := s.Model.Name(); got != "claude-opus-5" {
		t.Errorf("model = %q, want claude-opus-5 — the newest non-synthetic assistant record wins", got)
	}
	if s.Name == nil || *s.Name != "FIXTURE example session" {
		t.Errorf("name = %v, want the custom title", s.Name)
	}
	if got, ok := s.WorkspaceName(); !ok || got != "example-app" {
		t.Errorf("workspace = %q ok=%v", got, ok)
	}
	if s.LastActivity == nil {
		t.Error("last_activity absent; mtime is always available")
	}

	// Everything the vendor does not put on disk stays nil. This is the row
	// that renders as em dashes in the HUD and must never render as zero.
	if s.ContextPercent != nil {
		t.Errorf("context_pct = %v, want nil (no context window size on disk)", *s.ContextPercent)
	}
	if s.Cost != nil {
		t.Errorf("cost = %v, want nil (stdin-only field)", *s.Cost)
	}
	if len(s.Quota) != 0 {
		t.Errorf("quota = %v, want none (stdin-only field)", s.Quota)
	}
	if s.LivenessHint != nil {
		t.Errorf("liveness hint = %v, want nil", *s.LivenessHint)
	}
}

// The synthetic record sits between two real ones and carries all-zero usage.
// Letting it through would blank the model cell and zero the token reading.
func TestSyntheticModelNeverOverwritesARealReading(t *testing.T) {
	a := testAdapter(t)
	refs, _ := a.Discover(context.Background())
	s, err := a.Read(context.Background(), refByID(t, refs, healthyID))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Model.Name(); got == syntheticModel {
		t.Fatal("the <synthetic> model id reached the model cell")
	}
	// The last real record carried input=2, cache_read=213388,
	// cache_creation=2464 — reading input_tokens alone would render "2".
	if got := extra(s, "ctx tokens"); got != "215k" {
		t.Errorf("ctx tokens = %q, want 215k (input + cache_read + cache_creation)", got)
	}
}

// A torn tail must be invisible: the same file with those bytes removed renders
// identically. This is the §7.7 hud-torn-tail assertion at the adapter level.
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
	proj := filepath.Join(dir, "C--Users-dev-code-example-app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, healthyID+".jsonl"), cut, 0o644); err != nil {
		t.Fatal(err)
	}
	b := NewWithRoot(dir)
	brefs, _ := b.Discover(context.Background())
	trimmed, err := b.Read(context.Background(), refByID(t, brefs, healthyID))
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
// filename, and every sourced field is absent rather than invented.
func TestTornOnlyRecordStillListsWithEverythingAbsent(t *testing.T) {
	a := testAdapter(t)
	refs, _ := a.Discover(context.Background())
	s, err := a.Read(context.Background(), refByID(t, refs, tornID))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(a.Capabilities()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if s.Has(model.FieldModel) {
		t.Error("model parsed out of a torn record")
	}
	if s.Has(model.FieldWorkspace) {
		t.Error("workspace parsed out of a torn record")
	}
	if s.Has(model.FieldName) {
		t.Error("name parsed out of a torn record")
	}
	if !s.Has(model.FieldLastActivity) {
		t.Error("last_activity should still come from the file's mtime")
	}
}

func TestReadReportsSessionGone(t *testing.T) {
	a := testAdapter(t)
	_, err := a.Read(context.Background(), model.SessionRef{
		Vendor:  Vendor,
		ID:      healthyID,
		Locator: filepath.Join("testdata", "projects", "gone", healthyID+".jsonl"),
	})
	if !errors.Is(err, model.ErrSessionGone) {
		t.Fatalf("Read of a vanished file returned %v, want ErrSessionGone", err)
	}
}

// Clock skew: a transcript whose mtime is ahead of the observation clock has no
// readable age. It degrades to absent, which renders "—" — never "0s", which
// would claim the session was active this instant.
func TestFutureMtimeDegradesRatherThanClampingToZero(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "C--Users-dev-code-skewed")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(proj, healthyID+".jsonl")
	if err := os.WriteFile(path, []byte("{\"type\":\"user\",\"cwd\":\"C:\\\\x\\\\skewed\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	a := NewWithRoot(dir)
	refs, _ := a.Discover(context.Background())
	s, err := a.Read(context.Background(), refByID(t, refs, healthyID))
	if err != nil {
		t.Fatal(err)
	}
	if s.LastActivity != nil {
		t.Fatalf("last_activity = %v, want nil for a future mtime", *s.LastActivity)
	}
	if !s.Degraded.Has(model.FieldLastActivity) {
		t.Error("a future mtime should mark last_activity degraded")
	}
	if err := s.Validate(a.Capabilities()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got := s.Liveness(time.Now(), model.DefaultLivenessThresholds); got != model.LivenessUnknown {
		t.Errorf("liveness = %s, want unknown (which renders absent, never stale)", got)
	}
}

// ------------------------------------------------------- sub-agent counting

// fanoutTree builds a synthesized project tree with a subagents/ sidecar whose
// mtimes are set explicitly.
//
// Checkout sets every fixture's mtime to checkout time, so a repo fixture can
// never pin a recency boundary — it would pass on fresh CI and fail on a clone
// that sat over a weekend. The ages that matter are therefore written here.
func fanoutTree(t *testing.T, ages ...time.Duration) *Adapter {
	t.Helper()
	dir := t.TempDir()
	proj := filepath.Join(dir, "C--Users-dev-code-fanout")
	side := filepath.Join(proj, healthyID, "subagents")
	if err := os.MkdirAll(side, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, healthyID+".jsonl"),
		[]byte("{\"type\":\"user\",\"cwd\":\"C:\\\\x\\\\fanout\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-transcript neighbours: the sidecar tree holds these too, and
	// counting one would inflate the chip. .meta.json is the shape the vendor
	// actually writes beside every agent transcript (verified 2026-08-01).
	if err := os.WriteFile(filepath.Join(side, "tool-results.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(side, "agent-a0000000000000000.meta.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	write := func(p string, age time.Duration) {
		t.Helper()
		if err := os.WriteFile(p, []byte("{\"type\":\"assistant\"}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		mod := now.Add(-age)
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	// Flat sidecar files carry the vendor's real basename shape.
	for i, age := range ages {
		write(filepath.Join(side, "agent-a000000000000000"+string(rune('1'+i))+".jsonl"), age)
	}
	// One workflow-nested transcript, fresh: subagents/workflows/<wf>/agent-*.jsonl
	// is the second real shape and must be counted (review blocker follow-up).
	wf := filepath.Join(side, "workflows", "wf_test0000-000")
	if err := os.MkdirAll(wf, 0o755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(wf, "agent-b0000000000000001.jsonl"), time.Minute)
	return NewWithRoot(dir)
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

// The chip counts a fan-out in progress, not a fan-out that ever happened. A
// transcript last written two hours ago is not a running sub-agent, and
// counting it would put a "⑂3" on a session that has one thing going.
func TestSubagentCountIsRecentOnly(t *testing.T) {
	a := fanoutTree(t, time.Minute, 10*time.Minute, 2*time.Hour, -3*time.Hour)
	s := readOne(t, a, healthyID)
	if s.Subagents == nil {
		t.Fatal("subagents absent; the sidecar exists and is readable")
	}
	if *s.Subagents != 3 {
		t.Errorf("counted %d recent sub-agents, want 3 (flat 1m and 10m inside the %s horizon, plus the workflow-nested 1m; 2h is out, an mtime 3h in the FUTURE is not a readable time, and .meta.json/tool-results.json are not transcripts)",
			*s.Subagents, subagentHorizon)
	}
	if !s.Derived.Has(model.FieldSubagents) {
		t.Error("the count is computed from a recency boundary and must be marked derived")
	}
}

// A session that never fanned out has a MEASURED zero, not missing data: we
// looked where the sidecar would be and there was nothing there.
func TestSubagentCountIsZeroWithoutASidecar(t *testing.T) {
	a := testAdapter(t)
	s := readOne(t, a, tornID)
	if s.Subagents == nil {
		t.Fatal("subagents absent; an absent sidecar directory is a countable zero")
	}
	if *s.Subagents != 0 {
		t.Errorf("counted %d, want 0", *s.Subagents)
	}
}

// The repo fixture pins the FILTER, not the boundary: its mtimes are whatever
// checkout wrote. Two uuid transcripts plus one non-transcript neighbour, so
// the count can never exceed two however fresh the clone is.
func TestSubagentCountNeverExceedsTheTranscriptsPresent(t *testing.T) {
	s := readOne(t, testAdapter(t), healthyID)
	if s.Subagents == nil {
		t.Fatal("subagents absent for a session whose sidecar exists")
	}
	if *s.Subagents > 2 {
		t.Errorf("counted %d, want at most 2 — tool-results.json is not a transcript", *s.Subagents)
	}
}

func TestUUIDFilter(t *testing.T) {
	ok := []string{
		"00000000-aaaa-4bbb-8ccc-000000000001.jsonl",
		"DEADBEEF-0000-4000-8000-00000000FFFF.jsonl",
	}
	bad := []string{
		".memory-sync-manifest.json",
		"notes.jsonl",
		"00000000-aaaa-4bbb-8ccc-00000000000.jsonl", // 35 chars
		"00000000_aaaa_4bbb_8ccc_000000000001.jsonl",
		"0000000g-aaaa-4bbb-8ccc-000000000001.jsonl",
		"00000000-aaaa-4bbb-8ccc-000000000001.txt",
	}
	for _, n := range ok {
		if _, got := sessionIDFromFile(n); !got {
			t.Errorf("%q rejected, want accepted", n)
		}
	}
	for _, n := range bad {
		if id, got := sessionIDFromFile(n); got {
			t.Errorf("%q accepted as session %q, want rejected", n, id)
		}
	}
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

// The one branch that produces the honest "we could not count" state: the
// sidecar path exists but is not listable.
//
// Driven directly rather than through a fixture tree: OS error mapping for
// "file where a directory was expected" is platform-dependent (Windows maps it
// to ErrNotExist, which is the measured-zero branch), so the reliable
// cross-platform trigger for a non-NotExist ReadDir failure is an invalid path
// (embedded NUL).
func TestSubagentCountDegradesWhenTheSidecarIsUnlistable(t *testing.T) {
	a := NewWithRoot(t.TempDir())
	s := &model.Session{Vendor: Vendor, ID: healthyID, ObservedAt: time.Now()}
	a.countSubagents(s, filepath.Join(t.TempDir(), "bad\x00path", healthyID+".jsonl"), time.Now())
	if s.Subagents != nil {
		t.Errorf("count = %d; an unlistable sidecar must be nil — \"0\" would be a claim", *s.Subagents)
	}
	if !s.Degraded.Has(model.FieldSubagents) {
		t.Error("unlistable sidecar must mark the field degraded")
	}
	found := false
	for _, d := range s.Diagnostics {
		if strings.Contains(d, "subagents directory unreadable") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing diagnostic; got %v", s.Diagnostics)
	}
}

// §6 Q8: NTFS defers mtime while the writer holds the transcript, so the
// newest record timestamp must be able to outvote a stale mtime — and a
// future record timestamp must not (a broken clock is not activity).
func TestLastActivityUsesNewestRecordTimestampOverStaleMtime(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "C--Users-dev-code-q8")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	fresh := now.Add(-30 * time.Second)
	rec := `{"type":"user","cwd":"C:/x/q8","timestamp":"` + fresh.Format(time.RFC3339Nano) + `"}` + "\n"
	p := filepath.Join(proj, healthyID+".jsonl")
	if err := os.WriteFile(p, []byte(rec), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := now.Add(-10 * time.Minute)
	if err := os.Chtimes(p, stale, stale); err != nil {
		t.Fatal(err)
	}
	s := readOne(t, NewWithRoot(dir), healthyID)
	if s.LastActivity == nil {
		t.Fatal("last_activity absent")
	}
	if s.LastActivity.Before(fresh.Add(-time.Second)) {
		t.Errorf("last_activity = %v, want the record timestamp (~%v) to outvote the stale mtime (%v)",
			s.LastActivity, fresh, stale)
	}

	// Future record timestamp: excluded, mtime stands.
	future := now.Add(time.Hour)
	rec2 := `{"type":"user","cwd":"C:/x/q8","timestamp":"` + future.Format(time.RFC3339Nano) + `"}` + "\n"
	if err := os.WriteFile(p, []byte(rec2), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, stale, stale); err != nil {
		t.Fatal(err)
	}
	s = readOne(t, NewWithRoot(dir), healthyID)
	if s.LastActivity == nil {
		t.Fatal("last_activity absent")
	}
	if s.LastActivity.After(stale.Add(time.Second)) {
		t.Errorf("last_activity = %v; a FUTURE record timestamp must not outvote the mtime (%v)", s.LastActivity, stale)
	}
}
