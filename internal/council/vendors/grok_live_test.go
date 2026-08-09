//go:build live

package vendors

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// Live end-to-end for the Grok seat: the real binary, driven through this
// adapter's own Spec, parsed by this adapter's own ParseEvent.
//
//	go test ./internal/council/vendors -tags=live -run TestLiveGrok -v -count=1 -timeout 10m
//
// It exists because the unit tests in grok_test.go prove one half and cannot
// prove the other. They pin the PARSER against captured lines, so they would
// still pass if the invocation this adapter builds were rejected outright by
// the CLI — the failure mode ADR-008 keeps catching, where a flag set is
// assembled from a help page and never run. This test runs the argv.
//
// Excluded from CI by the build tag on purpose: it spends real quota against
// San's account, needs a signed-in grok, and takes about a minute.
func TestLiveGrok(t *testing.T) {
	if testing.Short() {
		t.Skip("live e2e")
	}
	bin, err := exec.LookPath("grok")
	if err != nil {
		t.Skipf("grok not on PATH: %v", err)
	}

	// A prompt with an exact expected answer, so the assertion is about the pipe
	// rather than about the model's prose. The temp dir is the workspace: this
	// turn has no reason to see the repo, and the seat writes by default.
	ws := t.TempDir()

	// FENCED, and that is the whole point of this line. The first version of
	// this test sent "Reply with exactly: LIVEOK", which begins with a letter,
	// and it passed against an argv that could not send a real council turn at
	// all: a briefed room prepends Brief.Apply's `---` fence, clap refused the
	// hyphen-leading token as a flag value, and the seat exited 2 before any
	// event. A live test whose prompt is shaped unlike the product's is a green
	// check over a case that never ships.
	const fenced = "--- operating context ---\n" +
		"You are one seat in a room of five. Answer only what is asked.\n" +
		"--- end operating context. The request follows. ---\n\n" +
		"Reply with exactly: LIVEOK. Do not use any tool."

	spec, err := Grok{}.FirstTurn(fenced, ws, bin, PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	first := runLiveGrok(t, spec)

	if !strings.Contains(first.text, "LIVEOK") {
		t.Errorf("first turn text = %q, want it to contain LIVEOK", first.text)
	}
	if first.session == "" {
		t.Fatal("no session id on the first turn; the seat cannot resume")
	}
	// The claim that makes this seat the room's first with a cost line. If the
	// vendor ever stops reporting it, this is where that is noticed — and the
	// correct response is a nil CostUSD, never a figure derived from tokens.
	if first.cost == nil {
		t.Error("no total_cost_usd on the first turn; detect.go's cost claim rests on it")
	} else if *first.cost <= 0 {
		t.Errorf("reported cost %v; a real turn costs something", *first.cost)
	}

	// The resume path, which is the half most likely to rot: --resume and -p
	// have to compose, and the thread has to be the vendor's rather than a
	// re-send of ours. Asking about the first turn's content is what tells those
	// two apart — a fresh conversation cannot answer it.
	// Fenced too. Brief.Apply is first-turn only, so a real resume prompt would
	// NOT carry the fence — which is exactly why this one does: the resume path
	// must be proven safe for the hyphen-leading shape rather than merely never
	// handed it, so that a future change to when the brief is applied cannot
	// resurrect the exit-2 failure on a path nothing exercises.
	next, err := Grok{}.NextTurn(
		"--- operating context ---\nStill the same room.\n"+
			"--- end operating context. The request follows. ---\n\n"+
			"What exact word did you just reply with? Answer with only that word.",
		ws, bin, first.session, PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	second := runLiveGrok(t, next)

	if second.session != first.session {
		t.Errorf("resumed session id = %q, want the first turn's %q",
			second.session, first.session)
	}
	if !strings.Contains(second.text, "LIVEOK") {
		t.Errorf("resumed turn text = %q — it could not recall its own first turn, "+
			"so --resume re-sent rather than resumed", second.text)
	}
}

type liveGrokTurn struct {
	text    string
	session string
	cost    *float64
	acts    []runner.ActCall
}

// runLiveGrok executes one Spec and folds its stdout through ParseEvent. It
// deliberately does not use internal/council/runner: the thing under test is
// the adapter, and borrowing the runner would let a runner bug pass for an
// adapter that works.
func runLiveGrok(t *testing.T, spec runner.Spec) liveGrokTurn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, spec.Binary, spec.Args...)
	cmd.Dir = spec.Dir
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	var turn liveGrokTurn
	sc := bufio.NewScanner(out)
	// grok's tool_call_update carries a whole file's contents in rawOutput, so
	// the default 64K line budget is not enough for a real turn.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		ev, ok := Grok{}.ParseEvent(sc.Bytes())
		if !ok {
			continue
		}
		switch ev.Kind {
		case runner.KindText:
			turn.text += ev.Text
		case runner.KindActivity:
			turn.acts = append(turn.acts, ev.Acts...)
		case runner.KindMeta:
			if ev.SessionID != "" {
				turn.session = ev.SessionID
			}
			if ev.CostUSD != nil {
				turn.cost = ev.CostUSD
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("grok exited with %v; stderr: %s", err, stderr.String())
	}
	t.Logf("text=%q session=%s acts=%d", turn.text, turn.session, len(turn.acts))
	return turn
}
