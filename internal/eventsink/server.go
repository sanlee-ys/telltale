package eventsink

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/sanlee-ys/telltale/internal/bindaddr"
	"github.com/sanlee-ys/telltale/internal/localonly"
)

// DefaultAddr is the sink's listen address. 4519 collides with nothing this
// fleet runs (4318 is the OTLP collector's); loopback for the same reason
// grokotel binds loopback — a sink reachable off-box would be an open door
// wearing a log's name.
const DefaultAddr = "127.0.0.1:4519"

// defaultPort is DefaultAddr's port on its own, because the collision message
// says one thing when this mode's own default is taken and a different thing
// when a port the operator chose is taken.
// TestDefaultAddrCarriesTheDefaultPort keeps the two in step.
const defaultPort = "4519"

// maxBody caps one POST. A hook payload is kilobytes; the cap is generous
// headroom, and a body past it is refused rather than half-read.
const maxBody = 4 << 20

// Server ties the store to the three HTTP endpoints and the WebSocket
// stream. Subscribers hold a buffered channel each; a subscriber that cannot
// keep up is dropped rather than allowed to stall the broadcast — the sink
// must never be the slowest part of the fleet it observes.
type Server struct {
	store *Store
	logf  func(format string, args ...any)

	mu   sync.Mutex
	subs map[chan []byte]bool
}

// NewServer wraps a store. logf receives one line per stored event and per
// dropped subscriber, for the human watching the mode run; nil silences it.
func NewServer(store *Store, logf func(string, ...any)) *Server {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Server{store: store, logf: logf, subs: map[chan []byte]bool{}}
}

// Run listens on addr and serves until the process is killed. The bind is
// refused unless addr is loopback. sweepEvery > 0 starts the retention
// sweeper: once at startup, then on that interval.
func (s *Server) Run(addr string, sweepEvery time.Duration) error {
	ln, err := s.listen(addr)
	if err != nil {
		return err
	}
	if sweepEvery > 0 {
		go s.sweepLoop(sweepEvery)
	}
	s.logf("telltale events: listening on %s, storing in %s", ln.Addr(), s.store.dir)
	server := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// No ReadTimeout / IdleTimeout: /stream connections are long-lived by
		// design, and http.Server timeouts would cut them. The POST path is
		// bounded by maxBody instead.
	}
	return server.Serve(ln)
}

func (s *Server) sweepLoop(every time.Duration) {
	for {
		deleted, err := s.store.Sweep()
		if err != nil {
			s.logf("telltale events: retention sweep: %v", err)
		} else if deleted > 0 {
			s.logf("telltale events: retention sweep deleted %d day files", deleted)
		}
		time.Sleep(every)
	}
}

// listen holds the two ways this bind can fail, and says something the
// operator can act on for each. The loopback refusal is a rule of this mode:
// the sink stores hook payloads VERBATIM (§7.21), so a sink reachable off-box
// would publish every tool call and file path this fleet makes. The busy-port
// refusal is the measured common case (below), and it used to arrive as the
// raw net.Listen error.
//
// # What a taken port looked like before, measured
//
// 2026-08-16, telltale built from main at 1995b34, Windows 11, a throwaway
// listener holding 127.0.0.1:4519. `telltale events` printed one line and
// exited 1:
//
//	telltale events: listen tcp 127.0.0.1:4519: bind: Only one usage of each socket address (protocol/network address/port) is normally permitted.
//
// That is the same shape `telltale otel grok` had before §7.16a's 2026-08-16
// amendment named it, and it fails the same way: the exit code and the
// loudness were already right, and the line carried nothing the operator could
// act on. What it did not say is that the likely holder is a sink already
// running, that --addr moves this side, and that moving this side ALONE stores
// nothing because the emitters keep posting to 4519.
func (s *Server) listen(addr string) (net.Listener, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("bad listen address %q: %w", addr, err)
	}
	if !bindaddr.IsLoopback(host) {
		return nil, fmt.Errorf("refusing to listen on %q: the event sink binds loopback only (127.0.0.1, ::1, localhost)", addr)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if bindaddr.InUse(err) {
			return nil, portTaken(addr, port, bindaddr.Next(host, port, defaultPort), err)
		}
		return nil, err
	}
	return ln, nil
}

// portTaken renders the collision. It states the cause it can defend and no
// more, which for this mode is a different cause than the collector's: 4318 is
// OTLP/HTTP's registered port and every local receiver wants it, while 4519 is
// this mode's own and nothing else in the fleet claims it — so the likely
// holder here is a sink the operator already started, not a rival product. On
// a port the operator picked telltale knows nothing about who holds it, so it
// does not guess.
//
// The redirect half is the part a shorter message would drop and the part that
// decides whether moving the port works at all. A moved sink with unmoved
// emitters is a server that listens forever and stores nothing, and §7.21
// measured why that is worse here than a crash: an emitter whose sink is
// unreachable prints one stderr line and exits 0 BY DESIGN, so the whole fleet
// goes quiet with no failure anywhere to notice.
func portTaken(addr, port, next string, err error) error {
	why := fmt.Sprintf("Another process on this machine holds %s. telltale cannot say which one.", addr)
	if port == defaultPort {
		why = "A telltale events sink you already started probably holds it. Port " + defaultPort + " is\n" +
			"this mode's own default and nothing else in this fleet claims it (§7.21), so the\n" +
			"likely holder is a second copy of this mode. Check for one before you move the\n" +
			"port: if a sink is already listening, the emitters already reach it and a second\n" +
			"sink buys nothing."
	}
	return fmt.Errorf(`%s is already in use, so the sink did not start.

%s

Move this sink to a free loopback port:
    telltale events --addr %s

Then move every emitter to the same address. A sink moved alone stores nothing:
the hooks go on posting to %s and never reach this one.
    python3 <path>/tools/emit-event.py --source-app <name> --server-url http://%s/events

--server-url is a second per-repo edit beside --source-app, in the same
.claude/settings.json hook command §7.21 describes. Every repo wired to the
sink needs it. An unmoved emitter is silent, not loud: it prints one stderr
line and exits 0 by design, so a half-moved sink looks exactly like a fleet
where no hook ever fired.

bind error: %w`, addr, why, next, addr, next, err)
}

