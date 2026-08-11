package grokotel

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/usagecache"
)

var pinned = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

// --- wire builders -------------------------------------------------------
//
// The fixtures are ASSEMBLED, not captured: the live payload carries San's
// real session, user and team uuids, and this repository is public. The
// builders encode the exact wire shapes the capture measured (otlp.go's field
// numbers), with synthesized values.

func tag(num, wire uint64) []byte {
	var buf [10]byte
	n := binary.PutUvarint(buf[:], num<<3|wire)
	return buf[:n]
}

func varint(v uint64) []byte {
	var buf [10]byte
	n := binary.PutUvarint(buf[:], v)
	return buf[:n]
}

func msg(num uint64, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	return bytes.Join([][]byte{tag(num, 2), varint(uint64(len(body))), body}, nil)
}

func str(num uint64, s string) []byte {
	return bytes.Join([][]byte{tag(num, 2), varint(uint64(len(s))), []byte(s)}, nil)
}

func vint(num, v uint64) []byte {
	return bytes.Join([][]byte{tag(num, 0), varint(v)}, nil)
}

// attrStr is KeyValue{key, AnyValue{string_value}}.
func attrStr(key, val string) []byte {
	return msg(6, str(1, key), msg(2, str(1, val)))
}

// attrInt is KeyValue{key, AnyValue{int_value}}.
func attrInt(key string, val uint64) []byte {
	return msg(6, str(1, key), msg(2, vint(3, val)))
}

// rec joins one LogRecord's fields into the record's bytes.
func rec(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

// logsBody wraps records into ExportLogsServiceRequest → ResourceLogs →
// ScopeLogs, the nesting the capture measured.
func logsBody(records ...[]byte) []byte {
	var wrapped [][]byte
	for _, r := range records {
		wrapped = append(wrapped, msg(2, r))
	}
	return msg(1, msg(2, bytes.Join(wrapped, nil)))
}

// apiRequestRecord builds one grok_code.api_request LogRecord with the four
// counts, a session id and a sequence — the complete measured shape.
func apiRequestRecord(session string, seq, in, out, reasoning, cacheRead uint64) []byte {
	return rec(
		attrInt("event.sequence", seq),
		attrStr("session.id", session),
		attrStr("model", "grok-4.5"),
		attrInt("duration_ms", 2112),
		attrStr("stop_reason", "end_turn"),
		attrInt("input_tokens", in),
		attrInt("output_tokens", out),
		attrInt("reasoning_tokens", reasoning),
		attrInt("cache_read_tokens", cacheRead),
		attrStr("user.id", "cccccccc-1111-4222-8333-000000000001"),
		attrStr("team.id", "dddddddd-4444-4555-8666-000000000002"),
		str(12, "grok_code.api_request"),
	)
}

func serve(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	s := New(dir, nil)
	s.now = func() time.Time { return pinned }
	return s, dir
}

func post(t *testing.T, s *Server, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, req)
	return w
}

func readCache(t *testing.T, dir string) usagecache.Entry {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "grok.json"))
	if err != nil {
		t.Fatal(err)
	}
	var e usagecache.Entry
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	return e
}

// --- the accumulation path ----------------------------------------------

func TestAnApiRequestIsCountedIntoTheCache(t *testing.T) {
	s, dir := serve(t)
	w := post(t, s, "/v1/logs", logsBody(
		apiRequestRecord("aaaaaaaa-0000-4000-8000-000000000001", 3, 20323, 56, 42, 2560),
	))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/x-protobuf" {
		t.Errorf("content type = %q", ct)
	}
	e := readCache(t, dir)
	if e.Requests != 1 || e.Turns != 0 {
		t.Errorf("window = %+v, want 1 request and no turns", e)
	}
	if e.InputTokens != 20323 || e.OutputTokens != 56 || e.CacheReadTokens != 2560 {
		t.Errorf("totals = %+v", e)
	}
	if e.ReasoningTokens == nil || *e.ReasoningTokens != 42 {
		t.Errorf("reasoning = %v, want 42", e.ReasoningTokens)
	}
	if e.CacheWriteTokens != nil {
		t.Errorf("a grok entry grew a cache-write count: %d", *e.CacheWriteTokens)
	}
}

func TestTwoRequestsAccumulate(t *testing.T) {
	s, dir := serve(t)
	post(t, s, "/v1/logs", logsBody(
		apiRequestRecord("aaaaaaaa-0000-4000-8000-000000000001", 3, 100, 10, 5, 1000),
	))
	post(t, s, "/v1/logs", logsBody(
		apiRequestRecord("aaaaaaaa-0000-4000-8000-000000000001", 4, 200, 20, 7, 2000),
	))
	e := readCache(t, dir)
	if e.Requests != 2 || e.InputTokens != 300 || e.OutputTokens != 30 ||
		e.CacheReadTokens != 3000 || e.ReasoningTokens == nil || *e.ReasoningTokens != 12 {
		t.Errorf("totals = %+v", e)
	}
}

// The exporter retries an unacknowledged batch, and a retried batch counted
// twice is an overstated total. Same session, same sequence: counted once.
func TestAReplayedBatchIsCountedOnce(t *testing.T) {
	s, dir := serve(t)
	body := logsBody(apiRequestRecord("aaaaaaaa-0000-4000-8000-000000000001", 3, 100, 10, 5, 1000))
	post(t, s, "/v1/logs", body)
	post(t, s, "/v1/logs", body)
	e := readCache(t, dir)
	if e.Requests != 1 || e.InputTokens != 100 {
		t.Errorf("a replay was double-counted: %+v", e)
	}
	// The same sequence in a DIFFERENT session is a different request.
	post(t, s, "/v1/logs", logsBody(
		apiRequestRecord("bbbbbbbb-0000-4000-8000-000000000002", 3, 50, 5, 1, 500),
	))
	if e := readCache(t, dir); e.Requests != 2 || e.InputTokens != 150 {
		t.Errorf("a distinct session's request was refused as a replay: %+v", e)
	}
}

