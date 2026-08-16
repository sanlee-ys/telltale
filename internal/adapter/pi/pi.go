// Package pi adapts Pi's on-disk session store to model.Session.
//
// This package is a HUD observer only. design.md §9.1 rejects Pi as a council
// seat (re-host class). This adapter does not spawn Pi and does not write a
// relay file.
//
// Source: four live sessions under ~/.pi/agent/sessions on this Windows box,
// written by pi 0.84.1 on 2026-08-11, read read-only. design.md §3.9b surveyed
// the writer source at v0.84.2 on 2026-08-16 and said the survey machine had
// no install. This pin is the installed binary, not that source survey. The
// live files match the source shape: format version 3, a tree of entries with
// parentId, and a first record of type "session" with a non-empty id.
//
// testdata is synthesized to that shape. No live session file is vendored.
// Live files hold model thinking blobs.
//
// # Layout
//
//	~/.pi/agent/sessions/--<cwd-slug>--/<ts>_<session-id>.jsonl
//
// The slug is the cwd with the leading separator dropped and with / \ : each
// replaced by "-". The slug is lossy, so this adapter does not decode it.
// Workspace comes from the header cwd.
//
// Sibling files under ~/.pi/agent/ (auth.json, settings.json, models.json)
// are outside the sessions tree. This adapter never builds a path to them.
//
// # Root resolution
//
//  1. PI_CODING_AGENT_SESSION_DIR, if set. That directory is the sessions root.
//  2. Else PI_CODING_AGENT_DIR/sessions, if set. The vendor uses that env as
//     the agent dir (~/.pi/agent), not as ~/.pi.
//  3. Else ~/.pi/agent/sessions.
//
// The default directory name is ".pi". A rebranded piConfig.configDir is not
// followed.
//
// # What this adapter cannot know, and why
//
// Each gap is a measurement, not a guess:
//
//   - cost as Session.Cost: every assistant message may write usage.cost.total
//     in dollars. That figure is the cost of THAT message. No session total
//     exists on disk. The TUI sums in memory. A sum is a derived number in a
//     read field's clothes (§3.9a). The last message's cost.total is an Extra
//     labeled as that message's cost.
//   - context_pct: last assistant usage is a numerator. The denominator lives
//     in Pi's model catalog, not in the session file. An assumed window is an
//     invented gauge.
//   - quota: four live files were walked for rate, limit, quota, and reset
//     keys. The only hits were a tool argument named limit and a truncation
//     flag. There is no account window on disk.
//   - liveness: no process registry and no turn-started mark. Last activity
//     is reported. The HUD classifies age. Process existence is not a hint
//     (design.md §4a.4).
//   - subagents: header.parentSession is a child-to-parent path. Zero of four
//     live files set it. A count from that link is too weak to declare.
//
// # Write-path quirk
//
// The vendor does not flush a session until an assistant message exists. A
// header-only file is still a session if the first record is type=session
// with an id. Fields that need later entries stay absent.
package pi

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
const Vendor = model.VendorPi

// verifiedAgainst names the vendor build this adapter's field map was read
// from. §3.9b surveyed source at v0.84.2. This pin is the binary on this
// Windows box.
const verifiedAgainst = "pi 0.84.1"

// canarySessionHeaderID is the first JSONL record: type "session" and a
// non-empty id. Four of four live files start that way. cwd, the activity
// clock, and the rest of the tree hang off that header. A file whose first
// record parses and is not that shape is not the field map below.
var canarySessionHeaderID = drift.Canary{
	Name: "session header id",
	Feeds: model.NewFieldSet(
		model.FieldName,
		model.FieldModel,
		model.FieldWorkspace,
		model.FieldLastActivity,
	),
}

// Read budget. One live file reached 1.4 MB, so Read does not take the
// middle. The head holds the session header. The tail holds the newest
// model_change, session_info, and assistant usage.
const (
	headBytes int64 = 64 << 10
	tailBytes int64 = 256 << 10
)

