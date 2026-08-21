// Package mcpserver serves the snapshot document over the Model Context
// Protocol on stdio, so a reader that is an AGENT can ask for fleet state with
// the mechanism it already has (docs/design.md §7.25).
//
// It is a FOURTH reader of the same scan, beside the statusline, the HUD and
// `telltale snapshot`, and it adds no reading of its own. The tool result IS
// `internal/snapshot`'s document — the same Build, the same Encode, byte for
// byte — because every honesty property that document carries is a property of
// its bytes: zero is the number 0, absent is `null`, no optional key is
// omitted, `estimated` names what an adapter computed, `unsupported` names what
// a vendor can never source, and `self_reported` names an entry whose writer
// claimed it. A second serializer here would be a second statement of that
// contract, and two statements of one contract drift (§7.22's own argument for
// a published schema over hand-written Go assertions).
//
// Why a mode rather than a flag on `snapshot`: what it prints goes somewhere
// else, which is the reason `doctor` and `events view` are their own modes too.
// `snapshot` prints one document and exits; this one holds the pipe open and
// answers requests until its client closes stdin, and its stdout carries
// protocol frames that no human reads.
//
// # It is a gauge, and it keeps the gauges' contract
//
// This mode writes nothing, anywhere — not even the quota relay, for §7.22's
// reason: it renders no quota of its own to relay. It calls no network and it
// binds no port. **stdio only**: the client starts this process and owns both
// pipes, so §7.24's question of who may push to a listener does not arise here
// — there is nothing to push to. `TestTheServerOpensNoSocket` and
// `TestTheServerWritesNothing` are what keep both halves true.
//
// A tool call runs one scan and answers. Nothing here starts a vendor, spends
// quota, reads a credential, or sends anything to a running agent.
//
// # The surface is deliberately four methods wide
//
// `initialize`, `tools/list`, `tools/call`, plus `ping` and the notifications a
// client sends and expects no answer to. That is what a client needs to reach
// one tool. Resources, prompts, completion, sampling, subscriptions and
// server-initiated requests are all absent, and their absence is stated in the
// capabilities object rather than discovered by a client that tried.
//
// This is hand-written JSON-RPC over stdlib `encoding/json` rather than the
// official MCP Go SDK, and that follows the module's standing position rather
// than inventing one: `go.mod` carries no direct dependency outside the TUI
// stack, and design.md records the same refusal for the SQLite reader (§3.2),
// the zstd reader, the OTLP listener (§7.16a) and the event emitter (§7.21).
// The surface above is small enough that a dependency would carry far more
// protocol than this mode serves.
package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/sanlee-ys/telltale/internal/snapshot"
)

// PinnedProtocolVersion is the revision this server answers with when the
// client asks for one it does not know.
//
// The lifecycle rule is that a server which does not support the requested
// version responds with one it does support, and the client disconnects if it
// cannot meet that. So this is the LATEST revision in supportedVersions below,
// not the oldest — an old client that cannot read this answer disconnects,
// which is the protocol working.
const PinnedProtocolVersion = "2025-11-25"

// supportedVersions are the revisions this server will echo back verbatim.
//
// The narrow surface above is spelled identically across all four: `initialize`
// negotiates a version, `tools/list` returns `tools`, and `tools/call` returns
// `content` with an optional `isError`. Nothing this server sends changes shape
// between them, so echoing the client's own revision is a true statement rather
// than a compatibility guess.
//
// **2026-07-28 is deliberately NOT here**, and it is the newest revision, so
// the omission is the interesting one. That revision requires a server to
// implement `server/discover`, and this one does not implement it. Claiming the
// version would be claiming a method a client is entitled to call — which is
// the ADR-001 failure in protocol form: a capability asserted rather than
// built. A client that speaks it and calls `server/discover` first gets
// "method not found"; the same revision's own versioning section says a client
// may instead invoke methods inline and handle a version error, which is the
// path that works here.
var supportedVersions = []string{
	"2024-11-05",
	"2025-03-26",
	"2025-06-18",
	PinnedProtocolVersion,
}

// ToolName is the one tool this server exposes.
//
// One tool, not a family. Every question an agent asks about the fleet — how
// much context is anything holding, what has been spent, which vendor stopped
// reading, how stale is the quota reading — is already one parse of this one
// document, and a second tool answering a subset would have to re-serialize
// part of it. That is where the zero-vs-absent rules would get restated, and a
// restated rule is a rule that drifts (§4a.1).
const ToolName = "fleet_snapshot"

