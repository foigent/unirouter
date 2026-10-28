#!/usr/bin/env python3
"""Offline failure-path checks. No HTTP sockets, services, Docker or old test reruns."""
import ast
import copy
import io
import json
import os
import re
import subprocess
import sys
import tarfile
import tempfile
import types
import unittest
import urllib.parse
from pathlib import Path
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
H = types.ModuleType('byte_candidate_under_test')
H.__file__ = str(HERE/'community-byte-candidate.py')
exec(compile(Path(H.__file__).read_bytes(), H.__file__, 'exec'), H.__dict__)
PRODUCER = 'a'*40
BOT = 41898282


class FakeHTTP:
    """Native REST wire shape, explicit responses; never opens a network socket."""
    def __init__(self):
        self.calls = []; self.releases = []; self.bodies = {}; self.tag = None
        self.repo_id = H.REPO_ID; self.head = PRODUCER
        self.anonymous_release = 404; self.anonymous_asset = 404
        self.create_timeout = False; self.upload_timeout = False
        self.created_tag = False; self.bad_digest = False; self.wrong_size = False
        self.corrupt_download = False; self.bad_upload_host = False
        self.redirect = None; self.early_pages = []

    def response(self, receive, status, value=None, raw=None, headers=None):
        body = H.encoded(value) if raw is None else raw
        return receive(status, headers or {}, io.BytesIO(body))

    def request(self, method, url, headers, data, receive):
        parsed = urllib.parse.urlsplit(url)
        path = parsed.path; authenticated = 'Authorization' in headers
        self.calls.append({'method': method, 'host': parsed.hostname, 'path': path,
                           'auth': authenticated, 'query': parsed.query})
        if parsed.hostname in ('release-assets.githubusercontent.com', 'objects.githubusercontent.com'):
            if not authenticated:
                return self.response(receive, 200, raw=b'payload')
            raise AssertionError('Authorization leaked to signed host')
        if parsed.hostname == 'github.com':
            return self.response(receive, self.anonymous_asset)
        if path == '/users/github-actions%5Bbot%5D':
            return self.response(receive, 200, {'id': BOT, 'type': 'Bot', 'login': 'github-actions[bot]'})
        if path == H.PREFIX:
            return self.response(receive, 200, {'id': self.repo_id, 'full_name': H.REPO,
                'private': False, 'default_branch': 'main'})
        if path == H.PREFIX+'/git/ref/heads/main':
            return self.response(receive, 200, {'object': {'type': 'commit', 'sha': self.head}})
        if path.startswith(H.PREFIX+'/git/ref/tags/'):
            return self.response(receive, 404 if self.tag is None else 200,
                {'object': {'type': 'commit', 'sha': self.tag}})
        if path == H.PREFIX+'/releases' and method == 'GET':
            page = int(urllib.parse.parse_qs(parsed.query)['page'][0])
            rows = self.early_pages[page-1] if page <= len(self.early_pages) else self.releases
            return self.response(receive, 200, rows)
        if path == H.PREFIX+'/releases' and method == 'POST':
            value = H.load(data)
            assert value['draft'] is True and value['make_latest'] == 'false'
            assert value['target_commitish'] == PRODUCER
            row = {**value, 'id': 91, 'published_at': None, 'author': {'id': BOT}, 'assets': [],
                'url': H.API+H.PREFIX+'/releases/91',
                'upload_url': 'https://uploads.github.com'+H.PREFIX+'/releases/91/assets{?name,label}'}
            if self.bad_upload_host:
                row['upload_url'] = row['upload_url'].replace('uploads.github.com', 'invalid.example')
            self.releases.append(row)
            if self.created_tag: self.tag = PRODUCER
            if self.create_timeout: raise TimeoutError('synthetic timeout; message must not be copied')
            return self.response(receive, 201, row)
        if path == H.PREFIX+'/releases/91':
            return self.response(receive, 200 if authenticated else self.anonymous_release,
                                 self.releases[0] if authenticated else None)
        if path == H.PREFIX+'/releases/91/assets' and method == 'POST':
            body = b''.join(iter(lambda: data.read(65536), b'')); name = urllib.parse.parse_qs(parsed.query)['name'][0]
            identity = 500+len(self.bodies)
            row = {'id': identity, 'name': name, 'state': 'uploaded', 'size': len(body),
                'uploader': {'id': BOT}, 'digest': 'sha256:'+H.sha(body),
                'url': H.API+H.PREFIX+'/releases/assets/'+str(identity),
                'browser_download_url': 'https://github.com/'+H.REPO+'/releases/download/untagged_candidate/'+name}
            if self.bad_digest: row['digest'] = 'sha256:'+'0'*64
            if self.wrong_size: row['size'] += 1
            self.bodies[identity] = body; self.releases[0]['assets'].append(row)
            if self.upload_timeout: raise TimeoutError('synthetic timeout')
            return self.response(receive, 201, row)
        if path.startswith(H.PREFIX+'/releases/assets/'):
            identity = int(path.rsplit('/', 1)[1])
            if not authenticated:
                return self.response(receive, self.anonymous_asset)
            if headers.get('Accept') == 'application/octet-stream':
                if self.redirect:
                    return self.response(receive, 302, headers={'Location': self.redirect})
                body = b'corrupt' if self.corrupt_download else self.bodies[identity]
                return self.response(receive, 200, raw=body, headers={'Content-Length': str(len(body))})
            row = next(a for a in self.releases[0]['assets'] if a['id'] == identity)
            return self.response(receive, 200, row)
        raise AssertionError('Unsupported fake route: '+method+' '+path)


class PublisherTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='ur-byte-offline-')
        self.root = Path(self.temp.name); self.transport = FakeHTTP()
        self.api = H.GitHub('SYNTHETIC_NOT_A_CREDENTIAL', self.transport)
        self.draft = H.Draft(self.api, self.root, PRODUCER, '123')

    def tearDown(self):
        self.temp.cleanup()

    def posts(self):
        return [c for c in self.transport.calls if c['method'] == 'POST']

    def marker(self):
        self.draft.create()
        path = self.root/H.MARKER; H.write_new(path, b'payload')
        self.draft.upload(path)

    def test_complete_native_draft_asset_cycle_and_closed_set(self):
        self.marker()
        for name in H.ASSETS:
            path = self.root/name; H.write_new(path, b'payload')
            self.draft.upload(path)
        self.draft.fresh()
        self.assertEqual(len(self.posts()), 7)
        self.assertEqual(set(self.draft.expected), set(H.ASSETS+(H.MARKER,)))
        self.assertTrue(self.draft.marker_verified)
        self.assertTrue(all(c['method'] in ('GET', 'POST') for c in self.transport.calls))
        self.assertEqual(self.draft.tag_before, {'present': False})
        self.assertFalse(self.draft.tag_after['present'])

    def test_actual_created_tag_is_recorded_not_assumed_absent(self):
        self.transport.created_tag = True; self.marker()
        self.assertEqual(self.draft.tag_after['commit'], PRODUCER)
        self.assertTrue(self.draft.effects[0]['create_readback']['present'])

    def test_repo_id_and_head_drift_prevent_create(self):
        for field, bad in (('repo_id', 1), ('head', 'b'*40)):
            with self.subTest(field=field):
                setattr(self.transport, field, bad)
                with self.assertRaises(H.Stop): self.draft.create()
                self.assertFalse(self.posts())
                setattr(self.transport, field, H.REPO_ID if field == 'repo_id' else PRODUCER)

    def test_existing_tag_or_second_page_draft_prevents_create(self):
        self.transport.tag = PRODUCER
        with self.assertRaises(H.Stop): self.draft.create()
        self.assertFalse(self.posts()); self.transport.tag = None
        self.transport.early_pages = [[{'tag_name': 'unrelated'}]*100,
                                     [{'tag_name': self.draft.tag, 'id': 91}]]
        with self.assertRaises(H.Stop): self.draft.create()
        self.assertFalse(self.posts())

    def test_create_uncertain_outcome_inspected_without_second_write(self):
        self.transport.create_timeout = True
        with self.assertRaises(H.Stop): self.draft.create()
        self.assertEqual(len(self.posts()), 1)
        self.assertTrue(self.draft.blocked)
        self.assertEqual(self.draft.effects[-1]['matching_draft_ids'], [91])
        with self.assertRaises(H.Stop): self.draft.create()
        self.assertEqual(len(self.posts()), 1)

    def test_existing_intent_never_reexecutes(self):
        H.write_new(self.root/'intent-create.json', b'{}')
        with self.assertRaises(H.Stop): self.draft.create()
        self.assertFalse(self.posts())

    def test_native_upload_host_author_and_publication_are_checked(self):
        self.transport.bad_upload_host = True
        with self.assertRaises(H.Stop): self.draft.create()
        self.assertEqual(len(self.posts()), 1)
        row = copy.deepcopy(self.transport.releases[0]); row['upload_url'] = (
            'https://uploads.github.com'+H.PREFIX+'/releases/91/assets{?name,label}')
        for field, value in (('published_at', '2026-10-17T00:00:00Z'), ('draft', False),
                             ('author', {'id': 7}), ('target_commitish', H.PRODUCT)):
            with self.subTest(field=field):
                changed = copy.deepcopy(row); changed[field] = value
                with self.assertRaises(H.Stop): self.draft.validate(changed)

    def test_marker_audience_failure_never_uploads_real_bytes(self):
        for status in (200, 403, 429):
            with self.subTest(status=status), tempfile.TemporaryDirectory() as tmp:
                self.transport = FakeHTTP(); self.transport.anonymous_asset = status
                draft = H.Draft(H.GitHub('SYNTHETIC', self.transport), Path(tmp), PRODUCER, '123')
                draft.create(); p = Path(tmp)/H.MARKER; H.write_new(p, b'payload')
                with self.assertRaises(H.Stop): draft.upload(p)
                self.assertFalse(draft.marker_verified); self.assertTrue(draft.blocked)
                self.assertEqual(len([c for c in self.transport.calls if c['method'] == 'POST']), 2)

    def test_anonymous_release_denial_required_and_fresh_before_each_asset(self):
        self.marker(); self.transport.anonymous_release = 200
        p = self.root/H.ASSETS[0]; H.write_new(p, b'payload')
        with self.assertRaises(H.Stop): self.draft.upload(p)
        self.assertEqual(len(self.posts()), 2)

    def test_main_drift_after_marker_prevents_binary_upload(self):
        self.marker(); self.transport.head = 'b'*40
        p = self.root/H.ASSETS[0]; H.write_new(p, b'payload')
        with self.assertRaises(H.Stop): self.draft.upload(p)
        self.assertEqual(len(self.posts()), 2)

    def test_foreign_asset_or_name_collision_prevents_write(self):
        self.marker()
        with self.assertRaises(H.Stop): self.draft.upload(self.root/H.MARKER)
        self.transport.releases[0]['assets'].append({'name': 'foreign', 'id': 900})
        with self.assertRaises(H.Stop): self.draft.fresh()
        self.assertEqual(len(self.posts()), 2)

    def test_browser_url_native_name_and_first_returned_identity_are_bound(self):
        self.marker()
        row = copy.deepcopy(self.draft.expected[H.MARKER]['row'])
        self.assertIn('/untagged_candidate/', row['browser_download_url'])
        for replacement in ('another-tag/'+H.MARKER, self.draft.tag+'/another-file.txt',
                            self.draft.tag+'/'+H.MARKER):
            changed = dict(row)
            changed['browser_download_url'] = 'https://github.com/'+H.REPO+'/releases/download/'+replacement
            with self.subTest(replacement=replacement), self.assertRaises(H.Stop):
                self.draft.asset_identity(changed, H.MARKER, self.draft.expected[H.MARKER]['fact'])
        changed = dict(row); changed['id'] += 1
        changed['url'] = H.API+H.PREFIX+'/releases/assets/'+str(changed['id'])
        with self.assertRaises(H.Stop):
            self.draft.asset_identity(changed, H.MARKER, self.draft.expected[H.MARKER]['fact'])

    def test_bad_size_server_digest_or_download_stops_once(self):
        for field in ('wrong_size', 'bad_digest', 'corrupt_download'):
            with self.subTest(field=field), tempfile.TemporaryDirectory() as tmp:
                transport = FakeHTTP(); setattr(transport, field, True)
                draft = H.Draft(H.GitHub('SYNTHETIC', transport), Path(tmp), PRODUCER, '123')
                draft.create(); p = Path(tmp)/H.MARKER; H.write_new(p, b'payload')
                with self.assertRaises(H.Stop): draft.upload(p)
                self.assertTrue(draft.blocked)
                self.assertEqual(len([c for c in transport.calls if c['method'] == 'POST']), 2)

    def test_upload_timeout_after_server_created_asset_no_retry(self):
        self.draft.create(); self.transport.upload_timeout = True
        path = self.root/H.MARKER; H.write_new(path, b'payload')
        with self.assertRaises(H.Stop): self.draft.upload(path)
        self.assertEqual(self.draft.effects[-1]['matching_asset_names'], [[H.MARKER]])
        with self.assertRaises(H.Stop): self.draft.upload(path)
        self.assertEqual(len(self.posts()), 2)

    def test_signed_download_drops_auth_and_never_records_url(self):
        self.marker(); self.transport.redirect = (
            'https://release-assets.githubusercontent.com/signed?secret=DO_NOT_RETAIN')
        p = self.root/'download-copy'
        self.assertEqual(self.api.download(self.draft.expected[H.MARKER]['row']['url'], p),
                         {'bytes': 7, 'sha256': H.sha(b'payload')})
        self.assertFalse(self.transport.calls[-1]['auth'])
        self.assertNotIn('DO_NOT_RETAIN', json.dumps(self.api.observations))
        self.assertNotIn('SYNTHETIC_NOT_A_CREDENTIAL', json.dumps(self.api.observations))

    def test_untrusted_signed_redirects_rejected(self):
        self.marker()
        for url in ('http://release-assets.githubusercontent.com/x', 'https://evil.example/x',
                    'https://user:password@release-assets.githubusercontent.com/x',
                    'https://release-assets.githubusercontent.com:8443/x',
                    'https://release-assets.githubusercontent.com/x#fragment'):
            with self.subTest(url=url):
                self.transport.redirect = url
                with self.assertRaises(H.Stop):
                    self.api.download(self.draft.expected[H.MARKER]['row']['url'], self.root/'absent')
                self.assertFalse((self.root/'absent').exists())

    def test_asset_limit_checked_before_upload(self):
        self.marker(); p = self.root/H.ASSETS[0]; H.write_new(p, b'12345678')
        with patch.object(H, 'ASSET_LIMIT', 10):
            with self.assertRaises(H.Stop): self.draft.upload(p)
        self.assertEqual(len(self.posts()), 2)


