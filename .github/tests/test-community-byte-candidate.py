#!/usr/bin/env python3
"""Offline failure-path checks. No HTTP sockets, services, Docker or old test reruns."""
import ast
import copy
import io
import json
import os
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


class LocalInputTests(unittest.TestCase):
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
            def initialize(g, work, env): g.work = work
            def fail(*args): reference.require(False, 'DO_NOT_RETAIN /private/path https://signed.example/?secret=x')
            with patch.dict(os.environ, env, clear=True), patch.object(sys, 'argv', [H.__file__]), \
                 patch.object(H, 'initialize_guard', side_effect=initialize), \
                 patch.object(H, 'module', side_effect=lambda name, path: guard if name == 'accepted_byte_guards' else None), \
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


if __name__ == '__main__':
    unittest.main(verbosity=2)
