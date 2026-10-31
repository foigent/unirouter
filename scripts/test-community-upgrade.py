#!/usr/bin/env python3
"""Finite host-boundary tests. Docker and service operations are never executed."""
import base64
import copy
import datetime
import importlib.util
import json
import hashlib
import io
import re
import shutil
from types import SimpleNamespace
from pathlib import Path
import tempfile
import tarfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module
M = load("community_upgrade_under_test", ROOT / "deploy/customer/upgrade.py")
I = load("community_installer_existing_fixtures", ROOT / "scripts/test-community-installer.py")

class OfflineTar:
    """Test-only stand-in for GNU tar; real payload bytes use the shared verifier.

    This fixture does not establish GNU tar/Linux/ACL runtime acceptance.
    """
    def __init__(self): self.commands = []
    def run(self, args, **kwargs):
        self.commands.append(list(args))
        if args == ["tar", "--version"]: return "tar (GNU tar) offline fixture"
        if "-cpf" in args:
            archive = Path(args[args.index("-cpf") + 1])
            roots = args[args.index("-C") + 2:]
            with tarfile.open(archive, "w:", dereference=False) as output:
                for name in roots: output.add(Path("/") / name, arcname=name)
        elif "-xpf" in args:
            archive = Path(args[args.index("-xpf") + 1])
            destination = Path(args[args.index("-C") + 1])
            with tarfile.open(archive, "r:") as incoming:
                # Production uses GNU tar only after shared archive validation.
                # The fixture has the same validated closed member inventory.
                options = {"numeric_owner": True}
                if hasattr(tarfile, "fully_trusted_filter"):
                    options["filter"] = tarfile.fully_trusted_filter
                incoming.extractall(destination, **options)
        else: raise AssertionError("unexpected archive command")
        return ""


