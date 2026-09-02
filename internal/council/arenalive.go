package council

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The live arena stat (§9.37, amended 2026-08-09): while a race runs, each
// racing seat's worktree is re-read — `git add -N . && git diff <base> --stat`,
// the same two mechanics collectArena carries in from claude-squad's
// session/git/diff.go — so the audience watches the diff grow instead of
// waiting for the finish line. The finish-time collectArena read stays the
// authoritative final; everything here renders as INTERIM and is replaced,
// never merged, the moment the seat lands (§4a.1: a mid-race read is a
// measured value at a moment that is already past, and letting it masquerade
// as the settled result is the estimate-as-reading bug in race clothing).
//
// Mechanics, and why each one is shaped the way it is:
//
//   - EVENT-TRIGGERED, never polled. A refresh is armed by stream activity on
//     that column (KindText / KindActivity in applyEvents) — an idle seat is
//     re-reading nothing, and a timer that ran git against four untouched
//     trees every two seconds would be spend without a measurement behind it.
//   - THROTTLED to one read per arenaRefreshInterval per seat, timed off
//     State.Now — the tick-stamped clock, never time.Now(), so the throttle is
//     testable on a State a test typed out and Render's purity is untouched.
//   - AS A COMMAND. The read spawns git, so it runs as a tea.Cmd (goroutine →
//     arenaStatMsg), never inline in Update and never in Render. One refresh
//     in flight per seat, enforced by skipping rather than queueing: a queued
//     read would measure a moment even further past by the time it ran.

// arenaRefreshInterval is the floor between two live stat reads of one seat's
// worktree.
//
// Two seconds, because the read is a subprocess pair per seat per interval and
// the surface it feeds is a human watching a race measured in tens of seconds
// (the first live race's podium ran 7s/15s/19s, §9.37): a faster cadence spends
// processes on frames nobody can distinguish, a slower one makes the "live"
// stat visibly stale against the streaming column beside it.
const arenaRefreshInterval = 2 * time.Second

// arenaRefreshMaxFails is how many CONSECUTIVE failed reads end the live stat
// for one seat. The race itself is never killed — the finish-time read still
// runs — and the stop is said on the column rather than rendered as a stat
// that silently froze. Three, not one, because the likeliest failure is
// transient by construction: the refresh shares the worktree's index with a
// vendor writing into it, and one contended read is not evidence the tree is
// unreadable. A success resets the count.
const arenaRefreshMaxFails = 3

// ArenaInterim is one MID-RACE read of a racing seat's worktree: the stat as
// it stood at a moment that is already past. On Column so Render stays pure —
// every field is computed when the arenaStatMsg lands, never during a frame.
//
// The three states §4a.1 requires stay three states:
//
//   - no read yet: the Column's pointer is nil, and the block renders nothing
//     at all — absence, not a zero.
//   - a read that returned empty: Stat == "" with Err == "" — the seat has
//     changed nothing YET, a measured zero, rendered as its own sentence
//     against the named base.
//   - a read that failed: Err carries git's own first stderr line — degraded,
//     never dressed up as "no changes".
type ArenaInterim struct {
	// Stat is `git diff <base> --stat` at the moment of the read, verbatim.
	Stat string
	// Base is the commit the stat answers against, for the measured-zero
	// sentence — the same recorded SHA the final diff anchors on.
	Base string
	// Err is the read's failure, when it failed: git's first stderr line. A
	// failed read degrades the live stat and nothing else — the race runs on.
	Err string
	// Stopped reports that arenaRefreshMaxFails consecutive reads failed and
	// this seat's live stat gave up. Said on the column, because a gauge that
	// stops updating without saying so is a gauge lying by omission.
	Stopped bool
}

// arenaLiveState is one racing seat's refresh bookkeeping. On turnState, so it
// dies with the turn — which is what stops all refreshing when the turn ends,
// with no cleanup path to forget. Seats whose worktree failed setup never get
// an entry at all (dispatch builds the map from arenaTrees), so a refresh
// structurally cannot fire for them.
type arenaLiveState struct {
	// armed is set by stream activity and consumed by the launch. An idle
	// seat never arms, so an idle seat is never read.
	armed bool
	// inFlight bounds concurrency to one read per seat: a due refresh that
	// finds one running is SKIPPED, not queued.
	inFlight bool
	// lastRead is when the previous read was LAUNCHED (State.Now at launch),
	// which is what "at most once per interval" throttles against.
	lastRead time.Time
	// fails counts consecutive failed reads; arenaRefreshMaxFails ends it.
	fails int
	// stopped ends refreshing for this seat: repeated failure, or the seat
	// landed and the final owns the block from here.
	stopped bool
}

// arenaStatMsg is one finished read arriving back in the Update loop. It names
// the vendor AND the turn, because the read ran in a goroutine and the room
// may have moved on: a stale message must be droppable by comparison, not by
// hoping the timing worked out.
type arenaStatMsg struct {
	vendor model.VendorID
	turnN  int
	stat   string
	err    string
}

