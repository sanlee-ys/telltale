package council

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/councilhost"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The turn's token count, on every surface that carries the cost.
//
// One seat reports one today — the grok ACP seat, whose prompt response
// carries a per-prompt count beside the session's running total
// (vendors/acp.go, acpMeta) — and the room draws it on the three surfaces the
// cost already has: the badge row, the turn separator, and the turn page. The
// figures here are fakes; the shape is the measured one.

// tokensRoom is a three-seat room whose third seat is the grok ACP seat with
// a finished turn behind it and the current turn done: the badge row carries
// the live count, the separator files the old one.
func tokensRoom() State {
	st := room()
	st.Columns[2] = Column{
		Vendor: model.VendorGrok, Label: "Grok",
		Avail:   AvailInstalled,
		Sandbox: SandboxClaim{Level: SandboxNone, Detail: "measured not to restrict writes"},
		Gran:    GranTokens, Phase: PhaseIdle,
	}
	st.Turn = 2
	g := &st.Columns[2]
	g.startTurn(1, "say your seat name", false)
	g.Body, g.Phase, g.Elapsed = "Grok.", PhaseDone, 4*time.Second
	g.Tokens = &model.TokenCounts{Input: 24567, Output: 40}
	g.startTurn(2, "count the lines in README.md", false)
	g.Body, g.Phase, g.Elapsed = "Forty-one lines.", PhaseDone, 9*time.Second
	g.Tokens = &model.TokenCounts{Input: 32277, Output: 181}
	return st
}

// TestReportedTokensRenderAndAbsentTokensDoNot is the honest-gauge rule on the
// count, the same rule TestReportedCostRendersAndAbsentCostDoesNot states for
// the cost: a vendor that counted zero and a vendor that sent no count must
// not render alike.
func TestReportedTokensRenderAndAbsentTokensDoNot(t *testing.T) {
	if got := render(room()); strings.Contains(got, " in ") || strings.Contains(got, " out ") {
		t.Error("a room with no reported count drew a token cell")
	}

	zero := room()
	zero.Columns[0].Tokens = &model.TokenCounts{}
	if !strings.Contains(render(zero), "in 0 out 0") {
		t.Error("a vendor that counted zero did not render `in 0 out 0`")
	}

	st := tokensRoom()
	got := render(st)
	if !strings.Contains(got, "in 24.5k out 40") {
		t.Error("the filed turn's count is missing from its separator")
	}
	// The live count does not fit a 37-cell column beside the badges, and it
	// leaves WHOLE: the separator above still carries turn 1's, and the turn
	// page carries turn 2's. Widen the room and it comes back, whole.
	if strings.Contains(got, "in 32") {
		t.Errorf("the badge row clipped or squeezed a count it had no room for:\n%s", got)
	}
	golden(t, "reported-tokens", got)

	wide := st
	wide.Width = 160
	if !strings.Contains(render(wide), "in 32.2k out 181") {
		t.Error("a column with room for the count did not draw it")
	}
}

// TestTheCountIsShownWholeOrNotShown is TestTheCostIsShownWholeOrNotShown's
// sweep over the count: at every width from the strip floor up, and beside a
// cost or alone, the badge row carries the whole cell or none of it, and it
// never lets the count cost the cost its place.
func TestTheCountIsShownWholeOrNotShown(t *testing.T) {
	cost := 0.0041
	base := room()
	base.Now = quotaNow
	tok := "in 32.2k out 181"

	type shape struct {
		name string
		col  Column
	}
	var shapes []shape
	for i, c := range base.Columns {
		c.Tokens = &model.TokenCounts{Input: 32277, Output: 181}
		shapes = append(shapes, shape{"seat " + string(rune('1'+i)), c})
		c.CostUSD = &cost
		shapes = append(shapes, shape{"seat " + string(rune('1'+i)) + " with a cost", c})
	}
	for _, g := range []Glyphs{UnicodeGlyphs(), GlyphsFor(true)} {
		for _, sh := range shapes {
			for w := stripWidth; w <= 80; w++ {
				row := badgeRow(base, sh.col, w, PlainStyles(), g)
				if lipgloss.Width(row) > w {
					t.Errorf("%s w=%d: the badge row is %d cells: %q", sh.name, w, lipgloss.Width(row), row)
				}
				if strings.Contains(row, "in ") && !strings.Contains(row, tok) {
					t.Errorf("%s w=%d: a partial count on the row: %q", sh.name, w, row)
				}
				if sh.col.CostUSD != nil && strings.Contains(row, tok) && !strings.Contains(row, "$0.0041") {
					t.Errorf("%s w=%d: the count took the cost's place: %q", sh.name, w, row)
				}
			}
		}
	}
}

