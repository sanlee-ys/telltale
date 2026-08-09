package cursorhook

import (
	"errors"
	"strings"
	"testing"
)

// realShape is the afterAgentResponse payload as cursor-agent 2026.08.04-aaa8809
// actually assembles it: the four token counts from `tokenUsage`, the fields
// ./src/after-agent-hooks.ts adds, and the ones the executor in 190.index.js
// enriches it with afterwards — including the reply TEXT and the user's EMAIL.
//
// It is one string, used by several tests, because the value of this fixture is
// that it is the whole payload rather than the convenient half of it.
const realShape = `{
  "conversation_id": "0f00dbaa-1234-4a77-9b02-000000000042",
  "generation_id": "aaaaaaaa-bbbb-4ccc-8ddd-000000000001",
  "model": "composer-2.5",
  "text": "SECRET-REPLY-BODY the model wrote this and it must never be stored",
  "input_tokens": 48012,
  "output_tokens": 1203,
  "cache_read_tokens": 1904221,
  "cache_write_tokens": 62004,
  "hook_event_name": "afterAgentResponse",
  "cursor_version": "2026.08.04-aaa8809",
  "workspace_roots": ["C:\\src\\code\\telltale"],
  "session_id": "0f00dbaa-1234-4a77-9b02-000000000042",
  "transcript_path": "C:\\Users\\SECRET-USER\\.cursor\\chats\\x.jsonl",
  "user_email": "SECRET-EMAIL@example.com"
}`

func TestParseTakesTheFourCountsUndrived(t *testing.T) {
	turn, err := Parse(strings.NewReader(realShape))
	if err != nil {
		t.Fatal(err)
	}
	if !turn.Complete() {
		t.Fatalf("a payload with all four counts read as incomplete: %+v", turn)
	}
	// The exact numbers matter: these are the vendor's own tokenUsage values,
	// NOT print mode's `usage.inputTokens`, which publishes
	// max(raw - cacheRead - cacheWrite, 0) and printed 24076 for this same
	// 48012 (measured 2026-08-08). If input_tokens ever starts reading 24076
	// here, the relay has been repointed at the derived source.
	if *turn.InputTokens != 48012 || *turn.OutputTokens != 1203 ||
		*turn.CacheReadTokens != 1904221 || *turn.CacheWriteTokens != 62004 {
		t.Errorf("counts = %d/%d/%d/%d", *turn.InputTokens, *turn.OutputTokens,
			*turn.CacheReadTokens, *turn.CacheWriteTokens)
	}
}

// The allowlist is the struct, and this is the assertion that says so. Markers
// are planted in every content-bearing and identity-bearing field of a REAL
// payload shape, and none of them may reach anything a Turn can hold — the
// same technique internal/adapter/cursor uses against a store that keeps OAuth
// tokens beside session state.
//
// This is the §7.15 keys-and-numbers standard applied one step earlier than
// the cache: the field that never gets parsed cannot be written by accident
// later, however the cache's schema grows.
func TestNoContentFieldCanReachATurn(t *testing.T) {
	turn, err := Parse(strings.NewReader(realShape))
	if err != nil {
		t.Fatal(err)
	}
	// A Turn is four integer pointers. There is no string in it at all, which
	// is the property being asserted — walked field by field rather than
	// eyeballed, so that adding a string field has to fight this test.
	for _, marker := range []string{
		"SECRET-REPLY-BODY", "SECRET-EMAIL", "SECRET-USER",
		"composer-2.5", "0f00dbaa", "telltale", "afterAgentResponse",
	} {
		if strings.Contains(dump(turn), marker) {
			t.Errorf("marker %q survived parsing into %s", marker, dump(turn))
		}
	}
}

// dump renders a Turn's entire observable content as text. If a future field
// carries a string, it shows up here and the marker test above fails.
func dump(t Turn) string {
	var b strings.Builder
	for _, v := range []*int64{t.InputTokens, t.OutputTokens, t.CacheReadTokens, t.CacheWriteTokens} {
		if v == nil {
			b.WriteString("nil ")
			continue
		}
		b.WriteString(itoa(*v) + " ")
	}
	return b.String()
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var d []byte
	for v > 0 {
		d = append([]byte{byte('0' + v%10)}, d...)
		v /= 10
	}
	if neg {
		return "-" + string(d)
	}
	return string(d)
}

// Absent is not zero (§4a.1). The vendor emits a token field only when
// tokenUsage carried it, so a missing count must stay missing all the way
// through — a Turn that read it as 0 would let a partial payload be summed as
// a complete one.
func TestAMissingCountStaysMissing(t *testing.T) {
	turn, err := Parse(strings.NewReader(`{"input_tokens":10,"output_tokens":2,"cache_read_tokens":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if turn.CacheWriteTokens != nil {
		t.Errorf("an absent count materialized as %d", *turn.CacheWriteTokens)
	}
	if turn.Complete() {
		t.Error("a payload missing cache_write_tokens read as complete")
	}
}

// A measured zero is a reading and must survive as one. This is the other half
// of the rule above: absent hides, zero counts.
func TestAMeasuredZeroIsAReading(t *testing.T) {
	turn, err := Parse(strings.NewReader(
		`{"input_tokens":0,"output_tokens":0,"cache_read_tokens":0,"cache_write_tokens":0}`))
	if err != nil {
		t.Fatal(err)
	}
	if !turn.Complete() {
		t.Fatal("four measured zeros read as incomplete")
	}
	if *turn.InputTokens != 0 {
		t.Errorf("input = %d", *turn.InputTokens)
	}
}

// A payload with no counts at all is not a failure — the hook fired and the
// vendor had nothing to report — but it is not a turn to count either, and the
// caller has to be able to tell it apart from a broken payload.
func TestAPayloadWithNoCountsIsErrEmptyNotAParseFailure(t *testing.T) {
	_, err := Parse(strings.NewReader(`{"conversation_id":"x","text":"hello"}`))
	if !errors.Is(err, ErrEmpty) {
		t.Fatalf("err = %v, want ErrEmpty", err)
	}
	if _, err := Parse(strings.NewReader(`{not json`)); err == nil || errors.Is(err, ErrEmpty) {
		t.Fatalf("broken JSON must be a parse error, got %v", err)
	}
}

func TestNegativeCountsAreRejected(t *testing.T) {
	turn, err := Parse(strings.NewReader(
		`{"input_tokens":-1,"output_tokens":2,"cache_read_tokens":3,"cache_write_tokens":4}`))
	if err != nil {
		t.Fatal(err)
	}
	if turn.Nonnegative() {
		t.Error("a negative token count passed Nonnegative")
	}
}
