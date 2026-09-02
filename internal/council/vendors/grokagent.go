package vendors

import (
	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// GrokAgent drives Grok Build as ONE LIVE ACP SERVER: `grok agent stdio`.
//
// NOTHING IN THIS FILE IS MEASURED. Every claim below is a documentation read,
// dated and cited, and the seat is registered on that reading because the
// alternative — a fifth seat that pays a whole process per brief while four
// neighbours stay warm — was measured costing 5.6 s and 6.4 s of cold start on
// the two seats that had it (design.md §9.54's motivation). The measured
// invocation, `grok --single=<prompt>` (grok.go), stays beside this one as the
// fallback the room may retreat to when the ACP handshake fails, and every
// figure that seat can show — the vendor's own `total_cost_usd` above all —
// still comes only from that seat.
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
// What is NOT read anywhere, and is therefore withheld rather than guessed:
//
//   - **Cost.** `grok --single` reports the vendor's own `total_cost_usd` on
//     its `end` frame, and that is the reason the grok column can show money
//     at all (grok.go). The ACP `session/prompt` response is declared as
//     `{stopReason, _meta?}` and no page says what, if anything, grok puts in
//     `_meta`. So on this seat COST RENDERS ABSENT — nil, never zero — until a
//     capture shows a figure the vendor itself computed. Reading a number out
//     of an unspecified extension field would be the derived-cost move §4a.1
//     forbids, one step removed.
//   - **Modes.** No page names a plan or read mode for this server, so the
//     read posture sends no `session/set_mode` and the badge stays
//     `unsandboxed`, exactly as grok.go's is. What the read posture DOES do on
//     this seat is refuse `session/request_permission` itself (acp.go's
//     read-posture branch, measured on cursor-agent), which is a containment
//     the batch seat structurally cannot offer — and is still not a badge,
//     because whether grok raises a request before a write at all is
//     unmeasured.
//   - **Permission option ids.** Picked by KIND from each request, never
//     spelled here. See acpDialect.fixedOptions.
//   - **Whether stdin close ends the process.** Grace bounds the wait; the
//     runner's kill ends it.
//
// The gate, such as it is: in a write posture a permission request becomes the
// room's ordinary approval card and the answer goes back down the same pipe,
// through Decide. That is the ACP mechanism §9.36 measured on cursor-agent, and
// it is what "the gate can ask Grok" means. It is NOT `gated`: canGate is a
// coverage measurement and this seat has none.
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
// command-line ceiling with it. What it does NOT retire is unmeasured: whether
// this server's own slash-command parser eats a brief beginning `/` the way
// the batch path was measured doing (grokSlashEaten). Nothing rewrites the
// brief either way.
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
