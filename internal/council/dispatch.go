package council

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// eventBuffer bounds the shared channel every vendor writes into.
//
// Bounded on purpose. When it fills, the reading goroutine blocks, the OS pipe
// fills behind it, and the vendor stalls — which the user sees as a column that
// has paused. The alternative, an unbounded channel, trades that visible pause
// for unbounded memory and an event backlog that renders minutes after the
// vendor produced it. A stalled column is honest; a lagging one is not.
const eventBuffer = 512

// drainMax is how many events one Update may consume.
//
// Token-level streaming produces events far faster than a terminal can usefully
// redraw. Batching caps redraws at one per Update regardless of token rate,
// while the bound keeps a fast vendor from starving input handling — the key
// that cancels the turn has to stay responsive while three agents talk.
const drainMax = 64

// turnState is ONE DISPATCH: the seats one press of enter sent a brief to, and
// everything those seats share while they answer it.
//
// Until §9.54 there was at most one of these, held in Model.turn, and its
// existence was the room's whole notion of "busy". Now the room holds one per
// dispatch still in flight, reached per SEAT through Model.turns — a brief to
// @codex and a later brief to @grok are two records, and a brief to @all is one
// record three seats point at. What stays shared at this level is exactly what
// a dispatch decides once for everyone it addresses: the turn number, the route
// the header names, and the arena's all-or-nothing bookkeeping. What moved down
// to the seat — the process handle, the cancel, the give-up — is everything the
// operator can now do to one seat while its neighbours work on.
type turnState struct {
	// n is the dispatch's number, the same value every seat it addressed
	// carries as Column.TurnN. The room keeps ONE sequence across concurrent
	// dispatches rather than a counter per seat, and that is a ruling rather
	// than a leftover: every surface that already prints a turn number — the
	// separators, the by-turn page (PageTurns), `/retry`, room.json, the
	// reattach card — reads this one coordinate, and a per-seat count would
	// have two seats both on "their turn 4" while the page could open only one
	// of them. So a turn number is a DISPATCH number: "turn 5" is the fifth
	// brief the room sent, whoever it went to, and a seat's own history is the
	// subset of those numbers it took part in.
	n int
	// route is where this dispatch went, kept so the header can name the most
	// recent dispatch's destination (§9.21) and retire it when THAT dispatch
	// lands rather than when the room goes quiet.
	route Route
	// flow marks a /flow hop's dispatch. The chain's teardown-on-death runs
	// when the hop's dispatch ends, and only then: an unrelated brief landing
	// while a hop streams must not be read as the hop dying (turnColumnFinished).
	flow bool
	// cancel ends the dispatch's context, the parent of every seat's own; it is
	// what teardown pulls and what the last seat landing pulls behind it.
	cancel context.CancelFunc
	// ctx is that context itself, kept so a seat that RETREATS mid-dispatch
	// (retreatSeat, vendors.LiveFallback) can mint its batch child's context
	// under the same parent every sibling's hangs from — a retreat's process
	// must die with the dispatch exactly as the one it replaced would have.
	// Nil on a dispatch a test or a replay typed out by hand; the retreat then
	// hangs the child from Background, which the room's own teardown still
	// reaches through the seat's cancel.
	ctx context.Context
	// prompts holds, per seat, the prompt this dispatch handed it — the
	// vendor's own text, before the brief was applied. It exists for one
	// reader: a live seat whose handshake is refused AFTER the turn was
	// handed over (the protocol queues the turn until the handshake answers)
	// retreats to its batch adapter on the same dispatch, and the brief the
	// operator typed has to reach that adapter unchanged.
	prompts map[model.VendorID]string
	// seatCancel ends one seat's context — the one its process was started on
	// — so ctrl+c on a focused seat kills that seat's child and nobody else's.
	// A persistent seat's process lives on roomCtx and is INTERRUPTED instead
	// (cancelSeat), so its entry here ends nothing but is kept for symmetry:
	// every seat that entered live has one.
	seatCancel map[model.VendorID]context.CancelFunc
	// live is the set of columns that have not reached a terminal phase yet, so
	// the turn knows when it is over without polling.
	//
	// A SET rather than the counter it used to be. A persistent seat can be told
	// its turn ended by two different signals — the vendor's own end-of-turn
	// line, and, if the process then dies, its exit — and a counter would
	// decrement twice for one column and end the turn while another seat was
	// still talking. Membership is idempotent; a count is not.
	live map[model.VendorID]bool
	// persistent names the columns driven by a long-lived process, whose child
	// must survive this turn's teardown.
	persistent map[model.VendorID]bool
	// seatHandles keys THIS ORDINARY turn's one-shot processes by vendor — the
	// non-arena mirror of arenaHandles below, and NEW plumbing rather than a
	// widening of it. Two fields rather than one because arenaHandles answers a
	// second question this one must not: arenaRacing reads it to decide whose
	// exit a KindDone is while two processes wear one vendor id, and an ordinary
	// turn's handle landing in that map would send every ordinary exit down the
	// racer's branch.
	//
	// Added 2026-08-17, when the owner reversed §9.37's "an ordinary turn's seats
	// share one fate by design" line: that line was written for the four-seat
	// room, and in the five-seat room the most probable live failure is one
	// stalled vendor on an @all turn. The flat `handles` list that used to sit
	// beside this went with §9.54: its two consumers, cancel and teardown, were
	// all-or-nothing acts, and neither is any more — ctrl+c addresses one seat
	// and teardown walks the seats — so the keyed maps are the only record.
	seatHandles map[model.VendorID]racerHandle

	// arena marks a /arena turn: every racing seat is a FRESH one-shot session
	// in its own worktree (arena.go). The flag gates two things — the session-id
	// capture in applyEvents, because a race's throwaway ids must never replace
	// the room's saved threads, and the diff collection in finishColumn.
	arena      bool
	arenaBase  string
	arenaTrees map[model.VendorID]string
	// arenaRaceN is the number this race's names were minted with — read from
	// the repo's own arena refs at setup (arenaRaceNumber), NOT the turn
	// counter, because branches and worktrees outlive the room while the turn
	// counter resets with it. finishColumn derives the branch, the commit
	// subject and the result's RaceN from this field; deriving any of them
	// from Column.TurnN instead re-creates the collision the scan exists to
	// prevent — the two numbers agree only until a leftover pushes them apart.
	arenaRaceN int
	// arenaEphemeral holds the throwaway live-protocol sessions racing this
	// turn, keyed by vendor (today: the ACP seat, §9.37's deferred follow-up).
	//
	// On the TURN and never in m.procs, and the placement is the isolation: the
	// seat-process registry is the room's conversation — seatProcess would read
	// a racer parked there as the seat's live process and hand the NEXT ordinary
	// brief to a session that lives in a worktree and dies with the race. A
	// racer registered here instead is killed by finishColumn when its column
	// lands, and its context is the turn's rather than the room's, so every
	// teardown path that cancels the turn kills it as the backstop.
	arenaEphemeral map[model.VendorID]seatSession
	// arenaHandles keys this race's ONE-SHOT racer processes by vendor, for the
	// give-up key (`x`, program.go — §9.37, amended 2026-08-09). handles above
	// stayed a flat list until §9.54 on the argument that its two consumers,
	// cancel and teardown, were all-or-nothing acts; neither is now, and the
	// list is gone (see seatHandles). The give-up was the first act that
	// killed ONE racer while the others ran, and it had to land on the right
	// process — the second live
	// race measured why: one stuck seat held a decided race hostage for ~20
	// minutes because ctrl+c was the only exit and it cancels everything.
	// Arena turns only; the non-arena paths keep no per-vendor record because
	// no per-vendor act exists there (a persistent seat is already addressable
	// through ts.persistent and m.procs).
	arenaHandles map[model.VendorID]racerHandle
	// arenaSeeds holds each racer's .worktreeinclude receipt from setup, so
	// finishColumn can stamp it onto the ArenaResult it builds at landing —
	// seeding happened before the seat spawned, but the column that states it
	// is only born when the seat lands. Empty when the room repo has no
	// .worktreeinclude, and the render then draws no seed line at all.
	arenaSeeds map[model.VendorID]*SeedReport
	// arenaFinished counts racers that have landed, in the order the ROOM saw
	// them land — finishColumn call order, which is host-observed time, never a
	// vendor's own claim about when it finished (the host-stamps rule). Event
	// batching bounds the resolution: two seats landing in one drained batch
	// rank in batch order, which is the honest limit of what the room measured.
	arenaFinished int
	// arenaLive is the per-seat live-stat bookkeeping (arenalive.go), built
	// from arenaTrees so a seat whose worktree failed setup has no entry and
	// can never be read. ON THE TURN, deliberately: when the turn tears down
	// the whole map goes with it, which is what stops all refreshing at turn
	// end with no cleanup path to forget.
	arenaLive map[model.VendorID]*arenaLiveState
}

// racerHandle is the one-method slice of runner.Handle the give-up key drives.
//
// An interface for exactly seatSession's reason: the property under test is
// "the RIGHT seat's process was killed and the others' survived", and a test
// that needed four real children just to watch one be killed would be spawning
// processes to check a map lookup. runner.Handle is the only production
// implementation, and dispatch's two spawn-per-turn branches — the arena's and
// the ordinary turn's — are the only places that store one.
type racerHandle interface{ Kill() }

type eventBatchMsg struct{ events []runner.Event }

type dispatchFailedMsg struct {
	vendor model.VendorID
	note   string
}

