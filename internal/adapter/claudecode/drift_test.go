package claudecode

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// driftedID is the transcript in testdata/drifted: well-formed JSON records that
// this adapter's field map no longer describes, because the envelope moved to
// snake_case. Nothing about it fails to parse — which is the point.
const driftedID = "00000000-aaaa-4bbb-8ccc-000000000009"

func driftedAdapter() *Adapter {
	return NewWithRoot(filepath.Join("testdata", "drifted", "projects"))
}

func driftReport(s *model.Session) string {
	for _, d := range s.Diagnostics {
		if strings.Contains(d, "shape drift") {
			return d
		}
	}
	return ""
}

// The corpus this adapter was verified against is silent, including the
// transcript whose only record was torn mid-write — that is the vendor's writer
// being faster than the reader, not a corpus that moved. A mechanism that cried
// wolf on the healthy tree would be ignored the day it fired for real.
func TestVerifiedCorpusReportsNoDrift(t *testing.T) {
	a := testAdapter(t)
	for _, id := range []string{healthyID, tornID} {
		if d := driftReport(readOne(t, a, id)); d != "" {
			t.Errorf("%s: the verified corpus reported drift: %q", id, d)
		}
	}
}

// The failure this whole mechanism exists for: every record parses, the reader
// finds nothing where the field map says to look, and without a canary the row
// renders em dashes that mean "the vendor had nothing to say".
func TestARenamedEnvelopeSaysSo(t *testing.T) {
	a := driftedAdapter()
	s := readOne(t, a, driftedID)

	report := driftReport(s)
	if report == "" {
		t.Fatalf("a corpus with no sessionId anywhere reported no drift: %v", s.Diagnostics)
	}
	if !strings.Contains(report, "sessionId") {
		t.Errorf("the report does not name the canary: %q", report)
	}
	// Both versions travel: the pin the field map was read at, and what the
	// transcript itself says it was written by.
	if !strings.Contains(report, verifiedAgainst) || !strings.Contains(report, "2.9.0") {
		t.Errorf("the report does not carry both versions: %q", report)
	}

	// Nothing invented, nothing thrown away: cwd and the assistant record's
	// model survived the rename and are still values, so only the title — which
	// really did move out of reach — is marked degraded.
	if !s.Has(model.FieldWorkspace) || !s.Has(model.FieldModel) {
		t.Errorf("a field that survived the rename went missing: workspace=%v model=%v",
			s.WorkspaceDir, s.Model)
	}
	if want := model.NewFieldSet(model.FieldName); s.Degraded != want {
		t.Errorf("degraded = %s, want %s", s.Degraded, want)
	}
	if err := s.Validate(a.Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}
