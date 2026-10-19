#!/usr/bin/env python3
"""Offline origin/diagnostic tests; optional actual retained files, never Docker/network."""
import copy
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import sys
import types
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
H = types.ModuleType('backend_reuse_under_test')
H.__file__ = str(HERE/'community-backend-reuse.py')
exec(compile(Path(H.__file__).read_bytes(), H.__file__, 'exec'), H.__dict__)
O = H.ORIGIN
C = types.ModuleType('producer_orchestration_under_test')
C.__file__ = str(HERE/'community-byte-candidate.py')
exec(compile(Path(C.__file__).read_bytes(), C.__file__, 'exec'), C.__dict__)
FIXTURE = os.environ.get('UR_BACKEND_ORIGIN_FIXTURE_DIR')


class API:
    def __init__(self, fixture=None):
        self.fixture = fixture; self.calls = []; self.downloads = []
        self.status = 200; self.timeout = False; self.exposed = False; self.tamper = None
        self.run = {'id': O['run_id'], 'workflow_id': O['workflow_id'], 'run_attempt': 1,
                    'status': 'completed', 'conclusion': 'failure', 'event': 'workflow_dispatch',
                    'head_sha': O['producer'], 'head_branch': 'main',
                    'path': '.github/workflows/community-byte-candidate.yml',
                    'repository': {'id': O['repo_id'], 'full_name': 'foigent/unirouter'},
                    'head_repository': {'id': O['repo_id']},
                    'actor': {'id': O['actor_id']}, 'triggering_actor': {'id': O['actor_id']}}
        self.release = {'id': O['draft_id'], 'draft': True, 'prerelease': True,
                        'published_at': None, 'target_commitish': O['producer'],
                        'tag_name': O['tag'], 'author': {'id': O['bot_id']}, 'assets': []}
        for name, a in O['assets'].items():
            self.release['assets'].append({'id': a['id'], 'name': name, 'state': 'uploaded',
                'size': a['bytes'], 'digest': 'sha256:'+a['sha256'], 'uploader': {'id': O['bot_id']},
                'url': H.API+H.PREFIX+'/releases/assets/'+str(a['id']),
                'browser_download_url': 'https://github.com/foigent/unirouter/releases/download/'+O['tag']+'/'+name})
        if fixture is not None:
            # Actual captured native API shapes as well as original payload bytes.
            self.run=json.loads((fixture/'final-run.json').read_text())['response']
            releases=json.loads((fixture/'candidate-release-match.json').read_text())
            assert len(releases)==1
            self.release=releases[0]

    def request(self, method, path, anonymous=False):
        assert method == 'GET', 'Any remote write is forbidden in origin preflight'
        self.calls.append((method, path, anonymous))
        if self.timeout: raise TimeoutError('synthetic transport failure')
        if self.status != 200: return self.status, None
        if path == H.PREFIX+'/actions/runs/'+str(O['run_id']):
            assert anonymous, 'Public run lookup must not require new Actions permissions'
            return 200, self.run
        if anonymous:
            return (200, {'id': O['repo_id'], 'private': False}) if path == H.PREFIX else (404, None)
        if path == H.PREFIX+'/releases/'+str(O['draft_id']): return 200, self.release
        raise AssertionError('Unexpected endpoint')

    def download(self, url, destination=None, maximum=None, authenticated=True, require_absent=False):
        self.downloads.append((url, authenticated, require_absent))
        if require_absent:
            return b'exposed' if self.exposed else None
        assert self.fixture is not None, 'An actual retained fixture is required for full byte validation'
        a = next(x for x in self.release['assets'] if x['url'] == url)
        body = (self.fixture/'assets'/a['name']).read_bytes()
        if self.tamper == a['name']: body = bytes([body[0]^1])+body[1:]
        H.write_new(destination, body)
        # Deliberately claim the original receipt even when bytes were tampered.
        return {k: O['assets'][a['name']][k] for k in ('bytes', 'sha256')}


