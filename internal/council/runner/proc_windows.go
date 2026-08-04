//go:build windows

package runner

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowsGroup kills a process tree with a Job Object.
//
// This is not belt-and-braces on Windows, it is the only thing that works for
// the vendor we most need to kill. `codex` resolves to an npm .cmd shim, so the
// process we start is cmd.exe, which starts node, which starts the real
// executable. Killing our direct child leaves an agent running, holding a
// session and spending quota, with nothing on screen to say so.
//
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE also covers the case we cannot code
// around: if telltale itself dies, the handle closes and Windows reaps the
// whole tree for us.
type windowsGroup struct {
	job windows.Handle
}

func newProcGroup() procGroup { return &windowsGroup{} }

func (g *windowsGroup) prepare(cmd *exec.Cmd) {
	// CREATE_NEW_PROCESS_GROUP stops a console Ctrl+C from reaching the child
	// on its own. Cancellation goes through the job object instead, so that one
	// path handles both the keystroke and a context deadline.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP,
	}
}

func (g *windowsGroup) attach(cmd *exec.Cmd) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return err
	}

	h, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		windows.CloseHandle(job)
		return err
	}
	defer windows.CloseHandle(h)

	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		windows.CloseHandle(job)
		return err
	}
	g.job = job
	return nil
}

func (g *windowsGroup) kill() error {
	if g.job == 0 {
		return nil
	}
	return windows.TerminateJobObject(g.job, 1)
}

func (g *windowsGroup) close() error {
	if g.job == 0 {
		return nil
	}
	h := g.job
	g.job = 0
	return windows.CloseHandle(h)
}
