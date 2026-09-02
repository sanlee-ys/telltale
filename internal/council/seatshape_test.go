package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The three seats that moved to long-lived processes on 2026-09-02 (design.md
// §9.54), and what their badges may and may not say about it.
//
// None of the three shapes has been driven from this repository: each was
// built from vendor documentation at a named version, and the badge is the
// place a reader learns that before trusting a column. So the property pinned
// here is the WORD. "unmeasured" is in every live badge by construction
// (seatShape), and the spelling that would let a future edit quietly promote a
// reading into a measurement — "measured at <the unmeasured version>" — is
// asserted absent.

// seatFallbacks seats every LiveFallback vendor's batch adapter for the life
// of one test: the room after a full retreat, and the only room that still
// has an ordinary one-shot seat in it.
//
// The give-up and re-send suites use it because their subject is the code
// path that kills a spawn-per-turn child, and since 2026-09-02 the default
// registry drives no seat that way. That path is still production — it is
// every arena racer and it is the fallback itself — so constructing the state
// is honest where scripting a refused handshake to reach it would be theatre.
func seatFallbacks(t *testing.T) {
	t.Helper()
	real := vendors.Registry
	vendors.Registry = vendors.FallbackRegistry
	t.Cleanup(func() { vendors.Registry = real })
}

// liveSeatRows is the table every assertion below walks: the seat, the
// version its live shape was READ at, and the version its fallback was
// MEASURED at. A row is a claim about detect.go's seatShape and nothing else.
var liveSeatRows = []struct {
	v        model.VendorID
	live     string // the first word of the live badge: the protocol
	readAt   string // the version the live shape was read from docs at
	fallback string // the first word of the fallback badge: the batch invocation
	measured string // the version the fallback was measured at
	asks     bool   // whether the live shape carries an approval channel
}{
	{model.VendorCodex, "app-server", "0.152.1", "exec", "0.149.1", true},
	{model.VendorGrok, "acp", "1.0.13", "single", "1.0.4", true},
	{model.VendorAntigravity, "stream-json", "1.1.24", "print", "1.1.13", false},
}

func TestLiveSeatBadgesSayUnmeasuredAndNameTheVersion(t *testing.T) {
	for _, row := range liveSeatRows {
		shape := seatShape(row.v, false)
		if !strings.HasPrefix(shape, row.live+" · ") {
			t.Errorf("%s: live badge %q does not open with the protocol %q", row.v, shape, row.live)
		}
		if !strings.HasSuffix(shape, "unmeasured at "+row.readAt) {
			t.Errorf("%s: live badge %q does not say unmeasured at %s", row.v, shape, row.readAt)
		}
		asks := strings.Contains(shape, " · asks · ")
		if asks != row.asks {
			t.Errorf("%s: live badge %q says asks=%v, want %v", row.v, shape, asks, row.asks)
		}
		if !row.asks && !strings.Contains(shape, " · unasked · ") {
			t.Errorf("%s: a seat with no approval channel must say so: %q", row.v, shape)
		}
		// The spelling a quiet promotion would use. A badge that read
		// "measured at 0.152.1" would be claiming a run nobody made.
		if strings.Contains(shape, " measured at "+row.readAt) {
			t.Errorf("%s: live badge claims a measurement at %s: %q", row.v, row.readAt, shape)
		}

		back := seatShape(row.v, true)
		if !strings.HasPrefix(back, row.fallback+" · unasked · fallback, measured at "+row.measured) {
			t.Errorf("%s: fallback badge = %q, want the measured batch invocation at %s", row.v, back, row.measured)
		}
		if strings.Contains(back, "unmeasured") {
			t.Errorf("%s: the fallback IS the measured seat and must not say unmeasured: %q", row.v, back)
		}
	}
	// The seats that did not move have no shape words, and a caller that asks
	// gets nothing rather than a guess.
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorCursor} {
		if got := seatShape(v, false); got != "" {
			t.Errorf("%s has no live-shape badge yet returned %q", v, got)
		}
	}
}