// dispatch sends the draft to every seated vendor.
//
// Turn 1 is blind: each vendor receives the brief alone, with no other vendor's
// answer attached. Later turns resume each vendor's OWN session, so a vendor
// still only ever sees its own history — the independence guarantee is
// structural rather than a formatting convention (ADR-008 §4).
//
// This function used to open with `if m.turn != nil` and the notice "a turn is
// already in flight — ctrl+c cancels it". That line was the wall between a
// committee and a crew (§9.54): seats were concurrent inside one turn and the
// room was serial across turns. It is gone. What replaces it is narrower and
// per seat — a brief to a seat that is still answering is refused for THAT
// seat, inside sendTurn, and the idle seats it also named still go. The two
// room-wide refusals that remain are both about the arena, and each says why.
func (m *Model) dispatch() tea.Cmd {
	if race := m.race(); race != nil {
		// A race owns every worktree and every seat for as long as it runs: an
		// ordinary brief sent under it would hand a seat whose racer is writing
		// into a worktree a second prompt about the room's own tree. All or
		// nothing is the race's contract (§9.37), and this is that contract
		// read from the other side.
		m.st.Notice = "race t" + itoa(race.arenaRaceN) + " is in flight and owns every seat — ctrl+c cancels it, x gives up on one racer"
		return nil
	}
	if m.arenaPrep != nil {
		// A race is between the enter and the spawn: its worktrees are being
		// cut off the loop, which is exactly why the room is still reading this
		// keystroke at all. Refused with the same shape as the guard above,
		// because a second dispatch here would race two setups against one
		// repository and both would be cutting names from the same scan.
		//
		// Short on purpose: this notice shares the footer with the step line
		// rather than replacing it (modeLine), and a longer sentence would shed
		// the step to fit at 120 columns.
		m.st.Notice = "a race is already being prepared — ctrl+c stops it"
		return nil
	}
	if m.seatPrep != nil {
		// The same window for a seat's worktree being cut (seattree.go,
		// §9.55): the brief it stands in front of has not spawned, and a
		// second dispatch would cut names from the same repository under it.
		m.st.Notice = "worktrees are being prepared for a brief — ctrl+c stops it"
		return nil
	}
	activeFlow := m.flowChain != nil && m.flowChain.Current() != nil && m.flowDraft != ""
	if m.st.Draft == "" && !activeFlow {
		m.st.Notice = "nothing to dispatch: the brief is empty"
		return nil
	}

	arenaMode := false

	// Flows start only when the draft IS the /flow command. Bare "->" in prose
	// must never become a chain — that would turn ordinary briefs into
	// orchestrations — and neither may a word that merely opens with those five
	// letters: isFlowCommand applies roomcmd.go's one vocabulary rule here so
	// "/flowchart the auth path" is prose and " /flow/gate.log" is the escape
	// hatch §9.31 promises rather than a syntax error.
	var route Route
	var prompt string
	if activeFlow || isFlowCommand(m.st.Draft) {
		// Reuse an in-progress chain when the user just authorized a write gate
		// against the same draft; otherwise parse fresh.
		if !activeFlow {
			fc, err := ParseFlowChain(m.st.Draft)
			if err != nil {
				m.st.Notice = "flow syntax error: " + err.Error()
				return nil
			}
			m.flowChain = fc
			m.flowDraft = m.st.Draft
			m.flowWriteArmed = false
		}
		curr := m.flowChain.Current()
		stage := m.flowChain.Stage()
		if curr == nil || len(stage) == 0 {
			m.st.Notice = "flow has no current step"
			m.flowChain = nil
			m.clearFlowMarker()
			return nil
		}
		// Set before the gates below, not after them. Every path from here can
		// return without dispatching — a blocked write hop, a refused one — and
		// leaving the marker on the PREVIOUS hop would point at the seat that
		// already finished while the room waits on this one.
		//
		// The marker counts STAGES (§9.55): a fan of two hops is one hop on
		// the header, named by every seat in it (FlowSeats), because the two
		// run as one dispatch and land as one join.
		m.st.FlowHop = m.flowChain.StageN()
		m.st.FlowSteps = m.flowChain.Stages()
		m.st.FlowVendor = curr.Vendor
		m.st.FlowSeats = m.flowChain.FanLabel()
		// Posture comes from the STAGE. A hop with no declared target is a read
		// hop even in a --write room, and this is set before any of the paths
		// below can spawn anything. One posture per stage is what the parser
		// guarantees: it refuses a stage that mixes write and read hops.
		m.flowReadHop = !m.flowChain.StageWrites()

		// A write hop in a READ room is refused, not downgraded. Running it
		// read-only and reporting "returned" would be the room claiming to have
		// done work it structurally could not do; running it at all would be the
		// room granting itself authority the user withheld. Checked ahead of the
		// y/n gate because there is nothing to authorize — no keystroke here can
		// produce a legal dispatch.
		//
		// THE REMEDY IS /write, AND NAMING THE FLAG INSTEAD WAS THE §9.17 DEFECT
		// ITSELF. §9.17 quotes this very notice — "the notice says the room is
		// read-only and names the flag that would change it" — as the tell that a
		// control was trapped in a flag, and `/read`/`/write` were built to close
		// it. The sentence outlived the fix: the room went on telling a user with
		// a chain half-typed to quit and start over, which is the one outcome the
		// whole sweep exists to remove. It reports the POSTURE, not the launch
		// argv, because /read reaches this state too and "opened with --read"
		// would then be false as well as useless.
		if m.flowChain.StageWrites() && !m.st.Write {
			if curr.State == FlowStateQueued {
				_ = m.flowChain.MarkAwaitingWrite("the room is read-only; write hops need a room that can write — /write lets it")
			}
			m.flowWritePending = false
			m.flowWriteArmed = false
			m.st.Notice = fmt.Sprintf("flow blocked at step %d: %s is a write hop and the room is read-only — /write lets it, between turns",
				m.flowChain.StageN(), flowWriteTargets(stage))
			return nil
		}

		// Pre-dispatch write gate: Path marks write authority. Do not spawn the
		// seat until the user authorizes (y). A fanned stage is one gate naming
		// every write hop in it, because one y releases one dispatch.
		if m.flowChain.StageWrites() && !m.flowWriteArmed {
			if curr.State == FlowStateQueued {
				_ = m.flowChain.MarkAwaitingWrite("awaiting user authorization before write hop runs")
			}
			m.flowWritePending = true
			m.st.Notice = "flow write gate: y authorizes " + flowWriteTargets(stage) + " · n cancels"
			return nil
		}
		if curr.State == FlowStateBlocked && m.flowWriteArmed {
			if err := m.flowChain.ClearBlockForStart(); err != nil {
				m.st.Notice = "flow gate: " + err.Error()
				return nil
			}
		}
		// A hop goes to its own seats and waits on those seats alone (§9.54):
		// the next stage is dispatched the moment the previous stage's last
		// seat lands, whatever the rest of the room is doing. What a hop cannot
		// do is take a seat that is still answering something else — an
		// ordinary brief the operator sent to it while the earlier hop ran.
		// That is refused here, before Start would mark the step running, and
		// it ends the chain rather than queueing behind the seat: a chain that
		// dispatched itself later, when a seat happened to free up, would be
		// the room acting on its own at a moment nobody chose, which is the
		// exact thing the hop marker on the header exists to prevent (§9.16).
		// The refusal names the seat and the turn it is on, and the whole
		// chain is retired so the next enter is the operator's brief and not
		// the corpse's (§9.35). A fan is checked seat by seat: one busy seat
		// stops the whole stage, because a stage that ran two of its three
		// hops would not be the stage the operator typed.
		for _, s := range stage {
			if ts := m.turnOf(s.Vendor); ts != nil {
				m.st.Notice = fmt.Sprintf("flow stopped at hop %d/%d: @%s is still on turn %d — ctrl+c on its column cancels that, then send the chain again",
					m.flowChain.StageN(), m.flowChain.Stages(), s.Vendor, ts.n)
				m.endFlowChain()
				return nil
			}
		}
		route = Route{}
		for _, s := range stage {
			route.Vendors = append(route.Vendors, s.Vendor)
		}
		if m.seatedIn(route) == 0 {
			// Every seat this stage names is out of the room. Ended here rather
			// than left Running, so the next enter is the operator's brief.
			m.st.Notice = "none of the seats this hop names are seated — flow ended"
			m.endFlowChain()
			return nil
		}
		m.flowWriteArmed = false
		m.flowWritePending = false
		// A writing stage in a writing room may need seats' worktrees cut first
		// (seattree.go, §9.55). The cut runs off the loop and the stage is
		// launched when it lands; Start runs THEN, so a write hop's baseline is
		// read in the tree its seat will actually write into.
		if need := m.seatsNeedingTrees(route); len(need) > 0 {
			return m.beginSeatSetup(need, m.launchFlowStage)
		}
		return m.launchFlowStage()
	} else {
		m.endFlowChain()
		if brief, ok := parseCommand(m.st.Draft, "/arena"); ok {
			// The drop verb is caught before anything a race needs, because it
			// is not a race: nothing spawns, nothing is billed, and no posture
			// applies — deleting a kept worktree is the USER acting on the
			// user's own receipt (lifecycle.go), so a read-only room may do it
			// exactly as it may /cd. Only the exact two-word form is taken
			// (parseArenaDrop); anything longer is a brief and races as prose.
			if seat, force, isDrop := parseArenaDrop(brief); isDrop {
				m.arenaDrop(seat, force)
				return nil
			}
			// The record verb, caught here for the drop verb's reasons and one
			// more: it does not even read a worktree, only two `for-each-ref`
			// scans, so it runs in a read-only room and DURING a turn as well
			// (§9.47). Only the exact one-word form is taken — anything longer
			// after /arena is a brief and races as prose, the vocabulary rule the
			// drop verb keeps one line up.
			if strings.TrimSpace(brief) == "record" {
				m.arenaRecordCommand()
				return nil
			}
			// The check verb, caught here for the record verb's reasons: it
			// spawns nothing, bills nothing and mutates no worktree — it names
			// the command a LATER race will run. It is the one /arena sub-verb
			// that takes free text, so the guard that keeps it from stealing a
			// brief is inside it rather than in the parse (arenacheck.go).
			if arg, isCheck := parseArenaCheck(brief); isCheck {
				m.arenaCheckCommand(arg)
				return nil
			}
			// A race is a dispatch, not room state, so it lives here beside
			// /flow rather than in roomCommand — and like a flow write hop, it
			// cannot run in a room that may not write: every racing seat gets
			// write posture, contained by its worktree rather than by a flag.
			if !m.st.Write {
				m.st.Notice = "the room is read-only and /arena races writing seats — /write lets it, between turns"
				return nil
			}
			if strings.TrimSpace(brief) == "" {
				m.st.Notice = "/arena needs a brief — /arena <brief> races every seat in its own worktree"
				return nil
			}
			arenaMode = true
			prompt = strings.TrimSpace(brief)
			// Everyone seated races. Routing a race (@codex-only arenas) is
			// deliberately absent from v1: the value is the comparison, and a
			// one-seat race is an ordinary turn in a worktree — /cd does that.
			route = Route{}
		} else {
			route, prompt = ParseRoute(m.st.Draft)
		}
	}
	if route.Mixed {
		// Checked before the empty-brief case on purpose. A draft that mixes
		// the two forms cannot be routed at all, so telling the user to add a
		// brief would send them back to a line that is going to be refused for
		// a second reason the moment they do.
		m.st.Notice = "@ narrows and -@ excludes — use one form, not both"
		return nil
	}
	if prompt == "" {
		m.st.Notice = "that is a mention with no brief after it"
		return nil
	}
	if n := m.seatedIn(route); n == 0 {
		// Addressed only to vendors that are not seated. Dispatching to nobody
		// while the columns sat idle would look like the key did nothing.
		m.st.Notice = "none of the vendors you addressed are seated"
		return nil
	}

	if arenaMode {
		// A race is one turn across every seat, all or nothing (§9.37), so it
		// needs the whole room idle — the one place §9.54's per-seat rule does
		// not apply. A race that skipped a busy seat would not be a comparison,
		// and a race that took one would hand a seat two prompts at once. The
		// refusal names who is busy and on what, so the operator knows whether
		// to wait or to cancel.
		if busy := m.busySeats(); len(busy) > 0 {
			m.st.Notice = busy + " — a race needs every seat idle; ctrl+c on a column cancels that seat's turn"
			return nil
		}
		// A race cuts one worktree per seat before anything spawns, and that
		// work runs OFF this loop (arenasetup.go, §9.37 amended 2026-08-17):
		// dispatch stops here, the room keeps drawing and reading keys while
		// git works, and applyArenaSetup calls sendTurn below with whatever the
		// setup measured. Every refusal above ran first, so nothing is prepared
		// for a race the room was going to turn down anyway.
		return m.beginArenaSetup(route, prompt)
	}
	// A writing brief in a writing room may need a seat's worktree cut before
	// it spawns (seattree.go, §9.55). The cut runs off the loop, exactly as a
	// race's does, and the brief is sent when it lands; a seat that already
	// has its tree — every turn after its first — spawns here, synchronously,
	// as it always did.
	launch := func() tea.Cmd { return m.sendTurn(route, prompt, nil) }
	if need := m.seatsNeedingTrees(route); len(need) > 0 {
		return m.beginSeatSetup(need, launch)
	}
	return launch()
}

