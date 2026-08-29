package cursor

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/sanlee-ys/telltale/internal/adapter/drift"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The cursor-agent CLI session manifest, and the second store this adapter
// reads (docs/design.md §3.9's 2026-08-29 addendum).
//
// # Why there is a second reader at all
//
// §3.9 recorded on 2026-08-17 that a live `cursor-agent` CLI session drew no
// HUD row, and that this was the design rather than a defect: the Composer
// reader above opens exactly one store, the IDE's `state.vscdb`, and the CLI
// keeps its own. That observation closed a question and left a gap — the
// operator runs CLI sessions and the HUD cannot see them.
//
// The 2026-08-29 survey re-opened the CLI's tree and measured a manifest the
// 2026-08-02 survey could not have seen, because `cursor-agent` was not
// installed on this machine then:
//
//	~/.cursor/chats/<workspace-hash>/<session-uuid>/meta.json
//
// It is plain JSON, it is 124–140 bytes, and it carries a DECLARED
// `schemaVersion` — the first Cursor surface anywhere that names its own
// format version, against §3.9's standing "no changelog, no schema version in
// the file, no documentation" caution. Three of its keys map straight onto
// cells this adapter already sources from SQLite: `title` → name, `cwd` →
// workspace, `updatedAtMs` → last activity.
//
// # A sibling reader the Adapter composes, not a second adapter
//
// Both stores describe Cursor sessions, so both feed model.VendorCursor, and a
// vendor id is what the HUD's identity column and the `--vendor` flag address.
// Two adapters sharing one id would give the registry two answers to "which
// adapter is cursor" and would double the vendor line. So the Adapter above
// owns both readers and merges their rows.
//
// The composition has one honest cost and it is written down rather than
// hidden: model.Capabilities is static per ADAPTER, and this manifest carries
// no model and no context figure. A CLI row therefore renders those two cells
// as "absent now" when the truthful reading for that row is "this store has no
// such field" — the distinction model.Capabilities exists to keep. It is not
// representable per row, so the row states it in words instead: see
// extraNotInManifest. The alternative was a second vendor id (`cursor-cli`),
// which buys that one distinction and pays for it with a second vendor line, a
// second `--vendor` value and a second doctor pin for what the operator
// experiences as one tool. §3.9's addendum records the trade.
//
// # The credential rule, unchanged and narrower here
//
// The tree this reader walks sits beside stores that are opaque or worse. Each
// session directory also holds `store.db` (+ `-shm`, `-wal`) and sometimes
// `prompt_history.json`; `~/.cursor` itself holds config and cache files. This
// reader opens ONE file name and no other: `meta.json`, at a fixed depth of
// two directories below chats/. It never opens `store.db`, never reads
// `prompt_history.json`, never recurses, and never looks at a sibling of the
// manifest for any purpose — the walk lists directory NAMES and stats the one
// file it will open. `chats_test.go` plants credential-shaped and prompt-shaped
// markers in every one of those neighbours and asserts none of them reaches a
// Session, a Diagnostic or an Extra, which is the same standing test the
// Composer reader carries.
//
// # What the manifest cannot say
//
//   - model — no key of any kind. The IDE's `composerData` names one; this
//     store does not, so a CLI row leaves model nil rather than inheriting a
//     plausible default.
//   - context %, cost, quota — the 2026-08-29 survey swept 1,951 CLI records
//     across 71 transcripts for `usage|tokens|input_tokens|cost|…` and matched
//     nothing, confirming §7.16's 2026-08-08 measurement on a 1.4x larger
//     corpus at a build one week newer. Cost and quota stay CapNone for the
//     whole vendor and nothing here moves them.
//   - liveness — same as the Composer side: no in-flight session was sampled,
//     so age classification is the HUD's job.
const (
	// chatsDir is the CLI session tree, relative to ~/.cursor.
	chatsDir = "chats"
	// metaFile is the ONE file name this reader opens. See the credential note.
	metaFile = "meta.json"
)