class UpgradeBoundaryTests(unittest.TestCase):
    def test_native_lease_expiry_preserves_postgres_precision_and_offsets(self):
        for digits in range(7):
            fraction = "1" * digits
            microsecond = int(fraction.ljust(6, "0")) if fraction else 0
            for suffix, seconds in (("Z", 0), ("+00:00", 0), ("-02:30", -9000),
                                    ("+01:02:03", 3723), ("-00:00:30", -30)):
                with self.subTest(digits=digits, offset=suffix):
                    value = "2000-01-01T00:00:00" + ("." + fraction if fraction else "") + suffix
                    expected = datetime.datetime(2000, 1, 1, microsecond=microsecond,
                        tzinfo=datetime.timezone(datetime.timedelta(seconds=seconds)))
                    self.assertEqual(expected, M._native_lease_expiry(value))
                    self.assertEqual(microsecond, M._native_lease_expiry(value).microsecond)
                    self.assertEqual(expected.utcoffset(), M._native_lease_expiry(value).utcoffset())

    def test_native_lease_expiry_refuses_unknown_or_invalid_input_without_echo(self):
        for value in (None, 0, True, {}, [], "secret-sentinel", "2000-01-01T00:00:00",
                      "2000-01-01T00:00:00.1234567Z", "2000-01-01T00:00:00.Z",
                      "2000-02-30T00:00:00Z", "2000-01-01T24:00:00Z",
                      "2000-01-01T00:00:00+24:00", "2000-01-01T00:00:00+00:60",
                      "2000-01-01T00:00:00+00:00:60", "2000-01-01T00:00:00+0000",
                      "2000-01-01T00:00:00+01:02:03.1", "2000-01-01T00:00:00Z\n"):
            with self.subTest(value=value):
                with self.assertRaises(M.install.InstallError) as caught:
                    M._native_lease_expiry(value)
                self.assertEqual("Native lease expiry has an unsupported timestamp", str(caught.exception))

    def test_retirement_ready_keeps_past_equal_future_and_invalid_lease_boundaries(self):
        real_datetime = datetime.datetime
        now = real_datetime(2026, 10, 28, microsecond=123000, tzinfo=datetime.timezone.utc)
        class Clock(real_datetime):
            @classmethod
            def now(cls, tz=None): return now
        operation = object.__new__(M.Upgrade)
        for seconds in (0, 3723, -30):
            for difference in (-1, 0, 1):
                value = (now + datetime.timedelta(microseconds=difference)).astimezone(
                    datetime.timezone(datetime.timedelta(seconds=seconds))).isoformat()
                metadata = {"generations": [{"state": "draining", "lease_expires_at": value}]}
                before = copy.deepcopy(metadata)
                operation.native_metadata = lambda: metadata
                with self.subTest(offset=seconds, difference=difference), patch.object(M.datetime, "datetime", Clock):
                    if difference > 0:
                        with self.assertRaisesRegex(M.install.InstallError, "lease is still live"):
                            operation._retirement_ready()
                    else:
                        operation._retirement_ready()
                self.assertEqual(before, metadata)
        for value in (None, "invalid-private-input", "2000-01-01T00:00:00", "2099-01-01T00:00:00.1Z"):
            metadata = {"generations": [{"state": "serving", "lease_expires_at": value}]}
            before = copy.deepcopy(metadata)
            operation.native_metadata = lambda: metadata
            with self.assertRaises(M.install.InstallError): operation._retirement_ready()
            self.assertEqual(before, metadata)

    @staticmethod
    def mount_inventory():
        return [{"Type": "bind", "Source": "/synthetic/content", "Destination": "/content",
                 "Mode": "rw", "RW": True, "Propagation": "rprivate",
                 "Unknown": {"options": ["first", "second"], "binding": {"a": 1, "b": 2}}},
                {"Type": "volume", "Name": "synthetic-logs", "Source": "/synthetic/logs",
                 "Destination": "/app/logs", "Driver": "local", "Mode": "z", "RW": True, "Propagation": ""}]

    def setUp(self):
        self.installer = I.InstallerTests(methodName="test_check_is_read_only")
        self.installer.setUp()
        self.addCleanup(self.installer.doCleanups)
        self.installer.prepare()

    def test_status_is_read_only_and_has_no_service_or_secret_action(self):
        before = {str(p): p.read_bytes() for p in self.installer.directory.rglob("*") if p.is_file()}
        run = lambda *args, **kwargs: self.fail("status issued a Docker/API command")
        installation = M.Installation(self.installer.directory, run)
        with patch.object(M.install.secrets, "token_hex", side_effect=AssertionError("secret creation")):
            result = installation.status()
        self.assertEqual("not_started", result["state"])
        self.assertEqual(before, {str(p): p.read_bytes() for p in self.installer.directory.rglob("*") if p.is_file()})
        self.assertNotIn("source_state", result)

    def test_mount_inventory_ignores_only_top_level_record_order(self):
        original = self.mount_inventory()
        reordered = copy.deepcopy(original[::-1])
        before = M.canonical(original), M.canonical(reordered)
        self.assertTrue(M.same_mounts(original, reordered))
        self.assertEqual(before, (M.canonical(original), M.canonical(reordered)))
        self.assertTrue(M.same_mounts(original + original[:1], original[:1] + reordered))
        self.assertTrue(M.same_mounts([], []))
        mutations = [(0, "Type", "volume"), (0, "Source", "/different/source"),
                     (0, "Destination", "/different/destination"), (0, "Mode", "ro"),
                     (0, "RW", False), (0, "RW", 1), (0, "Propagation", "rshared"),
                     (1, "Name", "another-volume"), (1, "Driver", "another-driver"),
                     (0, "Unknown", {"options": ["second", "first"], "binding": {"a": 1, "b": 2}}),
                     (0, "Additional", "new-field")]
        for index, key, value in mutations:
            changed = copy.deepcopy(reordered)
            changed[1 - index][key] = value
            with self.subTest(field=key, value=value):
                self.assertFalse(M.same_mounts(original, changed))
        for changed in (original[:1], original + original[:1], [original[0], original[0]], None, (), "", [None]):
            with self.subTest(inventory=changed):
                self.assertFalse(M.same_mounts(original, changed))

    def test_claim_retains_original_bytes_and_revokes_old_package_binding(self):
        installation = M.Installation(self.installer.directory, self.installer.fake.run)
        original = installation.state_raw
        installation.claim({"package_sha256": self.installer.release.sha}, {"package_sha256": "f" * 64}, {"operation_id":"ucu_"+"a"*32})
        witness = installation.state["upgrade"]
        self.assertEqual(original, installation.source_snapshot(witness))
        self.assertEqual("f" * 64, installation.state["package_sha256"])
        self.assertEqual(self.installer.release.sha, M.install.digest(self.installer.directory / "release.tar.gz"))
        with self.assertRaisesRegex(M.install.InstallError, "already owns"):
            installation.claim({}, {"package_sha256": "e" * 64})
        with self.assertRaisesRegex(I.M.InstallError, "upgrade witness"):
            self.installer.installer.load_state()

    def test_identical_image_override_is_confirmed_without_rewriting(self):
        # Directly cover the permitted owner input where only package metadata
        # changes. Configuration already equals the requested exact bytes.
        operation = M.Upgrade.__new__(M.Upgrade)
        operation.directory = self.installer.directory
        operation.runtime = operation.directory / "runtime"
        operation.release = SimpleNamespace(sha="f"*64, images=self.installer.release.images)
        operation.roles = ("backend",)
        operation.compose = []
        operation.state = {"upgrade":{"format":M.SCHEMA,"candidate":{"package_sha256":"f"*64},
            "candidate_image":{"id":"sha256:"+"a"*64},"done":[],"pending":"configure-images",
            "receipts":{},"image_override":str(operation.runtime/"upgrade-image.json")}}
        path = Path(operation.witness["image_override"])
        raw = M.canonical(operation._image_override())
        M.install.private_file(path,raw)
        operation.witness["source_image_override"] = {"bytes":base64.b64encode(raw).decode(),
            "sha256":hashlib.sha256(raw).hexdigest(),"metadata":M.recovery.filemeta(path)}
        before = path.read_bytes(), M.recovery.filemeta(path)
        operation._write_owned = lambda *args: self.fail("unchanged override was rewritten")
        self.assertEqual({"file":str(path),"sha256":hashlib.sha256(raw).hexdigest()},operation._configure_images())
        self.assertEqual(before,(path.read_bytes(),M.recovery.filemeta(path)))

    def test_single_and_dual_writer_bindings_share_native_input(self):
        for count in (1, 2):
            metadata, source, candidate, proofs = self.fixture(count)
            document = M.maintenance_input(metadata, source, candidate, "e" * 64, proofs)
            self.assertEqual(count, len(document["retirements"]))
            self.assertEqual("actual-source-db", document["expected_database"])
            self.assertEqual(1234, document["expected_database_oid"])
            self.assertTrue(all(row["expected_ingress_head"] == "withdraw-all" for row in document["retirements"]))
            self.assertEqual(2, document["retirements"][0]["expected_revision"])
            self.assertNotIn("public_keys", document)
            self.assertNotIn("approved", document)

    def test_missing_second_exit_or_unresolved_ingress_refuses_whole_batch(self):
        for kind in ("second_missing", "second_not_exited", "second_image", "receiver", "pending", "source"):
            with self.subTest(kind=kind):
                metadata, source, candidate, proofs = self.fixture(2)
                if kind == "second_missing": del proofs["uri_" + "b" * 32]
                elif kind == "second_not_exited": proofs["uri_" + "b" * 32]["old_processes_gone"] = False
                elif kind == "second_image": proofs["uri_" + "b" * 32]["image"] = "sha256:" + "f" * 64
                elif kind == "receiver": metadata["ingress"][0]["membership_after"] = [{"instance": "other"}]
                elif kind == "pending": metadata["ingress"][0]["status"] = "pending"
                elif kind == "source": source["release_sequence"] += 1
                before = copy.deepcopy(metadata)
                with self.assertRaises(M.install.InstallError):
                    M.maintenance_input(metadata, source, candidate, "e" * 64, proofs)
                self.assertEqual(before, metadata)

    def test_public_ring_identity_normalizes_existing_encodings(self):
        raw = b"s" * 32
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "public.json"
            path.write_text(json.dumps({"existing": base64.b64encode(raw).decode()}))
            expected = M.ring_digest(path)
            path.write_text(json.dumps({"existing": raw.hex()}))
            self.assertEqual(expected, M.ring_digest(path))
            path.write_text('{"existing":"a","existing":"b"}')
            with self.assertRaises(M.install.InstallError): M.ring_digest(path)

    def test_paired_file_restore_preserves_body_config_links_and_modes(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            config, bodies, evidence = root / "runtime", root / "bodies", root / "evidence"
            for path in (config, bodies, evidence): path.mkdir(mode=0o700)
            (config / ".env").write_bytes(b"SYNTHETIC_CONFIGURATION=fixture-only\n")
            (config / ".env").chmod(0o600)
            (bodies / "body.bin").write_bytes(bytes(range(256)))
            (bodies / "body.bin").chmod(0o640)
            (config / "retained-link").symlink_to("../bodies/body.bin")
            transport = OfflineTar()
            original = M.write_paired_files([config, bodies], evidence, transport.run)
            result = M.restore_and_verify_paired_files(evidence, transport.run)
            restored = Path(result["verification_directory"])
            self.assertEqual(bytes(range(256)), (restored / str(bodies / "body.bin").lstrip("/")).read_bytes())
            self.assertEqual(bytes(range(256)), (restored / str(config / "retained-link").lstrip("/")).read_bytes())
            self.assertEqual(0o640, (restored / str(bodies / "body.bin").lstrip("/")).stat().st_mode & 0o777)
            self.assertEqual(original["archive_sha256"], result["archive_sha256"])
            self.assertEqual(result, M.restore_and_verify_paired_files(evidence, transport.run), "readback reuses exact existing restored bytes")
            self.assertEqual(b"SYNTHETIC_CONFIGURATION=fixture-only\n", (config / ".env").read_bytes())

    def test_paired_archive_refuses_external_symlink_without_creating_backup(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            source, evidence = root / "source", root / "evidence"
            source.mkdir(); evidence.mkdir()
            (root / "outside").write_bytes(b"not in the approved roots")
            (source / "external").symlink_to("../outside")
            with self.assertRaisesRegex(M.recovery.RecoveryError, "outside exact archived"):
                M.write_paired_files([source], evidence, OfflineTar().run)
            self.assertEqual([], list(evidence.iterdir()))

    def test_paired_archive_tamper_or_existing_restore_drift_is_not_overwritten(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            source, evidence = root / "source", root / "evidence"
            source.mkdir(); evidence.mkdir()
            (source / "body").write_bytes(b"synthetic body")
            transport = OfflineTar()
            M.write_paired_files([source], evidence, transport.run)
            result = M.restore_and_verify_paired_files(evidence, transport.run)
            restored = Path(result["verification_directory"]) / str(source / "body").lstrip("/")
            restored.write_bytes(b"unexpected restore write")
            with self.assertRaisesRegex(M.install.InstallError, "Existing recovery verification files differ"):
                M.restore_and_verify_paired_files(evidence, transport.run)
            self.assertEqual(b"unexpected restore write", restored.read_bytes())
            (evidence / "paired-files.tar").write_bytes(b"changed archive")
            with self.assertRaisesRegex(M.install.InstallError, "archive changed"):
                M.restore_and_verify_paired_files(evidence, transport.run)

    def running_fixture(self, count, source_sequence=10):
        fixture = I.InstallerTests(methodName="test_check_is_read_only")
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        data = fixture.fixture
        data.manifest.update(version="1.15.0", release_sequence=source_sequence+1, min_current_sequence=0,
                             max_current_sequence=source_sequence, min_current_schema=M.SOURCE_SCHEMA, key_id="fixture-release")
        for name in ("upgrade.py", "recovery.py", "source-budget-restore-rebase.sql"):
            raw = (ROOT / ("scripts" if name.endswith(".sql") else "deploy/customer") / name).read_bytes()
            member = ("payload/scripts/" if name.endswith(".sql") else I.M.PREFIX) + name
            path = data.release / member
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(raw)
            data.manifest["artifacts"].append({"path": member, "size": len(raw), "sha256": hashlib.sha256(raw).hexdigest()})
        manifest_raw = json.dumps(data.manifest).encode()
        data.manifest_sha = hashlib.sha256(manifest_raw).hexdigest()
        with tarfile.open(data.package, "w:gz") as archive:
            member = tarfile.TarInfo("manifest.json"); member.size = len(manifest_raw)
            archive.addfile(member, io.BytesIO(manifest_raw))
            for artifact in data.manifest["artifacts"]:
                raw = (data.release / artifact["path"]).read_bytes()
                member = tarfile.TarInfo(artifact["path"]); member.size = len(raw)
                archive.addfile(member, io.BytesIO(raw))
        data.keys.write_text(json.dumps({"fixture-release": base64.b64encode(b"t" * 32).decode()}))
        fixture.release = data.load()
        fixture.fake = I.DockerFixture(fixture.release, fixture.directory)
        fixture.installer = I.M.Installer(fixture.release, fixture.directory, "fixture-ur", 65283,
                                         Path("/var/run/docker.sock"), fixture.fake.run)
        fixture.prepare()
        old_images = dict(fixture.release.images)
        old_images["backend"] = "registry.example.invalid/backend-old@sha256:" + "a" * 64
        old_images["frontend"] = "registry.example.invalid/frontend-old@sha256:" + "b" * 64
        images = {name: "sha256:" + str(index) * 64 for index, name in enumerate(I.M.SERVICES, 1)}
        image_by_reference = {reference: images[name] for name, reference in old_images.items()}
        descriptor = json.dumps({"schema_version": "unirouter.install.v1", "images": old_images}).encode()
        source_manifest = {"version": "1.14.2", "commit": "a" * 40, "release_sequence": source_sequence,
                           "target_schema": M.SOURCE_SCHEMA, "min_current_sequence": 0, "key_id": "fixture-release",
                           "artifacts": [{"path": I.M.DESCRIPTOR, "size": len(descriptor), "sha256": hashlib.sha256(descriptor).hexdigest()}]}
        source_raw = json.dumps(source_manifest).encode()
        source_package = fixture.directory / "release.tar.gz"
        with tarfile.open(source_package, "w:gz") as archive:
            for name, raw in (("manifest.json", source_raw), (I.M.DESCRIPTOR, descriptor)):
                member = tarfile.TarInfo(name); member.size = len(raw)
                archive.addfile(member, io.BytesIO(raw))
        old_sha = I.M.digest(source_package)
        fixture.installer.state["package_sha256"] = old_sha
        fixture.installer.state["image_ids"] = images
        I.M.private_file(fixture.directory / "runtime/runtime-image.json", M.canonical({"services": {"backend": {"environment": {"RUNTIME_IMAGE_DIGEST": images["backend"]}}}}))
        fixture.installer.state["files"]["runtime/runtime-image.json"] = I.M.digest(fixture.directory / "runtime/runtime-image.json")
        I.M.save_state(fixture.directory, fixture.installer.state)
        env_path = fixture.directory / "runtime/.env"
        env_raw = env_path.read_text()
        for name in ("backend", "frontend"):
            env_raw = env_raw.replace(fixture.release.images[name], old_images[name])
        env_path.write_text(env_raw)
        rendered_env = {"RUNTIME_ENVIRONMENT": "production", "JWT_SECRET": M.environment(env_raw.encode())["JWT_SECRET"],
                        "RUNTIME_IMAGE_DIGEST": images["backend"]}
        metadata, _, _, _ = self.fixture(count)
        metadata["release"].update(release_sequence=source_sequence, version="1.14.2", commit="a" * 40, package_sha256=old_sha, trusted_key_id="fixture-release")
        metadata["recorded_at"] = "2026-10-25T00:00:00Z"
        metadata["database"] = M.environment(env_raw.encode())["POSTGRES_DB"]
        for row in metadata["generations"]:
            row["artifact_digest"] = images["backend"]
        roles = ("backend", "backend-canary") if count == 2 else ("backend",)
        edge = None
        if count == 2:
            edge = fixture.root / "edge"; edge.mkdir(mode=0o700)
            (edge / "runtime-overrides.json").write_text(json.dumps({"services": {"backend": {}, "backend-canary": {}}}))
            (edge / "compose.edge.json").write_text(json.dumps({"services": {"nginx": {"container_name": "uniroute-nginx"}}}))
            (edge / "nginx.conf").write_text("synthetic ingress")
        calls = []
        state = SimpleNamespace(metadata=metadata, running_env=dict(rendered_env), duplicate_instance=False)
        def run(args, *, label, input=None):
            calls.append((label, list(args)))
            if label == "signed release verification": return data.verify(args)
            if label == "verify retained source signature":
                return "verified=true\npackage_sha256=" + old_sha + "\nmanifest_sha256=" + hashlib.sha256(source_raw).hexdigest()
            if label == "check original Docker daemon": return json.dumps({"ID": "fixture-daemon", "OSType": "linux", "SecurityOptions": []})
            if label == "read exact installation metadata":
                query = input.decode()
                if "pg_database_size" in query: return "1024"
                if "rolcreatedb" in query: return "t"
                return json.dumps(state.metadata)
            if label == "check retained effective Compose":
                services = {name: {"image": old_images["backend" if name in roles else name],
                                   "environment": dict(rendered_env) if name in roles else {}}
                            for name in set(I.M.SERVICES) | set(roles)}
                return json.dumps({"services": services})
            if label == "inspect owned upgrade resource":
                name = args[-1]
                if name == "uniroute-nginx": return json.dumps([{"Config": {"Labels": {"com.docker.compose.project": "fixture-ur-edge", "com.docker.compose.service": "nginx"}}, "State": {"Running": True}}])
                role = name.removeprefix("fixture-ur-").removesuffix("-1")
                return json.dumps([{"Id": "container-" + role, "Image": images["backend" if role in roles else role],
                    "Config": {"Labels": {I.M.LABEL: fixture.installer.state["installation_id"], "com.docker.compose.project": "fixture-ur", "com.docker.compose.service": role},
                               "Env": [key + "=" + value for key, value in state.running_env.items()] if role in roles else []},
                    "State": {"Running": True}, "HostConfig": {"PortBindings": {}}}])
            if label == "check retained signed image bytes": return json.dumps([{"Id": image_by_reference[args[-1]], "Os": "linux", "Architecture": "amd64"}])
            if label == "check exact source writer readiness":
                second = args[args.index("exec") + 1].endswith("backend-canary") and not state.duplicate_instance
                return json.dumps({"status": "ok", "runtime": {"artifact_digest": images["backend"], "instance": "uri_" + ("b" if second else "a") * 32, "state": "serving"}})
            if label == "check source artifact native authority": return "{}"
            raise AssertionError("unexpected host check operation: " + label)
        operation = M.Upgrade(fixture.directory, data.release, data.verifier, data.keys, source_package,
                              "ur_edge_pair" if count == 2 else "community_single", edge, run)
        return operation, state, calls

    def test_whole_host_preflight_reuses_single_and_dual_layout_without_writes(self):
        for count in (1, 2):
            with self.subTest(writers=count):
                operation, state, calls = self.running_fixture(count)
                before = {str(p): p.read_bytes() for p in operation.directory.rglob("*") if p.is_file()}
                with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                    result = operation.check()
                self.assertEqual(count, result["writers"])
                self.assertEqual(0, result["writes"])
                self.assertEqual(before, {str(p): p.read_bytes() for p in operation.directory.rglob("*") if p.is_file()})
                self.assertFalse(any(any(word in argv for word in ("pull", "stop", "up", "start", "create", "rm")) for _, argv in calls))
                self.assertNotIn(state.running_env["JWT_SECRET"], json.dumps(result))

    def test_host_preflight_rejects_unaccounted_writer_or_changed_running_settings(self):
        for kind in ("unknown_writer", "secret_drift", "duplicate_runtime", "pending_ingress"):
            with self.subTest(kind=kind):
                operation, state, calls = self.running_fixture(2)
                if kind == "unknown_writer":
                    extra = dict(state.metadata["generations"][0]); extra["instance_id"] = "uri_" + "c" * 32
                    state.metadata["generations"].append(extra)
                elif kind == "secret_drift": state.running_env["JWT_SECRET"] = "different synthetic live value"
                elif kind == "duplicate_runtime": state.duplicate_instance = True
                else: state.metadata["ingress"][0]["status"] = "pending"
                with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                    with self.assertRaises(M.install.InstallError): operation.check()
                self.assertFalse(any("pull" in argv or "stop" in argv or "up" in argv for _, argv in calls))

    @staticmethod
    def fixture(count):
        original = {"state": "completed", "plan": {"installation_id": "ufi_" + "a" * 32, "digest": "d" * 64, "target": {"environment": "production"}, "artifact":{"ledger":[M.SOURCE_SCHEMA]}}}
        metadata = {"installation": "unirouter.fresh-install.v1\n" + json.dumps(original), "ledger": [M.SOURCE_SCHEMA],
                    "database": "actual-source-db", "database_oid": 1234, "system_id": "123456789",
                    "release": {"release_sequence": 10, "package_sha256": "a" * 64, "target_schema": M.SOURCE_SCHEMA},
                    "ingress": [{"change_id": "withdraw-all", "predecessor_id": None, "status": "applied", "membership_after": []}], "generations": []}
        proofs = {}
        for index in range(count):
            letter = chr(ord("a") + index)
            identity = "uri_" + letter * 32
            metadata["generations"].append({"instance_id": identity, "artifact_digest": "sha256:" + "c" * 64,
                "manifest_digest": "f" * 64, "compatibility_epoch": "unirouter.runtime-compatibility.v1",
                "schema_head": M.SOURCE_SCHEMA, "revision": 2, "state": "draining", "ingress_state": "absent"})
            proofs[identity] = {"instance_id": identity, "image": "sha256:" + "c" * 64, "container_id": letter * 64,
                                "clean_exit": True, "old_processes_gone": True}
        return metadata, {"release_sequence": 10, "package_sha256": "a" * 64, "manifest_sha256": "b" * 64}, {"package_sha256": "c" * 64, "manifest_sha256": "d" * 64}, proofs

class Interrupted(Exception):
    pass


def historical_fixture(root, installation, metadata, source, kind="process_exit", normalized=False):
    """Synthetic original observations, shared by decoder and whole-host cases."""
    root.mkdir(mode=0o700)
    def reference(name, value, pointer=""):
        path = root / name
        raw = value if isinstance(value, bytes) else M.canonical(value)
        M.install.private_file(path, raw)
        return {"path": str(path), "sha256": hashlib.sha256(raw).hexdigest(), "pointer": pointer}
    generation = dict(metadata["generations"][0], instance_id="uri_" + "f" * 32,
        revision=7, state="serving", ingress_state="absent", lease_expires_at="2000-01-01T00:00:00Z")
    metadata["generations"].append(generation)
    before = {"installation_id": installation["installation_id"], "instance_id": generation["instance_id"],
              "container_id": "d" * 64, "image": generation["artifact_digest"]}
    raw_before = reference("before.json", before)
    bindings = {key: dict(raw_before, pointer="/" + key) for key in before}
    argv = ["docker", "compose", "--project-name", installation["project"],
            "stop" if kind == "process_exit" else "up", "backend"]
    receipt = reference("operation.process.json", {"returncode": 0, **({} if normalized else {"argv": argv})})
    record = {"kind": kind, "generation": {key: generation[key] for key in M._GENERATION_FIELDS},
              "service": "backend", "before": bindings, "command": receipt, "program": None}
    if normalized:
        program = reference("original-program.txt", b"Original recorded native program; never executed by this reader.\n", None)
        record["program"] = program
        record["command"] = reference("normalized.process.json", {"format": "unirouter.normalized-command-observation.v1",
            "returncode": 0, "argv": argv, "original_process": receipt, "program_sha256": program["sha256"]})
    if kind == "process_exit":
        record["after"] = reference("after.json", {"Id": before["container_id"], "Image": before["image"],
            "State": {"Running": False, "Pid": 0, "ExitCode": 0, "OOMKilled": False}})
        record["processes"] = reference("processes.json", {"98765": "100"})
    else:
        successor = dict(before, instance_id=metadata["generations"][0]["instance_id"], container_id="a" * 64)
        raw_successor = reference("successor.json", successor)
        record["successor"] = {key: dict(raw_successor, pointer="/" + key) for key in successor}
        name = installation["project"] + "-backend-1"
        record["output"] = reference("operation.stderr", ("\n".join(" Container " + name + "  " + state
            for state in ("Recreate", "Recreated", "Started", "Healthy")) + "\n").encode(), None)
    document = {"format": "unirouter.historical-exits.v1", "installation_id": installation["installation_id"],
        "project": installation["project"], "database_system_id": metadata["system_id"], "database": metadata["database"],
        "database_oid": metadata["database_oid"], "source_sequence": source["release_sequence"],
        "source_package_sha256": source["package_sha256"], "records": [record]}
    path = root / "historical-exits.json"
    M.install.private_file(path, M.canonical(document))
    return path, document, generation


class HistoricalObservationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.installation = {"installation_id": "ufi_" + "a" * 32, "project": "fixture-ur"}
        self.metadata, self.source, self.candidate, self.current_proofs = UpgradeBoundaryTests.fixture(1)

    def prepare(self, kind="process_exit", normalized=False):
        return historical_fixture(self.root / "observations", self.installation, self.metadata, self.source, kind, normalized)

    def validate(self, document, *, current=None, process=None):
        return M.HistoricalObservations(M.canonical(document)).validate(self.installation, self.metadata, self.source,
            current or (lambda _: None), process or (lambda _: None))

    def test_original_process_proof_and_full_union_reuse_one_native_input(self):
        _, document, generation = self.prepare()
        history = self.validate(document)
        self.assertIsInstance(history[generation["instance_id"]], M._HistoricalExit)
        result = M.maintenance_input(self.metadata, self.source, self.candidate, "e" * 64, self.current_proofs, history)
        self.assertEqual(len(result["retirements"]), 2)
        with self.assertRaises(M.install.InstallError):
            M.maintenance_input(self.metadata, self.source, self.candidate, "e" * 64, self.current_proofs)
        with self.assertRaises(M.install.InstallError):
            M.maintenance_input(self.metadata, self.source, self.candidate, "e" * 64, self.current_proofs,
                {generation["instance_id"]: {"clean_exit": True, "gone": True}})

    def test_normalized_replacement_preserves_termination_without_inventing_clean_exit(self):
        _, document, generation = self.prepare("native_replacement", True)
        history = self.validate(document)
        result = M.maintenance_input(self.metadata, self.source, self.candidate, "e" * 64, self.current_proofs, history)
        self.assertEqual(len(result["retirements"]), 2)
        self.assertNotIn(b"clean_exit", history[generation["instance_id"]].observation)
        with self.assertRaises(M.install.InstallError):
            self.validate(document, current=lambda _: {"Image": generation["artifact_digest"], "Project": "fixture-ur",
                "Service": "backend", "Running": True, "Pid": 42})

    def test_replacement_completion_spacing_preserves_original_raw_proof(self):
        _, document, generation = self.prepare("native_replacement", True)
        reference = document["records"][0]["output"]
        path = Path(reference["path"])
        for spacing in (" ", "  ", "\t", " \t "):
            with self.subTest(spacing=repr(spacing)):
                raw = ("\n".join(" Container" + spacing + "fixture-ur-backend-1" + spacing + state
                    for state in ("Recreated", "Started", "Healthy")) + "\n").encode()
                path.write_bytes(raw); reference["sha256"] = M.install.digest(path)
                original = M.canonical(document)
                evidence = M.HistoricalObservations(original)
                actual = evidence.validate(self.installation, self.metadata, self.source, lambda _: None, lambda _: None)
                self.assertEqual(raw, path.read_bytes())
                self.assertEqual(raw, evidence.sources[str(path)])
                self.assertEqual(original, M.canonical(document))
                self.assertEqual(M.canonical(document["records"][0]), actual[generation["instance_id"]].observation)

    def test_replacement_completion_requires_every_exact_token(self):
        _, document, _ = self.prepare("native_replacement", True)
        reference = document["records"][0]["output"]
        path = Path(reference["path"])
        rows = [" Container fixture-ur-backend-1  " + state for state in ("Recreated", "Started", "Healthy")]
        variants = [[row for index, row in enumerate(rows) if index != missing] for missing in range(3)]
        variants += [[row.replace("fixture-ur-backend-1", "other-backend-1") for row in rows],
            [row.replace("fixture-ur-backend-1", "fixture-ur-backend-canary-1") for row in rows],
            [row.replace("Recreated", "Recreate").replace("Started", "Starting").replace("Healthy", "Health") for row in rows],
            [row + " extra" for row in rows], ["extra " + row for row in rows],
            ["\x1b[32m" + row + "\x1b[0m" for row in rows],
            [json.dumps({"id": "Container fixture-ur-backend-1", "status": state}) for state in ("Recreated", "Started", "Healthy")],
            [rows[0], rows[0], rows[2]], [rows[0], rows[1], rows[2].replace("fixture-ur", "other")]]
        for index, variant in enumerate(variants):
            with self.subTest(variant=index):
                raw = ("\n".join(variant) + "\n").encode(); path.write_bytes(raw); reference["sha256"] = M.install.digest(path)
                with self.assertRaisesRegex(M.install.InstallError, "replacement completion is missing"):
                    self.validate(document)
                self.assertEqual(raw, path.read_bytes())

    def test_replacement_completion_never_substitutes_command_identity_or_origin(self):
        _, original, generation = self.prepare("native_replacement")
        sources = {path: path.read_bytes() for path in (self.root / "observations").iterdir() if path.is_file()}
        for kind in ("failed-command", "wrong-service", "same-successor", "live-original", "foreign-output", "changed-output"):
            with self.subTest(kind=kind):
                for path, raw in sources.items(): path.write_bytes(raw)
                document = copy.deepcopy(original); record = document["records"][0]
                if kind in ("failed-command", "wrong-service"):
                    reference = record["command"]; path = Path(reference["path"]); receipt = json.loads(path.read_bytes())
                    if kind == "failed-command": receipt["returncode"] = 1
                    else: receipt["argv"][-1] = "frontend"
                    path.write_bytes(M.canonical(receipt)); reference["sha256"] = M.install.digest(path)
                elif kind == "same-successor":
                    path = Path(record["successor"]["container_id"]["path"]); successor = json.loads(path.read_bytes())
                    successor["container_id"] = "d" * 64; path.write_bytes(M.canonical(successor))
                    for reference in record["successor"].values(): reference["sha256"] = M.install.digest(path)
                elif kind == "foreign-output":
                    reference = record["output"]; path = self.root / "other.stderr"
                    M.install.private_file(path, Path(reference["path"]).read_bytes()); reference["path"] = str(path)
                elif kind == "changed-output":
                    Path(record["output"]["path"]).write_bytes(b"changed original progress")
                current = (lambda _: {"Image": generation["artifact_digest"], "Project": "fixture-ur",
                    "Service": "backend", "Running": True, "Pid": 42}) if kind == "live-original" else None
                with self.assertRaises(M.install.InstallError): self.validate(document, current=current)

    def test_missing_argv_is_not_repaired_by_arbitrary_nonempty_program(self):
        _, document, _ = self.prepare("native_replacement", True)
        normalized = json.loads(Path(document["records"][0]["command"]["path"]).read_bytes())
        document["records"][0]["command"] = normalized["original_process"]
        with self.assertRaisesRegex(M.install.InstallError, "exact original service arguments"):
            self.validate(document)

    def test_crossed_identity_or_duplicate_record_refuses(self):
        _, original, _ = self.prepare()
        for key in ("installation_id", "database_system_id", "database", "database_oid", "source_sequence", "source_package_sha256"):
            bad = copy.deepcopy(original); bad[key] = 99 if isinstance(bad[key], int) else "other"
            with self.subTest(key=key), self.assertRaises(M.install.InstallError): self.validate(bad)
        bad = copy.deepcopy(original);bad["records"].append(copy.deepcopy(bad["records"][0]))
        with self.assertRaises(M.install.InstallError):self.validate(bad)
        for key in ("artifact_digest", "manifest_digest", "instance_id", "revision"):
            bad = copy.deepcopy(original);bad["records"][0]["generation"][key] = 99 if key == "revision" else "changed"
            with self.subTest(key=key), self.assertRaises(M.install.InstallError):self.validate(bad)

    def test_live_or_reused_PID_and_changed_original_refuse(self):
        _, document, _ = self.prepare()
        with self.assertRaises(M.install.InstallError):self.validate(document, process=lambda _: "100")
        self.validate(document, process=lambda _: "101")
        path = Path(document["records"][0]["processes"]["path"])
        path.write_bytes(b"PID\n98765\n")
        document["records"][0]["processes"].update(sha256=M.install.digest(path), pointer=None)
        self.validate(document)
        with self.assertRaises(M.install.InstallError):self.validate(document, process=lambda _: "101")
        path.write_bytes(b"changed")
        with self.assertRaises(M.install.InstallError):self.validate(document)

    def test_missing_source_permissions_pointer_or_caller_flags_refuse(self):
        _, document, _ = self.prepare()
        for changed in ("pointer", "flags", "mode"):
            bad = copy.deepcopy(document)
            if changed == "pointer":bad["records"][0]["before"]["image"]["pointer"] = "/missing"
            elif changed == "flags":bad["records"][0]["clean_exit"] = True
            else:Path(bad["records"][0]["after"]["path"]).chmod(0o644)
            with self.subTest(changed=changed), self.assertRaises(M.install.InstallError):self.validate(bad)

    def test_resource_lookup_failure_is_never_absence(self):
        owner = object.__new__(M.Upgrade);owner.docker = ["docker"]
        def failed(*args):raise M.install.InstallError("native tool unavailable")
        owner.command = failed
        with self.assertRaises(M.install.InstallError):owner._historical_resource("a" * 64)
        owner.command = lambda *args: ""
        self.assertIsNone(owner._historical_resource("a" * 64))

    def test_json_pointer_is_data_only_and_supports_retained_encoded_HTTP_body(self):
        raw = M.canonical({"/ready": {"body": json.dumps({"runtime": {"instance": "same"}})}})
        self.assertEqual(M.observation_value(raw, "/~1ready/body/runtime/instance"), "same")
        for pointer in ("execute()", "/bad~2", "/~1ready/missing"):
            with self.subTest(pointer=pointer), self.assertRaises(M.install.InstallError):M.observation_value(raw, pointer)


class WholeUpgradeTests(unittest.TestCase):
    """Execute the real finite host owner against an offline Docker/SQL fixture.

    Native authorization, PostgreSQL restore and GNU tar acceptance are covered
    independently; this fixture proves argv, ordering and interrupted readback.
    It performs no actual Docker, PostgreSQL or Provider operation.
    """
    setUp = UpgradeBoundaryTests.setUp
    running_fixture = UpgradeBoundaryTests.running_fixture
    fixture = staticmethod(UpgradeBoundaryTests.fixture)
    mount_inventory = staticmethod(UpgradeBoundaryTests.mount_inventory)

    def mounted_host(self, count):
        operation, state, calls, bind = self.host(count)
        inventory = self.mount_inventory()
        for index, name in enumerate(("mounted-content", "mounted-logs")):
            root = operation.directory / name
            root.mkdir(mode=0o700)
            (root / "synthetic.bin").write_bytes(b"retained mount content\n")
            (root / "synthetic.bin").chmod(0o600)
            inventory[index]["Source"] = str(root)
        for row in state.resources.values(): row["Mounts"] = copy.deepcopy(inventory)
        return operation, state, calls, bind

    def content_host(self, count, native):
        """Nonempty real files; the Go test supplies the existing native CLI."""
        operation, state, calls, bind = self.host(count)
        body, parent = operation.directory / "bound-body", operation.directory / "bound-token"
        body.mkdir(mode=0o700); parent.mkdir(mode=0o700)
        objects, token = body / "objects", parent / "binding.token"
        objects.mkdir(mode=0o700)
        identity = "whole-owner-native-binding"
        result = native(["init", "--root", str(objects), "--identity", identity, "--token-file", str(token)])
        self.assertEqual(0, result.returncode, "native synthetic binding initialization failed")
        raw = b"Nonempty retained whole-owner body; no customer or provider data.\n"
        name = hashlib.sha256(raw).hexdigest()
        (objects / name).write_bytes(raw); (objects / name).chmod(0o600)
        binding = token.read_bytes().strip()
        state.objects.append({"path": name, "bytes": len(raw), "digest": "sha256:" + name,
            "identity": identity, "binding_sha": hashlib.sha256(binding).hexdigest()})
        settings = {"SOURCE_CONTENT_STORAGE_ROOT": "/fixture-content/objects",
            "SOURCE_CONTENT_STORAGE_IDENTITY": identity, "SOURCE_CONTENT_STORAGE_TOKEN_FILE": "/fixture-secrets/binding.token"}
        state.running_env.update(settings)
        for role in operation.roles:
            state.resources["fixture-ur-" + role + "-1"]["Config"]["Env"].extend(key + "=" + value for key, value in settings.items())
        env_path = operation.runtime / ".env"
        text = env_path.read_text()
        for key, value in settings.items():
            text, count = re.subn(r"^" + key + r"=.*$", key + "=" + value, text, flags=re.M)
            if not count: text += "\n" + key + "=" + value + "\n"
        env_path.write_text(text)
        operation.env_raw = env_path.read_bytes(); operation.env = M.environment(operation.env_raw)
        original_run = operation.run
        def configured(args, *, label, input=None):
            result = original_run(args, label=label, input=input)
            if label == "check retained effective Compose":
                rendered = json.loads(result)
                for role in operation.roles: rendered["services"][role]["environment"].update(settings)
                return json.dumps(rendered)
            return result
        operation.run = configured
        def bind_content(target):
            bind(target); target.run = configured
            return target
        return operation, state, calls, bind_content, objects, token, raw

    def host(self, count, source_sequence=10):
        operation, state, calls = self.running_fixture(count, source_sequence)
        base_run = operation.run
        archive = OfflineTar()
        raw_ingress = b"synthetic ingress\n"
        if operation.edge_directory:
            (operation.edge_directory / "nginx.conf").write_bytes(raw_ingress)
        state.metadata["ingress"][0].update(desired_config_digest=hashlib.sha256(raw_ingress).hexdigest())
        members = []
        for row in state.metadata["generations"]:
            row.update(state="serving", ingress_state="receiving", lease_expires_at="2000-01-01T00:00:00Z")
            members.append({"instance": row["instance_id"], "artifact_digest": row["artifact_digest"]})
        state.metadata["ingress"][0]["membership_after"] = members
        old_artifact = state.metadata["generations"][0]["artifact_digest"]
        state.metadata["manifests"] = [{"artifact_digest": old_artifact, "manifest_digest": "f" * 64}]
        state.metadata["history"] = [dict(state.metadata["release"])]
        state.coverage = [{"coverage_epoch": 1, "ended_at": None, "state": "certified", "reason": "initial_certification"}]
        state.streams, state.objects = [], []
        state.business = [{"id": 1, "value": "synthetic preserved history"}]
        state.models = [{"id": 1, "name": "synthetic model"}]
        state.setups = [{"installation_id": "synthetic-paired-installation", "org_id": 2, "project_id": 2,
            "setup_state": "waiting_data", "setup_progress": {"paired": True, "grants": 1},
            "updated_at": "2026-10-01T00:00:00Z"}]
        state.sessions = [{"id": 1, "user_id": 1, "sid_hash": "synthetic-expired-hash",
            "expires_at": "2026-10-01T00:00:00Z"}, {"id": 2, "user_id": 1,
            "sid_hash": "synthetic-live-hash", "expires_at": "2099-01-01T00:00:00Z"}]
        state.clones, state.dumps, state.processes, state.resources = {}, {}, {}, {}
        state.failure = None
        state.native_calls = []
        new_ids = {"backend": "sha256:" + "d" * 64, "frontend": "sha256:" + "e" * 64}
        state.candidate_ids = new_ids
        state.hop = 0
        state.next_identity = 2
        source_calls = list(M.install.SERVICES) + (["backend-canary"] if count == 2 else []) + (["edge"] if count == 2 else [])
        for index, role in enumerate(source_calls, 101):
            name = "uniroute-nginx" if role == "edge" else "fixture-ur-" + role + "-1"
            value = M.install.strict_json(base_run(operation.docker + ["inspect", name], label="inspect owned upgrade resource"))[0]
            value.setdefault("Id", "container-edge"); value.setdefault("Image", "sha256:" + "9" * 64)
            value.setdefault("Mounts", [])
            value["State"].update(Running=True, Pid=index, ExitCode=0, OOMKilled=False, Health={"Status": "healthy"})
            state.processes[str(index)] = "old-start"
            state.resources[name] = value
        source_db = operation.env["POSTGRES_DB"]
        def database(db):
            if db == source_db:
                return {"metadata": state.metadata, "coverage": state.coverage, "streams": state.streams, "objects": state.objects, "business": state.business, "models": state.models, "setups": state.setups, "sessions":state.sessions}
            return state.clones[db]
        def tables(db):
            data = database(db); meta = data["metadata"]
            return {"business_history": data["business"], "model_deployments": data["models"], "xinsight_connector_installations": data["setups"], "admin_sessions":data["sessions"],
                "schema_migrations": [{"version": value} for value in meta["ledger"]],
                "runtime_generations": meta["generations"], "runtime_support_manifests": meta["manifests"],
                "runtime_ingress_changes": meta["ingress"], "release_state": [meta["release"]], "release_history": meta["history"],
                "execution_source_budget_coverage": data["coverage"], "execution_content_streams": data["streams"],
                "execution_content_objects": data["objects"]}
        def sql(query, db):
            if "pg_database_size" in query: return "1048576"
            if "rolcreatedb OR rolsuper" in query: return "t"
            if "pg_control_system()" in query: return json.dumps(database(db)["metadata"])
            if "'key_sha256'" in query and "FROM admin_sessions" in query:
                cutoff = re.search("expires_at<'([^']+)'",query)[1]
                rows = database(db)["sessions"]
                hashes = [hashlib.sha256(M.canonical(row)).hexdigest() for row in sorted(rows,key=M.canonical)]
                return json.dumps({"table":{"count":len(rows),"sha256":hashlib.sha256("".join(hashes).encode()).hexdigest()},
                    "rows":[{"key_sha256":hashlib.sha256(str(row["id"]).encode()).hexdigest(),
                    "row_sha256":hashlib.sha256(M.canonical(row)).hexdigest(),
                    "expired_before":datetime.datetime.fromisoformat(row["expires_at"].replace('Z','+00:00')) < datetime.datetime.fromisoformat(cutoff)} for row in rows]})
            if "'stable_sha256'" in query and "FROM xinsight_connector_installations" in query:
                identity = re.search("installation_id='([^']+)'", query)[1]
                row = next(row for row in database(db)["setups"] if row["installation_id"] == identity)
                return json.dumps({"row": copy.deepcopy(row), "stable_sha256": hashlib.sha256(M.canonical({k:v for k,v in row.items() if k != "updated_at"})).hexdigest(),
                    "progress_sha256": hashlib.sha256(M.canonical(row["setup_progress"])).hexdigest(), "clock": "2026-11-01T00:00:00Z"})
            if "FROM pg_tables" in query: return json.dumps(sorted(tables(db)))
            if "fingerprints;" in query:
                result = {}
                parts = query.split(" UNION ALL ")
                for name, rows in tables(db).items():
                    part = next(value for value in parts if 'FROM "' + name + '" t' in value)
                    values = copy.deepcopy(rows)
                    if name == "model_deployments" and "-'upstream_release_observation'" in part:
                        values = [{key: value for key, value in row.items() if key not in ("upstream_release_observation", "upstream_observation_revision")} for row in values]
                    if name == "schema_migrations" and "WHERE version NOT IN" in part:
                        excluded = re.findall("'([^']*)'",part.split("WHERE version NOT IN",1)[1])
                        values = [row for row in values if row["version"] not in excluded]
                    if name == "xinsight_connector_installations" and "CASE WHEN installation_id=" in part:
                        identity = re.search("CASE WHEN installation_id='([^']+)'", part)[1]
                        values = [{k:v for k,v in row.items() if k != "updated_at" or row["installation_id"] != identity} for row in values]
                    if name == "admin_sessions" and "NOT IN (" in part:
                        missing = re.findall("'([a-f0-9]{64})'",part)
                        values = [row for row in values if hashlib.sha256(str(row['id']).encode()).hexdigest() not in missing]
                    for key in ("instance_id", "artifact_digest", "change_id"):
                        match = re.search("WHERE " + key + r" IN \(([^)]+)\)", part)
                        if match: values = [row for row in values if row[key] in re.findall("'([^']*)'", match[1])]
                    match = re.search(r"WHERE release_sequence<=(\d+)", part)
                    if match: values = [row for row in values if row["release_sequence"] <= int(match[1])]
                    if name == "execution_content_streams" and "-'recovery_epoch'" in part:
                        values = [{key: value for key, value in row.items() if key != "recovery_epoch"} for row in values]
                    if name == "execution_source_budget_coverage" and "-'ended_at'" in part:
                        maximum = int(re.search(r"coverage_epoch<=(\d+)", part)[1])
                        values = [{key: value for key, value in row.items() if key != "ended_at"} for row in values if row["coverage_epoch"] <= maximum]
                    material = "".join(hashlib.sha256(M.canonical(row)).hexdigest() for row in sorted(values,key=M.canonical)).encode() if name == "admin_sessions" else M.canonical(sorted(values,key=M.canonical))
                    result[name] = {"count": len(values), "sha256": hashlib.sha256(material).hexdigest()}
                return json.dumps(result)
            if "final" not in query and "'active_generations'" in query:
                return json.dumps({"clients": 0, "prepared": 0, "unsettled": 0, "reserved": 0, "expired_ready": 0, "pending_capture": 0,
                    "active_generations": sum(row["state"] != "stopped" or row["ingress_state"] != "absent" for row in database(db)["metadata"]["generations"]),
                    "pending_ingress": sum(row["status"] != "applied" for row in database(db)["metadata"]["ingress"])})
            if "'streams'" in query and "'coverage'" in query:
                return json.dumps({"streams": database(db)["streams"], "coverage": database(db)["coverage"]})
            if "FROM execution_content_objects WHERE state='ready'" in query: return json.dumps(database(db)["objects"])
            if "SELECT count(*) FROM pg_database" in query: return "0"
            if "SELECT oid FROM pg_database" in query: return "901"
            if query.startswith("CREATE DATABASE "):
                clone = query.split('"')[1]; state.clones[clone] = None; return ""
            if "restore_rebase" in query and "DO $$" in query:
                data = database(db); data["coverage"][0]["ended_at"] = "synthetic-restore-boundary"
                data["coverage"].append({"coverage_epoch": 2, "ended_at": None, "state": "certified", "reason": "restore_rebase", "predecessor_epoch": 1, "restore_boundary": "synthetic-restore-boundary"})
                return ""
            if query.startswith("SELECT count(*) FROM model_deployments"): return "0"
            raise AssertionError("unexpected finite SQL query")
        def file_command(args, label, input_path, output_path):
            calls.append((label, list(args)))
            if output_path:
                M.install.private_file(output_path, b"synthetic native custom dump")
                state.dumps[str(output_path)] = copy.deepcopy(database(source_db))
            elif "--list" not in args:
                if "--section=pre-data" in args: return
                clone = args[args.index("-d")+1]
                if "--section=post-data" not in args:
                    state.clones[clone] = copy.deepcopy(state.dumps[str(input_path)])
        def run(args, *, label, input=None):
            if label in ("signed release verification", "verify retained source signature"):
                package = Path(args[args.index("--package") + 1])
                with tarfile.open(package) as incoming:
                    raw = incoming.extractfile("manifest.json").read(); manifest = json.loads(raw)
                return "verified=true\npackage_sha256=" + M.install.digest(package) + "\nmanifest_sha256=" + hashlib.sha256(raw).hexdigest() + "\n" + "\n".join(key + "=" + str(manifest[key]) for key in ("version", "commit", "target_schema", "release_sequence"))
            if label in ("check retained effective Compose", "check retained signed image bytes"):
                with tarfile.open(operation.source_package) as incoming:
                    references = json.load(incoming.extractfile(I.M.DESCRIPTOR))["images"]
                if label == "check retained signed image bytes":
                    role = next(role for role, ref in references.items() if ref == args[-1])
                    return json.dumps([{"Id": state.resources["fixture-ur-"+role+"-1"]["Image"], "Os":"linux", "Architecture":"amd64"}])
                expected_env = dict(state.running_env)
                expected_env["JWT_SECRET"] = M.environment((operation.runtime / ".env").read_bytes())["JWT_SECRET"]
                expected_env["RUNTIME_IMAGE_DIGEST"] = state.resources["fixture-ur-backend-1"]["Image"]
                return json.dumps({"services":{role:{"image":references["backend" if role in operation.roles else role],
                    "environment":dict(expected_env) if role in operation.roles else {}} for role in set(I.M.SERVICES)|set(operation.roles)}})
            if label == "check exact source writer readiness":
                row = next((row for row in state.resources.values() if row["Id"] == args[args.index("exec")+1]),None)
                if row is None:raise M.install.InstallError("synthetic source container no longer exists")
                active = [g for g in state.metadata["generations"] if g["state"] != "stopped" and g["artifact_digest"] == row["Image"]]
                index = 1 if row["Config"]["Labels"]["com.docker.compose.service"] == "backend-canary" and not state.duplicate_instance else 0
                return json.dumps({"status":"ok","runtime":{"artifact_digest":row["Image"],"instance":active[index]["instance_id"],"state":"serving"}})
            if label == "read exact installation metadata":
                calls.append((label, list(args)))
                return sql(input.decode(), args[-1])
            if label == "inspect owned upgrade resource":
                calls.append((label, list(args)))
                name = args[-1]
                row = state.resources.get(name) or next(value for value in state.resources.values() if value["Id"] == name)
                return json.dumps([row])
            if label == "read exact installed ingress configuration": return base64.b64encode(raw_ingress).decode()
            if label.startswith("acquire candidate "):
                calls.append((label, list(args))); return ""
            if label == "verify acquired candidate image":
                role = next(role for role in new_ids if operation.release.images[role] == args[-1])
                return json.dumps([{"Id": new_ids[role], "Os": "linux", "Architecture": "amd64", "RepoDigests": [args[-1]]}])
            if label == "read original process identities":
                row = next(row for row in state.resources.values() if row["Id"] == args[args.index("top") + 1])
                return "PID\n" + str(row["State"]["Pid"]) + "\n"
            if label.startswith("gracefully stop exact source "):
                calls.append((label, list(args)))
                role = label.rsplit(" ", 1)[1]
                if state.failure == "stop-unknown": raise M.install.InstallError("synthetic unknown stop result")
                name = "uniroute-nginx" if role == "edge" else "fixture-ur-" + role + "-1"
                row = state.resources[name]
                state.processes.pop(str(row["State"]["Pid"]))
                row["State"].update(Running=False, Pid=0)
                return ""
            if label.startswith("native upgrade owner: "):
                calls.append((label, list(args)))
                argv = args[args.index("./uniroute") + 1:]
                state.native_calls.append(argv)
                document = json.loads(input) if input else None
                if argv == ["runtime-generation", "support"]:
                    return json.dumps({"schema_heads": [M.SOURCE_SCHEMA, M.OBSERVATION_SCHEMA, M.TARGET_SCHEMA]}, separators=(",", ":"))
                if argv == ["runtime-generation", "prepare"]:
                    state.metadata["ingress"].append(dict(document, status="pending"))
                    before = {row["instance"] for row in document["membership_before"]}
                    after = {row["instance"] for row in document["membership_after"]}
                    for row in state.metadata["generations"]:
                        if row["instance_id"] in after: row.update(ingress_state="pending_join", revision=row["revision"] + 1)
                        elif row["instance_id"] in before: row.update(ingress_state="withdrawing", revision=row["revision"] + 1)
                    return "{}"
                if argv == ["runtime-generation", "complete"]:
                    plan = next(row for row in state.metadata["ingress"] if row["change_id"] == document["change_id"])
                    plan["status"] = "applied"
                    after = {row["instance"] for row in plan["membership_after"]}
                    for row in state.metadata["generations"]:
                        if row["ingress_state"] in ("withdrawing", "pending_join"):
                            row.update(ingress_state="receiving" if row["instance_id"] in after else "absent", revision=row["revision"] + 1)
                    return "{}"
                if argv == ["upgrade", "maintenance-check"]: return "{}"
                if argv == ["upgrade", "maintenance-retire"]:
                    for item in document["retirements"]:
                        row = next(row for row in state.metadata["generations"] if row["instance_id"] == item["instance_id"])
                        self.assertEqual(item["expected_revision"], row["revision"])
                    for row in state.metadata["generations"]: row.update(state="stopped", revision=row["revision"] + 1)
                    return "{}"
                if argv[:2] == ["upgrade", "restore-compat-check"] or argv[:2] == ["upgrade", "restore-compat-apply"]:
                    correction="ALTER FUNCTION public.valid_upstream_release_observation(jsonb) SET search_path TO pg_catalog, public;"
                    return json.dumps(dict(schema_version=document["schema_version"],status="applied" if argv[1].endswith("apply") else "eligible",
                        source_package_sha256=operation.witness["source"]["package_sha256"],clone_database=document["clone_database"],
                        clone_database_oid=document["clone_database_oid"],dump_sha256=document["dump_sha256"],correction_migration=M.TARGET_SCHEMA,
                        correction_sha256=hashlib.sha256(correction.encode()).hexdigest(),source_definition={"body_sha256":"synthetic","config":None},
                        restored_definition={"body_sha256":"synthetic","config":["search_path=pg_catalog, public"]}))
                if argv[:2] == ["fresh-install", "check"]: return "state=completed"
                if argv == ["migrate", "up"]:
                    expanding = state.metadata["ledger"][-1] == M.SOURCE_SCHEMA
                    if expanding:
                        for row in state.models: row.update(upstream_release_observation=None, upstream_observation_revision=0)
                    if state.failure == "migrate-after-observation":
                        state.metadata["ledger"].append(M.OBSERVATION_SCHEMA)
                        state.failure = None
                        raise Interrupted("native migration committed M128, M129 not started")
                    state.metadata["ledger"] = M.transition_ledger(state.metadata["ledger"])
                    return ""
                if argv == ["runtime-generation", "inspect"]: return "{}"
                if argv[:2] == ["upgrade", "record-applied"]:
                    self.assertIn("--expected-source-sequence", argv)
                    self.assertEqual(operation.witness["source"]["package_sha256"], argv[-1])
                    state.metadata["release"] = dict(operation.witness["candidate"], target_schema=M.TARGET_SCHEMA, trusted_key_id="fixture-release")
                    state.metadata["history"].append(dict(state.metadata["release"]))
                    return ""
                raise AssertionError("unexpected native finite owner")
            if label == "start exact candidate writers":
                calls.append((label, list(args)))
                state.hop += 1
                for index, role in enumerate(operation.roles):
                    name = "fixture-ur-" + role + "-1"; row = state.resources[name]
                    if "--force-recreate" in args or row["Image"] != new_ids["backend"]:
                        row.update(Id="new-" + str(state.hop) + "-" + role)
                    row["Image"] = new_ids["backend"]
                    deployed_env = dict(state.running_env, RUNTIME_IMAGE_DIGEST=new_ids["backend"])
                    row["Config"]["Env"] = [key+"="+value for key,value in deployed_env.items()]
                    row["State"].update(Running=True, Pid=201 + index, StartedAt="2026-10-29T00:00:00.123456789Z")
                    identity = "uri_" + format(state.next_identity + index, "x") * 32
                    state.processes[str(201 + index)] = "new-start-" + str(state.hop)
                    state.metadata["generations"].append({"instance_id": identity, "artifact_digest": new_ids["backend"], "manifest_digest": operation.witness["candidate_support_sha256"], "schema_head": M.TARGET_SCHEMA, "state": "serving", "ingress_state": "absent", "revision": 1,
                        "compatibility_epoch":"unirouter.runtime-compatibility.v1", "lease_expires_at":"2000-01-01T00:00:00Z"})
                existing = [m for m in state.metadata["manifests"] if m["artifact_digest"] == new_ids["backend"]]
                if existing:
                    self.assertTrue(all(m["manifest_digest"] == operation.witness["candidate_support_sha256"] for m in existing))
                else:
                    state.metadata["manifests"].append({"artifact_digest": new_ids["backend"], "manifest_digest": operation.witness["candidate_support_sha256"]})
                return ""
            if label == "check candidate writer readiness":
                row = next(row for row in state.resources.values() if row["Id"] == args[args.index("exec")+1])
                role = row["Config"]["Labels"]["com.docker.compose.service"]
                index = operation.roles.index(role)
                return json.dumps({"status": "ok", "runtime": {"state": "serving", "artifact_digest": new_ids["backend"], "instance": "uri_" + format(state.next_identity + index, "x") * 32,
                    "lease_remaining_seconds": 30, "schema_ledger_digest": operation.witness["receipts"]["migrate"]["ledger_sha256"], "support_manifest_digest": operation.witness["candidate_support_sha256"]}})
            if label in ("start exact candidate frontend", "start retained named edge"):
                calls.append((label, list(args)))
                name = "uniroute-nginx" if "edge" in label else "fixture-ur-frontend-1"
                pid = 302 if "edge" in label else 301
                row = state.resources[name]; row["State"].update(Running=True, Pid=pid)
                if "frontend" in label:
                    if "--force-recreate" in args or row["Image"] != new_ids["frontend"]:
                        row.update(Id="new-frontend-"+str(state.hop))
                    row["Image"] = new_ids["frontend"]
                state.processes[str(pid)] = "new-receiver-"+str(state.hop)
                return ""
            if args and args[0] == "tar": return archive.run(args, label=label, input=input)
            return base_run(args, label=label, input=input)
        def bind(target):
            nonlocal operation
            operation = target
            operation.run = run
            operation.test_file_command = file_command
            operation._process_start = lambda pid: state.processes.get(str(pid))
            return operation
        bind(operation)
        return operation, state, calls, bind

    def test_complete_single_and_dual_host_upgrade_uses_one_finite_owner(self):
        for count in (1, 2):
            with self.subTest(writers=count):
                operation, state, calls, _ = self.host(count)
                original_env = (operation.runtime / ".env").read_bytes()
                original_password = (operation.directory / "admin-password").read_bytes()
                with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                    result = operation.execute()
                self.assertEqual("completed", result["state"])
                self.assertEqual(original_env, (operation.runtime / ".env").read_bytes())
                self.assertEqual(original_password, (operation.directory / "admin-password").read_bytes())
                self.assertEqual(count, len(operation.witness["receipts"]["start-writers"]["writers"]))
                labels = [label for label, _ in calls]
                self.assertLess(labels.index("acquire candidate frontend image before withdrawal"), labels.index("gracefully stop exact source frontend"))
                self.assertLess(labels.index("native upgrade owner: upgrade maintenance-retire"), labels.index("write original cold database dump"))
                self.assertLess(labels.index("restore cold dump into independent database"), labels.index("native upgrade owner: migrate up"))
                self.assertEqual(1, state.native_calls.count(["migrate", "up"]))
                self.assertEqual(1, sum(argv[:2] == ["upgrade", "record-applied"] for argv in state.native_calls))

    def test_reordered_mounts_survive_whole_upgrade_interruption_and_resume_without_rewriting_witness(self):
        for count in (1, 2):
            for interrupted in (False, True):
                with self.subTest(writers=count, interrupted=interrupted):
                    operation, state, calls, bind = self.mounted_host(count)
                    original_run = operation.run
                    reads, reordered_reads, retained = {}, [], []
                    def reorder(args, *, label, input=None):
                        result = original_run(args, label=label, input=input)
                        if label == "inspect owned upgrade resource":
                            rows = M.install.strict_json(result)
                            for row in rows:
                                key = row["Id"]; reads[key] = reads.get(key, 0) + 1
                                if reads[key] % 2 == 0:
                                    row["Mounts"].reverse(); reordered_reads.append(key)
                            return json.dumps(rows)
                        return result
                    def checkpoint(point):
                        if point == "started:prepare-withdrawal":
                            retained.append(M.canonical(operation.witness["source_containers"]))
                        if interrupted and point == "effect:cold-pair": raise Interrupted()
                    operation.run, operation.test_checkpoint = reorder, checkpoint
                    with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                        if interrupted:
                            with self.assertRaises(Interrupted): operation.execute()
                            resumed = M.Upgrade(operation.directory, operation.release.directory, operation.verifier,
                                operation.public_keys, layout=operation.layout, edge_directory=operation.edge_directory, run=reorder)
                            bind(resumed); resumed.run = reorder
                            self.assertEqual("completed", resumed.execute(resume=True)["state"])
                            operation = resumed
                        else:
                            self.assertEqual("completed", operation.execute()["state"])
                        self.assertTrue(reordered_reads)
                        self.assertEqual([M.canonical(operation.witness["source_containers"])], retained)
                        completed = M.Upgrade(operation.directory, operation.release.directory, operation.verifier,
                            operation.public_keys, layout=operation.layout, edge_directory=operation.edge_directory, run=reorder)
                        bind(completed); completed.run = reorder
                        self.assertEqual("completed", completed.execute(resume=True)["state"])
                        self.assertEqual(retained[0], M.canonical(completed.witness["source_containers"]))

    def test_all_six_mount_guards_reject_real_record_or_resource_identity_drift(self):
        stages = [("acquisition", "native upgrade owner: runtime-generation support", "backend", 1),
                  ("before-stop", "started:stop-backend", "backend", 1),
                  ("after-stop", "gracefully stop exact source backend", "backend", 1),
                  ("new-writer", "start exact candidate writers", "backend", 1),
                  ("new-frontend", "start exact candidate frontend", "frontend", 1),
                  ("edge", "start retained named edge", "edge", 2)]
        for stage, trigger, role, count in stages:
            for drift in ("source", "read-only", "propagation", "container", "image"):
                with self.subTest(stage=stage, drift=drift):
                    operation, state, calls, _ = self.mounted_host(count)
                    original_run = operation.run
                    armed, observed = [], []
                    target = "uniroute-nginx" if role == "edge" else "fixture-ur-" + role + "-1"
                    def inspect_drift(args, *, label, input=None):
                        result = original_run(args, label=label, input=input)
                        if label == trigger: armed.append(True)
                        if armed and label == "inspect owned upgrade resource":
                            rows = M.install.strict_json(result)
                            if rows[0]["Id"] == state.resources[target]["Id"]:
                                row = rows[0]; observed.append(True)
                                if drift == "source": row["Mounts"][0]["Source"] = "/replacement/content"
                                elif drift == "read-only": row["Mounts"][0]["RW"] = False
                                elif drift == "propagation": row["Mounts"][0]["Propagation"] = "rshared"
                                elif drift == "image": row["Image"] = "sha256:" + "f" * 64
                                else: row["Id"] = operation.containers[role]["Id"] if stage in ("new-writer", "new-frontend") else "unexpected-container"
                                return json.dumps(rows)
                        return result
                    def checkpoint(point):
                        if point == trigger: armed.append(True)
                    operation.run, operation.test_checkpoint = inspect_drift, checkpoint
                    with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                        with self.assertRaises(M.install.InstallError): operation.execute()
                    self.assertTrue(armed); self.assertTrue(observed)
                    self.assertNotEqual("completed", operation.witness["state"])
                    if stage in ("acquisition", "before-stop", "after-stop"):
                        self.assertFalse(any(argv[:2] == ["upgrade", "maintenance-retire"] for argv in state.native_calls))
                    if stage == "acquisition":
                        self.assertFalse(any(label.startswith("gracefully stop") for label, _ in calls))
                    if stage == "before-stop":
                        self.assertFalse(any(label == "gracefully stop exact source backend" for label, _ in calls))
                    if stage == "new-writer":
                        self.assertFalse(any(argv[:2] == ["upgrade", "record-applied"] for argv in state.native_calls))
                    if stage in ("new-frontend", "edge"):
                        self.assertNotIn("admit-receivers", operation.witness["done"])

    def test_historical_union_freezes_originals_and_resumes_both_termination_kinds(self):
        for count, kind in ((1, "process_exit"), (2, "native_replacement")):
            with self.subTest(writers=count, kind=kind):
                operation, state, calls, bind = self.host(count)
                source = state.metadata["release"]
                path, document, generation = historical_fixture(operation.directory / "historical-inputs",
                    operation.state, state.metadata, source, kind, kind == "native_replacement")
                generation["lease_expires_at"] = "2000-01-01T00:00:00" + (".1" if count == 1 else ".12345") + "+00:00"
                with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                    with self.assertRaisesRegex(M.install.InstallError, "unaccounted"):
                        operation.check()  # Same real host entry without historical input remains strict.
                operation.historical_path, operation.historical_raw = path, path.read_bytes()
                operation._historical_resource = lambda _: None  # Synthetic transport only.
                def interrupt(point):
                    if point == "done:retire-writers": raise Interrupted()
                operation.test_checkpoint = interrupt
                with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                    with self.assertRaises(Interrupted): operation.execute()
                self.assertEqual(len(operation.witness["retirement_input"]["retirements"]), count + 1)
                self.assertTrue(all(row["state"] == "stopped" for row in state.metadata["generations"]))
                before = list(state.native_calls), (operation.directory / "install-state.json").read_bytes()
                original = Path(document["records"][0]["before"]["image"]["path"])
                raw = original.read_bytes(); original.write_bytes(b"changed original observation")
                def reopen():
                    current = self.reopen(operation, bind)
                    current._historical_resource = lambda _: None
                    return current
                with self.assertRaises(M.install.InstallError): reopen().execute(resume=True)
                self.assertEqual(before, (list(state.native_calls), (operation.directory / "install-state.json").read_bytes()))
                original.write_bytes(raw)
                completed = reopen()
                self.assertEqual(completed.execute(resume=True)["state"], "completed")
                self.assertEqual(reopen().execute(resume=True)["state"], "completed")
                self.assertEqual(1, sum(argv == ["upgrade", "maintenance-retire"] for argv in state.native_calls))
                self.assertEqual(next(row for row in state.metadata["generations"] if row["instance_id"] == generation["instance_id"])["revision"], 8)

    def test_historical_host_preflight_preserves_every_postgres_lease_precision(self):
        for count, kind in ((1, "process_exit"), (2, "native_replacement")):
            for digits in range(7):
                with self.subTest(writers=count, kind=kind, digits=digits):
                    operation, state, calls, _ = self.host(count)
                    path, _, generation = historical_fixture(operation.directory / "historical-inputs",
                        operation.state, state.metadata, state.metadata["release"], kind, kind == "native_replacement")
                    generation["lease_expires_at"] = "2000-01-01T00:00:00" + ("." + "1" * digits if digits else "") + "+00:00"
                    operation.historical_path, operation.historical_raw = path, path.read_bytes()
                    operation._historical_resource = lambda _: None  # Existing offline resource transport only.
                    before = {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in operation.directory.rglob("*") if p.is_file()}
                    before_metadata = copy.deepcopy(state.metadata)
                    with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                        result = operation.check()
                    self.assertEqual(count, result["writers"])
                    self.assertEqual(0, result["writes"])
                    self.assertEqual(before, {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in operation.directory.rglob("*") if p.is_file()})
                    self.assertEqual(before_metadata, state.metadata)
                    self.assertNotIn("upgrade", operation.state)

    def test_native_replacement_host_preflight_preserves_legacy_and_native_spacing(self):
        for count in (1, 2):
            for spacing in (" ", "  ", "\t"):
                with self.subTest(writers=count, spacing=repr(spacing)):
                    operation, state, calls, _ = self.host(count)
                    path, document, _ = historical_fixture(operation.directory / "historical-inputs",
                        operation.state, state.metadata, state.metadata["release"], "native_replacement", True)
                    reference = document["records"][0]["output"]; output = Path(reference["path"])
                    raw = ("\n".join(" Container" + spacing + "fixture-ur-backend-1" + spacing + status
                        for status in ("Recreated", "Started", "Healthy")) + "\n").encode()
                    output.write_bytes(raw); reference["sha256"] = M.install.digest(output); path.write_bytes(M.canonical(document))
                    operation.historical_path, operation.historical_raw = path, path.read_bytes()
                    operation._historical_resource = lambda _: None  # Existing offline native-resource transport.
                    before = {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in operation.directory.rglob("*") if p.is_file()}
                    before_metadata = copy.deepcopy(state.metadata)
                    with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                        result = operation.check()
                    self.assertEqual(count, result["writers"]); self.assertEqual(0, result["writes"])
                    self.assertEqual(before, {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in operation.directory.rglob("*") if p.is_file()})
                    self.assertEqual(before_metadata, state.metadata)
                    self.assertEqual(raw, operation.historical_sources[str(output)])
                    self.assertNotIn("upgrade", operation.state)

    def test_complete_union_limit_is_checked_before_maintenance_or_pull(self):
        operation, state, calls, _ = self.host(2)
        operation._historical = lambda *args: {"uri_" + str(i).zfill(32): object() for i in range(31)}
        before = (operation.directory / "install-state.json").read_bytes()
        with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
            with self.assertRaisesRegex(M.install.InstallError, "union exceeds"): operation.check()
        self.assertEqual(before, (operation.directory / "install-state.json").read_bytes())
        self.assertFalse(any("acquire candidate" in label or "gracefully stop" in label for label, _ in calls))

    def reopen(self, operation, bind):
        return bind(M.Upgrade(operation.directory, operation.release.directory, operation.verifier, operation.public_keys,
                              operation.source_package, operation.layout, operation.edge_directory, operation.run))

    def next_candidate(self, operation, state, bind, sequence, *, same_images=False):
        """Another synthetic package for the same existing host simulator."""
        directory = operation.directory.parent / ("candidate-" + str(sequence))
        shutil.copytree(operation.release.directory, directory)
        manifest = copy.deepcopy(operation.release.manifest)
        manifest.update(release_sequence=sequence, max_current_sequence=sequence-1,
                        version="0.0."+str(sequence), commit=hashlib.sha256(str(sequence).encode()).hexdigest()[:40])
        descriptor = json.loads((directory / I.M.DESCRIPTOR).read_bytes())
        if not same_images:
            for role in ("backend", "frontend"):
                digest = hashlib.sha256((str(sequence)+role).encode()).hexdigest()
                descriptor["images"][role] = "registry.example.invalid/"+role+"@sha256:"+digest
                state.candidate_ids[role] = "sha256:"+digest
        (directory / I.M.DESCRIPTOR).write_bytes(M.canonical(descriptor))
        with tarfile.open(directory / "release.tar.gz", "w:gz") as archive:
            for artifact in manifest["artifacts"]:
                raw = (directory / artifact["path"]).read_bytes()
                artifact.update(size=len(raw), sha256=hashlib.sha256(raw).hexdigest())
            wire = M.canonical(manifest)
            member = tarfile.TarInfo("manifest.json"); member.size = len(wire)
            archive.addfile(member, io.BytesIO(wire))
            for artifact in manifest["artifacts"]:
                raw = (directory / artifact["path"]).read_bytes()
                member = tarfile.TarInfo(artifact["path"]); member.size = len(raw)
                archive.addfile(member, io.BytesIO(raw))
        state.next_identity += len(operation.roles)
        state.running_env["RUNTIME_IMAGE_DIGEST"] = state.resources["fixture-ur-backend-1"]["Image"]
        return bind(M.Upgrade(operation.directory, directory, operation.verifier, operation.public_keys,
                             layout=operation.layout, edge_directory=operation.edge_directory, run=operation.run))

    def test_same_images_replace_app_containers_once_across_lost_receipts(self):
        for count in (1, 2):
            for interrupt in (None, "effect:start-writers", "effect:start-receivers"):
                with self.subTest(writers=count, interrupt=interrupt):
                    old, state, calls, bind = self.completed17(count)
                    before_ids = {role: state.resources["fixture-ur-" + role + "-1"]["Id"]
                                  for role in (*old.roles, "frontend")}
                    before_rows = copy.deepcopy((state.business, state.models, state.setups, state.sessions))
                    before_ledger = list(state.metadata["ledger"])
                    migrations = state.native_calls.count(["migrate", "up"])
                    calls_before = len(calls)
                    operation = self.next_candidate(old, state, bind, 20, same_images=True)
                    self.assertEqual(old.release.images, operation.release.images)
                    def stop(point):
                        if point == interrupt: raise Interrupted()
                    operation.test_checkpoint = stop
                    with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                        if interrupt:
                            with self.assertRaises(Interrupted): operation.execute()
                            operation = self.reopen(operation, bind)
                            self.assertEqual("completed", operation.execute(resume=True)["state"])
                        else:
                            self.assertEqual("completed", operation.execute()["state"])
                        completed = (operation.directory / "install-state.json").read_bytes()
                        self.assertEqual("completed", self.reopen(operation, bind).execute(resume=True)["state"])
                    self.assertEqual(completed, (operation.directory / "install-state.json").read_bytes())
                    self.assertEqual(2, state.hop, "Lost receipts must not recreate writers again")
                    starts = [(label, args) for label, args in calls[calls_before:]
                              if label in ("start exact candidate writers", "start exact candidate frontend")]
                    self.assertEqual(2, len(starts), "Each application start effect occurs once")
                    for _, args in starts:
                        for flag in ("--force-recreate", "--no-deps", "--no-build"):
                            self.assertIn(flag, args)
                        self.assertEqual("never", args[args.index("--pull") + 1])
                    for role, original_id in before_ids.items():
                        self.assertNotEqual(original_id, state.resources["fixture-ur-" + role + "-1"]["Id"])
                    self.assertEqual(before_ledger, state.metadata["ledger"])
                    self.assertEqual(migrations, state.native_calls.count(["migrate", "up"]))
                    self.assertEqual(before_rows, (state.business, state.models, state.setups, state.sessions))

    def test_same_image_original_restart_still_refuses_without_effects(self):
        for role in ("backend", "backend-canary", "frontend"):
            for pending in (False, True):
                with self.subTest(role=role, pending=pending):
                    old, state, calls, bind = self.completed17(2)
                    operation = self.next_candidate(old, state, bind, 20, same_images=True)
                    phase = "start-receivers" if role == "frontend" else "start-writers"
                    checkpoint = "started:" + phase if pending else (
                        "done:prepare-admission" if role == "frontend" else "done:configure-images")
                    def stop(point):
                        if point == checkpoint: raise Interrupted()
                    operation.test_checkpoint = stop
                    with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                        with self.assertRaises(Interrupted): operation.execute()
                    state.resources["fixture-ur-" + role + "-1"]["State"].update(Running=True, Pid=919)
                    path = operation.directory / "install-state.json"
                    original = path.read_bytes()
                    expected = json.loads(original)
                    if not pending:
                        # A fresh step records its existing intent before its
                        # action; refusal may retain only that pending marker.
                        expected["upgrade"]["pending"] = phase
                    before = list(state.native_calls), state.hop, copy.deepcopy((state.business, state.models, state.setups, state.sessions))
                    with self.assertRaises(M.install.InstallError):
                        self.reopen(operation, bind).execute(resume=True)
                    self.assertEqual(expected, json.loads(path.read_bytes()))
                    if pending: self.assertEqual(original, path.read_bytes())
                    self.assertEqual(before, (state.native_calls, state.hop, (state.business, state.models, state.setups, state.sessions)))

    def completed17(self, count=1):
        operation, state, calls, bind = self.host(count, source_sequence=16)
        with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
            self.assertEqual("completed", operation.execute()["state"])
        # This current-head synthetic application tests historical witness shape
        # (embedded source16 and opaque closed tool19), not actual M128 artifact
        # execution. Real legacy M128 restore is covered by native PG tests.
        raw = operation.source_snapshot(operation.witness)
        operation.witness.pop("source_state_path")
        operation.witness.update(source_state_bytes=base64.b64encode(raw).decode(), source_state=json.loads(raw),
                                 closed_recovery={"signed_tool_sequence":19,"application_sequence":17,"synthetic":True})
        M.install.save_state(operation.directory, operation.state)
        state.models[0].update(upstream_release_observation={"synthetic":"existing declared upstream version"}, upstream_observation_revision=7)
        return operation, state, calls, bind

    def legacy128_completed17(self, count=1):
        # Construct an explicit historical simulator input, not a claim that
        # today's M129 owner created a historical M128 application artifact.
        old,state,calls,bind=self.completed17(count)
        manifest=copy.deepcopy(old.release.manifest);manifest["target_schema"]=M.OBSERVATION_SCHEMA
        package=old.directory/"synthetic-historical-m128-17.tar.gz"
        with tarfile.open(package,"w:gz") as archive:
            wire=M.canonical(manifest);entry=tarfile.TarInfo("manifest.json");entry.size=len(wire);archive.addfile(entry,io.BytesIO(wire))
            for artifact in manifest["artifacts"]:
                wire=(old.release.directory/artifact["path"]).read_bytes();entry=tarfile.TarInfo(artifact["path"]);entry.size=len(wire);archive.addfile(entry,io.BytesIO(wire))
        package.chmod(0o600);sha=M.install.digest(package)
        state.metadata["ledger"].remove(M.TARGET_SCHEMA)
        state.metadata["release"].update(target_schema=M.OBSERVATION_SCHEMA,package_sha256=sha)
        for row in state.metadata["history"]:
            if row["release_sequence"]==17:row.update(target_schema=M.OBSERVATION_SCHEMA,package_sha256=sha)
        for row in state.metadata["generations"]:
            if row["schema_head"]==M.TARGET_SCHEMA:row["schema_head"]=M.OBSERVATION_SCHEMA
        old.state["current_release"].update(package=str(package),package_sha256=sha)
        old.state["package_sha256"]=sha
        old.witness["candidate"]["package_sha256"]=sha
        M.install.save_state(old.directory,old.state)
        return old,state,calls,bind

    def test_completed_source_checks_keep_receiving_writer_and_refuse_unaccounted_history(self):
        for legacy in (True, False):
            with self.subTest(legacy_m128=legacy):
                old, state, calls, bind = self.legacy128_completed17() if legacy else self.completed17()
                active = [row for row in state.metadata["generations"] if row["state"] != "stopped"]
                self.assertEqual(1, len(active))
                self.assertEqual("receiving", active[0]["ingress_state"])
                self.assertTrue(any(row["state"] == "stopped" for row in state.metadata["generations"]))
                operation = self.next_candidate(old, state, bind, 20)
                original = (old.directory / "install-state.json").read_bytes()
                before = copy.deepcopy(state.metadata)
                result = operation.check()
                self.assertEqual((0, 1), (result["writes"], result["writers"]))
                self.assertEqual(before, state.metadata)
                self.assertEqual(original, (old.directory / "install-state.json").read_bytes())
                # No historical input is needed for the current receiving
                # writer. An additional unaccounted old generation still fails.
                state.metadata["generations"].append(dict(active[0], instance_id="uri_" + "f" * 32,
                    ingress_state="absent", lease_expires_at="2000-01-01T00:00:00Z"))
                before = copy.deepcopy(state.metadata)
                with self.assertRaisesRegex(M.install.InstallError, "unaccounted"):
                    operation.check()
                self.assertEqual(before, state.metadata)
                self.assertEqual(original, (old.directory / "install-state.json").read_bytes())

    def test_legacy_m128_completed17_closed19_uses_bound_phased_restore_then_migrates_once(self):
        for count in (1,2):
            with self.subTest(writers=count):
                old,state,calls,bind=self.legacy128_completed17(count)
                observation=copy.deepcopy(state.models);original=(old.directory/"install-state.json").read_bytes()
                operation=self.next_candidate(old,state,bind,20)
                before=len(state.native_calls)
                def interrupt(point):
                    if point=="effect:cold-pair":raise Interrupted(point)
                operation.test_checkpoint=interrupt
                with patch.object(M.shutil,"disk_usage",return_value=SimpleNamespace(free=32<<30)):
                    with self.assertRaises(Interrupted):operation.execute()
                    cloned=copy.deepcopy(state.clones)
                    phase_calls=[(label,args) for label,args in calls if "pg_restore" in args and "--list" not in args]
                    self.assertEqual(["--section=pre-data","--section=data","--section=post-data"],
                        [next(arg for arg in args if arg.startswith("--section=")) for _,args in phase_calls[-3:]])
                    resumed=self.reopen(operation,bind)
                    self.assertEqual("completed",resumed.execute(resume=True)["state"])
                self.assertEqual(cloned,state.clones,"completed cold-pair readback does not replay partial restore")
                self.assertEqual(observation,state.models)
                self.assertEqual(original,operation.source_snapshot(operation.witness))
                self.assertEqual([M.SOURCE_SCHEMA,M.OBSERVATION_SCHEMA,M.TARGET_SCHEMA],state.metadata["ledger"])
                self.assertEqual(1,state.native_calls[before:].count(["migrate","up"]))
                restore_calls=[argv for argv in state.native_calls[before:] if argv[:1]==["upgrade"] and argv[1].startswith("restore-compat-")]
                self.assertEqual([["upgrade","restore-compat-check"],["upgrade","restore-compat-apply"]],restore_calls)
                self.assertIn(["fresh-install","check","--plan","/upgrade/evidence/source-install-plan.json","--completed-source-schema",M.OBSERVATION_SCHEMA],state.native_calls[before:])
                pair=resumed._pair();self.assertEqual("applied",pair["ddl_compatibility_delta"]["status"])
                self.assertEqual(M.OBSERVATION_SCHEMA,pair["source_metadata"]["ledger"][-1])

    def test_legacy_m128_partial_cold_pair_is_preserved_and_never_blind_replayed(self):
        old,state,calls,bind=self.legacy128_completed17()
        operation=self.next_candidate(old,state,bind,20)
        original=operation._files
        def partial(args,label,**kwargs):
            result=original(args,label,**kwargs)
            if "--section=pre-data" in args:raise Interrupted("partial pre-data restore")
            return result
        operation._files=partial
        with patch.object(M.shutil,"disk_usage",return_value=SimpleNamespace(free=32<<30)):
            with self.assertRaises(Interrupted):operation.execute()
        before=list(calls),list(state.native_calls)
        with self.assertRaisesRegex(M.install.InstallError,"result is unconfirmed"):
            self.reopen(operation,bind).execute(resume=True)
        self.assertEqual(before[1],state.native_calls)
        self.assertFalse(any("pg_restore" in args for _,args in calls[len(before[0]):]))
        self.assertEqual("cold-pair",operation.witness["pending"])

    def test_completed17_with_closed19_supports_second_and_repeated_same_schema_upgrade(self):
        for count in (1, 2):
            with self.subTest(writers=count):
                operation, state, calls, bind = self.completed17(count)
                observation = copy.deepcopy(state.models)
                for sequence in (20, 21):
                    original = (operation.directory / "install-state.json").read_bytes()
                    old_folder = operation.evidence
                    old_files = {str(p):p.read_bytes() for p in old_folder.rglob("*") if p.is_file()}
                    operation = self.next_candidate(operation, state, bind, sequence)
                    before = list(state.native_calls), {str(p):p.read_bytes() for p in operation.directory.rglob("*") if p.is_file()}
                    with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                        checked = operation.check()
                        self.assertEqual(sequence-1 if sequence==21 else 17, checked["source"]["release_sequence"])
                        self.assertEqual(before[1], {str(p):p.read_bytes() for p in operation.directory.rglob("*") if p.is_file()})
                        self.assertEqual("completed", operation.execute()["state"])
                    self.assertEqual(original, operation.source_snapshot(operation.witness))
                    self.assertNotIn("source_state_bytes", operation.witness)
                    self.assertNotIn("source_state", operation.witness)
                    self.assertLess(len((operation.directory / "install-state.json").read_bytes()), 1 << 20)
                    self.assertEqual(observation, state.models)
                    self.assertEqual([M.SOURCE_SCHEMA,M.OBSERVATION_SCHEMA,M.TARGET_SCHEMA], state.metadata["ledger"])
                    self.assertEqual("same_schema", operation.witness["receipts"]["migrate"]["mode"])
                    self.assertNotIn(["migrate","up"], state.native_calls[len(before[0]):])
                    self.assertEqual(old_files, {str(p):p.read_bytes() for p in old_folder.rglob("*") if p.is_file()})
                    native = list(state.native_calls)
                    self.assertEqual("completed", self.reopen(operation, bind).execute(resume=True)["state"])
                    self.assertEqual(native+[["runtime-generation","inspect"]], state.native_calls)

    def test_same_schema_interruptions_and_orphan_preparation_keep_original_bytes(self):
        for point in ("source-state-retained", "effect:migrate", "done:start-writers", "done:start-receivers"):
            with self.subTest(checkpoint=point):
                old, state, _, bind = self.completed17()
                original = (old.directory / "install-state.json").read_bytes()
                operation = self.next_candidate(old, state, bind, 20)
                def interrupt(name):
                    if name == point: raise Interrupted(point)
                operation.test_checkpoint = interrupt
                with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                    with self.assertRaises(Interrupted): operation.execute()
                if point == "source-state-retained":
                    self.assertEqual(original, (old.directory / "install-state.json").read_bytes())
                    orphans = {str(p):p.read_bytes() for p in (old.directory/"upgrades").rglob("source-install-state.json")}
                resumed = self.reopen(operation, bind)
                with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                    self.assertEqual("completed", resumed.execute(resume=point!="source-state-retained")["state"])
                if point == "source-state-retained":
                    for name, raw in orphans.items(): self.assertEqual(raw, Path(name).read_bytes())
                self.assertEqual(1, state.native_calls.count(["migrate","up"]), "only the original M127 expansion migrates")

    def test_known_native_migration_prefix_continues_with_full_cold_proof(self):
        operation,state,_,bind=self.host(1)
        state.failure="migrate-after-observation"
        with patch.object(M.shutil,"disk_usage",return_value=SimpleNamespace(free=32<<30)):
            with self.assertRaises(Interrupted):operation.execute()
            self.assertEqual([M.SOURCE_SCHEMA,M.OBSERVATION_SCHEMA],state.metadata["ledger"])
            self.assertEqual("migrate",operation.witness["pending"])
            self.assertEqual("completed",self.reopen(operation,bind).execute(resume=True)["state"])
        self.assertEqual([M.SOURCE_SCHEMA,M.OBSERVATION_SCHEMA,M.TARGET_SCHEMA],state.metadata["ledger"])
        self.assertEqual(2,state.native_calls.count(["migrate","up"]))

    def test_same_schema_mutation_or_missing_cold_proof_refuses_without_migration(self):
        for fault in ("observation", "schema_row", "expired_session", "cold_restore"):
            with self.subTest(fault=fault):
                old, state, _, bind = self.completed17()
                operation = self.next_candidate(old, state, bind, 20)
                def interrupt(point):
                    if point == "done:cold-pair": raise Interrupted(point)
                operation.test_checkpoint = interrupt
                with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                    with self.assertRaises(Interrupted): operation.execute()
                if fault == "observation": state.models[0]["upstream_observation_revision"] += 1
                elif fault == "schema_row": state.metadata["ledger"].pop()
                elif fault == "expired_session": state.sessions.pop(0)
                else: state.clones[operation.witness["verification_database"]]["business"][0]["value"]="unexpected restore mutation"
                before = list(state.native_calls)
                with self.assertRaises(M.install.InstallError): self.reopen(operation, bind).execute(resume=True)
                self.assertNotIn(["migrate","up"], state.native_calls[len(before):])

    def test_second_upgrade_binds_old_override_and_reconciles_atomic_replacement(self):
        for point in ("claimed","prepared:configure-images","replaced:configure-images","effect:configure-images"):
            with self.subTest(checkpoint=point):
                old,state,_,bind=self.completed17()
                path=old.runtime/"upgrade-image.json"
                original=path.read_bytes()
                operation=self.next_candidate(old,state,bind,20)
                def interrupt(name):
                    if name==point:raise Interrupted(point)
                operation.test_checkpoint=interrupt
                with patch.object(M.shutil,"disk_usage",return_value=SimpleNamespace(free=32<<30)):
                    with self.assertRaises(Interrupted):operation.execute()
                self.assertEqual(original,base64.b64decode(operation.witness["source_image_override"]["bytes"]))
                if point in ("claimed","prepared:configure-images"):self.assertEqual(original,path.read_bytes())
                else:self.assertEqual(M.canonical(operation._image_override()),path.read_bytes())
                resumed=self.reopen(operation,bind)
                with patch.object(M.shutil,"disk_usage",return_value=SimpleNamespace(free=32<<30)):
                    self.assertEqual("completed",resumed.execute(resume=True)["state"])
                self.assertNotEqual(original,path.read_bytes())
                self.assertEqual(1,state.native_calls.count(["migrate","up"]))

    def test_snapshot_binding_and_preparing_override_drift_fail_before_service_effects(self):
        for fault in ("snapshot_hash","snapshot_mode","snapshot_path","snapshot_symlink","directory_symlink","old_override"):
            with self.subTest(fault=fault):
                old,state,_,bind=self.completed17()
                operation=self.next_candidate(old,state,bind,20)
                def interrupt(point):
                    if point=="claimed":raise Interrupted(point)
                operation.test_checkpoint=interrupt
                with patch.object(M.shutil,"disk_usage",return_value=SimpleNamespace(free=32<<30)):
                    with self.assertRaises(Interrupted):operation.execute()
                path=operation.directory/operation.witness["source_state_path"]
                if fault=="snapshot_hash":path.write_bytes(b"{}")
                elif fault=="snapshot_mode":path.chmod(0o644)
                elif fault=="snapshot_path":
                    operation.witness["source_state_path"]="../outside-source.json"
                    M.install.save_state(operation.directory,operation.state)
                elif fault=="snapshot_symlink":
                    target=path.with_suffix(".retained");path.rename(target);path.symlink_to(target)
                elif fault=="directory_symlink":
                    parent=path.parent;target=parent.with_name(parent.name+"-retained");parent.rename(target);parent.symlink_to(target,target_is_directory=True)
                else:(operation.runtime/"upgrade-image.json").write_bytes(b"{}")
                before=list(state.native_calls)
                with self.assertRaises(M.install.InstallError):self.reopen(operation,bind).execute(resume=True)
                self.assertEqual(before,state.native_calls)

    def test_every_flat_checkpoint_resumes_with_native_readback(self):
        for count in (1, 2):
            baseline, _, _, _ = self.host(count)
            checkpoints = []
            baseline.test_checkpoint = checkpoints.append
            with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                baseline.execute()
            for point in checkpoints:
                with self.subTest(writers=count, checkpoint=point):
                    operation, state, calls, bind = self.host(count)
                    def interrupt(name):
                        if name == point: raise Interrupted(point)
                    operation.test_checkpoint = interrupt
                    with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                        with self.assertRaises(Interrupted): operation.execute()
                    resumed = self.reopen(operation, bind)
                    result = resumed.execute(resume=point != "source-state-retained")
                    self.assertEqual("completed", result["state"])
                    self.assertEqual(1, state.native_calls.count(["migrate", "up"]))
                    self.assertEqual(1, sum(argv[:2] == ["upgrade", "record-applied"] for argv in state.native_calls))

    def test_populated_startup_interruptions_preserve_setup_and_sessions_without_exceptions(self):
        # Owner simulation only: actual Go workers/native PG are tested in Go;
        # the Linux fixture separately crosses the real cleanup timer.
        for count in (1, 2):
            for point in ("effect:start-writers", "done:prepare-admission", "done:start-receivers"):
                with self.subTest(writers=count, checkpoint=point):
                    operation, state, _, bind = self.host(count)
                    original = copy.deepcopy((state.setups, state.sessions))
                    def interrupt(name):
                        if name == point: raise Interrupted(point)
                    operation.test_checkpoint = interrupt
                    with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                        with self.assertRaises(Interrupted): operation.execute()
                    prefix = copy.deepcopy(operation.witness["receipts"])
                    resumed = self.reopen(operation, bind)
                    self.assertEqual("completed", resumed.execute(resume=True)["state"])
                    self.assertEqual(original, (state.setups, state.sessions))
                    for step, receipt in prefix.items():
                        self.assertEqual(receipt, resumed.witness["receipts"][step])
                    self.assertEqual(1, state.native_calls.count(["migrate", "up"]))

    def test_held_protected_mutations_still_refuse_strict_resume(self):
        for kind in ("setup_clock", "setup_progress", "expired_session_delete", "unrelated_history"):
            with self.subTest(mutation=kind):
                operation, state, _, bind = self.host(1)
                def interrupt(point):
                    if point == "done:start-writers": raise Interrupted(point)
                operation.test_checkpoint = interrupt
                with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                    with self.assertRaises(Interrupted): operation.execute()
                if kind == "setup_clock": state.setups[0]["updated_at"] = "2026-11-01T00:00:00Z"
                elif kind == "setup_progress": state.setups[0]["setup_progress"]["unexpected"] = True
                elif kind == "expired_session_delete": state.sessions.pop(0)
                else: state.business[0]["value"] = "unrelated mutation while held"
                before = list(state.native_calls)
                with self.assertRaises(M.install.InstallError): self.reopen(operation, bind).execute(resume=True)
                self.assertFalse(any(argv[:2] == ["upgrade", "record-applied"] for argv in state.native_calls[len(before):]))

    def test_live_old_lease_waits_without_claiming_retirement_pending(self):
        operation, state, _, bind = self.host(2)
        for row in state.metadata["generations"]: row["lease_expires_at"] = "2099-01-01T00:00:00Z"
        with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
            with self.assertRaisesRegex(M.install.InstallError, "lease is still live"): operation.execute()
        self.assertIsNone(operation.witness["pending"])
        self.assertNotIn("retirement_input", operation.witness)
        self.assertFalse(any(argv == ["upgrade", "maintenance-retire"] for argv in state.native_calls))
        for row in state.metadata["generations"]: row["lease_expires_at"] = "2000-01-01T00:00:00Z"
        self.assertEqual("completed", self.reopen(operation, bind).execute(resume=True)["state"])

    def test_unknown_physical_stop_is_not_repeated_or_treated_as_expired_lease(self):
        operation, state, calls, bind = self.host(2)
        state.failure = "stop-unknown"
        with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
            with self.assertRaisesRegex(M.install.InstallError, "unknown stop"): operation.execute()
        stopped = sum(label.startswith("gracefully stop exact source ") for label, _ in calls)
        state.failure = None
        with self.assertRaisesRegex(M.install.InstallError, "result is unconfirmed"):
            self.reopen(operation, bind).execute(resume=True)
        self.assertEqual(stopped, sum(label.startswith("gracefully stop exact source ") for label, _ in calls))
        self.assertFalse(any(argv == ["upgrade", "maintenance-retire"] for argv in state.native_calls))

    def test_completed_default_resume_preserves_new_legitimate_business_writes(self):
        operation, state, calls, bind = self.host(2)
        def after_admission(point):
            if point == 'effect:admit-receivers':
                state.business.append({'id': 2, 'value': 'legitimate new business data'})
                state.models[0].update(upstream_release_observation={'synthetic': 'new valid declaration'}, upstream_observation_revision=1)
        operation.test_checkpoint = after_admission
        with patch.object(M.shutil, 'disk_usage', return_value=SimpleNamespace(free=32 << 30)):
            self.assertEqual('completed', operation.execute()['state'])
        before_native = list(state.native_calls)
        before_labels = [label for label, _ in calls]
        # Exercise the actual default, without an explicit old --source-package.
        resumed = bind(M.Upgrade(operation.directory, operation.release.directory, operation.verifier, operation.public_keys,
                                 layout=operation.layout, edge_directory=operation.edge_directory, run=operation.run))
        self.assertEqual('completed', resumed.execute(resume=True)['state'])
        self.assertEqual(2, len(state.business))
        self.assertEqual(1, state.models[0]['upstream_observation_revision'])
        self.assertEqual(before_native + [['runtime-generation', 'inspect']], state.native_calls)
        later = [label for label, _ in calls][len(before_labels):]
        self.assertFalse(any(label.startswith(('gracefully stop', 'start exact', 'acquire candidate')) for label in later))

    def test_pull_time_configuration_or_fleet_drift_stops_before_withdrawal(self):
        for kind in ('env', 'edge', 'fleet', 'head'):
            with self.subTest(kind=kind):
                operation, state, calls, _ = self.host(2)
                original_run = operation.run
                def drift(args, *, label, input=None):
                    result = original_run(args, label=label, input=input)
                    if label == 'acquire candidate frontend image before withdrawal':
                        if kind == 'env':
                            with (operation.runtime / '.env').open('ab') as stream: stream.write(b'\nNEW_OPERATOR_SETTING=true\n')
                        elif kind == 'edge':
                            with (operation.edge_directory / 'runtime-overrides.json').open('ab') as stream: stream.write(b'\n')
                        elif kind == 'fleet':
                            extra = dict(state.metadata['generations'][0]); extra['instance_id'] = 'uri_' + 'f' * 32
                            state.metadata['generations'].append(extra)
                        else:
                            state.metadata['ingress'][0]['change_id'] = 'concurrent-different-native-head'
                    return result
                operation.run = drift
                with patch.object(M.shutil, 'disk_usage', return_value=SimpleNamespace(free=32 << 30)):
                    with self.assertRaises(M.install.InstallError): operation.execute()
                self.assertFalse(any(argv == ['runtime-generation', 'prepare'] for argv in state.native_calls))
                self.assertFalse(any(label.startswith('gracefully stop') for label, _ in calls))
                self.assertFalse(any(argv == ['migrate', 'up'] for argv in state.native_calls))

    def test_resume_configuration_drift_refuses_before_any_further_mutation(self):
        operation, _, calls, bind = self.host(2)
        def interrupt(point):
            if point == 'done:prepare-withdrawal': raise Interrupted(point)
        operation.test_checkpoint = interrupt
        with patch.object(M.shutil, 'disk_usage', return_value=SimpleNamespace(free=32 << 30)):
            with self.assertRaises(Interrupted): operation.execute()
        with (operation.edge_directory / 'runtime-overrides.json').open('ab') as stream: stream.write(b'\n')
        writes = [label for label, _ in calls if label.startswith(('native upgrade owner:', 'gracefully stop', 'start exact'))]
        resumed = self.reopen(operation, bind)
        with self.assertRaisesRegex(M.install.InstallError, 'configuration changed'):
            resumed.execute(resume=True)
        self.assertEqual(writes, [label for label, _ in calls if label.startswith(('native upgrade owner:', 'gracefully stop', 'start exact'))])

    def test_completed_compose_or_override_drift_refuses_before_native_carrier(self):
        for kind in ('compose', 'override'):
            with self.subTest(kind=kind):
                operation, state, calls, bind = self.host(2)
                with patch.object(M.shutil, 'disk_usage', return_value=SimpleNamespace(free=32 << 30)):
                    self.assertEqual('completed', operation.execute()['state'])
                before = list(state.native_calls)
                path = operation.runtime / ('docker-compose.yml' if kind == 'compose' else 'upgrade-image.json')
                with path.open('ab') as stream: stream.write(b'\n')
                resumed = bind(M.Upgrade(operation.directory, operation.release.directory, operation.verifier, operation.public_keys,
                                         layout=operation.layout, edge_directory=operation.edge_directory, run=operation.run))
                with self.assertRaisesRegex(M.install.InstallError, 'configuration changed|override changed'):
                    resumed.execute(resume=True)
                self.assertEqual(before, state.native_calls)
                self.assertEqual('completed', resumed.status()['state'])

def native_content_restore_cases(native):
    """Go tests always exercise this native boundary; early Python needs no Go.

    Existing offline Docker/SQL transport maps the view to real restored files.
    The existing CLI process actually initializes/checks every binding. Real
    Linux acceptance separately qualifies tmpfs and read-only OCI mounts.
    """
    reports = []
    for count in (1, 2):
        for token_layout in ("directory", "file"):
            for failure in (None, "parent", "identity", "token", "mode"):
                case = WholeUpgradeTests(methodName="test_complete_single_and_dual_host_upgrade_uses_one_finite_owner")
                try:
                    case.setUp()
                    operation, state, calls, bind, objects, token, raw = case.content_host(count, native)
                    for role in operation.roles:
                        state.resources["fixture-ur-" + role + "-1"]["Mounts"] = [
                            {"Type": "bind", "Source": str(objects.parent), "Destination": "/fixture-content", "RW": True},
                            {"Type": "bind", "Source": str(token.parent if token_layout == "directory" else token),
                             "Destination": "/fixture-secrets" if token_layout == "directory" else "/fixture-secrets/binding.token", "RW": False}]
                    before = M.recovery.filemeta(token), M.recovery.filemeta(objects / hashlib.sha256(raw).hexdigest())
                    original_run = operation.run
                    checked = []
                    def run(args, *, label, input=None):
                        if label != "native restored content binding check":
                            return original_run(args, label=label, input=input)
                        case.assertIn("--read-only", args)
                        case.assertEqual("none", args[args.index("--network") + 1])
                        case.assertEqual("never", args[args.index("--pull") + 1])
                        parent, options = args[args.index("--tmpfs") + 1].split(":", 1)
                        options = dict((value.split("=", 1) if "=" in value else (value, True)) for value in options.split(","))
                        case.assertEqual({"rw": True, "noexec": True, "nosuid": True, "nodev": True,
                                          "size": "65536", "mode": "0700"}, options)
                        command = args[args.index("./uniroute") + 1:]
                        case.assertEqual(["content-store", "check"], command[:2])
                        flags = dict(zip(command[2::2], command[3::2]))
                        case.assertEqual({"--root", "--identity", "--token-file"}, set(flags))
                        case.assertEqual(parent, str(Path(flags["--token-file"]).parent))
                        volumes = {}
                        for index, value in enumerate(args):
                            if value == "--volume":
                                host, destination, mode = args[index + 1].rsplit(":", 2)
                                case.assertEqual("ro", mode); volumes[destination] = Path(host)
                        case.assertEqual({flags["--root"], flags["--token-file"]}, set(volumes))
                        restored_root, restored_token = volumes[flags["--root"]], volumes[flags["--token-file"]]
                        original_inventory = M.paired_file_inventory(operation._recovery_roots())["files"]
                        case.assertEqual(token_layout == "directory", str(token.parent).lstrip("/") in original_inventory)
                        frozen = M.recovery.filemeta(restored_token), M.recovery.filemeta(restored_root / hashlib.sha256(raw).hexdigest())
                        with tempfile.TemporaryDirectory(prefix="content-view-", dir=operation.directory.parent) as directory:
                            view = Path(directory).resolve()
                            view.chmod(0o755 if failure == "parent" else int(options["mode"], 8))
                            view_token = view / Path(flags["--token-file"]).name
                            if failure == "token":
                                value = restored_token.read_bytes().strip()
                                different = (b"0" if value[:1] != b"0" else b"1") + value[1:]
                                view_token.write_bytes(different + b"\n"); view_token.chmod(0o600)
                            elif failure == "mode":
                                view_token.write_bytes(restored_token.read_bytes()); view_token.chmod(0o644)
                            else:
                                view_token.hardlink_to(restored_token)
                            native_args = ["check", "--root", str(restored_root), "--identity",
                                "wrong-synthetic-identity" if failure == "identity" else flags["--identity"],
                                "--token-file", str(view_token)]
                            result = native(native_args)
                        case.assertEqual(frozen, (M.recovery.filemeta(restored_token), M.recovery.filemeta(restored_root / hashlib.sha256(raw).hexdigest())))
                        checked.append(result.returncode)
                        if result.returncode:
                            raise M.install.InstallError("native restored content binding check failed; native output kept private")
                        return result.stdout.decode()
                    operation.run = run
                    with patch.object(M.shutil, "disk_usage", return_value=SimpleNamespace(free=32 << 30)):
                        if failure:
                            with case.assertRaisesRegex(M.install.InstallError, "native restored content binding check failed"):
                                operation.execute()
                            case.assertEqual("cold-pair", operation.witness["pending"])
                            case.assertFalse(any(argv == ["migrate", "up"] for argv in state.native_calls))
                        else:
                            case.assertEqual("completed", operation.execute()["state"])
                            resumed = M.Upgrade(operation.directory, operation.release.directory, operation.verifier,
                                operation.public_keys, layout=operation.layout, edge_directory=operation.edge_directory, run=operation.run)
                            bind(resumed)
                            case.assertEqual("completed", resumed.execute(resume=True)["state"])
                    case.assertEqual([1] if failure else [0], checked)
                    case.assertEqual(before, (M.recovery.filemeta(token), M.recovery.filemeta(objects / hashlib.sha256(raw).hexdigest())))
                    reports.append({"writers": count, "token_layout": token_layout, "failure": failure,
                                    "native_binding_checks": len(checked), "original_and_restored_inputs_unchanged": True})
                finally:
                    case.doCleanups()
    return reports


if __name__ == "__main__": unittest.main()
