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
// The restraint about vocabulary survives and its FALL-THROUGH does not (§9.31).
// Only a draft that IS a command is executed — that half is untouched, and it is
// what still keeps "/read the design doc" from quietly running a setting. What
// changed is what happens to the rest. A draft whose first character is a slash
// used to dispatch as typed, so a mistyped verb was billed to every seated
// vendor as a brief: `/unseat codex` was typed into a live room before /unseat
// existed, and three seats spent a turn discussing the string until the user
// cancelled. A leading slash is almost never prose, and refusing costs nothing —
// nothing spawns, nothing is billed, and the draft stays in the composer.
//
// The escape hatch is a leading SPACE, and addressesRoom is where it lives.

// addressesRoom reports whether a draft is aimed at the room rather than at the
// vendors: a slash in COLUMN ONE.
//
// The column-one test is the whole escape hatch, and it is what lets the refusal
// below afford to be strict. A brief that genuinely begins with a slash — a
// POSIX path, a regex — is sent by typing one space first, because
// sanitizeKeepingSpace deliberately does not trim ("trimming would make the
// string on screen disagree with the string about to be dispatched") so the
// leading space survives the composer, the parse and the dispatch untouched.
// One keystroke, and it is the cheapest honest escape available: nothing else
// the composer can hold distinguishes "I meant this as text" without inventing a
// second grammar to learn.
func addressesRoom(draft string) bool { return strings.HasPrefix(draft, "/") }

// roomVerb is one word this file takes out of the conversation.
type roomVerb struct {
	verb string
	// bare marks a verb recognised ONLY as the whole draft. See
	// parseBareCommand: /read and /write take no argument and are both words a
	// person addresses a room with, so "/read the design doc" must not run a
	// setting.
	bare bool
	// run handles the command. NIL for /flow, which is a room word parsed and
	// dispatched by dispatch.go against the same draft rather than intercepted
	// here — it still has to sit in this table, because a word missing from the
	// table is a word the refusal would reject.
	run func(m *Model, arg string) bool
}

// roomVerbs is the room's whole vocabulary, and the ONE place that list exists.
//
// The refusal reads it and TestTheRefusalListsTheLiveCommandTable walks it, so a
// command added here cannot go missing from the sentence that teaches it. A
// hand-kept second copy inside a notice string would be the list that goes stale
// on the next command — and a refusal is the surface least likely to be re-read
// after the thing it refuses becomes possible (§9.17's own finding, which is how
// the /flow write-hop notice went on naming a flag for two releases).
//
// Alphabetical, which is also the order the refusal prints: a reader scanning
// for the word they meant to type finds it by spelling rather than by knowing
// which feature shipped first.
func roomVerbs() []roomVerb {
	return []roomVerb{
		// /arena and /flow carry no run: both are DISPATCHES, parsed in
		// dispatch.go against this same draft, and are recognised here only so
		// the refusal does not reject the room's own words.
		{verb: "/arena"},
		{verb: "/cd", run: (*Model).cdCommand},
		{verb: "/flow"},
		{verb: "/read", bare: true, run: func(m *Model, _ string) bool { return m.postureCommand(false) }},
		{verb: "/seat", run: (*Model).seatCommand},
		{verb: "/trace", run: (*Model).traceCommand},
		{verb: "/unseat", run: (*Model).unseatCommand},
		{verb: "/write", bare: true, run: func(m *Model, _ string) bool { return m.postureCommand(true) }},
	}
}

// match applies this verb's own parse rule to a draft.
func (rc roomVerb) match(draft string) (arg string, ok bool) {
	if rc.bare {
		return "", parseBareCommand(draft, rc.verb)
	}
	return parseCommand(draft, rc.verb)
}

// roomWords is the vocabulary as the refusal prints it.
func roomWords() []string {
	vs := roomVerbs()
	out := make([]string, 0, len(vs))
	for _, rc := range vs {
		out = append(out, rc.verb)
	}
	return out
}

// unknownVerbEcho caps the word a refusal quotes back.
//
// The notice is one line of the mode bar and it truncates from the RIGHT, so an
// uncapped echo — a pasted 200-character path is one word — would push the
// remedy and the vocabulary off the end and leave a refusal that names only the
// mistake. Capping the part the user already knows is the cheaper loss.
const unknownVerbEcho = 20

// firstWord is the word a draft opens with, capped for the notice.
func firstWord(draft, ell string) string {
	w := draft
	if i := strings.IndexAny(w, " \t"); i >= 0 {
		w = w[:i]
	}
	return truncate(w, unknownVerbEcho, ell)
}