// busySeats names every seat still answering, with the turn it is on, in the
// words a room-wide refusal opens with: "a turn is in flight on codex (turn 4),
// grok (turn 5)". Seating order, so the sentence reads the way the grid does.
// Empty when the room is idle.
//
// The turn number is the dispatch the seat is answering — the number the
// reader can find at the top of that column and on its separator. Before
// §9.54 every one of these refusals said "a turn is in flight" and stopped,
// because there was one turn and it was the room's; now there can be several
// and the reader is owed WHICH seats are holding the room, so they know
// whether to wait or to cancel one.
func (m *Model) busySeats() string {
	var parts []string
	for _, c := range m.st.Columns {
		if ts := m.turnOf(c.Vendor); ts != nil {
			parts = append(parts, string(c.Vendor)+" (turn "+itoa(ts.n)+")")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "a turn is in flight on " + strings.Join(parts, ", ")
}

// seatBusy is busySeats for ONE seat, in the words a per-seat refusal opens
// with: "a turn is in flight on Claude Code (turn 4)". Empty when it is idle.
func (m *Model) seatBusy(c *Column) string {
	ts := m.turnOf(c.Vendor)
	if ts == nil {
		return ""
	}
	return "a turn is in flight on " + c.Label + " (turn " + itoa(ts.n) + ")"
}

// sendTurn is the half of a dispatch that actually spawns: the turn's geometry
// and clock, one process — or one write to a stdin already open — per addressed
// seat, and the wait on their events.
//
// It is reached two ways, and the split is what took the arena's worktree setup
// off the render loop. An ordinary turn comes straight from dispatch with race
// nil. A race arrives here later, from applyArenaSetup, carrying the finished
// setup — so everything stamped below (the turn's start time, the snapshot of
// the previous replies, the turn's context) is stamped when the SEATS start
// rather than when the operator pressed enter, which is the honest reading of
// every clock this turn will render.
func (m *Model) sendTurn(route Route, prompt string, race *arenaSetupResult) tea.Cmd {
	// The registry as THIS room sees it: a seat that retreated to its batch
	// adapter earlier in the room is that adapter here (fallback.go).
	reg := m.registry()
	// A fanned flow stage hands each seat its own task (launchFlowStage,
	// §9.55). Consumed here, on the way in, so no later dispatch can inherit
	// a prompt meant for a stage that already ran.
	fan := m.fanPrompts
	m.fanPrompts = nil

	// The seats this brief names that are still answering an earlier one
	// (§9.54). Decided BEFORE anything below moves, because every one of the
	// refusals here has to leave the room exactly as it found it: a brief that
	// reaches only busy seats keeps its draft and dispatches nothing, and the
	// rebuild, the geometry and the columns are all untouched by a press of
	// enter that sent nothing.
	//
	// Per seat and never per room. A busy seat is refused and named; an idle
	// seat the same brief names still goes. A persistent seat is exactly the
	// case that makes this a rule rather than a courtesy: the stream-json and
	// ACP processes hold ONE turn open at a time, and a second prompt written
	// into a process mid-turn is the failure the old room-wide wall was standing
	// in front of. The wall is gone; this is what stood behind it.
	var busy []string
	for _, c := range m.st.Columns {
		if !m.st.seats(c) || !route.addresses(c.Vendor) {
			continue
		}
		if ts := m.turnOf(c.Vendor); ts != nil {
			busy = append(busy, string(c.Vendor)+" (turn "+itoa(ts.n)+")")
		}
	}
	if len(busy) > 0 && len(busy) == m.seatedIn(route) {
		// Every seat the brief reached is busy, so there is nothing to send and
		// the draft stays where it was typed. The remedy is per seat, and it is
		// the focused-seat ctrl+c rather than the room's: naming the whole-room
		// cancel here would tell an operator who wanted grok to stop codex.
		m.st.Notice = "a turn is in flight on " + strings.Join(busy, ", ") +
			" — ctrl+c on its column cancels that turn, or address another seat"
		return nil
	}

	// The first brief retires the room-open rebuild (rebuild.go): startTurn is
	// about to clear every per-turn field including the note the rebuild wrote,
	// and a run left standing would go on owning events for a seat this turn is
	// now driving.
	m.endRebuild()

	// Geometry for this turn is decided here, from the route, and stays until
	// the next dispatch. Empty FrameOwners = equal columns (@all / everyone).
	//
	// Since §9.54 the owners are the route's seats AND the seats still
	// answering an earlier brief (frameOwnersFor reads both off State). The
	// no-mid-stream-reflow rule is about the room moving because a VENDOR did
	// something; a dispatch is the operator's own act, and a brief to grok
	// while codex streams is a statement that both answers are wanted at
	// reading width — narrowing the seat that is mid-answer to make room would
	// be reflowing prose under the reader on a key that says nothing about it.
	m.st.FrameOwners = frameOwnersFor(route, m.st)

	// Turn 1 is blind no matter what is armed: the whole value of the room is
	// three opinions formed without sight of each other, and a first round that
	// quoted anything would have nothing to quote but would still establish the
	// wrong precedent in the code.
	// One timestamp for the whole dispatch, so the columns are measured against
	// the same starting line. Reading the clock per column would make the
	// vendor that happened to be constructed last look marginally faster.
	now := time.Now()
	quoting := m.st.Quote && m.st.Turn > 0
	// Snapshotted BEFORE any column is reset, because the loop below clears the
	// bodies it would otherwise be quoting.
	//
	// The snapshot is taken at THIS dispatch, per seat it addresses (§9.54): a
	// rebuttal quotes the others' last answers as they stood the moment this
	// seat's turn was sent. A neighbour that is still answering has no last
	// answer to quote yet — its body is a partial reply, and quoting a reply
	// the vendor has not finished would put half an argument in front of
	// another model as though it were whole. So a busy seat contributes its
	// last FILED turn instead (settledReply), which is the most recent thing it
	// actually said, and nothing at all if it has never finished a turn.
	priorReplies := make([]Column, 0, len(m.st.Columns))
	for _, c := range m.st.Columns {
		priorReplies = append(priorReplies, m.settledReply(c))
	}

	ctx, cancel := context.WithCancel(context.Background())
	ts := &turnState{
		n:          m.st.Turn + 1,
		route:      route,
		flow:       m.flowChain != nil,
		cancel:     cancel,
		ctx:        ctx,
		seatCancel: map[model.VendorID]context.CancelFunc{},
		live:       map[model.VendorID]bool{},
		persistent: map[model.VendorID]bool{},
		prompts:    map[model.VendorID]string{},
	}
	var failures []dispatchFailedMsg

	// What the transcript echoes, for every column this turn touches. The
	// user's brief with the mentions stripped — the same text the vendors are
	// asked about — through the one sanitize choke point everything else on
	// State goes through. Not redacted: see promptEcho.
	echo := sanitize(prompt)
	// Per seat when a stage fans: the column's echo is the task THAT seat was
	// handed, not the first hop's.
	echoFor := func(v model.VendorID) string {
		if p, ok := fan[v]; ok {
			return sanitize(p)
		}
		return echo
	}
	next := ts.n

	// Worktrees were added BEFORE any seat spawns, and the base SHA read once so
	// all attempts race from the same commit — all of it already done by the
	// time this runs, off the loop (arenasetup.go). A workspace that is not a
	// git repo, or a setup that met the deadline, never reaches here at all: it
	// lands on the notice and the room goes back to composing. What can still
	// arrive is a PARTIAL race — a single seat whose worktree could not be added
	// is skipped and told why, because a partial race still answers the brief
	// (§4a.1's degrade-the-field rule, one level up).
	var arenaSeatErr map[model.VendorID]string
	if race != nil {
		trees := race.trees
		ts.arena, ts.arenaRaceN, ts.arenaBase, ts.arenaTrees, ts.arenaSeeds, arenaSeatErr =
			true, race.raceN, race.base, trees, race.seeds, race.seatErr
		// One live-stat slot per seat that actually has a tree to read. Seats
		// skipped above (worktree add failed) are absent here too, which is the
		// never-refresh-a-failed-setup rule enforced by construction rather
		// than by a check somewhere a refactor could lose it.
		ts.arenaLive = map[model.VendorID]*arenaLiveState{}
		for v := range trees {
			ts.arenaLive[v] = &arenaLiveState{}
		}
		// The race's receipt, for the end-of-life verbs (/adopt, /arena drop —
		// lifecycle.go). Recorded here, at creation, because Column.Arena is a
		// per-turn fact the next dispatch clears while the worktrees are kept
		// until the user deletes them (§9.37) — the verbs need a target that
		// lives as long as the trees do. A copy of the map, not the map: drop
		// deletes entries from the receipt, and ts.arenaTrees still describes
		// this turn's dispatch.
		raceTrees := make(map[model.VendorID]string, len(trees))
		for v, tr := range trees {
			raceTrees[v] = tr
		}
		// The setup's OWN workspace, not the room's. The room stayed usable
		// while the trees were being cut, so `/cd` could have moved it since —
		// and the trees are siblings of where the setup ran (arenaSetupResult).
		m.lastRace = &arenaRace{workspace: race.workspace, raceN: race.raceN, base: race.base, trees: raceTrees}
	}

	for i := range m.st.Columns {
		c := &m.st.Columns[i]
		if !m.st.seats(*c) {
			continue
		}
		if m.turnOf(c.Vendor) != nil {
			// Still answering an earlier brief. Untouched, whether or not this
			// brief named it: every per-turn field on its column and every
			// per-seat map below describes the turn it is on, and its refusal
			// — if it was addressed — was already worded above. Not even the
			// "not addressed" note: that note is about a turn the seat sat out,
			// and a seat mid-answer is not sitting anything out.
			continue
		}
		// Cleared for every column the loop reaches, BEFORE any of the paths
		// below can skip one. A refused thread belongs to the turn that refused
		// it, and a flag left set on a seat that is merely unaddressed — or that
		// fails to dispatch for an unrelated reason — would suppress the next
		// turn's genuine failure note.
		delete(m.threadLost, c.Vendor)
		delete(m.failure, c.Vendor)
		// Same lifetime, same reason: the id this seat was asked to resume is a
		// fact about ONE dispatch, and a stale entry would let a later turn's
		// perfectly ordinary new conversation be compared against an id nobody
		// asked for on it.
		delete(m.forkWatch, c.Vendor)
		// The give-up and the cancel outlive the seat's turn on purpose
		// (Model.givenUp) and end here, at the seat's next dispatch, so an
		// echo from the stopped process can never re-label the turn being sent.
		delete(m.givenUp, c.Vendor)
		delete(m.cancelling, c.Vendor)
		if !route.addresses(c.Vendor) {
			// Not in this turn. Its previous reply stays on screen, because
			// that is still the last thing this vendor said — but the note
			// makes clear it is not participating, so a stale answer beside two
			// fresh ones cannot be mistaken for a third opinion on the new
			// brief.
			//
			// Nothing is recorded for it either: startTurn is never reached, so
			// its transcript holds the turns it actually took and skips straight
			// from turn 2 to turn 5 if that is what happened. A history entry
			// for a turn this seat sat out would be the room inventing a
			// conversation.
			c.Note = "not addressed in turn " + itoa(next)
			// Flagged, not merely worded: this note is about a turn the seat sat
			// out, which is neither a fact about the turn it last took nor a
			// failure. Both consequences are on Column.Skipped.
			c.Skipped = true
			continue
		}
		v, ok := reg[c.Vendor]
		if !ok {
			// A seat detected but not yet drivable. Said out loud rather than
			// left looking idle: this is the honest shape of a half-built
			// feature, and it disappears as each adapter lands.
			c.Phase = PhaseFailed
			c.Note = "no adapter yet — this column arrives in a later build"
			continue
		}

		// Each vendor may receive a DIFFERENT prompt on a quoting turn, since
		// each one is shown the others' answers and not its own — and on a
		// fanned stage, where each hop is its own task.
		vendorPrompt := prompt
		if p, ok := fan[c.Vendor]; ok {
			vendorPrompt = p
		}
		if quoting {
			vendorPrompt = BuildRebuttalPrompt(vendorPrompt, *c, priorReplies)
		}
		// Where this seat's process is about to run, stamped on the column
		// (seattree.go, §9.55) — the badge that says which containment holds.
		// Stamped before the spawn, from the same read the spawn uses
		// (seatDir), so the two cannot disagree. A racer's is its arena tree,
		// stamped below once that tree is known to exist.
		if !ts.arena {
			c.Containment = m.containmentFor(c.Vendor)
		}

		// A seat with a long-lived process is handed the turn on the stdin it
		// already has open. A seat without one pays a fresh spawn, as before.
		//
		// note is carried past the column reset below, because that reset is
		// what clears the PREVIOUS turn's note and this one is about THIS turn.
		note := ""
		// One context per SEAT, a child of the dispatch's, so cancelling this
		// seat (ctrl+c on its column, §9.54) kills this seat's child and no
		// neighbour's. Minted before the spawn because runner.Start kills on it,
		// and recorded on the turn so the seat's retirement can pull it.
		sctx, scancel := context.WithCancel(ctx)
		ts.seatCancel[c.Vendor] = scancel
		if ts.arena {
			// Every racing seat — the persistent one included — is a fresh
			// one-shot session in its worktree, through the same FirstTurn every
			// vendor already implements. The room's live process and saved
			// threads are untouched: a race is a parallel universe for one turn,
			// not a detour the conversation has to survive. PostureWrite for all,
			// stated plainly: a one-shot process has no channel to be asked on,
			// so the gate cannot exist here, and the containment is the worktree
			// — which is the whole reason the worktree exists.
			tree, ok := ts.arenaTrees[c.Vendor]
			if !ok {
				why := arenaSeatErr[c.Vendor]
				if why == "" {
					why = "worktree could not be added"
				}
				failures = append(failures, dispatchFailedMsg{c.Vendor, "arena: " + why})
				continue
			}
			c.Containment = ContainClaim{Level: ContainSeatTree, Branch: arenaBranch(ts.arenaRaceN, c.Vendor)}
			// The one turn shape where the room adds words to a brief. Every
			// racer gets the SAME constant line ahead of the same brief, so
			// the comparison the race exists for is untouched — see
			// arenaConduct for the ruling and the incident that forced it.
			vendorPrompt = arenaConduct + "\n\n" + vendorPrompt
			// The race, carried into the turn trace on every racer's spec
			// (runner.Spec.Race). Stamped HERE, on both arms below, because
			// this is the last point that still knows a spawn is an attempt:
			// the record is written by the runner's clock when the process
			// exits, on its own goroutine, long after this turn's state is
			// gone. Without it a racer's trace line is byte-identical in
			// shape to an ordinary turn's line for the same seat, which is
			// the gap STATE.md recorded on 2026-08-15/16.
			raceTag := arenaRaceTag(ts.arenaRaceN)
			if cv, ok := v.(vendors.Conversational); ok {
				// The one seat FirstTurn cannot carry. The ACP refounding made
				// this vendor live-only (§9.36) and the first live race duly
				// surfaced its refusal on the column (§9.37's verification
				// note), so its race is the sanctioned follow-up built as
				// specified: a THROWAWAY ACP session in the racer's worktree —
				// one process, one session, one prompt, killed when the column
				// lands. §9.36's own machinery pointed at a throwaway session,
				// not a second protocol.
				sess, err := m.startEphemeralRacer(sctx, cv, c, tree, vendorPrompt, raceTag)
				if err != nil {
					failures = append(failures, dispatchFailedMsg{c.Vendor, err.Error()})
					continue
				}
				if ts.arenaEphemeral == nil {
					ts.arenaEphemeral = map[model.VendorID]seatSession{}
				}
				ts.arenaEphemeral[c.Vendor] = sess
			} else {
				spec, err := v.FirstTurn(vendorPrompt, tree, c.Binary, vendors.PostureWrite)
				if err != nil {
					failures = append(failures, dispatchFailedMsg{c.Vendor, err.Error()})
					continue
				}
				spec.Race = raceTag
				h, err := startProcess(sctx, spec, m.events, v.ParseEvent)
				if err != nil {
					failures = append(failures, dispatchFailedMsg{c.Vendor, err.Error()})
					continue
				}
				// Keyed by vendor, so `x` can kill THIS racer and no other
				// (turnState.arenaHandles) — and since §9.54 so can ctrl+c on
				// the focused seat, and teardown walks the same map. A second
				// Kill on an already-killed handle is a no-op by Handle's own
				// contract.
				if ts.arenaHandles == nil {
					ts.arenaHandles = map[model.VendorID]racerHandle{}
				}
				ts.arenaHandles[c.Vendor] = h
			}
		} else {
			// The prompt as handed, kept for a retreat that happens after
			// this loop has moved on (turnState.prompts).
			ts.prompts[c.Vendor] = vendorPrompt
			batch := v
			if liveSeat(v) {
				n, err := m.sendPersistentTurn(v, c, vendorPrompt)
				switch {
				case errors.Is(err, errFellBack):
					// The handshake was refused before this brief could be
					// handed over, and the seat has retreated (retreat). The
					// same brief goes down the batch branch below, on this
					// same press of enter: a refusal the operator has to
					// notice and re-send would be the brief lost.
					batch = m.fallbackFor(c.Vendor)
					note = fallbackNote(c.Vendor)
				case err != nil:
					failures = append(failures, dispatchFailedMsg{c.Vendor, err.Error()})
					continue
				default:
					ts.persistent[c.Vendor] = true
					note = n
					batch = nil
				}
			}
			if batch != nil {
				if err := m.startBatchSeat(ts, sctx, batch, c, vendorPrompt); err != nil {
					failures = append(failures, dispatchFailedMsg{c.Vendor, err.Error()})
					continue
				}
			}
		}

		ts.live[c.Vendor] = true
		// The finished turn goes to history and everything describing it is
		// reset — the line that used to be five assignments erasing the
		// previous answer off the screen.
		c.startTurn(next, echoFor(c.Vendor), quoting)
		c.Phase = PhaseStreaming
		// GranUnknown lands in Waiting alongside GranFinalOnly, and the
		// asymmetry is the point. PhaseStreaming asserts "output is arriving and
		// you are seeing it as it lands" — a claim an unestablished vendor has
		// not earned. Opening in Waiting and upgrading on the first chunk
		// (applyEvents) is honest in that direction only, so an unknown vendor
		// gets the modest claim and is promoted by evidence.
		if c.Gran == GranFinalOnly || c.Gran == GranUnknown {
			c.Phase = PhaseWaiting
		}
		c.Note = note
		// A persistent process reports a RUNNING TOTAL, not this turn's spend.
		// Measured: two turns of one process reported $0.1061493 then
		// $0.1177296 while the per-turn usage block stayed at 2 input tokens
		// both times. Rendering that as a turn cost would be a false figure, and
		// subtracting one from the other would be council inventing a number —
		// so the badge says which one it is instead.
		c.CostSession = ts.persistent[c.Vendor]
		c.Started = now
		m.redactors[c.Vendor] = &Redactor{}
	}

	for _, f := range failures {
		if c := m.column(f.vendor); c != nil {
			// A seat that could not be dispatched to still TOOK this turn: the
			// brief was addressed to it and it has a failure to report. So its
			// previous answer is filed with the turn it belongs to rather than
			// left under the new turn's separator, where it would read as this
			// brief's reply with someone else's failure note stapled underneath.
			c.startTurn(next, echoFor(c.Vendor), quoting)
			c.Phase = PhaseFailed
			c.Note = f.note
			// Terminal without ever being live, so this is the one retirement
			// finishColumn does not stamp: the inbox (needsyou.go) reads it.
			c.Ended = now
			// No process was ever started, so the vendor was never asked about
			// the conversation. That is the pre-flight class in its strongest
			// form — one step earlier than the stderr cases, which at least got
			// as far as the binary — and it is why a seat that cannot be
			// dispatched at all no longer forfeits a saved thread it was never
			// given the chance to use.
			m.failure[c.Vendor] = runner.FailurePreflight
			// This column never reaches finishColumn — it never entered a live
			// phase — so its restored thread is settled here instead. Without
			// this, a seat that could not be dispatched at all would keep a
			// restored id on probation forever.
			m.settleRestoredThread(c)
		}
	}

	if len(ts.live) == 0 {
		cancel()
		m.st.Notice = "no vendor could be dispatched to — see the columns"
		if len(busy) > 0 {
			m.st.Notice = "a turn is in flight on " + strings.Join(busy, ", ") + " — and no other seat could be dispatched to; see the columns"
		}
		return nil
	}

	// Every seat that entered live now points at this dispatch (Model.turns).
	// This is the line that used to read `m.turn = ts`.
	m.holdTurn(ts)
	// The header carries this until the last column of THIS dispatch lands
	// (§9.21, as amended by §9.54: the cell names the most recent dispatch, and
	// an older one still in flight is counted rather than named). Set HERE and
	// not beside FrameOwners, even though both are the turn's intent captured at
	// the one moment it is known: everything above this line can still refuse
	// the dispatch, and a route on the header of a turn that never started would
	// be the room reporting a spend that never happened. The two also have
	// opposite lifetimes — the geometry outlives the turn so nothing reflows
	// under a reader, the route is retired the moment the turn ends.
	sent := route
	m.st.TurnRoute = &sent
	m.st.Turn = ts.n
	if m.st.Page.Open {
		// Dispatching from the by-turn page lands on the turn just sent.
		//
		// This is not §7.1 rule 4's exception, it is its condition: that rule
		// forbids the view moving because a VENDOR did something, and this moves
		// it because the user pressed enter. A brief is a statement about what
		// you want to read next, and a projection that answered it by staying on
		// turn 7 would be the room showing an old conversation while spending
		// quota on a new one (§9.22).
		m.openPage(m.st.Turn)
	}
	m.st.Mode = ModeViewing
	if race == nil {
		// A race cleared the composer when the operator pressed enter, several
		// seconds and one worktree setup ago (beginArenaSetup) — and may have
		// been typed into since, because the room stayed usable throughout.
		// Clearing again here would delete a draft this turn was never about.
		m.setDraft("")
	}
	m.st.Notice = ""
	if len(busy) > 0 {
		// A partial send says so: who took the brief and who was skipped, and
		// why. Measured, both halves — the seats in ts.live and the seats whose
		// turn refused them — and the remedy is the per-seat one.
		var sent []string
		for _, c := range m.st.Columns {
			if ts.live[c.Vendor] {
				sent = append(sent, string(c.Vendor))
			}
		}
		m.st.Notice = "sent to " + strings.Join(sent, ", ") + " — skipped: " + strings.Join(busy, ", ") +
			", still on a turn; ctrl+c on its column cancels it"
	}
	return m.waitEvents()
}

// holdTurn registers a dispatch on every seat it put in flight. It is the one
// writer of Model.turns besides the seat's own retirement, and the shape tests
// build a turn in flight with — which is why it tolerates a nil map: a Model a
// test types out as a literal has none, and the constructor is the only other
// place the map is made.
func (m *Model) holdTurn(ts *turnState) {
	if m.turns == nil {
		m.turns = map[model.VendorID]*turnState{}
	}
	for v := range ts.live {
		m.turns[v] = ts
	}
	// The --record file sees every dispatch through this one writer of
	// Model.turns (recording.go). A nil recorder is a no-op.
	m.recordDispatch(ts)
}

// settledReply is what a rebuttal may quote from this column at THIS moment
// (§9.54): the column itself when its last turn has ended, its last filed turn
// while a new one is still arriving, and an unquotable blank when it has never
// finished one.
//
// A seat mid-answer has a body, and that body is exactly what must not be
// quoted — it is a reply the vendor has not finished, and another model reading
// it as "what participant A said" would be rebutting half an argument. The
// filed record is the seat's most recent COMPLETE answer, which is the claim the
// quote makes. A column with no record and a live turn contributes a blank that
// quotable() refuses, so its participant letter is still assigned (the labels
// are positional and must not shuffle) and nothing is said in its name.
func (m *Model) settledReply(c Column) Column {
	if m.turnOf(c.Vendor) == nil {
		return c
	}
	out := Column{Vendor: c.Vendor, Label: c.Label, Avail: c.Avail}
	if n := len(c.History); n > 0 {
		last := c.History[n-1]
		out.Body, out.Phase, out.TurnN = last.Body, last.Phase, last.N
	}
	return out
}

// posture is what the room is currently asking vendors for.
//
// A /flow READ hop overrides it downward and only downward. Posture is a
// property of the STEP, not of the room: a chain whose first hop is "@codex
// review security" must not hand codex write authority merely because the room
// was started with --write and a LATER hop needs it. The reverse — a write hop
// lifting a read room — is refused in dispatch before anything spawns, so there
// is no upward case for this function to express.
func (m *Model) posture() vendors.Posture {
	if m.flowReadHop {
		return vendors.PostureRead
	}
	if m.st.Write {
		return vendors.PostureWrite
	}
	return vendors.PostureRead
}

// seatPosture is what a seat that can ASK is invoked with.
//
// Split from posture() rather than folded into it, because the gate is not a
// room-wide property and must never render as one. Three of the four vendors
// are batch CLIs with no channel to ask on; giving them a gated posture would
// be a flag that does nothing behind a badge that claims something.
func (m *Model) seatPosture() vendors.Posture {
	// A flow read hop is read for the seat too. Not "write, but gated": a gate
	// the user has to answer is still an offer of write authority this hop was
	// never granted, and the badge would claim one.
	if m.flowReadHop {
		return vendors.PostureRead
	}
	// m.st.Asking, not m.opts.Auto: the flag only SEEDS this at launch, and `a`
	// moves it afterwards. Reading the flag here would spawn a gated process for
	// a room that has stopped asking, so the seat would raise cards nobody is
	// answering — a gate whose only effect is to block.
	if m.st.Write && m.st.Asking() {
		return vendors.PostureWriteGated
	}
	return m.posture()
}

// seatedIn counts how many seated columns a route actually reaches.
//
// One line, because the footer quotes this number before enter is pressed and
// the renderer cannot reach a *Model: the arithmetic moved to State.SeatsIn so
// the bill and the dispatch cannot be computed two different ways (§9.21).
func (m *Model) seatedIn(route Route) int { return m.st.SeatsIn(route) }

// frameOwnersFor lists the visible seated vendors that own column width for
// this turn. Empty means equal four-up — @all, everyone, or a route that
// happens to address every seat still on screen.
//
// A seat still answering an earlier brief owns width too (§9.54). Read off the
// column's own phase rather than off Model.turns, so this stays a function of
// State: a column that is Busy or Settling is one whose answer is arriving,
// and a dispatch that narrowed it to make room for the new seat would reflow
// a reply under the reader on a key that said nothing about that column.
func frameOwnersFor(route Route, st State) []model.VendorID {
	if route.Mixed {
		return nil
	}
	var out []model.VendorID
	for _, idx := range st.VisibleColumns() {
		c := st.Columns[idx]
		if route.addresses(c.Vendor) || c.inFlight() {
			out = append(out, c.Vendor)
		}
	}
	if len(out) == 0 || len(out) == len(st.VisibleColumns()) {
		return nil
	}
	return out
}

// itoa is strconv.Itoa under a shorter name, kept local so the dispatch path
// reads as prose.
func itoa(i int) string { return strconv.Itoa(i) }

// startBatchSeat spawns one seat's turn as a fresh process — the shape every
// seat took before any of them kept a process, and the shape three of them
// retreat to when their live handshake is refused (vendors.LiveFallback).
//
// It is the body sendTurn's batch branch always had, lifted out so it has TWO
// callers rather than a copy: the dispatch loop, and a retreat that happens
// after that loop has moved on (retreatSeat). The handle lands in
// turnState.seatHandles so `x`, the focused-seat ctrl+c and teardown reach it
// exactly as they reach any one-shot seat.
func (m *Model) startBatchSeat(ts *turnState, sctx context.Context, v vendors.Vendor, c *Column, prompt string) error {
	spec, resumed, err := m.specFor(v, c, prompt)
	if err != nil {
		return err
	}
	h, err := startProcess(sctx, spec, m.events, v.ParseEvent)
	if err != nil {
		return err
	}
	// Keyed by vendor, so `x` can cut THIS seat and no other on an
	// ordinary turn (turnState.seatHandles), and since §9.54 so can the
	// focused-seat ctrl+c; teardown walks the same map. A give-up's Kill
	// on an already-killed handle is a no-op by Handle's own contract.
	if ts.seatHandles == nil {
		ts.seatHandles = map[model.VendorID]racerHandle{}
	}
	ts.seatHandles[c.Vendor] = h
	// The id half of §9.43's comparison, recorded at the one moment it is
	// known and ONLY for a seat whose vendor has been measured to fork a
	// lost thread in silence. Gating here rather than at the comparison is
	// deliberate: it keeps applyEvents free of vendor knowledge, and it
	// makes the honesty rule structural — a seat that never enters this
	// map can never raise the card, so a vendor whose resume semantics
	// nobody has measured cannot be accused of losing a thread.
	if resumed != "" {
		if _, forks := v.(vendors.SilentResumeFork); forks {
			m.forkWatch[c.Vendor] = resumed
		}
	}
	return nil
}

// specFor builds this vendor's invocation for the current turn.
//
// A vendor with a session id resumes it; one without starts fresh. A resume
// that the vendor refuses is not silently downgraded — ErrNoResume falls back
// to a first turn, and the column says the thread was lost.
//
// The second return is the id this invocation actually asked the vendor to
// resume, empty on a first turn. It is returned rather than recorded here
// because it is the ONE fact a caller cannot re-derive from the Spec — the id is
// buried in a vendor-specific argv position — and because a method that quietly
// wrote model state would fire on the several tests that call this directly to
// inspect an invocation. What the caller does with it is §9.43's comparison.
func (m *Model) specFor(v vendors.Vendor, c *Column, prompt string) (runner.Spec, string, error) {
	p := m.posture()
	// The directory the process runs in: the seat's own worktree in a writing
	// room, the workspace otherwise (seatDir, §9.55).
	dir := m.seatDir(c.Vendor)
	if id := m.sessions[c.Vendor]; id != "" {
		// Resume: the brief is already in this vendor's own history.
		spec, err := v.NextTurn(prompt, dir, c.Binary, id, p)
		if err == nil {
			return spec, id, nil
		}
	}
	// First turn for THIS vendor, so it gets the operating context. Per vendor
	// rather than per room: a seat added to a later turn is still a stranger,
	// and would otherwise be the only one guessing.
	spec, err := v.FirstTurn(m.brief.Apply(prompt), dir, c.Binary, p)
	return spec, "", err
}

// waitEvents blocks on one event, then drains what is already queued into a
// single batch. One redraw per batch instead of one per token.
//
// ONE reader at a time (Model.eventsArmed). Every dispatch and every batch
// re-arms the pump, and with two dispatches in flight (§9.54) that is two
// callers wanting a goroutine on one channel; two readers would deliver batches
// to Update in whichever order the scheduler woke them, and an exit could land
// before the text it followed. The second caller gets nil, which tea.Batch
// ignores, and the one parked goroutine serves every seat.
func (m *Model) waitEvents() tea.Cmd {
	if m.eventsArmed {
		return nil
	}
	m.eventsArmed = true
	ch := m.events
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		batch := []runner.Event{ev}
		for len(batch) < drainMax {
			select {
			case next, ok := <-ch:
				if !ok {
					return eventBatchMsg{batch}
				}
				batch = append(batch, next)
			default:
				return eventBatchMsg{batch}
			}
		}
		return eventBatchMsg{batch}
	}
}

func (m *Model) applyEvents(batch []runner.Event) {
	for _, ev := range batch {
		c := m.column(ev.Vendor)
		if c == nil {
			continue
		}
		// A seat the room-open rebuild is still launching owns its own events
		// (rebuild.go, design.md §9.52). Intercepted BEFORE the switch rather
		// than handled inside it, because every branch below is written for a
		// turn and several of them read m.turn — an init line arriving at an
		// idle room has no turn to belong to, and walking it through that
		// machinery is how a process announcement becomes a column body.
		//
		// Ownership is narrow and it ends by itself: rebuildOwns is false the
		// moment the seat stops launching, and false for every seat while a
		// turn is running.
		if m.rebuildOwns(ev.Vendor) {
			m.applyRebuildEvent(c, ev)
			if !m.rebuildInFlight() {
				m.settleRebuild()
			}
			continue
		}
		switch ev.Kind {
		case runner.KindText:
			// Every byte of vendor output reaches state through the redactor,
			// which is the single choke point Render can be reasoned about from.
			c.Body += m.redact(ev.Vendor, ev.Text)
			if c.Phase == PhaseWaiting {
				// It streamed after all. Upgrading the phase is honest in this
				// direction only: the column now IS showing incremental output.
				c.Phase = PhaseStreaming
			}
			// Stream activity is what arms a racing seat's live stat read
			// (arenalive.go): the vendor is demonstrably doing something, so
			// its tree is worth a look. Text and tool calls arm; the meta
			// events below do not — a session id arriving is not evidence the
			// tree moved, and an idle seat re-reads nothing.
			m.armArenaRefresh(ev.Vendor)

		case runner.KindActivity:
			// One event can carry a parallel batch of calls, or a batch of
			// results, or both — the adapter decides, and the column folds each
			// entry in by id.
			for _, a := range ev.Acts {
				c.recordAct(a, m.redactWhole)
			}
			m.armArenaRefresh(ev.Vendor)

		case runner.KindSession:
			// An arena attempt's id is throwaway BY DESIGN (arena.go): letting it
			// land here would replace the room's saved thread with a session that
			// lives in a worktree and dies with the race — the user would quit,
			// reattach, and find every conversation swapped for a discarded one.
			// Read off the SEAT's turn (racing), where it read m.turn.arena: a
			// race owns every seat, so the two are the same fact per seat.
			if ev.SessionID != "" && !m.racing(ev.Vendor) {
				m.adoptSession(c, ev.SessionID)
			}

		case runner.KindGate:
			m.queueGate(c, ev.Gate)

		case runner.KindMeta:
			if ev.SessionID != "" && !m.racing(ev.Vendor) {
				m.adoptSession(c, ev.SessionID)
			}
			if ev.CostUSD != nil {
				c.CostUSD = ev.CostUSD
			}
			// End-of-turn result text.
			//
			// Flush the streaming redactor FIRST. A single-token reply
			// ("ALPHA") has no whitespace, so Feed held it and Body still
			// looked empty — measured 2026-08-05 with cursor-agent: then
			// `redact(result)+flush` became ALPHAALPHA, and that doubled
			// body is what /flow saved into the next hop's artifact.
			// Result text is a complete message; never run it through Feed.
			//
			// This branch used to carry a Cursor-only rule: its `result` was the
			// vendor's authoritative whole reply, so it REPLACED the streamed body
			// rather than filling an empty one, which is what kept a delta/repeat
			// pair from corrupting a /flow handoff. That rule is gone with the
			// protocol it described. The ACP seat's turn resolves with a stop
			// reason and no text at all (§9.36), so there is nothing authoritative
			// to prefer — and nothing to repeat either, which is the same
			// measurement seen from the other side.
			//
			// The honest consequence, stated because it is a real loss: this seat
			// no longer has a fallback for a turn that streamed nothing. §9.6c
			// leaned on that `result` explicitly — "the failure mode is a column
			// that fills at the end, never one that is wrong" — and on ACP a
			// broken chunk parser would give an EMPTY column instead of a late
			// one. The mitigation would have to be invented, so it is named rather
			// than faked.
			c.Body += m.flush(ev.Vendor)
			if ev.Text != "" && c.Body == "" {
				c.Body = m.redactWhole(ev.Text)
			}
			// On a persistent seat this line is the ONLY end-of-turn signal:
			// the process does not exit, so no KindDone is coming. An ephemeral
			// arena racer speaks the same protocol, so the same is true of it —
			// what differs is what finishColumn then does with the process.
			if ev.EndsTurn && m.ephemeralRacer(ev.Vendor) != nil {
				if strings.TrimSpace(c.Body) == "" {
					// A clean end with nothing streamed. On this seat that is
					// AMBIGUOUS by measurement, not by neglect: ACP's turn
					// resolves with a stop reason and no reply, so a racer that
					// worked silently and a chunk parser that broke render
					// identically (§9.36's stated loss). The note names the
					// fact rather than picking a story, and points at the one
					// receipt a race still has — the diff.
					c.Note = "the turn ended with nothing streamed — this seat sends no final reply, so the diff is the attempt's only receipt"
					if c.Body == "" {
						c.Body = "[Turn completed with 0 text chunks streamed — see diff]"
					}
				}
				m.finishColumn(c, PhaseDone)
			} else if ev.EndsTurn && m.isPersistent(ev.Vendor) {
				if strings.TrimSpace(c.Body) == "" {
					c.Body = "[Turn completed with 0 text chunks streamed]"
				}
				m.finishColumn(c, PhaseDone)
			} else if ev.EndsTurn && m.turnOf(ev.Vendor) != nil &&
				(c.Phase == PhaseStreaming || c.Phase == PhaseWaiting) {
				// A SPAWN-PER-TURN seat that names its own end of turn, which
				// until now nothing did: the batch CLIs ended a turn by dying and
				// the column read the exit. Codex says it twice — `turn.completed`
				// and then, seconds later, the exit — and the gap between the two
				// is dead time the column used to render as `streaming`. Measured
				// at 4.06s and 4.25s on codex-cli 0.147.0, and at 7.94s in §9.33
				// on the same build; on a demo stage that reads as a hang.
				//
				// So the PHASE settles here and the RETIREMENT does not. The
				// column stops claiming to work, stamps the elapsed it actually
				// earned, and stays in its dispatch's live set — the point of
				// splitting them. turnColumnFinished is what cancels the turn's
				// context, and that context is what runner.Start kills the child
				// on, so retiring here would kill a process that is still winding
				// down. The tail was measured empty (vendors/codex.go carries the
				// capture) but only for a turn that ran no tools, and shortening a
				// vendor's life on evidence that does not cover the tool path is
				// the inference-not-measurement move ADR-001 exists to refuse.
				// The exit still arrives, still runs KindDone, and still retires
				// this column; it just no longer decides what the column SAYS.
				//
				// Elapsed is stamped here rather than left to finishColumn, and
				// that is a correctness change of its own: finishColumn only fills
				// it when it is zero, so the number the column keeps is now the
				// time to the ANSWER instead of the time to the process's exit.
				// The old figure billed the user four seconds of nothing.
				//
				// The body is completed here too. flush ran at the top of this
				// branch, this is the vendor's last line on both captures, and a
				// column that says `done` while its placeholder is still four
				// seconds away would be settling into a lie about a different
				// field.
				//
				// EVERY write is behind the guard on this branch, and the guard is
				// wider than a phase test for a reason found in review. A killed
				// process drains its buffered stdout, so a `turn.completed` can
				// arrive AFTER the column it belongs to is already terminal — the
				// give-up path (giveUpSeat) kills an arena racer and retires its
				// column as PhaseCancelled, and the queued line lands afterwards.
				// An unguarded placeholder would then overwrite a cancelled
				// column's note-bearing body with "[Turn completed …]", i.e. a
				// cancelled seat asserting that its turn completed. The liveness
				// half (turnOf, which read m.turn.live before §9.54) covers the
				// same line arriving after the turn boundary entirely, where it
				// could otherwise settle a FRESH turn's column on the strength of
				// the previous turn's answer.
				if strings.TrimSpace(c.Body) == "" {
					c.Body = "[Turn completed with 0 text chunks streamed]"
				}
				if c.Elapsed == 0 && !c.Started.IsZero() {
					c.Elapsed = time.Since(c.Started)
				}
				c.Phase = PhaseDone
				// Settling is the linger made visible. Without it the room goes
				// quiet — no spinner, every column reading `done` — while the
				// composer is still locked and the footer offers `q`, which key()
				// then refuses. That is §7.8's surprise, created by this very
				// change, so the fact that produced it is put on screen rather
				// than left to be inferred.
				c.Settling = true
				// NOT cancellation-aware, unlike finishColumn. A ctrl+c during the
				// linger cannot un-answer a turn that already answered: the reply
				// is on screen and in the vendor's rollout. The keystroke kills the
				// process, KindDone lands on a column that is already terminal, and
				// `done` is what it stays.
			}

		case runner.KindDone:
			// The seat the operator cut with `x` this turn. Its column is already
			// terminal and carries the give-up's own note, so the only thing left
			// for this exit to do is the PROCESS bookkeeping — which is why this
			// is a branch and not a bare `continue`: a persistent seat that was
			// INTERRUPTED keeps its process, and if that process later dies for
			// real, forgetting it here is what stops the next brief writing into
			// a closed pipe. The liveness test is the same one the stale-exit
			// guards below trust: a process that exited cannot be Alive.
			if m.wasGivenUp(ev.Vendor) {
				if p, ok := m.procs[ev.Vendor]; ok && (p.sess == nil || !p.sess.Alive()) {
					m.dropProcess(ev.Vendor)
				}
				continue
			}
			// A terminal event names a VENDOR, not a process. When this seat's
			// CURRENT process is alive, this exit belongs to a predecessor — the
			// one a /cd respawn killed, or one that died while the room was idle
			// and whose exit sat queued until the next turn drained the channel.
			// A process that exited cannot be Alive, so the test is exact.
			// Acting on a stale exit would fail the live turn, drop the live
			// process from procs (leaving it running and invisible, which is the
			// exact state this product refuses), and discard the earned thread
			// through the probation rule. Found in review before it shipped.
			//
			// A cursor race puts TWO processes behind one vendor id — the room's
			// idle seat and the throwaway racer — and events carry only the
			// vendor, so an exit here is attributed by the same liveness test
			// the guard above already trusts: a process that exited cannot be
			// Alive. While the racer is alive the exit can only be the room's
			// own process dying in the background; once the racer is dead this
			// exit is its, and it must not be eaten by the m.procs guard below —
			// that guard reading a live ROOM process as "this seat is fine"
			// would leave the race column streaming forever and the turn unable
			// to end.
			if es := m.ephemeralRacer(ev.Vendor); es != nil {
				if es.Alive() {
					// The room's idle seat died mid-race. Forgotten so the next
					// ordinary brief respawns it; the racing column is not
					// touched — its process is still running.
					if p, ok := m.procs[ev.Vendor]; ok && (p.sess == nil || !p.sess.Alive()) {
						m.dropProcess(ev.Vendor)
					}
					continue
				}
				// The racer died WITHOUT ending its turn — on this seat the
				// turn's end is a protocol response (§9.36), so a bare exit,
				// even a zero one, means no answer ever arrived. Failed with
				// the reason named, never PhaseDone: an exit dressed as a
				// completed attempt is the empty-success render this seat's
				// missing result line makes possible, stated in §9.36 and
				// refused here.
				c.Body += m.flush(ev.Vendor)
				c.Elapsed = time.Since(c.Started)
				if !m.cancelling[ev.Vendor] {
					c.Note = "the racer's process ended before its turn did — no answer arrived; anything it wrote is in the diff"
				}
				m.finishColumn(c, PhaseFailed)
				continue
			}
			// The OTHER half of the two-processes-one-vendor-id split, and the
			// one the guard below cannot see. A ONE-SHOT racer ends its turn by
			// EXITING — that is the whole protocol for it (there is no live
			// session to answer, which is what separates it from the ephemeral
			// branch above) — so this KindDone is the column's only retirement
			// signal. When the same vendor also runs a persistent ROOM seat, the
			// stale-exit guard below reads that live process as "this seat is
			// fine" and eats the racer's exit, and the column then streams until
			// the user cancels: the exact outcome KindDone's own attribution
			// comment names, reached by the path it did not cover.
			//
			// MEASURED, 2026-08-13, race t10 on the reference box: the racer
			// exited 52s in with its reply complete, the room went on rendering
			// `streaming` for 21 minutes, and no diff, commit, rank or seed
			// receipt ever ran. Race t9 sits on disk in the same state, from
			// before the gate-hook build, so this predates #223 rather than
			// following from it. The trigger is a WARM seat: race before the
			// room has sent Claude an ordinary brief and m.procs is empty, the
			// guard never fires, and the race lands — which is why earlier races
			// passed and why this one did not.
			//
			// giveUpSeat's comment already knew the guard eats this exit and
			// judged it harmless, correctly, for ITS path: a given-up column is
			// already terminal when the exit arrives. On the ordinary path the
			// column is not, and the same swallow is the difference between a
			// race that finishes and one that cannot.
			//
			// dropProcess is deliberately NOT called here. The exit belongs to
			// the racer; the room's own seat is still running, and forgetting a
			// live process would leave it running and invisible — the state this
			// product refuses, and the reason this is its own branch rather than
			// a hole poked in the guard.
			if m.arenaRacing(ev.Vendor) {
				c.Body += m.flush(ev.Vendor)
				if strings.TrimSpace(c.Body) == "" {
					c.Body = "[Turn completed with 0 text chunks streamed]"
				}
				m.finishColumn(c, PhaseDone)
				continue
			}
			if p, ok := m.procs[ev.Vendor]; ok && p.sess != nil && p.sess.Alive() {
				continue
			}
			// A persistent process reaching here has DIED — the turn's end never
			// takes it down. Either the room is quitting, or something ended it
			// under us; both mean the seat has no process any more.
			persistent := m.isPersistent(ev.Vendor)
			// A live seat that died on its FIRST turn before it ever named a
			// session could not be brought up at all — the agy stream shape's
			// refusal, a build without the subcommand (vendors.LiveFallback).
			// The seat retreats to its batch adapter on this same dispatch,
			// so the brief is not lost; a later death is a real death.
			if persistent && m.retreatOnDeath(c) {
				continue
			}
			m.dropProcess(ev.Vendor)
			c.Body += m.flush(ev.Vendor)
			if persistent && (c.Phase == PhaseStreaming || c.Phase == PhaseWaiting) && !m.cancelling[ev.Vendor] {
				// Mid-turn death. Said as a failure rather than a clean finish,
				// because the answer the user was waiting for is not coming and
				// a column that simply stopped would look like one that finished.
				//
				// A seat on a restored thread has this note replaced by
				// settleRestoredThread, inside finishColumn: for that seat the
				// death IS the reattach being refused, and "the process ended"
				// would describe the symptom rather than the cause.
				c.Elapsed = time.Since(c.Started)
				c.Note = "the vendor process ended mid-turn — the next brief starts a new session"
				m.finishColumn(c, PhaseFailed)
				continue
			}
			// The placeholder is a claim about THIS turn — it completed and
			// produced no text — so only a column that was still live may
			// acquire it. This is the same defect the end-of-turn branch above
			// was fixed for and the same argument
			// (TestALateEndOfTurnLineCannotSettleATerminalColumn): the phase
			// write has always been guarded, the BODY write was not, so an exit
			// arriving on a column that had already ended wrote "[Turn completed
			// …]" over it. The give-up made that reachable on an exit rather
			// than only on a queued end-of-turn line — a seat cut with `x`
			// whose child exits after the turn boundary is past
			// turnState.givenUp's lifetime, and a cut seat claiming a measured
			// zero is §4a.1's false zero.
			if strings.TrimSpace(c.Body) == "" && (c.Phase == PhaseStreaming || c.Phase == PhaseWaiting) {
				c.Body = "[Turn completed with 0 text chunks streamed]"
			}
			m.finishColumn(c, PhaseDone)

		case runner.KindError:
			// The cut seat again, and this is the branch that made the guard
			// necessary rather than merely tidy. interruptSeat cancels a
			// persistent turn by ASKING the vendor to abandon it, and the vendor
			// answers with a failed `result` — measured as is_error true with
			// terminal_reason "aborted_tools". Without this the tail below would
			// write that error over "given up after 1m2s …", so a seat the
			// operator stopped would read as a seat that fell over, and
			// m.failure would record a vendor failure for a keystroke.
			if m.wasGivenUp(ev.Vendor) {
				if !ev.EndsTurn {
					if p, ok := m.procs[ev.Vendor]; ok && (p.sess == nil || !p.sess.Alive()) {
						m.dropProcess(ev.Vendor)
					}
				}
				continue
			}
			// The same predecessor guard as KindDone, for the process-level
			// half only: a vendor-REPORTED failure (EndsTurn) rides the current
			// process's own stdout and is never stale, but a process exit can
			// be an old process's — and during a cursor race, either of two
			// processes' (see KindDone's attribution rule; same rule here).
			if !ev.EndsTurn {
				if es := m.ephemeralRacer(ev.Vendor); es != nil {
					if es.Alive() {
						// The room's idle seat failed in the background
						// mid-race. Forgotten; the racing column is not its.
						if p, ok := m.procs[ev.Vendor]; ok && (p.sess == nil || !p.sess.Alive()) {
							m.dropProcess(ev.Vendor)
						}
						continue
					}
					// The racer itself crashed. Fall through: the tail below
					// sets the note from the event and finishColumn — which is
					// what reaps the dead racer — runs on the Err/ExitCode test.
				} else if p, ok := m.procs[ev.Vendor]; ok && p.sess != nil && p.sess.Alive() {
					continue
				}
			}
			c.Body += m.flush(ev.Vendor)
			// A seat already told that its saved thread was refused keeps that
			// sentence. A dead thread produces TWO events — the vendor's own
			// failed `result`, then the process exit carrying its stderr — and
			// the second one arriving last would replace "here is what happens
			// to your next brief" with a bare `exit status 1: No conversation
			// found with session ID`. Both are true; only one of them tells the
			// user what to do, and the raw one reads as a broken vendor.
			if !m.threadLost[ev.Vendor] {
				if ev.Note != "" {
					c.Note = ev.Note
				} else if ev.Err != nil {
					c.Note = ev.Err.Error()
				}
			}
			// Recorded on the same terms the note is, and for the same reason a
			// dead thread's two events must not overwrite each other: a turn
			// that produced one classified failure has been classified, and a
			// later Unclassified process exit carrying the same failure's stderr
			// must not erase it. Only an upgrade lands.
			if ev.Failure != runner.FailureUnclassified {
				m.failure[ev.Vendor] = ev.Failure
			}
			if m.isPersistent(ev.Vendor) {
				if ev.EndsTurn {
					// The vendor reported the turn failed. On a persistent seat
					// that is the end of the TURN, not of the process, so the
					// column retires and the seat stays open for the next brief.
					//
					// Unless the "failure" is the handshake itself: the two RPC
					// protocols report a refused initialize as a failed turn
					// and mark themselves Dead, and a seat with a measured
					// batch adapter behind it retreats to that adapter on this
					// same dispatch rather than retiring the column
					// (vendors.LiveFallback). The process is up and useless,
					// and stopProc ends it on the way.
					if m.retreatOnRefusal(c) {
						continue
					}
					// An interrupt lands here too: cancelling produced a result
					// with is_error true and terminal_reason "aborted_tools".
					// The user's keystroke is not a vendor failure, so
					// finishColumn's cancellation check re-labels it.
					if m.cancelling[ev.Vendor] {
						c.Note = ""
					}
					m.finishColumn(c, PhaseFailed)
				} else {
					// The PROCESS failed. Nothing more is coming from it — and
					// if it failed on its first turn before naming a session,
					// it was never up (retreatOnDeath, the KindDone rule).
					if m.retreatOnDeath(c) {
						continue
					}
					m.dropProcess(ev.Vendor)
					c.Elapsed = time.Since(c.Started)
					m.finishColumn(c, PhaseFailed)
				}
				continue
			}
			if ev.EndsTurn && m.ephemeralRacer(ev.Vendor) != nil {
				// The racer's own protocol reported the turn dead — a refused
				// handshake, a session the vendor would not open, a failed
				// prompt. NO process exit follows: an ACP server survives its
				// own refusals (§9.36 measured it up and useless after a failed
				// initialize), so the "KindDone still follows" assumption below
				// would leave this column streaming forever. finishColumn is
				// what kills the process.
				c.Elapsed = time.Since(c.Started)
				m.finishColumn(c, PhaseFailed)
				continue
			}
			// Only when nothing has recorded one, which is the same rule
			// finishColumn follows. A seat that ANSWERED and then failed on its
			// way out already stamped the time to its answer, and restamping here
			// would hand the column the process's whole lifetime — the exact
			// figure the settle branch above exists to stop billing.
			if c.Elapsed == 0 && !c.Started.IsZero() {
				c.Elapsed = time.Since(c.Started)
			}
			// Read BEFORE the phase is written, because the phase is the test:
			// this column was working until this line, and it is the same guard
			// the end-of-turn branch above uses, for the same two reasons — a
			// killed process drains its buffered stdout onto a column that is
			// already terminal, and a line can arrive after the turn boundary
			// entirely.
			// turnOf is the per-seat read of what was m.turn.live[ev.Vendor].
			live := m.turnOf(ev.Vendor) != nil &&
				(c.Phase == PhaseStreaming || c.Phase == PhaseWaiting)
			c.Phase = PhaseFailed
			// A vendor-reported failure arrives BEFORE the process exits, so
			// this is not the end of the column's life; KindDone still follows
			// and is what retires it.
			if ev.ExitCode != 0 || ev.Err != nil {
				m.finishColumn(c, PhaseFailed)
			} else if live {
				// The failure the PROCESS has not reported: agy says a turn died
				// in its own stream (a `result` with status ERROR), which the
				// adapter turns into this event with exit code 0 and no error
				// because nothing has exited yet. The column is terminal and the
				// turn is not, which is exactly the split §9.33 named for a seat
				// that answers ahead of its process — and the same word carries
				// it. Without this the seat is neither Busy nor Settling while
				// its turn is live: every column reads terminal, no spinner
				// moves, and the footer offers a `q` that key() refuses with "a
				// turn is in flight" (§7.8's surprise, and the gap InFlight's own
				// doc comment named).
				//
				// A settle rather than a retirement, on §9.33's measurement
				// argument rather than on taste: turnColumnFinished cancels the
				// turn's context and runner.Start kills the child on it, so
				// retiring here would kill a process that is still winding down.
				//
				// This vendor's linger HAS since been measured, and only for the
				// succeeding turn: 0.049s, 0.135s and 0.314s after the `result`
				// line, on agy 1.1.13 (design.md §9.43's 2026-08-16 amendment).
				// That reading makes the settle cheaper rather than wrong — there
				// is close to nothing here to cut short — and it does not reach
				// this branch, because all three probe turns ended SUCCESS. The
				// failing turn's tail is still a number nobody has, which is a
				// reason to wait for the exit rather than a licence to cut it
				// short.
				//
				// NOT cancellation-aware, for the same reason the settle above is
				// not: a ctrl+c during the linger does not un-fail the turn, and
				// finishColumn clears the word when the process finally goes.
				c.Settling = true
			}
		}
	}
}

// finishColumn retires one column from the turn.
//
// Every path that ends a column goes through here, which is what keeps the
// cancellation wording in one place: a turn the user stopped must never be
// rendered as a vendor failure, and there are now four ways a column can end.
func (m *Model) finishColumn(c *Column, phase Phase) {
	// The redactor holds the tail of the stream — everything after the last
	// word boundary, so a secret split across two chunks cannot straddle the
	// match. A turn that ends without flushing it EATS that tail: measured on
	// a live persistent seat, whose end-of-turn is a `result` line rather than
	// a process exit, so the KindDone/KindError flushes never ran and every
	// reply lost its final word. Flushed here, at the one place every
	// retirement passes through; the per-event flushes remain and are
	// harmless — a flushed redactor yields "".
	c.Body += m.flush(c.Vendor)
	// The linger is over by definition: every path into this function is the
	// process ending, one way or another. Cleared here rather than in the
	// KindDone branch so a seat that settles and then FAILS its exit — or is
	// killed by a ctrl+c during the linger — cannot keep the word either.
	c.Settling = false
	// Whatever this seat was waiting to be told, it is no longer waiting. A card
	// left up for a vendor that has stopped asking invites a keystroke that
	// decides nothing, and the footer would go on claiming the room is gated.
	m.dropGates(c.Vendor)
	if c.Elapsed == 0 && !c.Started.IsZero() {
		c.Elapsed = time.Since(c.Started)
	}
	if c.Phase == PhaseStreaming || c.Phase == PhaseWaiting {
		c.Phase = phase
		if m.cancelling[c.Vendor] {
			c.Phase = PhaseCancelled
			c.Note = "cancelled — the output above is partial"
		}
	}
	// The seat's turn, read once for the block below and for the stamp. The
	// dispatch this seat is answering, where the code read m.turn (§9.54).
	ts := m.turnOf(c.Vendor)
	if ts != nil {
		// When this seat's turn ENDED, on the room's clock, for the inbox
		// (needsyou.go): a seat that landed while the reader was elsewhere is
		// listed until they go to it. Stamped only while the seat still holds a
		// turn, because retirement is reached twice for some seats — a
		// persistent seat's end-of-turn line and then its exit, an ACP racer's
		// response and then the exit of the process it killed — and a second
		// stamp would re-list a seat the reader has already read.
		c.Ended = time.Now()
	}

	// The race's deliverable is the diff, so it is read the moment this seat
	// lands — including on a cancelled or failed attempt, whose partial work is
	// still a receipt in a kept worktree. Synchronous, and deliberately so for
	// v1: two `git diff` runs against a fresh worktree are milliseconds, and an
	// async path would add a message type for a stall nobody has measured. If a
	// monorepo ever makes this visible, that measurement is the trigger to move
	// it onto a Cmd.
	if ts != nil && ts.arena {
		if es, ok := ts.arenaEphemeral[c.Vendor]; ok {
			// The racer dies AT ITS OWN finish line, not the turn's. Kill, and
			// never a polite wait: §9.33 measured this vendor's process
			// lingering ~2.5s after answering, and a racer has nothing to say
			// after its turn ends — the turn's end is the protocol response
			// that just retired this column (§9.36). Killed BEFORE the diff is
			// read below, so the receipt is a snapshot of a stopped attempt
			// rather than a tree a live process is still writing into. The
			// turn's context is the backstop for the paths that never reach
			// here per column (ctrl+c, room teardown), and a seat cannot be
			// cleared mid-race at all — askClearSeat refuses while a turn is in
			// flight.
			es.Kill()
			delete(ts.arenaEphemeral, c.Vendor)
		}
		if tree, ok := ts.arenaTrees[c.Vendor]; ok && c.Arena == nil {
			// c.Arena == nil makes collection once-only. A racer driven by a
			// live protocol retires twice — its end-of-turn response, then the
			// exit of the process that response got killed — and a second pass
			// here would re-rank the race on an echo.
			r := collectArena(tree, ts.arenaBase)
			if ls := ts.arenaLive[c.Vendor]; ls != nil {
				// This seat's live stat is over either way — the final owns
				// the block from here, and a read launched after this line
				// would only ever arrive to be dropped.
				ls.stopped = true
				if r.Err != "" && ls.inFlight {
					// The one collision the live refresh introduces: an
					// interim read still running holds the worktree's
					// index.lock for the milliseconds its `add -N` takes, and
					// git fails rather than waits. A final that failed WHILE a
					// refresh was in flight is retried once, because reporting
					// "diff unavailable" for a lock this feature itself was
					// holding would be the refresh degrading the authoritative
					// read it exists to complement. One retry, not a loop: a
					// second failure is a real one and is reported as such.
					r = collectArena(tree, ts.arenaBase)
				}
			}
			// Named with the RACE's recorded number, never c.TurnN: the race
			// numbers itself past older rooms' leftovers (arenaRaceNumber),
			// so the turn and the race can legitimately disagree — and the
			// branch the receipt claims must be the branch setup created.
			r.Branch = arenaBranch(ts.arenaRaceN, c.Vendor)
			r.RaceN = ts.arenaRaceN
			// Commit-per-turn (§9.37, amended 2026-08-09): once the diff is
			// read, the attempt is parked on its arena branch, so every race
			// survives the worktree it happened in — diffable, adoptable,
			// rollbackable, even after the tree is deleted. Two deliberate
			// skips: a zero-diff attempt gets NO empty commit (a receipt for
			// work that did not happen is §4a.1's false zero, mirrored), and
			// a seat whose collection already failed gets one sentence, not
			// two — its tree is unreadable, so a commit error would restate
			// the same degradation in different words. A commit failure lands
			// on THIS seat as a named reason and nowhere else: the race, the
			// other racers and the room's repo are not this seat's blast
			// radius.
			if r.Err == "" && strings.TrimSpace(r.Stat) != "" {
				sha, cerr := commitArena(tree, ts.arenaBase, arenaCommitMsg(ts.arenaRaceN, c.Prompt))
				if cerr != nil {
					r.CommitErr = "not committed: " + cerr.Error()
				} else {
					r.Commit = sha
				}
			}
			// The seed receipt was measured at setup; the column that states
			// it exists now. nil when the room repo has no .worktreeinclude —
			// the render draws nothing for nil, per zero-vs-absent.
			r.Seed = ts.arenaSeeds[c.Vendor]
			// Rank is the order the room OBSERVED seats land, stamped here on
			// the host's clock. Every racer gets one — a DNF finished too, just
			// not well, and the render pairs the rank with the phase word so
			// "2nd" on a failed attempt cannot read as a result.
			ts.arenaFinished++
			r.Rank, r.Of = ts.arenaFinished, len(ts.arenaTrees)
			// The check runs LAST, and the order is a ruling (§9.48): the diff
			// is read and the attempt is committed above, so nothing the check
			// writes into the tree can reach this seat's stat or its receipt.
			// A check that runs before the commit would park its own build
			// output on the arena branch wearing the racer's name. The run
			// itself is queued rather than started — it is a subprocess of the
			// operator's choosing, so it goes off the render loop the way the
			// worktree setup did (arenacheck.go).
			r.Check = m.armArenaCheck(c.Vendor, c.TurnN, tree)
			c.Arena = &r
			// The finish-time read REPLACES the last interim stat, never
			// merges with it (§9.37): the interim was a moment already past,
			// and leaving any of it beside the settled result would be two
			// answers on one column with nothing to say which is final.
			c.ArenaInterim = nil
		}
	}

	m.finishFlowHop(c)

	m.settleRestoredThread(c)
	m.turnColumnFinished(c.Vendor)
}

// flowWriteTargets names a stage's write hops for the gate and the refusal:
// `@codex → docs/a.md, @grok → docs/b.md`. One hop reads exactly as it did
// before stages existed.
func flowWriteTargets(stage []*FlowStep) string {
	var parts []string
	for _, s := range stage {
		if s.RequiresWriteGate() {
			parts = append(parts, "@"+string(s.Vendor)+" → "+s.Path)
		}
	}
	return strings.Join(parts, ", ")
}

// launchFlowStage starts the current stage and spawns it: every hop's seat at
// once, each with its own task, the carry from the previous stage fenced under
// each (§9.55). Reached from dispatch directly, or from the seat setup that
// stood in front of it.
//
// Start runs here rather than in dispatch so that a write hop's baseline is
// captured in the directory its seat runs in (StartIn with seatDir) — which,
// in a writing room, is a worktree that may only just have been cut.
func (m *Model) launchFlowStage() tea.Cmd {
	fc := m.flowChain
	if fc == nil {
		return nil
	}
	stage := fc.Stage()
	if len(stage) == 0 {
		return nil
	}
	if err := fc.StartIn(m.seatDir); err != nil {
		m.st.Notice = "flow start error: " + err.Error()
		// The whole chain, marker included — this used to nil the chain by
		// hand and leave the header claiming a hop, the half-cleared state
		// endFlowChain exists to make unrepresentable (§9.35).
		m.endFlowChain()
		return nil
	}
	route := Route{}
	prompts := map[model.VendorID]string{}
	for _, s := range stage {
		route.Vendors = append(route.Vendors, s.Vendor)
		p := strings.TrimSpace(s.Task)
		if p == "" {
			p = s.Verb
		}
		if m.flowCarry != "" {
			p += "\n\n" + m.flowCarry
		}
		prompts[s.Vendor] = p
	}
	m.flowCarry = ""
	if len(stage) > 1 {
		// Each hop of a fan carries its own task, so the one prompt sendTurn
		// takes is overridden per seat (Model.fanPrompts, consumed there).
		m.fanPrompts = prompts
	}
	return m.sendTurn(route, prompts[stage[0].Vendor], nil)
}

// finishFlowHop records harness-observed completion of one flow seat.
// Non-write hops become Returned (not approved). Write hops (Path set) must
// already have been user-gated before dispatch; on PhaseDone we verify the disk
// receipt and MarkPublished or MarkFailed. Artifact save failure fails the hop.
//
// Per SEAT, per stage (§9.55): the landing column finds its own hop in the
// current stage (StepFor), so a fan's seats retire one at a time, each adding
// its fenced reply to the carry, and the chain advances only when the stage's
// last seat has landed (StageDone) — that is the join.
func (m *Model) finishFlowHop(c *Column) {
	if m.flowChain == nil || c.Phase != PhaseDone || strings.TrimSpace(c.Body) == "" {
		return
	}
	step := m.flowChain.StepFor(c.Vendor)
	if step == nil || step.State != FlowStateRunning {
		return
	}
	hop := m.flowChain.StageN()

	store, err := NewArtifactStore()
	if err != nil {
		_ = m.flowChain.MarkFailedAt(step, "artifact store: "+err.Error())
		m.st.Notice = "flow hop failed: artifact store: " + err.Error()
		m.endFlowChain()
		return
	}
	sessID := m.sessions[c.Vendor]
	if sessID == "" {
		sessID = flowSessionID(m.st.Workspace)
	}
	path, err := store.SaveArtifact(sessID, c.TurnN, c.Vendor, c.Body, c.Prompt)
	if err != nil {
		_ = m.flowChain.MarkFailedAt(step, "artifact save: "+err.Error())
		m.st.Notice = "flow hop failed: " + err.Error()
		m.endFlowChain()
		return
	}
	m.st.Notice = "artifact saved: " + path

	if step.RequiresWriteGate() {
		// Verified where the seat actually wrote: its own worktree in a
		// writing room (seatDir), the workspace otherwise. A receipt read in
		// the workspace for a file the seat wrote into its tree would fail a
		// write that landed.
		receipt := VerifyReceipt(m.seatDir(c.Vendor), step)
		step.Receipt = receipt
		if !receipt.Verified {
			_ = m.flowChain.MarkFailedAt(step, receipt.Detail)
			m.st.Notice = joinNotice(m.st.Notice, "publish failed: "+receipt.Detail)
			m.endFlowChain()
			return
		}
		if err := m.flowChain.MarkPublishedAt(step, receipt); err != nil {
			m.st.Notice = joinNotice(m.st.Notice, err.Error())
			return
		}
		m.st.Notice = joinNotice(m.st.Notice, "flow hop published ("+receipt.Detail+")")
	} else {
		if err := m.flowChain.MarkReturnedAt(step); err != nil {
			m.st.Notice = joinNotice(m.st.Notice, err.Error())
			return
		}
		m.st.Notice = joinNotice(m.st.Notice, fmt.Sprintf("flow hop %d returned (@%s %s) — not an approval", hop, step.Vendor, step.Verb))
	}

	// This hop is already finished — Returned or Published — so a failure reading
	// its own artifact back stops the CHAIN and leaves the hop's record alone. The
	// previous spelling called MarkFailed here, which on a published hop would
	// overwrite a verified receipt: the write had landed, the workspace still held
	// it, and the room would have reported that it failed.
	// Body only. The provenance header is council's own bookkeeping, and a fence
	// carrying it hands the next seat a session id and a prompt hash as though
	// the previous seat had written them — measured on a live chain, where codex
	// answered with the artifact's PromptSHA256-8 quoted back.
	//
	// Read back per seat and APPENDED to the carry, in landing order: the
	// stage that follows a fan receives every predecessor's reply, each in its
	// own labelled fence. Only this stage's replies — the carry was emptied
	// when this stage launched, so older artifacts never accumulate.
	artifact, err := store.LoadArtifactBody(sessID, c.TurnN, c.Vendor)
	if err != nil {
		m.st.Notice = joinNotice(m.st.Notice, "flow stopped: cannot read this hop's artifact back: "+err.Error())
		m.endFlowChain()
		return
	}
	fence := FormatFencedArtifact(c.Label, c.TurnN, artifact)
	if m.flowCarry == "" {
		m.flowCarry = fence
	} else {
		m.flowCarry += "\n\n" + fence
	}

	if !m.flowChain.StageDone() {
		// The join: another seat of this stage is still answering, and the
		// next stage waits on it. The notice names what is still owed.
		if left := m.flowChain.Unfinished(); left != nil {
			m.st.Notice = joinNotice(m.st.Notice, "waiting on @"+string(left.Vendor)+" before hop "+itoa(hop+1))
		}
		return
	}

	// `s` was pressed while this stage ran. The stage itself finished on its
	// own terms — artifacts saved, receipts verified, Returned or Published
	// exactly as recorded above — and the chain ends here instead of handing
	// off. Checked AFTER the stage's record is written, because stopping is
	// about the NEXT hop and must not cost this one its receipt (§9.35).
	if m.st.FlowStop {
		total := m.flowChain.Stages()
		m.st.Notice = joinNotice(m.st.Notice, fmt.Sprintf(
			"flow stopped after hop %d/%d — %s not dispatched", hop, total, hopsWord(total-hop)))
		m.endFlowChain()
		return
	}

	hasNext, err := m.flowChain.Advance()
	if err != nil {
		m.st.Notice = joinNotice(m.st.Notice, "flow stopped: "+err.Error())
		m.endFlowChain()
		return
	}
	if !hasNext {
		// The last hop returned, so the chain is over. The whole chain goes, not
		// just the marker: flowChain and flowDraft kept alive here made dispatch
		// treat the NEXT enter as the chain's, and the user's following brief was
		// swallowed by "flow start error: cannot start step in state returned" —
		// a finished chain eating the first message typed after it (§9.35).
		m.endFlowChain()
		return
	}
	m.flowAdvancePending = true
}

// clearFlowMarker takes the hop indicator off the header.
//
// Called wherever a chain stops advancing — finished, failed, or replaced by an
// ordinary brief. A marker that outlived its chain would assert that the room is
// mid-orchestration while it sits idle, which is the one thing this indicator
// was added to prevent. FlowStop goes with it: a "stops here" promise about a
// chain that no longer exists is the same lie one word longer.
func (m *Model) clearFlowMarker() {
	m.st.FlowHop, m.st.FlowSteps, m.st.FlowVendor = 0, 0, ""
	m.st.FlowSeats = ""
	m.st.FlowStop = false
}

// endFlowChain retires the chain itself, not just its marker.
//
// Every way a chain can be over — finished, failed, cancelled, stopped by `s`,
// refused at its write gate — comes through here, because a chain cleared
// halfway is worse than one not cleared at all. clearFlowMarker alone left
// flowChain and flowDraft behind, and dispatch reads exactly those to decide
// that the next enter belongs to the chain: the corpse swallowed the user's
// following brief with "flow start error: cannot start step in state returned".
// Measured (2026-08-08) on a chain that had COMPLETED — the price was being
// paid on the happy path, not just on cancels.
func (m *Model) endFlowChain() {
	m.flowChain = nil
	m.flowDraft = ""
	m.flowCarry = ""
	m.flowReadHop = false
	m.flowAdvancePending = false
	m.flowWritePending = false
	m.flowWriteArmed = false
	m.fanPrompts = nil
	m.clearFlowMarker()
}

// hopsWord counts the hops a stopped chain will never run, in words a notice
// can carry.
func hopsWord(n int) string {
	if n == 1 {
		return "1 later hop"
	}
	return strconv.Itoa(n) + " later hops"
}

func flowSessionID(workspace string) string {
	sum := sha256.Sum256([]byte(workspace))
	return "room-" + hex.EncodeToString(sum[:8])
}

func joinNotice(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " · " + b
	}
}

