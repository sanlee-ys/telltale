// Package quotacache is the relay between the statusline and the HUD: the one
// sanctioned write on a gauge path (CLAUDE.md "the read/write boundary",
// design.md §7.15).
//
// Why it exists: Claude's and Antigravity's quota arrive ONLY on their
// statusline stdin payloads — both vendors were grepped live and write nothing
// quota-shaped to disk (claudecode.go package doc; design.md §3.8). The HUD
// reads disk. Without a relay the header can speak only for vendors whose
// stores carry quota (today: Codex), which is how a correct number ends up
// impersonating the fleet. The statusline already holds the missing readings
// every time it renders; this package lets it put them where the HUD looks.
//
// What the write is allowed to be, all four load-bearing:
//
//   - numbers only, never content. An Entry is vendor id, timestamp, and
//     usage windows — the same keys-not-content standard as council's
//     room.json. A test asserts the serialized form carries no field that
//     could hold session text.
//   - one file per vendor under ~/.telltale/quota/, written atomically
//     (temp + rename), so the HUD never reads a torn entry.
//   - best-effort. A gauge must never fail its render over its cache; every
//     caller ignores Write's error after the line is already on stdout.
//   - self-expiring on the read side. A relayed reading is a snapshot of
//     account state at WrittenAt; the reader drops windows whose reset has
//     passed (the window it describes no longer exists) and whole entries
//     past maxAge or with a clock-skewed future timestamp. Staleness in
//     between is the renderer's to DISPLAY, not this package's to hide —
//     the age travels with the reading (§7.12's basis rule).
package quotacache

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
	// maxAge is where a relayed reading stops rendering at all. A day-old
	// percentage of a five-hour or weekly window is archaeology, not state —
	// and every window we relay today resets within a week, so the reset-passed
	// rule usually retires an entry long before this backstop does.
	maxAge = 24 * time.Hour

	// futureSkew mirrors the adapters' future-skew guard: a timestamp from
	// slightly ahead (clock jitter) is tolerated, one from the future proper
	// means a clock we cannot reason about, and the entry is dropped.
	futureSkew = 5 * time.Minute
)

// Window is one usage window as serialized in the cache. It is a separate
// type from model.QuotaWindow so the on-disk format is explicit about its
// field names and cannot drift when the model grows fields the cache must
// not carry.
type Window struct {
	ID          string     `json:"id"`
	Label       string     `json:"label"`
	UsedPercent *float64   `json:"used_percent,omitempty"`
	ResetsAt    *time.Time `json:"resets_at,omitempty"`
}

// Entry is one vendor's cache file.
type Entry struct {
	Vendor    string    `json:"vendor"`
	WrittenAt time.Time `json:"written_at"`
	Windows   []Window  `json:"windows"`
}

// Account is a read-side result: one vendor's surviving windows plus the
// timestamp that lets the renderer state the reading's age.
type Account struct {
	Vendor    model.VendorID
	Windows   []model.QuotaWindow
	WrittenAt time.Time
}

// Dir is the cache directory, ~/.telltale/quota — beside council's room.json,
// the only other file telltale writes.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".telltale", "quota"), nil
}

// Write atomically replaces vendor's cache file. Windows without a reading
// AND without a reset time are dropped at write: an entry of empty windows
// asserts "I measured nothing", which is what NOT writing says for free.
func Write(dir, vendor string, windows []Window, now time.Time) error {
	kept := windows[:0:0]
	for _, w := range windows {
		if w.UsedPercent != nil || w.ResetsAt != nil {
			kept = append(kept, w)
		}
	}
	if len(kept) == 0 || vendor == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(Entry{Vendor: vendor, WrittenAt: now, Windows: kept})
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
	if err := os.Rename(tmpName, filepath.Join(dir, vendor+".json")); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// ReadAll returns every vendor's surviving relayed reading, sorted by vendor
// id for a deterministic frame. A missing directory is the common case (no
// statusline has ever fired) and returns nothing quietly; so does every
// malformed or expired entry — the honest display for "no reading" is
// absence, never an error banner (§7.7 shows LESS on failure).
func ReadAll(dir string, now time.Time) []Account {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Account
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			continue
		}
		var e Entry
		if err := json.Unmarshal(raw, &e); err != nil || e.Vendor == "" {
			continue
		}
		if e.WrittenAt.IsZero() || e.WrittenAt.After(now.Add(futureSkew)) || now.Sub(e.WrittenAt) > maxAge {
			continue
		}
		var windows []model.QuotaWindow
		for _, w := range e.Windows {
			// A window whose reset has passed describes a window that no
			// longer exists; its percentage is not stale, it is false.
			if w.ResetsAt != nil && !w.ResetsAt.After(now) {
				continue
			}
			mw := model.QuotaWindow{ID: w.ID, Label: w.Label, ResetsAt: w.ResetsAt}
			if w.UsedPercent != nil && *w.UsedPercent >= 0 && *w.UsedPercent <= 100 {
				p := model.Percent(*w.UsedPercent)
				mw.UsedPercent = &p
			}
			if mw.UsedPercent == nil && mw.ResetsAt == nil {
				continue
			}
			windows = append(windows, mw)
		}
		if len(windows) == 0 {
			continue
		}
		out = append(out, Account{Vendor: model.VendorID(e.Vendor), Windows: windows, WrittenAt: e.WrittenAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Vendor < out[j].Vendor })
	return out
}
