package drift

import (
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

var (
	canaryA = Canary{Name: "record envelope", Feeds: model.NewFieldSet(model.FieldModel, model.FieldWorkspace)}
	canaryB = Canary{Name: "meta record", Feeds: model.NewFieldSet(model.FieldName)}
)

func session() *model.Session {
	return &model.Session{
		Vendor:     model.VendorClaude,
		ID:         "s1",
		ObservedAt: time.Unix(1785700000, 0),
	}
}

// The whole point of the mechanism: a read that found its canaries says
// nothing, so the steady state of a healthy corpus is silence.
func TestASightedCorpusSaysNothing(t *testing.T) {
	w := NewWatch("vendor 1.0", canaryA, canaryB)
	w.Saw(canaryA)
	w.Saw(canaryB)

	s := session()
	w.Fold(s, 12)
	if !s.Degraded.Empty() || len(s.Diagnostics) != 0 {
		t.Fatalf("a matching corpus reported drift: %s %v", s.Degraded, s.Diagnostics)
	}
}

// A read that examined nothing is not evidence of drift. This is the guard that
// keeps the routine race with a vendor's writer — an empty file, a transcript
// whose only record was torn mid-write — from becoming a standing false alarm.
func TestNoUnitsExaminedIsNoEvidence(t *testing.T) {
	w := NewWatch("vendor 1.0", canaryA)

	s := session()
	w.Fold(s, 0)
	if !s.Degraded.Empty() || len(s.Diagnostics) != 0 {
		t.Fatalf("an empty read reported drift: %s %v", s.Degraded, s.Diagnostics)
	}
}

// A missing canary degrades exactly the fields it feeds that the session could
// not source another way, and says so once.
func TestAMissingCanaryDegradesWhatItFeeds(t *testing.T) {
	w := NewWatch("vendor 1.0", canaryA, canaryB)
	w.Saw(canaryB)

	s := session()
	w.Fold(s, 3)

	want := model.NewFieldSet(model.FieldModel, model.FieldWorkspace)
	if s.Degraded != want {
		t.Errorf("degraded = %s, want %s", s.Degraded, want)
	}
	if len(s.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v, want exactly one", s.Diagnostics)
	}
	if !strings.Contains(s.Diagnostics[0], "record envelope") ||
		strings.Contains(s.Diagnostics[0], "meta record") {
		t.Errorf("the report names the wrong canaries: %q", s.Diagnostics[0])
	}
}

// A value that was read is a value. The canary is gone, the report stands, and
// the field that arrived from somewhere else is NOT marked degraded — Validate
// forbids present-and-degraded, and a rendered value with a degraded mark on it
// is the contradiction the schema exists to prevent.
func TestAFieldSourcedAnywayIsNotDegraded(t *testing.T) {
	w := NewWatch("vendor 1.0", canaryA)

	s := session()
	s.WorkspaceDir = model.Ptr(`C:\src\code`)
	w.Fold(s, 3)

	if s.Degraded.Has(model.FieldWorkspace) {
		t.Error("a sourced workspace was marked degraded")
	}
	if !s.Degraded.Has(model.FieldModel) {
		t.Error("the unsourced model was not marked degraded")
	}
	if len(s.Diagnostics) != 1 {
		t.Fatalf("the report did not stand: %v", s.Diagnostics)
	}
	if err := s.Validate(model.Capabilities{
		Reported: model.NewFieldSet(model.FieldModel, model.FieldWorkspace, model.FieldName),
	}); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// The version is context, never the trigger — so a store on a version the
// adapter has never seen, whose shape still matches, reports nothing.
func TestANewerVersionAloneIsNotDrift(t *testing.T) {
	w := NewWatch("vendor 1.0", canaryA)
	w.Saw(canaryA)
	w.Observed("9.9.9")

	s := session()
	w.Fold(s, 5)
	if len(s.Diagnostics) != 0 {
		t.Fatalf("a version bump alone reported drift: %v", s.Diagnostics)
	}
}

// When the shape HAS moved, both versions travel with the report: the pin the
// field map was read from, and whatever the store says it is now.
func TestTheReportCarriesBothVersions(t *testing.T) {
	w := NewWatch("vendor 1.0", canaryA)
	w.Observed("2.4.0")

	s := session()
	w.Fold(s, 5)
	if len(s.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v, want exactly one", s.Diagnostics)
	}
	got := s.Diagnostics[0]
	for _, want := range []string{"shape drift", "record envelope", "vendor 1.0", "2.4.0"} {
		if !strings.Contains(got, want) {
			t.Errorf("report %q does not carry %q", got, want)
		}
	}
}

// A store that names no version of its own still produces a complete report:
// the pin alone is enough to say what the shape was measured against.
func TestAVersionlessStoreStillReports(t *testing.T) {
	w := NewWatch("vendor 1.0", canaryA)

	s := session()
	w.Fold(s, 5)
	if len(s.Diagnostics) != 1 || !strings.Contains(s.Diagnostics[0], "vendor 1.0") {
		t.Fatalf("report = %v", s.Diagnostics)
	}
	if strings.Contains(s.Diagnostics[0], "store reports") {
		t.Errorf("the report invented an observed version: %q", s.Diagnostics[0])
	}
}

// The cost claim, measured rather than asserted: a watch over one read is one
// small allocation plus a string compare per sighting, and it does no I/O at all
// because every canary is a fact the adapter had already parsed. Sized here at
// the shape of a real read — a couple of canaries and a few hundred records —
// so a future canary set that stops being cheap fails visibly.
func BenchmarkWatchOverOneRead(b *testing.B) {
	s := session()
	for b.Loop() {
		w := NewWatch("vendor 1.0", canaryA, canaryB)
		for i := 0; i < 400; i++ {
			w.Saw(canaryA)
			w.Saw(canaryB)
			w.Observed("1.0.1")
		}
		w.Fold(s, 400) // the healthy case: sighted, so Fold writes nothing
	}
}

// The nil watch is legal so an adapter that could not build one (a store it
// never opened) does not need a branch at every call site.
func TestNilWatchIsInert(t *testing.T) {
	var w *Watch
	w.Saw(canaryA)
	w.Observed("1.2.3")
	if len(w.Missing()) != 0 {
		t.Error("a nil watch reported missing canaries")
	}
	s := session()
	w.Fold(s, 10)
	if !s.Degraded.Empty() || len(s.Diagnostics) != 0 {
		t.Fatalf("a nil watch wrote to the session: %s %v", s.Degraded, s.Diagnostics)
	}
}