// refuseUnknownCommand answers a draft addressed to the room that names nothing
// the room knows. NOTHING IS DISPATCHED: no process starts, no quota moves, and
// the draft stays in the composer to be edited — the same handing-back /cd does
// for a mistyped path, for the same reason.
//
// THE ORDER OF THE THREE CLAUSES IS THE DESIGN. What failed, then how to send it
// anyway, then the vocabulary. This line truncates from the right at narrow
// widths, so the clause most likely to be lost has to be the one a reader can
// get elsewhere: `?` lists the room controls, and nothing else on screen teaches
// the space. A refusal whose remedy is undiscoverable is §9.17's defect wearing
// a different hat.
func (m *Model) refuseUnknownCommand() bool {
	// "sends", not "dispatches", and a comma, not an em dash — six characters
	// bought back when /arena joined the vocabulary, because this line has a
	// HARD budget: TestTheRefusalFitsTheRoomItIsShownIn fails any wording the
	// room's own width clips. The remedy clause outranks elegance; a refusal
	// that truncates its vocabulary teaches a partial alphabet.
	m.st.Notice = "no room command " + firstWord(m.st.Draft, m.glyphs.Ellipsis) +
		", a leading space sends it · " + strings.Join(roomWords(), " ")
	return true
}

// isFlowCommand reports whether a draft IS the /flow command.
//
// Through the same vocabulary rule every other room word obeys, rather than the
// TrimSpace'd prefix test dispatch.go used to run. That one took any draft whose
// first non-space characters were "/flow", which made "/flowchart the auth path"
// an orchestration — and, worse, swallowed a path escaped with a leading space
// (" /flow/gate.log"), which would have made the escape hatch a lie for exactly
// one prefix. An escape hatch with an exception nobody can see is not one.
func isFlowCommand(draft string) bool {
	if !addressesRoom(draft) {
		return false
	}
	_, ok := parseCommand(draft, "/flow")
	return ok
}

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

// parseBareCommand recognises ONLY the argument-less form of a verb.
//
// Separate from parseCommand, and the difference is the vocabulary rule this
// file opens with rather than a stylistic split. `/cd` and `/trace` take an
// argument, so "/cd " and "/trace " are already unmistakably commands — nobody
// types them as prose. `/read` and `/write` take none, and both are words a
// person addresses a ROOM with: "/read the design doc before answering",
// "/write a test for this" are ordinary briefs, and intercepting them would
// steal two live verbs out of the conversation to run a setting the user did
// not ask for. So the bare draft is the command and nothing else runs one,
// which is the strictest reading of "only a draft that IS a command is
// intercepted".
//
// §9.31 changed what happens to the REST of that set and not this rule. A
// "/read the design doc" is still not the posture command — the failure this
// function exists to prevent, a brief vanishing into a setting, is unreachable —
// but it is no longer dispatched either. It is refused, with the space escape
// named, which is the outcome that neither swallows the turn nor bills it.
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
// Every roster-changing command belongs in here, under roomCommand's save — and
// the table below is what makes that true by construction rather than by a
// reviewer noticing: a command reachable from roomVerbs is reachable from here.
func (m *Model) runRoomCommand() bool {
	if !addressesRoom(m.st.Draft) {
		return false
	}
	for _, rc := range roomVerbs() {
		arg, ok := rc.match(m.st.Draft)
		if !ok {
			continue
		}
		if rc.run == nil {
			// /flow, which dispatch.go parses against this same draft. Recognised
			// here only so the refusal below does not reject the room's own word.
			return false
		}
		return rc.run(m, arg)
	}
	return m.refuseUnknownCommand()
}

// cdCommand moves the room's workspace between turns.
func (m *Model) cdCommand(arg string) bool {
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
			" — /seat <list> narrows, /unseat <list> subtracts, /seat all puts everyone back"
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

	m.applySeats(want)
	return true
}

