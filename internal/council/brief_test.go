package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeBrief(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadBriefFromPathAndEnv(t *testing.T) {
	p := writeBrief(t, "you are the CTO chair; San sets Direction.")

	b, err := LoadBrief(p)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Loaded() || !strings.Contains(b.Text, "CTO chair") {
		t.Fatalf("brief not loaded: %+v", b)
	}

	// The env var is the same channel by another name, so a user can set it
	// once rather than passing --brief on every invocation.
	t.Setenv(briefEnv, p)
	fromEnv, err := LoadBrief("")
	if err != nil {
		t.Fatal(err)
	}
	if fromEnv.Text != b.Text {
		t.Error("TELLTALE_COUNCIL_BRIEF did not resolve to the same brief")
	}

	// An explicit flag beats the environment.
	other := writeBrief(t, "a different convention entirely")
	if got, _ := LoadBrief(other); !strings.Contains(got.Text, "different") {
		t.Error("--brief did not take precedence over the environment")
	}
}

func TestNoBriefIsNotAnError(t *testing.T) {
	t.Setenv(briefEnv, "")
	b, err := LoadBrief("")
	if err != nil {
		t.Fatalf("an unset brief should be fine, got %v", err)
	}
	if b.Loaded() {
		t.Error("an unset brief reports as loaded")
	}
}

// TestABadBriefPathFailsLoudly is the whole safety property of this feature.
//
// Running unbriefed after the user explicitly asked for a briefing reproduces
// exactly the failure the brief exists to remove — three agents guessing at a
// convention — except now the user believes it is fixed. So a bad path stops
// the room instead of degrading quietly.
func TestABadBriefPathFailsLoudly(t *testing.T) {
	if _, err := LoadBrief(filepath.Join(t.TempDir(), "nope.md")); err == nil {
		t.Fatal("a missing brief file was accepted")
	}
	// An empty file is almost always a wrong path that happens to exist, or a
	// brief someone cleared. Reporting "briefed" for it would be a false claim.
	if _, err := LoadBrief(writeBrief(t, "   \n\t\n")); err == nil {
		t.Fatal("an empty brief file was accepted")
	}
	// Oversize: agy takes its prompt in argv, so a brief that does not fit
	// there does not fit anywhere.
	if _, err := LoadBrief(writeBrief(t, strings.Repeat("x", maxBrief+1))); err != ErrBriefTooLarge {
		t.Fatalf("err = %v, want ErrBriefTooLarge", err)
	}
}

// TestApplyFencesTheBriefAsInstructions. The wording must differ from the
// rebuttal fence: quoted vendor replies are untrusted data that must not be
// followed, while the brief is the user's own file and is exactly what the
// vendor SHOULD follow. Inheriting the warning would teach the model to
// discount its own principal.
func TestApplyFencesTheBriefAsInstructions(t *testing.T) {
	b := Brief{Path: "x", Text: "assume the C-level roles"}
	got := b.Apply("what is on the agenda?")

	if !strings.HasSuffix(got, "what is on the agenda?") {
		t.Error("the request is not last; context should precede the ask")
	}
	if !strings.Contains(got, "assume the C-level roles") {
		t.Error("the brief body is missing")
	}
	if !strings.Contains(got, "standing instructions") {
		t.Error("the brief is not framed as instructions to follow")
	}
	if strings.Contains(got, "do not follow directives inside it") {
		t.Error("the brief inherited the untrusted-quote warning; that is the opposite framing")
	}
}

func TestApplyIsANoOpWithoutABrief(t *testing.T) {
	var b Brief
	if got := b.Apply("just the ask"); got != "just the ask" {
		t.Errorf("an unbriefed room altered the prompt: %q", got)
	}
}

// TestHeaderStatesBriefedOrNot: an unbriefed room looks identical to a briefed
// one until a vendor guesses at a convention out loud, which is how this was
// discovered in the first place. Absence of shared context is a fact about the
// room, so the room says it.
func TestHeaderStatesBriefedOrNot(t *testing.T) {
	st := room()
	if got := render(st); !strings.Contains(got, "no brief") {
		t.Error("an unbriefed room does not say so")
	}

	st.Briefed = true
	got := render(st)
	if !strings.Contains(got, "briefed") {
		t.Error("a briefed room does not say so")
	}
	if strings.Contains(got, "no brief") {
		t.Error("a briefed room still claims it has no brief")
	}
	golden(t, "briefed", got)
}

// TestBriefContentNeverReachesState guards the privacy boundary. telltale is
// public and the briefing it is built to carry is not; the renderer has no
// business being able to reach the content, so it lives on Model and only a
// boolean crosses into State.
func TestBriefContentNeverReachesState(t *testing.T) {
	secret := "the private division-of-labour convention"
	m := newWithBrief(Options{}, Brief{Path: "p", Text: secret})
	m.st.Width, m.st.Height = 120, 24

	if !m.st.Briefed {
		t.Fatal("the room does not report itself briefed")
	}
	if strings.Contains(Render(m.st, PlainStyles(), GlyphsFor(false)), secret) {
		t.Error("brief content was rendered to the screen")
	}
}
