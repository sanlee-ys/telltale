// Package codex adapts Codex CLI's rollout files to model.Session.
//
// # Verification status: SOURCE-READ, NOT LIVE-VERIFIED
//
// Codex CLI is not installed on the development machine (~/.codex absent,
// codex not on PATH, re-confirmed 2026-08-01), so every claim below is read
// from github.com/openai/codex at commit 1e85ca09 rather than from a running
// install: codex-rs/utils/home-dir/src/lib.rs, codex-rs/rollout/src/{lib,
// recorder,compression,policy,metadata}.rs and codex-rs/protocol/src/protocol.rs.
//
// ADR-001's live-session verification is still owed. docs/design.md §3.4
// carries the nine-point checklist, and this adapter is not "done" until it is
// discharged and the fixtures in testdata are reconciled against a real
// rollout file. The two judgement calls most likely to move are marked
// UNVERIFIED in the code below.
//
// # What this adapter cannot know, and why
//
//   - cost — Codex persists token counts, never dollars. Neither the rollout
//     file nor any sidecar carries a USD figure.
//   - name — there is no session title in the format. Rows fall back to the
//     workspace basename.
//   - liveness — there is no analogue of a session/PID registry, so mtime is
//     the only signal and mtime is last activity, not liveness. Codex's own
//     metadata extractor does the same thing (file_modified_time_utc feeds
//     updated_at / recency_at), so this is vendor-consistent rather than a
//     limitation we invented.
//
// # The capability matrix inverts against Claude
//
// Codex ships the context-window denominator on disk (model_context_window)
// and its rate limits with it; Claude ships neither, and puts quota on the
// statusline stdin seam instead. So the HUD's quota column carries a real
// number for Codex rows and an em dash for Claude rows. That asymmetry is a
// design fact recorded in design.md §3.3, not a bug to paper over.
package codex

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sanlee-ys/telltale/internal/jsonl"
	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/theme"
)

// Vendor is the stable id for rows this adapter produces.
const Vendor = model.VendorCodex

// ErrSubAgentThread reports that a rollout file is a sub-agent thread rather
// than a top-level session (session_meta carries agent_nickname / agent_role).
// It is the Codex analogue of Claude's isSidechain flag, and unlike Claude's
// it cannot be seen from the filename — only Read can tell, so the filter
// lives there and the HUD drops the row.
var ErrSubAgentThread = errors.New("codex: rollout is a sub-agent thread, not a session")

// Read budget. Rollout files grow with the session; the head carries
// session_meta (always the first record) and the tail carries the newest
// turn_context and token_count.
const (
	headBytes int64 = 64 << 10
	tailBytes int64 = 256 << 10
)

// futureSkew mirrors the Claude adapter: an mtime meaningfully ahead of the
// observation clock has no readable age and degrades to absent rather than
// rendering "0s".
const futureSkew = 2 * time.Second

// Adapter reads Codex rollout files. It holds no mutable state and is safe for
// concurrent use.
type Adapter struct {
	root string
}

// New returns an adapter rooted at $CODEX_HOME, falling back to ~/.codex —
// the same resolution order as codex-rs/utils/home-dir.
func New() *Adapter {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return &Adapter{root: h}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return &Adapter{}
	}
	return &Adapter{root: filepath.Join(home, ".codex")}
}

// NewWithRoot points the adapter at an explicit CODEX_HOME.
func NewWithRoot(root string) *Adapter { return &Adapter{root: root} }

// Root is the directory this adapter watches, for the HUD's empty state.
func (a *Adapter) Root() string { return a.root }

func (a *Adapter) Vendor() model.VendorID { return Vendor }

// Capabilities is static. context_pct is DERIVED, not reported: Codex ships a
// denominator, so a percentage is computable, but it is computed by us and the
// HUD marks it as an estimate rather than passing it off as a vendor figure.
func (a *Adapter) Capabilities() model.Capabilities {
	return model.Capabilities{
		Reported: model.NewFieldSet(
			model.FieldModel,
			model.FieldWorkspace,
			model.FieldQuota,
			model.FieldLastActivity,
		),
		Derived: model.NewFieldSet(model.FieldContextPercent),
	}
}

