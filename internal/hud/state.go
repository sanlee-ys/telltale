package hud

import (
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// Filter is the vendor filter. It cycles rather than opening a menu: with two
// vendors a cycle is one keystroke and no mode, and the active filter is
// always stated in the footer.
type Filter uint8

const (
	FilterAll Filter = iota
	FilterClaude
	FilterCodex
)

func (f Filter) Next() Filter {
	if f >= FilterCodex {
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

// VendorStatus is what the empty state says about a vendor. Exactly three
// words, because there are exactly three distinguishable facts and collapsing
// any two of them would hide one.
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
)

func (v VendorStatus) String() string {
	switch v {
	case StatusNotDetected:
		return "not detected"
	case StatusUnreadable:
		return "unreadable"
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

// NewState returns a State with the v1 defaults filled in.
func NewState() State {
	return State{Thresholds: model.DefaultLivenessThresholds}
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
