#!/usr/bin/env python3
"""Signed, bounded recovery of known setup timestamp and expired-session drift.

This entry is not an installer. Verify its complete recovery envelope with the
already trusted native verifier BEFORE extracting or executing this program.
It keeps the original signed release and its native host state machine intact.
"""
import argparse
import datetime as dt
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import socket
import stat
import subprocess
import tarfile

TOOL_VERSION = "1.15.2-recovery.1"
PLAN_SCHEMA = "unirouter.timestamp-recovery.v2"
OWNER_SCHEMA = "unirouter.timestamp-recovery-owner.v1"
M127 = "000127_xinsight_communication_policy"
M128 = "000128_upstream_release_observation"
TABLE = "xinsight_connector_installations"
SESSION_TABLE = "admin_sessions"
ARTIFACTS = {"payload/recovery/resume.py", "payload/recovery/plan.json", "payload/provenance/build-provenance.json"}
MAX_BUNDLE = 1 << 20
VERIFIER_SHA256 = "d767e64f2db31f97c886c53e14fe30b948794933aef28137db982442a2a0b02f"
RING_SHA256 = "96c91635460ddfb2a14aa1ea97d8445951730e9eb04a33cbdf4320141e61d63d"
UPGRADE_SHA256 = "bb62a391ebded4266054b7f0892a1ded9d37d32654f83455083db9d8fb6c7131"
RECOVERY_SHA256 = "41038f3e0913aa55c4e1740f35af3df52c0aea0ff520234e57408512c8b972e1"
PREFIX = ["prepare-withdrawal", "stop-edge", "stop-frontend", "complete-withdrawal", "stop-backend",
          "retire-writers", "cold-pair", "migrate", "configure-images", "start-writers"]
REMAINING = ["record-release", "prepare-admission", "start-receivers", "admit-receivers"]


class RecoveryError(RuntimeError):
    pass