// A record missing any of the four counts is refused whole — §7.16's partial
// gate, on the wire this vendor actually sends.
func TestAnIncompleteRequestIsNotCounted(t *testing.T) {
	s, dir := serve(t)
	partial := rec(
		attrStr("session.id", "aaaaaaaa-0000-4000-8000-000000000001"),
		attrInt("event.sequence", 3),
		attrInt("input_tokens", 100),
		attrInt("output_tokens", 10),
		attrInt("cache_read_tokens", 1000),
		// reasoning_tokens deliberately absent
		str(12, "grok_code.api_request"),
	)
	if w := post(t, s, "/v1/logs", logsBody(partial)); w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "grok.json")); !os.IsNotExist(err) {
		t.Fatal("an incomplete request reached the cache")
	}
}

// Only grok_code.api_request is a countable claim. A turn_completed record —
// which carries no counts — and any other event walk past unread even when a
// count-shaped attribute is planted on them.
func TestOtherEventsAreNotCounted(t *testing.T) {
	s, dir := serve(t)
	turnDone := rec(
		attrInt("duration_ms", 2131),
		attrStr("outcome", "completed"),
		// planted counts that a real turn_completed never carries: the event
		// gate, not the attribute set, is what must refuse them.
		attrInt("input_tokens", 999999),
		attrInt("output_tokens", 999999),
		attrInt("reasoning_tokens", 999999),
		attrInt("cache_read_tokens", 999999),
		str(12, "grok_code.turn_completed"),
	)
	if w := post(t, s, "/v1/logs", logsBody(turnDone)); w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "grok.json")); !os.IsNotExist(err) {
		t.Fatal("a non-api_request event was counted")
	}
}

// --- the refusals --------------------------------------------------------

func TestMalformedProtobufIsA400(t *testing.T) {
	s, dir := serve(t)
	if w := post(t, s, "/v1/logs", []byte{0xff, 0xff, 0xff}); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "grok.json")); !os.IsNotExist(err) {
		t.Fatal("a malformed payload reached the cache")
	}
}

func TestMetricsAreAcknowledgedAndDiscarded(t *testing.T) {
	s, dir := serve(t)
	// A well-formed metrics body would be redundant with the events; even a
	// garbage one is acknowledged, because the refusal that matters is "not
	// read", not "not parsed".
	if w := post(t, s, "/v1/metrics", []byte{0x01, 0x02}); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("the metrics path wrote something: %v", entries)
	}
}

func TestUnknownPathsAndMethodsAreRefused(t *testing.T) {
	s, _ := serve(t)
	if w := post(t, s, "/v1/traces", []byte{}); w.Code != http.StatusNotFound {
		t.Errorf("POST /v1/traces = %d, want 404", w.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/logs", nil)
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /v1/logs = %d, want 405", w.Code)
	}
}

func TestAGzipBodyIsAccepted(t *testing.T) {
	s, dir := serve(t)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write(logsBody(apiRequestRecord("aaaaaaaa-0000-4000-8000-000000000001", 1, 7, 8, 9, 10)))
	gz.Close()
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if e := readCache(t, dir); e.InputTokens != 7 {
		t.Errorf("totals = %+v", e)
	}
}

func TestTheBindRefusesNonLoopback(t *testing.T) {
	s, _ := serve(t)
	for _, addr := range []string{"0.0.0.0:4318", "192.168.1.10:4318", "[::]:4318"} {
		if err := s.Run(addr); err == nil || !strings.Contains(err.Error(), "loopback") {
			t.Errorf("Run(%q) = %v, want a loopback refusal", addr, err)
		}
	}
}

// --- nothing but numbers reaches disk ------------------------------------

// The wire carries uuids on every record, and would carry prompt text if the
// grok-side content gate were ever opened. The planted markers cover both:
// everything content-shaped on a REAL record shape, plus a user_prompt event
// the way the gate would send it. None of it may survive to the file.
func TestNothingFromTheWireReachesDisk(t *testing.T) {
	s, dir := serve(t)
	promptEvent := rec(
		attrInt("event.sequence", 2),
		attrStr("session.id", "eeeeeeee-0000-4000-8000-00SECRET0003"),
		attrStr("prompt", "SECRET-PROMPT-TEXT"),
		attrInt("prompt_length", 18),
		str(12, "grok_code.user_prompt"),
	)
	api := rec(
		apiRequestRecord("eeeeeeee-0000-4000-8000-00SECRET0003", 3, 11, 22, 33, 44),
		attrStr("file_path", `C:\Users\SECRET-USER\code\x`),
	)
	if w := post(t, s, "/v1/logs", logsBody(promptEvent, api)); w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "grok.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{
		"SECRET", "prompt", "grok-4.5", "eeeeeeee", "cccccccc", "dddddddd",
		"session", "user", "team", "model", "duration",
	} {
		if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(marker)) {
			t.Errorf("marker %q reached the cache file:\n%s", marker, raw)
		}
	}
	if !strings.Contains(string(raw), "11") || !strings.Contains(string(raw), "44") {
		t.Errorf("the counts did not reach the cache file:\n%s", raw)
	}
}
