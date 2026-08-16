package cursor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

func driftReport(s *model.Session) string {
	for _, d := range s.Diagnostics {
		if strings.Contains(d, "shape drift") {
			return d
		}
	}
	return ""
}

// The verified store is silent, on every session it holds.
func TestVerifiedStoreReportsNoDrift(t *testing.T) {
	for _, id := range []string{idHappy, idDerived, idNoWorkspace, idNoData, idSkew, idNoClock} {
		if d := driftReport(mustRead(t, id)); d != "" {
			t.Errorf("%s: the verified store reported drift: %q", id, d)
		}
	}
}

// driftedRoot copies the verified store and renames columns inside it, byte for
// byte.
//
// Built here rather than checked in for the reason the agy fixture gives: what
// makes this a drift fixture is that it is the verified store with named
// identifiers moved and nothing else. The renames are length-preserving, so no
// page, cell or offset shifts and the file stays valid SQLite — which is what a
// vendor's column rename actually looks like on disk.
func driftedRoot(t *testing.T, renames map[string]string) string {
	t.Helper()
	dst := t.TempDir()

	raw, err := os.ReadFile(filepath.Join(root(), globalStorage, storeFile))
	if err != nil {
		t.Fatal(err)
	}
	for from, to := range renames {
		if len(from) != len(to) {
			t.Fatalf("rename %q -> %q changes length; the patch would corrupt the store", from, to)
		}
		if !bytes.Contains(raw, []byte(from)) {
			t.Fatalf("fixture drifted: %q is not in the state store", from)
		}
		raw = bytes.ReplaceAll(raw, []byte(from), []byte(to))
	}
	if err := os.MkdirAll(filepath.Join(dst, globalStorage), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, globalStorage, storeFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

// This vendor's silent failure is narrower than the others' and worse for it.
// One store backs every session, so its file mtime is deliberately not folded
// into last_activity: the header row's own timestamp columns are the ONLY clock
// a Cursor row has. Rename all three and every row degrades its age with a
// per-row diagnostic — "no readable activity timestamp on this session's header
// row" — which is true of the row and says nothing about the store, so the HUD
// shows a corpus-wide format change as a coincidence.
func TestARenamedRowClockSaysSo(t *testing.T) {
	a := NewWithRoot(driftedRoot(t, map[string]string{
		colLastUpdatedAt: "lastRefreshed", // 13
		colRecency:       "sortKey",       // 7
		colCheckpointAt:  "checkpointOn",  // 12
	}))
	s, err := readOne(t, a, idHappy)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	report := driftReport(s)
	if report == "" {
		t.Fatalf("a store with no header timestamps reported no drift: %v", s.Diagnostics)
	}
	if !strings.Contains(report, tableHeaders) || !strings.Contains(report, VerifiedAgainst) {
		t.Errorf("report = %q", report)
	}
	if !s.Degraded.Has(model.FieldLastActivity) {
		t.Errorf("degraded = %s, want last_activity", s.Degraded)
	}

	// The rows are still sessions: the columns that identify and name them did
	// not move, so the store reports less rather than reporting nothing.
	if !s.Has(model.FieldName) {
		t.Error("the session lost its name; only the clock columns moved")
	}
	if err := s.Validate(a.Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// The fatal tier is unchanged and still wins: a store with no composerHeaders
// table at all is reported from Discover, on the vendor line, rather than as a
// per-session diagnostic nobody would see because there would be no sessions.
func TestAnUnreadableShapeStaysFatal(t *testing.T) {
	a := NewWithRoot(driftedRoot(t, map[string]string{tableHeaders: "composerHeaderX"}))
	if _, err := a.Discover(t.Context()); err == nil {
		t.Fatal("Discover accepted a store with no composerHeaders table")
	}
}
