package hud

import (
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// Burn-rate forecasting: telltale's own measurement of how fast an account
// quota window is being consumed, and nothing else (design.md §7.12).
//
// The whole point of this feature is what it REFUSES to do. Incumbent monitors
// project a burn line from a plan budget nobody publishes; that is the exact
// fabrication decisions/001 exists to forbid. telltale instead samples the
// vendor's own used_percentage over its own runtime and reports the slope it
// measured, marked derived, with the sampling window stated beside it. Below a
// minimum basis it renders NOTHING — silence is a true statement about a
// measurement we have not made yet, and a line drawn from two samples 900 ms
// apart is not.
//
// The ring buffer is HUD-layer state, deliberately not part of model.Session
// (§4a): it is a property of this process's observation history, not of the
// session, and putting it in the schema would let an adapter claim to have
// measured something it never watched.

const (
	// minBasisSamples and minBasisSpan are the floor below which no forecast
	// renders at all.
	//
	// Three samples because two are a line through any two points and cannot
	// disagree with themselves; the third is the first one that can. Five
	// minutes because Claude's five-hour window moves in visible steps rather
	// than continuously, and a span shorter than the gap between two steps
	// measures the step, not the rate.
	minBasisSamples = 3
	minBasisSpan    = 5 * time.Minute

	// sampleInterval is the minimum spacing between retained samples. The poll
	// runs at 1 Hz, but 1 Hz samples of a percentage that moves in steps are
	// 15 redundant copies of the same reading, and they would let the newest
	// step dominate a least-squares fit.
	sampleInterval = 15 * time.Second

	// sampleWindow is how far back the buffer remembers. A forecast quoting a
	// basis older than this is describing a session the user has moved on from.
	sampleWindow = 30 * time.Minute

	// maxSamples bounds the ring. sampleWindow/sampleInterval plus slack.
	maxSamples = 128

	// forecastHorizon is the longest projection that may be rendered as a wall
	// clock. "~04:12" carries no date, so past a day it is ambiguous rather
	// than informative.
	forecastHorizon = 24 * time.Hour

	// resetJump is how far ResetsAt must move forward to count as a window
	// rollover rather than vendor jitter. A rollover advances it by a whole
	// window; jitter does not.
	resetJump = time.Minute
)

// BurnSample is one observation of one quota window.
type BurnSample struct {
	At   time.Time
	Used float64
	// Resets is the window's reset time as reported at sampling time. A jump
	// forward means the window rolled over and the history before it describes
	// a window that no longer exists.
	Resets *time.Time
}

// BurnSeries is the retained history for one quota window.
type BurnSeries struct {
	// WindowID keys the series to model.QuotaWindow.ID.
	WindowID string
	Samples  []BurnSample
}

// Burn is the HUD's sampling history across every quota window it has seen.
// It is a value type so State stays copyable and Render stays pure over it.
type Burn struct {
	Series []BurnSeries
	// Source identifies the session whose snapshots feed the series. Two
	// sessions carry the same account windows sampled at different moments, so
	// a series stitched across sources mixes measurement times; when the most
	// recently active quota-bearing session changes, the history resets and
	// the basis rebuilds rather than blending (review finding).
	Source string
}

// Observe folds one scan's quota windows into the history.
//
// It is called once per completed scan with the SAME windows the header block
// renders (§7.1: account quota appears once, sourced from one session), so the
// forecast can never describe a window the header is not showing. source is
// that session's identity; a source change resets every series.
func (b *Burn) Observe(windows []model.QuotaWindow, source string, now time.Time) {
	if source != b.Source {
		b.Series = nil
		b.Source = source
	}
	for i := range windows {
		w := windows[i]
		if w.UsedPercent == nil {
			// Nothing to sample. Not a reset either: the vendor simply has no
			// figure this scan, and dropping the history for that would throw
			// away real measurements over a momentary gap.
			continue
		}
		b.observeWindow(w.ID, float64(*w.UsedPercent), w.ResetsAt, now)
	}
}

func (b *Burn) observeWindow(id string, used float64, resets *time.Time, now time.Time) {
	idx := -1
	for i := range b.Series {
		if b.Series[i].WindowID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		b.Series = append(b.Series, BurnSeries{WindowID: id})
		idx = len(b.Series) - 1
	}
	s := &b.Series[idx]

	if n := len(s.Samples); n > 0 {
		last := s.Samples[n-1]
		switch {
		case rolledOver(last, used, resets):
			// The window rolled over. Every sample before the rollover
			// describes a window that no longer exists, and fitting a line
			// across the discontinuity would report a negative burn or a wild
			// one. Start again from this observation.
			s.Samples = s.Samples[:0]
		case !now.After(last.At):
			// A clock that did not advance cannot order the series. Refuse the
			// sample rather than fitting a line over a zero or negative
			// interval. Checked BEFORE the throttle: a negative difference is
			// also "less than the sample interval", so behind the throttle
			// this branch would be unreachable and the rule would be enforced
			// by accident rather than on purpose.
			return
		case now.Sub(last.At) < sampleInterval:
			return
		}
	}

	s.Samples = append(s.Samples, BurnSample{At: now, Used: used, Resets: resets})

	// Evict by age first, then by count. Both bounds are needed: age keeps the
	// basis honest, count keeps memory bounded if the clock misbehaves.
	cutoff := now.Add(-sampleWindow)
	keep := 0
	for keep < len(s.Samples) && s.Samples[keep].At.Before(cutoff) {
		keep++
	}
	if keep > 0 {
		s.Samples = append(s.Samples[:0], s.Samples[keep:]...)
	}
	if n := len(s.Samples); n > maxSamples {
		s.Samples = append(s.Samples[:0], s.Samples[n-maxSamples:]...)
	}
}

// rolledOver reports whether this observation belongs to a different window
// than the previous one.
//
// Two independent signals, either of which is sufficient:
//
//   - usage dropped. A window's used_percentage is monotonic within the
//     window, so a drop is a rollover (or a vendor correction, which we treat
//     the same way — the prior history no longer describes the current state).
//   - the reported reset time jumped forward by more than resetJump. Jitter of
//     a few seconds is not a rollover; a whole window is.
func rolledOver(last BurnSample, used float64, resets *time.Time) bool {
	if used < last.Used {
		return true
	}
	if last.Resets != nil && resets != nil && resets.Sub(*last.Resets) > resetJump {
		return true
	}
	return false
}

// Forecast is a rendered-ready projection for one window.
type Forecast struct {
	// At is when the window is projected to reach 100% at the measured rate.
	At time.Time
	// Basis is the span the samples cover — the honest scope of the claim.
	Basis time.Duration
	// Samples is how many observations the fit used.
	Samples int
}

// Forecast projects one window's exhaustion, or reports that it will not.
//
// Four conditions must all hold, and every one of them exists to prevent a
// specific lie:
//
//  1. at least minBasisSamples observations spanning at least minBasisSpan —
//     below that we have not measured a rate, we have measured noise;
//  2. a positive slope — a flat or falling window is not burning, and "you
//     will never run out" is not a time;
//  3. exhaustion before the window resets, when the vendor told us when that
//     is — projecting past the reset describes a window that will not exist;
//  4. exhaustion within forecastHorizon, because the render is a wall clock
//     with no date on it.
//
// The slope is a least-squares fit rather than a first-to-last difference:
// vendor usage percentages move in steps, and a two-point slope over a stepped
// series is dominated by whichever endpoints happen to straddle a step. The
// projection is anchored to the LAST OBSERVED value rather than to the fitted
// line, so the forecast starts from the number actually on screen beside it.
func (b Burn) Forecast(windowID string, now time.Time) (Forecast, bool) {
	var s *BurnSeries
	for i := range b.Series {
		if b.Series[i].WindowID == windowID {
			s = &b.Series[i]
			break
		}
	}
	if s == nil || len(s.Samples) < minBasisSamples {
		return Forecast{}, false
	}
	first, last := s.Samples[0], s.Samples[len(s.Samples)-1]
	basis := last.At.Sub(first.At)
	if basis < minBasisSpan {
		return Forecast{}, false
	}

	slope, ok := leastSquaresSlope(s.Samples)
	if !ok || slope <= 0 {
		return Forecast{}, false
	}

	remaining := 100 - last.Used
	if remaining <= 0 {
		// Already at the top. There is no time to project and no gauge left to
		// forecast; the used figure beside it already says everything.
		return Forecast{}, false
	}
	eta := last.At.Add(time.Duration(remaining / slope * float64(time.Second)))
	if !eta.After(now) {
		// The projection has already elapsed without the window filling, which
		// means the measured rate stopped applying. Say nothing rather than
		// print a time in the past.
		return Forecast{}, false
	}
	if eta.Sub(now) > forecastHorizon {
		return Forecast{}, false
	}
	if last.Resets != nil && !eta.Before(*last.Resets) {
		return Forecast{}, false
	}
	return Forecast{At: eta, Basis: basis, Samples: len(s.Samples)}, true
}

// leastSquaresSlope fits used% against time and returns the slope in percent
// per second.
func leastSquaresSlope(samples []BurnSample) (float64, bool) {
	n := float64(len(samples))
	if n < 2 {
		return 0, false
	}
	origin := samples[0].At
	var sx, sy, sxy, sxx float64
	for _, s := range samples {
		x := s.At.Sub(origin).Seconds()
		y := s.Used
		sx += x
		sy += y
		sxy += x * y
		sxx += x * x
	}
	den := n*sxx - sx*sx
	if den == 0 {
		// Every sample landed at the same instant. No interval, no rate.
		return 0, false
	}
	return (n*sxy - sx*sy) / den, true
}
