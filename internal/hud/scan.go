package hud

import (
	"context"
	"errors"
	"sort"
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
