package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

func TestArtifactStoreSaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewArtifactStore(tempDir)
	if err != nil {
		t.Fatalf("NewArtifactStore failed: %v", err)
	}

	sessionID := "test-session-123"
	turnN := 1
	vendor := model.VendorID("claude")
	content := "# Objective\nImplement v1 flow and artifact seam."

	path, err := store.SaveArtifact(sessionID, turnN, vendor, content)
	if err != nil {
		t.Fatalf("SaveArtifact failed: %v", err)
	}

	// Verify file is saved in tempDir and not relative to repo
	if !strings.HasPrefix(path, tempDir) {
		t.Errorf("expected path to start with %s, got %s", tempDir, path)
	}

	loaded, err := store.LoadArtifact(sessionID, turnN, vendor)
	if err != nil {
		t.Fatalf("LoadArtifact failed: %v", err)
	}

	if loaded != content {
		t.Errorf("expected loaded content %q, got %q", content, loaded)
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

func TestArtifactStoreHomeResolution(t *testing.T) {
	store, err := NewArtifactStore("")
	if err != nil {
		t.Fatalf("NewArtifactStore with default home failed: %v", err)
	}
	home, _ := os.UserHomeDir()
	expectedBase := filepath.Join(home, ".telltale", "council", "artifacts")
	if store.baseDir != expectedBase {
		t.Errorf("expected base dir %s, got %s", expectedBase, store.baseDir)
	}
}
