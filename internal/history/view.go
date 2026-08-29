package history

import (
	"strconv"
	"strings"
)

// This file renders the history as plain text on stdout, and "plain text" is the
// design rather than a shortcut — the same argument internal/doctor's view.go
// makes, for the same two reasons.
//
// A history is read once and it is read in the two places a TUI cannot go: piped
// into a file, and pasted into a message by someone asking where a week went. So
// there is no alternate screen, no Bubble Tea, no lipgloss and no colour, which
// means every distinction this report draws is carried by a WORD. CLAUDE.md's
// "colour, and any single glyph, is always a second signal" is satisfied here by
// having no first signal that is not a word. --ascii and NO_COLOR have nothing to
// switch off, which is why neither is a flag on this mode: a flag that does
// nothing is a promise that something was configurable.
//
// Render is PURE over its Report — no time.Now, no filesystem, no env reads — for
// the reason council's and doctor's renders are (CLAUDE.md, golden-test traps).
// Everything measured, the observation clock included, is measured in Read and
// arrives here as data. That is what makes the golden stable.

// Options is the render's only knob.
type Options struct {
	// Width is the wrap column. Zero takes DefaultWidth.
	Width int
}

const (
	// DefaultWidth is wider than doctor's 80 because this surface is a TABLE
	// with eight columns, and the alternative to the extra twenty cells is
	// truncating a workspace path to the point where two repositories read the
	// same. Prose still wraps, so the report is readable when it is narrowed.
	DefaultWidth = 100
	// minWidth is the floor. Below it the numbers would start colliding, and a
	// table whose columns run together is worse than one that overruns.
	minWidth = 60
	// indent is the body's left margin, matching doctor's.
	indent = "  "
	// gap separates two columns. Two spaces, so a column boundary is visible
	// without a rule character claiming to be a border.
	gap = "  "
	// minWorkspace is the narrowest the workspace column may become before the
	// table simply overruns the wrap column. A path cut below this identifies
	// nothing, and an unreadable identifier is worse than a long line.
	minWorkspace = 14
	// noWorkspace fills the cell for a bucket whose records carried no cwd. It
	// is a WORD rather than an em dash because the footnote under the table
	// explains it, and a dash cannot be looked up.
	noWorkspace = "(no cwd)"
)

// column headers. They are the second input to every column's width, so a header
// longer than its widest value still fits.
const (
	hDay        = "DAY"
	hWorkspace  = "WORKSPACE"
	hIn         = "IN"
	hCacheRead  = "CACHE READ"
	hCacheWrite = "CACHE WRITE"
	hOut        = "OUT"
	hRequests   = "REQUESTS"
	hSessions   = "SESSIONS"
)

// Render draws the whole report.
func Render(r Report, o Options) string {
	width := o.Width
	if width <= 0 {
		width = DefaultWidth
	}
	if width < minWidth {
		width = minWidth
	}

	var b strings.Builder
	// The title wraps like every other paragraph. It names the vendor twice on
	// purpose: the first says whose spend this is, the second says where the
	// numbers came from, and those are the two claims a reader has to carry down
	// the page for the coverage block at the bottom to mean anything.
	writeWrapped(&b, "", "telltale history — what "+r.Vendor+" spent, day by day, read from "+
		r.Vendor+"'s own session files", width)
	b.WriteString("\n")

	writeFacts(&b, r, width)
	b.WriteString("\n")

	switch {
	case r.RootAbsent:
		// Not "you have spent nothing". The store is not here, so nothing was
		// read, and those are different statements — §4a.1's rule applied to a
		// whole vendor instead of to a cell.
		writeWrapped(&b, indent, "there is no "+r.Vendor+" session store on this machine to read, "+
			"so this report measured nothing. It is not a claim that nothing was spent.", width)
	case len(r.Rows) == 0:
		writeWrapped(&b, indent, "no record in this window carried a token count. "+
			"That is a measured result over the "+plural(r.Transcripts, "transcript", "transcripts")+
			" walked, not a store telltale could not find.", width)
	default:
		writeTable(&b, r, width)
	}

	if len(r.Diagnostics) > 0 {
		b.WriteString("\n")
		for _, d := range r.Diagnostics {
			writeWrapped(&b, indent, d, width)
		}
	}
	if r.Incomplete {
		b.WriteString("\n")
		// Loud, and its own paragraph: the ROWS are still true and the WINDOW is
		// not, which is the one sentence a reader must not miss.
		writeWrapped(&b, indent, "the deadline ended this walk early. Every count above is real; "+
			"the window is not complete, so treat each day as a lower bound and re-run with a longer --timeout.", width)
	}

	// The two refusals speak about counts, so they print only when there are
	// counts. "Every count above is claude's alone" over an empty table is a
	// sentence about nothing, and a report that says it anyway teaches a reader
	// to skim past the paragraph on the runs where it matters.
	if len(r.Rows) > 0 {
		b.WriteString("\n")
		writeRules(&b, r, width)
	}
	// The coverage block prints unconditionally, including on an empty report.
	// An empty table is exactly where a reader is most likely to conclude the
	// FLEET spent nothing, so this is the run it matters most on.
	b.WriteString("\n")
	writeCoverage(&b, r, width)
	return b.String()
}

