"""Re-scan the live Codex corpus for the design.md §3.4 passive-tail observables.

Dev-only, stdlib-only. Run with:  python tools/scan-passive-tail.py
(or `uv run python tools/scan-passive-tail.py`)

The three §3.4 "still owed" items this watches for:
  1. Mid-stream nulls — a token_count whose `info` or `rate_limits` is null AFTER
     a populated one earlier in the same rollout (settles "cleared vs unchanged").
  2. API-key login capture — a native session whose token_counts carry no
     rate_limits at all; also reports plan_type values and whether a paid plan
     ever populates `secondary`.
  3. The 7-day .zst compression pass — reports any *.zst under sessions/.

Imported external-agent transcripts (affirmative marker: a task_started turn_id
beginning `external-import-turn`) are excluded, matching the adapter's
ErrImportedTranscript filter. archived_sessions/ is not walked, matching the
adapter.
"""
import json
from pathlib import Path

ROOT = Path.home() / ".codex" / "sessions"

reports = []
plan_types = {}
populated_rl = 0
secondary_seen = []
midstream_nulls = []
no_ratelimit_sessions = []
zst_files = sorted(ROOT.rglob("*.zst"))

for f in sorted(ROOT.rglob("*.jsonl")):
    meta = None
    imported = False
    tc = []  # (line_no, info_is_null, rl_is_null, rl_obj)
    with f.open(encoding="utf-8", errors="replace") as fh:
        for i, line in enumerate(fh, 1):
            line = line.strip()
            if not line:
                continue
            try:
                rec = json.loads(line)
            except json.JSONDecodeError:
                continue
            typ = rec.get("type")
            payload = rec.get("payload") or {}
            if typ == "session_meta":
                meta = payload
            elif typ == "event_msg" and payload.get("type") == "token_count":
                info = payload.get("info")
                rl = payload.get("rate_limits")
                tc.append((i, info is None, rl is None, rl))
            elif typ == "task_started" or (
                typ == "event_msg" and payload.get("type") == "task_started"
            ):
                tid = payload.get("turn_id") or ""
                if str(tid).startswith("external-import-turn"):
                    imported = True
    if imported:
        continue
    name = f.name
    origin = (meta or {}).get("originator"), (meta or {}).get("source")
    any_rl = False
    for (ln, inull, rlnull, rl) in tc:
        if rl is not None:
            any_rl = True
            populated_rl += 1
            pt = rl.get("plan_type")
            plan_types[pt] = plan_types.get(pt, 0) + 1
            sec = rl.get("secondary")
            if sec is not None:
                secondary_seen.append((name, ln, sec))
    if tc and not any_rl:
        no_ratelimit_sessions.append((name, origin, len(tc)))
    seen_info = seen_rl = False
    for (ln, inull, rlnull, rl) in tc:
        if seen_info and inull:
            midstream_nulls.append((name, ln, "info"))
        if seen_rl and rlnull:
            midstream_nulls.append((name, ln, "rate_limits"))
        if not inull:
            seen_info = True
        if not rlnull:
            seen_rl = True
    reports.append(
        (name, origin, len(tc), sum(1 for t in tc if t[1]), sum(1 for t in tc if t[2]))
    )

print(f"native rollouts scanned: {len(reports)}")
for name, origin, n, ni, nr in reports:
    print(f"  {name}  origin={origin}  token_counts={n}  info_nulls={ni}  rl_nulls={nr}")
print(f"\n.zst files under sessions/: {len(zst_files)}")
for z in zst_files[:10]:
    print(f"  {z.relative_to(ROOT)}")
print(f"\nplan_type counts across {populated_rl} populated rate_limits: {plan_types}")
print(f"\nsecondary non-null sightings: {len(secondary_seen)}")
for s in secondary_seen[:10]:
    print(f"  {s[0]} line {s[1]}: {json.dumps(s[2])[:300]}")
print(f"\nsessions with token_counts but zero rate_limits (API-key signature): {no_ratelimit_sessions}")
print(f"\nMID-STREAM NULLS: {len(midstream_nulls)}")
for m in midstream_nulls[:40]:
    print(f"  {m[0]} line {m[1]}: {m[2]} null after populated")
