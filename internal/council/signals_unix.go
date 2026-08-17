//go:build !windows

package council

import (
	"os"
	"os/signal"
	"syscall"
)

// watchExitSignals runs the room's teardown before an abnormal exit, and is the
// non-Windows half of "quitting the room never leaves an agent running".
//
// # What Bubble Tea already does, measured
//
// Measured 2026-08-17 on the macOS box (Intel x86_64, macOS 26.5.2) against
// bubbletea v2.0.8, with a throwaway program that spawned a `sleep` child in its
// own process group — the shape runner/proc_unix.go gives every seat — and then
// took each signal with no handler of its own:
//
//	signal   what Bubble Tea did to the program              did Update run?  child
//	SIGINT   p.Run() returned "program was killed:           no               ORPHANED
//	         program was interrupted"
//	SIGTERM  p.Run() returned nil                            no               ORPHANED
//	SIGHUP   nothing at all — the default disposition        no               ORPHANED
//	         killed the process outright
//	SIGKILL  nothing, and nothing can                        no               ORPHANED
//
// So Bubble Tea does end the program on two of the four, and ending the program
// is the whole of what it does. Its handler answers above the model's head:
// tea.go's eventLoop returns on QuitMsg and on InterruptMsg BEFORE it calls
// model.Update, so the model is never handed the message and council's teardown
// — reachable only from the q and ctrl+c key handlers — did not run on any of
// them. Five seats kept running, holding sessions and spending quota, with no
// room attached. That is the defect this file closes.
//
// # Why the two arms are not the same act
//
//   - SIGINT and SIGTERM: teardown, and nothing else. Bubble Tea's own handler
//     already brings the program down — measured above — so a second shutdown
//     initiator here would buy nothing, and it would run inside a window the
//     source shows is delicate: at that moment Bubble Tea's signal goroutine is
//     blocked on an UNBUFFERED `p.msgs <- QuitMsg{}` (tea.go's msgs channel is
//     made with no capacity), while p.shutdown waits for that same goroutine to
//     return. A probe that DID call Kill on SIGTERM ran 20 times without hanging,
//     so the claim here is only that the Kill is unnecessary — not that a hang
//     was measured.
//   - SIGHUP: teardown, then Kill. Bubble Tea never registers SIGHUP, so nothing
//     else will end the program, and installing this handler has just displaced
//     the default disposition that would have. Kill is safe here for the same
//     reason it is unnecessary above — no Bubble Tea goroutine is mid-send on a
//     signal it did not subscribe to — and it restores the terminal, which the
//     default disposition would not have. Measured with the handler installed:
//     the child is reaped and p.Run() returns ErrProgramKilled.
//
// SIGKILL is absent from the list and cannot be added: it is uncatchable, so
// `kill -9` on the room still orphans every seat and this file does not pretend
// otherwise. On Windows the job object covers that case and unix has no
// equivalent — see runner/proc_unix.go and PARITY.md.
//
// The returned stop deregisters the watcher. Run defers it, so a room that ends
// any other way does not leave a goroutine parked on a channel.
func watchExitSignals(m *Model, p roomProgram) (stop func()) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan struct{})

	go func() {
		select {
		case <-done:
			return
		case s := <-sig:
			// First, and synchronously. Killing a seat is a signal to a process
			// group and cannot block, so it happens before anything slower is
			// attempted — and before the process can be taken down by whatever
			// sent this one.
			m.teardown()
			// Handed back to its default disposition now that the seats are
			// dead. A SECOND signal must do what it would have done with no
			// handler here — end the room — rather than land in a channel
			// nobody reads again. Deliberately after the teardown, so the
			// signal that arrives while the seats are being killed cannot cut
			// that short.
			signal.Stop(sig)
			if s == syscall.SIGHUP {
				p.Kill()
			}
		}
	}()

	return func() {
		signal.Stop(sig)
		close(done)
	}
}
