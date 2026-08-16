// Package gemini adapts Gemini CLI's on-disk chat recordings to
// model.Session.
//
// Source verified against gemini-cli v0.53.1 (the installed version), read at
// tag v0.53.1: packages/core/src/services/chatRecordingService.ts (writer),
// chatRecordingTypes.ts (record shapes), config/storage.ts (tree layout) and
// config/projectRegistry.ts (projects.json). The live-corpus pass is itemized
// in docs/design.md §3.7; claims below that only a corpus can settle are
// marked there, not silently assumed here.
//
// # Layout
//
// Sessions are JSONL at ~/.gemini/tmp/<project-slug>/chats/session-<ts>-<id8>.jsonl.
// The slug is assigned by ~/.gemini/projects.json ({projects: {<abs path>:
// <slug>}}); the registry migrated from sha256-hash directory names in 0.5x,
// and hash-named directories may persist on old installs. Sub-agent
// transcripts nest one level down at chats/<parent-session-id>/<id>.jsonl —
// unlike Codex, the parent link is structural, so a sub-agent count is
// attributable and FieldSubagents is declared.
//
// # What this adapter cannot know, and why
//
//   - context_pct — nothing on disk carries a context window size. The CLI's
//     own percentage comes from a static per-model table compiled into its
//     source, which is exactly the assumed denominator decisions/001 forbids.
//   - cost — no USD figure anywhere in the recording.
//   - quota — nothing is persisted. Rate limiting exists only as runtime 429
//     handling (googleQuotaErrors.ts, retry.ts); no window, ordinal or reset
//     time ever reaches disk.
//   - liveness — same posture as every adapter: last activity is reported and
//     the HUD classifies age with the shared thresholds.
//
// # Traps encoded here
//
//   - The writer DELETES a session file on exit when it holds no resumable
//     content (deleteCurrentSessionIfNotResumableAsync), so files vanish
//     between Discover and Read in normal operation: ErrSessionGone, row
//     dropped, no diagnostic.
//   - Messages are UPSERTS: the writer re-appends the full message record
//     with the same id when tokens or tool calls settle. The adapter replays
//     them through an ordered per-id log so the re-appended values win.
//   - {"$rewindTo": id} removes that message and everything after it — the
//     vendor's loader truncates its map there — so the adapter's replay
//     truncates its log the same way. A rewind to an id outside the read
//     windows conservatively clears the collected message state: values that
//     may have been rewound away are absence, not a display (review finding,
//     2026-08-02).
//   - {"$set": {"messages": [...]}} is a CHECKPOINT: the loader clears and
//     rebuilds from the array, so the replay does too. Those lines can carry
//     the entire conversation; framing goes through internal/jsonl, which has
//     no line cap — though a checkpoint larger than the tail budget is
//     outside the bounded read entirely (documented in design.md §3.7).
//   - Legacy pre-JSONL sessions are single-document *.json files. Discover
//     skips them: parsing a second format for sessions that are by
//     construction from an older install is not worth a second parse path in
//     v1, and skipping is absence, not a wrong row.
package gemini

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
const Vendor = model.VendorGemini

// VerifiedAgainst names the vendor build this adapter's field map was read at
// (see the package doc). Nothing in the recording states the writer's version,
// so a drift report here carries the pin and no observed counterpart.
const VerifiedAgainst = "gemini-cli v0.53.1"

// canaryMetadata is the recording's opening line — sessionId plus projectHash,
// the first thing chatRecordingService.ts writes for every session, and the only
// line that carries both. It anchors the full session id the sub-agent nest is
// keyed by and is where a summary becomes the row's name.
//
// It is always inside the read windows: the head window covers the start of the
// file whenever it is enabled at all, and when it is not, the tail covers the
// whole file.
var canaryMetadata = drift.Canary{
	Name: "metadata record",
	Feeds: model.NewFieldSet(
		model.FieldName,
		model.FieldSubagents,
	),
}

// ErrSubagentTranscript reports that a chat file records a sub-agent run, not
// a top-level session (metadata kind == "subagent"). Sub-agent files normally
// nest under chats/<parent-id>/ and never pass Discover; this sentinel is the
// backstop for a vendor change that moves them inline, mirroring Codex's
// ErrSubAgentThread. The HUD drops the row.
var ErrSubagentTranscript = errors.New("gemini: chat file is a sub-agent transcript, not a session")

