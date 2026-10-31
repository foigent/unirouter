#!/usr/bin/env python3
"""Offline orchestration tests. Docker calls below are simulated, never executed."""
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("community_install", ROOT / "deploy/customer/install.py")
M = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(M)


class Fixture:
    def __init__(self, root):
        self.root = root
        self.release = root / "release"
        self.release.mkdir()
        self.images = {name: "registry.example.invalid/" + name + "@sha256:" + str(index) * 64
                       for index, name in enumerate(M.SERVICES, 1)}
        payloads = {
            M.DESCRIPTOR: json.dumps({"schema_version": "unirouter.install.v1", "images": self.images}).encode(),
            M.PREFIX + "config/prometheus.yml": b"global: {}\n",
            M.PREFIX + "config/grafana/provisioning/datasources/default.yml": b"apiVersion: 1\n",
            M.PREFIX + "config/grafana/dashboards/default.json": b"{}\n",
        }
        for name in ("install.py", "docker-compose.yml", ".env.example", "preflight.sh"):
            payloads[M.PREFIX + name] = (ROOT / "deploy/customer" / name).read_bytes()
        self.manifest = {"schema_version": "unirouter.upgrade-manifest.v2", "expires_at": "never", "version": "1.10.0", "commit": "a" * 40, "target_schema": M.SCHEMA,
                         "release_sequence": 1, "artifacts": []}
        for name, data in payloads.items():
            target = self.release / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
            self.manifest["artifacts"].append({"path": name, "size": len(data), "sha256": hashlib.sha256(data).hexdigest()})
        self.package = self.release / "release.tar.gz"
        with tarfile.open(self.package, "w:gz") as archive:
            wire = json.dumps(self.manifest).encode()
            self.manifest_sha = hashlib.sha256(wire).hexdigest()
            entry = tarfile.TarInfo("manifest.json")
            entry.size = len(wire)
            archive.addfile(entry, io.BytesIO(wire))
        self.verifier = root / "synthetic-verifier"
        self.verifier.write_text("test fixture only")
        self.keys = root / "synthetic-public-ring.json"
        self.keys.write_text("{}")

    def verify(self, args, **kwargs):
        # Real signature interoperability is separately tested by the Go CLI test.
        assert args[1] == "verify"
        return "verified=true\nmanifest_sha256=" + self.manifest_sha + "\npackage_sha256=" + M.digest(self.package) + "\n" + "\n".join(
            key + "=" + str(self.manifest[key]) for key in ("version", "commit", "target_schema", "release_sequence"))

    def load(self, run=None):
        return M.Release(self.release, self.package, self.verifier, self.keys, "amd64", run or self.verify)


