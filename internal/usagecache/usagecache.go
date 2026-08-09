// Package usagecache is the relay between a vendor's per-turn hook and the
// HUD: the THIRD sanctioned write on a gauge path (CLAUDE.md "the read/write
// boundary", design.md §7.16), and a deliberate mirror of internal/quotacache
// rather than a second design.
//
// # Why a sibling package and not a second store inside quotacache
//
// The two caches share their mechanism exactly — one file per vendor under
// ~/.telltale/, atomic temp+rename, best-effort write, self-expiring read,
// numbers-and-keys pinned by a test — and share nothing else. quotacache's
// Entry is windows: ids, labels, percentages, reset instants, and a read rule
// ("a window whose reset has passed describes a window that no longer exists")
// that has no meaning for a counter. Folding a token total into that Entry
// would put one keys-not-content test and one package doc in charge of two
// unrelated formats, and would leave every reader of quotacache.Window
// wondering which of its fields a token count is supposed to use. So the
// PATTERNS are copied on purpose, function for function, and the schema is
// its own.
//
// # What the write is allowed to be
//
//   - numbers only, never content. An Entry is a vendor id, two timestamps, a
//     turn count and four token totals. There is no field a reply, a prompt,
//     a path or an email address could occupy — see internal/cursorhook, where
//     the payload's `text`, `transcript_path` and `user_email` are discarded
//     before this package is ever called.
//   - one file per vendor under ~/.telltale/usage/, written atomically
//     (temp + rename in the same directory) so the HUD never reads a torn
//     entry.
//   - best-effort. The hook runs inside the vendor's turn; a cache that can
//     fail a turn is worse than no cache, so Add's error is reported to the
//     caller and the caller exits clean regardless.
//   - self-expiring on the read side, and on the ACCUMULATE side. A total is
//     honest only while its window is, so an entry past maxAge or stamped from
//     the future is not summed onto — it is replaced, and a fresh window opens.
//     This is the load-bearing half: without it a sum could silently span a
//     week-long gap and still call itself a total.
//
// # What it is NOT
//
// It is not quota. There is no denominator anywhere in it — Cursor exposes no
// account limit without a network call (§3.9, re-verified 2026-08-08) — so
// nothing here may render as a percentage, a gauge, or anything else with an
// implied ceiling. It counts what this machine spent, and that is the whole
// claim.
package usagecache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

const (
	// maxAge is where a total stops being a total. A day-old sum is not a
	// state, and — unlike a quota window, which retires itself when its reset
	// passes — a counter has no natural expiry of its own, so this backstop is
	// the only one it gets. It is the same 24h quotacache uses, deliberately:
	// two caches under ~/.telltale/ ageing out on different clocks would be a
	// fact a reader has to look up.
	maxAge = 24 * time.Hour

	// futureSkew mirrors quotacache and the adapters: a timestamp slightly
	// ahead is clock jitter and tolerated; one from the future proper means a
	// clock we cannot reason about, and the entry is dropped rather than
	// summed onto.
	futureSkew = 5 * time.Minute
)

// Delta is one turn's measured consumption — the unit Add accumulates.
//
// Plain int64s, not pointers: a Delta is built only from a turn that reported
// all four counts (cursorhook.Turn.Complete), because a partial turn summed
// into a running total understates it invisibly. Absence is handled before
// this type, which is why this type does not have to model it.
type Delta struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
}

// Entry is one vendor's cache file: a running total, and everything a reader
// needs to know what the total is a total OF.
//
// Since and Turns are not decoration. A sum with no window is a number
// pretending to be a state — "48.0k tokens" answers nothing unless it also
// says over how many turns and since when. Both travel in the file and both
// travel to the screen; §7.16 forbids rendering the sum without them.
type Entry struct {
	Vendor    string    `json:"vendor"`
	Since     time.Time `json:"since"`
	WrittenAt time.Time `json:"written_at"`
	Turns     int       `json:"turns"`

	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
}

// Total is a read-side result: an Entry whose vendor id has been narrowed to
// the model's vocabulary, returned only when it survived the expiry rules.
type Total struct {
	Vendor model.VendorID
	Entry
}

