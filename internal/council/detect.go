package council

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// BinaryKind is how a resolved path will have to be executed.
//
// This distinction is load-bearing on Windows and exists for exactly one
// reason: Go's os/exec runs .cmd and .bat files through cmd.exe, whose
// argument parsing cannot be safely quoted for arbitrary text. A council prompt
// is arbitrary text — quotes, ampersands, newlines, whatever San typed — so a
// vendor whose only entry point is a shim must take its prompt on stdin, and
// one that can do neither must not be driven at all.
//
// `codex` is the live case, not a hypothetical: npm installs it as codex.cmd.
type BinaryKind uint8

const (
	// KindNative: a real executable. argv is safe.
	KindNative BinaryKind = iota
	// KindShim: a .cmd/.bat that will cross cmd.exe.
	KindShim
)

// VendorInfo is one detection result.
type VendorInfo struct {
	Vendor model.VendorID
	Label  string
	Binary string
	Kind   BinaryKind
	Avail  Availability
	// Source is HOW the binary was resolved, in the words the card shows.
	//
	// It exists because "not installed" and "installed somewhere LookPath does
	// not look" are different facts that used to render identically. ADR-008 §7
	// asserted cursor-agent was not installed on the strength of a failed PATH
	// lookup; it was installed the whole time. A card that names where the
	// binary came from cannot make that mistake silently.
	Source string
	// Note explains an Avail that is not Installed, in the words the card shows.
	Note string
}

// candidate is a vendor council knows how to seat.
type candidate struct {
	vendor model.VendorID
	label  string
	// names are the command names to try on PATH, in order.
	names []string
	// knownPaths are absolute locations to stat when PATH misses.
	//
	// PATH is a claim about the CURRENT process, not about the machine. An
	// installer that appends to the user PATH does nothing for a shell that was
	// already open, and council is frequently launched from exactly such a
	// shell. Checking a handful of well-known locations is the difference
	// between "you have not installed this" and "your terminal predates the
	// install", and only one of those is true often enough to print.
	//
	// A wrong guess here is cheap and cannot lie: an absent path simply fails
	// its stat, so the worst case is the detection council would have made
	// anyway.
	knownPaths []string
	// envVar overrides the resolved path entirely, so a user whose vendor is
	// installed somewhere unusual — or who wants to bypass an npm shim by
	// pointing straight at the vendored executable — can say so.
	envVar string
	// stdinPrompt reports that this vendor accepts its prompt on stdin, which
	// is what makes a shim safe to drive.
	stdinPrompt bool
	// unusableHint is what to tell a user whose only entry point is a shim.
	// Empty falls back to the generic advice, which is right wherever a native
	// executable actually exists to point envVar at.
	unusableHint string
}

func candidates() []candidate {
	return []candidate{
		{
			vendor:      model.VendorClaude,
			label:       "Claude Code",
			names:       []string{"claude"},
			envVar:      "TELLTALE_COUNCIL_CLAUDE_BIN",
			stdinPrompt: true,
		},
		{
			vendor: model.VendorCodex,
			label:  "Codex",
			names:  []string{"codex"},
			envVar: "TELLTALE_COUNCIL_CODEX_BIN",
			// Verified: `codex exec` takes `-` and reads the prompt from stdin.
			// This is what makes the npm .cmd shim drivable.
			stdinPrompt: true,
		},
		{
			vendor: model.VendorAntigravity,
			label:  "Antigravity",
			names:  []string{"agy"},
			envVar: "TELLTALE_COUNCIL_AGY_BIN",
			// Not established yet — the PR 3 spike settles it. Until then agy is
			// assumed argv-only, which is safe because it resolves to a native
			// .exe; if it ever resolved to a shim it would be marked unusable
			// rather than driven through cmd.exe.
			stdinPrompt: false,
		},
		{
			vendor: model.VendorCursor,
			label:  "Cursor",
			// `cursor-agent` ONLY. The bare `cursor` on PATH is the editor
			// launcher — that half of ADR-008 §7 was right and still is — and
			// `agent` (which the installer also drops next to it) is far too
			// generic a name to claim off a shared PATH.
			names:      []string{"cursor-agent"},
			knownPaths: cursorKnownPaths(),
			envVar:     "TELLTALE_COUNCIL_CURSOR_BIN",
			// Verified argv-only, by reading the shipped bundle rather than by
			// trusting the help text. Print mode's prompt is the variadic
			// positional argument and nothing else: the emit path guards with
			// `t.trim() || "Error: No prompt provided for print mode"`, where t
			// is the joined argv, and every process.stdin reference in the
			// bundle belongs either to the interactive Ink UI or to the stdin of
			// a tool the agent itself spawns. There is no `-` sentinel, no
			// --prompt-file, nothing.
			//
			// The consequence is the whole story of this seat on Windows, where
			// the only entry point is cursor-agent.cmd: argv + shim is exactly
			// what runner.ErrShellShimWithArgvPrompt refuses, so the column is
			// AvailUnusable here. On macOS and Linux the same install is an
			// extensionless script, which is KindNative, and the seat works.
			stdinPrompt: false,
			// The generic advice does not apply: there is no native executable
			// to point the override at. The Windows install is a .cmd that
			// shells into PowerShell, which runs a bundled node.exe against a
			// 9MB index.js. Driving node directly would mean this adapter owning
			// a version-pinned path into someone else's install directory, which
			// is a worse failure than an honest empty seat.
			unusableHint: "this install ships no native executable — the .cmd shells into " +
				"a bundled node — so TELLTALE_COUNCIL_CURSOR_BIN has nothing to point at " +
				"on Windows. The seat works where cursor-agent is not a .cmd",
		},
	}
}

