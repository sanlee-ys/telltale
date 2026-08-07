package council

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStateMDStaleNoticeWording(t *testing.T) {
	orig := measureStateMDBehind
	t.Cleanup(func() { measureStateMDBehind = orig })

	cases := []struct {
		n       int
		ok      bool
		want    string
		wantEmpty bool
	}{
		{n: 12, ok: true, want: "STATE.md is 12 commits behind HEAD"},
		{n: 1, ok: true, want: "STATE.md is 1 commit behind HEAD"},
		{n: 0, ok: true, wantEmpty: true},
		{n: 5, ok: false, wantEmpty: true},
		{n: -1, ok: true, wantEmpty: true},
	}
	for _, tc := range cases {
		measureStateMDBehind = func(string) (int, bool) { return tc.n, tc.ok }
		got := stateMDStaleNotice("/any")
		if tc.wantEmpty {
			if got != "" {
				t.Fatalf("n=%d ok=%v: got %q, want silence", tc.n, tc.ok, got)
			}
			continue
		}
		if got != tc.want {
			t.Fatalf("n=%d: got %q, want %q", tc.n, got, tc.want)
		}
	}
}

// TestStateMDStaleNoticeJoinsReattach pins the load-bearing join: a room that
// both fails to restore a saved room and has a stale pickup doc must say both,
// not pick one. Either alone would hide the other.
func TestStateMDStaleNoticeJoinsReattach(t *testing.T) {
	orig := measureStateMDBehind
	t.Cleanup(func() { measureStateMDBehind = orig })
	measureStateMDBehind = func(string) (int, bool) { return 3, true }

	m := newWithBrief(Options{Dir: t.TempDir()}, Brief{}, HookSet{}, Reattachment{
		Path:    "-",
		Ignored: "the saved room could not be read",
	})
	if !strings.Contains(m.st.Notice, "the saved room was not restored") {
		t.Fatalf("lost the reattach half: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "STATE.md is 3 commits behind HEAD") {
		t.Fatalf("lost the staleness half: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, " · ") {
		t.Fatalf("halves were not joined: %q", m.st.Notice)
	}
}

func TestNewWithBriefSurfacesStateMDStaleness(t *testing.T) {
	orig := measureStateMDBehind
	t.Cleanup(func() { measureStateMDBehind = orig })
	measureStateMDBehind = func(string) (int, bool) { return 7, true }

	m := newWithBrief(Options{Dir: t.TempDir()}, Brief{}, HookSet{}, Reattachment{})
	if !strings.Contains(m.st.Notice, "STATE.md is 7 commits behind HEAD") {
		t.Fatalf("room opened without the staleness notice: %q", m.st.Notice)
	}
}

func TestMeasureStateMDBehindGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=telltale",
			"GIT_AUTHOR_EMAIL=telltale@example.com",
			"GIT_COMMITTER_NAME=telltale",
			"GIT_COMMITTER_EMAIL=telltale@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	// Default branch name varies by git version; rev-list uses HEAD either way.
	if err := os.WriteFile(filepath.Join(dir, "STATE.md"), []byte("pickup\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "STATE.md")
	run("commit", "-m", "state")
	n, ok := measureStateMDBehindGit(dir)
	if !ok || n != 0 {
		t.Fatalf("fresh STATE.md at HEAD: n=%d ok=%v, want 0/true", n, ok)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "other.txt")
	run("commit", "-m", "other")
	n, ok = measureStateMDBehindGit(dir)
	if !ok || n != 1 {
		t.Fatalf("one commit after STATE.md: n=%d ok=%v, want 1/true", n, ok)
	}

	// No STATE.md → unmeasurable, not zero.
	empty := t.TempDir()
	runEmpty := exec.Command("git", "-C", empty, "init")
	if out, err := runEmpty.CombinedOutput(); err != nil {
		t.Fatalf("git init empty: %v\n%s", err, out)
	}
	if n, ok := measureStateMDBehindGit(empty); ok || n != 0 {
		t.Fatalf("no STATE.md: n=%d ok=%v, want unmeasurable", n, ok)
	}
}
