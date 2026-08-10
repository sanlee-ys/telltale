package council

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// stubNoNativeClipboard forces the OSC 52 fallback for one test and restores
// the real helper afterwards, so the escape-sequence path stays covered on
// machines that would otherwise never take it.
func stubNoNativeClipboard(t *testing.T) {
	t.Helper()
	orig := nativeClipboard
	nativeClipboard = func(string) bool { return false }
	t.Cleanup(func() { nativeClipboard = orig })
}

// TestNativeClipboardRoundTripsOnThisPlatform.
//
// The bug this exists for could not have been caught by a unit test of the old
// path, and that is the part worth keeping. OSC 52 has no acknowledgement, so a
// test could only ever assert that the sequence was CONSTRUCTED — and on macOS
// the sequence was constructed perfectly while the clipboard stayed untouched.
//
// This asserts the effect instead: text in, same text back out of the OS. It
// can, because the native helper is the checkable mechanism. Skipped rather than
// failed where no helper exists (Windows by design; a headless Linux box with
// neither wl-copy nor xclip): a machine that cannot run the check has not
// falsified anything, and a skip says that where a pass would lie about it.
func TestNativeClipboardRoundTripsOnThisPlatform(t *testing.T) {
	name, _ := clipboardHelper()
	if name == "" {
		t.Skipf("no native clipboard helper on %s — OSC 52 is the mechanism there", runtime.GOOS)
	}

	const want = "telltale clipboard probe — ALPHA\nsecond line"
	if !writeNativeClipboard(want) {
		t.Skipf("%s is not installed or refused to run; the OSC 52 fallback covers this machine", name)
	}

	var reader string
	var readerArgs []string
	switch runtime.GOOS {
	case "darwin":
		reader = "pbpaste"
	case "linux":
		reader, readerArgs = "wl-paste", []string{"--no-newline"}
	default:
		return
	}
	path, err := exec.LookPath(reader)
	if err != nil {
		t.Skipf("no %s to verify with", reader)
	}
	out, err := exec.Command(path, readerArgs...).Output()
	if err != nil {
		t.Skipf("%s failed: %v", reader, err)
	}
	if strings.TrimSpace(string(out)) != strings.TrimSpace(want) {
		t.Errorf("clipboard holds %q, want %q", string(out), want)
	}
}

// TestEmptyYankNeverReachesTheClipboard pins the rule the empty case has always
// had, now on the second mechanism as well: writing an empty string is the
// documented way to CLEAR a clipboard, so "there was nothing to copy" must never
// be spelled the same way as "your clipboard is now empty".
func TestEmptyYankNeverReachesTheClipboard(t *testing.T) {
	for _, empty := range []string{"", "   ", "\n\t "} {
		if writeNativeClipboard(empty) {
			t.Errorf("an empty yank (%q) reached the clipboard and would have cleared it", empty)
		}
	}
}
