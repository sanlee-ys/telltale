package council

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	Vendor  model.VendorID
	Verb    string
	State   FlowStepState
	Path    string // Optional target file path for write/publish steps
	Receipt Receipt
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
	"gemini": true,
}

// ParseFlowChain parses arrow-delimited workflow instructions.
// Example: "@claude draft -> @codex review -> @agy publish docs/spec.md"
// or "/flow @claude draft -> @codex review -> @agy publish docs/spec.md"
func ParseFlowChain(input string) (*FlowChain, error) {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "/flow") {
		input = strings.TrimSpace(strings.TrimPrefix(input, "/flow"))
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
			return nil, fmt.Errorf("invalid hop format %q: expected '@seat verb [path]'", raw)
		}

		seatStr := parts[0]
		if !strings.HasPrefix(seatStr, "@") {
			return nil, fmt.Errorf("invalid seat %q in hop: seat must start with '@'", seatStr)
		}

		vendorID := model.VendorID(strings.TrimPrefix(seatStr, "@"))
		if !validSeats[vendorID] {
			return nil, fmt.Errorf("unknown vendor seat @%s in flow chain", vendorID)
		}

		verb := strings.ToLower(parts[1])
		path := ""
		if len(parts) >= 3 {
			path = parts[2]
		}

		steps = append(steps, FlowStep{
			Vendor: vendorID,
			Verb:   verb,
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

// Advance moves the chain to the next hop if the current step is completed/approved.
func (fc *FlowChain) Advance() bool {
	if fc.CurrentIndex < len(fc.Steps)-1 {
		fc.CurrentIndex++
		fc.Steps[fc.CurrentIndex].State = FlowStateRunning
		return true
	}
	return false
}

// VerifyReceipt checks for empirical evidence on disk before declaring a step published/done.
func VerifyReceipt(workspace string, step *FlowStep) Receipt {
	if step == nil {
		return Receipt{Verified: false, Detail: "nil step"}
	}

	if step.Path == "" {
		if step.State == FlowStatePublished {
			return Receipt{Verified: true, Detail: "step completed without target file requirement"}
		}
		return Receipt{Verified: false, Detail: "no target path specified and step not published"}
	}

	targetPath := step.Path
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(workspace, targetPath)
	}

	// Boundary check: targetPath must be within workspace
	rel, err := filepath.Rel(workspace, targetPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return Receipt{Verified: false, Detail: fmt.Sprintf("target path %s is outside workspace boundary", step.Path)}
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return Receipt{Verified: false, Detail: fmt.Sprintf("file %s not found on disk", step.Path)}
	}

	if info.Size() == 0 {
		return Receipt{Verified: false, Detail: fmt.Sprintf("file %s exists but is 0 bytes", step.Path)}
	}

	return Receipt{
		Verified: true,
		Detail:   fmt.Sprintf("verified file %s on disk (%d bytes)", step.Path, info.Size()),
	}
}
