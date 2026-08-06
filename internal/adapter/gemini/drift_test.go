package gemini

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// driftedID is the recording in testdata/drifted. Its opening line carries
// session_id / project_hash where the verified writer emits sessionId /
// projectHash, so the line parses, matches none of the four record shapes, and
// is silently skipped — taking the session id the sub-agent nest is keyed by
// with it.
const driftedID = "session-2026-08-02T12-00-00000000"

func driftReport(s *model.Session) string {
	for _, d := range s.Diagnostics {
		if strings.Contains(d, "shape drift") {
			return d
		}
	}
	return ""
}

// The verified corpus is silent.
func TestVerifiedCorpusReportsNoDrift(t *testing.T) {
	a := NewWithRoot(filepath.Join("testdata", "tmp"))
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	for _, ref := range refs {
		s, err := a.Read(context.Background(), ref)
		if err != nil || s == nil {
			continue
		}
		if d := driftReport(s); d != "" {
			t.Errorf("%s: the verified corpus reported drift: %q", ref.ID, d)
		}
	}
}

// The opening metadata line is the only line carrying the full session id, and
// the sub-agent nest is a directory named after it. Rename its keys and the
// count goes to nil with a diagnostic that reads like a fact about this session;
// the canary supplies the fact that is actually true, which is about the store.
func TestARenamedMetadataRecordSaysSo(t *testing.T) {
	a := NewWithRoot(filepath.Join("testdata", "drifted", "tmp"))
	s := readOne(t, a, driftedID)

	report := driftReport(s)
	if report == "" {
		t.Fatalf("a recording with no metadata record reported no drift: %v", s.Diagnostics)
	}
	if !strings.Contains(report, "metadata record") || !strings.Contains(report, verifiedAgainst) {
		t.Errorf("report = %q", report)
	}
	// Nothing on disk states the writer's version, and the report does not
	// pretend otherwise.
	if strings.Contains(report, "store reports") {
		t.Errorf("the report invented an observed version: %q", report)
	}

	// The message records did not move, so the model is still a reading. Only
	// what the metadata line fed is degraded.
	if !s.Has(model.FieldModel) {
		t.Error("the model went missing; the message records did not move")
	}
	want := model.NewFieldSet(model.FieldName, model.FieldSubagents)
	if s.Degraded != want {
		t.Errorf("degraded = %s, want %s", s.Degraded, want)
	}
	if err := s.Validate(a.Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}