// writeFacts draws the three provenance lines. They are a label column and a
// value, on the detail pane's shape, so a reader can tell a fact about the READ
// from a fact about the spend before reading a word of either.
func writeFacts(b *strings.Builder, r Report, width int) {
	const label = 12
	fact := func(k, v string) {
		writeWrapped(b, indent+pad(k, label), v, width)
	}
	root := r.Root
	if root == "" {
		root = "(no store path could be resolved on this machine)"
	}
	fact("read from", root)
	// The window and the zone travel together and always: a day column is a
	// derived bucket, and the zone is the convention that derived it (see the
	// package doc on why this is a stated convention and not a `~`).
	fact("window", strconv.Itoa(r.Days)+" local "+plural2(r.Days, "day", "days")+", "+
		r.From+" through "+r.To+", days resolved in "+r.Zone)
	// Grouped, like every count in the table below: one number in this report
	// rendered a different way than the others is a number a reader has to stop
	// and re-read.
	fact("read", group(int64(r.Transcripts))+" "+plural2(r.Transcripts, "transcript", "transcripts")+
		", "+group(int64(r.Records))+" "+plural2(r.Records, "record", "records"))
}

// writeTable draws the ledger.
//
// Column widths are computed from the values actually present rather than fixed,
// so a quiet week does not print eleven-cell columns of whitespace — and the
// WORKSPACE column absorbs whatever is left, because it is the only cell whose
// truncation costs the reader something recoverable (the path's tail survives,
// and the tail is the identifying half).
func writeTable(b *strings.Builder, r Report, width int) {
	wIn, wCR, wCW, wOut, wReq, wSes := len(hIn), len(hCacheRead), len(hCacheWrite),
		len(hOut), len(hRequests), len(hSessions)
	for _, row := range r.Rows {
		wIn = max(wIn, len(group(row.Counts.Input)))
		wCR = max(wCR, len(group(row.Counts.CacheRead)))
		wCW = max(wCW, len(group(row.Counts.CacheWrite)))
		wOut = max(wOut, len(group(row.Counts.Output)))
		wReq = max(wReq, len(group(int64(row.Requests))))
		wSes = max(wSes, len(group(int64(row.Sessions))))
	}
	numeric := (wIn + wCR + wCW + wOut + wReq + wSes) + 6*len(gap)
	wWork := width - len(indent) - len(dayLayout) - len(gap) - numeric
	if wWork < minWorkspace {
		wWork = minWorkspace
	}

	line := func(day, work, in, cr, cw, out, req, ses string) {
		b.WriteString(indent + pad(day, len(dayLayout)) + gap + pad(work, wWork) +
			gap + lpad(in, wIn) + gap + lpad(cr, wCR) + gap + lpad(cw, wCW) +
			gap + lpad(out, wOut) + gap + lpad(req, wReq) + gap + lpad(ses, wSes))
		b.WriteString("\n")
	}
	line(hDay, hWorkspace, hIn, hCacheRead, hCacheWrite, hOut, hRequests, hSessions)

	anonymous := false
	prevDay := ""
	for _, row := range r.Rows {
		work := row.Workspace
		if work == "" {
			work = noWorkspace
			anonymous = true
		}
		day := row.Day
		if day == prevDay {
			// A repeated day is drawn once. Two projects on one day are two rows
			// of one day, not two days — and restating the date makes the eye
			// read a second reading where there is one.
			day = ""
		}
		prevDay = row.Day
		line(day, fitLeft(work, wWork),
			group(row.Counts.Input), group(row.Counts.CacheRead),
			group(row.Counts.CacheWrite), group(row.Counts.Output),
			group(int64(row.Requests)), group(int64(row.Sessions)))
	}

	b.WriteString("\n")
	// The absent-is-not-zero sentence is printed on every run that draws a table,
	// on doctor's legend precedent: the whole point of the distinction is that a
	// reader who has not been told it will read a missing day as a zero one.
	writeWrapped(b, indent, "A day inside the window with no row carried no token-bearing record. "+
		"It is not drawn as a row of zeros: a request that reported zero counts renders 0, "+
		"and a request that never happened renders nothing at all.", width)
	if anonymous {
		writeWrapped(b, indent, noWorkspace+" is a request whose own record named no working directory. "+
			"It is not folded into a neighbouring project, because attributing it on the strength "+
			"of a nearby record would be a guess.", width)
	}
}

