package council

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The write acknowledgement card (LEDGER.md, 2026-09-04).
//
// # What it is for
//
// The stage sentence says telltale "refuses to drive one unwatched". Before
// this file the room SHOWED an unwatched seat and drove it anyway: the badge
// row said `WRITES`, the postures page said `unasked`, and a write brief still
// reached the seat on one press of enter. The 2026-09-04 adversarial review
// named that gap as fault C, and the owner ruled the sentence true rather than
// narrower. This card is the mechanism the second clause now names.
//
// The rule is one sentence. A write brief may reach a seat that writes
// unasked, or a seat whose asking nothing has measured, only after the
// operator acknowledges it on a card that names the seat.
//
// # What it is NOT
//
// It is not containment. The workspace is still the containment, and the
// worktree per writing seat is still the control that holds (docs/council.md).
// A seat that writes unasked writes unasked after `y` exactly as it did
// before. What the card adds is that the operator SAW the seat named, once per
// turn, before the brief left the room.
//
// It is not a second gate queue either. A gate card is a vendor stopped
// mid-call; this card is a turn the room has not sent. The two are separate
// states, they render in separate places, and this one is answered in one
// keystroke.
//
// # The NEEDS YOU lead does NOT ride on it
//
// The 2026-09-03 ruling in LEDGER.md is exact: `NEEDS YOU` means a vendor is
// stopped on a keystroke, and nothing else may say it. While this card is up
// no vendor is stopped, because no vendor has been started: the room holds the
// brief and spawns nothing. So the needs-you strip is untouched by this card,
// and what the card borrows from the gate is the LEAD TREATMENT its own title
// wears: the warning mark and the Alert style that say "this is the thing you
// are about to answer" (gateCardLines). The words are the signal, as always,
// and `--ascii` and NO_COLOR read the identical sentence.
//
// # Where the classes come from
//
// The card reads `seatShape` (detect.go) and nothing else, so the card and the
// badge cannot disagree about a seat. `seatShape` states three words: the
// protocol, whether the seat can be ASKED, and the evidence class. The middle
// word is the one this file reads.

// ackClass is how a seat's write posture answers one question: can the room
// make this seat ask before it changes something?
//
// Three answers, and only the first keeps a seat off the card. The complement
// of the card is exactly `gated`, which is the owner's rule stated as code: a
// seat is acknowledged unless it asks about everything that changes anything,
// measured.
type ackClass uint8

const (
	// ackGated is the one class that is not acknowledged: the seat asks before
	// every tool call and the room answers (canGate). Claude Code alone, and
	// only while the room is still asking.
	ackGated ackClass = iota
	// ackUnasked is a seat that writes without a card. It covers three shapes
	// that differ in evidence and not in consequence: a live shape measured
	// NOT asking (grok at 1.0.13), a live shape read from documentation as not
	// asking (antigravity), a seat measured asking about some things and not
	// about edits (cursor), and every seat that fell back to its batch adapter,
	// which asks about nothing.
	ackUnasked
	// ackUnmeasured is a seat that CAN be asked where nothing has measured that
	// it does. Codex's `app-server` thread is the live case: it routes an
	// approval request to the room's card, and no live run has produced one.
	//
	// Kept apart from ackUnasked by the zero-versus-absent rule the whole
	// product is written under. A seat nobody has measured is named as
	// unmeasured, never as unasked: the first is an absent measurement and the
	// second is a measurement.
	ackUnmeasured
)