// cursorKnownPaths is where the cursor-agent installer puts the CLI.
//
// The Windows entry is measured: it is where the binary actually sits on the
// machine ADR-008 §7 declared it absent from. The POSIX entries are the
// installer's documented targets and are NOT verified here; they are safe to
// guess precisely because a knownPath either exists or is skipped, so a wrong
// one cannot produce a wrong claim.
func cursorKnownPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		return []string{filepath.Join(home, "AppData", "Local", "cursor-agent", "cursor-agent.cmd")}
	}
	return []string{
		filepath.Join(home, ".local", "bin", "cursor-agent"),
		filepath.Join(home, ".cursor", "bin", "cursor-agent"),
	}
}

// Detect resolves every candidate against PATH.
//
// LookPath only. Council never runs a vendor to find out whether it works: a
// probe turn costs real quota, and "is it authenticated?" is a question the
// first real dispatch answers for free (ADR-008 §6). A vendor that is installed
// but not signed in therefore detects as Installed here and reports its auth
// failure as a column note when a turn is actually sent.
func Detect() []VendorInfo {
	cands := candidates()
	out := make([]VendorInfo, 0, len(cands))
	for _, c := range cands {
		out = append(out, detectOne(c))
	}
	return out
}

func detectOne(c candidate) VendorInfo {
	info := VendorInfo{Vendor: c.vendor, Label: c.label}

	if override := strings.TrimSpace(os.Getenv(c.envVar)); override != "" {
		info.Binary = override
		info.Kind = kindOf(override)
		info.Source = c.envVar
		if _, err := os.Stat(override); err != nil {
			info.Avail = AvailNotInstalled
			info.Note = c.envVar + " points at " + override + ", which does not exist"
			return info
		}
		return classify(info, c)
	}

	for _, name := range c.names {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		info.Binary = path
		info.Kind = kindOf(path)
		info.Source = "on PATH"
		return classify(info, c)
	}

	// PATH missed. That is not the same as absent, and saying so was the
	// original Cursor mistake.
	for _, path := range c.knownPaths {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		info.Binary = path
		info.Kind = kindOf(path)
		info.Source = "a known install location, not on this shell's PATH"
		return classify(info, c)
	}

	info.Avail = AvailNotInstalled
	info.Note = "not found on PATH (looked for " + strings.Join(c.names, ", ") + ")"
	if len(c.knownPaths) > 0 {
		info.Note += ", nor at " + strings.Join(c.knownPaths, " or ")
	}
	return info
}

// classify decides whether a resolved binary is one council will drive.
func classify(info VendorInfo, c candidate) VendorInfo {
	if info.Kind == KindShim && !c.stdinPrompt {
		// The refusal is the point. Driving this would mean handing a prompt to
		// cmd.exe, and a prompt containing a quote or an ampersand would either
		// break or, worse, execute as something else.
		info.Avail = AvailUnusable
		// The path and its provenance are in the card on purpose. "Unusable" is
		// the kind of verdict a user disbelieves, and the first question is
		// always WHICH binary council is talking about.
		info.Note = "found " + info.Binary
		if info.Source != "" {
			info.Note += " (" + info.Source + ")"
		}
		info.Note += ", a shell shim that takes its prompt as an argument. " +
			"Council will not put prompt text through cmd.exe, whose quoting is " +
			"unsafe for arbitrary text — " + c.hint()
		return info
	}
	info.Avail = AvailInstalled
	return info
}

// hint is the fix offered on an unusable card.
func (c candidate) hint() string {
	if c.unusableHint != "" {
		return c.unusableHint
	}
	return "set " + c.envVar + " to the real executable"
}

