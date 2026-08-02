package hud

import (
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The forecast's arithmetic is pinned against INJECTED sample series, never
// against a clock: the whole feature is a claim about a measured rate, and a
// test whose numbers depend on how long it took to run cannot pin a rate.

// series is a linear ramp ending at the pinned clock.
func series(from, to float64, span time.Duration, n int, resets *time.Time) []BurnSample {
	out := make([]BurnSample, 0, n)
	for i := 0; i < n; i++ {
		f := float64(i) / float64(n-1)
		out = append(out, BurnSample{
			At:     pinned.Add(-span).Add(time.Duration(f * float64(span))),
			Used:   from + f*(to-from),
			Resets: resets,
		})
	}
	return out
}

func burnWith(id string, s []BurnSample) Burn {
	return Burn{Series: []BurnSeries{{WindowID: id, Samples: s}}}
}

// The whole point of the minimum basis: below it, telltale renders NOTHING.
// Two samples fit a line through themselves and cannot disagree; ninety
// seconds of a five-hour window measures a step, not a rate.
func TestForecastRefusesToProjectBelowTheMinimumBasis(t *testing.T) {
	cases := []struct {
		name  string
		burn  Burn
		wantF bool
	}{
		{"two samples over an hour", burnWith("w", series(10, 40, time.Hour, 2, nil)), false},
		{"three samples over 90s", burnWith("w", series(10, 40, 90*time.Second, 3, nil)), false},
		{"three samples over 4m59s", burnWith("w", series(10, 40, 299*time.Second, 3, nil)), false},
		{"three samples over 5m", burnWith("w", series(10, 40, 5*time.Minute, 3, nil)), true},
		{"no samples at all", Burn{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, ok := c.burn.Forecast("w", pinned)
			if ok != c.wantF {
				t.Errorf("Forecast ok=%v, want %v", ok, c.wantF)
			}
		})
	}
}

// The arithmetic, pinned exactly. 30% to 42% over 18 minutes is 12 points per
// 1080 seconds; 58 points remain, so the window fills 5220 seconds after the
// last sample.
func TestForecastArithmeticIsPinned(t *testing.T) {
	b := burnWith("five_hour", series(30, 42, 18*time.Minute, 7, nil))
	f, ok := b.Forecast("five_hour", pinned)
	if !ok {
		t.Fatal("no forecast from a 7-sample 18-minute rising series")
	}
	want := pinned.Add(5220 * time.Second)
	if !f.At.Equal(want) {
		t.Errorf("projected %v, want %v", f.At, want)
	}
	if f.Basis != 18*time.Minute {
		t.Errorf("basis = %v, want 18m — the basis IS the scope of the claim", f.Basis)
	}
	if f.Samples != 7 {
		t.Errorf("samples = %d, want 7", f.Samples)
	}
}

// The projection is anchored to the LAST OBSERVED value, not to the fitted
// line, so the forecast starts from the percentage rendered beside it. A
// series with one low outlier at the end must still project from that reading.
func TestForecastAnchorsToTheLastObservedValue(t *testing.T) {
	s := series(30, 42, 18*time.Minute, 7, nil)
	// Same slope, but every reading shifted up: the ETA must move earlier by
	// exactly the shift divided by the slope.
	shifted := append([]BurnSample(nil), s...)
	for i := range shifted {
		shifted[i].Used += 10
	}
	a, _ := burnWith("w", s).Forecast("w", pinned)
	b, _ := burnWith("w", shifted).Forecast("w", pinned)
	if !b.At.Before(a.At) {
		t.Fatalf("a series 10 points further along projected no earlier: %v vs %v", b.At, a.At)
	}
	slope := 12.0 / 1080.0
	want := a.At.Add(-time.Duration(10 / slope * float64(time.Second)))
	if d := b.At.Sub(want); d > time.Second || d < -time.Second {
		t.Errorf("projected %v, want %v (anchored to the last reading)", b.At, want)
	}
}

// "You will never run out" is not a time. A flat or falling window renders
// nothing rather than an infinity or a date in the past.
func TestForecastRefusesAFlatOrFallingWindow(t *testing.T) {
	for _, c := range []struct {
		name     string
		from, to float64
	}{
		{"flat", 40, 40},
		{"falling", 40, 30},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := burnWith("w", series(c.from, c.to, 20*time.Minute, 5, nil)).Forecast("w", pinned); ok {
				t.Error("a window that is not filling produced a fill time")
			}
		})
	}
}

