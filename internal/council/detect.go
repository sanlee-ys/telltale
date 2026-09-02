package council

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
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
	// nativeUnder is asked, for every path this candidate resolves to, whether
	// there is a NATIVE executable underneath it that would do the same job.
	//
	// It exists because "the entry point is a shim" and "this vendor cannot be
	// driven" turned out to be different claims. A shim that is a thin launcher
	// — one that picks a path and execs a real binary — can be stepped over,
	// and stepping over it removes the shell from the invocation entirely, which
	// is the whole of what the argv rule cares about. cursor-agent is exactly
	// that shape and was marked unusable on Windows for a shell it only ever
	// passed through.
	//
	// The contract is deliberately narrow: it returns the path to run, or ""
	// plus a sentence naming what it expected and did not find. It may never
	// return a path it has not stat'd — a resolved-but-absent binary would turn
	// a detection question into a failed turn, which is the trade this whole
	// file exists to refuse.
	nativeUnder func(found string) (binary, missing string)
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
			// That fact has not changed. What changed is what follows from it —
			// see nativeUnder below. argv + shim is still refused; the Windows
			// install simply turned out not to need the shim.
			stdinPrompt: false,
			// The unlock. cursor-agent.cmd is a launcher and nothing else, so
			// council runs what the launcher would have run.
			nativeUnder: cursorNodeUnderlay,
			// Reachable again only if the layout stops matching what the
			// launcher itself expects, and in that case the advice is real
			// rather than a shrug: the node the .cmd runs is a genuine
			// executable sitting in the install, and pointing the override at it
			// is exactly what nativeUnder would have done automatically.
			unusableHint: "point TELLTALE_COUNCIL_CURSOR_BIN at the bundled node.exe " +
				"beside this install's index.js — the .cmd runs nothing else",
		},
		{
			vendor:     model.VendorGrok,
			label:      "Grok",
			names:      []string{"grok"},
			knownPaths: grokKnownPaths(),
			envVar:     "TELLTALE_COUNCIL_GROK_BIN",
			// Verified argv-only by running it, not by reading the flag list:
			// `-p/--single` takes the prompt as its VALUE and there is no `-`
			// stdin sentinel. Safe anyway, because the installer drops a native
			// grok.exe rather than a shim — so classify() never reaches the
			// refusal, and no cmd.exe ever sees a brief.
			stdinPrompt: false,
		},
	}
}

// grokKnownPaths is where grok's own installer puts the CLI.
//
// The Windows entry is measured — it is where the binary sits on this machine
// and where `grok update` keeps it — rather than guessed. The POSIX entry is
// the same layout under the same dotdir and is NOT verified here, which is safe
// for the reason cursorKnownPaths states: a knownPath either exists or is
// skipped, so a wrong one cannot produce a wrong claim.
func grokKnownPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		return []string{filepath.Join(home, ".grok", "bin", "grok.exe")}
	}
	return []string{filepath.Join(home, ".grok", "bin", "grok")}
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

// cursorVersionDir matches the version directory names cursor-agent's own
// launcher is willing to run, and it is copied from that launcher rather than
// inferred from the one directory on this machine.
//
// Both forms are the vendor's: the legacy `YYYY.M.D-<commit>` and the newer
// `YYYY.M.D-HH-MM-SS-<commit>` that adds a build timestamp. Go's regexp has no
// PowerShell-ism to translate here; this is the same pattern, verbatim.
var cursorVersionDir = regexp.MustCompile(`^(\d{4})\.(\d{1,2})\.(\d{1,2})(-\d{2}-\d{2}-\d{2})?-[a-f0-9]+$`)

