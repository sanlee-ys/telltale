package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/snapshot"
)

// Every test here drives the server the way a client does: lines of JSON in,
// lines of JSON out. Nothing calls a handler directly, because the framing is
// half of what this package is — a handler that answered correctly into a
// stream nobody could parse would pass a unit test and fail every client.

// fixture is a document with both halves of the zero-vs-absent pair in it, and
// no scan behind it. The server must carry it through untouched; it is not this
// package's business to produce one.
func fixture() snapshot.Document {
	at := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	zero := 0.0
	ctx := 61.3
	return snapshot.Document{
		SchemaVersion: snapshot.SchemaVersion,
		GeneratedAt:   at,
		Fleet: snapshot.Fleet{
			Sessions:        2,
			Live:            1,
			VendorsWatching: 2,
			ContextPctMax:   &ctx,
			// CostUSDTotal stays nil: no session anywhere reported a cost. That
			// is the absent half, and it must never print as 0.
		},
		Vendors: []snapshot.Vendor{
			{
				Vendor: model.VendorClaude, Status: "watching", Sessions: 2, Live: 1,
				ContextPctMax: &ctx,
				Quota: []snapshot.QuotaWindow{
					// A measured zero, beside a fleet cost that is absent.
					{ID: "seven_day", Label: "7d", UsedPct: &zero},
				},
				QuotaReadAt: &at,
				Estimated:   []string{model.FieldContextPercent.String()},
				Unsupported: []string{},
			},
			{
				Vendor: model.VendorCodex, Status: "not detected", Sessions: 0,
				Quota: []snapshot.QuotaWindow{}, Estimated: []string{},
				Unsupported: []string{model.FieldCost.String()},
			},
		},
	}
}

func options() Options {
	return Options{
		Name:    "telltale",
		Version: "test",
		Fleet: func(_ context.Context, vendor string) (snapshot.Document, error) {
			if vendor != "all" && vendor != "claude" {
				return snapshot.Document{}, errUnknownVendor(vendor)
			}
			return fixture(), nil
		},
	}
}

type vendorError string

func (e vendorError) Error() string { return string(e) }

func errUnknownVendor(v string) error {
	return vendorError("unknown --vendor " + v + " (want all, claude, codex, gemini, agy, cursor, grok, pi, self-reported)")
}

// drive runs the server over the given request lines and returns the response
// lines it wrote.
func drive(t *testing.T, opt Options, lines ...string) []map[string]json.RawMessage {
	t.Helper()
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	var out strings.Builder
	if err := Serve(context.Background(), in, &out, opt); err != nil {
		t.Fatalf("Serve returned %v; a closed stdin is a clean end of session", err)
	}
	var got []map[string]json.RawMessage
	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("the server wrote a line no client could parse: %q (%v)", line, err)
		}
		got = append(got, m)
	}
	return got
}

const initLine = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"probe","version":"0"}}}`
const callLine = `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"fleet_snapshot","arguments":{}}}`

// TestTheHandshakeAnswersAndTheNotificationDoesNot pins the two halves of the
// opening exchange every client performs. The second half is the one worth a
// test: `notifications/initialized` carries no id, and a response to it would
// be a message the client has nothing to correlate with.
func TestTheHandshakeAnswersAndTheNotificationDoesNot(t *testing.T) {
	got := drive(t, options(), initLine, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if len(got) != 1 {
		t.Fatalf("want one response to two messages, got %d: a notification must never be answered", len(got))
	}
	var res initResult
	mustResult(t, got[0], &res)
	if res.ProtocolVersion != "2025-06-18" {
		t.Errorf("the server answered %q to a client asking for a version it supports; the handshake echoes a supported revision", res.ProtocolVersion)
	}
	if res.ServerInfo.Name != "telltale" || res.ServerInfo.Version != "test" {
		t.Errorf("serverInfo is %+v; a client's log must name the build that answered", res.ServerInfo)
	}
}

// TestAnUnknownProtocolVersionGetsThePinnedOne: the lifecycle rule is that a
// server which cannot meet the requested revision answers with one it supports.
// Answering with the client's own unknown string would be a claim to speak a
// revision nobody here has read.
func TestAnUnknownProtocolVersionGetsThePinnedOne(t *testing.T) {
	got := drive(t, options(), `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	var res initResult
	mustResult(t, got[0], &res)
	if res.ProtocolVersion != PinnedProtocolVersion {
		t.Errorf("got %q, want the pinned %q", res.ProtocolVersion, PinnedProtocolVersion)
	}
}