// Span is how long the accumulation window has been open. It is what the
// render says "over" — the distance from the first counted turn to the last,
// never to now, because the window describes turns and not waiting.
func (t Total) Span() time.Duration {
	d := t.WrittenAt.Sub(t.Since)
	if d < 0 {
		return 0
	}
	return d
}

// Dir is the cache directory, ~/.telltale/usage — beside quota/ and council/,
// the only places telltale writes.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".telltale", "usage"), nil
}

// Add folds one turn into the vendor's running total and writes it.
//
// The accumulation rule, stated once here because the render quotes it:
//
//   - if a LIVE entry exists (readable, this vendor, not expired, not
//     future-stamped), the delta is added to it and Since is kept. The window
//     continues.
//   - otherwise a new window opens with Since = now. That covers the first
//     turn ever, the first turn after a day of silence, and the first turn
//     after a corrupted or clock-skewed file — all of which are the same fact:
//     there is no total here that this turn may honestly join.
//
// WrittenAt is stamped every time, so the reader can age the whole entry.
//
// Concurrency: this is a read-modify-write, and two hook processes finishing
// in the same instant can lose one turn. That is accepted rather than locked
// because the failure is bounded (one turn's counts, and the Turns count that
// names how many turns are in the sum drops with it, so the total stays
// self-consistent) and because the alternative — a lock file on a path the
// vendor's own turn is waiting on — can hang a turn, which is a strictly worse
// thing for a gauge to do than to undercount. §7.16 records it as a known
// limitation rather than hiding it.
func Add(dir, vendor string, d Delta, now time.Time) error {
	if vendor == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, vendor+".json")

	e := Entry{Vendor: vendor, Since: now}
	if prev, ok := readEntry(path, now); ok && prev.Vendor == vendor {
		e = prev
	}
	e.WrittenAt = now
	e.Turns++
	e.InputTokens += d.InputTokens
	e.OutputTokens += d.OutputTokens
	e.CacheReadTokens += d.CacheReadTokens
	e.CacheWriteTokens += d.CacheWriteTokens

	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	// Temp file in the SAME directory: rename is only atomic within a volume,
	// and the whole point is that the HUD never sees half an entry.
	tmp, err := os.CreateTemp(dir, vendor+"-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// ReadAll returns every vendor's surviving total, sorted by vendor id for a
// deterministic frame. A missing directory is the common case (no hook has
// ever fired) and returns nothing quietly; so does every malformed or expired
// entry — the honest display for "no reading" is absence, never an error
// banner (§7.7 shows LESS on failure).
func ReadAll(dir string, now time.Time) []Total {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Total
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		e, ok := readEntry(filepath.Join(dir, f.Name()), now)
		if !ok {
			continue
		}
		out = append(out, Total{Vendor: model.VendorID(e.Vendor), Entry: e})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Vendor < out[j].Vendor })
	return out
}

// readEntry is the one place the liveness rules live, shared by the reader and
// by Add. Sharing it is the point: if accumulation kept a window the renderer
// would have dropped, telltale would be summing into a total nobody can see.
func readEntry(path string, now time.Time) (Entry, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, false
	}
	var e Entry
	if err := json.Unmarshal(raw, &e); err != nil || e.Vendor == "" {
		return Entry{}, false
	}
	if e.WrittenAt.IsZero() || e.Since.IsZero() {
		return Entry{}, false
	}
	if e.WrittenAt.After(now.Add(futureSkew)) || now.Sub(e.WrittenAt) > maxAge {
		return Entry{}, false
	}
	// A window that starts after it was last written to is incoherent — the
	// file has been edited or a clock moved under it — and an incoherent
	// window cannot say what its total is a total of.
	if e.Since.After(e.WrittenAt.Add(futureSkew)) {
		return Entry{}, false
	}
	// Turns is what makes the sum readable; a total claiming zero turns, or a
	// negative count, is a broken reading rather than a small one.
	if e.Turns <= 0 {
		return Entry{}, false
	}
	if e.InputTokens < 0 || e.OutputTokens < 0 || e.CacheReadTokens < 0 || e.CacheWriteTokens < 0 {
		return Entry{}, false
	}
	return e, true
}