// Projecting past the window's own reset describes a window that will not
// exist. The countdown beside it already says when the slate is wiped.
func TestForecastRefusesToProjectPastTheWindowReset(t *testing.T) {
	soon := pinned.Add(20 * time.Minute)
	late := pinned.Add(6 * time.Hour)
	// 30 -> 42 over 18m fills in 87 minutes, which is after `soon` and before
	// `late`.
	if _, ok := burnWith("w", series(30, 42, 18*time.Minute, 7, &soon)).Forecast("w", pinned); ok {
		t.Error("projected an exhaustion the window resets before reaching")
	}
	if _, ok := burnWith("w", series(30, 42, 18*time.Minute, 7, &late)).Forecast("w", pinned); !ok {
		t.Error("refused a projection that lands well inside the window")
	}
}

// The render is a wall clock with no date on it, so anything past a day is
// ambiguous rather than informative.
func TestForecastRefusesBeyondADay(t *testing.T) {
	// 18 -> 18.1 over 18 minutes: a seven-day window barely moving.
	if _, ok := burnWith("seven_day", series(18, 18.1, 18*time.Minute, 7, nil)).Forecast("seven_day", pinned); ok {
		t.Error("projected a time more than 24h out into a format with no date in it")
	}
}

// A rollover discards the history: fitting a line across the discontinuity
// would report a negative rate or a wild one, and every sample before it
// describes a window that no longer exists.
func TestUsageDropClearsTheSamples(t *testing.T) {
	var b Burn
	base := pinned.Add(-20 * time.Minute)
	for i := 0; i < 5; i++ {
		b.Observe([]model.QuotaWindow{{ID: "w", Label: "5h", UsedPercent: model.PercentPtr(float64(80 + i))}}, "test-src",
			base.Add(time.Duration(i)*5*time.Minute))
	}
	if n := len(b.Series[0].Samples); n != 5 {
		t.Fatalf("collected %d samples, want 5", n)
	}
	b.Observe([]model.QuotaWindow{{ID: "w", Label: "5h", UsedPercent: model.PercentPtr(3)}}, "test-src", pinned)
	if n := len(b.Series[0].Samples); n != 1 {
		t.Errorf("after a usage drop the buffer holds %d samples, want only the post-reset one", n)
	}
	if _, ok := b.Forecast("w", pinned); ok {
		t.Error("a forecast survived a window rollover")
	}
}

// The other rollover signal: the vendor's reset time jumps forward by a whole
// window. Jitter of a few seconds is not a rollover.
func TestResetsAtJumpClearsTheSamplesButJitterDoesNot(t *testing.T) {
	first := pinned.Add(time.Hour)
	jitter := first.Add(3 * time.Second)
	next := first.Add(5 * time.Hour)

	build := func(final *time.Time) Burn {
		var b Burn
		base := pinned.Add(-20 * time.Minute)
		for i := 0; i < 4; i++ {
			b.Observe([]model.QuotaWindow{{ID: "w", Label: "5h",
				UsedPercent: model.PercentPtr(float64(10 + i)), ResetsAt: &first}},
				"test-src", base.Add(time.Duration(i)*5*time.Minute))
		}
		b.Observe([]model.QuotaWindow{{ID: "w", Label: "5h",
			UsedPercent: model.PercentPtr(20), ResetsAt: final}}, "test-src", pinned)
		return b
	}
	if n := len(build(&jitter).Series[0].Samples); n != 5 {
		t.Errorf("three seconds of reset-time jitter discarded the history (%d samples left)", n)
	}
	if n := len(build(&next).Series[0].Samples); n != 1 {
		t.Errorf("a five-hour jump in resets_at kept %d samples, want only the post-rollover one", n)
	}
}

// The poll runs at 1 Hz. Sampling at 1 Hz would store fifteen copies of the
// same stepped reading per interval and let the newest step dominate the fit.
func TestObserveThrottlesToTheSampleInterval(t *testing.T) {
	var b Burn
	for i := 0; i < 60; i++ {
		b.Observe([]model.QuotaWindow{{ID: "w", Label: "5h", UsedPercent: model.PercentPtr(float64(i))}}, "test-src",
			pinned.Add(time.Duration(i)*time.Second))
	}
	// 60 seconds at a 15-second floor: t=0, 15, 30, 45.
	if n := len(b.Series[0].Samples); n != 4 {
		t.Errorf("kept %d samples over 60s at a %s floor, want 4", n, sampleInterval)
	}
}

// The buffer is bounded by AGE as well as by count: a forecast quoting a basis
// from an hour ago is describing a session the user has moved on from.
func TestObserveForgetsSamplesOlderThanTheWindow(t *testing.T) {
	var b Burn
	start := pinned.Add(-2 * time.Hour)
	for i := 0; i < 200; i++ {
		b.Observe([]model.QuotaWindow{{ID: "w", Label: "5h", UsedPercent: model.PercentPtr(float64(i) / 4)}}, "test-src",
			start.Add(time.Duration(i)*time.Minute))
	}
	s := b.Series[0].Samples
	if len(s) > maxSamples {
		t.Errorf("ring grew to %d samples, cap is %d", len(s), maxSamples)
	}
	span := s[len(s)-1].At.Sub(s[0].At)
	if span > sampleWindow {
		t.Errorf("retained %v of history, window is %v", span, sampleWindow)
	}
}