// Read budget, mirroring the other adapters: the head carries the initial
// metadata record (always the first line the writer emits) and the tail
// carries the newest messages, token summaries and $set updates.
const (
	headBytes int64 = 64 << 10
	tailBytes int64 = 256 << 10
)

// futureSkew mirrors the other adapters: a timestamp meaningfully ahead of
// the observation clock has no readable age and degrades to absent rather
// than rendering "0s".
const futureSkew = 2 * time.Second

// registryFile is the vendor's project-path-to-slug mapping, one level above
// the tmp root this adapter watches.
const registryFile = "projects.json"

// subagentHorizon mirrors the Claude adapter: the shared liveness boundary is
// what turns "files written lately" into "a fan-out is running", and it is
// the reason the count is DERIVED rather than reported.
var subagentHorizon = model.DefaultLivenessThresholds.Idle

// Adapter reads Gemini CLI chat recordings. It holds no mutable state and is
// safe for concurrent use.
type Adapter struct {
	// root is the tmp directory holding per-project subtrees, ~/.gemini/tmp.
	root string
}

// New returns an adapter rooted at the user's Gemini tmp directory,
// honouring GEMINI_CLI_HOME — the same resolution order as the vendor's
// homedir() (paths.ts): the override replaces the home directory, and
// .gemini/tmp hangs beneath whichever wins.
func New() *Adapter {
	if h := os.Getenv("GEMINI_CLI_HOME"); h != "" {
		return &Adapter{root: filepath.Join(h, ".gemini", "tmp")}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return &Adapter{}
	}
	return &Adapter{root: filepath.Join(home, ".gemini", "tmp")}
}

// NewWithRoot points the adapter at an explicit tmp directory. Tests use it.
func NewWithRoot(root string) *Adapter { return &Adapter{root: root} }

// Root is the directory this adapter watches, for the HUD's empty state.
func (a *Adapter) Root() string { return a.root }

func (a *Adapter) Vendor() model.VendorID { return Vendor }

// Capabilities is static. See the package doc for why three fields are absent.
//
// workspace is REPORTED: the value is read verbatim from the vendor's
// projects.json registry entry for the session's project directory. It is a
// lookup, not a computation — though the vendor normalizes the recorded path
// (lowercased on Windows), which is a fidelity caveat, not an estimate.
//
// subagents is DERIVED for the same reason as Claude's: the files are counted
// exactly and the recency boundary is the inference.
func (a *Adapter) Capabilities() model.Capabilities {
	return model.Capabilities{
		Reported: model.NewFieldSet(
			model.FieldName,
			model.FieldModel,
			model.FieldWorkspace,
			model.FieldLastActivity,
		),
		Derived: model.NewFieldSet(model.FieldSubagents),
	}
}

// Discover lists chat files. Directory listing and stat only.
//
// The walk is tmp/<slug>/chats/*.jsonl at fixed depth — never recursive.
// Everything under chats/ that is a DIRECTORY is a parent session's sub-agent
// nest and is deliberately not walked here (their COUNT is a fact about the
// parent, gathered at Read time). Non-sessions filtered by construction:
//
//   - *.json (no l) — legacy single-document recordings from pre-JSONL
//     installs; skipped, see the package doc.
//   - tmp/bin/ — the vendor's tool-download cache lives inside the tmp root
//     (storage.ts getGlobalBinDir); it has no chats/ subdirectory so the
//     fixed-shape walk never enters it, but it is named here so nobody
//     "fixes" the walk into a recursive one.
//
// The filename prefix ("session-") is deliberately NOT required: main
// sessions carry it today, but sub-agent files are excluded structurally by
// nesting rather than by name, and a prefix pin is the rename-away-to-zero
// failure mode the Claude review already caught once.
func (a *Adapter) Discover(ctx context.Context) ([]model.SessionRef, error) {
	if a.root == "" {
		return nil, model.ErrVendorAbsent
	}
	projects, err := os.ReadDir(a.root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, model.ErrVendorAbsent
	}
	if err != nil {
		return nil, err
	}

	var refs []model.SessionRef
	for _, p := range projects {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !p.IsDir() {
			continue
		}
		chats := filepath.Join(a.root, p.Name(), "chats")
		entries, err := os.ReadDir(chats)
		if err != nil {
			// No chats/ under this project dir (bin/, memory-only projects) or
			// the tree mutated mid-sweep. Either way: not sessions, keep going.
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			refs = append(refs, model.SessionRef{
				Vendor:       Vendor,
				ID:           strings.TrimSuffix(e.Name(), ".jsonl"),
				Locator:      filepath.Join(chats, e.Name()),
				LastActivity: model.TimePtr(info.ModTime()),
			})
		}
	}
	return refs, nil
}

