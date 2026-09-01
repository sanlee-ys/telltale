//go:build !windows

package councilhost

import (
	"errors"
	"time"
)

// ErrHostGone is returned when the pid a discovery file names is not running.
var ErrHostGone = errors.New("councilhost: the process host.json names is not running")

// ErrNotTelltale is returned when the pid is running and is somebody else.
var ErrNotTelltale = errors.New("councilhost: the process host.json names is not a telltale host")

// verifyHostProcess refuses off Windows, and Probe never reaches it.
//
// ProbePipe already answers PipeAbsent on this platform, because a host cannot
// run here at all (ErrNotBuiltHere), so Probe returns before an identity check
// is asked for. This exists to compile, and it refuses rather than returning
// nil: a future caller that reached it must not be told a host was verified on
// a platform that cannot run one.
func verifyHostProcess(pid int, startedAt time.Time) error { return ErrNotBuiltHere }

// killProcess refuses off Windows, for verifyHostProcess's reason.
//
// The stronger reason is the one proc_unix.go measured: a process group NAMES a
// set of processes and does not BIND their lifetimes, so ending a host here
// would LEAK every seat rather than reap it. A kill that left the agents running
// would be the worst possible spelling of this command.
func killProcess(pid int) error { return ErrNotBuiltHere }