// ackAskingWord is the middle word of a seat's shape (detect.go, seatShape):
// `asks` or `unasked`, and empty for a seat with no shape at all.
//
// Read out of the shape string rather than stated a second time here. The
// badge, the postures page and this card then say one thing, and a seat whose
// asking word moves (grok's moved on 2026-09-04) moves on every surface at
// once. TestAckReadsEverySeatShape pins the parse against every vendor and
// both fallback states, so a shape that stopped carrying three words would
// fail a test rather than silently classify a seat as unasked.
func ackAskingWord(v model.VendorID, fellBack bool) string {
	parts := strings.Split(seatShape(v, fellBack), " · ")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// ackClassFor is the seat's class, from the room's own facts.
//
// The arguments are the two facts postureClaimFor reads for the same decision:
// whether the room is still asking, and whether this seat retreated to its
// batch adapter. Taking them rather than a Column keeps this callable from a
// test that types out no room at all.
func ackClassFor(v model.VendorID, gated, fellBack bool) ackClass {
	// The fallback IS the measured batch adapter, so a fallen-back seat can
	// never be gated. No seat with a gate has a fallback today (Claude has
	// none), and the guard is here so that a seat which grows one cannot keep
	// the gated word by accident.
	if gated && !fellBack && canGate(v) {
		return ackGated
	}
	if ackAskingWord(v, fellBack) == "asks" {
		return ackUnmeasured
	}
	return ackUnasked
}

// PendingAck is the write brief the room is holding until the operator
// acknowledges the seats it reaches.
//
// A plain value on State, like PendingGate, so the card can be rendered by a
// test that types one out by hand and so Render stays pure over State.
//
// Two lists rather than one list of classified seats. The card states the two
// claims separately and they are different claims: one says a seat writes with
// no card, the other says nothing has measured whether it asks. A single list
// would put both under one word, which is the exact conflation §4a.1 forbids.
type PendingAck struct {
	// Unasked names the addressed seats that write without asking, in seating
	// order.
	Unasked []model.VendorID
	// Unmeasured names the addressed seats that can be asked where nothing has
	// measured that they do, in seating order.
	Unmeasured []model.VendorID
	// Rest reports that `n` has somewhere to send the turn: at least one
	// addressed seat is not on this card, AND this turn is one a seat can be
	// dropped from at all.
	//
	// Stored rather than derived, because the renderer cannot see the route or
	// the dispatch. It decides one word on the keys line, and that word must be
	// true: `n drop them` over a turn that is about to be cancelled instead
	// would be §7.8's surprise on the one line that guards a write.
	//
	// The second half is what makes a race and a `/flow` stage different from
	// an ordinary brief, and both rules are the room's own. A race is all or
	// nothing (§9.37): every attempt runs from one commit in its own worktree,
	// and a racer dropped after its tree was cut would leave an `arena/<n>/<seat>`
	// branch with no attempt on it, which `/arena record` would then count as a
	// seat that raced and lost. A stage runs whole for the fan's own reason: a
	// stage that ran two of its three hops is not the stage the operator typed,
	// and the join would wait for a hop nothing dispatched. So on those two, `n`
	// cancels.
	Rest bool
}

// Count is how many seats the card names.
func (a *PendingAck) Count() int {
	if a == nil {
		return 0
	}
	return len(a.Unasked) + len(a.Unmeasured)
}

// Named is every seat on the card, unasked first, in seating order within each
// class. It is the set `n` drops.
func (a *PendingAck) Named() []model.VendorID {
	if a == nil {
		return nil
	}
	out := make([]model.VendorID, 0, a.Count())
	out = append(out, a.Unasked...)
	return append(out, a.Unmeasured...)
}

// ackUnaskedWords and ackUnmeasuredWords are the two claims, in the words the
// postures page already speaks. One word for one meaning: `unasked` and
// `unmeasured` are seatShape's own words, so a reader who has read a badge has
// already learned this card's vocabulary.
const (
	ackUnaskedWords    = "write unasked"
	ackUnmeasuredWords = "asking unmeasured"
)

// ackRows is how many rows the card spends. Two, or none.
//
// A constant rather than a measurement, and the card SHEDS rather than wraps
// (ackSubject), for roomFacts' own reason: a height that depended on the width
// or on the glyph set would make the room's geometry move under `--ascii`.
const ackRows = 2

// ackWants reports that this State has a card to draw.
func ackWants(st State) bool { return st.Ack.Count() > 0 }

// ackLabel is a seat's name on the card, from the room's own columns.
func ackLabel(st State, v model.VendorID) string {
	for _, c := range st.Columns {
		if c.Vendor == v {
			return c.Label
		}
	}
	return string(v)
}

// ackClause is one claim and the seats it names: `write unasked: Antigravity,
// Cursor`.
func ackClause(words string, names []string) string {
	return words + ": " + strings.Join(names, ", ")
}

// ackSubject is the card's question at width w.
//
// **The ladder is longest-first, widest-that-fits-wins**, which is
// needsYouLine's idiom and stripHeader's before it, so a reader of this
// package meets one shedding shape rather than another new one. Its three
// rungs are:
//
//  1. every seat, by name.
//  2. every seat, by the two-letter tag §9.25 made permanent.
//  3. the claims alone, with no seat named.
//
// **The floor keeps the words and loses the names**, exactly as the needs-you
// strip's floor does. `4 seats write unasked` is still true, still says what
// the operator is being asked, and is honest about being unable to say who at
// that width. Dropping the card instead would trade the only statement that a
// write brief is reaching an unwatched seat for a handful of cells.
//
// Nothing is ever clipped at rungs 1 and 2. A clipped seat name is not a
// shortened seat name (§9.18), so the rung yields whole and the next one is
// tried.
func ackSubject(st State, w int, g Glyphs) string {
	a := st.Ack
	n := a.Count()
	head := itoa(n) + " " + plural(n, "seat")
	sep := strings.Repeat(" ", gutter) + g.Sep + strings.Repeat(" ", gutter)

	build := func(name func(model.VendorID) string) string {
		var clauses []string
		if len(a.Unasked) > 0 {
			var names []string
			for _, v := range a.Unasked {
				names = append(names, name(v))
			}
			clauses = append(clauses, ackClause(ackUnaskedWords, names))
		}
		if len(a.Unmeasured) > 0 {
			var names []string
			for _, v := range a.Unmeasured {
				names = append(names, name(v))
			}
			clauses = append(clauses, ackClause(ackUnmeasuredWords, names))
		}
		return head + " " + strings.Join(clauses, sep)
	}

	byLabel := build(func(v model.VendorID) string { return ackLabel(st, v) })
	if lipgloss.Width(byLabel) <= w {
		return byLabel
	}
	byTag := build(func(v model.VendorID) string { return vendorTag(v) })
	if lipgloss.Width(byTag) <= w {
		return byTag
	}
	var words []string
	if len(a.Unasked) > 0 {
		words = append(words, ackUnaskedWords)
	}
	if len(a.Unmeasured) > 0 {
		words = append(words, ackUnmeasuredWords)
	}
	bare := head + " " + strings.Join(words, sep)
	if lipgloss.Width(bare) <= w {
		return bare
	}
	return truncate(bare, w, g.Ellipsis)
}

// ackKeys is the keys line, and its middle cell states the truth about `n`.
//
// `n` drops the seats the card names and sends the turn to the rest. When the
// card names every addressed seat there IS no rest, so the key cancels the
// turn and the label says cancel. A key labelled `drop them` that cancelled a
// turn instead would be §7.8's surprise on the one line that guards a write.
func ackKeys(st State) string {
	return "y send" + needsYouGap + "n " + ackDropLabel(st) +
		needsYouGap + "a send, stop asking"
}

// ackDropLabel is what `n` does, in one phrase, for the card and the footer at
// once. One literal, so the two can never disagree about a key (§9.30).
func ackDropLabel(st State) string {
	if st.Ack.Rest {
		return "drop them"
	}
	return "cancel the turn"
}

// ackCardLines is the card: the question, then the keys.
//
// Two lines, always, so the layout's row budget and the paint cannot disagree
// (ackRows). The grammar is gateCardLines': a title at weight behind the
// warning mark, and the keys indented under it, in the same two cells the gate
// card's keys use. The mark and the styles are a SECOND signal only. Under
// PlainStyles and the ASCII glyph set the two lines say the same sentence,
// which is the property every distinction in this room has to have.
func ackCardLines(st State, w int, sty Styles, g Glyphs) []string {
	if !ackWants(st) || w < 1 {
		return nil
	}
	lead := g.Warn + " "
	subject := ackSubject(st, maxInt(1, w-lipgloss.Width(lead)), g)
	return []string{
		sty.Alert.Render(lead + subject),
		sty.Identity.Render("  " + truncate(ackKeys(st), maxInt(1, w-2), g.Ellipsis)),
	}
}

// ackTurn is the dispatch the card is holding.
//
// It carries exactly what sendTurn was called with, so releasing the card is
// re-entering the same function with the same arguments rather than rebuilding
// a dispatch from State. A rebuild is where the two would drift: the fanned
// prompts and a race's finished worktree setup are both consumed on the way
// in, and neither can be recovered from the room afterwards.
type ackTurn struct {
	route  Route
	prompt string
	race   *arenaSetupResult
	// fan is the per-seat task map a fanned flow stage handed in (§9.55),
	// given back to the room while the card is up so the released dispatch
	// reads it exactly as the held one would have.
	fan map[model.VendorID]string
	// whole reports that this turn cannot lose a seat: a race, or a `/flow`
	// stage. PendingAck.Rest states the consequence and says why.
	whole bool
	// chain reports that a `/flow` chain is behind this turn, so a refusal
	// retires the chain rather than leaving a hop marked running with nothing
	// dispatched (flowWriteGateKey's own rule, §9.35).
	chain bool
	// named is every seat the card names, so `n` drops exactly those and not
	// whatever the classes would say a keystroke later.
	named []model.VendorID
}

// ackFor is the card this dispatch has to raise, or nil.
//
// Four conditions keep it nil, and each is a rule rather than an optimisation:
//
//   - a READ room writes nothing, so there is nothing to acknowledge;
//   - a room that has stopped asking has already answered this question. That
//     covers `--auto`, which seeds GateOff at the door, and it covers a room
//     where `a` was pressed and remembered for the session. One flag, both
//     cases, and the footer's `a not asking` cell is already the way back;
//   - a `/flow` hop with no `write:` target runs at read posture whatever the
//     room's is (flowReadHop), so it writes nothing either;
//   - a room with no seat to name.
//
// The seats are the ones this dispatch will actually reach: seated, addressed,
// idle, and holding an adapter. A seat that is answering an earlier brief is
// not on this turn, and a seat with no adapter never spawns; naming either
// would ask the operator to acknowledge a write that is not going to happen.
func (m *Model) ackFor(route Route, reg map[model.VendorID]vendors.Vendor, whole bool) *PendingAck {
	if !m.st.Write || !m.st.Asking() || m.flowReadHop {
		return nil
	}
	a := &PendingAck{}
	for _, c := range m.st.Columns {
		if !m.st.seats(c) || !route.addresses(c.Vendor) || m.turnOf(c.Vendor) != nil {
			continue
		}
		if _, ok := reg[c.Vendor]; !ok {
			continue
		}
		switch ackClassFor(c.Vendor, m.st.Asking(), m.fellBack[c.Vendor]) {
		case ackUnasked:
			a.Unasked = append(a.Unasked, c.Vendor)
		case ackUnmeasured:
			a.Unmeasured = append(a.Unmeasured, c.Vendor)
		default:
			// Gated, and therefore the rest `n` sends to, unless this turn is
			// one no seat may be dropped from (PendingAck.Rest).
			a.Rest = !whole
		}
	}
	if a.Count() == 0 {
		return nil
	}
	return a
}

// ackRest is the seats `n` sends the turn to: addressed, seated, idle, and not
// on the card.
//
// Recomputed at the keystroke rather than stored beside the card, because the
// room stays live while the card is up: a neighbour seat can land, and a seat
// can retreat to its batch adapter, between the raise and the answer. The
// stored half is what the card SAID, which is what `n` drops; who is left is a
// fact about now.
func (m *Model) ackRest(t *ackTurn) []model.VendorID {
	if t == nil {
		return nil
	}
	dropped := make(map[model.VendorID]bool, len(t.named))
	for _, v := range t.named {
		dropped[v] = true
	}
	var out []model.VendorID
	for _, c := range m.st.Columns {
		if dropped[c.Vendor] || !m.st.seats(c) || !t.route.addresses(c.Vendor) {
			continue
		}
		if m.turnOf(c.Vendor) != nil {
			continue
		}
		out = append(out, c.Vendor)
	}
	return out
}

// clearAck takes the card down and forgets what it held.
func (m *Model) clearAck() *ackTurn {
	t := m.ackTurn
	m.st.Ack, m.ackTurn = nil, nil
	return t
}

// ackKey answers the card.
//
// **Only four keys, and everything else is answered rather than passed on.**
// The gate's keymap falls through to viewKey so a reader can scroll a column
// before deciding, and that trade is wrong here: the operator was TYPING when
// this card went up, the draft is still in the composer, and a fall-through
// would let `q` quit the room and take the brief with it. Nothing in the
// columns changes this decision either, because the question is about the
// seats the card already names.
//
// **ctrl+c keeps meaning what it always means, one step at a time.** It takes
// the card down and keeps the draft, which is stopArenaSetup's own shape: this
// keystroke is the operator's way out of a state the room put them in, and
// once they are out, a second ctrl+c means what it meant before.
func (m *Model) ackKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.ackTurn == nil {
		// A card with no held turn behind it. The one way to reach this is a
		// REPLAY, whose card is drawn from the file and answered by it
		// (replayAck); replayKey takes y, n and a before this and says so, and
		// this is the guard that keeps the remaining keys from reaching for a
		// dispatch that does not exist.
		m.st.Notice = ackHeldNotice(m.st)
		return m, nil
	}
	switch msg.String() {
	case "y":
		// Recorded before it is answered, so a --record file holds the card as
		// the operator saw it and the decision that followed (recording.go).
		// Same on the two keys below.
		m.recordAckDecision(ackDecisionSend)
		t := m.clearAck()
		m.ackArmed = true
		return m, m.releaseAck(t, t.route, "")
	case "n":
		m.recordAckDecision(ackDecisionDrop)
		t := m.clearAck()
		rest := m.ackRest(t)
		if t.whole {
			m.cancelAck(t, "the turn was cancelled: no seat may be dropped from it")
			return m, nil
		}
		if len(rest) == 0 {
			m.cancelAck(t, "the turn was cancelled: every seat it addressed writes with no card")
			return m, nil
		}
		return m, m.releaseAck(t, Route{Vendors: rest},
			"dropped from this turn: "+ackNames(m.st, t.named))
	case "a":
		m.recordAckDecision(ackDecisionSendStopAsking)
		t := m.clearAck()
		// The room's own control, reused whole (program.go). It drains the
		// gate queue, re-stamps every badge and sets GateOff, which is the
		// flag ackFor reads: from here this card behaves exactly as it does
		// under --auto, and `a` in view mode is the way back. Run BEFORE the
		// dispatch so the seats spawn at the posture the operator just chose.
		m.stopAsking()
		m.ackArmed = true
		return m, m.releaseAck(t, t.route,
			"sent, and nothing will ask again this session · a starts asking")
	case "ctrl+c":
		m.cancelAck(m.clearAck(), "the brief was not sent")
		return m, nil
	}
	m.st.Notice = ackHeldNotice(m.st)
	return m, nil
}

