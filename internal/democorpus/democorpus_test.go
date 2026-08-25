package democorpus_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/democorpus"
	"github.com/sanlee-ys/telltale/internal/hud"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The corpus is only useful if the REAL adapters read it: these tests write it
// to a temp directory and run the HUD's own Scan over the rooted roster, so
// every assertion below is about what the demo will actually display.

const (
	claudeLive = "11111111-0000-4000-8000-000000000001"
	codexLive  = "22222222-0000-4000-8000-000000000001"
	grokIdle   = "33333333-0000-4000-8000-000000000002"
	piStale    = "44444444-0000-4000-8000-000000000001"
)

func scanCorpus(t *testing.T) (hud.Snapshot, []model.Adapter, time.Time) {
	t.Helper()
	dir := t.TempDir()
	now := time.Now()
	if err := democorpus.Write(dir, now); err != nil {
		t.Fatalf("Write: %v", err)
	}
	adapters := democorpus.Adapters(dir)
	snap := hud.Scan(context.Background(), adapters, now)
	return snap, adapters, now
}

func find(t *testing.T, snap hud.Snapshot, id string) *model.Session {
	t.Helper()
	for _, s := range snap.Sessions {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("session %s not in scan (%d sessions)", id, len(snap.Sessions))
	return nil
}

func TestCorpusScansCleanThroughTheRealAdapters(t *testing.T) {
	snap, adapters, _ := scanCorpus(t)

	if len(snap.Sessions) != 8 {
		t.Errorf("scan read %d sessions, want 8", len(snap.Sessions))
	}

	// Every populated vendor is watching and drift-free. A corpus that
	// reports drift on the projector would be the demo refuting itself.
	want := map[model.VendorID]int{
		model.VendorClaude: 3,
		model.VendorCodex:  2,
		model.VendorGrok:   2,
		model.VendorPi:     1,
	}
	for _, v := range snap.Vendors {
		n, populated := want[v.Vendor]
		if !populated {
			if v.Status != hud.StatusNotDetected {
				t.Errorf("%s: status %v, want not detected (no corpus data)", v.Vendor, v.Status)
			}
			continue
		}
		if v.Status != hud.StatusWatching {
			t.Errorf("%s: status %v (err %q), want watching", v.Vendor, v.Status, v.Err)
		}
		if v.Drifted != 0 {
			t.Errorf("%s: %d of %d sessions report drift, want none", v.Vendor, v.Drifted, v.Sessions)
		}
		if v.Sessions != n {
			t.Errorf("%s: read %d sessions, want %d", v.Vendor, v.Sessions, n)
		}
	}

	// Every session must satisfy the model contract against its adapter's
	// declared capabilities — the same check the eval harness runs.
	caps := map[model.VendorID]model.Capabilities{}
	for _, a := range adapters {
		caps[a.Vendor()] = a.Capabilities()
	}
	for _, s := range snap.Sessions {
		if err := s.Validate(caps[s.Vendor]); err != nil {
			t.Errorf("%s/%s: %v", s.Vendor, s.ID, err)
		}
		if len(s.Diagnostics) != 0 {
			t.Errorf("%s/%s: diagnostics %q, want none", s.Vendor, s.ID, s.Diagnostics)
		}
	}
}

// The public-repo boundary, in its checkable form: the corpus cannot name a
// private repository if every workspace it names starts under the invented
// prefix.
func TestEveryWorkspaceIsInvented(t *testing.T) {
	snap, _, _ := scanCorpus(t)
	for _, s := range snap.Sessions {
		if s.WorkspaceDir == nil {
			t.Errorf("%s/%s: no workspace", s.Vendor, s.ID)
			continue
		}
		if !strings.HasPrefix(*s.WorkspaceDir, democorpus.WorkspacePrefix) {
			t.Errorf("%s/%s: workspace %q is outside %q", s.Vendor, s.ID, *s.WorkspaceDir, democorpus.WorkspacePrefix)
		}
	}
}

// The demo's states: a session that streams (live), sessions that wait
// (idle), and stale rows — classified by the HUD's own shared thresholds.
func TestCorpusCarriesTheLivenessStates(t *testing.T) {
	snap, _, now := scanCorpus(t)
	th := model.DefaultLivenessThresholds

	counts := map[model.Liveness]int{}
	for _, s := range snap.Sessions {
		counts[s.Liveness(now, th)]++
	}
	if counts[model.LivenessLive] != 3 {
		t.Errorf("live sessions = %d, want 3", counts[model.LivenessLive])
	}
	if counts[model.LivenessIdle] != 3 {
		t.Errorf("idle sessions = %d, want 3", counts[model.LivenessIdle])
	}
	if counts[model.LivenessStale] != 2 {
		t.Errorf("stale sessions = %d, want 2", counts[model.LivenessStale])
	}
	if counts[model.LivenessUnknown] != 0 {
		t.Errorf("%d sessions have unknown liveness — an activity timestamp failed its read", counts[model.LivenessUnknown])
	}
}

// Zero versus absent, the distinction the repo exists to protect, present in
// the corpus itself: grok's idle session carries a vendor-written 0%% context
// reading, and claude's rows can never carry one (CapNone).
func TestCorpusKeepsZeroAndAbsentApart(t *testing.T) {
	snap, adapters, _ := scanCorpus(t)

	g := find(t, snap, grokIdle)
	if g.ContextPercent == nil {
		t.Fatal("grok idle session: context is absent, want a measured 0")
	}
	if *g.ContextPercent != 0 {
		t.Errorf("grok idle session: context = %v, want 0", *g.ContextPercent)
	}
	if g.Derived.Has(model.FieldContextPercent) {
		t.Error("grok context is marked derived, want reported (the vendor wrote the percentage)")
	}

	c := find(t, snap, claudeLive)
	if c.ContextPercent != nil {
		t.Errorf("claude session carries context %v, want none (CapNone)", *c.ContextPercent)
	}
	for _, a := range adapters {
		if a.Vendor() != model.VendorClaude {
			continue
		}
		if a.Capabilities().Known().Has(model.FieldContextPercent) {
			t.Error("claude adapter declares context_pct — the absent half of zero-vs-absent is gone")
		}
	}
}

// The honest-marker states: codex context is derived (renders with ~), its
// quota windows come from vendor rate limits, and claude's fan-out chip is a
// counted sidecar.
func TestCorpusCarriesTheMarkedValues(t *testing.T) {
	snap, _, _ := scanCorpus(t)

	x := find(t, snap, codexLive)
	if x.ContextPercent == nil {
		t.Fatal("codex live session: no context percentage")
	}
	if !x.Derived.Has(model.FieldContextPercent) {
		t.Error("codex context is not marked derived — the ~ marker would not render")
	}
	if len(x.Quota) != 2 {
		t.Fatalf("codex live session: %d quota windows, want 2", len(x.Quota))
	}
	labels := x.Quota[0].Label + " " + x.Quota[1].Label
	if labels != "5h 7d" {
		t.Errorf("codex quota labels = %q, want \"5h 7d\"", labels)
	}

	c := find(t, snap, claudeLive)
	if c.Subagents == nil || *c.Subagents != 2 {
		t.Errorf("claude live session: subagents = %v, want 2", c.Subagents)
	}

	p := find(t, snap, piStale)
	if p.Name == nil || *p.Name != "Rename the config keys" {
		t.Errorf("pi session name = %v, want the invented title", p.Name)
	}
}

// One rendered frame over the corpus, through the HUD's real Render: the rows
// draw, and nothing outside the invented fleet appears. This is the closest a
// test can get to looking at the projector.
func TestCorpusRendersAFrame(t *testing.T) {
	snap, _, now := scanCorpus(t)

	st := hud.NewState()
	st.Snap = snap
	st.Now = now
	st.Width, st.Height = 140, 30
	st.ShowAll = true

	out := hud.Render(st, hud.PlainStyles(), hud.UnicodeGlyphs())
	// A named session renders its title plus the workspace's parent
	// directory; only a nameless one (codex) falls back to the workspace
	// basename as its label. The wants below follow that, and together they
	// touch every vendor and both label forms.
	for _, want := range []string{
		"Wire the retry queue",
		"Chase the flaky login test",
		"Profile the cold start",
		"Sweep the TODO backlog",
		"Rename the config keys",
		"tidepool", "marmalade-api",
		`C:\demo\src`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("frame does not draw %q:\n%s", want, out)
		}
	}
}
