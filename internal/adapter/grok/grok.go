// Package grok adapts the Grok CLI's on-disk session store to model.Session.
//
// Source surveyed live on 2026-08-09 against grok 1.0.0 (3cd0d0cbce) on the
// Windows 11 dev box, signed in against grok.com with model grok-4.5: 30
// session directories under ~/.grok/sessions across 8 workspaces, read
// read-only — no lock taken, no byte written, the `.lock` sidecars beside every
// JSONL deliberately never opened. The survey is written up in docs/design.md
// §3.9a — numbered with the surveys rather than after §3.10's inventory of them
// — and nothing from it is reproduced here or in testdata, which is synthesized
// to shape.
//
// # Layout
//
// Sessions are DIRECTORIES, not files:
//
//	~/.grok/sessions/<percent-encoded-cwd>/<uuid>/summary.json
//	                                             /signals.json
//	                                             /updates.jsonl
//	                                             /chat_history.jsonl, events.jsonl, …
//
// Five files were present in 30 of 30 session directories — summary.json,
// chat_history.jsonl, events.jsonl, prompt_context.json, system_prompt.txt —
// and everything else varies (signals.json 23 of 30, resources_state.json 13 of
// 30, a terminal/ subdirectory 5 of 30). This adapter reads only summary.json
// (the invariant), signals.json and the tail of updates.jsonl, and the variance
// is why: a field sourced from a file two thirds of sessions have is ABSENT on
// the other third, which is a first-class state here, not a failure.
//
// # What this adapter cannot know, and why
//
// Each was grepped for across the live corpus with the result recorded, and is
// declared CapNone rather than filled with a plausible number:
//
//   - cost — the vendor DOES write money, and it is still not a session total.
//     `updates.jsonl` carries one `turn_completed` record per prompt whose
//     `usage.costUsdTicks` is that TURN's cost; `"[a-z_]*cost[a-z_]*"` matched
//     nothing else anywhere in the store, and no cumulative figure exists in
//     summary.json, signals.json or any sidecar. Three consecutive turns of one
//     session read 455412000, 820464000, 747416000 ticks — the third smaller
//     than the second — so the field is per-turn and not a running total. A
//     session total would therefore have to be summed across every turn record,
//     and updates.jsonl reached 818 KB in one observed session, past this
//     adapter's tail budget. A sum over the tail window is a LOWER BOUND, and a
//     lower bound rendered in a column headed COST is a derived number wearing a
//     read one's clothes (decisions/001). The last turn's cost is carried as a
//     display-only Extra instead, labeled as the turn's, where it cannot be
//     mistaken for the session's.
//   - quota — nothing account-level reaches disk. `"[a-z_]*(rate|limit|quota)[a-z_]*"`
//     over every .json and .jsonl in the store returned only tool-configuration
//     keys (`output_byte_limit`, `head_limit`). There is no window, no ordinal
//     and no reset time to report.
//   - liveness — see the note on active_sessions.json below.
//   - subagents — grok offers a `spawn_subagent` tool (it is in the tool list
//     every headless run prints), but no sub-agent transcript, nest, count or
//     parent link was found on disk: `"subagent[A-Za-z_]*"` matched nothing
//     outside the system prompt's tool description. Declaring the field and
//     emitting zero would assert "this session is running no sub-agents", which
//     the format gives no way to check — the same ruling Codex got (§3.3).
//
// # Liveness, and the registry that claims it and does not
//
// ~/.grok/sessions/../active_sessions.json looks like the liveness source this
// adapter would want. It is not, and that verdict is MEASURED rather than
// argued: with 30 sessions in the store the file held the two bytes `[]`, and
// it still held `[]` while a headless turn was mid-flight — sampled with
// grok.exe confirmed running by PID, and with the file's own mtime freshly
// stamped by that run, so the vendor had written it and written nothing in it.
// A registry that is empty during a live session cannot distinguish "nothing is
// running" from "the thing running is not the kind it tracks", and design.md
// §4a.4 already names process-existence as the one case where an adapter can
// lie to the HUD undetectably. So the honest primitive is the vendor's own
// activity timestamp, and the HUD classifies liveness from its age against the
// one shared threshold set, identically for every vendor.
//
// events.jsonl carries a second tempting signal — `phase_changed` (1765 of them
// in one session), `turn_started`/`turn_ended`, and a `permission_requested`
// that is a genuine needs-input state. It is left unread in v1 for the reason
// the file itself demonstrates: the newest session in the corpus ends on an
// unresolved `permission_requested` written minutes before grok exited, so "the
// last event is a prompt" is indistinguishable from "a dead session was killed
// at a prompt". A liveness hint that stays true forever after the process is
// gone is worse than no hint (design.md §4a.4).
//
// # What is deliberately not opened
//
//   - `~/.grok/auth.json` — the OAuth token store. This adapter never resolves a
//     path outside the sessions tree; the Cursor seam (§3.9) is the precedent
//     for saying so out loud.
//   - `sessions/session_search.sqlite` — a full-text index whose `session_docs`
//     table holds a `content` column carrying transcript TEXT. It would answer
//     "what is this session about" cheaply and that is exactly the trade this
//     repo does not make: the title in summary.json is the vendor's own label,
//     and nothing the HUD renders needs conversation content.
//   - `prompt_context.json` — inlines the user's CLAUDE.md/AGENTS.md files
//     verbatim. Same rule.
//   - the `.lock` sidecars — never opened, never created. The gauges read.
package grok

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sanlee-ys/telltale/internal/adapter/drift"
	"github.com/sanlee-ys/telltale/internal/jsonl"
	"github.com/sanlee-ys/telltale/internal/model"
)

