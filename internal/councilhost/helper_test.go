package councilhost

import (
	"os"
	"time"
)

// helperEnv turns this test binary into one of the two stand-in processes the
// containment measurement needs.
//
// Re-executing the test binary is this repo's own precedent, not a new idea:
// arenacheck_test.go runs THIS TEST BINARY as the arena check command, because
// that is the only way to assert the claim the whole feature rests on. The
// claim here is the same shape — that a hard-killed host really does reap its
// seats — and it cannot be asserted without two real processes.
//
// Neither helper spawns a vendor and neither reaches a spawn var, so neither
// goes past TestMain's guard. They run BEFORE the guard is armed because they
// are not tests: a helper process runs no test at all.
const helperEnv = "TELLTALE_COUNCILHOST_TEST_ROLE"

const (
	// helperHost is the stand-in host: it builds the room job, puts itself in
	// it, starts a seat, prints the seat's pid, and then waits to be killed.
	helperHost = "host"
	// helperSeat is the stand-in vendor: a process that stays alive long enough
	// that its death can only be the job object's doing.
	helperSeat = "seat"
)

// seatLifetime is how long a stand-in seat stays up.
//
// Long enough that it CANNOT exit on its own inside the measurement window.
// That matters: a seat which timed out by itself would make the reap test pass
// while measuring nothing, which is the failure mode a containment test can
// least afford.
const seatLifetime = 3 * time.Minute

// runTestHelper runs a stand-in process and reports whether it did.
func runTestHelper() (int, bool) {
	switch os.Getenv(helperEnv) {
	case helperSeat:
		time.Sleep(seatLifetime)
		return 0, true
	case helperHost:
		return runStandInHost(), true
	default:
		return 0, false
	}
}
