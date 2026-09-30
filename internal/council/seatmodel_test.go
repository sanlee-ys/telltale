package council

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/adapter/antigravity"
	"github.com/sanlee-ys/telltale/internal/adapter/claudecode"
	"github.com/sanlee-ys/telltale/internal/adapter/codex"
	"github.com/sanlee-ys/telltale/internal/adapter/cursor"
	"github.com/sanlee-ys/telltale/internal/adapter/grok"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// ---------------------------------------------------------------- the request

func TestParseModelsReadsOneRequestPerSeat(t *testing.T) {
	got, err := ParseModels(" Claude=opus , antigravity=gemini-3-pro,codex=gpt-5.6-sol,grok=grok-4.5")
	if err != nil {
		t.Fatal(err)
	}
	want := map[model.VendorID]string{
		model.VendorClaude:      "opus",
		model.VendorAntigravity: "gemini-3-pro",
		model.VendorCodex:       "gpt-5.6-sol",
		model.VendorGrok:        "grok-4.5",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseModels = %v, want %v", got, want)
	}
	// Empty is the vendor default for every seat, and it is nil rather than
	// an empty map, so "asked for nothing" has one spelling.
	if got, err := ParseModels("  "); err != nil || got != nil {
		t.Fatalf("empty --model = %v, %v; want nil, nil", got, err)
	}
}

func TestParseModelsRefusesWhatItCannotPass(t *testing.T) {
	cases := map[string]string{
		"no equals sign":     "codex",
		"no seat":            "=gpt-5.6-sol",
		"no model":           "codex=",
		"every seat at once": "all=opus",
		"unknown seat":       "gemini=gemini-3-pro",
		"named twice":        "codex=a,codex=b",
		"a flag as a name":   "claude=--dangerously-skip-permissions",
		"a shell character":  "codex=a&b",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if got, err := ParseModels(in); err == nil {
				t.Fatalf("ParseModels(%q) = %v, want an error", in, got)
			}
		})
	}
}

// TestParseModelsRefusesTheCursorSeatByName is the "refused, not dropped" rule.
// cursor-agent was not installed where the flags were measured, so there is no
// help line to quote and no position to put a model on. The request must be an
// error the caller can name, before the room opens.
func TestParseModelsRefusesTheCursorSeatByName(t *testing.T) {
	_, err := ParseModels("cursor=composer-2")
	if !errors.Is(err, vendors.ErrModelNotSupported) {
		t.Fatalf("err = %v, want vendors.ErrModelNotSupported", err)
	}
	if !strings.Contains(err.Error(), "refused rather than dropped") {
		t.Errorf("the refusal does not say the request was refused: %v", err)
	}
}

func TestAHostedRoomRefusesAModelRequest(t *testing.T) {
	err := refuseHostedFlags(Options{Models: map[model.VendorID]string{model.VendorCodex: "gpt-5.6-sol"}})
	if !errors.Is(err, ErrHostedFlag) || !errors.Is(err, errModelHosted) {
		t.Fatalf("err = %v, want ErrHostedFlag wrapping errModelHosted", err)
	}
	if err := refuseHostedFlags(Options{}); err != nil {
		t.Fatalf("a hosted room with no model request was refused: %v", err)
	}
}

