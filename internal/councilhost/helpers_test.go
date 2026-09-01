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

// grepTree searches every regular file under root for marker and returns the
// first path that holds it.
//
// Read whole rather than streamed: these are test trees with a handful of small
// files, and a partial read is how a search like this reports clean over a
// marker that spans a chunk boundary.
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
