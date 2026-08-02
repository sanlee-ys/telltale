#!/usr/bin/env python3
"""Generate the Antigravity CLI adapter fixtures.

Run from this directory:

    uv run python gen_fixtures.py
    (if that errors about a missing .env: env -u UV_ENV_FILE uv run python gen_fixtures.py)

…then re-run the package tests:

    go -C ../../../.. test ./internal/adapter/antigravity/

Stdlib only — sqlite3 ships with CPython and the protobuf wire format is
forty lines of varint encoding, so there is nothing to install and nothing to
pin.

EVERYTHING HERE IS SYNTHETIC. Invented conversation ids, invented workspace
paths, invented token numbers, invented prompt text. This repo is public and
the real corpus carries full prompt content, file contents and the account
email (docs/design.md §3.8) — no real database and no real transcript enters
testdata/, ever. The prompt text below is deliberately a shouty marker string
so a test can assert it never reaches a rendered field.

The shapes reproduced here are the ones §3.8 recorded from agy 1.1.9:

  gen_metadata.data              protobuf, one #1 generation per row
  #1.#19 / #1.#21                model id / display name
  #1.#4.{#2,#3,#9,#10,#11}       in / out / thinking / answer / response id
  top-level #4                   a UUID that is CONSTANT per conversation —
                                 the trap the adapter must not dedup on
  trajectory_metadata_blob.data  workspace URI at #1.#1 and again flat at #7
"""

import os
import pathlib
import shutil
import sqlite3

HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE / "root"

# ---------------------------------------------------------------- protobuf


def tag(num, wire):
    return varint(num << 3 | wire)


def varint(v):
    out = bytearray()
    while True:
        b = v & 0x7F
        v >>= 7
        if v:
            out.append(b | 0x80)
        else:
            out.append(b)
            return bytes(out)


def pb_varint(num, v):
    return tag(num, 0) + varint(v)


def pb_bytes(num, data):
    if isinstance(data, str):
        data = data.encode("utf8")
    return tag(num, 2) + varint(len(data)) + data


def tokens(in_, out, thinking, answer, resp_id, cache_read=None):
    b = pb_varint(2, in_) + pb_varint(3, out)
    if cache_read is not None:
        b += pb_varint(5, cache_read)
    b += pb_varint(6, 24) + pb_varint(9, thinking) + pb_varint(10, answer)
    b += pb_bytes(11, resp_id)
    return b


def generation(model_id, model_name, tok, filler=0):
    b = pb_varint(3, 1071) + pb_bytes(4, tok)
    if filler:
        # The real blobs carry the whole request/response tree at #8. Size is
        # what forces the overflow-page path, so the fixture carries it too.
        chunk = bytes((i * 37) % 251 for i in range(1000))
        for _ in range(filler):
            b += pb_bytes(8, chunk)
    b += pb_bytes(19, model_id) + pb_bytes(20, "x" * 20) + pb_bytes(21, model_name)
    return b


def gen_blob(gens, conversation_uuid):
    """One gen_metadata.data blob. The top-level #4 is the per-conversation
    UUID an adapter must NOT use as a dedup key."""
    b = pb_bytes(2, b"\x01") + pb_bytes(3, b"\x08\x2f")
    b += pb_bytes(4, conversation_uuid)
    for g in gens:
        b += pb_bytes(1, g)
    return b


def trajectory_blob(workspace_uri, seconds=1785705961):
    b = b""
    if workspace_uri:
        b += pb_bytes(1, pb_bytes(1, workspace_uri) + pb_bytes(3, b""))
    b += pb_bytes(2, pb_varint(1, seconds) + pb_varint(2, 766662700))
    if workspace_uri:
        b += pb_bytes(7, workspace_uri)
    return b


# ---------------------------------------------------------------- sqlite

SCHEMA = [
    "CREATE TABLE `trajectory_meta` (`trajectory_id` text,`cascade_id` text,"
    "`trajectory_type` integer,`source` integer,PRIMARY KEY (`trajectory_id`))",
    "CREATE TABLE `steps` (`idx` integer,`step_type` integer NOT NULL DEFAULT 0,"
    "`status` integer NOT NULL DEFAULT 0,`has_subtrajectory` numeric NOT NULL DEFAULT false,"
    "`metadata` blob,`step_payload` blob,`step_format` integer NOT NULL DEFAULT 0,"
    "PRIMARY KEY (`idx`))",
    "CREATE TABLE `gen_metadata` (`idx` integer,`data` blob,`size` integer NOT NULL DEFAULT 0,"
    "PRIMARY KEY (`idx`))",
    "CREATE TABLE `parent_references` (`idx` integer,`data` blob,PRIMARY KEY (`idx`))",
    "CREATE TABLE `trajectory_metadata_blob` (`id` text DEFAULT \"main\",`data` blob,"
    "PRIMARY KEY (`id`))",
]


