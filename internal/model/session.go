// Package model defines the vendor-neutral session schema that every telltale
// adapter produces and both renderers consume. It is the one contract the rest
// of the program hangs on (docs/design.md §4).
//
// Two constraints govern everything in this file:
//
//   - Honest gauge (decisions/001): a displayed value must have been read from
//     vendor data. Absence is therefore a first-class state and must survive the
//     trip from adapter to renderer, so every optional value is a pointer — nil
//     means "no value", and a zero value always means the vendor said zero.
//     There is deliberately no "unset" sentinel number anywhere in this package.
//   - Stdlib only. The statusline path (ADR-002) has a process-spawn-bound
//     latency budget and must never link a TUI framework. Bubble Tea and
//     Lipgloss are approved for the HUD render layer, not for this package or
//     for any adapter.
//
// Absence has two distinct causes and the HUD renders them differently, so the
// schema separates them:
//
//	absent now    the adapter can source the field, but this session has no
//	              value for it right now (Claude's rate_limits on an API-key
//	              login) — nil pointer, capability declared. Renders "—".
//	can't know    the vendor exposes no such thing, ever (Codex has no quota
//	              window) — nil pointer, capability NOT declared. The HUD drops
//	              the column for that vendor rather than printing a row of
//	              dashes that implies missing data.
//
// A field's capability is static per adapter (Capabilities); a field's value is
// per read (Session). The pair is what lets the HUD tell those two apart.
package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// VendorID identifies a coding agent. It is stable, lowercase ASCII, matches
// the adapter's package name, and appears in config keys and eval fixtures —
// treat it as an API, not a label. Display names belong in the renderer.
type VendorID string

const (
	VendorClaude VendorID = "claude"
	VendorCodex  VendorID = "codex"
	VendorGemini VendorID = "gemini"
	// VendorAntigravity is Antigravity CLI. Its id is the binary's own name,
	// `agy`, rather than the product's: it is what the user types, what the
	// --vendor flag takes and what the header count prints, and a vendor
	// column reading "antigravity" would cost eleven cells to say what three
	// say (decisions/006). The adapter package is named for the product.
	VendorAntigravity VendorID = "agy"
)

// Field enumerates the optional fields of a Session: exactly those a vendor may
// or may not be able to source. Vendor, ID and ObservedAt are not here because
// an adapter that cannot produce them has no session to report at all.
//
// The constants are an internal representation; the names returned by String
// are the stable identifiers used in Capabilities documentation and in eval
// fixtures. Add fields at the end; never renumber.
type Field uint8

const (
	FieldName Field = iota
	FieldModel
	FieldWorkspace
	FieldContextPercent
	FieldCost
	FieldQuota
	FieldLastActivity
	FieldLiveness
	FieldSubagents
	fieldCount
)

var fieldNames = [fieldCount]string{
	FieldName:           "name",
	FieldModel:          "model",
	FieldWorkspace:      "workspace",
	FieldContextPercent: "context_pct",
	FieldCost:           "cost",
	FieldQuota:          "quota",
	FieldLastActivity:   "last_activity",
	FieldLiveness:       "liveness",
	FieldSubagents:      "subagents",
}

// AllFields lists every Field in declaration order. Renderers iterate this so a
// newly added field cannot be silently skipped.
var AllFields = func() []Field {
	fs := make([]Field, 0, fieldCount)
	for f := Field(0); f < fieldCount; f++ {
		fs = append(fs, f)
	}
	return fs
}()

func (f Field) String() string {
	if f >= fieldCount {
		return fmt.Sprintf("field(%d)", uint8(f))
	}
	return fieldNames[f]
}

// ParseField maps a stable field name back to its Field. Fixtures and config
// carry names, never ordinals.
func ParseField(name string) (Field, bool) {
	for f := Field(0); f < fieldCount; f++ {
		if fieldNames[f] == name {
			return f, true
		}
	}
	return 0, false
}

// FieldSet is a set of Fields. It is a bitmask because sets are compared and
// intersected on every render pass; fieldCount is small and fixed.
type FieldSet uint32

func NewFieldSet(fs ...Field) FieldSet {
	var s FieldSet
	for _, f := range fs {
		s = s.With(f)
	}
	return s
}

