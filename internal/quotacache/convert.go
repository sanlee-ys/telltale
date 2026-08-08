package quotacache

import (
	"sort"
	"time"

	"github.com/sanlee-ys/telltale/internal/antigravity"
	"github.com/sanlee-ys/telltale/internal/claude"
)

// FromClaude converts a Claude statusline payload's rate limits to cache
// windows. Ids and labels match what the statusline itself renders ("5h",
// "7d") so the two surfaces cannot call the same window different things.
// A window without used_percentage is relayed only for its reset time; an
// absent rate_limits (API-key login) converts to nothing, and the caller
// writes nothing — the previous reading, if any, ages out on its own.
func FromClaude(rl *claude.RateLimits, now time.Time) []Window {
	var out []Window
	if w := rl.GetFiveHour(); w != nil {
		out = appendClaudeWindow(out, "five_hour", "5h", w)
	}
	if w := rl.GetSevenDay(); w != nil {
		out = appendClaudeWindow(out, "seven_day", "7d", w)
	}
	return out
}

func appendClaudeWindow(out []Window, id, label string, w *claude.Window) []Window {
	cw := Window{ID: id, Label: label, UsedPercent: w.UsedPercentage}
	if w.ResetsAt != nil {
		t := time.Unix(*w.ResetsAt, 0)
		cw.ResetsAt = &t
	}
	return append(out, cw)
}

// FromAntigravity converts agy's named quota buckets. Bucket ids are relayed
// VERBATIM as both id and label — they are vendor vocabulary ("gemini-weekly",
// "3p-weekly" observed), and translating them through an assumed mapping is
// how a gauge starts narrating (the statusline's own rule). used% is the same
// (1-remaining)*100 unit conversion the statusline performs; the reset
// instant is pinned against now at conversion time, because the cache stores
// when the window resets, not how long that was from a render that already
// happened.
func FromAntigravity(quota map[string]*antigravity.QuotaBucket, now time.Time) []Window {
	if len(quota) == 0 {
		return nil
	}
	ids := make([]string, 0, len(quota))
	for id := range quota {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []Window
	for _, id := range ids {
		b := quota[id]
		if b == nil || b.RemainingFraction == nil {
			continue
		}
		used := (1 - *b.RemainingFraction) * 100
		w := Window{ID: id, Label: id, UsedPercent: &used}
		if d, ok := resetIn(b, now); ok && d > 0 {
			t := now.Add(d)
			w.ResetsAt = &t
		}
		out = append(out, w)
	}
	return out
}

// resetIn mirrors the statusline's agyResetIn: prefer the vendor's relative
// reset_in_seconds, fall back to the absolute reset_time.
func resetIn(b *antigravity.QuotaBucket, now time.Time) (time.Duration, bool) {
	if b.ResetInSeconds != nil {
		return time.Duration(*b.ResetInSeconds) * time.Second, true
	}
	if b.ResetTime != "" {
		if t, err := time.Parse(time.RFC3339, b.ResetTime); err == nil {
			return t.Sub(now), true
		}
	}
	return 0, false
}
