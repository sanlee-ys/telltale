package council

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// The scrub (design.md §9.56, and the 2026-09-04 review's fault F):
// `telltale council replay-scrub <in> <out>` reads a --record file and writes
// a second recording that keeps every STRUCTURAL fact and replaces every WORD.
//
// # Why it exists
//
// A recording is the only artifact that shows the room without five paid
// logins, and the only recording that exists is the operator's own. It carries
// the conversation verbatim (recording.go's "no redaction, on purpose"), so it
// cannot be committed, and a repository with no recording in it gives a visitor
// with no vendor installed nothing to replay at all. The scrub separates the
// two facts the file holds. The SHAPE of a room -- how many seats, which of
// them answered, in what order, how long each took, where the card went up and
// what it was answered with -- is a fact about telltale, and it is the fact a
// renderer regression would break. The WORDS are the operator's, and the room
// does not need them to draw.
//
// So CLAUDE.md's fixture rule is kept rather than bent: "fixtures are
// synthesized, never real" binds the CONTENT, and every word in a scrubbed
// file is synthesized here. The event shape is real, the file says so on its
// own room line, and every frame of a scrubbed replay says so too.
//
// # What is kept, and what is replaced
//
// Kept: the record kinds, the millisecond offsets, the turn numbers, the
// routes, which vendor each record belongs to, the persistent and quoted
// flags, the event kinds, ends_turn, the act outcomes, the exit codes, the
// failure classes, the gate decisions, the cost figures, the seats list with
// its labels and postures, and the room's write and gate flags.
//
// Replaced: every prompt, every text chunk, every act name and detail that can
// carry a path or a file name, every session id, every request and tool-use
// id, the workspace, the room's wall stamp, and every err and note string.
//
// # Length classes, and why they are exact
//
// Every replacement is the same RUNE LENGTH as what it replaces, and every
// newline stays where it was. That is not decoration: the column body wraps on
// width and scrolls on line count, so a replacement that shortened a paragraph
// would draw a different room, and the golden this file exists to feed would
// pin a frame the real room never had. A path keeps its separators, its depth
// and its extension for the same reason -- the trace line truncates on width,
// and a fake path of the same depth truncates in the same place.
//
// # Determinism
//
// Scrubbing one file twice gives one output, byte for byte, so the fixture can
// be regenerated and reviewed by diff. The generator is a splitmix64 written
// here rather than math/rand, because a stdlib generator's sequence is a
// stdlib decision and this file's output is committed bytes. Each structured
// replacement is seeded from the record's INDEX and field name, so a change to
// one record does not move every record after it. Each seat's prose comes from
// one stream per vendor instead, advanced in file order: a vendor streams a
// reply in one-token and two-token chunks, and a per-chunk seed would restart
// the words at every chunk boundary and read as one long unbroken word.
//
// # The boundary this file is written under
//
// The same one --record is written under, and no wider. The scrub reads a path
// the operator typed and writes a path the operator typed; it refuses to write
// inside ~/.telltale, where the gauges keep their numbers-and-keys stores; it
// refuses to overwrite an existing file, because the output is a reviewed
// artifact and a silent truncation would destroy a review; and it starts no
// vendor, opens no room, and reads no state of its own.

// scrubbedStart is the wall stamp a scrubbed recording carries.
//
// The offsets carry every duration the room draws, so the wall date buys the
// replay nothing and tells a reader which evening the operator was at the
// desk. One fixed synthetic stamp, and the replay counts from it.
const scrubbedStart = "2026-01-01T09:00:00Z"

// scrubSeed separates this file's streams from any other use of the same
// generator. It has no meaning beyond that.
const scrubSeed uint64 = 0x5343525542000001

// rng is splitmix64: one 64-bit state, one multiply-xor-shift step per draw.
// Written here rather than taken from math/rand because the output of this
// file is committed bytes, and a generator whose sequence the standard library
// may re-tune would rewrite the fixture on a Go upgrade.
type rng struct{ s uint64 }

func newRNG(seed uint64) *rng { return &rng{s: seed} }

