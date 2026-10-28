#!/usr/bin/env python3
"""Offline checks for fixed and incomplete v2/never inputs. No network or Docker."""
import contextlib
import copy
import datetime as dt
import importlib.util
import io
import json
import os
import re
import subprocess
from pathlib import Path
import tempfile
import types
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('consumer', ROOT / '.github/tests/community-release-acceptance.py')
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)

class ConsumerTests(unittest.TestCase):
    def setUp(self):
        self.config = m.load_config(ROOT)

    def test_formal_instance_uses_closed_record_schema_instead_of_live_checkout(self):
        spec=importlib.util.spec_from_file_location('native_fixture_for_consumer',ROOT/'.github/tests/community-installer.py')
        native=importlib.util.module_from_spec(spec);spec.loader.exec_module(native)
        for schema in ('000127_xinsight_communication_policy','000128_upstream_release_observation'):
            with self.subTest(schema=schema),tempfile.TemporaryDirectory() as tmp:
                config=copy.deepcopy(self.config)
                config['immutable']['product']['schema']=schema
                def loader(path,digest,name):
                    return native if name=='accepted_native_installer' else types.SimpleNamespace()
                with mock.patch.object(m,'load_module',side_effect=loader),mock.patch.object(m.shutil,'which',return_value='/fixture/docker'):
                    instance=m.make_acceptance(ROOT,Path(tmp),config,{})
                self.assertEqual(instance.schema,schema)
                self.assertEqual(instance.release_sequence,config['release']['release_sequence'])

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
        c['release'].update(release_sequence=c['immutable']['historical_sequence_reserved_through'] + 1,
            tag='v' + c['immutable']['product']['version'] + '-preview.' + str(c['immutable']['historical_sequence_reserved_through'] + 1),
            created_at=(now - dt.timedelta(minutes=1)).strftime('%Y-%m-%dT%H:%M:%SZ'),
            expires_at='never', bootstrap_size=2048, bootstrap_sha256='c'*64,
            package_size=1000, package_sha256='d'*64)
        c['release']['bootstrap_url'] = m.asset_url(c['release'], 'community-bootstrap.json')
        with mock.patch.object(m, 'IMMUTABLE_SHA256', m.immutable_digest(c['immutable'])):
            yield c

    def refuses_without_effects(self, c):
        with mock.patch.object(m.subprocess, 'run') as run, mock.patch.object(m.tempfile, 'mkdtemp') as mkdir:
            # Preserve the actual historical data-negative assertions; missing
            # current-stage authority must not make all of these pass trivially.
            with self.assertRaises((m.Stop, ValueError, TypeError, KeyError)):
                m.validate(c, ready=True)
            with self.assertRaises((m.Stop, ValueError, TypeError, KeyError)):
                m.execute(c, ROOT)
            run.assert_not_called(); mkdir.assert_not_called()

    def test_incomplete_release_decision_cannot_execute_before_signature_pins(self):
        value = copy.deepcopy(self.config)
        value['state'] = 'incomplete'
        for key in m.RELEASE_KEYS - {'expires_at'}:
            value['release'][key] = None
        self.assertEqual(value['immutable']['product']['commit'],
                         '21572d9e73ca901583a8b526c9af7cd0319d59f1')
        self.assertIsNone(value['release']['release_sequence'])
        self.assertEqual(value['release']['expires_at'], 'never')
        with mock.patch.object(m, 'load_config', return_value=value), \
                mock.patch.object(m.subprocess, 'run') as run, mock.patch.object(m.tempfile, 'mkdtemp') as mkdir:
            output, error = io.StringIO(), io.StringIO()
            with contextlib.redirect_stdout(output), contextlib.redirect_stderr(error):
                self.assertEqual(m.main(['describe']), 0)
                self.assertFalse(json.loads(output.getvalue())['ready'])
                self.assertEqual(m.main(['validate']), 1)
            run.assert_not_called(); mkdir.assert_not_called()
        self.refuses_without_effects(value)

    def test_actual_signed_release_bindings_validate_without_effects(self):
        self.assertEqual(self.config['state'], 'ready')
        self.assertEqual(self.config['release']['release_sequence'], 16)
        self.assertEqual(self.config['release']['tag'], 'v1.14.2-preview.16')
        self.assertEqual(self.config['release']['created_at'], '2026-10-25T00:59:50Z')
        self.assertEqual(self.config['release']['package_size'], 130032075)
        self.assertEqual(self.config['release']['package_sha256'],
                         '2a7177d08ed74f8cccdaaeca04a69cd1f2706727e8b282614651b7056d8309e7')
        # The historical signed record stays readable. Its old bootstrap program
        # must not be silently substituted by the current upgrade-capable owner.
        with self.assertRaises(m.Stop): m.reference_sources(ROOT, self.config)
        with mock.patch.object(m.subprocess, 'run') as run, mock.patch.object(m.tempfile, 'mkdtemp') as mkdir:
            output = io.StringIO()
            with contextlib.redirect_stdout(output):
                self.assertEqual(m.main(['describe']), 0)
                self.assertTrue(json.loads(output.getvalue())['ready'])
                self.assertEqual(m.main(['validate']), 0)
            run.assert_not_called(); mkdir.assert_not_called()

    def test_incomplete_fixture_still_refuses_without_effects(self):
        value = copy.deepcopy(self.config)
        value['state'] = 'incomplete'
        for key in ('release_sequence', 'created_at', 'tag', 'bootstrap_url',
                    'bootstrap_size', 'bootstrap_sha256', 'package_size', 'package_sha256'):
            value['release'][key] = None
        for path in (('product', 'commit'), ('images', 'backend'), ('verifier', 'sha256')):
            value['immutable'][path[0]][path[1]] = None
        with mock.patch.object(m, 'IMMUTABLE_SHA256', m.immutable_digest(value['immutable'])):
            with mock.patch.object(m, 'load_config', return_value=value), \
                 mock.patch.object(m.subprocess, 'run') as run, mock.patch.object(m.tempfile, 'mkdtemp') as mkdir:
                output = io.StringIO()
                with contextlib.redirect_stdout(output): self.assertEqual(m.main(['describe']), 0)
                self.assertFalse(json.loads(output.getvalue())['ready'])
                run.assert_not_called(); mkdir.assert_not_called()
            self.refuses_without_effects(value)

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
            for number in (True,0,11,12,2**63):
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
                'max_current_sequence':f['max_current_sequence'],'min_current_schema':m.SCHEMA,'target_schema':m.SCHEMA,
                'provenance_sha256':f['provenance_sha256'],'artifacts':f['artifacts']}
            m.manifest_binding(c,manifest)
            for key,value in [('schema_version','unirouter.upgrade-manifest.v1'),('expires_at',None),
                              ('expires_at','2099-01-01T00:00:00Z'),('commit','0'*40)]:
                q=copy.deepcopy(manifest); q[key]=value
                with self.assertRaises(m.Stop):m.manifest_binding(c,q)

    def test_bootstrap_exact_new_package_and_unmodified_trust(self):
        with self.synthetic() as c:
            f,r=c['immutable'],c['release']
            value={'release':{'version':f['product']['version'],'commit':f['product']['commit'],
                'release_sequence':r['release_sequence'],'key_id':f['key_id'],'target_schema':m.SCHEMA},
                'assets':{'verifier':dict(f['verifier'],url=m.asset_url(r,'uniroute-upgrade-linux-amd64')),
                'public_keys':dict(f['public_ring'],url=m.asset_url(r,'public-keys.json')),
                'package':{'url':m.asset_url(r,'unirouter-' + f['product']['version'] + '-linux-amd64.tar.gz'),
                'size':r['package_size'],'sha256':r['package_sha256']}}}
            m.bootstrap_binding(c,value)
            value['assets']['package']['url']=m.asset_url(r,'unirouter-1.13.0-linux-amd64.tar.gz')
            with self.assertRaises(m.Stop):m.bootstrap_binding(c,value)


    def test_non_native_context_never_runs_git_or_creates_destination(self):
        with self.synthetic() as value:
            self.assertEqual([],m.validate(value,ready=True))
            with mock.patch.dict(os.environ,{},clear=True),mock.patch.object(m.subprocess,'run') as run, \
                 mock.patch.object(m.tempfile,'mkdtemp') as mkdir:
                with self.assertRaises(m.Stop):m.native_guard(ROOT)
                with self.assertRaises(m.Stop):m.execute(value,ROOT)
                run.assert_not_called();mkdir.assert_not_called()

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


    def test_1141_tag_package_and_zero_thirteen_window_use_the_same_pinned_subject(self):
        c=copy.deepcopy(self.config);f=c['immutable'];r=c['release']
        f['product']['version']='1.14.1'
        f['historical_sequence_reserved_through']=13
        f['min_current_sequence']=0;f['max_current_sequence']=13
        c['state']='ready'  # Synthetic in-memory identity only, never an allocated release.
        r.update(release_sequence=14,tag='v1.14.1-preview.14',created_at='2000-01-01T00:00:00Z',
            bootstrap_size=1024,bootstrap_sha256='c'*64,package_size=1000,package_sha256='d'*64)
        r['bootstrap_url']=m.asset_url(r,'community-bootstrap.json')
        with mock.patch.object(m,'IMMUTABLE_SHA256',m.immutable_digest(f)):
            self.assertEqual(m.validate(c,ready=True),[])
            manifest={'schema_version':'unirouter.upgrade-manifest.v2','product':'unirouter',
                'key_id':f['key_id'],'signature_algorithm':'ed25519','release_sequence':14,
                'version':'1.14.1','commit':f['product']['commit'],'created_at':r['created_at'],
                'expires_at':'never','target_os':'linux','target_arch':'amd64',
                'min_current_sequence':0,'max_current_sequence':13,'min_current_schema':m.SCHEMA,
                'target_schema':m.SCHEMA,'provenance_sha256':f['provenance_sha256'],'artifacts':f['artifacts']}
            m.manifest_binding(c,manifest)
            for number in (0,14):
                bad=dict(manifest,max_current_sequence=number)
                with self.subTest(number=number),self.assertRaises(m.Stop):m.manifest_binding(c,bad)
            bootstrap={'release':{'version':'1.14.1','commit':f['product']['commit'],
                'release_sequence':14,'key_id':f['key_id'],'target_schema':m.SCHEMA},'assets':{
                'verifier':dict(f['verifier'],url=m.asset_url(r,'uniroute-upgrade-linux-amd64')),
                'public_keys':dict(f['public_ring'],url=m.asset_url(r,'public-keys.json')),
                'package':{'size':r['package_size'],'sha256':r['package_sha256'],
                    'url':m.asset_url(r,'unirouter-1.14.1-linux-amd64.tar.gz')}}}
            m.bootstrap_binding(c,bootstrap)
            bad=copy.deepcopy(bootstrap);bad['assets']['package']['url']=m.asset_url(r,'unirouter-1.14.0-linux-amd64.tar.gz')
            with self.assertRaises(m.Stop):m.bootstrap_binding(c,bad)
            bad=copy.deepcopy(c);bad['release']['tag']='v1.14.0-preview.14'
            with self.assertRaises(m.Stop):m.validate(bad,ready=True)

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


