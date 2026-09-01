package councilhost

import (
	"bytes"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// TestAFrameSurvivesTheWire is the round trip, including the one field that
// carries what a person typed.
func TestAFrameSurvivesTheWire(t *testing.T) {
	var buf bytes.Buffer
	fw := NewFrameWriter(&buf)
	want := Frame{
		Kind:   KindRoom,
		Room:   &Room{Version: RoomVersion, Workspace: `C:\src`, Turn: 3, Posture: "read", Seats: []Seat{{Vendor: model.VendorClaude, Phase: PhaseStreaming, Body: "line one\nline two"}}},
		Prompt: "",
	}
	if err := fw.Write(want); err != nil {
		t.Fatal(err)
	}
	if bytes.Count(buf.Bytes(), []byte("\n")) != 1 {
		t.Fatalf("a frame carrying an embedded newline produced %d lines; the framing rule "+
			"is one frame per line and encoding/json escapes the newline inside the string",
			bytes.Count(buf.Bytes(), []byte("\n")))
	}
	got, err := NewFrameReader(&buf).Read()
	if err != nil {
		t.Fatal(err)
	}
	if got.Room == nil || got.Room.Seats[0].Body != "line one\nline two" {
		t.Fatalf("the body did not survive: %+v", got.Room)
	}
	if got.Room.Turn != 3 || got.Room.Workspace != `C:\src` {
		t.Fatalf("the room did not survive: %+v", got.Room)
	}
}

// TestAFinalLineWithNoNewlineIsStillALine is runner.pumpStdout's rule, applied
// to this wire.
//
// A peer that wrote its last frame and closed without a terminator has still
// said something, and dropping it would lose exactly the last thing said.
func TestAFinalLineWithNoNewlineIsStillALine(t *testing.T) {
	fr := NewFrameReader(strings.NewReader(`{"kind":"shutdown"}`))
	f, err := fr.Read()
	if err != nil {
		t.Fatalf("a terminator-less final frame was dropped: %v", err)
	}
	if f.Kind != KindShutdown {
		t.Fatalf("read %+v", f)
	}
	if _, err := fr.Read(); err != io.EOF {
		t.Fatalf("the reader did not end after the last line: %v", err)
	}
}

// TestAnOverLongFrameIsRefusedRatherThanTruncated.
//
// bufio.Scanner's 64K default was rejected one layer down (runner.maxLine) for
// silently losing the largest replies. The ceiling here is explicit and it
// REFUSES: a truncated frame is invalid JSON that would be reported as a
// protocol fault somewhere far from the cause.
func TestAnOverLongFrameIsRefusedRatherThanTruncated(t *testing.T) {
	long := `{"kind":"dispatch","prompt":"` + strings.Repeat("x", maxFrame+16) + `"}`
	if _, err := NewFrameReader(strings.NewReader(long)).Read(); err != ErrFrameTooLong {
		t.Fatalf("an over-long frame came back as %v", err)
	}
	var buf bytes.Buffer
	if err := NewFrameWriter(&buf).Write(Frame{Kind: KindDispatch, Prompt: strings.Repeat("y", maxFrame)}); err != ErrFrameTooLong {
		t.Fatalf("writing an over-long frame came back as %v", err)
	}
}

// TestConcurrentWritersCannotInterleaveAFrame pins the one failure this format
// cannot recover from.
//
// Two goroutines write to a host's client — the coalescing tick sends rooms
// while the handshake and refusal paths send their own. An interleaved write
// would split a frame across another, and every subsequent line would be
// garbage.
func TestConcurrentWritersCannotInterleaveAFrame(t *testing.T) {
	var buf syncBuffer
	fw := NewFrameWriter(&buf)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = fw.Write(Frame{Kind: KindDispatch, Prompt: strings.Repeat("abcdefgh", 512)})
		}(i)
	}
	wg.Wait()

	fr := NewFrameReader(bytes.NewReader(buf.Bytes()))
	for i := 0; i < 16; i++ {
		f, err := fr.Read()
		if err != nil {
			t.Fatalf("frame %d did not parse — a write was interleaved: %v", i, err)
		}
		if f.Kind != KindDispatch {
			t.Fatalf("frame %d read as %+v", i, f)
		}
	}
}

// syncBuffer is a sink that SPLITS every write into small chunks, with a
// scheduling point between them.
//
// The first version of this was a mutex-guarded bytes.Buffer, and it made the
// test above pass vacuously. FrameWriter hands its sink one whole frame in one
// Write call, so an atomic sink keeps frames intact whether or not FrameWriter
// holds its own lock — deleting the lock left the test green. It was measuring
// the scaffolding.
//
// The hazard the lock actually guards is a sink that can interleave, which is
// exactly what an *os.File over a pipe is: two goroutines writing at once can
// have their bytes land intermixed. So this reproduces that. Each Write is
// chopped into 64-byte pieces appended under the sink's own lock, with a
// runtime.Gosched between them, which gives a competing goroutine every chance
// to slot its bytes into the middle of a frame. With FrameWriter's lock the
// frames stay whole; without it they do not.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	const chunk = 64
	for off := 0; off < len(p); off += chunk {
		end := off + chunk
		if end > len(p) {
			end = len(p)
		}
		s.mu.Lock()
		s.buf.Write(p[off:end])
		s.mu.Unlock()
		runtime.Gosched()
	}
	return len(p), nil
}

func (s *syncBuffer) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.buf.Bytes()...)
}
