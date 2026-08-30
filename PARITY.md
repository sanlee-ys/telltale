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
| **Codex** | **read `ro:enforced`, write `unsandboxed`** | Re-measured 2026-08-29 against codex-cli 0.149.1 on Windows 11: `-s read-only` now ENFORCES — a shell write came back `Access is denied.` at exit 1 with no file on disk, a read ran clean, and the resume override (`-c sandbox_mode="read-only"`) enforced the same. The read posture passes `-s read-only` on every OS again. Two Windows-only residues, both measured: the sandbox cannot spawn this machine's PowerShell (`CreateProcessAsUserW ... (Windows error 5)`, the same line 0.146.0 failed everything with) and turns complete because the model retries through cmd.exe, so a read turn can still fail to inspect when it does not retry; and `-s workspace-write` denies `.git` there and REFUSES the `writable_roots` override that unlocks it on macOS — so the write posture keeps `danger-full-access` on Windows, or the seat edits all session and never commits. The old row (both postures `danger-full-access`, verified at 0.146.0: every sandboxed spawn failed, reads included) is history as of 0.149.1; design.md §9.2's 2026-08-29 amendment carries the full capture. |
| **Cursor** | **no sandbox request at all**, and **no workspace-trust screen** | Both are properties of the ACP server this seat now runs on, not of the platform: the protocol has no sandbox parameter and no trust step. The old row said `--sandbox enabled` kills the turn on Windows — true of print mode, verified 2026-08-04 against 2026.07.23-e383d2b, and no longer a flag council passes on any OS. Trust is the sharper half: verified 2026-08-08 against 2026.08.04-aaa8809, a directory print mode refused with "⚠ Workspace Trust Required" was written to over ACP with no prompt. |
| **Antigravity** | same as elsewhere | `unsandboxed` on every platform — it was asked to write a file under both of its own read-only flags and wrote it. Refuted, not unverified. |
| **Grok** | **measured on both, and the platforms differ** | The seat is `unsandboxed` on both platforms. `--permission-mode plan` is REFUTED on both. grok 1.0.0 (3cd0d0cbce) wrote the file under that flag, and the control run without the flag also wrote it. Windows measured this on 2026-08-09 and macOS on 2026-08-14. The macOS run confirmed the file on disk and did not use the reply text. `--sandbox` DIVERGES. On Windows the flag is UNOBSERVABLE: given `bogus-profile-xyz`, grok printed no error and no warning, and it answered normally at exit 0. On macOS the same build validates the profile and fails closed. It prints `sandbox could not be applied`, then it prints `Refusing to start with its protections missing`, and it exits 1 with no turn. The macOS section below states what this result does and does not permit. **Re-measured on Windows at grok 1.0.4 (d846eb93d9) on 2026-08-14, and both results held.** The write landed again under `--permission-mode plan`, and `bogus-profile-xyz` again drew no error and exit 0. **The Mac was updated to 1.0.4 (d846eb93d94d) on 2026-08-17 and both probes were re-run there, with both results holding.** So this is a SAME-BUILD comparison again, on both halves, and the `--sandbox` divergence is a property of the operating system rather than of the release. See the note below. |

**`codex app-server` is unverified off Windows, 2026-08-29.** The row above
describes the SEATED path, `codex exec --json`, and it is the only codex path
the room dispatches. A second protocol ships parsed and unseated (§9.49), and
every one of its eight arms ran on Windows 11 against codex-cli 0.149.1. Two of
its findings are Windows-shaped and are the likeliest to differ on a Mac: the
tool router wrapping shell commands in `pwsh.exe`, which cannot start under the
Windows sandbox and left a read-posture seat unable to inspect; and the `.git`
deny under `workspace-write`, which the `exec` path's own row records behaving
differently on macOS. **On the Mac, run the probe before believing either.** The
protocol also exposes `windowsSandbox/readiness` and `windowsSandbox/setupStart`
as client requests — `readiness` answered `{"status":"ready"}` here and costs no
model turn — and whether those exist at all off Windows is unread.

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

