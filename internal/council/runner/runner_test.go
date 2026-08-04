package runner

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The helper-process pattern: the test binary re-executes ITSELF with a mode in
// the environment, and TestHelperProcess plays a scripted vendor.
//
// This is what lets every runner behaviour — streaming, a torn final line, a
// line larger than any buffer, a nonzero exit, a process tree that has to die —
// be tested without installing a vendor, without a terminal, and without
// spending a single token of real quota.
const helperEnv = "TELLTALE_COUNCIL_HELPER"

func helperSpec(t *testing.T, mode string, args ...string) Spec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	a := []string{"-test.run=TestHelperProcess", "--", mode}
	a = append(a, args...)
	os.Setenv(helperEnv, "1")
	return Spec{Vendor: model.VendorClaude, Binary: exe, Args: a}
}

// TestHelperProcess is not a real test. It is the scripted vendor.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		os.Exit(2)
	}

	switch args[0] {
	case "lines":
		for i := 0; i < 3; i++ {
			os.Stdout.WriteString(`{"t":"` + strconv.Itoa(i) + `"}` + "\n")
		}
		os.Exit(0)

	case "torn":
		// A final line with NO trailing newline. Dropping it would lose the
		// last thing a vendor said.
		os.Stdout.WriteString(`{"t":"a"}` + "\n" + `{"t":"final"}`)
		os.Exit(0)

	case "huge":
		// Larger than bufio.Scanner's 64K default token, which is why the
		// reader uses ReadBytes.
		os.Stdout.WriteString(`{"t":"` + strings.Repeat("x", 200_000) + `"}` + "\n")
		os.Exit(0)

	case "echo-stdin":
		b, _ := io.ReadAll(os.Stdin)
		// Marshalled, not concatenated: the point of this case is a prompt full
		// of quotes and ampersands, which would not survive string building.
		out, _ := json.Marshal(struct {
			T string `json:"t"`
		}{string(b)})
		os.Stdout.Write(append(out, '\n'))
		os.Exit(0)

	case "fail-auth":
		os.Stderr.WriteString("Error: not logged in. Please run `codex login`.\n")
		os.Exit(1)

	case "fail-plain":
		os.Stderr.WriteString("something broke\n")
		os.Exit(3)

	case "spawn-grandchild":
		// Spawns a grandchild that keeps appending to a file, then blocks. The
		// tree-kill test asserts the GRANDCHILD stops growing the file — the
		// case a plain cmd.Process.Kill() would miss, and the one that matters
		// because codex is reached through an npm shim.
		path := args[1]
		exe, _ := os.Executable()
		child := exec.Command(exe, "-test.run=TestHelperProcess", "--", "tick", path)
		child.Env = append(os.Environ(), helperEnv+"=1")
		_ = child.Start()
		os.Stdout.WriteString(`{"t":"spawned"}` + "\n")
		block()

	case "tick":
		path := args[1]
		for {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err == nil {
				f.WriteString("x")
				f.Close()
			}
			time.Sleep(20 * time.Millisecond)
		}

	case "sleep":
		block()
	}
	os.Exit(0)
}

// block parks the helper until something kills it.
//
// NOT `select {}`: Go's runtime detects that as an all-goroutines-asleep
// deadlock and panics with exit 2, so the child would die on its own before
// the test ever got to kill it — and the test would pass for the wrong reason.
func block() {
	for {
		time.Sleep(time.Second)
	}
}

// parseT is a minimal parser over the helper's {"t":"..."} lines.
func parseT(line []byte) (Event, bool) {
	var v struct {
		T string `json:"t"`
	}
	if err := json.Unmarshal(line, &v); err != nil {
		return Event{}, false
	}
	return Event{Kind: KindText, Text: v.T}, true
}

// collect drains events until a terminal one arrives.
func collect(t *testing.T, ch <-chan Event) (texts []string, last Event) {
	t.Helper()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case ev := <-ch:
			switch ev.Kind {
			case KindText:
				texts = append(texts, ev.Text)
			case KindDone, KindError:
				return texts, ev
			}
		case <-deadline:
			t.Fatal("timed out waiting for a terminal event")
		}
	}
}

func TestStreamsLinesAsEvents(t *testing.T) {
	ch := make(chan Event, 16)
	if _, err := Start(context.Background(), helperSpec(t, "lines"), ch, parseT); err != nil {
		t.Fatal(err)
	}
	texts, last := collect(t, ch)
	if got := strings.Join(texts, ","); got != "0,1,2" {
		t.Errorf("texts = %q, want 0,1,2", got)
	}
	if last.Kind != KindDone {
		t.Errorf("terminal event = %v, want KindDone (note %q)", last.Kind, last.Note)
	}
}

// TestFinalLineWithoutNewlineIsNotLost: a clean exit whose last line has no
// terminator still said something, and a reader that requires '\n' would eat it.
func TestFinalLineWithoutNewlineIsNotLost(t *testing.T) {
	ch := make(chan Event, 16)
	if _, err := Start(context.Background(), helperSpec(t, "torn"), ch, parseT); err != nil {
		t.Fatal(err)
	}
	texts, _ := collect(t, ch)
	if len(texts) != 2 || texts[1] != "final" {
		t.Errorf("texts = %v, want the torn final line kept", texts)
	}
}

// TestLineLargerThanScannerDefault is why the reader is ReadBytes and not
// bufio.Scanner: Scanner caps a token at 64K, so the largest replies — exactly
// the ones worth reading — would vanish.
func TestLineLargerThanScannerDefault(t *testing.T) {
	ch := make(chan Event, 16)
	if _, err := Start(context.Background(), helperSpec(t, "huge"), ch, parseT); err != nil {
		t.Fatal(err)
	}
	texts, _ := collect(t, ch)
	if len(texts) != 1 || len(texts[0]) != 200_000 {
		t.Errorf("got %d events, first is %d bytes; want one 200000-byte line",
			len(texts), len(texts[0]))
	}
}

