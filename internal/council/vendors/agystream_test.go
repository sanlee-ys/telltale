package vendors

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The long-lived Antigravity seat's tests.
//
// The envelope and the flag are a DOCUMENTATION READ
// (antigravity.google/docs/cli/headless, 2026-09-02) and every output line
// below is the batch parser's synthesized shape; nothing here was captured
// from a process running `--input-format stream-json`. design.md §9.54 lists
// the runs that would replace these with a version-pinned capture.

func TestAgyStreamSessionKeepsEveryFlagAndNoPrompt(t *testing.T) {
	spec, err := (AntigravityStream{}).Session(`C:\ws`, "agy.exe", "/some/hooks.json", PostureWrite)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]string{
		{"--output-format", "stream-json"},
		{"--input-format", "stream-json"},
		{"--disable-slash-commands"},
		{"--print-timeout", "30m"},
	} {
		i := slices.Index(spec.Args, want[0])
		if i < 0 {
			t.Fatalf("missing %v in %v", want, spec.Args)
		}
		if len(want) == 2 && (i+1 >= len(spec.Args) || spec.Args[i+1] != want[1]) {
			t.Fatalf("%s is not followed by %q in %v", want[0], want[1], spec.Args)
		}
	}
	// No -p and no prompt: every turn is a Turn() line written later. The
	// hooks file is ignored, because nothing in print mode answers an ask.
	if slices.Contains(spec.Args, "-p") || spec.StdinPrompt != "" {
		t.Fatalf("a prompt reached the session invocation: %v %q", spec.Args, spec.StdinPrompt)
	}
	if slices.ContainsFunc(spec.Args, func(a string) bool { return strings.Contains(a, "hooks.json") }) {
		t.Fatalf("the hooks file was wired into a seat with nobody to answer it: %v", spec.Args)
	}
	if spec.Vendor != model.VendorAntigravity || spec.Dir != `C:\ws` {
		t.Fatalf("spec = %+v", spec)
	}

	// Resume adds --conversation and nothing else; an empty id refuses.
	res, err := (AntigravityStream{}).SessionResume(`C:\ws`, "agy.exe", "", "conv-1", PostureWrite)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Args, append(spec.Args, "--conversation", "conv-1")) {
		t.Fatalf("resume args = %v, want the session args plus --conversation", res.Args)
	}
	if _, err := (AntigravityStream{}).SessionResume(`C:\ws`, "agy.exe", "", "", PostureWrite); !errors.Is(err, ErrNoResume) {
		t.Fatalf("an empty resume id must refuse, got %v", err)
	}
}

func TestAgyStreamAsksForNothingInEitherPosture(t *testing.T) {
	// The batch seat's rule, carried across: this vendor's invocation does
	// not vary by posture, and the refuted flags stay off.
	read, _ := (AntigravityStream{}).Session(`C:\ws`, "agy.exe", "", PostureRead)
	write, _ := (AntigravityStream{}).Session(`C:\ws`, "agy.exe", "", PostureWrite)
	gated, _ := (AntigravityStream{}).Session(`C:\ws`, "agy.exe", "/hooks", PostureWriteGated)
	if !slices.Equal(read.Args, write.Args) || !slices.Equal(write.Args, gated.Args) {
		t.Fatalf("the postures diverged: %v / %v / %v", read.Args, write.Args, gated.Args)
	}
	for _, never := range []string{"--sandbox", "plan", "--dangerously-skip-permissions"} {
		if slices.Contains(write.Args, never) {
			t.Fatalf("%q reappeared on the stream seat", never)
		}
	}
}