class FrontendEvidenceBoundaryTests(unittest.TestCase):
    """Synthetic filesystem/stream fixtures; real inventory, receiver and validator."""
    def setUp(self):
        self.reference = H.module('boundary_image_reference', HERE/'community-image-candidate.py')
        self.helper = H.module('boundary_source_observer', HERE/'community-source-evidence.py')

    @staticmethod
    def archive(files):
        out = io.BytesIO()
        with tarfile.open(fileobj=out, mode='w') as tf:
            for name, body in files.items():
                row = tarfile.TarInfo(name); row.size = len(body)
                tf.addfile(row, io.BytesIO(body))
        return out.getvalue()

    def membership_files(self):
        body = b'Synthetic boundary-test notice.\n'
        value = {
            'status': 'actual_build_graph_and_output_observations_not_license_clearance',
            'source_commit': H.PRODUCT, 'actual_vite_version': '6.4.3', 'actual_rollup_version': '4.62.0',
            'original_source_lock_sha256': self.helper.ORIGINAL_LOCK_SHA,
            'original_package_json_sha256': self.helper.ORIGINAL_PACKAGE_SHA,
            'post_install_package_lock_sha256': 'a'*64,
            'post_install_package_json_sha256': 'b'*64, 'installed_hidden_lock_sha256': 'c'*64,
            'plugin_order': [], 'graph': [], 'watch_inputs': [], 'emitted': [],
            'output_files': [], 'public_copies': [], 'unexplained_output_files': [],
            'packages': [{'location': 'node_modules/boundary-fixture', 'name': 'boundary-fixture',
                'version': '1.0.0', 'declared_license': 'MIT', 'license_metadata_kind': 'string',
                'unsupported_license_sha256': None, 'source_lock_version': '1.0.0',
                'installed_lock_version': '1.0.0', 'exact_lock_version_match': True,
                'reasons': ['build_tool_notice_candidate'], 'notices': [{
                    'file': 'npm-notices/0001-LICENSE', 'source': 'node_modules/boundary-fixture/LICENSE',
                    'bytes': len(body), 'sha256': H.sha(body)}]}],
            'limits': self.helper.OBSERVER_LIMITS, 'limits_of_claim': self.helper.CLAIM_LIMITS,
        }
        return {'output/frontend-membership.json': H.encoded(value),
                'output/npm-notices/0001-LICENSE': body}

    def inventory_first(self, root, outputs):
        data = root/'data'; data.mkdir()
        inventory = self.archive({
            'lib/apk/db/installed': b'P:boundary-fixture\nV:1\nL:\n\n',
            'usr/share/licenses/NOTICE': b'Synthetic image notice.\n',
        })
        output = self.archive(outputs)
        calls = []
        def stream(label, args, consume):
            calls.append(label)
            if label == 'frontend stopped byte inventory': body = inventory
            elif label == 'actual final frontend membership': body = output
            else: raise AssertionError('unexpected command phase')
            return consume(io.BytesIO(body))
        guard = types.SimpleNamespace(work=root, create=lambda *args: 'stopped-frontend',
            time_guard=lambda: None, apk_packages=self.reference.apk_packages, stream_command=stream)
        cid, binary, report = H.inventory(guard, self.helper, 'frontend', 'sha256:'+'1'*64, data)
        self.assertIsNone(binary)
        return guard, cid, data, report, calls

    def test_frontend_inventory_then_membership_keeps_both_closed_sets(self):
        outputs = self.membership_files()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            guard, cid, data, report, calls = self.inventory_first(root, outputs)
            inventory_files = {p.relative_to(data).as_posix(): H.file_fact(p)
                               for p in data.rglob('*') if p.is_file()}
            self.assertTrue((data/'frontend/databases/lib/apk/db/installed').is_file())
            self.assertEqual(report['apk'][0]['license'], '')
            self.assertTrue((data/report['notice_filename_candidates'][0]['retained_text']).is_file())
            member = H.collect_frontend_membership(guard, self.helper, cid, data)
            self.assertEqual(member, H.load(outputs['output/frontend-membership.json']))
            for name, fact in inventory_files.items(): self.assertEqual(H.file_fact(data/name), fact)
            copied = {p.relative_to(data/'frontend-membership').as_posix(): p.read_bytes()
                      for p in (data/'frontend-membership').rglob('*') if p.is_file()}
            self.assertEqual(copied, {name.removeprefix('output/'): body for name, body in outputs.items()})
            self.assertEqual(calls, ['frontend stopped byte inventory', 'actual final frontend membership'])

    def test_frontend_membership_still_rejects_missing_or_changed_notice(self):
        for change in ('missing', 'changed'):
            with self.subTest(change=change), tempfile.TemporaryDirectory() as tmp:
                outputs = self.membership_files()
                if change == 'missing': del outputs['output/npm-notices/0001-LICENSE']
                else: outputs['output/npm-notices/0001-LICENSE'] += b'tampered'
                guard, cid, data, _, _ = self.inventory_first(Path(tmp), outputs)
                with self.assertRaises(self.helper.Rejected):
                    H.collect_frontend_membership(guard, self.helper, cid, data)

    def test_frontend_membership_rejects_foreign_output_and_existing_destination(self):
        with tempfile.TemporaryDirectory() as tmp:
            outputs = self.membership_files(); outputs['output/lib/apk/db/installed'] = b'foreign inventory'
            guard, cid, data, _, _ = self.inventory_first(Path(tmp), outputs)
            with self.assertRaises(self.helper.Rejected):
                H.collect_frontend_membership(guard, self.helper, cid, data)
        with tempfile.TemporaryDirectory() as tmp:
            guard, cid, data, _, calls = self.inventory_first(Path(tmp), self.membership_files())
            H.write_new(data/'frontend-membership/existing', b'preserve')
            with self.assertRaises(FileExistsError):
                H.collect_frontend_membership(guard, self.helper, cid, data)
            self.assertEqual((data/'frontend-membership/existing').read_bytes(), b'preserve')
            self.assertEqual(calls, ['frontend stopped byte inventory'])


