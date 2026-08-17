// Package localonly is the check that both loopback listeners apply to every
// request: this request came from a program on this machine, not from a page
// in the operator's browser.
//
// # Why a loopback bind was not the whole containment
//
// `telltale otel grok` (design.md §7.16a) and `telltale events` (§7.21) both
// bind 127.0.0.1 and both treat that bind as the thing that contains them. It
// contains less than it reads like. A loopback port is reachable by every
// program on the box — which is the accepted half, and §7.24 argues it is the
// same trust the filesystem already grants — but it is ALSO reachable by a web
// page the operator merely visits, and a web page is not a program on this box
// at all. It cannot write ~/.telltale/usage/grok.json. It could push into it.
//
// That gap is what this package closes, and closing it is what makes the two
// paths' trust equal rather than merely stated to be.
//
// # What was measured, and against what
//
// 2026-08-16, Windows 11, telltale built from main at 65a113c, Chrome
// 151.0.0.0 headless, a probe page served from http://127.0.0.1:41999 against
// a collector on 41318 and a sink on 41519, both with a redirected home:
//
//   - A cross-origin fetch with a text/plain body reached BOTH listeners and
//     was stored by both. It needed no preflight, because neither handler read
//     Content-Type, and text/plain is one of the three CORS-safelisted values
//     — so the request is a "simple" one the browser sends outright.
//   - The same page opened ws://127.0.0.1:41519/stream and received the sink's
//     whole `initial` snapshot: every retained hook payload, verbatim. A
//     WebSocket handshake is exempt from CORS, so nothing about content types
//     or preflights was ever going to stop that one.
//   - Every browser request carried Origin. The fetches carried
//     `Origin: http://127.0.0.1:41999` beside `Sec-Fetch-Mode: no-cors`; the
//     WebSocket handshake carried the same Origin and NO Sec-Fetch-* header at
//     all. That asymmetry is why the check below reads Origin and not
//     Sec-Fetch-Site: Origin is the only header measured present in every
//     browser case, and a check that missed the handshake would miss the one
//     vector that reads content rather than writing numbers.
//   - Neither real sender carried Origin. tools/emit-event.py arrived as
//     `User-Agent: Python-urllib/3.14`, `Content-Type: application/json`, no
//     Origin and no Sec-Fetch-*; a request shaped like grok's exporter arrived
//     the same way with `application/x-protobuf`. §7.16a's live capture pins
//     grok's own exporter to that Content-Type.
//   - With a non-simple Content-Type the browser sent only an OPTIONS
//     preflight to each endpoint and NO POST followed, because neither server
//     answers a preflight with CORS headers. That is the second arm working
//     from the other side.
//
// # Why two arms and not one
//
// RefuseBrowser is the arm that generalizes: it names the sender class rather
// than a transport detail, and it is the only arm that can cover the WebSocket
// handshake. RequireContentType is the arm that holds when a future browser
// stops sending Origin on some path nobody has measured yet — a request the
// listeners accept is then a request no page can make simple, so the browser
// has to preflight, and the preflight is refused. Neither arm is load-bearing
// alone, and they fail in different directions, which is the whole reason to
// carry both.
//
// # What this deliberately does not do
//
// It is not authentication and it must not be read as any. A program running
// on this machine as a principal the store's ACL admits is still trusted
// completely, on purpose: it can write ~/.telltale/usage/grok.json directly,
// and design.md §7.24 records the measurement showing a plain file write
// plants the same row with MORE control than a POST does. A shared secret on
// the HTTP path would not change that principal's reach by one byte, so this
// package does not carry one.
package localonly

import (
	"fmt"
	"mime"
	"net/http"
	"strings"
)

// RefuseBrowser reports why a request may not be served when it came from a
// page in a browser.
//
// The whole check is the presence of Origin. No non-browser sender measured
// against these listeners sends it, and every browser request measured against
// them does — including the WebSocket opening handshake, which carries Origin
// and no Sec-Fetch-* header. A page cannot suppress the header: it is attached
// by the user agent, not by the script, which is what makes it usable as a
// signal at all.
//
// The refusal deliberately does NOT try to distinguish a "trusted" origin from
// an untrusted one. Nothing telltale ships runs in a browser, so every Origin
// is equally wrong, and an allowlist would only be a place for a later reader
// to add the entry that reopens this.
func RefuseBrowser(r *http.Request) error {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	return fmt.Errorf("refusing a request from a web page (Origin: %s). "+
		"This listener accepts pushes from programs on this machine only. "+
		"Nothing telltale ships runs in a browser, so an Origin header means a "+
		"page you visited reached this port; see design.md §7.24", origin)
}

// RequireContentType reports why a request may not be served when it does not
// carry the media type this endpoint's measured sender sends.
//
// Parameters are ignored, so `application/json; charset=utf-8` passes for
// `application/json`: the parameter is the sender's business and no claim here
// depends on it. A missing Content-Type is a refusal rather than a pass — the
// error names what to send, which is more use to a hand-rolled emitter than a
// silent accept, and browsers always send one anyway.
//
// The security value is indirect and worth stating plainly: requiring a media
// type outside the CORS-safelisted three (application/x-www-form-urlencoded,
// multipart/form-data, text/plain) means no page can reach this endpoint
// without a preflight, and neither listener answers a preflight. Measured —
// see the package doc.
func RequireContentType(r *http.Request, want string) error {
	got := r.Header.Get("Content-Type")
	if got == "" {
		return fmt.Errorf("refusing a request with no Content-Type; this endpoint wants %s", want)
	}
	media, _, err := mime.ParseMediaType(got)
	if err != nil {
		return fmt.Errorf("refusing a request whose Content-Type %q does not parse; this endpoint wants %s", got, want)
	}
	if !strings.EqualFold(media, want) {
		return fmt.Errorf("refusing a request with Content-Type %s; this endpoint wants %s", media, want)
	}
	return nil
}

// Refuse writes the one refusal shape both listeners use: 403, plain text, the
// reason on the wire.
//
// 403 rather than 400 because the payload may be perfectly well formed and the
// SENDER is the problem, and rather than 401 because there is no credential
// that would help — see the package doc on why no secret is carried here. It
// also matters that 403 is a 4xx: an OTLP exporter retries a 5xx and gives up
// on a 4xx, so a refusal ends the exchange instead of making grok's exporter
// loop against a door that will not open (§7.16a's reason for answering
// /v1/metrics at all).
func Refuse(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusForbidden)
}
