//go:build !windows

package councilhost

// runDetachHost and runDetachClient have nothing to stand in for off Windows.
//
// The host does not run on this platform at all (ErrNotBuiltHere), and the
// property the pair measures is a Job Object reaping a detached host's seats —
// which proc_unix.go's dated measurement says has no equivalent here: a process
// group NAMES a set of processes and does not BIND their lifetimes. A helper
// here would have to assert a property that is false on this platform, so there
// is none. roomjob_other_test.go's stand-in says the same thing about the same
// claim.
func runDetachHost() int { return 0 }

// runDetachClient refuses for runDetachHost's reason: there is no host here to
// detach from.
func runDetachClient() int { return 0 }
