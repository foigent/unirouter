#!/usr/bin/env python3
"""Offline closed input/context checks; no API, key, Docker or program launch."""
import base64
import copy
import importlib.util
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parent

def load(name, file):
    spec=importlib.util.spec_from_file_location(name,ROOT/file); module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module);return module
M=load("stage", "community-stage-record.py")
F=load("stage_fixture", "community-stage-fixtures.py")

class StageTests(unittest.TestCase):
    def inputs(self, value):
        return dict(M.encode(value),expected_workflow_commit='a'*40,admission_sha256='b'*64)

    def test_two_versions_and_sources_use_the_same_program(self):
        for version, commit in (('1.15.0','a'*40),('1.15.1','b'*40)):
            for value in (F.producer(version,commit),F.publisher(F.producer(version,commit)['product'])):
                with self.subTest(version=version,stage=value['stage']):
                    raw,actual=M.from_dispatch(self.inputs(value),value['stage'])
                    self.assertEqual(value,actual)
                    self.assertEqual(M.sha(raw),M.encode(value)['stage_record_sha256'])

    def test_decode_and_aggregate_input_limits_are_independent(self):
        value=F.producer(); raw=M.canonical(value)
        raw+=b' '*(M.MAX_RAW-len(raw))
        encoded=base64.urlsafe_b64encode(raw).decode().rstrip('=')
        self.assertLessEqual(len(encoded),M.MAX_ENCODED)
        self.assertEqual(value,M.decode_record(encoded,M.sha(raw),'producer')[1])
        oversized=raw+b' '
        with self.assertRaises(M.Stop):M.decode_record(base64.urlsafe_b64encode(oversized).decode().rstrip('='),M.sha(oversized),'producer')
        M.dispatch_bound({'value':'x'*(M.MAX_DISPATCH-len('{"value":""}'))})
        with self.assertRaises(M.Stop):M.dispatch_bound({'value':'x'*(M.MAX_DISPATCH+1)})
        M.dispatch_bound({str(i):'' for i in range(25)})
        with self.assertRaises(M.Stop):M.dispatch_bound({str(i):'' for i in range(26)})

    def test_unknown_duplicate_source_ring_and_cross_stage_refuse(self):
        for kind in ('approved','image_host','source_file','schema','parent','role'):
            with self.subTest(kind=kind):
                value=F.producer()
                if kind=='approved':value['approved']=True
                elif kind=='image_host':value['inputs']['images']['backend-builder']['pull_reference']='https://foreign.invalid/command'
                elif kind=='source_file':value['inputs']['source_inventory']['files']['../../key']={'bytes':1,'sha256':'a'*64}
                elif kind=='schema':value['product']['schema']='unknown-future-schema'
                elif kind=='parent':value['predecessor']={}
                else:value['inputs']['images']['custom-role']={}
                with self.assertRaises(M.Stop):M.validate_record(value)
        with self.assertRaises(M.Stop):M.strict_json(br'{"stage":"producer","\u0073tage":"producer"}')
        with self.assertRaises(M.Stop):M.decode_record(**{'encoded':M.encode(F.producer())['stage_record_b64'],'expected_sha256':'0'*64})
        with self.assertRaises(M.Stop):M.from_dispatch(self.inputs(F.producer()),'publisher')

    def test_publisher_images_are_separate_exact_native_assets(self):
        value=F.publisher();M.validate_record(value)
        for kind in ('foreign','collision','tag','parent_digest','extra'):
            with self.subTest(kind=kind):
                record=copy.deepcopy(value)
                if kind=='foreign':record['inputs']['images']['backend']['repository']='ghcr.io/foreign/image'
                elif kind=='collision':record['inputs']['images']['frontend']['asset_id']=record['inputs']['images']['backend']['asset_id']
                elif kind=='tag':record['inputs']['tag']='latest'
                elif kind=='parent_digest':record['inputs']['evidence']['semantic_sha256']='a'*64
                else:record['inputs']['images']['backend']['credential']='never-accepted'
                with self.assertRaises(M.Stop):M.validate_record(record)

    def test_exact_native_context_is_independent_of_record_validity(self):
        value=F.producer();inputs=self.inputs(value)
        env={'GITHUB_REPOSITORY':M.REPOSITORY,'GITHUB_REPOSITORY_ID':str(M.REPOSITORY_ID),'GITHUB_REF':'refs/heads/main',
             'GITHUB_EVENT_NAME':'workflow_dispatch','GITHUB_SHA':'a'*40,'GITHUB_WORKFLOW_SHA':'a'*40,'GITHUB_RUN_ATTEMPT':'1',
             'GITHUB_ACTOR_ID':str(M.ACTOR_ID),'GITHUB_WORKFLOW_REF':M.REPOSITORY+'/.github/workflows/'+M.WORKFLOWS['producer']+'@refs/heads/main',
             'RUNNER_ENVIRONMENT':'github-hosted','RUNNER_OS':'Linux','RUNNER_ARCH':'X64'}
        event={'repository':{'id':M.REPOSITORY_ID,'full_name':M.REPOSITORY},'sender':{'id':M.ACTOR_ID},'inputs':inputs}
        M.check_context(env,event,'producer',inputs)
        for key in ('GITHUB_SHA','GITHUB_WORKFLOW_SHA','GITHUB_RUN_ATTEMPT','GITHUB_ACTOR_ID','GITHUB_WORKFLOW_REF','RUNNER_ENVIRONMENT'):
            with self.subTest(key=key):
                changed=dict(env);changed[key]='wrong'
                with self.assertRaises(M.Stop):M.check_context(changed,event,'producer',inputs)
        changed=copy.deepcopy(event);changed['inputs']['admission_sha256']='c'*64
        with self.assertRaises(M.Stop):M.check_context(env,changed,'producer',inputs)

