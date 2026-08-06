# telltale — contributor contract

This file is the convention layer a fresh session cannot get from Go itself. Read
`README.md`, `STATE.md`, `PARITY.md` and `docs/design.md` too — this file is short on
purpose and defers to them for the product's own claims.

## Build and test

```
go vet ./...
go test ./...
go build -o telltale.exe ./cmd/telltale
```

That is the whole CI gate (`.github/workflows/ci.yml`), which runs on `windows-latest`
— **Windows is the primary target (ADR-002)**, not an afterthought platform. CI then
smoke-tests the *built binary* against two fixtures and asserts honesty properties
directly on stdout, e.g.:

```powershell
if ($out -match '5h|7d') { throw "honest-gauge violation: quota rendered without rate_limits: $out" }
```

If you touch a statusline segment, expect a similar assertion to exist or to be worth
adding — this repo tests the rendered string, not just that the code ran.

## Golden tests: the actual workflow and its traps

`internal/council` and `internal/hud` render to fixed-width strings and diff them
against `testdata/golden/*.txt`. Both packages define the same `-update` flag:

```
go test ./internal/council -update
go test ./internal/hud -update
```

Traps, in order of how often they bite:

- **Read the diff before you regenerate.** `-update` overwrites the golden with
  whatever `Render` currently produces — including a bug you just introduced. A
  golden that changed is a claim that the room's *appearance* changed; look at
  what actually moved before you accept it.
- **Goldens render with `PlainStyles()`** (or `GlyphsFor(true)` for the ASCII
  variant) — the identity style set where every `Render` is a no-op — specifically
  so the bytes don't depend on the CI terminal's colour profile. Colour itself is
  asserted separately (e.g. `TestPhaseColors`). Don't add a golden that embeds
  ANSI escapes.
- **`Render` must stay pure over its `State`** — no `time.Now()`, no filesystem, no
  env reads inside `Render`. Time comes in as `State.Now`, stamped once when a tick
  arrives. `TestRenderIsPure` / `TestElapsedIsPureOverState` guard this; breaking
  it makes goldens flaky in a way that only shows up in CI.
- **`fit`, not `padRight`, on any line that can carry style.** Body lines in
  council can contain ANSI escapes (outcome marks); `padRight` truncates rune by
  rune and will cut through an escape sequence. Goldens render `PlainStyles` and
  are blind to that bug — it only shows up in the real terminal. This is called
  out explicitly in `internal/council/view.go` (search "ANSI trap").
- One golden file per named scenario (`activity.txt`, `waiting-vs-streaming.txt`,
  `zero-vs-absent.txt`, …). The name says what property it pins down — add a new
  golden rather than overloading an existing one with an unrelated case.

## Commit / PR voice

Lowercase, declarative, describing the **behavior change from the user's side** —
never a Conventional-Commits `feat(x): add y` label. Real examples from merged PRs:

- `council: the gate reads the command, not the punctuation`
- `council: a hiccup no longer costs the whole conversation`
- `council: the room writes by default, and says which hop is driving it`
- `council: -@claude addresses everyone but that seat`
- `docs: a pickup doc that cannot go stale, and a parity file that names what was measured`
- `fix-macos-env-record` / `Correct the macOS verification environment: Intel x86_64, macOS 26.5.2` (plain sentence, still describes the fix, not the mechanism)

The title says *what a user now sees or can do*, not which function changed. A
handful of older merged PRs slipped into `feat(council): ...` / `fix(council): ...`
— treat those as the exception, not the model to copy.

## The honesty rules this project is built on (ADR-001)

This is the thing the whole codebase optimizes for, more than idiomatic Go. Read
`docs/design.md` §4a.1 before touching any adapter or any render path.

- **A displayed value must come from measured vendor/tool output.** Anything
  inferred is either omitted or visibly marked as an estimate (a leading `~`).
  Never derive a number (e.g. a dollar cost from token counts) and present it as
  if it were read — that's on the repo's "deliberately rejected" list.
- **"Zero" and "absent" are different states, and rendering must keep them
  different.** `internal/hud` has a literal test for this: one session at 0%
  context and one session whose vendor exposes no context source render as
  different rows — 0% draws a full empty track, absent draws nothing (an em
  dash / `g.Absent`). Collapsing those two into the same glyph is the one
  regression this repo exists to prevent — see `testdata/golden/zero-vs-absent.txt`.