// TestEverySpawnPathCarriesTheRequest drives the three spawn shapes a seat can
// take, through the stubbed spawn vars, and reads the argv each one launched.
// A path that skipped withSeatModel would run the vendor default under a column
// that says `asked`.
func TestEverySpawnPathCarriesTheRequest(t *testing.T) {
	log := countSpawns(t)
	m := newModel(Options{Models: map[model.VendorID]string{
		model.VendorClaude: "opus",
		model.VendorCodex:  "gpt-5.6-sol",
		model.VendorGrok:   "grok-4.5",
	}}, room())
	col := func(v model.VendorID) *Column { return &Column{Vendor: v, Binary: "telltale-no-such-binary"} }

	// The batch turn, first and resumed.
	spec, _, err := m.specFor(vendors.Codex{}, col(model.VendorCodex), "brief")
	if err != nil {
		t.Fatal(err)
	}
	assertArgs(t, "codex exec", spec.Args, "-c", `model="gpt-5.6-sol"`)
	m.sessions[model.VendorCodex] = "thread-1"
	spec, resumed, err := m.specFor(vendors.Codex{}, col(model.VendorCodex), "brief")
	if err != nil || resumed != "thread-1" {
		t.Fatalf("resume: %v, resumed %q", err, resumed)
	}
	assertArgs(t, "codex exec resume", spec.Args, "-c", `model="gpt-5.6-sol"`)

	// The live stream-json seat, and the live conversational seat.
	if _, _, _, err := m.spawnSeat(vendors.Claude{}, col(model.VendorClaude), "", vendors.PostureRead); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := m.spawnSeat(vendors.GrokAgent{}, col(model.VendorGrok), "", vendors.PostureRead); err != nil {
		t.Fatal(err)
	}
	if len(log.specs) != 2 {
		t.Fatalf("spawned %d, want 2", len(log.specs))
	}
	assertArgs(t, "claude session", log.specs[0].Args, "--model", "opus")
	assertArgs(t, "grok agent", log.specs[1].Args, "agent", "--model", "grok-4.5", "stdio")

	// A seat the room asked nothing of keeps the argv it had before --model.
	agy, _, err := m.specFor(vendors.Antigravity{}, col(model.VendorAntigravity), "brief")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agy.Args {
		if a == "--model" {
			t.Fatalf("an unrequested seat carries --model: %q", agy.Args)
		}
	}
}

// assertArgs fails unless want appears in args as one contiguous run.
func assertArgs(t *testing.T, what string, args []string, want ...string) {
	t.Helper()
	for i := 0; i+len(want) <= len(args); i++ {
		if reflect.DeepEqual(args[i:i+len(want)], want) {
			return
		}
	}
	t.Errorf("%s: argv %q does not carry %q", what, args, want)
}

// ---------------------------------------------------------------- the read

// fixtureSources points every seat's reader at that adapter's own fixture
// tree, so this package reads the same synthesized records the adapters' own
// tests pin. Restored on cleanup.
func fixtureSources(t *testing.T) {
	t.Helper()
	orig := seatModelSources
	t.Cleanup(func() { seatModelSources = orig })
	adapter := filepath.Join("..", "adapter")
	seatModelSources = map[model.VendorID]func() model.Adapter{
		model.VendorClaude: func() model.Adapter {
			return claudecode.NewWithRoot(filepath.Join(adapter, "claudecode", "testdata", "projects"))
		},
		model.VendorCodex: func() model.Adapter {
			return codex.NewWithRoot(filepath.Join(adapter, "codex", "testdata"))
		},
		model.VendorAntigravity: func() model.Adapter {
			return antigravity.NewWithRoot(filepath.Join(adapter, "antigravity", "testdata", "root"))
		},
		model.VendorGrok: func() model.Adapter {
			return grok.NewWithRoot(filepath.Join(adapter, "grok", "testdata", "sessions"))
		},
		model.VendorCursor: func() model.Adapter {
			return cursor.NewWithRoot(filepath.Join(adapter, "cursor", "testdata", "root"))
		},
	}
}

// TestTheReaderNamesWhatEachVendorsRecordSays reads one fixture session per
// vendor. The expected names are the ones each adapter's own test pins for the
// same fixture, so this is the room and the HUD agreeing on one record.
func TestTheReaderNamesWhatEachVendorsRecordSays(t *testing.T) {
	fixtureSources(t)
	cases := []struct {
		vendor  model.VendorID
		session string
		want    string
	}{
		{model.VendorClaude, "00000000-aaaa-4bbb-8ccc-000000000001", "claude-opus-5"},
		{model.VendorCodex, "00000000-bbbb-4ccc-8ddd-000000000002", "gpt-fixture-codex"},
		// The id, not the display name ("Gemini 3.6 Flash (High)"): the request
		// is an id, and comparing it with a display name would mismatch always.
		{model.VendorAntigravity, "00000000-dddd-4eee-8fff-000000000001", "gemini-3.6-flash"},
		{model.VendorGrok, "00000000-1111-7222-8333-000000000001", "grok-4.5"},
		{model.VendorCursor, "00000000-eeee-4fff-8aaa-000000000001", "composer-2.5"},
	}
	for _, tc := range cases {
		t.Run(string(tc.vendor), func(t *testing.T) {
			if got := readSeatModel(context.Background(), tc.vendor, tc.session); got != tc.want {
				t.Fatalf("readSeatModel(%s) = %q, want %q", tc.session, got, tc.want)
			}
		})
	}
}

