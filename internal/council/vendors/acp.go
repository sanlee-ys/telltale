package vendors

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// The ACP wire, as it was actually driven.
//
// EVERYTHING in this file is MEASURED against cursor-agent **2026.08.04-aaa8809**
// on Windows 11, over thirteen live arms on 2026-08-08. design.md §9.36 carries
// the trial counts and the timings; this file carries the shapes, so that a
// reader changing a struct field can see the line it was copied from without
// leaving the code.
//
// The subcommand is `acp`, registered hidden — `Ce.command("acp",{hidden:!0})`
// — and absent from `--help`, which is why §9.33 drove it rather than believed
// it. It speaks JSON-RPC 2.0, one object per line, on stdin/stdout.
//
// The handshake, verbatim (abridged only where a list of thirty models sat):
//
//	>> {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{…}}}
//	<< {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,…},"authMethods":[…]}}
//	>> {"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"…","mcpServers":[]}}
//	<< {"jsonrpc":"2.0","id":2,"result":{"sessionId":"2abf0328-…","modes":{"currentModeId":"agent","availableModes":[…]},"configOptions":[…]}}
//
// A turn:
//
//	>> {"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"…","prompt":[{"type":"text","text":"…"}]}}
//	<< {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"…","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Second line:"}}}}
//	<< {"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn"}}
//
// Three absences decide as much of this adapter as the presences do, and each
// one is a thing print mode HAD:
//
//   - **No cost, and no token usage anywhere.** Print mode's `result` carried a
//     `usage` block; the ACP turn resolves with `{"stopReason":…}` and nothing
//     else. CostUSD was already nil for this vendor forever (no monetary figure
//     exists in the bundle); now the token counts are gone too.
//   - **No final whole reply, so no safety net.** Print mode's `result` carried
//     the entire answer, and §9.6c leaned on it explicitly as the fallback if the
//     dedup fields ever changed: "the failure mode is a column that fills at the
//     end, never one that is wrong". ACP has no such line. If chunk parsing ever
//     breaks here the column is EMPTY, not late. Stated rather than mitigated,
//     because the mitigation would have to be invented.
//   - **No whole-message repeat, and no `model_call_id`.** §9.33 raised this as a
//     two-turn hypothesis and flagged §9.6c's standing warning against
//     generalising from a thin capture. It is now measured across the arms §9.36
//     lists — including turns with four tool calls and several model segments —
//     and there is no repeat and no such field anywhere in ACP traffic. So the
//     dedup rule is not carried over; it was a fact about a surface this seat no
//     longer uses.
//
// SINCE 2026-09-02 THIS FILE IS THE SHARED ACP CLIENT, and the rename from
// cursoracp.go records that. Two seats drive it: the Cursor seat (cursor.go),
// against which every measurement above was taken, and the Grok seat
// (grokagent.go), which speaks the same protocol from a server nobody here has
// driven. What differs between them is held in acpDialect and nothing else —
// the mode a read posture asks for, how a permission answer is spelled, and
// whether `session/load` may be sent without the server advertising it. The
// state machine, the replay guard, the terminal handshake state and every
// refusal are one implementation, so a fix on one seat is a fix on the other
// and a divergence between them has exactly one place to hide. Everything a
// dialect field says about grok is UNMEASURED and labelled so at its
// definition; design.md §9.54 lists the runs that would change that.

// acpProtocol is one ACP conversation, for the life of one process.
//
// Stateful because the protocol is: a `session/prompt` cannot be built before
// `session/new` has answered, and the answer arrives on a goroutine that is not
// the one taking turns. Hence the mutex — Inbound runs on the runner's stdout
// pump, Turn/Interrupt/Decide on the room's update loop.
type acpProtocol struct {
	workspace string
	// resumeID is a thread from a saved room, or empty for a new conversation.
	resumeID string
	posture  Posture
	// dialect is the one vendor-shaped thing about this conversation. See
	// acpDialect for what it may vary and why nothing else is allowed to.
	dialect acpDialect

	mu     sync.Mutex
	nextID int
	// what each outstanding id of OURS was asked for, so a response can be
	// routed without a switch on a method name we no longer have.
	awaiting map[int]string
	// sessionID is the vendor's own id for this conversation, and the key the
	// saved room stores. Empty until session/new or session/load answers.
	sessionID string
	// queued holds turns taken before the handshake finished.
	//
	// A slice rather than a single slot because nothing structurally prevents a
	// second brief arriving during a slow handshake, and dropping one silently is
	// the failure this whole product refuses. In practice the room dispatches one
	// turn at a time per seat.
	queued []string
	// ready is the handshake being COMPLETE, which is later than the session
	// existing and is the difference between two windows a turn can arrive in.
	//
	// Keying the queue on sessionID instead was a real bug and worth the field:
	// a read-posture seat sets its session id and then spends a round trip on
	// session/set_mode, and a turn arriving in THAT window would have gone out
	// immediately — under the server's default `agent` mode, while the column's
	// badge said `ro:requested`. Exactly the invisible race the set_mode
	// sequencing exists to close, arriving through the door left open beside it.
	// It would also have overtaken a turn already queued.
	ready bool
	// dead is the handshake having failed, and it is a TERMINAL state.
	//
	// Without it the seat wedges the whole room, which is worse than it sounds:
	// an ACP server that refuses `initialize` does not exit, so the room's
	// stale-exit guard correctly sees a live process and keeps handing it briefs.
	// Each one would be queued against a handshake that has already finished
	// failing, nothing would ever answer, and the turn would never end — so no
	// further brief could be dispatched and the room could not even be quit. The
	// most likely trigger is an unauthenticated CLI, i.e. somebody's first run.
	dead bool
	// replaying suppresses the history session/load streams back before it
	// answers. See Inbound.
	replaying bool
	// perms maps the key the room decides by to the RAW JSON id the vendor wants
	// echoed, and to the two option ids that answer it. Raw, and in its own
	// namespace, because the vendor numbers its requests from 0 independently of
	// ours — "id 0" inbound is a question and "id 0" outbound is an answer to
	// one — and because JSON-RPC permits an id to be a string as easily as a
	// number.
	perms   map[string]acpPending
	permSeq int
	// loadSession is what the server's initialize response advertised under
	// agentCapabilities.loadSession, and it is only consulted by a dialect that
	// asks for it. The ACP schema (agentclientprotocol.com/protocol/schema, read
	// 2026-09-02) declares the field optional, so a server that sent nothing
	// leaves this false — and a dialect that gates session/load on it then opens
	// a fresh conversation rather than sending a method the server never
	// offered.
	loadSession bool

	// turnTextChunks and turnActs track turn activity to build a fallback
	// summary when an ACP stream returns 0 text chunks before ending.
	turnTextChunks int
	turnActs       []string
	// turnOpen is a `session/prompt` having been sent and not yet answered.
	// It is what Closing reads to decide whether a cancel is owed at teardown;
	// Interrupt predates it and keeps its own test (a session and no held
	// brief), because that test was measured and this flag was not.
	turnOpen bool
}

