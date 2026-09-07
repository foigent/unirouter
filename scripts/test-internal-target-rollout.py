#!/usr/bin/env python3
"""Bounded rollout authority/recovery regressions; no Docker or paid calls."""
import copy
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("rollout", Path(__file__).with_name("internal-target-rollout.py"))
rollout = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rollout)


def fixture():
    image = {"Id": "sha256:" + "a" * 64, "Config": {"Env": ["PATH=/bin"], "Cmd": ["./uniroute"], "WorkingDir": "/app"}}
    service = {"command": None, "environment": {"RUNTIME_ENVIRONMENT": "development", "DB_DSN": "fixture-only"},
               "ports": [{"target": 8080, "published": "8080", "host_ip": "127.0.0.1", "protocol": "tcp"}],
               "volumes": [{"type": "bind", "source": "/release/migrations", "target": "/app/migrations", "read_only": True}],
               "stop_grace_period": "35m", "networks": {"default": None}}
    container = {"Id": "old", "Image": image["Id"], "State": {"Running": False, "ExitCode": 0, "OOMKilled": False},
                 "Config": {"Env": image["Config"]["Env"] + ["RUNTIME_ENVIRONMENT=development", "DB_DSN=fixture-only"],
                            "Cmd": ["./uniroute"], "Entrypoint": None, "User": "", "WorkingDir": "/app", "StopTimeout": 2100},
                 "HostConfig": {"PortBindings": {"8080/tcp": [{"HostIp": "127.0.0.1", "HostPort": "8080"}]},
                                "RestartPolicy": {"Name": "no"}, "LogConfig": {"Type": "json-file", "Config": {}}},
                 "Mounts": [{"Type": "bind", "Source": "/release/migrations", "Destination": "/app/migrations", "RW": False}],
                 "NetworkSettings": {"Networks": {"unirouter_default": {"IPAMConfig": None}}}}
    return service, container, image


class RuntimeConfigurationTests(unittest.TestCase):
    def test_image_defaults_and_compose_null_are_checked_against_actual_runtime(self):
        rollout.check_service(*fixture(), "unirouter", "development")

    def test_environment_drift_is_rejected_without_disclosing_values(self):
        service, container, image = fixture()
        service["environment"]["DB_DSN"] = "must-not-appear-in-diagnostic"
        with self.assertRaisesRegex(RuntimeError, "environment differs") as error:
            rollout.check_service(service, container, image, "unirouter", "development")
        self.assertNotIn("must-not-appear", str(error.exception))

    def test_document_development_does_not_allow_actual_production(self):
        service, container, image = fixture()
        service["environment"]["RUNTIME_ENVIRONMENT"] = "production"
        container["Config"]["Env"][1] = "RUNTIME_ENVIRONMENT=production"
        with self.assertRaisesRegex(RuntimeError, "outside.*environment"):
            rollout.check_service(service, container, image, "unirouter", "development")

    def test_ports_mounts_and_runtime_settings_are_not_label_only_checks(self):
        for changed in ("ports", "mount", "privilege", "health"):
            with self.subTest(changed=changed):
                service, container, image = fixture()
                if changed == "ports":
                    container["HostConfig"]["PortBindings"]["8080/tcp"][0]["HostIp"] = "0.0.0.0"
                elif changed == "mount":
                    container["Mounts"][0]["RW"] = True
                elif changed == "privilege":
                    container["HostConfig"]["Privileged"] = True
                else:
                    container["Config"]["Healthcheck"] = {"Test": ["NONE"]}
                with self.assertRaises(RuntimeError):
                    rollout.check_service(service, container, image, "unirouter", "development")

    def test_host_interpolation_does_not_reach_compose(self):
        owner = object.__new__(rollout.Rollout)
        with patch.dict(os.environ, {"JWT_SECRET": "host-override", "RUNTIME_ENVIRONMENT": "production", "COMPOSE_FILE": "wrong", "DOCKER_HOST": "tcp://wrong"}):
            env = owner.compose_env({"backend": "local:backend", "frontend": "local:frontend"})
            self.assertNotIn("JWT_SECRET", env)
            self.assertNotIn("COMPOSE_FILE", env)
            self.assertNotIn("RUNTIME_ENVIRONMENT", env)
            self.assertEqual(env["DOCKER_HOST"], "unix:///var/run/docker.sock")

    def test_missing_image_refuses_before_any_stop_or_preparation(self):
        owner = object.__new__(rollout.Rollout)
        with patch.object(owner, "identity"), patch.object(owner, "images", side_effect=RuntimeError("missing local image")), \
                patch.object(rollout, "run") as command, patch.object(rollout, "inspect") as inspect, \
                self.assertRaisesRegex(RuntimeError, "missing local image"):
            owner.plan()
        command.assert_not_called()
        inspect.assert_not_called()


