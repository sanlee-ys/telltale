package eventview

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sanlee-ys/telltale/internal/eventsink"
)

// This file renders a listing as plain text on stdout, and "plain text, no
// colour" is the design rather than a shortcut. internal/doctor made the same
// call for the same reasons and its file says so at length; the two that
// carry here are that this output is read in places a TUI cannot go (piped to
// a file, pasted into an issue while asking why a hook stored nothing) and
// that every distinction it makes is therefore carried by a WORD. That
// satisfies CLAUDE.md's "colour, and any single glyph, is always a second
// signal" by having no first signal that is not a word. NO_COLOR and --ascii
// have nothing to switch off here, which is why neither is a flag on this
// mode: a flag that does nothing is a promise that something was
// configurable.
//
// Render is PURE over its Listing. No time.Now, no filesystem, no env reads.
// The clock this mode shows is the one stamped on each stored row, and the
// store path arrives as data. That is the same rule internal/hud and
// internal/council render under, and it is what makes the tests below able to
// assert an exact string.

// headerWhen labels the time column. The column shows UTC because the store
// itself is organized in UTC days, and a listing whose rows read in local time
// while --day selects a UTC file would put the reader's evening events in
// "tomorrow".
const headerWhen = "when (UTC)"

// absent is the word for a field the row does not carry. It is the render
// half of the rule this repo exists to keep: absent is not zero. A row whose
// timestamp is missing must not print as 1970-01-01, which reads as a
// measurement and would be one of the few dates a reader is certain is wrong
// only if they happen to look. See TestAnAbsentTimestampIsNotNineteenSeventy.
const absent = "absent"

// Options is what the render needs and the Listing does not carry.
type Options struct {
	// Dir is the store the listing was read from, printed so an empty result
	// still says where the reader looked.
	Dir string

	// ShowPayload prints each row's stored payload, and its error text, under
	// the row. It is off by default because those are the two fields that
	// carry hook CONTENT verbatim: the tool input a command ran with, the file
	// path it touched, the text an error carried. The row above names that
	// they exist; this flag is the reader asking for the bodies.
	ShowPayload bool
}

// Widths are the column widths one listing renders at.
type Widths struct {
	ID, When, Source, Session, Type int

	// Detail is true when at least one row has a promoted field to show, so
	// the header only names a column that has something under it.
	Detail bool
}

// WidthsFor sizes the columns to the rows they will hold, never smaller than
// the header labels.
//
// Nothing is ever truncated to fit, and that is the one layout rule worth
// arguing. The widest column is the session id, which is a 36-character UUID
// for every vendor measured, and a session id is the field a reader carries
// somewhere else: into `--session`, into a vendor's own logs, into an issue.
// A clipped one is a session nobody can correlate. internal/doctor states the
// same rule about paths. So a long value makes the line long, and follow mode
// keeps the widths it started with rather than redrawing rows already on
// screen: a later value wider than its column pushes the rest of its own line
// right and costs the alignment of that line only.
func WidthsFor(events []eventsink.Event) Widths {
	w := Widths{
		ID:      width("id"),
		When:    width(headerWhen),
		Source:  width("source"),
		Session: width("session"),
		Type:    width("type"),
	}
	for _, e := range events {
		w.ID = max(w.ID, width(idText(e)))
		w.When = max(w.When, width(When(e)))
		w.Source = max(w.Source, width(orAbsent(e.SourceApp)))
		w.Session = max(w.Session, width(orAbsent(e.SessionID)))
		w.Type = max(w.Type, width(orAbsent(e.HookEventType)))
		if len(Detail(e)) > 0 {
			w.Detail = true
		}
	}
	return w
}

