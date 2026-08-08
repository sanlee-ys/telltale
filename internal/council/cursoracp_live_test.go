//go:build live

package council

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// Live end-to-end for the ACP seat: one real cursor-agent process, two turns,
// one of them running a tool.
//
//	go test ./internal/council -tags=live -run TestLiveCursorACPMultiTurn -v -count=1 -timeout 10m
//
// It exists because everything else about this seat is replay. The fixture tests
// prove the parser reads the shapes §9.36 captured; only this proves the shapes
// are still what a live vendor sends, through the merged code rather than
// through an instrument written beside it.
//
// The two assertions are the two claims the switch was made for:
//
//   - Turn two answers a question only turn one's history could answer, which is
//     what "one process, one conversation" means from the user's side.
//   - Turn two is DRAMATICALLY faster than turn one, because the ~2-4s handshake
//     is paid once. The threshold is loose on purpose — this is a network-bound
//     model call and a tight bound would be a flaky test — but the shape it
//     rules out is the one that matters: a seat that quietly went back to paying
//     a full startup per turn.
func TestLiveCursorACPMultiTurn(t *testing.T) {
	if testing.Short() {
		t.Skip("live e2e")
	}
	seats, err := ParseSeats("cursor")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	m := newWithBrief(Options{
		Dir: ws, Seats: seats, Write: true, Fresh: true,
	}, Brief{}, HookSet{}, Reattachment{})

	cursor := m.column(model.VendorCursor)
	if cursor == nil || cursor.Avail != AvailInstalled {
		t.Fatalf("cursor not seated: %+v", cursor)
	}

	deadline := time.Now().Add(8 * time.Minute)

	// Turn one runs a TOOL. go.mod is a file only a read of this workspace can
	// answer about, and the module line is a fact no model would guess.
	m.st.Draft = "@cursor Read go.mod in this directory and reply with the module path on its `module` line, and nothing else."
	cmd := m.dispatch()
	if cmd == nil {
		t.Fatalf("first turn did not dispatch: notice=%q", m.st.Notice)
	}
	pump(t, m, cmd, deadline)

	first := cursor.Elapsed
	body := strings.TrimSpace(cursor.Body)
	t.Logf("turn 1 phase=%v elapsed=%s note=%q body:\n%s", cursor.Phase, first, cursor.Note, body)
	for _, a := range cursor.Acts {
		t.Logf("  act %q -> %v %s", a.Text, a.Status, a.Detail)
	}
	if !strings.Contains(body, "github.com/sanlee-ys/telltale") {
		t.Fatalf("turn 1 did not read the workspace:\n%s", body)
	}
	if len(cursor.Acts) == 0 {
		t.Error("a turn that read a file recorded no activity; the trace is blind to ACP tool calls")
	}

	proc := m.procs[model.VendorCursor]
	if proc == nil {
		t.Fatal("the seat has no process after its first turn")
	}
	if !proc.sess.Alive() {
		t.Fatal("the process exited at the end of the turn; this seat is supposed to outlive one")
	}

	// Turn two: same process, and a question only turn one's history answers.
	m.st.Draft = "@cursor What was the last path you just reported? Reply with that path and nothing else."
	cmd = m.dispatch()
	if cmd == nil {
		t.Fatalf("second turn did not dispatch: notice=%q", m.st.Notice)
	}
	pump(t, m, cmd, deadline)

	second := cursor.Elapsed
	body2 := strings.TrimSpace(cursor.Body)
	t.Logf("turn 2 phase=%v elapsed=%s note=%q body:\n%s", cursor.Phase, second, cursor.Note, body2)

	if m.procs[model.VendorCursor] != proc {
		t.Error("the second turn ran on a different process; the conversation was not continuous")
	}
	if proc.sent != 2 {
		t.Errorf("the process was handed %d turns, want 2", proc.sent)
	}
	if !strings.Contains(body2, "github.com/sanlee-ys/telltale") {
		t.Fatalf("turn 2 could not see turn 1's history:\n%s", body2)
	}
	// A seat back on spawn-per-turn would pay ~8.1s of process cost here (§9.33).
	if second >= first {
		t.Errorf("turn 2 (%s) was not faster than turn 1 (%s) — the handshake looks like it is being paid again",
			second, first)
	}
}
