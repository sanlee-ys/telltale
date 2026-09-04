package council

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scrubbed reads the synthesized fixture and scrubs it, which is the pair
// every test below asserts over: one recording in, one recording out.
func scrubbed(t *testing.T) (*recording, []recordLine) {
	t.Helper()
	rec, err := readRecording(fixtureRecording)
	if err != nil {
		t.Fatal(err)
	}
	return rec, scrubRecording(rec)
}

// TestAScrubKeepsTheShapeAndReplacesTheWords is the tool's whole contract:
// every structural fact of the room survives, and no word does.
func TestAScrubKeepsTheShapeAndReplacesTheWords(t *testing.T) {
	rec, out := scrubbed(t)
	if len(out) != len(rec.lines)+1 {
		t.Fatalf("scrub wrote %d lines for %d records plus a room line", len(out), len(rec.lines))
	}
	head := out[0]
	if !head.Scrubbed {
		t.Error("the room line does not say scrubbed")
	}
	if head.Started != scrubbedStart {
		t.Errorf("started = %q, want the synthesized stamp", head.Started)
	}
	if head.Write != rec.room.Write || head.GateOff != rec.room.GateOff || head.Briefed != rec.room.Briefed {
		t.Error("the posture flags moved")
	}
	if len(head.Seats) != len(rec.room.Seats) {
		t.Fatalf("seats = %d, want %d", len(head.Seats), len(rec.room.Seats))
	}
	for i, s := range head.Seats {
		was := rec.room.Seats[i]
		// The label and the sandbox claim are telltale's own words about a
		// vendor and they are the badge row a replay draws, so they stay.
		if s.Vendor != was.Vendor || s.Label != was.Label || s.Avail != was.Avail ||
			s.Sandbox != was.Sandbox || s.Detail != was.Detail || s.Gran != was.Gran {
			t.Errorf("seat %d changed: %+v -> %+v", i, was, s)
		}
	}

	ids := map[string]string{}
	for i, line := range out[1:] {
		was := rec.lines[i]
		if line.Kind != was.Kind || line.MS != was.MS || line.Turn != was.Turn ||
			line.Vendor != was.Vendor || line.Event != was.Event ||
			line.EndsTurn != was.EndsTurn || line.ExitCode != was.ExitCode ||
			line.Failure != was.Failure || line.Allow != was.Allow {
			t.Errorf("record %d lost a structural fact: %+v -> %+v", i, was, line)
		}
		if (line.CostUSD == nil) != (was.CostUSD == nil) ||
			(line.CostUSD != nil && *line.CostUSD != *was.CostUSD) {
			t.Errorf("record %d changed a cost figure", i)
		}
		if (line.Route == nil) != (was.Route == nil) {
			t.Errorf("record %d lost its route", i)
		}
		if line.Route != nil && strings.Join(line.Route.Vendors, ",") != strings.Join(was.Route.Vendors, ",") {
			t.Errorf("record %d re-routed: %v -> %v", i, was.Route.Vendors, line.Route.Vendors)
		}
		for j, s := range line.Sent {
			w := was.Sent[j]
			if s.Vendor != w.Vendor || s.Quoted != w.Quoted || s.Persistent != w.Persistent {
				t.Errorf("record %d seat %d lost a flag: %+v -> %+v", i, j, w, s)
			}
			if s.Prompt == w.Prompt {
				t.Errorf("record %d seat %d kept its brief", i, j)
			}
			if len([]rune(s.Prompt)) != len([]rune(w.Prompt)) {
				t.Errorf("record %d seat %d brief is %d runes, want %d",
					i, j, len([]rune(s.Prompt)), len([]rune(w.Prompt)))
			}
		}
		if was.Text != "" {
			if line.Text == was.Text {
				t.Errorf("record %d kept its text", i)
			}
			if len([]rune(line.Text)) != len([]rune(was.Text)) {
				t.Errorf("record %d text is %d runes, want %d", i, len([]rune(line.Text)), len([]rune(was.Text)))
			}
			if strings.Count(line.Text, "\n") != strings.Count(was.Text, "\n") {
				t.Errorf("record %d moved a newline: %q", i, line.Text)
			}
		}
		if was.SessionID != "" {
			if line.SessionID == was.SessionID {
				t.Errorf("record %d kept its session id", i)
			}
			if len(line.SessionID) != len(was.SessionID) {
				t.Errorf("record %d session id is the wrong shape: %q", i, line.SessionID)
			}
			if got, seen := ids[was.SessionID]; seen && got != line.SessionID {
				t.Errorf("record %d re-keyed one session id two ways: %q and %q", i, got, line.SessionID)
			}
			ids[was.SessionID] = line.SessionID
		}
		for j, a := range line.Acts {
			w := was.Acts[j]
			if a.Outcome != w.Outcome {
				t.Errorf("record %d act %d lost its mark: %d -> %d", i, j, w.Outcome, a.Outcome)
			}
			if len([]rune(a.Text)) != len([]rune(w.Text)) {
				t.Errorf("record %d act %d is %d runes, want %d", i, j, len([]rune(a.Text)), len([]rune(w.Text)))
			}
			if w.ID != "" && a.ID == w.ID {
				t.Errorf("record %d act %d kept its id", i, j)
			}
		}
		if line.Gate != nil {
			if line.Gate.Tool != was.Gate.Tool {
				t.Errorf("record %d lost the gate's tool name", i)
			}
			if line.Gate.Text == was.Gate.Text {
				t.Errorf("record %d kept the gate card's text", i)
			}
		}
	}

	// The card and the answer name one request, and a scrub that re-keyed
	// them apart would leave a card nobody answered.
	var raised, decided string
	for _, line := range out {
		if line.Kind == "event" && line.Gate != nil {
			raised = line.Gate.RequestID
		}
		if line.Kind == "gate" {
			decided = line.RequestID
		}
	}
	if raised == "" || raised != decided {
		t.Errorf("the gate card is %q and the decision is %q", raised, decided)
	}
}

