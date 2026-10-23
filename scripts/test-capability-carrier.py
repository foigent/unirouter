#!/usr/bin/env python3
"""Official carrier trust-boundary tests; no GitHub, SSH or Provider calls."""
import base64
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
import zipfile


def load(name, file):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(file))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


publisher = load("publisher", "publish-capability-bundle.py")
bridge = load("bridge", "compatible-expand.py")


class CarrierTrustTests(unittest.TestCase):
    def setUp(self):
        self.wire = b'{"isolated":"source provenance fixture"}'
        self.digest = hashlib.sha256(self.wire).hexdigest()
        self.commit, self.tree, self.blob = "a"*40, "b"*40, "c"*40
        self.path = "capability-bundles/glm-fixture.json"
        self.execution = {"id": 12, "status": "completed", "conclusion": "success", "event": "workflow_dispatch", "head_branch": "main", "head_sha": self.commit, "path": publisher.WORKFLOW, "repository": {"id": 7, "full_name": publisher.REPOSITORY}, "head_repository": {"id": 7}, "run_attempt": 1}
        self.provenance = {"repository": publisher.REPOSITORY, "commit": self.commit, "tree": self.tree, "path": self.path, "bundle_digest": self.digest, "workflow_run_id": 12, "run_attempt": 1}
        self.package()

    def package(self, wire=None, extra=False):
        buffer = io.BytesIO()
        with zipfile.ZipFile(buffer, "w") as output:
            output.writestr("bundle.json", self.wire if wire is None else wire)
            output.writestr("provenance.json", json.dumps(self.provenance))
            if extra:
                output.writestr("../../unapproved", b"bad")
        self.archive = buffer.getvalue()
        self.item = {"id": 34, "expired": False, "name": "capability-bundle-"+self.digest, "size_in_bytes": len(self.archive), "digest": "sha256:"+hashlib.sha256(self.archive).hexdigest(), "workflow_run": {"id": 12, "head_sha": self.commit, "repository_id": 7, "head_repository_id": 7}}

    def api(self, path, raw=False):
        return {"actions/runs/12": self.execution, "actions/artifacts/34": self.item,
                "actions/artifacts/34/zip": self.archive, f"contents/{self.path}?ref={self.commit}": {"type": "file", "sha": self.blob},
                f"git/blobs/{self.blob}": {"encoding": "base64", "size": len(self.wire), "content": base64.b64encode(self.wire).decode()},
                f"git/commits/{self.commit}": {"tree": {"sha": self.tree}}}[path]

    def verify(self):
        with patch.object(publisher, "api", self.api):
            return publisher.verify_source(12, 34, self.commit, self.path, self.digest)

    def test_exact_ci_archive_and_tracked_source_agree(self):
        wire, carrier, archive = self.verify()
        self.assertEqual(wire, self.wire)
        self.assertEqual(carrier["artifact_id"], 34)
        self.assertEqual(archive, self.item["digest"])

    def test_untrusted_run_cannot_authorize_bytes(self):
        for field, value in (("conclusion", "failure"), ("head_branch", "feature"), ("head_sha", "d"*40), ("event", "pull_request"), ("path", "other.yml"), ("head_repository", {"id": 99})):
            with self.subTest(field=field):
                before = self.execution[field]
                self.execution[field] = value
                with self.assertRaises(ValueError):
                    self.verify()
                self.execution[field] = before

    def test_archive_integrity_and_expiry_are_not_optional(self):
        for field, value in (("digest", "sha256:"+"0"*64), ("size_in_bytes", 1), ("expired", True)):
            before = self.item[field]
            self.item[field] = value
            with self.assertRaises(ValueError):
                self.verify()
            self.item[field] = before

    def test_ci_label_does_not_authorize_different_source_bytes(self):
        self.package(wire=b'{"changed":true}')
        with self.assertRaisesRegex(ValueError, "differ"):
            self.verify()

    def test_extra_archive_members_reject_without_extraction(self):
        self.package(extra=True)
        with self.assertRaisesRegex(ValueError, "members"):
            self.verify()

    def test_rerun_provenance_is_exact(self):
        self.execution["run_attempt"] = 2
        with self.assertRaisesRegex(ValueError, "provenance"):
            self.verify()

    def test_bridge_compares_running_image_support_and_debug_ports(self):
        manifest = {"schema_heads": bridge.HEADS}
        wire = json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode()
        runtime = {"support_manifest_digest": hashlib.sha256(wire).hexdigest(), "supported_schema_heads": bridge.HEADS,
                   "compatibility_epoch": "unirouter.runtime-compatibility.v1", "artifact_digest": "sha256:"+"a"*64}
        backend = {"Image": runtime["artifact_digest"], "HostConfig": {"PortBindings": {"8080/tcp": [{"HostIp": "127.0.0.1"}]}}}
        bridge.verify_bridge(runtime, backend, wire)
        backend["Image"] = "sha256:"+"b"*64
        with self.assertRaises(RuntimeError):
            bridge.verify_bridge(runtime, backend, wire)
        backend["Image"] = runtime["artifact_digest"]
        backend["HostConfig"]["PortBindings"]["8080/tcp"][0]["HostIp"] = "0.0.0.0"
        with self.assertRaisesRegex(RuntimeError, "public"):
            bridge.verify_bridge(runtime, backend, wire)

    def test_source_bridge_requires_exact_head_and_each_runtime_implementation(self):
        required = bridge.COUNTERS + [bridge.SOURCE_FACTS]
        for missing in [None, *required, "head", "future"]:
            with self.subTest(missing=missing):
                manifest = {"schema_heads": list(bridge.HEADS), "implementations": list(required)}
                if missing == "head":
                    manifest["schema_heads"] = bridge.HEADS[:3]
                elif missing == "future":
                    manifest["schema_heads"].append("000117_unapproved")
                elif missing is not None:
                    manifest["implementations"].remove(missing)
                wire = json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode()
                runtime = {"support_manifest_digest": hashlib.sha256(wire).hexdigest(),
                           "supported_schema_heads": manifest["schema_heads"],
                           "compatibility_epoch": "unirouter.runtime-compatibility.v1",
                           "artifact_digest": "sha256:"+"a"*64}
                backend = {"Image": runtime["artifact_digest"], "HostConfig": {"PortBindings": {}}}
                if missing is None:
                    bridge.verify_bridge(runtime, backend, wire, bridge.HEADS[3])
                else:
                    with self.assertRaises(RuntimeError):
                        bridge.verify_bridge(runtime, backend, wire, bridge.HEADS[3])

    def test_m119_through_m125_require_exact_prefix_and_inherited_implementations(self):
        required = (bridge.COUNTERS + [bridge.SOURCE_FACTS, bridge.SOURCE_OBSERVATIONS,
                    bridge.SOURCE_DISPATCH, *bridge.BUDGET_REQUIREMENTS, bridge.CONTENT_LIFECYCLE,
                    bridge.PRE_DISPATCH_OBSERVATIONS, *bridge.SCOPED_CONTENT,
                    *bridge.XINSIGHT_POLICY, *bridge.XINSIGHT_ADOPTION, *bridge.XINSIGHT_CREDENTIAL])
        for end in range(6, len(bridge.HEADS) + 1):
            with self.subTest(end=end):
                manifest = {"schema_heads": bridge.HEADS[:end], "implementations": required}
                wire = json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode()
                runtime = {"support_manifest_digest": hashlib.sha256(wire).hexdigest(),
                           "supported_schema_heads": manifest["schema_heads"],
                           "compatibility_epoch": "unirouter.runtime-compatibility.v1",
                           "artifact_digest": "sha256:" + "a" * 64}
                backend = {"Image": runtime["artifact_digest"], "HostConfig": {"PortBindings": {}}}
                bridge.verify_bridge(runtime, backend, wire, bridge.HEADS[end - 1])
                for missing in required:
                    reduced = dict(manifest, implementations=[ref for ref in required if ref != missing])
                    reduced_wire = json.dumps(reduced, sort_keys=True, separators=(",", ":")).encode()
                    reduced_runtime = dict(runtime, support_manifest_digest=hashlib.sha256(reduced_wire).hexdigest())
                    required_for_head = (missing in bridge.COUNTERS or missing in
                        [bridge.SOURCE_FACTS, bridge.SOURCE_OBSERVATIONS, bridge.SOURCE_DISPATCH,
                         *bridge.BUDGET_REQUIREMENTS] or
                        (end >= 7 and missing == bridge.CONTENT_LIFECYCLE) or
                        (end >= 9 and missing == bridge.PRE_DISPATCH_OBSERVATIONS) or
                        (end >= 10 and missing in bridge.SCOPED_CONTENT) or
                        (end >= 11 and missing in bridge.XINSIGHT_POLICY) or
                        (end >= 12 and missing in bridge.XINSIGHT_ADOPTION) or
                        (end >= 13 and missing in bridge.XINSIGHT_CREDENTIAL))
                    if required_for_head:
                        with self.assertRaises(RuntimeError):
                            bridge.verify_bridge(reduced_runtime, backend, reduced_wire, bridge.HEADS[end - 1])
                    else:
                        bridge.verify_bridge(reduced_runtime, backend, reduced_wire, bridge.HEADS[end - 1])
                if end < len(bridge.HEADS):
                    with self.assertRaises(RuntimeError):
                        bridge.verify_bridge(runtime, backend, wire, bridge.HEADS[end])

    def test_host_check_invokes_only_readonly_owner_action(self):
        implementations = (bridge.COUNTERS + [bridge.SOURCE_FACTS, bridge.SOURCE_OBSERVATIONS,
                           bridge.SOURCE_DISPATCH, *bridge.BUDGET_REQUIREMENTS, bridge.CONTENT_LIFECYCLE,
                           bridge.PRE_DISPATCH_OBSERVATIONS, *bridge.SCOPED_CONTENT, *bridge.XINSIGHT_POLICY,
                           *bridge.XINSIGHT_ADOPTION, *bridge.XINSIGHT_CREDENTIAL])
        manifest = {"schema_heads": bridge.HEADS, "implementations": implementations}
        wire = json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode()
        sha = "a" * 64
        image = "sha256:" + "b" * 64
        runtime = {"support_manifest_digest": hashlib.sha256(wire).hexdigest(),
                   "supported_schema_heads": bridge.HEADS,
                   "compatibility_epoch": "unirouter.runtime-compatibility.v1", "artifact_digest": image}
        backend = {"Image": image, "HostConfig": {"PortBindings": {}}}
        snapshot = {"before": {}, "configuration_sha256": sha,
                    "health": {role: runtime for role in bridge.ingress.HOSTS}}

        def host_read(command):
            if command == ["hostname"]:
                return b"isolated-fixture"
            if command[:2] == ["docker", "inspect"]:
                return json.dumps([backend]).encode()
            self.fail(f"unexpected host command: {command}")

        def container_read(host, *command):
            if command[:1] == ("sh",):
                return b""
            self.assertEqual(("./uniroute", "runtime-generation", "support"), command)
            return wire

        with tempfile.TemporaryDirectory(prefix="ur-expand-check-") as directory:
            argv = ["compatible-expand.py", "--directory", directory, "--expected-container-id", sha,
                    "--expected-hostname", "isolated-fixture", "--expected-system-identifier", "123",
                    "--environment", "test", "--migration-digest", "sha256:" + sha,
                    "--target-version", bridge.HEADS[-1], "--expected-config-sha256", sha,
                    "--expected-nginx-sha256", sha]
            owner = SimpleNamespace(returncode=0, stdout=b'{"status":"check"}')
            with patch.object(sys, "argv", argv), patch.object(Path, "open", side_effect=lambda *args, **kwargs: open(os.devnull, "a")), \
                 patch.object(bridge.fcntl, "flock"), patch.object(bridge.ingress, "run", side_effect=host_read), \
                 patch.object(bridge.ingress, "docker_exec", side_effect=container_read), \
                 patch.object(bridge.ingress, "snapshot", return_value=snapshot), \
                 patch.object(bridge.ingress, "digest", return_value=sha), \
                 patch.object(bridge, "verify_recovery_process_inventory"), \
                 patch.object(bridge.subprocess, "run", return_value=owner) as execute, \
                 patch("builtins.print"):
                bridge.main()
            self.assertEqual(1, execute.call_count)
            self.assertEqual(["docker", "exec", "-i", bridge.ingress.HOSTS["stable"],
                              "./uniroute", "compatible-expand", "check"], execute.call_args.args[0])
            self.assertEqual("test", json.loads(execute.call_args.kwargs["input"])["environment"])




