# telltale — decision ledger

One dated line per ruling. This file exists because `docs/design.md` grew to
eighteen thousand lines by recording taste beside measurement, and taste that is
recorded starts to feel settled. Measurements go to `PARITY.md` and to tests.
Decisions go here, in one line each, with the reason. A section in `design.md`
is no longer the price of a change.

- **2026-09-02 — Telltale is a harness that measures what it's driving and
  refuses to drive one unwatched.** (San.) This replaces "an instrument, not a
  harness." The room crossed that line at `/adopt`, and §9.55 (the room as
  integrator) and §9.56 (routing) finished the crossing; the ruling says so out
  loud. The honesty rules (§4a.1) bind the harness exactly as they bound the
  gauges.
- **2026-09-02 — Three taste rulings are LIFTED.** (San.) "Council adds no hues
  of its own" (§9.28, 2026-08-07); colour and any single glyph as only ever a
  second signal; Windows Terminal as the reference renderer. Reason: they were
  correct for honesty and wrong for how the room looks, and nobody revisited
  them because they were recorded. `--ascii` and `NO_COLOR` must still render a
  usable room; that is accessibility, not taste, and it stays.
- **2026-09-02 — What stays, by name, because it is load-bearing.** (San.) The
  spawn guard (a plain test run once started a billed agent turn with write
  access); the zero-versus-absent tests; and measured-at-a-pinned-version for
  any claim said on a stage. Everything else is negotiable.
- **2026-09-02 — No new `design.md` sections through 2026-09-30.** (San, "the
  pstack cut.") The 58 sections of §9 and 30 of §7 stay as history. New
  measurements: `PARITY.md` or a test. New decisions: a line here.
- **2026-09-03 — The room's identity is MONOGRAPH, with four grafts.** (San, on
  an independent audit of three rendered prototypes.) One ink at six values, two
  accent pigments, and the measured value as the brightest thing on screen.
  Grafted onto it: the race board's fixed lane and empty track, the leading
  verdict marks and `/adopt`, and the posture badges ordered by EVIDENCE, from
  `explore/room-broadcast`; the continuous posture rail and the recorder-strip
  trace, from `explore/room-instrument`, repainted in this palette. Two rulings
  ride with it — seat hues stay RETIRED, and truecolour may enhance the identity
  but may never define it, so every distinction is carried by value, weight and a
  word. `docs/room-identity.md` is the page; `--ascii` and `NO_COLOR` are
  untouched, which is why six of 118 goldens moved.
- **2026-09-03 — `NEEDS YOU` means a vendor is stopped on a keystroke, and
  nothing else may say it.** A gated WRITE room at the end of an `@all` brief
  drew `⚠ NEEDS YOU   2 Codex done   3 Antigravity done   4 Cursor done   5
  Grok done` with no gate pending, and two readers saw a blocked seat that was
  not there. The strip was §9.54's inbox, and it was correct; the lead was the
  gate's word and the gate's mark on a reply that could wait. Reason: §4a.1
  binds the lead as it binds the entries, and a qualifier at Muted does not
  overrule a lead at Alert. The inbox now opens `UNREAD` with no mark; one
  blocked seat on the line brings `⚠ NEEDS YOU` back. The recorder was not at
  fault: `replay-check` reported no gate card because there was none.
- **2026-09-04 — A machine may pay for a live claim, and it must ask first.**
  `telltale probe` drives each installed seat through its handshake, one turn of
  one word and its stop, and `telltale doctor` reports what it recorded. This is
  the first mode that SPENDS the operator's money, so three things bind it: it
  states the cost before it runs, it refuses to run with no terminal unless
  `--yes` is given, and its one-word brief is a constant rather than a flag. Its
  file is a fourth bounded write exception and the strictest one: no brief, no
  reply, no session id, no path, and no failure reason, because a vendor's own
  error line carries paths. Reason: `doctor` could say telltale's survey was
  stale and nothing on the machine could answer it, so every live claim was paid
  by hand and recorded in prose. What is NOT probed is part of the ruling: no
  write, no approval flow, no resume. Those stay hand measurements with a reader.
- **2026-09-03 — Telltale spends width on the trace that needs it, time on the
  silence between acts, and states each fact once where it belongs.** (The integrating session, on
  an independent audit of three prototypes rendered from a real recorded room.)
  The density and repetition pass. CLOCK is the base: the cue row, the quiet
  clock, the act count, the scroll cue on the chrome row, and the per-column turn
  coordinate. From LEDGER, as a hard invariant: a fact about the ROOM prints once
  on the room's own line, and a column prints only what is true of that seat.
  From STRIP: a hard width threshold, below which a column changes FORM to a
  strip. Refused, and named on the page: the LEDGER lane's narrow-width
  transcript geometry; the STRIP lane's history-navigation row; `nothing has
  arrived yet` as the description of a quiet seat; the strip form above the
  threshold. `docs/room-identity.md` carries the argument. The identity does not
  move.
- **2026-09-04 — A recording's SHAPE may be committed; its WORDS may not.** (The
  scrub lane, on the 2026-09-04 adversarial review's fault F.) "Fixtures are
  synthesized, never real" binds the content, so `telltale council replay-scrub`
  keeps every structural fact of a real room and replaces every word with
  synthesized text of the same length. `examples/demo.jsonl` is the first such
  file, and two goldens pin its frames. The claim travels in the file
  (`scrubbed: true` on the room line), because a scrubbed replay must never pass
  for a capture: same rule that puts REPLAY on every frame.
- **2026-09-04 — The demo is five beats, and the observability surfaces leave
  it.** (San.) The room and one `@all` brief, `ctrl+r`, the gated write card
  with the posture rail, `/arena` with its check and `/adopt`, and the whole
  session under `--record` with a `replay-check` and a scrubbed replay to close.
  `telltale doctor` becomes a one-line aside after the card beat, and `telltale
  hud`, `telltale snapshot` and the `y` clipboard beat leave the demo and stay
  in the README. Reason: those are observability surfaces, which is ground other
  tools already own, and the demo's first ninety seconds must do the things a
  session supervisor has no verb for. This closes the beat order the 2026-08-17
  amendment reopened. The geometry is ruled with it: the 38-inch 3840x1600
  display shared through Zoom, at the ordinary desk window of about 181 by 71
  cells, never maximized.
- **2026-09-04 — v0.3.0 is cut with the crew checklist unpaid.** (San.) The
  2026-09-02 amendment made that checklist the next minor's precondition. It
  stays the precondition for a demo of measured claims, and it is no longer the
  precondition for the tag. Reason: the README installs v0.2.0 of 2026-08-14,
  more than 130 commits behind `main`, and a visitor who follows the README must
  get the room the README describes. The release notes name the checklist as
  owed.
