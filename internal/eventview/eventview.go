// Package eventview is the event sink's first reader (design.md §7.21,
// amended 2026-08-17). `telltale events view` lists what the sink stored,
// filters it by the three tag axes or by day, and follows the store live.
//
// # Its trust position: it reads the FILES, and opens no socket
//
// The sink serves two read endpoints of its own, `GET /events/recent` and
// `GET /events/filter-options`, plus the `/stream` WebSocket. This package
// uses none of them. It opens the day files under ~/.telltale/events/ and
// parses them, which is a deliberate choice on three grounds.
//
// Trust is the first, and §7.24 already settled it. That section measured a
// plain file write planting the same row a POST plants, with MORE control over
// it, and states the boundary once: a program running as a principal the
// store's ACL admits is trusted by these listeners exactly as far as it is
// trusted by the filesystem. So the HTTP path would grant this reader nothing
// the file path does not already grant it. The endpoints are not closed to it
// either. `internal/localonly` refuses a request carrying `Origin`, and a
// local program sends none, so a viewer that connected would be served. The
// point is that it would gain nothing by connecting.
//
// Availability is the second, and it is the one that decides the design. The
// sink is a foreground mode the operator starts. Its endpoints answer only
// while that process is alive, and only over the window that process loaded
// into memory at startup. The day files outlive it. A reader that needed a
// running sink would be dark in exactly the case the operator reaches for it:
// after the fact, wanting to know what happened this morning.
//
// The third is that reading files makes the boundary checkable rather than
// promised. This package's own imports carry no net package, which
// TestTheViewerOpensNoSocket asserts against the toolchain's own answer.
//
// What it costs, stated rather than glossed: follow mode POLLS the day files
// on an interval instead of receiving a push, so the interval is the honest
// latency bound and the flag names it. The trade buys something back. The
// sink's broadcast drops a subscriber whose buffer fills, by design, while a
// file tail cannot miss an event that way because the file is the durable
// record and the tail only ever moves forward through it.
//
// # What this mode deliberately does not do
//
// It writes nothing, anywhere. TestTheViewerWritesNothing hashes the store
// before and after a read and a follow poll.
//
// It is not a gauge and no gauge gained a read of these files.
// TestNoGaugeReadsTheEventStore asserts that over the import graphs of
// internal/hud, internal/statusline and internal/snapshot. The event store is
// the one store under ~/.telltale/ that holds content rather than keys and
// numbers, and CLAUDE.md's read/write boundary contains it by scope: its own
// foreground mode, a loopback bind, a sender that is not a web page, and no
// gauge reading it. A viewer that is its own mode keeps all four of those.
package eventview

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sanlee-ys/telltale/internal/eventsink"
	"github.com/sanlee-ys/telltale/internal/jsonl"
)

// DefaultLimit is how many events a listing shows when --limit does not say.
// It is a screenful and a bit rather than the sink's own 100, because this
// output is read by eyes where /events/recent is parsed by a program.
const DefaultLimit = 50

// DefaultInterval is how often follow mode re-reads the day files. It matches
// the poll interval the HUD uses over vendor transcripts (design.md §3.1): a
// second is under the threshold where a human calls a log "live", and a tail
// read of the bytes appended since the last poll costs almost nothing.
const DefaultInterval = time.Second

// Filter selects which stored events a listing shows.
//
// The three tag axes are the sink's own filter surface, the same three
// `GET /events/filter-options` offers, and they are the fields Validate
// requires of every stored row. Day is the fourth axis and it is different in
// kind: it selects a FILE.
type Filter struct {
	// Sources, Sessions and Types each match a row when the row's value is any
	// one of the listed values. An empty list matches every row.
	Sources  []string
	Sessions []string
	Types    []string

	// Day is a YYYY-MM-DD date naming one day file, or empty for every
	// retained day.
	Day string

	// Limit is how many of the newest matching events Read returns. Zero takes
	// DefaultLimit. It does not apply to Poll: follow mode reports what
	// arrived, and dropping some of that would be the viewer inventing a
	// quiet fleet.
	Limit int
}