func TestAgyStreamTurnIsTheDocumentedEnvelope(t *testing.T) {
	line, err := (AntigravityStream{}).Turn("a \"quoted\"\nbrief")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Event   string `json:"event"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("the turn is not one JSON object: %v", err)
	}
	if m.Event != "user" || m.Message.Content != "a \"quoted\"\nbrief" {
		t.Fatalf("envelope = %s, want {event:user, message:{content:<the brief>}}", line)
	}
	if strings.Contains(string(line), "\n") {
		t.Fatal("a turn is one line; a raw newline would be read as a second message")
	}
}

func TestAgyStreamResultEndsTheTurnAndNothingElseDoes(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		kind runner.EventKind
		ends bool
	}{
		{"init names the conversation", `{"event":"init","conversation_id":"33333333-3333-4333-8333-333333333333","init":{"cwd":"C:\\ws"}}`, runner.KindSession, false},
		{"a step is activity", `{"event":"step_update","step_update":{"conversation_id":"3","step_index":2,"state":"ACTIVE","step_type":"tool","tool_name":"list_dir"}}`, runner.KindActivity, false},
		{"a delta is text", `{"event":"step_update","step_update":{"conversation_id":"3","step_index":3,"state":"DONE","step_type":"agent_response","text_delta":"ok\n"}}`, runner.KindText, false},
		{"a result ends the turn", `{"event":"result","result":{"conversation_id":"33333333-3333-4333-8333-333333333333","status":"SUCCESS","response":"ok"}}`, runner.KindMeta, true},
		{"a failed result ends it too", `{"event":"result","result":{"conversation_id":"33333333-3333-4333-8333-333333333333","status":"ERROR","error":"Agent execution terminated due to error."}}`, runner.KindError, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev, ok := (AntigravityStream{}).ParseEvent([]byte(tc.line))
			if !ok {
				t.Fatal("the line was dropped")
			}
			if ev.Kind != tc.kind || ev.EndsTurn != tc.ends {
				t.Fatalf("kind=%v ends=%v, want kind=%v ends=%v", ev.Kind, ev.EndsTurn, tc.kind, tc.ends)
			}
			if tc.ends && ev.SessionID == "" {
				t.Fatal("the result must carry the conversation id the room compares (§9.43)")
			}
			if tc.ends && ev.CostUSD != nil {
				t.Fatal("this vendor publishes no monetary figure")
			}
		})
	}
	// And the batch parser's own verdict is unchanged: on `agy -p` the exit
	// is the end-of-turn signal, measured.
	ev, _ := Antigravity{}.ParseEvent([]byte(`{"event":"result","result":{"conversation_id":"3","status":"SUCCESS","response":"ok"}}`))
	if ev.EndsTurn {
		t.Fatal("the batch seat's result must not end the turn; its process exit does, measured")
	}
}

func TestAgyStreamReplaysTheForkedFixtureAndKeepsTheTell(t *testing.T) {
	// The same fixture agy_test.go replays through the batch parser: a
	// conversation whose reported id is NOT the one asked for. The stream
	// seat must surface the same id on the same events — the comparison the
	// room makes (§9.43) is the only tell there is — and end the turn on it.
	raw, err := os.ReadFile(filepath.Join("testdata", "agy-forked-conversation.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	var ended bool
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		ev, ok := (AntigravityStream{}).ParseEvent([]byte(l))
		if !ok {
			continue
		}
		if ev.SessionID != "" {
			ids = append(ids, ev.SessionID)
		}
		if ev.EndsTurn {
			ended = true
		}
	}
	if len(ids) == 0 {
		t.Fatal("no conversation id came out of the stream; the fork could not be told")
	}
	if !ended {
		t.Fatal("the replayed turn never ended; on a live process nothing else would end it")
	}
	got := (AntigravityStream{}).SilentResumeForkMeasuredAt()
	if !strings.Contains(got, "1.1.11") || !strings.Contains(got, "unmeasured") {
		t.Fatalf("the fork tell must name the build it was measured on and say the stream path is not: %q", got)
	}
}

func TestAgyStreamHasNoInterruptAndNoGate(t *testing.T) {
	if _, err := (AntigravityStream{}).Interrupt("x"); !errors.Is(err, ErrAgyStreamNoInterrupt) {
		t.Fatalf("Interrupt must refuse so the room kills instead, got %v", err)
	}
	if _, err := (AntigravityStream{}).Decide("r", true, "", nil); !errors.Is(err, ErrAgyStreamNoGate) {
		t.Fatalf("Decide must refuse; nothing was asked, got %v", err)
	}
}

func TestAgyStreamTeardownIsTheStdinCloseAndABound(t *testing.T) {
	if lines := (AntigravityStream{}).Closing(); len(lines) != 0 {
		t.Fatalf("there is no interrupt to send before the pipe closes, got %s", lines)
	}
	if g := (AntigravityStream{}).Grace(); g <= 0 {
		t.Fatalf("grace = %v, want a positive bound past which the kill lands", g)
	}
}

func TestAgyStreamFallsBackToTheMeasuredPrintSeat(t *testing.T) {
	seat, ok := Registry()[model.VendorAntigravity].(AntigravityStream)
	if !ok {
		t.Fatalf("the antigravity seat must be the stream adapter, got %T", Registry()[model.VendorAntigravity])
	}
	if _, ok := seat.Fallback().(Antigravity); !ok {
		t.Fatalf("the fallback must be the measured print-mode adapter, got %T", seat.Fallback())
	}
	// The batch entry points are the measured seat's: an arena racer and a
	// fallen-back room both get the invocation agy.go verified.
	got, _ := seat.FirstTurn("brief", `C:\ws`, "agy.exe", PostureWrite)
	want, _ := Antigravity{}.FirstTurn("brief", `C:\ws`, "agy.exe", PostureWrite)
	if !slices.Equal(got.Args, want.Args) {
		t.Fatalf("FirstTurn diverged from the measured seat: %v vs %v", got.Args, want.Args)
	}
	got, _ = seat.NextTurn("brief", `C:\ws`, "agy.exe", "conv-1", PostureWrite)
	want, _ = Antigravity{}.NextTurn("brief", `C:\ws`, "agy.exe", "conv-1", PostureWrite)
	if !slices.Equal(got.Args, want.Args) {
		t.Fatalf("NextTurn diverged from the measured seat: %v vs %v", got.Args, want.Args)
	}
}
