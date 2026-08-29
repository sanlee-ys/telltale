package history

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Fixtures here are SYNTHESIZED to shape and never copied from a real corpus
// (CLAUDE.md). Session ids are runs of zeros, workspaces are fake paths, and no
// record carries message text of any kind — this repository is public, and the
// records this mode reads are the ones that sit next to a model's reply.

// now is the pinned observation clock every test in this file reads. It is a
// fixed instant in a fixed zone, because the day bucket is derived from the
// zone and a test that took the machine's own would pass or fail by geography.
var now = time.Date(2026, 8, 29, 14, 30, 0, 0, time.FixedZone("TST", -5*3600))

// day renders an offset from the pinned clock's own day, so a fixture can say
// "two days ago" without restating the date.
func day(n int) string { return now.AddDate(0, 0, n).Format(dayLayout) }

// stamp is an RFC3339 instant inside the local day n days from the clock's.
func stamp(n, hour int) string {
	d := now.AddDate(0, 0, n)
	return time.Date(d.Year(), d.Month(), d.Day(), hour, 0, 0, 0, now.Location()).Format(time.RFC3339Nano)
}

// corpus is a substitute HOME directory, laid out the way Query.Home expects
// and the way `go run ./tools/demo-corpus` writes one.
type corpus struct{ home string }

func newCorpus(t *testing.T) *corpus {
	t.Helper()
	return &corpus{home: t.TempDir()}
}