**The Grok seat is now measured on macOS, 2026-08-14.** The machine is an Intel
x86_64 MBP on macOS 26.5.2. It ran grok 1.0.0 (3cd0d0cbcebe) **on that date**,
which was the build the Windows capture used. It runs 1.0.4 now; see the SETTLED
note below, which keeps these results and adds the same probes at 1.0.4. This file listed four questions as
unverified off Windows. All four now have an answer, and three of the four
match Windows:

| question | macOS answer |
|---|---|
| Does grok accept `--sandbox <nonsense>` silently here? | **No. This answer diverges from Windows.** grok prints a warning, refuses to start, and exits 1 with no turn |
| Does `--permission-mode plan` still write the file? | **Yes. REFUTED, as on Windows.** The run created `wrote.txt`, and a later check confirmed the file on disk |
| Does the binary land at `~/.grok/bin/grok`, the POSIX guess in `grokKnownPaths`? | **Yes.** That path is a symlink to `~/.grok/downloads/grok-macos-x86_64`. The installer also writes `~/.local/bin/grok`, which points at the same file and resolves first on PATH |
| Is `grok` a native executable and not a shell shim? | **Yes.** `telltale doctor` reports `drivable ok … a native executable`, so the argv transport for the brief holds here |

`go test ./internal/council/vendors -tags=live -run TestLiveGrok` PASSED on this
machine in 17.82s, over two turns that reused one session id. That command
exercises the whole invocation, and not one flag at a time.

**What the `--sandbox` result permits, and what it does not.** It does not change
the flags that council passes today. Council passes no sandbox flag because
`--sandbox` is not dependable across the platforms of the fleet. A flag that
fails closed on one OS and does nothing on another is still not a posture that
the badge can state. The seat stays `unsandboxed` on both platforms, because
`--permission-mode plan` is the flag that restricts writes, and both platforms
refute it.

The result does change the stated REASON. "grok cannot tell a real profile from
a typo" is a fact about Windows, not a fact about grok. Any text that uses that
sentence as the whole justification is now half true. A per-platform code path
for the macOS validation is a design decision. Nobody has made that decision,
and this file does not make it.

**SETTLED 2026-08-17: the `--sandbox` divergence is the PLATFORM, not the
version.** The machines went out of step on 2026-08-14, when Windows was found on
grok **1.0.4 (d846eb93d9)** and the Mac was still on 1.0.0. That made the
divergence a different-build comparison, and it could not rule out a version
difference as the cause. This file named the experiment that would settle it, and
the Mac has now run it.

The Mac was updated 1.0.0 → **1.0.4 (d846eb93d94d)** with grok's own
`grok update`, so **both machines now run the same build**, and both probes were
re-run there:

| probe, at 1.0.4 on macOS | result |
|---|---|
| `grok --sandbox bogus-profile-xyz -p …` | **Still fails closed.** Same warning, same `Refusing to start with its protections missing`, exit 1, no turn |
| `grok --permission-mode plan -p …` | **Still refuted.** `wrote.txt` confirmed on disk afterwards, not read out of the reply |

So the `--sandbox` behaviour tracks the operating system and not the release.
Windows accepts a nonsense profile at both 1.0.0 and 1.0.4; macOS validates and
refuses at both. The row above states this as a platform difference rather than a
version one, and it is now a same-build comparison on both halves.

`go test ./internal/council/vendors -tags=live -run TestLiveGrok` PASSED again at
1.0.4 (19.65s), so the update did not break the invocation the seat depends on.

**The `grokKnownPaths` guess survived the update, which is the part worth
keeping.** After updating, `~/.grok/bin/grok` points at
`../downloads/grok-1.0.4-macos-x86_64` — the download filename now carries the
version, but the `bin/grok` path that detection looks for is stable across an
update. An updater that moved that path would fold the seat out silently.

