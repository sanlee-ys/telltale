package vendors

import (
	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// GrokAgent drives Grok Build as ONE LIVE ACP SERVER: `grok agent stdio`.
//
// MEASURED 2026-09-04 at grok 1.0.13 (5e9a58528b76), Windows 11, with the
// shared client's own frames (acp.go's header carries every arm and what it
// showed). The seat was registered two days earlier on a documentation read,
// because the alternative — a fifth seat that pays a whole process per brief
// while four neighbours stay warm — was measured costing 5.6 s and 6.4 s of
// cold start on the two seats that had it (design.md §9.57's motivation); the
// reading is kept below, dated, because it says what the seat was BUILT from,
// and the measurements say what it does. The measured batch invocation,
// `grok --single=<prompt>` (grok.go), stays beside this one as the fallback the
// room may retreat to when the ACP handshake fails, and the vendor's own
// `total_cost_usd` still comes only from that seat.
//
// What the drive settled, in one line each (acp.go's header has the frames):
//
//   - The read posture asks for `plan` (grokDialect) and the badge says
//     `ro:requested`: two write briefs under it wrote nothing, one read brief
//     under it read. `set_mode` accepts any id, so the mode's acceptance is
//     not the evidence; its effect is.
//   - A write session is NOT asked: the server resolves its own permission
//     interactions (`"yolo":true`) unless a `/always-approve off` prompt turns
//     them on, which this seat does not send. So the shape word is `unasked`,
//     and the room's card is wired for a request that will not come until
//     that changes.
//   - `session/load` resumes a saved id in a new process from the same cwd,
//     streams the history first (dropped by acp.go's replay guard), and
//     refuses a foreign cwd or an unknown id with `-32603 FS_NOT_FOUND` while
//     the process lives on.
//   - `x.ai/hooks` is the agent's on-disk hook system, not a client protocol;
//     it holds no posture for the room.
//   - Cost: `_meta.usage` carries token counts and `costUsdTicks`; the tick's
//     unit is still unmeasured against grok.com, so CostUSD stays nil.
//
// What was read, and where (all 2026-09-02):
//
//   - Grok Build 1.0.13 ships `grok agent stdio`, "runs the agent over
//     JSON-RPC on stdin/stdout" for tool integration
//     (docs.x.ai/build/cli/headless-scripting). The same page names `--cwd`
//     and `--resume` as flags of the headless CLI. Zed's registry entry launches
//     it as `npx @xai-official/grok@<version> agent stdio` with no other
//     argument (zed.dev/acp/agent/grok-build).
//   - The protocol is ACP (agentclientprotocol.com/protocol/schema): the same
//     `initialize` → `session/new` → `session/prompt` shape the Cursor seat was
//     MEASURED driving (design.md §9.36), with `session/request_permission` as
//     the server's blocking question and `session/cancel` as the client's
//     interrupt. This seat therefore reuses acp.go whole, under grokDialect,
//     which is where every difference from the measured seat is listed.
//
// What was NOT read anywhere on 2026-09-02, and was therefore withheld
// rather than guessed until the drive above answered it:
//
//   - **Cost.** `grok --single` reports the vendor's own `total_cost_usd` on
//     its `end` frame, and that is the reason the grok column can show money
//     at all (grok.go). The ACP `session/prompt` response is declared as
//     `{stopReason, _meta?}`; at 1.0.13 grok puts a `usage` block there with a
//     `costUsdTicks` figure in an unmeasured unit. So on this seat COST
//     RENDERS ABSENT — nil, never zero — until the tick is measured against
//     grok.com's own billing. Reading a number out of an extension field in a
//     unit nobody has checked would be the derived-cost move §4a.1 forbids,
//     one step removed.
//   - **Modes.** No page names one; the drive found `plan`, and the read
//     posture asks for it now (grokDialect). The read posture ALSO refuses
//     `session/request_permission` itself (acp.go's read-posture branch), a
//     second line the batch seat structurally cannot offer — and one the drive
//     showed is never reached in a default session, because the server does
//     not ask.
//   - **Permission option ids.** Picked by KIND from each request, never
//     spelled here. See acpDialect.fixedOptions.
//   - **Whether stdin close ends the process.** It does, in 2.2–4.4 s (acp.go,
//     Grace); the runner's kill still stands behind the 2 s grace.
//
// The gate, such as it is: in a write posture a permission request becomes the
// room's ordinary approval card and the answer goes back down the same pipe,
// through Decide. That is the ACP mechanism §9.36 measured on cursor-agent, and
// it is what "the gate can ask Grok" means — and it was measured working on
// grok too, once `/always-approve off` had been sent. The seat does not send
// it, so no card is drawn for this seat today. It is NOT `gated`: canGate is
// a coverage measurement, and the one measured here is that a shell command
// is never asked about even with the toggle off.
type GrokAgent struct{}

var (
	_ Vendor         = GrokAgent{}
	_ Conversational = GrokAgent{}
	_ LiveFallback   = GrokAgent{}
)

func (GrokAgent) ID() model.VendorID { return model.VendorGrok }

// Fallback is the measured seat: `grok --single=<prompt>`, one process per
// turn, the vendor's own cost on its `end` frame.
func (GrokAgent) Fallback() Vendor { return Grok{} }

// FirstTurn, NextTurn and ParseEvent are the batch adapter's, reached without
// a type switch.
//
// Unlike the Cursor seat, which refuses its batch entry points because its
// print-mode path was measured and REPLACED, this seat's batch path is the
// measured one and the live path is the reading. A caller that wants one
// process per turn — the room after a failed handshake, or a future arena arm
// — gets the invocation grok.go verified rather than an error naming a
// decision nobody has made about this vendor yet.
func (GrokAgent) FirstTurn(prompt, workspace, binary string, p Posture) (runner.Spec, error) {
	return Grok{}.FirstTurn(prompt, workspace, binary, p)
}

func (GrokAgent) NextTurn(prompt, workspace, binary, sessionID string, p Posture) (runner.Spec, error) {
	return Grok{}.NextTurn(prompt, workspace, binary, sessionID, p)
}

func (GrokAgent) ParseEvent(line []byte) (runner.Event, bool) { return Grok{}.ParseEvent(line) }

// Open builds the invocation and the protocol driver for one process.
//
// `agent stdio` and nothing else. `--cwd` is deliberately NOT passed even
// though the headless page offers it: the ACP `session/new` request carries
// the workspace as its required `cwd`, and asking for one thing on two
// channels is how a moved room ends up with a process in one directory and a
// session in another. `--resume` is not passed for the same reason — a saved
// thread is a `session/load`, sent only when the server advertises it can.
// Dir is the workspace for tidiness; on the measured ACP seat the SESSION's cwd
// is what binds, and nothing says this server differs.
//
// No StdinPrompt, and no path by which prompt text could reach argv: the brief
// is a JSON string on the pipe. That retires the `--single=` hazard grok.go
// documents (a brief beginning `---` read as a flag), and the ~32K Windows
// command-line ceiling with it. What it does NOT retire, measured 2026-09-04:
// this server eats a brief beginning `/` when the name after it is a command
// it advertised (`/hooks-list`, `/always-approve off` were handled by the
// agent with no model call), and passes one it did not advertise (`/help`)
// to the model as text. Nothing rewrites the brief either way.
func (g GrokAgent) Open(workspace, binary, sessionID string, p Posture) (runner.Spec, runner.Protocol, error) {
	return runner.Spec{
			Vendor: g.ID(),
			Binary: binary,
			Args:   []string{"agent", "stdio"},
			Dir:    workspace,
		},
		newACPProtocolWith(grokDialect, workspace, sessionID, p),
		nil
}
