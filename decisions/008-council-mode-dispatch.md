# 008 — Interactive LLM Council mode for multi-vendor real-time agent dispatch

Status: Accepted (2026-08-03) — Amended via Codex review

## Context

`telltale` was created under a strict posture rule: *telltale never writes; it is a read-only ribbon on a sail showing true agent airflow*.

However, as the fleet strategy evolved under `agent-ops/ADR-010` into a four-vendor orchestration model (Claude Code as control plane, Codex for independent challenge, Cursor in the IDE, and Antigravity for Gemini research), a missing capability emerged: **real-time round-table deliberation**.

San (Council Lead) requires an interactive terminal space where a single brief or prompt can be broadcast simultaneously to all three active reasoning model families (Anthropic/Claude, GPT/Codex, Gemini/Antigravity), allowing their streaming responses to be compared side-by-side in real-time, followed by multi-turn discussion.

Following a live independent review by **Codex** (`codex exec`), two critical requirements were integrated to harden the architecture:
1. **Model Independence**: Avoiding consensus anchoring by keeping the initial dispatch blind.
2. **Enforced Sandboxing**: Enforcing process-level read-only execution flags so `telltale council` cannot mutate the repository.

## Decision

1. **New subcommand: `telltale council`**  
   We introduce `telltale council` as a distinct entry point (`cmd/telltale/main.go` and `internal/council`).

2. **Posture Separation Boundary preserved**  
   `telltale hud` and `telltale statusline` remain 100% read-only observers without write access, credential access, or network calls. `telltale council` is explicitly defined as an opt-in interactive dispatch room.

3. **Read-only posture, stated per vendor — not as a blanket claim**  
   The first draft of this ADR claimed "enforced read-only sandboxing" for all three vendors. That
   was not true: only Codex had a named mechanism, Antigravity's was "soft-denied" (i.e. not
   enforced), and Claude Code's was labelled read-only with no mechanism at all. A repo whose
   thesis is that a gauge must never overstate what it knows cannot ship that sentence. Corrected
   after verifying each installed binary's own `--help` and the Claude Code headless docs:

   | Vendor | Mechanism | What it actually enforces |
   |---|---|---|
   | Claude Code | `--disallowedTools <write/exec list>` + `--strict-mcp-config` | **Verified absent** — the named tools are not in the session, checked by reading the session's own reported tool list. A deny list cannot cover a tool a future release adds. |
   | Codex | `-s read-only` | **Requested on Windows.** Measured: every sandboxed process spawn fails with `CreateProcessAsUserW` access-denied — including one asked merely to list a directory. No shell write can land, but the mechanism is a blanket spawn failure, not a read/write distinction. Enforced on macOS/Linux. |
   | Antigravity | `--mode plan --sandbox` | **Refuted.** Asked to write a file under both flags, it wrote the file. Reported permission mode and tool list were identical to a run without them. Badge is `unsandboxed`, with no `ro:` prefix. |

   Each column renders its own badge (`ro:enforced` / `ro:tools` / `ro:requested`). There is no
   blanket read-only claim anywhere in the UI.

   **Execution is argv-based, never a shell.** Prompts are arbitrary user text containing quotes
   and newlines, so no prompt is ever interpolated into a command string. Specs are
   `{Binary, Args []string, StdinPrompt, Dir}` run through `exec.CommandContext`. This matters
   more than it looks on Windows: `LookPath("codex")` resolves to `codex.cmd`, an npm shim, and Go
   runs `.cmd` files through `cmd.exe`, whose argument parsing cannot be safely quoted for
   arbitrary text. Codex and Claude therefore take their prompt on **stdin** (both support it —
   Codex via the `-` sentinel), which also sidesteps the ~32K Windows command-line limit on later
   turns. The runner refuses to place prompt text in argv when the resolved binary is `.cmd`/`.bat`.

   Output streams are sanitized at a single choke point and credential strings redacted — on
   line-buffered text, so a secret split across two stream chunks cannot straddle the match — before
   anything reaches the state the renderer can read.

4. **Blind First-Round Protocol & Multi-Turn via native resume**  
   - **Turn 1 (Blind Dispatch)**: The opening brief is dispatched to all 3 vendors simultaneously without cross-agent context, ensuring completely independent opinions.
   - **Subsequent Turns**: carried by each vendor's **own session-resume mechanism**
     (`claude --resume <id>`, `codex exec resume <id>`, `agy --conversation <id>` — all verified to
     exist), not by re-sending the transcript. Re-sending grows input quadratically (roughly 30K
     redundant input tokens per vendor by turn five, times three vendors) against metered quotas,
     and flattens native turn structure into quoted prose. Resume sends only the new turn, lets the
     vendor replay its own stored history, and keeps the blind-round guarantee *structural*: each
     session contains only its own history, so cross-contamination cannot happen by accident.
   - **Cross-agent rebuttal is explicit and opt-in**: a compose-mode toggle prepends the other
     vendors' final answers from the previous turn only, truncated to a budget and each block
     labelled as quoted, untrusted material.

