package antigravity

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The multi-chunk conversation, and what it is and is not evidence of.
//
// design.md §3.8's 1.1.13 re-read found a `logs/chunks/` tree on the 4 newest
// conversations and recorded the hole it left: every one of them held exactly
// ONE chunk, so "whether the flat file stays complete once a second chunk
// exists" was unobserved, and it is the standing watch item that block left
// behind. It is still unobserved. No live multi-chunk conversation has been
// captured on any machine.
//
// So this fixture is SYNTHESIZED, and the tests below pin the adapter's
// contract with ITSELF rather than a vendor claim. They answer "what does
// telltale do when a second chunk is on disk", under each behavior the vendor
// might turn out to have, and they answer nothing at all about which of those
// behaviors agy actually has. A live capture remains the missing instrument —
// see the 2026-08-16 amendment in §3.8.
//
// What the fixture is worth is concrete rather than hypothetical: it is the
// first transcript in this package larger than 1 KiB. A transcript big enough
// to be chunked is the first one that splits the adapter's head+tail read
// budget at all, so until this fixture existed the head path, the unread middle
// and the overlap guard that narrows the head window were carried by no test on
// this vendor — every other fixture transcript is swallowed whole by the tail
// read and never reaches jsonl.Head.
const idMultiChunk = "00000000-dddd-4eee-8fff-000000000007"

// The fixture's own numbers, from testdata/gen_fixtures.py. 700 records at one
// second apart from 09:00:00Z: chunk 0 is step 0-299, chunk 1 is step 300-699.
var (
	multiChunkNewest      = time.Date(2026, 8, 1, 9, 11, 39, 0, time.UTC)
	multiChunkChunk0Last  = time.Date(2026, 8, 1, 9, 4, 59, 0, time.UTC)
	multiChunkChunkPoison = time.Date(2026, 8, 1, 23, 59, 0, 0, time.UTC)
)

func multiChunkLogs(root string) string {
	return filepath.Join(root, "brain", idMultiChunk, ".system_generated", "logs")
}