func kindOf(path string) BinaryKind {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat":
		return KindShim
	default:
		return KindNative
	}
}

// sandboxFor is the posture council will apply to a vendor, as a claim stated
// per vendor rather than as a blanket promise (ADR-008 §3).
//
// These are the claims the scaffold renders; the flags that back them arrive
// with each vendor's adapter. Codex reads Requested rather than Enforced on
// Windows because `-s read-only` is OS-enforced on macOS and Linux and its
// Windows behaviour is not yet verified — claiming enforcement we have not
// seen would be the exact overstatement this product exists to refuse.
// postureClaim is what a column advertises, given the room's posture.
//
// In write mode the columns that cannot ask all carry the SAME loud badge, and
// that uniformity is the point: the per-vendor distinctions below are all
// shades of "how much read-only did we manage to ask for", and once the answer
// is "none", grading them would imply a safety difference that does not exist.
// What contains such a room is the directory it was pointed at, not a flag — so
// the badge says the plain thing and the header repeats it.
//
// One column breaks that uniformity, and only because it earned a real
// difference rather than a nicer word: it asks first. gated is given to exactly
// the seat that can be driven as a live process, because that is what a
// permission request needs — somewhere to arrive and somewhere for the answer
// to go back. The other three are batch CLIs and get the badge that describes
// what they actually do.
// The gate's detail takes one more argument than the rest, and it is not a
// stylistic wrinkle: hooked is read from whether a hooks file actually exists,
// so the sentence about the guard cannot outlive the thing it describes. A
// claim keyed off "we tried to wire it" would survive an unreadable settings
// file, an empty hooks section, and a temp directory that could not be created
// — three ways to end up unscreened while the column says otherwise.
func postureClaim(v model.VendorID, windows, write, gated, hooked bool) SandboxClaim {
	if write && gated && canGate(v) {
		return SandboxClaim{
			Level:  SandboxGated,
			Detail: gatedDetail(hooked),
		}
	}
	if write {
		return SandboxClaim{
			Level: SandboxWrite,
			Detail: "started with --write: this column may edit and run things in " +
				"the workspace above. Containment is that directory, not a flag — " +
				"point council at a worktree if that matters",
		}
	}
	return sandboxFor(v, windows)
}

// gatedDetail is what the gated column defends, and the two branches differ in
// what they CLAIM rather than in tone.
//
// The shared half is the gate itself. The half that varies is the seat's other
// screen: this posture passes --setting-sources "", which drops the user's
// permission allow rules on purpose — a rule that pre-approves a call is
// exactly what a gate cannot sit behind — and used to drop their hooks with
// them, as collateral. Whether the hooks came back is a fact about a file on
// disk, so it is read from that file and not from an intention.
//
// The read-only carve-out is stated in both branches and matters more in the
// absent one: a shell command the CLI itself classifies read-only is approved
// without asking, so with no hook wired those calls have nothing at all in
// front of them. That is precisely the case the wired branch closes, and it is
// why "the guard is absent" is worth a sentence rather than a shrug.
func gatedDetail(hooked bool) string {
	const shared = "started with --write, and this column asks before every tool " +
		"call that changes anything: y approves, n denies, and nothing runs " +
		"until you answer. Your settings' permission allow rules are dropped " +
		"for this seat on purpose, and shell commands the CLI itself classifies " +
		"read-only are approved without asking"
	if hooked {
		return shared + ". Your own hooks are carried into this seat and do run " +
			"in front of it, including on the calls the gate is not asked about"
	}
	return shared + ". No hooks were carried into this seat — none were found to " +
		"copy — so the calls the gate is not asked about have nothing screening them"
}

// canGate reports whether a seat can be asked to ask.
//
// Read from the registry rather than from a list here, so the badge and the
// invocation can never disagree about which seats gate: both answer the
// question "is this vendor drivable as a live process".
func canGate(v model.VendorID) bool {
	_, ok := vendors.Registry()[v].(vendors.Persistent)
	return ok
}