// metaRecord is the initial metadata line and any $set update, folded into
// one shape. The writer's first line carries sessionId + projectHash; later
// lines carry {"$set": {...partial...}}. Unknown fields are ignored by
// design. Messages appears in two places the loader also handles: on a $set
// it is a whole-conversation CHECKPOINT (clear and rebuild); on the initial
// line it is a legacy single-line record's message array.
type metaRecord struct {
	SessionID   string          `json:"sessionId"`
	ProjectHash string          `json:"projectHash"`
	StartTime   string          `json:"startTime"`
	LastUpdated string          `json:"lastUpdated"`
	Summary     string          `json:"summary"`
	Kind        string          `json:"kind"`
	Directories []string        `json:"directories"`
	Messages    []messageRecord `json:"messages"`
}

// messageRecord is the subset of a message record this adapter reads. Only
// records of type "gemini" carry model and tokens.
type messageRecord struct {
	ID        string  `json:"id"`
	Timestamp string  `json:"timestamp"`
	Type      string  `json:"type"`
	Model     string  `json:"model"`
	Tokens    *tokens `json:"tokens"`
}

// tokens mirrors TokensSummary (chatRecordingTypes.ts): input is
// promptTokenCount — the size of what was sent, which makes it the honest
// context-occupancy proxy; cached is a subset of it, not an addition.
type tokens struct {
	Input  int64 `json:"input"`
	Output int64 `json:"output"`
	Total  int64 `json:"total"`
}

// envelope detects the two $-prefixed record shapes: a metadata update and a
// rewind marker.
type envelope struct {
	Set      *metaRecord `json:"$set"`
	RewindTo *string     `json:"$rewindTo"`
}

// msgSnapshot is one message's contribution to the fields this adapter
// sources, held in an ordered log so rewinds and checkpoints can truncate or
// rebuild it exactly as the vendor's own loader does.
type msgSnapshot struct {
	id     string
	model  string
	tokens *tokens
}

