#!/usr/bin/env python3
"""Upgrade one retained Linux community installation using a full signed release.

The native upgrade and runtime-generation owners decide database authority.
This host entry owns only its installation, Docker processes and paired backup.
"""
import argparse
import base64
import binascii
from collections import namedtuple
import copy
import datetime
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import re
import shutil
import stat
import subprocess
import sys
import tarfile
import uuid

_SPEC = importlib.util.spec_from_file_location("community_install", Path(__file__).with_name("install.py"))
install = importlib.util.module_from_spec(_SPEC)
_SPEC.loader.exec_module(install)
_RECOVERY_SPEC = importlib.util.spec_from_file_location("community_recovery", Path(__file__).with_name("recovery.py"))
recovery = importlib.util.module_from_spec(_RECOVERY_SPEC)
_RECOVERY_SPEC.loader.exec_module(recovery)
require = install.require
SCHEMA = "unirouter.community-upgrade.v1"
SOURCE_SCHEMA = "000127_xinsight_communication_policy"
TARGET_SCHEMA = install.SCHEMA
_NOT_STARTED = object()
_HistoricalExit = namedtuple("_HistoricalExit", "generation observation")
_GENERATION_FIELDS = ("instance_id", "artifact_digest", "manifest_digest", "compatibility_epoch",
                      "schema_head", "revision", "state", "ingress_state")


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()


def same_mounts(observed, expected):
    """Docker's top-level mount inventory has no order; every record is exact."""
    return (isinstance(observed, list) and isinstance(expected, list)
            and all(isinstance(row, dict) for row in observed + expected)
            and sorted(canonical(row) for row in observed) == sorted(canonical(row) for row in expected))


def read_private(path, maximum=install.MAX_METADATA):
    mode = install.regular(path).stat()
    require(mode.st_uid == os.geteuid() and stat.S_IMODE(mode.st_mode) == 0o600,
            "Retained installation input must be private and owned by the operator")
    with path.open("rb") as stream:
        raw = stream.read(maximum + 1)
    require(len(raw) <= maximum, "Retained installation input exceeds its bound")
    return raw


def environment(raw):
    values = {}
    for line in raw.decode().splitlines():
        if not line or line.startswith("#"):
            continue
        require("=" in line, "Unsupported retained environment syntax")
        key, value = line.split("=", 1)
        require(re.fullmatch(r"[A-Z][A-Z0-9_]*", key) and key not in values,
                "Unsupported or duplicate retained environment field")
        if value.startswith(("'", '"')):
            require(len(value) >= 2 and value[-1] == value[0], "Unsupported retained environment quoting")
            value = value[1:-1]
        values[key] = value
    return values


def ring_digest(path):
    raw = install.strict_json(install.regular(path).read_bytes())
    require(isinstance(raw, dict) and 0 < len(raw) <= 32, "Expected the independently retained public-key ring")
    normalized = {}
    for key, value in raw.items():
        require(isinstance(key, str) and re.fullmatch(r"[A-Za-z0-9._-]{1,64}", key)
                and isinstance(value, str), "Unsupported public-key ring shape")
        decoded = None
        for decode in (lambda text: base64.b64decode(text, validate=True),
                       lambda text: base64.b64decode(text + "=" * (-len(text) % 4), validate=True),
                       bytes.fromhex):
            try:
                candidate = decode(value.strip())
                if len(candidate) == 32:
                    decoded = candidate
                    break
            except (ValueError, binascii.Error):
                pass
        require(decoded is not None, "Invalid retained Ed25519 public key")
        normalized[key] = base64.b64encode(decoded).decode()
    return hashlib.sha256(canonical(normalized)).hexdigest()


def exact_fields(value, names, label):
    require(isinstance(value, dict) and set(value) == set(names), label)


def observation_value(raw, pointer):
    """A bounded RFC6901 data reference; never an executable adapter/query."""
    if pointer is None:
        return raw.decode("utf-8")
    require(isinstance(pointer, str) and len(pointer) <= 1024 and (not pointer or pointer.startswith("/")),
            "Invalid historical observation reference")
    value = install.strict_json(raw)
    for token in pointer.split("/")[1:] if pointer else []:
        require(not re.search(r"~(?![01])", token), "Invalid historical observation escape")
        token = token.replace("~1", "/").replace("~0", "~")
        if isinstance(value, str):
            value = install.strict_json(value.encode())  # Retained JSON HTTP body, still only data.
        if isinstance(value, dict):
            require(token in value, "Historical observation field is missing")
            value = value[token]
        else:
            require(isinstance(value, list) and re.fullmatch(r"0|[1-9][0-9]*", token)
                    and int(token) < len(value), "Historical observation index is invalid")
            value = value[int(token)]
    return value


class HistoricalObservations:
    """Read/freeze explicit existing host facts; this class performs no effects."""
    def __init__(self, raw):
        self.document = install.strict_json(raw)
        exact_fields(self.document, ("format", "installation_id", "project", "database_system_id", "database",
            "database_oid", "source_sequence", "source_package_sha256", "records"), "Invalid historical exit document")
        require(self.document["format"] == "unirouter.historical-exits.v1"
                and isinstance(self.document["records"], list) and len(self.document["records"]) <= 31,
                "Unsupported historical exit document")
        self.sources = {}

    def read(self, reference):
        exact_fields(reference, ("path", "sha256", "pointer"), "Invalid original observation identity")
        path = Path(reference["path"])
        require(path.is_absolute() and path == path.resolve() and re.fullmatch(r"[a-f0-9]{64}", reference["sha256"]),
                "Original observation needs an exact private path and digest")
        # Reuse the existing private metadata bound, including the cold-pair
        # receipt limit. No private config/body is discovered or interpreted.
        raw = read_private(path, 16 << 20)
        require(hashlib.sha256(raw).hexdigest() == reference["sha256"], "Original historical observation changed")
        require(str(path) not in self.sources or self.sources[str(path)] == raw, "Historical source changed while reading")
        self.sources[str(path)] = raw
        return observation_value(raw, reference["pointer"])

    def before(self, references, installation):
        exact_fields(references, ("installation_id", "instance_id", "container_id", "image"),
                     "Incomplete historical source identity")
        value = {key: self.read(reference) for key, reference in references.items()}
        require(all(isinstance(item, str) for item in value.values()) and value["installation_id"] == installation and
                re.fullmatch(r"uri_[a-f0-9]{32}", value["instance_id"]) and
                re.fullmatch(r"[a-f0-9]{64}", value["container_id"]) and
                re.fullmatch(r"sha256:[a-f0-9]{64}", value["image"]), "Historical source identity is invalid")
        return value

    def command(self, record, operation, project, service):
        receipt = self.read(record["command"])
        require(isinstance(receipt, dict) and type(receipt.get("returncode")) is int and receipt["returncode"] == 0,
                "Original native control command did not succeed")
        original_reference = record["command"]
        if receipt.get("format") == "unirouter.normalized-command-observation.v1":
            exact_fields(receipt, ("format", "returncode", "argv", "original_process", "program_sha256"),
                         "Invalid normalized native command observation")
            original_reference = receipt["original_process"]
            original = self.read(original_reference)
            require(isinstance(original, dict) and type(original.get("returncode")) is int
                    and original["returncode"] == receipt["returncode"]
                    and ("argv" not in original or original["argv"] == receipt["argv"]),
                    "Normalized command differs from its actual native receipt")
            require(isinstance(record["program"], dict) and
                    receipt["program_sha256"] == record["program"]["sha256"],
                    "Normalization needs its independently reviewed original program identity")
            # Legacy normalization belongs to operator preparation and review.
            # The signed entry checks explicit data and original hashes; it
            # never executes or parses a historical program.
            self.read(record["program"])
        argv = receipt.get("argv")
        require(isinstance(argv, list) and all(isinstance(part, str) for part in argv) and
                argv and Path(argv[0]).name == "docker" and "compose" in argv and
                "--project-name" in argv and argv.index("--project-name") + 1 < len(argv)
                and argv[argv.index("--project-name") + 1] == project and operation in argv and
                argv[-1] == service and "--no-recreate" not in argv and "--no-start" not in argv,
                "Native command needs its exact original service arguments")
        return original_reference

    def validate(self, installation, metadata, source, resource, process_start, retired=False):
        wanted = {"installation_id": installation["installation_id"], "project": installation["project"],
            "database_system_id": metadata["system_id"], "database": metadata["database"], "database_oid": metadata["database_oid"],
            "source_sequence": source["release_sequence"], "source_package_sha256": source["package_sha256"]}
        require(canonical({key: self.document[key] for key in wanted}) == canonical(wanted),
                "Historical exits belong to another installation, database or source package")
        rows = {row["instance_id"]: row for row in metadata["generations"]}
        result = {}
        for record in self.document["records"]:
            common = ("kind", "generation", "service", "before", "command", "program")
            kind = record.get("kind") if isinstance(record, dict) else None
            extras = ("after", "processes") if kind == "process_exit" else ("successor", "output")
            require(kind in ("process_exit", "native_replacement"), "Unknown historical termination kind")
            exact_fields(record, (*common, *extras), "Unexpected historical termination fields")
            generation = record["generation"]
            exact_fields(generation, _GENERATION_FIELDS, "Incomplete original generation binding")
            instance = generation["instance_id"]
            require(instance not in result and instance in rows and type(generation["revision"]) is int
                    and generation["revision"] > 0 and generation["schema_head"] == SOURCE_SCHEMA
                    and generation["ingress_state"] == "absent", "Historical generation is duplicate or unsupported")
            expected = dict(generation)
            if retired:
                expected.update(state="stopped", revision=generation["revision"] + 1)
            actual = {key: rows[instance].get(key) for key in _GENERATION_FIELDS}
            require(canonical(actual) == canonical(expected), "Original historical generation changed")
            expiry = datetime.datetime.fromisoformat(rows[instance]["lease_expires_at"].replace("Z", "+00:00"))
            require(expiry <= datetime.datetime.now(datetime.timezone.utc), "Historical generation lease is still live")
            before = self.before(record["before"], installation["installation_id"])
            require(before["instance_id"] == instance and before["image"] == generation["artifact_digest"]
                    and record["service"] in ("backend", "backend-canary"), "Historical resource/generation binding differs")
            current = resource(before["container_id"])
            if current is not None:
                require(current["Image"] == before["image"] and current["Project"] == installation["project"]
                        and current["Service"] == record["service"], "Historical resource ownership differs")
            if kind == "process_exit":
                self.command(record, "stop", installation["project"], record["service"])
                after = self.read(record["after"])
                require(isinstance(after, dict) and after.get("Id") == before["container_id"] and after.get("Image") == before["image"],
                        "Original stopped resource identity differs")
                state = after.get("State", {})
                require(state.get("Running") is False and state.get("Pid") == 0 and state.get("ExitCode") == 0
                        and state.get("OOMKilled") is False, "Original historical clean stop is unconfirmed")
                processes = self.read(record["processes"])
                if isinstance(processes, str):
                    lines = processes.splitlines()
                    require(lines and lines[0].strip() == "PID" and all(re.fullmatch(r"[1-9][0-9]*", row.strip()) for row in lines[1:]),
                            "Invalid original native PID inventory")
                    processes = {row.strip(): None for row in lines[1:]}
                require(isinstance(processes, dict) and processes, "Original process inventory is empty")
                for pid, start in processes.items():
                    require(re.fullmatch(r"[1-9][0-9]*", pid) and (start is None or isinstance(start, str) and start.isdecimal()),
                            "Original process identity is invalid")
                    now = process_start(int(pid))
                    require(now is None if start is None else now != start, "An original historical process may still be alive")
            else:
                native_receipt = self.command(record, "up", installation["project"], record["service"])
                successor = self.before(record["successor"], installation["installation_id"])
                require(successor["instance_id"] != instance and successor["container_id"] != before["container_id"]
                        and successor["image"] == before["image"], "Native replacement successor is not distinct or matching")
                output = self.read(record["output"])
                command_path, output_path = Path(native_receipt["path"]), Path(record["output"]["path"])
                require(command_path.name.endswith(".process.json") and output_path in
                        (command_path.with_name(command_path.name.removesuffix(".process.json") + ".stdout"),
                         command_path.with_name(command_path.name.removesuffix(".process.json") + ".stderr")),
                        "Native replacement output belongs to another command")
                name = installation["project"] + "-" + record["service"] + "-1"
                require(isinstance(output, str) and {"Container " + name + " " + state for state in ("Recreated", "Started", "Healthy")}
                        .issubset({line.strip() for line in output.splitlines()}), "Original native replacement completion is missing")
                require(current is None or current["Running"] is False and current["Pid"] == 0,
                        "Original replaced container is still live or unknown")
                # No original PID/starttime/application exitcode was retained
                # for this kind. Termination is not relabeled a clean stop.
            result[instance] = _HistoricalExit(canonical(generation), canonical(record))
        return result


