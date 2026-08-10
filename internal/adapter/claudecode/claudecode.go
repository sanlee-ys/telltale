// Package claudecode adapts Claude Code's on-disk transcripts to
// model.Session.
//
// Source verified live on 2026-08-01 against Claude Code 2.1.219 on the dev
// machine: 33 project directories, 837 top-level transcripts, 13,211 records
// walked read-only. The survey is written up in docs/design.md §3.1; nothing
// from it is reproduced here or in testdata, which is synthesized to shape.
//
// # What this adapter cannot know, and why
//
// These are declared CapNone rather than filled with a plausible number. Each
// was grepped for across the live corpus and returned zero matches:
//
//   - context_pct — there is no context_window_size on disk. Token counts are
//     sourced, but the denominator varies by model and by the [1m] variant, so
//     any percentage would need an assumed window. An assumed denominator is
//     an invented gauge (decisions/001).
//   - cost — cost.total_cost_usd exists only on the statusline stdin payload,
//     which the HUD does not consume.
//   - quota — rate_limits likewise stdin-only. Claude's quota lives on the
//     statusline seam; Codex's lives on the disk seam (design.md §3.3).
//   - liveness — see the LivenessHint note below.
//
// # What it derives
//
//   - subagents — the count of recently written transcripts in the session's
//     subagents/ sidecar (design.md §3.1). The files are counted exactly; the
//     inference is the recency boundary that turns "written lately" into "a
//     fan-out is running", so the field is CapDerived and the HUD marks it.
//
// # Liveness
//
// The honest primitive is the transcript's mtime, which is LAST ACTIVITY and
// is reported as such; the HUD classifies liveness from its age against one
// shared threshold set, identically for every vendor.
//
// The survey found an undocumented registry at ~/.claude/sessions/<PID>.json
// mapping live PIDs to session ids. This adapter deliberately does not read
// it. Every use of it reduces to "a process with this id exists", which
// design.md §4a.4 names explicitly as evidence a process exists rather than
// evidence the session is doing anything — the one case where an adapter can
// lie to the HUD undetectably. It is recorded in the design doc as a verified
// observation and left unused in v1.
package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sanlee-ys/telltale/internal/adapter/drift"
	"github.com/sanlee-ys/telltale/internal/jsonl"
	"github.com/sanlee-ys/telltale/internal/model"
)

// Vendor is the stable id for rows this adapter produces.
const Vendor = model.VendorClaude

// verifiedAgainst names the vendor build this adapter's field map was read from
// (see the package doc). It is what a drift report is measured against.
const verifiedAgainst = "Claude Code 2.1.219"

// canarySessionID is the record envelope's identity field. The survey found it
// on the first record of 60 of 60 transcripts sampled, and on the housekeeping
// records besides — it is the one field every record type here shares.
//
// A transcript whose records PARSE and carry no sessionId is not the shape this
// adapter's field map describes: cwd, the title records and the assistant
// message block all hang off that envelope, so their absence would otherwise
// read as "the vendor had nothing to say".
var canarySessionID = drift.Canary{
	Name: "sessionId",
	Feeds: model.NewFieldSet(
		model.FieldName,
		model.FieldModel,
		model.FieldWorkspace,
	),
}

// Read budget. Live transcripts routinely reach 7.7 MB, so Read never touches
// the middle of a file: the head carries session identity (verified present on
// the first record of 60 of 60 files sampled) and the tail carries the newest
// model and usage. Both windows are generous enough that a normal session is
// read whole.
const (
	headBytes int64 = 64 << 10
	tailBytes int64 = 256 << 10
)

// futureSkew is how far a transcript's mtime may sit ahead of the observation
// clock before the timestamp is treated as unreadable rather than as an age.
// Sub-second skew between a file's mtime and the local clock is measurement
// noise; minutes of it is a broken signal, and rendering it as "0s" would
// claim the session was active this instant.
const futureSkew = 2 * time.Second

