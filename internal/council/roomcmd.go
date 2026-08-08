package council

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/sanlee-ys/telltale/internal/model"
)

// Room commands: drafts addressed to the ROOM rather than to the vendors.
//
// This file said "there is exactly one, /cd, because the workspace is the one
// piece of room state the P0 demands be movable from inside" — and that scoping
// claim is what design.md §9.17 retired. It was true while the room could not
// run long enough for anything else to drift; a daily driver drifts in several
// places at once, and the rule now is that ANY state which changes while the
// room is open is reachable from inside it. `/cd` and `/trace` are the two that
// take an argument, which is why they are commands rather than keys.
//
// The restraint that survives is the one about vocabulary. Only a draft that IS
// a command is intercepted; anything else, including text that merely starts
// with a slash, dispatches to the vendors as typed. Every word taken here is a
// word taken out of the conversation, which is the argument that kept `/clear`
// out of this file (§9.17) — vendors own that one.

// parseCommand recognises "<verb>" and "<verb> <arg>". The second return is the
// argument, trimmed; ok is false for every draft that should dispatch.
//
// The bare verb and the verb-plus-space are the only two forms, which is what
// keeps "/cdx" and "/tracing" prose. One function for every command so that rule
// cannot drift between them — a second command matching on a looser prefix would
// steal words this file promises not to take.
func parseCommand(draft, verb string) (arg string, ok bool) {
	s := strings.TrimSpace(draft)
	if s == verb {
		return "", true
	}
	if rest, found := strings.CutPrefix(s, verb+" "); found {
		return strings.TrimSpace(rest), true
	}
	return "", false
}

// parseRoomCommand recognises "/cd" and "/cd <dir>".
func parseRoomCommand(draft string) (arg string, ok bool) {
	return parseCommand(draft, "/cd")
}

// parseBareCommand recognises ONLY the argument-less form of a verb.
//
// Separate from parseCommand, and the difference is the vocabulary rule this
// file opens with rather than a stylistic split. `/cd` and `/trace` take an
// argument, so "/cd " and "/trace " are already unmistakably commands — nobody
// types them as prose. `/read` and `/write` take none, and both are words a
// person addresses a ROOM with: "/read the design doc before answering",
// "/write a test for this" are ordinary briefs, and intercepting them would
// steal two live verbs out of the conversation to run a setting the user did
// not ask for. So the bare draft is the command and everything else dispatches
// untouched, which is the strictest reading of "only a draft that IS a command
// is intercepted".
func parseBareCommand(draft, verb string) bool {
	return strings.TrimSpace(draft) == verb
}

// roomCommand handles a room-addressed draft, and is the ONE place a roster
// change is persisted. Returns false when the draft is ordinary and should
// dispatch.
//
// **The save is here rather than inside the command that made the change**, and
// that is the whole reason this wrapper exists (§9.32). The room file is what a
// reattach reads, so a roster held only in memory is undone by quitting — `c`'s
// argument, in `clearSeat`'s own words, applied to the other thing a user
// deliberately takes out of the room. `c` could put its `saveRoom` inside
// itself because there is exactly one way to clear a seat; the roster has `/seat`
// and `/unseat` and will have whatever narrows it next, and a save per command
// is a save the third one forgets.
//
// So it is written as an OBSERVATION rather than a call: snapshot the roster,
// run the command, save if it moved. Any command reachable from here inherits
// persistence without knowing this function exists, which is what lets a
// `/unseat` written in parallel compose with this without either side being
// told about the other.
//
// Saved only when it MOVED. `/seat` with a typo, `/seat` mid-turn and bare
// `/seat` all report without reseating, and rewriting the file on each of them
// would refresh SavedAt — the age a reattach shows — for a room that answered a
// question and did nothing.
func (m *Model) roomCommand() bool {
	before := m.st.Seats
	handled := m.runRoomCommand()
	if handled && !sameSeats(before, m.st.Seats) {
		m.saveRoom()
	}
	return handled
}

