// Antigravity CLI (agy) statusline rendering. Same honest-gauge rules and
// millisecond budget as the Claude path (see package doc); a separate render
// function rather than a normalized input because the two vendors' payloads
// disagree about what exists (named quota buckets vs fixed windows, reported
// agent state, in-payload vcs), and flattening them would either invent
// fields or drop vendor truth.
package statusline

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sanlee-ys/telltale/internal/antigravity"
	"github.com/sanlee-ys/telltale/internal/theme"
)

// RenderAntigravity produces the full statusline for an agy payload.
func RenderAntigravity(in *antigravity.StatuslineInput, opts Options) string {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	var segs []string
	if s, ok := agyModelSegment(in, opts); ok {
		segs = append(segs, s)
	}
	if s, ok := agyContextSegment(in, opts); ok {
		segs = append(segs, s)
	}
	segs = append(segs, agyQuotaSegments(in, opts)...)
	if s, ok := agyStateSegment(in, opts); ok {
		segs = append(segs, s)
	}
	if s, ok := agyBranchSegment(in, opts); ok {
		segs = append(segs, s)
	}
	if s, ok := agyDirSegment(in, opts); ok {
		segs = append(segs, s)
	}
	line := strings.Join(segs, sep)
	if opts.NoColor {
		line = stripANSI(line)
	}
	return line
}

func agyModelSegment(in *antigravity.StatuslineInput, opts Options) (string, bool) {
	name := in.Model.DisplayName
	if name == "" {
		name = in.Model.ID
	}
	if name == "" {
		return "", false
	}
	return colorize(cyan, name, opts), true
}

func agyContextSegment(in *antigravity.StatuslineInput, opts Options) (string, bool) {
	if in.ContextWindow == nil || in.ContextWindow.UsedPercentage == nil {
		return "", false
	}
	return fmt.Sprintf("ctx %s", pct(*in.ContextWindow.UsedPercentage, opts)), true
}

// agyQuotaSegments renders every named quota bucket, sorted by id so the line
// is stable across invocations (Go map order is not).
//
// The vendor reports the REMAINING fraction; used% = (1-remaining)*100 is a
// unit conversion on the sourced value, the same rule that permits the reset
// countdown. Bucket ids are rendered VERBATIM: they are vendor vocabulary
// ("gemini-weekly", "3p-weekly" observed), and translating them through an
// assumed mapping is how a gauge starts narrating. A bucket without
// remaining_fraction hides entirely — never 0%, never 100%.
func agyQuotaSegments(in *antigravity.StatuslineInput, opts Options) []string {
	if len(in.Quota) == 0 {
		return nil
	}
	ids := make([]string, 0, len(in.Quota))
	for id := range in.Quota {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var segs []string
	for _, id := range ids {
		b := in.Quota[id]
		if b == nil || b.RemainingFraction == nil {
			continue
		}
		used := (1 - *b.RemainingFraction) * 100
		s := colorize(dim, id, opts) + " " + pct(used, opts)
		if d, ok := agyResetIn(b, opts.Now); ok && d > 0 {
			s += colorize(dim, " ↻ "+theme.Countdown(d), opts)
		}
		segs = append(segs, s)
	}
	return segs
}

// agyResetIn prefers the vendor's relative reset_in_seconds (no clock
// comparison needed) and falls back to the absolute reset_time.
func agyResetIn(b *antigravity.QuotaBucket, now time.Time) (time.Duration, bool) {
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

// agyStateSegment renders the vendor-reported agent state — the one signal no
// other vendor's seam offers. A pending tool confirmation outranks the state
// word: "waiting on you" is the fact the user needs.
//
// Unknown state strings render verbatim in dim: the vendor's vocabulary may
// grow, and a state the gauge does not recognize is still the vendor's truth.
func agyStateSegment(in *antigravity.StatuslineInput, opts Options) (string, bool) {
	if in.ToolConfirmationPending {
		return colorize(red, "confirm?", opts), true
	}
	switch in.AgentState {
	case "":
		return "", false
	case "idle":
		return colorize(dim, "idle", opts), true
	case "thinking":
		return colorize(yellow, "thinking", opts), true
	case "working":
		return colorize(cyan, "working", opts), true
	case "tool_use":
		return colorize(cyan, "tool", opts), true
	case "initializing":
		return colorize(dim, "init", opts), true
	default:
		return colorize(dim, in.AgentState, opts), true
	}
}

// agyBranchSegment is sourced from the payload's vcs object — agy puts the
// branch on stdin, so unlike the Claude path no exec is needed and the
// no-I/O-beyond-stdin rule holds. Documented; not yet observed live (§3.8).
func agyBranchSegment(in *antigravity.StatuslineInput, opts Options) (string, bool) {
	if in.VCS == nil || in.VCS.Branch == "" {
		return "", false
	}
	b := in.VCS.Branch
	if in.VCS.Dirty {
		b += "*"
	}
	return colorize(dim, b, opts), true
}

func agyDirSegment(in *antigravity.StatuslineInput, opts Options) (string, bool) {
	dir := in.Cwd
	if in.Workspace != nil && in.Workspace.CurrentDir != "" {
		dir = in.Workspace.CurrentDir
	}
	if dir == "" {
		return "", false
	}
	base := dir
	for i := len(dir) - 1; i >= 0; i-- {
		if dir[i] == '/' || dir[i] == '\\' {
			base = dir[i+1:]
			break
		}
	}
	if base == "" {
		return "", false
	}
	return colorize(dim, base, opts), true
}
