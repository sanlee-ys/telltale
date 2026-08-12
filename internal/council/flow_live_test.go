//go:build live

package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/sanlee-ys/telltale/internal/model"
)

// Live end-to-end: real cursor-agent then real codex through /flow auto-advance.
//
//	go test ./internal/council -tags=live -run TestLiveFlowCursorThenCodex -v -count=1 -timeout 10m
func TestLiveFlowCursorThenCodex(t *testing.T) {
	if testing.Short() {
		t.Skip("live e2e")
	}
	seats, err := ParseSeats("cursor,codex")
	if err != nil {
		t.Fatal(err)
	}
	// The repo root rather than a temp dir. That used to be forced — print-mode
	// cursor-agent tripped its workspace-trust prompt in an unfamiliar directory
	// and exited with an empty body — and on the ACP seat it no longer is: trust
	// does not apply on that path, measured in §9.36 by writing a file into the
	// very directory print mode had just refused. It stays because codex is the
	// second hop and because a live test that runs where the repo lives is easier
	// to read the failures of.
	ws, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws, "go.mod")); err != nil {
		t.Fatalf("workspace %s is not the telltale module root: %v", ws, err)
	}
	m := newWithBrief(Options{
		Dir:   ws,
		Seats: seats,
		Write: true,
		Fresh: true,
	}, Brief{}, GateHook{}, Reattachment{})

	cursor := m.column(model.VendorCursor)
	codex := m.column(model.VendorCodex)
	if cursor == nil || cursor.Avail != AvailInstalled {
		t.Fatalf("cursor not seated: %+v", cursor)
	}
	if codex == nil || codex.Avail != AvailInstalled {
		t.Fatalf("codex not seated: %+v", codex)
	}

	m.st.Draft = `/flow @cursor Reply with exactly one token: ALPHA. No other text. Do not edit files. -> @codex Reply with exactly: AUDITED ALPHA. Include the predecessor artifact's token. Do not edit files.`
	cmd := m.dispatch()
	if cmd == nil {
		t.Fatalf("first hop did not dispatch: notice=%q", m.st.Notice)
	}
	t.Logf("hop marker: %d/%d @%s", m.st.FlowHop, m.st.FlowSteps, m.st.FlowVendor)

	deadline := time.Now().Add(8 * time.Minute)
	pump(t, m, cmd, deadline)

	gotCursor := strings.TrimSpace(cursor.Body)
	gotCodex := strings.TrimSpace(codex.Body)
	t.Logf("cursor phase=%v note=%q body:\n%s", cursor.Phase, cursor.Note, gotCursor)
	t.Logf("codex phase=%v note=%q body:\n%s", codex.Phase, codex.Note, gotCodex)
	t.Logf("notice: %s", m.st.Notice)
	t.Logf("flowHop=%d advance=%v chain=%v", m.st.FlowHop, m.flowAdvancePending, m.flowChain != nil)

	if n := strings.Count(strings.ToUpper(gotCursor), "ALPHA"); n != 1 {
		t.Fatalf("cursor hop contains ALPHA %d times, want exactly once:\n%s", n, gotCursor)
	}
	if !strings.Contains(strings.ToUpper(gotCodex), "AUDIT") {
		t.Fatalf("codex hop missing AUDIT:\n%s", gotCodex)
	}
	if n := strings.Count(strings.ToUpper(gotCodex), "ALPHA"); n != 1 {
		t.Fatalf("codex hop contains ALPHA %d times, want exactly once (predecessor artifact must be clean):\n%s", n, gotCodex)
	}
}

func pump(t *testing.T, m *Model, cmd tea.Cmd, deadline time.Time) {
	t.Helper()
	for cmd != nil {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: notice=%q flowHop=%d advance=%v turn=%v",
				m.st.Notice, m.st.FlowHop, m.flowAdvancePending, m.turn != nil)
		}
		msg := cmd()
		if msg == nil {
			if m.turn != nil {
				cmd = m.waitEvents()
				continue
			}
			if m.flowAdvancePending {
				m.flowAdvancePending = false
				cmd = m.dispatch()
				continue
			}
			return
		}
		nextModel, next := m.Update(msg)
		var ok bool
		m, ok = nextModel.(*Model)
		if !ok {
			t.Fatalf("Update returned %T, want *Model", nextModel)
		}
		cmd = next
	}
}