func sandboxFor(v model.VendorID, windows bool) SandboxClaim {
	switch v {
	case model.VendorClaude:
		return SandboxClaim{
			Level: SandboxTools,
			// Precisely what was verified, and nothing more. --allowedTools
			// does NOT restrict a session (it pre-approves); the enforcement is
			// a deny list plus --strict-mcp-config, checked by reading the
			// session's own reported tool list. A deny list cannot cover a tool
			// that does not exist yet, and the wording says so.
			Detail: "named write/exec tools denied and MCP servers dropped; verified " +
				"against the session's own tool list, but a deny list cannot cover a " +
				"tool a future release adds",
		}
	case model.VendorCodex:
		if windows {
			return SandboxClaim{
				Level: SandboxRequested,
				// Measured, and the measurement is stranger than "unverified"
				// suggests. Under -s read-only every sandboxed process spawn
				// fails with CreateProcessAsUserW access-denied — including a
				// spawn asked to merely LIST a directory. So no shell write can
				// land, but the mechanism is a blanket inability to start a
				// process, not a read/write distinction. codex's own feature
				// list shows the Windows sandbox as removed/in flux.
				Detail: "-s read-only passed; on Windows it degrades to a blanket " +
					"process-spawn failure rather than a read/write distinction",
			}
		}
		return SandboxClaim{
			Level:  SandboxEnforced,
			Detail: "-s read-only, enforced by the OS sandbox",
		}
	case model.VendorAntigravity:
		return SandboxClaim{
			Level: SandboxNone,
			// Refuted, not unverified. Asked to write a file under both flags,
			// it wrote the file; the reported permission mode and tool list
			// were identical to a run without them.
			Detail: "--mode plan --sandbox are passed but do not restrict it: asked " +
				"to write a file under both, it wrote the file. Treat this column " +
				"as able to change your workspace",
		}
	case model.VendorCursor:
		return SandboxClaim{
			Level: SandboxRequested,
			// The weakest evidence behind any badge in this room, and the detail
			// has to say so out loud.
			//
			// `--mode plan` is documented by the CLI's own help as
			// "read-only/planning (analyze, propose plans, no edits)", and
			// `--sandbox enabled` exists beside a real cursorsandbox.exe in the
			// install. Neither was seen to engage, and could not be: the
			// installed cursor-agent reports "Not logged in", and it checks
			// authentication BEFORE it validates flags, so no invocation gets
			// far enough to demonstrate — or refute — anything.
			//
			// Two facts push this claim further down than Codex's, which wears
			// the same badge on the strength of an actual measurement:
			//
			//   - The CLI's own help for -p says print mode "Has access to all
			//     tools, including write and shell". That is the vendor stating
			//     that the mode council runs in is unrestricted by default, so
			//     everything rests on --mode plan being honoured.
			//   - The self-report cannot settle it later either. The
			//     system/init event's permissionMode is a hardcoded "default"
			//     literal in the shipped bundle, not a readout of the session.
			//     The trick that caught Claude's --allowedTools — run it and
			//     read what the session says about itself — does not work on
			//     this vendor.
			Detail: "--mode plan --sandbox enabled are requested, and nothing more is " +
				"known: this CLI is not signed in here, so no run could test them. Its " +
				"own help says print mode has access to write and shell tools, and its " +
				"init event reports a hardcoded permission mode, so the posture cannot " +
				"be confirmed by asking the session either. Weaker than every other " +
				"column's claim",
		}
	default:
		return SandboxClaim{}
	}
}

// granularityFor is how finely a vendor reports progress.
//
// All three are now measured rather than assumed, and the measurement was worse
// than the guess for two of them. The provisional label for Codex and
// Antigravity was "events", on the reasoning that a coarse stream is still a
// stream. Live runs say otherwise:
//
//   - Codex emits one item.completed per COMPLETE agent message. There are no
//     message deltas; its own feature list has none under development.
//   - Antigravity emits a whole agent response as a single text_delta when the
//     step goes ACTIVE. A one-word reply left the column empty for 73 seconds
//     and then painted at once.
//
// Both are therefore GranFinalOnly, which starts their columns in PhaseWaiting
// and renders the card that says no incremental output is coming. That card was
// built for a case we hoped would not arrive; it turns out to be two thirds of
// the room.
//
// Cursor is the first vendor to land on GranUnknown, and that is a deliberate
// refusal rather than an oversight. It has `--stream-partial-output`, whose help
// says "Stream partial output as individual text deltas", and the shipped
// bundle does write one assistant event per text chunk when it is set. Neither
// is a measurement. Antigravity is the standing warning here: its schema emits
// an event whose key is literally `text_delta`, and the whole reply arrived in
// one of them after 73 seconds of empty column. A name in a help string and an
// emit path in a bundle are both upstream of the thing that matters, which is
// what actually comes down the pipe — and that could not be observed, because
// the installed cursor-agent is not signed in.
//
// So no granularity word is printed for this column, and dispatch opens it in
// PhaseWaiting: if output does arrive incrementally the phase upgrades on the
// first chunk, which is honest in that direction only.
func granularityFor(v model.VendorID) Granularity {
	switch v {
	case model.VendorClaude:
		return GranTokens
	case model.VendorCodex, model.VendorAntigravity:
		return GranFinalOnly
	default:
		return GranUnknown
	}
}