func (r *rng) next() uint64 {
	r.s += 0x9e3779b97f4a7c15
	z := r.s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// intn returns a number in [0,n). Zero for a non-positive n, so a caller with
// an empty table gets an index it can use rather than a panic.
func (r *rng) intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.next() % uint64(n))
}

// seedFor mixes a field name and a record index into a seed. FNV-1a, because
// the mixing quality only has to separate one field of one record from the
// next, and the constant is readable.
func seedFor(field string, index int) uint64 {
	h := uint64(14695981039346656037)
	mix := func(b byte) {
		h ^= uint64(b)
		h *= 1099511628211
	}
	for i := 0; i < len(field); i++ {
		mix(field[i])
	}
	for shift := 0; shift < 64; shift += 8 {
		mix(byte(uint64(index) >> shift))
	}
	return h ^ scrubSeed
}

// scrubVocabulary is the whole word supply for synthesized prose. Ordinary
// words with no subject of their own: a reader must be able to see at a glance
// that the body is filler, and a reader must never be able to read a claim out
// of it.
var scrubVocabulary = []string{
	"the", "a", "this", "that", "and", "but", "so", "then", "also", "first",
	"next", "last", "one", "two", "each", "every", "some", "other", "same",
	"new", "small", "large", "short", "long", "clear", "plain", "quiet",
	"ready", "open", "above", "below", "after", "before", "here", "there",
	"line", "note", "case", "step", "rule", "name", "list", "group", "order",
	"point", "part", "piece", "shape", "frame", "field", "value", "count",
	"limit", "range", "level", "state", "check", "test", "start", "end",
	"begin", "finish", "report", "answer", "reason", "result", "change",
	"review", "detail", "method", "option", "output", "input", "number",
	"column", "table", "screen", "window", "header", "footer", "body", "word",
	"term", "index", "label", "mark", "block", "chain", "stack", "queue",
	"band", "edge", "node", "link", "tree", "leaf", "root", "base", "side",
	"light", "warm", "cool", "fast", "slow", "near", "wide", "deep", "flat",
	"round", "square", "again", "still", "enough", "almost", "rather",
	"between", "within", "against", "beside", "across", "around", "beyond",
	"reads", "writes", "draws", "holds", "keeps", "takes", "gives", "shows",
	"names", "counts", "moves", "waits", "opens", "closes", "makes",
}

// proseStream is one seat's synthesized reply, drawn on demand.
//
// A vendor streams a reply as many small chunks -- one and two runes at a time
// on a token-granular seat -- and each chunk is one record. The stream is what
// makes the concatenation of those chunks read as words: it hands out the next
// N runes of an endless synthesized sentence, so a chunk boundary falls inside
// a word exactly as the vendor's own did.
type proseStream struct {
	r   *rng
	buf []rune
}

func newProseStream(seed uint64) *proseStream { return &proseStream{r: newRNG(seed)} }

// take returns exactly n runes.
func (p *proseStream) take(n int) []rune {
	for len(p.buf) < n {
		w := scrubVocabulary[p.r.intn(len(scrubVocabulary))]
		p.buf = append(p.buf, []rune(w+" ")...)
	}
	out := make([]rune, n)
	copy(out, p.buf[:n])
	p.buf = p.buf[n:]
	return out
}

// breakLine drops the words still queued, so the next line starts on a word
// boundary rather than in the middle of one.
func (p *proseStream) breakLine() { p.buf = p.buf[:0] }

// prose replaces s with synthesized words of the same rune length, keeping
// every newline where it was.
//
// Newlines are the one part of the original text this keeps, and they are kept
// because they are structure rather than content: the column wraps on width
// and scrolls on line count, so a paragraph that lost its breaks would draw a
// room the recording never held.
func (p *proseStream) prose(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	pending := 0
	flush := func() {
		if pending > 0 {
			b.WriteString(string(p.take(pending)))
			pending = 0
		}
	}
	for _, r := range s {
		if r == '\n' {
			flush()
			p.breakLine()
			b.WriteRune('\n')
			continue
		}
		pending++
	}
	flush()
	return b.String()
}

