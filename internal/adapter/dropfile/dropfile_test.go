package dropfile

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// Fixtures are synthesized AT RUN TIME rather than checked in, and that is not
// a shortcut. Half of what this adapter decides is a function of the file's
// mtime, and a checked-in fixture carries whatever mtime the clone gave it —
// so an expiry test over vendored testdata would assert nothing on one machine
// and fail on another. Writing the file and stamping it is the only way to
// pin the rule.
//
// No drop file here describes a real session. Every tool name, workspace and
// id is invented; this repo is public.

// write puts one document in dir and stamps its mtime. A nil body writes the
// bytes verbatim so a test can hand over malformed JSON.
func write(t *testing.T, dir, name string, doc map[string]any, raw string, age time.Duration) string {
	t.Helper()
	path := filepath.Join(dir, name)
	var b []byte
	if doc != nil {
		var err error
		b, err = json.Marshal(doc)
		if err != nil {
			t.Fatalf("marshal fixture: %v", err)
		}
	} else {
		b = []byte(raw)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	at := time.Now().Add(-age)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatalf("stamp fixture: %v", err)
	}
	return path
}

// good is a complete document, every field populated and in range.
func good() map[string]any {
	return map[string]any{
		"schema_version": 1,
		"tool":           "windsurf",
		"name":           "refactor the parser",
		"workspace":      `C:\src\code\example-app`,
		"model":          "gpt-5-codex",
		"context_pct":    42.5,
		"cost_usd":       1.25,
		"last_activity":  time.Now().Add(-30 * time.Second).UTC().Format(time.RFC3339),
		"subagents":      2,
	}
}

func readOne(t *testing.T, dir, id string) *model.Session {
	t.Helper()
	a := NewWithRoot(dir)
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	for _, ref := range refs {
		if ref.ID != id {
			continue
		}
		s, err := a.Read(context.Background(), ref)
		if err != nil {
			t.Fatalf("Read(%s): %v", id, err)
		}
		return s
	}
	t.Fatalf("Discover did not return %q (got %d refs)", id, len(refs))
	return nil
}

// TestAReadRowSatisfiesTheHonestGaugeGate runs the machine-checkable form of
// ADR-001 over what this adapter actually produces (§4a.6).
func TestAReadRowSatisfiesTheHonestGaugeGate(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.json", good(), "", time.Minute)

	a := NewWithRoot(dir)
	if err := a.Capabilities().Validate(); err != nil {
		t.Fatalf("capabilities are not disjoint: %v", err)
	}
	s := readOne(t, dir, "app")
	if err := s.Validate(a.Capabilities()); err != nil {
		t.Fatalf("session fails Validate: %v", err)
	}
	if s.Vendor != model.VendorSelfReported {
		t.Errorf("vendor = %q, want %q", s.Vendor, model.VendorSelfReported)
	}
	if s.ContextPercent == nil || float64(*s.ContextPercent) != 42.5 {
		t.Errorf("context_pct did not survive the read: %v", s.ContextPercent)
	}
	if s.Cost == nil || float64(*s.Cost) != 1.25 {
		t.Errorf("cost_usd did not survive the read: %v", s.Cost)
	}
	if s.Subagents == nil || *s.Subagents != 2 {
		t.Errorf("subagents did not survive the read: %v", s.Subagents)
	}
}

// TestNothingIsMarkedDerived is the honesty crux from the adapter's side.
//
// This adapter reads verbatim and computes nothing, so no field may carry the
// estimate marker. A row that arrived here marked derived would tell the HUD
// and the snapshot that telltale INFERRED a number a stranger wrote down —
// a different claim, and a false one.
func TestNothingIsMarkedDerived(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.json", good(), "", time.Minute)

	if got := readOne(t, dir, "app").Derived; !got.Empty() {
		t.Errorf("Derived = %s, want none — see the package doc on why ~ is the wrong mark", got)
	}
	if got := NewWithRoot(dir).Capabilities().Derived; !got.Empty() {
		t.Errorf("Capabilities.Derived = %s, want none", got)
	}
}

