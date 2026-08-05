# Cursor replies land twice — build plan

Found by the live e2e on 2026-08-05. Cursor was told *"reply with exactly one
token: ALPHA"* and its column read `ALPHAALPHA`. Not cosmetic: the doubled body
is what the artifact store saved, so hop 2 received `AUDITED ALPHA ALPHAALPHA`.
A corrupted reply now travels down a chain.

**Not an ADR.** Work plan; delete it when the work lands.

---

## What is already known

`cursor-agent` sends a model call's text deltas and then a repeat of that call's
WHOLE message. Appending both renders the passage twice. The adapter drops the
repeat at `internal/council/vendors/cursor.go:562`:

```go
if cl.ModelCallID != "" || cl.TimestampMS == nil {
    return runner.Event{}, false
}
```

Two discriminators, both captured off the wire on 2026-08-04:

- a repeat that ends a **mid-turn** model call carries `model_call_id`
- the repeat that ends the **turn** carries neither `model_call_id` nor `timestamp_ms`

The live run proves a **third shape** exists that carries `timestamp_ms` and no
`model_call_id`, because two events got through and concatenated. Most likely a
single-segment turn with no tool call, where the vendor has one model call and
does not number it — but that is a hypothesis, and the whole point of item 1 is
that nobody gets to act on it before it is a capture.

Ruled out already, so nobody re-checks it: the `result` fallback is correctly
guarded at `internal/council/dispatch.go:503` (`c.Body == ""`), so it is not the
second copy.

## 1. Capture the stream before changing a line  [no code]

The repo's own rule, learned three times in the Cursor adapter alone: reading
what a program constructs and reading what arrives on the pipe are different
measurements, and only the second one a parser can be tested against.

Run `cursor-agent` in print mode with the same single-token brief the live test
uses, capture raw stdout JSONL, and record for **every** assistant event:
`model_call_id` present/absent, `timestamp_ms` present/absent, and the text.

Deliverable is the captured lines pasted into this file. The fix is designed
from those lines, not from the paragraph above.

## 2. Make the discriminator stop being a guess  [cursor.go]

Whatever item 1 shows, a third field test is the same bet a third time — the
current pair has now been defeated twice by version drift.

Preferred shape, to be confirmed against the capture: drop an assistant event
whose text is the concatenation of the deltas already accumulated for this model
call. A repeat is BY DEFINITION the completed form of text already sent, so
comparing against what was accumulated tests the thing itself rather than a
field that happens to correlate with it. Needs per-call accumulation state in
the adapter, which it does not have today.

Keep the existing field tests as the cheap path. The equality test is the belt.

Whatever ships, the comment above it states which lines off the wire it was
built from, like the two before it.

## 3. Tighten the live test so it cannot pass on a doubled body  [flow_live_test.go]

`internal/council/flow_live_test.go` asserts `Contains(body, "ALPHA")`. `ALPHAALPHA`
satisfies that, which is why a green run reported a broken column. It has to
assert the token appears **exactly once** in the cursor hop, and that codex's
reply contains it exactly once too — the second half is what pins that the
artifact carried a clean body forward.

This is the defect that let the first defect through, and it is worth fixing
even if item 2 slips.

## 4. Unit-pin the shape  [cursor_test.go]

A table case per repeat shape, fed the literal captured lines: mid-turn repeat
(has `model_call_id`), turn-final repeat (has neither), and whichever third
shape item 1 finds. Assert exactly one text event survives a delta+repeat pair.

## Seams for splitting

| Work | Files | Note |
|---|---|---|
| Item 1 | none — a capture | **blocks 2 and 4**; one owner, do it first |
| Item 2 | `vendors/cursor.go` | same owner as 1, or hand over the capture |
| Item 3 | `flow_live_test.go` | independent, can start now |
| Item 4 | `vendors/cursor_test.go` | needs item 1's lines |

Item 3 is the only one that can proceed in parallel today.
