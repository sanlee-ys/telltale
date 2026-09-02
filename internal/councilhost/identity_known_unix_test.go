//go:build linux || darwin

package councilhost

// noProcessTable is nil here: Linux and macOS read the process table, so no
// identity test may skip.
var noProcessTable error