// TestTheVendorIdIsNotTheWritersToChoose is the anti-impersonation property,
// asserted the way it is enforced: not by rejecting a bad value, but by there
// being nowhere to put one.
func TestTheVendorIdIsNotTheWritersToChoose(t *testing.T) {
	dir := t.TempDir()
	doc := good()
	// Every spelling a writer might reach for to claim it is a measured vendor.
	doc["vendor"] = "claude"
	doc["vendor_id"] = "claude"
	doc["id"] = "claude"
	doc["self_reported"] = false
	write(t, dir, "app.json", doc, "", time.Minute)

	s := readOne(t, dir, "app")
	if s.Vendor != model.VendorSelfReported {
		t.Fatalf("a drop file changed its own vendor id to %q — impersonation is possible", s.Vendor)
	}
	if s.ID != "app" {
		t.Errorf("session id = %q, want the file stem %q: the filesystem names the row, not the document", s.ID, "app")
	}
}

// TestZeroIsAMeasurementAndAbsentIsNil is §4a.1 at this format's edge, and it
// covers BOTH spellings of absence the format accepts.
func TestZeroIsAMeasurementAndAbsentIsNil(t *testing.T) {
	dir := t.TempDir()

	zero := good()
	zero["context_pct"] = 0
	zero["cost_usd"] = 0
	zero["subagents"] = 0
	write(t, dir, "zero.json", zero, "", time.Minute)

	// Spelling one: the keys are present and explicitly null.
	null := good()
	null["context_pct"] = nil
	null["cost_usd"] = nil
	null["subagents"] = nil
	write(t, dir, "null.json", null, "", time.Minute)

	// Spelling two: the keys are simply not there.
	omitted := good()
	delete(omitted, "context_pct")
	delete(omitted, "cost_usd")
	delete(omitted, "subagents")
	write(t, dir, "omitted.json", omitted, "", time.Minute)

	z := readOne(t, dir, "zero")
	if z.ContextPercent == nil || float64(*z.ContextPercent) != 0 {
		t.Error("a claimed 0 context_pct did not survive as a measured zero")
	}
	if z.Cost == nil || float64(*z.Cost) != 0 {
		t.Error("a claimed 0 cost_usd did not survive as a measured zero")
	}
	if z.Subagents == nil || *z.Subagents != 0 {
		t.Error("a claimed 0 subagents did not survive as a measured zero")
	}

	for _, id := range []string{"null", "omitted"} {
		s := readOne(t, dir, id)
		if s.ContextPercent != nil {
			t.Errorf("%s: context_pct = %v, want nil", id, *s.ContextPercent)
		}
		if s.Cost != nil {
			t.Errorf("%s: cost_usd = %v, want nil", id, *s.Cost)
		}
		if s.Subagents != nil {
			t.Errorf("%s: subagents = %v, want nil", id, *s.Subagents)
		}
		// Absence is not degradation: nothing was tried and failed here.
		if !s.Degraded.Empty() {
			t.Errorf("%s: Degraded = %s, want none — an omitted field is absent, not broken", id, s.Degraded)
		}
	}
}

// TestABadFieldDegradesAndTheRowSurvives is §4a.5's partial-read rule: one
// unusable value costs its own cell and nothing else.
func TestABadFieldDegradesAndTheRowSurvives(t *testing.T) {
	dir := t.TempDir()
	doc := good()
	doc["context_pct"] = "forty two" // wrong type
	doc["cost_usd"] = -3.0           // impossible value
	doc["subagents"] = 1.5           // not a whole number
	doc["model"] = []any{"a", "b"}   // wrong type
	write(t, dir, "app.json", doc, "", time.Minute)

	s := readOne(t, dir, "app")

	// The row is still here, and the fields that were fine are still fine.
	if s.Name == nil || !strings.Contains(*s.Name, "refactor the parser") {
		t.Errorf("a good field did not survive a neighbour's bad value: name = %v", s.Name)
	}
	if s.WorkspaceDir == nil {
		t.Error("workspace did not survive a neighbour's bad value")
	}

	for _, f := range []model.Field{
		model.FieldContextPercent, model.FieldCost,
		model.FieldSubagents, model.FieldModel,
	} {
		if !s.Degraded.Has(f) {
			t.Errorf("%s was not marked degraded", f)
		}
		if s.Has(f) {
			t.Errorf("%s is both present and degraded, which Validate forbids", f)
		}
	}
	if err := s.Validate(NewWithRoot(dir).Capabilities()); err != nil {
		t.Fatalf("a degraded row must still validate: %v", err)
	}
	if len(s.Diagnostics) == 0 {
		t.Error("degradation was silent; §4a.2 requires an operator-facing note")
	}
}

