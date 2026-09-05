package vendors

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The `codex app-server` wire, as it was actually driven.
//
// EVERYTHING in this file is MEASURED against **codex-cli 0.149.1** on Windows
// 11, over eight billed turns on 2026-08-29 (design.md §9.50 carries the arms,
// the timings and the two verdicts). This file carries the shapes, so a reader
// changing a struct field can see the line it was copied from without leaving
// the code. Where a shape is a SCHEMA READ rather than a capture, the comment
// says so in the same sentence — `codex app-server generate-json-schema --out
// <dir>` writes the whole protocol at the installed build, and a schema read at
// a pinned version is weaker evidence than a live line but stronger than a
// guess.
//
// It is JSON-RPC 2.0, one object per line, on stdin/stdout, and it is NOT ACP:
// the method names, the id namespaces and the turn shape are all this vendor's
// own. So this is a SIBLING of acpProtocol behind runner.Protocol, never a
// shared client. Nothing here is derived from cursoracp.go, and the two files
// deliberately do not factor a "JSON-RPC base" out between them — the only
// thing they share is the transport, and a shared base would make every future
// change to one seat a change to the other.
//
// The handshake, verbatim (abridged only where a thread record sat):
//
//	>> {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"telltale-probe",…}}}
//	<< {"id":1,"result":{"userAgent":"telltale-probe/0.149.1 (Windows 10.0.26200; x86_64) …","codexHome":"C:\\Users\\…\\.codex","platformFamily":"windows","platformOs":"windows"}}
//	>> {"jsonrpc":"2.0","method":"initialized","params":{}}
//	>> {"jsonrpc":"2.0","id":2,"method":"thread/start","params":{"cwd":"…","sandbox":"read-only"}}
//	<< {"id":2,"result":{"thread":{"id":"01a04fdd-…","sessionId":"01a04fdd-…",…}}}
//	<< {"method":"thread/started","params":{"thread":{…}}}
//
// A turn:
//
//	>> {"jsonrpc":"2.0","id":3,"method":"turn/start","params":{"threadId":"…","input":[{"type":"text","text":"…"}]}}
//	<< {"method":"turn/started","params":{"threadId":"…","turn":{"id":"01a04fde-c25a-…","status":"inProgress",…}}}
//	<< {"method":"item/agentMessage/delta","params":{"threadId":"…","turnId":"…","itemId":"msg_…","delta":"I"}}
//	<< {"method":"item/completed","params":{"item":{"type":"agentMessage","id":"msg_…","text":"…","phase":"final_answer"}}}
//	<< {"method":"turn/completed","params":{"threadId":"…","turn":{"id":"…","status":"completed","durationMs":32622,…}}}
//
// What this surface has that `codex exec --json` does not, all captured live:
//
//   - **A warm thread.** Turn 2 on the same process cost **1.486 s** against
//     turn 1's 32.785 s, and it answered a question only turn 1's history could
//     answer. `codex exec --json` pays its whole fixed cost on every turn.
//   - **Token usage, live, with a denominator.** `thread/tokenUsage/updated`
//     carries `total` AND `last` counts plus `modelContextWindow`. The `--json`
//     stream carries a `usage` block on `turn.completed` and no window at all.
//   - **Account rate limits, live.** `account/rateLimits/updated` carries
//     `usedPercent`, `windowDurationMins`, `resetsAt` and `planType` per window.
//     The `--json` stream carries none of it; the statusline reads the same
//     fields off disk instead (§3.4).
//   - **Hooks.** `hook/started` and `hook/completed` name the hook, its source
//     file and its `durationMs`. The `--json` stream mentions no hook at all.
//   - **Typed shell items.** `commandExecution` carries `command`, `cwd`,
//     `processId`, `status`, `exitCode`, `durationMs` and `aggregatedOutput`.
//
// And one thing it has that the room must defend against, which is the reason
// §9.6c's rule is re-asserted here rather than assumed away: **the deltas and
// the completed item are the SAME text.** Measured across four agentMessage
// items over two turns, the concatenated `item/agentMessage/delta` payloads
// equal `item/completed`'s `text` byte for byte, every time. A parser that read
// both would print every answer twice.
//
// WHY THIS SEAT IS REGISTERED NOW, AND WHAT IS STILL OWED. From 2026-08-29 to
// 2026-09-02 the room dispatched codex through `codex exec --json`
// (vendors/codex.go) and this protocol shipped unseated, on the measurement
// appServerSandbox records: the shell router goes through `pwsh.exe`, pwsh
// cannot start under the Windows sandbox on this box, and the model abandoned
// the turn rather than retrying in two of three read-posture arms. That
// measurement has NOT been repeated. What changed is the ledger's other side
// (design.md §9.57): a crew tool needs seats that stay up between briefs and a
// gate that can ask more than one vendor, and this protocol is the only codex
// surface with either — a warm turn measured at 1.486 s against exec's whole
// fixed cost, and a server-initiated approval request the room can answer. So
// the seat moved, with three things holding the honesty line:
//
//   - The exec adapter stays beside it as the FALLBACK (vendors.LiveFallback):
//     a refused handshake hands the brief to the measured invocation instead
//     of reporting the same refusal every turn.
//   - The seat OWNS THE KILL. §9.50 measured stdin close not reliably ending
//     this server (four runs exited in 1.5–3.3 s, one was alive 15 s later),
//     so teardown says `turn/interrupt` for a turn still open, closes stdin,
//     waits a bounded grace, and then kills (vendors.GracefulStop; Closing and
//     Grace below).
//   - The badge says UNMEASURED. The installed build is 0.152.1 and nothing in
//     this file was driven against it; the approval flow was never driven at
//     all. detect.go's codex badges carry the version and the word, and §9.57
//     lists the runs that would let them drop it — the read posture's liveness
//     first, because that is the property the read badge sells and the one
//     this path was measured failing.

