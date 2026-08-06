package hud

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// maxReadsInFlight bounds concurrent Read calls per vendor. The poll is
// stat-first and most ticks re-read nothing, so this exists to keep a first
// scan over a large projects tree from opening hundreds of handles at once.
const maxReadsInFlight = 8

// Rooted is the optional interface an adapter implements to name the directory
// it watches. Only the empty state renders it — per-session locators are never
// displayed, because on a shared machine they can name another user's paths.
type Rooted interface {
	Root() string
}

// Scan runs every adapter and assembles one Snapshot.
//
// Errors are handled by showing LESS, never by showing a banner:
//
//   - ErrVendorAbsent removes the vendor's rows and marks it "not detected". A
//     user without Codex should not stare at a Codex error forever.
//   - Any other Discover error marks the vendor "unreadable" and carries the
//     OS message, which is the one case where the operating system knows
//     something the user needs.
//   - A Read failure drops that row silently. ErrSessionGone is the expected
//     case (the file rotated between Discover and Read); a sub-agent thread
//     that is not a session at all reports itself the same way.
//
// Shape drift is the one status that cannot be decided here from an error,
// because it is not one: every read succeeded. It is rolled up from what the
// reads reported, after they are all in — see foldDrift.
func Scan(ctx context.Context, adapters []model.Adapter, now time.Time) Snapshot {
	snap := Snapshot{At: now}

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		sessions []*model.Session
		views    []VendorView
	)

	for _, a := range adapters {
		wg.Add(1)
		go func(a model.Adapter) {
			defer wg.Done()
			view := VendorView{Vendor: a.Vendor(), Caps: a.Capabilities()}
			if r, ok := a.(Rooted); ok {
				view.Root = r.Root()
			}

			refs, err := a.Discover(ctx)
			switch {
			case errors.Is(err, model.ErrVendorAbsent):
				view.Status = StatusNotDetected
				mu.Lock()
				views = append(views, view)
				mu.Unlock()
				return
			case err != nil:
				view.Status = StatusUnreadable
				view.Err = err.Error()
				mu.Lock()
				views = append(views, view)
				mu.Unlock()
				return
			}
			view.Status = StatusWatching

			got := readAll(ctx, a, refs)
			foldDrift(&view, got)

			mu.Lock()
			sessions = append(sessions, got...)
			views = append(views, view)
			mu.Unlock()
		}(a)
	}
	wg.Wait()

	// Deterministic order in, deterministic frame out: the renderer sorts, but
	// the sort's tie-break needs a stable input to be stable at all.
	sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].Key() < sessions[j].Key() })
	sort.SliceStable(views, func(i, j int) bool { return views[i].Vendor < views[j].Vendor })

	snap.Sessions = sessions
	snap.Vendors = views
	if err := ctx.Err(); err != nil {
		snap.Err = err.Error()
	}
	return snap
}

// driftNote is the opening of every report internal/adapter/drift writes.
//
// This is a coupling across a package boundary that the compiler cannot see:
// drift.Watch.note is unexported and its verdict reaches the HUD only as text
// on Session.Diagnostics, so the vendor line recognizes drift by the words the
// adapter layer chose. That is a real hazard — a reworded note would leave the
// vendor line silently quiet, which is indistinguishable from nothing being
// wrong and is the failure mode this whole feature exists to end — so it is
// pinned by a test that folds a REAL drift.Watch onto
// a session and asserts this prefix still matches
// (TestDriftIsRecognizedFromTheNoteTheAdapterLayerActuallyWrites).
const driftNote = "shape drift: "

// reportsDrift is whether a session's read found the store's shape moved.
//
// Diagnostics, not Degraded: drift.Fold degrades only the fields the session
// failed to source anyway, so a session that got its values from elsewhere
// drifts with an empty Degraded delta — and an ordinary torn record degrades
// fields with no drift at all. Degraded is neither necessary nor sufficient
// here; the report is.
func reportsDrift(s *model.Session) bool {
	if s == nil {
		return false
	}
	for _, d := range s.Diagnostics {
		if strings.HasPrefix(d, driftNote) {
			return true
		}
	}
	return false
}

// foldDrift rolls a read's per-session drift reports up to the vendor.
//
// ANY drifted session drifts the vendor, and the counts travel so the display
// can say which kind of any it was. Requiring every session would be a monitor
// that goes quiet exactly when it should be loudest: a vendor mid-rollout
// writes the new shape only into sessions started since the update, so the
// newest and most-used rows are the first to drift and the last to outvote a
// long tail of old transcripts. The alarm would wait for the evidence to age
// out.
//
// The false-alarm cost that would usually argue for a threshold is already
// paid upstream: drift.Fold reports nothing from a read that examined no
// well-formed units, which is what a torn or empty file produces. So "any" is
// not a loose rule here, and a count-based one would be an invented boundary
// (decisions/001).
//
// It is a function rather than three lines inline because the golden fixtures
// build vendor views by hand: sharing it is what stops a fixture from pinning
// a roll-up the real scan would never produce.
func foldDrift(view *VendorView, sessions []*model.Session) {
	view.Sessions = len(sessions)
	view.Drifted = 0
	for _, s := range sessions {
		if reportsDrift(s) {
			view.Drifted++
		}
	}
	if view.Drifted > 0 {
		view.Status = StatusDrifted
	}
}

func readAll(ctx context.Context, a model.Adapter, refs []model.SessionRef) []*model.Session {
	if len(refs) == 0 {
		return nil
	}
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		out  []*model.Session
		gate = make(chan struct{}, maxReadsInFlight)
	)
	for _, ref := range refs {
		wg.Add(1)
		go func(ref model.SessionRef) {
			defer wg.Done()
			gate <- struct{}{}
			defer func() { <-gate }()

			s, err := a.Read(ctx, ref)
			if err != nil || s == nil {
				// Showing less: a row we cannot read is a row we do not draw.
				return
			}
			mu.Lock()
			out = append(out, s)
			mu.Unlock()
		}(ref)
	}
	wg.Wait()
	return out
}