// adoptSession takes the session id a vendor just reported, and — on the one
// seat where that id can contradict the room — says so first.
//
// The ordinary case is a single assignment: whatever id the vendor named is the
// thread the next turn resumes.
//
// The case this function exists for is §9.43. MEASURED 2026-08-09 against agy
// 1.1.11 during the wire-fixture capture: handed a `--conversation` id it does
// not hold, that CLI does not refuse it — it opens a NEW conversation, answers
// the brief normally, and reports `status: "SUCCESS"` with exit 0 and a
// DIFFERENT `conversation_id`. Every other seat either resumes or says the
// history is gone; this one claims success either way, so a room reading status
// and exit code alone would render a continued conversation for a turn with no
// history behind it. The returned id not matching the requested one is the only
// tell the capture surfaced, and this is where it is read.
//
// Three decisions are worth stating, because each had an alternative:
//
//   - **The reply stands.** The turn succeeded and the answer is real; what is
//     false is only the claim that it was informed by everything before it. So
//     the body renders untouched and the card corrects the labelling. Failing
//     the column instead would throw away work the user paid for to punish the
//     vendor for a bookkeeping mismatch.
//   - **The NEW id is adopted**, not discarded. The reply already happened
//     inside it, so keeping the requested id would leave the room pointing at a
//     conversation with one fewer turn in it than the transcript shows — and
//     would rebuild the same forking invocation on every later turn.
//   - **The card is the calm one already in use** (settleRestoredThread's), not
//     a second card saying the same thing in different words. The fact is
//     identical — this seat is starting fresh — and only the body differs,
//     because the mechanics differ: there the turn FAILED and the id was let go,
//     here the turn succeeded in a thread the user never asked for.
//
// The comparison only ever runs for a seat dispatch put in forkWatch, which is
// gated on vendors.SilentResumeFork. An id mismatch on a vendor whose resume
// semantics have not been measured stays unremarked (§4a.1: a card is a measured
// fact, never an inference).
func (m *Model) adoptSession(c *Column, id string) {
	if asked, watching := m.forkWatch[c.Vendor]; watching && asked != id {
		// Spent on sight. The comparison is about the dispatch, and both the
		// init and the result frame carry an id — leaving the entry in place
		// would raise the same card twice on one turn.
		delete(m.forkWatch, c.Vendor)
		c.Note = "thread not restored — starting fresh"
		c.NoteDetail = "this seat asked to resume its saved thread and the vendor answered in a new conversation instead, " +
			"reporting success. the reply below is real; the history behind it is not. " +
			"the new thread is kept, so your next brief continues from this turn."
		c.NoteCalm = true
		// The seat is no longer on the thread the room reattached it to, so the
		// marker that says it is has to go — the same correction
		// settleRestoredThread makes when a restored id is let go.
		c.Restored = false
		// Guards this sentence against the rest of the turn's events, exactly as
		// the refused-reattach card is guarded: a later note about the same seat
		// must not replace the one line that says what happened to the history.
		m.threadLost[c.Vendor] = true
		// Probation is over, and settled by EVIDENCE rather than by a turn's
		// outcome. The restored id is gone — the vendor answered somewhere else —
		// so there is nothing left for settleRestoredThread to decide, and
		// leaving the seat marked unproven would let a later failure on the NEW
		// thread be blamed on a reattach that has already been reported.
		delete(m.unproven, c.Vendor)
		delete(m.resumeIDs, c.Vendor)
	}
	m.sessions[c.Vendor] = id
}