// TestTheReaderSaysUnknownAndNeverGuesses is every path to `unknown`: a
// session the store does not hold (the missing-file case), a store that does
// not exist, a seat with no session id, and a vendor with no reader. None of
// them may borrow a name from a neighbouring session in the same store.
func TestTheReaderSaysUnknownAndNeverGuesses(t *testing.T) {
	fixtureSources(t)
	ctx := context.Background()
	if got := readSeatModel(ctx, model.VendorClaude, "00000000-aaaa-4bbb-8ccc-00000000ffff"); got != "" {
		t.Errorf("a session with no transcript read as %q, want unknown", got)
	}
	if got := readSeatModel(ctx, model.VendorClaude, ""); got != "" {
		t.Errorf("a seat with no session id read as %q, want unknown", got)
	}
	if got := readSeatModel(ctx, model.VendorPi, "anything"); got != "" {
		t.Errorf("a vendor with no reader read as %q, want unknown", got)
	}
	seatModelSources[model.VendorGrok] = func() model.Adapter {
		return grok.NewWithRoot(filepath.Join(t.TempDir(), "no-such-store"))
	}
	if got := readSeatModel(ctx, model.VendorGrok, "00000000-1111-7222-8333-000000000001"); got != "" {
		t.Errorf("a missing store read as %q, want unknown", got)
	}
}

// TestAReadLandsOnTheColumnsAndClearsAGoneSession is applySeatModels' contract:
// a read names or says unknown, a seat the read did not speak for loses its old
// name, and a request with no read keeps only the request.
func TestAReadLandsOnTheColumnsAndClearsAGoneSession(t *testing.T) {
	fixtureSources(t)
	m := newModel(Options{Models: map[model.VendorID]string{model.VendorCodex: "gpt-5.6-sol"}}, room())
	m.sessions[model.VendorClaude] = "00000000-aaaa-4bbb-8ccc-000000000001"
	m.sessions[model.VendorCodex] = "00000000-bbbb-4ccc-8ddd-00000000ffff"

	msg := m.readSeatModelsCmd()().(seatModelMsg)
	m.applySeatModels(msg)

	claude, codexCol, agy := m.st.Columns[0].Model, m.st.Columns[1].Model, m.st.Columns[2].Model
	if claude == nil || !claude.Read || claude.Resolved != "claude-opus-5" || claude.Requested != "" {
		t.Errorf("claude = %+v, want an unrequested read of claude-opus-5", claude)
	}
	if codexCol == nil || !codexCol.Read || codexCol.Resolved != "" || codexCol.Requested != "gpt-5.6-sol" {
		t.Errorf("codex = %+v, want the request and a read that found no record", codexCol)
	}
	if agy != nil {
		t.Errorf("agy = %+v, want nil: no request and no session", agy)
	}

	// The claude thread is cleared. The next read does not speak for it, and
	// its old name must go rather than describe a conversation that is gone.
	delete(m.sessions, model.VendorClaude)
	m.applySeatModels(m.readSeatModelsCmd()().(seatModelMsg))
	if m.st.Columns[0].Model != nil {
		t.Errorf("a cleared session kept its model: %+v", m.st.Columns[0].Model)
	}
	delete(m.sessions, model.VendorCodex)
	m.applySeatModels(m.readSeatModelsCmd()().(seatModelMsg))
	if got := m.st.Columns[1].Model; got == nil || got.Read || got.Requested != "gpt-5.6-sol" {
		t.Errorf("codex after its session went = %+v, want the request alone", got)
	}
}

