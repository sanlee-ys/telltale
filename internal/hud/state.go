package hud

import (
	"strings"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// Filter is the vendor filter. It cycles rather than opening a menu: with a
// handful of vendors a cycle is one keystroke and no mode, and the active
// filter is always stated in the footer.
type Filter uint8

const (
	FilterAll Filter = iota
	FilterClaude
	FilterCodex
	FilterGemini
	FilterAntigravity
	FilterCursor
)

func (f Filter) Next() Filter {
	if f >= FilterCursor {
		return FilterAll
	}
	return f + 1
}

func (f Filter) String() string {
	switch f {
	case FilterClaude:
		return "claude"
	case FilterCodex:
		return "codex"
	case FilterGemini:
		return "gemini"
	case FilterAntigravity:
		return string(model.VendorAntigravity)
	case FilterCursor:
		return string(model.VendorCursor)
	default:
		return "all"
	}
}

func (f Filter) Accepts(v model.VendorID) bool {
	switch f {
	case FilterClaude:
		return v == model.VendorClaude
	case FilterCodex:
		return v == model.VendorCodex
	case FilterGemini:
		return v == model.VendorGemini
	case FilterAntigravity:
		return v == model.VendorAntigravity
	case FilterCursor:
		return v == model.VendorCursor
	default:
		return true
	}
}

// SortKey orders the rows. Activity is the default because it puts the
// sessions that are actually doing something on top, which is what removes the
// need for a selection cursor.
type SortKey uint8

const (
	SortActivity SortKey = iota
	SortContext
	SortCost
)

func (s SortKey) Next() SortKey {
	if s >= SortCost {
		return SortActivity
	}
	return s + 1
}

func (s SortKey) String() string {
	switch s {
	case SortContext:
		return "context"
	case SortCost:
		return "cost"
	default:
		return "activity"
	}
}

// VendorStatus is what the HUD says about a vendor's store. Exactly four
// words, because there are exactly four distinguishable facts and collapsing
// any two of them would hide one.
//
// It was three, and three was right for as long as Discover was the only thing
// that knew anything: the directory is there, it is not there, or the OS
// refused it. internal/adapter/drift added a fact none of those three can
// express — the directory opened, the sessions parsed, and the structure the
// readings hang off is no longer where the adapter was verified to find it.
// Calling that "unreadable" would borrow the word for "the OS refused" to
// describe a store the OS handed over intact, which is the exact collapse this
// vocabulary exists to refuse. So four is now the honest count; the third word
// was never wrong, it was answering a different question.
//
// The fourth word also arrives at a different time, and that has a rendering
// consequence. The first three are known before a single session is read, so
// the empty state can speak them. Drift is only knowable AFTER the read, and a
// vendor cannot drift without having produced sessions — which means the grid
// is almost never empty when it happens. The vendor line is still this word's
// home, but footerLine carries it whenever there are rows, or the fourth word
// would be one nobody ever sees.
type VendorStatus uint8

const (
	// StatusWatching: the directory exists and is readable.
	StatusWatching VendorStatus = iota
	// StatusNotDetected: the directory is absent — the vendor is not
	// installed. Not an error, and never an error banner.
	StatusNotDetected
	// StatusUnreadable: the directory exists and the OS refused. This is the
	// one that deserves the operating system's own message beside it.
	StatusUnreadable
	// StatusDrifted: the store opened and read, and at least one session's
	// read could not find the structure the adapter was verified against.
	//
	// It upgrades StatusWatching and nothing else, which is the whole of the
	// precedence rule. A store the OS refused is a strictly bigger fact than
	// one that no longer matches — we know nothing at all about its shape —
	// and a vendor that is not installed has no sessions to drift. Because
	// Scan only ever reaches the drift roll-up down the path where Discover
	// succeeded, that ordering is structural rather than a comparison someone
	// has to remember to write.
	StatusDrifted
)

func (v VendorStatus) String() string {
	switch v {
	case StatusNotDetected:
		return "not detected"
	case StatusUnreadable:
		return "unreadable"
	case StatusDrifted:
		// "drifted", not "mismatched": cursor.ErrSchemaMismatch is the OTHER
		// tier — a store whose shape cannot be read at all — and two words that
		// near-rhyme for two different failures is how a vocabulary rots. Not
		// "unrecognized" either: padded into the same column as "unreadable" it
		// is the same length and the same opening syllable, and a reader
		// scanning five vendor lines would take one for the other.
		return "drifted"
	default:
		return "watching"
	}
}

// VendorView is one vendor's standing in the current snapshot.
type VendorView struct {
	Vendor model.VendorID
	Root   string
	Status VendorStatus
	Err    string
	Caps   model.Capabilities

	// Drifted counts the sessions this scan read whose read reported shape
	// drift; Sessions counts every session it read for this vendor.
	//
	// The pair travels because the word alone collapses two real cases: one
	// drifted session out of forty is a vendor mid-rollout, forty out of forty
	// is a format that moved under the whole store, and a reader deciding
	// whether to go look needs to know which. It is the same reason the
	// unreadable line carries the operating system's own message rather than
	// just the word — a status is more useful with the measurement that
	// produced it attached.
	Drifted  int
	Sessions int
}

// Snapshot is one completed scan.
type Snapshot struct {
	Sessions []*model.Session
	Vendors  []VendorView
	// At is when the scan completed. The zero value means no scan has ever
	// completed, which is a different thing from a scan that completed and
	// found nothing.
	At time.Time
	// Err is the scan's own failure, distinct from a vendor being absent.
	Err string
}

// State is everything Render reads. It is deliberately a plain value with no
// methods that touch the world: View() is pure over exactly this.
type State struct {
	Snap    Snapshot
	Now     time.Time
	Width   int
	Height  int
	Filter  Filter
	Sort    SortKey
	ShowAll bool
	Help    bool
	Scroll  int

	// Query is the type-to-filter substring, matched case-insensitively
	// against the row's identity (§7.14). Empty matches everything.
	Query string
	// Finding reports that the footer is accepting query keystrokes. It is a
	// mode, and it is the only one in the product — which is why it announces
	// itself in the footer rather than changing what an unmodified key does
	// silently.
	Finding bool

	// Cursor is the selected row's index among the VISIBLE rows, or -1 for no
	// selection.
	//
	// -1 is the default and it is load-bearing: v1 shipped with no selection
	// cursor at all, and a monitor that boots with a row already highlighted
	// asserts that row matters. The mark appears the first time the user asks
	// for it and not before, so the steady-state frame is unchanged.
	Cursor int
	// Detail opens the per-session pane over the row area (§7.11).
	Detail bool

	// Burn is telltale's own sampling history of the account quota windows.
	// It is HUD state, not schema (§4a): it describes this process's
	// observation history, not the session.
	Burn Burn

	// Scanning reports that a scan is in flight. It only ever produces a
	// spinner while no scan has completed yet; later slow scans surface as
	// staleness, not as motion (§7.6).
	Scanning bool
	Spinner  int

	Thresholds model.LivenessThresholds

	// Home is the user's home directory, resolved ONCE at program start so
	// Render stays pure over State (no environment reads on the view path;
	// review finding 2026-08-01). Empty disables home redaction.
	Home string
}

// NewState returns a State with the defaults filled in.
func NewState() State {
	return State{Thresholds: model.DefaultLivenessThresholds, Cursor: -1}
}

// Matches reports whether a session satisfies the find query.
//
// It matches the row's identity as rendered: the vendor's session name, the
// workspace path, and the session id — which is what a row falls back to
// showing when it has neither of the other two. Matching only "title" would
// make a torn-record row (labelled by its id) unfindable by the only text on
// its line.
//
// Case-insensitive substring, no globs and no regex: the query is displayed
// literally in the footer, and a syntax that can silently mean something other
// than what it looks like is a filter that hides rows without saying so.
func (st State) Matches(s *model.Session) bool {
	if st.Query == "" {
		return true
	}
	q := strings.ToLower(st.Query)
	if s.Name != nil && strings.Contains(strings.ToLower(*s.Name), q) {
		return true
	}
	if s.WorkspaceDir != nil && strings.Contains(strings.ToLower(*s.WorkspaceDir), q) {
		return true
	}
	return strings.Contains(strings.ToLower(s.ID), q)
}

func (st State) scanAge() time.Duration {
	if st.Snap.At.IsZero() {
		return 0
	}
	d := st.Now.Sub(st.Snap.At)
	if d < 0 {
		return 0
	}
	return d
}