// collectArenaStat is the interim read: intent-to-add plus the stat, and
// deliberately NOT the full patch. The patch is the finish line's deliverable
// (yankable, 1 MB budget); mid-race it would be re-read every interval and
// rendered never — the stat is what the live block shows, so the stat is what
// the live read pays for. `git add -N .` runs first for collectArena's stated
// reason: without it a seat whose work so far is only NEW files reads as "no
// changes yet", the false zero again.
func collectArenaStat(tree, base string) (stat, errLine string) {
	// Excluded here for the same reason the finish-time read excludes it: a
	// "so far" block that opened with council's own brief file would report the
	// room's write as the racer's first move (arenabrief.go).
	if _, err := gitOut(tree, append([]string{"add", "-N", "."}, arenaBriefArgs(tree)...)...); err != nil {
		return "", err.Error()
	}
	out, err := gitOut(tree, "--no-pager", "diff", base, "--stat")
	if err != nil {
		return "", err.Error()
	}
	return out, ""
}

// refreshArenaCmd runs one interim read off the Update loop. The closure
// captures plain strings rather than the Model — a Cmd runs on a goroutine,
// and the only thing it may share with the room is the message it returns.
func refreshArenaCmd(v model.VendorID, turnN int, tree, base string) tea.Cmd {
	return func() tea.Msg {
		stat, errLine := collectArenaStat(tree, base)
		return arenaStatMsg{vendor: v, turnN: turnN, stat: stat, err: errLine}
	}
}

// armArenaRefresh marks one racing seat as having produced stream activity, so
// the next due check may read its tree. Called from applyEvents on the events
// that mean the vendor is DOING something (text, tool calls) — a session id or
// a cost figure arriving is not evidence the tree moved.
func (m *Model) armArenaRefresh(v model.VendorID) {
	// The race in flight (race), where this read m.turn: a race is the only
	// dispatch while it runs, so its record is the one every racer shares.
	ts := m.race()
	if ts == nil {
		return
	}
	if ls := ts.arenaLive[v]; ls != nil {
		ls.armed = true
	}
}

// dueArenaRefreshes launches every armed, unthrottled, not-already-reading
// seat's interim read, and returns the batch (nil when nothing is due).
//
// Called from Update on event batches and on the spinner tick — the tick
// matters, because arming and firing are decoupled by the throttle: a seat
// whose activity arrived at second 1 of a 2-second interval would otherwise
// wait for its NEXT event to be read, and a vendor that goes quiet after a
// burst of writes is exactly the seat whose stat is most behind.
//
// Time is m.st.Now, the tick-stamped clock, never time.Now(): the throttle is
// a decision about state, and reading a wall clock here would make it the one
// piece of this feature a test cannot pin.
func (m *Model) dueArenaRefreshes() tea.Cmd {
	ts := m.race()
	if ts == nil {
		return nil
	}
	var cmds []tea.Cmd
	for v, ls := range ts.arenaLive {
		if !ls.armed || ls.inFlight || ls.stopped {
			continue
		}
		// A seat that has landed is out of the race: the final result owns its
		// block, and a read launched now would arrive as a stale message built
		// to be dropped. live is the same set turn teardown drains, so "the
		// turn ended" and "this seat finished" are one check.
		if !ts.live[v] {
			continue
		}
		if !ls.lastRead.IsZero() && m.st.Now.Sub(ls.lastRead) < arenaRefreshInterval {
			continue
		}
		c := m.column(v)
		if c == nil {
			continue
		}
		ls.armed, ls.inFlight, ls.lastRead = false, true, m.st.Now
		cmds = append(cmds, refreshArenaCmd(v, c.TurnN, ts.arenaTrees[v], ts.arenaBase))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// applyArenaStat lands one interim read on its column — or drops it, and the
// drop conditions are the point. A read that outlived its turn (turn ended,
// turn number moved on) or its seat (the final already landed) says nothing:
// the final REPLACES the interim, and a stale goroutine must never write over
// either the next turn or the settled result.
func (m *Model) applyArenaStat(msg arenaStatMsg) {
	ts := m.race()
	if ts == nil {
		return
	}
	ls := ts.arenaLive[msg.vendor]
	if ls == nil {
		return
	}
	// The seat's one read slot frees whatever happens below — even a dropped
	// stale message is a read that is no longer running.
	ls.inFlight = false
	c := m.column(msg.vendor)
	if c == nil || c.TurnN != msg.turnN || c.Arena != nil {
		return
	}
	if msg.err != "" {
		ls.fails++
		in := &ArenaInterim{Err: msg.err, Base: ts.arenaBase}
		if ls.fails >= arenaRefreshMaxFails {
			// Give up on THIS seat's live stat and say so on the column. The
			// race is untouched: the vendor is still running and the
			// finish-time read still lands the authoritative result.
			ls.stopped = true
			in.Stopped = true
		}
		c.ArenaInterim = in
		return
	}
	ls.fails = 0
	c.ArenaInterim = &ArenaInterim{Stat: msg.stat, Base: ts.arenaBase}
}
