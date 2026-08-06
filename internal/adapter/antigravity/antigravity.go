// Package antigravity adapts Antigravity CLI's on-disk conversation state to
// model.Session.
//
// Antigravity CLI (`agy`) is closed source — the GitHub repo is docs and
// examples only — so this adapter follows the Claude Code precedent rather
// than the Codex/Gemini one: documented contracts cross-checked against a live
// corpus, no source read possible. The survey, the field map, the protobuf
// field numbers and the build cautions are docs/design.md §3.8 (re-survey
// block, agy 1.1.9, 2026-08-02); the decision is decisions/006. Nothing is
// claimed here that §3.8 did not observe.
//
// # Layout
//
//	~/.gemini/antigravity-cli/
//	  conversations/<conversation-id>.db          SQLite, protobuf blobs
//	  conversations/<conversation-id>.db-wal       load-bearing sidecar
//	  brain/<conversation-id>/.system_generated/logs/transcript.jsonl
//
// The transcript is plain JSONL, one record per conversation step, and it is
// the primary source: structural fields only (step_index, source, type,
// status, created_at), never `content` or `thinking`. The database supplies
// two things the transcript does not carry — the model and the workspace — and
// the token counts.
//
// Note the tree is `antigravity-cli/`, not the `antigravity/` the vendor's own
// docs advertise; the documented path has never existed on the survey machine.
//
// # What this adapter cannot know, and why
//
//   - context_pct — the token NUMERATOR is on disk but the window size is not.
//     agy's 1,048,576-token window appears nowhere in the conversation state;
//     it reaches telltale only through the statusline payload (§2.1). A
//     percentage needs both, and inventing the denominator is the assumed
//     budget decisions/001 exists to refuse. The measured numerator is surfaced
//     as a display-only extra instead.
//   - cost — consumer auth, no pricing anywhere on disk.
//   - quota — the weekly buckets are server-refreshed in memory and never
//     persisted. They exist on the statusline seam and only there.
//   - subagents — `parent_references` is a real table and was empty in every
//     conversation surveyed, with `has_subtrajectory` zero throughout. The
//     structure exists; the observation does not. Declaring the field and
//     emitting zero would assert "this session is running no sub-agents", which
//     is a claim the corpus cannot support yet (decisions/006).
//   - liveness — `steps.status` looks like a liveness signal and is not one
//     yet: all 38 rows in the survey read DONE and no in-flight session was
//     ever sampled, so the mapping from a status code to "working now" is
//     untested. The HUD classifies age from last_activity, same as every other
//     vendor.
//
// # Traps encoded here
//
//   - The WAL sidecar is not optional. A `-wal` larger than its database has
//     been observed; reading the `.db` bytes alone reports a stale snapshot as
//     current. internal/sqlite applies the sidecar with SQLite's own recovery
//     semantics, and a sidecar that fails its checksums is dropped with a
//     diagnostic rather than trusted.
//   - `conversation_summaries.db` is a stale index — one row for four
//     conversations at survey time. Discovery enumerates the directory and
//     never consults it.
//   - Token counts are guarded by an arithmetic identity: thinking + answer
//     must equal output (15/15 in the survey). A generation that fails its own
//     self-check contributes nothing and says so; the numbers are
//     reverse-engineered from an unversioned wire format and this is what turns
//     that from a guess into a checked reading.
//   - The dedup key for a generation is the per-generation response id at
//     `#1.#4.#11`, NEVER the top-level `#4` UUID, which is constant for the
//     whole conversation and would collapse every generation into one.
//   - PII. Transcript `content`/`thinking` and the request blobs carry full
//     prompt text and file contents, and the account email appears in the
//     vendor's own log. This adapter reads structural fields only. Nothing
//     model-authored or user-authored reaches a Session field, a diagnostic, or
//     a log line — which is also why the session's NAME is its conversation
//     id: it is the only label on disk that is not somebody's prompt.
package antigravity

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
	"github.com/sanlee-ys/telltale/internal/sqlite"
)

// Vendor is the stable id for rows this adapter produces.
const Vendor = model.VendorAntigravity

// ErrNoTranscript reports a conversation with no transcript file. It mirrors
// the Gemini adapter's ErrSubagentTranscript: not an error condition, a row
// the HUD drops. A conversation directory can exist before the CLI has written
// anything, and a row assembled from a database alone would have no activity
// signal at all.
var ErrNoTranscript = errors.New("antigravity: conversation has no transcript")