// Render draws a whole listing: a header saying what was found and where, then
// the rows.
func Render(l Listing, o Options) string {
	var b strings.Builder
	switch {
	case l.Diag.StoreMissing:
		fmt.Fprintf(&b, "telltale events view — no store at %s\n", o.Dir)
		b.WriteString(noStoreNote)
		return b.String()

	case l.Diag.Records == 0:
		// The store exists and holds nothing. Distinct from the case above on
		// purpose: "the sink has never run here" and "the sink ran and stored
		// nothing" are different answers, and one empty screen for both is the
		// collapse this repo's honesty rules refuse.
		fmt.Fprintf(&b, "telltale events view — the store is here and holds no event\n")
		b.WriteString(storeLine(l, o.Dir))
		b.WriteString(emptyStoreNote)
		return b.String()

	case l.Matched == 0:
		fmt.Fprintf(&b, "telltale events view — no event matched, out of %d read\n", l.Diag.Records)
		b.WriteString(storeLine(l, o.Dir))
		b.WriteString(optionsBlock(l.Options))
		return b.String()
	}

	fmt.Fprintf(&b, "telltale events view — %d of %d matching %s, newest first\n",
		len(l.Events), l.Matched, plural(l.Matched, "event", "events"))
	b.WriteString(storeLine(l, o.Dir))
	if !o.ShowPayload {
		b.WriteString(keysOnlyNote)
	}
	b.WriteByte('\n')

	w := WidthsFor(l.Events)
	b.WriteString(Header(w))
	for _, e := range l.Events {
		b.WriteString(Row(e, w, o.ShowPayload))
	}
	return b.String()
}

// Header labels the columns.
func Header(w Widths) string {
	line := pad("id", w.ID) + pad(headerWhen, w.When) + pad("source", w.Source) +
		pad("session", w.Session) + pad("type", w.Type)
	if w.Detail {
		line += "detail"
	}
	return strings.TrimRight(line, " ") + "\n"
}

// Row draws one event, and its payload block when the reader asked for it.
func Row(e eventsink.Event, w Widths, showPayload bool) string {
	line := pad(idText(e), w.ID) + pad(When(e), w.When) + pad(orAbsent(e.SourceApp), w.Source) +
		pad(orAbsent(e.SessionID), w.Session) + pad(orAbsent(e.HookEventType), w.Type) +
		strings.Join(Detail(e), " ")

	var b strings.Builder
	b.WriteString(strings.TrimRight(line, " "))
	b.WriteByte('\n')
	if !showPayload {
		return b.String()
	}
	if e.Error != "" {
		fmt.Fprintf(&b, "%s%s%s\n", bodyIndent, pad("error", bodyLabel), e.Error)
	}
	// One line, always: JSONL frames a record on the 0x0A byte, so a stored
	// payload cannot contain a raw newline and no wrapping decision arises.
	// The bytes print exactly as the sink stored them, with no re-indent and
	// no re-encode. This is the surface that shows measured content, and a
	// prettier spelling of it is still a spelling telltale chose.
	fmt.Fprintf(&b, "%s%s%s\n", bodyIndent, pad("payload", bodyLabel), payloadText(e))
	return b.String()
}

const (
	bodyIndent = "    "
	bodyLabel  = 9
)

// FollowBanner is the line follow mode prints before it starts waiting.
//
// It names the interval because the interval IS the latency: this mode polls
// the day files rather than subscribing to the sink's /stream, so "live" here
// means "within one interval", and a banner that said only "following" would
// be claiming a push it does not have. See this package's doc for why the
// file is the source anyway.
func FollowBanner(dir string, every time.Duration, storeMissing bool) string {
	s := fmt.Sprintf("following %s, re-reading every %s. New events print as they land; Ctrl-C stops.\n", dir, every)
	if storeMissing {
		s += "That directory does not exist yet, so nothing has been stored on this machine. " +
			"This keeps watching: it appears when `telltale events` first runs.\n"
	}
	return s
}

// idText renders the sink's arrival id. The # is not decoration: without it a
// bare small integer in the first column reads as a count of something.
func idText(e eventsink.Event) string { return "#" + fmt.Sprint(e.ID) }

// When renders the stamp the row carries, in UTC.
//
// A zero stamp is ABSENT, not 1970. The sink stamps its own arrival time onto
// any POST that carries none, so a stored row normally has one; a row without
// one came from something that wrote the day file directly, which §7.24
// measured as possible and which this reader must therefore render honestly
// rather than assume away.
func When(e eventsink.Event) string {
	if e.TimestampMS == 0 {
		return absent
	}
	return time.UnixMilli(e.TimestampMS).UTC().Format("2006-01-02 15:04:05") + "Z"
}