// TestAnOutOfRangePercentIsDroppedNotClamped. A clamped value is invented
// data (model.Percent's doc), and it is the failure mode a lenient reader
// walks into first.
func TestAnOutOfRangePercentIsDroppedNotClamped(t *testing.T) {
	dir := t.TempDir()
	doc := good()
	doc["context_pct"] = 140.0
	write(t, dir, "app.json", doc, "", time.Minute)

	s := readOne(t, dir, "app")
	if s.ContextPercent != nil {
		t.Errorf("context_pct = %v, want nil — 140 must be dropped, never clamped to 100",
			float64(*s.ContextPercent))
	}
	if !s.Degraded.Has(model.FieldContextPercent) {
		t.Error("the dropped percentage was not marked degraded")
	}
}

// TestAStaleDropFileDrawsNoRow. A writer that stopped writing must stop
// speaking; the honest display for "no reading" is absence, exactly as
// internal/quotacache decided for a relayed reading.
func TestAStaleDropFileDrawsNoRow(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "fresh.json", good(), "", time.Minute)
	write(t, dir, "expired.json", good(), "", maxAge+time.Hour)

	refs, err := NewWithRoot(dir).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	var ids []string
	for _, r := range refs {
		ids = append(ids, r.ID)
	}
	if len(ids) != 1 || ids[0] != "fresh" {
		t.Errorf("Discover returned %v, want only [fresh] — a day-old claim is archaeology", ids)
	}
}

// TestAFutureStampedDropFileDrawsNoRow: a clock we cannot reason about is not
// a reading we can age.
func TestAFutureStampedDropFileDrawsNoRow(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "ahead.json", good(), "", -(futureSkew + time.Hour))

	refs, err := NewWithRoot(dir).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("Discover returned %d refs, want none", len(refs))
	}
}

// TestLastActivityCannotOutrunTheFilesOwnMtime is the one claim in this format
// telltale can check, and the check is the reason a dead writer's row goes
// quiet instead of reading live forever.
func TestLastActivityCannotOutrunTheFilesOwnMtime(t *testing.T) {
	dir := t.TempDir()
	doc := good()
	// The file was last written two hours ago and claims it was busy a second
	// ago. Only one of those two facts is telltale's own measurement.
	doc["last_activity"] = time.Now().Add(-time.Second).UTC().Format(time.RFC3339)
	write(t, dir, "app.json", doc, "", 2*time.Hour)

	s := readOne(t, dir, "app")
	if s.LastActivity == nil {
		t.Fatal("last_activity is absent; the mtime was available and is a real reading")
	}
	if age := time.Since(*s.LastActivity); age < time.Hour {
		t.Errorf("last_activity resolved to %s ago; the claim outran the mtime and was believed", age)
	}
	if s.Liveness(time.Now(), model.DefaultLivenessThresholds) != model.LivenessStale {
		t.Error("a two-hour-old file rendered as something other than stale")
	}
	var noted bool
	for _, d := range s.Diagnostics {
		if strings.Contains(d, "mtime") {
			noted = true
		}
	}
	if !noted {
		t.Error("the substitution was silent; the operator has no way to see the claim was overridden")
	}
}

// TestADocumentWithNoContractIsNotRead. A schema_version telltale does not
// speak means the field names may not mean what this adapter thinks. That is
// not a partial read, and reading it anyway would invent every value at once.
func TestADocumentWithNoContractIsNotRead(t *testing.T) {
	dir := t.TempDir()
	for name, doc := range map[string]map[string]any{
		"future":  {"schema_version": FormatVersion + 1, "tool": "windsurf"},
		"missing": {"tool": "windsurf"},
		"notool":  {"schema_version": FormatVersion},
		"empty":   {"schema_version": FormatVersion, "tool": ""},
	} {
		write(t, dir, name+".json", doc, "", time.Minute)
	}
	write(t, dir, "torn.json", nil, `{"schema_version":1,"tool":`, time.Minute)

	a := NewWithRoot(dir)
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(refs) != 5 {
		t.Fatalf("Discover found %d files, want 5 — the rejection must happen in Read", len(refs))
	}
	for _, ref := range refs {
		if s, err := a.Read(context.Background(), ref); err == nil {
			t.Errorf("%s produced a row (%v); it carries no contract this adapter speaks", ref.ID, s.Vendor)
		}
	}
}

