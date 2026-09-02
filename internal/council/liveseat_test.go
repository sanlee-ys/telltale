package council

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The live seat's tests (design.md §9.53).
//
// Not one of them starts a process. They drive the emulator and the render path
// directly, which is the shape arenacheck_test.go uses for the check var and
// the only shape the package's spawn guard allows: a pseudoconsole child is
// `claude` running on the operator's own account.

// liveScript is the fixed byte stream every test in this file feeds the
// emulator, and it is chosen to carry two things at once.
//
// It carries ESCAPES that must be CONSUMED — an erase-display, a cursor home,
// an absolute cursor position that overwrites earlier text, a colour run, and
// the window-manipulation sequence that would resize the operator's real
// terminal if it ever reached one.
//
// It also carries NUMBERS THAT MUST NOT BE MEASURED. The cost, the weekly
// limit, the elapsed figure and the posture word are all shapes this room
// renders for real, from the structured path, on the very same column. If any
// of them can reach a gauge from here, this is where it shows.
const liveScript = "\x1b[2J\x1b[H" +
	"claude interactive\r\n" +
	"\x1b[32mready\x1b[0m\r\n" +
	"$12.34 spent | 75% of weekly limit | 4.2s | ro:enforced\r\n" +
	"second line\r\n" +
	"\x1b[2;1Hoverwritten\r\n" +
	"\x1b[8;20;60t"

// liveGridFrom decodes a script the way the model does, without a model.
func liveGridFrom(t *testing.T, script string, cols, rows int) []string {
	t.Helper()
	e := vt.NewEmulator(cols, rows)
	e.SetScrollbackSize(liveScrollback)
	if _, err := e.WriteString(script); err != nil {
		t.Fatalf("emulator refused the script: %v", err)
	}
	m := &Model{emu: e}
	return m.liveGrid()
}

// liveRoom is room() with a live Claude seat holding a decoded screen.
func liveRoom(t *testing.T) State {
	t.Helper()
	st := room()
	cols, rows, ok := livePaneRectFor(t, st, model.VendorClaude)
	if !ok {
		t.Fatal("the fixture room has no live pane to draw into")
	}
	st.Live = LiveSeat{
		Seat:  model.VendorClaude,
		Phase: LiveShowing,
		Cols:  cols, Rows: rows,
		Grid: liveGridFrom(t, liveScript, cols, rows),
	}
	return st
}

// livePaneRectFor asks the pure geometry what rectangle a seat's pane will get,
// by putting the seat far enough into LiveShowing for the question to have an
// answer.
func livePaneRectFor(t *testing.T, st State, v model.VendorID) (int, int, bool) {
	t.Helper()
	probe := st
	probe.Live = LiveSeat{Seat: v, Phase: LiveShowing}
	return livePaneRect(probe)
}

// TestLiveSeatMeasuresNothing is the display-only contract, asserted as a
// property of the update path rather than as a review note.
//
// It feeds the model a screen carrying a dollar cost, a weekly-limit
// percentage, an elapsed figure and a posture word — every shape this room
// renders for real on that same column — and then demands that the ONLY thing
// the whole State gained is Live. A future edit that reached for a cost, a
// phase or a quota here fails here, which is the point: §4a.1 and ADR-001 both
// turn on a number's provenance, and a number read off a repaint has none.
func TestLiveSeatMeasuresNothing(t *testing.T) {
	m := &Model{st: room(), styles: PlainStyles(), glyphs: GlyphsFor(false)}
	m.st.Live = LiveSeat{Seat: model.VendorClaude, Phase: LiveOpening}
	m.emu = vt.NewEmulator(60, 12)
	m.emu.SetScrollbackSize(liveScrollback)

	before := m.st
	before.Live = LiveSeat{}

	m.applyPTY([]runner.PTYChunk{
		{Vendor: model.VendorClaude, Data: []byte(liveScript[:20])},
		{Vendor: model.VendorClaude, Data: []byte(liveScript[20:])},
	})

	after := m.st
	after.Live = LiveSeat{}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("a live screen changed State outside Live.\nbefore: %+v\nafter:  %+v", before, after)
	}
	if m.st.Live.Phase != LiveShowing {
		t.Fatalf("a screen that painted should be showing, got phase %d", m.st.Live.Phase)
	}
	if len(m.st.Live.Grid) == 0 {
		t.Fatal("the emulator decoded nothing, so this test proved nothing")
	}

	// The seat's own column is untouched, field by field, and this is the half
	// a DeepEqual over State could pass by accident if Live ever grew a Column.
	col := m.st.Columns[0]
	if col.Vendor != model.VendorClaude {
		t.Fatalf("fixture drift: column 0 is %s", col.Vendor)
	}
	if col.CostUSD != nil {
		t.Errorf("a cost reached the seat from a picture of a screen: %v", *col.CostUSD)
	}
	if col.Quota != nil {
		t.Errorf("a quota reading reached the seat from a picture of a screen: %+v", *col.Quota)
	}
	if col.Elapsed != 0 {
		t.Errorf("an elapsed figure reached the seat from a picture of a screen: %v", col.Elapsed)
	}
	if col.Body != "" {
		t.Errorf("pty bytes reached Column.Body, which is the sanitize path: %q", col.Body)
	}
	if col.Phase != PhaseIdle {
		t.Errorf("a screen moved the seat's phase to %v; only the structured path may", col.Phase)
	}
	if col.Sandbox.Detail != "tool allowlist" {
		t.Errorf("the posture claim moved to %q; the screen said ro:enforced", col.Sandbox.Detail)
	}
}

