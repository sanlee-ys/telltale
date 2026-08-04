//go:build windows

package runner

import (
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

// TestChildrenGetNoConsoleWindow pins the flags that keep a console from
// flashing over the room.
//
// This cannot be asserted by observing the screen from a test, so it asserts
// the mechanism instead: both suppression flags must be set on every child.
// The failure it guards against is not subtle to a user — dispatching to Codex
// pops a visible cmd.exe window on top of the alternate screen, because that
// vendor resolves to an npm .cmd shim and the process we start is therefore a
// console application.
func TestChildrenGetNoConsoleWindow(t *testing.T) {
	cmd := exec.Command("does-not-need-to-exist")
	newProcGroup().prepare(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("no SysProcAttr set; the child would inherit default console behaviour")
	}
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Error("CREATE_NO_WINDOW is not set: dispatching would flash a console window")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Error("HideWindow is not set; it covers launcher chains CREATE_NO_WINDOW alone can miss")
	}
	// Still required for the teardown path: cancellation goes through the job
	// object, not through a console Ctrl+C.
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Error("CREATE_NEW_PROCESS_GROUP was dropped; cancellation semantics depend on it")
	}
}
