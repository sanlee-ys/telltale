package cursor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/sqlite"
)

// The fixture composer ids, from testdata/gen_fixtures.py. All synthetic: this
// repo is public and the real store carries prompt text, encryption keys and
// live access tokens (docs/design.md §3.9).
const (
	idHappy       = "00000000-eeee-4fff-8aaa-000000000001"
	idDerived     = "00000000-eeee-4fff-8aaa-000000000002"
	idNoWorkspace = "00000000-eeee-4fff-8aaa-000000000003"
	idNoData      = "00000000-eeee-4fff-8aaa-000000000004"
	idWindowless  = "00000000-eeee-4fff-8aaa-000000000006"
	idArchived    = "00000000-eeee-4fff-8aaa-000000000007"
	idSubagent    = "00000000-eeee-4fff-8aaa-000000000008"
	idDraftFlag   = "00000000-eeee-4fff-8aaa-000000000009"
	idSkew        = "00000000-eeee-4fff-8aaa-000000000010"
	idNoClock     = "00000000-eeee-4fff-8aaa-000000000011"
	idDraft       = "empty-state-draft"
)

// The three markers planted through every fixture. Each stands in for the real
// thing: prompt text and file lists, live auth tokens, and the plan-entitlement
// constants that must never be rendered as a quota.
const (
	promptMarker      = "SYNTHETIC-PROMPT-TEXT-MUST-NEVER-RENDER"
	credentialMarker  = "SYNTHETIC-CREDENTIAL-MUST-NEVER-BE-READ"
	entitlementMarker = "SYNTHETIC-ENTITLEMENT-MUST-NEVER-RENDER"
)

// baseMS is the fixtures' epoch-millisecond origin; msAt mirrors the
// generator's ms() helper so an expectation reads the same in both files.
const baseMS = 1785700000000

func msAt(offsetSeconds int64) time.Time {
	return time.Unix((baseMS+offsetSeconds*1000)/1000, 0).UTC()
}

func root() string { return filepath.Join("testdata", "root") }

func newAdapter() *Adapter { return NewWithRoot(root()) }

func readOne(t *testing.T, a *Adapter, id string) (*model.Session, error) {
	t.Helper()
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	for _, r := range refs {
		if r.ID == id {
			return a.Read(context.Background(), r)
		}
	}
	t.Fatalf("session %s not discovered (regenerate: cd testdata && uv run python gen_fixtures.py)", id)
	return nil, nil
}

func mustRead(t *testing.T, id string) *model.Session {
	t.Helper()
	s, err := readOne(t, newAdapter(), id)
	if err != nil {
		t.Fatalf("Read(%s): %v", id, err)
	}
	return s
}

// ------------------------------------------------------------- capabilities

func TestCapabilityTable(t *testing.T) {
	caps := newAdapter().Capabilities()
	if err := caps.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []model.Field{
		model.FieldName, model.FieldModel, model.FieldWorkspace, model.FieldLastActivity,
	} {
		if caps.Capability(f) != model.CapReported {
			t.Errorf("%s = %v, want reported", f, caps.Capability(f))
		}
	}
	// context_pct is declared DERIVED even though the vendor's own percentage
	// is usually read verbatim: the declaration is static and cannot say
	// "reported unless it is missing", so it publishes the weaker claim. See
	// Capabilities' doc and decisions/007.
	if caps.Capability(model.FieldContextPercent) != model.CapDerived {
		t.Errorf("context_pct = %v, want derived", caps.Capability(model.FieldContextPercent))
	}
	// The four the package doc argues are unsourceable. Cost and quota in
	// particular are claims about a store that holds a zero and an entitlement
	// and neither of them is a reading — changing either is an argument for
	// §3.9, not a capability bit.
	for _, f := range []model.Field{
		model.FieldCost, model.FieldQuota, model.FieldLiveness, model.FieldSubagents,
	} {
		if caps.Capability(f) != model.CapNone {
			t.Errorf("%s = %v, want none", f, caps.Capability(f))
		}
	}
}

