# telltale

**An honest gauge for your coding agents.** A dispatch room where several vendor CLIs
answer one brief side by side, each column claiming only what was measured about that
vendor — standing on a statusline and a cross-vendor HUD where every number is traceable
to measured tool output, nothing narrated, nothing guessed.

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

**Status: pre-v1, under active development.** All three modes are built.

**v1 is a snapshot, not a freeze.** It cuts when three checkable gates hold — nothing on
the surface is half-finished or unused by its own author, the README is verified true
against the code, and no breaking change to the grammar or keymap is planned — not when
development goes quiet, because it doesn't: this room is driven daily. Cutting v1 as
gauges only — statusline and HUD, with declared vendor version pins — was the standing
alternative and it is rejected: a release names the product, and the product is the room.
The gates and the argument are in [docs/design.md §1](docs/design.md).

Every adapter here was verified against something real rather than against vendor docs — a
live on-disk corpus, a live payload capture, or the vendor's own persistence code read at a
pinned version — and [docs/design.md §3](docs/design.md) itemizes per vendor which of those
it was, what the verification changed about this project's guesses, and what each one still
owes. **Cursor (Composer)** is the first IDE-resident agent here, and the first whose store
also holds live credentials, which is why that adapter's most load-bearing property is the
list of things it does not read ([docs/design.md §3.9](docs/design.md)). **`telltale
council`** seats the 4-vendor fleet, and every sandbox and streaming claim in it was
measured against a live run of that CLI rather than read off its `--help`
([docs/design.md §9](docs/design.md)).

## Install

**First release pending.** v1 cuts when the snapshot gates hold (above), so nothing is
tagged yet and the two package-manager lines below do not resolve today. They are
written down rather than promised later because the packaging is built and exercised:
`.goreleaser.yaml` and `.github/workflows/release.yml` cut all four binaries, the
checksums and the scoop manifest from a `v*` tag, and the whole path has been run end
to end in snapshot mode. Until the tag, build from source — one command, and it needs
nothing but Go.

**From source** — works now:

```
go build -o telltale.exe ./cmd/telltale
./telltale.exe council
```

**Windows, scoop** — once the first release is published:

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
applied to a platform ([docs/design.md §8](docs/design.md)).

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
dollars on disk. None of those is rendered as a zero. Read the `CU` row against the `CX`
row directly above it: both show a context bar, and only the Codex one is marked an
estimate, because Cursor writes its own percentage down and telltale reads it. The `AG`
row is labelled by its conversation id for a different reason: Antigravity writes no
session title anywhere, and the only free text on its disk is your prompts, which
telltale will not read.

In the HUD, `↑`/`↓` move the selection and `enter` opens a detail pane for it — quota
windows, the vendor extras, and the session's own diagnostics and degraded-field marks,
plus a line naming the fields that vendor **cannot** source at all. That last line is the
answer to "why is this cell empty?", and it is the difference between "we have no value
right now" and "this vendor never had one". `/` narrows the rows by name or path.

`telltale hud` flags: `--vendor all|claude|codex|gemini|agy|cursor`, `--ascii` (also
`TELLTALE_ASCII=1`), `--no-title`. `NO_COLOR` is honoured through the standard
mechanism.

## What it is

- **A dispatch room** — `telltale council`, one brief typed once and answered by Claude
  Code, Codex, Cursor and Antigravity side by side. The one mode that spawns vendor CLIs
  instead of reading their files; it gets its own section below.
- **In-prompt statusline for Claude Code** — model, context %, session cost, and quota
  pacing (`rate_limits` windows), rendered from the JSON Claude Code hands your
  statusline command on stdin. No network calls, no credential reads.
- **A watch-mode HUD (TUI)** for parallel sessions — the cross-vendor surface, and a
  first-class UI investment (Go + Bubble Tea/Lipgloss; Windows Terminal is the reference
  environment). Ships with adapters for **Claude Code**, **Codex CLI**, **Gemini CLI**,
  **Antigravity CLI** and **Cursor (Composer)**, each reading that vendor's own native
  on-disk data — for Antigravity and Cursor that means a read-only SQLite reader written
  into this repo rather than a 9 MB dependency added to it.
- **A documented adapter interface** — one module per vendor — so you can wire in
  anything else that leaves session data on disk. The worked example in
  [docs/design.md §4a.7](docs/design.md) is the method the Gemini adapter was actually
  built with, kept alongside what live verification changed about its guesses.

