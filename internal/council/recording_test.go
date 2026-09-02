package council

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// fixtureRecording is the synthesized recording every replay test plays: three
// seats, one @all dispatch, one gate card raised and allowed, one seat that
// finishes. Fake ids, fake paths, realistic shape only (CLAUDE.md's fixture
// rule).
const fixtureRecording = "testdata/replay/crew.jsonl"

// TestTheRecorderWritesWhatApplyEventsSawAndNothingElse is the recorder's
// whole contract: the room line, the dispatch as its seats held it, every
// event in the batch applyEvents got, the operator's decision on a card —
// and no keystroke, no notice, no scroll, no redaction.
func TestTheRecorderWritesWhatApplyEventsSawAndNothingElse(t *testing.T) {
	countSpawns(t)
	path := filepath.Join(t.TempDir(), "run.jsonl")
	rec, err := openRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	m := crewRoom(t)
	m.rec = rec
	rec.room(m.st)

	send(t, m, "@codex go")
	// Keys that move the reader and not the room: none of these is a record.
	m.key(key("tab"))
	m.key(key("i"))
	m.setDraft("half a draft")
	m.key(key("esc"))

	raw := "the token is sk-ant-api03-FAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKE0000000000000000000000000000000000000000000000000000000000-FAKE "
	m.Update(eventBatchMsg{events: []runner.Event{
		{Vendor: model.VendorCodex, Kind: runner.KindText, Text: raw},
		{Vendor: model.VendorCodex, Kind: runner.KindDone},
	}})

	// A card on the persistent seat, answered with y.
	send(t, m, "@claude write it")
	m.Update(eventBatchMsg{events: []runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindGate,
		Gate: &runner.Gate{RequestID: "req-1", ToolUseID: "toolu-1", Tool: "Write", Text: "Write: notes.txt",
			Input: map[string]any{"content": "the whole file"}},
	}}})
	if !m.st.Gating() {
		t.Fatal("the fixture did not raise a card")
	}
	m.key(key("y"))
	if err := rec.close(); err != nil {
		t.Fatal(err)
	}

	got, err := readRecording(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.room.Version != recordingVersion || len(got.room.Seats) != 4 || !got.room.Write {
		t.Errorf("room line = %+v", got.room)
	}
	var kinds []string
	for _, l := range got.lines {
		kinds = append(kinds, l.Kind+":"+l.Event)
	}
	want := []string{"dispatch:", "event:text", "event:done", "dispatch:", "event:gate", "gate:"}
	if strings.Join(kinds, " ") != strings.Join(want, " ") {
		t.Fatalf("records = %v, want %v", kinds, want)
	}
	d := got.lines[0]
	if d.Turn != 1 || len(d.Sent) != 1 || d.Sent[0].Vendor != "codex" || d.Sent[0].Prompt != "go" {
		t.Errorf("dispatch = %+v", d)
	}
	if d.Route == nil || len(d.Route.Vendors) != 1 || d.Route.Vendors[0] != "codex" {
		t.Errorf("route = %+v", d.Route)
	}
	// Verbatim: what applyEvents saw, not what it drew. The replay runs the
	// same redactor over it, so the frame comes out the same; the file does
	// not (recording.go's ruling, and replay-check's warning).
	if got.lines[1].Text != raw {
		t.Errorf("text was not recorded verbatim: %q", got.lines[1].Text)
	}
	if got.lines[3].Sent[0].Persistent != true {
		t.Error("the persistent seat was not marked persistent")
	}
	g := got.lines[4]
	if g.Gate == nil || g.Gate.RequestID != "req-1" || g.Gate.Text != "Write: notes.txt" {
		t.Errorf("gate event = %+v", g.Gate)
	}
	if got.lines[5].RequestID != "req-1" || !got.lines[5].Allow || got.lines[5].Vendor != "claude" {
		t.Errorf("gate decision = %+v", got.lines[5])
	}
	for i := 1; i < len(got.lines); i++ {
		if got.lines[i].MS < got.lines[i-1].MS {
			t.Errorf("record %d runs backwards", i)
		}
	}
	// Input never reaches the file: it is a Write's whole content and no
	// replay can hand it back.
	file, _ := os.ReadFile(path)
	if bytes.Contains(file, []byte("the whole file")) {
		t.Error("the gate's Input blob reached the recording")
	}
	if bytes.Contains(file, []byte("half a draft")) {
		t.Error("an undispatched draft reached the recording")
	}
}