// futureSkew matches the other adapters. A timestamp more than this ahead of
// ObservedAt has no readable age. The field degrades rather than render "0s".
const futureSkew = 2 * time.Second

// Adapter reads Pi session files. It holds no mutable state and is safe for
// concurrent use.
type Adapter struct {
	// root is the sessions directory, ~/.pi/agent/sessions.
	root string
}

// New returns an adapter rooted at the user's Pi sessions directory.
func New() *Adapter {
	if dir := os.Getenv("PI_CODING_AGENT_SESSION_DIR"); dir != "" {
		return &Adapter{root: dir}
	}
	if dir := os.Getenv("PI_CODING_AGENT_DIR"); dir != "" {
		return &Adapter{root: filepath.Join(dir, "sessions")}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// An unresolvable home is the same as "not installed" here.
		// Discover reports the vendor absent.
		return &Adapter{}
	}
	return &Adapter{root: filepath.Join(home, ".pi", "agent", "sessions")}
}

// NewWithRoot points the adapter at an explicit sessions directory. Tests use
// it. A future config key would use it too.
func NewWithRoot(root string) *Adapter { return &Adapter{root: root} }

// Root is the directory this adapter watches, for the HUD's empty state. It
// is the only path this adapter exposes for display.
func (a *Adapter) Root() string { return a.root }

func (a *Adapter) Vendor() model.VendorID { return Vendor }

// Capabilities is static. See the package doc for the four CapNone fields.
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

// Discover lists session files. Directory listing and stat only.
//
// The walk is sessions/<slug>/*.jsonl at fixed depth. It does not recurse.
// It does not walk outside root. Files at the sessions root (auth.json if a
// caller pointed NewWithRoot at ~/.pi/agent by mistake) are not sessions.
func (a *Adapter) Discover(ctx context.Context) ([]model.SessionRef, error) {
	if a.root == "" {
		return nil, model.ErrVendorAbsent
	}
	slugs, err := os.ReadDir(a.root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, model.ErrVendorAbsent
	}
	if err != nil {
		return nil, err
	}

	var refs []model.SessionRef
	for _, slug := range slugs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !slug.IsDir() {
			continue
		}
		dir := filepath.Join(a.root, slug.Name())
		entries, err := os.ReadDir(dir)
		if err != nil {
			// The tree mutates during a sweep. A slug that vanishes mid-walk
			// must not drop every other vendor's rows.
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
			refs = append(refs, model.SessionRef{
				Vendor:       Vendor,
				ID:           id,
				Locator:      filepath.Join(dir, e.Name()),
				LastActivity: model.TimePtr(info.ModTime()),
			})
		}
	}
	return refs, nil
}

// sessionIDFromFile accepts <ts>_<session-id>.jsonl. The timestamp has no
// underscore. The session id may contain underscores.
func sessionIDFromFile(name string) (string, bool) {
	if !strings.HasSuffix(name, ".jsonl") {
		return "", false
	}
	stem := strings.TrimSuffix(name, ".jsonl")
	if stem == "" {
		return "", false
	}
	if i := strings.IndexByte(stem, '_'); i >= 0 && i+1 < len(stem) {
		return stem[i+1:], true
	}
	return stem, true
}

// entry is the subset of a Pi JSONL record this adapter reads. Unknown keys
// are ignored. Message content is not a field here, so a planted secret in
// text cannot reach a Session value.
type entry struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
	// model_change
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
	// session_info
	Name string `json:"name"`
	// message
	Message *piMessage `json:"message"`
}

type piMessage struct {
	Role  string   `json:"role"`
	Model string   `json:"model"`
	Usage *piUsage `json:"usage"`
}

type piUsage struct {
	Input  int64   `json:"input"`
	Output int64   `json:"output"`
	Cost   *piCost `json:"cost"`
}

type piCost struct {
	Total *float64 `json:"total"`
}

