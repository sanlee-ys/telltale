package probe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/doctor"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The probe file is pinned to keys and numbers the way the three relay files
// beside it are (quotacache, usagecache, council/room.json), and this file is
// that pin.
//
// It matters more here than on any of the three. Those relay a reading a gauge
// already rendered; this one is written by a mode that DRIVES an agent, so the
// material within reach of the writer is the brief, the reply, the session id
// the vendor named and the directory the seat ran in. Every one of those is
// content, and none of them may reach the disk.

func stamp() time.Time { return time.Date(2026, 9, 4, 12, 34, 56, 0, time.UTC) }

func passingResult() Result {
	return Result{
		Vendor:   model.VendorClaude,
		Label:    "Claude Code",
		Version:  "2.1.226 (Claude Code)",
		ProbedAt: stamp(),
		Checks: []Check{
			{Name: CheckHandshake, Status: doctor.Passed, Took: 1200 * time.Millisecond},
			{Name: CheckTurn, Status: doctor.Passed, Took: 4800 * time.Millisecond},
			{Name: CheckStop, Status: doctor.Passed, Took: 400 * time.Millisecond},
		},
	}
}

func TestTheProbeFileCarriesKeysAndNumbersOnly(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, passingResult().Record("0.3.0")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "claude.json"))
	if err != nil {
		t.Fatal(err)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	for k := range top {
		switch k {
		case "vendor", "version", "probed_at", "telltale_version", "checks":
		default:
			t.Errorf("unexpected top-level probe field %q", k)
		}
	}

	var checks []map[string]json.RawMessage
	if err := json.Unmarshal(top["checks"], &checks); err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		for k := range c {
			switch k {
			case "name", "status", "ms":
			default:
				t.Errorf("unexpected check field %q", k)
			}
		}
	}
}

// The brief, the reply, the session id, the workspace and the failure reason
// are the five things this file may never carry, and this plants a marker for
// each of them where the drive could reach one.
//
// The failure reason is the sharpest, and it is why the record drops Detail on
// every branch: a vendor's own first stderr line routinely carries an absolute
// path or a session id, so a file that quoted it would carry content by the
// back door — on exactly the runs a reader is most likely to paste somewhere.
func TestNoPlantedContentSurvivesIntoTheFile(t *testing.T) {
	const marker = "PLANTED-CONTENT"
	res := passingResult()
	res.Checks[1] = Check{
		Name: CheckTurn, Status: doctor.Failed, Took: time.Second,
		Detail: "the turn failed in C:\\Users\\someone\\secret\\" + marker +
			" for session " + marker,
	}
	res.Label = marker
	res.Skipped = marker

	dir := t.TempDir()
	if err := Write(dir, res.Record("0.3.0")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), marker) {
		t.Fatalf("planted content reached the probe file:\n%s", raw)
	}
	if strings.Contains(string(raw), Brief) {
		t.Fatalf("the brief reached the probe file:\n%s", raw)
	}
}

// A check that did not run writes NO `ms` key, and one that ran writes one even
// at zero. That is design.md §4a.1's rule applied to the one number this file
// carries: "the check took no measurable time" and "the check did not run" are
// different facts, and a reader of the raw file must be able to tell them
// apart.
func TestANotRunCheckWritesNoDurationAndAZeroOneDoes(t *testing.T) {
	res := Result{
		Vendor: model.VendorCodex, ProbedAt: stamp(),
		Checks: []Check{
			{Name: CheckHandshake, Status: doctor.Failed, Took: 0},
			{Name: CheckTurn, Status: doctor.NotChecked},
			{Name: CheckStop, Status: doctor.NotChecked},
		},
	}
	rec := res.Record("0.3.0")

	if rec.Checks[0].Millis == nil {
		t.Error("a check that RAN and took no measurable time wrote no ms key")
	}
	for _, c := range rec.Checks[1:] {
		if c.Millis != nil {
			t.Errorf("%s did not run and still wrote ms = %d", c.Name, *c.Millis)
		}
		if c.Status != StatusNotRun {
			t.Errorf("%s wrote status %q, want %q", c.Name, c.Status, StatusNotRun)
		}
	}
}

func TestWriteThenReadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := passingResult().Record("0.3.0")
	if err := Write(dir, want); err != nil {
		t.Fatal(err)
	}
	got, ok := Read(dir, "claude")
	if !ok {
		t.Fatal("the file just written did not read back")
	}
	if got.Version != want.Version || got.TelltaleVersion != want.TelltaleVersion {
		t.Errorf("read back %+v, want %+v", got, want)
	}
	if !got.ProbedAt.Equal(want.ProbedAt) {
		t.Errorf("probed_at read back as %s, want %s", got.ProbedAt, want.ProbedAt)
	}
	if len(got.Checks) != 3 || got.Checks[0].Name != CheckHandshake {
		t.Errorf("checks read back as %+v", got.Checks)
	}
}

// The stamp is RFC 3339 on disk. Written down because a reader of this
// directory is a person with a text editor, and a Unix integer there would be a
// number nobody can date by eye.
func TestTheStampIsRFC3339(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, passingResult().Record("0.3.0")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	var top struct {
		ProbedAt string `json:"probed_at"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	if _, err := time.Parse(time.RFC3339, top.ProbedAt); err != nil {
		t.Errorf("probed_at = %q, which is not RFC 3339: %v", top.ProbedAt, err)
	}
}

// An absent file, an unreadable one and one naming no seat all report false —
// and the caller renders "never" for all three, which is the honest sentence
// for each. What may never happen is the other collapse, so the boolean is what
// stops a caller writing a pass by accident.
func TestAnAbsentOrBrokenFileIsNeverAResult(t *testing.T) {
	dir := t.TempDir()
	if _, ok := Read(dir, "claude"); ok {
		t.Error("a directory with no file in it reported a probe")
	}
	if err := os.WriteFile(filepath.Join(dir, "codex.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := Read(dir, "codex"); ok {
		t.Error("an unparseable file reported a probe")
	}
	if err := os.WriteFile(filepath.Join(dir, "grok.json"), []byte(`{"version":"1.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := Read(dir, "grok"); ok {
		t.Error("a file naming no seat reported a probe for one")
	}
}

func TestWriteLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, passingResult().Record("0.3.0")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("a temp file survived the write: %s", e.Name())
		}
	}
}