class SourceAuthorityTests(unittest.TestCase):
    def test_source_identity_and_dirty_state_are_checked_read_only(self):
        with tempfile.TemporaryDirectory(prefix="unirouter-source-test-") as directory:
            root = Path(directory).resolve()
            (root / "VERSION").write_text("1.2.3\n")
            source = {"directory": str(root), "commit": "a" * 40, "tree": "b" * 40, "version": "1.2.3"}
            for outputs, message in (([b"wrong"], "commit changed"),
                                     ([b"a" * 40, b"wrong"], "tree changed"),
                                     ([b"a" * 40, b"b" * 40, b" M source"], "dirty"),
                                     ([b"a" * 40, b"b" * 40, b""], None)):
                with patch.object(rollout, "run", side_effect=outputs) as command:
                    if message:
                        with self.assertRaisesRegex(RuntimeError, message):
                            rollout.check_source(source)
                    else:
                        rollout.check_source(source)
                    for call in command.call_args_list:
                        self.assertEqual(call.args[0][:3], ["git", "-C", str(root)])
                        self.assertIn(call.args[0][3], ("rev-parse", "status"))
            self.assertEqual(list(root.iterdir()), [root / "VERSION"])

    def test_copied_tool_cannot_control_another_exact_source(self):
        owner = object.__new__(rollout.Rollout)
        owner.source = {}
        owner.new_source = Path("/another-source")
        with patch.object(rollout, "check_source"), self.assertRaisesRegex(RuntimeError, "tool is outside"):
            owner.sources()

    def test_inspection_refuses_changed_source_before_reading_or_mutating_components(self):
        owner = object.__new__(rollout.Rollout)
        with patch.object(owner, "identity"), patch.object(owner, "retained_plan", return_value={}), \
                patch.object(owner, "sources", side_effect=RuntimeError("source commit changed")), \
                patch.object(rollout, "inspect") as inspect, patch.object(owner, "images") as images, \
                self.assertRaisesRegex(RuntimeError, "source commit changed"):
            owner.progress()
        images.assert_not_called()
        inspect.assert_not_called()


class RecoveryBoundaryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="unirouter-rollout-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.evidence = rollout.Evidence(self.root)
        self.service, self.container, self.image = fixture()
        self.owner = object.__new__(rollout.Rollout)
        self.owner.spec = {"images": {"backend": {"tag": "local:next", "digest": self.image["Id"]},
                                      "frontend": {"tag": "local:next-ui", "digest": self.image["Id"]}}}
        self.owner.target = {"project": "unirouter", "environment": "development"}
        self.owner.evidence = self.evidence
        self.owner.new_source = Path("/new-source")
        self.plan = {"plan_digest": "exact-plan", "containers": {"canary": {"id": "old"}}, "old_instances": {"canary": "old-instance"}}

    def test_failed_withdrawal_cannot_stop_backend(self):
        with patch.object(self.owner, "prove_withdrawal", side_effect=RuntimeError("unproven")), \
                patch.object(rollout, "run") as command, self.assertRaisesRegex(RuntimeError, "unproven"):
            self.owner.stop("canary", self.plan)
        command.assert_not_called()

    def test_lost_stop_result_checks_existing_exit_and_drain_without_second_stop(self):
        self.evidence.write("start-stop-canary.json", {"at": "2026-09-07T00:00:00Z"})
        stopped = SimpleNamespace(returncode=0, stdout=b"Server exited\n", stderr=b"")
        with patch.object(self.owner, "prove_withdrawal"), patch.object(self.owner, "ready"), \
                patch.object(rollout, "inspect", return_value=self.container), \
                patch.object(rollout.subprocess, "run", return_value=stopped) as command:
            result = self.owner.stop("canary", self.plan)
        self.assertEqual(result["container_id"], "old")
        self.assertEqual(command.call_count, 1)
        self.assertEqual(command.call_args.args[0][:2], ["docker", "logs"])

    def test_stopped_container_with_incomplete_drain_is_not_a_success(self):
        self.evidence.write("start-stop-canary.json", {"at": "2026-09-07T00:00:00Z"})
        stopped = SimpleNamespace(returncode=0, stdout=b"HTTP runtime drain incomplete: deadline\nServer exited\n", stderr=b"")
        with patch.object(self.owner, "prove_withdrawal"), patch.object(self.owner, "ready"), \
                patch.object(rollout, "inspect", return_value=self.container), \
                patch.object(rollout.subprocess, "run", return_value=stopped), self.assertRaisesRegex(RuntimeError, "drain/stop is unproven"):
            self.owner.stop("canary", self.plan)

    def test_unconfirmed_docker_stop_is_not_resent_while_daemon_may_still_run(self):
        self.container["State"]["Running"] = True
        self.evidence.write("command-stop-canary.json", {"plan_digest": "exact-plan"})
        with patch.object(self.owner, "prove_withdrawal"), patch.object(self.owner, "ready"), \
                patch.object(rollout, "inspect", return_value=self.container), patch.object(rollout, "run") as command, \
                self.assertRaisesRegex(RuntimeError, "remains unconfirmed"):
            self.owner.stop("canary", self.plan)
        command.assert_not_called()

    def test_unconfirmed_compose_create_is_not_resent_from_old_or_absent_state(self):
        self.evidence.write("command-create-canary.json", {"plan_digest": "exact-plan"})
        for observed in (self.container, None):
            with self.subTest(observed=observed is not None), patch.object(self.owner, "prove_withdrawal"), \
                    patch.object(rollout, "inspect", return_value=observed), patch.object(self.owner, "require_interrupted_create"), \
                    patch.object(self.owner, "images", return_value={"backend": self.image}), \
                    patch.object(self.owner, "config_for", return_value={"services": {"backend-canary": self.service}}), \
                    patch.object(rollout, "run") as command, self.assertRaisesRegex(RuntimeError, "remains unconfirmed"):
                self.owner.create("canary", self.plan)
            command.assert_not_called()

    def test_lost_create_result_reads_desired_container_without_recreation(self):
        self.container["Id"] = "new"
        with patch.object(self.owner, "prove_withdrawal"), patch.object(rollout, "inspect", return_value=self.container), \
                patch.object(self.owner, "images", return_value={"backend": self.image}), \
                patch.object(self.owner, "config_for", return_value={"services": {"backend-canary": self.service}}), \
                patch.object(self.owner, "ready", return_value={"instance": "new-instance"}), patch.object(rollout, "run") as command:
            self.assertEqual(self.owner.create("canary", self.plan)["container_id"], "new")
        command.assert_not_called()

    def test_create_rechecks_images_and_explicitly_disables_pull(self):
        newer = copy.deepcopy(self.container)
        newer["Id"] = "new"
        with patch.object(self.owner, "prove_withdrawal"), patch.object(rollout, "inspect", side_effect=[self.container, newer]), \
                patch.object(self.owner, "images", return_value={"backend": self.image}) as image_check, \
                patch.object(self.owner, "config_for", return_value={"services": {"backend-canary": self.service}}), \
                patch.object(self.owner, "ready", return_value={"instance": "new-instance"}), \
                patch.object(self.owner, "chain", return_value=[]), patch.object(self.owner, "compose", return_value=["docker", "compose"]), \
                patch.object(rollout, "run") as command:
            self.owner.create("canary", self.plan)
        image_check.assert_called_once()
        args = command.call_args.args[0]
        self.assertIn("--no-build", args)
        self.assertEqual(args[args.index("--pull") + 1], "never")
        self.assertEqual(args[-1], "backend-canary")

    def test_old_backend_still_running_cannot_be_recreated(self):
        self.container["State"]["Running"] = True
        with patch.object(self.owner, "prove_withdrawal"), patch.object(rollout, "inspect", return_value=self.container), \
                patch.object(self.owner, "images", return_value={"backend": self.image}), \
                patch.object(self.owner, "config_for", return_value={"services": {"backend-canary": self.service}}), \
                patch.object(rollout, "run") as command, self.assertRaisesRegex(RuntimeError, "has not stopped"):
            self.owner.create("canary", self.plan)
        command.assert_not_called()

    def test_exact_evidence_is_exclusive_and_symlink_rejected(self):
        self.evidence.write("one.json", {"value": 1})
        with self.assertRaises(FileExistsError):
            self.evidence.write("one.json", {"value": 2})
        (self.root / "alias.json").symlink_to(self.root / "one.json")
        with self.assertRaises(RuntimeError):
            self.evidence.read("alias.json")
        self.assertEqual(self.evidence.read("one.json"), {"value": 1})

    def test_interrupted_create_requires_prior_stop_and_no_running_old_container(self):
        self.evidence.write("start-create-canary.json", {})
        self.evidence.write("command-create-canary.json", {})
        with patch.object(rollout, "inspect", return_value=None), self.assertRaisesRegex(RuntimeError, "stop was not verified"):
            self.owner.require_interrupted_create("canary", self.plan)
        self.evidence.write("done-stop-canary.json", {})
        with patch.object(rollout, "inspect", return_value=None):
            self.owner.require_interrupted_create("canary", self.plan)
        self.container["State"]["Running"] = True
        with patch.object(rollout, "inspect", return_value=self.container), self.assertRaisesRegex(RuntimeError, "another name"):
            self.owner.require_interrupted_create("canary", self.plan)

    def test_missing_container_is_not_inferred_from_docker_failure(self):
        for failed in (SimpleNamespace(returncode=1, stdout=b"[]", stderr=b"Cannot connect to Docker daemon"),
                       SimpleNamespace(returncode=1, stdout=b"", stderr=b"Error: No such object: exact")):
            with patch.object(rollout.subprocess, "run", return_value=failed), self.assertRaisesRegex(RuntimeError, "absence is unproven"):
                rollout.inspect("exact", missing_ok=True)
        with patch.object(rollout.subprocess, "run", return_value=SimpleNamespace(returncode=1, stdout=b"[]", stderr=b"Error: No such object: exact")):
            self.assertIsNone(rollout.inspect("exact", missing_ok=True))

    def test_child_mutation_inherits_real_locks_if_parent_exits(self):
        with patch.object(rollout, "LIFECYCLE_LOCK", self.root / "lifecycle.lock"):
            with rollout.operation_locks({"ingress_directory": str(self.root)}):
                with patch.object(rollout.subprocess, "run", return_value=SimpleNamespace(returncode=0, stdout=b"ok")) as child:
                    rollout.run(["docker", "stop", "exact-container"])
                self.assertEqual(len(child.call_args.kwargs["pass_fds"]), 2)
                for fd in child.call_args.kwargs["pass_fds"]:
                    self.assertGreater(os.fstat(fd).st_ino, 0)
        self.assertEqual(rollout.HELD_LOCK_FDS, [])

    def test_formal_ingress_cannot_rejoin_between_withdrawal_and_stop(self):
        target = {"ingress_directory": str(self.root)}
        with patch.object(rollout, "LIFECYCLE_LOCK", self.root / "lifecycle.lock"):
            with rollout.operation_locks(target):
                args = [sys.executable, str(Path(__file__).with_name("runtime-ingress.py")), "--directory", str(self.root),
                        "--expected-container-id", "a" * 64, "--desired", "both", "--apply", "--expected-config-sha256", "b" * 64,
                        "--expected-nginx-sha256", "c" * 64]
                result = subprocess.run(args, capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("Resource temporarily unavailable", result.stderr)
            # Process/operation exit releases both locks; recovery can reacquire.
            with rollout.operation_locks(target):
                pass

    def test_real_child_keeps_lock_after_parent_closes_descriptors(self):
        target = {"ingress_directory": str(self.root)}
        with patch.object(rollout, "LIFECYCLE_LOCK", self.root / "lifecycle.lock"):
            with rollout.operation_locks(target):
                child = subprocess.Popen([sys.executable, "-c", "import sys; print('ready', flush=True); sys.stdin.readline()"],
                                         stdin=subprocess.PIPE, stdout=subprocess.PIPE, pass_fds=tuple(rollout.HELD_LOCK_FDS))
                self.assertEqual(child.stdout.readline(), b"ready\n")
            try:
                with self.assertRaises(BlockingIOError):
                    with rollout.operation_locks(target):
                        pass
            finally:
                child.communicate(b"finish\n", timeout=10)
            with rollout.operation_locks(target):
                pass

    def test_prepared_plan_and_overlay_bytes_cannot_be_substituted(self):
        self.owner.spec = {"test": "input"}
        self.evidence.write("input.json", self.owner.spec)
        old, new = b"old runtime", b"new runtime"
        plan = {"input_digest": rollout.digest(rollout.canonical(self.owner.spec)),
                "old_overlay_digest": rollout.digest(old), "new_overlay_digest": rollout.digest(new)}
        plan["plan_digest"] = rollout.digest(rollout.canonical(plan))
        self.evidence.write("plan.json", plan)
        self.evidence.write("prepared.json", {"plan_digest": plan["plan_digest"]})
        self.evidence.write("old.runtime.yml", old)
        self.evidence.write("new.runtime.yml", new)
        self.assertEqual(self.owner.retained_plan(), plan)
        (self.root / "new.runtime.yml").write_bytes(b"replacement")
        with self.assertRaisesRegex(RuntimeError, "bytes changed"):
            self.owner.retained_plan()
        (self.root / "new.runtime.yml").write_bytes(new)
        plan["input_digest"] = "wrong"
        (self.root / "plan.json").write_bytes(rollout.canonical(plan))
        with self.assertRaisesRegex(RuntimeError, "digest differs"):
            self.owner.retained_plan()

    def test_resume_rejects_configuration_drift_before_create(self):
        with patch.object(self.owner, "prove_withdrawal"), patch.object(rollout, "inspect", return_value=self.container), \
                patch.object(self.owner, "images", return_value={"backend": self.image}), \
                patch.object(self.owner, "config_for", side_effect=RuntimeError("configuration drifted")), \
                patch.object(rollout, "run") as command, self.assertRaisesRegex(RuntimeError, "drifted"):
            self.owner.create("canary", self.plan)
        command.assert_not_called()


class CompletedStateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="unirouter-complete-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.owner = rollout.Rollout({"target": {"project": "unirouter", "environment": "development", "overlay": str(self.root / "runtime.yml"),
                                                  "ingress_directory": str(self.root)}, "source": {"directory": "/new"},
                                      "evidence_directory": str(self.root), "operation_id": "operation", "images": {}})
        self.owner.target["tls_overlay"] = "/tls.yml"
        self.evidence = self.owner.evidence
        self.service, _, self.image = fixture()
        self.owner.spec["images"] = {k: {"tag": "local:new", "digest": self.image["Id"]} for k in ("backend", "frontend")}
        self.plan = {"plan_digest": "exact", "pins": {"schema": "same"}, "old_overlay_digest": rollout.digest(b"old"),
                     "new_overlay_digest": rollout.digest(b"new"), "nginx_static_digest": "static", "initial_ingress_head": "initial",
                     "containers": {r: {"id": "old-" + r, "image": self.image["Id"]} for r in rollout.SERVICES},
                     "old_instances": {r: "old-" + r for r in ("stable", "canary")}}
        self.config = {"services": {v: copy.deepcopy(self.service) for v in rollout.SERVICES.values()}}
        self.containers = {}
        for role, name in rollout.CONTAINERS.items():
            _, c, _ = fixture()
            c["Id"], c["State"]["Running"] = "new-" + role, True
            c["Config"]["Labels"] = {"com.docker.compose.project.config_files": ",".join(self.owner.chain(self.owner.new_source))}
            self.containers[name] = c
        self.evidence.write("apply-started.json", {"plan_digest": "exact"})
        for stage in rollout.STAGES:
            role = stage.removeprefix("create-")
            result = {"container_id": "new-" + role, "instance": "new-" + role, "image": self.image["Id"]} if stage.startswith("create-") else {}
            self.evidence.write("start-" + stage + ".json", {"plan_digest": "exact"})
            self.evidence.write("done-" + stage + ".json", {"plan_digest": "exact", "result": result})
        (self.root / "runtime.yml").write_bytes(b"new")
        self.wire = self.owner.ingress.membership("both")
        (self.root / "upstreams.conf").write_bytes(self.wire)
        self.members = sorted([{"instance": "new-" + r, "artifact_digest": self.image["Id"]} for r in ("stable", "canary")], key=lambda m: m["instance"])
        self.state = {"head": "operation-refresh-frontend", "head_status": "applied",
                      "generations": [dict(m, ingress_state="receiving") for m in self.members]}
        self.receipt = {"status": "applied", "plan": {"change_id": self.state["head"], "predecessor_id": "operation-rejoin-stable",
                        "desired_config_digest": rollout.digest(self.wire), "desired_config_bytes": rollout.base64.b64encode(self.wire).decode(),
                        "expected_config_digest": rollout.digest(self.wire), "membership_after": self.members, "membership_before": self.members}}
        patches = [patch.object(self.owner, "identity"), patch.object(self.owner, "retained_plan", return_value=self.plan),
                   patch.object(self.owner, "sources"), patch.object(self.owner, "images"),
                   patch.object(self.owner, "pins", return_value=self.plan["pins"]),
                   patch.object(self.owner, "ingress_static_digest", return_value="static"), patch.object(self.owner, "owner", return_value="exact"),
                   patch.object(self.owner, "config_for", return_value=self.config),
                   patch.object(self.owner, "ready", side_effect=lambda role, image: {"instance": "new-" + role}),
                   patch.object(self.owner.ingress, "owner_command", side_effect=lambda owner, action, *args: self.state if action == "inspect" else self.receipt),
                   patch.object(self.owner.ingress, "docker_exec", side_effect=lambda *args: (self.root / "upstreams.conf").read_bytes()),
                   patch.object(rollout, "inspect", side_effect=lambda name, **kw: self.containers[name]),
                   patch.object(rollout, "run", return_value=rollout.canonical([self.image]))]
        for p in patches:
            p.start()
            self.addCleanup(p.stop)

    def test_complete_inspection_checks_current_authority_without_writes(self):
        before = {p.name: p.read_bytes() for p in self.root.iterdir()}
        self.assertEqual(self.owner.progress()[1]["deployment"], "complete")
        self.assertEqual({p.name: p.read_bytes() for p in self.root.iterdir()}, before)

    def test_old_overlay_cannot_hide_behind_completed_stages(self):
        (self.root / "runtime.yml").write_bytes(b"old")
        with self.assertRaisesRegex(RuntimeError, "overlay differs"):
            self.owner.progress()

    def test_historical_or_pending_ingress_cannot_hide_behind_completed_stages(self):
        for head, status in (("initial", "applied"), ("operation-rejoin-stable", "applied"), ("operation-refresh-frontend", "pending")):
            self.state.update(head=head, head_status=status)
            with self.subTest(head=head, status=status), self.assertRaisesRegex(RuntimeError, "ingress (head|status) differs"):
                self.owner.progress()

    def test_applied_receipt_does_not_replace_actual_carrier_or_database_membership(self):
        (self.root / "upstreams.conf").write_bytes(self.owner.ingress.membership("stable"))
        with self.assertRaisesRegex(RuntimeError, "installed membership differs"):
            self.owner.progress()
        (self.root / "upstreams.conf").write_bytes(self.wire)
        self.state["generations"] = self.state["generations"][:1]
        with self.assertRaisesRegex(RuntimeError, "database receivers differ"):
            self.owner.progress()

    def test_changed_receipt_and_restarted_process_refuse_complete(self):
        self.receipt["plan"]["predecessor_id"] = "unrelated"
        with self.assertRaisesRegex(RuntimeError, "predecessor differs"):
            self.owner.progress()
        self.receipt["plan"]["predecessor_id"] = "operation-rejoin-stable"
        with patch.object(self.owner, "ready", return_value={"instance": "unplanned-restart"}), self.assertRaisesRegex(RuntimeError, "process changed"):
            self.owner.progress()

    def test_only_current_pending_transition_allows_its_two_carrier_states(self):
        before_members = sorted([{"instance": "old-stable", "artifact_digest": self.image["Id"]},
                                 {"instance": "new-canary", "artifact_digest": self.image["Id"]}], key=lambda m: m["instance"])
        after_members = [m for m in before_members if m["instance"] == "new-canary"]
        after = self.owner.ingress.membership("canary")
        self.state.update(head="operation-withdraw-stable", head_status="pending", generations=[dict(m, ingress_state="receiving") for m in before_members])
        self.receipt = {"status": "pending", "plan": {"change_id": self.state["head"], "predecessor_id": "operation-rejoin-canary",
                        "desired_config_digest": rollout.digest(after), "desired_config_bytes": rollout.base64.b64encode(after).decode(),
                        "expected_config_digest": rollout.digest(self.wire), "membership_after": after_members, "membership_before": before_members}}
        completed = list(rollout.STAGES[:rollout.STAGES.index("withdraw-stable")])
        for wire in (self.wire, after):
            (self.root / "upstreams.conf").write_bytes(wire)
            self.owner.check_ingress_stage(self.plan, completed)
        self.state.update(head_status="applied", generations=[dict(m, ingress_state="receiving") for m in after_members])
        self.receipt["status"] = "applied"
        self.owner.check_ingress_stage(self.plan, completed)
        (self.root / "upstreams.conf").write_bytes(self.wire)
        with self.assertRaisesRegex(RuntimeError, "installed membership differs"):
            self.owner.check_ingress_stage(self.plan, completed)


class ObserverEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="unirouter-observer-test-")
        self.addCleanup(self.temp.cleanup)
        self.evidence = rollout.Evidence(Path(self.temp.name).resolve())

    def window(self, number, failed=False, end=True):
        name = f"observer-{number}"
        self.evidence.write(name + "-start.json", {"at": "start"})
        rows = [{"at": "start", "completed_at": "end", "protocol": protocol, "exit": 16 if failed else 0,
                 "result": "000 2" if failed else "401 2" if protocol == "--http2" else "401 1.1", "ok": not failed}
                for protocol in ("--http1.1", "--http2")]
        self.evidence.write(name + "-samples.jsonl", b"".join(rollout.canonical(r) + b"\n" for r in rows))
        if end:
            self.evidence.write(name + "-end.json", {"at": "end", "samples": 2, "failed": failed, "error": None})

    def test_clean_observation_only_proves_short_requests(self):
        self.window(1)
        self.assertEqual(rollout.observation_acceptance(self.evidence), "passed_short_requests_only")

    def test_crash_without_end_cannot_be_repaired_by_green_resume(self):
        self.window(1, end=False)
        self.window(2)
        self.assertEqual(rollout.observation_acceptance(self.evidence), "unproven_gap")

    def test_prior_failed_sample_survives_green_resume(self):
        self.window(1, failed=True)
        self.window(2)
        self.assertEqual(rollout.observation_acceptance(self.evidence), "failed")

    def test_crash_before_any_sample_is_an_observation_gap(self):
        self.evidence.write("observer-1-start.json", {"at": "start"})
        self.assertEqual(rollout.observation_acceptance(self.evidence), "unproven_gap")

    def test_truncated_sample_or_lost_end_remains_unproven(self):
        self.window(1)
        path = self.evidence.root / "observer-1-samples.jsonl"
        path.write_bytes(path.read_bytes()[:-2])
        self.assertEqual(rollout.observation_acceptance(self.evidence), "unproven_gap")

    def test_successful_samples_do_not_erase_failed_deployment_attempt(self):
        self.window(1)
        self.evidence.write("observer-1-operation-failed.json", {"error_class": "RuntimeError"})
        self.window(2)
        self.assertEqual(rollout.observation_acceptance(self.evidence), "failed")


