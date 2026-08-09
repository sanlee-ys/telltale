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
| **Cursor** | **no sandbox request at all**, and **no workspace-trust screen** | Both are properties of the ACP server this seat now runs on, not of the platform: the protocol has no sandbox parameter and no trust step. The old row said `--sandbox enabled` kills the turn on Windows — true of print mode, verified 2026-08-04 against 2026.07.23-e383d2b, and no longer a flag council passes on any OS. Trust is the sharper half: verified 2026-08-08 against 2026.08.04-aaa8809, a directory print mode refused with "⚠ Workspace Trust Required" was written to over ACP with no prompt. |
| **Antigravity** | same as elsewhere | `unsandboxed` on every platform — it was asked to write a file under both of its own read-only flags and wrote it. Refuted, not unverified. |
| **Grok** | **measured here, unverified elsewhere** | `unsandboxed`, on two different kinds of evidence. `--permission-mode plan` was REFUTED: asked to write a file under it, grok 1.0.0 (3cd0d0cbce) wrote the file, exactly as the control run without it did. `--sandbox` is not refuted but UNOBSERVABLE: handed `bogus-profile-xyz` it neither errored nor warned and answered normally at exit 0, so council has no way to tell a real profile from a typo and asks for neither flag. Nothing in the invocation is platform-specific, so a macOS run is *expected* to work and has not been shown to. |

**Cursor's ACP seat is unverified off Windows, 2026-08-08.** Every one of the
thirteen arms behind that seat ran on Windows 11 against cursor-agent
2026.08.04-aaa8809. Nothing in the invocation is platform-specific — it is the
single subcommand `acp`, and detection already resolves the native entry point
per OS — so a macOS run is *expected* to work and has not been shown to. What is
worth checking there specifically, because each one is a claim the badge makes:
that `session/load` reloads a thread into a new process; that `session/set_mode`
`plan` is accepted and refuses a write; that `session/request_permission` fires
for a shell command and not for an edit; and that workspace trust is absent on
that path too, since the Mac is where print mode's trust prompt was least likely
to be hit. Record what you find here rather than in `docs/design.md §9.36`, which
is the Windows capture and should stay one.

**The Grok seat is unverified off Windows, 2026-08-09.** Every measurement behind
that seat ran on Windows 11 against grok 1.0.0 (3cd0d0cbce), signed in against
grok.com rather than an API key. Nothing in the invocation is platform-specific —
`--output-format streaming-json` plus a trailing `-p` — so a macOS run is
*expected* to work and has not been shown to. Four things are worth checking
there specifically, because each is a claim the seat makes: that
`grok --sandbox <nonsense> -p "hi"` is silently accepted there too (the whole
reason council passes no sandbox flag); that `--permission-mode plan` still
writes the file; that the installer's POSIX path guess in `grokKnownPaths` —
`~/.grok/bin/grok` — is where the binary actually lands; and that `grok` resolves
to a native executable rather than a shell shim, since the argv transport for
the brief depends on it. `go test ./internal/council/vendors -tags=live -run
TestLiveGrok` is the one command that exercises the invocation end to end.
Record what you find here rather than in `docs/design.md` §9.39, which is the
Windows capture and should stay one.

**Windows launch-parent trap when driving cursor-agent by hand.** Launched from a
Git Bash parent, this machine's `PreToolUse` credential-guard wrapper fails closed
and every cursor-agent tool call comes back "Hook blocked with message: … syntax
error near unexpected token `&`". It looks exactly like a broken vendor and is
not; it cost an arm of the §9.36 capture. Drive cursor-agent from a PowerShell or
cmd parent on Windows. Upstream wrapper bug, agent-ops ADR-012.

**The symlink refusal now runs on Windows — Developer Mode is the prerequisite.**
`TestSeedSymlinksAreNamedNotFollowed` (`internal/council/arena_seed_test.go`)
pins `.worktreeinclude` seeding's refusal to follow a symlink. Creating a
symlink needs `SeCreateSymbolicLinkPrivilege`, which an ordinary Windows shell
does not hold, so this box skipped it with *"A required privilege is not held by
the client"* until **Developer Mode was enabled on 2026-08-09; it then PASSED,
measured on this workstation.** The sting worth keeping is why it mattered:
design.md §9.37's stated reason for refusing symlinks is *"Windows is primary
and symlink semantics differ per platform"* — so until that day, the platform
the claim is about was the only platform never checking it.

