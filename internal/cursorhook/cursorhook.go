// Package cursorhook parses the payload Cursor hands an `afterAgentResponse`
// hook on stdin.
//
// It is the Cursor-side twin of internal/claude and internal/antigravity: a
// vendor's own documented stdin contract, parsed into the narrowest struct
// that can carry what telltale is allowed to keep.
//
// # Why the hook and not print mode
//
// `cursor-agent -p --output-format json` also emits a `usage` block, and its
// `inputTokens` is NOT the raw count — the CLI publishes
// max(raw − cacheRead − cacheWrite, 0). Measured 2026-08-08: it printed 24,076
// where the un-derived input count was 48,012. Rendering that under the label
// "input tokens" would be telltale repeating a vendor's arithmetic as if it
// were a reading, which is the ADR-001 violation this project exists to
// refuse. The hook payload carries the vendor's own `tokenUsage` fields
// untouched, so the hook wins.
//
// Source-read at cursor-agent 2026.08.04-aaa8809 (`versions/<v>/8674.index.js`,
// `./src/after-agent-hooks.ts`), where the payload is assembled as:
//
//	{conversation_id, generation_id, model, text,
//	 input_tokens:  tokenUsage?.inputTokens,
//	 output_tokens: tokenUsage?.outputTokens,
//	 cache_read_tokens:  tokenUsage?.cacheReadTokens,
//	 cache_write_tokens: tokenUsage?.cacheWriteTokens}
//
// and then enriched by the executor (`190.index.js`) with `hook_event_name`,
// `cursor_version`, `workspace_roots`, `session_id`, `transcript_path` and
// **`user_email`** before it reaches the command's stdin.
//
// # The allowlist is the struct
//
// That last paragraph is the whole reason this package exists rather than a
// map[string]any somewhere. The payload carries the model's full reply text
// AND the user's email address AND a path to the transcript. encoding/json
// discards every field with no destination, so a Turn cannot be made to hold
// any of them without someone adding a field on purpose — the same technique
// internal/adapter/cursor uses against a store that keeps OAuth tokens beside
// session state (decisions/007), applied to a payload that keeps PII beside
// numbers.
//
// Four fields survive, and they are all counts. What was deliberately left
// out, beyond the content above:
//
//   - `model` and `generation_id` — per-turn facts. The relay accumulates a
//     TOTAL across turns (design.md §7.16), and a total that names one turn's
//     model would invite reading the sum as that model's spend.
//   - `conversation_id` — the id of a cursor-agent CLI conversation, which
//     names nothing the HUD draws: the HUD's Cursor rows come from the IDE's
//     Composer store (§3.9) and the CLI keeps a separate one. Storing it would
//     dangle a join that does not exist.
package cursorhook

import (
	"encoding/json"
	"errors"
	"io"
)

// Turn is one `afterAgentResponse` firing, reduced to what telltale keeps.
//
// Every count is a pointer because the vendor emits the field only when
// `tokenUsage` carried it (`void 0` otherwise) — and absent is not zero
// (design.md §4a.1). A turn that reports no output tokens has not reported
// zero output tokens.
type Turn struct {
	InputTokens      *int64 `json:"input_tokens"`
	OutputTokens     *int64 `json:"output_tokens"`
	CacheReadTokens  *int64 `json:"cache_read_tokens"`
	CacheWriteTokens *int64 `json:"cache_write_tokens"`
}

// ErrEmpty reports a payload that decoded but carried no token counts at all.
// It is separated from a parse failure because it is not one: the hook fired,
// the JSON was well formed, and the vendor simply had no usage to report.
var ErrEmpty = errors.New("no token counts in payload")

// Complete reports whether all four counts arrived.
//
// The relay accumulates only complete turns, and this is where that rule is
// expressible. A partial payload summed into a running total would understate
// it by an amount nothing on screen could name — the total would keep looking
// like a total while quietly drifting low. Refusing the turn instead makes the
// counter go QUIET, and a visible absence is the failure mode this repo
// prefers every time (§7.7 shows less on failure).
func (t Turn) Complete() bool {
	return t.InputTokens != nil && t.OutputTokens != nil &&
		t.CacheReadTokens != nil && t.CacheWriteTokens != nil
}

// Nonnegative reports whether every count present is a count. A negative token
// figure is not a small reading, it is a broken one, and the honest response to
// a broken reading is to keep the previous total rather than corrupt it.
func (t Turn) Nonnegative() bool {
	for _, v := range []*int64{t.InputTokens, t.OutputTokens, t.CacheReadTokens, t.CacheWriteTokens} {
		if v != nil && *v < 0 {
			return false
		}
	}
	return true
}

// Parse reads one hook payload.
//
// A single JSON object, decoded once. Unlike the statusline's parsers this one
// has no second vendor to disambiguate against — the hook is registered by name
// in hooks.json, so anything arriving here claims to be an afterAgentResponse
// payload and is treated as one or rejected.
func Parse(r io.Reader) (Turn, error) {
	var t Turn
	if err := json.NewDecoder(r).Decode(&t); err != nil {
		return Turn{}, err
	}
	if t.InputTokens == nil && t.OutputTokens == nil &&
		t.CacheReadTokens == nil && t.CacheWriteTokens == nil {
		return t, ErrEmpty
	}
	return t, nil
}
