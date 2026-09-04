package vendors

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// AntigravityStream drives the Antigravity CLI as ONE process taking many
// turns: `agy --input-format stream-json --output-format stream-json`.
//
// NOTHING IN THIS FILE IS MEASURED. Every flag and every envelope below was
// read from the vendor's documentation on 2026-09-02 and is seated on that
// reading, with the measured print-mode invocation (agy.go, `-p` per turn) kept
// beside it as the fallback. The seat exists because a crew tool needs seats
// that stay up between briefs and this vendor's cold start was measured at
// 6.4 s (design.md §9.57's motivation), which every `-p` turn pays again.
//
// What was read, and where:
//
//   - antigravity.google/changelog: **1.1.15 (2026-08-19)** "Added
//     `--input-format stream-json` to print mode, which reads newline-delimited
//     JSON prompts from stdin and runs one turn per message in a single
//     conversation". The current build is 1.1.24.
//   - antigravity.google/docs/cli/headless: the stdin line is
//     `{ "event": "user", "message": { "content": "…" } }`, one object per
//     line, "The event key specifies the message type (matching the output
//     stream format)"; each turn "emits a series of step_update events for the
//     active turn" and "concludes the turn with a final result event"; the
//     flag "requires --output-format stream-json"; and "to close a session
//     gracefully, simply close stdin. The process exits after the input pipe is
//     closed and the current turn completes."
//
// What follows from that reading, and what does not:
//
//   - The OUTPUT is the stream agy.go already parses — `init`, `step_update`,
//     `result` — so ParseEvent is the measured parser with ONE change: `result`
//     ends the turn. On the batch seat it deliberately does not, because the
//     process exit was measured landing 0.05–0.31 s later and settling early
//     bought nothing (agy.go's result branch). Here the process does not exit
//     between turns, so `result` is the only end-of-turn signal there is. That
//     is a claim about THIS shape, read off the documentation sentence quoted
//     above, and it is the first thing a live run must confirm.
//   - There is NO GATE CHANNEL. The envelope carries a prompt and nothing
//     else; a PreToolUse hook may answer `ask` and nothing in print mode
//     answers it (agy.go's --dangerously-skip-permissions paragraph records
//     the measured half: a turn that hits one sits until --print-timeout). So
//     Decide and Interrupt both refuse, the room kills the process on an
//     interrupt as the documented fallback, and the badge keeps saying writes
//     are unasked and the containment is the workspace.
//   - Resume is `--conversation <id>` ON THE SAME ARGV, and whether that
//     composes with `--input-format` is not stated on either page. The §9.43
//     guard is what makes sending it safe: this seat implements
//     SilentResumeFork, so the room compares the id it asked for against the
//     id `init` reports and says out loud when the history did not come back.
//     A refusal that is loud instead would be the ordinary lost-thread path.
//
// The fallback trigger has a specific shape for a Persistent seat, and it is
// the room's to read rather than this file's (vendors.LiveFallback): a build
// that does not know `--input-format` exits at argument parsing — agy.go's
// `-p` paragraph records the measured spelling of that class, "flag needs an
// argument", exit 2 — BEFORE any `init` names a conversation. A process that
// died with no session id after its first turn is the whole signal.
type AntigravityStream struct{}

var (
	_ Vendor           = AntigravityStream{}
	_ Persistent       = AntigravityStream{}
	_ LiveFallback     = AntigravityStream{}
	_ GracefulStop     = AntigravityStream{}
	_ SilentResumeFork = AntigravityStream{}
)

func (AntigravityStream) ID() model.VendorID { return model.VendorAntigravity }

// Fallback is the measured seat: `agy -p <prompt>`, one process per turn.
func (AntigravityStream) Fallback() Vendor { return Antigravity{} }

// FirstTurn and NextTurn are the batch adapter's, and they are what the arena
// runs: a racer is a fresh one-shot in its worktree (dispatch.go), so this
// seat's race keeps the measured invocation rather than the read one.
func (AntigravityStream) FirstTurn(prompt, workspace, binary string, p Posture) (runner.Spec, error) {
	return Antigravity{}.FirstTurn(prompt, workspace, binary, p)
}

func (AntigravityStream) NextTurn(prompt, workspace, binary, sessionID string, p Posture) (runner.Spec, error) {
	return Antigravity{}.NextTurn(prompt, workspace, binary, sessionID, p)
}

// SilentResumeForkMeasuredAt carries the batch seat's measurement across, and
// says which half of it is borrowed.
//
// The FORK — an unknown `--conversation` id opening a new conversation, exit
// 0, a different id reported — was measured on `agy -p` at 1.1.11 (agy.go).
// Whether the same flag under `--input-format stream-json` forks the same way,
// refuses loudly, or is ignored is unmeasured; implementing the interface here
// is the conservative reading, because the room's response to it is a
// COMPARISON that fires only on a mismatch. A vendor that refuses loudly is
// caught by the ordinary lost-thread path and never reaches it.
func (AntigravityStream) SilentResumeForkMeasuredAt() string {
	return Antigravity{}.SilentResumeForkMeasuredAt() + " on agy -p; the stream-json input path is unmeasured"
}

