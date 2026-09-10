#!/usr/bin/env python3
"""Generate the finite D snapshot schemas; never generate events or amounts."""
import hashlib
import json
from copy import deepcopy
from pathlib import Path

ROOT = Path(__file__).resolve().parent
VERSION = "unirouter.budget-snapshot.v1"
PROFILES = ["unirouter.budget-definition.v1", "unirouter.budget-period-summary.v1", "unirouter.budget-alert.v1"]
BASE = "https://unirouter.local/contracts/source-budgets/v1/"


def obj(properties, optional=()):
    return {"type": "object", "properties": properties, "required": [p for p in properties if p not in optional], "additionalProperties": False}


def text(maximum=256, minimum=1):
    return {"type": "string", "minLength": minimum, "maxLength": maximum}


def enum(*values):
    return {"type": "string", "enum": list(values)}


def ref(name):
    return {"$ref": "#/$defs/" + name}


def array(items, maximum=50):
    return {"type": "array", "items": items, "maxItems": maximum}


ID = {"type": "string", "pattern": "^[1-9][0-9]{0,18}$"}
COUNT = {"type": "string", "pattern": "^(0|[1-9][0-9]{0,18})$"}
TIME = {"type": "string", "format": "date-time", "maxLength": 40}
HASH = {"type": "string", "pattern": "^sha256:[a-f0-9]{64}$"}
MONEY = obj({"value_minor": {"type": "string", "pattern": "^(0|[1-9][0-9]{0,77})$"}, "currency": {"type": "string", "pattern": "^[A-Z]{3}$"}, "scale": ID})
SIGNED_MONEY = obj({**MONEY["properties"], "value_minor": {"type": "string", "pattern": "^(0|-?[1-9][0-9]{0,77})$"}})
DEFINITION = obj({
    "origin": enum("gateway_project", "enterprise_project"), "revision": ID,
    "setting_state": enum("unset", "unsupported", "active", "inactive_zero", "inactive_period_none", "not_approved"),
    "original_cents": {"type": "string", "pattern": "^(0|-?[1-9][0-9]{0,18})$"}, "original_period": text(20),
    "period_kind": enum("unknown", "none", "daily", "monthly", "fiscal_period"), "fiscal_period_ref": text(80),
    "amount": ref("money"), "authority_ref": text(), "changed_at": TIME,
    "change_kind": enum("observed_baseline", "created", "value_changed", "authority_version"),
    "approval_state": enum("draft", "approved", "closed"), "runtime_effect": {"const": "report_only"},
}, ("original_cents", "original_period", "fiscal_period_ref", "amount", "approval_state"))
COMPARISON = obj({
    "state": enum("not_comparable", "inactive_budget", "coverage_gap", "invalid_amount", "unit_mismatch", "comparable"),
    "remaining": ref("signed_money"), "utilization_numerator": MONEY["properties"]["value_minor"],
    "utilization_denominator": {"type": "string", "pattern": "^[1-9][0-9]{0,77}$"}, "threshold_state": enum("below", "at", "above"),
}, ("remaining", "utilization_numerator", "utilization_denominator", "threshold_state"))
COMPARISON["allOf"] = [{"if": {"properties": {"state": {"const": "comparable"}}}, "then": {"required": ["remaining", "utilization_numerator", "utilization_denominator", "threshold_state"]}, "else": {"not": {"anyOf": [{"required": [x]} for x in ("remaining", "utilization_numerator", "utilization_denominator", "threshold_state")]}}}]
COVERAGE = obj({
    "state": enum("uncertified", "known_subtotal_with_gap", "complete_settled_as_of", "unknown", "no_allocation_run", "unit_mismatch", "exact", "partial"),
    "reasons": array(enum("period_not_configured", "writer_uncertified", "period_before_certification", "restore_gap", "financial_journal_unavailable", "unattributable_financial_outbox", "financial_materialization_pending", "financial_materialization_gap", "no_allocation_run", "unit_mismatch", "allocation_partial"), 12),
    "coverage_epoch": ID, "certified_from": TIME, "restore_boundary": TIME,
    "writer_contract": enum("unirouter.gateway-financial-journal.v2", "enterprise_finance.report_only"),
    "materialization_pending": COUNT, "materialized_awaiting_ack": COUNT,
}, ("coverage_epoch", "certified_from", "restore_boundary"))
UNRESOLVED = obj({
    "state": enum("not_observed", "none_observed", "unresolved", "unknown", "not_modeled"),
    "open_reservations": COUNT, "financial_holds": COUNT, "reserved_at_read": ref("money"),
    "scope": enum("all_current_project_reservations_as_of_read", "enterprise_project_allocation"),
}, ("reserved_at_read",))
SUMMARY = obj({
    "period_state": enum("not_configured", "original_utc_period", "open", "preliminary", "reconciled", "closed"),
    "period_start": TIME, "period_end": TIME, "amount_cutoff": TIME,
    "authority": enum("usage_finalization_journal.settled.charged_cost_micros", "enterprise_finance_allocation_results.project_slice"),
    "known_subtotal": ref("money"), "coverage": ref("coverage"), "comparison": ref("comparison"), "unresolved_economics": ref("unresolved"),
    "excluded": array(enum("asynchronous_media_charge_facts", "calculated_cost", "wallet_balance", "legacy_project_counter", "historical_call_log_baseline", "gateway_journal", "other_allocation_targets", "organization_totals"), 8),
    "allocation_run_ref": text(160), "committed_state": {"const": "not_modeled"}, "forecast_state": {"const": "not_modeled"},
}, ("period_start", "period_end", "known_subtotal", "allocation_run_ref"))
ALERT = obj({
    "event_ref": {"type": "string", "pattern": "^notification_events:[1-9][0-9]{0,18}$"}, "triggered_at": TIME,
    "trigger_evidence": enum("unknown", "original_event_amounts", "original_legacy_event_cents"), "budget_version_state": {"const": "version_unknown"},
    "budget_at_trigger": ref("money"), "spent_at_trigger": ref("money"), "period_kind": enum("daily", "monthly"), "period_start": TIME,
    "threshold_percent": ID, "status": enum("open", "acknowledged", "resolved"), "source_updated_at": TIME, "status_cutoff": TIME,
    "delivery_state": enum("not_configured", "pending", "delivered", "failed"), "last_delivery_at": TIME, "delivery_cutoff": TIME, "human_read": {"const": "not_observed"},
}, ("budget_at_trigger", "spent_at_trigger", "period_kind", "period_start", "threshold_percent", "last_delivery_at"))
ALERTS = obj({"coverage": enum("project_budget_notifications", "not_modeled_by_original_authority"), "entries": array(ref("alert")), "has_more": {"type": "boolean"}, "next_cursor": text(4096)}, ("next_cursor",))
DEFS = {"money": MONEY, "signed_money": SIGNED_MONEY, "definition": DEFINITION, "comparison": COMPARISON, "coverage": COVERAGE, "unresolved": UNRESOLVED, "summary": SUMMARY, "alert": ALERT, "alerts": ALERTS}
COMMON = {"schema_version": {"const": VERSION}, "source_instance": text(128), "org_id": ID, "project_id": ID,
          "reading_id": {"type": "string", "pattern": "^ur-budget-reading-[a-f0-9]{32}$"}, "reading_cutoff": TIME, "observed_at": TIME,
          "budget_ref": text(), "definition_revision": ID, "binding_revision": {"type": "string", "pattern": "^([1-9][0-9]{0,18})?$"}}
