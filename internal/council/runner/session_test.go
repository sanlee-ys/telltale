package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// parseSession models the one thing that distinguishes a persistent stream from
// a batch one: turns end with a LINE, not with a process exit.
func parseSession(line []byte) (Event, bool) {
	var v struct {
		T   string `json:"t"`
		End bool   `json:"end"`
	}
	if err := json.Unmarshal(line, &v); err != nil {
		return Event{}, false
	}
	if v.End {
		return Event{Kind: KindMeta, Text: v.T, EndsTurn: true}, true
	}
	return Event{Kind: KindText, Text: v.T}, true
}

// turnLine is one turn in the envelope the real vendor takes, so the test
// exercises marshalling rather than a shape invented for the test.
func turnLine(t *testing.T, content string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": content},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// awaitTurn drains until an end-of-turn event, returning the text seen.
func awaitTurn(t *testing.T, ch <-chan Event) (texts []string, last Event) {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case ev := <-ch:
			switch {
			case ev.EndsTurn:
				return texts, ev
			case ev.Kind == KindText:
				texts = append(texts, ev.Text)
			case ev.Kind == KindDone, ev.Kind == KindError:
				return texts, ev
			}
		case <-deadline:
			t.Fatal("timed out waiting for the turn to end")
		}
	}
}

// TestSessionTakesManyTurnsOnOneProcess is the whole point of the type: three
// turns, one child, no session init in between.
func TestSessionTakesManyTurnsOnOneProcess(t *testing.T) {
	ch := make(chan Event, 64)
	s, err := StartSession(context.Background(), helperSpec(t, "session"), ch, parseSession)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Kill()

	for _, want := range []string{"one", "two", "three"} {
		if err := s.Send(turnLine(t, want)); err != nil {
			t.Fatalf("send %q: %v", want, err)
		}
		texts, last := awaitTurn(t, ch)
		if !last.EndsTurn {
			t.Fatalf("turn %q ended with %v, not an end-of-turn line (note %q)",
				want, last.Kind, last.Note)
		}
		if got := strings.Join(texts, ""); got != want {
			t.Errorf("turn text = %q, want %q", got, want)
		}
		if !s.Alive() {
			t.Fatalf("the process died after the %q turn; it must outlive a turn", want)
		}
	}
}