// cursorNodeUnderlay finds the native executable that a cursor-agent shim would
// have run, so council can run it instead.
//
// This is NOT this repo guessing at someone else's install layout. It is the
// algorithm the vendor's own launcher performs, transcribed. `cursor-agent.cmd`
// contains no logic at all — it hands its argv to `cursor-agent.ps1` — and that
// script's entire body is:
//
//	if (Test-Path "$scriptPath\node.exe") {
//	    & "$scriptPath\node.exe" "$scriptPath\index.js" $args; exit $LASTEXITCODE
//	}
//	$versionDir = Get-ChildItem -Path "$scriptPath\versions" -Directory |
//	    Where-Object { $_.Name -match '^\d{4}\.\d{1,2}\.\d{1,2}(-\d{2}-\d{2}-\d{2})?-[a-f0-9]+$' } |
//	    Sort-Object { Parse-VersionString $_.Name } -Descending | Select-Object -First 1
//	& "$scriptPath\versions\$versionName\node.exe" "$scriptPath\versions\$versionName\index.js" $args
//
// So the shim's whole job is to pick a directory and exec a bundled node
// against a JavaScript entry point. Doing that selection here deletes both
// cmd.exe and powershell.exe from the invocation, which is the only thing the
// argv rule ever cared about: `node.exe` is a real executable, Go's os/exec
// quotes its arguments itself, and a prompt containing a quote or an ampersand
// stops being a shell's problem. It is the same reason agy's argv transport is
// safe — not a new exception carved for this vendor.
//
// VERIFIED, on 2026-08-04 against 2026.07.23-e383d2b: run directly, from a
// directory unrelated to the install and with none of the environment the .ps1
// sets, `node.exe index.js --help` produced output byte-identical to
// `cursor-agent.cmd --help` (86 lines, diff clean), and `node.exe index.js
// status` reported the signed-in account. The bundle needs no cwd and no env.
//
// The version directory is re-resolved on every call, and Detect() is called
// once per room, so a room started after an auto-update picks up the new
// directory. A cached path is the failure this shape exists to avoid: it would
// go on pointing at a version directory the updater had already deleted, and
// the seat would fail at dispatch rather than at detection.
func cursorNodeUnderlay(found string) (binary, missing string) {
	// Already a node interpreter — an override pointing straight at one, or a
	// second pass. All it needs is its bundle beside it.
	if isNodeInterpreter(found) {
		bundle := vendors.CursorNodeBundle(found)
		if _, err := os.Stat(bundle); err != nil {
			return "", "found the node interpreter " + found +
				", but not the bundle it has to run: expected " + bundle
		}
		return found, ""
	}
	if kindOf(found) != KindShim {
		// The POSIX install is an extensionless script, which is already native
		// and already drivable. Nothing to step over.
		return found, ""
	}

	dir := filepath.Dir(found)
	// The launcher's own first branch: an install flattened into one directory.
	if b, ok := cursorNodePair(dir); ok {
		return b, ""
	}

	versions := filepath.Join(dir, "versions")
	entries, err := os.ReadDir(versions)
	if err != nil {
		return "", "found " + found + ", a shell shim whose launcher runs a bundled node, " +
			"but " + versions + " could not be read: " + err.Error()
	}
	newest := ""
	newestKey := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m := cursorVersionDir.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		// Parse-VersionString's arithmetic: zero-pad month and day, concatenate
		// with the year, compare as an integer. The build timestamp and the
		// commit hash take no part in the ordering upstream, and they take none
		// here either.
		key := atoi(m[1])*10000 + atoi(m[2])*100 + atoi(m[3])
		// Ties are broken by name, descending. The launcher leaves them to
		// Sort-Object's stability, which is to say to directory order — fine for
		// a script run once, not fine for a detection whose answer must be the
		// same twice in a row.
		if key > newestKey || (key == newestKey && e.Name() > newest) {
			newest, newestKey = e.Name(), key
		}
	}
	if newest == "" {
		return "", "found " + found + ", a shell shim whose launcher runs a bundled node, " +
			"but no version directory under " + versions + " matches the layout that launcher expects"
	}
	b, ok := cursorNodePair(filepath.Join(versions, newest))
	if !ok {
		return "", "found " + found + ", a shell shim whose launcher runs a bundled node, " +
			"but " + filepath.Join(versions, newest) + " holds no node.exe beside an index.js"
	}
	return b, ""
}

