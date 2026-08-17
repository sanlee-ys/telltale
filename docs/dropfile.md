# The drop-file relay

telltale ships an adapter for a fixed set of tools. Every one of those adapters
reads that tool's own store, and every field in it is a measurement telltale
took. That is the product's whole contract ([ADR-001](design.md#adr-001)), and
it is also why the adapter set is small: telltale cannot honestly draw a row
for a tool whose store it has never surveyed.

The drop file is the other door. A tool telltale ships no adapter for — or a
script you write around one — can write a small JSON document in a known shape,
and telltale draws it as a fleet row. No plugin runs, no code of yours executes
inside telltale, and telltale writes nothing back.

**Every value in a drop file is your claim, not telltale's measurement.** The
row says so, on every surface, and [the honesty rules](#how-a-drop-file-row-is-marked)
below are the part of this format you should read even if you skip the rest.

## Where the file goes

```
~/.telltale/dropfile/<name>.json
```

`<name>` is yours to pick. It becomes the row's stable id, so keep it stable:
the same session should keep the same file name for as long as it lives.

One file is one row. A tool running three sessions writes three files.

Set `TELLTALE_DROPFILE_DIR` to read the files from somewhere else instead.

To see what telltale made of your file:

```
telltale snapshot --vendor self-reported
```

That prints the parsed result as JSON and exits, which is the fastest way to
tell a rejected document from an accepted one. Once it looks right, `telltale
hud` draws the row.

## A complete example

```json
{
  "schema_version": 1,
  "tool": "windsurf",
  "name": "refactor the parser",
  "workspace": "C:\\src\\code\\example-app",
  "model": "gpt-5-codex",
  "context_pct": 42.5,
  "cost_usd": 1.25,
  "last_activity": "2026-08-16T15:04:05Z",
  "subagents": 2
}
```

That document draws this row, beside a measured Claude Code row for contrast:

```
 telltale  │  2 sessions  │  claude 1  self-reported 1
 ─────────────────────────────────────────────────────────────────────────────
        SESSION                                  MODEL         CONTEXT    COST
 ● CC │ telltale  C:\src\code                    Opus 5     ███▊──  34%  $1.20
 ● SR │ windsurf: refactor the parser  C:\src…   gpt-5-c…   ████▌─  43%  $1.25
```

## Fields

Two fields are required. Without either one there is no row at all — see
[Rejection](#rejection-what-produces-no-row-at-all).

| Field | Type | Meaning |
|---|---|---|
| `schema_version` | integer | Must be `1`. The contract number for this format. |
| `tool` | string | Who is making the claim. It leads the row's label. |

Everything else is optional.

| Field | Type | Range | Maps to |
|---|---|---|---|
| `name` | string | | The session's own label. Rendered after `tool`. |
| `workspace` | string | absolute path, native format | The row's folder column. |
| `model` | string | | The model id driving the session. |
| `context_pct` | number | 0–100 | The context gauge. Percent of the window in use. |
| `cost_usd` | number | ≥ 0 | The cost column. **US dollars only.** |
| `last_activity` | string | RFC 3339 | When the session last did something. Drives liveness. |
| `subagents` | integer | ≥ 0 | The sub-agent count chip. |

`cost_usd` is USD and telltale will not convert. If your tool reports another
currency, omit the field rather than converting at a rate nobody measured.

`context_pct` is a percentage on a 0–100 scale, not a 0–1 fraction. Convert
once, on your side.

## Absent is not zero

This is the distinction telltale exists to keep, and a drop file has to keep it
too.

- **`0` is a measurement.** It means you looked and the answer was zero. A
  `"cost_usd": 0` draws `$0.00`, and a `"context_pct": 0` draws a full empty
  gauge track.
- **Absent means you have no reading.** It draws `—`, which is a different mark
  and means a different thing.

Write absence in **either** of two ways. They mean exactly the same thing:

```json
{ "cost_usd": null }     // explicitly null
{ }                      // key simply omitted
```

Both accepted, because a writer cannot always be made to emit every key, and
failing a document over a field it had no value for would be worse than reading
it. What is *never* accepted as absence is `0`. Do not use a sentinel number —
not `0`, not `-1` — for "I do not know".

## Staleness

telltale judges your file by its **modification time**, which is the one thing
about it telltale measures rather than reads.

| The file's mtime | What happens |
|---|---|
| within 24 hours | The row draws. |
| older than 24 hours | No row at all. The claim has expired. |
| more than 5 minutes in the future | No row at all. That clock cannot be reasoned about. |
| older than 5 minutes | The row draws, and carries the reading's age in the detail pane. |

**So keep writing the file.** Rewrite it whenever the session does something —
on every turn boundary is a good rule. A file you stop writing stops drawing a
row within a day, on purpose: a writer that died should go quiet rather than
keep asserting whatever it last said.

### `last_activity` cannot outrun the file

If `last_activity` claims a time later than the file's own mtime, telltale uses
**the mtime** and records the substitution.

A file cannot have activity newer than its own last write, so this is the one
claim in the format telltale can check — and it checks it. The effect is that
writing `"last_activity": <now>` into a file you then stop updating does not pin
your row to the top of the grid; the row ages exactly as the file does.

## How a drop-file row is marked

A drop-file row is **never** allowed to look like a measured one. Three things
carry that, and none of them is a colour:

1. **The vendor id is `self-reported`**, for every drop file, always. The HUD's
   identity column reads `SR` and the header census reads `self-reported 2`.
2. **`tool` leads the row's label**, because `SR` is shared by every drop file
   and cannot tell one writer from another.
3. **`telltale snapshot` sets `"self_reported": true`** on the vendor entry.

There is **no field in this format for a vendor id**, and that is deliberate. A
drop file cannot claim to be Claude Code or Codex, because there is nowhere in
the document to make the claim — not because telltale checks for it.

Note what `self_reported` is *not*. The snapshot's `estimated` array marks
fields telltale **computed** from something that was not the value, and the HUD
draws those with a leading `~`. A drop-file value is not computed — telltale
read it verbatim, exactly as written. The two are different statements, and
mixing them would lose the difference between "telltale inferred this" and
"somebody asserted this". A drop-file row therefore carries **no** `~`, and its
`estimated` array is empty.

## Degradation: one bad field does not lose the row

A value telltale cannot use costs that field and nothing else. The rest of the
row still draws, the bad field renders `—`, and the reason appears in the
detail pane.

This happens when a field is the wrong JSON type (`"context_pct": "forty two"`),
or holds an impossible value (`"cost_usd": -3`, `"subagents": 1.5`).

An out-of-range percentage is **dropped, never clamped**: `"context_pct": 140`
renders `—`, not `100%`. A clamped number is invented data.

## Rejection: what produces no row at all

Degradation is for a field. These four are for the whole document:

- **`schema_version` is missing, or is not `1`.** A contract number telltale
  does not speak means the field names may no longer mean what telltale thinks.
  Reading it anyway would invent every value at once.
- **`tool` is missing or empty.** A claim with no claimant cannot be attributed,
  and attribution is this row's whole honesty requirement.
- **The file is not valid JSON**, or is not a JSON object.
- **The file is larger than 64 KiB.** A drop file is a handful of keys.

Files that are not `<name>.json` are ignored entirely, as are dotfiles and
subdirectories. A `README.md` in that directory is safe.

## What this format deliberately cannot express

Each of these is a decision, not a gap waiting to be filled. Asking for one of
them is asking to change the honesty rules, so the reasoning is here rather than
in a changelog:

- **Quota / rate-limit windows.** Quota is a property of an *account*, and
  telltale sources it from the statusline relay ([§7.15](design.md#s7-15)). A
  session-shaped document has no account to speak for, and a per-session quota
  claim is one no vendor publishes.
- **Liveness.** You cannot declare your row "live". A writer that could set its
  own liveness could pin its row to the top of the grid forever, and no reader
  could check it. telltale classifies liveness from `last_activity`, which the
  mtime rule above bounds.
- **Token counts.** They feed a fleet spend total ([§7.17](design.md#s7-17))
  that sums across vendors. A claimed count summed beside measured ones would
  produce a total carrying no mark at all.

## Writing the file safely

telltale may read your file at any moment. Write to a temporary file in the
same directory and rename it into place, so a scan never reads half a document:

```python
import json, os, tempfile, datetime

d = os.path.expanduser("~/.telltale/dropfile")
os.makedirs(d, exist_ok=True)

doc = {
    "schema_version": 1,
    "tool": "windsurf",
    "name": "refactor the parser",
    "workspace": os.getcwd(),
    "context_pct": 42.5,
    "cost_usd": 1.25,
    "last_activity": datetime.datetime.now(datetime.timezone.utc)
                     .replace(microsecond=0).isoformat().replace("+00:00", "Z"),
}

fd, tmp = tempfile.mkstemp(dir=d, suffix=".tmp")
with os.fdopen(fd, "w") as f:
    json.dump(doc, f)
os.replace(tmp, os.path.join(d, "example-app.json"))
```

Rename is only atomic within one volume, which is why the temporary file goes in
the same directory rather than in the system temp directory.

When the session ends, delete the file. It would expire on its own within a day,
but a row that disappears when the work stops is the honest one.

## Removing a row

```
rm ~/.telltale/dropfile/example-app.json
```

The row disappears on the next scan. telltale never deletes these files itself —
the directory is yours.
