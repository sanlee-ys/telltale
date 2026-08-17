package council

import (
	"context"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// A race's worktrees are prepared OFF the render loop (§9.37, amended
// 2026-08-17). Until now `arenaSetup` ran inline in `dispatch`, which runs
// inside Update — so for as long as git took, the room drew nothing, answered
// nothing, and read no keys. The measurement that forced it is the operator's
// own: parallel sessions against one repo, a held lock, and a room frozen with
// ctrl+c unread while the one command that could have ended it sat in a queue
// nobody was draining.
//
// Three rules shape what replaced it, and each one is a refusal as much as a
// mechanic:
//
//   - THE SETUP MOVES, THE ORDER DOES NOT. `git worktree add` still runs once
//     per seat, serially, in seat order (arenaSetup). Parallelising it is the
//     obvious next thought and is refused: those adds write the repo's own
//     refs and administrative files, so N at once contend for the very lock
//     this change exists to survive.
//   - THE FRAME NAMES THE STEP, NEVER THE PROGRESS. Each stage reports the
//     words for what it is about to do — "preparing worktree for codex" — and
//     the room draws that. There is no percentage, no bar, and no count of
//     seats done, because council cannot know how long a checkout will take and
//     a number it cannot measure is a number it may not draw (§4a.1). The
//     spinner beside the words is liveness, not progress: it says git is
//     running, which is exactly the fact a frozen room could not say.
//   - A SETUP THAT ENDS BADLY HANDS THE ROOM BACK. A deadline hit or a git
//     failure lands on the room's existing notice — the step, then git's own
//     sentence — the brief returns to the composer, and the room is composing
//     again. Nothing about the failure leaves the operator with a room they
//     cannot act in, which is the whole complaint this change answers.
const (
	// arenaSetupDeadline bounds the WHOLE setup: base read, race numbering,
	// seed plan, and every seat's worktree add and seeding, together.
	//
	// One deadline over the whole thing rather than one per git call, because
	// the number an operator experiences is how long the room was unusable, and
	// a per-call bound times five seats is a total nobody chose. It is enforced
	// through the one context arenaSetup carries, so the same clock also ends
	// the setup between steps rather than only inside a command.
	//
	// 90 seconds, against a measurement rather than a guess. On this box (Intel
	// Mac, macOS 26.5.2, 2026-08-17) a FIVE-seat setup against a synthetic
	// repository built to telltale's own shape — 540 files in 60 directories,
	// ~8 MB of content, matching the 526 tracked files and 8 MB this repo
	// carries — ran end to end, worktree adds included, in 2.3s cold and 1.3s /
	// 1.4s on the two runs after it.
	//
	// So the deadline is roughly 40x the measured case, and the margin is the
	// decision, not the number. The failure it exists for is a LOCK another
	// session holds, which is unbounded by nature and says nothing about how big
	// the repository is; a tree ten times this one on slower storage is an
	// ordinary machine rather than an exotic one. What the deadline must never
	// be is tight enough to kill a setup that would have finished — a `git
	// worktree add` killed mid-checkout leaves a half-created tree the operator
	// then clears by hand, which is a worse room than a slow one. 90s is past
	// the point where a person has already concluded something is stuck and
	// would rather be told than keep waiting.
	arenaSetupDeadline = 90 * time.Second

	// The step vocabulary. Plain words, present tense, no counts — the frame
	// says what is happening and refuses to say how far along it is.
	arenaStepBase   = "reading the base commit"
	arenaStepNumber = "numbering the race"
	arenaStepPlan   = "reading " + seedFileName
)

// arenaStepTree and arenaStepSeed name the two per-seat stages. The vendor id
// rather than the column label, because these are read against branch and tree
// names that carry the same id (arenaBranch, arenaTree) — a frame naming
// "Codex" while the tree it is cutting says "codex" would be one more thing to
// reconcile at the exact moment the operator is trying to work out what is
// stuck.
func arenaStepTree(v model.VendorID) string { return "preparing worktree for " + string(v) }
func arenaStepSeed(v model.VendorID) string { return "seeding worktree for " + string(v) }

// arenaSetupStop is the sentence a setup that ended WHOLESALE carries: the step
// it was on, then why it stopped there.
//
// The step leads because it is the half the operator cannot reconstruct. "fatal:
// Unable to create index.lock: File exists" names a lock and not which of eight
// git calls met it; "preparing worktree for codex: fatal: …" says where the room
// actually was. The three endings stay three sentences (§4a.1): a deadline that
// expired, an operator who cancelled, and a git command that refused are
// different facts, and only the last one has a git message to quote.
func arenaSetupStop(ctx context.Context, step string, gerr error) error {
	why := ""
	if gerr != nil {
		why = gerr.Error()
	}
	switch ctx.Err() {
	case context.DeadlineExceeded:
		// git was killed by the clock, so it usually said nothing at all. What
		// it did say is quoted; when it said nothing, nothing is invented — the
		// deadline is council's own act and the sentence owns it.
		s := step + ": the " + dur(arenaSetupDeadline) + " setup deadline expired"
		if why != "" {
			s += " — git said: " + why
		}
		return errors.New(s)
	case context.Canceled:
		return errors.New(step + ": cancelled")
	}
	return errors.New(step + ": " + why)
}

// arenaSetupResult is one finished setup, exactly what arenaSetup returned plus
// the workspace it ran against.
//
// The workspace rides along rather than being re-read from State when the turn
// starts, and that is the one thing a setup off the loop must not get wrong: the
// room stays usable while git works, so `/cd` can move it under a setup already
// running. The race's receipt (Model.lastRace, which /adopt and /arena drop
// resolve against) has to name the directory the trees are actually siblings of,
// not the one the room happens to be in when the last worktree lands.
type arenaSetupResult struct {
	workspace string
	raceN     int
	base      string
	trees     map[model.VendorID]string
	seeds     map[model.VendorID]*SeedReport
	seatErr   map[model.VendorID]string
	err       error
}

// arenaPrep is the race whose worktrees are being prepared right now. Nil when
// no setup is running, which is the room's ordinary state.
//
// It holds the whole dispatch the setup is standing in front of — route, prompt
// and the draft as typed — because that dispatch cannot be re-derived when the
// setup lands: the composer is empty by then, and re-parsing whatever the
// operator has typed since would race a brief nobody pressed enter on.
type arenaPrep struct {
	// id numbers this setup, so a message from one the room has already stopped
	// is dropped by comparison rather than by hoping the timing worked out —
	// arenaStatMsg's rule, for the same reason: this runs on a goroutine.
	id int
	// ch carries every step and the one result. Buffered past the largest
	// number of messages a setup can produce (three fixed stages plus two per
	// seat, plus the result), so an ABANDONED setup's goroutine can always
	// finish writing and exit: a cancelled race must not leave a goroutine
	// parked on a channel nobody will ever read again.
	ch chan arenaSetupMsg
	// cancel ends the setup's context — the deadline's timer and the operator's
	// ctrl+c are the same act from git's side.
	cancel context.CancelFunc
	// route, prompt and racers are the dispatch this setup is preparing.
	route  Route
	prompt string
	racers []model.VendorID
	// brief is the draft exactly as typed. It goes back into the composer if
	// the setup cannot finish: a race that never started must not cost the text
	// it was going to race.
	brief string
	// workspace is the directory the setup was launched against.
	workspace string
}

// arenaSetupMsg is one step beginning, or the whole setup landing. Both arrive
// on the same channel so the room reads them in the order the setup produced
// them, and both name their prep for the staleness comparison above.
type arenaSetupMsg struct {
	prep int
	// step names the stage that just began, in words. Empty on the last
	// message.
	step string
	// done is the finished setup, and is non-nil on exactly one message.
	done *arenaSetupResult
}

// beginArenaSetup starts a race's worktree setup off the loop and returns the
// command that reads its first message.
//
// The draft is cleared HERE rather than when the turn spawns, because from the
// operator's side the brief was sent the moment they pressed enter — a composer
// that kept the text through a ten-second setup would invite a second enter on
// a race already being prepared. It comes back on any ending that is not a
// dispatched turn (applyArenaSetup, stopArenaSetup).
func (m *Model) beginArenaSetup(route Route, prompt string) tea.Cmd {
	reg := vendors.Registry()
	var racers []model.VendorID
	for i := range m.st.Columns {
		c := m.st.Columns[i]
		if _, ok := reg[c.Vendor]; ok && m.st.seats(c) {
			racers = append(racers, c.Vendor)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), arenaSetupDeadline)
	m.arenaPrepN++
	p := &arenaPrep{
		id:        m.arenaPrepN,
		ch:        make(chan arenaSetupMsg, 2*len(racers)+8),
		cancel:    cancel,
		route:     route,
		prompt:    prompt,
		racers:    racers,
		brief:     m.st.Draft,
		workspace: m.st.Workspace,
	}
	m.arenaPrep = p
	m.setDraft("")
	// Viewing, exactly as a dispatched turn leaves the room. It is also what
	// makes ctrl+c reach stopArenaSetup instead of quitting: compose mode's
	// ctrl+c is the room's way out, and a setup running under it would have
	// answered the operator's "stop this" by closing the room.
	m.st.Mode = ModeViewing
	m.st.Notice = ""
	m.st.ArenaSetup = arenaStepBase
	// Everything the goroutine touches is copied out of the Model first. A Cmd
	// and its worker share nothing with the room but the messages they send —
	// arenalive.go's rule, and it binds harder here because this worker outlives
	// several frames.
	id, ch, ws, turn := p.id, p.ch, p.workspace, m.st.Turn+1
	go func() {
		defer close(ch)
		raceN, base, trees, seeds, seatErr, err := arenaSetup(ctx, ws, turn, racers,
			func(s string) { ch <- arenaSetupMsg{prep: id, step: s} })
		ch <- arenaSetupMsg{prep: id, done: &arenaSetupResult{
			workspace: ws, raceN: raceN, base: base,
			trees: trees, seeds: seeds, seatErr: seatErr, err: err,
		}}
	}()
	return m.waitArenaSetup()
}

// waitArenaSetup reads one message from the running setup. One at a time, and
// deliberately not drained into a batch the way waitEvents drains vendor output:
// each step is a frame the operator is meant to see, and a batch would collapse
// the four seats of a slow race into whichever step happened to be last.
func (m *Model) waitArenaSetup() tea.Cmd {
	if m.arenaPrep == nil {
		return nil
	}
	ch := m.arenaPrep.ch
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			// The setup goroutine finished and closed. Nothing to say and
			// nothing to re-arm: the result already arrived on the message
			// before this one.
			return nil
		}
		return msg
	}
}

