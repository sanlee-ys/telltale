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
   The first draft of this ADR claimed "enforced read-only sandboxing" for all vendors in the fleet. That
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
   - **Turn 1 (Blind Dispatch)**: The opening brief is dispatched to all 4 vendors simultaneously without cross-agent context, ensuring completely independent opinions across the 4-vendor fleet (Claude Code, Codex, Cursor, Antigravity).
   - **Subsequent Turns**: carried by each vendor's **own session-resume mechanism**
     (`claude --resume <id>`, `codex exec resume <id>`, `agy --conversation <id>` — all verified to
     exist), not by re-sending the transcript. Re-sending grows input quadratically (roughly 30K
     redundant input tokens per vendor by turn five, times four vendors) against metered quotas,
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

7. **Cursor takes the fourth seat** *(superseded 2026-08-04; the original text is preserved below
   because the reason it was wrong is the point)*

   ~~Council seats the vendors that expose a headless CLI. Cursor ships one as a product
   (`cursor-agent`), but it is **not installed** in this environment — the `cursor` binary on PATH
   is the editor launcher (diff/merge/goto flags only).~~

   The second sentence was right and is still enforced: `cursor` on PATH is the editor launcher and
   council never claims it. The first was **false when it was written**. `cursor-agent` was
   installed at `%LOCALAPPDATA%\cursor-agent\cursor-agent.cmd`, and had been since July. Cursor is
   now seated, with an adapter and a detection path that cannot repeat the mistake. See the fifth
   amendment for what the spike measured, what it could not, and why this seat's badges are the
   weakest in the room. Note that the HUD reaches Cursor a different way entirely — a built-in
   adapter over its on-disk seam (ADR-007), not a CLI.

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

### Amendment, 2026-08-04 (fourth): the room's default state was three strangers

Observed in use, on turn 4 of a real session. Asked to "assume our C-level roles for this
session", all four vendors guessed — separately and differently. Claude named its reading and
asked to confirm; Codex asked which roles it should take; Antigravity improvised a corporate
register. The convention they were groping for was already written down, in a private repo none
of them had been given.

That is council's default state, not a one-off: every vendor starts a fresh session with no
shared history, so anything the user has already established is invisible to all of them.

`--brief <file>` (or `TELLTALE_COUNCIL_BRIEF`) hands one file of operating context to every
vendor on its **first turn**. Later turns resume each vendor's own session, so the context is
already in its history — re-sending it per turn per vendor would spend the whole brief again
against metered quotas for nothing.

**Why the prompt rather than a system-prompt flag.** Only Claude has one (`--append-system-prompt`);
`codex exec` has none and neither does `agy`. Even for Claude it takes the content in **argv**,
which is the wrong channel twice over: a command line is visible in process listings, and the
brief this feature exists to carry is private. Prompt-on-stdin is uniform across vendors and
keeps the content off the command line — except for Antigravity, which does not accept stdin at
all, and whose argv path is why the brief is capped at 24K.

**The fence is worded UNLIKE the rebuttal fence, deliberately.** Quoted vendor replies are
another model's words, marked as untrusted data that must not be followed (§ rebuttal).
The brief is the user's own file, handed over on purpose, and is exactly what the vendor should
follow. Inheriting the untrusted warning would teach the model to discount its own principal.

**Privacy is a structural constraint, not a note.** telltale is public and the briefing is not,
so the flag takes a PATH: nothing is baked into this repo, no default location inside a repo is
searched, the content is never logged and never rendered. It lives on `Model`, never on `State`
— the renderer has no business being able to reach it — and only a boolean crosses the boundary.

**A bad path stops the room.** Running unbriefed after the user explicitly asked for a briefing
reproduces the exact failure this removes, except now the user believes it is fixed. Missing,
empty and oversize all fail before the alternate screen is entered.

The header states `briefed` or `no brief` on every frame. An unbriefed room looks identical to a
briefed one until a vendor guesses out loud, which is how this was found.

Verified live rather than inferred: with a brief instructing a specific reply, the vendor
returned exactly that reply.

### Amendment, 2026-08-04 (fifth): the fourth seat, and a claim that was simply false

The three amendments above are all the same lesson learned three ways: a flag's name is not
evidence of its effect. §7 was a fourth instance of it, in a form none of them cover — **a claim
about the world, asserted from one failed lookup and never retested.**

"cursor-agent is not installed" was not a misread flag. It was untrue. The binary was at
`%LOCALAPPDATA%\cursor-agent\cursor-agent.cmd`, dated July, and the directory containing it is on
this machine's user PATH — so even the lookup that produced the sentence would pass today. The
sentence then sat in an ADR and in a test (`TestCursorIsNotSeated`) that pinned it, which is the
part worth recording: **a test can hold a false claim in place just as firmly as a true one.** It
asserted the absence of a seat, so the day the world changed underneath it, it went on passing.

Detection now resolves a vendor in three steps rather than one — env override, PATH, then a short
list of known install locations — and every card names *where* the binary came from. The
knownPaths list did not turn out to be necessary here, and it stays anyway: PATH is a claim about
the current process, not about the machine, and a shell opened before an installer ran is exactly
the situation that produces this false negative.

**What the spike established, and how.** Not the way the other vendors in the 4-vendor fleet were established,
and the difference has to be stated rather than left to resemblance:

> The installed `cursor-agent` reports **"Not logged in"**, and it checks authentication **before**
> it parses flags. A deliberately invalid flag combination (`--stream-partial-output` with
> `--output-format text`, which the CLI is coded to reject) returned the authentication error
> instead. So no probe reached flag validation, let alone a turn. **Nothing in this seat was
> confirmed by running it.**

What replaced the live run is the CLI's own `--help` plus the shipped JavaScript bundle that
implements print mode — weaker than a measurement, stronger than the docs this repo has twice been
burned by, and labelled as such everywhere it is relied on.