// scrubKeepWords are the runs a shape replacement leaves alone. A drive letter
// and `Users` are what make a fake Windows path read as a Windows path, and
// neither of them names anybody: the segment after them is the one that does,
// and it is replaced.
var scrubKeepWords = map[string]bool{
	"C": true, "D": true, "E": true, "Users": true, "home": true,
}

// scrubKeepExtensions are the file types a shape replacement keeps after a
// dot. The extension is the tool line's shape, not its identity: a reader
// looking at a trace wants to see that a seat read a markdown file, and the
// name of that file is what is taken away.
var scrubKeepExtensions = map[string]bool{
	"md": true, "go": true, "py": true, "txt": true, "json": true,
	"jsonl": true, "js": true, "ts": true, "yaml": true, "yml": true,
	"toml": true, "html": true, "css": true, "sh": true, "ps1": true,
	"exe": true, "log": true, "csv": true, "png": true, "svg": true,
}

const (
	scrubConsonants = "bcdfgklmnprstvz"
	scrubVowels     = "aeiou"
)

// shape replaces every run of letters and digits and keeps every other rune
// where it stood, so the result has the original's punctuation, its
// separators, its depth and its exact length.
//
// A Windows path comes out a Windows path of the same depth; a quoted shell
// command comes out a quoted shell command; a flag stays a flag. The case of
// each rune is kept as well, because a path segment that lost its capital
// would not look like the segment it replaced.
func shape(r *rng, s string) string {
	runes := []rune(s)
	out := make([]rune, 0, len(runes))
	for i := 0; i < len(runes); {
		if !isAlnum(runes[i]) {
			out = append(out, runes[i])
			i++
			continue
		}
		j := i
		for j < len(runes) && isAlnum(runes[j]) {
			j++
		}
		run := runes[i:j]
		word := string(run)
		afterDot := i > 0 && runes[i-1] == '.'
		switch {
		case scrubKeepWords[word]:
			out = append(out, run...)
		case afterDot && scrubKeepExtensions[strings.ToLower(word)]:
			out = append(out, run...)
		default:
			out = append(out, fakeRun(r, run)...)
		}
		i = j
	}
	return string(out)
}

// fakeRun is one run of letters and digits, replaced rune for rune. A digit
// stays a digit, a letter stays a letter of the same case, and the letters
// alternate consonant and vowel so the result can be read aloud.
func fakeRun(r *rng, run []rune) []rune {
	out := make([]rune, len(run))
	for i, c := range run {
		switch {
		case unicode.IsDigit(c):
			out[i] = rune('0' + r.intn(10))
		case i%2 == 0:
			out[i] = rune(scrubConsonants[r.intn(len(scrubConsonants))])
		default:
			out[i] = rune(scrubVowels[r.intn(len(scrubVowels))])
		}
		if unicode.IsUpper(c) {
			out[i] = unicode.ToUpper(out[i])
		}
	}
	return out
}

