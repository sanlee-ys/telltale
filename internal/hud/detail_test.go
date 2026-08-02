package hud

import (
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

func paneText(st State) string {
	rows := visibleSessions(st)
	return strings.Join(detailLines(st, rows, PlainStyles(), UnicodeGlyphs()), "\n")
}

// The pane's reason to exist. §4a.1 says absence has two causes and the HUD
// renders them differently; the grid can only draw one of them, because a
// dropped column and an em dash both mean "nothing here". The pane is where
// the difference is stated in words.
func TestDetailPaneSeparatesCantKnowFromAbsentNow(t *testing.T) {
	st := v11State(120, 20)
	st.Detail, st.Cursor = true, 0
	got := paneText(st)

	// Claude cannot source context, cost or quota from disk. Those get no
	// line at all — they are named once, together, as not sourced.
	for _, absent := range []string{"context ", "cost ", "quota "} {
		if strings.Contains(got, "  "+absent) {
			t.Errorf("pane drew a %q line for a field claude declares CapNone\n%s", absent, got)
		}
	}
	if !strings.Contains(got, "not sourced   context_pct, cost, quota") {
		t.Errorf("pane does not name the fields claude cannot source\n%s", got)
	}

	// The Codex row, by contrast, CAN source quota — it just has none right
	// now. That is an em dash, not a missing line.
	st.Cursor = 2
	got = paneText(st)
	if !strings.Contains(got, "quota         —") {
		t.Errorf("a declared-but-empty quota did not render the absent marker\n%s", got)
	}
	if !strings.Contains(got, "not sourced   name, cost, subagents") {
		t.Errorf("pane does not name what codex cannot source\n%s", got)
	}
}

// v1 carried Diagnostics and the Degraded set from adapter to renderer and
// displayed neither. This is the surface that changes that.
func TestDetailPaneShowsDegradedFieldsAndDiagnostics(t *testing.T) {
	st := v11State(120, 20)
	st.Detail, st.Cursor = true, 2
	got := paneText(st)
	if !strings.Contains(got, "degraded      workspace, context_pct") {
		t.Errorf("degraded field names missing\n%s", got)
	}
	if !strings.Contains(got, "2 unparseable records skipped") {
		t.Errorf("diagnostics missing\n%s", got)
	}
	if !strings.Contains(got, "no turn_context record in the read window") {
		t.Errorf("second diagnostic dropped; every note must survive\n%s", got)
	}
}

// A session with nothing wrong says so explicitly. A blank honesty block would
// be indistinguishable from a pane that forgot to render it.
func TestDetailPaneStatesTheAbsenceOfProblems(t *testing.T) {
	st := v11State(120, 20)
	st.Detail, st.Cursor = true, 0
	got := paneText(st)
	if !strings.Contains(got, "degraded      —") || !strings.Contains(got, "diagnostics   —") {
		t.Errorf("a clean session did not state that it is clean\n%s", got)
	}
}

// Extras are display-only by contract (§4a.2) and this is the only place they
// appear. Carrying them and never showing them was the v1 gap.
func TestDetailPaneIsTheOnlyPlaceExtrasAppear(t *testing.T) {
	st := v11State(120, 20)
	grid := Render(st, PlainStyles(), UnicodeGlyphs())
	if strings.Contains(grid, "2.1.219") {
		t.Error("an extra leaked into the grid")
	}
	st.Detail, st.Cursor = true, 0
	pane := paneText(st)
	for _, want := range []string{"branch        main", "cli           2.1.219", "ctx tokens    215k"} {
		if !strings.Contains(pane, want) {
			t.Errorf("extra %q missing from the pane\n%s", want, pane)
		}
	}
}

// Zero sub-agents is a MEASUREMENT. The grid draws no chip for it because a
// chip is a claim; the pane says "0 recent" because a blank there would be
// indistinguishable from a count we could not take.
func TestDetailPaneStatesAMeasuredZeroFanOut(t *testing.T) {
	st := v11State(120, 20)
	st.Detail, st.Cursor = true, 1
	got := paneText(st)
	if !strings.Contains(got, "subagents     ~0 recent") {
		t.Errorf("a measured zero was not stated\n%s", got)
	}
}

// A count the adapter could not take is absent, and absent renders as absent —
// never as zero.
func TestDetailPaneRendersAnUncountableFanOutAsAbsent(t *testing.T) {
	st := v11State(120, 20)
	st.Detail, st.Cursor = true, 0
	rows := visibleSessions(st)
	rows[0].Subagents = nil
	rows[0].Derived = rows[0].Derived.Without(model.FieldSubagents)
	rows[0].Degraded = rows[0].Degraded.With(model.FieldSubagents)
	got := paneText(st)
	if !strings.Contains(got, "subagents     —") {
		t.Errorf("an uncountable fan-out did not render absent\n%s", got)
	}
	if strings.Contains(got, "subagents     ~0") {
		t.Fatal("a failed count rendered as a measured zero")
	}
}

// The liveness classification and the evidence for it travel together: the
// class is the HUD's verdict and the age is what it was drawn from.
func TestDetailPaneStatesLivenessWithItsEvidence(t *testing.T) {
	st := v11State(120, 20)
	st.Detail, st.Cursor = true, 0
	if got := paneText(st); !strings.Contains(got, "activity      live · 12s ago") {
		t.Errorf("activity line missing its evidence\n%s", got)
	}

	// With no timestamp there is no evidence, so there is no age — and the
	// class is "unknown", never "stale".
	st.Snap.Sessions = []*model.Session{
		sess(model.VendorClaude, "id-with-no-clock", `C:\x\y`, "claude-opus-5", 0,
			withName("no-clock"), noActivity(), withSubagents(0)),
	}
	st.Cursor = 0
	got := paneText(st)
	if !strings.Contains(got, "activity      unknown") {
		t.Errorf("a session with no timestamp did not report unknown liveness\n%s", got)
	}
	if strings.Contains(got, "ago") {
		t.Error("an age was rendered for a session with no activity timestamp")
	}
}

// The selection is an index into the visible rows, and rows come and go. A
// pane that silently retargets would relabel one session's diagnostics with
// another's.
func TestDetailPaneSaysSoWhenItsSessionIsGone(t *testing.T) {
	st := v11State(120, 20)
	st.Detail, st.Cursor = true, 9
	if got := paneText(st); !strings.Contains(got, "no longer listed") {
		t.Errorf("an out-of-range selection rendered a pane anyway\n%s", got)
	}
}

// The pane replaces the row area, so it must not be labelled by a column
// header describing columns that are not on screen.
func TestDetailPaneDropsTheColumnHeader(t *testing.T) {
	st := v11State(120, 20)
	st.Detail, st.Cursor = true, 0
	if got := Render(st, PlainStyles(), UnicodeGlyphs()); strings.Contains(got, "SESSION  ") {
		t.Errorf("the column header survived over the detail pane\n%s", got)
	}
}

// Every frame fits its terminal, pane included — including the narrowest tier,
// where the value column has to give way.
func TestDetailPaneFitsEveryWidth(t *testing.T) {
	for _, w := range []int{60, 72, 80, 100, 120, 200} {
		st := v11State(w, 20)
		st.Detail, st.Cursor = true, 0
		out := Render(st, PlainStyles(), UnicodeGlyphs())
		for i, line := range strings.Split(out, "\n") {
			if n := len([]rune(line)); n > w {
				t.Errorf("width %d: pane line %d is %d columns\n%s", w, i, n, line)
			}
		}
	}
}

// Model-authored text reaches the pane too: a diagnostic or an extra carrying
// U+2028 must not tear it, exactly as in the grid.
func TestDetailPaneSanitizesModelAuthoredText(t *testing.T) {
	st := v11State(120, 20)
	st.Detail, st.Cursor = true, 0
	rows := visibleSessions(st)
	rows[0].Extras = append(rows[0].Extras, model.Extra{Label: "note", Value: "before\u2028after"})
	rows[0].Diagnostics = append(rows[0].Diagnostics, "torn\u2029record")
	out := Render(st, PlainStyles(), UnicodeGlyphs())
	if strings.ContainsAny(out, "\u2028\u2029") {
		t.Fatal("a separator character reached the rendered pane")
	}
	for i, line := range strings.Split(out, "\n") {
		if n := len([]rune(line)); n > st.Width {
			t.Fatalf("line %d is %d columns wide, budget is %d", i, n, st.Width)
		}
	}
}

// A quota window with no figure renders the absent marker in the pane for the
// same reason it does in the header: "5h 0%" is the load-bearing lie.
func TestDetailPaneNeverRendersAnEmptyQuotaWindowAsZero(t *testing.T) {
	st := v11State(120, 20)
	st.Detail, st.Cursor = true, 2
	rows := visibleSessions(st)
	rows[2].Quota = []model.QuotaWindow{{ID: "primary", Label: "5h"}}
	got := paneText(st)
	if strings.Contains(got, "0%") {
		t.Errorf("a window with no usage figure rendered as zero\n%s", got)
	}
	if !strings.Contains(got, "5h") {
		t.Errorf("the window vanished instead of rendering absent\n%s", got)
	}
}

func TestDetailPaneCountdownUsesTheSharedFormatter(t *testing.T) {
	st := v11State(120, 20)
	st.Detail, st.Cursor = true, 2
	rows := visibleSessions(st)
	rows[2].Quota = []model.QuotaWindow{
		window("primary", "5h", 42, 2*time.Hour+13*time.Minute),
		window("secondary", "7d", 18, 5*24*time.Hour+2*time.Hour),
	}
	got := paneText(st)
	for _, want := range []string{"↻2h13m", "↻5d02h"} {
		if !strings.Contains(got, want) {
			t.Errorf("countdown %q missing\n%s", want, got)
		}
	}
}