// cursorNodePair reports the node interpreter in dir, but only when the bundle
// it would be given is there too. Half a pair is not a resolution: a node with
// no index.js beside it would start and immediately fail to find its entry
// point, on every turn, with an error about a JavaScript file the user never
// asked about.
func cursorNodePair(dir string) (string, bool) {
	node := filepath.Join(dir, "node.exe")
	if _, err := os.Stat(node); err != nil {
		return "", false
	}
	if _, err := os.Stat(vendors.CursorNodeBundle(node)); err != nil {
		return "", false
	}
	return node, true
}

// isNodeInterpreter is the same test the vendor's launcher makes, by name.
func isNodeInterpreter(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return strings.TrimSuffix(base, filepath.Ext(base)) == "node"
}

// atoi is strconv.Atoi with the error dropped, which is safe here and only here:
// every string it sees came out of a regexp group of the form \d{1,4}.
func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
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
	// Asked BEFORE the shim refusal, because it is the question the refusal was
	// missing: not "is this path a shim" but "is a shell actually going to be
	// involved". A launcher that only picks a path and execs a real binary
	// answers no, and council steps over it.
	//
	// A vendor with no underlay is unaffected — codex's .cmd is npm's wrapper
	// with real work in it, not a two-line launcher, and it stays on the stdin
	// transport that made it safe.
	if c.nativeUnder != nil {
		under, missing := c.nativeUnder(info.Binary)
		if under == "" {
			// The layout stopped matching what the vendor's own launcher
			// expects. Named rather than guessed around: a resolution that
			// silently fell back to the shim would put prompt text through
			// cmd.exe, and one that fell back to a stale path would fail at
			// dispatch instead of here.
			info.Avail = AvailUnusable
			info.Note = missing + " — " + c.hint()
			return info
		}
		if under != info.Binary {
			info.Source = info.Source + ", stepping over its launcher to the bundled " +
				filepath.Base(under) + " it runs"
			info.Binary = under
			info.Kind = kindOf(under)
		}
	}

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
// difference rather than a nicer word: it asks first, about everything that
// changes anything. Being a live process is what MAKES that possible — a
// permission request needs somewhere to arrive and somewhere for the answer to
// go back — but it is not what earns the badge, and the Cursor seat is now the
// case that separates the two: it is a live ACP process, it can be asked, and it
// does not ask about edits. See canGate.
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
		return SandboxClaim{Level: SandboxWrite, Detail: writeDetail(v, gated)}
	}
	return sandboxFor(v, windows)
}

// writeDetail is the sentence behind WRITES.
//
// Shared by three of the four seats, and that uniformity is the point made
// above: once the answer to "how much read-only did we manage to ask for" is
// "none", grading them would imply a safety difference that does not exist.
//
// One seat needs a clause the others do not, and it is an addition rather than a
// grade. The Cursor column really does raise approval cards in this posture —
// its ACP server asks about shell commands its own allowlist does not cover, and
// the room answers — so a user who sees a card there and finds nothing in the
// posture page explaining it would reasonably conclude the room was doing
// something it had not told them about.
//
// asking is in the signature for the same reason gatedDetail takes `hooked`: the
// clause is a claim about what this room will DO, and with `a` pressed the room
// answers those requests itself and draws no card at all. Promising cards to the
// one user who has switched them off would be a false claim in the branch where
// it matters most.
//
// Either way the clause ends on what the cards do NOT cover, which is the part
// that survives both branches: this seat wrote a file with no card, measured
// twice, in a directory it had never been told to trust.
func writeDetail(v model.VendorID, asking bool) string {
	const shared = "this column may edit and run things in the workspace above. " +
		"Containment is that directory, not a flag — point council at a " +
		"worktree if that matters. --read opens a room that only talks"
	switch v {
	case model.VendorCursor:
		const limit = " It does NOT ask about file edits — measured twice, it wrote a file " +
			"with no card, in a directory it had never been told to trust. The boundary " +
			"is still the directory, not the cards"
		if asking {
			return shared + ". This seat sometimes asks first, and the room answers: its " +
				"ACP server raises an approval card for a shell command its own allowlist " +
				"does not cover." + limit
		}
		return shared + ". This seat's ACP server does ask about some shell commands, and " +
			"with asking off the room answers yes for you without drawing a card." + limit
	case model.VendorCodex, model.VendorGrok, model.VendorAntigravity:
		// The three seats that moved to long-lived processes on 2026-09-02
		// (design.md §9.57), each on a documentation read rather than a live
		// run. The shape words come first, in the vocabulary the column
		// header already speaks, and "unmeasured" is in them by construction:
		// seatShape refuses to spell the word "measured" for a seat nobody has
		// driven, so a badge cannot claim a gate it has not seen.
		return seatShape(v, false) + ": " + shared + ". " + liveSeatDetail(v, asking)
	}
	return shared
}

