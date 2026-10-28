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

class StageChainTests(unittest.TestCase):
    def setup_chain(self,path,version='1.15.0',commit='a'*40):
        world=P.World(path);world.source=F.producer(version,commit)
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
        return {'GITHUB_REPOSITORY':M.REPOSITORY,'GITHUB_REPOSITORY_ID':str(M.REPOSITORY_ID),
            'GITHUB_REF':'refs/heads/main','GITHUB_EVENT_NAME':'workflow_dispatch','GITHUB_SHA':'a'*40,
            'GITHUB_WORKFLOW_SHA':'a'*40,'GITHUB_RUN_ATTEMPT':'1','GITHUB_ACTOR_ID':str(M.ACTOR_ID),
            'GITHUB_WORKFLOW_REF':M.REPOSITORY+'/.github/workflows/'+M.WORKFLOWS[stage]+'@refs/heads/main',
            'RUNNER_ENVIRONMENT':'github-hosted','RUNNER_OS':'Linux','RUNNER_ARCH':'X64','GITHUB_EVENT_PATH':str(event),
            'STAGE_RECORD_B64':inputs['stage_record_b64'],'STAGE_RECORD_SHA256':inputs['stage_record_sha256'],
            'EXPECTED_WORKFLOW_COMMIT':inputs['expected_workflow_commit'],'ADMISSION_SHA256':inputs['admission_sha256']}

    def test_two_versions_connect_actual_publisher_and_current_consumer_owners(self):
        for version,commit in (('1.15.0','a'*40),('1.15.1','b'*40)):
            with self.subTest(version=version),tempfile.TemporaryDirectory() as tmp:
                world,publisher,result,bindings,provenance,record=self.setup_chain(Path(tmp),version,commit)
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
        with tempfile.TemporaryDirectory() as tmp:
            _,_,_,_,_,original=self.setup_chain(Path(tmp))
            for kind in ('ring','source','payload_duplicate','schema','parent','app_layer','unknown','sequence','missing_upgrade'):
                with self.subTest(kind=kind):
                    record=copy.deepcopy(original);fixed=record['inputs']['config']['immutable']
                    if kind=='ring':fixed['public_ring']['sha256']='0'*64
                    elif kind=='source':fixed['product']['commit']='f'*40
                    elif kind=='payload_duplicate':fixed['artifacts'].append(copy.deepcopy(fixed['artifacts'][0]))
                    elif kind=='schema':fixed['min_current_schema']='unknown'
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
