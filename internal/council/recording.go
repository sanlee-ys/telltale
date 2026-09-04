package council

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The room's recording (design.md §9.56): `--record <file>` keeps a real run,
// and `--replay <file>` shows it again without a vendor (replay.go).
//
// # Why it exists
//
// The room cannot be seen without five paid CLI logins. Every frame the README
// could show is a frame somebody had to spend quota to produce, on a machine
// with every vendor installed, and the two conditions the hero slot puts on a
// capture — the owner drives it, and every frame is reviewed for paths and
// identity before it is committed — describe a thing that is hard to make
// and cannot be re-made. A recording is that run, kept: the event stream the
// room applied, with the clock it applied it on, so the same room can be
// drawn again by a binary that starts nothing.
//
// # What it is, and what it is not
//
// A recording is a REAL run played back. It is not the "invented recording"
// design.md §8 refuses — a scripted race is invented because nobody ran it;
// a replay is a run somebody ran, and every event in it was measured off a
// vendor's own stdout the day it happened. What the honesty rule demands in
// exchange is the LABEL: a replayed frame must never pass for a live one, so
// REPLAY sits in the header where WRITE and READ sit, on every column's badge
// row, and in the footer where enter is named — on every frame, in words
// that survive --ascii and NO_COLOR.
//
// # The boundary this file is written under
//
// CLAUDE.md's read/write boundary enumerates what telltale may write and holds
// every one of those writes to "numbers and keys only, never content": the
// quota relay, the token relay, and council's own room.json (session ids and
// a workspace, never transcript or brief content). A recording is CONTENT —
// each brief, each reply, each tool line, verbatim — and so it is NOT one of
// those writes and is not argued as one. It is the file the event sink's
// paragraph in CLAUDE.md describes: "a fourth exception different in kind",
// where what contains it is SCOPE, not redaction. Three facts contain it:
//
//   - **Explicit path.** The file is the operator's, at a path the operator
//     typed. Nothing under ~/.telltale is ever written by this code, and a
//     --record path that resolves inside ~/.telltale is refused before a byte
//     is written (openRecorder): that directory is where the gauges keep
//     their numbers-and-keys stores, and a content file among them would be
//     the boundary crossed by location rather than by argument. The room.json
//     rule is untouched — a recording carries no session id the room did not
//     already hold, and it never rewrites the room file.
//   - **Explicit flag.** `--record` is typed at the door and does one thing.
//     No key in the room starts a recording, no default location is searched,
//     and a room opened without the flag writes nothing this file describes.
//   - **Off by default.** The ordinary `telltale council` is unchanged: the
//     recorder is nil, every hook on it is a no-op, and no test that builds a
//     Model gets one.
//
// # No redaction, on purpose
//
// The recorder writes what applyEvents saw and nothing else. It does not run
// the redactor, because a recording that differed from the run would be a
// second truth: the replay puts every event through the SAME redactor the
// live room used (applyEvents is the one path), so what the replay draws is
// what the room drew, and the file underneath is the raw stream. The cost is
// that the raw stream may hold what the redactor would have hidden on
// screen, and the answer to that is the review the README already requires
// of every frame, given a tool: `telltale council replay-check <file>` lists
// the workspace, the seats, every session id, and every tool line and gate
// card the file carries, so the owner can read the identities before a
// capture is committed. It does not read the prose, and says so.
//
// # The file
//
// JSON lines. The first line is the room as it stood before the first key;
// every later line is one record with a millisecond offset from the moment
// the recorder opened, read from the monotonic clock. Four kinds:
//
//	{"kind":"room","v":1,"started":…,"workspace":…,"write":…,"seats":[…]}
//	{"kind":"dispatch","ms":…,"turn":N,"route":{…},"sent":[{"vendor":…,"prompt":…}]}
//	{"kind":"event","ms":…,"vendor":…,"event":"text|activity|session|meta|gate|done|error",…}
//	{"kind":"gate","ms":…,"vendor":…,"request_id":…,"allow":…}
//
// One flat record type rather than four, so a hand-written fixture is
// readable and the reader has one shape to validate. runner.Gate.Input — the
// whole argument blob — is the one field of an event that is NOT recorded:
// it is never rendered, a replay has no vendor to hand it back to, and it is
// a Write's entire file content.
//
// What the recording does NOT hold, and a replay therefore cannot show: the
// operator's cancels and give-ups (a column cancelled live replays as the
// vendor's own exit), focus moves, scrolling, and the text of the --brief
// file (each seat's Prompt is the brief the operator typed, as startTurn
// echoes it). Each of those is a fact about the operator, not the run.

