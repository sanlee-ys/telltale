package codex

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// driftedID is the rollout in testdata/drifted. Its envelopes carry `kind` where
// the verified format carries `type`, so every line parses cleanly and applyLine
// matches nothing at all — no parse failure, no error, no data.
const driftedID = "00000000-bbbb-4ccc-8ddd-000000000009"

func driftReport(s *model.Session) string {
	for _, d := range s.Diagnostics {
		if strings.Contains(d, "shape drift") {
			return d
		}
	}
	return ""
}

func readRollout(t *testing.T, root, id string) *model.Session {
	t.Helper()
	a := NewWithRoot(root)
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	s, err := a.Read(context.Background(), refByID(t, refs, id))
	if err != nil {
		t.Fatalf("Read(%s): %v", id, err)
	}
	return s
}

// The verified corpus is silent — every rollout in it, including the ones that
// exist to exercise torn tails and missing quota.
func TestVerifiedCorpusReportsNoDrift(t *testing.T) {
	a := NewWithRoot("testdata")
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	for _, ref := range refs {
		s, err := a.Read(context.Background(), ref)
		if err != nil || s == nil {
			continue // sub-agent threads and imports are not rows; not this test's business
		}
		if d := driftReport(s); d != "" {
			t.Errorf("%s: the verified corpus reported drift: %q", ref.ID, d)
		}
	}
}

// A renamed envelope discriminator is the worst case in this format: the whole
// field map hangs off one key, so the model, the workspace, the quota windows
// and the context percentage all vanish at once without a single parse failure.
func TestARenamedEnvelopeDiscriminatorSaysSo(t *testing.T) {
	a := NewWithRoot(filepath.Join("testdata", "drifted"))
	s := readRollout(t, filepath.Join("testdata", "drifted"), driftedID)

	report := driftReport(s)
	if report == "" {
		t.Fatalf("a rollout whose envelopes carry no type reported no drift: %v", s.Diagnostics)
	}
	for _, want := range []string{"envelope type", "session_meta record", verifiedAgainst} {
		if !strings.Contains(report, want) {
			t.Errorf("the report does not carry %q: %q", want, report)
		}
	}

	// The cost is stated, not hidden: everything the envelope fed is degraded,
	// and the timestamps — which are on the envelope's own outer keys and did
	// not move — still date the row.
	want := model.NewFieldSet(
		model.FieldModel,
		model.FieldWorkspace,
		model.FieldQuota,
		model.FieldContextPercent,
	)
	if s.Degraded != want {
		t.Errorf("degraded = %s, want %s", s.Degraded, want)
	}
	if s.LastActivity == nil {
		t.Error("last_activity went missing; the envelope's timestamp did not move")
	}
	if err := s.Validate(a.Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}