// TestACountedTurnWithNoReportedCostDrawsNoCostCell drives the exact event
// the grok ACP seat emits at 1.0.13 — a count and no cost, off a frame that
// carried `costUsdTicks` (vendors' TestGrokACPCountsThisPromptsTokensAndStill
// ShowsNoCost pins the frame; this pins the cells) — through a persistent
// seat, and reads what the column draws. The cost cell is EMPTY, not
// `$0.0000`: the tick is a cumulative figure in a unit that has never been
// checked against a dollar on this seam, and the room shows what it read,
// never what it could compute (vendors/acp.go).
func TestACountedTurnWithNoReportedCostDrawsNoCostCell(t *testing.T) {
	m := turnModel(true)
	m.st.Columns[0].TurnN = 1
	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindMeta, EndsTurn: true,
		Tokens: &model.TokenCounts{Input: 3210, Output: 45},
	}})
	c := &m.st.Columns[0]
	if c.Phase != PhaseDone {
		t.Fatalf("phase = %v, want done", c.Phase)
	}
	if got := tokensCell(*c); got != "in 3.2k out 45" {
		t.Errorf("tokens cell = %q", got)
	}
	if got := costCell(*c); got != "" {
		t.Errorf("cost cell = %q, want nothing: no vendor reported a dollar figure", got)
	}
	// And the filed record keeps the count where the separator reads it.
	c.startTurn(2, "again", false)
	if c.Tokens != nil {
		t.Error("the new turn inherited the old turn's count")
	}
	if len(c.History) != 1 || c.History[0].Tokens == nil || c.History[0].Tokens.Input != 3210 {
		t.Fatalf("the record lost the count: %+v", c.History)
	}
	if got := historyMeta(c.History[0]); !strings.Contains(got, "in 3.2k out 45") {
		t.Errorf("separator meta = %q, want the count", got)
	}
}

// TestTheTurnPageCarriesTheCount: the third surface, in the same spelling.
func TestTheTurnPageCarriesTheCount(t *testing.T) {
	st := tokensRoom()
	st.Page = TurnView{Open: true, Turn: 2}
	if got := render(st); !strings.Contains(got, "in 32.2k out 181") {
		t.Errorf("the turn page does not carry the live turn's count:\n%s", got)
	}
	st.Page = TurnView{Open: true, Turn: 1}
	if got := render(st); !strings.Contains(got, "in 24.5k out 40") {
		t.Errorf("the turn page does not carry the filed turn's count:\n%s", got)
	}
}

// TestAHostedSeatsCountCrossesTheWire: nil stays nil, a count stays a count,
// and a zero stays a zero — on the seat and in its history.
func TestAHostedSeatsCountCrossesTheWire(t *testing.T) {
	s := councilhost.Seat{Vendor: model.VendorGrok, Drivable: true, Phase: councilhost.PhaseDone, Turn: 2,
		Tokens:  &councilhost.TokenCounts{In: 32277, Out: 181},
		History: []councilhost.TurnRecord{{N: 1, Phase: councilhost.PhaseDone, Tokens: &councilhost.TokenCounts{}}},
	}
	c := columnFromSeat(s, Column{}, true, true)
	if c.Tokens == nil || *c.Tokens != (model.TokenCounts{Input: 32277, Output: 181}) {
		t.Fatalf("tokens = %+v", c.Tokens)
	}
	if len(c.History) != 1 || c.History[0].Tokens == nil || *c.History[0].Tokens != (model.TokenCounts{}) {
		t.Fatalf("history tokens = %+v", c.History)
	}
	s.Tokens = nil
	if c := columnFromSeat(s, Column{}, true, true); c.Tokens != nil {
		t.Fatalf("a seat that sent no count arrived with one: %+v", c.Tokens)
	}
}