// writeRules states the two sums this mode refuses. Both are printed every run,
// because both are the kind of arithmetic a reader will otherwise do by hand off
// the numbers above and attribute to telltale.
func writeRules(b *strings.Builder, r Report, width int) {
	writeWrapped(b, indent, "Every count above is "+r.Vendor+"'s own and "+r.Vendor+"'s alone. "+
		"No line here sums two vendors: their counts are different measurements of different "+
		"things, and one number over them would be arithmetic telltale invented.", width)
	writeWrapped(b, indent, "The four columns are not added either. Input, cache read, cache write "+
		"and output are billed as four separate categories and telltale holds no price, so a total "+
		"across them would look like a bill and would not be one.", width)
}

// writeCoverage names every vendor this mode does not report, and why.
//
// It prints on every run rather than under a flag, and that is the whole reason
// this block exists: a table headed with one vendor's name, read quickly, becomes
// a fleet answer. Naming the other six in the same frame is what stops it.
func writeCoverage(b *strings.Builder, r Report, width int) {
	rows := r.Coverage
	if len(rows) == 0 {
		// A Report built by a caller that forgot the field still gets the block.
		// The one output this surface may never produce is silence about a
		// vendor, so the fallback is the package's own table rather than an
		// omission — and reading a package-level constant keeps Render pure.
		rows = Survey()
	}
	var covered []string
	for _, c := range rows {
		if c.Covered {
			covered = append(covered, string(c.Vendor))
		}
	}
	writeWrapped(b, indent, "covered today: "+strings.Join(covered, ", ")+
		". Every other vendor in the fleet is surveyed and not yet read:", width)
	b.WriteString("\n")
	const label = 10
	for _, c := range rows {
		if c.Covered {
			continue
		}
		writeWrapped(b, indent+indent+pad(string(c.Vendor), label), c.Why, width)
	}
}

// writeWrapped draws text at width, with the first line after prefix and every
// continuation line indented to the same column. A prefix of spaces plus a label
// therefore produces a hanging indent with no second mechanism.
func writeWrapped(b *strings.Builder, prefix, text string, width int) {
	cont := strings.Repeat(" ", len([]rune(prefix)))
	avail := width - len([]rune(prefix))
	if avail < 20 {
		avail = 20
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		b.WriteString(strings.TrimRight(prefix, " ") + "\n")
		return
	}
	cur := prefix
	curLen := 0
	for _, w := range words {
		wl := len([]rune(w))
		if curLen > 0 && curLen+1+wl > avail {
			b.WriteString(cur + "\n")
			cur, curLen = cont, 0
		}
		if curLen > 0 {
			cur += " "
			curLen++
		}
		cur += w
		curLen += wl
	}
	b.WriteString(cur + "\n")
}

// group renders an exact count with thousands separators.
//
// It does NOT floor to "1.9M" the way theme.Tokens does on the gauge surfaces,
// and the difference is deliberate rather than an oversight. Flooring exists
// there because a header line has no room for the digits and rounding UP would
// invent tokens nobody was billed for. This is a table with the room, so it
// rounds nothing at all — which is strictly the more honest of the two, and is
// only affordable here.
func group(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

// fitLeft truncates a path from the LEFT, keeping the tail. A workspace path's
// identifying half is its end — three repositories under one code root differ in
// the last segment and agree in every earlier one — so cutting the head is the
// cut that leaves the cell readable. The marker is at the front, where it says
// "something was removed here" before the reader has read the value.
func fitLeft(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w <= 3 {
		return string(r[len(r)-w:])
	}
	return "..." + string(r[len(r)-(w-3):])
}

func pad(s string, w int) string {
	if n := w - len([]rune(s)); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func lpad(s string, w int) string {
	if n := w - len([]rune(s)); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// plural2 is plural() without the leading count, for a phrase that has already
// stated the number.
func plural2(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