// ---------------------------------------------------------------- the render

// modelRoom is the demo roster, four model families, at the ruled demo
// geometry (LEDGER.md 2026-09-04: the ordinary desk window of about 181 by
// 71 cells). Each seat carries one of the four states the cell draws.
func modelRoom() State {
	st := room()
	st.Width, st.Height = 181, 71
	st.Columns = append(st.Columns, Column{
		Vendor: model.VendorGrok, Label: "grok",
		Avail:   AvailInstalled,
		Sandbox: SandboxClaim{Level: SandboxNone, Detail: "unsandboxed"},
		Gran:    GranFinalOnly, Phase: PhaseIdle,
	})
	return st
}

func withModels(st State) State {
	st.Columns[0].Model = &SeatModel{Requested: "claude-opus-5", Read: true, Resolved: "claude-opus-5"}
	st.Columns[1].Model = &SeatModel{Requested: "gpt-5.6-sol", Read: true, Resolved: "gpt-5.6-terra"}
	st.Columns[2].Model = &SeatModel{Read: true}
	st.Columns[3].Model = &SeatModel{Requested: "grok-4.5"}
	return st
}

// TestTheSeatModelGolden pins the four states at the demo geometry: a match, a
// mismatch, an unrequested unknown, and a request with no read yet.
func TestTheSeatModelGolden(t *testing.T) {
	got := render(withModels(modelRoom()))
	golden(t, "seat-model", got)
	// The match sheds `as asked` at this width and keeps its reading; the
	// mismatch sheds `asked <name>` and keeps its mark.
	for _, want := range []string{"ran claude-opus-5", "⚠ ran gpt-5.6-terra", "ran unknown", "asked grok-4.5"} {
		if !strings.Contains(got, want) {
			t.Errorf("the frame does not carry %q:\n%s", want, got)
		}
	}
}

// TestTheSeatModelChangesOneRowAndNothingElse is the golden discipline made a
// test: the frame with models differs from the frame without them on the
// badge row alone.
func TestTheSeatModelChangesOneRowAndNothingElse(t *testing.T) {
	without := strings.Split(render(modelRoom()), "\n")
	with := strings.Split(render(withModels(modelRoom())), "\n")
	if len(with) != len(without) {
		t.Fatalf("the frame changed height: %d lines, was %d", len(with), len(without))
	}
	var changed []int
	for i := range with {
		if with[i] != without[i] {
			changed = append(changed, i)
		}
	}
	if len(changed) != 1 {
		t.Fatalf("lines %v changed, want exactly one (the badge row)", changed)
	}
	if !strings.Contains(with[changed[0]], "ran") {
		t.Errorf("the changed line is not the badge row: %q", with[changed[0]])
	}
}

// TestNoModelFactIsZeroCells is the zero-vs-absent rule for this cell. Nil (no
// request, no read) draws nothing and takes nothing. A read that found no name
// draws the word `unknown`, which is a different fact and a different row. A
// request with no read draws `asked` and no `ran`, because nothing ran yet
// that the room has read.
func TestNoModelFactIsZeroCells(t *testing.T) {
	st := modelRoom()
	bare := badgeRow(st, st.Columns[2], 60, PlainStyles(), UnicodeGlyphs())

	st.Columns[2].Model = nil
	if got := badgeRow(st, st.Columns[2], 60, PlainStyles(), UnicodeGlyphs()); got != bare {
		t.Errorf("a nil model changed the row\n got %q\nwant %q", got, bare)
	}
	st.Columns[2].Model = &SeatModel{Read: true}
	unknown := badgeRow(st, st.Columns[2], 60, PlainStyles(), UnicodeGlyphs())
	if unknown == bare || !strings.Contains(unknown, "ran unknown") {
		t.Errorf("a read that found nothing did not say unknown: %q", unknown)
	}
	st.Columns[2].Model = &SeatModel{Requested: "gemini-3-pro"}
	asked := badgeRow(st, st.Columns[2], 60, PlainStyles(), UnicodeGlyphs())
	if !strings.Contains(asked, "asked gemini-3-pro") || strings.Contains(asked, "ran") {
		t.Errorf("a request with no read = %q, want `asked` and no `ran`", asked)
	}
}

