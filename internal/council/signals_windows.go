//go:build windows

package council

// watchExitSignals does nothing on Windows, and the reason is a capability
// difference rather than an omission.
//
// The signals its non-Windows twin catches are the reason it exists: on unix a
// process group is a name for a set of processes, not a lifetime bound to
// telltale's own, so a room that dies without killing its seats leaves them
// running. Windows has the opposite property built in — every seat is in a job
// object created with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE (runner/proc_windows.go),
// and when telltale's process dies its handle closes and Windows reaps the whole
// tree. The abnormal exit is already covered here, by the OS, on every way out
// including the ones no handler can catch.
//
// A console close (CTRL_CLOSE_EVENT, delivered through SetConsoleCtrlHandler)
// is a different mechanism from a POSIX signal and is NOT what this file
// declines to implement — it is out of this change's scope entirely, and the job
// object covers the seats through it for the same reason. If a reason to handle
// it ever appears, it belongs in its own change with its own measurement.
func watchExitSignals(*Model, roomProgram) (stop func()) { return func() {} }