Two mechanics for whoever hits this on another box. **No new shell or logon is
needed**: Go's `os.Symlink` retries with `SYMBOLIC_LINK_FLAG_ALLOW_UNPRIVILEGED_CREATE`,
which reads Developer Mode at runtime rather than reading a token privilege
granted at logon — an already-running terminal picks it up immediately. And
**an elevated shell also passes** (Administrators hold the privilege) but does
not close this gap, because the point is that the test runs in ordinary use.

Still not visible: whether CI's `windows-latest` job skips it, because that job
runs `go test` without `-v`. It runs for real on the `ubuntu-latest` race job.

**One test still skips on Windows, and this one is legitimate.**
`TestSavedRoomIsNotWorldReadable` (`internal/council/resume_test.go:446`) skips
with *"posix file modes are not the access control on windows"* — measured
2026-08-09 in a full `go test ./... -count=1 -v` pass, where it is the **only**
skip in the entire suite. The mechanism is correctly skipped; POSIX modes really
are not how Windows controls access. The residue is that the *property* — a
saved `room.json` is not world-readable — is therefore only ever asserted on
platforms that are not the primary target. Recorded as a measured gap, not as a
defect: writing the ACL-based equivalent is a judgement about whether that
property needs a Windows assertion at all, and nobody has made it.

**Measuring skips at all needs `-count=1`.** `go test` caches passing packages
and replays their output, so a `-v` run over cached results can report a
different skip count than the same command uncached — observed here the same
day, one run reporting a skip and the next reporting none with nothing changed
in between. A skip census over a cached suite measures the cache.

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

## `telltale doctor`

Same shape as the adapters: every version probe behind the doctor report was
measured on this Windows box only (2026-08-09) — five installs, five
`--version` answers. Path resolution reuses council's own detection, portable
by construction, but no macOS run is recorded, and cursor's POSIX entry point
taking the bare `--version` is true by construction there, not by measurement.
Treat a wrong-looking doctor row on the Mac as unverified rather than broken,
and record what you find here.

## Terminal profile

The same frame reads better on macOS than on Windows, and the reason is not a
platform difference in telltale — there is one code path, one glyph set, and
Windows Terminal is the declared reference environment. macOS terminals ship
with more generous leading and a softer rasterizer. What closes most of that
gap is the terminal's own configuration, and it is usually untouched.

This profile is the tuned reference. It is additive: keep your existing
profiles and open this one to compare.

```json
{
    "name": "telltale council",
    "commandline": "pwsh.exe -NoExit -Command \"telltale council\"",
    "font": {
        "face": "JetBrainsMono Nerd Font Mono",
        "size": 12,
        "weight": "medium",
        "cellHeight": "1.25"
    },
    "padding": "16, 12, 16, 12",
    "antialiasingMode": "grayscale"
}
```

Why each of those, since none of it is obvious:

- **`cellHeight: "1.25"`** is the one that matters. It is the leading macOS
  gives you by default and Windows Terminal does not, and a grid of character
  cells with no air between rows is most of what "cramped" means. Requires
  Windows Terminal 1.19 or newer.
- **`weight: "medium"`** compensates for DirectWrite drawing thinner stems than
  CoreText at the same nominal weight. This is the closest a setting gets to
  the rasterizer difference; it does not eliminate it.
- **`antialiasingMode: "grayscale"`** rather than ClearType, whose subpixel
  fringing is what reads as "digital" against a macOS capture.
- **`padding`** buys margin the frame itself should not have to spend cells on.

**Do not reach for a smaller font to fit more in.** `TestFrameCorpusReportsFill`
in `internal/council` measures this: an idle four-seat room occupies about six
rows *regardless of window height*, so every additional row of terminal is one
more row of rules drawn around nothing — 75% of the frame at 24 rows, 90% at
60. Shrinking the font makes an idle room emptier, not denser. Going up a size
is the counterintuitive but measured direction.

**What this cannot fix.** DirectWrite is not CoreText and a TUI cannot reach
past its terminal's rasterizer. The bar here is the best native result on each
platform, not pixel parity with the Mac; anything claiming otherwise is
selling a screenshot.

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