Still unrun on the Mac at any build: the wire capture, and `--resume` against
1.0.4's optional-value spelling.

Record further findings here. Do not record them in `docs/design.md` §9.39,
which is the Windows capture and must stay one.

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

## Killing the seats when the room dies abnormally

**Measured 2026-08-17**, on the Mac (Intel x86_64, macOS 26.5.2), against
bubbletea v2.0.8. This is the sharpest platform difference council has, because
the platform that behaves worse is the one that fails SILENTLY: five agents keep
running, holding sessions and spending quota, with no room attached and nothing
on screen to say so.

The two platforms bound a seat's lifetime by different mechanisms, and only one
of them is a lifetime:

| platform | mechanism | does the seat die when telltale dies? |
|---|---|---|
| Windows | Job Object, `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` | **yes, always** — the handle closes with the process and Windows reaps the tree, on every way out including the ones no handler can catch |
| macOS, Linux | process group, `Setpgid` | **no** — a group is a name for a set of processes, not a lifetime. It dies when something signals it and at no other moment |

So on unix the kill has to be MADE on the way out, and until this was measured
nothing made it on any signal. `runner/proc_unix.go` claimed the "same guarantee
the Windows job object gives"; it gives half of it, and the corrected comment
there now says which half.

### Method, so the numbers can be re-taken

A throwaway Go program: a Bubble Tea v2.0.8 model, plus one `sleep 600` child
started with `SysProcAttr{Setpgid: true}` — the shape `runner/proc_unix.go`
gives every seat. Teardown was reachable only from a key handler, which is how
the shipped room was written. Each run signalled the program, waited two
seconds, and asked whether the child's pid was still alive.

| signal | what Bubble Tea did to the program | did the model's `Update` run? | the child |
|---|---|---|---|
| `SIGINT` | `p.Run()` returned `program was killed: program was interrupted` | **no** | **orphaned** |
| `SIGTERM` | `p.Run()` returned `nil` | **no** | **orphaned** |
| `SIGHUP` | nothing — the default disposition killed the process outright | **no** | **orphaned** |
| `SIGKILL` | nothing, and nothing can | **no** | **orphaned** |

Bubble Tea does end the program on two of those four, and ending the program is
the whole of what it does. Its handler answers above the model's head — `tea.go`'s
event loop returns on `QuitMsg` and `InterruptMsg` *before* it calls
`model.Update` — so a model can never be handed the message, and council's
teardown never ran.

`internal/council/signals_unix.go` now runs teardown on the three catchable
signals before the room goes out. The same probe with that handler installed
reaped the child on all three.

### What is still true after the fix

- **`kill -9` still orphans every seat on macOS and Linux.** SIGKILL is
  uncatchable, so no handler can cover it, and this is a real difference from
  Windows rather than a bug worth filing. If a room is ever `kill -9`'d, check
  for surviving vendor processes by hand.
- **A Windows console close is a different mechanism** — `CTRL_CLOSE_EVENT`
  through `SetConsoleCtrlHandler`, not a POSIX signal — and it is unmeasured.
  It does not need a handler for the seats' sake: the job object covers them
  through it too.
- **The unix behaviour is measured on macOS only.** Linux shares the
  `Setpgid` code path and the same Bubble Tea build, so it is expected to match
  and has not been shown to. Record a Linux run here.

## HUD adapters

Adapter path resolution is portable by construction — `os.UserHomeDir()` and
`os.UserConfigDir()`, which return the right root on all three platforms. The
gap is **verification, not portability**: every adapter's live-corpus pass was
run on Windows. No macOS or Linux corpus pass is recorded for any of the six —
grok joined the list on 2026-08-09 (design.md §3.9a), surveyed against 30
sessions on the Windows box and nowhere else. Pi joined on 2026-08-16
(design.md §3.9b live pass): four probe sessions on this Windows box,
pi 0.84.1. No macOS or Linux corpus pass. Its root honours `GROK_HOME`,
which the vendor's own startup log names, so the override is portable too and is
likewise unexercised off Windows.

