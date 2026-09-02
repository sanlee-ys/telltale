package council

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// TestDoctorAsksTheCursorSeatForItsOwnVersionNotNodes is the one place a
// preflight could print a perfectly real number that belongs to a different
// program.
//
// Detection resolves this seat to the bundled node.exe its .cmd launcher would
// have run — that is the whole unlock (§9.33) — so `--version` alone answers
// node's version. Measured on the reference box, 2026-08-09, one install, two
// invocations: `node.exe --version` → v24.5.0; `node.exe index.js --version` →
// 2026.08.04-aaa8809. Only the second is a fact about cursor-agent, and a row
// labelled `cursor` may only carry the second.
//
// The bundle path comes from vendors.CursorNodeBundle rather than from a
// filepath.Join here, for that function's own reason: two copies of one join is
// the agreement that silently stops holding.
func TestDoctorAsksTheCursorSeatForItsOwnVersionNotNodes(t *testing.T) {
	root, shim := fakeCursorInstall(t, "2026.08.04-aaa8809")
	info := detectCursorAt(t, shim)
	if info.Avail != AvailInstalled {
		t.Fatalf("the fake install did not resolve: %v (%s)", info.Avail, info.Note)
	}

	args := versionArgs(info)
	if len(args) != 2 {
		t.Fatalf("argv = %v; the bundle must come before the flag, or node answers instead", args)
	}
	want := filepath.Join(root, "versions", "2026.08.04-aaa8809", "index.js")
	if args[0] != want {
		t.Errorf("argv[0] = %q, want the bundle %q", args[0], want)
	}
	if args[1] != "--version" {
		t.Errorf("argv[1] = %q, want --version", args[1])
	}
	if args[0] != vendors.CursorNodeBundle(info.Binary) {
		t.Error("the bundle path was derived here instead of through vendors.CursorNodeBundle")
	}
}

// TestEveryOtherSeatIsAskedPlainly. All four take a bare --version, verified by
// running each of them (2026-08-09, reference box): claude "2.1.226 (Claude
// Code)", codex "codex-cli 0.147.0", agy "1.1.11", grok "grok 1.0.0
// (3cd0d0cbce) [stable]". Asserted so a future seat cannot silently inherit
// cursor's special case, or lose it.
func TestEveryOtherSeatIsAskedPlainly(t *testing.T) {
	for _, v := range []model.VendorID{
		model.VendorClaude, model.VendorCodex, model.VendorAntigravity, model.VendorGrok,
	} {
		args := versionArgs(VendorInfo{Vendor: v, Binary: `C:\fake\x.exe`})
		if len(args) != 1 || args[0] != "--version" {
			t.Errorf("%s version argv = %v, want [--version]", v, args)
		}
	}
}

// TestAnUnresolvedOverrideIsNotReportedAsFound. detectOne sets Binary to the
// override path and then finds nothing there, so `Binary != ""` is not the
// question — Avail is. A seat reported found on the strength of a path someone
// typed would be the preflight inventing a file.
func TestAnUnresolvedOverrideIsNotReportedAsFound(t *testing.T) {
	noVendorOverrides(t)
	missing := filepath.Join(t.TempDir(), "not-here.exe")
	t.Setenv("TELLTALE_COUNCIL_CLAUDE_BIN", missing)
	t.Setenv("PATH", t.TempDir())

	var claude *seatFor
	for _, s := range DoctorSeats() {
		if s.Vendor == string(model.VendorClaude) {
			claude = &seatFor{s.Found, s.Note, s.Drivable, s.Capability}
		}
	}
	if claude == nil {
		t.Fatal("claude is missing from the preflight entirely")
	}
	if claude.found {
		t.Error("a binary that is not there was reported found")
	}
	if !strings.Contains(claude.note, "does not exist") {
		t.Errorf("the reason does not explain the override failed: %q", claude.note)
	}
	if claude.drivable {
		t.Error("a binary that is not there was reported drivable")
	}
}

type seatFor struct {
	found      bool
	note       string
	drivable   bool
	capability string
}

