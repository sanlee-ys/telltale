// Package democorpus writes a synthetic session corpus the HUD can read, and
// names the adapter roster that reads it.
//
// The corpus exists for one job: showing the HUD on a projector without a real
// session name or a real workspace path on screen. The live scan renders the
// operator's own repositories; a public room must never see those. So the demo
// reads invented data through the REAL adapters — every file here is in the
// vendor's native on-disk format, and no render path learns a special case.
// This is CLAUDE.md's fixture rule ("synthesized, never real, realistic shape
// only") applied at demo size, and it is why the honesty rules survive the
// demo: the HUD still displays only what an adapter read from a store.
//
// Two halves live here on purpose, so they cannot drift apart:
//
//   - Write lays a corpus out under one directory, shaped like a home
//     directory: .claude/projects, .codex/sessions, .grok/sessions,
//     .pi/agent/sessions.
//   - Adapters returns the full adapter roster rooted beneath that same
//     directory, one entry per registered vendor, each at its native
//     home-relative subpath. `telltale hud --root <dir>` consumes this.
//
// The corpus is written with timestamps relative to Write's `now` argument, so
// liveness on screen is real: regenerate just before the demo and the live
// session is live because its records are seconds old, not because anything
// faked a state. The pairing to run:
//
//	go run ./tools/demo-corpus
//	telltale hud --root <the directory it printed>
//
// What the corpus deliberately exercises, because the demo shows them:
//
//   - several vendors side by side (claude, codex, grok, pi);
//   - a live session, idle sessions, and stale sessions — the streams/waits
//     distinction, carried by record timestamps and file mtimes;
//   - zero versus absent on the context column: a grok session whose vendor
//     wrote contextWindowUsage 0 (a measured zero, full empty track) beside
//     claude rows whose vendor exposes no context source at all (CapNone,
//     no track) — the distinction testdata/golden/zero-vs-absent.txt pins;
//   - a derived value wearing its marker: codex context is computed from the
//     vendor's own token counts and window, so the HUD draws it with `~`;
//   - a session quota read from vendor rate limits (codex), and a claude
//     fan-out counted from a subagents sidecar.
//
// Every identifier is invented: session ids are fixed synthetic UUIDs,
// workspaces live under C:\demo\, remotes point at example.invalid. Nothing
// here was copied from a real session, and nothing may be.
package democorpus

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	agyadapter "github.com/sanlee-ys/telltale/internal/adapter/antigravity"
	"github.com/sanlee-ys/telltale/internal/adapter/claudecode"
	"github.com/sanlee-ys/telltale/internal/adapter/codex"
	"github.com/sanlee-ys/telltale/internal/adapter/cursor"
	"github.com/sanlee-ys/telltale/internal/adapter/dropfile"
	"github.com/sanlee-ys/telltale/internal/adapter/gemini"
	grokadapter "github.com/sanlee-ys/telltale/internal/adapter/grok"
	piadapter "github.com/sanlee-ys/telltale/internal/adapter/pi"
	"github.com/sanlee-ys/telltale/internal/model"
)

// MarkerName is the file Write drops at the corpus root. tools/demo-corpus
// deletes an existing output directory only when this marker is present, so
// the generator can never wipe a directory it did not write.
const MarkerName = "telltale-demo-corpus.txt"

// Adapters is the full registered adapter roster, each vendor rooted beneath
// dir at its native home-relative subpath. It mirrors cmd/telltale's
// allAdapters in members and order; the subpaths restate each adapter's own
// New() resolution with dir standing in for the home directory.
//
// Cursor is the one vendor whose live root hangs off the platform config
// directory rather than home. The corpus uses the POSIX form (.config) on
// every platform, because a corpus is data that travels between machines and
// a per-platform layout would make the same tree mean different fleets.
//
// A vendor Write lays down no data for resolves to a directory that does not
// exist, reports model.ErrVendorAbsent, and vanishes from the HUD — the same
// honest path a machine without that vendor takes.
func Adapters(dir string) []model.Adapter {
	return []model.Adapter{
		claudecode.NewWithRoot(filepath.Join(dir, ".claude", "projects")),
		codex.NewWithRoot(filepath.Join(dir, ".codex")),
		gemini.NewWithRoot(filepath.Join(dir, ".gemini", "tmp")),
		agyadapter.NewWithRoot(filepath.Join(dir, ".gemini", "antigravity-cli")),
		cursor.NewWithRoot(filepath.Join(dir, ".config", "Cursor", "User")),
		grokadapter.NewWithRoot(filepath.Join(dir, ".grok", "sessions")),
		piadapter.NewWithRoot(filepath.Join(dir, ".pi", "agent", "sessions")),
		dropfile.NewWithRoot(filepath.Join(dir, ".telltale", "dropfile")),
	}
}