var _ runner.Protocol = (*acpProtocol)(nil)

// acpLine is one JSON-RPC message in either direction.
type acpLine struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *acpError       `json:"error,omitempty"`
}

// acpError is the vendor's own refusal.
//
// CAPTURED from a `session/load` on a fabricated id, which is the exact shape
// the reattach path has to survive:
//
//	{"jsonrpc":"2.0","id":2,"error":{"code":-32602,"message":"Invalid params",
//	 "data":{"message":"Session \"00000000-…\" not found"}}}
//
// `data.message` is where the sentence a user can act on lives; `message` alone
// is the JSON-RPC category and says nothing.
type acpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Message string `json:"message"`
	} `json:"data"`
}

func (e *acpError) text() string {
	if e == nil {
		return ""
	}
	if e.Data.Message != "" {
		return e.Data.Message
	}
	return e.Message
}

// acpUpdate is the payload of a session/update notification.
type acpUpdate struct {
	SessionID string `json:"sessionId"`
	Update    struct {
		// SessionUpdate is the discriminator. Observed values: agent_message_chunk,
		// agent_thought_chunk, tool_call, tool_call_update, user_message_chunk,
		// available_commands_update, session_info_update.
		SessionUpdate string `json:"sessionUpdate"`

		// Content is one field carrying two unrelated shapes, which is why it is
		// raw rather than typed. On a message or thought chunk it is an OBJECT —
		// {"type":"text","text":"…"} — and on a completed edit it is an ARRAY of
		// blocks, [{"type":"diff","path":"…"}]. Both captured. A struct that
		// declared one would fail the whole line's unmarshal on the other and lose
		// the event rather than just its detail.
		Content json.RawMessage `json:"content"`

		// ToolCallID carries a literal newline in the middle —
		// "call-2b7fa526-…-0\nfc_ovRqA7e-…_0" — exactly as print mode's call_id
		// did. Both halves of a call carry the identical string, so correlation is
		// unaffected, and the id is never rendered.
		ToolCallID string `json:"toolCallId"`
		// Title is the vendor's OWN human-readable line for the call, and it is
		// the only naming field that is always populated. "Read File", "Find",
		// "grep", and for a shell call the command itself in backticks:
		// "`sed -n '2p' \"C:/…/marker.txt\"`".
		Title string `json:"title"`
		// Kind is the call's class: read, edit, execute, search, other.
		Kind string `json:"kind"`
		// Status: pending, in_progress, completed. `failed` is in the ACP schema
		// and was never observed here — every failure this capture produced
		// arrived as `completed` with an error in rawOutput.
		Status string `json:"status"`
		// RawInput is populated for a shell call ({"command":…}) and EMPTY for
		// Read/Find/grep, which is why Title rather than an argument field is what
		// names an entry. Measured on the live announcements; the same calls
		// replayed by session/load carry a fuller RawInput, which is a difference
		// worth knowing and not one anything here depends on.
		RawInput struct {
			Command string `json:"command"`
		} `json:"rawInput"`
		RawOutput json.RawMessage `json:"rawOutput"`
	} `json:"update"`
}

// chunkText reads the text out of a content OBJECT, and nothing out of an
// array. A chunk is always the object form; anything else yields "" rather than
// a guess at what a shape this adapter has not seen was carrying.
func (u acpUpdate) chunkText() string {
	var c struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(u.Update.Content, &c) != nil {
		return ""
	}
	return c.Text
}

// acpToolResult is what a completed call said about itself.
//
// Three shapes, all captured:
//
//	success (read)  rawOutput:{"content":"alpha\nbravo\ncharlie\n"}
//	success (shell) rawOutput:{"exitCode":0,"stdout":"…"}
//	failure         rawOutput:{"error":"Hook blocked with message: …"}
//
// and a fourth that carries NOTHING, which is the one that matters most: a call
// the user REJECTED at the permission prompt arrives as status `completed` with
// no rawOutput at all. On the wire a denial is indistinguishable from a
// completion that said nothing — which is the §9.8 argument for ActDenied
// arriving here in a sharper form. The room records a refusal from its own
// keystroke, and recordAct refuses to let this echo overwrite it.
//
// AMENDED 2026-08-15: the third shape is a hook that ERRORED, not a hook that
// denied, and the difference decides which of the four shapes arrives.
// Measured live on cursor-agent 2026.08.04-aaa8809 (design.md §7.16's amendment
// of the same date): a `beforeShellExecution` hook returning
// `permission: "deny"` cleanly, exit 0, produced NO error at all. The call ran
// `pending` → `in_progress` → `completed` with no rawOutput, and the turn ended
// `stopReason: end_turn`, while the command never ran — the FOURTH shape, not
// the third. The `Hook blocked with message: …` text belongs to a hook that
// failed to execute, which is exactly what PARITY.md records for this box's
// Git-Bash-parented wrapper (a bash syntax error, surfaced as that string).
// So the fourth shape covers two different refusals — the user's and a hook's —
// and neither is legible on the wire. That widens the argument above rather
// than changing it: the room still records a refusal from its own keystroke,
// and a hook's refusal is one the room cannot see at all.
type acpToolResult struct {
	Error    json.RawMessage `json:"error"`
	Content  json.RawMessage `json:"content"`
	Stdout   json.RawMessage `json:"stdout"`
	ExitCode *int            `json:"exitCode"`
}

// acpPermission is the vendor BLOCKING on a question.
//
// CAPTURED, whole, on a shell command the user's own cli-config allowlist did
// not cover:
//
//	{"jsonrpc":"2.0","id":0,"method":"session/request_permission","params":{
//	  "sessionId":"dacb7960-…",
//	  "toolCall":{"toolCallId":"call-4f71771e-…","title":"`mkdir zzz`","kind":"execute",
//	              "status":"pending","content":[{"type":"content","content":{"type":"text","text":"Not in allowlist: mkdir"}}]},
//	  "options":[{"optionId":"allow-once","name":"Allow once","kind":"allow_once"},
//	             {"optionId":"allow-always","name":"Allow always","kind":"allow_always"},
//	             {"optionId":"reject-once","name":"Reject","kind":"reject_once"}]}}
//
// Both branches were driven. `allow-once` ran the command; `reject-once` did
// not, and the directory was never created.
//
// `allow-always` is NEVER selected by council, in any posture, and that is a
// rule rather than a default. It writes a permanent rule into the user's own
// ~/.cursor/cli-config.json — council reaching into somebody's config to widen
// what an agent may do without asking again is the same line vendors.Cursor
// already declines to cross with `--trust`.
type acpPermission struct {
	SessionID string `json:"sessionId"`
	ToolCall  struct {
		ToolCallID string `json:"toolCallId"`
		Title      string `json:"title"`
		Kind       string `json:"kind"`
	} `json:"toolCall"`
	Options []struct {
		OptionID string `json:"optionId"`
		Kind     string `json:"kind"`
	} `json:"options"`
}

