#!/usr/bin/env python3
"""Validate the independent v2 source schema, identities and golden vectors."""
import hashlib
import json
from pathlib import Path
import struct

from jsonschema import Draft202012Validator

HERE = Path(__file__).resolve().parent


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()


def digest(wire):
    return "sha256:" + hashlib.sha256(wire).hexdigest()


def source_id(parts):
    encoded = [part.encode() for part in parts]
    return "uf1_" + hashlib.sha256(b"".join(struct.pack(">I", len(part)) + part for part in encoded)).hexdigest()


def check():
    manifest = json.loads((HERE / "manifest.json").read_text())
    schema_wire = (HERE / manifest["source_schema_file"]).read_bytes()
    vectors_wire = (HERE / manifest["source_vectors_file"]).read_bytes()
    assert digest(schema_wire) == manifest["source_schema_sha256"]
    assert digest(vectors_wire) == manifest["source_vectors_sha256"]
    schema = json.loads(schema_wire)
    Draft202012Validator.check_schema(schema)
    validator = Draft202012Validator(schema)
    vectors = json.loads(vectors_wire)
    for case in vectors["valid"]:
        doc = case["document"]
        validator.validate(doc)
        wire = canonical(doc)
        assert wire.decode() == case["canonical_utf8"]
        assert digest(wire) == case["source_sha256"]
        fact = source_id([doc[k] for k in ("schema_version", "source_instance", "org_id", "project_id", "profile", "request_id", "subject_kind", "subject_id", "observation_kind")])
        assert doc["source_fact_id"] == fact
        assert doc["event_id"] == source_id([doc["schema_version"] + ".revision", fact, doc["revision"]])
        assert doc["context_ref"] == source_id(["unirouter.source-fact.v1", doc["source_instance"], doc["org_id"], doc["project_id"], "unirouter.execution-context.v1", doc["request_id"], "authenticated"])
    for case in vectors["invalid"]:
        assert list(validator.iter_errors(case["document"])), case["name"]
    print(json.dumps({"schema_pins_verified": True, "positive": len(vectors["valid"]), "negative": len(vectors["invalid"])}))


if __name__ == "__main__":
    check()
