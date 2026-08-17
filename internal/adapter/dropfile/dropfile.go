// Package dropfile adapts the drop-file relay to model.Session: a documented
// format any tool can write, so a vendor telltale ships no adapter for can
// still put a row on the HUD (docs/design.md §7.23, spec in docs/dropfile.md).
//
// It is the extensibility seam this repo chose INSTEAD of a plugin runtime. A
// plugin runtime would run a stranger's code inside a process whose whole
// contract is "reads, never writes, no network, no credentials"; a drop file
// buys the same reach with a reader that opens one JSON document and can do
// nothing else.
//
// # This adapter reads. It adds no write exception
//
// The three sanctioned writes under ~/.telltale/ (CLAUDE.md, design.md §7.15
// and §7.16) are unchanged. Nothing here writes, creates, or removes a file —
// the directory is the operator's to fill, and telltale only opens what is
// already in it. A missing directory is the ordinary case and reports the
// vendor absent.
//
// # The honesty problem this format has and no other adapter has
//
// Every other adapter reads a store its vendor wrote as a byproduct of doing
// its own work. Nobody had a reason to write a flattering number into it, and
// the adapter's package doc can say which live corpus each field was grepped
// out of. A drop file is the opposite: it is written FOR telltale, by whoever,
// and every value in it is that writer's claim. telltale measured that a file
// exists, when it was last written, and what it says. It did not measure the
// session.
//
// So the row must never be readable as a measured row (ADR-001, §4a.1). Three
// mechanisms carry that, and none of them is a colour:
//
//  1. The vendor id is model.VendorSelfReported for every drop file, and the
//     format has no field that could change it. The HUD's identity column
//     reads "SR" and the header count reads "self-reported N". A drop file
//     cannot draw a row that claims to be Claude, because the claim has
//     nowhere to live.
//  2. The claimed tool leads the row's label, so a reader sees WHO claimed it
//     on the row itself rather than in a pane they may never open.
//  3. The one number telltale can check, it checks. See the mtime clamp below.
//
// The estimate marker "~" is deliberately NOT used. It means the adapter
// computed a value from something that is not the value (model.CapDerived),
// and the snapshot's `estimated` array says so in those words. This adapter
// computes nothing — it reads the number the writer wrote, verbatim. Marking
// these rows "~" would state a falsehood about HOW the number arrived and
// would collapse two different provenances into one spelling, which is the
// failure §4a.1 exists to prevent. Provenance is a property of the whole row,
// and it is marked once, on the row, not smeared over every cell.
//
// # The mtime clamp
//
// A writer that stops writing leaves a file behind that keeps asserting
// whatever it last said. If that file claims a last_activity of "now", the row
// would render live forever on a dead session.
//
// telltale cannot check cost or context percentage against anything. It CAN
// check last_activity, because a file cannot have activity newer than its own
// last write, and the mtime is telltale's own measurement rather than the
// writer's claim. So a last_activity ahead of the file's mtime is replaced by
// the mtime and the substitution is recorded in Diagnostics. The value stays
// present because a measured mtime is a better answer than absence, and it is
// not marked Degraded — Degraded fields must be absent (§4a.2).
//
// # Staleness
//
// A file whose mtime is older than maxAge, or is from the future beyond
// futureSkew, is not read at all and draws no row. This is the rule
// internal/quotacache already applies to a relayed reading, for the reason
// stated there: the honest display for "no reading" is absence, never an error
// banner. Between those bounds the reading's age travels with it, as an Extra
// past ageShown — §7.12's basis rule, and the same five minutes the account
// relay uses.
//
// # What this format deliberately cannot express
//
// Each omission is a decision, not a gap waiting to be filled:
//
//   - quota: an account property, which the HUD sources from the statusline
//     relay (§7.15). A session-shaped file has no account to speak for, and
//     §7.1 forbids a per-session quota claim outright.
//   - liveness: §4a.4 allows a hint only from a positive vendor signal the HUD
//     cannot see. A writer that could set its own liveness could pin its row
//     to the top of the grid forever, and no reader could check it. It is the
//     field where a false claim is most tempting and least detectable, so the
//     format has no field for it. Liveness is classified by the HUD from
//     last_activity, which the mtime clamp bounds.
//   - tokens: model.TokenCounts feeds a fleet spend sum (§7.17). A claimed
//     count would be summed beside measured ones and the total would carry no
//     mark at all.
package dropfile

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// Vendor is the stable id for rows this adapter produces. Every drop file gets
// it; see model.VendorSelfReported for why one id serves all of them.
const Vendor = model.VendorSelfReported

