#!/usr/bin/env python3
"""Validate the candidate with the pinned general Draft 2020-12 engine.

JSON Schema does not compare two document fields or interpret decimal-string
int64 bounds. Those two semantic vectors remain required Go decoder checks.
Duplicate-key and canonical-byte checks are performed before schema validation.
"""
import argparse
import importlib.metadata
import json
from pathlib import Path

from jsonschema import Draft202012Validator
from check_pages import canonical, require


def exact_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate JSON key")
        result[key] = value
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--http-pages", action="append", type=Path, default=[])
    args = parser.parse_args()
    require(importlib.metadata.version("jsonschema") == "4.25.1", "install the pinned requirements-schema.txt")
    root = Path(__file__).parent
    schema = json.loads((root / "source-fact.schema.json").read_text())
    require(schema["$schema"] == "https://json-schema.org/draft/2020-12/schema", "exact schema dialect")
    Draft202012Validator.check_schema(schema)
    validator = Draft202012Validator(schema)
    vectors = json.loads((root / "golden-source-facts.json").read_text())
    for vector in vectors["valid"]:
        validator.validate(vector["document"])
    semantic = {"scope_identity_mismatch", "signed_int64_overflow"}
    lexical = {"duplicate_key", "noncanonical_whitespace"}
    schema_rejected, wire_rejected, domain_only = 0, 0, 0
    for vector in vectors["invalid"]:
        name, wire = vector["name"], vector["wire"]
        try:
            document = json.loads(wire, object_pairs_hook=exact_object)
            require(canonical(document).decode() == wire, "noncanonical source bytes")
        except ValueError:
            require(name in lexical, "unexpected lexical vector classification")
            wire_rejected += 1
            continue
        errors = list(validator.iter_errors(document))
        if name in semantic:
            require(not errors, "domain-only vector unexpectedly changed schema meaning")
            domain_only += 1
        else:
            require(errors, "invalid schema vector accepted: " + name)
            schema_rejected += 1
    require((schema_rejected, wire_rejected, domain_only) == (5, 2, 2), "exact negative coverage")
    page_vectors = json.loads((root / "golden-pages.json").read_text())
    static_payloads = 0
    for vector in page_vectors["valid"]:
        for event in vector["page"]["events"]:
            validator.validate(event["payload"])
            static_payloads += 1
    actual_payloads = 0
    for directory in args.http_pages:
        files = sorted(directory.glob("*.json"))
        require(files, "missing authenticated HTTP evidence")
        for path in files:
            fixture = json.loads(path.read_text())
            require(fixture.get("fixture_only") is True, "synthetic fixture required")
            for vector in fixture["pages"]:
                for event in vector["page"]["events"]:
                    validator.validate(event["payload"])
                    actual_payloads += 1
    print(f"PASS: jsonschema 4.25.1 Draft 2020-12 meta-schema; {len(vectors['valid'])} valid source vectors; {schema_rejected} schema negatives, {wire_rejected} wire negatives; {domain_only} domain-only negatives explicitly left to required Go decoder tests; {static_payloads} static page payloads; {actual_payloads} authenticated HTTP payloads")


if __name__ == "__main__":
    main()