// Read parses one session file into the normalized model.
//
// Partial failure is not an error. A field that cannot be parsed stays nil,
// is marked degraded, and is explained in Diagnostics.
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

	mtimeOK := false
	var mtime time.Time
	if mod := info.ModTime(); !mod.After(now.Add(futureSkew)) {
		mtimeOK, mtime = true, mod
	}

	// Head and tail must not overlap. A record in both would parse twice and
	// inflate the unparseable-record count.
	headWindow := int64(headBytes)
	if gap := info.Size() - tailBytes; gap < headWindow {
		headWindow = gap // <= 0 disables the head read
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

	w := drift.NewWatch(verifiedAgainst, canarySessionHeaderID)
	var bad, good int
	var newestTS time.Time
	var lastInfoName string
	var sawInfo bool
	noteTS := func(raw string) {
		ts, ok := parseTS(raw)
		if !ok || !ts.After(newestTS) || ts.After(now.Add(futureSkew)) {
			return
		}
		newestTS = ts
	}

	apply := func(recs [][]byte, firstIsFileStart bool) {
		for i, raw := range recs {
			var r entry
			if err := json.Unmarshal(raw, &r); err != nil {
				bad++
				continue
			}
			good++
			noteTS(r.Timestamp)
			if firstIsFileStart && i == 0 {
				if r.Type == "session" && r.ID != "" {
					w.Saw(canarySessionHeaderID)
					s.ID = r.ID
					if r.Cwd != "" {
						s.WorkspaceDir = model.Ptr(r.Cwd)
					}
				}
				continue
			}
			a.applyEntry(s, &r, &lastInfoName, &sawInfo)
		}
	}
	// The first complete record is the canary. When the tail covers the
	// whole file, that record is tail[0]. When the head is in use, it is
	// head[0]. An empty head on a large file means the first line did not
	// fit the window. That case does not treat tail[0] as the start.
	if headWindow > 0 {
		apply(head, true)
		apply(tail, false)
	} else {
		apply(tail, true)
	}

	if sawInfo && lastInfoName != "" {
		s.Name = model.Ptr(lastInfoName)
	} else if n, ok := s.WorkspaceName(); ok {
		s.Name = model.Ptr(n)
	}

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
		// Structure only. This repo is public.
		s.Diagnostics = append(s.Diagnostics, plural(bad, "unparseable record skipped", "unparseable records skipped"))
	}

	w.Fold(s, good)

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

// applyEntry folds one non-header record. Later records win.
func (a *Adapter) applyEntry(s *model.Session, r *entry, lastInfoName *string, sawInfo *bool) {
	switch r.Type {
	case "session_info":
		*sawInfo = true
		*lastInfoName = r.Name
	case "model_change":
		if id := joinModel(r.Provider, r.ModelID); id != "" {
			s.Model = &model.Model{ID: id}
		}
	case "message":
		if r.Message == nil {
			return
		}
		if r.Message.Role != "assistant" {
			return
		}
		if r.Message.Model != "" {
			s.Model = &model.Model{ID: r.Message.Model}
		}
		if r.Message.Usage == nil {
			return
		}
		s.Tokens = &model.TokenCounts{
			Input:  r.Message.Usage.Input,
			Output: r.Message.Usage.Output,
		}
		// Last assistant wins. A later usage with no cost must not keep an
		// earlier message's figure under the same label.
		if r.Message.Usage.Cost != nil && r.Message.Usage.Cost.Total != nil && *r.Message.Usage.Cost.Total > 0 {
			setExtra(s, "message cost", formatUSD(*r.Message.Usage.Cost.Total))
		} else {
			clearExtra(s, "message cost")
		}
	}
}

func joinModel(provider, modelID string) string {
	switch {
	case provider != "" && modelID != "":
		return provider + "/" + modelID
	case modelID != "":
		return modelID
	default:
		return ""
	}
}

func parseTS(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return ts, true
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts, true
	}
	return time.Time{}, false
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

func clearExtra(s *model.Session, label string) {
	out := s.Extras[:0]
	for _, e := range s.Extras {
		if e.Label != label {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		s.Extras = nil
		return
	}
	s.Extras = out
}

// formatUSD renders four decimal places. A Pi message on this box measured
// $0.0139. Two places would render most messages as $0.01.
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
