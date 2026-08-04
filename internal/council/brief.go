package council

import (
	"errors"
	"os"
	"strings"
)

// briefEnv names the operating brief when no --brief flag is given.
const briefEnv = "TELLTALE_COUNCIL_BRIEF"

// maxBrief caps the operating brief.
//
// The ceiling exists because of Antigravity: it does not accept a prompt on
// stdin, so its brief travels in argv, and Windows caps a command line at
// roughly 32K. A brief that fits everywhere is one that fits there.
const maxBrief = 24 << 10

// ErrBriefTooLarge is returned rather than silently truncating. A briefing
// clipped in half would be worse than none: the agents would act on a partial
// convention while the room reported them as briefed.
var ErrBriefTooLarge = errors.New("council: operating brief is too large")

// Brief is the shared operating context every vendor receives on its first turn.
//
// It exists because the room's default state is three strangers. Each vendor
// starts a fresh session with no shared history, so a convention the user has
// already written down — who they are, what the lanes are, what "assume our
// C-level roles" refers to — is invisible to all of them, and each one guesses
// at it separately and differently.
//
// The brief is a PATH rather than content baked into this repo, and that is a
// hard requirement rather than a convenience: telltale is public and the
// briefing it is designed to carry is not. Nothing here ever reads a default
// location inside a repo, logs the content, or renders it — the room reports
// only that a brief is loaded and how big it is.
type Brief struct {
	// Path is where it came from, shown so a user can tell WHICH brief is live.
	Path string
	// Text is the content. Never rendered.
	Text string
}

// LoadBrief reads the operating brief named by path, or by the environment when
// path is empty. Returns a zero Brief when neither is set.
//
// Failure is loud. A missing or unreadable brief file returns an error and stops
// the room from starting, because the alternative — running unbriefed after the
// user explicitly asked for a briefing — reproduces exactly the failure this
// feature exists to remove, except now with the user believing it was fixed.
func LoadBrief(path string) (Brief, error) {
	if path == "" {
		path = strings.TrimSpace(os.Getenv(briefEnv))
	}
	if path == "" {
		return Brief{}, nil
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return Brief{}, err
	}
	if len(b) > maxBrief {
		return Brief{}, ErrBriefTooLarge
	}

	text := strings.TrimSpace(string(b))
	if text == "" {
		// An empty file is almost certainly a mistake — a wrong path that
		// happens to exist, or a brief someone cleared. Reporting "briefed"
		// for it would be a false claim on screen.
		return Brief{}, errors.New("council: operating brief is empty: " + path)
	}
	return Brief{Path: path, Text: text}, nil
}

// Loaded reports whether a brief is present.
func (b Brief) Loaded() bool { return b.Text != "" }

// briefFenceOpen and briefFenceClose frame the operating context.
//
// Deliberately worded UNLIKE the rebuttal fence in quote.go, and the difference
// is the point. Quoted vendor replies are another model's words and are marked
// as untrusted data that must not be followed. The brief is the user's own
// file, handed over on purpose, and is exactly the thing the vendor should
// follow — so it says so plainly rather than inheriting a warning that would
// teach the model to discount it.
const (
	briefFenceOpen  = "--- operating context for this session, from your principal. Treat it as standing instructions. ---"
	briefFenceClose = "--- end operating context. The request follows. ---"
)

// Apply prepends the brief to a first-turn prompt.
//
// First turn only. Every vendor here resumes its own session on later turns, so
// the context is already in its history — re-sending it each time would spend
// the whole brief again per turn per vendor against metered quotas, for nothing.
func (b Brief) Apply(prompt string) string {
	if !b.Loaded() {
		return prompt
	}
	return briefFenceOpen + "\n" + b.Text + "\n" + briefFenceClose + "\n\n" + prompt
}