// Validate reports why a filter cannot be applied. Only Day can be malformed:
// the tag axes are free strings and an unknown value is an honest empty
// listing, not an error.
func (f Filter) Validate() error {
	if f.Day == "" {
		return nil
	}
	if _, err := time.Parse(eventsink.DayLayout, f.Day); err != nil {
		return fmt.Errorf("--day wants a date like 2026-08-16, not %q. The store keeps "+
			"one file per UTC day and that file's name is what --day is matched against", f.Day)
	}
	return nil
}

// Matches reports whether one event passes the three tag axes.
//
// The comparison ignores letter case. These values are names a reader types
// back after reading them off a listing, and a case-sensitive miss renders
// identically to an empty store: no rows, no explanation. An exact-case rule
// would make `--type pretooluse` a silent lie about what the fleet did.
func (f Filter) Matches(e eventsink.Event) bool {
	return matchesAny(f.Sources, e.SourceApp) &&
		matchesAny(f.Sessions, e.SessionID) &&
		matchesAny(f.Types, e.HookEventType)
}

func matchesAny(want []string, got string) bool {
	if len(want) == 0 {
		return true
	}
	for _, w := range want {
		if strings.EqualFold(w, got) {
			return true
		}
	}
	return false
}

// wantsDay reports whether a day file is in scope. Day matches the file's
// name, which is the day the SINK recorded the row, not the day the emitter
// stamped on it. The two differ when an emitter's clock is off, or when a
// hook fires either side of UTC midnight. The file's name is the fact this
// reader can check; the stamp is the sender's claim, and it is rendered in
// its own column where the reader can compare them.
func (f Filter) wantsDay(fileName string) bool {
	if f.Day == "" {
		return true
	}
	return strings.TrimSuffix(fileName, ".jsonl") == f.Day
}

// Diagnostics is what the read itself observed, as distinct from what it
// found. It exists so a degraded read can be reported rather than rendered as
// a quiet fleet (CLAUDE.md's partial-read rule).
type Diagnostics struct {
	// StoreMissing is true when the store directory does not exist. That is
	// not an error: the sink creates the directory when it first runs, so an
	// absent directory means it has never run here. It is reported as its own
	// state because "the sink never ran" and "the sink ran and stored nothing"
	// are different answers and must not render as the same empty screen.
	StoreMissing bool

	// Files is how many day files the read covered, after --day narrowed them.
	Files int

	// Records is how many complete records parsed, before the filter.
	Records int

	// Skipped is how many lines did not parse as an event. A torn last line
	// from a sink killed mid-append is the expected cause, and it costs that
	// line only. The count is surfaced rather than swallowed, because a store
	// that is 40% unreadable and a store that is empty look the same on screen
	// otherwise.
	Skipped int
}

// Listing is one read of the store.
type Listing struct {
	// Events are the matching rows. Read returns them newest first and trimmed
	// to the limit; Poll returns them oldest first and trims nothing.
	Events []eventsink.Event

	// Matched is how many rows matched the filter before the limit trimmed
	// them, so a listing can say "50 of 312" rather than implying 50 is all
	// there was.
	Matched int

	// Options is the DISTINCT of the three tag axes over every record read,
	// before the tag filters were applied. It is what makes an empty listing
	// actionable: it names the values that ARE in the store. When --day
	// narrowed the files, this describes that day.
	Options eventsink.FilterOptions

	Diag Diagnostics
}

