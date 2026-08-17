package eventview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/eventsink"
)

// Every fixture here is synthesized. Fake source apps, fake session ids, fake
// commands, and a payload whose only content is a marker this file planted.
// CLAUDE.md's rule: no real session content belongs in this repository, and
// the event store is the one store that would carry it.

const (
	payloadMarker = "synthesized-payload-marker"
	errorMarker   = "synthesized-error-marker"
)

// baseMS is a fixed stamp so every expected string in this file is exact.
var baseMS = time.Date(2026, 8, 16, 20, 38, 2, 0, time.UTC).UnixMilli()

func ev(id int64, app, session, typ string, ms int64) eventsink.Event {
	return eventsink.Event{
		ID:            id,
		SourceApp:     app,
		SessionID:     session,
		HookEventType: typ,
		TimestampMS:   ms,
		Payload:       json.RawMessage(`{"tool_input":{"command":"echo ` + payloadMarker + `"}}`),
	}
}

func line(t *testing.T, e eventsink.Event) string {
	t.Helper()
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw) + "\n"
}

func writeDay(t *testing.T, dir, day string, events ...eventsink.Event) {
	t.Helper()
	var b strings.Builder
	for _, e := range events {
		b.WriteString(line(t, e))
	}
	if err := os.WriteFile(filepath.Join(dir, day+".jsonl"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendRaw(t *testing.T, dir, day, text string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, day+".jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func ids(events []eventsink.Event) []int64 {
	out := make([]int64, len(events))
	for i, e := range events {
		out[i] = e.ID
	}
	return out
}

func sameIDs(got []eventsink.Event, want ...int64) bool {
	g := ids(got)
	if len(g) != len(want) {
		return false
	}
	for i := range g {
		if g[i] != want[i] {
			return false
		}
	}
	return true
}

func TestReadListsNewestFirstAndTrimsToTheLimit(t *testing.T) {
	dir := t.TempDir()
	writeDay(t, dir, "2026-08-15",
		ev(1, "app-alpha", "sess-0001", "PreToolUse", baseMS),
		ev(2, "app-alpha", "sess-0001", "PostToolUse", baseMS+1000))
	writeDay(t, dir, "2026-08-16",
		ev(3, "app-beta", "sess-0002", "Stop", baseMS+2000),
		ev(4, "app-beta", "sess-0002", "PreToolUse", baseMS+3000))

	all, err := Read(dir, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if !sameIDs(all.Events, 4, 3, 2, 1) {
		t.Fatalf("want newest first 4,3,2,1 — got %v", ids(all.Events))
	}
	if all.Matched != 4 || all.Diag.Files != 2 || all.Diag.Records != 4 {
		t.Fatalf("want 4 matched over 2 files and 4 records, got %d/%d/%d",
			all.Matched, all.Diag.Files, all.Diag.Records)
	}

	// The limit trims the OLDEST, and Matched still reports the whole match so
	// a listing can say "2 of 4" rather than implying 2 was all there was.
	trimmed, err := Read(dir, Filter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !sameIDs(trimmed.Events, 4, 3) {
		t.Fatalf("want the two newest — got %v", ids(trimmed.Events))
	}
	if trimmed.Matched != 4 {
		t.Fatalf("a trimmed listing must still report 4 matching, got %d", trimmed.Matched)
	}
}

func TestFiltersSelectByEachTagAxis(t *testing.T) {
	dir := t.TempDir()
	writeDay(t, dir, "2026-08-16",
		ev(1, "app-alpha", "sess-0001", "PreToolUse", baseMS),
		ev(2, "app-beta", "sess-0001", "PostToolUse", baseMS+1000),
		ev(3, "app-beta", "sess-0002", "PreToolUse", baseMS+2000))

	for _, tc := range []struct {
		name   string
		filter Filter
		want   []int64
	}{
		{"source", Filter{Sources: []string{"app-beta"}}, []int64{3, 2}},
		{"session", Filter{Sessions: []string{"sess-0002"}}, []int64{3}},
		{"type", Filter{Types: []string{"PreToolUse"}}, []int64{3, 1}},
		{"two axes at once", Filter{Sources: []string{"app-beta"}, Types: []string{"PreToolUse"}}, []int64{3}},
		{"a list matches any member", Filter{Sources: []string{"app-alpha", "app-beta"}}, []int64{3, 2, 1}},
		{"no match", Filter{Sources: []string{"app-gamma"}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Read(dir, tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			if !sameIDs(got.Events, tc.want...) {
				t.Fatalf("want %v — got %v", tc.want, ids(got.Events))
			}
		})
	}
}

// TestFilterMatchingIgnoresLetterCase pins the reason stated on Matches: a
// case-sensitive miss and an empty store render identically, so the stricter
// rule would turn a typo into a false claim that nothing happened.
func TestFilterMatchingIgnoresLetterCase(t *testing.T) {
	dir := t.TempDir()
	writeDay(t, dir, "2026-08-16", ev(1, "App-Alpha", "sess-0001", "PreToolUse", baseMS))

	for _, typed := range []string{"pretooluse", "PRETOOLUSE", "PreToolUse"} {
		got, err := Read(dir, Filter{Types: []string{typed}})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Events) != 1 {
			t.Fatalf("--type %s must match the stored PreToolUse row, got %d rows", typed, len(got.Events))
		}
	}
	got, err := Read(dir, Filter{Sources: []string{"app-alpha"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 1 {
		t.Fatalf("--source app-alpha must match the stored App-Alpha row, got %d rows", len(got.Events))
	}
}

// TestDayFilterSelectsTheFileNotTheStamp pins the semantic wantsDay documents.
// The fixture is the case that separates the two: a row stored in the 08-16
// file carrying a stamp from 08-17, which is what a hook firing either side of
// UTC midnight, or a sender with a wrong clock, actually produces.
func TestDayFilterSelectsTheFileNotTheStamp(t *testing.T) {
	dir := t.TempDir()
	nextDayMS := time.Date(2026, 8, 17, 4, 0, 0, 0, time.UTC).UnixMilli()
	writeDay(t, dir, "2026-08-16", ev(1, "app-alpha", "sess-0001", "Stop", nextDayMS))

	inFile, err := Read(dir, Filter{Day: "2026-08-16"})
	if err != nil {
		t.Fatal(err)
	}
	if !sameIDs(inFile.Events, 1) {
		t.Fatalf("--day must select the file the sink wrote, got %v", ids(inFile.Events))
	}
	byStamp, err := Read(dir, Filter{Day: "2026-08-17"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byStamp.Events) != 0 {
		t.Fatalf("--day must not select on the emitter's stamp, got %v", ids(byStamp.Events))
	}
	if byStamp.Diag.Files != 0 {
		t.Fatalf("--day 2026-08-17 must read no file at all, read %d", byStamp.Diag.Files)
	}
}

func TestAMalformedDaySaysWhatItWanted(t *testing.T) {
	if err := (Filter{Day: "16-08-2026"}).Validate(); err == nil {
		t.Fatal("a day that is not YYYY-MM-DD must be refused, not silently matched against no file")
	}
	if _, err := Read(t.TempDir(), Filter{Day: "yesterday"}); err == nil {
		t.Fatal("Read must apply Validate rather than returning an empty listing")
	}
}

// TestAnAbsentTimestampIsNotNineteenSeventy is the zero-vs-absent rule on this
// surface. A row with no stamp has no reading, and epoch-millisecond zero
// rendered through a date formatter produces 1970-01-01, which reads exactly
// like a measurement.
func TestAnAbsentTimestampIsNotNineteenSeventy(t *testing.T) {
	stamped := ev(1, "app-alpha", "sess-0001", "PreToolUse", baseMS)
	unstamped := ev(2, "app-alpha", "sess-0001", "PreToolUse", 0)

	if got := When(stamped); got != "2026-08-16 20:38:02Z" {
		t.Fatalf("a stamped row must render its own UTC stamp, got %q", got)
	}
	if got := When(unstamped); got != absent {
		t.Fatalf("an unstamped row must render %q, got %q", absent, got)
	}

	w := WidthsFor([]eventsink.Event{stamped, unstamped})
	row := Row(unstamped, w, false)
	if strings.Contains(row, "1970") {
		t.Fatalf("an absent stamp rendered as a 1970 date: %q", row)
	}
	if Row(stamped, w, false) == row {
		t.Fatal("a stamped row and an unstamped row must not render identically")
	}
}

// TestAMeasuredFalseIsNotAnAbsentField is the same rule on the one boolean the
// emitter promotes. `stop_hook_active: false` is a reading; a row without the
// key is not.
func TestAMeasuredFalseIsNotAnAbsentField(t *testing.T) {
	no := false
	yes := true
	measuredFalse := ev(1, "app-alpha", "sess-0001", "Stop", baseMS)
	measuredFalse.StopHookActive = &no
	measuredTrue := ev(2, "app-alpha", "sess-0001", "Stop", baseMS)
	measuredTrue.StopHookActive = &yes
	noReading := ev(3, "app-alpha", "sess-0001", "Stop", baseMS)

	if got := Detail(measuredFalse); len(got) != 1 || got[0] != "stop-hook=false" {
		t.Fatalf("a measured false must render as a value, got %v", got)
	}
	if got := Detail(measuredTrue); len(got) != 1 || got[0] != "stop-hook=true" {
		t.Fatalf("a measured true must render as a value, got %v", got)
	}
	if got := Detail(noReading); len(got) != 0 {
		t.Fatalf("a field the row does not carry must render nothing, got %v", got)
	}

	w := WidthsFor([]eventsink.Event{measuredFalse, noReading})
	if Row(measuredFalse, w, false) == Row(noReading, w, false) {
		t.Fatal("stop-hook=false and an absent stop_hook_active rendered the same row")
	}
}

func TestAnAbsentTagAxisRendersAsAbsent(t *testing.T) {
	// The sink's own Validate refuses a row missing a tag axis, so this row
	// can only arrive by a direct write to the day file. design.md §7.24
	// measured that a local program can do exactly that, which is why the
	// reader renders the state rather than assuming it away.
	planted := eventsink.Event{ID: 1, SessionID: "sess-0001", HookEventType: "Stop", TimestampMS: baseMS}
	w := WidthsFor([]eventsink.Event{planted})
	row := Row(planted, w, false)
	if !strings.Contains(row, absent) {
		t.Fatalf("a row with no source_app must say so: %q", row)
	}
}

// TestThePayloadIsWithheldUntilAsked is the content boundary on this surface.
// The default listing prints keys; the payload and the error text are hook
// content and appear only when the reader asks for them.
func TestThePayloadIsWithheldUntilAsked(t *testing.T) {
	e := ev(1, "app-alpha", "sess-0001", "PostToolUse", baseMS)
	e.ToolName = "Bash"
	e.Error = errorMarker
	w := WidthsFor([]eventsink.Event{e})

	quiet := Row(e, w, false)
	if strings.Contains(quiet, payloadMarker) {
		t.Fatalf("the payload body reached the default row: %q", quiet)
	}
	if strings.Contains(quiet, errorMarker) {
		t.Fatalf("the error text reached the default row: %q", quiet)
	}
	if !strings.Contains(quiet, "error") {
		t.Fatalf("the default row must still say the row HAS an error: %q", quiet)
	}
	if !strings.Contains(quiet, "tool=Bash") {
		t.Fatalf("a promoted key must appear on the default row: %q", quiet)
	}

	asked := Row(e, w, true)
	if !strings.Contains(asked, payloadMarker) || !strings.Contains(asked, errorMarker) {
		t.Fatalf("--payload must print the payload and the error text: %q", asked)
	}
	// Verbatim means byte for byte, not re-encoded or re-indented.
	if !strings.Contains(asked, string(e.Payload)) {
		t.Fatalf("--payload must print the stored bytes unchanged: %q", asked)
	}
}

// TestALongSessionIdIsNeverTruncated pins the layout rule WidthsFor argues: a
// clipped session id is a session nobody can correlate or type back.
func TestALongSessionIdIsNeverTruncated(t *testing.T) {
	const fake = "00000000-aaaa-4bbb-8ccc-000000000001"
	e := ev(1, "app-alpha", fake, "PreToolUse", baseMS)
	w := WidthsFor([]eventsink.Event{e})
	if !strings.Contains(Row(e, w, false), fake) {
		t.Fatalf("the session id must appear whole in the row: %q", Row(e, w, false))
	}
}

// TestATornLineCostsThatLineOnly is CLAUDE.md's partial-read rule: a bad
// record degrades the read and is counted, it never fails it.
func TestATornLineCostsThatLineOnly(t *testing.T) {
	dir := t.TempDir()
	writeDay(t, dir, "2026-08-16", ev(1, "app-alpha", "sess-0001", "PreToolUse", baseMS))
	appendRaw(t, dir, "2026-08-16", "{\"id\":2,\"source_app\":\"app-al\n")
	appendRaw(t, dir, "2026-08-16", line(t, ev(3, "app-alpha", "sess-0001", "Stop", baseMS+2000)))

	got, err := Read(dir, Filter{})
	if err != nil {
		t.Fatalf("an unreadable line must not fail the read: %v", err)
	}
	if !sameIDs(got.Events, 3, 1) {
		t.Fatalf("want the two readable rows — got %v", ids(got.Events))
	}
	if got.Diag.Skipped != 1 {
		t.Fatalf("the unreadable line must be counted once, got %d", got.Diag.Skipped)
	}
	if !strings.Contains(Render(got, Options{Dir: dir}), "1 unreadable line skipped") {
		t.Fatal("a degraded read must say so on screen, not only in the struct")
	}
}

func TestFollowReturnsOnlyWhatArrivedSinceTheLastPoll(t *testing.T) {
	dir := t.TempDir()
	writeDay(t, dir, "2026-08-16",
		ev(1, "app-alpha", "sess-0001", "PreToolUse", baseMS),
		ev(2, "app-alpha", "sess-0001", "PostToolUse", baseMS+1000))

	tailer := NewTailer(dir, Filter{})
	first, err := tailer.Poll()
	if err != nil {
		t.Fatal(err)
	}
	// The first poll is the retained tail, oldest first, so follow output runs
	// in one direction from the top of the screen to the bottom.
	if !sameIDs(first.Events, 1, 2) {
		t.Fatalf("the first poll must return the retained rows oldest first, got %v", ids(first.Events))
	}

	quiet, err := tailer.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if len(quiet.Events) != 0 {
		t.Fatalf("a poll with nothing appended must return nothing, got %v", ids(quiet.Events))
	}

	appendRaw(t, dir, "2026-08-16", line(t, ev(3, "app-alpha", "sess-0001", "Stop", baseMS+2000)))
	next, err := tailer.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if !sameIDs(next.Events, 3) {
		t.Fatalf("want only the newly appended row, got %v", ids(next.Events))
	}
}

// TestFollowHoldsBackAHalfWrittenLine is why this reader goes through
// internal/jsonl. The sink appends while this poll reads, so a record without
// its terminating newline is not yet a record: counting it as unreadable would
// report a torn store every time a hook fired during a poll.
func TestFollowHoldsBackAHalfWrittenLine(t *testing.T) {
	dir := t.TempDir()
	writeDay(t, dir, "2026-08-16", ev(1, "app-alpha", "sess-0001", "PreToolUse", baseMS))

	tailer := NewTailer(dir, Filter{})
	if _, err := tailer.Poll(); err != nil {
		t.Fatal(err)
	}

	whole := line(t, ev(2, "app-alpha", "sess-0001", "Stop", baseMS+1000))
	half := whole[:len(whole)/2]
	appendRaw(t, dir, "2026-08-16", half)

	mid, err := tailer.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if len(mid.Events) != 0 {
		t.Fatalf("a half-written line must not be delivered, got %v", ids(mid.Events))
	}
	if mid.Diag.Skipped != 0 {
		t.Fatalf("a half-written line must not be counted unreadable, got %d", mid.Diag.Skipped)
	}

	appendRaw(t, dir, "2026-08-16", whole[len(whole)/2:])
	done, err := tailer.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if !sameIDs(done.Events, 2) {
		t.Fatalf("the completed line must arrive on the next poll, got %v", ids(done.Events))
	}
}

// TestFollowCrossesAUtcDayRollover: the sink names its file from the current
// UTC day, so a session running past midnight starts writing a file the tailer
// has never opened. A tailer that only watched the file it started on would go
// silent at midnight and look exactly like a fleet that stopped working.
func TestFollowCrossesAUtcDayRollover(t *testing.T) {
	dir := t.TempDir()
	writeDay(t, dir, "2026-08-16", ev(1, "app-alpha", "sess-0001", "PreToolUse", baseMS))

	tailer := NewTailer(dir, Filter{})
	if _, err := tailer.Poll(); err != nil {
		t.Fatal(err)
	}
	writeDay(t, dir, "2026-08-17", ev(2, "app-alpha", "sess-0001", "Stop", baseMS+86_400_000))

	next, err := tailer.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if !sameIDs(next.Events, 2) {
		t.Fatalf("a new day file must be picked up, got %v", ids(next.Events))
	}
}

// TestAMissingStoreIsNotAnEmptyStore keeps two answers apart that would
// otherwise share one blank screen: the sink has never run here, and the sink
// ran and stored nothing.
func TestAMissingStoreIsNotAnEmptyStore(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "never-created")
	gone, err := Read(missing, Filter{})
	if err != nil {
		t.Fatalf("a missing store is a state, not an error: %v", err)
	}
	if !gone.Diag.StoreMissing {
		t.Fatal("a missing store directory must be reported as missing")
	}

	empty, err := Read(t.TempDir(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Diag.StoreMissing {
		t.Fatal("an existing but empty store must not report as missing")
	}

	goneText := Render(gone, Options{Dir: missing})
	emptyText := Render(empty, Options{Dir: "somewhere"})
	if goneText == emptyText {
		t.Fatal("the two empty states must not render the same screen")
	}
	if !strings.Contains(goneText, "no store at") {
		t.Fatalf("a missing store must say where it looked: %q", goneText)
	}
}

// TestAnEmptyResultNamesWhatTheStoreHolds makes a no-match listing a next step
// instead of a dead end. These are the same three axes the sink serves at
// /events/filter-options, computed here from the files.
func TestAnEmptyResultNamesWhatTheStoreHolds(t *testing.T) {
	dir := t.TempDir()
	writeDay(t, dir, "2026-08-16",
		ev(1, "app-alpha", "sess-0001", "PreToolUse", baseMS),
		ev(2, "app-beta", "sess-0002", "Stop", baseMS+1000))

	got, err := Read(dir, Filter{Types: []string{"NoSuchHook"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Matched != 0 {
		t.Fatalf("want no match, got %d", got.Matched)
	}
	want := eventsink.FilterOptions{
		SourceApps:     []string{"app-alpha", "app-beta"},
		SessionIDs:     []string{"sess-0001", "sess-0002"},
		HookEventTypes: []string{"PreToolUse", "Stop"},
	}
	if !sameStrings(got.Options.SourceApps, want.SourceApps) ||
		!sameStrings(got.Options.SessionIDs, want.SessionIDs) ||
		!sameStrings(got.Options.HookEventTypes, want.HookEventTypes) {
		t.Fatalf("want the distinct axes %+v — got %+v", want, got.Options)
	}

	text := Render(got, Options{Dir: dir})
	for _, must := range []string{"--source", "app-alpha", "--session", "sess-0002", "--type", "PreToolUse"} {
		if !strings.Contains(text, must) {
			t.Fatalf("an empty result must name %q so the reader can retype it:\n%s", must, text)
		}
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	g := append([]string(nil), got...)
	sort.Strings(g)
	for i := range g {
		if g[i] != want[i] {
			return false
		}
	}
	return true
}

// TestTheViewerWritesNothing is the read/write boundary, asserted rather than
// promised. This mode reads the one content-bearing store in the product; it
// must leave every byte of it where it found it, and must not create the
// store directory either.
func TestTheViewerWritesNothing(t *testing.T) {
	dir := t.TempDir()
	writeDay(t, dir, "2026-08-16",
		ev(1, "app-alpha", "sess-0001", "PreToolUse", baseMS),
		ev(2, "app-beta", "sess-0002", "Stop", baseMS+1000))
	before := fingerprint(t, dir)

	listing, err := Read(dir, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	Render(listing, Options{Dir: dir, ShowPayload: true})

	tailer := NewTailer(dir, Filter{})
	if _, err := tailer.Poll(); err != nil {
		t.Fatal(err)
	}
	if _, err := tailer.Poll(); err != nil {
		t.Fatal(err)
	}
	if after := fingerprint(t, dir); after != before {
		t.Fatalf("the viewer changed the store:\nbefore %s\nafter  %s", before, after)
	}

	// A missing store stays missing: a reader that created the directory would
	// make "the sink never ran here" unanswerable on the next run.
	absentDir := filepath.Join(t.TempDir(), "never-created")
	if _, err := Read(absentDir, Filter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(absentDir); !os.IsNotExist(err) {
		t.Fatal("reading a missing store must not create it")
	}
}

// fingerprint hashes every file name and its bytes, so the comparison catches
// a rewrite that kept the size as well as an added or deleted file.
func fingerprint(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	for _, ent := range entries {
		h.Write([]byte(ent.Name()))
		data, err := os.ReadFile(filepath.Join(dir, ent.Name()))
		if err != nil {
			t.Fatal(err)
		}
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TestTheListingHeaderSaysWhatIsWithheld: the default view shows keys, and a
// reader who does not know a payload was stored cannot know to ask for it.
func TestTheListingHeaderSaysWhatIsWithheld(t *testing.T) {
	dir := t.TempDir()
	writeDay(t, dir, "2026-08-16", ev(1, "app-alpha", "sess-0001", "PreToolUse", baseMS))
	listing, err := Read(dir, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	quiet := Render(listing, Options{Dir: dir})
	if !strings.Contains(quiet, "--payload") {
		t.Fatalf("the default listing must name the flag that shows the bodies:\n%s", quiet)
	}
	if strings.Contains(quiet, payloadMarker) {
		t.Fatalf("the default listing printed a payload body:\n%s", quiet)
	}
	loud := Render(listing, Options{Dir: dir, ShowPayload: true})
	if !strings.Contains(loud, payloadMarker) {
		t.Fatalf("--payload must print the body:\n%s", loud)
	}
}

// TestRenderIsPureOverItsListing mirrors the rule internal/hud and
// internal/council render under: the same listing renders the same bytes, so
// nothing on this path reads a clock, the filesystem or the environment.
func TestRenderIsPureOverItsListing(t *testing.T) {
	dir := t.TempDir()
	writeDay(t, dir, "2026-08-16",
		ev(1, "app-alpha", "sess-0001", "PreToolUse", baseMS),
		ev(2, "app-beta", "sess-0002", "Stop", 0))
	listing, err := Read(dir, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Dir: dir, ShowPayload: true}
	first := Render(listing, opts)
	time.Sleep(2 * time.Millisecond)
	if second := Render(listing, opts); second != first {
		t.Fatalf("Render is not pure over its Listing:\n%s\nvs\n%s", first, second)
	}
}

// TestFollowBannerNamesTheLatencyItActuallyHas: this mode polls, so "live"
// means "within one interval". A banner that said only "following" would claim
// a push the reader is not getting.
func TestFollowBannerNamesTheLatencyItActuallyHas(t *testing.T) {
	got := FollowBanner("C:\\store", 2*time.Second, false)
	if !strings.Contains(got, "2s") || !strings.Contains(got, "C:\\store") {
		t.Fatalf("the banner must name the interval and the store: %q", got)
	}
	missing := FollowBanner("C:\\store", time.Second, true)
	if !strings.Contains(missing, "does not exist yet") {
		t.Fatalf("following a store that is not there yet must say so: %q", missing)
	}
}