// The invented fleet. Fixed ids so a re-run overwrites the same sessions, and
// so tests can address one session by name.
const (
	claudeLive  = "11111111-0000-4000-8000-000000000001"
	claudeIdle  = "11111111-0000-4000-8000-000000000002"
	claudeStale = "11111111-0000-4000-8000-000000000003"
	codexLive   = "22222222-0000-4000-8000-000000000001"
	codexIdle   = "22222222-0000-4000-8000-000000000002"
	grokLive    = "33333333-0000-4000-8000-000000000001"
	grokIdle    = "33333333-0000-4000-8000-000000000002"
	piStale     = "44444444-0000-4000-8000-000000000001"
)

// WorkspacePrefix is the invented root every corpus workspace lives under.
// Tests assert it, which is the checkable form of "no real path": the corpus
// cannot name a private repository if every path it names starts here.
const WorkspacePrefix = `C:\demo\`

// The invented workspaces. Two vendors share tidepool and two share
// quartz-docs on purpose — a fleet where two agents work one repo is the
// picture the talk draws.
const (
	wsTidepool  = WorkspacePrefix + `src\tidepool`
	wsLantern   = WorkspacePrefix + `src\lanternfish`
	wsQuartz    = WorkspacePrefix + `src\quartz-docs`
	wsMarmalade = WorkspacePrefix + `src\marmalade-api`
	wsSaltmarsh = WorkspacePrefix + `src\saltmarsh`
)

// How old each session reads at generation time, against the shared liveness
// thresholds (live <= 2m, idle <= 15m, stale beyond). The margins are wide so
// a demo that starts a few minutes after generation still shows the states.
const (
	ageClaudeLive  = 25 * time.Second
	ageClaudeIdle  = 6 * time.Minute
	ageClaudeStale = 150 * time.Minute
	ageCodexLive   = 50 * time.Second
	ageCodexIdle   = 8 * time.Minute
	ageGrokLive    = 30 * time.Second
	ageGrokIdle    = 7 * time.Minute
	agePiStale     = 110 * time.Minute
)

// Write lays the corpus out under dir, with every timestamp relative to now.
// It creates directories as needed and overwrites its own files; it deletes
// nothing. Callers that want a clean slate delete first — see tools/demo-corpus
// and MarkerName.
func Write(dir string, now time.Time) error {
	if err := writeClaude(dir, now); err != nil {
		return err
	}
	if err := writeCodex(dir, now); err != nil {
		return err
	}
	if err := writeGrok(dir, now); err != nil {
		return err
	}
	if err := writePi(dir, now); err != nil {
		return err
	}
	marker := "This directory is a synthetic telltale demo corpus.\n" +
		"tools/demo-corpus wrote it on " + now.UTC().Format(time.RFC3339) + " and may delete and rewrite it.\n" +
		"Every session, path and id in it is invented. Point the HUD at it with:\n" +
		"  telltale hud --root " + dir + "\n"
	return os.WriteFile(filepath.Join(dir, MarkerName), []byte(marker), 0o644)
}

// ts renders a timestamp the way the JSONL-writing vendors do: RFC3339, UTC,
// millisecond precision, Z suffix.
func ts(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// writeLines writes one file of JSONL records and stamps its mtime, which is
// half of every adapter's last-activity fold (the other half is the newest
// record timestamp; both point at `at` here, so the fold is unambiguous).
func writeLines(path string, lines []string, at time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	return os.Chtimes(path, at, at)
}

// jsonPath escapes a Windows path for embedding in a JSON string literal.
func jsonPath(p string) string { return strings.ReplaceAll(p, `\`, `\\`) }

// claudeSlug is the project-directory name Claude Code derives from a cwd.
// The adapter never decodes it (its own doc says why), so the exact mapping
// only has to be shaped right: separators and the drive colon become dashes.
func claudeSlug(ws string) string {
	s := strings.ReplaceAll(ws, ":", "-")
	return strings.ReplaceAll(s, `\`, "-")
}

func writeClaude(dir string, now time.Time) error {
	root := filepath.Join(dir, ".claude", "projects")
	type spec struct {
		id, name, ws, model, branch string
		age                         time.Duration
		subagents                   int
	}
	sessions := []spec{
		{claudeLive, "Wire the retry queue", wsTidepool, "claude-opus-5", "feat/retry-queue", ageClaudeLive, 2},
		{claudeIdle, "Chase the flaky login test", wsLantern, "claude-sonnet-5", "fix/flaky-login", ageClaudeIdle, 0},
		{claudeStale, "Draft the release notes", wsQuartz, "claude-sonnet-5", "docs/release-notes", ageClaudeStale, 0},
	}
	for _, sp := range sessions {
		last := now.Add(-sp.age)
		start := last.Add(-4 * time.Minute)
		ws := jsonPath(sp.ws)
		lines := []string{
			fmt.Sprintf(`{"parentUuid":null,"isSidechain":false,"userType":"external","cwd":"%s","sessionId":"%s","version":"2.1.233","gitBranch":"%s","entrypoint":"cli","type":"user","message":{"role":"user","content":"synthetic demo turn - no real session content"},"uuid":"aaaa0000-0000-4000-8000-000000000001","timestamp":"%s"}`,
				ws, sp.id, sp.branch, ts(start)),
			fmt.Sprintf(`{"type":"custom-title","customTitle":"%s","sessionId":"%s"}`, sp.name, sp.id),
			fmt.Sprintf(`{"parentUuid":"aaaa0000-0000-4000-8000-000000000001","isSidechain":false,"userType":"external","cwd":"%s","sessionId":"%s","version":"2.1.233","gitBranch":"%s","entrypoint":"cli","type":"assistant","message":{"id":"msg_demo_0001","type":"message","role":"assistant","model":"%s","content":[{"type":"text","text":"synthetic demo reply"}],"stop_reason":"end_turn","usage":{"input_tokens":12,"cache_creation_input_tokens":2048,"cache_read_input_tokens":51200,"output_tokens":312,"service_tier":"standard"}},"uuid":"aaaa0000-0000-4000-8000-000000000002","timestamp":"%s"}`,
				ws, sp.id, sp.branch, sp.model, ts(last)),
		}
		file := filepath.Join(root, claudeSlug(sp.ws), sp.id+".jsonl")
		if err := writeLines(file, lines, last); err != nil {
			return err
		}
		// The fan-out sidecar: counted by stat, so the files only need to
		// exist and be recent. Kept inside the liveness Idle horizon the
		// adapter shares with the row's own dot.
		for i := 0; i < sp.subagents; i++ {
			side := filepath.Join(root, claudeSlug(sp.ws), sp.id, "subagents",
				fmt.Sprintf("agent-demo%013d.jsonl", i+1))
			at := now.Add(-time.Duration(40+30*i) * time.Second)
			line := fmt.Sprintf(`{"type":"user","sessionId":"%s","message":{"role":"user","content":"synthetic demo subagent"},"timestamp":"%s"}`, sp.id, ts(at))
			if err := writeLines(side, []string{line}, at); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeCodex(dir string, now time.Time) error {
	type spec struct {
		id, ws, branch, repo string
		age                  time.Duration
		usedTokens           int64
		withRateLimits       bool
	}
	sessions := []spec{
		{codexLive, wsTidepool, "feat/retry-queue", "tidepool", ageCodexLive, 118200, true},
		{codexIdle, wsMarmalade, "chore/dep-bumps", "marmalade-api", ageCodexIdle, 31000, false},
	}
	for _, sp := range sessions {
		last := now.Add(-sp.age)
		start := last.Add(-5 * time.Minute)
		ws := jsonPath(sp.ws)
		rate := "null"
		if sp.withRateLimits {
			rate = fmt.Sprintf(`{"limit_id":"demo_limit","limit_name":"demo plan","primary":{"used_percent":41.2,"window_minutes":300,"resets_at":%d},"secondary":{"used_percent":8.5,"window_minutes":10080,"resets_at":%d},"credits":null,"individual_limit":null,"spend_control_reached":false,"plan_type":"plus","rate_limit_reached_type":null}`,
				now.Add(2*time.Hour).Unix(), now.Add(3*24*time.Hour).Unix())
		}
		lines := []string{
			fmt.Sprintf(`{"timestamp":"%s","ordinal":1,"type":"session_meta","payload":{"id":"%s","session_id":"%s","timestamp":"%s","cwd":"%s","originator":"codex_cli_rs","cli_version":"0.0.0-demo","source":"cli","model_provider":"openai","history_mode":"paginated","git":{"commit_hash":"0000000000000000000000000000000000000000","branch":"%s","repository_url":"https://example.invalid/demo/%s.git"}}}`,
				ts(start), sp.id, sp.id, ts(start), ws, sp.branch, sp.repo),
			fmt.Sprintf(`{"timestamp":"%s","ordinal":2,"type":"turn_context","payload":{"turn_id":"turn_demo_0001","cwd":"%s","current_date":"%s","timezone":"UTC","approval_policy":"on-request","sandbox_policy":{"mode":"workspace-write"},"model":"gpt-5.2-codex"}}`,
				ts(start.Add(time.Second)), ws, start.UTC().Format("2006-01-02")),
			fmt.Sprintf(`{"timestamp":"%s","ordinal":3,"type":"response_item","payload":{"type":"message","id":"item_demo_0001","role":"user","content":[{"type":"input_text","text":"synthetic demo turn - no real session content"}]}}`,
				ts(start.Add(2*time.Second))),
			fmt.Sprintf(`{"timestamp":"%s","ordinal":4,"type":"response_item","payload":{"type":"message","id":"item_demo_0002","role":"assistant","content":[{"type":"output_text","text":"synthetic demo reply"}]}}`,
				ts(last.Add(-time.Second))),
			fmt.Sprintf(`{"timestamp":"%s","ordinal":5,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":800,"reasoning_output_tokens":0,"total_tokens":%d},"last_token_usage":{"input_tokens":%d,"cached_input_tokens":0,"cache_write_input_tokens":0,"output_tokens":800,"reasoning_output_tokens":0,"total_tokens":%d},"model_context_window":272000},"rate_limits":%s}}`,
				ts(last), sp.usedTokens, sp.usedTokens+800, sp.usedTokens, sp.usedTokens+800, rate),
		}
		day := start.UTC()
		file := filepath.Join(dir, ".codex", "sessions",
			day.Format("2006"), day.Format("01"), day.Format("02"),
			fmt.Sprintf("rollout-%s-%s.jsonl", day.Format("2006-01-02T15-04-05"), sp.id))
		if err := writeLines(file, lines, last); err != nil {
			return err
		}
	}
	return nil
}