// FormatVersion is the drop-file contract number this adapter speaks.
//
// A document must carry it. A document carrying a different number is skipped
// whole rather than read leniently: the partial-read rule (§4a.5) degrades a
// FIELD whose value could not be read, and this is not that. A contract number
// telltale does not know means the field names may no longer mean what this
// adapter thinks they mean, and reading them anyway would invent every value
// at once.
const FormatVersion = 1

const (
	// maxAge is where a drop file stops drawing a row. It is quotacache's
	// backstop and the same duration, because it answers the same question: a
	// day-old claim about a coding session is archaeology, not state.
	maxAge = 24 * time.Hour

	// futureSkew tolerates clock jitter and rejects a clock we cannot reason
	// about. It matches the relay caches rather than the adapters' 2s, because
	// what is being judged here is a file's mtime against the local clock —
	// the relay's question, not a transcript timestamp's.
	futureSkew = 5 * time.Minute

	// ageShown is when the reading starts carrying its age into the detail
	// pane. Below it a live writer is firing often enough that an age line
	// would be noise; from it on, the age IS the honesty (§7.12).
	ageShown = 5 * time.Minute
)

// maxStringBytes bounds every string this format accepts before it reaches a
// Session. The grid truncates for display anyway, so this is not a layout
// guard — it stops one file from making a scan hold an unbounded string, and
// it is a byte bound rather than a rune bound because it is defending memory.
const maxStringBytes = 4096

// maxFileBytes bounds the document itself. A drop file is a handful of keys;
// anything larger is a mistake or a stunt, and Read must not page it in.
const maxFileBytes = 64 << 10

// Adapter reads drop files. It holds no mutable state and is safe for
// concurrent use.
type Adapter struct {
	root string
}

// New returns an adapter rooted at ~/.telltale/dropfile.
//
// TELLTALE_DROPFILE_DIR overrides the location. It exists for tests and for an
// operator whose home is not where telltale's other state lives; it is read
// once here rather than on the scan path, so Discover and Read consult no
// environment.
func New() *Adapter {
	if dir := os.Getenv("TELLTALE_DROPFILE_DIR"); dir != "" {
		return &Adapter{root: dir}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// An unresolvable home is the same as "no drop files": Discover
		// reports the vendor absent and the HUD shows nothing at all.
		return &Adapter{}
	}
	return &Adapter{root: filepath.Join(home, ".telltale", "dropfile")}
}

// NewWithRoot points the adapter at an explicit directory. Tests use it.
func NewWithRoot(root string) *Adapter { return &Adapter{root: root} }

// Root is the directory this adapter watches, for the HUD's empty state.
func (a *Adapter) Root() string { return a.root }

func (a *Adapter) Vendor() model.VendorID { return Vendor }

// SelfReported declares that every row from this adapter is a claim its writer
// made, not a reading telltale took. hud.Scan carries it onto the vendor line
// and internal/snapshot emits it as `self_reported`.
//
// It is an adapter-level statement rather than a per-session flag because it
// is true of every row this adapter will ever produce, which is exactly the
// shape model.Capabilities already has: a static promise about the SOURCE.
func (a *Adapter) SelfReported() bool { return true }

// Capabilities is static, and here it describes the FORMAT rather than a
// vendor's store — for this adapter those are the same thing, because the
// format is the only source there is.
//
// Everything is Reported. Nothing is Derived: this adapter performs no
// arithmetic on any value, it parses what the writer wrote. That is a
// statement about mechanism and not about trust, and it is why these rows
// carry no "~" — see the package doc.
//
// FieldSubagents is Reported here although model.Session's own doc calls it a
// derived field. That doc describes the adapters that must COUNT transcripts
// against a recency boundary; a drop file writes the number down, so reading
// it is a read. The capability names the source, not the field's usual fate.
func (a *Adapter) Capabilities() model.Capabilities {
	return model.Capabilities{
		Reported: model.NewFieldSet(
			model.FieldName,
			model.FieldModel,
			model.FieldWorkspace,
			model.FieldContextPercent,
			model.FieldCost,
			model.FieldLastActivity,
			model.FieldSubagents,
		),
	}
}