// Session is the persistent invocation: one process, many turns, fed JSONL on
// an stdin that stays open.
//
// The batch flags plus `--input-format stream-json`, and the ORDER is the
// batch seat's rule kept: every flag precedes the point where `-p` would have
// gone, and `-p` itself is absent — every turn is a Turn() line written later.
// `--print-timeout 30m` rides along from agy.go, and one thing about it is
// owed a measurement rather than assumed: the help text calls it "Timeout for
// print mode wait", and whether that bounds each turn or the whole process
// under stream input is not stated. A per-process reading would end this
// seat's conversation thirty minutes into the room, loudly, as a process
// exit.
//
// hooksFile is accepted and ignored, and the ignoring is the honest part.
// The parameter exists because the Claude seat wires council's PreToolUse
// gate hook through it; this vendor accepts a hook that answers `ask` and has
// nothing in print mode that answers the ask, so wiring one here would stall
// every tool call on a question with no listener. The posture and the
// workspace ride through baseArgs, for agy.go's reason: `--add-dir` names the
// workspace, and `--mode accept-edits` lands the write posture's edits. Both
// were measured on this stream path on 2026-09-03: one user line down stdin,
// and the file landed in the named tree.
func (a AntigravityStream) Session(workspace, binary, _ string, p Posture) (runner.Spec, error) {
	return runner.Spec{
		Vendor: a.ID(),
		Binary: binary,
		Args:   append(Antigravity{}.baseArgs(workspace, p), "--input-format", "stream-json"),
		Dir:    workspace,
	}, nil
}

// SessionResume is Session started on a conversation from a previous room.
//
// `--conversation` before the input flag, as it sits before `-p` on the batch
// path. Composition with `--input-format` is UNMEASURED; see the type comment
// for why sending it is safe anyway.
func (a AntigravityStream) SessionResume(workspace, binary, hooksFile, sessionID string, p Posture) (runner.Spec, error) {
	if sessionID == "" {
		return runner.Spec{}, ErrNoResume
	}
	spec, err := a.Session(workspace, binary, hooksFile, p)
	if err != nil {
		return runner.Spec{}, err
	}
	spec.Args = append(spec.Args, "--conversation", sessionID)
	return spec, nil
}

// agyUserMessage is the turn envelope, in the documented shape:
//
//	{ "event": "user", "message": { "content": "your prompt here" } }
//
// Marshalled rather than assembled, for claude.go's reason: a brief contains
// quotes and newlines by the time anyone uses this seriously.
type agyUserMessage struct {
	Event   string `json:"event"`
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

func (AntigravityStream) Turn(prompt string) ([]byte, error) {
	m := agyUserMessage{Event: "user"}
	m.Message.Content = prompt
	return json.Marshal(m)
}

// ErrAgyStreamNoInterrupt is returned by Interrupt, always.
//
// The envelope carries prompts and nothing else, so there is no line that
// abandons a turn without ending the process. An error rather than a silent
// success, on acpProtocol's argument: interruptSeat reads a clean return as
// "the cancel was delivered" and stops, and nothing would then end the turn.
// The error falls through to the kill, and the next brief starts a fresh
// process — which on this seat costs the cold start the whole shape exists to
// avoid, stated rather than hidden.
var ErrAgyStreamNoInterrupt = errors.New(
	"vendors: the antigravity seat has no interrupt channel; the process has to be killed")

func (AntigravityStream) Interrupt(string) ([]byte, error) { return nil, ErrAgyStreamNoInterrupt }

// ErrAgyStreamNoGate is returned by Decide, always: this seat raises no Gate,
// so there is no request id an answer could name.
var ErrAgyStreamNoGate = errors.New(
	"vendors: the antigravity seat has no approval channel; nothing was asked")

func (AntigravityStream) Decide(string, bool, string, map[string]any) ([]byte, error) {
	return nil, ErrAgyStreamNoGate
}

// Closing has nothing to say: there is no interrupt to send, and the documented
// shutdown is the stdin close itself, which the room performs.
func (AntigravityStream) Closing() [][]byte { return nil }

// Grace bounds the wait after stdin closes. The documentation says the process
// exits "after the input pipe is closed and the current turn completes", and a
// current turn can run for minutes — so this is not the time the process is
// expected to take, it is how long the room is willing to give it before the
// kill that actually ends it.
func (AntigravityStream) Grace() time.Duration { return 3 * time.Second }

// ParseEvent is the measured parser with the turn's end added.
//
// `result` is this vendor's answer-complete marker (agy.go measured it as the
// last line on stdout, three trials at 1.1.13), and on a process that does not
// exit between turns it is the ONLY end-of-turn signal — so EndsTurn is set on
// it, on both the success and the failure shape. On every other event the
// batch parser's word is final.
func (AntigravityStream) ParseEvent(line []byte) (runner.Event, bool) {
	ev, ok := Antigravity{}.ParseEvent(line)
	if !ok {
		return ev, ok
	}
	// The result event is the one that carries the conversation id under its
	// own key and a Kind of Meta or Error. KindError is produced by no other
	// branch of the batch parser, and KindMeta by no other event.
	if ev.Kind == runner.KindMeta || ev.Kind == runner.KindError {
		ev.EndsTurn = true
	}
	return ev, true
}