class PinnedObserverTests(unittest.TestCase):
    CLIENT = {"container_id": "d" * 64, "image_digest": "sha256:" + "e" * 64,
              "curl_version": "curl 8.19.0 (fixture) libcurl/8.19.0\nProtocols: http https\nFeatures: HTTP2 SSL",
              "vantage": "pinned_ingress_container_public_https"}

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="unirouter-pinned-observer-")
        self.addCleanup(self.temp.cleanup)
        self.evidence = rollout.Evidence(Path(self.temp.name).resolve())
        self.observer = rollout.Observer(self.evidence, "https://8.130.18.85", self.CLIENT["container_id"])
        self.observer.client = dict(self.CLIENT)

    def sample(self, body=b'{"error":{"type":"authentication_error"}}', metadata=b"401 2 1", exit=0, protocol="--http2"):
        response = SimpleNamespace(returncode=exit, stdout=body + b"\n" + metadata, stderr=b"sensitive error not retained")
        with patch.object(rollout.subprocess, "run", return_value=response) as command:
            row = self.observer.sample(protocol)
        return row, command

    def write_window(self, row=None):
        self.evidence.write("observer-1-start.json", {"at": "start", "contract_version": rollout.OBSERVER_CONTRACT,
                                                     "origin": self.observer.origin, "client": self.CLIENT})
        h2 = row or self.sample()[0]
        h1 = self.sample(metadata=b"401 1.1 1", protocol="--http1.1")[0]
        self.evidence.write("observer-1-samples.jsonl", b"".join(rollout.canonical(r) + b"\n" for r in [h1, h2]))
        self.evidence.write("observer-1-end.json", {"at": "end", "samples": 2, "failed": not h2["ok"], "error": None})

    def test_existing_exact_container_and_config_free_credential_free_command(self):
        row, command = self.sample(metadata=b"401 2 2")
        argv = command.call_args.args[0]
        self.assertEqual(argv[:5], ["docker", "exec", self.CLIENT["container_id"], "curl", "-q"])
        self.assertIn("--http2-prior-knowledge", argv)
        self.assertEqual(argv[argv.index("--retry") + 1], "0")
        self.assertEqual(argv[argv.index("--noproxy") + 1], "*")
        self.assertEqual(argv[argv.index("--max-filesize") + 1], "65536")
        self.assertEqual(argv[-1], "https://8.130.18.85/v1/chat/completions")
        self.assertEqual(argv[argv.index("-H") + 1], "Content-Type: application/json")
        self.assertFalse(any(v in argv for v in ("-k", "-L", "--netrc", "--retry-all-errors", "-K", "--config")))
        self.assertNotIn("shell", command.call_args.kwargs)
        self.assertEqual(row["num_connects"], 2)
        self.assertTrue(row["ok"])
        self.assertNotIn("sensitive", str(row))
        self.assertEqual(row["client_digest"], rollout.digest(rollout.canonical(self.CLIENT)))

    def test_failures_are_not_reinterpreted_as_success(self):
        for kwargs in ({"metadata": b"401 2 1", "exit": 92}, {"metadata": b"000 0 1", "exit": 16},
                       {"metadata": b"401 1.1 1"}, {"metadata": b"503 2 1"},
                       {"body": b'{"error":{"message":"truncated"}'}, {"body": b'{"error":{}}'},
                       {"body": b'{"choices":[]}'}, {"body": b'{"error":{},"error":{"type":"x"}}'},
                       {"metadata": b"401 2 0"}, {"metadata": b"401 2 x"}, {"metadata": b"401 2"},
                       {"body": b'{"error":{"message":"' + b"x" * 65536 + b'"}}'}):
            with self.subTest(kwargs={k: str(v)[:70] for k, v in kwargs.items()}):
                self.assertFalse(self.sample(**kwargs)[0]["ok"])

    def test_client_preflight_refuses_old_or_unsupported_versions(self):
        for version in ("curl 7.81.0 (fixture)\nProtocols: https\nFeatures: HTTP2", "not curl",
                        "curl 8.19.0 (fixture)\nProtocols: https\nFeatures: SSL",
                        "curl 8.19.0 (fixture)\nProtocols: http\nFeatures: HTTP2"):
            with self.subTest(version=version), self.assertRaises(RuntimeError):
                rollout.qualified_observer_client(dict(self.CLIENT, curl_version=version))

    def test_unavailable_container_refuses_before_thread_or_receipt(self):
        with patch.object(rollout, "inspect", return_value={"Id": self.CLIENT["container_id"], "State": {"Running": False}}), \
                patch.object(self.observer.thread, "start") as start, self.assertRaises(RuntimeError):
            self.observer.start()
        start.assert_not_called()
        self.assertFalse(self.evidence.has("observer-1-start.json"))

    def test_named_container_or_url_credentials_are_never_accepted(self):
        for origin, identity in (("https://8.130.18.85", "uniroute-nginx"), ("http://8.130.18.85", "d" * 64),
                                 ("https://secret@8.130.18.85", "d" * 64), ("https://8.130.18.85/path", "d" * 64)):
            with self.subTest(origin=origin), self.assertRaises(RuntimeError):
                rollout.Observer(self.evidence, origin, identity)

    def test_client_image_drift_makes_end_unproven(self):
        self.write_window()
        # Use a fresh actual window name; do not overwrite immutable fixtures.
        self.observer.name = "observer-2"
        with patch.object(self.observer.thread, "join"), patch.object(self.observer.thread, "is_alive", return_value=False), \
                patch.object(self.observer, "read_client", return_value=dict(self.CLIENT, image_digest="sha256:" + "f" * 64)):
            self.observer.stop()
        self.assertEqual(self.evidence.read("observer-2-end.json")["error"], "RuntimeError")

    def test_new_proof_is_strict_but_keeps_legacy_failure(self):
        self.write_window()
        self.assertEqual(rollout.observation_acceptance(self.evidence), "passed_short_requests_only")
        self.evidence.write("observer-2-start.json", {"at": "legacy"})
        bad = {"at": "start", "completed_at": "end", "protocol": "--http2", "exit": 16, "result": "000 0", "ok": False}
        self.evidence.write("observer-2-samples.jsonl", rollout.canonical(bad) + b"\n" + rollout.canonical(bad) + b"\n")
        self.evidence.write("observer-2-end.json", {"at": "end", "samples": 2, "failed": True, "error": None})
        self.assertEqual(rollout.observation_acceptance(self.evidence), "failed")

    def test_new_window_requires_both_protocols_in_order(self):
        self.write_window()
        path = self.evidence.root / "observer-1-samples.jsonl"
        rows = [rollout.load(line) for line in path.read_bytes().splitlines()]
        path.write_bytes(rollout.canonical(rows[1]) + b"\n" + rollout.canonical(rows[1]) + b"\n")
        self.assertEqual(rollout.observation_acceptance(self.evidence), "unproven_gap")

    def test_missing_or_unknown_version_cannot_downgrade_new_proof(self):
        for mutation in ("remove_version", "unknown_version", "legacy_start", "wrong_client", "false_verdict"):
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory(prefix="unirouter-proof-negative-") as directory:
                old = self.evidence
                self.evidence = rollout.Evidence(Path(directory).resolve())
                self.write_window()
                if mutation in ("remove_version", "unknown_version", "legacy_start"):
                    path = self.evidence.root / "observer-1-start.json"
                    value = rollout.load(path.read_bytes())
                    if mutation == "remove_version":
                        del value["contract_version"]
                    elif mutation == "unknown_version":
                        value["contract_version"] = "future"
                    else:
                        value = {"at": value["at"]}
                    path.write_bytes(rollout.canonical(value))
                else:
                    path = self.evidence.root / "observer-1-samples.jsonl"
                    rows = [rollout.load(line) for line in path.read_bytes().splitlines()]
                    if mutation == "wrong_client":
                        rows[0]["client_digest"] = "0" * 64
                    else:
                        rows[0]["exit"] = 92
                    path.write_bytes(b"".join(rollout.canonical(r) + b"\n" for r in rows))
                self.assertEqual(rollout.observation_acceptance(self.evidence), "unproven_gap")
                self.evidence = old


if __name__ == "__main__":
    unittest.main()
