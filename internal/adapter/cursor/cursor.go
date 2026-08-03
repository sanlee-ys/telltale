// Package cursor adapts Cursor's Composer agent state to model.Session.
//
// Cursor is closed source and its store format is undocumented and unversioned,
// so this adapter follows the Claude Code and Antigravity precedent: a live
// read-only survey, cross-checked field by field, with every reading traceable
// to something the vendor itself wrote down. The survey, the field map with its
// MEASURED / PARTIAL / ABSENT labels, and the build cautions are docs/design.md
// §3.9 (Cursor 3.14.7, 2026-08-02); the decision is decisions/007. Nothing is
// claimed here that §3.9 did not observe.
//
// # Layout
//
//	%APPDATA%\Cursor\User\                        (os.UserConfigDir()/Cursor/User)
//	  globalStorage/state.vscdb                   SQLite: ONE store, every session
//	  globalStorage/state.vscdb-wal               where the newest state actually is
//	  workspaceStorage/<workspace-id>/workspace.json
//
// Two tables in that store matter. `composerHeaders` is one row per Composer
// session — `composerId`, `workspaceId`, the timestamps, `isArchived`,
// `isSubagent`, and a `value` JSON blob carrying the session's title.
// `cursorDiskKV` is a key/value table in which the keys `composerData:<id>`
// hold per-session state: the model and the vendor's own context-usage
// percentage.
//
// # The credential rule, which outranks every field on the map
//
// The SAME FILE holds live credentials — access and refresh tokens, MCP OAuth
// secrets, git-IPC auth tokens — and the per-session blobs hold encryption
// keys. A monitor that reads a vendor's whole store because the data it wants
// happens to live there is a credential-exfiltration path with a friendly UI.
// So this adapter reads a fixed allowlist and nothing else (decisions/007):
//
//   - `composerHeaders`, the eight named columns and two named `value` fields;
//   - `cursorDiskKV` rows whose key has the exact prefix `composerData:` AND
//     whose id belongs to a session that survived filtering, and within those
//     rows four named JSON fields;
//   - `workspaceStorage/<id>/workspace.json`.
//
// It never touches `ItemTable`, where the tokens live. It never reads
// `bubbleId:*` or `ofsContent:*` — the message payloads. Nothing from
// `value.subtitle`, message text, todos or tool-call payloads reaches a
// Session field, an Extra, a Diagnostic or a log line.
//
// The one honest caveat, stated rather than buried: a b-tree walk visits the
// rows of the table it walks, so filtering `cursorDiskKV` by key prefix happens
// after a row is decoded, not before — there is no index to seek with.
// sqlite.Rows exists so that walk RETAINS nothing: a row that is not a
// `composerData:` key for a kept session is looked at and dropped, never
// copied out, never held. `ItemTable` is not walked at all.
//
// # What this adapter cannot know, and why
//
//   - cost — `usageData` was observed `{}` in every session and the per-message
//     `tokenCount.inputTokens`/`outputTokens` were 0 in all 310 message rows
//     surveyed: the schema is present and never populated. A zero that really
//     means "unpopulated" must not render as $0.00 or as 0 tokens, which is the
//     honest-gauge rule's exact failure mode, so the field is CapNone.
//   - quota — no consumption record exists on disk at all. The store does hold
//     plan ENTITLEMENT constants (`credit_dollars`, `included_usage_dollars`);
//     those are what the plan grants, not what has been used, and rendering
//     them in a quota gauge would be a lie with a number on it. This adapter
//     does not read them.
//   - subagents — `isSubagent`, `numSubComposers` and `subComposerIds` are all
//     structural: zero and empty everywhere in the survey. Declaring the field
//     and emitting zero would assert "this session is running no sub-agents",
//     which the corpus cannot support. The fields are still used defensively —
//     a row marked `isSubagent` is dropped from top-level discovery.
//   - liveness — `status`, `generatingBubbleIds` and `hasBlockingPendingActions`
//     look like liveness and needs-input signals and no in-flight session was
//     ever sampled, so the mapping is untested. The HUD classifies age from
//     last_activity, same as every other vendor. §3.9 records the documented
//     Cursor Hooks payload as the seam where that becomes buildable.
//
// # Traps encoded here
//
//   - THE WAL IS WHERE THE DATA IS. This is not the usual "read the sidecar
//     too" caution: workspace-level `state.vscdb` files were observed at 4096
//     bytes — one page, empty — with every byte of content in a 300–500 KB
//     `-wal`. A reader that opens only the `.db` sees an empty database and
//     reports nothing at all. internal/sqlite overlays the sidecar with
//     SQLite's own recovery semantics; a fixture reproduces the shape.
//   - `ItemTable['composer.composerHeaders']` is a legacy JSON mirror of the
//     `composerHeaders` table and it is STALE: at survey time it named 3
//     sessions to the table's 9, and all three were the empty/draft ones. The
//     table wins. (It is also in `ItemTable`, so the credential rule forbids it
//     independently.)
//   - Most rows are not sessions. 5 of 9 in the survey were drafts, archived
//     threads, or the pre-created composers a new window makes before anyone
//     types. The filter is load-bearing, not hygiene.
//   - `modelConfig.modelName` is sometimes the literal string `default`. That
//     is an unresolved alias, and it renders verbatim: resolving it to a model
//     name would mean guessing which model the vendor's server picked.
//   - One store, many sessions — so the store's file mtime dates the STORE and
//     not any session in it, and folding it into last_activity would report
//     every Cursor row as active whenever Cursor wrote anything. The Q8 fold
//     runs over the per-row timestamps only. See lastActivity.
package cursor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/sqlite"
)

