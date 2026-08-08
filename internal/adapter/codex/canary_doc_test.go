package codex

import (
	"os"
	"strings"
	"testing"
)

// designDoc is the canary inventory's home (§3.10).
const designDoc = "../../../docs/design.md"

// TestTheCanaryInventoryMatchesThisAdapter.
//
// §3.10 is a hand-maintained copy of a fact that lives in this file, and
// STATE.md's own opening warns that exactly this shape goes stale by the next
// merge — it has, three times, for other facts. The inventory was still worth
// writing: the question "what is being watched" is asked across adapters and
// answering it five subsections apart is how it went unanswered for a release.
// So it is written once and pinned here, beside the truth it copies.
//
// Asserted on the canary's own Name and on verifiedAgainst rather than on prose
// around them, because those two strings are what a reader re-verifying this
// vendor would go looking for.
func TestTheCanaryInventoryMatchesThisAdapter(t *testing.T) {
	b, err := os.ReadFile(designDoc)
	if err != nil {
		t.Fatalf("cannot read the design doc that documents this adapter: %v", err)
	}
	doc := string(b)

	if !strings.Contains(doc, verifiedAgainst) {
		t.Errorf("design.md §3.10 does not name the version this adapter is pinned to (%q)", verifiedAgainst)
	}
	for _, c := range []string{canaryEnvelopeType.Name, canarySessionMeta.Name} {
		if !strings.Contains(doc, c) {
			t.Errorf("design.md §3.10 does not name the %q canary; the inventory has drifted from the adapter", c)
		}
	}
}
