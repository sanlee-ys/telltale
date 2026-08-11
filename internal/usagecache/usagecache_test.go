package usagecache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/cursorhook"
	"github.com/sanlee-ys/telltale/internal/model"
)

var pinned = time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

func ip(v int64) *int64 { return &v }

func turn(in, out, cr, cw int64) Delta {
	return Delta{Turns: 1, InputTokens: in, OutputTokens: out, CacheReadTokens: cr, CacheWriteTokens: ip(cw)}
}

// req is a grok-shaped delta: one api_request, reasoning present, cache_write
// absent because grok's export has no such type (§4a.1 on the wire).
func req(in, out, re, cr int64) Delta {
	return Delta{Requests: 1, InputTokens: in, OutputTokens: out, CacheReadTokens: cr, ReasoningTokens: ip(re)}
}

func TestTurnsAccumulateIntoOneTotal(t *testing.T) {
	dir := t.TempDir()
	if err := Add(dir, "cursor", turn(100, 10, 1000, 50), pinned); err != nil {
		t.Fatal(err)
	}
	if err := Add(dir, "cursor", turn(200, 20, 2000, 60), pinned.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}

	got := ReadAll(dir, pinned.Add(6*time.Minute))
	if len(got) != 1 {
		t.Fatalf("ReadAll = %v", got)
	}
	e := got[0]
	if e.Vendor != model.VendorCursor {
		t.Errorf("vendor = %q", e.Vendor)
	}
	if e.Turns != 2 {
		t.Errorf("turns = %d, want 2", e.Turns)
	}
	if e.InputTokens != 300 || e.OutputTokens != 30 || e.CacheReadTokens != 3000 ||
		e.CacheWriteTokens == nil || *e.CacheWriteTokens != 110 {
		t.Errorf("totals = %+v", e.Entry)
	}
	// Cursor reports no reasoning count; a zero here would be an invented one.
	if e.ReasoningTokens != nil {
		t.Errorf("a cursor total grew a reasoning count: %d", *e.ReasoningTokens)
	}
	// Since is the FIRST turn's instant and does not move; WrittenAt is the
	// last. The pair is what makes the sum readable at all.
	if !e.Since.Equal(pinned) {
		t.Errorf("since = %v, want the first turn's instant", e.Since)
	}
	if !e.WrittenAt.Equal(pinned.Add(5 * time.Minute)) {
		t.Errorf("written_at = %v, want the last turn's instant", e.WrittenAt)
	}
	if e.Span() != 5*time.Minute {
		t.Errorf("span = %v, want 5m", e.Span())
	}
}

// The load-bearing half of the accumulation rule: a window that the READER
// would have dropped must not be summed onto either. Without this a total
// could silently span a week-long gap and still call itself a total.
func TestAnExpiredWindowIsReplacedNotExtended(t *testing.T) {
	dir := t.TempDir()
	if err := Add(dir, "cursor", turn(100, 10, 1000, 50), pinned); err != nil {
		t.Fatal(err)
	}
	later := pinned.Add(maxAge + time.Hour)
	if err := Add(dir, "cursor", turn(7, 8, 9, 10), later); err != nil {
		t.Fatal(err)
	}

	got := ReadAll(dir, later)
	if len(got) != 1 {
		t.Fatalf("ReadAll = %v", got)
	}
	e := got[0]
	if e.Turns != 1 || e.InputTokens != 7 {
		t.Errorf("the day-old total was extended instead of replaced: %+v", e.Entry)
	}
	if !e.Since.Equal(later) {
		t.Errorf("since = %v, want a fresh window at %v", e.Since, later)
	}
}

func TestStaleAndSkewedEntriesAreDroppedAtRead(t *testing.T) {
	dir := t.TempDir()
	if err := Add(dir, "cursor", turn(1, 1, 1, 1), pinned); err != nil {
		t.Fatal(err)
	}
	if got := ReadAll(dir, pinned.Add(maxAge+time.Minute)); len(got) != 0 {
		t.Fatalf("a total older than maxAge rendered: %+v", got)
	}
	// A future timestamp beyond skew tolerance means a clock we cannot reason
	// about — mirror of quotacache's and the adapters' guard.
	if got := ReadAll(dir, pinned.Add(-futureSkew-time.Minute)); len(got) != 0 {
		t.Fatalf("a future-stamped total rendered: %+v", got)
	}
	if got := ReadAll(dir, pinned.Add(-time.Minute)); len(got) != 1 {
		t.Fatalf("jitter within futureSkew should be tolerated: %+v", got)
	}
}

