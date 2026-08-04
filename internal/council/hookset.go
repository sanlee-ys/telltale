package council

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// userSettingsFile is where Claude Code keeps the settings the gated seat stops
// reading.
//
// Hard-coded rather than resolved through CLAUDE_CONFIG_DIR, and that is a
// deliberate refusal rather than an oversight: this repo's standing rule is that
// a name is not evidence of a behaviour, and nothing here measured what a
// relocated config directory does. The cost of guessing wrong is bounded and
// visible — a machine whose config lives elsewhere finds no hooks, wires
// nothing, and the badge says the guard is absent. That is the honest failure,
// not a silent one.
const userSettingsFile = ".claude/settings.json"

// HookSet is the user's own hook configuration, copied into an ephemeral
// settings file the gated Claude seat can be pointed at.
//
// It exists to close a hole the gate opened. The gated seat passes
// --setting-sources "" because permission ALLOW RULES are consulted before the
// permission callback, so any call they cover never reaches the gate at all
// (ADR-008, seventh amendment). Dropping the sources drops the user's hooks
// along with the rules, and the two are not the same kind of thing: the rules
// say what may proceed unattended, which the gate is replacing on purpose; the
// hooks are a screen the user built, which nothing was replacing. Worse, the
// calls the gate never sees are exactly the ones a hook was covering — a shell
// command the CLI classifies read-only is approved without asking, so with the
// hooks gone it ran with no screening of any kind.
//
// Measured 2026-08-04, in a throwaway directory, on Claude Code 2.1.220:
//
//   - Gated posture, no --settings: `echo TELLTALE_HOOK_MARKER` raised NO
//     permission request and returned `"content":"TELLTALE_HOOK_MARKER"`. The
//     call was neither gated nor screened.
//   - Same posture, --settings pointed at a hooks-only file carrying a planted
//     PreToolUse hook: the same call came back
//     `"content":"SPIKE-HOOK-DENIED-TELLTALE_HOOK_MARKER","is_error":true` with
//     `"non_execution_kind":"permission-rule"`, and the hook's own breadcrumb
//     file recorded the invocation.
//
// So --settings composes with --setting-sources "": the sources stay dropped
// and the named file is still read.
type HookSet struct {
	// Path is the ephemeral hooks-only settings file, absolute. Empty means
	// nothing was carried over.
	//
	// Absolute because it must be: a relative --settings path is resolved
	// against the CHILD's working directory, which is the workspace council was
	// pointed at rather than telltale's own. Measured — a relative path failed
	// with "Error: Settings file not found".
	Path string

	// Absent says why nothing was carried over, in the words the badge uses. It
	// is a reason, never a reproduction of anything read from the settings file.
	Absent string

	// dir is the temporary directory holding Path, removed whole on cleanup.
	dir string
}

// Wired reports whether the seat will actually carry the user's hooks. The
// badge's claim is derived from this and from nothing else, so the claim cannot
// outlive the file.
func (h HookSet) Wired() bool { return h.Path != "" }

// Cleanup removes the ephemeral file. Safe to call on a zero HookSet and safe
// to call twice, because it is called from teardown AND from the function that
// started the room — quitting is not the only way out of a TUI.
func (h HookSet) Cleanup() {
	if h.dir == "" {
		return
	}
	os.RemoveAll(h.dir)
}

// LoadHookSet copies the hooks out of the user's settings into a file of its
// own.
//
// It never fails the room. A brief that cannot be read stops council, because
// the user asked for that brief by name and running unbriefed would be the
// failure it exists to remove. Nobody asks for this: it is repair work the room
// does on its own behalf, so the failure mode is to wire nothing and say so,
// never to refuse to open.
func LoadHookSet() HookSet {
	home, err := os.UserHomeDir()
	if err != nil {
		return HookSet{Absent: "your home directory could not be located, so no hooks were read"}
	}
	return loadHookSetFrom(filepath.Join(home, filepath.FromSlash(userSettingsFile)))
}

// noHooks is the one sentence every absent path resolves to, apart from the
// two that know something more specific. Deliberately uniform: a badge that
// distinguished "no settings file" from "a settings file with no hooks" would
// be reporting the user's filesystem, and the only fact the seat's claim rests
// on is whether a hook is in front of it.
const noHooks = "no hooks were found in your settings to carry over"

func loadHookSetFrom(src string) HookSet {
	raw, err := os.ReadFile(src)
	if err != nil {
		return HookSet{Absent: noHooks}
	}
	hooks := extractHooks(raw)
	if len(hooks) == 0 {
		return HookSet{Absent: noHooks}
	}

	dir, err := os.MkdirTemp("", "telltale-council-hooks-")
	if err != nil {
		return HookSet{Absent: "a temporary file for your hooks could not be created, so none were carried over"}
	}
	body, err := json.Marshal(map[string]json.RawMessage{"hooks": hooks})
	if err != nil {
		os.RemoveAll(dir)
		return HookSet{Absent: noHooks}
	}
	path := filepath.Join(dir, "hooks.json")
	// 0600 is what this asks for. On Windows Go maps the mode to the read-only
	// attribute and the ACL is inherited from the temp directory, so the real
	// containment there is that the directory is the user's own — which is
	// stated rather than left to the mode bits looking stricter than they are.
	if err := os.WriteFile(path, body, 0o600); err != nil {
		os.RemoveAll(dir)
		return HookSet{Absent: "your hooks could not be written to a temporary file, so none were carried over"}
	}
	return HookSet{Path: path, dir: dir}
}

// extractHooks returns the settings file's `hooks` value and nothing else.
//
// Built by NAMING the one key to keep rather than by deleting the keys to drop,
// and that is a safety property rather than a style choice. The same spike that
// established this mechanism also established what a leak would cost:
//
//	--settings file carrying {"permissions":{"allow":["Bash(mkdir:*)"]}} …
//	`mkdir zzz-leak-probe` → NO permission request, and the directory was on disk.
//
// So a permissions block reaching this file re-admits exactly the allow rules
// --setting-sources "" was passed to drop, and the room would go on rendering
// `gated` while calls walked past the gate. A denylist would have to be updated
// every time Claude Code adds a settings key; an allowlist of one cannot be.
//
// Malformed JSON yields nothing rather than an error, for the same reason the
// loader cannot fail the room: an unparseable settings file is a fact about the
// user's machine, and the honest response is a seat that says it carries no
// hooks.
func extractHooks(raw []byte) json.RawMessage {
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return nil
	}
	h, ok := top["hooks"]
	if !ok {
		return nil
	}
	// Decoded only to find out whether it holds anything. An empty object is
	// treated as absent: writing `{"hooks":{}}` and then claiming the seat is
	// screened would be a claim with nothing behind it.
	var events map[string]json.RawMessage
	if json.Unmarshal(h, &events) != nil || len(events) == 0 {
		return nil
	}
	// Re-emitted VERBATIM. A hook command is a shell string the user wrote; a
	// round trip through a typed struct would be council quietly deciding which
	// fields of someone else's config survive.
	return h
}