// Vendor is the stable id for rows this adapter produces.
const Vendor = model.VendorCursor

// Store layout, relative to the adapter's root.
const (
	globalStorage    = "globalStorage"
	storeFile        = "state.vscdb"
	workspaceStorage = "workspaceStorage"
	workspaceFile    = "workspace.json"
)

// The two tables read, and the one key prefix. `ItemTable` is deliberately
// absent from this file: the name of the table holding the tokens should not
// appear in a reader that must never walk it.
const (
	tableHeaders = "composerHeaders"
	tableDiskKV  = "cursorDiskKV"
	dataPrefix   = "composerData:"
)

// composerHeaders columns, addressed by NAME. Positional indices would keep
// parsing and start meaning something else the first time Cursor adds a column
// to an unversioned schema.
const (
	colComposerID    = "composerId"
	colWorkspaceID   = "workspaceId"
	colLastUpdatedAt = "lastUpdatedAt"
	colIsArchived    = "isArchived"
	colIsSubagent    = "isSubagent"
	colRecency       = "recency"
	colCheckpointAt  = "checkpointAt"
	colValue         = "value"
)

// Sentinels the store itself uses for things that are not sessions.
const (
	// emptyWindow is the workspaceId of a Cursor window with no folder open.
	// Those rows have no workspace to name and no work attached to them.
	emptyWindow = "empty-window"
	// draftComposer is the fixed composerId of the empty-state draft — the
	// composer backing the "start a new chat" box, one per install.
	draftComposer = "empty-state-draft"
)

// maxStoreBytes caps how much of the store this adapter will hold in memory.
// The surveyed store was 9 MB with a 4.6 MB sidecar; heavy use grows both. The
// cap exists so a pathological file cannot turn a one-second poll tick into an
// allocation storm — over it the vendor reports unreadable, with the reason.
const maxStoreBytes = 256 << 20

// futureSkew mirrors the other adapters: a timestamp meaningfully ahead of the
// observation clock has no readable age and degrades to absent rather than
// rendering "0s".
const futureSkew = 2 * time.Second

// nameLen is how much of the composerId becomes the display name when the
// vendor wrote no title. Eight hex characters, matching the agy adapter.
const nameLen = 8

