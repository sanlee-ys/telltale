package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// TestTheRouteCellPricesTheTurn. Post-#99 the expensive route is always
// explicit — silence goes to Claude alone — so the cell that shows the route is
// the cell that should show what it costs, at the one moment the user can still
// change it.
func TestTheRouteCellPricesTheTurn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		draft string
		want  string // the whole routing cell, as the plain copy renders it
	}{
		// One seat states no count. A route whose own words name every seat it
		// reaches does not need a number saying "one".
		{"default", "go", "→ claude"},
		{"single mention", "@codex go", "→ codex"},
		{"two mentions", "@codex @agy go", "→ codex, agy (2 seats)"},
		{"everyone", "@all go", "→ everyone (3 seats)"},
		{"negated", "-@codex go", "→ everyone but codex (2 seats)"},
		// The refusal keeps its label untouched: it addresses nobody, so there
		// is nothing to price, and the one thing this cell owes a reader
		// mid-typing is what is wrong with the line.
		{"mixed", "@claude -@codex go", "→ mixed @ and -@"},
	} {
		st := room() // three seats, all drivable
		st.Mode = ModeComposing
		st.Route, _ = ParseRoute(tc.draft)
		_, plain := hints(PlainStyles(), UnicodeGlyphs(), modeHints(st, UnicodeGlyphs()))
		cell := strings.SplitN(plain, "  "+UnicodeGlyphs().Sep+"  ", 2)[0]
		if cell != tc.want {
			t.Errorf("%s: routing cell = %q, want %q", tc.name, cell, tc.want)
		}
	}
}

// TestTheBillCountsSeatsAndNotMentions is the honesty half. A route may name a
// vendor that is not in the room; dispatch will not spawn it, so the footer must
// not charge for it.
func TestTheBillCountsSeatsAndNotMentions(t *testing.T) {
	st := deadSeats() // codex not installed, agy unusable — one drivable seat
	st.Mode = ModeComposing

	st.Route, _ = ParseRoute("@claude @codex @agy go")
	if n := st.SeatsIn(st.Route); n != 1 {
		t.Errorf("SeatsIn = %d, want only the seat that can actually be dispatched to", n)
	}
	_, plain := hints(PlainStyles(), UnicodeGlyphs(), modeHints(st, UnicodeGlyphs()))
	if strings.Contains(plain, "seats") {
		t.Errorf("the footer billed three seats in a room with one: %q", plain)
	}

	// The same arithmetic the dispatch gate uses, asserted as the same
	// arithmetic rather than as two numbers that happen to agree today.
	m := &Model{st: st, glyphs: GlyphsFor(false)}
	if got, want := m.seatedIn(st.Route), st.SeatsIn(st.Route); got != want {
		t.Errorf("dispatch counts %d seats and the footer quotes %d", got, want)
	}

	// --vendor is the other half: a seat that is drivable but left out of the
	// room is not dispatched to and must not be billed.
	full := room()
	full.Mode = ModeComposing
	full.Seats = Seats{Only: []model.VendorID{model.VendorClaude, model.VendorCodex}}
	full.Route = Route{}
	if n := full.SeatsIn(full.Route); n != 2 {
		t.Errorf("SeatsIn = %d, want the two seats --vendor left in the room", n)
	}
}

// TestTheHeaderCarriesTheLiveTurnsRoute. Once the turn is dispatched the
// composer has cleared and its routing cell has reset to the next draft's
// default, so while four vendors are working the room has nowhere else to say
// where this turn went.
func TestTheHeaderCarriesTheLiveTurnsRoute(t *testing.T) {
	st := room()
	st.Turn = 10
	st.Columns[0].Phase = PhaseStreaming
	r := Route{}
	st.TurnRoute = &r

	head := strings.Split(render(st), "\n")[0]
	if !strings.Contains(head, "turn 10 → everyone") {
		t.Errorf("the header does not carry the live turn's route: %q", head)
	}

	// It uses the route's OWN label(), so what is displayed is what would have
	// to be typed to produce it — never a second routing vocabulary.
	named := Route{Vendors: []model.VendorID{model.VendorCodex, model.VendorAntigravity}}
	st.TurnRoute = &named
	if head := strings.Split(render(st), "\n")[0]; !strings.Contains(head, "turn 10 → codex, agy") {
		t.Errorf("the header invented its own routing words: %q", head)
	}
	if got, want := named.label(), "codex, agy"; got != want {
		t.Errorf("label() = %q, want the header to be printing %q", got, want)
	}

	// And it reverts the moment the turn is over. The route is history then, and
	// each column's transcript already records its own participation.
	st.TurnRoute = nil
	head = strings.Split(render(st), "\n")[0]
	if strings.Contains(head, "→") {
		t.Errorf("the header still names a route between turns: %q", head)
	}
	if !strings.Contains(head, "turn 10") {
		t.Errorf("the header lost the turn number with the route: %q", head)
	}
}

