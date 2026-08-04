package vendors

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// argAfter returns the value following flag, and whether it was present.
func argAfter(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

// TestSessionResumeMatchesTheVerifiedProbe pins the invocation that was run for
// real, because the composition it depends on is exactly the kind of thing this
// file has been wrong about three times.
//
// The sixth ADR-008 amendment states that the persistent session passes no
// --resume, "because there is nothing to resume". Reattaching a saved room is
// the case where there is — and whether the two flags compose was a genuinely
// open question, since one had only ever been used on the spawn-per-turn path
// and the other only on the persistent one. It was measured, not reasoned
// about: the process started, took a turn on stdin, answered from the PRIOR
// session's content, and reported the same session_id back.
func TestSessionResumeMatchesTheVerifiedProbe(t *testing.T) {
	spec, err := Claude{}.SessionResume("/ws", "claude", "sess-1", PostureRead)
	if err != nil {
		t.Fatal(err)
	}

	// Every flag the persistent session carries has to survive. A resume that
	// quietly dropped --input-format would spawn a batch process that read its
	// stdin once and closed it, which is the shape that cannot be gated at all.
	base, err := Claude{}.Session("/ws", "claude", PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range base.Args {
		found := false
		for _, got := range spec.Args {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("resume dropped the session flag %q", want)
		}
	}
	if v, ok := argAfter(spec.Args, "--input-format"); !ok || v != "stream-json" {
		t.Errorf("--input-format stream-json is missing: %v", spec.Args)
	}
	if v, ok := argAfter(spec.Args, "--resume"); !ok || v != "sess-1" {
		t.Errorf("--resume does not carry the session id: %v", spec.Args)
	}

	// No prompt anywhere. This is the property that makes the shim refusal moot
	// for a session and it must not be weakened by adding a resume.
	if spec.StdinPrompt != "" {
		t.Errorf("a session spec carries a prompt: %q", spec.StdinPrompt)
	}
	if strings.Contains(strings.Join(spec.Args, " "), "prompt") {
		t.Errorf("prompt text reached argv: %v", spec.Args)
	}
	if spec.Dir != "/ws" {
		t.Errorf("dir = %q, want the workspace", spec.Dir)
	}
}

// TestSessionResumeWithoutAnIdRefuses. Falling through to a plain Session would
// open a NEW conversation while the caller believed it had resumed one — the
// seat would answer normally with no history and nothing would say so.
func TestSessionResumeWithoutAnIdRefuses(t *testing.T) {
	if _, err := (Claude{}).SessionResume("/ws", "claude", "", PostureRead); err != ErrNoResume {
		t.Fatalf("err = %v, want ErrNoResume", err)
	}
}

// TestSessionResumeKeepsThePosture. The gate flags are what make --write ask
// first; a reattached seat that silently dropped them would act unasked in a
// room whose header still promised every call would be gated.
func TestSessionResumeKeepsThePosture(t *testing.T) {
	spec, err := Claude{}.SessionResume("/ws", "claude", "sess-1", PostureWriteGated)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(spec.Args, " ")
	for _, want := range gateArgs {
		if want != "" && !strings.Contains(joined, want) {
			t.Errorf("a resumed gated seat lost %q: %v", want, spec.Args)
		}
	}
	if strings.Contains(joined, "--disallowedTools") {
		t.Error("a write-posture resume still carries the read-only deny list")
	}
}

// TestOnlyClaudeCanResumeASession keeps the interface honest about the room's
// actual shape: three of the four vendors are batch programs with no persistent
// session to resume, and the compiler is what enforces that rather than a note.
func TestOnlyClaudeCanResumeASession(t *testing.T) {
	persistent := 0
	for id, v := range Registry() {
		p, ok := v.(Persistent)
		if !ok {
			continue
		}
		persistent++
		if id != model.VendorClaude {
			t.Errorf("%s implements Persistent; the ADR says only claude can", id)
		}
		if _, err := p.SessionResume("/ws", "bin", "", PostureRead); err != ErrNoResume {
			t.Errorf("%s does not refuse an empty resume id", id)
		}
	}
	if persistent != 1 {
		t.Errorf("%d persistent vendors, want exactly 1", persistent)
	}
}