// A window with no usage figure this scan is a gap, not a reset: dropping the
// history for it would throw away real measurements over a momentary absence,
// and inventing a repeat of the last value would flatten the measured slope
// with data we did not measure.
func TestObserveIgnoresAWindowWithNoUsageFigure(t *testing.T) {
	var b Burn
	base := pinned.Add(-20 * time.Minute)
	for i := 0; i < 4; i++ {
		b.Observe([]model.QuotaWindow{{ID: "w", Label: "5h", UsedPercent: model.PercentPtr(float64(10 + i*3))}}, "test-src",
			base.Add(time.Duration(i)*5*time.Minute))
	}
	before := len(b.Series[0].Samples)
	b.Observe([]model.QuotaWindow{{ID: "w", Label: "5h"}}, "test-src", pinned)
	if got := len(b.Series[0].Samples); got != before {
		t.Errorf("a nil UsedPercent changed the buffer from %d to %d samples", before, got)
	}
}

// A clock that does not advance cannot order the series, and a line fitted
// over a zero or negative interval is a rate with no meaning or the wrong
// sign. The rollover check runs first, so the sample must RISE to reach the
// clock guard at all — otherwise the drop is read as a window rollover.
func TestObserveRefusesASampleFromANonAdvancingClock(t *testing.T) {
	for _, c := range []struct {
		name string
		at   time.Time
	}{
		{"backwards", pinned.Add(-time.Hour)},
		{"identical", pinned},
	} {
		t.Run(c.name, func(t *testing.T) {
			var b Burn
			b.Observe([]model.QuotaWindow{{ID: "w", Label: "5h", UsedPercent: model.PercentPtr(10)}}, "test-src", pinned)
			b.Observe([]model.QuotaWindow{{ID: "w", Label: "5h", UsedPercent: model.PercentPtr(11)}}, "test-src", c.at)
			s := b.Series[0].Samples
			if len(s) != 1 {
				t.Fatalf("a %s clock added a sample: %d retained", c.name, len(s))
			}
			if s[0].Used != 10 {
				t.Errorf("the refused sample overwrote the kept one: used=%v", s[0].Used)
			}
		})
	}
}

// The forecast is keyed to a window id, so two windows sampled from the same
// account never contaminate each other.
func TestForecastIsPerWindow(t *testing.T) {
	b := Burn{Series: []BurnSeries{
		{WindowID: "five_hour", Samples: series(30, 42, 18*time.Minute, 7, nil)},
		{WindowID: "seven_day", Samples: series(18, 18.1, 18*time.Minute, 7, nil)},
	}}
	if _, ok := b.Forecast("five_hour", pinned); !ok {
		t.Error("the fast window lost its forecast")
	}
	if _, ok := b.Forecast("seven_day", pinned); ok {
		t.Error("the slow window borrowed the fast one's rate")
	}
	if _, ok := b.Forecast("no_such_window", pinned); ok {
		t.Error("forecast produced for a window never sampled")
	}
}

// A source change resets the history: two sessions carry snapshots of the
// same account windows taken at different moments, and a series stitched
// across sources mixes measurement times (review finding, v1.1).
func TestBurnResetsWhenTheSampledSessionChanges(t *testing.T) {
	var b Burn
	for i := 0; i < 30; i++ {
		b.Observe([]model.QuotaWindow{{ID: "w", Label: "5h", UsedPercent: model.PercentPtr(float64(10 + i))}}, "codex/s1",
			pinned.Add(time.Duration(i)*sampleInterval))
	}
	if _, ok := b.Forecast("w", pinned.Add(30*sampleInterval)); !ok {
		t.Fatal("precondition: a healthy single-source series must forecast")
	}
	b.Observe([]model.QuotaWindow{{ID: "w", Label: "5h", UsedPercent: model.PercentPtr(41)}}, "codex/s2",
		pinned.Add(31*sampleInterval))
	if _, ok := b.Forecast("w", pinned.Add(31*sampleInterval)); ok {
		t.Fatal("a source switch must reset the basis; forecasting across it blends measurement times")
	}
	if len(b.Series) != 1 || len(b.Series[0].Samples) != 1 {
		t.Fatalf("history after source switch: %+v, want exactly the one new sample", b.Series)
	}
}
