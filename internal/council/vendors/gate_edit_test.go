package vendors

import (
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// The gate card draws a before/after only when the vendor's own payload carried
// one (§9.41), so the whole feature rests on which measured request shapes
// carry both halves. These tests are the captures themselves, verbatim.
//
// Both were taken on 2026-08-09 against Claude Code 2.1.226 on Windows, driving
// the gated invocation (--permission-prompt-tool stdio, --permission-mode
// manual, --setting-sources "") in a throwaway directory, with the workspace
// paths rewritten to a synthesized `C:\ws\` — this repo is public and a real
// path is content, not shape. Nothing else is edited.

// TestAnEditRequestCarriesBothHalves.
//
// Edit is the tool that measured to send them, and it sends them under
// `old_string` / `new_string` alongside a `replace_all` the room has no use for.
func TestAnEditRequestCarriesBothHalves(t *testing.T) {
	line := []byte(`{"type":"control_request","request_id":"d0b6b7ee-b7d4-4b1c-b550-eb090f179f72","request":{"subtype":"can_use_tool","tool_name":"Edit","display_name":"Edit","input":{"file_path":"C:\\ws\\greeting.txt","old_string":"hello world","new_string":"goodbye world","replace_all":false},"description":"greeting.txt","permission_suggestions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}],"tool_use_id":"toolu_012agqh64EdJ6VZYiGJ5gfHA"}}`)

	ev, ok := Claude{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindGate || ev.Gate == nil {
		t.Fatalf("the edit gate was dropped: ok=%v kind=%v gate=%v", ok, ev.Kind, ev.Gate)
	}
	if ev.Gate.OldContent != "hello world" {
		t.Errorf("old = %q, want the before the vendor sent", ev.Gate.OldContent)
	}
	if ev.Gate.NewContent != "goodbye world" {
		t.Errorf("new = %q, want the after the vendor sent", ev.Gate.NewContent)
	}
	// The card still names the call the same way the trace does; the preview is
	// an addition to that line, never a replacement for it.
	if ev.Gate.Text != `Edit: C:\ws\greeting.txt` {
		t.Errorf("text = %q, want the tool and its argument line", ev.Gate.Text)
	}
}

// TestAWriteRequestCarriesNoBefore is the honesty half, and the reason `content`
// is not on editHalves' list.
//
// A Write knows what the file will say and says nothing about what it says now
// — measured in the same session as the Edit above. Reading `content` as a new
// half would render a green block against a before council never saw, which is
// §4a.1's "a field nothing sourced is absent rather than plausible" broken on
// the one card in this room that guards a write.
func TestAWriteRequestCarriesNoBefore(t *testing.T) {
	line := []byte(`{"type":"control_request","request_id":"3aab5b14-4c66-4b40-9d13-b03a2768c92c","request":{"subtype":"can_use_tool","tool_name":"Write","display_name":"Write","input":{"file_path":"C:\\ws\\note.txt","content":"PONG\n"},"description":"note.txt","permission_suggestions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}],"tool_use_id":"toolu_01Ruebz7AkjAtSdDhbrrwdnu"}}`)

	ev, ok := Claude{}.ParseEvent(line)
	if !ok || ev.Gate == nil {
		t.Fatalf("the write gate was dropped: ok=%v gate=%v", ok, ev.Gate)
	}
	if ev.Gate.OldContent != "" || ev.Gate.NewContent != "" {
		t.Errorf("old = %q new = %q, want both empty: a write carries no before",
			ev.Gate.OldContent, ev.Gate.NewContent)
	}
	// The content itself is still on Input, because the protocol requires it
	// echoed back on an approval. What must not happen is it reaching the two
	// fields that cross onto State.
	if got, _ := ev.Gate.Input["content"].(string); got != "PONG\n" {
		t.Errorf("input content = %q; the approval would echo the wrong payload", got)
	}
}

// TestEditHalvesIsBothOrNeither.
//
// The renderer's test for "may I draw a preview" is that the two halves differ,
// so a half filled from a payload that carried only one would turn a partial
// request into a diff against nothing. The table walks the shapes a malformed
// or future payload could take; only the last one is a pair.
func TestEditHalvesIsBothOrNeither(t *testing.T) {
	for _, tc := range []struct {
		name  string
		in    map[string]any
		wantO string
		wantN string
		wantK bool
	}{
		{"nothing at all", map[string]any{}, "", "", false},
		{"a write's content only", map[string]any{"content": "PONG"}, "", "", false},
		{"only the before", map[string]any{"old_string": "a"}, "", "", false},
		{"only the after", map[string]any{"new_string": "b"}, "", "", false},
		{"the pair, wrong types", map[string]any{"old_string": 1.0, "new_string": 2.0}, "", "", false},
		{"one of the pair mistyped", map[string]any{"old_string": "a", "new_string": nil}, "", "", false},
		// A deletion: the pair arrived, and the after is legitimately empty.
		{"a deletion", map[string]any{"old_string": "a", "new_string": ""}, "a", "", true},
		{"a replacement", map[string]any{"old_string": "a", "new_string": "b"}, "a", "b", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotO, gotN, gotK := editHalves(tc.in)
			if gotO != tc.wantO || gotN != tc.wantN || gotK != tc.wantK {
				t.Errorf("editHalves = (%q, %q, %v), want (%q, %q, %v)",
					gotO, gotN, gotK, tc.wantO, tc.wantN, tc.wantK)
			}
		})
	}
}
