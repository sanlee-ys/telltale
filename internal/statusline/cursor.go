// Cursor CLI (cursor-agent) statusline rendering. Same honest-gauge rules and
// millisecond budget as the other two paths (see package doc); a third render
// function rather than a normalized input for the reason §2.1 already gives —
// the vendors disagree about what exists, and flattening them would invent
// fields or drop vendor truth.
//
// What this payload does NOT have is most of what the other two render: no
// cost anywhere, no quota of any kind, no vendor-reported agent state, no vcs
// object. What it uniquely has is `autorun`. The line is short on purpose.
package statusline

import (
	"fmt"
	"strings"
	"time"

	"github.com/sanlee-ys/telltale/internal/cursorstatus"
)

// RenderCursor produces the full statusline for a cursor-agent payload.
//
// Reached only via `telltale statusline --vendor cursor`, because this vendor
// stamps no marker on its payload (internal/cursorstatus package doc).
func RenderCursor(in *cursorstatus.StatuslineInput, opts Options) string {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	var segs []string
	if s, ok := cursorModelSegment(in, opts); ok {
		segs = append(segs, s)
	}
	if s, ok := cursorContextSegment(in, opts); ok {
		segs = append(segs, s)
	}
	if s, ok := cursorAutorunSegment(in, opts); ok {
		segs = append(segs, s)
	}
	if s, ok := cursorWorktreeSegment(in, opts); ok {
		segs = append(segs, s)
	}
	if s, ok := cursorDirSegment(in, opts); ok {
		segs = append(segs, s)
	}
	line := strings.Join(segs, sep)
	if opts.NoColor {
		line = stripANSI(line)
	}
	return line
}

func cursorModelSegment(in *cursorstatus.StatuslineInput, opts Options) (string, bool) {
	name := in.Model.DisplayName
	if name == "" {
		name = in.Model.ID
	}
	if name == "" {
		return "", false
	}
	return colorize(cyan, name, opts), true
}

// cursorContextSegment renders the ONE context number this vendor sources
// rather than computes. `remaining_percentage` and `total_input_tokens` never
// reach this function — they are not on the struct at all, deliberately
// (internal/cursorstatus package doc).
//
// A payload whose `context_window` is present with every key null is a session
// that has not made an API call yet. That is a read this gauge could not get,
// not a measured zero, so the segment hides — the zero-vs-absent rule, on the
// one path where the vendor hands over the absent case by default.
func cursorContextSegment(in *cursorstatus.StatuslineInput, opts Options) (string, bool) {
	if in.ContextWindow == nil || in.ContextWindow.UsedPercentage == nil {
		return "", false
	}
	return fmt.Sprintf("ctx %s", pct(*in.ContextWindow.UsedPercentage, opts)), true
}

// cursorAutorunSegment renders the vendor's auto-run posture, and only when it
// is ON.
//
// The asymmetry is deliberate and is NOT the zero-vs-absent rule being bent.
// That rule governs gauge READINGS, where 0% and "no source" are two different
// facts a user must be able to tell apart. This is a posture flag with a
// default: `autorun:false` is the ordinary state of every session, and a
// statusline that spends a segment saying "nothing unusual" on every line
// teaches the reader to stop looking at it. Off and absent both render nothing;
// on renders a word, in yellow, because it is the state where the agent may run
// a command without asking first.
func cursorAutorunSegment(in *cursorstatus.StatuslineInput, opts Options) (string, bool) {
	if in.Autorun == nil || !*in.Autorun {
		return "", false
	}
	return colorize(yellow, "autorun", opts), true
}

// cursorWorktreeSegment uses the same ⌥ mark as the Claude path so one reader
// watching two vendors learns one glyph. Documented by the vendor; not observed
// live (internal/cursorstatus package doc).
func cursorWorktreeSegment(in *cursorstatus.StatuslineInput, opts Options) (string, bool) {
	if in.Worktree == nil || in.Worktree.Name == "" {
		return "", false
	}
	return colorize(dim, "⌥"+in.Worktree.Name, opts), true
}

// cursorDirSegment shows the working folder's basename from the payload only —
// no filesystem or git call, same as the other two paths.
func cursorDirSegment(in *cursorstatus.StatuslineInput, opts Options) (string, bool) {
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