// Detail renders the promoted fields the emitter lifted out of the payload so
// a reader could filter without parsing every body.
//
// Two rules decide what appears here. A field the row does not carry prints
// NOTHING rather than an empty token, and a field that carries a measured
// false prints `stop-hook=false`, so the two states stay different in the
// rendered string and not only in the struct.
//
// And `error` appears as a bare word, never as its text. The other promoted
// fields are keys: a tool name, an id, an agent type. An error message is
// free text the hook was handed, which makes it content in the same sense the
// payload is, and content belongs behind --payload. The word still tells the
// reader the row has one, which is the part that decides whether they ask.
func Detail(e eventsink.Event) []string {
	var out []string
	if e.ToolName != "" {
		out = append(out, "tool="+e.ToolName)
	}
	if e.ToolUseID != "" {
		out = append(out, "tool-use="+e.ToolUseID)
	}
	if e.AgentType != "" {
		out = append(out, "agent-type="+e.AgentType)
	}
	if e.AgentID != "" {
		out = append(out, "agent="+e.AgentID)
	}
	if e.StopHookActive != nil {
		out = append(out, fmt.Sprintf("stop-hook=%t", *e.StopHookActive))
	}
	if e.Error != "" {
		out = append(out, "error")
	}
	return out
}

func payloadText(e eventsink.Event) string {
	if len(e.Payload) == 0 {
		return absent
	}
	return string(e.Payload)
}

// storeLine says where the listing came from and how the read went. The
// skipped count is printed only when there is one, and it is printed loudly
// when there is: unreadable lines are the difference between a quiet fleet and
// a store this reader could not read.
func storeLine(l Listing, dir string) string {
	s := fmt.Sprintf("store: %s (%d day %s, %d %s read",
		dir, l.Diag.Files, plural(l.Diag.Files, "file", "files"),
		l.Diag.Records, plural(l.Diag.Records, "record", "records"))
	if l.Diag.Skipped > 0 {
		s += fmt.Sprintf(", %d unreadable %s skipped",
			l.Diag.Skipped, plural(l.Diag.Skipped, "line", "lines"))
	}
	return s + ")\n"
}

// optionsBlock names what the store does hold, so an empty result is a next
// step rather than a dead end. These are the same three axes the sink serves
// at /events/filter-options, computed here from the files.
func optionsBlock(o eventsink.FilterOptions) string {
	var b strings.Builder
	b.WriteString("\nThe store holds these values. Any of them can go in the matching flag:\n")
	for _, row := range []struct {
		label  string
		values []string
	}{
		{"--source", o.SourceApps},
		{"--session", o.SessionIDs},
		{"--type", o.HookEventTypes},
	} {
		b.WriteString("  " + pad(row.label, 11))
		if len(row.values) == 0 {
			// Every stored row carries all three axes, because the sink's own
			// Validate refuses a row missing one. An empty set here therefore
			// means the rows read were written around that check, and saying
			// so is more use than printing a blank.
			b.WriteString("none, on any row read\n")
			continue
		}
		b.WriteString(strings.Join(row.values, ", ") + "\n")
	}
	return b.String()
}

const keysOnlyNote = "Keys only: each row's payload is stored verbatim and prints with --payload.\n"

const noStoreNote = "The sink creates that directory when it first runs, so nothing has stored an\n" +
	"event on this machine. Start the sink with `telltale events`, wire an emitter as a\n" +
	"hook command of the shape `python3 <path>/tools/emit-event.py --source-app <name>`,\n" +
	"then run `telltale events view` again.\n"

const emptyStoreNote = "The sink has run here and stored nothing yet. If a hook should have fired, check\n" +
	"the emitter first: it prints one line on stderr and exits 0 when it cannot reach\n" +
	"the sink, by design, so a miswired hook is silent rather than loud.\n"

func orAbsent(s string) string {
	if s == "" {
		return absent
	}
	return s
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// pad left-aligns a value in its column and always leaves the two-space gutter,
// even when the value overflows. A column that runs long pushes the rest of
// its line right; nothing is cut. See WidthsFor for why.
func pad(s string, n int) string {
	if width(s) >= n {
		return s + "  "
	}
	return s + strings.Repeat(" ", n-width(s)) + "  "
}

// width counts runes, not bytes, for the same reason internal/doctor's does:
// enough that a non-ASCII source app name cannot silently push a column, and
// not a display-width library, because this mode stays clear of the TUI stack.
func width(s string) int { return utf8.RuneCountInString(s) }