// applyArenaSetup lands one step or the finished setup, and returns whatever
// runs next: the read of the following message, the dispatch the setup was
// preparing, or nothing at all.
func (m *Model) applyArenaSetup(msg arenaSetupMsg) tea.Cmd {
	p := m.arenaPrep
	if p == nil || p.id != msg.prep {
		// A setup the room has already stopped, still writing its buffered
		// messages out on the way to exiting. Dropped by comparison.
		return nil
	}
	if msg.done == nil {
		m.st.ArenaSetup = msg.step
		return m.waitArenaSetup()
	}
	res := msg.done
	m.arenaPrep, m.st.ArenaSetup = nil, ""
	// Released whichever way this ended, so the deadline's timer does not
	// outlive the setup it was bounding.
	p.cancel()
	if res.err != nil {
		// The room's existing error rendering, unchanged — one notice, opened
		// with "arena:" the way a refused race always was. What is new is only
		// what the sentence carries: the step it stopped on and git's own words
		// (arenaSetupStop). The brief goes back and the room composes again,
		// which is the property that matters more than the wording: a setup
		// that failed must hand the room back, not keep it.
		m.st.Notice = "arena: " + res.err.Error()
		m.setDraft(p.brief)
		m.st.Mode = ModeComposing
		return nil
	}
	cmd := m.sendTurn(p.route, p.prompt, res)
	if cmd == nil && m.turn == nil {
		// The setup succeeded and the dispatch still produced nothing — every
		// racer's worktree failed on its own, so there was nobody left to send
		// to. Each seat says why on its column; what belongs here is the brief,
		// back where it was typed, for the reason the failure branch above
		// gives. Inline, this path never lost it: dispatch returned before it
		// reached the line that clears the composer.
		m.setDraft(p.brief)
		m.st.Mode = ModeComposing
	}
	return cmd
}

// stopArenaSetup is ctrl+c during a setup: the context ends, the brief comes
// back, and the room says what was left on disk.
//
// The leftovers are NAMED rather than cleaned up. A worktree this setup already
// added is a real tree on a real branch, and §9.37's founding ruling is that
// worktrees are kept until the USER deletes them — a cancel that quietly removed
// them would be the room deciding for the operator at the one moment they have
// just said stop. The next race numbers itself past them (arenaRaceNumber), so
// nothing is blocked by leaving them there.
func (m *Model) stopArenaSetup() {
	p := m.arenaPrep
	if p == nil {
		return
	}
	m.arenaPrep, m.st.ArenaSetup = nil, ""
	p.cancel()
	m.setDraft(p.brief)
	m.st.Mode = ModeComposing
	m.st.Notice = "race stopped while its worktrees were being prepared — any tree already added is kept · git worktree remove clears one"
}