// syntheticModel is the model id Claude Code writes on locally generated
// assistant records (API errors, interrupts). Those records carry zeroed
// usage, so they must neither reach the model cell nor overwrite a real
// reading.
const syntheticModel = "<synthetic>"

// subagentDir is the sidecar directory name under <sessionId>/ that holds a
// session's sub-agent transcripts (design.md §3.1). Discover deliberately does
// not walk into it — those files are not sessions — but their COUNT is a fact
// about the parent session, and counting them is a stat pass, not a read.
const subagentDir = "subagents"

// subagentHorizon is how recently a sub-agent transcript must have been
// written to be counted as part of a fan-out in progress.
//
// It is deliberately model.DefaultLivenessThresholds.Idle rather than a second
// number: the chip sits on a row whose dot already classifies "recent" at that
// boundary, and two different definitions of recent on one line is how a
// display starts contradicting itself. Sourcing it from the shared default
// also means an operator who moves the boundary moves both.
//
// This boundary is the whole reason the count is DERIVED rather than reported.
// The files are counted exactly; "these are a fan-out running now" is the
// inference, and the HUD marks it.
var subagentHorizon = model.DefaultLivenessThresholds.Idle

// Adapter reads Claude Code transcripts. It is safe for concurrent use; the one
// piece of mutable state is the parse cache below, and it is behind a mutex.
type Adapter struct {
	root string

	// mu guards parses. Reads run concurrently (internal/hud.readAll fans out
	// across a semaphore) and every one of them consults this map, so the
	// synchronization is real rather than decorative — the repo runs a `race`
	// job in CI.
	mu sync.Mutex
	// parses is the head+tail parse of each transcript, keyed by locator and
	// stamped with the file identity it was parsed from. See parsed.
	parses map[string]*parsed
}

// parsed is everything ONE head+tail parse of a transcript determined, kept so
// that a tick on which the file did not move does not parse it again.
//
// Why this exists, measured on the owner's corpus 2026-08-09 (967 Claude
// transcripts, 693 MB on disk, Windows 11): a full scan read 164 MB and
// unmarshalled 46,727 JSON records EVERY SECOND, and json.Unmarshal alone was
// 2.65 s of the 3.6 s of serial work — against a 1 s poll. The footer's
// "last scan Ns ago" staleness notice was therefore on permanently, which is
// how a true signal becomes wallpaper. I/O was not the problem: open 73 ms,
// stat 14 ms, head 185 ms, tail 364 ms over the same 967 files. Parsing was.
//
// # Why this cannot lie
//
// The cache is keyed on (size, mtime) of the transcript, and NOTHING derived
// from anything else lives in here. Specifically:
//
//   - LastActivity is NOT cached. §6 Q8 makes it max(mtime, newest record
//     timestamp), and BOTH inputs can move — so only the record-timestamp half
//     (newestTS), which is a pure function of the bytes this parse read, is
//     stored. The mtime half is re-stat'ed and the max re-folded on every read.
//   - The subagent count is NOT cached. It is a function of `now` (a
//     transcript written 40 minutes ago crosses subagentHorizon with no file
//     changing at all), so countSubagents runs on every read, cache hit or not.
//     It was always a stat pass and stays one: 321 ms serial over 967 sessions.
//   - Diagnostics are stored and replayed verbatim, so a cache hit reports
//     exactly the torn records and drift a fresh read would have reported. A
//     hit that quietly dropped a diagnostic would be worse than the slow scan.
//   - Absence is stored as absence: an empty string here means the parse found
//     no value, and the rebuilt session leaves the field nil. Nothing in here
//     can resurrect a value whose record is gone, because a file whose records
//     changed has a different mtime and is re-parsed.
//
// The residual risk is the one every mtime cache carries: a rewrite that lands
// on byte-identical size AND identical mtime is invisible. NTFS timestamps are
// 100 ns, the vendor appends rather than rewrites, and internal/adapter/cursor
// already accepts this same trade for the same reason (see its snapshot type).
type parsed struct {
	size int64
	mod  time.Time

	// The four things applyRecord and aiTitle can set, as plain values.
	workspace string
	name      string
	modelID   string
	extras    []model.Extra

	// newestTS is the newest in-window record timestamp, zero if none parsed.
	newestTS time.Time
	// bad and good are the unparseable and well-formed record counts; good is
	// what drift.Fold weighs, and zero is its load-bearing case.
	bad, good int
	// watch is the drift verdict for this parse. Fold only READS a Watch, so
	// replaying one across ticks is safe, and its verdict is a function of the
	// records — exactly what the key covers.
	watch *drift.Watch
}