// seatShape is the badge's account of WHICH process shape a seat is running
// as, and whether that shape has been driven live.
//
// Three words, dot-separated, in the header's existing vocabulary: the
// protocol (`app-server`, `acp`, `stream-json`, or the batch invocation it
// fell back to), whether the seat can be asked (`asks` / `unasked`), and the
// evidence class with the version it was read or measured at. The evidence
// word is the load-bearing one and it is chosen by CONSTRUCTION rather than by
// hand: the live shapes were built from vendor documentation on 2026-09-02
// (design.md §9.57) and nothing here has watched one run, so every live entry
// says `unmeasured at <version>`; the fallback entries name the batch
// invocation each seat was measured driving and the build it was measured
// at. A later change that drives a live shape on the reference box may flip
// exactly one word, and the checklist in §9.57 is the price of flipping it.
//
// fellBack is whether the room retreated to the measured batch adapter after
// the live handshake failed (vendors.LiveFallback). The room's posture badge
// today renders the live shape only; the fallback branch is here so the badge
// has one place to read from once the room carries that state, and so a test
// can pin both spellings now.
func seatShape(v model.VendorID, fellBack bool) string {
	switch v {
	case model.VendorCodex:
		if fellBack {
			return "exec · unasked · fallback, measured at 0.149.1"
		}
		return "app-server · asks · unmeasured at 0.152.1"
	case model.VendorGrok:
		if fellBack {
			return "single · unasked · fallback, measured at 1.0.4"
		}
		return "acp · asks · unmeasured at 1.0.13"
	case model.VendorAntigravity:
		if fellBack {
			return "print · unasked · fallback, measured at 1.1.13"
		}
		return "stream-json · unasked · unmeasured at 1.1.24"
	}
	return ""
}

// liveSeatDetail is the clause behind seatShape for a write posture: what the
// seat's live shape can ask, what it cannot, and that none of it has been
// watched. The asking argument matters for the two seats that can be asked,
// for the reason writeDetail states above: promising cards to the one user who
// has switched them off would be a false claim in the branch where it matters
// most.
func liveSeatDetail(v model.VendorID, asking bool) string {
	const boundary = " The boundary is still the directory, not the cards, and nothing " +
		"on this shape has been watched running: read from the vendor's docs, " +
		"not from a live turn"
	switch v {
	case model.VendorCodex:
		const shape = "This seat is one live `codex app-server` process, opened with " +
			"approvalPolicy on-request, so the vendor asks when it wants more than " +
			"the workspace-write sandbox allows"
		if asking {
			return shape + " — and that request is an approval card here, answered down " +
				"the same pipe. It does not ask about a write inside the workspace." + boundary +
				". If the app-server handshake is refused the seat falls back to " +
				"`codex exec --json`, which asks about nothing"
		}
		return shape + ", and with asking off the room answers yes for you without " +
			"drawing a card. It does not ask about a write inside the workspace." + boundary +
			". If the app-server handshake is refused the seat falls back to " +
			"`codex exec --json`, which asks about nothing"
	case model.VendorGrok:
		const shape = "This seat is one live `grok agent stdio` ACP process"
		if asking {
			return shape + ", and a permission request it raises is an approval card " +
				"here, answered down the same pipe. Whether it raises one before a " +
				"write at all is unknown: on the measured invocation (`--single`) it " +
				"wrote with no request under every flag tried." + boundary +
				". If the ACP handshake is refused the seat falls back to `--single`, " +
				"which asks about nothing and is where its cost figure comes from"
		}
		return shape + ", and with asking off the room answers yes to any permission " +
			"request without drawing a card. Whether it raises one before a write " +
			"at all is unknown." + boundary +
			". If the ACP handshake is refused the seat falls back to `--single`, " +
			"which asks about nothing and is where its cost figure comes from"
	case model.VendorAntigravity:
		return "This seat is one live `agy --input-format stream-json` process that " +
			"stays open across turns, and NOTHING on that channel asks: the envelope " +
			"carries prompts, a hook that answers `ask` has nobody in print mode to " +
			"answer it, and every write is unasked. The workspace above is the " +
			"containment, exactly as on the measured `-p` invocation it falls back " +
			"to." + boundary
	}
	return ""
}