| | what the spike found | strength |
|---|---|---|
| Prompt channel | **argv only.** Print mode's guard is `t.trim() \|\| "No prompt provided for print mode"` against the joined argv; no code path in the bundle reads the prompt from stdin, and there is no `-` sentinel or `--prompt-file`. | Read from the shipped bundle |
| Windows drivability | **Unusable.** argv-only + a `.cmd` shim is exactly what `runner.ErrShellShimWithArgvPrompt` refuses. The env override has nothing to point at: the install ships no native executable, only a `.cmd` that shells into PowerShell which runs a bundled `node.exe` against a 9MB `index.js`. | Measured (the install was inspected) |
| Non-Windows | **Works.** The same install is an extensionless script elsewhere, which is `KindNative`, and argv is safe when no shell is involved. | Inferred from `kindOf`; not run |
| Event schema | `system/init`, `user`, `assistant`, `tool_call`, `result` — each with a top-level `session_id`. Assistant text is `message.content[].text`. | Read from the bundle's own emit calls |
| Streaming | **Unknown, and left unclaimed.** `--stream-partial-output` promises "individual text deltas" and the bundle does emit per chunk. Antigravity is why that is not enough: its schema emits a key literally named `text_delta` that carried an entire reply at once, 73 seconds late. | Not observed |
| Resume | `--resume <chatId>` exists and the bundle turns a string value into a resume; council passes the `session_id` off the event stream. **That the two ids are the same id was never round-tripped.** | Read from the bundle |
| Sandbox | `--mode plan` ("read-only/planning ... no edits") and `--sandbox enabled` are **requested**. Nothing more is known. | Not observed |
| Cost | None. `usage` carries token counts and the bundle has no monetary figure anywhere, so `CostUSD` stays nil. | Read from the bundle |

**Two findings are worth more than the rest, because they close doors.**

First, the trick that caught `--allowedTools` — run it and read what the session reports about
itself — **does not work on this vendor.** Its `system/init` event reports `permissionMode` as a
hardcoded `"default"` string literal in the bundle, not a readout of the session. Even an
authenticated run could not confirm the posture that way. Combined with the CLI's own help for
`-p`, which says print mode "Has access to all tools, including write and shell", this seat's
read-only claim rests entirely on `--mode plan` being honoured, unverified. Its badge is
`ro:requested` and its detail says the claim is weaker than every other column's — which is the
first time a badge in this room has had to rank itself against its neighbours.

Second, **`GranUnknown` stopped being a placeholder.** It had no vendor and no behaviour; a column
carrying it would have opened in `PhaseStreaming`, asserting "output is arriving and you are seeing
it as it lands" on the strength of nothing. Unknown now opens in `PhaseWaiting` alongside
final-only, prints no granularity word at all, and gets its own waiting card — "whether this vendor
reports incremental output has not been established" — rather than borrowing the sentence two
vendors earned by measurement. Promotion happens on evidence: the first chunk of real output
upgrades the phase.

Three flags are refused in **both** postures, and the reasons are not interchangeable.
`-f/--force` and `--yolo` are the skip-permissions class, same ruling as
`--dangerously-skip-permissions` on Antigravity. `--approve-mcps` reaches outside the directory
council was pointed at, which is the boundary `--write` widens and not one it removes.
`--trust` accepts a workspace-trust prompt on the user's behalf; the honest consequence — an
untrusted workspace may stall a print-mode turn with nobody to answer — is stated rather than
traded away for convenience.

One hazard is recorded unresolved rather than guessed at: a brief whose first character is `-`
would be read as an unknown option, since the prompt is a variadic positional. The usual fix is a
bare `--` separator, but that is inferred from the argument parser rather than observed, and
getting it wrong breaks *every* brief instead of a rare one. It waits for someone with an
authenticated CLI to run both forms once.

### Amendment, 2026-08-04 (sixth): one process per Claude seat

Every turn used to be a fresh `claude -p --resume`. The cost of that was not
subtle — a one-word "gm" took about 25 seconds and $0.23, nearly all of it
session init, paid again on every turn of every conversation. The second cost
was structural and is the one that actually forced this: **a batch process
cannot ask permission.** Its stdin is written and closed before the first token
arrives, so there is no channel for it to ask on and none for an answer to come
back on.

`--input-format stream-json` puts the CLI into realtime streaming input: one
process, stdin held open, one JSONL message per turn. Verified live against
Claude Code 2.1.220 on Windows rather than read from documentation, which is the
standing rule in this file and which earned its keep again below.

**What the spike established, and how.**

| | finding | strength |
|---|---|---|
| Turn envelope | `{"type":"user","message":{"role":"user","content":"…"}}`, one line. | Sent; answered |
| Persistence | Two turns down one stdin with a pause between: same pid, alive throughout, **same `session_id`**. Closing stdin exited 0. | Measured |
| Turn boundary | One `result` per turn. There is no process exit to infer it from, so this line is the ONLY end-of-turn signal a persistent column gets. | Measured |
| `system/init` | **Re-emitted at the start of EVERY turn**, carrying the same `session_id`. A parser that treats init as "a new session started" would reset the seat once per turn. | Measured |
| Interrupt | `{"type":"control_request","request_id":"…","request":{"subtype":"interrupt"}}` → `control_response` with `still_queued`, then a `result` whose `terminal_reason` is `aborted_tools`. **The process stayed alive and took a further turn.** | Measured |
| Streaming granularity | **Unchanged.** 20 text deltas, mean 80 chars, over a 250-word reply — against a spawn-per-turn control run of 22 deltas, mean 79.9. | Measured, with a control |

**Cancelling a turn no longer kills the seat.** The interrupt is what makes that
possible, and it matters more than it sounds: killing would work, and it would
also throw away the session init the room just paid for, so cancelling one turn
would quietly make the next one expensive. If the interrupt cannot be delivered
the seat is killed and restarted, and the column says the thread was lost rather
than resuming a conversation it no longer has.

**The reported cost changed meaning, and the badge had to change with it.**
`total_cost_usd` is a RUNNING TOTAL for the process: across two turns it went
$0.1061493 → $0.1177296 while the per-turn `usage` block stayed at 2 input
tokens both times. The cost cell has meant "this turn" everywhere else in this
room. Rendering a session total there would be a false reading of a true number,
and subtracting one from the other would be council inventing a figure — which
is on this repo's deliberately-rejected list. So the badge says
`$0.1177 session` and neither happens.