class CommunityPublisherTargetTests(unittest.TestCase):
    def setUp(self):
        no_io = patch.object(publisher, "execute",
                             side_effect=AssertionError("offline test attempted external I/O"))
        no_io.start()
        self.addCleanup(no_io.stop)

    def args(self, owner="unirouter-community-backend-1", environment="production"):
        return SimpleNamespace(owner_container=owner, expected_environment=environment,
                               expected_hostname="isolated-fixture",
                               expected_owner_image="sha256:" + "c" * 64)

    def argv(self, plan, owner, environment):
        return ["publish-capability-bundle.py", "--run", "12", "--artifact", "34",
                "--commit", "a" * 40, "--path", "capability-bundles/fixture.json",
                "--digest", "b" * 64, "--publication", "community-fixture",
                "--target", "isolated-fixture", "--expected-hostname", "isolated-fixture",
                "--expected-system-identifier", "123", "--expected-environment", environment,
                "--owner-container", owner, "--expected-owner-image", "sha256:" + "c" * 64,
                "--plan", str(plan)]

    def test_existing_pairs_and_exact_community_pair(self):
        for owner in ("uniroute-api", "uniroute-api-canary"):
            for environment in ("development", "test", "integration"):
                publisher.validate_target_pair(self.args(owner, environment))
        publisher.validate_target_pair(self.args())

    def test_cross_pairs_fail_before_any_api_or_ssh(self):
        pairs = [("uniroute-api", "production"), ("uniroute-api-canary", "production")]
        pairs += [("unirouter-community-backend-1", value)
                  for value in ("development", "test", "integration")]
        with tempfile.TemporaryDirectory() as directory:
            for owner, environment in pairs:
                with self.subTest(owner=owner, environment=environment):
                    with patch.object(sys, "argv", self.argv(Path(directory) / "plan", owner, environment)), \
                            patch.object(publisher, "verify_source") as verify, \
                            patch.object(publisher, "remote") as remote:
                        with self.assertRaisesRegex(ValueError, "production"):
                            publisher.main()
                        verify.assert_not_called()
                        remote.assert_not_called()

    def test_community_requires_exact_host_image_and_compose_labels(self):
        valid = [b"isolated-fixture\n", ("sha256:" + "c" * 64 + "\n").encode(),
                 b"unirouter-community|backend\n"]
        with patch.object(publisher, "remote", side_effect=valid) as remote:
            publisher.check_target(self.args())
            commands = [call.args[1] for call in remote.call_args_list]
            self.assertEqual(commands[0], ["hostname"])
            self.assertEqual(commands[1][0:3], ["docker", "inspect", "--format"])
            self.assertIn("com.docker.compose.project", commands[2][3])
            self.assertIn("com.docker.compose.service", commands[2][3])
            self.assertNotIn(".Config.Env", commands[2][3])
        for position, value in [(0, b"other-host"), (1, b"sha256:" + b"d" * 64),
                                (2, b""), (2, b"other-project|backend"),
                                (2, b"unirouter-community|frontend")]:
            with self.subTest(position=position, value=value):
                wrong = list(valid)
                wrong[position] = value
                with patch.object(publisher, "remote", side_effect=wrong):
                    with self.assertRaises(ValueError):
                        publisher.check_target(self.args())

    def test_legacy_owner_keeps_original_two_read_checks(self):
        with patch.object(publisher, "remote",
                          side_effect=[b"isolated-fixture", ("sha256:" + "c" * 64).encode()]) as remote:
            publisher.check_target(self.args("uniroute-api", "test"))
            self.assertEqual(remote.call_count, 2)

    def test_default_plan_and_saved_apply_keep_exact_source_and_native_plan(self):
        wire = b'{"local":"fixture"}'
        carrier = {"repository": publisher.REPOSITORY, "commit": "a" * 40,
                   "tree": "d" * 40, "path": "capability-bundles/fixture.json",
                   "workflow_run_id": 12, "artifact_id": 34}
        archive = "sha256:" + "e" * 64
        plan_wire = json.dumps({"target_identity": {"system_identifier": "123",
                                                   "environment": "production"}},
                               sort_keys=True, separators=(",", ":")).encode()
        result = {"plan_bytes": base64.b64encode(plan_wire).decode(),
                  "plan_digest": hashlib.sha256(plan_wire).hexdigest()}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "plan.json"
            argv = self.argv(path, "unirouter-community-backend-1", "production")
            with patch.object(sys, "argv", argv), \
                    patch.object(publisher, "verify_source", return_value=(wire, carrier, archive)), \
                    patch.object(publisher, "owner", return_value=result) as owner, \
                    patch("sys.stdout", new=io.StringIO()):
                publisher.main()
                self.assertEqual(owner.call_args.args[1], "plan-v2")
                self.assertIsNone(owner.call_args.args[2]["plan_bytes"])
            saved = json.loads(path.read_bytes())
            self.assertEqual(saved["plan"], result)
            self.assertEqual(saved["source"]["carrier"], carrier)
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            with patch.object(sys, "argv", argv + ["--apply"]), \
                    patch.object(publisher, "verify_source", return_value=(wire, carrier, archive)), \
                    patch.object(publisher, "owner", return_value={"status": "committed"}) as owner, \
                    patch("sys.stdout", new=io.StringIO()):
                publisher.main()
                self.assertEqual(owner.call_args.args[1], "apply-v2")
                self.assertEqual(owner.call_args.args[2]["plan_digest"], result["plan_digest"])
            with patch.object(sys, "argv", argv + ["--apply"]), \
                    patch.object(publisher, "verify_source", return_value=(wire, carrier, "sha256:" + "f" * 64)), \
                    patch.object(publisher, "owner") as owner:
                with self.assertRaisesRegex(ValueError, "different verified CI bytes"):
                    publisher.main()
                owner.assert_not_called()

    def test_wrong_native_target_or_plan_digest_cannot_create_plan_file(self):
        for system, environment, bad_digest in [("999", "production", False),
                                                 ("123", "test", False),
                                                 ("123", "production", True)]:
            with self.subTest(system=system, environment=environment, bad_digest=bad_digest):
                wire = json.dumps({"target_identity": {"system_identifier": system,
                                                      "environment": environment}}).encode()
                result = {"plan_bytes": base64.b64encode(wire).decode(),
                          "plan_digest": "0" * 64 if bad_digest else hashlib.sha256(wire).hexdigest()}
                with tempfile.TemporaryDirectory() as directory:
                    path = Path(directory) / "plan.json"
                    argv = self.argv(path, "unirouter-community-backend-1", "production")
                    with patch.object(sys, "argv", argv), \
                            patch.object(publisher, "verify_source", return_value=(b"{}", {}, "sha256:" + "e" * 64)), \
                            patch.object(publisher, "owner", return_value=result) as owner:
                        with self.assertRaises(ValueError):
                            publisher.main()
                        self.assertEqual(owner.call_args.args[1], "plan-v2")
                        self.assertFalse(path.exists())

    def test_help_has_no_api_or_ssh(self):
        with patch.object(sys, "argv", ["publish-capability-bundle.py", "--help"]), \
                patch.object(publisher, "verify_source") as verify, \
                patch.object(publisher, "remote") as remote, \
                patch("sys.stdout", new=io.StringIO()):
            with self.assertRaises(SystemExit) as result:
                publisher.main()
            self.assertEqual(result.exception.code, 0)
            verify.assert_not_called()
            remote.assert_not_called()


if __name__ == "__main__":
    unittest.main()