// recordingVersion is the file format's version. A reader refuses any other.
const recordingVersion = 1

// maxRecording bounds what readRecording will load. A room that streamed for
// an afternoon is a few megabytes; anything at this path past sixty-four is
// not a file council wrote, and reading it into memory to find that out is
// the wrong order to do things in (LoadRoom's rule).
const maxRecording = 64 << 20

// maxRecordLine bounds one line. A single event is one vendor line plus its
// framing; four megabytes is well past any measured line.
const maxRecordLine = 4 << 20

// recordLine is one line of the file. Every kind uses the fields it needs and
// omits the rest; the doc comment above says which.
type recordLine struct {
	Kind string `json:"kind"`
	MS   int64  `json:"ms,omitempty"`

	// The room line.
	Version   int          `json:"v,omitempty"`
	Started   string       `json:"started,omitempty"`
	Workspace string       `json:"workspace,omitempty"`
	Write     bool         `json:"write,omitempty"`
	GateOff   bool         `json:"gate_off,omitempty"`
	Briefed   bool         `json:"briefed,omitempty"`
	SeatsAll  bool         `json:"seats_all,omitempty"`
	SeatsOnly []string     `json:"seats_only,omitempty"`
	Seats     []recordSeat `json:"seats,omitempty"`

	// The dispatch line.
	Turn  int          `json:"turn,omitempty"`
	Route *recordRoute `json:"route,omitempty"`
	Sent  []recordSent `json:"sent,omitempty"`

	// The event line, and the gate line's vendor and request id.
	Vendor    string      `json:"vendor,omitempty"`
	Event     string      `json:"event,omitempty"`
	Text      string      `json:"text,omitempty"`
	SessionID string      `json:"session_id,omitempty"`
	EndsTurn  bool        `json:"ends_turn,omitempty"`
	Acts      []recordAct `json:"acts,omitempty"`
	CostUSD   *float64    `json:"cost_usd,omitempty"`
	ExitCode  int         `json:"exit,omitempty"`
	Err       string      `json:"err,omitempty"`
	Note      string      `json:"note,omitempty"`
	Failure   int         `json:"failure,omitempty"`
	Gate      *recordGate `json:"gate,omitempty"`

	// The gate line.
	RequestID string `json:"request_id,omitempty"`
	Allow     bool   `json:"allow,omitempty"`

	// stale marks an event line the replay must not apply: a process exit
	// that belongs to a seat's replaced process (markStaleExits). Derived on
	// read, never written.
	stale bool
}

// recordSeat is one column as the room opened it: enough to draw the seat
// header, the badge row and the unavailable card, and nothing about the
// machine — no binary path, no PATH source.
type recordSeat struct {
	Vendor  string `json:"vendor"`
	Label   string `json:"label"`
	Avail   int    `json:"avail"`
	Sandbox int    `json:"sandbox"`
	Detail  string `json:"detail,omitempty"`
	Gran    int    `json:"gran"`
	Note    string `json:"note,omitempty"`
}

// recordRoute is a Route as dispatched. No vendors and not negated is the
// zero route, everyone seated; Auto never reaches a dispatch (dispatchAuto
// resolves it first) and has no field.
type recordRoute struct {
	Vendors []string `json:"vendors,omitempty"`
	Negated bool     `json:"negated,omitempty"`
}

// recordSent is one seat a dispatch put in flight: the brief as its column
// echoes it, whether it quoted the others, and whether it ran on a process
// that outlives the turn — the last one decides how a replay reads its
// end-of-turn line (applyEvents' persistent branches).
type recordSent struct {
	Vendor     string `json:"vendor"`
	Prompt     string `json:"prompt"`
	Quoted     bool   `json:"quoted,omitempty"`
	Persistent bool   `json:"persistent,omitempty"`
}