// Discover lists rollout files. Directory listing and stat only.
//
// The layout is sessions/<YYYY>/<MM>/<DD>/rollout-<ts>-<uuid>.jsonl at fixed
// depth, so there is no recursive walk and none of Claude's subagent-inflation
// problem. Three things in that tree are deliberately not sessions:
//
//   - *.jsonl.zst — rollouts older than 7 days are compressed in place. A
//     compressed file is by construction at least a week cold and cannot be a
//     live row, so globbing *.jsonl skips them and avoids a zstd dependency
//     for data no HUD row would ever show.
//   - rollout-compression.lock and *.tmp, left by the compression worker.
//   - archived_sessions/, a separate root, deliberately ignored.
//
// Note the date directory is LOCAL time in the vendor's writer, not UTC. That
// is why the glob does not filter by date: deriving today's directory from a
// UTC clock silently loses sessions across midnight and DST.
func (a *Adapter) Discover(ctx context.Context) ([]model.SessionRef, error) {
	if a.root == "" {
		return nil, model.ErrVendorAbsent
	}
	sessions := filepath.Join(a.root, "sessions")
	if _, err := os.Stat(sessions); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, model.ErrVendorAbsent
		}
		return nil, err
	}

	var refs []model.SessionRef
	for _, year := range readDirQuietly(sessions) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !year.IsDir() {
			continue
		}
		yearDir := filepath.Join(sessions, year.Name())
		for _, month := range readDirQuietly(yearDir) {
			if !month.IsDir() {
				continue
			}
			monthDir := filepath.Join(yearDir, month.Name())
			for _, day := range readDirQuietly(monthDir) {
				if !day.IsDir() {
					continue
				}
				dayDir := filepath.Join(monthDir, day.Name())
				for _, e := range readDirQuietly(dayDir) {
					if e.IsDir() {
						continue
					}
					id, ok := sessionIDFromFile(e.Name())
					if !ok {
						continue
					}
					info, err := e.Info()
					if err != nil {
						continue
					}
					refs = append(refs, model.SessionRef{
						Vendor:       Vendor,
						ID:           id,
						Locator:      filepath.Join(dayDir, e.Name()),
						LastActivity: model.TimePtr(info.ModTime()),
					})
				}
			}
		}
	}
	return refs, nil
}

// readDirQuietly swallows read failures: the tree mutates under a sweep (the
// compression worker rewrites files), and a sweep that aborts on one unreadable
// directory loses every other row too.
func readDirQuietly(dir string) []fs.DirEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	return entries
}

// sessionIDFromFile accepts rollout-<timestamp>-<uuid>.jsonl and returns the
// uuid. The .zst, .tmp and .lock neighbours fail the suffix check.
func sessionIDFromFile(name string) (string, bool) {
	if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
		return "", false
	}
	stem := name[len("rollout-") : len(name)-len(".jsonl")]
	if len(stem) < 37 || stem[len(stem)-37] != '-' {
		return "", false
	}
	id := stem[len(stem)-36:]
	if !isUUID(id) {
		return "", false
	}
	return id, true
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < 36; i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}

// rolloutLine is the outer envelope: RolloutLine with RolloutItem flattened in
// under serde(tag="type", content="payload").
type rolloutLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type sessionMeta struct {
	ID            string  `json:"id"`
	SessionID     string  `json:"session_id"`
	Cwd           string  `json:"cwd"`
	CLIVersion    string  `json:"cli_version"`
	Source        string  `json:"source"`
	HistoryMode   string  `json:"history_mode"`
	AgentNickname *string `json:"agent_nickname"`
	AgentRole     *string `json:"agent_role"`
	Git           *struct {
		Branch string `json:"branch"`
	} `json:"git"`
}

type turnContext struct {
	Model string `json:"model"`
	Cwd   string `json:"cwd"`
}

// eventMsg is INTERNALLY tagged: unlike the outer envelope there is no
// "content" key, so the discriminator sits inside payload alongside the
// fields. This is the single easiest thing to get wrong in the format.
type eventMsg struct {
	Type       string      `json:"type"`
	Info       *tokenInfo  `json:"info"`
	RateLimits *rateLimits `json:"rate_limits"`
}

type tokenInfo struct {
	TotalTokenUsage    *tokenUsage `json:"total_token_usage"`
	LastTokenUsage     *tokenUsage `json:"last_token_usage"`
	ModelContextWindow *int64      `json:"model_context_window"`
}

type tokenUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

type rateLimits struct {
	PlanType  string      `json:"plan_type"`
	Primary   *rateWindow `json:"primary"`
	Secondary *rateWindow `json:"secondary"`
}

type rateWindow struct {
	UsedPercent   *float64 `json:"used_percent"`
	WindowMinutes *int64   `json:"window_minutes"`
	ResetsAt      *int64   `json:"resets_at"`
}