// gatedDetail is what the gated column defends, and the two branches differ in
// what they CLAIM rather than in tone.
//
// The shared half is the gate itself, and it got stronger on 2026-08-12. It
// used to end on a carve-out — shell commands the CLI classifies read-only were
// approved before the callback, so `git status` and every `Read` went past
// unseen. Council's own PreToolUse hook answers "ask" with no matcher, so every
// call now reaches the room; the ones that change nothing are answered by
// council rather than by the CLI, which is a decision the operator can read in
// this repo instead of one buried in a vendor's classifier.
//
// The half that varies is WHICH of the two gates the seat got, and this is the
// sentence that had to change. Until 2026-08-12 both branches said the
// operator's permission rules were dropped, because they were: the seat passed
// --setting-sources "". A wired hook keeps them — their deny rules, their
// user-level commands and their own hooks are in force again — and it is the
// ask hook, not the missing rules, that holds the gate. An unwired hook falls
// back to dropping the sources, which still gates and still says so.
//
// Both facts are read off a file on disk rather than off an intention, which is
// the property that survived the rewrite: a temp directory that could not be
// created and a binary that could not locate itself end in the same place, and
// the column says which gate it actually got.
func gatedDetail(hooked bool) string {
	const shared = "this column asks before every tool " +
		"call: y approves, n denies, and nothing runs until you answer. Reads, " +
		"globs and routine shell commands are answered yes for you, by council " +
		"rather than by the CLI"
	if hooked {
		return shared + ". Your own settings stay loaded for this seat — your " +
			"deny rules and your own hooks are in force — because council's own " +
			"hook asks first and an ask beats an allow rule underneath it"
	}
	return shared + ". Council's own gate hook could not be wired here, so this " +
		"seat drops your settings instead — your allow rules would otherwise " +
		"approve calls before the gate saw them, and your deny rules and hooks " +
		"go with them"
}

// canGate reports whether a seat can be asked to ask about EVERYTHING that
// changes something.
//
// It used to read "is this vendor drivable as a live process", off the registry,
// and that was right for as long as those two questions had the same answer. It
// stopped being right the moment the Cursor seat became a live ACP process
// (§9.36), and it is worth being precise about how, because "Cursor cannot ask"
// would be just as false as "Cursor gates":
//
//   - It CAN ask. `session/request_permission` blocks the vendor until an answer
//     goes back down the same pipe, and both branches were measured — a rejection
//     left the command unrun.
//   - It does not ask about EDITS. Asked to create a file, in a directory it had
//     never been told to trust, it wrote the file and raised nothing. Twice.
//
// The `gated` badge's whole sentence is "nothing that changes anything runs
// without your keystroke". A seat that writes files silently and asks about
// shell commands cannot carry it — so it keeps WRITES, and the fact that it asks
// about some things is stated in that badge's detail rather than promoted into a
// claim it would break. Council still ANSWERS every request: an unanswered one
// blocks the vendor forever, which is a column that never finishes.
//
// A list rather than an interface assertion, therefore, because what qualifies a
// seat here is a MEASUREMENT of its coverage and not a property of its type.
//
// Two more seats can now be ASKED and neither is here, on the same rule
// (2026-09-02, design.md §9.57). The codex app-server seat routes
// `item/*/requestApproval` through the room's card and the grok ACP seat
// routes `session/request_permission` the same way — so "the gate can ask"
// both, in the sense that a request the vendor raises reaches a person. What
// neither has is a coverage measurement: no live run on either path has
// produced a request at all, let alone shown one raised before every write.
// Their write badges say `asks · unmeasured` where the argument lives, and
// stay `WRITES`. The day a capture shows one of them asking before every
// change, this list is the one line to change and §9.57 is the record to
// update.
func canGate(v model.VendorID) bool {
	return v == model.VendorClaude
}

