# 006 — Antigravity CLI becomes the fourth built-in adapter, on a hand-written SQLite reader

Status: Accepted (2026-08-02)

## Context

ADR-004 shipped agy as a statusline-only vendor and said so in its title: *not (yet) a
HUD vendor*. Its reasoning was a disk survey that found conversations stored as SQLite
databases full of undocumented protobuf, and no transcript at the path both the vendor's
docs and its own statusline payload advertise.

ADR-005's scheduled re-survey ran the same day, against the same agy version (1.1.9),
and reversed both halves (design.md §3.8 re-survey block; ADR-004 carries the
amendment):

- **The transcript is real** — written default-on for every conversation at
  `~/.gemini/antigravity-cli/brain/<id>/.system_generated/logs/transcript.jsonl`, plain
  JSONL, one record per step, verified 38/38 against the `steps` table. The first
  survey's "never written" was wrong at the same version.
- **`gen_metadata` decodes with a stdlib protobuf wire walk**, and — the part that
  matters — the token counts carry an arithmetic identity: `thinking + answer ==
  output`, which held 15/15 across the surveyed corpus. That is what separates a field
  map from field-guessing: a wrong guess about which number is which breaks the identity,
  and the identity is checkable at read time.

§8's roadmap named the adapter the next work item. San greenlit the build.

## Decision

`internal/adapter/antigravity` ships built-in: wired into the HUD's adapter slice, the
vendor filter cycle, the `--vendor` flag, the header counts and the empty-state vendor
table. `VendorAntigravity` is `agy` — the binary's own name, what the user types and
what the header prints; a vendor column reading `antigravity` would cost eleven cells to
say what three say. The package is named for the product.

**Capabilities: four fields, all REPORTED, none derived.**

| Field | Verdict | Source |
|---|---|---|
| `name` | reported | the conversation id, shortened to eight characters for the grid |
| `model` | reported | `gen_metadata` `#1.#21` display name, `#1.#19` id as fallback |
| `workspace` | reported | the trajectory blob's `file:///` URI at `#1.#1` or `#7`, converted to a native path |
| `last_activity` | reported | the Q8 fold: newest transcript `created_at` vs. the mtimes of transcript, database and sidecar |
| `context_pct` | **CapNone** | the numerator is measured; the 1,048,576-token window appears nowhere on disk |
| `cost` | **CapNone** | consumer auth, no pricing on disk |
| `quota` | **CapNone** | server-refreshed in memory, never persisted |
| `liveness` | **CapNone** | deferred, see below |
| `subagents` | **CapNone** | deferred, see below |

Four rulings hold the rest of it up.

### 1. Zero new dependencies: the SQLite reader is written into the repo

`internal/sqlite` reads database bytes directly — the 100-byte header, the
`sqlite_master` b-tree on page 1, table b-trees with their overflow-page chains (blobs
run to 25 KiB against a 4 KiB page, so overflow is the common path, not the exotic one),
the record format's serial types, and a **WAL overlay** applying SQLite's own recovery
semantics: salts must match the sidecar header's, the rolling checksum must verify, the
first frame that fails either ends the scan, and frames past the last commit frame are
not part of the database. It is read-only by construction — it takes `[]byte`, opens no
files and holds no locks.

**The rejected alternative is a pure-Go SQLite driver, `modernc.org/sqlite`** (or
`glebarez/go-sqlite`, the same engine repackaged). It would have been perhaps thirty
lines of adapter code instead of five hundred lines of reader. It was rejected for three
reasons, in order of weight:

1. **Minimal dependencies is contractual here, not a preference.** decisions/001 scopes
   this product around a statusline that is spawned fresh on every prompt, and §3.2
   already rejected a SQLite dependency once for the same reason. Reversing that for the
   fourth adapter would make the earlier refusal look like an accident.
2. **Cost of the payload.** `modernc.org/sqlite` is a machine translation of the C
   amalgamation: roughly 9 MB of generated Go added to a binary that currently links a
   TUI framework and nothing else. Every statusline invocation pays the link cost of a
   database engine it never calls.
3. **We do not want a database engine.** The requirement is "walk two tables and hand me
   the blob column". A SQL engine brings a query planner, a locking protocol and a write
   path — all of which are surface area on a file another process owns, and one of which
   (the write path) is a capability this product must not have. The hand-written reader
   *cannot* write; the driver merely doesn't.

The honest cost is stated rather than buried: five hundred lines of file-format parsing
is a real maintenance surface, and the mitigation is that it is exercised by fifteen
tests against generated fixtures covering overflow, WAL commit, corrupt frames, bad
headers, salt mismatches, torn tails and truncated files — and that its failure mode is
an error, never a wrong value.

