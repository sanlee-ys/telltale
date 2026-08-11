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
)

// DefaultAddr is the sink's listen address. 4519 collides with nothing this
// fleet runs (4318 is the OTLP collector's); loopback for the same reason
// grokotel binds loopback — a sink reachable off-box would be an open door
// wearing a log's name.
const DefaultAddr = "127.0.0.1:4519"

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
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("bad listen address %q: %w", addr, err)
	}
	if !isLoopback(host) {
		return fmt.Errorf("refusing to listen on %q: the event sink binds loopback only (127.0.0.1, ::1, localhost)", addr)
	}
	ln, err := net.Listen("tcp", addr)
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

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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

func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
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
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
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