// transcript writes <home>/.claude/projects/<slug>/<uuid>.jsonl with the given
// raw records. The basename must be a UUID or the adapter's Discover skips it,
// which is the trap worth reproducing in the fixture rather than working around.
func (c *corpus) transcript(t *testing.T, slug, id string, records ...string) {
	t.Helper()
	dir := filepath.Join(c.home, ".claude", "projects", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join(records, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// uuid builds a legal session id from one distinguishing digit.
func uuid(n int) string { return "0000000" + string(rune('0'+n)) + "-0000-0000-0000-000000000000" }

// usageRec is one assistant record carrying a usage block.
func usageRec(ts, cwd string, in, cr, cw, out int64) string {
	return `{"type":"assistant","cwd":"` + cwd + `","timestamp":"` + ts + `",` +
		`"message":{"model":"claude-opus-5","usage":{"input_tokens":` + itoa(in) +
		`,"cache_read_input_tokens":` + itoa(cr) +
		`,"cache_creation_input_tokens":` + itoa(cw) +
		`,"output_tokens":` + itoa(out) + `}}}`
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func read(t *testing.T, c *corpus, days int) Report {
	t.Helper()
	rep, err := Read(context.Background(), Query{Home: c.home, Days: days, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func find(t *testing.T, r Report, d, workspace string) (Row, bool) {
	t.Helper()
	for _, row := range r.Rows {
		if row.Day == d && row.Workspace == workspace {
			return row, true
		}
	}
	return Row{}, false
}

// TestZeroAndAbsentAreDifferentRows is the literal test this repository exists
// to keep passing, on a new surface (CLAUDE.md; internal/hud's
// zero-vs-absent golden is the original).
//
// Three states are deliberately in one fixture, because the bug is always a
// collapse of two of them:
//
//   - a request that reported all four counts as ZERO. That is a measured zero:
//     the request happened, and the row must exist with 0 in every count.
//   - a workspace that made no request on that day. There must be NO ROW, and
//     specifically not a row of zeros — a zero row asserts a request nobody sent.
//   - a day inside the window with nothing at all. Same rule, one axis up.
//
// The failure this catches is the natural implementation: pre-seeding the
// buckets with every (day, workspace) pair in the window so the table is
// rectangular. A rectangular table here is a table of claims.
func TestZeroAndAbsentAreDifferentRows(t *testing.T) {
	c := newCorpus(t)
	// quiet made ONE request yesterday and it reported zeros.
	c.transcript(t, "quiet", uuid(1), usageRec(stamp(-1, 9), "/w/quiet", 0, 0, 0, 0))
	// busy made a request the day before yesterday only.
	c.transcript(t, "busy", uuid(2), usageRec(stamp(-2, 9), "/w/busy", 100, 200, 300, 400))

	r := read(t, c, 7)

	zero, ok := find(t, r, day(-1), "/w/quiet")
	if !ok {
		t.Fatalf("the measured zero has no row; rows: %+v", r.Rows)
	}
	if zero.Requests != 1 {
		t.Errorf("the zero row counted %d requests, want 1 — a request that reported "+
			"zero counts still happened", zero.Requests)
	}
	if zero.Counts != (Counts{}) {
		t.Errorf("the zero row carries %+v, want all four counts at 0", zero.Counts)
	}

	// busy did not touch yesterday. Absent must not become a zero row.
	if row, ok := find(t, r, day(-1), "/w/busy"); ok {
		t.Errorf("a workspace with no request on %s got a row anyway: %+v.\n"+
			"Absent and zero are different states and the table must keep them apart —\n"+
			"this row asserts a request nobody sent.", day(-1), row)
	}
	// And a whole day with nothing must not appear at all.
	for _, row := range r.Rows {
		if row.Day == day(-3) {
			t.Errorf("day %s had no token-bearing record and got a row anyway: %+v", day(-3), row)
		}
	}
	if len(r.Rows) != 2 {
		t.Errorf("got %d rows, want exactly 2 (one measured zero, one real day); rows: %+v",
			len(r.Rows), r.Rows)
	}
}

// TestCountsBucketByDayAndWorkspace pins the two axes the mode is named for,
// including the case that makes a per-project axis worth having: one session
// that moved between two working directories contributes to two rows.
func TestCountsBucketByDayAndWorkspace(t *testing.T) {
	c := newCorpus(t)
	c.transcript(t, "p", uuid(1),
		usageRec(stamp(-1, 9), "/w/alpha", 10, 20, 30, 40),
		usageRec(stamp(-1, 11), "/w/alpha", 1, 2, 3, 4),
		usageRec(stamp(-1, 13), "/w/beta", 5, 0, 0, 6),
		usageRec(stamp(0, 8), "/w/alpha", 7, 0, 0, 8),
	)
	// A second session in the same workspace on the same day folds into one row
	// and raises the SESSIONS count, not the request count twice over.
	c.transcript(t, "q", uuid(2), usageRec(stamp(-1, 15), "/w/alpha", 100, 0, 0, 100))

	r := read(t, c, 7)

	alpha, ok := find(t, r, day(-1), "/w/alpha")
	if !ok {
		t.Fatalf("no row for alpha on %s; rows: %+v", day(-1), r.Rows)
	}
	want := Counts{Input: 111, CacheRead: 22, CacheWrite: 33, Output: 144}
	if alpha.Counts != want {
		t.Errorf("alpha counts %+v, want %+v", alpha.Counts, want)
	}
	if alpha.Requests != 3 {
		t.Errorf("alpha requests %d, want 3", alpha.Requests)
	}
	if alpha.Sessions != 2 {
		t.Errorf("alpha sessions %d, want 2", alpha.Sessions)
	}
	if _, ok := find(t, r, day(-1), "/w/beta"); !ok {
		t.Errorf("the second workspace on the same day has no row of its own")
	}
	if _, ok := find(t, r, day(0), "/w/alpha"); !ok {
		t.Errorf("today's row is missing; the window is half-open and must include today")
	}

	// Rows come out day-ascending, so the newest day lands next to the prompt.
	for i := 1; i < len(r.Rows); i++ {
		if r.Rows[i-1].Day > r.Rows[i].Day {
			t.Fatalf("rows are not day-ascending: %q before %q", r.Rows[i-1].Day, r.Rows[i].Day)
		}
	}
}

// TestARecordWithNoReadableTimestampIsInNoDay. The tempting fallback is to put
// it in today, and that is the bug: it would move a measurement onto a day
// nothing said it belonged to, and the day column would then carry a value the
// vendor never wrote.
func TestARecordWithNoReadableTimestampIsInNoDay(t *testing.T) {
	c := newCorpus(t)
	c.transcript(t, "p", uuid(1),
		`{"type":"assistant","cwd":"/w/a","timestamp":"not-a-time",`+
			`"message":{"model":"claude-opus-5","usage":{"input_tokens":9,`+
			`"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":9}}}`,
		usageRec(stamp(-1, 9), "/w/a", 1, 0, 0, 1),
	)

	r := read(t, c, 7)
	for _, row := range r.Rows {
		if row.Counts.Input == 9 || row.Counts.Input == 10 {
			t.Errorf("the undated record was folded into %s: %+v", row.Day, row)
		}
	}
	if !hasDiagnostic(r, "no readable timestamp") {
		t.Errorf("the undated record was dropped silently; diagnostics: %v", r.Diagnostics)
	}
}

// TestARecordAheadOfTheClockCannotBeDated mirrors every adapter's future-skew
// rule. A clock that is wrong must not be able to invent a day's spend.
func TestARecordAheadOfTheClockCannotBeDated(t *testing.T) {
	c := newCorpus(t)
	ahead := now.Add(time.Hour).Format(time.RFC3339Nano)
	c.transcript(t, "p", uuid(1), usageRec(ahead, "/w/a", 5, 0, 0, 5))

	r := read(t, c, 7)
	if len(r.Rows) != 0 {
		t.Errorf("a record stamped ahead of the clock was dated anyway: %+v", r.Rows)
	}
	if !hasDiagnostic(r, "ahead of this clock") {
		t.Errorf("the skewed record was dropped silently; diagnostics: %v", r.Diagnostics)
	}
}

// TestASyntheticRecordIsNotARequest. Claude Code writes its own assistant
// records for API errors and interrupts, stamped <synthetic> and carrying a
// zeroed usage block. Counting one would put a request in the ledger that was
// never sent to a model — and it would look exactly like the measured zero the
// test above insists on keeping, which is why the two live in one package.
func TestASyntheticRecordIsNotARequest(t *testing.T) {
	c := newCorpus(t)
	c.transcript(t, "p", uuid(1),
		`{"type":"assistant","cwd":"/w/a","timestamp":"`+stamp(-1, 9)+`",`+
			`"message":{"model":"<synthetic>","usage":{"input_tokens":0,`+
			`"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":0}}}`,
	)
	r := read(t, c, 7)
	if len(r.Rows) != 0 {
		t.Errorf("a <synthetic> record was counted as a request: %+v", r.Rows)
	}
}

// TestAnUnparseableRecordDegradesTheRunAndDoesNotEndIt is CLAUDE.md's partial-read
// rule at the level that matters here: the unit that must survive is the walk.
func TestAnUnparseableRecordDegradesTheRunAndDoesNotEndIt(t *testing.T) {
	c := newCorpus(t)
	c.transcript(t, "p", uuid(1),
		`{"type":"assistant", this is not json`,
		usageRec(stamp(-1, 9), "/w/a", 3, 0, 0, 4),
	)
	r := read(t, c, 7)
	row, ok := find(t, r, day(-1), "/w/a")
	if !ok {
		t.Fatalf("one bad record cost the whole file; rows: %+v", r.Rows)
	}
	if row.Counts.Input != 3 {
		t.Errorf("counts after the bad record: %+v", row.Counts)
	}
	if !hasDiagnostic(r, "unparseable record") {
		t.Errorf("the bad record was skipped silently; diagnostics: %v", r.Diagnostics)
	}
}

// TestOutsideTheWindowIsNotCountedAndIsNotADiagnostic. A record older than the
// window is the question the reader asked, answered — not a degradation. Naming
// it in Diagnostics would make a correct narrow window look like a broken read.
func TestOutsideTheWindowIsNotCountedAndIsNotADiagnostic(t *testing.T) {
	c := newCorpus(t)
	c.transcript(t, "p", uuid(1),
		usageRec(stamp(-30, 9), "/w/a", 999, 0, 0, 999),
		usageRec(stamp(-1, 9), "/w/a", 1, 0, 0, 1),
	)
	r := read(t, c, 7)
	if len(r.Rows) != 1 || r.Rows[0].Counts.Input != 1 {
		t.Errorf("the out-of-window record leaked into the report: %+v", r.Rows)
	}
	if len(r.Diagnostics) != 0 {
		t.Errorf("an out-of-window record produced a diagnostic: %v", r.Diagnostics)
	}
	if r.From != day(-6) || r.To != day(0) {
		t.Errorf("window %s..%s, want %s..%s", r.From, r.To, day(-6), day(0))
	}
}

// TestASidechainRecordIsNotCountedTwice. Sub-agent transcripts live in their own
// sidecar tree Discover does not walk; one appearing INLINE would be the same
// tokens a second time. The live corpus has none, so the diagnostic exists to
// make a vendor change visible rather than silent.
func TestASidechainRecordIsNotCountedTwice(t *testing.T) {
	c := newCorpus(t)
	c.transcript(t, "p", uuid(1),
		`{"type":"assistant","isSidechain":true,"cwd":"/w/a","timestamp":"`+stamp(-1, 9)+`",`+
			`"message":{"model":"claude-opus-5","usage":{"input_tokens":50,`+
			`"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":50}}}`,
		usageRec(stamp(-1, 10), "/w/a", 1, 0, 0, 1),
	)
	r := read(t, c, 7)
	row, _ := find(t, r, day(-1), "/w/a")
	if row.Counts.Input != 1 {
		t.Errorf("a sidechain record was counted: %+v", row.Counts)
	}
	if !hasDiagnostic(r, "sub-agent record") {
		t.Errorf("the inline sidechain record was skipped silently; diagnostics: %v", r.Diagnostics)
	}
}

// TestARecordWithNoWorkspaceIsNotAttributedToANeighbour. Folding it into the
// last cwd seen would be a guess, and a guess in the PROJECT column is
// indistinguishable from a reading once it is on screen.
func TestARecordWithNoWorkspaceIsNotAttributedToANeighbour(t *testing.T) {
	c := newCorpus(t)
	c.transcript(t, "p", uuid(1),
		usageRec(stamp(-1, 9), "/w/a", 1, 0, 0, 1),
		`{"type":"assistant","timestamp":"`+stamp(-1, 10)+`",`+
			`"message":{"model":"claude-opus-5","usage":{"input_tokens":7,`+
			`"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":7}}}`,
	)
	r := read(t, c, 7)
	named, _ := find(t, r, day(-1), "/w/a")
	if named.Counts.Input != 1 {
		t.Errorf("the record with no cwd was folded into the named workspace: %+v", named.Counts)
	}
	anon, ok := find(t, r, day(-1), "")
	if !ok {
		t.Fatalf("the record with no cwd vanished instead of getting its own bucket; rows: %+v", r.Rows)
	}
	if anon.Counts.Input != 7 {
		t.Errorf("anonymous bucket counts %+v", anon.Counts)
	}
}

// TestAMissingStoreIsNotAnEmptyLedger. "There is no store here" and "the store
// held nothing" are different statements, and the report keeps them apart on the
// struct so the render cannot blur them.
func TestAMissingStoreIsNotAnEmptyLedger(t *testing.T) {
	r, err := Read(context.Background(), Query{
		Home: filepath.Join(t.TempDir(), "nothing-here"), Days: 7, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !r.RootAbsent {
		t.Error("a store that is not on disk reported as present with no rows")
	}
	if len(r.Rows) != 0 {
		t.Errorf("rows from a store that does not exist: %+v", r.Rows)
	}
}

// TestAnEmptyStoreIsNotAMissingOne is the other half of the pair above.
func TestAnEmptyStoreIsNotAMissingOne(t *testing.T) {
	c := newCorpus(t)
	c.transcript(t, "p", uuid(1), `{"type":"user","cwd":"/w/a","timestamp":"`+stamp(-1, 9)+`"}`)
	r := read(t, c, 7)
	if r.RootAbsent {
		t.Error("a store that exists and holds no counts reported as missing")
	}
	if r.Transcripts != 1 {
		t.Errorf("walked %d transcripts, want 1", r.Transcripts)
	}
	if len(r.Rows) != 0 {
		t.Errorf("rows: %+v", r.Rows)
	}
}

// TestACancelledWalkReportsWhatItReadAndSaysSo. The deadline must not throw away
// real readings to avoid admitting one, and it must not present a truncated walk
// as a complete window.
func TestACancelledWalkReportsWhatItReadAndSaysSo(t *testing.T) {
	c := newCorpus(t)
	c.transcript(t, "p", uuid(1), usageRec(stamp(-1, 9), "/w/a", 1, 0, 0, 1))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := Read(ctx, Query{Home: c.home, Days: 7, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Incomplete {
		t.Error("a walk stopped by its deadline reported a complete window")
	}
}

// TestTheWindowIsWholeLocalDaysEndingToday pins the half-open boundary. A record
// stamped exactly at local midnight belongs to the day it starts, once.
func TestTheWindowIsWholeLocalDaysEndingToday(t *testing.T) {
	c := newCorpus(t)
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	c.transcript(t, "p", uuid(1),
		usageRec(midnight.Format(time.RFC3339Nano), "/w/a", 1, 0, 0, 1),
		usageRec(midnight.Add(-time.Nanosecond).Format(time.RFC3339Nano), "/w/a", 2, 0, 0, 2),
	)
	r := read(t, c, 1)
	if len(r.Rows) != 1 {
		t.Fatalf("a one-day window produced %d rows: %+v", len(r.Rows), r.Rows)
	}
	if r.Rows[0].Day != day(0) || r.Rows[0].Counts.Input != 1 {
		t.Errorf("row %+v: midnight belongs to the day it starts, and the nanosecond "+
			"before it is the previous day and outside a one-day window", r.Rows[0])
	}
}

func hasDiagnostic(r Report, want string) bool {
	for _, d := range r.Diagnostics {
		if strings.Contains(d, want) {
			return true
		}
	}
	return false
}