// TestLiveSeatNumbersStayInTheGrid is the same contract seen from the RENDER
// side, and it is a different assertion rather than a second spelling.
//
// applyPTY could be perfect and a renderer could still hoist a figure out of
// the grid into the badge row. So this reads the frame: every planted number
// must appear only among the rows the pane drew, and never above them.
func TestLiveSeatNumbersStayInTheGrid(t *testing.T) {
	st := liveRoom(t)
	frame := strings.Split(render(st), "\n")

	// The chrome region of the live column ends where its grid begins. Found by
	// the marker row the pane always draws, so the split is taken from the frame
	// itself rather than from an arithmetic the renderer could change.
	marker := -1
	for i, l := range frame {
		if strings.Contains(l, "display only") {
			marker = i
			break
		}
	}
	if marker < 0 {
		t.Fatal("the live pane drew no marker row, so it never said what it is")
	}
	for _, planted := range []string{"$12.34", "75%", "4.2s", "ro:enforced"} {
		for i := 0; i <= marker; i++ {
			if strings.Contains(frame[i], planted) {
				t.Errorf("%q reached the frame above the live grid, on row %d: %q",
					planted, i, frame[i])
			}
		}
	}
}

// TestLivePaneConsumesEscapes pins the reason the emulator is mandatory.
//
// fit cannot make these bytes safe: lipgloss.Width counts a cursor move and an
// erase as zero cells, so a width helper passes them straight through into
// telltale's frame where they execute against the HOST terminal. One of the
// sequences in liveScript asks the real terminal to resize itself to 20 by 60.
func TestLivePaneConsumesEscapes(t *testing.T) {
	frame := render(liveRoom(t))
	if strings.ContainsRune(frame, 0x1b) {
		t.Error("an escape byte survived into the rendered frame")
	}
	for _, s := range []string{"[8;20;60t", "[2J", "[2;1H", "[32m"} {
		if strings.Contains(frame, s) {
			t.Errorf("the escape payload %q reached the frame as literal text", s)
		}
	}
	// The proof the escapes were ACTED on rather than merely dropped: the
	// absolute cursor position overwrote the line the erase had cleared.
	if !strings.Contains(frame, "overwritten") {
		t.Error("the cursor-position escape was dropped instead of consumed")
	}
	if strings.Contains(frame, "ready") {
		t.Error("the row the guest overwrote is still on screen")
	}
}

