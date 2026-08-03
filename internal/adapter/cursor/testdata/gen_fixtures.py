#!/usr/bin/env python3
"""Generate the Cursor (Composer) adapter fixtures.

Run from this directory:

    uv run python gen_fixtures.py
    (if that errors about a missing .env: env -u UV_ENV_FILE uv run python gen_fixtures.py)

…then re-run the package tests:

    go -C ../../../.. test ./internal/adapter/cursor/

Stdlib only — sqlite3 ships with CPython, so there is nothing to install and
nothing to pin.

EVERYTHING HERE IS SYNTHETIC. Invented composer ids, invented session titles,
invented workspace paths, invented numbers. This repo is public and the real
store carries prompt text, file contents, encryption keys AND LIVE ACCESS
TOKENS (docs/design.md §3.9) — no real store and no real fragment of one enters
testdata/, ever.

Three marker strings are planted deliberately, each so a test can assert it
NEVER reaches a rendered field, an extra, a diagnostic or a log line:

    SYNTHETIC-PROMPT-TEXT-MUST-NEVER-RENDER   prompt/subtitle/todo/message text
    SYNTHETIC-CREDENTIAL-MUST-NEVER-BE-READ   the ItemTable auth keys' shape
    SYNTHETIC-ENTITLEMENT-MUST-NEVER-RENDER   the plan-entitlement constants

The shapes reproduced here are the ones §3.9 recorded from Cursor 3.14.7:

  composerHeaders(composerId, workspaceId, createdAt, lastUpdatedAt,
                  isArchived, isSubagent, recency, checkpointAt, value)
  cursorDiskKV(key, value)      keys `composerData:<id>`, plus the noise keys
                                (`bubbleId:*`) an adapter must never read
  ItemTable(key, value)         the credential table, present and never walked
  workspaceStorage/<id>/workspace.json   {"folder": "file:///c%3A/..."}
"""

import json
import os
import pathlib
import shutil
import sqlite3

HERE = pathlib.Path(__file__).resolve().parent

PROMPT = "SYNTHETIC-PROMPT-TEXT-MUST-NEVER-RENDER"
CRED = "SYNTHETIC-CREDENTIAL-MUST-NEVER-BE-READ"
ENTITLEMENT = "SYNTHETIC-ENTITLEMENT-MUST-NEVER-RENDER"

# Epoch milliseconds, which is what the header columns hold. 1785700000000 is
# an invented instant in the fixtures' own past; the tests never compare it to
# a wall clock, only to each other.
BASE_MS = 1785700000000


def ms(offset_seconds):
    return BASE_MS + offset_seconds * 1000


SCHEMA_HEADERS = (
    "CREATE TABLE composerHeaders (composerId TEXT PRIMARY KEY, workspaceId TEXT, "
    "createdAt INTEGER, lastUpdatedAt INTEGER, isArchived INTEGER, isSubagent INTEGER, "
    "recency INTEGER, checkpointAt INTEGER, value TEXT)"
)
SCHEMA_KV = "CREATE TABLE cursorDiskKV (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB)"
SCHEMA_ITEM = "CREATE TABLE ItemTable (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB)"


def header_value(name=None, draft=False, archived=False, workspace_id=None):
    """The `value` JSON blob. Only `name` and `isDraft` are on the adapter's
    allowlist; everything else here exists so a test can prove it stays put."""
    v = {
        "composerId": "…",
        "isDraft": draft,
        "isArchived": archived,
        "unifiedMode": "agent",
        "forceMode": "edit",
        "numSubComposers": 0,
        "hasBlockingPendingActions": False,
        # The file list a turn touched. Vendor-written, still a description of
        # somebody's private tree, and never surfaced.
        "subtitle": "Edited " + PROMPT + ".md, notes.md",
    }
    if name is not None:
        v["name"] = name
    if workspace_id is not None:
        v["workspaceIdentifier"] = {"id": workspace_id}
    return json.dumps(v, sort_keys=True)