// pick returns the option id whose kind matches, or "" when the vendor did not
// offer one. Chosen by KIND rather than by the literal id, because the ids are
// this vendor's spelling and the kinds are the protocol's.
func (p acpPermission) pick(kind string) string {
	for _, o := range p.Options {
		if o.Kind == kind {
			return o.OptionID
		}
	}
	return ""
}

// acpPending is one permission request the vendor is blocked on, held until
// the room answers it.
//
// The two option ids are chosen when the request ARRIVES, not when it is
// answered, so that Decide and Interrupt have nothing to look up in the request
// beyond this record. An empty id means the vendor offered no option of that
// kind, and the answer is then the protocol's own `cancelled` outcome rather
// than an id invented for it — see acpDecisionFor.
type acpPending struct {
	id     json.RawMessage
	allow  string
	reject string
}

// acpDialect is everything about this client that is allowed to differ per
// vendor.
//
// Three fields, each one a decision that a live capture settled for cursor-agent
// and that nothing has settled for grok. Anything NOT in this struct is shared
// by construction: the state machine, the queue, the replay guard, the terminal
// handshake state and the refusal of every unanswered request are one
// implementation, so a divergence between the two seats has exactly one place
// to hide and a reader can see the whole of it here.
type acpDialect struct {
	// seat names the vendor in error text and nowhere else.
	seat string
	// readModeID is the `session/set_mode` id a read posture asks for, or ""
	// when this seat asks for no mode at all. See acpMode for the cursor
	// measurement behind "plan"; an empty value here is not "the same mode
	// under another name", it is the seat declining to request a mode nobody
	// has seen its server honour.
	readModeID string
	// fixedOptions answers a permission request with the option ids the vendor
	// was MEASURED offering (acpDecision), rather than with an id picked by kind
	// from the request itself. Cursor keeps the measured spelling on purpose:
	// a request whose option list changed shape would then be answered with an
	// id the vendor refuses, visibly, rather than with a remembered one that
	// still parses. A seat nobody has captured has no measured spelling to
	// keep, so it picks by kind — the field the protocol defines — and answers
	// `cancelled` when the kind it wants was not offered.
	fixedOptions bool
	// loadNeedsCapability gates `session/load` on the server having advertised
	// `agentCapabilities.loadSession: true`. The ACP schema requires a client
	// to check it; cursor-agent's capture carried it true on every handshake,
	// so the cursor seat never needed the gate and does not get one now, which
	// keeps its measured behaviour byte-identical. A seat whose server has not
	// been captured gets the gate, because sending a method the server never
	// offered is the one shape of handshake failure a client can avoid by
	// reading what it was told.
	loadNeedsCapability bool
}

// cursorDialect is the measured seat: every value here is the one the thirteen
// arms of 2026-08-08 drove (design.md §9.36).
var cursorDialect = acpDialect{seat: "cursor", readModeID: "plan", fixedOptions: true}

// grokDialect is the UNMEASURED seat, and each zero value is a claim withheld
// rather than a default accepted. READ FROM DOCS, NOT FROM A RUN: Grok Build
// 1.0.13's `grok agent stdio` is described as an ACP server over stdin/stdout
// (docs.x.ai/build/cli/headless-scripting and zed.dev/acp/agent/grok-build,
// both read 2026-09-02), and nothing in either page names a mode id, a
// permission option id, or whether session/load is advertised. So: no mode is
// requested in the read posture (the room refuses that posture's permission
// requests itself, which is the containment the badge claims and no more),
// options are picked by kind from each request, and session/load is sent only
// when the server says it may be. design.md §9.54 lists the runs owed.
var grokDialect = acpDialect{seat: "grok", loadNeedsCapability: true}

// mode is the session mode this posture asks for under this dialect, or ""
// when nothing is requested. See acpMode for the cursor measurement.
func (d acpDialect) mode(p Posture) string {
	if p == PostureRead {
		return d.readModeID
	}
	return ""
}

// newACPProtocol builds the cursor seat's driver for one process. Kept under
// its original name because it is the measured one and every cursor test and
// fixture replay names it.
func newACPProtocol(workspace, resumeID string, p Posture) *acpProtocol {
	return newACPProtocolWith(cursorDialect, workspace, resumeID, p)
}

// newACPProtocolWith builds the driver for one process under one dialect.
func newACPProtocolWith(d acpDialect, workspace, resumeID string, p Posture) *acpProtocol {
	return &acpProtocol{
		workspace: workspace,
		resumeID:  resumeID,
		posture:   p,
		dialect:   d,
		awaiting:  map[int]string{},
		perms:     map[string]acpPending{},
	}
}

// request builds one JSON-RPC request and remembers what it was for.
// The caller must hold the mutex.
func (a *acpProtocol) request(method string, params map[string]any) []byte {
	a.nextID++
	id := a.nextID
	a.awaiting[id] = method
	line, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method, "params": params,
	})
	if err != nil {
		// Marshalling a map of strings the room already holds cannot fail in
		// practice; returning nothing rather than a broken line keeps a corrupt
		// object off the vendor's stdin if it ever does.
		return nil
	}
	return line
}

func acpNotify(method string, params map[string]any) []byte {
	line, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "method": method, "params": params,
	})
	return line
}

// Opening is the handshake's first step and only its first step.
//
// The rest is sequenced by the RESPONSES rather than pipelined, and that is
// deliberate: session/new needs nothing from initialize's result, but issuing
// both at once would mean a failed initialize is followed by a session request
// against a server that has already said no. One question at a time is also what
// makes the state machine below readable as the transcript it mirrors.
func (a *acpProtocol) Opening() [][]byte {
	a.mu.Lock()
	defer a.mu.Unlock()
	return [][]byte{a.request("initialize", map[string]any{
		"protocolVersion": 1,
		// Declared FALSE, both of them, and the honesty is the point rather than
		// the caution. These advertise that the CLIENT will read and write files
		// on the agent's behalf; council does no such thing — the read/write
		// boundary in CLAUDE.md says the room writes only under ~/.telltale — so
		// claiming the capability would be an offer this program cannot honour.
		"clientCapabilities": map[string]any{
			"fs": map[string]any{"readTextFile": false, "writeTextFile": false},
		},
	})}
}

