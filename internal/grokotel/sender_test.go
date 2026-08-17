package grokotel

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// The tests in this file pin one property: a push that a web page could make
// is not counted. design.md §7.24 carries the measurement that forced them —
// a headless Chrome on another origin planted a row in this collector's cache
// with a cross-origin fetch, because the handler read neither Origin nor
// Content-Type and a text/plain body is a CORS-simple request.

// postWith is post() with the sender's headers under the test's control.
func postWith(t *testing.T, s *Server, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, req)
	return w
}

func wroteNothing(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a refused push wrote something: %v", entries)
	}
}

// A page cannot suppress Origin — the user agent attaches it — so its presence
// is the signal, and a well-formed body behind it changes nothing. The record
// posted here is the exact shape the live capture measured, so this test fails
// for the sender and not for the payload.
func TestAPushFromAWebPageIsNotCounted(t *testing.T) {
	s, dir := serve(t)
	body := logsBody(apiRequestRecord("aaaaaaaa-0000-4000-8000-000000000001", 1, 11, 22, 33, 44))
	w := postWith(t, s, "/v1/logs", body, map[string]string{
		"Content-Type": contentType,
		"Origin":       "https://a-page-you-visited.example",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	wroteNothing(t, dir)
}

// The measured browser push carried text/plain, which is why it needed no
// preflight. Requiring the exporter's own media type is what makes a page
// preflight at all, and this collector answers no preflight.
func TestACorsSimpleContentTypeIsNotCounted(t *testing.T) {
	s, dir := serve(t)
	body := logsBody(apiRequestRecord("aaaaaaaa-0000-4000-8000-000000000001", 1, 11, 22, 33, 44))
	for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", "multipart/form-data", ""} {
		headers := map[string]string{}
		if ct != "" {
			headers["Content-Type"] = ct
		}
		if w := postWith(t, s, "/v1/logs", body, headers); w.Code != http.StatusForbidden {
			t.Errorf("Content-Type %q = %d, want 403", ct, w.Code)
		}
	}
	wroteNothing(t, dir)
}

// The metrics path stores nothing, so this is not about a corrupted total. It
// is about the two handlers agreeing: an endpoint that answers a sender its
// sibling refuses is the door a later reader props open by copying the
// shorter handler.
func TestTheMetricsPathRefusesTheSameSenders(t *testing.T) {
	s, _ := serve(t)
	w := postWith(t, s, "/v1/metrics", []byte{}, map[string]string{
		"Content-Type": contentType,
		"Origin":       "https://a-page-you-visited.example",
	})
	if w.Code != http.StatusForbidden {
		t.Errorf("browser POST /v1/metrics = %d, want 403", w.Code)
	}
	if w := postWith(t, s, "/v1/metrics", []byte{}, map[string]string{"Content-Type": "text/plain"}); w.Code != http.StatusForbidden {
		t.Errorf("text/plain POST /v1/metrics = %d, want 403", w.Code)
	}
}

// The other half of the gate, and the half that would go unnoticed if it
// broke: the shape §7.16a measured grok's own exporter sending must still be
// counted, parameters and all. A charset parameter is the sender's business.
func TestTheMeasuredExporterShapeIsStillCounted(t *testing.T) {
	s, dir := serve(t)
	body := logsBody(apiRequestRecord("aaaaaaaa-0000-4000-8000-000000000001", 1, 11, 22, 33, 44))
	if w := postWith(t, s, "/v1/logs", body, map[string]string{"Content-Type": contentType}); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if e := readCache(t, dir); e.InputTokens != 11 || e.Requests != 1 {
		t.Fatalf("the measured exporter shape was not counted: %+v", e)
	}

	s2, dir2 := serve(t)
	if w := postWith(t, s2, "/v1/logs", body, map[string]string{"Content-Type": "application/x-protobuf; charset=utf-8"}); w.Code != http.StatusOK {
		t.Fatalf("a parameterised media type = %d, want 200", w.Code)
	}
	if e := readCache(t, dir2); e.InputTokens != 11 {
		t.Fatalf("a parameterised media type was not counted: %+v", e)
	}
}

// A refusal must end the exchange rather than make grok's exporter loop.
// 403 is a 4xx, and an OTLP exporter gives up on a 4xx — the same reasoning
// that made /v1/metrics answer 200 at all (§7.16a).
func TestARefusalIsA4xxSoTheExporterStopsRetrying(t *testing.T) {
	s, _ := serve(t)
	w := postWith(t, s, "/v1/logs", nil, map[string]string{"Origin": "https://a-page-you-visited.example"})
	if w.Code < 400 || w.Code >= 500 {
		t.Fatalf("status = %d, want a 4xx", w.Code)
	}
}
