package council

import (
	"strings"
	"testing"
	"time"
)

// The four one-line render helpers in view.go — shortSHA, ordinal, phaseWord and
// dur — reach the screen through the arena's finish line and every column
// header, and until now nothing called them directly. What coverage they had was
// incidental: goldens that would have accepted a different string had it been
// there all along, and tests like arena_test.go:612 that build their expected
// value by calling shortSHA on the other side of the comparison, which cannot
// catch a shortSHA bug at all — it can only catch a caller that forgot to call
// it.
//
// These tests pin the helpers at the edges the callers never reach: zero,
// negative, the exact rollover, and inputs longer than the helper's assumption.
// Where the current answer at an edge is arithmetic rather than intent
// ("-21th", an hour drawn as "60m0s"), the test says so in its own comment
// rather than dressing the behavior up as a decision — a pinned edge is a record
// of what happens, and the note is how a later reader tells the two apart.

// TestShortSHACutsAtSevenAndLeavesShorterInputsAlone.
//
// Seven is the git convention, and the guard below it is the one that matters:
// the synthetic bases tests construct ("base", "HEAD") and the empty string a
// not-yet-committed attempt carries are all shorter than seven, and a naive
// sha[:7] would panic on every one of them. The 7/8 pair is the off-by-one — at
// exactly seven nothing is dropped, at eight exactly one character is.
func TestShortSHACutsAtSevenAndLeavesShorterInputsAlone(t *testing.T) {
	for _, tc := range []struct {
		name string
		sha  string
		want string
	}{
		{"empty", "", ""},
		{"one character", "a", "a"},
		{"six, one under the cut", "abcdef", "abcdef"},
		{"exactly seven", "abcdef0", "abcdef0"},
		{"eight, one over the cut", "abcdef01", "abcdef0"},
		{"a full forty-character sha", "0123456789abcdef0123456789abcdef01234567", "0123456"},
		{"a symbolic base, not a sha at all", "HEAD", "HEAD"},
		{"a synthetic base from a fixture", "base", "base"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shortSHA(tc.sha); got != tc.want {
				t.Errorf("shortSHA(%q) = %q, want %q", tc.sha, got, tc.want)
			}
		})
	}
}

// TestShortSHAOnlyEverShortens.
//
// The property behind the table: whatever comes out is a prefix of what went in,
// at most seven bytes long. It is stated separately because it is the claim the
// callers depend on — "committed <x>." is a sentence about a real commit, so a
// shortSHA that ever added or reordered a character would put a commit id on
// screen that does not exist.
//
// The multi-byte case is here to record a limit, not to bless the output: the
// cut is a BYTE slice, so a seven-byte prefix of a multi-byte string can split a
// rune and render as a replacement character. Every real caller passes hex or a
// git refname, so this is unreachable today; the test asserts only the prefix
// and the length, which stay true either way, and deliberately does not assert
// that the mojibake is correct.
func TestShortSHAOnlyEverShortens(t *testing.T) {
	for _, in := range []string{
		"",
		"a",
		"abcdef0",
		"abcdef01",
		"0123456789abcdef0123456789abcdef01234567",
		"HEAD",
		"feature/some-branch-name",
		"日本語ですよねこれは",
	} {
		got := shortSHA(in)
		if !strings.HasPrefix(in, got) {
			t.Errorf("shortSHA(%q) = %q, which is not a prefix of the input", in, got)
		}
		if len(got) > 7 {
			t.Errorf("shortSHA(%q) = %q, %d bytes long, want at most 7", in, got, len(got))
		}
	}
}

// TestOrdinalSpellsTheTeensRule.
//
// The teens are the whole reason ordinal is three lines instead of one, and the
// pairs are what make the rule testable: 1/11/21/101/111 all end in the same
// digit and only two of them take "st". Council seats five today (§9.39), so
// everything past 5th is guarding an assumption rather than a live case — which
// is exactly the doc comment's argument for writing the rule out, and the reason
// the test goes past the rank the room can currently produce.
func TestOrdinalSpellsTheTeensRule(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		{0, "0th"}, // no rank is zero, but zero is the value an unset int has
		{1, "1st"},
		{2, "2nd"},
		{3, "3rd"},
		{4, "4th"},
		{5, "5th"}, // the last rank five seats can produce
		{9, "9th"},
		{10, "10th"},
		{11, "11th"}, // the teens: ends in 1, still "th"
		{12, "12th"},
		{13, "13th"},
		{14, "14th"}, // and out the other side
		{20, "20th"},
		{21, "21st"}, // the bug the teens rule exists to not have
		{22, "22nd"},
		{23, "23rd"},
		{100, "100th"},
		{101, "101st"}, // 101%100 is 1, not a teen
		{102, "102nd"},
		{103, "103rd"},
		{111, "111th"}, // 111%100 is 11, a teen again
		{112, "112th"},
		{113, "113th"},
		{1013, "1013th"}, // the teens rule reads two digits, not the whole number
		{1021, "1021st"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			if got := ordinal(tc.n); got != tc.want {
				t.Errorf("ordinal(%d) = %q, want %q", tc.n, got, tc.want)
			}
		})
	}
}

