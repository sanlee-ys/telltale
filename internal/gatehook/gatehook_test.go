package gatehook

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDecisionIsTheShapeClaudeCodeRead pins the field names against a live run
// rather than against the documentation for them.
//
// Measured 2026-08-12, Claude Code 2.1.228: a hook printing exactly this object
// turned an allow-covered `mkdir` into a permission request carrying
// decision_reason_type "hook", on both trials, with nothing created on disk. A
// renamed or nested field here does not fail loudly — the hook exits 0 having
// said nothing, Claude Code reads no decision, and the call runs. So every name
// is asserted, including the ones that look like boilerplate.
func TestDecisionIsTheShapeClaudeCodeRead(t *testing.T) {
	var got struct {
		Out struct {
			Event  string `json:"hookEventName"`
			Decide string `json:"permissionDecision"`
			Reason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(Decision(), &got); err != nil {
		t.Fatalf("the decision is not valid JSON: %v", err)
	}
	if got.Out.Event != "PreToolUse" {
		t.Errorf("hookEventName = %q, want PreToolUse", got.Out.Event)
	}
	if got.Out.Decide != "ask" {
		t.Errorf("permissionDecision = %q, want ask — anything else is a call that runs ungated", got.Out.Decide)
	}
	if got.Out.Reason != Reason {
		t.Errorf("permissionDecisionReason = %q, want %q", got.Out.Reason, Reason)
	}
}

// TestTheDecisionIsNeverAllow. "allow" is a legal value of this field and it is
// the one value that would silently retire the whole gate: the seat would run
// every call, the room would draw no cards, and the badge would go on saying
// nothing runs without a keystroke.
func TestTheDecisionIsNeverAllow(t *testing.T) {
	body := string(Decision())
	for _, banned := range []string{`"allow"`, `"deny"`, `"defer"`} {
		if strings.Contains(body, banned) {
			t.Errorf("the gate hook can decide %s: %s", banned, body)
		}
	}
}

// TestTheDecisionIsOneLine. A hook's stdout IS its result. Claude Code reads it
// as one JSON object, so an encoder that ever pretty-printed this would break
// the gate rather than make it readable.
func TestTheDecisionIsOneLine(t *testing.T) {
	if strings.ContainsAny(string(Decision()), "\n\r") {
		t.Errorf("the decision spans lines: %q", Decision())
	}
}

// TestTheReasonSaysWhoStoppedTheCall. Claude Code forwards this string verbatim
// onto the permission request (measured: it arrived in decision_reason), so a
// person reads it. "Permission required" would tell them nothing they could
// act on; naming council tells them which thing to argue with.
func TestTheReasonSaysWhoStoppedTheCall(t *testing.T) {
	if !strings.Contains(Reason, "telltale council") {
		t.Errorf("Reason = %q, and it does not name what stopped the call", Reason)
	}
}
