package councilhost

import (
	"strings"
	"testing"
	"time"
)

// noticeFixture is one host, described the same way for every notice, so the
// test below compares SENTENCES rather than the numbers in them.
func noticeFixture() HostFile {
	return HostFile{
		Version: HostFileVersion, PID: 4242, Pipe: PipeName("notice-fixture"),
		StartedAt: time.Date(2026, 9, 1, 19, 40, 12, 0, time.UTC),
		Workspace: `C:\Users\dev\code\telltale`, Turn: 3,
	}
}

// TestTheFourNoticesNeverRenderAlike is §4a.1 applied to a process, and it is
// the regression design.md §7.29 exists to prevent.
//
// §9.52 already rules that `rebuilt` and `survived` rendering alike would be the
// most expensive lie this surface can tell, because an operator would trust a
// history no process holds. Detach adds a fourth state — a refusal — and a
// fifth surface a client can be on, and every one of them has to be its own
// sentence.
//
// It compares the notices AS TEXT rather than checking each for a keyword. A
// keyword check passes on two notices that share every other word, and sharing
// every other word is exactly how two states start to read alike.
func TestTheFourNoticesNeverRenderAlike(t *testing.T) {
	f := noticeFixture()
	notices := map[string]string{
		"detached": RenderDetached(f.PID),
		"rejoined": RenderRejoined(f),
		// The TUI's one-line form (§7.31). It is the same fact as the line
		// above and must still be its own sentence beside every other state.
		"rejoined-line": RejoinedNotice(f),
		"died":          RenderHostDied(f, "~/.telltale/council/room.json"),
		"refused":       RenderDetachRefused(),
		"busy":          RenderHostBusy(f),
		"exited":        RenderHostExit(),
	}
	seen := map[string]string{}
	for name, text := range notices {
		if text == "" {
			t.Errorf("the %s notice is empty", name)
			continue
		}
		if other, dup := seen[text]; dup {
			t.Errorf("the %s notice and the %s notice render identically:\n%s", name, other, text)
		}
		seen[text] = name
	}
}

// TestTheRejoinNoticeSaysNothingWasRebuilt is the one clause that has to be
// there, and it is the whole difference between this state and §9.52's.
//
// §9.52's `rebuilt` sentence says a NEW process was launched on a saved id. This
// one says a process was never restarted at all. An operator who read a rebuild
// as a survival would trust a conversation no process holds — §9.52 calls that
// the most expensive lie this surface can tell — so the sentence that IS a
// survival has to say the opposite thing out loud.
func TestTheRejoinNoticeSaysNothingWasRebuilt(t *testing.T) {
	out := RenderRejoined(noticeFixture())
	if !strings.Contains(out, "nothing was rebuilt") {
		t.Errorf("the rejoin notice does not deny a rebuild:\n%s", out)
	}
	if !strings.Contains(out, "no session was resumed") {
		t.Errorf("the rejoin notice does not deny a resume:\n%s", out)
	}
	if !strings.Contains(out, "kept working while you were away") {
		t.Errorf("the rejoin notice does not say what survived:\n%s", out)
	}
	// The words §9.52 gave to the OTHER state must not appear here. `rebuilt`
	// does, inside "nothing was rebuilt", which is the denial — so the check is
	// on the claim, not the word.
	for _, forbidden := range []string{"seats rebuilt", "came back", "restored"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("the rejoin notice borrows a rebuild's words (%q):\n%s", forbidden, out)
		}
	}

	// The one-line form the TUI's notice row carries (§7.31) keeps the denial
	// and the pid, and fits a 120-column room beside the warning mark: the
	// notice line truncates from the right, and a denial that fell off the
	// end would leave a rejoin reading as a rebuild.
	line := RejoinedNotice(noticeFixture())
	if strings.Contains(line, "\n") {
		t.Errorf("the one-line rejoin notice has a newline in it: %q", line)
	}
	for _, want := range []string{"nothing was rebuilt", "no session was resumed", "pid 4242"} {
		if !strings.Contains(line, want) {
			t.Errorf("the one-line rejoin notice lacks %q:\n%s", want, line)
		}
	}
	if n := len([]rune(line)); n > 110 {
		t.Errorf("the one-line rejoin notice is %d cells; a 120-column room clips it", n)
	}
}

// TestTheDiedNoticeNamesTheProcessAndTheWayForward.
//
// §7.28's first crash mitigation: the operator cannot SEE a host die, because it
// has no terminal, so the client is the only thing that can say it happened. A
// notice that said only "gone" would leave them with no pid to look for and no
// idea their session ids survived.
func TestTheDiedNoticeNamesTheProcessAndTheWayForward(t *testing.T) {
	out := RenderHostDied(noticeFixture(), "~/.telltale/council/room.json")
	if !strings.Contains(out, "pid 4242") {
		t.Errorf("the died notice does not name the process:\n%s", out)
	}
	if !strings.Contains(out, "the seats went with it") {
		t.Errorf("the died notice does not say the seats died too:\n%s", out)
	}
	if !strings.Contains(out, "room.json") {
		t.Errorf("the died notice does not say what survived on disk:\n%s", out)
	}
	if !strings.Contains(out, "rebuilds those seats") {
		t.Errorf("the died notice does not point at §9.52's rebuild, so an operator would "+
			"read a recoverable conversation as a lost one:\n%s", out)
	}
}

// TestNoNoticeCarriesAnEscape.
//
// This whole surface is words and no colour, on doctor's and history's
// precedent, and Render's own doc says so. A notice with an escape in it would
// read as garbage in a pipe and in a pasted issue.
func TestNoNoticeCarriesAnEscape(t *testing.T) {
	f := noticeFixture()
	for _, text := range []string{
		RenderDetached(f.PID), RenderRejoined(f),
		RenderHostDied(f, "room.json"), RenderDetachRefused(),
		RenderHostBusy(f), RenderHostExit(), clientBanner(f.PID),
	} {
		if strings.ContainsRune(text, '\x1b') {
			t.Errorf("a notice carries an ANSI escape: %q", text)
		}
	}
}
