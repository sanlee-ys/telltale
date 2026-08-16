package antigravity

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

// The verified corpus is silent — including the conversation whose generation
// fails its token self-check, which is a bad READING and not a moved shape.
func TestVerifiedCorpusReportsNoDrift(t *testing.T) {
	for _, id := range []string{idHappy, idWAL, idBroken, idNoWorkspace, idZero, idMultiChunk} {
		if d := driftReport(mustRead(t, id)); d != "" {
			t.Errorf("%s: the verified corpus reported drift: %q", id, d)
		}
	}
}

// driftedRoot copies one conversation out of the verified corpus and renames a
// table inside it, byte for byte.
//
// The fixture is built here rather than checked in because what makes it a
// DRIFT fixture is its relationship to the verified one: the same database with
// exactly one identifier moved. A second binary in testdata/ would state that
// relationship in a comment and then be free to stop being true. The rename is
// length-preserving, so no page, cell or offset in the file shifts — the
// database stays valid SQLite and only its schema reads differently, which is
// exactly what a vendor's table rename looks like on disk.
func driftedRoot(t *testing.T, id, from, to string) string {
	t.Helper()
	if len(from) != len(to) {
		t.Fatalf("rename %q -> %q changes length; the patch would corrupt the file", from, to)
	}
	dst := t.TempDir()

	db, err := os.ReadFile(filepath.Join(root(), "conversations", id+".db"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(db, []byte(from)) {
		t.Fatalf("fixture drifted: %q is not in the conversation database", from)
	}
	if err := os.MkdirAll(filepath.Join(dst, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	patched := bytes.ReplaceAll(db, []byte(from), []byte(to))
	if err := os.WriteFile(filepath.Join(dst, "conversations", id+".db"), patched, 0o644); err != nil {
		t.Fatal(err)
	}

	// The transcript is the session; without it Read reports ErrNoTranscript and
	// there is no row to carry a diagnostic.
	logs := filepath.Join("brain", id, ".system_generated", "logs")
	src, err := os.ReadFile(filepath.Join(root(), logs, "transcript.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dst, logs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, logs, "transcript.jsonl"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

// A renamed table is the SQLite form of the same failure the JSONL adapters
// face: the database opens, every row that is there reads correctly, and the
// one the model comes from is simply not there any more.
func TestARenamedTableSaysSo(t *testing.T) {
	a := NewWithRoot(driftedRoot(t, idHappy, tableGenMetadata, "gen_metadatx"))
	s, err := readOne(t, a, idHappy)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	report := driftReport(s)
	if report == "" {
		t.Fatalf("a database with no %s table reported no drift: %v", tableGenMetadata, s.Diagnostics)
	}
	if !strings.Contains(report, tableGenMetadata) || !strings.Contains(report, VerifiedAgainst) {
		t.Errorf("report = %q", report)
	}
	if strings.Contains(report, tableTrajectory) {
		t.Errorf("the report blames a table that is still there: %q", report)
	}

	// The workspace comes from the other table and is untouched, so the cost is
	// exactly the model — stated, not spread.
	if !s.Has(model.FieldWorkspace) {
		t.Error("the workspace went missing; its table did not move")
	}
	if want := model.NewFieldSet(model.FieldModel); s.Degraded != want {
		t.Errorf("degraded = %s, want %s", s.Degraded, want)
	}
	if err := s.Validate(a.Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}
