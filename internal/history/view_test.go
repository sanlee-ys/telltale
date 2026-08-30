package history

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

var update = flag.Bool("update", false, "rewrite the golden renders")

// ledger is the report every golden in this file starts from. It is built by
// hand rather than read from a corpus, because Render is pure over a Report and
// a golden that went through the reader would fail for two different reasons at
// once.
func ledger() Report {
	return Report{
		Vendor: string(model.VendorClaude),
		Root:   `C:\src\home\.claude\projects`,
		Zone:   "TST UTC-05:00",
		Days:   7,
		From:   "2026-08-23",
		To:     "2026-08-29",
		Rows: []Row{
			{Day: "2026-08-24", Workspace: `C:\src\code\telltale`,
				Counts:   Counts{Input: 1204, CacheRead: 1903551, CacheWrite: 62004, Output: 13118},
				Requests: 14, Sessions: 2},
			{Day: "2026-08-27", Workspace: `C:\src\code\notes-api`,
				Counts:   Counts{Input: 96, CacheRead: 0, CacheWrite: 4102, Output: 812},
				Requests: 3, Sessions: 1},
			{Day: "2026-08-27", Workspace: `C:\src\code\telltale`,
				Counts:   Counts{Input: 22140, CacheRead: 8830112, CacheWrite: 511903, Output: 140277},
				Requests: 191, Sessions: 5},
			{Day: "2026-08-29", Workspace: `C:\src\code\a-very-long-workspace-path\telltale`,
				Counts:   Counts{Input: 3, CacheRead: 12, CacheWrite: 0, Output: 44},
				Requests: 1, Sessions: 1},
		},
		Transcripts: 41,
		Records:     39184,
		Diagnostics: []string{"2 unparseable records skipped"},
		Coverage:    Survey(),
	}
}

// TestGoldenLedger pins the whole frame: the provenance lines, the table, the
// two refusals and the coverage block. One golden per named scenario, per
// CLAUDE.md.
func TestGoldenLedger(t *testing.T) {
	compareGolden(t, "ledger", Render(ledger(), Options{}))
}

// TestGoldenZeroVsAbsent is the rendered half of the property
// TestZeroAndAbsentAreDifferentRows pins on the data.
//
// It is its own golden rather than a case inside `ledger` for the reason
// internal/hud keeps its own zero-vs-absent file: the name says what the golden
// is FOR, and a reviewer who sees this one change knows immediately which
// distinction moved.
//
// In the frame: `quiet` made one request that reported four zeros and draws
// `0` four times; `busy` made none that day and draws nothing at all, on either
// day it is absent from. There is no rectangular grid here and there must never
// be one.
func TestGoldenZeroVsAbsent(t *testing.T) {
	r := ledger()
	r.Diagnostics = nil
	r.Rows = []Row{
		{Day: "2026-08-27", Workspace: `C:\src\code\quiet`, Requests: 1, Sessions: 1},
		{Day: "2026-08-28", Workspace: `C:\src\code\busy`,
			Counts:   Counts{Input: 500, CacheRead: 9000, CacheWrite: 120, Output: 640},
			Requests: 4, Sessions: 1},
	}
	compareGolden(t, "zero-vs-absent", Render(r, Options{}))
}

// TestGoldenNoStore pins the empty state that is NOT a zero: the vendor's store
// is not on this machine, so nothing was read, and the frame says so in words.
func TestGoldenNoStore(t *testing.T) {
	r := ledger()
	r.RootAbsent = true
	r.Rows, r.Diagnostics, r.Transcripts, r.Records = nil, nil, 0, 0
	compareGolden(t, "no-store", Render(r, Options{}))
}

// TestGoldenNarrow pins the 60-column floor. Every number survives; the
// workspace column is the one that gives way, and it gives way at the FRONT so
// the identifying tail of a path stays readable.
func TestGoldenNarrow(t *testing.T) {
	compareGolden(t, "narrow", Render(ledger(), Options{Width: 60}))
}

// TestRenderIsPureOverItsReport. Two renders of one report must be byte
// identical — no clock, no filesystem, no env read anywhere on this path. It is
// what makes the goldens above stable in CI, and it is the trap CLAUDE.md names
// first among the golden-test traps.
func TestRenderIsPureOverItsReport(t *testing.T) {
	r := ledger()
	if a, b := Render(r, Options{}), Render(r, Options{}); a != b {
		t.Error("two renders of one Report differ; something on the render path reads the world")
	}
}

// TestNoTotalIsRenderedAnywhere is the hard rule of design.md §7.17, asserted on
// the rendered string rather than on the code — this repository tests what a
// reader sees.
//
// Two sums are refused and both are checked here:
//
//   - across VENDORS. Only the covered vendor's name may appear beside a number.
//   - across the four COLUMNS. Input, cache read, cache write and output are
//     billed separately and telltale holds no price, so no cell may hold their
//     sum. The test computes what such a total WOULD read as and fails if it
//     appears anywhere in the frame.
func TestNoTotalIsRenderedAnywhere(t *testing.T) {
	r := ledger()
	out := Render(r, Options{})

	for _, row := range r.Rows {
		sum := row.Counts.Input + row.Counts.CacheRead + row.Counts.CacheWrite + row.Counts.Output
		if strings.Contains(out, group(sum)) {
			t.Errorf("the frame carries %s, which is one row's four counts added together.\n"+
				"They are four separately billed categories and telltale holds no price:\n"+
				"a total across them looks like a bill and is not one (design.md §7.17).",
				group(sum))
		}
	}
	// And the word that would introduce one.
	for _, banned := range []string{"TOTAL", "Total spend", "fleet total"} {
		if strings.Contains(out, banned) {
			t.Errorf("the frame carries %q", banned)
		}
	}
}