// openSession is the request that follows a successful initialize. The caller
// must hold the mutex.
func (a *acpProtocol) openSession() []byte {
	params := map[string]any{
		"cwd": a.workspace,
		// Empty, never absent. MCP servers reach OUTSIDE the directory council
		// was pointed at, which is the boundary --write widens; the same reason
		// the Claude adapter passes --strict-mcp-config in BOTH postures.
		"mcpServers": []any{},
	}
	if a.resumeID != "" && a.dialect.loadNeedsCapability && !a.loadSession {
		// The server did not say it can load a session, so the saved id is
		// spent without being sent: a `session/load` here would be a method the
		// server never offered, and the schema says a client checks first. The
		// one-attempt rule is unchanged — the id is gone, a fresh conversation
		// opens, and settleRestoredThread hears about it through the ordinary
		// turn exactly as it does when a load is refused.
		a.resumeID = ""
	}
	if a.resumeID != "" {
		params["sessionId"] = a.resumeID
		a.replaying = true
		return a.request("session/load", params)
	}
	return a.request("session/new", params)
}

// acpMode is the session mode this posture asks for ON THE CURSOR SEAT. It is
// the value cursorDialect.readModeID carries, kept as a function so the
// measurement below stays beside the word it justifies; the grok dialect asks
// for no mode, and that is a withheld claim rather than a different answer.
//
// MEASURED, one trial each, and the two results are not equally strong:
//
//   - `plan` REFUSED a write. Asked to create a file, the seat answered "Plan
//     mode is still on, so I can't create the file yet" and no file landed. That
//     is better evidence than print mode's `--mode plan` ever produced — there
//     the same mode was measured DISPATCHING `cat` and `ls -1` as shell calls —
//     but it is still one trial, and the refusal is worded as the model obeying
//     its mode rather than as an enforcement layer stopping it. The badge stays
//     `ro:requested` for exactly that reason.
//   - `agent` is the server's own default (`currentModeId":"agent"` on every
//     session/new captured), so the write postures send no set_mode at all.
//     Sending one to reassert a default is a request the room would then have to
//     defend, for no change in behaviour.
func acpMode(p Posture) string {
	if p == PostureRead {
		return "plan"
	}
	return ""
}

// ErrACPHandshakeFailed is returned by Turn once the handshake has failed.
//
// Refusing is the whole point: this process is up and useless, and the caller's
// remedy is to kill it and let the next brief start a fresh one — which is also
// the only recovery there is, since the usual cause is an auth or version
// problem the user may have fixed in the meantime.
var ErrACPHandshakeFailed = errors.New(
	"vendors: this seat's ACP handshake failed; its process cannot take a turn")

// ErrACPTurnNotStarted is returned by Interrupt when there is no outstanding
// prompt for the vendor to abandon.
//
// It is an error rather than a silent success because of what the caller does
// with each. interruptSeat treats a clean return as "the cancel was delivered"
// and stops — and nothing would then end the turn, because the turn's end IS the
// session/prompt response and no session/prompt was ever sent. An error falls
// through to the kill, which is the documented fallback for exactly this: a
// cancel that silently did nothing leaves the user watching a column they
// believe they stopped. Nothing is lost by killing here — there is no
// conversation yet, or none this turn reached.
var ErrACPTurnNotStarted = errors.New(
	"vendors: this ACP seat has no turn in flight to interrupt")

// Turn takes one turn, queueing it until the handshake is finished.
func (a *acpProtocol) Turn(prompt string) ([][]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.turnTextChunks = 0
	a.turnActs = nil
	if a.dead {
		return nil, ErrACPHandshakeFailed
	}
	if !a.ready {
		// The handshake is still in flight. Held rather than refused: the room
		// has already told the user this seat is working, and a turn dropped here
		// would be a column that sat out a brief it was addressed in.
		a.queued = append(a.queued, prompt)
		return nil, nil
	}
	return [][]byte{a.promptLine(prompt)}, nil
}

func (a *acpProtocol) promptLine(prompt string) []byte {
	a.turnOpen = true
	return a.request("session/prompt", map[string]any{
		"sessionId": a.sessionID,
		// A content-block ARRAY, not a string. The prompt is arbitrary user text
		// and is marshalled rather than assembled for the same reason Claude's
		// turn envelope is: by the time anyone uses this seriously the brief
		// contains quotes and newlines, and string building would produce a
		// broken line rather than a wrong one.
		"prompt": []map[string]any{{"type": "text", "text": prompt}},
	})
}

// Interrupt abandons the turn in flight without killing the process.
//
// VERIFIED LIVE: `session/cancel` is a NOTIFICATION — no id, no response — and
// the effect is on the outstanding `session/prompt`, which resolved 23ms later
// with `{"stopReason":"cancelled"}`. The process took a further turn 1.1s after
// that, from the same pid and the same session.
//
// The id argument is ignored, and that is a fact about this protocol rather than
// a stub: there is nothing to correlate. Claude's interrupt is a control request
// that comes back with a receipt; this one is fire-and-forget, and what confirms
// it is the prompt resolving, not an acknowledgement of its own.
func (a *acpProtocol) Interrupt(string) ([][]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Anything still waiting on the handshake is dropped whatever else happens.
	// A cancelled brief that arrives anyway is the room doing something the user
	// just stopped, and the window is not the narrow one it looks: a read-posture
	// seat holds its turn across the session/set_mode round trip too, which is
	// after the session id exists.
	held := len(a.queued) > 0
	a.queued = nil

	if a.sessionID == "" || held {
		// There is no outstanding session/prompt, so there is nothing for
		// session/cancel to end — and nothing that will ever produce this turn's
		// end. Reported as an error so the caller kills the process instead; see
		// ErrACPTurnNotStarted.
		return nil, ErrACPTurnNotStarted
	}

	// Anything the vendor is BLOCKED on is refused first, and this ordering is
	// load-bearing. A pending session/request_permission holds the vendor still
	// until it is answered; whether session/cancel releases one has never been
	// measured, and rejection has — reject-once resolves the call and the command
	// does not run. So the cancel is guaranteed to reach a vendor that is
	// listening rather than one waiting on a question nobody will now answer.
	//
	// Rejecting is also what ctrl+c MEANS for a call the user was being asked
	// about. Approving it on the way out would run the thing they just stopped.
	lines := a.refuseHeld()
	return append(lines, acpNotify("session/cancel",
		map[string]any{"sessionId": a.sessionID})), nil
}