// runRoomCommand routes a room-addressed draft to the command that owns it.
// Every roster-changing command belongs in here, under roomCommand's save.
func (m *Model) runRoomCommand() bool {
	if arg, ok := parseCommand(m.st.Draft, "/trace"); ok {
		return m.traceCommand(arg)
	}
	if parseBareCommand(m.st.Draft, "/read") {
		return m.postureCommand(false)
	}
	if parseBareCommand(m.st.Draft, "/write") {
		return m.postureCommand(true)
	}
	if arg, ok := parseCommand(m.st.Draft, "/seat"); ok {
		return m.seatCommand(arg)
	}
	arg, ok := parseRoomCommand(m.st.Draft)
	if !ok {
		return false
	}
	if m.turn != nil {
		// The turn in flight was dispatched against the old directory, and the
		// spawn-per-turn seats read the workspace at dispatch. Moving it under
		// them would make the room's header disagree with where three agents
		// are actually acting.
		m.st.Notice = "a turn is in flight — /cd moves the room between turns"
		return true
	}
	if arg == "" {
		// "/cd" alone answers the question it half-asks.
		m.st.Notice = "the room is in " + abbreviate(m.st.Workspace, m.st.Home) +
			" — /cd <dir> moves it"
		m.setDraft("")
		return true
	}

	dir, err := m.resolveCD(arg)
	if err != nil {
		m.st.Notice = err.Error()
		// The draft is kept: a mistyped path is expensive to retype and nothing
		// has been dispatched — the same reasoning esc uses.
		return true
	}
	if sameDir(dir, m.st.Workspace) {
		m.st.Notice = "the room is already in " + abbreviate(dir, m.st.Home)
		m.setDraft("")
		return true
	}

	m.st.Workspace = dir
	m.setDraft("")
	// Nothing is killed here. The spawn-per-turn seats read the workspace at
	// their next dispatch, and a persistent seat's process is respawned lazily
	// by seatProcess when the mismatch is seen — so a /cd that is /cd'd back
	// before anyone dispatches costs nothing at all.
	m.st.Notice = "the room now works in " + abbreviate(dir, m.st.Home) +
		" — seats move on their next turn"
	return true
}

// cdError is a resolution failure in the words the notice shows.
type cdError string

func (e cdError) Error() string { return string(e) }

// resolveCD turns a /cd argument into the absolute directory it names.
//
// Three candidates, in order: the path as given (absolute, or relative to the
// room's CURRENT workspace, the way a shell would read it), then a SIBLING of
// the current workspace. The sibling rule is what makes "/cd kb-agent" work
// from ~/code/telltale without telltale hardcoding anyone's directory layout:
// repos that live side by side reach each other by name.
func (m *Model) resolveCD(arg string) (string, error) {
	p := arg
	// "~" is composer text, not shell input, so nothing expands it unless we
	// do. Only the leading-tilde form; a "~user" form is not worth guessing at.
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if m.st.Home == "" {
			// Expanding against an empty home would quietly resolve "~" to the
			// current workspace and report a move that never happened.
			return "", cdError("the home directory is unknown — use an absolute path")
		}
		if p == "~" {
			p = m.st.Home
		} else {
			p = filepath.Join(m.st.Home, p[2:])
		}
	}

	var candidates []string
	if filepath.IsAbs(p) {
		candidates = []string{p}
	} else {
		candidates = []string{
			filepath.Join(m.st.Workspace, p),
			filepath.Join(filepath.Dir(m.st.Workspace), p),
		}
	}
	for _, c := range candidates {
		fi, err := os.Stat(c)
		if err != nil || !fi.IsDir() {
			continue
		}
		if abs, err := filepath.Abs(c); err == nil {
			return filepath.Clean(abs), nil
		}
	}
	return "", cdError("no directory named " + arg + " here or beside " +
		abbreviate(m.st.Workspace, m.st.Home))
}

