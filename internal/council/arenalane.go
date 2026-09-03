package council

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// The arena drawn as a RACE (room-identity graft, 2026-09-03).
//
// §9.37 gave a race the transcript's own grammar — a labelled rule, a worktree
// path, a finish line, a receipt, a verdict, a stat — and that is a correct
// RECORD of a race and not a picture of one. A reader arriving at three columns
// of it has to read three paragraphs and hold three numbers in their head to
// answer the question the whole feature exists for: who won, and is what they
// wrote any good. The 2026-09-03 audit said the same thing from outside: of the
// three identity prototypes, only the one that drew lanes, empty tracks, leading
// verdict marks and `/adopt` "form the only convincing race grammar."
//
// So this file is that grammar, grafted onto the MONOGRAPH ink and deliberately
// three moves and no more:
//
//  1. a LANE — the finish line with a track in front of it, filled to the
//     racer's host-observed position;
//  2. a VERDICT MARK — one glyph in front of checkLine's existing sentence, so
//     PASS and FAIL are findable at projector distance instead of being the
//     fourth word of the fifth line of a paragraph;
//  3. the ADOPT AFFORDANCE — the command that ends the race, on the block's own
//     rule, where the room already prints the branch it would merge.
//
// **What did NOT come with them, by ruling.** The prototype gave the lane the
// seat's own hue and put the verdict word on a coloured card. Seat hues are
// retired (style.go's seatInk) and the audit upheld that retirement, so the lane
// is drawn in the measured ink; and the audit asked for the leading marks and
// the explicit PASS / FAIL / unavailable wording "but no coloured verdict
// backgrounds", so there is no chip here at all. The verdict's own severity ink
// (checkStyle) was already carrying the line and still is.
//
// Two rules bind every line here and neither is negotiable. The WORDS come
// first: strip the colour and strip the glyphs and the block still says the
// rank, the verdict and the command, which is what `--ascii` and NO_COLOR get.
// And nothing is drawn that was not measured: a racer with no rank gets an EMPTY
// track, never a short one, because a short track is a finishing position the
// host never reported (§4a.1).

// laneCells is how many cells a lane track spends.
//
// Fixed rather than proportional, and small. A track that grew with the column
// would make the same race look like a different one at two window widths, which
// is the opposite of what a scoreboard is for; and the words beside it are the
// precision, so the track only has to carry the GLANCE — the HUD's own argument
// for dropping its gauge to whole cells under `--ascii`.
const laneCells = 8

// laneLines is one settled racer's finish line with its lane in front of it.
//
// **What the fill means, stated exactly, because a bar that means nothing is
// decoration.** It is the racer's host-observed finishing position and nothing
// else: first of three fills the track, last of three fills a third of it. Rank
// and Of are both measured — the same two numbers the words beside the track
// spell out — so the bar is a second rendering of a measured fact rather than a
// derived one. It never appears without those words, which is the §4a.1 rule the
// rank line already followed ("2nd · failed" is a different fact from "2nd ·
// done", so the number never stands alone).
//
// **The shed.** Below the width where the track and the whole finish line both
// fit, the TRACK goes and the words stay, and the line is then byte-identical to
// what §9.37 drew. Identity yields first (§9.11), and here the track is the
// identity: it says who won, and the words say what winning was.
func laneLines(rank, of int, finish string, w int, sty Styles, g Glyphs) []string {
	if rank <= 0 || of <= 0 || laneCells+2+lipgloss.Width(finish) > w {
		return styleAll(wrap(finish, w), sty.bold(sty.Text))
	}
	// Integer arithmetic, floored, then raised to one: a racer that finished is
	// on the board, so its lane is never empty — an empty track is reserved for
	// the racer that has not finished (runningLaneLines), and the two must not
	// collapse.
	filled := (of - rank + 1) * laneCells / of
	if filled < 1 {
		filled = 1
	}
	if filled > laneCells {
		filled = laneCells
	}
	lane := sty.Lane().Render(strings.Repeat(g.Fill, filled)) +
		sty.Track().Render(strings.Repeat(g.Rule, laneCells-filled))
	return []string{fit(lane+"  "+sty.bold(sty.Text).Render(finish), w)}
}

// runningLaneLines is the lane of a racer that has NOT finished.
//
// An EMPTY track and a spinner. The track is drawn at full length in the
// hairline ink, so the reader sees the distance and sees that none of it has been
// claimed; the spinner is the room's existing mark for "in flight" and it is the
// only moving thing in the block, which is what makes motion mean something here
// rather than being ambient.
//
// "no rank yet" rather than a blank: the interim block's whole discipline is that
// a mid-race read must not wear the finish line's clothes, and a lane with no
// words beside it would be a bar the reader is invited to interpret.
//
// The spinner frame comes from State, never from a clock, because Render is pure
// over its State (TestRenderIsPure).
func runningLaneLines(st State, w int, sty Styles, g Glyphs) []string {
	// The middle dot is the arena block's own separator in BOTH glyph sets —
	// `1st of 3 · done · 20s` is what the ascii golden already holds — so this
	// line joins with the punctuation its neighbours use rather than inventing a
	// second spelling for the reduced set.
	words := "racing · no rank yet"
	mark := g.Idle
	if len(g.Spinner) > 0 {
		mark = g.Spinner[st.Spinner%len(g.Spinner)]
	}
	head := mark + " "
	if lipgloss.Width(head)+laneCells+2+lipgloss.Width(words) > w {
		return styleAll(wrap(words, w), sty.Muted)
	}
	return []string{fit(sty.Muted.Render(head)+
		sty.Track().Render(strings.Repeat(g.Rule, laneCells))+
		"  "+sty.Muted.Render(words), w)}
}

