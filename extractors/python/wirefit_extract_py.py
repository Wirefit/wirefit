#!/usr/bin/env python3
"""wirefit Python extractor (Phase 5, PRD 5.6).

Implements the public extractor protocol v1 (docs/extractor-protocol.md):
Request JSON on stdin, Response JSON on stdout. Built deliberately as an
EXTERNAL extractor — no wirefit source required — to dogfood the protocol.

Pydantic v2 models (and union type aliases via TypeAdapter) are converted
with pydantic's own machinery (model_json_schema), then normalized to
wirefit IR.

Spec format: "path/to/module.py#ModelName"
Role: consumed → mode="validation" (defaults make fields optional)
      provided → mode="serialization" (defaults are always emitted → required)

Mapping notes:
  int → int64 (Python ints are arbitrary-precision; int64 is the honest
  ceiling for interop — values beyond it should be str/Decimal anyway)
  float → float64 · str → string · bool → bool · UUID → uuid
  datetime → datetime · date → date · timedelta → duration · Decimal → decimal
  X | None → nullable (distinct from optional-by-default, SPEC §7)
  Literal[...] / str-Enums → enum · dict[str, T] → open object carrying T
  Field(discriminator=...) unions → oneOf + lifted discriminator
Hard errors: Any/object fields, non-string-keyed dicts, tuples, bytes-as-base64
ambiguity is accepted (bytes → bytes), non-string enums.
"""
import importlib.util
import json
import sys
import os

PROTOCOL_VERSION = 1

FORMAT_SCALARS = {
    "uuid": "uuid", "date-time": "datetime", "date": "date", "duration": "duration",
}


def die(msg):
    json.dump({"schemaVersion": PROTOCOL_VERSION, "schemas": {}, "error": msg}, sys.stdout)
    sys.exit(0)  # protocol: error in body


def load_target(project_dir, spec):
    file, _, name = spec.partition("#")
    if not name:
        die(f"spec {spec} must be <file.py>#<ModelName>")
    path = os.path.join(project_dir, file)
    modname = "wirefit_target_" + file.replace("/", "_").replace(".", "_")
    s = importlib.util.spec_from_file_location(modname, path)
    if s is None:
        die(f"cannot load {path}")
    mod = importlib.util.module_from_spec(s)
    sys.modules[modname] = mod
    try:
        s.loader.exec_module(mod)
    except Exception as e:  # noqa: BLE001 — report, don't guess
        die(f"importing {file} failed: {e}")
    if not hasattr(mod, name):
        die(f"{name} not found in {file}")
    return getattr(mod, name)


def json_schema_for(target, role):
    mode = "serialization" if role == "provided" else "validation"
    if hasattr(target, "model_json_schema"):
        return target.model_json_schema(mode=mode)
    from pydantic import TypeAdapter
    return TypeAdapter(target).json_schema(mode=mode)


def scalar(s):
    jt = {"bool": "boolean", "int32": "integer", "int64": "integer",
          "float32": "number", "float64": "number", "decimal": "number"}.get(s, "string")
    return {"type": jt, "x-ct-scalar": s}


def to_ir(js, defs, ctx, ref_stack):
    if js is True or js is None or js == {}:
        die(f"unconstrained schema at {ctx} (Any/object): give it a concrete type")
    if "$ref" in js:
        key = js["$ref"].removeprefix("#/$defs/")
        if key == js["$ref"]:
            die(f"unsupported $ref {js['$ref']} at {ctx}")
        if key in ref_stack:
            return {"x-ct-recursive": True}
        if key not in defs:
            die(f"missing $defs entry {key} at {ctx}")
        return to_ir(defs[key], defs, ctx, ref_stack + [key])

    node = dict(js)
    nullable = False
    if isinstance(node.get("type"), list):
        types = [t for t in node["type"] if t != "null"]
        nullable = len(types) != len(node["type"])
        if len(types) != 1:
            die(f"multi-type schema at {ctx}")
        node["type"] = types[0]
    variants = node.get("anyOf") or node.get("oneOf")
    if variants and any(v.get("type") == "null" for v in variants):
        rest = [v for v in variants if v.get("type") != "null"]
        nullable = True
        if len(rest) == 1:
            inner = to_ir(rest[0], defs, ctx, ref_stack)
            inner["x-ct-nullable"] = True
            return inner
        node = {**node, "anyOf": rest}
        node.pop("oneOf", None)

    ir = core_to_ir(node, defs, ctx, ref_stack)
    if nullable:
        ir["x-ct-nullable"] = True
    return ir