// appServerProtocol is one `codex app-server` conversation, for the life of one
// process.
//
// Stateful because the protocol is: a `turn/start` cannot be built before
// `thread/start` has answered with a thread id, and the answer arrives on a
// goroutine that is not the one taking turns. Hence the mutex — Inbound runs on
// the runner's stdout pump, Turn/Interrupt/Decide on the room's update loop.
// Same shape as acpProtocol, for the same mechanical reason, and by parallel
// derivation rather than by sharing code.
type appServerProtocol struct {
	workspace string
	// resumeID is a thread from a saved room, or empty for a new conversation.
	resumeID string
	posture  Posture

	mu     sync.Mutex
	nextID int
	// what each outstanding id of OURS was asked for, so a response can be
	// routed without a switch on a method name we no longer have.
	awaiting map[int]string
	// threadID is the vendor's own id for this conversation, and the key the
	// saved room stores. Empty until thread/start or thread/resume answers.
	threadID string
	// turnID is the vendor's id for the turn in flight, captured off
	// `turn/started`. It is what `turn/interrupt` names, and it is the reason
	// that request cannot be built from the room's own bookkeeping.
	turnID string
	// queued holds turns taken before the handshake finished.
	//
	// A slice rather than a single slot, for acpProtocol's reason: nothing
	// structurally prevents a second brief arriving during a slow handshake,
	// and dropping one silently is the failure this product refuses. The
	// handshake here is two round trips and was measured at 0.67 s to 2.77 s,
	// the spread being this box's five MCP servers rather than the vendor.
	queued []string
	// ready is the handshake being COMPLETE, which is the thread existing.
	//
	// Unlike the ACP seat there is no mode round trip after the thread opens —
	// the posture rides `thread/start`'s own `sandbox` parameter — so ready and
	// "threadID is set" become true on the same line. It is still a separate
	// field, because the two are separate claims and a future posture channel
	// that needed its own round trip would otherwise reopen the exact race
	// acpProtocol's `ready` exists to close.
	ready bool
	// dead is the handshake having failed, and it is a TERMINAL state.
	//
	// Same argument as acpProtocol's, and it applies harder here: an app-server
	// that refuses `initialize` does NOT exit. Closing its stdin does not
	// reliably stop it either — one probe run stayed alive 15 s after stdin
	// closed and had to be killed — so the room's stale-exit guard would see a
	// live process and keep handing it briefs forever.
	dead bool
	// seenDeltas records which agentMessage items streamed at least one delta.
	//
	// This is the whole-message-repeat guard, and it is keyed per item rather
	// than per turn because a turn carries several messages and each one is
	// independently complete. MEASURED: the deltas concatenate to the completed
	// item's text exactly, on every item of every arm. So a completed item whose
	// deltas were seen contributes a SEPARATOR and no text; one whose deltas
	// were not seen contributes its text, which is the safety net for a build
	// that stops streaming.
	seenDeltas map[string]bool

	// approvalSeq numbers the approvals this seat has seen, so each one lands as
	// its own trace entry or its own card rather than overwriting the last.
	approvalSeq int
	// pending holds the approvals a write posture has handed to the room and
	// not yet answered, keyed by the id the room decides by. The value carries
	// the vendor's raw JSON-RPC id and which generation of the approval
	// vocabulary answers it (see appServerDecision). A read posture never adds
	// to it: those requests are refused on arrival.
	pending map[string]appServerPending

	// usage and limits are the last figures the vendor reported.
	//
	// PARSED AND NOTHING ELSE — nothing reads them, nothing relays them, nothing
	// renders them. That is deliberate and it is the same line acpUsage stops
	// at. Rendering a quota or a token total is §7.15/§7.16 vocabulary and those
	// seams have owners: the statusline writes the quota relay, the token relay's
	// DISPLAY is retired by its owner, and council's own render surface makes no
	// claim about either today. Capturing the fields here is what makes the
	// follow-up a read rather than a re-measurement.
	usage  *appServerTokenUsage
	limits *appServerRateLimits
}

var _ runner.Protocol = (*appServerProtocol)(nil)

// appServerLine is one JSON-RPC message in either direction.
//
// `id` is raw for the reason acpProtocol's is: the vendor numbers its own
// requests independently of ours, and JSON-RPC permits an id to be a string as
// easily as a number.
type appServerLine struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *appServerError `json:"error,omitempty"`
}

// appServerError is the vendor's own refusal, in the JSON-RPC envelope's own
// shape. `data` is declared by the schema at this build and was never observed
// populated, so the fallback to `message` is the path that has actually run.
type appServerError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *appServerError) text() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// appServerItem is one item on the thread, in the shape `item/started` and
// `item/completed` both carry it.
//
// The observed `type` values across the arms: `userMessage`, `agentMessage`,
// `reasoning`, `commandExecution`. The schema at this build declares more
// (file changes, MCP tool calls, plans); those are UNOBSERVED here and are
// dropped rather than guessed at, which is safe in this direction — an unlisted
// type produces no trace entry instead of a wrong one.
type appServerItem struct {
	Type string `json:"type"`
	ID   string `json:"id"`

	// agentMessage. Phase was observed as `commentary` and `final_answer`; both
	// are the vendor speaking and both are rendered, because a seat that showed
	// only its final answer would go quiet through the part of the turn it was
	// busiest — the same argument codex.go's item whitelist rests on.
	Text string `json:"text"`

	// commandExecution. `command` is the shell line the vendor actually ran,
	// already assembled by its own tool router; on this box that router wraps
	// commands in `pwsh.exe -NoProfile -Command …` unless the model names a
	// program itself. Captured verbatim:
	//
	//	"command":"\"C:\\WINDOWS\\system32\\cmd.exe\" /c '>inside.txt echo probe'"
	Command string `json:"command"`
	// Status was captured as `inProgress`, `completed` and `failed`. All three
	// are live values on this wire, which is a real difference from the `exec`
	// stream, where `failed` was captured and `completed` only ever arrived
	// beside an exit code that already settled it.
	Status string `json:"status"`
	// ExitCode is a POINTER for codex.go's reason, and the reason survives the
	// protocol change: `null` while the command runs, a real number when it
	// ends. A plain int would flatten "still running" into zero, which is the
	// spelling of success.
	ExitCode *int `json:"exitCode"`
	// AggregatedOutput is the command's own output, and the only source of a
	// failure line this adapter will show. Council composes no diagnosis of its
	// own. Captured on the sandbox arms as exactly `"Access is denied.\r\n"`.
	AggregatedOutput string `json:"aggregatedOutput"`
}

// appServerTokenUsage is `thread/tokenUsage/updated`'s payload.
//
// CAPTURED whole, on every arm:
//
//	{"method":"thread/tokenUsage/updated","params":{"threadId":"…","turnId":"…",
//	 "tokenUsage":{"total":{"totalTokens":23760,"inputTokens":23595,"cachedInputTokens":6912,
//	 "cacheWriteInputTokens":0,"outputTokens":165,"reasoningOutputTokens":0},
//	 "last":{…},"modelContextWindow":258400}}}
//
// `total` is the thread's running figure and `last` is this turn's; both were
// observed, and on the first turn of a thread they are equal, which is a fact
// about the first turn and not about the fields. `modelContextWindow` is the
// denominator §3.2 records as absent from every other codex surface — it is
// what would make a context percentage a read rather than an invention.
//
// Pointers on nothing here, and that is deliberate rather than an oversight:
// every count was observed populated on every capture, `cacheWriteInputTokens`
// and `reasoningOutputTokens` included, both at a MEASURED zero. There is no
// absent case to keep distinct because none was seen.
type appServerTokenUsage struct {
	Total              appServerTokenCounts `json:"total"`
	Last               appServerTokenCounts `json:"last"`
	ModelContextWindow int64                `json:"modelContextWindow"`
}

type appServerTokenCounts struct {
	TotalTokens           int64 `json:"totalTokens"`
	InputTokens           int64 `json:"inputTokens"`
	CachedInputTokens     int64 `json:"cachedInputTokens"`
	CacheWriteInputTokens int64 `json:"cacheWriteInputTokens"`
	OutputTokens          int64 `json:"outputTokens"`
	ReasoningOutputTokens int64 `json:"reasoningOutputTokens"`
}