func (s FieldSet) With(f Field) FieldSet { return s | 1<<f }
func (s FieldSet) Without(f Field) FieldSet {
	return s &^ (1 << f)
}
func (s FieldSet) Has(f Field) bool          { return s&(1<<f) != 0 }
func (s FieldSet) Union(o FieldSet) FieldSet { return s | o }
func (s FieldSet) Intersect(o FieldSet) FieldSet {
	return s & o
}

// Minus returns the fields in s that are not in o.
func (s FieldSet) Minus(o FieldSet) FieldSet { return s &^ o }

func (s FieldSet) Empty() bool { return s == 0 }

func (s FieldSet) String() string {
	if s == 0 {
		return "none"
	}
	var names []string
	for _, f := range AllFields {
		if s.Has(f) {
			names = append(names, f.String())
		}
	}
	return strings.Join(names, "|")
}

// Capability is what an adapter can do with one field, for every session of
// that vendor. It answers "can't know" versus "absent now" before any session
// is read, which is what lets the HUD lay out columns.
type Capability uint8

const (
	// CapNone: the vendor exposes nothing that maps to this field. The HUD
	// omits it for this vendor. A Session carrying a value for a CapNone field
	// is a contract violation and Validate rejects it.
	CapNone Capability = iota
	// CapReported: the value comes from vendor output verbatim (modulo unit
	// conversion). Rendered as a plain gauge.
	CapReported
	// CapDerived: the adapter computes the value from vendor data that is not
	// the value itself — summing transcript token counts into a context
	// percentage, for example. Honest-gauge rule: derived values render with an
	// estimate marker, never as if the vendor reported them.
	CapDerived
)

func (c Capability) String() string {
	switch c {
	case CapReported:
		return "reported"
	case CapDerived:
		return "derived"
	default:
		return "none"
	}
}

// Capabilities is an adapter's static declaration of what it can source. The
// two sets are disjoint: a field is reported, derived, or unavailable.
//
// Declaring a capability is a promise about the source, not about any given
// session — a field may still be absent in a particular read.
type Capabilities struct {
	Reported FieldSet
	Derived  FieldSet
}

func (c Capabilities) Capability(f Field) Capability {
	switch {
	case c.Reported.Has(f):
		return CapReported
	case c.Derived.Has(f):
		return CapDerived
	default:
		return CapNone
	}
}

// Known returns every field the adapter can source by any means.
func (c Capabilities) Known() FieldSet { return c.Reported.Union(c.Derived) }

// Validate enforces the disjointness invariant. Adapters should call it in a
// package-level test; the eval harness calls it for every registered adapter.
func (c Capabilities) Validate() error {
	if both := c.Reported.Intersect(c.Derived); !both.Empty() {
		return fmt.Errorf("model: fields declared both reported and derived: %s", both)
	}
	return nil
}

// Percent is a percentage on a 0–100 scale, as vendors report it (23.5 means
// 23.5%). Adapters convert to this scale once, at the edge; nothing downstream
// rescales. A vendor value outside the range is a broken read: record the field
// as degraded and leave it nil rather than clamping, because a clamped value is
// invented data.
type Percent float64

// USD is a cost in US dollars. Only USD is modeled in v1; an adapter whose
// vendor reports another currency must not populate this field (CapNone)
// rather than convert at an unsourced rate.
type USD float64

// Model identifies the model driving a session. At least one of the two fields
// is non-empty, mirroring the statusline rule of falling back to the id when a
// display name is missing.
type Model struct {
	ID          string
	DisplayName string
}

// Name returns the preferred display string, and whether there is one at all.
func (m *Model) Name() (string, bool) {
	if m == nil {
		return "", false
	}
	if m.DisplayName != "" {
		return m.DisplayName, true
	}
	if m.ID != "" {
		return m.ID, true
	}
	return "", false
}

// QuotaWindow is one labeled usage window (Claude's five_hour / seven_day, or
// whatever a vendor exposes). Windows are a slice rather than named fields
// because the set is vendor-defined: an adapter emits only the windows its
// vendor actually has, in display order, shortest first.
//
// Presence rules, which differ by level and are both load-bearing:
//
//   - a window the vendor does not have is absent from the slice entirely;
//   - a window that exists but has no usage figure yet is present with a nil
//     UsedPercent and renders "—". It is never rendered 0%.
type QuotaWindow struct {
	// ID is the stable machine key, snake_case, unique within the session
	// (e.g. "five_hour"). Fixtures and thresholds key off it.
	ID string
	// Label is the short display string (e.g. "5h"). Budgeted for the
	// statusline: keep it to four cells or fewer.
	Label string
	// UsedPercent is the fraction of the window consumed. Nil when the vendor
	// has not reported one.
	UsedPercent *Percent
	// ResetsAt is when the window rolls over. Nil when unknown — the countdown
	// hides rather than showing a guess.
	ResetsAt *time.Time
}

