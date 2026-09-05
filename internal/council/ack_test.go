package council

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The write acknowledgement card (ack.go, LEDGER.md 2026-09-04).
//
// Every behavioural test here asserts an OBSERVABLE, on flow_security_test.go's
// rule: how many processes were spawned, which seats the card named, what the
// frame says. None of them asserts that a helper returned a value, because the
// defect this card exists to prevent is a room that SAYS a seat is unwatched
// and drives it anyway, and a test that read the flag would pass over exactly
// that.

// answerAck presses `y` on the card when one is up.
//
// It is what an operator does now, which is why it lives beside `send` and
// `raceNow` rather than inside the tests that use it: a write brief in a
// writing room stops on this card, so "type it and press enter" is two
// keystrokes and the helpers say so. A room with no card up is untouched, so a
// refused brief and a read room still take the helper unchanged.
func answerAck(m *Model) {
	if ackWants(m.st) {
		m.key(key("y"))
	}
}

// sendHeld types a brief and presses enter, and STOPS there.
//
// It is `send` minus the acknowledgement, because these tests are about the
// card itself: `send` answers it, which is right for a test about something
// else and would delete the subject here.
func sendHeld(t *testing.T, m *Model, brief string) {
	t.Helper()
	m.st.Mode = ModeComposing
	m.setDraft(brief)
	m.key(key("enter"))
}

// ackRoom is a four-seat writing room with the card's three classes in it:
// claude gated, codex asking-unmeasured, antigravity and cursor unasked.
//
// The seats carry the labels the room draws them under, and the workspace is a
// fixed string rather than the temporary directory flowRoom hands out, because
// two of these tests take a golden: a frame carrying a path with a run counter
// in it is a golden that fails on its second run.
func ackRoom(t *testing.T) *Model {
	t.Helper()
	m := flowRoom(t, true)
	m.st.Width, m.st.Height = 120, 24
	m.st.Mode = ModeComposing
	m.st.Workspace = "/home/dev/code/telltale"
	m.st.Home = "/home/dev"
	for i := range m.st.Columns {
		m.st.Columns[i].Label = ackLabels[m.st.Columns[i].Vendor]
	}
	return m
}

// ackLabels is each seat's own name, as detect.go gives it.
var ackLabels = map[model.VendorID]string{
	model.VendorClaude:      "Claude Code",
	model.VendorCodex:       "Codex",
	model.VendorAntigravity: "Antigravity",
	model.VendorCursor:      "Cursor",
	model.VendorGrok:        "Grok",
}

// TestAckReadsEverySeatShape pins the parse ack.go makes of seatShape, for
// every vendor and both fallback states.
//
// It exists because the classification is READ out of the badge's own string
// rather than stated twice, and a shape that stopped carrying three
// dot-separated words would silently classify every seat as unasked. That
// failure is invisible on screen: the card would still be raised, still name
// the seats, and quietly move one of them out of the class that says nothing
// has measured it.
//
// The table is also the zero-versus-absent property in one place. Grok was
// MEASURED not asking on 2026-09-04, so it is `write unasked`; codex CAN be
// asked and nothing has measured it, so it is `asking unmeasured`. The two must
// not swap, and a seat with no shape at all falls to unasked rather than to
// unmeasured, because "nothing to read" is not evidence of an absent
// measurement.
func TestAckReadsEverySeatShape(t *testing.T) {
	for _, tc := range []struct {
		name     string
		vendor   model.VendorID
		fellBack bool
		gated    bool
		want     ackClass
	}{
		{"claude, asking", model.VendorClaude, false, true, ackGated},
		{"claude, not asking", model.VendorClaude, false, false, ackUnasked},
		{"codex live can be asked and nobody has watched it", model.VendorCodex, false, true, ackUnmeasured},
		{"codex fallen back asks about nothing", model.VendorCodex, true, true, ackUnasked},
		{"grok was measured not asking", model.VendorGrok, false, true, ackUnasked},
		{"grok fallen back", model.VendorGrok, true, true, ackUnasked},
		{"antigravity asks about nothing", model.VendorAntigravity, false, true, ackUnasked},
		{"antigravity fallen back", model.VendorAntigravity, true, true, ackUnasked},
		{"cursor has no shape and does not ask about edits", model.VendorCursor, false, true, ackUnasked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ackClassFor(tc.vendor, tc.gated, tc.fellBack); got != tc.want {
				t.Errorf("ackClassFor(%s, gated=%v, fellBack=%v) = %d, want %d",
					tc.vendor, tc.gated, tc.fellBack, got, tc.want)
			}
		})
	}
	// The parse itself, so a shape that lost its middle word fails here rather
	// than at a class that looks plausible.
	for _, v := range []model.VendorID{model.VendorCodex, model.VendorGrok, model.VendorAntigravity} {
		for _, back := range []bool{false, true} {
			switch got := ackAskingWord(v, back); got {
			case "asks", "unasked":
			default:
				t.Errorf("seatShape(%s, fellBack=%v) has no asking word: %q", v, back, got)
			}
		}
	}
}

