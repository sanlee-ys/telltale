//go:build !windows

package councilhost

// runStandInHost has nothing to stand in for off Windows.
//
// The containment it measures is a Job Object, and proc_unix.go's dated
// measurement is that this platform has no equivalent: a process group names a
// set of processes and does not bind their lifetimes. A helper here would have
// to assert a property that is false, so there is none.
func runStandInHost() int { return 0 }