// Read parses one chat recording into the normalized model.
//
// Partial failure is not an error: a field that cannot be parsed is left nil,
// marked degraded and explained in Diagnostics, and the row still renders
// with an em dash in that cell.
func (a *Adapter) Read(ctx context.Context, ref model.SessionRef) (*model.Session, error) {
	f, err := os.Open(ref.Locator)
	if errors.Is(err, fs.ErrNotExist) {
		// Normal operation, not an anomaly: the writer deletes non-resumable
		// sessions on exit (see the package doc).
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

	// §6 Q8: last_activity is the fresher of the file's mtime and the newest
	// record timestamp; each signal passes the future-skew guard on its own,
	// and only if both are unreadable does the field degrade.
	mtimeOK := false
	var mtime time.Time
	if mod := info.ModTime(); !mod.After(now.Add(futureSkew)) {
		mtimeOK, mtime = true, mod
	}

	// Head/tail windows must not overlap (see the Claude adapter): records in
	// the overlap would parse twice and inflate the unparseable-record count.
	headWindow := int64(headBytes)
	if gap := info.Size() - tailBytes; gap < headWindow {
		headWindow = gap // <= 0 disables the head read entirely
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

	var bad, good int
	var newestTS time.Time
	var sessionID string
	w := drift.NewWatch(VerifiedAgainst, canaryMetadata)
	noteTS := func(raw string) {
		if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil &&
			ts.After(newestTS) && !ts.After(now.Add(futureSkew)) {
			newestTS = ts
		}
	}

	// The ordered message log, replayed exactly as the vendor's loader
	// replays its map: upserts update in place, a rewind truncates from the
	// target id, a checkpoint clears and rebuilds. Timestamps are noted at
	// encounter regardless — a rewound record was still WRITTEN when its
	// timestamp says, so it stays evidence of activity while its values stop
	// being displayable.
	var msgs []msgSnapshot
	idx := map[string]int{}
	resetMsgs := func() {
		msgs = msgs[:0]
		idx = map[string]int{}
	}
	applyMsg := func(m *messageRecord) {
		noteTS(m.Timestamp)
		if i, ok := idx[m.ID]; ok {
			if m.Type == "gemini" {
				if m.Model != "" {
					msgs[i].model = m.Model
				}
				if m.Tokens != nil {
					msgs[i].tokens = m.Tokens
				}
			}
			return
		}
		idx[m.ID] = len(msgs)
		snap := msgSnapshot{id: m.ID}
		if m.Type == "gemini" {
			snap.model = m.Model
			snap.tokens = m.Tokens
		}
		msgs = append(msgs, snap)
	}
	rewind := func(id string) {
		if i, ok := idx[id]; ok {
			for _, m := range msgs[i:] {
				delete(idx, m.id)
			}
			msgs = msgs[:i]
			return
		}
		// The target sits outside the read windows (or never existed — the
		// loader clears everything in that case too). Conservative side of
		// the honest gauge: values that MAY have been rewound away are
		// absence, and later records re-establish whatever survived.
		resetMsgs()
	}
	applyMeta := func(m *metaRecord, isCheckpoint bool) {
		if m.Kind == "subagent" {
			// Backstop only — sub-agent files are excluded structurally by
			// Discover. Signalled via sessionID below because apply() cannot
			// return an error mid-pass.
			sessionID = ""
			bad = -1 // sentinel checked after the pass
			return
		}
		if m.SessionID != "" {
			sessionID = m.SessionID
		}
		if m.Summary != "" {
			s.Name = model.Ptr(m.Summary)
		}
		noteTS(m.LastUpdated)
		noteTS(m.StartTime)
		if len(m.Messages) > 0 {
			// $set.messages is a whole-conversation checkpoint: the loader
			// clears its map and rebuilds from the array. A legacy initial
			// record's array is applied without the clear, same as the loader.
			if isCheckpoint {
				resetMsgs()
			}
			for i := range m.Messages {
				applyMsg(&m.Messages[i])
			}
		}
	}
	apply := func(recs [][]byte) {
		for _, raw := range recs {
			if bad == -1 {
				return
			}
			// A record is exactly one of: $set update, $rewindTo marker,
			// message (string id), or the initial metadata line (sessionId +
			// projectHash).
			var env envelope
			if err := json.Unmarshal(raw, &env); err != nil {
				bad++
				continue
			}
			good++
			if env.Set != nil {
				applyMeta(env.Set, true)
				continue
			}
			if env.RewindTo != nil {
				rewind(*env.RewindTo)
				continue
			}
			var msg messageRecord
			if err := json.Unmarshal(raw, &msg); err != nil {
				bad++
				continue
			}
			if msg.ID != "" {
				applyMsg(&msg)
				continue
			}
			var meta metaRecord
			if err := json.Unmarshal(raw, &meta); err != nil {
				bad++
				continue
			}
			if meta.SessionID != "" && meta.ProjectHash != "" {
				w.Saw(canaryMetadata)
				applyMeta(&meta, false)
			}
			// Anything else is a known-shaped record with nothing to read;
			// it is not a parse failure.
		}
	}
	apply(head)
	apply(tail)
	if bad == -1 {
		return nil, ErrSubagentTranscript
	}

	// The newest surviving contribution wins — surviving meaning it was not
	// truncated by a rewind or replaced by a checkpoint. A tokens reading of
	// zero is a reading (the writer persists promptTokenCount ?? 0), so nil
	// is the only absence.
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].model != "" {
			s.Model = &model.Model{ID: msgs[i].model}
			break
		}
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].tokens != nil {
			setExtra(s, "ctx tokens", formatTokens(msgs[i].tokens.Input))
			break
		}
	}

	// Q8 fold: the fresher valid signal wins; both invalid degrades.
	switch {
	case mtimeOK && newestTS.After(mtime):
		s.LastActivity = model.TimePtr(newestTS)
	case mtimeOK:
		s.LastActivity = model.TimePtr(mtime)
	case !newestTS.IsZero():
		s.LastActivity = model.TimePtr(newestTS)
	default:
		s.Degraded = s.Degraded.With(model.FieldLastActivity)
		s.Diagnostics = append(s.Diagnostics, "no readable activity timestamp (mtime ahead of the clock, no record timestamps)")
	}

	if bad > 0 {
		// Structure only, never transcript content: this repo is public.
		s.Diagnostics = append(s.Diagnostics, plural(bad, "unparseable record skipped", "unparseable records skipped"))
	}

	a.resolveWorkspace(s, ref.Locator)
	a.countSubagents(s, ref.Locator, sessionID, now)

	// Last, because the verdict reads what the session managed to source. The
	// subagent count degrades on its own above and says what it cost; this says
	// why, which is the part a per-row diagnostic cannot know.
	w.Fold(s, good)

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

