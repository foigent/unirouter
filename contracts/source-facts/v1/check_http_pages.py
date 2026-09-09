#!/usr/bin/env python3
"""Independently consume synthetic pages emitted by authenticated local HTTP tests.

Uses the existing stdlib canonical/hash and SQLite consumer. This proves neither
general JSON Schema qualification nor an external XI integration.
"""
import argparse
import json
from pathlib import Path
import tempfile
import time

from check_pages import Consumer, require, validate_page


def check(path):
    fixture = json.loads(path.read_text())
    require(fixture.get("fixture_only") is True, "synthetic fixture required")
    pages, records = fixture["pages"], fixture["i01"]
    identities = {e["identity"]["idempotency_key"] for v in pages for e in validate_page(v)["events"]}
    calculated = sum(int(r["snapshot"]["money"]["calculated_cost_micros"]) for r in records)
    charged = sum(int(r["snapshot"]["money"]["charged_cost_micros"]) for r in records)
    require(charged == 0, "local observation fixture must not charge")
    for source_first in (False, True):
        with tempfile.TemporaryDirectory(prefix="authenticated-source-consumer-") as root:
            database = Path(root) / "consumer.sqlite"
            c = Consumer(database)
            if not source_first:
                for record in records:
                    c.financial(record)
            injected = False
            for vector in pages:
                if not injected and vector["page"]["events"]:
                    try:
                        c.source(vector, fail_after=1)
                    except InterruptedError:
                        pass
                    else:
                        raise AssertionError("partial-page failure not exercised")
                    c.db.close()
                    c = Consumer(database)
                    try:
                        c.source(vector, fail_checkpoint=True)
                    except InterruptedError:
                        pass
                    else:
                        raise AssertionError("checkpoint failure not exercised")
                    injected = True
                c.source(vector)
                c.source(vector, consumer_adapter="example-v2")
            require(injected, "nonempty authenticated page required")
            if source_first:
                require(c.totals() == (0, 0, 0), "source explanation cannot invent money")
                for record in records:
                    c.financial(record)
            for record in records:
                c.financial(record)
            require(c.db.execute("SELECT count(*) FROM facts").fetchone()[0] == len(identities), "no loss or duplicate after restart")
            require(c.totals() == (len(records), calculated, charged), "original I-01 economics must survive replay")
            require(c.joined_references() == len(records), "all captured terminal cost references join original I-01 hashes")
            c.db.close()
    return len(identities), len(pages), len(records)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("directory", type=Path)
    parser.add_argument("--expected-cases", type=int, required=True)
    args = parser.parse_args()
    files = [args.directory] if args.directory.is_file() else sorted(args.directory.glob("*.json"))
    require(len(files) == args.expected_cases, "exact expected HTTP case count")
    started = time.monotonic()
    totals = [0, 0, 0]
    for path in files:
        values = check(path)
        totals = [a + b for a, b in zip(totals, values)]
    print(f"PASS: {len(files)} authenticated HTTP cases; {totals[0]} facts, {totals[1]} pages, {totals[2]} original I-01 records; both arrival orders, partial-page process restart, checkpoint failure, adapter replay; 0 charged; consumer verification {1000*(time.monotonic()-started):.3f} ms")


if __name__ == "__main__":
    main()
