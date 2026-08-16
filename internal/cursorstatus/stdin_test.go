package cursorstatus

import (
	"encoding/json"
	"strings"
	"testing"
)

// capturedShape is the payload cursor-agent actually wrote to a statusline
// command's stdin, at 2026.08.04-aaa8809, with the identifying values replaced
// by fakes (this repository is public; fixtures are synthesized, never real).
//
// The context_window block is verbatim in SHAPE: a session that has not yet
// made an API call sends all six keys as null. The two derived siblings are
// filled in here rather than left null, because a test that fed nulls could not
// tell a struct that refuses them from one that merely had nothing to read.
const capturedShape = `{
  "session_id": "00000000-0000-4000-8000-00000000000f",
  "session_name": "New Agent",
  "transcript_path": "/fake/.cursor/projects/fake/agent-transcripts/f/f.jsonl",
  "render_width_chars": 116,
  "cwd": "/fake/code/example-app",
  "autorun": false,
  "model": {"id": "composer-2.5", "display_name": "Composer 2.5 Fast", "param_summary": "Fast"},
  "workspace": {"current_dir": "/fake/code/example-app", "project_dir": "/fake/.cursor/projects/fake", "added_dirs": []},
  "version": "2026.08.04-aaa8809",
  "output_style": {"name": "compact"},
  "context_window": {
    "total_input_tokens": 68900,
    "total_output_tokens": 41,
    "context_window_size": 200000,
    "used_percentage": 34.5,
    "remaining_percentage": 65.5,
    "current_usage": {"input_tokens": 1}
  }
}`

// The structural half of the honest-gauge guarantee for this vendor, asserted
// where internal/cursorhook asserts its own: at the PARSER, not at the render.
//
// `remaining_percentage` (100 − used) and `total_input_tokens` (used% × window
// size — the vendor's own docs call it "derived from used_percentage") are
// computed by the CLI and named as though they were read. They have no field on
// StatuslineInput, so encoding/json drops them on the way in and there is
// nothing for a later render change to reach for. Round-tripping the parsed
// struct is what proves the drop actually happened rather than being intended.
func TestDerivedFieldsHaveNoDestination(t *testing.T) {
	in, err := Parse(strings.NewReader(capturedShape))
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, banned := range []string{
		"remaining_percentage", "65.5",
		"total_input_tokens", "68900",
		// Dropped for weaker reasons than the two above, but dropped: nothing
		// renders them, and the struct is the allowlist.
		"context_window_size", "200000",
		"current_usage", "total_output_tokens",
	} {
		if strings.Contains(got, banned) {
			t.Errorf("a field with no destination survived the parse: %q in %s", banned, got)
		}
	}
	if in.ContextWindow == nil || in.ContextWindow.UsedPercentage == nil {
		t.Fatal("the one vendor-sourced reading must survive")
	}
	if *in.ContextWindow.UsedPercentage != 34.5 {
		t.Errorf("used_percentage = %v, want 34.5", *in.ContextWindow.UsedPercentage)
	}
}

// autorun must stay a tri-state through the parse. A payload that omits it did
// not report "off", and flattening the two would be the same class of collapse
// as zero-vs-absent one field over.
func TestAutorunKeepsAbsentDistinctFromFalse(t *testing.T) {
	in, err := Parse(strings.NewReader(capturedShape))
	if err != nil {
		t.Fatal(err)
	}
	if in.Autorun == nil {
		t.Fatal("a payload that sent autorun:false must parse as a present false")
	}
	if *in.Autorun {
		t.Fatal("autorun parsed as true from a false payload")
	}
	absent, err := Parse(strings.NewReader(`{"model":{"id":"composer-2.5"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if absent.Autorun != nil {
		t.Fatal("a payload that never mentioned autorun must parse as absent, not false")
	}
}

// The null context block is the shape a real session sends before its first API
// call, and it must arrive as an unread field rather than a zero.
func TestNullContextParsesAsAbsentNotZero(t *testing.T) {
	in, err := Parse(strings.NewReader(
		`{"model":{"id":"m"},"context_window":{"used_percentage":null,"remaining_percentage":null}}`))
	if err != nil {
		t.Fatal(err)
	}
	if in.ContextWindow == nil {
		t.Fatal("the block itself was present and must parse")
	}
	if in.ContextWindow.UsedPercentage != nil {
		t.Fatalf("a null reading must stay absent, got %v", *in.ContextWindow.UsedPercentage)
	}
	zero, err := Parse(strings.NewReader(`{"model":{"id":"m"},"context_window":{"used_percentage":0}}`))
	if err != nil {
		t.Fatal(err)
	}
	if zero.ContextWindow.UsedPercentage == nil || *zero.ContextWindow.UsedPercentage != 0 {
		t.Fatal("a measured zero is data and must survive as a present 0")
	}
}

// Defensive, and recorded as defensive: this seam was MEASURED without a BOM
// (both captures began `{"s`), while the same vendor's hook payload has one and
// it breaks a plain parse (design.md §7.16).
func TestBOMPrefixParses(t *testing.T) {
	withBOM, err := Parse(strings.NewReader("\xef\xbb\xbf" + capturedShape))
	if err != nil {
		t.Fatalf("a BOM-prefixed payload must parse: %v", err)
	}
	if withBOM.Model.ID != "composer-2.5" {
		t.Fatalf("model id = %q", withBOM.Model.ID)
	}
	if _, err := Parse(strings.NewReader(capturedShape)); err != nil {
		t.Fatalf("the measured no-BOM payload must still parse: %v", err)
	}
}

// A truncated payload is an error the caller turns into a clean exit, never a
// half-filled struct rendered as though it were read.
func TestTruncatedPayloadErrors(t *testing.T) {
	if _, err := Parse(strings.NewReader(`{"model":{"id":`)); err == nil {
		t.Fatal("a truncated payload must fail the parse")
	}
}
