// Package statusline renders the one-line Claude Code statusline.
//
// Honest-gauge rules enforced here (see decisions/001, docs/design.md §2):
//   - a segment renders only from a value present in the parsed input;
//   - an absent source hides the segment — it is never rendered as zero;
//   - derived displays (reset countdown) are arithmetic on a sourced value.
//
// This path has a single-digit-millisecond budget (ADR-002): stdlib only,
// no TUI framework, no reads beyond the already-read stdin. Render itself
// does no I/O at all; the quota relay (design.md §7.15) is the cmd layer's,
// and it runs after the line is already on stdout.
package statusline

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/sanlee-ys/telltale/internal/claude"
	"github.com/sanlee-ys/telltale/internal/theme"
)

// ANSI styling. Claude Code renders ANSI in statuslines (documented).
const (
	reset  = "\x1b[0m"
	dim    = "\x1b[2m"
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	red    = "\x1b[31m"
	cyan   = "\x1b[36m"
)

const sep = " \x1b[2m│\x1b[0m "

// Thresholds for percentage coloring (docs/design.md §2, §7.5): green below
// warn, yellow from warn, red from crit.
//
// The numbers live in internal/theme so the HUD cannot drift from them. That
// package is stdlib-only and holds no Style type precisely so this path can
// share the numbers without linking a TUI framework (ADR-002).
const (
	warnPct = theme.WarnPct
	critPct = theme.CritPct
)

type Options struct {
	NoColor bool
	// Now lets tests pin the clock for countdown rendering.
	Now time.Time
}

// Render produces the full statusline for the given input.
func Render(in *claude.StatuslineInput, opts Options) string {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	var segs []string
	if s, ok := modelSegment(in, opts); ok {
		segs = append(segs, s)
	}
	if s, ok := contextSegment(in, opts); ok {
		segs = append(segs, s)
	}
	if s, ok := cacheSegment(in, opts); ok {
		segs = append(segs, s)
	}
	if s, ok := costSegment(in, opts); ok {
		segs = append(segs, s)
	}
	if s, ok := windowSegment("5h", in.RateLimits.GetFiveHour(), opts); ok {
		segs = append(segs, s)
	}
	if s, ok := windowSegment("7d", in.RateLimits.GetSevenDay(), opts); ok {
		segs = append(segs, s)
	}
	if s, ok := dirSegment(in, opts); ok {
		segs = append(segs, s)
	}
	if s, ok := worktreeSegment(in, opts); ok {
		segs = append(segs, s)
	}
	line := strings.Join(segs, sep)
	if opts.NoColor {
		line = stripANSI(line)
	}
	return line
}

func modelSegment(in *claude.StatuslineInput, opts Options) (string, bool) {
	name := in.Model.DisplayName
	if name == "" {
		name = in.Model.ID
	}
	if name == "" {
		return "", false
	}
	return colorize(cyan, name, opts), true
}

func contextSegment(in *claude.StatuslineInput, opts Options) (string, bool) {
	if in.ContextWindow == nil || in.ContextWindow.UsedPercentage == nil {
		return "", false
	}
	p := *in.ContextWindow.UsedPercentage
	return fmt.Sprintf("ctx %s", pct(p, opts)), true
}

