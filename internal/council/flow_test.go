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
	input := "@claude draft feature spec -> @codex review security -> @agy publish docs/spec.md"
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

func TestWriteGateBeforeStart(t *testing.T) {
	chain, _ := ParseFlowChain("@agy publish docs/x.md -> @codex review")
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
	chain, _ := ParseFlowChain("@agy publish out.md -> @codex review")
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
