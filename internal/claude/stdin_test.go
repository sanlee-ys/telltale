package claude

import (
	"bytes"
	"strings"
	"testing"
)

// U+2028 (LINE SEPARATOR) and U+2029 (PARAGRAPH SEPARATOR) are legal
// *unescaped* inside a JSON string value, and Claude Code's payloads and
// transcripts carry model-authored text that can contain them. Readers that
// split records with a Unicode-aware "line" splitter (Node's readline is the
// canonical offender) tear one JSON record into two, and both halves then fail
// to parse. Reference implementation of the correct rule — split on \n only:
// pi, packages/coding-agent/src/modes/rpc/jsonl.ts.
//
// Go is structurally safer here (see docs/design.md §4): bufio.ScanLines,
// bufio.Reader.ReadBytes('\n') and strings/bytes.Split all match the 0x0A byte
// exactly, and the UTF-8 encodings of U+2028 (E2 80 A8) and U+2029 (E2 80 A9)
// contain no 0x0A byte. These tests pin that property rather than assume it, so
// the HUD's JSONL adapters inherit a checked rule instead of a re-audit.
const (
	lineSep = "\u2028"
	paraSep = "\u2029"
)

// TestParseKeepsUnicodeSeparatorsIntact is the regression test for the parse
// site: one statusline record whose string values contain U+2028/U+2029 must
// decode as ONE record with the text preserved byte-for-byte.
func TestParseKeepsUnicodeSeparatorsIntact(t *testing.T) {
	name := "refactor" + lineSep + "the parser" + paraSep + "and ship"
	payload := `{
	  "cwd": "C:\\Users\\dev\\code\\telltale",
	  "session_id": "abc",
	  "session_name": "` + name + `",
	  "transcript_path": "t.jsonl",
	  "version": "1.0.0",
	  "model": {"id": "claude-sonnet-4-5", "display_name": "Sonnet 4.5"}
	}`

	in, err := Parse(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("Parse returned an error on a payload containing U+2028/U+2029: %v", err)
	}
	if in.SessionName != name {
		t.Errorf("session_name mangled across Unicode separators:\n got %q\nwant %q", in.SessionName, name)
	}
	if in.Model.DisplayName != "Sonnet 4.5" {
		t.Errorf("record torn: display_name = %q, want %q", in.Model.DisplayName, "Sonnet 4.5")
	}
}

// TestJSONLFramingIsByteLevel pins the framing rule the HUD's vendor adapters
// must follow: a JSONL record carrying U+2028/U+2029 is ONE record, because
// framing is the 0x0A byte and nothing else.
func TestJSONLFramingIsByteLevel(t *testing.T) {
	record := `{"session_id":"abc","version":"1.0.0","session_name":"a` +
		lineSep + `b` + paraSep + `c","model":{"id":"m","display_name":"M"}}`
	stream := []byte(record + "\n")

	// Framing: exactly one record plus the empty tail after the final \n.
	lines := bytes.Split(stream, []byte{'\n'})
	if len(lines) != 2 || len(lines[1]) != 0 {
		t.Fatalf("byte-level framing produced %d pieces, want 1 record + empty tail", len(lines))
	}

	in, err := Parse(bytes.NewReader(lines[0]))
	if err != nil {
		t.Fatalf("framed record failed to parse: %v", err)
	}
	if want := "a" + lineSep + "b" + paraSep + "c"; in.SessionName != want {
		t.Errorf("session_name = %q, want %q", in.SessionName, want)
	}

	// Guard the premise: neither separator's UTF-8 encoding contains 0x0A, which
	// is why byte-level splitting cannot tear these records.
	for _, sep := range []string{lineSep, paraSep} {
		if bytes.IndexByte([]byte(sep), '\n') != -1 {
			t.Errorf("premise violated: UTF-8 of %q contains a 0x0A byte", sep)
		}
	}
}