// cachedParse returns the stored parse when the file identity still matches.
func (a *Adapter) cachedParse(locator string, info fs.FileInfo) (*parsed, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.parses[locator]
	if !ok || p.size != info.Size() || !p.mod.Equal(info.ModTime()) {
		return nil, false
	}
	return p, true
}

func (a *Adapter) storeParse(locator string, p *parsed) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.parses == nil {
		a.parses = make(map[string]*parsed)
	}
	a.parses[locator] = p
}

// pruneParses drops cache entries for transcripts the latest Discover did not
// see, so a long-running HUD does not accumulate an entry per session that ever
// existed. Discover is the only honest place to do this: it is the one caller
// that knows the whole live set.
func (a *Adapter) pruneParses(live map[string]struct{}) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for k := range a.parses {
		if _, ok := live[k]; !ok {
			delete(a.parses, k)
		}
	}
}

// New returns an adapter rooted at the user's Claude projects directory.
func New() *Adapter {
	home, err := os.UserHomeDir()
	if err != nil {
		// An unresolvable home is indistinguishable from "not installed" for
		// our purposes: Discover will report the vendor absent.
		return &Adapter{}
	}
	return &Adapter{root: filepath.Join(home, ".claude", "projects")}
}

// NewWithRoot points the adapter at an explicit projects directory. Tests use
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
// subagents is DERIVED: nothing on disk states the number, and the adapter
// computes it from a directory listing plus a recency boundary. Claude Code is
// the only vendor that writes the sidecar tree, so it is the only adapter that
// declares the field at all — for Codex it stays CapNone and the HUD never
// draws a chip on a Codex row.
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

// Discover lists top-level transcripts. Directory listing and stat only — the
// HUD calls this every poll tick.
//
// Two traps this encodes, both measured rather than assumed:
//
//   - The glob is projects/<slug>/<uuid>.jsonl and is NOT recursive.
//     Recursing found 2021 files where 837 are sessions; the extra 1,184 are
//     subagent transcripts under <sessionId>/subagents/ plus tool-results/ and
//     workflows/ sidecars. A recursive glob inflates the session list 2.4x and
//     double-counts every token.
//   - The basename is validated as a UUID, not just as *.jsonl. Non-session
//     .json/.md/.txt neighbours share these directories.
//
// The project-directory slug is never decoded to a path: it maps both '\' and
// a literal '-' to '-', and the drive letter's case is not stable. cwd is read
// from the record instead.
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

	// Keyed by session id: the same tree can hold sibling project directories
	// differing only in drive-letter case, and a duplicate id would break the
	// HUD's row matching. Newest mtime wins.
	byID := make(map[string]model.SessionRef)
	for _, p := range projects {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !p.IsDir() {
			continue
		}
		dir := filepath.Join(a.root, p.Name())
		entries, err := os.ReadDir(dir)
		if err != nil {
			// The tree mutates during a sweep — a project directory vanished
			// between enumeration and open during the live survey. A sweep
			// that aborts on that loses every other vendor's rows too.
			continue
		}
		for _, e := range entries {
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
			mod := info.ModTime()
			if prev, seen := byID[id]; seen && prev.LastActivity != nil && !mod.After(*prev.LastActivity) {
				continue
			}
			byID[id] = model.SessionRef{
				Vendor:       Vendor,
				ID:           id,
				Locator:      filepath.Join(dir, e.Name()),
				LastActivity: model.TimePtr(mod),
			}
		}
	}

	refs := make([]model.SessionRef, 0, len(byID))
	live := make(map[string]struct{}, len(byID))
	for _, r := range byID {
		refs = append(refs, r)
		live[r.Locator] = struct{}{}
	}
	a.pruneParses(live)
	return refs, nil
}