// metaSchemaVersion is the manifest contract this reader speaks, PINNED to the
// value observed live: `schemaVersion` was 1 on 43 of 43 manifests at
// cursor-agent 2026.08.11-e8db854 on 2026-08-29.
//
// A manifest declaring anything else draws no row, which is dropfile's rule and
// for dropfile's reason: a contract number the reader does not know means the
// keys may no longer mean what this code thinks they mean, and reading them
// anyway would invent every value at once. The skip is COUNTED and reported —
// see chatsReader.scan — because a silent skip would turn a vendor format bump
// into "you have no CLI sessions", which is the wrong answer rather than a
// missing one (ErrSchemaMismatch's rule, one tier down).
const metaSchemaVersion = 1

// chatsVerifiedAgainst names the vendor build this manifest's field map was
// read at. It is the CLI's own date-stamped scheme, which is why it is not
// VerifiedAgainst: that constant names the Cursor APPLICATION the SQLite store
// was surveyed inside, and internal/adapter/pins already records that the two
// numbering schemes cannot be compared.
const chatsVerifiedAgainst = "cursor-agent 2026.08.11-e8db854"

// cliIDPrefix namespaces CLI session ids away from Composer ids.
//
// model.Session.ID must be unique within a vendor and this adapter now merges
// two id spaces. Both are UUIDs, so a collision is not a practical worry — but
// Read has to route a ref back to the store it came from, and a prefix makes
// that structural instead of a guess-and-fall-back. It also means an id on
// screen or in a snapshot document says which store the row was read from,
// with no lookup.
const cliIDPrefix = "cli:"

// maxMetaBytes bounds one manifest. The observed files are 124–140 bytes; this
// is four orders of magnitude of headroom and exists only so a pathological or
// hostile file cannot make a poll tick allocate.
const maxMetaBytes = 64 << 10

// maxChatDirs bounds the walk. The tree is fixed-depth, so this is not a
// recursion guard — it caps how many directory entries one tick will consider
// if the vendor's own pruning ever stops running.
const maxChatDirs = 8192

// canaryChatsClock is the manifest's activity timestamp.
//
// It is the CLI row's only clock apart from the manifest's own mtime, and the
// two were measured agreeing within 0.2 s on 43 of 43 files — so a rename of
// this key would NOT show up as a missing age. It would show up as an age that
// is silently the file's mtime instead of the vendor's own reading, which is
// exactly the drift `internal/adapter/drift` exists to make audible.
var canaryChatsClock = drift.Canary{
	Name:  metaFile + " updatedAtMs",
	Feeds: model.NewFieldSet(model.FieldLastActivity),
}

// meta is the manifest, and the struct is the allowlist.
//
// The observed key set is exactly these six (40 manifests carry five of them;
// 3 add `title`). A key with no field here has no destination and encoding/json
// drops it before this reader sees it — the same "the allowlist is the struct"
// mechanism internal/cursorhook uses against a payload carrying reply text and
// an email address.
//
// The numbers are pointers so that a vendor-written zero stays distinguishable
// from a key the vendor omitted, and HasConversation is a pointer for the
// sharper version of the same reason: an ABSENT flag must not be read as a
// declared false. See chatsVerdict.
type meta struct {
	SchemaVersion   *int     `json:"schemaVersion"`
	CWD             string   `json:"cwd"`
	Title           string   `json:"title"`
	CreatedAtMs     *float64 `json:"createdAtMs"`
	UpdatedAtMs     *float64 `json:"updatedAtMs"`
	HasConversation *bool    `json:"hasConversation"`
}