// The cache is numbers and keys only — the room.json and quota-relay standard
// (design.md §7.15, §7.16). The serialized form is asserted field by field so
// a future Entry field that could carry a prompt, a reply, a path or an email
// address has to fight this test to ship.
func TestTheCacheCarriesKeysAndNumbersOnly(t *testing.T) {
	dir := t.TempDir()
	if err := Add(dir, "cursor", turn(1, 2, 3, 4), pinned); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "cursor.json"))
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	for k := range top {
		switch k {
		case "vendor", "since", "written_at", "turns",
			"input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens":
		default:
			t.Errorf("unexpected cache field %q", k)
		}
	}
	// The two grok-only keys may not leak into a cursor entry: their absence
	// is the claim that Cursor keeps neither count (§4a.1 in the serialized
	// form).
	for _, k := range []string{"reasoning_tokens", "requests"} {
		if _, leaked := top[k]; leaked {
			t.Errorf("cursor entry grew a %q key it has no measurement for", k)
		}
	}
}

// The grok-shaped entry is the same contract from the other side: requests as
// the window unit, reasoning present, and NO cache_write or turns key — grok's
// export has no cache-write type and this relay counts api requests, not
// turns. A zero in either place would be an invented measurement.
func TestTheGrokShapedEntryCarriesItsOwnKeysOnly(t *testing.T) {
	dir := t.TempDir()
	if err := Add(dir, "grok", req(20323, 56, 42, 2560), pinned); err != nil {
		t.Fatal(err)
	}
	if err := Add(dir, "grok", req(100, 4, 8, 16), pinned.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "grok.json"))
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	for k := range top {
		switch k {
		case "vendor", "since", "written_at", "requests",
			"input_tokens", "output_tokens", "cache_read_tokens", "reasoning_tokens":
		default:
			t.Errorf("unexpected grok cache field %q", k)
		}
	}

	got := ReadAll(dir, pinned.Add(2*time.Minute))
	if len(got) != 1 {
		t.Fatalf("ReadAll = %+v", got)
	}
	e := got[0]
	if e.Requests != 2 || e.Turns != 0 {
		t.Errorf("window = %d requests %d turns, want 2 requests only", e.Requests, e.Turns)
	}
	if e.InputTokens != 20423 || e.OutputTokens != 60 || e.CacheReadTokens != 2576 {
		t.Errorf("totals = %+v", e.Entry)
	}
	if e.ReasoningTokens == nil || *e.ReasoningTokens != 50 {
		t.Errorf("reasoning total = %v, want 50", e.ReasoningTokens)
	}
	if e.CacheWriteTokens != nil {
		t.Errorf("a grok total grew a cache-write count: %d", *e.CacheWriteTokens)
	}
}

// A delta that advances neither window unit is refused whole: its counts
// would join a total whose window could no longer say what it spans.
func TestADeltaWithNoWindowUnitIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := Add(dir, "grok", Delta{InputTokens: 5}, pinned); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a windowless delta was written: %v", entries)
	}
}

// End to end from the wire: a REAL payload shape, carrying the reply text and
// the user's email the way cursor-agent actually sends them, must leave
// nothing of either on disk. The parser's allowlist and the cache's schema are
// two separate defences and this asserts the pair of them together, because
// what ships is the pair.
func TestNothingFromTheWirePayloadReachesDisk(t *testing.T) {
	dir := t.TempDir()
	payload := `{
	  "conversation_id":"0f00dbaa-1234-4a77-9b02-000000000042",
	  "generation_id":"aaaaaaaa-bbbb-4ccc-8ddd-000000000001",
	  "model":"composer-2.5",
	  "text":"SECRET-REPLY-BODY",
	  "input_tokens":48012,"output_tokens":1203,
	  "cache_read_tokens":1904221,"cache_write_tokens":62004,
	  "workspace_roots":["C:\\src\\code\\SECRET-REPO"],
	  "transcript_path":"C:\\Users\\SECRET-USER\\.cursor\\chats\\x.jsonl",
	  "user_email":"SECRET-EMAIL@example.com"
	}`
	turn, err := cursorhook.Parse(strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	d, ok := FromCursorTurn(turn)
	if !ok {
		t.Fatal("a complete payload was refused")
	}
	if err := Add(dir, "cursor", d, pinned); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "cursor.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{
		"SECRET-REPLY-BODY", "SECRET-EMAIL", "SECRET-USER", "SECRET-REPO",
		"composer-2.5", "0f00dbaa", "aaaaaaaa", ".cursor", "conversation",
	} {
		if strings.Contains(string(raw), marker) {
			t.Errorf("marker %q reached the cache file:\n%s", marker, raw)
		}
	}
	// And the numbers that were supposed to arrive, did.
	if !strings.Contains(string(raw), "48012") || !strings.Contains(string(raw), "1904221") {
		t.Errorf("the counts did not reach the cache file:\n%s", raw)
	}
}

