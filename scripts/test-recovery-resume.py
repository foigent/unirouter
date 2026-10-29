#!/usr/bin/env python3
"""Offline native-owner continuation tests; no Docker, Target or formal keys."""
import copy
import datetime
import hashlib
import importlib.util
import io
import json
import tarfile
from pathlib import Path
import tempfile
import unittest
from contextlib import redirect_stdout
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module
R = load("timestamp_recovery", ROOT / "deploy/customer/recovery_resume.py")
F = load("existing_finite_owner_fixture", ROOT / "scripts/test-community-upgrade.py")
M = F.M


class RecoveryTests(F.WholeUpgradeTests):
    # Reuse the existing transport fixture and original class constructor. Only
    # this class's test_recovery_* cases are selected by this script's main.
    def pending(self, second_row=False, cleanup=True, expired=True):
        operation, state, calls, bind = self.host(1)
        state.setups.append({"installation_id":"xci_" + "A" * 22, "org_id":2, "project_id":2,
            "setup_state":"waiting_data", "setup_progress":{"source_grant_id":1},
            "control_fence":1, "updated_at":"2026-10-28T00:00:00Z"})
        if second_row:
            state.setups.append(dict(state.setups[0], installation_id="xci_" + "B" * 22, project_id=3))
        state.sessions.append({"id":1,"user_id":2,"sid_hash":"synthetic-only", "revoked_at":None,
            "expires_at":"2026-10-28T23:00:00Z" if expired else "2026-11-01T00:00:00Z"})
        def tick(point):
            if point == "done:start-writers":
                state.setups[0]["updated_at"] = "2026-10-29T00:00:01Z"
                if cleanup and expired: state.sessions.clear()
        operation.test_checkpoint = tick
        with patch.object(M.shutil, "disk_usage", return_value=F.SimpleNamespace(free=32 << 30)):
            with self.assertRaisesRegex(M.install.InstallError, "Protected original database/content facts changed"):
                operation.execute()
        self.assertEqual("record-release", operation.witness["pending"])
        pair = operation._pair(); w = operation.witness
        old = state.clones[pair["verification_database"]]["setups"][0]
        writer = w["receipts"]["start-writers"]["writers"]["backend"]
        start = state.resources["fixture-ur-backend-1"]["State"]["StartedAt"]
        plan = {"schema_version":R.PLAN_SCHEMA, "tool_version":R.TOOL_VERSION,
            "identity":{"target_instance":"i-fixture", "hostname":"fixture", "installation_id":operation.state["installation_id"],
                "operation_id":w["operation_id"], "directory":str(operation.directory), "release_directory":str(operation.release.directory),
                "source_package":str(operation.source_package), "layout":operation.layout, "edge_directory":None, "historical_exits":None},
            "source":{k:w["source"][k] for k in ("release_sequence", "package_sha256")},
            "candidate":dict({k:w["candidate"][k] for k in ("release_sequence", "package_sha256", "commit")}, manifest_sha256=w["candidate_manifest_sha256"]),
            "database":{"name":w["database"], "system_id":w["database_system_id"], "oid":w["database_oid"], "schema":R.M128,
                "ledger_sha256":w["receipts"]["migrate"]["ledger_sha256"]},
            "cold_pair":{"receipt_sha256":w["receipts"]["cold-pair"]["receipt_sha256"], "dump_sha256":pair["dump_sha256"], "verification_database":pair["verification_database"]},
            "frozen":{"initial_witness_sha256":R.sha((operation.directory / "install-state.json").read_bytes()),
                "source_state_sha256":w["source_state_sha256"], "configuration_map_sha256":R.sha(M.canonical(w["configuration_sha256"])),
                "prefix_receipts_sha256":R.sha(M.canonical({s:w["receipts"][s] for s in w["done"]}))},
            "writer":dict(writer, started_at=start, pid=state.resources["fixture-ur-backend-1"]["State"]["Pid"]),
            "allowed_row":{"installation_id":old["installation_id"], "org_id":old["org_id"], "project_id":old["project_id"],
                "updated_at":old["updated_at"], "setup_state":old["setup_state"],
                "setup_progress_sha256":R.sha(M.canonical(old["setup_progress"])),
                "all_other_columns_sha256":R.sha(M.canonical({k:v for k,v in old.items() if k != "updated_at"}))}}
        plan["allowed_session_gc"] = {"table":R.SESSION_TABLE,"source_table":pair["tables"][R.SESSION_TABLE],
            "max_removed":1,"expires_before":R.docker_start_floor(start).isoformat()}
        identity = {"schema_version":R.OWNER_SCHEMA, "package_sha256":"7" * 64, "plan_sha256":R.sha(R.canonical(plan)),
                    "source_commit":"9" * 40, "tool_version":R.TOOL_VERSION, "release_sequence":12}
        def reopen(read_only=False, plan_override=None, identity_override=None):
            cls = R.make_owner_class(M)
            target = cls(operation.directory, operation.release.directory, operation.verifier, operation.public_keys,
                         operation.source_package, operation.layout, operation.edge_directory, operation.run)
            bind(target)
            return target.set_recovery(plan_override or plan, identity_override or identity, read_only=read_only)
        return operation, state, calls, bind, plan, identity, reopen

    def snapshot(self, directory):
        return {str(p): (R.sha(p.read_bytes()), p.stat().st_mode) for p in directory.rglob("*") if p.is_file()}

    def test_recovery_check_is_pure_and_resume_reuses_original_native_steps(self):
        operation, state, calls, bind, plan, identity, reopen = self.pending()
        files, metadata, native = self.snapshot(operation.directory), copy.deepcopy(state.metadata), list(state.native_calls)
        checked = R.check_owner(reopen(True), M, plan["source"]["release_sequence"])
        self.assertTrue(checked["checked"]); self.assertEqual(0, checked["writes"])
        self.assertEqual(files, self.snapshot(operation.directory)); self.assertEqual(metadata, state.metadata)
        self.assertEqual(native, state.native_calls)
        with redirect_stdout(io.StringIO()):
            result = reopen().execute(resume=True)
        self.assertEqual("completed", result["state"])
        self.assertEqual(1, state.native_calls.count(["migrate", "up"]))
        self.assertEqual(1, sum(c[:2] == ["upgrade", "record-applied"] for c in state.native_calls))
        saved = json.loads((operation.directory / "install-state.json").read_bytes())
        self.assertEqual(identity, saved["upgrade"]["recovery_owner"])
        self.assertEqual({"record-release", "prepare-admission", "start-receivers"}, set(saved["upgrade"]["recovery_observations"]))
        stock = bind(M.Upgrade(operation.directory, operation.release.directory, operation.verifier, operation.public_keys,
                              operation.source_package, operation.layout, operation.edge_directory, operation.run))
        self.assertEqual("completed", stock.execute(resume=True)["state"], "unmodified original completed owner must still accept")

    def test_recovery_other_drift_refuses_before_first_witness_or_native_write(self):
        for kind in ("other_column", "progress", "other_table", "other_row", "future_time", "before_start", "row_deleted", "scope", "restore", "writer", "plan", "witness"):
            with self.subTest(kind=kind):
                operation, state, calls, bind, plan, identity, reopen = self.pending()
                if kind == "other_column": state.setups[0]["control_fence"] += 1
                if kind == "progress": state.setups[0]["setup_progress"]["extra"] = True
                if kind == "other_table": state.business.append({"id":2,"value":"unexpected"})
                if kind == "other_row": state.setups.append(dict(state.setups[0], installation_id="xci_" + "B" * 22))
                if kind == "future_time": state.setups[0]["updated_at"] = "2100-01-01T00:00:00Z"
                if kind == "before_start": state.setups[0]["updated_at"] = "2026-10-28T23:59:59Z"
                if kind == "row_deleted": state.setups.clear()
                if kind == "scope": state.setups[0]["project_id"] = 3
                if kind == "restore": state.clones[plan["cold_pair"]["verification_database"]]["business"].append({"id":2})
                if kind == "writer": state.resources["fixture-ur-backend-1"]["State"]["StartedAt"] = "2026-10-29T00:00:00Z"
                if kind == "plan": plan = copy.deepcopy(plan); plan["candidate"]["package_sha256"] = "0" * 64
                if kind == "witness":
                    value = json.loads((operation.directory / "install-state.json").read_bytes())
                    value["upgrade"]["unexpected"] = True
                    M.install.save_state(operation.directory, value)
                files, native = self.snapshot(operation.directory), list(state.native_calls)
                with redirect_stdout(io.StringIO()):
                    with self.assertRaises((R.RecoveryError, M.install.InstallError, ValueError, KeyError, StopIteration)):
                        reopen(plan_override=plan).execute(resume=True)
                self.assertEqual(files, self.snapshot(operation.directory))
                self.assertEqual(native, state.native_calls)

    def test_recovery_every_remaining_effect_resumes_without_migration_or_data_loss(self):
        for phase in R.REMAINING:
            with self.subTest(phase=phase):
                operation, state, calls, bind, plan, identity, reopen = self.pending()
                first = reopen()
                def interrupt(point):
                    if point == "effect:" + phase:
                        if phase == "admit-receivers":
                            state.business.append({"id":2,"value":"legitimate post-admission"})
                            state.models[0].update(upstream_release_observation={"synthetic":True}, upstream_observation_revision=1)
                        raise F.Interrupted()
                first.test_checkpoint = interrupt
                with redirect_stdout(io.StringIO()):
                    with self.assertRaises(F.Interrupted): first.execute(resume=True)
                before = self.snapshot(operation.directory)
                checked = R.check_owner(reopen(True), M, state.metadata["release"]["release_sequence"])
                self.assertTrue(checked["checked"]); self.assertEqual(before, self.snapshot(operation.directory))
                with redirect_stdout(io.StringIO()):
                    self.assertEqual("completed", reopen().execute(resume=True)["state"])
                self.assertEqual(1, state.native_calls.count(["migrate", "up"]))
                self.assertEqual(1, sum(c[:2] == ["upgrade", "record-applied"] for c in state.native_calls))
                if phase == "admit-receivers": self.assertEqual(2, len(state.business))

    def test_recovery_bound_owner_still_rejects_original_receipt_owner_and_prefix_drift(self):
        for kind in ("receipt", "owner", "prefix", "pending"):
            with self.subTest(kind=kind):
                operation, state, calls, bind, plan, identity, reopen = self.pending()
                first = reopen()
                def interrupt(point):
                    if point == "effect:record-release": raise F.Interrupted()
                first.test_checkpoint = interrupt
                with redirect_stdout(io.StringIO()):
                    with self.assertRaises(F.Interrupted): first.execute(resume=True)
                value = json.loads((operation.directory / "install-state.json").read_bytes())
                w = value["upgrade"]
                self.assertEqual(identity, w["recovery_owner"])
                if kind == "receipt": w["receipts"]["retire-writers"] = {"unexpected":True}
                if kind == "owner": w["recovery_owner"]["package_sha256"] = "0" * 64
                if kind == "prefix": w["done"][0] = "unknown-step"
                if kind == "pending": w["pending"] = "unknown-effect"
                M.install.save_state(operation.directory, value)
                before, native = self.snapshot(operation.directory), list(state.native_calls)
                reason = {"receipt":"Original completed native receipts changed", "owner":"Another recovery owner",
                          "prefix":"Unsupported recovery completed prefix", "pending":"Unknown pending recovery effect"}[kind]
                with self.assertRaisesRegex(R.RecoveryError, reason):
                    R.check_owner(reopen(True), M, state.metadata["release"]["release_sequence"])
                self.assertEqual(before, self.snapshot(operation.directory)); self.assertEqual(native, state.native_calls)

    def test_recovery_another_original_rows_timestamp_is_never_exempt(self):
        operation, state, calls, bind, plan, identity, reopen = self.pending(second_row=True)
        state.setups[1]["updated_at"] = "2026-10-29T00:00:02Z"
        before, native = self.snapshot(operation.directory), list(state.native_calls)
        with self.assertRaisesRegex(R.RecoveryError, "Other protected original"):
            reopen().execute(resume=True)
        self.assertEqual(before, self.snapshot(operation.directory)); self.assertEqual(native, state.native_calls)

    def test_recovery_completed_check_preserves_legitimate_new_business_writes(self):
        operation, state, calls, bind, plan, identity, reopen = self.pending()
        with redirect_stdout(io.StringIO()): self.assertEqual("completed", reopen().execute(resume=True)["state"])
        state.business.append({"id":2,"value":"legitimate"})
        state.setups[0]["setup_progress"]["later_real_change"] = True
        state.models[0].update(upstream_release_observation={"synthetic":True}, upstream_observation_revision=1)
        state.sessions.append({"id":2,"user_id":2,"sid_hash":"synthetic-post-admission","expires_at":"2026-12-01T00:00:00Z"})
        before = self.snapshot(operation.directory)
        self.assertTrue(R.check_owner(reopen(True), M, plan["candidate"]["release_sequence"])["checked"])
        self.assertEqual(before, self.snapshot(operation.directory))
        with redirect_stdout(io.StringIO()): self.assertEqual("completed", reopen().execute(resume=True)["state"])
        self.assertEqual(2, len(state.business))

    def test_recovery_missing_existing_lock_never_creates_one(self):
        operation, state, calls, bind, plan, identity, reopen = self.pending()
        # Task-created fixture only. Production check never creates this path.
        (operation.directory / ".install.lock").unlink()
        before = self.snapshot(operation.directory)
        with self.assertRaises(FileNotFoundError): R.check_owner(reopen(True), M, plan["source"]["release_sequence"])
        self.assertEqual(before, self.snapshot(operation.directory))

    def test_recovery_read_allows_access_time_but_rejects_links_and_duplicate_data(self):
        with tempfile.TemporaryDirectory() as raw:
            root = Path(raw).resolve()
            path = root / "file"; path.write_bytes(b"public fixture")
            self.assertEqual(b"public fixture", R.read(path))
            link = root / "link"; link.symlink_to(path)
            with self.assertRaises(R.RecoveryError): R.read(link)
        with self.assertRaises(R.RecoveryError): R.strict_json(b'{"x":1,"x":2}')
        with self.assertRaises(R.RecoveryError): R.strict_json(b'{"x":NaN}')
        with self.assertRaises(R.RecoveryError): R.public_fields("verified=true\nverified=true\n")

    def test_recovery_nanosecond_start_rounds_toward_safe_microsecond(self):
        self.assertEqual(R.stamp("2026-10-29T00:00:00.123456Z"), R.docker_start_floor("2026-10-29T00:00:00.123456789Z"))
        self.assertEqual(R.stamp("2026-10-29T00:00:00.123457Z"), R.docker_start_ceiling("2026-10-29T00:00:00.123456789Z"))
        self.assertEqual(R.stamp("2026-10-29T00:00:00.123456Z"), R.docker_start_ceiling("2026-10-29T00:00:00.123456000Z"))

    def test_recovery_expired_session_gc_and_retained_live_session(self):
        for cleanup,expired in ((True,True),(False,True),(False,False)):
            with self.subTest(cleanup=cleanup,expired=expired):
                operation,state,calls,bind,plan,identity,reopen=self.pending(cleanup=cleanup,expired=expired)
                before=self.snapshot(operation.directory)
                result=R.check_owner(reopen(True),M,plan['source']['release_sequence'])
                self.assertEqual(int(cleanup and expired),result['observation']['session_gc']['removed_count'])
                self.assertEqual(before,self.snapshot(operation.directory))
                with redirect_stdout(io.StringIO()):self.assertEqual('completed',reopen().execute(resume=True)['state'])

    def test_recovery_session_changes_and_plan_drift_refuse_before_effect(self):
        for kind in ('missing_live','changed_live','new_expired','changed_expired','late_expiry','cutoff','count','hash','maximum','unknown_field'):
            with self.subTest(kind=kind):
                live=kind in ('missing_live','changed_live')
                operation,state,calls,bind,plan,identity,reopen=self.pending(cleanup=False,expired=not live)
                if kind in ('missing_live','late_expiry'):state.sessions.clear()
                if kind in ('changed_live','changed_expired'):state.sessions[0]['sid_hash']='synthetic-changed'
                if kind=='new_expired':state.sessions.append(dict(state.sessions[0],id=2))
                if kind=='late_expiry':
                    clone=state.clones[plan['cold_pair']['verification_database']]
                    clone['sessions'][0]['expires_at']='2026-10-29T00:00:00.000001Z'
                if kind=='cutoff':plan['allowed_session_gc']['expires_before']='2026-10-30T00:00:00+00:00'
                if kind=='count':plan['allowed_session_gc']['source_table']=dict(plan['allowed_session_gc']['source_table'],count=2)
                if kind=='hash':plan['allowed_session_gc']['source_table']=dict(plan['allowed_session_gc']['source_table'],sha256='0'*64)
                if kind=='maximum':plan['allowed_session_gc']['max_removed']=2
                if kind=='unknown_field':plan['allowed_session_gc']['extra']=True
                before,native=self.snapshot(operation.directory),list(state.native_calls)
                with redirect_stdout(io.StringIO()):
                    with self.assertRaises((R.RecoveryError,M.install.InstallError)):
                        reopen().execute(resume=True)
                self.assertEqual(before,self.snapshot(operation.directory));self.assertEqual(native,state.native_calls)

    def test_recovery_session_observation_race_refuses_before_first_effect(self):
        for side in ('live','live_changed','restore'):
            with self.subTest(side=side):
                operation,state,calls,bind,plan,identity,reopen=self.pending(cleanup=side!='live_changed',expired=side!='live_changed')
                before,native=self.snapshot(operation.directory),list(state.native_calls)
                original=M.recovery.table_fingerprints
                fired=[]
                def change(sql,db,*args,**kwargs):
                    projections=kwargs.get('projections',{})
                    wanted=(db==operation.env['POSTGRES_DB']) if side.startswith('live') else (db==plan['cold_pair']['verification_database'] and R.SESSION_TABLE in projections)
                    if wanted and projections and not fired:
                        rows=state.sessions if side.startswith('live') else state.clones[db]['sessions']
                        if side=='live_changed':rows[0]['sid_hash']='synthetic-race-change'
                        else:rows.append({'id':2,'user_id':2,'sid_hash':'synthetic-race','revoked_at':None,'expires_at':'2026-10-28T23:00:00Z'})
                        fired.append(True)
                    return original(sql,db,*args,**kwargs)
                with patch.object(M.recovery,'table_fingerprints',side_effect=change),redirect_stdout(io.StringIO()):
                    with self.assertRaises(R.RecoveryError):reopen().execute(resume=True)
                self.assertEqual([True],fired)
                self.assertEqual(before,self.snapshot(operation.directory));self.assertEqual(native,state.native_calls)

    def test_recovery_body_guard_still_refuses_before_first_effect(self):
        operation,state,calls,bind,plan,identity,reopen=self.pending()
        before,native=self.snapshot(operation.directory),list(state.native_calls)
        owner=reopen()
        with patch.object(owner,'_content',return_value={'objects':[],'inventory':{'synthetic-changed':True}}),redirect_stdout(io.StringIO()):
            with self.assertRaisesRegex(R.RecoveryError,'Other protected original'):owner.execute(resume=True)
        self.assertEqual(before,self.snapshot(operation.directory));self.assertEqual(native,state.native_calls)


