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

3. **Enforced Read-Only Process Sandboxing**  
   `internal/council` dispatches prompts via local CLI wrappers with enforced read-only sandboxing:
   - **Codex (GPT)**: `codex exec -s read-only --skip-git-repo-check --cd <workspace> "<prompt>"`
   - **Antigravity (Gemini)**: `agy -p "<prompt>"` (with command-level mutation soft-denied)
   - **Claude Code**: `claude -p "<prompt>"` (read-only invocation)
   Output streams are sanitized and credential strings are redacted before rendering to TUI viewports.

4. **Blind First-Round Protocol & Multi-Turn Transcript**  
   - **Turn 1 (Blind Dispatch)**: The opening brief is dispatched to all 3 vendors simultaneously without cross-agent context, ensuring completely independent opinions.
   - **Subsequent Turns (Rebuttal/Discussion)**: Follow-up turns include previous prompt/response history where prior agent responses are formatted as labeled, untrusted quoted material.

5. **Bubble Tea TUI Layout**  
   - **Top Header**: Active workspace directory and Council status bar.
   - **Viewport Body**: 3 vertical side-by-side columns (or tabbed views on narrower terminals) rendering streaming stdout from Claude Code, Codex, and Antigravity.
   - **Prompt Footer**: Interactive text input bar (`Bubble Tea textinput`).
   - **Keybindings**: `[Enter]` to dispatch prompt to all agents, `[Tab]` to cycle vendor focus, `[Esc]` to edit prompt, `[q]` or `[Ctrl+C]` to teardown session cleanly.

6. **Fail-Closed Fallback for Uninstalled/Unconfigured Vendors**  
   If a vendor CLI binary is absent or unauthenticated, `telltale council` does not crash. It renders a degraded explanation card in that vendor's column while allowing the active vendors to proceed normally.

## Consequences

- Incorporates Codex's independent review feedback, guaranteeing both process-level read-only safety and model independence.
- `telltale` expands beyond status monitoring to include multi-vendor agent dispatch without compromising the read-only guarantees of `telltale hud`.
- Provides San with a unified, real-time command center for multi-model decision-making directly inside Windows Terminal.
