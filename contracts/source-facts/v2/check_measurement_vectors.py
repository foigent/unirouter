#!/usr/bin/env python3
"""Independent acceptance arithmetic; not a production metrics query engine.

Consumes the published synthetic vectors only. Actual HTTP boundary evidence is
provided separately by native/public/media integration tests.
"""
from copy import deepcopy
from datetime import datetime
from decimal import Decimal
from pathlib import Path
import json


def percentile(values, fraction):
    if not values:
        return None
    ordered = sorted(Decimal(str(v)) for v in values)
    rank = Decimal(len(ordered) - 1) * Decimal(fraction)
    lo = int(rank)
    hi = min(lo + 1, len(ordered) - 1)
    return ordered[lo] + (ordered[hi] - ordered[lo]) * (rank - lo)


def check():
    fixture = json.loads(Path(__file__).with_name("measurement-vectors.json").read_text())
    assert fixture["fixture_only"] is True
    c = {case["id"]: case for case in fixture["cases"]}
    assert set(c) == {f"T{i:02d}" for i in range(1, 11)}
    v = c["T01"]
    assert v["response_written"] - v["ingress"] == v["expected_response_complete"] == 3000
    assert v["old_log_latency"] == 5200 and v["expected_first_output_state"] == "not_applicable"
    v = c["T02"]
    assert v["frame_prepared"] - v["old_stream_start"] == v["expected_old_ttft"] == 200
    assert v["first_write_and_flush"] - v["ingress"] == v["expected_first_output"] == 2250
    assert v["terminal_write_and_flush"] - v["ingress"] == v["expected_stream_complete"] == 6800
    assert v["old_log"] < v["terminal_write_and_flush"]
    assert not c["T03"]["first_write_succeeded"] and c["T03"]["expected_state"] == "not_observed"
    assert [s["value"] for s in c["T04"]["samples"] if s["state"] == "observed"] == c["T04"]["expected_included"] == [0]
    v = c["T05"]
    for values, expected in [(v["completed_observed"], v["expected"]), (v["completed_observed"] + [0], v["with_observed_zero"])]:
        assert len(values) == expected["n"]
        assert percentile(values, ".5") == Decimal(expected["p50"])
        assert percentile(values, ".95") == Decimal(expected["p95"])
    assert percentile([], ".95") is None and v["empty"]["state"] == "not_computable"
    v = c["T06"]
    all_rows = v["requests"]
    included = [r for r in all_rows if r["state"] in ("completed", "technical_failure")]
    failed = sum(r["state"] == "technical_failure" for r in included)
    assert (len(all_rows), failed, len(included), len(all_rows)-len(included)) == (8, 1, 4, 4)
    assert Decimal(failed)/len(included) == Decimal(v["expected"]["diagnostic_ratio"])
    assert v["expected"]["slo"] is False
    v = c["T07"]
    attempts = [a for r in v["requests"] for a in r["attempts"]]
    assert len(v["requests"]) == 3 and len(attempts) == 3
    assert sum(a["response_observed"] for a in attempts) == 2
    assert sum(len(r["attempts"]) > 1 for r in v["requests"]) == 1
    v = c["T08"]
    old_copy = deepcopy(v["old_reading"])
    def read_state(reading):
        cutoff = datetime.fromisoformat(reading["cutoff"])
        unique = {e["event_id"]: e for e in reading["events"] if datetime.fromisoformat(e["recorded_at"]) <= cutoff}
        return max(unique.values(), key=lambda e: e["recorded_at"])["kind"], len(unique)
    assert read_state(v["old_reading"]) == ("started", 1)
    assert read_state(v["new_reading"]) == ("completed", 2)
    assert v["old_reading"] == old_copy and v["request"]["started_at"][:10] == v["expected"]["request_day"]
    assert c["T09"]["expected"] == {"actor":"unknown", "human_cancel":False, "provider_timeout":False}
    assert c["T10"]["expected"] == {"ur_request_increment":0, "runtime_count_state":"unknown", "cost_source_state":"available"}
    print(json.dumps({"fixture_only":True, "cases_passed":list(c), "production_analytics":False}))


if __name__ == "__main__":
    check()
