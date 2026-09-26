#!/usr/bin/env python3
"""Local host-proof tests; no Docker, Target or database connection."""
import importlib.util
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("retire", Path(__file__).with_name("runtime-generation-retire.py"))
retire = importlib.util.module_from_spec(spec)
spec.loader.exec_module(retire)


class Evidence:
    def __init__(self, records):
        self.records = records

    def read(self, name):
        return self.records[name]

    def has(self, name):
        return name in self.records


class RetirementHostProofTests(unittest.TestCase):
    def fixture(self):
        plan = {"plan_digest": "p" * 64, "old_instances": {"canary": "uri_" + "a" * 32},
                "containers": {"canary": {"id": "b" * 64, "image": "sha256:" + "c" * 64}}}
        results = {
            "withdraw-canary": {"change_id": "operation-withdraw-canary", "outcome": "old_proxy_workers_exited", "after_sha256": "d" * 64},
            "stop-canary": {"container_id": "b" * 64, "exit": 0, "shutdown_events": ["Server exited"], "incomplete": False},
            "create-canary": {"container_id": "e" * 64, "instance": "uri_" + "e" * 32},
            "rejoin-canary": {"change_id": "operation-rejoin-canary"}, "verify": {"pins_unchanged": True}}
        records = {}
        for stage, result in results.items():
            records["start-" + stage + ".json"] = {"plan_digest": plan["plan_digest"]}
            records["done-" + stage + ".json"] = {"plan_digest": plan["plan_digest"], "result": result}
        old = {"instance": plan["old_instances"]["canary"], "artifact_digest": plan["containers"]["canary"]["image"],
               "support_manifest_digest": "f" * 64, "compatibility_epoch": "unirouter.runtime-compatibility.v1",
               "schema_head": "000115_stream_counter_version", "revision": 3, "state": "serving",
               "ingress_state": "absent", "lease_live": False}
        current = {"head": "latest-head", "head_status": "applied", "generations": [old,
                   {"instance": "uri_" + "1" * 32, "artifact_digest": "sha256:" + "1" * 64, "ingress_state": "receiving"},
                   {"instance": "uri_" + "2" * 32, "artifact_digest": "sha256:" + "2" * 64, "ingress_state": "receiving"}]}
        backends = {"uniroute-api": {"Id": "1" * 64, "Image": "sha256:" + "1" * 64, "State": {"Running": True}},
                    "uniroute-api-canary": {"Id": "2" * 64, "Image": "sha256:" + "2" * 64, "State": {"Running": True}}}
        operation = SimpleNamespace(identity=lambda: None, retained_plan=lambda: plan, evidence=Evidence(records),
                                    change_id=lambda stage: "operation-" + stage,
                                    ready=lambda role, image: {"artifact_digest": image,
                                        "instance": "uri_" + ("1" if role == "stable" else "2") * 32},
                                    target={"environment": "test", "database_system_id": "123", "ingress_directory": "/fixture"},
                                    ingress=SimpleNamespace(MOUNT="/mounted", docker_exec=lambda *_: b"config"))
        def owner(_container, command, document=None):
            if command == "inspect":
                return current
            self.assertEqual(command, "read")
            if document["change_id"] == "latest-head":
                return {"status": "applied", "plan": {"desired_config_digest": retire.rollout.digest(b"config"),
                    "membership_after": [{"instance": "uri_" + "1" * 32, "artifact_digest": "sha256:" + "1" * 64},
                                         {"instance": "uri_" + "2" * 32, "artifact_digest": "sha256:" + "2" * 64}]}}
            return {"status": "applied", "plan": {"desired_config_digest": "d" * 64,
                "membership_after": [{"instance": "uri_" + "1" * 32}]}}
        def inspect(name, missing_ok=False):
            if name == "b" * 64:
                return None
            return backends[name]
        return operation, records, owner, inspect

    def test_exact_completed_rollout_produces_bound_host_proof(self):
        operation, _, owner, inspect = self.fixture()
        with patch.object(retire.rollout, "inspect", side_effect=inspect), \
             patch.object(retire.rollout, "regular", return_value=SimpleNamespace(read_bytes=lambda: b"config")), \
             patch.object(retire.expand, "verify_recovery_process_inventory"), \
             patch.object(retire, "owner_command", side_effect=owner):
            document, carrier = retire.retirement_document(operation, "canary")
        self.assertEqual(carrier, "1" * 64)
        self.assertEqual(document["instance_id"], "uri_" + "a" * 32)
        self.assertEqual(document["expected_revision"], 3)
        self.assertRegex(document["host_proof_sha256"], r"^sha256:[a-f0-9]{64}$")

    def test_missing_stop_or_restartable_old_container_fails_closed(self):
        operation, records, owner, inspect = self.fixture()
        del records["done-stop-canary.json"]
        with self.assertRaisesRegex(RuntimeError, "stage evidence is missing"):
            retire.retirement_document(operation, "canary")
        operation, _, owner, inspect = self.fixture()
        def old_still_present(name, missing_ok=False):
            if name == "b" * 64:
                return {"Id": name, "State": {"Running": False}}
            return inspect(name, missing_ok)
        with patch.object(retire.rollout, "inspect", side_effect=old_still_present), \
             self.assertRaisesRegex(RuntimeError, "could be restarted"):
            retire.retirement_document(operation, "canary")

    def test_unaccounted_process_or_drifted_current_ingress_fails_closed(self):
        operation, _, owner, inspect = self.fixture()
        with patch.object(retire.rollout, "inspect", side_effect=inspect), \
             patch.object(retire.expand, "verify_recovery_process_inventory", side_effect=RuntimeError("unaccounted process")), \
             self.assertRaisesRegex(RuntimeError, "unaccounted process"):
            retire.retirement_document(operation, "canary")
        with patch.object(retire.rollout, "inspect", side_effect=inspect), \
             patch.object(retire.expand, "verify_recovery_process_inventory"), \
             patch.object(retire.rollout, "regular", return_value=SimpleNamespace(read_bytes=lambda: b"changed")), \
             patch.object(retire, "owner_command", side_effect=owner), \
             self.assertRaisesRegex(RuntimeError, "installed ingress differs"):
            retire.retirement_document(operation, "canary")

    def test_wrong_historical_binding_and_unapplied_head_fail_closed(self):
        operation, _, owner, inspect = self.fixture()
        operation.retained_plan()["containers"]["canary"]["image"] = "sha256:" + "0" * 64
        with patch.object(retire.rollout, "inspect", side_effect=inspect), \
             patch.object(retire.expand, "verify_recovery_process_inventory"), \
             patch.object(retire, "owner_command", side_effect=owner), \
             self.assertRaisesRegex(RuntimeError, "old runtime identity is active or differs"):
            retire.retirement_document(operation, "canary")
        operation, _, owner, inspect = self.fixture()
        def pending_head(container, command, document=None):
            result = owner(container, command, document)
            if command == "inspect":
                result = dict(result, head_status="pending")
            return result
        with patch.object(retire.rollout, "inspect", side_effect=inspect), \
             patch.object(retire.expand, "verify_recovery_process_inventory"), \
             patch.object(retire, "owner_command", side_effect=pending_head), \
             self.assertRaisesRegex(RuntimeError, "head is not applied"):
            retire.retirement_document(operation, "canary")


if __name__ == "__main__":
    unittest.main()
