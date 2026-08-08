package council

import "strings"

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
	// ruled with the mode itself (§9.35): "y yanks the full diff for that
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