// TestAWriteBriefStopsOnACardThatNamesEverySeatTheRoomCannotMakeAsk is the
// ruling: nothing spawns, and the card names each seat under its own class.
func TestAWriteBriefStopsOnACardThatNamesEverySeatTheRoomCannotMakeAsk(t *testing.T) {
	log := countSpawns(t)
	m := ackRoom(t)

	sendHeld(t, m, "@all add a marker file")

	if log.n() != 0 {
		t.Fatalf("%d process(es) spawned before the operator acknowledged the seats: %+v", log.n(), log.specs)
	}
	if m.st.Ack == nil {
		t.Fatal("no card is up, so nothing held the brief back")
	}
	if m.anyInFlight() {
		t.Error("a turn is in flight for a write brief nobody acknowledged")
	}
	if got := vendorList(m.st.Ack.Unasked); got != "agy, cursor" {
		t.Errorf("unasked = %q, want the two seats that write with no card", got)
	}
	if got := vendorList(m.st.Ack.Unmeasured); got != "codex" {
		t.Errorf("unmeasured = %q, want the seat that can be asked and has not been watched", got)
	}
	if !m.st.Ack.Rest {
		t.Error("claude is gated and addressed, so n has somewhere to send the turn")
	}
	// And y is what releases it, otherwise this test would pass on a room that
	// never dispatches at all.
	m.key(key("y"))
	if m.st.Ack != nil {
		t.Error("y left the card up")
	}
	if log.n() != 4 {
		t.Fatalf("after y: %d spawns, want the four seated columns", log.n())
	}
}

// TestTheGatedSeatAloneRaisesNoCard is the complement, and it is the whole of
// the rule stated the other way: a room whose only addressed seat asks before
// every change is a room the operator has already agreed to.
func TestTheGatedSeatAloneRaisesNoCard(t *testing.T) {
	log := countSpawns(t)
	m := ackRoom(t)

	send(t, m, "@claude add a marker file")

	if m.st.Ack != nil {
		t.Fatalf("the gated seat raised a card: %+v", m.st.Ack)
	}
	if log.n() != 1 {
		t.Fatalf("%d spawns, want the one gated seat dispatched straight through", log.n())
	}
}