class LocalInputTests(unittest.TestCase):
    def test_actual_stage_module_and_main_dispatch_reach_only_validated_boundary(self):
        stages = H.module('real_stage_contract', HERE/'community-stage-record.py')
        fixture = types.ModuleType('fixture_stage_data')
        fixture.__file__ = str(HERE/'community-stage-fixtures.py')
        exec(compile(Path(fixture.__file__).read_bytes(), fixture.__file__, 'exec'), fixture.__dict__)
        class PublicEnvironment(dict):
            def get(self, key, *default):
                if key == 'GITHUB_TOKEN': raise AssertionError('token read before validation')
                return super().get(key, *default)
            def pop(self, key, *default):
                if key == 'GITHUB_TOKEN': raise AssertionError('token exposed before validation')
                return super().pop(key, *default)
            def __getitem__(self, key):
                if key == 'GITHUB_TOKEN': raise AssertionError('token copied before validation')
                return super().__getitem__(key)
        for invalid in (False, True):
            with self.subTest(invalid=invalid), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp); event = root/'event.json'; record = fixture.producer()
                inputs = dict(stages.encode(record), expected_workflow_commit=PRODUCER, admission_sha256='d'*64)
                if invalid: inputs['stage_record_sha256'] = '0'*64
                H.write_new(event, H.encoded({'repository': {'id': H.REPO_ID, 'full_name': H.REPO},
                    'sender': {'id': H.ACTOR_ID}, 'inputs': inputs}))
                env = PublicEnvironment({'RUNNER_TEMP': tmp, 'GITHUB_RUN_ID': '123', 'GITHUB_EVENT_PATH': str(event),
                    'EXPECTED_WORKFLOW_COMMIT': PRODUCER, 'GITHUB_SHA': PRODUCER,
                    'GITHUB_WORKFLOW_SHA': PRODUCER, 'GITHUB_REPOSITORY': H.REPO,
                    'GITHUB_REPOSITORY_ID': str(H.REPO_ID), 'GITHUB_EVENT_NAME': 'workflow_dispatch',
                    'GITHUB_REF': 'refs/heads/main', 'GITHUB_RUN_ATTEMPT': '1', 'GITHUB_ACTOR_ID': str(H.ACTOR_ID),
                    'GITHUB_WORKFLOW_REF': H.REPO+'/'+H.WORKFLOW+'@refs/heads/main',
                    'RUNNER_ENVIRONMENT': 'github-hosted', 'RUNNER_OS': 'Linux', 'RUNNER_ARCH': 'X64',
                    'ADMISSION_SHA256': inputs['admission_sha256'], 'STAGE_RECORD_B64': inputs['stage_record_b64'],
                    'STAGE_RECORD_SHA256': inputs['stage_record_sha256'], 'GITHUB_TOKEN': 'SYNTHETIC'})
                with patch.object(os, 'environ', env), patch.object(sys, 'argv', [H.__file__]), \
                     patch.object(H, 'bound_inputs', side_effect=H.Stop('validated_record_boundary')) as boundary, \
                     patch.object(H, 'GitHub', side_effect=AssertionError('external client before validation')) as client, \
                     patch.object(H, 'initialize_guard', side_effect=AssertionError('tool owner before validation')) as tools, \
                     patch.object(subprocess, 'run', side_effect=AssertionError('subprocess before validation')), \
                     patch.object(H.urllib.request, 'urlopen', side_effect=AssertionError('network before validation')):
                    self.assertEqual(1, H.main())
                self.assertEqual(0 if invalid else 1, boundary.call_count)
                client.assert_not_called(); tools.assert_not_called()
                self.assertFalse((root/'ur-byte-candidate-123-attempt1').exists())
                terminal = H.load(H.regular(H.terminal_path(env), 64*1024))
                self.assertEqual('Stop' if invalid else 'validated_record_boundary', terminal['error'])

    def test_stage_program_digest_is_required_before_loading(self):
        with tempfile.TemporaryDirectory() as tmp:
            changed = Path(tmp)/'community-stage-record.py'
            changed.write_bytes((HERE/changed.name).read_bytes()+b'\n# unreviewed change\n')
            with self.assertRaisesRegex(H.Stop, 'observer_identity'):
                H.module('tampered_stage', changed)

    def test_notice_reuse_keeps_historical_observation_separate_from_current_product(self):
        _, index = H.bound_inputs()
        self.assertEqual(index['product_source']['commit'], H.PRODUCT)
        self.assertNotEqual(index['observed_evidence_source']['commit'], H.PRODUCT)
        reuse = index['dependency_material_reuse']
        self.assertFalse(reuse['new_product_image_emission_observed'])
        self.assertTrue(reuse['fresh_native_image_and_frontend_membership_required'])
        self.assertEqual(reuse['baseline_notice_archive_sha256'], '1c82db604fe54e6a076723374e80cbf066b55d46b6880b6b630237324fb3913a')
        for name in ('go.mod', 'go.sum'):
            self.assertEqual(reuse['dependency_files'][name]['historical_sha256'], reuse['dependency_files'][name]['current_sha256'])
        for name in ('frontend/package.json', 'frontend/package-lock.json'):
            self.assertEqual(reuse['dependency_files'][name]['metadata_difference'], 'project version only')
        self.assertFalse(index['complete_license_clearance'])

    def test_reused_guard_failure_reaches_partial_archive_and_saved_public_summary(self):
        reference = H.module('accepted_diagnostic_reference', HERE/'community-image-candidate.py')
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); event = root/'event.json'
            H.write_new(event, H.encoded({'repository': {'id': H.REPO_ID, 'full_name': H.REPO},
                                         'sender': {'id': H.ACTOR_ID}}))
            env = {'RUNNER_TEMP': tmp, 'GITHUB_RUN_ID': '123', 'GITHUB_EVENT_PATH': str(event),
                'EXPECTED_WORKFLOW_COMMIT': PRODUCER, 'GITHUB_SHA': PRODUCER,
                'GITHUB_WORKFLOW_SHA': PRODUCER, 'GITHUB_REPOSITORY': H.REPO,
                'GITHUB_REPOSITORY_ID': str(H.REPO_ID), 'GITHUB_EVENT_NAME': 'workflow_dispatch',
                'GITHUB_REF': 'refs/heads/main', 'GITHUB_RUN_ATTEMPT': '1', 'GITHUB_ACTOR_ID': str(H.ACTOR_ID),
                'GITHUB_WORKFLOW_REF': H.REPO+'/'+H.WORKFLOW+'@refs/heads/main',
                'RUNNER_ENVIRONMENT': 'github-hosted', 'RUNNER_OS': 'Linux', 'RUNNER_ARCH': 'X64',
                'ADMISSION_SHA256': 'd'*64, 'GITHUB_TOKEN': 'SYNTHETIC'}
            guard = types.SimpleNamespace(work=None, owned=[], time_guard=lambda: None,
                cleanup=lambda: None, receipt={'commands': [{'phase': 'one retained backend build',
                    'status': 'failed', 'exit_code': 2}], 'cleanup': []})
            transport = FakeHTTP(); client = H.GitHub
            def initialize(g, work, env, stage_record=None): g.work = work
            stages = H.module('fixture_stage_contracts', HERE/'community-stage-record.py')
            fixture = types.ModuleType('fixture_stage_data'); fixture.__file__=str(HERE/'community-stage-fixtures.py')
            exec(compile(Path(fixture.__file__).read_bytes(),fixture.__file__,'exec'),fixture.__dict__)
            record = fixture.producer()
            def bind(subject,files,images): guard.FIXED={'source':subject,'source_files':files,'images':images}
            guard.configure_stage_inputs=bind
            def modules(name,path):
                if name=='community_stage_contract': return types.SimpleNamespace(read_dispatch=lambda stage:(stages.canonical(record),record,{}))
                if name=='accepted_byte_guards': return guard
                if name=='accepted_source_observer': return types.SimpleNamespace(configure_stage_inputs=lambda fixed:None)
                return None
            def fail(*args): reference.require(False, 'DO_NOT_RETAIN /private/path https://signed.example/?secret=x')
            with patch.dict(os.environ, env, clear=True), patch.object(sys, 'argv', [H.__file__]), \
                 patch.object(H, 'initialize_guard', side_effect=initialize), \
                 patch.object(H, 'frontend_input_preflight', return_value=None), \
                 patch.object(H, 'bound_inputs', return_value=('synthetic-ring',{})), \
                 patch.object(H, 'prepare_source_inputs', return_value=None), \
                 patch.object(H, 'module', side_effect=modules), \
                 patch.object(H, 'GitHub', side_effect=lambda token, guard: client(token, transport)), \
                 patch.object(H, 'build', side_effect=fail):
                self.assertEqual(H.main(), 1)
            terminal = H.load(H.regular(H.terminal_path(env), 64*1024))
            diagnostic = terminal['diagnostic']
            self.assertEqual(diagnostic['exception_class'], 'Stop')
            self.assertTrue(any(row['module_role'] == 'accepted_image_guard' for row in diagnostic['locations']))
            self.assertEqual(diagnostic['last_owned_command']['phase'], 'one retained backend build')
            self.assertEqual(diagnostic['last_owned_command']['exit_code'], 2)
            asset = next(a for a in transport.releases[0]['assets'] if a['name'] == 'candidate-evidence.tar.gz')
            with tarfile.open(fileobj=io.BytesIO(transport.bodies[asset['id']])) as tf:
                partial = json.load(tf.extractfile('partial-terminal.json'))
            self.assertEqual(partial['diagnostic'], diagnostic)
            raw = H.safe_terminal(terminal)
            self.assertNotIn(b'DO_NOT_RETAIN', raw)
            self.assertNotIn(b'signed.example', raw)
            self.assertNotIn(b'/private/path', H.encoded(partial))
            self.assertEqual(H.safe_terminal(H.load(raw)), raw)

    def test_numeric_errno_and_reused_observer_location_without_exception_text(self):
        error = PermissionError(13, 'DO_NOT_RETAIN credentials or filename')
        diagnostic = H.failure_diagnostic(error)
        self.assertEqual(diagnostic['errno'], 13)
        self.assertNotIn('DO_NOT_RETAIN', H.encoded(diagnostic).decode())
        helper = H.module('accepted_observer_diagnostic', HERE/'community-source-evidence.py')
        try: helper.need(False, 'DO_NOT_RETAIN')
        except Exception as error: diagnostic = H.failure_diagnostic(error)
        self.assertEqual(diagnostic['locations'][-1]['module_role'], 'accepted_source_observer')
        H.validate_diagnostic(diagnostic)

    def test_recipe_derivation_keeps_served_paths_and_carries_notice_materials(self):
        guard = H.module('accepted_recipe_reference', HERE/'community-image-candidate.py')
        with tempfile.TemporaryDirectory() as tmp:
            source = Path(tmp)/'source'; source.mkdir(); front = source/'frontend'; front.mkdir()
            work = Path(tmp)/'work'; work.mkdir(); guard.work = work
            H.write_new(source/'Dockerfile', b'FROM golang:1.25.12-alpine AS builder\n'
                b'FROM alpine:3.23\nCOPY migrations/ ./migrations/\nCMD ["./uniroute"]\n')
            H.write_new(front/'Dockerfile', ('FROM node:24.19.0-alpine3.23@'+
                guard.FIXED['images']['frontend-builder']['index_digest']+' AS builder\n'
                'RUN npm run build\n# Production stage\nFROM nginx:1.30.3-alpine\n'
                'COPY --from=builder /app/dist /usr/share/nginx/html\n'
                'COPY nginx.conf /etc/nginx/conf.d/default.conf\n').encode())
            for name in ('LICENSE', 'NOTICE'): H.write_new(source/name, b'synthetic test notice')
            for name in ('package.json', 'package-lock.json'): H.write_new(front/name, b'{}')
            back, frontend, evidence = H.derive_recipes(guard, source)
            self.assertIn('COPY --from=builder /app/dist /usr/share/nginx/html', frontend.read_text())
            self.assertIn('COPY .ur-byte-inputs/ /usr/share/licenses/unirouter/', back.read_text())
            self.assertIn('COPY --from=builder /app/.ur-source-evidence/output /usr/share/licenses/unirouter/output', frontend.read_text())
            self.assertEqual(frontend.read_text().count('./node_modules/.bin/vite build '), 1)
            self.assertNotIn('/usr/share/nginx/html/licenses', frontend.read_text())
            self.assertEqual(H.file_fact(source/'.ur-byte-inputs/component-notices.tar.gz'),
                             H.file_fact(H.INPUTS/'component-notices.tar.gz'))
            self.assertTrue(evidence['not_claimed_byte_equal_to_old_ephemeral_images'])

    def test_stopped_inventory_retains_actual_notice_and_blank_APK_without_following_links(self):
        reference = H.module('accepted_inventory_reference', HERE/'community-image-candidate.py')
        helper = H.module('accepted_inventory_receiver', HERE/'community-source-evidence.py')
        out = io.BytesIO()
        with tarfile.open(fileobj=out, mode='w') as tf:
            for name, body in [('app/uniroute', b'ELF-test-binary'), ('usr/share/licenses/LICENSE', b'copyright test'),
                ('lib/apk/db/installed', b'P:.redis-rundeps\nV:1\nL:\nU:https://example.test/source\n\n'),
                ('app/notice.js', b'not notice text')]:
                row = tarfile.TarInfo(name); row.size = len(body); tf.addfile(row, io.BytesIO(body))
            link = tarfile.TarInfo('app/foreign-link'); link.type = tarfile.SYMTYPE
            link.linkname = '../../../../outside'; tf.addfile(link)
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); data = root/'data'; data.mkdir()
            fake = types.SimpleNamespace(work=root, create=lambda *args: 'stopped-cid',
                time_guard=lambda: None, apk_packages=reference.apk_packages,
                stream_command=lambda label, args, consume: consume(io.BytesIO(out.getvalue())))
            cid, binary, report = H.inventory(fake, helper, 'backend', 'sha256:'+'1'*64, data)
            self.assertEqual(cid, 'stopped-cid'); self.assertEqual(binary.stat().st_mode & 0o777, 0o644)
            self.assertEqual(report['apk'][0]['license'], '')
            self.assertIsNone(report['apk'][0]['origin'])
            self.assertEqual(report['apk'][0]['upstream_url'], 'https://example.test/source')
            self.assertEqual(len(report['notice_filename_candidates']), 1)
            notice = report['notice_filename_candidates'][0]
            self.assertEqual((data/notice['retained_text']).read_bytes(), b'copyright test')
            self.assertEqual(report['entries'][-1]['target'], '../../../../outside')
            self.assertFalse((root/'outside').exists())

    def test_stopped_inventory_rejects_duplicate_and_traversal_names(self):
        helper = H.module('accepted_bad_inventory_receiver', HERE/'community-source-evidence.py')
        for names in (['safe', 'safe'], ['../outside']):
            out = io.BytesIO()
            with tarfile.open(fileobj=out, mode='w') as tf:
                for name in names:
                    row = tarfile.TarInfo(name); row.size = 1; tf.addfile(row, io.BytesIO(b'x'))
            with self.subTest(names=names), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                fake = types.SimpleNamespace(work=root, create=lambda *args: 'stopped-cid',
                    time_guard=lambda: None, apk_packages=lambda _: [],
                    stream_command=lambda label, args, consume: consume(io.BytesIO(out.getvalue())))
                with self.assertRaises(H.Stop): H.inventory(fake, helper, 'frontend', 'sha256:'+'1'*64, root/'data')

    def test_bound_inputs_and_duplicate_json_rejection(self):
        ring, index = H.bound_inputs()
        self.assertEqual(len(ring), 111)
        self.assertEqual(len(index['components']), 318)
        with self.assertRaises(H.Stop): H.load(b'{"x":1,"x":2}')

    def test_input_tamper_and_symlink_are_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for name in H.BOUND:
                source = (H.INPUTS if name.endswith(('.gz', '.json')) else HERE)/name
                target = root/'.github'/('release-inputs' if name.endswith(('.gz', '.json')) else 'tests')/name
                H.write_new(target, source.read_bytes())
            ring = root/'.github/release-inputs/community-public-keys.json'
            ring.write_bytes(b'{}')
            with self.assertRaises(H.Stop): H.bound_inputs(root)
            ring.unlink(); ring.symlink_to(H.INPUTS/'community-public-keys.json')
            with self.assertRaises(H.Stop): H.bound_inputs(root)

    def test_metadata_closed_set_contains_actual_bytes_and_rejects_link(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); data = root/'data'; data.mkdir()
            H.write_new(data/'notice.txt', b'actual notice')
            output = root/'evidence.tar.gz'; H.metadata_archive(data, output)
            with tarfile.open(output) as tf:
                manifest = json.load(tf.extractfile('closed-file-manifest.json'))
                self.assertEqual(manifest['files']['notice.txt']['sha256'], H.sha(b'actual notice'))
                self.assertEqual(set(tf.getnames()), {'notice.txt', 'closed-file-manifest.json'})
            (data/'link').symlink_to(data/'notice.txt')
            with self.assertRaises(H.Stop): H.metadata_archive(data, root/'invalid.tar.gz')

    def test_metadata_limit_is_32MiB_and_checked_before_packaging(self):
        self.assertEqual(H.ASSET_LIMIT, 1024**3); self.assertEqual(H.METADATA_LIMIT, 32*1024**2)
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); data = root/'data'; data.mkdir()
            with (data/'oversized').open('wb') as out: out.truncate(29*1024**2)
            with self.assertRaises(H.Stop): H.metadata_archive(data, root/'not-created.tar.gz')
            self.assertFalse((root/'not-created.tar.gz').exists())

    def test_actor_attempt_and_producer_are_bound(self):
        env = {'EXPECTED_WORKFLOW_COMMIT': PRODUCER, 'GITHUB_SHA': PRODUCER,
            'GITHUB_WORKFLOW_SHA': PRODUCER, 'GITHUB_REPOSITORY': H.REPO,
            'GITHUB_REPOSITORY_ID': str(H.REPO_ID), 'GITHUB_EVENT_NAME': 'workflow_dispatch',
            'GITHUB_REF': 'refs/heads/main', 'GITHUB_RUN_ATTEMPT': '1',
            'GITHUB_ACTOR_ID': str(H.ACTOR_ID), 'GITHUB_WORKFLOW_REF': H.REPO+'/'+H.WORKFLOW+'@refs/heads/main',
            'RUNNER_ENVIRONMENT': 'github-hosted', 'RUNNER_OS': 'Linux', 'RUNNER_ARCH': 'X64',
            'ADMISSION_SHA256': 'd'*64}
        event = {'repository': {'id': H.REPO_ID, 'full_name': H.REPO}, 'sender': {'id': H.ACTOR_ID}}
        self.assertEqual(H.runtime_identity(env, event), PRODUCER)
        for key, bad in (('GITHUB_RUN_ATTEMPT', '2'), ('GITHUB_ACTOR_ID', '7'),
                         ('GITHUB_WORKFLOW_SHA', 'b'*40), ('DOCKER_HOST', 'tcp://foreign')):
            changed = dict(env); changed[key] = bad
            with self.subTest(key=key), self.assertRaises(H.Stop): H.runtime_identity(changed, event)

    def test_summary_closed_projection_never_starts_tools_or_network(self):
        with tempfile.TemporaryDirectory() as tmp:
            env = {'RUNNER_TEMP': tmp, 'GITHUB_RUN_ID': '123'}
            value = {'status': 'stopped_partial_candidate', 'run_id': '123', 'producer': PRODUCER,
                'error': 'TimeoutError', 'draft_id': 91, 'assets': [], 'owned_cleanup_complete': True,
                'secret_unrecognized_field': 'DO_NOT_PROJECT',
                'remote_outcome': {'writes_blocked': True, 'matching_draft_ids': [91],
                    'tag_before': {'present': False}, 'tag_after': {'present': True, 'commit': PRODUCER}}}
            projected = H.safe_terminal(value)
            self.assertEqual(H.safe_terminal(H.load(projected)), projected)
            H.write_new(H.terminal_path(env), projected)
            out = io.StringIO()
            with patch.object(H, 'GitHub', side_effect=AssertionError('network forbidden')), \
                 patch.object(H, 'module', side_effect=AssertionError('tool loading forbidden')), \
                 H.contextlib.redirect_stdout(out):
                H.summary(env)
            self.assertNotIn('DO_NOT_PROJECT', out.getvalue())
            self.assertEqual(H.load(out.getvalue())['draft_id'], 91)
            self.assertEqual(H.load(out.getvalue())['remote_outcome']['tag_observations'],
                {'before': {'present': False, 'commit': None}, 'after': {'present': True, 'commit': PRODUCER}})
            cli = subprocess.run([sys.executable, '-I', '-B', H.__file__, '--summary'],
                env={'RUNNER_TEMP': tmp, 'GITHUB_RUN_ID': '123'}, capture_output=True, check=True)
            self.assertEqual(H.load(cli.stdout), H.load(projected))

    def test_static_workflow_no_publish_registry_artifact_or_service_actions(self):
        workflow = (H.ROOT/H.WORKFLOW).read_text()
        self.assertIn('workflow_dispatch:', workflow)
        self.assertIn('contents: write', workflow)
        self.assertIn('timeout-minutes: 45', workflow)
        self.assertEqual(workflow.count('persist-credentials: false'), 2)
        for forbidden in ('upload-artifact', 'schedule:', 'pull_request:', 'push:', 'secrets.GH',
                          'docker login', 'docker push', 'compose up', '--admin'):
            self.assertNotIn(forbidden, workflow)
        tree = ast.parse(Path(H.__file__).read_text())
        literals = [n.value for n in ast.walk(tree) if isinstance(n, ast.Constant) and isinstance(n.value, str)]
        self.assertNotIn('PATCH', literals); self.assertNotIn('DELETE', literals)
        self.assertNotIn('PUT', literals)
        self.assertNotIn('publish', literals)


class ActualFrontendBindingTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        # Current stage inputs come from the actual source tree. A new shallow
        # checkout need not fetch a historical product commit to run these tests.
        cls.originals={name:H.regular(H.ROOT/'frontend'/name,H.MIB) for name in ('package.json','package-lock.json')}

    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(prefix='ur-stage-frontend-binding-');self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name);self.source=self.root/'source'
        for name,raw in self.originals.items():H.write_new(self.source/'frontend'/name,raw)
        for name in ('frontend-membership-observer.mjs','community-source-evidence.py'):
            H.write_new(self.root/'.github/tests'/name,(HERE/name).read_bytes())
        self.fixed={'source':{'commit':'a'*40,'tree':'b'*40,'version':json.loads(self.originals['package.json'])['version']},
                    'source_files':{'frontend/'+name:{'sha256':H.sha(raw),'bytes':len(raw)} for name,raw in self.originals.items()}}

    def check(self):return H.frontend_input_preflight(self.root,self.source,self.fixed)

    def test_actual_current_source_and_program_pins_pass(self):
        result=self.check();self.assertEqual('a'*40,result['source_commit'])
        self.assertEqual({name:H.sha(raw) for name,raw in self.originals.items()},result['original_file_sha256'])

    def test_each_current_original_tamper_rejected(self):
        for name,raw in self.originals.items():
            with self.subTest(name=name):
                path=self.source/'frontend'/name;path.write_bytes(raw+b' ')
                with self.assertRaisesRegex(H.Stop,'frontend_original_input_identity'):self.check()
                path.write_bytes(raw)

    def test_symlink_original_rejected(self):
        path=self.source/'frontend/package.json';copy_path=self.root/'copy.json';H.write_new(copy_path,path.read_bytes())
        path.unlink();path.symlink_to(copy_path)
        with self.assertRaisesRegex(H.Stop,'regular_file_limit'):self.check()

    def test_current_requested_version_or_hash_drift_is_rejected(self):
        for kind in ('version','package','lock'):
            with self.subTest(kind=kind):
                fixed=copy.deepcopy(self.fixed)
                if kind=='version':fixed['source']['version']='0.0.0'
                else:fixed['source_files']['frontend/'+('package.json' if kind=='package' else 'package-lock.json')]['sha256']='0'*64
                with self.assertRaises(H.Stop):H.frontend_input_preflight(self.root,self.source,fixed)

    def test_program_bytes_are_pinned_separately_from_product_data(self):
        path=self.root/'.github/tests/frontend-membership-observer.mjs';path.write_bytes(path.read_bytes()+b'\n// synthetic unreviewed program change\n')
        with self.assertRaisesRegex(H.Stop,'frontend_observer_identity'):self.check()