// TestOrdinalOnANegativeRank records arithmetic, not intent.
//
// A rank comes from a finish order and cannot be negative, so nothing here is a
// string a user can see. It is pinned because Go's % keeps the sign of the
// dividend: -21%10 is -1, which matches no case in the switch, so every negative
// falls through to "th" and "-21th" is what comes out. That is not English and
// this test does not claim it is — it claims the helper does not panic and does
// not accidentally produce "-21st", so a future reader who changes the modulo
// arithmetic finds out here rather than in a rendered room.
func TestOrdinalOnANegativeRank(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		{-1, "-1th"},
		{-2, "-2th"},
		{-3, "-3th"},
		{-11, "-11th"},
		{-21, "-21th"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			if got := ordinal(tc.n); got != tc.want {
				t.Errorf("ordinal(%d) = %q, want %q", tc.n, got, tc.want)
			}
		})
	}
}

// TestPhaseWordNamesEveryDeclaredPhase.
//
// The doc comment's claim is that a rank must never print without the word
// saying what KIND of finish it ranks, so the table covers all six declared
// phases plus a value past the end of the enum — the case a later constant would
// arrive as if someone adds a phase and forgets this switch.
//
// The three non-terminal phases collapsing to "running" is intended: phaseWord
// is finish-line vocabulary, reached from the arena's rank line, where the only
// question is whether this seat has stopped and how. "Idle" and "waiting" and
// "streaming" are all, from that line's point of view, not finished yet.
func TestPhaseWordNamesEveryDeclaredPhase(t *testing.T) {
	for _, tc := range []struct {
		name  string
		phase Phase
		want  string
	}{
		{"idle", PhaseIdle, "running"},
		{"waiting", PhaseWaiting, "running"},
		{"streaming", PhaseStreaming, "running"},
		{"done", PhaseDone, "done"},
		{"failed", PhaseFailed, "failed"},
		{"cancelled", PhaseCancelled, "cancelled"},
		{"past the end of the enum", Phase(200), "running"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := phaseWord(tc.phase); got != tc.want {
				t.Errorf("phaseWord(%v) = %q, want %q", tc.phase, got, tc.want)
			}
		})
	}
}

// TestPhaseWordKeepsTheThreeFinishesDistinct.
//
// "2nd · failed" and "2nd · done" are different facts (§4a.1) — the rank alone
// says nothing about whether the seat succeeded. This asserts the property the
// table above only implies: the three terminal phases produce three different
// words, and none of them is the word a still-running seat gets. A switch that
// mapped cancelled onto failed would still pass every golden that happens not to
// contain a cancelled seat; it would not pass this.
func TestPhaseWordKeepsTheThreeFinishesDistinct(t *testing.T) {
	seen := map[string]Phase{}
	for _, p := range []Phase{PhaseDone, PhaseFailed, PhaseCancelled} {
		w := phaseWord(p)
		if prior, dup := seen[w]; dup {
			t.Errorf("phase %v and phase %v both render as %q — a rank cannot say which finish it ranks", prior, p, w)
		}
		seen[w] = p
		if w == phaseWord(PhaseStreaming) {
			t.Errorf("terminal phase %v renders as %q, the same word a running seat gets", p, w)
		}
	}
}

// TestPhaseWordIsAlwaysOneWord.
//
// The word lands mid-sentence between two ` · ` separators, so an empty or
// multi-word answer would break the finish line's shape rather than just read
// oddly. Phase is a uint8, so the whole domain is 256 values and can simply be
// walked.
func TestPhaseWordIsAlwaysOneWord(t *testing.T) {
	for i := 0; i < 256; i++ {
		w := phaseWord(Phase(i))
		if w == "" {
			t.Fatalf("phaseWord(Phase(%d)) is empty", i)
		}
		if len(strings.Fields(w)) != 1 || strings.TrimSpace(w) != w {
			t.Fatalf("phaseWord(Phase(%d)) = %q, want a single bare word", i, w)
		}
	}
}

