package hud

import (
	"math"
	"strings"

	"github.com/sanlee-ys/telltale/internal/model"
)

// gauge renders a horizontal bar for a percentage on the 0–100 scale.
//
// Three rules, each of which exists to stop the bar from lying, and all three
// are pinned by TestGaugeScale:
//
//  1. The last cell is reserved below 100%. Fill is computed over cells-1, so
//     99.9% always leaves one visible track cell and only an exact 100% fills
//     the bar. A 92.6% bar that renders solid is a gauge claiming "full".
//  2. Any nonzero value draws at least one eighth. 0.4% must not be
//     pixel-identical to 0%.
//  3. Absent draws NOTHING — not an empty track, nothing. An empty track means
//     zero, so an absent gauge with a track would make "no data" and "zero"
//     pixel-identical. This is the load-bearing render assertion of the whole
//     HUD (design.md §7.1 principle 1).
//
// pct is nil for absent. The returned string is always exactly cells wide.
func gauge(pct *model.Percent, cells int, g Glyphs, sty Styles) string {
	if cells <= 0 {
		return ""
	}
	if pct == nil {
		// Rule 3.
		return strings.Repeat(" ", cells)
	}
	p := float64(*pct)
	// Out of range is not clamped: clamping invents a reading (103% shown as
	// an authoritative full bar). A non-conforming adapter value renders as
	// absent — the same "we don't know" the rest of the HUD uses. Conforming
	// adapters never get here; model.Validate rejects out-of-range percents.
	if p < 0 || p > 100 {
		return strings.Repeat(" ", cells)
	}

	if p >= 100 {
		return sty.Sev(p).Render(strings.Repeat(g.Fill, cells))
	}

	// Rule 1: the value is laid out over cells-1, leaving the last cell as
	// visible track for anything short of 100%.
	span := cells - 1
	var filled string
	var used int

	if len(g.Eighths) == 0 {
		// ASCII: whole cells only. Rule 2 still holds — any nonzero value
		// draws one cell.
		n := int(math.Round(p / 100 * float64(span)))
		if n == 0 && p > 0 {
			n = 1
		}
		if n > span {
			n = span
		}
		used = n
		filled = strings.Repeat(g.Fill, n)
	} else {
		eighths := int(math.Round(p / 100 * float64(span) * 8))
		if eighths == 0 && p > 0 {
			// Rule 2.
			eighths = 1
		}
		if eighths > span*8 {
			eighths = span * 8
		}
		full := eighths / 8
		rem := eighths % 8
		used = full
		filled = strings.Repeat(g.Fill, full)
		if rem > 0 {
			filled += g.Eighths[rem-1]
			used++
		}
	}

	track := strings.Repeat(g.Track, cells-used)
	return sty.Sev(p).Render(filled) + sty.Track.Render(track)
}