// TestAnAutoRoomAndAReadRoomNeverRaiseTheCard.
//
// Two rooms, one flag. `--auto` seeds GateOff at the door and has answered the
// question already; a --read room writes nothing, so there is nothing to
// acknowledge. Each is asserted through the SPAWN as well as through the card,
// because "no card" would also be true of a room that refused the brief.
func TestAnAutoRoomAndAReadRoomNeverRaiseTheCard(t *testing.T) {
	t.Run("auto", func(t *testing.T) {
		log := countSpawns(t)
		m := ackRoom(t)
		m.st.GateOff = true

		send(t, m, "@all add a marker file")

		if m.st.Ack != nil {
			t.Fatalf("a room that has stopped asking raised a card: %+v", m.st.Ack)
		}
		if log.n() != 4 {
			t.Fatalf("%d spawns, want every seat dispatched straight through", log.n())
		}
	})
	t.Run("read", func(t *testing.T) {
		log := countSpawns(t)
		m := ackRoom(t)
		m.st.Write = false

		send(t, m, "@all what do you think?")

		if m.st.Ack != nil {
			t.Fatalf("a read-only room raised a write acknowledgement: %+v", m.st.Ack)
		}
		if log.n() != 4 {
			t.Fatalf("%d spawns, want every seat dispatched straight through", log.n())
		}
	})
}

// TestNDropsTheNamedSeatsAndSendsToTheRest: one seat is left, so the turn goes
// to it and the room says who was dropped.
func TestNDropsTheNamedSeatsAndSendsToTheRest(t *testing.T) {
	log := countSpawns(t)
	m := ackRoom(t)

	sendHeld(t, m, "@all add a marker file")
	if m.st.Ack == nil {
		t.Fatal("no card is up")
	}
	m.key(key("n"))

	if m.st.Ack != nil {
		t.Error("n left the card up")
	}
	if log.n() != 1 {
		t.Fatalf("%d spawns, want the gated seat alone", log.n())
	}
	if log.specs[0].Vendor != model.VendorClaude {
		t.Errorf("the turn went to %s, want the one seat the card did not name", log.specs[0].Vendor)
	}
	for _, v := range []model.VendorID{model.VendorCodex, model.VendorAntigravity, model.VendorCursor} {
		if m.turnOf(v) != nil {
			t.Errorf("%s took the turn after n dropped it", v)
		}
	}
	for _, want := range []string{"dropped from this turn", "Codex", "Antigravity", "Cursor"} {
		if !strings.Contains(m.st.Notice, want) {
			t.Errorf("the notice does not say %q: %q", want, m.st.Notice)
		}
	}
}

// TestNWithNoSeatLeftCancelsTheTurnAndSaysSo: the card names every addressed
// seat, so `n` has nowhere to send the turn. It cancels rather than dispatching
// to nobody, the draft stays where it was typed, and the key on the card said
// cancel before it was pressed.
func TestNWithNoSeatLeftCancelsTheTurnAndSaysSo(t *testing.T) {
	log := countSpawns(t)
	m := ackRoom(t)

	sendHeld(t, m, "@codex @agy add a marker file")
	if m.st.Ack == nil {
		t.Fatal("no card is up")
	}
	if m.st.Ack.Rest {
		t.Fatal("the card thinks a seat is left; this room addressed only seats it names")
	}
	if got := ackDropLabel(m.st); got != "cancel the turn" {
		t.Errorf("the key reads %q over a turn it is about to cancel", got)
	}
	if !strings.Contains(render(m.st), "n cancel the turn") {
		t.Errorf("the card does not say what n does:\n%s", render(m.st))
	}

	m.key(key("n"))

	if log.n() != 0 {
		t.Fatalf("%d spawns after the turn was cancelled", log.n())
	}
	if m.anyInFlight() {
		t.Error("a turn is in flight after n cancelled it")
	}
	if !strings.Contains(m.st.Notice, "cancelled") {
		t.Errorf("the room did not say the turn was cancelled: %q", m.st.Notice)
	}
	if m.st.Draft != "@codex @agy add a marker file" {
		t.Errorf("the cancelled brief left the composer: %q", m.st.Draft)
	}
}