class Installation:
    """Read the existing installer identity; never regenerate its settings."""
    def __init__(self, directory, run=install.execute):
        require(directory.is_absolute() and directory == directory.resolve() and not directory.is_symlink(),
                "Use the original absolute installation directory")
        mode = directory.stat()
        require(mode.st_uid == os.geteuid() and stat.S_IMODE(mode.st_mode) == 0o700,
                "Original installation directory must remain private and operator-owned")
        self.directory, self.run = directory, run
        self.state_raw = read_private(directory / "install-state.json", 1 << 20)
        self.state = install.strict_json(self.state_raw)
        state = self.state
        require(state.get("format") == "unirouter.install-state.v1" and state.get("directory") == str(directory)
                and re.fullmatch(r"[a-z][a-z0-9_-]{0,47}", state.get("project", ""))
                and isinstance(state.get("installation_id"), str) and state.get("daemon"),
                "Retained installer identity is invalid")
        socket = Path(state["docker_socket"])
        require(socket.is_absolute() and not any(c in str(socket) for c in (",", "\n", "\r")),
                "Only the original local Docker socket is supported")
        self.runtime = directory / "runtime"
        self.docker = ["docker", "--host", "unix://" + str(socket)]
        self.compose = self.docker + ["compose", "--project-name", state["project"], "--project-directory", str(self.runtime),
                                     "--env-file", str(self.runtime / ".env"), "-f", str(self.runtime / "docker-compose.yml"),
                                     "-f", str(self.runtime / "installation.json"), "-f", str(self.runtime / "runtime-image.json")]
        self.env_raw = read_private(self.runtime / ".env")
        self.env = environment(self.env_raw)
        for key in ("POSTGRES_USER", "POSTGRES_DB"):
            require(re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]{0,62}", self.env.get(key, "")),
                    "Unsupported retained PostgreSQL identity")

    def command(self, args, label, input=None):
        return self.run(args, label=label, input=input)

    def status(self):
        witness = self.state.get("upgrade")
        require(witness is None or witness.get("format") == SCHEMA, "Unsupported upgrade witness")
        return {"format": SCHEMA, "installation_id": self.state["installation_id"],
                "state": "not_started" if witness is None else witness.get("state", "pending"),
                "pending": None if witness is None else witness.get("pending"),
                "completed_steps": [] if witness is None else witness.get("done", []),
                "source": None if witness is None else witness.get("source"),
                "candidate": None if witness is None else witness.get("candidate")}

    def inspect(self, name):
        # Docker configuration may contain secrets. Inspect in memory only.
        items = install.strict_json(self.command(self.docker + ["inspect", name], "inspect owned upgrade resource"))
        require(isinstance(items, list) and len(items) == 1, "Ambiguous owned Docker resource")
        return items[0]

    def sql(self, query, database=None):
        # Queries below are authored by this module. No shell, passwords,
        # connection strings or business row contents appear in argv/logs.
        pg = self.state["project"] + "-postgres-1"
        return self.command(self.docker + ["exec", "-i", pg, "psql", "-X", "-qAt", "--set", "ON_ERROR_STOP=1",
                                           "-U", self.env["POSTGRES_USER"], "-d", database or self.env["POSTGRES_DB"]],
                            "read exact installation metadata", query.encode()).strip()

    def native_metadata(self):
        return install.strict_json(self.sql("""SELECT jsonb_build_object(
 'database',current_database(),'database_oid',(SELECT oid::bigint FROM pg_database WHERE datname=current_database()),
 'system_id',(SELECT system_identifier::text FROM pg_control_system()),
 'ledger',(SELECT jsonb_agg(version ORDER BY version) FROM schema_migrations),
 'installation',obj_description('schema_migrations'::regclass),
 'release',(SELECT to_jsonb(s) FROM release_state s WHERE singleton=true),
 'history',(SELECT COALESCE(jsonb_agg(to_jsonb(h) ORDER BY release_sequence),'[]') FROM release_history h),
 'recorded_at',(SELECT h.recorded_at FROM release_history h JOIN release_state s USING(release_sequence) WHERE s.singleton=true),
 'generations',(SELECT COALESCE(jsonb_agg(to_jsonb(g)||jsonb_build_object('manifest_digest',m.manifest_digest) ORDER BY instance_id),'[]') FROM runtime_generations g LEFT JOIN runtime_support_manifests m USING(artifact_digest)),
 'manifests',(SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY artifact_digest),'[]') FROM runtime_support_manifests m),
 'ingress',(SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY change_id),'[]') FROM runtime_ingress_changes c));"""))

    def claim(self, source, candidate, details=None):
        require("upgrade" not in self.state, "An upgrade already owns this installation; use resume-upgrade")
        require(re.fullmatch(r"[a-f0-9]{64}", candidate.get("package_sha256", ""))
                and candidate["package_sha256"] != self.state["package_sha256"],
                "Upgrade claim must revoke the original package resume identity")
        snapshot = copy.deepcopy(self.state)
        self.state["upgrade"] = {"format": SCHEMA, "state": "preparing", "pending": None, "done": [],
                                 "source": source, "candidate": candidate,
                                 "source_state_sha256": hashlib.sha256(self.state_raw).hexdigest(),
                                 "source_state_bytes": base64.b64encode(self.state_raw).decode(),
                                 "source_state": snapshot, "receipts": {}}
        # The original M127 installer already validates this exact field before
        # any restart/password step. Changing it atomically revokes stale resume
        # even when an old installer does not know the new upgrade member.
        self.state["upgrade"].update(details or {})
        self.state["package_sha256"] = candidate["package_sha256"]
        install.save_state(self.directory, self.state)


def retained_installation(metadata):
    prefix = "unirouter.fresh-install.v1\n"
    envelope = metadata.get("installation", "")
    require(envelope.startswith(prefix), "Original completed installation envelope is required")
    original = install.strict_json(envelope[len(prefix):])
    require(original.get("state") == "completed" and metadata["ledger"][-1] == SOURCE_SCHEMA,
            "Community maintenance requires the exact completed M127 source")
    return original


def maintenance_input(metadata, source, candidate, trusted_ring_sha256, stop_proofs, historical=None):
    """Bind host-proved exits to the same native transaction for one/two writers.

    This document grants no authority. The native owner verifies the installed
    signatures, exact current database and every retirement predicate again.
    """
    original = retained_installation(metadata)
    active = [row for row in metadata["generations"] if row["state"] != "stopped"]
    historical = historical or {}
    require(1 <= len(stop_proofs) <= 2 and 1 <= len(active) <= 32
            and not set(stop_proofs).intersection(historical)
            and {row["instance_id"] for row in active} == set(stop_proofs) | set(historical),
            "Only exact current writers and independently proved historical exits may be retired")
    require(not any(row["status"] in ("pending", "ambiguous") for row in metadata["ingress"]),
            "An ingress result is unresolved")
    predecessors = {row["predecessor_id"] for row in metadata["ingress"] if row.get("predecessor_id")}
    heads = [row for row in metadata["ingress"] if row["change_id"] not in predecessors]
    require(len(heads) == 1 and heads[0]["status"] == "applied" and heads[0]["membership_after"] == [],
            "The exact complete withdrawal of all receivers is required")
    state = metadata["release"]
    require(state["package_sha256"] == source["package_sha256"]
            and state["release_sequence"] == source["release_sequence"]
            and state["target_schema"] == SOURCE_SCHEMA,
            "Installed source identity changed")
    document = {"schema_version": "unirouter.community-maintenance.v1",
                "installation_id": original["plan"]["installation_id"],
                "installation_plan_digest": original["plan"]["digest"],
                "source_package": "/upgrade/source.tar.gz", "candidate_package": "/upgrade/candidate.tar.gz",
                "expected_sequence": state["release_sequence"],
                "expected_source_package_sha256": source["package_sha256"],
                "expected_source_manifest_sha256": source["manifest_sha256"],
                "expected_candidate_package_sha256": candidate["package_sha256"],
                "expected_candidate_manifest_sha256": candidate["manifest_sha256"],
                "expected_trusted_ring_sha256": trusted_ring_sha256,
                "expected_environment": original["plan"]["target"]["environment"],
                "expected_database_system_id": metadata["system_id"],
                "expected_database": metadata["database"], "expected_database_oid": metadata["database_oid"],
                "expected_ingress_head": heads[0]["change_id"], "retirements": []}
    for row in sorted(active, key=lambda value: value["instance_id"]):
        require(row["ingress_state"] == "absent" and row["schema_head"] == SOURCE_SCHEMA,
                "Retirement requires exact absent M127 generations")
        if row["instance_id"] in historical:
            observation = historical[row["instance_id"]]
            require(isinstance(observation, _HistoricalExit) and observation.generation ==
                    canonical({key: row.get(key) for key in _GENERATION_FIELDS}), "Historical exit was not independently validated")
            proof_bytes = observation.observation
        else:
            proof = stop_proofs.get(row["instance_id"])
            require(proof and proof.get("instance_id") == row["instance_id"]
                    and proof.get("image") == row["artifact_digest"] and proof.get("clean_exit") is True
                    and proof.get("old_processes_gone") is True and proof.get("container_id"),
                    "Every exact current writer needs its own clean host exit proof")
            proof_bytes = canonical(proof)
        document["retirements"].append({"instance_id": row["instance_id"], "artifact_digest": row["artifact_digest"],
            "manifest_digest": row["manifest_digest"], "compatibility_epoch": row["compatibility_epoch"],
            "expected_schema_head": row["schema_head"], "expected_revision": row["revision"],
            "expected_ingress_head": heads[0]["change_id"],
            "expected_environment": document["expected_environment"],
            "expected_database_system_id": metadata["system_id"],
            "host_proof_sha256": "sha256:" + hashlib.sha256(proof_bytes).hexdigest()})
    return document