// A partial turn is refused rather than summed with the missing count treated
// as zero: a total drifting low is invisible, and a counter going quiet is not
// (design.md §7.16, §7.7).
func TestAnIncompleteTurnIsNotAccumulated(t *testing.T) {
	partial := cursorhook.Turn{InputTokens: ip(1), OutputTokens: ip(2), CacheReadTokens: ip(3)}
	if _, ok := FromCursorTurn(partial); ok {
		t.Error("a turn missing cache_write_tokens was accepted")
	}
	negative := cursorhook.Turn{
		InputTokens: ip(-1), OutputTokens: ip(2), CacheReadTokens: ip(3), CacheWriteTokens: ip(4),
	}
	if _, ok := FromCursorTurn(negative); ok {
		t.Error("a turn with a negative count was accepted")
	}
	// The whole turn is refused, not clamped: three trustworthy numbers plus
	// one invented one is not a reading.
	complete := cursorhook.Turn{
		InputTokens: ip(1), OutputTokens: ip(2), CacheReadTokens: ip(3), CacheWriteTokens: ip(4),
	}
	d, ok := FromCursorTurn(complete)
	if !ok || !reflect.DeepEqual(d, turn(1, 2, 3, 4)) {
		t.Errorf("FromCursorTurn = %+v, %v", d, ok)
	}
}

// A malformed or incoherent file is a non-event, not an error banner, and it
// must not take a readable sibling down with it (§7.7 shows less on failure).
func TestMalformedAndIncoherentEntriesAreNonEvents(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "junk.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A window that starts after it was last written to has been edited or has
	// had a clock move under it; it cannot say what its total is a total of.
	bad := Entry{
		Vendor: "gemini", Since: pinned.Add(time.Hour), WrittenAt: pinned,
		Turns: 3, InputTokens: 1,
	}
	raw, _ := json.Marshal(bad)
	if err := os.WriteFile(filepath.Join(dir, "gemini.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	// A total claiming zero turns is a broken reading rather than a small one.
	zero := Entry{Vendor: "codex", Since: pinned, WrittenAt: pinned, Turns: 0, InputTokens: 5}
	raw, _ = json.Marshal(zero)
	if err := os.WriteFile(filepath.Join(dir, "codex.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Add(dir, "cursor", turn(1, 1, 1, 1), pinned); err != nil {
		t.Fatal(err)
	}

	got := ReadAll(dir, pinned)
	if len(got) != 1 || got[0].Vendor != model.VendorCursor {
		t.Fatalf("a malformed sibling changed the result: %+v", got)
	}
}

// The write is atomic: a reader never sees half an entry. The mechanism
// (temp + rename in the same directory) is asserted by its observable
// consequence — no temp files left behind, and the target readable
// immediately after Add returns.
func TestAddLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	if err := Add(dir, "cursor", turn(1, 1, 1, 1), pinned); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
	if len(entries) != 1 || entries[0].Name() != "cursor.json" {
		t.Errorf("dir = %v", entries)
	}
}

func TestReadAllOrdersByVendor(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []string{"cursor", "codex"} {
		if err := Add(dir, v, turn(1, 1, 1, 1), pinned); err != nil {
			t.Fatal(err)
		}
	}
	got := ReadAll(dir, pinned)
	if len(got) != 2 || got[0].Vendor != model.VendorCodex || got[1].Vendor != model.VendorCursor {
		t.Fatalf("order = %+v", got)
	}
}

// A vendorless call writes nothing rather than creating a ".json" file, the
// same non-event quotacache's empty write is.
func TestAVendorlessAddIsANonEvent(t *testing.T) {
	dir := t.TempDir()
	if err := Add(dir, "", turn(1, 1, 1, 1), pinned); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a vendorless add left a file: %v", entries)
	}
}
