package eventsink

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

func testEvent(app, session, typ string) Event {
	return Event{
		SourceApp:     app,
		SessionID:     session,
		HookEventType: typ,
		Payload:       json.RawMessage(`{"probe":true}`),
	}
}

func TestAddAssignsIDsAndPersists(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	s, err := Open(dir, DefaultRetain, fixedNow(now))
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Add(testEvent("appA", "sess-1", "PreToolUse"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Add(testEvent("appB", "sess-2", "Stop"))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != 1 || second.ID != 2 {
		t.Fatalf("want ids 1,2 — got %d,%d", first.ID, second.ID)
	}
	if first.TimestampMS != now.UnixMilli() {
		t.Fatalf("a POST without a timestamp must get the arrival time, got %d", first.TimestampMS)
	}

	// A reopened store continues the ID sequence rather than reusing one.
	s2, err := Open(dir, DefaultRetain, fixedNow(now))
	if err != nil {
		t.Fatal(err)
	}
	third, err := s2.Add(testEvent("appA", "sess-1", "PostToolUse"))
	if err != nil {
		t.Fatal(err)
	}
	if third.ID != 3 {
		t.Fatalf("reopened store must continue ids, got %d", third.ID)
	}
	if got := len(s2.Recent(10)); got != 3 {
		t.Fatalf("want 3 events after reload, got %d", got)
	}
}

func TestRecentIsNewestFirst(t *testing.T) {
	s, err := Open(t.TempDir(), DefaultRetain, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"a", "b", "c"} {
		if _, err := s.Add(testEvent("app", "sess", typ)); err != nil {
			t.Fatal(err)
		}
	}
	got := s.Recent(2)
	if len(got) != 2 || got[0].HookEventType != "c" || got[1].HookEventType != "b" {
		t.Fatalf("want newest first [c b], got %+v", got)
	}
}

func TestOptionsAreDistinctAndSorted(t *testing.T) {
	s, err := Open(t.TempDir(), DefaultRetain, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []Event{
		testEvent("beta", "s2", "Stop"),
		testEvent("alpha", "s1", "PreToolUse"),
		testEvent("beta", "s1", "Stop"),
	} {
		if _, err := s.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	opts := s.Options()
	if strings.Join(opts.SourceApps, ",") != "alpha,beta" {
		t.Fatalf("source_apps: %v", opts.SourceApps)
	}
	if strings.Join(opts.SessionIDs, ",") != "s1,s2" {
		t.Fatalf("session_ids: %v", opts.SessionIDs)
	}
	if strings.Join(opts.HookEventTypes, ",") != "PreToolUse,Stop" {
		t.Fatalf("hook_event_types: %v", opts.HookEventTypes)
	}
}

func TestValidateRefusesHalfAddressableEvents(t *testing.T) {
	cases := []Event{
		{SessionID: "s", HookEventType: "t", Payload: json.RawMessage(`{}`)},
		{SourceApp: "a", HookEventType: "t", Payload: json.RawMessage(`{}`)},
		{SourceApp: "a", SessionID: "s", Payload: json.RawMessage(`{}`)},
		{SourceApp: "a", SessionID: "s", HookEventType: "t"},
	}
	for i, e := range cases {
		if err := e.Validate(); err == nil {
			t.Fatalf("case %d: want a refusal, got nil", i)
		}
	}
}

func TestSweepDeletesExpiredDayFilesAndPrunesMemory(t *testing.T) {
	dir := t.TempDir()
	old := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	s, err := Open(dir, DefaultRetain, fixedNow(old))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(testEvent("app", "sess", "old")); err != nil {
		t.Fatal(err)
	}
	// Move the clock two months forward and add a fresh event, which lands in
	// a second day file.
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	s.now = fixedNow(now)
	if _, err := s.Add(testEvent("app", "sess", "fresh")); err != nil {
		t.Fatal(err)
	}

	deleted, err := s.Sweep()
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("want 1 day file deleted, got %d", deleted)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-06-01.jsonl")); !os.IsNotExist(err) {
		t.Fatal("the expired day file must be gone")
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-08-11.jsonl")); err != nil {
		t.Fatal("the fresh day file must survive:", err)
	}
	got := s.Recent(10)
	if len(got) != 1 || got[0].HookEventType != "fresh" {
		t.Fatalf("memory must hold only the fresh event, got %+v", got)
	}
}

func TestSweepNeverTouchesForeignFiles(t *testing.T) {
	dir := t.TempDir()
	stray := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(stray, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, time.Hour, fixedNow(time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sweep(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Fatal("a file that is not a day file must never be swept:", err)
	}
}

func TestOpenSkipsATornLine(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "2026-08-11.jsonl")
	good, _ := json.Marshal(Event{ID: 1, SourceApp: "a", SessionID: "s", HookEventType: "t",
		Payload: json.RawMessage(`{}`), TimestampMS: time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC).UnixMilli()})
	content := string(good) + "\n" + `{"id":2,"source_app":"torn` + "\n"
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, DefaultRetain, fixedNow(time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(s.Recent(10)); got != 1 {
		t.Fatalf("want the good line kept and the torn one skipped, got %d events", got)
	}
}

func TestPostRecentAndFilterOptionsEndToEnd(t *testing.T) {
	s, err := Open(t.TempDir(), DefaultRetain, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewServer(s, nil).Handler())
	defer srv.Close()

	body := `{"source_app":"telltale","session_id":"sess-e2e","hook_event_type":"PreToolUse",` +
		`"payload":{"tool_name":"Bash"},"tool_name":"Bash"}`
	resp, err := http.Post(srv.URL+"/events", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /events: %d %s", resp.StatusCode, b)
	}
	var stored Event
	if err := json.NewDecoder(resp.Body).Decode(&stored); err != nil {
		t.Fatal(err)
	}
	if stored.ID != 1 || stored.ToolName != "Bash" || stored.TimestampMS == 0 {
		t.Fatalf("stored row: %+v", stored)
	}

	resp2, err := http.Get(srv.URL + "/events/recent?limit=5")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var recent []Event
	if err := json.NewDecoder(resp2.Body).Decode(&recent); err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].SessionID != "sess-e2e" {
		t.Fatalf("recent: %+v", recent)
	}

	resp3, err := http.Get(srv.URL + "/events/filter-options")
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	var opts FilterOptions
	if err := json.NewDecoder(resp3.Body).Decode(&opts); err != nil {
		t.Fatal(err)
	}
	if len(opts.SourceApps) != 1 || opts.SourceApps[0] != "telltale" {
		t.Fatalf("filter-options: %+v", opts)
	}
}

func TestPostRefusesAnEventMissingATagAxis(t *testing.T) {
	s, err := Open(t.TempDir(), DefaultRetain, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewServer(s, nil).Handler())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/events", "application/json",
		strings.NewReader(`{"session_id":"s","hook_event_type":"t","payload":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

func TestRunRefusesANonLoopbackBind(t *testing.T) {
	s, err := Open(t.TempDir(), DefaultRetain, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewServer(s, nil).Run("0.0.0.0:0", 0); err == nil {
		t.Fatal("want a refusal for a non-loopback bind")
	}
}

// wsClient is a raw-socket websocket client for the tests: enough of the
// client side of RFC 6455 to shake hands and read server text frames.
type wsClient struct {
	conn net.Conn
	r    *bufio.Reader
}

func dialWS(t *testing.T, srvURL string) *wsClient {
	t.Helper()
	addr := strings.TrimPrefix(srvURL, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	req := "GET /stream HTTP/1.1\r\nHost: " + addr + "\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\nSec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("handshake: %d", resp.StatusCode)
	}
	sum := sha1.Sum([]byte(key + wsGUID))
	want := base64.StdEncoding.EncodeToString(sum[:])
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != want {
		t.Fatalf("Sec-WebSocket-Accept: got %q want %q", got, want)
	}
	return &wsClient{conn: conn, r: r}
}

// readText reads one server frame and asserts it is unmasked FIN text.
func (c *wsClient) readText(t *testing.T) []byte {
	t.Helper()
	c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var head [2]byte
	if _, err := io.ReadFull(c.r, head[:]); err != nil {
		t.Fatal(err)
	}
	if head[0] != 0x80|opText {
		t.Fatalf("want a FIN text frame, got header byte %#x", head[0])
	}
	if head[1]&0x80 != 0 {
		t.Fatal("server frames must be unmasked")
	}
	length := uint64(head[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.r, ext[:]); err != nil {
			t.Fatal(err)
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(c.r, ext[:]); err != nil {
			t.Fatal(err)
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.r, payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

// close sends a masked close frame, as a client must.
func (c *wsClient) close() {
	frame := []byte{0x80 | opClose, 0x80, 0, 0, 0, 0}
	c.conn.Write(frame)
	c.conn.Close()
}

func TestStreamSendsInitialThenBroadcastsInserts(t *testing.T) {
	s, err := Open(t.TempDir(), DefaultRetain, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(testEvent("app", "sess", "before-connect")); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewServer(s, nil).Handler())
	defer srv.Close()

	c := dialWS(t, srv.URL)
	defer c.close()

	var initial struct {
		Type string  `json:"type"`
		Data []Event `json:"data"`
	}
	if err := json.Unmarshal(c.readText(t), &initial); err != nil {
		t.Fatal(err)
	}
	if initial.Type != "initial" || len(initial.Data) != 1 || initial.Data[0].HookEventType != "before-connect" {
		t.Fatalf("initial: %+v", initial)
	}

	resp, err := http.Post(srv.URL+"/events", "application/json",
		strings.NewReader(`{"source_app":"app","session_id":"sess","hook_event_type":"after-connect","payload":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST: %d", resp.StatusCode)
	}

	var got struct {
		Type string `json:"type"`
		Data Event  `json:"data"`
	}
	if err := json.Unmarshal(c.readText(t), &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "event" || got.Data.HookEventType != "after-connect" {
		t.Fatalf("broadcast: %+v", got)
	}
	if !bytes.Equal(got.Data.Payload, []byte(`{}`)) {
		t.Fatalf("payload must arrive verbatim, got %s", got.Data.Payload)
	}
}