// TestARecordingRefusesTelltalesOwnDirectory. A recording carries content;
// ~/.telltale holds numbers and keys. The refusal is by resolved path, and a
// sibling that merely shares the prefix is not the directory.
func TestARecordingRefusesTelltalesOwnDirectory(t *testing.T) {
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		filepath.Join(home, ".telltale", "run.jsonl"),
		filepath.Join(home, ".telltale", "council", "run.jsonl"),
		filepath.Join(home, ".telltale"),
	} {
		if _, err := openRecorder(p); err == nil || !strings.Contains(err.Error(), "~/.telltale") {
			t.Errorf("openRecorder(%s) = %v, want a refusal naming ~/.telltale", p, err)
		}
	}
	sibling := filepath.Join(home, ".telltale-notes", "run.jsonl")
	if err := os.MkdirAll(filepath.Dir(sibling), 0o700); err != nil {
		t.Fatal(err)
	}
	rec, err := openRecorder(sibling)
	if err != nil {
		t.Fatalf("a sibling directory was refused: %v", err)
	}
	rec.close()
}

// TestARecordingRefusesToOverwrite: one run, one file. An existing capture
// is neither truncated nor extended.
func TestARecordingRefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.jsonl")
	rec, err := openRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	rec.close()
	if _, err := openRecorder(path); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("a second open = %v, want a refusal", err)
	}
	// A nil recorder is every room that did not ask, and every hook on it is
	// a no-op.
	var none *recorder
	none.events([]runner.Event{{Vendor: model.VendorCodex, Kind: runner.KindText}})
	none.gate(PendingGate{}, true)
	none.room(room())
	if err := none.close(); err != nil {
		t.Error(err)
	}
}

// TestAnEventRoundTripsThroughTheFile: every kind, every field the room
// reads, in and back out — except Input, which is dropped on purpose.
func TestAnEventRoundTripsThroughTheFile(t *testing.T) {
	cost := 0.5
	for _, ev := range []runner.Event{
		{Vendor: model.VendorCodex, Kind: runner.KindText, Text: "hello\n"},
		{Vendor: model.VendorClaude, Kind: runner.KindActivity, Acts: []runner.ActCall{
			{ID: "a", Text: "Bash: go test", Outcome: runner.ActOK},
			{ID: "b", Text: "Read: x", Outcome: runner.ActFailed, Detail: "no such file"},
		}},
		{Vendor: model.VendorClaude, Kind: runner.KindSession, SessionID: "sess-1"},
		{Vendor: model.VendorClaude, Kind: runner.KindMeta, SessionID: "sess-1", CostUSD: &cost, Text: "final", EndsTurn: true},
		{Vendor: model.VendorClaude, Kind: runner.KindGate, Gate: &runner.Gate{
			RequestID: "r", ToolUseID: "t", Tool: "Edit", Text: "Edit: f", OldContent: "a", NewContent: "b",
			Input: map[string]any{"file_path": "f"}}},
		{Vendor: model.VendorCodex, Kind: runner.KindDone, ExitCode: 3},
		{Vendor: model.VendorAntigravity, Kind: runner.KindError, Err: errors.New("boom"), Note: "it broke",
			EndsTurn: true, Failure: runner.FailureVendorUnavailable},
	} {
		line := eventRecord(ev)
		back, ok := line.event()
		if !ok {
			t.Fatalf("%v did not come back", ev.Kind)
		}
		if back.Vendor != ev.Vendor || back.Kind != ev.Kind || back.Text != ev.Text ||
			back.SessionID != ev.SessionID || back.EndsTurn != ev.EndsTurn ||
			back.ExitCode != ev.ExitCode || back.Note != ev.Note || back.Failure != ev.Failure {
			t.Errorf("round trip changed %+v into %+v", ev, back)
		}
		if (ev.CostUSD == nil) != (back.CostUSD == nil) || (ev.CostUSD != nil && *ev.CostUSD != *back.CostUSD) {
			t.Errorf("cost changed: %v -> %v", ev.CostUSD, back.CostUSD)
		}
		if (ev.Err == nil) != (back.Err == nil) || (ev.Err != nil && ev.Err.Error() != back.Err.Error()) {
			t.Errorf("err changed: %v -> %v", ev.Err, back.Err)
		}
		if len(back.Acts) != len(ev.Acts) {
			t.Errorf("acts changed: %v -> %v", ev.Acts, back.Acts)
		}
		for i := range ev.Acts {
			if back.Acts[i] != ev.Acts[i] {
				t.Errorf("act %d changed: %+v -> %+v", i, ev.Acts[i], back.Acts[i])
			}
		}
		if ev.Gate != nil {
			if back.Gate == nil || back.Gate.RequestID != ev.Gate.RequestID || back.Gate.Text != ev.Gate.Text ||
				back.Gate.OldContent != ev.Gate.OldContent || back.Gate.NewContent != ev.Gate.NewContent {
				t.Errorf("gate changed: %+v -> %+v", ev.Gate, back.Gate)
			}
			if back.Gate.Input != nil {
				t.Error("Input survived the round trip")
			}
		}
	}
	// Every runner kind has a word, so a kind added to the runner fails here
	// before a recording silently drops it.
	for k := runner.KindText; k <= runner.KindError; k++ {
		if eventWords[k] == "" {
			t.Errorf("EventKind %d has no word in the file format", k)
		}
	}
}