// refuseHeld answers every permission request still open with a rejection and
// forgets it. The caller must hold the mutex.
func (a *acpProtocol) refuseHeld() [][]byte {
	var lines [][]byte
	for key, pending := range a.perms {
		lines = append(lines, acpDecisionFor(pending, false))
		delete(a.perms, key)
	}
	return lines
}

// Closing is what the room writes before it closes this process's stdin at
// teardown (vendors.GracefulStop).
//
// The same two moves Interrupt makes, in the same order and for the same
// reason: a vendor blocked on a question is refused first so the cancel reaches
// a server that is listening, and the open `session/prompt` is then cancelled
// so the process is idle when the pipe closes. Nothing is written when no turn
// is in flight — an idle server has nothing to abandon, and a cancel for a
// prompt that was never sent is the exact quiet no-op §9.36 warned about.
//
// UNMEASURED on either seat as a teardown sequence: the cursor capture measured
// `session/cancel` resolving a prompt in 23 ms and the process taking a further
// turn, never a stdin close after it. What a server does once its input pipe
// closes is the grace period's question, and Grace bounds it.
func (a *acpProtocol) Closing() [][]byte {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.queued = nil
	if a.sessionID == "" {
		return nil
	}
	lines := a.refuseHeld()
	if a.turnOpen {
		lines = append(lines, acpNotify("session/cancel",
			map[string]any{"sessionId": a.sessionID}))
	}
	return lines
}

// Grace is how long the room waits after closing stdin before it kills the
// process. Two seconds, and the figure is a bound rather than a measurement:
// no ACP server here has been watched exiting on a closed pipe, and the kill
// behind it is what actually ends the process. A number that was measured
// would be quoted with its capture; this one is quoted with its absence.
func (a *acpProtocol) Grace() time.Duration { return 2 * time.Second }

// Dead reports the terminal handshake state, for the room's fallback decision
// (vendors.LiveFallback). It is the same flag Turn refuses on; exposing it lets
// a caller ask BEFORE spending a brief on a process that cannot take one.
func (a *acpProtocol) Dead() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dead
}

// ErrACPUnknownRequest is returned when a decision names a request this protocol
// is not holding — an answer arriving after the turn that raised it ended.
var ErrACPUnknownRequest = errors.New("vendors: no such permission request is outstanding")

// Decide answers one blocked tool call.
//
// input is unused: ACP's answer is an option id and nothing else, where Claude's
// requires the tool's whole argument blob echoed back. The parameter stays in
// the signature so the room has one call shape for every persistent seat.
//
// reason is unused for the same kind of reason and it is worth naming, because
// it is a real difference in what the two seats can promise. Claude's denial
// carries council's own sentence back to the model — "denied by the person
// running this council room… do not retry a variation of it" — which is the one
// piece of council-authored text a vendor ever reads. ACP's rejection carries no
// message field at all, so this seat cannot ask the model not to retry. It was
// measured saying "DONE" after a rejection as though nothing had happened.
func (a *acpProtocol) Decide(requestID string, allow bool, _ string, _ map[string]any) ([][]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	pending, ok := a.perms[requestID]
	if !ok {
		return nil, ErrACPUnknownRequest
	}
	delete(a.perms, requestID)
	return [][]byte{acpDecisionFor(pending, allow)}, nil
}

// acpDecision is the response the CURSOR vendor is blocked on.
//
// The option ids are the ones this vendor offered on every captured request, and
// they are hardcoded HERE rather than remembered per request because the
// alternative is worse in the failure case: an option list that changed shape
// would leave the room echoing an id it did not understand. A wrong id is
// refused by the vendor and the turn fails visibly; a remembered-but-stale one
// would be answered as though it meant something.
func acpDecision(id json.RawMessage, allow bool) []byte {
	option := "reject-once"
	if allow {
		// allow-ONCE, never allow-always. See acpPermission.
		option = "allow-once"
	}
	return acpSelected(id, option)
}

// acpDecisionFor answers one held request with the option chosen for it when it
// arrived, or with the protocol's own `cancelled` outcome when the vendor
// offered no option of the kind the room wants.
//
// `cancelled` is a SCHEMA READ (agentclientprotocol.com/protocol/schema,
// 2026-09-02: RequestPermissionOutcome is `selected` with an optionId, or
// `cancelled`) and has never been sent to a live server from here. It is the
// answer for a request this client cannot honestly select on — a vendor that
// offers only `allow_always`, say — and it is chosen over inventing an id
// because a cancelled prompt is a call that does not run, which is the failure
// direction this room accepts.
func acpDecisionFor(p acpPending, allow bool) []byte {
	option := p.reject
	if allow {
		option = p.allow
	}
	if option == "" {
		line, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      p.id,
			"result":  map[string]any{"outcome": map[string]any{"outcome": "cancelled"}},
		})
		return line
	}
	return acpSelected(p.id, option)
}

func acpSelected(id json.RawMessage, option string) []byte {
	line, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"outcome": map[string]any{"outcome": "selected", "optionId": option},
		},
	})
	return line
}

// pendingFor decides, when a request ARRIVES, which option id will answer each
// branch. Under a fixed-options dialect the ids are the measured spelling;
// otherwise they are looked up by KIND on the request — `allow_once` and
// `reject_once`, the protocol's names — and `allow_always` is never a
// candidate under either, for the reason acpPermission states.
func (a *acpProtocol) pendingFor(id json.RawMessage, perm acpPermission) acpPending {
	p := acpPending{id: append(json.RawMessage(nil), id...)}
	if a.dialect.fixedOptions {
		p.allow, p.reject = "allow-once", "reject-once"
		return p
	}
	p.allow, p.reject = perm.pick("allow_once"), perm.pick("reject_once")
	return p
}

// Inbound is the whole state machine: one line in, events for the room out, and
// whatever has to be said back.
func (a *acpProtocol) Inbound(line []byte) ([]runner.Event, [][]byte) {
	if len(line) == 0 || line[0] != '{' {
		return nil, nil
	}
	var msg acpLine
	if err := json.Unmarshal(line, &msg); err != nil {
		return nil, nil
	}

	switch {
	case msg.Method != "" && len(msg.ID) > 0:
		return a.serverRequest(msg)
	case msg.Method != "":
		return a.notification(msg)
	case len(msg.ID) > 0:
		return a.response(msg)
	}
	return nil, nil
}

