// Package cursorstatus parses the JSON cursor-agent passes to its statusline
// command on stdin.
//
// MEASURED against cursor-agent **2026.08.04-aaa8809** on Windows 11,
// 2026-08-15/16 — a live capture plus a source read of the same build, in that
// order. design.md §2.2 carries the segment table and §7.16's dated amendment
// carries the per-surface measurement; this file carries the shapes and the
// refusals.
//
// # The seam
//
// A top-level `statusLine` object in `~/.cursor/cli-config.json`:
//
//	{"statusLine":{"type":"command","command":"telltale statusline --vendor cursor",
//	               "padding":0,"updateIntervalMs":300,"timeoutMs":2000}}
//
// The contract is the one telltale already implements for two other vendors:
// one JSON object on stdin, the command's stdout is the line. From
// `./src/hooks/use-status-line.ts` in the bundle: the command is split by
// string-argv, `~` is expanded on the FILE only, and it is spawned with
// `shell:true` on Windows; stdout has every `\r` removed and trailing newlines
// trimmed; a non-zero exit with empty stdout keeps the previous text.
// `updateIntervalMs` is clamped to >= 300 and `timeoutMs` defaults to 2000 with
// a floor of 50. **The 2000ms is the vendor's kill deadline, not a budget** —
// this path keeps ADR-002's single-digit-millisecond target, because the binary
// is spawned fresh on every update and a debounced 300ms cadence spends whatever
// it spends several times a second.
//
// # Which surfaces invoke it — measured, not assumed
//
// This is the §7.16 lesson applied before the fact: a config that validates is
// not a subsystem that initializes, so every surface was driven rather than
// reasoned about. With the marker installed at the config above:
//
//   - **interactive TUI, real console** — FIRES. Two invocations inside 0.4s of
//     the first frame, in a workspace cursor already had a project directory for.
//   - **ACP** (`cursor-agent acp`, the handshake `cursoracp.go` builds:
//     `initialize` + `session/new`) — DOES NOT FIRE.
//   - **print mode** (`cursor-agent -p`, one real turn) — DOES NOT FIRE.
//   - **no-TTY launch** (interactive argv, piped stdio) — the TUI never mounts at
//     all, so nothing fires.
//   - **interactive in a workspace cursor has never opened** — DOES NOT FIRE, two
//     trials totalling 55s, while the same build in `~/.cursor/projects/`-known
//     workspace fired within seconds. Recorded as measured; the mechanism was not
//     chased.
//
// The source read agrees and explains it: the invocation is a React hook
// (`./src/hooks/use-status-line.ts`) called from the ink chat component in
// `./src/ui.tsx`, and `statusLine` appears in exactly one bundle chunk
// (`8674.index.js`). No headless surface mounts that component. **So this seam
// is interactive-only, and council's Cursor seat — which runs on ACP (§9.36) —
// cannot feed it.** Same shape as the token relay's finding, one seam over.
//
// # Routing: there is no vendor marker, so the flag is the marker
//
// Every captured payload was checked for one. There is **no `product` field, no
// `hook_event_name`, and nothing else naming the vendor** — `version` carries the
// CLI's own build string, which is a value not a marker. The payload is
// deliberately Claude-shaped: the vendor's own skill doc says "the spec is
// aligned with Claude Code's status line", and the overlap is `session_id`,
// `transcript_path`, `cwd`, `model.*`, `workspace.*`, `version` and
// `output_style.name`.
//
// So routing CANNOT be affirmative the way `antigravity`'s `product` field is,
// and guessing from structure (`render_width_chars` present, `cost` absent) would
// be a heuristic on a payload the vendor is free to grow. `cmd/telltale` routes
// this vendor on an explicit `--vendor cursor` in the configured command instead:
// the operator writes it once into the config above, and it is the only
// unambiguous signal available.
//
// # The two fields this package REFUSES to carry
//
// `context_window` arrives with six keys. Two of them are computed by the CLI
// from a third and named as though they were read, which is the exact ADR-001
// violation print mode's `inputTokens` already cost this repo once (§7.16). From
// the bundle's own payload builder in `./src/ui.tsx`, at the pinned build:
//
//	m = null!=Ve?Ve:null,                                        // used_percentage
//	f = null!=m ? Math.max(0,Math.round(10*(100-m))/10) : null,  // remaining_percentage
//	v = null!=Ke&&null!=m ? Math.round(m/100*Ke) : null          // total_input_tokens
//
// `Ve` is the vendor's context-usage reading and `Ke` its window size; `f` and
// `v` are arithmetic on them. The vendor documents this itself — its bundled
// `statusline` skill calls `total_input_tokens` "Estimated input tokens (derived
// from used_percentage)". A token count that is really a rounded percentage,
// under a name that reads like a meter, is the thing telltale exists to not do.
//
// They are absent from the structs below rather than parsed-and-ignored, on the
// `internal/cursorhook` rule: the struct IS the allowlist, `encoding/json` drops
// every field with no destination, and a field that does not exist cannot be
// rendered by a later change that did not read this comment.
//
// `context_window_size` is genuinely vendor-reported and is dropped for a
// different and much weaker reason — nothing renders it, and the same
// struct-as-allowlist rule applies. Add it back with the segment that wants it.
//
// `current_usage` is dropped for a third reason: it was observed only as `null`
// (the vendor documents it as null before the first API call), so its populated
// shape is unmeasured. Declaring one would be inventing a schema.
package cursorstatus

