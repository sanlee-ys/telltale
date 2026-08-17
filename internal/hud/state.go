package hud

import (
	"strings"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/quotacache"
	"github.com/sanlee-ys/telltale/internal/usagecache"
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
	FilterGrok
	FilterPi
	FilterSelfReported
)

func (f Filter) Next() Filter {
	if f >= FilterSelfReported {
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
	case FilterGrok:
		return string(model.VendorGrok)
	case FilterPi:
		return string(model.VendorPi)
	case FilterSelfReported:
		return string(model.VendorSelfReported)
	default:
		return "all"
	}
}

// VendorID is the vendor a single-vendor filter selects, and false for
// FilterAll. The footer and the hide list both compare filters to vendors,
// and one mapping here beats two switch statements that can drift apart.
func (f Filter) VendorID() (model.VendorID, bool) {
	switch f {
	case FilterClaude:
		return model.VendorClaude, true
	case FilterCodex:
		return model.VendorCodex, true
	case FilterGemini:
		return model.VendorGemini, true
	case FilterAntigravity:
		return model.VendorAntigravity, true
	case FilterCursor:
		return model.VendorCursor, true
	case FilterGrok:
		return model.VendorGrok, true
	case FilterPi:
		return model.VendorPi, true
	case FilterSelfReported:
		return model.VendorSelfReported, true
	default:
		return "", false
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
	case FilterGrok:
		return v == model.VendorGrok
	case FilterPi:
		return v == model.VendorPi
	case FilterSelfReported:
		return v == model.VendorSelfReported
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
// the empty state — the only place the vendor line renders — can speak them.
// Drift is knowable only AFTER the read, and a vendor cannot drift without
// having produced sessions. Those sessions can still all be hidden, by the
// idle cutoff or a filter or a query, so the vendor line does get to say this
// word (testdata/golden/empty-drifted.txt is exactly that frame) — but the
// ordinary case is a grid full of rows, where the vendor line is not on screen
// at all. So the word has a second home: footerLine renders driftNotice under
// EVERY body, grid and empty state and help overlay and detail pane alike.
// Without that, the fourth word would be one nobody ever sees.
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

	// SelfReported says this vendor's rows are claims its writer made, not
	// readings telltale took (design.md §7.23). It is set from the adapter's
	// own declaration, never inferred.
	//
	// It is a separate axis from Status on purpose. Status answers "could the
	// store be read" — and a self-reporting vendor can be watching, not
	// detected or unreadable exactly like any other. Folding the two into one
	// word would give the vocabulary a fifth member that is not comparable
	// with the other four, which is the collapse VendorStatus's own doc exists
	// to refuse.
	SelfReported bool
}

// Snapshot is one completed scan.
type Snapshot struct {
	Sessions []*model.Session
	Vendors  []VendorView
	// Account is the statusline-relayed account quota, one entry per vendor
	// whose reading survived the cache's expiry rules (design.md §7.15). It
	// is not folded into Sessions because it is not one: rate limits are a
	// property of the account, and pinning them to a fabricated session row
	// would assert per-session quota, the claim §7.1 exists to forbid.
	Account []quotacache.Account
	// Spend is the hook-relayed token total, one entry per vendor whose
	// running total survived the cache's expiry rules (design.md §7.16).
	//
	// It is a separate field from Account for the reason §7.16 exists to
	// state: these are two different measurements, and sharing a field is how
	// they would come to be read as one. Account is a percentage OF something
	// — a window whose ceiling the vendor published. Spend has no denominator
	// anywhere, because Cursor exposes no account limit without a network
	// call (§3.9), so it can be counted and never gauged.
	//
	// Like Account it is not folded into Sessions: the hook fires for
	// cursor-agent CLI conversations, which are not the IDE Composer sessions
	// the Cursor rows come from, so pinning a total to any row would be a
	// per-session claim the seam cannot support.
	//
	// NOTHING RENDERS THIS as of 2026-08-09, and the field is kept filled on
	// purpose. The owner retired the display — the total was honest and it
	// bought no decision — while the relay under it stays wired: `telltale hook
	// cursor` keeps writing, this scan keeps reading, and the day the display is
	// wanted back it is a call site rather than a re-plumb (§7.16's amendment).
	// A reader looking for where this is consumed will find nowhere, and that is
	// the answer rather than a bug.
	Spend []usagecache.Total
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

	// Usage opens the fleet usage view over the row area (§7.17): one block
	// per vendor, saying what that vendor can honestly claim about its
	// account.
	//
	// It is a THIRD body rather than a section of the detail pane, because
	// what it reports is not a session fact. Quota is a property of the
	// account (§7.1's sixth rule) and the relayed token total names a
	// cursor-agent CLI conversation the HUD's rows do not come from (§7.16) —
	// so neither has a row to hang off, and both would be a per-session claim
	// if they did. Detail, Help, Usage and Week are mutually exclusive: one
	// body at a time, enforced in Update rather than in Render, so the state
	// can never describe two panes at once.
	Usage bool

	// Week opens the week page over the row area (§7.19): one line per
	// vendor, the slow quota windows only. It is a lens over the same
	// account data Usage renders, and a separate flag rather than a mode on
	// Usage so each door has one key and the esc chain stays one-step-one-
	// layer.
	Week bool

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

	// Hidden is the launch-time hide list (`--hide` / TELLTALE_HUD_HIDE,
	// §7.20). The scan already dropped these vendors from the snapshot, so
	// Render never has to check it row by row; the field exists so the footer
	// can STATE the hide — a monitor that silently hides vendors is the same
	// liar as one that silently hides rows — and so the `v` cycle can skip
	// filters that could only ever show an empty grid. Sorted at parse time,
	// so the footer's wording is stable frame to frame.
	Hidden []model.VendorID
}

// hiddenHas reports whether a vendor is on the launch-time hide list.
func (st State) hiddenHas(v model.VendorID) bool {
	for _, h := range st.Hidden {
		if h == v {
			return true
		}
	}
	return false
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