// serverRequest answers a question the vendor is blocked on.
func (a *acpProtocol) serverRequest(msg acpLine) ([]runner.Event, [][]byte) {
	if msg.Method != "session/request_permission" {
		// A vendor extension. `cursor/create_plan` was captured in plan mode and
		// is the only one this capture produced.
		//
		// Answered with an EMPTY result rather than ignored, and rather than
		// guessed at. A request left unanswered blocks the vendor forever, which
		// on a persistent seat is a column that never finishes and a room that
		// never lets go of the turn; a fabricated payload would be council
		// inventing a side of a protocol it has not read. An empty object is the
		// smallest well-formed thing that unblocks it, and the vendor accepted
		// it — the call it belonged to completed immediately afterwards.
		return nil, [][]byte{acpEmptyResult(msg.ID)}
	}

	var perm acpPermission
	if err := json.Unmarshal(msg.Params, &perm); err != nil {
		// Unreadable, and still answered: a request the room cannot parse is
		// still a vendor blocked on it. Refused with whatever this dialect
		// refuses with, which for a by-kind seat with no options to read is the
		// protocol's own `cancelled`.
		return nil, [][]byte{acpDecisionFor(a.pendingFor(msg.ID, acpPermission{}), false)}
	}

	a.mu.Lock()
	read := a.posture == PostureRead
	a.permSeq++
	key := "acp-perm-" + strconv.Itoa(a.permSeq)
	pending := a.pendingFor(msg.ID, perm)
	if !read {
		a.perms[key] = pending
	}
	a.mu.Unlock()

	if read {
		// A read-posture seat asking to change something is not a question for
		// the user; it is already answered. Raising a card here would offer
		// authority this posture withheld, which is the same silent upgrade
		// dispatch.go refuses when a write hop lands in a read room.
		//
		// It is still reported, as an ordinary trace entry, because a seat that
		// tried and was stopped is a thing that happened and a column that hid it
		// would read as one that never tried.
		return []runner.Event{{
			Kind: runner.KindActivity,
			Acts: []runner.ActCall{{
				ID:      perm.ToolCall.ToolCallID,
				Text:    acpCallText(perm.ToolCall.Title, perm.ToolCall.Kind, ""),
				Outcome: runner.ActFailed,
				Detail:  "refused: this seat is read-only",
			}},
		}}, [][]byte{acpDecisionFor(pending, false)}
	}

	// Handed to the room, which answers through Decide. Nothing is written back
	// here: the vendor is BLOCKED until it is, and that block is the whole value
	// of the card.
	return []runner.Event{{
		Kind: runner.KindGate,
		Gate: &runner.Gate{
			RequestID: key,
			ToolUseID: perm.ToolCall.ToolCallID,
			Tool:      perm.ToolCall.Kind,
			Text:      acpCallText(perm.ToolCall.Title, perm.ToolCall.Kind, ""),
			// Nil, and it must stay nil. Claude's Input exists because its
			// protocol requires the tool's whole argument blob echoed back on an
			// approval; ACP's answer is an option id. Carrying a blob nothing
			// sends would be a copy of a Write's entire file content held in
			// memory for no purpose.
			//
			// OldContent/NewContent are left empty for the same reason, and the
			// consequence is stated rather than hidden: this seat's cards carry
			// NO before/after preview (§9.41). `session/request_permission`'s
			// captured params name the call — a title and a kind — and carry
			// neither half of an edit, so there is nothing measured to draw. The
			// seat also does not ask about edits at all (§9.36), which is why
			// this is a smaller hole than it sounds.
		},
	}}, nil
}

func acpEmptyResult(id json.RawMessage) []byte {
	line, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": append(json.RawMessage(nil), id...),
		"result": map[string]any{},
	})
	return line
}

// notification handles session/update, which is the whole of the vendor's
// streaming surface.
func (a *acpProtocol) notification(msg acpLine) ([]runner.Event, [][]byte) {
	if msg.Method != "session/update" {
		return nil, nil
	}
	var up acpUpdate
	if err := json.Unmarshal(msg.Params, &up); err != nil {
		return nil, nil
	}

	a.mu.Lock()
	replaying := a.replaying
	a.mu.Unlock()
	if replaying {
		// HISTORY, not news. `session/load` streams the ENTIRE prior conversation
		// back as ordinary session/update notifications before it answers — the
		// user's old prompts, the old tool calls with their real output, the old
		// replies — and a parser that appended them would refill a reattached
		// column with the whole previous room. Measured on both load arms.
		//
		// The gate is the pending response rather than the `replay-` prefix the
		// replayed tool ids happen to carry, because a prefix is a spelling and
		// the pending request is the protocol: everything arriving before
		// session/load resolves is by definition what was already said.
		return nil, nil
	}

	switch up.Update.SessionUpdate {
	case "agent_message_chunk":
		// The vendor speaking. Chunks concatenate into the reply exactly as sent
		// ("Second line:" + " `bravo`\n\nDONE"); any separator this adapter added
		// would be a character the vendor did not write.
		if t := up.chunkText(); t != "" {
			a.mu.Lock()
			a.turnTextChunks++
			a.mu.Unlock()
			return []runner.Event{{Kind: runner.KindText, Text: t}}, nil
		}

	case "agent_thought_chunk":
		// Reasoning. Dropped, on exactly the policy print mode's `thinking` was
		// dropped under: it is neither the vendor's answer nor a thing the vendor
		// DID, so routing it to the body would pad the column with commentary
		// nobody asked for and routing it to the trace would file it as a call.

	case "user_message_chunk":
		// Council's OWN prompt, coming back. Only ever seen inside a session/load
		// replay, which the guard above has already dropped — it is named here
		// anyway because print mode had the identical trap on its `user` event,
		// and a future capture that produced one outside a replay would otherwise
		// put the brief into the column as though the vendor had said it.

	case "tool_call", "tool_call_update":
		if act, ok := acpAct(up); ok {
			if act.Text != "" {
				a.mu.Lock()
				title := act.Text
				alreadyHave := false
				for _, existing := range a.turnActs {
					if existing == title {
						alreadyHave = true
						break
					}
				}
				if !alreadyHave {
					a.turnActs = append(a.turnActs, title)
				}
				a.mu.Unlock()
			}
			return []runner.Event{{Kind: runner.KindActivity, Acts: []runner.ActCall{act}}}, nil
		}

	case "session_info_update", "available_commands_update", "current_mode_update", "plan":
		// Chrome for an interactive client: the chat's generated title, the slash
		// commands it could offer, the mode selector's state. None of it is
		// something the vendor said or did, and available_commands_update in
		// particular is a multi-kilobyte payload on every session — rendering it
		// would bury the turn it arrived beside.
	}
	return nil, nil
}

