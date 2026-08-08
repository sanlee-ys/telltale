package runner

// Protocol is a per-PROCESS conversation with a child that ANSWERS, rather than
// one that only streams.
//
// Everything else in this package treats a vendor's stdout as a one-way pipe:
// `ParseFunc` sees a line, returns an event, and has nowhere to reply to. That
// held for as long as every persistent seat spoke a protocol where the room's
// side of the conversation was a monologue — Claude's stream-json is exactly
// that, and its parser stays a `ParseFunc` for exactly that reason.
//
// cursor-agent's hidden `acp` subcommand is not that. It is JSON-RPC 2.0 over
// newline-delimited stdin/stdout, and three of its properties are unreachable
// through a ParseFunc (all MEASURED 2026-08-08 against 2026.08.04-aaa8809; see
// design.md §9.36):
//
//   - A turn cannot be ENCODED until the child has answered a request of the
//     room's own. `session/prompt` carries a `sessionId` that only arrives in
//     the response to `session/new`, so the first turn has to wait for a reply
//     rather than merely for a line.
//   - The child asks QUESTIONS. `session/request_permission` blocks the vendor
//     until an answer goes back down the same pipe — measured on both branches,
//     with a rejection leaving the command unrun.
//   - Correlation is by JSON-RPC id, in TWO namespaces. The child numbers its
//     own requests from 0 independently of ours, so "id 0" inbound is a question
//     and "id 0" outbound is an answer to one.
//
// So a Protocol is stateful, lives exactly as long as one process, and owns both
// directions. Runner still owns everything about the PROCESS — the job object,
// the bounded channel, the turn clock, the guarantee that no prompt reaches
// argv — and knows nothing about what the bytes mean.
//
// Every method returns LINES rather than writing them. That is what keeps a
// Protocol testable by replay the way a ParseFunc is: feed it a captured stream
// and assert on the events it emits and the answers it would have sent, with no
// process anywhere near the test.
type Protocol interface {
	// Opening is what to write the moment the process is up, before any turn.
	//
	// It does NOT open a turn on the clock. The handshake is the process's own
	// startup and is charged to the first turn as spawn cost, which is the same
	// accounting `clock.pending` already applies to a spawn — billing it as
	// somebody's wait would make the room's own opening look like a slow vendor.
	Opening() [][]byte

	// Inbound consumes one line from the child. It returns events for the room
	// and lines to write straight back.
	//
	// Both halves are needed at once and by the same line: the response to
	// `session/new` yields a KindSession event AND releases a turn that has been
	// queued since before there was a session to put it in.
	Inbound(line []byte) ([]Event, [][]byte)

	// Turn encodes one turn.
	//
	// Returning no lines and no error is legal and means the protocol has TAKEN
	// the turn and will send it once it can. The caller has still handed the turn
	// over, so the turn clock starts — the user is waiting from that moment
	// whether or not a byte has moved.
	Turn(prompt string) ([][]byte, error)

	// Interrupt abandons the turn in flight WITHOUT killing the process.
	Interrupt(id string) ([][]byte, error)

	// Decide answers one Gate. allow=false carries reason back as the refusal.
	//
	// input is the tool's own arguments, echoed back where a vendor requires it.
	// ACP does not; the parameter is in the signature so the room has ONE call
	// shape for every persistent seat rather than a branch at the call site.
	Decide(requestID string, allow bool, reason string, input map[string]any) ([][]byte, error)
}