// TestAMismatchKeepsItsMarkWhenItSheds pins the ladder: at a width where the
// dressed form does not fit, the bare form keeps the mark, and --ascii spells
// the mark as `!`.
func TestAMismatchKeepsItsMarkWhenItSheds(t *testing.T) {
	m := &SeatModel{Requested: "gpt-5.6-sol", Read: true, Resolved: "gpt-5.6-terra"}
	if !m.Mismatch() {
		t.Fatal("different names are not a mismatch")
	}
	_, wide := seatModelCell(m, 80, PlainStyles(), UnicodeGlyphs())
	if wide != "⚠ asked gpt-5.6-sol  ran gpt-5.6-terra" {
		t.Errorf("wide = %q", wide)
	}
	_, narrow := seatModelCell(m, 22, PlainStyles(), UnicodeGlyphs())
	if narrow != "⚠ ran gpt-5.6-terra" {
		t.Errorf("narrow = %q, want the bare form with its mark", narrow)
	}
	_, ascii := seatModelCell(m, 22, PlainStyles(), GlyphsFor(true))
	if !strings.HasPrefix(ascii, "! ") {
		t.Errorf("ascii = %q, want the ascii mark", ascii)
	}
	if _, none := seatModelCell(m, 5, PlainStyles(), UnicodeGlyphs()); none != "" {
		t.Errorf("a cell too narrow for any rung drew %q, want nothing rather than a clip", none)
	}
	match := &SeatModel{Requested: "claude-opus-5", Read: true, Resolved: "claude-opus-5"}
	if _, p := seatModelCell(match, 80, PlainStyles(), UnicodeGlyphs()); p != "ran claude-opus-5 as asked" {
		t.Errorf("wide match = %q", p)
	}
	if _, p := seatModelCell(match, 20, PlainStyles(), UnicodeGlyphs()); p != "ran claude-opus-5" {
		t.Errorf("narrow match = %q", p)
	}
	// Case is not a mismatch; an expanded alias is, and both names show.
	if (&SeatModel{Requested: "GPT-5.6-SOL", Read: true, Resolved: "gpt-5.6-sol"}).Mismatch() {
		t.Error("a case difference is a mismatch")
	}
	if !(&SeatModel{Requested: "opus", Read: true, Resolved: "claude-opus-5"}).Mismatch() {
		t.Error("an alias the room cannot resolve is not a mismatch; the room would be guessing")
	}
}

// TestTheModelNeverEvictsThePosture pins the ruling on the row: the model cell
// takes the space left, and the granularity word is the only older word that
// yields to it.
func TestTheModelNeverEvictsThePosture(t *testing.T) {
	st := modelRoom()
	c := st.Columns[1]
	c.Model = &SeatModel{Requested: "gpt-5.6-sol", Read: true, Resolved: "gpt-5.6-terra"}
	for w := 15; w <= 60; w++ {
		row := badgeRow(st, c, w, PlainStyles(), UnicodeGlyphs())
		plainRow := badgeRow(st, Column{Vendor: c.Vendor, Avail: c.Avail, Sandbox: c.Sandbox, Gran: c.Gran}, w, PlainStyles(), UnicodeGlyphs())
		if strings.Contains(plainRow, c.Sandbox.Badge()) && !strings.Contains(row, c.Sandbox.Badge()) {
			t.Errorf("width %d: the model evicted the posture badge: %q", w, row)
		}
	}
	// At a width where the model fits only without the granularity word, the
	// word goes and the model stays.
	row := badgeRow(st, c, 36, PlainStyles(), UnicodeGlyphs())
	if !strings.Contains(row, "⚠ ran gpt-5.6-terra") || strings.Contains(row, c.Gran.String()) {
		t.Errorf("width 36 = %q, want the model and no granularity word", row)
	}
}
