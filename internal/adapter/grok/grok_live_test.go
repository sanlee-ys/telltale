//go:build live

package grok

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/hud"
	"github.com/sanlee-ys/telltale/internal/model"
)

// Live read of the REAL grok store on this machine, through the HUD's own scan
// and render path rather than through the adapter alone:
//
//	go test ./internal/adapter/grok -tags=live -run TestLiveGrokStore -v -count=1
//
// It exists because grok_test.go proves one half and cannot prove the other. The
// fixtures pin the PARSER against a tree this repo wrote, so they would still
// pass if `Discover` walked the wrong depth, if the real store's directory names
// were shaped differently, or if a field the survey found on 30 of 30 sessions
// turned out to be spelled another way on a session nobody sampled. This walks
// the store that is actually there.
//
// Excluded from CI by the build tag, for the reason the council's live test is:
// CI has no grok store, and a test that passes by skipping is a test that
// reports green for having done nothing.
//
// It is READ-ONLY and stays that way — it drives the production adapter, which
// opens three files per session and takes no lock. Nothing here writes to
// ~/.grok, and nothing here prints session content: the frame is rendered so the
// SHAPE can be read, and this repo is public, so the assertions below are about
// structure and the output is left for a human running it deliberately.
func TestLiveGrokStore(t *testing.T) {
	a := New()
	refs, err := a.Discover(context.Background())
	if err == model.ErrVendorAbsent {
		t.Skipf("no grok store at %s", a.Root())
	}
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	t.Logf("store %s: %d sessions discovered", a.Root(), len(refs))
	if len(refs) == 0 {
		t.Skip("grok is installed and has no sessions; nothing to verify")
	}

	var sourced, named, withCtx, withCost int
	for _, ref := range refs {
		s, err := a.Read(context.Background(), ref)
		if err != nil {
			t.Errorf("Read(%s): %v", ref.ID, err)
			continue
		}
		if err := s.Validate(a.Capabilities()); err != nil {
			t.Errorf("Validate(%s): %v", ref.ID, err)
		}
		if s.Cost != nil {
			t.Errorf("%s claims a session cost; the store holds no total", ref.ID)
		}
		if len(s.Quota) != 0 {
			t.Errorf("%s claims quota windows", ref.ID)
		}
		if s.Subagents != nil {
			t.Errorf("%s claims a sub-agent count", ref.ID)
		}
		if s.LivenessHint != nil {
			t.Errorf("%s claims liveness; active_sessions.json was measured empty during a live turn", ref.ID)
		}
		if s.Has(model.FieldModel) && s.Has(model.FieldWorkspace) {
			sourced++
		}
		if s.Has(model.FieldName) {
			named++
		}
		if s.Has(model.FieldContextPercent) {
			withCtx++
		}
		for _, e := range s.Extras {
			if e.Label == "turn cost" && strings.HasPrefix(e.Value, "$") {
				withCost++
			}
		}
	}
	t.Logf("%d/%d carry model+workspace, %d titled, %d with a context reading, %d with a turn cost",
		sourced, len(refs), named, withCtx, withCost)
	if sourced == 0 {
		t.Error("not one real session sourced both a model and a workspace; the field map has moved")
	}

	// The frame the HUD would draw for this vendor, at a fixed width so the
	// output is comparable between runs.
	st := hud.NewState()
	st.Now = time.Now()
	st.Width, st.Height = 120, 4+len(refs)
	st.Filter = hud.FilterGrok
	st.Snap = hud.Scan(context.Background(), []model.Adapter{New()}, st.Now)
	t.Logf("HUD frame:\n%s", hud.Render(st, hud.PlainStyles(), hud.GlyphsFor(false)))
}
