package vendors

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// Every JSON literal in this file is a REAL captured line from grok 1.0.0
// (3cd0d0cbce) on 2026-08-09, edited only to shorten ids and to replace the
// probe's absolute temp path. Synthesizing a line and then asserting the parser
// agrees with it would test this file against itself; the whole point of the
// adapter's doc comments is that the schema was observed, and these are the
// observations.

// A tool call as it was announced and then resolved, verbatim apart from the
// truncated id.
const (
	grokAnnounce = `{"type":"tool_call","toolCallId":"call-12ae-0","title":"read_file",` +
		`"kind":"read","status":"pending","toolName":"read_file",` +
		`"rawInput":{"target_file":"notes.txt"},"content":[],"locations":[]}`
	grokInterim = `{"type":"tool_call_update","toolCallId":"call-12ae-0","status":null,` +
		`"content":[],"rawOutput":null,"locations":[{"path":"notes.txt"}]}`
	grokDone = `{"type":"tool_call_update","toolCallId":"call-12ae-0","status":"completed",` +
		`"content":[{"type":"content","content":{"type":"text","text":"1→alpha\nbeta\n"}}],` +
		`"rawOutput":{"type":"ReadFile","FileContent":{"content":"1→alpha\nbeta\n"}},"locations":[]}`
	grokFailed = `{"type":"tool_call_update","toolCallId":"call-a959-0","status":"failed",` +
		`"content":[{"type":"content","content":{"type":"text",` +
		`"text":"Error: does-not-exist-xyz.txt does not exist.\nNote: your current working directory is ..."}}],` +
		`"rawOutput":{"type":"ReadFile","FileNotFound":"Error: ..."},"locations":[]}`
	grokEnd = `{"type":"end","stopReason":"end_turn","sessionId":"019fe742-5f41-7242-96c4-c3d94d063784",` +
		`"requestId":"ce04034b","usage":{"input_tokens":16782,"output_tokens":87},` +
		`"num_turns":2,"total_cost_usd":0.0407676,"total_cost_usd_ticks":407676000,` +
		`"modelUsage":{"grok-4.5-build":{"costUSD":0.0407676}}}`
	// The slash-eaten turn, complete. No usage, no cost, no text — and the
	// reason CostUSD has to be a pointer.
	grokEndNoCost = `{"type":"end","stopReason":"end_turn","sessionId":"019fe747-8ea4",` +
		`"requestId":"9ec5bbf7"}`
)

func parseGrok(t *testing.T, line string) (runner.Event, bool) {
	t.Helper()
	return Grok{}.ParseEvent([]byte(line))
}

// TestGrokStreamsTextAndSuppressesThought is the judgement call in grok.go,
// pinned: `text` is the vendor speaking and reaches the column, `thought` is the
// vendor reasoning and does not.
//
// It matters more here than the same rule does on the Codex seat, because the
// volume is lopsided: the first captured turn carried 46 thought lines against
// 14 of text. A parser that let them through would fill the column with the
// model talking to itself and bury the answer the room convened to compare.
func TestGrokStreamsTextAndSuppressesThought(t *testing.T) {
	ev, ok := parseGrok(t, `{"type":"text","data":"I'll"}`)
	if !ok || ev.Kind != runner.KindText || ev.Text != "I'll" {
		t.Errorf("text delta = %+v ok=%v, want KindText %q", ev, ok, "I'll")
	}
	if _, ok := parseGrok(t, `{"type":"thought","data":"The user wants me to"}`); ok {
		t.Error("a thought reached the column; the room compares answers, not reasoning")
	}
	// Inventory chatter, four times a turn, reporting nothing the vendor did.
	if _, ok := parseGrok(t, `{"type":"available_commands","tools":["read_file"],"commands":["compact"]}`); ok {
		t.Error("available_commands reached the room")
	}
	// Token counts mid-turn: real, but the turn's dollar figure comes from the
	// vendor on `end`, and two sources for one line is how they disagree.
	if _, ok := parseGrok(t, `{"type":"usage","usage":{"input_tokens":1},"signature":"opaque"}`); ok {
		t.Error("a mid-turn usage line reached the room")
	}
}

