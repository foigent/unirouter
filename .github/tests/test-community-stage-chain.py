#!/usr/bin/env python3
"""Actual finite stage handoffs and consumer entry, exclusively offline fixtures."""
import base64
import contextlib
import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import tarfile
import types
import unittest
from unittest.mock import patch
HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[1]
def module(name,file):
    spec=importlib.util.spec_from_file_location(name,HERE/file);value=importlib.util.module_from_spec(spec);spec.loader.exec_module(value);return value
M=module('chain_contract','community-stage-record.py')
P=module('chain_publisher_tests','test-private-image-publisher.py')
C=module('chain_consumer','community-release-acceptance.py')
F=module('chain_fixtures','community-stage-fixtures.py')
B=module('chain_byte_producer','community-byte-candidate.py')

class StageChainTests(unittest.TestCase):
    def setup_chain(self,path,version='1.15.0',commit='a'*40, *, schema='000128_upstream_release_observation'):
        world=P.World(path);world.source=F.producer(version,commit,schema=schema)
        world.tag='source-'+commit+'-run-'+str(world.parent_run)+'-linux-amd64';world.prepare_chain()
        terminal=world.execute();publisher=M.canonical(world.record);result=M.canonical(terminal['stage_result'])
        bindings=M.publication_bindings(publisher,result)
        provenance=M.canonical({'schema_version':'synthetic-build-provenance','community_stage_chain':bindings})
        config=C.load_config(ROOT);fixed=config['immutable'];release=config['release']
        fixed['product']={key:world.source['product'][key] for key in ('commit','tree','version','schema')}
        fixed.update(min_current_sequence=0,max_current_sequence=16,min_current_schema='000127_xinsight_communication_policy',historical_sequence_reserved_through=16)
        fixed['bootstrap_script']={'url':'https://raw.githubusercontent.com/foigent/unirouter/'+commit+'/deploy/community/bootstrap.py',
            'size':(ROOT/'deploy/community/bootstrap.py').stat().st_size,'sha256':C.sha(ROOT/'deploy/community/bootstrap.py')}
        fixed['native_acceptance_helper_sha256']=C.sha(HERE/'community-installer.py')
        fixed['provenance_sha256']=M.sha(provenance)
        for row in fixed['artifacts']:
            if row['path']=='payload/provenance/build-provenance.json':row.update(size=len(provenance),sha256=M.sha(provenance))
        for relative in ('deploy/customer/upgrade.py','deploy/customer/recovery.py'):
            body=(ROOT/relative).read_bytes()
            fixed['artifacts'].append({'path':'payload/deploy/'+Path(relative).name,'size':len(body),'sha256':M.sha(body)})
        fixed['artifacts'].sort(key=lambda row:row['path'])
        release.update(release_sequence=17,tag='v'+version+'-preview.17',created_at='2000-01-01T00:00:00Z',
            bootstrap_size=100,bootstrap_sha256='e'*64,package_size=1000,package_sha256='f'*64)
        release['bootstrap_url']=C.asset_url(release,'community-bootstrap.json')
        consumer=M.next_consumer(publisher,result,config)
        return world,publisher,result,bindings,provenance,consumer

    def native_env(self,path,record,stage):
        body=M.canonical(record)
        inputs={'stage_record_b64':base64.urlsafe_b64encode(body).decode().rstrip('='),'stage_record_sha256':M.sha(body),
            'expected_workflow_commit':'a'*40,'admission_sha256':'d'*64}
        event=path/'consumer-event.json';event.write_bytes(M.canonical({'repository':{'id':M.REPOSITORY_ID,'full_name':M.REPOSITORY},'sender':{'id':M.ACTOR_ID},'inputs':inputs}))
        return {'GITHUB_ACTIONS':'true','GITHUB_RUN_ID':'67890','RUNNER_TEMP':str(path),'GITHUB_WORKSPACE':str(ROOT),
            'GITHUB_REPOSITORY':M.REPOSITORY,'GITHUB_REPOSITORY_ID':str(M.REPOSITORY_ID),
            'GITHUB_REF':'refs/heads/main','GITHUB_EVENT_NAME':'workflow_dispatch','GITHUB_SHA':'a'*40,
            'GITHUB_WORKFLOW_SHA':'a'*40,'GITHUB_RUN_ATTEMPT':'1','GITHUB_ACTOR_ID':str(M.ACTOR_ID),
            'GITHUB_WORKFLOW_REF':M.REPOSITORY+'/.github/workflows/'+M.WORKFLOWS[stage]+'@refs/heads/main',
            'RUNNER_ENVIRONMENT':'github-hosted','RUNNER_OS':'Linux','RUNNER_ARCH':'X64','GITHUB_EVENT_PATH':str(event),
            'STAGE_RECORD_B64':inputs['stage_record_b64'],'STAGE_RECORD_SHA256':inputs['stage_record_sha256'],
            'EXPECTED_WORKFLOW_COMMIT':inputs['expected_workflow_commit'],'ADMISSION_SHA256':inputs['admission_sha256']}

    def test_two_versions_connect_actual_publisher_and_current_consumer_owners(self):
        for version,commit,schema in (('1.15.0','a'*40,'000128_upstream_release_observation'),
                ('1.15.1','b'*40,'000128_upstream_release_observation'),
                ('1.15.3','e'*40,'000129_upstream_observation_restore_namespace')):
            with self.subTest(version=version,schema=schema),tempfile.TemporaryDirectory() as tmp:
                world,publisher,result,bindings,provenance,record=self.setup_chain(Path(tmp),version,commit,schema=schema)
                actual_producer_stage=B.module('current_producer_stage',HERE/'community-stage-record.py')
                actual_producer_stage.validate_record(world.source,'producer')
                self.assertEqual(schema,json.loads(result)['product']['schema'])
                config=record['inputs']['config'];self.assertEqual([],C.validate(config,ready=True,stage_record=record))
                C.reference_sources(ROOT,config)
                M.verify_publication_bindings(bindings,config,record)
                fixed,release=config['immutable'],config['release']
                manifest={'schema_version':'unirouter.upgrade-manifest.v2','product':'unirouter',
                    'key_id':fixed['key_id'],'signature_algorithm':'ed25519','release_sequence':17,'version':version,
                    'commit':commit,'created_at':release['created_at'],'expires_at':'never','target_os':'linux','target_arch':'amd64',
                    'min_current_sequence':0,'max_current_sequence':16,'min_current_schema':'000127_xinsight_communication_policy',
                    'target_schema':record['product']['schema'],'provenance_sha256':fixed['provenance_sha256'],'artifacts':fixed['artifacts']}
                C.manifest_binding(config,manifest)
                env=self.native_env(Path(tmp),record,'consumer')
                with patch.dict(os.environ,env,clear=True),patch.object(C.subprocess,'run',side_effect=AssertionError('process forbidden')), \
                     patch.object(C.tempfile,'mkdtemp',side_effect=AssertionError('destination forbidden')), \
                     patch.object(C.urllib.request,'urlopen',side_effect=AssertionError('network forbidden')),contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(0,C.main(['validate','--stage-dispatch']))
                    with patch.object(C,'native_guard',side_effect=C.Stop('native_context_boundary')) as boundary:
                        self.assertEqual(1,C.main(['execute','--stage-dispatch']))
                        self.assertEqual(1,boundary.call_count)
                self.assertEqual(world.parent_run,config['immutable']['accepted_assembly_run'])
                self.assertEqual(67890,config['immutable']['actual_private_publisher_run'])
                self.assertLessEqual(len(M.canonical(record)),M.MAX_RAW)

    def test_m129_signed_handoff_rejects_internally_consistent_m128_consumer(self):
        with tempfile.TemporaryDirectory() as tmp:
            _,_,_,bindings,_,record=self.setup_chain(Path(tmp),'1.15.3','e'*40,
                schema='000129_upstream_observation_restore_namespace')
            changed=copy.deepcopy(record)
            changed['product']['schema']='000128_upstream_release_observation'
            changed['inputs']['config']['immutable']['product']['schema']=changed['product']['schema']
            M.validate_record(changed,'consumer')
            with self.assertRaisesRegex(M.Stop,'stage_consumer_full_predecessor'):
                M.verify_publication_bindings(bindings,changed['inputs']['config'],changed)

    def test_three_actual_loaders_reject_changed_stage_bytes_before_import(self):
        raw=(HERE/'community-stage-record.py').read_bytes()
        for stage,owner in (('producer',B),('publisher',P.H),('consumer',C)):
            with self.subTest(stage=stage),tempfile.TemporaryDirectory() as tmp:
                root=Path(tmp);path=root/'community-stage-record.py';path.write_bytes(raw)
                loader=(lambda:B.module('exact_stage_loader',path)) if stage=='producer' else owner.stage_module
                with patch.object(owner,'__file__',str(root/Path(owner.__file__).name)):
                    loaded=loader()
                    loaded.validate_record(F.producer('1.15.3','e'*40,
                        schema='000129_upstream_observation_restore_namespace'),'producer')
                    path.write_bytes(raw+b'\nraise AssertionError("tampered program must never import")\n')
                    with self.assertRaisesRegex(Exception,'observer_identity|Stage program identity changed'):
                        loader()

    def test_stage_hash_and_schema_refusals_precede_effects_at_all_three_actual_entries(self):
        class PublicEnvironment(dict):
            def get(self,key,*default):
                if key in ('GH_TOKEN','GITHUB_TOKEN'):raise AssertionError('credential read before stage validation')
                return super().get(key,*default)
            def __getitem__(self,key):
                if key in ('GH_TOKEN','GITHUB_TOKEN'):raise AssertionError('credential copied before stage validation')
                return super().__getitem__(key)
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp)
            world,publisher,_,_,_,consumer=self.setup_chain(root,'1.15.3','e'*40,
                schema='000129_upstream_observation_restore_namespace')
            original_stage=(HERE/'community-stage-record.py').read_bytes()
            entries=(('producer',B,world.source),('publisher',P.H,json.loads(publisher)),('consumer',C,consumer))
            cases=[(kind,*entry) for kind in ('hash','unknown-schema') for entry in entries]
            cases.append(('missing-m129','producer',B,world.source))
            for index,(kind,stage,owner,original_record) in enumerate(cases):
                with self.subTest(stage=stage,kind=kind):
                    record=copy.deepcopy(original_record)
                    if kind=='unknown-schema':record['product']['schema']='000130_unknown'
                    elif kind=='missing-m129':del record['inputs']['source_inventory']['files']['migrations/000129_upstream_observation_restore_namespace.up.sql']
                    (root/'community-stage-record.py').write_bytes(original_stage+
                        (b'\nraise AssertionError("tampered program must never import")\n' if kind=='hash' else b''))
                    env=PublicEnvironment(self.native_env(root,record,stage))
                    env['GITHUB_RUN_ID']=str(67890+index)
                    loader='module' if stage=='producer' else 'stage_module'
                    with patch.object(owner,'__file__',str(root/Path(owner.__file__).name)), \
                         patch.object(owner,loader,wraps=getattr(owner,loader)) as actual_loader, \
                         patch.object(os,'environ',env),patch.object(B.sys,'argv',[B.__file__]), \
                         patch.object(subprocess,'run',side_effect=AssertionError('external process before validation')) as process, \
                         patch.object(tempfile,'mkdtemp',side_effect=AssertionError('destination before validation')) as destination, \
                         patch.object(B,'GitHub',side_effect=AssertionError('external client before validation')) as client, \
                         patch.object(B,'bound_inputs',side_effect=AssertionError('fixed inputs before stage validation')) as inputs, \
                         patch.object(B,'initialize_guard',side_effect=AssertionError('tool owner before validation')) as tools, \
                         patch.object(P.H,'gh',side_effect=AssertionError('external API before validation')) as api, \
                         patch.object(P.H,'command',side_effect=AssertionError('Docker before validation')) as docker, \
                         patch.object(C,'native_guard',side_effect=AssertionError('native destination before validation')) as native, \
                         patch.object(C.urllib.request,'urlopen',side_effect=AssertionError('network before validation')) as network, \
                         contextlib.redirect_stdout(io.StringIO()),contextlib.redirect_stderr(io.StringIO()) as errors:
                        if stage=='producer':self.assertEqual(1,B.main())
                        elif stage=='publisher':
                            with self.assertRaisesRegex(Exception,'Stage program identity changed' if kind=='hash' else 'stage_product_scope'):P.H.execute()
                        else:
                            self.assertEqual(1,C.main(['execute','--stage-dispatch']))
                            # The consumer redacts a foreign shared-parser
                            # exception to its class, preserving that boundary.
                            self.assertEqual('Formal consumer stopped: '+
                                ('Stage program identity changed.' if kind=='hash' else 'Stop')+'\n',errors.getvalue())
                        self.assertEqual(1,actual_loader.call_count)
                        for effect in (process,destination,client,inputs,tools,api,docker,native,network):effect.assert_not_called()

    def test_signed_provenance_rejects_cross_source_parent_assets_layers_or_registry(self):
        with tempfile.TemporaryDirectory() as tmp:
            _,publisher,result,bindings,_,record=self.setup_chain(Path(tmp))
            config=record['inputs']['config']
            for kind in ('source','parent','program','layer','asset','registry','trust','extra'):
                with self.subTest(kind=kind):
                    current=copy.deepcopy(config);proof=copy.deepcopy(bindings);child=copy.deepcopy(record)
                    if kind=='source':current['immutable']['product']['commit']='f'*40
                    elif kind=='parent':current['immutable']['stage_chain']['publisher_result_sha256']='e'*64
                    elif kind=='program':child['predecessor']['program_commit']='f'*40
                    elif kind=='layer':current['immutable']['app_image_identities'][0]['diff_ids']=['sha256:'+'f'*64]
                    elif kind=='asset':current['immutable']['app_image_identities'][0]['asset_id']=999
                    elif kind=='registry':current['immutable']['images']['frontend']='ghcr.io/foigent/unirouter-frontend@sha256:'+'f'*64
                    elif kind=='trust':proof['publisher_result_b64']=base64.urlsafe_b64encode(b'{}').decode().rstrip('=')
                    else:proof['approval']=True
                    with self.assertRaises(M.Stop):M.verify_publication_bindings(proof,current,child)

    def test_actual_prepare_owner_checks_full_parent_before_first_anonymous_pull(self):
        class PullBoundary(Exception):pass
        for changed in (False,True):
            with self.subTest(changed=changed),tempfile.TemporaryDirectory() as tmp:
                root=Path(tmp);_,_,_,_,provenance,record=self.setup_chain(root)
                if changed:record['predecessor']['program_commit']='f'*40
                config=record['inputs']['config'];f=config['immutable'];r=config['release']
                C.validate(config,ready=True,stage_record=record)
                anchor={'release':{'version':f['product']['version'],'commit':f['product']['commit'],
                    'release_sequence':r['release_sequence'],'key_id':f['key_id'],'target_schema':f['product']['schema']},
                    'assets':{'verifier':dict(f['verifier'],url=C.asset_url(r,'uniroute-upgrade-linux-amd64')),
                    'public_keys':dict(f['public_ring'],url=C.asset_url(r,'public-keys.json')),
                    'package':{'url':C.asset_url(r,'unirouter-'+f['product']['version']+'-linux-amd64.tar.gz'),
                        'size':r['package_size'],'sha256':r['package_sha256']}}}
                bootstrap=types.SimpleNamespace(download=lambda identity,path:path.write_bytes(b'synthetic-download'),
                    Bootstrap=lambda raw,digest:types.SimpleNamespace(value=anchor))
                original_loader=C.load_module
                def modules(path,digest,name):
                    return bootstrap if name=='accepted_product_bootstrap' else original_loader(path,digest,name)
                owner=root/'consumer-owner';owner.mkdir();calls=[]
                with patch.object(C,'load_module',side_effect=modules),patch.object(C.shutil,'which',return_value='/usr/bin/false'):
                    fixture=C.make_acceptance(ROOT,owner,config,{},stage_record=record)
                def run(label,args):
                    calls.append(label)
                    if label.startswith('anonymous pull'):raise PullBoundary()
                    self.assertEqual('prepare real owner-signed public release',label)
                    fixture.release.mkdir()
                    manifest={'schema_version':'unirouter.upgrade-manifest.v2','product':'unirouter',
                        'key_id':f['key_id'],'signature_algorithm':'ed25519','release_sequence':r['release_sequence'],
                        'version':f['product']['version'],'commit':f['product']['commit'],'created_at':r['created_at'],
                        'expires_at':'never','target_os':'linux','target_arch':'amd64','min_current_sequence':0,
                        'max_current_sequence':16,'min_current_schema':f['min_current_schema'],'target_schema':f['product']['schema'],
                        'provenance_sha256':f['provenance_sha256'],'artifacts':f['artifacts']}
                    with tarfile.open(fixture.release/'release.tar.gz','w:gz') as archive:
                        raw=M.canonical(manifest);info=tarfile.TarInfo('manifest.json');info.size=len(raw);archive.addfile(info,io.BytesIO(raw))
                    path=fixture.release/'payload/provenance/build-provenance.json';path.parent.mkdir(parents=True);path.write_bytes(provenance)
                    (fixture.release/'payload/install.json').write_bytes(M.canonical({'schema_version':'unirouter.install.v1','images':f['images']}))
                    return ''
                fixture.run=run
                with patch.object(C.subprocess,'run',side_effect=AssertionError('real tool forbidden')), \
                     patch.object(C.urllib.request,'urlopen',side_effect=AssertionError('real network forbidden')):
                    if changed:
                        with self.assertRaisesRegex(Exception,'stage_consumer_full_predecessor'):fixture.prepare()
                    else:
                        with self.assertRaises(PullBoundary):fixture.prepare()
                self.assertEqual(0 if changed else 1,sum(label.startswith('anonymous pull') for label in calls))

    def test_invalid_current_inputs_refuse_at_actual_entry_before_effects(self):
        for schema in ('000128_upstream_release_observation','000129_upstream_observation_restore_namespace'):
            with self.subTest(schema=schema),tempfile.TemporaryDirectory() as tmp:
                _,_,_,_,_,original=self.setup_chain(Path(tmp),schema=schema)
                for kind in ('ring','source','payload_duplicate','schema','schema_binding','parent','app_layer','unknown','sequence','missing_upgrade'):
                    with self.subTest(kind=kind):
                        record=copy.deepcopy(original);fixed=record['inputs']['config']['immutable']
                        if kind=='ring':fixed['public_ring']['sha256']='0'*64
                        elif kind=='source':fixed['product']['commit']='f'*40
                        elif kind=='payload_duplicate':fixed['artifacts'].append(copy.deepcopy(fixed['artifacts'][0]))
                        elif kind=='schema':fixed['min_current_schema']='unknown'
                        elif kind=='schema_binding':record['product']['schema']='000128_upstream_release_observation' if schema.endswith('namespace') else '000129_upstream_observation_restore_namespace'
                        elif kind=='parent':record['predecessor']['run_id']+=1
                        elif kind=='app_layer':fixed['app_image_identities'][0]['diff_ids']=['invalid']
                        elif kind=='unknown':fixed['approval']=True
                        elif kind=='missing_upgrade':fixed['artifacts']=[row for row in fixed['artifacts'] if row['path']!='payload/deploy/upgrade.py']
                        else:fixed['max_current_sequence']=True
                        env=self.native_env(Path(tmp),record,'consumer')
                        with patch.dict(os.environ,env,clear=True),patch.object(C.subprocess,'run') as process, \
                             patch.object(C.tempfile,'mkdtemp') as destination,patch.object(C,'native_guard') as context, \
                             patch.object(C.urllib.request,'urlopen') as network,contextlib.redirect_stderr(io.StringIO()):
                            self.assertEqual(1,C.main(['execute','--stage-dispatch']))
                            process.assert_not_called();destination.assert_not_called();context.assert_not_called();network.assert_not_called()

    def test_workflow_effect_order_uses_the_shared_validator_and_record_data(self):
        for stage,workflow in M.WORKFLOWS.items():
            text=(ROOT/'.github/workflows'/workflow).read_text()
            self.assertLess(text.index('uses: actions/checkout@'),text.index('community-stage-record.py validate --stage '+stage+' --dispatch'))
            for key in M.INPUT_ENV:self.assertIn(key+':',text)
            self.assertNotIn("${{ inputs.stage_record_b64 }}\n        run:",text)
            if stage=='producer':
                self.assertLess(text.index('--stage producer --dispatch'),text.index('name: Checkout record-bound product source'))
                self.assertIn('ref: ${{ steps.stage.outputs.product_commit }}',text)
                self.assertNotIn('21572d9e73ca901583a8b526c9af7cd0319d59f1',text)
                self.assertLess(text.index('--stage producer --dispatch'),text.index('GITHUB_TOKEN: ${{ github.token }}'))
            elif stage=='publisher':
                self.assertLess(text.index('--stage publisher --dispatch'),text.index('GH_TOKEN: ${{ github.token }}'))
            else:
                self.assertLess(text.index('community-release-acceptance.py validate --stage-dispatch'),text.index('uses: actions/setup-python@'))
                self.assertIn('GITHUB_WORKFLOW_SHA="$GITHUB_WORKFLOW_SHA"',text)
                self.assertIn('GITHUB_EVENT_PATH="$GITHUB_EVENT_PATH"',text)

    def test_full_pack_rehearsal_has_an_independent_read_only_retry(self):
        rehearsal = module('full_pack_context', 'community-full-pack-rehearsal.py')
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary)
            world = P.World(path)
            product = 'd' * 40
            world.source = F.producer(rehearsal.TARGET_VERSION, product, schema=rehearsal.TARGET_SCHEMA)
            world.tag = f'source-{product}-run-{world.parent_run}-linux-amd64'
            world.prepare_chain()
            record = copy.deepcopy(world.record)
            record['predecessor']['program_commit'] = product
            body = M.canonical(record)
            env = self.native_env(path, record, 'publisher')
            for name in ("GITHUB_SHA", "GITHUB_WORKFLOW_SHA", "EXPECTED_WORKFLOW_COMMIT"):
                env[name] = product
            event_path = Path(env['GITHUB_EVENT_PATH'])
            event = M.strict_json(event_path.read_bytes())
            event['inputs']['expected_workflow_commit'] = product
            event_path.write_bytes(M.canonical(event))
            with self.subTest(product=product):
                self.assertEqual((body, record), rehearsal.read_rehearsal_context(M, P.H, env))
                self.assertEqual(body, M.read_dispatch('publisher', env)[0])
                retry = dict(env, GITHUB_RUN_ATTEMPT='2')
                self.assertEqual((body, record), rehearsal.read_rehearsal_context(M, P.H, retry))
                with self.assertRaises(M.Stop):
                    M.read_dispatch('publisher', retry)
                for field, value in (
                    ('GITHUB_ACTOR_ID', '1'), ('GITHUB_REF', 'refs/heads/elsewhere'),
                    ('GITHUB_WORKFLOW_REF', f'{M.REPOSITORY}/.github/workflows/ci.yml@refs/heads/main'),
                    ('STAGE_RECORD_SHA256', '0' * 64),
                ):
                    with self.subTest(field=field), self.assertRaises((AssertionError, RuntimeError, M.Stop)):
                        rehearsal.read_rehearsal_context(M, P.H, {**retry, field: value})
                other = copy.deepcopy(record)
                other['product']['version'] = '1.15.6'
                other_env = self.native_env(path, other, 'publisher')
                for name in ("GITHUB_SHA", "GITHUB_WORKFLOW_SHA", "EXPECTED_WORKFLOW_COMMIT"):
                    other_env[name] = product
                event_path = Path(other_env['GITHUB_EVENT_PATH'])
                event = M.strict_json(event_path.read_bytes())
                event['inputs']['expected_workflow_commit'] = product
                event_path.write_bytes(M.canonical(event))
                with self.assertRaises(AssertionError):
                    rehearsal.read_rehearsal_context(M, P.H, other_env)

            amended = self.native_env(path, record, 'publisher')
            with patch.object(P.H, 'RETAINED_RECORD_SHA256', M.sha(body)):
                self.assertEqual((body, record), rehearsal.read_rehearsal_context(M, P.H, amended))
                with self.assertRaises(AssertionError):
                    rehearsal.read_rehearsal_context(M, P.H, {**amended, 'GITHUB_WORKFLOW_SHA': 'f' * 40})
                alternate = copy.deepcopy(record)
                alternate['inputs']['draft_id'] += 1
                with self.assertRaisesRegex(RuntimeError, 'exact retained'):
                    rehearsal.read_rehearsal_context(M, P.H, self.native_env(path, alternate, 'publisher'))

        workflow = (ROOT / '.github/workflows' / M.WORKFLOWS['publisher']).read_text()
        publisher, verify = workflow.split('\n  rehearse-full-materials:\n')
        self.assertIn('github.run_attempt == 1', publisher)
        self.assertIn('runs-on: ubuntu-24.04', publisher)
        self.assertLess(publisher.index('--validate-publisher-subject'), publisher.index('--check-docker-archive-identity'))
        self.assertLess(publisher.index('--check-docker-archive-identity'), publisher.index('GH_TOKEN:'))
        self.assertIn('--execute-approved-public-stage', publisher)
        self.assertIn('needs: publish-public', verify)
        self.assertIn("needs.publish-public.result == 'success'", verify)
        self.assertIn("needs.publish-public.result == 'skipped' && github.run_attempt == 1", verify)
        self.assertIn(rehearsal.RETAINED_RECORD_SHA, publisher)
        self.assertIn(rehearsal.RETAINED_RECORD_SHA, verify)
        self.assertIn(rehearsal.RETAINED_PRODUCT_COMMIT, publisher)
        self.assertIn(rehearsal.RETAINED_PRODUCT_COMMIT, verify)
        self.assertIn('PUBLISHER_RECEIPT: ${{ needs.publish-public.outputs.publisher_receipt }}', verify)
        self.assertIn('ref: ${{ needs.publish-public.outputs.product_commit }}', verify)
        self.assertIn('permissions:\n      contents: write\n      actions: read\n    runs-on:', verify)
        self.assertNotIn('packages:', verify)
        self.assertNotIn('--execute-approved-public-stage', verify)

    def test_preview26_recovery_uses_its_retained_predecessor_in_the_prepare_step(self):
        materials = module('full_pack_guides', 'community-full-pack-materials.py')
        predecessor = {'version': '1.15.8', 'schema': '000131_customer_standard_pricing'}
        product = {'version': '1.16.0', 'schema': predecessor['schema'], 'commit': 'a' * 40}
        guide = materials.recovery_guide(product, predecessor, 25).decode()
        target_release = 'https://github.com/foigent/unirouter/releases/tag/v1.16.0-preview.26'
        source_release = 'https://github.com/foigent/unirouter/releases/tag/v1.15.8-preview.25'

        def check(body):
            words = ' '.join(body.split())
            self.assertIn('from **1.15.8 / M131 / sequence 25** to **1.16.0 / M131 / sequence 26**', words)
            self.assertIn('The complete ledger is checked and no migration is applied.', words)
            self.assertIn(target_release, body)
            self.assertIn(source_release, body)
            source_arguments = re.findall(
                r'retained (?:signed )?([0-9]+[.][0-9]+[.][0-9]+) (?:signed )?full package as `--source-package`', words)
            self.assertEqual(['1.15.8'], source_arguments)

        check(guide)
        check(guide + '\n2026-11-23 historical Preview 25 used 1.15.7 / M130 and applied M131.\n')
        # The original preview document had the correct header and also
        # embedded this incorrect operational instruction further down.
        with self.assertRaises(AssertionError):
            check(guide + '\nrun `prepare` with the retained 1.15.7 signed full package as `--source-package`.\n')
        for name in ('install-linux.md', 'install-linux.zh-CN.md'):
            with self.subTest(guide=name):
                text = (ROOT / 'docs/community' / name).read_text()
                self.assertLess(text.index(target_release), text.index(source_release))

    def test_full_pack_replays_only_the_pinned_successful_publisher(self):
        rehearsal = module('full_pack_replay', 'community-full-pack-rehearsal.py')
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary)
            product = rehearsal.RETAINED_PRODUCT_COMMIT
            _, raw, result_raw, _, _, _ = self.setup_chain(
                path, rehearsal.TARGET_VERSION, product, schema=rehearsal.TARGET_SCHEMA)
            record = M.strict_json(raw)
            record['predecessor']['program_commit'] = product
            record['product']['tree'] = rehearsal.RETAINED_PRODUCT_TREE
            raw = M.canonical(record)
            receipt_images = M.strict_json(result_raw)['images']
            result = M.publisher_result(raw, product, rehearsal.RETAINED_PUBLISHER_RUN, receipt_images)
            receipt = {'stage_result': result, 'images': receipt_images}
            frozen = path / 'reviewed-result.json'
            frozen.write_bytes(M.canonical(result))
            env = self.native_env(path, record, 'publisher')
            run = {'id': rehearsal.RETAINED_PUBLISHER_RUN, 'workflow_id': 387706956,
                   'run_attempt': 1, 'event': 'workflow_dispatch', 'head_sha': product, 'head_branch': 'main',
                   'status': 'completed', 'conclusion': 'success',
                   'path': '.github/workflows/publish-accepted-community-images.yml',
                   'repository': {'id': M.REPOSITORY_ID, 'full_name': M.REPOSITORY},
                   'head_repository': {'id': M.REPOSITORY_ID},
                   'actor': {'id': M.ACTOR_ID}, 'triggering_actor': {'id': M.ACTOR_ID}}
            job = {'id': rehearsal.RETAINED_PUBLISHER_JOB, 'run_id': rehearsal.RETAINED_PUBLISHER_RUN,
                   'run_attempt': 1, 'name': 'publish-public', 'status': 'completed', 'conclusion': 'success',
                   'head_sha': product, 'labels': ['ubuntu-24.04']}
            branch = {'protected': True, 'commit': {'sha': env['GITHUB_SHA']}}
            replies = {'branches/main': branch, f'actions/runs/{run["id"]}': run, f'actions/jobs/{job["id"]}': job}
            calls = []
            run_path = f'actions/runs/{run["id"]}'
            job_path = f'actions/jobs/{job["id"]}'
            denied_path = None
            def read_native(argv, **_):
                self.assertEqual('GET', argv[argv.index('--method') + 1])
                endpoint = argv[-1].removeprefix(f'repos/{M.REPOSITORY}/')
                calls.append(endpoint)
                if endpoint == denied_path:
                    return subprocess.CompletedProcess(argv, 1,
                        b'HTTP/2.0 403 Forbidden\r\nX-GitHub-Request-Id: BEF1:258A:112F:C191:12345\r\n'
                        b'Location: https://example.invalid/release?signed=synthetic-private\r\n\r\n'
                        b'{"token":"synthetic-private"}', b'gh: synthetic-private (HTTP 403)')
                return subprocess.CompletedProcess(argv, 0, b'HTTP/2.0 200 OK\r\n\r\n' + M.canonical(replies[endpoint]), b'')
            with patch.multiple(rehearsal, RETAINED_RECORD_SHA=M.sha(raw), RETAINED_RESULT=frozen,
                                RETAINED_RESULT_SHA=M.sha(frozen.read_bytes())), patch.object(rehearsal.subprocess, 'run', side_effect=read_native):
                self.assertEqual((raw, record), rehearsal.replay_context(M, P.H, env))
                for change in ({'GITHUB_RUN_ATTEMPT':'2'}, {'GITHUB_WORKFLOW_SHA':'e'*40},
                               {'STAGE_RECORD_SHA256':'0'*64}):
                    with self.subTest(change=change), self.assertRaises((AssertionError, RuntimeError, M.Stop)):
                        rehearsal.replay_context(M, P.H, {**env, **change})
                self.assertEqual([], calls)
                # A wrong result fails before the first GitHub GET.
                unmodified = frozen.read_bytes()
                frozen.write_bytes(unmodified+b' ')
                with self.assertRaises(rehearsal.RehearsalFailure) as invalid:
                    rehearsal.retained_receipt(M, P.H, env, raw)
                self.assertEqual('verify_retained_publisher_result', invalid.exception.report['stage'])
                self.assertEqual([], calls)
                frozen.write_bytes(unmodified)
                self.assertEqual(receipt, rehearsal.retained_receipt(M, P.H, env, raw))
                self.assertEqual(['branches/main', run_path, job_path], calls)
                for fields, name, swapped, phase in (
                        (branch, 'protected', False, 'verify_protected_main'),
                        (run, 'conclusion', 'failure', 'verify_original_publisher_run'),
                        (run, 'head_sha', 'e'*40, 'verify_original_publisher_run'),
                        (job, 'run_id', 999, 'verify_original_publisher_job'),
                        (job, 'name', 'other', 'verify_original_publisher_job')):
                    value = fields[name]
                    fields[name] = swapped
                    with self.subTest(name=name), self.assertRaises(rehearsal.RehearsalFailure) as invalid:
                        rehearsal.retained_receipt(M, P.H, env, raw)
                    self.assertEqual(phase, invalid.exception.report['stage'])
                    fields[name] = value
                denied_path = job_path
                with self.assertRaises(rehearsal.RehearsalFailure) as denied:
                    rehearsal.retained_receipt(M, P.H, env, raw)
                self.assertEqual({'state':'linux_complete_formal_members_stopped',
                                  'stage':'read_original_publisher_job','failure_type':'GitHubReadError',
                                  'http_status':403,'github_request_id':'BEF1:258A:112F:C191:12345'},
                                 denied.exception.report)
                self.assertNotIn('synthetic-private', json.dumps(denied.exception.report))

    def test_rehearsal_download_diagnostic_discards_the_child_error(self):
        rehearsal = module('full_pack_safe_failure', 'community-full-pack-rehearsal.py')
        args = ['gh', 'api', '--method', 'GET', 'unused']
        stderr = b'gh: https://example.invalid/a?signed=synthetic-private (HTTP 429)\n'
        reply = subprocess.CompletedProcess(args, 1, b'', stderr)
        with patch.object(rehearsal.subprocess, 'run', return_value=reply):
            with self.assertRaises(rehearsal.RehearsalFailure) as error:
                rehearsal.download_github_asset(args, {}, 'download_producer_evidence', output=io.BytesIO())
        report = error.exception.report
        self.assertEqual(('download_producer_evidence', 429, None),
                         (report['stage'], report['http_status'], report['github_request_id']))
        self.assertNotIn('synthetic-private', json.dumps(report))
        self.assertEqual({'http_status': None, 'github_request_id': None},
            rehearsal.http_failure(b'HTTP/2.0 418 Ignored\nX-GitHub-Request-Id: invalid-value'))

    def test_rehearsal_request_id_requires_complete_unambiguous_http_headers(self):
        rehearsal = module('full_pack_http_shape', 'community-full-pack-rehearsal.py')
        status = b'HTTP/2.0 503 Unavailable\r\n'
        header = b'X-GitHub-Request-Id: BEF1:258A:112F:C191:12345\r\n'
        self.assertEqual({'http_status':503,'github_request_id':'BEF1:258A:112F:C191:12345'},
                         rehearsal.http_failure(status+header+b'\r\nbody'))
        for response, expected_status in (
                (b'CLI diagnostic text\r\n'+header+b'\r\n', None),
                (status+header, None),
                (status+header+header+b'\r\n', 503),
                (status+header+b'x-github-request-id: invalid-value\r\n\r\n', 503),
                (status+b'Content-Type: text/plain\r\n\r\n'+header, 503),
                (status+header+b'incomplete header line\r\n\r\n', 503)):
            with self.subTest(response=response):
                self.assertEqual({'http_status':expected_status,'github_request_id':None},
                                 rehearsal.http_failure(response))
        self.assertEqual({'http_status':403,'github_request_id':None}, rehearsal.http_failure(
            b'CLI diagnostic text\r\n'+header+b'\r\n',
            b'gh: could not read (HTTP 403)\n'+header))

    def test_offline_handoff_commands_are_exclusive_and_use_existing_receipts(self):
        with tempfile.TemporaryDirectory() as tmp:
            path=Path(tmp);world,publisher,result,bindings,_,record=self.setup_chain(path)
            evidence=path/'evidence.tar.gz';evidence.write_bytes(world.payloads[4])
            child=path/'publisher.json'
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(0,M.main(['publisher-candidate','--evidence',str(evidence),'--out',str(child)]))
                self.assertEqual(publisher,child.read_bytes())
                self.assertEqual(1,M.main(['publisher-candidate','--evidence',str(evidence),'--out',str(child)]))
                receipt=path/'receipt.json';receipt.write_bytes(M.canonical({'stage_result':json.loads(result)}))
                proof=path/'publication-bindings.json'
                self.assertEqual(0,M.main(['publication-bindings','--publisher-record',str(child),'--publisher-result',str(receipt),'--out',str(proof)]))
                self.assertEqual(bindings,json.loads(proof.read_bytes()))
                config=path/'config.json';config.write_bytes(M.canonical(record['inputs']['config']))
                consumer=path/'consumer.json'
                self.assertEqual(0,M.main(['consumer-candidate','--publisher-record',str(child),'--publisher-result',str(receipt),'--config',str(config),'--out',str(consumer)]))
                self.assertEqual(record,json.loads(consumer.read_bytes()))

if __name__=='__main__':unittest.main(verbosity=2)