// Adapter reads Cursor's Composer state. Safe for concurrent use.
type Adapter struct {
	// root is Cursor's user-data directory, %APPDATA%\Cursor\User on Windows.
	root string

	// One store backs every session, so the parse is per-STORE, not per
	// session: Discover loads it and the Reads that follow reuse that load.
	// Re-reading 14 MB five times a second to answer five questions about one
	// file would be the adapter's own fault, not the vendor's.
	mu    sync.Mutex
	cache *snapshot
}

// New returns an adapter rooted at Cursor's user-data directory.
//
// os.UserConfigDir is exactly right on all three platforms — %APPDATA% on
// Windows, ~/Library/Application Support on macOS, ~/.config elsewhere — which
// is why there is no environment override to get wrong. Tests use NewWithRoot.
func New() *Adapter {
	dir, err := os.UserConfigDir()
	if err != nil {
		return &Adapter{}
	}
	return &Adapter{root: filepath.Join(dir, "Cursor", "User")}
}

// NewWithRoot points the adapter at an explicit user-data directory.
func NewWithRoot(root string) *Adapter { return &Adapter{root: root} }

// Root is the directory this adapter watches, for the HUD's empty state.
func (a *Adapter) Root() string { return a.root }

func (a *Adapter) Vendor() model.VendorID { return Vendor }

// Capabilities is static.
//
// context_pct is declared DERIVED and not reported, and the asymmetry is
// deliberate. Cursor persists its own `contextUsagePercent` — that value is
// read verbatim and rendered without an estimate marker — but when it is
// missing and the raw token counts are not, the adapter computes the
// percentage instead, and a computed value must be marked. Capabilities is a
// static per-vendor declaration with no way to say "reported when the vendor
// wrote it down, derived when it did not", and the two are disjoint by
// construction, so the weaker of the two claims is the safe one to publish:
// the HUD never promises more than the adapter can guarantee, and Session's
// per-read Derived set tells the truth row by row.
func (a *Adapter) Capabilities() model.Capabilities {
	return model.Capabilities{
		Reported: model.NewFieldSet(
			model.FieldName,
			model.FieldModel,
			model.FieldWorkspace,
			model.FieldLastActivity,
		),
		Derived: model.NewFieldSet(model.FieldContextPercent),
	}
}

// record is one Composer session as the store describes it. Everything this
// adapter will ever surface about a session is in this struct — which is the
// point: the fields that are not here (subtitle, message text, todos, tool
// calls, encryption keys) are never decoded, and a field that is never decoded
// cannot be accidentally rendered.
type record struct {
	id           string
	name         string    // vendor title; empty when the session has none
	workspaceID  string    // "" when the row named none
	lastActivity time.Time // zero when no row timestamp was readable
	modelName    string    // "" when no composerData row, or none named
	pct          float64
	hasPct       bool
	pctDerived   bool
	tokensUsed   float64
	tokenLimit   float64
	hasTokens    bool
}

// snapshot is one parse of the store, plus the file identity it was parsed
// from. A tick on which neither file moved reuses it and does no I/O beyond
// two stats.
type snapshot struct {
	mainSize int64
	mainMod  time.Time
	walSize  int64
	walMod   time.Time
	base     []byte // the main file's bytes, reused while the main file is unchanged
	recs     []record
	notes    []string
}

func (s *snapshot) find(id string) (record, bool) {
	for _, r := range s.recs {
		if r.id == id {
			return r, true
		}
	}
	return record{}, false
}

func (a *Adapter) storePath() string {
	return filepath.Join(a.root, globalStorage, storeFile)
}