// Vendor is the stable id for rows this adapter produces.
const Vendor = model.VendorGrok

// verifiedAgainst names the vendor build this adapter's field map was read from
// (see the package doc). Nothing inside a session directory states the writer's
// version — `chat_format_version` is 1 on every session and is a record-format
// ordinal, not a build — so a drift report here carries the pin and no observed
// counterpart. The build string is the one `grok --version` prints.
//
// Moved to 1.0.4 on 2026-08-14, and the scope of what was re-checked is stated
// rather than implied, because it is NARROWER than the 30-session survey the
// package doc describes. What was re-read: one session directory written by
// 1.0.4 itself, holding every key this adapter names — `info.id`, `info.cwd`,
// `generated_title`, `session_summary`, `current_model_id`, `last_active_at`,
// `updated_at`, `created_at`; signals.json's `contextWindowUsage`,
// `contextTokensUsed`, `contextWindowTokens`; and updates.jsonl's
// `params.update.sessionUpdate: "turn_completed"` carrying `usage.costUsdTicks`.
// `chat_format_version` is still 1. The quota verdict was re-run the same way
// the survey ran it — a rate/limit/quota key sweep over the new directory — and
// it still matches nothing account-level, so quota stays CapNone.
//
// What was NOT re-done: the file-presence census across many sessions and many
// workspaces, and the `active_sessions.json` liveness measurement. Those
// verdicts still rest on the 2026-08-09 survey, and this constant does not
// promise otherwise.
const verifiedAgainst = "grok 1.0.4 (d846eb93d9)"

// canarySummaryInfoID is summary.json's identity envelope, `info.id`.
//
// The survey found `info` with an `id` on 30 of 30 session directories, and it
// is the envelope every other reading here hangs off: `info.cwd` is the
// workspace, and the sibling keys are the title, the model and the activity
// timestamps. A summary.json that PARSES and carries no info.id is not the
// shape this field map describes, and the fields would otherwise go quiet in a
// way indistinguishable from a session the vendor had nothing to say about.
var canarySummaryInfoID = drift.Canary{
	Name: "summary.json info.id",
	Feeds: model.NewFieldSet(
		model.FieldName,
		model.FieldModel,
		model.FieldWorkspace,
		model.FieldLastActivity,
	),
}

// Read budget.
//
// summary.json and signals.json are read whole because they are BOUNDED by
// construction — the vendor rewrites each in place with a fixed key set, and
// across 30 sessions the largest were 918 and 1538 bytes. The caps below are
// two orders of magnitude above that and exist for the case the survey cannot
// rule out: one session carried a `last_turn_summary` key holding model prose,
// so this file is not structurally incapable of growing. A file past its cap is
// not slurped — the fields it feeds degrade and say so, which is the same
// answer an unreadable file gets.
//
// updates.jsonl has no such bound (818 KB in one observed session, and it only
// ever appends), so it is read from the tail like every other JSONL in this
// repo. Its window is a QUARTER of the 256 KB the field-bearing adapters use,
// and deliberately: the only record it is here for is written at a turn
// boundary and the streaming chunks between boundaries are a few hundred bytes
// each, so the newest one is close to the end — and everything this file feeds
// is a display-only Extra. Missing it costs a line in the detail pane and no
// field on the row, which is not worth four times the I/O on every poll. A turn
// still streaming can push the last COMPLETED turn out of the window; that is
// the known cost and it degrades nothing.
const (
	maxSummaryBytes  int64 = 64 << 10
	maxSignalsBytes  int64 = 64 << 10
	updatesTailBytes int64 = 64 << 10
)

