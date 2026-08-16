// Package antigravity parses the JSON Antigravity CLI (agy) passes to
// statusline commands on stdin.
//
// Schema verified two ways on 2026-08-02 against agy 1.1.9: the documented
// contract (antigravity.google/docs/cli/statusline) and a live capture of six
// payloads from a real interactive session on the dev machine (docs/design.md
// §3.8). Pointer types mark fields observed or documented as conditionally
// absent — absence must stay distinguishable from a zero value.
//
// That pin is STILL 1.1.9 and the 2026-08-15 re-verification at agy 1.1.13 did
// not move it, because moving it needs a live payload capture and a capture
// needs an interactive session. What the re-verification did measure on the
// quota surface is one rung less direct and is recorded as such: `agy -p
// "/quota"` reported FOUR windows — a weekly and a five-hour one for each of
// "Gemini Models" and "Claude and GPT models" — where §3.8 observed two weekly
// buckets in the payload. Those are the slash command's human labels, not the
// payload's bucket ids, so this is a reason to expect more buckets and NOT an
// observation of their ids. Nothing here needs changing either way: the ids are
// relayed verbatim and the renderer draws one segment per named bucket, so a
// third and fourth bucket already render without a vocabulary to update.
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
// # transcript_path, and a correction (2026-08-15, agy 1.1.13)
//
// This paragraph used to read: "The payload also advertises transcript_path;
// agy 1.1.9 never writes that file (§3.8), which is why there is no Antigravity
// HUD adapter." Both halves of that sentence are FALSE, and they were false
// within hours of being written — §3.8's own re-survey block, the same day,
// already recorded the opposite. What is true:
//
//   - agy DOES write the transcript. It was found for all four conversations in
//     the 2026-08-02 re-survey and for all 81 in the 2026-08-15 re-read, at
//     `~/.gemini/antigravity-cli/brain/<id>/.system_generated/logs/transcript.jsonl`,
//     with an untruncated `transcript_full.jsonl` beside it. The first survey's
//     "no transcript" reading was a miss, not a vendor behavior. What the docs
//     get wrong is the TREE, not the file: they advertise `antigravity/` and the
//     data is under `antigravity-cli/`.
//   - There IS an Antigravity HUD adapter — `internal/adapter/antigravity`,
//     shipped 2026-08-02, re-verified against agy 1.1.13 on 2026-08-15. So the
//     transcript file cannot be the reason for its absence, because it is not
//     absent.
//
// What this package still does not do is READ transcript_path, and that is a
// separate and unchanged decision: this is the statusline seam, whose whole
// contract is that it does no I/O beyond stdin (§2). The HUD adapter reaches
// the transcript by its own root, never by trusting a path handed to a gauge.
// Whether the payload's transcript_path string now points at the real file or
// still at the docs' `antigravity/` tree is UNMEASURED at 1.1.13 — confirming it
// needs a live payload capture, and the 2026-08-15 re-read captured none.
//
// # email is never parsed and must never be rendered
//
// The payload carries the signed-in account email (§3.8), and there is no field
// for it in StatuslineInput. That absence IS the enforcement, not an oversight:
// encoding/json discards every key with no destination, so the address cannot
// reach a segment, a diagnostic, a log line or the quota relay unless somebody
// adds a field on purpose — the allowlist-is-the-struct technique
// internal/cursorhook uses against a payload that puts PII beside numbers, and
// internal/adapter/cursor uses against a store that puts OAuth tokens beside
// session state (decisions/007). TestNoIdentityFieldReachesTheStruct plants
// markers in a real payload shape and asserts none survives.
//
// Do not add an Email field, and do not render one if a future payload moves the
// address into a key this struct already has. It is identity, not a gauge — the
// same ruling §2.1 makes for plan_tier, which IS parsed (the vendor names the
// tier its quota buckets belong to) and is still never drawn.
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
	Quota      map[string]*QuotaBucket `json:"quota,omitempty"`
	AgentState string                  `json:"agent_state,omitempty"`
	VCS        *VCS                    `json:"vcs,omitempty"`
	Sandbox    *Sandbox                `json:"sandbox,omitempty"`
	// PlanTier is parsed because the vendor names the tier its quota buckets
	// belong to, and it is NEVER rendered: §2.1 rules it identity, not a gauge.
	//
	// There is deliberately no Email field here, and this is the line where one
	// would go. The payload carries the signed-in address; the struct's silence
	// is what keeps it out of every downstream surface. See the package doc's
	// "email is never parsed and must never be rendered", and do not add one.
	PlanTier                string `json:"plan_tier,omitempty"`
	ArtifactCount           *int   `json:"artifact_count,omitempty"`
	TaskCount               *int   `json:"task_count,omitempty"`
	PendingInputCount       *int   `json:"pending_input_count,omitempty"`
	ToolConfirmationPending bool   `json:"tool_confirmation_pending,omitempty"`
	TerminalWidth           int    `json:"terminal_width,omitempty"`
	ExecutionMode           string `json:"execution_mode,omitempty"`
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
