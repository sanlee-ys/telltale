package councilhost

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// TestHostMemoryPerTurn is the measurement design.md §7.28 said must come
// before a turn ceiling, and it is a measurement, not a ceiling. It drives an
// in-process host through N turns with stubbed seats that each say a fixed
// number of bytes per turn, and reports three numbers the room has never had:
//
//   - the host's live heap after every turn, GC forced, so the retained cost
//     of one turn is a measured delta and not a guess from the struct;
//   - the size of the room frame the pump would write after that turn, in the
//     exact bytes FrameWriter puts on the wire (json.Marshal plus the newline),
//     so the frame cost §7.31 named is read off the real encoding;
//   - the turn at which that frame crosses maxFrame, the 8 MiB line ceiling,
//     or the fact that it never does because maxHistory stops it first.
//
// It is SKIPPED unless TELLTALE_MEASURE_HOST_MEMORY is set, because it runs
// hundreds of turns and sizes a heap, which is a minute of wall clock the
// ordinary suite should not pay. Run it by hand:
//
//	TELLTALE_MEASURE_HOST_MEMORY=1 go test ./internal/councilhost -run TestHostMemoryPerTurn -v
//
// The numbers it prints are the ones recorded in design.md §7.28's dated
// amendment and STATE.md. What it does not measure, said here so a reader does
// not take the table for more than it is: the seats are stubs, so the body
// bytes are a flat text pattern with a newline every 80 columns and two acts
// per turn, not a vendor's own output shape; the host runs inside the test
// binary, so the heap it reads is the test process's and not a `telltale
// council host` process's (the baseline is subtracted for that reason, the
// absolute figure is not reported); and the pipe is not on the path, because
// the frame's size is a property of the encoding and the client's read rate is
// not what this measures. A Linux run says nothing about Windows.
//
// Nothing here spawns a vendor: countSpawns stubs the three spawn vars, the
// roster's binaries do not resolve, and TestMain's guard would panic on either.
func TestHostMemoryPerTurn(t *testing.T) {
	if os.Getenv("TELLTALE_MEASURE_HOST_MEMORY") == "" {
		t.Skip("set TELLTALE_MEASURE_HOST_MEMORY=1 to run the host memory measurement")
	}
	turns := 150
	if s := os.Getenv("TELLTALE_MEASURE_TURNS"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			t.Fatalf("TELLTALE_MEASURE_TURNS=%q is not a turn count", s)
		}
		turns = n
	}
	// 2 and 32 KiB are the two sizes asked for. 48 KiB is there because at 32
	// a capped history stops short of maxFrame, and the question "what happens
	// when a room crosses it" deserves a run rather than an extrapolation.
	for _, perSeat := range []int{2 << 10, 32 << 10, 48 << 10} {
		t.Run(fmt.Sprintf("%dKiB-per-seat", perSeat>>10), func(t *testing.T) {
			m := measureHostMemory(t, perSeat, turns)
			t.Log(m.report())
		})
	}
}

// measuredRoster is every seat the host can drive. Four, not five: cursor is
// in the registry and the host refuses it (New marks it undrivable, because it
// speaks a protocol the host does not carry and has no batch fallback), so a
// hosted room has at most these four producing columns. claude is the one
// persistent seat and ends its turn on a KindMeta line; the other three fall
// back to their batch adapters and end a turn by dying (KindDone).
var measuredRoster = []RosterEntry{
	{Vendor: model.VendorClaude, Binary: "telltale-no-such-vendor-binary"},
	{Vendor: model.VendorCodex, Binary: "telltale-no-such-vendor-binary"},
	{Vendor: model.VendorAntigravity, Binary: "telltale-no-such-vendor-binary"},
	{Vendor: model.VendorGrok, Binary: "telltale-no-such-vendor-binary"},
}

// hostMemoryRun is one variant's result.
type hostMemoryRun struct {
	perSeat, seats, turns int
	// heap[i] is HeapAlloc after turn i+1, GC forced; baseline is the same
	// reading before turn 1 with the host built and the fold running.
	baseline uint64
	heap     []uint64
	// frame[i] is the wire length of the room frame after turn i+1, in bytes,
	// and 0 when FrameWriter refused it as over maxFrame.
	frame   []int
	tooLong int // first turn whose frame FrameWriter refused; 0 if none
	// pumpFramesAtTooLong is the pump's frame count when the first refused
	// frame was measured, so what the pump did afterwards is a measured
	// difference and not a reading of its code.
	pumpFramesAtTooLong int
	// pumpFrames and pumpBytes count what the real pump actually wrote over the
	// run, so the per-turn figure can be read beside the coalesced rate.
	pumpFrames, pumpBytes int
}

// countingWriter is the pump's sink: it counts frames and bytes and keeps
// nothing, so the measured heap does not include the pump's own output.
type countingWriter struct {
	mu     sync.Mutex
	frames int
	bytes  int
}

func (c *countingWriter) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.frames++
	c.bytes += len(p)
	c.mu.Unlock()
	return len(p), nil
}