// Liveness is how alive a session is. The zero value is Unknown, so a Session
// that carries no liveness evidence cannot accidentally claim one.
type Liveness uint8

const (
	LivenessUnknown Liveness = iota
	LivenessLive
	LivenessIdle
	LivenessStale
)

func (l Liveness) String() string {
	switch l {
	case LivenessLive:
		return "live"
	case LivenessIdle:
		return "idle"
	case LivenessStale:
		return "stale"
	default:
		return "unknown"
	}
}

// LivenessThresholds are the age boundaries used to classify a session from its
// last activity. They live in the HUD layer, not in adapters, so that every
// vendor's rows are classified by the same rule and are comparable side by side.
type LivenessThresholds struct {
	// Live: age at or below this is LivenessLive.
	Live time.Duration
	// Idle: age at or below this (and above Live) is LivenessIdle. Above it is
	// LivenessStale.
	Idle time.Duration
}

// DefaultLivenessThresholds is the v1 default.
//
// Live is 2 minutes because a working agent can go a long single turn without
// writing anything to disk; a boundary shorter than the longest quiet stretch
// of real work would flap between live and idle mid-task. Above 15 minutes a
// session is almost always one the user walked away from, so it sorts to the
// bottom of the HUD rather than competing for attention. Both numbers are
// defaults, not constants: the HUD may expose them and the eval harness pins
// renders against explicit values.
var DefaultLivenessThresholds = LivenessThresholds{
	Live: 2 * time.Minute,
	Idle: 15 * time.Minute,
}

// Extra is a vendor-specific display-only key/value pair — an escape hatch that
// exists so an adapter with something extra to show does not stuff it into a
// field that means something else.
//
// Extras are never gauged: no thresholds, no colors, no sorting. They appear in
// the detail pane only. If an extra deserves a gauge, it deserves a Field.
type Extra struct {
	Label string
	Value string
}

// Session is one agent session, normalized. Adapters construct it; renderers
// only read it.
//
// Every optional field is a pointer, and nil always means "no value" — never
// "zero". Combined with the adapter's Capabilities, nil resolves to either
// "absent now" (capability declared) or "can't know" (not declared).
type Session struct {
	// Vendor and ID identify the session. ID is opaque, stable for the life of
	// the session, and unique within the vendor. Both are required: without
	// them there is no row to render and no way to match a row across polls.
	Vendor VendorID
	ID     string

	// ObservedAt is when the adapter read this snapshot — not when the session
	// last did something (that is LastActivity). The HUD needs it to mark a row
	// whose data has gone stale because polling failed; presenting an old
	// snapshot as current is the honest-gauge rule's exact failure mode.
	// Required.
	ObservedAt time.Time

	// Name is a human label for the session, if the vendor has one.
	// May contain arbitrary model-authored text including U+2028/U+2029
	// (docs/design.md §4) — renderers must not assume it is a single line.
	Name *string

	// Model drives the session.
	Model *Model

	// WorkspaceDir is the session's working directory as an absolute
	// native-format path. Renderers usually show WorkspaceName instead.
	WorkspaceDir *string

	// ContextPercent is the share of the model's context window in use.
	ContextPercent *Percent

	// Cost is the session's cost so far.
	Cost *USD

	// Quota holds the vendor's usage windows in display order, shortest first.
	// Empty means the vendor exposes none (or none yet); see QuotaWindow.
	Quota []QuotaWindow

	// LastActivity is when the session last did something observable. This is
	// the input to liveness classification; it is not itself a claim about
	// whether the session is alive.
	LastActivity *time.Time

	// Subagents is how many of the session's sub-agent transcripts the adapter
	// found recently modified — a fan-out in progress, counted rather than
	// inferred.
	//
	// It is a COUNT, and the two absences are the usual pair: nil means the
	// adapter could not perform the count (and says why in Diagnostics); zero
	// means it counted and found none. Zero is a measurement and must survive
	// as one — the HUD simply draws no chip for it, the same way an empty
	// gauge track means zero rather than "no data".
	//
	// It is DERIVED, not reported: no vendor writes this number down. The
	// adapter computes it from a directory listing plus a recency boundary,
	// and that boundary is the inference the estimate marker exists to expose.
	Subagents *int

	// LivenessHint is the adapter's own verdict, and it overrides the
	// age-based classification. Set it ONLY from a positive vendor signal that
	// the HUD cannot see — a turn-started/turn-ended event, or a session the
	// vendor has recorded as ended (LivenessStale even though LastActivity is
	// seconds old). Never derive it from the age of LastActivity: that is the
	// HUD's job, and duplicating it in adapters makes vendors incomparable.
	// "A process exists" is not evidence of liveness and must not become a hint.
	LivenessHint *Liveness

	// Derived marks fields whose value in THIS snapshot was computed by the
	// adapter rather than reported by the vendor. It must be a subset of the
	// adapter's declared Capabilities.Derived, and every marked field must
	// actually carry a value. Renderers show these with an estimate marker.
	Derived FieldSet

	// Degraded marks fields the adapter tried and failed to read — a truncated
	// JSONL record, an unparseable number. Degraded fields must be absent.
	//
	// Degraded and plain-absent render IDENTICALLY as "—": the distinction is
	// diagnostic, surfaced in the detail pane, and must never become a gauge of
	// its own. Otherwise "we failed to read it" starts to look like a value.
	Degraded FieldSet

	// Diagnostics are operator-facing notes explaining degradation. They are
	// never rendered as values. Public-repo boundary: diagnostics must describe
	// structure ("record 41 truncated"), never carry transcript content.
	Diagnostics []string

	// Extras are vendor-specific display-only pairs. See Extra.
	Extras []Extra
}

