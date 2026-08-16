#!/usr/bin/env python3
"""Validate `telltale snapshot` output against docs/snapshot.schema.json.

This is the schema's first consumer. CI runs it in the `test` job against the
document the BUILT binary prints, and against the four golden documents in
`internal/snapshot/testdata/golden/`, so the published contract is checked
against what ships rather than against what a struct looks like.

Why a real validator and not hand-written checks: hand-written assertions are a
second statement of the contract, and two statements drift. A JSON Schema
validator gives the schema file itself the last word, so the schema cannot be
right about a document the gate passes.

Why Python and not Go: `go.mod` has zero direct dependencies outside the TUI
stack, and design.md records that stance repeatedly (the SQLite reader, the
zstd reader, the OTLP listener and the event emitter are all stdlib work that
refused a library). A Go schema module would put a test-only dependency in the
shipped module's graph. This runs beside the build instead, and the shipped
binary keeps its dependency list.

    python -m pip install jsonschema==4.23.0
    ./telltale.exe snapshot --compact | python tools/validate-snapshot.py -
    python tools/validate-snapshot.py internal/snapshot/testdata/golden/*.json

`--mutate <name>` breaks the document on purpose and then requires the
validator to REJECT it. A gate that has never failed is a gate nobody has
measured, so CI runs every mutation on the real output of every run.
"""

import argparse
import json
import pathlib
import sys

try:
    import jsonschema
except ImportError:  # pragma: no cover - the message is the whole value here
    sys.exit(
        "tools/validate-snapshot.py needs the jsonschema package:\n"
        "    python -m pip install jsonschema==4.23.0"
    )

REPO_ROOT = pathlib.Path(__file__).resolve().parent.parent
DEFAULT_SCHEMA = REPO_ROOT / "docs" / "snapshot.schema.json"


def mutate_drop_key(doc):
    """Delete an optional key from the first vendor block.

    This is the omitempty regression the document exists to prevent: a key that
    vanishes when its value is absent makes "no reading" and "this schema moved
    under me" the same observation. `required` is what catches it.
    """
    if not doc.get("vendors"):
        raise SystemExit("mutation drop-key needs at least one vendor block")
    del doc["vendors"][0]["cost_usd_total"]
    return "vendors[0] lost the key cost_usd_total"


def mutate_retype_null(doc):
    """Put a sentinel string where a nullable number belongs.

    A sentinel is the other way to collapse zero and absent: a reader that must
    special-case "n/a" has lost the type distinction the document is built on.
    """
    doc["fleet"]["context_pct_max"] = "n/a"
    return 'fleet.context_pct_max became the string "n/a"'


def mutate_bump_version(doc):
    """Raise schema_version without changing the schema.

    The contract number is pinned with `const`, so a bump that nobody wrote a
    schema for fails here rather than reaching a consumer that trusts the
    number.
    """
    doc["schema_version"] = doc.get("schema_version", 1) + 1
    return "schema_version rose past the version this schema describes"


MUTATIONS = {
    "drop-key": mutate_drop_key,
    "retype-null": mutate_retype_null,
    "bump-version": mutate_bump_version,
}


def load(path):
    if path == "-":
        return json.load(sys.stdin), "<stdin>"
    text = pathlib.Path(path).read_text(encoding="utf-8")
    return json.loads(text), path


def describe(error):
    """One line a reader can act on: where it failed and what it wanted."""
    where = "/".join(str(p) for p in error.absolute_path) or "<document root>"
    return f"  at {where}: {error.message}"


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("documents", nargs="+", help="JSON documents to validate, or - for stdin")
    ap.add_argument("--schema", default=str(DEFAULT_SCHEMA))
    ap.add_argument(
        "--mutate",
        choices=sorted(MUTATIONS),
        help="break the document this way first, and require the validator to reject it",
    )
    args = ap.parse_args()

    schema = json.loads(pathlib.Path(args.schema).read_text(encoding="utf-8"))
    # check_schema first: a schema with a typo in a keyword name validates
    # everything happily and gates nothing.
    jsonschema.Draft202012Validator.check_schema(schema)
    validator = jsonschema.Draft202012Validator(schema)

    failed = False
    for path in args.documents:
        doc, name = load(path)
        note = ""
        if args.mutate:
            note = f" [mutated: {MUTATIONS[args.mutate](doc)}]"

        errors = sorted(validator.iter_errors(doc), key=lambda e: list(e.absolute_path))
        if args.mutate:
            if errors:
                print(f"ok   {name} was rejected as it should be{note}")
                print(describe(errors[0]))
            else:
                failed = True
                print(f"FAIL {name} VALIDATED after being broken on purpose{note}")
                print("     the schema does not gate this mutation, so the gate is vacuous")
        else:
            if errors:
                failed = True
                print(f"FAIL {name} does not match {args.schema}")
                for e in errors:
                    print(describe(e))
            else:
                print(f"ok   {name}")

    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
