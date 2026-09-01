package councilhost

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubRoomJob replaces the room job for an IN-PROCESS host.
//
// Not optional and not tidiness. NewRoomJob assigns the calling process into a
// job carrying JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, so a test that let a host
// build a real one would put the `go test` binary in that job — and the first
// Shutdown, which closes the last handle, would terminate the whole suite
// mid-run. newRoomJob's doc says the same thing from the other side.
func stubRoomJob(t *testing.T) {
	t.Helper()
	orig := newRoomJob
	newRoomJob = func() (*RoomJob, error) { return &RoomJob{}, nil }
	t.Cleanup(func() { newRoomJob = orig })
}

// readRoom waits for the next room frame.
func readRoom(t *testing.T, fr *FrameReader) (Room, error) {
	t.Helper()
	for {
		f, err := fr.Read()
		if err != nil {
			return Room{}, err
		}
		if f.Kind == KindRoom && f.Room != nil {
			return *f.Room, nil
		}
	}
}

// awaitRoom reads room frames until one satisfies want, or the test fails.
//
// Reading until the condition holds rather than asserting on the first frame:
// the host coalesces, so how many frames a change arrives across is a timing
// detail, and a test that assumed one would be flaky in exactly the way a
// coalescing tick makes things flaky.
func awaitRoom(t *testing.T, fr *FrameReader, want func(Room) bool) Room {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last Room
	for time.Now().Before(deadline) {
		r, err := readRoom(t, fr)
		if err != nil {
			t.Fatalf("the host stopped sending rooms: %v (last: %+v)", err, last)
		}
		last = r
		if want(r) {
			return r
		}
	}
	t.Fatalf("no room frame satisfied the condition; the last one was %+v", last)
	return Room{}
}

// assertGrepTreeWorks is the POSITIVE CONTROL for grepTree.
//
// grepTree swallows every walk and read error and returns "" — so a broken
// searcher, a wrong root, or a WalkDir that fails at the top all look exactly
// like "the marker is not on disk", and the assertion built on it passes while
// measuring nothing. That is the same class of defect as the stand-in host that
// died on Go's deadlock detector while two containment tests went green.
//
// So before a test trusts a negative from grepTree, it plants the marker, finds
// it, and removes it. A searcher that cannot find a file it was just handed
// cannot be believed when it says a tree is clean.
func assertGrepTreeWorks(t *testing.T, root, marker string) {
	t.Helper()
	canary := filepath.Join(root, "grep-canary.txt")
	if err := os.WriteFile(canary, []byte("prefix "+marker+" suffix"), 0o600); err != nil {
		t.Fatalf("could not plant the canary: %v", err)
	}
	if found := grepTree(t, root, marker); found == "" {
		t.Fatalf("grepTree could not find a marker it was just handed, under %s. "+
			"Every negative it reports is therefore worthless.", root)
	}
	if err := os.Remove(canary); err != nil {
		t.Fatalf("could not remove the canary: %v", err)
	}
	if found := grepTree(t, root, marker); found != "" {
		t.Fatalf("grepTree still finds the marker at %s after the canary was removed", found)
	}
}

// grepTree searches every regular file under root for marker and returns the
// first path that holds it.
//
// Read whole rather than streamed: these are test trees with a handful of small
// files, and a partial read is how a search like this reports clean over a
// marker that spans a chunk boundary.
//
// Errors are swallowed, which makes a negative from this function untrustworthy
// on its own. Call assertGrepTreeWorks first.
func grepTree(t *testing.T, root, marker string) string {
	t.Helper()
	var found string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(b), marker) {
			found = path
		}
		return nil
	})
	return found
}
