package jsonl

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// lineSep / paraSep are the two characters that tear a record apart when a
// reader splits on Unicode line boundaries instead of the 0x0A byte.
const (
	lineSep = " "
	paraSep = " "
)

func TestSplitFramesOnByteOnly(t *testing.T) {
	// One record whose JSON string value contains raw U+2028 and U+2029.
	rec := `{"text":"before` + lineSep + `after` + paraSep + `end"}`
	buf := []byte(rec + "\n")

	recs, partial := Split(buf)
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1 — the separators tore the record", len(recs))
	}
	if partial != nil {
		t.Fatalf("got trailing partial %q, want none", partial)
	}
	var v struct{ Text string }
	if err := json.Unmarshal(recs[0], &v); err != nil {
		t.Fatalf("record does not parse: %v", err)
	}
	if !strings.Contains(v.Text, lineSep) || !strings.Contains(v.Text, paraSep) {
		t.Fatalf("separators did not survive the round trip: %q", v.Text)
	}
}

func TestSeparatorBytesContainNoNewline(t *testing.T) {
	// The structural reason Go is safe here, pinned so nobody "optimizes" a
	// splitter into something Unicode-aware.
	for _, s := range []string{lineSep, paraSep} {
		if bytes.IndexByte([]byte(s), '\n') >= 0 {
			t.Fatalf("%q contains a 0x0A byte", s)
		}
	}
	if got := []byte(lineSep); !bytes.Equal(got, []byte{0xE2, 0x80, 0xA8}) {
		t.Fatalf("U+2028 encodes as % x, want e2 80 a8", got)
	}
	if got := []byte(paraSep); !bytes.Equal(got, []byte{0xE2, 0x80, 0xA9}) {
		t.Fatalf("U+2029 encodes as % x, want e2 80 a9", got)
	}
}

func TestSplitHoldsBackTrailingPartial(t *testing.T) {
	buf := []byte("{\"a\":1}\n{\"b\":2}\n{\"c\":3 — still writ")
	recs, partial := Split(buf)
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if string(partial) != `{"c":3 — still writ` {
		t.Fatalf("partial = %q", partial)
	}
}

func TestScanSurvivesRecordsOverThe64KiBScannerCap(t *testing.T) {
	// bufio.Scanner's default cap is 64 KiB and the live corpus has records at
	// ~1 MB. This is the exact case that silently truncates a transcript.
	big := strings.Repeat("x", 300<<10)
	src := fmt.Sprintf("{\"i\":0}\n{\"big\":%q}\n{\"i\":2}\n", big)

	var got []int
	var bigLen int
	err := Scan(strings.NewReader(src), func(rec []byte) error {
		var v struct {
			I   int    `json:"i"`
			Big string `json:"big"`
		}
		if err := json.Unmarshal(rec, &v); err != nil {
			return err
		}
		if v.Big != "" {
			bigLen = len(v.Big)
		}
		got = append(got, v.I)
		return nil
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("read %d records, want 3 — a size cap swallowed the tail", len(got))
	}
	if bigLen != 300<<10 {
		t.Fatalf("oversized record truncated to %d bytes", bigLen)
	}
}

func TestScanDropsTrailingPartial(t *testing.T) {
	var n int
	err := Scan(strings.NewReader("{\"a\":1}\n{\"b\":2 — torn"), func([]byte) error {
		n++
		return nil
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if n != 1 {
		t.Fatalf("delivered %d records, want 1 (the torn tail is not a record)", n)
	}
}

func TestScanReturnsReadErrors(t *testing.T) {
	want := errors.New("disk fell over")
	err := Scan(io.MultiReader(strings.NewReader("{\"a\":1}\n"), errReader{want}), func([]byte) error {
		return nil
	})
	if !errors.Is(err, want) {
		t.Fatalf("Scan swallowed the read error: %v", err)
	}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func TestTailDiscardsTheFragmentASeekLandsIn(t *testing.T) {
	full := "{\"i\":0}\n{\"i\":1}\n{\"i\":2}\n{\"i\":3}\n"
	ra := bytes.NewReader([]byte(full))

	// A window that starts inside record 1.
	recs, err := Tail(ra, int64(len(full)), 20)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	for _, r := range recs {
		if !json.Valid(bytes.TrimSpace(r)) {
			t.Fatalf("Tail returned a fragment that is not valid JSON: %q", r)
		}
	}
	if len(recs) == 0 {
		t.Fatal("Tail returned no records")
	}
	var last struct{ I int }
	if err := json.Unmarshal(recs[len(recs)-1], &last); err != nil {
		t.Fatalf("last record: %v", err)
	}
	if last.I != 3 {
		t.Fatalf("last record i=%d, want 3", last.I)
	}
}

func TestTailReadsWholeFileWhenWindowExceedsIt(t *testing.T) {
	full := "{\"i\":0}\n{\"i\":1}\n"
	ra := bytes.NewReader([]byte(full))
	recs, err := Tail(ra, int64(len(full)), 4096)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2 — no fragment should be dropped when the window covers the file", len(recs))
	}
}

func TestTailDropsTornTail(t *testing.T) {
	full := "{\"i\":0}\n{\"i\":1}\n{\"i\":2 — still writ"
	ra := bytes.NewReader([]byte(full))
	recs, err := Tail(ra, int64(len(full)), 4096)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
}

func TestHeadDropsTheUnterminatedFinalFragment(t *testing.T) {
	src := "{\"i\":0}\n{\"i\":1}\n{\"i\":2}\n"
	recs, err := Head(strings.NewReader(src), 20)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2 (the third is cut by the window)", len(recs))
	}
}