// Read lists the newest matching events in the store at dir.
//
// It is one Poll of a fresh Tailer, reversed and trimmed. Sharing that path
// with follow mode is deliberate: two readers of one file format drift, and
// the drift shows up as a row the list renders and the tail does not.
func Read(dir string, f Filter) (Listing, error) {
	if err := f.Validate(); err != nil {
		return Listing{}, err
	}
	l, err := NewTailer(dir, f).Poll()
	if err != nil {
		return Listing{}, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	// Newest first, which is the sink's own order for /events/recent and the
	// order a log reader expects when the question is "what just happened".
	events := make([]eventsink.Event, 0, min(limit, len(l.Events)))
	for i := len(l.Events) - 1; i >= 0 && len(events) < limit; i-- {
		events = append(events, l.Events[i])
	}
	l.Events = events
	return l, nil
}

// Tailer reads the store's day files and remembers how far into each one it
// has already read.
//
// It holds byte offsets rather than event IDs, because the offset is what
// makes the next poll cheap: a tail read of the bytes appended since the last
// one, not a re-parse of a 30-day window every second.
type Tailer struct {
	dir     string
	filter  Filter
	offsets map[string]int64
}

// NewTailer returns a tailer that has read nothing. Its first Poll therefore
// returns every retained event that matches, which is what makes follow mode
// need no separate priming read: one code path fills the initial listing and
// then continues from exactly where it stopped, so no event can fall into a
// gap between a list and a tail, and none can be printed twice.
func NewTailer(dir string, f Filter) *Tailer {
	return &Tailer{dir: dir, filter: f, offsets: map[string]int64{}}
}

// Poll returns the complete records appended since the last Poll, oldest
// first, filtered.
//
// A record is complete when its terminating newline is on disk. internal/jsonl
// holds that rule and this reader depends on it: the sink appends a row while
// this process may be reading the same file, so a poll that landed mid-append
// would otherwise parse half a row, fail, and count a torn line that was never
// torn. The partial bytes stay unread until the poll that finds their newline.
func (t *Tailer) Poll() (Listing, error) {
	var l Listing
	names, err := eventsink.DayFiles(t.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			l.Diag.StoreMissing = true
			return l, nil
		}
		return l, err
	}

	apps, sessions, types := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, name := range names {
		if !t.filter.wantsDay(name) {
			continue
		}
		l.Diag.Files++
		recs, err := t.readSince(name)
		if err != nil {
			return Listing{}, err
		}
		for _, rec := range recs {
			var e eventsink.Event
			if err := json.Unmarshal(rec, &e); err != nil {
				l.Diag.Skipped++
				continue
			}
			l.Diag.Records++
			addKey(apps, e.SourceApp)
			addKey(sessions, e.SessionID)
			addKey(types, e.HookEventType)
			if t.filter.Matches(e) {
				l.Events = append(l.Events, e)
			}
		}
	}

	// By ID, which is the sink's arrival order, and not by timestamp, which is
	// the emitter's claim about when the hook fired. The two disagree whenever
	// a sender's clock is off, and arrival order is the one this reader can
	// stand behind. Stable, so two rows sharing an ID (possible only if the
	// store directory was emptied and the sequence restarted) keep file order
	// rather than swapping between polls.
	sort.SliceStable(l.Events, func(i, j int) bool { return l.Events[i].ID < l.Events[j].ID })
	l.Matched = len(l.Events)
	l.Options = eventsink.FilterOptions{
		SourceApps:     sortedKeys(apps),
		SessionIDs:     sortedKeys(sessions),
		HookEventTypes: sortedKeys(types),
	}
	return l, nil
}

// readSince reads one day file from this tailer's offset to its current end
// and returns the complete records in between.
func (t *Tailer) readSince(name string) ([][]byte, error) {
	f, err := os.Open(filepath.Join(t.dir, name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// The retention sweep deletes a whole day file, and it can do so
			// between the listing above and this open. A file that is gone
			// holds no records; it is not a failed read.
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size, off := fi.Size(), t.offsets[name]
	if size < off {
		// A day file only ever grows or is deleted whole, so a file shorter
		// than where this reader stopped is a file that was replaced. Re-read
		// it from the start: a repeated row is visible to the reader and a
		// dropped one is not.
		off = 0
	}
	if size == off {
		return nil, nil
	}

	buf := make([]byte, size-off)
	n, err := f.ReadAt(buf, off)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	recs, partial := jsonl.Split(buf[:n])
	t.offsets[name] = off + int64(n-len(partial))
	return recs, nil
}

// addKey collects a tag value for the DISTINCT sets, skipping the empty
// string. A row whose tag axis is missing has no value to offer as a filter
// choice, and listing "" as one would be offering a filter that matches
// something the reader cannot type.
func addKey(m map[string]bool, v string) {
	if v != "" {
		m[v] = true
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