// recordAct is runner.ActCall, field for field.
type recordAct struct {
	ID      string `json:"id,omitempty"`
	Text    string `json:"text"`
	Outcome int    `json:"outcome,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// recordGate is runner.Gate without Input.
type recordGate struct {
	RequestID string `json:"request_id"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	Tool      string `json:"tool,omitempty"`
	Text      string `json:"text"`
	Old       string `json:"old,omitempty"`
	New       string `json:"new,omitempty"`
}

// eventWords is EventKind's spelling in the file, both ways. Words rather
// than the enum's integers, so a fixture reads as what it is and a kind added
// to the runner has to be named here before a recording can carry it.
var eventWords = map[runner.EventKind]string{
	runner.KindText:     "text",
	runner.KindActivity: "activity",
	runner.KindSession:  "session",
	runner.KindMeta:     "meta",
	runner.KindGate:     "gate",
	runner.KindDone:     "done",
	runner.KindError:    "error",
}

func eventKind(word string) (runner.EventKind, bool) {
	for k, w := range eventWords {
		if w == word {
			return k, true
		}
	}
	return 0, false
}

// eventRecord is one runner.Event as a line. Input is dropped, as the file's
// doc comment says; everything else the room could read off the event is kept.
func eventRecord(ev runner.Event) recordLine {
	r := recordLine{
		Kind:      "event",
		Vendor:    string(ev.Vendor),
		Event:     eventWords[ev.Kind],
		Text:      ev.Text,
		SessionID: ev.SessionID,
		EndsTurn:  ev.EndsTurn,
		CostUSD:   ev.CostUSD,
		ExitCode:  ev.ExitCode,
		Note:      ev.Note,
		Failure:   int(ev.Failure),
	}
	if ev.Err != nil {
		r.Err = ev.Err.Error()
	}
	for _, a := range ev.Acts {
		r.Acts = append(r.Acts, recordAct{ID: a.ID, Text: a.Text, Outcome: int(a.Outcome), Detail: a.Detail})
	}
	if ev.Gate != nil {
		r.Gate = &recordGate{
			RequestID: ev.Gate.RequestID, ToolUseID: ev.Gate.ToolUseID,
			Tool: ev.Gate.Tool, Text: ev.Gate.Text,
			Old: ev.Gate.OldContent, New: ev.Gate.NewContent,
		}
	}
	return r
}

// event is the line as the runner.Event applyEvents will see. Err comes back
// as an error carrying the same words; Input comes back empty, which a replay
// never reads.
func (r recordLine) event() (runner.Event, bool) {
	kind, ok := eventKind(r.Event)
	if !ok {
		return runner.Event{}, false
	}
	ev := runner.Event{
		Vendor:    model.VendorID(r.Vendor),
		Kind:      kind,
		Text:      r.Text,
		SessionID: r.SessionID,
		EndsTurn:  r.EndsTurn,
		CostUSD:   r.CostUSD,
		ExitCode:  r.ExitCode,
		Note:      r.Note,
		Failure:   runner.FailureClass(r.Failure),
	}
	if r.Err != "" {
		ev.Err = errors.New(r.Err)
	}
	for _, a := range r.Acts {
		ev.Acts = append(ev.Acts, runner.ActCall{ID: a.ID, Text: a.Text, Outcome: runner.ActStatus(a.Outcome), Detail: a.Detail})
	}
	if r.Gate != nil {
		ev.Gate = &runner.Gate{
			RequestID: r.Gate.RequestID, ToolUseID: r.Gate.ToolUseID,
			Tool: r.Gate.Tool, Text: r.Gate.Text,
			OldContent: r.Gate.Old, NewContent: r.Gate.New,
		}
	}
	return ev, true
}

// recorder writes the file. Every method is nil-safe, so the hooks that feed
// it (Update's event batch, holdTurn, gateKey) cost the ordinary room one
// branch and no test has to build one.
//
// A mutex, although every writer today is the update loop: the file is the
// room's memory and the cost of the lock is nothing, while the cost of a
// second writer arriving in a later change and interleaving two lines is a
// file the reader refuses.
type recorder struct {
	mu    sync.Mutex
	f     *os.File
	path  string
	began time.Time
	err   error
}