// settleRestoredThread decides the fate of a session id that came back from a
// saved room and has now had its first turn.
//
// One rule for all four seats, deliberately. The DEFAULT is still one attempt:
// a restored id whose first turn fails is dropped, because retrying a genuinely
// dead id rebuilds the same doomed invocation on every turn for the life of the
// room, and that wedge is the hole the ninth amendment closed.
//
// What the sixteenth amendment adds is one exception, and it is narrow on
// purpose. The one-attempt rule was written when no adapter reported anything a
// caller could branch on. Two signals have since been captured that say the
// vendor never reached the conversation at all — a pre-flight refusal, and agy's
// own 503 — and against those the rule was spending a whole conversation on a
// hiccup. Where the failure is classified transient the id stays on probation
// for the next turn, which is exactly the treatment a CANCELLED turn already
// gets and for the same stated reason: nothing was learned about the thread.
//
// An unclassified failure is unchanged. The asymmetry is deliberate — a wedged
// seat retrying a dead id forever is worse than a lost conversation — so the
// exception fires only on positive evidence, never on the absence of it.
//
// Ids EARNED in this process are never touched here. A transient failure in the
// middle of a working conversation must not throw the thread away; the whole
// point of resume is that the history survives a bad turn.
func (m *Model) settleRestoredThread(c *Column) {
	if !m.unproven[c.Vendor] {
		return
	}
	switch {
	case c.Phase == PhaseDone:
		// It answered, so the thread is real. From here this is an ordinary
		// session and the probation is over.
		delete(m.unproven, c.Vendor)
	case c.Phase == PhaseCancelled:
		// The user stopped it. Nothing was learned about the thread either way,
		// so it stays on probation rather than being discarded for a keystroke.
	case m.failure[c.Vendor].Transient():
		// The turn failed for a reason that is known not to be about the
		// conversation: the vendor refused before any model call, or reported
		// its own service unavailable. Treated exactly as a cancellation —
		// nothing was learned, so nothing is forfeited — and the seat keeps its
		// probation, so the NEXT failure still costs it the id if that one is
		// unclassified.
		//
		// The seat says nothing special here. The failure already put the
		// vendor's own actionable sentence in c.Note ("not signed in —
		// authenticate this vendor…"), and adding "your thread survived" beside
		// it would be the room congratulating itself in the middle of somebody
		// else's error message.
	default:
		// The first turn on a restored id failed. Drop the id: retrying it would
		// rebuild the same dead invocation on every subsequent turn, and the
		// vendor would keep reporting a failure the user cannot act on.
		delete(m.unproven, c.Vendor)
		delete(m.sessions, c.Vendor)
		delete(m.resumeIDs, c.Vendor)
		c.Restored = false
		// The vendor's own words for this are about a missing session id, which
		// reads as a broken vendor and sends the user looking for a problem
		// with it. What they need to know is what happens to the next brief.
		//
		// The note used to open "the saved thread was refused — this seat's
		// history is gone", and that second clause was a diagnosis this code
		// cannot make. MEASURED 2026-08-04, agy 1.1.10, single trial: a
		// conversation id was round-tripped through `agy --conversation <id>`
		// and the thread was demonstrably ALIVE — the same conversation_id came
		// back, step_index CONTINUED (10 → 11) instead of restarting at 0, and
		// result.num_turns was 2 — and that same turn still ended status "ERROR"
		// with "Agent execution terminated due to error." A separate attempt
		// died before any thread was involved at all, on "Eligibility check
		// failed: UNAVAILABLE (code 503)". So a first turn that fails on a
		// restored id is not evidence that the history is gone; agy turns fail
		// transiently for reasons that have nothing to do with the conversation.
		//
		// The mechanism has since been narrowed rather than left alone: the
		// transient case above now keeps the id (sixteenth amendment). Reaching
		// HERE means the failure was not one of the two signals that have been
		// captured, so nothing is known and the one-attempt default stands.
		//
		// The claim stays exactly as narrow as it was — the turn failed, the
		// seat let the id go, here is what happens next — and it is now SHAPED
		// as a card rather than delivered as one long warning sentence. The
		// title is the outcome the user cares about and the mechanics hang
		// under it, quieter, which is the grammar every other card in a column
		// already uses. No warning mark: this is the same fact reattachCard
		// states calmly at idle when no thread came back, discovered a turn
		// later, and spending the ⚠ on it blunts the mark that carries real
		// failures.
		c.Note = "thread not restored — starting fresh"
		c.NoteDetail = "the first turn on the saved thread failed, so this seat let it go. " +
			"your next brief opens a new session, with the brief re-applied."
		c.NoteCalm = true
		m.threadLost[c.Vendor] = true
	}
}

