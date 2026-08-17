// Package snapshot renders the fleet's current gauge state as one JSON
// document, for a reader that is a program rather than a person
// (docs/design.md §7.22).
//
// It is a THIRD read surface beside the statusline and the HUD, and it renders
// the same scan they render. It is not a fourth store and not a second scan
// path: `Build` takes a completed hud.Snapshot and reshapes it. An agent that
// wants to know what the fleet is doing runs one command and parses one
// document; it does not read ~/.telltale/, and it does not drive a TUI.
//
// Five rules govern the schema, and each one is a rule this repo already lives
// by:
//
//   - **Zero and absent stay different** (ADR-001, design.md §4a.1). A measured
//     zero is the number 0. An absent value is JSON `null`. No field uses a
//     sentinel number, and no optional field is omitted — a key that is always
//     present with an explicit `null` is what lets a reader tell "the fleet has
//     no cost" from "this schema changed under me".
//   - **A derived value says so.** `estimated` lists the fields whose value in
//     this document came from an adapter's computation rather than from vendor
//     output. It is the JSON form of the render layer's `~` marker.
//   - **"Can't know" is not "absent now".** `unsupported` lists the fields the
//     vendor exposes nothing for, ever. A `null` on a field in that list is a
//     capability statement; a `null` on any other field is this moment's
//     reading.
//   - **A claimed value says so.** `self_reported` marks an entry whose
//     numbers a writer asserted rather than one telltale measured (§7.23). It
//     is a different statement from `estimated`, which is about an adapter's
//     arithmetic, and the two never share a spelling.
//   - **Numbers and keys, never content.** The read/write boundary (CLAUDE.md)
//     holds here as it holds everywhere else: no transcript, no brief, no reply
//     text, no session name, no workspace path. A snapshot names vendors,
//     counts sessions and states measurements.
//
// Two things are deliberately NOT in this document.
//
// Session-level rows are absent because the rollup is the product. An agent
// asking "what is the fleet doing" wants one answer per vendor and one for the
// fleet, and a per-session array would put the reader back in the business of
// aggregating — plus every honest session identity (name, workspace) is
// content this surface may not carry.
//
// The relayed token totals are absent because their DISPLAY is held by the
// owner (§7.16's amendment, applied to grok on arrival in §7.16a). The relay
// is wired, the HUD reads it, and nothing renders it. A JSON field is a
// rendering; adding one here would end that hold as a side effect of a
// different feature.
package snapshot

import (
	"bytes"
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/sanlee-ys/telltale/internal/hud"
	"github.com/sanlee-ys/telltale/internal/model"
)

// SchemaVersion is the document's contract number. It rises when a field
// changes meaning or leaves; a field ADDED at the end is not a break, because
// every reader here parses by name.
const SchemaVersion = 1

// Document is the whole answer to one `telltale snapshot` run.
type Document struct {
	SchemaVersion int `json:"schema_version"`
	// GeneratedAt is when the scan completed, truncated to the second. It is
	// the scan's clock, not the render's: a reader deciding whether this
	// document is fresh must measure the reading, not the printing.
	GeneratedAt time.Time `json:"generated_at"`
	// ScanError is the scan's own failure (a cancelled context, a deadline),
	// distinct from any vendor's status. Null on a clean run.
	ScanError *string `json:"scan_error"`
	// Fleet is the pre-computed rollup. It exists so the common question costs
	// one parse and no arithmetic on the reader's side.
	Fleet Fleet `json:"fleet"`
	// Vendors is one entry per adapter that ran, sorted by vendor id. Never
	// null: an empty fleet is `[]`.
	Vendors []Vendor `json:"vendors"`
}

