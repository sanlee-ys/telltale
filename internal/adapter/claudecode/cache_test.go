package claudecode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The parse cache exists to keep a 1 s poll ahead of a thousand transcripts
// (see the parsed type). Every test in this file asks the same question in a
// different way: CAN A CACHE HIT SHOW SOMETHING A FRESH READ WOULD NOT? A
// speedup that can answer yes is a lie with a speedup attached, which is the
// one thing this product exists to refuse.

// cacheTree writes one transcript and returns an adapter over it, plus the
// transcript's path so a test can move it under the adapter's feet.
func cacheTree(t *testing.T, body string) (*Adapter, string) {
	t.Helper()
	root := t.TempDir()
	proj := filepath.Join(root, "C--Users-dev-code-cache")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(proj, healthyID+".jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewWithRoot(root), path
}

// read goes through Discover so the cache's prune step runs on every call, the
// way the HUD's scan runs it.
func read(t *testing.T, a *Adapter) (*model.Session, error) {
	t.Helper()
	refs, err := a.Discover(context.Background())
	if err != nil {
		return nil, err
	}
	return a.Read(context.Background(), refByID(t, refs, healthyID))
}

func mustRead(t *testing.T, a *Adapter) *model.Session {
	t.Helper()
	s, err := read(t, a)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return s
}

// comparable strips the two fields that are SUPPOSED to move between two reads
// of an unchanged file — the observation clock, and last_activity's dependence
// on it — so everything else can be compared for exact equality.
func comparable(s *model.Session) model.Session {
	c := *s
	c.ObservedAt = time.Time{}
	return c
}

// The headline property. Two reads of a file that did not move must produce the
// same session, field for field — not "close enough", and not just the four
// columns the HUD happens to draw today.
func TestACacheHitProducesTheSameSessionAsAFreshRead(t *testing.T) {
	body := `{"type":"user","cwd":"C:\\x\\cache","gitBranch":"main","version":"2.1.219","sessionId":"s","timestamp":"2026-08-01T10:00:00Z"}
{"type":"assistant","sessionId":"s","timestamp":"2026-08-01T10:01:00Z","message":{"model":"claude-opus-4","usage":{"input_tokens":10,"cache_read_input_tokens":200}}}
`
	a, _ := cacheTree(t, body)

	cold := mustRead(t, a) // parses
	warm := mustRead(t, a) // must hit the cache

	if !reflect.DeepEqual(comparable(cold), comparable(warm)) {
		t.Fatalf("cache hit diverged from a fresh read\n cold: %s\n warm: %s", describe(cold), describe(warm))
	}
	// Guard the guard: if the cache silently stopped being used this test would
	// still pass, and would be testing nothing.
	if _, hit := a.cachedParse(a.locatorFor(t), statOf(t, a.locatorFor(t))); !hit {
		t.Fatal("nothing was cached, so this test proved nothing about the cache")
	}
}

func (a *Adapter) locatorFor(t *testing.T) string {
	t.Helper()
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return refByID(t, refs, healthyID).Locator
}

func statOf(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// A file that MOVED must be re-read. Both halves of the key are exercised
// because a cache keyed on one of them is a cache that serves stale content for
// the other kind of edit.
func TestAChangedTranscriptIsReparsed(t *testing.T) {
	base := `{"type":"assistant","sessionId":"s","message":{"model":"claude-opus-4"}}` + "\n"
	a, path := cacheTree(t, base)

	if got, _ := mustRead(t, a).Model.Name(); got != "Opus 4" && got == "" {
		t.Fatalf("setup: no model read, got %q", got)
	}
	first, _ := mustRead(t, a).Model.Name()

	// Size AND mtime move: the ordinary append.
	if err := os.WriteFile(path, []byte(base+`{"type":"assistant","sessionId":"s","message":{"model":"claude-sonnet-4"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, _ := mustRead(t, a).Model.Name()
	if second == first {
		t.Fatalf("an appended record did not reach the row: still %q", first)
	}

	// mtime alone moves, size held constant: an in-place rewrite of the same
	// length. A cache keyed on size only would serve the old model forever.
	same := len(base + `{"type":"assistant","sessionId":"s","message":{"model":"claude-sonnet-4"}}` + "\n")
	rewrite := `{"type":"assistant","sessionId":"s","message":{"model":"claude-haiku-4"}}` + "\n"
	for len(rewrite) < same {
		rewrite += "\n"
	}
	if err := os.WriteFile(path, []byte(rewrite[:same]), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	third, _ := mustRead(t, a).Model.Name()
	if third == second {
		t.Fatalf("a same-length rewrite did not reach the row: still %q", second)
	}
}

// The subagent count is a function of the CLOCK as much as of the disk: a
// transcript written 40 minutes ago crosses subagentHorizon with no file
// changing at all, and the parent transcript's stat never moves. If the count
// rode the cache, a fan-out chip would sit on a row forever.
func TestTheSubagentCountIsNotFrozenByTheCache(t *testing.T) {
	a := fanoutTree(t, time.Minute)
	first := readOne(t, a, healthyID)
	if first.Subagents == nil {
		t.Fatal("setup: no subagent count")
	}
	before := *first.Subagents

	// Add a sub-agent transcript. The PARENT transcript is untouched, so the
	// parse cache hits — and the count must still move.
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	side := filepath.Join(strings.TrimSuffix(refByID(t, refs, healthyID).Locator, ".jsonl"), subagentDir)
	if err := os.WriteFile(filepath.Join(side, "agent-c0000000000000009.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	second := readOne(t, a, healthyID)
	if second.Subagents == nil || *second.Subagents != before+1 {
		t.Fatalf("cache froze the fan-out count: was %d, now %v", before, second.Subagents)
	}
}

// A cache that swallows a diagnostic is worse than the slow scan it replaced:
// the row renders an em dash with nothing explaining it, which is exactly the
// confidently-wrong display the honest-gauge rule forbids.
func TestDiagnosticsSurviveACacheHit(t *testing.T) {
	a, _ := cacheTree(t, `{"type":"user","cwd":"C:\\x\\cache","sessionId":"s"}`+"\n"+`{"type":"assistant",`+"\n")

	cold := mustRead(t, a)
	warm := mustRead(t, a)
	if len(cold.Diagnostics) == 0 {
		t.Fatal("setup: the torn record produced no diagnostic")
	}
	if !reflect.DeepEqual(cold.Diagnostics, warm.Diagnostics) {
		t.Fatalf("diagnostics changed across a cache hit:\n cold: %v\n warm: %v", cold.Diagnostics, warm.Diagnostics)
	}
	if cold.Degraded != warm.Degraded {
		t.Fatalf("degraded set changed across a cache hit: %s vs %s", cold.Degraded, warm.Degraded)
	}
}

// Absent must stay absent. A session whose records name no model has no model,
// and no amount of caching may promote an empty string into a rendered value.
func TestAbsenceSurvivesACacheHit(t *testing.T) {
	a, _ := cacheTree(t, `{"type":"user","sessionId":"s"}`+"\n")

	for i, s := range []*model.Session{mustRead(t, a), mustRead(t, a)} {
		if s.Model != nil {
			t.Fatalf("read %d invented a model: %+v", i, s.Model)
		}
		if s.Name != nil {
			t.Fatalf("read %d invented a name: %q", i, *s.Name)
		}
		if s.WorkspaceDir != nil {
			t.Fatalf("read %d invented a workspace: %q", i, *s.WorkspaceDir)
		}
	}
}

// A warm cache must not outlive its source. The stat happens first for exactly
// this reason: a deleted transcript is ErrSessionGone, never a replay.
func TestADeletedTranscriptIsGoneEvenWithAWarmCache(t *testing.T) {
	a, path := cacheTree(t, `{"type":"assistant","sessionId":"s","message":{"model":"claude-opus-4"}}`+"\n")
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ref := refByID(t, refs, healthyID)
	if _, err := a.Read(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	// The same ref the HUD would still be holding from the previous tick.
	if _, err := a.Read(context.Background(), ref); !errors.Is(err, model.ErrSessionGone) {
		t.Fatalf("a warm cache resurrected a deleted session: err=%v", err)
	}
}

// Extras is a slice on a struct the cache keeps across ticks. Handing the same
// backing array to every reader is how one row's append rewrites another's.
func TestExtrasAreNotSharedBetweenReads(t *testing.T) {
	a, _ := cacheTree(t, `{"type":"user","sessionId":"s","gitBranch":"main","version":"2.1.219"}`+"\n")
	first := mustRead(t, a)
	if len(first.Extras) == 0 {
		t.Fatal("setup: no extras")
	}
	first.Extras[0].Value = "mutated-by-a-consumer"
	if got := extra(mustRead(t, a), "branch"); got == "mutated-by-a-consumer" {
		t.Fatal("a later read saw a previous read's mutation: Extras is aliased through the cache")
	}
}

// The HUD fans Reads out concurrently and CI runs a race job. The cache is the
// adapter's only mutable state, so it is the only thing that can make that job
// go red.
func TestConcurrentReadsShareTheCacheSafely(t *testing.T) {
	a, _ := cacheTree(t, `{"type":"assistant","sessionId":"s","message":{"model":"claude-opus-4"}}`+"\n")
	refs, err := a.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ref := refByID(t, refs, healthyID)

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := a.Read(context.Background(), ref); err != nil {
				t.Error(err)
			}
		}()
	}
	// Discover prunes the same map the Reads are consulting.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := a.Discover(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

// A HUD left running for a day must not accumulate one cache entry per session
// that ever existed. Discover is the only caller that knows the live set.
func TestTheCacheDoesNotGrowPastTheLiveSet(t *testing.T) {
	a, path := cacheTree(t, `{"type":"assistant","sessionId":"s","message":{"model":"claude-opus-4"}}`+"\n")
	mustRead(t, a)

	a.mu.Lock()
	n := len(a.parses)
	a.mu.Unlock()
	if n != 1 {
		t.Fatalf("expected one cached parse, got %d", n)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	n = len(a.parses)
	a.mu.Unlock()
	if n != 0 {
		t.Fatalf("the cache kept %d entries for transcripts that are gone", n)
	}
}