class OriginIdentityTests(unittest.TestCase):
    def setUp(self):
        # Synthetic API-shape negatives use a deterministic native URL. Actual
        # retained tests use the captured API URLs and unchanged production pins.
        assets=copy.deepcopy(O['assets'])
        for name,a in assets.items():
            url='https://github.com/foigent/unirouter/releases/download/'+O['tag']+'/'+name
            a['browser_url_sha256']=H.sha(url.encode())
        binding=patch.dict(H.ORIGIN,{'assets':assets});binding.start();self.addCleanup(binding.stop)

    def test_main_origin_refusal_cannot_create_new_draft_or_upload(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp); event=root/'event.json'; H.write_new(event,b'{}')
            env={'RUNNER_TEMP':tmp,'GITHUB_RUN_ID':'77','GITHUB_EVENT_PATH':str(event)}
            calls=[]
            def request(method,path,**kwargs):
                calls.append((method,path))
                assert method=='GET', 'New draft write forbidden after origin refusal'
                return (200,{'id':C.REPO_ID,'full_name':C.REPO,'private':False,'default_branch':'main'}) if path==C.PREFIX else (200,{'object':{'type':'commit','sha':'a'*40}})
            api=types.SimpleNamespace(request=request)
            guard=types.SimpleNamespace(work=None,owned=[],receipt={'commands':[],'cleanup':[]},
                                        cleanup=lambda:None,time_guard=lambda:None)
            def initialize(g,work,env):g.work=work
            with patch.dict(os.environ,env,clear=True),patch.object(sys,'argv',[C.__file__]), \
                 patch.object(C,'runtime_identity',return_value='a'*40),patch.object(C,'bound_inputs',return_value=('ring',{})), \
                 patch.object(C,'frontend_input_preflight',return_value=None), \
                 patch.object(C,'module',side_effect=lambda name,path:guard if name=='accepted_byte_guards' else None), \
                 patch.object(C,'initialize_guard',side_effect=initialize),patch.object(C,'GitHub',return_value=api), \
                 patch.object(C,'prepare_source_inputs',return_value=None), \
                 patch.object(C,'prepare_backend_origin',side_effect=C.Stop('origin_wrong_source')):
                self.assertEqual(C.main(),1)
            terminal=C.load(C.regular(C.terminal_path(env),65536))
            self.assertEqual(terminal['error'],'origin_wrong_source');self.assertIsNone(terminal['draft_id'])
            self.assertTrue(all(method=='GET' for method,_ in calls))

    def test_all_three_assets_and_anonymous_checks_before_any_import(self):
        api = API(); H.fresh(api)
        self.assertEqual(len(api.downloads), 6)
        self.assertTrue(all(not authenticated and absent for _,authenticated,absent in api.downloads))
        self.assertTrue(all(method == 'GET' for method,_,_ in api.calls))

    def test_failed_partial_run_identity_must_not_be_replaced_with_success(self):
        for key, value in [('conclusion','success'), ('run_attempt',2), ('head_sha','a'*40),
                           ('workflow_id',1), ('event','push'), ('head_branch','other')]:
            with self.subTest(key=key):
                api=API(); api.run[key]=value
                with self.assertRaises(H.Stop): H.fresh(api)
                self.assertFalse(api.downloads)
        api=API(); api.run['actor']['id']=1
        with self.assertRaises(H.Stop): H.fresh(api)

    def test_origin_draft_must_remain_original_and_unpublished(self):
        for key, value in [('draft',False), ('prerelease',False), ('published_at','now'),
                           ('target_commitish','a'*40), ('tag_name','other')]:
            with self.subTest(key=key):
                api=API(); api.release[key]=value
                with self.assertRaises(H.Stop): H.fresh(api)

    def test_every_asset_identity_and_exact_set_required(self):
        for key,value in [('size',1), ('digest','sha256:'+'0'*64), ('state','new'),
                          ('url','https://unrelated.invalid/asset')]:
            with self.subTest(key=key):
                api=API(); api.release['assets'][1][key]=value
                with self.assertRaises(H.Stop): H.fresh(api)
        api=API(); api.release['assets'].append(copy.deepcopy(api.release['assets'][0]))
        with self.assertRaises(H.Stop): H.fresh(api)

    def test_unknown_auth_network_and_audience_results_stop(self):
        for status in [401,403,404,429,500]:
            api=API(); api.status=status
            with self.subTest(status=status), self.assertRaises(H.Stop): H.fresh(api)
            self.assertFalse(api.downloads)
        api=API(); api.timeout=True
        with self.assertRaises(TimeoutError): H.fresh(api)
        api=API(); api.exposed=True
        with self.assertRaisesRegex(H.Stop,'origin_asset_audience'): H.fresh(api)

    def test_inventory_content_comparison_does_not_depend_on_tar_order(self):
        origin={'image_id':'id','regular_bytes':3,'apk':[],'dpkg_unparsed':False,
                'entries':[{'path':'a','type':'regular','sha256':'a'}, {'path':'b','type':'regular','sha256':'b'}],
                'notice_filename_candidates':[]}
        actual=copy.deepcopy(origin); actual['entries'].reverse(); H.compare_inventory(actual,origin)
        actual['entries'][0]['sha256']='tamper'
        with self.assertRaises(H.Stop): H.compare_inventory(actual,origin)


class DiagnosticTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory(prefix='ur-safe-build-diagnostic-')
        self.addCleanup(self.tmp.cleanup); self.root=Path(self.tmp.name); (self.root/'data').mkdir()

    def guard(self, body):
        H.write_new(self.root/'1.command.log',body)
        row={'phase':'one retained frontend build','status':'failed','exit_code':1,
             'log_receiver_complete':True,'log_bytes':len(body),'log_sha256':H.sha(body)}
        return types.SimpleNamespace(work=self.root,child_env={'PATH':'/usr/bin'},receipt={'commands':[row]})

    def test_only_finite_markers_retained_no_raw_credentials_urls_paths_or_commands(self):
        body=(b'npm error SECRET_VALUE ghp_NOT_A_REAL_TOKEN sk-FAKE\n'
              b'https://user:password@example.invalid/path?sig=FAKE_SIGNED_SECRET\n'
              b'/private/host-secret\n::error::injection\n'
              b'original source input identity mismatch\nerror TS2307: private/path\n')
        value=H.capture_build_diagnostic(self.guard(body),self.root/'data')
        self.assertEqual(value['capture'],'finite_markers_only')
        self.assertEqual(value['markers'],['npm_error','observer_original_identity','typescript_diagnostic'])
        raw=H.encoded(value)
        for secret in [b'SECRET_VALUE',b'ghp_',b'sk-',b'https:',b'password',b'private/',b'::error::',b'TS2307']:
            self.assertNotIn(secret,raw)
        self.assertFalse(value['raw_log_uploaded']); self.assertTrue(value['original_failure_preserved'])

    def test_no_recognized_markers_still_preserves_original_and_capture_outcome(self):
        value=H.capture_build_diagnostic(self.guard(b'unknown output with confidential detail'),self.root/'data')
        self.assertEqual(value['markers'],[]); self.assertEqual(value['capture'],'finite_markers_only')

    def test_unsafe_environment_receiver_or_log_identity_never_project_output(self):
        guard=self.guard(b'original source input identity mismatch')
        for kind in ['env','api_key','receiver','limit','receiver_error','digest']:
            with self.subTest(kind=kind):
                current=copy.deepcopy(guard)
                if kind=='env': current.child_env['GITHUB_TOKEN']='SYNTHETIC'
                elif kind=='api_key': current.child_env['API_KEY']='SYNTHETIC'
                elif kind=='receiver': current.receipt['commands'][0]['log_receiver_complete']=False
                elif kind=='limit':current.receipt['commands'][0]['log_limit_exceeded']=True
                elif kind=='receiver_error':current.receipt['commands'][0]['log_receiver_error_class']='Sensitive text must not escape'
                else: current.receipt['commands'][0]['log_sha256']='0'*64
                value=H.capture_build_diagnostic(current,self.root/'data')
                self.assertNotEqual(value['capture'],'finite_markers_only'); self.assertEqual(value['markers'],[])

    def test_capture_write_failure_never_replaces_original(self):
        guard=self.guard(b'npm error')
        with patch.object(H,'write_new',side_effect=PermissionError('must not escape')):
            value=H.capture_build_diagnostic(guard,self.root/'data')
        self.assertEqual(value['persistence'],'unavailable'); self.assertTrue(value['original_failure_preserved'])

    def test_oversized_or_missing_log_is_bounded_and_unavailable(self):
        guard=self.guard(b'x'*(2*H.MIB+1))
        value=H.capture_build_diagnostic(guard,self.root/'data')
        self.assertEqual(value['capture'],'capture_unavailable'); self.assertEqual(value['markers'],[])
        (self.root/'1.command.log').unlink()
        value=H.capture_build_diagnostic(guard,self.root/'data')
        self.assertEqual(value['capture'],'capture_unavailable'); self.assertEqual(value['markers'],[])


