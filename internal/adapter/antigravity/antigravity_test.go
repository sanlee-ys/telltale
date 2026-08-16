package antigravity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The fixture conversation ids, from testdata/gen_fixtures.py. All synthetic:
// this repo is public and the real corpus is prompt text and an email address.
const (
	idHappy        = "00000000-dddd-4eee-8fff-000000000001"
	idWAL          = "00000000-dddd-4eee-8fff-000000000002"
	idBroken       = "00000000-dddd-4eee-8fff-000000000003"
	idNoWorkspace  = "00000000-dddd-4eee-8fff-000000000004"
	idZero         = "00000000-dddd-4eee-8fff-000000000005"
	idNoTranscript = "00000000-dddd-4eee-8fff-000000000006"
)

// piiMarker is planted in every fixture transcript's `content` and `thinking`.
// It stands in for the real thing: full prompt text and file contents.
const piiMarker = "SYNTHETIC-PROMPT-TEXT-MUST-NEVER-RENDER"

func root() string { return filepath.Join("testdata", "root") }

func newAdapter() *Adapter { return NewWithRoot(root()) }

// readOne discovers and reads one conversation by id.
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
	t.Fatalf("conversation %s not discovered (regenerate: cd testdata && uv run python gen_fixtures.py)", id)
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
		model.FieldModel, model.FieldWorkspace, model.FieldLastActivity,
	} {
		if caps.Capability(f) != model.CapReported {
			t.Errorf("%s = %v, want reported", f, caps.Capability(f))
		}
	}
	// The six the package doc argues at length are unsourceable. A change
	// here is a claim about the vendor's disk and must be argued in §3.8
	// first, not slipped in with a capability bit. name joined this list
	// 2026-08-12: no on-disk title exists, so the HUD sources the row's
	// label itself rather than this adapter filling it from the id.
	for _, f := range []model.Field{
		model.FieldName, model.FieldContextPercent, model.FieldCost, model.FieldQuota,
		model.FieldLiveness, model.FieldSubagents,
	} {
		if caps.Capability(f) != model.CapNone {
			t.Errorf("%s = %v, want none", f, caps.Capability(f))
		}
	}
}

// ---------------------------------------------------------------- discovery

func TestDiscoverEnumeratesConversationsNotTheSummaryIndex(t *testing.T) {
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
		if r.LastActivity == nil {
			t.Errorf("ref %s has no freshness hint", r.ID)
		}
	}
	// Seven conversations exist on disk; conversation_summaries.db names one.
	// Trusting the index would hide six sessions that are right there.
	for _, want := range []string{
		idHappy, idWAL, idBroken, idNoWorkspace, idZero, idNoTranscript, idMultiChunk,
	} {
		if !got[want] {
			t.Errorf("conversation %s was not discovered", want)
		}
	}
	if len(refs) != 7 {
		t.Errorf("discovered %d conversations, want 7: %v", len(refs), got)
	}
	// The sidecars are not sessions.
	for id := range got {
		if strings.HasSuffix(id, "-wal") || strings.HasSuffix(id, "-shm") {
			t.Errorf("sidecar %q discovered as a session", id)
		}
	}
}

func TestAbsentVendorDirectory(t *testing.T) {
	a := NewWithRoot(filepath.Join("testdata", "no-such-tree"))
	if _, err := a.Discover(context.Background()); !errors.Is(err, model.ErrVendorAbsent) {
		t.Errorf("Discover on a missing tree = %v, want ErrVendorAbsent", err)
	}
	if _, err := NewWithRoot("").Discover(context.Background()); !errors.Is(err, model.ErrVendorAbsent) {
		t.Error("an unresolved home must report the vendor absent, not panic")
	}
}

// -------------------------------------------------------------- happy path

