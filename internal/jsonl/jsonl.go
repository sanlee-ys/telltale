// Package jsonl implements the JSONL framing rule that docs/design.md §4 makes
// binding on every telltale adapter. Both vendors' on-disk sources are JSONL
// carrying model-authored text, so the rule exists in one tested place rather
// than being re-derived per adapter.
//
// Three properties, each of which is a real bug in some shipped JSONL reader:
//
//   - A record is framed by the 0x0A byte and NOTHING else. U+2028 (LINE
//     SEPARATOR) and U+2029 (PARAGRAPH SEPARATOR) are legal unescaped inside a
//     JSON string, so a reader that splits on "lines" in the Unicode sense
//     tears one record in two and loses both halves. Everything here matches
//     the byte; the UTF-8 encodings E2 80 A8 and E2 80 A9 contain no 0x0A.
//   - There is no token size cap. bufio.Scanner stops at 64 KiB by default and
//     then returns bufio.ErrTooLong; a survey of the live Claude corpus on
//     2026-08-01 found 107 of 13,211 records over that limit and one line of
//     1,004,230 bytes. An unchecked scanner error truncates the file silently,
//     which reads as "no more sessions". Scan uses bufio.Reader.ReadBytes and
//     returns every error it sees.
//   - A trailing partial line is not a record. The vendor may be mid-write.
//     Holding it back is what makes a torn tail render identically to a file
//     that never had those bytes, instead of producing a half-parsed row.
//
// Records handed to callers keep their terminating 0x0A where one was present;
// encoding/json tolerates trailing whitespace, and stripping it would cost a
// copy on every record.
package jsonl

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// readBufSize is the initial bufio.Reader buffer. It is a starting size, not a
// cap: ReadBytes grows past it as needed, which is the whole point.
const readBufSize = 64 << 10

// Split divides a fully-read buffer into complete records and the trailing
// partial remainder. partial is nil when the buffer ended on a 0x0A byte.
//
// The returned records alias buf; callers that retain them must copy.
func Split(buf []byte) (recs [][]byte, partial []byte) {
	for len(buf) > 0 {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			return recs, buf
		}
		recs = append(recs, buf[:i+1])
		buf = buf[i+1:]
	}
	return recs, nil
}

// Scan streams complete records from r, calling fn for each one in order. A
// trailing partial record is not delivered.
//
// fn may return an error to stop early; that error is returned unchanged.
// Every read error other than io.EOF is returned — the caller must not be able
// to mistake a truncated read for a short file.
func Scan(r io.Reader, fn func(rec []byte) error) error {
	br := bufio.NewReaderSize(r, readBufSize)
	for {
		line, err := br.ReadBytes('\n')
		switch {
		case errors.Is(err, io.EOF):
			// line has no terminator here: the vendor is still writing it, so
			// it is not yet a record. Dropping it is the rule, not a shortcut.
			return nil
		case err != nil:
			return err
		}
		if err := fn(line); err != nil {
			return err
		}
	}
}

// Tail returns the complete records inside the last n bytes of a file of the
// given size, newest last.
//
// When the read starts past the beginning of the file the first fragment is
// discarded: a backward seek lands mid-record, and parsing that fragment is
// how a bounded tail read invents a record the vendor never wrote. The
// trailing partial record is discarded for the same reason it is everywhere
// else in this package.
//
// A tail read is what makes a 1 s poll affordable — live Claude transcripts
// reach 7.7 MB and re-reading them whole every tick is not viable (design.md
// §3.1).
func Tail(ra io.ReaderAt, size, n int64) ([][]byte, error) {
	if size <= 0 || n <= 0 {
		return nil, nil
	}
	off := size - n
	seeked := off > 0
	if !seeked {
		off = 0
	}
	buf := make([]byte, size-off)
	read, err := ra.ReadAt(buf, off)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	recs, _ := Split(buf[:read])
	if seeked && len(recs) > 0 {
		recs = recs[1:]
	}
	return recs, nil
}

// Head returns the complete records inside the first n bytes of r.
//
// The final fragment is dropped even though it may be a complete record in the
// file: within this window its terminator was not observed, and the rule is
// that an unterminated fragment is not a record. A head read is for the
// session metadata vendors write first (ids, cwd, branch), where the first few
// records are all that matter.
func Head(r io.Reader, n int64) ([][]byte, error) {
	if n <= 0 {
		return nil, nil
	}
	buf, err := io.ReadAll(io.LimitReader(r, n))
	if err != nil {
		return nil, err
	}
	recs, _ := Split(buf)
	return recs, nil
}