// TestGrokTextDeltasConcatenateUntouched guards the one thing a streaming column
// can get wrong invisibly: separators.
//
// grok's deltas carry their own leading spaces (" read", " then"), so joining
// them with anything doubles the spacing — and a golden rendered from a
// synthesized stream would never show it. Reassembled here from the real
// capture's first seven deltas.
func TestGrokTextDeltasConcatenateUntouched(t *testing.T) {
	deltas := []string{"I'll", " read", " `", "notes", ".txt", "`,", " then"}
	var got strings.Builder
	for _, d := range deltas {
		b, _ := json.Marshal(map[string]string{"type": "text", "data": d})
		ev, ok := parseGrok(t, string(b))
		if !ok {
			t.Fatalf("delta %q dropped", d)
		}
		got.WriteString(ev.Text)
	}
	if want := "I'll read `notes.txt`, then"; got.String() != want {
		t.Errorf("reassembled = %q, want %q", got.String(), want)
	}
}

// TestGrokNullStatusDoesNotResolveACall is the pointer-ness of grokLine.Status,
// pinned as behaviour rather than as a type.
//
// The first tool_call_update of EVERY captured call carries `"status":null` and
// exists only to report locations. Flattened to "", the obvious reading — "not
// pending any more" — would close the entry the instant the call started, so a
// long command would render as finished-with-unknown-outcome while it was still
// running. That is a false gauge in the direction this repo cares about most.
func TestGrokNullStatusDoesNotResolveACall(t *testing.T) {
	ev, ok := parseGrok(t, grokInterim)
	if !ok {
		t.Fatal("the interim update was dropped")
	}
	if len(ev.Acts) != 1 || ev.Acts[0].Outcome != runner.ActPending {
		t.Errorf("interim update resolved the call: %+v, want still pending", ev.Acts)
	}
	if ev.Acts[0].ID != "call-12ae-0" {
		t.Errorf("interim id = %q, want the announcement's id", ev.Acts[0].ID)
	}
}

// TestGrokOutcomesAreTheCapturedSpellings. Both are measured, which is what lets
// this seat report success at all — the Codex adapter cannot, because
// "completed" was never captured there and it refuses to guess the spelling.
func TestGrokOutcomesAreTheCapturedSpellings(t *testing.T) {
	cases := []struct {
		name string
		line string
		want runner.ActStatus
	}{
		{"completed", grokDone, runner.ActOK},
		{"failed", grokFailed, runner.ActFailed},
		{"announced", grokAnnounce, runner.ActPending},
		{"null", grokInterim, runner.ActPending},
		// A status no capture has shown. Unknown rather than mapped: an
		// unrecognised value is not evidence, and inventing an outcome for it
		// is how a trace starts lying quietly.
		{"unseen", `{"type":"tool_call_update","toolCallId":"x","status":"cancelled"}`, runner.ActUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev, ok := parseGrok(t, c.line)
			if !ok || len(ev.Acts) != 1 {
				t.Fatalf("line produced %+v ok=%v", ev, ok)
			}
			if ev.Acts[0].Outcome != c.want {
				t.Errorf("outcome = %v, want %v", ev.Acts[0].Outcome, c.want)
			}
		})
	}
}

// TestGrokNamesTheCallAndItsArgument pins the trace grammar this room shares:
// the tool's real name, then ": " and the one argument that identifies it.
func TestGrokNamesTheCallAndItsArgument(t *testing.T) {
	ev, ok := parseGrok(t, grokAnnounce)
	if !ok || len(ev.Acts) != 1 {
		t.Fatalf("announcement produced %+v ok=%v", ev, ok)
	}
	if want := "read_file: notes.txt"; ev.Acts[0].Text != want {
		t.Errorf("act text = %q, want %q", ev.Acts[0].Text, want)
	}
}

