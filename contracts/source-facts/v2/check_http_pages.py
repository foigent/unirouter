#!/usr/bin/env python3
"""Independent synthetic A/B HTTP consumer; no network or producer imports.

Checks B's published JSON Schema, source identity, envelope integrity, and
durable replay/checkpoint behavior. A uses its unchanged accepted validator.
This is Local evidence, not an XI integration or production acceptance.
"""
import argparse
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import sqlite3
import sys
import tempfile

from jsonschema import Draft202012Validator, FormatChecker

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("source_a_pages", HERE.parent / "v1/check_pages.py")
a = importlib.util.module_from_spec(spec)
spec.loader.exec_module(a)
canonical, sha, require = a.canonical, a.sha, a.require
SCHEMA = json.loads((HERE / "source-observation.schema.json").read_text())
Draft202012Validator.check_schema(SCHEMA)
VALIDATOR = Draft202012Validator(SCHEMA, format_checker=FormatChecker())
BINDINGS = {
    "unirouter.runtime-observation.v1": ("execution.runtime.observed", "runtime-observation"),
    "unirouter.attempt-observation.v1": ("execution.attempt.observed", "attempt-observation"),
    "unirouter.capability-snapshot.v1": ("execution.capability.recorded", "capability-snapshot"),
    "unirouter.policy-observation.v1": ("execution.policy.observed", "policy-observation"),
    "unirouter.destination-observation.v1": ("execution.destination.observed", "destination-observation"),
}


def source_id(parts):
    return "uf1_" + hashlib.sha256(a.length_prefixed(parts)).hexdigest()


def validate_page(vector):
    if vector["profile"] not in BINDINGS:
        return a.validate_page(vector)
    page = vector["page"]
    require(len(canonical(page)) < 8388608, "page byte limit")
    require(page["batch_version"] == "foigent.event-batch.v1", "batch version")
    require(page["connector_id"] == "unirouter.workfacts" and page["connector_version"] == "2.0.0", "B connector")
    require(isinstance(page["events"], list) and len(page["events"]) <= 50, "page item limit")
    for field in ("next_cursor", "high_watermark"):
        require(isinstance(page[field], str) and 0 < len(page[field].encode()) <= 512, "opaque cursor bound")
    for event in page["events"]:
        p = event["payload"]
        VALIDATOR.validate(p)
        require(len(canonical(p)) <= 65536, "source byte limit")
        require(p["profile"] == vector["profile"] and p["org_id"] == vector["org_id"] and p["project_id"] == vector["project_id"], "source scope/profile")
        require(p["source_instance"] == page["producer_instance"], "source instance")
        require(event["scope"] == {"org_ref": p["org_id"], "project_ref": p["project_id"]}, "envelope scope")
        require(event["subject"] == {"type": p["subject_kind"], "ref": p["subject_id"]}, "subject binding")
        require(event["correlation"]["request_ref"] == p["request_id"], "request binding")
        require(event["producer"] == {"product": "unirouter", "instance": p["source_instance"], "connector_id": "unirouter.workfacts", "connector_version": "2.0.0"}, "producer binding")
        event_type, schema_name = BINDINGS[p["profile"]]
        require(event["event_type"] == event_type and event["schema_ref"] == "foigent://events/unirouter/" + schema_name + "/v1", "event schema binding")
        fact = source_id([p[k] for k in ("schema_version", "source_instance", "org_id", "project_id", "profile", "request_id", "subject_kind", "subject_id", "observation_kind")])
        event_id = source_id([p["schema_version"] + ".revision", fact, p["revision"]])
        require(p["source_fact_id"] == fact and p["event_id"] == event_id and event["event_id"] == event_id, "source identity")
        require(sha(canonical(p)) == event["integrity"]["source_hash"], "source hash")
        identity = b"foigent.event-idempotency.v1" + a.length_prefixed(["unirouter", p["source_instance"], "", p["org_id"], p["project_id"], event_type, event_id])
        require(sha(identity) == event["identity"]["idempotency_key"], "idempotency identity")
        hash_input = copy.deepcopy(event)
        expected = hash_input["integrity"].pop("envelope_hash")
        require(sha(canonical(hash_input)) == expected, "envelope hash")
        require(event["data_policy"] == {"payload_capture_mode": "metadata_only", "raw_payload_present": False}, "metadata policy")
    return page


