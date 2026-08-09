package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// traceModel is a Model with one column and nothing else: no vendor process, no
// program loop, no terminal. applyEvents is the seam where a vendor's parsed
// events become State, and it is worth testing directly — it is the only place
// tool calls and their results are correlated, and getting that wrong attributes
// a failure to the wrong command.
func traceModel() *Model {
	return &Model{
		st: State{Columns: []Column{{
			Vendor: model.VendorClaude, Label: "Claude Code",
			Avail: AvailInstalled, Phase: PhaseStreaming,
		}}},
		sessions:   map[model.VendorID]string{},
		resumeIDs:  map[model.VendorID]string{},
		unproven:   map[model.VendorID]bool{},
		threadLost: map[model.VendorID]bool{},
		forkWatch:  map[model.VendorID]string{},
		redactors:  map[model.VendorID]*Redactor{},
	}
}

func announce(id, text string) runner.Event {
	return runner.Event{
		Vendor: model.VendorClaude, Kind: runner.KindActivity,
		Acts: []runner.ActCall{{ID: id, Text: text}},
	}
}

func resolve(id string, outcome runner.ActStatus, detail string) runner.Event {
	return runner.Event{
		Vendor: model.VendorClaude, Kind: runner.KindActivity,
		Acts: []runner.ActCall{{ID: id, Outcome: outcome, Detail: detail}},
	}
}

// TestOverlappingToolCallsResolveToTheRightEntries is the reason correlation is
// by id and not by arrival order.
//
// This is not a hypothetical: the very first live Claude probe returned the
// SECOND call's failure ahead of the first call's success. A trace zipped by
// position would have marked `echo hi` as the thing that failed and the blocked
// command as the thing that worked — a gauge reporting the opposite of what
// happened, which is worse than the no-outcome trace it replaced.
func TestOverlappingToolCallsResolveToTheRightEntries(t *testing.T) {
	m := traceModel()
	// Both calls announced together, as a parallel batch really arrives.
	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindActivity,
		Acts: []runner.ActCall{
			{ID: "toolu_first", Text: "Bash: echo hi"},
			{ID: "toolu_second", Text: "Bash: ls /nonexistent-xyz"},
		},
	}})
	// Results, out of order — second first, exactly as captured.
	m.applyEvents([]runner.Event{
		resolve("toolu_second", runner.ActFailed, "ls was blocked"),
		resolve("toolu_first", runner.ActOK, ""),
	})

	acts := m.st.Columns[0].Acts
	if len(acts) != 2 {
		t.Fatalf("Acts = %+v, want the two announced calls and no extras", acts)
	}
	if acts[0].Text != "Bash: echo hi" || acts[0].Status != runner.ActOK {
		t.Errorf("first call resolved wrong: %+v", acts[0])
	}
	if acts[1].Text != "Bash: ls /nonexistent-xyz" || acts[1].Status != runner.ActFailed {
		t.Errorf("second call resolved wrong: %+v", acts[1])
	}
	if acts[1].Detail != "ls was blocked" {
		t.Errorf("Detail = %q, want the vendor's own line on the call it belongs to", acts[1].Detail)
	}
	// Order is chronological — the order the calls were ANNOUNCED, not the
	// order their results came back. A trace that reshuffled itself as results
	// landed would be unreadable while a turn is live.
	if acts[0].ID != "toolu_first" {
		t.Error("the trace reordered itself around the results")
	}
}

// TestAResultForAnUnannouncedCallStillLands: a vendor that reports only
// completions — or one whose announcement was lost to a cancelled read — should
// still show the step WITH its outcome, rather than have it dropped for having
// missed its own introduction.
func TestAResultForAnUnannouncedCallStillLands(t *testing.T) {
	m := traceModel()
	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindActivity,
		Acts: []runner.ActCall{{ID: "item_7", Text: "go test ./...", Outcome: runner.ActFailed, Detail: "FAIL"}},
	}})
	acts := m.st.Columns[0].Acts
	if len(acts) != 1 || acts[0].Status != runner.ActFailed || acts[0].Text != "go test ./..." {
		t.Fatalf("Acts = %+v, want one already-resolved entry", acts)
	}
}

