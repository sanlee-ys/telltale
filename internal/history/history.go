// Package history answers one question the gauges deliberately cannot: what did
// a vendor spend, day by day, according to that vendor's own session files.
//
// # Why it is a mode and not a HUD page
//
// Every other reader in this repository answers about NOW. The statusline and
// the HUD render a scan; `snapshot` and `mcp` serve one scan to a program;
// `doctor` reports a preflight. A history is the one question none of them can
// hold: it is a walk over files the scan reads only the head and tail of
// (internal/adapter/claudecode's parse reads 64 KiB in and 256 KiB back, because
// a 1 s poll cannot afford 693 MB — design.md §7.18). Putting a whole-corpus walk
// behind a keypress in the HUD would put a multi-second read on a surface whose
// whole design is a 1 s tick. So this is a foreground mode that reads once and
// returns, on `doctor`'s precedent and `snapshot`'s.
//
// # What it is allowed to say (design.md §7.16, §7.17)
//
// This is a SPEND surface, and the vocabulary rules are §7.17's, not a new set:
//
//   - No gauge, no percentage, no bar, no countdown, no ceiling. There is no
//     denominator anywhere in a token count. Inventing one is the same class of
//     error as filling a CapNone field with a plausible guess.
//   - No total across vendors, ever (§7.17's "a fabricated fleet total"). Two
//     vendors' counts are different measurements of different things, and any
//     single number over them would be arithmetic telltale invented. This mode
//     reports ONE vendor per run and the report says which on every surface.
//   - No total across the four counts either, for a narrower version of the same
//     reason: input, cache read, cache write and output are four separately
//     billed categories, and telltale holds no price. Adding them would produce a
//     number that looks like a bill and is not one.
//   - A sum never prints without its window. Every count here carries the day it
//     belongs to, and the report carries the span it read.
//
// # Absent is not zero, on two axes
//
//   - A request whose usage block reported zeros is a MEASURED ZERO and renders
//     `0`. It is a request that happened.
//   - A day, or a workspace, with no token-bearing record gets NO ROW. It is not
//     rendered as a row of zeros, because a zero row asserts a request that never
//     happened. What makes a missing day readable is the window line: the report
//     states the span it walked, so a day inside that span with no row means
//     nothing was written that day, and the reader is told so in words.
//
// # What it reads, and what it derives
//
// It reads the same transcripts internal/adapter/claudecode reads, discovered by
// that adapter's own Discover — so a session this mode counts is a session the
// HUD would draw, and the two cannot come to disagree about what a session is.
// What it does NOT reuse is that adapter's head+tail parse: a history needs every
// record, so it walks the whole file through internal/jsonl.Scan.
//
// One thing here is derived and the report says so in words rather than with a
// `~`: the CALENDAR DAY. The vendor writes an instant; a day is that instant
// resolved in a time zone, and the zone is a choice telltale made. The report
// states the zone on the window line so the day column is never read as the
// vendor's own claim. A `~` is the marker for an estimated VALUE (§4a.1), and a
// day bucket is not an estimate — it is an exact reading under a stated
// convention, which is a different thing and takes a different disclosure.
//
// # The read/write boundary
//
// This mode writes nothing at all. It reads vendor stores, makes no network call,
// binds no port, reads no credential, and does not relay quota — it renders none.
// It joins statusline, hud, snapshot and mcp as a reader (CLAUDE.md's boundary
// section). Nothing in this package imports internal/eventsink or
// internal/eventview, and nothing here writes under ~/.telltale/.
//
// # Content never reaches a value here
//
// The record struct below is the allowlist, on internal/cursorhook's precedent:
// encoding/json drops every field with no destination, so no message text, no
// tool result and no title can reach a Report. The only strings that survive are
// the workspace path the vendor wrote in cwd and timestamps. Diagnostics carry
// counts and never bytes from a file.
package history

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/sanlee-ys/telltale/internal/adapter/claudecode"
	"github.com/sanlee-ys/telltale/internal/jsonl"
	"github.com/sanlee-ys/telltale/internal/model"
)

