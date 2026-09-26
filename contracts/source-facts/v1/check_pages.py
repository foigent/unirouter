#!/usr/bin/env python3
"""Offline hash/page/join and durable-checkpoint fixtures, using only stdlib.

This is a bounded independent consumer example, not a general JSON Schema
validator or evidence of authenticated HTTP/XI/production integration.
"""
import copy
import hashlib
import json
from pathlib import Path
import re
import sqlite3
import struct
import tempfile


def canonical(value):
    return json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(",", ":")).encode()


def sha(wire):
    return "sha256:" + hashlib.sha256(wire).hexdigest()


def length_prefixed(parts):
    return b"".join(struct.pack(">I", len(p.encode())) + p.encode() for p in parts)


def require(condition, reason):
    if not condition:
        raise ValueError(reason)


def validate_page(vector):
    page = vector["page"]
    require(len(canonical(page)) < 8388608, "page bytes")
    require(page["batch_version"] == "foigent.event-batch.v1", "page version")
    require(page["connector_id"] == "unirouter.workfacts", "connector")
    require(isinstance(page["events"], list) and len(page["events"]) <= 50, "items")
    for key in ("next_cursor", "high_watermark"):
        value = page[key]
        require(isinstance(value, str) and 0 < len(value.encode()) <= 512 and value.strip() == value, "cursor")
    fields = {"unirouter.execution-context.v1": "core", "unirouter.execution-identity.v1": "identity", "unirouter.cost-basis.v1": "cost"}
    for event in page["events"]:
        p = event["payload"]
        require(p["profile"] == vector["profile"] and p["org_id"] == vector["org_id"] and p["project_id"] == vector["project_id"], "page scope/profile")
        require(event["scope"] == {"org_ref": p["org_id"], "project_ref": p["project_id"]}, "event scope")
        require(event["subject"]["ref"] == p["request_id"] and event["correlation"]["request_ref"] == p["request_id"], "request binding")
        producer = event["producer"]
        require(producer == {"product": "unirouter", "instance": page["producer_instance"], "connector_id": page["connector_id"], "connector_version": page["connector_version"]}, "producer binding")
        require(p["source_instance"] == page["producer_instance"], "source instance")
        for name in ("org_id", "project_id", "revision"):
            value = p[name]
            require(isinstance(value, str) and re.fullmatch(r"[1-9][0-9]*", value) and int(value) <= 2**63-1, "int64 decimal")
        expected_field = fields[p["profile"]]
        require(not (({"core", "identity", "cost"} - {expected_field}) & p.keys()), "profile field leak")
        fact = "uf1_" + hashlib.sha256(length_prefixed([p["schema_version"], p["source_instance"], p["org_id"], p["project_id"], p["profile"], p["request_id"], p["stage"]])).hexdigest()
        event_id = "uf1_" + hashlib.sha256(length_prefixed([p["schema_version"] + ".revision", fact, p["revision"]])).hexdigest()
        require(p["source_fact_id"] == fact and p["event_id"] == event_id and event["event_id"] == event_id, "source identity")
        require(sha(canonical(p)) == event["integrity"]["source_hash"], "source hash")
        identity = b"foigent.event-idempotency.v1" + length_prefixed(["unirouter", p["source_instance"], "", p["org_id"], p["project_id"], event["event_type"], event_id])
        require(sha(identity) == event["identity"]["idempotency_key"], "idempotency identity")
        hash_input = copy.deepcopy(event)
        expected = hash_input["integrity"].pop("envelope_hash")
        require(sha(canonical(hash_input)) == expected, "envelope hash")
        if expected_field == "cost":
            require("calculated_cost_micros" not in p["cost"] and "charged_cost_micros" not in p["cost"], "cost explanation is not another amount")
    return page


