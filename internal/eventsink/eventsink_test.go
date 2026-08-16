package eventsink

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/bindaddr"
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

// --- the port a sink you already started is holding ------------------------

// newServer is the pair every bind test below needs: a server over a temp
// store, and the store directory so a test can assert nothing was written.
func newServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := Open(dir, DefaultRetain, nil)
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(store, nil), dir
}

// freePort returns a loopback port nothing holds right now, by binding one and
// letting it go. It is a starting point, not a reservation — the same honesty
// bindaddr.Next's doc states — and that is fine for a test that binds it again
// immediately.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	return port
}

func TestDefaultAddrCarriesTheDefaultPort(t *testing.T) {
	_, port, err := net.SplitHostPort(DefaultAddr)
	if err != nil {
		t.Fatal(err)
	}
	if port != defaultPort {
		t.Errorf("DefaultAddr port = %q, defaultPort = %q: the collision message reads the wrong one", port, defaultPort)
	}
}

// A held port is a startup failure this mode meets whenever a sink is already
// running, and before 2026-08-16 it arrived as the raw net.Listen error
// (listen's doc has the measured line). The test pins every fact the operator
// needs out of it, because dropping any one of them leaves a sink that listens
// and stores nothing.
func TestAHeldPortSaysWhoProbablyHasItAndWhatElseToMove(t *testing.T) {
	port := freePort(t)
	addr := net.JoinHostPort("127.0.0.1", port)
	holder, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("could not hold %s for the test: %v", addr, err)
	}
	defer holder.Close()

	s, dir := newServer(t)
	ln, err := s.listen(addr)
	if err == nil {
		ln.Close()
		t.Fatal("listen succeeded on a held port")
	}
	msg := err.Error()
	for _, want := range []string{
		addr,                    // which address failed
		"already in use",        // what happened
		"--addr",                // how to move this side
		"--server-url",          // how to move the emitters
		"stores nothing",        // why moving only one side is not enough
		"exits 0",               // why that failure is silent rather than loud
		".claude/settings.json", // where the emitter edit is made
		"tools/emit-event.py",   // which command carries it
		"§7.21",                 // where the record is
		"bind error:",           // the raw error is kept, not swallowed
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the collision message never says %q:\n%s", want, msg)
		}
	}
	// The way out it offers must be a different port from the one that failed.
	suggested := bindaddr.Next("127.0.0.1", port, defaultPort)
	if suggested == addr || !strings.Contains(msg, suggested) {
		t.Errorf("the message suggests no port other than the failed one (%s):\n%s", addr, msg)
	}
	// A refused bind is not a start: nothing may reach the store behind it.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a failed bind wrote %d file(s) into the store", len(entries))
	}
}

// On this mode's own default the message may name the likely holder, and the
// holder it names is NOT the collector's. 4318 is OTLP/HTTP's registered port,
// so §7.16a can blame a rival receiver; 4519 is telltale's own, so the only
// defensible guess is a second copy of this mode. On a port the operator chose
// it guesses nothing, because telltale knows nothing about who took it.
func TestTheDefaultPortCollisionNamesASinkAlreadyRunning(t *testing.T) {
	shared := portTaken(DefaultAddr, defaultPort, "127.0.0.1:4520", errors.New("bind: taken")).Error()
	if !strings.Contains(shared, "telltale events sink you already started") {
		t.Errorf("the default-port collision does not name the likely holder:\n%s", shared)
	}
	if !strings.Contains(shared, "127.0.0.1:4520") {
		t.Errorf("the default-port collision suggests no port to move to:\n%s", shared)
	}
	// The advice that costs nothing to follow comes first: a sink already
	// listening is a sink already collecting, and a second one buys nothing.
	if !strings.Contains(shared, "Check for one before you move") {
		t.Errorf("the default-port collision never says to look for the running sink first:\n%s", shared)
	}

	chosen := portTaken("127.0.0.1:4600", "4600", "127.0.0.1:4601", errors.New("bind: taken")).Error()
	if strings.Contains(chosen, "already started probably holds it") {
		t.Errorf("a chosen port's collision guesses at a holder telltale cannot know:\n%s", chosen)
	}
	if !strings.Contains(chosen, "cannot say which one") {
		t.Errorf("a chosen port's collision does not admit it cannot name the holder:\n%s", chosen)
	}
}

// The way out the message offers has to work: a moved port binds, and the sink
// on it stores a real event.
func TestAMovedPortBindsAndStoresAnEvent(t *testing.T) {
	s, dir := newServer(t)
	addr := net.JoinHostPort("127.0.0.1", freePort(t))
	ln, err := s.listen(addr)
	if err != nil {
		t.Fatalf("listen(%q) = %v", addr, err)
	}
	defer ln.Close()
	if ln.Addr().String() != addr {
		t.Errorf("listening on %s, asked for %s", ln.Addr(), addr)
	}
	go http.Serve(ln, s.Handler())

	body := `{"source_app":"telltale","session_id":"sess-moved-port","hook_event_type":"PreToolUse",` +
		`"payload":{"tool_name":"Bash"},"tool_name":"Bash"}`
	resp, err := http.Post("http://"+addr+"/events", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /events on the moved port: %d %s", resp.StatusCode, b)
	}
	var stored Event
	if err := json.NewDecoder(resp.Body).Decode(&stored); err != nil {
		t.Fatal(err)
	}
	if stored.ID != 1 || stored.SessionID != "sess-moved-port" {
		t.Errorf("an event posted to the moved port was not stored: %+v", stored)
	}
	// Durable, not just acknowledged — the moved sink writes its day file the
	// same way the default one does.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the moved sink wrote %d day file(s), want 1", len(entries))
	}
}

// The loopback rule is absolute, and moving the port may not become a way
// around it: a non-loopback host is refused whatever port it carries. This
// matters more here than for the collector — these rows carry hook payloads
// verbatim (§7.21), so an off-box bind would publish the fleet's tool calls
// and file paths, not a token count.
func TestAMovedPortIsStillLoopbackOnly(t *testing.T) {
	s, _ := newServer(t)
	for _, addr := range []string{"0.0.0.0:4520", "192.168.1.10:4600", "[::]:9999"} {
		ln, err := s.listen(addr)
		if err == nil {
			ln.Close()
			t.Errorf("listen(%q) bound off loopback", addr)
			continue
		}
		if !strings.Contains(err.Error(), "loopback") {
			t.Errorf("listen(%q) = %v, want a loopback refusal", addr, err)
		}
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