// Read budget for the transcript, mirroring the other adapters. The head
// carries the opening steps and the tail the newest ones; the observed
// transcripts are far smaller than either window.
const (
	headBytes int64 = 64 << 10
	tailBytes int64 = 256 << 10
)

// maxDBBytes caps how much of a conversation database this adapter will hold
// in memory. The observed corpus runs 150–320 KiB; the cap exists so a
// pathological or hostile file cannot turn a poll tick into an allocation
// storm. Over the cap the database fields degrade and the transcript-sourced
// row still renders.
const maxDBBytes = 64 << 20

// futureSkew mirrors the other adapters: a timestamp meaningfully ahead of the
// observation clock has no readable age and degrades to absent rather than
// rendering "0s".
const futureSkew = 2 * time.Second

// nameLen is how much of the conversation id becomes the session's display
// name. Eight hex characters is the vendor's own short form and matches the
// id8 the Gemini CLI embeds in a session filename; the full id is always on
// the detail pane's `session` line.
const nameLen = 8

// Protobuf field numbers from docs/design.md §3.8. They are
// reverse-engineered from a live corpus, unversioned, and every value read
// through them is either a string that is displayed verbatim or a number that
// must pass the token invariant before it renders.
const (
	fieldGeneration  = 1  // gen_metadata: one per model call
	fieldTokens      = 4  // generation: the token counts submessage
	fieldModelID     = 19 // generation: "gemini-3.6-flash"
	fieldModelName   = 21 // generation: "Gemini 3.6 Flash (High)"
	fieldTokIn       = 2  // tokens: uncached input
	fieldTokOut      = 3  // tokens: total output
	fieldTokThinking = 9  // tokens: thinking half of the output
	fieldTokAnswer   = 10 // tokens: answer half of the output
	fieldTokRespID   = 11 // tokens: per-generation response id — the dedup key

	fieldWorkspace     = 1 // trajectory blob: workspace submessage
	fieldWorkspaceURI  = 1 // …containing a file:/// URI
	fieldWorkspaceFlat = 7 // trajectory blob: the same URI, unwrapped
)

// Table names, discovered from sqlite_master rather than assumed.
const (
	tableGenMetadata = "gen_metadata"
	tableTrajectory  = "trajectory_metadata_blob"
)

// verifiedAgainst names the vendor build this adapter's field map was surveyed
// against (see the package doc). Nothing on disk states the writer's version, so
// a drift report here carries the pin and no observed counterpart.
const verifiedAgainst = "agy 1.1.9"

// The two tables every conversation database carried in the survey. Their names
// are the whole contract: the protobuf field numbers below are unversioned
// guesses promoted to a schema by a self-check, but a table that is not there at
// all is not a guess — it is the shape having moved.
var (
	canaryGenMetadata = drift.Canary{
		Name:  tableGenMetadata + " table",
		Feeds: model.NewFieldSet(model.FieldModel),
	}
	canaryTrajectory = drift.Canary{
		Name:  tableTrajectory + " table",
		Feeds: model.NewFieldSet(model.FieldWorkspace),
	}
)

// Adapter reads Antigravity CLI conversations. It holds no mutable state and
// is safe for concurrent use.
type Adapter struct {
	// root is the CLI's data directory, ~/.gemini/antigravity-cli.
	root string
}

// New returns an adapter rooted at the user's Antigravity CLI directory.
//
// There is no environment override: §3.8 documents none, and inventing one
// would mean shipping a configuration knob nobody can verify against the
// vendor. Tests use NewWithRoot.
func New() *Adapter {
	home, err := os.UserHomeDir()
	if err != nil {
		return &Adapter{}
	}
	return &Adapter{root: filepath.Join(home, ".gemini", "antigravity-cli")}
}

// NewWithRoot points the adapter at an explicit data directory. Tests use it.
func NewWithRoot(root string) *Adapter { return &Adapter{root: root} }

// Root is the directory this adapter watches, for the HUD's empty state.
func (a *Adapter) Root() string { return a.root }

func (a *Adapter) Vendor() model.VendorID { return Vendor }

// Capabilities is static. See the package doc for why five fields are absent.
//
// Everything declared is REPORTED, nothing is DERIVED: the model string and
// the workspace URI are read verbatim from the vendor's own blobs (the URI's
// conversion to a native path is a unit conversion, not a computation), the
// name is the vendor's conversation id, and last_activity is the fresher of
// two signals the vendor itself wrote.
func (a *Adapter) Capabilities() model.Capabilities {
	return model.Capabilities{
		Reported: model.NewFieldSet(
			model.FieldName,
			model.FieldModel,
			model.FieldWorkspace,
			model.FieldLastActivity,
		),
	}
}

