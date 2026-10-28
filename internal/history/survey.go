package history

import "github.com/sanlee-ys/telltale/internal/model"

// Coverage is one vendor's answer to a single question: can this mode read a
// dated token count out of the files that vendor already writes?
//
// It is a struct rather than a sentence in the help because the same verdict has
// to reach three places without being retyped — the report every run prints, the
// long help, and the error a refused --vendor returns. A restated verdict is how
// one of the three comes to promise a vendor the reader cannot get.
type Coverage struct {
	// Vendor is the fleet id, so this table orders and reads like every other
	// per-vendor surface in the product.
	Vendor model.VendorID
	// Covered says whether `telltale history` reports this vendor TODAY.
	Covered bool
	// Why states the verdict in the survey's own terms. It names the field, the
	// file and the reason, because "not supported" would send a reader to open
	// an issue about work that is already understood.
	Why string
}

// survey is the 2026-08-29 read of every adapter's own field map, recorded so
// the mode's partial coverage can never read as a fleet answer.
//
// # What this survey is, and what it is not
//
// It is a SOURCE READ of this repository's adapters at the revision it was
// written on — each adapter's record struct, its package doc, and the live-corpus
// verdicts those docs already carry. It is NOT a fresh measurement against a live
// vendor. That distinction is the difference between "telltale's Claude field map
// says the counts are there" and "the counts were seen on this machine today",
// and CLAUDE.md's measured-claims rule makes it worth writing down: the vendor
// pins each verdict rests on are the adapters' own VerifiedAgainst constants, not
// anything this lane re-ran.
//
// # The two questions, in order
//
// A vendor joins this mode only when BOTH answer yes, and the order matters
// because the second one is the one that surprised the survey:
//
//  1. Does a per-turn or per-message token count reach disk at all?
//  2. Is that count DATED — can a reader put it in a calendar day without
//     inventing the day?
//
// Four vendors pass the first question and fail the second or fail on units. A
// count with no timestamp is not a smaller version of a history; it has no day
// axis at all, which is why agy is refused here despite writing real
// per-generation numbers.
//
// # The source read has already been caught out once, exactly where it said it
// # would be
//
// grok's verdict said its record carried a total and no date. A LIVE re-measure
// on 2026-08-29 at grok 1.0.5 read the record off disk and found a full
// input/output/cache split beside the envelope's own timestamp, present since
// 1.0.0 (design.md §3.9a). Nothing was wrong with the reading of
// internal/adapter/grok — that struct really does parse totalTokens alone. What
// was wrong is that a struct is an allowlist, so what it omits is a decision and
// not an absence, and the verdict reported the omission as the file's shape.
// This is the caveat above landing rather than a surprise, and the lesson
// generalises to every uncovered row here: BEFORE a vendor is built, re-read its
// records, not its struct.
//
// # The same rule stopped the grok build on 2026-10-28, one step later
//
// A lane sat down to build the grok reader against that corrected verdict and
// re-read the records as the rule says. This repository holds TWO measurements
// of the same `turn_completed.usage` object, and they disagree on the one
// property a day sum rests on — whether a record is one turn's counts or the
// session's running total:
//
//   - 2026-08-29, grok 1.0.5, the CLI (design.md §3.9a's re-measure): per-turn.
//     Two records of one session read inputTokens 21548 then 21958, "the second
//     is not the first plus anything", and numTurns is 1 on every record.
//   - 2026-09-04, grok 1.0.13, `grok agent stdio` (internal/council/vendors/
//     acp.go's header): the prompt response's `_meta.usage` is the SESSION's
//     running total — numTurns 2 then 3, every count larger than the per-prompt
//     count beside it — and it is recorded as value-for-value the
//     `turn_completed.usage` record the same session wrote to updates.jsonl.
//     The same capture says the total does not reconcile by subtraction with
//     the per-prompt count (top-level inputTokens 50,963 against a usage block
//     that grew by 62,054), so no delta over consecutive records recovers a
//     turn either.
//
// Whether that is build drift (1.0.5 to 1.0.13) or two shapes for two drivers
// is not something either measurement answers, and no on-disk sample at 1.0.13
// is in this repository: acp.go quotes the wire and asserts the disk matched.
// Summing running totals per day counts every earlier turn again, and
// subtracting them is measured not to work, so the reader was NOT built. The
// verdict below says what is owed instead of guessing which measurement wins.
//
// # Order
//
// FIXED FLEET ORDER — claude, codex, gemini, agy, cursor, grok — the same order
// the header's per-vendor counts and §7.17's usage blocks walk, so a vendor sits
// in the same place on every per-vendor surface in the product. Ordering this
// block by how CLOSE each vendor is to being covered was considered and
// declined: it reads well as a roadmap, and it would make one list in this
// product order vendors by a property no other list orders them by, which is
// exactly the reshuffle §7.17 spends a paragraph refusing. The roadmap signal
// lives in the words instead — codex's verdict says it is the next slice.
// pi is last because it is the newest adapter and is last in fleetOrder.
var survey = []Coverage{
	{
		Vendor:  model.VendorClaude,
		Covered: true,
		Why: "every assistant record carries message.usage with four raw counts — " +
			"input_tokens, cache_read_input_tokens, cache_creation_input_tokens, " +
			"output_tokens — beside an RFC3339 timestamp and the cwd the turn ran in. " +
			"Four billed categories, dated, per request, per project. " +
			"Field map pinned at Claude Code 2.1.233 (internal/adapter/claudecode).",
	},
	{
		Vendor:  model.VendorCodex,
		Covered: false,
		Why: "the strongest next slice. A token_count event carries " +
			"info.last_token_usage (this turn) and info.total_token_usage " +
			"(cumulative) beside the envelope's own timestamp, so the day axis is " +
			"there. Two things are owed first: which of the two a day may sum is a " +
			"ruling nobody has made, and there is no cache split, so a codex block " +
			"would carry two columns where the claude block carries four.",
	},
	{
		Vendor:  model.VendorGemini,
		Covered: false,
		Why: "a message carries tokens.input beside a timestamp, but input is " +
			"promptTokenCount — the size of the context that was SENT, which the " +
			"adapter already labels an occupancy proxy. Summing it per day counts " +
			"one conversation's prefix once per turn, under a column header that " +
			"would read like uncached input. The cached subset is not separable " +
			"from what the adapter parses, so the split that makes claude's four " +
			"columns honest is unavailable here.",
	},
	{
		Vendor:  model.VendorAntigravity,
		Covered: false,
		Why: "counts are on disk and are the best-guarded in the fleet — " +
			"gen_metadata carries uncached input and output per generation, behind " +
			"a thinking + answer == output identity the adapter asserts. Nothing " +
			"dates them: the reverse-engineered field map (design.md §3.8) carries " +
			"no per-generation timestamp, so there is no day to put a generation in.",
	},
	{
		Vendor:  model.VendorCursor,
		Covered: false,
		Why: "there is nothing to read. tokenCount.inputTokens and outputTokens " +
			"were 0 in 310 of 310 message rows in the live store, and the field is " +
			"declared CapNone rather than filled with a plausible number " +
			"(design.md §7.16). The vendor's counts arrive by hook, not on disk.",
	},
	{
		Vendor:  model.VendorGrok,
		Covered: false,
		Why: "the split and the date are both there, and two measurements of " +
			"the same record disagree on what one record IS, which is what blocks " +
			"a day sum. At grok 1.0.5 a turn_completed record's usage is one " +
			"turn's counts and numTurns reads 1 on every record (design.md §3.9a, " +
			"2026-08-29, the CLI). At grok 1.0.13 the same usage object is the " +
			"SESSION's running total — numTurns 2 then 3 — recorded as " +
			"value-for-value the record updates.jsonl wrote " +
			"(internal/council/vendors/acp.go, 2026-09-04, grok agent stdio), and " +
			"that capture says the total does not reconcile by subtraction with " +
			"the per-prompt count beside it. Summing a running total per day counts " +
			"every earlier turn again, and no delta recovers a turn, so nothing is " +
			"built on either reading. Owed first: one read of updates.jsonl at the " +
			"installed build, driven both ways, saying which shape the disk holds " +
			"and whether numTurns tells them apart. The unit trap stands beside it: " +
			"inputTokens INCLUDES the cache read here, where claude's input_tokens " +
			"excludes it, so the four columns are not the same four.",
	},
	{
		Vendor:  model.VendorPi,
		Covered: false,
		Why: "an assistant message carries usage.input and usage.output with a record " +
			"timestamp and a cwd, so this vendor is datable — the second-strongest " +
			"seam after codex. It carries no cache split either, and it carries " +
			"usage.cost.total per message — money, which this mode renders nowhere " +
			"and would have to rule on.",
	},
}

// Survey returns the coverage table, newest verdicts included, in fleet order.
//
// The slice is copied on the way out for the reason internal/adapter/claudecode
// copies its Extras: a package-level slice handed to a caller is one append away
// from a second caller seeing a table it did not build.
func Survey() []Coverage { return append([]Coverage(nil), survey...) }

// CoveredVendors lists the vendors this mode reports today.
func CoveredVendors() []model.VendorID {
	var out []model.VendorID
	for _, c := range survey {
		if c.Covered {
			out = append(out, c.Vendor)
		}
	}
	return out
}

// Verdict returns one vendor's coverage row.
func Verdict(v model.VendorID) (Coverage, bool) {
	for _, c := range survey {
		if c.Vendor == v {
			return c, true
		}
	}
	return Coverage{}, false
}