// sessionIDFromFile accepts <uuid>.jsonl and nothing else.
func sessionIDFromFile(name string) (string, bool) {
	if !strings.HasSuffix(name, ".jsonl") {
		return "", false
	}
	id := strings.TrimSuffix(name, ".jsonl")
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

// record is the subset of a transcript record this adapter reads. Unknown
// fields are ignored by design: the vendor adds fields between versions.
//
// Only assistant and user records carry message. custom-title, last-prompt,
// mode and ai-title carry {type, sessionId, <one payload key>} with no
// timestamp and no cwd, so every field here is optional at the type level.
type record struct {
	Type        string `json:"type"`
	SessionID   string `json:"sessionId"`
	Cwd         string `json:"cwd"`
	GitBranch   string `json:"gitBranch"`
	Version     string `json:"version"`
	Entrypoint  string `json:"entrypoint"`
	IsSidechain bool   `json:"isSidechain"`
	CustomTitle string `json:"customTitle"`
	// Timestamp is RFC3339, written by the vendor on most records (verified
	// live 2026-08-01; housekeeping records may omit it). It feeds the §6 Q8
	// ruling: NTFS defers mtime while the writer holds the file, so the
	// newest record timestamp is the fresher of the two vendor signals on a
	// hot session.
	Timestamp string `json:"timestamp"`
	Message   *msg   `json:"message"`
}

// aiTitle extracts the label from an ai-title record.
//
// The survey verified the record's SHAPE — {type, sessionId, <one payload
// key>} — but did not capture the payload key's name, and the live-doc rule
// forbids guessing a field name from memory. So this matches on the verified
// structure instead: exactly one key beyond type and sessionId, holding a
// string. A record with a different shape yields no title, and the session
// falls back to its workspace name, which is absence rather than a wrong label.
func aiTitle(raw []byte) (string, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", false
	}
	var found string
	var n int
	for k, v := range obj {
		if k == "type" || k == "sessionId" {
			continue
		}
		n++
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			found = s
		}
	}
	if n != 1 || found == "" {
		return "", false
	}
	return found, true
}

type msg struct {
	Model string `json:"model"`
	Usage *usage `json:"usage"`
}

type usage struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
}

// contextIn is the tokens actually sent as context on a request.
//
// input_tokens alone is a lie and the live corpus proves it: a real record
// carried input_tokens=2 alongside cache_read=213388 and cache_creation=2464.
// Reading input_tokens as "context used" renders 2 tokens for a ~216k context.
func (u *usage) contextIn() int64 {
	if u == nil {
		return 0
	}
	return u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
}