// Handler routes the sink's four paths. Everything else is a 404: this is
// not a general server, and answering paths it does not understand would
// disguise a miswired emitter as a working one.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/events", s.handlePost)
	mux.HandleFunc("/events/recent", s.handleRecent)
	mux.HandleFunc("/events/filter-options", s.handleOptions)
	mux.HandleFunc("/stream", s.handleStream)
	return mux
}

// contentType is the media type the reference emitter sends
// (tools/emit-event.py, measured: `Content-Type: application/json`, no Origin,
// User-Agent Python-urllib). Requiring it costs a legitimate emitter nothing
// and costs a web page everything — see internal/localonly.
const contentType = "application/json"

// fromALocalProgram is the gate every endpoint runs first, and it matters more
// here than at the collector, because this store holds hook payloads VERBATIM
// (§7.21) rather than four token counts. It is applied to the READ paths and
// to the stream, not only to the POST: a browser that cannot plant a row can
// still ask for the rows already there, and §7.24 measured a page doing exactly
// that over /stream. On refusal the response is already written.
func (s *Server) fromALocalProgram(w http.ResponseWriter, r *http.Request) bool {
	if err := localonly.RefuseBrowser(r); err != nil {
		s.logf("telltale events: %v", err)
		localonly.Refuse(w, err)
		return false
	}
	return true
}

func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.fromALocalProgram(w, r) {
		return
	}
	if err := localonly.RequireContentType(r, contentType); err != nil {
		s.logf("telltale events: %v", err)
		localonly.Refuse(w, err)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var e Event
	if err := json.Unmarshal(body, &e); err != nil {
		// 400, not 500: a malformed payload is the sender's fact, and the
		// emitter does not retry — it logs to stderr and exits 0.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := e.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	stored, err := s.store.Add(e)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.logf("telltale events: stored #%d %s/%s %s", stored.ID, stored.SourceApp, stored.SessionID, stored.HookEventType)
	s.broadcast(stored)
	writeJSON(w, stored)
}

func (s *Server) handleRecent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.fromALocalProgram(w, r) {
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			http.Error(w, "limit must be a positive integer", http.StatusBadRequest)
			return
		}
		limit = n
	}
	writeJSON(w, s.store.Recent(limit))
}

func (s *Server) handleOptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.fromALocalProgram(w, r) {
		return
	}
	writeJSON(w, s.store.Options())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

// streamMsg is the one envelope the stream speaks: `initial` once on
// connect, then `event` per insert.
type streamMsg struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// handleStream upgrades to WebSocket, sends the initial snapshot, then
// relays broadcasts until the client goes away.
//
// The sender gate runs BEFORE the upgrade, and this is the endpoint that
// forced internal/localonly to exist. A WebSocket handshake is exempt from
// CORS, so no content-type rule and no preflight was ever going to reach it: a
// page could simply open ws://127.0.0.1:4519/stream and be handed the
// `initial` snapshot — the last hundred hook payloads, verbatim. §7.24
// measured a headless Chrome doing precisely that against this handler, which
// is why the fix is a check on the request rather than anything about
// transports. The handshake carries Origin (measured, and it carries no
// Sec-Fetch-* header, which is why Origin is the header the check reads).
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if !s.fromALocalProgram(w, r) {
		return
	}
	conn, err := upgrade(w, r)
	if err != nil {
		// upgrade has already written the HTTP error where it could.
		return
	}
	defer conn.Close()

	initial, err := json.Marshal(streamMsg{Type: "initial", Data: s.store.Recent(100)})
	if err != nil {
		return
	}
	if err := writeText(conn, initial); err != nil {
		return
	}

	// The subscription channel is buffered so one slow write does not stall
	// the POST path; a full buffer means this client cannot keep up and is
	// dropped, which it can observe as a closed socket and reconnect from.
	ch := make(chan []byte, 64)
	s.mu.Lock()
	s.subs[ch] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}()

	// The read side runs beside the writes: it answers pings, consumes
	// whatever else the client sends, and closes `done` when the client
	// closes or the connection breaks.
	done := make(chan struct{})
	go func() {
		defer close(done)
		readUntilClose(conn)
	}()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				// broadcast closed the channel: this client fell behind.
				return
			}
			if err := writeText(conn, msg); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}

// broadcast fans one stored event out to every subscriber. Non-blocking by
// construction: a full channel drops the SUBSCRIBER (closing it wakes its
// stream goroutine into a dead send and exit), never delays the store.
func (s *Server) broadcast(e Event) {
	msg, err := json.Marshal(streamMsg{Type: "event", Data: e})
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- msg:
		default:
			delete(s.subs, ch)
			close(ch)
			s.logf("telltale events: dropped a stream client that stopped reading")
		}
	}
}