func isAlnum(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// scrubToolVerbs is the set of act heads a scrub keeps whole. Each is a vendor
// TOOL NAME, which is a fact about the run rather than about the operator, and
// the trace draws it as the act's kind.
//
// A head is kept only when it is unmistakably one of these or carries an
// underscore, which is how every snake_case vendor spells a tool. Anything
// else in that slot is replaced, because a bare word there is a file name
// often enough: `hello`, `rebuttal` and `notes` all appeared as whole act
// lines in the room recorded on 2026-09-03.
var scrubToolVerbs = map[string]bool{
	"Read": true, "Write": true, "Edit": true, "MultiEdit": true,
	"NotebookEdit": true, "Bash": true, "Glob": true, "Grep": true,
	"grep": true, "List": true, "Execute": true, "Search": true,
	"Task": true, "WebFetch": true, "WebSearch": true, "TodoWrite": true,
	"Fetch": true, "Delete": true, "Move": true, "Copy": true, "Run": true,
}

// actHead splits an act line into the tool name the trace draws and the
// argument that can name a path.
//
// The separators are the three the measured vendors use: nothing at all (a
// bare tool name, `read_file`), a colon and a space (`write_to_file: <path>`),
// and a space and a backtick (`Read `+"`"+`<path>`). A line that matches none
// of them is replaced whole, which is the safe answer: the head this function
// could not name is not provably a tool name.
func actHead(s string) (head string, rest string, ok bool) {
	i := 0
	runes := []rune(s)
	if len(runes) == 0 || !unicode.IsLetter(runes[0]) {
		return "", s, false
	}
	for i < len(runes) && (isAlnum(runes[i]) || runes[i] == '_' || runes[i] == '-') {
		i++
	}
	cand := string(runes[:i])
	if !scrubToolVerbs[cand] && !strings.Contains(cand, "_") {
		return "", s, false
	}
	// A doubled underscore is how every measured vendor spells an MCP tool:
	// `<server>__<tool>`. The server half names something the OPERATOR wired
	// up rather than something the vendor ships, so the whole line is
	// replaced. shape() keeps the underscores and the length, so the trace
	// still draws a long namespaced tool name of the same width.
	if strings.Contains(cand, "__") {
		return "", s, false
	}
	tail := string(runes[i:])
	switch {
	case tail == "":
		return cand, "", true
	case strings.HasPrefix(tail, ": "):
		return cand + ": ", tail[len(": "):], true
	case strings.HasPrefix(tail, " `"):
		return cand + " `", tail[len(" `"):], true
	}
	return "", s, false
}

// scrubber holds what has to stay consistent across the whole file: one
// synthesized identifier per real identifier, and one prose stream per seat.
type scrubber struct {
	ids   map[string]string
	taken map[string]bool
	prose map[string]*proseStream
}

func newScrubber() *scrubber {
	return &scrubber{ids: map[string]string{}, taken: map[string]bool{}, prose: map[string]*proseStream{}}
}

// stream is one seat's prose, made on first use and kept for the file.
func (s *scrubber) stream(vendor string) *proseStream {
	p := s.prose[vendor]
	if p == nil {
		p = newProseStream(seedFor("prose:"+vendor, 0))
		s.prose[vendor] = p
	}
	return p
}

// id re-keys one identifier, and gives the same answer every time it is asked
// about the same one.
//
// Consistency is the whole point: a gate card and the operator's decision name
// one request id, and a tool call and its result name one tool-use id. A scrub
// that re-keyed them apart would break the correlation the room draws from,
// and replay-check would report a card nobody answered.
func (s *scrubber) id(orig string) string {
	if orig == "" {
		return ""
	}
	if got, ok := s.ids[orig]; ok {
		return got
	}
	r := newRNG(seedFor("id:"+orig, len(s.ids)))
	var out string
	for attempt := 0; ; attempt++ {
		if isUUID(orig) {
			out = fakeUUID(r, orig)
		} else {
			out = fakeIdentifier(r, orig)
		}
		if !s.taken[out] {
			break
		}
		if attempt > 8 {
			// Eight collisions on a 64-bit stream is not a thing that
			// happens; suffixing rather than looping forever is the honest
			// way to say so without a panic in a tool that writes a file.
			out += "x"
			break
		}
	}
	s.ids[orig] = out
	s.taken[out] = true
	return out
}

// isUUID reports the 8-4-4-4-12 hexadecimal shape.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !isHex(byte(c)) {
				return false
			}
		}
	}
	return true
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// fakeUUID re-keys a UUID and keeps the two nibbles that are structure rather
// than identity: the version at index 14 and the variant at index 19. A seat
// whose ids are version 7 goes on looking like a seat whose ids are version 7,
// which is a fact about the vendor and not about the operator.
func fakeUUID(r *rng, orig string) string {
	const hex = "0123456789abcdef"
	out := []byte(orig)
	for i := range out {
		switch i {
		case 8, 13, 18, 23, 14, 19:
			continue
		default:
			out[i] = hex[r.intn(16)]
		}
	}
	return string(out)
}