// sameDir reports whether two cleaned absolute paths name one directory.
// Case folds on Windows only — the same rule the old room key used, and for
// the same reason: `C:\Users\...` and `c:\users\...` are one directory there,
// and two directories anywhere else.
func sameDir(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// traceCommand turns the turn trace on, off, or reports where it is going.
//
// Unlike /cd and `c`, this one does NOT refuse while a turn is in flight, and
// the difference is the point rather than an inconsistency. Those two change
// state the seats are actively using — a workspace they were dispatched against,
// a thread they are mid-conversation on. This changes nothing any vendor can
// observe: it opens a file on the room's side. Refusing here would refuse at the
// exact moment the trace is most wanted, since the turn you cannot explain is
// usually the one still running — and because clock.go emits at end(), a trace
// opened mid-turn still catches that turn.
//
// A council-chosen path is deliberately absent. Bare /trace reports and never
// enables, so the only file council writes on its own initiative remains
// room.json — the read/write boundary in README.md and CLAUDE.md is a sentence
// this command is not allowed to make false.
func (m *Model) traceCommand(arg string) bool {
	switch arg {
	case "":
		// Answers the question it half-asks, the way bare /cd does.
		if p := m.trace.target(); p != "" {
			m.st.Notice = "tracing to " + abbreviate(p, m.st.Home) + " — /trace off stops"
		} else {
			m.st.Notice = "not tracing — /trace <file> records each turn's spawn/wait/stream (" +
				strconv.Itoa(m.trace.held()) + " turns held)"
		}
		m.setDraft("")
		return true
	case "off":
		if m.trace.target() == "" {
			m.st.Notice = "not tracing"
			m.setDraft("")
			return true
		}
		m.trace.close()
		// The ring keeps filling while the trace is off, so stopping is not
		// throwing anything away — and saying so is what stops "off" reading as
		// a decision you have to be sure about.
		m.st.Notice = "trace stopped — the room keeps measuring, /trace <file> resumes"
		m.setDraft("")
		return true
	}

	n, err := m.trace.open(m.resolveTrace(arg))
	if err != nil {
		m.st.Notice = "trace: " + err.Error()
		// The draft is kept, the same way /cd keeps a mistyped path: nothing has
		// been dispatched and a path is expensive to retype.
		return true
	}
	m.setDraft("")
	// The count is the honest part. A trace opened mid-conversation reaches back
	// only as far as the ring, so reporting how many turns it could actually
	// write stops a short file being read as a quiet session.
	m.st.Notice = "tracing to " + abbreviate(m.trace.target(), m.st.Home) +
		" — wrote " + strconv.Itoa(n) + " held " + plural(n, "turn") + ", and each one from here"
	return true
}

// resolveTrace turns a /trace argument into the path to open.
//
// Relative to the ROOM's workspace rather than to the process's cwd, matching
// /cd's reading of a relative path: the room is the frame of reference for
// everything else typed into it, and a trace landing wherever the terminal
// happened to start would be the one exception.
//
// A "~" prefix is expanded here for the same reason /cd expands it: this is
// composer text, not shell input, so nothing else will.
func (m *Model) resolveTrace(arg string) string {
	p := arg
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if m.st.Home != "" {
			if p == "~" {
				p = m.st.Home
			} else {
				p = filepath.Join(m.st.Home, p[2:])
			}
		}
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(m.st.Workspace, p))
}

