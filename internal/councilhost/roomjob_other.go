//go:build !windows

package councilhost

// RoomJob is the Windows containment, and it has no equivalent here.
//
// runner/proc_unix.go carries the measurement, dated 2026-08-17 on macOS
// 26.5.2: a process group NAMES a set of processes and does not BIND their
// lifetimes, and a program of that shape survived SIGINT, SIGTERM, SIGHUP and
// SIGKILL. So there is nothing on this platform that reaps a hard-killed host's
// seats, and pretending otherwise with a type that returns nil errors would
// make design.md §7.28's containment table read true on a platform where it is
// false.
//
// The type exists so the host compiles. Every entry point refuses, and the host
// itself refuses one step earlier at Listen — see ErrNotBuiltHere.
type RoomJob struct{}

// NewRoomJob refuses off Windows. See ErrNotBuiltHere.
func NewRoomJob() (*RoomJob, error) { return nil, ErrNotBuiltHere }

// Kill is a no-op off Windows.
func (j *RoomJob) Kill() error { return nil }

// Close is a no-op off Windows.
func (j *RoomJob) Close() error { return nil }
