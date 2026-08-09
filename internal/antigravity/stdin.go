// Package antigravity parses the JSON Antigravity CLI (agy) passes to
// statusline commands on stdin.
//
// Schema verified two ways on 2026-08-02 against agy 1.1.9: the documented
// contract (antigravity.google/docs/cli/statusline) and a live capture of six
// payloads from a real interactive session on the dev machine (docs/design.md
// §3.8). Pointer types mark fields observed or documented as conditionally
// absent — absence must stay distinguishable from a zero value.
//
// Detection: every observed payload carries `"product": "antigravity"`, which
// Claude Code's statusline payload does not. cmd/telltale routes on that
// field — an affirmative, documented marker, not a heuristic.
//
// What this payload has that no other vendor's statusline seam offers:
//
//   - quota as NAMED BUCKETS (observed: "gemini-weekly", "3p-weekly"), each
//     with remaining_fraction and an exact reset_time;
//   - agent_state — the first vendor-REPORTED liveness signal in this
//     product's universe (idle/thinking/working/tool_use/initializing);
//   - vcs — branch and dirty state in the payload itself, so a branch segment
//     needs no exec (documented; not yet observed live — the capture session
//     ran outside a repo).
//
// The payload also advertises transcript_path; agy 1.1.9 never writes that
// file (§3.8), which is why there is no Antigravity HUD adapter.
package antigravity

import (
	"encoding/json"
	"io"
)

// Product is the documented product marker this package detects.
const Product = "antigravity"

type StatuslineInput struct {
	Cwd            string         `json:"cwd"`
	ConversationID string         `json:"conversation_id"`
	SessionID      string         `json:"session_id"` // documented back-compat alias
	TranscriptPath string         `json:"transcript_path"`
	Version        string         `json:"version"`
	Product        string         `json:"product"`
	Model          Model          `json:"model"`
	Workspace      *Workspace     `json:"workspace,omitempty"`
	ContextWindow  *ContextWindow `json:"context_window,omitempty"`
	// Quota is a map of bucket id to window; bucket ids are vendor-defined
	// (two weekly buckets observed on the Starter tier) and are rendered
	// verbatim rather than translated through an assumed vocabulary.
	Quota                   map[string]*QuotaBucket `json:"quota,omitempty"`
	AgentState              string                  `json:"agent_state,omitempty"`
	VCS                     *VCS                    `json:"vcs,omitempty"`
	Sandbox                 *Sandbox                `json:"sandbox,omitempty"`
	PlanTier                string                  `json:"plan_tier,omitempty"`
	ArtifactCount           *int                    `json:"artifact_count,omitempty"`
	TaskCount               *int                    `json:"task_count,omitempty"`
	PendingInputCount       *int                    `json:"pending_input_count,omitempty"`
	ToolConfirmationPending bool                    `json:"tool_confirmation_pending,omitempty"`
	TerminalWidth           int                     `json:"terminal_width,omitempty"`
	ExecutionMode           string                  `json:"execution_mode,omitempty"`
}

type Model struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Effort      string `json:"effort,omitempty"`
}

type Workspace struct {
	CurrentDir string `json:"current_dir"`
	ProjectDir string `json:"project_dir"`
}

type ContextWindow struct {
	TotalInputTokens    int64         `json:"total_input_tokens"`
	TotalOutputTokens   int64         `json:"total_output_tokens"`
	ContextWindowSize   int64         `json:"context_window_size"`
	UsedPercentage      *float64      `json:"used_percentage,omitempty"`
	RemainingPercentage *float64      `json:"remaining_percentage,omitempty"`
	CurrentUsage        *CurrentUsage `json:"current_usage,omitempty"`
}

type CurrentUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

// QuotaBucket is one named quota window. The vendor reports the REMAINING
// fraction; rendering converts to used% (a unit conversion on a sourced
// value, same rule as the countdown arithmetic).
type QuotaBucket struct {
	RemainingFraction *float64 `json:"remaining_fraction,omitempty"`
	ResetTime         string   `json:"reset_time,omitempty"` // RFC3339
	ResetInSeconds    *int64   `json:"reset_in_seconds,omitempty"`
}

type VCS struct {
	Type   string `json:"type,omitempty"` // git / jj / hg (documented)
	Branch string `json:"branch,omitempty"`
	Dirty  bool   `json:"dirty,omitempty"`
}

type Sandbox struct {
	Enabled      bool `json:"enabled"`
	AllowNetwork bool `json:"allow_network,omitempty"`
}

// Parse decodes statusline stdin. Unknown fields are ignored by design; the
// same single-JSON-value framing note as the claude package applies (no line
// splitting, so U+2028/U+2029 cannot tear a record here).
func Parse(r io.Reader) (*StatuslineInput, error) {
	var in StatuslineInput
	dec := json.NewDecoder(r)
	if err := dec.Decode(&in); err != nil {
		return nil, err
	}
	return &in, nil
}