// appServerRateLimits is `account/rateLimits/updated`'s payload.
//
// CAPTURED whole:
//
//	{"method":"account/rateLimits/updated","params":{"rateLimits":{"limitId":"codex",
//	 "primary":{"usedPercent":10,"windowDurationMins":300,"resetsAt":1788058823},
//	 "secondary":{"usedPercent":2,"windowDurationMins":10080,"resetsAt":1788645623},
//	 "credits":{"hasCredits":false,"unlimited":false,"balance":"0"},"planType":"plus",…}}}
//
// These are QUOTA fields in §7.15's vocabulary — a share of a window that
// resets — and never spend. Nothing here converts one into the other, and the
// window pair is carried as the vendor sent it rather than reduced to one
// number, because "10% of a 5-hour window" and "2% of a weekly window" are two
// different facts and a room that showed one would be answering a question
// nobody asked.
//
// `secondary` is a pointer because the schema declares it nullable and a
// capture with it null is the case the room would otherwise render as a
// measured zero. It was observed POPULATED on this account; the pointer is for
// the account that has no weekly window, not for this one.
type appServerRateLimits struct {
	LimitID   string                `json:"limitId"`
	Primary   *appServerRateWindow  `json:"primary"`
	Secondary *appServerRateWindow  `json:"secondary"`
	PlanType  string                `json:"planType"`
	Credits   *appServerRateCredits `json:"credits"`
}

type appServerRateWindow struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins int64   `json:"windowDurationMins"`
	ResetsAt           int64   `json:"resetsAt"`
}

type appServerRateCredits struct {
	HasCredits bool   `json:"hasCredits"`
	Unlimited  bool   `json:"unlimited"`
	Balance    string `json:"balance"`
}

// newAppServerProtocol builds the driver for one process.
func newAppServerProtocol(workspace, resumeID string, p Posture) *appServerProtocol {
	return &appServerProtocol{
		workspace:  workspace,
		resumeID:   resumeID,
		posture:    p,
		awaiting:   map[int]string{},
		seenDeltas: map[string]bool{},
		pending:    map[string]appServerPending{},
	}
}

// appServerPending is one approval the vendor is blocked on, held until the
// room answers it.
type appServerPending struct {
	id json.RawMessage
	// v2 is the `item/*/requestApproval` generation, answered with
	// accept/decline/cancel; false is the legacy `execCommandApproval` /
	// `applyPatchApproval` pair, answered with approved/denied. Decided when
	// the request arrives, off its method name, so the answer never has to
	// re-derive it.
	v2 bool
}

// request builds one JSON-RPC request and remembers what it was for.
// The caller must hold the mutex.
func (a *appServerProtocol) request(method string, params any) []byte {
	a.nextID++
	id := a.nextID
	a.awaiting[id] = method
	line, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method, "params": params,
	})
	if err != nil {
		// Marshalling values the room already holds cannot fail in practice;
		// returning nothing rather than a broken line keeps a corrupt object off
		// the vendor's stdin if it ever does. acpProtocol's request does the
		// same, for the same reason.
		return nil
	}
	return line
}

func appServerNotify(method string, params any) []byte {
	line, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "method": method, "params": params,
	})
	return line
}

// AppServerClientName is what this client calls itself in the handshake.
//
// It is not decoration: the vendor echoes it into the `userAgent` on the
// initialize response, and that string reaches the vendor's own telemetry and
// its rollout files. Naming the room honestly is the same rule the read/write
// boundary is written under — council does not pretend to be a different
// program.
const AppServerClientName = "telltale-council"

// Opening is the handshake's first step and only its first step.
//
// The rest is sequenced by the RESPONSES rather than pipelined, for
// acpProtocol's argument: `thread/start` needs nothing from initialize's
// result, but issuing both at once would mean a failed initialize is followed
// by a thread request against a server that has already said no.
//
// `initialize` here carries a `clientInfo` and no capabilities. Both halves are
// measured: the schema declares `capabilities` optional, the live handshake was
// driven WITHOUT it and answered normally, and the response carried no
// capability block of the server's own to negotiate against. Declaring a
// capability this program does not implement is the offer-it-cannot-honour that
// the ACP seat's `fs` block already refuses.
func (a *appServerProtocol) Opening() [][]byte {
	a.mu.Lock()
	defer a.mu.Unlock()
	return [][]byte{a.request("initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    AppServerClientName,
			"title":   "telltale council",
			"version": "0.0.0",
		},
	})}
}

// appServerSandbox is the sandbox this posture asks `thread/start` for.
//
// The values are the vendor's own `SandboxMode` enum, read from the schema this
// build generates and confirmed live on all three: `read-only`,
// `workspace-write`, `danger-full-access`. They are spelled identically to the
// `-s` values codex.go passes, and that is a coincidence of naming rather than
// a shared mechanism — this is a JSON field on a thread, not a first-turn-only
// flag, so unlike `codex exec` it is carried on `thread/resume` too and there
// is no resume-rejects-the-flag trap to route around here.
//
// WHAT WAS MEASURED, 2026-08-29, codex-cli 0.149.1, Windows 11, one turn per
// probe in throwaway directories, files checked on disk rather than read out of
// the reply (design.md §9.50):
//
//   - `read-only` ENFORCES when a process actually starts. Told to invoke
//     `cmd.exe` directly, the seat ran it and the write came back
//     `Access is denied.`, exit 1, `status: "failed"`, and no file on disk.
//     The listing call in the same turn failed on the vendor's own quoting,
//     which is itself the evidence that cmd.exe RAN inside the sandbox: its
//     error is cmd.exe's `is not recognized as an internal or external
//     command`, not a spawn refusal.
//   - `workspace-write` LANDS a write inside the workspace: exit 0 and the file
//     on disk.
//   - `workspace-write` DENIES `.git`. Two independent turns asked for
//     `.git\probe.txt`; both came back `Access is denied.`, exit 1, no file.
//     So the carve-out codex.go's gitWritableOverride exists to buy back is
//     present on this path too.
//   - THE LIVENESS DEFECT, and it is why this seat is not registered. This
//     protocol's tool router wraps a shell command in
//     `"…\WindowsApps\pwsh.exe" -NoProfile -Command "…"`, and pwsh cannot
//     start under the Windows sandbox on this box — every attempt failed
//     `CreateProcessAsUserW failed: 5 (Access is denied.)`. In TWO of three
//     read-posture arms the model then abandoned the turn and reported it
//     could not inspect. #311 measured `codex exec` retrying through cmd.exe
//     and obeying the sandbox; here the retry is the model's own move and it
//     did not always make it. A read seat that cannot list a directory is the
//     0.146.0-class defect on a new path.
//
// TWO THINGS ARE NOT MEASURED, and they are stated rather than inferred from
// the `exec` path's answers, because the whole reason this section exists is
// that the `exec` findings do not port:
//
//   - **Writing outside the workspace.** The one arm that tried it wrote into a
//     directory under `%TEMP%`, which `workspaceWrite` permits by default
//     (`excludeTmpdirEnvVar` defaults false). The write landed and the arm
//     proves NOTHING about outside-the-workspace enforcement. It is recorded
//     as void rather than as a permit.
//   - **The `writableRoots` override on `.git`.** A per-turn `sandboxPolicy`
//     naming `.git` was sent and the model's own shell quoting broke the call
//     before the sandbox ever saw it. So whether this path can buy `.git` back
//     — which `codex exec` measurably cannot on Windows (#311) — is open.
func appServerSandbox(p Posture) string {
	if p == PostureRead {
		return sandboxMode
	}
	// Write postures ask for the workspace boundary, never danger-full-access.
	//
	// This deliberately does NOT copy codex.go's Windows branch. That branch
	// exists because `exec`'s `.git` override was measured failing on Windows,
	// leaving a workspace-write seat able to edit and never commit; the
	// equivalent override on THIS path is unmeasured (see above), so choosing
	// danger-full-access here would be inheriting a finding from the other
	// surface, which is exactly what STATE.md rules against. The narrower
	// request is the one that can be defended at this build. Now that the seat
	// IS registered (2026-09-02), the consequence is stated rather than hidden:
	// a write-posture codex seat on Windows may be able to edit and not commit
	// until the `.git` override is measured on this path — and when it asks to
	// escalate, the room's approval card is where that shows up.
	return writeSandboxMode
}