That means a macOS run is expected to work and has not been shown to. Treat a
missing or empty adapter on macOS as unverified rather than broken, and record
what you find here.

## `telltale doctor`

The Windows box ran it first (2026-08-09) — five installs, five `--version`
answers. **The Mac has now run it too** (2026-08-10, Intel x86_64, macOS 26.5.2,
a binary built from `main` at `9b67e04`), so path resolution and the version
probes are measured on both platforms rather than portable-by-construction on
one:

| seat | binary | version |
|---|---|---|
| `claude` | `~/.local/bin/claude` | `2.1.222 (Claude Code)` |
| `codex` | `/usr/local/bin/codex` | `codex-cli 0.146.0` |
| `agy` | `~/.local/bin/agy` | `1.1.10` (`1.1.11` on the 2026-08-14 re-run) |
| `cursor` | `~/.local/bin/cursor-agent` | `2026.08.04-aaa8809` |
| `grok` | `~/.local/bin/grok` | `grok 1.0.0 (3cd0d0cbcebe)` at that run; **1.0.4 (d846eb93d94d)** since 2026-08-17 |

All four installed seats report `drivable ok` as **native executables**, which is
the row that matters beyond the version string: it is the measurement behind
"the prompt goes in argv and no shell sees it", so the brief's argv transport
holds on macOS and does not depend on a shell quoting it. Cursor's POSIX entry
point taking the bare `--version` was true by construction here; it is now true
by measurement.

**A cold run is several seconds slower than a warm one, and the report says so
per seat.** First run after boot: `claude` 3.89s, `cursor` 2.74s, the other two
under 0.6s. Immediately re-run, every probe came back under 1s. The probe is
launching a real vendor binary, so the first one pays that vendor's start-up —
don't read a multi-second `--version` row as a hung seat, and don't quote a warm
number as the cost of the check.

**`grok` was absent on 2026-08-10, and it is installed now.** The original row
read `binary FAILED`. That row was the report in correct operation, not a doctor
defect. An operator installed grok later on the same day. A re-run on
**2026-08-14**, from a binary built from `main` at `230ba54`, reports **all five
seats as `binary ok` and `drivable ok`, and each one as a native executable**.
The run passed 15 checks and failed 0 over 5 seats. This machine now seats the
same five vendors as the Windows machine. The capability gap that the Mac entry
in `dotfiles/PARITY.md` tracked is closed, and that entry is retired.

The re-run also showed two things worth a record. First, `agy` reported `1.1.11`
and not the `1.1.10` in the table above. `agy` updates itself, so a version in
this table is a timestamp and not a pin. Second, the `grok` probe was the fastest
of the five at 0.03s, against 7.02s for a cold `agy`. Do not read a slow
`--version` as a sick seat, and do not read a fast one as a healthy install.
That is the reason `auth` and `network` stay `not checked`.

Treat a wrong-looking doctor row on the Mac as unverified rather than broken, and
record what you find here.

## The macOS arrival of a released archive

**Measured 2026-08-17.** Every macOS run recorded above used a binary built on
the Mac itself. This entry is the first one that starts at a published release
asset, which is what a stranger actually gets.

| field | value |
|---|---|
| machine | Intel x86_64 MBP |
| OS | macOS 26.5.2, build 25F84 (`sw_vers`) |
| tag walked | `v0.2.0`, asset `telltale_0.2.0_darwin_amd64.tar.gz`, no re-tag |
| transport | `curl -fsSL` over HTTPS, HTTP 200, 3,912,801 bytes |
| checksum | `shasum -a 256 -c checksums.txt --ignore-missing` printed `telltale_0.2.0_darwin_amd64.tar.gz: OK` |
| signature | `codesign -dvv` printed `code object is not signed at all`; `spctl -a -vv` printed `rejected` and `source=no usable signature` |
| binary identity | `telltale version` printed `telltale 0.2.0` |