// TestEverySeatCarriesADeclaredCapability. "Installed" and "will stream to you"
// are different promises, and the seat a user is most likely to think is broken
// is the one that is working and silent until the end of the turn (§9.14). A
// blank line there sends them looking for a fault that is not one.
func TestEverySeatCarriesADeclaredCapability(t *testing.T) {
	noVendorOverrides(t)
	t.Setenv("PATH", t.TempDir())
	seats := DoctorSeats()
	if len(seats) == 0 {
		t.Fatal("the preflight lists no seats at all")
	}
	for _, s := range seats {
		if s.Capability == "" {
			t.Errorf("%s declares no capability", s.Vendor)
		}
		if s.Label == "" {
			t.Errorf("%s has no label", s.Vendor)
		}
		// Every seat, present or absent, owes a reason when it is not drivable.
		// A blank cell is the failure detect.go's Note field was added to stop.
		if !s.Drivable && s.Note == "" {
			t.Errorf("%s is not drivable and says nothing about why", s.Vendor)
		}
	}
}

// TestTheDeclaredCapabilityMatchesTheRoomsOwnMeasurements. The preflight must
// not become a second, drifting copy of what the badges say: these words come
// off granularityFor and canGate, which are the same functions the room's own
// chrome is drawn from.
func TestTheDeclaredCapabilityMatchesTheRoomsOwnMeasurements(t *testing.T) {
	reg := vendors.Registry()

	// The two seats measured to produce nothing until the turn ends must SAY
	// so. A column that goes quiet for 73 seconds is the case §9.14 exists for.
	for _, v := range []model.VendorID{model.VendorCodex, model.VendorAntigravity} {
		if got := declaredCapability(v, reg); !strings.Contains(got, "whole at the end") {
			t.Errorf("%s does not warn that its reply arrives whole: %q", v, got)
		}
	}
	// Only the seat that asks about everything may say it can be gated —
	// canGate's own ruling, which the Cursor seat is the live counterexample to.
	for v := range reg {
		says := strings.Contains(declaredCapability(v, reg), "ask first")
		if want := canGate(v); says != want {
			t.Errorf("%s claims gating = %v, want %v", v, says, want)
		}
	}
	// And the live-process distinction is read off the interface the seat
	// actually implements, not off a list that would go stale the next time a
	// vendor is re-founded on a new protocol (as cursor was, §9.36).
	if got := declaredCapability(model.VendorCursor, reg); !strings.Contains(got, "live process") {
		t.Errorf("the cursor seat does not report as a live process: %q", got)
	}
	// Since 2026-09-02 the grok seat is a live ACP process too (§9.54), and
	// its batch shape is the fallback: the declaration is read off the
	// registry the room is running, so the same function says "batch" for
	// the fallback room and "live" for the default one.
	if got := declaredCapability(model.VendorGrok, reg); !strings.Contains(got, "live process") {
		t.Errorf("grok is a live ACP seat and does not say so: %q", got)
	}
	if got := declaredCapability(model.VendorGrok, vendors.FallbackRegistry()); !strings.Contains(got, "batch") {
		t.Errorf("grok's fallback is a batch program per turn and does not say so: %q", got)
	}
}