import (
	"bufio"
	"encoding/json"
	"io"
)

// StatuslineInput is the payload, narrowed to what renders.
//
// Pointer types mark fields that must stay distinguishable from their zero
// value. `autorun` is the sharpest case: it is a posture, and a payload that
// omits it is not a payload that reported "off".
type StatuslineInput struct {
	Cwd     string `json:"cwd"`
	Version string `json:"version"`
	// Autorun is the vendor's own auto-run flag — the one signal in this
	// payload that Claude Code's has no counterpart for, and the one worth a
	// segment: it says the agent may run commands without asking.
	Autorun       *bool          `json:"autorun,omitempty"`
	Model         Model          `json:"model"`
	Workspace     *Workspace     `json:"workspace,omitempty"`
	ContextWindow *ContextWindow `json:"context_window,omitempty"`
	Worktree      *Worktree      `json:"worktree,omitempty"`
}

type Model struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type Workspace struct {
	CurrentDir string `json:"current_dir"`
}

// ContextWindow carries the ONE reading the vendor sources rather than
// computes. See the package doc for the two siblings that are missing and why.
//
// Observed null on every field for a session that has not yet made an API call
// — which is a degraded read, not a zero, and the renderer must hide the
// segment rather than draw `ctx 0%` for it.
type ContextWindow struct {
	UsedPercentage *float64 `json:"used_percentage,omitempty"`
}

// Worktree is documented by the vendor and was NOT observed live: both captures
// came from an ordinary checkout. Carried anyway, on the same footing as the agy
// adapter's `vcs.branch` (§3.8) — a documented field that renders when it
// arrives and hides when it does not costs nothing to be wrong about.
type Worktree struct {
	Name string `json:"name"`
}

// Parse decodes statusline stdin.
//
// The BOM strip is defensive and is recorded as such: this payload was measured
// to have **no** BOM — both captures began `7B 22 73` (`{"s`) — but the same
// vendor's HOOK payload does begin with one, which broke a plain parse
// (§7.16's trap note). One vendor writing two seams with two encodings is
// exactly the case where paying three bytes of caution is cheaper than
// rediscovering it.
//
// Unknown fields are ignored by design, and the single-JSON-value framing note
// from the claude and antigravity packages applies unchanged: the decoder reads
// one value and does no line splitting, so U+2028/U+2029 inside a string cannot
// tear a record.
func Parse(r io.Reader) (*StatuslineInput, error) {
	br := bufio.NewReader(r)
	if b, err := br.Peek(3); err == nil && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		_, _ = br.Discard(3)
	}
	var in StatuslineInput
	dec := json.NewDecoder(br)
	if err := dec.Decode(&in); err != nil {
		return nil, err
	}
	return &in, nil
}
