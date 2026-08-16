package grok

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// driftedID is the session in testdata/drifted. Its summary.json PARSES — the
// vendor renamed `info.id`/`info.cwd` to `info.session_uuid`/`info.workdir` —
// which is precisely the failure this canary exists for: JSON that unmarshals
// cleanly into a struct whose fields all come back empty, rendering a row of em
// dashes that reads as "grok had nothing to say".
const driftedID = "00000000-1111-7222-8333-000000000001"

func driftReport(s *model.Session) string {
	for _, d := range s.Diagnostics {
		if strings.Contains(d, "shape drift") {
			return d
		}
	}
	return ""
}

func TestVerifiedCorpusReportsNoDrift(t *testing.T) {
	a := NewWithRoot(fixtureRoot)
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

// TestARenamedInfoEnvelopeSaysSo. The torn-summary fixture is the control for
// this one: an unparseable summary.json examines no well-formed unit, so
// drift.Fold reports nothing and the routine race with the vendor's writer does
// not become a standing false alarm. Here the file parsed, so the silence is
// evidence.
func TestARenamedInfoEnvelopeSaysSo(t *testing.T) {
	a := NewWithRoot(filepath.Join("testdata", "drifted", "sessions"))
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != driftedID {
		t.Fatalf("Discover = %v, want the one drifted session", refs)
	}
	s, err := a.Read(context.Background(), refs[0])
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	report := driftReport(s)
	if report == "" {
		t.Fatalf("a summary.json with no info.id reported no drift: %v", s.Diagnostics)
	}
	if !strings.Contains(report, canarySummaryInfoID.Name) || !strings.Contains(report, VerifiedAgainst) {
		t.Errorf("report = %q", report)
	}
	// Nothing inside a session directory states the writer's build, and the
	// report does not pretend otherwise.
	if strings.Contains(report, "store reports") {
		t.Errorf("the report invented an observed version: %q", report)
	}

	// The workspace survived — the directory name still decodes — and a field
	// that was sourced anyway is NOT degraded, which is drift.Fold's contract.
	if !s.Has(model.FieldWorkspace) {
		t.Error("the workspace went missing; the directory name did not move")
	}
	if s.Degraded.Has(model.FieldWorkspace) {
		t.Error("workspace is both present and degraded")
	}
	want := model.NewFieldSet(model.FieldName, model.FieldModel)
	if s.Degraded != want {
		t.Errorf("degraded = %s, want %s", s.Degraded, want)
	}
	if err := s.Validate(a.Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}