// chatsRecord is one CLI session as its manifest describes it.
type chatsRecord struct {
	id   string // the session uuid, without cliIDPrefix
	path string // the manifest this record was read from, the row's Locator
	// name is the vendor title; empty on 40 of 43 observed manifests, which is
	// what makes an absent title genuine absence rather than a failed read.
	name         string
	workspace    string // the manifest's cwd, a native path, verbatim
	lastActivity time.Time
	sawClock     bool // the manifest carried a readable updatedAtMs
}

// verdict is what one manifest turned out to be.
type verdict uint8

const (
	// vSession: a real session. The record is filled.
	vSession verdict = iota
	// vShell: `hasConversation` is false — created and never conversed in.
	vShell
	// vUnknownVersion: schemaVersion is not metaSchemaVersion.
	vUnknownVersion
	// vUnparseable: not JSON this reader can read at all.
	vUnparseable
)

// chatsReader reads the CLI manifest tree. Safe for concurrent use.
//
// It caches per MANIFEST rather than per store, which is the opposite of the
// Composer reader above and for the opposite reason: there the whole corpus is
// one 9 MB file, so the parse is per-store; here it is 43 files of 140 bytes,
// so the cheap unit is the file. A tick on which nothing moved costs one
// directory listing per workspace hash plus one stat per session, and opens
// nothing.
type chatsReader struct {
	// root is ~/.cursor/chats. Empty means this reader is switched off, which
	// is what NewWithRoot leaves behind.
	root string

	mu    sync.Mutex
	cache map[string]cachedMeta
}

// cachedMeta is one manifest's parse, plus the file identity it was parsed
// from.
type cachedMeta struct {
	size int64
	mod  time.Time
	rec  chatsRecord
	v    verdict
}

func newChatsReader(root string) *chatsReader {
	return &chatsReader{root: root, cache: map[string]cachedMeta{}}
}

// chatsScan is one walk of the tree.
type chatsScan struct {
	recs  []chatsRecord
	notes []string
	// examined is how many manifests this walk read or reused — the drift
	// sample size, and the number that makes "every one of them was
	// unreadable" a statement rather than a coincidence.
	examined int
	// unknown counts manifests skipped for an unrecognized schemaVersion. It
	// is separate from examined because it is the one skip that means the
	// FORMAT moved rather than the session being uninteresting.
	unknown int
	watch   *drift.Watch
}

// scan walks chats/<hash>/<uuid>/meta.json at fixed depth.
//
// Never recursive, and it lists directory names rather than opening what it
// finds: the only file this function opens is one named exactly metaFile. A
// directory that holds no manifest is skipped in silence and that is a measured
// case, not an oversight — 22 of the 65 session directories on the survey
// machine held `store.db` with no `meta.json` at all, because the manifest is
// newer than the tree. Those sessions carry no readable name, workspace or
// vendor timestamp, so a row for one would assert a session existed and date it
// from a directory mtime whose meaning was never measured.
func (c *chatsReader) scan(ctx context.Context, now time.Time) (*chatsScan, error) {
	if c == nil || c.root == "" {
		return nil, model.ErrVendorAbsent
	}
	hashes, err := os.ReadDir(c.root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, model.ErrVendorAbsent
	}
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	out := &chatsScan{watch: drift.NewWatch(chatsVerifiedAgainst, canaryChatsClock)}
	// Rebuilt from scratch each walk so that pruned sessions leave the cache
	// with them; the vendor prunes this tree on a schedule of its own.
	fresh := make(map[string]cachedMeta, len(c.cache))
	unparseable, seen := 0, 0

	for _, h := range hashes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !h.IsDir() {
			continue
		}
		sessions, err := os.ReadDir(filepath.Join(c.root, h.Name()))
		if err != nil {
			// One workspace hash that vanished or that the OS refuses must not
			// take the others down: a partial read degrades, it does not fail
			// the vendor (§4a.5).
			continue
		}
		for _, s := range sessions {
			if seen++; seen > maxChatDirs {
				out.notes = append(out.notes,
					"stopped walking the cursor-agent CLI session tree at "+strconv.Itoa(maxChatDirs)+" directories")
				return c.finish(out, fresh, unparseable), nil
			}
			if !s.IsDir() {
				continue
			}
			path := filepath.Join(c.root, h.Name(), s.Name(), metaFile)
			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				continue
			}
			entry, ok := c.reuse(path, info)
			if !ok {
				entry = readMeta(path, info, s.Name(), now)
			}
			fresh[path] = entry
			out.examined++
			switch entry.v {
			case vSession:
				if entry.rec.sawClock {
					out.watch.Saw(canaryChatsClock)
				}
				out.recs = append(out.recs, entry.rec)
			case vShell:
				// Skipped in silence; see finish.
			case vUnknownVersion:
				out.unknown++
			case vUnparseable:
				unparseable++
			}
		}
	}
	return c.finish(out, fresh, unparseable), nil
}