// acpCallText names one call for a narrow column.
//
// Title first because it is the only field always populated, and because for a
// shell call it IS the command — the vendor writes it in backticks itself. The
// backticks are stripped: they are markdown for a chat client, and the trace
// renders its entries in plain text beside three other vendors that send none.
//
// rawInput.command is the fallback, and the bare kind is the last resort. A call
// that named itself nothing still lands, because a column that went quiet during
// the part of the turn it was busiest reads as one that hung.
func acpCallText(title, kind, command string) string {
	for _, s := range []string{strings.Trim(strings.TrimSpace(title), "`"), command, kind} {
		if strings.TrimSpace(s) != "" {
			return clipArg(s)
		}
	}
	return "tool call"
}

// acpAct turns one tool_call or tool_call_update into a trace entry.
func acpAct(up acpUpdate) (runner.ActCall, bool) {
	u := up.Update
	if u.ToolCallID == "" {
		// Nothing to correlate an outcome onto later. Dropped rather than landed
		// as a permanently pending entry.
		return runner.ActCall{}, false
	}
	act := runner.ActCall{
		ID:   u.ToolCallID,
		Text: acpCallText(u.Title, u.Kind, u.RawInput.Command),
	}
	// An update carries no title of its own on most lines; recordAct keeps the
	// announcement's text when a later line has none, so an empty one here is
	// not a loss.
	if u.Title == "" && u.RawInput.Command == "" {
		act.Text = ""
	}

	switch u.Status {
	case "", "pending", "in_progress":
		// Announced or running. ActPending is what makes a call visible before it
		// resolves, and recordAct will not let a second announcement un-resolve
		// an entry that already has an outcome.
		return act, true
	case "failed":
		// In the ACP schema and never observed here — every failure this capture
		// produced arrived as `completed` with an error in rawOutput. Handled so
		// that a vendor which starts using it is not read as a success.
		act.Outcome = runner.ActFailed
		act.Detail = clipArg(firstLine(acpResultText(u.RawOutput)))
		return act, true
	}

	// completed.
	var res acpToolResult
	if len(u.RawOutput) > 0 {
		_ = json.Unmarshal(u.RawOutput, &res)
	}
	switch {
	case len(res.Error) > 0:
		act.Outcome = runner.ActFailed
		act.Detail = clipArg(firstLine(acpResultText(res.Error)))
	case len(res.Content) > 0 || len(res.Stdout) > 0 || res.ExitCode != nil || len(u.Content) > 0:
		// It produced something: a file's contents, a shell's stdout and exit
		// code, or the `content:[{"type":"diff",…}]` an edit reports. All three
		// captured.
		act.Outcome = runner.ActOK
	default:
		// It ended and said NOTHING about how — which is the shape a REJECTED
		// call arrives in. ActUnknown rather than ActOK: inventing a success on a
		// vendor's behalf is the one thing this trace is built not to do, and a
		// user who denied this call already has ActDenied recorded from their own
		// keystroke, which recordAct will not let this overwrite.
		act.Outcome = runner.ActUnknown
	}
	return act, true
}

// acpResultText digs the vendor's own words out of a payload that is a string
// in some shapes and an object in others. Same discipline as cursorFailText was
// written under, and for the same reason: a struct that guessed wrong would lose
// the entry's detail rather than just its shape.
func acpResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		Error   string `json:"error"`
		Message string `json:"message"`
		Stderr  string `json:"stderr"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		for _, v := range []string{obj.Error, obj.Message, obj.Stderr} {
			if v != "" {
				return v
			}
		}
	}
	return ""
}

// response routes an answer to one of our own requests.
func (a *acpProtocol) response(msg acpLine) ([]runner.Event, [][]byte) {
	var id int
	if json.Unmarshal(msg.ID, &id) != nil {
		return nil, nil
	}
	a.mu.Lock()
	method, ours := a.awaiting[id]
	if !ours {
		a.mu.Unlock()
		return nil, nil
	}
	delete(a.awaiting, id)

	switch method {
	case "initialize":
		if msg.Error != nil {
			ev := a.fail("the vendor refused the ACP handshake: " + msg.Error.text())
			a.mu.Unlock()
			return ev, nil
		}
		// What the server advertised, read for exactly one decision (see
		// loadSession). The rest of the capability block is not modelled,
		// on claude.go's Capabilities rule: an advertisement is not what a
		// behaviour claim rests on.
		var caps struct {
			AgentCapabilities struct {
				LoadSession bool `json:"loadSession"`
			} `json:"agentCapabilities"`
		}
		_ = json.Unmarshal(msg.Result, &caps)
		a.loadSession = caps.AgentCapabilities.LoadSession
		out := a.openSession()
		a.mu.Unlock()
		return nil, [][]byte{out}

	case "session/load":
		a.replaying = false
		if msg.Error != nil {
			// The saved thread is gone. MEASURED: `-32602 … Session "…" not
			// found`, 0.45s, and — the part that decides the design — the PROCESS
			// SURVIVES. A fresh session opened in the same process 0.45s later
			// and answered.
			//
			// So a dead thread costs this seat two round trips and no respawn,
			// where the print-mode path paid a whole process to discover the same
			// thing by exiting. The one-attempt rule still holds and is now
			// cheap: the id is spent here, a new conversation opens immediately,
			// and settleRestoredThread hears about it through the ordinary turn.
			a.resumeID = ""
			out := a.openSession()
			a.mu.Unlock()
			return nil, [][]byte{out}
		}
		// A loaded session keeps the id it was loaded with — there is no new one
		// in the response — which is what keeps the saved-room file valid across
		// repeated reattaches.
		return a.sessionOpened(a.resumeID, msg)

	case "session/new":
		if msg.Error != nil {
			ev := a.fail("the vendor would not open a session: " + msg.Error.text())
			a.mu.Unlock()
			return ev, nil
		}
		var out struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(msg.Result, &out)
		if out.SessionID == "" {
			ev := a.fail("the vendor opened a session and did not name it")
			a.mu.Unlock()
			return ev, nil
		}
		return a.sessionOpened(out.SessionID, msg)

	case "session/set_mode":
		// The mode is set or it is not; either way the queued turn goes now.
		//
		// A REFUSED set_mode is a failure of the turn, not a reason to run it
		// anyway: the room would be dispatching a brief under an authority the
		// column's badge says it does not have. That is the silent upgrade
		// seatProcess respawns to avoid, one layer down.
		if msg.Error != nil {
			ev := a.fail("this seat could not be put in its requested mode: " +
				msg.Error.text())
			a.mu.Unlock()
			return ev, nil
		}
		lines := a.flushQueued()
		a.mu.Unlock()
		return nil, lines

	case "session/prompt":
		a.turnOpen = false
		textChunks := a.turnTextChunks
		acts := append([]string(nil), a.turnActs...)
		a.mu.Unlock()
		return acpTurnEnded(msg, textChunks, acts), nil
	}
	a.mu.Unlock()
	return nil, nil
}