// Read parses one transcript into the normalized model.
//
// Partial failure is not an error: a field that cannot be parsed is left nil,
// marked degraded and explained in Diagnostics, and the row still renders with
// an em dash in that cell.
func (a *Adapter) Read(ctx context.Context, ref model.SessionRef) (*model.Session, error) {
	// Stat before open. On the common tick the parse is cached and the file is
	// never opened at all, and a stat is the cheapest way to answer both
	// questions this path asks: does the transcript still exist (a vanished one
	// is ErrSessionGone, exactly as before), and did it move.
	info, err := os.Stat(ref.Locator)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, model.ErrSessionGone
	}
	if err != nil {
		return nil, err
	}

	p, hit := a.cachedParse(ref.Locator, info)
	if !hit {
		p, err = a.parse(ref.Locator, info)
		if err != nil {
			return nil, err
		}
		a.storeParse(ref.Locator, p)
	}

	now := time.Now()
	s := &model.Session{Vendor: Vendor, ID: ref.ID, ObservedAt: now}

	// The parse's four values, restored the same way a fresh parse would set
	// them. An empty string is a parse that found nothing, so the field stays
	// nil — absence survives the cache.
	if p.workspace != "" {
		s.WorkspaceDir = model.Ptr(p.workspace)
	}
	if p.name != "" {
		s.Name = model.Ptr(p.name)
	}
	if p.modelID != "" {
		s.Model = &model.Model{ID: p.modelID}
	}
	if len(p.extras) > 0 {
		// Copied, not aliased: Extras is a slice on a struct the cache keeps
		// across ticks, and handing the same backing array to every reader is
		// how one row's append silently rewrites another's.
		s.Extras = append([]model.Extra(nil), p.extras...)
	}

	// last_activity is the fresher of two vendor-written signals: the file's
	// mtime and the newest record timestamp (§6 Q8 ruling, 2026-08-01). NTFS
	// defers the mtime update while the writer holds the file — observed lags
	// of ~100 s on a hot session and ~20 min on a closing one — so mtime alone
	// UNDER-reports activity on exactly the rows the HUD exists to watch. A
	// signal meaningfully ahead of the observation clock is a broken read and
	// is excluded. Only if BOTH signals are unreadable does the field degrade.
	//
	// The mtime half is re-stat'ed on EVERY read and the max re-folded here,
	// cache hit or not — this is the field the cache is least allowed to freeze,
	// because it is the one the whole staleness display hangs off.
	mtimeOK := false
	var mtime time.Time
	if mod := info.ModTime(); !mod.After(now.Add(futureSkew)) {
		mtimeOK, mtime = true, mod
	}

	// Q8 fold: the fresher valid signal wins; both invalid degrades.
	switch {
	case mtimeOK && p.newestTS.After(mtime):
		s.LastActivity = model.TimePtr(p.newestTS)
	case mtimeOK:
		s.LastActivity = model.TimePtr(mtime)
	case !p.newestTS.IsZero():
		s.LastActivity = model.TimePtr(p.newestTS)
	default:
		s.Degraded = s.Degraded.With(model.FieldLastActivity)
		s.Diagnostics = append(s.Diagnostics, "no readable activity timestamp (mtime ahead of the clock, no record timestamps)")
	}

	if p.bad > 0 {
		// Structure only, never transcript content: this repo is public.
		s.Diagnostics = append(s.Diagnostics, plural(p.bad, "unparseable record skipped", "unparseable records skipped"))
	}

	// Not cached, and deliberately: the count is a function of `now` as much as
	// of the disk (subagentHorizon), so a session's fan-out chip must expire on
	// the clock even though no file moved. It was always a stat pass.
	a.countSubagents(s, ref.Locator, now)

	// Last, because the verdict reads what the session managed to source. Fold
	// only reads the Watch, which is what makes replaying a cached one sound.
	p.watch.Fold(s, p.good)

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

