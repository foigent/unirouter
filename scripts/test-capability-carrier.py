#!/usr/bin/env python3
"""Official carrier trust-boundary tests; no GitHub, SSH or Provider calls."""
import base64
import hashlib
import importlib.util
import io
import json
from pathlib import Path
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


if __name__ == "__main__":
    unittest.main()