5. **Bubble Tea TUI Layout**  
   - **Top Header**: Active workspace directory and Council status bar.
   - **Viewport Body**: 3 vertical side-by-side columns (≥96 cols) or tabbed views on narrower
     terminals, rendering streaming stdout from Claude Code, Codex, and Antigravity. Each column
     header carries its vendor's *streaming granularity* alongside its sandbox badge, because the
     three do not stream alike: Claude emits token-level deltas (verified), the others emit
     coarser event-level updates. A vendor that turns out to emit only a final result renders a
     waiting card that says so, rather than faking incremental flow.
   - **Prompt Footer**: Interactive text input, hand-rolled on the pattern `internal/hud` already
     uses for find mode, **not** `bubbles/textinput`. This repo carries exactly two Charm
     dependencies and re-benchmarks statusline init cost whenever that changes; a third dependency
     for one input line is not worth the budget, and hand-rolling keeps `State` a plain value that
     the pure renderer and its golden tests can construct directly.
   - **Keybindings**: `[Enter]` to dispatch prompt to all agents, `[Tab]` to cycle vendor focus, `[Esc]` to edit prompt, `[q]` or `[Ctrl+C]` to teardown session cleanly.

6. **Fail-Closed Fallback for Uninstalled/Unconfigured Vendors**  
   If a vendor CLI binary is absent or unauthenticated, `telltale council` does not crash. It renders a degraded explanation card in that vendor's column while allowing the active vendors to proceed normally.

7. **Cursor is out of scope for v1**  
   Council seats the vendors that expose a headless CLI. Cursor ships one as a product
   (`cursor-agent`), but it is **not installed** in this environment — the `cursor` binary on PATH
   is the editor launcher (diff/merge/goto flags only). Cursor is therefore a fourth seat the
   vendor interface can accept later as a drop-in adapter, not v1 code written against a binary
   that isn't there. Note that this is the opposite situation from the HUD, where Cursor *is* a
   built-in adapter (ADR-007) because its seam is on disk, not behind a CLI.

## Consequences

- `telltale` expands beyond status monitoring to include multi-vendor agent dispatch. The
  observation surfaces keep their read-only guarantee unchanged; the boundary moved from "telltale
  never writes" to "the gauges never write", and `README.md` and `docs/design.md` §7.8 were
  amended to say so rather than leaving the repo asserting two contradictory contracts.
- Provides San with a unified, real-time command center for multi-model decision-making directly inside Windows Terminal — one room instead of four terminals.
- The read-only claim is now per-vendor and visible on screen, which means the UI will sometimes
  admit it does not know what a vendor's sandbox flag enforces. That is the intended behaviour;
  the alternative is the blanket claim this ADR originally made.

### Amendment, 2026-08-04: the Claude mechanism named above was itself wrong

The first version of this ADR claimed read-only enforcement with no mechanism at all. The
amendment that corrected it named `--allowedTools "Read,Glob,Grep"` — and that is also wrong,
in a way that only a live check could catch.

`--allowedTools` **pre-approves** tools for permission prompts. It does not remove them from
the session. Running the exact invocation and reading the `system/init` event's own `tools`
array showed `Edit`, `Write` and `Bash` still present. A `ro:tools` badge on top of that flag
would have been a third false claim, shipped with a passing test suite behind it, because
every test asserted the *flag* and none asserted the *effect*.

What actually works is `--disallowedTools` plus `--strict-mcp-config`. Two parts of that are
easy to get wrong:

- **Deny PowerShell, not just Bash.** Denying only Bash leaves a working shell on Windows,
  which is the platform this product targets.
- **Drop MCP servers.** Without `--strict-mcp-config` the session inherits whatever the user
  has connected; the verification run surfaced Gmail write tools in a session that had every
  built-in write tool denied. No fixed deny list can name those in advance.

The honest limitation now stated in the badge and in design.md §9.2: a deny list cannot cover
a tool that does not exist yet. The claim is *"these named tools are absent, verified"*, not
*"this session cannot write"*.

The general lesson, which is why this is written down rather than quietly fixed: **a flag's
name is not evidence of its effect.** The verification that mattered was not reading `--help`,
it was reading what the session reported about itself afterwards.

### Amendment, 2026-08-04 (second): the live spike refuted one sandbox claim outright

Codex and Antigravity were both run for real before their adapters shipped. Two results
changed what this ADR is allowed to say.

