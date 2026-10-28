#!/usr/bin/env python3
"""Finite host-boundary tests. Docker and service operations are never executed."""
import base64
import copy
import importlib.util
import json
import hashlib
import io
import re
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

    def test_claim_retains_original_bytes_and_revokes_old_package_binding(self):
        installation = M.Installation(self.installer.directory, self.installer.fake.run)
        original = installation.state_raw
        installation.claim({"package_sha256": self.installer.release.sha}, {"package_sha256": "f" * 64})
        witness = installation.state["upgrade"]
        self.assertEqual(original, base64.b64decode(witness["source_state_bytes"]))
        self.assertEqual("f" * 64, installation.state["package_sha256"])
        self.assertEqual(self.installer.release.sha, M.install.digest(self.installer.directory / "release.tar.gz"))
        with self.assertRaisesRegex(M.install.InstallError, "already owns"):
            installation.claim({}, {"package_sha256": "e" * 64})
        with self.assertRaisesRegex(I.M.InstallError, "upgrade witness"):
            self.installer.installer.load_state()

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

    def running_fixture(self, count):
        fixture = I.InstallerTests(methodName="test_check_is_read_only")
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        data = fixture.fixture
        data.manifest.update(version="1.15.0", release_sequence=11, min_current_sequence=0,
                             max_current_sequence=10, min_current_schema=M.SOURCE_SCHEMA, key_id="fixture-release")
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
        source_manifest = {"version": "1.14.2", "commit": "a" * 40, "release_sequence": 10,
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
        metadata["release"].update(version="1.14.2", commit="a" * 40, package_sha256=old_sha, trusted_key_id="fixture-release")
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
        original = {"state": "completed", "plan": {"installation_id": "ufi_" + "a" * 32, "digest": "d" * 64, "target": {"environment": "production"}}}
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
        record["output"] = reference("operation.stderr", ("\n".join(" Container " + name + " " + state
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

    def host(self, count):
        operation, state, calls = self.running_fixture(count)
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
        state.streams = []
        state.business = [{"id": 1, "value": "synthetic preserved history"}]
        state.models = [{"id": 1, "name": "synthetic model"}]
        state.clones, state.dumps, state.processes, state.resources = {}, {}, {}, {}
        state.failure = None
        state.native_calls = []
        new_ids = {"backend": "sha256:" + "d" * 64, "frontend": "sha256:" + "e" * 64}
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
                return {"metadata": state.metadata, "coverage": state.coverage, "streams": state.streams, "business": state.business, "models": state.models}
            return state.clones[db]
        def tables(db):
            data = database(db); meta = data["metadata"]
            return {"business_history": data["business"], "model_deployments": data["models"],
                "schema_migrations": [{"version": value} for value in meta["ledger"]],
                "runtime_generations": meta["generations"], "runtime_support_manifests": meta["manifests"],
                "runtime_ingress_changes": meta["ingress"], "release_state": [meta["release"]], "release_history": meta["history"],
                "execution_source_budget_coverage": data["coverage"], "execution_content_streams": data["streams"]}
        def sql(query, db):
            if "pg_database_size" in query: return "1048576"
            if "rolcreatedb OR rolsuper" in query: return "t"
            if "pg_control_system()" in query: return json.dumps(database(db)["metadata"])
            if "FROM pg_tables" in query: return json.dumps(sorted(tables(db)))
            if "fingerprints;" in query:
                result = {}
                parts = query.split(" UNION ALL ")
                for name, rows in tables(db).items():
                    part = next(value for value in parts if 'FROM "' + name + '" t' in value)
                    values = copy.deepcopy(rows)
                    if name == "model_deployments" and "-'upstream_release_observation'" in part:
                        values = [{key: value for key, value in row.items() if key not in ("upstream_release_observation", "upstream_observation_revision")} for row in values]
                    if name == "schema_migrations" and "WHERE version<>" in part:
                        values = [row for row in values if row["version"] != M.TARGET_SCHEMA]
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
                    result[name] = {"count": len(values), "sha256": hashlib.sha256(M.canonical(sorted(values, key=lambda row: M.canonical(row)))).hexdigest()}
                return json.dumps(result)
            if "final" not in query and "'active_generations'" in query:
                return json.dumps({"clients": 0, "prepared": 0, "unsettled": 0, "reserved": 0, "expired_ready": 0, "pending_capture": 0,
                    "active_generations": sum(row["state"] != "stopped" or row["ingress_state"] != "absent" for row in database(db)["metadata"]["generations"]),
                    "pending_ingress": sum(row["status"] != "applied" for row in database(db)["metadata"]["ingress"])})
            if "'streams'" in query and "'coverage'" in query:
                return json.dumps({"streams": database(db)["streams"], "coverage": database(db)["coverage"]})
            if "FROM execution_content_objects WHERE state='ready'" in query: return "[]"
            if "SELECT count(*) FROM pg_database" in query: return "0"
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
                state.clones[args[-1]] = copy.deepcopy(state.dumps[str(input_path)])
        def run(args, *, label, input=None):
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
                    return json.dumps({"schema_heads": [M.SOURCE_SCHEMA, M.TARGET_SCHEMA]}, separators=(",", ":"))
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
                if argv == ["fresh-install", "check", "--plan", "/upgrade/evidence/source-install-plan.json"]: return "state=completed"
                if argv == ["migrate", "up"]:
                    state.metadata["ledger"].append(M.TARGET_SCHEMA)
                    for row in state.models: row.update(upstream_release_observation=None, upstream_observation_revision=0)
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
                for index, role in enumerate(operation.roles):
                    name = "fixture-ur-" + role + "-1"; row = state.resources[name]
                    row.update(Id="new-" + role, Image=new_ids["backend"])
                    row["State"].update(Running=True, Pid=201 + index)
                    identity = "uri_" + ("c" if index == 0 else "d") * 32
                    state.metadata["generations"].append({"instance_id": identity, "artifact_digest": new_ids["backend"], "manifest_digest": operation.witness["candidate_support_sha256"], "schema_head": M.TARGET_SCHEMA, "state": "serving", "ingress_state": "absent", "revision": 1})
                state.metadata["manifests"].append({"artifact_digest": new_ids["backend"], "manifest_digest": operation.witness["candidate_support_sha256"]})
                return ""
            if label == "check candidate writer readiness":
                role = args[args.index("exec") + 1].removeprefix("new-")
                index = operation.roles.index(role)
                return json.dumps({"status": "ok", "runtime": {"state": "serving", "artifact_digest": new_ids["backend"], "instance": "uri_" + ("c" if index == 0 else "d") * 32,
                    "lease_remaining_seconds": 30, "schema_ledger_digest": operation.witness["receipts"]["migrate"]["ledger_sha256"], "support_manifest_digest": operation.witness["candidate_support_sha256"]}})
            if label in ("start exact candidate frontend", "start retained named edge"):
                calls.append((label, list(args)))
                name = "uniroute-nginx" if "edge" in label else "fixture-ur-frontend-1"
                row = state.resources[name]; row["State"].update(Running=True, Pid=301)
                if "frontend" in label: row.update(Id="new-frontend", Image=new_ids["frontend"])
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

    def test_historical_union_freezes_originals_and_resumes_both_termination_kinds(self):
        for count, kind in ((1, "process_exit"), (2, "native_replacement")):
            with self.subTest(writers=count, kind=kind):
                operation, state, calls, bind = self.host(count)
                source = state.metadata["release"]
                path, document, generation = historical_fixture(operation.directory / "historical-inputs",
                    operation.state, state.metadata, source, kind, kind == "native_replacement")
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
                    result = resumed.execute(resume=True)
                    self.assertEqual("completed", result["state"])
                    self.assertEqual(1, state.native_calls.count(["migrate", "up"]))
                    self.assertEqual(1, sum(argv[:2] == ["upgrade", "record-applied"] for argv in state.native_calls))

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

if __name__ == "__main__": unittest.main()
