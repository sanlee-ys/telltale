package council

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// The act ledger: one turn, read for what the seats DID (design.md §9.22,
// amended 2026-08-17).
//
// The page answers "what did the seats say about turn 10"; this answers "what
// did they DO in it" — the tool calls, the commands, the edits, each with the
// outcome the vendor reported. It adds NO record. Every line here comes out of
// turnEntries, the same projection the page and `Y` already read, so the three
// surfaces cannot disagree about who was in a turn or what they ran; the acts
// were parsed, redacted, sanitized and retained long before this file, and what
// was missing was a surface that shows them without a reply wrapped around them.
//
// Why it is worth a surface at all: in the grid a trace entry lives inside a
// 37-cell column, where the outcome is a single mark and a wrapped command is
// most of the column. The ledger spends the whole frame on the same records, so
// the outcome is a WORD, a failure keeps the vendor's own first line under it,
// and five seats' work in one turn reads as one list instead of five scrolls.
//
// The two claims this file is careful about are the two it could get wrong for
// free. An act with no reported outcome renders as having none — never as
// success — which is runner.ActStatus' whole reason for existing (antigravity's
// steps flip ACTIVE then DONE and no captured line has ever carried a success
// signal). And a seat with no acts says no act was RECORDED, not that the seat
// did nothing: a trace is a reading of what a vendor chose to report, and "it
// did nothing" is a claim no adapter here can source (§4a.1).

// noActs is what a seat that recorded nothing this turn prints.
//
// The wording is the honest half of the distinction above, and it is one string
// because the screen and the clipboard must make the same claim. "(no reply)" is
// its sibling on the page: both say a fact was measured to be empty rather than
// leaving a hole a reader would read as the room dropping a seat.
const noActs = "(no acts recorded)"

// retentionNotice is the qualification the ledger's own header carries: how far
// back this room can be asked about acts at all.
//
// It exists because "the acts" is a claim about a scope, and the scope has a
// hard edge — maxHistory drops the oldest turn per seat, so a room deep into a
// session cannot be asked what happened in turn 3, and a ledger that said
// nothing about that would be offering an unqualified record with a silent floor
// under it. Read off the live constant rather than typed, so the sentence cannot
// outlive a change to the cap, and spelled once here because the yank document
// makes the same claim (YankActsN) — two spellings would be two contracts.
//
// It is a LINE rather than meta on the rule. labelRuleIn drops its meta whole
// when the width will not take it, which is correct for a route and a count and
// wrong for this: at the narrow tier the qualification would be the first thing
// to go, leaving the unqualified claim standing exactly where the room has the
// least space to make it.
func retentionNotice() string {
	return "the room keeps the last " + strconv.Itoa(maxHistory) +
		" turns per seat — an act from an older turn is gone."
}

// ledgerLines is the whole ledger for one turn, as a flat list of lines.
//
// pageLines' own assembly, deliberately: the same heavy turn rule at the top,
// the same brief once, the same blank row where the speaker changes, the same
// per-seat labelled rule carrying that seat's outcome and clock. A reader
// flipping between the two faces is reading one document in two registers, and a
// second layout grammar here would make them two places.
func ledgerLines(st State, n, w int, sty Styles, g Glyphs) []string {
	entries := st.turnEntries(n)
	if len(entries) == 0 {
		return evictedLines(n, w, sty)
	}

	// The heavy rule, on the page's own argument (§9.26): this is the one line
	// inside the frame that heads a whole document rather than a part of one.
	out := []string{strongLabelRule("acts in turn "+strconv.Itoa(n),
		ledgerMeta(st, entries), w, g.RuleHeavy, sty)}
	// Hanging under it in the card grammar every other title in this room uses
	// (§9.11): the reader came for the line above, and this is the qualification
	// they read because that line made them want to.
	out = append(out, styleAll(indentWrap("  ", retentionNotice(), w), sty.Muted)...)

	// The brief once, exactly as the page prints it and for the page's reason: a
	// list of commands under a question it does not contain is unreadable a week
	// later, and there is one brief in a turn rather than one per seat (§9.22).
	if echo := promptEcho(entries[0].Prompt, entries[0].Quoted, w, sty, g); len(echo) > 0 {
		out = append(out, "")
		out = append(out, echo...)
	}

	for _, e := range entries {
		out = append(out, "")
		out = append(out, ledgerSeat(st, e, w, sty, g)...)
	}
	return out
}

// ledgerSeat is one seat's block: its name and how its turn ended, then every
// act it recorded, in the order the vendor reported them.
//
// No note card and no reply, and both omissions are the projection rather than a
// shortfall. How the turn ENDED is already on this seat's own rule, in
// seatMeta's words — the same three the column header states — so a note card
// under it would spend rows restating an outcome the reader has just read. The
// reply is the other face; `t` is one keystroke away and the mode word says
// which face is live.
func ledgerSeat(st State, e turnEntry, w int, sty Styles, g Glyphs) []string {
	out := []string{seatRule(e.Vendor, e.Label, seatMeta(st, e, g), w, sty, g)}
	if len(e.Acts) == 0 {
		return append(out, sty.Muted.Render(padRight(noActs, w, g)))
	}
	for _, a := range e.Acts {
		mark, style := ledgerMark(a.Status, e.working(), sty, g)
		out = append(out, actLinesMarked(a, mark, style, w, sty, g)...)
	}
	return out
}