// Read parses one rollout file into the normalized model.
func (a *Adapter) Read(ctx context.Context, ref model.SessionRef) (*model.Session, error) {
	f, err := os.Open(ref.Locator)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, model.ErrSessionGone
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	s := &model.Session{Vendor: Vendor, ID: ref.ID, ObservedAt: now}

	if mod := info.ModTime(); mod.After(now.Add(futureSkew)) {
		s.Degraded = s.Degraded.With(model.FieldLastActivity)
		s.Diagnostics = append(s.Diagnostics, "rollout mtime is ahead of the local clock")
	} else {
		s.LastActivity = model.TimePtr(mod)
	}

	// Head window ends where the tail window begins, so no record parses
	// twice (a torn record in the overlap would double the bad count).
	headWindow := headBytes
	if gap := info.Size() - tailBytes; gap < headWindow {
		headWindow = gap
	}
	var head [][]byte
	if headWindow > 0 {
		var err error
		head, err = jsonl.Head(f, headWindow)
		if err != nil {
			return nil, err
		}
	}
	tail, err := jsonl.Tail(f, info.Size(), tailBytes)
	if err != nil {
		return nil, err
	}

	var (
		st       state
		bad      int
		subAgent bool
	)
	consume := func(recs [][]byte) {
		for _, raw := range recs {
			var line rolloutLine
			if err := json.Unmarshal(raw, &line); err != nil {
				bad++
				continue
			}
			if applyLine(&st, &line) {
				subAgent = true
			}
		}
	}
	consume(head)
	consume(tail)

	if subAgent {
		return nil, ErrSubAgentThread
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if bad > 0 {
		// Structure only, never transcript content: this repo is public.
		s.Diagnostics = append(s.Diagnostics, plural(bad, "unparseable record skipped", "unparseable records skipped"))
		// Set BEFORE fold(): fold's degraded-marking checks this flag, so
		// setting it afterwards made the mark unreachable (review finding).
		st.sawBadRecord = true
	}
	st.fold(s)
	return s, nil
}

// state accumulates the last value seen for each datum.
//
// UNVERIFIED JUDGEMENT (design.md §3.4): a token_count event whose info or
// rate_limits is null is treated as CLEARING that datum, not as "unchanged".
// protocol.rs annotates the neighbouring field with "None is unavailable, not
// a sparse-update recovery", which reads as: null means we do not have it,
// rather than reuse the previous value. That is also the conservative side of
// the honest-gauge rule — never show a number the vendor's most recent
// statement did not contain. Confirm against a live session before this is
// called done.
type state struct {
	sawMeta      bool
	cwd          string
	branch       string
	cliVersion   string
	historyMode  string
	modelID      string
	plan         string
	sawTokens    bool
	info         *tokenInfo
	sawLimits    bool
	limits       *rateLimits
	sawBadRecord bool
}

// applyLine folds one envelope into the accumulator and reports whether the
// record identifies this rollout as a sub-agent thread.
func applyLine(st *state, line *rolloutLine) bool {
	switch line.Type {
	case "session_meta":
		var m sessionMeta
		if json.Unmarshal(line.Payload, &m) != nil {
			return false
		}
		st.sawMeta = true
		if m.Cwd != "" {
			st.cwd = m.Cwd
		}
		if m.Git != nil && m.Git.Branch != "" {
			st.branch = m.Git.Branch
		}
		st.cliVersion = m.CLIVersion
		st.historyMode = m.HistoryMode
		// Either marker means this thread belongs to a sub-agent, so it is not
		// a session to report at all.
		return (m.AgentNickname != nil && *m.AgentNickname != "") ||
			(m.AgentRole != nil && *m.AgentRole != "")

	case "turn_context":
		// The model lives here, not on session_meta, and is rewritten once per
		// real user turn — so the LAST turn_context is the current model.
		var tc turnContext
		if json.Unmarshal(line.Payload, &tc) != nil {
			return false
		}
		if tc.Model != "" {
			st.modelID = tc.Model
		}
		if tc.Cwd != "" {
			st.cwd = tc.Cwd
		}

	case "event_msg":
		var ev eventMsg
		if json.Unmarshal(line.Payload, &ev) != nil {
			return false
		}
		if ev.Type != "token_count" {
			return false
		}
		st.sawTokens = true
		st.info = ev.Info
		st.sawLimits = true
		st.limits = ev.RateLimits
		if ev.RateLimits != nil {
			st.plan = ev.RateLimits.PlanType
		}
	}
	return false
}

// fold writes the accumulated state onto the session, applying the honest-gauge
// rules at the one place they are easy to check.
func (st *state) fold(s *model.Session) {
	if st.cwd != "" {
		s.WorkspaceDir = model.Ptr(st.cwd)
	}
	if st.modelID != "" {
		s.Model = &model.Model{ID: st.modelID}
	}
	if st.branch != "" {
		s.Extras = append(s.Extras, model.Extra{Label: "branch", Value: st.branch})
	}
	if st.cliVersion != "" {
		s.Extras = append(s.Extras, model.Extra{Label: "cli", Value: st.cliVersion})
	}
	if st.historyMode != "" {
		s.Extras = append(s.Extras, model.Extra{Label: "history", Value: st.historyMode})
	}
	if st.plan != "" {
		s.Extras = append(s.Extras, model.Extra{Label: "plan", Value: st.plan})
	}

	st.foldContext(s)
	st.foldQuota(s)
}

// foldContext computes the derived context percentage.
//
// Formula: last_token_usage.total_tokens / model_context_window.
//
// It is DELIBERATELY NOT Codex's own percent_of_context_window_remaining,
// which subtracts BASELINE_TOKENS = 12000 from both numerator and denominator
// so the UI reads 100% left after the first prompt. That is a different
// statistic from anything Claude reports, and rendering the two in one column
// as though they were comparable is the honest-gauge violation this whole
// schema exists to make hard (design.md §3.3, §6 Q7).
//
// total_token_usage is deliberately not the numerator: it is cumulative across
// the session and exceeds the window after a compaction, which would produce a
// context gauge over 100%.
func (st *state) foldContext(s *model.Session) {
	if !st.sawTokens {
		return
	}
	if st.info == nil || st.info.ModelContextWindow == nil || *st.info.ModelContextWindow <= 0 || st.info.LastTokenUsage == nil {
		// The vendor's own "no data". Absent, and marked degraded only when a
		// record we failed to parse could have carried it.
		if st.sawBadRecord {
			s.Degraded = s.Degraded.With(model.FieldContextPercent)
		}
		return
	}
	used := float64(st.info.LastTokenUsage.TotalTokens)
	window := float64(*st.info.ModelContextWindow)
	pct := used / window * 100
	if pct < 0 || pct > 100 {
		// Out of range is a broken read, not a value to clamp: a clamped
		// number is invented data.
		s.Degraded = s.Degraded.With(model.FieldContextPercent)
		s.Diagnostics = append(s.Diagnostics, "context percentage out of range 0..100")
		return
	}
	s.ContextPercent = model.PercentPtr(pct)
	s.Derived = s.Derived.With(model.FieldContextPercent)
}

// foldQuota turns the vendor's rate-limit snapshot into labeled windows.
//
// Presence works at two levels and both are load-bearing: a window the vendor
// does not have is absent from the slice, and a window that exists without a
// usage figure is present with a nil percentage and renders "—". Never 0%.
func (st *state) foldQuota(s *model.Session) {
	if !st.sawLimits || st.limits == nil {
		return
	}
	outOfRange := false
	add := func(id string, w *rateWindow, fallback string) {
		if w == nil {
			return
		}
		label := ""
		if w.WindowMinutes != nil {
			label = theme.WindowLabel(*w.WindowMinutes)
		}
		if label == "" {
			// A window whose length the vendor did not report gets a
			// positional label. Calling it "5h" on a guess would be a duration
			// claim with no source.
			label = fallback
		}
		win := model.QuotaWindow{ID: id, Label: label}
		switch {
		case w.UsedPercent == nil:
			// Vendor's own "no data": present window, nil percent, renders —.
		case *w.UsedPercent >= 0 && *w.UsedPercent <= 100:
			win.UsedPercent = model.PercentPtr(*w.UsedPercent)
		default:
			// Same rule as foldContext: out of range is a broken read, diagnosed
			// — not silently dropped (review finding). The Degraded mark is
			// applied after both windows fold, because FieldQuota is present if
			// ANY window carries data and Validate forbids present-and-degraded.
			outOfRange = true
			s.Diagnostics = append(s.Diagnostics, "quota "+id+" used_percent out of range 0..100")
		}
		if w.ResetsAt != nil {
			win.ResetsAt = model.UnixTimePtr(*w.ResetsAt)
		}
		s.Quota = append(s.Quota, win)
	}
	// Display order, shortest first: primary is the shorter window in the
	// vendor's own naming, and the labels carry the real durations anyway.
	add("primary", st.limits.Primary, "1st")
	add("secondary", st.limits.Secondary, "2nd")

	if outOfRange {
		present := false
		for i := range s.Quota {
			if s.Quota[i].UsedPercent != nil || s.Quota[i].ResetsAt != nil {
				present = true
				break
			}
		}
		if !present {
			s.Degraded = s.Degraded.With(model.FieldQuota)
		}
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(int64(n)) + " " + many
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// compile-time contract check.
var _ model.Adapter = (*Adapter)(nil)
