package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The README's council frame is the project's most public claim about what the
// room shows, and it is generated, not typed: the `activity` golden with its
// all-blank rows dropped and nothing else changed — the README says exactly
// that next to the frame. The HUD's hero frame has carried this gate since it
// was pasted (TestReadmeHeroFrameMatchesItsGolden); the council frame did not,
// and it drifted precisely the way a gate would have caught, until PR #123
// re-pasted it by hand. This asserts the README's own claim: every non-blank
// golden row, byte for byte and in order, with the blank rows out.
func TestReadmeCouncilFrameMatchesItsGolden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile(filepath.Join("testdata", "golden", "activity.txt"))
	if err != nil {
		t.Fatal(err)
	}
	doc := strings.ReplaceAll(string(raw), "\r\n", "\n")

	// The blank rows are the room's empty scrollback — real in a live terminal,
	// dead weight in a README. Dropping them is the whole transform; trailing
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
		t.Errorf("README.md's council frame is stale.\n"+
			"Re-paste it from internal/council/testdata/golden/activity.txt with the all-blank rows removed.\n--- expected ---\n%s", want)
	}
}