// openRecorder creates the file, and refuses two paths before it does.
//
// A path under ~/.telltale is refused for the reason the file's doc comment
// gives at length: that directory holds numbers and keys, and this file holds
// content. An existing file is refused rather than truncated or appended to:
// a recording is one run, and a second run appended after it would be two
// headers in one file — while truncating would destroy a capture the owner
// may have already reviewed. Name a new file.
func openRecorder(path string) (*recorder, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("--record %s: %w", path, err)
	}
	if home, herr := os.UserHomeDir(); herr == nil {
		own := filepath.Join(home, ".telltale")
		if rel, rerr := filepath.Rel(own, abs); rerr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, errors.New("--record " + path + ": that is telltale's own state directory, which holds numbers and keys only — a recording carries the conversation, so name a path outside ~/.telltale")
		}
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, errors.New("--record " + path + ": the file already exists — a recording is one run, so name a new file rather than overwrite or extend a capture")
		}
		return nil, fmt.Errorf("--record %s: %w", path, err)
	}
	return &recorder{f: f, path: abs, began: time.Now()}, nil
}

// write appends one line. The first error is kept and reported at close; the
// room is the product and a full disk must not take it down (trace.go's rule).
func (r *recorder) write(line recordLine) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil || r.err != nil {
		return
	}
	raw, err := json.Marshal(line)
	if err != nil {
		r.err = err
		return
	}
	if _, err := r.f.Write(append(raw, '\n')); err != nil {
		r.err = err
	}
}

// ms is the monotonic offset from the moment the file was opened. Zero on a
// nil recorder, so applyEvents can take the stamp without a branch.
func (r *recorder) ms() int64 {
	if r == nil {
		return 0
	}
	return time.Since(r.began).Milliseconds()
}

// room writes the first line: the room before the first key.
//
// The workspace is recorded as the header DRAWS it — abbreviated against the
// home directory — rather than as the absolute path, so the file discloses
// what the screen disclosed and no more, and the replay's header reads
// exactly as the live one did.
func (r *recorder) room(st State) {
	if r == nil {
		return
	}
	line := recordLine{
		Kind:      "room",
		Version:   recordingVersion,
		Started:   r.began.UTC().Format(time.RFC3339),
		Workspace: abbreviate(st.Workspace, st.Home),
		Write:     st.Write,
		GateOff:   st.GateOff,
		Briefed:   st.Briefed,
		SeatsAll:  st.Seats.All,
	}
	for _, v := range st.Seats.Only {
		line.SeatsOnly = append(line.SeatsOnly, string(v))
	}
	// The columns the room DRAWS, not every column it holds. A seat --vendor
	// left out is still a Column (its card stays reachable), but it takes no
	// turn and no frame shows it, so a file that listed it would replay and
	// replay-check a room the operator never had. Measured 2026-09-03: a
	// four-seat room recorded five seats and replay-check listed the fifth.
	for _, i := range st.VisibleColumns() {
		c := st.Columns[i]
		line.Seats = append(line.Seats, recordSeat{
			Vendor: string(c.Vendor), Label: c.Label, Avail: int(c.Avail),
			Sandbox: int(c.Sandbox.Level), Detail: c.Sandbox.Detail,
			Gran: int(c.Gran), Note: c.Note,
		})
	}
	r.write(line)
}

// event writes one event applyEvents is about to apply, at the batch's stamp.
// Called from applyEvents itself, after its stale-exit guard, so the file
// carries what the room applied and not what the channel delivered.
func (r *recorder) event(ev runner.Event, ms int64) {
	if r == nil {
		return
	}
	line := eventRecord(ev)
	line.MS = ms
	r.write(line)
}

// gate writes one operator decision on one card.
func (r *recorder) gate(p PendingGate, allow bool) {
	if r == nil {
		return
	}
	r.write(recordLine{Kind: "gate", MS: r.ms(), Vendor: string(p.Vendor), RequestID: p.RequestID, Allow: allow})
}

// close ends the file and reports the first write error, if any.
func (r *recorder) close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return r.err
	}
	if cerr := r.f.Close(); cerr != nil && r.err == nil {
		r.err = cerr
	}
	r.f = nil
	return r.err
}

