package council

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

type turnState struct {
	cancel  context.CancelFunc
	handles []*runner.Handle
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

	// arena marks a /arena turn: every racing seat is a FRESH one-shot session
	// in its own worktree (arena.go). The flag gates two things — the session-id
	// capture in applyEvents, because a race's throwaway ids must never replace
	// the room's saved threads, and the diff collection in finishColumn.
	arena      bool
	arenaBase  string
	arenaTrees map[model.VendorID]string
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
func (m *Model) dispatch() tea.Cmd {
	if m.turn != nil {
		m.st.Notice = "a turn is already in flight — ctrl+c cancels it"
		return nil
	}
	activeFlow := m.flowChain != nil && m.flowChain.Current() != nil && m.flowDraft != ""
	if m.st.Draft == "" && !activeFlow {
		m.st.Notice = "nothing to dispatch: the brief is empty"
		return nil
	}

	reg := vendors.Registry()
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
		if curr == nil {
			m.st.Notice = "flow has no current step"
			m.flowChain = nil
			m.clearFlowMarker()
			return nil
		}
		// Set before the gates below, not after them. Every path from here can
		// return without dispatching — a blocked write hop, a refused one — and
		// leaving the marker on the PREVIOUS hop would point at the seat that
		// already finished while the room waits on this one.
		m.st.FlowHop = m.flowChain.CurrentIndex + 1
		m.st.FlowSteps = len(m.flowChain.Steps)
		m.st.FlowVendor = curr.Vendor
		// Posture comes from the STEP. A hop with no declared target is a read
		// hop even in a --write room, and this is set before any of the paths
		// below can spawn anything.
		m.flowReadHop = !curr.RequiresWriteGate()

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
		if curr.RequiresWriteGate() && !m.st.Write {
			if curr.State == FlowStateQueued {
				_ = m.flowChain.MarkAwaitingWrite("the room is read-only; write hops need a room that can write — /write lets it")
			}
			m.flowWritePending = false
			m.flowWriteArmed = false
			m.st.Notice = fmt.Sprintf("flow blocked at step %d: @%s → %s is a write hop and the room is read-only — /write lets it, between turns",
				m.flowChain.CurrentIndex+1, curr.Vendor, curr.Path)
			return nil
		}

		// Pre-dispatch write gate: Path marks write authority. Do not spawn the
		// seat until the user authorizes (y).
		if curr.RequiresWriteGate() && !m.flowWriteArmed {
			if curr.State == FlowStateQueued {
				_ = m.flowChain.MarkAwaitingWrite("awaiting user authorization before write hop runs")
			}
			m.flowWritePending = true
			m.st.Notice = fmt.Sprintf("flow write gate: y authorizes @%s → %s · n cancels", curr.Vendor, curr.Path)
			return nil
		}
		if curr.State == FlowStateBlocked && m.flowWriteArmed {
			if err := m.flowChain.ClearBlockForStart(); err != nil {
				m.st.Notice = "flow gate: " + err.Error()
				return nil
			}
		}
		if err := m.flowChain.Start(m.st.Workspace); err != nil {
			m.st.Notice = "flow start error: " + err.Error()
			// The whole chain, marker included — this used to nil the chain by
			// hand and leave the header claiming a hop, the half-cleared state
			// endFlowChain exists to make unrepresentable (§9.35).
			m.endFlowChain()
			return nil
		}
		m.flowWriteArmed = false
		m.flowWritePending = false
		curr = m.flowChain.Current()
		route = Route{Vendors: []model.VendorID{curr.Vendor}}
		prompt = strings.TrimSpace(curr.Task)
		if prompt == "" {
			prompt = curr.Verb
		}
		if m.flowCarry != "" {
			prompt += "\n\n" + m.flowCarry
			m.flowCarry = ""
		}
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

	// Geometry for this turn is decided here, from the route, and stays until
	// the next dispatch. Empty FrameOwners = equal columns (@all / everyone).
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
	priorReplies := append([]Column(nil), m.st.Columns...)

	ctx, cancel := context.WithCancel(context.Background())
	ts := &turnState{
		cancel:     cancel,
		live:       map[model.VendorID]bool{},
		persistent: map[model.VendorID]bool{},
	}
	var failures []dispatchFailedMsg

	// What the transcript echoes, for every column this turn touches. The
	// user's brief with the mentions stripped — the same text the vendors are
	// asked about — through the one sanitize choke point everything else on
	// State goes through. Not redacted: see promptEcho.
	echo := sanitize(prompt)
	next := m.st.Turn + 1

	// Worktrees are added BEFORE any seat spawns, and the base SHA is read once
	// so all attempts race from the same commit. A workspace that is not a git
	// repo fails here, wholesale, with git's own sentence; a single seat whose
	// worktree could not be added is skipped and told why, because a partial
	// race still answers the brief (§4a.1's degrade-the-field rule, one level
	// up).
	var arenaSeatErr map[model.VendorID]string
	if arenaMode {
		var racers []model.VendorID
		for i := range m.st.Columns {
			c := m.st.Columns[i]
			if _, ok := reg[c.Vendor]; ok && m.st.seats(c) {
				racers = append(racers, c.Vendor)
			}
		}
		base, trees, seeds, seatErrs, aerr := arenaSetup(m.st.Workspace, next, racers)
		if aerr != nil {
			cancel()
			m.st.Notice = "arena: " + aerr.Error()
			return nil
		}
		ts.arena, ts.arenaBase, ts.arenaTrees, ts.arenaSeeds, arenaSeatErr = true, base, trees, seeds, seatErrs
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
		m.lastRace = &arenaRace{workspace: m.st.Workspace, turn: next, base: base, trees: raceTrees}
	}

	for i := range m.st.Columns {
		c := &m.st.Columns[i]
		if !m.st.seats(*c) {
			continue
		}
		// Cleared for every column the loop reaches, BEFORE any of the paths
		// below can skip one. A refused thread belongs to the turn that refused
		// it, and a flag left set on a seat that is merely unaddressed — or that
		// fails to dispatch for an unrelated reason — would suppress the next
		// turn's genuine failure note.
		delete(m.threadLost, c.Vendor)
		delete(m.failure, c.Vendor)
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
		// each one is shown the others' answers and not its own.
		vendorPrompt := prompt
		if quoting {
			vendorPrompt = BuildRebuttalPrompt(prompt, *c, priorReplies)
		}

		// A seat with a long-lived process is handed the turn on the stdin it
		// already has open. A seat without one pays a fresh spawn, as before.
		//
		// note is carried past the column reset below, because that reset is
		// what clears the PREVIOUS turn's note and this one is about THIS turn.
		note := ""
		if arenaMode {
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
			if cv, ok := v.(vendors.Conversational); ok {
				// The one seat FirstTurn cannot carry. The ACP refounding made
				// this vendor live-only (§9.36) and the first live race duly
				// surfaced its refusal on the column (§9.37's verification
				// note), so its race is the sanctioned follow-up built as
				// specified: a THROWAWAY ACP session in the racer's worktree —
				// one process, one session, one prompt, killed when the column
				// lands. §9.36's own machinery pointed at a throwaway session,
				// not a second protocol.
				sess, err := m.startEphemeralRacer(ctx, cv, c, tree, vendorPrompt)
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
				h, err := startProcess(ctx, spec, m.events, v.ParseEvent)
				if err != nil {
					failures = append(failures, dispatchFailedMsg{c.Vendor, err.Error()})
					continue
				}
				ts.handles = append(ts.handles, h)
			}
		} else if liveSeat(v) {
			n, err := m.sendPersistentTurn(v, c, vendorPrompt)
			if err != nil {
				failures = append(failures, dispatchFailedMsg{c.Vendor, err.Error()})
				continue
			}
			ts.persistent[c.Vendor] = true
			note = n
		} else {
			spec, err := m.specFor(v, c, vendorPrompt)
			if err != nil {
				failures = append(failures, dispatchFailedMsg{c.Vendor, err.Error()})
				continue
			}
			h, err := startProcess(ctx, spec, m.events, v.ParseEvent)
			if err != nil {
				failures = append(failures, dispatchFailedMsg{c.Vendor, err.Error()})
				continue
			}
			ts.handles = append(ts.handles, h)
		}

		ts.live[c.Vendor] = true
		// The finished turn goes to history and everything describing it is
		// reset — the line that used to be five assignments erasing the
		// previous answer off the screen.
		c.startTurn(next, echo, quoting)
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
			c.startTurn(next, echo, quoting)
			c.Phase = PhaseFailed
			c.Note = f.note
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
		return nil
	}

	m.turn = ts
	// The header carries this until the last column lands (§9.21). Set HERE and
	// not beside FrameOwners, even though both are the turn's intent captured at
	// the one moment it is known: everything above this line can still refuse
	// the dispatch, and a route on the header of a turn that never started would
	// be the room reporting a spend that never happened. The two also have
	// opposite lifetimes — the geometry outlives the turn so nothing reflows
	// under a reader, the route is retired the moment the turn ends.
	sent := route
	m.st.TurnRoute = &sent
	m.st.Turn++
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
	m.setDraft("")
	m.st.Notice = ""
	return m.waitEvents()
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
func frameOwnersFor(route Route, st State) []model.VendorID {
	if route.Mixed {
		return nil
	}
	var out []model.VendorID
	for _, idx := range st.VisibleColumns() {
		c := st.Columns[idx]
		if route.addresses(c.Vendor) {
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

// specFor builds this vendor's invocation for the current turn.
//
// A vendor with a session id resumes it; one without starts fresh. A resume
// that the vendor refuses is not silently downgraded — ErrNoResume falls back
// to a first turn, and the column says the thread was lost.
func (m *Model) specFor(v vendors.Vendor, c *Column, prompt string) (runner.Spec, error) {
	p := m.posture()
	if id := m.sessions[c.Vendor]; id != "" {
		// Resume: the brief is already in this vendor's own history.
		spec, err := v.NextTurn(prompt, m.st.Workspace, c.Binary, id, p)
		if err == nil {
			return spec, nil
		}
	}
	// First turn for THIS vendor, so it gets the operating context. Per vendor
	// rather than per room: a seat added to a later turn is still a stranger,
	// and would otherwise be the only one guessing.
	return v.FirstTurn(m.brief.Apply(prompt), m.st.Workspace, c.Binary, p)
}

// waitEvents blocks on one event, then drains what is already queued into a
// single batch. One redraw per batch instead of one per token.
func (m *Model) waitEvents() tea.Cmd {
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
			if ev.SessionID != "" && !(m.turn != nil && m.turn.arena) {
				m.sessions[ev.Vendor] = ev.SessionID
			}

		case runner.KindGate:
			m.queueGate(c, ev.Gate)

		case runner.KindMeta:
			if ev.SessionID != "" && !(m.turn != nil && m.turn.arena) {
				m.sessions[ev.Vendor] = ev.SessionID
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
				}
				m.finishColumn(c, PhaseDone)
			} else if ev.EndsTurn && m.isPersistent(ev.Vendor) {
				m.finishColumn(c, PhaseDone)
			}

		case runner.KindDone:
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
				if !m.cancelling {
					c.Note = "the racer's process ended before its turn did — no answer arrived; anything it wrote is in the diff"
				}
				m.finishColumn(c, PhaseFailed)
				continue
			}
			if p, ok := m.procs[ev.Vendor]; ok && p.sess != nil && p.sess.Alive() {
				continue
			}
			// A persistent process reaching here has DIED — the turn's end never
			// takes it down. Either the room is quitting, or something ended it
			// under us; both mean the seat has no process any more.
			persistent := m.isPersistent(ev.Vendor)
			m.dropProcess(ev.Vendor)
			c.Body += m.flush(ev.Vendor)
			if persistent && (c.Phase == PhaseStreaming || c.Phase == PhaseWaiting) && !m.cancelling {
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
			m.finishColumn(c, PhaseDone)

		case runner.KindError:
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
					// An interrupt lands here too: cancelling produced a result
					// with is_error true and terminal_reason "aborted_tools".
					// The user's keystroke is not a vendor failure, so
					// finishColumn's cancellation check re-labels it.
					if m.cancelling {
						c.Note = ""
					}
					m.finishColumn(c, PhaseFailed)
				} else {
					// The PROCESS failed. Nothing more is coming from it.
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
			c.Elapsed = time.Since(c.Started)
			c.Phase = PhaseFailed
			// A vendor-reported failure arrives BEFORE the process exits, so
			// this is not the end of the column's life; KindDone still follows
			// and is what retires it.
			if ev.ExitCode != 0 || ev.Err != nil {
				m.finishColumn(c, PhaseFailed)
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
	// Whatever this seat was waiting to be told, it is no longer waiting. A card
	// left up for a vendor that has stopped asking invites a keystroke that
	// decides nothing, and the footer would go on claiming the room is gated.
	m.dropGates(c.Vendor)
	if c.Elapsed == 0 && !c.Started.IsZero() {
		c.Elapsed = time.Since(c.Started)
	}
	if c.Phase == PhaseStreaming || c.Phase == PhaseWaiting {
		c.Phase = phase
		if m.cancelling {
			c.Phase = PhaseCancelled
			c.Note = "cancelled — the output above is partial"
		}
	}

	// The race's deliverable is the diff, so it is read the moment this seat
	// lands — including on a cancelled or failed attempt, whose partial work is
	// still a receipt in a kept worktree. Synchronous, and deliberately so for
	// v1: two `git diff` runs against a fresh worktree are milliseconds, and an
	// async path would add a message type for a stall nobody has measured. If a
	// monorepo ever makes this visible, that measurement is the trigger to move
	// it onto a Cmd.
	if m.turn != nil && m.turn.arena {
		if es, ok := m.turn.arenaEphemeral[c.Vendor]; ok {
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
			delete(m.turn.arenaEphemeral, c.Vendor)
		}
		if tree, ok := m.turn.arenaTrees[c.Vendor]; ok && c.Arena == nil {
			// c.Arena == nil makes collection once-only. A racer driven by a
			// live protocol retires twice — its end-of-turn response, then the
			// exit of the process that response got killed — and a second pass
			// here would re-rank the race on an echo.
			r := collectArena(tree, m.turn.arenaBase)
			if ls := m.turn.arenaLive[c.Vendor]; ls != nil {
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
					r = collectArena(tree, m.turn.arenaBase)
				}
			}
			r.Branch = arenaBranch(c.TurnN, c.Vendor)
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
				sha, cerr := commitArena(tree, m.turn.arenaBase, arenaCommitMsg(c.TurnN, c.Prompt))
				if cerr != nil {
					r.CommitErr = "not committed: " + cerr.Error()
				} else {
					r.Commit = sha
				}
			}
			// The seed receipt was measured at setup; the column that states
			// it exists now. nil when the room repo has no .worktreeinclude —
			// the render draws nothing for nil, per zero-vs-absent.
			r.Seed = m.turn.arenaSeeds[c.Vendor]
			// Rank is the order the room OBSERVED seats land, stamped here on
			// the host's clock. Every racer gets one — a DNF finished too, just
			// not well, and the render pairs the rank with the phase word so
			// "2nd" on a failed attempt cannot read as a result.
			m.turn.arenaFinished++
			r.Rank, r.Of = m.turn.arenaFinished, len(m.turn.arenaTrees)
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

// finishFlowHop records harness-observed completion of the active flow seat.
// Non-write hops become Returned (not approved). Write hops (Path set) must
// already have been user-gated before dispatch; on PhaseDone we verify the disk
// receipt and MarkPublished or MarkFailed. Artifact save failure fails the hop.
func (m *Model) finishFlowHop(c *Column) {
	if m.flowChain == nil || c.Phase != PhaseDone || strings.TrimSpace(c.Body) == "" {
		return
	}
	curr := m.flowChain.Current()
	if curr == nil || c.Vendor != curr.Vendor || curr.State != FlowStateRunning {
		return
	}

	store, err := NewArtifactStore()
	if err != nil {
		_ = m.flowChain.MarkFailed("artifact store: " + err.Error())
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
		_ = m.flowChain.MarkFailed("artifact save: " + err.Error())
		m.st.Notice = "flow hop failed: " + err.Error()
		m.endFlowChain()
		return
	}
	m.st.Notice = "artifact saved: " + path

	if curr.RequiresWriteGate() {
		receipt := VerifyReceipt(m.st.Workspace, curr)
		curr.Receipt = receipt
		if !receipt.Verified {
			_ = m.flowChain.MarkFailed(receipt.Detail)
			m.st.Notice = joinNotice(m.st.Notice, "publish failed: "+receipt.Detail)
			m.endFlowChain()
			return
		}
		if err := m.flowChain.MarkPublished(receipt); err != nil {
			m.st.Notice = joinNotice(m.st.Notice, err.Error())
			return
		}
		m.st.Notice = joinNotice(m.st.Notice, "flow hop published ("+receipt.Detail+")")
	} else {
		if err := m.flowChain.MarkReturned(); err != nil {
			m.st.Notice = joinNotice(m.st.Notice, err.Error())
			return
		}
		m.st.Notice = joinNotice(m.st.Notice, fmt.Sprintf("flow hop %d returned (@%s %s) — not an approval", m.flowChain.CurrentIndex+1, curr.Vendor, curr.Verb))
	}

	// `s` was pressed while this hop ran. The hop itself finished on its own
	// terms — artifact saved, receipt verified, Returned or Published exactly as
	// recorded above — and the chain ends here instead of handing off. Checked
	// AFTER the hop's record is written, because stopping is about the NEXT hop
	// and must not cost this one its receipt; and BEFORE the artifact is read
	// back, because a stopped chain has no successor to feed (§9.35).
	if m.st.FlowStop {
		hop, total := m.flowChain.CurrentIndex+1, len(m.flowChain.Steps)
		m.st.Notice = joinNotice(m.st.Notice, fmt.Sprintf(
			"flow stopped after hop %d/%d — %s not dispatched", hop, total, hopsWord(total-hop)))
		m.endFlowChain()
		return
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
	artifact, err := store.LoadArtifactBody(sessID, c.TurnN, c.Vendor)
	if err != nil {
		m.st.Notice = joinNotice(m.st.Notice, "flow stopped: cannot read this hop's artifact back: "+err.Error())
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
	m.flowCarry = FormatFencedArtifact(c.Label, c.TurnN, artifact)
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
	if m.turn == nil {
		return
	}
	if !m.turn.live[v] {
		return
	}
	delete(m.turn.live, v)
	if len(m.turn.live) > 0 {
		return
	}
	m.turn.cancel()
	m.turn = nil
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
	if m.flowChain != nil && !m.flowAdvancePending {
		curr := m.flowChain.Current()
		if curr != nil && curr.State != FlowStateReturned && curr.State != FlowStatePublished {
			hop, total := m.flowChain.CurrentIndex+1, len(m.flowChain.Steps)
			verb := "stopped"
			if m.cancelling {
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
	m.cancelling = false
	// The route stops being live news the instant the turn is over: each column
	// has recorded its own participation by now, so a header that went on naming
	// it would be repeating history in the one cell that describes the present
	// (§9.21).
	m.st.TurnRoute = nil
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
	if m.st.Turn == 0 {
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

// isPersistent reports whether this seat's turn is running on a long-lived
// process. Read from the turn rather than from the registry, so a seat that
// fell back to a spawn is treated as what it actually is.
func (m *Model) isPersistent(v model.VendorID) bool {
	return m.turn != nil && m.turn.persistent[v]
}

// ephemeralRacer returns the throwaway session racing this vendor in the
// current turn, or nil on every other kind of turn.
//
// Read from the turn for isPersistent's reason, and one more of its own: this
// is the fact applyEvents attributes a vendor's events by while TWO processes
// wear one vendor id (the room's idle seat and the racer), and a lookup that
// outlived the turn would go on attributing exits to a racer that has already
// been reaped.
func (m *Model) ephemeralRacer(v model.VendorID) seatSession {
	if m.turn == nil {
		return nil
	}
	return m.turn.arenaEphemeral[v]
}

// cancelTurn stops everything in flight. The columns keep whatever they already
// received: that output was really produced, and the card says it is partial
// rather than implying the turn completed.
//
// A persistent seat is INTERRUPTED rather than killed. Killing it would work,
// and it would also throw away the conversation and the session-init cost that
// bought it — so cancelling one turn would silently make the next one expensive.
// The vendor offers a real interrupt, it was measured, and the process was still
// answering afterwards; if the message cannot be delivered the seat is killed
// instead, which is the old behaviour and is stated in the column rather than
// hidden.
func (m *Model) cancelTurn() {
	if m.turn == nil {
		return
	}
	m.cancelling = true
	m.st.Notice = "cancelling…"
	for _, h := range m.turn.handles {
		h.Kill()
	}
	for v := range m.turn.persistent {
		m.interruptSeat(v)
	}
	// An ephemeral racer is KILLED, not interrupted, and the asymmetry against
	// the loop above is the point: an interrupt exists to spare a conversation
	// and its session-init cost, and a throwaway race session has neither —
	// nothing resumes it, nothing is saved from it. A cancel that waited
	// politely here would be waiting on the ~2.5s post-answer linger §9.33
	// measured, for a process whose next act is the bin either way.
	for _, es := range m.turn.arenaEphemeral {
		es.Kill()
	}
	m.turn.cancel()
}

// teardown kills every child on the way out.
//
// Without this, quitting the room would leave agents running, holding sessions
// and spending quota, with nothing on screen to say so — the exact invisible
// state this product exists to refuse. The persistent seats are included, and
// they are the reason this matters more than it did: a process that survives a
// turn by design is exactly the process that would survive the room by accident.
func (m *Model) teardown() {
	// Written before anything is killed, so the last thing the room does with
	// its state is preserve it. Redundant with the per-turn save in the common
	// case and deliberately kept: it refreshes saved-at, which is what the
	// reattach card ages, so "saved 2h ago" means the room was last OPEN two
	// hours ago rather than merely last answered then.
	m.saveRoom()
	for v, p := range m.procs {
		p.sess.Kill()
		delete(m.procs, v)
	}
	// The children die before the file they were pointed at is removed. The
	// other order would leave a live seat holding a path to a deleted hooks
	// file, which is the shape of a seat that quietly stops being screened.
	m.hooks.Cleanup()
	if m.roomCancel != nil {
		m.roomCancel()
	}
	if m.turn == nil {
		return
	}
	for _, h := range m.turn.handles {
		h.Kill()
	}
	// The racers too — they are exactly the process the paragraph above warns
	// about: not in procs (by design, see turnState.arenaEphemeral), so the
	// loop over procs never sees them, and a quit mid-race would otherwise
	// leave a vendor's ACP server running in a worktree with nothing on screen
	// to say so. The turn context cancelled below kills the real ones again;
	// the explicit kill is what makes the property hold synchronously and
	// under test.
	for _, es := range m.turn.arenaEphemeral {
		es.Kill()
	}
	m.turn.cancel()
	m.turn = nil
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
