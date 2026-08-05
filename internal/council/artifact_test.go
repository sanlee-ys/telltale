package council

import (
	"os"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

func TestArtifactStoreSaveAndLoad(t *testing.T) {
	store, err := NewArtifactStore()
	if err != nil {
		t.Fatalf("NewArtifactStore failed: %v", err)
	}

	sessionID := "test-session-123"
	turnN := 1
	vendor := model.VendorID("claude")
	content := "# Objective\nImplement v1 flow and artifact seam."
	prompt := "draft v1 spec"

	path, err := store.SaveArtifact(sessionID, turnN, vendor, content, prompt)
	if err != nil {
		t.Fatalf("SaveArtifact failed: %v", err)
	}

	// Verify file is saved in user home dir
	home, _ := os.UserHomeDir()
	if !strings.HasPrefix(path, home) {
		t.Errorf("expected path to start with home dir %s, got %s", home, path)
	}

	loaded, err := store.LoadArtifact(sessionID, turnN, vendor)
	if err != nil {
		t.Fatalf("LoadArtifact failed: %v", err)
	}

	if !strings.Contains(loaded, content) {
		t.Errorf("expected loaded content to contain %q, got %q", content, loaded)
	}
	if !strings.Contains(loaded, "SessionID: test-session-123") {
		t.Errorf("expected provenance metadata in artifact, got: %s", loaded)
	}
}

func TestFormatFencedArtifact(t *testing.T) {
	fenced := FormatFencedArtifact("Claude Code", 2, "Draft content")
	if !strings.Contains(fenced, "--- artifact turn-2 from Claude Code") {
		t.Errorf("missing expected header in fenced output: %s", fenced)
	}
	if !strings.Contains(fenced, "--- end artifact turn-2 from Claude Code ---") {
		t.Errorf("missing expected footer in fenced output: %s", fenced)
	}
}