def native_envelope_cases(native, directory, private_key, ring):
    """Real native cryptography with fixture keys; no formal signing identity."""
    case = RecoveryTests("test_recovery_nanosecond_start_rounds_toward_safe_microsecond")
    case.setUp()
    try:
        _, _, _, _, plan, _, _ = case.pending()
        root = directory / "extracted"; root.mkdir(mode=0o700)
        verifier = directory / "fixture-verifier-identity"
        verifier.write_bytes(b"test-only native process adapter; not executable")
        source = "b" * 40
        def call(args):
            result = native(args[1:])
            R.need(result.returncode == 0, "Native fixture command refused")
            return result.stdout.decode()
        def pack(path, artifacts, *, version, sequence, minimum, maximum, schema, provenance=None):
            args = ["pack", "--key-id", "recovery-fixture", "--private-key", private_key,
                "--release-sequence", str(sequence), "--version", version, "--commit", source,
                "--created-at", "2026-10-01T00:00:00Z", "--no-expiry", "--target-os", "linux", "--target-arch", "amd64",
                "--min-current-sequence", str(minimum), "--max-current-sequence", str(maximum),
                "--min-current-schema", schema, "--target-schema", R.M128, "--out", path]
            if provenance: args += ["--provenance-sha256", provenance]
            for name, file in artifacts.items(): args += ["--artifact", name + "=" + str(file)]
            result = native(args)
            case.assertEqual(0, result.returncode, "native fixture packing failed")
        # Authenticate actual old Python import dependencies before execution.
        old = directory / "old"; (old / "payload/deploy").mkdir(mode=0o700, parents=True)
        old_files = {}
        for name in ("install.py", "recovery.py", "upgrade.py"):
            dest = old / "payload/deploy" / name
            dest.write_bytes((ROOT / "deploy/customer" / name).read_bytes())
            old_files["payload/deploy/" + name] = dest
        old_package = old / "release.tar.gz"
        pack(old_package, old_files, version="1.15.0", sequence=11, minimum=10, maximum=10, schema=R.M127)
        with tarfile.open(old_package) as archive: manifest_raw = archive.extractfile("manifest.json").read()
        plan["identity"]["release_directory"] = str(old)
        plan["candidate"].update(package_sha256=R.sha(old_package.read_bytes()), manifest_sha256=R.sha(manifest_raw), commit=source)
        program = root / "payload/recovery/resume.py"; program.parent.mkdir(mode=0o700, parents=True)
        program.write_bytes((ROOT / "deploy/customer/recovery_resume.py").read_bytes())
        plan_file = program.with_name("plan.json"); plan_file.write_bytes(R.canonical(plan))
        provenance = root / "payload/provenance/build-provenance.json"
        provenance.parent.mkdir(mode=0o700)
        provenance.write_bytes(R.canonical({"schema_version":"unirouter.recovery-source.v1", "source_commit":source,
            "plan_sha256":R.sha(plan_file.read_bytes()), "program_sha256":R.sha(program.read_bytes())}))
        artifacts = {"payload/recovery/resume.py":program, "payload/recovery/plan.json":plan_file, "payload/provenance/build-provenance.json":provenance}
        package = directory / "synthetic-recovery.tar.gz"
        pack(package, artifacts, version=R.TOOL_VERSION, sequence=12, minimum=10, maximum=11, schema=R.M128, provenance=R.sha(provenance.read_bytes()))
        refused = 0
        with patch.object(R, "VERIFIER_SHA256", R.sha(verifier.read_bytes())), patch.object(R, "RING_SHA256", R.sha(ring.read_bytes())):
            for current in (10, 11):
                checked, owner = R.verify_envelope(package, root, verifier, ring, current, run=call)
                case.assertEqual(plan, checked); case.assertEqual(12, owner["release_sequence"])
            for current in (9, 12):
                with case.assertRaises(R.RecoveryError): R.verify_envelope(package, root, verifier, ring, current, run=call)
                refused += 1
            for path in (program, plan_file, provenance):
                saved = path.read_bytes(); path.write_bytes(saved + b"changed")
                with case.assertRaises(R.RecoveryError): R.verify_envelope(package, root, verifier, ring, 10, run=call)
                refused += 1; path.write_bytes(saved)
            wrong_ring = directory / "wrong-ring.json"; wrong_ring.write_bytes(b'{}')
            with case.assertRaises(R.RecoveryError): R.verify_envelope(package, root, verifier, wrong_ring, 10, run=call)
            refused += 1
            tampered = directory / "tampered.tar.gz"; saved = bytearray(package.read_bytes()); saved[len(saved)//2] ^= 1; tampered.write_bytes(saved)
            with case.assertRaises(R.RecoveryError): R.verify_envelope(tampered, root, verifier, ring, 10, run=call)
            refused += 1
            # Loading the authenticated originals is safe; each dependency
            # tamper must be rejected BEFORE its top-level code can run.
            loaded = R.load_original(plan, verifier, ring, run=call)
            case.assertTrue(hasattr(loaded, "Upgrade"))
            def ordinary_run(args, **_):
                result = native(args[1:])
                loaded.install.require(result.returncode == 0, "Native installer verification refused")
                return result.stdout.decode()
            # A repair envelope cannot enter the ordinary fresh installer:
            # its sequence window refuses zero before any installation effect.
            with case.assertRaisesRegex(loaded.install.InstallError, "Native installer verification refused"):
                loaded.install.Release(root, package, verifier, ring, "amd64", ordinary_run)
            # Even a fixture signature with a fresh-compatible window cannot
            # replace the ordinary installer's complete application inventory.
            thin = directory / "synthetic-thin-fresh-window.tar.gz"
            pack(thin, artifacts, version=R.TOOL_VERSION, sequence=12, minimum=0, maximum=11,
                 schema=R.M128, provenance=R.sha(provenance.read_bytes()))
            with case.assertRaisesRegex(loaded.install.InstallError, "Signed release is missing payload/install.json"):
                loaded.install.Release(root, thin, verifier, ring, "amd64", ordinary_run)
            with case.assertRaises(R.RecoveryError):
                R.verify_envelope(thin, root, verifier, ring, 10, run=call)
            refused += 1
            sentinel = directory / "must-not-execute"
            for path in old_files.values():
                saved = path.read_bytes()
                path.write_text("from pathlib import Path\nPath(" + repr(str(sentinel)) + ").write_text('bad')\n")
                with case.assertRaises(R.RecoveryError): R.load_original(plan, verifier, ring, run=call)
                case.assertFalse(sentinel.exists()); path.write_bytes(saved)
        return {"positive_windows":2,"refused_cases":refused,"old_dependencies_refused":3,
                "ordinary_installer_refused":2}
    finally:
        case.doCleanups()


if __name__ == "__main__":
    names = [n for n in unittest.defaultTestLoader.getTestCaseNames(RecoveryTests) if n.startswith("test_recovery_")]
    result = unittest.TextTestRunner(verbosity=2).run(unittest.TestSuite(RecoveryTests(n) for n in names))
    raise SystemExit(not result.wasSuccessful())
