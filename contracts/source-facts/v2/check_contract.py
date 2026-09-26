#!/usr/bin/env python3
"""Validate public B assets independently of Go producer code."""
import hashlib
import json
from pathlib import Path
import struct

from jsonschema import Draft202012Validator, FormatChecker

HERE = Path(__file__).resolve().parent


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()


def digest(wire):
    return "sha256:" + hashlib.sha256(wire).hexdigest()


def source_id(parts):
    encoded = [p.encode() for p in parts]
    return "uf1_" + hashlib.sha256(b"".join(struct.pack(">I", len(p))+p for p in encoded)).hexdigest()


def check():
    manifest = json.loads((HERE/"manifest.json").read_text())
    counts = {}
    for family, schema_file, vectors_file, expected_hash in [
        ("observations", manifest["source_schema_file"], manifest["source_vectors_file"], manifest["source_schema_sha256"]),
        ("runtime_current", manifest["runtime_current"]["schema_file"], manifest["runtime_current"]["vectors_file"], manifest["runtime_current"]["schema_sha256"]),
    ]:
        wire = (HERE/schema_file).read_bytes()
        assert digest(wire) == expected_hash, "manifest schema pin mismatch"
        schema = json.loads(wire)
        Draft202012Validator.check_schema(schema)
        validator = Draft202012Validator(schema, format_checker=FormatChecker())
        vectors = json.loads((HERE/vectors_file).read_text())
        for case in vectors["valid"]:
            document = case["document"]
            validator.validate(document)
            if family == "observations":
                assert canonical(document).decode() == case["canonical_utf8"]
                assert digest(canonical(document)) == case["source_sha256"]
                fact = source_id([document[k] for k in ("schema_version", "source_instance", "org_id", "project_id", "profile", "request_id", "subject_kind", "subject_id", "observation_kind")])
                assert document["source_fact_id"] == fact
                assert document["event_id"] == source_id([document["schema_version"]+".revision", fact, document["revision"]])
            else:
                content = {k:v for k,v in document.items() if k != "source_sha256"}
                assert canonical(content).decode() == case["canonical_source_utf8"]
                assert digest(canonical(content)) == document["source_sha256"]
        for case in vectors["invalid"]:
            if case.get("validation") == "domain":
                document = case["document"]
                fact = source_id([document[k] for k in ("schema_version", "source_instance", "org_id", "project_id", "profile", "request_id", "subject_kind", "subject_id", "observation_kind")])
                assert document["source_fact_id"] != fact
            else:
                assert list(validator.iter_errors(case["document"])), "invalid schema vector accepted: " + case["name"]
        counts[family] = {"positive":len(vectors["valid"]), "negative":len(vectors["invalid"])}
    print(json.dumps({"fixture_only":True, "schema_pins_verified":True, "passed":counts}))


if __name__ == "__main__":
    check()
