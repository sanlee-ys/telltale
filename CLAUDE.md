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

**Run git as plain `git`, never `git -C <path>` — if you are a council seat.**
This is posture-dependent, and PR #223 did not change it either way — read
`STATE.md`'s "write seat's `git -C` allowlist gap" entry for the full
measurement. In the **ungated write posture** (`PostureWrite`), Claude Code's
own `--allowedTools` matcher answers your permission prompts, and it is
prefix-only: `autoAllowedTools` in `internal/council/vendors/claude.go` grants
`Bash(git status:*)` and friends, but that cannot match a command beginning
`git -C`, and no rule spelling fixes it (`Bash(git -C:*)` stays rejected —
it would pre-approve every `-C` verb, destructive ones included). Your cwd is
already the workspace, so `-C` buys nothing and costs that posture's git calls
an approval. In the **gated posture** (`PostureWriteGated`, the one PR #223
hardened), a different mechanism answers instead — council's own
`autoApproveRoutine`, via `safeGitArgs` in `internal/council/persistent.go` —
and it strips a leading `-C <path>` before classifying the command, so
`git -C <path> status` and `git status` are already the same call to the gate
(`internal/council/gate_git_test.go` pins this). That classifier predates
PR #223 by a week and PR #223 never touched it, so the gated posture was never
the bug this instruction describes. Write plain `git` regardless: a seat
cannot tell which posture it is running under, plain `git` costs nothing extra
when gated, and it saves an approval when ungated. This **inverts** the usual
advice: outside a seat, `git -C` is preferred precisely to avoid `cd &&`
chains. Same trap on `gh` in the ungated posture — `gh pr` and `gh run` are
allowlisted there, `gh api` is not, so reach for the porcelain rather than the
API.

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

## A council test never spawns a vendor

`go test ./internal/council` must not start a vendor CLI, and this is enforced
mechanically rather than by convention. `internal/council/main_test.go`'s
`TestMain` wraps the package's three spawn vars (`startProcess`, `startSession`,
`startRPCSession`) so that reaching one with a binary this machine can actually
resolve **panics**, naming the call site and the full argv.

The rule exists because the opposite default was measured costing real money. A
plain suite run on a Windows box with Codex installed was starting
`codex exec --json -s danger-full-access` — a live agent turn with full write
access, on the operator's own account — from a test that only wanted a second
seat for a dispatch to address. **CI could never catch it**: CI has no vendors
installed, so every seat resolves `AvailNotInstalled` and nothing dispatches.
A green pipeline over a local-only defect is exactly what a guard is for.

Two consequences when you write a council test:

- **Dispatching for real means stubbing.** Call `countSpawns(t)`
  (`flow_security_test.go`) — it stubs all three vars and restores them in
  `t.Cleanup`. Anything that builds an `AvailInstalled` column with a real
  binary name and then reaches `dispatch()` needs it.
- **A deliberately unspawnable binary is still allowed through.** Several tests
  hand over a `telltale-no-such-binary` path to exercise the process-died
  branch; a path `exec.LookPath` cannot resolve launches nothing, so the guard
  lets it reach the real call and fail there. The gate is what would actually
  run, not a declared intent — an opt-in marker would just become the thing a
  future test copies without meaning it.

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

**The gauges never write to anything that isn't theirs.** `telltale statusline`
and `telltale hud` read vendor files, make no network calls, read no credentials,
and no keybinding mutates vendor state. `telltale snapshot` (design.md §7.22) is
a third reader of the same scan and holds the contract with one item spare — it
writes nothing at all, not even the quota relay, because it renders no quota of
its own to relay. **Three** deliberate, bounded exceptions
exist, all under `~/.telltale/` and all numbers-and-keys only, never content:

- `telltale council` — spawns vendor CLIs; writes `council/room.json` (session
  ids and workspace, never transcript or brief content).
- the **statusline's quota relay** — `quota/<vendor>.json`, the rate-limit
  windows it just rendered, written after the line is on stdout so the HUD can
  attribute account quota per vendor (design.md §7.15, amended 2026-08-07).
- the **token relay** — `usage/<vendor>.json`, a running total of per-turn
  token counts, with two writers and one schema. `telltale hook cursor` reads
  Cursor's `afterAgentResponse` payload on stdin (design.md §7.16, added
  2026-08-08); it is its own mode rather than a flag on a gauge because a
  hook's stdout is parsed by the vendor as a hook result, so that path prints
  nothing at all and exits 0 on every branch. `telltale otel grok` (§7.16a,
  added 2026-08-10) is a loopback-only OTLP listener that grok's own external
  OpenTelemetry exporter pushes to — the push is grok's, so the gauges still
  make no network calls; they only read the file it writes. **The DISPLAY of
  both totals is retired/held by the owner** (§7.16's amendment, applied to
  grok on arrival) — the writes, the cache and the HUD's read of them are all
  wired, and nothing renders a total today. Don't "clean up" the reader on the
  grounds that it is unused; being wired is the point of it.

A fourth exception is different in kind and says so: the **event sink**
(`telltale events`, design.md §7.21) stores hook payloads VERBATIM under
`~/.telltale/events/` — content, not numbers-and-keys. What contains it is
scope, not redaction: it is its own foreground mode the operator starts, the
server binds loopback only, and no gauge reads or renders its files. The
numbers-and-keys rule above still binds everything the gauges themselves
write.

Each of the three relay exceptions carries a test pinning the serialized form
to keys and numbers. If you're
adding a feature to `internal/hud` or `internal/statusline` that would write
anywhere else, shell out, or touch a credential store, that is almost certainly
the wrong package for it.

Two sharper versions of the same boundary, worth reading before you touch either
seam. The Cursor **adapter** (`internal/adapter/cursor`) reads an on-disk store
that holds OAuth/refresh tokens in the *same SQLite file* as session state, so
"reads no credentials" is enforced with a read allowlist plus a test that plants
credential-shaped strings in fixtures and asserts none reaches anything the HUD
can display. The Cursor **hook** (`internal/cursorhook`) is handed a payload that
carries the model's full reply text and the user's email address alongside the
four numbers telltale wants — so the allowlist there is the struct itself:
`encoding/json` drops every field with no destination, and a test plants markers
in a real payload shape and asserts none survives, at the parser and again on the
serialized cache file.

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
