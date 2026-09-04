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

// TestTheTokenCountsParseAtTheMeasuredVersion pins the 2.1.233 shape §7.16b
// measured: the three count fields land, and `prompt_id` lands despite being
// absent from the vendor's statusline documentation page.
//
// The arithmetic assertion at the end is the load-bearing one. It does not
// test the vendor — it pins the RELATIONSHIP the measurement found, so a later
// edit cannot quietly turn this fixture into one where total_input_tokens is a
// cumulative session spend. The moment those numbers stop agreeing, the
// fixture has started claiming something nobody measured.
func TestTheTokenCountsParseAtTheMeasuredVersion(t *testing.T) {
	payload := `{
	  "session_id": "abc",
	  "prompt_id": "00000000-0000-4000-8000-00000000cafe",
	  "version": "2.1.233",
	  "model": {"id": "claude-opus-5", "display_name": "Opus"},
	  "context_window": {
	    "context_window_size": 200000,
	    "used_percentage": 8,
	    "total_input_tokens": 16000,
	    "total_output_tokens": 512,
	    "current_usage": {
	      "input_tokens": 1200,
	      "output_tokens": 512,
	      "cache_creation_input_tokens": 2800,
	      "cache_read_input_tokens": 12000
	    }
	  }
	}`

	in, err := Parse(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("Parse failed on the 2.1.233 shape: %v", err)
	}
	if in.PromptID != "00000000-0000-4000-8000-00000000cafe" {
		t.Errorf("prompt_id did not parse: %q", in.PromptID)
	}
	cw := in.ContextWindow
	if cw == nil || cw.CurrentUsage == nil {
		t.Fatal("context_window or current_usage is nil at the version that ships them")
	}
	if cw.TotalInputTokens == nil || *cw.TotalInputTokens != 16000 {
		t.Errorf("total_input_tokens = %v, want 16000", cw.TotalInputTokens)
	}
	if cw.TotalOutputTokens == nil || *cw.TotalOutputTokens != 512 {
		t.Errorf("total_output_tokens = %v, want 512", cw.TotalOutputTokens)
	}
	u := cw.CurrentUsage
	for name, got := range map[string]*int64{
		"input_tokens":                u.InputTokens,
		"output_tokens":               u.OutputTokens,
		"cache_creation_input_tokens": u.CacheCreationInputTokens,
		"cache_read_input_tokens":     u.CacheReadInputTokens,
	} {
		if got == nil {
			t.Errorf("current_usage.%s did not parse", name)
		}
	}
	if t.Failed() {
		return
	}
	// TAw's own arithmetic, at the version it was read from:
	// total_input_tokens = input + cache_creation + cache_read, all from ONE call.
	sum := *u.InputTokens + *u.CacheCreationInputTokens + *u.CacheReadInputTokens
	if sum != *cw.TotalInputTokens {
		t.Errorf("the fixture no longer models the measured arithmetic: "+
			"input+cache_creation+cache_read = %d but total_input_tokens = %d. "+
			"total_input_tokens is the LAST CALL's context occupancy (§7.16b), not a running spend",
			sum, *cw.TotalInputTokens)
	}
	if *u.OutputTokens != *cw.TotalOutputTokens {
		t.Errorf("total_output_tokens (%d) is the last call's output_tokens (%d) and nothing else",
			*cw.TotalOutputTokens, *u.OutputTokens)
	}
}

// TestAnOlderPayloadLeavesTheTokenCountsAbsent is the zero-vs-absent rule
// (§4a.1) applied to a schema that grew.
//
// A CLI older than 2.1.233 sends no such keys. They must read as nil — "this
// CLI does not report it" — and never as 0, which at 2.1.233 is a MEASURED
// value meaning "no messages yet". Both states exist in the wild at once,
// because the payload's own version field is the only thing that separates
// them, and a zero here would make the two indistinguishable forever.
func TestAnOlderPayloadLeavesTheTokenCountsAbsent(t *testing.T) {
	payload := `{
	  "session_id": "abc",
	  "version": "2.1.90",
	  "model": {"id": "claude-opus-5", "display_name": "Opus"},
	  "context_window": {"context_window_size": 200000, "used_percentage": 8}
	}`

	in, err := Parse(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("Parse failed on the pre-2.1.233 shape: %v", err)
	}
	cw := in.ContextWindow
	if cw == nil {
		t.Fatal("context_window did not parse")
	}
	if cw.TotalInputTokens != nil {
		t.Errorf("total_input_tokens = %d for a CLI that never sent the key; absent must not become zero", *cw.TotalInputTokens)
	}
	if cw.TotalOutputTokens != nil {
		t.Errorf("total_output_tokens = %d for a CLI that never sent the key; absent must not become zero", *cw.TotalOutputTokens)
	}
	if cw.CurrentUsage != nil {
		t.Error("current_usage is non-nil for a CLI that never sent it")
	}
	if in.PromptID != "" {
		t.Errorf("prompt_id = %q for a payload that carried none", in.PromptID)
	}
}