class DockerFixture:
    def __init__(self, release, directory):
        self.release, self.directory = release, directory
        self.commands = []
        self.state = "unclaimed"
        self.recorded = False
        self.rotations = 0
        self.fail = None
        self.old_project = False
        self.foreign_installation = False
        self.networks = []
        self.exact_resources = {kind: {} for kind in ("container", "volume", "network")}
        self.before_claim = False
        self.wrong_compose_image = False
        self.wrong_compose_volume = False
        self.image_ids = {name: "sha256:" + str(index) * 64 for index, name in enumerate(M.SERVICES, 1)}

    def run(self, args, *, label, input=None):
        args = [str(a) for a in args]
        self.commands.append((label, args, input))
        if label == "Docker Compose v2 prerequisite":
            return "2.38.2"
        if label == "local Docker daemon prerequisite":
            return json.dumps({"OSType": "linux", "ID": "fixture-daemon"})
        if label == "inspect installation resources":
            return "old-container" if self.old_project and "ps" in args else ""
        if label.startswith("inspect allocated "):
            return "\n".join(self.exact_resources[args[3]])
        if label == "inspect resource identity":
            if args[-1] in self.exact_resources[args[3]]:
                labels = self.exact_resources[args[3]][args[-1]]
                return json.dumps([{"Id": args[-1], "Name": args[-1], "Labels": labels,
                                    "Config": {"Labels": labels}, "Image": self.image_ids["postgres"]}])
            return json.dumps([{"Config": {"Labels": {M.LABEL: "foreign", "com.docker.compose.service": "postgres"}},
                                "Image": self.image_ids["postgres"]}])
        if label == "inspect network allocation":
            return "\n".join(str(n) for n in range(len(self.networks)))
        if label == "inspect network subnet":
            return json.dumps([{"IPAM": {"Config": [{"Subnet": self.networks[int(args[-1])]}]}}])
        if label == "verify selected image":
            service = next(name for name, value in self.release.images.items() if value == args[-1])
            return json.dumps([{"Id": self.image_ids[service], "Os": "linux", "Architecture": "amd64", "RepoDigests": [args[-1]]}])
        if label == "verify effective Compose identities":
            services = {name: {"image": ref} for name, ref in self.release.images.items()}
            services["backend"]["environment"] = {"RUNTIME_IMAGE_DIGEST": self.image_ids["backend"]}
            if self.wrong_compose_image:
                services["backend"]["image"] = "other.example.invalid/backend@sha256:" + "9" * 64
            volumes = {name: {"name": "fixture-ur_" + name} for name in M.VOLUMES}
            if self.wrong_compose_volume:
                volumes["postgres_data"]["name"] = "unrelated_database"
            return json.dumps({"services": services, "volumes": volumes,
                               "networks": {"default": {"name": "fixture-ur_default"}}})
        if label == "fresh-plan":
            (self.directory / "evidence/install.json").write_text("original-plan")
        if label in ("check original fresh plan", "verify completed fresh owner"):
            return "fresh_install_check=PASS state=" + self.state + " ledger_entries=126 writes=0"
        if label == "fresh-apply":
            evidence = self.directory / "evidence/publications"
            if "apply" in args:
                evidence.mkdir(mode=0o700)  # Mirrors prepareFreshEvidence before DB claim.
            else:
                assert evidence.is_dir()
            if self.before_claim:
                raise M.InstallError("synthetic interruption before claim")
            self.state = "completed"  # Can fail after committing, before response.
        if label == "record-release":
            self.recorded = True
        if label in ("read initial release registration", "verify initial release registration"):
            if not self.recorded:
                return "release_sequence=0\ncurrent_schema=" + M.SCHEMA
            return "\n".join(("release_sequence=1", "version=1.10.0", "commit=" + "a" * 40,
                              "package_sha256=" + self.release.sha, "recorded_target_schema=" + M.SCHEMA,
                              "current_schema=" + M.SCHEMA))
        if label == "rotate-administrator":
            self.rotations += 1
        if label == self.fail:
            raise M.InstallError("synthetic lost response")
        return ""


