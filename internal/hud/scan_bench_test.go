package hud

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/adapter/claudecode"
	"github.com/sanlee-ys/telltale/internal/model"
)

// BenchmarkScan pins the poll's cost at the scale the HUD actually runs at.
//
// # Why this exists
//
// The HUD polls once a second and the footer says "last scan Ns ago" past three
// (view.go staleAfter). On the owner's corpus that notice was on permanently:
// 1,404 sessions across six vendors, and a warm scan took 1.8–3.4 s. Profiling
// on 2026-08-09 put 2.65 s of the 3.6 s of serial work in json.Unmarshal —
// 46,727 records re-parsed every single second, none of which had changed.
// docs/design.md §7.18 carries the full per-vendor, per-phase table.
//
// A number is the only thing that keeps that from coming back, so this
// benchmark is the baseline a future regression has to fail against.
//
// # What it measures, and what it does not
//
// The corpus is Claude-shaped and Claude only. That is a deliberate narrowing,
// not an oversight: Claude was 967 of the owner's 1,404 sessions and 2.6 s of
// the 2.9 s of read time, and it is the only adapter carrying the parse cache
// this benchmark guards. The other five were measured at 138 ms (codex), 42 ms
// (agy), 11 ms (grok), 3 ms (gemini) and 2 ms (cursor) — together under a fifth
// of the budget — and synthesizing an SQLite store and a protobuf database to
// re-measure that would buy a worse signal for much more test.
//
// Session count matches the real corpus scale (1,400); per-file size is chosen
// to reproduce the real RECORD count inside the read windows (~48 records per
// session, the measured average) rather than the real bytes on disk, because
// the reads are head/tail-bounded and it is records-parsed that costs. That
// keeps the generated tree around 30 MB instead of 700 MB — this runs in CI.
//
// Nothing here is committed as data: the tree is generated into b.TempDir() and
// goes away with the run. This repository is public and holds no real corpus.
func BenchmarkScan(b *testing.B) {
	sessions := 1400
	if testing.Short() {
		// -short still exercises the cache path end to end; it just stops
		// paying 30 MB of disk to do it.
		sessions = 50
	}
	root := benchCorpus(b, sessions)
	adapters := []model.Adapter{claudecode.NewWithRoot(root)}

	// One scan outside the timer: the first is a cold parse of every file by
	// construction, and averaging it in would hide exactly the thing being
	// measured — that the SECOND scan and every one after is nearly free.
	if got := len(Scan(context.Background(), adapters, time.Now()).Sessions); got != sessions {
		b.Fatalf("corpus did not read back: %d of %d sessions", got, sessions)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Scan(context.Background(), adapters, time.Now())
	}
}

// BenchmarkScanCold is the other half of the pair: the first scan after launch,
// where every transcript must genuinely be parsed. It is the cost the spinner
// covers, and it is what the steady-state number should be compared against to
// see how much the cache is actually saving.
func BenchmarkScanCold(b *testing.B) {
	sessions := 1400
	if testing.Short() {
		sessions = 50
	}
	root := benchCorpus(b, sessions)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// A fresh adapter is a cold cache. Building it is a struct literal, so
		// it costs nothing the measurement has to apologize for.
		Scan(context.Background(), []model.Adapter{claudecode.NewWithRoot(root)}, time.Now())
	}
}

// benchCorpus writes a synthesized Claude projects tree. Fake ids, fake paths,
// realistic SHAPE only — the repo's fixture rule, applied to generated data.
func benchCorpus(b *testing.B, sessions int) string {
	b.Helper()
	root := b.TempDir()

	// Spread over project directories the way a real tree is: Discover does one
	// ReadDir per project, so a single flat directory would understate it.
	const perProject = 20
	// ~48 records is the measured average number that lands inside the head and
	// tail windows on the owner's corpus.
	const recordsPerSession = 48

	var body strings.Builder
	for i := 0; i < sessions; i++ {
		proj := filepath.Join(root, fmt.Sprintf("C--Users-dev-code-project-%03d", i/perProject))
		if err := os.MkdirAll(proj, 0o755); err != nil {
			b.Fatal(err)
		}
		id := fmt.Sprintf("%08x-aaaa-4bbb-8ccc-000000000001", i)
		body.Reset()
		ts := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
		for r := 0; r < recordsPerSession; r++ {
			ts = ts.Add(time.Minute)
			fmt.Fprintf(&body,
				`{"type":"assistant","sessionId":"%s","cwd":"C:\\dev\\project-%03d","gitBranch":"main","version":"2.1.219","timestamp":"%s","message":{"model":"claude-opus-4","usage":{"input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d}},"padding":"%s"}`+"\n",
				id, i/perProject, ts.Format(time.RFC3339), 12+r, 190000+r*7, 300+r,
				// Records in the real corpus are a few hundred bytes because
				// they carry message content this adapter never decodes. The
				// padding stands in for it: encoding/json still has to scan
				// past it, which is the cost being measured.
				strings.Repeat("x", 300))
		}
		if err := os.WriteFile(filepath.Join(proj, id+".jsonl"), []byte(body.String()), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	return root
}
