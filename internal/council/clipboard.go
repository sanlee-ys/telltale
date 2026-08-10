package council

import (
	"os/exec"
	"runtime"
	"strings"
)

// The clipboard has two mechanisms, and the reason there are two is that the
// first one cannot be checked.
//
// OSC 52 (bubbletea's tea.SetClipboard, §9.15) writes an escape sequence into
// the terminal and returns. There is no reply, no capability query, and nothing
// a program can read afterwards — so a terminal that ignores it looks exactly
// like one that honoured it. That limitation was stated when the key shipped
// and it was believed to be theoretical, because the reference box is Windows
// Terminal, which honours the sequence.
//
// MEASURED FALSE on macOS, 2026-08-10: `y` reported "copied …" and the
// clipboard was untouched, in the same build where the identical key works on
// the Windows box. Terminal.app does not implement OSC 52 clipboard writes at
// all, and iTerm2 ships the permission OFF ("Applications in terminal may
// access clipboard"). Nothing was broken in council; the honest gauge was
// reporting an action it had no way to observe, which is the one failure mode
// §4a.1 exists to prevent — and it took a second machine to see it.
//
// So the native utility is tried FIRST wherever one exists. It is not the
// fancier option, it is the CHECKABLE one: pbcopy exits non-zero if it fails,
// which is more than the escape sequence can ever say about itself. OSC 52
// stays as the fallback, because it is the only mechanism that works over SSH
// and in terminals with no local helper.
//
// No new dependency, no daemon, no disk. The clipboard never touches a file:
// the text goes to the helper's stdin and dies with the process. That matters
// because §9.15 already declined a file-based fallback for exactly the reason a
// file would persist somebody's private reply in a location they never chose.

// clipboardHelper names the platform's clipboard-writing utility and its args.
//
// Windows is deliberately absent even though clip.exe exists: OSC 52 is
// MEASURED working there across every build this project runs on, and adding a
// second mechanism to the one platform that does not need it would trade a
// known-good path for a process spawn per keystroke.
func clipboardHelper() (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "pbcopy", nil
	case "linux":
		// Wayland first: on a Wayland session xclip either fails or talks to an
		// XWayland clipboard the compositor's own apps cannot read, which is a
		// silent wrong answer rather than a loud one.
		if path, err := exec.LookPath("wl-copy"); err == nil {
			return path, nil
		}
		return "xclip", []string{"-selection", "clipboard"}
	}
	return "", nil
}

// nativeClipboard is the seam the OSC 52 tests reach for.
//
// A var for the same reason the three spawn vars in this package are vars: the
// fallback path is real — it is what every SSH session and every Windows box
// uses — and a suite that could only exercise whichever mechanism the machine
// running it happens to have would leave the other one covered on one platform
// and untested on the rest. Stub it false to drive the escape-sequence path.
var nativeClipboard = writeNativeClipboard

// writeNativeClipboard puts text on the clipboard through the platform helper.
//
// Reports whether it actually ran and succeeded, which is the whole point of
// preferring it — the caller can then decide between claiming the copy happened
// and falling back to a mechanism that can only claim it was attempted.
func writeNativeClipboard(text string) bool {
	name, args := clipboardHelper()
	if name == "" || strings.TrimSpace(text) == "" {
		return false
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return false
	}
	// Plain argv, never a shell — §9.3's rule covers every process council
	// starts, and the text here is a vendor's reply: arbitrary bytes, quotes and
	// newlines included. It goes on stdin so none of it can reach a command line.
	cmd := exec.Command(path, args...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run() == nil
}
