//go:build windows

package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/sanlee-ys/telltale/internal/model"
)

// This file is the ONLY place in telltale that starts a process without
// os/exec, and it has no choice about it.
//
// A child attaches to a pseudoconsole through PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
// which lives in a PROC_THREAD_ATTRIBUTE_LIST, which is only honoured when
// STARTUPINFOEX is passed with EXTENDED_STARTUPINFO_PRESENT. Go's
// syscall.SysProcAttr on Windows has no field for an attribute list, so
// exec.Command cannot express any of it. The spawn half is therefore a direct
// windows.CreateProcess. The CONTAINMENT half is not rewritten: windowsGroup's
// job object was measured working unchanged on a ConPTY child, so this file
// calls the same attachPid the exec.Cmd path calls.
//
// # CREATE_NO_WINDOW MUST NOT BE SET, and getting this wrong is silent
//
// windowsGroup.prepare sets CREATE_NEW_PROCESS_GROUP|CREATE_NO_WINDOW and
// HideWindow on every other child this package starts, and copying it here is
// the most likely way to write this file wrong. Measured 2026-08-31 on build
// 10.0.26200: a ConPTY child created with CREATE_NO_WINDOW emits ZERO bytes —
// not even conhost's own preamble — accepts no input, and exits 0 while
// CreateProcess returns a NIL error. There is no log line and no failure to
// catch; the pane is simply blank. DETACHED_PROCESS fails the same way.
// CREATE_NEW_PROCESS_GROUP and HideWindow are both compatible and both
// measured working, so both are kept.
//
// Nothing is lost by dropping the flag. CREATE_NO_WINDOW exists to stop a
// console flash from an npm .cmd shim under anonymous pipes, and a child
// holding the pseudoconsole attribute never asks Windows for a console at all —
// it joins the pseudoconsole's own headless conhost. A differential EnumWindows
// scan carrying a positive control found zero new visible windows across every
// flag combination, including twenty-second runs of the real agent CLI.
//
// # STARTF_USESTDHANDLES is required, and this is the other non-obvious one
//
// Without it, and with all three std handles left at zero, the child does not
// attach to the pseudoconsole. It keeps the PARENT's std handles instead, even
// with bInheritHandles false and even when the parent owns no console:
// `cmd /c echo MARKER` printed on the spike's own stdout and the pty stream
// stayed empty. That one flag is the difference between a pane and nothing.

// ptyReadChunk is the read buffer size for one chunk off the pseudoconsole.
//
// 16 KiB rather than the 64 KiB the line pump uses. A screen is bounded by its
// own cell rectangle, so a chunk larger than a full repaint buys nothing, and a
// smaller read reaches the emulator sooner. A 5000-line flood measured 369 KB
// over 6.5 seconds, which is about 23 of these per second.
const ptyReadChunk = 16 << 10

// winPTY is one pseudoconsole and the child attached to it.
type winPTY struct {
	hpc    windows.Handle
	in     *os.File // write here -> the child's stdin
	out    *os.File // read here  <- the child's screen, VT-encoded
	proc   windows.Handle
	thread windows.Handle
	pid    uint32
	group  procGroup

	mu     sync.Mutex
	closed bool
	killed bool
	done   chan struct{}
}

// StartPTY runs spec under a pseudoconsole sized to cols by rows and streams
// the child's screen into out.
//
// out is BOUNDED by its caller and this function blocks on it, which is the
// same backpressure decision dispatch.go argues for the event channel: a full
// channel stalls the child, and a stalled pane is honest where a lagging one is
// not.
func StartPTY(ctx context.Context, spec Spec, cols, rows int, out chan<- PTYChunk) (PTYSession, error) {
	if err := ptyBuildSupported(); err != nil {
		return nil, err
	}
	if cols < 1 || rows < 1 {
		return nil, fmt.Errorf("runner: a live seat needs a pane at least 1x1, got %dx%d", cols, rows)
	}
	// The same question the operating system is about to ask, asked here so a
	// missing vendor reports as a missing vendor rather than as a
	// CreateProcess error code.
	bin, err := exec.LookPath(spec.Binary)
	if err != nil {
		return nil, fmt.Errorf("runner: cannot start a live seat for %s: %w", spec.Binary, err)
	}

	p := &winPTY{group: newProcGroup(), done: make(chan struct{})}
	if err := p.open(cols, rows); err != nil {
		p.teardown()
		return nil, err
	}
	if err := p.spawn(windows.ComposeCommandLine(append([]string{bin}, spec.Args...)), spec.Dir); err != nil {
		p.teardown()
		return nil, err
	}
	// The documented race is the same one startSession carries: a grandchild
	// spawned between CreateProcess and this line escapes the job. Accepted
	// there, accepted here, and for the same reason — the alternative is
	// creating the process suspended, which is a second thing to get right on
	// a path that already cannot use os/exec.
	if err := p.group.attachPid(p.pid); err != nil {
		p.Kill()
		p.teardown()
		return nil, fmt.Errorf("runner: a live seat could not be contained, so it was not started: %w", err)
	}

	go p.pump(spec.Vendor, out)
	go func() {
		select {
		case <-ctx.Done():
			p.Kill()
		case <-p.done:
		}
	}()
	return p, nil
}