// working reports that this seat is STILL AT IT — the turn is the column's
// current one and the vendor is waiting or streaming.
//
// It is pageSeat's own predicate, spelled once here because the ledger asks the
// same question of the same entry and getting it wrong is expensive. turnEntry.Live
// means "this is the column's current turn rather than a filed record", which is
// NOT the same as "the seat is running": the newest turn stays the current one
// long after every seat has landed. An unresolved call on a seat that has
// finished is a call the vendor never resolved, and reading Live alone would
// report it as running for the rest of the session.
func (e turnEntry) working() bool {
	return e.Live && (e.Phase == PhaseWaiting || e.Phase == PhaseStreaming)
}

// ledgerMeta is what the ledger's own rule carries beside the turn number: where
// the turn went, and how many acts this document holds.
//
// The route comes from pageRoute, so both faces of one turn state the same
// destination in Route.label()'s vocabulary — what is displayed is what would
// have to be typed to reproduce it (§9.21).
//
// The count is a COUNT of records, never a figure derived from them: it says how
// long the list under it is, which is the one number a reader wants before they
// start scrolling. Zero prints the same words the empty seat does rather than
// "0 acts", because "no acts recorded" is the honest reading of a zero here —
// the room recorded none, which is not the same as the seats having done none.
func ledgerMeta(st State, entries []turnEntry) string {
	var parts []string
	if r := pageRoute(st, entries); r != "" {
		// The literal arrow, as everywhere else this room states a route (§9.21).
		parts = append(parts, "→ "+r)
	}
	n := 0
	for _, e := range entries {
		n += len(e.Acts)
	}
	if n == 0 {
		parts = append(parts, noActs)
	} else {
		parts = append(parts, itoa(n)+" "+plural(n, "act"))
	}
	// historyMeta's own two spaces: one grammar for the numbers that belong to a
	// label, in both projections and on both faces.
	return strings.Join(parts, "  ")
}

// actWord is the outcome of one act, in words.
//
// The WORD is the signal and the mark beside it is the second one, which is this
// room's rule for every distinction it draws — so the ledger reads the same under
// --ascii and under NO_COLOR, where a mark is all that would be left of a
// coloured tick. It is also what the clipboard document prints, so the screen and
// the paste cannot drift into two vocabularies for one status.
//
// The five statuses stay five. `ok` and `outcome unknown` are the pair
// runner.ActStatus exists for: a vendor that reports a step ENDED and says
// nothing about how has not reported success, and rendering one as the other is
// the §4a.1 failure this file would commit most cheaply. `denied by you` keeps
// actMark's exact words, because a refusal echoed back by the vendor as an error
// is indistinguishable from a broken tool unless the room says whose decision it
// was — and that one is council's own record of a keystroke.
//
// ActPending splits on whether the SEAT is still working, and the split is the
// same distinction one level down. While the vendor is waiting or streaming, a
// call it announced and has not resolved is a call that is running. Once that
// seat has landed, the resolution never came — which is neither "running" nor
// "unknown", because the vendor never said the step ended at all. Two facts, two
// sentences. The predicate is turnEntry.working rather than turnEntry.Live: the
// newest turn stays the column's current one long after every seat has finished.
//
// An unrecognised status returns the empty string and the caller prints no
// outcome at all. A future value must arrive as a gap somebody notices, never as
// a claim this function invented for it.
func actWord(s runner.ActStatus, working bool) string {
	switch s {
	case runner.ActOK:
		return "ok"
	case runner.ActFailed:
		return "failed"
	case runner.ActUnknown:
		return "outcome unknown"
	case runner.ActDenied:
		return "denied by you"
	case runner.ActPending:
		if working {
			return "running"
		}
		return "no outcome reported"
	default:
		return ""
	}
}

// ledgerMark is actWord with the mark and the hue the trace already spends on
// that status, so one status has one appearance across both surfaces.
//
// The marks are actMark's, unchanged and for actMark's reasons: ActUnknown never
// borrows ActOK's tick, and a denial is warned rather than alarmed because it is
// the room working. What is new is only that the word rides with the mark at a
// width that can afford it.
//
// A PENDING act carries no mark, which is glyphs.go's standing rule — "a mark
// for 'nothing is known yet' would be a claim" — so the words stand behind an em
// dash instead. Punctuation rather than a glyph, so it is the same character in
// both glyph sets, and it is there to stop the outcome reading as the tail of the
// command it follows.
func ledgerMark(s runner.ActStatus, working bool, sty Styles, g Glyphs) (string, lipgloss.Style) {
	word := actWord(s, working)
	if word == "" {
		return "", sty.Text
	}
	switch s {
	case runner.ActOK:
		return g.ActOK + " " + word, sty.SevOK
	case runner.ActFailed:
		return g.ActFail + " " + word, sty.SevCrit
	case runner.ActUnknown:
		// Muted, not a severity: not knowing how a step went is not an alarm, and
		// colouring it as one would train the eye to ignore the real ones.
		return g.ActUnknown + " " + word, sty.Muted
	case runner.ActDenied:
		return g.ActFail + " " + word, sty.SevWarn
	default:
		return "— " + word, sty.Muted
	}
}
