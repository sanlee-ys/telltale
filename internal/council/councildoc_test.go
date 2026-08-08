package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The council frame in docs/council.md is the project's most public claim about
// what the room shows, and it is generated, not typed: the `activity` golden
// with its all-blank rows dropped and nothing else changed — the doc says
// exactly that next to the frame. The frame carried no gate at all while it
// lived in README.md, and it drifted precisely the way a gate would have
// caught, until PR #123 re-pasted it by hand; PR #126 pinned it, and it moved
// here with the walkthrough when the README went back to being a front door.
// This asserts the doc's own claim: every non-blank golden row, byte for byte
// and in order, with the blank rows out.
func TestCouncilDocFrameMatchesItsGolden(t *testing.T) {
	docPath := filepath.Join("..", "..", "docs", "council.md")
	raw, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("%v — the council walkthrough is part of the deliverable", err)
	}
	golden, err := os.ReadFile(filepath.Join("testdata", "golden", "activity.txt"))
	if err != nil {
		t.Fatal(err)
	}
	doc := strings.ReplaceAll(string(raw), "\r\n", "\n")

	// The blank rows are the room's empty scrollback — real in a live terminal,
	// dead weight in a doc. Dropping them is the whole transform; trailing
	// spaces on the kept rows are part of the frame and are kept.
	var kept []string
	for _, line := range strings.Split(strings.ReplaceAll(string(golden), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		kept = append(kept, line)
	}
	want := strings.Join(kept, "\n")

	if !strings.Contains(doc, want) {
		t.Errorf("docs/council.md's council frame is stale.\n"+
			"Re-paste it from internal/council/testdata/golden/activity.txt with the all-blank rows removed.\n--- expected ---\n%s", want)
	}
}