// Key is the cross-vendor unique identity of a session, used to match rows
// between polls.
func (s *Session) Key() string { return string(s.Vendor) + "/" + s.ID }

// Has reports whether the field carries a displayable value in this snapshot.
// It is the single definition of "present" — renderers and Validate both use it
// so they cannot disagree about what counts as data.
func (s *Session) Has(f Field) bool {
	if s == nil {
		return false
	}
	switch f {
	case FieldName:
		return s.Name != nil && *s.Name != ""
	case FieldModel:
		_, ok := s.Model.Name()
		return ok
	case FieldWorkspace:
		return s.WorkspaceDir != nil && *s.WorkspaceDir != ""
	case FieldContextPercent:
		return s.ContextPercent != nil
	case FieldCost:
		return s.Cost != nil
	case FieldQuota:
		for i := range s.Quota {
			if s.Quota[i].UsedPercent != nil || s.Quota[i].ResetsAt != nil {
				return true
			}
		}
		return false
	case FieldLastActivity:
		return s.LastActivity != nil
	case FieldLiveness:
		// Only a hint counts as a sourced liveness value; age-based
		// classification is computed by the HUD from FieldLastActivity.
		return s.LivenessHint != nil && *s.LivenessHint != LivenessUnknown
	case FieldSubagents:
		// A count of zero is present: the adapter looked and found none. Only
		// a failed or impossible count is absent.
		return s.Subagents != nil
	default:
		return false
	}
}

// Present returns every field carrying a value.
func (s *Session) Present() FieldSet {
	var set FieldSet
	for _, f := range AllFields {
		if s.Has(f) {
			set = set.With(f)
		}
	}
	return set
}

// Age is how long since the session's last observable activity, and whether
// that is knowable at all. A last activity in the future (clock skew between a
// file's mtime and the local clock) clamps to zero rather than going negative.
func (s *Session) Age(now time.Time) (time.Duration, bool) {
	if s == nil || s.LastActivity == nil {
		return 0, false
	}
	d := now.Sub(*s.LastActivity)
	if d < 0 {
		d = 0
	}
	return d, true
}

// Liveness classifies the session.
//
// Ownership rule: the HUD decides, from LastActivity and one shared set of
// thresholds, so that every vendor is judged identically. The adapter's only
// input is a LivenessHint, which it may set solely from a positive vendor
// signal (see the field's doc) and which wins when present.
//
// With neither a hint nor a last-activity timestamp the answer is
// LivenessUnknown, and Unknown renders as absent — never as "stale", which is a
// claim we have no basis for.
func (s *Session) Liveness(now time.Time, th LivenessThresholds) Liveness {
	if s == nil {
		return LivenessUnknown
	}
	if s.LivenessHint != nil && *s.LivenessHint != LivenessUnknown {
		return *s.LivenessHint
	}
	age, ok := s.Age(now)
	if !ok {
		return LivenessUnknown
	}
	switch {
	case age <= th.Live:
		return LivenessLive
	case age <= th.Idle:
		return LivenessIdle
	default:
		return LivenessStale
	}
}