// ackHeldNotice answers a key this card does not take, in the three keys it
// does. A dead key must say why it did nothing (§9.12), and the answer here is
// the whole keymap, because the keymap is three keys long.
func ackHeldNotice(st State) string {
	return "the room is holding this brief: y sends, n " + ackDropLabel(st) +
		", a sends and stops asking"
}

// cancelAck gives the room back with nothing dispatched, and says what is left
// behind.
//
// Three things happen, and each one is an existing rule rather than a new one:
//
//   - A `/flow` chain is RETIRED. launchFlowStage marked the stage running
//     before it reached the dispatch, so a chain left standing would carry a
//     hop marker over a hop nothing sent, and its join would wait forever. That
//     is flowWriteGateKey's own answer to a refused write hop (§9.35): the
//     whole chain goes, because a chain whose write was refused has nothing
//     legal to do next.
//   - A race's WORKTREES are kept and the notice says so, which is
//     stopArenaSetup's sentence exactly. The trees are the operator's, at names
//     they can read, and `git worktree remove` clears one.
//   - The BRIEF goes back in the composer. An ordinary turn never lost it; a
//     race cleared it at enter, several seconds and one worktree setup ago, so
//     it is put back the way stopArenaSetup puts it back.
func (m *Model) cancelAck(t *ackTurn, lead string) {
	if t == nil {
		return
	}
	var why string
	switch {
	case t.race != nil:
		why = "no racer started, and any worktree already added is kept · git worktree remove clears one"
		if m.st.Draft == "" {
			m.setDraft("/arena " + t.prompt)
		}
	case t.chain:
		m.endFlowChain()
		why = "the chain is retired: a stage that ran some of its hops is not the stage you typed"
	default:
		why = "the brief is still in the composer"
	}
	m.st.Notice = lead + " · " + why
	m.st.Mode = ModeComposing
}

