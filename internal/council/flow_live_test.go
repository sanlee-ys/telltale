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
	// Temp dirs trip cursor-agent's workspace-trust prompt and exit with an
	// empty body. Use the repo root — already trusted on a dogfood machine.
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
	}, Brief{}, HookSet{}, Reattachment{})

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

	if !strings.Contains(strings.ToUpper(gotCursor), "ALPHA") {
		t.Fatalf("cursor hop missing ALPHA:\n%s", gotCursor)
	}
	if !strings.Contains(strings.ToUpper(gotCodex), "AUDIT") {
		t.Fatalf("codex hop missing AUDIT:\n%s", gotCodex)
	}
	if !strings.Contains(strings.ToUpper(gotCodex), "ALPHA") {
		t.Fatalf("codex hop did not see ALPHA from predecessor (auto-advance feed broken):\n%s", gotCodex)
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