// finish installs the new cache and words the walk's notes.
//
// Two of the skips are deliberately NOT reported. Empty shells and
// manifest-less directories are the CLI's version of §3.9's "most rows are not
// sessions": a permanent, expected property of the tree — 3 shells and 22
// manifest-less directories out of 65 on the survey machine — and a diagnostic
// that is always present is a diagnostic nobody reads. An unrecognized
// schemaVersion is the opposite. It is the format moving, it is what this
// adapter would otherwise report as "you have no CLI sessions", and it is said
// every time it happens.
func (c *chatsReader) finish(out *chatsScan, fresh map[string]cachedMeta, unparseable int) *chatsScan {
	c.cache = fresh
	if out.unknown > 0 {
		out.notes = append(out.notes,
			plural(out.unknown, "cursor-agent CLI session manifest carries", "cursor-agent CLI session manifests carry")+
				" a schemaVersion this adapter does not read (it reads "+strconv.Itoa(metaSchemaVersion)+")")
	}
	if unparseable > 0 {
		// Structure only, never content. This repo is public.
		out.notes = append(out.notes,
			plural(unparseable, "cursor-agent CLI session manifest did not parse", "cursor-agent CLI session manifests did not parse"))
	}
	sort.Slice(out.recs, func(i, j int) bool { return out.recs[i].id < out.recs[j].id })
	out.watch.Observed(metaFile + " schemaVersion " + strconv.Itoa(metaSchemaVersion))
	return out
}

// reuse returns the cached parse when the manifest has not moved since it was
// read.
func (c *chatsReader) reuse(path string, info os.FileInfo) (cachedMeta, bool) {
	e, ok := c.cache[path]
	if !ok || e.size != info.Size() || !e.mod.Equal(info.ModTime()) {
		return cachedMeta{}, false
	}
	return e, true
}

// readMeta opens one manifest and rules on it.
func readMeta(path string, info os.FileInfo, id string, now time.Time) cachedMeta {
	e := cachedMeta{size: info.Size(), mod: info.ModTime(), v: vUnparseable}
	if info.Size() > maxMetaBytes {
		return e
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return e
	}
	var m meta
	if json.Unmarshal(raw, &m) != nil {
		return e
	}
	if m.SchemaVersion == nil || *m.SchemaVersion != metaSchemaVersion {
		e.v = vUnknownVersion
		return e
	}
	// hasConversation:false is an EMPTY SHELL and draws no row. The ruling is
	// measured rather than assumed: all three such manifests on the survey
	// machine held nothing but themselves — no `store.db`, no
	// `prompt_history.json` — and their `updatedAtMs` stood 263–387 ms after
	// their `createdAtMs`. They are a directory the vendor stamps when a
	// session is created and abandons when nothing is typed, which is the same
	// class as the Composer store's `empty-state-draft` and its pre-created
	// composers (§3.9, "most rows are not sessions"). An ABSENT flag is not a
	// declared false and keeps its row: a manifest that stopped writing the key
	// must not silently empty the HUD.
	if m.HasConversation != nil && !*m.HasConversation {
		e.v = vShell
		return e
	}

	rec := chatsRecord{id: id, path: path, name: m.Title, workspace: m.CWD}
	// §6 Q8 for this vendor's CLI half, and it applies here where it
	// deliberately does not on the Composer side: that store gives every
	// session ONE file, so its mtime dates the store; this one gives every
	// session its own manifest, so the mtime dates the session. The fold is the
	// newest of the two, each passing the future-skew guard on its own. On the
	// survey machine the two agreed within 0.2 s on 43 of 43 manifests, so the
	// fold is a guard rather than a source: it is what keeps the row honest if
	// the vendor ever writes the file without restamping the key.
	if m.UpdatedAtMs != nil {
		if t, ok := fromMillis(*m.UpdatedAtMs); ok && !t.After(now.Add(futureSkew)) {
			rec.lastActivity, rec.sawClock = t, true
		}
	}
	if mod := info.ModTime(); !mod.After(now.Add(futureSkew)) && mod.After(rec.lastActivity) {
		rec.lastActivity = mod
	}
	e.rec, e.v = rec, vSession
	return e
}