// appServerApprovalPolicy is the `approvalPolicy` this posture asks
// `thread/start` for.
//
// SCHEMA READ, NOT DRIVEN. The values are the v2 `AskForApproval` enum read
// from codex-rs/app-server-protocol/src/protocol/v2/shared.rs on the main
// branch on 2026-09-02 (`#[serde(rename_all = "kebab-case")]`, with
// `UnlessTrusted` renamed to `untrusted`): `untrusted`, `on-request`, `never`,
// and an experimental `granular`. The measured arms of 2026-08-29 sent NO
// approvalPolicy at all and produced no approval request; nothing below has
// been observed changing that.
//
//   - Read posture: `never`. Nobody is there to ask, a read seat asking to
//     change something is already answered (serverRequest refuses one anyway),
//     and `codex exec` — the measured fallback — runs non-interactively under
//     the same word.
//   - Write postures: `on-request`, "the model decides when to ask". This is
//     the codex default for an interactive session and the narrower of the two
//     asking policies: the workspace-write sandbox stays the containment, and
//     the vendor asks when it wants MORE than the sandbox allows — a path
//     outside the workspace, the network — which is exactly the question the
//     room's card should carry. `untrusted` was considered and NOT chosen: it
//     asks about every command off the vendor's trusted list, and the docs
//     read for this file do not say whether a command approved under it then
//     runs outside the sandbox. A policy that might trade the sandbox for a
//     keystroke is not one to adopt unmeasured.
//
// The gated posture never reaches this function as itself: the room collapses
// PostureWriteGated to PostureWrite at spawn for every Conversational seat
// (persistent.go's spawnPosture), because whether a request becomes a card or
// an automatic yes is the room's decision when one arrives.
func appServerApprovalPolicy(p Posture) string {
	if p == PostureRead {
		return "never"
	}
	return "on-request"
}

// ErrAppServerHandshakeFailed is returned by Turn once the handshake has failed.
//
// Refusing is the whole point: this process is up and useless, and the caller's
// remedy is to kill it and let the next brief start a fresh one. It matters more
// here than on the ACP seat because this process was measured surviving a
// stdin close by 15 s, so nothing else would stop it.
var ErrAppServerHandshakeFailed = errors.New(
	"vendors: this codex app-server seat's handshake failed; its process cannot take a turn")

// ErrAppServerTurnNotStarted is returned by Interrupt when there is no
// outstanding turn for the vendor to abandon.
//
// An error rather than a silent success, for acpProtocol's reason: a caller
// that reads a clean return as "the cancel was delivered" would stop, and
// nothing would then end the turn. An error falls through to the kill.
var ErrAppServerTurnNotStarted = errors.New(
	"vendors: this codex app-server seat has no turn in flight to interrupt")

// ErrAppServerUnknownRequest is returned when a decision names a request this
// protocol is not holding.
var ErrAppServerUnknownRequest = errors.New("vendors: no such approval request is outstanding")

// Turn takes one turn, queueing it until the thread exists.
func (a *appServerProtocol) Turn(prompt string) ([][]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dead {
		return nil, ErrAppServerHandshakeFailed
	}
	if !a.ready {
		// Held rather than refused: the room has already told the user this seat
		// is working, and a turn dropped here would be a column that sat out a
		// brief it was addressed in.
		a.queued = append(a.queued, prompt)
		return nil, nil
	}
	return [][]byte{a.turnLine(prompt)}, nil
}

// turnLine encodes one turn. The caller must hold the mutex.
//
// The prompt is a content-block ARRAY, not a string, and it is marshalled
// rather than assembled for the reason every seat in this package builds its
// envelope that way: by the time anyone uses this seriously the brief contains
// quotes and newlines, and string building produces a broken line rather than a
// wrong one. No shell and no argv parser ever sees it, which retires the
// cmd.exe shim question codex.go has to route around on the `exec` path.
func (a *appServerProtocol) turnLine(prompt string) []byte {
	return a.request("turn/start", map[string]any{
		"threadId": a.threadID,
		"input":    []map[string]any{{"type": "text", "text": prompt}},
	})
}

// Interrupt abandons the turn in flight WITHOUT killing the process.
//
// SCHEMA READ at codex-cli 0.149.1, and NOT DRIVEN — no probe interrupted a
// turn, so nothing here is a claim about what the vendor does with it. The
// schema declares `turn/interrupt` a client request taking `{threadId, turnId}`,
// both required, which is why turnID is captured off `turn/started` at all:
// the room's own bookkeeping cannot name the turn the vendor is running.
//
// The honest consequence is that a caller must not treat a clean return here as
// a delivered cancel. It is a request that was sent, and the turn ends when the
// vendor says so or when the caller kills the process — the same fallback the
// ACP seat documents for a cancel it DID measure.
func (a *appServerProtocol) Interrupt(string) ([][]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Anything still waiting on the handshake is dropped whatever else happens.
	// A cancelled brief that arrives anyway is the room doing something the user
	// just stopped.
	held := len(a.queued) > 0
	a.queued = nil

	if a.threadID == "" || a.turnID == "" || held {
		return nil, ErrAppServerTurnNotStarted
	}
	// Anything the vendor is BLOCKED on is answered first, and the ordering is
	// acpProtocol's, for its reason: a pending approval holds the vendor still
	// until it is answered, and an interrupt that reached a server waiting on
	// a question nobody will now answer is an interrupt nobody has measured
	// landing. The answer is the v2 `cancel` — the decision the schema
	// describes as declining AND interrupting the turn — so on that
	// generation the cancel and the interrupt say the same thing twice, which
	// is cheaper than saying it once to the wrong listener.
	lines := a.cancelHeld()
	return append(lines, a.request("turn/interrupt", map[string]any{
		"threadId": a.threadID,
		"turnId":   a.turnID,
	})), nil
}

// cancelHeld answers every approval still open with a cancel and forgets it.
// The caller must hold the mutex.
func (a *appServerProtocol) cancelHeld() [][]byte {
	var lines [][]byte
	for key, p := range a.pending {
		lines = append(lines, appServerDecision(p, appServerCancel))
		delete(a.pending, key)
	}
	return lines
}

// Closing is what the room writes before it closes this process's stdin at
// teardown (vendors.GracefulStop), and it is the first of the three steps that
// make this seat own its own kill.
//
// THE MEASUREMENT IT ANSWERS is §9.50's: closing stdin did not reliably stop
// this server at 0.149.1 — four runs exited in 1.5–3.3 s and one was still
// alive 15 s later. So teardown is: this (an interrupt for a turn still open,
// a cancel for any approval still held), then the pipe closes, then Grace
// passes, then the runner's kill — the same job-object kill every seat gets,
// reused rather than rewritten. Nothing is written for an idle thread: an
// interrupt names a turn id, and there is none to name.
//
// Whether `turn/interrupt` before the close shortens the exit is UNMEASURED;
// the interrupt itself is a schema read (see Interrupt). What is measured is
// only that the kill is needed, and the kill is still there.
func (a *appServerProtocol) Closing() [][]byte {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.queued = nil
	if a.threadID == "" {
		return nil
	}
	lines := a.cancelHeld()
	if a.turnID != "" {
		lines = append(lines, a.request("turn/interrupt", map[string]any{
			"threadId": a.threadID,
			"turnId":   a.turnID,
		}))
	}
	return lines
}