// DefaultDays is the window a bare `telltale history` reads.
//
// Seven, because it is the span the fleet's own slowest quota window is named
// for (§7.19) and therefore the span the operator already thinks in — not
// because a week is a natural unit for a token count. Nothing here is a quota
// and the window is stated on every run, so the default is a convenience and
// never a claim.
const DefaultDays = 7

// dayLayout is the day bucket's rendered form. ISO order so the column sorts as
// text, which is what lets the renderer stay pure over a []Row it did not build.
const dayLayout = "2006-01-02"

// futureSkew mirrors every adapter in this repository: a record stamped further
// than this ahead of the observation clock is not a readable time, so it cannot
// be dated. Bucketing it anyway would let a skewed clock invent a day's spend.
const futureSkew = 2 * time.Second

// syntheticModel is Claude Code's id for its own locally generated assistant
// records — API errors and interrupts. They carry a zeroed usage block, and
// counting one as a measured zero would put a request in the ledger that was
// never sent. internal/adapter/claudecode refuses them for the neighbouring
// reason (they must not reach the MODEL cell); this refuses them for this one.
const syntheticModel = "<synthetic>"

// Counts is one bucket's four raw token counts, exactly as the vendor wrote
// them. They are four numbers and never five: see the package doc on why no
// total across them is rendered.
type Counts struct {
	Input      int64
	CacheRead  int64
	CacheWrite int64
	Output     int64
}

func (c *Counts) add(o Counts) {
	c.Input += o.Input
	c.CacheRead += o.CacheRead
	c.CacheWrite += o.CacheWrite
	c.Output += o.Output
}

// Row is one (day, workspace) bucket.
//
// Workspace is empty when the token-bearing records in the bucket carried no cwd
// of their own. That is an absence and the renderer says so in words; it is not
// folded into a neighbouring workspace, because attributing a request to a
// project on the strength of a nearby record is a guess.
type Row struct {
	Day       string
	Workspace string
	Counts    Counts
	// Requests is the number of records that carried a usage block. It is
	// deliberately not called "turns": one turn can produce several API
	// requests, so "turns" would be a count telltale did not take.
	Requests int
	// Sessions is how many distinct transcripts contributed to this bucket.
	Sessions int
}

// Report is everything one run measured. Render is pure over it.
type Report struct {
	// Vendor is the one vendor this report speaks for. It is on the struct
	// rather than assumed by the renderer because the hard rule this mode is
	// built around is that no number here may ever be read as a fleet figure.
	Vendor string
	// Root is the directory that was walked, for the empty state.
	Root string
	// RootAbsent is true when that directory is not there at all — which is a
	// different statement from "it held nothing", and renders differently.
	RootAbsent bool
	// Zone is the time zone the day buckets were resolved in, rendered verbatim
	// so the derived day carries its convention.
	Zone string
	// Days, From and To are the window. They travel with every count.
	Days int
	From string
	To   string

	Rows []Row
	// Transcripts is how many session files were opened and walked.
	Transcripts int
	// Records is how many well-formed records were read across them.
	Records int

	// Diagnostics name what degraded. Counts and structure only, never bytes
	// from a vendor file — this repository is public.
	Diagnostics []string
	// Incomplete is set when the deadline ended the walk early. The rows are
	// still true about the files that were read; the WINDOW is no longer true
	// about the corpus, which is why this is a field and not a diagnostic
	// string the renderer might shed.
	Incomplete bool

	// Coverage is the survey, carried on the report so the rendered output can
	// never omit which vendors this mode does not speak for.
	Coverage []Coverage
}

// Query is what Read is asked for. Now is injected rather than read, so a test
// can pin a window and the day bucketing without a clock, and so the whole
// report is a pure function of (corpus, clock) — which is what lets Render stay
// pure over it.
//
// It is named Query rather than Options because Options is the RENDER's knob in
// this package (view.go), on internal/doctor's naming: a reader who sees both in
// one call is entitled to expect them to mean different things.
type Query struct {
	// Home is a substitute HOME DIRECTORY, not a vendor store path — the same
	// meaning `telltale hud --root` gives it, and deliberately so. Two flags
	// spelled --root that took different kinds of path would be the worst
	// possible pair: passing the wrong one resolves to a directory that does not
	// exist, and this mode would then report an absent store rather than an
	// error. Empty reads this machine's own home. `go run ./tools/demo-corpus`
	// writes a directory of this shape.
	Home string
	Days int
	Now  time.Time
}

