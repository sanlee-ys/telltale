// Package claude parses the JSON Claude Code passes to statusline commands on
// stdin. Schema verified against https://code.claude.com/docs/en/statusline on
// 2026-08-01; every field below maps 1:1 to a documented field. Pointer types
// mark fields the docs list as conditionally absent — absence must stay
// distinguishable from a zero value (the honest-gauge rule).
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

type ContextWindow struct {
	ContextWindowSize   int      `json:"context_window_size"`
	UsedPercentage      *float64 `json:"used_percentage,omitempty"`
	RemainingPercentage *float64 `json:"remaining_percentage,omitempty"`
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