// Grace is how long the room waits after closing stdin before the kill.
//
// Four seconds, and the number comes from the capture rather than from taste:
// the four runs that DID exit on a closed pipe took 1.5–3.3 s (§9.50), so a
// bound just past that lets the ordinary case end on its own and spends the
// kill on the case that was measured needing it — the one still alive at 15 s.
// MEASURED 2026-09-05 at codex-cli 0.151.0 (design.md §9.57): after `turn/interrupt`
// completed and stdin closed, the process exited after 6.79, 1.76, 6.69, 7.77
// and 6.42 s — four of five runs over this grace. The value is left as it is
// on purpose: raising it to fit would hide a teardown nobody has diagnosed
// (the `codex.cmd` shim's cmd.exe and node's own exit are both in the path).
// The fix is routed to its own change; until it lands, the reaper ends this
// seat most of the time, and that is the measured truth of this number.
func (a *appServerProtocol) Grace() time.Duration { return 4 * time.Second }

// Dead reports the terminal handshake state, for the room's fallback decision
// (vendors.LiveFallback). It is the flag Turn refuses on; exposing it lets the
// room ask before it spends a brief on a process that cannot take one.
func (a *appServerProtocol) Dead() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dead
}

// Decide answers one approval the room was handed.
//
// UNMEASURED IN BOTH DIRECTIONS. No arm has produced an approval request on
// this path — the measured arms sent no approvalPolicy — so nothing here is a
// claim that the vendor blocks until answered, runs on accept, or stops on
// decline. What IS pinned is the wire: the response is `{"decision": …}` with
// the v2 enum, read from the app-server README and from v2/item.rs on
// 2026-09-02 (see appServerDecision), and the request is forgotten once
// answered so a later keystroke cannot answer it twice.
//
// input and reason are unused, and the asymmetry is worth naming because it is a
// real difference in what seats can promise. Claude's protocol requires the
// tool's whole argument blob echoed back on an approval, and its denial carries
// council's own sentence to the model. This schema's approval response is a
// `decision` string and nothing else, so this seat can neither echo nor
// explain — a declined command is declined without the model being told who
// said no, which is the retry-a-variation risk denialText exists to close on
// the Claude seat and cannot close here.
func (a *appServerProtocol) Decide(requestID string, allow bool, _ string, _ map[string]any) ([][]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.pending[requestID]
	if !ok {
		return nil, ErrAppServerUnknownRequest
	}
	delete(a.pending, requestID)
	verdict := appServerDecline
	if allow {
		verdict = appServerAccept
	}
	return [][]byte{appServerDecision(p, verdict)}, nil
}

// The three answers this seat gives, and the two vocabularies it gives them
// in.
//
// v2 (`item/commandExecution/requestApproval`, `item/fileChange/requestApproval`,
// `item/permissions/requestApproval`): `accept`, `decline`, `cancel` — READ
// from codex-rs/app-server-protocol/src/protocol/v2/item.rs on 2026-09-02
// (`CommandExecutionApprovalDecision` and `FileChangeApprovalDecision`, both
// `rename_all = "camelCase"`) and from the app-server README's approval
// sections, which show `{ "decision": "accept" }`, `"decline"` and
// `"cancel"` verbatim. `acceptForSession` exists and is NEVER sent, for the
// reason the ACP seat never sends `allow-always`: it widens what the agent may
// do without being asked again, and the room asks per call.
//
// v1 (`execCommandApproval`, `applyPatchApproval`): `approved`, `denied` — the
// schema read at 0.149.1 this file has carried since 2026-08-29. Kept because
// the method names are still in the schema and a request under one of them is
// still a vendor blocked on it.
type appServerVerdict uint8

const (
	appServerAccept appServerVerdict = iota
	appServerDecline
	appServerCancel
)

// appServerDecision is the response the vendor is blocked on.
//
// The words are written here rather than remembered per request for
// acpDecision's reason: a shape that changed would leave the room echoing a
// value it did not understand, and a wrong value is refused visibly where a
// remembered-but-stale one is answered as though it meant something.
func appServerDecision(p appServerPending, v appServerVerdict) []byte {
	var decision string
	switch {
	case p.v2 && v == appServerAccept:
		decision = "accept"
	case p.v2 && v == appServerDecline:
		decision = "decline"
	case p.v2:
		decision = "cancel"
	case v == appServerAccept:
		decision = "approved"
	default:
		// v1 has no cancel; a denial is the nearest thing that stops the call.
		decision = "denied"
	}
	line, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      append(json.RawMessage(nil), p.id...),
		"result":  map[string]any{"decision": decision},
	})
	return line
}

func appServerEmptyResult(id json.RawMessage) []byte {
	line, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": append(json.RawMessage(nil), id...),
		"result": map[string]any{},
	})
	return line
}