// ---------------------------------------------------------------- discovery

// The filter is load-bearing, not hygiene: five of the eleven fixture rows are
// not sessions, matching the ratio the live survey found.
func TestDiscoveryDropsEverythingThatIsNotASession(t *testing.T) {
	refs, err := newAdapter().Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range refs {
		got[r.ID] = true
		if r.Vendor != Vendor {
			t.Errorf("ref %s has vendor %q", r.ID, r.Vendor)
		}
	}
	for _, want := range []string{idHappy, idDerived, idNoWorkspace, idNoData, idSkew, idNoClock} {
		if !got[want] {
			t.Errorf("session %s was not discovered", want)
		}
	}
	for _, reason := range []struct{ id, why string }{
		{idDraft, "the empty-state draft is not a session"},
		{idWindowless, "a window with no folder open has no session"},
		{idArchived, "archived threads are ignored (the Codex precedent)"},
		{idSubagent, "a sub-agent row is never a top-level row"},
		{idDraftFlag, "value.isDraft marks a draft whose id looks ordinary"},
	} {
		if got[reason.id] {
			t.Errorf("%s was discovered: %s", reason.id, reason.why)
		}
	}
	if len(refs) != 6 {
		t.Errorf("discovered %d sessions, want 6: %v", len(refs), got)
	}
}

func TestAbsentVendorDirectory(t *testing.T) {
	a := NewWithRoot(filepath.Join("testdata", "no-such-tree"))
	if _, err := a.Discover(context.Background()); !errors.Is(err, model.ErrVendorAbsent) {
		t.Errorf("Discover on a missing tree = %v, want ErrVendorAbsent", err)
	}
	if _, err := NewWithRoot("").Discover(context.Background()); !errors.Is(err, model.ErrVendorAbsent) {
		t.Error("an unresolved config dir must report the vendor absent, not panic")
	}
}

// A store whose shape this adapter does not recognize must SAY so. Reporting
// zero sessions instead would tell the user their agents are idle, which is a
// wrong answer rather than a missing one — and the format is undocumented and
// unversioned, so the day the table is renamed will arrive without notice.
func TestUnrecognizedSchemaIsAnErrorNotAnEmptyResult(t *testing.T) {
	a := NewWithRoot(filepath.Join("testdata", "root-noheaders"))
	refs, err := a.Discover(context.Background())
	if !errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("Discover = %d refs, err %v; want ErrSchemaMismatch", len(refs), err)
	}
	if errors.Is(err, model.ErrVendorAbsent) {
		t.Error("a store that exists and cannot be read is not an absent vendor")
	}
	if !strings.Contains(err.Error(), "composerHeaders") {
		t.Errorf("the error does not name the missing table: %v", err)
	}
}

// -------------------------------------------------------------- happy path

