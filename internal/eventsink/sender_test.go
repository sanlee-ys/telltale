package eventsink

import (
	"bufio"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The tests in this file pin one property: a request a web page could make is
// refused on every endpoint, the stream included. design.md §7.24 carries the
// measurement that forced them, and the stream is the reason the check reads
// the REQUEST rather than the transport — a WebSocket handshake is exempt from
// CORS, and a headless Chrome on another origin was handed this sink's whole
// `initial` snapshot: every retained hook payload, verbatim.

const aPageYouVisited = "https://a-page-you-visited.example"

func sinkFor(t *testing.T) *httptest.Server {
	t.Helper()
	s, err := Open(t.TempDir(), DefaultRetain, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(Event{
		SourceApp:     "telltale",
		SessionID:     "sess-local",
		HookEventType: "PreToolUse",
		Payload:       []byte(`{"tool_name":"Bash","command":"a command a page must not read"}`),
		TimestampMS:   time.Now().UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewServer(s, nil).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func withOrigin(t *testing.T, method, url string, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", aPageYouVisited)
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// Every endpoint, not only the POST. A page that cannot plant a row can still
// ask for the rows already there, and these rows are content.
func TestAWebPageIsRefusedOnEveryEndpoint(t *testing.T) {
	srv := sinkFor(t)
	cases := []struct {
		method, path, body string
	}{
		{http.MethodPost, "/events", `{"source_app":"a","session_id":"b","hook_event_type":"c","payload":{}}`},
		{http.MethodGet, "/events/recent", ""},
		{http.MethodGet, "/events/filter-options", ""},
	}
	for _, c := range cases {
		resp := withOrigin(t, c.method, srv.URL+c.path, c.body)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403", c.method, c.path, resp.StatusCode)
		}
		payload, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(payload), "a command a page must not read") {
			t.Errorf("%s %s handed a stored payload to a web page", c.method, c.path)
		}
	}
}

// The endpoint that forced internal/localonly. CORS never applied here, so the
// refusal has to happen before the upgrade — and it must happen as an HTTP
// error rather than a 101 followed by a close, because a page that reaches
// onopen has already been handed the snapshot by the time anything else runs.
func TestTheStreamRefusesAWebPageBeforeTheUpgrade(t *testing.T) {
	srv := sinkFor(t)
	addr := strings.TrimPrefix(srv.URL, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	// The exact handshake a browser sends, measured 2026-08-16: Origin is
	// present and no Sec-Fetch-* header is.
	req := "GET /stream HTTP/1.1\r\nHost: " + addr + "\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Origin: " + aPageYouVisited + "\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\nSec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("handshake = %d, want 403", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "a command a page must not read") {
		t.Fatal("the refusal itself leaked a stored payload")
	}
}

// The other half of the gate: the shape tools/emit-event.py was measured
// sending must still be stored, and the stream a local client opens must still
// work. A guard that also stopped the sink working would be found by nobody
// until an event went missing.
func TestTheReferenceEmitterShapeIsStillStored(t *testing.T) {
	srv := sinkFor(t)
	body := `{"source_app":"probe","session_id":"sess-probe","hook_event_type":"PreToolUse","payload":{"tool_name":"Bash"}}`
	for _, ct := range []string{contentType, "application/json; charset=utf-8"} {
		resp, err := http.Post(srv.URL+"/events", ct, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Content-Type %q = %d, want 200", ct, resp.StatusCode)
		}
	}
	c := dialWS(t, srv.URL)
	defer c.conn.Close()
	if msg := c.readText(t); !strings.Contains(string(msg), `"initial"`) {
		t.Fatalf("a local stream client got %q", msg)
	}
}

// A POST with a CORS-simple media type is refused, which is what makes a page
// preflight — and this sink answers no preflight.
func TestACorsSimpleContentTypeIsNotStored(t *testing.T) {
	srv := sinkFor(t)
	body := `{"source_app":"a","session_id":"b","hook_event_type":"c","payload":{}}`
	for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", "multipart/form-data"} {
		resp, err := http.Post(srv.URL+"/events", ct, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Content-Type %q = %d, want 403", ct, resp.StatusCode)
		}
	}
}
