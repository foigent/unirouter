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

    def test_m119_through_m124_require_exact_prefix_and_inherited_implementations(self):
        required = (bridge.COUNTERS + [bridge.SOURCE_FACTS, bridge.SOURCE_OBSERVATIONS,
                    bridge.SOURCE_DISPATCH, *bridge.BUDGET_REQUIREMENTS, bridge.CONTENT_LIFECYCLE,
                    bridge.PRE_DISPATCH_OBSERVATIONS, *bridge.SCOPED_CONTENT,
                    *bridge.XINSIGHT_POLICY, *bridge.XINSIGHT_ADOPTION])
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
                        (end >= 12 and missing in bridge.XINSIGHT_ADOPTION))
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
                           bridge.PRE_DISPATCH_OBSERVATIONS, *bridge.SCOPED_CONTENT, *bridge.XINSIGHT_POLICY, *bridge.XINSIGHT_ADOPTION])
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


if __name__ == "__main__":
    unittest.main()
