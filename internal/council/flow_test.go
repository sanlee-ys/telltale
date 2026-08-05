package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

func TestParseFlowChainValid(t *testing.T) {
	input := "@claude draft feature spec -> @codex review security -> @agy publish write:docs/spec.md"
	chain, err := ParseFlowChain(input)
	if err != nil {
		t.Fatalf("ParseFlowChain failed: %v", err)
	}
	if len(chain.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(chain.Steps))
	}
	if !chain.Steps[2].RequiresWriteGate() {
		t.Fatal("publish with path must require write gate")
	}
	if chain.Steps[0].RequiresWriteGate() {
		t.Fatal("draft without path must not require write gate")
	}
}

func TestParseFlowChainRejectsMerge(t *testing.T) {
	_, err := ParseFlowChain("@claude draft -> @agy merge")
	if err == nil {
		t.Fatal("expected merge verb rejected")
	}
}

func TestParseFlowChainInvalidSeat(t *testing.T) {
	_, err := ParseFlowChain("@gemini draft -> @codex review")
	if err == nil {
		t.Fatal("expected error for @gemini")
	}
}

func TestParseFlowChainInvalidCommandPrefix(t *testing.T) {
	_, err := ParseFlowChain("/flower @claude draft -> @codex review")
	if err == nil {
		t.Fatal("expected error for /flower")
	}
}

func TestVerifyReceiptPathlessNeverVerifies(t *testing.T) {
	step := &FlowStep{
		Vendor:    model.VendorClaude,
		Verb:      "draft",
		State:     FlowStatePublished,
		StartedAt: time.Now(),
	}
	r := VerifyReceipt(t.TempDir(), step)
	if r.Verified {
		t.Fatalf("pathless must not verify: %+v", r)
	}
}