// fakeIdentifier re-keys anything else that names a thing: `toolu_<base62>`,
// `call-<uuid>-<n>`, `step-<n>`. The first run of letters is kept when it is a
// short word, because it is the vendor's prefix rather than the id; every
// other run is replaced rune for rune, so the result has the original's
// length, its punctuation and its character classes.
func fakeIdentifier(r *rng, orig string) string {
	runes := []rune(orig)
	out := make([]rune, 0, len(runes))
	first := true
	for i := 0; i < len(runes); {
		if !isAlnum(runes[i]) {
			out = append(out, runes[i])
			i++
			continue
		}
		j := i
		for j < len(runes) && isAlnum(runes[j]) {
			j++
		}
		run := runes[i:j]
		if first && len(run) <= 8 && allLetters(run) {
			out = append(out, run...)
		} else if allHex(run) && len(run) >= 4 {
			const hex = "0123456789abcdef"
			for range run {
				out = append(out, rune(hex[r.intn(16)]))
			}
		} else {
			out = append(out, fakeRun(r, run)...)
		}
		first = false
		i = j
	}
	return string(out)
}

func allLetters(run []rune) bool {
	for _, c := range run {
		if !unicode.IsLetter(c) {
			return false
		}
	}
	return true
}

func allHex(run []rune) bool {
	for _, c := range run {
		if c > 127 || !isHex(byte(c)) {
			return false
		}
	}
	return true
}

// act replaces one act line: the tool name stays, the argument goes.
func (s *scrubber) act(text string, index, n int) string {
	if strings.TrimSpace(text) == "" {
		// A result resolving an earlier call by id. It names nothing, and
		// replay-check counts these rather than listing them.
		return text
	}
	r := newRNG(seedFor(fmt.Sprintf("act:%d", n), index))
	head, rest, ok := actHead(text)
	if !ok {
		return shape(r, text)
	}
	return head + shape(r, rest)
}

// text replaces one field of ordinary prose: a brief, a note, an error string,
// a tool result's detail, a gate card's before and after.
func (s *scrubber) text(field string, index int, v string) string {
	return newProseStream(seedFor(field, index)).prose(v)
}

// ScrubRecording reads the recording at in and writes a scrubbed one at out.
//
// It refuses the two paths --record refuses, for the two reasons --record
// refuses them: ~/.telltale holds the gauges' numbers and keys, and an
// existing file is an artifact somebody may already have reviewed.
func ScrubRecording(in, out string, w io.Writer) error {
	rec, err := readRecording(in)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(out)
	if err != nil {
		return fmt.Errorf("replay-scrub %s: %w", out, err)
	}
	if err := refuseOwnStateDir("replay-scrub", out, abs); err != nil {
		return err
	}
	inAbs, err := filepath.Abs(in)
	if err == nil && strings.EqualFold(inAbs, abs) {
		return errors.New("replay-scrub " + out + ": that is the file being read. A scrub writes a second file and never edits the capture")
	}
	lines := scrubRecording(rec)
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("replay-scrub " + out + ": the file already exists. Name a new file rather than overwrite a fixture somebody has reviewed")
		}
		return fmt.Errorf("replay-scrub %s: %w", out, err)
	}
	for _, line := range lines {
		raw, merr := json.Marshal(line)
		if merr != nil {
			f.Close()
			return fmt.Errorf("replay-scrub %s: %w", out, merr)
		}
		if _, werr := f.Write(append(raw, '\n')); werr != nil {
			f.Close()
			return fmt.Errorf("replay-scrub %s: %w", out, werr)
		}
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("replay-scrub %s: %w", out, err)
	}
	fmt.Fprintf(w, "scrubbed %d %s into %s\n", len(rec.lines), plural(len(rec.lines), "record"), out)
	fmt.Fprintln(w, "Every structural fact is kept and every word is synthesized. The room line says scrubbed, and so does every frame of the replay.")
	fmt.Fprintln(w, "Read the whole file before you commit it, and run `telltale council replay-check` over it.")
	return nil
}