// recordDispatch is holdTurn's hook: the dispatch as its seats now hold it.
//
// Read off the columns rather than off the arguments sendTurn had, because
// the columns are what the replay has to reproduce: each seat's Prompt is the
// brief as startTurn echoed it, and TurnN is the number the separator prints.
// Seating order, so the replay's columns start in the order the room's did.
func (m *Model) recordDispatch(ts *turnState) {
	if m.rec == nil || ts == nil {
		return
	}
	line := recordLine{Kind: "dispatch", MS: m.rec.ms(), Turn: ts.n, Route: &recordRoute{Negated: ts.route.Negated}}
	for _, v := range ts.route.Vendors {
		line.Route.Vendors = append(line.Route.Vendors, string(v))
	}
	for _, c := range m.st.Columns {
		if !ts.live[c.Vendor] {
			continue
		}
		line.Sent = append(line.Sent, recordSent{
			Vendor: string(c.Vendor), Prompt: c.Prompt, Quoted: c.Quoted,
			Persistent: ts.persistent[c.Vendor],
		})
	}
	m.rec.write(line)
}

// recordGate is gateKey's hook: the oldest card, which is the one the key
// answers (decideGate's own rule), and how it was answered.
func (m *Model) recordGate(allow bool) {
	if m.rec == nil || len(m.st.Gates) == 0 {
		return
	}
	m.rec.gate(m.st.Gates[0], allow)
}

// recording is one file, read and checked.
type recording struct {
	room  recordLine
	lines []recordLine
}

// readRecording loads and validates a file. Every refusal names the line, so
// a hand-edited fixture that breaks is fixed at the line rather than by
// re-recording.
//
// The clock is checked as well as the shape: a record earlier than the one
// before it is a file no recorder wrote, and a replay that ran it would sleep
// a negative time or draw a turn ending before it began.
func readRecording(path string) (*recording, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("--replay %s: %w", path, err)
	}
	if fi.Size() > maxRecording {
		return nil, errors.New("--replay " + path + ": the file is implausibly large for a recording")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("--replay %s: %w", path, err)
	}
	defer f.Close()
	return parseRecording(f, path)
}

func parseRecording(src io.Reader, path string) (*recording, error) {
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 0, 64<<10), maxRecordLine)
	rec := &recording{}
	n := 0
	seats := map[string]bool{}
	var last int64
	for sc.Scan() {
		n++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		var line recordLine
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			return nil, fmt.Errorf("--replay %s: line %d is not a record: %v", path, n, err)
		}
		if n == 1 || (rec.room.Kind == "" && len(rec.lines) == 0) {
			if line.Kind != "room" {
				return nil, fmt.Errorf("--replay %s: line %d: a recording opens with the room line, not %q", path, n, line.Kind)
			}
			if line.Version != recordingVersion {
				return nil, fmt.Errorf("--replay %s: line %d: recording version %d; this build reads version %d", path, n, line.Version, recordingVersion)
			}
			if len(line.Seats) == 0 {
				return nil, fmt.Errorf("--replay %s: line %d: the room line names no seats", path, n)
			}
			for _, s := range line.Seats {
				seats[s.Vendor] = true
			}
			rec.room = line
			continue
		}
		if line.MS < last {
			return nil, fmt.Errorf("--replay %s: line %d runs backwards (%dms after %dms)", path, n, line.MS, last)
		}
		last = line.MS
		switch line.Kind {
		case "dispatch":
			if line.Turn <= 0 || len(line.Sent) == 0 {
				return nil, fmt.Errorf("--replay %s: line %d: a dispatch needs a turn number and at least one seat", path, n)
			}
			for _, s := range line.Sent {
				if !seats[s.Vendor] {
					return nil, fmt.Errorf("--replay %s: line %d: dispatch to %q, which the room line does not seat", path, n, s.Vendor)
				}
			}
		case "event":
			if _, ok := eventKind(line.Event); !ok {
				return nil, fmt.Errorf("--replay %s: line %d: unknown event %q", path, n, line.Event)
			}
			if !seats[line.Vendor] {
				return nil, fmt.Errorf("--replay %s: line %d: event for %q, which the room line does not seat", path, n, line.Vendor)
			}
		case "gate":
			if line.RequestID == "" || !seats[line.Vendor] {
				return nil, fmt.Errorf("--replay %s: line %d: a gate decision needs a seat and a request id", path, n)
			}
		default:
			return nil, fmt.Errorf("--replay %s: line %d: unknown record kind %q", path, n, line.Kind)
		}
		rec.lines = append(rec.lines, line)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("--replay %s: %w", path, err)
	}
	if rec.room.Kind == "" {
		return nil, errors.New("--replay " + path + ": the file is empty")
	}
	markStaleExits(rec.lines)
	return rec, nil
}

