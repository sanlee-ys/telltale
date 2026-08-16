// Package pins is the machine-readable half of design.md §3.10's canary
// inventory: which vendor build each adapter's field map was read at, and which
// survey section to re-open when that build is no longer the one installed.
//
// # Why this exists at all
//
// Every adapter here reads a private, undocumented format and pins itself to the
// build the survey read. `internal/adapter/drift` watches the SHAPE of that
// format and reports when it moves, and its package doc is explicit that a
// version comparison is deliberately NOT a canary: vendors ship releases that do
// not move a byte of the format, so a report that fired on every release would be
// a report nobody reads. Nothing in that ruling is disturbed here, and this
// package adds no trigger to any adapter's read path.
//
// What that ruling leaves open is the other audience. A canary tells the USER
// that a row went quiet. Nobody tells the MAINTAINER that the survey behind a
// row is now older than the vendor on this disk — and two of these vendors
// self-update, so the pin drifts past silently. CI can never see it: CI has no
// vendors installed, so every version probe there resolves to nothing. The loop
// has to live where the vendors live, and the owner ruled on 2026-08-16 that the
// place is `telltale doctor`, which already resolves each seat's binary and asks
// it its version.
//
// # One source, and no second copy of a pin
//
// Each VerifiedAgainst below is the adapter's own exported constant, not a
// string repeated here. That is the whole reason those six constants are
// exported: a table that copied them would be a second place for a pin to live,
// and design.md §3.8 already records what happens when a pin has two homes — the
// §3.10 inventory cell read `agy 1.1.9` for a full release after the adapter
// moved to `agy 1.1.13`. The compiler now makes that particular fork
// unrepresentable, and pins_doc_test.go ties the surviving copy — the doc's own
// table — to these constants cell by cell.
//
// Section and DocLabel are the only facts this table adds. They have no other
// home: an adapter knows what it was verified against, but not which prose
// section carries the evidence, and a pointer into the doc is what turns "this
// is stale" into an instruction someone can act on.
package pins

import (
	"github.com/sanlee-ys/telltale/internal/adapter/antigravity"
	"github.com/sanlee-ys/telltale/internal/adapter/claudecode"
	"github.com/sanlee-ys/telltale/internal/adapter/codex"
	"github.com/sanlee-ys/telltale/internal/adapter/cursor"
	"github.com/sanlee-ys/telltale/internal/adapter/gemini"
	"github.com/sanlee-ys/telltale/internal/adapter/grok"
	"github.com/sanlee-ys/telltale/internal/model"
)

// Pin is one adapter's survey pin.
type Pin struct {
	Vendor model.VendorID
	// VerifiedAgainst is the vendor build the field map was read at, in the
	// adapter's own words. Always the adapter's constant.
	VerifiedAgainst string
	// Section is the design.md section holding that survey's evidence, written
	// the way the doc writes it ("§3.8"). It is what a staleness notice tells the
	// maintainer to re-open.
	Section string
	// DocLabel is the name §3.10's table gives this adapter's row. It exists so
	// the doc guard can match a row to an adapter exactly, rather than comparing
	// unordered sets of version strings and calling that agreement.
	DocLabel string
	// Incomparable, when set, says why this pin and an installed version cannot
	// be compared at all. It is a property of the two artifacts, not of any
	// machine, so it is recorded once here rather than discovered per run.
	//
	// A seat carrying it gets NO drift verdict in either direction — see
	// internal/doctor. Reporting "no drift" for a comparison that never happened
	// is the same collapse §4a.1 forbids everywhere else in this repo.
	Incomparable string
}

// all is the table. Ordered as §3.10 orders its rows so that a reader diffing
// the two reads them in the same sequence.
var all = []Pin{
	{
		Vendor:          claudecode.Vendor,
		VerifiedAgainst: claudecode.VerifiedAgainst,
		Section:         "§3.1",
		DocLabel:        "Claude Code",
	},
	{
		Vendor:          codex.Vendor,
		VerifiedAgainst: codex.VerifiedAgainst,
		Section:         "§3.2",
		DocLabel:        "Codex CLI",
	},
	{
		Vendor:          gemini.Vendor,
		VerifiedAgainst: gemini.VerifiedAgainst,
		Section:         "§3.7",
		DocLabel:        "Gemini CLI",
		// Not a council seat (internal/council/detect.go names five candidates
		// and this is not one of them), so `telltale doctor` never probes it and
		// this row never renders. It is carried anyway because the table's job is
		// to mirror §3.10, and an inventory that silently omitted a row would let
		// that row's pin drift with nothing watching it.
	},
	{
		Vendor:          antigravity.Vendor,
		VerifiedAgainst: antigravity.VerifiedAgainst,
		Section:         "§3.8",
		DocLabel:        "Antigravity",
	},
	{
		Vendor:          cursor.Vendor,
		VerifiedAgainst: cursor.VerifiedAgainst,
		Section:         "§3.9",
		DocLabel:        "Cursor",
		// The two strings name different programs, which is why no comparison is
		// possible rather than merely awkward. The pin is the Cursor APPLICATION
		// version the store was surveyed inside (§3.9's environment line, "Cursor
		// 3.14.7"). What doctor probes is cursor-agent, reached through the
		// bundled node its .cmd launcher runs, and that answers a date-stamped
		// build of its own — `2026.08.04-aaa8809`, measured 2026-08-09 alongside
		// the other four seats. Comparing 3.14.7 with 2026.08.04 would manufacture
		// a permanent drift notice out of two unrelated numbering schemes.
		Incomparable: "the pin names the Cursor application the store was surveyed inside, " +
			"and this seat's binary is cursor-agent, which versions on a date-stamped scheme of its own",
	},
	{
		Vendor:          grok.Vendor,
		VerifiedAgainst: grok.VerifiedAgainst,
		Section:         "§3.9a",
		DocLabel:        "Grok CLI",
	},
}

// All returns the table. A copy, because a caller holding the package's own
// slice could reorder or rewrite the inventory for every other caller.
func All() []Pin {
	out := make([]Pin, len(all))
	copy(out, all)
	return out
}

// For returns the pin for one vendor. The second result distinguishes "this
// vendor has no surveyed adapter" from a zero pin, which is the same
// zero-vs-absent distinction the rest of this repo keeps.
func For(v model.VendorID) (Pin, bool) {
	for _, p := range all {
		if p.Vendor == v {
			return p, true
		}
	}
	return Pin{}, false
}