class Consumer:
    def __init__(self, path):
        self.db = sqlite3.connect(path)
        self.db.executescript("CREATE TABLE IF NOT EXISTS facts(id TEXT PRIMARY KEY, hash TEXT NOT NULL); CREATE TABLE IF NOT EXISTS checkpoints(stream TEXT PRIMARY KEY, cursor TEXT NOT NULL);")

    def source(self, vector, fail_after=None, fail_checkpoint=False):
        page = validate_page(vector)
        stream = canonical([page["producer_instance"], vector["org_id"], vector["project_id"], vector["profile"]]).decode()
        with self.db:
            self.db.execute("INSERT OR IGNORE INTO checkpoints VALUES(?,?)", (stream, vector["previous_cursor"]))
        for number, event in enumerate(page["events"], 1):
            identity, digest = event["identity"]["idempotency_key"], event["integrity"]["envelope_hash"]
            old = self.db.execute("SELECT hash FROM facts WHERE id=?", (identity,)).fetchone()
            require(old is None or old[0] == digest, "immutable identity conflict")
            with self.db:
                self.db.execute("INSERT OR IGNORE INTO facts VALUES(?,?)", (identity, digest))
            if number == fail_after:
                raise InterruptedError("synthetic partial-page process exit")
        if fail_checkpoint:
            raise InterruptedError("synthetic checkpoint failure")
        with self.db:
            current = self.db.execute("SELECT cursor FROM checkpoints WHERE stream=?", (stream,)).fetchone()[0]
            require(current in (vector["previous_cursor"], page["next_cursor"]), "checkpoint predecessor")
            self.db.execute("UPDATE checkpoints SET cursor=? WHERE stream=? AND cursor=?", (page["next_cursor"], stream, vector["previous_cursor"]))


def check(path):
    fixture = json.loads(path.read_text())
    require(fixture.get("fixture_only") is True, "synthetic fixture required")
    pages = fixture["pages"]
    identities = {e["identity"]["idempotency_key"] for v in pages for e in validate_page(v)["events"]}
    require(any(v["profile"] in BINDINGS and v["page"]["events"] for v in pages), "actual B pages required")
    with tempfile.TemporaryDirectory(prefix="source-b-consumer-") as root:
        database = Path(root) / "consumer.sqlite"
        consumer = Consumer(database)
        injected = False
        for vector in pages:
            if not injected and vector["profile"] in BINDINGS and vector["page"]["events"]:
                for failure in ({"fail_after": 1}, {"fail_checkpoint": True}):
                    try:
                        consumer.source(vector, **failure)
                    except InterruptedError:
                        pass
                    else:
                        raise AssertionError("failure not exercised")
                    consumer.db.close()
                    consumer = Consumer(database)
                injected = True
            consumer.source(vector)
            consumer.source(vector)
        require(injected, "B replay fixture required")
        require(consumer.db.execute("SELECT count(*) FROM facts").fetchone()[0] == len(identities), "all facts exactly once")
        consumer.db.close()
    # B has no economic amount writer. Validate original A/I-01 joins using the
    # unchanged accepted consumer in a separate database, in either arrival order.
    for source_first in (False, True):
        with tempfile.TemporaryDirectory(prefix="source-ab-economics-") as root:
            economic = a.Consumer(Path(root) / "economics.sqlite")
            if not source_first:
                for record in fixture["i01"]:
                    economic.financial(record)
            for vector in pages:
                if vector["profile"] not in BINDINGS:
                    economic.source(vector)
            if source_first:
                require(economic.totals() == (0, 0, 0), "source cannot manufacture money")
                for record in fixture["i01"]:
                    economic.financial(record)
            expected = (len(fixture["i01"]), sum(int(r["snapshot"]["money"]["calculated_cost_micros"]) for r in fixture["i01"]), sum(int(r["snapshot"]["money"]["charged_cost_micros"]) for r in fixture["i01"]))
            require(economic.totals() == expected, "original I-01 totals")
            economic.joined_references()
            economic.db.close()
    return {"test": fixture["test"], "facts": len(identities), "pages": len(pages)}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("paths", type=Path, nargs="+")
    parser.add_argument("--expected-cases", type=int)
    args = parser.parse_args()
    files = [file for path in args.paths for file in (sorted(path.glob("*.json")) if path.is_dir() else [path])]
    if args.expected_cases is not None:
        require(len(files) == args.expected_cases, "exact fixture count")
    require(files, "no fixture files")
    results = [check(path) for path in files]
    print(json.dumps({"status": "PASS", "scope": "Local synthetic authenticated A/B pages", "results": results}))


if __name__ == "__main__":
    main()
