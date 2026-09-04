package council

// A scratch emitter for the room-identity density pass. It is gated on an
// environment variable, so the suite never runs it. It plays a real --record
// file through the same Update and Render the goldens use, and it writes one
// text frame and one dark SVG at every dispatch, gate, and turn end, at each
// geometry named in TELLTALE_FRAMES_GEOMETRY (COLSxROWS, comma separated).
//
//   TELLTALE_FRAMES_FROM=<record.jsonl> TELLTALE_FRAMES_OUT=<dir> \
//   TELLTALE_FRAMES_GEOMETRY=181x71,180x50 go test ./internal/council -run TestEmitFramesFromRecording

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/svgframe"
)

func TestEmitFramesFromRecording(t *testing.T) {
	from := os.Getenv("TELLTALE_FRAMES_FROM")
	out := os.Getenv("TELLTALE_FRAMES_OUT")
	if from == "" || out == "" {
		t.Skip("set TELLTALE_FRAMES_FROM and TELLTALE_FRAMES_OUT to emit frames")
	}
	geoms := os.Getenv("TELLTALE_FRAMES_GEOMETRY")
	if geoms == "" {
		geoms = "180x50"
	}
	rec, err := readRecording(from)
	if err != nil {
		t.Fatal(err)
	}

	// The moments: the record index after which a frame is taken, and a name.
	type moment struct {
		after int
		name  string
	}
	var moments []moment
	turn := 0
	for i, l := range rec.lines {
		switch l.Kind {
		case "dispatch":
			turn = l.Turn
			moments = append(moments, moment{i, fmt.Sprintf("t%02d-dispatch", turn)})
		case "gate":
			moments = append(moments, moment{i, fmt.Sprintf("t%02d-gate-%s", turn, l.Vendor)})
		}
		if l.EndsTurn {
			moments = append(moments, moment{i, fmt.Sprintf("t%02d-end-%s", turn, l.Vendor)})
		}
	}
	// A mid-turn frame between each dispatch and the next, so a streaming
	// column is seen streaming.
	for k := 0; k+1 < len(moments); k++ {
		if strings.HasSuffix(moments[k].name, "-dispatch") {
			mid := (moments[k].after + moments[k+1].after) / 2
			if mid > moments[k].after {
				moments = append(moments, moment{mid, moments[k].name[:3] + "-mid"})
			}
		}
	}
	moments = append(moments, moment{len(rec.lines) - 1, "zz-final"})
	// Order by record index.
	for a := range moments {
		for b := a + 1; b < len(moments); b++ {
			if moments[b].after < moments[a].after {
				moments[a], moments[b] = moments[b], moments[a]
			}
		}
	}

	for _, g := range strings.Split(geoms, ",") {
		wh := strings.SplitN(strings.TrimSpace(g), "x", 2)
		if len(wh) != 2 {
			t.Fatalf("geometry %q is not COLSxROWS", g)
		}
		w, _ := strconv.Atoi(wh[0])
		h, _ := strconv.Atoi(wh[1])
		dir := filepath.Join(out, fmt.Sprintf("%dx%d", w, h))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		m := newReplayModel(Options{}, rec, from)
		m.st.Width, m.st.Height = w, h
		// Stale exits: a persistent seat's previous process ends within a
		// second of the dispatch that replaced it, and the recording carries
		// only the vendor name, so a replay marks the NEW turn failed. The
		// live room did not show that. Skip them here; the product fix is a
		// separate change.
		// The rule: a `done` from a seat that has not said one word on this
		// turn is the previous process ending, not this turn's end. In the
		// recording those land 0.1s to 11s after the dispatch.
		stale := map[int]bool{}
		spoke := map[string]bool{}
		for i := range rec.lines {
			l := &rec.lines[i]
			switch {
			case l.Kind == "dispatch":
				spoke = map[string]bool{}
			case l.Kind == "event" && l.Event == "text":
				spoke[l.Vendor] = true
			case l.Kind == "event" && l.Event == "done" && !spoke[l.Vendor]:
				stale[i] = true
			}
		}
		played := 0
		for n, mo := range moments {
			for played <= mo.after {
				if stale[played] {
					// applyReplay drops any index but the next one, so the
					// skipped record has to be consumed, not bypassed.
					m.replay.i++
				} else {
					m.Update(replayMsg{played})
				}
				played++
			}
			plain := Render(m.st, PlainStyles(), GlyphsFor(false))
			base := filepath.Join(dir, fmt.Sprintf("%02d-%s", n, mo.name))
			if err := os.WriteFile(base+".txt", []byte(plain), 0o644); err != nil {
				t.Fatal(err)
			}
			styled := Render(m.st, NewStyles(true), GlyphsFor(false))
			svg, err := svgframe.Render(svgframe.Frame{
				Caption: "telltale council --replay",
				Alt:     mo.name,
				Lines:   strings.Split(styled, "\n"),
			}, svgframe.Dark())
			if err != nil {
				t.Fatalf("%s: %v", mo.name, err)
			}
			if err := os.WriteFile(base+".svg", svg, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("%s: %d frames", dir, len(moments))
	}
}