// parse reads the head and tail windows and unmarshals them. This is the
// expensive half of Read and the only half the cache stores; see parsed.
func (a *Adapter) parse(locator string, info fs.FileInfo) (*parsed, error) {
	f, err := os.Open(locator)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, model.ErrSessionGone
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	now := time.Now()
	p := &parsed{
		size:  info.Size(),
		mod:   info.ModTime(),
		watch: drift.NewWatch(verifiedAgainst, canarySessionID),
	}

	// The head window must end where the tail window begins, or records in
	// the overlap parse twice and inflate the unparseable-record count
	// (last-wins application hides the duplication, the diagnostics don't).
	// Files at or under tailBytes are covered by the tail alone; between
	// tailBytes and tailBytes+headBytes the head window shrinks to the gap.
	headWindow := int64(headBytes)
	if gap := info.Size() - tailBytes; gap < headWindow {
		headWindow = gap // <= 0 disables the head read entirely
	}
	var head [][]byte
	if headWindow > 0 {
		head, err = jsonl.Head(f, headWindow)
		if err != nil {
			return nil, err
		}
	}
	tail, err := jsonl.Tail(f, info.Size(), tailBytes)
	if err != nil {
		return nil, err
	}

	// The parse folds into a scratch Session rather than into parsed's plain
	// fields directly, purely so applyRecord and setExtra stay the ONE place
	// that decides precedence (last record wins, customTitle outranks ai-title,
	// a synthetic model never overwrites a real one). Two copies of that logic
	// is how a cache and a fresh read start disagreeing.
	scratch := &model.Session{Vendor: Vendor}
	apply := func(recs [][]byte) {
		for _, raw := range recs {
			var r record
			if err := json.Unmarshal(raw, &r); err != nil {
				// One bad record degrades the fields it feeds, not the row.
				p.bad++
				continue
			}
			p.good++
			// The canary is checked before the sidechain skip: a sidechain
			// record is not a row, but it is still evidence about the shape.
			if r.SessionID != "" {
				p.watch.Saw(canarySessionID)
			}
			p.watch.Observed(r.Version)
			// The skew bound is taken against the PARSE clock, which is within
			// a tick of the read clock that used to take it. A record stamped
			// ahead of the clock is excluded and stays excluded until the file
			// moves again — the conservative direction, and it costs at most
			// futureSkew of freshness on a row whose mtime says the same thing.
			if ts, err := time.Parse(time.RFC3339Nano, r.Timestamp); err == nil &&
				ts.After(p.newestTS) && !ts.After(now.Add(futureSkew)) {
				p.newestTS = ts
			}
			if r.IsSidechain {
				// Free insurance: 0 of 837 top-level transcripts carried one
				// on 2.1.219 (they live in the subagents/ sidecar tree), but a
				// vendor change that moves them inline must not inflate rows.
				continue
			}
			a.applyRecord(scratch, &r)
			// A custom title outranks a generated one, so an ai-title never
			// overwrites a customTitle already seen.
			if r.Type == "ai-title" && scratch.Name == nil {
				if t, ok := aiTitle(raw); ok {
					scratch.Name = model.Ptr(t)
				}
			}
		}
	}
	apply(head)
	apply(tail)

	if scratch.WorkspaceDir != nil {
		p.workspace = *scratch.WorkspaceDir
	}
	if scratch.Name != nil {
		p.name = *scratch.Name
	}
	if scratch.Model != nil {
		p.modelID = scratch.Model.ID
	}
	p.extras = scratch.Extras
	return p, nil
}