// Fleet is the cross-vendor rollup.
//
// Every count here is a count and can be zero. Every measurement is a pointer
// and can be null. The two kinds never share a field.
type Fleet struct {
	Sessions int `json:"sessions"`
	Live     int `json:"live"`
	Idle     int `json:"idle"`
	Stale    int `json:"stale"`
	// Unknown counts sessions whose liveness has no basis at all — no hint and
	// no last activity. It is its own count rather than being folded into
	// Stale, which is a claim about age that these rows cannot support.
	Unknown int `json:"unknown"`

	VendorsWatching    int `json:"vendors_watching"`
	VendorsNotDetected int `json:"vendors_not_detected"`
	VendorsUnreadable  int `json:"vendors_unreadable"`
	VendorsDrifted     int `json:"vendors_drifted"`

	// ContextPctMax is the highest context percentage any session reports, and
	// null when no session anywhere reports one. It is a max rather than a
	// mean: the fleet question is "is anything close to its window", which an
	// average over idle sessions hides.
	ContextPctMax *float64 `json:"context_pct_max"`
	// CostUSDTotal sums every session that reports a cost, and is null when
	// none does. A fleet of sessions that all cost zero totals 0, which is a
	// measurement.
	CostUSDTotal *float64 `json:"cost_usd_total"`
	// LastActivity is the most recent activity any session reports.
	LastActivity *time.Time `json:"last_activity"`
}

// Vendor is one adapter's standing.
type Vendor struct {
	Vendor model.VendorID `json:"vendor"`
	// Status is the vendor line's own word: watching, not detected,
	// unreadable, or drifted (hud.VendorStatus). A reader switching on this
	// gets the same four facts the HUD draws.
	Status string `json:"status"`
	// Error is the operating system's message when Status is "unreadable",
	// and null otherwise. It is the one case where the OS knows something the
	// reader needs.
	Error *string `json:"error"`

	Sessions int `json:"sessions"`
	Live     int `json:"live"`
	// Drifted counts the sessions whose read no longer found the structure the
	// adapter was verified against. It travels beside Sessions because one
	// drifted row out of forty and forty out of forty are different events.
	Drifted int `json:"drifted"`

	ContextPctMax *float64   `json:"context_pct_max"`
	CostUSDTotal  *float64   `json:"cost_usd_total"`
	SubagentsMax  *int       `json:"subagents_max"`
	LastActivity  *time.Time `json:"last_activity"`

	// Quota is the account quota this vendor can honestly speak for: the
	// windows the statusline relayed (design.md §7.15). Never null; a vendor
	// with no relayed reading carries `[]`.
	//
	// It comes from the account relay and never from a session, because quota
	// is a property of the account (§7.1). Hanging it off a row would assert a
	// per-session limit no vendor publishes.
	Quota []QuotaWindow `json:"quota"`
	// QuotaReadAt is when that relay was written, and null when there is no
	// relayed reading. A quota figure without the age of its reading is a
	// number a reader cannot judge.
	QuotaReadAt *time.Time `json:"quota_read_at"`

	// Estimated lists the fields in THIS document whose value an adapter
	// computed rather than read (model.Field names, sorted). Never null.
	Estimated []string `json:"estimated"`
	// Unsupported lists the fields this vendor exposes nothing for, ever
	// (model.Field names, sorted). Never null. A null value on a field named
	// here means "this vendor cannot know"; a null on any other field means
	// "not right now".
	Unsupported []string `json:"unsupported"`

	// SelfReported says every number in this entry is a claim its writer made,
	// rather than a reading telltale took from a vendor's own store
	// (design.md §7.23). False for every adapter that reads a vendor store.
	//
	// It is NOT `estimated` and must never be folded into it. `estimated`
	// names fields an adapter COMPUTED from something that is not the value,
	// and a drop-file adapter computes nothing — it reads verbatim what the
	// writer wrote. The two are different provenances, and a reader that could
	// not tell them apart would be back to the collapse §4a.1 forbids: one
	// spelling for "telltale inferred this" and "someone asserted this".
	//
	// It is a whole-entry flag rather than a per-field list because it is true
	// of every value in the entry without exception. A list would invite the
	// reading that the unlisted fields were measured.
	//
	// Adding this key does not raise SchemaVersion: it is a new field, and
	// this document's own rule is that a field ADDED is not a break because
	// every reader parses by name. Nothing already in the document changed
	// meaning, and no key left.
	SelfReported bool `json:"self_reported"`
}