// recordAct folds one piece of tool-call news into a column's trace.
//
// Update-or-append, correlated by the vendor's own id rather than by arrival
// order — which is not defensive programming. The very FIRST live Claude probe
// came back with the second call's failure ahead of the first call's success,
// so a trace zipped by position would have blamed the wrong command on its
// first real run.
//
// A result whose id matches nothing still lands, as an entry that is already
// resolved. That is what keeps the trace honest for a vendor that reports only
// completions: the step shows up with its outcome rather than being dropped for
// having missed its own announcement.
func (c *Column) recordAct(a runner.ActCall, clean func(string) string) {
	text := strings.TrimSpace(clean(a.Text))

	if a.ID != "" {
		for i := range c.Acts {
			if c.Acts[i].ID != a.ID {
				continue
			}
			if c.Acts[i].Status == runner.ActDenied {
				// A denial is council's own record of a keystroke, and the
				// vendor is about to echo it back as an is_error tool_result
				// carrying our refusal text. Letting that overwrite the entry
				// would turn "you did not allow this" into "this failed" — the
				// one substitution the gate exists to prevent.
				return
			}
			if text != "" {
				c.Acts[i].Text = text
			}
			// A second announcement never un-resolves an entry. Downgrading a
			// known outcome back to pending would make a finished call look
			// like a running one, which is the one direction this trace must
			// not move.
			if a.Outcome != runner.ActPending {
				c.Acts[i].Status = a.Outcome
				c.Acts[i].Detail = strings.TrimSpace(clean(a.Detail))
			}
			return
		}
	}

	if text == "" {
		// A result for a call that was never announced and that carries no
		// text of its own. There is nothing to name it by, so there is nothing
		// worth rendering — an entry reading only "✗" would say a nameless
		// something failed.
		return
	}
	c.Acts = append(c.Acts, Act{
		ID:     a.ID,
		Text:   text,
		Status: a.Outcome,
		Detail: strings.TrimSpace(clean(a.Detail)),
	})
}