class ProducerChainTests(unittest.TestCase):
    def chain(self):
        original=F.producer();raw=M.canonical(original)
        images=F.publisher(original['product'])['inputs']['images']
        members={'source-and-fixed-inputs.json':b'{"synthetic":"source"}', 'actual-image-inspection.json':b'{"synthetic":"observed"}'}
        files=dict(members)
        files.update(M.producer_chain(raw,'c'*40,12345,456,images,{'asset_id':3,'bytes':1,'sha256':'a'*64},members))
        self.close(files)
        child=json.loads(files[M.CHAIN_PATHS[2]])
        return files,child

    def close(self,files):
        facts={name:M.byte_fact(body) for name,body in files.items() if name!=M.CLOSED_MANIFEST}
        files[M.CLOSED_MANIFEST]=M.canonical({'files':facts,'bytes':sum(row['bytes'] for row in facts.values()),'scope':'synthetic exact metadata closure'})

    def test_parent_and_machine_next_record_have_non_circular_complete_closure(self):
        files,child=self.chain();result=M.verify_producer_chain(files,child)
        self.assertEqual({'source-and-fixed-inputs.json','actual-image-inspection.json'},set(result['members']))
        self.assertTrue(set(M.CHAIN_PATHS)<=set(json.loads(files[M.CLOSED_MANIFEST])['files']))
        self.assertNotIn(M.CLOSED_MANIFEST,result['members'])

    def test_extra_missing_changed_or_cross_source_refuse_even_with_rebuilt_outer_manifest(self):
        for kind in ('extra','missing','changed','source','chain_extra','self_reference'):
            with self.subTest(kind=kind):
                files,child=self.chain()
                if kind=='extra':files['unexpected.json']=b'{}'
                elif kind=='missing':files.pop('actual-image-inspection.json')
                elif kind=='changed':files['actual-image-inspection.json']=b'changed'
                elif kind=='source':child['product']['commit']='f'*40
                elif kind=='chain_extra':files['stage/unapproved-ignore.json']=b'{}'
                else:
                    result=json.loads(files[M.CHAIN_PATHS[1]])
                    result['members'][M.CLOSED_MANIFEST]=M.byte_fact(files[M.CLOSED_MANIFEST])
                    files[M.CHAIN_PATHS[1]]=M.canonical(result)
                self.close(files)
                with self.assertRaises(M.Stop):M.verify_producer_chain(files,child)