// QuotaWindow is one relayed usage window.
type QuotaWindow struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// UsedPct is null when the window exists and carries no figure yet. It is
	// never 0 for that case — the distinction this whole package is built on.
	UsedPct  *float64   `json:"used_pct"`
	ResetsAt *time.Time `json:"resets_at"`
}

// reported is the set of fields this document carries a SESSION-sourced value
// for. Anything outside it cannot appear in `estimated` or `unsupported`,
// because a reader would have no field to attach the statement to.
//
// FieldQuota is deliberately not in this list, and the first live run is why.
// An adapter's quota capability describes what a SESSION exposes; the `quota`
// array here comes from the account relay (§7.15), which is a different source
// with a different owner. Listing both put `agy` in the document with two
// relayed windows and the word "quota" under `unsupported` — two true
// statements that read as one contradiction. Quota's absence needs no list: an
// empty array with a null read time already says it, definitively.
var reported = []model.Field{
	model.FieldContextPercent,
	model.FieldCost,
	model.FieldLastActivity,
	model.FieldSubagents,
}

// Build reshapes one completed scan into the document.
//
// It is pure over its arguments — no clock, no filesystem, no environment —
// for the reason internal/hud's Render is: a golden test can only pin an
// output that depends on nothing else.
func Build(snap hud.Snapshot, th model.LivenessThresholds) Document {
	doc := Document{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   stamp(snap.At),
		Vendors:       []Vendor{},
	}
	if snap.Err != "" {
		e := snap.Err
		doc.ScanError = &e
	}

	byVendor := map[model.VendorID][]*model.Session{}
	for _, s := range snap.Sessions {
		byVendor[s.Vendor] = append(byVendor[s.Vendor], s)
	}
	account := map[model.VendorID]quota{}
	for _, a := range snap.Account {
		account[a.Vendor] = quota{windows: a.Windows, at: a.WrittenAt}
	}

	var fleetCtx, fleetCost *float64
	var fleetLast *time.Time
	for _, view := range snap.Vendors {
		sessions := byVendor[view.Vendor]
		v := Vendor{
			Vendor:       view.Vendor,
			Status:       view.Status.String(),
			Sessions:     len(sessions),
			Drifted:      view.Drifted,
			Quota:        []QuotaWindow{},
			Estimated:    []string{},
			Unsupported:  []string{},
			SelfReported: view.SelfReported,
		}
		if view.Err != "" {
			e := view.Err
			v.Error = &e
		}

		estimated := map[model.Field]bool{}
		for _, s := range sessions {
			switch s.Liveness(snap.At, th) {
			case model.LivenessLive:
				v.Live++
				doc.Fleet.Live++
			case model.LivenessIdle:
				doc.Fleet.Idle++
			case model.LivenessStale:
				doc.Fleet.Stale++
			default:
				doc.Fleet.Unknown++
			}
			if s.ContextPercent != nil {
				v.ContextPctMax = maxFloat(v.ContextPctMax, float64(*s.ContextPercent))
			}
			if s.Cost != nil {
				v.CostUSDTotal = addFloat(v.CostUSDTotal, float64(*s.Cost))
			}
			if s.Subagents != nil {
				v.SubagentsMax = maxInt(v.SubagentsMax, *s.Subagents)
			}
			if s.LastActivity != nil {
				v.LastActivity = laterTime(v.LastActivity, *s.LastActivity)
			}
			for _, f := range reported {
				if s.Derived.Has(f) && s.Has(f) {
					estimated[f] = true
				}
			}
		}
		doc.Fleet.Sessions += len(sessions)

		if q, ok := account[view.Vendor]; ok && len(q.windows) > 0 {
			for _, w := range q.windows {
				out := QuotaWindow{ID: w.ID, Label: w.Label}
				if w.UsedPercent != nil {
					out.UsedPct = roundPtr(float64(*w.UsedPercent), 1)
				}
				if w.ResetsAt != nil {
					t := stamp(*w.ResetsAt)
					out.ResetsAt = &t
				}
				v.Quota = append(v.Quota, out)
			}
			t := stamp(q.at)
			v.QuotaReadAt = &t
		}

		for _, f := range reported {
			if estimated[f] {
				v.Estimated = append(v.Estimated, f.String())
			}
			if view.Caps.Capability(f) == model.CapNone {
				v.Unsupported = append(v.Unsupported, f.String())
			}
		}
		sort.Strings(v.Estimated)
		sort.Strings(v.Unsupported)

		v.ContextPctMax = roundOpt(v.ContextPctMax, 1)
		v.CostUSDTotal = roundOpt(v.CostUSDTotal, 4)
		if v.LastActivity != nil {
			t := stamp(*v.LastActivity)
			v.LastActivity = &t
		}

		switch view.Status {
		case hud.StatusNotDetected:
			doc.Fleet.VendorsNotDetected++
		case hud.StatusUnreadable:
			doc.Fleet.VendorsUnreadable++
		case hud.StatusDrifted:
			doc.Fleet.VendorsDrifted++
		default:
			doc.Fleet.VendorsWatching++
		}

		if v.ContextPctMax != nil {
			fleetCtx = maxFloat(fleetCtx, *v.ContextPctMax)
		}
		if v.CostUSDTotal != nil {
			fleetCost = addFloat(fleetCost, *v.CostUSDTotal)
		}
		if v.LastActivity != nil {
			fleetLast = laterTime(fleetLast, *v.LastActivity)
		}
		doc.Vendors = append(doc.Vendors, v)
	}

	doc.Fleet.ContextPctMax = roundOpt(fleetCtx, 1)
	doc.Fleet.CostUSDTotal = roundOpt(fleetCost, 4)
	doc.Fleet.LastActivity = fleetLast
	return doc
}