// countSubagents counts the session's recently written sub-agent transcripts.
//
// This is a stat pass and nothing more: one ReadDir plus one Info per entry,
// no file is opened, no byte is parsed. That is what makes it affordable on
// the 1 s poll — the same reason Discover is listing-only.
//
// The sidecar lives beside the transcript, at <transcript without .jsonl>/
// subagents/ (design.md §3.1). Two absences, distinguished on purpose:
//
//   - the directory does not exist → this session has never fanned out, which
//     is a measured ZERO, not missing data;
//   - the directory exists and the OS refuses → nil plus a diagnostic. We do
//     not know, and "0" would be a claim.
func (a *Adapter) countSubagents(s *model.Session, locator string, now time.Time) {
	dir := filepath.Join(strings.TrimSuffix(locator, ".jsonl"), subagentDir)
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

	// Verified against the live tree 2026-08-01 (names only): sub-agent
	// transcripts are agent-<hex>.jsonl with agent-<hex>.meta.json neighbours,
	// NOT <uuid>.jsonl — the first cut of this function filtered for UUIDs and
	// counted zero forever (review blocker). Workflow fan-outs nest exactly one
	// level deeper at subagents/workflows/<wf_id>/agent-<hex>.jsonl, so those
	// subdirectories are listed too; nothing recurses past that.
	cutoff := now.Add(-subagentHorizon)
	n := 0
	countFile := func(e fs.DirEntry) {
		if !isSubagentTranscript(e.Name()) {
			return
		}
		info, err := e.Info()
		if err != nil {
			// Racing the vendor's writer is normal. Skipping the entry
			// undercounts by one, which is the conservative direction: a chip
			// that says 2 when 3 are running understates a fan-out; a chip
			// that counts an entry it could not stat asserts one it never saw.
			return
		}
		mod := info.ModTime()
		if mod.Before(cutoff) {
			return
		}
		if mod.After(now.Add(futureSkew)) {
			// Same rule as the session's own mtime: a timestamp ahead of the
			// clock is not a readable time, so it cannot be evidence of
			// recency. Counting it would let a skewed clock invent a fan-out.
			return
		}
		n++
	}
	for _, e := range entries {
		if !e.IsDir() {
			countFile(e)
			continue
		}
		if e.Name() != "workflows" {
			continue
		}
		wfs, err := os.ReadDir(filepath.Join(dir, e.Name()))
		if err != nil {
			// The flat count is still real; a half-readable tree degrades the
			// part we could not see into a diagnostic, not into silence.
			s.Diagnostics = append(s.Diagnostics, "subagents workflow tree unreadable")
			continue
		}
		for _, wf := range wfs {
			if !wf.IsDir() {
				continue
			}
			agents, err := os.ReadDir(filepath.Join(dir, e.Name(), wf.Name()))
			if err != nil {
				s.Diagnostics = append(s.Diagnostics, "subagents workflow tree unreadable")
				continue
			}
			for _, ae := range agents {
				if !ae.IsDir() {
					countFile(ae)
				}
			}
		}
	}
	s.Subagents = model.Ptr(n)
	s.Derived = s.Derived.With(model.FieldSubagents)
}

// isSubagentTranscript matches the sidecar transcript shape: a .jsonl file
// that is not a .meta.json neighbour. The basename prefix is deliberately NOT
// pinned to "agent-": the vendor renamed this shape once already (early trees
// held UUID basenames), and a count that goes quietly to zero on the next
// rename is this function's known failure mode.
func isSubagentTranscript(name string) bool {
	return strings.HasSuffix(name, ".jsonl")
}

// applyRecord folds one record into the session. Later records win, so the
// tail's values overwrite the head's — which is what makes "the newest model"
// fall out of a linear pass.
func (a *Adapter) applyRecord(s *model.Session, r *record) {
	if r.Cwd != "" {
		s.WorkspaceDir = model.Ptr(r.Cwd)
	}
	if r.CustomTitle != "" {
		s.Name = model.Ptr(r.CustomTitle)
	}
	if r.GitBranch != "" {
		setExtra(s, "branch", r.GitBranch)
	}
	if r.Version != "" {
		setExtra(s, "cli", r.Version)
	}
	if r.Message == nil {
		return
	}
	// A synthetic record is Claude Code's own locally generated notice. Its
	// zeroed usage must not overwrite the preceding real reading, and its id
	// must never reach the model cell.
	if r.Message.Model == "" || r.Message.Model == syntheticModel {
		return
	}
	// The model identity is sourced from the record alone — an assistant
	// record without a usage block still names the model (review finding:
	// gating it on usage left the MODEL cell blank for real sessions).
	s.Model = &model.Model{ID: r.Message.Model}
	if r.Message.Usage == nil {
		return
	}
	// Token counts are sourced but have no home in the v1 schema as a gauge:
	// without a context window size there is no honest percentage to compute
	// (see the package doc). They are carried as a display-only extra so the
	// measurement is not silently discarded.
	setExtra(s, "ctx tokens", formatTokens(r.Message.Usage.contextIn()))
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