**The seat is Claude's alone, and that is a fact about the other CLIs.**
`codex exec` and `agy -p` are batch programs: they read a prompt, answer, and
exit. Neither exposes a mode that keeps a process alive across turns, so neither
can be handed a second turn and neither has a channel to ask a question on
mid-turn. Their columns keep spawn-per-turn and their badges keep saying exactly
what they said before. A room where one seat is cheap and three are not is the
honest shape of this; making them look uniform would be the lie.

**A process that outlives a turn by design is the one that outlives the room by
accident.** The persistent child lives in the same kill-on-close job object, and
every quit path now tears down — including the two that previously did not,
because "no turn in flight" used to mean "no children" and stopped meaning it
the moment this landed.

### Amendment, 2026-08-04 (seventh): `--write` asks first

The third amendment settled that the read-only posture was never the containment and that
the workspace is. That is still true, and it is a coarse control: it says *where* three
agents may act, never *what* they are about to do. With one seat now driven as a live
process (sixth amendment), a finer one became possible for the first time — the process can
ask, and wait.

**`telltale council --write` is now gated by default. `--write --auto` restores the old
behaviour.** The default is the attended one because the room the user opened is the room
they are looking at; unattended has to be typed.

**Three flags turn the gate on, and two of them do nothing alone.** This is recorded in
detail because the failure mode of each is silence rather than an error:

| flag | alone |
|---|---|
| `--permission-prompt-tool stdio` | **Not in `--help`** for 2.1.220, and real — an invented flag is rejected with "unknown option" while this one parses, and the shipped binary's own SDK spawn code pushes exactly this pair for a `canUseTool` callback. Passed by itself the session ran in `auto` mode, emitted no request, and wrote the file. |
| `--permission-mode manual` | Reported back as `permissionMode: "default"`. Passed by itself there is nobody to ask, so the call short-circuits to *"Claude requested permissions to write to …, but you haven't granted it yet"* and the vendor gives up rather than waiting. |
| `--setting-sources ""` | See below. This is the one that decides whether the feature's headline sentence is true. |

Together, the request appears on stdout and the turn blocks on it. Blocking was measured
rather than assumed: the answer was withheld for twenty seconds, nothing else arrived in
that window, and the tool result landed 0.25s after it was sent. Both branches were driven
live — an allow ran the tool and put the file on disk; a deny came back as an `is_error`
tool_result carrying council's own refusal text, and the file was never created.

**The settings hole, which is the finding worth the whole spike.** Permission *allow rules*
in the user's settings files are consulted **before** the permission callback, so any call
they cover never reaches the gate at all. Measured on a machine whose global settings allow
`Bash(mkdir:*)`:

- default setting sources, `mkdir zzz` → **no request, directory created**
- `--setting-sources ""`, same call → request raised, denial honoured, nothing created

Without that flag, *"nothing writes without your keystroke"* is false — and false quietly,
on a machine whose owner wrote those rules long ago for a different purpose. A first pass
tested this with `git status`, which is ungated under **both** configurations; that nearly
produced the opposite conclusion, and the control that settled it was picking a command that
is both allowlisted and genuinely mutating.

**Two limits are stated on the badge rather than buried in this file.** Shell commands the
CLI itself classifies as read-only are approved without asking, so every claim is worded
about calls that *change* things. And dropping the setting sources also drops the user's own
hooks and user-level commands from that seat: the gate **replaces** their permission layer
rather than sitting behind it. That is a real trade, and the reason it is acceptable is
precisely that someone is watching every call — which is why `--auto` does not pay it.

**A denial is not a failure, and keeping them apart needed a fifth outcome value.** The
vendor reports a refusal as an `is_error` tool_result whose content is council's own refusal
text echoed back. Read off the stream alone that is indistinguishable from a tool that broke,
and the trace would report the command *failing* when what happened is that it was *not
allowed to run*. `ActDenied` is recorded from the keystroke, before the echo arrives, and the
echo is not permitted to overwrite it. It renders `✗ denied by you` — words first, colour
second, and `SevWarn` rather than `SevCrit` because a refusal is the room working, not
something going wrong. It is the only line in the trace that is not a reading of a vendor's
words; it is the record of a keystroke.

**The refusal text names who refused.** A model told only "denied" treats it as an obstacle
to route around, and the next thing it does is a slightly different spelling of the same
call — which is a second request for a user who has already said no once.

**The gate is Claude-only, and the badge says so per column.** `codex exec` and `agy -p` are
batch programs with no channel a question could arrive on, so they keep `WRITES` and only the
seat that asks carries `gated`. Giving all four the same word would be exactly the blanket
claim §3 of this ADR exists to refuse, one level up. `gated` is safe to read as a plain word
only because the header still carries the persistent `⚠ WRITE` marker for the whole session —
the badge is read in a context that has already said this room can write.

**The approval card is chrome, not body.** The badge line earned that place because a claim
you cannot see is not a claim; this earns it for a stronger reason — a vendor is *stopped*
behind it, and during a turn every column is following its own tail, so a card in the body
would be pushed off screen by the output of the very call it is asking about. For the same
reason the gate's mode line is the one footer state a transient notice may not displace.

### Amendment, 2026-08-04 (eighth): the gated seat carries the guard again

The seventh amendment closed the settings hole and named its price in one sentence: dropping
the setting sources "also drops the user's own hooks and user-level commands from that seat".
That sentence was accurate and it was too calm. Two different things were being dropped:

- **Permission allow rules**, which the gate is *supposed* to replace. A rule that pre-approves
  a call is precisely what a gate cannot sit behind.
- **Hooks**, which nothing was replacing. A `PreToolUse` hook is a screen the user built — on
  this machine, a credential guard that blocks credential-file reads and bulk env dumps — and
  it was being removed as collateral.

