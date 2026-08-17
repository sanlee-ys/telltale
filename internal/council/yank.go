package council

import (
	"strings"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// Yank is what a copy keystroke produced: the text that goes on the clipboard,
// and one line saying what was taken.
//
// A struct rather than a bare string because the notice is not decoration here.
// OSC 52 is a write into the terminal with no acknowledgement of any kind — the
// room cannot observe whether the sequence was honoured, so the ONLY feedback a
// user gets that a key did anything is this line. A silent copy and a copy into
// a terminal that ignores OSC 52 would look identical, which is precisely the
// ambiguity §4a.1 forbids everywhere else in this product.
type Yank struct {
	Text   string
	Notice string
}

// Empty reports that there was nothing to copy. The caller says so rather than
// putting an empty string on the clipboard: clearing someone's clipboard is a
// destructive act, and "nothing happened" must not be spelled the same way as
// "your clipboard is now empty".
func (y Yank) Empty() bool { return y.Text == "" }

// YankColumn copies one seat's current reply — the sanitized Body the renderer
// is showing, and nothing else.
//
// Three things it is deliberately NOT. Not the raw stream: everything on State
// has been through the redaction and sanitize choke point, and copying anything
// upstream of that would put a credential on a clipboard, which is a worse place
// than a screen because it outlives the room. Not the trace: the trace is what
// the seat DID and the reply is what it SAID, and §4a.1's rule that two kinds of
// claim must not be concatenated does not stop applying because the destination
// is a document. And not another seat's: the key addresses the focused column,
// the same column every scroll key addresses, because a copy key that took from
// somewhere other than where the eye is would be the §9.12 failure with a
// clipboard attached.
//
// It falls back to the newest finished turn when the current one has produced
// nothing yet. "The last answer" is what a user means by this key, and a seat
// that has been asked a new question has not stopped having answered the old
// one.
func (s State) YankColumn(idx int) Yank {
	if idx < 0 || idx >= len(s.Columns) {
		return Yank{}
	}
	c := s.Columns[idx]

	// An arena seat's deliverable is its diff, so that is what y copies —
	// ruled with the mode itself (§9.37): "y yanks the full diff for that
	// seat". The reply is still on screen and in history; the diff is the
	// thing the reader takes somewhere else to review or apply.
	if c.Arena != nil && c.Arena.Diff != "" {
		notice := "copied " + c.Label + "'s arena diff (turn " + itoa(c.TurnN) + ")"
		if c.Arena.DiffTruncated {
			notice += " — truncated at 1 MB; the worktree holds the whole of it"
		}
		return Yank{Text: c.Arena.Diff, Notice: notice}
	}

	text, turn := strings.TrimSpace(c.Body), c.TurnN
	if text == "" {
		// Newest first: History is oldest-first, so this walks backwards to the
		// most recent turn that actually said something.
		for i := len(c.History) - 1; i >= 0; i-- {
			if b := strings.TrimSpace(c.History[i].Body); b != "" {
				text, turn = b, c.History[i].N
				break
			}
		}
	}
	if text == "" {
		return Yank{Notice: "nothing to copy — " + c.Label + " has not answered yet"}
	}
	return Yank{
		Text:   text,
		Notice: "copied " + c.Label + "'s turn-" + itoa(turn) + " reply",
	}
}

// YankTurn copies the whole CURRENT turn. See YankTurnN.
func (s State) YankTurn() Yank { return s.YankTurnN(s.Turn) }

// YankTurnN copies one turn: every seat that took part, labelled, with the brief
// that produced it at the top.
//
// Any turn rather than only the newest, since the by-turn page can be open on an
// older one and `y` there takes the page (§9.22). The participants come from
// turnEntries, which is the SAME call the page renders from — that is the whole
// reason the page could be built: this document already decided who is in a
// turn, in what order, and with the brief at the top, and the only thing that
// could read it was a clipboard.
//

// The brief is included and it is not padding. A file of four answers to a
// question it does not contain is unreadable a week later, and the brief is the
// user's own words — already echoed on screen under the composer's own mark, and
// deliberately un-redacted there for reasons §9.9 states at length. What is NOT
// included is anything that rode along with it: a first turn carries the --brief
// file, whose content this ADR keeps off State entirely, and a rebuttal turn
// carries other vendors' answers. The echo boundary is the same one here — the
// principal's words go in, and what was attached to them does not.
//
// Only seats that took THIS turn are included, which is the routing rule
// (§9.9) rather than a filter: a seat that sat out turn 11 still holds turn 9's
// reply, and pasting it under a turn-11 heading would be the room inventing a
// conversation into a document, where it would outlive every chance to notice.
func (s State) YankTurnN(n int) Yank {
	entries := s.turnEntries(n)
	if len(entries) == 0 {
		return Yank{Notice: "nothing to copy — no seat has taken this turn yet"}
	}

	var b strings.Builder
	brief := ""
	for i, e := range entries {
		text := strings.TrimSpace(e.Body)
		if text == "" {
			// A seat that was asked and said nothing is a fact, not a gap —
			// the same reason the transcript prints "(no reply)" rather than
			// leaving a hole. It counts as a participant.
			text = "(no reply)"
		}
		if brief == "" {
			brief = strings.TrimSpace(e.Prompt)
		}
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## " + e.Label + "\n\n" + text)
	}

	head := "# turn " + itoa(n)
	if brief != "" {
		head += "\n\n> " + strings.ReplaceAll(brief, "\n", "\n> ")
	}
	noun := " seats"
	if len(entries) == 1 {
		noun = " seat"
	}
	return Yank{
		Text:   head + "\n\n" + b.String() + "\n",
		Notice: "copied turn " + itoa(n) + " — " + itoa(len(entries)) + noun,
	}
}

// YankPage copies whatever the open page is SHOWING.
//
// One call for both faces, and it is what keeps the key honest rather than a
// convenience: §9.22 gave the page's `y` a footer cell on the argument that here
// the key takes the thing in front of the reader, which is a claim a reader can
// check against the screen. A face flip that left the copy key on the other
// document would break exactly that claim, silently, into a file.
func (s State) YankPage() Yank {
	if s.Page.Ledger {
		return s.YankActsN(s.Page.Turn)
	}
	return s.YankTurnN(s.Page.Turn)
}

// YankActsN copies one turn's ACTS: every seat that took part, labelled, with
// each call and the outcome the vendor reported for it.
//
// YankTurnN's own document with the other half of the turn in it, assembled from
// the SAME turnEntries call — which is the point rather than a saving. There is
// no second sanitizer here and there must never be one: everything on State has
// already been through the single redact-and-sanitize choke point on the way in,
// so a cleaning step of this file's own would be a second answer to "what is safe
// to put on a clipboard", and the two answers would differ on the day one of them
// was updated. The brief, the participants and the sat-out rule are all the same
// for the same reasons (§9.15) — a seat that sat this turn out is absent, because
// filing its older acts under this turn's heading would be the room inventing a
// history into a file that outlives every chance to notice.
//
// The retention sentence is carried into the document because the document
// outlives the room. On screen it qualifies a claim the reader can re-check by
// pressing `[`; in a file pasted into an issue a week later it is the only thing
// saying that "the acts" was ever bounded.
func (s State) YankActsN(n int) Yank {
	entries := s.turnEntries(n)
	if len(entries) == 0 {
		return Yank{Notice: "nothing to copy — no seat has taken this turn yet"}
	}

	var b strings.Builder
	brief, acts := "", 0
	for i, e := range entries {
		if brief == "" {
			brief = strings.TrimSpace(e.Prompt)
		}
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## " + e.Label + "\n\n")
		if len(e.Acts) == 0 {
			// The screen's own words, so a paste and the surface it was taken
			// from make the same claim: no act was RECORDED, which is not the
			// same as the seat having done nothing.
			b.WriteString(noActs)
			continue
		}
		for j, a := range e.Acts {
			acts++
			if j > 0 {
				b.WriteString("\n")
			}
			// The word alone, with no mark: a mark is a screen affordance and
			// actWord is the signal it seconds, so the document loses nothing by
			// dropping it (§9.11's rule, one surface over).
			b.WriteString("- " + a.Text)
			if w := actWord(a.Status, e.working()); w != "" {
				b.WriteString(" — " + w)
			}
			// A failure's own first line, on the trace's rule: only a failure has
			// one, and it is the vendor's words rather than a diagnosis council
			// wrote.
			if a.Status == runner.ActFailed && a.Detail != "" {
				b.WriteString("\n  " + a.Detail)
			}
		}
	}

	head := "# turn " + itoa(n) + " — acts"
	if brief != "" {
		head += "\n\n> " + strings.ReplaceAll(brief, "\n", "\n> ")
	}
	head += "\n\n" + retentionNotice()

	noun := " seats"
	if len(entries) == 1 {
		noun = " seat"
	}
	tally := noActs
	if acts > 0 {
		tally = itoa(acts) + " " + plural(acts, "act")
	}
	return Yank{
		Text: head + "\n\n" + b.String() + "\n",
		Notice: "copied turn " + itoa(n) + "'s acts — " +
			itoa(len(entries)) + noun + ", " + tally,
	}
}