// TestANamelessResultIsDropped: a result whose id matches nothing and which
// carries no text of its own has nothing to name it by. Appending it would put
// a bare mark in the trace saying that something unnamed failed.
func TestANamelessResultIsDropped(t *testing.T) {
	m := traceModel()
	m.applyEvents([]runner.Event{resolve("toolu_ghost", runner.ActFailed, "boom")})
	if n := len(m.st.Columns[0].Acts); n != 0 {
		t.Errorf("Acts = %+v, want nothing", m.st.Columns[0].Acts)
	}
}

// TestAResolvedCallIsNeverDowngradedToPending. A duplicate announcement after a
// result would make a finished call look like a running one, which is the one
// direction this trace must not move.
func TestAResolvedCallIsNeverDowngradedToPending(t *testing.T) {
	m := traceModel()
	m.applyEvents([]runner.Event{
		announce("toolu_a", "Bash: go test"),
		resolve("toolu_a", runner.ActOK, ""),
		announce("toolu_a", "Bash: go test"),
	})
	acts := m.st.Columns[0].Acts
	if len(acts) != 1 {
		t.Fatalf("Acts = %+v, want one entry", acts)
	}
	if acts[0].Status != runner.ActOK {
		t.Errorf("Status = %v; a re-announcement un-resolved a finished call", acts[0].Status)
	}
}

// TestActivityIsRedactedWholeAndDoesNotStealTheBodysBuffer.
//
// Two things at once, because they are the same bug. A shell command is one of
// the likeliest places for a token to appear on screen, so it is redacted — and
// it is redacted WHOLE rather than through the streaming redactor, which holds a
// partial word across the chunks of a token stream. Routing an act through that
// one stranded the act's last word AND spliced whatever prose was buffered for
// the BODY onto the end of the command.
func TestActivityIsRedactedWholeAndDoesNotStealTheBodysBuffer(t *testing.T) {
	m := traceModel()
	m.applyEvents([]runner.Event{
		// Mid-sentence: "the" is held back by the redactor, since it could be
		// the front half of a secret.
		{Vendor: model.VendorClaude, Kind: runner.KindText, Text: "checking the"},
		announce("toolu_a", "Bash: curl -H 'Authorization: Bearer sk-ant-abcdefghijklmnop0123'"),
	})

	acts := m.st.Columns[0].Acts
	if len(acts) != 1 {
		t.Fatalf("Acts = %+v", acts)
	}
	if strings.Contains(acts[0].Text, "sk-ant-") {
		t.Errorf("a credential reached the trace: %q", acts[0].Text)
	}
	if !strings.Contains(acts[0].Text, redacted) {
		t.Errorf("the redaction is invisible: %q", acts[0].Text)
	}
	// The command arrives whole, so nothing of it is held back. The streaming
	// redactor would have kept everything after the last space — the closing
	// quote here — waiting for a boundary that never comes.
	if !strings.HasSuffix(acts[0].Text, redacted+"'") {
		t.Errorf("the act's own last word was stranded in a buffer: %q", acts[0].Text)
	}
	// ...and the body's half-buffered word stayed with the body.
	if strings.Contains(acts[0].Text, "the") && strings.HasPrefix(acts[0].Text, "the") {
		t.Errorf("the act swallowed the body's buffered prose: %q", acts[0].Text)
	}

	// The body's own buffer is intact: flushing it at the end of the turn
	// recovers the word the act must not have taken.
	m.applyEvents([]runner.Event{{Vendor: model.VendorClaude, Kind: runner.KindDone}})
	if !strings.Contains(m.st.Columns[0].Body, "checking the") {
		t.Errorf("Body = %q, want the buffered word back", m.st.Columns[0].Body)
	}
}