// TestSessionSurvivesAnInterrupt: a control message ends the turn and leaves the
// process able to take the next one. That is what makes cancelling cheap —
// killing would work too, and would throw away the session init the room paid
// for.
func TestSessionSurvivesAnInterrupt(t *testing.T) {
	ch := make(chan Event, 64)
	s, err := StartSession(context.Background(), helperSpec(t, "session"), ch, parseSession)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Kill()

	ctl, err := json.Marshal(map[string]any{
		"type":       "control_request",
		"request_id": "int-1",
		"request":    map[string]any{"subtype": "interrupt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(ctl); err != nil {
		t.Fatal(err)
	}
	if _, last := awaitTurn(t, ch); !last.EndsTurn || last.Text != "interrupt" {
		t.Fatalf("interrupt ended the turn as %+v, want an end-of-turn from the interrupt", last)
	}

	if err := s.Send(turnLine(t, "after")); err != nil {
		t.Fatalf("the process would not take a turn after an interrupt: %v", err)
	}
	texts, last := awaitTurn(t, ch)
	if !last.EndsTurn || strings.Join(texts, "") != "after" {
		t.Errorf("post-interrupt turn = %v / %+v, want the process still answering", texts, last)
	}
}

// TestSessionDeathMidTurnIsTerminal: the column must not hang. A batch child
// says "the turn is over" by dying; a persistent one says it with a line, so a
// process that dies instead has to produce a terminal event of its own or the
// room waits forever.
func TestSessionDeathMidTurnIsTerminal(t *testing.T) {
	ch := make(chan Event, 64)
	s, err := StartSession(context.Background(), helperSpec(t, "session-dies"), ch, parseSession)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Kill()

	if err := s.Send(turnLine(t, "hello")); err != nil {
		t.Fatal(err)
	}
	texts, last := awaitTurn(t, ch)
	if last.Kind != KindError {
		t.Fatalf("terminal event = %v (note %q), want KindError", last.Kind, last.Note)
	}
	if last.EndsTurn {
		t.Error("a process that died reported EndsTurn; that signal belongs to the vendor's own line")
	}
	if !strings.Contains(last.Note, "the vendor fell over") {
		t.Errorf("note = %q, want the vendor's own stderr quoted", last.Note)
	}
	// The partial output really was produced and must survive.
	if strings.Join(texts, "") != "partial" {
		t.Errorf("texts = %v, want the partial output kept", texts)
	}
}

// TestSendAfterDeathIsReported: writing into a dead pipe must fail loudly.
// Silently swallowing the turn would leave a column waiting on an answer that
// was never asked for.
func TestSendAfterDeathIsReported(t *testing.T) {
	ch := make(chan Event, 64)
	s, err := StartSession(context.Background(), helperSpec(t, "session-dies"), ch, parseSession)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Send(turnLine(t, "hello")); err != nil {
		t.Fatal(err)
	}
	awaitTurn(t, ch)

	deadline := time.Now().Add(10 * time.Second)
	for s.Alive() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if err := s.Send(turnLine(t, "anyone there")); err != ErrSessionClosed {
		t.Fatalf("send to a dead session = %v, want ErrSessionClosed", err)
	}
}

// TestSessionKillReapsTheWholeTree: a process that outlives a turn by design is
// exactly the one that would outlive the room by accident, so it gets the same
// job-object teardown a spawn-per-turn child gets.
func TestSessionKillReapsTheWholeTree(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ticks")
	ch := make(chan Event, 64)
	s, err := StartSession(context.Background(),
		helperSpec(t, "spawn-grandchild", marker), ch, parseSession)
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for {
		if fi, err := os.Stat(marker); err == nil && fi.Size() > 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the grandchild never started writing")
		}
		time.Sleep(20 * time.Millisecond)
	}

	s.Kill()
	time.Sleep(500 * time.Millisecond)
	before := size(t, marker)
	time.Sleep(750 * time.Millisecond)
	if after := size(t, marker); after != before {
		t.Fatalf("the grandchild is still running: marker grew %d -> %d bytes after Kill",
			before, after)
	}
}

// TestRoomCancellationKillsTheSession: the room's context is the only one that
// ends these processes, and it must actually end them.
func TestRoomCancellationKillsTheSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan Event, 16)
	s, err := StartSession(ctx, helperSpec(t, "session"), ch, parseSession)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-s.Done():
	case <-time.After(20 * time.Second):
		t.Fatal("the session outlived the room's context")
	}
}

// TestSendDoesNotBlockTheCaller. Send is called from the Bubble Tea update loop,
// so a vendor that has stopped reading its stdin must not be able to take the
// room's input handling down with it — including the key that cancels it.
func TestSendDoesNotBlockTheCaller(t *testing.T) {
	ch := make(chan Event, 16)
	// "sleep" never reads stdin at all, which is the worst case: the pipe fills
	// and stays full.
	s, err := StartSession(context.Background(), helperSpec(t, "sleep"), ch, parseSession)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Kill()

	big := make([]byte, 256<<10)
	for i := range big {
		big[i] = 'x'
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		// More than the queue holds, each larger than any pipe buffer. Every
		// call must return, whether it queued the line or refused it.
		for i := 0; i < sendQueue*4; i++ {
			if err := s.Send(big); err != nil && err != ErrSendBacklog && err != ErrSessionClosed {
				t.Errorf("send %d = %v, want nil, ErrSendBacklog or ErrSessionClosed", i, err)
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Send blocked on a child that never reads its stdin")
	}
}
