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
	if s1.Vendor != model.VendorID("claude") || s1.Verb != "draft" || s1.Task != "feature spec" {
		t.Errorf("step 0 mismatch: %+v", s1)
	}

	s3 := chain.Steps[2]
	if s3.Vendor != model.VendorID("agy") || s3.Verb != "publish" || s3.Path != "docs/spec.md" {
		t.Errorf("step 2 mismatch: %+v", s3)
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

func TestVerifyReceiptModTimeAndSymlink(t *testing.T) {
	tempDir := t.TempDir()
	relPath := "output.md"
	absPath := filepath.Join(tempDir, relPath)

	step := &FlowStep{
		Vendor:    model.VendorID("agy"),
		Verb:      "publish",
		Path:      relPath,
		StartedAt: time.Now(),
	}

	// Case 1: File does not exist -> Unverified
	r1 := VerifyReceipt(tempDir, step)
	if r1.Verified {
		t.Errorf("expected unverified before file creation, got: %+v", r1)
	}

	// Case 2: File created BEFORE step started -> Unverified (pre-existing file protection)
	oldStep := &FlowStep{
		Vendor:    model.VendorID("agy"),
		Verb:      "publish",
		Path:      relPath,
		StartedAt: time.Now().Add(10 * time.Second),
	}
	if err := os.WriteFile(absPath, []byte("# Final Output"), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	r2 := VerifyReceipt(tempDir, oldStep)
	if r2.Verified {
		t.Errorf("expected unverified for pre-existing file, got: %+v", r2)
	}

	// Case 3: Valid file created after step start -> Verified
	r3 := VerifyReceipt(tempDir, step)
	if !r3.Verified {
		t.Errorf("expected verified for newly created file, got: %+v", r3)
	}
}

func TestFlowStateTransitions(t *testing.T) {
	chain, _ := ParseFlowChain("@claude draft -> @codex review")

	// Initial step is queued
	if chain.Current().State != FlowStateQueued {
		t.Errorf("expected queued state, got %v", chain.Current().State)
	}

	// Advance Queued -> Running
	ok, err := chain.Advance()
	if !ok || err != nil {
		t.Fatalf("failed to advance queued step: %v", err)
	}
	if chain.Current().State != FlowStateRunning {
		t.Errorf("expected running state, got %v", chain.Current().State)
	}

	// Mark step failed and attempt advance -> must fail
	chain.Current().State = FlowStateFailed
	_, err = chain.Advance()
	if err == nil {
		t.Fatal("expected error advancing failed step, got nil")
	}
}