// TestNoUnallowlistedStringReachesAnythingDisplayable is the planted-credential
// test the other adapters carry, in the shape internal/cursorhook set: the
// allowlist IS the struct, so a key with no destination is dropped by
// encoding/json before this package sees it.
//
// The markers are planted where a careless reader would pick them up — at the
// top level, nested, and under names that look like the fields this format
// really has.
func TestNoUnallowlistedStringReachesAnythingDisplayable(t *testing.T) {
	const marker = "PLANTEDSECRET"
	dir := t.TempDir()
	doc := good()
	doc["api_key"] = "sk-ant-" + marker
	doc["token"] = marker
	doc["authorization"] = "Bearer " + marker
	doc["password"] = marker
	doc["transcript"] = "the model said " + marker
	doc["prompt"] = marker
	doc["env"] = map[string]any{"ANTHROPIC_API_KEY": marker}
	doc["quota"] = []any{map[string]any{"id": marker, "label": marker}}
	doc["extras"] = map[string]any{"note": marker}
	doc["diagnostics"] = []any{marker}
	write(t, dir, "app.json", doc, "", time.Minute)

	s := readOne(t, dir, "app")

	// Every string a renderer can reach, gathered from the session itself so a
	// field added later cannot quietly escape this sweep.
	var reachable []string
	if s.Name != nil {
		reachable = append(reachable, *s.Name)
	}
	if s.WorkspaceDir != nil {
		reachable = append(reachable, *s.WorkspaceDir)
	}
	if n, ok := s.Model.Name(); ok {
		reachable = append(reachable, n)
	}
	reachable = append(reachable, s.ID, string(s.Vendor))
	reachable = append(reachable, s.Diagnostics...)
	for _, e := range s.Extras {
		reachable = append(reachable, e.Label, e.Value)
	}
	for _, w := range s.Quota {
		reachable = append(reachable, w.ID, w.Label)
	}

	for _, got := range reachable {
		if strings.Contains(got, marker) {
			t.Errorf("a planted secret reached a displayable string: %q", got)
		}
	}
	// And the format grew no quota door while nobody was looking.
	if len(s.Quota) != 0 {
		t.Errorf("the document asserted %d quota windows; this format has no field for one", len(s.Quota))
	}
}

// TestTheAdapterDeclaresItselfSelfReporting. hud.Scan reads this through an
// optional interface, so a silent rename here would drop the marking from
// every surface without failing a compile.
func TestTheAdapterDeclaresItselfSelfReporting(t *testing.T) {
	var a any = New()
	sr, ok := a.(interface{ SelfReported() bool })
	if !ok {
		t.Fatal("the adapter no longer satisfies hud.SelfReporting; every row would render as measured")
	}
	if !sr.SelfReported() {
		t.Error("SelfReported() = false")
	}
}

// TestAMissingDirectoryIsAnAbsentVendor, not an error banner: nobody has
// written a drop file, which is the ordinary case.
func TestAMissingDirectoryIsAnAbsentVendor(t *testing.T) {
	a := NewWithRoot(filepath.Join(t.TempDir(), "nothing-here"))
	if _, err := a.Discover(context.Background()); err == nil {
		t.Fatal("Discover returned no error for a missing directory")
	} else if err != model.ErrVendorAbsent {
		t.Errorf("Discover error = %v, want ErrVendorAbsent", err)
	}
}

// TestOnlyJSONFilesAreDropFiles. The directory is the operator's, and a README
// or a half-renamed temp file in it is not a row.
func TestOnlyJSONFilesAreDropFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.json", good(), "", time.Minute)
	write(t, dir, "notes.md", nil, "not a drop file", time.Minute)
	write(t, dir, "app-123.tmp", nil, "{}", time.Minute)
	write(t, dir, ".hidden.json", good(), "", time.Minute)
	if err := os.Mkdir(filepath.Join(dir, "sub.json"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	refs, err := NewWithRoot(dir).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != "app" {
		t.Errorf("Discover returned %d refs, want only [app]", len(refs))
	}
}

// TestTheClaimedToolLeadsTheLabel. "SR" is shared by every drop file, so the
// row itself has to say who claimed it.
func TestTheClaimedToolLeadsTheLabel(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "named.json", good(), "", time.Minute)

	bare := good()
	delete(bare, "name")
	write(t, dir, "bare.json", bare, "", time.Minute)

	if s := readOne(t, dir, "named"); s.Name == nil || !strings.HasPrefix(*s.Name, "windsurf") {
		t.Errorf("label = %v, want it to lead with the claiming tool", s.Name)
	}
	if s := readOne(t, dir, "bare"); s.Name == nil || *s.Name != "windsurf" {
		t.Errorf("label = %v, want the tool alone when the file names no session", s.Name)
	}
}