// TestThePreflightPostureIsTheRoomsOwnBadge is the "one source, two surfaces"
// assertion, and it is deliberately made through a DIFFERENT construction path
// on each side: DoctorSeats on one, the room's own stateWith on the other.
//
// Comparing doctorPosture against postureClaim would be comparing a call with
// itself. What can actually go wrong is that a preflight grows a per-vendor
// table of its own — right on the day it is written, and silently wrong the day
// a level moves. This walks the columns a `--read` room would open with and
// requires the same word on both surfaces.
func TestThePreflightPostureIsTheRoomsOwnBadge(t *testing.T) {
	noVendorOverrides(t)
	t.Setenv("PATH", t.TempDir())

	// Write is false: the room `--read` opens, which is the room the preflight's
	// rows describe.
	room := map[model.VendorID]string{}
	for _, c := range stateWith(Options{}, false).Columns {
		room[c.Vendor] = c.Sandbox.Badge()
	}
	seats := DoctorSeats()
	if len(seats) == 0 {
		t.Fatal("the preflight lists no seats at all")
	}
	for _, s := range seats {
		v := model.VendorID(s.Vendor)
		want, ok := room[v]
		if !ok {
			t.Errorf("%s is in the preflight and not in the room", s.Vendor)
			continue
		}
		if s.Posture.Badge != want {
			t.Errorf("%s: the preflight says %q and the column says %q",
				s.Vendor, s.Posture.Badge, want)
		}
		if s.Posture.Evidence == "" {
			t.Errorf("%s carries the badge %q with no evidence class behind it",
				s.Vendor, s.Posture.Badge)
		}
		// The gating fact is read off canGate on both surfaces, so the preflight
		// cannot promise a gate the room will not open.
		if s.Posture.CanGate != canGate(v) {
			t.Errorf("%s: the preflight says gatable = %v, canGate says %v",
				s.Vendor, s.Posture.CanGate, canGate(v))
		}
	}
}

// TestEveryPostureLevelHasAnEvidenceClass mirrors TestEveryBadgeIsExplained, and
// closes the same gap one surface further out: a badge that renders with nothing
// to say what KIND of evidence is behind it. That gap comes back the day a sixth
// level lands, which is why this walks the type rather than listing the five.
func TestEveryPostureLevelHasAnEvidenceClass(t *testing.T) {
	seen := map[string]SandboxLevel{}
	for l := SandboxUnknown; l <= SandboxGated; l++ {
		b := SandboxClaim{Level: l}.Badge()
		e := evidenceClass(l)
		if b == "" {
			// SandboxUnknown renders no badge, so there is nothing to classify —
			// and it must not invent a sentence about a seat council makes no
			// claim about.
			if e != "" {
				t.Errorf("%v renders no badge and still carries an evidence class: %q", l, e)
			}
			continue
		}
		if e == "" {
			t.Errorf("the badge %q renders on a column and nothing says what evidence "+
				"stands behind it", b)
			continue
		}
		if prev, dup := seen[e]; dup {
			t.Errorf("%v and %v share one evidence class, so the preflight cannot tell "+
				"them apart: %q", prev, l, e)
		}
		seen[e] = l
	}
}

// TestNoEvidenceClassSoftensItsBadge, on TestThePostureLegendDoesNotSoftenAnyClaim's
// terms exactly. These sentences classify evidence; they never weaken it. The two
// words that mean "this seat can change your files" keep meaning that, the weakest
// badge keeps admitting it is weak, and nothing here may call a posture read-only,
// safe, or unable to write — the badges break the `ro:` prefix on purpose, and a
// classification that put it back would undo that outside the room, where there is
// no legend to correct it.
func TestNoEvidenceClassSoftensItsBadge(t *testing.T) {
	for _, tc := range []struct {
		level SandboxLevel
		want  []string
	}{
		{SandboxNone, []string{"measured", "change your files"}},
		{SandboxRequested, []string{"never observed"}},
		{SandboxTools, []string{"absent"}},
		{SandboxEnforced, []string{"operating system"}},
		{SandboxWrite, []string{"edit and run"}},
		{SandboxGated, []string{"asks before every tool call"}},
	} {
		got := strings.ToLower(evidenceClass(tc.level))
		for _, w := range tc.want {
			if !strings.Contains(got, strings.ToLower(w)) {
				t.Errorf("the evidence class for %q dropped %q: %q",
					SandboxClaim{Level: tc.level}.Badge(), w, got)
			}
		}
	}
	for l := SandboxUnknown; l <= SandboxGated; l++ {
		if l != SandboxNone && l != SandboxWrite && l != SandboxGated {
			continue
		}
		g := strings.ToLower(evidenceClass(l))
		for _, forbidden := range []string{"read-only", "safe", "cannot write"} {
			if strings.Contains(g, forbidden) {
				t.Errorf("the evidence class for %q says %q: %q",
					SandboxClaim{Level: l}.Badge(), forbidden, g)
			}
		}
	}
}
