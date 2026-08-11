// Package eventsink is the fleet event sink (design.md §7.21): a
// loopback-only HTTP server that accepts one hook event per POST, appends it
// to a durable on-disk log, and rebroadcasts it to every connected WebSocket
// client. Any process that can pipe JSON is an event source — the Python
// emitter in tools/emit-event.py is the reference source, and the vendor
// wrappers can call it too.
//
// # Why this is not a gauge, and what boundary it moves
//
// The gauges read vendor files and write numbers-and-keys only. This mode is
// different on both axes, on purpose: it is a foreground server the operator
// starts (like `telltale otel grok`), and the rows it stores carry the hook
// payload VERBATIM — tool names, file paths, whatever the hook was handed.
// That makes it the first content-bearing store under ~/.telltale/, and the
// contract that keeps it honest is scope, not redaction: the server binds
// loopback only, nothing in the gauges reads these files, and the whole
// subsystem is dark until the operator runs the mode and wires an emitter.
// CLAUDE.md's read/write boundary section names this exception.
//
// # The store, and why it is not SQLite
//
// The reference design keeps one `events` table with indexes on the three tag
// axes and the timestamp. This repo takes no dependency for a storage path
// (decisions/001; internal/sqlite is a byte-level READER and has no write
// path), so the same contract is met with stdlib parts: one JSONL file per
// UTC day under the sink directory, the full retention window held in memory,
// and the three distinct-value sets kept beside it. The queries the endpoints
// need — last N by arrival, DISTINCT of three columns — are exactly what that
// in-memory shape answers. An index is a means; the endpoints are the
// contract.
//
// # Retention
//
// The reference design has none; this sink sweeps. A day file whose events
// are all older than the retention window is deleted, and the in-memory tail
// is pruned to match. The sweep runs at startup and then hourly, so a sink
// left running for months holds a bounded window rather than everything ever
// emitted.
package eventsink

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Event is one hook event. The three tag axes (source_app, session_id,
// hook_event_type) and the timestamp are required; the promoted fields are
// optional conveniences lifted out of the payload by the emitter so a reader
// can filter without parsing every payload. The payload itself is stored
// verbatim and never interpreted here.
type Event struct {
	ID            int64           `json:"id"`
	SourceApp     string          `json:"source_app"`
	SessionID     string          `json:"session_id"`
	HookEventType string          `json:"hook_event_type"`
	Payload       json.RawMessage `json:"payload"`
	// TimestampMS is epoch milliseconds. The emitter stamps it; a POST that
	// carries none gets the server's arrival time, so a row can never sort as
	// 1970.
	TimestampMS int64 `json:"timestamp"`

	ToolName       string `json:"tool_name,omitempty"`
	ToolUseID      string `json:"tool_use_id,omitempty"`
	Error          string `json:"error,omitempty"`
	AgentID        string `json:"agent_id,omitempty"`
	AgentType      string `json:"agent_type,omitempty"`
	StopHookActive *bool  `json:"stop_hook_active,omitempty"`
}

// Validate reports why an event cannot be stored. The three tag axes are the
// filter surface; a row missing one would be unreachable through every filter
// and unattributable in the stream, so it is refused rather than stored
// half-addressable.
func (e *Event) Validate() error {
	if e.SourceApp == "" {
		return errors.New("event has no source_app")
	}
	if e.SessionID == "" {
		return errors.New("event has no session_id")
	}
	if e.HookEventType == "" {
		return errors.New("event has no hook_event_type")
	}
	if len(e.Payload) == 0 {
		// An empty payload is legal shape-wise but always a wiring bug: every
		// hook has a payload, and storing `null` would hide the bug as data.
		return errors.New("event has no payload")
	}
	return nil
}

// FilterOptions is the DISTINCT of the three tag axes over the retention
// window — what a client offers as filter choices.
type FilterOptions struct {
	SourceApps     []string `json:"source_apps"`
	SessionIDs     []string `json:"session_ids"`
	HookEventTypes []string `json:"hook_event_types"`
}

// Dir is where the sink stores its day files.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".telltale", "events"), nil
}

// Store is the durable log plus its in-memory working set. One mutex guards
// both: event volume is a human-driven fleet's hook rate, and a lock held for
// a map insert and a file append is not the bottleneck of anything here.
type Store struct {
	dir    string
	retain time.Duration
	now    func() time.Time

	mu     sync.Mutex
	events []Event // ascending by ID == arrival order
	nextID int64
}