def build_db(path, conv_id, gen_blobs, workspace_uri, wal=False):
    for suffix in ("", "-wal", "-shm"):
        p = pathlib.Path(str(path) + suffix)
        if p.exists():
            p.unlink()

    con = sqlite3.connect(path)
    con.execute("PRAGMA page_size=4096")
    con.execute("PRAGMA journal_mode=WAL" if wal else "PRAGMA journal_mode=DELETE")
    if wal:
        con.execute("PRAGMA wal_autocheckpoint=0")
    for sql in SCHEMA:
        con.execute(sql)
    con.execute(
        "INSERT INTO `trajectory_meta` VALUES (?,?,?,?)",
        ("00000000-tttt-4uuu-8vvv-000000000001".replace("t", "1").replace("u", "2").replace("v", "3"),
         conv_id, 4, 17),
    )
    for i, blob in enumerate(gen_blobs):
        con.execute("INSERT INTO `gen_metadata` VALUES (?,?,?)", (i, blob, len(blob)))
    tb = trajectory_blob(workspace_uri)
    con.execute("INSERT INTO `trajectory_metadata_blob` VALUES (?,?)", ("main", tb))
    for i in range(4):
        con.execute(
            "INSERT INTO `steps` VALUES (?,?,?,?,?,?,?)",
            (i, 14 + i, 3, 0, b"", b"\x08\x01", 0),
        )
    con.commit()
    return con


def transcript(conv_id, stamps):
    """Write the vendor's transcript.jsonl. `content` and `thinking` carry a
    marker string: the adapter must never surface either, and a test asserts
    the marker appears in no rendered field."""
    d = ROOT / "brain" / conv_id / ".system_generated" / "logs"
    d.mkdir(parents=True, exist_ok=True)
    marker = "SYNTHETIC-PROMPT-TEXT-MUST-NEVER-RENDER"
    lines = []
    for i, ts in enumerate(stamps):
        rec = {
            "step_index": i,
            "source": "USER_EXPLICIT" if i == 0 else "MODEL",
            "type": "USER_INPUT" if i == 0 else "MODEL_RESPONSE",
            "status": "DONE",
            "created_at": ts,
        }
        if i == 0:
            rec["content"] = marker + " ask about the widget"
        else:
            rec["thinking"] = marker + " reasoning here"
            rec["tool_calls"] = [{"name": "read_file", "exit_code": 0}]
        lines.append(rec)
    import json

    (d / "transcript.jsonl").write_text(
        "".join(json.dumps(r, sort_keys=True) + "\n" for r in lines), encoding="utf8"
    )
    # The undocumented untruncated sibling exists in the real tree; the adapter
    # must read transcript.jsonl and ignore this one.
    (d / "transcript_full.jsonl").write_text(
        json.dumps({"step_index": 99, "created_at": "2030-01-01T00:00:00Z"}) + "\n",
        encoding="utf8",
    )


# ---------------------------------------------------------------- fixtures

CONV = {
    "happy": "00000000-dddd-4eee-8fff-000000000001",
    "wal": "00000000-dddd-4eee-8fff-000000000002",
    "broken": "00000000-dddd-4eee-8fff-000000000003",
    "noworkspace": "00000000-dddd-4eee-8fff-000000000004",
    "zero": "00000000-dddd-4eee-8fff-000000000005",
    "notranscript": "00000000-dddd-4eee-8fff-000000000006",
}

CONST_UUID = "cafe0000-0000-4000-8000-00000000feed"


def conv_path(conv_id):
    return ROOT / "conversations" / (conv_id + ".db")


def gen_happy():
    """Two generations, both self-consistent, one of them large enough that its
    record spans overflow pages (25 KiB against a 4 KiB page)."""
    cid = CONV["happy"]
    g1 = generation(
        "gemini-3.6-flash", "Gemini 3.6 Flash (High)",
        tokens(18099, 30, 29, 1, "resp-0000000000000001"),
    )
    g2 = generation(
        "gemini-3.6-flash", "Gemini 3.6 Flash (High)",
        tokens(22617, 350, 309, 41, "resp-0000000000000002", cache_read=20548),
        filler=25,
    )
    con = build_db(conv_path(cid), cid, [gen_blob([g1], CONST_UUID), gen_blob([g2], CONST_UUID)],
                   "file:///C:/src/code/example-app")
    con.close()
    transcript(cid, ["2026-08-01T11:58:00Z", "2026-08-01T11:59:30Z", "2026-08-01T11:59:48Z"])