// claudeStore is where the covered vendor's transcripts live under a home
// directory. It is stated once here and matches internal/democorpus.Adapters,
// which is the other place in this repository that resolves the same path.
var claudeStore = []string{".claude", "projects"}

// record is the subset of a transcript record this mode reads, and it is the
// content allowlist (see the package doc).
type record struct {
	Cwd         string `json:"cwd"`
	IsSidechain bool   `json:"isSidechain"`
	Timestamp   string `json:"timestamp"`
	Message     *struct {
		Model string `json:"model"`
		Usage *struct {
			InputTokens              int64 `json:"input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// bucket accumulates one (day, workspace) pair mid-walk.
type bucket struct {
	counts   Counts
	requests int
	sessions map[string]struct{}
}

type bucketKey struct{ day, workspace string }

// Read walks the covered vendor's session files and buckets every token-bearing
// record by day and by workspace.
//
// A failure to read ONE transcript degrades that transcript and nothing else: it
// is counted and named once in Diagnostics, and the walk continues. That is
// CLAUDE.md's "a partial read degrades a field, it does not fail the row",
// applied one level up — here the unit that must survive is the run.
func Read(ctx context.Context, o Query) (Report, error) {
	if o.Days <= 0 {
		o.Days = DefaultDays
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	loc := now.Location()

	a := claudecode.New()
	if o.Home != "" {
		a = claudecode.NewWithRoot(filepath.Join(append([]string{o.Home}, claudeStore...)...))
	}

	zone, offset := now.Zone()
	rep := Report{
		Vendor:   string(claudecode.Vendor),
		Root:     a.Root(),
		Zone:     zone + " " + utcOffset(offset),
		Days:     o.Days,
		Coverage: Survey(),
	}

	// The window is [first, end): whole local days, ending with today. Half-open
	// so "today" is included whatever the hour, and so a record stamped exactly
	// at midnight lands in the day it starts rather than in both.
	first := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(o.Days - 1))
	end := first.AddDate(0, 0, o.Days)
	rep.From = first.Format(dayLayout)
	rep.To = end.AddDate(0, 0, -1).Format(dayLayout)

	refs, err := a.Discover(ctx)
	if errors.Is(err, model.ErrVendorAbsent) {
		rep.RootAbsent = true
		return rep, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		// The deadline can land during the listing, before a byte is read. That
		// is the same event as landing mid-walk and takes the same answer: an
		// empty report that says its window is incomplete. Returning the context
		// error instead would print "context deadline exceeded" and no report at
		// all, which tells the reader nothing about what was or was not read.
		rep.Incomplete = true
		return rep, nil
	}
	if err != nil {
		return rep, err
	}
	// Deterministic order, so a run over the same corpus reads the same twice and
	// so a deadline truncates at a predictable place rather than wherever the
	// filesystem happened to enumerate.
	sort.Slice(refs, func(i, j int) bool { return refs[i].Locator < refs[j].Locator })

	buckets := map[bucketKey]*bucket{}
	var (
		unparseable int
		undated     int
		skewed      int
		sidechain   int
		unreadable  int
	)

	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			// The deadline is honest about what it cost: the rows already
			// collected stay, and the report says the window is no longer
			// complete. A run that returned nothing here would throw away real
			// readings to avoid admitting one.
			rep.Incomplete = true
			break
		}
		f, err := os.Open(ref.Locator)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				// A transcript that vanished between Discover and open is the
				// tree mutating under a sweep, which is normal and is not a
				// degradation. Anything else is.
				unreadable++
			}
			continue
		}
		rep.Transcripts++

		scanErr := jsonl.Scan(f, func(raw []byte) error {
			var r record
			if err := json.Unmarshal(raw, &r); err != nil {
				unparseable++
				return nil
			}
			rep.Records++
			if r.IsSidechain {
				// A sidechain record is a sub-agent's, and sub-agent transcripts
				// live in their own sidecar tree that Discover does not walk. One
				// appearing inline would be double counting, so it is refused and
				// the count is reported: the live corpus has 0 of 179,614, so a
				// non-zero here is a vendor change worth seeing.
				sidechain++
				return nil
			}
			if r.Message == nil || r.Message.Usage == nil {
				return nil
			}
			if r.Message.Model == syntheticModel {
				return nil
			}
			ts, err := time.Parse(time.RFC3339Nano, r.Timestamp)
			if err != nil {
				// Counts with no readable timestamp cannot be dated. They are
				// NOT folded into today: that would move a measurement to a day
				// nothing said it belonged to.
				undated++
				return nil
			}
			if ts.After(now.Add(futureSkew)) {
				skewed++
				return nil
			}
			local := ts.In(loc)
			if local.Before(first) || !local.Before(end) {
				// Outside the window is not a degradation. It is the question
				// the reader asked, answered.
				return nil
			}
			k := bucketKey{day: local.Format(dayLayout), workspace: r.Cwd}
			b := buckets[k]
			if b == nil {
				b = &bucket{sessions: map[string]struct{}{}}
				buckets[k] = b
			}
			b.counts.add(Counts{
				Input:      r.Message.Usage.InputTokens,
				CacheRead:  r.Message.Usage.CacheReadInputTokens,
				CacheWrite: r.Message.Usage.CacheCreationInputTokens,
				Output:     r.Message.Usage.OutputTokens,
			})
			b.requests++
			b.sessions[ref.ID] = struct{}{}
			return nil
		})
		f.Close()
		if scanErr != nil {
			// jsonl.Scan returns every read error it sees precisely so a
			// truncated read cannot be mistaken for a short file. Whatever this
			// file contributed before the error stays; the file is named as
			// degraded by count.
			unreadable++
		}
	}

	for k, b := range buckets {
		rep.Rows = append(rep.Rows, Row{
			Day:       k.day,
			Workspace: k.workspace,
			Counts:    b.counts,
			Requests:  b.requests,
			Sessions:  len(b.sessions),
		})
	}
	// Day ascending, then workspace: the newest day lands at the bottom of the
	// output, next to the prompt the reader is looking at. Sorting by SPEND was
	// refused for §7.17's reason — position is the navigation, and a table that
	// reshuffles when a count moves spends the reader's attention to encode
	// nothing the numbers do not already say.
	sort.Slice(rep.Rows, func(i, j int) bool {
		if rep.Rows[i].Day != rep.Rows[j].Day {
			return rep.Rows[i].Day < rep.Rows[j].Day
		}
		return rep.Rows[i].Workspace < rep.Rows[j].Workspace
	})

	rep.Diagnostics = diagnostics(unparseable, undated, skewed, sidechain, unreadable)
	return rep, nil
}

// diagnostics turns the walk's degradation counters into sentences. Each one
// names what was lost and how much of it; none of them names a file's contents.
func diagnostics(unparseable, undated, skewed, sidechain, unreadable int) []string {
	var out []string
	if unparseable > 0 {
		out = append(out, plural(unparseable,
			"unparseable record skipped", "unparseable records skipped"))
	}
	if undated > 0 {
		out = append(out, plural(undated,
			"record carried token counts and no readable timestamp, so it is in no day",
			"records carried token counts and no readable timestamp, so they are in no day"))
	}
	if skewed > 0 {
		out = append(out, plural(skewed,
			"record was stamped ahead of this clock and could not be dated",
			"records were stamped ahead of this clock and could not be dated"))
	}
	if sidechain > 0 {
		out = append(out, plural(sidechain,
			"sub-agent record found inline and left out, to avoid counting it twice",
			"sub-agent records found inline and left out, to avoid counting them twice"))
	}
	if unreadable > 0 {
		out = append(out, plural(unreadable,
			"transcript could not be read to the end; what it gave before that is counted",
			"transcripts could not be read to the end; what they gave before that is counted"))
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// utcOffset renders a zone offset as ±HH:MM. The zone NAME alone is ambiguous
// across regions ("EST" is not one offset worldwide), and the day column is
// derived from this offset, so the offset is the part that has to be on screen.
func utcOffset(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign, seconds = "-", -seconds
	}
	h := seconds / 3600
	m := (seconds % 3600) / 60
	return "UTC" + sign + pad2(h) + ":" + pad2(m)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