// DefaultRetain is the retention window when the flag does not set one.
const DefaultRetain = 30 * 24 * time.Hour

// Open loads every day file still inside the retention window into memory
// and returns the store. A line that does not parse is counted and skipped,
// never fatal — the partial-read rule (CLAUDE.md): a torn last line from a
// kill mid-append must not cost the rest of the log.
func Open(dir string, retain time.Duration, now func() time.Time) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	if retain <= 0 {
		retain = DefaultRetain
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, retain: retain, now: now, nextID: 1}
	cutoff := now().Add(-retain).UnixMilli()

	names, err := dayFiles(dir)
	if err != nil {
		return nil, err
	}
	var skipped int
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var e Event
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				skipped++
				continue
			}
			if e.TimestampMS < cutoff {
				continue
			}
			if e.ID >= s.nextID {
				s.nextID = e.ID + 1
			}
			s.events = append(s.events, e)
		}
	}
	// Files are read in name order (= day order) but IDs are the arrival
	// order across restarts; sort so Recent's tail is truly the newest.
	sort.Slice(s.events, func(i, j int) bool { return s.events[i].ID < s.events[j].ID })
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "telltale events: %d unreadable log lines skipped at load\n", skipped)
	}
	return s, nil
}

// dayFiles lists the store's own files, sorted by name. The name pattern is
// affirmative — anything else in the directory is not touched, so a stray
// file can never be swept by retention.
func dayFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if len(name) == len("2026-08-11.jsonl") && strings.HasSuffix(name, ".jsonl") {
			if _, err := time.Parse("2006-01-02", strings.TrimSuffix(name, ".jsonl")); err == nil {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names, nil
}

// Add assigns the next ID, appends the event to today's file and to memory,
// and returns the stored row. The file write happens before the method
// returns so an acknowledged POST is on disk, not only in a buffer this
// process could die holding.
func (s *Store) Add(e Event) (Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e.TimestampMS == 0 {
		e.TimestampMS = s.now().UnixMilli()
	}
	e.ID = s.nextID

	line, err := json.Marshal(e)
	if err != nil {
		return Event{}, err
	}
	name := s.now().UTC().Format("2006-01-02") + ".jsonl"
	f, err := os.OpenFile(filepath.Join(s.dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Event{}, err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return Event{}, err
	}
	if err := f.Close(); err != nil {
		return Event{}, err
	}

	s.nextID++
	s.events = append(s.events, e)
	return e, nil
}

// Recent returns the newest limit events, newest first.
func (s *Store) Recent(limit int) []Event {
	if limit <= 0 {
		limit = 100
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.events)
	if limit > n {
		limit = n
	}
	out := make([]Event, limit)
	for i := 0; i < limit; i++ {
		out[i] = s.events[n-1-i]
	}
	return out
}

// Options returns the DISTINCT of the three tag axes, each sorted, over what
// is in memory — which is the retention window, by construction.
func (s *Store) Options() FilterOptions {
	s.mu.Lock()
	defer s.mu.Unlock()
	apps := map[string]bool{}
	sessions := map[string]bool{}
	types := map[string]bool{}
	for _, e := range s.events {
		apps[e.SourceApp] = true
		sessions[e.SessionID] = true
		types[e.HookEventType] = true
	}
	return FilterOptions{
		SourceApps:     sortedKeys(apps),
		SessionIDs:     sortedKeys(sessions),
		HookEventTypes: sortedKeys(types),
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Sweep applies retention: memory drops events older than the window, and a
// day file is deleted only when the whole DAY is past the window — a day
// file is append-organized by date, so per-row deletion inside one is never
// needed. Returns how many files were deleted.
func (s *Store) Sweep() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := s.now().Add(-s.retain)
	cutoffMS := cutoff.UnixMilli()

	// Memory: events arrive in time order per source but not globally, so
	// filter rather than slice at an index.
	kept := s.events[:0]
	for _, e := range s.events {
		if e.TimestampMS >= cutoffMS {
			kept = append(kept, e)
		}
	}
	s.events = kept

	names, err := dayFiles(s.dir)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, name := range names {
		day, err := time.Parse("2006-01-02", strings.TrimSuffix(name, ".jsonl"))
		if err != nil {
			continue
		}
		// The file's newest possible event is the end of its day; only when
		// that is past the cutoff can every row in it be expired.
		if day.Add(24 * time.Hour).Before(cutoff) {
			if err := os.Remove(filepath.Join(s.dir, name)); err != nil {
				return deleted, err
			}
			deleted++
		}
	}
	return deleted, nil
}
