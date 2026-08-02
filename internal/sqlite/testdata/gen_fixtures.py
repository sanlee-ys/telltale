#!/usr/bin/env python3
"""Generate the byte-level fixtures for internal/sqlite.

Run from this directory:

    uv run python gen_fixtures.py
    (if that errors about a missing .env: env -u UV_ENV_FILE uv run python gen_fixtures.py)

…then re-run the package tests:

    go -C ../../.. test ./internal/sqlite/

Stdlib only (sqlite3 ships with CPython), so there is no environment to set up
and no dependency to pin. Everything written here is SYNTHETIC — invented ids,
invented text, invented blobs. This repo is public and no real data enters
testdata/, ever.

The three fixtures exist for three different reader paths:

  plain.db     one page, small rows, every storage class — the record decoder.
  overflow.db  a 25 KiB blob against a 4 KiB page — the overflow-chain walk,
               which is the COMMON path for the vendor data this reader was
               written for, not an exotic one.
  wal.db(+-wal)  a row whose committed value lives only in the sidecar — a
               reader that skips the WAL reports the stale value and the test
               says so by name.
"""

import os
import pathlib
import shutil
import sqlite3

HERE = pathlib.Path(__file__).resolve().parent


def fresh(name):
    for suffix in ("", "-wal", "-shm"):
        p = HERE / (name + suffix)
        if p.exists():
            p.unlink()
    return HERE / name


def gen_plain():
    path = fresh("plain.db")
    con = sqlite3.connect(path)
    con.execute("PRAGMA page_size=4096")
    con.execute("PRAGMA journal_mode=DELETE")
    con.execute(
        "CREATE TABLE `kv` (`idx` integer, `label` text, `payload` blob, "
        "`ratio` real, `flag` integer, PRIMARY KEY (`idx`))"
    )
    rows = [
        (1, "alpha", b"\x00\x01\x02", 0.5, 0),
        (2, "beta", b"", 1.5, 1),
        (3, "gamma", None, -2.25, 65537),
        (4, None, b"\xff" * 12, 0.0, -1),
    ]
    con.executemany("INSERT INTO `kv` VALUES (?,?,?,?,?)", rows)
    con.execute("CREATE TABLE `empty_table` (`idx` integer, `data` blob, PRIMARY KEY (`idx`))")
    con.commit()
    con.close()
    print("wrote", path.name)


def gen_overflow():
    path = fresh("overflow.db")
    con = sqlite3.connect(path)
    con.execute("PRAGMA page_size=4096")
    con.execute("PRAGMA journal_mode=DELETE")
    con.execute("CREATE TABLE `blobs` (`idx` integer, `data` blob, PRIMARY KEY (`idx`))")
    # Deterministic, non-repeating content: a byte pattern that would be
    # indistinguishable from zero-fill if a chunk were dropped or duplicated.
    big = bytes((i * 37 + (i >> 8) * 11) % 251 for i in range(25_000))
    mid = bytes((i * 13) % 241 for i in range(4_500))
    con.execute("INSERT INTO `blobs` VALUES (?,?)", (1, big))
    con.execute("INSERT INTO `blobs` VALUES (?,?)", (2, mid))
    con.execute("INSERT INTO `blobs` VALUES (?,?)", (3, b"short"))
    # Enough rows to force an interior page, so the walk is exercised too.
    for i in range(4, 40):
        con.execute("INSERT INTO `blobs` VALUES (?,?)", (i, bytes([i]) * 900))
    con.commit()
    con.close()
    print("wrote", path.name, "with a 25000-byte blob")


def gen_wal():
    live = HERE / "_wal_build.db"
    for suffix in ("", "-wal", "-shm"):
        p = pathlib.Path(str(live) + suffix)
        if p.exists():
            p.unlink()

    con = sqlite3.connect(live)
    con.execute("PRAGMA page_size=4096")
    con.execute("PRAGMA journal_mode=WAL")
    con.execute("PRAGMA wal_autocheckpoint=0")
    con.execute("CREATE TABLE `kv` (`idx` integer, `label` text, PRIMARY KEY (`idx`))")
    con.execute("INSERT INTO `kv` VALUES (1, 'base-value')")
    con.commit()
    # Force the base file to hold the pre-update state, then leave the update
    # in the sidecar only.
    con.execute("PRAGMA wal_checkpoint(FULL)")
    con.execute("UPDATE `kv` SET `label`='wal-value' WHERE `idx`=1")
    con.execute("INSERT INTO `kv` VALUES (2, 'wal-only-row')")
    con.commit()

    # Copy both files while the connection is still open: closing it would
    # checkpoint the sidecar back into the database and destroy the fixture.
    fresh("wal.db")
    shutil.copyfile(live, HERE / "wal.db")
    shutil.copyfile(str(live) + "-wal", HERE / "wal.db-wal")
    con.close()

    for suffix in ("", "-wal", "-shm"):
        p = pathlib.Path(str(live) + suffix)
        if p.exists():
            p.unlink()

    size = os.path.getsize(HERE / "wal.db-wal")
    print("wrote wal.db + wal.db-wal", size, "bytes of sidecar")


if __name__ == "__main__":
    gen_plain()
    gen_overflow()
    gen_wal()