DOCUMENT = obj({**COMMON, "profile": enum(*PROFILES), "body": {}})
DOCUMENT["allOf"] = [{"if": {"properties": {"profile": {"const": p}}}, "then": {"properties": {"body": ref(body)}}} for p, body in zip(PROFILES, ("definition", "summary", "alerts"))]
READING = obj({**COMMON, "access_generation": ID, "catalog_revision": ID, "documents": {**array(obj({"profile": enum(*PROFILES), "document": DOCUMENT, "source_sha256": HASH}), 3), "minItems": 1}}, ("binding_revision",))
ENTRY = obj({"budget_ref": text(), "origin": enum("gateway_project", "enterprise_project"), "definition_revision": ID, "binding_revision": ID}, ("binding_revision",))
CATALOG = obj({"schema_version": {"const": "unirouter.budget-catalog.v1"}, "source_instance": text(128), "org_id": ID, "project_id": ID, "reading_cutoff": TIME,
               "catalog_revision": ID, "access_generation": ID, "profiles": {**array(enum(*PROFILES), 3), "uniqueItems": True, "minItems": 1},
               "enterprise_scope": enum("scope_not_configured", "explicit_project_binding"), "entries": array(ENTRY), "has_more": {"type": "boolean"}, "next_cursor": text(4096, 0)})


def write(name, value):
    wire = (json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + "\n").encode()
    (ROOT / name).write_bytes(wire)
    return "sha256:" + hashlib.sha256(wire).hexdigest()


files = {}
for name, schema in (("budget-document.schema.json", DOCUMENT), ("budget-reading.schema.json", READING), ("budget-catalog.schema.json", CATALOG)):
    full = {"$schema": "https://json-schema.org/draft/2020-12/schema", "$id": BASE + name, "$defs": DEFS, **schema}
    files[name] = write(name, full)

def canonical(value):
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()


