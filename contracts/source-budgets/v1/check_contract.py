#!/usr/bin/env python3
"""Independent schema/hash/reading consumer; optional real HTTP sample directory."""
import hashlib
import json
import sqlite3
import sys
from datetime import datetime
from pathlib import Path

from jsonschema import Draft202012Validator, FormatChecker

ROOT = Path(__file__).resolve().parent


def canonical(value):
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"), allow_nan=False).encode()


manifest = json.loads((ROOT / "manifest.json").read_bytes())
for name, digest in manifest["files"].items():
    assert digest == "sha256:" + hashlib.sha256((ROOT / name).read_bytes()).hexdigest(), name
validators = {}
for name in ("budget-document", "budget-reading", "budget-catalog"):
    schema = json.loads((ROOT / (name + ".schema.json")).read_bytes())
    Draft202012Validator.check_schema(schema)
    validators[name] = Draft202012Validator(schema, format_checker=FormatChecker())


def validate(value):
    catalog = value["schema_version"] == "unirouter.budget-catalog.v1"
    validators["budget-catalog" if catalog else "budget-reading"].validate(value)
    if catalog:
        refs = [e["budget_ref"] for e in value["entries"]]
        assert refs == sorted(set(refs))
        assert bool(value["next_cursor"]) == value["has_more"]
        return
    seen = set()
    for item in value["documents"]:
        doc = item["document"]
        assert item["profile"] == doc["profile"] and doc["profile"] not in seen
        seen.add(doc["profile"])
        assert item["source_sha256"] == "sha256:" + hashlib.sha256(canonical(doc)).hexdigest()
        for field in ("schema_version", "source_instance", "org_id", "project_id", "reading_id", "reading_cutoff", "observed_at", "budget_ref", "definition_revision", "binding_revision"):
            assert doc.get(field, "") == value.get(field, ""), field
        body = doc["body"]
        if doc["profile"] == "unirouter.budget-definition.v1":
            assert body["revision"] == value["definition_revision"]
        elif doc["profile"] == "unirouter.budget-period-summary.v1":
            assert body["amount_cutoff"] == value["reading_cutoff"]
            coverage, comparison = body["coverage"], body["comparison"]
            if coverage["state"] == "complete_settled_as_of":
                assert not coverage["reasons"] and coverage["materialization_pending"] == "0"
                assert datetime.fromisoformat(body["period_start"].replace("Z", "+00:00")) >= datetime.fromisoformat(coverage["certified_from"].replace("Z", "+00:00"))
            if comparison["state"] == "comparable":
                assert coverage["state"] in ("complete_settled_as_of", "exact")
                numerator, denominator = int(comparison["utilization_numerator"]), int(comparison["utilization_denominator"])
                assert str(numerator) == body["known_subtotal"]["value_minor"]
                assert int(comparison["remaining"]["value_minor"]) == denominator - numerator
                assert comparison["threshold_state"] == ("below" if numerator < denominator else "at" if numerator == denominator else "above")
                assert all(comparison["remaining"][unit] == body["known_subtotal"][unit] for unit in ("currency", "scale"))
            else:
                assert "remaining" not in comparison
        elif doc["profile"] == "unirouter.budget-alert.v1":
            assert bool(body.get("next_cursor")) == body["has_more"]
            for alert in body["entries"]:
                assert alert["human_read"] == "not_observed"
                assert alert["status_cutoff"] == value["reading_cutoff"] == alert["delivery_cutoff"]


vectors = json.loads((ROOT / "golden-readings.json").read_bytes())
database = sqlite3.connect(":memory:")
database.execute("CREATE TABLE readings(reading_id TEXT PRIMARY KEY, body BLOB NOT NULL)")


def consume(value):
    validate(value)
    if "reading_id" not in value:
        return
    wire = canonical(value)
    before = database.execute("SELECT body FROM readings WHERE reading_id=?", (value["reading_id"],)).fetchone()
    if before:
        assert before[0] == wire, "same reading identity cannot change"
    else:
        database.execute("INSERT INTO readings VALUES(?,?)", (value["reading_id"], wire))


for value in vectors["positive"]:
    consume(value)
    consume(value)
# A later valid response cannot overwrite an already retained reading identity.
changed = json.loads(json.dumps(vectors["positive"][0]))
changed["catalog_revision"] = str(int(changed["catalog_revision"]) + 1)
validate(changed)
try:
    consume(changed)
except AssertionError as error:
    assert "same reading identity cannot change" in str(error)
else:
    raise AssertionError("consumer overwrote a retained reading")

for vector in vectors["negative"]:
    try:
        validate(vector["value"])
    except Exception:
        pass
    else:
        raise AssertionError("negative vector accepted: " + vector["name"])
samples = []
if len(sys.argv) > 1:
    samples = sorted(Path(sys.argv[1]).glob("*.json"))
    assert samples, "sample directory is empty"
    for path in samples:
        value = json.loads(path.read_bytes())
        consume(value)
        consume(value)
database.commit()
print(json.dumps({"status": "passed", "positive": len(vectors["positive"]), "negative": len(vectors["negative"]), "http_samples": len(samples), "retained_readings": database.execute("SELECT count(*) FROM readings").fetchone()[0], "transport": "snapshot_not_financial_event"}, sort_keys=True))