### 2. Never open the live database; assert the identity or degrade

The database and its sidecar are read as **bytes** (`os.ReadFile` of both, missing
sidecar is normal), never opened for write and never locked. The two reads cannot be
atomic, so a checkpoint landing between them would pair a new database with a sidecar
describing the old one; the reader re-reads when either file changed underneath it, and
past that the defenses are structural — the WAL's per-frame checksums, and:

**Every decoded token row must satisfy `thinking + answer == output` before it counts.**
A generation that fails its own arithmetic contributes nothing to the totals and the
session carries a diagnostic saying a self-check failed. The field numbers are
reverse-engineered from an unversioned wire format; a failure is evidence the guess is
wrong for that row, and rendering the number anyway is precisely what decisions/001
forbids. On the live corpus at merge time the identity held **16/16**.

The measured totals are surfaced as detail-pane **extras**, not as fields: `uncached in`,
`output`, `generations`. `uncached in` is named that way on purpose — the cache-read
component sits at a field number §3.8 marks lower-confidence, and this adapter will not
fold an uncertain number into a certain one to make a rounder total. The obvious next
step — dividing the total by 1,048,576 to produce a context percentage — is exactly the
assumed denominator this product exists to refuse.

### 3. The PII boundary: the id is the name, and content is never read

Transcript `content` and `thinking` carry full prompt text and file contents, the
request blobs carry more of the same, and the account email appears in the vendor's own
log. The adapter's transcript record struct **does not contain those fields**: a field
that is never decoded cannot be accidentally rendered, logged or put in a diagnostic.

That is also why the session's name is its conversation id. agy writes no human title
anywhere — the only free text on its disk is somebody's prompt — so the id, shortened to
eight characters for the grid, is the only honest label available. The full id is on the
detail pane's `session` line, one keystroke away. A test plants a marker string in every
fixture transcript's `content` and `thinking` and asserts it reaches no field, extra or
diagnostic.

### 4. Liveness and subagents are deferred pending observation, not effort

`steps.status` looks like a liveness signal. §3.8 read `DONE` on all 38 observed rows and
never sampled an in-flight session, so the mapping from a status code to "working now" is
untested — and §4a.4 sets a high bar for a `LivenessHint` precisely because it is the one
place an adapter can lie undetected. The HUD classifies age from `last_activity`, same as
every other vendor.

`parent_references` is a real table, empty in every conversation surveyed, with
`has_subtrajectory` zero throughout and `define_subagent`/`invoke_subagent` present in
the tool registry. The structure exists; the observation does not. Declaring the field
and emitting zero would assert "this session is running no sub-agents", which the corpus
cannot support. Both become buildable the moment a live fan-out or an in-flight session
is observed; neither is blocked on work.

## Consequences

- **"Cross-vendor" now means four vendors on the disk seam**, and agy is served on
  *both* seams — the only vendor that is. ADR-004's title keeps its *yet*; its original
  verdict stands as an honest record of what the first survey saw.
- The README's "Antigravity is statusline-only in the other direction" claim was true
  when written and is now false; it is corrected in place with a pointer to both ADRs,
  because the reversal is the interesting part.
- The help overlay's vendor line changed separator from `->` to `>`. A fourth vendor
  pushed it past the 60-column floor, and `TestNoLineExceedsTheTerminalWidth` failed the
  build rather than shipping a sheared overlay. Recorded because it is the golden system
  doing its job on a change nobody would have thought to check.
- The empty-state vendor table, the capability golden and the README hero all show four
  vendors; the doc-sync tests pin them.
- Binary fixtures (`.db`, `.db-wal`) enter `testdata/` for the first time, generated by
  committed stdlib Python scripts and marked `binary` in `.gitattributes` — a single
  byte rewritten in transit fails a WAL checksum, which the reader under test would then
  silently ignore.
- Verification posture: the adapter is **live-verified**, not source-verified — agy is
  closed source, so this follows the Claude Code precedent. All five local conversations
  were read at merge time with an empty degraded set. What that does *not* cover is
  itemized: no in-flight session was sampled, no fan-out was observed, and the corpus is
  one machine, one day, one model.

## Downstream surfaces

design.md (§3.3 matrix gains an Antigravity column, §3.8 "Adapter built" block, §5 eval
table ×2 rows, §7.3 renders G/H/I re-spliced, §7.8 keyboard cycle + `--vendor` line, §8
adoption item 4 closed), README.md (status, hero frame + caption, adapter list, honest
claim, flags), decisions/README.md index, HUD goldens ×5, `.gitattributes`,
memory: telltale-project.