// TestARemembersAndTheNextBriefGoesStraightThrough is `a`, and both halves of
// it: this turn is sent, and the room stops asking for the rest of the session
// through the SAME flag `--auto` seeds and the footer's `a not asking` cell
// reverses.
func TestARemembersAndTheNextBriefGoesStraightThrough(t *testing.T) {
	log := countSpawns(t)
	m := ackRoom(t)

	sendHeld(t, m, "@all add a marker file")
	m.key(key("a"))

	if m.st.Ack != nil {
		t.Error("a left the card up")
	}
	if log.n() != 4 {
		t.Fatalf("after a: %d spawns, want the turn sent as addressed", log.n())
	}
	if m.st.Asking() {
		t.Error("a did not stop the room asking")
	}
	if !strings.Contains(m.st.Notice, "nothing will ask again this session") {
		t.Errorf("the room did not say what a did: %q", m.st.Notice)
	}

	// The rest of the room. Every seat is retired first, so the second brief
	// reaches idle seats.
	for _, c := range m.st.Columns {
		m.turnColumnFinished(c.Vendor)
	}
	sendHeld(t, m, "@all and another")
	if m.st.Ack != nil {
		t.Fatalf("the card came back in a room that had stopped asking: %+v", m.st.Ack)
	}
	// Six, not eight. The second brief spawns only the two seats this room
	// drives one process per turn (flowRoom pins the batch registry); claude
	// and cursor keep their live processes and take the turn on the stdin they
	// already have.
	if log.n() != 6 {
		t.Fatalf("%d spawns, want the second brief sent with no card", log.n())
	}

	// And the way back is the one that already existed.
	m.st.Mode = ModeViewing
	m.key(key("a"))
	if !m.st.Asking() {
		t.Error("a in view mode did not turn the asking back on")
	}
}

// vendorList is a card's seats as a comparable string.
func vendorList(vs []model.VendorID) string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, string(v))
	}
	return strings.Join(out, ", ")
}

// TestTheRecordingCarriesTheCardAndItsAnswer: two `ack` lines per card, the
// seats then the decision, and the dispatch after them.
func TestTheRecordingCarriesTheCardAndItsAnswer(t *testing.T) {
	countSpawns(t)
	path := filepath.Join(t.TempDir(), "run.jsonl")
	rec, err := openRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	m := ackRoom(t)
	m.rec = rec
	rec.room(m.st)

	sendHeld(t, m, "@all add a marker file")
	m.key(key("y"))
	if err := rec.close(); err != nil {
		t.Fatal(err)
	}

	got, err := readRecording(path)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, l := range got.lines {
		kinds = append(kinds, l.Kind)
	}
	if strings.Join(kinds, " ") != "ack ack dispatch" {
		t.Fatalf("records = %v, want the card, its answer, then the dispatch", kinds)
	}
	raised, decided := got.lines[0], got.lines[1]
	if strings.Join(raised.Unasked, ",") != "agy,cursor" || strings.Join(raised.Unmeasured, ",") != "codex" {
		t.Errorf("the raised card = %+v", raised)
	}
	if !raised.Rest {
		t.Error("the raised card did not record that a seat was left for n")
	}
	if decided.Decision != ackDecisionSend {
		t.Errorf("decision = %q, want %q", decided.Decision, ackDecisionSend)
	}
	// The file itself, because the field names are the format.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"kind":"ack"`, `"unasked":["agy","cursor"]`, `"unmeasured":["codex"]`, `"decision":"send"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the file does not carry %s:\n%s", want, body)
		}
	}
	// replay-check reads it back for a review.
	var out strings.Builder
	if err := ReplayCheck(path, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"write acknowledgement cards", "agy, cursor, codex", "send"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("replay-check does not report %q:\n%s", want, out.String())
		}
	}
}

