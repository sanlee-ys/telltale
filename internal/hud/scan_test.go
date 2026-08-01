package hud

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// fakeAdapter is an injected vendor. It exists so the scan's error handling
// can be asserted without a filesystem: the three vendor states below are the
// three the empty screen has words for, and each has to come from a different
// error shape.
type fakeAdapter struct {
	vendor      model.VendorID
	root        string
	caps        model.Capabilities
	refs        []model.SessionRef
	discoverErr error
	readErr     map[string]error
	sessions    map[string]*model.Session
}

func (f *fakeAdapter) Vendor() model.VendorID           { return f.vendor }
func (f *fakeAdapter) Capabilities() model.Capabilities { return f.caps }
func (f *fakeAdapter) Root() string                     { return f.root }

func (f *fakeAdapter) Discover(context.Context) ([]model.SessionRef, error) {
	if f.discoverErr != nil {
		return nil, f.discoverErr
	}
	return f.refs, nil
}

func (f *fakeAdapter) Read(_ context.Context, ref model.SessionRef) (*model.Session, error) {
	if err := f.readErr[ref.ID]; err != nil {
		return nil, err
	}
	return f.sessions[ref.ID], nil
}

func fakeVendor(v model.VendorID, ids ...string) *fakeAdapter {
	f := &fakeAdapter{
		vendor:   v,
		root:     `C:\fixture\` + string(v),
		caps:     fullCaps,
		readErr:  map[string]error{},
		sessions: map[string]*model.Session{},
	}
	for _, id := range ids {
		f.refs = append(f.refs, model.SessionRef{Vendor: v, ID: id, Locator: "fixture:" + id})
		f.sessions[id] = sess(v, id, `C:\src\code\`+id, "claude-opus-5", time.Second)
	}
	return f
}

func TestScanCollectsEveryVendorInParallel(t *testing.T) {
	snap := Scan(context.Background(), []model.Adapter{
		fakeVendor(model.VendorClaude, "a", "b"),
		fakeVendor(model.VendorCodex, "c"),
	}, pinned)

	if len(snap.Sessions) != 3 {
		t.Fatalf("collected %d sessions, want 3", len(snap.Sessions))
	}
	// Deterministic order in, deterministic frame out.
	want := []string{"claude/a", "claude/b", "codex/c"}
	for i, s := range snap.Sessions {
		if s.Key() != want[i] {
			t.Errorf("session %d = %q, want %q", i, s.Key(), want[i])
		}
	}
	for _, v := range snap.Vendors {
		if v.Status != StatusWatching {
			t.Errorf("vendor %s = %s, want watching", v.Vendor, v.Status)
		}
	}
}

// A vendor that is not installed disappears. A user without Codex should not
// stare at a Codex error forever.
func TestVendorAbsentBecomesNotDetected(t *testing.T) {
	missing := fakeVendor(model.VendorCodex)
	missing.discoverErr = model.ErrVendorAbsent

	snap := Scan(context.Background(), []model.Adapter{
		fakeVendor(model.VendorClaude, "a"),
		missing,
	}, pinned)

	if len(snap.Sessions) != 1 {
		t.Fatalf("collected %d sessions, want 1 — the other vendor still renders", len(snap.Sessions))
	}
	var codex VendorView
	for _, v := range snap.Vendors {
		if v.Vendor == model.VendorCodex {
			codex = v
		}
	}
	if codex.Status != StatusNotDetected {
		t.Errorf("codex status = %s, want not detected", codex.Status)
	}
	if codex.Err != "" {
		t.Errorf("not-detected must not carry an error message, got %q", codex.Err)
	}
}

// A directory that exists and the OS refuses is the third word, and it is the
// one that keeps the operating system's own message.
func TestUnreadableVendorKeepsTheOSMessage(t *testing.T) {
	refused := fakeVendor(model.VendorClaude)
	refused.discoverErr = errors.New("Access is denied.")

	snap := Scan(context.Background(), []model.Adapter{refused}, pinned)
	v := snap.Vendors[0]
	if v.Status != StatusUnreadable {
		t.Fatalf("status = %s, want unreadable", v.Status)
	}
	if v.Err != "Access is denied." {
		t.Errorf("err = %q, want the OS message verbatim", v.Err)
	}
}

// Showing less, not showing a banner: a row that cannot be read is a row that
// is not drawn, and every other row survives.
func TestUnreadableSessionDropsOnlyItsOwnRow(t *testing.T) {
	a := fakeVendor(model.VendorClaude, "ok", "gone", "broken")
	a.readErr["gone"] = model.ErrSessionGone
	a.readErr["broken"] = errors.New("unexpected end of JSON input")

	snap := Scan(context.Background(), []model.Adapter{a}, pinned)
	if len(snap.Sessions) != 1 || snap.Sessions[0].ID != "ok" {
		t.Fatalf("collected %v, want only the readable session", keys(snap.Sessions))
	}
	if snap.Err != "" {
		t.Errorf("a per-row failure must not become a scan failure: %q", snap.Err)
	}
}

func TestScanRootsAreCarriedForTheEmptyState(t *testing.T) {
	snap := Scan(context.Background(), []model.Adapter{fakeVendor(model.VendorClaude)}, pinned)
	if snap.Vendors[0].Root != `C:\fixture\claude` {
		t.Errorf("root = %q, want the adapter's watched directory", snap.Vendors[0].Root)
	}
}

// A completed scan that found nothing is a different state from a scan that
// never completed: only the second one is allowed to show a spinner.
func TestEmptyScanStillStampsAt(t *testing.T) {
	snap := Scan(context.Background(), nil, pinned)
	if snap.At.IsZero() {
		t.Fatal("a completed scan must stamp At even when it found nothing")
	}
	st := NewState()
	st.Now, st.Snap = pinned, snap
	st.Width, st.Height = 120, 10
	out := Render(st, PlainStyles(), UnicodeGlyphs())
	for _, frame := range UnicodeGlyphs().Spinner {
		if contains(out, frame) {
			t.Fatalf("a completed scan rendered a spinner frame %q", frame)
		}
	}
}

func keys(ss []*model.Session) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.Key())
	}
	return out
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}()
}