**The method reproduces the browser download; it is not a browser download.**
`curl` writes `com.apple.provenance` and does **not** write
`com.apple.quarantine`, which was confirmed with `xattr -l` on the fetched
archive. Quarantine is the attribute Gatekeeper acts on, so the operator wrote it
by hand with `xattr -w com.apple.quarantine "0083;00000000;Chrome;"` before
unpacking. The system `tar` then propagated it: the extracted binary carried
`com.apple.quarantine: 0283;6a832c78;;`. Read every result below as measured over
a reproduced mark rather than over a real browser download.

**The gate, verbatim.** `./telltale doctor` under the mark was killed every time.
The terminal reported:

```
/bin/bash: line 1: 45341 Killed: 9               ./telltale doctor
```

The exit status was 137, and the binary wrote nothing to stdout or stderr. macOS
also raised a dialog on the operator's screen, which read:

```
"telltale" Not Opened

Apple could not verify "telltale" is free of malware that may harm your Mac or
compromise your privacy.

[Move to Trash]  [Done]
```

The operator clicked neither button. The binary was **not** deleted by the kill;
it stayed on disk with the attribute intact.

**The remedy exercised.** `xattr -d com.apple.quarantine telltale` removed the
attribute, which `xattr -l` confirmed. `./telltale doctor` then ran to completion
at exit 0 and reported `15 checks passed, 0 failed, 10 not checked, over 5
seats`. Right-click-open was never a candidate here: `telltale` is a command-line
binary and not an app bundle, so the Finder path does not apply to it.

One shape worth carrying into the docs: `xattr -d com.apple.quarantine` prints
`xattr: telltale: No such xattr: com.apple.quarantine` and exits 1 when the
attribute is absent, so it is not a harmless no-op line in a `curl` sequence.
That is why `README.md` keeps `xattr -d` out of its main block and offers it as
the browser-path remedy. The block it does show was then run line by line in a
clean directory on the same day, and every line exited 0.

**Owed, and unrun at any date:**

- **A real browser walk.** Nobody has downloaded a release archive with a browser
  on this machine and run the result. Until somebody does, the quarantine mark in
  this entry is reproduced and the flags a real browser writes are unmeasured.
- **The System Settings > Privacy & Security "Open Anyway" path.** Only the
  `xattr -d` remedy was exercised. Whether that pane offers an entry for a
  killed command-line binary, and whether the entry works, is unmeasured.
- **The `darwin_arm64` archive.** It was not walked at all, which leaves its
  "built, not verified" label exactly where it was.

Windows and SmartScreen belong to the other machine. `SECURITY.md` still records
that prompt as unmeasured, and nothing here changes it.

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

### The clipboard is not the same mechanism on both machines

**Measured 2026-08-10**, and it is the one place where the reference box being
Windows hid a real defect rather than merely flattering the render. `y` copied
correctly on Windows Terminal and copied **nothing** on the macOS box, in the
same build, while reporting `copied …` on both.

The mechanism was OSC 52 — an escape sequence written into the terminal with no
acknowledgement of any kind (design.md §9.15). Windows Terminal honours it.
**Terminal.app does not implement it at all**, and **iTerm2 ships the permission
off** (General → Selection → *"Applications in terminal may access clipboard"*).
Because the sequence cannot be acknowledged, council had no way to tell the two
outcomes apart, and the notice claimed the copy on both.

Fixed by preferring the platform's own helper, which is checkable where the
escape sequence is not:

| platform | mechanism | can council tell whether it worked? |
|---|---|---|
| macOS | `pbcopy` | **yes** — exit status |
| Linux | `wl-copy`, else `xclip -selection clipboard` | **yes** — exit status |
| Windows | OSC 52 | no, and it does not need to: measured working |
| over SSH, anywhere | OSC 52 | no — the standing limitation, unchanged |

If `y` ever reports a copy and your clipboard is empty, the seat fell back to
OSC 52 and the terminal ate it. That is a terminal fact, not a council bug —
record the emulator and its version here.

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