// TestThePromptCacheBlockParsesAtTheMeasuredVersion pins the 2.1.260 shape
// (§7.16c). The payload here is the one measured in the shipped executable on
// 2026-09-04, undocumented siblings included, because the allowlist claim in
// PromptCache's doc is only worth making against a payload that actually
// carries the fields it drops.
func TestThePromptCacheBlockParsesAtTheMeasuredVersion(t *testing.T) {
	payload := `{
	  "session_id": "abc",
	  "version": "2.1.260",
	  "model": {"id": "claude-opus-5", "display_name": "Opus"},
	  "prompt_cache": {
	    "warm": true,
	    "caching_observed": true,
	    "ttl": "1h",
	    "expires_at": 1754078400,
	    "requests": 14,
	    "misses": 2,
	    "expected_rebuilds": 1,
	    "hit_ratio": 0.91,
	    "cache_write_tokens": 352000,
	    "miss_recache_tokens": 310200,
	    "last_miss_at": 1754070000,
	    "last_miss_cause": {"causes": ["tools_changed"], "tools_added": 3},
	    "miss_causes": {"tools_changed": 2},
	    "recache_tokens_if_cold": 45000
	  }
	}`

	in, err := Parse(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("Parse failed on the 2.1.260 shape: %v", err)
	}
	pc := in.PromptCache
	if pc == nil {
		t.Fatal("prompt_cache is nil at the version that ships it")
	}
	if pc.HitRatio == nil || *pc.HitRatio != 0.91 {
		t.Errorf("hit_ratio = %v, want 0.91", pc.HitRatio)
	}
	if pc.CachingObserved == nil || !*pc.CachingObserved {
		t.Errorf("caching_observed = %v, want true", pc.CachingObserved)
	}
	if pc.Warm == nil || !*pc.Warm {
		t.Errorf("warm = %v, want true", pc.Warm)
	}
	if pc.Requests == nil || *pc.Requests != 14 {
		t.Errorf("requests = %v, want 14", pc.Requests)
	}
}

// TestAPayloadWithoutThePromptCacheBlockLeavesItAbsent covers the two ways the
// key does not arrive, and they are one nil for two different reasons: a CLI
// older than 2.1.251 never sends it, and a newer one withholds it until the
// main conversation's first API response, because the assembly function returns
// an empty object while `requests` is 0. Neither may become a zeroed struct —
// a PromptCache with a 0 HitRatio would render `cache 0%` and claim a cache
// reading nobody took (§4a.1).
func TestAPayloadWithoutThePromptCacheBlockLeavesItAbsent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
	}{
		{"pre-2.1.251 CLI", `{"session_id":"a","version":"2.1.233","model":{"id":"m"}}`},
		{"before the first API response", `{"session_id":"a","version":"2.1.260","model":{"id":"m"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Parse(strings.NewReader(tc.payload))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if in.PromptCache != nil {
				t.Errorf("prompt_cache = %+v for a payload that carried none; absent must not become zero", in.PromptCache)
			}
		})
	}
}

// TestCachingObservedFalseIsNotAMissingBlock is the sharpest case in this file.
//
// A provider or gateway that reports no cache tokens still gets a prompt_cache
// object: `caching_observed` is false, `hit_ratio` is null, and `requests`
// counts real requests. Three states meet here and all three must stay
// separable — no block at all, a block saying this source does not exist, and a
// block reporting a measured 0.0 ratio. Collapsing any pair is the
// zero-vs-absent regression this repo exists to prevent.
func TestCachingObservedFalseIsNotAMissingBlock(t *testing.T) {
	payload := `{
	  "session_id": "abc",
	  "version": "2.1.260",
	  "model": {"id": "claude-opus-5"},
	  "prompt_cache": {"warm": false, "caching_observed": false, "requests": 4,
	                   "hit_ratio": null, "expires_at": null, "last_miss_at": null}
	}`

	in, err := Parse(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	pc := in.PromptCache
	if pc == nil {
		t.Fatal("prompt_cache went nil for a provider that reports no cache tokens; " +
			"that is a different state from a payload with no block at all")
	}
	if pc.HitRatio != nil {
		t.Errorf("hit_ratio = %v for an explicit JSON null; null must not become 0", *pc.HitRatio)
	}
	if pc.CachingObserved == nil || *pc.CachingObserved {
		t.Errorf("caching_observed = %v, want a parsed false", pc.CachingObserved)
	}
	if pc.Requests == nil || *pc.Requests != 4 {
		t.Errorf("requests = %v, want 4 — the session made requests, they just reported no cache", pc.Requests)
	}
}