class FullBackendBuildTests(unittest.TestCase):
    """Command-order and trust failures; all Docker/native operations are mocked."""
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='ur-full-build-guard-')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.source = self.root/'source'
        (self.source/'frontend').mkdir(parents=True)
        self.assets = self.root/'assets'; self.assets.mkdir()
        self.data = self.root/'data'; self.data.mkdir()
        for name in ('LICENSE', 'NOTICE'):
            H.write_new(self.source/name, b'synthetic notice')
        self.back, self.front = self.root/'backend.Dockerfile', self.root/'frontend.Dockerfile'
        H.write_new(self.back, b'FROM fixed-backend'); H.write_new(self.front, b'FROM fixed-frontend')
        self.binary = self.root/'synthetic-backend'; H.write_new(self.binary, b'synthetic-backend')
        self.ring = 'synthetic-public-ring-only'
        self.compiler,self.node,self.npm='1.25.12','24.19.0','11.17.0'
        H.write_new(self.source/'go.mod',b'module synthetic\n\ntoolchain go1.25.12\n')
        H.write_new(self.source/'frontend/package.json',H.encoded({'engines':{'node':self.node,'npm':self.npm},'packageManager':'npm@'+self.npm}))
        self.calls = []; self.retained = []; self.saved = []; self.pulls = []
        self.bad_buildinfo = False; self.bad_notice = False; self.bad_front = False
        self.old_backend = False; self.non_release_trust = False; self.actual_compiler = None
        self.layers = ['sha256:'+'2'*64]
        self.guard = types.SimpleNamespace(work=self.root, run_id='77',
            FIXED={'source': {'commit':H.PRODUCT,'tree':H.TREE,'version':'1.14.2'}, 'images': {role+'-runtime': {'rootfs_diff_ids': self.layers} for role in ('backend','frontend')}},
            receipt={'images': {}, 'source_export': {'commit': H.PRODUCT}, 'commands': []},
            docker=self.docker, pull=self.pull, tool=self.tool, inspect_image=self.inspect)
        self.guard.FIXED['images'].update({'backend-builder':{'version_clues':{'GOLANG_VERSION':self.compiler}},
            'frontend-builder':{'version_clues':{'NODE_VERSION':self.node}}})

    def pull(self, role):
        self.pulls.append(role); return role

    def docker(self, label, *args):
        self.assertEqual(args[0], 'build', 'No backend origin load/reuse is permitted')
        self.calls.append((label, list(args)))
        H.write_new(Path(args[args.index('--iidfile')+1]), ('sha256:'+'1'*64+'\n').encode())

    def inspect(self, role, ref):
        self.guard.receipt['images'][role] = {'id': ref, 'rootfs_diff_ids': self.layers}

    def tool(self, label, *args):
        if label == 'byte-go-version': return 'go version go'+(self.actual_compiler or self.compiler)+' linux/amd64'
        if label == 'byte-node-version': return 'v'+self.node+'\n'+self.npm
        self.assertEqual(label, 'actual-backend-buildinfo')
        version = '1.13.0' if self.old_backend else self.guard.FIXED['source']['version']
        commit = ('9e0240c68a20deebf9bcd0e2cc7b58034d09b696' if self.old_backend else
                  '0'*40 if self.bad_buildinfo else self.guard.FIXED['source']['commit'])
        return ('go'+self.compiler+' CGO_ENABLED=0 GOOS=linux GOARCH=amd64 '
            'main.buildVersion='+version+' main.buildCommit='+commit+
            ' main.buildTree='+self.guard.FIXED['source']['tree']+
            ' github.com/uniroute/unirouter/internal/releasetrust.buildMode='+('development' if self.non_release_trust else 'release')+
            ' github.com/uniroute/unirouter/internal/releasetrust.embeddedUpgradeKeyRingBase64='+self.ring)

    def inventory(self, guard, helper, role, image, data):
        return role, self.binary if role == 'backend' else None, {'entries': [], 'apk': []}

    def copy_notice(self, guard, cid, path, target, cap):
        H.write_new(target, b'wrong-notice' if self.bad_notice else (H.INPUTS/'component-notices.tar.gz').read_bytes())

    def save(self, guard, role, image, assets, remaining):
        self.saved.append(role)
        H.write_new(assets/(role+'-image.tar.gz'), ('synthetic-'+role).encode())
        return {'image_id': image, 'compressed': H.file_fact(assets/(role+'-image.tar.gz'))}

    def verifier(self, guard, helper, source, go, assets, data):
        H.write_new(assets/'uniroute-upgrade-linux-amd64', b'synthetic-verifier')

    def build(self):
        membership = {'packages': [], 'output_files': []}
        if self.bad_front: membership['output_files'] = [{'file':'missing.js','bytes':1,'sha256':'a'*64}]
        with patch.dict(os.environ, {'EXPECTED_WORKFLOW_COMMIT':PRODUCER, 'ADMISSION_SHA256':'b'*64}, clear=True), \
             patch.object(H, 'inventory', side_effect=self.inventory), \
             patch.object(H, 'collect_frontend_membership', return_value=membership), \
             patch.object(H, 'saved_image', side_effect=self.save), \
             patch.object(H, 'verifier', side_effect=self.verifier), \
             patch.object(H, 'component_binding', return_value={'synthetic_test_only': True}):
            H.build(self.guard, types.SimpleNamespace(copy_exact=self.copy_notice), self.ring, {},
                self.assets, self.data, lambda p:self.retained.append(p.name),
                (self.source, self.back, self.front, {}))

    def test_each_role_builds_and_retains_once_with_exact_release_arguments(self):
        self.build()
        self.assertEqual([name for name,args in self.calls], ['one retained backend build','one retained frontend build'])
        self.assertEqual(self.saved, ['backend','frontend'])
        self.assertEqual(self.retained, ['backend-image.tar.gz','frontend-image.tar.gz','uniroute-upgrade-linux-amd64'])
        self.assertEqual(self.pulls, ['backend-builder','frontend-builder','backend-runtime','frontend-runtime'])
        backend = self.calls[0][1]
        for value in ['RUNTIME_VERSION=1.14.2','RUNTIME_COMMIT='+H.PRODUCT,'RUNTIME_TREE='+H.TREE,
                      'UNIROUTER_RELEASE_TRUST_MODE=release','UNIROUTER_UPGRADE_TRUST_BUNDLE_B64='+self.ring]:
            self.assertIn(value, backend)
        self.assertIn('VITE_APP_VERSION=1.14.2', self.calls[1][1])
        self.assertTrue((self.data/'actual-component-bindings.json').exists())

    def assert_current_build(self,version,commit,tree):
        self.guard.FIXED['source']={'version':version,'commit':commit,'tree':tree}
        self.build()
        for value in ('RUNTIME_VERSION='+version,'RUNTIME_COMMIT='+commit,'RUNTIME_TREE='+tree):
            self.assertIn(value,self.calls[0][1])
        self.assertIn('VITE_APP_VERSION='+version,self.calls[1][1])

    def test_actual_builder_consumes_current_source_1150(self):
        self.assert_current_build('1.15.0','a'*40,'c'*40)

    def test_actual_builder_consumes_second_source_without_program_edits(self):
        self.compiler,self.node,self.npm='1.25.13','24.20.0','11.17.1'
        (self.source/'go.mod').write_text('module synthetic\n\ntoolchain go'+self.compiler+'\n')
        (self.source/'frontend/package.json').write_bytes(H.encoded({'engines':{'node':self.node,'npm':self.npm},'packageManager':'npm@'+self.npm}))
        self.guard.FIXED['images']['backend-builder']['version_clues']['GOLANG_VERSION']=self.compiler
        self.guard.FIXED['images']['frontend-builder']['version_clues']['NODE_VERSION']=self.node
        self.assert_current_build('1.15.1','b'*40,'d'*40)

    def test_actual_compiler_drift_refuses_before_app_build_or_retention(self):
        self.actual_compiler='1.25.13'
        with self.assertRaisesRegex(H.Stop,'actual_build_tools'):self.build()
        self.assertFalse(self.calls);self.assertFalse(self.retained)

    def test_source_and_builder_toolchains_must_match_before_pull_or_build(self):
        self.guard.FIXED['images']['frontend-builder']['version_clues']['NODE_VERSION']='24.20.0'
        with self.assertRaisesRegex(H.Stop,'source_builder_toolchain_binding'):self.build()
        self.assertFalse(self.pulls);self.assertFalse(self.calls);self.assertFalse(self.retained)

    def test_wrong_backend_source_buildinfo_refuses_before_retention(self):
        self.bad_buildinfo = True
        with self.assertRaisesRegex(H.Stop, 'backend_release_buildinfo'): self.build()
        self.assertEqual(self.saved, []); self.assertEqual(self.retained, [])

    def test_old_1_13_backend_refuses_before_retention(self):
        self.old_backend = True
        with self.assertRaisesRegex(H.Stop, 'backend_release_buildinfo'): self.build()
        self.assertEqual(self.saved, []); self.assertEqual(self.retained, [])

    def test_non_release_backend_trust_refuses_before_retention(self):
        self.non_release_trust = True
        with self.assertRaisesRegex(H.Stop, 'backend_release_buildinfo'): self.build()
        self.assertEqual(self.saved, []); self.assertEqual(self.retained, [])

    def test_changed_image_notice_refuses_before_retention(self):
        self.bad_notice = True
        with self.assertRaisesRegex(H.Stop, 'image_notice_binding'): self.build()
        self.assertEqual(self.saved, []); self.assertEqual(self.retained, [])

    def test_frontend_output_mismatch_preserves_only_new_backend_partial(self):
        self.bad_front = True
        with self.assertRaisesRegex(H.Stop, 'final_dist_membership'): self.build()
        self.assertEqual(self.saved, ['backend']); self.assertEqual(self.retained, ['backend-image.tar.gz'])
        self.assertEqual((self.assets/'backend-image.tar.gz').read_bytes(), b'synthetic-backend')


