package pi

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// driftedID is the session in testdata/drifted. Its first record PARSES. The
// vendor renamed type=session/id/cwd to type=meta/session_uuid/workdir. That
// is the failure this canary exists for: JSON that unmarshals and is not the
// header this field map describes.
const driftedID = "00000000-aaaa-4bbb-8ccc-000000000009"

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

func TestARenamedSessionHeaderSaysSo(t *testing.T) {
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
		t.Fatalf("a first record with no type=session id reported no drift: %v", s.Diagnostics)
	}
	if !strings.Contains(report, canarySessionHeaderID.Name) || !strings.Contains(report, verifiedAgainst) {
		t.Errorf("report = %q", report)
	}
	if strings.Contains(report, "store reports") {
		t.Errorf("the report invented an observed version: %q", report)
	}

	// Later records did not move. Model and last_activity still read. The
	// header cwd is gone, so workspace and the name fallback degrade.
	if !s.Has(model.FieldModel) {
		t.Error("the model went missing; the model_change record did not move")
	}
	if !s.Has(model.FieldLastActivity) {
		t.Error("last_activity went missing; later timestamps did not move")
	}
	want := model.NewFieldSet(model.FieldName, model.FieldWorkspace)
	if s.Degraded != want {
		t.Errorf("degraded = %s, want %s", s.Degraded, want)
	}
	if err := s.Validate(a.Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}
