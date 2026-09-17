package council

import (
	"strings"
	"testing"
	"time"
)

// TestTheReplayFactIsHonestAboutWhatTheFileCarries pins the room line's
// provenance clause by clause (roomline.go, replayFact): a live room draws
// none, a capture draws its stamp in the zone the recorder wrote, a file with
// no readable stamp draws no date rather than the epoch, and a scrubbed file
// says its date is synthesized with its words.
func TestTheReplayFactIsHonestAboutWhatTheFileCarries(t *testing.T) {
	live := room()
	if got := replayFact(live); got != "" {
		t.Errorf("a live room has a provenance line: %q", got)
	}

	capture := room()
	capture.Replay = true
	capture.Recorded = time.Date(2026, 9, 3, 21, 14, 0, 0, time.FixedZone("", -4*60*60))
	if got, want := replayFact(capture), "recorded 2026-09-03 21:14 -0400"; got != want {
		t.Errorf("capture = %q, want %q", got, want)
	}

	unstamped := room()
	unstamped.Replay = true
	if got := replayFact(unstamped); got != "" {
		t.Errorf("a file with no readable stamp drew %q, want nothing", got)
	}
	if got := render(unstamped); strings.Contains(got, "recorded") || strings.Contains(got, "1970") || strings.Contains(got, "0001") {
		t.Errorf("a file with no readable stamp drew a date:\n%s", got)
	}

	scrubbed := capture
	scrubbed.Scrubbed = true
	if got := replayFact(scrubbed); !strings.HasPrefix(got, "recorded 2026-09-03 21:14 -0400 · scrubbed: ") ||
		!strings.Contains(got, "the date and every word are synthesized") {
		t.Errorf("scrubbed = %q", got)
	}

	scrubbedUnstamped := unstamped
	scrubbedUnstamped.Scrubbed = true
	if got := replayFact(scrubbedUnstamped); strings.Contains(got, "date") || !strings.HasPrefix(got, "scrubbed: ") {
		t.Errorf("scrubbed with no stamp = %q, want the claim with no date in it", got)
	}
}

// TestTheReplayFactLeadsTheRoomLine. The provenance is the fact about the
// whole room, so it is the first segment and the collapsed-seat sentence
// follows it, on roomline.go's own order.
func TestTheReplayFactLeadsTheRoomLine(t *testing.T) {
	st := room()
	st.Replay = true
	st.Recorded = time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	st.Columns[1].Avail = AvailNotInstalled
	lines := roomLines(st, 160, GlyphsFor(false))
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "recorded 2026-09-01 10:00 UTC") {
		t.Errorf("the room line does not lead with the provenance: %q", lines)
	}
	joined := strings.Join(lines, " ")
	if !strings.Contains(joined, "not on screen") {
		t.Errorf("the collapsed seat left the room line: %q", lines)
	}
}
