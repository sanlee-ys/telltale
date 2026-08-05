package council

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// FlowStepState represents the empirical, measured status of a workflow step.
type FlowStepState uint8

const (
	FlowStateQueued FlowStepState = iota
	FlowStateRunning
	FlowStateBlocked
	FlowStateApproved
	FlowStatePublished
	FlowStateFailed
)

func (s FlowStepState) String() string {
	switch s {
	case FlowStateQueued:
		return "queued"
	case FlowStateRunning:
		return "running"
	case FlowStateBlocked:
		return "blocked"
	case FlowStateApproved:
		return "approved"
	case FlowStatePublished:
		return "published"
	case FlowStateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Receipt holds empirical evidence verifying a step's completion.
type Receipt struct {
	Verified bool
	Detail   string
}

// FlowStep represents one hop in an orchestrated workflow chain.
type FlowStep struct {
	Vendor    model.VendorID
	Verb      string
	Task      string // Full preserved task instruction text
	State     FlowStepState
	Path      string // Optional target file path for write/publish steps
	StartedAt time.Time
	Receipt   Receipt
}

// FlowChain is an ordered pipeline of steps.
type FlowChain struct {
	Steps        []FlowStep
	CurrentIndex int
}

var validSeats = map[model.VendorID]bool{
	"claude": true,
	"codex":  true,
	"agy":    true,
	"cursor": true,
}

// ParseFlowChain parses arrow-delimited workflow instructions.
// Example: "@claude draft feature spec -> @codex review security -> @agy publish docs/spec.md"
// or "/flow @claude draft -> @codex review -> @agy publish"
func ParseFlowChain(input string) (*FlowChain, error) {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "/flow") {
		rest := input[len("/flow"):]
		if rest != "" && !strings.HasPrefix(rest, " ") && !strings.HasPrefix(rest, "\t") {
			return nil, fmt.Errorf("invalid command prefix %q: expected '/flow '", input)
		}
		input = strings.TrimSpace(rest)
	}
	if input == "" {
		return nil, errors.New("empty flow instruction")
	}

	rawHops := strings.Split(input, "->")
	if len(rawHops) < 2 {
		return nil, errors.New("flow chain must contain at least 2 hops separated by '->'")
	}

	var steps []FlowStep
	for _, raw := range rawHops {
		raw = strings.TrimSpace(raw)
		parts := strings.Fields(raw)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid hop format %q: expected '@seat verb [task/path]'", raw)
		}

		seatStr := parts[0]
		if !strings.HasPrefix(seatStr, "@") {
			return nil, fmt.Errorf("invalid seat %q in hop: seat must start with '@'", seatStr)
		}

		vendorID := model.VendorID(strings.TrimPrefix(seatStr, "@"))
		if !validSeats[vendorID] {
			return nil, fmt.Errorf("unknown vendor seat @%s in flow chain (valid seats: @claude, @codex, @agy, @cursor)", vendorID)
		}

		verb := strings.ToLower(parts[1])

		// Preserve all remaining text after seat and verb as task/path
		task := ""
		path := ""
		if len(parts) >= 3 {
			task = strings.Join(parts[2:], " ")
			// If the last token looks like a file path (has extension or slash), set path
			lastToken := parts[len(parts)-1]
			if strings.Contains(lastToken, ".") || strings.Contains(lastToken, "/") || strings.Contains(lastToken, "\\") {
				path = lastToken
			}
		}

		steps = append(steps, FlowStep{
			Vendor: vendorID,
			Verb:   verb,
			Task:   task,
			State:  FlowStateQueued,
			Path:   path,
		})
	}

	return &FlowChain{
		Steps:        steps,
		CurrentIndex: 0,
	}, nil
}

// Current returns the step currently active or queued.
func (fc *FlowChain) Current() *FlowStep {
	if fc.CurrentIndex < 0 || fc.CurrentIndex >= len(fc.Steps) {
		return nil
	}
	return &fc.Steps[fc.CurrentIndex]
}

// Advance strictly validates state transitions before moving to the next hop.
func (fc *FlowChain) Advance() (bool, error) {
	curr := fc.Current()
	if curr == nil {
		return false, errors.New("no active step to advance")
	}

	// Enforce state machine transition invariants
	switch curr.State {
	case FlowStateFailed:
		return false, fmt.Errorf("cannot advance flow chain: step %d (@%s %s) failed", fc.CurrentIndex+1, curr.Vendor, curr.Verb)
	case FlowStateBlocked:
		return false, fmt.Errorf("cannot advance flow chain: step %d (@%s %s) is blocked awaiting user approval", fc.CurrentIndex+1, curr.Vendor, curr.Verb)
	case FlowStateQueued:
		// Transition Queued -> Running
		curr.State = FlowStateRunning
		curr.StartedAt = time.Now()
		return true, nil
	case FlowStateRunning, FlowStateApproved, FlowStatePublished:
		if fc.CurrentIndex < len(fc.Steps)-1 {
			fc.CurrentIndex++
			fc.Steps[fc.CurrentIndex].State = FlowStateRunning
			fc.Steps[fc.CurrentIndex].StartedAt = time.Now()
			return true, nil
		}
		return false, nil
	default:
		return false, fmt.Errorf("invalid step state %v", curr.State)
	}
}

// VerifyReceipt checks for empirical evidence on disk before declaring a step published/done.
// It verifies:
// 1. Target path exists inside workspace boundary (evaluating symlinks).
// 2. File size is non-zero.
// 3. File modification time is equal to or after the step's StartedAt timestamp (preventing pre-existing files from passing).
func VerifyReceipt(workspace string, step *FlowStep) Receipt {
	if step == nil {
		return Receipt{Verified: false, Detail: "nil step"}
	}

	if step.Path == "" {
		if step.State == FlowStatePublished {
			return Receipt{Verified: true, Detail: "step marked published cleanly"}
		}
		return Receipt{Verified: false, Detail: "no target path specified and step unpublished"}
	}

	targetPath := step.Path
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(workspace, targetPath)
	}

	// Symlink and boundary evaluation
	evalWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		evalWorkspace = workspace
	}

	evalTarget, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		return Receipt{Verified: false, Detail: fmt.Sprintf("target path %s does not exist or symlink is broken", step.Path)}
	}

	rel, err := filepath.Rel(evalWorkspace, evalTarget)
	if err != nil || strings.HasPrefix(rel, "..") {
		return Receipt{Verified: false, Detail: fmt.Sprintf("target path %s resolves outside workspace boundary (%s)", step.Path, evalTarget)}
	}

	info, err := os.Stat(evalTarget)
	if err != nil {
		return Receipt{Verified: false, Detail: fmt.Sprintf("file %s not found on disk", step.Path)}
	}

	if info.Size() == 0 {
		return Receipt{Verified: false, Detail: fmt.Sprintf("file %s exists but is 0 bytes", step.Path)}
	}

	// Verify file modification time is on or after step start time (with 1-second margin for filesystem clock granularity)
	if !step.StartedAt.IsZero() {
		if info.ModTime().Before(step.StartedAt.Add(-1 * time.Second)) {
			return Receipt{
				Verified: false,
				Detail:   fmt.Sprintf("file %s modtime (%s) predates step start time (%s)", step.Path, info.ModTime().Format(time.RFC3339), step.StartedAt.Format(time.RFC3339)),
			}
		}
	}

	return Receipt{
		Verified: true,
		Detail:   fmt.Sprintf("verified file %s on disk (%d bytes, modtime %s)", step.Path, info.Size(), info.ModTime().Format(time.RFC3339)),
	}
}
