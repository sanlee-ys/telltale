//go:build !windows && !linux && !darwin

package councilhost

import (
	"errors"
	"time"
)

// truncatedImageLen is unused here: no image name is ever read.
const truncatedImageLen = -1

// errNoProcessTable is why every reading in this file refuses.
//
// A Unix this package has no process-table reading for can build the host and
// run it, and what it loses is stated: verifyHostProcess cannot vouch for a
// pid, so a discovery file here is reported as a host that cannot be
// identified and is never acted on, and killProcess degrades to the host's own
// process group and says so. That is a narrower surface than a guess about a
// platform nobody measured.
var errNoProcessTable = errors.New("councilhost: no process-table reading exists for this platform yet")

func processImage(pid int) (string, error)    { return "", errNoProcessTable }
func processStart(pid int) (time.Time, error) { return time.Time{}, errNoProcessTable }
func sessionMembers(sid int) ([]int, error)   { return nil, errNoProcessTable }
