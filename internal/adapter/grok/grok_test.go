package grok

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

const fixtureRoot = "testdata/sessions"

// forbiddenMarker is planted in the two files this adapter promises never to
// open — prompt_context.json (which inlines the user's AGENTS.md verbatim) and
// chat_history.jsonl (the transcript). See TestNothingFromTheUnreadFilesReaches
// TheSession.
const forbiddenMarker = "SYNTHETIC-FORBIDDEN-CONTENT-MARKER"

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

// TestDiscoverFindsSessionDirectoriesAndNothingElse pins the four shapes in the
// tree that are NOT sessions. Every one of them exists in the real store, and
// three of them would be picked up by a walk that looked one level less
// carefully.
func TestDiscoverFindsSessionDirectoriesAndNothingElse(t *testing.T) {
	refs := discover(t, fixtureRoot)

	want := []string{
		"00000000-1111-7222-8333-000000000001",
		"00000000-1111-7222-8333-000000000002",
		"00000000-1111-7222-8333-000000000003",
		"00000000-1111-7222-8333-000000000004",
		"00000000-1111-7222-8333-000000000005",
	}
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
			t.Errorf("ref %d has no freshness hint; the hint is summary.json's mtime, not the directory's", i)
		}
	}
	// The excluded four, named so a future "simplification" of the walk fails
	// here rather than in the HUD: a non-UUID directory, a session directory
	// with no summary.json yet, a workspace-level file, and the full-text index
	// at the sessions root.
	for _, ref := range refs {
		switch filepath.Base(ref.Locator) {
		case "not-a-uuid", "00000000-1111-7222-8333-000000000009",
			"prompt_history.jsonl", "session_search.sqlite":
			t.Errorf("Discover returned %q, which is not a session", ref.Locator)
		}
	}
}

