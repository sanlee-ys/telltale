// Package grokotel is the receiving end of grok's external OpenTelemetry
// export: a loopback-only OTLP/HTTP listener that folds the token counts of
// each grok_code.api_request event into ~/.telltale/usage/grok.json — the
// same cache, mechanism and refusal rules as the Cursor token relay
// (internal/usagecache, design.md §7.16), fed by a push instead of a hook.
//
// # Why a listener at all, and why that does not breach §4a.5
//
// grok writes no cumulative spend to disk and no quota anywhere (§3.9a: the
// disk swept twice, the free network half probed, the vendor's own monitoring
// schema read). The one place it is DESIGNED to report what an account is
// doing is this export stream — documented in its user guide 24
// ("Monitoring Usage"), double opt-in, content-free by default. §4a.5's
// contract stands untouched because the PUSH IS GROK'S: telltale opens a
// socket on 127.0.0.1 and grok's own exporter connects out to it. The gauges
// still make no network calls and read no credentials; the collector is its
// own mode (`telltale otel grok`) for the same reason `telltale hook cursor`
// is — its I/O contract (a listening socket, a foreground process) belongs to
// neither gauge, and the gauges read only the FILE it writes.
//
// # What was measured, and against what
//
// Live capture, 2026-08-10, grok 1.0.0 (3cd0d0cbce) on Windows 11, headless
// `grok -p "hi"` with GROK_EXTERNAL_OTEL=1 and both exporters selected,
// against a dump collector on 127.0.0.1:4318 (§3.9a's export addendum has the
// full record):
//
//   - transport: OTLP http/protobuf POSTs to /v1/logs and /v1/metrics,
//     Content-Type application/x-protobuf, uncompressed, from
//     OTel-OTLP-Exporter-Rust/0.32.0. Flushed before the headless process
//     exited.
//   - grok_code.api_request events carry input_tokens, output_tokens,
//     reasoning_tokens and cache_read_tokens as int attributes, beside
//     session.id and a per-session monotonic event.sequence.
//   - the grok_code.token.usage metric carries THE SAME four counts (delta
//     temporality, by model and type) — value-for-value equal to the
//     api_request event of the same turn. That equality is why this package
//     reads ONLY the event stream and acknowledges /v1/metrics without
//     reading it: one source, one claim, no chance of double-counting a
//     number that arrives twice.
//   - turn_completed events carry outcome and duration and NO token counts,
//     exactly as the vendor's schema table says.
//
// **NOT re-measured at grok 1.0.4 (2026-08-14).** The seat was re-verified when
// four unnoticed patch bumps turned up (design.md §9.39's amendment), and that
// pass covered the council wire, the flags and the on-disk store — it did NOT
// re-run this export capture, which needs its own instrumented turn. Two things
// were checked from outside and neither moves a verdict here: quota is still
// structurally absent (a rate/limit/quota sweep of a store 1.0.4 wrote matches
// nothing account-level, so #195's closed-enum finding stands), and 1.0.4 has a
// `grok trace` subcommand that exports or uploads a session's trace data, which
// moves a transcript rather than reporting a ceiling. Nothing here is built on
// it. The bullets above stay pinned to 1.0.0 until somebody re-runs them.
//
// # What of the stream is kept
//
// Four counts, and nothing else. The record's model, duration, stop reason,
// user.id and team.id have no destination in the parse; session.id and
// event.sequence are read into collector MEMORY for duplicate refusal (the
// exporter retries on failure, and a total that counts a retried batch twice
// is overstated by an amount nothing can name) and are never written to the
// cache file. The file stays what §7.16 made it: totals, a window count, two
// timestamps, a vendor id.
//
// # What this deliberately is not
//
// It is not quota. The export carries no window, no reset, no limit;
// subscription tier is on its never-exported list (§3.9a). Nothing arriving
// here may ever render as a percentage, a gauge or a countdown, and the
// display of the total this cache accumulates is HELD in the same ruling that
// retired Cursor's (§7.16's amendment): a running count for a vendor with no
// ceiling buys no decision. The relay runs, the file accrues history, and
// reinstating a display is a call site — not a re-plumb.
package grokotel

import (
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/usagecache"
)