def composer_data(model_name=None, pct=None, used=None, limit=None, extra=None):
    """A `composerData:<id>` blob. Four fields are read; the rest are the trap."""
    d = {
        "_v": 3,
        "composerId": "…",
        "status": "completed",
        "generatingBubbleIds": [],
        "hasBlockingPendingActions": False,
        # Observed empty in every surveyed session: the cost schema exists and
        # is never populated.
        "usageData": {},
        # Observed zero in all 310 surveyed message rows. An adapter that reads
        # these renders "0 tokens" for a session that spent millions.
        "tokenCount": {"inputTokens": 0, "outputTokens": 0},
        # Encryption keys live right here beside the fields worth reading.
        "blobEncryptionKey": CRED + "-blob",
        "speculativeSummarizationEncryptionKey": CRED + "-spec",
        # Prompt text, in four of the shapes it takes.
        "text": PROMPT,
        "richText": {"root": {"children": [{"text": PROMPT}]}},
        "todos": [{"content": PROMPT, "status": "completed"}],
        "conversationMap": {"bubble-1": {"text": PROMPT}},
    }
    if model_name is not None:
        d["modelConfig"] = {"modelName": model_name, "maxMode": False}
    if pct is not None:
        d["contextUsagePercent"] = pct
    if used is not None:
        d["contextTokensUsed"] = used
    if limit is not None:
        d["contextTokenLimit"] = limit
    if extra:
        d.update(extra)
    return json.dumps(d, sort_keys=True)


# ------------------------------------------------------------------ the rows

ID = {
    "happy": "00000000-eeee-4fff-8aaa-000000000001",
    "derived": "00000000-eeee-4fff-8aaa-000000000002",
    "noworkspace": "00000000-eeee-4fff-8aaa-000000000003",
    "nodata": "00000000-eeee-4fff-8aaa-000000000004",
    "windowless": "00000000-eeee-4fff-8aaa-000000000006",
    "archived": "00000000-eeee-4fff-8aaa-000000000007",
    "subagent": "00000000-eeee-4fff-8aaa-000000000008",
    "draftflag": "00000000-eeee-4fff-8aaa-000000000009",
    "skew": "00000000-eeee-4fff-8aaa-000000000010",
    "noclock": "00000000-eeee-4fff-8aaa-000000000011",
}

# Far enough ahead that no plausible test-run clock catches up to it.
FUTURE_MS = BASE_MS + 400 * 24 * 3600 * 1000


def header_rows():
    """(composerId, workspaceId, createdAt, lastUpdatedAt, isArchived,
    isSubagent, recency, checkpointAt, value) — six sessions and five rows that
    are not sessions, which is the ratio the survey actually found."""
    return [
        # A complete session: title, workspace, model, the vendor's own percent.
        (ID["happy"], "ws-alpha", ms(0), ms(600), 0, 0, ms(600), ms(540),
         header_value(name="refactor the widget parser", workspace_id="ws-alpha")),
        # No vendor percent; raw token counts present, so the adapter derives
        # one and must mark it.
        (ID["derived"], "ws-beta", ms(10), ms(500), 0, 0, ms(500), None,
         header_value(name="wire up the retry budget", workspace_id="ws-beta")),
        # Its workspaceStorage directory is gone — Cursor prunes them.
        (ID["noworkspace"], "ws-gone", ms(20), ms(400), 0, 0, ms(400), None,
         header_value(name="triage the flaky test", workspace_id="ws-gone")),
        # No composerData row at all, no title, a workspace that names no
        # folder, and its newest timestamp an ISO-8601 STRING in an INTEGER
        # column: model, context and name all fall back, the workspace is
        # absent rather than degraded, and the mixed-timestamp parse is what
        # carries last_activity (the ISO string is newer than `recency`).
        (ID["nodata"], "ws-empty", ms(30), None, 0, 0, ms(100),
         "2026-08-02T21:00:00Z", header_value(workspace_id="ws-empty")),
        # ---- the five that are not sessions ----
        # The empty-state draft: one per install, flagged twice over.
        ("empty-state-draft", "empty-window", ms(40), ms(40), 0, 0, ms(40), None,
         header_value(draft=True)),
        # A window with no folder open.
        (ID["windowless"], "empty-window", ms(50), ms(300), 0, 0, ms(300), None,
         header_value(name="scratch thread")),
        # Archived: the Codex precedent says archived is ignored.
        (ID["archived"], "ws-alpha", ms(60), ms(900), 1, 0, ms(900), ms(900),
         header_value(name="last week's migration", archived=True, workspace_id="ws-alpha")),
        # A sub-agent row. Structural only in the survey, filtered defensively:
        # a fan-out is a chip on its parent, never a top-level row.
        (ID["subagent"], "ws-alpha", ms(70), ms(800), 0, 1, ms(800), None,
         header_value(name="sub-agent: run the suite", workspace_id="ws-alpha")),
        # A draft flagged only inside `value` — the id is a normal uuid.
        (ID["draftflag"], "ws-alpha", ms(80), ms(200), 0, 0, ms(200), None,
         header_value(name="unsent", draft=True, workspace_id="ws-alpha")),
        # ---- the clock cases ----
        # lastUpdatedAt is ahead of any plausible clock; recency is readable.
        # The guard must skip the first and carry the field with the second.
        (ID["skew"], "ws-beta", ms(90), FUTURE_MS, 0, 0, ms(700), None,
         header_value(name="clock ran fast", workspace_id="ws-beta")),
        # Every timestamp unreadable: the field degrades rather than guessing.
        (ID["noclock"], "ws-beta", ms(95), FUTURE_MS, 0, 0, FUTURE_MS, FUTURE_MS,
         header_value(name="no readable clock", workspace_id="ws-beta")),
    ]


