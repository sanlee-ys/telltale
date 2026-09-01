//go:build !windows

package councilhost

import "os/exec"

// hideHostConsole has nothing to hide off Windows: there is no console
// allocation to suppress and no window to show. The host does not run on this
// platform anyway — see ErrNotBuiltHere.
func hideHostConsole(cmd *exec.Cmd) {}