// TestGrokFailureDetailIsTheVendorsOwnWords — §9.6a. The detail is grok's first
// line, clipped, and never a sentence composed here. The huge rawOutput blob
// beside it stays unparsed.
func TestGrokFailureDetailIsTheVendorsOwnWords(t *testing.T) {
	ev, _ := parseGrok(t, grokFailed)
	if len(ev.Acts) != 1 {
		t.Fatalf("failed update produced %+v", ev.Acts)
	}
	got := ev.Acts[0].Detail
	if !strings.HasPrefix(got, "Error: does-not-exist-xyz.txt does not exist") {
		t.Errorf("detail = %q, want the vendor's own first line", got)
	}
	if strings.Contains(got, "\n") {
		t.Errorf("detail carries a newline into a one-line trace cell: %q", got)
	}
	// A successful call carries no detail: there is nothing wrong to explain,
	// and the read's own output is not news about the room.
	done, _ := parseGrok(t, grokDone)
	if d := done.Acts[0].Detail; d != "" {
		t.Errorf("a completed call carried detail %q", d)
	}
}

// TestGrokEndCarriesThreadAndTheVendorsOwnCost.
//
// This is the seat that made council's cost line reachable from somewhere other
// than Claude, and the reason it is allowed is that the number is READ. The
// assertion is on the exact captured figure so that any future arithmetic —
// rounding, a unit conversion, a helpful division by anything — fails here.
func TestGrokEndCarriesThreadAndTheVendorsOwnCost(t *testing.T) {
	ev, ok := parseGrok(t, grokEnd)
	if !ok || ev.Kind != runner.KindMeta {
		t.Fatalf("end produced %+v ok=%v, want KindMeta", ev, ok)
	}
	if want := "019fe742-5f41-7242-96c4-c3d94d063784"; ev.SessionID != want {
		t.Errorf("sessionId = %q, want %q", ev.SessionID, want)
	}
	if ev.CostUSD == nil {
		t.Fatal("the vendor reported a cost and the adapter dropped it")
	}
	if *ev.CostUSD != 0.0407676 {
		t.Errorf("cost = %v, want the vendor's own 0.0407676 — unrounded, underived",
			*ev.CostUSD)
	}
}

// TestGrokAbsentCostStaysAbsent is design.md §4a.1's zero-vs-absent rule on the
// one field where this vendor actually produces both states.
//
// The captured turn behind grokEndNoCost is the slash-eaten one: grok consumed a
// brief beginning with "/" as a slash command, no model call happened, and the
// end event carries no usage and no cost keys at all. A nil-to-zero flattening
// would print "$0.0000" for a turn that reported nothing — the exact collapse
// the HUD has a golden file to prevent.
func TestGrokAbsentCostStaysAbsent(t *testing.T) {
	ev, ok := parseGrok(t, grokEndNoCost)
	if !ok || ev.Kind != runner.KindMeta {
		t.Fatalf("end produced %+v ok=%v", ev, ok)
	}
	if ev.CostUSD != nil {
		t.Errorf("cost = %v, want nil — the vendor reported no cost, which is not zero",
			*ev.CostUSD)
	}
	if ev.SessionID == "" {
		t.Error("a turn with no cost still has a thread, and it was dropped")
	}
}

// TestGrokToolArgIsDeterministic. Go randomises map iteration; a rendered line
// and a golden must not flicker between runs. The rule is lowest key by byte
// order, so `a_path` wins over `z_path` on every pass.
func TestGrokToolArgIsDeterministic(t *testing.T) {
	const line = `{"type":"tool_call","toolCallId":"m","toolName":"multi",` +
		`"rawInput":{"z_path":"zebra.txt","a_path":"apple.txt","count":7}}`
	first, _ := parseGrok(t, line)
	for i := 0; i < 50; i++ {
		ev, _ := parseGrok(t, line)
		if ev.Acts[0].Text != first.Acts[0].Text {
			t.Fatalf("iteration %d gave %q, first gave %q", i, ev.Acts[0].Text, first.Acts[0].Text)
		}
	}
	if want := "multi: apple.txt"; first.Acts[0].Text != want {
		t.Errorf("act text = %q, want %q (lowest key by byte order; 7 is not a string)",
			first.Acts[0].Text, want)
	}
}