// TestAScrubbedFileIsAValidRecording: what the scrub writes, the reader reads.
// A file the replay refuses would be a fixture nobody can play.
func TestAScrubbedFileIsAValidRecording(t *testing.T) {
	_, out := scrubbed(t)
	var buf bytes.Buffer
	for _, line := range out {
		raw, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(append(raw, '\n'))
	}
	back, err := parseRecording(&buf, "scrubbed.jsonl")
	if err != nil {
		t.Fatalf("the scrub wrote a file the reader refuses: %v", err)
	}
	if !back.room.Scrubbed {
		t.Error("the scrubbed flag did not survive the round trip")
	}
	if len(back.lines) != len(out)-1 {
		t.Errorf("read back %d records, wrote %d", len(back.lines), len(out)-1)
	}
}

// TestAScrubIsDeterministic. The output is committed bytes, so two runs over
// one file are one file and a regenerated fixture is reviewable by diff.
func TestAScrubIsDeterministic(t *testing.T) {
	rec, err := readRecording(fixtureRecording)
	if err != nil {
		t.Fatal(err)
	}
	first, err := json.Marshal(scrubRecording(rec))
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(scrubRecording(rec))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Error("two scrubs of one recording wrote two files")
	}
}

// TestAScrubLeavesNoWordOfTheOriginal. The fixture's own prose, paths and ids
// are searched for in the whole output, which is the check a reviewer runs by
// hand before committing a scrubbed capture.
func TestAScrubLeavesNoWordOfTheOriginal(t *testing.T) {
	_, out := scrubbed(t)
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, gone := range []string{
		"add a --version flag",
		"Reading main.go first",
		"cmd/example/main.go",
		"cmd/example/version.go",
		"version_test.go",
		"11111111-2222-4333-8444-555555555555",
		"01999999-aaaa-4bbb-8ccc-dddddddddddd",
		"req-fixture-01",
		"toolu_fixture01",
		"toolu_fixture02",
	} {
		if strings.Contains(body, gone) {
			t.Errorf("the scrub kept %q", gone)
		}
	}
	// And the structural words that must survive.
	for _, kept := range []string{"claude", "codex", "agy", "Claude Code", "Antigravity", "\"turn\":1"} {
		if !strings.Contains(body, kept) {
			t.Errorf("the scrub dropped %q", kept)
		}
	}
}

