//go:build !windows

package councilhost

// errNoProcessTableOnThisPlatform is what identity_other.go's readings return,
// and nil on the two platforms that have readings — so a test can skip on the
// first and never on the second.
func errNoProcessTableOnThisPlatform() error { return noProcessTable }