The second is worse than it sounds, because of *which* calls it covered. The seventh amendment
already recorded that shell commands the CLI classifies read-only are approved without asking.
Those calls never reach the gate. With the hooks gone they reached nothing at all, and reading
a credential file is exactly that shape of call. The gated seat was the room's most-supervised
column and its least-screened one at the same time.

**The mechanism, established live before anything was built.** `--settings <file>` composes
with `--setting-sources ""`. Three probes in a throwaway directory against Claude Code 2.1.220,
driven through a stand-in for council's persistent seat, with a *planted* hook rather than the
real guard so that "the hook ran" could be told from "the command failed":

| probe | result |
|---|---|
| Gated posture, no `--settings`: `echo TELLTALE_HOOK_MARKER` | No permission request at all, and `"content":"TELLTALE_HOOK_MARKER"` came back. Neither gated nor screened — the hole, reproduced. |
| Same, `--settings` at a hooks-only file with a planted `PreToolUse` hook | `"content":"SPIKE-HOOK-DENIED-TELLTALE_HOOK_MARKER","is_error":true`, `"non_execution_kind":"permission-rule"`, and the hook's own breadcrumb file recorded the call. |
| Same file, an allowlisted `mkdir zzz-guard-probe` | `{"type":"control_request",…"subtype":"can_use_tool"…}` was raised, the denial was honoured, nothing was created. The gate is intact and the user's ~210 allow rules are still dropped. |

Then the run that matters, on the shipped loader rather than a hand-written fixture: council's
own `LoadHookSet()` was pointed at the real `~/.claude/settings.json`, and the seat it produced
answered `cat <path>/.env` with the machine's actual credential guard — *"CREDENTIAL GUARD (v2,
path-based default-deny)"* — on a call that raised no gate request. The guard is back in front
of the calls the gate never sees.

**A fourth probe is the reason the extraction is written the way it is.** A `--settings` file
carrying `{"permissions":{"allow":["Bash(mkdir:*)"]}}` made the same `mkdir` run with **no
request**, and the directory was on disk. So this flag is a live re-entry point for the exact
rules `--setting-sources ""` exists to drop, and the room would have gone on rendering `gated`
while calls walked past the gate. The file council writes is therefore built by **naming the
single key `hooks`**, never by deleting keys it does not want: an allowlist of one cannot rot
as Claude Code adds settings keys, and a denylist would rot silently.