// ptyBuildSupported refuses below the build ConPTY appeared in.
//
// The floor is DOCUMENTED and not measured here, and the refusal says so in the
// only way that helps: it names the build it found. §9.53 records the whole of
// what was measured — one build, 26200 — so an operator on an older machine
// gets a sentence rather than a pane that silently draws nothing.
func ptyBuildSupported() error {
	v := windows.RtlGetVersion()
	if v == nil {
		return fmt.Errorf(
			"a live seat needs Windows build %d or later, and this machine's build could not be read",
			minPTYBuild)
	}
	if v.BuildNumber < minPTYBuild {
		return fmt.Errorf(
			"a live seat needs Windows build %d or later for a pseudoconsole; this machine reports %d.%d.%d",
			minPTYBuild, v.MajorVersion, v.MinorVersion, v.BuildNumber)
	}
	return nil
}

// open creates the two pipes and the pseudoconsole itself.
func (p *winPTY) open(cols, rows int) error {
	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return fmt.Errorf("runner: CreatePipe(in): %w", err)
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		return fmt.Errorf("runner: CreatePipe(out): %w", err)
	}
	var hpc windows.Handle
	if err := windows.CreatePseudoConsole(
		windows.Coord{X: int16(cols), Y: int16(rows)}, inRead, outWrite, 0, &hpc,
	); err != nil {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		windows.CloseHandle(outWrite)
		return fmt.Errorf("runner: CreatePseudoConsole: %w", err)
	}
	// The pseudoconsole holds its own duplicates of these two ends now, and the
	// parent must drop them: keeping the write end alive means the read side
	// never sees EOF, so a dead child would look like a live one that had
	// stopped talking.
	windows.CloseHandle(inRead)
	windows.CloseHandle(outWrite)

	p.hpc = hpc
	p.in = os.NewFile(uintptr(inWrite), "telltale-pty-in")
	p.out = os.NewFile(uintptr(outRead), "telltale-pty-out")
	return nil
}

// spawn is the direct CreateProcess. Read this file's header before changing
// any flag here.
func (p *winPTY) spawn(cmdline, dir string) error {
	al, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return fmt.Errorf("runner: NewProcThreadAttributeList: %w", err)
	}
	defer al.Delete()

	// lpValue for PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE is the HPCON VALUE, not a
	// pointer to it. Reading the handle's bits AS a pointer is what keeps this
	// free of a uintptr-to-unsafe.Pointer conversion, which go vet's unsafeptr
	// check would reject.
	hv := *(*unsafe.Pointer)(unsafe.Pointer(&p.hpc))
	if err := al.Update(
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, hv, unsafe.Sizeof(p.hpc),
	); err != nil {
		return fmt.Errorf("runner: UpdateProcThreadAttribute: %w", err)
	}

	si := new(windows.StartupInfoEx)
	si.StartupInfo.Cb = uint32(unsafe.Sizeof(*si))
	// Required. See the header: without it the child keeps the PARENT's std
	// handles and the pty stream stays empty.
	si.StartupInfo.Flags |= windows.STARTF_USESTDHANDLES | windows.STARTF_USESHOWWINDOW
	si.StartupInfo.ShowWindow = windows.SW_HIDE
	si.ProcThreadAttributeList = al.List()

	cl, err := windows.UTF16PtrFromString(cmdline)
	if err != nil {
		return err
	}
	var dirp *uint16
	if dir != "" {
		if dirp, err = windows.UTF16PtrFromString(dir); err != nil {
			return err
		}
	}

	// CREATE_NO_WINDOW and DETACHED_PROCESS are absent on purpose and their
	// absence is load-bearing. Adding either one makes this child emit nothing
	// at all, with no error anywhere.
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT |
		windows.CREATE_UNICODE_ENVIRONMENT |
		windows.CREATE_NEW_PROCESS_GROUP)

	pi := new(windows.ProcessInformation)
	if err := windows.CreateProcess(
		nil, cl, nil, nil, false, flags, nil, dirp, &si.StartupInfo, pi,
	); err != nil {
		return fmt.Errorf("runner: CreateProcess: %w", err)
	}
	p.proc, p.thread, p.pid = pi.Process, pi.Thread, pi.ProcessId
	return nil
}