// TestLiveSeatWriteDetailsOpenWithTheShapeAndStayWRITES: the write posture's
// argument lives in the detail, and the detail leads with the shape words so
// a reader who opens the posture page finds the evidence class before the
// prose. The LEVEL is unchanged — `gated` is a coverage measurement (canGate)
// and no live shape has one.
func TestLiveSeatWriteDetailsOpenWithTheShapeAndStayWRITES(t *testing.T) {
	for _, row := range liveSeatRows {
		for _, asking := range []bool{true, false} {
			claim := postureClaim(row.v, true, true, asking, asking)
			if claim.Level != SandboxWrite {
				t.Errorf("%s (asking=%v): level %v, want SandboxWrite — no live shape has a coverage measurement",
					row.v, asking, claim.Level)
			}
			if !strings.HasPrefix(claim.Detail, seatShape(row.v, false)+": ") {
				t.Errorf("%s (asking=%v): write detail does not open with its shape: %q", row.v, asking, claim.Detail)
			}
			if !strings.Contains(claim.Detail, "not from a live turn") {
				t.Errorf("%s (asking=%v): write detail does not say the shape is unwatched: %q", row.v, asking, claim.Detail)
			}
			if row.asks && !strings.Contains(claim.Detail, "falls back to") {
				t.Errorf("%s (asking=%v): write detail does not name the fallback: %q", row.v, asking, claim.Detail)
			}
			if row.asks && asking && !strings.Contains(claim.Detail, "approval card") {
				t.Errorf("%s: with asking on, the detail must say a request becomes a card: %q", row.v, claim.Detail)
			}
			if row.asks && !asking && !strings.Contains(claim.Detail, "without drawing a card") {
				t.Errorf("%s: with asking off, the detail must not promise cards: %q", row.v, claim.Detail)
			}
		}
		// The read posture leads with the same words: the shape is a fact
		// about the process, not about the posture.
		for _, win := range []bool{true, false} {
			d := postureClaim(row.v, win, false, false, false).Detail
			if !strings.HasPrefix(d, seatShape(row.v, false)+": ") {
				t.Errorf("%s (windows=%v): read detail does not open with its shape: %q", row.v, win, d)
			}
		}
	}
}

// TestTheGateStillBelongsToOneSeat: two more seats can now be ASKED — the
// codex app-server routes item/*/requestApproval through the room's card and
// the grok ACP seat routes session/request_permission the same way — and
// neither is `gated`, because canGate is a coverage measurement and no live
// run on either path has produced a request at all.
func TestTheGateStillBelongsToOneSeat(t *testing.T) {
	for id := range vendors.Registry() {
		if got := canGate(id); got != (id == model.VendorClaude) {
			t.Errorf("canGate(%s) = %v; a seat that can be asked is not a seat measured asking about everything", id, got)
		}
	}
}

// TestCodexAppServerReadBadgeIsRequestedOffWindows is the one badge the seat
// move LOWERED, pinned so it cannot drift back on the strength of the exec
// seat's measurement. Every app-server arm ran on Windows (§9.50); the macOS
// `ro:enforced` was `codex exec`'s, and a seat move re-measures rather than
// inherits.
func TestCodexAppServerReadBadgeIsRequestedOffWindows(t *testing.T) {
	unix := sandboxFor(model.VendorCodex, false)
	if unix.Level != SandboxRequested {
		t.Errorf("codex off Windows claims %v, want SandboxRequested — app-server has never been driven there", unix.Level)
	}
	if !strings.Contains(unix.Detail, "never through app-server") {
		t.Errorf("the off-Windows detail does not say which path was measured: %q", unix.Detail)
	}
	// Windows keeps the level, because the SAME 0.149.1 session measured the
	// sandbox on the app-server path — and the detail has to carry the sharper
	// liveness residual that path was measured having.
	win := sandboxFor(model.VendorCodex, true)
	if win.Level != SandboxEnforced {
		t.Errorf("codex on Windows claims %v, want SandboxEnforced — measured on the app-server path at 0.149.1", win.Level)
	}
	if !strings.Contains(win.Detail, "two of three read turns") {
		t.Errorf("the Windows detail hides the app-server liveness residual: %q", win.Detail)
	}
	if !strings.Contains(win.Detail, "0.152.1") {
		t.Errorf("the Windows detail does not name the installed, undriven build: %q", win.Detail)
	}
}

// TestFallbackRegistryIsTheBatchRoom: the registry a full retreat leaves has
// no live seat among the three that moved, and the two that did not move are
// untouched. liveSeat is the room's own test for "does this seat keep a
// process", so it is the predicate asserted rather than a type list.
func TestFallbackRegistryIsTheBatchRoom(t *testing.T) {
	back := vendors.FallbackRegistry()
	for _, row := range liveSeatRows {
		v, ok := back[row.v]
		if !ok {
			t.Fatalf("%s is missing from the fallback registry", row.v)
		}
		if liveSeat(v) {
			t.Errorf("%s is still a live seat after the fallback: %T", row.v, v)
		}
		if _, still := v.(vendors.LiveFallback); still {
			t.Errorf("%s's fallback names a further fallback: %T", row.v, v)
		}
	}
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorCursor} {
		if !liveSeat(back[v]) {
			t.Errorf("%s lost its live shape in the fallback registry: %T", v, back[v])
		}
	}
	// And the default registry drives all five live, which is the state the
	// fixture helper exists to step out of.
	for id, v := range vendors.Registry() {
		if !liveSeat(v) {
			t.Errorf("%s is not a live seat in the default registry: %T", id, v)
		}
	}
}
