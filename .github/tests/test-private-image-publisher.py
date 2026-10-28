#!/usr/bin/env python3
"""Offline publisher behavior checks; no credentials, Docker or network calls."""
import contextlib
import copy
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile
import tarfile
import textwrap
import types
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / '.github/workflows/publish-accepted-community-images.yml'
H = types.ModuleType('private_publisher_under_test')
H.__file__ = str(ROOT/'.github/tests/community-image-publisher.py')
exec(compile(Path(H.__file__).read_bytes(), H.__file__, 'exec'), H.__dict__)
def module(name, filename):
    result=types.ModuleType(name); result.__file__=str(ROOT/'.github/tests'/filename)
    exec(compile(Path(result.__file__).read_bytes(),result.__file__,'exec'),result.__dict__); return result
M=module('publisher_stage_fixture_contract','community-stage-record.py')
F=module('publisher_stage_fixture_inputs','community-stage-fixtures.py')
COMMIT = 'a' * 40


class World:
    """Synthetic native API/Docker responses; unknown calls fail immediately."""
    def __init__(self, directory):
        self.directory = Path(directory)
        self.calls = []; self.images = []; self.payloads = {}
        self.states = {}; self.exists = {}; self.reads = {}
        self.conflict = None; self.bad_layers = None; self.pull_failure = None
        self.version_failure = None; self.tamper = None; self.appear = None
        self.visibility = 'public'; self.link = H.REPOSITORY
        self.push_lost_response = None; self.manifest_mismatch = None; self.appear_read = 2
        for n, role in enumerate(('backend', 'frontend'), 1):
            data = (role + '-accepted-bytes').encode()
            image = {'role': role, 'package_name': 'unirouter-' + role,
                     'repository': 'ghcr.io/foigent/unirouter-' + role,
                     'asset_id': n, 'archive_name': role + '-image.tar.gz',
                     'archive_bytes': len(data), 'archive_sha256': hashlib.sha256(data).hexdigest(),
                     'config_id': 'sha256:' + str(n) * 64, 'diff_ids': ['sha256:' + str(n + 2) * 64]}
            self.images.append(image); self.payloads[n] = data
            self.states[role] = None; self.exists[role] = True; self.reads[role] = 0
        self.source = F.producer(); self.draft=456; self.parent_program='c'*40; self.parent_run=12345
        self.tag = 'source-'+self.source['product']['commit']+'-run-'+str(self.parent_run)+'-linux-amd64'
        self.prepare_chain()

    def prepare_chain(self):
        members={'synthetic-source-observation.json':b'{"source":"synthetic"}'}
        self.verifier=b'synthetic-verifier'; self.payloads[3]=self.verifier
        files=dict(members)
        files.update(M.producer_chain(M.canonical(self.source),self.parent_program,self.parent_run,self.draft,
            {row['role']:row for row in self.images}, {'asset_id':3,**M.byte_fact(self.verifier)},members))
        facts={name:M.byte_fact(body) for name,body in files.items()}
        files[M.CLOSED_MANIFEST]=M.canonical({'files':facts,'bytes':sum(row['bytes'] for row in facts.values()),'scope':'synthetic closed metadata'})
        self.record=json.loads(files[M.CHAIN_PATHS[2]])
        output=io.BytesIO()
        with tarfile.open(fileobj=output,mode='w:gz') as archive:
            for name,body in files.items():
                info=tarfile.TarInfo(name);info.size=len(body);archive.addfile(info,io.BytesIO(body))
        self.payloads[4]=output.getvalue(); self.payloads[5]=b'synthetic-checksums';self.payloads[6]=b'synthetic-marker'

    def digest(self, role):
        return 'sha256:' + ('b' if role == 'backend' else 'c') * 64

    def gh(self, env, path, allow404=False):
        self.calls.append(('GET', path))
        if path == 'repos/' + H.REPOSITORY:
            return {'id':M.REPOSITORY_ID,'full_name': H.REPOSITORY, 'visibility': 'public'}
        if path.endswith('/git/ref/heads/main'):
            return {'object': {'sha': COMMIT}}
        if path == 'repos/'+H.REPOSITORY+'/actions/runs/'+str(self.parent_run):
            return {'id':self.parent_run,'run_attempt':1,'status':'completed','conclusion':'success',
                'head_sha':self.parent_program,'event':'workflow_dispatch','head_branch':'main','path':'.github/workflows/community-byte-candidate.yml',
                'repository':{'id':M.REPOSITORY_ID,'full_name':H.REPOSITORY},'head_repository':{'id':M.REPOSITORY_ID},
                'actor':{'id':M.ACTOR_ID},'triggering_actor':{'id':M.ACTOR_ID}}
        if path == 'repos/' + H.REPOSITORY + '/releases/' + str(self.draft):
            names={1:'backend-image.tar.gz',2:'frontend-image.tar.gz',3:'uniroute-upgrade-linux-amd64',
                4:'candidate-evidence.tar.gz',5:'SHA256SUMS',6:'private-channel-check.txt'}
            return {'id':self.draft,'draft':True,'prerelease':True,'tag_name':'ur-byte-candidate-'+str(self.parent_run)+'-attempt1','published_at':None,'target_commitish':self.parent_program,
                'author':{'id':41898282},'assets':[{'id':ident,'name':names[ident],'size':len(body),
                'digest':'sha256:'+H.hashlib.sha256(body).hexdigest(),'state':'uploaded','uploader':{'id':41898282}}
                for ident,body in self.payloads.items()]}
        for image in self.images:
            role = image['role']; prefix = 'orgs/foigent/packages/container/' + image['package_name']
            if path == prefix:
                if not self.exists[role]:
                    assert allow404
                    return None
                return {'visibility': self.visibility, 'repository': {'full_name': self.link}}
            if path == prefix + '/versions?state=active&per_page=100&page=1':
                self.reads[role] += 1
                if role == self.version_failure:
                    raise RuntimeError('synthetic authenticated API failure')
                if role == self.appear and self.reads[role] == self.appear_read:
                    self.states[role] = self.digest(role)
                digest = self.states[role]
                return [] if digest is None else [
                    {'id': image['asset_id'], 'name': digest,
                     'metadata': {'package_type': 'container', 'container': {'tags': [self.tag]}}}]
        raise AssertionError('Unexpected API call: ' + path)

    def command(self, args, env, inp=None, output=None):
        self.calls.append(tuple(args[:3]))
        if args[:2] == ['gh', 'api']:
            ident = int(args[4].rsplit('/', 1)[1])
            data = self.payloads[ident]
            if self.tamper == ident:
                data = b'x' * len(data)
            output.write(data)
            return b''
        if args[:2] == ['docker', 'login']:
            assert inp == b'offline-synthetic-value\n'
            return b''
        if args[:3] == ['docker', 'image', 'inspect']:
            image = next(x for x in self.images if args[-1] == x['config_id'] or
                         args[-1].startswith(x['repository'] + ':'))
            role = image['role']
            q = {'Id': image['config_id'], 'RootFS': {'Layers': image['diff_ids']},
                 'Os': 'linux', 'Architecture': 'amd64', 'RepoDigests': []}
            if self.states[role] is not None:
                q['RepoDigests'] = [image['repository'] + '@' + self.states[role]]
            if self.manifest_mismatch == role:
                q['RepoDigests'] = [image['repository'] + '@sha256:' + 'd' * 64]
            if self.conflict == role:
                q['Id'] = 'sha256:' + 'f' * 64
            if self.bad_layers == role:
                q['RootFS'] = {'Layers': ['sha256:' + 'e' * 64]}
            return json.dumps([q]).encode()
        if args[:2] in (['docker', 'pull'], ['docker', 'push']):
            image = next(x for x in self.images if args[-1] == x['repository'] + ':' + self.tag)
            role = image['role']
            if args[1] == 'pull':
                if self.pull_failure == role or self.states[role] is None:
                    raise RuntimeError('synthetic Docker pull failure')
            else:
                self.states[role] = self.digest(role); self.exists[role] = True
                if self.push_lost_response == role:
                    raise RuntimeError('synthetic lost push response')
            return b''
        if args[:2] == ['docker', 'load'] or args[:3] == ['docker', 'image', 'tag']:
            return b''
        raise AssertionError('Unexpected command: ' + repr(args))

    def execute(self):
        env = {'GITHUB_ACTIONS': 'true', 'GITHUB_REPOSITORY': H.REPOSITORY,
               'GITHUB_REF': 'refs/heads/main', 'GITHUB_EVENT_NAME': 'workflow_dispatch',
               'GITHUB_WORKFLOW_REF': H.WORKFLOW_REF, 'GITHUB_SHA': COMMIT,
               'GH_TOKEN': 'offline-synthetic-value', 'GITHUB_ACTOR': 'offline-fixture',
               'RUNNER_TEMP': str(self.directory), 'GITHUB_STEP_SUMMARY': str(self.directory / 'summary.md')}
        inputs=dict(M.encode(self.record),expected_workflow_commit=COMMIT,admission_sha256='d'*64)
        event=self.directory/'event.json';event.write_bytes(M.canonical({'repository':{'id':M.REPOSITORY_ID,'full_name':H.REPOSITORY},
            'sender':{'id':M.ACTOR_ID},'inputs':inputs}))
        env.update(GITHUB_REPOSITORY_ID=str(M.REPOSITORY_ID),GITHUB_ACTOR_ID=str(M.ACTOR_ID),GITHUB_WORKFLOW_SHA=COMMIT,
            GITHUB_RUN_ATTEMPT='1',GITHUB_RUN_ID='67890',RUNNER_ENVIRONMENT='github-hosted',RUNNER_OS='Linux',RUNNER_ARCH='X64',
            GITHUB_EVENT_PATH=str(event),STAGE_RECORD_B64=inputs['stage_record_b64'],STAGE_RECORD_SHA256=inputs['stage_record_sha256'],
            EXPECTED_WORKFLOW_COMMIT=COMMIT,ADMISSION_SHA256=inputs['admission_sha256'])
        with patch.dict(os.environ, env, clear=True), \
             patch.object(H, 'gh', self.gh), patch.object(H, 'command', self.command), \
             patch.object(H.shutil, 'which', return_value='/unused'), \
             patch.object(H.subprocess, 'run', side_effect=AssertionError('Real subprocess forbidden')), \
             contextlib.redirect_stdout(io.StringIO()) as out:
            H.execute()
        return json.loads(out.getvalue())

    def pushes(self):
        return [x for x in self.calls if x[:2] == ('docker', 'push')]


class PublisherTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='ur-private-publisher-test-')
        self.addCleanup(self.temp.cleanup)
        self.world = World(self.temp.name)

    def test_existing_public_packages_accept_new_exact_tags(self):
        result = self.world.execute()
        self.assertEqual(len(self.world.pushes()), 2)
        self.assertTrue(all(x['operation'] == 'new_public_push_of_exact_accepted_image' for x in result['images']))
        first_push = next(i for i, c in enumerate(self.world.calls) if c[:2] == ('docker', 'push'))
        self.assertEqual(sum(c[:2] == ('docker', 'load') for c in self.world.calls[:first_push]), 2)

    def test_same_actual_entry_accepts_two_sources_without_changing_program(self):
        for version,commit in (('1.15.0','a'*40),('1.15.1','b'*40)):
            with self.subTest(version=version),tempfile.TemporaryDirectory() as tmp:
                world=World(tmp);world.source=F.producer(version,commit)
                world.tag='source-'+commit+'-run-'+str(world.parent_run)+'-linux-amd64';world.prepare_chain()
                result=world.execute()
                self.assertEqual(commit,result['product_source'])
                self.assertEqual(version,result['stage_result']['product']['version'])
                self.assertEqual(2,len(world.pushes()))
                M.verify_publisher_result(M.canonical(world.record),result['stage_result'])

    def test_changed_parent_evidence_refuses_before_docker_login_or_push(self):
        original=self.world.payloads[4]
        files=M.evidence_files(self._evidence_file(original))
        files['synthetic-source-observation.json']=b'{"changed":true}'
        facts={name:M.byte_fact(body) for name,body in files.items() if name!=M.CLOSED_MANIFEST}
        files[M.CLOSED_MANIFEST]=M.canonical({'files':facts,'bytes':sum(row['bytes'] for row in facts.values()),'scope':'synthetic rebuilt outer closure'})
        output=io.BytesIO()
        with tarfile.open(fileobj=output,mode='w:gz') as archive:
            for name,body in files.items():
                info=tarfile.TarInfo(name);info.size=len(body);archive.addfile(info,io.BytesIO(body))
        self.world.payloads[4]=output.getvalue()
        with self.assertRaisesRegex(Exception,'stage_semantic_membership'):self.world.execute()
        self.assertFalse(any(call[:2]==('docker','login') for call in self.world.calls))
        self.assertFalse(self.world.pushes())

    def _evidence_file(self,body):
        path=Path(self.temp.name)/'synthetic-evidence.tar.gz';path.write_bytes(body);return path

    def test_wrong_native_producer_identity_refuses_before_any_docker_effect(self):
        real=self.world.gh
        def changed(env,path,allow404=False):
            value=real(env,path,allow404)
            if '/actions/runs/' in path:value['head_sha']='f'*40
            return value
        with patch.object(self.world,'gh',side_effect=changed),self.assertRaisesRegex(RuntimeError,'producer run'):
            self.world.execute()
        self.assertFalse(any(call[0]=='docker' for call in self.world.calls))

    def test_both_existing_exact_tags_reuse_without_push(self):
        for role in self.world.states:
            self.world.states[role] = self.world.digest(role)
        result = self.world.execute()
        self.assertFalse(self.world.pushes())
        self.assertTrue(all(x['operation'] == 'reused_existing_exact_public_image_no_push' for x in result['images']))

    def test_partial_previous_upload_reuses_backend_and_pushes_only_frontend(self):
        self.world.states['backend'] = self.world.digest('backend')
        self.world.execute()
        self.assertEqual(self.world.pushes(), [('docker', 'push', self.world.images[1]['repository'] + ':' + self.world.tag)])

    def test_second_existing_image_conflict_or_layer_mismatch_blocks_both(self):
        for field in ('conflict', 'bad_layers'):
            with self.subTest(field=field):
                self.world.calls.clear(); setattr(self.world, field, 'frontend')
                self.world.states['frontend'] = self.world.digest('frontend')
                with self.assertRaises(RuntimeError): self.world.execute()
                self.assertFalse(self.world.pushes()); setattr(self.world, field, None)

    def test_authenticated_api_failure_stops_before_docker(self):
        self.world.version_failure = 'frontend'
        with self.assertRaises(RuntimeError): self.world.execute()
        self.assertFalse(any(c[0] == 'docker' for c in self.world.calls))

    def test_existing_pull_failure_is_never_absence_or_push_permission(self):
        self.world.states['backend'] = self.world.digest('backend'); self.world.pull_failure = 'backend'
        with self.assertRaises(RuntimeError): self.world.execute()
        self.assertFalse(self.world.pushes())

    def test_bad_second_archive_stops_before_either_push(self):
        self.world.tamper = 2
        with self.assertRaisesRegex(RuntimeError, 'SHA256'): self.world.execute()
        self.assertFalse(self.world.pushes())

    def test_target_appearing_during_preparation_stops_before_either_push(self):
        self.world.appear = 'frontend'
        with self.assertRaisesRegex(RuntimeError, 'changed before upload'): self.world.execute()
        self.assertFalse(self.world.pushes())

    def test_last_recheck_does_not_overwrite_second_tag_after_first_push(self):
        self.world.appear = 'frontend'; self.world.appear_read = 3
        with self.assertRaisesRegex(RuntimeError, 'changed before upload'): self.world.execute()
        self.assertEqual(self.world.pushes(), [('docker', 'push', self.world.images[0]['repository'] + ':' + self.world.tag)])

    def test_existing_tag_manifest_mismatch_stops_before_either_push(self):
        self.world.states['frontend'] = self.world.digest('frontend'); self.world.manifest_mismatch = 'frontend'
        with self.assertRaisesRegex(RuntimeError, 'digest changed during pull'): self.world.execute()
        self.assertFalse(self.world.pushes())

    def test_uncertain_push_is_not_retried_and_later_exact_recovery_is_possible(self):
        self.world.push_lost_response = 'backend'
        with self.assertRaises(RuntimeError): self.world.execute()
        self.assertEqual(len(self.world.pushes()), 1)
        self.world.push_lost_response = None; self.world.calls.clear()
        self.world.execute()  # A separate synthetic invocation, not automatic retry.
        self.assertEqual(self.world.pushes(), [('docker', 'push', self.world.images[1]['repository'] + ':' + self.world.tag)])

    def test_existing_package_visibility_or_association_refused(self):
        for field, value in [('visibility', 'private'), ('visibility', 'internal'), ('link', 'unrelated/repository')]:
            with self.subTest(field=field):
                old = getattr(self.world, field); setattr(self.world, field, value)
                with self.assertRaises(RuntimeError): self.world.execute()
                self.assertFalse(self.world.pushes()); setattr(self.world, field, old)

    def test_either_missing_package_refuses_before_docker_or_push(self):
        for role in ('backend', 'frontend'):
            with self.subTest(role=role):
                self.world.calls.clear()
                self.world.exists = {'backend': role != 'backend', 'frontend': role != 'frontend'}
                with self.assertRaisesRegex(RuntimeError, 'existing public packages'):
                    self.world.execute()
                self.assertFalse(any(c[0] == 'docker' for c in self.world.calls))
                self.assertFalse(self.world.pushes())

    def test_full_pagination_finds_tag_on_later_page(self):
        first = [{'id': i + 1, 'name': 'sha256:' + '1' * 64,
                  'metadata': {'package_type': 'container', 'container': {'tags': []}}} for i in range(100)]
        last = {'id': 101, 'name': 'sha256:' + '2' * 64,
                'metadata': {'package_type': 'container', 'container': {'tags': [self.world.tag]}}}
        with patch.object(H, 'gh', side_effect=[{'visibility': 'public', 'repository': {'full_name': H.REPOSITORY}}, first, [last]]) as get:
            self.assertEqual(H.package_tag({}, self.world.images[0], self.world.tag), (True, last['name']))
        self.assertTrue(get.call_args_list[-1].args[1].endswith('page=2'))

    def test_ambiguous_and_malformed_metadata_refused(self):
        row = {'id': 1, 'name': 'sha256:' + '2' * 64,
               'metadata': {'package_type': 'container', 'container': {'tags': [self.world.tag]}}}
        invalid = [None, [row, copy.deepcopy(row)], [row, {**row, 'id': 2}],
                   [{**row, 'name': 'invalid'}], [{**row, 'metadata': {'package_type': 'npm'}}]]
        for rows in invalid:
            with self.subTest(rows=rows), patch.object(H, 'gh', side_effect=[
                    {'visibility': 'public', 'repository': {'full_name': H.REPOSITORY}}, rows]):
                with self.assertRaises((RuntimeError, KeyError)): H.package_tag({}, self.world.images[0], self.world.tag)

    def test_wire_errors_are_not_missing_tags(self):
        for status in (401, 403, 404, 429, 500):
            wire = ('HTTP/2.0 ' + str(status) + '\r\n\r\n{}').encode()
            with self.subTest(status=status), patch.object(H.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1, wire, b'not printed')):
                with self.assertRaises(RuntimeError): H.gh({}, 'orgs/foigent/packages/container/example/versions')
        for wire in (b'', b'HTTP/2.0 200\r\n\r\ninvalid'):
            with self.subTest(wire=wire), self.assertRaises((RuntimeError, ValueError)):
                H.decode_gh_response(wire, 1)

    def test_context_refusal_precedes_credentials_and_API(self):
        with patch.dict(os.environ, {}, clear=True), patch.object(H, 'gh', side_effect=AssertionError('API forbidden')):
            with self.assertRaisesRegex(RuntimeError, 'authorized GitHub Actions'): H.execute()

    def test_describe_has_no_credentials_commands_or_filesystem_effects(self):
        class ForbiddenEnv(dict):
            def get(self, *args): raise AssertionError('Environment access forbidden')
            def __getitem__(self, key): raise AssertionError('Environment access forbidden')
        with patch.object(H.os, 'environ', ForbiddenEnv()), patch.object(H, 'gh', side_effect=AssertionError('API forbidden')), \
             patch.object(H, 'command', side_effect=AssertionError('Command forbidden')), \
             patch('builtins.open', side_effect=AssertionError('File write forbidden')):
            result = H.describe()
        self.assertEqual(result['mode'], 'offline_review_only')


if __name__ == '__main__':
    unittest.main()