// TestReplayCheckListsTheIdentities is the frame review, given a tool: the
// workspace, the seats, every session id and every tool line the file
// carries are printed; the prose is not, and the output says so.
func TestReplayCheckListsTheIdentities(t *testing.T) {
	var out bytes.Buffer
	if err := ReplayCheck(fixtureRecording, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"workspace: ~/code/example",
		"posture: writes, asking",
		"seats: claude (Claude Code), codex (Codex), agy (Antigravity)",
		"dispatches: 1",
		"claude  11111111-2222-4333-8444-555555555555",
		"codex  01999999-aaaa-4bbb-8ccc-dddddddddddd",
		"claude  Read: cmd/example/main.go",
		"claude  gate  Write: cmd/example/version.go  → allowed",
		"vendor output: 3 text events",
		"verbatim and unredacted",
		"does not read the prose",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("replay-check does not say %q:\n%s", want, got)
		}
	}
	// The prose stays in the file. A check that quoted it would be the
	// review reading the conversation for the owner.
	for _, prose := range []string{"Reading main.go first", "Added --version", "add a --version flag"} {
		if strings.Contains(got, prose) {
			t.Errorf("replay-check printed prose: %q", prose)
		}
	}
	if err := ReplayCheck(filepath.Join(t.TempDir(), "missing.jsonl"), &out); err == nil {
		t.Error("a missing file was checked")
	}
}

// TestAMalformedRecordingIsRefusedAtTheLine. Every refusal names the line,
// so a hand-edited fixture is fixed where it broke.
func TestAMalformedRecordingIsRefusedAtTheLine(t *testing.T) {
	room := `{"kind":"room","v":1,"started":"2026-09-01T10:00:00Z","workspace":"~/x","seats":[{"vendor":"codex","label":"Codex"}]}`
	for _, tc := range []struct {
		name, body, want string
	}{
		{"empty", "", "empty"},
		{"no room line", `{"kind":"event","vendor":"codex","event":"text"}`, "opens with the room line"},
		{"wrong version", `{"kind":"room","v":2,"seats":[{"vendor":"codex"}]}`, "version 2"},
		{"no seats", `{"kind":"room","v":1}`, "names no seats"},
		{"runs backwards", room + "\n" + `{"kind":"event","ms":5,"vendor":"codex","event":"text"}` + "\n" +
			`{"kind":"event","ms":4,"vendor":"codex","event":"text"}`, "line 3 runs backwards"},
		{"unknown event", room + "\n" + `{"kind":"event","vendor":"codex","event":"sparkle"}`, `unknown event "sparkle"`},
		{"unseated vendor", room + "\n" + `{"kind":"event","vendor":"grok","event":"text"}`, "does not seat"},
		{"unknown kind", room + "\n" + `{"kind":"keystroke"}`, "unknown record kind"},
		{"not json", room + "\n" + `{`, "not a record"},
		{"dispatch without seats", room + "\n" + `{"kind":"dispatch","turn":1}`, "at least one seat"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseRecording(strings.NewReader(tc.body), "f.jsonl")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
	rec, err := parseRecording(strings.NewReader(room+"\n\n"+`{"kind":"gate","ms":9,"vendor":"codex","request_id":"r","allow":true}`), "f.jsonl")
	if err != nil || len(rec.lines) != 1 {
		t.Errorf("a blank line broke the read: %v %+v", err, rec)
	}
}