// DefaultAddr is OTLP/HTTP's registered default port on loopback — the
// address grok's exporter posts to when OTEL_EXPORTER_OTLP_ENDPOINT is unset,
// so the zero-flag pairing of `telltale otel grok` and a bare grok session
// finds itself.
const DefaultAddr = "127.0.0.1:4318"

// maxBody caps one POST. The largest measured batch is under 2 KB; the cap is
// generous headroom, not a tuned figure, and a body past it is refused rather
// than half-read.
const maxBody = 8 << 20

// seenHorizon is how long a session's replay guard outlives its last record.
// It matches the cache's own 24h expiry: a duplicate arriving after the total
// it would corrupt has already expired can only start a fresh window, which
// is the same thing a first delivery would do.
const seenHorizon = 24 * time.Hour

// maxTrackedSessions and maxTrackedSeqs bound the replay guard's memory. This
// is a long-lived foreground server on a port every local process can reach,
// so "one entry per record forever" is not an acceptable growth curve. Both
// bounds are far past anything measured (one headless turn = one session and
// single-digit sequences); crossing one sheds the OLDEST guard state, which
// weakens duplicate refusal for exactly the records old enough that the
// exporter's retry horizon has long passed them.
const (
	maxTrackedSessions = 1024
	maxTrackedSeqs     = 1 << 16
)

// Server accumulates api_request counts into one vendor cache file. It is a
// single process holding a single mutex, which quietly retires §7.16's known
// read-modify-write race for this vendor: every Add on grok.json goes through
// here serialized.
type Server struct {
	dir  string
	now  func() time.Time
	logf func(format string, args ...any)

	mu   sync.Mutex
	seen map[string]*sessionSeen
}

// sessionSeen is one session's replay guard: the set of event sequences
// already counted. A set rather than a high-water mark so a batch delivered
// late, after a younger batch already landed, is still counted once rather
// than refused as "old" — the exporter retries in order, but the guard should
// not depend on that.
type sessionSeen struct {
	seqs map[uint64]struct{}
	last time.Time
}

// New returns a Server writing to dir (usagecache.Dir() in production; a
// temp dir in tests). logf receives one line per counted request and one per
// refusal, for the human watching the mode run; nil silences it.
func New(dir string, logf func(string, ...any)) *Server {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Server{dir: dir, now: time.Now, logf: logf, seen: map[string]*sessionSeen{}}
}

// Run listens on addr and serves until the process is killed. The bind is
// refused unless addr is loopback: this mode exists to receive one local
// process's push, and a collector reachable off-box would be an open door
// wearing a gauge's name.
func (s *Server) Run(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("bad listen address %q: %w", addr, err)
	}
	if !isLoopback(host) {
		return fmt.Errorf("refusing to listen on %q: the collector binds loopback only (127.0.0.1, ::1, localhost)", addr)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.logf("telltale otel grok: listening on %s, writing %s", ln.Addr(), filepath.Join(s.dir, "grok.json"))
	// Full read and idle timeouts, not just headers: a local process that
	// trickles a body forever would otherwise hold a connection open
	// indefinitely. The measured exporter completes a POST in milliseconds.
	server := &http.Server{
		Handler:           s.handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	return server.Serve(ln)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// handler routes the two OTLP paths. Everything else is a 404: this is not a
// general collector, and answering paths it does not understand would only
// disguise a misconfigured exporter as a working one.
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/logs", s.handleLogs)
	mux.HandleFunc("/v1/metrics", s.handleMetrics)
	return mux
}

// handleMetrics acknowledges and discards. The token.usage metric restates
// the api_request counts value-for-value (measured — package doc), so
// reading both would be two chances to count one number. The 200 matters:
// an unacknowledged export is retried, and a collector that made the
// exporter loop on a signal it never reads would be spending grok's batches
// on nothing.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	io.Copy(io.Discard, io.LimitReader(r.Body, maxBody))
	respondOK(w)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := readBody(r)
	if err != nil {
		// 400, not 500: a malformed payload is the sender's fact, and OTLP
		// exporters do not retry a 400 — the honest refusal is also the one
		// that does not make the exporter loop.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reqs, err := apiRequests(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, req := range reqs {
		s.count(req)
	}
	respondOK(w)
}