// TestLivePaneRectAgreesWithWhatIsDrawn keeps the update loop and the renderer
// from disagreeing about the pane.
//
// livePaneRect is what Update sizes the pseudoconsole from, and columnCell is
// what draws it. If those two ever differ the guest paints to a width the pane
// does not have, which reads as a rendering bug and is a resize bug.
func TestLivePaneRectAgreesWithWhatIsDrawn(t *testing.T) {
	for _, size := range [][2]int{{120, 24}, {120, 40}, {100, 30}, {160, 50}} {
		st := room()
		st.Width, st.Height = size[0], size[1]
		cols, rows, ok := livePaneRectFor(t, st, model.VendorClaude)
		if !ok {
			t.Fatalf("%dx%d: no pane", size[0], size[1])
		}
		st.Live = LiveSeat{
			Seat: model.VendorClaude, Phase: LiveShowing, Cols: cols, Rows: rows,
			// One numbered row per emulator row, so a pane that drew the wrong
			// number of them says WHICH rows went missing.
			Grid: numberedGrid(rows),
		}
		frame := render(st)
		for i := 0; i < rows; i++ {
			want := "row-" + itoa(i)
			if !strings.Contains(frame, want) {
				t.Errorf("%dx%d: pane row %d (%q) is not on screen; the rect said %dx%d",
					size[0], size[1], i, want, cols, rows)
			}
		}
		if strings.Contains(frame, "row-"+itoa(rows)) {
			t.Errorf("%dx%d: the pane drew more rows than the rect allowed %d",
				size[0], size[1], rows)
		}
	}
}

func numberedGrid(rows int) []string {
	out := make([]string, rows)
	for i := range out {
		out[i] = "row-" + itoa(i)
	}
	return out
}

// TestLiveSeatStatesRenderApart is §4a.1 applied to this pane.
//
// A refused seat, a starting seat and an ended seat all have nothing useful to
// draw, and rendering the three alike is exactly the zero-vs-absent collapse
// this repo exists to prevent.
func TestLiveSeatStatesRenderApart(t *testing.T) {
	base := room()
	frames := map[string]string{}
	for name, l := range map[string]LiveSeat{
		"unavailable": {Seat: model.VendorClaude, Phase: LiveUnavailable,
			Note: "a live seat needs Windows build 17763 or later for a pseudoconsole"},
		"opening": {Seat: model.VendorClaude, Phase: LiveOpening},
		"ended":   {Seat: model.VendorClaude, Phase: LiveEnded, Grid: []string{"the last thing it drew"}},
	} {
		st := base
		st.Live = l
		frames[name] = render(st)
	}
	if frames["unavailable"] == frames["opening"] {
		t.Error("a refused live seat renders the same as one that is starting")
	}
	if frames["opening"] == frames["ended"] {
		t.Error("a starting live seat renders the same as one that has ended")
	}
	if !strings.Contains(frames["unavailable"], "17763") {
		t.Error("the refusal does not name what it needed, which is the whole of an honest refusal")
	}
	if !strings.Contains(frames["ended"], "the last thing it drew") {
		t.Error("an ended pane blanked the last screen the operator was reading")
	}

	// A child that ended badly says WHY, above its last screen. The marker row
	// has room for a word and this is a sentence, so the two carry different
	// halves rather than one repeating the other.
	bad := base
	bad.Live = LiveSeat{Seat: model.VendorClaude, Phase: LiveEnded,
		Note: "the live seat exited with code 137", Grid: []string{"the last thing it drew"}}
	badFrame := render(bad)
	if !strings.Contains(badFrame, "code 137") {
		t.Error("a live seat that exited badly did not say so anywhere on the pane")
	}
	if badFrame == frames["ended"] {
		t.Error("a bad exit renders the same as a clean one")
	}
}

// TestLiveSeatSurvivesASCIIAndPlainStyles is the CLAUDE.md rule applied here:
// every distinction is carried first by a word or a glyph, and colour only
// makes it easier to find.
func TestLiveSeatSurvivesASCIIAndPlainStyles(t *testing.T) {
	st := liveRoom(t)
	st.ASCII = true
	frame := Render(st, PlainStyles(), GlyphsFor(true))
	if !strings.Contains(frame, "live") {
		t.Error("the ascii frame does not say the pane is live")
	}
	if !strings.Contains(frame, "display only") {
		t.Error("the ascii frame does not say the pane measures nothing")
	}
}