// markStaleExits finds the process exits a replay must skip.
//
// The recorder no longer writes them (applyEvents' staleExit guard), but the
// files recorded before that guard existed carry them, and the first real
// recording is one of those: every dispatch to a persistent seat was followed
// by a `done` from the seat's PREVIOUS process, 0.1s to 11s later, and the
// replay read each one as the new turn ending in failure. The live room had
// attributed the exit to the replaced process by asking the current one
// whether it was alive; the file carries only the vendor name.
//
// The rule that recovers the attribution without a process: an exit is the
// LAST event a process emits (runner.Session's lifecycle goroutine sends its
// one terminal event after the readers drain), so any event from the same
// seat that lands after an exit and before the seat's next dispatch came from
// a different process — the replacement the live room was already talking to.
// The exit was therefore not this turn's end. That covers the stale exit, and
// it covers the one other case where the room discards a persistent seat's
// exit and the seat goes on: a first-turn death that retreats to the batch
// adapter on the same dispatch (retreatOnDeath). An exit nothing follows is a
// real end — a death, or a one-shot seat's ordinary finish — and is applied.
//
// Only a seat dispatched as PERSISTENT on the current turn is read this way.
// A one-shot seat ends by exiting and nothing follows; if a file ever carried
// something after it, the exit would still be its end.
func markStaleExits(lines []recordLine) {
	persistent := map[string]bool{}
	for i := range lines {
		l := &lines[i]
		switch l.Kind {
		case "dispatch":
			persistent = map[string]bool{}
			for _, s := range l.Sent {
				if s.Persistent {
					persistent[s.Vendor] = true
				}
			}
			continue
		case "event":
		default:
			continue
		}
		if !persistent[l.Vendor] {
			continue
		}
		if l.Event != "done" && (l.Event != "error" || l.EndsTurn) {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			n := &lines[j]
			if n.Kind == "dispatch" {
				break
			}
			if n.Kind == "event" && n.Vendor == l.Vendor {
				l.stale = true
				break
			}
		}
	}
}

// staleExits counts the lines markStaleExits set aside.
func (r *recording) staleExits() int {
	n := 0
	for _, l := range r.lines {
		if l.stale {
			n++
		}
	}
	return n
}

// started is the recording's wall-clock start, or the zero time when the
// header's stamp cannot be read — the replay then counts from the epoch, which
// is honest for every duration on screen and wrong only for a date nothing
// draws.
func (r *recording) started() time.Time {
	t, err := time.Parse(time.RFC3339, r.room.Started)
	if err != nil {
		return time.Time{}
	}
	return t
}

// span is how long the recording runs, first record to last.
func (r *recording) span() time.Duration {
	if len(r.lines) == 0 {
		return 0
	}
	return time.Duration(r.lines[len(r.lines)-1].MS) * time.Millisecond
}