// futureSkew mirrors the other adapters: a timestamp meaningfully ahead of the
// observation clock has no readable age and degrades to absent rather than
// rendering "0s".
const futureSkew = 2 * time.Second

// Files inside a session directory. Only these three are opened.
const (
	summaryFile = "summary.json"
	signalsFile = "signals.json"
	updatesFile = "updates.jsonl"
)

// Adapter reads Grok CLI sessions. It holds no mutable state and is safe for
// concurrent use.
type Adapter struct {
	// root is the directory holding per-workspace subtrees, ~/.grok/sessions.
	root string
}

// New returns an adapter rooted at the user's Grok sessions directory,
// honouring GROK_HOME.
//
// The override is not a guess: grok's own ~/.grok/logs/unified.jsonl records
// its startup resolution as an `AuthManager::new` line naming `grok_home`
// beside the four environment variables it consulted — HOME, GROK_HOME,
// GROK_AUTH_PATH, GROK_AUTH — and every session's summary.json then writes the
// resolved `grok_home` back out. GROK_HOME replaces the whole .grok directory
// rather than the home above it, which is why it is joined without ".grok".
func New() *Adapter {
	if h := os.Getenv("GROK_HOME"); h != "" {
		return &Adapter{root: filepath.Join(h, "sessions")}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// An unresolvable home is indistinguishable from "not installed" for
		// our purposes: Discover will report the vendor absent.
		return &Adapter{}
	}
	return &Adapter{root: filepath.Join(home, ".grok", "sessions")}
}

// NewWithRoot points the adapter at an explicit sessions directory. Tests use
// it; so would a future config key.
func NewWithRoot(root string) *Adapter { return &Adapter{root: root} }

// Root is the directory this adapter watches, for the HUD's empty state. It is
// the only path this adapter exposes for display — per-session locators can
// name another user's paths on a shared machine and are never rendered.
func (a *Adapter) Root() string { return a.root }

func (a *Adapter) Vendor() model.VendorID { return Vendor }

// Capabilities is static. See the package doc for why four of the nine fields
// are absent from both sets.
//
// context_pct is REPORTED, and it is the second vendor after Cursor to earn
// that: signals.json carries `contextWindowUsage`, a percentage the VENDOR
// computed, beside the raw `contextTokensUsed`/`contextWindowTokens` it
// computed it from. The denominator problem that makes the field CapNone for
// Claude, Gemini and agy simply is not present — grok writes the window size
// down. Recomputing the percentage from the two raw numbers would be more
// precise (the vendor truncates: 39656/500000 = 7.93 is written as 7) and it
// would also be a number the vendor never said, which is what CapDerived is
// for and what this field does not need to be.
//
// Nothing is DERIVED here. That set is empty on purpose rather than by
// omission: every value below was read, and the two the adapter could have
// computed — a session cost total and a finer context percentage — are the two
// the package doc explains it will not.
func (a *Adapter) Capabilities() model.Capabilities {
	return model.Capabilities{
		Reported: model.NewFieldSet(
			model.FieldName,
			model.FieldModel,
			model.FieldWorkspace,
			model.FieldContextPercent,
			model.FieldLastActivity,
		),
	}
}

