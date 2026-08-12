// Package gatehook is the one sentence council's own PreToolUse hook says, and
// the words that reach it.
//
// It exists as its own package for the same reason internal/theme does: two
// unrelated callers need the same constant and neither should own it. The
// council room writes a settings file naming `telltale hook gate` as a command;
// cmd/telltale answers that command. If either side spelled the words itself,
// a rename would produce a room whose seat runs a hook that is not there — and
// a hook that fails to run makes NO decision, which is the failure this whole
// build is guarding against (design.md §9.8, amended 2026-08-12).
//
// Stdlib-only, deliberately. This runs once per tool call on the gated seat, so
// its process cost is a product cost: measured at 36.2 ms median through the
// shell Claude Code launches it with, against a 6.4 s warm turn.
package gatehook

import "encoding/json"

// Mode and Verb are the argv words. `telltale hook gate` sits beside
// `telltale hook cursor` because both are the same kind of thing: a mode whose
// stdout the vendor parses, rather than a flag on a gauge.
const (
	Mode = "hook"
	Verb = "gate"
)

// Reason is forwarded to the permission card VERBATIM by Claude Code, measured
// 2026-08-12 on 2.1.228: the request arrived carrying decision_reason_type
// "hook" and this string in decision_reason. So it is read by a person, not by
// a parser, and it says who stopped the call rather than that something did.
const Reason = "telltale council gates every call in this room"

// Decision is the whole of what the hook prints.
//
// permissionDecision "ask" is step ONE of Claude Code's six-step evaluation and
// an allow rule is step five, which is the entire mechanism this build rests
// on: the operator's own allow rules can stay loaded, and a call they cover
// still reaches council's card. Measured, two trials, three tool shapes — a
// Bash call an allow rule covers (`mkdir`), a Bash call no rule covers, and a
// non-Bash tool (`Write`) — all three gated, and nothing landed on disk.
//
// Returned as bytes rather than written here so the caller owns the one write
// to stdout. A hook's stdout IS its result, so a stray Println anywhere near it
// is a malformed decision rather than a log line.
func Decision() []byte {
	out, err := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "ask",
			"permissionDecisionReason": Reason,
		},
	})
	if err != nil {
		// Unreachable over a map of strings, and handled anyway because the
		// consequence of an empty stdout is not an error message: it is a hook
		// that made no decision, and therefore a call that runs ungated behind
		// a badge still claiming the gate. Falling back to a literal keeps the
		// decision even if marshalling ever stops working.
		return []byte(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask"}}`)
	}
	return out
}
