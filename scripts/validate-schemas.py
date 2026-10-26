#!/usr/bin/env python3
"""Validate the lugarn-comms protocol schemas, their examples, and the docs' enum table.

Checks, in order:
  1. Every schema/*.schema.json is a valid JSON Schema draft 2020-12 document.
  2. Every file under schema/examples/ validates against the definition its name selects:
       team-spec/<name>.json            team-spec.v1  (root)
       app-ops/<op>[--x].request.json   app-ops.v1    #/$defs/Request   (op must equal <op>)
       app-ops/<op>[--x].response.json  app-ops.v1    #/$defs/Response  (op must equal <op>)
       app-ops/<name>.ndjson            app-ops.v1    #/$defs/WatchLine (each line)
       mcp/<tool>[--x].input.json       mcp-tools.v1  #/$defs/<Tool>Input
       mcp/<tool>[--x].output.json      mcp-tools.v1  #/$defs/<Tool>Output
       mcp/<tool>[--x].error.json       mcp-tools.v1  #/$defs/ToolError
  3. A few documents that must be rejected are rejected (conditional rules actually bite).
  4. The enum table in docs/protocol.md ("## Enums") lists exactly the enums the schemas
     define, with exactly the same values.

Needs `jsonschema` >= 4.18 (uses `referencing`). Exit status 0 when everything passes.
Usage: scripts/validate-schemas.py [repo-root]
"""
from __future__ import annotations

import json
import re
import sys
from pathlib import Path

from jsonschema import Draft202012Validator
from referencing import Registry, Resource

ROOT = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).resolve().parent.parent
SCHEMA_DIR = ROOT / "schema"
EXAMPLES = SCHEMA_DIR / "examples"
DOC = ROOT / "docs" / "protocol.md"

failures: list[str] = []


def fail(msg: str) -> None:
    failures.append(msg)
    print(f"FAIL  {msg}")


def ok(msg: str) -> None:
    print(f"ok    {msg}")


# 1. Schemas -------------------------------------------------------------------------------
schemas: dict[str, dict] = {}
for path in sorted(SCHEMA_DIR.glob("*.schema.json")):
    doc = json.loads(path.read_text())
    try:
        Draft202012Validator.check_schema(doc)
    except Exception as exc:  # SchemaError
        fail(f"{path.name}: not a valid draft 2020-12 schema: {exc}")
        continue
    if doc.get("$schema") != "https://json-schema.org/draft/2020-12/schema":
        fail(f"{path.name}: $schema is not draft 2020-12")
    schemas[path.name] = doc
    ok(f"{path.name}: valid draft 2020-12")

registry = Registry().with_resources(
    (doc["$id"], Resource.from_contents(doc)) for doc in schemas.values()
)
ids = {name: doc["$id"] for name, doc in schemas.items()}


def validator_for(schema_file: str, pointer: str = "") -> Draft202012Validator:
    ref = ids[schema_file] + (f"#{pointer}" if pointer else "")
    return Draft202012Validator({"$ref": ref}, registry=registry)


def check(instance, v: Draft202012Validator, label: str) -> bool:
    errors = sorted(v.iter_errors(instance), key=lambda e: list(e.absolute_path))
    if errors:
        best = errors[0]
        where = "/".join(str(p) for p in best.absolute_path) or "(root)"
        fail(f"{label}: {where}: {best.message[:300]}")
        return False
    return True


# 2. Examples ------------------------------------------------------------------------------
def tool_def(tool: str, suffix: str) -> str:
    return f"/$defs/{tool[:1].upper()}{tool[1:]}{suffix}"


count = 0
for path in sorted(EXAMPLES.rglob("*")):
    if not path.is_file():
        continue
    rel = path.relative_to(EXAMPLES).as_posix()
    group, name = rel.split("/", 1)
    stem = name.split(".", 1)[0]
    base = stem.split("--", 1)[0]
    count += 1
    if group == "team-spec" and name.endswith(".json"):
        if check(json.loads(path.read_text()), validator_for("team-spec.v1.schema.json"), rel):
            ok(rel)
    elif group == "app-ops" and name.endswith(".ndjson"):
        v = validator_for("app-ops.v1.schema.json", "/$defs/WatchLine")
        lines = [l for l in path.read_text().splitlines() if l.strip()]
        good = all(check(json.loads(l), v, f"{rel}:{i + 1}") for i, l in enumerate(lines))
        if good:
            ok(f"{rel} ({len(lines)} lines)")
    elif group == "app-ops" and name.endswith((".request.json", ".response.json")):
        kind = "Request" if name.endswith(".request.json") else "Response"
        doc = json.loads(path.read_text())
        good = check(doc, validator_for("app-ops.v1.schema.json", f"/$defs/{kind}"), rel)
        if doc.get("op") != base:
            fail(f"{rel}: op is {doc.get('op')!r} but the file name says {base!r}")
            good = False
        if good:
            ok(rel)
    elif group == "mcp" and name.endswith((".input.json", ".output.json", ".error.json")):
        tools = schemas["mcp-tools.v1.schema.json"]["$defs"]["ToolName"]["enum"]
        if base not in tools:
            fail(f"{rel}: {base!r} is not a tool")
            continue
        if name.endswith(".error.json"):
            pointer = "/$defs/ToolError"
        else:
            pointer = tool_def(base, "Input" if name.endswith(".input.json") else "Output")
        if check(json.loads(path.read_text()), validator_for("mcp-tools.v1.schema.json", pointer), rel):
            ok(rel)
    else:
        fail(f"{rel}: no schema matches this file name")