- **A field an adapter cannot source is declared `CapNone`, not filled with a
  plausible guess.** `internal/adapter/claudecode/claudecode.go`'s package doc is
  the model: it lists exactly which fields (`context_pct`, `cost`, `quota`,
  liveness-by-PID) were grepped for in a live corpus and came back with zero
  matches, and says why an assumed value would be dishonest (e.g. context
  percentage needs a window-size denominator that varies by model, so any
  percentage would be invented).
- **Claims about vendor behavior are measured against a live run, never read off
  `--help` or vendor docs.** The council sandbox badges are the clearest example:
  Codex's Windows posture (`unsandboxed` rather than `ro:enforced`) exists
  because `-s read-only` was measured failing *every* process spawn on Windows,
  including one that only listed a directory — not because the flag's
  documentation says so. If you add a claim about what a vendor does, it needs a
  measurement backing it (a live run, a source read at a pinned version) — cite
  it in a doc comment the way the existing adapters do, including the version
  pinned against.
- **A partial read degrades a field, it does not fail the row.** See
  `claudecode.go`'s `Read`: a bad JSONL record increments a counter and is
  reported once in `Diagnostics`; it never aborts the whole session read. The
  distinction that matters is "we could not read this field" (degraded, shown as
  absent, explained in diagnostics) vs. "there is nothing there" (a measured
  zero) — collapsing those is the same class of bug as zero-vs-absent above.

## The read/write boundary

**The gauges never write.** `telltale statusline` and `telltale hud` read vendor
files, make no network calls, read no credentials, and no keybinding mutates
vendor state. `telltale council` is the **single deliberate exception** — it
spawns vendor CLIs, and it is the only mode that writes anything to disk (one
file, `~/.telltale/council/room.json`, holding session ids and workspace — never
transcript or brief content). If you're adding a feature to `internal/hud` or
`internal/statusline` that would write a file, shell out, or touch a credential
store, that is almost certainly the wrong package for it.

The Cursor adapter (`internal/adapter/cursor`) is the sharpest version of this
boundary: its on-disk store holds OAuth/refresh tokens in the *same SQLite file*
as session state, so "reads no credentials" is enforced there with a read
allowlist plus a test that plants credential-shaped strings in fixtures and
asserts none of them reaches anything the HUD can display.

## Where state lives — and why nothing is copied between these

- **`STATE.md`** — intent, in-flight work, and open questions. What git and
  GitHub *cannot* tell you.
- **`PARITY.md`** — cross-platform / cross-machine differences, each one
  measured rather than assumed (see the Codex/Cursor/Antigravity Windows rows).
- **GitHub** (`gh pr list --state merged`) — what actually landed.

None of these three copies a fact the others already hold, on purpose — `STATE.md`
says outright that a hand-maintained copy of a derived fact goes stale by the next
merge, and that it has gone stale *that way three times already*. Don't add a
"recent changes" section to `STATE.md`, and don't restate PR history in
`PARITY.md`. If you need to know what shipped, run `gh pr list`; if you need to
know why a machine behaves differently, that fact belongs in `PARITY.md` and
nowhere else.

## Code idiom notes (not generic Go advice)

- **Doc comments justify design decisions, not just describe signatures.**
  `internal/council/view.go` and `internal/hud/view.go` are the reference: a
  comment routinely explains *why* a line is drawn this way, cites the design-doc
  section (`§9.11`, `§4a.1`) or the field report that forced the change, and says
  what regressed before the fix. Match that density when you touch either
  package — a bare `// renders the header` is below this repo's bar.
- **Fixtures are synthesized, never real.** Test data uses fake session ids, fake
  paths, realistic shape only. No real session content belongs in this
  repository — it's public.
- **`internal/theme` stays stdlib-only.** It's the shared palette both
  `internal/hud` and `internal/council` map into `lipgloss` values, specifically
  so the `telltale statusline` binary never links the Bubble Tea/Lipgloss TUI
  framework on its fast path (ADR-002). Don't import a TUI dependency into
  `internal/theme` or `internal/model`.
- **Council adds no hues of its own** (`internal/council/style.go`) — it maps
  `internal/theme`'s existing palette, and spends only *weight* (bold) as a new
  signal. If you're tempted to add a new color for a new council concept, that's
  very likely the wrong move — reuse an existing severity/identity token instead.
- **Colour, and any single glyph, is always a second signal.** Every distinction
  this UI makes (phase, sandbox posture, focus) is carried first by a word or a
  glyph that survives `--ascii` and `NO_COLOR`, with colour/weight only making it
  easier to spot. If your change only reads correctly in colour, it's incomplete.