// cacheSegment renders the session's prompt-cache hit ratio: "cache 91%".
//
// It sits beside `ctx` because the two answer one question together — how full
// the window is, and how much of what fills it the vendor served from cache.
//
// # Why this segment is allowed to exist and a transcript-derived one is not
//
// `prompt_cache.hit_ratio` is REPORTED. The vendor sums cache reads, cache
// writes and uncached input across the session's main-conversation requests and
// divides, and claude.PromptCache's doc pins that formula to a 2026-09-04
// source read at CLI 2.1.260. This function reads the quotient and multiplies
// by 100 — the unit conversion §2.1 already permits — and computes nothing
// else. The same ratio built from the transcript's own
// `cache_read_input_tokens` would be arithmetic telltale invented over a
// head+tail read window, which ADR-001 refuses, so the adapter keeps the counts
// and gains no gauge.
//
// # Three absences hide the segment, and none of them renders as zero
//
//   - `prompt_cache` absent — a CLI older than 2.1.251, or a session before its
//     first API response (the vendor emits the key only once `requests` is
//     nonzero);
//   - `caching_observed` false — no response this session reported cache tokens,
//     so prompt caching is off or this provider does not report it. That is an
//     unread field, not a 0% hit rate, and rendering `cache 0%` there would
//     claim a measurement nobody took;
//   - `hit_ratio` null — the vendor's own guard against dividing by three zero
//     counts.
//
// A ratio of 0 with caching observed IS a reading and renders `cache 0%`.
//
// # Why this number carries no threshold colour
//
// Every other percentage on this line is a CONSUMPTION — context filled, quota
// spent — so pct() paints high values red. A cache hit ratio inverts that: 91%
// is the healthy end. Reusing pct() would paint a well-cached session red, and
// inverting the scale would mean picking the ratio at which a cache becomes
// "bad", which nothing here has measured. So the value renders unpainted and
// the word `cache` carries the whole distinction — which is what the ASCII /
// NO_COLOR / 16-colour rule asks of every distinction on this UI anyway.
func cacheSegment(in *claude.StatuslineInput, opts Options) (string, bool) {
	pc := in.PromptCache
	if pc == nil || pc.HitRatio == nil {
		return "", false
	}
	if pc.CachingObserved == nil || !*pc.CachingObserved {
		return "", false
	}
	// One decimal only when the source has one, matching pct()'s convention on
	// this line. The ×100 is the unit conversion; nothing else is computed.
	//
	// The rounding is display precision and it is not optional here: a ratio
	// arrives as a raw quotient, and 0.87*100 lands on 87.00000000000001 in
	// float64, so the whole-number branch below would miss and the line would
	// read `cache 87.0%` for one value and `cache 91%` for the next. Rounding
	// to the tenth this line already shows makes that branch answer the
	// question it means to ask.
	p := math.Round(*pc.HitRatio*1000) / 10
	if p == float64(int64(p)) {
		return fmt.Sprintf("cache %d%%", int64(p)), true
	}
	return fmt.Sprintf("cache %.1f%%", p), true
}

func costSegment(in *claude.StatuslineInput, opts Options) (string, bool) {
	if in.Cost == nil || in.Cost.TotalCostUSD == nil {
		return "", false
	}
	return fmt.Sprintf("$%.2f", *in.Cost.TotalCostUSD), true
}

// windowSegment renders one rate-limit window: "5h 23% ↻2h13m".
// Absent window, or a window without used_percentage, hides the segment
// entirely — an API-key login must show nothing here, not 0%.
func windowSegment(label string, w *claude.Window, opts Options) (string, bool) {
	if w == nil || w.UsedPercentage == nil {
		return "", false
	}
	s := fmt.Sprintf("%s %s", label, pct(*w.UsedPercentage, opts))
	if w.ResetsAt != nil {
		if d := time.Unix(*w.ResetsAt, 0).Sub(opts.Now); d > 0 {
			// Space after the glyph, matching the HUD: ↻ renders at ambiguous
			// width in common fonts and glued digits read as one garbled token.
			// theme.Countdown is the shared formatter — the local shortDur had
			// no days branch and rendered a 7d window as "122h13m".
			s += colorize(dim, " ↻ "+theme.Countdown(d), opts)
		}
	}
	return s, true
}

// dirSegment shows the working folder's basename, sourced from the stdin
// payload only — no filesystem or git calls on this path. Git branch is
// deliberately NOT here: it would need an exec, and the statusline path
// reads nothing beyond stdin (docs/design.md §2).
func dirSegment(in *claude.StatuslineInput, opts Options) (string, bool) {
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

func worktreeSegment(in *claude.StatuslineInput, opts Options) (string, bool) {
	if in.Worktree == nil || in.Worktree.Name == "" {
		return "", false
	}
	return colorize(dim, "⌥"+in.Worktree.Name, opts), true
}

// pct formats a percentage with threshold coloring. Values arrive as
// percentages (e.g. 23.5) from the source; no rescaling happens here.
func pct(p float64, opts Options) string {
	c := green
	switch {
	case p >= critPct:
		c = red
	case p >= warnPct:
		c = yellow
	}
	// Whole numbers render without a decimal; source precision is otherwise kept to one place.
	if p == float64(int64(p)) {
		return colorize(c, fmt.Sprintf("%d%%", int64(p)), opts)
	}
	return colorize(c, fmt.Sprintf("%.1f%%", p), opts)
}

func colorize(code, s string, opts Options) string {
	if opts.NoColor {
		return s
	}
	return code + s + reset
}

func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if r == 'm' {
				inEsc = false
			}
		case r == '\x1b':
			inEsc = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
