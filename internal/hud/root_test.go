package hud

import (
	"strings"
	"testing"
)

// The substitute root (`--root`) is stated in the footer for the whole run.
// Every row on such a frame was read from a corpus rather than from this
// machine's stores, and nothing else on screen can say so — the rows
// themselves are indistinguishable from live ones, which is the point of
// reading the corpus through the real adapters.
func TestTheFooterStatesTheSubstituteRoot(t *testing.T) {
	st := NewState()
	st.Now = pinned
	st.Width, st.Height = 120, 14
	st.Snap = hiddenSnap()
	st.Root = `C:\demo\corpus`

	out := Render(st, PlainStyles(), UnicodeGlyphs())
	if !strings.Contains(out, `root C:\demo\corpus`) {
		t.Errorf("the footer does not state the substitute root:\n%s", out)
	}
}

// No root, no notice: the live scan is the default and defaults are not
// announced.
func TestNoRootNoticeOnALiveScan(t *testing.T) {
	st := NewState()
	st.Now = pinned
	st.Width, st.Height = 120, 14
	st.Snap = hiddenSnap()

	out := Render(st, PlainStyles(), UnicodeGlyphs())
	if strings.Contains(out, "root ") {
		t.Errorf("a live-scan footer carries a root notice:\n%s", out)
	}
}
