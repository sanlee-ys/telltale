package council

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	// Note explains an Avail that is not Installed, in the words the card shows.
	Note string
}

// candidate is a vendor council knows how to seat.
//
// Cursor is deliberately absent. It ships a headless CLI as a product
// (cursor-agent), but the `cursor` binary on PATH is the editor launcher, and
// seating a vendor against a binary that answers to a different command would
// produce a column that fails in a confusing way rather than an honest empty
// seat. When cursor-agent is installed, it becomes one more entry here plus one
// adapter (ADR-008 §7).
type candidate struct {
	vendor model.VendorID
	label  string
	// names are the command names to try, in order.
	names []string
	// envVar overrides the resolved path entirely, so a user whose vendor is
	// installed somewhere unusual — or who wants to bypass an npm shim by
	// pointing straight at the vendored executable — can say so.
	envVar string
	// stdinPrompt reports that this vendor accepts its prompt on stdin, which
	// is what makes a shim safe to drive.
	stdinPrompt bool
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
		return classify(info, c)
	}

	info.Avail = AvailNotInstalled
	info.Note = "not found on PATH (looked for " + strings.Join(c.names, ", ") + ")"
	return info
}

// classify decides whether a resolved binary is one council will drive.
func classify(info VendorInfo, c candidate) VendorInfo {
	if info.Kind == KindShim && !c.stdinPrompt {
		// The refusal is the point. Driving this would mean handing a prompt to
		// cmd.exe, and a prompt containing a quote or an ampersand would either
		// break or, worse, execute as something else.
		info.Avail = AvailUnusable
		info.Note = "resolves to a shell shim (" + filepath.Base(info.Binary) +
			") and takes its prompt as an argument; set " + c.envVar +
			" to the real executable"
		return info
	}
	info.Avail = AvailInstalled
	return info
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