// ReplayCheck prints what a recording carries that a review has to see:
// the workspace, the seats, every session id, every tool line and gate card
// (each may name a path), and how much prose is in it — and says plainly that
// the prose is not read here.
//
// It is the README's frame review, given a tool. A capture is committed only
// after every frame is read for paths and identity; a recording is every
// frame at once, so the identities are listed at once. It reads the file
// and writes stdout, and does nothing else: no vendor, no room, no state.
func ReplayCheck(path string, w io.Writer) error {
	rec, err := readRecording(path)
	if err != nil {
		return err
	}
	h := rec.room
	fmt.Fprintf(w, "replay-check: %s\n", path)
	fmt.Fprintf(w, "  recorded %s, %d records over %s\n", h.Started, len(rec.lines), dur(rec.span()))
	fmt.Fprintf(w, "  workspace: %s\n", h.Workspace)
	posture := "read-only"
	if h.Write {
		posture = "writes, asking"
		if h.GateOff {
			posture = "writes, not asking"
		}
	}
	fmt.Fprintf(w, "  posture: %s\n", posture)
	var seats []string
	for _, s := range h.Seats {
		seats = append(seats, s.Vendor+" ("+s.Label+")")
	}
	fmt.Fprintf(w, "  seats: %s\n", strings.Join(seats, ", "))

	dispatches, texts, textChars, briefChars := 0, 0, 0, 0
	ids := map[string]map[string]bool{}
	var tools []string
	// A tool line with no name is a RESULT: the vendor resolving an earlier
	// call by id, carrying an outcome and no text (recordAct folds it into the
	// call). It names no path, so listing each one would be a bare vendor
	// word per line — measured at 112 for one seat on the first real
	// recording — and they are counted per seat instead.
	unnamed := map[string]int{}
	decided := map[string]string{}
	for _, l := range rec.lines {
		if l.Kind == "gate" {
			if l.Allow {
				decided[l.RequestID] = "allowed"
			} else {
				decided[l.RequestID] = "denied"
			}
		}
	}
	for _, l := range rec.lines {
		switch l.Kind {
		case "dispatch":
			dispatches++
			for _, s := range l.Sent {
				briefChars += len(s.Prompt)
			}
		case "event":
			if l.SessionID != "" {
				if ids[l.Vendor] == nil {
					ids[l.Vendor] = map[string]bool{}
				}
				ids[l.Vendor][l.SessionID] = true
			}
			if l.Event == "text" {
				texts++
				textChars += len(l.Text)
			}
			for _, a := range l.Acts {
				if strings.TrimSpace(a.Text) == "" {
					unnamed[l.Vendor]++
					continue
				}
				tools = append(tools, "  "+l.Vendor+"  "+a.Text)
			}
			if l.Gate != nil {
				verdict := decided[l.Gate.RequestID]
				if verdict == "" {
					verdict = "never answered in this file"
				}
				tools = append(tools, "  "+l.Vendor+"  gate  "+l.Gate.Text+"  → "+verdict)
			}
		}
	}
	fmt.Fprintf(w, "  dispatches: %d\n", dispatches)
	if n := rec.staleExits(); n > 0 {
		fmt.Fprintf(w, "  stale exits: %d (a replaced process ending after its seat moved on; a replay skips them)\n", n)
	}

	fmt.Fprintln(w, "session ids the file carries:")
	if len(ids) == 0 {
		fmt.Fprintln(w, "  none")
	}
	var vendors []string
	for v := range ids {
		vendors = append(vendors, v)
	}
	sort.Strings(vendors)
	for _, v := range vendors {
		var list []string
		for id := range ids[v] {
			list = append(list, id)
		}
		sort.Strings(list)
		for _, id := range list {
			fmt.Fprintf(w, "  %s  %s\n", v, id)
		}
	}

	fmt.Fprintln(w, "tool calls and gate cards (each may name a path):")
	if len(tools) == 0 {
		fmt.Fprintln(w, "  none")
	}
	for _, t := range tools {
		fmt.Fprintln(w, t)
	}
	var unnamedSeats []string
	for v := range unnamed {
		unnamedSeats = append(unnamedSeats, v)
	}
	sort.Strings(unnamedSeats)
	for _, v := range unnamedSeats {
		fmt.Fprintf(w, "  %s  %d unnamed tool %s: results resolving calls above by id, no path\n", v, unnamed[v], plural(unnamed[v], "result"))
	}
	fmt.Fprintf(w, "vendor output: %d text %s, %d chars, verbatim and unredacted\n", texts, plural(texts, "event"), textChars)
	dispatchWord := "dispatches"
	if dispatches == 1 {
		dispatchWord = "dispatch"
	}
	fmt.Fprintf(w, "briefs: %d %s, %d chars, verbatim\n", dispatches, dispatchWord, briefChars)
	fmt.Fprintln(w, "This file carries the conversation. This check lists identities and paths and does not read the prose;")
	fmt.Fprintln(w, "read the file whole before you commit or share it.")
	return nil
}