// Discover lists drop files. Directory listing and stat only.
//
// The walk is flat: root/*.json, no recursion. A drop file is a file the
// operator put in one directory, and a tree would invite a tool to bury state
// under it that telltale would then have to decide whether to read.
//
// Expiry is applied here rather than in Read because mtime is already in hand
// at stat time, and a row that will not render should not cost a file open.
func (a *Adapter) Discover(ctx context.Context) ([]model.SessionRef, error) {
	if a.root == "" {
		return nil, model.ErrVendorAbsent
	}
	entries, err := os.ReadDir(a.root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, model.ErrVendorAbsent
	}
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var refs []model.SessionRef
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if e.IsDir() {
			continue
		}
		id, ok := sessionIDFromFile(e.Name())
		if !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			// The directory mutates while it is being walked. One file that
			// vanishes mid-stat must not drop the others.
			continue
		}
		if expired(info.ModTime(), now) {
			continue
		}
		refs = append(refs, model.SessionRef{
			Vendor:       Vendor,
			ID:           id,
			Locator:      filepath.Join(a.root, e.Name()),
			LastActivity: model.TimePtr(info.ModTime()),
		})
	}
	// Deterministic order out of a directory listing whose order is the
	// filesystem's business.
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	return refs, nil
}

// expired reports whether a file's mtime puts it outside the window in which
// it may draw a row.
func expired(mtime, now time.Time) bool {
	if mtime.IsZero() {
		return true
	}
	if mtime.After(now.Add(futureSkew)) {
		return true
	}
	return now.Sub(mtime) > maxAge
}

// sessionIDFromFile turns <name>.json into the row's id.
//
// The FILE NAME is the identity, never a field inside the document. The
// operator's filesystem decides what a row is called, which means a drop file
// cannot rename itself onto another file's row, and the id is stable for as
// long as the file is — exactly what model.Session.ID asks for.
func sessionIDFromFile(name string) (string, bool) {
	if !strings.HasSuffix(name, ".json") {
		return "", false
	}
	stem := strings.TrimSuffix(name, ".json")
	// A temp file mid-rename (quotacache writes "<vendor>-*.tmp" in its own
	// directory) and a dotfile are not drop files.
	if stem == "" || strings.HasPrefix(stem, ".") {
		return "", false
	}
	return stem, true
}

// document is the wire form, and it is the allowlist.
//
// Every field is a json.RawMessage so that one key of the wrong type cannot
// fail the whole decode — encoding/json only errors here on JSON that is
// malformed outright. Each value is then parsed on its own, so a bad
// context_pct degrades context_pct and nothing else (§4a.5's partial-read
// rule).
//
// A key with no field here has no destination and is dropped by encoding/json
// before this adapter sees it. That is the same allowlist-is-the-struct
// mechanism internal/cursorhook uses against a payload carrying reply text and
// an email address, and it is what a planted-credential test asserts.
type document struct {
	SchemaVersion json.RawMessage `json:"schema_version"`
	Tool          json.RawMessage `json:"tool"`
	Name          json.RawMessage `json:"name"`
	Workspace     json.RawMessage `json:"workspace"`
	Model         json.RawMessage `json:"model"`
	ContextPct    json.RawMessage `json:"context_pct"`
	CostUSD       json.RawMessage `json:"cost_usd"`
	LastActivity  json.RawMessage `json:"last_activity"`
	Subagents     json.RawMessage `json:"subagents"`
}

// state is what one field's parse concluded. The three outcomes are the
// distinction this whole repo is built on, at field scale.
type state int

const (
	// absent: the key was omitted, or it was explicitly null. The format
	// accepts BOTH spellings and means one thing by them — a writer cannot be
	// made to emit every optional key, and rejecting an omitted key would fail
	// a document over a field it simply had no value for. What must never
	// collapse is absent against ZERO, and zero is a number in both spellings.
	absent state = iota
	// present: a value was read.
	present
	// bad: the key was there and the value could not be used — wrong JSON
	// type, or a number outside the range the model accepts. The field
	// degrades; the row survives.
	bad
)