def core_to_ir(node, defs, ctx, ref_stack):
    variants = node.get("anyOf") or node.get("oneOf")
    if variants:
        return union_to_ir(node, variants, defs, ctx, ref_stack)
    if "const" in node:
        if not isinstance(node["const"], str):
            die(f"non-string const at {ctx}: IR enums are string-valued")
        return {**scalar("string"), "enum": [node["const"]]}
    if "enum" in node:
        if not all(isinstance(v, str) for v in node["enum"]):
            die(f"non-string enum at {ctx}: IR enums are string-valued (v1)")
        return {**scalar("string"), "enum": sorted(node["enum"])}

    t = node.get("type")
    if t == "string":
        if node.get("format") == "binary":
            return scalar("bytes")
        return scalar(FORMAT_SCALARS.get(node.get("format"), "string"))
    if t == "integer":
        return scalar("int64")
    if t == "number":
        # pydantic Decimal emits anyOf[number,string]; plain number is float64
        return scalar("float64")
    if t == "boolean":
        return scalar("bool")
    if t == "array":
        if "prefixItems" in node:
            die(f"tuple at {ctx}: not representable in IR v1")
        return {"type": "array", "items": to_ir(node.get("items"), defs, ctx + "[]", ref_stack)}
    if t == "object":
        props = node.get("properties") or {}
        ap = node.get("additionalProperties")
        open_values = ap not in (None, False)
        if open_values and not props:
            # Dict[str, V] carries V's schema; a bare True dict stays unexpressed.
            value = True if ap is True else to_ir(ap, defs, ctx + "{}", ref_stack)
            return {"type": "object", "additionalProperties": value}
        if open_values:
            die(f"mixed dict and named fields at {ctx}: not representable")
        if not props:
            die(f"object with no fields at {ctx}")
        out_props = {n: to_ir(p, defs, f"{ctx}.{n}", ref_stack) for n, p in props.items()}
        out = {"type": "object", "properties": out_props}
        required = sorted(r for r in node.get("required", []) if r in props)
        if required:
            out["required"] = required
        return out
    die(f"unsupported JSON Schema at {ctx}: {json.dumps(node)[:200]}")


def union_to_ir(node, variants, defs, ctx, ref_stack):
    resolved = [to_ir(v, defs, ctx, ref_stack) for v in variants]
    if all(r.get("enum") and "properties" not in r for r in resolved):
        values = sorted({v for r in resolved for v in r["enum"]})
        return {**scalar("string"), "enum": values}
    if not all("properties" in r for r in resolved):
        die(f"unsupported union at {ctx}: object unions with a discriminator or string-literal unions only")

    disc = (node.get("discriminator") or {}).get("propertyName")
    if not disc:
        # fall back: shared single-value enum property
        for name in sorted(resolved[0]["properties"]):
            vals = [r["properties"].get(name, {}).get("enum") for r in resolved]
            if all(v and len(v) == 1 for v in vals) and len({v[0] for v in vals}) == len(resolved):
                disc = name
                break
    if not disc:
        die(f"untagged object union at {ctx}: use Field(discriminator=...) (IR v1: tagged unions only)")

    branches = []
    for r in resolved:
        tag = r["properties"][disc]["enum"][0]
        b = {**r, "properties": {k: v for k, v in r["properties"].items() if k != disc}}
        req = [x for x in b.get("required", []) if x != disc]
        b.pop("required", None)
        if req:
            b["required"] = req
        b["x-ct-discriminator-value"] = tag
        branches.append(b)
    branches.sort(key=lambda b: b["x-ct-discriminator-value"])
    return {"x-ct-discriminator": disc, "oneOf": branches}


def main():
    req = json.load(sys.stdin)
    if req.get("schemaVersion") != PROTOCOL_VERSION:
        die(f"unsupported protocol version {req.get('schemaVersion')}")
    project_dir = req.get("projectDir", ".")
    sys.path.insert(0, project_dir)
    schemas = {}
    for spec in req.get("specs", []):
        target = load_target(project_dir, spec["ref"])
        js = json_schema_for(target, spec.get("role", "consumed"))
        schemas[spec["ref"]] = to_ir(js, js.get("$defs", {}), spec["ref"], [])
    json.dump({"schemaVersion": PROTOCOL_VERSION, "schemas": schemas}, sys.stdout, indent=1)


if __name__ == "__main__":
    main()
