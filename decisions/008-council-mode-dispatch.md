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
   | Claude Code | `--tools "Read,Glob,Grep"` (+ `--permission-mode plan`) | **Enforced by construction** — the write tools are not in the session. Costs the column Bash and web access; conservative by default. |
   | Codex | `-s read-only` | **Enforced** at OS level on macOS/Linux. On Windows, unverified — a `codex-windows-sandbox-setup.exe` ships in the vendored package, but whether it engages is unproven. Badge downgrades to *requested* until a live spike proves it. |
   | Antigravity | `--mode plan --sandbox` | **Requested, unverified.** The flags exist; their semantics are not established. |

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

## Verification status

Flag surfaces were verified against the installed binaries' own `--help` output and, for Claude
Code, against the live headless documentation. Still unverified and scheduled as a live spike
before the Codex and Antigravity columns land: the Codex `--json` event schema and delta
granularity, whether `codex -s read-only` actually engages on Windows, and Antigravity's
stream-json schema, conversation-id location, stdin support and `--sandbox` semantics. Those
columns render honest *requested* badges until the spike says otherwise.
