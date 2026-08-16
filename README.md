# telltale

**A dispatch room for your coding agents.** One brief, answered by five vendor CLIs
side by side — Claude Code, Codex, Antigravity, Cursor and Grok — each column claiming
only what was measured about that vendor. Under the room, an honest gauge: a statusline
and a cross-vendor HUD where every number is traceable to measured tool output, nothing
narrated, nothing guessed.

> A telltale is the ribbon on a sail that shows true airflow. Sailors watch whether
> it streams smoothly or flutters to judge the sail's trim. It doesn't interpret;
> it just tells you what's actually happening.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="images/telltale-council-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="images/telltale-council-light.svg">
    <img alt="telltale council dispatch room showing multi-agent panel" src="images/telltale-council-dark.svg">
  </picture>
</p>

---

**Status: v1 shipped as v0.2.0 on 2026-08-14; development continues daily.** All three
surfaces are shipped — the room, the statusline and the HUD. The same binary carries
five more modes that render no surface of their own: the relays (`telltale hook`,
`telltale otel`), the preflight (`telltale doctor`), and the observation modes
(`telltale events`, `telltale snapshot`).

**v1 is a snapshot, not a freeze.** It cuts when three checkable gates hold — nothing on
the surface is half-finished or unused by its own author, the README is verified true
against the code, and no breaking change to the grammar or keymap is planned — not when
development goes quiet, because it doesn't: this room is driven daily. Cutting v1 as
gauges only — statusline and HUD, with declared vendor version pins — was the standing
alternative and it is rejected: a release names the product, and the product is the room.
The gates and the argument are in [docs/design.md §1](docs/design.md#s1).

Every adapter here was verified against something real rather than against vendor docs — a
live on-disk corpus, a live payload capture, or the vendor's own persistence code read at a
pinned version — and [docs/design.md §3](docs/design.md#s3) itemizes per vendor which of those
it was, what the verification changed about this project's guesses, and what each one still
owes. **Cursor (Composer)** is the first IDE-resident agent here, and the first whose store
also holds live credentials, which is why that adapter's most load-bearing property is the
list of things it does not read ([docs/design.md §3.9](docs/design.md#s3-9)). **`telltale
council`** seats the 5-vendor fleet, and every sandbox and streaming claim in it was
measured against a live run of that CLI rather than read off its `--help`
([docs/design.md §9](docs/design.md#s9)).

## Install

**v0.2.0 is released** (2026-08-14, the first release; the snapshot gates held). The
repo's own CI gate ran before any artifact was built, and the release attaches four
archives with a `checksums.txt`. Read the per-download verification labels on the
release itself before you pick one — they say what was measured, and they differ by
platform.

One honest note on the scoop line below, in this project's usual terms: the install was
exercised once, on Windows 11 on 2026-08-14 — scoop verified the archive's SHA-256
itself, the installed binary reported `telltale 0.2.0`, and `telltale doctor` ran clean
through it. One exercised install is one data point, not a support matrix. Building from
source is the path this project runs every day.

**From source** — works now:

```
go build -o telltale.exe ./cmd/telltale
./telltale.exe council
```

However you installed: run `telltale` with no arguments for a short first frame — the
three modes that need no configuration, and which one to start with. `telltale doctor`
is that one. It reports which vendor CLIs are on this machine, where each was found and
what version it reports, and says out loud what it never checked: it runs
`<binary> --version` and nothing else — no turn, no login, no network, and it writes
nothing. On a machine with no vendor store yet, the HUD is not blank either: it names
every path it looked in, and points at `doctor` for the one thing it cannot see —
whether the vendor binaries are installed at all.

**Windows, scoop** — the bucket is live; exercised once on 2026-08-14 (above):

```
scoop bucket add telltale https://github.com/sanlee-ys/telltale
scoop install telltale
telltale council
```

**Windows, winget** — pending submission to `microsoft/winget-pkgs`; the manifest
draft and the flow are in [packaging/](packaging/):

```
winget install sanlee-ys.telltale
telltale council
```

**Direct download** — each release attaches archives for `windows_amd64`,
`darwin_amd64`, `darwin_arm64` and `linux_amd64` with a `checksums.txt`. Unpack one,
put `telltale` on your PATH, and:

```
telltale council
```

**The binaries do not all claim the same thing, and each release says so per
download.** Windows is the **continuously verified target** — every commit runs the
suite, the build and binary-level smokes on `windows-latest`. `darwin_amd64` is
**smoke-verified on Intel macOS**, point-in-time and SHA-bearing. `darwin_arm64` and
`linux_amd64` are **built, not verified**: cross-compiled and never run by this
project. That is the same flagged-limitation rule the gauges apply to a segment,
applied to a platform ([docs/design.md §8](docs/design.md#s8)).

**No binary here is signed, on any platform.** The release workflow runs no
signing step, so every archive is unsigned and the macOS archives are not
notarized. Windows raises no signature to check, and `scoop` and `winget` install
that same unsigned binary. On macOS, Gatekeeper refuses an unsigned, un-notarized
binary that a browser downloaded and marked with `com.apple.quarantine`. Check
the SHA-256 in `checksums.txt` before you run a download — it proves the archive
is the one the release built, which is a weaker claim than a signature and is the
claim this project can make today. [SECURITY.md](SECURITY.md) states the per-platform
detail and says why signing is an owner decision rather than a to-do.

`telltale version` prints the tag a binary was built from; a source build says `dev`.

`telltale.exe council` opens the room, which is the mode this project is for and has its
own section below. The two gauges wire in underneath it.

Then wire the statusline into Claude Code (`~/.claude/settings.json`):

```json
{
  "statusLine": {
    "type": "command",
    "command": "C:\\path\\to\\telltale.exe statusline"
  }
}
```

…and/or into Antigravity CLI (`~/.gemini/antigravity-cli/settings.json` — same block,
same binary; telltale detects the vendor from the payload's documented `product`
field):

```json
{
  "statusLine": {
    "type": "command",
    "command": "C:\\path\\to\\telltale.exe statusline"
  }
}
```

…and/or into Cursor CLI (`~/.cursor/cli-config.json`, a top-level key). **This one needs
`--vendor cursor`**: unlike the other two, Cursor's payload carries no vendor name to
detect, so the flag is the only way to route it.

```json
{
  "statusLine": {
    "type": "command",
    "command": "C:\\path\\to\\telltale.exe statusline --vendor cursor"
  }
}
```

Cursor's statusline is **interactive-only** — it does not fire in `-p` print mode or over
ACP (measured; `docs/design.md` §7.16). To see it, start an interactive session in a
folder you have opened with `cursor-agent` before:

```
cursor-agent
```

…and run the HUD:

```
telltale.exe hud
```

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="images/telltale-hud-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="images/telltale-hud-light.svg">
    <img alt="telltale TUI HUD showing cross-vendor agent telemetry" src="images/telltale-hud-dark.svg">
  </picture>
</p>

That picture is emitted from the HUD's own render by the test suite, not drawn by hand,
and it is doing the thing this project is about: the Claude, Gemini and Antigravity rows' context cells are em
dashes because none of those vendors writes a context-window size to disk, the Codex
row's `~` marks a percentage telltale computed rather than read, the Gemini row's `⑂~2`
chip is a sub-agent count telltale derived (and marks as derived) from the vendor's
nested transcript tree, and the `COST` column is missing entirely because no vendor puts
a session total in dollars on disk — Grok writes a per-turn figure and still gets no COST
cell for it. None of those is rendered as a zero. Read the `CU` and `GR` rows against the
`CX` row: all three show a context bar, and only the Codex one is marked an estimate,
because Cursor and Grok write their own percentages down and telltale reads them. The `AG`
row is labelled by its conversation id for a different reason: Antigravity writes no
session title anywhere, and the only free text on its disk is your prompts, which
telltale will not read.

In the HUD, `↑`/`↓` move the selection and `enter` opens a detail pane for it — quota
windows, the vendor extras, and the session's own diagnostics and degraded-field marks,
plus a line naming the fields that vendor **cannot** source at all. That last line is the
answer to "why is this cell empty?", and it is the difference between "we have no value
right now" and "this vendor never had one". `/` narrows the rows by name or path.

`telltale hud` flags: `--vendor all|claude|codex|gemini|agy|cursor|grok`, `--hide
gemini,cursor` (a standing hide list; `TELLTALE_HUD_HIDE` is its default, the footer
states the hide), `--ascii` (also `TELLTALE_ASCII=1`), `--no-title`. `NO_COLOR` is
honoured through the standard mechanism.

## What it is

- **A dispatch room** — `telltale council`, one brief typed once and answered by Claude
  Code, Codex, Cursor, Antigravity and Grok side by side. The one mode that spawns vendor
  CLIs instead of reading their files; it gets its own section below.
- **In-prompt statusline for Claude Code** — model, context %, session cost, and quota
  pacing (`rate_limits` windows), rendered from the JSON Claude Code hands your
  statusline command on stdin. No network calls, no credential reads.
- **A watch-mode HUD (TUI)** for parallel sessions — the cross-vendor surface, and a
  first-class UI investment (Go + Bubble Tea/Lipgloss; Windows Terminal is the reference
  environment). Ships with adapters for **Claude Code**, **Codex CLI**, **Gemini CLI**,
  **Antigravity CLI**, **Cursor (Composer)**, **Grok CLI**, and **Pi**, each reading that vendor's
  own native on-disk data — for Antigravity and Cursor that means a read-only SQLite
  reader written into this repo rather than a 9 MB dependency added to it.
- **A machine-readable read mode** — `telltale snapshot` prints the fleet's current state
  as one JSON document, for a reader that is a program rather than a person. It gets its
  own section below.
- **A documented adapter interface** — one module per vendor — so you can wire in
  anything else that leaves session data on disk. The worked example in
  [docs/design.md §4a.7](docs/design.md#s4a-7) is the method the Gemini adapter was actually
  built with, kept alongside what live verification changed about its guesses.

One binary, three surfaces: `telltale statusline`, `telltale hud` and `telltale council` —
and they are not three co-equal products. **The room is the product; the gauges are the
infrastructure under it.** The statusline and the HUD are where this project surveyed each
vendor's seam and wrote down what it found, and the honest-gauge rule they were built
under is the rule a council column inherits when it states a sandbox posture. They are
finished, they are load-bearing, and they are not the thing this is for. The statusline
code path never initializes the TUI framework (the single binary links it, but no Bubble
Tea code runs on a statusline invocation).

Three surfaces are not the whole binary, and this file does not claim they are. The
binary has eight modes, and `telltale` run with no argument prints all of them. The
five that draw no surface stand behind these three: the
**relays** (`telltale hook <vendor>`, `telltale otel <vendor>`) read one turn's token
counts and print nothing; the **preflight** (`telltale doctor`) reports which vendor
binaries this machine has; and the **observation modes** (`telltale events`, `telltale
snapshot`) answer a program rather than a person. `telltale snapshot`, `telltale events`
and `telltale doctor` get their own sections below; the two relays are described under
the read/write boundary, because a relay is a write.

Honest claim, stated precisely: *dispatch across the 5-vendor fleet (Claude Code, Codex,
Cursor, Antigravity, Grok); cross-vendor monitoring; vendor-native statusline where the seam
exists — and it exists twice: Claude Code and Antigravity CLI.* (Codex CLI has
no statusline hook today. Antigravity was statusline-only until a re-survey found the
transcript its own docs advertise, which is what made its HUD adapter buildable — the
first verdict and the reversal are both in [docs/design.md §2.1](docs/design.md#s2-1)/[§3.8](docs/design.md#s3-8).
Cursor reaches telltale both ways: a built-in HUD adapter because its seam is on disk,
and a council seat driven through `cursor-agent`'s own bundled `node.exe` — the `cursor`
binary on PATH is only the editor launcher and council never drives it. Grok is a council
seat and a HUD adapter on the same binary — the seat has no fleet guards wired yet, which
is an open obligation on the fleet rather than a reason the column is missing.)

**The gauges never write to anything that isn't theirs.** `telltale statusline` and
`telltale hud` read vendor files, make no network calls, read no credentials, and no
keybinding can mutate vendor state or send anything to a running agent. What the gauges
and the room write is three stores of their own under `~/.telltale/`, all keys and
numbers, never content: council's room file (`council/room.json` — the vendor session ids
reattaching needs and the room's workspace, no transcript, output or brief content),
the statusline's quota relay (`quota/<vendor>.json` — the rate-limit windows it
just rendered, so the HUD can show account quota per vendor instead of only for
vendors whose stores carry it; [docs/design.md §7.15](docs/design.md#s7-15)), and the
token relay (`usage/<vendor>.json` — a running total of per-turn token counts,
with two writers: `telltale hook cursor` reads Cursor's `afterAgentResponse`
payload on stdin, and `telltale otel grok` is a loopback listener grok's own
OpenTelemetry exporter pushes to; [docs/design.md §7.16](docs/design.md#s7-16), [§7.16a](docs/design.md#s7-16a)).
That last one is spend, not quota: there is no denominator anywhere in it, so it
never renders as a percentage or a bar, and Cursor's and grok's account quota
stay visibly absent.

**A fourth store carries content, and it is named as an exception rather than
counted with the three.** The event sink (`telltale events`, below) stores each hook payload
VERBATIM under `~/.telltale/events/` — content, not keys and numbers. What contains
it is scope, not redaction: it is its own foreground mode that you start, its server
binds loopback only, and no gauge reads or renders those files. The keys-and-numbers
rule above still binds every store the gauges themselves write
([docs/design.md §7.21](docs/design.md#s7-21)).

`telltale
council` remains the one mode that acts on the world, and it is labelled as one
everywhere it can be: it spawns vendor CLIs, it is entered only by typing the
subcommand, it is not reachable from the HUD, and it shares no keybinding with it.
"Reads no credentials" stopped being free with the Cursor adapter — that vendor
keeps its access tokens, refresh tokens and OAuth secrets in the *same SQLite file* as
its session state — so it is enforced there as a read allowlist with a test that plants
credential-shaped strings in the fixtures and asserts none of them reaches anything the
HUD can display ([docs/design.md §3.9](docs/design.md#s3-9)). The Cursor *hook* is the same
discipline against a different hazard: its payload carries the model's reply text and
the user's email address beside the four numbers, so the parser's struct is the
allowlist and markers planted in a real payload shape must reach neither the parse nor
the file.

## The dispatch room

`telltale council` is one brief, typed once and answered by five vendors side by side —
**Claude Code**, **Codex**, **Antigravity**, **Cursor** and **Grok**, each in its own
column, in your terminal. It exists because the alternative is five terminals and a
clipboard.

```
telltale.exe council
```

Every column carries its own sandbox posture and its own streaming granularity, because the
five vendors differ on both and one blanket claim would be false for at least one of them —
and each of those claims was measured against a live run of that CLI rather than read off
its `--help`. A plain `telltale council` can write, and says so in the header for the whole
session; `--read` opens a room that only talks. But no badge is what keeps this room out of
your files — the directory it was pointed at is, and `--cd` is how you move it.

An unaddressed brief goes to Claude alone; `@codex`, `@agy`, `@cursor`, `@grok` and `@all`
route a turn, `-@claude` addresses everyone but that seat, and the composer prices the route before
you press enter. Each seat keeps its own conversation and rides that vendor's own native
resume rather than a re-sent transcript, so no session ever holds another's history.

**[docs/council.md](docs/council.md) is the room's own guide** — the frame, the badge
vocabulary, the routing grammar, the reading keys and the turn view, taking an answer out
of the room with `y`, and every flag. [docs/design.md §9](docs/design.md#s9) is the record
behind it: what was measured per vendor, what each seam cost, and what is still unverified.

## `telltale snapshot` — the fleet as JSON

```
telltale.exe snapshot
```

One scan, one JSON document on stdout, exit 0. It reads the same vendor stores the HUD
reads, and it prints numbers instead of a frame — so an agent gets its answer from one
command and one parse, and never from scraping a TUI or reading `~/.telltale/` behind
telltale's back.

Three flags: `--vendor <id>` reports one vendor, `--compact` prints the document on one
line, and `--timeout <dur>` bounds the scan (default 10s). An unknown flag, an unknown
vendor or a stray argument is refused with the correction and prints no document — a
script that mistypes a flag must not receive a well-formed answer to a different
question.

The schema is `{schema_version, generated_at, scan_error, fleet, vendors[]}`, and it
carries this project's honesty rules rather than restating them in prose:

| the document says | it means |
|---|---|
| `"cost_usd_total": 0` | measured zero |
| `"cost_usd_total": null` | no reading right now |
| `"unsupported": ["cost"]` | this vendor exposes no such thing, ever |
| `"estimated": ["context_pct"]` | the adapter computed that value; it was not reported |
| `"quota": []` | no relayed account reading — never an implied 0% |

No optional key is ever omitted, so an absent value and a changed schema can never look
alike. `fleet` is the pre-computed rollup — the session count, the liveness census, the
vendor census by status, the highest context percentage anywhere and the total cost — so
the common question costs no arithmetic on the reader's side.

It renders **numbers and keys, never content**: no session names, workspace paths,
transcripts, briefs or reply text, and no per-session rows at all. It writes nothing, calls
no network and reads no credential. [docs/design.md §7.22](docs/design.md#s7-22) is the full
schema record and the reasoning; `internal/snapshot/testdata/golden/zero-vs-absent.json`
is the build-failing test that the two kinds of nothing stay apart.

### A worked consumer

[`tools/fleet-prompt.ps1`](tools/fleet-prompt.ps1) is one PowerShell function that runs
the command once, parses the line, and returns a fleet segment for a prompt:

```
. .\tools\fleet-prompt.ps1
Get-TelltaleFleetLine
```

Driven on Windows PowerShell 5.1 against this machine's real stores, that prints:

```
tt 6 watching | 1554 sessions, 3 live | ctx ~75.8% codex | quota 12.2% agy/gemini-weekly
```

It carries the rules above into a caller rather than restating them. A null prints
nothing: a fleet with no context reading anywhere has no `ctx` segment at all, while a
measured zero prints as `0%`. The `~` is there because codex's block lists `context_pct`
under `estimated`. An unknown `schema_version` prints an empty line rather than a guess.

[`docs/snapshot.schema.json`](docs/snapshot.schema.json) is the contract it reads against
— the same file CI validates the built binary's output with. Its `-FromFile` parameter
reads a document instead of running the binary, which is how the refused store, the
drifted store and the scan error were driven: those are shapes a healthy machine does not
produce, and `internal/snapshot/testdata/golden/` carries all four.

## `telltale events` — the fleet event sink, and it runs dark

```
telltale.exe events
```

One hook event per POST on loopback, appended to a durable log under
`~/.telltale/events/` and rebroadcast to every client connected to `/stream`. Any
process that can pipe JSON is a source: wire `tools/emit-event.py` as a hook command,
where `--source-app <name>` is the one per-repo edit. Two flags: `--addr <host:port>`
(default `127.0.0.1:4519`; any other host is refused at startup) and `--retain <days>`
(default 30).

**Nothing renders these events.** The sink runs dark by design — events accrue and
stream, and no telltale surface displays one. A viewer is a later call site, not a
re-plumb. This is also the one store that holds content rather than keys and numbers,
and the read/write boundary above names it as the fourth exception.
[docs/design.md §7.21](docs/design.md#s7-21) carries the record.

## `telltale doctor` — the launch-time preflight

```
telltale.exe doctor
```

Which vendor binaries are on this machine, where each one was found, and what version
each one reports — plus, said out loud rather than left blank, what was never checked.
Auth and network always read `not checked`: nothing here probes a login or calls the
network, and a preflight that implied otherwise would be trusted on the one day it was
wrong. It runs each vendor bounded to `<binary> --version`, writes nothing, and gives
each seat its own `--timeout` (default 15s) so a wedged vendor costs its own deadline
and not the report. The report is words and no colour, so it reads the same in a
terminal, in a pipe and in a pasted issue.

## The honest-gauge rule

A segment may only display a value read from tool or vendor output. Anything inferred is
either omitted or visibly marked as an estimate. Every segment's data source is named in
[docs/design.md](docs/design.md), and the eval harness asserts each segment's render
against fixture inputs — including empty and degraded states. A gauge that can't tell
"no data" from "zero" fails the build.

That last sentence is a literal test. `internal/hud` renders one session at 0% and one
session whose vendor exposes no context source, and asserts the two rows differ: 0% draws
a full track, absent draws nothing at all.

Fixtures are **synthesized** — fake session ids, fake text, fake paths, realistic in shape
only. No real session content is in this repository.

## Design

- [docs/council.md](docs/council.md) — the dispatch room in use: the badge vocabulary, the
  routing grammar, the reading keys, and every flag
- [docs/design.md](docs/design.md) — segments, data sources, the normalized schema, the
  adapter contract, the HUD UI specification, and the council record (§9)
- [STATE.md](STATE.md) — where the project is right now: what is in flight, what is
  unsettled, and what is known-missing and unowned
- [PARITY.md](PARITY.md) — cross-platform and cross-machine differences, and which of
  them are measured rather than assumed

## License

MIT