def make_reading(index, complete, amount="1000000000", spent="600000000"):
    at = "2026-10-02T00:00:00.000000Z"
    base = {"schema_version": VERSION, "source_instance": "synthetic-UR-<&>-范围", "org_id": "1", "project_id": "2",
            "reading_id": "ur-budget-reading-" + f"{index:032x}", "reading_cutoff": at, "observed_at": at,
            "budget_ref": "gateway-project:2", "definition_revision": "2", "binding_revision": ""}
    budget = {"value_minor": amount, "currency": "CNY", "scale": "1000000"}
    subtotal = {**budget, "value_minor": spent}
    definition = {"origin": "gateway_project", "revision": "2", "setting_state": "active", "period_kind": "monthly", "amount": budget,
                  "authority_ref": "projects:2", "changed_at": "2026-09-10T00:00:00.000000Z", "change_kind": "value_changed", "runtime_effect": "report_only"}
    comparison = {"state": "coverage_gap"}
    if complete:
        comparison = {"state": "comparable", "remaining": {**budget, "value_minor": str(int(amount) - int(spent))},
                      "utilization_numerator": spent, "utilization_denominator": amount,
                      "threshold_state": "below" if int(spent) < int(amount) else "at" if spent == amount else "above"}
    summary = {"period_state": "original_utc_period", "period_start": "2026-10-01T00:00:00.000000Z", "period_end": "2026-11-01T00:00:00.000000Z", "amount_cutoff": at,
               "authority": "usage_finalization_journal.settled.charged_cost_micros", "known_subtotal": subtotal,
               "coverage": {"state": "complete_settled_as_of" if complete else "known_subtotal_with_gap", "reasons": [] if complete else ["restore_gap"],
                            "coverage_epoch": "1", "certified_from": "2026-09-10T00:00:00.000000Z" if complete else "2026-10-01T12:00:00.000000Z",
                            "writer_contract": "unirouter.gateway-financial-journal.v2", "materialization_pending": "0", "materialized_awaiting_ack": "1"},
               "comparison": comparison, "unresolved_economics": {"state": "unresolved", "open_reservations": "1", "financial_holds": "0", "reserved_at_read": {**budget, "value_minor": "200000000"}, "scope": "all_current_project_reservations_as_of_read"},
               "excluded": ["asynchronous_media_charge_facts", "wallet_balance"], "committed_state": "not_modeled", "forecast_state": "not_modeled"}
    alerts = {"coverage": "project_budget_notifications", "has_more": False, "entries": [{"event_ref": "notification_events:1", "triggered_at": "2026-09-30T00:00:00.000000Z", "trigger_evidence": "original_event_amounts", "budget_version_state": "version_unknown", "budget_at_trigger": budget, "spent_at_trigger": {**budget, "value_minor": "1200000000"}, "period_kind": "monthly", "period_start": "2026-09-01T00:00:00.000000Z", "threshold_percent": "100", "status": "acknowledged", "source_updated_at": at, "status_cutoff": at, "delivery_state": "delivered", "last_delivery_at": at, "delivery_cutoff": at, "human_read": "not_observed"}]}
    documents = []
    for profile, body in zip(PROFILES, (definition, summary, alerts)):
        doc = {**base, "profile": profile, "body": body}
        documents.append({"profile": profile, "document": doc, "source_sha256": "sha256:" + hashlib.sha256(canonical(doc)).hexdigest()})
    return {**base, "access_generation": "1", "catalog_revision": "2", "documents": documents}


positive = [make_reading(1, True), make_reading(2, False, spent="200000000"), make_reading(3, True, spent="0"), make_reading(4, True, spent="1200000000"), make_reading(5, True, spent="999999999")]
negative = []
def bad(name, change, kind="domain"):
    value = deepcopy(positive[0]); change(value)
    if name != "wrong_hash":
        for item in value["documents"]:
            item["source_sha256"] = "sha256:" + hashlib.sha256(canonical(item["document"])).hexdigest()
    negative.append({"name": name, "reject_by": kind, "value": value})
bad("numeric_money", lambda r: r["documents"][0]["document"]["body"]["amount"].update(value_minor=1000), "schema")
bad("fabricated_request", lambda r: r.update(request_id="not-an-execution-event"), "schema")
bad("wrong_hash", lambda r: r["documents"][0].update(source_sha256="sha256:" + "0" * 64))
bad("mixed_reading", lambda r: r["documents"][0]["document"].update(reading_id="ur-budget-reading-" + "f" * 32))
bad("duplicate_profile", lambda r: r["documents"].__setitem__(2, deepcopy(r["documents"][0])))
bad("wrong_remaining", lambda r: r["documents"][1]["document"]["body"]["comparison"]["remaining"].update(value_minor="400000001"))
bad("partial_is_not_comparable", lambda r: r["documents"][1]["document"]["body"]["coverage"].update(state="known_subtotal_with_gap", reasons=["restore_gap"]))
bad("ack_is_not_read", lambda r: r["documents"][2]["document"]["body"]["entries"][0].update(human_read="confirmed"), "schema")
files["golden-readings.json"] = write("golden-readings.json", {"positive": positive, "negative": negative})
write("manifest.json", {"schema_version": "unirouter.budget-contract-manifest.v1", "status": "local_candidate", "source_schema": VERSION,
                        "transport": "authorized_current_snapshots", "profiles": PROFILES, "files": files, "event_feed": False,
                        "financial_authority": "original_gateway_journal_or_bound_enterprise_allocation", "retention": "receiver_retains_readings_no_event_cursor"})