// TestTheFrameBorrowsNoneOfQuotasVocabulary is
// TestUsageSpendBorrowsNoneOfQuotasVocabulary (design.md §7.17) on this surface.
//
// There is no denominator anywhere in a token count, so a gauge, a percentage,
// a countdown or a ceiling here would INVENT one — the same class of error as
// filling a CapNone field with a plausible guess. The check is on the rendered
// frame because that is where the violation would be.
func TestTheFrameBorrowsNoneOfQuotasVocabulary(t *testing.T) {
	out := Render(ledger(), Options{})
	for _, banned := range []string{"%", "█", "▏", "↻", "quota", "remaining", "limit of"} {
		if strings.Contains(out, banned) {
			t.Errorf("the history frame carries %q. This is a SPEND surface: no gauge, no\n"+
				"percentage, no countdown, no ceiling — there is no denominator in a token\n"+
				"count and rendering one would invent it (design.md §7.16, §7.17).", banned)
		}
	}
}

// TestEveryCountCarriesItsWindowAndItsVendor. §7.16's rule is that a sum never
// prints without its window; §7.17's is that a figure never travels without the
// vendor it belongs to. Both are checked on the frame, at the default width and
// at the floor, because the shed cascade is where a fact gets dropped.
func TestEveryCountCarriesItsWindowAndItsVendor(t *testing.T) {
	r := ledger()
	for _, w := range []int{200, 100, 80, 60, 40} {
		// Whitespace-normalized, because a fact that WRAPPED is still on the
		// frame. What this test is about is a fact that was DROPPED, and the two
		// look identical to a naive substring match on a narrow render.
		out := strings.Join(strings.Fields(Render(r, Options{Width: w})), " ")
		for _, want := range []string{r.Vendor, r.From, r.To, r.Zone} {
			if !strings.Contains(out, want) {
				t.Errorf("width %d: the frame lost %q. The window and the vendor are facts,\n"+
					"not decoration, and they may not shed.", w, want)
			}
		}
	}
}

// TestTheDerivedDayNamesItsZone. The vendor writes an instant; a calendar day is
// telltale's convention over it, and a derived value has to be marked. Here the
// marking is the zone printed beside the window rather than a `~`, because `~`
// marks an estimated VALUE and this is an exact reading under a stated
// convention (see the package doc).
func TestTheDerivedDayNamesItsZone(t *testing.T) {
	out := Render(ledger(), Options{})
	if !strings.Contains(out, "days resolved in TST UTC-05:00") {
		t.Error("the frame does not say which zone the day buckets were resolved in;\n" +
			"without it the DAY column reads as a fact the vendor wrote")
	}
}

// TestTheFrameNamesEveryVendorItDoesNotRead is the property that stops a
// one-vendor table from reading as a fleet answer.
//
// It asserts every uncovered vendor is named AND that a reason travels with each
// name — a bare list would say a vendor is missing and leave a reader to guess
// whether the gap is telltale's or the vendor's.
func TestTheFrameNamesEveryVendorItDoesNotRead(t *testing.T) {
	out := Render(ledger(), Options{})
	for _, c := range Survey() {
		if c.Covered {
			continue
		}
		if !strings.Contains(out, string(c.Vendor)) {
			t.Errorf("the frame never names %s, so its silence about that vendor reads as zero",
				c.Vendor)
		}
		// The first several words of the verdict, which is enough to prove the
		// reason travelled and is not so much that rewording the verdict breaks
		// this test as well as the golden.
		head := strings.Join(strings.Fields(c.Why)[:4], " ")
		if !strings.Contains(strings.Join(strings.Fields(out), " "), head) {
			t.Errorf("%s is named with no reason beside it (%q)", c.Vendor, head)
		}
	}
}

// TestTheTableSurvivesANarrowFrame. Every number has to be legible at the floor:
// the workspace is the only cell allowed to give way, and it gives way at the
// front so the path's identifying tail survives.
func TestTheTableSurvivesANarrowFrame(t *testing.T) {
	out := Render(ledger(), Options{Width: 60})
	for _, want := range []string{"1,903,551", "8,830,112", "140,277"} {
		if !strings.Contains(out, want) {
			t.Errorf("at 60 columns the frame lost the count %s", want)
		}
	}
	if !strings.Contains(out, "...") {
		t.Error("no workspace was truncated at 60 columns, so the fixture is not exercising the floor")
	}
	if strings.Contains(out, `...C:\src`) {
		t.Error("a workspace was truncated at the END; the tail is the identifying half")
	}
}

// TestCountsAreExactAndNeverRounded. theme.Tokens floors to "1.9M" on the gauge
// surfaces because a header line has no room for the digits. This surface is a
// table with the room, so it rounds nothing at all — and a reader who sees
// "1.9M" here would have no way to tell it from an exact 1,900,000.
func TestCountsAreExactAndNeverRounded(t *testing.T) {
	out := Render(ledger(), Options{})
	if !strings.Contains(out, "1,903,551") {
		t.Error("an exact count is missing from the frame")
	}
	for _, rounded := range []string{"1.9M", "8.8M", "1M", "140k"} {
		if strings.Contains(out, rounded) {
			t.Errorf("the frame carries the rounded form %q; this table rounds nothing", rounded)
		}
	}
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".txt")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/history -update)", err)
	}
	want := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if got != want {
		t.Errorf("render differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