// TestTheToolListNamesTheOneTool. A client that cannot see the tool never calls
// it, which is the same failure shape as a mode missing from the usage text.
func TestTheToolListNamesTheOneTool(t *testing.T) {
	got := drive(t, options(), `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	var res toolsListResult
	mustResult(t, got[0], &res)
	if len(res.Tools) != 1 || res.Tools[0].Name != ToolName {
		t.Fatalf("tools/list returned %+v, want the one tool %s", res.Tools, ToolName)
	}
	// The description carries the document's honesty rules, and a model that
	// does not read them will read a null as a zero. That is the collapse §4a.1
	// forbids, moved one process outward into the agent.
	for _, want := range []string{"null", "estimated", "unsupported", "self_reported"} {
		if !strings.Contains(res.Tools[0].Description, want) {
			t.Errorf("the tool description never mentions %q; the calling model reads this and nothing else before it decides what a value means", want)
		}
	}
}

// TestTheToolResultIsTheSnapshotDocumentUnchanged is the whole point of this
// package.
//
// The bytes a client reads must be the bytes `telltale snapshot` prints, from
// the same Encode. If this ever fails, some second serializer has appeared —
// and a second serializer is where zero-vs-absent, the explicit nulls and the
// estimated/unsupported arrays would drift apart from the CLI's document.
func TestTheToolResultIsTheSnapshotDocumentUnchanged(t *testing.T) {
	got := drive(t, options(), callLine)
	var res callResult
	mustResult(t, got[0], &res)
	if res.IsError {
		t.Fatalf("a healthy call reported an error: %+v", res.Content)
	}
	want, err := snapshot.Encode(fixture(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 1 || res.Content[0].Text != string(want) {
		t.Errorf("the text content is not snapshot.Encode's output:\n got %q\nwant %q", res.Content[0].Text, want)
	}
	// structuredContent is the same document again. Encoding it back must give
	// the identical bytes, or the two spellings a client may read disagree.
	again, err := snapshot.Encode(*res.StructuredContent, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(want) {
		t.Errorf("structuredContent and the text content are different documents:\n got %q\nwant %q", again, want)
	}
}

// TestZeroAndAbsentSurviveTheToolCall asserts the property this repo exists to
// protect, on the new surface, in JSON TYPES rather than in bytes.
//
// The snapshot package already pins it for the document; this pins that nothing
// between Build and the client's parser collapsed it — a re-marshal through a
// map, an `omitempty` on the envelope reaching inside, or a text rendering that
// printed a null as 0.
func TestZeroAndAbsentSurviveTheToolCall(t *testing.T) {
	got := drive(t, options(), callLine)
	var res callResult
	mustResult(t, got[0], &res)

	var doc map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].Text), &doc); err != nil {
		t.Fatal(err)
	}
	fleet := doc["fleet"].(map[string]any)
	if _, present := fleet["cost_usd_total"]; !present {
		t.Fatal("cost_usd_total vanished; an absent reading is an explicit null, never a missing key")
	}
	if fleet["cost_usd_total"] != nil {
		t.Errorf("an absent fleet cost arrived as %v, want null", fleet["cost_usd_total"])
	}
	vendors := doc["vendors"].([]any)
	window := vendors[0].(map[string]any)["quota"].([]any)[0].(map[string]any)
	used, isNumber := window["used_pct"].(float64)
	if !isNumber || used != 0 {
		t.Errorf("a measured zero arrived as %#v; it must be the number 0", window["used_pct"])
	}
}

// TestABadVendorIsAResultTheModelCanRead, not a transport error it cannot.
//
// The distinction is the one `telltale snapshot` draws in its own flag
// handling: an input this surface cannot honour comes back with the correction
// in it. Here the corrector is the model, so the correction has to land where
// the model reads — in content, with isError set.
func TestABadVendorIsAResultTheModelCanRead(t *testing.T) {
	got := drive(t, options(), `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"fleet_snapshot","arguments":{"vendor":"chatgpt"}}}`)
	if _, isErr := got[0]["error"]; isErr {
		t.Fatalf("a bad argument came back as a JSON-RPC error: %s", got[0]["error"])
	}
	var res callResult
	mustResult(t, got[0], &res)
	if !res.IsError {
		t.Fatal("a bad vendor was reported as a successful call")
	}
	if res.StructuredContent != nil {
		t.Error("a failed call carried a document; there is no measurement behind it")
	}
	if !strings.Contains(res.Content[0].Text, "chatgpt") {
		t.Errorf("the refusal does not name what was wrong: %q", res.Content[0].Text)
	}
}

// TestAnUnknownToolIsAProtocolError, because it is the CLIENT's mistake rather
// than the model's: a client that calls a tool this server never listed is
// wired wrong, and the correction belongs in its plumbing.
func TestAnUnknownToolIsAProtocolError(t *testing.T) {
	got := drive(t, options(), `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"fleet_quota"}}`)
	var e rpcError
	mustError(t, got[0], &e)
	if e.Code != codeInvalidParams {
		t.Errorf("code %d, want %d", e.Code, codeInvalidParams)
	}
	if !strings.Contains(e.Message, ToolName) {
		t.Errorf("the refusal does not name the tool that does exist: %q", e.Message)
	}
}

// TestMalformedInputAnswersAndKeepsServing. A client that sends one bad line
// must not lose its session: the next request has to be answered.
func TestMalformedInputAnswersAndKeepsServing(t *testing.T) {
	got := drive(t, options(), `{"jsonrpc":"2.0",`, `{"jsonrpc":"2.0","id":9,"method":"ping"}`)
	if len(got) != 2 {
		t.Fatalf("want two responses, got %d: a parse error must not end the session", len(got))
	}
	var e rpcError
	mustError(t, got[0], &e)
	if e.Code != codeParseError {
		t.Errorf("code %d, want %d", e.Code, codeParseError)
	}
	if string(got[0]["id"]) != "null" {
		t.Errorf("id %s on a parse error, want null: there is no id to echo", got[0]["id"])
	}
	if string(got[1]["id"]) != "9" {
		t.Errorf("the second request was answered with id %s", got[1]["id"])
	}
}

// TestAnUnknownMethodNamesWhatThisServerHas. The surface is three methods wide
// on purpose, so the refusal has to say which three rather than leaving a
// client to probe.
func TestAnUnknownMethodNamesWhatThisServerHas(t *testing.T) {
	got := drive(t, options(), `{"jsonrpc":"2.0","id":7,"method":"resources/list"}`)
	var e rpcError
	mustError(t, got[0], &e)
	if e.Code != codeMethodNotFound {
		t.Errorf("code %d, want %d", e.Code, codeMethodNotFound)
	}
	for _, want := range []string{"initialize", "tools/list", "tools/call"} {
		if !strings.Contains(e.Message, want) {
			t.Errorf("the refusal never names %q: %q", want, e.Message)
		}
	}
}

// TestEveryFrameIsOneLine is the transport's own rule, and it is the one that
// breaks a client silently: a document with an embedded newline would be read
// as two frames, the first of them truncated JSON.
func TestEveryFrameIsOneLine(t *testing.T) {
	opt := options()
	opt.Fleet = func(context.Context, string) (snapshot.Document, error) {
		doc := fixture()
		msg := "the store refused:\nAccess is denied.\r\n"
		doc.ScanError = &msg
		return doc, nil
	}
	in := strings.NewReader(callLine + "\n")
	var out strings.Builder
	if err := Serve(context.Background(), in, &out, opt); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	if strings.Count(body, "\n") != 1 || !strings.HasSuffix(body, "\n") {
		t.Fatalf("a scan error with newlines in it broke the framing: %q", body)
	}
}

// TestAResponseFromTheClientIsIgnored. A message with an id and no method is a
// response to a request this server never sent. Answering it would itself be a
// protocol error, so the server must write nothing at all.
func TestAResponseFromTheClientIsIgnored(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":42,"result":{}}` + "\n")
	var out strings.Builder
	if err := Serve(context.Background(), in, &out, options()); err != nil {
		t.Fatal(err)
	}
	if out.String() != "" {
		t.Errorf("the server answered a response: %q", out.String())
	}
}

// TestServeRefusesWithoutAScan: a server with no Fleet function would advertise
// a tool it cannot answer, which is a capability claim with nothing behind it.
func TestServeRefusesWithoutAScan(t *testing.T) {
	if err := Serve(context.Background(), strings.NewReader(""), &strings.Builder{}, Options{}); err == nil {
		t.Fatal("Serve accepted a server that can answer nothing")
	}
}

func mustResult(t *testing.T, msg map[string]json.RawMessage, into any) {
	t.Helper()
	raw, ok := msg["result"]
	if !ok {
		t.Fatalf("no result in %v", msg)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("result %s: %v", raw, err)
	}
}

func mustError(t *testing.T, msg map[string]json.RawMessage, into *rpcError) {
	t.Helper()
	raw, ok := msg["error"]
	if !ok {
		t.Fatalf("no error in %v", msg)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("error %s: %v", raw, err)
	}
}
