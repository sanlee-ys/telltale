package council

import (
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

func TestAutoApproveBasicGit(t *testing.T) {
	allow := []string{
		"git status",
		"git add -A",
		"git commit -m fix",
		"git push origin HEAD",
		"git checkout -b feat/x",
		"git switch -c feat/y",
		"git log --oneline -5",
		"git -C /tmp/ws status",
		"gh pr create --title t --body b",
		"gh run list",
	}
	for _, cmd := range allow {
		g := &runner.Gate{Tool: "Bash", Input: map[string]any{"command": cmd}}
		if !autoApproveBasicGit(g) {
			t.Errorf("should auto-approve %q", cmd)
		}
	}
	deny := []string{
		"git push --force",
		"git push -f origin main",
		"git reset --hard",
		"git clean -fd",
		"git checkout README.md",
		"git commit --amend",
		"rm -rf /",
		"git status; rm -rf .",
		"Bash(something)",
	}
	for _, cmd := range deny {
		g := &runner.Gate{Tool: "Bash", Input: map[string]any{"command": cmd}}
		if autoApproveBasicGit(g) {
			t.Errorf("must not auto-approve %q", cmd)
		}
	}
	if autoApproveBasicGit(&runner.Gate{Tool: "Write", Input: map[string]any{"command": "git status"}}) {
		t.Error("non-Bash tools must not use the git auto-approver")
	}
}