// Discover lists the Composer sessions in the store.
//
// This is the one adapter whose Discover cannot be stat-only: there is no
// directory of sessions to list, only rows inside a single database. The cost
// is paid once per tick and shared with the Reads that follow (see Adapter.mu),
// and a tick on which the store did not move costs two stats.
func (a *Adapter) Discover(ctx context.Context) ([]model.SessionRef, error) {
	if a.root == "" {
		return nil, model.ErrVendorAbsent
	}
	snap, err := a.load()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	path := a.storePath()
	refs := make([]model.SessionRef, 0, len(snap.recs))
	for _, r := range snap.recs {
		ref := model.SessionRef{Vendor: Vendor, ID: r.id, Locator: path}
		if !r.lastActivity.IsZero() {
			ref.LastActivity = model.TimePtr(r.lastActivity)
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// Read assembles one session from the store parse Discover already did.
//
// Partial failure is not an error: a field that cannot be read is left nil,
// marked degraded and explained in Diagnostics, and the row still renders with
// an em dash in that cell.
func (a *Adapter) Read(ctx context.Context, ref model.SessionRef) (*model.Session, error) {
	snap, err := a.load()
	if err != nil {
		return nil, err
	}
	rec, ok := snap.find(ref.ID)
	if !ok {
		// The session was archived, deleted or filtered between Discover and
		// Read. The HUD drops the row silently.
		return nil, model.ErrSessionGone
	}

	s := &model.Session{
		Vendor:     Vendor,
		ID:         rec.id,
		ObservedAt: time.Now(),
		Name:       model.Ptr(displayName(rec)),
	}
	s.Diagnostics = append(s.Diagnostics, snap.notes...)

	if rec.modelName != "" {
		// The id IS the display name here: Cursor writes one string
		// (`composer-2.5`, `grok-4.5`, `default`) and no prettier form of it.
		// `default` is rendered exactly as the vendor wrote it — it is an
		// unresolved alias for whatever the server picks, and resolving it
		// would mean naming a model nobody recorded.
		s.Model = &model.Model{ID: rec.modelName, DisplayName: rec.modelName}
	}

	if rec.hasPct {
		s.ContextPercent = model.PercentPtr(rec.pct)
		if rec.pctDerived {
			s.Derived = s.Derived.With(model.FieldContextPercent)
		}
	}
	if rec.hasTokens {
		setExtra(s, "ctx tokens", formatTokens(rec.tokensUsed)+" / "+formatTokens(rec.tokenLimit))
	}

	a.applyWorkspace(s, rec)

	if rec.lastActivity.IsZero() {
		s.Degraded = s.Degraded.With(model.FieldLastActivity)
		s.Diagnostics = append(s.Diagnostics,
			"no readable activity timestamp on this session's header row")
	} else {
		s.LastActivity = model.TimePtr(rec.lastActivity)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

// displayName is the session's label: the vendor's own generated title when it
// wrote one, and the head of the composerId when it did not.
//
// The title is the one user-adjacent string this adapter surfaces, and it is
// the same class of value as the Claude session summaries the HUD already
// shows: vendor-generated, a few words long, written to be a label. The
// session's `subtitle` — which lists the files a turn touched — is NOT.
func displayName(r record) string {
	if r.name != "" {
		return r.name
	}
	if len(r.id) > nameLen {
		return r.id[:nameLen]
	}
	return r.id
}

// applyWorkspace resolves workspaceId through workspaceStorage.
//
// A row that named no workspace, or one whose workspace.json is gone (Cursor
// prunes them), has no path to show: that is ABSENCE and the session still
// renders. A workspace.json that exists and cannot be read or parsed is a
// failed read and is marked degraded, because "we could not read it" and "there
// is nothing to read" are different facts.
func (a *Adapter) applyWorkspace(s *model.Session, r record) {
	if r.workspaceID == "" {
		return
	}
	path := filepath.Join(a.root, workspaceStorage, r.workspaceID, workspaceFile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		s.Degraded = s.Degraded.With(model.FieldWorkspace)
		s.Diagnostics = append(s.Diagnostics, "workspace mapping unreadable: "+err.Error())
		return
	}
	var ws struct {
		Folder string `json:"folder"`
	}
	if err := json.Unmarshal(raw, &ws); err != nil {
		s.Degraded = s.Degraded.With(model.FieldWorkspace)
		s.Diagnostics = append(s.Diagnostics, "workspace mapping did not parse as JSON")
		return
	}
	if ws.Folder == "" {
		// A multi-root or otherwise folderless workspace names no single
		// directory. Absence, not failure.
		return
	}
	if p := pathFromFileURI(ws.Folder); p != "" {
		s.WorkspaceDir = model.Ptr(p)
	}
}

// load returns a parse of the store, reusing the cached one when neither the
// database nor its sidecar has moved.
func (a *Adapter) load() (*snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	path := a.storePath()
	main, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, model.ErrVendorAbsent
	}
	if err != nil {
		return nil, err
	}
	walSize, walMod := statOrZero(path + "-wal")

	if c := a.cache; c != nil &&
		c.mainSize == main.Size() && c.mainMod.Equal(main.ModTime()) &&
		c.walSize == walSize && c.walMod.Equal(walMod) {
		return c, nil
	}

	snap, err := a.reload(main)
	if err != nil {
		return nil, err
	}
	a.cache = snap
	return snap, nil
}

// reload reads the store's bytes and parses it.
//
// The files are read, never opened for write and never locked: telltale reads
// vendor state, and a monitor that can wedge the editor it monitors is not a
// monitor. The two files cannot be read atomically, so a checkpoint landing
// between them would pair a new database with a sidecar describing the old one;
// the loop re-reads when either file changed underneath it, and past that the
// defense is structural — the WAL's per-frame checksums, which reject a torn
// sidecar rather than applying half of it.
func (a *Adapter) reload(first os.FileInfo) (*snapshot, error) {
	const attempts = 3
	path := a.storePath()
	before := first
	var lastErr error

	for i := 0; i < attempts; i++ {
		if before.Size() > maxStoreBytes {
			return nil, errors.New("cursor: state store is larger than the read budget")
		}

		// The main file changes only on a checkpoint, so its bytes are cached
		// across ticks; the sidecar is the file being written and is re-read.
		base := a.reuseBase(before)
		if base == nil {
			b, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			base = b
		}
		wal, err := os.ReadFile(path + "-wal")
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			// A sidecar that exists and cannot be read is not the same as no
			// sidecar: on this vendor the sidecar is where the data is.
			return nil, err
		}
		if len(wal) > maxStoreBytes {
			return nil, errors.New("cursor: state store sidecar is larger than the read budget")
		}

		after, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		walSize, walMod := statOrZero(path + "-wal")
		if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
			lastErr = errors.New("cursor: state store changed mid-read")
			before = after
			continue
		}

		db, err := sqlite.Open(base, wal)
		if err != nil {
			return nil, err
		}
		snap := &snapshot{
			mainSize: after.Size(), mainMod: after.ModTime(),
			walSize: walSize, walMod: walMod,
			base:  base,
			notes: db.Notes(),
		}
		if err := a.parse(snap, db); err != nil {
			return nil, err
		}
		return snap, nil
	}
	return nil, lastErr
}

// reuseBase returns the cached main-file bytes when the main file has not
// moved since they were read.
func (a *Adapter) reuseBase(info os.FileInfo) []byte {
	c := a.cache
	if c == nil || c.base == nil {
		return nil
	}
	if c.mainSize == info.Size() && c.mainMod.Equal(info.ModTime()) {
		return c.base
	}
	return nil
}

// ErrSchemaMismatch reports a state store whose shape this adapter does not
// recognize: no `composerHeaders` table, or one missing a column the field map
// is built on.
//
// It is deliberately an ERROR and not an empty result. The store format is
// undocumented and unversioned, so the day Cursor renames a table is a day this
// adapter must say "I cannot read this" — reporting zero sessions instead would
// tell the user their agents are idle, which is a wrong answer rather than a
// missing one. The HUD renders it on the vendor line beside the store's path.
var ErrSchemaMismatch = errors.New("cursor: unrecognized state store schema")

// parse walks the two allowlisted tables.
func (a *Adapter) parse(snap *snapshot, db *sqlite.File) error {
	cols, ok, err := db.Columns(tableHeaders)
	if err != nil {
		return err
	}
	if !ok {
		return schemaErr("no " + tableHeaders + " table in the state store")
	}
	idx := index(cols)
	for _, want := range []string{colComposerID, colWorkspaceID, colValue} {
		if _, ok := idx[want]; !ok {
			return schemaErr(tableHeaders + " has no " + want + " column")
		}
	}

	kept := map[string]int{}
	if _, err := db.Rows(tableHeaders, func(r sqlite.Row) bool {
		rec, ok := headerRecord(r, idx)
		if !ok {
			return true
		}
		kept[rec.id] = len(snap.recs)
		snap.recs = append(snap.recs, rec)
		return true
	}); err != nil {
		return err
	}
	if len(kept) == 0 {
		return nil
	}

	// The second pass reads the per-session blobs. The key prefix and the kept
	// set together are the allowlist: a row that is not `composerData:` for a
	// session that survived filtering is looked at and dropped, never copied.
	dataCols, _, err := db.Columns(tableDiskKV)
	if err != nil {
		return err
	}
	kv := index(dataCols)
	keyAt, keyOK := kv["key"]
	valAt, valOK := kv["value"]
	if !keyOK || !valOK {
		snap.notes = append(snap.notes, "state store's "+tableDiskKV+" is not a key/value table")
		return nil
	}

	found := 0
	if _, err := db.Rows(tableDiskKV, func(r sqlite.Row) bool {
		if keyAt >= len(r.Values) || valAt >= len(r.Values) {
			return true
		}
		key, ok := r.Values[keyAt].Text()
		if !ok || !strings.HasPrefix(key, dataPrefix) {
			return true
		}
		at, ok := kept[key[len(dataPrefix):]]
		if !ok {
			return true
		}
		raw, ok := textOrBlob(r.Values[valAt])
		if !ok {
			return true
		}
		applyComposerData(&snap.recs[at], raw)
		found++
		return found < len(kept)
	}); err != nil {
		return err
	}
	return nil
}

// headerRecord turns one composerHeaders row into a session, or reports that
// the row is not one.
//
// Five of the nine rows in the survey were not sessions, so this filter is
// load-bearing rather than hygiene: sub-agent rows (defensive — the field was
// zero everywhere, and a fan-out is a chip on its parent, never a top-level
// row), archived threads (the Codex precedent: archived is ignored), the
// empty-state draft, the pre-created composers a window makes before anyone
// types, and rows belonging to a window with no folder open.
func headerRecord(r sqlite.Row, idx map[string]int) (record, bool) {
	id, _ := textAt(r, idx, colComposerID)
	if id == "" || id == draftComposer {
		return record{}, false
	}
	if n, ok := intAt(r, idx, colIsSubagent); ok && n != 0 {
		return record{}, false
	}
	if n, ok := intAt(r, idx, colIsArchived); ok && n != 0 {
		return record{}, false
	}
	ws, _ := textAt(r, idx, colWorkspaceID)
	if ws == emptyWindow {
		return record{}, false
	}

	rec := record{id: id, workspaceID: ws}
	if raw, ok := textAt(r, idx, colValue); ok {
		var v struct {
			Name    string `json:"name"`
			IsDraft bool   `json:"isDraft"`
		}
		// A `value` that does not parse costs the title and nothing else: the
		// columns beside it are still a session.
		if json.Unmarshal([]byte(raw), &v) == nil {
			if v.IsDraft {
				return record{}, false
			}
			rec.name = v.Name
		}
	}
	rec.lastActivity = lastActivity(r, idx, time.Now())
	return rec, true
}

// lastActivity is §6 Q8 for this vendor: the newest of the row's own activity
// timestamps, each passing the future-skew guard on its own.
//
// The fold deliberately does NOT include the store's file mtime, and that is
// the one place this adapter departs from the shape the other three use. Every
// other vendor gives a session its own file, so that file's mtime dates that
// session. Cursor gives every session ONE file: its mtime says when Cursor last
// wrote anything at all, which is continuously while the editor is open. Fold
// it in and every Cursor row reports as live forever, including the ones
// abandoned last week — a number that is always fresh and never true. Three
// per-row timestamps are available instead, and when none of them is readable
// the field degrades, which is the honest answer.
func lastActivity(r sqlite.Row, idx map[string]int, now time.Time) time.Time {
	var best time.Time
	for _, col := range []string{colLastUpdatedAt, colRecency, colCheckpointAt} {
		i, ok := idx[col]
		if !ok || i >= len(r.Values) {
			continue
		}
		t, ok := flexTime(r.Values[i])
		if !ok || t.After(now.Add(futureSkew)) {
			continue
		}
		if t.After(best) {
			best = t
		}
	}
	return best
}

// composerData is the subset of a per-session blob this adapter decodes.
//
// Everything absent from this struct is never decoded: `blobEncryptionKey`,
// `speculativeSummarizationEncryptionKey`, `richText`, `text`, `todos`,
// `conversationMap`, `context`, `subtitle`. The pointers are pointers so that a
// vendor-written zero stays distinguishable from a field the vendor omitted —
// the same rule the schema itself runs on.
type composerData struct {
	ModelConfig struct {
		ModelName string `json:"modelName"`
	} `json:"modelConfig"`
	ContextUsagePercent *float64 `json:"contextUsagePercent"`
	ContextTokensUsed   *float64 `json:"contextTokensUsed"`
	ContextTokenLimit   *float64 `json:"contextTokenLimit"`
}

// applyComposerData folds one session's blob into its record.
func applyComposerData(rec *record, raw []byte) {
	var d composerData
	if json.Unmarshal(raw, &d) != nil {
		return
	}
	rec.modelName = d.ModelConfig.ModelName

	if d.ContextTokensUsed != nil && d.ContextTokenLimit != nil && *d.ContextTokenLimit > 0 {
		rec.tokensUsed, rec.tokenLimit, rec.hasTokens = *d.ContextTokensUsed, *d.ContextTokenLimit, true
	}

	// The vendor's own percentage wins and is REPORTED: it is what Cursor
	// itself shows, computed by whoever knows what counts against the window.
	if p := d.ContextUsagePercent; p != nil && *p >= 0 && *p <= 100 {
		rec.pct, rec.hasPct, rec.pctDerived = *p, true, false
		return
	}
	// Only when it is missing does the adapter compute one — and then it is
	// marked, because used ÷ limit is this program's arithmetic and not the
	// vendor's reading.
	if rec.hasTokens {
		p := rec.tokensUsed / rec.tokenLimit * 100
		if p >= 0 && p <= 100 {
			rec.pct, rec.hasPct, rec.pctDerived = p, true, true
		}
	}
}

// ------------------------------------------------------------------ values

// schemaErr wraps a specific mismatch in the typed sentinel, so a caller can
// test for the class and a user still reads which part of the shape moved.
func schemaErr(what string) error {
	return fmt.Errorf("%w: %s", ErrSchemaMismatch, what)
}

// index maps a table's column names to their record positions.
func index(cols []string) map[string]int {
	out := make(map[string]int, len(cols))
	for i, c := range cols {
		if _, dup := out[c]; !dup {
			out[c] = i
		}
	}
	return out
}

func textAt(r sqlite.Row, idx map[string]int, col string) (string, bool) {
	i, ok := idx[col]
	if !ok || i >= len(r.Values) {
		return "", false
	}
	return r.Values[i].Text()
}

func intAt(r sqlite.Row, idx map[string]int, col string) (int64, bool) {
	i, ok := idx[col]
	if !ok || i >= len(r.Values) {
		return 0, false
	}
	if r.Values[i].Type != sqlite.Int {
		return 0, false
	}
	return r.Values[i].Int, true
}

// textOrBlob reads a column that a store may declare TEXT and store as BLOB,
// which VS Code-derived key/value tables do routinely.
func textOrBlob(v sqlite.Value) ([]byte, bool) {
	switch v.Type {
	case sqlite.Text, sqlite.Blob:
		if len(v.Bytes) == 0 {
			return nil, false
		}
		return v.Bytes, true
	}
	return nil, false
}

// flexTime reads a timestamp that may be stored either way.
//
// The store's timestamps are MIXED — the header columns are epoch
// milliseconds, and ISO-8601 UTC strings appear elsewhere in the same store
// (§3.9) — and the schema is undocumented and unversioned, so a column that is
// an integer today is not promised to be one after an update. Accepting both
// costs twenty lines and turns a silent degradation into a reading.
//
// Zero and negative are ABSENCE, not 1970: a store writing 0 means "no value".
func flexTime(v sqlite.Value) (time.Time, bool) {
	switch v.Type {
	case sqlite.Int:
		return fromMillis(float64(v.Int))
	case sqlite.Float:
		return fromMillis(v.Float)
	case sqlite.Text:
		s, _ := v.Text()
		s = strings.TrimSpace(s)
		if s == "" {
			return time.Time{}, false
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if t, err := time.Parse(layout, s); err == nil {
				return t, true
			}
		}
		if ms, err := strconv.ParseFloat(s, 64); err == nil {
			return fromMillis(ms)
		}
	}
	return time.Time{}, false
}

func fromMillis(ms float64) (time.Time, bool) {
	if ms <= 0 {
		return time.Time{}, false
	}
	sec := int64(ms) / 1000
	nsec := (int64(ms) % 1000) * int64(time.Millisecond)
	return time.Unix(sec, nsec).UTC(), true
}

// statOrZero reports a file's size and mtime, or zeroes when it is absent. A
// missing sidecar is normal on a freshly checkpointed store.
func statOrZero(path string) (int64, time.Time) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, time.Time{}
	}
	return info.Size(), info.ModTime()
}

