package council

import (
	"strings"
	"testing"
	"time"
)

// The frame corpus renders the same rooms at the heights a person actually
// runs them, and reports how much of each frame is chrome drawn around
// nothing.
//
// Every layout golden in this package renders at 120x24, because that is what
// room() builds. A four-seat room with a line or two per column fills a
// 24-row frame; the same room in a 60-row window is mostly rules drawn through
// empty space, and no test in this package had ever rendered one. The frame
// that prompted this file was 120x60 and read as a spreadsheet of pipes — a
// state the corpus was structurally unable to show.
//
// So the height is the variable and the width is held still. Three heights:
// the 24 rows the rest of the corpus already pins, 40 as a common window, and
// 60 as the one that was reported.

var corpusHeights = []int{24, 40, 60}

// corpusIdle is a seated room before anything has been dispatched.
func corpusIdle() State { return room() }

// corpusReattached is the shape that was reported: a room fact repeated once
// per column. It uses the real Reattached path (empty Body + Active reattach)
// so the hoist — notice once, per-seat line in columns — is what the corpus
// measures, not a hand-pasted Body that would keep lying after the fix.
func corpusReattached() State {
	st := room()
	st.Now = time.Date(2026, 8, 5, 22, 0, 0, 0, time.UTC)
	st.Turn = 109
	st.Reattached = Reattach{
		Turn:    109,
		SavedAt: st.Now.Add(-time.Minute),
	}
	for i := range st.Columns {
		st.Columns[i].Restored = true
	}
	st.Notice = "reattached from ~/.telltale/council/room.json — turn 109 was the last, saved just now, 3/3 seats restored"
	return st
}

// corpusStreaming is one seat producing and the rest waiting on it, which is
// the state the room spends most of a turn in.
func corpusStreaming() State {
	st := room()
	st.Columns[0].Phase = PhaseStreaming
	st.Columns[0].Body = "Reading internal/council/layout.go to find the breakpoints."
	for i := 1; i < len(st.Columns); i++ {
		st.Columns[i].Phase = PhaseWaiting
	}
	return st
}

// TestFrameCorpus pins each room at each height.
//
// These goldens exist to be LOOKED at, not only compared: the diff after a
// layout change is the review artifact. They are deliberately separate from
// the behavioural goldens so that regenerating them says "the frame moved"
// rather than "a claim changed".
func TestFrameCorpus(t *testing.T) {
	for _, c := range []struct {
		name  string
		state func() State
	}{
		{"idle", corpusIdle},
		{"reattached", corpusReattached},
		{"streaming", corpusStreaming},
	} {
		for _, h := range corpusHeights {
			st := c.state()
			st.Width, st.Height = 120, h
			golden(t, "corpus-"+c.name+"-120x"+itoa(h), render(st))
		}
	}
}

// TestFrameCorpusReportsFill turns "it looks empty" into a number.
//
// A row counts as empty when removing the rules and the spaces leaves nothing:
// that is precisely the case the report is about, a line of the frame whose
// only content is the chrome dividing content that is not there. The test
// asserts nothing about the ratio — a bar would be a taste judgement pinned as
// a fact — it prints the table so a layout change can be argued from measured
// numbers instead of from two people's memory of a screenshot.
func TestFrameCorpusReportsFill(t *testing.T) {
	for _, c := range []struct {
		name  string
		state func() State
	}{
		{"idle", corpusIdle},
		{"reattached", corpusReattached},
		{"streaming", corpusStreaming},
	} {
		for _, h := range corpusHeights {
			st := c.state()
			st.Width, st.Height = 120, h
			rows := strings.Split(render(st), "\n")
			empty := 0
			for _, r := range rows {
				if bare(r) == "" {
					empty++
				}
			}
			t.Logf("%-12s 120x%-3d  %2d/%2d rows are rules around nothing (%d%%)",
				c.name, h, empty, len(rows), empty*100/max1(len(rows)))
		}
	}
}

// bare strips the characters that draw the frame, leaving only what the frame
// was drawn around.
func bare(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		switch r {
		case '│', '─', '|', '-', ' ', '\t':
			return -1
		}
		return r
	}, s))
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