def gen_wal():
    """The committed model lives ONLY in the sidecar. A reader that skips the
    WAL reports the old one."""
    cid = CONV["wal"]
    old = gen_blob([generation("gemini-3.6-flash", "Gemini 3.6 Flash (High)",
                               tokens(1000, 10, 6, 4, "resp-0000000000000010"))], CONST_UUID)
    new = gen_blob([generation("gemini-3.6-pro", "Gemini 3.6 Pro (High)",
                               tokens(2000, 20, 12, 8, "resp-0000000000000011"))], CONST_UUID)

    path = conv_path(cid)
    con = build_db(path, cid, [old], "file:///C:/src/code/example-app", wal=True)
    con.execute("PRAGMA wal_checkpoint(FULL)")
    con.execute("UPDATE `gen_metadata` SET `data`=?, `size`=? WHERE `idx`=0", (new, len(new)))
    con.commit()

    # Copy both files while the connection is open: closing checkpoints the
    # sidecar back into the database and destroys the fixture.
    staged = HERE / "_wal_stage.db"
    for suffix in ("", "-wal", "-shm"):
        p = pathlib.Path(str(staged) + suffix)
        if p.exists():
            p.unlink()
    shutil.copyfile(path, staged)
    shutil.copyfile(str(path) + "-wal", str(staged) + "-wal")
    con.close()
    for suffix in ("", "-wal", "-shm"):
        p = pathlib.Path(str(path) + suffix)
        if p.exists():
            p.unlink()
    shutil.move(staged, path)
    shutil.move(str(staged) + "-wal", str(path) + "-wal")

    transcript(cid, ["2026-08-01T11:50:00Z", "2026-08-01T11:55:00Z"])


def gen_broken():
    """thinking + answer != output. The self-check fails and the tokens must
    not render; the model and the row still do."""
    cid = CONV["broken"]
    g = generation("gemini-3.6-flash", "Gemini 3.6 Flash (High)",
                   tokens(5000, 400, 100, 100, "resp-0000000000000020"))
    con = build_db(conv_path(cid), cid, [gen_blob([g], CONST_UUID)],
                   "file:///C:/src/code/example-app")
    con.close()
    transcript(cid, ["2026-08-01T11:30:00Z", "2026-08-01T11:45:00Z"])


def gen_noworkspace():
    """A conversation started outside any workspace: the trajectory blob has no
    URI at all. Absence, not degradation."""
    cid = CONV["noworkspace"]
    g = generation("gemini-3.6-flash", "Gemini 3.6 Flash (High)",
                   tokens(700, 8, 5, 3, "resp-0000000000000030"))
    con = build_db(conv_path(cid), cid, [gen_blob([g], CONST_UUID)], None)
    con.close()
    transcript(cid, ["2026-08-01T10:00:00Z"])


def gen_zero():
    """Every count zero. The invariant holds (0 + 0 == 0) and zero is a
    MEASUREMENT: it must render as 0, never as absence."""
    cid = CONV["zero"]
    g = generation("gemini-3.6-flash", "Gemini 3.6 Flash (High)",
                   tokens(0, 0, 0, 0, "resp-0000000000000040"))
    con = build_db(conv_path(cid), cid, [gen_blob([g], CONST_UUID)],
                   "file:///C:/src/code/example-app")
    con.close()
    transcript(cid, ["2026-08-01T09:00:00Z"])


def gen_notranscript():
    """A database with no transcript: discovered, then dropped by Read."""
    cid = CONV["notranscript"]
    g = generation("gemini-3.6-flash", "Gemini 3.6 Flash (High)",
                   tokens(10, 2, 1, 1, "resp-0000000000000050"))
    con = build_db(conv_path(cid), cid, [gen_blob([g], CONST_UUID)],
                   "file:///C:/src/code/example-app")
    con.close()


if __name__ == "__main__":
    if ROOT.exists():
        shutil.rmtree(ROOT)
    (ROOT / "conversations").mkdir(parents=True)
    gen_happy()
    gen_wal()
    gen_broken()
    gen_noworkspace()
    gen_zero()
    gen_notranscript()
    # The stale index the adapter must never consult: one row for six
    # conversations, exactly as observed.
    idx = sqlite3.connect(ROOT / "conversation_summaries.db")
    idx.execute("CREATE TABLE `summaries` (`id` text, `title` text)")
    idx.execute("INSERT INTO `summaries` VALUES (?,?)", (CONV["happy"], "stale index entry"))
    idx.commit()
    idx.close()

    for p in sorted(ROOT.rglob("*")):
        if p.is_file():
            print(f"{os.path.getsize(p):>9}  {p.relative_to(HERE)}")