// TestGrokDropsWhatItCannotRead. A line that is not JSON, and a type this
// adapter has never seen, are both dropped rather than failing the turn — the
// ParseEvent contract in vendor.go.
func TestGrokDropsWhatItCannotRead(t *testing.T) {
	for _, line := range []string{
		"", "not json at all", "[]",
		`{"type":"some_future_event","data":"x"}`,
	} {
		if _, ok := parseGrok(t, line); ok {
			t.Errorf("line %q was accepted", line)
		}
	}
}

// TestGrokAsksForNothingInEitherPosture.
//
// The Antigravity seat's test by the same name, for the same reason and on the
// same class of evidence: --permission-mode plan was measured writing the file,
// and --sandbox was measured silently accepting a profile that cannot exist. A
// posture flag that appeared here would be a claim this repo has refuted or
// cannot observe, and the badge in detect.go says council asks for neither.
func TestGrokAsksForNothingInEitherPosture(t *testing.T) {
	read, err := Grok{}.FirstTurn("brief", "/ws", "grok", PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	write, err := Grok{}.FirstTurn("brief", "/ws", "grok", PostureWrite)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(read.Args, " ") != strings.Join(write.Args, " ") {
		t.Errorf("posture changed the invocation:\n read: %v\nwrite: %v", read.Args, write.Args)
	}
	for _, banned := range []string{"--sandbox", "--permission-mode", "--always-approve"} {
		for _, a := range read.Args {
			if a == banned {
				t.Errorf("%s is passed; detect.go's badge says council asks for nothing", banned)
			}
		}
	}
}

// TestGrokPutsThePromptLastOnArgv.
//
// -p takes the prompt as its VALUE, so the prompt must be the argument
// immediately after it — the discipline agy.go learned the expensive way, where
// a flag added after -p is silently swallowed INTO the prompt. Asserted on both
// entry points, because the resume path is where a new flag would most likely be
// appended by someone reading only half this file.
func TestGrokPutsThePromptLastOnArgv(t *testing.T) {
	check := func(t *testing.T, spec runner.Spec) {
		t.Helper()
		n := len(spec.Args)
		if n < 2 || spec.Args[n-2] != "-p" || spec.Args[n-1] != "the brief" {
			t.Errorf("argv does not end in -p <prompt>: %v", spec.Args)
		}
		if spec.StdinPrompt != "" {
			t.Errorf("prompt on stdin (%q); grok offers no stdin channel for it",
				spec.StdinPrompt)
		}
		if spec.Vendor != model.VendorGrok {
			t.Errorf("spec vendor = %q", spec.Vendor)
		}
	}
	first, err := Grok{}.FirstTurn("the brief", "/ws", "grok", PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	check(t, first)

	next, err := Grok{}.NextTurn("the brief", "/ws", "grok", "sess-1", PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	check(t, next)
}

// TestGrokResumesOnItsOwnThread. --resume takes the id and precedes -p;
// verified live as a real resume rather than a re-send (see NextTurn's comment).
func TestGrokResumesOnItsOwnThread(t *testing.T) {
	spec, err := Grok{}.NextTurn("again", "/ws", "grok", "019fe742", PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(spec.Args, " ")
	if !strings.Contains(joined, "--resume 019fe742") {
		t.Errorf("resume args = %v", spec.Args)
	}
	if !strings.Contains(joined, "--output-format streaming-json") {
		t.Errorf("resume dropped the output format: %v", spec.Args)
	}
}

// TestGrokRefusesToInventAThread. No id means the room says the thread was
// lost; an adapter that quietly opened a new conversation would take that
// sentence away from it.
func TestGrokRefusesToInventAThread(t *testing.T) {
	if _, err := (Grok{}).NextTurn("p", "/ws", "grok", "", PostureRead); err != ErrNoResume {
		t.Errorf("NextTurn with no session id returned %v, want ErrNoResume", err)
	}
}

// TestGrokIsRegistered. The registry is what decides whether a seat renders as a
// column or as an unavailable card, and this adapter's invocation has been
// verified against a live binary — which is the bar vendor.go sets for being in
// there at all.
func TestGrokIsRegistered(t *testing.T) {
	v, ok := Registry()[model.VendorGrok]
	if !ok {
		t.Fatal("grok is not in the registry")
	}
	if v.ID() != model.VendorGrok {
		t.Errorf("registered adapter reports id %q", v.ID())
	}
}