def kv_rows():
    rows = [
        (f"composerData:{ID['happy']}",
         composer_data("composer-2.5", pct=37.05234375, used=94854, limit=256000)),
        (f"composerData:{ID['derived']}",
         composer_data("grok-4.5", used=28131, limit=256000)),
        # `default` is an unresolved alias and renders verbatim; no context of
        # any kind, so the percentage is absent rather than zero.
        (f"composerData:{ID['noworkspace']}", composer_data("default")),
        (f"composerData:{ID['skew']}", composer_data("composer-2.5")),
        (f"composerData:{ID['noclock']}", composer_data("composer-2.5")),
        # Blobs for rows the filter drops. If the adapter ever reads one of
        # these, it read a row it had already decided was not a session.
        (f"composerData:{ID['archived']}", composer_data("composer-2.5", pct=99.5)),
        (f"composerData:{ID['subagent']}", composer_data("composer-2.5", pct=98.5)),
        (f"composerData:{ID['draftflag']}", composer_data("composer-2.5", pct=97.5)),
        (f"composerData:{ID['windowless']}", composer_data("composer-2.5", pct=96.5)),
        ("composerData:empty-state-draft", composer_data("grok-4.5")),
    ]
    # The message payloads. Full prompt and model text, thousands of rows in a
    # real store, and outside the allowlist by name.
    for i in range(6):
        rows.append((f"bubbleId:{ID['happy']}:bubble-{i}",
                     json.dumps({"text": PROMPT, "type": 1,
                                 "tokenCount": {"inputTokens": 0, "outputTokens": 0}})))
    rows.append(("ofsContent:" + ID["happy"], json.dumps({"contents": PROMPT})))
    rows.append(("checkpointId:" + ID["happy"], json.dumps({"note": PROMPT})))
    return rows


def item_rows():
    """ItemTable. The adapter must never walk this table, so every row in it is
    something that would be a disclosure if it did."""
    return [
        ("cursorAuth/accessToken", CRED + "-access"),
        ("cursorAuth/refreshToken", CRED + "-refresh"),
        ("mcpOAuth.secret.example", CRED + "-mcp"),
        ("src.vs.platform.reactivestorage.browser.reactiveStorageServiceImpl.persistentStorage.applicationUser",
         json.dumps({"credit_dollars": ENTITLEMENT, "included_usage_dollars": ENTITLEMENT})),
        # The legacy JSON mirror of composerHeaders, and it is STALE: it names
        # three composers to the table's eleven, and every one of them is a row
        # the filter drops. An adapter that reads the mirror reports zero
        # sessions on a machine with six.
        ("composer.composerHeaders", json.dumps({"allComposers": [
            {"composerId": "empty-state-draft", "name": "stale mirror entry"},
            {"composerId": ID["windowless"], "name": "stale mirror entry"},
            {"composerId": ID["archived"], "name": "stale mirror entry"},
        ]})),
    ]