def need(condition, message):
    if not condition:
        raise RecoveryError(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def read(path, maximum=MAX_BUNDLE):
    path = Path(path)
    need(path.is_absolute() and path == path.resolve(), "Use an original absolute regular path")
    info = path.lstat()
    need(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_size <= maximum
         and info.st_uid == os.geteuid() and not stat.S_IMODE(info.st_mode) & 0o022,
         "Recovery input is not a bounded original regular file")
    with path.open("rb") as f:
        raw = f.read(maximum + 1)
        after = os.fstat(f.fileno())
        need(all(getattr(after, k) == getattr(info, k) for k in ("st_dev", "st_ino", "st_size", "st_mtime_ns", "st_ctime_ns", "st_mode", "st_nlink", "st_uid", "st_gid"))
             and len(raw) <= maximum, "Recovery input changed while reading")
    return raw


def strict_json(raw):
    def pairs(items):
        result = {}
        for key, value in items:
            need(key not in result, "Duplicate recovery field")
            result[key] = value
        return result
    def invalid(_):
        raise RecoveryError("Nonfinite recovery data")
    return json.loads(raw, object_pairs_hook=pairs, parse_constant=invalid)


def closed(value, keys):
    need(isinstance(value, dict) and set(value) == set(keys.split()), "Unknown or missing recovery field")


def validate_plan(p):
    closed(p, "schema_version tool_version identity source candidate database cold_pair frozen writer allowed_row allowed_session_gc")
    need(p["schema_version"] == PLAN_SCHEMA and p["tool_version"] == TOOL_VERSION, "Unsupported recovery plan")
    closed(p["identity"], "target_instance hostname installation_id operation_id directory release_directory source_package layout edge_directory historical_exits")
    closed(p["source"], "release_sequence package_sha256")
    closed(p["candidate"], "release_sequence package_sha256 manifest_sha256 commit")
    closed(p["database"], "name system_id oid schema ledger_sha256")
    closed(p["cold_pair"], "receipt_sha256 dump_sha256 verification_database")
    closed(p["frozen"], "initial_witness_sha256 source_state_sha256 configuration_map_sha256 prefix_receipts_sha256")
    closed(p["writer"], "container_id instance_id artifact_digest started_at pid")
    closed(p["allowed_row"], "installation_id org_id project_id updated_at setup_state setup_progress_sha256 all_other_columns_sha256")
    session_gc_binding(p)
    need(p["database"]["schema"] == M128 and p["identity"]["layout"] in ("community_single", "ur_edge_pair"), "Unsupported recovery layout/schema")
    need(type(p["source"]["release_sequence"]) is int and type(p["candidate"]["release_sequence"]) is int
         and 0 < p["source"]["release_sequence"] < p["candidate"]["release_sequence"], "Invalid recovery predecessor")
    need(re.fullmatch(r"ucu_[a-f0-9]{32}", p["identity"]["operation_id"]) and
         re.fullmatch(r"xci_[A-Za-z0-9_-]{22}", p["allowed_row"]["installation_id"]), "Invalid exact recovery identity")
    need(type(p["allowed_row"]["org_id"]) is int and p["allowed_row"]["org_id"] > 0
         and type(p["allowed_row"]["project_id"]) is int and p["allowed_row"]["project_id"] > 0, "Invalid recovery row scope")
    need(p["allowed_row"]["setup_state"] in ("provisioning", "waiting_data", "ready"), "Not the known normal setup writer")
    need(type(p["writer"]["pid"]) is int and p["writer"]["pid"] > 0, "Invalid physical candidate PID")
    for obj in (p["source"], p["candidate"], p["database"], p["cold_pair"], p["frozen"], p["allowed_row"]):
        for key, value in obj.items():
            if key.endswith("sha256"):
                need(isinstance(value, str) and re.fullmatch(r"[a-f0-9]{64}", value), "Invalid bound recovery digest")
    return p


def stamp(value):
    need(isinstance(value, str) and re.fullmatch(r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,6})?(?:Z|[+-]\d\d:\d\d)", value), "Invalid bounded UTC observation")
    parsed = dt.datetime.strptime(value, "%Y-%m-%dT%H:%M:%S" + (".%f" if "." in value else "") + "%z")
    return parsed.astimezone(dt.timezone.utc)


def docker_start_floor(value):
    need(isinstance(value, str), "Invalid candidate start identity")
    match = re.fullmatch(r"(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.(\d{1,9}))?Z", value)
    need(match, "Invalid candidate start identity")
    fraction = (match[2] or "").ljust(9, "0")
    return stamp(match[1] + "." + fraction[:6] + "Z")


def docker_start_ceiling(value):
    result = docker_start_floor(value)
    fraction = value[:-1].partition(".")[2]
    return result + (dt.timedelta(microseconds=1) if int(fraction[6:] or "0") else dt.timedelta())


def session_gc_binding(plan):
    gc = plan["allowed_session_gc"]
    closed(gc, "table source_table max_removed expires_before")
    closed(gc["source_table"], "count sha256")
    need(gc["table"] == SESSION_TABLE and type(gc["source_table"]["count"]) is int and gc["source_table"]["count"] == 1
         and type(gc["max_removed"]) is int and gc["max_removed"] == 1
         and isinstance(gc["source_table"]["sha256"], str) and re.fullmatch(r"[a-f0-9]{64}", gc["source_table"]["sha256"])
         and gc["expires_before"] == docker_start_floor(plan["writer"]["started_at"]).isoformat(), "Wrong exact session cleanup binding")
    return gc


def session_probe_query(cutoff):
    # A fixed cutoff, full-row hashes and table aggregate share one SQL snapshot.
    cutoff = stamp(cutoff).isoformat()
    row = "encode(sha256(convert_to(to_jsonb(t)::text,'UTF8')),'hex')"
    return """SET TIME ZONE 'UTC'; BEGIN READ ONLY; SELECT jsonb_build_object(
'table',jsonb_build_object('count',count(*),'sha256',encode(sha256(convert_to(COALESCE(string_agg(""" + row + ",'' ORDER BY to_jsonb(t)::text),''),'UTF8')),'hex'))," + """
'rows',COALESCE(jsonb_agg(jsonb_build_object(
'key_sha256',encode(sha256(convert_to(id::text,'UTF8')),'hex'),
'row_sha256',""" + row + ", 'expired_before',expires_at<'" + cutoff + "'::timestamptz) ORDER BY id),'[]'::jsonb)) FROM admin_sessions t; COMMIT;"


def session_gc_delta(source, current, plan, original_table):
    """Allow only the bound original session's proved pre-start expiry deletion."""
    gc = session_gc_binding(plan)
    need(gc["source_table"] == original_table, "Wrong original session table")
    def rows(probe):
        closed(probe, "table rows"); closed(probe["table"], "count sha256")
        need(isinstance(probe["rows"], list) and type(probe["table"]["count"]) is int
             and 0 <= probe["table"]["count"] <= 1 and probe["table"]["count"] == len(probe["rows"]), "Unexpected session population")
        result = {}
        for row in probe["rows"]:
            closed(row, "key_sha256 row_sha256 expired_before")
            need(all(isinstance(row[k], str) and re.fullmatch(r"[a-f0-9]{64}", row[k]) for k in ("key_sha256", "row_sha256"))
                 and type(row["expired_before"]) is bool and row["key_sha256"] not in result, "Invalid private session observation")
            result[row["key_sha256"]] = row
        need(probe["table"]["sha256"] == sha("".join(r["row_sha256"] for r in result.values()).encode()), "Incoherent session observation")
        return result
    originals, retained = rows(source), rows(current)
    need(source["table"] == original_table and set(retained).issubset(originals)
         and all(row == originals[key] for key,row in retained.items()), "Added or changed original session")
    missing = sorted(set(originals) - set(retained))
    need(len(missing) <= gc["max_removed"] and all(originals[key]["expired_before"] for key in missing), "Missing session was not already expired")
    # Expected rows come only from the authenticated original observation.
    remaining = [row for key,row in originals.items() if key not in missing]
    reference = {"count":len(remaining), "sha256":sha("".join(r["row_sha256"] for r in remaining).encode())}
    return missing, reference, {"source_table":source["table"], "retained_count":len(remaining), "removed_count":len(missing),
        "expires_before":gc["expires_before"], "removed_original_set_sha256":sha(canonical([originals[k]["row_sha256"] for k in missing]))}


def session_source_projection(missing):
    need(all(isinstance(k,str) and re.fullmatch(r"[a-f0-9]{64}",k) for k in missing), "Invalid session projection identity")
    where = " WHERE encode(sha256(convert_to(id::text,'UTF8')),'hex') NOT IN (" + ",".join("'" + k + "'" for k in missing) + ")" if missing else ""
    return "to_jsonb(t)", where


def conditional_row_projection(row_id):
    need(isinstance(row_id, str) and re.fullmatch(r"xci_[A-Za-z0-9_-]{22}", row_id), "Invalid exact setup projection")
    return "CASE WHEN installation_id='" + row_id + "' THEN to_jsonb(t)-'updated_at' ELSE to_jsonb(t) END"


def setup_delta(source, current, plan):
    """Pure row oracle shared by the host owner and real-PG regression."""
    allowed = plan["allowed_row"]
    need(source["row"]["updated_at"] == allowed["updated_at"] and source["stable_sha256"] == current["stable_sha256"] == allowed["all_other_columns_sha256"]
         and source["progress_sha256"] == current["progress_sha256"] == allowed["setup_progress_sha256"], "Unexpected original setup fields changed")
    need({k:v for k,v in source["row"].items() if k != "updated_at"} == {k:v for k,v in current["row"].items() if k != "updated_at"}
         and all(current["row"][k] == allowed[k] for k in ("installation_id", "org_id", "project_id", "setup_state")), "Wrong exact setup row/scope")
    observed, origin, clock = stamp(current["row"]["updated_at"]), stamp(allowed["updated_at"]), stamp(current["clock"])
    need(origin <= observed <= clock and observed >= docker_start_ceiling(plan["writer"]["started_at"]), "Setup timestamp is outside the exact post-start interval")
    return {"original":allowed["updated_at"], "observed":current["row"]["updated_at"],
            "checked_at":current["clock"], "other_columns_sha256":current["stable_sha256"]}


def command(args):
    result = subprocess.run([str(a) for a in args], stdin=subprocess.DEVNULL, capture_output=True)
    need(result.returncode == 0, "Native public verification refused")
    return result.stdout.decode()


def public_fields(output):
    values = {}
    for line in output.splitlines():
        if "=" in line:
            key, value = line.split("=", 1)
            need(key not in values, "Duplicate native verification field")
            values[key] = value
    return values


def verify_envelope(bundle, extracted, verifier, ring, current_sequence, run=command):
    root = Path(extracted)
    info = root.lstat()
    need(root.is_absolute() and root == root.resolve() and stat.S_ISDIR(info.st_mode)
         and info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o700, "Use a new private recovery directory")
    bundle_raw = read(bundle)
    need(sha(read(verifier, 8 << 20)) == VERIFIER_SHA256 and sha(read(ring, 4096)) == RING_SHA256,
         "Use the existing trusted native verifier/ring")
    output = run([verifier, "verify", "--package", bundle, "--public-keys", ring, "--target-os", "linux",
                  "--target-arch", "amd64", "--current-sequence", str(current_sequence), "--current-schema", M128,
                  "--max-payload-bytes", str(MAX_BUNDLE)])
    fields = public_fields(output)
    need(fields.get("verified") == "true" and fields.get("package_sha256") == sha(bundle_raw), "Unverified recovery envelope")
    with tarfile.open(bundle, "r:gz") as archive:
        first = archive.next()
        need(first and first.name == "manifest.json" and first.isfile() and 0 < first.size <= 128 << 10, "Missing verified recovery manifest")
        manifest_raw = archive.extractfile(first).read()
    need(sha(manifest_raw) == fields.get("manifest_sha256"), "Verified recovery manifest differs")
    m = strict_json(manifest_raw)
    need(m["version"] == TOOL_VERSION and m["min_current_schema"] == m["target_schema"] == M128, "Wrong recovery tool identity")
    need({a["path"] for a in m["artifacts"]} == ARTIFACTS and len(m["artifacts"]) == 3, "Wrong recovery payload inventory")
    files = {}
    for a in m["artifacts"]:
        path = Path(extracted) / a["path"]
        raw = read(path)
        need(len(raw) == a["size"] and sha(raw) == a["sha256"], "Extracted recovery bytes differ")
        files[a["path"]] = raw
    need(sha(read(Path(__file__).resolve())) == sha(files["payload/recovery/resume.py"]), "Use the signed recovery entry itself")
    p = validate_plan(strict_json(files["payload/recovery/plan.json"]))
    need(m["min_current_sequence"] == p["source"]["release_sequence"] and m["max_current_sequence"] == p["candidate"]["release_sequence"]
         and m["release_sequence"] > m["max_current_sequence"], "Wrong recovery resume window")
    provenance = strict_json(files["payload/provenance/build-provenance.json"])
    closed(provenance, "schema_version source_commit plan_sha256 program_sha256")
    need(provenance["schema_version"] == "unirouter.recovery-source.v1" and provenance["source_commit"] == m["commit"]
         and provenance["plan_sha256"] == sha(files["payload/recovery/plan.json"])
         and provenance["program_sha256"] == sha(files["payload/recovery/resume.py"])
         and m["provenance_sha256"] == sha(files["payload/provenance/build-provenance.json"]), "Recovery provenance differs")
    return p, {"schema_version": OWNER_SCHEMA, "package_sha256": sha(bundle_raw), "plan_sha256": provenance["plan_sha256"],
               "source_commit": m["commit"], "tool_version": TOOL_VERSION, "release_sequence": m["release_sequence"]}


def load_original(p, verifier, ring, run=command):
    root = Path(p["identity"]["release_directory"])
    package = root / "release.tar.gz"
    for directory in (root, root / "payload", root / "payload/deploy"):
        info = directory.lstat()
        need(directory.is_absolute() and directory == directory.resolve() and stat.S_ISDIR(info.st_mode)
             and info.st_uid == os.geteuid() and not stat.S_IMODE(info.st_mode) & 0o022, "Original import directory is writable or replaced")
    info = package.lstat()
    need(package == package.resolve() and stat.S_ISREG(info.st_mode) and info.st_nlink == 1
         and info.st_uid == os.geteuid() and not stat.S_IMODE(info.st_mode) & 0o022 and 0 < info.st_size <= 512 << 20,
         "Original signed package is not the retained regular file")
    # Authenticate the complete original archive BEFORE importing upgrade.py:
    # its top-level code immediately imports install.py and recovery.py.
    result = public_fields(run([verifier, "verify", "--public-keys", ring, "--package", package,
        "--target-os", "linux", "--target-arch", "amd64", "--current-sequence", str(p["source"]["release_sequence"]),
        "--current-schema", M128, "--max-payload-bytes", str(512 << 20)]))
    need(result.get("verified") == "true" and result.get("package_sha256") == p["candidate"]["package_sha256"]
         and result.get("manifest_sha256") == p["candidate"]["manifest_sha256"], "Original complete signed package refused")
    with tarfile.open(package, "r:gz") as archive:
        member = archive.next()
        need(member and member.name == "manifest.json" and member.isfile() and 0 < member.size <= 128 << 10, "Missing original verified manifest")
        raw = archive.extractfile(member).read()
    need(sha(raw) == p["candidate"]["manifest_sha256"], "Original verified manifest differs")
    manifest = strict_json(raw)
    declared = {a["path"]:a for a in manifest["artifacts"]}
    for name in ("install.py", "recovery.py", "upgrade.py"):
        relative = "payload/deploy/" + name
        need(relative in declared, "Original import dependency is undeclared")
        raw = read(root / relative)
        need(len(raw) == declared[relative]["size"] and sha(raw) == declared[relative]["sha256"], "Original import dependency changed")
    path = root / "payload/deploy/upgrade.py"
    need(sha(read(path)) == UPGRADE_SHA256 and sha(read(path.with_name("recovery.py"))) == RECOVERY_SHA256,
         "Original signed host programs differ")
    # The original constructor verifies the complete separately signed seq17
    # payload, including install.py and these original self identities.
    spec = importlib.util.spec_from_file_location("original_signed_upgrade", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def make_owner_class(old):
    class RecoveryUpgrade(old.Upgrade):
        def set_recovery(self, plan, identity, *, read_only):
            self.repair_plan, self.repair_identity, self.read_only = validate_plan(plan), identity, read_only
            self.last_observation = None
            self.audit_enabled = False
            return self

        def _resume_context(self):
            old.Upgrade._resume_context(self)
            p, w = self.repair_plan, self.witness
            identity = p["identity"]
            need(self.state["installation_id"] == identity["installation_id"] and w["operation_id"] == identity["operation_id"], "Wrong retained recovery operation")
            need(str(self.directory) == identity["directory"] and str(self.release.directory) == identity["release_directory"]
                 and str(self.source_package) == identity["source_package"] and self.layout == identity["layout"]
                 and (str(self.edge_directory) if self.edge_directory else None) == identity["edge_directory"]
                 and (str(self.historical_path) if self.historical_path else None) == identity["historical_exits"], "Recovery paths/layout differ")
            need(all(w["source"][k] == v for k, v in p["source"].items()) and
                 all(w["candidate"][k] == v for k, v in p["candidate"].items() if k != "manifest_sha256")
                 and w["candidate_manifest_sha256"] == p["candidate"]["manifest_sha256"], "Wrong source/candidate recovery binding")
            need(w["source_state_sha256"] == p["frozen"]["source_state_sha256"] and
                 sha(canonical(w["configuration_sha256"])) == p["frozen"]["configuration_map_sha256"], "Wrong frozen recovery configuration")
            done = w["done"]
            prefix = PREFIX if self.edge_compose else [s for s in PREFIX if s != "stop-edge"]
            need(done[:len(prefix)] == prefix and done[len(prefix):] == REMAINING[:len(done)-len(prefix)]
                 and len(done) <= len(prefix) + len(REMAINING), "Unsupported recovery completed prefix")
            need(sha(canonical({s:w["receipts"][s] for s in prefix})) == p["frozen"]["prefix_receipts_sha256"],
                 "Original completed native receipts changed")
            need(w["state"] in ("maintenance", "completed") and
                 (w["state"] != "completed" or done == prefix + REMAINING), "Unsupported recovery witness state")
            next_step = REMAINING[len(done)-len(prefix)] if len(done) < len(prefix)+len(REMAINING) else None
            need(w["pending"] in (None, next_step), "Unknown pending recovery effect")
            existing = w.get("recovery_owner")
            if existing is None:
                need(w["state"] == "maintenance" and w["pending"] == "record-release" and done == prefix and
                     sha(self.state_raw) == p["frozen"]["initial_witness_sha256"], "First recovery requires its exact original checkpoint")
            else:
                need(existing == self.repair_identity, "Another recovery owner already binds this operation")
            metadata = self.native_metadata()
            db = p["database"]
            need(metadata["system_id"] == db["system_id"] and metadata["database"] == db["name"] and metadata["database_oid"] == db["oid"]
                 and metadata["ledger"] == w["source_ledger"] + [M128] and sha(canonical(metadata["ledger"])) == db["ledger_sha256"], "Wrong native database/schema binding")
            release = metadata["release"]["release_sequence"]
            need(release in (p["source"]["release_sequence"], p["candidate"]["release_sequence"])
                 and (existing is not None or release == p["source"]["release_sequence"]), "Unexpected native release before recovery")
            need(not hasattr(self, "expected_current_sequence") or release == self.expected_current_sequence, "Native sequence changed since verification")
            pair = self._pair()
            need(old.install.digest(self.evidence / "backup/receipt.json") == p["cold_pair"]["receipt_sha256"]
                 and pair["dump_sha256"] == p["cold_pair"]["dump_sha256"] and old.install.digest(self.evidence / "backup/database.dump") == pair["dump_sha256"]
                 and pair["verification_database"] == p["cold_pair"]["verification_database"], "Wrong original recovery pair")
            need((self.evidence / "backup/restore-files").is_dir(), "Retain original isolated restored files")
            old.restore_and_verify_paired_files(self.evidence / "backup", self.run)
            sql = lambda db, query: self.sql(query, db)
            need(old.recovery.table_fingerprints(sql, pair["verification_database"]) == pair["restored_tables"]
                 and old.recovery.boundaries(sql, pair["verification_database"]) == pair["restored_boundaries"], "Original isolated restore changed")
            need(self._new_writers() == w["receipts"]["start-writers"], "Candidate writer receipt changed")
            writer = self.inspect(p["writer"]["container_id"])
            expected_writer = {k: p["writer"][k] for k in ("container_id", "instance_id", "artifact_digest")}
            need(set(w["receipts"]["start-writers"]["writers"]) == {"backend"} and w["receipts"]["start-writers"]["writers"]["backend"] == expected_writer
                 and writer["State"]["StartedAt"] == p["writer"]["started_at"] and writer["State"]["Pid"] == p["writer"]["pid"], "Candidate physical start changed")
            if not self.read_only and existing is None:
                # Validate the complete new predicate before the first new
                # informational witness write. A failed plan stays untouched.
                self._preserved()
                w["recovery_owner"] = dict(self.repair_identity)
                self._save()
            self.audit_enabled = not self.read_only

        def _preserved(self, recorded=False):
            p, w = self.repair_plan, self.witness
            pair, metadata = self._pair(), self.native_metadata()
            need(metadata["ledger"] == w["source_ledger"] + [M128], "Exact M128 successor ledger is required")
            original = pair["source_metadata"]
            new_instances = {r["instance_id"] for r in w["receipts"]["start-writers"]["writers"].values()}
            need({r["instance_id"] for r in metadata["generations"]} == {r["instance_id"] for r in original["generations"]} | new_instances, "Unknown runtime generation appeared")
            manifests = {r["artifact_digest"] for r in original["manifests"]} | {w["candidate_image"]["id"]}
            need({r["artifact_digest"] for r in metadata["manifests"]}.issubset(manifests), "Unknown support manifest appeared")
            history = {r["release_sequence"] for r in original["history"]}
            if recorded:
                history.add(w["candidate"]["release_sequence"])
            need({r["release_sequence"] for r in metadata["history"]} == history, "Unknown release history appeared")
            ingress = {r["change_id"] for r in original["ingress"]}
            if w.get("admission"):
                ingress.add(w["admission"]["change_id"])
            need({r["change_id"] for r in metadata["ingress"]}.issubset(ingress), "Unknown ingress transition appeared")
            row_id = p["allowed_row"]["installation_id"]
            query = """SET TIME ZONE 'UTC'; BEGIN READ ONLY; SELECT jsonb_build_object('row',to_jsonb(t),
 'stable_sha256',encode(sha256(convert_to((to_jsonb(t)-'updated_at')::text,'UTF8')),'hex'),
 'progress_sha256',encode(sha256(convert_to(setup_progress::text,'UTF8')),'hex'),'clock',clock_timestamp())
 FROM xinsight_connector_installations t WHERE installation_id='""" + row_id + "'; COMMIT;"
            source = old.install.strict_json(self.sql(query, pair["verification_database"]))
            current = old.install.strict_json(self.sql(query))
            delta = setup_delta(source, current, p)
            projection = conditional_row_projection(row_id)
            projections = {
                "model_deployments": ("to_jsonb(t)-'upstream_release_observation'-'upstream_observation_revision'", ""),
                "schema_migrations": ("to_jsonb(t)", " WHERE version<>'" + M128 + "'"),
                "runtime_generations": ("to_jsonb(t)", " WHERE instance_id IN " + self._in(r["instance_id"] for r in original["generations"])),
                "runtime_support_manifests": ("to_jsonb(t)", " WHERE artifact_digest IN " + self._in(r["artifact_digest"] for r in original["manifests"])),
                "runtime_ingress_changes": ("to_jsonb(t)", " WHERE change_id IN " + self._in(r["change_id"] for r in original["ingress"])),
                "release_history": ("to_jsonb(t)", " WHERE release_sequence<=" + str(w["source"]["release_sequence"]))}
            sql = lambda db, text: self.sql(text, db)
            need(old.recovery.table_fingerprints(sql, pair["verification_database"]) == pair["restored_tables"]
                 and old.recovery.boundaries(sql, pair["verification_database"]) == pair["restored_boundaries"], "Original isolated restore changed")
            expected = dict(pair["tables"])
            cutoff = p["allowed_session_gc"]["expires_before"]
            source_sessions = strict_json(self.sql(session_probe_query(cutoff), pair["verification_database"]))
            live_sessions = strict_json(self.sql(session_probe_query(cutoff)))
            missing, session_reference, session_proof = session_gc_delta(source_sessions, live_sessions, p, expected[SESSION_TABLE])
            # The restored table was validated against its retained full proof
            # before applying this single explicitly signed conditional projection.
            restored = old.recovery.table_fingerprints(sql, pair["verification_database"],
                projections={TABLE:(projection, ""), SESSION_TABLE:session_source_projection(missing)})
            need(restored[SESSION_TABLE] == session_reference, "Restored session observation changed during preservation")
            expected[TABLE] = restored[TABLE]
            expected[SESSION_TABLE] = restored[SESSION_TABLE]
            projections[TABLE] = (projection, "")
            actual = old.recovery.table_fingerprints(sql, self.env["POSTGRES_DB"], projections=projections)
            need(actual[SESSION_TABLE] == live_sessions["table"], "Live session observation changed during preservation")
            if recorded:
                need(isinstance(self._recorded_release(), dict), "Exact native candidate record is required")
                actual.pop("release_state"); expected.pop("release_state")
            need(actual == expected and self._content() == pair["content"], "Other protected original database/content facts changed")
            need(self.sql("SELECT count(*) FROM model_deployments WHERE upstream_release_observation IS NOT NULL OR upstream_observation_revision<>0;") == "0", "Unexpected observation writes precede admission")
            proof = {"owner_package_sha256":self.repair_identity["package_sha256"], "plan_sha256":self.repair_identity["plan_sha256"],
                     "cold_pair_sha256":p["cold_pair"]["receipt_sha256"], "table":TABLE, "installation_id":row_id,
                     "field":"updated_at", **delta, "session_gc":session_proof,
                     "original_whole_table_sha256":pair["tables"][TABLE]["sha256"],
                     "original_row_sha256":sha(canonical(source["row"])), "observed_row_sha256":sha(canonical(current["row"])),
                     "projected_tables_sha256":sha(canonical(actual)), "all_other_facts_equal":True}
            self.last_observation = proof
            if not self.read_only and self.audit_enabled:
                # Keep the first authorization observation for each native step;
                # every later attempt still computes and emits a fresh proof.
                observations = w.setdefault("recovery_observations", {})
                phase = w["pending"]
                need(phase in ("record-release", "prepare-admission", "start-receivers"), "Unknown preservation phase")
                if phase not in observations:
                    observations[phase] = proof
                    self._save()
                print(json.dumps({"recovery_preservation":proof}, sort_keys=True), flush=True)
            return metadata
    return RecoveryUpgrade


def inspect_completed(owner, old):
    w, metadata = owner.witness, owner.native_metadata()
    need(w.get("pre_admission_preservation") == {"cold_pair_receipt_sha256":w["receipts"]["cold-pair"]["receipt_sha256"], "candidate_package_sha256":owner.release.sha}, "Missing pre-admission preservation identity")
    need(metadata["ledger"] == w["source_ledger"] + [M128] and isinstance(owner._recorded_release(), dict)
         and isinstance(owner._new_writers(), dict) and isinstance(owner._receivers_started(), dict) and isinstance(owner._admitted(), dict), "Incomplete native recovery readback")
    admitted = {r["instance"] for r in w["admission"]["membership_after"]}
    need(all(r["ingress_state"] == ("receiving" if r["instance_id"] in admitted else "absent") for r in metadata["generations"]), "Unexpected native receiving fleet")
    # Completed operations allow legitimate later business/observation/content
    # writes. Original backup/restore bytes were checked in resume context.
    return owner.status()


def check_owner(owner, old, current_sequence):
    path = owner.directory / ".install.lock"
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, "rb") as lock:
        fcntl.flock(lock, fcntl.LOCK_SH | fcntl.LOCK_NB)
        owner._resume_context()
        native = owner.native_metadata()["release"]["release_sequence"]
        need(native == current_sequence, "Native sequence changed since verification")
        if owner.witness["state"] == "completed" or isinstance(owner._admitted(), dict):
            result = inspect_completed(owner, old)
        else:
            owner._preserved(recorded=native == owner.repair_plan["candidate"]["release_sequence"])
            result = owner.status()
        need(read(owner.directory / "install-state.json") == owner.state_raw, "Host witness changed during check")
        return {"checked":True, "writes":0, "status":result, "observation":owner.last_observation}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("check", "resume"))
    parser.add_argument("--bundle", required=True, type=Path)
    parser.add_argument("--extracted", required=True, type=Path)
    parser.add_argument("--verifier", required=True, type=Path)
    parser.add_argument("--public-keys", required=True, type=Path)
    parser.add_argument("--current-sequence", required=True, type=int)
    a = parser.parse_args(argv)
    try:
        p, identity = verify_envelope(a.bundle, a.extracted, a.verifier, a.public_keys, a.current_sequence)
        target = p["identity"]
        need(os.geteuid() == 0 and socket.gethostname() == target["hostname"] and
             Path("/var/lib/cloud/data/instance-id").read_text().strip() == target["target_instance"], "Wrong original target")
        old = load_original(p, a.verifier, a.public_keys)
        owner = make_owner_class(old)(Path(target["directory"]), Path(target["release_directory"]), a.verifier, a.public_keys,
            Path(target["source_package"]), target["layout"], Path(target["edge_directory"]) if target["edge_directory"] else None,
            historical_exits=Path(target["historical_exits"]) if target["historical_exits"] else None)
        owner.set_recovery(p, identity, read_only=a.action == "check")
        owner.expected_current_sequence = a.current_sequence
        if a.action == "check":
            # Reuse the existing lock read-only. Never create a lock or witness
            # from check, including when the input points to a missing lock.
            print(json.dumps(check_owner(owner, old, a.current_sequence), sort_keys=True))
        else:
            result = owner.execute(resume=True)
            print(json.dumps({"recovered":result["state"] == "completed","status":result,"owner":identity}, sort_keys=True))
        return 0
    except Exception:
        # Do not echo row values, secrets, SQL or signatures on refusal.
        print("Recovery refused; retain original files and native checkpoint. No automatic retry or rollback.", file=__import__("sys").stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