// Discover lists session directories. Directory listing and stat only — the HUD
// calls this every poll tick.
//
// The walk is sessions/<workspace>/<uuid>/ at fixed depth and is NOT recursive.
// Three things in that tree are not sessions and are excluded structurally:
//
//   - sessions/session_search.sqlite and sessions/<workspace>/prompt_history.jsonl
//     are FILES, and only directories are considered.
//   - <uuid>/terminal/ holds per-tool-call logs and nests one level deeper than
//     the walk goes.
//   - a directory whose name is not a UUID is skipped outright. The ids grok
//     writes are UUIDv7 (`019fe7e8-4d98-7e70-…`), which the shape check accepts
//     as the version-agnostic UUID it is; pinning the version digit would be a
//     guess about the next release.
//
// A candidate must also hold a readable summary.json, which is the invariant
// file (30 of 30 sessions) and doubles as the freshness hint: the DIRECTORY's
// own mtime moves when an entry is added or removed, not when a file inside it
// grows, so a session that has done nothing but append all day would look
// untouched. summary.json is rewritten every turn.
func (a *Adapter) Discover(ctx context.Context) ([]model.SessionRef, error) {
	if a.root == "" {
		return nil, model.ErrVendorAbsent
	}
	workspaces, err := os.ReadDir(a.root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, model.ErrVendorAbsent
	}
	if err != nil {
		return nil, err
	}

	var refs []model.SessionRef
	for _, ws := range workspaces {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !ws.IsDir() {
			continue
		}
		dir := filepath.Join(a.root, ws.Name())
		entries, err := os.ReadDir(dir)
		if err != nil {
			// The tree mutates during a sweep — grok creates a workspace
			// directory the moment a session starts in a new cwd. A sweep that
			// aborts on that loses every other vendor's rows too.
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || !isUUID(e.Name()) {
				continue
			}
			sess := filepath.Join(dir, e.Name())
			info, err := os.Stat(filepath.Join(sess, summaryFile))
			if err != nil {
				// No summary.json: either not a session directory, or one grok
				// is in the middle of creating. Either way there is nothing to
				// read yet, and a row with no readable field is not a row.
				continue
			}
			refs = append(refs, model.SessionRef{
				Vendor:       Vendor,
				ID:           e.Name(),
				Locator:      sess,
				LastActivity: model.TimePtr(info.ModTime()),
			})
		}
	}
	return refs, nil
}

// isUUID accepts the 8-4-4-4-12 hex shape, version-agnostic.
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

// summaryDoc is the subset of summary.json this adapter reads. Unknown fields
// are ignored by design: the vendor adds keys between versions, and the survey
// already saw six that appear on one session and not the rest (the git block,
// `last_turn_summary`).
type summaryDoc struct {
	Info struct {
		ID  string `json:"id"`
		Cwd string `json:"cwd"`
	} `json:"info"`
	// GeneratedTitle is the vendor's own label for the session, written on 17
	// of 30. SessionSummary carries the same string when both are present and
	// is the empty string on a headless `--single` run, which never gets a
	// title at all — absence, not failure.
	GeneratedTitle string `json:"generated_title"`
	SessionSummary string `json:"session_summary"`
	CurrentModelID string `json:"current_model_id"`
	// Three RFC3339 timestamps, all written by the vendor. last_active_at was
	// present on 29 of 30 and is the one that means what it says; updated_at is
	// the fallback and was present on 30 of 30.
	LastActiveAt string `json:"last_active_at"`
	UpdatedAt    string `json:"updated_at"`
	CreatedAt    string `json:"created_at"`
}

// signalsDoc is the subset of signals.json this adapter reads.
//
// The file is a flat camelCase telemetry blob — turn counts, latency
// percentiles, doom-loop counters — and three of its keys are the context
// gauge. It is written at a turn boundary: the 7 sessions in the corpus with no
// signals.json were sessions killed mid-turn or that never completed one, which
// is why its absence is "no reading yet" rather than "this vendor cannot".
type signalsDoc struct {
	ContextWindowUsage  *float64 `json:"contextWindowUsage"`
	ContextTokensUsed   int64    `json:"contextTokensUsed"`
	ContextWindowTokens int64    `json:"contextWindowTokens"`
}

// updateLine is the subset of an updates.jsonl record this adapter reads.
//
// updates.jsonl is grok's own ACP session-update stream persisted verbatim —
// the same wire internal/council/vendors/grok.go parses live — so the shapes
// agree by construction. Only `turn_completed` is read.
type updateLine struct {
	Params struct {
		Update struct {
			SessionUpdate string `json:"sessionUpdate"`
			Usage         *usage `json:"usage"`
		} `json:"update"`
	} `json:"params"`
}

