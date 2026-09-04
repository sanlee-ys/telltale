# telltale

Telltale is a harness that measures what it's driving and refuses to
drive one unwatched.

Telltale is for a builder who pays for two to five personal AI coding
subscriptions. You put one question to all of them and you see who is
right. You also see what each seat was allowed to do while it answered.

The room gives you five surfaces:

- `@all` sends one brief to every seat: Claude Code, Codex, Antigravity,
  Cursor, and Grok. The seats answer side by side, each in its own
  column.
- `ctrl+r` arms a rebuttal round. Each seat then reads the other seats'
  last answers, fenced and labelled as untrusted material.
- A gate card stops a seat that asks before a write, and `y` approves it.
  The posture rail beside the card says what each seat may do, and it
  marks a posture that no run measured. `?` explains each badge.
- `/arena` races one brief across the seats, each attempt in its own git
  worktree. The room ranks the attempts by diff, and `/arena check
  <command>` adds a PASS or a FAIL from a real exit code. `/adopt` merges
  the attempt you pick.
- `--record <file>` keeps a real run, with every seat's output and every
  card. `--replay <file>` plays it back with the same renderer, and
  `telltale council replay-check <file>` reports what the file carries
  before you share it.

Every number comes from measured tool output, and an empty gauge means
telltale did not measure it.

**Sixty seconds.** Install on Windows with one paste:

```powershell
irm https://raw.githubusercontent.com/sanlee-ys/telltale/main/packaging/install.ps1 | iex
```

