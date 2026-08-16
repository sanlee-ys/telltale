// Package claude parses the JSON Claude Code passes to statusline commands on
// stdin. Schema verified against https://code.claude.com/docs/en/statusline on
// 2026-08-01. Pointer types mark fields the docs list as conditionally absent —
// absence must stay distinguishable from a zero value (the honest-gauge rule).
//
// Every field mapped 1:1 to a DOCUMENTED field until 2026-08-16, when the
// payload was re-measured by source read at CLI 2.1.233 (design.md §7.16b).
// Two things changed and both are recorded on the types below:
//
//   - `context_window` grew `total_input_tokens`, `total_output_tokens` and a
//     `current_usage` object. They are modelled here and used nowhere — see
//     ContextWindow for why a per-API-call level may not feed the token relay.
//   - `prompt_id` is present and is NOT on the statusline documentation page.
//     It arrives through the vendor's shared session-basics helper rather than
//     the statusline's own assembly, so the docs page is not wrong so much as
//     not the whole payload. A field measured in the bundle but absent from the
//     docs is still a measured field; this package now says which is which.
//
// The parser's tolerance of unknown fields (see Parse) is what made that growth
// a non-event for every telltale release in between.
package claude

import (
	"encoding/json"
	"io"
)

type StatuslineInput struct {
	Cwd            string         `json:"cwd"`
	SessionID      string         `json:"session_id"`
	SessionName    string         `json:"session_name,omitempty"`
	TranscriptPath string         `json:"transcript_path"`
	Version        string         `json:"version"`
	Model          Model          `json:"model"`
	Workspace      *Workspace     `json:"workspace,omitempty"`
	Cost           *Cost          `json:"cost,omitempty"`
	ContextWindow  *ContextWindow `json:"context_window,omitempty"`
	RateLimits     *RateLimits    `json:"rate_limits,omitempty"`
	Worktree       *Worktree      `json:"worktree,omitempty"`
	Agent          *Agent         `json:"agent,omitempty"`

	// PromptID is the vendor's own correlation id for the prompt being
	// processed — `pr.requestJournal.promptId()`, the same UUID the vendor's
	// OpenTelemetry stream calls `prompt.id`. It reaches the payload through
	// the shared session-basics helper rather than the statusline's own
	// assembly (§7.16b), which is why it arrives with no documentation on the
	// statusline page.
	//
	// It is parsed and used by NOTHING. It is here because it is the only
	// field that could ever de-duplicate repeated fires of one prompt, and
	// §7.16b measured that there is no per-turn count for it to de-duplicate;
	// modelling it costs one string and records what a future writer would
	// need if the vendor ever ships one.
	PromptID string `json:"prompt_id,omitempty"`
}

type Model struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type Workspace struct {
	CurrentDir  string `json:"current_dir"`
	ProjectDir  string `json:"project_dir"`
	GitWorktree string `json:"git_worktree,omitempty"`
}

type Cost struct {
	TotalCostUSD    *float64 `json:"total_cost_usd,omitempty"`
	TotalDurationMS int64    `json:"total_duration_ms"`
}