// toolDescription is what the calling MODEL reads before it decides to call.
//
// It states the document's provenance rules rather than describing the
// fields, because a reader that does not know them will read `null` as zero and
// an estimate as a measurement — which is the exact collapse this repo exists
// to prevent, moved one process outward into the agent.
const toolDescription = "Read the current state of this machine's coding-agent fleet: one scan, " +
	"one JSON document, one entry per vendor plus a pre-computed fleet rollup. " +
	"Three rules govern the values and a reader that ignores them will be wrong: " +
	"a measured zero is the number 0 and an absent reading is null, never 0; " +
	"a field named in a vendor's \"estimated\" list was computed by telltale rather than read from the vendor; " +
	"a field named in \"unsupported\" is one that vendor can never report, so a null there is a capability statement rather than this moment's reading. " +
	"An entry with \"self_reported\": true carries numbers its own writer claimed. " +
	"The document holds numbers and keys only: no session name, workspace path, transcript or reply text."

// FleetFunc runs one scan and returns the document for it.
//
// It is injected rather than called directly so this package depends on no
// adapter, no store and no clock: the scan lives in cmd/telltale beside
// `snapshot`'s own, which is what keeps the two modes from drifting into two
// scans. A test drives this server with a fixture document and never touches
// the machine it runs on.
//
// vendor is the `--vendor` vocabulary, "all" by default. An unknown value is an
// error here, and the caller turns it into a tool result the model can read
// rather than a transport error it cannot.
type FleetFunc func(ctx context.Context, vendor string) (snapshot.Document, error)

// Options configures one Serve run.
type Options struct {
	// Name and Version are what `initialize` reports as serverInfo. Version is
	// the binary's own version string, so a client's logs name the build that
	// answered.
	Name    string
	Version string
	Fleet   FleetFunc
}