// verdictLines is checkLine's sentence with a MARK in front of it.
//
// **The words do not change.** `check PASS · 1m14s · go test ./...` is what
// §9.48 settled and what every golden pins; what is added is a leading glyph from
// the alphabet the trace already uses for exactly these four facts — `✓`
// succeeded, `✗` failed, `⚠` could not be measured, the spinner still running.
// It is a second signal over a sentence that already carried the whole thing.
//
// **Why the mark is worth two cells here when it was not before.** A race puts
// three of these sentences side by side, one per column, and the only cell that
// differs between them is the fourth word. §9.48 already argues that the command
// goes LAST because it is identical across the race and the verdict is what the
// eye should reach first — the mark finishes that argument by putting the verdict
// at the start of the line instead of in the middle of it.
//
// The whole line keeps checkStyle's own ink, which is the audit's ruling: leading
// marks and explicit wording, and no coloured verdict background. A grey tick in
// front of a coloured sentence would read as punctuation rather than as the
// verdict arriving first, so the mark takes the sentence's own ink with it.
func verdictLines(ck *ArenaCheck, st State, w int, sty Styles, g Glyphs) []string {
	raw := verdictMark(ck, st, g) + " " + checkLine(ck)
	out := make([]string, 0, 2)
	for _, line := range wrap(raw, w) {
		out = append(out, fit(checkStyle(sty, ck).Render(line), w))
	}
	return out
}

// verdictMark is the glyph one check verdict wears.
//
// FIVE cases for checkLine's own five, in checkLine's own order, so the mark and
// the sentence cannot disagree about what happened. Every one is a mark the
// activity trace already spends on this exact fact — nothing new is introduced,
// which is what keeps the ASCII set whole (`+`, `x`, `!`, `?`).
func verdictMark(ck *ArenaCheck, st State, g Glyphs) string {
	switch {
	case ck.Running:
		if len(g.Spinner) > 0 {
			return g.Spinner[st.Spinner%len(g.Spinner)]
		}
		return g.Idle
	case ck.Err != "":
		// Never a verdict mark. Nothing measured this attempt, and a tick or a
		// cross would read as one of the two outcomes it explicitly is not.
		return g.Warn
	case ck.Passed():
		return g.ActOK
	case ck.Exited:
		return g.ActFail
	default:
		return g.ActUnknown
	}
}

// arenaRule is the arena block's own labelled rule, with the adopt command in the
// slot the grammar already reserves for what belongs to the label.
//
// **The affordance is the point.** `/adopt` is what a race is FOR, and until now
// it appeared nowhere on the frame: the room drew the branch, the diff and the
// verdict, and then left the one command that acts on all three to be remembered
// or found in `?`. The rule already names the branch `/adopt` would merge, so
// this is the command printed beside its own object.
//
// **Bare, not `/adopt <seat>`, and that is a width decision with a correctness
// argument behind it.** A three-seat race at 120 columns gives each block 37
// cells; `arena arena/t6/claude` plus `/adopt claude` does not fit, and labelRule
// would shed the affordance at exactly the width the race is normally read at.
// Bare `/adopt` fits, and lifecycle.go already makes the bare form answer for
// itself — it lists the seats and says what the command does — so what is printed
// is a command that works when typed, not an abbreviation of one.
//
// The label stays chrome and the command takes weight, which is the room's own
// treatment of a key (Styles.Text names "the keys in the mode line" among its
// spenders). It is deliberately NOT the measured ink: a command is something the
// reader may do, not something the room read.
func arenaRule(branch, adopt string, w int, sty Styles, g Glyphs) string {
	label := "arena " + branch
	plain := labelRule(label, adopt, w, g)
	if adopt == "" || !strings.HasSuffix(plain, adopt) {
		// The affordance shed at this width. Chrome whole, exactly as before.
		return sty.Muted.Render(padRight(plain, w, g))
	}
	head := strings.TrimSuffix(plain, adopt)
	return fit(sty.Muted.Render(head)+sty.bold(sty.Text).Render(adopt), w)
}

// adoptAffordance is the command that adopts an attempt.
//
// A function rather than a constant so the next reader sees where a
// seat-qualified spelling would go, and so there is one place to change if
// `/adopt` ever stops taking a bare form.
func adoptAffordance() string { return "/adopt" }
