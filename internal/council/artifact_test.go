package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

func TestArtifactStoreSaveAndLoadInTempDir(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewArtifactStoreWithBaseDir(tempDir)
	if err != nil {
		t.Fatalf("NewArtifactStoreWithBaseDir failed: %v", err)
	}

	sessionID := "test-session-123"
	turnN := 1
	vendor := model.VendorID("claude")
	content := "# Objective\nImplement v1 flow and artifact seam with sk-ant-api-key-1234567890123456."
	prompt := "draft v1 spec for sk-ant-secret-key-1234567890123456"

	path, err := store.SaveArtifact(sessionID, turnN, vendor, content, prompt)
	if err != nil {
		t.Fatalf("SaveArtifact failed: %v", err)
	}

	// Verify file is saved strictly inside tempDir and NOT in user home
	if !strings.HasPrefix(path, tempDir) {
		t.Errorf("expected path to start with temp dir %s, got %s", tempDir, path)
	}

	loaded, err := store.LoadArtifact(sessionID, turnN, vendor)
	if err != nil {
		t.Fatalf("LoadArtifact failed: %v", err)
	}

	// Verify secret redaction in body; prompt stored as fingerprint only
	if strings.Contains(loaded, "sk-ant-api-key-1234567890123456") {
		t.Errorf("secret in content was not redacted: %s", loaded)
	}
	if strings.Contains(loaded, "sk-ant-secret-key-1234567890123456") {
		t.Errorf("secret in prompt must not be stored: %s", loaded)
	}
	if !strings.Contains(loaded, "«redacted»") {
		t.Errorf("expected redacted marker in artifact body: %s", loaded)
	}
	if !strings.Contains(loaded, "PromptSHA256-8:") {
		t.Errorf("expected prompt fingerprint header: %s", loaded)
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

func TestDefaultArtifactStorePath(t *testing.T) {
	store, err := NewArtifactStore()
	if err != nil {
		t.Fatalf("NewArtifactStore failed: %v", err)
	}
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".telltale", "council", "artifacts")
	if store.baseDir != expected {
		t.Errorf("expected default baseDir %s, got %s", expected, store.baseDir)
	}
}

func TestArtifactRefuseOverwrite(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewArtifactStoreWithBaseDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveArtifact("s", 1, model.VendorClaude, "once", "p")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveArtifact("s", 1, model.VendorClaude, "twice", "p")
	if err == nil {
		t.Fatal("expected overwrite refusal")
	}
}
