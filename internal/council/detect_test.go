package council

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// noVendorOverrides blanks every seat's TELLTALE_COUNCIL_*_BIN for the length
// of the test, iterating candidates() so a seat added later is covered without
// anyone remembering this helper exists.
//
// It is for the tests that run detection across ALL seats: their assertions
// are written against what detectOne does with PATH and knownPaths, and an
// override var a developer keeps in their own environment preempts both. The
// concrete failure it prevents is an override naming a binary that has since
// moved — detectOne then reports "points at …, which does not exist", a note
// that names neither PATH nor shim, and TestDetectNamesEveryMissingVendor
// fails on that developer's machine while CI, which has no overrides, stays
// green. Same class as tempHome's brief blanking, one env read over.
func noVendorOverrides(t *testing.T) {
	t.Helper()
	for _, c := range candidates() {
		t.Setenv(c.envVar, "")
	}
}

// TestKindOfClassifiesShims is the Windows correctness case, not a style
// preference: Go's os/exec runs .cmd and .bat through cmd.exe, whose argument
// parsing cannot be safely quoted for arbitrary text. Misclassifying one of
// these as native is how a prompt containing a quote or an ampersand stops
// being a prompt.
func TestKindOfClassifiesShims(t *testing.T) {
	cases := map[string]BinaryKind{
		`C:\Users\dev\AppData\Roaming\npm\codex.cmd`: KindShim,
		`C:\Users\dev\AppData\Roaming\npm\codex.CMD`: KindShim,
		`C:\tools\wrapper.bat`:                       KindShim,
		`C:\Users\dev\.local\bin\claude.exe`:         KindNative,
		`/usr/local/bin/claude`:                      KindNative,
		`/home/dev/.local/bin/agy`:                   KindNative,
	}
	for path, want := range cases {
		if got := kindOf(path); got != want {
			t.Errorf("kindOf(%q) = %v, want %v", path, got, want)
		}
	}
}

// TestShimWithoutStdinIsRefused: a vendor that can only take its prompt as an
// argument, resolved to a shim, must be marked unusable rather than driven. The
// refusal is the feature.
func TestShimWithoutStdinIsRefused(t *testing.T) {
	info := classify(
		VendorInfo{Vendor: model.VendorAntigravity, Label: "Antigravity", Binary: `C:\npm\agy.cmd`, Kind: KindShim},
		candidate{vendor: model.VendorAntigravity, envVar: "TELLTALE_COUNCIL_AGY_BIN", stdinPrompt: false},
	)
	if info.Avail != AvailUnusable {
		t.Fatalf("shim + argv-only prompt = %v, want AvailUnusable", info.Avail)
	}
	if !strings.Contains(info.Note, "TELLTALE_COUNCIL_AGY_BIN") {
		t.Errorf("the note does not say how to fix it: %q", info.Note)
	}
}

// TestShimWithStdinIsSeated is the Codex case: npm installs it as codex.cmd,
// and it is drivable anyway because the prompt goes on stdin, so only fixed
// metacharacter-free flags ever cross cmd.exe.
func TestShimWithStdinIsSeated(t *testing.T) {
	info := classify(
		VendorInfo{Vendor: model.VendorCodex, Label: "Codex", Binary: `C:\npm\codex.cmd`, Kind: KindShim},
		candidate{vendor: model.VendorCodex, envVar: "TELLTALE_COUNCIL_CODEX_BIN", stdinPrompt: true},
	)
	if info.Avail != AvailInstalled {
		t.Fatalf("shim + stdin prompt = %v, want AvailInstalled", info.Avail)
	}
}

func TestEnvOverridePointingAtNothingIsReported(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-here")
	t.Setenv("TELLTALE_COUNCIL_CLAUDE_BIN", missing)

	info := detectOne(candidates()[0])
	if info.Avail != AvailNotInstalled {
		t.Fatalf("override at a missing path = %v, want AvailNotInstalled", info.Avail)
	}
	if !strings.Contains(info.Note, "does not exist") {
		t.Errorf("the note does not explain the override failed: %q", info.Note)
	}
}