// TestAScrubbedReplaySaysSo: the honesty rule that puts REPLAY on every frame,
// applied to the second claim a scrubbed room makes.
func TestAScrubbedReplaySaysSo(t *testing.T) {
	countSpawns(t)
	rec, out := scrubbed(t)
	sc := &recording{room: out[0], lines: out[1:]}
	m := newReplayModel(Options{}, sc, "demo.jsonl")
	if !strings.Contains(m.st.Notice, "scrubbed") {
		t.Errorf("the opening notice does not say scrubbed: %q", m.st.Notice)
	}
	m.replay.i = len(sc.lines)
	m.replayNext()
	if !strings.Contains(m.st.Notice, "scrubbed") {
		t.Errorf("the closing notice does not say scrubbed: %q", m.st.Notice)
	}
	// A capture says nothing of the sort.
	live := newReplayModel(Options{}, rec, fixtureRecording)
	if strings.Contains(live.st.Notice, "scrubbed") {
		t.Errorf("a capture was called scrubbed: %q", live.st.Notice)
	}
}

// TestReplayCheckSaysScrubbedAndNotVerbatim. `verbatim` is the warning on
// those lines; over a scrub it would be a false one.
func TestReplayCheckSaysScrubbedAndNotVerbatim(t *testing.T) {
	_, out := scrubbed(t)
	path := filepath.Join(t.TempDir(), "demo.jsonl")
	writeRecordLines(t, path, out)
	var buf bytes.Buffer
	if err := ReplayCheck(path, &buf); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{"scrubbed:", "synthesized", "carries no conversation"} {
		if !strings.Contains(got, want) {
			t.Errorf("replay-check does not say %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "verbatim") {
		t.Errorf("replay-check called a scrubbed file verbatim:\n%s", got)
	}
}

// writeRecordLines writes lines as a recording file.
func writeRecordLines(t *testing.T, path string, lines []recordLine) {
	t.Helper()
	var buf bytes.Buffer
	for _, line := range lines {
		raw, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(append(raw, '\n'))
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestAScrubRefusesThePathsARecordRefuses. Same file, same class of content,
// same two refusals: telltale's own state directory, and a file that is
// already there.
func TestAScrubRefusesThePathsARecordRefuses(t *testing.T) {
	home, _ := os.UserHomeDir()
	var buf bytes.Buffer
	own := filepath.Join(home, ".telltale", "demo.jsonl")
	if err := ScrubRecording(fixtureRecording, own, &buf); err == nil || !strings.Contains(err.Error(), "~/.telltale") {
		t.Errorf("a scrub into ~/.telltale = %v, want a refusal", err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "demo.jsonl")
	if err := ScrubRecording(fixtureRecording, out, &buf); err != nil {
		t.Fatal(err)
	}
	if err := ScrubRecording(fixtureRecording, out, &buf); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("a second scrub onto one path = %v, want a refusal", err)
	}
	if err := ScrubRecording(out, out, &buf); err == nil || !strings.Contains(err.Error(), "being read") {
		t.Errorf("a scrub onto its own input = %v, want a refusal", err)
	}
	if err := ScrubRecording(filepath.Join(dir, "missing.jsonl"), filepath.Join(dir, "b.jsonl"), &buf); err == nil {
		t.Error("a missing input was scrubbed")
	}
	// The file it did write is a recording, and it says what it is.
	back, err := readRecording(out)
	if err != nil || !back.room.Scrubbed {
		t.Errorf("the written file = %v, scrubbed %v", err, back != nil && back.room.Scrubbed)
	}
}

// TestReplayCheckNamesASelfRead is the 2026-09-04 review's fault D: on
// 2026-09-03 a seat read the room's own recording, the trace showed the read,
// and nothing named it as the recording.
func TestReplayCheckNamesASelfRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "room.jsonl")
	room := `{"kind":"room","v":1,"started":"2026-09-01T10:00:00Z","workspace":"~/x","seats":[{"vendor":"grok","label":"Grok"}]}`
	body := room + "\n" +
		`{"kind":"event","ms":1200,"vendor":"grok","event":"activity","acts":[{"id":"a","text":"Read ` + "`" + escapeJSONPath(path) + `"}]}` + "\n" +
		`{"kind":"event","ms":1400,"vendor":"grok","event":"activity","acts":[{"id":"b","text":"Read ` + "`" + `room.jsonl"}]}` + "\n" +
		`{"kind":"event","ms":1600,"vendor":"grok","event":"activity","acts":[{"id":"c","text":"Read ` + "`" + `notes.md"}]}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := ReplayCheck(path, &buf); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{
		"self-read: grok read this recording at 1200ms",
		"self-read: grok read this recording at 1400ms",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("replay-check does not say %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "at 1600ms") {
		t.Errorf("an ordinary read was called a self-read:\n%s", got)
	}
	// A file nothing read says nothing.
	var clean bytes.Buffer
	if err := ReplayCheck(fixtureRecording, &clean); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(clean.String(), "self-read") {
		t.Errorf("the fixture was called a self-read:\n%s", clean.String())
	}
}

// escapeJSONPath makes a Windows path safe to paste inside a JSON string.
func escapeJSONPath(p string) string { return strings.ReplaceAll(p, `\`, `\\`) }

// TestRecordPlacementWarning is the other half of fault D: a recording the
// seats can reach gets said out loud once, before the room opens.
func TestRecordPlacementWarning(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "repo")
	for _, tc := range []struct {
		name, path string
		want       string
	}{
		{"inside the workspace", filepath.Join(ws, "run.jsonl"), "inside the workspace"},
		{"deeper inside it", filepath.Join(ws, "docs", "run.jsonl"), "inside the workspace"},
		{"the parent directory", filepath.Join(root, "run.jsonl"), "one directory above"},
		{"a seat's worktree", filepath.Join(root, "repo-seat-grok", "run.jsonl"), ""},
		{"somewhere else", filepath.Join(root, "other", "deeper", "run.jsonl"), ""},
		{"no record path", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := recordPlacementWarning(tc.path, ws)
			if tc.want == "" {
				if got != "" {
					t.Errorf("warned about %s: %q", tc.path, got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("warning = %q, want it to say %q", got, tc.want)
			}
		})
	}
	// A room with no workspace resolved says nothing rather than guessing.
	if got := recordPlacementWarning(filepath.Join(ws, "run.jsonl"), ""); got != "" {
		t.Errorf("warned with no workspace: %q", got)
	}
}

// TestScrubShapeKeepsAPathAPath. The trace line truncates on width, so a fake
// path has to have the original's depth, its separators and its length, or a
// golden pins a frame the room never drew.
func TestScrubShapeKeepsAPathAPath(t *testing.T) {
	r := newRNG(1)
	for _, in := range []string{
		`C:\Users\someone\Desktop\rooms\scratch-seat-claude\notes.md`,
		`~/code/example/cmd/main.go`,
		`"C:\\WINDOWS\\system32\\cmd.exe" /c 'type hello.py'`,
		`git -C "C:\Users\someone\repo" status`,
	} {
		got := shape(r, in)
		if got == in {
			t.Errorf("shape kept %q whole", in)
		}
		if len([]rune(got)) != len([]rune(in)) {
			t.Errorf("shape(%q) is %d runes, want %d", in, len([]rune(got)), len([]rune(in)))
		}
		for i, c := range in {
			if isAlnum(c) {
				continue
			}
			if []rune(got)[i] != c {
				t.Errorf("shape(%q) moved %q at %d", in, string(c), i)
			}
		}
	}
	// The two runs that make a Windows path readable as one, and the
	// extension that makes a tool line readable as one.
	got := shape(newRNG(2), `C:\Users\someone\notes.md`)
	if !strings.HasPrefix(got, `C:\Users\`) || !strings.HasSuffix(got, ".md") {
		t.Errorf("a Windows path stopped looking like one: %q", got)
	}
	// A string with nothing to replace comes back whole, which is how the
	// workspace `~` of a room opened at home stays `~`.
	if got := shape(newRNG(3), "~"); got != "~" {
		t.Errorf("shape(~) = %q", got)
	}
}

// TestScrubKeepsAToolNameAndDropsAFileName. The act's head is the kind the
// trace draws; a bare word in that slot is a file name often enough that it
// goes.
func TestScrubKeepsAToolNameAndDropsAFileName(t *testing.T) {
	s := newScrubber()
	for _, tc := range []struct{ in, head string }{
		{"read_file", "read_file"},
		{"run_terminal_command", "run_terminal_command"},
		{"grep", "grep"},
		{`Read ` + "`" + `C:\Users\someone\notes.md`, "Read `"},
		{"Write: C:\\Users\\someone\\notes.md", "Write: "},
		{"write_to_file: C:\\Users\\someone\\notes.md", "write_to_file: "},
		{"hello", ""},
		{"rebuttal", ""},
		{"one-line README", ""},
		{"Updating plan", ""},
	} {
		got := s.act(tc.in, 1, 0)
		if len([]rune(got)) != len([]rune(tc.in)) {
			t.Errorf("act(%q) is %d runes, want %d", tc.in, len([]rune(got)), len([]rune(tc.in)))
		}
		if tc.head == "" {
			if strings.HasPrefix(got, strings.SplitN(tc.in, " ", 2)[0]) {
				t.Errorf("act(%q) kept a head it could not name: %q", tc.in, got)
			}
			continue
		}
		if !strings.HasPrefix(got, tc.head) {
			t.Errorf("act(%q) = %q, want it to lead with %q", tc.in, got, tc.head)
		}
		if tc.head != tc.in && got == tc.in {
			t.Errorf("act(%q) kept its argument", tc.in)
		}
	}
	// An empty act is a RESULT resolving an earlier call by id. It names
	// nothing, and replay-check counts these rather than listing them.
	if got := s.act("", 1, 0); got != "" {
		t.Errorf("an unnamed act became %q", got)
	}
	// An MCP tool names a server the OPERATOR wired up, so both halves go.
	// The shape stays: same length, same underscores, same hyphen.
	mcp := s.act("kb-agent__search_kb", 2, 0)
	if strings.Contains(mcp, "kb") || strings.Contains(mcp, "search") {
		t.Errorf("an MCP tool name survived: %q", mcp)
	}
	if len(mcp) != len("kb-agent__search_kb") || !strings.Contains(mcp, "__") || !strings.Contains(mcp, "-") {
		t.Errorf("an MCP tool name lost its shape: %q", mcp)
	}
}

// TestScrubProseKeepsEveryNewline. The column wraps on width and scrolls on
// line count, so a paragraph that lost a break would draw a different room.
func TestScrubProseKeepsEveryNewline(t *testing.T) {
	in := "first line\nsecond line is longer\n\nand a last one\n"
	got := newProseStream(7).prose(in)
	if got == in {
		t.Error("the prose was kept")
	}
	if len([]rune(got)) != len([]rune(in)) {
		t.Errorf("prose is %d runes, want %d", len([]rune(got)), len([]rune(in)))
	}
	for i, c := range in {
		if c == '\n' && []rune(got)[i] != '\n' {
			t.Errorf("newline at %d moved: %q", i, got)
		}
	}
	if strings.Count(got, "\n") != strings.Count(in, "\n") {
		t.Errorf("newline count changed: %q", got)
	}
	// A stream hands out one sentence across many chunks, which is what makes
	// a token-granular seat's reply read as words rather than as one.
	p := newProseStream(11)
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString(p.prose("abc"))
	}
	if !strings.Contains(b.String(), " ") {
		t.Errorf("twenty chunks came out as one word: %q", b.String())
	}
}