// scrubRecording is the transform itself: one recording in, the lines of the
// scrubbed file out, room line first. Pure over its input, so a test reads the
// result without touching a disk.
func scrubRecording(rec *recording) []recordLine {
	s := newScrubber()
	out := make([]recordLine, 0, len(rec.lines)+1)

	head := rec.room
	head.Scrubbed = true
	head.Started = scrubbedStart
	head.Workspace = shape(newRNG(seedFor("workspace", 0)), head.Workspace)
	seats := make([]recordSeat, len(head.Seats))
	copy(seats, head.Seats)
	for i := range seats {
		// The label and the sandbox detail are telltale's own words about a
		// vendor, printed the same on every machine, so they stay: they are
		// the badge row the replay has to draw. The note is the room talking
		// about this run, and it goes.
		seats[i].Note = s.text("seat.note", i, seats[i].Note)
	}
	head.Seats = seats
	out = append(out, head)

	for i, line := range rec.lines {
		switch line.Kind {
		case "dispatch":
			sent := make([]recordSent, len(line.Sent))
			copy(sent, line.Sent)
			for j := range sent {
				// Seeded from the DISPATCH rather than from the seat, so an
				// @all brief that reached four seats as one string comes out
				// as one string, and a brief that differs only by its mention
				// comes out sharing that prefix.
				sent[j].Prompt = s.text("prompt", i, sent[j].Prompt)
			}
			line.Sent = sent
		case "event":
			line.SessionID = s.id(line.SessionID)
			line.Text = s.stream(line.Vendor).prose(line.Text)
			line.Err = s.text("err", i, line.Err)
			line.Note = s.text("note", i, line.Note)
			if len(line.Acts) > 0 {
				acts := make([]recordAct, len(line.Acts))
				copy(acts, line.Acts)
				for j := range acts {
					acts[j].ID = s.id(acts[j].ID)
					acts[j].Text = s.act(acts[j].Text, i, j)
					acts[j].Detail = s.text(fmt.Sprintf("act.detail:%d", j), i, acts[j].Detail)
				}
				line.Acts = acts
			}
			if line.Gate != nil {
				g := *line.Gate
				g.RequestID = s.id(g.RequestID)
				g.ToolUseID = s.id(g.ToolUseID)
				g.Text = s.act(g.Text, i, 0)
				g.Old = s.text("gate.old", i, g.Old)
				g.New = s.text("gate.new", i, g.New)
				line.Gate = &g
			}
		case "gate":
			line.RequestID = s.id(line.RequestID)
		}
		out = append(out, line)
	}
	return out
}

// refuseOwnStateDir is the ~/.telltale refusal, shared by --record and
// replay-scrub. Both write CONTENT, and that directory holds numbers and keys.
func refuseOwnStateDir(what, typed, abs string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	own := filepath.Join(home, ".telltale")
	rel, rerr := filepath.Rel(own, abs)
	if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	return errors.New(what + " " + typed + ": that is telltale's own state directory, which holds numbers and keys only — a recording carries the conversation, so name a path outside ~/.telltale")
}

// recordPlacementWarning is the --record placement guard (the 2026-09-04
// review's fault D).
//
// A recording written inside the workspace, or in the directory just above it,
// is a file the seats can read, and on 2026-09-03 one of them did: a seat read
// the room's own recording mid-room, and the trace showed the read. The room
// gives each writing seat a worktree BESIDE the workspace (§9.55), so the
// parent directory is inside every seat's reach as surely as the workspace
// itself is.
//
// A warning and not a refusal. The path is the operator's, the file is the
// operator's, and there are rooms where the recording belongs there; what the
// operator cannot do is know the consequence without being told it once.
// Returns the empty string when the placement is clear.
func recordPlacementWarning(recordPath, workspace string) string {
	if recordPath == "" || workspace == "" {
		return ""
	}
	rec, err := filepath.Abs(recordPath)
	if err != nil {
		return ""
	}
	ws, err := filepath.Abs(workspace)
	if err != nil {
		return ""
	}
	dir := filepath.Dir(rec)
	if within(ws, dir) {
		return "telltale council --record " + recordPath +
			": this file is inside the workspace, so a seat can read the room's own recording. Name a path outside " + workspace + "."
	}
	if strings.EqualFold(filepath.Clean(dir), filepath.Clean(filepath.Dir(ws))) {
		return "telltale council --record " + recordPath +
			": this file is one directory above the workspace, beside every seat's worktree, so a seat can read it. Name a path further away."
	}
	return ""
}

// within reports whether dir is root or sits under it. Case-insensitive,
// because Windows is the primary target (ADR-002) and two spellings of one
// directory are one directory there.
func within(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
