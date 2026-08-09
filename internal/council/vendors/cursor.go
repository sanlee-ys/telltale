package vendors

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// Cursor drives cursor-agent as ONE LIVE ACP SERVER, and only that way.
//
// This seat has been written three times, and the third rewrite is the one this
// file is. The first two were print mode — `cursor-agent -p --output-format
// stream-json`, a fresh child per turn — first from reading the shipped
// JavaScript bundle, then corrected by four live turns on 2026.07.23-e383d2b.
// Both are gone. §9.33 measured what that path actually cost and what the
// alternative was, §9.36 drove the alternative through everything two trivial
// turns could not reach, and the ruling was WHOLESALE: the spawn-per-turn path
// is replaced rather than kept beside the new one. There is no fallback, and
// that is deliberate — a fallback is a second protocol to keep honest, and the
// numbers below are why nobody would want to fall back to it.
//
//	            print mode (§9.33)     ACP (§9.36)
//	a follow-up turn   ~13s            1.1–1.8s
//	fixed cost         ~8.1s PER TURN  ~2–4s once, at handshake
//	resume             a whole respawn  session/load, ~0.9s, same process
//
// What the switch COST is stated beside what it bought, because the losses are
// real and two of them are permanent:
//
//   - **Token usage is gone.** Print mode's `result` carried an inputTokens /
//     outputTokens block. ACP's turn resolves with a stop reason and nothing
//     else. (Cost was already absent: this vendor publishes no monetary figure
//     anywhere, so CostUSD has always been nil here and still is.)
//   - **The final-reply safety net is gone.** Print mode's `result` carried the
//     whole answer, which §9.6c leaned on as the fallback for a column that
//     streamed nothing. ACP has no equivalent line.
//   - **`--sandbox enabled` is gone off Windows.** ACP takes no sandbox
//     parameter, so the request this seat used to make on macOS and Linux cannot
//     be made. What is lost is a REQUEST whose enforcement was never observed —
//     the old comment said so in as many words — not a measured protection. On
//     Windows nothing is lost at all: the flag was measured KILLING the turn
//     there, which is why it was already not passed.
//
// And what came back the other way, none of which print mode could do:
//
//   - A conversation survives the ROOM. `session/load` reloads a thread into a
//     brand-new process, verified twice by asking about facts only the earlier
//     process could know.
//   - `cwd` and the posture mode are SESSION parameters, not argv. Moving the
//     room or changing the posture costs a new session in the live process, not
//     a new process.
//   - The vendor can ASK. `session/request_permission` blocks it until answered,
//     both branches measured. It does not ask about everything — see
//     detect.go's Cursor branch for exactly what it covers and what it does not.
type Cursor struct{}

// CursorNodeBundle is the JavaScript entry point a node interpreter has to be
// handed to become cursor-agent, or "" when this path is not a node at all.
//
// Exported because detection and this adapter must not derive it separately.
// Detection resolves the seat's binary to the node the vendor's own .cmd
// launcher would have run and checks that this file sits beside it; the
// invocation below puts that same file in argv[1]. Two copies of one
// filepath.Join is exactly the kind of agreement that silently stops holding.
//
// The sibling relationship is the launcher's, not this repo's: cursor-agent.ps1
// runs `& "$dir\node.exe" "$dir\index.js" $args` in both of its branches.
func CursorNodeBundle(binary string) string {
	base := strings.ToLower(filepath.Base(binary))
	if strings.TrimSuffix(base, filepath.Ext(base)) != "node" {
		return ""
	}
	return filepath.Join(filepath.Dir(binary), "index.js")
}

// Registration lives in vendor.go; these pin the interfaces at compile time.
var (
	_ Vendor         = Cursor{}
	_ Conversational = Cursor{}
)

func (Cursor) ID() model.VendorID { return model.VendorCursor }

// ErrCursorIsLiveOnly is what the batch entry points return.
//
// They exist because Conversational embeds Vendor and the registry is one map;
// they cannot DO anything because there is no longer a batch invocation of this
// seat to build. Returning a named error rather than quietly constructing a
// print-mode spec is the difference between a path that is gone and a path that
// is gone except when something forgets — and the something would be a caller
// that stopped recognising this vendor as Conversational, which is precisely the
// bug worth failing loudly on.
var ErrCursorIsLiveOnly = errors.New(
	"vendors: the cursor seat is driven as a live ACP process, not as one child per turn")

func (Cursor) FirstTurn(_, _, _ string, _ Posture) (runner.Spec, error) {
	return runner.Spec{}, ErrCursorIsLiveOnly
}

func (Cursor) NextTurn(_, _, _, _ string, _ Posture) (runner.Spec, error) {
	return runner.Spec{}, ErrCursorIsLiveOnly
}

// ParseEvent has nothing to parse.
//
// The ACP stream is not a sequence of independent lines that mean something one
// at a time: a `session/update` belongs to a session opened by an earlier
// response, and half the traffic is answers to questions. All of that lives on
// the per-process protocol, which is what Open returns. This method survives
// only because Vendor requires it.
func (Cursor) ParseEvent([]byte) (runner.Event, bool) { return runner.Event{}, false }

// Open builds the invocation and the protocol driver for one process.
//
// sessionID is a thread from a saved room, or empty for a new conversation. It
// is ONE argument rather than the Session/SessionResume pair the stream-json
// seats use, and the difference is a fact about the protocol rather than a
// simplification: for Claude, resuming changes ARGV, so the two cases are two
// different invocations and folding them together would hide that decision at
// every call site. Here both cases are the same process launched the same way,
// and the choice is made later — one JSON-RPC method or another — by the
// protocol itself, on a field it already holds.
func (c Cursor) Open(workspace, binary, sessionID string, p Posture) (runner.Spec, runner.Protocol, error) {
	return runner.Spec{
			Vendor: c.ID(),
			Binary: binary,
			// `acp` and nothing else. Every flag the print-mode invocation
			// carried — -p, --output-format, --stream-partial-output, --mode,
			// --workspace, --sandbox, the `--` separator that kept a brief
			// beginning with a dash from being read as a flag — belonged to a
			// surface this seat no longer uses. The posture arrives as
			// session/set_mode, the workspace as session/new's cwd, and the brief
			// as a JSON string that no shell and no argv parser ever sees.
			Args: cursorArgs(binary, []string{"acp"}),
			// No StdinPrompt, and now there is no path by which prompt text could
			// reach argv at all — which retires the whole shim question this seat
			// used to have to reason about.
			//
			// Dir is the workspace for tidiness rather than for effect: MEASURED,
			// the SESSION's cwd is what binds. A server started in one directory
			// ran one session in ws1 reading ws1's file and another in ws3 reading
			// ws3's, from the same process.
			Dir: workspace,
		},
		newACPProtocol(workspace, sessionID, p),
		nil
}

// cursorArgs prepends the JavaScript entry point when the resolved binary is a
// node interpreter rather than cursor-agent itself.
//
// Both cases are live. On Windows detection resolves this seat to the bundled
// node.exe, stepping over a .cmd launcher whose only job was to do the same
// thing; on macOS and Linux it resolves to cursor-agent, which needs no bundle.
// The adapter reads which case it is off the path it was handed rather than
// off runtime.GOOS, because an override (TELLTALE_COUNCIL_CURSOR_BIN) can put
// either shape on either OS.
func cursorArgs(binary string, rest []string) []string {
	bundle := CursorNodeBundle(binary)
	if bundle == "" {
		return rest
	}
	return append([]string{bundle}, rest...)
}