// unseatCommand is /seat's subtractive form: it names who LEAVES.
//
// One vocabulary, because it is literally parseSeatList — same aliases, same
// `@` tolerance, same trailing punctuation, same dedupe. A second list parser is
// how "/seat agy" would work and "/unseat agy" would not, which is the
// two-vocabularies defect mentions.go already refuses for `--vendor`.
//
// WHY IT IS WORTH A WORD OF ITS OWN. The correction a user actually reaches for
// mid-session is "not that seat" — one vendor is answering badly, or expensively,
// and the other three are fine. Spelling that with `/seat` means retyping the
// complement, which is arithmetic done at the keyboard on the one line where
// getting it wrong quietly reseats the room around the seats you did not mean.
// A subtraction reads straight off the screen. It is the same argument `-@`
// makes against making the user compute the complement of a mention, one
// control up.
//
// It kills nothing, for seatCommand's reasons: an unseated seat keeps its
// thread, its process and every id that would resume it, so `/seat all` puts it
// back mid-conversation with no resume to fail. And it refuses mid-turn, because
// the roster is dispatch state — the grid for a turn in flight was decided at
// dispatch, and reseating under it would redraw the room around columns that are
// mid-answer.
func (m *Model) unseatCommand(arg string) bool {
	if m.turn != nil {
		m.st.Notice = "a turn is in flight — /unseat changes the room between turns"
		return true
	}

	if arg == "" {
		// Answers the question it half-asks, the way bare /cd, /trace and /seat
		// do. The same sentence /seat reports, because it is the same fact.
		m.st.Notice = "seated: " + strings.Join(m.seatedLabels(), " ") +
			" — /unseat <list> subtracts, /seat all puts everyone back"
		m.setDraft("")
		return true
	}

	if allAliases[strings.ToLower(strings.TrimSpace(arg))] {
		// "/unseat all" is a sentence someone will type, and it names an empty
		// room. Answered here rather than left to parseSeatList, whose honest
		// report would be "no seat called all" — a spelling complaint about a
		// word the room understands perfectly well.
		m.st.Notice = "/unseat all would empty the room — it needs at least one seat"
		return true
	}

	drop, unknown := parseSeatList(arg)
	if len(unknown) > 0 {
		m.st.Notice = "no seat called " + strings.Join(unknown, " or ") +
			" — /unseat takes claude, codex, agy or cursor"
		return true
	}
	if len(drop) == 0 {
		m.st.Notice = "/unseat needs a seat to remove — /unseat <list>, or /seat <list> to name who stays"
		return true
	}

	// WHAT COUNTS AS BEING IN THE ROOM IS WHAT THE ROOM SHOWS, not what it can
	// drive, and the distinction is load-bearing rather than pedantic. `/seat
	// cursor` FORCES an uninstalled seat on screen — "a user who asked for it is
	// owed the card explaining why it is not there" — so that seat is in the room
	// in every sense a subtraction cares about, and a /unseat that could not
	// remove it would leave the one card a user most wants gone stuck there.
	dropped := map[model.VendorID]bool{}
	var absent []string
	for _, v := range drop {
		dropped[v] = true
		if !m.showsVendor(v) {
			absent = append(absent, string(v))
		}
	}
	if len(absent) > 0 {
		// Naming a seat that is already out changes nothing, and saying so is
		// /seat's typo argument rather than pedantry: a command that quietly did
		// less than it was asked to is discovered several turns later, as a seat
		// still answering that the user believes they removed.
		m.st.Notice = "not in the room: " + strings.Join(absent, " ") +
			" — /seat all puts everyone back"
		return true
	}

	var keep []model.VendorID
	before, after := 0, 0
	for _, c := range m.st.Columns {
		if !m.st.shows(c) {
			continue
		}
		if c.Avail == AvailInstalled {
			before++
		}
		if dropped[c.Vendor] {
			continue
		}
		keep = append(keep, c.Vendor)
		if c.Avail == AvailInstalled {
			after++
		}
	}
	if len(keep) == 0 {
		// The last seat. /seat's own refusal, in /seat's words, because it is the
		// same room being refused — reached by subtraction instead of by naming
		// nobody.
		m.st.Notice = "that would empty the room — it needs at least one seat"
		return true
	}
	if before > 0 && after == 0 {
		// Every seat that could actually answer, gone, leaving only cards that
		// explain why the rest cannot. The guard is CONDITIONAL on the room having
		// had one, and that is the honest half: on a machine where nothing is
		// installed the room was already unable to answer before this was typed,
		// and refusing there would blame /unseat for a state it did not cause.
		m.st.Notice = "that would leave no seat that can answer — the room needs at least one seat it can drive"
		return true
	}

	m.applySeats(keep)
	return true
}

// applySeats installs a roster and says what the room now looks like.
//
// Shared by /seat and /unseat so the two cannot describe one room in two voices,
// and so the default-route warning cannot be taught to one of them and not the
// other — the room would then have a way to unseat claude that says nothing
// about it.
func (m *Model) applySeats(want []model.VendorID) {
	m.st.Seats = Seats{Only: want}
	m.setDraft("")
	m.rehomeFocus()

	notice := "seated: " + strings.Join(m.seatedLabels(), " ") + " — the rest keep their threads, /seat all brings them back"
	// The default route is claude, so a room that unseats claude answers nothing
	// at all until every brief is @mentioned. Dispatch would say so per turn
	// (seatedIn == 0); saying it once, here, is the difference between a rule the
	// user learns now and one they discover on their next enter.
	if !m.seatsVendor(model.VendorClaude) {
		notice += ". Unaddressed briefs go to claude, who is not seated — @mention a seat"
	}
	m.st.Notice = notice
}

// rehomeFocus puts the keys back on a column the room actually draws.
//
// `stateWith` does this once at launch — "focus lands on a column that is
// actually drawn" — and until now nothing did it again, so unseating the FOCUSED
// seat left `State.Focus` pointing at a column the grid no longer draws. The
// focus mark vanished from the room and `f`, the scroll keys and `y` went on
// addressing the hidden column: keys that still worked, over a transcript
// nobody could see, which is worse than keys that stop. Same rule as launch and
// the same first-visible answer, applied wherever the roster moves — /seat can
// unseat the focused column exactly as easily as /unseat can.
func (m *Model) rehomeFocus() {
	vis := m.st.VisibleColumns()
	for _, i := range vis {
		if i == m.st.Focus {
			return
		}
	}
	if len(vis) > 0 {
		m.st.Focus = vis[0]
	}
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

// showsVendor reports whether this vendor is IN the room — drawn — which is a
// wider set than seatsVendor's: a seat named to `--vendor` or `/seat` is forced
// on screen even when it is not installed. /unseat subtracts from this set,
// because a card the user asked for is a card they can ask to be rid of.
func (m *Model) showsVendor(v model.VendorID) bool {
	for _, c := range m.st.Columns {
		if c.Vendor == v {
			return m.st.shows(c)
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