// ackRecording is a hand-written file with one card in it: the room, the card
// raised, the card answered, and the dispatch that followed.
func ackRecording(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ack.jsonl")
	lines := []string{
		`{"kind":"room","v":1,"started":"2026-09-04T10:00:00Z","workspace":"~/ws","write":true,` +
			`"seats":[{"vendor":"claude","label":"Claude Code","avail":0,"sandbox":6,"detail":"gated","gran":1},` +
			`{"vendor":"codex","label":"Codex","avail":0,"sandbox":5,"detail":"writes","gran":2},` +
			`{"vendor":"agy","label":"Antigravity","avail":0,"sandbox":5,"detail":"writes","gran":2}]}`,
		`{"kind":"ack","ms":1000,"unasked":["agy"],"unmeasured":["codex"],"rest":true}`,
		`{"kind":"ack","ms":4000,"decision":"send"}`,
		`{"kind":"dispatch","ms":4100,"turn":1,"route":{},"sent":[{"vendor":"codex","prompt":"add a marker file"}]}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestAReplayDrawsTheCardAndTakesItDown: the two records are two moments, so
// the replay shows the question for as long as the operator looked at it.
func TestAReplayDrawsTheCardAndTakesItDown(t *testing.T) {
	countSpawns(t)
	path := ackRecording(t)
	rec, err := readRecording(path)
	if err != nil {
		t.Fatal(err)
	}
	m := newReplayModel(Options{}, rec, path)
	m.st.Width, m.st.Height = 120, 24

	play(m, 0, 1)
	if m.st.Ack == nil {
		t.Fatal("the ack record did not raise a card")
	}
	frame := render(m.st)
	for _, want := range []string{"2 seats", "write unasked: Antigravity", "asking unmeasured: Codex", "y send"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the replayed frame does not say %q:\n%s", want, frame)
		}
	}
	// The replay answers its own card, and the keys say so rather than acting.
	m.key(key("y"))
	if m.st.Ack == nil {
		t.Error("y on a replay answered the recorded card")
	}
	if !strings.Contains(m.st.Notice, replayNotice) {
		t.Errorf("the refusal does not say the room is a replay: %q", m.st.Notice)
	}

	play(m, 1, 2)
	if m.st.Ack != nil {
		t.Error("the decision record did not take the card down")
	}
	if !strings.Contains(m.st.Notice, "send") {
		t.Errorf("the replay does not say how the card was answered: %q", m.st.Notice)
	}
	play(m, 2, 3)
	if m.st.Turn != 1 {
		t.Errorf("the dispatch after the card did not land: turn %d", m.st.Turn)
	}
}

// TestAnUnknownRecordKindIsSkippedAndCounted.
//
// The refusal that used to be here made the format one-way: a file a newer
// telltale wrote would not open at all in an older one, so every kind added
// after a release retired every binary before it. The count is what keeps the
// skip honest, and it is asserted on the surface a review reads.
func TestAnUnknownRecordKindIsSkippedAndCounted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.jsonl")
	lines := []string{
		`{"kind":"room","v":1,"started":"2026-09-04T10:00:00Z","workspace":"~/ws","write":true,` +
			`"seats":[{"vendor":"codex","label":"Codex","avail":0,"sandbox":5,"detail":"writes","gran":2}]}`,
		`{"kind":"somethingnewer","ms":10,"text":"a kind this build has never heard of"}`,
		`{"kind":"dispatch","ms":20,"turn":1,"route":{},"sent":[{"vendor":"codex","prompt":"go"}]}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec, err := readRecording(path)
	if err != nil {
		t.Fatalf("a file with one unknown kind in it was refused outright: %v", err)
	}
	if rec.unknown != 1 {
		t.Errorf("unknown = %d, want the one record this build skipped", rec.unknown)
	}
	if len(rec.lines) != 1 || rec.lines[0].Kind != "dispatch" {
		t.Errorf("the skipped record was applied: %+v", rec.lines)
	}
	var out strings.Builder
	if err := ReplayCheck(path, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "unknown records: 1") {
		t.Errorf("replay-check does not report the skip:\n%s", out.String())
	}
}

// TestAMalformedAckIsStillRefused. The skip above is for kinds this build does
// not know, never for a kind it does: an ack line naming no seat and carrying
// no decision is a file no recorder wrote.
func TestAMalformedAckIsStillRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
	}{
		{"neither seats nor a decision", `{"kind":"ack","ms":10}`},
		{"a decision nothing answers to", `{"kind":"ack","ms":10,"decision":"maybe"}`},
		{"a seat the room line does not seat", `{"kind":"ack","ms":10,"unasked":["grok"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bad.jsonl")
			lines := []string{
				`{"kind":"room","v":1,"started":"2026-09-04T10:00:00Z","workspace":"~/ws","write":true,` +
					`"seats":[{"vendor":"codex","label":"Codex","avail":0,"sandbox":5,"detail":"writes","gran":2}]}`,
				tc.line,
			}
			if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readRecording(path); err == nil {
				t.Error("a malformed ack line was accepted")
			}
		})
	}
}

// TestTheScrubKeepsAnAckWhole. A scrubbed recording keeps every structural
// fact and replaces every word; an ack line is structure end to end, so it
// comes out identical.
func TestTheScrubKeepsAnAckWhole(t *testing.T) {
	in := ackRecording(t)
	rec, err := readRecording(in)
	if err != nil {
		t.Fatal(err)
	}
	out := scrubRecording(rec)
	var acks []recordLine
	for _, l := range out {
		if l.Kind == "ack" {
			acks = append(acks, l)
		}
	}
	if len(acks) != 2 {
		t.Fatalf("%d ack lines survived the scrub, want 2", len(acks))
	}
	if strings.Join(acks[0].Unasked, ",") != "agy" || strings.Join(acks[0].Unmeasured, ",") != "codex" {
		t.Errorf("the scrub renamed the seats on the card: %+v", acks[0])
	}
	if !acks[0].Rest || acks[1].Decision != ackDecisionSend {
		t.Errorf("the scrub changed the card's answer: %+v %+v", acks[0], acks[1])
	}
}

// TestTheAckCardFrameInBothGlyphSets is the frame, pinned.
//
// Both glyph sets, because every distinction this room makes has to be carried
// by a word: the ASCII frame says the identical sentence with `!` for the
// warning mark and `|` between the two claims, and a golden is what proves that
// rather than a claim about it.
func TestTheAckCardFrameInBothGlyphSets(t *testing.T) {
	countSpawns(t)
	m := ackRoom(t)
	sendHeld(t, m, "@all add a marker file")
	if m.st.Ack == nil {
		t.Fatal("no card is up to draw")
	}
	golden(t, "ack-card", render(m.st))
	golden(t, "ack-card-ascii", Render(m.st, PlainStyles(), GlyphsFor(true)))

	// The floor: the card sheds to the tags and then to the claims alone, and
	// it never wraps, so the room's height is the same at every width.
	for _, w := range []int{120, 96, 80, 60} {
		st := m.st
		st.Width = w
		lay := layoutFor(st, GlyphsFor(false))
		if lay.Ack != ackRows {
			t.Errorf("at %d columns the card wants %d rows, want %d", w, lay.Ack, ackRows)
		}
		lines := ackCardLines(st, w-2*framePad, PlainStyles(), GlyphsFor(false))
		if len(lines) != ackRows {
			t.Fatalf("at %d columns the card drew %d lines", w, len(lines))
		}
		for _, l := range lines {
			if got := len([]rune(l)); got > w-2*framePad {
				t.Errorf("at %d columns a card line is %d cells wide: %q", w, got, l)
			}
		}
		if !strings.Contains(lines[0], "write unasked") || !strings.Contains(lines[0], "asking unmeasured") {
			t.Errorf("at %d columns the card lost a claim: %q", w, lines[0])
		}
	}
}

// TestTheFooterAndTheBorderSayTheRoomIsHoldingTheBrief.
//
// The mode line is the contract that names every key on every frame (§7.8), and
// this card gives y, n and a meanings they have nowhere else. The border's word
// is the other half: the draft is still in the composer, so a border reading
// COMPOSE would describe a thing the room has stopped doing.
func TestTheFooterAndTheBorderSayTheRoomIsHoldingTheBrief(t *testing.T) {
	countSpawns(t)
	m := ackRoom(t)
	sendHeld(t, m, "@all add a marker file")

	frame := render(m.st)
	for _, want := range []string{"HOLD", "y send", "n drop them", "a send, stop asking"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the frame does not say %q:\n%s", want, frame)
		}
	}
	if strings.Contains(frame, "COMPOSE") {
		t.Errorf("the border says COMPOSE over a room that will not take text:\n%s", frame)
	}
	// The 2026-09-03 ruling: NEEDS YOU means a vendor is stopped on a
	// keystroke, and no vendor has been started here.
	if strings.Contains(frame, "NEEDS YOU") {
		t.Errorf("the strip claims a blocked vendor over a room that has spawned nothing:\n%s", frame)
	}
	// An unrecognised key is answered rather than passed on. `q` would quit the
	// room and take the draft with it.
	m.key(key("q"))
	if m.st.Ack == nil {
		t.Error("an unrecognised key took the card down")
	}
	if !strings.Contains(m.st.Notice, "holding this brief") {
		t.Errorf("an unrecognised key was swallowed: %q", m.st.Notice)
	}
	// ctrl+c gives the room back and keeps the draft.
	m.key(key("ctrl+c"))
	if m.st.Ack != nil {
		t.Error("ctrl+c left the card up")
	}
	if m.st.Draft != "@all add a marker file" {
		t.Errorf("ctrl+c dropped the brief: %q", m.st.Draft)
	}
}

// TestAFlowReadHopRaisesNoCard: a hop with no `write:` target runs at read
// posture whatever the room's is, so there is nothing to acknowledge. The
// write hop beside it is the control.
func TestAFlowReadHopRaisesNoCard(t *testing.T) {
	t.Run("read hop", func(t *testing.T) {
		log := countSpawns(t)
		m := flowRoom(t, true)
		m.st.Draft = "/flow @codex read the poller -> @claude review it"
		m.dispatch()
		if m.st.Ack != nil {
			t.Fatalf("a read hop raised a write acknowledgement: %+v", m.st.Ack)
		}
		if log.n() != 1 {
			t.Fatalf("%d spawns, want the read hop dispatched straight through", log.n())
		}
	})
	t.Run("write hop", func(t *testing.T) {
		log := countSpawns(t)
		m := flowRoom(t, true)
		m.st.Draft = "/flow @codex publish write:docs/out.md -> @claude review it"
		m.dispatch()
		m.key(key("y")) // the flow write gate
		if m.st.Ack == nil {
			t.Fatal("a write hop reached the seat with no acknowledgement")
		}
		if log.n() != 0 {
			t.Fatalf("%d spawns before the card was answered", log.n())
		}
		m.key(key("y"))
		if log.n() != 1 {
			t.Fatalf("after y: %d spawns, want the hop dispatched", log.n())
		}
	})
}

// TestAFannedStageKeepsItsPerSeatTasksAcrossTheCard.
//
// The prompts a fanned stage hands each seat are consumed on the way into
// sendTurn, and the card returns a keystroke later. A release that read an
// empty map would send every seat the first hop's brief instead of its own,
// which is a real brief to a real vendor about the wrong task.
func TestAFannedStageKeepsItsPerSeatTasksAcrossTheCard(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, true)
	m.st.Draft = "/flow @codex refactor the poller write:a.go & @agy write the docs write:b.md -> @claude review both"
	m.dispatch()
	m.key(key("y")) // the flow write gate
	if m.st.Ack == nil {
		t.Fatal("the fanned write stage reached its seats with no acknowledgement")
	}
	m.key(key("y"))
	if log.n() != 2 {
		t.Fatalf("%d spawns, want both hops of the stage", log.n())
	}
	// The parser keeps each hop's verb apart from its task, so what a seat is
	// handed is the task alone. What matters here is that the two differ and
	// that each landed on the seat its own hop named.
	if strings.Contains(hopPrompt(t, log, 1), "poller") {
		t.Error("the second seat was handed the first hop's task")
	}
	for i, want := range map[int]string{0: "the poller", 1: "the docs"} {
		if got := hopPrompt(t, log, i); !strings.Contains(got, want) {
			t.Errorf("spawn %d was not handed its own task (%q):\n%s", i, want, got)
		}
	}
}

// TestARaceStopsOnTheCardBeforeASeatSpawns. A race gives every seat write
// posture in its own worktree, and the worktree is the containment; the card is
// the acknowledgement, and it is raised after the trees are cut and before any
// racer starts.
func TestARaceStopsOnTheCardBeforeASeatSpawns(t *testing.T) {
	log := countSpawns(t)
	m := arenaRoom(t)
	m.st.Draft = "/arena add a marker file"

	pumpArenaSetup(t, m, m.dispatch())
	if m.st.Ack == nil {
		t.Fatal("the race reached its seats with no acknowledgement")
	}
	if log.n() != 0 {
		t.Fatalf("%d racer(s) spawned before the operator acknowledged them", log.n())
	}
	m.key(key("y"))
	if !m.anyInFlight() {
		t.Fatal("y did not release the race")
	}
	if m.race() == nil {
		t.Error("the turn that started is not a race")
	}
}

// TestTheCardReadsTheFallbackRatherThanTheLiveShape. A seat that retreated to
// its batch adapter asks about nothing, and it was MEASURED asking about
// nothing, so it moves out of `asking unmeasured` and into `write unasked`.
func TestTheCardReadsTheFallbackRatherThanTheLiveShape(t *testing.T) {
	countSpawns(t)
	m := ackRoom(t)
	if m.fellBack == nil {
		m.fellBack = map[model.VendorID]bool{}
	}
	m.fellBack[model.VendorCodex] = true

	sendHeld(t, m, "@codex add a marker file")
	if m.st.Ack == nil {
		t.Fatal("no card is up")
	}
	if got := vendorList(m.st.Ack.Unasked); got != "codex" {
		t.Errorf("unasked = %q, want the fallen-back seat", got)
	}
	if len(m.st.Ack.Unmeasured) != 0 {
		t.Errorf("the fallen-back seat is still named unmeasured: %v", m.st.Ack.Unmeasured)
	}
}

// TestASeatWithNoAdapterIsNeverNamed. The card names the seats this dispatch
// will reach; a seat with no adapter never spawns, so naming it would ask the
// operator to acknowledge a write that is not going to happen.
func TestASeatWithNoAdapterIsNeverNamed(t *testing.T) {
	countSpawns(t)
	m := ackRoom(t)
	reg := map[model.VendorID]vendors.Vendor{}
	for k, v := range vendors.Registry() {
		if k == model.VendorAntigravity {
			continue
		}
		reg[k] = v
	}
	ack := m.ackFor(Route{}, reg)
	if ack == nil {
		t.Fatal("no card at all")
	}
	if got := vendorList(ack.Unasked); got != "cursor" {
		t.Errorf("unasked = %q, want the seat that still has an adapter", got)
	}
}

// TestTheAckRecordSurvivesAJSONRoundTrip pins the field names, because they
// are the file format and a rename would be silently readable as absent.
func TestTheAckRecordSurvivesAJSONRoundTrip(t *testing.T) {
	line := recordLine{Kind: "ack", MS: 12, Unasked: []string{"agy"}, Unmeasured: []string{"codex"}, Rest: true}
	raw, err := json.Marshal(line)
	if err != nil {
		t.Fatal(err)
	}
	var back recordLine
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Kind != "ack" || back.MS != 12 || !back.Rest ||
		strings.Join(back.Unasked, ",") != "agy" || strings.Join(back.Unmeasured, ",") != "codex" {
		t.Errorf("round trip = %+v from %s", back, raw)
	}
}