func TestVerifyReceiptRequiresChangeFromBaseline(t *testing.T) {
	tempDir := t.TempDir()
	relPath := "output.md"
	absPath := filepath.Join(tempDir, relPath)
	if err := os.WriteFile(absPath, []byte("# pre-existing"), 0600); err != nil {
		t.Fatal(err)
	}
	step := &FlowStep{Vendor: model.VendorAntigravity, Verb: "publish", Path: relPath, StartedAt: time.Now()}
	captureBaseline(tempDir, step)
	if VerifyReceipt(tempDir, step).Verified {
		t.Fatal("unchanged pre-existing must not verify")
	}
	if err := os.WriteFile(absPath, []byte("# published"), 0600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(absPath, time.Now(), time.Now().Add(2*time.Second))
	if !VerifyReceipt(tempDir, step).Verified {
		t.Fatal("changed file should verify")
	}
}

func TestFlowReturnedNotApproved(t *testing.T) {
	chain, err := ParseFlowChain("@claude draft -> @codex review")
	if err != nil {
		t.Fatal(err)
	}
	if err := chain.Start(""); err != nil {
		t.Fatal(err)
	}
	if _, err := chain.Advance(); err == nil {
		t.Fatal("Advance from Running must fail")
	}
	if err := chain.MarkReturned(); err != nil {
		t.Fatal(err)
	}
	if chain.Current().State != FlowStateReturned {
		t.Fatalf("got %s", chain.Current().State)
	}
	ok, err := chain.Advance()
	if !ok || err != nil {
		t.Fatalf("Advance after Returned: %v", err)
	}
}

// TestMarkFailedCannotRewriteAFinishedHop pins the guard that keeps a late
// failure from denying work that already happened.
//
// finishFlowHop reads the artifact back AFTER the hop has been marked, so an
// unguarded MarkFailed would flip a Published step — one holding a verified
// receipt for a mutation still sitting in the tree — to Failed{Verified:false}.
// The room would then report that a write the user authorized had failed.
func TestMarkFailedCannotRewriteAFinishedHop(t *testing.T) {
	// Running is a legitimate source: that is where the save/receipt failures
	// this method exists for are detected.
	chain, err := ParseFlowChain("@claude draft -> @codex review")
	if err != nil {
		t.Fatal(err)
	}
	if err := chain.Start(""); err != nil {
		t.Fatal(err)
	}
	if err := chain.MarkFailed("artifact save exploded"); err != nil {
		t.Fatalf("MarkFailed from Running must succeed: %v", err)
	}
	if chain.Current().State != FlowStateFailed {
		t.Fatalf("got %s", chain.Current().State)
	}

	// Returned is terminal. A later failure stops the chain instead.
	returned, _ := ParseFlowChain("@claude draft -> @codex review")
	_ = returned.Start("")
	if err := returned.MarkReturned(); err != nil {
		t.Fatal(err)
	}
	if err := returned.MarkFailed("artifact load: disk gone"); err == nil {
		t.Fatal("MarkFailed from Returned must be refused")
	}
	if returned.Current().State != FlowStateReturned {
		t.Fatalf("Returned was overwritten: got %s", returned.Current().State)
	}

	// Published is the case that costs something: the receipt must survive.
	published, _ := ParseFlowChain("@agy publish write:out.md -> @codex review")
	_ = published.Start(t.TempDir())
	if err := published.MarkPublished(Receipt{Verified: true, Detail: "created out.md"}); err != nil {
		t.Fatal(err)
	}
	if err := published.MarkFailed("artifact load: disk gone"); err == nil {
		t.Fatal("MarkFailed from Published must be refused")
	}
	if published.Current().State != FlowStatePublished {
		t.Fatalf("Published was overwritten: got %s", published.Current().State)
	}
	if !published.Current().Receipt.Verified {
		t.Fatal("verified receipt was destroyed by a late failure")
	}
}

func TestWriteGateBeforeStart(t *testing.T) {
	chain, _ := ParseFlowChain("@agy publish write:docs/x.md -> @codex review")
	if err := chain.MarkAwaitingWrite("awaiting auth"); err != nil {
		t.Fatal(err)
	}
	if err := chain.Start(""); err == nil {
		t.Fatal("Start from Blocked must fail")
	}
	if err := chain.ClearBlockForStart(); err != nil {
		t.Fatal(err)
	}
	if err := chain.Start(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestMarkPublishedRequiresVerifiedReceipt(t *testing.T) {
	chain, _ := ParseFlowChain("@agy publish write:out.md -> @codex review")
	_ = chain.Start(t.TempDir())
	if err := chain.MarkPublished(Receipt{Verified: false}); err == nil {
		t.Fatal("expected refusal")
	}
}

func TestArtifactPromptIsFingerprint(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewArtifactStoreWithBaseDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.SaveArtifact("s", 1, model.VendorClaude, "body", "secret brief text sk-ant-abcdefghijklmnopqrst")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	if strings.Contains(s, "secret brief text") {
		t.Fatal("full prompt must not be stored")
	}
	if !strings.Contains(s, "PromptSHA256-8:") {
		t.Fatal("expected fingerprint header")
	}
}

// TestWriteAuthorityIsNeverInferredFromProse is blocker 2 in one table.
//
// The shipped parser promoted a hop to a write hop when its last token
// contained '.', '/' or '\'. Every case below was therefore write authority
// handed out by punctuation — a sentence that ended in a period, a task that
// named a file it was only meant to READ, a Windows path quoted in a question.
func TestWriteAuthorityIsNeverInferredFromProse(t *testing.T) {
	for _, task := range []string{
		"review docs/spec.md",
		"summarize the auth flow.",
		`explain C:\Users\me\notes.txt`,
		"compare v1.2 and v1.3",
		"read ./README",
	} {
		chain, err := ParseFlowChain("@codex review " + task + " -> @claude summarize")
		if err != nil {
			t.Fatalf("%q: %v", task, err)
		}
		step := chain.Steps[0]
		if step.RequiresWriteGate() {
			t.Errorf("%q was granted write authority by punctuation (path=%q)", task, step.Path)
		}
		if step.Path != "" {
			t.Errorf("%q left a target path %q on a read hop", task, step.Path)
		}
	}
}

// TestDeclaredWriteTargetLeavesTheTaskIntact: the token is authority, not text.
// Whatever remains after it is removed is what the seat is actually asked.
func TestDeclaredWriteTargetLeavesTheTaskIntact(t *testing.T) {
	chain, err := ParseFlowChain("@agy publish the reviewed spec write:docs/spec.md now -> @claude check")
	if err != nil {
		t.Fatal(err)
	}
	step := chain.Steps[0]
	if step.Path != "docs/spec.md" {
		t.Errorf("path = %q", step.Path)
	}
	if step.Task != "the reviewed spec now" {
		t.Errorf("task = %q — the write: token must not survive into the prompt", step.Task)
	}
	if !step.RequiresWriteGate() {
		t.Error("a declared target must require the gate")
	}
}

// TestParseRejectsIllegalWriteTargets refuses at PARSE time, which is the last
// moment the answer is free: after this the seat is spawned with authority.
func TestParseRejectsIllegalWriteTargets(t *testing.T) {
	for _, bad := range []string{
		"@agy publish write: -> @claude check",                  // empty target
		"@agy publish write:/etc/shadow -> @claude check",       // unix-absolute
		`@agy publish write:C:\Windows\x.ini -> @claude check`,  // windows-absolute
		"@agy publish write:../outside.md -> @claude check",     // traversal
		`@agy publish write:docs\..\..\out.md -> @claude check`, // traversal, backslash
		"@agy publish write:a/../../b.md -> @claude check",      // traversal, mid-path
		"@agy publish write:a.md write:b.md -> @claude check",   // two targets
		"@agy write:a.md -> @claude check",                      // target in the verb slot
	} {
		if _, err := ParseFlowChain(bad); err == nil {
			t.Errorf("accepted an illegal write target: %q", bad)
		}
	}
}