class Upgrade(Installation):
    """One finite host operation; database decisions remain native-owned."""
    def __init__(self, directory, release_directory, verifier, public_keys, source_package=None,
                 layout="community_single", edge_directory=None, run=install.execute, *, historical_exits=None):
        super().__init__(directory, run)
        require(layout in ("community_single", "ur_edge_pair"), "Unsupported upgrade layout")
        self.layout, self.edge_directory = layout, edge_directory
        retained_package = self.state.get("upgrade", {}).get("source_package") or self.state.get("current_release", {}).get("package")
        self.source_package = install.regular(source_package or (Path(retained_package) if retained_package else directory / "release.tar.gz"))
        for path in (self.source_package, release_directory, verifier, public_keys):
            require(path.is_absolute() and path == path.resolve() and not any(c in str(path) for c in (",", ":", "\n", "\r")),
                    "Upgrade inputs need absolute real paths safe for the existing Docker mount interface")
        self.verifier, self.public_keys = install.regular(verifier), install.regular(public_keys)
        self.release = install.Release(release_directory, release_directory / "release.tar.gz", verifier,
                                       public_keys, "amd64", run)
        for name in ("upgrade.py", "recovery.py"):
            declared = install.PREFIX + name
            require(declared in self.release.files
                    and install.digest(Path(__file__).with_name(name)) == install.digest(self.release.files[declared]),
                    "Use the upgrade and recovery programs shipped in this exact signed release")
        self.rebase = self.release.files.get("payload/scripts/source-budget-restore-rebase.sql")
        require(self.rebase is not None, "Signed release is missing the existing restore-rebase owner")
        self.roles = ("backend",)
        self.edge_compose = None
        if layout == "ur_edge_pair":
            require(edge_directory and edge_directory.is_absolute() and edge_directory == edge_directory.resolve()
                    and not edge_directory.is_symlink(), "Use the retained named UR edge directory")
            for name in ("runtime-overrides.json", "compose.edge.json", "nginx.conf"):
                require(install.regular(edge_directory / name).stat().st_uid == os.geteuid(),
                        "Named edge configuration ownership changed")
            overrides = install.strict_json((edge_directory / "runtime-overrides.json").read_bytes())
            require(set(overrides) == {"services"} and set(overrides["services"]) in
                    ({"backend"}, {"backend", "backend-canary"}), "Unknown writer layout in the retained edge adapter")
            if "backend-canary" in overrides["services"]:
                self.roles = ("backend", "backend-canary")
            self.compose += ["-f", str(edge_directory / "runtime-overrides.json")]
            edge = install.strict_json((edge_directory / "compose.edge.json").read_bytes())
            require(set(edge.get("services", {})) == {"nginx"}
                    and edge["services"]["nginx"].get("container_name") == "uniroute-nginx",
                    "Unknown external ingress layout")
            self.edge_compose = self.docker + ["compose", "--project-name", self.state["project"] + "-edge",
                "--project-directory", str(edge_directory), "--env-file", "/dev/null", "-f", str(edge_directory / "compose.edge.json")]
        override = self.state.get("upgrade", {}).get("image_override") or self.state.get("current_release", {}).get("image_override")
        if override and Path(override).exists():
            require(Path(override) == self.runtime / "upgrade-image.json", "Unknown retained upgrade image override")
            self.compose += ["-f", override]
        self.rendered = None
        self.containers = {}
        self.source = None
        self.metadata = None
        self.writer_instances = {}
        retained_history = self.state.get("upgrade", {}).get("historical_exits_path")
        self.historical_path = historical_exits or (Path(retained_history) if retained_history else None)
        self.historical_raw = None
        self.historical_sources = {}
        if self.historical_path:
            require(self.historical_path.is_absolute() and self.historical_path == self.historical_path.resolve(),
                    "Historical exits require the original absolute private input")
            self.historical_raw = read_private(self.historical_path)

    def _historical_resource(self, identifier):
        listed = self.command(self.docker + ["ps", "-a", "--no-trunc", "--filter", "id=" + identifier,
            "--format", "{{.ID}}"], "read exact historical resource presence").splitlines()
        require(listed in ([], [identifier]), "Historical resource lookup is ambiguous")
        if not listed:
            return None  # Successful native exact-ID listing, not a failed inspect.
        fmt = '{"Id":{{json .Id}},"Image":{{json .Image}},"Running":{{json .State.Running}},"Pid":{{json .State.Pid}},"Project":{{json (index .Config.Labels "com.docker.compose.project")}},"Service":{{json (index .Config.Labels "com.docker.compose.service")}}}'
        row = install.strict_json(self.command(self.docker + ["inspect", "--format", fmt, identifier],
            "read owned historical resource state"))
        require(row.get("Id") == identifier, "Historical resource changed while inspecting")
        return row

    def _historical(self, metadata, source):
        if self.historical_path is None:
            return {}
        require(read_private(self.historical_path) == self.historical_raw, "Historical exit input changed")
        witness = self.state.get("upgrade", {})
        if witness:
            require(witness.get("historical_exits_path") == str(self.historical_path)
                    and witness.get("historical_exits_sha256") == hashlib.sha256(self.historical_raw).hexdigest(),
                    "Resume requires the original frozen historical exit input")
        # A native all-or-none retirement may have committed before its host
        # reply. Match every originally named tuple, never a historical subset.
        rows = {row["instance_id"]: row for row in metadata["generations"]}
        retired_inputs = witness.get("retirement_input", {}).get("retirements", [])
        retired = bool(retired_inputs) and all(
            (row := rows.get(item["instance_id"])) and row["state"] == "stopped" and row["ingress_state"] == "absent"
            and row["artifact_digest"] == item["artifact_digest"] and row["manifest_digest"] == item["manifest_digest"]
            and row["revision"] == item["expected_revision"] + 1 and row["schema_head"] == SOURCE_SCHEMA
            for item in retired_inputs)
        observations = HistoricalObservations(self.historical_raw)
        result = observations.validate(self.state, metadata, source, self._historical_resource, self._process_start, retired)
        self.historical_sources = observations.sources
        return result

    def _retain_historical(self):
        if self.historical_path is None:
            return
        self._write_owned(self.evidence / "historical-exits.json", self.historical_raw)
        for raw in self.historical_sources.values():
            self._write_owned(self.evidence / ("historical-source-" + hashlib.sha256(raw).hexdigest() + ".data"), raw)

    def _verify_historical_copies(self):
        if self.historical_path is None or self.witness["state"] == "preparing":
            return
        require(read_private(self.evidence / "historical-exits.json") == self.historical_raw,
                "Retained historical input changed or is missing")
        for raw in self.historical_sources.values():
            path = self.evidence / ("historical-source-" + hashlib.sha256(raw).hexdigest() + ".data")
            require(read_private(path, 16 << 20) == raw, "Retained original historical observation changed or is missing")

    def execute(self, resume=False):
        """Use the installer's one host lock and a flat, finite maintenance path."""
        fd = os.open(self.directory / ".install.lock", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "a+b") as lock:
            require(stat.S_ISREG(os.fstat(lock.fileno()).st_mode), "Installation lock must be regular")
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            require(read_private(self.directory / "install-state.json", 1 << 20) == self.state_raw,
                    "Installation state changed before the host lock")
            if resume:
                self._resume_context()
            else:
                self._begin()
            if self.witness["state"] == "preparing":
                self._finish_preparing()
            if self.witness["state"] == "completed":
                self._verify_completed()
                return self.status()
            self._withdraw()
            for role in self.roles:
                self._step("stop-" + role, lambda role=role: self._stop(role),
                           lambda role=role: self._stopped(role) if role in self.witness.get("stop_intents", {}) else _NOT_STARTED)
            if "retire-writers" not in self.witness["done"]:
                self._retirement_ready()
            self._step("retire-writers", self._retire, self._retirement_result)
            self._step("cold-pair", self._backup, self._backup_result)
            self._step("migrate", self._migrate, self._migration_result)
            self._step("configure-images", self._configure_images, self._configured_images)
            self._step("start-writers", self._start_writers, self._new_writers)
            self._step("record-release", self._record_release, self._recorded_release)
            self._step("prepare-admission", self._prepare_admission, self._admission_prepared)
            self._step("start-receivers", self._start_receivers, self._receivers_started)
            self._step("admit-receivers", self._admit, self._admitted)
            self._verify_completed()
            self.witness["state"] = "completed"
            self.state["current_release"] = dict(self.witness["candidate"], package=str(self.retained_candidate),
                                                  image_override=self.witness["image_override"])
            self.state["image_ids"].update(backend=self.witness["candidate_image"]["id"],
                                             frontend=self.witness["frontend_image"]["id"])
            self._save()
            return self.status()

    def _selected_image(self, reference, label):
        items = install.strict_json(self.command(self.docker + ["image", "inspect", reference], label))
        require(len(items) == 1 and items[0].get("Os") == "linux" and items[0].get("Architecture") == "amd64"
                and reference in (items[0].get("RepoDigests") or [])
                and re.fullmatch(r"sha256:[a-f0-9]{64}", items[0].get("Id", "")),
                "Selected immutable image bytes/platform differ")
        return {"reference": reference, "id": items[0]["Id"]}

    def _begin(self):
        report = self.check()
        ingress = self._ingress_config()
        configuration = [self.runtime / name for name in (".env", "docker-compose.yml", "installation.json", "runtime-image.json")]
        if self.edge_directory:
            configuration += [self.edge_directory / name for name in ("runtime-overrides.json", "compose.edge.json", "nginx.conf")]
        config_sha256 = {str(path): install.digest(install.regular(path)) for path in configuration}
        rendering_sha256 = hashlib.sha256(canonical(self.rendered)).hexdigest()
        head = self._head(self.metadata)
        preparation_head = None if head is None else {key: head.get(key) for key in ("change_id", "status", "desired_config_digest", "membership_after")}
        preparation_fleet = {row["instance_id"]: {key: row[key] for key in ("artifact_digest", "schema_head", "state", "ingress_state")}
                             for row in self.metadata["generations"] if row["state"] != "stopped"}
        # Cache acquisition is explicit and happens while the source still
        # serves. check/status never pull or write an operation witness.
        selected = {}
        for role in ("backend", "frontend"):
            reference = self.release.images[role]
            self.command(self.docker + ["pull", reference], "acquire candidate " + role + " image before withdrawal")
            selected[role] = self._selected_image(reference, "verify acquired candidate image")
        details = dict(operation_id="ucu_" + uuid.uuid4().hex, layout=self.layout,
            edge_directory=str(self.edge_directory) if self.edge_directory else None,
            source_package=str(self.source_package), source_image={"reference": self.source["images"]["backend"], "id": self.containers["backend"]["Image"]},
            candidate_image=selected["backend"], frontend_image=selected["frontend"],
            candidate_manifest_sha256=self._manifest_digest(),
            writer_instances=self.writer_instances, ingress_config=base64.b64encode(ingress).decode(),
            configuration_sha256=config_sha256, rendering_sha256=rendering_sha256,
            preparation_head=preparation_head, preparation_fleet=preparation_fleet,
            environment_sha256=hashlib.sha256(self.env_raw).hexdigest(),
            source_ledger=self.metadata["ledger"], database_system_id=self.metadata["system_id"],
            database=self.metadata["database"], database_oid=self.metadata["database_oid"],
            source_containers={role: {key: value for key, value in row.items() if key in ("Id", "Image", "Mounts")}
                               for role, row in self.containers.items()},
            image_override=str(self.runtime / "upgrade-image.json"),
            content_settings={key: self.rendered["services"]["backend"].get("environment", {}).get(key, "")
                              for key in ("SOURCE_CONTENT_STORAGE_ROOT", "SOURCE_CONTENT_STORAGE_IDENTITY", "SOURCE_CONTENT_STORAGE_TOKEN_FILE")})
        if self.historical_path is not None:
            details.update(historical_exits_path=str(self.historical_path),
                historical_exits_sha256=hashlib.sha256(self.historical_raw).hexdigest())
        self.claim(self.source, report["candidate"], details)
        witness = self.witness
        self._resume_context()
        self._checkpoint("claimed")

    def _finish_preparing(self):
        witness = self.witness
        # Preparation has no service effects. Exact existing files are read
        # back so an interruption here can finish before withdrawal starts.
        parent = self.evidence.parent
        parent.mkdir(mode=0o700, exist_ok=True)
        require(parent == parent.resolve() and stat.S_IMODE(parent.stat().st_mode) == 0o700, "Unsafe upgrade evidence parent")
        self.evidence.mkdir(mode=0o700, exist_ok=True)
        require(self.evidence == self.evidence.resolve() and stat.S_IMODE(self.evidence.stat().st_mode) == 0o700, "Unsafe upgrade evidence directory")
        self._write_owned(self.evidence / "source-install-state.json", base64.b64decode(witness["source_state_bytes"]))
        self._retain_historical()
        self._retain_candidate()
        support_raw = self._carrier(True, ["runtime-generation", "support"])
        support = install.strict_json(support_raw)
        require(SOURCE_SCHEMA in support.get("schema_heads", []) and TARGET_SCHEMA in support.get("schema_heads", []),
                "Candidate artifact does not declare this finite schema successor")
        witness["candidate_support_sha256"] = hashlib.sha256(support_raw.encode()).hexdigest()
        self._assert_configuration()
        expected_containers = copy.deepcopy(witness["source_containers"])
        self.check(allow_preparing=True)
        require(hashlib.sha256(canonical(self.rendered)).hexdigest() == witness["rendering_sha256"]
                and self._ingress_config() == base64.b64decode(witness["ingress_config"]),
                "Effective source or ingress configuration changed during candidate acquisition")
        for role, expected in expected_containers.items():
            current = self.containers[role]
            require(all(current.get(key, []) == expected.get(key, []) for key in ("Id", "Image"))
                    and same_mounts(current.get("Mounts", []), expected.get("Mounts", [])),
                    "Exact source process/resource changed during candidate acquisition")
        head = self._head(self.metadata)
        actual_head = None if head is None else {key: head.get(key) for key in ("change_id", "status", "desired_config_digest", "membership_after")}
        actual_fleet = {row["instance_id"]: {key: row[key] for key in ("artifact_digest", "schema_head", "state", "ingress_state")}
                        for row in self.metadata["generations"] if row["state"] != "stopped"}
        require(actual_head == witness["preparation_head"] and actual_fleet == witness["preparation_fleet"],
                "Exact native ingress predecessor or writer fleet changed during candidate acquisition")
        witness["state"] = "maintenance"
        self._save()

    def _manifest_digest(self):
        with tarfile.open(self.release.package, "r:gz") as archive:
            return hashlib.sha256(archive.extractfile(archive.next()).read()).hexdigest()

    def _resume_context(self):
        witness = self.witness
        require(witness.get("layout") == self.layout
                and witness.get("edge_directory") == (str(self.edge_directory) if self.edge_directory else None)
                and witness.get("source_package") == str(self.source_package)
                and install.digest(self.source_package) == witness["source"]["package_sha256"]
                and hashlib.sha256(self.env_raw).hexdigest() == witness["environment_sha256"],
                "Retained source package/layout/settings changed; preserve the pending installation")
        require(re.fullmatch(r"ucu_[a-f0-9]{32}", witness["operation_id"])
                and witness["candidate_manifest_sha256"] == self._manifest_digest(), "Candidate manifest or private evidence identity changed")
        raw_state = base64.b64decode(witness["source_state_bytes"], validate=True)
        require(hashlib.sha256(raw_state).hexdigest() == witness["source_state_sha256"]
                and install.strict_json(raw_state) == witness["source_state"], "Retained original installation state changed")
        # A completed readback can still run the native carrier through this
        # Compose definition. Freeze execution configuration for that readback;
        # legitimate new business rows are outside this file identity check.
        self._assert_configuration()
        if Path(witness["image_override"]).exists():
            require(read_private(Path(witness["image_override"])) == canonical(self._image_override()), "Upgrade image override changed during recovery")
        self.evidence = self.directory / "upgrades" / witness["operation_id"]
        self.retained_candidate = self.directory / "releases" / (self.release.sha + ".tar.gz")
        self.containers = witness["source_containers"]
        self.writer_instances = witness["writer_instances"]
        info = install.strict_json(self.command(self.docker + ["info", "--format", "{{json .}}"], "check original Docker daemon"))
        require(info.get("ID") == self.state["daemon"] and info.get("OSType") == "linux"
                and not any("rootless" in value for value in info.get("SecurityOptions", [])), "The original Docker daemon changed")
        actual = self.native_metadata()
        require(actual["system_id"] == witness["database_system_id"] and actual["database"] == witness["database"]
                and actual["database_oid"] == witness["database_oid"], "The original database identity changed")
        require(bool(witness.get("historical_exits_path")) == (self.historical_path is not None),
                "Historical exit presence differs from the original upgrade")
        self._historical(actual, witness["source"])
        if witness["state"] != "preparing":
            require(self.evidence.is_dir() and not self.evidence.is_symlink() and self.evidence.stat().st_uid == os.geteuid()
                    and stat.S_IMODE(self.evidence.stat().st_mode) == 0o700, "Private upgrade evidence directory changed")
            require(install.digest(self.retained_candidate) == self.release.sha, "Retained full candidate package changed")
        self._verify_historical_copies()

    def _assert_configuration(self):
        require(all(install.digest(install.regular(Path(path))) == expected
                    for path, expected in self.witness["configuration_sha256"].items()),
                "Retained source configuration changed before withdrawal")

    def _retain_candidate(self):
        self.retained_candidate.parent.mkdir(mode=0o700, exist_ok=True)
        require(self.retained_candidate.parent == self.retained_candidate.parent.resolve(), "Unsafe retained release directory")
        if not self.retained_candidate.exists():
            fd = os.open(self.retained_candidate, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            with os.fdopen(fd, "wb") as output, self.release.package.open("rb") as source:
                shutil.copyfileobj(source, output, 1 << 20)
                output.flush(); os.fsync(output.fileno())
        require(install.digest(install.regular(self.retained_candidate)) == self.release.sha, "Retained full package copy is incomplete or changed")

    def _save(self):
        install.save_state(self.directory, self.state)

    def _checkpoint(self, name):
        hook = getattr(self, "test_checkpoint", None)
        if hook is not None:
            hook(name)

    @property
    def witness(self):
        value = self.state.get("upgrade")
        require(value and value.get("format") == SCHEMA
                and value["candidate"]["package_sha256"] == self.release.sha,
                "Retained upgrade differs from the verified candidate")
        return value

    def _step(self, name, action, reconcile):
        witness = self.witness
        if name in witness["done"]:
            return witness["receipts"][name]
        require(witness["pending"] in (None, name), "Another upgrade step has an unresolved result")
        if witness["pending"] == name:
            result = reconcile()
            require(result is not None, "Upgrade result is unconfirmed at " + name + "; inspect upgrade-status before another mutation")
        else:
            witness["pending"] = name
            self._save()
            self._checkpoint("started:" + name)
            result = _NOT_STARTED
        # Only the step's own readback can establish that its effect was never
        # started. An unknown result is not permission to repeat a mutation.
        if result is _NOT_STARTED:
            result = action()
            self._checkpoint("effect:" + name)
        require(isinstance(result, dict), "Upgrade step has no verified receipt")
        witness["receipts"][name] = result
        witness["done"].append(name)
        witness["pending"] = None
        self._save()
        self._checkpoint("done:" + name)
        return result

    def _write_owned(self, path, raw):
        if path.exists() or path.is_symlink():
            require(read_private(path, max(install.MAX_METADATA, len(raw))) == raw,
                    "Retained upgrade input changed; preserve it")
        else:
            install.private_file(path, raw)

    def _carrier(self, candidate, argv, document=None, database=None):
        image = self.witness["candidate_image"] if candidate else self.witness["source_image"]
        overlay = {"services": {"backend": {"image": image["reference"],
                   "environment": {"RUNTIME_IMAGE_DIGEST": image["id"]}, "read_only": True}}}
        if database:
            from urllib.parse import urlsplit
            current = urlsplit(self.env["DB_DSN"])
            require(re.fullmatch(r"ur_upgrade_verify_[a-f0-9]{24}", database), "Unknown verification database")
            overlay["services"]["backend"]["environment"]["DB_DSN"] = current._replace(path="/" + database).geturl()
        path = self.evidence / (("candidate" if candidate else "source") + ("-restore" if database else "") + "-carrier.json")
        self._write_owned(path, canonical(overlay))
        args = self.compose + ["-f", str(path), "run", "--rm", "--no-deps", "--pull", "never", "-T",
            "--volume", str(self.source_package) + ":/upgrade/source.tar.gz:ro",
            "--volume", str(self.release.package) + ":/upgrade/candidate.tar.gz:ro",
            "--volume", str(self.evidence) + ":/upgrade/evidence:ro", "backend", "./uniroute", *argv]
        return self.command(args, "native upgrade owner: " + " ".join(argv[:2]),
                            None if document is None else canonical(document))

    def _ingress_config(self):
        if self.edge_directory:
            raw = install.regular(self.edge_directory / "nginx.conf").read_bytes()
            encoded = self.command(self.docker + ["exec", "uniroute-nginx", "base64", "/etc/nginx/nginx.conf"],
                                   "read exact installed ingress configuration")
            actual = base64.b64decode("".join(encoded.split()), validate=True)
        else:
            front = self.inspect(self.state["project"] + "-frontend-1")
            encoded = self.command(self.docker + ["exec", front["Id"], "base64", "/etc/nginx/conf.d/default.conf"],
                                   "read exact installed ingress configuration")
            raw = base64.b64decode("".join(encoded.split()), validate=True)
            actual = raw
        require(raw and raw == actual, "Actual ingress configuration differs from its retained source")
        return raw

    @staticmethod
    def _process_start(pid):
        try:
            return Path("/proc/" + str(pid) + "/stat").read_text().rsplit(")", 1)[1].split()[19]
        except FileNotFoundError:
            return None

    def _process_proof(self, container):
        rows = self.command(self.docker + ["top", container, "-eo", "pid"], "read original process identities").splitlines()[1:]
        result = {}
        for row in rows:
            pid = int(row.strip())
            start = self._process_start(pid)
            require(start is not None, "A source process changed before stop")
            result[str(pid)] = start
        require(result, "Original running process inventory is empty")
        return result

    def _stopped(self, role):
        intent = self.witness.get("stop_intents", {}).get(role)
        if not intent:
            return None
        after = self.inspect(intent["container_id"])
        state = after["State"]
        require(after["Id"] == intent["container_id"] and after["Image"] == intent["image"]
                and same_mounts(after.get("Mounts", []), intent["mounts"]), "Original stopped resource identity changed")
        if state["Running"]:
            return None
        require(state.get("Pid") == 0 and state.get("ExitCode") == 0 and not state.get("OOMKilled"),
                "Original process did not exit cleanly")
        require(all(self._process_start(int(pid)) != start for pid, start in intent["old_processes"].items()),
                "An old proxy or writer process still exists")
        return {key: value for key, value in intent.items() if key != "mounts"} | {"clean_exit": True, "old_processes_gone": True}

    def _stop(self, role):
        container = self.containers[role]
        observed = self.inspect(container["Id"])
        require(observed["Id"] == container["Id"] and observed["Image"] == container["Image"]
                and observed["State"]["Running"] and same_mounts(observed.get("Mounts", []), container.get("Mounts", [])),
                "Exact source resource changed before graceful stop")
        intent = {"container_id": container["Id"], "image": container["Image"],
                  "mounts": container.get("Mounts", []), "old_processes": self._process_proof(container["Id"])}
        if role in self.roles:
            intent["instance_id"] = self.witness["writer_instances"][role]
        self.witness.setdefault("stop_intents", {})[role] = intent
        self._save()
        compose, service = (self.edge_compose, "nginx") if role == "edge" else (self.compose, role)
        # Compose retains the installation's own stop_grace_period. No forced
        # timeout reduction or repeated signal is used to make a stop pass.
        self.command(compose + ["stop", service], "gracefully stop exact source " + role)
        result = self._stopped(role)
        require(result is not None, "Original process exit remains unconfirmed")
        return result

    def _head(self, metadata):
        predecessors = {row["predecessor_id"] for row in metadata["ingress"] if row.get("predecessor_id")}
        heads = [row for row in metadata["ingress"] if row["change_id"] not in predecessors]
        require(len(heads) <= 1 and (not heads or heads[0]["status"] == "applied"), "Ingress predecessor is unresolved")
        return heads[0] if heads else None

    def _prepare_withdrawal(self):
        self._assert_configuration()
        require(self._ingress_config() == base64.b64decode(self.witness["ingress_config"]), "Source ingress changed before withdrawal")
        metadata = self.native_metadata()
        head = self._head(metadata)
        raw = base64.b64decode(self.witness["ingress_config"])
        digest = hashlib.sha256(raw).hexdigest()
        require(not head or head["desired_config_digest"] == digest, "Installed ingress predecessor differs")
        before = sorted(({"instance": row["instance_id"], "artifact_digest": row["artifact_digest"]}
                         for row in metadata["generations"] if row["ingress_state"] != "absent"), key=lambda row: row["instance"])
        plan = {"change_id": self.witness["operation_id"] + "-withdraw", "predecessor_id": head["change_id"] if head else "",
                "expected_config_digest": digest, "desired_config_digest": digest, "desired_config_bytes": base64.b64encode(raw).decode(),
                "membership_before": before, "membership_after": []}
        existing = self.witness.get("withdrawal")
        require(existing is None or existing == plan, "Retained withdrawal input changed")
        self.witness["withdrawal"] = plan
        self._save()
        self._carrier(False, ["runtime-generation", "prepare"], plan)
        result = self._withdrawal_prepared()
        require(result is not None and result is not _NOT_STARTED, "Native withdrawal preparation is unconfirmed")
        return result

    def _withdrawal_prepared(self):
        plan = self.witness.get("withdrawal")
        if not plan:
            return _NOT_STARTED
        metadata = self.native_metadata()
        rows = [row for row in metadata["ingress"] if row["change_id"] == plan["change_id"]]
        if not rows:
            head = self._head(metadata)
            require((head["change_id"] if head else "") == plan["predecessor_id"],
                    "Ingress predecessor changed before withdrawal")
            return _NOT_STARTED
        row = rows[0]
        require(row["status"] in ("pending", "applied") and row["membership_after"] == []
                and row["desired_config_digest"] == plan["desired_config_digest"], "Native withdrawal input differs")
        return {"change_id": plan["change_id"], "configuration_sha256": plan["desired_config_digest"]}

    def _complete_withdrawal(self):
        plan = self.witness["withdrawal"]
        exits = [self.witness["receipts"]["stop-" + role]
                 for role in (("edge", "frontend") if self.edge_compose else ("frontend",))]
        self._carrier(False, ["runtime-generation", "complete"], {"change_id": plan["change_id"],
            "observed_digest": plan["desired_config_digest"], "old_workers_gone": True,
            "observation": {"old_receiver_exit_proofs": [hashlib.sha256(canonical(value)).hexdigest() for value in exits]}})
        result = self._withdrawn()
        require(result is not None, "Native withdrawal completion is unconfirmed")
        return result

    def _withdraw(self):
        # Every phase has one pending owner. Physical stop is never nested in
        # another pending step; prepare and complete remain native boundaries.
        self._step("prepare-withdrawal", self._prepare_withdrawal, self._withdrawal_prepared)
        for role in (("edge", "frontend") if self.edge_compose else ("frontend",)):
            self._step("stop-" + role, lambda role=role: self._stop(role),
                       lambda role=role: self._stopped(role) if role in self.witness.get("stop_intents", {}) else _NOT_STARTED)
        def completion_result():
            result = self._withdrawn()
            if result is not None:
                return result
            prepared = self._withdrawal_prepared()
            return _NOT_STARTED if isinstance(prepared, dict) else None
        return self._step("complete-withdrawal", self._complete_withdrawal, completion_result)

    def _withdrawn(self):
        plan = self.witness.get("withdrawal")
        if not plan:
            return None
        metadata = self.native_metadata()
        rows = [row for row in metadata["ingress"] if row["change_id"] == plan["change_id"]]
        if not rows or rows[0]["status"] != "applied":
            return None
        require(rows[0]["membership_after"] == [] and rows[0]["desired_config_digest"] == plan["desired_config_digest"]
                and all(row["ingress_state"] == "absent" for row in metadata["generations"]),
                "Full receiver withdrawal is not proved")
        return {"change_id": plan["change_id"], "configuration_sha256": plan["desired_config_digest"]}

    def _retired(self):
        document = self.witness.get("retirement_input")
        if not document:
            return None
        metadata = self.native_metadata()
        rows = {row["instance_id"]: row for row in metadata["generations"]}
        for expected in document["retirements"]:
            row = rows.get(expected["instance_id"])
            if not row or row["state"] != "stopped":
                return None
            require(row["artifact_digest"] == expected["artifact_digest"] and row["ingress_state"] == "absent"
                    and row["revision"] == expected["expected_revision"] + 1,
                    "Retired writer differs from its exact native predecessor")
        require(not any(row["state"] != "stopped" for row in rows.values()), "An unaccounted writer appeared")
        return {"instances": sorted(item["instance_id"] for item in document["retirements"]),
                "input_sha256": hashlib.sha256(canonical(document)).hexdigest()}

    def _retirement_ready(self):
        # Waiting for an expired lease has no external effect and must not
        # claim a pending retirement operation.
        metadata = self.native_metadata()
        now = datetime.datetime.now(datetime.timezone.utc)
        for row in metadata["generations"]:
            if row["state"] != "stopped":
                expires = datetime.datetime.fromisoformat(row["lease_expires_at"].replace("Z", "+00:00"))
                require(expires <= now, "A stopped writer's original native lease is still live; wait and read back before retiring")

    def _retire(self):
        metadata = self.native_metadata()
        proofs = {self.witness["writer_instances"][role]: self.witness["receipts"]["stop-" + role] for role in self.roles}
        candidate = dict(self.witness["candidate"], manifest_sha256=self.witness["candidate_manifest_sha256"])
        document = maintenance_input(metadata, self.witness["source"], candidate,
                                     ring_digest(self.public_keys), proofs, self._historical(metadata, self.witness["source"]))
        require(self.witness.get("retirement_input") in (None, document), "Retained retirement predecessor changed")
        self.witness["retirement_input"] = document
        self._save()
        self._carrier(True, ["upgrade", "maintenance-check"], document)
        self._carrier(True, ["upgrade", "maintenance-retire"], document)
        result = self._retired()
        require(result is not None, "Native retirement commit is unconfirmed")
        return result

    def _retirement_result(self):
        result = self._retired()
        if result is not None:
            return result
        document = self.witness.get("retirement_input")
        if document is None:
            return _NOT_STARTED
        rows = {row["instance_id"]: row for row in self.native_metadata()["generations"]}
        # The native transaction is all-or-none. Exact original revisions prove
        # no retirement committed; any mixed or changed result remains unknown.
        if all((row := rows.get(item["instance_id"])) and row["state"] != "stopped"
               and row["revision"] == item["expected_revision"]
               and row["artifact_digest"] == item["artifact_digest"] and row["ingress_state"] == "absent"
               for item in document["retirements"]):
            return _NOT_STARTED
        return None

    def _files(self, args, label, input_path=None, output_path=None):
        """Native binary file transport; never decode a dump or log child output."""
        custom = getattr(self, "test_file_command", None)
        if custom is not None:
            return custom(args, label, input_path, output_path)
        incoming = install.regular(input_path).open("rb") if input_path else None
        outgoing = None
        try:
            if output_path:
                fd = os.open(output_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
                outgoing = os.fdopen(fd, "wb")
            result = subprocess.run([str(arg) for arg in args], stdin=incoming, stdout=outgoing or subprocess.DEVNULL,
                                    stderr=subprocess.PIPE, env=install.clean_environment(), check=False)
            require(result.returncode == 0, label + " failed; preserve its private partial output and read back")
            if outgoing:
                outgoing.flush(); os.fsync(outgoing.fileno())
        finally:
            if incoming: incoming.close()
            if outgoing: outgoing.close()

    def _recovery_roots(self):
        roots = [self.runtime, self.directory / "admin-password"]
        if self.edge_directory:
            roots.append(self.edge_directory)
        mounts = [mount for role, row in self.containers.items() if role in (*self.roles, "frontend", "edge") for mount in row.get("Mounts", [])]
        for mount in mounts:
            require(mount["Type"] in ("bind", "volume"), "Unsupported backend recovery mount")
            path = Path(mount["Source"])
            require(path.is_absolute() and path == path.resolve() and len(path.parts) >= 3
                    and not self.evidence.is_relative_to(path), "Unsafe or overly broad recovery dependency")
            roots.append(path)
        # Mounted content or TLS dependencies can live under another archived
        # directory. Archive each exact tree once, retaining its metadata.
        return sorted((path for path in set(roots) if not any(path != other and path.is_relative_to(other) for other in roots)), key=str)

    def _host_path(self, destination):
        target = Path(destination)
        mounts = [row for row in self.containers["backend"].get("Mounts", [])
                  if target == Path(row["Destination"]) or target.is_relative_to(Path(row["Destination"]))]
        require(len(mounts) == 1, "Content recovery needs its exact original host mount")
        mount = mounts[0]
        return Path(mount["Source"]) / target.relative_to(mount["Destination"])

    def _content(self):
        objects = install.strict_json(self.sql("SELECT COALESCE(jsonb_agg(jsonb_build_object('path',relative_path,'bytes',saved_bytes,'digest',digest,'identity',storage_identity,'binding_sha',encode(sha256(convert_to(storage_binding_token,'UTF8')),'hex')) ORDER BY relative_path),'[]'::jsonb) FROM execution_content_objects WHERE state='ready'"))
        settings = self.witness["content_settings"]
        root = settings["SOURCE_CONTENT_STORAGE_ROOT"]
        require(root or not objects, "Stored content has no retained binding root")
        if not root:
            return {"objects": [], "inventory": None}
        host = self._host_path(root)
        return {"objects": objects, "root": str(host),
                "inventory": recovery.content_inventory(host, objects, settings["SOURCE_CONTENT_STORAGE_IDENTITY"])}

    def _backup(self):
        recovery.quiet(lambda db, query: self.sql(query, db), self.env["POSTGRES_DB"])
        metadata = self.native_metadata()
        require(metadata["ledger"] == self.witness["source_ledger"] and self._retired(), "Source changed before cold pair")
        backup = self.evidence / "backup"
        require(not backup.exists(), "A partial cold pair exists; preserve it and inspect upgrade-status")
        backup.mkdir(mode=0o700)
        source_db = self.env["POSTGRES_DB"]
        sql = lambda db, query: self.sql(query, db)
        tables = recovery.table_fingerprints(sql, source_db)
        before = recovery.boundaries(sql, source_db)
        require(before["coverage"], "Original financial coverage is absent")
        epoch = max(row["coverage_epoch"] for row in before["coverage"])
        normalized = recovery.table_fingerprints(sql, source_db, True, epoch)
        content = self._content()
        roots = self._recovery_roots()
        dump = backup / "database.dump"
        pg = self.state["project"] + "-postgres-1"
        self._files(self.docker + ["exec", pg, "pg_dump", "-Fc", "--lock-wait-timeout=5s", "-U", self.env["POSTGRES_USER"], "-d", source_db],
                    "write original cold database dump", output_path=dump)
        self._files(self.docker + ["exec", "-i", pg, "pg_restore", "--list"], "read native cold dump inventory", input_path=dump)
        inventory = write_paired_files(roots, backup, self.run)
        restored = restore_and_verify_paired_files(backup, self.run)
        clone = "ur_upgrade_verify_" + uuid.uuid4().hex[:24]
        require(self.sql("SELECT count(*) FROM pg_database WHERE datname='" + clone + "';", "postgres") == "0", "Independent restore database already exists")
        self.witness["verification_database"] = clone
        self._save()
        self.sql('CREATE DATABASE "' + clone + '" OWNER "' + self.env["POSTGRES_USER"] + '";', "postgres")
        self._files(self.docker + ["exec", "-i", pg, "pg_restore", "--exit-on-error", "--single-transaction", "-U", self.env["POSTGRES_USER"], "-d", clone],
                    "restore cold dump into independent database", input_path=dump)
        require(recovery.table_fingerprints(sql, clone) == tables and recovery.boundaries(sql, clone) == before,
                "Independent database restore differs from the cold source")
        # This is the original SQL restore owner shipped in the signed package,
        # not a Python recreation of financial/content epoch mutation.
        self.sql(self.rebase.read_text(), clone)
        after = recovery.boundaries(sql, clone)
        require(recovery.table_fingerprints(sql, clone, True, epoch) == normalized,
                "Restore rebase changed unrelated original data")
        require(len(after["streams"]) == len(before["streams"]), "Restored content stream count differs")
        for old, new in zip(before["streams"], after["streams"]):
            require({k: v for k, v in old.items() if k != "recovery_epoch"} == {k: v for k, v in new.items() if k != "recovery_epoch"}
                    and isinstance(new["recovery_epoch"], int) and new["recovery_epoch"] > 0
                    and old["recovery_epoch"] != new["recovery_epoch"], "Restored content epoch did not rebase")
        old_active = [row for row in before["coverage"] if row["ended_at"] is None]
        new_active = [row for row in after["coverage"] if row["ended_at"] is None]
        require(len(old_active) == len(new_active) == 1 and len(after["coverage"]) == len(before["coverage"]) + 1,
                "Restored financial coverage lineage count differs")
        old, new = old_active[0], new_active[0]
        require(new["state"] == "certified" and new["reason"] == "restore_rebase" and new["predecessor_epoch"] == old["coverage_epoch"]
                and new["restore_boundary"] and next(row for row in after["coverage"] if row["coverage_epoch"] == old["coverage_epoch"])["ended_at"] == new["restore_boundary"],
                "Restored financial coverage successor differs")
        origin = retained_installation(metadata)["plan"]
        self._write_owned(self.evidence / "source-install-plan.json", canonical(origin))
        checked = self._carrier(False, ["fresh-install", "check", "--plan", "/upgrade/evidence/source-install-plan.json"], database=clone)
        require("state=completed" in checked, "Source artifact did not validate the independent restored origin")
        if content["inventory"] is not None:
            restored_root = Path(restored["verification_directory"]) / content["root"].lstrip("/")
            settings = self.witness["content_settings"]
            require(recovery.content_inventory(restored_root, content["objects"], settings["SOURCE_CONTENT_STORAGE_IDENTITY"]) == content["inventory"],
                    "Restored content bytes/binding differ")
            token = Path(restored["verification_directory"]) / str(self._host_path(settings["SOURCE_CONTENT_STORAGE_TOKEN_FILE"])).lstrip("/")
            result = install.strict_json(self.command(self.docker + ["run", "--rm", "--network", "none", "--read-only", "--pull", "never",
                "--volume", str(restored_root) + ":/restore/content:ro", "--volume", str(token) + ":/restore/binding.token:ro",
                self.witness["source_image"]["reference"], "./uniroute", "content-store", "check", "--root", "/restore/content",
                "--identity", settings["SOURCE_CONTENT_STORAGE_IDENTITY"], "--token-file", "/restore/binding.token"], "native restored content binding check"))
            require(result.get("status") == "ok", "Restored native content binding is unconfirmed")
        require(recovery.table_fingerprints(sql, source_db) == tables and self._content() == content
                and paired_file_inventory(roots)["files"] == inventory["files"], "Cold source changed during independent restore")
        recovery.quiet(sql, source_db)
        receipt = {"format": "unirouter.community-cold-pair.v1", "tables": tables, "boundaries": before,
                   "content": content, "roots": [str(path) for path in roots], "files": inventory,
                   "dump_sha256": install.digest(dump), "rebase_sha256": install.digest(self.rebase),
                   "verification_database": clone, "restored_tables": recovery.table_fingerprints(sql, clone),
                   "restored_boundaries": after, "source_metadata": metadata}
        self._write_owned(backup / "receipt.json", canonical(receipt))
        return {"receipt_sha256": install.digest(backup / "receipt.json"), "dump_sha256": receipt["dump_sha256"], "verification_database": clone}

    def _backup_result(self):
        backup = self.evidence / "backup"
        if not backup.exists():
            return _NOT_STARTED
        receipt_path = backup / "receipt.json"
        if not receipt_path.exists():
            return None
        receipt = install.strict_json(read_private(receipt_path, 16 << 20))
        require(install.digest(backup / "database.dump") == receipt["dump_sha256"] and install.digest(self.rebase) == receipt["rebase_sha256"], "Retained cold dump/restore owner changed")
        restore_and_verify_paired_files(backup, self.run)
        sql = lambda db, query: self.sql(query, db)
        require(recovery.table_fingerprints(sql, self.env["POSTGRES_DB"]) == receipt["tables"] and self._content() == receipt["content"]
                and paired_file_inventory([Path(path) for path in receipt["roots"]])["files"] == receipt["files"]["files"]
                and recovery.table_fingerprints(sql, receipt["verification_database"]) == receipt["restored_tables"]
                and recovery.boundaries(sql, receipt["verification_database"]) == receipt["restored_boundaries"],
                "Source or independent restore changed after the cold pair")
        recovery.quiet(sql, self.env["POSTGRES_DB"])
        return {"receipt_sha256": install.digest(receipt_path), "dump_sha256": receipt["dump_sha256"], "verification_database": receipt["verification_database"]}

    def _pair(self):
        path = self.evidence / "backup/receipt.json"
        require(install.digest(install.regular(path)) == self.witness["receipts"]["cold-pair"]["receipt_sha256"],
                "Cold recovery receipt changed")
        return install.strict_json(read_private(path, 16 << 20))

    @staticmethod
    def _in(values):
        values = list(values)
        require(values, "Expected a nonempty retained identity set")
        return "(" + ",".join("'" + str(value).replace("'", "''") + "'" for value in values) + ")"

    def _preserved(self, recorded=False):
        pair = self._pair()
        metadata = self.native_metadata()
        require(metadata["ledger"] == self.witness["source_ledger"] + [TARGET_SCHEMA], "Exact M128 successor ledger is required")
        original = pair["source_metadata"]
        new_instances = {row["instance_id"] for row in self.witness["receipts"].get("start-writers", {}).get("writers", {}).values()}
        require({row["instance_id"] for row in metadata["generations"]} == {row["instance_id"] for row in original["generations"]} | new_instances,
                "Unknown runtime generation appeared after the cold pair")
        allowed_manifests = {row["artifact_digest"] for row in original["manifests"]} | {self.witness["candidate_image"]["id"]}
        require({row["artifact_digest"] for row in metadata["manifests"]}.issubset(allowed_manifests), "Unknown runtime support manifest appeared")
        history = {row["release_sequence"] for row in original["history"]}
        if recorded: history.add(self.witness["candidate"]["release_sequence"])
        require({row["release_sequence"] for row in metadata["history"]} == history, "Unknown release history appeared")
        ingress = {row["change_id"] for row in original["ingress"]}
        if self.witness.get("admission"): ingress.add(self.witness["admission"]["change_id"])
        require({row["change_id"] for row in metadata["ingress"]}.issubset(ingress), "Unknown ingress transition appeared")
        projections = {
            "model_deployments": ("to_jsonb(t)-'upstream_release_observation'-'upstream_observation_revision'", ""),
            "schema_migrations": ("to_jsonb(t)", " WHERE version<>'" + TARGET_SCHEMA + "'"),
            "runtime_generations": ("to_jsonb(t)", " WHERE instance_id IN " + self._in(row["instance_id"] for row in original["generations"])),
            "runtime_support_manifests": ("to_jsonb(t)", " WHERE artifact_digest IN " + self._in(row["artifact_digest"] for row in original["manifests"])),
            "runtime_ingress_changes": ("to_jsonb(t)", " WHERE change_id IN " + self._in(row["change_id"] for row in original["ingress"])),
            "release_history": ("to_jsonb(t)", " WHERE release_sequence<=" + str(self.witness["source"]["release_sequence"]))}
        actual = recovery.table_fingerprints(lambda db, query: self.sql(query, db), self.env["POSTGRES_DB"], projections=projections)
        expected = dict(pair["tables"])
        if recorded:
            require(isinstance(self._recorded_release(), dict), "Exact target release record is required")
            actual.pop("release_state"); expected.pop("release_state")
        require(actual == expected and self._content() == pair["content"], "Protected original database/content facts changed")
        require(self.sql("SELECT count(*) FROM model_deployments WHERE upstream_release_observation IS NOT NULL OR upstream_observation_revision<>0;") == "0",
                "Unexpected post-migration observation writes precede admission")
        return metadata

    def _migration_result(self):
        metadata = self.native_metadata()
        if metadata["ledger"] == self.witness["source_ledger"]:
            return _NOT_STARTED
        require(metadata["ledger"] == self.witness["source_ledger"] + [TARGET_SCHEMA], "Migration has an unexpected ledger outcome")
        self._carrier(True, ["runtime-generation", "inspect"])
        self._preserved()
        return {"schema": TARGET_SCHEMA, "ledger_sha256": hashlib.sha256(canonical(metadata["ledger"])).hexdigest()}

    def _migrate(self):
        # Reprove the completed cold pair immediately before native mutation.
        require(isinstance(self._backup_result(), dict), "Verified cold recovery pair is required")
        self._carrier(True, ["migrate", "up"])
        result = self._migration_result()
        require(isinstance(result, dict), "Native migration result is unconfirmed")
        return result

    def _image_override(self):
        return {"services": {**{role: {"image": self.release.images["backend"], "environment": {"RUNTIME_IMAGE_DIGEST": self.witness["candidate_image"]["id"]}}
                                  for role in self.roles}, "frontend": {"image": self.release.images["frontend"]}}}

    def _configured_images(self):
        path = Path(self.witness["image_override"])
        if not path.exists():
            return _NOT_STARTED
        require(read_private(path) == canonical(self._image_override()), "Retained candidate image override changed")
        if str(path) not in self.compose:
            self.compose += ["-f", str(path)]
        return {"file": str(path), "sha256": install.digest(path)}

    def _configure_images(self):
        self._write_owned(Path(self.witness["image_override"]), canonical(self._image_override()))
        return self._configured_images()

    def _new_writers(self):
        found = {}
        for role in self.roles:
            row = self.inspect(self.state["project"] + "-" + role + "-1")
            if row["Id"] == self.containers[role]["Id"]:
                require(not row["State"]["Running"], "An original writer was restarted")
                return _NOT_STARTED if not found else None
            require(row["Image"] == self.witness["candidate_image"]["id"]
                    and same_mounts(row.get("Mounts", []), self.containers[role].get("Mounts", [])), "New writer image or mounts differ")
            if not row["State"]["Running"] or row["State"].get("Health", {}).get("Status") != "healthy":
                return None
            labels = row.get("Config", {}).get("Labels", {})
            require(labels.get(install.LABEL) == self.state["installation_id"] and labels.get("com.docker.compose.project") == self.state["project"]
                    and labels.get("com.docker.compose.service") == role, "New writer ownership differs")
            ready = install.strict_json(self.command(self.docker + ["exec", row["Id"], "wget", "-qO-", "http://127.0.0.1:8080/health/ready"], "check candidate writer readiness"))
            runtime = ready.get("runtime", {})
            require(ready.get("status") == "ok" and runtime.get("state") == "serving"
                    and runtime.get("artifact_digest") == row["Image"] and runtime.get("lease_remaining_seconds", 0) > 0
                    and runtime.get("schema_ledger_digest") == self.witness["receipts"]["migrate"]["ledger_sha256"]
                    and runtime.get("support_manifest_digest") == self.witness["candidate_support_sha256"]
                    and re.fullmatch(r"uri_[a-f0-9]{32}", runtime.get("instance", ""))
                    and runtime["instance"] not in self.witness["writer_instances"].values(), "New writer native readiness differs")
            require(runtime["instance"] not in [value["instance_id"] for value in found.values()], "New writers reused one process identity")
            found[role] = {"container_id": row["Id"], "instance_id": runtime["instance"], "artifact_digest": row["Image"]}
        metadata = self.native_metadata()
        active = {row["instance_id"]: row for row in metadata["generations"] if row["state"] != "stopped"}
        require(set(active) == {row["instance_id"] for row in found.values()}
                and all(row["artifact_digest"] == self.witness["candidate_image"]["id"] and row["schema_head"] == TARGET_SCHEMA
                        and row["state"] == "serving" for row in active.values()), "Current writer fleet differs from the new exact processes")
        return {"writers": found}

    def _start_writers(self):
        self._preserved()
        self.command(self.compose + ["up", "-d", "--wait", "--no-deps", "--pull", "never", "--no-build", *self.roles], "start exact candidate writers")
        result = self._new_writers()
        require(isinstance(result, dict), "Candidate writer startup is unconfirmed")
        return result

    def _recorded_release(self):
        state = self.native_metadata()["release"]
        expected = self.witness["candidate"]
        source = self.witness["source"]
        if state["release_sequence"] == source["release_sequence"] and state["package_sha256"] == source["package_sha256"]:
            return _NOT_STARTED
        require(all(state.get(key) == expected[key] for key in ("release_sequence", "version", "commit", "package_sha256"))
                and state["target_schema"] == TARGET_SCHEMA and state["trusted_key_id"] == self.release.manifest["key_id"], "Native release record differs from the exact candidate")
        return {"release_sequence": state["release_sequence"], "package_sha256": state["package_sha256"]}

    def _record_release(self):
        self._preserved()
        require(isinstance(self._new_writers(), dict), "Exact candidate readiness is required before recording")
        source = self.witness["source"]
        self._carrier(True, ["upgrade", "record-applied", "--package", "/upgrade/candidate.tar.gz", "--recorded-by", "community-upgrader",
            "--expected-source-sequence", str(source["release_sequence"]), "--expected-source-package-sha256", source["package_sha256"]])
        return self._recorded_release()

    def _prepare_admission(self):
        self._preserved(recorded=True)
        require(self._withdrawn() is not None, "Retained withdrawal is required before receiver admission")
        new = self._new_writers()
        require(isinstance(new, dict), "Exact new writer readiness is required")
        raw = base64.b64decode(self.witness["ingress_config"])
        plan = {"change_id": self.witness["operation_id"] + "-admit", "predecessor_id": self.witness["withdrawal"]["change_id"],
                "expected_config_digest": self.witness["withdrawal"]["desired_config_digest"],
                "desired_config_digest": hashlib.sha256(raw).hexdigest(), "desired_config_bytes": base64.b64encode(raw).decode(),
                "membership_before": [], "membership_after": sorted(({"instance": row["instance_id"], "artifact_digest": row["artifact_digest"]}
                    for row in new["writers"].values()), key=lambda row: row["instance"])}
        require(self.witness.get("admission") in (None, plan), "Admission input changed")
        self.witness["admission"] = plan
        self._save()
        self._carrier(True, ["runtime-generation", "prepare"], plan)
        result = self._admission_prepared()
        require(isinstance(result, dict), "Native admission preparation is unconfirmed")
        return result

    def _admission_prepared(self):
        plan = self.witness.get("admission")
        if plan is None:
            return _NOT_STARTED
        rows = [row for row in self.native_metadata()["ingress"] if row["change_id"] == plan["change_id"]]
        if not rows:
            return _NOT_STARTED
        row = rows[0]
        require(row["status"] in ("pending", "applied") and row["membership_after"] == plan["membership_after"]
                and row["desired_config_digest"] == plan["desired_config_digest"], "Native admission plan differs")
        return {"change_id": plan["change_id"]}

    def _receivers_started(self):
        front = self.inspect(self.state["project"] + "-frontend-1")
        if front["Id"] == self.containers["frontend"]["Id"]:
            require(not front["State"]["Running"], "An old frontend was restarted")
            return _NOT_STARTED
        require(front["Image"] == self.witness["frontend_image"]["id"] and same_mounts(front.get("Mounts", []), self.containers["frontend"].get("Mounts", [])), "Candidate frontend image or mounts differ")
        if not front["State"]["Running"] or front["State"].get("Health", {}).get("Status") != "healthy":
            return None
        if self.edge_compose:
            edge = self.inspect("uniroute-nginx")
            require(edge["Id"] == self.containers["edge"]["Id"] and edge["Image"] == self.containers["edge"]["Image"]
                    and same_mounts(edge.get("Mounts", []), self.containers["edge"].get("Mounts", [])), "Named edge ownership changed")
            if not edge["State"]["Running"]:
                return None
        require(self._ingress_config() == base64.b64decode(self.witness["ingress_config"]), "Resumed ingress configuration differs")
        return {"frontend_container": front["Id"], "frontend_image": front["Image"], "edge": bool(self.edge_compose)}

    def _start_receivers(self):
        self._preserved(recorded=True)
        self._verify_original_configuration()
        require(isinstance(self._new_writers(), dict), "Exact new writers are required before receiver start")
        self.witness["pre_admission_preservation"] = {"cold_pair_receipt_sha256": self.witness["receipts"]["cold-pair"]["receipt_sha256"],
            "candidate_package_sha256": self.release.sha}
        self._save()
        require(isinstance(self._admission_prepared(), dict), "Prepare native membership before starting any receiver")
        self.command(self.compose + ["up", "-d", "--wait", "--no-deps", "--pull", "never", "--no-build", "frontend"], "start exact candidate frontend")
        if self.edge_compose:
            self.command(self.edge_compose + ["start", "nginx"], "start retained named edge")
        result = self._receivers_started()
        require(isinstance(result, dict), "New receiver startup is unconfirmed")
        return result

    def _admitted(self):
        plan = self.witness.get("admission")
        if plan is None:
            return _NOT_STARTED
        rows = [row for row in self.native_metadata()["ingress"] if row["change_id"] == plan["change_id"]]
        if not rows or rows[0]["status"] == "pending":
            return _NOT_STARTED
        require(rows[0]["status"] == "applied" and rows[0]["membership_after"] == plan["membership_after"], "Native admission is unresolved or differs")
        return {"change_id": plan["change_id"]}

    def _admit(self):
        require(isinstance(self._receivers_started(), dict), "New receiver readback is required")
        self._carrier(True, ["runtime-generation", "complete"], {"change_id": self.witness["admission"]["change_id"],
            "observed_digest": self.witness["admission"]["desired_config_digest"], "old_workers_gone": True,
            "observation": {"candidate_receivers_verified": True}})
        return self._admitted()

    def _verify_original_configuration(self):
        # This exact comparison belongs BEFORE opening the new receivers.
        # Normal writes can add content files after admission.
        pair = self._pair()
        current = paired_file_inventory([Path(path) for path in pair["roots"]])["files"]
        addition = str(Path(self.witness["image_override"])).lstrip("/")
        require(current.pop(addition, None) == recovery.filemeta(Path(self.witness["image_override"])) and current == pair["files"]["files"],
                "Retained original configuration changed beyond the exact candidate image override")

    def _verify_completed(self):
        # Cold-state equality was proved before physical admission. Once the
        # service is open, legitimate new business rows/observations/content do
        # not invalidate the completed upgrade or trigger migration again.
        require(self.witness.get("pre_admission_preservation") == {
            "cold_pair_receipt_sha256": self.witness["receipts"]["cold-pair"]["receipt_sha256"],
            "candidate_package_sha256": self.release.sha}, "Retained pre-admission verification identity is absent")
        metadata = self.native_metadata()
        require(metadata["ledger"] == self.witness["source_ledger"] + [TARGET_SCHEMA]
                and isinstance(self._recorded_release(), dict), "Completed upgrade lacks its exact native release/schema")
        self._carrier(True, ["runtime-generation", "inspect"])
        require(isinstance(self._new_writers(), dict) and isinstance(self._receivers_started(), dict)
                and isinstance(self._admitted(), dict), "Completed upgrade lacks exact current runtime or admission readback")
        admitted = {row["instance"] for row in self.witness["admission"]["membership_after"]}
        require(all(row["ingress_state"] == ("receiving" if row["instance_id"] in admitted else "absent") for row in metadata["generations"]),
                "Native receiving fleet differs from the admitted exact candidate")
        pair = self._pair()
        require(install.digest(self.evidence / "backup/database.dump") == pair["dump_sha256"], "Retained recovery dump changed")
        restore_and_verify_paired_files(self.evidence / "backup", self.run)

    def source_release(self, metadata):
        state = metadata["release"]
        require(state["release_sequence"] > 0 and state["target_schema"] == SOURCE_SCHEMA
                and metadata["recorded_at"], "Original native source release/history is required")
        with tarfile.open(self.source_package, "r:gz") as archive:
            first = archive.next()
            require(first and first.name == "manifest.json" and first.isfile()
                    and 0 < first.size <= install.MAX_METADATA, "Retained source manifest is invalid")
            manifest_raw = archive.extractfile(first).read()
            manifest = install.strict_json(manifest_raw)
        verified = install.fields(self.command([self.verifier, "verify", "--public-keys", self.public_keys,
            "--package", self.source_package, "--target-os", "linux", "--target-arch", "amd64",
            "--current-sequence", str(manifest["min_current_sequence"]), "--current-schema", SOURCE_SCHEMA,
            "--at", metadata["recorded_at"]], "verify retained source signature"))
        source_sha = install.digest(self.source_package)
        require(verified.get("verified") == "true" and verified.get("package_sha256") == source_sha
                and source_sha == state["package_sha256"]
                and verified.get("manifest_sha256") == hashlib.sha256(manifest_raw).hexdigest(),
                "Retained source signature does not match the installed native release")
        for field in ("version", "commit", "release_sequence", "target_schema"):
            require(manifest[field] == state[field], "Retained source manifest differs from the installed release")
        require(manifest["key_id"] == state["trusted_key_id"] == self.release.manifest["key_id"],
                "This upgrade entry does not rotate installed signing keys")
        descriptor = [row for row in manifest["artifacts"] if row["path"] == install.DESCRIPTOR]
        require(len(descriptor) == 1 and descriptor[0]["size"] <= install.MAX_METADATA,
                "Retained source has no unique bounded image descriptor")
        with tarfile.open(self.source_package, "r:gz") as archive:
            entry = archive.getmember(install.DESCRIPTOR)
            require(entry.isfile() and entry.size == descriptor[0]["size"], "Retained source descriptor is invalid")
            raw = archive.extractfile(entry).read()
        require(hashlib.sha256(raw).hexdigest() == descriptor[0]["sha256"], "Retained source image descriptor differs")
        images = install.strict_json(raw)
        require(set(images) == {"schema_version", "images"} and images["schema_version"] == "unirouter.install.v1"
                and set(images["images"]) == set(install.SERVICES), "Unsupported retained source descriptor")
        require(all(install.immutable_image_reference(value) for value in images["images"].values()),
                "Retained source images must keep their immutable digest references")
        return {"release_sequence": state["release_sequence"], "version": state["version"], "commit": state["commit"],
                "package_sha256": source_sha, "manifest_sha256": verified["manifest_sha256"], "images": images["images"]}

    def check(self, allow_preparing=False):
        require("upgrade" not in self.state or (allow_preparing and self.witness["state"] == "preparing"),
                "An existing upgrade owns this installation; inspect upgrade-status")
        info = install.strict_json(self.command(self.docker + ["info", "--format", "{{json .}}"],
                                               "check original Docker daemon"))
        require(info.get("ID") == self.state["daemon"] and info.get("OSType") == "linux"
                and not any("rootless" in item for item in info.get("SecurityOptions", [])),
                "The original local rootful Linux Docker daemon is required")
        self.metadata = self.native_metadata()
        from urllib.parse import urlsplit, unquote
        dsn = urlsplit(self.env["DB_DSN"])
        require(self.metadata["database"] == self.env["POSTGRES_DB"] and unquote(dsn.path.lstrip("/")) == self.env["POSTGRES_DB"]
                and unquote(dsn.username or "") == self.env["POSTGRES_USER"] and dsn.hostname == "postgres" and dsn.port in (None, 5432),
                "Native database and original runtime connection differ")
        require(self.metadata["ledger"] and self.metadata["ledger"][-1] == SOURCE_SCHEMA,
                "This finite upgrade starts from M127 only")
        original = retained_installation(self.metadata)
        require(original["plan"]["target"]["environment"] == self.env.get("RUNTIME_ENVIRONMENT"),
                "Current environment differs from the retained installation scope")
        self.source = self.source_release(self.metadata)
        manifest = self.release.manifest
        require(manifest["min_current_schema"] == SOURCE_SCHEMA and manifest["target_schema"] == TARGET_SCHEMA
                and manifest["min_current_sequence"] <= self.source["release_sequence"] <= manifest["max_current_sequence"]
                and manifest["release_sequence"] > self.source["release_sequence"],
                "Candidate does not admit this exact installed source")
        for name in set(install.SERVICES) - {"backend", "frontend"}:
            require(self.release.images[name] == self.source["images"][name],
                    "This finite maintenance entry preserves database and dependency images")
        # Rendered Compose contains secrets. Retain only its sanitized identity
        # checks, never the complete rendering in normal reports or receipts.
        self.rendered = install.strict_json(self.command(self.compose + ["config", "--format", "json"],
                                                         "check retained effective Compose"))
        expected_services = set(install.SERVICES) | set(self.roles)
        require(set(self.rendered.get("services", {})) == expected_services, "Unknown services in the installed writer layout")
        identities = {}
        for role in sorted(expected_services):
            rendered = self.rendered["services"][role]
            image_role = "backend" if role in self.roles else role
            require(rendered.get("image") == self.source["images"][image_role] and "build" not in rendered
                    and not rendered.get("privileged") and rendered.get("network_mode") != "host",
                    "Installed service image or isolation differs from the retained signed source")
            container = self.inspect(self.state["project"] + "-" + role + "-1")
            labels = container.get("Config", {}).get("Labels", {})
            require(labels.get(install.LABEL) == self.state["installation_id"]
                    and labels.get("com.docker.compose.project") == self.state["project"]
                    and labels.get("com.docker.compose.service") == role and container["State"]["Running"],
                    "Installed service belongs to another resource or is not running")
            inspected_image = install.strict_json(self.command(self.docker + ["image", "inspect", rendered["image"]],
                                                               "check retained signed image bytes"))
            require(len(inspected_image) == 1 and container["Image"] == inspected_image[0]["Id"]
                    and inspected_image[0].get("Os") == "linux" and inspected_image[0].get("Architecture") == "amd64",
                    "Actual service image/platform differs from the signed source")
            actual_env = install.fields("\n".join(container.get("Config", {}).get("Env", [])))
            require(all(value is not None and actual_env.get(key) == str(value)
                        for key, value in rendered.get("environment", {}).items()),
                    "Retained configuration differs from the running service; do not reset secrets or settings")
            self.containers[role] = container
            if role in self.roles:
                exposed = container.get("HostConfig", {}).get("PortBindings") or {}
                require(all(binding.get("HostIp") in ("127.0.0.1", "::1") for values in exposed.values()
                            for binding in (values or [])), "A writer has an unmanaged public ingress")
                ready = install.strict_json(self.command(self.docker + ["exec", container["Id"], "wget", "-qO-", "http://127.0.0.1:8080/health/ready"],
                                                        "check exact source writer readiness"))
                require(ready.get("status") == "ok" and ready["runtime"]["artifact_digest"] == container["Image"],
                        "Source writer readiness differs from its actual image")
                instance = ready["runtime"].get("instance", "")
                require(re.fullmatch(r"uri_[a-f0-9]{32}", instance) and instance not in identities
                        and ready["runtime"].get("state") == "serving",
                        "Every writer needs a distinct current serving identity")
                identities[instance] = container["Image"]
                self.writer_instances[role] = instance
                # The installed source artifact verifies its complete ledger;
                # checking only MAX(version) would miss a broken predecessor.
                self.command(self.docker + ["exec", container["Id"], "./uniroute", "runtime-generation", "inspect"],
                             "check source artifact native authority")
        active = {row["instance_id"]: row["artifact_digest"] for row in self.metadata["generations"] if row["state"] != "stopped"}
        historical = self._historical(self.metadata, self.source)
        require(not set(identities).intersection(historical), "A live writer cannot be labeled historical")
        require(len(identities) + len(historical) <= 32, "Complete maintenance union exceeds the native limit")
        require(active == identities | {key: install.strict_json(value.generation)["artifact_digest"] for key, value in historical.items()},
                "An unaccounted or missing writer blocks the whole upgrade")
        require(not any(row["status"] in ("pending", "ambiguous") for row in self.metadata["ingress"]),
                "Resolve the existing ingress outcome before upgrade")
        if self.edge_compose:
            edge = self.inspect("uniroute-nginx")
            labels = edge.get("Config", {}).get("Labels", {})
            require(labels.get("com.docker.compose.project") == self.state["project"] + "-edge"
                    and labels.get("com.docker.compose.service") == "nginx" and edge["State"]["Running"],
                    "The named edge receiver identity changed")
            self.containers["edge"] = edge
        database_bytes = int(self.sql("SELECT pg_database_size(current_database());"))
        require(shutil.disk_usage(self.directory).free >= max(8 << 30, 2 * database_bytes + 2 * self.release.package.stat().st_size),
                "Insufficient local space for cold backup, independent restore and candidate images")
        require(self.sql("SELECT rolcreatedb OR rolsuper FROM pg_roles WHERE rolname=current_user;") == "t",
                "The retained backup owner cannot create an independent verification database")
        return {"format": SCHEMA, "state": "checked", "installation_id": self.state["installation_id"],
                "source": {key: self.source[key] for key in ("release_sequence", "version", "commit", "package_sha256", "manifest_sha256")},
                "candidate": {"release_sequence": manifest["release_sequence"], "version": manifest["version"],
                              "commit": manifest["commit"], "package_sha256": self.release.sha},
                "writers": len(self.roles), "writes": 0, "image_pull_required_during_explicit_upgrade": True}


def paired_file_inventory(roots, prefix=Path("/")):
    # Use the previously verified metadata/link-membership algorithm. Fixed
    # target paths, content identities and schema admission do not enter it.
    return {"roots": [str(path) for path in roots], "files": recovery.private_inventory(roots, prefix)}


def require_gnu_tar(run):
    version = run(["tar", "--version"], label="check supported paired archive tool")
    require("GNU tar" in version, "Paired upgrade recovery requires the existing Linux GNU tar with ACL/xattr support")


def write_paired_files(roots, evidence, run=install.execute):
    require_gnu_tar(run)
    inventory = paired_file_inventory(roots)
    archive_path = evidence / "paired-files.tar"
    install.private_file(archive_path, b"")
    run(["tar", "--acls", "--xattrs", "--numeric-owner", "--hard-dereference", "-cpf", str(archive_path),
         "-C", "/", *[str(path).lstrip("/") for path in roots]], label="write cold paired recovery archive")
    with install.regular(archive_path).open("r+b") as stream:
        stream.flush()
        os.fsync(stream.fileno())
    recovery.verify_archive(archive_path, inventory["files"])
    require(paired_file_inventory(roots) == inventory, "Recovery files changed while creating the cold backup")
    inventory["archive_sha256"] = install.digest(archive_path)
    install.private_file(evidence / "paired-files.json", canonical(inventory))
    return inventory


def restore_and_verify_paired_files(evidence, run=install.execute):
    require_gnu_tar(run)
    inventory = install.strict_json(read_private(evidence / "paired-files.json", 8 << 20))
    archive_path = install.regular(evidence / "paired-files.tar")
    require(install.digest(archive_path) == inventory["archive_sha256"], "Paired recovery archive changed")
    recovery.verify_archive(archive_path, inventory["files"])
    destination = evidence / "restore-files"
    if not destination.exists():
        destination.mkdir(mode=0o700)
        run(["tar", "--acls", "--xattrs", "--numeric-owner", "-xpf", str(archive_path), "-C", str(destination)],
            label="restore paired recovery files into isolated directory")
    require(not destination.is_symlink() and destination == destination.resolve(), "Unsafe recovery verification directory")
    # Existing verification bytes are read back, never overwritten. The shared
    # inventory verifies content, ownership, modes and exact symlink targets.
    roots = [Path(path) for path in inventory["roots"]]
    require(recovery.private_inventory(roots, destination) == inventory["files"],
            "Existing recovery verification files differ or are incomplete; preserve them")
    return {"archive_sha256": inventory["archive_sha256"], "verified_entries": len(inventory["files"]),
            "verification_directory": str(destination)}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("check-upgrade", "upgrade", "resume-upgrade", "upgrade-status"))
    parser.add_argument("--directory", type=Path, default=Path("/opt/unirouter"))
    parser.add_argument("--release", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--verifier", type=Path)
    parser.add_argument("--public-keys", type=Path)
    parser.add_argument("--source-package", type=Path)
    parser.add_argument("--layout", choices=("community_single", "ur_edge_pair"), default="community_single")
    parser.add_argument("--edge-directory", type=Path)
    parser.add_argument("--historical-exits", type=Path, help="private original observations for explicitly known M127 historical exits")
    args = parser.parse_args(argv)
    try:
        require(platform.system() == "Linux" and platform.machine() in ("x86_64", "amd64"), "This upgrade entry supports Linux amd64")
        require(os.geteuid() == 0, "Use the original installation operator")
        installation = Installation(args.directory)
        if args.action == "upgrade-status":
            print(json.dumps(installation.status(), sort_keys=True))
            return 0
        require(args.verifier and args.public_keys, "Use the independently verified release verifier and retained public-key ring")
        operation = Upgrade(args.directory, args.release, args.verifier, args.public_keys, args.source_package,
                            args.layout, args.edge_directory, historical_exits=args.historical_exits)
        if args.action == "check-upgrade":
            print(json.dumps(operation.check(), sort_keys=True))
            return 0
        print(json.dumps(operation.execute(resume=args.action == "resume-upgrade"), sort_keys=True))
        return 0
    except (install.InstallError, recovery.RecoveryError, RuntimeError, OSError, ValueError, KeyError, TypeError, tarfile.TarError) as exc:
        print("Upgrade stopped: " + (str(exc) if isinstance(exc, (install.InstallError, recovery.RecoveryError)) else type(exc).__name__), file=sys.stderr)
        print("Retain the original installation and upgrade evidence. No automatic reset or rollback was attempted.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
