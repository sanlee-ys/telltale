# telltale — PARITY

Cross-platform and cross-machine status. Read this when you change machines and
something behaves differently than it did on the other one.

Two different things get called "parity" and only one of them is achievable:

- **Capability parity** — the same build, the same seats, the same postures.
  Achievable, and the checklist below is how you get it.
- **Session continuity** — picking up the same conversation on another machine.
  **Not achievable, by design.** See *What does not travel*.

## Capability parity checklist

1. **Build here.** `go build -o telltale.exe ./cmd/telltale` (drop `.exe` off
   Windows). The binary is per-platform; it does not travel.
2. **Open the room and read the fold line.** `telltale council` names any seat
   that folded out of the grid and why. A seat can be installed and still not
   drivable — that is the case worth catching.
3. **Copy the brief.** `--brief <path>` / `TELLTALE_COUNCIL_BRIEF` points at a
   file that lives outside the repo on purpose, so git does not carry it. Without
   it, every seat on the new machine is guessing at conventions the old one had.

## Per-vendor platform differences

These are measured, not read off `--help`. Where a claim rests on something
weaker than a live run, it says so.

| Seat | Windows | Notes |
|---|---|---|
| **Claude Code** | same as elsewhere | No known platform difference. |
| **Codex** | **`unsandboxed`** | `-s read-only` is not a read/write distinction on Windows — it is a seat that cannot spawn anything at all. Verified against codex-cli 0.146.0 on Windows 11: a sandboxed spawn fails with `CreateProcessAsUserW ... (Windows error 5)`, including one asked merely to list a directory. Both postures therefore pass `danger-full-access` on Windows, and the badge says `unsandboxed` rather than claiming a restriction that is not there. |
| **Cursor** | sandbox flag unusable | `--sandbox enabled` does not weakly apply on Windows; it kills the turn. Verified 2026-08-04 against cursor-agent 2026.07.23-e383d2b. |
| **Antigravity** | same as elsewhere | `unsandboxed` on every platform — it was asked to write a file under both of its own read-only flags and wrote it. Refuted, not unverified. |

**Cursor install location, Windows.** `cursor-agent` lives at
`%LOCALAPPDATA%\cursor-agent\cursor-agent.cmd` and is frequently absent from the
PATH of the shell running the detection. A seat that folds out as "not
installed" here is usually this. The `cursor` binary on PATH is only the editor
launcher and council never drives it.

## HUD adapters

Adapter path resolution is portable by construction — `os.UserHomeDir()` and
`os.UserConfigDir()`, which return the right root on all three platforms. The
gap is **verification, not portability**: every adapter's live-corpus pass was
run on Windows. No macOS or Linux corpus pass is recorded for any of the five.

That means a macOS run is expected to work and has not been shown to. Treat a
missing or empty adapter on macOS as unverified rather than broken, and record
what you find here.

## What does not travel between machines

- **The room.** `~/.telltale/council/room.json` holds each vendor's *session
  ids*, and those point into that machine's local session store. Copying the file
  does not copy the transcripts. Council already handles this honestly: a seat
  whose thread the vendor no longer has says the history is gone and starts
  fresh, briefed.
- **Flow artifacts.** `~/.telltale/council/artifacts/` is machine-local for the
  same reason.
- **The brief.** Deliberately outside the repo, so git does not carry it.

**The cross-machine carrier is `STATE.md` plus GitHub**, not room state. That is
what those are for.

## Flags worth knowing

`--write` is accepted and ignored — the room writes by default now. `--read`
opens a deliberation-only room in which no seat may touch the workspace. If you
are following older notes that say to pass `--write`, they are stale.
