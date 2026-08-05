package council

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

func TestParseFlowChain(t *testing.T) {
	input := "@claude draft -> @codex review -> @agy publish docs/spec.md"
	chain, err := ParseFlowChain(input)
	if err != nil {
		t.Fatalf("ParseFlowChain failed: %v", err)
	}

	if len(chain.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(chain.Steps))
	}

	s1 := chain.Steps[0]
	if s1.Vendor != model.VendorID("claude") || s1.Verb != "draft" {
		t.Errorf("step 0 mismatch: %+v", s1)
	}

	s3 := chain.Steps[2]
	if s3.Vendor != model.VendorID("agy") || s3.Verb != "publish" || s3.Path != "docs/spec.md" {
		t.Errorf("step 2 mismatch: %+v", s3)
	}
}

func TestParseFlowChainWithSlashPrefix(t *testing.T) {
	input := "/flow @claude draft -> @codex review"
	chain, err := ParseFlowChain(input)
	if err != nil {
		t.Fatalf("ParseFlowChain failed: %v", err)
	}
	if len(chain.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(chain.Steps))
	}
}

func TestVerifyReceipt(t *testing.T) {
	tempDir := t.TempDir()
	relPath := "output.md"
	absPath := filepath.Join(tempDir, relPath)

	step := &FlowStep{
		Vendor: model.VendorID("agy"),
		Verb:   "publish",
		Path:   relPath,
		State:  FlowStatePublished,
	}

	// Before file exists
	r1 := VerifyReceipt(tempDir, step)
	if r1.Verified {
		t.Errorf("expected unverified before file creation, got verified: %+v", r1)
	}

	// Create file
	if err := os.WriteFile(absPath, []byte("# Final Output"), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// After file exists
	r2 := VerifyReceipt(tempDir, step)
	if !r2.Verified {
		t.Errorf("expected verified after file creation, got unverified: %+v", r2)
	}
}