// postureCommand moves the room between read and write from inside it.
//
// The §9.17 case for this one is the sharpest in the sweep, because the defect
// is already written down elsewhere as a feature: §9.16's refusal of a /flow
// write hop into a read-only room "names the flag that would change it". The
// room knows what you want, knows what would grant it, and could only tell you
// to quit and start over.
//
// TWO ASYMMETRIES, both deliberate.
//
// The first is the confirmation. /read applies at once and /write asks, because
// they are not the same act: tightening takes authority away from four seats and
// the worst case of a stray one is that a turn has to be re-run, while loosening
// hands editing and command authority to every seat in the room — and in an
// --auto room, hands it with nothing left asking. `c` spends a keystroke on the
// irreversible direction for exactly this reason. So does this.
//
// The second is that neither direction is offered mid-turn. Posture is argv,
// fixed at spawn (persistent.go), so the seats already running hold the flags
// they were launched with no matter what this function sets. Flipping under them
// would leave the badge claiming a posture the live process does not have, which
// is the "column would say READ while the live process still held the write
// flags" failure the per-step posture rule exists to forbid. /cd refuses for the
// same reason and this is the same refusal, not a house style.
//
// Nothing is persisted as a posture to be restored. resume.go records the
// posture "for the record only" and TestReattachDoesNotRestoreWritePosture is
// the guarantee that it never comes back from a file — "a posture that can
// arrive from a file is not one anyone typed" survives this change intact,
// because a posture typed into the composer is typed.
func (m *Model) postureCommand(write bool) bool {
	if m.turn != nil {
		// The draft is kept, the way /cd keeps it: the command is still what the
		// user wants, one turn later.
		m.st.Notice = "a turn is in flight — /read and /write move the room between turns"
		return true
	}
	if write == m.st.Write {
		if write {
			m.st.Notice = "the room already writes — /read makes it read-only"
		} else {
			m.st.Notice = "the room is already read-only — /write lets it write again"
		}
		m.setDraft("")
		return true
	}
	m.setDraft("")

	if !write {
		m.applyPosture(false)
		// "on their next turn" is the honest half. Nothing is killed here: a live
		// seat is respawned lazily by seatProcess when it sees the mismatch, the
		// same way /cd moves one, so a /read that is /write'd back before anyone
		// dispatches costs nothing at all.
		m.st.Notice = "the room is read-only — seats answer and compare, none of them writes. " +
			"They move on their next turn"
		return true
	}

	m.writePending = true
	// The card names which write the user is about to get, because the two are
	// materially different and only one of them asks first. An ungated room has
	// no gate to restore, and saying "y approves each change" there would be a
	// promise this room cannot keep.
	//
	// READ FROM m.st.Asking(), NOT m.opts.Auto. The flag only SEEDS the gate at
	// launch (stateWith) and `a` has moved it ever since (§9.17's last control),
	// so a room opened gated and then told to stop asking would have been handed
	// a card promising a seat would ask — the exact promise this branch exists to
	// avoid, made in the more dangerous direction, since the user reads "claude
	// asks before each change" and then nothing does. dispatch.go states the rule
	// for the request path and this is the same rule on the confirmation path.
	// The `--auto` wording went with it: the flag is no longer the only route to
	// an ungated room, so crediting it would name a cause that may not be true.
	if !m.st.Asking() {
		m.st.Notice = "let the room write again? y confirms — nothing will ask before it acts · n keeps it read-only"
	} else {
		m.st.Notice = "let the room write again? y confirms — claude asks before each change, the other seats do not · n keeps it read-only"
	}
	return true
}

// seatCommand decides who is in the room, from inside it. `--vendor`'s twin,
// and it takes the same argument on purpose — `/seat claude,codex` and
// `--vendor claude,codex` are one grammar, the way `/cd` is `--cd`'s.
//
// WHAT IT DOES NOT DO IS THE DESIGN. An unseated seat keeps its thread, keeps
// its process, and keeps every id that would resume it; all that changes is
// whether it is drawn and dispatched to. That was ruled deliberately over the
// alternative of killing the process to reclaim it:
//
//   - The thread is what the user is protecting. A seat with a live process and
//     no reported session id yet has its whole conversation IN that process
//     (§9.8) — killing it there destroys a thread that `seatHasThread` would
//     have called real, silently, on a command nobody thinks of as destructive.
//     That is `c`'s job, and `c` asks first for exactly this reason.
//   - Nothing is being spent. An unseated seat is never dispatched to, so an
//     idle process costs a process and no quota. Trading a guaranteed-correct
//     return for a resource nobody is short of is the wrong trade.
//
// So this is fully reversible by construction: `/seat all` puts everyone back
// where they were, mid-conversation, with no resume to fail. What it buys is
// what the fold-out already buys an uninstalled seat — the WIDTH goes to the
// seats that are answering.
//
// Sitting out is a different control and already exists: a seat nobody
// addresses does not answer and is not billed (§9.19 renders a long absence as
// one line). This is for the seat you want off the SCREEN, not merely quiet.
//
// It does not save, and that is not an omission: roomCommand persists any roster
// this returns having moved (§9.32), so the file follows without this function
// or its future siblings having to remember to write it. What that does NOT
// cover is a room that has never dispatched — saveRoom writes nothing at turn 0,
// because a room with no turns has no keys to save and readRoom refuses one
// anyway. A `/seat` typed before the first brief rides out on that brief's own
// save, which is the only save there was ever going to be.
func (m *Model) seatCommand(arg string) bool {
	if m.turn != nil {
		// The grid for a turn in flight was decided at dispatch (frameOwnersFor),
		// so reseating under it would redraw the room around columns that are
		// mid-answer. /cd's refusal, for /cd's reason.
		m.st.Notice = "a turn is in flight — /seat changes the room between turns"
		return true
	}

	if arg == "" {
		m.st.Notice = "seated: " + strings.Join(m.seatedLabels(), " ") +
			" — /seat <list> narrows, /seat all puts everyone back"
		m.setDraft("")
		return true
	}

	if arg == "all" {
		m.st.Seats = Seats{All: true}
		m.setDraft("")
		m.st.Notice = "everyone is seated — threads were kept, so each seat carries on where it left off"
		return true
	}

	want, unknown := parseSeatList(arg)
	if len(unknown) > 0 {
		// Named but unrecognised is a typo, and a typo that silently seated a
		// smaller room than asked for would be discovered as a missing answer
		// several turns later.
		m.st.Notice = "no seat called " + strings.Join(unknown, " or ") +
			" — /seat takes claude, codex, agy, cursor, or all"
		return true
	}
	if len(want) == 0 {
		m.st.Notice = "/seat needs at least one seat — /seat all puts everyone back"
		return true
	}

	m.st.Seats = Seats{Only: want}
	m.setDraft("")

	notice := "seated: " + strings.Join(m.seatedLabels(), " ") + " — the rest keep their threads, /seat all brings them back"
	// The default route is claude, so a room that unseats claude answers nothing
	// at all until every brief is @mentioned. Dispatch would say so per turn
	// (seatedIn == 0); saying it once, here, is the difference between a rule the
	// user learns now and one they discover on their next enter.
	if !m.seatsVendor(model.VendorClaude) {
		notice += ". Unaddressed briefs go to claude, who is not seated — @mention a seat"
	}
	m.st.Notice = notice
	return true
}

