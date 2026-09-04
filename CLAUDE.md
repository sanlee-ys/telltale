# telltale — contributor contract

This file is the convention layer a fresh session cannot get from Go itself. Read
`README.md`, `STATE.md`, `PARITY.md` and `docs/design.md` too — this file is short on
purpose and defers to them for the product's own claims.
Rulings since 2026-09-02 live in `LEDGER.md`, one dated line each; read it before
citing a `design.md` taste ruling, because three of them are lifted there.

## Build and test

```
go vet ./...
go test ./...
go build -o telltale.exe ./cmd/telltale
```

**A local `go test ./...` can time out in `internal/council`.** That package
spawns processes and runs ~455s on a reference workstation, close to Go's 600s
default, so a loaded machine overruns it — a machine-speed condition, not a
regression (CI runs the whole suite in ~4m22s). Two fan-out lanes hit it
independently on 2026-08-16. Run `go test ./internal/council -timeout 20m`, or
test the package you touched, when the full run overruns locally.

That is the whole CI gate (`.github/workflows/ci.yml`), which runs on `windows-latest`
— **Windows is the primary target ([ADR-002](docs/design.md#adr-002))**, not an afterthought platform. CI then
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
`TestMain` wraps the package's four vendor spawn vars (`startProcess`,
`startSession`, `startRPCSession`, `startPTYSession`) so that reaching one with a
binary this machine can actually resolve **panics**, naming the call site and the
full argv. `startEditor` and `startCheck` are wrapped there too, on the same rule
for programs that are not vendors, and `startHostedRoom` and `joinHostedRoom`
(design.md §7.29) are wrapped there too, which makes **eight** in all.

The last two are not vendor spawns and they are the sharpest of the eight.
`telltale council --host` starts a HOST — telltale's own binary, which resolves
on any machine that built it — and that process then spawns real vendors two
processes away from whatever assertion provoked it. `telltale council` finding a
live room JOINS one, which starts nothing and reaches a host that is *already*
holding vendor processes, so a turn dispatched through it is billed by seats
this package never started. `internal/councilhost` has a `TestMain` of its own
over three vars, and it covers ITS test binary only: `go test ./internal/council`
is a different binary and `startHost` is unexported there, so nothing in that
guard reaches a spawn made from here.

`startPTYSession` is the live seat (design.md §9.53) and it is guarded with no
softening. Its output is display only — the pane draws a screen and no gauge
reads it — and that is a claim about what the room may DRAW, not about what the
process costs. A pseudoconsole child is `claude` running interactively on the
operator's own account.

The rule exists because the opposite default was measured costing real money. A
plain suite run on a Windows box with Codex installed was starting
`codex exec --json -s danger-full-access` — a live agent turn with full write
access, on the operator's own account — from a test that only wanted a second
seat for a dispatch to address. **CI could never catch it**: CI has no vendors
installed, so every seat resolves `AvailNotInstalled` and nothing dispatches.
A green pipeline over a local-only defect is exactly what a guard is for.

Two consequences when you write a council test:

- **Dispatching for real means stubbing.** Call `countSpawns(t)`
  (`flow_security_test.go`) — it stubs all eight vars and restores them in
  `t.Cleanup`. Anything that builds an `AvailInstalled` column with a real
  binary name and then reaches `dispatch()` needs it.
- **A deliberately unspawnable binary is still allowed through.** Several tests
  hand over a `telltale-no-such-binary` path to exercise the process-died
  branch; a path `exec.LookPath` cannot resolve launches nothing, so the guard
  lets it reach the real call and fail there. The gate is what would actually
  run, not a declared intent — an opt-in marker would just become the thing a
  future test copies without meaning it.

**`internal/probe` carries the same guard over its own two spawn vars**, and it
needs it more than any package here. That package exists to spend a billed turn,
so a test of it that reached a resolvable binary would not be an accident in a
test that wanted a second column: it would be the suite doing the exact thing
the operator is asked to confirm. Its `TestMain` wraps `startSession` and
`startRPCSession` on the identical rule (a binary this machine can resolve
panics; one it cannot is let through to fail), and it points HOME and
USERPROFILE at a sandbox for the same reason council's does, sharpened: a suite
that wrote the operator's own `~/.telltale/probe` files would put a result on
their disk that no probe of theirs produced, and `telltale doctor` would then
report it as a measurement made on that machine.

## A council test never writes the operator's own state

Same file, same reason, quieter defect. `TestMain` points `HOME` and
`USERPROFILE` at a temporary directory for the whole test binary, so
`~/.telltale/council` resolves inside a sandbox rather than on the disk of
whoever ran the suite.

It is there because the opposite was measured. On 2026-09-01 the operator's real
`council/room.json` carried a `workspace` naming a Go test temp directory, with
that morning's timestamp, and the suffix changed between two reads minutes apart
— so **every** plain `go test ./internal/council` was rewriting the file the next
`telltale council` reattaches from, pointing it at a directory the test had
already deleted. Five of some sixty test files redirected `HOME` by hand; the
rest wrote the operator's disk.

- **Do not add a per-test redirect for this.** `t.Setenv` still works and still
  wins where a test wants its own home for its own reasons. It is no longer the
  thing standing between the suite and the operator's state, because the failure
  mode is a test that forgets.
- **The check runs after `m.Run()`**, and it snapshots one directory —
  `~/.telltale/council` — by name, size and modification time. Not the whole of
  `~/.telltale`: the statusline's quota relay writes `~/.telltale/quota` on every
  prompt of every other tool the operator has open, so the wider snapshot would
  fail on a busy desk for a reason that is not this suite.
- **CI cannot catch this class either**, and less visibly than with the spawn
  guard: the runner's home is fresh per job, so the file the suite corrupts is
  created, corrupted and discarded inside one green run.

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

## The honesty rules this project is built on ([ADR-001](docs/design.md#adr-001))

This is the thing the whole codebase optimizes for, more than idiomatic Go. Read
[`docs/design.md` §4a.1](docs/design.md#s4a-1) before touching any adapter or any render path.

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
  `--help` or vendor docs.** The council sandbox badges are the clearest example,
  in both directions: Codex's Windows read posture wore `unsandboxed` rather than
  `ro:enforced` because `-s read-only` was measured failing *every* process spawn
  there at codex-cli 0.146.0 — and it got `ro:enforced` back only when a
  2026-08-29 re-measurement at 0.149.1 showed a live shell write denied with no
  file on disk (design.md §9.2's dated amendment). Neither move rested on the
  flag's documentation. If you add a claim about what a vendor does, it needs a
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
its own to relay. `telltale mcp` (design.md §7.25) is a fourth reader of that
same document and holds the same contract: stdio only, so it binds no port
either. `telltale history` (design.md §7.26) is a fifth reader, and the first
that does not read the scan at all — it walks one vendor's transcripts whole,
which is why it is a foreground mode rather than a HUD page. It holds the
contract with the same item spare, for the same reason: it renders no quota, so
it has none to relay. `telltale council ls` (design.md §7.27) is a **sixth**
reader, and it is the first that reads a file *council itself wrote* rather than
a vendor's: it prints what `~/.telltale/council/room.json` holds — workspace,
turn, roster, and which seats have a session id saved — and writes nothing,
spawns no vendor, and binds nothing. It holds the contract with the same item
spare, for the same reason as the two above it. It never says a saved thread is
*live*: nothing it can read proves that, and only the vendor answers it, on a
resume. Since design.md §7.29 (2026-09-01) it also reports whether a **host** is
running, and it holds every clause above unchanged — the liveness probe asks
whether a pipe NAME exists (`WaitNamedPipe`) and never opens it, so the listing
cannot end the room it is listing, and a **stale `host.json` is reported and
never removed**, because a reader that tidied would be a writer. The room
removes that file; `ls` only says it is there. **Four** deliberate, bounded exceptions
exist, all under `~/.telltale/` and all numbers-and-keys only, never content:

- `telltale council` — spawns vendor CLIs; writes `council/room.json` (session
  ids and workspace, never transcript or brief content). `telltale council
  host` (design.md §7.28, added 2026-09-01) is the same grant from a second
  process: it owns the vendor CLIs instead of the TUI owning them, and it adds
  ONE file beside the first — `council/host.json`, holding a pid, a pipe name,
  a start time, the workspace, the seat ids and a turn count. Four of those
  seven are already in `room.json`; the other three are process facts, and
  `resume.go`'s leak sentence covers the shape unchanged. **The room's
  conversation never reaches disk from the host either** — it lives in host
  memory and dies with the host, on `resume.go`'s own ruling for the same
  data. §7.29 (2026-09-01) exposed detach and added NO file and no field: a
  rejoining client is handed the host's current projection over the wire, not a
  replay from disk, and a room that outlives its terminal is still a room whose
  conversation dies with its process.
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
- **`telltale probe`** — `probe/<vendor>.json`, one file per seat: the vendor
  id, the version string that binary printed, the day, the telltale build that
  probed, and one result plus a millisecond count for each of the three checks
  it ran (handshake, one turn of one word, stop). It is the strictest of the
  four because its writer DRIVES an agent, so four kinds of content are within
  reach of it and none of them may be written: the brief, the reply, the
  session id the vendor named, and the directory the seat ran in. **The failure
  reason is refused too**, and that is the decision to read before changing
  anything here: a vendor's own first stderr line routinely carries a path or a
  session id, so it would carry content by the back door, on exactly the runs a
  reader is most likely to paste somewhere. The reason prints in the terminal
  where the probe ran and stops there; `telltale doctor` reports WHICH check
  failed and names the command that shows why. `Result.Record` is the one place
  that decides what reaches disk, and it drops the reason on every branch.

A fifth exception is different in kind and says so: the **event sink**
(`telltale events`, design.md §7.21) stores hook payloads VERBATIM under
`~/.telltale/events/` — content, not numbers-and-keys. What contains it is
scope, not redaction: it is its own foreground mode the operator starts, the
server binds loopback only, and no gauge reads or renders its files. The
numbers-and-keys rule above still binds everything the gauges themselves
write. Its reader (`telltale events view`, `internal/eventview`, added
2026-08-17) is a separate foreground mode for that last reason, and it reads
the day FILES rather than the sink's endpoints, because §7.24 already ruled a
local program's file read and its loopback request equally trusted and only
the file read answers after the sink exits.
`TestNoGaugeReadsTheEventStore` fails the build if a gauge ever imports either
package.

**Who may push to the two listening modes** (`telltale otel grok`, `telltale
events`) is its own contract, added 2026-08-16 (design.md §7.24). A loopback
bind is not containment on its own: a web page the operator merely visits
reaches 127.0.0.1 too, and a measured headless Chrome planted a forged row in
`usage/grok.json`, planted an event in the sink, and read the sink's whole
verbatim store over `/stream`. Both modes now refuse any request carrying an
`Origin` header — the stream included, before the upgrade — and require the
media type the measured sender sends. `internal/localonly` holds the check;
don't add a per-endpoint copy of it, and don't add an origin allowlist, because
nothing telltale ships runs in a browser. A local **program** is still trusted
completely and on purpose: it can write `usage/<vendor>.json` directly, which
`internal/usagecache/trust_test.go` pins, so a bearer token on the HTTP path
would buy nothing against it.

Each of the four exceptions carries a test pinning the serialized form
to keys and numbers. If you're
adding a feature to `internal/hud` or `internal/statusline` that would write
anywhere else, shell out, or touch a credential store, that is almost certainly
the wrong package for it.

**`telltale probe` is also the one mode that SPENDS a vendor turn**, and that
is a second boundary, separate from what it writes. `doctor` widened the
no-vendor rule to `<binary> --version` and drew the line at cost and side
effect (design.md §9.42); this mode is on the far side of it. So the cost is
stated before the run, the operator is asked at the terminal, and a run with no
terminal is refused unless `--yes` is given: a mode that spends money must not
be reachable from a hook, a script or a CI step by accident. Nothing else in
the binary calls it. Do not wire it into a gauge, the room, a test or a
schedule, and do not make its one-word brief configurable — a settable brief is
a way to spend a real turn through a mode that promised a trivial one.

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
  so the `telltale statusline` **code path** never reaches the Bubble Tea/Lipgloss
  TUI framework ([ADR-002](docs/design.md#adr-002)). Don't import a TUI dependency into `internal/theme` or
  `internal/model` — `TestFastPathNeverReachesTUIFramework` fails the build if you
  do, naming the package. Note the precise claim: the shipped **binary** does link
  both modules, because it is one binary and `telltale hud` is in it
  (`go version -m telltale.exe` shows them). What [ADR-002](docs/design.md#adr-002) buys is that neither
  module's package init runs on a path that executes on every prompt. design.md §5's
  2026-08-16 amendment says what the gate asserts and what it deliberately does not.
- **Council HAS its own ink set, and the rule that forbade it is LIFTED.** Read
  `LEDGER.md`'s 2026-09-02 and 2026-09-03 lines before you cite anything about
  council's colour, then `docs/room-identity.md` and `internal/council/style.go`.
  "Council adds no hues of its own" was true until the ledger lifted it; the room
  now carries one warm ink at six values plus two accent pigments, all of them
  hex, all of them inside `internal/council`. `internal/theme` is untouched, so
  `internal/statusline` and `internal/hud` keep the 4-bit palette and ADR-002 is
  unaffected — the blast radius of that set is one package, and it stays that
  way.
- **Truecolour may ENHANCE the identity; it may never DEFINE it.** A hex triple
  is allowed. A distinction that DEPENDS on one is not. Every distinction this UI
  makes (phase, sandbox posture, focus, a verdict, a rank) is carried first by a
  word or a mark that survives `--ascii`, `NO_COLOR` and a 16-colour console,
  with colour, weight and the one painted ground only making it easier to spot.
  If your change only reads correctly in colour, it's incomplete. This is
  accessibility rather than taste, and `LEDGER.md` says it was NOT lifted with
  the three taste rulings.
