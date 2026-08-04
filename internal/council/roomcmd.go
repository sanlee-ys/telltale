package council

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// The room command: a draft addressed to the ROOM rather than to the vendors.
//
// There is exactly one, /cd, because the workspace is the one piece of room
// state the P0 demands be movable from inside — "go into whatever repo I
// want" is a sentence typed at the room, not a flag typed at a shell. Only a
// draft that IS the command is intercepted; anything else, including text
// that merely starts with a slash, dispatches to the vendors as typed, so no
// vocabulary is quietly stolen from the conversation.

// parseRoomCommand recognises "/cd" and "/cd <dir>". The second return is the
// argument, trimmed; ok is false for every draft that should dispatch.
func parseRoomCommand(draft string) (arg string, ok bool) {
	s := strings.TrimSpace(draft)
	if s == "/cd" {
		return "", true
	}
	if rest, found := strings.CutPrefix(s, "/cd "); found {
		return strings.TrimSpace(rest), true
	}
	return "", false
}

// roomCommand handles a room-addressed draft. Returns false when the draft is
// ordinary and should dispatch.
func (m *Model) roomCommand() bool {
	arg, ok := parseRoomCommand(m.st.Draft)
	if !ok {
		return false
	}
	if m.turn != nil {
		// The turn in flight was dispatched against the old directory, and the
		// spawn-per-turn seats read the workspace at dispatch. Moving it under
		// them would make the room's header disagree with where three agents
		// are actually acting.
		m.st.Notice = "a turn is in flight — /cd moves the room between turns"
		return true
	}
	if arg == "" {
		// "/cd" alone answers the question it half-asks.
		m.st.Notice = "the room is in " + abbreviate(m.st.Workspace, m.st.Home) +
			" — /cd <dir> moves it"
		m.setDraft("")
		return true
	}

	dir, err := m.resolveCD(arg)
	if err != nil {
		m.st.Notice = err.Error()
		// The draft is kept: a mistyped path is expensive to retype and nothing
		// has been dispatched — the same reasoning esc uses.
		return true
	}
	if sameDir(dir, m.st.Workspace) {
		m.st.Notice = "the room is already in " + abbreviate(dir, m.st.Home)
		m.setDraft("")
		return true
	}

	m.st.Workspace = dir
	m.setDraft("")
	// Nothing is killed here. The spawn-per-turn seats read the workspace at
	// their next dispatch, and a persistent seat's process is respawned lazily
	// by seatProcess when the mismatch is seen — so a /cd that is /cd'd back
	// before anyone dispatches costs nothing at all.
	m.st.Notice = "the room now works in " + abbreviate(dir, m.st.Home) +
		" — seats move on their next turn"
	return true
}

// cdError is a resolution failure in the words the notice shows.
type cdError string

func (e cdError) Error() string { return string(e) }

// resolveCD turns a /cd argument into the absolute directory it names.
//
// Three candidates, in order: the path as given (absolute, or relative to the
// room's CURRENT workspace, the way a shell would read it), then a SIBLING of
// the current workspace. The sibling rule is what makes "/cd kb-agent" work
// from ~/code/telltale without telltale hardcoding anyone's directory layout:
// repos that live side by side reach each other by name.
func (m *Model) resolveCD(arg string) (string, error) {
	p := arg
	// "~" is composer text, not shell input, so nothing expands it unless we
	// do. Only the leading-tilde form; a "~user" form is not worth guessing at.
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if m.st.Home == "" {
			// Expanding against an empty home would quietly resolve "~" to the
			// current workspace and report a move that never happened.
			return "", cdError("the home directory is unknown — use an absolute path")
		}
		if p == "~" {
			p = m.st.Home
		} else {
			p = filepath.Join(m.st.Home, p[2:])
		}
	}

	var candidates []string
	if filepath.IsAbs(p) {
		candidates = []string{p}
	} else {
		candidates = []string{
			filepath.Join(m.st.Workspace, p),
			filepath.Join(filepath.Dir(m.st.Workspace), p),
		}
	}
	for _, c := range candidates {
		fi, err := os.Stat(c)
		if err != nil || !fi.IsDir() {
			continue
		}
		if abs, err := filepath.Abs(c); err == nil {
			return filepath.Clean(abs), nil
		}
	}
	return "", cdError("no directory named " + arg + " here or beside " +
		abbreviate(m.st.Workspace, m.st.Home))
}

// sameDir reports whether two cleaned absolute paths name one directory.
// Case folds on Windows only — the same rule the old room key used, and for
// the same reason: `C:\Users\...` and `c:\users\...` are one directory there,
// and two directories anywhere else.
func sameDir(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