// sandboxFor is the read-posture claim, stated per vendor rather than as a
// blanket promise (ADR-008 §3).
//
// These are the claims the scaffold renders; what backs them arrives with each
// vendor's adapter. Codex is the seat where the OS most changed the answer:
// `-s read-only` is OS-enforced everywhere now, but the Windows branch said
// `unsandboxed` until 2026-08-29, because until codex-cli 0.149.1 the mode
// failed every process spawn there and council could not pass it at all.
// Each branch may claim only what was measured on it — claiming a posture we
// have not seen would be the exact overstatement this product exists to
// refuse, in either direction.
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
			// Reordered 2026-08-04 so the first clause answers "so what?" rather
			// than naming a mechanism. Every factual clause is unchanged and none
			// is softened: the verification is still stated as reading the
			// session's own tool list, and the residual — a deny list cannot
			// cover a tool that does not exist yet — is still the last word.
			Detail: "this seat has no write or shell tools in its session, so it cannot " +
				"edit your files. Verified by reading what the session reported about " +
				"itself, not by trusting a flag: the named write/exec tools are denied " +
				"and MCP servers are dropped. The residual is that a deny list cannot " +
				"cover a tool a future release adds",
		}
	case model.VendorCodex:
		if windows {
			return SandboxClaim{
				// SandboxNone until 2026-08-29, and the level moved on a
				// measurement rather than a release note. At codex-cli 0.146.0
				// both sandboxed modes failed every process spawn on Windows,
				// so council passed -s danger-full-access and this branch said
				// `unsandboxed` — the honest word for a seat with no sandbox at
				// all. Re-measured 2026-08-29 at codex-cli 0.149.1: the mode
				// enforces now. A shell write under -s read-only came back
				// "Access is denied." at exit 1 with no file on disk, a read
				// ran clean, and the resume path's -c override was measured
				// enforcing the same. Council passes -s read-only here again,
				// so the badge may say so. vendors/codex.go carries the full
				// capture, including the caveat the detail below names.
				Level: SandboxEnforced,
				// The seat moved to `codex app-server` on 2026-09-02 (§9.57)
				// and the level HOLDS on this branch because the same 0.149.1
				// session measured the sandbox on that path too (§9.50): a
				// read-only thread's shell write through cmd.exe came back
				// "Access is denied." at exit 1 with no file on disk, and
				// cmd.exe's own error on the next call proved it ran INSIDE
				// the sandbox. The liveness residual is SHARPER there, and the
				// detail says so rather than carrying the exec seat's milder
				// sentence: the router's pwsh could not start and the model
				// abandoned two of three read turns. The installed build is
				// 0.152.1 and neither path has been driven at it.
				Detail: seatShape(model.VendorCodex, false) + ": sandbox read-only on the " +
					"app-server thread, applied by the vendor's own Windows sandbox — " +
					"measured 2026-08-29 at codex-cli 0.149.1 on that path: a shell write " +
					"was denied with no file on disk. The residual is liveness, not " +
					"safety, and it is worse here than on the `codex exec` fallback: the " +
					"sandbox could not spawn this machine's PowerShell, and in two of " +
					"three read turns the model gave up rather than retrying through " +
					"cmd.exe. Nothing has been driven at 0.152.1",
			}
		}
		return SandboxClaim{
			// REQUESTED, not enforced, since 2026-09-02 — and this is the one
			// badge the seat move LOWERED. The macOS measurement behind the old
			// `ro:enforced` was `codex exec -s read-only` (2026-08-05, 0.146.0),
			// and every app-server arm ran on Windows (§9.50, PARITY.md). The
			// seatbelt is the same codex core either way, but "the same
			// mechanism" is an inference and §9.50's rule is that a seat move
			// re-measures rather than inherits. The fallback still earns the
			// old word; this shape has not.
			Level: SandboxRequested,
			Detail: seatShape(model.VendorCodex, false) + ": sandbox read-only requested " +
				"on the app-server thread. The OS-level sandbox behind that word was " +
				"measured on macOS through `codex exec -s read-only` and never through " +
				"app-server, which has only ever been driven on Windows — so this " +
				"column asks for the posture and cannot yet say it saw it held. The " +
				"`codex exec` fallback keeps the measured enforcement",
		}
	case model.VendorAntigravity:
		return SandboxClaim{
			Level: SandboxNone,
			// Refuted, not unverified. Asked to write a file under both flags,
			// it wrote the file; the reported permission mode and tool list
			// were identical to a run without them.
			//
			// The last clause used to read "The flags are still passed; they do
			// not restrict it". They are not passed any more (ADR-008,
			// seventeenth amendment) — their only measured effect was killing a
			// turn outright — and a detail claiming council asks for something
			// it has stopped asking for would be a false claim about this
			// tool's own behaviour, which is the one kind this file has no
			// excuse for.
			Detail: seatShape(model.VendorAntigravity, false) + ": treat this column as " +
				"able to change your files, and that is " +
				"MEASURED rather than assumed: asked to write a file under both " +
				"--mode plan and --sandbox, it wrote the file, and its reported " +
				"permission mode and tool list were identical to a run without them. " +
				"Council no longer passes either flag: their only observed effect was " +
				"a turn that died with an empty column when the agent reached for a " +
				"shell. The workspace above is the containment, not a flag. Since " +
				"2026-09-02 the seat is one `agy --input-format stream-json` process " +
				"kept open across turns, read from the 1.1.24 docs and not yet driven; " +
				"nothing on that channel restricts it either",
		}
	case model.VendorCursor:
		// Re-measured end to end on 2026-08-08 against the ACP server (§9.36),
		// because §9.33's third fork said outright that every claim on this seat
		// had been measured against a surface it no longer uses. The badge LEVEL
		// is unchanged and the reasons under it are almost all new.
		//
		// Two things got BETTER, and neither is enough to move the level:
		//
		//   - The read posture is now `session/set_mode` with modeId `plan`,
		//     accepted by the server, and asked to create a file the seat refused
		//     — "Plan mode is still on, so I can't create the file yet" — and no
		//     file landed. That is strictly better evidence than print mode ever
		//     produced, where the same mode was measured DISPATCHING `cat` and
		//     `ls -1` as shell calls. It is still one trial, and the refusal is
		//     worded as the model obeying its mode rather than as a layer stopping
		//     it, so `requested` is what it has earned.
		//   - A permission request that arrives in this posture is REFUSED by the
		//     adapter without troubling the user. A read-posture seat asking to
		//     change something is not a question; it is already answered.
		//
		// One thing got WORSE, and it is the sentence a user most needs:
		//
		//   - Workspace trust does not apply on this path. Print mode refuses a
		//     turn in a directory it has not been told to trust — "⚠ Workspace
		//     Trust Required" — and the ACP server, in THE SAME directory, wrote a
		//     file into it. That is not a flag council chose; there is no trust
		//     parameter in the protocol. The screen this seat used to have on the
		//     way in is gone, and the badge says so rather than letting a reader
		//     carry over an assumption from the old path.
		//
		// The sandbox request is gone with the flag: ACP takes no sandbox
		// parameter, on any OS, which is why this branch is no longer split by
		// platform. On Windows nothing was lost — `--sandbox enabled` was measured
		// killing the turn there, so it was already not passed. On macOS and Linux
		// what was lost is a REQUEST whose enforcement was never observed.
		return SandboxClaim{
			Level: SandboxRequested,
			Detail: "asked for, and one trial says it held: this seat is put in the ACP " +
				"server's `plan` mode, and asked to create a file it declined and created " +
				"nothing. Treat it as able to run things anyway — that is one trial, and " +
				"a mode the model obeys is not a layer that stops it. Two things this " +
				"column can NOT claim: there is no sandbox request at all any more (the " +
				"ACP protocol has no such parameter, on any OS), and workspace trust does " +
				"not apply here — the same directory that print mode refused to run in " +
				"was written to over ACP without a prompt",
		}
	case model.VendorGrok:
		return SandboxClaim{
			Level: SandboxNone,
			// Refuted, not unverified, and refuted twice over — which is why
			// this reads like the Antigravity branch rather than like the
			// Cursor one. Asked to create a file under --permission-mode plan,
			// it called its `write` tool, reported the call "completed", said
			// so, and the file was on disk afterwards. The control run without
			// the flag also wrote its file. The only difference between the two
			// arms was which write tool the model picked.
			//
			// The --sandbox clause is a DIFFERENT kind of evidence and is worth
			// its own sentence: that flag was never refuted, it was shown to be
			// unobservable. Handed a profile name that cannot exist it neither
			// errored nor warned, so council has no way to tell a real profile
			// from a typo, and asks for nothing rather than putting a word in
			// this badge that the CLI may never have read.
			Detail: seatShape(model.VendorGrok, false) + ": treat this column as able " +
				"to change your files, and that is " +
				"MEASURED rather than assumed: asked to write a file under " +
				"--permission-mode plan, it wrote the file, exactly as the run " +
				"without it did. Council passes neither that nor --sandbox — " +
				"--sandbox silently ACCEPTS a profile name that does not exist, so " +
				"nothing council asked of it could be observed. The workspace above " +
				"is the containment, not a flag. Since 2026-09-02 the seat is one " +
				"`grok agent stdio` ACP process, read from the 1.0.13 docs and not yet " +
				"driven; a permission request it raises in this posture is refused by " +
				"the room itself, and whether it raises one before a write is unknown",
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
// — named in the column header, with `final only` beside it, and one calm line
// in the body saying the reply arrives whole. That phase was built for a case we
// hoped would not arrive; it turns out to be two thirds of the room, which is
// exactly why its explanation lives on the help page rather than in the body of
// every waiting turn (§9.14).
//
// Cursor was the first vendor to land on GranUnknown, was promoted to
// GranTokens the only way this repo promotes anything — someone watched the pipe
// — and has now had that promotion RE-EARNED against a different protocol, which
// is the part worth reading before touching this line.
//
// The print-mode measurement (2026-08-04) really was token-level: a one-word
// reply came down as `"P"` then `"ONG"`, and a sentence as "I", " said", " P",
// "ONG", ".". That surface is gone. The word could have been inherited; §9.33's
// third fork says explicitly that it must not be, so it was measured again.
//
// ACP, 2026-08-08, a 300-word reply: 24 `agent_message_chunk` notifications over
// 2.6 seconds — about 95 characters each, about nine a second. Coarser than
// print mode's tokens and finer, in time, than the Claude seat that already
// carries this word: §9.7 measured Claude's deltas at ~80 characters, about
// three a second, and flagged in the same breath that "tokens" overstates them.
//
// So the word stays, with its existing looseness and no new looseness: this
// column streams text as the vendor writes it, in the same units and faster than
// the seat beside it. If §9.7's flagged overstatement is ever fixed, it is one
// change to one word on both seats at once, which is exactly why it was left as
// a separate change to a separate surface rather than quietly corrected here.
//
// The delta trap this capture used to carry is GONE with the protocol: ACP
// repeats nothing, and there is no `model_call_id` in its traffic. See
// vendors/cursoracp.go.
// A fifth seat now carries GranTokens on the STRONGEST evidence in the room,
// which is worth recording precisely because §9.7's flagged overstatement
// above is the reason to be careful with the word. Grok's captured deltas are
// "I'll", " read", " `", "notes", ".txt" — actual tokens, not the ~80-character
// chunks the Claude seat calls tokens nor the ~95-character ACP chunks the
// Cursor seat does. Its `thought` stream is dropped (see vendors/grok.go), so
// a turn that reasons for a long time before speaking still shows an empty
// column while it reasons; what this word promises is that once the answer
// starts, it arrives as it is written.
func granularityFor(v model.VendorID) Granularity {
	switch v {
	case model.VendorClaude, model.VendorCursor, model.VendorGrok:
		return GranTokens
	case model.VendorCodex, model.VendorAntigravity:
		return GranFinalOnly
	default:
		return GranUnknown
	}
}
