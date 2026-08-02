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
//     with the same id when tokens or tool calls settle. A linear last-wins
//     pass is therefore correct and needs no dedup map.
//   - $set checkpoint records may carry the entire message array on one line;
//     framing goes through internal/jsonl, which has no line cap.
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

	"github.com/sanlee-ys/telltale/internal/jsonl"
	"github.com/sanlee-ys/telltale/internal/model"
)

// Vendor is the stable id for rows this adapter produces.
const Vendor = model.VendorGemini

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

// New returns an adapter rooted at the user's Gemini tmp directory.
func New() *Adapter {
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
// design.
type metaRecord struct {
	SessionID   string   `json:"sessionId"`
	ProjectHash string   `json:"projectHash"`
	StartTime   string   `json:"startTime"`
	LastUpdated string   `json:"lastUpdated"`
	Summary     string   `json:"summary"`
	Kind        string   `json:"kind"`
	Directories []string `json:"directories"`
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

// setEnvelope detects a metadata-update record.
type setEnvelope struct {
	Set *metaRecord `json:"$set"`
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

	var bad int
	var newestTS time.Time
	var sessionID string
	noteTS := func(raw string) {
		if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil &&
			ts.After(newestTS) && !ts.After(now.Add(futureSkew)) {
			newestTS = ts
		}
	}
	applyMeta := func(m *metaRecord) {
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
	}
	apply := func(recs [][]byte) {
		for _, raw := range recs {
			if bad == -1 {
				return
			}
			// A record is exactly one of: $set update, message (string id),
			// rewind marker, or the initial metadata line (sessionId +
			// projectHash). Rewind markers carry nothing this adapter reads.
			var env setEnvelope
			if err := json.Unmarshal(raw, &env); err != nil {
				bad++
				continue
			}
			if env.Set != nil {
				applyMeta(env.Set)
				continue
			}
			var msg messageRecord
			if err := json.Unmarshal(raw, &msg); err != nil {
				bad++
				continue
			}
			if msg.ID != "" {
				noteTS(msg.Timestamp)
				if msg.Type == "gemini" {
					if msg.Model != "" {
						s.Model = &model.Model{ID: msg.Model}
					}
					if msg.Tokens != nil && msg.Tokens.Input > 0 {
						setExtra(s, "ctx tokens", formatTokens(msg.Tokens.Input))
					}
				}
				continue
			}
			var meta metaRecord
			if err := json.Unmarshal(raw, &meta); err != nil {
				bad++
				continue
			}
			if meta.SessionID != "" && meta.ProjectHash != "" {
				applyMeta(&meta)
			}
			// Anything else (e.g. $rewindTo) is a known shape with nothing to
			// read; it is not a parse failure.
		}
	}
	apply(head)
	apply(tail)
	if bad == -1 {
		return nil, ErrSubagentTranscript
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