// turnColumnFinished retires one column from the turn and tears the turn down
// when the last one lands.
//
// Idempotent by vendor: a persistent seat can be retired by its end-of-turn
// line and then again by its process dying, and the turn must not end early
// because one column reported twice.
func (m *Model) turnColumnFinished(v model.VendorID) {
	ts := m.turnOf(v)
	if ts == nil {
		return
	}
	// The SEAT retires here, whatever its dispatch's other seats are doing
	// (§9.54): its entry leaves Model.turns, so the next brief can address it,
	// and its own context is pulled, which is what kills a child still winding
	// down — the dispatch-level cancel below used to do that for everyone at
	// once. The cancellation word dies with the seat's turn too; a later echo
	// from the stopped process meets the give-up guard instead (Model.givenUp).
	// cancelled is read before the flag goes, because it is the difference
	// between two words on the chain's death notice below.
	cancelled := m.cancelling[v]
	delete(m.turns, v)
	delete(m.cancelling, v)
	if scancel := ts.seatCancel[v]; scancel != nil {
		scancel()
	}
	delete(ts.live, v)
	if len(ts.live) > 0 {
		return
	}
	ts.cancel()
	// The whole dispatch has landed. Everything from here is a fact about the
	// dispatch, not the room: the room may still hold other seats in flight.
	m.dispatchEnded = true
	// A live chain that did not hand off by the time its turn tore down is over:
	// the hop was cancelled, its vendor failed, or it returned nothing
	// finishFlowHop could carry — none of which reach finishFlowHop's own
	// endings, so this teardown is the one place every such chain passes
	// through. Before this sweep the chain survived a ctrl+c as a corpse: the
	// header went on claiming "hop 1/3", and the user's next brief was swallowed
	// by "flow start error: cannot start step in state running" (measured
	// 2026-08-08, §9.35). The death is said out loud, and says WHICH death —
	// cancelled and failed are different facts, and a stopped chain must never
	// read as a finished one (§4a.1). Checked before cancelling is reset, since
	// that flag is the difference between the two words.
	//
	// Only the HOP's own dispatch may bury the chain (ts.flow): an unrelated
	// brief landing while a hop streams is not the hop dying, and reading it
	// as one would end a chain that is still working.
	if ts.flow && m.flowChain != nil && !m.flowAdvancePending {
		// The first hop of the stage that did not finish, which on a fan is
		// not always the first hop of the stage (§9.55).
		curr := m.flowChain.Unfinished()
		if curr != nil {
			hop, total := m.flowChain.StageN(), m.flowChain.Stages()
			verb := "stopped"
			if cancelled {
				verb = "cancelled"
			}
			note := fmt.Sprintf("flow %s at hop %d/%d (@%s %s)", verb, hop, total, curr.Vendor, curr.Verb)
			if rest := total - hop; rest > 0 {
				note += " — " + hopsWord(rest) + " not run"
			}
			m.st.Notice = joinNotice(m.st.Notice, note)
		}
		m.endFlowChain()
	}
	// The route stops being live news the instant the turn is over: each column
	// has recorded its own participation by now, so a header that went on naming
	// it would be repeating history in the one cell that describes the present
	// (§9.21). Since §9.54 the header names the MOST RECENT dispatch, so only
	// that one retires the cell — an older dispatch landing after a newer one
	// was sent must not blank a route that is still true.
	if ts.n == m.st.Turn {
		m.st.TurnRoute = nil
	}
	m.st.Mode = ModeComposing
	// The turn is over, so the ids it produced are worth keeping. Saved HERE
	// rather than only on the way out, because the failure this exists to
	// survive is the room not getting a clean exit — a crash, a closed terminal,
	// a machine that went down. State written only at quit would be missing in
	// exactly those cases.
	m.saveRoom()
}

