# examples

## `demo.jsonl`

A council room that really ran, on 2026-09-03, with every word replaced.

Play it:

```
telltale council --replay examples/demo.jsonl --replay-speed 8
```

Nothing starts. The replay draws the recorded room from the file, so a machine
with no vendor CLI installed sees the same room the operator saw.

**What the file holds.** Five seats, and four of them take turns: `--vendor`
left Cursor off the dispatch. Seven briefs, turns 6 to 12, over 39 minutes and
36 seconds. 1,863 records. One approval card, raised on a write and answered
`y`. 314 tool calls. Ten stale exits, which are replaced processes ending after
their seat moved on. Turns 10 and 11 go to two seats only. Turn 12 goes to one.

**What was replaced.** Every brief, every reply, every path, every file name,
every session id and every request id. `telltale council replay-scrub` wrote
this file from the capture: it keeps each record kind, each millisecond offset,
each turn number and route, each act and its outcome, each exit code, each cost
figure, and the seats with their postures. It replaces every word with
synthesized text of the same length, so the room wraps and scrolls exactly as
it did. The room line carries `scrubbed: true`, and the replay says `scrubbed`
in its notice.

**What a recording cannot hold**, so the replay cannot draw it: the operator's
own cancels and give-ups, the focus moves, the scrolling, and the text of the
`--brief` file. A column the operator cancelled live replays as the vendor's
own exit.

Read `telltale council replay-check examples/demo.jsonl` for the identities the
file carries. They are synthesized too, and the first line of the output says
so.