// Read parses one drop file into the normalized model.
func (a *Adapter) Read(ctx context.Context, ref model.SessionRef) (*model.Session, error) {
	f, err := os.Open(ref.Locator)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, model.ErrSessionGone
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxFileBytes {
		return nil, errors.New("drop file is larger than the format allows")
	}

	now := time.Now()
	mtime := info.ModTime()
	// Discover already applied this, but a file can be rewritten between the
	// stat and the open, and a row must not render off a reading the expiry
	// rule would have refused.
	if expired(mtime, now) {
		return nil, model.ErrSessionGone
	}

	raw, err := os.ReadFile(ref.Locator)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, model.ErrSessionGone
		}
		return nil, err
	}

	var doc document
	if err := json.Unmarshal(raw, &doc); err != nil {
		// Not a partial read: nothing in this document can be trusted to be
		// the field it appears to be. No row.
		return nil, errors.New("drop file is not a JSON object this format can read")
	}

	if v, st := readInt(doc.SchemaVersion); st != present || v != FormatVersion {
		return nil, errors.New("drop file carries no schema_version this adapter speaks")
	}
	tool, st := readString(doc.Tool)
	if st != present || tool == "" {
		// A claim with no claimant cannot be attributed, and attribution is
		// the whole honesty requirement for this row. There is nothing to
		// degrade to.
		return nil, errors.New("drop file names no tool, so its row could not say who claimed it")
	}

	s := &model.Session{Vendor: Vendor, ID: ref.ID, ObservedAt: now}

	// The claimed tool leads the label. A reader must be able to tell one
	// self-reported row from another without opening the detail pane, and the
	// tag column cannot do it — every drop file shares "SR" by design.
	name, nameState := readString(doc.Name)
	switch {
	case nameState == present && name != "":
		s.Name = model.Ptr(tool + ": " + name)
	default:
		s.Name = model.Ptr(tool)
	}
	if nameState == bad {
		s.Degraded = s.Degraded.With(model.FieldName)
		s.Diagnostics = append(s.Diagnostics, "name is not a string")
	}

	if v, st := readString(doc.Workspace); st == present && v != "" {
		s.WorkspaceDir = model.Ptr(v)
	} else if st == bad {
		s.Degraded = s.Degraded.With(model.FieldWorkspace)
		s.Diagnostics = append(s.Diagnostics, "workspace is not a string")
	}

	if v, st := readString(doc.Model); st == present && v != "" {
		s.Model = &model.Model{ID: v}
	} else if st == bad {
		s.Degraded = s.Degraded.With(model.FieldModel)
		s.Diagnostics = append(s.Diagnostics, "model is not a string")
	}

	switch v, st := readFloat(doc.ContextPct); {
	case st == present && v >= 0 && v <= 100:
		s.ContextPercent = model.PercentPtr(v)
	case st == present:
		// Out of range is dropped, never clamped: a clamped percentage is
		// invented data (model.Percent's doc, and Validate rejects it).
		s.Degraded = s.Degraded.With(model.FieldContextPercent)
		s.Diagnostics = append(s.Diagnostics, "context_pct is outside 0..100")
	case st == bad:
		s.Degraded = s.Degraded.With(model.FieldContextPercent)
		s.Diagnostics = append(s.Diagnostics, "context_pct is not a number")
	}

	switch v, st := readFloat(doc.CostUSD); {
	case st == present && v >= 0:
		s.Cost = model.USDPtr(v)
	case st == present:
		s.Degraded = s.Degraded.With(model.FieldCost)
		s.Diagnostics = append(s.Diagnostics, "cost_usd is negative")
	case st == bad:
		s.Degraded = s.Degraded.With(model.FieldCost)
		s.Diagnostics = append(s.Diagnostics, "cost_usd is not a number")
	}

	switch v, st := readInt(doc.Subagents); {
	case st == present && v >= 0:
		// Zero is a measurement here exactly as it is everywhere else: the
		// writer said it is fanning out to nothing.
		s.Subagents = model.Ptr(v)
	case st == present:
		s.Degraded = s.Degraded.With(model.FieldSubagents)
		s.Diagnostics = append(s.Diagnostics, "subagents is negative")
	case st == bad:
		s.Degraded = s.Degraded.With(model.FieldSubagents)
		s.Diagnostics = append(s.Diagnostics, "subagents is not a whole number")
	}

	a.applyLastActivity(s, doc.LastActivity, mtime, now)

	s.Extras = append(s.Extras, model.Extra{Label: "reported by", Value: tool})
	s.Extras = append(s.Extras, model.Extra{Label: "source", Value: "self-reported drop file"})
	if age := now.Sub(mtime); age >= ageShown {
		s.Extras = append(s.Extras, model.Extra{Label: "reported", Value: shortAge(age) + " ago"})
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

// applyLastActivity resolves the row's activity clock, and it is the one place
// this adapter checks a claim rather than reading it.
//
// A file cannot have activity newer than its own last write. The mtime is
// telltale's measurement; the claim is the writer's. So the claim is accepted
// only up to the mtime, and past it the mtime wins and says so.
func (a *Adapter) applyLastActivity(s *model.Session, rawTS json.RawMessage, mtime, now time.Time) {
	claimed, st := readTime(rawTS)
	switch st {
	case bad:
		s.Degraded = s.Degraded.With(model.FieldLastActivity)
		s.Diagnostics = append(s.Diagnostics, "last_activity is not an RFC3339 timestamp")
		return
	case absent:
		// No claim at all. The file's own mtime is a real reading of when this
		// row last changed, and it is the only one there is.
		s.LastActivity = model.TimePtr(mtime)
		return
	}
	if claimed.After(mtime.Add(futureSkew)) {
		s.LastActivity = model.TimePtr(mtime)
		s.Diagnostics = append(s.Diagnostics,
			"last_activity claimed ahead of the file's own mtime; the mtime is used instead")
		return
	}
	if claimed.After(now.Add(futureSkew)) {
		s.LastActivity = model.TimePtr(mtime)
		s.Diagnostics = append(s.Diagnostics,
			"last_activity claimed ahead of this clock; the mtime is used instead")
		return
	}
	s.LastActivity = model.TimePtr(claimed)
}

// isAbsent folds the format's two spellings of absence into one answer. A key
// that was omitted arrives as a nil RawMessage; a key written as null arrives
// as the four bytes "null".
func isAbsent(raw json.RawMessage) bool {
	t := strings.TrimSpace(string(raw))
	return len(raw) == 0 || t == "null"
}

func readString(raw json.RawMessage) (string, state) {
	if isAbsent(raw) {
		return "", absent
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", bad
	}
	if len(v) > maxStringBytes {
		return "", bad
	}
	return v, present
}

func readFloat(raw json.RawMessage) (float64, state) {
	if isAbsent(raw) {
		return 0, absent
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, bad
	}
	// NaN and the infinities cannot survive a JSON round trip, so anything
	// that decoded is finite. A string holding a number is still bad: this
	// format does not guess at types.
	return v, present
}

func readInt(raw json.RawMessage) (int, state) {
	if isAbsent(raw) {
		return 0, absent
	}
	var v json.Number
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, bad
	}
	n, err := v.Int64()
	if err != nil {
		// A fractional count is not a count.
		return 0, bad
	}
	return int(n), present
}

func readTime(raw json.RawMessage) (time.Time, state) {
	s, st := readString(raw)
	if st != present {
		return time.Time{}, st
	}
	if s == "" {
		return time.Time{}, bad
	}
	if ts, err := time.Parse(time.RFC3339Nano, s); err == nil && !ts.IsZero() {
		return ts, present
	}
	if ts, err := time.Parse(time.RFC3339, s); err == nil && !ts.IsZero() {
		return ts, present
	}
	return time.Time{}, bad
}

// shortAge renders a reading's age for the detail pane. Coarse on purpose: the
// question it answers is "should I trust this", and a seconds-precise age
// invites the reader to treat a claimed row as a live one.
func shortAge(d time.Duration) string {
	switch {
	case d >= time.Hour:
		return itoa(int64(d/time.Hour)) + "h"
	case d >= time.Minute:
		return itoa(int64(d/time.Minute)) + "m"
	default:
		return itoa(int64(d/time.Second)) + "s"
	}
}

func itoa(n int64) string {
	if n <= 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// compile-time contract check.
var _ model.Adapter = (*Adapter)(nil)
