# telltale

**An honest gauge for your coding agents.** A statusline and a minimal cross-vendor HUD
where every number is traceable to measured tool output — nothing narrated, nothing
guessed.

> A telltale is the ribbon on a sail that shows true airflow. It doesn't interpret;
> it just tells you what's actually happening.

**Status: pre-v1, under active development.** Nothing here is installable yet.

## What it will be

- **In-prompt statusline for Claude Code** — model, context %, session cost, and quota
  pacing (`rate_limits` windows), rendered from the JSON Claude Code hands your
  statusline command on stdin. No network calls, no credential reads.
- **A minimal watch-mode HUD (TUI)** for parallel sessions — the cross-vendor surface.
  Ships with adapters for **Claude Code** and **Codex CLI**, each reading that vendor's
  own native data (statusline JSON / transcripts; session JSONL / hook events).
- **A documented adapter interface** — one module per vendor — so you can wire in
  Gemini CLI, Cursor, or anything else that leaves session data on disk.

Honest claim, stated precisely: *cross-vendor monitoring; vendor-native statusline where
the seam exists.* (Codex CLI has no statusline hook today — see
[decisions/001](decisions/001-v1-scope.md).)

## The honest-gauge rule

A segment may only display a value read from tool or vendor output. Anything inferred is
either omitted or visibly marked as an estimate. Every segment's data source is named in
[docs/design.md](docs/design.md), and the eval harness asserts each segment's render
against fixture inputs — including empty and degraded states. A gauge that can't tell
"no data" from "zero" fails the build.

## Design & decisions

- [docs/design.md](docs/design.md) — segments, data sources, adapter contract
- [decisions/](decisions/) — ADR log

## License

MIT
