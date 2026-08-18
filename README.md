# telltale

A dispatch room for five vendor CLIs. One brief, answered side by side
by Claude Code, Codex, Antigravity, Cursor, and Grok.
A statusline and a HUD sit under the room.
Every number comes from measured tool output.

> A telltale is the ribbon on a sail. It shows the air. It does not interpret it.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="images/telltale-council-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="images/telltale-council-light.svg">
    <img alt="telltale council dispatch room showing multi-agent panel" src="images/telltale-council-dark.svg">
  </picture>
</p>

**v0.2.0** (2026-08-14). Windows is verified on every commit.
Intel macOS is smoke-checked. `darwin_arm64` and `linux_amd64` are built, not run.
No binary is signed. Check `checksums.txt` on the release.
Detail: [SECURITY.md](SECURITY.md). The v1 cut gates are in [docs/design.md §1](docs/design.md#s1).

## Install

**From source**

```
go build -o telltale.exe ./cmd/telltale
./telltale.exe doctor
```

A source build reports `dev` from `telltale version`. A release binary reports its tag.

**Windows, scoop** (exercised once, 2026-08-14)

```
scoop bucket add telltale https://github.com/sanlee-ys/telltale
scoop install telltale
```

**Direct download.** Each release attaches `windows_amd64`, `darwin_amd64`,
`darwin_arm64`, and `linux_amd64`, plus `checksums.txt`. Unpack one archive
and put `telltale` on `PATH`.

Measured on Intel macOS against `v0.2.0` (2026-08-17):

```
curl -fLO https://github.com/sanlee-ys/telltale/releases/download/v0.2.0/telltale_0.2.0_darwin_amd64.tar.gz
curl -fLO https://github.com/sanlee-ys/telltale/releases/download/v0.2.0/checksums.txt
shasum -a 256 -c checksums.txt --ignore-missing
tar -xzf telltale_0.2.0_darwin_amd64.tar.gz
./telltale doctor
```

A browser download on macOS sets `com.apple.quarantine`. After the checksum
passes, run `xattr -d com.apple.quarantine telltale`. Do not add that line
to the `curl` block: `curl` does not set the mark, and the command then
exits 1. The measured walk is in [SECURITY.md](SECURITY.md).

**Windows, winget.** Not submitted. Use scoop or a source build.
Draft: [packaging/](packaging/).

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

HUD flags: `--vendor all|claude|codex|gemini|agy|cursor|grok`,
`--hide gemini,cursor` (default from `TELLTALE_HUD_HIDE`),
`--ascii` (`TELLTALE_ASCII=1`), `--no-title`. `NO_COLOR` is honoured.

## What it is

- **`telltale council`:** one brief, five vendor columns. This is the product.
- **`telltale statusline`:** model, context, session cost, and quota pacing
  from the JSON the vendor sends on stdin. No network. No credential read.
- **`telltale hud`:** a watch TUI over Claude Code, Codex, Gemini CLI,
  Antigravity CLI, Cursor (Composer), Grok CLI, and Pi.
- **`telltale snapshot`:** the same scan as JSON, for a program.
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

## The dispatch room

```
telltale.exe council
```

An unaddressed brief goes to Claude. `@codex`, `@agy`, `@cursor`,
`@grok`, and `@all` route a turn. `-@claude` addresses every seat but
that one. `--read` opens a room that only talks. `--cd` sets the
workspace. A plain `telltale council` can write, and the header says so.

[docs/council.md](docs/council.md) is the room guide: badges, routing,
keys, and flags. [docs/design.md §9](docs/design.md#s9) is the measured
record per vendor.

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