// resolveWorkspace maps the session's project-directory slug back to the
// project path via the vendor's registry, ~/.gemini/projects.json.
//
// The registry value is the vendor's own record of the project root, read
// verbatim — hence REPORTED, with one documented fidelity caveat: the writer
// normalizes the path (lowercased on Windows, projectRegistry.ts).
//
// Absence is not degradation: a missing registry, or a slug with no entry
// (the registry self-heals from .project_root markers and can lag), leaves
// the field nil. Only a registry that exists and cannot be read or parsed
// degrades the field — that is a broken read, not a missing fact.
func (a *Adapter) resolveWorkspace(s *model.Session, locator string) {
	slug := filepath.Base(filepath.Dir(filepath.Dir(locator)))
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(a.root), registryFile))
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		s.Degraded = s.Degraded.With(model.FieldWorkspace)
		s.Diagnostics = append(s.Diagnostics, "project registry unreadable")
		return
	}
	var reg struct {
		Projects map[string]string `json:"projects"`
	}
	if err := json.Unmarshal(raw, &reg); err != nil {
		s.Degraded = s.Degraded.With(model.FieldWorkspace)
		s.Diagnostics = append(s.Diagnostics, "project registry unparseable")
		return
	}
	for path, id := range reg.Projects {
		if id == slug {
			s.WorkspaceDir = model.Ptr(path)
			return
		}
	}
}

// countSubagents counts the session's recently written sub-agent transcripts.
//
// The nest is chats/<full-session-id>/ beside the parent's file — keyed by
// the FULL session id from the metadata record, not the filename, which
// embeds only the id's first 8 characters. A session whose metadata never
// parsed has no resolvable nest: nil plus degraded, because "0" would be a
// claim about a directory we could not name.
//
// Semantics mirror the Claude adapter exactly: directory absent is a measured
// ZERO; unreadable is nil plus a diagnostic; entries are stat-only, skipped
// when older than the shared horizon or ahead of the clock.
func (a *Adapter) countSubagents(s *model.Session, locator, sessionID string, now time.Time) {
	if sessionID == "" {
		s.Degraded = s.Degraded.With(model.FieldSubagents)
		s.Diagnostics = append(s.Diagnostics, "session id unreadable, subagent nest unresolvable")
		return
	}
	dir := filepath.Join(filepath.Dir(locator), sessionID)
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		s.Subagents = model.Ptr(0)
		s.Derived = s.Derived.With(model.FieldSubagents)
		return
	case err != nil:
		s.Degraded = s.Degraded.With(model.FieldSubagents)
		s.Diagnostics = append(s.Diagnostics, "subagents directory unreadable")
		return
	}

	cutoff := now.Add(-subagentHorizon)
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			// Racing the writer is normal; undercounting is the conservative
			// direction.
			continue
		}
		mod := info.ModTime()
		if mod.Before(cutoff) || mod.After(now.Add(futureSkew)) {
			continue
		}
		n++
	}
	s.Subagents = model.Ptr(n)
	s.Derived = s.Derived.With(model.FieldSubagents)
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
