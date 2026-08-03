# 008 — Interactive LLM Council mode for multi-vendor real-time agent dispatch

Status: Proposed (2026-08-03)

## Context

`telltale` was created under a strict posture rule: *telltale never writes; it is a read-only ribbon on a sail showing true agent airflow*.

However, as the fleet strategy evolved under `agent-ops/ADR-010` into a four-vendor orchestration model (Claude Code as control plane, Codex for independent challenge, Cursor in the IDE, and Antigravity for Gemini research), a missing capability emerged: **real-time round-table deliberation**.

San (Council Lead) requires an interactive terminal space where a single brief or prompt can be broadcast simultaneously to all three active reasoning model families (Anthropic/Claude, GPT/Codex, Gemini/Antigravity), allowing their streaming responses to be compared side-by-side in real-time, followed by multi-turn discussion.

## Decision

1. **New subcommand: `telltale council`**  
   We introduce `telltale council` as a distinct entry point (`cmd/telltale/main.go` and `internal/council`).

2. **Posture Separation Boundary preserved**  
   `telltale hud` and `telltale statusline` remain 100% read-only observers without write access, credential access, or network calls. `telltale council` is explicitly defined as an opt-in interactive dispatch room.

3. **Headless Dispatch Seam**  
   `internal/council` dispatches prompts via local CLI wrappers using non-blocking asynchronous processes:
   - **Claude Code**: `claude -p "<prompt>"`
   - **Codex (GPT)**: `codex exec --skip-git-repo-check --cd <workspace> "<prompt>"`
   - **Antigravity (Gemini)**: `agy -p "<prompt>"`

4. **Multi-Turn Shared Transcript Buffer**  
   `telltale council` manages an in-memory transcript buffer for the active council session. On each user turn, the history of previous prompts and responses is formatted into the payload sent to each vendor CLI wrapper, enabling continuous back-and-forth multi-agent discussion.

5. **Bubble Tea TUI Layout**  
   - **Top Header**: Active workspace directory and Council status bar.
   - **Viewport Body**: 3 vertical side-by-side columns (or tabbed views on narrower terminals) rendering streaming stdout from Claude Code, Codex, and Antigravity.
   - **Prompt Footer**: Interactive text input bar (`Bubble Tea textinput`).
   - **Keybindings**: `[Enter]` to dispatch prompt to all agents, `[Tab]` to cycle vendor focus, `[Esc]` to edit prompt, `[q]` or `[Ctrl+C]` to teardown session cleanly.

6. **Fail-Closed Fallback for Uninstalled/Unconfigured Vendors**  
   If a vendor CLI binary is absent or unauthenticated, `telltale council` does not crash. It renders a degraded explanation card in that vendor's column while allowing the active vendors to proceed normally.

## Consequences

- `telltale` expands beyond status monitoring to include multi-vendor agent dispatch without compromising the read-only guarantees of `telltale hud`.
- Provides San with a unified, real-time command center for multi-model decision-making directly inside Windows Terminal.