// readCLI assembles one CLI session out of the walk Discover held.
//
// It answers from that walk rather than re-reading the manifest, for the reason
// the Composer half reuses its snapshot: a Read that re-measured would let one
// frame mix rows read at different instants, and the walk is already cached
// per file.
func (a *Adapter) readCLI(ctx context.Context, id string) (*model.Session, error) {
	scan := a.chatsSnapshot()
	for _, rec := range scan.recs {
		if rec.id != id {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return a.cliSession(rec, scan, time.Now()), nil
	}
	// The vendor pruned the session, or it stopped passing the filter, between
	// Discover and Read. The HUD drops the row silently.
	return nil, model.ErrSessionGone
}

// cliSession turns one CLI record into the normalized model.
func (a *Adapter) cliSession(rec chatsRecord, scan *chatsScan, now time.Time) *model.Session {
	s := &model.Session{
		Vendor:     Vendor,
		ID:         cliIDPrefix + rec.id,
		ObservedAt: now,
	}

	// The title when the vendor wrote one — it did on 3 of 43 manifests, so an
	// absent title is genuine absence and not a read that failed. Otherwise the
	// workspace basename, which is the fallback internal/hud's own sessionLabel
	// applies and which internal/adapter/pi already applies at the adapter.
	// Naming the row after a directory the manifest itself states is a reading;
	// the eight-hex-character id the Composer side falls back to is what is
	// left when there is not even that.
	switch {
	case rec.name != "":
		s.Name = model.Ptr(rec.name)
	case rec.workspace != "":
		s.Name = model.Ptr(filepath.Base(rec.workspace))
	}

	// The manifest writes a native path (`C:\...` on 43 of 43 here), not the
	// `file:///c%3A/...` URI the Composer store's workspace.json carries. It is
	// taken verbatim: there is no unit to convert and inventing one would be
	// the adapter guessing at a path.
	if rec.workspace != "" {
		s.WorkspaceDir = model.Ptr(rec.workspace)
	}

	if rec.lastActivity.IsZero() {
		s.Degraded = s.Degraded.With(model.FieldLastActivity)
		s.Diagnostics = append(s.Diagnostics,
			"no readable activity timestamp on this session's manifest")
	} else {
		s.LastActivity = model.TimePtr(rec.lastActivity)
	}

	s.Extras = append(s.Extras,
		model.Extra{Label: extraSource, Value: sourceCLI},
		model.Extra{Label: extraNotInManifest, Value: "model, context %"},
	)
	s.Diagnostics = append(s.Diagnostics, scan.notes...)
	scan.watch.Fold(s, scan.examined)
	return s
}

// plural renders "1 thing" / "N things" without a format string, the way the pi
// adapter's does. Diagnostics are displayed strings, so the count is the only
// variable part.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
