// Package drift reports that a vendor's on-disk store no longer has the shape
// its adapter was verified against.
//
// Every adapter here reads a private, undocumented format, pinned to the vendor
// build the survey read (docs/design.md §3). When a vendor ships an update that
// renames a key or moves a table, an adapter does not fail: the JSON still
// parses, the database still opens, and the reader simply stops finding what it
// was looking for. The row renders em dashes, the em dashes mean "the vendor had
// nothing to say", and the display is confidently wrong. That is the one failure
// mode this project exists to forbid, and it is the only one a reader cannot
// catch by reading harder — absence and a rename look identical from the side of
// the thing that is missing.
//
// A canary tells them apart. The survey establishes that some structural fact is
// present on EVERY well-formed unit of a corpus: Codex writes session_meta as the
// first record of every rollout, an agy conversation database always carries a
// gen_metadata table. A read that examined units of the corpus and found no
// canary is reading a corpus that has moved, and it says so.
//
// # Two tiers, and this is the second
//
// A store whose shape cannot be read AT ALL is a vendor-level failure and is
// reported as an error from Discover, which the HUD renders on the vendor line
// beside the store's path; cursor.ErrSchemaMismatch is the precedent and this
// package does not disturb it. The tier here is the other one: the store is
// readable, the sessions are real, and some part of the shape the readings hang
// off is no longer there. That degrades the fields the canary feeds and explains
// why, in the honest-gauge vocabulary the schema already has (Session.Degraded,
// Session.Diagnostics) rather than a parallel one.
//
// # What a canary deliberately is not
//
//   - Not a version comparison. Vendors ship releases that do not move a byte of
//     the format. A report that fires on every release is a report nobody reads,
//     so a version is CONTEXT on a structural failure and never the trigger.
//   - Not a schema fingerprint. A hash of the table and column set changes when a
//     vendor ADDS a column, which costs this program nothing — every reader here
//     addresses columns by name for exactly that reason. A canary is the
//     load-bearing subset of a fingerprint, and only that subset.
//   - Not a parse-success ratio. Drift usually produces no parse failures at all,
//     which is what makes it silent; a ratio measures torn writes, which the
//     adapters already count and diagnose. A threshold on one would also be an
//     invented boundary (decisions/001).
package drift

import (
	"strings"

	"github.com/sanlee-ys/telltale/internal/model"
)

// Canary is one structural fact an adapter's readings depend on, established by
// the survey as present on every well-formed unit of the corpus.
//
// Name is structural and is rendered to the user: a record type, a key, a table.
// It must be incapable of carrying corpus content — this repo is public and a
// diagnostic is a displayed string.
//
// Feeds names the normalized fields that stop being sourceable once the canary
// is gone. It is what turns "the shape moved" into "and this is what it cost",
// and it must name only fields the adapter declares in its Capabilities.
type Canary struct {
	Name  string
	Feeds model.FieldSet
}

// Watch accumulates canary sightings over one read.
//
// It is per-read state and never adapter state: adapters are polled in parallel
// and hold nothing mutable. The cost is one small allocation plus one string
// compare per sighting against a set that is never larger than a handful — the
// check rides along on a parse that is happening anyway and adds no I/O
// whatsoever, which is what keeps it off the poll loop's budget.
type Watch struct {
	verified string
	observed string
	expect   []Canary
	seen     []bool
}

// NewWatch starts a watch on the canaries an adapter's field map depends on.
//
// verified names the vendor build that field map was read from, because the
// first question a drift report raises is "moved from what?".
func NewWatch(verified string, cs ...Canary) *Watch {
	return &Watch{verified: verified, expect: cs, seen: make([]bool, len(cs))}
}

// Saw records a sighting. Sighting the same canary repeatedly is the normal case
// and costs nothing.
func (w *Watch) Saw(c Canary) {
	if w == nil {
		return
	}
	for i := range w.expect {
		if w.expect[i].Name == c.Name {
			w.seen[i] = true
			return
		}
	}
}

// Observed records the vendor version the store named, for stores that name one.
// It never triggers a report on its own (see the package doc); it is what an
// operator reads next, after the report says something moved.
func (w *Watch) Observed(version string) {
	if w != nil && version != "" {
		w.observed = version
	}
}

// Missing lists the canaries this read did not sight, in declaration order.
func (w *Watch) Missing() []Canary {
	if w == nil {
		return nil
	}
	var out []Canary
	for i, c := range w.expect {
		if !w.seen[i] {
			out = append(out, c)
		}
	}
	return out
}

// Fold writes the watch's verdict onto a session. It must run after every field
// is set, because it reads what the session managed to source.
//
// sampled is how many well-formed units of the corpus this read examined —
// records, rows, one database. ZERO IS THE LOAD-BEARING CASE: a read that
// examined nothing (an empty file, a transcript whose only record was torn
// mid-write) has no evidence either way, and calling that drift would turn the
// routine race with a vendor's writer into a standing false alarm.
//
// A field the session sourced anyway is NOT degraded: a canary can be gone while
// the value it usually feeds arrives from elsewhere, and a value that was read is
// a value. The report still stands — the shape moved, and that is worth saying
// whether or not this particular row paid for it.
func (w *Watch) Fold(s *model.Session, sampled int) {
	if w == nil || s == nil || sampled <= 0 {
		return
	}
	missing := w.Missing()
	if len(missing) == 0 {
		return
	}
	var feeds model.FieldSet
	names := make([]string, 0, len(missing))
	for _, c := range missing {
		names = append(names, c.Name)
		feeds = feeds.Union(c.Feeds)
	}
	s.Degraded = s.Degraded.Union(feeds.Minus(s.Present()))
	s.Diagnostics = append(s.Diagnostics, w.note(names))
}

// note is the single place a drift report is worded, so that five adapters
// cannot describe one event five ways.
func (w *Watch) note(names []string) string {
	var b strings.Builder
	b.WriteString("shape drift: ")
	b.WriteString(strings.Join(names, ", "))
	b.WriteString(" not found (adapter verified against ")
	b.WriteString(w.verified)
	if w.observed != "" && w.observed != w.verified {
		b.WriteString("; store reports ")
		b.WriteString(w.observed)
	}
	b.WriteString(")")
	return b.String()
}