// releaseAck sends the held brief on the route the keystroke chose, and says
// what the keystroke did once the dispatch has had its say.
//
// The notice is set AFTER sendTurn and only into an empty slot. sendTurn
// clears the notice on a successful dispatch and writes its own on a partial
// one, for a seat that went busy while the card was up. That sentence is
// about the turn the operator just sent, so it outranks a report of the
// keystroke that sent it.
//
// A notice the room was ALREADY showing survives into that empty slot too, and
// it survives because of what a notice under this card is: the room is stopped
// and nothing else has happened, so whatever it says is about this brief.
// `@auto` is the case that made this a rule rather than a nicety. It names the
// seat its readings picked, after the routing cell that made the pick has gone
// (dispatchAuto), and a dispatch that cleared it would leave the operator
// holding a turn with nothing on screen saying who it went to.
func (m *Model) releaseAck(t *ackTurn, route Route, notice string) tea.Cmd {
	if t == nil {
		return nil
	}
	held := m.st.Notice
	m.fanPrompts = t.fan
	cmd := m.sendTurn(route, t.prompt, t.race)
	if m.st.Notice == "" {
		if notice != "" {
			m.st.Notice = notice
		} else {
			m.st.Notice = held
		}
	}
	return cmd
}

// ackNames is a list of seats in the labels the room draws them under.
func ackNames(st State, vs []model.VendorID) string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, ackLabel(st, v))
	}
	return strings.Join(out, ", ")
}
