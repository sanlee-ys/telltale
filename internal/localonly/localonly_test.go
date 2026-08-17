package localonly

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The header sets below are transcribed from the 2026-08-16 capture recorded
// in the package doc and design.md §7.24 — a headless Chrome 151 posting
// cross-origin to both listeners, and the two real senders posting to the same
// ports. They are the measurement, kept where a change to the check has to
// walk past them.

// browserFetch is what Chrome sent on a cross-origin fetch POST.
var browserFetch = map[string]string{
	"Content-Type":    "text/plain",
	"Origin":          "http://127.0.0.1:41999",
	"Sec-Fetch-Site":  "same-site",
	"Sec-Fetch-Mode":  "no-cors",
	"Sec-Fetch-Dest":  "empty",
	"Referer":         "http://127.0.0.1:41999/",
	"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) HeadlessChrome/151.0.0.0",
	"Accept-Encoding": "gzip, deflate, br, zstd",
}

// browserHandshake is what Chrome sent opening ws://127.0.0.1:41519/stream.
// Note what is NOT here: any Sec-Fetch-* header. This is the case that decides
// which header the check reads.
var browserHandshake = map[string]string{
	"Connection":            "Upgrade",
	"Upgrade":               "websocket",
	"Origin":                "http://127.0.0.1:41999",
	"Sec-WebSocket-Version": "13",
	"Sec-WebSocket-Key":     "4ZOd55kia3PCnu3Yjq52uw==",
	"User-Agent":            "Mozilla/5.0 (Windows NT 10.0; Win64; x64) HeadlessChrome/151.0.0.0",
}

// pythonEmitter is what tools/emit-event.py sent.
var pythonEmitter = map[string]string{
	"Accept-Encoding": "identity",
	"Host":            "127.0.0.1:41519",
	"User-Agent":      "Python-urllib/3.14",
	"Content-Type":    "application/json",
	"Connection":      "close",
}

// exporterShaped is what a request shaped like grok's exporter sent. §7.16a's
// live capture pins the same Content-Type on grok's own exporter.
var exporterShaped = map[string]string{
	"Accept-Encoding": "identity",
	"Host":            "127.0.0.1:41318",
	"User-Agent":      "Python-urllib/3.14",
	"Content-Type":    "application/x-protobuf",
	"Connection":      "close",
}

func request(headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestTheMeasuredBrowserRequestsAreRefused(t *testing.T) {
	for name, headers := range map[string]map[string]string{
		"fetch":     browserFetch,
		"handshake": browserHandshake,
	} {
		err := RefuseBrowser(request(headers))
		if err == nil {
			t.Fatalf("%s: a browser request was accepted", name)
		}
		// The refusal has to name the origin, or the operator watching the mode
		// run learns only that something was refused.
		if !strings.Contains(err.Error(), "http://127.0.0.1:41999") {
			t.Errorf("%s: the refusal does not name the origin: %v", name, err)
		}
	}
}

func TestTheMeasuredLocalSendersAreAccepted(t *testing.T) {
	for name, headers := range map[string]map[string]string{
		"emit-event.py":  pythonEmitter,
		"exporter shape": exporterShaped,
	} {
		if err := RefuseBrowser(request(headers)); err != nil {
			t.Errorf("%s: a local sender was refused: %v", name, err)
		}
	}
	if err := RequireContentType(request(pythonEmitter), "application/json"); err != nil {
		t.Errorf("emit-event.py content type: %v", err)
	}
	if err := RequireContentType(request(exporterShaped), "application/x-protobuf"); err != nil {
		t.Errorf("exporter content type: %v", err)
	}
}

// The three CORS-safelisted media types are the whole reason the browser push
// needed no preflight. None of them may pass either endpoint's requirement.
func TestNoCorsSafelistedMediaTypePasses(t *testing.T) {
	safelisted := []string{"text/plain", "application/x-www-form-urlencoded", "multipart/form-data"}
	for _, want := range []string{"application/json", "application/x-protobuf"} {
		for _, ct := range safelisted {
			if err := RequireContentType(request(map[string]string{"Content-Type": ct}), want); err == nil {
				t.Errorf("Content-Type %q passed a %q endpoint", ct, want)
			}
		}
	}
}

// A parameter is the sender's business; the media type is the claim.
func TestParametersAndCaseDoNotChangeTheMediaType(t *testing.T) {
	for _, ct := range []string{
		"application/json; charset=utf-8",
		"Application/JSON",
		"application/json;charset=UTF-8",
	} {
		if err := RequireContentType(request(map[string]string{"Content-Type": ct}), "application/json"); err != nil {
			t.Errorf("Content-Type %q was refused: %v", ct, err)
		}
	}
}

// A missing or unparseable header is refused, and the refusal names what to
// send — the error is the documentation a hand-rolled emitter actually reads.
func TestAMissingOrBrokenContentTypeIsRefusedAndSaysWhatToSend(t *testing.T) {
	for _, headers := range []map[string]string{
		{},
		{"Content-Type": "not a media type at all"},
	} {
		err := RequireContentType(request(headers), "application/json")
		if err == nil {
			t.Fatalf("headers %v were accepted", headers)
		}
		if !strings.Contains(err.Error(), "application/json") {
			t.Errorf("the refusal does not say what to send: %v", err)
		}
	}
}

// 403 and not 401: there is no credential that would help, on purpose. A 4xx
// also stops an OTLP exporter retrying, which a 5xx would not.
func TestARefusalIsA403(t *testing.T) {
	w := httptest.NewRecorder()
	Refuse(w, RefuseBrowser(request(browserFetch)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}
