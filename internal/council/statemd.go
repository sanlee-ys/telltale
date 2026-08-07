package council

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// stateMDStaleNotice is the room-open line for a pickup doc that has drifted
// behind HEAD.
//
// STATE.md holds intent git cannot — and it has gone stale that way three times
// already (its own preamble). The gap used to be invisible: a room opened on a
// clone whose pickup doc last moved N commits ago looked exactly like a room
// whose pickup doc was current. Comparing `git log -1 STATE.md` against HEAD
// is the measurement; the notice is the render. Zero and unmeasurable both
// stay silent — collapsing "current" and "we could not read git" into a fake
// number would be the honesty bug this product exists to refuse (§4a.1).
//
// The check is against the ROOM's workspace, not telltale's install path: a
// council session pointed at another tree has no claim on this repo's pickup
// doc, and inventing one would be a lie about where the room is working.
func stateMDStaleNotice(workspace string) string {
	n, ok := measureStateMDBehind(workspace)
	if !ok || n <= 0 {
		return ""
	}
	unit := "commits"
	if n == 1 {
		unit = "commit"
	}
	return "STATE.md is " + strconv.Itoa(n) + " " + unit + " behind HEAD"
}

// measureStateMDBehind returns how many commits HEAD has advanced since
// STATE.md was last touched. ok is false when the gap cannot be measured
// (no file, not a git repo, git missing, empty history) — never a guessed zero.
//
// Overridable in tests so the notice wording does not depend on a live clone.
var measureStateMDBehind = measureStateMDBehindGit

func measureStateMDBehindGit(workspace string) (n int, ok bool) {
	if workspace == "" {
		return 0, false
	}
	path := filepath.Join(workspace, "STATE.md")
	if _, err := os.Stat(path); err != nil {
		// Absent file: not a telltale workspace. Silence, not "0 behind".
		return 0, false
	}
	sha, err := gitOutput(workspace, "log", "-1", "--format=%H", "--", "STATE.md")
	if err != nil || sha == "" {
		return 0, false
	}
	count, err := gitOutput(workspace, "rev-list", "--count", sha+"..HEAD")
	if err != nil {
		return 0, false
	}
	n, err = strconv.Atoi(count)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func gitOutput(workspace string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", workspace}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
