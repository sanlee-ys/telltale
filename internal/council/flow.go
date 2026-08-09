package council

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// FlowStepState is harness-observed workflow state — never inferred from model prose.
type FlowStepState uint8

const (
	FlowStateQueued FlowStepState = iota
	FlowStateRunning
	FlowStateBlocked  // awaiting user write authorization, or post-write receipt failure
	FlowStateReturned // seat finished; not a judgment that the work is "approved"
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
	case FlowStateReturned:
		return "returned"
	case FlowStatePublished:
		return "published"
	case FlowStateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Receipt holds empirical evidence for a write hop's target path.
// Verified means the file was created or changed after step start inside the
// workspace — not that this seat authored it, and never that State claimed success.
type Receipt struct {
	Verified bool
	Detail   string
}

// FlowStep is one hop in a /flow chain.
//
// Write authority is explicit: Path != "" means the hop may mutate the workspace
// and requires a pre-dispatch user gate plus a post-dispatch disk receipt.
// English verbs are labels only — they do not confer write authority.
type FlowStep struct {
	Vendor    model.VendorID
	Verb      string
	Task      string
	State     FlowStepState
	Path      string
	StartedAt time.Time
	Receipt   Receipt

	BaselineExists bool
	BaselineSize   int64
	BaselineMod    time.Time
}

// writeTargetPrefix is the ONLY thing that confers write authority on a hop.
//
// It is a declared token, not a shape the parser recognises in prose. The
// shipped parser sniffed the last word for `.`, `/` or `\` and promoted the hop
// on a match, which meant "review the auth flow." and "config.yaml" and a
// Windows path pasted into a sentence were all indistinguishable from an
// instruction to mutate the workspace. English does not grant permissions.
const writeTargetPrefix = "write:"

// validWriteTarget accepts only a workspace-relative path with no traversal.
//
// Checked at PARSE time rather than at receipt time on purpose: VerifyReceipt
// can prove after the fact that a write landed outside the workspace, but by
// then the seat has already been spawned with write authority and pointed at
// the path. The only place the answer is free is before the gate is drawn.
func validWriteTarget(target string) error {
	if target == "" {
		return errors.New("empty target")
	}
	// filepath.IsAbs is platform-dependent — on Windows it does not consider
	// "/etc/shadow" absolute — so the leading-separator and volume cases are
	// spelled out rather than delegated to it.
	if filepath.IsAbs(target) || strings.HasPrefix(target, "/") || strings.HasPrefix(target, `\`) {
		return errors.New("must be relative to the workspace")
	}
	if filepath.VolumeName(target) != "" || strings.Contains(target, ":") {
		return errors.New("must not name a volume or drive")
	}
	// Segment-wise, so "..", "a/../../b" and the backslash spellings of both are
	// all one rule instead of a list of prefixes to keep in sync.
	for _, seg := range strings.FieldsFunc(target, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return errors.New("must not traverse out of the workspace")
		}
	}
	return nil
}

// FlowChain is an ordered pipeline of steps.
type FlowChain struct {
	Steps        []FlowStep
	CurrentIndex int
}

// RequiresWriteGate reports explicit write hops (target path present).
func (s *FlowStep) RequiresWriteGate() bool {
	return s != nil && strings.TrimSpace(s.Path) != ""
}

// ParseFlowChain parses arrow-delimited workflow instructions.
//
// Example: "/flow @claude draft feature spec -> @codex review security ->
// @agy publish write:docs/spec.md". The third hop is a write hop because it
// says so, not because "docs/spec.md" looks like a filename.
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
			return nil, fmt.Errorf("unknown vendor seat @%s in flow chain (valid seats: @%s)",
				name, strings.Join(SeatNames(), ", @"))
		}

		verb := strings.ToLower(parts[1])
		// The verb slot would otherwise swallow a target the user clearly meant
		// to declare, and the hop would run as a READ hop that looks written.
		// Silently downgrading a declared write is the same class of lie as
		// silently upgrading a read, so it is refused out loud.
		if strings.HasPrefix(verb, writeTargetPrefix) {
			return nil, fmt.Errorf("hop %q puts the %s target in the verb slot: expected '@seat verb %s<path>'", raw, writeTargetPrefix, writeTargetPrefix)
		}
		if verb == "merge" {
			return nil, fmt.Errorf("merge hops are not supported in v1: file receipts cannot prove a GitHub merge")
		}

		// Write authority is declared, never inferred. Only a `write:<path>`
		// token makes a hop a write hop — no path sniffing, no verb allowlist.
		// "@cursor implement authentication" is a read hop; so is a task that
		// happens to end in "v1." Anything else guesses authority from English.
		task := ""
		path := ""
		var taskWords []string
		for _, tok := range parts[2:] {
			rest, isTarget := strings.CutPrefix(tok, writeTargetPrefix)
			if !isTarget {
				taskWords = append(taskWords, tok)
				continue
			}
			if path != "" {
				return nil, fmt.Errorf("hop %q declares more than one %s target", raw, writeTargetPrefix)
			}
			rest = strings.TrimSpace(rest)
			if err := validWriteTarget(rest); err != nil {
				return nil, fmt.Errorf("%s target %q in hop %q: %v", writeTargetPrefix, rest, raw, err)
			}
			path = rest
		}
		task = strings.Join(taskWords, " ")

		steps = append(steps, FlowStep{
			Vendor: vendorID,
			Verb:   verb,
			Task:   task,
			State:  FlowStateQueued,
			Path:   path,
		})
	}

	return &FlowChain{Steps: steps, CurrentIndex: 0}, nil
}

// Current returns the active step.
func (fc *FlowChain) Current() *FlowStep {
	if fc == nil || fc.CurrentIndex < 0 || fc.CurrentIndex >= len(fc.Steps) {
		return nil
	}
	return &fc.Steps[fc.CurrentIndex]
}

// MarkAwaitingWrite parks a Queued write hop until the user authorizes dispatch.
func (fc *FlowChain) MarkAwaitingWrite(detail string) error {
	curr := fc.Current()
	if curr == nil {
		return errors.New("no active step")
	}
	if curr.State != FlowStateQueued {
		return fmt.Errorf("cannot await write from state %s", curr.State)
	}
	if !curr.RequiresWriteGate() {
		return errors.New("step has no target path — not a write hop")
	}
	curr.State = FlowStateBlocked
	curr.Receipt = Receipt{Verified: false, Detail: detail}
	return nil
}

// Start moves Queued → Running and captures a path baseline.
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

// ClearBlockForStart returns a write hop from Blocked (pre-auth) to Queued so Start can run.
func (fc *FlowChain) ClearBlockForStart() error {
	curr := fc.Current()
	if curr == nil {
		return errors.New("no active step")
	}
	if curr.State != FlowStateBlocked {
		return fmt.Errorf("cannot clear block from state %s", curr.State)
	}
	curr.State = FlowStateQueued
	curr.Receipt = Receipt{}
	return nil
}

// MarkReturned records that the seat finished (PhaseDone). Not an approval.
func (fc *FlowChain) MarkReturned() error {
	curr := fc.Current()
	if curr == nil {
		return errors.New("no active step")
	}
	if curr.State != FlowStateRunning {
		return fmt.Errorf("cannot mark returned in state %s", curr.State)
	}
	curr.State = FlowStateReturned
	return nil
}

// MarkFailed records a harness-detected failure (e.g. artifact save).
//
// It refuses a step that has already finished, and that guard is the point
// rather than tidiness. Returned and Published are terminal successes, and a
// Published step carries a VERIFIED receipt — evidence that a write the user
// authorized actually landed in the workspace. Rewriting it to
// Failed{Verified:false} because something went wrong AFTERWARDS would make the
// record deny a mutation the tree still holds, which is the same silent
// demotion of a declared write that the parser refuses at the other end.
//
// A failure discovered after the hop finished stops the chain. It does not
// relitigate the hop.
func (fc *FlowChain) MarkFailed(detail string) error {
	curr := fc.Current()
	if curr == nil {
		return errors.New("no active step")
	}
	if curr.State == FlowStateReturned || curr.State == FlowStatePublished {
		return fmt.Errorf("cannot fail step %d (@%s %s): already %s", fc.CurrentIndex+1, curr.Vendor, curr.Verb, curr.State)
	}
	curr.State = FlowStateFailed
	curr.Receipt = Receipt{Verified: false, Detail: detail}
	return nil
}

// MarkPublished sets Published only with a verified disk receipt.
func (fc *FlowChain) MarkPublished(receipt Receipt) error {
	curr := fc.Current()
	if curr == nil {
		return errors.New("no active step")
	}
	if !receipt.Verified {
		return errors.New("cannot mark published without a verified receipt")
	}
	if curr.State != FlowStateRunning {
		return fmt.Errorf("cannot publish step in state %s", curr.State)
	}
	curr.Receipt = receipt
	curr.State = FlowStatePublished
	return nil
}

// Advance moves to the next hop only from Returned or Published.
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
		return false, fmt.Errorf("cannot advance: step %d (@%s %s) is still %s", fc.CurrentIndex+1, curr.Vendor, curr.Verb, curr.State)
	case FlowStateReturned, FlowStatePublished:
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

// VerifyReceipt checks disk evidence for a write hop with a target Path.
// Proves create/change after start inside the workspace — not authorship.
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
		return Receipt{
			Verified: true,
			Detail:   fmt.Sprintf("verified new file %s (%d bytes); authorship not proven", step.Path, info.Size()),
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
		Detail:   fmt.Sprintf("verified changed file %s (%d bytes); authorship not proven", step.Path, info.Size()),
	}
}

// PromptFingerprint is a non-reversible handle for provenance headers.
func PromptFingerprint(redactedPrompt string) string {
	sum := sha256.Sum256([]byte(redactedPrompt))
	return hex.EncodeToString(sum[:8])
}