# ------------------------------------------------------------------ builders


def fresh(path):
    for suffix in ("", "-wal", "-shm"):
        p = pathlib.Path(str(path) + suffix)
        if p.exists():
            p.unlink()
    return path


def write_store(path, headers=True, wal_only=False):
    """Build a state.vscdb. With wal_only, every byte of content lands in the
    -wal and the main file stays one empty page — the shape §3.9 observed on
    every workspace-level store, and the one that makes a `.db`-only reader
    report an empty database."""
    fresh(path)
    con = sqlite3.connect(path)
    con.execute("PRAGMA page_size=4096")
    if wal_only:
        # Enable WAL on an empty database and switch off autocheckpoint BEFORE
        # anything is created: the schema itself then lives in the sidecar.
        con.execute("PRAGMA journal_mode=WAL")
        con.execute("PRAGMA wal_autocheckpoint=0")
    else:
        con.execute("PRAGMA journal_mode=DELETE")

    con.execute(SCHEMA_ITEM)
    con.execute(SCHEMA_KV)
    if headers:
        con.execute(SCHEMA_HEADERS)
        con.executemany(
            "INSERT INTO composerHeaders VALUES (?,?,?,?,?,?,?,?,?)", header_rows())
    con.executemany("INSERT INTO cursorDiskKV VALUES (?,?)", kv_rows())
    con.executemany("INSERT INTO ItemTable VALUES (?,?)", item_rows())
    con.commit()

    if not wal_only:
        con.close()
        return

    # Copy both files while the connection is open: closing checkpoints the
    # sidecar back into the database and destroys the fixture.
    staged = fresh(HERE / "_stage.vscdb")
    shutil.copyfile(path, staged)
    shutil.copyfile(str(path) + "-wal", str(staged) + "-wal")
    con.close()
    fresh(path)
    shutil.move(staged, path)
    shutil.move(str(staged) + "-wal", str(path) + "-wal")


def write_workspaces(root):
    """workspaceStorage/<id>/workspace.json. `ws-gone` is deliberately absent —
    Cursor prunes these, and a session whose mapping is gone still exists."""
    folders = {
        "ws-alpha": "file:///c%3A/src/code/example-app",
        # A path with a space, to exercise the percent decoder on the one
        # escape that actually shows up.
        "ws-beta": "file:///c%3A/src/code/notes%20api",
        # A window with no folder: valid JSON, nothing to name.
        "ws-empty": "",
    }
    for wsid, folder in folders.items():
        d = root / "workspaceStorage" / wsid
        d.mkdir(parents=True, exist_ok=True)
        payload = {} if folder == "" else {"folder": folder}
        (d / "workspace.json").write_text(
            json.dumps(payload, sort_keys=True), encoding="utf8")


def build(name, headers=True, wal_only=False, workspaces=True):
    root = HERE / name
    if root.exists():
        shutil.rmtree(root)
    (root / "globalStorage").mkdir(parents=True)
    write_store(root / "globalStorage" / "state.vscdb",
                headers=headers, wal_only=wal_only)
    if workspaces:
        write_workspaces(root)
    return root


if __name__ == "__main__":
    roots = [
        # The main tree: a checkpointed store, content in the .db.
        build("root"),
        # The trap: main file one empty page, everything in the sidecar.
        build("root-wal", wal_only=True),
        # A store this adapter does not recognize: no composerHeaders table.
        build("root-noheaders", headers=False),
    ]
    for root in roots:
        for p in sorted(root.rglob("*")):
            if p.is_file():
                print(f"{os.path.getsize(p):>9}  {p.relative_to(HERE)}")