One binary, three modes: `telltale statusline`, `telltale hud` and `telltale council` —
and they are not three co-equal products. **The room is the product; the gauges are the
infrastructure under it.** The statusline and the HUD are where this project surveyed each
vendor's seam and wrote down what it found, and the honest-gauge rule they were built
under is the rule a council column inherits when it states a sandbox posture. They are
finished, they are load-bearing, and they are not the thing this is for. The statusline
code path never initializes the TUI framework (the single binary links it, but no Bubble
Tea code runs on a statusline invocation).

Honest claim, stated precisely: *dispatch across the 4-vendor fleet (Claude Code, Codex,
Cursor, Antigravity); cross-vendor monitoring; vendor-native statusline where the seam
exists — and it exists twice: Claude Code and Antigravity CLI.* (Codex CLI has
no statusline hook today. Antigravity was statusline-only until a re-survey found the
transcript its own docs advertise, which is what made its HUD adapter buildable — the
first verdict and the reversal are both in [docs/design.md §2.1/§3.8](docs/design.md).
Cursor reaches telltale both ways: a built-in HUD adapter because its seam is on disk,
and a council seat driven through `cursor-agent`'s own bundled `node.exe` — the `cursor`
binary on PATH is only the editor launcher and council never drives it.)

**The gauges never write to anything that isn't theirs.** `telltale statusline` and
`telltale hud` read vendor files, make no network calls, read no credentials, and no
keybinding can mutate vendor state or send anything to a running agent. What telltale
writes to disk is three files of its own under `~/.telltale/`, all keys and numbers,
never content: council's room file (`council/room.json` — the vendor session ids
reattaching needs and the room's workspace, no transcript, output or brief content),
the statusline's quota relay (`quota/<vendor>.json` — the rate-limit windows it
just rendered, so the HUD can show account quota per vendor instead of only for
vendors whose stores carry it; [docs/design.md §7.15](docs/design.md)), and the
cursor token relay (`usage/<vendor>.json` — a running total of the token counts
Cursor's `afterAgentResponse` hook reports per turn, so the HUD can say what this
machine spent; [docs/design.md §7.16](docs/design.md)). That last one is spend, not
quota: there is no denominator anywhere in it, so it never renders as a percentage
or a bar, and Cursor's account quota stays visibly absent. `telltale
council` remains the one mode that acts on the world, and it is labelled as one
everywhere it can be: it spawns vendor CLIs, it is entered only by typing the
subcommand, it is not reachable from the HUD, and it shares no keybinding with it.
"Reads no credentials" stopped being free with the Cursor adapter — that vendor
keeps its access tokens, refresh tokens and OAuth secrets in the *same SQLite file* as
its session state — so it is enforced there as a read allowlist with a test that plants
credential-shaped strings in the fixtures and asserts none of them reaches anything the
HUD can display ([docs/design.md §3.9](docs/design.md)). The Cursor *hook* is the same
discipline against a different hazard: its payload carries the model's reply text and
the user's email address beside the four numbers, so the parser's struct is the
allowlist and markers planted in a real payload shape must reach neither the parse nor
the file.

## The dispatch room

`telltale council` is one brief, typed once and answered by the 4-vendor fleet side by
side — **Claude Code**, **Codex**, **Antigravity**, and **Cursor**, each in its own column,
in your terminal. It exists because the alternative is four terminals and a clipboard.

```
telltale.exe council
```

Every column carries its own sandbox posture and its own streaming granularity, because the
four vendors differ on both and one blanket claim would be false for at least one of them —
and each of those claims was measured against a live run of that CLI rather than read off
its `--help`. A plain `telltale council` can write, and says so in the header for the whole
session; `--read` opens a room that only talks. But no badge is what keeps this room out of
your files — the directory it was pointed at is, and `--cd` is how you move it.

An unaddressed brief goes to Claude alone; `@codex`, `@agy`, `@cursor` and `@all` route a
turn, `-@claude` addresses everyone but that seat, and the composer prices the route before
you press enter. Each seat keeps its own conversation and rides that vendor's own native
resume rather than a re-sent transcript, so no session ever holds another's history.

**[docs/council.md](docs/council.md) is the room's own guide** — the frame, the badge
vocabulary, the routing grammar, the reading keys and the turn view, taking an answer out
of the room with `y`, and every flag. [docs/design.md §9](docs/design.md) is the record
behind it: what was measured per vendor, what each seam cost, and what is still unverified.

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