type usage struct {
	TotalTokens int64 `json:"totalTokens"`
	// CostUsdTicks is a fixed-point USD figure at 1e10 ticks to the dollar.
	//
	// The scale is MEASURED, not inferred from the name. grok's headless wire
	// prints both forms of the same number on its `end` event, and three live
	// runs on this box on 2026-08-09 gave 0.0306488/306488000,
	// 0.0315248/315248000 and 0.0382104/382104000 — exactly 1e10, three times.
	// The disk field then had to be shown to be the same quantity rather than
	// merely a similarly named one, so the three runs' on-disk costUsdTicks
	// were read back and matched the wire's ticks value for value. Without
	// that second step this would be a plausible unit, and design.md §3.9a
	// records both halves.
	CostUsdTicks int64 `json:"costUsdTicks"`
}

// usdTicksPerDollar is the fixed-point scale pinned above.
const usdTicksPerDollar = 1e10

// Read parses one session directory into the normalized model.
//
// Partial failure is not an error: a field that cannot be parsed is left nil,
// marked degraded and explained in Diagnostics, and the row still renders with
// an em dash in that cell.
func (a *Adapter) Read(ctx context.Context, ref model.SessionRef) (*model.Session, error) {
	now := time.Now()
	s := &model.Session{Vendor: Vendor, ID: ref.ID, ObservedAt: now}

	raw, mtime, err := readCapped(filepath.Join(ref.Locator, summaryFile), maxSummaryBytes)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// The directory went away between Discover and Read, or grok removed
		// the session. The HUD drops the row silently.
		return nil, model.ErrSessionGone
	case errors.Is(err, errTooLarge):
		s.Degraded = s.Degraded.
			With(model.FieldName).With(model.FieldModel).
			With(model.FieldWorkspace).With(model.FieldLastActivity)
		s.Diagnostics = append(s.Diagnostics, "summary.json past the read budget")
		return s, ctx.Err()
	case err != nil:
		s.Degraded = s.Degraded.
			With(model.FieldName).With(model.FieldModel).
			With(model.FieldWorkspace).With(model.FieldLastActivity)
		s.Diagnostics = append(s.Diagnostics, "summary.json unreadable")
		return s, ctx.Err()
	}

	w := drift.NewWatch(verifiedAgainst, canarySummaryInfoID)

	var sum summaryDoc
	sampled := 0
	if err := json.Unmarshal(raw, &sum); err != nil {
		// A torn write is the routine race with the vendor's writer, not drift:
		// sampled stays 0 and the watch reports nothing (drift.Fold's
		// load-bearing zero case).
		s.Degraded = s.Degraded.
			With(model.FieldName).With(model.FieldModel).
			With(model.FieldWorkspace).With(model.FieldLastActivity)
		s.Diagnostics = append(s.Diagnostics, "summary.json unparseable")
	} else {
		sampled = 1
		if sum.Info.ID != "" {
			w.Saw(canarySummaryInfoID)
		}
		a.applySummary(s, &sum, ref.Locator, mtime, now)
	}

	a.applySignals(s, ref.Locator)
	a.applyLastTurn(s, ref.Locator)

	// Last, because the verdict reads what the session managed to source.
	w.Fold(s, sampled)

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

// applySummary folds summary.json into the session.
func (a *Adapter) applySummary(s *model.Session, sum *summaryDoc, locator string, mtime, now time.Time) {
	// A generated title outranks the running summary; they carried the same
	// string on every session that had both, and the fallback exists for the
	// order in which the vendor writes them, not for a disagreement.
	switch {
	case sum.GeneratedTitle != "":
		s.Name = model.Ptr(sum.GeneratedTitle)
	case sum.SessionSummary != "":
		s.Name = model.Ptr(sum.SessionSummary)
	}

	if sum.CurrentModelID != "" {
		s.Model = &model.Model{ID: sum.CurrentModelID}
	}

	// The vendor's own record of where the session runs, native format,
	// absolute. The directory name one level up carries the same path
	// percent-encoded and is used only when this is missing — see
	// decodeWorkspace for why decoding is legitimate here and is not for
	// Claude Code (§3.1).
	if sum.Info.Cwd != "" {
		s.WorkspaceDir = model.Ptr(sum.Info.Cwd)
	} else if dec, ok := decodeWorkspace(filepath.Base(filepath.Dir(locator))); ok {
		s.WorkspaceDir = model.Ptr(dec)
	}

	// §6 Q8 fold: last_activity is the freshest of the vendor's own timestamps
	// and the file's mtime, each guarded independently against a clock ahead of
	// ours. Only if every signal is unreadable does the field degrade.
	newest := time.Time{}
	note := func(raw string) {
		ts, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil || ts.After(now.Add(futureSkew)) || !ts.After(newest) {
			return
		}
		newest = ts
	}
	note(sum.LastActiveAt)
	note(sum.UpdatedAt)
	note(sum.CreatedAt)
	if !mtime.After(now.Add(futureSkew)) && mtime.After(newest) {
		newest = mtime
	}
	if newest.IsZero() {
		s.Degraded = s.Degraded.With(model.FieldLastActivity)
		s.Diagnostics = append(s.Diagnostics, "no readable activity timestamp (mtime ahead of the clock, no readable summary timestamps)")
		return
	}
	s.LastActivity = model.TimePtr(newest)
}