# Every tool has an input and an output example; every op has a request example.
tools = schemas["mcp-tools.v1.schema.json"]["$defs"]["ToolName"]["enum"]
for tool in tools:
    for part in ("input", "output"):
        if not (EXAMPLES / "mcp" / f"{tool}.{part}.json").exists():
            fail(f"mcp/{tool}.{part}.json is missing")

# 3. Documents that must be rejected --------------------------------------------------------
spec_ok = json.loads((EXAMPLES / "team-spec" / "writer-reviewer.json").read_text())


def must_reject(instance, v: Draft202012Validator, label: str) -> None:
    if v.is_valid(instance):
        fail(f"negative case accepted: {label}")
    else:
        ok(f"rejects: {label}")


team_v = validator_for("team-spec.v1.schema.json")
req_v = validator_for("app-ops.v1.schema.json", "/$defs/Request")
mcp_send_v = validator_for("mcp-tools.v1.schema.json", "/$defs/SendInput")

bad = json.loads(json.dumps(spec_ok))
bad["grid"][1]["can"] = ["wake"]
must_reject(bad, team_v, "grid edge with wake but not message")
bad = json.loads(json.dumps(spec_ok))
bad["members"][0]["name"] = "human"
must_reject(bad, team_v, "member named human")
bad = json.loads(json.dumps(spec_ok))
bad["members"][0]["spawn"] = {"enabled": True}
must_reject(bad, team_v, "spawn enabled without a worker brief")
bad = json.loads(json.dumps(spec_ok))
bad["members"][0]["host"] = "ufo"
must_reject(bad, team_v, "member host set in v1")
bad = json.loads(json.dumps(spec_ok))
bad["limits"]["per_recipient"] = 2
must_reject(bad, team_v, "per_recipient other than 1")
must_reject({"protocol": 1, "op": "send", "team": "t1", "to": "m1", "all": True, "text": "x"},
            req_v, "send with both to and all")
must_reject({"protocol": 1, "op": "timeline", "team": "t1", "before": "h.1", "after": "h.2"},
            req_v, "timeline with both before and after")
must_reject({"protocol": 1, "op": "continue-budget", "team": "t1", "writer": "w"},
            req_v, "continue-budget writer without kind rounds and reviewer")
must_reject({"protocol": 1, "op": "status", "team": "t1", "verbose": True},
            req_v, "unknown request field")
must_reject({"to": "all", "text": "hi"}, mcp_send_v, "agent send to all")
must_reject({"to": "human", "text": "x" * 8001}, mcp_send_v, "agent text to human over 8000 code points")
must_reject({"protocol": 1, "op": "hold", "team": "t1", "member": "m1", "ttl_seconds": 30, "release": True},
            req_v, "hold with both ttl_seconds and release")
must_reject({"protocol": 1, "op": "hold", "team": "t1", "member": "m1", "ttl_seconds": 600},
            req_v, "hold ttl over 120 s")
must_reject({"protocol": 1, "op": "remove-worktrees", "team": "t1", "paths": []},
            req_v, "remove-worktrees with no paths")

# 4. Enum table in docs/protocol.md ---------------------------------------------------------
schema_enums: dict[str, list[str]] = {}
for fname, doc in schemas.items():
    for name, d in doc.get("$defs", {}).items():
        if isinstance(d, dict) and "enum" in d:
            if name in schema_enums and schema_enums[name] != d["enum"]:
                fail(f"enum {name} is defined twice with different values")
            schema_enums[name] = d["enum"]

if not DOC.exists():
    fail(f"{DOC.relative_to(ROOT)} is missing")
else:
    text = DOC.read_text()
    m = re.search(r"^## Enums\n(.*?)(?=^## )", text, re.S | re.M)
    if not m:
        fail("docs/protocol.md has no '## Enums' section")
    else:
        doc_enums: dict[str, list[str]] = {}
        for line in m.group(1).splitlines():
            row = re.match(r"^\|\s*`([A-Za-z]+)`\s*\|([^|]*)\|", line)
            if row:
                doc_enums[row.group(1)] = re.findall(r"`([^`]+)`", row.group(2))
        for name in sorted(set(schema_enums) | set(doc_enums)):
            if name not in doc_enums:
                fail(f"enum {name} is in the schemas but not in docs/protocol.md's enum table")
            elif name not in schema_enums:
                fail(f"enum {name} is in docs/protocol.md's enum table but no schema defines it")
            elif doc_enums[name] != schema_enums[name]:
                fail(f"enum {name}: docs say {doc_enums[name]}, schemas say {schema_enums[name]}")
        if not failures or all("enum" not in f for f in failures):
            ok(f"docs/protocol.md enum table matches the schemas ({len(schema_enums)} enums)")

print()
print(f"{len(schemas)} schemas, {count} example files, {len(failures)} failure(s)")
sys.exit(1 if failures else 0)