// Discover lists conversation databases. Directory listing and stat only.
//
// The walk is conversations/*.db at fixed depth. `conversation_summaries.db`
// lives one level up and is never read: it is an INDEX, and at survey time it
// held one row for four conversations, so trusting it would have hidden three
// sessions that were right there on disk.
//
// The `-wal` and `-shm` sidecars do not match the `.db` suffix and so are
// filtered by construction; their mtimes are folded into the freshness hint,
// because on a live conversation the sidecar is the file being written and the
// database's own mtime can be minutes stale.
func (a *Adapter) Discover(ctx context.Context) ([]model.SessionRef, error) {
	if a.root == "" {
		return nil, model.ErrVendorAbsent
	}
	dir := filepath.Join(a.root, "conversations")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, model.ErrVendorAbsent
	}
	if err != nil {
		return nil, err
	}

	// One pass for the databases, one lookup table for the sidecars: both come
	// from the single directory listing already in hand.
	sidecar := map[string]time.Time{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".db-wal") {
			continue
		}
		if info, err := e.Info(); err == nil {
			sidecar[strings.TrimSuffix(name, "-wal")] = info.ModTime()
		}
	}

	var refs []model.SessionRef
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		mod := info.ModTime()
		if w, ok := sidecar[e.Name()]; ok && w.After(mod) {
			mod = w
		}
		refs = append(refs, model.SessionRef{
			Vendor:       Vendor,
			ID:           strings.TrimSuffix(e.Name(), ".db"),
			Locator:      filepath.Join(dir, e.Name()),
			LastActivity: model.TimePtr(mod),
		})
	}
	return refs, nil
}