// applySignals folds signals.json's context gauge into the session.
//
// Three outcomes, and keeping them apart is the point:
//
//   - the file does not exist → the session has not finished a turn, so there
//     is no reading yet. Nil, NOT degraded: the capability is declared and the
//     HUD draws an em dash meaning "absent now".
//   - the file exists and cannot be read or parsed, or carries a percentage
//     outside 0..100 → degraded plus a diagnostic. A percentage out of range is
//     dropped rather than clamped, because a clamped value is invented data
//     (model.Percent's contract).
//   - a reading of 0 is a reading. It survives as one.
func (a *Adapter) applySignals(s *model.Session, locator string) {
	raw, _, err := readCapped(filepath.Join(locator, signalsFile), maxSignalsBytes)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return
	case err != nil:
		s.Degraded = s.Degraded.With(model.FieldContextPercent)
		s.Diagnostics = append(s.Diagnostics, "signals.json unreadable")
		return
	}
	var sig signalsDoc
	if err := json.Unmarshal(raw, &sig); err != nil {
		s.Degraded = s.Degraded.With(model.FieldContextPercent)
		s.Diagnostics = append(s.Diagnostics, "signals.json unparseable")
		return
	}
	if sig.ContextTokensUsed > 0 {
		setExtra(s, "ctx tokens", formatTokens(sig.ContextTokensUsed))
	}
	if sig.ContextWindowTokens > 0 {
		setExtra(s, "ctx window", formatTokens(sig.ContextWindowTokens))
	}
	if sig.ContextWindowUsage == nil {
		return
	}
	p := *sig.ContextWindowUsage
	if p < 0 || p > 100 {
		s.Degraded = s.Degraded.With(model.FieldContextPercent)
		s.Diagnostics = append(s.Diagnostics, "context percentage out of range")
		return
	}
	s.ContextPercent = model.PercentPtr(p)
}

// applyLastTurn reads the newest turn_completed record out of updates.jsonl and
// carries its cost and token count as display-only Extras.
//
// This is the one place the adapter touches money, and the labels do the work
// the COST column is not allowed to: "turn cost" is the cost of ONE turn, read
// verbatim from the vendor's own figure, and there is no claim anywhere on the
// row that it is the session's. See the package doc for why the session total
// is CapNone rather than a sum.
//
// Absence is silent. updates.jsonl was missing from 1 of 30 sessions and a
// session that has completed no turn has no record in it; neither is a failure
// to read, so neither degrades anything — an Extra that is not set simply does
// not appear in the detail pane.
func (a *Adapter) applyLastTurn(s *model.Session, locator string) {
	f, err := os.Open(filepath.Join(locator, updatesFile))
	if err != nil {
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return
	}
	recs, err := jsonl.Tail(f, info.Size(), updatesTailBytes)
	if err != nil {
		return
	}

	var last *usage
	var bad int
	for _, rec := range recs {
		var line updateLine
		if err := json.Unmarshal(rec, &line); err != nil {
			// One bad record degrades the fields it feeds, not the row — and
			// these feed only Extras, so it costs a diagnostic and nothing else.
			// The tail's FIRST record is routinely a fragment of a line the
			// window cut in half, which is why this is counted rather than
			// treated as an anomaly.
			bad++
			continue
		}
		if line.Params.Update.SessionUpdate == "turn_completed" && line.Params.Update.Usage != nil {
			last = line.Params.Update.Usage
		}
	}
	if bad > 0 {
		// Structure only, never transcript content: this repo is public.
		s.Diagnostics = append(s.Diagnostics, plural(bad, "unparseable update record skipped", "unparseable update records skipped"))
	}
	if last == nil {
		return
	}
	if last.CostUsdTicks > 0 {
		setExtra(s, "turn cost", formatUSD(float64(last.CostUsdTicks)/usdTicksPerDollar))
	}
	if last.TotalTokens > 0 {
		setExtra(s, "turn tokens", formatTokens(last.TotalTokens))
	}
}