// saveRoom writes the keys needed to reattach this room later.
//
// Best effort by design: a room that cannot write its state file is still a
// working room, and refusing to continue over it would trade the whole session
// for a convenience. The failure is stated in the footer rather than swallowed,
// because a user who quits believing they can reattach and then cannot has been
// told something false by silence.
//
// Nothing is written before the first turn. A room with no turns has no keys to
// save, and writing one anyway would drop a file into ~/.telltale/council for
// every accidental launch — including one opened in the wrong directory and
// immediately quit.
func (m *Model) saveRoom() {
	// A replay never writes room.json (replay.go): its turn counter is the
	// recording's, and saving it would point the operator's next live room at
	// a conversation that happened on another day, possibly on another
	// machine.
	if m.st.Turn == 0 || m.replay != nil {
		return
	}
	sessions := make(map[model.VendorID]string, len(m.sessions))
	for v, id := range m.sessions {
		if id != "" {
			sessions[v] = id
		}
	}
	err := SaveRoom(SavedRoom{
		Workspace: m.st.Workspace,
		// The room's SHAPE, and the half of §9.32's line that comes back: where
		// it was pointed and who was at the table. Read off State, not off
		// opts.Seats, because `/seat` has been moving it from inside the room
		// since §9.17 — the flag is only the seed.
		Seats: m.st.Seats,
		// Its AUTHORITY, recorded and never restored. Both arguments are LIVE
		// state (§9.32): this used to pass m.opts.Auto beside m.st.Write, so a
		// room told `a` saved a description of a room nobody was in.
		Posture:  savedPosture(m.st.Write, m.st.Asking()),
		Turn:     m.st.Turn,
		Sessions: sessions,
		// The PATH only. Never the content — see SavedRoom and Brief.
		BriefPath: m.brief.Path,
		SavedAt:   time.Now(),
	})
	// Held as well as announced. The footer serves a room that is still running;
	// the field serves the save on the way out, whose notice would be set on a
	// model nobody will ever see again — Run prints that one to stderr.
	m.saveErr = err
	if err != nil {
		m.st.Notice = "the room state could not be saved: " + err.Error()
	}
}

// turnOf is the dispatch this seat is answering, or nil when the seat is idle
// (§9.54). It is THE read of Model.turns: every site that used to ask `m.turn
// != nil` about a seat asks this, and the answer is about that seat alone.
func (m *Model) turnOf(v model.VendorID) *turnState { return m.turns[v] }

// anyInFlight reports that at least one seat is still answering — the
// room-wide question, kept for the few acts that genuinely need the whole room
// idle (quit, /cd, /seat, a race) and deliberately NOT the test dispatch uses.
func (m *Model) anyInFlight() bool { return len(m.turns) > 0 }

// dispatches is every distinct dispatch still in flight — the walk teardown
// makes. Deduplicated by pointer, because a brief to @all is one record three
// seats point at. Read off the map rather than off the columns, so a seat that
// is in flight without a column (a fixture; or a seat unseated mid-turn, which
// /unseat refuses but a future path might not) is still reaped.
func (m *Model) dispatches() []*turnState {
	var out []*turnState
	seen := map[*turnState]bool{}
	for _, ts := range m.turns {
		if !seen[ts] {
			seen[ts] = true
			out = append(out, ts)
		}
	}
	return out
}

// race is the arena dispatch in flight, or nil. A race refuses to start over a
// busy seat and refuses every brief while it runs, so at most one exists and
// it is the only dispatch — which is what lets the arena's shared bookkeeping
// (its trees, its rank, its live-stat slots) be read from any of its seats.
func (m *Model) race() *turnState {
	for _, ts := range m.turns {
		if ts.arena {
			return ts
		}
	}
	return nil
}

// racing reports that this seat's own turn is an arena attempt — the per-seat
// spelling of what `m.turn != nil && m.turn.arena` used to say for the room.
func (m *Model) racing(v model.VendorID) bool {
	ts := m.turnOf(v)
	return ts != nil && ts.arena
}

// isPersistent reports whether this seat's turn is running on a long-lived
// process. Read from the seat's turn rather than from the registry, so a seat
// that fell back to a spawn is treated as what it actually is.
func (m *Model) isPersistent(v model.VendorID) bool {
	ts := m.turnOf(v)
	return ts != nil && ts.persistent[v]
}

// ephemeralRacer returns the throwaway session racing this vendor in the
// current turn, or nil on every other kind of turn.
//
// Read from the seat's turn for isPersistent's reason, and one more of its own:
// this is the fact applyEvents attributes a vendor's events by while TWO
// processes wear one vendor id (the room's idle seat and the racer), and a
// lookup that outlived the turn would go on attributing exits to a racer that
// has already been reaped.
func (m *Model) ephemeralRacer(v model.VendorID) seatSession {
	ts := m.turnOf(v)
	if ts == nil {
		return nil
	}
	return ts.arenaEphemeral[v]
}

// arenaRacing reports whether this vendor is racing on a ONE-SHOT process this
// turn — the sibling of ephemeralRacer, and read from the turn for the same
// reason it is.
//
// The two together are the whole of the attribution KindDone needs: a vendor
// racing at all wears its exit itself, and a vendor that is not gets the
// stale-exit guard. Keyed presence is the test rather than liveness, because a
// handle is not a session and cannot be asked whether it is alive — which is
// exactly the case: the process has already exited, and the map is what says
// whose exit it was.
func (m *Model) arenaRacing(v model.VendorID) bool {
	ts := m.turnOf(v)
	if ts == nil {
		return false
	}
	_, racing := ts.arenaHandles[v]
	return racing
}

// wasGivenUp reports whether the operator cut this seat with `x` and the cut
// has not been superseded by a new dispatch to it (Model.givenUp).
//
// It used to read the turn, and the turn's lifetime bounded it. Per-seat turns
// end the moment the cut column lands (§9.54), while the stopped process is
// still draining — so the fact lives on the Model and is retired by the seat's
// next dispatch instead: a seat cut in turn 4 is an ordinary seat again the
// moment turn 5 reaches it, which is the same boundary drawn one event later.
func (m *Model) wasGivenUp(v model.VendorID) bool { return m.givenUp[v] }

// cancelSeat stops ONE seat's turn (§9.54). The column keeps whatever it
// already received: that output was really produced, and the card says it is
// partial rather than implying the turn completed. Its neighbours — on the same
// dispatch or another — are not touched.
//
// A persistent seat is INTERRUPTED rather than killed. Killing it would work,
// and it would also throw away the conversation and the session-init cost that
// bought it — so cancelling one turn would silently make the next one expensive.
// The vendor offers a real interrupt, it was measured, and the process was still
// answering afterwards; if the message cannot be delivered the seat is killed
// instead, which is the old behaviour and is stated in the column rather than
// hidden.
//
// An ephemeral racer is KILLED, not interrupted, and the asymmetry is the
// point: an interrupt exists to spare a conversation and its session-init cost,
// and a throwaway race session has neither — nothing resumes it, nothing is
// saved from it. A cancel that waited politely would be waiting on the ~2.5s
// post-answer linger §9.33 measured, for a process whose next act is the bin
// either way.
//
// It reports whether there was a turn to cancel, so the key can tell "stopped
// the focused seat" from "nothing was running there".
func (m *Model) cancelSeat(v model.VendorID) bool {
	ts := m.turnOf(v)
	if ts == nil {
		return false
	}
	m.cancelling[v] = true
	switch {
	case ts.persistent[v]:
		m.interruptSeat(v)
	default:
		if es, ok := ts.arenaEphemeral[v]; ok {
			es.Kill()
		} else if h, ok := ts.arenaHandles[v]; ok {
			h.Kill()
		} else if h, ok := ts.seatHandles[v]; ok {
			h.Kill()
		}
	}
	// The seat's own context, which is what runner.Start kills the child on.
	// The dispatch-level context is left alone: it is the parent of every
	// sibling's, and pulling it would cancel the seats this act is sparing.
	if scancel := ts.seatCancel[v]; scancel != nil {
		scancel()
	}
	return true
}

// cancelAll stops every seat in flight, on every dispatch — the whole-room act
// ctrl+c performs when the focused seat has nothing running (§9.54), and what
// the key did unconditionally before that.
func (m *Model) cancelAll() {
	if !m.anyInFlight() {
		return
	}
	m.st.Notice = "cancelling…"
	for _, c := range m.st.Columns {
		m.cancelSeat(c.Vendor)
	}
}

// teardown kills every child on the way out.
//
// Without this, quitting the room would leave agents running, holding sessions
// and spending quota, with nothing on screen to say so — the exact invisible
// state this product exists to refuse. The persistent seats are included, and
// they are the reason this matters more than it did: a process that survives a
// turn by design is exactly the process that would survive the room by accident.
//
// ONE-SHOT, and safe to call from two goroutines at once. Two callers reach it
// now: the q and ctrl+c keys, on the update loop, and the exit-signal watcher on
// its own goroutine (signals_unix.go). Those two can arrive together — a `kill`
// landing on a room the user is already quitting — and the loop below deletes
// from the map it ranges over, which two goroutines cannot do at the same time
// without a runtime panic. The second caller returns at the flag instead; see
// Model.teardownDone for why one shot is the right shape rather than a lock the
// act can re-enter.
func (m *Model) teardown() {
	m.teardownMu.Lock()
	defer m.teardownMu.Unlock()
	if m.teardownDone {
		return
	}
	m.teardownDone = true
	// The room is on its way out, whatever it finds below. Set before the kill
	// loop so the closing line is printed even by a teardown that panics past
	// this point — a room that ended seats and said nothing is the defect
	// §9.52 exists to close.
	m.closed = true
	// Written before anything is killed, so the last thing the room does with
	// its state is preserve it. Redundant with the per-turn save in the common
	// case and deliberately kept: it refreshes saved-at, which is what the
	// reattach card ages, so "saved 2h ago" means the room was last OPEN two
	// hours ago rather than merely last answered then.
	m.saveRoom()
	// Every seat gets its last word, its stdin closed, and its grace before
	// the kill (stopProc, vendors.GracefulStop) — and every seat's grace runs
	// at the SAME time, so a room of three live seats waits for the longest
	// of them rather than the sum. The wait is here, on the way out, because
	// this is the one caller that must not return while a child may still be
	// running: the closing line counts the agents this teardown ended, and a
	// kill issued after the room has exited is a kill nobody made.
	var stops []<-chan struct{}
	for v, p := range m.procs {
		stops = append(stops, stopProc(p))
		delete(m.procs, v)
		// Counted here rather than from len(m.procs) before the loop, so the
		// figure the closing line prints is the number of Kill calls this
		// teardown actually made (§9.52).
		m.ended++
	}
	for _, done := range stops {
		<-done
	}
	// The live seat is a vendor process too (§9.53), so it dies here and is
	// counted here. Killed explicitly rather than left to the room context that
	// would also reach it: the closing line reports how many agents this
	// teardown ended, and a child that died from a cancellation somewhere below
	// would go uncounted while still having been ended by this room.
	if m.killLive() {
		m.ended++
	}
	// The children die before the file they were pointed at is removed. The
	// other order would leave a live seat holding a path to a deleted hooks
	// file, which is the shape of a seat that quietly stops being screened.
	m.hooks.Cleanup()
	// A setup still cutting worktrees is a git process this room started, so it
	// dies with the room for the same reason every vendor child does — quitting
	// must never leave one running with nothing on screen to say so. The trees
	// it already added are KEPT (stopArenaSetup's ruling); it is the running
	// command that is ended here, not the receipts.
	if m.arenaPrep != nil {
		m.arenaPrep.cancel()
		m.arenaPrep = nil
		m.st.ArenaSetup = ""
	}
	if m.roomCancel != nil {
		m.roomCancel()
	}
	// Every dispatch still in flight, each walked once however many seats point
	// at it (dispatches). This is where the room used to read m.turn; with
	// several dispatches live at once (§9.54) every one of them is reaped, and
	// every process each one holds — the one-shot children by their handles,
	// the racers too. The racers are exactly the process the paragraph above
	// warns about: not in procs (by design, see turnState.arenaEphemeral), so
	// the loop over procs never sees them, and a quit mid-race would otherwise
	// leave a vendor's ACP server running in a worktree with nothing on screen
	// to say so. The contexts cancelled below kill the real ones again; the
	// explicit kills are what make the property hold synchronously and under
	// test.
	for _, ts := range m.dispatches() {
		for _, h := range ts.seatHandles {
			h.Kill()
		}
		for _, h := range ts.arenaHandles {
			h.Kill()
		}
		for _, es := range ts.arenaEphemeral {
			es.Kill()
		}
		ts.cancel()
	}
	m.turns = map[model.VendorID]*turnState{}
}

func (m *Model) column(v model.VendorID) *Column {
	for i := range m.st.Columns {
		if m.st.Columns[i].Vendor == v {
			return &m.st.Columns[i]
		}
	}
	return nil
}

func (m *Model) redact(v model.VendorID, s string) string {
	r, ok := m.redactors[v]
	if !ok {
		r = &Redactor{}
		m.redactors[v] = r
	}
	return sanitize(r.Feed(s))
}

// redactWhole redacts a string that is already COMPLETE.
//
// Deliberately NOT the streaming redactor. That one holds a partial word across
// the chunks of a token stream, and routing an activity line through it did two
// wrong things at once: it stranded the line's own last word until the next
// tool call, and — because the fix for that was to flush — it spliced whatever
// prose was buffered for the BODY onto the end of a command. A tool call
// arrives whole, so it can be redacted whole, and the body's buffer is left
// alone.
//
// Redaction still applies, and matters more here than in prose: a shell command
// is one of the likeliest places for a token to appear on screen.
func (m *Model) redactWhole(s string) string { return sanitize(Redact(s)) }

func (m *Model) flush(v model.VendorID) string {
	r, ok := m.redactors[v]
	if !ok {
		return ""
	}
	return sanitize(r.Flush())
}