// WorkspaceName is the basename of the workspace directory, for the HUD's
// folder column.
//
// It scans for either separator on every platform on purpose: adapters do
// filesystem work with path/filepath, but a Session can be read from a fixture
// recorded on another OS, and filepath.Base on Unix would return a whole
// Windows path as its own basename. This is display-only string handling, not
// path manipulation.
func (s *Session) WorkspaceName() (string, bool) {
	if s == nil || s.WorkspaceDir == nil {
		return "", false
	}
	dir := strings.TrimRight(*s.WorkspaceDir, `/\`)
	if dir == "" {
		return "", false
	}
	if i := strings.LastIndexAny(dir, `/\`); i >= 0 {
		dir = dir[i+1:]
	}
	if dir == "" {
		return "", false
	}
	return dir, true
}

// Window returns the quota window with the given ID.
func (s *Session) Window(id string) (*QuotaWindow, bool) {
	if s == nil {
		return nil, false
	}
	for i := range s.Quota {
		if s.Quota[i].ID == id {
			return &s.Quota[i], true
		}
	}
	return nil, false
}

// Validate checks a Session against the capabilities its adapter declared. It
// is the machine-checkable form of the honest-gauge rule: an adapter cannot
// quietly emit a value for a field it told the HUD it cannot source, mark a
// value derived without declaring it, or report a field as both present and
// degraded.
//
// The eval harness runs this over every fixture; adapters should run it in
// their own tests. It is not called on the render path.
func (s *Session) Validate(caps Capabilities) error {
	if s == nil {
		return errors.New("model: nil session")
	}
	var errs []error
	if err := caps.Validate(); err != nil {
		errs = append(errs, err)
	}
	if s.Vendor == "" {
		errs = append(errs, errors.New("model: session has no vendor"))
	}
	if s.ID == "" {
		errs = append(errs, errors.New("model: session has no id"))
	}
	if s.ObservedAt.IsZero() {
		errs = append(errs, errors.New("model: session has no ObservedAt (a snapshot must know when it was taken)"))
	}

	present := s.Present()
	if undeclared := present.Minus(caps.Known()); !undeclared.Empty() {
		errs = append(errs, fmt.Errorf("model: values present for fields declared unsupported: %s", undeclared))
	}
	if overreach := s.Derived.Minus(caps.Derived); !overreach.Empty() {
		errs = append(errs, fmt.Errorf("model: fields marked derived but not declared derived: %s", overreach))
	}
	if empty := s.Derived.Minus(present); !empty.Empty() {
		errs = append(errs, fmt.Errorf("model: fields marked derived but carrying no value: %s", empty))
	}
	if both := s.Degraded.Intersect(present); !both.Empty() {
		errs = append(errs, fmt.Errorf("model: fields both present and degraded: %s", both))
	}

	if s.ContextPercent != nil && !validPercent(*s.ContextPercent) {
		errs = append(errs, fmt.Errorf("model: context_pct %v out of range 0..100 (drop it, do not clamp)", float64(*s.ContextPercent)))
	}
	if s.Cost != nil && *s.Cost < 0 {
		errs = append(errs, fmt.Errorf("model: cost %v is negative", float64(*s.Cost)))
	}
	if s.LastActivity != nil && s.LastActivity.IsZero() {
		errs = append(errs, errors.New("model: last_activity is the zero time (absence is nil, not zero)"))
	}
	if s.Subagents != nil && *s.Subagents < 0 {
		errs = append(errs, fmt.Errorf("model: subagents %d is negative", *s.Subagents))
	}

	seen := make(map[string]bool, len(s.Quota))
	for i, w := range s.Quota {
		switch {
		case w.ID == "":
			errs = append(errs, fmt.Errorf("model: quota window %d has no id", i))
		case seen[w.ID]:
			errs = append(errs, fmt.Errorf("model: duplicate quota window id %q", w.ID))
		default:
			seen[w.ID] = true
		}
		if w.Label == "" {
			errs = append(errs, fmt.Errorf("model: quota window %q has no label", w.ID))
		}
		if w.UsedPercent != nil && !validPercent(*w.UsedPercent) {
			errs = append(errs, fmt.Errorf("model: quota window %q used_percent %v out of range 0..100", w.ID, float64(*w.UsedPercent)))
		}
		if w.ResetsAt != nil && w.ResetsAt.IsZero() {
			errs = append(errs, fmt.Errorf("model: quota window %q resets_at is the zero time (absence is nil, not zero)", w.ID))
		}
	}

	for i, e := range s.Extras {
		if e.Label == "" {
			errs = append(errs, fmt.Errorf("model: extra %d has no label", i))
		}
	}
	return errors.Join(errs...)
}

func validPercent(p Percent) bool { return p >= 0 && p <= 100 }

// SessionRef is the cheap identity of a discovered session: enough to decide
// whether to read it, and nothing more. Discover produces these with stat-level
// work only, so the HUD's poll loop can skip sessions that have not changed.
type SessionRef struct {
	Vendor VendorID
	ID     string
	// Locator is vendor-private (a transcript path, a pipe name). It is opaque
	// to the HUD, which passes it back to Read unchanged, and it is never
	// rendered — on a shared machine it can name another user's paths.
	Locator string
	// LastActivity is a cheap freshness hint, typically a file mtime. Nil when
	// the adapter cannot get one without a full read. It is a scheduling hint,
	// not a value: the LastActivity the HUD displays comes from Read.
	LastActivity *time.Time
}

func (r SessionRef) Key() string { return string(r.Vendor) + "/" + r.ID }

// Errors an adapter may return from Discover or Read. Both are conditions the
// HUD handles by showing less, not by showing an error banner.
var (
	// ErrVendorAbsent: the vendor is not installed or its data directory does
	// not exist. The HUD hides the vendor entirely — a user without Codex
	// should not see a Codex error forever.
	ErrVendorAbsent = errors.New("model: vendor not present on this machine")
	// ErrSessionGone: the session vanished between Discover and Read (rotated,
	// deleted). The HUD drops the row silently.
	ErrSessionGone = errors.New("model: session no longer exists")
)

// Adapter is the per-vendor contract (docs/design.md §4). One implementation
// per vendor, in its own package, depending on stdlib only.
//
// Implementations must be safe for concurrent use: the HUD polls vendors in
// parallel. They must not write to vendor state — telltale reads, never
// mutates — and must not make network calls or read credentials.
type Adapter interface {
	// Vendor is the stable id this adapter reports sessions for.
	Vendor() VendorID

	// Capabilities is the static declaration of which normalized fields this
	// vendor can source, and how. It must not vary with what any particular
	// session happens to contain — that is what nil pointers are for. Callers
	// may treat the result as constant and cache it.
	Capabilities() Capabilities

	// Discover finds live and recent sessions from vendor-native data on disk.
	// It must stay cheap (directory listing and stat, no parsing): the HUD
	// calls it on every poll tick. Returns ErrVendorAbsent when the vendor is
	// not installed.
	Discover(ctx context.Context) ([]SessionRef, error)

	// Read parses one discovered session into the normalized model. The
	// returned Session must satisfy Validate against this adapter's
	// Capabilities.
	//
	// Partial failure is not an error: a field the adapter cannot parse is left
	// nil, added to Degraded, and explained in Diagnostics — the row still
	// renders with "—" in that cell. Read returns an error only when there is
	// no session to report at all, and ErrSessionGone when the session
	// disappeared after Discover.
	Read(ctx context.Context, ref SessionRef) (*Session, error)
}

// Convenience constructors for optional fields. Adapters build Sessions from
// parsed vendor data, and `p := 42.0; s.X = &p` at every field is where
// mistakes hide.

func Ptr[T any](v T) *T { return &v }

func PercentPtr(v float64) *Percent    { p := Percent(v); return &p }
func USDPtr(v float64) *USD            { c := USD(v); return &c }
func TimePtr(t time.Time) *time.Time   { return &t }
func LivenessPtr(l Liveness) *Liveness { return &l }

// UnixTimePtr converts a vendor's unix-seconds timestamp (Claude's resets_at)
// to a *time.Time. A zero or negative input is treated as absent rather than as
// 1970, because a vendor writing 0 means "no value", not "the epoch".
func UnixTimePtr(sec int64) *time.Time {
	if sec <= 0 {
		return nil
	}
	t := time.Unix(sec, 0)
	return &t
}