// TestLiveSeatIsPureOverState is TestRenderIsPure's assertion, made again over
// a State that carries a live pane.
//
// It is worth its own test because the tempting implementation is the one that
// fails it: a Render that asked the emulator for the current screen would draw
// correctly once and differ on the second call.
func TestLiveSeatIsPureOverState(t *testing.T) {
	st := liveRoom(t)
	if a, b := render(st), render(st); a != b {
		t.Error("rendering a live pane twice gave two different frames")
	}
}

// TestTheSpawnGuardCountsTheLiveSeat asserts the wiring the whole guard rests
// on: countSpawns must see a pseudoconsole spawn.
//
// Without this, "nothing was spawned" would pass over a vendor that had been
// launched with a pseudoconsole attached — which is precisely the assertion the
// security tests exist to make.
func TestTheSpawnGuardCountsTheLiveSeat(t *testing.T) {
	log := countSpawns(t)
	spec := runner.Spec{Vendor: model.VendorClaude, Binary: "telltale-no-such-binary"}
	if _, err := startPTYSession(context.Background(), spec, 80, 24, nil); err != nil {
		t.Fatalf("the stubbed spawn failed: %v", err)
	}
	if log.n() != 1 {
		t.Fatalf("countSpawns saw %d spawns, want 1 — startPTYSession is not stubbed", log.n())
	}
	if log.specs[0].Binary != spec.Binary {
		t.Errorf("the counted spec was %q, want %q", log.specs[0].Binary, spec.Binary)
	}
}

// TestParseLiveTakesOnlyAPersistentSeat pins which seats can be live, and pins
// it against the REGISTRY rather than against a list typed here.
func TestParseLiveTakesOnlyAPersistentSeat(t *testing.T) {
	if v, err := ParseLive(""); err != nil || v != "" {
		t.Errorf("an empty --live is the ordinary room, got (%q, %v)", v, err)
	}
	if v, err := ParseLive("claude"); err != nil || v != model.VendorClaude {
		t.Errorf(`ParseLive("claude") = (%q, %v), want the claude seat`, v, err)
	}
	for _, seat := range []string{"codex", "agy", "cursor", "grok"} {
		if _, err := ParseLive(seat); err == nil {
			t.Errorf("%s took a live seat, and it does not keep a process across turns", seat)
		}
	}
	if _, err := ParseLive("nonesuch"); err == nil {
		t.Error("an unknown seat was accepted")
	}
}

// TestLivePaneIsNotDrawnWhereThereIsNoPane covers the geometry answer that
// means "do not resize".
//
// A pane the operator cannot see must not make the guest repaint its whole
// screen — measured at 1624 bytes for one resize — so every body that replaces
// the column area has to answer false.
func TestLivePaneIsNotDrawnWhereThereIsNoPane(t *testing.T) {
	base := room()
	base.Live = LiveSeat{Seat: model.VendorClaude, Phase: LiveShowing}
	if _, _, ok := livePaneRect(base); !ok {
		t.Fatal("the plain grid has no pane, so the rest of this test is vacuous")
	}
	cases := map[string]func(*State){
		"help panel open":  func(s *State) { s.Help = HelpKeys },
		"turn page open":   func(s *State) { s.Page.Open = true },
		"below the floor":  func(s *State) { s.Width = MinWidth - 1 },
		"no live seat":     func(s *State) { s.Live = LiveSeat{} },
		"seat not in room": func(s *State) { s.Live.Seat = model.VendorGrok },
	}
	for name, mut := range cases {
		st := base
		mut(&st)
		if _, _, ok := livePaneRect(st); ok {
			t.Errorf("%s: livePaneRect answered yes, so a resize would be issued for a pane nobody can see", name)
		}
	}
}

// TestLiveGoldens pins the pane's chrome and a fixed decoded grid.
//
// It pins what a golden CAN pin. The bytes are a fixed script rather than a
// live child, and the grid is plain text rather than the guest's colours, so
// nothing here embeds vendor ANSI — which CLAUDE.md forbids and PlainStyles
// would not strip, because PlainStyles neutralises telltale's own escapes and
// is blind to another program's.
func TestLiveGoldens(t *testing.T) {
	st := liveRoom(t)
	golden(t, "live-seat", render(st))
	st.ASCII = true
	golden(t, "live-seat-ascii", Render(st, PlainStyles(), GlyphsFor(true)))
}