**Antigravity does not enforce anything.** Under `--mode plan --sandbox` it was asked to write
a file, and it wrote the file — confirmed on disk. Its reported `permission_mode` was byte
identical to a run without the flags, and its tool list still held `write_to_file`,
`replace_file_content` and `run_command`. This is not an unestablished claim, it is a refuted
one. A fourth sandbox level (`SandboxNone`) was added for it, badged `unsandboxed` rather than
`ro:none`: every other badge starts with `ro:`, a reader scanning three headers takes in the
prefix before the qualifier, and this vendor must not read as read-only at a glance.

**Codex's Windows sandbox degrades to something odd rather than something safe.** `-s read-only`
does prevent a write, but only because every sandboxed process spawn fails outright — the
control run, asked to list a directory, failed identically. `codex features list` reports the
Windows sandbox surfaces as removed. The badge stays *requested*.

**Neither vendor streams.** Codex emits one `item.completed` per complete agent message and has
no message-delta feature. Antigravity delivers a whole response as a single `text_delta`; a
one-word reply left its column empty for 73 seconds and then painted at once. Both are
therefore `GranFinalOnly` and open in `PhaseWaiting`. The waiting card, added on the theory that
some vendor might not stream, turns out to describe two thirds of the room.

Two invocation traps worth recording because both fail silently rather than loudly:

- `codex exec` and `codex exec resume` **do not accept the same flags.** `-s` and `--cd` are
  rejected by `resume` with an argument-parsing error, which produces empty stdout — a naive
  resume would blank the column every follow-up turn with nothing to explain it. Resume carries
  the posture as `-c sandbox_mode="read-only"` instead.
- `agy`'s `-p` is a **string flag whose value is the prompt**, not a boolean. Written in the
  natural order, `agy -p --output-format stream-json "<brief>"` exits 0 and cheerfully answers a
  question about the flag it swallowed. `-p` must come last, with the brief immediately after.
  Antigravity also does not accept a prompt on stdin, so its brief goes in argv and is subject
  to the ~32K Windows command-line limit.

### Amendment, 2026-08-04 (third): `--write`, and what actually contains this room

The read-only posture was never the containment, and holding three different
almost-restrictions in the same room made that easy to miss:

- Claude was genuinely restricted (deny list + no MCP).
- Codex was *broken*-restricted: under `-s read-only` on Windows every sandboxed process
  spawn fails, including one asked merely to list a directory. It could not run a read.
- Antigravity was never restricted at all — measured writing a file under both of its own
  read-only flags.

One contained seat, one crippled seat, one open seat is not a safety posture. And the fleet
contract settled the question independently: **agent-ops ADR-012 (2026-08-04) rules capability
parity — all four vendors read and write, and guard wiring rather than lane shape is the
control.** Nothing in council may be read as "this vendor is read-only"; the badges describe
what *this tool asked for on this invocation*, never what a vendor is capable of.

So `telltale council --write` exists, and it is honest about what it is:

| | read posture | write posture |
|---|---|---|
| Claude | `--disallowedTools <list>` | deny list dropped, `--permission-mode acceptEdits` |
| Codex | `-s read-only` | `-s workspace-write` (which also un-breaks it) |
| Antigravity | `--mode plan --sandbox` | both dropped |

Three decisions inside that table are worth stating:

- **`--strict-mcp-config` stays in BOTH postures.** Write mode widens what a vendor may do
  inside the directory council was pointed at. MCP servers reach *outside* it — the
  verification run surfaced Gmail write tools — and "may edit this worktree" is a different
  grant from "may act on your accounts".
- **Codex gets `workspace-write`, not `danger-full-access`.** The flag should agree with the
  boundary council actually offers rather than remove it.
- **`--dangerously-skip-permissions` is NOT passed to Antigravity**, in either posture. ADR-012
  records that agy's print mode auto-denies approval-needing tools and points at that flag;
  passing it would auto-approve every tool request, which is a larger grant than `--write`
  asks for. The consequence is stated rather than papered over: an agy turn in write mode may
  still refuse a tool, and its column will say so.

**The containment is the workspace, not the flag.** `--cd` already exists, so a write-mode
room belongs in a throwaway worktree:

```
git worktree add ../telltale-council
telltale council --write --cd ../telltale-council
```

Because that is the real control, the room states its posture where it cannot be missed: a
persistent `⚠ WRITE` in the header for the whole session, and the same `WRITES` badge on every
column. Uniform on purpose — the per-vendor gradations are all shades of "how much read-only
did we manage to ask for", and once the answer is "none", grading them would imply a safety
difference that does not exist.

## Verification status

Flag surfaces were verified against the installed binaries' own `--help` output and, for Claude
Code, against the live headless documentation. Still unverified and scheduled as a live spike
before the Codex and Antigravity columns land: the Codex `--json` event schema and delta
granularity, whether `codex -s read-only` actually engages on Windows, and Antigravity's
stream-json schema, conversation-id location, stdin support and `--sandbox` semantics. Those
columns render honest *requested* badges until the spike says otherwise.