func TestEnvOverrideIsHonoured(t *testing.T) {
	dir := t.TempDir()
	name := "fake-claude"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("not a real binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TELLTALE_COUNCIL_CLAUDE_BIN", path)

	info := detectOne(candidates()[0])
	if info.Avail != AvailInstalled {
		t.Fatalf("override at an existing path = %v, want AvailInstalled", info.Avail)
	}
	if info.Binary != path {
		t.Errorf("Binary = %q, want the override %q", info.Binary, path)
	}
}

// TestDetectNamesEveryMissingVendor: an absent vendor must carry a note saying
// what was looked for. A blank card would be the same failure as a HUD column
// that renders an em dash for two different reasons.
func TestDetectNamesEveryMissingVendor(t *testing.T) {
	noVendorOverrides(t)
	t.Setenv("PATH", t.TempDir())
	for _, info := range Detect() {
		if info.Avail == AvailInstalled {
			continue
		}
		if info.Note == "" {
			t.Errorf("%s is unavailable with no explanation", info.Vendor)
		}
		if !strings.Contains(info.Note, "PATH") && !strings.Contains(info.Note, "shim") {
			t.Errorf("%s note does not say what went wrong: %q", info.Vendor, info.Note)
		}
	}
}

// TestCursorIsSeatedButOnlyUnderItsAgentName replaces TestCursorIsNotSeated,
// which pinned ADR-008 §7's original ruling. Half of that ruling was right and
// half of it was false, and the new contract has to keep the right half.
//
// Right: `cursor` on PATH is the editor launcher (diff/merge/goto), not an
// agent CLI. Seating that binary would produce a column that fails
// confusingly. `agent` is likewise not claimable — the installer drops an
// agent.cmd next to cursor-agent.cmd, but the name is far too generic to grab
// off a shared PATH.
//
// False: "cursor-agent is not installed". It was, at
// %LOCALAPPDATA%\cursor-agent, and had been since July. The check that produced
// that sentence was a PATH lookup in one shell.
func TestCursorIsSeatedButOnlyUnderItsAgentName(t *testing.T) {
	var cursor *candidate
	for i, c := range candidates() {
		if c.vendor == model.VendorCursor {
			cursor = &candidates()[i]
		}
	}
	if cursor == nil {
		t.Fatal("cursor is not seated; ADR-008 §7 was amended to seat it once cursor-agent was found installed")
	}
	if !slices.Contains(cursor.names, "cursor-agent") {
		t.Errorf("cursor does not look for cursor-agent: %v", cursor.names)
	}
	for _, banned := range []string{"cursor", "agent"} {
		if slices.Contains(cursor.names, banned) {
			t.Errorf("%q is not the agent CLI; claiming it off PATH is how a column fails confusingly", banned)
		}
	}
	if len(cursor.knownPaths) == 0 {
		t.Error("cursor has no known install locations, which is the check that would have caught the original false claim")
	}
	// argv-only, read out of the shipped bundle: print mode's prompt is the
	// variadic positional and no code path reads it from stdin. That is what
	// made the Windows shim unusable rather than merely awkward — and it is
	// still true, which is why the seat rests on the underlay below rather than
	// on a stdin path someone hoped for.
	if cursor.stdinPrompt {
		t.Error("cursor is marked as taking its prompt on stdin; the bundle has no such path, and driving the .cmd on that belief would put prompt text through cmd.exe")
	}
	if cursor.nativeUnder == nil {
		t.Error("cursor has no native underlay; without it the Windows seat is back to AvailUnusable behind a .cmd it never needed to go through")
	}
}

// TestKnownPathResolutionIsHonestAboutWhereItLooked is the detection fix
// itself. A binary that exists but is not on this shell's PATH must resolve,
// and the card must say it came from somewhere else — "not installed" was the
// original false claim and an unlabelled find would be the next one.
func TestKnownPathResolutionIsHonestAboutWhereItLooked(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir()) // a PATH with nothing in it
	name := "fake-agent"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("not a real binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	c := candidate{
		vendor:     model.VendorCursor,
		label:      "Cursor",
		names:      []string{"nothing-by-this-name"},
		knownPaths: []string{path},
		envVar:     "TELLTALE_COUNCIL_CURSOR_BIN",
	}
	info := detectOne(c)
	if info.Avail != AvailInstalled {
		t.Fatalf("a binary off PATH detected as %v, want AvailInstalled", info.Avail)
	}
	if info.Binary != path {
		t.Errorf("Binary = %q, want %q", info.Binary, path)
	}
	if !strings.Contains(info.Source, "PATH") {
		t.Errorf("Source = %q; it must say the binary did not come from PATH", info.Source)
	}
}

