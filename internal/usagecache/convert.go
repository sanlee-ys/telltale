package usagecache

import "github.com/sanlee-ys/telltale/internal/cursorhook"

// FromCursorTurn converts one afterAgentResponse payload into a Delta, and
// reports whether it may be accumulated at all.
//
// The gate is deliberately strict, and both halves of it are the same rule
// stated twice (design.md §4a.1: absent and zero are different states):
//
//   - an INCOMPLETE turn is refused. Summing the three counts a payload did
//     carry, and treating the fourth as zero, produces a total that is wrong
//     by an amount nothing on screen can name. The counter going quiet is
//     visible; a total drifting low is not.
//   - a NEGATIVE count is refused for the whole turn, not clamped. Clamping
//     would keep three trustworthy numbers company with one invented one.
//
// It sits in its own file for the same reason quotacache's does: the cache
// knows nothing about any vendor's payload shape, and the one function that
// does is the one that has to move when the vendor's shape moves.
func FromCursorTurn(t cursorhook.Turn) (Delta, bool) {
	if !t.Complete() || !t.Nonnegative() {
		return Delta{}, false
	}
	return Delta{
		InputTokens:      *t.InputTokens,
		OutputTokens:     *t.OutputTokens,
		CacheReadTokens:  *t.CacheReadTokens,
		CacheWriteTokens: *t.CacheWriteTokens,
	}, true
}
