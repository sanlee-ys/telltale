package council

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/sanlee-ys/telltale/internal/gatehook"
)

// GateHook is council's own PreToolUse hook, written to an ephemeral settings
// file the gated Claude seat is pointed at.
//
// It REPLACED a thing with the same shape and the opposite content. Until
// 2026-08-12 this file copied the USER's hooks into the ephemeral file, because
// the gated seat passed --setting-sources "" and dropped their whole settings;
// the copy was repair for that. The seat no longer drops them, so there is
// nothing to repair — and copying them now would run every one of them twice.
// What the file carries instead is the hook that makes the gate hold.
//
// Why a hook rather than the flag, measured 2026-08-12 on Claude Code 2.1.228,
// Windows 11, claude-haiku-4-5, two trials per arm in throwaway directories
// (design.md §9.8's second dated block):
//
//   - A matcherless PreToolUse hook returning permissionDecision "ask" gated
//     ALL THREE tool shapes: `mkdir` (which the operator's own allow rules
//     cover), `install -d` (which no rule covers) and `Write` (not a shell
//     command at all). Every request carried decision_reason_type "hook" and
//     the hook's own sentence in decision_reason. Nothing landed on disk.
//   - The control is what makes that a finding rather than a coincidence: the
//     SAME file, the same hook process running (its breadcrumbs prove it), the
//     decision removed. `mkdir` raised no request and the directory landed.
//     The decision causes the gate, not the file.
//   - The matcher forms were measured against each other in one turn:
//     matcherless, "*" and "" each saw both a Bash and a Write call; "Bash"
//     saw only the Bash call. A matcherless hook seeing every tool was
//     documentation until that turn.
//
// And why a hook rather than a LIST of ask rules, which was the other candidate:
// `touch probe-marker` creates a file and ran ungated under the operator's own
// rules. Their rules cover more shapes than anyone would enumerate, so an ask
// list leaks exactly the way an allow list leaks. A matcherless hook has no
// list to leak.
type GateHook struct {
	// Path is the ephemeral settings file, absolute. Empty means the hook could
	// not be wired, and the gated seat falls back to --setting-sources "" —
	// which is honest, and is what shipped until 2026-08-12.
	//
	// Absolute because it must be: a relative --settings path resolves against
	// the CHILD's working directory, which is the workspace council was pointed
	// at rather than telltale's own. Measured — a relative path failed with
	// "Error: Settings file not found".
	Path string

	// Absent says why the hook could not be wired, in the words the badge uses.
	Absent string

	// dir is the temporary directory holding Path, removed whole on cleanup.
	dir string
}

// Wired reports whether council's own hook is in front of the seat. The badge's
// claim is derived from this and from nothing else, so the claim cannot outlive
// the file.
func (h GateHook) Wired() bool { return h.Path != "" }

// Cleanup removes the ephemeral file. Safe to call on a zero GateHook and safe
// to call twice, because it is called from teardown AND from the function that
// started the room — quitting is not the only way out of a TUI.
func (h GateHook) Cleanup() {
	if h.dir == "" {
		return
	}
	os.RemoveAll(h.dir)
}

// NewGateHook writes the settings file that puts council's hook in front of the
// seat.
//
// It never fails the room. A brief that cannot be read stops council, because
// the user asked for that brief by name and running unbriefed would be the
// failure it exists to remove. Nobody asks for this: it is the room wiring its
// own gate, so the failure mode is to wire nothing and say so — and the seat
// then launches in the older posture, which drops the operator's settings and
// gates on the flag instead. Weaker in what it keeps, never weaker at the gate.
func NewGateHook() GateHook {
	exe, err := os.Executable()
	if err != nil {
		return GateHook{Absent: "telltale could not locate its own binary, so the gate hook was not wired"}
	}
	dir, err := os.MkdirTemp("", "telltale-council-gate-")
	if err != nil {
		return GateHook{Absent: "a temporary file for the gate hook could not be created, so it was not wired"}
	}
	path := filepath.Join(dir, "gate.json")
	// 0600 is what this asks for. On Windows Go maps the mode to the read-only
	// attribute and the ACL is inherited from the temp directory, so the real
	// containment there is that the directory is the user's own — which is
	// stated rather than left to the mode bits looking stricter than they are.
	if err := os.WriteFile(path, gateHookSettings(exe), 0o600); err != nil {
		os.RemoveAll(dir)
		return GateHook{Absent: "the gate hook could not be written to a temporary file, so it was not wired"}
	}
	return GateHook{Path: path, dir: dir}
}

