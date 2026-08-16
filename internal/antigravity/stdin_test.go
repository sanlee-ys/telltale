package antigravity

import (
	"encoding/json"
	"strings"
	"testing"
)

// realShape is an Antigravity statusline payload in the shape agy actually
// sends (§3.8's six-payload capture), with every identity-bearing value
// replaced by a marker. It carries the two keys the capture recorded and this
// struct deliberately has no home for — the signed-in `email`, and a
// `transcript_path` this seam must never follow — beside the fields that ARE
// gauges.
//
// It is one string used by several tests, because the value of a payload
// fixture is that it is the whole payload rather than the convenient half.
const realShape = `{
  "cwd": "C:\\src\\code\\telltale",
  "conversation_id": "0f00dbaa-1234-4a77-9b02-000000000042",
  "product": "antigravity",
  "version": "1.1.13",
  "email": "SECRET-EMAIL@example.com",
  "user_email": "SECRET-EMAIL2@example.com",
  "account": {"email": "SECRET-EMAIL3@example.com", "id": "SECRET-ACCOUNT"},
  "transcript_path": "C:\\Users\\holder\\.gemini\\antigravity\\brain\\x\\transcript.jsonl",
  "plan_tier": "Starter",
  "model": {"id": "gemini-3.6-flash", "display_name": "Gemini 3.6 Flash (High)", "effort": "high"},
  "workspace": {"current_dir": "C:\\src\\code\\telltale", "project_dir": "C:\\src\\code\\telltale"},
  "context_window": {
    "total_input_tokens": 48012,
    "total_output_tokens": 1203,
    "context_window_size": 1048576,
    "used_percentage": 4.5
  },
  "quota": {
    "gemini-weekly": {"remaining_fraction": 0.87, "reset_time": "2026-08-17T01:23:56Z"},
    "3p-weekly": {"remaining_fraction": 1.0, "reset_in_seconds": 604800}
  },
  "agent_state": "tool_use",
  "terminal_width": 120
}`

// The allowlist is the struct, and this is the assertion that says so.
//
// The payload carries the signed-in account email (§3.8); StatuslineInput has
// no field for it, and encoding/json therefore drops it at the PARSE rather
// than at the render. That ordering is the point — a value that never enters
// the struct cannot be leaked later by a segment, a diagnostic, or the quota
// relay, however those grow. Same technique as internal/cursorhook against
// Cursor's hook payload.
//
// The address is planted under three spellings on purpose. §3.8 recorded that
// the payload carries an email without pinning which key holds it, so the
// property worth asserting is not "the `email` key is dropped" but "no key
// carrying it has a destination here" — including one nested inside an object
// this struct does not model at all.
//
// If someone adds an Email field to StatuslineInput, this test fails, which is
// the intended way to find out that it happened.
func TestNoIdentityFieldReachesTheStruct(t *testing.T) {
	in, err := Parse(strings.NewReader(realShape))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{
		"SECRET-EMAIL", "SECRET-EMAIL2", "SECRET-EMAIL3", "SECRET-ACCOUNT",
	} {
		if strings.Contains(dump(t, in), marker) {
			t.Errorf("marker %q survived parsing; the struct is holding identity it must not hold", marker)
		}
	}
}

// plan_tier is the near miss, and it is parsed on purpose: the vendor names the
// tier its quota buckets belong to, so the field is part of the quota contract
// this package records. It is still identity and §2.1 rules it never rendered —
// a rule no test here can enforce, because it binds the RENDERER. What this
// test pins is the narrower fact that keeps the doc comment honest: the field
// arrives, so "never drawn" is a choice the statusline makes and not an
// accident of parsing.
func TestPlanTierIsParsedAndIsNotEmailShaped(t *testing.T) {
	in, err := Parse(strings.NewReader(realShape))
	if err != nil {
		t.Fatal(err)
	}
	if in.PlanTier != "Starter" {
		t.Errorf("plan_tier = %q, want %q", in.PlanTier, "Starter")
	}
}

// The gauges still have to arrive, or the test above would pass on a parser
// that dropped everything.
func TestGaugeFieldsSurviveTheSamePayload(t *testing.T) {
	in, err := Parse(strings.NewReader(realShape))
	if err != nil {
		t.Fatal(err)
	}
	if in.Product != Product {
		t.Errorf("product = %q, want %q — routing depends on this marker", in.Product, Product)
	}
	if in.Model.DisplayName != "Gemini 3.6 Flash (High)" {
		t.Errorf("model display_name = %q", in.Model.DisplayName)
	}
	if in.ContextWindow == nil || in.ContextWindow.UsedPercentage == nil || *in.ContextWindow.UsedPercentage != 4.5 {
		t.Errorf("context used_percentage did not survive: %+v", in.ContextWindow)
	}
	if len(in.Quota) != 2 {
		t.Errorf("quota buckets = %d, want 2", len(in.Quota))
	}
	if in.AgentState != "tool_use" {
		t.Errorf("agent_state = %q", in.AgentState)
	}
}

// transcript_path is parsed — it is in the documented contract and the struct
// records the contract — but this seam does no I/O beyond stdin (§2), so
// nothing may ever open it. This test pins the field's VALUE rather than its
// absence, so that the doc comment's claim and the code agree: the string is
// held, and the reason it is safe is that no caller follows it.
func TestTranscriptPathIsHeldButNeverAPath(t *testing.T) {
	in, err := Parse(strings.NewReader(realShape))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(in.TranscriptPath, "transcript.jsonl") {
		t.Fatalf("transcript_path did not parse (%q); the documented contract carries it", in.TranscriptPath)
	}
	// The HUD adapter reaches the transcript by its own root. If this seam ever
	// grows a reader, it will have to delete this test first.
	if in.Cwd == "" {
		t.Error("cwd did not parse")
	}
}

// dump renders everything a StatuslineInput can hold, by re-serializing it.
// A marker planted in the payload shows up here if any field kept it —
// including a field nested inside one of the pointer structs.
func dump(t *testing.T, in *StatuslineInput) string {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