class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="ur-installer-offline-")
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name).resolve()
        self.fixture = Fixture(self.root)
        self.release = self.fixture.load()
        self.directory = self.root / "instance"
        self.fake = DockerFixture(self.release, self.directory)
        self.installer = M.Installer(self.release, self.directory, "fixture-ur", 65283,
                                     Path("/var/run/docker.sock"), self.fake.run)

    def check(self, resume=False):
        with patch.object(M.platform, "system", return_value="Linux"), patch.object(M.os, "geteuid", return_value=0):
            return self.installer.check(resume=resume)

    def prepare(self):
        self.check()
        self.installer.prepare()

    def test_check_is_read_only(self):
        before = set(self.root.rglob("*"))
        with patch.object(M.secrets, "token_hex", side_effect=AssertionError("secret generated during check")):
            self.check()
        self.assertEqual(before, set(self.root.rglob("*")))
        self.assertFalse(any(label.startswith(("pull-", "start-", "fresh-")) for label, _, _ in self.fake.commands))

    def test_changed_prepared_file_rejected(self):
        (self.fixture.release / M.PREFIX / "preflight.sh").write_text("altered")
        with self.assertRaisesRegex(M.InstallError, "differs from signed"):
            self.fixture.load()

    def test_verification_failure_precedes_docker_or_writes(self):
        def reject(*args, **kwargs):
            raise M.InstallError("signature invalid")
        with self.assertRaisesRegex(M.InstallError, "signature invalid"):
            self.fixture.load(reject)
        self.assertFalse(self.directory.exists())

    def test_same_release_identity_cannot_substitute_a_different_manifest(self):
        def wrong_manifest(args, **kwargs):
            return self.fixture.verify(args, **kwargs).replace(self.fixture.manifest_sha, "0" * 64)
        with self.assertRaisesRegex(M.InstallError, "differs from the verified manifest"):
            self.fixture.load(wrong_manifest)
        self.assertFalse(self.directory.exists())

    def test_prepared_payload_changed_between_verification_and_copy_is_rejected(self):
        self.check()
        (self.fixture.release / M.PREFIX / "preflight.sh").write_text("altered after verification")
        with self.assertRaisesRegex(M.InstallError, "changed while copying"):
            self.installer.prepare()
        self.assertFalse(any(label.startswith(("pull-", "start-", "fresh-")) for label, _, _ in self.fake.commands))

    def test_descriptor_changed_after_initial_hash_is_not_used(self):
        original = M.Release.read_declared_metadata
        def replace_before_read(release, name):
            if name == M.DESCRIPTOR:
                path = release.files[name]
                descriptor = json.loads(path.read_bytes())
                descriptor["images"]["backend"] = "other.example.invalid/backend@sha256:" + "9" * 64
                path.write_text(json.dumps(descriptor))
            return original(release, name)
        with patch.object(M.Release, "read_declared_metadata", replace_before_read):
            with self.assertRaisesRegex(M.InstallError, "metadata changed after verification"):
                self.fixture.load()
        self.assertFalse(self.directory.exists())

    def test_payload_symlink_rejected(self):
        path = self.fixture.release / M.PREFIX / "preflight.sh"
        backup = self.root / "original-preflight"
        path.rename(backup)
        path.symlink_to(backup)
        with self.assertRaisesRegex(M.InstallError, "Symlinks"):
            self.fixture.load()

    def test_duplicate_json_and_traversal_rejected(self):
        with self.assertRaisesRegex(M.InstallError, "Duplicate"):
            M.strict_json('{"a":1,"a":2}')
        for name in ("../outside", "/absolute", "payload/../outside", "payload//outside", "payload\\outside"):
            with self.subTest(name=name), self.assertRaises(M.InstallError):
                M.safe_child(self.root, name)

    def test_existing_project_or_subnet_is_never_adopted(self):
        self.fake.old_project = True
        with self.assertRaisesRegex(M.InstallError, "already exists"):
            self.check()
        self.fake.old_project = False
        self.fake.networks = ["172.30.0.0/16"]
        with self.assertRaisesRegex(M.InstallError, "overlaps"):
            self.check()
        self.assertFalse(self.directory.exists())

    def test_exact_name_collisions_are_rejected_independently_of_labels(self):
        for kind, name in (("volume", "fixture-ur_postgres_data"), ("network", "fixture-ur_default"),
                           ("container", "fixture-ur-postgres-1")):
            for labels in ({}, {"com.docker.compose.project": "unrelated"}):
                with self.subTest(kind=kind, labels=labels):
                    self.fake.exact_resources[kind][name] = labels
                    with self.assertRaisesRegex(M.InstallError, "already exists"):
                        self.check()
                    self.fake.exact_resources[kind].clear()
        self.assertFalse(self.directory.exists())
        self.assertFalse(any(label.startswith(("pull-", "start-", "fresh-")) for label, _, _ in self.fake.commands))

    def test_resume_rejects_unlabelled_exact_volume(self):
        self.prepare()
        self.fake.exact_resources["volume"]["fixture-ur_postgres_data"] = {}
        with self.assertRaisesRegex(M.InstallError, "another installation"):
            self.check(resume=True)

    def test_descriptor_and_effective_compose_must_agree_before_services(self):
        self.prepare()
        self.fake.wrong_compose_image = True
        with patch("sys.stdout", io.StringIO()), self.assertRaisesRegex(M.InstallError, "image differs"):
            self.installer.install()
        self.fake.wrong_compose_image = False
        self.fake.wrong_compose_volume = True
        with patch("sys.stdout", io.StringIO()), self.assertRaisesRegex(M.InstallError, "resource names differ"):
            self.installer.install()
        self.assertFalse(any(label.startswith(("start-", "fresh-")) for label, _, _ in self.fake.commands))

    def test_config_is_private_and_independent(self):
        self.prepare()
        env = M.fields("\n".join(line for line in (self.directory / "runtime/.env").read_text().splitlines() if not line.startswith("#")))
        keys = ("POSTGRES_PASSWORD", "REDIS_PASSWORD", "JWT_SECRET", "API_KEY_HASH_SECRET", "ENCRYPTION_KEY",
                "UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET", "GRAFANA_ADMIN_PASSWORD")
        self.assertEqual(len(keys), len({env[k] for k in keys}))
        self.assertEqual(32, len(env["ENCRYPTION_KEY"]))
        self.assertEqual("production", env["RUNTIME_ENVIRONMENT"])
        self.assertEqual("127.0.0.1", env["UNIROUTER_LISTEN_ADDRESS"])
        self.assertEqual(0o600, (self.directory / "runtime/.env").stat().st_mode & 0o777)
        self.assertEqual(0o644, (self.directory / "runtime/config/prometheus.yml").stat().st_mode & 0o777)
        self.assertEqual(0o755, (self.directory / "runtime/config").stat().st_mode & 0o777)

    def test_success_orders_authority_before_http_without_login(self):
        self.prepare()
        output = io.StringIO()
        with patch("sys.stdout", output):
            self.installer.install()
        labels = [label for label, _, _ in self.fake.commands]
        self.assertLess(labels.index("fresh-apply"), labels.index("record-release"))
        self.assertLess(labels.index("record-release"), labels.index("rotate-administrator"))
        self.assertLess(labels.index("rotate-administrator"), labels.index("start-console"))
        self.assertEqual(1, self.fake.rotations)
        password = (self.directory / "admin-password").read_text().strip()
        self.assertNotIn(password, output.getvalue())
        for _, args, _ in self.fake.commands:
            self.assertNotIn(password, " ".join(args))
            self.assertNotIn("login", args)
        image_config = json.loads((self.directory / "runtime/runtime-image.json").read_text())
        self.assertEqual(self.fake.image_ids["backend"], image_config["services"]["backend"]["environment"]["RUNTIME_IMAGE_DIGEST"])

    def test_completed_resume_does_not_restart_services_or_rotate_again(self):
        self.prepare()
        with patch("sys.stdout", io.StringIO()):
            self.installer.install()
            self.installer.load_state()
            self.installer.install()
        labels = [label for label, _, _ in self.fake.commands]
        self.assertEqual(1, self.fake.rotations)
        self.assertEqual(1, labels.count("start-console"))
        self.assertEqual(1, labels.count("start-databases"))

    def test_fresh_resume_refuses_upgrade_witness_before_service_commands(self):
        self.prepare()
        self.installer.state["upgrade"] = {"format": "unirouter.community-upgrade.v1", "pending": "withdraw"}
        M.save_state(self.directory, self.installer.state)
        before = list(self.fake.commands)
        with self.assertRaisesRegex(M.InstallError, "upgrade witness"):
            self.installer.load_state()
        self.assertEqual(before, self.fake.commands)


    def test_rootless_daemon_is_rejected_before_preparation(self):
        original = self.installer.run
        def rootless(args, *, label, input=None):
            if label == "local Docker daemon prerequisite":
                return json.dumps({"ID": "fixture-daemon", "OSType": "linux", "SecurityOptions": ["name=rootless"]})
            return original(args, label=label, input=input)
        self.installer.run = rootless
        with self.assertRaisesRegex(M.InstallError, "rootful"):
            self.check()
        self.assertFalse(self.directory.exists())

    def test_unknown_password_result_stops_without_replay_or_http(self):
        self.prepare()
        self.fake.fail = "rotate-administrator"
        with self.assertRaisesRegex(M.InstallError, "lost response"):
            self.installer.install()
        self.fake.fail = None
        self.installer.load_state()
        with self.assertRaisesRegex(M.InstallError, "uncertain result"):
            self.installer.install()
        self.assertEqual(1, self.fake.rotations)
        self.assertNotIn("start-console", [label for label, _, _ in self.fake.commands])

    def test_unknown_release_record_is_reconciled_without_duplicate(self):
        self.prepare()
        original = (self.directory / "runtime/.env").read_bytes()
        self.fake.fail = "record-release"
        with self.assertRaises(M.InstallError):
            self.installer.install()
        self.fake.fail = None
        self.installer.load_state()
        self.installer.install()
        self.assertEqual(1, sum(label == "record-release" for label, _, _ in self.fake.commands))
        self.assertEqual(original, (self.directory / "runtime/.env").read_bytes())

    def test_unknown_fresh_commit_uses_original_plan_and_readback(self):
        self.prepare()
        self.fake.fail = "fresh-apply"
        with self.assertRaises(M.InstallError):
            self.installer.install()
        original = (self.directory / "evidence/install.json").read_bytes()
        self.fake.fail = None
        self.installer.load_state()
        self.installer.install()
        labels = [label for label, _, _ in self.fake.commands]
        self.assertEqual(1, labels.count("fresh-plan"))
        self.assertEqual(1, labels.count("fresh-apply"))
        self.assertEqual(original, (self.directory / "evidence/install.json").read_bytes())

    def test_unclaimed_database_with_retained_evidence_uses_resume(self):
        self.prepare()
        self.fake.before_claim = True
        with patch("sys.stdout", io.StringIO()), self.assertRaisesRegex(M.InstallError, "before claim"):
            self.installer.install()
        evidence = self.directory / "evidence/publications"
        self.assertTrue(evidence.is_dir())
        self.assertEqual("unclaimed", self.fake.state)
        marker = evidence / "retained"
        marker.write_text("do not remove")
        self.fake.before_claim = False
        self.installer.load_state()
        with patch("sys.stdout", io.StringIO()):
            self.installer.install()
        calls = [args for label, args, _ in self.fake.commands if label == "fresh-apply"]
        self.assertIn("apply", calls[0])
        self.assertIn("resume", calls[1])
        self.assertEqual("do not remove", marker.read_text())

    def test_resume_refuses_altered_secret_and_changed_daemon(self):
        self.prepare()
        self.installer.state["daemon"] = "different"
        with self.assertRaisesRegex(M.InstallError, "daemon changed"):
            self.check(resume=True)
        (self.directory / "runtime/.env").write_text("altered")
        with self.assertRaisesRegex(M.InstallError, "changed"):
            self.installer.load_state()

    def test_resume_refuses_foreign_project_resources(self):
        self.prepare()
        self.installer.state["image_ids"] = self.fake.image_ids
        self.fake.old_project = True
        with self.assertRaisesRegex(M.InstallError, "another installation"):
            self.check(resume=True)

    def test_child_error_cannot_echo_secret_and_ambient_config_is_ignored(self):
        with patch.dict(os.environ, {"JWT_SECRET": "private-value", "COMPOSE_FILE": "other.yml", "DOCKER_HOST": "tcp://remote"}):
            env = M.clean_environment()
            self.assertNotIn("JWT_SECRET", env)
            self.assertNotIn("COMPOSE_FILE", env)
            self.assertNotIn("DOCKER_HOST", env)
        result = M.subprocess.CompletedProcess([], 1, b"private-value", b"private-value")
        with patch.object(M.subprocess, "run", return_value=result), self.assertRaises(M.InstallError) as caught:
            M.execute(["synthetic"], label="preflight")
        self.assertNotIn("private-value", str(caught.exception))


if __name__ == "__main__":
    unittest.main()