// parseSeatList reads "claude,codex" into vendors, through the SAME alias table
// @mentions use. Two tables would let /seat agy work and @agy not, or the
// reverse, and the room would be teaching two vocabularies for one set of names.
func parseSeatList(arg string) (want []model.VendorID, unknown []string) {
	aliases := mentionAliases()
	seen := map[model.VendorID]bool{}
	for _, f := range strings.Split(arg, ",") {
		name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(f, "@")))
		if name == "" {
			continue
		}
		v, ok := aliases[name]
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		if !seen[v] {
			seen[v] = true
			want = append(want, v)
		}
	}
	return want, unknown
}

// seatsVendor reports whether this vendor takes turns in the room as it stands.
func (m *Model) seatsVendor(v model.VendorID) bool {
	for _, c := range m.st.Columns {
		if c.Vendor == v {
			return m.st.seats(c)
		}
	}
	return false
}

// seatedLabels names the seats that take turns, in the grid's own order, for a
// notice that has just changed who they are.
func (m *Model) seatedLabels() []string {
	var out []string
	for _, c := range m.st.Columns {
		if m.st.seats(c) {
			out = append(out, c.Label)
		}
	}
	if len(out) == 0 {
		return []string{"nobody"}
	}
	return out
}

// applyPosture sets the room's posture and rebuilds every column's claim about
// it.
//
// The rebuild is the whole function. Sandbox is computed once in stateWith from
// opts.Write, so a posture that moved without this loop would leave four badges
// describing the room the user just left — a column reading "write" beside a
// room that only talks, which is a displayed value no longer coming from what
// was measured (§4a.1). The badge is the seat's own claim about what it may do;
// it may not outlive the claim being true.
//
// Recomputed from postureClaim rather than patched in place so the badge and the
// invocation cannot drift: the same function answers at launch and here, reading
// the gate from the room's own Asking() and the guard from the hooks file that
// was actually written. It said "from opts.Auto" until `a` made the flag a seed
// rather than the answer; the code already read Asking(), so the comment was the
// only thing left describing the room council stopped shipping.
func (m *Model) applyPosture(write bool) {
	m.st.Write = write
	windows := runtime.GOOS == "windows"
	for i := range m.st.Columns {
		c := &m.st.Columns[i]
		c.Sandbox = postureClaim(c.Vendor, windows, write, m.st.Asking(), m.hooks.Wired())
	}
}

// plural is the one-word difference between "1 turn" and "2 turns".
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