// ContextWindow carries the occupancy of the window, and — since CLI 2.1.233 —
// the token counts that occupancy is computed from.
//
// The token fields are PARSED AND RENDERED BY NOTHING, and they relay nowhere.
// That is a measured decision, not an oversight; design.md §7.16b is the
// record and this comment is the short form, because the next reader to find
// three unused count fields will otherwise correctly conclude they are dead.
//
// Source-read at Claude Code **2.1.233** (`GIT_SHA
// f8d57569aaf350fe25dc4dfa10cad59db8ea4d45`, `BUILD_TIME 2026-08-14T17:21:48Z`,
// win32 build), where the whole block is assembled by one function:
//
//	function TAw(e,t){let r=wMo(e,t);return{
//	  total_input_tokens: e ? e.input_tokens + e.cache_creation_input_tokens
//	                          + e.cache_read_input_tokens : 0,
//	  total_output_tokens: e?.output_tokens ?? 0,
//	  context_window_size: t, current_usage: e,
//	  used_percentage: r.used, remaining_percentage: r.remaining}}
//
// and `e` comes from `TDr(messages)`, which walks the message list BACKWARDS
// and returns the first usage it finds — the single most recent assistant
// message. The vendor's own doc comments in the same bundle say it plainly:
// `current_usage` is "Token usage from last API call (null if no messages
// yet)", `total_input_tokens` is "Input tokens currently in the context window
// (incl. cache reads/writes)", and `total_output_tokens` is "Output tokens
// from the most recent API response".
//
// So every number here describes ONE API call, and the two totals are a LEVEL
// — what is in the window now — not a counter of what a turn spent. Neither is
// a per-turn count, which is the only thing internal/usagecache is allowed to
// accumulate: taking a level as a delta would sum the same context repeatedly,
// and differencing successive fires to recover a delta is the "never derive a
// number and present it as a reading" refusal (ADR-001, §4a.1) with an extra
// hazard, because the statusline fires many times per API call and every one
// of those fires reports the same last call.
type ContextWindow struct {
	ContextWindowSize   int      `json:"context_window_size"`
	UsedPercentage      *float64 `json:"used_percentage,omitempty"`
	RemainingPercentage *float64 `json:"remaining_percentage,omitempty"`

	// Pointers because a payload from a CLI older than 2.1.233 carries no such
	// key at all, and "this CLI does not report it" must stay distinguishable
	// from the measured zero TAw emits before the first message (§4a.1). The
	// statusline fixture pinned at 2.1.90 is exactly that older shape.
	TotalInputTokens  *int64        `json:"total_input_tokens,omitempty"`
	TotalOutputTokens *int64        `json:"total_output_tokens,omitempty"`
	CurrentUsage      *CurrentUsage `json:"current_usage,omitempty"`
}

// CurrentUsage is the last API call's four counts, exactly as the vendor
// reports them — the raw numbers TAw sums into total_input_tokens.
//
// Every count is a pointer for the reason cursorhook.Turn's are: TDr copies
// `input_tokens` and `output_tokens` straight off the API's usage object with
// no fallback, so either can be absent, while the two cache counts arrive
// through `?? 0` and are therefore always present. An absent count is not a
// zero one, and collapsing the two is the regression this repo exists to
// prevent.
//
// This struct is the whole allowlist for the block, the same technique
// internal/cursorhook uses: encoding/json drops every sibling field with no
// destination here, so nothing else in the payload can reach a caller by
// accident.
type CurrentUsage struct {
	InputTokens              *int64 `json:"input_tokens,omitempty"`
	OutputTokens             *int64 `json:"output_tokens,omitempty"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens,omitempty"`
}

// RateLimits appears only for Pro/Max subscribers after the first API
// response; each window may be independently absent (documented).
type RateLimits struct {
	FiveHour *Window `json:"five_hour,omitempty"`
	SevenDay *Window `json:"seven_day,omitempty"`
}

// Nil-safe accessors: callers hold a possibly-nil *RateLimits and each window
// may be independently absent.
func (r *RateLimits) GetFiveHour() *Window {
	if r == nil {
		return nil
	}
	return r.FiveHour
}

func (r *RateLimits) GetSevenDay() *Window {
	if r == nil {
		return nil
	}
	return r.SevenDay
}

type Window struct {
	UsedPercentage *float64 `json:"used_percentage,omitempty"`
	ResetsAt       *int64   `json:"resets_at,omitempty"` // unix seconds
}

type Worktree struct {
	Name   string `json:"name"`
	Branch string `json:"branch,omitempty"`
}

type Agent struct {
	Name string `json:"name"`
}

// Parse decodes statusline stdin. Unknown fields are ignored by design: the
// vendor adds fields between versions and a gauge must not break when the
// payload grows.
//
// Framing note (audited 2026-08-01, see docs/design.md §4): this reads ONE JSON
// value with a streaming decoder — there is no line splitting on this path, so
// the U+2028/U+2029 record-tearing hazard that bites JSONL readers cannot occur
// here. Do not "optimize" this into a read-a-line-then-Unmarshal: statusline
// payloads carry model-authored text (session_name, display names), and those
// characters are legal unescaped inside a JSON string value. stdin_test.go
// pins this.
func Parse(r io.Reader) (*StatuslineInput, error) {
	var in StatuslineInput
	dec := json.NewDecoder(r)
	if err := dec.Decode(&in); err != nil {
		return nil, err
	}
	return &in, nil
}
