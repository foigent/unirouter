#!/usr/bin/env python3
"""Offline checks for the unbound v2/never consumer. No network or Docker."""
import contextlib
import copy
import datetime as dt
import importlib.util
import io
import json
import os
import subprocess
from pathlib import Path
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('consumer', ROOT / '.github/tests/community-release-acceptance.py')
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)

class ConsumerTests(unittest.TestCase):
    def setUp(self):
        self.config = m.load_config(ROOT)

    @contextlib.contextmanager
    def synthetic(self):
        # Deliberately synthetic, in-memory bindings only. Never a release input.
        c = copy.deepcopy(self.config)
        def fill(v, key=''):
            if v is None:
                if key.endswith('size') or key.endswith('_run') or key.endswith('_id'): return 1
                if key == 'diff_ids': return ['sha256:' + 'b' * 64]
                if key in ('tree', 'commit'): return 'a' * 40
                if key == 'url': return 'https://example.invalid/synthetic'
                return 'b' * 64
            if isinstance(v, dict): return {k: fill(x, k) for k, x in v.items()}
            if isinstance(v, list): return [fill(x) for x in v]
            return v
        c['immutable'] = fill(c['immutable'])
        now = dt.datetime.now(dt.timezone.utc)
        c['state'] = 'ready'
        c['release'].update(release_sequence=12, tag='v1.14.0-preview.12',
            created_at=(now - dt.timedelta(minutes=1)).strftime('%Y-%m-%dT%H:%M:%SZ'),
            expires_at='never', bootstrap_size=2048, bootstrap_sha256='c'*64,
            package_size=1000, package_sha256='d'*64)
        c['release']['bootstrap_url'] = m.asset_url(c['release'], 'community-bootstrap.json')
        with mock.patch.object(m, 'IMMUTABLE_SHA256', m.immutable_digest(c['immutable'])):
            yield c

    def refuses_without_effects(self, c):
        with mock.patch.object(m.subprocess, 'run') as run, mock.patch.object(m.tempfile, 'mkdtemp') as mkdir:
            with self.assertRaises((m.Stop, ValueError, TypeError, KeyError)):
                m.execute(c, ROOT)
            run.assert_not_called(); mkdir.assert_not_called()

    def test_prepared_config_is_incomplete_and_describe_is_pure(self):
        self.assertIsNone(self.config['immutable']['product']['commit'])
        self.assertIsNone(self.config['immutable']['images']['backend'])
        self.assertIsNone(self.config['immutable']['verifier']['sha256'])
        self.assertEqual(self.config['release']['expires_at'], 'never')
        with mock.patch.object(m.subprocess, 'run') as run, mock.patch.object(m.tempfile, 'mkdtemp') as mkdir:
            output=io.StringIO()
            with contextlib.redirect_stdout(output): self.assertEqual(m.main(['describe']),0)
            self.assertFalse(json.loads(output.getvalue())['ready'])
            run.assert_not_called(); mkdir.assert_not_called()
        self.refuses_without_effects(self.config)

    def test_nonexpiring_does_not_default_missing_or_finite_policy(self):
        with self.synthetic() as c:
            self.assertEqual(m.validate(c,ready=True),[])
            for value in (None,'','Never',' never','never ','2099-01-01T00:00:00Z'):
                q=copy.deepcopy(c); q['release']['expires_at']=value
                self.refuses_without_effects(q)
            q=copy.deepcopy(c); del q['release']['expires_at']; self.refuses_without_effects(q)

    def test_each_missing_release_or_build_binding_refuses(self):
        with self.synthetic() as c:
            for key in m.RELEASE_KEYS:
                q=copy.deepcopy(c); q['release'][key]=None; self.refuses_without_effects(q)
            for path in [('product','commit'),('images','backend'),('verifier','sha256')]:
                q=copy.deepcopy(c); q['immutable'][path[0]][path[1]]=None
                self.refuses_without_effects(q)

    def test_created_at_guard_remains(self):
        with self.synthetic() as c:
            for stamp in ('2099-01-01T00:00:00Z','2026-01-01T00:00:00+00:00','2026-01-01T00:00:00'):
                q=copy.deepcopy(c); q['release']['created_at']=stamp; self.refuses_without_effects(q)
            c['release']['created_at']='2000-01-01T00:00:00Z'
            self.assertEqual(m.validate(c,ready=True),[]) # old creation is not expiry

    def test_identity_and_sequence_still_bound(self):
        with self.synthetic() as c:
            for number in (True,0,11,2**63):
                q=copy.deepcopy(c); q['release']['release_sequence']=number; self.refuses_without_effects(q)
            for key in ('product','public_ring','images','app_image_identities','artifacts','provenance_sha256'):
                q=copy.deepcopy(c); q['immutable'][key]=None; self.refuses_without_effects(q)

    def test_exact_v2_manifest_binding_and_stale_v1_refusal(self):
        with self.synthetic() as c:
            f,r=c['immutable'],c['release']
            manifest={'schema_version':'unirouter.upgrade-manifest.v2','product':'unirouter',
                'key_id':f['key_id'],'signature_algorithm':'ed25519','release_sequence':r['release_sequence'],
                'version':f['product']['version'],'commit':f['product']['commit'],'created_at':r['created_at'],
                'expires_at':'never','target_os':'linux','target_arch':'amd64','min_current_sequence':0,
                'max_current_sequence':0,'min_current_schema':m.SCHEMA,'target_schema':m.SCHEMA,
                'provenance_sha256':f['provenance_sha256'],'artifacts':f['artifacts']}
            m.manifest_binding(c,manifest)
            for key,value in [('schema_version','unirouter.upgrade-manifest.v1'),('expires_at',None),
                              ('expires_at','2099-01-01T00:00:00Z'),('commit','0'*40)]:
                q=copy.deepcopy(manifest); q[key]=value
                with self.assertRaises(m.Stop):m.manifest_binding(c,q)

    def test_bootstrap_exact_new_package_and_unmodified_trust(self):
        with self.synthetic() as c:
            f,r=c['immutable'],c['release']
            value={'release':{'version':'1.14.0','commit':f['product']['commit'],
                'release_sequence':r['release_sequence'],'key_id':f['key_id'],'target_schema':m.SCHEMA},
                'assets':{'verifier':dict(f['verifier'],url=m.asset_url(r,'uniroute-upgrade-linux-amd64')),
                'public_keys':dict(f['public_ring'],url=m.asset_url(r,'public-keys.json')),
                'package':{'url':m.asset_url(r,'unirouter-1.14.0-linux-amd64.tar.gz'),
                'size':r['package_size'],'sha256':r['package_sha256']}}}
            m.bootstrap_binding(c,value)
            value['assets']['package']['url']=m.asset_url(r,'unirouter-1.13.0-linux-amd64.tar.gz')
            with self.assertRaises(m.Stop):m.bootstrap_binding(c,value)


    def test_non_native_context_never_runs_git_or_creates_destination(self):
        with self.synthetic() as value:
            self.refuses_without_effects(value)

    def native_environment_fixture(self):
        return {"PATH": "/usr/bin:/bin", "HOME": "/var/tmp/unused-test-home", "GITHUB_ACTIONS": "true",
                "RUNNER_ENVIRONMENT": "github-hosted", "GITHUB_REPOSITORY": "foigent/unirouter",
                "GITHUB_EVENT_NAME": "workflow_dispatch", "GITHUB_REF": "refs/heads/main",
                "GITHUB_WORKFLOW_REF": m.WORKFLOW, "GITHUB_SHA": "a" * 40,
                "GITHUB_WORKSPACE": str(ROOT), "GITHUB_RUN_ID": "123", "GITHUB_RUN_ATTEMPT": "1"}

    def test_wrong_repo_event_branch_runner_or_workflow_refuse_before_git(self):
        for key, bad in (("GITHUB_REPOSITORY", "other/repository"), ("GITHUB_EVENT_NAME", "pull_request"),
                         ("GITHUB_REF", "refs/heads/other"), ("RUNNER_ENVIRONMENT", "self-hosted"),
                         ("GITHUB_WORKFLOW_REF", "other/workflow@refs/heads/main")):
            env = self.native_environment_fixture()
            env[key] = bad
            with self.subTest(key=key), mock.patch.dict(os.environ, env, clear=True), \
                 mock.patch.object(m.platform, "system", return_value="Linux"), \
                 mock.patch.object(m.platform, "machine", return_value="x86_64"), \
                 mock.patch.object(m.os, "geteuid", return_value=0), mock.patch.object(m.subprocess, "run") as run:
                with self.assertRaises(m.Stop):
                    m.native_guard(ROOT)
                run.assert_not_called()

    def test_actual_checkout_mismatch_refuses_and_git_env_has_no_token(self):
        env = self.native_environment_fixture()
        env["GH_TOKEN"] = "sentinel-not-forwarded"
        with mock.patch.dict(os.environ, env, clear=True), mock.patch.object(m.platform, "system", return_value="Linux"), \
             mock.patch.object(m.platform, "machine", return_value="x86_64"), mock.patch.object(m.os, "geteuid", return_value=0), \
             mock.patch.object(m.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, stdout=b"b" * 40)) as run:
            with self.assertRaises(m.Stop):
                m.native_guard(ROOT)
            self.assertNotIn("GH_TOKEN", run.call_args.kwargs["env"])

    def test_secret_environment_is_removed_and_original_restored(self):
        with mock.patch.dict(os.environ, {"GH_TOKEN": "sentinel-never-emit", "DOCKER_AUTH_CONFIG": "sentinel-never-emit"}):
            original = dict(os.environ)
            clean = {"PATH": "/usr/bin:/bin", "HOME": "/var/tmp/new-owned-anonymous-home"}
            with m.clean_environment(clean):
                self.assertEqual(dict(os.environ), clean)
            self.assertEqual(dict(os.environ), original)

    def test_changed_helper_is_never_imported(self):
        with tempfile.TemporaryDirectory(prefix="ur-formal-helper-test-") as temp:
            path = Path(temp) / "helper.py"
            marker = Path(temp) / "must-not-exist"
            path.write_text("from pathlib import Path\nPath(" + repr(str(marker)) + ").write_text('bad')\n")
            with self.assertRaises(m.Stop):
                m.load_module(path, "0" * 64, "must_not_import")
            self.assertFalse(marker.exists())

    def test_duplicate_json_fields_refuse(self):
        with self.assertRaises(m.Stop):
            m.strict_json(b'{"state":"incomplete","state":"ready"}')

if __name__=='__main__':unittest.main()