func TestHappyPathRead(t *testing.T) {
	s := mustRead(t, idHappy)

	if s.Name != nil {
		t.Errorf("name = %v, want absent — no on-disk title, the HUD sources the label itself", *s.Name)
	}
	if s.ID != idHappy {
		t.Errorf("id = %q, want the full conversation id", s.ID)
	}
	if s.Model == nil || s.Model.DisplayName != "Gemini 3.6 Flash (High)" ||
		s.Model.ID != "gemini-3.6-flash" {
		t.Errorf("model = %+v, want the vendor's id and display name", s.Model)
	}
	if s.WorkspaceDir == nil {
		t.Fatal("workspace absent; the trajectory blob carries a file:/// URI")
	}
	if got := filepath.ToSlash(*s.WorkspaceDir); got != "C:/src/code/example-app" {
		t.Errorf("workspace = %q, want the URI converted to a native path", *s.WorkspaceDir)
	}
	if s.LastActivity == nil {
		t.Error("last_activity absent")
	}
	if !s.Degraded.Empty() {
		t.Errorf("degraded fields on a clean read: %s (%v)", s.Degraded, s.Diagnostics)
	}
	if err := s.Validate(newAdapter().Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// The second fixture generation is 25 KiB against a 4 KiB page, so its record
// spans an overflow chain. If the chain were dropped the blob would parse
// short and the generation would vanish from the totals.
func TestOverflowBlobIsReadWhole(t *testing.T) {
	s := mustRead(t, idHappy)
	if got := extra(s, "generations"); got != "2" {
		t.Errorf("generations = %q, want 2 — the overflowing row did not decode", got)
	}
	// 18099 + 22617 = 40716 -> "40k"; 30 + 350 = 380.
	if got := extra(s, "uncached in"); got != "40k" {
		t.Errorf("uncached in = %q, want 40k", got)
	}
	if got := extra(s, "output"); got != "380" {
		t.Errorf("output = %q, want 380", got)
	}
}

// The dedup key is the per-generation response id. The top-level #4 UUID is
// constant for a whole conversation, and deduping on it would collapse two
// generations into one — this fixture would report 1 generation, not 2.
func TestGenerationsDedupOnTheResponseIDNotTheConversationUUID(t *testing.T) {
	s := mustRead(t, idHappy)
	if got := extra(s, "generations"); got != "2" {
		t.Errorf("generations = %q, want 2: two rows carry the same top-level "+
			"conversation UUID and different response ids", got)
	}
}

// --------------------------------------------------------------------- WAL

// The load-bearing WAL assertion: the sidecar holds the committed model and
// the base file holds the superseded one.
func TestWALOverlayChangesTheModel(t *testing.T) {
	s := mustRead(t, idWAL)
	if s.Model == nil {
		t.Fatal("no model")
	}
	if s.Model.DisplayName != "Gemini 3.6 Pro (High)" {
		t.Errorf("model = %q, want \"Gemini 3.6 Pro (High)\" — the WAL sidecar's "+
			"committed value must win over the base file's", s.Model.DisplayName)
	}
	if got := extra(s, "uncached in"); got != "2k" {
		t.Errorf("uncached in = %q, want 2k (the sidecar's generation, not the base file's 1000)", got)
	}
}

// A missing sidecar is the normal case for a checkpointed conversation and
// must not degrade anything.
func TestMissingWALIsFine(t *testing.T) {
	if _, err := os.Stat(filepath.Join(root(), "conversations", idHappy+".db-wal")); !os.IsNotExist(err) {
		t.Skip("the happy fixture grew a sidecar; regenerate testdata")
	}
	s := mustRead(t, idHappy)
	if !s.Degraded.Empty() || len(s.Diagnostics) != 0 {
		t.Errorf("a conversation with no sidecar degraded: %s %v", s.Degraded, s.Diagnostics)
	}
}

// A sidecar whose frames fail their checksums is dropped, the base file's
// (older) values are used, and the row SAYS so. The alternative — trusting a
// frame that failed its own integrity check — is how a torn read becomes a
// rendered number.
func TestCorruptWALDegradesToTheBaseFileAndSaysSo(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, root(), dir)

	wal := filepath.Join(dir, "conversations", idWAL+".db-wal")
	raw, err := os.ReadFile(wal)
	if err != nil {
		t.Fatal(err)
	}
	raw[32+24+64] ^= 0xff // a byte inside the first frame's page image
	if err := os.WriteFile(wal, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := readOne(t, NewWithRoot(dir), idWAL)
	if err != nil {
		t.Fatal(err)
	}
	if s.Model == nil || s.Model.DisplayName != "Gemini 3.6 Flash (High)" {
		t.Errorf("model = %+v, want the base file's superseded value", s.Model)
	}
	if !hasDiagnostic(s, "wal") {
		t.Errorf("no diagnostic named the rejected sidecar: %v", s.Diagnostics)
	}
}

// ------------------------------------------------------------- the invariant

// thinking + answer must equal output. A generation that fails its own
// arithmetic contributes nothing: the field numbers are reverse-engineered
// from an unversioned wire format, and a number that fails its self-check is
// evidence the guess is wrong here, not a number to render.
func TestInvariantViolationDropsTheTokensRatherThanRenderingThem(t *testing.T) {
	s := mustRead(t, idBroken)
	for _, label := range []string{"uncached in", "output", "generations"} {
		if got := extra(s, label); got != "" {
			t.Errorf("extra %q = %q; a generation that failed its self-check must render nothing", label, got)
		}
	}
	if !hasDiagnostic(s, "self-check") {
		t.Errorf("no diagnostic explains the dropped tokens: %v", s.Diagnostics)
	}
	// The rest of the row survives: a bad token reading is not a bad session.
	if s.Model == nil || s.Model.DisplayName == "" {
		t.Error("the model went missing with the tokens")
	}
	if s.LastActivity == nil {
		t.Error("last_activity went missing with the tokens")
	}
	if err := s.Validate(newAdapter().Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// Zero is a measurement — the vendor counted and wrote down zero — and the
// invariant holds for it (0 + 0 == 0). It must survive as a rendered zero, not
// collapse into absence.
func TestZeroTokensAreDataNotAbsence(t *testing.T) {
	s := mustRead(t, idZero)
	if got := extra(s, "uncached in"); got != "0" {
		t.Errorf("uncached in = %q, want \"0\": the vendor said zero", got)
	}
	if got := extra(s, "output"); got != "0" {
		t.Errorf("output = %q, want \"0\"", got)
	}
	if hasDiagnostic(s, "self-check") {
		t.Errorf("an all-zero generation was treated as a failure: %v", s.Diagnostics)
	}
}

// ------------------------------------------------------------- absence rules

// A conversation started outside a workspace has no URI to read. That is
// absence, and absence is not degradation: nothing failed.
func TestNoWorkspaceIsAbsenceNotDegradation(t *testing.T) {
	s := mustRead(t, idNoWorkspace)
	if s.WorkspaceDir != nil {
		t.Errorf("workspace = %q, want absent", *s.WorkspaceDir)
	}
	if s.Degraded.Has(model.FieldWorkspace) {
		t.Error("an absent workspace was marked degraded; nothing failed to read")
	}
	if err := s.Validate(newAdapter().Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// A conversation with no transcript has no activity signal and never had one.
// It is reported with a typed sentinel and the HUD drops the row, exactly as
// the Gemini adapter reports a sub-agent file.
func TestMissingTranscriptIsSkippedWithASentinel(t *testing.T) {
	_, err := readOne(t, newAdapter(), idNoTranscript)
	if !errors.Is(err, ErrNoTranscript) {
		t.Errorf("Read = %v, want ErrNoTranscript", err)
	}
}

// A database that vanished between Discover and Read is normal operation.
func TestVanishedDatabaseIsSessionGone(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, root(), dir)
	if err := os.Remove(filepath.Join(dir, "conversations", idHappy+".db")); err != nil {
		t.Fatal(err)
	}
	a := NewWithRoot(dir)
	_, err := a.Read(context.Background(), model.SessionRef{
		Vendor:  Vendor,
		ID:      idHappy,
		Locator: filepath.Join(dir, "conversations", idHappy+".db"),
	})
	if !errors.Is(err, model.ErrSessionGone) {
		t.Errorf("Read on a vanished database = %v, want ErrSessionGone", err)
	}
}

// A database that is present but unreadable degrades the two fields it
// sources, and the transcript-sourced row still renders.
func TestUnreadableDatabaseDegradesRatherThanDroppingTheRow(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, root(), dir)
	db := filepath.Join(dir, "conversations", idHappy+".db")
	if err := os.WriteFile(db, []byte("this is not a database"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := readOne(t, NewWithRoot(dir), idHappy)
	if err != nil {
		t.Fatalf("a broken database must not drop the row: %v", err)
	}
	if !s.Degraded.Has(model.FieldModel) || !s.Degraded.Has(model.FieldWorkspace) {
		t.Errorf("degraded = %s, want model and workspace", s.Degraded)
	}
	if s.Model != nil || s.WorkspaceDir != nil {
		t.Error("a degraded field must be absent")
	}
	if s.LastActivity == nil {
		t.Error("the transcript still dates this session; last_activity must survive")
	}
	if err := s.Validate(newAdapter().Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// ---------------------------------------------------------------- time rules

// §6 Q8: last_activity is the fresher of the file mtimes and the newest step
// timestamp. The fixture's steps are stamped 2026-08-01; the checked-out files
// carry today's mtime, so the mtime side normally wins — and when the files are
// backdated, the step timestamp must take over.
func TestLastActivityTakesTheFresherSignal(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, root(), dir)

	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range []string{
		filepath.Join(dir, "conversations", idHappy+".db"),
		filepath.Join(dir, "brain", idHappy, ".system_generated", "logs", "transcript.jsonl"),
	} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}

	s, err := readOne(t, NewWithRoot(dir), idHappy)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 8, 1, 11, 59, 48, 0, time.UTC)
	if s.LastActivity == nil || !s.LastActivity.Equal(want) {
		t.Errorf("last_activity = %v, want the newest step timestamp %v", s.LastActivity, want)
	}
}

// A file mtime ahead of the observation clock has no readable age. It is
// skipped, and the step timestamp carries the field instead of the row
// rendering "0s" off a bad clock.
func TestFutureMtimeIsSkippedNotRendered(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, root(), dir)

	future := time.Now().Add(48 * time.Hour)
	for _, p := range []string{
		filepath.Join(dir, "conversations", idHappy+".db"),
		filepath.Join(dir, "brain", idHappy, ".system_generated", "logs", "transcript.jsonl"),
	} {
		if err := os.Chtimes(p, future, future); err != nil {
			t.Fatal(err)
		}
	}

	s, err := readOne(t, NewWithRoot(dir), idHappy)
	if err != nil {
		t.Fatal(err)
	}
	if s.LastActivity == nil {
		t.Fatal("last_activity degraded; the step timestamps are still readable")
	}
	if s.LastActivity.After(time.Now().Add(futureSkew)) {
		t.Errorf("last_activity = %v is ahead of the clock", s.LastActivity)
	}
	want := time.Date(2026, 8, 1, 11, 59, 48, 0, time.UTC)
	if !s.LastActivity.Equal(want) {
		t.Errorf("last_activity = %v, want the newest step timestamp %v", s.LastActivity, want)
	}
}

// With every timestamp unreadable the field degrades rather than guessing.
func TestNoReadableTimestampDegradesLastActivity(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, root(), dir)

	logs := filepath.Join(dir, "brain", idHappy, ".system_generated", "logs", "transcript.jsonl")
	if err := os.WriteFile(logs, []byte("{\"step_index\":0,\"status\":\"DONE\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(48 * time.Hour)
	for _, p := range []string{
		filepath.Join(dir, "conversations", idHappy+".db"),
		logs,
	} {
		if err := os.Chtimes(p, future, future); err != nil {
			t.Fatal(err)
		}
	}

	s, err := readOne(t, NewWithRoot(dir), idHappy)
	if err != nil {
		t.Fatal(err)
	}
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

// ------------------------------------------------------------------ the gate

// Every session this adapter can produce must satisfy the present-XOR-degraded
// contract against its own capability table. This is the machine-checked form
// of the honest-gauge rule, and it runs over every fixture rather than the
// handful a hand-written assertion would remember.
func TestEveryProducedSessionValidates(t *testing.T) {
	a := newAdapter()
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	read := 0
	for _, ref := range refs {
		s, err := a.Read(context.Background(), ref)
		if errors.Is(err, ErrNoTranscript) {
			continue
		}
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
		t.Errorf("read %d sessions, want 6 (seven conversations, one with no transcript)", read)
	}
}

// The PII boundary, asserted rather than promised. Every fixture transcript
// carries a marker string in `content` and `thinking`; this repo is public and
// the real ones carry prompt text, file contents and an email address.
func TestTranscriptContentNeverReachesASession(t *testing.T) {
	a := newAdapter()
	refs, _ := a.Discover(context.Background())
	for _, ref := range refs {
		s, err := a.Read(context.Background(), ref)
		if err != nil {
			continue
		}
		var carried []string
		if s.Name != nil {
			carried = append(carried, *s.Name)
		}
		if s.WorkspaceDir != nil {
			carried = append(carried, *s.WorkspaceDir)
		}
		if s.Model != nil {
			carried = append(carried, s.Model.ID, s.Model.DisplayName)
		}
		carried = append(carried, s.ID)
		carried = append(carried, s.Diagnostics...)
		for _, e := range s.Extras {
			carried = append(carried, e.Label, e.Value)
		}
		for _, v := range carried {
			if strings.Contains(v, piiMarker) {
				t.Errorf("session %s surfaced transcript content: %q", s.ID, v)
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
		{"file:///C:/src/code/example-app", "C:/src/code/example-app"},
		{"file:///home/dev/code/example-app", "/home/dev/code/example-app"},
		{"file:///C:/src/with%20space", "C:/src/with space"},
		{"file:///C:/src/%C3%A9", "C:/src/é"},
		{"", ""},
		{"https://example.com/x", ""},
		{"file://server/share", ""}, // UNC: not converted rather than guessed
		{"file:///C:/bad%zz", ""},   // a broken escape yields absence
		{"file:///C:/trunc%", ""},
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

func hasDiagnostic(s *model.Session, substr string) bool {
	for _, d := range s.Diagnostics {
		if strings.Contains(strings.ToLower(d), strings.ToLower(substr)) {
			return true
		}
	}
	return false
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