func TestHappyPathRead(t *testing.T) {
	s := mustRead(t, idHappy)

	if s.Name == nil || *s.Name != "refactor the widget parser" {
		t.Errorf("name = %v, want the vendor's own session title", s.Name)
	}
	if s.Model == nil || s.Model.ID != "composer-2.5" || s.Model.DisplayName != "composer-2.5" {
		t.Errorf("model = %+v, want composer-2.5 in both fields (the vendor writes one string)", s.Model)
	}
	if s.WorkspaceDir == nil {
		t.Fatal("workspace absent; ws-alpha maps to a folder URI")
	}
	if got := filepath.ToSlash(*s.WorkspaceDir); got != "C:/src/code/example-app" {
		t.Errorf("workspace = %q, want the URI decoded to a native path", *s.WorkspaceDir)
	}
	if s.ContextPercent == nil || float64(*s.ContextPercent) != 37.05234375 {
		t.Errorf("context_pct = %v, want the vendor's own 37.05234375", s.ContextPercent)
	}
	if s.Derived.Has(model.FieldContextPercent) {
		t.Error("the vendor's own percentage was marked as an estimate; it is a reading")
	}
	if got := extra(s, "ctx tokens"); got != "94k / 256k" {
		t.Errorf("ctx tokens = %q, want \"94k / 256k\"", got)
	}
	if s.LastActivity == nil || !s.LastActivity.Equal(msAt(600)) {
		t.Errorf("last_activity = %v, want the newest row timestamp %v", s.LastActivity, msAt(600))
	}
	if !s.Degraded.Empty() {
		t.Errorf("degraded fields on a clean read: %s (%v)", s.Degraded, s.Diagnostics)
	}
	if err := s.Validate(newAdapter().Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// ----------------------------------------------------------------- the WAL

// The load-bearing WAL assertion, and it is stronger for this vendor than for
// any other: §3.9 observed main files of 4096 bytes — one empty page — with
// every byte of content in the sidecar. A reader that opens only the `.db`
// does not report stale data here, it reports NOTHING.
func TestAllContentInTheWALStillReads(t *testing.T) {
	dir := filepath.Join("testdata", "root-wal")
	info, err := os.Stat(filepath.Join(dir, "globalStorage", "state.vscdb"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 8192 {
		t.Fatalf("the fixture's main file is %d bytes; this test needs the "+
			"empty-main shape (regenerate testdata)", info.Size())
	}

	a := NewWithRoot(dir)
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 6 {
		t.Fatalf("discovered %d sessions from a store whose main file is one empty "+
			"page; want 6 — the sidecar was not applied", len(refs))
	}
	s, err := readOne(t, a, idHappy)
	if err != nil {
		t.Fatal(err)
	}
	if s.Model == nil || s.Model.ID != "composer-2.5" {
		t.Errorf("model = %+v; the composerData blob lives in the sidecar too", s.Model)
	}
	if s.ContextPercent == nil {
		t.Error("context_pct absent; the whole store is in the sidecar")
	}
}

// ---------------------------------------------------------------- context %

// The vendor did not write a percentage, but it wrote both raw numbers. The
// adapter computes one — and MARKS it, because used ÷ limit is this program's
// arithmetic and not the vendor's reading.
func TestDerivedContextPercentIsMarked(t *testing.T) {
	s := mustRead(t, idDerived)
	want := 28131.0 / 256000.0 * 100
	if s.ContextPercent == nil || float64(*s.ContextPercent) != want {
		t.Fatalf("context_pct = %v, want %v derived from 28131/256000", s.ContextPercent, want)
	}
	if !s.Derived.Has(model.FieldContextPercent) {
		t.Error("a computed percentage must carry the estimate marker")
	}
	if err := s.Validate(newAdapter().Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// Neither a percentage nor raw counts: the cell is empty. Absence is the
// answer, not a zero and not a guess.
func TestNoContextNumbersIsAbsenceNotZero(t *testing.T) {
	s := mustRead(t, idNoWorkspace)
	if s.ContextPercent != nil {
		t.Errorf("context_pct = %v, want absent", *s.ContextPercent)
	}
	if s.Derived.Has(model.FieldContextPercent) {
		t.Error("an absent field cannot be derived")
	}
	if got := extra(s, "ctx tokens"); got != "" {
		t.Errorf("ctx tokens = %q with no token counts on disk", got)
	}
}

// `default` is an unresolved alias for whatever the server picks. It renders
// verbatim: resolving it would mean naming a model nobody recorded.
func TestDefaultModelRendersLiterally(t *testing.T) {
	s := mustRead(t, idNoWorkspace)
	if s.Model == nil || s.Model.ID != "default" || s.Model.DisplayName != "default" {
		t.Errorf("model = %+v, want the literal string \"default\"", s.Model)
	}
}

// ------------------------------------------------------- cost, quota, zeros

// The cost seam is a schema that is present and never populated: `usageData`
// was `{}` in every session and every message row's `tokenCount` read zero.
// A zero that really means "unpopulated" must not become a rendered $0.00 or a
// rendered 0 tokens, so the whole field is CapNone and no extra carries it
// either.
func TestUnpopulatedZerosNeverBecomeAReading(t *testing.T) {
	a := newAdapter()
	refs, _ := a.Discover(context.Background())
	for _, ref := range refs {
		s, err := a.Read(context.Background(), ref)
		if err != nil {
			t.Fatal(err)
		}
		if s.Cost != nil {
			t.Errorf("session %s reported a cost of %v from a store that records none",
				s.ID, float64(*s.Cost))
		}
		if len(s.Quota) != 0 {
			t.Errorf("session %s reported %d quota windows; the store holds "+
				"entitlements, not consumption", s.ID, len(s.Quota))
		}
		if s.Subagents != nil {
			t.Errorf("session %s reported %d sub-agents; the field is structural only",
				s.ID, *s.Subagents)
		}
		for _, e := range s.Extras {
			if strings.Contains(e.Value, "$") || strings.Contains(strings.ToLower(e.Label), "cost") {
				t.Errorf("session %s carries a money-shaped extra %q = %q", s.ID, e.Label, e.Value)
			}
		}
	}
}

// ------------------------------------------------------------- absence rules

// A session whose workspaceStorage directory is gone — Cursor prunes them —
// still exists. The workspace is ABSENT, nothing failed, and the row renders.
func TestMissingWorkspaceMappingIsAbsenceAndTheSessionSurvives(t *testing.T) {
	s := mustRead(t, idNoWorkspace)
	if s.WorkspaceDir != nil {
		t.Errorf("workspace = %q, want absent", *s.WorkspaceDir)
	}
	if s.Degraded.Has(model.FieldWorkspace) {
		t.Error("an absent mapping was marked degraded; nothing failed to read")
	}
	if s.Name == nil || *s.Name != "triage the flaky test" {
		t.Errorf("name = %v; the rest of the row must survive", s.Name)
	}
	if err := s.Validate(newAdapter().Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// A workspace.json that exists and does not parse is a FAILED read, and the
// two are different facts: this one is degraded and says why.
func TestUnparseableWorkspaceMappingDegrades(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, root(), dir)
	bad := filepath.Join(dir, "workspaceStorage", "ws-alpha", "workspace.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := readOne(t, NewWithRoot(dir), idHappy)
	if err != nil {
		t.Fatal(err)
	}
	if s.WorkspaceDir != nil {
		t.Error("a degraded field must be absent")
	}
	if !s.Degraded.Has(model.FieldWorkspace) {
		t.Error("an unreadable mapping must be marked degraded")
	}
	if s.Name == nil || s.LastActivity == nil {
		t.Error("the rest of the row went missing with the workspace")
	}
	if err := s.Validate(newAdapter().Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// A workspace that names no folder (a window opened without one) has nothing
// to show. Absence, not failure — and the session keeps its id-derived name.
func TestFolderlessWorkspaceIsAbsenceAndTheNameFallsBack(t *testing.T) {
	s := mustRead(t, idNoData)
	if s.WorkspaceDir != nil {
		t.Errorf("workspace = %q, want absent", *s.WorkspaceDir)
	}
	if s.Degraded.Has(model.FieldWorkspace) {
		t.Error("a folderless workspace was marked degraded")
	}
	if s.Name == nil || *s.Name != idNoData[:nameLen] {
		t.Errorf("name = %v, want the composerId's first %d characters", s.Name, nameLen)
	}
	// No composerData row at all: the model is absent rather than empty-string.
	if s.Model != nil {
		t.Errorf("model = %+v, want absent — this session has no composerData row", s.Model)
	}
}

// ---------------------------------------------------------------- time rules

// The header columns are epoch milliseconds and ISO-8601 strings appear
// elsewhere in the same undocumented store, so the timestamp reader accepts
// both. This row's newest signal is an ISO string sitting in an INTEGER
// column, and it must win over the older epoch-ms one beside it.
func TestMixedTimestampsParseBothWays(t *testing.T) {
	s := mustRead(t, idNoData)
	want, err := time.Parse(time.RFC3339, "2026-08-02T21:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if s.LastActivity == nil {
		t.Fatal("last_activity absent; the ISO-8601 checkpoint is readable")
	}
	if !s.LastActivity.Equal(want) {
		t.Errorf("last_activity = %v, want the ISO-8601 checkpoint %v (it is newer "+
			"than the epoch-ms recency beside it)", s.LastActivity, want)
	}
}

func TestFlexTimeAcceptsBothEncodingsAndRejectsNonsense(t *testing.T) {
	iso, _ := time.Parse(time.RFC3339, "2026-08-02T21:00:00Z")
	text := func(s string) sqlite.Value {
		return sqlite.Value{Type: sqlite.Text, Bytes: []byte(s)}
	}
	for _, c := range []struct {
		name string
		val  sqlite.Value
		want time.Time
		ok   bool
	}{
		{"epoch ms", sqlite.Value{Type: sqlite.Int, Int: baseMS}, msAt(0), true},
		{"epoch ms as float", sqlite.Value{Type: sqlite.Float, Float: baseMS}, msAt(0), true},
		{"iso-8601", text("2026-08-02T21:00:00Z"), iso, true},
		{"iso-8601 with fraction", text("2026-08-02T21:00:00.000Z"), iso, true},
		{"numeric string", text("1785700000000"), msAt(0), true},
		{"zero is absence, not 1970", sqlite.Value{Type: sqlite.Int}, time.Time{}, false},
		{"negative is absence", sqlite.Value{Type: sqlite.Int, Int: -5}, time.Time{}, false},
		{"null", sqlite.Value{Type: sqlite.Null}, time.Time{}, false},
		{"empty string", text(""), time.Time{}, false},
		{"not a timestamp", text("yesterday"), time.Time{}, false},
		{"a blob is not a timestamp", sqlite.Value{Type: sqlite.Blob, Bytes: []byte("x")}, time.Time{}, false},
	} {
		got, ok := flexTime(c.val)
		if ok != c.ok {
			t.Errorf("%s: ok = %v, want %v", c.name, ok, c.ok)
			continue
		}
		if ok && !got.Equal(c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// A timestamp ahead of the observation clock has no readable age. It is
// skipped and the next-best signal carries the field, rather than the row
// rendering "0s" off a bad clock.
func TestFutureTimestampIsSkippedNotRendered(t *testing.T) {
	s := mustRead(t, idSkew)
	if s.LastActivity == nil {
		t.Fatal("last_activity degraded; `recency` is still readable")
	}
	if s.LastActivity.After(time.Now().Add(futureSkew)) {
		t.Errorf("last_activity = %v is ahead of the clock", s.LastActivity)
	}
	if !s.LastActivity.Equal(msAt(700)) {
		t.Errorf("last_activity = %v, want the readable recency %v", s.LastActivity, msAt(700))
	}
}

// With every timestamp unreadable the field degrades rather than guessing —
// and it degrades rather than borrowing the store's file mtime, which would
// date this session by when Cursor last wrote anything at all.
func TestNoReadableTimestampDegradesLastActivity(t *testing.T) {
	s := mustRead(t, idNoClock)
	if s.LastActivity != nil {
		t.Errorf("last_activity = %v, want absent", s.LastActivity)
	}
	if !s.Degraded.Has(model.FieldLastActivity) {
		t.Error("an unreadable activity signal must be marked degraded")
	}
	if err := s.Validate(newAdapter().Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// ------------------------------------------------------------- the mirror

// `ItemTable['composer.composerHeaders']` is a legacy JSON mirror of the table
// and it is STALE. The fixture makes them disagree on purpose: the mirror
// names three composers, every one of which the filter drops, so an adapter
// reading the mirror reports ZERO sessions on a store holding six. The table
// wins — and the mirror is in ItemTable, which the credential rule forbids
// walking at all.
func TestTheTableWinsOverTheStaleLegacyMirror(t *testing.T) {
	refs, err := newAdapter().Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) == 0 {
		t.Fatal("zero sessions: the stale ItemTable mirror was read instead of the table")
	}
	for _, r := range refs {
		for _, mirrored := range []string{idDraft, idWindowless, idArchived} {
			if r.ID == mirrored {
				t.Errorf("session %s came from the mirror; the table drops it", r.ID)
			}
		}
	}
}

// ------------------------------------------------------------------ the gate

// Every session this adapter can produce must satisfy the present-XOR-degraded
// contract against its own capability table. This is the machine-checked form
// of the honest-gauge rule, run over every fixture rather than the handful a
// hand-written assertion would remember.
func TestEveryProducedSessionValidates(t *testing.T) {
	a := newAdapter()
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	read := 0
	for _, ref := range refs {
		s, err := a.Read(context.Background(), ref)
		if err != nil {
			t.Errorf("Read(%s): %v", ref.ID, err)
			continue
		}
		read++
		if err := s.Validate(a.Capabilities()); err != nil {
			t.Errorf("session %s: %v", s.Key(), err)
		}
	}
	if read != 6 {
		t.Errorf("read %d sessions, want 6", read)
	}
}

// The boundary that outranks every field on the map, asserted rather than
// promised. The fixtures plant prompt text, credential-shaped ItemTable keys
// and plan-entitlement constants; none of the three may reach any field, any
// extra or any diagnostic. In the real store those markers are live access
// tokens.
func TestNothingOffTheAllowlistReachesASession(t *testing.T) {
	a := newAdapter()
	refs, _ := a.Discover(context.Background())
	if len(refs) == 0 {
		t.Fatal("no sessions to check")
	}
	for _, ref := range refs {
		s, err := a.Read(context.Background(), ref)
		if err != nil {
			t.Fatal(err)
		}
		var carried []string
		carried = append(carried, s.ID, ref.Locator)
		if s.Name != nil {
			carried = append(carried, *s.Name)
		}
		if s.WorkspaceDir != nil {
			carried = append(carried, *s.WorkspaceDir)
		}
		if s.Model != nil {
			carried = append(carried, s.Model.ID, s.Model.DisplayName)
		}
		carried = append(carried, s.Diagnostics...)
		for _, e := range s.Extras {
			carried = append(carried, e.Label, e.Value)
		}
		for _, v := range carried {
			for _, marker := range []string{promptMarker, credentialMarker, entitlementMarker} {
				if strings.Contains(v, marker) {
					t.Errorf("session %s surfaced off-allowlist content: %q", s.ID, v)
				}
			}
		}
	}
}

// ------------------------------------------------------------------- helpers

func TestFileURIConversion(t *testing.T) {
	cases := []struct {
		uri  string
		want string // in slash form; the adapter returns native separators
	}{
		// Cursor writes the drive letter lower-cased and percent-encodes the
		// colon; the HUD's folder column shows the upper-cased form every other
		// adapter produces.
		{"file:///c%3A/Users/dev/code/example-app", "C:/Users/dev/code/example-app"},
		{"file:///C:/src/code/example-app", "C:/src/code/example-app"},
		{"file:///home/dev/code/example-app", "/home/dev/code/example-app"},
		{"file:///c%3A/src/with%20space", "C:/src/with space"},
		{"file:///c%3A/src/%C3%A9", "C:/src/é"},
		{"", ""},
		{"https://example.com/x", ""},
		{"file://server/share", ""}, // UNC: not converted rather than guessed
		{"file:///c%3A/bad%zz", ""}, // a broken escape yields absence
		{"file:///c%3A/trunc%", ""},
	}
	for _, c := range cases {
		got := filepath.ToSlash(pathFromFileURI(c.uri))
		if got != c.want {
			t.Errorf("pathFromFileURI(%q) = %q, want %q", c.uri, got, c.want)
		}
	}
}

// ----------------------------------------------------------------- utilities

func extra(s *model.Session, label string) string {
	for _, e := range s.Extras {
		if e.Label == label {
			return e.Value
		}
	}
	return ""
}

// copyTree copies a fixture tree so a test can mutate it. Fixtures are
// read-only inputs; a test that edits testdata/ in place breaks every test
// that runs after it.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}