// errTooLarge reports a file past its read budget. It is deliberately not a
// read error: the file is fine, the budget said no.
var errTooLarge = errors.New("grok: file past the read budget")

// readCapped reads a whole small file, refusing anything past cap, and returns
// its mtime alongside. The stat happens on the open handle so the size that is
// checked is the size that is read.
func readCapped(path string, cap int64) ([]byte, time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, time.Time{}, err
	}
	if info.Size() > cap {
		return nil, info.ModTime(), errTooLarge
	}
	buf := make([]byte, info.Size())
	n, err := f.ReadAt(buf, 0)
	if err != nil && int64(n) != info.Size() {
		return nil, info.ModTime(), err
	}
	return buf[:n], info.ModTime(), nil
}

// decodeWorkspace turns a session tree's percent-encoded directory name back
// into the path it names.
//
// `C%3A%5CUsers%5Csanle%5Ccode%5Ctelltale` is `C:\Users\sanle\code\telltale`.
// Across the 8 workspace directories surveyed the encoding was ordinary
// percent-encoding — `:` as %3A and `\` as %5C, with letters, digits and `-`
// passing through literally — and it ROUND-TRIPS: every decoded name matched
// the `info.cwd` its sessions recorded, character for character including the
// drive letter's case.
//
// That is the whole reason this function exists at all, and it is worth stating
// against the neighbouring rule: the Claude Code adapter refuses to decode its
// project-directory slug (§3.1) because that encoding maps both '\' and a
// literal '-' onto '-' and is therefore lossy — decoding it would invent a
// path. Grok's is injective, so decoding it invents nothing.
//
// It is still only a FALLBACK, used when summary.json parsed and carried no
// cwd — never observed, since 30 of 30 sessions had one. The vendor's own
// record of where it is running outranks a key this adapter reconstructed, and
// a malformed escape yields no path rather than a mangled one.
func decodeWorkspace(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	var b strings.Builder
	b.Grow(len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c != '%' {
			b.WriteByte(c)
			continue
		}
		if i+2 >= len(name) {
			return "", false
		}
		hi, ok := unhex(name[i+1])
		if !ok {
			return "", false
		}
		lo, ok := unhex(name[i+2])
		if !ok {
			return "", false
		}
		b.WriteByte(hi<<4 | lo)
		i += 2
	}
	out := b.String()
	if out == "" {
		return "", false
	}
	return out, true
}

func unhex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

func setExtra(s *model.Session, label, value string) {
	for i := range s.Extras {
		if s.Extras[i].Label == label {
			s.Extras[i].Value = value
			return
		}
	}
	s.Extras = append(s.Extras, model.Extra{Label: label, Value: value})
}

func formatTokens(n int64) string {
	switch {
	case n <= 0:
		return "0"
	case n < 1000:
		return itoa(n)
	case n < 1_000_000:
		return itoa(n/1000) + "k"
	default:
		return itoa(n/1_000_000) + "M"
	}
}

// formatUSD renders cents-and-a-bit. Four decimal places, because a single grok
// turn measured $0.0306 on this box and two places would render most turns as
// $0.03 or $0.00 — a rounding that turns a measurement into a shrug.
func formatUSD(v float64) string {
	if v < 0 {
		return "$0.0000"
	}
	ten4 := int64(v*10000 + 0.5)
	whole := ten4 / 10000
	frac := ten4 % 10000
	var b [4]byte
	for i := 3; i >= 0; i-- {
		b[i] = byte('0' + frac%10)
		frac /= 10
	}
	return "$" + itoa(whole) + "." + string(b[:])
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

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(int64(n)) + " " + many
}

// compile-time contract check.
var _ model.Adapter = (*Adapter)(nil)