// Serve reads newline-delimited JSON-RPC from in and writes it to out until in
// reaches EOF, then returns nil.
//
// EOF is the normal end of an MCP stdio session: the client closes the pipe
// when it is done with the server, and a server that treated that as an error
// would put a red line in the client's log for a clean shutdown.
//
// Requests are handled ONE at a time, in arrival order. The protocol permits
// concurrent responses, and this server declines the permission: a scan is the
// only cost here, interleaving would need a writer lock for no measured gain,
// and sequential replies make the ordering of this mode's stdout trivially
// correct rather than argued.
func Serve(ctx context.Context, in io.Reader, out io.Writer, opt Options) error {
	if opt.Fleet == nil {
		return errors.New("mcpserver: no Fleet function; the server would advertise a tool it cannot answer")
	}
	r := bufio.NewReader(in)
	// One encoder for the whole run. Compact by construction (Encode writes no
	// indentation and one trailing newline), which is the framing the stdio
	// transport requires: one message per line, no embedded line breaks.
	// encoding/json escapes a newline inside any string it writes, so a vendor's
	// OS error message cannot break a frame.
	enc := json.NewEncoder(out)
	// No HTML escaping, for §7.22's reason: an OS error message can carry & or
	// <, and the escaped spelling is noise in every reader of this document.
	enc.SetEscapeHTML(false)

	for {
		line, err := r.ReadString('\n')
		if len(strings.TrimSpace(line)) > 0 {
			if resp, ok := handle(ctx, []byte(line), opt); ok {
				if werr := enc.Encode(resp); werr != nil {
					return werr
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

// handle turns one incoming line into at most one response.
//
// The second return value is false when there is nothing to send. That case is
// not an edge: a NOTIFICATION carries no id and must never be answered, and
// `notifications/initialized` is one every client sends. A server that replied
// to it would be sending a response to a message that has no id to correlate
// it with.
func handle(ctx context.Context, line []byte, opt Options) (response, bool) {
	trimmed := bytes.TrimSpace(line)
	if trimmed[0] == '[' {
		// JSON-RPC batching was removed from MCP in 2025-06-18 and this server
		// supports revisions on both sides of that. Refusing it with the reason
		// beats answering half of it.
		return errorResponse(nil, codeInvalidRequest, "this server takes one JSON-RPC message per line; batch arrays are not supported (removed from MCP in 2025-06-18)"), true
	}

	var req request
	if err := json.Unmarshal(trimmed, &req); err != nil {
		return errorResponse(nil, codeParseError, "not JSON-RPC: "+err.Error()), true
	}
	if req.Method == "" {
		// A message with an id and no method is a RESPONSE to a request this
		// server never sent. Answering it would itself be a protocol error, so
		// it is dropped in silence.
		return response{}, false
	}
	if len(req.ID) == 0 || string(req.ID) == "null" {
		// A notification. Nothing this server does is triggered by one, and
		// none of them is an error: an unknown notification is explicitly a
		// thing a peer may ignore.
		return response{}, false
	}
	if req.JSONRPC != "2.0" {
		return errorResponse(req.ID, codeInvalidRequest, `"jsonrpc" must be "2.0"`), true
	}

	switch req.Method {
	case "initialize":
		return okResponse(req.ID, initializeResult(req.Params, opt)), true
	case "ping":
		// An empty result object is the whole of a pong.
		return okResponse(req.ID, struct{}{}), true
	case "tools/list":
		return okResponse(req.ID, toolsListResult{Tools: []tool{describeTool()}}), true
	case "tools/call":
		return okResponse(req.ID, callTool(ctx, req.Params, opt)), true
	default:
		return errorResponse(req.ID, codeMethodNotFound, "unknown method "+req.Method+
			"; this server implements initialize, tools/list, tools/call and ping"), true
	}
}

// initializeResult negotiates the version and states what this server can do.
//
// The version rule is the lifecycle's: echo the client's own revision when it
// is one this server supports, and otherwise answer with the latest it does. A
// client that cannot meet the answer disconnects, which is the negotiation
// working rather than failing.
//
// A malformed or missing params object is NOT an error here. The one field this
// server reads is protocolVersion, and the fallback for "the client did not say"
// is identical to the fallback for "the client said something unknown". Failing
// the handshake over a field with a working default would refuse a session this
// server can serve.
func initializeResult(params json.RawMessage, opt Options) initResult {
	negotiated := PinnedProtocolVersion
	var p initParams
	if len(params) > 0 && json.Unmarshal(params, &p) == nil {
		for _, v := range supportedVersions {
			if p.ProtocolVersion == v {
				negotiated = v
				break
			}
		}
	}
	return initResult{
		ProtocolVersion: negotiated,
		// Tools is the only capability, and it carries no `listChanged`: this
		// server's tool list is a constant, so it can never change and a
		// subscription to its changes would be an offer of nothing. Every other
		// capability is absent because it is unimplemented, which is the same
		// distinction the document itself draws between absent and zero.
		Capabilities: capabilities{Tools: struct{}{}},
		ServerInfo:   implementation{Name: opt.Name, Version: opt.Version},
		Instructions: "Call " + ToolName + " to read this machine's coding-agent fleet. " +
			"It reports what telltale measured and marks what it did not: null is an absent reading and never a zero, " +
			"and a field listed in a vendor's \"estimated\" or \"unsupported\" array carries a provenance statement about itself.",
	}
}

// callTool runs the one tool.
//
// A failure comes back as a RESULT with isError set rather than as a JSON-RPC
// error, and the split is about who can fix it. A bad `vendor` argument and a
// scan that failed are things the MODEL asked for and the model can correct, so
// they belong where the model reads — in the content of a result. The one
// exception is a tool NAME this server does not have: that is the client's
// wiring rather than the model's request, and it is answered as invalid params.
func callTool(ctx context.Context, params json.RawMessage, opt Options) any {
	var p callParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return toolFailure("could not read the call parameters: " + err.Error())
		}
	}
	if p.Name != ToolName {
		return rpcError{Code: codeInvalidParams, Message: "unknown tool " + p.Name + "; this server has one: " + ToolName}
	}
	vendor := strings.TrimSpace(p.Arguments.Vendor)
	if vendor == "" {
		vendor = "all"
	}

	doc, err := opt.Fleet(ctx, vendor)
	if err != nil {
		return toolFailure(err.Error())
	}
	// The document is encoded by the package that owns it, in the indented form
	// `telltale snapshot` prints by default. Nothing here re-serializes it: the
	// bytes a client reads are the bytes the CLI prints, which is what makes
	// this a fourth READER rather than a second format.
	body, err := snapshot.Encode(doc, false)
	if err != nil {
		return toolFailure("could not encode the fleet document: " + err.Error())
	}
	return callResult{
		Content: []content{{Type: "text", Text: string(body)}},
		// StructuredContent is the same document again, as JSON rather than as
		// text, for a client that reads it that way. It is the identical value
		// — one Build, one document, marshalled twice by one package — so the
		// two can never disagree.
		//
		// No `outputSchema` is declared beside it, on purpose. The document's
		// schema is published at docs/snapshot.schema.json and CI validates the
		// shipped binary's output against that file; embedding a copy in this
		// binary would be the second statement of one contract that §7.22
		// refused when it chose a schema file over hand-written assertions.
		StructuredContent: &doc,
		IsError:           false,
	}
}

// toolFailure is an error the calling model should read and act on.
func toolFailure(msg string) callResult {
	return callResult{
		Content: []content{{Type: "text", Text: msg}},
		IsError: true,
	}
}

// describeTool is the tool's advertised shape.
//
// `additionalProperties: false` on the input, deliberately, and it is the
// opposite choice from the document's own schema (§7.22 sets it true there).
// The two are different directions of trust: the document is this server's
// output and a reader must tolerate a field added later, while these arguments
// are input and an unrecognized one means the caller believes in a control this
// tool does not have. Refusing it beats scanning the whole fleet for a request
// that asked for something narrower.
func describeTool() tool {
	return tool{
		Name:        ToolName,
		Title:       "Fleet snapshot",
		Description: toolDescription,
		InputSchema: inputSchema{
			Type: "object",
			Properties: map[string]schemaProperty{
				"vendor": {
					Type: "string",
					Description: "Report one vendor only. Accepts the same words as `telltale snapshot --vendor`: " +
						"all (default), claude, codex, gemini, agy, cursor, grok, pi, self-reported.",
				},
			},
			AdditionalProperties: false,
		},
	}
}

// The JSON-RPC envelope.
//
// `omitempty` appears here on Result and Error, and once more on a failed
// call's StructuredContent. Neither is the `omitempty` §7.22 forbids. That rule
// is about a MEASUREMENT going missing — a key that vanishes when its value is
// absent makes "no reading" and "the schema moved" one observation. These two
// are the transport's own contract instead: a response carries exactly one of
// result and error, and a call that measured nothing carries no document.
// Nothing inside the fleet document is touched by either.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// The JSON-RPC codes this server uses, spelled out rather than inlined so a
// reader can see the whole vocabulary at once.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// okResponse wraps a result. A handler that returns an rpcError value gets it
// carried as a protocol error instead — which is how tools/call reports an
// unknown tool name without a second return value on every path.
func okResponse(id json.RawMessage, result any) response {
	if e, isErr := result.(rpcError); isErr {
		return response{JSONRPC: "2.0", ID: id, Error: &e}
	}
	return response{JSONRPC: "2.0", ID: id, Result: result}
}

func errorResponse(id json.RawMessage, code int, msg string) response {
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}

type initParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

type initResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    capabilities   `json:"capabilities"`
	ServerInfo      implementation `json:"serverInfo"`
	Instructions    string         `json:"instructions"`
}

type capabilities struct {
	Tools struct{} `json:"tools"`
}

type implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type toolsListResult struct {
	Tools []tool `json:"tools"`
}

type tool struct {
	Name        string      `json:"name"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	InputSchema inputSchema `json:"inputSchema"`
}

type inputSchema struct {
	Type                 string                    `json:"type"`
	Properties           map[string]schemaProperty `json:"properties"`
	AdditionalProperties bool                      `json:"additionalProperties"`
}

type schemaProperty struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type callParams struct {
	Name      string `json:"name"`
	Arguments struct {
		Vendor string `json:"vendor"`
	} `json:"arguments"`
}

type callResult struct {
	Content []content `json:"content"`
	// StructuredContent is a pointer so a failed call omits it entirely. An
	// empty document here would be a fleet with nothing in it, which is a
	// measurement — and this is the absence of one.
	StructuredContent *snapshot.Document `json:"structuredContent,omitempty"`
	IsError           bool               `json:"isError"`
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
