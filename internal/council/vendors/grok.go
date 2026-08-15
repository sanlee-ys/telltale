package vendors

import (
	"encoding/json"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// Grok drives the Grok CLI (`grok`) in its headless single-turn mode.
//
// Every flag, every field name and every status spelling below was read off the
// installed binary on 2026-08-09 — grok 1.0.0 (3cd0d0cbce) on Windows 11,
// signed in against grok.com with model grok-4.5 — by running it and reading
// real stdout. None of it is transcribed from `--help`, and the two places
// where the help text and the measurement disagree are recorded as
// measurements: see grokBaseArgs on --sandbox and --permission-mode.
//
// **Re-verified 2026-08-14 against grok 1.0.4 (d846eb93d9), default model
// grok-4.6.** The vendor had moved four patch versions with nothing here
// noticing, and the version-pinned wire fixture is what said so
// (testdata/wire/README.md). Every claim in this file was re-run rather than
// re-dated, and every one of them held: the frame shapes are identical, the
// argv still drives a briefed turn, --resume still resumes, both containment
// flags are still dead, and the slash hazard still eats a leading `/`. Each
// paragraph below carries the build it was last measured against, so a claim
// nobody re-ran cannot borrow a fresher date from a neighbour.
//
// This seat is the fifth column and the first that reports MONEY. Its `end`
// event carries a `total_cost_usd` the vendor computed itself, so unlike Codex
// and Antigravity — whose adapters leave CostUSD nil forever because they
// report token counts and a dollar figure would have to be derived — this one
// passes a number through because it was READ. That is the honesty rule
// working in the direction it rarely gets to: the constraint was never
// "council does not show cost", it was "council does not invent cost".
type Grok struct{}

// Registry lives in vendor.go and this adapter does not edit it, for the reason
// stated there. That leaves nothing to check the interface at compile time, so
// this does it here — the same guard codex.go carries.
var _ Vendor = Grok{}

func (Grok) ID() model.VendorID { return model.VendorGrok }

// grokBaseArgs is the shared invocation, minus the prompt.
//
// It is SHORT, and the length is the finding rather than an omission. Two flags
// whose names promise containment were probed and neither survived, so neither
// is passed and no badge claims either.
//
// **--sandbox is not passed, because it validates nothing.** `grok --sandbox
// bogus-profile-xyz -p "hi"` does not error, does not warn, and answers the
// prompt normally with exit 0. A flag that silently accepts a profile name that
// cannot exist is a flag whose effect this repo has no way to observe, and
// passing it would put a word in the badge backed by a value the CLI never
// looked at. This is the ADR-008 trap in its purest form — a flag's name is not
// evidence of its effect — and it is the second time the room has caught it by
// feeding a vendor a deliberately invalid value rather than a plausible one.
//
// Still unobservable at 1.0.4, re-probed 2026-08-14 and re-probed for FREE,
// which is worth copying. The same bogus profile was passed alongside a prompt
// the vendor is already known to eat (`--single=/context`, see grokSlashEaten):
// profile validation happens at startup, so a refusal would surface before any
// model turn, and a turn that is never billed answers the question at no cost.
// It answered the way 1.0.0 did — two `available_commands`, an `end` with no
// usage, no cost and no text, exit 0, empty stderr. macOS still diverges and
// fails closed on the same input; PARITY.md holds that row.
//
// **--permission-mode plan is not passed, because it was REFUTED.** Measured
// with the write actually landing, which is the strong form of the evidence:
//
//	grok --output-format streaming-json --permission-mode plan \
//	  -p "Create a file named probe-plan.txt containing the word WROTE..."
//
// produced a `write` tool call, a tool_call_update with status "completed", the
// reply "Created `probe-plan.txt` with the content `WROTE`", exit 0 — and the
// file was confirmed on disk. The control run without the flag wrote its file
// too, via `search_replace`. So `plan` here is not a layer that stops anything
// headless; the only difference observed between the arms was which write tool
// the model reached for. This is the Antigravity ledger a second time (ADR-008,
// seventeenth amendment) and it lands the same way: council does not ask for a
// restriction that has never been observed to restrict.
//
// Still refuted at 1.0.4, re-run 2026-08-14 with a FENCED prompt and the write
// landing again: `write` called, `status:"completed"`, exit 0, probe-plan.txt on
// disk holding WROTE. `--help` still advertises `plan` among six permission
// modes, which is the point — the help text has said the same thing across five
// builds while the flag has never once been observed to stop a write.
//
// Deliberately absent, and not for lack of noticing: --always-approve, and
// --permission-mode with `dontAsk` / `bypassPermissions`. The default headless
// mode already writes without asking — measured, above — so there is nothing an
// approve-everything flag would buy, and ADR-008's fifth and seventh amendments
// refuse that whole class on every seat that offers it. Adding one here would
// be taking on the badge cost of a bypass flag in exchange for nothing.
//
// The posture argument is therefore unused, exactly as it is on the Antigravity
// seat, and it is kept in the signature for the same reason: a seat that
// quietly stopped accepting the room's posture would be harder to notice than
// one that accepts it and has nothing to do with it.
// TestGrokAsksForNothingInEitherPosture pins that.
func grokBaseArgs(_ Posture) []string {
	return []string{
		// NDJSON of the agent's native ACP session updates. The other three
		// values `--help` offers are wrong for this seat and were not guessed
		// at: `plain` and `json` have no per-step structure to build a trace
		// from, and `streaming-messages-json` is the Anthropic Messages wire
		// format, which would make this adapter a second Claude parser reading
		// a different vendor's turn.
		"--output-format", "streaming-json",
	}
}

// grokPromptArgs puts the prompt on argv as ONE token, ATTACHED to its flag.
//
// argv rather than stdin, and unlike the Codex seat that is not a preference —
// it is the only channel offered. There is no `-` sentinel. It is safe here for
// the reason it is safe on the Antigravity seat: `grok` resolves to a native
// grok.exe rather than a .cmd shim, so runner's shell-shim refusal does not
// trip and no cmd.exe ever sees the brief. The residual is the ~32K Windows
// command-line limit, which a long multi-turn brief can reach; --prompt-file is
// the escape hatch if it ever does, and it is deliberately not built before it
// is needed.
//
// **`--single=<prompt>`, not `-p <prompt>`, and this cost a live room to find.**
// The separated form was what shipped, and it was verified against a probe
// prompt that began with a letter. Council's real first turn does not: a briefed
// room prepends `Brief.Apply`'s fence, so the prompt begins with `---`. clap does
// not take a hyphen-leading token as a flag's value unless the flag opts into
// `allow_hyphen_values`, and this one does not — so the brief was read as an
// unknown FLAG, the seat died at exit 2 before a single event, and the column
// reported failed in 0s on every briefed turn. Measured, with the parser's own
// words:
//
//	error: unexpected argument '--- operating context ---
//	  You are in a room...' found
//	  tip: to pass '...' as a value, use '-- ...'
//
// The attached form fixes it because there is no second token to misread:
// everything after the first `=` in `--single=…` is the value, hyphens and
// newlines included. Verified against the exact failing shape — a leading `---`
// and an embedded newline — on the first turn AND composed with `--resume`,
// where the resumed turn recalled a codeword only the first turn carried.
//
// The long spelling is deliberate: `-p=…` is not the same thing to clap, whose
// attached form for a SHORT flag is `-pVALUE`. `--single=` is the unambiguous
// one, and it is worth the six extra characters to never have to remember that.
//
// This is why the seat's live test now sends a brief-shaped prompt rather than a
// benign one (grok_live_test.go): a green test over a prompt whose first
// character is a letter proved the argv worked for a case the product never
// sends.
func grokPromptArgs(p Posture, prompt string) []string {
	return append(grokBaseArgs(p), "--single="+prompt)
}

func (g Grok) FirstTurn(prompt, workspace, binary string, p Posture) (runner.Spec, error) {
	return runner.Spec{
		Vendor: g.ID(),
		Binary: binary,
		Args:   grokPromptArgs(p, prompt),
		// StdinPrompt stays empty: grok does not read the prompt from stdin.
		Dir: workspace,
	}, nil
}

// NextTurn resumes grok's own session.
//
// VERIFIED as a real resume rather than a re-send, on 2026-08-09. A second turn
// against a captured session id echoed the SAME sessionId back, answered a
// question only the first turn's content could answer ("alpha beta"), made no
// tool call to re-read the file it was answering about, and reported
// input_tokens 497 against cache_read_input_tokens 19456 — the conversation was
// on the vendor's side, not re-sent from ours.
//
// --resume takes the id as its own value and precedes the prompt. A missing id
// is ErrNoResume rather than a fresh turn invented here: the room says out loud
// when a thread was lost, and an adapter that silently started a new
// conversation would take that sentence away from it.
//
// The session id is separated rather than attached, and that asymmetry with the
// prompt is deliberate rather than an oversight: a session id is a UUID and can
// never begin with a hyphen, so the hazard grokPromptArgs exists to route around
// does not reach this flag. Both spellings were measured working here; the plain
// one is kept because it is the one the vendor documents.
//
// RE-MEASURED at 1.0.4 on 2026-08-14, and this flag is the one that earned the
// re-run rather than inheriting it. 1.0.4's help spells it
// `-r, --resume [<SESSION_ID_OR_TITLE>]` — an OPTIONAL value that also matches
// session titles, where the pinned build took a required id. An optional-value
// flag is exactly the clap shape whose separated form can stop binding, which
// would silently turn every follow-up turn into a fresh conversation. It did
// not: the separated argv resumed, the `end` frame echoed the same sessionId
// back, the turn recalled the first turn's own word, and it reported
// input_tokens 454 against cache_read_input_tokens 21504 — the conversation was
// on the vendor's side. The 1.0.0 evidence shape, reproduced.
func (g Grok) NextTurn(prompt, workspace, binary, sessionID string, p Posture) (runner.Spec, error) {
	if sessionID == "" {
		return runner.Spec{}, ErrNoResume
	}
	return runner.Spec{
		Vendor: g.ID(),
		Binary: binary,
		Args: append(append(grokBaseArgs(p), "--resume", sessionID),
			"--single="+prompt),
		Dir: workspace,
	}, nil
}

// grokLine is the subset of grok's streaming-json schema council models.
//
// The shape is flat — a `type` discriminator with the payload as sibling keys —
// which makes it the third distinct schema in this package and no relation to
// either neighbour. A whole successful turn, captured verbatim and trimmed only
// where noted:
//
//	{"type":"available_commands","tools":[...],"commands":[...]}
//	{"type":"thought","data":"The"}
//	{"type":"text","data":"I'll"}
//	{"type":"tool_call","toolCallId":"call-12ae…-0","title":"read_file",
//	 "kind":"read","status":"pending","toolName":"read_file",
//	 "rawInput":{"target_file":"notes.txt"},"content":[],"locations":[]}
//	{"type":"tool_call_update","toolCallId":"call-12ae…-0","status":null,…}
//	{"type":"tool_call_update","toolCallId":"call-12ae…-0","status":"completed",
//	 "content":[{"type":"content","content":{"type":"text","text":"1→alpha\nbeta\n"}}],…}
//	{"type":"usage","usage":{…},"signature":"…"}
//	{"type":"end","stopReason":"end_turn","sessionId":"019fe742-…",
//	 "usage":{…},"num_turns":2,"total_cost_usd":0.0407676,"modelUsage":{…}}
//
// One field is deliberately NOT modelled: `signature`, on the `usage` event. It
// is an opaque vendor blob, this adapter has nothing to do with it, and a field
// with no destination is the cheapest possible thing to leave out — the same
// allowlist-by-struct discipline internal/cursorhook uses to keep a payload's
// unwanted halves from reaching anything that can display them.
type grokLine struct {
	Type string `json:"type"`

	// Data carries the delta on both `text` and `thought`. One key for two
	// event types is the vendor's own design, not a flattening done here.
	Data string `json:"data"`

	// ToolCallID correlates a call with its updates. Captured stable across
	// both reports of the same call, which is what lets the trace resolve an
	// entry in place instead of appending a second line under it.
	ToolCallID string `json:"toolCallId"`
	ToolName   string `json:"toolName"`
	// Title is grok's own display name for the call. On every captured line it
	// equalled ToolName, so it is the FALLBACK rather than the preference — a
	// second field that agreed everywhere is not evidence about which one wins
	// when they disagree.
	Title string `json:"title"`
	// Status is a POINTER because grok spells three different things here and
	// two of them are not strings. `null` is a real, captured value: the FIRST
	// tool_call_update of every call carries `"status":null` and exists only to
	// report `locations`. A plain string would flatten that into "", and the
	// obvious next step — treating "" as "no longer pending" — would resolve
	// every tool call the instant it started, marking a running command as
	// finished with an unknown outcome. Pointer-ness is what keeps "grok said
	// nothing about the outcome yet" separate from "grok reported an outcome",
	// which is design.md §4a.1's zero-vs-absent rule wearing a tool call.
	Status *string `json:"status"`
	// RawInput is the call's arguments, with vendor-specific key names.
	// Captured: {"target_file":"notes.txt"} on read_file. RawMessage per value
	// for the reason agy's parameters map is: a parameter is not necessarily a
	// string, and a map[string]string would fail the whole line's unmarshal and
	// lose the activity entry over one odd field.
	RawInput map[string]json.RawMessage `json:"rawInput"`
	// Content is where a finished call's own words live. RawOutput sits beside
	// it carrying the same text inside a much larger tool-specific object — a
	// whole file's contents on a read — and is deliberately NOT modelled: the
	// trace needs one clipped line, and parsing a blob to find it would be
	// this adapter volunteering to hold a vendor's entire output in memory to
	// display forty characters of it.
	Content []struct {
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"content"`

	// end
	SessionID  string `json:"sessionId"`
	StopReason string `json:"stopReason"`
	// TotalCostUSD is the vendor's OWN figure and the reason this seat can show
	// money at all. A pointer, so a turn that reported no cost stays
	// distinguishable from one that reported zero — and that is not a
	// hypothetical here: the slash-command turn described on grokSlashEaten
	// ends with `{"type":"end","stopReason":"end_turn","sessionId":…}` and no
	// usage and no cost keys at all. Absent, not zero, measured.
	TotalCostUSD *float64 `json:"total_cost_usd"`
}

// grokSlashEaten records a measured hazard this adapter cannot fix, so that the
// next person to see a blank Grok column finds the answer here instead of
// re-deriving it.
//
// A brief whose first non-space character is `/` is consumed by grok's own
// slash-command parser and NEVER reaches the model. The turn is not an error:
// stdout carries `available_commands` and an `end` with no usage, no cost and
// no text, and the process exits 0. On screen that is a column that finishes
// instantly with nothing in it.
//
// Still eaten at 1.0.4 (2026-08-14): `--single=/context` produced two
// `available_commands` frames and an `end` with no usage, no cost and no text,
// at exit 0. That run is also the standing evidence for TotalCostUSD being a
// pointer — the absent-cost shape is measured on the current build, not
// inherited from the pinned one.
//
// Three channels were tried on 2026-08-09 and all three were eaten:
//
//	-p "/context"                              → no text, no usage
//	--verbatim -p "/context"                   → no text, no usage
//	--prompt-json '[{"type":"text","text":…}]' → no text, no usage
//
// The third had a CONTROL, which is what makes this a finding rather than a
// guess about a flag that might not work: the same --prompt-json invocation
// with a non-slash prompt answered normally ("CONTROLOK"). So the channel
// works and the slash is eaten regardless of channel. `--verbatim`, whose help
// text reads "Send the prompt exactly as given", does not send it exactly as
// given.
//
// The room mostly protects this seat already, and by accident rather than by
// design: council refuses to dispatch any draft whose first character is a
// slash (roomcmd.go, §9.31) — nothing spawns and nothing is billed. What does
// NOT hold is that refusal's documented escape hatch. A user who genuinely
// means a leading slash types one leading SPACE, and the space survives the
// composer, the parse and the dispatch untouched — and then grok trims it and
// eats the slash anyway. Measured: `-p " /context is a path I am asking
// about. Reply with exactly: SPACEOK"` produced no text and no usage.
//
// Nothing is rewritten here to compensate. Editing a user's brief on the way to
// one vendor would make the four columns answer different questions while
// claiming to answer one, which is a worse failure than a blank column — the
// room's whole premise is that the seats got the SAME brief. Left as a
// documented sharp edge, in the file whose column shows the symptom.
const grokSlashEaten = "a brief beginning with / is consumed by grok's slash-command parser"

// grokWhat names one call for the trace, in the grammar the other adapters use:
// the tool's real name, then ": " and the one argument that identifies the call.
func grokWhat(gl grokLine) string {
	name := gl.ToolName
	if name == "" {
		name = gl.Title
	}
	if arg := grokToolArg(gl.RawInput); arg != "" {
		return name + ": " + arg
	}
	return name
}

// grokToolArg picks the one argument worth showing in a narrow column, by the
// same four rules agyToolArg documents and defends: strings only, the single
// string wins, ties break on the LOWEST key name by byte order, none leaves the
// bare tool name.
//
// Shared reasoning, deliberately not shared code. The two vendors' argument
// objects have nothing in common — grok sends snake_case (`target_file`) where
// agy sends PascalCase (`TargetFile`) — so a common helper would be two
// unrelated schemas held together by the coincidence that this repo wants the
// same DISPLAY rule for both. What matters is the property rule 3 buys, and it
// is bought here independently: the answer never depends on Go's randomised map
// iteration order, so a rendered line and a golden cannot flicker between runs.
// TestGrokToolArgIsDeterministic pins it.
func grokToolArg(raw map[string]json.RawMessage) string {
	best, bestKey, found := "", "", false
	for k, v := range raw {
		var s string
		if json.Unmarshal(v, &s) != nil {
			continue
		}
		if s == "" {
			continue
		}
		if !found || k < bestKey {
			best, bestKey, found = s, k, true
		}
	}
	return clipArg(best)
}

// grokOutcome maps a tool_call_update's status to what is actually known.
//
// BOTH spellings are measured, which is what lets this map success as well as
// failure — the asymmetry codex.go has to live with (where "completed" was
// never captured and a finished item therefore renders Unknown) does not apply
// here. Captured 2026-08-09:
//
//	"completed" — the read that returned "1→alpha\nbeta\n"
//	"failed"    — a read of a path that does not exist
//	null        — the first update of every call, carrying only locations
//
// nil is NOT an outcome and returns pending, for the reason grokLine.Status is
// a pointer at all. Any other string is Unknown rather than mapped: an
// unrecognised status is not evidence, and inventing an outcome for it is how a
// trace starts lying quietly.
func grokOutcome(status *string) runner.ActStatus {
	if status == nil {
		return runner.ActPending
	}
	switch *status {
	case "completed":
		return runner.ActOK
	case "failed":
		return runner.ActFailed
	}
	return runner.ActUnknown
}

// grokDetail is the vendor's own first line about a call, never a sentence
// composed here (§9.6a).
//
// It reads ONE shape of content element — `{"type":"content","content":{"type":
// "text","text":…}}`, which is what a read returns — and the 1.0.4 re-measure
// found a second one it does not read. A WRITE call's element is a diff instead:
// `{"type":"diff","path":…,"oldText":"","newText":"WROTE\n"}`, with no nested
// `content` object at all, so this returns "" for it.
//
// Recorded rather than fixed, and the distinction matters: no write call was
// captured at 1.0.0, so this is a shape nobody had looked at, NOT a shape that
// changed under us. Two reasons it stays as it is. A detail is only rendered on
// a FAILED outcome, and a write that failed carries an error rather than a
// completed diff. And composing a sentence out of oldText/newText would be this
// package writing the vendor's line for it, which §9.6a forbids by name. If a
// failing write ever turns out to carry its reason in a diff element, that is a
// measurement, and it is the thing to add here.
func grokDetail(gl grokLine) string {
	for _, c := range gl.Content {
		if c.Content.Text != "" {
			return clipArg(firstLine(c.Content.Text))
		}
	}
	return ""
}

// ParseEvent maps grok's observed schema. Unknown lines are dropped rather than
// failing the turn.
//
// The column streams properly, and this is the first seat in the room where
// that word is not a stretch. Measured deltas: "I'll", " read", " `", "notes",
// ".txt", "`,", " then" — genuinely token-level, finer than the ~80-character
// chunks §9.7 flagged as overstating "tokens" on the Claude seat and finer than
// the ~95-character ACP chunks on the Cursor seat. So this column is
// GranTokens on stronger evidence than either seat already carrying the word.
func (Grok) ParseEvent(line []byte) (runner.Event, bool) {
	if len(line) == 0 || line[0] != '{' {
		return runner.Event{}, false
	}
	var gl grokLine
	if err := json.Unmarshal(line, &gl); err != nil {
		return runner.Event{}, false
	}

	switch gl.Type {
	case "text":
		// The vendor's speech. Deltas concatenate naturally — no separator is
		// added, because the captured stream carries its own spaces inside the
		// deltas (" read", " then") and appending anything would double them.
		if gl.Data != "" {
			return runner.Event{Kind: runner.KindText, Text: gl.Data}, true
		}
	case "thought":
		// DROPPED, and this is the one judgement call in the file, so the
		// reasoning is here rather than implied.
		//
		// `thought` is the model reasoning, not the model answering: on the
		// first capture it was 46 lines against 14 of `text`, and it opened
		// "The user wants me to read notes.txt". Routing it to the column would
		// put the vendor's private deliberation where its answer goes, and the
		// room exists to compare ANSWERS. It is the same line codex.go draws
		// when it excludes `reasoning` items from its whitelist.
		//
		// The cost is stated rather than hidden: a turn that thinks for a long
		// time before speaking shows an empty column while it does. That is
		// honest — nothing has been said yet — and it is strictly better than
		// the two seats whose columns are empty for the WHOLE turn, because
		// once this one starts speaking it streams token by token.
		return runner.Event{}, false
	case "available_commands":
		// Plumbing. It carries grok's tool and slash-command inventory, it
		// arrived FOUR times in a single turn, and it reports nothing the
		// vendor did. Suppressed on agyPlumbing's rule and its asymmetry:
		// hiding a vendor's ACTIONS would be a false gauge, hiding its
		// inventory is noise reduction.
		return runner.Event{}, false
	case "tool_call":
		// The call, announced. Captured with status "pending", which
		// grokOutcome maps to Unknown rather than pending — so the outcome is
		// set explicitly here instead of being read off the line. The
		// announcement IS the pending state; taking grok's word for it would
		// make the entry's mark depend on a string whose only captured value
		// happens to agree.
		return runner.Event{
			Kind: runner.KindActivity,
			Acts: []runner.ActCall{{
				ID:      gl.ToolCallID,
				Text:    grokWhat(gl),
				Outcome: runner.ActPending,
			}},
		}, true
	case "tool_call_update":
		// The resolution — or the null-status interim report, which resolves
		// nothing and must not. grokOutcome returns pending for it, which
		// re-announces the entry in place rather than closing it.
		//
		// Text is carried again on the update even though tool_call already
		// named the call, for codex.go's reason: if a future build ever stops
		// emitting the announcement, the resolution alone still says what ran
		// instead of resolving an entry that does not exist. The update carries
		// no rawInput, so grokWhat falls back to the bare tool name — and that
		// too is only reachable in that future, since today's stream always
		// announces first.
		act := runner.ActCall{
			ID:      gl.ToolCallID,
			Text:    grokWhat(gl),
			Outcome: grokOutcome(gl.Status),
		}
		if act.Outcome == runner.ActFailed {
			act.Detail = grokDetail(gl)
		}
		return runner.Event{Kind: runner.KindActivity, Acts: []runner.ActCall{act}}, true
	case "end":
		// The end-of-turn line, carrying the two things the room needs after a
		// turn: the thread to resume on, and what it cost.
		//
		// The session id arrives HERE and nowhere earlier — there is no
		// thread.started analogue in this stream. The consequence is worth
		// naming because it is a real difference from the Codex seat: a turn
		// that dies before `end` leaves no id, so the next turn cannot resume
		// and the room says the thread was lost rather than pretending
		// otherwise. `--session-id` would let council MINT the id up front and
		// close that gap; it is not done here because it would put UUID
		// generation into a package whose whole job is building specs out of
		// strings, and because a turn that died is a turn worth restarting
		// anyway.
		//
		// CostUSD passes straight through as the pointer it was parsed into.
		// Absent stays absent — see TotalCostUSD's comment for the captured
		// turn that has no cost keys at all.
		//
		// stopReason is read and deliberately NOT mapped to an outcome. Every
		// captured turn, successful and empty alike, said "end_turn"; a failing
		// turn produced no stdout whatsoever. So this adapter has never seen
		// this field distinguish anything, and a failure branch keyed off a
		// value nobody has observed would be the diagnosis invented. Failures
		// arrive the way codex's do — exit code plus stderr, which runner
		// already turns into a KindError. Verified: a bad --resume id exits 1
		// with an empty stdout and "Error: Failed to restore session from
		// remote: … 404 Not Found" on stderr.
		return runner.Event{
			Kind:      runner.KindMeta,
			SessionID: gl.SessionID,
			CostUSD:   gl.TotalCostUSD,
		}, true
	case "usage":
		// Token counts and an opaque signature, mid-turn, twice per turn. The
		// numbers are real but there is nowhere honest to put them: the room's
		// cost line is dollars, and the `end` event already carries the
		// vendor's own dollar figure for the whole turn. Adding token counts
		// beside a cost derived from a different source is how two true numbers
		// start disagreeing on one line.
		return runner.Event{}, false
	}
	return runner.Event{}, false
}
