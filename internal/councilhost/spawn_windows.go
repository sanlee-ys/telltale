//go:build windows

package councilhost

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// hideHostConsole starts the host with no visible console.
//
// CREATE_NO_WINDOW and HideWindow, the pair proc_windows.go already measured
// against a real flash: without them a console application gets a console, and
// Bubble Tea is holding the alternate screen at that moment, so the flash lands
// on top of the room.
//
// **Not DETACHED_PROCESS, and that is a deliberate deferral.** design.md
// §7.28's transport survey names DETACHED_PROCESS as what a host wants, because
// it gives the process no console at all — but that matters only once a host
// must OUTLIVE its client, which is rung 4 and is not exposed here. The two
// flags are mutually exclusive, and whether Go's
// syscall.SysProcAttr{CreationFlags: DETACHED_PROCESS} composes cleanly with
// HideWindow was NOT measured. Reaching for the unmeasured flag to buy a
// property this change does not ship would be exactly the guess this repo's
// honesty rule refuses. The flag that IS measured is used, and the swap is
// named here so rung 4 measures rather than inherits.
func hideHostConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW,
		HideWindow:    true,
	}
}