// step is the subset of a transcript record this adapter reads. `content`,
// `thinking` and `tool_calls` are deliberately NOT in this struct: a field
// that is never decoded cannot be accidentally rendered, logged, or put in a
// diagnostic.
type step struct {
	StepIndex int    `json:"step_index"`
	Source    string `json:"source"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// generation is one decoded model call from gen_metadata.
type generation struct {
	respID   string
	modelID  string
	modelStr string
	in       int64
	out      int64
	thinking int64
	answer   int64
	hasTok   bool
}

// checks reports whether the generation's own arithmetic holds: the vendor
// splits its output into a thinking half and an answer half, and the two must
// sum to the total it also writes down. §3.8 observed this 15 times out of 15,
// which is what promotes a set of guessed field numbers to a schema — and what
// makes a failure here evidence that the guess is wrong for this row.
func (g generation) checks() bool { return g.thinking+g.answer == g.out }

// Read parses one conversation into the normalized model.
//
// Partial failure is not an error: a field that cannot be read is left nil,
// marked degraded and explained in Diagnostics, and the row still renders with
// an em dash in that cell. The transcript alone is enough for a row; the
// database only ever adds to it.
func (a *Adapter) Read(ctx context.Context, ref model.SessionRef) (*model.Session, error) {
	now := time.Now()
	s := &model.Session{
		Vendor:     Vendor,
		ID:         ref.ID,
		ObservedAt: now,
		Name:       model.Ptr(shortID(ref.ID)),
	}

	// The transcript is the session. A conversation with none is not a row:
	// there is no activity signal for it and never was.
	tpath := a.transcriptPath(ref.ID)
	tf, err := os.Open(tpath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoTranscript
	}
	if err != nil {
		return nil, err
	}
	defer tf.Close()
	tinfo, err := tf.Stat()
	if err != nil {
		return nil, err
	}

	newestStep, torn := a.readTranscript(tf, tinfo.Size())
	if torn > 0 {
		s.Diagnostics = append(s.Diagnostics, plural(torn, "unparseable transcript record skipped", "unparseable transcript records skipped"))
	}

	// §6 Q8, pinned per adapter: last_activity is the fresher of the file
	// mtimes and the newest step timestamp. Each signal passes the future-skew
	// guard on its own; only if every one of them is unreadable does the field
	// degrade. The mtime side folds in the database and its sidecar because on
	// a live conversation those are the files actually being written — and on
	// Windows the database's own mtime can lag the writer by minutes (§3.4).
	mtime, mtimeOK := newest(now,
		tinfo.ModTime(),
		modTime(ref.Locator),
		modTime(ref.Locator+"-wal"),
	)

	// The watch's unit is the conversation database: one opened database is one
	// well-formed unit examined, and a database that would not open is no
	// evidence about the vendor's shape at all.
	w := drift.NewWatch(verifiedAgainst, canaryGenMetadata, canaryTrajectory)
	opened := 0

	db, dbErr := a.readDatabase(ref.Locator)
	switch {
	case errors.Is(dbErr, fs.ErrNotExist):
		// The database vanished between Discover and Read. With no database
		// there is no session to speak of, even though the transcript survived.
		return nil, model.ErrSessionGone
	case dbErr != nil:
		s.Degraded = s.Degraded.With(model.FieldModel).With(model.FieldWorkspace)
		s.Diagnostics = append(s.Diagnostics, "conversation database unreadable: "+dbErr.Error())
	default:
		opened = 1
		a.applyDatabase(s, db, w)
	}

	switch {
	case mtimeOK && newestStep.After(mtime):
		s.LastActivity = model.TimePtr(newestStep)
	case mtimeOK:
		s.LastActivity = model.TimePtr(mtime)
	case !newestStep.IsZero():
		s.LastActivity = model.TimePtr(newestStep)
	default:
		s.Degraded = s.Degraded.With(model.FieldLastActivity)
		s.Diagnostics = append(s.Diagnostics,
			"no readable activity timestamp (mtimes ahead of the clock, no step timestamps)")
	}

	// Last, because the verdict reads what the session managed to source.
	w.Fold(s, opened)

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

func (a *Adapter) transcriptPath(id string) string {
	return filepath.Join(a.root, "brain", id, ".system_generated", "logs", "transcript.jsonl")
}

// readTranscript returns the newest step timestamp and the number of records
// that did not parse.
//
// Only `created_at` is used. The steps' status codes are read into the struct
// and deliberately not acted on: §3.8 saw DONE on every one of them and never
// sampled an in-flight session, so mapping a status to a liveness claim would
// be a guess dressed as a vendor signal.
func (a *Adapter) readTranscript(f *os.File, size int64) (newestStep time.Time, bad int) {
	now := time.Now()

	// Head and tail must not overlap, or records in the overlap parse twice
	// and inflate the unparseable count (the same rule as the Claude adapter).
	headWindow := headBytes
	if gap := size - tailBytes; gap < headWindow {
		headWindow = gap
	}
	var recs [][]byte
	if headWindow > 0 {
		head, err := jsonl.Head(f, headWindow)
		if err != nil {
			return time.Time{}, bad
		}
		recs = append(recs, head...)
	}
	tail, err := jsonl.Tail(f, size, tailBytes)
	if err != nil {
		return newestStep, bad
	}
	recs = append(recs, tail...)

	for _, raw := range recs {
		var st step
		if err := json.Unmarshal(raw, &st); err != nil {
			bad++
			continue
		}
		if st.CreatedAt == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, st.CreatedAt)
		if err != nil {
			bad++
			continue
		}
		if ts.After(newestStep) && !ts.After(now.Add(futureSkew)) {
			newestStep = ts
		}
	}
	return newestStep, bad
}

// readDatabase reads the conversation database and its sidecar as BYTES.
//
// The file is never opened for write and never locked: telltale reads vendor
// state and a monitor that can wedge the thing it monitors is not a monitor.
// SQLite's own file format is the contract being read here, not its library.
//
// The two files cannot be read atomically, so a checkpoint landing between
// them would pair a new database with a sidecar describing the old one. The
// loop re-reads when either file changed underneath it; past that, the
// defenses are structural — the WAL's per-frame checksums, and the token
// invariant that gates every number this adapter renders.
func (a *Adapter) readDatabase(path string) (*sqlite.File, error) {
	const attempts = 3
	var lastErr error
	for i := 0; i < attempts; i++ {
		before, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if before.Size() > maxDBBytes {
			return nil, errors.New("conversation database is larger than the read budget")
		}
		db, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		wal, err := os.ReadFile(path + "-wal")
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			// A sidecar that exists and cannot be read is not the same as no
			// sidecar: the database bytes alone may be stale.
			return nil, err
		}
		after, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if after.Size() == before.Size() && after.ModTime().Equal(before.ModTime()) {
			return sqlite.Open(db, wal)
		}
		lastErr = errors.New("conversation database changed mid-read")
	}
	return nil, lastErr
}

// applyDatabase folds the database's two contributions into the session: the
// model and the workspace, plus the token totals as display-only extras.
func (a *Adapter) applyDatabase(s *model.Session, db *sqlite.File, w *drift.Watch) {
	s.Diagnostics = append(s.Diagnostics, db.Notes()...)

	a.applyGenerations(s, db, w)
	a.applyWorkspace(s, db, w)
}

// applyGenerations reads gen_metadata: the model, and the token totals.
//
// Absence and failure are different. A conversation with no gen_metadata rows
// has not called a model yet — the field is nil and nothing is degraded. A
// gen_metadata table that exists and cannot be walked is a broken read and
// says so.
func (a *Adapter) applyGenerations(s *model.Session, db *sqlite.File, w *drift.Watch) {
	rows, ok, err := db.Table(tableGenMetadata)
	if err != nil {
		s.Degraded = s.Degraded.With(model.FieldModel)
		s.Diagnostics = append(s.Diagnostics, "generation metadata unreadable: "+err.Error())
		return
	}
	if !ok {
		// No such table: the shape moved. The canary owns the wording and the
		// degradation from here, so the row says which version it was verified
		// against rather than only that a table it wanted was missing.
		return
	}
	w.Saw(canaryGenMetadata)

	seen := map[string]bool{}
	var gens []generation
	for _, r := range rows {
		blob, ok := firstBlob(r)
		if !ok {
			continue
		}
		for i, raw := range messages(blob, fieldGeneration) {
			g := decodeGeneration(raw)
			key := g.respID
			if key == "" {
				// No response id to dedup on. Fall back to a key that is at
				// least unique within this file, so one row cannot be counted
				// twice — but never merge two unidentified generations, which
				// would silently drop a real model call.
				key = "row:" + itoa(r.RowID) + ":" + itoa(int64(i))
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			gens = append(gens, g)
		}
	}

	// The newest generation's model is the session's model: a conversation can
	// switch models mid-run and the last call is the one in force.
	for i := len(gens) - 1; i >= 0; i-- {
		id, name := gens[i].modelID, gens[i].modelStr
		if id == "" && name == "" {
			continue
		}
		s.Model = &model.Model{ID: id, DisplayName: name}
		break
	}

	var in, out int64
	counted, failed := 0, 0
	for _, g := range gens {
		if !g.hasTok {
			continue
		}
		if !g.checks() {
			// The self-check failed, so the field numbers do not mean here
			// what they meant in the survey. Rendering the number anyway is
			// exactly what decisions/001 forbids.
			failed++
			continue
		}
		in += g.in
		out += g.out
		counted++
	}
	if failed > 0 {
		s.Diagnostics = append(s.Diagnostics,
			plural(failed, "generation failed its token self-check and was dropped",
				"generations failed their token self-check and were dropped"))
	}
	if counted > 0 {
		// Display-only extras, never gauges: these are token COUNTS with no
		// window to divide by (see the package doc). "uncached in" is named
		// that way because the cache-read component sits at a field number
		// §3.8 marks lower-confidence, and this adapter will not fold an
		// uncertain number into a certain one to make a rounder total.
		setExtra(s, "uncached in", formatTokens(in))
		setExtra(s, "output", formatTokens(out))
		setExtra(s, "generations", itoa(int64(counted)))
	}
}

// decodeGeneration pulls one generation's fields out of its protobuf blob.
func decodeGeneration(raw []byte) generation {
	var g generation
	if v, ok := lastString(raw, fieldModelID); ok {
		g.modelID = v
	}
	if v, ok := lastString(raw, fieldModelName); ok {
		g.modelStr = v
	}
	toks := messages(raw, fieldTokens)
	if len(toks) == 0 {
		return g
	}
	// The last token submessage wins, for the same reason the last string
	// does: a re-written field is the vendor correcting itself.
	t := toks[len(toks)-1]
	g.hasTok = true
	if v, ok := lastVarint(t, fieldTokIn); ok {
		g.in = int64(v)
	}
	if v, ok := lastVarint(t, fieldTokOut); ok {
		g.out = int64(v)
	}
	if v, ok := lastVarint(t, fieldTokThinking); ok {
		g.thinking = int64(v)
	}
	if v, ok := lastVarint(t, fieldTokAnswer); ok {
		g.answer = int64(v)
	}
	if v, ok := lastString(t, fieldTokRespID); ok {
		g.respID = v
	}
	return g
}

// applyWorkspace reads the trajectory metadata blob's workspace URI.
//
// The vendor writes it twice — nested at #1.#1 and flat at #7 — and this reads
// whichever it finds. A conversation started outside any workspace carries
// neither, which is absence, not degradation: the survey saw exactly that.
func (a *Adapter) applyWorkspace(s *model.Session, db *sqlite.File, w *drift.Watch) {
	rows, ok, err := db.Table(tableTrajectory)
	if err != nil {
		s.Degraded = s.Degraded.With(model.FieldWorkspace)
		s.Diagnostics = append(s.Diagnostics, "trajectory metadata unreadable: "+err.Error())
		return
	}
	if !ok {
		// Same as applyGenerations: the canary owns this case.
		return
	}
	w.Saw(canaryTrajectory)
	for _, r := range rows {
		blob, ok := firstBlob(r)
		if !ok {
			continue
		}
		uri := ""
		for _, sub := range messages(blob, fieldWorkspace) {
			if v, ok := lastString(sub, fieldWorkspaceURI); ok && strings.HasPrefix(v, "file:") {
				uri = v
			}
		}
		if uri == "" {
			if v, ok := lastString(blob, fieldWorkspaceFlat); ok && strings.HasPrefix(v, "file:") {
				uri = v
			}
		}
		if uri == "" {
			continue
		}
		if p := pathFromFileURI(uri); p != "" {
			s.WorkspaceDir = model.Ptr(p)
			return
		}
	}
}

// pathFromFileURI converts a file:/// URI to a native absolute path.
//
// This is a unit conversion, not an inference — the same class of operation as
// the statusline turning a remaining fraction into a used percentage — so the
// field stays REPORTED. A URI this function does not recognize yields the
// empty string and the field stays absent; a half-converted path would name a
// directory that does not exist.
func pathFromFileURI(uri string) string {
	rest, ok := strings.CutPrefix(uri, "file://")
	if !ok {
		return ""
	}
	// file://host/share/... is a UNC path; file:///C:/x and file:///home/x
	// both leave a leading slash here.
	if !strings.HasPrefix(rest, "/") {
		return ""
	}
	p, err := percentDecode(rest)
	if err != nil {
		return ""
	}
	// A Windows drive letter arrives as "/C:/Users/..."; strip the slash the
	// URI form requires. A POSIX path keeps its root.
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}

// percentDecode expands %XX escapes. net/url would do this, but it also
// normalizes, re-encodes and carries opinions about what a path may contain;
// this is twenty lines that do exactly one thing.
func percentDecode(s string) (string, error) {
	if !strings.Contains(s, "%") {
		return s, nil
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", errors.New("truncated percent escape")
		}
		hi, ok1 := unhex(s[i+1])
		lo, ok2 := unhex(s[i+2])
		if !ok1 || !ok2 {
			return "", errors.New("bad percent escape")
		}
		b.WriteByte(hi<<4 | lo)
		i += 2
	}
	return b.String(), nil
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

// firstBlob returns the row's first BLOB column.
//
// Position rather than name: this reader does not parse CREATE statements, and
// both tables it reads carry exactly one blob column (`data`). Taking the
// first blob is stable under a column being added or reordered, which a
// hard-coded index is not.
func firstBlob(r sqlite.Row) ([]byte, bool) {
	for _, v := range r.Values {
		if b, ok := v.Blob(); ok && len(b) > 0 {
			return b, true
		}
	}
	return nil, false
}

// newest returns the freshest of several file mtimes, skipping any that is
// ahead of the observation clock (a file mtime from a machine whose clock ran
// fast has no readable age) and any that is the zero time (the file is
// absent).
func newest(now time.Time, times ...time.Time) (time.Time, bool) {
	var best time.Time
	ok := false
	for _, t := range times {
		if t.IsZero() || t.After(now.Add(futureSkew)) {
			continue
		}
		if !ok || t.After(best) {
			best, ok = t, true
		}
	}
	return best, ok
}

// modTime is a stat that reports absence as the zero time. A missing sidecar
// is the normal case for a checkpointed conversation.
func modTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// shortID is the conversation id trimmed for the grid. The vendor writes no
// human title anywhere a public repo may read (the only free text on disk is
// prompt content), so the id IS the label — and the full one is on the detail
// pane's session line, one keystroke away.
func shortID(id string) string {
	if len(id) <= nameLen {
		return id
	}
	return id[:nameLen]
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
	neg := n < 0
	if neg {
		n = -n
	}
	var b [21]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
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