// TestMissingVendorNamesTheKnownPathsToo: the absent card has to be falsifiable.
// "not found on PATH" alone is what let the original claim stand — a reader had
// no way to see that nowhere else had been checked.
func TestMissingVendorNamesTheKnownPathsToo(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	c := candidate{
		vendor:     model.VendorCursor,
		names:      []string{"nothing-by-this-name"},
		knownPaths: []string{filepath.Join(t.TempDir(), "also-not-here")},
		envVar:     "TELLTALE_COUNCIL_CURSOR_BIN",
	}
	info := detectOne(c)
	if info.Avail != AvailNotInstalled {
		t.Fatalf("Avail = %v, want AvailNotInstalled", info.Avail)
	}
	if !strings.Contains(info.Note, "also-not-here") {
		t.Errorf("the note does not say which locations were checked: %q", info.Note)
	}
}

// TestUnusableCardNamesTheBinaryItFound: "unusable" is a verdict users
// disbelieve, and the first question is always which binary council means.
func TestUnusableCardNamesTheBinaryItFound(t *testing.T) {
	info := classify(
		VendorInfo{
			Vendor: model.VendorCursor, Label: "Cursor",
			Binary: `C:\Users\dev\AppData\Local\cursor-agent\cursor-agent.cmd`,
			Kind:   KindShim,
			Source: "a known install location, not on this shell's PATH",
		},
		candidate{
			vendor: model.VendorCursor, envVar: "TELLTALE_COUNCIL_CURSOR_BIN",
			stdinPrompt:  false,
			unusableHint: "this install ships no native executable",
		},
	)
	if info.Avail != AvailUnusable {
		t.Fatalf("Avail = %v, want AvailUnusable", info.Avail)
	}
	if !strings.Contains(info.Note, "cursor-agent.cmd") {
		t.Errorf("the note does not name the binary: %q", info.Note)
	}
	if !strings.Contains(info.Note, "known install location") {
		t.Errorf("the note does not say where it came from: %q", info.Note)
	}
	// The generic "point the env var at the real executable" advice is wrong
	// for a vendor that ships no such executable, and advice that cannot be
	// followed is worse than none.
	if !strings.Contains(info.Note, "no native executable") {
		t.Errorf("the per-vendor hint was dropped for the generic one: %q", info.Note)
	}
}