// quota pairs a vendor's relayed windows with the time they were relayed.
type quota struct {
	windows []model.QuotaWindow
	at      time.Time
}

// Encode serializes the document. It ends with a newline on both paths, so the
// output is a well-formed line in a pipe and a well-formed file on redirect.
//
// The compact form is one line, for a reader that streams. The default is
// indented, because a human reads this output too — the first reader of any
// agent-facing format is the person wiring the agent up.
func Encode(doc Document, compact bool) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// No HTML escaping: a vendor's OS error message can carry & or <, and
	// & in an error string is noise for every reader of this document.
	enc.SetEscapeHTML(false)
	if !compact {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// stamp normalizes a timestamp to UTC seconds.
//
// UTC because a document read on another machine must not need the writer's
// zone to be comparable; seconds because sub-second precision on a scan that
// walks the filesystem is noise, and it makes the goldens stable.
func stamp(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }

func maxFloat(cur *float64, v float64) *float64 {
	if cur == nil || v > *cur {
		return &v
	}
	return cur
}

func addFloat(cur *float64, v float64) *float64 {
	if cur == nil {
		return &v
	}
	sum := *cur + v
	return &sum
}

func maxInt(cur *int, v int) *int {
	if cur == nil || v > *cur {
		return &v
	}
	return cur
}

func laterTime(cur *time.Time, t time.Time) *time.Time {
	if cur == nil || t.After(*cur) {
		return &t
	}
	return cur
}

// roundOpt rounds a value in place and keeps absence absent. Rounding happens
// once, at the edge, so a sum of many small costs does not print the binary
// float's tail — and never turns a nil into a 0.
func roundOpt(v *float64, places int) *float64 {
	if v == nil {
		return nil
	}
	return roundPtr(*v, places)
}

func roundPtr(v float64, places int) *float64 {
	f := math.Pow(10, float64(places))
	r := math.Round(v*f) / f
	return &r
}
