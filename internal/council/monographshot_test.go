package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The MONOGRAPH prototype's capture harness (explore/room-monograph).
//
// It is a TEST rather than a command for the reason the identity brief gives:
// the fixtures that build a populated room already live in this package's tests,
// and the cheapest honest way to photograph the room is to render those fixtures
// through the real Render with the real Styles. Nothing here spawns a vendor,
// asks a model anything, or reads a real session — every State below is
// synthesized, which is this repository's own fixture rule.
//
// It is SKIPPED unless TELLTALE_SHOTS names a directory, so a plain
// `go test ./internal/council` neither writes files nor slows down.
//
//	TELLTALE_SHOTS=<dir> go test ./internal/council -run TestMonographCaptures
//
// What it writes is ANSI, one file per panel. Turning ANSI into a picture is a
// separate step on purpose: internal/svgframe already converts a frame to SVG
// for the README, and the contact sheet wants a font and a layout that are
// properties of the SHEET rather than of the product.
func TestMonographCaptures(t *testing.T) {
	dir := os.Getenv("TELLTALE_SHOTS")
	if dir == "" {
		t.Skip("set TELLTALE_SHOTS=<dir> to write the contact sheet's ANSI captures")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name+".ansi"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// dark and paper are the two ink sets; ascii is the accessibility floor —
	// PlainStyles is exactly what NO_COLOR resolves to, and GlyphsFor(true) is
	// --ascii, so one capture proves both.
	dark, paper, plain := NewStyles(true), NewStyles(false), PlainStyles()
	uni, ascii := GlyphsFor(false), GlyphsFor(true)

	for _, size := range []struct{ w, h int }{{120, 36}, {180, 50}} {
		tag := itoa(size.w) + "x" + itoa(size.h)

		cols := shotColumns(size.w, size.h)
		write("columns-"+tag+"-dark", Render(cols, dark, uni))

		race := shotArena(size.w, size.h)
		write("arena-"+tag+"-dark", Render(race, dark, uni))

		led := shotLedger(size.w, size.h)
		write("ledger-"+tag+"-dark", Render(led, dark, uni))
	}

	// The posture page: what each seat is ALLOWED to do, in the room's own
	// measured words. It is one reading area at the full frame, so it is
	// captured once, at the width where its paragraphs are not a stack of
	// two-word lines.
	post := postureRoom()
	post.Width, post.Height = 180, 50
	write("postures-180x50-dark", Render(post, dark, uni))

	// The paper set, at the width the columns tier is drawn for.
	write("columns-120x36-paper", Render(shotColumns(120, 36), paper, uni))

	// The floor. --ascii and NO_COLOR at once: no escape reaches this string and
	// no glyph outside 7-bit ASCII is in it.
	floor := Render(shotColumns(120, 36).withASCII(), plain, ascii)
	if strings.ContainsRune(floor, 0x1b) {
		t.Fatal("the NO_COLOR capture carries an escape sequence")
	}
	for _, r := range floor {
		if r > 0x7e && r != '\n' {
			t.Fatalf("the --ascii capture carries a non-ascii rune %q", r)
		}
	}
	write("columns-120x36-ascii", floor)
}

// withASCII is the switch --ascii throws, so the floor capture is the frame the
// flag actually produces rather than a Unicode frame with a different glyph set
// handed to Render.
func (s State) withASCII() State {
	s.ASCII = true
	return s
}

// shotNow is the fixed instant every capture is stamped against. Render is pure
// over State (TestRenderIsPure), so a literal here is what keeps two runs of the
// harness producing the same picture.
var shotNow = time.Date(2026, 9, 2, 14, 30, 0, 0, time.UTC)

// shotColumns is the answer grid with all five seats doing different things at
// once — the frame the identity has to survive, rather than the quiet one.
//
// Every value is invented, and the shapes are the ones the product really draws:
// a seat that finished and reported a cost, a seat still streaming with a clock
// running, a seat whose turn broke, a seat that has not been asked, and a seat
// carrying a relayed quota reading.
func shotColumns(w, h int) State {
	st := heroRoom()
	st.Width, st.Height = w, h
	st.Now = shotNow
	st.Turn = 7
	st.Briefed = true
	// Below 160 the layout drops five seats to the TABS tier (§9.24), which is
	// a different surface from the grid. A 120-column terminal running three
	// vendors draws the grid, so that is what the narrow capture seats — the
	// picture is of the same surface at two widths rather than of two surfaces.
	if w < 160 {
		st.Columns = st.Columns[:3]
	}

	// Two filed turns behind the live one, so the picture shows a TRANSCRIPT
	// rather than a mostly empty scrollback. A room in use is what the identity
	// has to survive; an idle one flatters any palette.
	brief5 := "read internal/council/resume.go and say what a reattach actually replays"
	brief6 := "is the replay per seat or once for the room?"
	for i := range st.Columns {
		p := &st.Columns[i]
		p.startTurn(5, brief5, false)
		p.Body = "It replays the whole prior transcript as input on every turn, so turn five pays for turns one through four again."
		p.Phase, p.Elapsed = PhaseDone, 12*time.Second
		p.startTurn(6, brief6, false)
		p.Body = "Per seat. Each vendor holds its own thread, so the replay is paid five times."
		p.Phase, p.Elapsed = PhaseDone, 8*time.Second
	}

	c := &st.Columns[0]
	c.startTurn(7, "where does the resume cost go by turn five?", false)
	c.Acts = []Act{
		{ID: "s1", Text: "Glob: internal/council/*.go", Status: runner.ActOK},
		{ID: "s2", Text: "Read: internal/council/resume.go", Status: runner.ActOK},
		{ID: "s3", Text: "Bash: go test ./internal/council", Status: runner.ActOK},
		{ID: "s4", Text: "Write: internal/council/notes.md", Status: runner.ActDenied},
	}
	c.Body = "About 30K redundant input tokens per seat by turn five. Native resume avoids the replay entirely, so the cost is flat rather than quadratic."
	c.Phase, c.Elapsed = PhaseDone, 41*time.Second
	cost := 0.0123
	c.CostUSD = &cost
	c.Quota = seatQuotaAt(st.Now, 90*time.Second, quotaWindowAt(st.Now, "5h", 62, 2*time.Hour))

	x := &st.Columns[1]
	x.startTurn(7, "where does the resume cost go by turn five?", false)
	x.Body = "Roughly thirty thousand input tokens a seat, and it compounds: the"
	x.Phase = PhaseStreaming
	x.Started = st.Now.Add(-18 * time.Second)

	a := &st.Columns[2]
	a.startTurn(7, "where does the resume cost go by turn five?", false)
	a.Body = "The replay is the whole of it."
	a.Phase, a.Elapsed = PhaseDone, 6*time.Second

	// The seat whose turn BROKE. It is the fourth, so the narrow grid does not
	// carry it — and that is the honest picture rather than a gap: at three
	// seats the failure lives on the arena board's own FAIL instead.
	if len(st.Columns) > 3 {
		u := &st.Columns[3]
		u.startTurn(7, "where does the resume cost go by turn five?", false)
		u.Phase, u.Elapsed = PhaseFailed, 3*time.Second
		u.Note = "the session ended before the first token"
	}

	return st
}

// shotArena is a finished race: three attempts, three different verdicts, and
// the one verdict that is not a verdict at all.
//
// It is arenacheck_test.go's own fixture — a PASS, a FAIL carrying its exit
// code, and a check that could not run and therefore reports neither.
func shotArena(w, h int) State {
	st := room()
	st.Width, st.Height = w, h
	st.Now = shotNow
	st.Turn = 6
	base := "abcdef1234567"
	for i := range st.Columns {
		st.Columns[i].Phase = PhaseDone
		st.Columns[i].TurnN = 6
		st.Columns[i].Elapsed = 20 * time.Second
	}
	st.Columns[0].Arena = &ArenaResult{
		Tree: "/home/dev/code/telltale-arena-t6-claude", Branch: "arena/t6/claude", Base: base,
		Stat: " a.txt | 2 +-\n 1 file changed", Rank: 1, Of: 3, Commit: "1111111aaaa",
		Check: &ArenaCheck{Cmd: "go test ./...", Exited: true, Code: 0, Elapsed: 74 * time.Second},
	}
	st.Columns[1].Arena = &ArenaResult{
		Tree: "/home/dev/code/telltale-arena-t6-codex", Branch: "arena/t6/codex", Base: base,
		Stat: " b.go | 9 ++++-----", Rank: 2, Of: 3, Commit: "2222222bbbb",
		Check: &ArenaCheck{Cmd: "go test ./...", Exited: true, Code: 2, Elapsed: 31 * time.Second},
	}
	st.Columns[2].Arena = &ArenaResult{
		Tree: "/home/dev/code/telltale-arena-t6-agy", Branch: "arena/t6/agy", Base: base,
		Rank: 3, Of: 3,
		Check: &ArenaCheck{Cmd: "go test ./...", Err: `exec: "go": executable file not found in $PATH`},
	}
	return st
}

// shotLedger is the act ledger: one turn, and what every seat DID in it — the
// calls, and the outcome the vendor reported for each. It is ledger_test.go's
// own fixture at the capture's size.
func shotLedger(w, h int) State {
	st := ledgered()
	st.Width, st.Height = w, h
	st.Now = shotNow
	return st
}

// seatQuotaAt and quotaWindowAt are quota_test.go's builders with the instant
// supplied, so a capture stamped at shotNow does not have to borrow quotaNow.
func seatQuotaAt(now time.Time, age time.Duration, windows ...model.QuotaWindow) *SeatQuota {
	return &SeatQuota{Windows: windows, WrittenAt: now.Add(-age)}
}

func quotaWindowAt(now time.Time, label string, pct float64, resetIn time.Duration) model.QuotaWindow {
	p := model.Percent(pct)
	wnd := model.QuotaWindow{ID: label, Label: label, UsedPercent: &p}
	if resetIn > 0 {
		t := now.Add(resetIn)
		wnd.ResetsAt = &t
	}
	return wnd
}