// TestTheLiveRouteIsSetAtDispatchAndRetiredWithTheTurn pins the two ends the
// header depends on. A route left set after the turn would make the header
// describe history; one never set would leave the live cell blank.
func TestTheLiveRouteIsSetAtDispatchAndRetiredWithTheTurn(t *testing.T) {
	// Nil is ABSENT, and it is not the same as Route{}, which is a real route
	// meaning everyone. A value field could not tell the two apart (§4a.1).
	if st := NewState(); st.TurnRoute != nil {
		t.Error("a room with no turn in flight already claims a route")
	}

	m := &Model{st: room(), glyphs: GlyphsFor(false)}
	sent := Route{Vendors: []model.VendorID{model.VendorCodex}}
	m.st.TurnRoute = &sent
	m.holdTurn(&turnState{
		cancel: func() {},
		live:   map[model.VendorID]bool{model.VendorCodex: true},
	})
	m.turnColumnFinished(model.VendorCodex)
	if m.st.TurnRoute != nil {
		t.Errorf("the last column landed and the header still claims %q", m.st.TurnRoute.label())
	}
}

// TestAFlowHopStatesNoRoute. A hop goes to exactly one named seat (§9.16) and
// the cell immediately to the right already says which — so the route here
// would be the header saying the same thing twice, and the arrow would read as
// pointing at the hop rather than at the turn.
func TestAFlowHopStatesNoRoute(t *testing.T) {
	st := room()
	st.Turn = 4
	st.FlowHop, st.FlowSteps, st.FlowVendor = 1, 3, model.VendorCodex
	r := Route{Vendors: []model.VendorID{model.VendorCodex}}
	st.TurnRoute = &r

	head := strings.Split(render(st), "\n")[0]
	if strings.Contains(head, "→") {
		t.Errorf("a flow hop's header states the route the hop cell already names: %q", head)
	}
	if !strings.Contains(head, "hop 1/3 @codex") {
		t.Errorf("the hop cell is gone: %q", head)
	}
}

// TestTheHeaderShedsTheRouteBeforeThePath. A fact with a home elsewhere yields
// to facts that have none: the route is in the composer a keystroke earlier and
// in the transcript a moment later, while the workspace is nowhere else at all
// — and it is the one that changes what the agents can see.
func TestTheHeaderShedsTheRouteBeforeThePath(t *testing.T) {
	st := room()
	st.Turn = 10
	st.Workspace = "/home/dev/code/telltale"
	r := Route{}
	st.TurnRoute = &r

	if head := strings.Split(render(st), "\n")[0]; !strings.Contains(head, "→ everyone") ||
		!strings.Contains(head, "telltale") {
		t.Fatalf("the wide header should hold both the route and the path: %q", head)
	}

	// Narrow until the two cannot both fit. The route goes; the path, the counts
	// and the turn number all stay.
	for w := 120; w >= MinWidth; w -= 2 {
		st.Width = w
		head := strings.Split(render(st), "\n")[0]
		if strings.Contains(head, "→ everyone") {
			continue
		}
		if !strings.Contains(head, "turn 10") ||
			!strings.Contains(head, "seated") ||
			!strings.Contains(head, "brief") {
			t.Errorf("w=%d: shedding the route cost the header a fact that lives nowhere else: %q", w, head)
		}
		if !strings.Contains(head, "telltale") && !strings.Contains(head, UnicodeGlyphs().Ellipsis) {
			t.Errorf("w=%d: the path went before the route did: %q", w, head)
		}
		return
	}
	t.Error("the route never sheds — the header has no narrow behaviour to test")
}

// TestTheRouteBillSurvivesASCII. Every distinction is carried by a word or a
// number first, so --ascii and NO_COLOR read identically (§9.11). The count is
// only ever MUTED, never coloured, so PlainStyles renders it verbatim.
func TestTheRouteBillSurvivesASCII(t *testing.T) {
	st := room()
	st.ASCII = true
	st.Mode = ModeComposing
	st.Turn = 10
	st.Route = Route{}
	r := Route{}
	st.TurnRoute = &r

	got := Render(st, PlainStyles(), GlyphsFor(true))
	for _, want := range []string{"turn 10 → everyone", "→ everyone (3 seats)"} {
		if !strings.Contains(got, want) {
			t.Errorf("--ascii dropped %q\n%s", want, got)
		}
	}
}

// TestTheRouteBillGolden pins the two cells in one frame.
func TestTheRouteBillGolden(t *testing.T) {
	st := room()
	st.Turn = 10
	st.Mode = ModeComposing
	st.Draft = "@all where does this leave the gate?"
	st.Route, _ = ParseRoute(st.Draft)
	r := Route{}
	st.TurnRoute = &r
	st.Columns[0].Phase = PhaseStreaming
	st.Columns[1].Phase = PhaseWaiting
	golden(t, "route-bill", render(st))
}