class Consumer:
    def __init__(self, path):
        self.db = sqlite3.connect(path)
        self.db.executescript("""
        CREATE TABLE IF NOT EXISTS facts(identity TEXT PRIMARY KEY, hash TEXT NOT NULL, payload TEXT NOT NULL);
        CREATE TABLE IF NOT EXISTS economics(identity TEXT PRIMARY KEY, hash TEXT NOT NULL, calculated INTEGER NOT NULL, charged INTEGER NOT NULL);
        CREATE TABLE IF NOT EXISTS checkpoints(stream TEXT PRIMARY KEY, cursor TEXT NOT NULL);
        """)

    def financial(self, record):
        wire = record["snapshot_wire"].encode()
        require(sha(wire) == record["snapshot_sha256"], "financial hash")
        snapshot = json.loads(wire)
        require(snapshot == record["snapshot"], "financial snapshot bytes")
        identity = self.economic_identity(record["source_instance"], str(snapshot["org_id"]), str(snapshot["project_id"]), snapshot["request_id"], record["snapshot_revision"])
        row = self.db.execute("SELECT hash FROM economics WHERE identity=?", (identity,)).fetchone()
        require(row is None or row[0] == record["snapshot_sha256"], "financial revision conflict")
        m = snapshot["money"]
        require(m["currency"] == "CNY" and m["amount_scale"] == 1000000, "money units")
        with self.db:
            self.db.execute("INSERT OR IGNORE INTO economics VALUES(?,?,?,?)", (identity, record["snapshot_sha256"], int(m["calculated_cost_micros"]), int(m["charged_cost_micros"])))

    @staticmethod
    def economic_identity(source, org, project, request, revision):
        return canonical([source, org, project, request, revision]).decode()

    def source(self, vector, fail_after=None, fail_checkpoint=False, consumer_adapter="example-v1"):
        # Adapter implementation labels never become an economic identity.
        require(consumer_adapter in ("example-v1", "example-v2"), "fixture adapter")
        page = validate_page(vector)
        stream = canonical([page["producer_instance"], vector["org_id"], vector["project_id"], vector["profile"]]).decode()
        with self.db:
            self.db.execute("INSERT OR IGNORE INTO checkpoints VALUES(?,?)", (stream, vector["previous_cursor"]))
        for number, event in enumerate(page["events"], 1):
            identity, digest = event["identity"]["idempotency_key"], event["integrity"]["envelope_hash"]
            old = self.db.execute("SELECT hash FROM facts WHERE identity=?", (identity,)).fetchone()
            require(old is None or old[0] == digest, "immutable event conflict")
            with self.db:
                self.db.execute("INSERT OR IGNORE INTO facts VALUES(?,?,?)", (identity, digest, canonical(event["payload"]).decode()))
            if fail_after == number:
                raise InterruptedError("simulated process exit before page checkpoint")
        if fail_checkpoint:
            raise InterruptedError("simulated checkpoint write failure")
        with self.db:
            current = self.db.execute("SELECT cursor FROM checkpoints WHERE stream=?", (stream,)).fetchone()[0]
            require(current in (vector["previous_cursor"], page["next_cursor"]), "checkpoint CAS conflict")
            self.db.execute("UPDATE checkpoints SET cursor=? WHERE stream=? AND cursor=?", (page["next_cursor"], stream, vector["previous_cursor"]))

    def totals(self):
        return self.db.execute("SELECT count(*),COALESCE(sum(calculated),0),COALESCE(sum(charged),0) FROM economics").fetchone()

    def joined_references(self):
        joined = 0
        for (wire,) in self.db.execute("SELECT payload FROM facts"):
            p = json.loads(wire)
            if "cost" not in p:
                continue
            ref = p["cost"]["economic_reference"]
            identity = self.economic_identity(ref["source_instance"], ref["org_id"], ref["project_id"], ref["request_id"], ref["revision"])
            row = self.db.execute("SELECT hash FROM economics WHERE identity=?", (identity,)).fetchone()
            if row:
                require(row[0] == ref["snapshot_sha256"], "I01/I02 hash disagreement")
                joined += 1
        return joined


def main():
    vectors = json.loads(Path(__file__).with_name("golden-pages.json").read_text())
    for v in vectors["valid"]:
        validate_page(v)
    for v in vectors["invalid"]:
        try:
            validate_page(v)
        except (ValueError, KeyError, TypeError):
            pass
        else:
            raise AssertionError("invalid fixture accepted: " + v["name"])
    finance = vectors["i01"]
    require([r["snapshot"]["money"]["calculated_cost_micros"] for r in finance] == ["30000000", "20000000", "40000000", "10000000", "5000000"], "original individual R1-R5 amounts")
    pages = [next(v for v in vectors["valid"] if v["name"] == "original_"+r["label"]) for r in finance]
    for source_first in (False, True):
        with tempfile.TemporaryDirectory(prefix="source-consumer-") as directory:
            path = Path(directory)/"consumer.sqlite"
            c = Consumer(path)
            if not source_first:
                for r in finance[:4]: c.financial(r)
            combined = copy.deepcopy(pages[0])
            combined["page"]["events"] += pages[1]["page"]["events"]
            combined["page"]["next_cursor"] = pages[1]["page"]["next_cursor"]
            combined["page"]["high_watermark"] = combined["page"]["next_cursor"]
            try: c.source(combined, fail_after=1)
            except InterruptedError: pass
            require(c.db.execute("SELECT count(*) FROM facts").fetchone()[0] == 1, "durable partial page")
            c.db.close()
            c = Consumer(path)
            try: c.source(combined, fail_checkpoint=True)
            except InterruptedError: pass
            require(c.db.execute("SELECT cursor FROM checkpoints").fetchone()[0] == combined["previous_cursor"], "failed checkpoint must not advance")
            c.source(combined)
            for p in pages[2:4]: c.source(p)
            if source_first:
                require(c.totals() == (0,0,0), "I02 must not invent I01 amounts")
                for r in finance[:4]: c.financial(r)
            require(c.totals() == (4,100000000,0) and c.joined_references() == 4, "initial R1-R4")
            c.source(combined, consumer_adapter="example-v2")
            for r in finance[:4]: c.financial(r)
            require(c.totals() == (4,100000000,0), "repeat/consumer adapter must not duplicate money")
            c.source(pages[4]);c.financial(finance[4])
            require(c.totals() == (5,105000000,0) and c.joined_references() == 5, "late R5")
            c.db.close()
    print("PASS: 14 valid / 7 invalid static pages; exact original R1-R5; both I01/I02 orders; durable partial-page restart, failed checkpoint, repeat and consumer-adapter replay; 100→105 CNY calculated, 0 charged")


if __name__ == "__main__":
    main()