**What the seat now carries.** The gated Claude invocation gains `--settings <ephemeral file>`,
and only that posture — `--auto` and the read posture load the user's settings natively, so
injecting the same hooks again would fire each of them twice, and a guard that asks two
questions per call is a guard people switch off. The file is absolute (a relative path resolves
against the *child's* cwd, which is the workspace, and fails with "Settings file not found"),
0600, in a per-room temp directory removed on teardown, and its content is never logged or
rendered — the same discipline `--brief` carries, and only a boolean crosses onto `State`.

**The badge is derived from the file, not from the attempt.** An unreadable settings file, an
empty hooks section, and a temp directory that could not be created all end in the same place,
so the gated detail has two forms and the absent one says the calls the gate is not asked about
have nothing screening them. A claim keyed off "we tried to wire it" would have survived all
three failures.

**The honest residual.** Hooks fire as that file described them **at spawn time**. Editing
`~/.claude/settings.json` mid-session changes nothing until the next room — and, because a
seat's process is respawned with the same path if it dies, not even then within one room. Also
unchanged: the user's user-level slash commands are still dropped from this seat, and no
attempt is made to resolve a relocated config directory, because nothing here measured what
`CLAUDE_CONFIG_DIR` does. That last one fails visibly rather than quietly — a machine whose
config lives elsewhere finds no hooks, wires nothing, and the badge says so.

### Amendment, 2026-08-04 (ninth): the room survives quitting, and council writes one file

Everything the room knew died with the process. Four vendors were each holding a conversation
several turns deep — that is the entire point of the resume mechanism in §4 — and the only
thing standing between the user and those conversations was a map of session ids in memory.
Quitting the room, closing the terminal, or rebooting threw the ids away and left four live
sessions stranded: the vendors still had the history, and nothing on the machine could name it.

`telltale council --resume` reopens the room last saved for a workspace. **This required
ratifying that council may write one file, and that ratification is the load-bearing part of
this amendment rather than the feature.**

**The gauges' contract is untouched.** `statusline` and `hud` still write nothing at all. The
boundary moved once already, in the Consequences above — from "telltale never writes" to "the
gauges never write" — and council was always the declared exception. This is the first thing
that exception is spent on, and it is spent narrowly.

**What is in the file, and the rule that decided it.** One file per workspace at
`~/.telltale/council/<sha256-of-workspace>.json`, mode 0600 in a 0700 directory:

| field | why it is there |
|---|---|
| `version` | a schema this build does not know is refused rather than misread |
| `workspace` | the hash names the file; the file has to agree, or a collision hands one project's sessions to another |
| `posture` | `read` / `write` / `write-gated` — **recorded to be displayed, never re-applied** |
| `turn` | so the counter continues at N+1 rather than restarting and reframing a resumed conversation as a new one |
| `sessions` | vendor id → that vendor's OWN session id. The keys the file exists for |
| `brief_path` | the PATH of the operating brief. **Never its content** |
| `saved_at` | so the card can say how stale the room is instead of presenting an old one as current |

The rule is that this file holds **keys, not content**. No transcripts, no vendor output, no
prompts, no brief text. Each vendor already stores its own history against its own id, so
anything copied here would be a second copy of a private conversation in a location the user
never chose — and `--brief` exists precisely because that content is private (fourth
amendment). If this file leaked it would disclose which directory was worked in, when, and a
set of opaque ids. Not a word anyone said. A test asserts that a room whose brief, reply,
draft and shell trace all contain distinctive strings writes none of them.

It lives under the home directory and not beside the workspace, which is a privacy decision
rather than a filesystem one: a dotfile dropped into the directory council was pointed at ends
up in someone's repo, in their `git status`, and eventually in a commit. The path is *hashed*
for the same reason — the listing of `~/.telltale/council` should not be an inventory of what
the user works on.

**Posture is never restored, and this is the safety property of the feature.** A `--write` room
saved to disk reopens read. Restoring it would mean a room that can edit a tree because of
something on disk rather than something the user typed, and the whole of the third amendment is
that a write-capable room announces itself in the command and in the header for the entire
session. A flag that can arrive from a file is not a flag anyone typed. The saved posture is
shown in the reattach notice when it differs, so the user learns it from the room rather than
from a vendor refusing to edit a file.

**Two failure modes, answered differently.** `--resume` with **no state file** is a plain error
before the alternate screen, mirroring `LoadBrief`: nothing was ever saved for that workspace,
so the user is wrong about which room they are reattaching to — usually a `--cd` pointing
somewhere else — and a room that opened looking successful would have them typing their next
brief into four fresh sessions believing it continued something. A file that **exists and
cannot be used** (corrupt JSON, unknown schema, another workspace's room) is not fatal: the
room opens unreattached and says loudly why. Telltale's own state being damaged must never be
the reason someone cannot open their tool. The bad file is left in place rather than deleted,
and the next completed turn overwrites it.

**Written at each turn's end, not only at quit.** The failure this exists to survive is the room
*not* getting a clean exit. Writes are atomic — temp file in the same directory, sync, rename —
because a torn file is exactly the input the corrupt path has to refuse, so a crash mid-write
would cost the user the reattach the feature exists to give them. Nothing is written before the
first turn: a room with no turns has no keys, and writing anyway would drop a file for every
launch in the wrong directory.

**The composition that had to be measured.** The sixth amendment states that the persistent
Claude session passes no `--resume` "because there is nothing to resume". Reattaching is the
case where there is — and whether `--resume` composes with `--input-format stream-json` was
genuinely open, since one flag had only ever been used on the spawn-per-turn path and the other
only on the persistent one. **Probed live, 2026-08-04, Claude Code 2.1.220, Windows**, in a
throwaway directory:

| | finding | strength |
|---|---|---|
| Composition | The process starts with the full persistent flag set **plus** `--resume <id>`, and takes a turn on stdin. | Measured |
| Real resume | Asked what word the *previous* process had been told to reply with, it answered `ALPHA` — a fact only the prior session's history carries. Not a fresh session that happened to launch. | Measured |
| Id stability | The reported `session_id` is the **same id**, unchanged. This is what keeps the saved file valid across repeated reattaches: the key does not rotate out from under it. | Measured |
| Exit | Closing stdin exited 0. | Measured |
| Stale id | A well-formed id with no conversation behind it exits 1 with `No conversation found with session ID: <id>` and a `result` carrying `is_error` and `num_turns` 0. It fails **fast and free** — no model turn is spent. | Measured |
| `num_turns` | Came back as **1** on a genuine resume: it counts THIS PROCESS's turns, not the conversation's. It cannot be used to tell a resume from a fresh start. | Measured — recorded as a trap |

**A restored id is on probation until a turn survives it**, and this rule replaced a narrower
one that was wrong. The first implementation handled only the persistent Claude seat, spending
its id once at process launch. An independent review found the hole: **no adapter reports a
stale id as such.** Every `NextTurn` returns `ErrNoResume` only for an *empty* id — a
well-formed id whose conversation has aged out builds a perfectly valid `resume <dead-id>`
invocation, and the failure surfaces later as a dead process. Nothing deleted the id, so the
three spawn-per-turn seats would rebuild the same doomed invocation on **every turn for the
life of the room**. Reattaching a room a few days later is the ordinary case that hits it, and
the ErrNoResume fallback §4 relies on could never fire.

So there is one rule for all four seats. A restored id is dropped the first time a turn on it
fails; a turn that comes back clean takes it off probation permanently; a *cancelled* turn
changes nothing, because a keystroke says nothing about whether the vendor still has the
conversation. **Ids earned in this process are never touched by any of this** — the whole value
of resume is that history survives a bad turn, and a rule that discarded a thread on any
failure would quietly undo the feature it is part of.

**A refused thread gets its own words.** The vendor reports a dead thread as a failed turn,
whose stock wording — "the vendor reported the turn failed", or a raw `exit status 1: No
conversation found with session ID` — reads as *this vendor broke* and sends the user looking
for a problem with the vendor instead of retyping a brief. The column says the saved thread was
refused, that its history is gone, and that the next brief starts a new session with the brief
re-applied. A dead thread emits **two** events — the vendor's failed `result`, then the process
exit carrying its stderr — and the second must not overwrite the first, because only one of
them tells the user what happens next.

**A saved room you did not ask for is named before it is replaced.** The file is keyed by
workspace, so opening a room in a directory that already has one and dispatching a single turn
renames a fresh file over the old keys: four conversations become unreachable, silently and
irreversibly. Council does not refuse and does not prompt — it states once that a saved room is
there and that `--resume` reattaches to it, which is enough to make the loss a choice.

**Reattaching is per seat, not per room.** A vendor that never answered left no id; one
installed since the room was saved was never in it. Both open beside seats that do continue, so
each column says which it is. The header gains nothing at all when the room is fresh — this
feature is invisible until it is used, which is what keeps every other golden in this package
honest.

**A reattached seat carries the guard.** The eighth amendment gives the gated seat the user's
own `PreToolUse` hooks through `--settings`; a resumed session is built from the same `Session`
spec and passes the same hooks file. Reattaching restores a *conversation*, never a weaker
posture — a seat that came back unscreened while the badge still said the guard was wired would
be the quietest false claim in the room, and it is asserted rather than assumed.

**A brief is not re-sent to a resumed seat.** It is already in the history being replayed, and
re-sending would spend the whole brief again against a metered quota for a vendor that has read
it — the same reasoning §4 and the fourth amendment already apply to later turns. The saved
`brief_path` is compared and *reported* when it differs from this room's, never loaded:
reopening a private file this invocation did not name is the one thing `--brief` exists to keep
deliberate. Only the paths are compared, because comparing content would need the content this
file refuses to hold.

**The one general lesson, and it is the same one this file keeps learning.** The first cut of
this feature tested that the *fallback* worked, with a hand-written vendor double that returned
`ErrNoResume` for a stale id. No shipped adapter does that. It was the fourth amendment's
mistake in a new costume — asserting the flag rather than the effect — except this time the
test double was the thing asserting a behaviour nothing in the product had. A test can hold a
false claim in place just as firmly as a true one (fifth amendment); a *mock* can invent one.


### Amendment, 2026-08-04 (tenth): the room is somewhere to talk, not a per-turn ticker

Nine amendments made this a very good way to **send one turn**, and never noticed that it was
not a place to hold a conversation. Dispatching turn N cleared turn N-1's body, trace, clock
and cost off the screen, and the user's own words were never rendered anywhere at all — so
the room could show four answers to a question it could not show you, and then discard them
when you asked the next one. This was PR 4 of the original plan, ratified with the rest and
then skipped. The complaint that reopened it was one sentence: *"I need a place to talk to
you all without going through any of you individually."*

**A finished turn is filed, not erased.** Each column keeps a per-turn record — the brief,
the reply, the trace, the note, the elapsed, the cost, the phase — and renders the lot
oldest-first as one scrollable transcript. Three things follow, and the first is why this
shape was chosen over a second pane: the scrollback needed **no new mechanism**, because the
window, the overflow markers, the tail and `MaxScroll` were already the code that walks a
column's lines. A past turn carries its own numbers, since the header and badge line are
chrome for the turn in flight. And a seat left out of a turn records nothing for it: routing
means Codex's transcript can skip from turn 3 to turn 5, and filling that gap would be the
room inventing a conversation.

**What is echoed is the principal's words, and that is the one real design fork here.** The
literal bytes a seat receives are not the user's brief: a first turn carries the `--brief`
file, whose content this ADR deliberately keeps off `State`, and a rebuttal turn carries the
other seats' answers, which are another model's words. Echoing "exactly what was sent" would
have printed a private file on screen and labelled a vendor's output as the user's. So the
brief is echoed under the composer's own `›`, and whatever rode along with it is reported on
its own line. It is sanitized like everything else and **not redacted**: this is the user's
typing shown back to the user, so a `«redacted»` here would hide a secret from the one person
who has it, do nothing about the copy already sent to four vendors, and make the echo
disagree with what was dispatched — the single thing the line exists to show.

**The composer grows to six rows, and `ctrl+j` is a keystroke rather than a paste.** The
newline goes into the draft raw, bypassing the filter that flattens newlines. That filter
still runs on everything else, which is the point: it exists so a pasted log cannot tear the
footer apart, not to overrule a key the user pressed on purpose. Verified against every
transport this repo drives — stdin for Claude and Codex, a JSON-marshalled envelope for the
persistent Claude turn, and one argv element on a native binary for agy (§9.3's rule holds:
no shell, and Go quotes an argument containing a newline).

**A seat that cannot be driven no longer costs a quarter of the width.** On the reference
machine Cursor is permanently `AvailUnusable`, and it was holding a full column all session
to display one card that never changes. Those seats fold out and the survivors take the
width — but the *fact* does not fold away, because a seat nobody can see is one nobody has a
reason to go looking for. One line under the header names each collapsed seat and which
failure it is, preserving §4a.1's distinction between "not installed" and "installed but not
drivable" at one line instead of one column. `--vendor` is the explicit control and it is
deliberately stronger than the HUD's filter of the same name: a listed room is drawn **and**
dispatched to, since spending a vendor's quota on a column nobody can see is exactly the
hidden state this product refuses. It parses the `@mention` vocabulary rather than inventing
a second one.

The general lesson this file keeps learning, in its newest costume: every amendment above
improved the *turn*, and the thing that was broken was the *conversation*. Nine rounds of
making each dispatch more honest never surfaced that the surface forgot everything the moment
you used it again — because each turn, examined alone, was correct.

### Amendment, 2026-08-04 (tenth): the shim was a wrapper, and node underneath is native

The fifth amendment seated Cursor and then declared it `AvailUnusable` on Windows, on a chain of
three facts each of which was true:

> The prompt is argv-only. The only entry point is `cursor-agent.cmd`. `runner.
> ErrShellShimWithArgvPrompt` refuses an argv prompt on a shim, because cmd.exe's quoting cannot
> be made safe for arbitrary text.

Every step held. The conclusion did not, and the reason is worth more than the fix: **the third
fact is about a shell being involved, and the second fact was never checked against it.** "The
entry point is a `.cmd`" was treated as "a shell will process this prompt", and those are only
the same sentence when the `.cmd` does something. This one does not. It is nine lines that hand
its argv to `cursor-agent.ps1`, whose entire body picks a directory and execs a bundled
`node.exe` against `index.js`. The shell was a doorway, not a room — and the refusal was aimed
at a room.

So council does what the launcher does. Detection resolves this seat to
`…\cursor-agent\versions\<version>\node.exe` and the adapter passes `index.js` as `argv[1]`.
`node.exe` is a real executable, Go's `os/exec` quotes its own arguments, cmd.exe and
powershell.exe are both gone from the invocation, and the seat is `AvailInstalled` with its
prompt still in argv — the same argv transport that already makes agy safe, not an exception
carved for this vendor. The refusal itself is untouched and still armed; it simply has nothing
left to fire on.

**Then it was signed in, and that is where the real work was.** Four live turns ran against
2026.07.23-e383d2b on 2026-08-04. Three of the fifth amendment's bundle-derived rows survived;
three did not, and every one of the three would have shipped as a visible defect.

| | fifth amendment said | what running it showed |
|---|---|---|
| Windows drivability | **Unusable**, no native executable to point at | **Installed.** `node.exe index.js --help` is byte-identical to `cursor-agent.cmd --help` (86 lines, diff clean), needs no cwd and no environment |
| Streaming | **Unknown**, left unclaimed | **Token-level.** `"P"` then `"ONG"`; a sentence as `"I"`, `" said"`, `" P"`, `"ONG"`, `"."`. Promoted to `GranTokens` |
| Resume | id correspondence **never round-tripped** | **Verified.** Turn two on `--resume <session_id>` answered a question only turn one could answer, and re-reported the same id |
| Assistant events | one event per chunk | **plus a repeat of the whole message.** Concatenating both rendered `PONGPONG` |
| `tool_call` discriminator | `tool_call.tool.case`, read from the bundle | **`tool_call.readToolCall` / `.shellToolCall`.** The oneof is flattened to a key on the wire; the old lookup matched nothing and every trace entry read "tool call" |
| `--sandbox enabled` | **requested**, mechanism exists | **Refused on Windows.** `Sandbox requires macOS or Linux`, exit 1, before any model call |
| `-` prefixed brief | hazard **recorded unresolved** | **Settled.** Without `--`: `error: unknown option`. With it: a normal turn. The separator is now passed |

The lesson is not "the bundle was wrong". The bundle was right about what the program
*constructs*. It could not be right about what arrives at the other end of a pipe, and a parser
consumes nothing else. **Reading the source of a thing and reading its output are different
measurements, and the second one is the only one a parser can be tested against.** Two of the
three misses were invisible failures — a doubled reply and an empty trace — that no exit code
would have flagged.

**The posture got worse, not better, and the badge says so.** `--sandbox enabled` is no longer
passed on Windows: it does not weakly apply there, it kills the turn, so the flag that read as
the stronger half of this posture was the reason the seat could never have answered. And under
`--mode plan` the agent was seen selecting and dispatching `cat …` and `ls -1` as
`shellToolCall` invocations. A hook stopped them, not the mode — so whether plan mode would have
refused them on its own is *still* unobserved, but a "no edits" mode let a shell command reach
the permission layer. The badge stays `ro:requested` for a better reason than before: it is now
contradicted by a capture rather than merely unsupported by one.

Council does **not** answer the Windows sandbox failure by passing `--sandbox disabled`.
Declining to ask for a restriction is council's business; reaching into someone's config to
remove one is not. A user whose own config enables it gets the vendor's sentence classified into
an actionable card instead.

**Two new failures are classified, and both were found by running the thing once.** An untrusted
workspace refuses a print-mode turn outright — `Workspace Trust Required`, exit 1, before any
model call — and it is not a rare state: no directory on this machine was trusted for the CLI.
The vendor's own fix is `--trust`, which council still refuses in both postures on the fifth
amendment's reasoning, so the card sends the user to their own terminal where they can see what
they are agreeing to. The sandbox refusal above gets the second card. Neither pattern was
invented; both strings are copied off captured stderr, which is the bar for adding a case to
`failureNote` at all.

**The cost of this unlock is a dependency on someone else's directory layout, and it is paid
explicitly.** `cursor-agent` auto-updates by dropping a new `versions/<version>/` beside the old
one, so the resolution is re-derived on **every** room startup and never cached — a remembered
path outlives the directory it names and would turn a detection question into a failed turn.
The version-name pattern and the date sort are transcribed from the vendor's own launcher rather
than inferred from the one directory on this machine, and both of its accepted forms are pinned
by tests. When the layout stops matching, the seat degrades to `AvailUnusable` with a note naming
the path it expected and did not find. It never falls back to driving the `.cmd`: an empty column
is a cost, and prompt text through cmd.exe is a defect.

**Still owed, and it is one specific thing.** A tool call that *succeeds*. Every tool call across
all four turns was blocked by broken hook wiring on the probe machine, so `result.success` has
never come down this pipe — the `ActOK` branch is the one part of this parser still resting on
the bundle's own field descriptors rather than on a capture. It is labelled as such in the code
and in its test. Everything else in the fifth amendment's table has now either been measured or
been replaced by what the measurement said instead.

### Amendment, 2026-08-04 (eleventh): one room, and the workspace is a field inside it

The ninth amendment gave the room a file and the tenth gave it a memory, and the launch path
still treated both as properties of a directory. Opening council meant supplying a workspace —
by `--cd` or by standing in one — and the state file was keyed by the sha256 of that path, so
the conversation the user wanted back was reachable only by re-naming an argument to a room
that already existed. The complaint that forced this was restated three times in one session,
and the load-bearing sentence survives cleanup intact: *"I should be able to just invoke the
council and talk to you all there, and go into whatever repo I want."* The anger was aimed at
one specific thing — naming a repo at launch — and the ruling that resolves it is a sentence
about object identity, not about flags: **the room is the persistent object; the workspace is
conversation state, changed from inside the room, never an invocation input.**

**`telltale council`, zero arguments, opens the one global room and reattaches.** This
deliberately flips the ninth amendment's default. That default was offer-don't-restore — name
the saved room once, make the loss a choice — and it was right *while the file was keyed by
workspace*, because an automatic reattach could reattach to the wrong room whenever the key
(usually a stray `--cd`) pointed somewhere the user did not mean. With one room there is no
wrong room to reattach to, and the P0 sentence above is the sanction for the flip: a user who
says "invoke the council and talk" is asking for the conversation, not for a fresh one.
`--fresh` is the opt-out and starts over; if a usable saved room exists it is named once before
the first dispatch replaces it — the ninth amendment's "named before it is replaced" rule,
unchanged, now standing guard on the other default. `--resume` stays accepted and is now
redundant, because it names the default. With nothing saved, a zero-arg launch opens fresh
silently — that is the first run ever, and a notice would be noise on an empty state. An
*explicit* `--resume` with nothing saved gets a notice rather than the ninth amendment's
before-the-alternate-screen error: that error existed because a per-workspace key could point
at the wrong room and a silently-fresh room would have the user typing into four new sessions
believing they continued something. There is no key any more, so there is nothing to be wrong
*about* — the notice says nothing was saved, and the room opens.

**Room identity is one file, and the workspace moved inside it.** `~/.telltale/council/room.json`,
schema v2, same 0600-in-0700 discipline. The workspace is demoted from the file's KEY (the
hashed filename) to a MUTABLE FIELD of the room. Migration is adoption, not conversion: on
first launch with no `room.json`, the newest valid v1 per-workspace file by `saved_at` is
adopted as the global room — which is literally "reattach to the prior conversation", the thing
the feature exists to do — and every v1 file is left on disk untouched: never written again,
never deleted. A `room.json` that is corrupt or version-skewed gets the same fail-closed
Ignored-notice behaviour the ninth amendment specified, and **no legacy fallback in that case**:
falling back would let a corrupt current room silently resurrect an older conversation and
present it as the one just lost, which is a worse failure than the notice.

**The privacy argument weakens, and it is stated as a trade rather than defended away.** The
ninth amendment hashed the filenames precisely so a listing of `~/.telltale/council` was not an
inventory of what the user works on. One fixed filename holding one workspace path gives that
up: `room.json` now names, in plaintext, the single directory the room currently points at.
That is one path, not an inventory, and the file still holds keys and no content — but it is a
real narrowing of the ninth amendment's property and this file does not pretend otherwise.

**`/cd <dir>` moves the room from inside it.** Only `/cd` is intercepted in the composer; any
other draft — including one that merely starts with `/` — dispatches as text, because a
composer that silently eats near-misses is a composer that loses briefs. Resolution order is
absolute path, then relative to the current workspace, then **sibling of the current
workspace**, so `/cd kb-agent` works from any sibling repo without telltale hardcoding anyone's
directory layout. The target must exist and be a directory, and the command is refused mid-turn
— a seat cannot be retargeted while a dispatch is in flight against the old target. `--cd`
survives as an optional launch-time override of the room's current workspace; the third
amendment's throwaway-worktree recipe is its remaining natural use, and the daily path never
needs it.

**Seat mechanics on a switch, each claim at its own strength.**

| | mechanism | strength |
|---|---|---|
| Codex / agy / Cursor (spawn-per-turn) | `specFor` already threads the live workspace into each turn's `Spec.Dir`; the three seats follow a `/cd` with no new mechanism at all | Unchanged — the code path that ships |
| Claude (persistent) — why it cannot follow | the stream-json input envelope has **no cwd field**, and no `control_request` subtype changes cwd; the documented mechanism for a mid-conversation workspace switch is respawn with `--resume <session_id>` | **Docs-derived** (Claude Code Agent SDK / headless docs), not measured |
| The respawn composition itself | `--resume <id>` plus the full persistent flag set: same `session_id`, real history replayed, exit 0 | **Measured** — ninth amendment, live probe |
| `--add-dir` | grants file *access* only; it does not move path resolution, so it is not a substitute and is not part of this design | Docs-derived |

So a `/cd` marks the persistent seat's process as pointed at the wrong directory, and the next
dispatch respawns it in the new one, spending the earned session id through the **same
one-attempt probation rule** restored ids already carry: spent once, dropped on the first
failed turn, proven permanently on a clean one. No second rule was invented for this. The brief
is **not** re-sent to the respawned seat — it is in the history being replayed, which is the
same reasoning the fourth and ninth amendments already apply to every resumed turn. The honest
residual: "the envelope has no cwd" is read from the vendor's documentation, which is the
weakest evidence class this file uses, and the day the envelope grows one, the respawn can be
retired for something cheaper.

**What is deliberately kept, restated so the flip cannot be misread as a loosening.** Posture
is never restored from disk: `--write` is typed or it is not in effect, exactly as the ninth
amendment ruled, and auto-reattach changes nothing about it — the room that reopens is a read
room. The saved `brief_path` is reported when it differs and never auto-loaded, unchanged.
`TELLTALE_COUNCIL_BRIEF` is how the daily path stays zero-flag *and* briefed — the environment
carries the standing context so the command line can stay empty.

**The new consequence, stated plainly rather than discovered later.** One global room means any
two councils open at once now share one state file, and the last save wins. The hazard existed
before — two rooms in the same directory could already trade the same per-workspace file — but
it was bounded by the directory and is global now: two terminals, two different repos, one
`room.json`. Council does not lock the file and does not pretend to; the second room's next
completed turn overwrites the first's keys, and the ninth amendment's atomic-write rule is what
keeps the loser's overwrite torn-free rather than harmless.

The general lesson, in this file's own terms: nine amendments perfected the turn, the tenth
gave the room a memory, and none of them noticed that the launch path still made the user name
a directory to enter a room that already existed. Every earlier lesson here was about a *claim*
being wrong — a flag asserted, an effect unverified. This one is about the *object* being
wrong: the room was modelled as a property of a workspace, when the whole time the workspace
was a property of the room. No measurement could have caught it, because every measured thing
was true; the user saying the same sentence three times is what caught it, and that is a
measurement too.

## Verification status

Flag surfaces were verified against the installed binaries' own `--help` output and, for Claude
Code, against the live headless documentation. Still unverified and scheduled as a live spike
before the Codex and Antigravity columns land: the Codex `--json` event schema and delta
granularity, whether `codex -s read-only` actually engages on Windows, and Antigravity's
stream-json schema, conversation-id location, stdin support and `--sandbox` semantics. Those
columns render honest *requested* badges until the spike says otherwise.

**Cursor stopped being the standing exception on 2026-08-04.** It was signed in, four turns ran,
and the tenth amendment records what each of them changed. The four questions this paragraph used
to list as owed are all answered — `--mode plan` restricts less than its name suggests,
`--stream-partial-output` produces real token-level deltas, `session_id` *is* the id `--resume`
wants, and a brief starting with `-` *does* need the `--` separator, which is now passed. Its
posture badge is still `ro:requested`, now because a capture contradicts anything stronger rather
than because nothing could be observed.

One item is owed on this seat and one only: **a tool call that succeeds.** Every call across those
four turns was blocked by broken hook wiring on the probe machine, so the parser's `ActOK` branch
still rests on the shipped bundle's field descriptors rather than on a captured line. It is
labelled as bundle-derived in `vendors/cursor.go` and in its test, and one clean tool call on any
machine closes it.
