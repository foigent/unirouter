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

    def write(self, name, value):
        self.records[name] = value

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


class MaintenanceRetirementTests(unittest.TestCase):
    def test_only_exact_adjacent_maintenance_ledgers_are_allowed(self):
        m125 = "000125_xinsight_control_credential_lifecycle"
        m126 = "000126_commercial_license_retirement"
        m127 = "000127_xinsight_communication_policy"
        m128 = "000128_upstream_release_observation"
        for artifact in (["earlier", m125, m126], ["earlier", m125, m126, m127], ["earlier", m125, m126, m127, m128]):
            retire.maintenance_schema_predecessor(artifact, artifact[:-1])
            for actual in (artifact, artifact[:-2], artifact[:-2] + ["unknown"], artifact[1:-1]):
                with self.assertRaises(RuntimeError):
                    retire.maintenance_schema_predecessor(artifact, actual)
        for artifact in ([], [m126], [m125, m127], [m126, "future"], [m127, "future"]):
            with self.assertRaises(RuntimeError):
                retire.maintenance_schema_predecessor(artifact, artifact[:-1])

    def fixture(self):
        op = object.__new__(retire.MaintenanceRetirement)
        op.target = {"environment": "test", "database_system_id": "123"}
        op.spec = {"operation_id": "maintenance"}
        plan = {"plan_digest": "p" * 64, "pins": {},
                "old_instances": {"stable": "old-stable", "canary": "old-canary"},
                "containers": {r: {"id": r + "-container", "image": r + "-image"} for r in ("stable", "canary")}}
        op.retained_plan = lambda: plan
        op.identity = lambda: None
        op.pins = lambda: {}
        op.ingress = SimpleNamespace(membership=lambda _: b"none")
        records = {}
        def record(stage, value):
            records["start-" + stage + ".json"] = {"plan_digest": plan["plan_digest"]}
            records["done-" + stage + ".json"] = {"plan_digest": plan["plan_digest"], "result": value}
        record("withdraw-all", {"change_id": "maintenance-withdraw-all", "outcome": "old_proxy_workers_exited",
                                "after_sha256": retire.rollout.digest(b"none")})
        for role in ("stable", "canary"):
            record("stop-" + role, {"container_id": role + "-container", "exit": 0, "incomplete": False,
                                    "shutdown_events": ["Server exited"]})
            record("remove-" + role, {"container_id": role + "-container", "removed": True})
        op.evidence = Evidence(records)
        current = {"head": "maintenance-withdraw-all", "generations": [
            {"instance": "old-" + role, "artifact_digest": role + "-image", "state": "serving",
             "ingress_state": "absent", "lease_live": False, "support_manifest_digest": "m",
             "compatibility_epoch": "e", "schema_head": "M125", "revision": 3}
            for role in ("stable", "canary")]}
        op.prove_withdrawal = lambda *args: current
        return op, current, records

    def test_both_exits_are_required_without_a_replacement(self):
        op, _, records = self.fixture()
        with patch.object(retire.rollout, "inspect", return_value=None), \
             patch.object(retire.expand, "verify_recovery_process_inventory") as inventory:
            document = op.retirement_document("stable")
            inventory.assert_called_once_with({})
            self.assertEqual(document["instance_id"], "old-stable")
            self.assertEqual(document["expected_revision"], 3)
            del records["done-stop-canary.json"]
            with self.assertRaisesRegex(RuntimeError, "evidence is missing"):
                op.retirement_document("stable")

    def test_restart_live_lease_and_failed_exit_refuse_retirement(self):
        for failure in ("container", "lease", "exit", "process"):
            with self.subTest(failure=failure):
                op, current, records = self.fixture()
                if failure == "lease": current["generations"][0]["lease_live"] = True
                if failure == "exit": records["done-stop-canary.json"]["result"]["incomplete"] = True
                with patch.object(retire.rollout, "inspect", return_value={} if failure == "container" else None), \
                     patch.object(retire.expand, "verify_recovery_process_inventory",
                                  side_effect=RuntimeError("unaccounted process") if failure == "process" else None):
                    with self.assertRaises(RuntimeError): op.retirement_document("stable")

    def test_check_has_no_evidence_writes_and_unknown_apply_is_read_back(self):
        op, current, records = self.fixture()
        document = {"instance_id": "old-stable", "expected_revision": 3, "host_proof_sha256": "proof"}
        op.retirement_document = lambda _: dict(document)
        calls = []
        def owner(receiver, command, request=None):
            self.assertEqual(receiver, "")  # Both serving processes may be gone.
            calls.append(command)
            if command == "inspect": return current
            return {"instance_id": "old-stable", "host_proof_sha256": "proof",
                    "status": "eligible" if command == "stop-check" else "stopped"}
        op.ingress.owner_command = owner
        before = dict(records)
        self.assertEqual(op.retire("check", "stable")["status"], "eligible")
        self.assertEqual(records, before)
        self.assertEqual(op.retire("apply", "stable")["status"], "stopped")
        self.assertEqual(records["retirement-stable.json"], document)
        current["generations"][0]["state"] = "stopped"
        document["expected_revision"] = 4
        self.assertTrue(op.retire("apply", "stable")["readback"])
        self.assertEqual(calls.count("stop-apply"), 1)
        document["expected_revision"] = 5
        with self.assertRaisesRegex(RuntimeError, "differs from retained request"):
            op.retire("apply", "stable")

    def test_unconfirmed_stop_is_not_replayed_and_clean_poststate_is_read_back(self):
        op, _, records = self.fixture()
        plan = op.retained_plan()
        records.pop("done-stop-stable.json")
        records["start-stop-stable.json"]["at"] = "2026-10-10T00:00:00Z"
        records["command-stop-stable.json"] = {"plan_digest": plan["plan_digest"]}
        container = {"Id": "stable-container", "Config": {"StopTimeout": 30},
                     "State": {"Running": True, "ExitCode": 0, "OOMKilled": False}}
        with patch.object(retire.rollout, "inspect", return_value=container), patch.object(retire.rollout, "run") as run:
            with self.assertRaisesRegex(RuntimeError, "refuse replay"):
                op.stage("stop", "stable")
            run.assert_not_called()
            container["State"]["Running"] = False
            with patch.object(retire.rollout.subprocess, "run", return_value=SimpleNamespace(returncode=0, stdout=b"Server exited", stderr=b"")):
                self.assertEqual(op.stage("stop", "stable")["exit"], 0)
            run.assert_not_called()

    def test_completed_stop_can_be_read_after_exact_recorded_removal(self):
        op, _, records = self.fixture()
        with patch.object(retire.rollout, "inspect", return_value=None), patch.object(retire.rollout, "run") as command:
            self.assertEqual(op.stage("stop", "stable")["exit"], 0)
            command.assert_not_called()
            del records["done-remove-stable.json"]
            with self.assertRaisesRegex(RuntimeError, "evidence is missing"):
                op.stage("stop", "stable")


if __name__ == "__main__":
    unittest.main()