// TestReadSourcesEveryFieldTheSurveyFound is the field map, asserted.
func TestReadSourcesEveryFieldTheSurveyFound(t *testing.T) {
	s := read(t, fixtureRoot, "00000000-1111-7222-8333-000000000001")

	if s.Name == nil || *s.Name != "Example App Session" {
		t.Errorf("name = %v, want the generated_title", s.Name)
	}
	if id, _ := s.Model.Name(); id != "grok-4.5" {
		t.Errorf("model = %q, want current_model_id", id)
	}
	if s.WorkspaceDir == nil || *s.WorkspaceDir != `C:\src\code\example-app` {
		t.Errorf("workspace = %v, want info.cwd verbatim", s.WorkspaceDir)
	}
	if s.ContextPercent == nil || *s.ContextPercent != 7 {
		t.Errorf("context = %v, want the vendor's own contextWindowUsage of 7", s.ContextPercent)
	}
	if s.LastActivity == nil {
		t.Fatal("last_activity absent")
	}
	if !s.Degraded.Empty() {
		t.Errorf("a healthy session degraded %s", s.Degraded)
	}

	// The extras, and the shape of the money claim.
	if v, ok := extra(s, "turn cost"); !ok || v != "$0.0747" {
		t.Errorf("turn cost extra = %q/%v, want the LAST turn_completed record's costUsdTicks at 1e10", v, ok)
	}
	if v, ok := extra(s, "turn tokens"); !ok || v != "143k" {
		t.Errorf("turn tokens extra = %q/%v", v, ok)
	}
	if v, ok := extra(s, "ctx tokens"); !ok || v != "39k" {
		t.Errorf("ctx tokens extra = %q/%v", v, ok)
	}
	if v, ok := extra(s, "ctx window"); !ok || v != "500k" {
		t.Errorf("ctx window extra = %q/%v", v, ok)
	}

	if err := s.Validate(NewWithRoot(fixtureRoot).Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// TestTheSessionTotalCostIsNeverClaimed is the honesty rule at its sharpest
// point on this vendor.
//
// grok is the one vendor whose store carries a real dollar figure, and the
// temptation is to put it in the COST column. It is a PER-TURN figure — the
// fixture's two turns are $0.0455 and $0.0747, and the second is not the sum —
// so the column stays empty and the number lives in an Extra whose label says
// which turn it belongs to.
func TestTheSessionTotalCostIsNeverClaimed(t *testing.T) {
	caps := NewWithRoot(fixtureRoot).Capabilities()
	if caps.Capability(model.FieldCost) != model.CapNone {
		t.Error("cost is declared; the store holds no session total, so any value would be a sum over whatever the tail window happened to reach")
	}
	s := read(t, fixtureRoot, "00000000-1111-7222-8333-000000000001")
	if s.Cost != nil {
		t.Errorf("cost = %v, want nil", *s.Cost)
	}
	if s.Has(model.FieldCost) {
		t.Error("session reports a cost value")
	}
	if v, _ := extra(s, "turn cost"); !strings.HasPrefix(v, "$") {
		t.Errorf("the turn's cost is not carried at all (%q); the measurement should survive as an extra", v)
	}
}

// TestHeadlessSessionHasNoTitleAndNoContextReading covers the shape a
// `--single` run leaves behind: session_summary is the empty string, there is
// no generated_title key at all, and signals.json has not been written because
// no turn boundary has passed through the writer that emits it.
//
// Both absences must be ABSENT-NOW and not degraded — the vendor has nothing to
// say yet, which is different from a read that failed.
func TestHeadlessSessionHasNoTitleAndNoContextReading(t *testing.T) {
	s := read(t, fixtureRoot, "00000000-1111-7222-8333-000000000002")

	if s.Name != nil {
		t.Errorf("name = %q, want absent on a session the vendor never titled", *s.Name)
	}
	if s.ContextPercent != nil {
		t.Errorf("context = %v, want absent with no signals.json", *s.ContextPercent)
	}
	if s.Degraded.Has(model.FieldName) || s.Degraded.Has(model.FieldContextPercent) {
		t.Errorf("degraded %s — a file the vendor has not written yet is absence, not a failed read", s.Degraded)
	}
	if len(s.Diagnostics) != 0 {
		t.Errorf("diagnostics %v, want none", s.Diagnostics)
	}
	// The row still carries what it does have.
	if id, _ := s.Model.Name(); id != "grok-4.5" {
		t.Errorf("model = %q", id)
	}
	if v, ok := extra(s, "turn cost"); !ok || v != "$0.0306" {
		t.Errorf("turn cost = %q/%v", v, ok)
	}
}

// TestZeroContextIsAReading is this repo's founding distinction, on the one
// vendor that reports the percentage rather than deriving it: a session that
// has used none of its window renders a full empty track, not an em dash.
//
// The same fixture pins the workspace FALLBACK: its summary.json parsed and
// carried an empty cwd, so the path comes from decoding the directory name.
func TestZeroContextIsAReading(t *testing.T) {
	s := read(t, fixtureRoot, "00000000-1111-7222-8333-000000000004")

	if s.ContextPercent == nil {
		t.Fatal("context absent; a measured 0 must survive as a value")
	}
	if *s.ContextPercent != 0 {
		t.Errorf("context = %v, want 0", *s.ContextPercent)
	}
	if !s.Has(model.FieldContextPercent) {
		t.Error("Has(context) is false for a measured zero")
	}
	if s.WorkspaceDir == nil || *s.WorkspaceDir != `C:\src\code\example-app` {
		t.Errorf("workspace = %v, want the decoded directory name", s.WorkspaceDir)
	}
	// contextTokensUsed of 0 is a measured zero too, but "0" in a detail pane
	// beside a window size reads as a broken extra rather than an empty one, so
	// only a positive reading becomes an extra.
	if _, ok := extra(s, "ctx tokens"); ok {
		t.Error("a zero token count became an extra")
	}
}

// TestTornSummaryDegradesTheRowAndDoesNotFailIt: racing the vendor's writer is
// routine, and a half-written summary.json must cost the fields it feeds and
// nothing else. The row still exists, with its id.
func TestTornSummaryDegradesTheRowAndDoesNotFailIt(t *testing.T) {
	s := read(t, fixtureRoot, "00000000-1111-7222-8333-000000000003")

	for _, f := range []model.Field{model.FieldName, model.FieldModel, model.FieldWorkspace, model.FieldLastActivity} {
		if !s.Degraded.Has(f) {
			t.Errorf("%s not degraded after an unparseable summary.json", f)
		}
		if s.Has(f) {
			t.Errorf("%s carries a value from an unparseable summary.json", f)
		}
	}
	if len(s.Diagnostics) == 0 {
		t.Error("no diagnostic explaining the degradation")
	}
	for _, d := range s.Diagnostics {
		if strings.Contains(d, `C:\src`) {
			t.Errorf("diagnostic %q carries corpus content; this repo is public", d)
		}
	}
	if err := s.Validate(NewWithRoot(fixtureRoot).Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// TestOutOfRangeContextIsDroppedNotClamped: 101% is a broken read, and clamping
// it to 100 would put an invented number on a gauge (model.Percent's contract).
func TestOutOfRangeContextIsDroppedNotClamped(t *testing.T) {
	s := read(t, fixtureRoot, "00000000-1111-7222-8333-000000000005")

	if s.ContextPercent != nil {
		t.Errorf("context = %v, want nil for an out-of-range reading", *s.ContextPercent)
	}
	if !s.Degraded.Has(model.FieldContextPercent) {
		t.Error("an out-of-range reading did not degrade the field")
	}
	// Everything else on the row survives: one bad number is not a bad row.
	if s.Name == nil || s.Model == nil || s.WorkspaceDir == nil || s.LastActivity == nil {
		t.Error("a bad context reading took the rest of the row with it")
	}
	if err := s.Validate(NewWithRoot(fixtureRoot).Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// TestNothingFromTheUnreadFilesReachesTheSession is the read-allowlist
// assertion, in the shape the Cursor adapter established (decisions/007): a
// marker is planted in the files this adapter promised not to open, and nothing
// the HUD can display may contain it.
//
// The promise is worth a test rather than a comment because the two files are
// the tempting ones: prompt_context.json inlines the user's AGENTS.md verbatim
// and would be the cheapest place to find a project name, and chat_history.jsonl
// is the transcript.
func TestNothingFromTheUnreadFilesReachesTheSession(t *testing.T) {
	s := read(t, fixtureRoot, "00000000-1111-7222-8333-000000000001")

	var displayed []string
	if s.Name != nil {
		displayed = append(displayed, *s.Name)
	}
	if s.WorkspaceDir != nil {
		displayed = append(displayed, *s.WorkspaceDir)
	}
	if id, ok := s.Model.Name(); ok {
		displayed = append(displayed, id)
	}
	displayed = append(displayed, s.Diagnostics...)
	for _, e := range s.Extras {
		displayed = append(displayed, e.Label, e.Value)
	}
	for _, v := range displayed {
		if strings.Contains(v, forbiddenMarker) {
			t.Errorf("content from a file outside the read allowlist reached the session: %q", v)
		}
	}
}

// TestCapabilitiesAreTheSurveysVerdict states the four CapNone fields by name.
// Adding one later should be a deliberate act with a measurement behind it, not
// something a refactor can do quietly.
func TestCapabilitiesAreTheSurveysVerdict(t *testing.T) {
	caps := NewWithRoot(fixtureRoot).Capabilities()
	if err := caps.Validate(); err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	reported := []model.Field{
		model.FieldName, model.FieldModel, model.FieldWorkspace,
		model.FieldContextPercent, model.FieldLastActivity,
	}
	for _, f := range reported {
		if caps.Capability(f) != model.CapReported {
			t.Errorf("%s is %s, want reported", f, caps.Capability(f))
		}
	}
	for _, f := range []model.Field{model.FieldCost, model.FieldQuota, model.FieldLiveness, model.FieldSubagents} {
		if caps.Capability(f) != model.CapNone {
			t.Errorf("%s is %s, want none", f, caps.Capability(f))
		}
	}
	if !caps.Derived.Empty() {
		t.Errorf("derived set is %s; nothing here is computed", caps.Derived)
	}
}

// TestNoLivenessHintIsEverSet: active_sessions.json was measured empty while
// grok.exe was mid-turn, and events.jsonl's last phase outlives the process
// that wrote it. Neither may become a hint, which means the HUD's shared
// age-based classification is the only thing speaking for a grok row.
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

// TestActivityFoldTakesTheFresherSignal pins the §6 Q8 shape on this vendor.
//
// summary.json is rewritten on every turn, so its mtime and its `last_active_at`
// normally agree to within a write. They stop agreeing in the two directions
// this test walks: NTFS defers an mtime update while the writer holds the file
// (so the vendor's own timestamp is fresher on a hot session), and a checkout or
// a copy stamps a new mtime onto old content (so the file is fresher than
// anything inside it). Neither may win by construction; the fresher one wins.
func TestActivityFoldTakesTheFresherSignal(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "C%3A%5Csrc%5Ccode%5Cexample-app", "00000000-1111-7222-8333-000000000001")
	if err := os.MkdirAll(sess, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(fixtureRoot, "C%3A%5Csrc%5Ccode%5Cexample-app",
		"00000000-1111-7222-8333-000000000001", "summary.json")
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(sess, "summary.json")
	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	// last_active_at in the fixture. Everything below is relative to it.
	stamped := time.Date(2026, 8, 9, 19, 14, 28, 231464800, time.UTC)

	// The file is older than its contents claim: the vendor's timestamp wins.
	older := stamped.Add(-30 * time.Minute)
	if err := os.Chtimes(dst, older, older); err != nil {
		t.Fatal(err)
	}
	s := read(t, dir, "00000000-1111-7222-8333-000000000001")
	if s.LastActivity == nil || !s.LastActivity.Equal(stamped) {
		t.Errorf("last_activity = %v, want the vendor's last_active_at %v", s.LastActivity, stamped)
	}

	// The file is newer than its contents claim: the mtime wins. Second
	// resolution, because that is all a filesystem is promised to keep.
	newer := stamped.Add(90 * time.Second).Truncate(time.Second)
	if err := os.Chtimes(dst, newer, newer); err != nil {
		t.Fatal(err)
	}
	s = read(t, dir, "00000000-1111-7222-8333-000000000001")
	if s.LastActivity == nil || !s.LastActivity.Truncate(time.Second).Equal(newer) {
		t.Errorf("last_activity = %v, want the file's mtime %v", s.LastActivity, newer)
	}
}

// TestDecodeWorkspaceRoundTrips pins the encoding the survey measured, and the
// contrast with Claude Code's lossy slug that makes decoding legitimate here.
func TestDecodeWorkspaceRoundTrips(t *testing.T) {
	cases := []struct{ enc, want string }{
		{`C%3A%5CUsers%5Csanle%5Ccode%5Ctelltale`, `C:\Users\sanle\code\telltale`},
		{`C%3A%5CUsers%5Csanle`, `C:\Users\sanle`},
		// A literal '-' passes through unescaped, which is exactly the character
		// Claude Code's slug is ambiguous about.
		{`C%3A%5Csrc%5Cexample-app-1234`, `C:\src\example-app-1234`},
	}
	for _, c := range cases {
		got, ok := decodeWorkspace(c.enc)
		if !ok || got != c.want {
			t.Errorf("decodeWorkspace(%q) = %q/%v, want %q", c.enc, got, ok, c.want)
		}
	}
	for _, bad := range []string{"", "C%3", "C%ZZUsers", "%"} {
		if got, ok := decodeWorkspace(bad); ok {
			t.Errorf("decodeWorkspace(%q) = %q, want a refusal rather than a mangled path", bad, got)
		}
	}
}

// TestVendorAbsentWhenTheStoreIsNotThere: a machine without grok shows no grok
// line at all, rather than an error the user can do nothing about.
func TestVendorAbsentWhenTheStoreIsNotThere(t *testing.T) {
	_, err := NewWithRoot(filepath.Join(t.TempDir(), "no-such-store")).Discover(context.Background())
	if err != model.ErrVendorAbsent {
		t.Errorf("Discover = %v, want ErrVendorAbsent", err)
	}
	if _, err := (&Adapter{}).Discover(context.Background()); err != model.ErrVendorAbsent {
		t.Errorf("rootless Discover = %v, want ErrVendorAbsent", err)
	}
}

// TestReadOfAVanishedSessionIsGone: the directory can be removed between
// Discover and Read, and the HUD drops the row silently.
func TestReadOfAVanishedSessionIsGone(t *testing.T) {
	a := NewWithRoot(fixtureRoot)
	_, err := a.Read(context.Background(), model.SessionRef{
		Vendor:  Vendor,
		ID:      "00000000-1111-7222-8333-00000000ffff",
		Locator: filepath.Join(fixtureRoot, "nowhere", "00000000-1111-7222-8333-00000000ffff"),
	})
	if err != model.ErrSessionGone {
		t.Errorf("Read = %v, want ErrSessionGone", err)
	}
}

// TestFormatUSDKeepsFourPlaces: a grok turn measured $0.0306 on the survey box,
// and two decimal places would render most turns as $0.03 — a rounding that
// turns a measurement into a shrug.
func TestFormatUSDKeepsFourPlaces(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0.0306488, "$0.0306"},
		{0.0747416, "$0.0747"},
		{0, "$0.0000"},
		{1.5, "$1.5000"},
	}
	for _, c := range cases {
		if got := formatUSD(c.in); got != c.want {
			t.Errorf("formatUSD(%v) = %s, want %s", c.in, got, c.want)
		}
	}
}