// Inbound is the whole state machine: one line in, events for the room out, and
// whatever has to be said back.
func (a *appServerProtocol) Inbound(line []byte) ([]runner.Event, [][]byte) {
	if len(line) == 0 || line[0] != '{' {
		// The server was observed writing ordinary log lines to STDERR, never to
		// stdout, so anything here that is not an object is noise rather than a
		// frame worth guessing at.
		return nil, nil
	}
	var msg appServerLine
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

// appServerApprovals are the server requests that ask the room to decide, and
// which vocabulary answers each (see appServerVerdict).
//
// SCHEMA READ — the v1 pair at codex-cli 0.149.1 from `ServerRequest.json`,
// the v2 trio re-read from the app-server README on 2026-09-02 — and none was
// observed live. Listed by name rather than matched by prefix because the same
// schema declares requests that are not approvals at all —
// `attestation/generate`, `mcpServer/elicitation/request`, `item/tool/call`,
// `item/tool/requestUserInput` — and answering one of those with a decision
// would be council inventing a side of a protocol it has not read.
var appServerApprovals = map[string]bool{
	"execCommandApproval":                   false,
	"applyPatchApproval":                    false,
	"item/commandExecution/requestApproval": true,
	"item/fileChange/requestApproval":       true,
	"item/permissions/requestApproval":      true,
}

// appServerApproval is the ALLOWLIST of what council reads out of an approval
// request, in the internal/cursorhook sense: encoding/json drops every field
// with no destination, and these are the only destinations.
//
// Field names are the v2 params read from v2/item.rs on 2026-09-02
// (`CommandExecutionRequestApprovalParams`: threadId, turnId, itemId, kind,
// approvalId, environmentId, reason, command, cwd, …;
// `FileChangeRequestApprovalParams`: threadId, turnId, itemId, reason,
// grantRoot). `command` and `reason` are the two that name the action for a
// card; everything else the request carries — `commandActions`,
// `additionalPermissions`, `networkApprovalContext`, the v1 shapes' own
// fields — has no field here and never reaches memory the room can render.
// The v1 pair is read through the same struct on a best-effort basis: its
// `command` was an array in the 0.149.1 schema and unmarshals to nothing here,
// so a v1 card names the method and no argument, which is less than it could
// say and nothing it cannot back.
type appServerApproval struct {
	ItemID  string `json:"itemId"`
	Command string `json:"command"`
	Reason  string `json:"reason"`
}

// serverRequest answers a question the vendor is blocked on.
//
// EVERY server request is answered, including one this adapter does not
// recognise, and that is the ACP seat's hardest-won lesson carried across
// unchanged: a request left unanswered blocks the vendor forever, which on a
// persistent seat is a column that never finishes and a room that never lets go
// of the turn. An empty object is the smallest well-formed thing that unblocks a
// request whose result shape is unknown.
//
// An approval goes one of two ways, on the posture, exactly as acpProtocol's
// does:
//
//   - A READ posture declines it on arrival and records the attempt in the
//     trace. A read-posture seat asking to change something is not a question
//     for the user; it is already answered, and raising a card would offer
//     authority this posture withheld. It asked for `never` anyway
//     (appServerApprovalPolicy), so a request arriving at all is the vendor
//     asking about something the room offered it no authority over.
//   - A WRITE posture holds it open and hands the room a Gate. The vendor is
//     BLOCKED until Decide answers, which is the whole value of the card, and
//     the reason nothing is written back here.
//
// Both branches are UNMEASURED on this path — no arm produced a request — and
// the write branch is the one that has to be watched first on a live run: a
// vendor that did not actually block would run the command while the card was
// still up.
func (a *appServerProtocol) serverRequest(msg appServerLine) ([]runner.Event, [][]byte) {
	v2, isApproval := appServerApprovals[msg.Method]
	if !isApproval {
		return nil, [][]byte{appServerEmptyResult(msg.ID)}
	}
	var req appServerApproval
	_ = json.Unmarshal(msg.Params, &req)
	pending := appServerPending{id: append(json.RawMessage(nil), msg.ID...), v2: v2}

	a.mu.Lock()
	read := a.posture == PostureRead
	a.approvalSeq++
	key := "app-server-approval-" + strconv.Itoa(a.approvalSeq)
	if !read {
		a.pending[key] = pending
	}
	a.mu.Unlock()

	tool, text := appServerApprovalText(msg.Method, req)
	if read {
		return []runner.Event{{
			Kind: runner.KindActivity,
			Acts: []runner.ActCall{{
				ID:      key,
				Text:    text,
				Outcome: runner.ActFailed,
				Detail:  "refused: this seat is read-only",
			}},
		}}, [][]byte{appServerDecision(pending, appServerDecline)}
	}
	return []runner.Event{{
		Kind: runner.KindGate,
		Gate: &runner.Gate{
			RequestID: key,
			ToolUseID: req.ItemID,
			Tool:      tool,
			Text:      text,
			// Nil, and it must stay nil, for the ACP seat's reason: the answer
			// here is a decision word, and carrying an argument blob nothing
			// sends back would be a copy of the request held for no purpose.
			// OldContent/NewContent are empty too, and the consequence is
			// stated: a fileChange card names the reason the vendor gave and
			// draws NO before/after preview (§9.41). The request carries no
			// halves of an edit — the diff is on the `item/started` that
			// precedes it, which this adapter does not hold — so there is
			// nothing measured to draw.
		},
	}}, nil
}

// appServerApprovalText names one approval for a card and for the trace, in
// the grammar every seat uses: the kind of thing, then ": " and the one
// argument that identifies it. The kind is the method's own middle segment
// (`commandExecution`, `fileChange`, `permissions`) or, for the v1 pair, the
// whole method name.
func appServerApprovalText(method string, req appServerApproval) (tool, text string) {
	tool = method
	if parts := strings.Split(method, "/"); len(parts) == 3 {
		tool = parts[1]
	}
	arg := req.Command
	if arg == "" {
		arg = req.Reason
	}
	if arg = strings.TrimSpace(arg); arg != "" {
		return tool, tool + ": " + clipArg(arg)
	}
	return tool, tool
}

// notification handles the vendor's streaming surface.
func (a *appServerProtocol) notification(msg appServerLine) ([]runner.Event, [][]byte) {
	switch msg.Method {
	case "turn/started":
		var p struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		if json.Unmarshal(msg.Params, &p) == nil && p.Turn.ID != "" {
			a.mu.Lock()
			a.turnID = p.Turn.ID
			a.mu.Unlock()
		}

	case "item/agentMessage/delta":
		// The vendor speaking, incrementally. Deltas concatenate exactly as sent
		// — measured against the completed item on every capture — so no
		// separator is added here. A separator this adapter invented would be a
		// character the vendor did not write.
		var p struct {
			ItemID string `json:"itemId"`
			Delta  string `json:"delta"`
		}
		if json.Unmarshal(msg.Params, &p) != nil || p.Delta == "" {
			return nil, nil
		}
		a.mu.Lock()
		a.seenDeltas[p.ItemID] = true
		a.mu.Unlock()
		return []runner.Event{{Kind: runner.KindText, Text: p.Delta}}, nil

	case "item/started", "item/completed":
		return a.item(msg)

	case "turn/completed":
		return a.turnCompleted(msg)

	case "thread/tokenUsage/updated":
		var p struct {
			TokenUsage *appServerTokenUsage `json:"tokenUsage"`
		}
		if json.Unmarshal(msg.Params, &p) == nil && p.TokenUsage != nil {
			a.mu.Lock()
			a.usage = p.TokenUsage
			a.mu.Unlock()
		}

	case "account/rateLimits/updated":
		var p struct {
			RateLimits *appServerRateLimits `json:"rateLimits"`
		}
		if json.Unmarshal(msg.Params, &p) == nil && p.RateLimits != nil {
			a.mu.Lock()
			a.limits = p.RateLimits
			a.mu.Unlock()
		}

	case "error":
		// The server's own out-of-band failure notification. It carries no turn
		// id and was never observed, so it ends the turn rather than being
		// correlated to one: a room told something failed and shown nothing is
		// worse than a room that stops.
		var p struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		note := "the vendor reported an error"
		if p.Message != "" {
			note = "the vendor reported an error: " + p.Message
		}
		return []runner.Event{{Kind: runner.KindError, EndsTurn: true, Note: note}}, nil
	}

	// Everything else is chrome for an interactive client, and the list is long
	// on this protocol: `mcpServer/startupStatus/updated` (five servers on this
	// box, each fired twice), `thread/status/changed`, `remoteControl/status/changed`,
	// `hook/started` and `hook/completed`, `thread/started`, `model/rerouted`.
	// Hooks are the interesting absence — they are the thing `codex exec --json`
	// HIDES and this surface carries — and they are still dropped, because a
	// hook is neither the vendor's answer nor a thing the vendor DID on the
	// room's behalf, and the one captured hook was the OPERATOR's own
	// `sessionStart` script. Rendering somebody's local configuration as
	// council activity is the machine-specific claim §9.33 already warned this
	// figure must never become.
	return nil, nil
}

// item turns one item/started or item/completed into an event.
func (a *appServerProtocol) item(msg appServerLine) ([]runner.Event, [][]byte) {
	var p struct {
		Item appServerItem `json:"item"`
	}
	if json.Unmarshal(msg.Params, &p) != nil {
		return nil, nil
	}
	it := p.Item
	completed := msg.Method == "item/completed"

	switch it.Type {
	case "agentMessage":
		if !completed {
			// The announcement carries `"text":""`. Nothing to say yet, and the
			// deltas are what fill the column.
			return nil, nil
		}
		a.mu.Lock()
		streamed := a.seenDeltas[it.ID]
		delete(a.seenDeltas, it.ID)
		a.mu.Unlock()
		if streamed {
			// THE WHOLE-MESSAGE REPEAT, measured on this wire. The text is
			// already in the column, delta by delta. What is still owed is the
			// BOUNDARY: a turn carries several complete messages, and appending
			// the next one's first delta to this one's last would run two
			// sentences together. Same argument codex.go's trailing newline
			// rests on, arriving here as a separator with no text.
			return []runner.Event{{Kind: runner.KindText, Text: "\n"}}, nil
		}
		if it.Text == "" {
			return nil, nil
		}
		// No deltas were seen for this item. Either the room attached mid-message
		// or a future build stopped streaming; the completed item is then the
		// only copy, and printing it is the safety net the ACP seat explicitly
		// does not have.
		return []runner.Event{{Kind: runner.KindText, Text: it.Text + "\n"}}, nil

	case "commandExecution":
		act := runner.ActCall{ID: it.ID, Text: appServerCommandText(it)}
		if !completed {
			// Announced. ActPending is what makes a running command visible
			// before it resolves — the same reason codex.go stopped dropping
			// `item.started`, where a long build showed nothing at all while it
			// ran.
			act.Outcome = runner.ActPending
			return []runner.Event{{Kind: runner.KindActivity, Acts: []runner.ActCall{act}}}, nil
		}
		act.Outcome = appServerOutcome(it.Status, it.ExitCode)
		if act.Outcome == runner.ActFailed {
			act.Detail = clipArg(firstLine(it.AggregatedOutput))
		}
		return []runner.Event{{Kind: runner.KindActivity, Acts: []runner.ActCall{act}}}, nil
	}

	// `userMessage` is council's OWN brief coming back, and rendering it would
	// put the prompt into the column as though the vendor had said it.
	// `reasoning` is the model thinking rather than acting, dropped on the
	// policy every seat in this package drops thinking under. Anything else is
	// an item type no arm produced, and an unrecognised type is dropped rather
	// than guessed at — a future codex release cannot invent activity here.
	return nil, nil
}

// appServerCommandText names one command for a narrow column. Clipped, because
// a generated patch or a heredoc can run to thousands of characters and a trace
// that scrolls the answer off screen has defeated its own purpose.
func appServerCommandText(it appServerItem) string {
	if it.Command != "" {
		return clipArg(it.Command)
	}
	return it.Type
}

// appServerOutcome maps a completed item to what is actually known about it.
//
// exitCode first, because it is the process's own answer, and it was captured
// live on both branches: `0` on the writes that landed and `1` on every
// `Access is denied.`
//
// The status fallback is WIDER here than codex.go's, and the difference is a
// measurement rather than a taste. On this wire `status: "failed"` was captured
// on a command that never started at all — where `exitCode` is null and there is
// no process answer to read — and `status: "completed"` was captured only ever
// beside an exit code that already settled it. So `failed` is load-bearing and
// `completed` is not, which is why one is mapped and the other is not: a
// success claim built on a string, on an item type that carries no exit code,
// is the move that put a read-only badge on a session that could write (§9.2).
func appServerOutcome(status string, exitCode *int) runner.ActStatus {
	if exitCode != nil {
		if *exitCode == 0 {
			return runner.ActOK
		}
		return runner.ActFailed
	}
	if status == "failed" {
		return runner.ActFailed
	}
	return runner.ActUnknown
}

// turnCompleted reads the notification a turn ends with.
//
// CAPTURED whole, on every arm:
//
//	{"method":"turn/completed","params":{"threadId":"…","turn":{"id":"…","items":[…],
//	 "itemsView":"summary","status":"completed","error":null,"startedAt":…,
//	 "completedAt":…,"durationMs":32622}}}
//
// `items` repeats the turn's final items, and it is DROPPED whole. Reading it
// would print the answer a third time, after the deltas and after the completed
// item — the same repeat trap one level up.
//
// EndsTurn is set and nothing is killed. This process is meant to stay up, so
// unlike the `exec` seat there is no linger to settle around: `codex exec`
// prints its last line and then holds the process open ~4 s (§9.33's
// amendment), where here the next turn starts immediately — turn 2 of the warm
// arm was sent on the same millisecond this notification landed. The one
// shutdown fact worth carrying is the opposite one: closing stdin does NOT
// reliably stop this server. Four probe runs exited in 1.5–3.3 s and one was
// still alive 15 s later and had to be killed, so the caller must own the kill.
func (a *appServerProtocol) turnCompleted(msg appServerLine) ([]runner.Event, [][]byte) {
	var p struct {
		Turn struct {
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(msg.Params, &p)

	a.mu.Lock()
	a.turnID = ""
	a.mu.Unlock()

	if p.Turn.Error != nil && p.Turn.Error.Message != "" {
		return []runner.Event{{
			Kind: runner.KindError, EndsTurn: true,
			Note: "the vendor reported the turn failed: " + p.Turn.Error.Message,
		}}, nil
	}
	switch p.Turn.Status {
	case "completed", "":
		// `completed` is the only status any arm produced. An EMPTY status is
		// treated as a clean end rather than as a failure, for the ACP seat's
		// reason: a turn that produced a whole reply and then resolved without
		// saying why is not evidence of a failure.
		return []runner.Event{{Kind: runner.KindMeta, EndsTurn: true}}, nil
	case "interrupted":
		// DOC READ, 2026-09-02, app-server README: "the turn finishes with
		// status: "interrupted"" after `turn/interrupt`, and `turn.status` is
		// one of `completed`, `interrupted`, `failed`. It is the user's own
		// keystroke coming back, on the ACP seat's `cancelled` precedent, and
		// finishColumn's cancellation check is what words it. Never observed
		// here, which is why the word is matched exactly and nothing near it
		// is.
		return []runner.Event{{Kind: runner.KindMeta, EndsTurn: true}}, nil
	}
	// The schema declares more values than any arm produced. Reported as a
	// failure in the vendor's own word rather than rendered as a normal answer,
	// and the word is quoted rather than translated because this adapter has
	// never seen one and has no business paraphrasing it.
	return []runner.Event{{
		Kind: runner.KindError, EndsTurn: true,
		Note: "the vendor stopped this turn: " + p.Turn.Status,
	}}, nil
}

// response routes an answer to one of our own requests.
func (a *appServerProtocol) response(msg appServerLine) ([]runner.Event, [][]byte) {
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
			ev := a.fail("the vendor refused the app-server handshake: " + msg.Error.text())
			a.mu.Unlock()
			return ev, nil
		}
		// `initialized` is a NOTIFICATION and it precedes the first real
		// request. Sent in the same batch as thread/start rather than waiting
		// for a response, because a notification has none to wait for.
		out := [][]byte{appServerNotify("initialized", map[string]any{}), a.openThread()}
		a.mu.Unlock()
		return nil, out

	case "thread/resume":
		if msg.Error != nil {
			// The saved thread is gone. UNMEASURED on this path — no arm resumed
			// a thread id the server did not hold — so what follows is the
			// one-attempt rule applied rather than a captured behaviour: the id
			// is spent, a NEW thread opens in the same process, and
			// settleRestoredThread hears about it through the ordinary turn.
			// The alternative, failing the seat, would cost a brief over a
			// recovery that costs one round trip.
			a.resumeID = ""
			out := a.openThread()
			a.mu.Unlock()
			return nil, [][]byte{out}
		}
		return a.threadOpened(msg)

	case "thread/start":
		if msg.Error != nil {
			ev := a.fail("the vendor would not open a thread: " + msg.Error.text())
			a.mu.Unlock()
			return ev, nil
		}
		return a.threadOpened(msg)

	case "turn/start":
		// The turn's END is `turn/completed`, not this response: MEASURED, the
		// response lands within ~200 ms of the request and the turn runs for
		// tens of seconds afterwards. So this only carries a failure.
		if msg.Error != nil {
			a.mu.Unlock()
			return []runner.Event{{
				Kind: runner.KindError, EndsTurn: true,
				Note: "the vendor refused the turn: " + msg.Error.text(),
			}}, nil
		}
	}
	a.mu.Unlock()
	return nil, nil
}

// openThread is the request that follows a successful initialize. The caller
// must hold the mutex.
func (a *appServerProtocol) openThread() []byte {
	params := map[string]any{
		"sandbox":        appServerSandbox(a.posture),
		"approvalPolicy": appServerApprovalPolicy(a.posture),
	}
	if a.workspace != "" {
		// `cwd` is a THREAD parameter here, where `codex exec` takes it as a
		// first-turn-only `--cd` flag that resume rejects. That is the single
		// biggest shape difference between the two surfaces, and it is why this
		// adapter needs no resume-override machinery at all.
		params["cwd"] = a.workspace
	}
	if a.resumeID != "" {
		params["threadId"] = a.resumeID
		return a.request("thread/resume", params)
	}
	return a.request("thread/start", params)
}

// threadOpened is the common tail of thread/start and thread/resume. The caller
// must hold the mutex; this releases it.
//
// The id is read from `result.thread.id`. The same object also carries a
// `sessionId` that was IDENTICAL to it on every capture, and the two are not
// collapsed here: `id` is the field `turn/start` and `turn/interrupt` name, so
// it is the one to carry, and an equality observed on one build is not a reason
// to depend on it.
func (a *appServerProtocol) threadOpened(msg appServerLine) ([]runner.Event, [][]byte) {
	var res struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	_ = json.Unmarshal(msg.Result, &res)
	if res.Thread.ID == "" {
		ev := a.fail("the vendor opened a thread and did not name it")
		a.mu.Unlock()
		return ev, nil
	}
	a.threadID = res.Thread.ID
	lines := a.flushQueued()
	a.mu.Unlock()
	return []runner.Event{{Kind: runner.KindSession, SessionID: res.Thread.ID}}, lines
}

// flushQueued opens the gate and turns whatever was waiting behind it into
// lines. The caller must hold the mutex.
//
// Setting ready HERE rather than at the call site is acpProtocol's discipline,
// kept even though this protocol has exactly one flush point today: the moment
// a second posture channel needs its own round trip, the flag and the flush
// stay tied together instead of drifting apart.
func (a *appServerProtocol) flushQueued() [][]byte {
	a.ready = true
	if len(a.queued) == 0 {
		return nil
	}
	var out [][]byte
	for _, p := range a.queued {
		out = append(out, a.turnLine(p))
	}
	a.queued = nil
	return out
}

// fail puts the protocol into its terminal state and reports it. The caller
// must hold the mutex.
func (a *appServerProtocol) fail(note string) []runner.Event {
	a.dead = true
	a.queued = nil
	return []runner.Event{{Kind: runner.KindError, EndsTurn: true, Note: note}}
}

// CodexAppServer drives the Codex CLI as ONE LIVE `codex app-server` process,
// and since 2026-09-02 it is the seat Registry() maps `model.VendorCodex` to.
//
// It was a second, unseated adapter from 2026-08-29 to 2026-09-02, on the
// measurement appServerSandbox records — a read-posture seat that abandoned its
// turn rather than inspecting on two of three arms. The file header says what
// moved it and what the move still owes; the short form is that the exec
// adapter is now the FALLBACK rather than the seat, the seat owns its own kill,
// and every badge on this column says "unmeasured at 0.152.1" until the read
// posture's liveness has been driven on this path at that build.
type CodexAppServer struct{}

var (
	_ Vendor         = CodexAppServer{}
	_ Conversational = CodexAppServer{}
	_ LiveFallback   = CodexAppServer{}
	_ GracefulStop   = (*appServerProtocol)(nil)
)

func (CodexAppServer) ID() model.VendorID { return model.VendorCodex }

// Fallback is the measured seat: `codex exec --json`, one process per turn,
// with the sandbox and resume behaviour codex.go records.
//
// WHEN the room should take it is the room's decision, made from what it can
// see; the shapes that mean "this protocol cannot be brought up" are all
// terminal in appServerProtocol and reported by Dead(): `initialize` refused
// (an error response, which is what an unauthenticated CLI or a build that
// refuses this client's version would send), `thread/start` refused (an error
// response — JSON-RPC's `-32601` for a method the build does not know arrives
// this way), or a thread opened with no id. A process that exits before
// answering `initialize` at all — a build without the subcommand — is a
// process death the room already sees, before any session id.
func (CodexAppServer) Fallback() Vendor { return Codex{} }

// ErrCodexAppServerIsLiveOnly is what the batch entry points return.
//
// They exist because Conversational embeds Vendor; they cannot DO anything
// because this seat has no spawn-per-turn invocation. A caller that wants one
// wants `Codex`, which is a different adapter for the same vendor and is the
// one the room seats.
var ErrCodexAppServerIsLiveOnly = errors.New(
	"vendors: the codex app-server seat is driven as a live process, not as one child per turn")

func (CodexAppServer) FirstTurn(_, _, _ string, _ Posture) (runner.Spec, error) {
	return runner.Spec{}, ErrCodexAppServerIsLiveOnly
}

func (CodexAppServer) NextTurn(_, _, _, _ string, _ Posture) (runner.Spec, error) {
	return runner.Spec{}, ErrCodexAppServerIsLiveOnly
}

// ParseEvent has nothing to parse: this stream is a conversation, not a
// sequence of independent lines. It survives only because Vendor requires it.
func (CodexAppServer) ParseEvent([]byte) (runner.Event, bool) { return runner.Event{}, false }

// Open builds the invocation and the protocol driver for one process.
//
// `app-server` and nothing else. Every flag the `exec` invocation carries —
// `--json`, `-s`, `--skip-git-repo-check`, `--cd`, the trailing `-` that puts
// the prompt on stdin — belongs to a surface this seat does not use. The
// posture arrives as `thread/start`'s `sandbox` and `approvalPolicy`, the
// workspace as its `cwd`, and the brief as a JSON string no shell and no argv
// parser ever sees. That last clause is also why the `codex.cmd` shim codex.go
// routes around is no concern here: runner.Start's refusal guards prompt text
// in argv, and there is none.
//
// `--skip-git-repo-check` has no counterpart and needs none: `thread/start`
// opened a thread in a throwaway directory outside any repository on every arm,
// where `codex exec` refuses to run at all without that flag.
//
// Dir is set for tidiness rather than for effect — `cwd` is what the thread
// binds to, and it was sent on every arm.
func (c CodexAppServer) Open(workspace, binary, sessionID string, p Posture) (runner.Spec, runner.Protocol, error) {
	return runner.Spec{
			Vendor: c.ID(),
			Binary: binary,
			Args:   []string{"app-server"},
			Dir:    workspace,
		},
		newAppServerProtocol(workspace, sessionID, p),
		nil
}