class VerifierSourceClosureTests(unittest.TestCase):
    def test_actual_offline_recipe_contains_all_transitive_local_package_sources(self):
        class RecipeReady(Exception): pass
        with tempfile.TemporaryDirectory(prefix='ur-verifier-recipe-') as temporary:
            class Guard:
                work=Path(temporary)
                FIXED={'images':{'backend-builder':{'pull_reference':'registry.example.invalid/go@sha256:'+'a'*64}}}
                def docker(self,*args): raise RecipeReady()
            with self.assertRaises(RecipeReady):
                H.verifier(Guard(),None,Path(temporary),'synthetic-go',Path(temporary),Path(temporary))
            recipe=(Path(temporary)/'verifier.Dockerfile').read_text()
            pending=['cmd/uniroute-upgrade'];seen=set()
            while pending:
                package=pending.pop()
                if package in seen: continue
                seen.add(package)
                for source in (HERE.parents[1]/package).glob('*.go'):
                    if source.name.endswith('_test.go'): continue
                    for imported in re.findall(r'"github\.com/uniroute/unirouter/([^"]+)"',source.read_text()):
                        if imported not in seen: pending.append(imported)
            self.assertIn('internal/strictjson',seen)
            for package in seen:
                self.assertIn('COPY '+package+'/ '+package+'/',recipe.splitlines())

if __name__ == '__main__':
    unittest.main(verbosity=2)