class CurrentSourceBindingTests(unittest.TestCase):
    def test_different_stage_source_and_image_facts_reach_existing_actual_owners(self):
        import ast
        import types
        for version,character in (('1.15.0','a'),('1.15.1','b')):
            guard=types.ModuleType('guard_'+character);guard.__file__=str(ROOT/'community-image-candidate.py')
            exec(compile(Path(guard.__file__).read_bytes(),guard.__file__,'exec'),guard.__dict__)
            observer=types.ModuleType('observer_'+character);observer.__file__=str(ROOT/'community-source-evidence.py')
            exec(compile(Path(observer.__file__).read_bytes(),observer.__file__,'exec'),observer.__dict__)
            record=F.producer(version,character*40)
            record['inputs']['source_inventory']['files']['frontend/package.json']['sha256']=character*64
            record['inputs']['source_inventory']['files']['frontend/package-lock.json']['sha256']=('c' if character=='a' else 'd')*64
            record['inputs']['images']['frontend-builder']['config_id']='sha256:'+character*64
            record['inputs']['images']['frontend-builder']['rootfs_diff_ids']=['sha256:'+character*64]
            M.validate_record(record)
            subject=dict(record['product'],tracked_files=record['inputs']['source_inventory']['tracked_files'],tracked_regular_bytes=record['inputs']['source_inventory']['tracked_regular_bytes'])
            guard.configure_stage_inputs(subject,record['inputs']['source_inventory']['files'],record['inputs']['images'])
            observer.configure_stage_inputs(guard.FIXED)
            self.assertEqual(subject,guard.receipt['source'])
            self.assertEqual(subject,observer.expected_source())
            self.assertEqual(record['inputs']['images']['frontend-builder'],observer.expected_images()['frontend-builder'])
            self.assertNotIn('accepted_batch_a',guard.receipt)
            image_role='frontend-builder'
            image_fields=('pull_reference','config_id','rootfs_diff_ids')
            terminal={'schema':'ur-source-evidence-v1','status':'partial',
                'run':{'id':'12345','attempt':1,'workflow_commit':'c'*40,'started_utc':'2026-10-28T00:00:00+00:00','ended_utc':'2026-10-28T00:00:01+00:00'},
                'expected_source':subject,'observed_source':{'commit':subject['commit'],'tree':subject['tree'],'files':subject['tracked_files'],'regular_bytes':subject['tracked_regular_bytes'],'archive_sha256':'a'*64},
                'images':{image_role:{key:record['inputs']['images'][image_role][key] for key in image_fields}},
                'notices':[{'file':row['file'],'status':'not_observed','bytes':0,'sha256':None} for row in observer.NOTICES],
                'binaries':{},'frontend':{'status':'not_observed','image_id':None,'membership_sha256':None,'disposition':[]},
                'operations':[],'cleanup':[],'gaps':[],'failure':None,'bundle':None,
                'producer':{'helper_sha256':'a'*64,'observer_sha256':'b'*64,'reference_sha256':observer.REFERENCE_SHA,'derived_Dockerfile_sha256':None},
                'authority':{'binary_source_proof':'build_declared_only','complete_license_clearance':False,'formal_artifact':False,'public_install_verified':False}}
            observer.validate_terminal(terminal)
            wrong=copy.deepcopy(terminal);wrong['images'][image_role]={key:observer.IMAGES[image_role][key] for key in image_fields}
            with self.assertRaises(observer.Rejected):observer.validate_terminal(wrong)
            guard.receipt['images']['source-frontend-builder']={'rootfs_diff_ids':record['inputs']['images'][image_role]['rootfs_diff_ids']}
            observer.require_frontend_parent(guard,'sha256:'+'a'*64,'sha256:'+'a'*64)
            guard.receipt['images']['source-frontend-builder']={'rootfs_diff_ids':observer.IMAGES[image_role]['rootfs_diff_ids']}
            with self.assertRaises(observer.Rejected):observer.require_frontend_parent(guard,'sha256:'+'a'*64,'sha256:'+'a'*64)
            self.assertEqual((record['inputs']['source_inventory']['files']['frontend/package.json']['sha256'],record['inputs']['source_inventory']['files']['frontend/package-lock.json']['sha256']),observer.expected_frontend_hashes())

if __name__=='__main__':unittest.main()