// TestDurRollsOverAtSixtySeconds.
//
// One-second resolution and a minutes rollover, and the two edges worth having
// in writing are 59→60 (the only place the format changes shape) and zero.
//
// Zero renders "0s", not "" — the distinction this repo exists to keep (§4a.1).
// A turn measured at under a second really did happen and its duration really is
// approximately zero; the absent case, a turn with no start time, is caught
// upstream in elapsedSince and returns the empty string before dur is ever
// called. If dur started returning "" for zero, those two states would print the
// same and a measured zero would read as "we do not know".
func TestDurRollsOverAtSixtySeconds(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero is a measured zero, not an absence", 0, "0s"},
		{"one second", time.Second, "1s"},
		{"fifty-nine, the last second before the rollover", 59 * time.Second, "59s"},
		{"sixty, the rollover itself", 60 * time.Second, "1m0s"},
		{"sixty-one", 61 * time.Second, "1m1s"},
		{"one second short of two minutes", 119 * time.Second, "1m59s"},
		{"two minutes exactly", 120 * time.Second, "2m0s"},
		{"one second short of an hour", 3599 * time.Second, "59m59s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dur(tc.d); got != tc.want {
				t.Errorf("dur(%v) = %q, want %q", tc.d, got, tc.want)
			}
		})
	}
}

// TestDurTruncatesTowardZeroRatherThanRounding.
//
// The doc comment's argument is that a hundred milliseconds is noise on a
// measurement of how long a model took to think, so the sub-second part is
// dropped rather than rounded. 999ms is therefore "0s" and 59.999s is "59s" —
// both a full unit below what rounding would give. Pinned because "0s" for
// something that took nearly a second looks like a bug on sight, and the next
// reader should find the decision here instead of "fixing" it.
func TestDurTruncatesTowardZeroRatherThanRounding(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{1 * time.Nanosecond, "0s"},
		{500 * time.Millisecond, "0s"},
		{999 * time.Millisecond, "0s"},
		{1500 * time.Millisecond, "1s"},
		{59999 * time.Millisecond, "59s"},
		{60500 * time.Millisecond, "1m0s"},
	} {
		t.Run(tc.d.String(), func(t *testing.T) {
			if got := dur(tc.d); got != tc.want {
				t.Errorf("dur(%v) = %q, want %q", tc.d, got, tc.want)
			}
		})
	}
}

// TestDurHasNoHoursUnit.
//
// There are two units, and minutes is the largest — an hour is "60m0s" and a
// long arena attempt keeps counting in minutes. That is a real product claim,
// not an oversight: the number measures one model turn, and a reader watching a
// seat work reads "94m" faster than they parse "1h34m". Written down so a change
// to it is a deliberate change and not a silent one, and so the arithmetic is
// checked well past the range a turn actually reaches.
func TestDurHasNoHoursUnit(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{time.Hour, "60m0s"},
		{time.Hour + time.Second, "60m1s"},
		{94*time.Minute + 30*time.Second, "94m30s"},
		{24 * time.Hour, "1440m0s"},
		{100 * time.Hour, "6000m0s"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			if got := dur(tc.d); got != tc.want {
				t.Errorf("dur(%v) = %q, want %q", tc.d, got, tc.want)
			}
		})
	}
}

// TestDurOnANegativeDuration records arithmetic, not intent — the dur twin of
// the negative-ordinal test.
//
// elapsedSince already returns "" for a negative interval, so this is not a
// string the elapsed path can produce. The other callers pass a stored
// Column.Elapsed, and a duration computed across a clock adjustment is the one
// way a negative could reach here. What comes out then is worth knowing: the
// `s < 60` branch catches every negative, so there is no minutes rollover on
// the negative side and -90s prints as "-90s" rather than "-1m-30s". Malformed
// either way; this pins which malformed, and pins that it does not panic.
func TestDurOnANegativeDuration(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{-1 * time.Nanosecond, "0s"}, // truncation toward zero, so not even signed
		{-1 * time.Second, "-1s"},
		{-1500 * time.Millisecond, "-1s"},
		{-59 * time.Second, "-59s"},
		{-90 * time.Second, "-90s"}, // no rollover below zero
		{-1 * time.Hour, "-3600s"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			if got := dur(tc.d); got != tc.want {
				t.Errorf("dur(%v) = %q, want %q", tc.d, got, tc.want)
			}
		})
	}
}
