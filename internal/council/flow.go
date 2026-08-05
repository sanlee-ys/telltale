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
// Column PhaseDone is turn state; these values are flow state — never derived
// from a model claiming it finished.
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

// Receipt holds empirical evidence verifying a publish/write hop.
// Verified is never true merely because State says so.
type Receipt struct {
	Verified bool
	Detail   string
}

// FlowStep represents one hop in an orchestrated workflow chain.
type FlowStep struct {
	Vendor    model.VendorID
	Verb      string
	Task      string
	State     FlowStepState
	Path      string // Optional target file path for write/publish steps
	StartedAt time.Time
	Receipt   Receipt

	// Baseline* captured when the hop enters Running. Publish receipts require
	// the target to appear or change relative to this snapshot — existence alone
	// of a pre-existing file is not evidence.
	BaselineExists bool
	BaselineSize   int64
	BaselineMod    time.Time
}

// FlowChain is an ordered pipeline of steps.
type FlowChain struct {
	Steps        []FlowStep
	CurrentIndex int
}

// ParseFlowChain parses arrow-delimited workflow instructions.
// Requires at least two hops. The "/flow " prefix is optional here; dispatch
// requires it so ordinary prose containing "->" never becomes a chain.
//
// Example: "@claude draft feature spec -> @codex review security -> @agy publish docs/spec.md"
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

	aliases := mentionAliases()
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

		name := strings.ToLower(strings.TrimPrefix(seatStr, "@"))
		vendorID, ok := aliases[name]
		if !ok || allAliases[name] {
			return nil, fmt.Errorf("unknown vendor seat @%s in flow chain (valid seats: @claude, @codex, @agy, @cursor)", name)
		}

		verb := strings.ToLower(parts[1])

		task := ""
		path := ""
		if len(parts) >= 3 {
			task = strings.Join(parts[2:], " ")
			lastToken := parts[len(parts)-1]
			if strings.ContainsAny(lastToken, "./\\") {
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
	if fc == nil || fc.CurrentIndex < 0 || fc.CurrentIndex >= len(fc.Steps) {
		return nil
	}
	return &fc.Steps[fc.CurrentIndex]
}

// Start moves the current Queued hop to Running and captures a path baseline.
func (fc *FlowChain) Start(workspace string) error {
	curr := fc.Current()
	if curr == nil {
		return errors.New("no active step to start")
	}
	if curr.State != FlowStateQueued {
		return fmt.Errorf("cannot start step in state %s", curr.State)
	}
	curr.State = FlowStateRunning
	curr.StartedAt = time.Now()
	captureBaseline(workspace, curr)
	return nil
}

// MarkApproved records that the current hop finished in a way the harness observed
// (seat PhaseDone for draft/review). It does not verify disk publication.
func (fc *FlowChain) MarkApproved() error {
	curr := fc.Current()
	if curr == nil {
		return errors.New("no active step")
	}
	if curr.State != FlowStateRunning && curr.State != FlowStateBlocked {
		return fmt.Errorf("cannot approve step in state %s", curr.State)
	}
	curr.State = FlowStateApproved
	return nil
}

// MarkBlocked parks a write/publish hop awaiting a user gate or a disk receipt.
func (fc *FlowChain) MarkBlocked(detail string) error {
	curr := fc.Current()
	if curr == nil {
		return errors.New("no active step")
	}
	if curr.State != FlowStateRunning {
		return fmt.Errorf("cannot block step in state %s", curr.State)
	}
	curr.State = FlowStateBlocked
	curr.Receipt = Receipt{Verified: false, Detail: detail}
	return nil
}

// MarkPublished sets Published only when VerifyReceipt already succeeded.
func (fc *FlowChain) MarkPublished(receipt Receipt) error {
	curr := fc.Current()
	if curr == nil {
		return errors.New("no active step")
	}
	if !receipt.Verified {
		return errors.New("cannot mark published without a verified receipt")
	}
	if curr.State != FlowStateBlocked && curr.State != FlowStateRunning {
		return fmt.Errorf("cannot publish step in state %s", curr.State)
	}
	curr.Receipt = receipt
	curr.State = FlowStatePublished
	return nil
}

// Advance moves to the next hop only from Approved or Published.
// Running/Blocked/Failed/Queued cannot skip forward — that was the narrated-status bug.
func (fc *FlowChain) Advance() (bool, error) {
	curr := fc.Current()
	if curr == nil {
		return false, errors.New("no active step to advance")
	}

	switch curr.State {
	case FlowStateFailed:
		return false, fmt.Errorf("cannot advance: step %d (@%s %s) failed", fc.CurrentIndex+1, curr.Vendor, curr.Verb)
	case FlowStateBlocked:
		return false, fmt.Errorf("cannot advance: step %d (@%s %s) is blocked", fc.CurrentIndex+1, curr.Vendor, curr.Verb)
	case FlowStateQueued, FlowStateRunning:
		return false, fmt.Errorf("cannot advance: step %d (@%s %s) is still %s — approve or publish first", fc.CurrentIndex+1, curr.Vendor, curr.Verb, curr.State)
	case FlowStateApproved, FlowStatePublished:
		if fc.CurrentIndex >= len(fc.Steps)-1 {
			return false, nil
		}
		fc.CurrentIndex++
		return true, nil
	default:
		return false, fmt.Errorf("invalid step state %v", curr.State)
	}
}

func captureBaseline(workspace string, step *FlowStep) {
	if step.Path == "" || workspace == "" {
		return
	}
	target := step.Path
	if !filepath.IsAbs(target) {
		target = filepath.Join(workspace, target)
	}
	eval, err := filepath.EvalSymlinks(target)
	if err != nil {
		step.BaselineExists = false
		return
	}
	info, err := os.Stat(eval)
	if err != nil {
		step.BaselineExists = false
		return
	}
	step.BaselineExists = true
	step.BaselineSize = info.Size()
	step.BaselineMod = info.ModTime()
}

// VerifyReceipt checks disk evidence for a write/publish hop.
// Never returns Verified for a pathless step. Never treats State as evidence.
// A pre-existing unchanged file does not verify; the file must be new or changed
// relative to the Running baseline.
func VerifyReceipt(workspace string, step *FlowStep) Receipt {
	if step == nil {
		return Receipt{Verified: false, Detail: "nil step"}
	}
	if step.Path == "" {
		return Receipt{Verified: false, Detail: "step has no target path; cannot verify disk receipt"}
	}
	if step.StartedAt.IsZero() {
		return Receipt{Verified: false, Detail: "step was never started; no baseline"}
	}

	targetPath := step.Path
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(workspace, targetPath)
	}

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
		return Receipt{Verified: false, Detail: fmt.Sprintf("target path %s resolves outside workspace (%s)", step.Path, evalTarget)}
	}

	info, err := os.Stat(evalTarget)
	if err != nil {
		return Receipt{Verified: false, Detail: fmt.Sprintf("file %s not found on disk", step.Path)}
	}
	if info.Size() == 0 {
		return Receipt{Verified: false, Detail: fmt.Sprintf("file %s exists but is 0 bytes", step.Path)}
	}

	if !step.BaselineExists {
		// File appeared after start — acceptable creation receipt.
		return Receipt{
			Verified: true,
			Detail:   fmt.Sprintf("verified new file %s (%d bytes)", step.Path, info.Size()),
		}
	}

	changed := info.Size() != step.BaselineSize || !info.ModTime().Equal(step.BaselineMod)
	if !changed {
		return Receipt{
			Verified: false,
			Detail:   fmt.Sprintf("file %s unchanged since step start — pre-existing content is not a publish receipt", step.Path),
		}
	}

	return Receipt{
		Verified: true,
		Detail:   fmt.Sprintf("verified changed file %s (%d bytes, modtime %s)", step.Path, info.Size(), info.ModTime().Format(time.RFC3339)),
	}
}

// IsWriteVerb reports hops that require a disk receipt and user gate.
func IsWriteVerb(verb string) bool {
	switch strings.ToLower(verb) {
	case "publish", "write", "land", "merge":
		return true
	default:
		return false
	}
}
