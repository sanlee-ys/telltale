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
	if got := declaredCapability(model.VendorGrok, reg); !strings.Contains(got, "batch") {
		t.Errorf("grok is a batch program per turn and does not say so: %q", got)
	}
}
