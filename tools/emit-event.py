"""Emit one hook event to the telltale event sink (design.md §7.21).

Stdlib-only, vendor-neutral: any process that can pipe JSON is an event
source. Wire it as a hook command:

  uv run tools/emit-event.py --source-app <repo-name> [--event-type <name>]

The hook payload arrives on stdin. `--source-app` is the ONE per-repo edit;
everything else has a default. `--event-type` names the hook when the payload
does not: Claude Code payloads carry `hook_event_name`, so their hooks can
omit the flag; a wrapper for a vendor with no such field passes it.

Hard rules, in priority order:
  1. Exit 0 on EVERY path. A hook that exits non-zero can colour the agent's
     turn with an error the user did not cause; the sink is never worth that.
  2. Never block the agent. A quarter-second connect probe asks "is the sink
     listening" before the POST; only the POST itself gets the 5 second
     budget, and there is no retry. A sink that is down costs the probe and
     one stderr line. The probe exists because "5 second timeout" did not
     bound the down path in practice: a refused connect never times out, and
     Winsock retries it internally for ~2 seconds per address family
     (measured 2026-08-15, Windows 11) — a plain POST to a down sink cost
     ~4.4s on every hook event, which is a blocked agent, not a bounded one.
  3. Send the payload verbatim. No summarization, no redaction here — scope
     control belongs to the sink (loopback only) and to what you wire.

The default URL names 127.0.0.1, never localhost: the sink binds 127.0.0.1
only, and on Windows `localhost` resolves ::1 first — so the old default
paid the full Winsock retry cycle against ::1 before reaching the sink,
even when the sink was up.
"""

import argparse
import json
import socket
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

DEFAULT_URL = "http://127.0.0.1:4519/events"
TIMEOUT_SECONDS = 5
PROBE_TIMEOUT_SECONDS = 0.25

# Fields promoted from the payload's top level to the event's top level, so a
# reader can filter without parsing every payload. Promotion is copy, not
# move: the payload stays verbatim.
PROMOTED = (
    "tool_name",
    "tool_use_id",
    "error",
    "agent_id",
    "agent_type",
    "stop_hook_active",
)


def build_event(payload, source_app, event_type):
    """Shape one sink event from a hook payload.

    The session id and the event type come from the payload when it carries
    them (Claude Code names them session_id / hook_event_name); flags and
    fallbacks cover vendors that do not.
    """
    event = {
        "source_app": source_app,
        "session_id": str(payload.get("session_id") or "unknown"),
        "hook_event_type": str(
            event_type or payload.get("hook_event_name") or "Unknown"
        ),
        "payload": payload,
        "timestamp": int(time.time() * 1000),
    }
    for key in PROMOTED:
        value = payload.get(key)
        if value is None:
            continue
        if key == "stop_hook_active":
            if isinstance(value, bool):
                event[key] = value
        else:
            event[key] = value if isinstance(value, str) else json.dumps(value)
    return event


def sink_is_listening(url):
    """One bounded connect against the sink's address. True means POST now.

    A False answer drops the event — rule 2 ranks the agent's time above the
    event. An address the URL parser cannot name answers True, so the POST
    path (and its own error handling) stays the single authority on failure.
    """
    try:
        parts = urllib.parse.urlsplit(url)
        host = parts.hostname
        port = parts.port or (443 if parts.scheme == "https" else 80)
    except ValueError:
        return True
    if not host:
        return True
    try:
        with socket.create_connection((host, port), timeout=PROBE_TIMEOUT_SECONDS):
            return True
    except OSError:
        return False


def main():
    parser = argparse.ArgumentParser(
        description="POST one hook event from stdin to the telltale event sink"
    )
    parser.add_argument(
        "--source-app",
        required=True,
        help="which repo or wrapper this event comes from — the one per-repo edit",
    )
    parser.add_argument(
        "--event-type",
        default=None,
        help="hook event name; default: the payload's hook_event_name, else Unknown",
    )
    parser.add_argument(
        "--server-url",
        default=DEFAULT_URL,
        help=f"sink endpoint (default {DEFAULT_URL})",
    )
    args = parser.parse_args()

    raw = sys.stdin.read()
    try:
        payload = json.loads(raw) if raw.strip() else {}
    except json.JSONDecodeError as err:
        # A payload the emitter cannot parse still gets recorded: the sink is
        # an observability surface, and a wiring bug is exactly what it is
        # for. The broken text travels inside a well-formed payload.
        payload = {"unparsed_stdin": raw, "parse_error": str(err)}
    if not isinstance(payload, dict):
        payload = {"non_object_payload": payload}

    if not sink_is_listening(args.server_url):
        # Fail open, fast: the sink being down must never block the agent.
        print("emit-event: sink not listening, event dropped", file=sys.stderr)
        return

    event = build_event(payload, args.source_app, args.event_type)
    body = json.dumps(event).encode("utf-8")
    request = urllib.request.Request(
        args.server_url,
        data=body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=TIMEOUT_SECONDS) as response:
            response.read()
    except (urllib.error.URLError, OSError, ValueError) as err:
        # Fail open: the sink being down must never block the agent.
        print(f"emit-event: sink unreachable, event dropped: {err}", file=sys.stderr)


if __name__ == "__main__":
    try:
        main()
    except SystemExit:
        # argparse exits 2 on a bad flag; a hook must still exit 0. The
        # message argparse already printed to stderr is the diagnostic.
        sys.exit(0)
    except Exception as err:  # noqa: BLE001 — rule 1: exit 0 on every path
        print(f"emit-event: {err}", file=sys.stderr)
        sys.exit(0)
    sys.exit(0)