// hookCommand is the shell string Claude Code runs once per tool call.
//
// THE SLASHES ARE LOAD-BEARING ON WINDOWS, and this cost the probe its first
// three arms. Claude Code hands the command to `/usr/bin/bash` — Git Bash, on
// the platform this product primarily targets — and bash eats every backslash
// in an unquoted Windows path. The first attempt shipped
// `C:\Users\…\telltale.exe` and the hook came back
//
//	/usr/bin/bash: line 1: C:UserssanleAppDataLocalTempclaudeC--…telltale.exe: command not found
//
// with exit_code 127. That is the worst failure this feature has, and it is
// silent: a hook that does not run makes no decision, so every call ran ungated
// while the badge went on claiming a gate. Two things prevent it here, and both
// are needed — filepath.ToSlash for the separators, and quoting for the spaces
// in a path like C:/Program Files/telltale/telltale.exe.
//
// The double quote is safe to add unconditionally: bash strips it, and a path
// containing a literal double quote is not a path Windows can produce.
func hookCommand(exe string) string {
	return `"` + filepath.ToSlash(exe) + `" ` + gatehook.Mode + " " + gatehook.Verb
}

// gateHookSettings builds the file, and it is built by NAMING the one key it
// needs rather than by starting from anything the operator wrote.
//
// That is a safety property rather than a style choice, and it is inherited
// intact from the file this replaced. The 2026-08-04 spike measured what a leak
// would cost:
//
//	--settings file carrying {"permissions":{"allow":["Bash(mkdir:*)"]}} …
//	`mkdir zzz-leak-probe` → NO permission request, and the directory was on disk.
//
// A `permissions` block in THIS file would re-admit an allow list at step five
// with nothing at step three to beat it, and the room would go on rendering
// `gated` while calls walked past the gate. Nothing but `hooks` is ever written
// here, and the test asserts the top-level key count rather than the absence of
// any particular villain.
//
// The entry carries no `matcher` field at all. "" and "*" were measured to
// behave identically to its absence, so all three were available; the absent
// field is chosen because it is the one form that cannot be read as a pattern
// somebody should widen later.
func gateHookSettings(exe string) []byte {
	body, err := json.Marshal(map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": hookCommand(exe),
						},
					},
				},
			},
		},
	})
	if err != nil {
		// Unreachable, and not papered over: an empty file would parse as no
		// hooks, and the seat would launch believing it was gated by one.
		return nil
	}
	return body
}

// readOnlyTools are the tool calls council answers yes to without drawing a
// card, and this list is the price of the hook being matcherless.
//
// The hook asks about EVERYTHING — measured, and it is the point: `Read`,
// `Glob` and `git status` all raised a request under it, and under the older
// posture none of the three raised anything at all, because Claude Code
// approves what it classifies read-only before the callback. So the build that
// closes the gate's last hole also hands the room three times the cards, and a
// gate that fires on everything is one people stop reading — that is not a
// guess, it is this room's own history: the first session with the gate carded
// the user THIRTY-FOUR times and autoApproveRoutine exists because of it.
//
// A POSITIVE list, and the direction is the whole argument. An unknown tool —
// one Claude Code adds next month — falls through to a card. That costs one
// keystroke. The complement, deriving this from the write tools, would
// auto-approve every tool nobody has heard of yet, which spends the operator's
// trust on a call they never saw.
//
// TodoWrite is deliberately absent although it would be the noisiest omission.
// Nothing here measured what it writes, and this repo does not put a tool on an
// auto-approve list off the strength of its name reading harmless.
var readOnlyTools = map[string]bool{
	"Read":         true,
	"Glob":         true,
	"Grep":         true,
	"NotebookRead": true,
}

// isReadOnlyTool reports whether a tool call changes nothing, by name.
//
// Name-only, and never a judgement about the arguments: `Read` on a credential
// file is still a read, and screening THAT is the operator's own PreToolUse
// hook's job — which this build is what gives back to the seat. Bash is not on
// the list and cannot be, because a shell command's name says nothing about
// what it does; autoApproveRoutine classifies those, and it inspects the
// command.
func isReadOnlyTool(tool string) bool {
	return readOnlyTools[strings.TrimSpace(tool)]
}