@unittest.skipUnless(FIXTURE, 'Actual retained private fixture opt-in; runtime validates real bytes before draft')
class ActualRetainedOriginTests(unittest.TestCase):
    def setUp(self):
        self.fixture=Path(FIXTURE); self.tmp=tempfile.TemporaryDirectory(prefix='ur-retained-origin-')
        self.addCleanup(self.tmp.cleanup); self.root=Path(self.tmp.name)
        self.assets=self.root/'assets'; self.assets.mkdir(); self.data=self.root/'data'; self.data.mkdir()
        self.files=H.metadata_files((self.fixture/'assets/candidate-evidence.tar.gz').read_bytes())
        self.source=json.loads(self.files['source-and-fixed-inputs.json'])
        self.calls=[]
        def docker(label,*args):
            self.calls.append((label,args)); assert args[0]=='load', 'No build/save/start/run permitted'
        def inspect(role,ref):
            self.calls.append(('inspect',(role,ref)))
            self.guard.receipt['images'][role]={'id':ref,'platform':{'os':'linux','arch':'amd64'},
                                              'rootfs_diff_ids':O['diff_ids']}
        self.guard=types.SimpleNamespace(work=self.root,FIXED=self.source['fixed_inputs'],time_guard=lambda:None,
            docker=docker,inspect_image=inspect,receipt={'images':{},'commands':[]})
        self.api=API(self.fixture)

    def prepare(self):
        return H.prepare(self.api,self.guard,self.assets,self.data,self.source['source_export'],self.files['backend.Dockerfile'])

    def test_real_all_three_original_assets_full_hashes_native_gzip_metadata_and_no_rebuild(self):
        value=self.prepare()
        self.assertEqual((self.assets/'backend-image.tar.gz').read_bytes(),(self.fixture/'assets/backend-image.tar.gz').read_bytes())
        self.assertEqual(value['record'],O)
        self.assertEqual([x[1][0] for x in self.calls if x[0]!='inspect'],['load'])
        self.assertEqual(len(value['files']),17)

    def test_real_backend_tamper_stops_before_any_Docker_call(self):
        self.api.tamper='backend-image.tar.gz'
        with self.assertRaisesRegex(H.Stop,'origin_download_bytes'): self.prepare()
        self.assertFalse(self.calls)

    def test_real_metadata_tamper_stops_before_backend_download_or_Docker(self):
        self.api.tamper='candidate-evidence.tar.gz'
        with self.assertRaisesRegex(H.Stop,'origin_download_bytes'): self.prepare()
        self.assertFalse(self.calls)
        self.assertFalse((self.assets/'backend-image.tar.gz').exists())

    def test_real_source_recipe_trust_partial_state_negatives(self):
        for kind in ['source','recipe','trust','success']:
            with self.subTest(kind=kind):
                files=copy.deepcopy(self.files)
                if kind=='source':
                    source=json.loads(files['source-and-fixed-inputs.json']); source['producer_commit']='a'*40
                    files['source-and-fixed-inputs.json']=H.encoded(source)
                elif kind=='recipe': files['backend.Dockerfile']+=b'\n'
                elif kind=='trust': files['public-keys.json']=b'{}'
                else:
                    partial=json.loads(files['partial-terminal.json']); partial['status']='success'
                    files['partial-terminal.json']=H.encoded(partial)
                with self.assertRaises(H.Stop):
                    H.validate_metadata(files,self.guard.FIXED,self.source['source_export'],self.files['backend.Dockerfile'])

    def test_wrong_loaded_config_layers_or_platform_never_accept(self):
        for field in ['config','layers','platform']:
            with self.subTest(field=field):
                root=self.root/field;root.mkdir();self.guard.work=root
                self.assets=root/'assets';self.assets.mkdir();self.data=root/'data';self.data.mkdir()
                def bad(role,ref):
                    image={'id':ref,'platform':{'os':'linux','arch':'amd64'},'rootfs_diff_ids':list(O['diff_ids'])}
                    if field=='config':image['id']='sha256:'+'0'*64
                    elif field=='layers':image['rootfs_diff_ids'][-1]='sha256:'+'0'*64
                    else:image['platform']['arch']='arm64'
                    self.guard.receipt['images'][role]=image
                self.guard.inspect_image=bad
                with self.assertRaisesRegex(H.Stop,'origin_loaded_image'):self.prepare()

    def test_real_backend_archive_survives_build_orchestration_without_build_save_or_recompression(self):
        reuse=self.prepare();source=self.root/'source';source.mkdir();(source/'frontend').mkdir()
        for name in ['LICENSE','NOTICE']:
            H.write_new(source/name,self.files['product-'+name+'.txt'])
        back=self.root/'backend.Dockerfile';front=self.root/'frontend.Dockerfile'
        H.write_new(back,self.files['backend.Dockerfile']);H.write_new(front,self.files['frontend.Dockerfile'])
        self.guard.run_id='77';self.guard.receipt['source_export']=self.source['source_export']
        def docker(label,*args):
            self.calls.append((label,args))
            self.assertEqual(args[0],'build');self.assertEqual(label,'one retained frontend build')
            H.write_new(Path(args[args.index('--iidfile')+1]),('sha256:'+'1'*64+'\n').encode())
        def inspect(role,ref):
            layers=O['diff_ids'] if role=='backend' else self.guard.FIXED['images']['frontend-runtime']['rootfs_diff_ids']
            self.guard.receipt['images'][role]={'id':ref,'platform':{'os':'linux','arch':'amd64'},'rootfs_diff_ids':layers}
        def tool(label,*args):
            if label=='byte-go-version':return 'go version go1.25.12 linux/amd64'
            if label=='byte-node-version':return 'v24.19.0\n11.17.0'
            self.assertEqual(label,'actual-backend-buildinfo');return self.files['backend-buildinfo.txt'].decode()
        pulls=[]
        def pull(role):pulls.append(role);return role
        self.guard.docker=docker;self.guard.inspect_image=inspect;self.guard.pull=pull;self.guard.tool=tool
        binary=self.root/'binary-placeholder';H.write_new(binary,b'unit-orchestration-placeholder')
        reuse.update(cid='loaded-backend',binary=binary,inventory=json.loads(self.files['backend-rootfs.json']))
        def inventory(guard,helper,role,image,data):
            self.assertEqual(role,'frontend','Backend stopped inventory already performed before draft')
            return 'frontend',None,{'entries':[],'apk':[]}
        def copy_exact(guard,cid,path,target,cap):
            H.write_new(target,(C.INPUTS/'component-notices.tar.gz').read_bytes())
        def save(guard,role,image,assets,remaining):
            self.assertEqual(role,'frontend','Backend must never be saved or recompressed')
            H.write_new(assets/'frontend-image.tar.gz',b'synthetic-front-orchestration-only')
            return {'image_id':image,'compressed':C.file_fact(assets/'frontend-image.tar.gz')}
        def verifier(guard,helper,source,go,assets,data):
            H.write_new(assets/'uniroute-upgrade-linux-amd64',b'synthetic-verifier-orchestration-only')
        _,index=C.bound_inputs()
        import base64
        ring=base64.urlsafe_b64encode(self.files['public-keys.json']).decode().rstrip('=')
        retained=[]
        with patch.dict(os.environ,{'EXPECTED_WORKFLOW_COMMIT':'a'*40,'ADMISSION_SHA256':'b'*64},clear=True), \
             patch.object(C,'inventory',side_effect=inventory), \
             patch.object(C,'collect_frontend_membership',return_value={'packages':[],'output_files':[]}), \
             patch.object(C,'saved_image',side_effect=save),patch.object(C,'verifier',side_effect=verifier):
            C.build(self.guard,types.SimpleNamespace(copy_exact=copy_exact),ring,index,self.assets,self.data,
                    lambda path:retained.append(path.name),(source,back,front,{}),reuse)
        self.assertEqual(retained,['backend-image.tar.gz','frontend-image.tar.gz','uniroute-upgrade-linux-amd64'])
        self.assertEqual(H.sha((self.assets/'backend-image.tar.gz').read_bytes()),O['assets']['backend-image.tar.gz']['sha256'])
        self.assertEqual([label for label,args in self.calls if args and args[0]=='build'],['one retained frontend build'])
        self.assertEqual(pulls,['backend-builder','frontend-builder','frontend-runtime'])


if __name__=='__main__':
    unittest.main(verbosity=2)