// bodyText is what a stubbed seat says: n bytes of flat printable text with a
// newline every 80 columns. The newline is there because a reply has them and
// JSON escapes each one to two bytes, so a body of n bytes is more than n on
// the wire; a pattern with none would understate the frame.
func bodyText(n int) string {
	var b strings.Builder
	b.Grow(n)
	const line = "the quick brown fox jumps over the lazy dog, and the seat keeps talking "
	for b.Len() < n {
		for _, r := range line {
			if b.Len() >= n {
				break
			}
			if b.Len()%80 == 79 {
				b.WriteByte('\n')
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

func measureHostMemory(t *testing.T, perSeat, turns int) hostMemoryRun {
	t.Helper()
	countSpawns(t)
	stubRoomJob(t)
	h, err := New(Config{
		Workspace: t.TempDir(),
		PipeName:  "telltale-test-memory-" + t.Name(),
		Posture:   vendors.PostureRead,
		Roster:    measuredRoster,
		Tick:      5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h.roomCtx, h.roomCancel = context.WithCancel(ctx)
	go h.fold()

	// The real pump, into a sink that keeps nothing. It is what a client would
	// be reading, so its frame count is the coalesced rate and its exit on an
	// over-long frame is the real consequence of crossing maxFrame.
	sink := &countingWriter{}
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go h.pump(NewFrameWriter(sink), stop)

	run := hostMemoryRun{perSeat: perSeat, seats: len(measuredRoster), turns: turns}
	body := bodyText(perSeat)
	const chunk = 1 << 10
	run.baseline = liveHeap()

	for n := 1; n <= turns; n++ {
		h.dispatch("turn "+itoa(n)+": say the usual", nil)
		for _, e := range measuredRoster {
			for off := 0; off < len(body); off += chunk {
				end := off + chunk
				if end > len(body) {
					end = len(body)
				}
				h.events <- runner.Event{Vendor: e.Vendor, Kind: runner.KindText, Text: body[off:end]}
			}
			h.events <- runner.Event{Vendor: e.Vendor, Kind: runner.KindActivity, Acts: []runner.ActCall{
				{ID: "call-" + itoa(n) + "-1", Text: "Bash: go test ./...", Outcome: runner.ActOK},
				{ID: "call-" + itoa(n) + "-2", Text: "Read: internal/councilhost/room.go", Outcome: runner.ActOK},
			}}
			if e.Vendor == model.VendorClaude {
				cost := 0.01 * float64(n)
				h.events <- runner.Event{Vendor: e.Vendor, Kind: runner.KindMeta, EndsTurn: true,
					SessionID: "sess-" + string(e.Vendor), CostUSD: &cost}
			} else {
				h.events <- runner.Event{Vendor: e.Vendor, Kind: runner.KindSession, SessionID: "sess-" + string(e.Vendor)}
				h.events <- runner.Event{Vendor: e.Vendor, Kind: runner.KindDone}
			}
		}
		awaitRoom2(t, h, func(r Room) bool {
			for _, s := range r.Seats {
				if s.Turn != n || s.Phase != PhaseDone || len(s.Body) != len(body) {
					return false
				}
			}
			return true
		})

		// The frame as FrameWriter would put it on the wire, byte for byte:
		// the same encoder, the same ceiling check, the same newline.
		snap := h.Snapshot()
		var buf bytes.Buffer
		err := NewFrameWriter(&buf).Write(Frame{Kind: KindRoom, Room: &snap})
		switch {
		case err == nil:
			run.frame = append(run.frame, buf.Len())
		case err == ErrFrameTooLong:
			run.frame = append(run.frame, 0)
			if run.tooLong == 0 {
				run.tooLong = n
				time.Sleep(50 * time.Millisecond)
				sink.mu.Lock()
				run.pumpFramesAtTooLong = sink.frames
				sink.mu.Unlock()
			}
		default:
			t.Fatal(err)
		}
		// Measured with the frame's buffer out of scope, so the heap reading is
		// the room and the host, not the measurement.
		buf, snap = bytes.Buffer{}, Room{}
		run.heap = append(run.heap, liveHeap())
	}
	// Let the last tick drain before the pump's counters are read.
	time.Sleep(50 * time.Millisecond)
	sink.mu.Lock()
	run.pumpFrames, run.pumpBytes = sink.frames, sink.bytes
	sink.mu.Unlock()
	return run
}

// liveHeap is HeapAlloc after two forced collections. Two, because the first
// can leave objects that were only reachable from a stack frame the collector
// scanned before it was popped; the second reading is the one that settles.
func liveHeap() uint64 {
	runtime.GC()
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

// report prints the run as the table the docs quote. Every number is read off
// the slices; nothing is extrapolated except the two "would cross" lines,
// which say so and show their arithmetic.
func (r hostMemoryRun) report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n%d seats x %d KiB body per seat per turn, %d turns, maxHistory=%d, maxFrame=%d bytes\n",
		r.seats, r.perSeat>>10, r.turns, maxHistory, maxFrame)
	fmt.Fprintf(&b, "baseline live heap (host built, no turn): %s\n", kib(r.baseline))
	at := func(n int) {
		if n < 1 || n > r.turns {
			return
		}
		fr := "over maxFrame (FrameWriter refused it)"
		if r.frame[n-1] > 0 {
			fr = fmt.Sprintf("%s (%d bytes)", kib(uint64(r.frame[n-1])), r.frame[n-1])
		}
		fmt.Fprintf(&b, "  turn %4d: live heap %s (+%s over baseline), frame %s\n",
			n, kib(r.heap[n-1]), kibDelta(r.heap[n-1], r.baseline), fr)
	}
	if os.Getenv("TELLTALE_MEASURE_EVERY_TURN") != "" {
		for n := 1; n <= r.turns; n++ {
			at(n)
		}
	} else {
		for _, n := range []int{1, 2, 10, 25, 50, 51, 52, 60, 100, r.turns} {
			at(n)
		}
	}
	// Per-turn slopes over two windows: the fill (turns 2..maxHistory, while
	// History is growing) and the plateau (maxHistory+2..N, once it is capped).
	// Turn 1 is excluded from the fill because the first turn files no
	// history and so retains less than every later one.
	frameSlope := func(lo, hi int) string {
		for i := lo; i <= hi; i++ {
			if r.frame[i] == 0 {
				return "over maxFrame"
			}
		}
		return slopeKiB(uint64(r.frame[lo]), uint64(r.frame[hi]), hi-lo) + "/turn"
	}
	if r.turns >= maxHistory {
		fmt.Fprintf(&b, "fill, turns 2..%d:    heap %s/turn, frame %s\n", maxHistory,
			slopeKiB(r.heap[1], r.heap[maxHistory-1], maxHistory-2), frameSlope(1, maxHistory-1))
	}
	// The plateau is reported as a range rather than a slope, because it is
	// not flat. Its maximum also catches the pump mid-write — a clone and an
	// encoded frame are live for as long as one write takes — which is a real
	// cost of the host and is left in rather than paused around. The floor: startTurn trims History by reslicing, the dropped records stay
	// reachable in the slice's backing array until append reallocates it, and
	// the heap saws between the capped history and about twice it. Its
	// per-turn frame growth is reported because it should be near zero.
	if r.turns >= maxHistory+12 {
		lo, hi := maxHistory+1, r.turns-1
		minH, maxH := r.heap[lo], r.heap[lo]
		minF, maxF := r.frame[lo], r.frame[lo]
		for i := lo; i <= hi; i++ {
			minH, maxH = min(minH, r.heap[i]), max(maxH, r.heap[i])
			minF, maxF = min(minF, r.frame[i]), max(maxF, r.frame[i])
		}
		fmt.Fprintf(&b, "plateau, turns %d..%d: live heap %s..%s (%s..%s over baseline), frame %s..%s, frame %s\n",
			lo+1, hi+1, kib(minH), kib(maxH), kibDelta(minH, r.baseline), kibDelta(maxH, r.baseline),
			kib(uint64(minF)), kib(uint64(maxF)), frameSlope(lo, hi))
	}
	switch {
	case r.tooLong > 0:
		fmt.Fprintf(&b, "the frame crossed maxFrame at turn %d (last frame under it: %s at turn %d); "+
			"the pump had written %d frames by then and %d by the end of the run\n",
			r.tooLong, kib(uint64(r.frame[r.tooLong-2])), r.tooLong-1, r.pumpFramesAtTooLong, r.pumpFrames)
	case r.turns > maxHistory+1 && r.frame[r.turns-1] > 0:
		per := float64(r.frame[maxHistory]-r.frame[1]) / float64(maxHistory-1)
		fmt.Fprintf(&b, "the frame never crossed maxFrame: it stops growing at turn %d (%s). "+
			"At this variant's fill rate of %.0f bytes/turn, %d bytes would have been reached at turn %.0f "+
			"had maxHistory not capped it (arithmetic, not a measurement).\n",
			maxHistory+1, kib(uint64(r.frame[maxHistory])), per, maxFrame,
			float64(maxFrame-r.frame[1])/per+2)
	}
	fmt.Fprintf(&b, "pump wrote %d frames, %s in all, over %d turns (%.2f frames/turn at a %s tick)\n",
		r.pumpFrames, kib(uint64(r.pumpBytes)), r.turns, float64(r.pumpFrames)/float64(r.turns), "5ms")
	return b.String()
}

func kib(n uint64) string { return fmt.Sprintf("%.1f KiB", float64(n)/1024) }

func kibDelta(a, base uint64) string {
	if a >= base {
		return kib(a - base)
	}
	return "-" + kib(base-a)
}

func slopeKiB(from, to uint64, n int) string {
	if n <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%+.2f KiB", (float64(to)-float64(from))/float64(n)/1024)
}