func TestPromptIsDeliveredOnStdin(t *testing.T) {
	spec := helperSpec(t, "echo-stdin")
	spec.StdinPrompt = "a brief with \"quotes\" & ampersands"
	ch := make(chan Event, 16)
	if _, err := Start(context.Background(), spec, ch, parseT); err != nil {
		t.Fatal(err)
	}
	texts, _ := collect(t, ch)
	if len(texts) == 0 || !strings.Contains(texts[0], "ampersands") {
		t.Errorf("stdin prompt did not reach the child: %v", texts)
	}
}

// TestShimWithArgvPromptIsRefused is the Windows safety rule as a hard failure.
// Go runs .cmd through cmd.exe, whose quoting cannot be made safe for arbitrary
// prompt text, so the runner refuses rather than hoping.
func TestShimWithArgvPromptIsRefused(t *testing.T) {
	spec := Spec{Vendor: model.VendorCodex, Binary: `C:\npm\codex.cmd`, Args: []string{"exec", "a prompt"}}
	ch := make(chan Event, 1)
	_, err := Start(context.Background(), spec, ch, parseT)
	if err != ErrShellShimWithArgvPrompt {
		t.Fatalf("err = %v, want ErrShellShimWithArgvPrompt", err)
	}
}

func TestShimWithStdinPromptIsAllowed(t *testing.T) {
	// Same shim, prompt on stdin: only fixed, metacharacter-free flags cross
	// cmd.exe, so this is the shape that makes Codex drivable at all.
	spec := Spec{
		Vendor:      model.VendorCodex,
		Binary:      `C:\npm\codex.cmd`,
		Args:        []string{"exec", "-"},
		StdinPrompt: "a prompt",
	}
	ch := make(chan Event, 1)
	_, err := Start(context.Background(), spec, ch, parseT)
	if err == ErrShellShimWithArgvPrompt {
		t.Fatal("a shim with a stdin prompt was refused; that is the supported shape")
	}
	// It will fail to start (the path does not exist), and that is fine — the
	// assertion is about WHICH error.
}

// TestAuthFailureIsTranslated: the two failures a user can act on get words
// they can act on. Everything else quotes the vendor rather than guessing.
func TestAuthFailureIsTranslated(t *testing.T) {
	ch := make(chan Event, 16)
	if _, err := Start(context.Background(), helperSpec(t, "fail-auth"), ch, parseT); err != nil {
		t.Fatal(err)
	}
	_, last := collect(t, ch)
	if last.Kind != KindError {
		t.Fatalf("kind = %v, want KindError", last.Kind)
	}
	if !strings.Contains(last.Note, "not signed in") {
		t.Errorf("note = %q, want the actionable auth message", last.Note)
	}
	if last.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1", last.ExitCode)
	}
}

func TestUnclassifiedFailureQuotesTheVendor(t *testing.T) {
	ch := make(chan Event, 16)
	if _, err := Start(context.Background(), helperSpec(t, "fail-plain"), ch, parseT); err != nil {
		t.Fatal(err)
	}
	_, last := collect(t, ch)
	if !strings.Contains(last.Note, "something broke") {
		t.Errorf("note = %q, want the vendor's own stderr quoted", last.Note)
	}
}

// TestKillReapsTheWholeTree is the guarantee that matters most on Windows.
//
// codex is reached through an npm .cmd shim, so the process telltale starts is
// cmd.exe and the real agent is a grandchild. Killing only the direct child
// would leave an agent running, holding a session and spending quota, with
// nothing on screen to say so.
func TestKillReapsTheWholeTree(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ticks")
	ch := make(chan Event, 64)
	h, err := Start(context.Background(), helperSpec(t, "spawn-grandchild", marker), ch, parseT)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the grandchild to be demonstrably alive.
	deadline := time.Now().Add(15 * time.Second)
	for {
		if fi, err := os.Stat(marker); err == nil && fi.Size() > 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the grandchild never started writing")
		}
		time.Sleep(20 * time.Millisecond)
	}

	h.Kill()
	// Give the kill time to propagate, then take a baseline and check the file
	// has genuinely stopped growing.
	time.Sleep(500 * time.Millisecond)
	before := size(t, marker)
	time.Sleep(750 * time.Millisecond)
	if after := size(t, marker); after != before {
		t.Fatalf("the grandchild is still running: marker grew %d -> %d bytes after Kill",
			before, after)
	}
}

// TestContextCancellationKillsTheChild: cancelling a turn must stop the work,
// not merely stop listening to it.
func TestContextCancellationKillsTheChild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan Event, 16)
	h, err := Start(ctx, helperSpec(t, "sleep"), ch, parseT)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-h.Done():
	case <-time.After(20 * time.Second):
		t.Fatal("the child outlived its context")
	}
}

// TestKilledTurnIsNotReportedAsAFailure: a process the user cancelled did not
// fail, and blaming the vendor for a keystroke would be a false claim on screen.
func TestKilledTurnIsNotReportedAsAFailure(t *testing.T) {
	ch := make(chan Event, 16)
	h, err := Start(context.Background(), helperSpec(t, "sleep"), ch, parseT)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	h.Kill()
	_, last := collect(t, ch)
	if last.Kind != KindDone {
		t.Errorf("kind = %v (note %q), want KindDone: the user cancelled, the vendor did not fail",
			last.Kind, last.Note)
	}
}

func size(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}