The script checks the archive against `checksums.txt`, and the binary is
not signed. [Install](#install) states every route.

Play a room back:

```
telltale council --replay examples/demo.jsonl --replay-speed 8
```

That file is a scrubbed recording of a real room, and it plays on a
machine with nothing installed.

Then read what this machine has:

```
telltale doctor
```

> A telltale is the ribbon on a sail. It shows the air. It does not interpret it.

[![CI](https://github.com/sanlee-ys/telltale/actions/workflows/ci.yml/badge.svg)](https://github.com/sanlee-ys/telltale/actions/workflows/ci.yml)

<!-- BADGE SLOT. One rule, and it is the honest-gauge rule wearing a different
     hat: a badge states a fact somebody measured, and it names who measured it.
     The CI badge above is GitHub rendering GitHub's own run result, so it needs
     no third-party host and cannot go stale.
     Allowed here later: a directory-inclusion badge, once that listing has
     actually merged. Never here: a star count, a download count, an install
     count, or any "used by" figure — telltale measures none of them, and a
     third-party render of an unmeasured number is the badge form of a rendered
     guess. See docs/design.md §8, the 2026-08-18 amendment. -->

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="images/telltale-council-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="images/telltale-council-light.svg">
    <img alt="telltale council dispatch room showing multi-agent panel" src="images/telltale-council-dark.svg">
  </picture>
</p>

<!-- HERO SLOT, for the animated capture that replaces or joins the still above.
     It is the last open piece of adoption item 1 (docs/design.md §8). Two
     conditions bind whatever lands here, and neither is negotiable by the
     session that lands it:
     1. The owner drives the eight beats. A scripted race is an invented
        recording (design.md §8, the recording chain).
     2. Every frame gets a review for workspace paths, session names and seat
        identity before the capture is committed (owner's ruling, 2026-08-17).
     The runbook is packaging/tape/README.md. No cast or GIF is in this
     repository today. -->

**v0.2.0** (2026-08-14). `main` carries work past that tag, and
[STATE.md](STATE.md) lists it. Windows is verified on every commit.
The crew work after the tag (per-seat turns, worktrees, `@auto`,
`--record`/`--replay`, the Unix host, three long-lived seats) is built and
tested offline; the live runs it owes are listed in [STATE.md](STATE.md).
A `darwin` CI job on Apple Silicon runs the suite and the binary smokes on
a runner with no vendor CLI; its first run (2026-09-02, on the crew PR) found
a socket path past macOS's 104-byte bound and a test that read a sentence
off screen, both fixed, and it is green since. Intel macOS is smoke-checked
by hand. `linux_amd64` is built, and a
source build was driven by hand on Linux with no vendor (`doctor`,
`council ls`, `replay-check`); the archive is not run.
No binary is signed. Check `checksums.txt` on the release.
Detail: [SECURITY.md](SECURITY.md). The v1 cut gates are in [docs/design.md §1](docs/design.md#s1).

## Install

This section states every install route, including the one paste from the
sixty-second path above.

**From source**

```
go build -o telltale.exe ./cmd/telltale
./telltale.exe doctor
```

A source build reports `dev` from `telltale version`. A release binary reports its tag.

**Windows, one paste** (measured against `v0.2.0`, 2026-08-18)

```powershell
irm https://raw.githubusercontent.com/sanlee-ys/telltale/main/packaging/install.ps1 | iex
```

It downloads the latest release, checks the archive against `checksums.txt`,
refuses on a mismatch, and puts `telltale.exe` on your user `PATH`.
It needs no administrator rights. The binary it installs is **not signed**,
and the script says so before it names the next command.
Source and knobs: [packaging/install.ps1](packaging/install.ps1).

**Windows, scoop** (exercised once, 2026-08-14)

```
scoop bucket add telltale https://github.com/sanlee-ys/telltale
scoop install telltale
```

**macOS, Homebrew** (the tap lives in this repository; not yet exercised by
a `brew install`, and the first one is owed)

```
brew tap sanlee-ys/telltale https://github.com/sanlee-ys/telltale
brew install telltale
telltale doctor
```

Homebrew fetches the release archive into its own cache and sets no
`com.apple.quarantine` mark, so Gatekeeper is never asked about the binary.
The binary is still **not signed**: the tap changes how it arrives, not what
it is. Apple Silicon gets `darwin_arm64`, Intel gets `darwin_amd64`, and
Linux gets `linux_amd64`. goreleaser rewrites
[Formula/telltale.rb](Formula/telltale.rb) at each tag; the one checked in
names `v0.2.0` with the sha256 values from that release's `checksums.txt`.

**macOS, curl** (measured on Intel macOS against `v0.2.0`, 2026-08-17; on
Apple Silicon write `darwin_arm64` for `darwin_amd64`, a substitution nobody
has walked by hand)

```
curl -fLO https://github.com/sanlee-ys/telltale/releases/download/v0.2.0/telltale_0.2.0_darwin_amd64.tar.gz
curl -fLO https://github.com/sanlee-ys/telltale/releases/download/v0.2.0/checksums.txt
shasum -a 256 -c checksums.txt --ignore-missing
tar -xzf telltale_0.2.0_darwin_amd64.tar.gz
./telltale doctor
```

There is no `xattr` line because `curl` writes no `com.apple.quarantine`
attribute and a browser does, and Gatekeeper acts on that attribute alone.
After a browser download, run `xattr -d com.apple.quarantine telltale` once
the checksum passes; inside the `curl` block that same line exits 1 because
there is nothing to remove. The measured walk is in [SECURITY.md](SECURITY.md).

**macOS, from source**

```
go build -o telltale ./cmd/telltale
./telltale doctor
```

**Direct download.** Each release attaches `windows_amd64`, `darwin_amd64`,
`darwin_arm64`, and `linux_amd64`, plus `checksums.txt`. Unpack one archive
and put `telltale` on `PATH`. The `curl` block above is that walk, measured.

**Windows, winget.** Not submitted. Use the one paste above, scoop, or a
source build. Draft: [packaging/](packaging/).

Run `telltale` with no arguments for the first frame.
`telltale doctor` is the preflight. `telltale council` opens the room.

Wire the statusline into Claude Code (`~/.claude/settings.json`):

```json
{
  "statusLine": {
    "type": "command",
    "command": "C:\\path\\to\\telltale.exe statusline"
  }
}
```

The same block works for Antigravity CLI
(`~/.gemini/antigravity-cli/settings.json`). telltale reads the vendor
from the payload `product` field.

Cursor CLI (`~/.cursor/cli-config.json`) needs `--vendor cursor`.
Its payload has no vendor name. This statusline is interactive only
(measured; [docs/design.md §7.16](docs/design.md#s7-16)):

```json
{
  "statusLine": {
    "type": "command",
    "command": "C:\\path\\to\\telltale.exe statusline --vendor cursor"
  }
}
```

Then start an interactive `cursor-agent` session and run `telltale hud`.

Wire the MCP server into a client once, so an agent can read the fleet:

```
claude mcp add telltale -- C:\path\to\telltale.exe mcp
```

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="images/telltale-hud-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="images/telltale-hud-light.svg">
    <img alt="telltale TUI HUD showing cross-vendor agent telemetry" src="images/telltale-hud-dark.svg">
  </picture>
</p>

The test suite emits that picture from the HUD render.
Empty cells are fields no vendor writes. A `~` marks an estimate.
`↑`/`↓` move the selection. `enter` opens the detail pane. `/` filters rows.

HUD flags: `--vendor all|claude|codex|gemini|agy|cursor|grok|pi|self-reported`,
`--hide gemini,cursor` (default from `TELLTALE_HUD_HIDE`),
`--ascii` (`TELLTALE_ASCII=1`), `--no-title`. `NO_COLOR` is honoured.

## First five minutes

Run `telltale doctor` first. It reports what is installed here, it probes no
login and makes no network call, and it **exits 0 even when every seat is
missing**. Read its own words before you read this table: each row below is
keyed to a line `doctor` actually prints.

| What you see | What it means | What to do |
|---|---|---|
| `0 checks passed`, and `no seat above passed every check that ran` | telltale is working. `council` drives a vendor CLI, and this machine has none. | Install one vendor CLI and run `doctor` again. `telltale hud` runs either way. |
| `binary FAILED  not found on PATH (looked for codex)` | This shell cannot resolve that vendor. | Open the shell you normally run the vendor in, or put its binary on `PATH`. `doctor` also finds a vendor at a known install location and says `a known install location, not on this shell's PATH`. |
| `drivable FAILED` under a `binary ok` | The binary is here and council will not seat it. "Is it there" and "can it be driven" have different fixes, so `doctor` refuses to collapse them. | Read the reason on that row. It names the entry point and why: usually a shell shim that takes its prompt as an argument, which council will not put through `cmd.exe`. |
| `auth  not checked` and `network  not checked`, on every seat, always | Not a failure and not a soft pass. This report probes neither. | Nothing. A seat that is installed and signed out reports its own auth failure on its column the first time you dispatch to it. |
| `re-measure §3.x before trusting the fields this adapter sources` | Your vendor runs a version other than the one telltale surveyed. | Nothing on this machine. It is a staleness fact about telltale: no check failed, the tally is unchanged, and the command still exits 0. |
| `telltale version` says `dev`, or an older tag, after the install | Another `telltale.exe` is earlier on `PATH`. The install script appends its directory rather than jumping the queue. | Run `Get-Command telltale` (`which -a telltale` on macOS). It names the one that runs. Remove the other one, or set `TELLTALE_INSTALL_DIR` to the directory it already lives in. |
| A column in the room stays empty after a dispatch | The seat answered nothing, or the vendor refused the turn. | The column carries the reason. [docs/council.md](docs/council.md) reads the badges and the phase words. |
| Windows warns before the first run | The binary is unsigned. No telltale release carries an Authenticode signature ([docs/design.md §8](docs/design.md#s8), item 8). | Verify the archive against `checksums.txt`, which is the whole verification this release offers. [SECURITY.md](SECURITY.md) states what that does and does not prove. |
| The statusline shows nothing, or `bad statusline input: unexpected end of JSON input` | The statusline is wired, not run. The vendor calls it and hands it JSON on stdin, so by hand it gets no payload. | Paste the `statusLine.command` block above, then start a session. |

`telltale doctor` output pastes into an issue as it stands: it is plain text
with no colour and no alternate screen, for exactly that reason.

## What it is

- **`telltale council`:** five vendor seats working as a crew, one column
  each. This is the product.
- **`telltale statusline`:** model, context, prompt-cache hit rate, session
  cost, and quota pacing from the JSON the vendor sends on stdin. No network.
  No credential read.
- **`telltale hud`:** a watch TUI over Claude Code, Codex, Gemini CLI,
  Antigravity CLI, Cursor (Composer and the `cursor-agent` CLI), Grok CLI,
  and Pi.
- **`telltale snapshot`:** the same scan as JSON, for a program.
- **`telltale mcp`:** the same document over MCP on stdio, for an agent.
- **`telltale history`:** what one vendor spent, day by day and project by
  project, re-read from that vendor's own session files. Claude today; the
  other six are surveyed and named on every run, with the reason each is not
  read yet. It never sums two vendors.
- **`telltale doctor`:** which vendor binaries this machine has.
- **`telltale events`** / **`telltale events view`:** a loopback hook sink
  and its reader.
- **`telltale hook`** / **`telltale otel`:** relays that write per-turn
  token totals under `~/.telltale/`. They print nothing.
- A documented adapter interface, plus a drop-file relay for a tool with
  no adapter. Spec: [docs/dropfile.md](docs/dropfile.md).

Gauges read vendor files on this machine. They make no network calls.
They write keys and numbers under `~/.telltale/` only.
`telltale council` is the one mode that starts vendor CLIs.
The Cursor store holds tokens in the same SQLite file as session state.
The adapter does not read them. [SECURITY.md](SECURITY.md) states the
boundary.

## The crew room

```
telltale.exe council
```

An unaddressed brief goes to Claude. `@codex`, `@agy`, `@cursor`,
`@grok`, and `@all` route a turn. `-@claude` addresses every seat but
that one. A seat takes its brief while the other seats are still
answering theirs; a busy seat is refused by name, and `ctrl+c` cancels
the seat you are looking at. `--read` opens a room that only talks.
`--cd` sets the workspace. A plain `telltale council` can write, and the
header says so.

In a writing room every seat works in its own git worktree, cut once
beside the workspace on `seat/<vendor>`. The room is the integrator:
`/adopt codex` merges that branch behind a y/n card, `/hand claude codex`
puts one seat's patch into another seat's brief, and `/flow … & @seat …`
fans a stage across seats and waits on all of them. `--shared-tree` is
the older room.

Every seat is a live process. Claude asks before every tool call,
measured, and wears `gated`. Codex and Grok can carry an approval request
into the same card on their live shapes, which were read from vendor
documentation and not yet driven: their badges say `unmeasured` until a
run on the reference box says otherwise. A live shape whose handshake is
refused falls back to the batch invocation that was measured, and the
column says so.

The strip under the header is the inbox: `⚠ NEEDS YOU` names the seats
stopped on a card and the seats whose turn ended while you were reading
another column; `.` goes to the next one. `@auto` routes a brief to the
seated idle seat with the most measured headroom in its shortest quota
window, from the same relay the badges read, and refuses when no seat has
a reading.

`--record <file>` keeps a real run, every seat's output and every card
with its timing; `--replay <file>` plays it back with the same renderer
on a machine with nothing installed, labelled `REPLAY` on every frame;
`telltale council replay-check <file>` lists what the file carries
before it is shared. No recording of a real five-seat room exists yet.

`--host` opens a read room in a process that outlives the terminal, on
Windows, macOS, and Linux; `/detach` walks away, `telltale council`
rejoins, `telltale council kill` ends it. Measured on Windows and Linux;
built for the Mac.

[docs/council.md](docs/council.md) is the room guide: badges, routing,
keys, and flags. [docs/design.md §9](docs/design.md#s9) is the measured
record per vendor, and §9.54 through §9.57 and §7.30 are the crew.

## `telltale snapshot`

```
telltale.exe snapshot
```

One scan, one JSON document on stdout, exit 0. Flags: `--vendor <id>`,
`--compact`, `--timeout <dur>` (default 10s). An unknown flag or vendor
prints no document.

| the document says | it means |
|---|---|
| `"cost_usd_total": 0` | measured zero |
| `"cost_usd_total": null` | no reading right now |
| `"unsupported": ["cost"]` | this vendor never exposes that field |
| `"estimated": ["context_pct"]` | the adapter computed the value |
| `"quota": []` | no relayed account reading |

The document holds numbers and keys. It holds no session names, paths,
or reply text. Schema: [docs/design.md §7.22](docs/design.md#s7-22) and
[docs/snapshot.schema.json](docs/snapshot.schema.json).

[`tools/fleet-prompt.ps1`](tools/fleet-prompt.ps1) is one consumer:

```
. .\tools\fleet-prompt.ps1
Get-TelltaleFleetLine
```

## `telltale mcp`

```
claude mcp add telltale -- <path>\telltale.exe mcp
```

The same document, served to an agent over the Model Context Protocol on
stdio. You do not type this command: an MCP client starts it. One tool,
`fleet_snapshot`, takes an optional `vendor` argument and returns the
document above — the same bytes, so every rule in that table holds here.
One flag: `--timeout <dur>` (default 10s), per call.

It speaks stdio only. It binds no port, calls no network, and writes
nothing. [docs/design.md §7.25](docs/design.md#s7-25) states the surface
and what is not verified.

## `telltale events`

```
telltale.exe events
telltale.exe events view
```

One hook event per POST on loopback, appended under `~/.telltale/events/`
and rebroadcast on `/stream`. Default bind: `127.0.0.1:4519`. Any other
host is refused. This store holds payload content.
`telltale events view` reads the files. No gauge reads them.
Flags and the boundary: [docs/design.md §7.21](docs/design.md#s7-21).

## `telltale doctor`

```
telltale.exe doctor
```

Which vendor binaries are on this machine, where each one was found, and
what version each one reports. Auth and network always read `not checked`.
The command runs `<binary> --version` and writes nothing.

## The honest-gauge rule

A segment may only display a value read from tool or vendor output.
Anything inferred is omitted or marked as an estimate.
A gauge that cannot tell "no data" from "zero" fails the build.
`internal/hud` renders one session at 0% and one session with no context
source, and asserts the two rows differ.

Fixtures are synthesized. This repository holds no real session content.

## Design

- [docs/council.md](docs/council.md): the room in use
- [docs/design.md](docs/design.md): segments, adapters, HUD, council record
- [STATE.md](STATE.md): what is in flight
- [PARITY.md](PARITY.md): what differs across machines
- [SECURITY.md](SECURITY.md): trust model, signing, checksums

## License

MIT