// fakeCursorInstall builds the directory layout cursor-agent's own launcher
// expects, so the resolution can be exercised on either machine.
//
// Nothing here is a stand-in for something that could have been tested for
// real: the live install was verified separately (node.exe + index.js run
// directly produce byte-identical --help output to the .cmd). What these files
// pin is the SELECTION — which directory, which pair, and what happens when the
// layout is not what the launcher assumed.
func fakeCursorInstall(t *testing.T, versions ...string) (root, shim string) {
	t.Helper()
	root = t.TempDir()
	shim = filepath.Join(root, "cursor-agent.cmd")
	write := func(p string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(shim)
	for _, v := range versions {
		write(filepath.Join(root, "versions", v, "node.exe"))
		write(filepath.Join(root, "versions", v, "index.js"))
	}
	return root, shim
}

func detectCursorAt(t *testing.T, shim string) VendorInfo {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	var c candidate
	for _, cand := range candidates() {
		if cand.vendor == model.VendorCursor {
			c = cand
		}
	}
	c.names = []string{"nothing-by-this-name"}
	c.knownPaths = []string{shim}
	return detectOne(c)
}

// TestCursorShimResolvesToTheBundledNode is the whole unlock, asserted as the
// chain it actually is rather than as a path string.
//
// The seat was AvailUnusable on Windows because argv-only + .cmd is what
// runner.ErrShellShimWithArgvPrompt refuses. The .cmd turned out to be a
// launcher that execs a bundled node — so council execs the same node, the
// resolved binary is KindNative, no shell is in the invocation, and the seat is
// AvailInstalled with its prompt still in argv.
func TestCursorShimResolvesToTheBundledNode(t *testing.T) {
	root, shim := fakeCursorInstall(t, "2026.07.23-e383d2b")
	info := detectCursorAt(t, shim)

	if info.Avail != AvailInstalled {
		t.Fatalf("Avail = %v (%s), want AvailInstalled", info.Avail, info.Note)
	}
	want := filepath.Join(root, "versions", "2026.07.23-e383d2b", "node.exe")
	if info.Binary != want {
		t.Errorf("Binary = %q, want the bundled node %q", info.Binary, want)
	}
	// The link in the chain that makes the argv prompt legal. If this ever
	// regressed to KindShim the seat would be refused by the runner instead —
	// correctly, but the seat would be gone again.
	if info.Kind != KindNative {
		t.Errorf("Kind = %v, want KindNative — a shim would put prompt text through cmd.exe", info.Kind)
	}
	// The card has to say council stepped over something. A seat that silently
	// swapped the binary a user pointed it at would be the same class of
	// unfalsifiable claim as the original "not installed".
	if !strings.Contains(info.Source, "launcher") {
		t.Errorf("Source = %q; it does not say the launcher was stepped over", info.Source)
	}
}

// TestCursorPicksTheNewestVersionDirectory. cursor-agent auto-updates by
// dropping a new directory beside the old one, and its launcher sorts by the
// date in the name. A resolution that took the first entry would run whatever
// the filesystem happened to list first, which is a version the user has no
// reason to expect and no way to see.
func TestCursorPicksTheNewestVersionDirectory(t *testing.T) {
	root, shim := fakeCursorInstall(t,
		"2026.07.23-e383d2b",
		"2026.11.2-aaaa111",  // single-digit month/day: the launcher zero-pads
		"2026.9.30-bbbb222",  // later in string order, earlier in date order
		"2025.12.31-cccc333", // an older year that sorts high on the month
	)
	info := detectCursorAt(t, shim)
	want := filepath.Join(root, "versions", "2026.11.2-aaaa111", "node.exe")
	if info.Binary != want {
		t.Errorf("Binary = %q, want the newest by date %q", info.Binary, want)
	}
}

// TestCursorAcceptsTheTimestampedVersionForm: upstream added a build timestamp
// between the date and the commit hash, and its launcher accepts both forms.
// Rejecting the new one would strand a seat on an old install directory — or,
// once the updater deleted it, on nothing.
func TestCursorAcceptsTheTimestampedVersionForm(t *testing.T) {
	root, shim := fakeCursorInstall(t, "2026.07.23-e383d2b", "2026.8.4-11-30-00-f00dcafe")
	info := detectCursorAt(t, shim)
	want := filepath.Join(root, "versions", "2026.8.4-11-30-00-f00dcafe", "node.exe")
	if info.Binary != want {
		t.Errorf("Binary = %q, want the timestamped form to be accepted and win: %q", info.Binary, want)
	}
}

// TestCursorResolutionIsNotCached. Auto-updates move the version directory, and
// Detect runs once per room — so the answer must be re-derived, never
// remembered. A cached path outlives the directory it names and turns a
// detection question into a failed turn.
func TestCursorResolutionIsNotCached(t *testing.T) {
	root, shim := fakeCursorInstall(t, "2026.07.23-e383d2b")
	first := detectCursorAt(t, shim)

	newer := filepath.Join(root, "versions", "2026.8.4-99999aa")
	if err := os.MkdirAll(newer, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"node.exe", "index.js"} {
		if err := os.WriteFile(filepath.Join(newer, n), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	second := detectCursorAt(t, shim)
	if second.Binary == first.Binary {
		t.Errorf("resolution did not move after an update: still %q", second.Binary)
	}
	if second.Binary != filepath.Join(newer, "node.exe") {
		t.Errorf("Binary = %q, want the newly installed %q", second.Binary, filepath.Join(newer, "node.exe"))
	}
}

// TestCursorBrokenLayoutIsUnusableAndNamesWhatWasMissing. The cost of this
// resolution is a dependency on someone else's directory layout, and the
// mitigation is not to be clever when it changes: degrade to an honest empty
// seat, name the path that was expected, and never fall back to driving the
// shim — which would put prompt text through cmd.exe to avoid an empty column.
func TestCursorBrokenLayoutIsUnusableAndNamesWhatWasMissing(t *testing.T) {
	// A node with no bundle beside it: the launcher's own pair, broken.
	root, shim := fakeCursorInstall(t)
	dir := filepath.Join(root, "versions", "2026.07.23-e383d2b")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node.exe"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	info := detectCursorAt(t, shim)
	if info.Avail != AvailUnusable {
		t.Fatalf("Avail = %v, want AvailUnusable when the layout does not match", info.Avail)
	}
	if !strings.Contains(info.Note, "index.js") && !strings.Contains(info.Note, "node.exe") {
		t.Errorf("the note does not name what was expected and not found: %q", info.Note)
	}
	if info.Kind == KindShim && info.Avail == AvailInstalled {
		t.Error("a broken layout fell back to driving the .cmd; that is the refusal this whole chain exists to keep")
	}
}

// TestCursorMissingVersionsDirIsUnusableNotAbsent: the install is there, so
// "not installed" would be the original false claim again, one layer down.
func TestCursorMissingVersionsDirIsUnusableNotAbsent(t *testing.T) {
	_, shim := fakeCursorInstall(t)
	info := detectCursorAt(t, shim)
	if info.Avail != AvailUnusable {
		t.Fatalf("Avail = %v, want AvailUnusable", info.Avail)
	}
	if !strings.Contains(info.Note, "versions") {
		t.Errorf("the note does not name the directory it looked in: %q", info.Note)
	}
}

// TestCursorOverrideAtANodeStillNeedsItsBundle. The override is documented as
// "point it at a native exe", and a node.exe satisfies that while being useless
// without the JavaScript beside it. Caught at detection, where a user can read
// the reason, rather than as an exit-1 turn quoting a missing-module error.
func TestCursorOverrideAtANodeStillNeedsItsBundle(t *testing.T) {
	dir := t.TempDir()
	node := filepath.Join(dir, "node.exe")
	if err := os.WriteFile(node, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TELLTALE_COUNCIL_CURSOR_BIN", node)

	var c candidate
	for _, cand := range candidates() {
		if cand.vendor == model.VendorCursor {
			c = cand
		}
	}
	info := detectOne(c)
	if info.Avail != AvailUnusable {
		t.Fatalf("Avail = %v, want AvailUnusable for a node with no bundle", info.Avail)
	}

	if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if info := detectOne(c); info.Avail != AvailInstalled || info.Binary != node {
		t.Errorf("a node WITH its bundle detected as %v at %q, want AvailInstalled at the override", info.Avail, info.Binary)
	}
}

// TestCursorNativeEntryPointIsLeftAlone is the non-Windows case. The POSIX
// install is an extensionless script, which is already native and already
// drivable, and there is nothing under it to step over — the resolution must
// not go looking for a node that is not part of that install's shape.
func TestCursorNativeEntryPointIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	agent := filepath.Join(dir, "cursor-agent")
	if err := os.WriteFile(agent, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TELLTALE_COUNCIL_CURSOR_BIN", agent)

	var c candidate
	for _, cand := range candidates() {
		if cand.vendor == model.VendorCursor {
			c = cand
		}
	}
	info := detectOne(c)
	if info.Avail != AvailInstalled {
		t.Fatalf("Avail = %v (%s), want AvailInstalled", info.Avail, info.Note)
	}
	if info.Binary != agent {
		t.Errorf("Binary = %q, want the entry point untouched %q", info.Binary, agent)
	}
}

// TestCursorClaimsNoMoreThanRequested, re-asked against the protocol that
// replaced the one it was written for.
//
// The level has survived two rewrites of this seat and it is the same level for
// a third set of reasons. Under ACP, plan mode did better than print mode's ever
// did — asked to create a file the seat declined and nothing landed — and the
// badge still says `requested`, because one trial of a mode the model obeys is
// not a layer that stops it.
//
// The two assertions below are the things a reader could otherwise carry over
// from the old path and be wrong about, both in the direction this product
// refuses to be wrong in.
func TestCursorClaimsNoMoreThanRequested(t *testing.T) {
	for _, win := range []bool{true, false} {
		claim := sandboxFor(model.VendorCursor, win)
		if claim.Level != SandboxRequested {
			t.Errorf("cursor (windows=%v) claims %v, want SandboxRequested", win, claim.Level)
		}
		if claim.Detail == "" {
			t.Errorf("cursor (windows=%v) has a badge with no explanation behind it", win)
		}
	}
	d := sandboxFor(model.VendorCursor, true).Detail
	// There is no sandbox request at all any more: ACP takes no such parameter,
	// on any OS. A reader who assumed one was still being asked for would be
	// assuming a protection nothing provides.
	if !strings.Contains(d, "no sandbox request") {
		t.Errorf("the detail does not say the sandbox request is gone: %q", d)
	}
	// And workspace trust does not apply on this path. Print mode refused to run
	// in an untrusted directory; the ACP server wrote a file into the SAME
	// directory without a prompt. That screen is gone and the badge has to say so.
	if !strings.Contains(d, "trust") {
		t.Errorf("the detail does not say workspace trust no longer applies: %q", d)
	}
	// The badge is now the same on every OS, because the thing that used to split
	// it — a flag — no longer exists.
	if sandboxFor(model.VendorCursor, false).Detail != d {
		t.Error("the cursor badge is still split by OS; nothing in the ACP invocation differs by platform")
	}
}

// TestOnlyTheSeatThatAsksAboutEverythingIsBadgedGated.
//
// canGate used to read "is this vendor drivable as a live process", off the
// registry, and that was right for exactly as long as those two questions had
// the same answer. The Cursor seat is a live ACP process now and CAN ask —
// session/request_permission blocks it, measured on both branches — and it does
// NOT ask about edits: it wrote a file, twice, in a directory it had never been
// told to trust, raising nothing.
//
// `gated` promises that nothing which changes anything runs without a keystroke.
// A seat that writes files silently cannot carry it, however much of a live
// process it is.
func TestOnlyTheSeatThatAsksAboutEverythingIsBadgedGated(t *testing.T) {
	for _, v := range []model.VendorID{
		model.VendorClaude, model.VendorCodex, model.VendorAntigravity, model.VendorCursor,
	} {
		claim := postureClaim(v, true, true, true, true)
		gated := claim.Level == SandboxGated
		if want := v == model.VendorClaude; gated != want {
			t.Errorf("%s: gated = %v, want %v — the badge is a measured coverage claim, not a fact about the transport",
				v, gated, want)
		}
	}
	// The seat that asks about SOME things says so where the badge's argument
	// lives, rather than being promoted into a claim it would break.
	d := postureClaim(model.VendorCursor, true, true, true, true).Detail
	if !strings.Contains(d, "does NOT ask about file edits") {
		t.Errorf("the cursor write detail hides what its cards do not cover: %q", d)
	}
}

// TestCursorGranularityIsMeasuredTokens replaces
// TestCursorGranularityIsNotClaimed, which held this column at GranUnknown
// because a help string and a bundle emit path are not measurements.
//
// Someone watched the pipe. A one-word reply arrived as "P" then "ONG", each in
// its own assistant event; a sentence arrived as "I", " said", " P", "ONG", ".".
// That is token-level and the column may say so — which is also why the
// promotion had to wait: Antigravity's `text_delta` looked exactly this
// promising in the schema and delivered a whole reply 73 seconds late.
func TestCursorGranularityIsMeasuredTokens(t *testing.T) {
	if got := granularityFor(model.VendorCursor); got != GranTokens {
		t.Errorf("cursor = %v, want GranTokens — per-chunk assistant events were captured live", got)
	}
	if got := granularityFor(model.VendorCursor).String(); got == "" {
		t.Error("cursor prints no granularity word; a measured one must be printed")
	}
}

// TestNoVendorClaimsUnverifiedEnforcement is the ADR-008 §3 correction, pinned.
//
// Codex may only claim OS-level enforcement where it has been verified, and
// since 2026-08-29 that is BOTH branches. Windows claimed nothing at all until
// then — at codex-cli 0.146.0 both sandboxed modes failed every process spawn
// (ADR-008, twelfth amendment) and council passed -s danger-full-access, so an
// ro: prefix there would have been the worst false badge this room could
// carry. The re-measurement at 0.149.1 earned the claim back the only way this
// repo accepts: a live shell write under -s read-only was denied with no file
// on disk. Claiming a sandbox we have not seen engage is exactly the
// overstatement this repo refuses — and so is claiming a requested one on an
// invocation that requests nothing.
func TestNoVendorClaimsUnverifiedEnforcement(t *testing.T) {
	if got := sandboxFor(model.VendorCodex, true).Level; got != SandboxEnforced {
		t.Errorf("codex on windows claims %v, want SandboxEnforced — the 2026-08-29 re-measurement earned it", got)
	}
	// The badge is the load-bearing half of that, in both directions. It
	// renders `ro:enforced` only while the invocation actually passes
	// -s read-only there; if the argv ever collapses back to
	// danger-full-access, this claim goes false with it, and
	// vendors.TestCodexPostureIsPerOS pins that half.
	if got := sandboxFor(model.VendorCodex, true).Badge(); got != "ro:enforced" {
		t.Errorf("codex badge on windows is %q, want ro:enforced", got)
	}
	// The detail must carry the measurement, not just the conclusion: the date
	// and the pinned version are what let a reader decide whether the claim is
	// stale on their build.
	if d := sandboxFor(model.VendorCodex, true).Detail; !strings.Contains(d, "0.149.1") {
		t.Errorf("the windows codex detail does not cite the build it was measured on: %q", d)
	}
	// Off Windows the codex badge is REQUESTED since 2026-09-02: the seat moved
	// to `codex app-server`, every arm of which ran on Windows, and the macOS
	// enforcement was measured through `codex exec`. A seat move re-measures
	// rather than inherits (§9.50, §9.54); seatshape_test.go pins the detail.
	if got := sandboxFor(model.VendorCodex, false).Level; got != SandboxRequested {
		t.Errorf("codex on unix claims %v, want SandboxRequested — app-server is unmeasured there", got)
	}
	// Antigravity is the strong case: not "unverified" but REFUTED. Asked to
	// write a file under --mode plan --sandbox, it wrote the file. Anything
	// above SandboxNone would put a read-only posture on a column measured not
	// to have one, and `ro:requested` reads at a glance as "some read-only
	// posture" — which is exactly the glance this badge must not survive.
	for _, win := range []bool{true, false} {
		if got := sandboxFor(model.VendorAntigravity, win).Level; got != SandboxNone {
			t.Errorf("agy claims %v, want SandboxNone — its flags were measured not to restrict it", got)
		}
	}
	if got := sandboxFor(model.VendorAntigravity, true).Badge(); strings.HasPrefix(got, "ro:") {
		t.Errorf("agy badge is %q; a vendor that can write must not wear an ro: prefix", got)
	}
	// And the detail may not claim council asks for something it has stopped
	// asking for (ADR-008, seventeenth amendment). Every other claim in this
	// file is about a VENDOR, where the honest fallback is "not observed"; this
	// one is about THIS TOOL's own behaviour, where there is no such fallback —
	// so a stale sentence here is the one class of false claim the repo has no
	// excuse for. It went stale the moment the flags came off, and nothing else
	// in the build would have caught it.
	if d := sandboxFor(model.VendorAntigravity, true).Detail; strings.Contains(d, "still passed") {
		t.Errorf("the agy detail still claims council passes the dropped flags: %q", d)
	}
	// Claude's mechanism is real but is a tool allowlist, not an OS sandbox,
	// and the badge must not imply otherwise.
	if got := sandboxFor(model.VendorClaude, true).Level; got != SandboxTools {
		t.Errorf("claude claims %v, want SandboxTools", got)
	}
	// Grok is the Antigravity case with a second, weaker-looking half that is
	// actually the sharper one. --permission-mode plan was REFUTED (asked to
	// write a file under it, it wrote the file), which is what forces
	// SandboxNone. --sandbox was not refuted but shown UNOBSERVABLE: it
	// silently accepts a profile name that cannot exist, so council has no way
	// to distinguish a real request from a typo and asks for neither (§9.39).
	for _, win := range []bool{true, false} {
		if got := sandboxFor(model.VendorGrok, win).Level; got != SandboxNone {
			t.Errorf("grok claims %v, want SandboxNone — plan mode was measured writing the file", got)
		}
	}
	if got := sandboxFor(model.VendorGrok, true).Badge(); strings.HasPrefix(got, "ro:") {
		t.Errorf("grok badge is %q; a vendor measured writing must not wear an ro: prefix", got)
	}
	// The same this-tool's-own-behaviour rule the agy detail is held to: the
	// badge may not say council passes a flag it does not pass. Asserted as the
	// absence of the two flag names in any affirmative form by checking the
	// invocation instead — vendors.TestGrokAsksForNothingInEitherPosture owns
	// that half — and here by requiring the detail to name why nothing is asked.
	if d := sandboxFor(model.VendorGrok, true).Detail; !strings.Contains(d, "--sandbox") {
		t.Errorf("the grok detail does not explain the unobservable sandbox flag: %q", d)
	}
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorCodex,
		model.VendorAntigravity, model.VendorGrok} {
		for _, win := range []bool{true, false} {
			if sandboxFor(v, win).Detail == "" {
				t.Errorf("%s (windows=%v) has a badge with no explanation behind it", v, win)
			}
		}
	}
}

// TestEverySeatableVendorHasACandidate. The registry says a seat CAN be driven;
// candidates() says council knows where to find its binary. A vendor in one and
// not the other is a seat that either renders as a permanently-absent column or
// resolves a path nothing can drive — both silent, and neither caught by any
// other test in this file.
func TestEverySeatableVendorHasACandidate(t *testing.T) {
	found := map[model.VendorID]bool{}
	for _, c := range candidates() {
		found[c.vendor] = true
		if c.envVar == "" {
			t.Errorf("%s has no override env var; an unusual install would be unreachable", c.vendor)
		}
		if len(c.names) == 0 {
			t.Errorf("%s has no command name to look for", c.vendor)
		}
	}
	for _, v := range addressableVendors() {
		if !found[v] {
			t.Errorf("%s can be addressed and seated but candidates() cannot find its binary", v)
		}
	}
}

// TestGranularityMatchesWhatWasMeasured. All three were run; two of them are
// worse than the provisional guess and must say so, because a column labelled
// as streaming that sits silent for a minute is a lie the user has no way to
// check.
func TestGranularityMatchesWhatWasMeasured(t *testing.T) {
	if got := granularityFor(model.VendorClaude); got != GranTokens {
		t.Errorf("claude = %v, want GranTokens — token deltas were observed live", got)
	}
	// Codex emits one item per COMPLETE message; Antigravity emits a whole
	// response as a single delta. Neither streams in any sense a user would
	// recognise, and both must land in PhaseWaiting rather than PhaseStreaming.
	for _, v := range []model.VendorID{model.VendorCodex, model.VendorAntigravity} {
		if got := granularityFor(v); got != GranFinalOnly {
			t.Errorf("%s = %v, want GranFinalOnly — measured to produce nothing until the turn ends", v, got)
		}
	}
	// Grok's deltas are the finest in the room — "I'll", " read", "notes",
	// ".txt" — so it carries GranTokens on better evidence than either seat
	// already wearing the word (§9.39). Asserted here rather than left to the
	// default so that a future re-measure has to argue with a test.
	if got := granularityFor(model.VendorGrok); got != GranTokens {
		t.Errorf("grok = %v, want GranTokens — token-level deltas were observed live", got)
	}
}
