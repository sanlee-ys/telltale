package council

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/councilhost"
)

// The stubbed end-to-end of design.md §7.31: this test binary re-executed as a
// REAL councilhost.Host over a real pipe, with an EMPTY roster, and the room's
// own Model driven over the client half of that pipe.
//
// # Why a re-executed process and not an in-process host
//
// councilhost.NewRoomJob assigns the CALLING process into a job carrying
// kill-on-close, so an in-process host would put `go test` itself in that job
// and the first shutdown would terminate the suite (councilhost's stubRoomJob
// says the same). A separate process is also what a detach is ABOUT: the claim
// is that the room outlives the client's process, and only a second process can
// be outlived.
//
// # Why councilhost.Join is called DIRECTLY, past the guarded var
//
// main_test.go wraps joinHostedRoom to panic when the pipe answers, because a
// live room is a turn away from somebody's quota. This host has an empty
// roster, so no path in it can spawn a vendor, and the guard cannot tell that
// from a pipe. Calling the package function past the var is the sanctioned way
// through — arenacheck_test.go does the same with runCheck — and it is done
// here, once, in a file whose name says what it is.

const (
	hostedHelperEnv     = "TELLTALE_COUNCIL_TEST_ROLE"
	hostedHelperRole    = "hosted-host"
	hostedHelperPipeEnv = "TELLTALE_COUNCIL_TEST_PIPE"
	hostedHelperWorkEnv = "TELLTALE_COUNCIL_TEST_WORKSPACE"
)

// runHostedHelper is the stand-in host. It runs before TestMain arms the guard
// and before any test runs, because it is not a test.
func runHostedHelper() (int, bool) {
	if os.Getenv(hostedHelperEnv) != hostedHelperRole {
		return 0, false
	}
	h, err := councilhost.New(councilhost.Config{
		Workspace: os.Getenv(hostedHelperWorkEnv),
		PipeName:  os.Getenv(hostedHelperPipeEnv),
		Posture:   vendors.PostureRead,
		Tick:      5 * time.Millisecond,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "hosted helper:", err)
		return 1, true
	}
	if err := h.Serve(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "hosted helper:", err)
		return 1, true
	}
	return 0, true
}

// TestAHostedRoomDrawsWithItsOwnColumnsAndIsLeftFromThere is the whole of
// §7.31 over a real pipe: the first frame builds a room the TUI draws as a
// hosted one, `/detach` typed into the composer leaves it with the host still
// answering, a second client rejoins and receives the room again, and ending
// that one ends the host.
func TestAHostedRoomDrawsWithItsOwnColumnsAndIsLeftFromThere(t *testing.T) {
	name := councilhost.PipeName(fmt.Sprintf("council-hosted-e2e-%d", os.Getpid()))
	host := exec.Command(os.Args[0])
	host.Env = append(os.Environ(),
		hostedHelperEnv+"="+hostedHelperRole,
		hostedHelperPipeEnv+"="+name,
		hostedHelperWorkEnv+"="+t.TempDir(),
	)
	host.Stderr = os.Stderr
	if err := host.Start(); err != nil {
		t.Fatalf("could not start the stand-in host: %v", err)
	}
	t.Cleanup(func() {
		_ = host.Process.Kill()
		_, _ = host.Process.Wait()
	})
	deadline := time.Now().Add(20 * time.Second)
	for {
		st, err := councilhost.ProbePipe(name)
		if err == nil && st != councilhost.PipeAbsent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the stand-in host never opened its pipe")
		}
		time.Sleep(10 * time.Millisecond)
	}

	c, err := councilhost.Join(councilhost.JoinConfig{PipeName: name, DialTimeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("could not join the stand-in host: %v", err)
	}
	first, err := c.Next()
	if err != nil {
		t.Fatalf("the host sent no first room: %v", err)
	}
	if first.Posture != "read" {
		t.Fatalf("the first frame is not the read room the helper opened: %+v", first)
	}

	m := newHostedModel(Options{}, first, c, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	frame := render(m.st)
	if !strings.Contains(frame, "council") || !strings.Contains(frame, "hosted") {
		t.Fatalf("the hosted room did not draw as the room's own frame:\n%s", frame)
	}
	if !strings.Contains(frame, "hosted pid "+itoa(c.HostPID())) {
		t.Fatalf("the border does not name the host:\n%s", frame)
	}
	if m.st.Hosted.PID != c.HostPID() || m.st.Hosted.PID != host.Process.Pid {
		t.Fatalf("State names pid %d; the client says %d and the process is %d",
			m.st.Hosted.PID, c.HostPID(), host.Process.Pid)
	}

	// /detach, typed into the composer. The answer arrives on the wire and is
	// folded where every frame is.
	m.setDraft("/detach")
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(m.st.Notice, "asking the host") {
		t.Fatalf("the detach was not asked for: notice=%q", m.st.Notice)
	}
	var quit tea.Cmd
	for i := 0; i < 50 && quit == nil; i++ {
		msg, ok := m.waitHost()().(hostFrameMsg)
		if !ok {
			t.Fatal("waitHost delivered something that is not a frame")
		}
		if msg.err != nil {
			t.Fatalf("the host went away during the detach: %v", msg.err)
		}
		quit = m.applyHostFrame(msg)
	}
	if quit == nil || m.hosted.outcome != councilhost.OutcomeDetached {
		t.Fatalf("the detach did not end the client: outcome=%v", m.hosted.outcome)
	}
	if _, isQuit := quit().(tea.QuitMsg); !isQuit {
		t.Fatal("the detach answer did not quit the program")
	}

	// The host is still there, with nobody in it. Polled, because the host
	// re-arms its listener AFTER it sees the client's close (Serve's Rearm),
	// and a probe that ran in that window would read a room that is about
	// to answer as one that is gone.
	deadline = time.Now().Add(10 * time.Second)
	for {
		st, err := councilhost.ProbePipe(name)
		if err == nil && st == councilhost.PipeFree {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after the detach the host's pipe reads %v (%v); PipeFree was expected", st, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// A second client REJOINS and receives the whole room as its first frame.
	c2, err := councilhost.Join(councilhost.JoinConfig{PipeName: name, DialTimeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("the detached host could not be rejoined: %v", err)
	}
	again, err := c2.Next()
	if err != nil {
		t.Fatalf("the rejoined host sent no room: %v", err)
	}
	m2 := newHostedModel(Options{}, again, c2, councilhost.RejoinedNotice(councilhost.HostFile{PID: c2.HostPID()}))
	m2.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	if got := render(m2.st); !strings.Contains(got, "nothing was rebuilt") {
		t.Fatalf("the rejoined room does not say nothing was rebuilt:\n%s", got)
	}

	// q ends the room: the shutdown frame goes, Close waits for the host, and
	// the process exits.
	m2.st.Mode = ModeViewing
	if _, cmd := m2.key(tea.KeyPressMsg{Code: 'q', Text: "q"}); cmd == nil {
		t.Fatal("q in an idle hosted room did not quit")
	}
	if m2.hosted.outcome != councilhost.OutcomeEnded {
		t.Fatalf("q did not end the room: outcome=%v", m2.hosted.outcome)
	}
	done := make(chan error, 1)
	go func() { done <- host.Wait() }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the host did not exit after the room was ended")
	}
}
