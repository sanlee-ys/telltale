package council

import (
	"os"
	"path/filepath"
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

	s1 := chain.Steps[0]
	if s1.Vendor != model.VendorClaude || s1.Verb != "draft" || s1.Task != "feature spec" {
		t.Errorf("step 0 mismatch: %+v", s1)
	}

	s3 := chain.Steps[2]
	if s3.Vendor != model.VendorAntigravity || s3.Verb != "publish" || s3.Path != "docs/spec.md" {
		t.Errorf("step 2 mismatch: %+v", s3)
	}
}

func TestParseFlowChainAcceptsAntigravityAlias(t *testing.T) {
	chain, err := ParseFlowChain("@claude draft -> @antigravity publish out.md")
	if err != nil {
		t.Fatalf("ParseFlowChain failed: %v", err)
	}
	if chain.Steps[1].Vendor != model.VendorAntigravity {
		t.Errorf("expected agy vendor for @antigravity, got %s", chain.Steps[1].Vendor)
	}
}

func TestParseFlowChainInvalidSeat(t *testing.T) {
	input := "@gemini draft -> @codex review"
	_, err := ParseFlowChain(input)
	if err == nil {
		t.Fatal("expected error for invalid seat @gemini, got nil")
	}
}

func TestParseFlowChainInvalidCommandPrefix(t *testing.T) {
	input := "/flower @claude draft -> @codex review"
	_, err := ParseFlowChain(input)
	if err == nil {
		t.Fatal("expected error for invalid prefix /flower, got nil")
	}
}

func TestVerifyReceiptPathlessNeverVerifies(t *testing.T) {
	step := &FlowStep{
		Vendor:    model.VendorClaude,
		Verb:      "draft",
		State:     FlowStatePublished, // narrated — must not matter
		StartedAt: time.Now(),
	}
	r := VerifyReceipt(t.TempDir(), step)
	if r.Verified {
		t.Fatalf("pathless step must not verify, even if State=published: %+v", r)
	}
}

func TestVerifyReceiptRequiresChangeFromBaseline(t *testing.T) {
	tempDir := t.TempDir()
	relPath := "output.md"
	absPath := filepath.Join(tempDir, relPath)

	if err := os.WriteFile(absPath, []byte("# pre-existing"), 0600); err != nil {
		t.Fatal(err)
	}

	step := &FlowStep{
		Vendor:    model.VendorAntigravity,
		Verb:      "publish",
		Path:      relPath,
		StartedAt: time.Now(),
	}
	captureBaseline(tempDir, step)
	if !step.BaselineExists {
		t.Fatal("expected baseline to see pre-existing file")
	}

	r1 := VerifyReceipt(tempDir, step)
	if r1.Verified {
		t.Fatalf("unchanged pre-existing file must not verify: %+v", r1)
	}

	if err := os.WriteFile(absPath, []byte("# published by agent"), 0600); err != nil {
		t.Fatal(err)
	}
	// Ensure mtime/size differ on coarse filesystems
	_ = os.Chtimes(absPath, time.Now(), time.Now().Add(2*time.Second))

	r2 := VerifyReceipt(tempDir, step)
	if !r2.Verified {
		t.Fatalf("changed file should verify: %+v", r2)
	}
}

func TestVerifyReceiptNewFileAfterStart(t *testing.T) {
	tempDir := t.TempDir()
	relPath := "new.md"
	step := &FlowStep{
		Vendor:    model.VendorAntigravity,
		Verb:      "publish",
		Path:      relPath,
		StartedAt: time.Now(),
	}
	captureBaseline(tempDir, step)
	if step.BaselineExists {
		t.Fatal("baseline should not exist yet")
	}

	r1 := VerifyReceipt(tempDir, step)
	if r1.Verified {
		t.Fatalf("missing file must not verify: %+v", r1)
	}

	if err := os.WriteFile(filepath.Join(tempDir, relPath), []byte("fresh"), 0600); err != nil {
		t.Fatal(err)
	}
	r2 := VerifyReceipt(tempDir, step)
	if !r2.Verified {
		t.Fatalf("new file should verify: %+v", r2)
	}
}

func TestFlowStateTransitions(t *testing.T) {
	chain, err := ParseFlowChain("@claude draft -> @codex review")
	if err != nil {
		t.Fatal(err)
	}

	if err := chain.Start(""); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if chain.Current().State != FlowStateRunning {
		t.Fatalf("expected running, got %s", chain.Current().State)
	}

	if _, err := chain.Advance(); err == nil {
		t.Fatal("Advance from Running must fail")
	}

	if err := chain.MarkApproved(); err != nil {
		t.Fatalf("MarkApproved: %v", err)
	}
	ok, err := chain.Advance()
	if !ok || err != nil {
		t.Fatalf("Advance after Approved: ok=%v err=%v", ok, err)
	}
	if chain.Current().Vendor != model.VendorCodex {
		t.Fatalf("expected codex hop, got %s", chain.Current().Vendor)
	}
	if chain.Current().State != FlowStateQueued {
		t.Fatalf("next hop should still be queued until Start, got %s", chain.Current().State)
	}
}

func TestMarkPublishedRequiresVerifiedReceipt(t *testing.T) {
	chain, _ := ParseFlowChain("@agy publish out.md -> @codex review")
	_ = chain.Start(t.TempDir())
	err := chain.MarkPublished(Receipt{Verified: false, Detail: "nope"})
	if err == nil {
		t.Fatal("MarkPublished must refuse unverified receipt")
	}
}