// pathFromFileURI converts a file:/// URI to a native absolute path.
//
// This is a unit conversion, not an inference, so the field stays REPORTED. A
// URI this does not recognize yields the empty string and the field stays
// absent; a half-converted path would name a directory that does not exist.
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
	// A Windows drive letter arrives as "/c:/Users/..." — Cursor writes the
	// letter lower-cased and percent-encodes the colon. Strip the slash the
	// URI form requires and upper-case the letter, which is the form every
	// other adapter's paths and the HUD's own folder column already use.
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
		if p[0] >= 'a' && p[0] <= 'z' {
			p = string(p[0]-'a'+'A') + p[1:]
		}
	}
	return filepath.FromSlash(p)
}

// percentDecode expands %XX escapes. net/url would do this, but it also
// normalizes, re-encodes and carries opinions about what a path may contain.
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

func setExtra(s *model.Session, label, value string) {
	for i := range s.Extras {
		if s.Extras[i].Label == label {
			s.Extras[i].Value = value
			return
		}
	}
	s.Extras = append(s.Extras, model.Extra{Label: label, Value: value})
}

func formatTokens(n float64) string {
	switch {
	case n <= 0:
		return "0"
	case n < 1000:
		return strconv.FormatInt(int64(n), 10)
	case n < 1_000_000:
		return strconv.FormatInt(int64(n)/1000, 10) + "k"
	default:
		return strconv.FormatInt(int64(n)/1_000_000, 10) + "M"
	}
}

// compile-time contract check.
var _ model.Adapter = (*Adapter)(nil)