// sessionOpened is the common tail of session/new and session/load. The caller
// must hold the mutex; this releases it.
func (a *acpProtocol) sessionOpened(id string, _ acpLine) ([]runner.Event, [][]byte) {
	a.sessionID = id
	ev := []runner.Event{{Kind: runner.KindSession, SessionID: id}}
	if mode := a.dialect.mode(a.posture); mode != "" {
		out := a.request("session/set_mode", map[string]any{
			"sessionId": id, "modeId": mode,
		})
		a.mu.Unlock()
		// The queued turn waits for the mode to land. Sending both at once would
		// race a brief against the posture it is supposed to run under, and the
		// race would be invisible: the reply would arrive looking exactly like a
		// reply from the mode the badge claims.
		return ev, [][]byte{out}
	}
	lines := a.flushQueued()
	a.mu.Unlock()
	return ev, lines
}

// flushQueued opens the gate and turns whatever was waiting behind it into
// lines. The caller must hold the mutex.
//
// Setting ready HERE rather than at the two call sites is deliberate: this is
// called at exactly the two moments the handshake is complete — session/new
// answering when no mode is wanted, and session/set_mode answering when one is —
// so tying the flag to the flush makes it impossible to open one without the
// other.
func (a *acpProtocol) flushQueued() [][]byte {
	a.ready = true
	if len(a.queued) == 0 {
		return nil
	}
	var out [][]byte
	for _, p := range a.queued {
		out = append(out, a.promptLine(p))
	}
	a.queued = nil
	return out
}

// fail puts the protocol into its terminal state and reports it. The caller must
// hold the mutex.
//
// Three things at once, and all three are needed: the turn ends visibly, the
// queue is emptied so nothing is delivered to a session that will never exist,
// and dead makes every later Turn refuse rather than queue against a handshake
// that has already finished failing.
func (a *acpProtocol) fail(note string) []runner.Event {
	a.dead = true
	a.queued = nil
	return acpFailed(note)
}

// acpUsage is the optional token block the ACP schema declares on a
// session/prompt response. See the field comment in acpTurnEnded for the
// version it was read at, the measurement that found it absent, and why nothing
// consumes it yet.
//
// Pointers on the three optional counts so that "the vendor did not send this"
// stays distinguishable from "the vendor sent zero" the day a capture arrives —
// the distinction the whole product exists to keep, and the one that is
// impossible to add back after the fact if the struct flattened it.
type acpUsage struct {
	InputTokens       int64  `json:"inputTokens"`
	OutputTokens      int64  `json:"outputTokens"`
	TotalTokens       int64  `json:"totalTokens"`
	CachedReadTokens  *int64 `json:"cachedReadTokens"`
	CachedWriteTokens *int64 `json:"cachedWriteTokens"`
	ThoughtTokens     *int64 `json:"thoughtTokens"`
}

// acpTurnEnded reads the response a turn resolves with.
//
// When textChunks is 0, a turn summary / fallback status is captured on the
// end event's Text field so the column is not left empty.
func acpTurnEnded(msg acpLine, textChunks int, acts []string) []runner.Event {
	if msg.Error != nil {
		return []runner.Event{{
			Kind: runner.KindError, EndsTurn: true,
			Note: "the vendor reported the turn failed: " + msg.Error.text(),
		}}
	}
	var out struct {
		StopReason string `json:"stopReason"`
		// Usage is PARSED AND NOTHING ELSE — nothing reads it, nothing relays
		// it, nothing renders it. That is the whole of the change, deliberately.
		//
		// The field names come from the ACP response schema in the bundle at
		// cursor-agent 2026.08.04-aaa8809 (`8096.index.js`), where the
		// session/prompt response is declared as `{stopReason, usage?}` and
		// usage as `{inputTokens, outputTokens, totalTokens, cachedReadTokens?,
		// cachedWriteTokens?, thoughtTokens?}`. So the shape is a source read at
		// a pinned version, not a guess.
		//
		// It was also measured ABSENT: the thirteen live arms of 2026-08-08
		// (§9.36) resolved every turn with `{"stopReason":…}` and nothing
		// beside it, which is the file-header claim above and it still stands.
		// The schema says optional; this build never sent one.
		//
		// Why it stops here rather than continuing into the token relay: this
		// vendor has already published a DERIVED token count under a raw-sounding
		// name once. Print mode's `result.usage` carried an `inputTokens` the CLI
		// computed rather than read (§7.16), and the statusline seam does it
		// again today — `total_input_tokens` there is `used_percentage × window
		// size`, which the vendor's own docs call derived. `inputTokens` is
		// exactly the name that burned, so whether THIS one is raw is an open
		// question that only a live capture at a pinned version can close.
		// Displaying it first and asking after is the ADR-001 violation itself.
		Usage *acpUsage `json:"usage"`
	}
	_ = json.Unmarshal(msg.Result, &out)
	switch out.StopReason {
	case "end_turn", "cancelled", "":
		// MEASURED, both words. `cancelled` is not a failure: it is the user's own
		// keystroke coming back, and finishColumn's cancellation check is what
		// words it. An empty reason is treated as a clean end rather than as an
		// error, because a turn that produced a whole reply and then resolved
		// without saying why is not evidence of a failure.
		ev := runner.Event{Kind: runner.KindMeta, EndsTurn: true}
		if textChunks == 0 {
			ev.Text = acpFallbackSummary(acts, out.StopReason)
		}
		return []runner.Event{ev}
	}
	// Anything else — the schema also carries `refusal`, `max_tokens` and
	// `max_turn_requests`, none of them observed here. Reported as a failure in
	// the vendor's own word rather than rendered as a normal answer, and the word
	// is quoted rather than translated because this adapter has never seen one
	// and has no business paraphrasing it.
	return []runner.Event{{
		Kind: runner.KindError, EndsTurn: true,
		Note: "the vendor stopped this turn: " + out.StopReason,
	}}
}

func acpFallbackSummary(acts []string, stopReason string) string {
	if len(acts) > 0 {
		return fmt.Sprintf("[Turn completed with 0 text chunks: %d tool call(s) executed (%s)]", len(acts), strings.Join(acts, ", "))
	}
	if stopReason == "cancelled" {
		return "[Turn cancelled with 0 text chunks streamed]"
	}
	return "[Turn completed with 0 text chunks streamed]"
}

// acpFailed ends the turn on a handshake that did not complete.
//
// EndsTurn is set even though no turn may have been sent yet, and that is the
// point: the room dispatched to this seat, the seat cannot answer, and a column
// left in PhaseStreaming would wait forever on a process that is up and useless.
func acpFailed(note string) []runner.Event {
	return []runner.Event{{Kind: runner.KindError, EndsTurn: true, Note: note}}
}