class FirstCustomerTests(unittest.TestCase):
    intent = 'a' * 32

    def setUp(self):
        self.before = {
            'admin': {'id':1,'org_id':1,'username':'admin','role':'admin',
                'organization_role':'platform_admin','status':'active','auth_epoch':1,'auth_provider':'local'},
            'users':1,'members':0,'rotations':1,
            'orgs':[{'id':1,'name':'Platform','status':'active','org_type':'partner','parent_id':None}],
            'wallets':[{'org_id':1,'currency':'CNY','balance_micros':0,'granted_balance_micros':0,
                'frozen_micros':0,'total_spent_micros':0,'auto_freeze':False,'low_balance_threshold_cents':0}],
            'projects':[{'id':7,'org_id':1,'slug':'default','status':'active'}],
            'bootstrap_audits':[],'release_state':[['synthetic-release']],'release_history':[['synthetic-history']]}
        self.preview = {'contract_version':'unirouter.first-customer-bootstrap-preview.v1',
            'dry_run':True,'ready':True,'checkpoint':'fresh_seed','organization_name':m.FIRST_CUSTOMER_NAME,
            'platform_org_id':1,'would_remove':{'project_id':7,'zero_wallet_org_id':1}}
        self.created = {'organization_id':9,'organization_name':m.FIRST_CUSTOMER_NAME,
            'wallet_balance_micros':0,'idempotent':False}
        self.after = copy.deepcopy(self.before)
        self.after['orgs'].append({'id':9,'name':m.FIRST_CUSTOMER_NAME,'status':'active','org_type':'customer','parent_id':None})
        self.after['wallets'][0]['org_id'] = 9
        self.after['projects'] = []
        self.after['bootstrap_audits'] = [{'org_id':1,'user_id':1,'resource':'organization',
            'resource_id':'9','client_request_id':self.intent}]

    def test_real_contract_preview_create_replay_and_restart_shapes(self):
        m.first_customer_preview(self.preview, self.before)
        self.assertEqual(m.first_customer_result(self.created,idempotent=False),9)
        m.first_customer_result(dict(self.created,idempotent=True),idempotent=True,customer_id=9)
        m.first_customer_transition(self.before,self.after,9,self.intent)
        restarted = copy.deepcopy(self.after)
        restarted['projects'] = [{'id':11,'org_id':9,'slug':'default','status':'active'}]
        m.first_customer_transition(self.before,restarted,9,self.intent,restarted=True)

    def test_preview_refuses_changed_seed_ids_and_contract(self):
        mutations = [('platform_org_id',2),('platform_org_id',True),('dry_run',False),
            ('ready',False),('checkpoint','strict_reset'),('organization_name','changed'),
            ('would_remove',{'project_id':8,'zero_wallet_org_id':1}),
            ('would_remove',{'project_id':7,'zero_wallet_org_id':True})]
        for key,value in mutations:
            with self.subTest(key=key,value=value):
                wrong = copy.deepcopy(self.preview); wrong[key] = value
                with self.assertRaises(m.Stop): m.first_customer_preview(wrong,self.before)

    def test_create_and_replay_refuse_wrong_result_fields(self):
        for key,value in [('organization_id',True),('organization_id',0),('organization_id',10),
            ('organization_name','other'),('wallet_balance_micros',1),('wallet_balance_micros',False),
            ('idempotent',True)]:
            with self.subTest(key=key,value=value):
                wrong = dict(self.created); wrong[key]=value
                with self.assertRaises(m.Stop): m.first_customer_result(wrong,idempotent=False,customer_id=9)

    def test_transition_refuses_metadata_duplicate_wallet_audit_or_project_mutations(self):
        cases = []
        for key in ('admin','users','members','rotations','release_state','release_history'):
            q=copy.deepcopy(self.after)
            q[key] = 99 if isinstance(q[key],int) else {'mutated':True}
            cases.append(q)
        for key in ('orgs','wallets','bootstrap_audits'):
            q=copy.deepcopy(self.after);q[key].append(copy.deepcopy(q[key][0]));cases.append(q)
        q=copy.deepcopy(self.after);q['wallets'][0]['balance_micros']=False;cases.append(q)
        q=copy.deepcopy(self.after);q['projects']=copy.deepcopy(self.before['projects']);cases.append(q)
        q=copy.deepcopy(self.after);q['bootstrap_audits'][0]['client_request_id']='b'*32;cases.append(q)
        for index,q in enumerate(cases):
            with self.subTest(index=index),self.assertRaises(m.Stop):
                m.first_customer_transition(self.before,q,9,self.intent)
        for org,status in [(1,'active'),(9,'disabled'),(True,'active')]:
            q=copy.deepcopy(self.after)
            q['projects']=[{'id':11,'org_id':org,'slug':'default','status':status}]
            with self.subTest(org=org,status=status),self.assertRaises(m.Stop):
                m.first_customer_transition(self.before,q,9,self.intent,restarted=True)

    def response_fixture(self, body, status=200):
        response=mock.MagicMock();response.__enter__.return_value=response
        response.status=status;response.read.side_effect=lambda cap:body[:cap]
        opener=mock.Mock();opener.open.return_value=response
        return response,opener

    def test_http_is_fixed_loopback_private_bounded_and_proxyless(self):
        response,opener=self.response_fixture(json.dumps(self.preview).encode())
        with mock.patch.object(m.urllib.request,'build_opener',return_value=opener) as build:
            result=m.first_customer_request('sentinel-no-log',self.intent,dry_run=True,expected_status=200)
        self.assertEqual(result,self.preview)
        request=opener.open.call_args.args[0]
        self.assertEqual(request.full_url,'http://127.0.0.1:13000/api/v1/bootstrap/first-customer')
        self.assertEqual(request.method,'POST')
        self.assertEqual(request.get_header('X-unirouter-client-request-id'),self.intent)
        self.assertEqual(json.loads(request.data),{'organization_name':m.FIRST_CUSTOMER_NAME,'dry_run':True})
        self.assertEqual(opener.open.call_args.kwargs,{'timeout':20})
        response.read.assert_called_once_with(m.FIRST_CUSTOMER_LIMIT+1)
        self.assertEqual(build.call_args.args[0].proxies,{})
        self.assertIsInstance(build.call_args.args[1],m.NoFirstCustomerRedirect)
        self.assertIsNone(build.call_args.args[1].redirect_request(None,None,302,'',{},'https://example.invalid'))

    def test_shared_HTTP_helpers_accept_explicit_case_ports_without_host_selection(self):
        for port in (13001,13002):
            for login in (False,True):
                value={'organization_role':'platform_admin','token':'public synthetic token'} if login else self.preview
                response,opener=self.response_fixture(json.dumps(value).encode())
                with self.subTest(port=port,login=login),mock.patch.object(m.urllib.request,'build_opener',return_value=opener) as build:
                    result=m.fixture_admin_login(m.FIRST_USE_TEST_INPUT,loopback_port=port) if login else \
                        m.first_customer_request('public synthetic token',self.intent,dry_run=True,expected_status=200,loopback_port=port)
                self.assertEqual(result,value)
                path='/auth/login' if login else '/bootstrap/first-customer'
                self.assertEqual(opener.open.call_args.args[0].full_url,'http://127.0.0.1:'+str(port)+'/api/v1'+path)
                self.assertEqual(build.call_args.args[0].proxies,{})
                self.assertIsInstance(build.call_args.args[1],m.NoFirstCustomerRedirect)
                response.read.assert_called_once_with(m.FIRST_CUSTOMER_LIMIT+1)

    def test_shared_HTTP_helpers_reject_invalid_ports_before_transport(self):
        for port in (True,False,0,1023,65536,'13001','https://example.invalid','127.0.0.1:13001',None):
            with self.subTest(port=port),mock.patch.object(m.urllib.request,'build_opener') as build:
                with self.assertRaises(m.Stop):m.fixture_admin_login(m.FIRST_USE_TEST_INPUT,loopback_port=port)
                with self.assertRaises(m.Stop):m.first_customer_request('public synthetic token',self.intent,
                    dry_run=True,expected_status=200,loopback_port=port)
                build.assert_not_called()

    def test_http_create_requires_201_and_invalid_inputs_do_not_open_transport(self):
        response,opener=self.response_fixture(json.dumps(self.created).encode(),201)
        with mock.patch.object(m.urllib.request,'build_opener',return_value=opener):
            self.assertEqual(m.first_customer_request('sentinel-no-log',self.intent,
                dry_run=False,expected_status=201),self.created)
        self.assertFalse(json.loads(opener.open.call_args.args[0].data)['dry_run'])
        for token,intent,dry in [('',self.intent,True),('x\r\ny',self.intent,True),
            ('sentinel',None,True),('sentinel','BAD',True),('sentinel',self.intent,'true')]:
            with self.subTest(intent=intent,dry=dry),mock.patch.object(m.urllib.request,'build_opener') as build:
                with self.assertRaises(m.Stop):
                    m.first_customer_request(token,intent,dry_run=dry,expected_status=200)
                build.assert_not_called()

    def test_http_refuses_status_redirect_large_or_malformed_without_body_disclosure(self):
        secret='SENTINEL-CREDENTIAL-MUST-NOT-APPEAR'
        for status,body in [(201,secret.encode()),(302,secret.encode()),(500,secret.encode()),
            (200,(secret*1000).encode()),(200,('{"password":"'+secret).encode()),
            (200,('{"password":"'+secret+'","password":"second"}').encode())]:
            response,opener=self.response_fixture(body,status)
            with self.subTest(status=status,length=len(body)), \
                 mock.patch.object(m.urllib.request,'build_opener',return_value=opener):
                with self.assertRaises(m.Stop) as error:
                    m.first_customer_request(secret,self.intent,dry_run=True,expected_status=200)
                self.assertNotIn(secret,str(error.exception))
                if status != 200: response.read.assert_not_called()
        opener=mock.Mock();opener.open.side_effect=m.urllib.error.HTTPError(
            'http://127.0.0.1:13000/',302,secret,{},None)
        with mock.patch.object(m.urllib.request,'build_opener',return_value=opener):
            with self.assertRaises(m.Stop) as error:
                m.first_customer_request(secret,self.intent,dry_run=False,expected_status=201)
            self.assertNotIn(secret,str(error.exception))


    def test_client_header_matches_current_native_authority(self):
        text=(ROOT/'internal/middleware/request_identity.go').read_text()
        native=re.search(r'ClientRequestIDHeader\s*=\s*"([^"]+)"',text).group(1)
        self.assertEqual(m.CLIENT_REQUEST_ID_HEADER,native)
        self.assertIn("headers: { '"+native+"': clientRequestId }",
                      (ROOT/'frontend/src/api/client.ts').read_text())
        response,opener=self.response_fixture(json.dumps(self.preview).encode())
        with mock.patch.object(m.urllib.request,'build_opener',return_value=opener):
            m.first_customer_request('synthetic-token',self.intent,dry_run=True,expected_status=200)
        request=opener.open.call_args.args[0]
        self.assertEqual(request.get_header(native.capitalize()),self.intent)
        self.assertIsNone(request.get_header('X-client-request-id'))

    def test_both_native_image_id_namespaces_stay_bound_to_exact_ref_and_layers(self):
        expected={'role':'backend','config_id':'sha256:'+'1'*64,
                  'diff_ids':['sha256:'+'3'*64,'sha256:'+'4'*64]}
        ref='ghcr.io/foigent/unirouter-backend@sha256:'+'2'*64
        image={'Id':expected['config_id'],'Os':'linux','Architecture':'amd64',
               'RepoDigests':[ref],'RootFS':{'Layers':list(expected['diff_ids'])}}
        for native_id in [expected['config_id'],ref.split('@')[1]]:
            actual=copy.deepcopy(image);actual['Id']=native_id
            self.assertEqual(m.verified_app_image_id(ref,expected,actual),native_id)
        mutations=[]
        for field,value in [('Id','sha256:'+'9'*64),('Os','windows'),('Architecture','arm64'),('RepoDigests',[])]:
            q=copy.deepcopy(image);q[field]=value;mutations.append((ref,expected,q))
        q=copy.deepcopy(image);q['RootFS']['Layers'].reverse();mutations.append((ref,expected,q))
        q=copy.deepcopy(image);q['RootFS']['Layers'][0]='sha256:'+'5'*64;mutations.append((ref,expected,q))
        mutations.append((ref.replace('foigent/','another-owner/'),expected,image))
        mutations.append((ref,dict(expected,role='frontend'),image))
        mutations.append((ref,dict(expected,config_id='invalid'),image))
        for index,args in enumerate(mutations):
            with self.subTest(index=index),self.assertRaises(m.Stop):
                m.verified_app_image_id(*args)

    def test_chosen_password_changes_exactly_one_epoch_and_one_audit(self):
        after=copy.deepcopy(self.before);after['admin']['auth_epoch']+=1;after['rotations']=2
        m.password_choice_delta(self.before,after)
        mutations=[]
        for field,value in [('id',2),('org_id',9),('status','disabled'),('auth_epoch',3)]:
            q=copy.deepcopy(after);q['admin'][field]=value;mutations.append(q)
        q=copy.deepcopy(after);q['rotations']=3;mutations.append(q)
        q=copy.deepcopy(after);q['release_history'].append(['unapproved']);mutations.append(q)
        q=copy.deepcopy(after);q['wallets'][0]['balance_micros']=1;mutations.append(q)
        q=copy.deepcopy(after);q['projects']=[];mutations.append(q)
        q=copy.deepcopy(after);q['extra']='unexpected';mutations.append(q)
        for index,q in enumerate(mutations):
            with self.subTest(index=index),self.assertRaises(m.Stop):
                m.password_choice_delta(self.before,q)
        bad=copy.deepcopy(self.before);bad['admin']['auth_epoch']=True
        with self.assertRaises(m.Stop):m.password_choice_delta(bad,after)

    def test_new_two_rotation_preview_does_not_relax_legacy_one(self):
        m.first_customer_preview(self.preview,self.before)
        two=copy.deepcopy(self.before);two['rotations']=2;two['admin']['auth_epoch']=2
        m.first_customer_preview(self.preview,two,expected_rotations=2)
        with self.assertRaises(m.Stop):m.first_customer_preview(self.preview,two)
        with self.assertRaises(m.Stop):m.first_customer_preview(self.preview,self.before,expected_rotations=2)
        for value in (True,0,3):
            with self.subTest(value=value),self.assertRaises(m.Stop):
                m.first_customer_preview(self.preview,two,expected_rotations=value)

    def test_chosen_login_uses_only_fixed_private_loopback_and_bounded_transport(self):
        value={'organization_role':'platform_admin','token':'SYNTHETIC-NOT-A-SECRET'}
        response,opener=self.response_fixture(json.dumps(value).encode())
        with mock.patch.object(m.urllib.request,'build_opener',return_value=opener) as build:
            self.assertEqual(m.fixture_admin_login(m.FIRST_USE_TEST_INPUT),value)
        request=opener.open.call_args.args[0]
        self.assertEqual(request.full_url,'http://127.0.0.1:13000/api/v1/auth/login')
        self.assertEqual(request.method,'POST')
        self.assertEqual(json.loads(request.data),{'username':'admin','password':m.FIRST_USE_TEST_INPUT})
        self.assertEqual(build.call_args.args[0].proxies,{})
        self.assertIsInstance(build.call_args.args[1],m.NoFirstCustomerRedirect)
        response.read.assert_called_once_with(m.FIRST_CUSTOMER_LIMIT+1)
        self.assertEqual(opener.open.call_args.kwargs,{'timeout':20})

    def test_login_old_input_refusal_and_errors_never_disclose_body_or_input(self):
        marker='SYNTHETIC-PRIVATE-ERROR-MARKER'
        opener=mock.Mock()
        opener.open.side_effect=m.urllib.error.HTTPError(
            'http://127.0.0.1:13000/api/v1/auth/login',401,marker,{},io.BytesIO(marker.encode()))
        with mock.patch.object(m.urllib.request,'build_opener',return_value=opener):
            self.assertIsNone(m.fixture_admin_login(m.FIRST_USE_TEST_INPUT,expected_status=401))
        for status,body in [(302,marker.encode()),(500,marker.encode()),
                            (200,(marker*1000).encode()),(200,b'not-json'),
                            (200,b'{"organization_role":"viewer","token":"'+marker.encode()+b'"}')]:
            response,opener=self.response_fixture(body,status)
            with self.subTest(status=status),mock.patch.object(m.urllib.request,'build_opener',return_value=opener):
                with self.assertRaises(m.Stop) as error:m.fixture_admin_login(m.FIRST_USE_TEST_INPUT)
                self.assertNotIn(marker,str(error.exception))
                self.assertNotIn(m.FIRST_USE_TEST_INPUT,str(error.exception))
        for value in ('', 'bad\ninput'):
            with mock.patch.object(m.urllib.request,'build_opener') as build:
                with self.assertRaises(m.Stop):m.fixture_admin_login(value)
                build.assert_not_called()


if __name__=='__main__':unittest.main()
