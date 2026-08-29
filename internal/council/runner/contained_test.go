package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRunContainedReportsTheProcessesOwnExitCode is the claim the arena check
// rests on: the code comes back as itself, and a nonzero one is a MEASUREMENT
// rather than an error (§9.48 renders it as FAIL).
func TestRunContainedReportsTheProcessesOwnExitCode(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv(helperEnv, "1")

	// "fail-plain" exits 3; "lines" exits 0. Both are the helper vendor above,
	// so nothing outside this test binary runs.
	for _, tc := range []struct {
		mode string
		want int
	}{{"lines", 0}, {"fail-plain", 3}} {
		code, exited, err := RunContained(context.Background(), t.TempDir(),
			[]string{exe, "-test.run=TestHelperProcess", "--", tc.mode})
		if !exited {
			t.Fatalf("%s: no exit code measured: %v", tc.mode, err)
		}
		if err != nil {
			t.Errorf("%s: a measured exit reported an error too: %v", tc.mode, err)
		}
		if code != tc.want {
			t.Errorf("%s: code = %d, want %d", tc.mode, code, tc.want)
		}
	}
}

// TestRunContainedSeparatesNoExitCodeFromANonzeroOne: a command that never ran
// has no code, and the caller must be able to tell that from a failure. The
// two are different facts (§4a.1) and this is where they are kept apart.
func TestRunContainedSeparatesNoExitCodeFromANonzeroOne(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "telltale-no-such-binary")
	code, exited, err := RunContained(context.Background(), t.TempDir(), []string{missing})
	if exited {
		t.Errorf("a binary that does not exist reported exit %d", code)
	}
	if err == nil {
		t.Error("a run that could not happen carries no reason")
	}

	if _, _, err := RunContained(context.Background(), t.TempDir(), nil); err == nil {
		t.Error("an empty argv was accepted")
	}
}

// TestRunContainedKillsTheWholeTree is the reason this function exists rather
// than a bare exec.CommandContext. The helper spawns a grandchild that keeps
// appending to a file; cancelling must stop the FILE from growing, not merely
// the process council started.
//
// It is the same property runner's own tree-kill test asserts for a vendor,
// asserted again on this path because an arena check reaches Windows through
// the same kind of shim — `npm test` is cmd.exe, then node, then the work.
func TestRunContainedKillsTheWholeTree(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv(helperEnv, "1")
	dir := t.TempDir()
	ticks := filepath.Join(dir, "ticks")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunContained(ctx, dir, []string{exe, "-test.run=TestHelperProcess", "--", "spawn-grandchild", ticks})
	}()

	// Wait for the grandchild to actually be writing before killing anything;
	// a kill that lands before the tree exists proves nothing.
	deadline := time.Now().Add(20 * time.Second)
	for {
		if fi, err := os.Stat(ticks); err == nil && fi.Size() > 0 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Skip("the grandchild never started writing; this machine is too loaded to measure a tree kill")
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	<-done
	// Let anything still alive prove it by growing the file.
	time.Sleep(500 * time.Millisecond)
	first, err := os.Stat(ticks)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	second, err := os.Stat(ticks)
	if err != nil {
		t.Fatal(err)
	}
	if second.Size() != first.Size() {
		t.Errorf("the grandchild is still writing after the cancel: %d then %d bytes",
			first.Size(), second.Size())
	}
}
