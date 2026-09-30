package vendors

import (
	"errors"
	"fmt"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The model request (telltale council --model seat=model).
//
// This file is the REQUEST half of the seat's model and nothing else. It puts
// the name the operator typed on the vendor's own argv, at the position that
// vendor's CLI parses it. It makes no claim that the vendor honours the
// request. That claim belongs to the READ half (internal/council/seatmodel.go),
// which reads the model the vendor's own session record names after a turn. A
// room that showed only the request would be a gauge that reads the dial it
// was set to, and an auto route that overrode the request would never show.
//
// Every flag below was read off the vendor's own --help on the operator's
// Windows 11 machine on 2026-09-30, at the build named beside it. The help
// line is quoted verbatim so that a reader can compare it with a later build.
// A help line is a statement about what the CLI PARSES. It is not a
// measurement of which model runs, and nothing here says otherwise.

// ModelFlag is one seat's measured model flag.
type ModelFlag struct {
	// Spelling is the flag as council passes it.
	Spelling string
	// Help is the vendor's own --help line for the flag, verbatim.
	Help string
	// MeasuredAt names the build and the date the help line was read at.
	MeasuredAt string
}

// modelFlags is every seat whose model flag was measured. A seat that is not
// in this table has no model request at all, and --model refuses it by name.
//
//   - claude: `claude --help`, Claude Code 2.1.273. One option on the root
//     command. Council puts it first, before every other option.
//   - codex: `codex exec --help`, `codex exec resume --help` and
//     `codex app-server --help`, codex-cli 0.151.0. `app-server` has NO -m
//     option. All three subcommands list `-c, --config <key=value>` with the
//     example `-c model="o3"`, so council uses that one channel on every
//     codex shape. codex.go already carries its sandbox override on -c for
//     the same reason: `exec resume` rejects some options that `exec` takes.
//   - agy: `agy --help`, Antigravity CLI 1.2.14 (the newest entry of
//     `agy changelog`; this CLI prints no --version). A Go-style flag. It
//     must come before -p, or -p takes it as prompt text (agy.go), so council
//     puts it first.
//   - grok: `grok --help` and `grok agent --help`, grok 1.0.13. The live
//     seat runs `grok agent stdio`, and `grok agent stdio --help` lists no
//     model option. `-m, --model` is an option of `agent`, so it goes between
//     `agent` and `stdio`. The batch fallback takes the root option.
//
// Deliberately absent: cursor. `cursor-agent` was not installed on the
// machine where these flags were read (the Cursor subscription ended
// 2026-09-03), so no help line exists to quote. A request for that seat is
// refused with ErrModelNotSupported rather than passed on a guessed spelling.
var modelFlags = map[model.VendorID]ModelFlag{
	model.VendorClaude: {
		Spelling:   "--model",
		Help:       "--model <model>  Model for the current session. Provide an alias for the latest model (e.g. 'fable', 'opus', or 'sonnet') or a model's full name (e.g. 'claude-fable-5').",
		MeasuredAt: "Claude Code 2.1.273, 2026-09-30",
	},
	model.VendorCodex: {
		Spelling:   "-c model=",
		Help:       "-c, --config <key=value>  Override a configuration value that would otherwise be loaded from `~/.codex/config.toml`. [...] Examples: - `-c model=\"o3\"`",
		MeasuredAt: "codex-cli 0.151.0, 2026-09-30",
	},
	model.VendorAntigravity: {
		Spelling:   "--model",
		Help:       "--model  Model for the current CLI session",
		MeasuredAt: "agy 1.2.14, 2026-09-30",
	},
	model.VendorGrok: {
		Spelling:   "--model",
		Help:       "-m, --model <MODEL>  Model ID to use",
		MeasuredAt: "grok 1.0.13, 2026-09-30",
	},
}

// ErrModelNotSupported is the refusal for a seat with no measured model flag.
//
// A named error, and it is returned before the room opens. The other choice
// was to accept the request and not pass it, and that is the failure this
// feature exists to prevent: the operator would believe a seat runs a model
// that nothing asked it for.
var ErrModelNotSupported = errors.New("this seat has no measured model flag")

// ErrModelName is the refusal for a model name council will not put on argv.
var ErrModelName = errors.New("a model name uses only letters, digits and . _ - : / @ +, and does not start with -")

// ModelFlagFor returns the measured model flag of one seat.
func ModelFlagFor(id model.VendorID) (ModelFlag, error) {
	f, ok := modelFlags[id]
	if !ok {
		return ModelFlag{}, fmt.Errorf("%s: %w", id, ErrModelNotSupported)
	}
	return f, nil
}

// ValidModelName checks a model name before it goes on argv.
//
// The allowed set is small on purpose. Codex can resolve to a .cmd shim, and
// cmd.exe parses its argument line again (runner.ErrShellShimWithArgvPrompt).
// A leading `-` would read as a flag to every one of these parsers. The names in
// the help examples (`opus`, `claude-fable-5`, `o3`) fit in this set. A name
// that does not fit is refused by name, and a later change can widen the set
// when a real model id needs it.
func ValidModelName(name string) error {
	if name == "" || name[0] == '-' {
		return ErrModelName
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-', r == ':', r == '/', r == '@', r == '+':
		default:
			return ErrModelName
		}
	}
	return nil
}

// WithModel returns spec with the model request put where the seat's CLI
// parses it. An empty request returns spec unchanged, which is the vendor's
// own default and the room as it was before this flag existed.
//
// It never aliases spec.Args. A spec is built fresh per spawn today, but a
// shared backing array is how one seat's argv would come to change another's.
func WithModel(spec runner.Spec, requested string) (runner.Spec, error) {
	if requested == "" {
		return spec, nil
	}
	if err := ValidModelName(requested); err != nil {
		return runner.Spec{}, fmt.Errorf("%s model %q: %w", spec.Vendor, requested, err)
	}
	if _, err := ModelFlagFor(spec.Vendor); err != nil {
		return runner.Spec{}, err
	}
	args := append([]string(nil), spec.Args...)
	switch spec.Vendor {
	case model.VendorClaude, model.VendorAntigravity:
		args = insertArgs(args, 0, "--model", requested)
	case model.VendorCodex:
		// The TOML string form from the help's own example. A bare word also
		// parses (the help says an unparsable value is taken as a literal), but
		// a name such as `1.5` would then parse as a number.
		kv := []string{"-c", `model="` + requested + `"`}
		if n := len(args); n > 0 && args[n-1] == "-" {
			// `-` is codex's stdin prompt sentinel and it must stay last
			// (codex.go), so the override goes in front of it.
			args = insertArgs(args, n-1, kv...)
		} else {
			args = append(args, kv...)
		}
	case model.VendorGrok:
		if len(args) > 0 && args[0] == "agent" {
			args = insertArgs(args, 1, "--model", requested)
		} else {
			args = insertArgs(args, 0, "--model", requested)
		}
	default:
		// Unreachable while modelFlags and this switch name the same seats.
		// TestEveryMeasuredModelFlagHasAPlacement pins that they do.
		return runner.Spec{}, fmt.Errorf("%s: %w", spec.Vendor, ErrModelNotSupported)
	}
	spec.Args = args
	return spec, nil
}

// insertArgs puts vals into args at index i.
func insertArgs(args []string, i int, vals ...string) []string {
	out := make([]string, 0, len(args)+len(vals))
	out = append(out, args[:i]...)
	out = append(out, vals...)
	return append(out, args[i:]...)
}