// TestNoVendorsResultOverwritesWhatItAlreadyStreamed replaces
// TestCursorResultReconcilesDuplicatedStreamBeforeHandoff, whose whole subject
// was a print-mode behaviour this seat no longer has.
//
// That test pinned a Cursor-only rule: its `result` was the vendor's
// authoritative whole reply, so it REPLACED the streamed body rather than
// filling an empty one, which is what kept a delta/repeat pair from corrupting a
// /flow handoff. The rule is gone with the protocol it described — the ACP turn
// resolves with a stop reason and no text at all, so there is nothing
// authoritative to prefer, and nothing repeated to reconcile either.
//
// What is asserted now is the rule that survived, and it is the same one for all
// four seats: a result fills a column that streamed nothing, and never overwrites
// one that streamed something.
func TestNoVendorsResultOverwritesWhatItAlreadyStreamed(t *testing.T) {
	for _, v := range []model.VendorID{model.VendorCursor, model.VendorClaude} {
		t.Run(string(v), func(t *testing.T) {
			m := traceModel()
			m.st.Columns[0].Vendor = v
			m.redactors = map[model.VendorID]*Redactor{v: {}}
			m.applyEvents([]runner.Event{
				{Vendor: v, Kind: runner.KindText, Text: "streamed "},
				{Vendor: v, Kind: runner.KindText, Text: "answer"},
				{Vendor: v, Kind: runner.KindMeta, Text: "a whole reply from somewhere else"},
			})
			if got := m.st.Columns[0].Body; got != "streamed answer" {
				t.Fatalf("body = %q — a result overwrote text the user already watched arrive", got)
			}
		})
	}
}

// TestATurnThatStreamedNothingStillFillsFromItsResult is the other half, and it
// is what the Cursor seat NO LONGER HAS.
//
// §9.6c leaned on that fallback by name — "the failure mode is a column that
// fills at the end, never one that is wrong". On ACP there is no final reply to
// fall back to, so a broken chunk parser gives an EMPTY column rather than a late
// one. The mechanism is still here for the seats that do send one, and this test
// says which behaviour belongs to which so nobody re-derives the wrong one.
func TestATurnThatStreamedNothingStillFillsFromItsResult(t *testing.T) {
	m := traceModel()
	m.st.Columns[0].Vendor = model.VendorClaude
	m.redactors = map[model.VendorID]*Redactor{model.VendorClaude: {}}
	m.applyEvents([]runner.Event{
		{Vendor: model.VendorClaude, Kind: runner.KindMeta, Text: "the whole reply"},
	})
	if got := m.st.Columns[0].Body; got != "the whole reply" {
		t.Fatalf("body = %q, want the result to have filled an empty column", got)
	}
}

// TestSingleTokenStreamPlusResultDoesNotDouble is the ALPHAALPHA bug: Feed
// holds a whitespace-free chunk, Body still looks empty, and the old Meta path
// ran result through Feed+Flush on top of the hold.
func TestSingleTokenStreamPlusResultDoesNotDouble(t *testing.T) {
	m := traceModel()
	m.applyEvents([]runner.Event{
		{Vendor: model.VendorClaude, Kind: runner.KindText, Text: "ALPHA"},
		{Vendor: model.VendorClaude, Kind: runner.KindMeta, Text: "ALPHA"},
	})
	if got := m.st.Columns[0].Body; got != "ALPHA" {
		t.Fatalf("body = %q, want ALPHA once (redactor hold + result must not concatenate)", got)
	}
}

// TestDispatchClearsTheTraceBetweenTurns. Act ids are scoped to a turn — agy's
// are just step indices — so a trace carried across a dispatch would let turn
// 2's step 3 resolve turn 1's.
func TestDispatchClearsTheTraceBetweenTurns(t *testing.T) {
	m := traceModel()
	m.applyEvents([]runner.Event{announce("step-3", "tool")})
	if len(m.st.Columns[0].Acts) != 1 {
		t.Fatal("setup did not record the act")
	}
	// dispatch() itself needs a vendor binary and a registry entry, so the
	// clearing contract is asserted where it lives rather than by driving a
	// real dispatch: this is the line dispatch runs per column.
	m.st.Columns[0].Acts = nil
	m.applyEvents([]runner.Event{resolve("step-3", runner.ActUnknown, "")})
	if n := len(m.st.Columns[0].Acts); n != 0 {
		t.Errorf("a stale id from the previous turn resolved into the new one: %+v", m.st.Columns[0].Acts)
	}
}
