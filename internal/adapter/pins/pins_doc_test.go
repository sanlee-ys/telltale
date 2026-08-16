package pins

import (
	"os"
	"strings"
	"testing"
)

const designDoc = "../../../docs/design.md"

func readDesign(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(designDoc)
	if err != nil {
		t.Fatalf("cannot read the design doc this table mirrors: %v", err)
	}
	return string(b)
}

// docPins parses §3.10's inventory table and returns its `verified against`
// column, keyed by the row label.
//
// It PARSES THE TABLE rather than searching the document, and that is the whole
// point of this file. Every adapter's own TestTheCanaryInventoryMatchesThisAdapter
// asks whether its pin appears anywhere in design.md, and design.md §3.8 records
// what that costs: when the Antigravity pin moved to `agy 1.1.13`, the dated
// paragraph announcing the move satisfied the substring search while the table
// the search exists to guard still read `agy 1.1.9`. The guard was green over a
// wrong cell for a full release. §3.8 closes by naming the fix — "scoping that
// assertion to the table" — and marking it unowned.
//
// So this reads the cell. A pin quoted in prose elsewhere in the document
// cannot satisfy it, and a stale cell cannot hide behind a fresh paragraph.
// The six adapter tests are left exactly as they are: they assert a different
// thing (that the adapter's own pin is documented at all), they are not weakened
// by anything here, and rewriting them is a separate concern from this one.
func docPins(t *testing.T, doc string) map[string]string {
	t.Helper()

	lines := strings.Split(strings.ReplaceAll(doc, "\r\n", "\n"), "\n")
	start := -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, "### 3.10 ") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("design.md has no `### 3.10 ` heading; the canary inventory this table mirrors has moved or been renumbered")
	}

	out := map[string]string{}
	for _, ln := range lines[start+1:] {
		if strings.HasPrefix(ln, "### ") {
			break // the next section; the table cannot run past it
		}
		if !strings.HasPrefix(strings.TrimSpace(ln), "|") {
			continue
		}
		parts := strings.Split(strings.TrimSpace(ln), "|")
		if len(parts) < 4 {
			continue
		}
		label := strings.TrimSpace(parts[1])
		cell := strings.TrimSpace(parts[2])
		switch {
		case label == "", label == "adapter":
			// A continuation row (a second canary for the same adapter) carries
			// no label and no pin, and the header row is not data.
			continue
		case strings.HasPrefix(label, "---"):
			continue
		}
		out[label] = strings.Trim(cell, "`")
	}
	if len(out) == 0 {
		t.Fatal("parsed §3.10 and found no rows; the table's shape has changed and this guard is now blind")
	}
	return out
}

// TestTheDocTableAndThePinTableAgreeCellByCell is the guard that lets this
// package be a second reader of the pins without becoming a second copy of them.
//
// The pin STRINGS are not copied at all — every VerifiedAgainst in all is the
// adapter's own exported constant, so the compiler already forbids that fork.
// What can still drift is design.md, which holds the only hand-written copy, and
// which drifted exactly this way once before.
func TestTheDocTableAndThePinTableAgreeCellByCell(t *testing.T) {
	fromDoc := docPins(t, readDesign(t))

	for _, p := range All() {
		got, ok := fromDoc[p.DocLabel]
		if !ok {
			t.Errorf("§3.10's table has no %q row; this table expects one pinned to %q",
				p.DocLabel, p.VerifiedAgainst)
			continue
		}
		if got != p.VerifiedAgainst {
			t.Errorf("§3.10's %s row is verified against %q, the adapter is pinned to %q — "+
				"the inventory cell has gone stale",
				p.DocLabel, got, p.VerifiedAgainst)
		}
		delete(fromDoc, p.DocLabel)
	}

	// A row in the doc with no adapter behind it is the other direction of the
	// same drift: an adapter that was removed, or a row whose label was reworded
	// so that nothing checks it any more.
	for label, pin := range fromDoc {
		t.Errorf("§3.10 carries a %q row pinned to %q that no adapter in this table claims", label, pin)
	}
}

// TestEveryPinPointsAtASectionThatExists. Section is a pointer into prose, and a
// pointer is worth exactly what it resolves to: `telltale doctor` prints it as
// the instruction for what to re-open, so a renumbered doc would send a
// maintainer to a section that is not there.
func TestEveryPinPointsAtASectionThatExists(t *testing.T) {
	doc := readDesign(t)
	for _, p := range All() {
		heading := "### " + strings.TrimPrefix(p.Section, "§") + " "
		if !strings.Contains(doc, "\n"+heading) {
			t.Errorf("%s's pin points at %s, and design.md has no %q heading",
				p.Vendor, p.Section, strings.TrimSpace(heading))
		}
	}
}

// TestEveryAdapterCarriesAPin guards the table against the quiet failure mode a
// list has: a new adapter is written, surveyed and pinned, and nobody adds the
// row — so the one vendor most likely to be moving is the one nothing watches.
//
// The count is asserted against §3.10's own row count rather than a literal, so
// the doc and the table have to grow together.
func TestEveryAdapterCarriesAPin(t *testing.T) {
	if got, want := len(All()), len(docPins(t, readDesign(t))); got != want {
		t.Errorf("the pin table has %d adapters and §3.10 documents %d", got, want)
	}
	for _, p := range All() {
		switch {
		case p.Vendor == "":
			t.Error("a pin names no vendor")
		case p.VerifiedAgainst == "":
			t.Errorf("%s carries no verified-against build", p.Vendor)
		case p.Section == "":
			t.Errorf("%s names no design.md section to re-measure", p.Vendor)
		case p.DocLabel == "":
			t.Errorf("%s names no §3.10 row label, so nothing can match it to the doc", p.Vendor)
		}
	}
}

// TestForDistinguishesAnUnknownVendorFromAZeroPin. The bool is not decoration:
// a caller that took the zero Pin for an answer would render "verified against
// ”" for a vendor this repo never surveyed.
func TestForDistinguishesAnUnknownVendorFromAZeroPin(t *testing.T) {
	if _, ok := For("telltale-no-such-vendor"); ok {
		t.Error("For claimed a pin for a vendor that does not exist")
	}
	p, ok := For(All()[0].Vendor)
	if !ok || p.VerifiedAgainst == "" {
		t.Errorf("For lost a pin that is in the table: %+v, ok=%v", p, ok)
	}
}

// TestAllHandsBackACopy. The table is package state read by a preflight; a
// caller that sorted or rewrote the slice in place would change what every later
// caller reports.
func TestAllHandsBackACopy(t *testing.T) {
	first := All()
	first[0].VerifiedAgainst = "tampered"
	if All()[0].VerifiedAgainst == "tampered" {
		t.Error("All handed out the package's own slice; a caller can rewrite the inventory")
	}
}