// pump reads the screen and reports exactly one terminal chunk.
//
// One terminal chunk, for the reason a Session emits one terminal event: a pane
// that was told twice that its child had gone would have to decide which report
// to believe, and there is no honest way to pick.
func (p *winPTY) pump(vendor model.VendorID, out chan<- PTYChunk) {
	defer close(p.done)
	buf := make([]byte, ptyReadChunk)
	var note string
	for {
		n, err := p.out.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			out <- PTYChunk{Vendor: vendor, Data: chunk}
		}
		if err != nil {
			// A killed session's pipe closing is not a failure. The room ended
			// it, and reporting an error there would put a red note under a
			// pane the operator closed on purpose.
			if !p.wasKilled() && !isPipeEnd(err) {
				note = err.Error()
			}
			break
		}
	}
	if code, ok := p.exitCode(); ok && code != 0 && note == "" && !p.wasKilled() {
		note = fmt.Sprintf("the live seat exited with code %d", code)
	}
	out <- PTYChunk{Vendor: vendor, Done: true, Note: note}
	p.teardown()
}

// isPipeEnd reports whether an error is the ordinary end of a pty read.
//
// A pseudoconsole's read side does not report io.EOF on Windows; the pipe is
// broken instead, and that is what a child exiting looks like from here.
func isPipeEnd(err error) bool {
	if err == nil {
		return false
	}
	var errno windows.Errno
	if e, ok := err.(interface{ Unwrap() error }); ok {
		if u, ok := e.Unwrap().(windows.Errno); ok {
			errno = u
		}
	}
	switch errno {
	case windows.ERROR_BROKEN_PIPE, windows.ERROR_PIPE_NOT_CONNECTED, windows.ERROR_HANDLE_EOF:
		return true
	}
	return err.Error() == "EOF"
}

func (p *winPTY) exitCode() (uint32, bool) {
	if p.proc == 0 {
		return 0, false
	}
	var code uint32
	if err := windows.GetExitCodeProcess(p.proc, &code); err != nil {
		return 0, false
	}
	const stillActive = 259
	if code == stillActive {
		return 0, false
	}
	return code, true
}

// Resize changes the pseudoconsole's rectangle. The caller resizes its emulator
// in the same operation.
func (p *winPTY) Resize(cols, rows int) error {
	if cols < 1 || rows < 1 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.hpc == 0 {
		return nil
	}
	return windows.ResizePseudoConsole(p.hpc, windows.Coord{X: int16(cols), Y: int16(rows)})
}

func (p *winPTY) Write(b []byte) error {
	p.mu.Lock()
	f, closed := p.in, p.closed
	p.mu.Unlock()
	if closed || f == nil {
		return os.ErrClosed
	}
	_, err := f.Write(b)
	return err
}

// Kill ends the child through the job object.
//
// The job, and not ClosePseudoConsole, because ClosePseudoConsole was MEASURED
// leaving a claude.exe REPL alive more than three seconds past the close — an
// orphaned agent holding a session and spending quota, which is the exact
// failure the job object was written to prevent.
func (p *winPTY) Kill() {
	p.mu.Lock()
	if p.killed {
		p.mu.Unlock()
		return
	}
	p.killed = true
	p.mu.Unlock()
	_ = p.group.kill()
}

func (p *winPTY) Alive() bool {
	select {
	case <-p.done:
		return false
	default:
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.closed
}

func (p *winPTY) Pid() int { return int(p.pid) }

func (p *winPTY) wasKilled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.killed
}

// teardown releases the pseudoconsole, the pipes and the job.
//
// Order matters and it is the measured order: the job handle closes FIRST, so
// kill-on-close reaps the tree, and ClosePseudoConsole runs against a child
// that is already gone. Closing the pseudoconsole first is the path that
// orphans.
func (p *winPTY) teardown() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	hpc, in, out, proc, thread := p.hpc, p.in, p.out, p.proc, p.thread
	p.hpc, p.in, p.out, p.proc, p.thread = 0, nil, nil, 0, 0
	p.mu.Unlock()

	_ = p.group.close()
	if hpc != 0 {
		windows.ClosePseudoConsole(hpc)
	}
	if in != nil {
		in.Close()
	}
	if out != nil {
		out.Close()
	}
	if thread != 0 {
		windows.CloseHandle(thread)
	}
	if proc != 0 {
		windows.CloseHandle(proc)
	}
}