// backdate pushes a path's mtime out of the way so the transcript's own step
// timestamps decide last_activity. The fixture files carry their checkout time,
// which is always fresher than a 2026-08-01 step (TestLastActivityTakesTheFresherSignal
// makes the same move for the same reason).
func backdate(t *testing.T, paths ...string) {
	t.Helper()
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range paths {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
}

// ------------------------------------------------------- the fixture's shape

// The fixture is only worth anything if it is actually bigger than the read
// budget; a regeneration that shrank it would leave every test below passing
// over a file the tail read swallows whole. This asserts the property by name
// so that failure is legible instead of silent.
func TestMultiChunkTranscriptExceedsTheReadBudget(t *testing.T) {
	info, err := os.Stat(filepath.Join(multiChunkLogs(root()), "transcript.jsonl"))
	if err != nil {
		t.Fatalf("%v (regenerate: cd testdata && uv run python gen_fixtures.py)", err)
	}
	budget := headBytes + tailBytes
	if info.Size() <= budget {
		t.Fatalf("flat transcript is %d bytes, want more than the %d-byte head+tail budget: "+
			"below it the tail read covers the whole file and the head path never runs",
			info.Size(), budget)
	}
}

// The one relationship the single-chunk corpus measured — the flat file is
// byte-identical to its chunk — carried forward to two chunks. The fixture
// models it rather than inventing a different concatenation, so a test that
// reasons about "what the chunks hold" is reasoning about the flat file too.
func TestMultiChunkFlatFileIsItsChunksConcatenated(t *testing.T) {
	logs := multiChunkLogs(root())
	flat, err := os.ReadFile(filepath.Join(logs, "transcript.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var joined []byte
	for _, name := range []string{"00000000.jsonl", "00000001.jsonl"} {
		part, err := os.ReadFile(filepath.Join(logs, "chunks", "transcript", name))
		if err != nil {
			t.Fatalf("%v (regenerate: cd testdata && uv run python gen_fixtures.py)", err)
		}
		joined = append(joined, part...)
	}
	if !bytes.Equal(flat, joined) {
		t.Errorf("the flat transcript is not its two chunks concatenated (%d bytes vs %d); "+
			"the fixture no longer models the md5 identity §3.8 measured", len(flat), len(joined))
	}
}

// ------------------------------------------------------ what the adapter reads

// The decision §3.8 recorded — "read the flat file until a multi-chunk
// conversation is measured; do not switch to the chunk tree on the strength of
// its name" — now has a test rather than a comment.
//
// The canary is the `transcript_full` chunk tree, which carries a step dated
// 23:59 where the flat file's newest is 09:11:39. A reader that followed the
// chunk tree would move last_activity by fourteen hours and this test would say
// so. Note what it deliberately cannot catch: `chunks/transcript/` is
// byte-identical to the flat file, so switching to THAT is invisible here — as
// it is on disk, which is exactly why the switch must not be made on the
// strength of a directory name.
func TestMultiChunkReadsTheFlatFileNotTheChunkTree(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, root(), dir)
	// EVERY file under logs/, not just the flat one. If only the flat file were
	// backdated, an adapter that switched to the chunk tree would be caught by
	// the chunk file's fresh mtime instead of by its poison timestamp — the test
	// would still fail, but for a reason that says nothing about which file was
	// read. Backdating the whole tree leaves the step timestamps as the only
	// signal, so the failure names the culprit.
	backdate(t, filepath.Join(dir, "conversations", idMultiChunk+".db"))
	logs := multiChunkLogs(dir)
	err := filepath.Walk(logs, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		backdate(t, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	s, err := readOne(t, NewWithRoot(dir), idMultiChunk)
	if err != nil {
		t.Fatal(err)
	}
	if s.LastActivity == nil {
		t.Fatal("last_activity absent; the flat transcript is full of readable timestamps")
	}
	if s.LastActivity.Equal(multiChunkChunkPoison) {
		t.Fatal("last_activity came from the chunk tree; the adapter must read the flat file")
	}
	if !s.LastActivity.Equal(multiChunkNewest) {
		t.Errorf("last_activity = %v, want the flat file's newest step %v",
			s.LastActivity, multiChunkNewest)
	}
}

// A read budget skips the middle of a large file. That is ABSENCE — bytes this
// adapter chose not to look at — and it must never be reported as corruption,
// which is the same distinction CLAUDE.md draws between "we could not read this
// field" and "there is nothing there". A diagnostic claiming 120 unparseable
// records on a perfectly good transcript would be a false accusation against
// the vendor, and it is what an overlapping or mis-framed window produces.
func TestMultiChunkUnreadMiddleIsNotReportedAsCorruption(t *testing.T) {
	s := mustRead(t, idMultiChunk)
	if hasDiagnostic(s, "unparseable") {
		t.Errorf("the skipped middle of the read budget was reported as damage: %v", s.Diagnostics)
	}
	if !s.Degraded.Empty() {
		t.Errorf("degraded = %s on a clean multi-chunk read (%v)", s.Degraded, s.Diagnostics)
	}
	if err := s.Validate(newAdapter().Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// Between the tail budget and the full budget the two windows would overlap,
// and readTranscript narrows the head window to close the gap. The fixture sits
// ABOVE that band, so this truncates a copy into it — the only size at which
// the narrowing branch executes at all, and one no fixture reaches on its own.
//
// The assertion is built to fail if the guard is removed rather than to pass
// quietly beside it. One record is damaged, positioned in the bytes the two
// windows would BOTH cover, and the count must come back 1. Duplication is
// invisible to every other signal here — the newest-timestamp fold is a maximum
// and survives reading a record twice — but the unparseable counter is a sum,
// so it is the one place a doubled read shows up, and it shows up as the
// adapter accusing the vendor of twice the damage that is there.
func TestMultiChunkHeadWindowNarrowsWhereTheWindowsWouldOverlap(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, root(), dir)

	flat := filepath.Join(multiChunkLogs(dir), "transcript.jsonl")
	raw, err := os.ReadFile(flat)
	if err != nil {
		t.Fatal(err)
	}
	recs, _ := splitLines(raw)

	// Keep whole records up to a size inside (tailBytes, headBytes+tailBytes).
	const target = tailBytes + (headBytes / 2)
	var kept [][]byte
	var size int64
	for _, r := range recs {
		if size+int64(len(r)) > target {
			break
		}
		kept = append(kept, r)
		size += int64(len(r))
	}
	if size <= tailBytes || size >= headBytes+tailBytes {
		t.Fatalf("truncated to %d bytes, which is not inside the overlap band (%d, %d)",
			size, tailBytes, headBytes+tailBytes)
	}

	// The contested bytes: at this size an unguarded head would read [0,
	// headBytes) and the tail [size-tailBytes, size), so a record starting
	// inside [size-tailBytes, headBytes) falls in both. The damage is
	// length-preserving so every later offset — and therefore the band itself —
	// stays where it was measured.
	lo, hi := size-tailBytes, headBytes
	damaged := -1
	var off int64
	for i, r := range kept {
		if off >= lo && off+int64(len(r)) <= hi {
			damaged = i
			break
		}
		off += int64(len(r))
	}
	if damaged < 0 {
		t.Fatalf("no whole record lies inside the contested band [%d, %d)", lo, hi)
	}
	torn := bytes.Repeat([]byte("x"), len(kept[damaged])-1)
	kept[damaged] = append(torn, '\n')

	if err := os.WriteFile(flat, bytes.Join(kept, nil), 0o644); err != nil {
		t.Fatal(err)
	}
	backdate(t, filepath.Join(dir, "conversations", idMultiChunk+".db"), flat)

	var last step
	if err := json.Unmarshal(kept[len(kept)-1], &last); err != nil {
		t.Fatal(err)
	}
	want, err := time.Parse(time.RFC3339, last.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}

	s, err := readOne(t, NewWithRoot(dir), idMultiChunk)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiagnostic(s, "1 unparseable transcript record skipped") {
		t.Errorf("want exactly one torn record counted once; the windows overlap and "+
			"counted it twice: %v", s.Diagnostics)
	}
	if s.LastActivity == nil || !s.LastActivity.Equal(want) {
		t.Errorf("last_activity = %v, want the newest kept step %v", s.LastActivity, want)
	}
}

// ------------------------------------------- the vendor behavior nobody has seen

// THE case §3.8 names as unobserved: the flat file stops being complete once a
// second chunk exists. The worst plausible form of it is modeled here — the
// vendor froze `transcript.jsonl` at chunk 0 and writes new steps only into
// `chunks/transcript/00000001.jsonl` — and the answer is that it costs
// PRECISION, not the row.
//
// The mtime fold is what carries it (§6 Q8): last_activity is the fresher of
// the step timestamps and the mtimes of the transcript, the database and its
// sidecar, and on a live conversation the database is the file actually being
// written. So a transcript frozen six minutes ago still yields a correct
// last_activity from the database's mtime. The row does not degrade, no
// diagnostic fires, and nothing is invented — which is the honest outcome, not
// a lucky one: the adapter never claimed the transcript was its only clock.
func TestMultiChunkFlatFileFrozenAtTheFirstChunkStillDatesTheRow(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, root(), dir)

	logs := multiChunkLogs(dir)
	chunk0, err := os.ReadFile(filepath.Join(logs, "chunks", "transcript", "00000000.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	flat := filepath.Join(logs, "transcript.jsonl")
	if err := os.WriteFile(flat, chunk0, 0o644); err != nil {
		t.Fatal(err)
	}
	backdate(t, flat)

	// The database is the file the vendor is still writing. Give it a definite
	// recent mtime so the assertion is about which signal won, not about how
	// fast the test ran.
	db := filepath.Join(dir, "conversations", idMultiChunk+".db")
	live := time.Now().Add(-90 * time.Second).Truncate(time.Second)
	if err := os.Chtimes(db, live, live); err != nil {
		t.Fatal(err)
	}

	s, err := readOne(t, NewWithRoot(dir), idMultiChunk)
	if err != nil {
		t.Fatalf("a frozen flat transcript must not fail the row: %v", err)
	}
	if s.LastActivity == nil {
		t.Fatal("last_activity absent; the database mtime is readable")
	}
	if s.LastActivity.Equal(multiChunkChunk0Last) {
		t.Errorf("last_activity = %v, the frozen transcript's last step — the fresher "+
			"database mtime %v must win", s.LastActivity, live)
	}
	if d := s.LastActivity.Sub(live); d < -time.Second || d > time.Second {
		t.Errorf("last_activity = %v, want the database mtime %v", s.LastActivity, live)
	}
	if !s.Degraded.Empty() {
		t.Errorf("a stale transcript degraded a field: %s (%v)", s.Degraded, s.Diagnostics)
	}
	if err := s.Validate(newAdapter().Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// The partial-read rule (CLAUDE.md, ADR-001) over the multi-chunk case: damage
// inside the second chunk degrades the READING and never the row. The vendor's
// own 1.1.13 release note claimed a transcript-corruption fix during
// compaction, so torn records arriving with a chunk boundary is the failure
// this seam should expect rather than the one it should be surprised by.
//
// Both ways a record can be unreadable are exercised, because they are
// different branches: JSON that does not parse, and JSON that parses with a
// timestamp that does not. Each counts once, the count is stated once in
// Diagnostics, and the newest SURVIVING step still dates the row.
func TestMultiChunkDamageInTheSecondChunkDegradesTheFieldNotTheRow(t *testing.T) {
	dir := t.TempDir()
	copyTree(t, root(), dir)

	flat := filepath.Join(multiChunkLogs(dir), "transcript.jsonl")
	raw, err := os.ReadFile(flat)
	if err != nil {
		t.Fatal(err)
	}
	recs, partial := splitLines(raw)
	if len(partial) != 0 {
		t.Fatalf("the fixture transcript does not end on a newline; %d trailing bytes", len(partial))
	}
	if len(recs) < 4 {
		t.Fatalf("transcript holds %d records, too few to damage", len(recs))
	}
	// The last three records, which are inside the tail window by construction.
	recs[len(recs)-1] = []byte("{\"step_index\": 699, \"created_at\":\n")
	recs[len(recs)-2] = []byte("not json at all\n")
	recs[len(recs)-3] = []byte("{\"step_index\":697,\"created_at\":\"the fourteenth\"}\n")
	if err := os.WriteFile(flat, bytes.Join(recs, nil), 0o644); err != nil {
		t.Fatal(err)
	}
	backdate(t,
		filepath.Join(dir, "conversations", idMultiChunk+".db"),
		flat,
	)

	s, err := readOne(t, NewWithRoot(dir), idMultiChunk)
	if err != nil {
		t.Fatalf("three torn records must not fail the row: %v", err)
	}
	if !hasDiagnostic(s, "3 unparseable transcript records skipped") {
		t.Errorf("the torn records were not counted once and named once: %v", s.Diagnostics)
	}
	// Step 696 is the newest record that survived: 09:00:00 + 696s.
	want := time.Date(2026, 8, 1, 9, 11, 36, 0, time.UTC)
	if s.LastActivity == nil || !s.LastActivity.Equal(want) {
		t.Errorf("last_activity = %v, want the newest surviving step %v", s.LastActivity, want)
	}
	if s.Degraded.Has(model.FieldLastActivity) {
		t.Error("last_activity degraded while readable timestamps remained; damage is not absence")
	}
	if err := s.Validate(newAdapter().Capabilities()); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

// The PII boundary over a transcript 400x the size of the others. The sweep in
// TestTranscriptContentNeverReachesASession already covers every discovered
// conversation, including this one; what is asserted here is the chunk tree,
// which that sweep cannot distinguish from the flat file.
func TestMultiChunkChunkTreeCarriesTheMarkerAndNothingSurfacesIt(t *testing.T) {
	chunk := filepath.Join(multiChunkLogs(root()), "chunks", "transcript", "00000001.jsonl")
	raw, err := os.ReadFile(chunk)
	if err != nil {
		t.Fatalf("%v (regenerate: cd testdata && uv run python gen_fixtures.py)", err)
	}
	if !strings.Contains(string(raw), piiMarker) {
		t.Fatal("the chunk fixture no longer plants the marker; this test asserts nothing")
	}
	s := mustRead(t, idMultiChunk)
	for _, d := range s.Diagnostics {
		if strings.Contains(d, piiMarker) {
			t.Errorf("a diagnostic surfaced transcript content: %q", d)
		}
	}
	for _, e := range s.Extras {
		if strings.Contains(e.Value, piiMarker) || strings.Contains(e.Label, piiMarker) {
			t.Errorf("an extra surfaced transcript content: %q = %q", e.Label, e.Value)
		}
	}
}

// splitLines frames on the 0x0A byte and keeps the terminator, the same rule
// internal/jsonl applies. A test that rebuilt the file with strings.Split would
// silently drop the terminators and change every offset in it.
func splitLines(buf []byte) (recs [][]byte, partial []byte) {
	for len(buf) > 0 {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			return recs, buf
		}
		recs = append(recs, buf[:i+1])
		buf = buf[i+1:]
	}
	return recs, nil
}
