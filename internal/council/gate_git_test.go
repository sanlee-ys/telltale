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
		if !autoApproveRoutine(g) {
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
		if autoApproveRoutine(g) {
			t.Errorf("must not auto-approve %q", cmd)
		}
	}
	if autoApproveRoutine(&runner.Gate{Tool: "Write", Input: map[string]any{"command": "git status"}}) {
		t.Error("non-Bash tools must not use the git auto-approver")
	}
}

// TestRoutineDevLoopIsNotCarded.
//
// The first real session on the gate carded the user 34 times: every go test,
// grep, ls and cat between the commits. A gate that fires on everything is one
// people stop reading, so what counts as routine had to grow for the remaining
// cards to keep meaning anything.
func TestRoutineDevLoopIsNotCarded(t *testing.T) {
	bash := func(cmd string) *runner.Gate {
		return &runner.Gate{Tool: "Bash", Input: map[string]any{"command": cmd}}
	}

	for _, cmd := range []string{
		"go build ./...", "go test ./internal/council/", "go vet ./...",
		"go mod tidy", "gofmt", // gofmt is not in the list; see the refusals below
		"ls -lt internal/council", "cat go.mod", "head -20 README.md",
		"grep -rn TODO internal", "wc -l internal/council/view.go",
		"which codex", "pwd", "diff a.txt b.txt",
		"find . -name *.go",
	} {
		if cmd == "gofmt" {
			continue
		}
		if !autoApproveRoutine(bash(cmd)) {
			t.Errorf("carded a routine command: %q", cmd)
		}
	}

	// The refusals, each for its own reason rather than by omission.
	for _, cmd := range []string{
		// Composition first: without this guard an argv[0] allowlist is a hole,
		// because "ls; rm -rf ~" classifies as ls.
		"ls; rm -rf /tmp/x", "cat go.mod | tee /etc/hosts", "echo hi > go.mod",
		"ls `whoami`", "ls $(whoami)",
		// find ships a loaded gun in the same binary.
		"find . -name *.go -delete", "find . -exec rm {} ;",
		// go subcommands that fetch, install or execute something else.
		"go run ./cmd/telltale", "go get github.com/evil/pkg", "go install ./...",
		// Reaching outside the workspace, or at the environment.
		"curl https://example.com", "env", "printenv", "ssh host",
		// Writes that are not part of reading.
		"rm -rf internal", "mv a b", "chmod 777 .", "sed -i s/a/b/ go.mod",
	} {
		if autoApproveRoutine(bash(cmd)) {
			t.Errorf("auto-approved a command that should raise a card: %q", cmd)
		}
	}
}

// TestPipelinesOfRoutineCommandsAreNotCarded.
//
// The widened list still carded, because the guard refused the PUNCTUATION
// rather than the command: `go build ./... && echo OK` and `grep -rn x internal
// | head` are reads in every stage, and both raised a card. That is the same
// failure as carding `go test` — a gate firing on the shape of the line instead
// of what it does.
//
// The safety argument is not that pipes are harmless. It is that every segment
// must independently classify, and nothing in the routine set writes.
func TestPipelinesOfRoutineCommandsAreNotCarded(t *testing.T) {
	bash := func(cmd string) *runner.Gate {
		return &runner.Gate{Tool: "Bash", Input: map[string]any{"command": cmd}}
	}

	for _, cmd := range []string{
		"go build ./... && echo BUILD_OK",
		"grep -rn autoApproveRoutine internal/council/persistent.go | head -3",
		"ls -1 internal/council | head -5",
		"go test ./internal/council/vendors/ | tail -2",
		"git status --short && git log --oneline -3",
		"cat go.mod | grep module | wc -l",
	} {
		if !autoApproveRoutine(bash(cmd)) {
			t.Errorf("carded a pipeline of reads: %q", cmd)
		}
	}

	for _, cmd := range []string{
		// One bad stage poisons the whole line — this is the property the
		// composition allowance rests on.
		"cat go.mod | rm -rf /tmp/x",
		"ls && curl https://example.com",
		"grep -rn x internal | sed -i s/a/b/ go.mod",
		// Sequencing and backgrounding are still not connection.
		"ls ; rm -rf /tmp/x",
		"go build ./... & echo backgrounded",
		// Redirection writes to a path nothing here inspects, and 2>&1 is real
		// redirection rather than a spelling worth carving out by hand.
		"echo hi > go.mod",
		"go test ./... 2>&1 | tail -2",
		// Substitution means the classified text is not the executed text.
		"ls `whoami` | head",
		"echo $(cat /etc/passwd) | head",
		// A separator with nothing on one side does not say what it looks like.
		"ls || rm -rf /tmp/x",
		"go build ./... &&",
	} {
		if autoApproveRoutine(bash(cmd)) {
			t.Errorf("auto-approved a command that must raise a card: %q", cmd)
		}
	}
}