func readBody(r *http.Request) ([]byte, error) {
	var rd io.Reader = http.MaxBytesReader(nil, r.Body, maxBody)
	// The measured exporter sends identity; gzip is the one encoding the OTLP
	// spec expects a receiver to take, so it is taken.
	if r.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(rd)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		// The cap must hold on the DECOMPRESSED side too: gzip expands up to
		// ~1000:1, so a small compressed POST could otherwise balloon past
		// the cap in io.ReadAll and take the collector down — and any local
		// process can reach this port, which is the same threat the loopback
		// bind exists for. One byte of headroom turns "at the cap" into a
		// detectable "past the cap".
		rd = io.LimitReader(gz, maxBody+1)
	}
	body, err := io.ReadAll(rd)
	if err != nil {
		return nil, err
	}
	if len(body) > maxBody {
		return nil, fmt.Errorf("body larger than %d bytes", maxBody)
	}
	return body, nil
}

// respondOK is the OTLP success shape: 200 with an empty Export*Response
// message, which serializes to zero bytes. Measured sufficient — the live
// exporter kept exporting against exactly this reply.
func respondOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
}

// count applies the §7.16 gates in order — complete, nonnegative, not a
// replay — and folds one api_request into the running total.
func (s *Server) count(r apiRequest) {
	if !r.complete() {
		s.logf("telltale otel grok: api request with incomplete counts; not counted")
		return
	}
	if !r.nonnegative() {
		s.logf("telltale otel grok: api request with a negative count; not counted")
		return
	}
	now := s.now()

	s.mu.Lock()
	if s.isReplay(r, now) {
		s.mu.Unlock()
		return
	}
	// The delta copies the parsed values so nothing the request sliced out of
	// the POST body outlives this call.
	reasoning := *r.reasoning
	d := usagecache.Delta{
		Requests:        1,
		InputTokens:     *r.input,
		OutputTokens:    *r.output,
		CacheReadTokens: *r.cacheRead,
		ReasoningTokens: &reasoning,
		// CacheWriteTokens stays nil: grok's export has no cache-write type,
		// and nil is how the cache says so (§4a.1).
	}
	err := usagecache.Add(s.dir, string(model.VendorGrok), d, now)
	s.mu.Unlock()

	if err != nil {
		s.logf("telltale otel grok: %v", err)
		return
	}
	s.logf("telltale otel grok: counted api request — in %d · out %d · reasoning %d · cache read %d",
		*r.input, *r.output, *r.reasoning, *r.cacheRead)
}

// isReplay consults and updates the replay guard. Caller holds s.mu.
//
// A record without both ids is counted WITHOUT the guard rather than refused:
// the ids exist on every measured record, and if a later grok stops sending
// them the honest failure is a counter exposed to duplicate retries, not one
// that silently goes quiet. The guard is memory-only — a collector restart
// forgets it, and a batch retried across that restart double-counts. Bounded,
// and recorded in §3.9a's export addendum rather than hidden.
func (s *Server) isReplay(r apiRequest, now time.Time) bool {
	if r.sessionID == "" || !r.hasSeq {
		return false
	}
	for id, sess := range s.seen {
		if now.Sub(sess.last) > seenHorizon {
			delete(s.seen, id)
		}
	}
	sess := s.seen[r.sessionID]
	if sess == nil {
		if len(s.seen) >= maxTrackedSessions {
			s.evictOldestSession()
		}
		sess = &sessionSeen{seqs: map[uint64]struct{}{}}
		s.seen[r.sessionID] = sess
	}
	sess.last = now
	if _, dup := sess.seqs[r.sequence]; dup {
		s.logf("telltale otel grok: replayed batch; api request already counted")
		return true
	}
	if len(sess.seqs) >= maxTrackedSeqs {
		// Reset rather than grow: the sequences shed are tens of thousands of
		// records old, far past any retry.
		sess.seqs = map[uint64]struct{}{}
	}
	sess.seqs[r.sequence] = struct{}{}
	return false
}

// evictOldestSession drops the guard state with the oldest last-seen time.
// Caller holds s.mu and has already pruned everything past seenHorizon.
func (s *Server) evictOldestSession() {
	var oldestID string
	var oldest time.Time
	for id, sess := range s.seen {
		if oldestID == "" || sess.last.Before(oldest) {
			oldestID, oldest = id, sess.last
		}
	}
	if oldestID != "" {
		delete(s.seen, oldestID)
	}
}