// grokEncode is the percent-encoded directory name grok derives from a cwd.
// The adapter prefers summary.json's own cwd and treats this as a fallback,
// so shape is what matters: colon and backslash are the encoded characters.
func grokEncode(ws string) string {
	s := strings.ReplaceAll(ws, ":", "%3A")
	return strings.ReplaceAll(s, `\`, "%5C")
}

func writeGrok(dir string, now time.Time) error {
	type spec struct {
		id, name, ws string
		age          time.Duration
		ctxPct       int
		ctxTokens    int64
	}
	sessions := []spec{
		{grokLive, "Profile the cold start", wsLantern, ageGrokLive, 34, 170000},
		// contextWindowUsage 0 is the corpus's measured zero: a reading of 0
		// is a reading (the adapter's own rule), and it must render as a full
		// empty track beside claude's absent context, never as the same glyph.
		{grokIdle, "Sweep the TODO backlog", wsSaltmarsh, ageGrokIdle, 0, 0},
	}
	for _, sp := range sessions {
		last := now.Add(-sp.age)
		start := last.Add(-9 * time.Minute)
		sess := filepath.Join(dir, ".grok", "sessions", grokEncode(sp.ws), sp.id)
		summary := fmt.Sprintf(`{
  "info": {
    "id": "%s",
    "cwd": "%s"
  },
  "session_summary": "%s",
  "created_at": "%s",
  "updated_at": "%s",
  "num_messages": 24,
  "num_chat_messages": 14,
  "current_model_id": "grok-4.5",
  "chat_format_version": 1,
  "git_root_dir": "%s",
  "git_remotes": [
    "https://example.invalid/demo/%s.git"
  ],
  "head_commit": "0000000000000000000000000000000000000000",
  "head_branch": "main",
  "grok_home": "C:\\demo\\home\\.grok",
  "last_active_at": "%s",
  "generated_title": "%s",
  "agent_name": "demo-agent",
  "sandbox_profile": "off",
  "reasoning_effort": "high"
}`, sp.id, jsonPath(sp.ws), sp.name, ts(start), ts(last),
			strings.ReplaceAll(sp.ws, `\`, "/"), filepath.Base(sp.ws), ts(last), sp.name)
		if err := os.MkdirAll(sess, 0o755); err != nil {
			return err
		}
		sumPath := filepath.Join(sess, "summary.json")
		if err := os.WriteFile(sumPath, []byte(summary+"\n"), 0o644); err != nil {
			return err
		}
		signals := fmt.Sprintf(`{"turnCount":3,"userMessageCount":3,"assistantMessageCount":8,"errorCount":0,"toolFailureCount":0,"cancellationCount":0,"regenerationCount":0,"hasReverted":false,"compactionCount":0,"totalTokensBeforeCompaction":0,"contextWindowUsage":%d,"contextTokensUsed":%d,"contextWindowTokens":500000,"toolCallCount":6,"toolsUsed":["grep","read_file"],"modelsUsed":["grok-4.5"],"primaryModelId":"grok-4.5","sessionDurationSeconds":540,"avgTimeToFirstTokenMs":9000,"avgResponseTimeMs":15000,"agentLinesAdded":0,"agentLinesRemoved":0,"agentFilesTouched":0,"humanFilesTouched":0,"totalFilesTouched":0,"peakRssBytes":0}`,
			sp.ctxPct, sp.ctxTokens)
		if err := os.WriteFile(filepath.Join(sess, "signals.json"), []byte(signals+"\n"), 0o644); err != nil {
			return err
		}
		// Discover stats the session directory; applySummary folds the file's
		// own mtime. Stamp both to the session's last activity.
		if err := os.Chtimes(sumPath, last, last); err != nil {
			return err
		}
		if err := os.Chtimes(sess, last, last); err != nil {
			return err
		}
	}
	return nil
}

// piMunge is the sessions-directory name pi derives from a cwd: every
// non-alphanumeric character becomes a dash and the whole is wrapped in
// leading and trailing dashes, matching the shape the live survey recorded.
// The adapter reads cwd from the session header, so this too is shape only.
func piMunge(ws string) string {
	var b strings.Builder
	b.WriteString("--")
	for _, r := range ws {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	b.WriteString("--")
	return b.String()
}

func writePi(dir string, now time.Time) error {
	last := now.Add(-agePiStale)
	start := last.Add(-3 * time.Minute)
	ws := jsonPath(wsQuartz)
	lines := []string{
		fmt.Sprintf(`{"type":"session","version":3,"id":"%s","timestamp":"%s","cwd":"%s"}`, piStale, ts(start), ws),
		fmt.Sprintf(`{"type":"model_change","id":"bbbb0001","parentId":null,"timestamp":"%s","provider":"anthropic","modelId":"claude-sonnet-5"}`, ts(start)),
		fmt.Sprintf(`{"type":"session_info","id":"bbbb0002","parentId":"bbbb0001","timestamp":"%s","name":"Rename the config keys"}`, ts(start)),
		fmt.Sprintf(`{"type":"message","id":"bbbb0003","parentId":"bbbb0002","timestamp":"%s","message":{"role":"user","content":[{"type":"text","text":"synthetic demo turn - no real session content"}],"timestamp":%d}}`,
			ts(start.Add(time.Second)), start.UnixMilli()),
		fmt.Sprintf(`{"type":"message","id":"bbbb0004","parentId":"bbbb0003","timestamp":"%s","message":{"role":"assistant","model":"claude-sonnet-5","provider":"anthropic","content":[{"type":"text","text":"synthetic demo reply"}],"usage":{"input":100,"output":20,"cacheRead":0,"cacheWrite":0,"reasoning":0,"totalTokens":120,"cost":{"input":0.001,"output":0.0002,"cacheRead":0.0,"cacheWrite":0.0,"total":0.0012}}}}`,
			ts(last)),
	}
	file := filepath.Join(dir, ".pi", "agent", "sessions", piMunge(wsQuartz),
		fmt.Sprintf("%s_%s.jsonl", start.UTC().Format("2006-01-02T15-04-05-000Z"), piStale))
	return writeLines(file, lines, last)
}
