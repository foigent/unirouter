#!/usr/bin/env python3
"""Offline checks for fixed and incomplete v2/never inputs. No network or Docker."""
import contextlib
import base64
import copy
import datetime as dt
import importlib.util
import hashlib
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


class PublicPredecessorTests(unittest.TestCase):
    def setUp(self):
        self.old = json.loads((ROOT / m.UPGRADE_SOURCE).read_bytes())
        self.current = copy.deepcopy(self.old)
        self.current['product'].update(commit='b' * 40, tree='c' * 40, version='1.15.6')
        c = self.current['inputs']['config']
        c['immutable']['product'] = {k: self.current['product'][k] for k in ('commit', 'tree', 'version', 'schema')}
        c['immutable'].update(max_current_sequence=22, historical_sequence_reserved_through=22,
            native_acceptance_helper_sha256=m.sha(ROOT / '.github/tests/community-installer.py'))
        bootstrap = ROOT / 'deploy/community/bootstrap.py'
        c['immutable']['bootstrap_script'] = {
            'url': ('https://raw.githubusercontent.com/foigent/unirouter/' +
                    self.current['product']['commit'] + '/deploy/community/bootstrap.py'),
            'sha256': m.sha(bootstrap), 'size': bootstrap.stat().st_size}
        c['release'].update(release_sequence=23, tag='v1.15.6-preview.23', package_sha256='d' * 64)
        c['release']['bootstrap_url'] = m.asset_url(c['release'], 'community-bootstrap.json')
        self.candidate = c  # Synthetic future metadata, never dispatched or signed.

    def git(self, root, *args):
        return subprocess.check_output(['git', '-c', 'user.name=Offline fixture',
            '-c', 'user.email=fixture@localhost', '-c', 'commit.gpgsign=false',
            '-c', 'core.hooksPath=/dev/null', '-C', str(root), *args], stderr=subprocess.DEVNULL)

    def commit_files(self, root):
        self.git(root, 'init', '-q')
        self.git(root, 'add', '.')
        self.git(root, 'commit', '-qm', 'Synthetic offline fixture')

    def test_exact_accepted_source_and_existing_closed_parsers(self):
        self.assertEqual(m.validate(self.candidate, ready=True, stage_record=self.current), [])
        self.assertEqual(m.validate_upgrade_source(self.old, self.candidate), self.old['inputs']['config'])
        self.assertEqual(self.old['inputs']['config']['release']['package_sha256'],
                         '46ebdf5d987603aed1989a83f7e7d6a2148214b7f301b80a91413c3964e83e91')
        for field, value in [('key_id', 'other'), ('raw_public_fingerprint', 'f' * 64),
                             ('public_ring', {'size': 83, 'sha256': 'f' * 64}),
                             ('max_current_sequence', 21), ('min_current_sequence', 23)]:
            with self.subTest(field=field):
                candidate = copy.deepcopy(self.candidate); candidate['immutable'][field] = value
                with self.assertRaises(m.Stop): m.validate_upgrade_source(self.old, candidate)
        candidate = copy.deepcopy(self.candidate); candidate['release']['release_sequence'] = 22
        with self.assertRaises(m.Stop): m.validate_upgrade_source(self.old, candidate)
        broken = copy.deepcopy(self.old); broken['inputs']['config']['immutable']['public_ring']['sha256'] = 'f' * 64
        with self.assertRaisesRegex(Exception, 'stage_consumer_trust'):
            m.validate_upgrade_source(broken, self.candidate)

    def test_fixture_is_bound_to_actual_git_blob_and_refuses_dirty_or_symlink_data(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp).resolve(); path = root / m.UPGRADE_SOURCE
            path.parent.mkdir(parents=True); raw = (ROOT / m.UPGRADE_SOURCE).read_bytes(); path.write_bytes(raw)
            self.commit_files(root)
            self.assertEqual(m.upgrade_source_record(root, self.candidate), self.old)
            path.write_bytes(raw + b' ')
            with self.assertRaises(m.Stop): m.upgrade_source_record(root, self.candidate)
            path.unlink(); target = root / 'same-public-data.json'; target.write_bytes(raw); path.symlink_to(target)
            with self.assertRaises(m.Stop): m.upgrade_source_record(root, self.candidate)

    def test_current_entry_preflight_stays_process_network_and_destination_free(self):
        stages = m.stage_module()
        for action in ('validate', 'describe'):
            with self.subTest(action=action), mock.patch.object(m, 'stage_module', return_value=stages), \
                 mock.patch.object(stages, 'read_dispatch', return_value=(None, self.current, None)), \
                 mock.patch.object(m.subprocess, 'run', side_effect=AssertionError('process forbidden')), \
                 mock.patch.object(m.tempfile, 'mkdtemp', side_effect=AssertionError('destination forbidden')), \
                 mock.patch.object(m.urllib.request, 'urlopen', side_effect=AssertionError('network forbidden')), \
                 contextlib.redirect_stdout(io.StringIO()) as output:
                self.assertEqual(m.main([action, '--stage-dispatch']), 0)
                if action == 'describe':
                    self.assertEqual(json.loads(output.getvalue())['upgrade_source'], self.old['product'])
        bad = copy.deepcopy(self.current)
        bad['inputs']['config']['immutable'].update(max_current_sequence=21, historical_sequence_reserved_through=21)
        release = bad['inputs']['config']['release']
        release.update(release_sequence=22, tag='v1.15.6-preview.22')
        release['bootstrap_url'] = m.asset_url(release, 'community-bootstrap.json')
        self.assertEqual(m.validate(bad['inputs']['config'], ready=True, stage_record=bad), [])
        with mock.patch.object(m, 'stage_module', return_value=stages), \
             mock.patch.object(stages, 'read_dispatch', return_value=(None, bad, None)), \
             mock.patch.object(m.subprocess, 'run', side_effect=AssertionError('process forbidden')), \
             mock.patch.object(m.tempfile, 'mkdtemp', side_effect=AssertionError('destination forbidden')), \
             mock.patch.object(m.urllib.request, 'urlopen', side_effect=AssertionError('network forbidden')), \
             contextlib.redirect_stderr(io.StringIO()) as error:
            self.assertEqual(m.main(['validate', '--stage-dispatch']), 1)
            self.assertIn('predecessor is outside', error.getvalue())

    def test_original_checkout_checks_commit_tree_and_every_executable_before_import(self):
        for fault in (None, 'commit', 'tree', 'helper', 'assertions', 'bootstrap', 'symlink', 'directory'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp).resolve(); old = root / m.PREDECESSOR_DIRECTORY; old.mkdir()
                names = ('.github/tests/community-installer.py', '.github/tests/community-release-acceptance.py',
                         '.github/tests/community-stage-record.py', 'deploy/community/bootstrap.py')
                for name in names:
                    f = old / name; f.parent.mkdir(parents=True, exist_ok=True); f.write_bytes(b'# Not executed: offline fixture\n')
                self.commit_files(old)
                record = copy.deepcopy(self.old)
                commit, tree = self.git(old, 'rev-parse', 'HEAD', 'HEAD^{tree}').decode().splitlines()
                record['product'].update(commit=commit, tree=tree)
                fixed = record['inputs']['config']['immutable']
                fixed['native_acceptance_helper_sha256'] = m.sha(old / names[0])
                fixed['bootstrap_script']['sha256'] = m.sha(old / names[3])
                if fault in ('commit', 'tree'): record['product'][fault] = 'f' * 40
                if fault in ('helper', 'assertions', 'bootstrap'):
                    index = {'helper': 0, 'assertions': 1, 'bootstrap': 3}[fault]
                    (old / names[index]).write_bytes(b'# Dirty executable\n')
                if fault == 'symlink':
                    f = old / names[0]; original = f.read_bytes(); f.unlink()
                    target = root / 'same-helper.py'; target.write_bytes(original); f.symlink_to(target)
                if fault == 'directory':
                    moved = root / 'other'; old.rename(moved); old.symlink_to(moved, target_is_directory=True)
                with mock.patch.object(m, 'load_module', side_effect=AssertionError('must not import')):
                    if fault:
                        with self.assertRaises(m.Stop): m.predecessor_checkout(root, record)
                    else:
                        self.assertEqual(m.predecessor_checkout(root, record), old)

    def test_actual_ring_bytes_and_raw_fingerprint_not_only_key_id(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp).resolve(); a, b = root / 'a.json', root / 'b.json'
            key = b'k' * 32; raw = json.dumps({'fixture': base64.b64encode(key).decode()}).encode()
            fixed = {'key_id': 'fixture', 'public_ring': {'size': len(raw), 'sha256': hashlib.sha256(raw).hexdigest()},
                     'raw_public_fingerprint': hashlib.sha256(key).hexdigest()}
            a.write_bytes(raw); b.write_bytes(raw); m.same_public_ring(a, b, fixed)
            b.write_bytes(raw + b'\n')
            with self.assertRaises(m.Stop): m.same_public_ring(a, b, fixed)
            b.write_bytes(raw); fixed['raw_public_fingerprint'] = 'f' * 64
            with self.assertRaises(m.Stop): m.same_public_ring(a, b, fixed)

    def test_bad_source_is_rejected_before_destination_import_or_external_effect(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp).resolve(); path = root / m.UPGRADE_SOURCE; path.parent.mkdir(parents=True)
            path.write_bytes((ROOT / m.UPGRADE_SOURCE).read_bytes()); self.commit_files(root)
            path.write_text('{}')  # Dirty data must fail before even looking for a checkout.
            with mock.patch.object(m, 'native_guard', return_value={}), mock.patch.object(m, 'reference_sources'), \
                 mock.patch.object(m.tempfile, 'mkdtemp') as mkdir, mock.patch.object(m, 'make_acceptance') as make, \
                 mock.patch.object(m.urllib.request.OpenerDirector, 'open') as network:
                with self.assertRaisesRegex(m.Stop, 'Installed-source fixture differs'):
                    m.execute(self.candidate, root, self.current)
                mkdir.assert_not_called(); make.assert_not_called(); network.assert_not_called()

    def test_public_bootstrap_and_native_install_keep_the_selected_port(self):
        spec = importlib.util.spec_from_file_location('actual_shared_fixture', ROOT / '.github/tests/community-installer.py')
        native = importlib.util.module_from_spec(spec); spec.loader.exec_module(native)
        with tempfile.TemporaryDirectory() as tmp, mock.patch.object(m.shutil, 'which', return_value='/fixture/docker'):
            root = Path(tmp).resolve()
            def loader(path, digest, name):
                return native if name == 'accepted_native_installer' else types.SimpleNamespace()
            with mock.patch.object(m, 'load_module', side_effect=loader):
                instance = m.make_acceptance(ROOT, root, self.old['inputs']['config'], {})
            instance.loopback_port = 13001
            for command in (instance.bootstrap_command('prepare', root / 'install', 'fixture'),
                            instance.command('install', root / 'install', 'fixture')):
                self.assertEqual(command[command.index('--port') + 1], '13001')

    def test_delta_only_uses_the_installed_basis_and_exact_reconstructed_target(self):
        old_config = self.old['inputs']['config']
        for fault in (None, 'basis', 'full-request', 'different-target'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as tmp, \
                 mock.patch.object(m.shutil, 'which', return_value='/fixture/docker'):
                root = Path(tmp).resolve()
                source = root / 'original' / 'release.tar.gz'
                old = types.SimpleNamespace(verifier=root / 'original-verifier')
                basis = {'size': old_config['release']['package_size'],
                         'sha256': 'f' * 64 if fault == 'basis' else old_config['release']['package_sha256']}
                assets = {role: {'url': role} for role in ('package', 'verifier', 'public_keys')}
                delta = {'basis': basis, 'asset': {'url': 'delta', 'size': 5,
                                                  'sha256': hashlib.sha256(b'delta').hexdigest()}}

                def prepare(anchor, stage, *, fetch, source_package, source_verifier):
                    self.assertEqual((source_package, source_verifier), (source, old.verifier))
                    stage.mkdir()
                    if fault == 'full-request':
                        fetch(anchor.assets['package'], stage / 'release.tar.gz')
                    fetch(anchor.assets['public_keys'], stage / 'public-keys.json')
                    fetch(anchor.delta['asset'], stage / 'delta.tar.gz')
                    (stage / 'release.tar.gz').write_bytes(b'different' if fault == 'different-target' else b'target')

                module = types.SimpleNamespace(BootstrapError=ValueError, prepare=mock.Mock(side_effect=prepare),
                                               download=lambda asset, path: path.write_bytes(asset['url'].encode()))
                load_module = m.load_module
                with mock.patch.object(m, 'load_module', side_effect=lambda path, digest, name:
                        module if name == 'accepted_product_bootstrap' else load_module(path, digest, name)):
                    instance = m.make_acceptance(ROOT, root, self.candidate, {})
                instance.upgrade_anchor = types.SimpleNamespace(assets=assets, delta=delta)
                instance.bootstrap_file.write_bytes(b'metadata')
                instance.bootstrap_script.write_bytes(b'script')
                original = instance.release
                original.mkdir()
                (original / 'release.tar.gz').write_bytes(b'target')
                if fault:
                    with self.assertRaises(m.Stop):
                        instance.prepare_public_upgrade_delta(source.parent, old, old_config)
                    self.assertEqual(instance.release, original)
                    if fault == 'basis':
                        module.prepare.assert_not_called()
                else:
                    instance.prepare_public_upgrade_delta(source.parent, old, old_config)
                    self.assertEqual(instance.release, root / 'prepared-upgrade-delta')
                    proof = instance.result['public_upgrade_delta']
                    self.assertEqual(proof['https_asset_body_bytes'], {'public_keys': 11, 'delta': 5})
                    self.assertEqual(proof['full_package_requests'], 0)
                    self.assertTrue(proof['verifier_reused'])

    def test_current_public_bootstrap_requires_delta_and_legacy_predecessor_loads(self):
        for config in (self.old['inputs']['config'], self.candidate):
            fixed, release = config['immutable'], config['release']
            product = fixed['product']
            name = 'unirouter-' + product['version'] + '-linux-amd64'
            value = {'release': {'version': product['version'], 'commit': product['commit'],
                       'release_sequence': release['release_sequence'], 'key_id': fixed['key_id'],
                       'target_schema': product['schema']},
                     'assets': {'verifier': dict(fixed['verifier'], url=m.asset_url(release, 'uniroute-upgrade-linux-amd64')),
                       'public_keys': dict(fixed['public_ring'], url=m.asset_url(release, 'public-keys.json')),
                       'package': {'url': m.asset_url(release, name + '.tar.gz'),
                                   'size': release['package_size'], 'sha256': release['package_sha256']}}}
            if config is self.candidate:
                with self.assertRaisesRegex(m.Stop, 'must include its exact predecessor delta'):
                    m.bootstrap_binding(config, value)
                value['delta'] = {'asset': {'url': m.asset_url(release, name + '.delta.tar.gz')}}
            m.bootstrap_binding(config, value)

    def test_current_formal_execution_requires_predecessor_and_verified_delta_result(self):
        for fault in (None, 'missing-predecessor', 'missing-result'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as tmp, \
                 mock.patch.object(m.shutil, 'which', return_value='/fixture/docker'):
                root = Path(tmp).resolve()
                source = None if fault == 'missing-predecessor' else (root / 'old-source', self.old)
                instance = m.make_acceptance(ROOT, root, self.candidate, {}, upgrade_source=source)
                for step in ('preflight', 'prepare', 'negative_checks', 'interrupt_after_plan', 'installed_checks',
                             'chosen_password_checks', 'first_customer_checks', 'release_completed_case'):
                    setattr(instance, step, mock.Mock())
                def installed():
                    if fault != 'missing-result':
                        instance.result['public_upgrade_delta'] = {'byte_identical': True}
                instance.installed_public_upgrade = mock.Mock(side_effect=installed)
                if fault:
                    with self.assertRaisesRegex(m.Stop, 'exact public predecessor|did not verify its exact predecessor delta'):
                        instance.execute()
                    self.assertNotEqual(instance.result['status'], 'passed')
                    if fault == 'missing-predecessor':
                        instance.preflight.assert_not_called()
                else:
                    instance.execute()
                    self.assertEqual(instance.result['status'], 'passed')
                    instance.installed_public_upgrade.assert_called_once()

    def formal_facts_fixture(self, root, factory=None):
        spec = importlib.util.spec_from_file_location('formal_port_fixture', ROOT / '.github/tests/community-installer.py')
        native = importlib.util.module_from_spec(spec); spec.loader.exec_module(native)
        with mock.patch.object(m, 'load_module', side_effect=lambda path, digest, name:
                               native if name == 'accepted_native_installer' else types.SimpleNamespace()), \
             mock.patch.object(m.shutil, 'which', return_value='/fixture/docker'):
            old = (factory or m.make_acceptance)(ROOT, root, self.old['inputs']['config'], {})
        directory = old.instances / 'preserved'
        (directory / 'evidence').mkdir(parents=True)
        old.image_ids = {role: 'sha256:' + str(i) * 64
                         for i, role in enumerate(sorted(old.images), 1)}
        state = {'image_ids': old.image_ids, 'files': {}, 'installation_id': 'synthetic-original'}
        plan = {'target': {'database': 'synthetic-original-db'}}
        (directory / 'install-state.json').write_text(json.dumps(state))
        (directory / 'evidence/install.json').write_text(json.dumps(plan))
        old.target = lambda *args: plan['target']
        release = {'sequence': old.release_sequence, 'version': old.version, 'commit': old.commit,
                   'package': old.config['release']['package_sha256'], 'schema': old.schema}
        def sql(_directory, _project, query):
            if query.startswith('SELECT version FROM'): return old.schema
            if query.startswith('SELECT obj_description'):
                return 'native record\n' + json.dumps({'state': 'completed', 'plan': plan})
            if query.startswith('SELECT count(*) FROM audit_logs'): return '1'
            return json.dumps(release)
        old.sql = sql
        port, address = ['13000'], ['127.0.0.1']
        def run(label, argv):
            if label == 'install original public predecessor without rebuild':
                port[0] = argv[argv.index('--port') + 1]
                return ''
            if label == 'read installed containers':
                return '\n'.join(old.image_ids)
            self.assertEqual(label, 'inspect installed containers')
            return json.dumps([{'Config': {'Labels': {'com.docker.compose.service': role}},
                'Image': image, 'State': {'Running': True, 'Health': {'Status': 'healthy'}},
                'HostConfig': {'PortBindings': {'80/tcp': [{'HostIp': address[0], 'HostPort': port[0]}]}
                               if role == 'frontend' else {}}}
                for role, image in old.image_ids.items()])
        old.run = run
        return old, directory, port, address

    def test_formal_facts_rejects_wrong_port_and_public_bind_without_relaxing_guard(self):
        with tempfile.TemporaryDirectory() as tmp:
            old, directory, port, address = self.formal_facts_fixture(Path(tmp).resolve())
            for declared in (13000, 13001):
                old.loopback_port, port[0] = declared, str(declared)
                self.assertEqual(old.facts(directory, 'fixture')['installation_id'], 'synthetic-original')
                port[0] = str(13001 if declared == 13000 else 13000)
                with self.assertRaisesRegex(m.Stop, 'non-loopback or unexpected published service'):
                    old.facts(directory, 'fixture')
            old.loopback_port = 13000
            port[0], address[0] = '13000', '0.0.0.0'
            with self.assertRaisesRegex(m.Stop, 'non-loopback or unexpected published service'):
                old.facts(directory, 'fixture')
            address[0] = '127.0.0.1'
            inspect = old.run
            def publish_backend(label, argv):
                raw = inspect(label, argv)
                if label == 'inspect installed containers':
                    items = json.loads(raw)
                    next(x for x in items if x['Config']['Labels']['com.docker.compose.service'] == 'backend')[
                        'HostConfig']['PortBindings'] = {'8080/tcp': [{'HostIp': address[0], 'HostPort': port[0]}]}
                    return json.dumps(items)
                return raw
            old.run = publish_backend
            with self.assertRaisesRegex(m.Stop, 'non-loopback or unexpected published service'):
                old.facts(directory, 'fixture')

    def test_signed_pair_installs_predecessor_on_its_formal_port_and_clears_prior_negative(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp).resolve()
            native = importlib.util.spec_from_file_location('signed_pair_fixture', ROOT / '.github/tests/community-installer.py')
            base = importlib.util.module_from_spec(native); native.loader.exec_module(base)
            with mock.patch.object(m, 'load_module', side_effect=lambda path, digest, name:
                                   base if name == 'accepted_native_installer' else types.SimpleNamespace()), \
                 mock.patch.object(m.shutil, 'which', return_value='/fixture/docker'):
                current = m.make_acceptance(ROOT, root, self.candidate, {},
                                            upgrade_source=(root / 'old-source', self.old))
            factory, observed = m.make_acceptance, {}
            def predecessor(_repo, old_root, _config, _context, **kwargs):
                old, directory, port, _ = self.formal_facts_fixture(old_root, factory)
                old.preflight = old.prepare = lambda: None
                def checked(_directory, project):
                    old.facts(_directory, project)
                    self.assertEqual(old.loopback_port, 13001)
                def first_use(*args, **kwargs):
                    self.assertEqual(old.loopback_port, 13001)
                    raise RuntimeError('passed actual predecessor facts; stop before private first-use or upgrade')
                old.installed_checks = checked
                old.native_first_use_checks = first_use
                observed['port'] = port
                return old
            current.result['installer_stop'] = 'Earlier expected wrong-project refusal'
            with mock.patch.object(m, 'make_acceptance', side_effect=predecessor), \
                 mock.patch.object(m, 'same_public_ring'):
                with self.assertRaisesRegex(RuntimeError, 'passed actual predecessor facts'):
                    current.installed_public_upgrade()
            self.assertEqual(observed['port'][0], '13001')
            self.assertEqual(current.loopback_port, 13001)
            self.assertNotIn('installer_stop', current.result)

    def test_nested_original_failure_keeps_its_phase_and_only_authored_stop(self):
        for step in ('preflight', 'prepare', 'install', 'installed_checks', 'native_first_use_checks', 'seed'):
            with self.subTest(step=step), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp).resolve()
                # Actual old/native imports have distinct exception classes.
                failure = type('SeparatelyImportedFailure', (Exception,), {})
                old = types.SimpleNamespace(instances=root / 'old-instances', ring=root / 'ring',
                    phase='original ' + step, result={'first_customer': {'organization_id': 2}},
                    preflight=lambda: None, prepare=lambda: None, command=lambda *a: [],
                    run=lambda *a: '', installed_checks=lambda *a: None,
                    native_first_use_checks=lambda *a, **kw: None, sql=lambda *a: '')
                def stop(*args, **kwargs):
                    if step == 'install':
                        old.result['installer_stop'] = 'Installation stopped: The selected loopback port is already in use'
                    raise failure('synthetic private child output must stay redacted')
                setattr(old, 'run' if step == 'install' else 'sql' if step == 'seed' else step, stop)
                # Recreate only the formal wrapper: all external effects are mocked.
                with mock.patch.object(m.shutil, 'which', return_value='/fixture/docker'):
                    paired_root = root / 'paired'; paired_root.mkdir()
                    current = m.make_acceptance(ROOT, paired_root, self.candidate, {},
                        upgrade_source=(root / 'old-source', self.old))
                current.result['installer_stop'] = 'Earlier expected refusal'
                with mock.patch.object(m, 'make_acceptance', return_value=old), mock.patch.object(m, 'same_public_ring'):
                    with self.assertRaises(failure): current.installed_public_upgrade()
                self.assertEqual(current.phase, old.phase)
                if step == 'install':
                    self.assertEqual(current.result['installer_stop'], old.result['installer_stop'])
                else:
                    self.assertNotIn('installer_stop', current.result)

    def test_formal_commands_discard_expected_refusals_and_keep_unexpected_native_stop(self):
        with tempfile.TemporaryDirectory() as tmp:
            instance = self.formal_facts_fixture(Path(tmp).resolve())[0]
            del instance.run
            parent = type(instance).__mro__[1]
            failure = parent.run.__globals__['Failed']
            def native(owner, label, argv, **kwargs):
                self.assertNotIn('installer_stop', owner.result)
                owner.result['installer_stop'] = 'Installation stopped: authored native reason'
                if kwargs.get('ok', True): raise failure('private child text')
                return 1, ''
            instance.result['installer_stop'] = 'stale previous reason'
            with mock.patch.object(parent, 'run', new=native):
                self.assertEqual(instance.run('expected refusal', [], ok=False), (1, ''))
                self.assertNotIn('installer_stop', instance.result)
                with self.assertRaises(failure): instance.run('unexpected native stop', [])
                self.assertEqual(instance.result['installer_stop'], 'Installation stopped: authored native reason')

    def test_failure_diagnostics_preserve_primary_stop_on_success_and_failure(self):
        for failed in (False, True):
            for primary in (None, 'Installation stopped: original primary reason'):
                with self.subTest(failed=failed, primary=primary), tempfile.TemporaryDirectory() as tmp:
                    instance, directory, _, _ = self.formal_facts_fixture(Path(tmp).resolve())
                    del instance.run
                    instance.directory, instance.project, instance.phase = directory, 'synthetic', 'original failure'
                    instance.result['status'] = 'failed'
                    if primary is not None: instance.result['installer_stop'] = primary
                    parent = type(instance).__mro__[1]
                    failure = parent.run.__globals__['Failed']
                    def diagnostic(owner, label, argv, **kwargs):
                        owner.phase = label
                        owner.result['installer_stop'] = 'Installation stopped: diagnostic secondary reason'
                        if failed: raise failure('private diagnostic text')
                        return ''
                    with mock.patch.object(parent, 'run', new=diagnostic): instance.failure_facts()
                    self.assertEqual(instance.phase, 'original failure')
                    self.assertEqual(instance.result.get('installer_stop'), primary)
                    if primary is None: self.assertNotIn('installer_stop', instance.result)
                    self.assertEqual(bool(instance.result.get('failure_diagnostics_incomplete')), failed)

    def test_formal_password_customer_and_restart_HTTP_use_declared_port(self):
        with tempfile.TemporaryDirectory() as tmp:
            instance, directory, _, _ = self.formal_facts_fixture(Path(tmp).resolve())
            instance.loopback_port = 13001
            (directory / 'admin-password').write_text('public synthetic initial input')
            (directory / 'runtime').mkdir()
            state = {'admin': {'org_id': 1}, 'projects': [{'id': 2}]}
            instance.first_customer_state = mock.Mock(return_value=state)
            instance.run = mock.Mock(return_value='')
            instance.request = mock.Mock(return_value=b'{"organization_role":"platform_admin","token":"public-synthetic-token"}')
            with mock.patch.object(m, 'password_choice_delta'), \
                 mock.patch.object(m, 'fixture_admin_login') as login, \
                 mock.patch.object(m, 'first_customer_request', return_value={}) as customer, \
                 mock.patch.object(m, 'first_customer_preview'), \
                 mock.patch.object(m, 'first_customer_result', return_value=2), \
                 mock.patch.object(m, 'first_customer_transition'):
                instance.chosen_password_checks(directory, 'synthetic')
                instance.first_customer_checks(directory, 'synthetic')
            self.assertEqual(len(login.call_args_list), 2)
            self.assertTrue(all(c.kwargs.get('loopback_port') == 13001 for c in login.call_args_list))
            self.assertEqual(len(customer.call_args_list), 3)
            self.assertTrue(all(c.kwargs.get('loopback_port') == 13001 for c in customer.call_args_list))
            self.assertEqual([c.args[0] for c in instance.request.call_args_list], [
                'http://127.0.0.1:13001/api/v1/auth/login', 'http://127.0.0.1:13001/health/ready',
                'http://127.0.0.1:13001/api/v1/auth/login'])

    def test_shipped_resume_still_refuses_project_socket_package_and_changed_daemon(self):
        spec = importlib.util.spec_from_file_location('unchanged_shipped_installer', ROOT / 'deploy/customer/install.py')
        installer = importlib.util.module_from_spec(spec); spec.loader.exec_module(installer)
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp).resolve(); directory = root / 'original'; directory.mkdir(mode=0o700)
            package = directory / 'release.tar.gz'; package.write_bytes(b'public synthetic package')
            package.chmod(0o600)
            digest = hashlib.sha256(package.read_bytes()).hexdigest()
            state = {'format': 'unirouter.install-state.v1', 'directory': str(directory),
                     'project': 'original', 'port': 13000, 'docker_socket': '/fixture/docker.sock',
                     'package_sha256': digest, 'daemon': 'original-daemon', 'files': {}}
            path = directory / 'install-state.json'; path.write_text(json.dumps(state)); path.chmod(0o600)
            release = types.SimpleNamespace(package=package, directory=root, sha=digest)
            original = installer.Installer(release, directory, 'original', 13000, Path('/fixture/docker.sock'))
            original.load_state()
            for field, value in [('project', 'other'), ('docker_socket', '/fixture/other.sock'),
                                 ('package_sha256', 'f' * 64)]:
                changed = dict(state, **{field: value}); path.write_text(json.dumps(changed))
                with self.subTest(field=field), self.assertRaisesRegex(installer.InstallError, 'Resume identity differs'):
                    original.load_state()
            path.write_text(json.dumps(state)); original.load_state()
            original.inventory = lambda: []
            def command(argv, label, input=None):
                return (json.dumps({'OSType': 'linux', 'ID': 'changed-daemon', 'SecurityOptions': []})
                        if label == 'local Docker daemon prerequisite' else 'Compose v2')
            original.command = command
            with mock.patch.object(installer.platform, 'system', return_value='Linux'), \
                 mock.patch.object(installer.os, 'geteuid', return_value=0):
                with self.assertRaisesRegex(installer.InstallError, 'selected Docker daemon changed'):
                    original.check(resume=True)

    def test_actual_report_keeps_authored_guard_reason_and_redacts_arbitrary_errors(self):
        for error in (m.Stop('non-loopback or unexpected published service'),
                      RuntimeError('synthetic private child output must stay redacted')):
            with self.subTest(error=type(error).__name__), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp).resolve()
                instance = types.SimpleNamespace(env={}, phase='predecessor facts',
                    result={'status': 'running', 'checks': []},
                    execute=mock.Mock(side_effect=error), failure_facts=mock.Mock())
                with mock.patch.object(m, 'validate'), mock.patch.object(m, 'native_guard', return_value={}), \
                     mock.patch.object(m, 'reference_sources'), mock.patch.object(m, 'upgrade_source_record', return_value=None), \
                     mock.patch.object(m.tempfile, 'mkdtemp', return_value=str(root)), \
                     mock.patch.object(m, 'make_acceptance', return_value=instance), \
                     mock.patch.object(m, 'clean_environment', side_effect=lambda env: contextlib.nullcontext()), \
                     contextlib.redirect_stdout(io.StringIO()) as output:
                    self.assertEqual(m.execute(self.candidate, ROOT, self.current), 1)
                report = json.loads(output.getvalue())
                self.assertEqual(report['error'], str(error) if isinstance(error, m.Stop) else 'consumer_check_failed_redacted')
                self.assertNotIn('synthetic private child output', output.getvalue())

    def test_public_upgrade_refuses_check_mutation_or_wrong_identity_before_native_write(self):
        # Exercise the actual new case through its first native-write boundary.
        # Services/downloads are synthetic here; file inventory is real. The
        # positive signed upgrade remains a later disposable Linux acceptance.
        for fault in (None, 'check-write', 'source-package', 'writes-flag', 'resume'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as tmp, \
                 mock.patch.object(m.shutil, 'which', return_value='/fixture/docker'):
                root = Path(tmp).resolve()
                instance = m.make_acceptance(ROOT, root, self.candidate, {},
                    upgrade_source=(root / 'old-source', self.old))
                old_root = root / 'installed-predecessor'
                directory = old_root / 'instances/preserved'
                def prepare():
                    (directory / 'runtime').mkdir(parents=True)
                    (directory / 'evidence').mkdir()
                    for name in ('admin-password', 'release.tar.gz', 'install-state.json',
                                 'runtime/.env', 'evidence/install.json'):
                        (directory / name).write_bytes(b'public synthetic fixture bytes')
                old = types.SimpleNamespace(instances=old_root / 'instances', ring=old_root / 'ring',
                    preflight=lambda: None, prepare=prepare, command=lambda *a: [],
                    installed_checks=lambda *a: None, native_first_use_checks=lambda *a, **kw: None,
                    result={'first_customer': {'organization_id': 2}}, release_sequence=22,
                    schema='000129_upstream_observation_restore_namespace', compose=lambda *a: [])
                old.run = lambda label, argv: (json.dumps({'services': {'backend': {'environment': {}}}})
                                              if label == 'read original content configuration' else '')
                old.sql = lambda _d, _p, q: ('2' if q.startswith('INSERT INTO projects') else
                                            '0' if q.startswith('SELECT count(*)') else '')
                recovery = m.load_module(ROOT / 'deploy/customer/recovery.py',
                    m.sha(ROOT / 'deploy/customer/recovery.py'), 'actual_inventory_for_public_case')
                instance.recovery_module = lambda: recovery
                instance.wait_business_startup = lambda: None
                instance.inventory = lambda: {'containers': []}
                instance.upgrade_fingerprint = lambda *a, **kw: {'schema_migrations': {'count': 129, 'sha256': 'stable'}}
                instance.protected_upgrade_rows = lambda *a: {name: {'count': 1} for name in
                    ('users', 'orgs', 'projects', 'wallets', 'transactions', 'audit_logs', 'release_history')}
                def command(label, argv):
                    self.assertEqual(instance.loopback_port, 13001)
                    instance.phase = label
                    if label == 'check exact public upgrade without effects':
                        if fault == 'check-write': (directory / 'unexpected-write').write_text('must be detected')
                        return json.dumps({'writes': 1 if fault == 'writes-flag' else 0,
                            'source': {'package_sha256': 'f' * 64 if fault == 'source-package' else
                                       self.old['inputs']['config']['release']['package_sha256']},
                            'candidate': {'package_sha256': self.candidate['release']['package_sha256']}})
                    if label == 'read completed exact public upgrade resume':
                        raise RuntimeError('native resume boundary reached')
                    self.assertEqual(label, 'read original public upgrade status')
                    return '{"state":"not_started"}'
                instance.run = command
                def upgrade(*args):
                    self.assertEqual(instance.loopback_port, 13001)
                    self.assertEqual(old.loopback_port, 13001)
                    instance.phase = 'current native upgrade'
                    if fault != 'resume': raise RuntimeError('native write boundary reached')
                instance.run_native_upgrade = mock.Mock(side_effect=upgrade)
                with mock.patch.object(m, 'make_acceptance', return_value=old), mock.patch.object(m, 'same_public_ring'):
                    if fault not in (None, 'resume'):
                        with self.assertRaisesRegex(m.Stop, 'Public upgrade check/status changed'):
                            instance.installed_public_upgrade()
                        instance.run_native_upgrade.assert_not_called()
                        self.assertEqual(instance.phase, 'read original public upgrade status')
                    else:
                        with self.assertRaisesRegex(RuntimeError, 'native resume boundary reached' if fault == 'resume'
                                                    else 'native write boundary reached'):
                            instance.installed_public_upgrade()
                        instance.run_native_upgrade.assert_called_once_with(directory, None, None)
                        self.assertEqual(instance.phase, 'read completed exact public upgrade resume' if fault == 'resume'
                                         else 'current native upgrade')

    def test_actual_post_upgrade_checks_report_their_own_phase_and_safe_login_status(self):
        # Run the real signed-pair method and report boundary. Native services
        # are doubled; package/state/receipt files and their hashes are real.
        native_path = ROOT / '.github/tests/community-installer.py'
        native = m.load_module(native_path, m.sha(native_path), 'post_upgrade_report_fixture')
        ledger = native.source_contract(ROOT).ledger
        for fault in ('readiness', 'login'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as tmp, \
                 mock.patch.object(m.shutil, 'which', return_value='/fixture/docker'):
                root = Path(tmp).resolve()
                instance = m.make_acceptance(ROOT, root, self.candidate, {},
                    upgrade_source=(root / 'old-source', self.old))
                directory = root / 'installed-predecessor/instances/preserved'
                old = types.SimpleNamespace(instances=directory.parent, ring=root / 'ring',
                    result={'first_customer': {'organization_id': 2}}, release_sequence=22,
                    schema=instance.schema, preflight=lambda: None, installed_checks=lambda *a: None,
                    native_first_use_checks=lambda *a, **kw: None, command=lambda *a: [], compose=lambda *a: [])
                def prepare():
                    (directory / 'evidence').mkdir(parents=True)
                    (directory / 'install-state.json').write_bytes(b'original synthetic state')
                old.prepare = prepare
                old.run = lambda label, argv: json.dumps({'services': {'backend': {'environment': {}}}})
                old.sql = lambda _d, _p, q: '2' if q.startswith('INSERT INTO projects') else '0'
                protected = {name: {'count': 1} for name in
                    ('users', 'orgs', 'projects', 'wallets', 'transactions', 'audit_logs', 'release_history')}
                fingerprint = {'schema_migrations': {'count': len(ledger), 'sha256': 'synthetic-stable'}}
                instance.wait_business_startup = lambda: None
                instance.migration_audit = root / 'synthetic-migration-audit.jsonl'
                instance.observe_migration_owner = lambda: instance.migration_audit.write_text(
                    '{"native_owner_calls":1,"migration_owner_calls":0}\n')
                instance.inventory = lambda: {}
                instance.upgrade_fingerprint = lambda *a, **kw: fingerprint
                instance.protected_upgrade_rows = lambda *a: protected
                instance.protected_upgrade_files = lambda *a, **kw: {}
                recovery = types.SimpleNamespace(private_inventory=lambda *a: {}, verify_archive=mock.Mock())
                instance.recovery_module = lambda: recovery
                checked = {'writes': 0,
                    'source': {'package_sha256': self.old['inputs']['config']['release']['package_sha256']},
                    'candidate': {'package_sha256': self.candidate['release']['package_sha256']}}
                def run(label, argv):
                    instance.phase = label
                    return json.dumps(checked if label.startswith('check exact') else
                        {'state': 'not_started'} if label == 'read original public upgrade status' else {})
                instance.run = run
                instance.sql = lambda *a: json.dumps(ledger)
                def upgrade(*args):
                    backup = directory / 'upgrades/synthetic-operation/backup'
                    backup.mkdir(parents=True)
                    for name in ('database.dump', 'paired-files.tar'):
                        (backup / name).write_bytes(b'synthetic restored fixture')
                    pair = {'verification_database': 'synthetic_restore', 'restored_tables': fingerprint,
                        'dump_sha256': m.sha(backup / 'database.dump'), 'roots': [],
                        'files': {'archive_sha256': m.sha(backup / 'paired-files.tar'), 'files': {}}}
                    (backup / 'receipt.json').write_text(json.dumps(pair))
                    (directory / 'original-state.json').write_bytes((directory / 'install-state.json').read_bytes())
                    witness = {'state': 'completed', 'pending': None, 'source_ledger': list(ledger),
                        'candidate': checked['candidate'], 'source': checked['source'],
                        'operation_id': 'synthetic-operation', 'source_state_path': 'original-state.json',
                        'receipts': {'migrate': {'mode': 'same_schema', 'ledger_sha256': hashlib.sha256(
                            json.dumps(list(ledger), sort_keys=True, separators=(',', ':')).encode()).hexdigest()},
                            'cold-pair': {'receipt_sha256': m.sha(backup / 'receipt.json')},
                            **{'stop-' + role: {'clean_exit': True, 'old_processes_gone': True}
                               for role in ('backend', 'frontend')}}}
                    (directory / 'install-state.json').write_text(json.dumps({'upgrade': witness}))
                instance.run_native_upgrade = mock.Mock(side_effect=upgrade)
                def ready(url):
                    self.assertEqual(url, 'http://127.0.0.1:13001/health/ready')
                    if fault == 'readiness': raise m.Stop('Synthetic readiness refusal.')
                    return b'{}'
                instance.request = mock.Mock(side_effect=ready)
                instance.execute = instance.installed_public_upgrade
                instance.failure_facts = mock.Mock()
                opener = mock.Mock()
                error_body = mock.Mock(wraps=io.BytesIO(b'SYNTHETIC-PRIVATE-REPORT-MARKER'))
                opener.open.side_effect = m.urllib.error.HTTPError('http://127.0.0.1:13001/private',
                    503, 'SYNTHETIC-PRIVATE-REPORT-MARKER', {}, error_body)
                # The first factory call is the report owner; the next is its
                # actual retained-predecessor call. No method body is replaced.
                with mock.patch.object(m, 'validate'), mock.patch.object(m, 'native_guard', return_value={}), \
                     mock.patch.object(m, 'reference_sources'), mock.patch.object(m, 'upgrade_source_record', return_value=None), \
                     mock.patch.object(m.tempfile, 'mkdtemp', return_value=str(root)), \
                     mock.patch.object(m, 'make_acceptance', side_effect=[instance, old]), \
                     mock.patch.object(m, 'same_public_ring'), \
                     mock.patch.object(m, 'clean_environment', side_effect=lambda env: contextlib.nullcontext()), \
                     mock.patch.object(m.urllib.request, 'build_opener', return_value=opener), \
                     contextlib.redirect_stdout(io.StringIO()) as output, \
                     contextlib.redirect_stderr(io.StringIO()) as stderr:
                    self.assertEqual(m.execute(self.candidate, ROOT, self.current), 1)
                report = json.loads(output.getvalue())
                self.assertEqual(report['status'], 'failed')
                self.assertEqual(report['phase'], 'verify public upgrade readiness' if fault == 'readiness'
                                 else 'verify original administrator after public upgrade')
                self.assertEqual(report['error'], 'Synthetic readiness refusal.' if fault == 'readiness'
                                 else 'Fixture administrator login failed: HTTP 503.')
                self.assertNotIn('installed_public_upgrade', report)
                self.assertNotIn('SYNTHETIC-PRIVATE-REPORT-MARKER', output.getvalue() + stderr.getvalue())
                instance.run_native_upgrade.assert_called_once()
                recovery.verify_archive.assert_called_once()
                self.assertEqual(opener.open.call_count, 0 if fault == 'readiness' else 1)
                error_body.read.assert_not_called()
                if fault == 'login': error_body.close.assert_called_once()


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
        with contextlib.closing(opener.open.side_effect), \
             mock.patch.object(m.urllib.request,'build_opener',return_value=opener):
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

    def test_login_retains_only_safe_HTTP_status_and_closes_error_without_reading(self):
        marker = 'SYNTHETIC-PRIVATE-HTTP-MARKER'
        for status in (302, 401, 403, 429, 500, 502, 503, True, 99, 600, marker):
            body = mock.Mock(wraps=io.BytesIO(marker.encode()))
            opener = mock.Mock()
            opener.open.side_effect = m.urllib.error.HTTPError('http://127.0.0.1:13001/' + marker,
                status, marker, {'X-Synthetic': marker}, body)
            with self.subTest(status=status), \
                 mock.patch.object(m.urllib.request, 'build_opener', return_value=opener):
                with self.assertRaises(m.Stop) as error:
                    m.fixture_admin_login(m.FIRST_USE_TEST_INPUT, loopback_port=13001)
                suffix = 'HTTP ' + str(status) if type(status) is int and 100 <= status <= 599 else 'invalid HTTP status'
                self.assertEqual(str(error.exception), 'Fixture administrator login failed: ' + suffix + '.')
                self.assertNotIn(marker, str(error.exception))
                self.assertEqual(opener.open.call_count, 1)
                body.read.assert_not_called(); body.close.assert_called_once()

    def test_login_classifies_transport_and_JSON_without_exception_or_response_text(self):
        marker = 'SYNTHETIC-PRIVATE-TRANSPORT-MARKER'
        for error, label in ((m.urllib.error.URLError(ConnectionRefusedError(marker)), 'transport error'),
                             (m.urllib.error.URLError(TimeoutError(marker)), 'timeout'),
                             (TimeoutError(marker), 'timeout'), (RuntimeError(marker), 'response processing error')):
            opener = mock.Mock(); opener.open.side_effect = error
            with self.subTest(label=label), mock.patch.object(m.urllib.request, 'build_opener', return_value=opener):
                with self.assertRaises(m.Stop) as stopped: m.fixture_admin_login(m.FIRST_USE_TEST_INPUT)
                self.assertEqual(str(stopped.exception), 'Fixture administrator login failed: ' + label + '.')
                self.assertEqual(opener.open.call_count, 1)
        for body in (b'not-json', b'\xff', b'[]', b'null'):
            response, opener = self.response_fixture(body)
            with self.subTest(body=body), mock.patch.object(m.urllib.request, 'build_opener', return_value=opener):
                with self.assertRaises(m.Stop) as stopped: m.fixture_admin_login(m.FIRST_USE_TEST_INPUT)
                self.assertEqual(str(stopped.exception), 'Fixture administrator login failed: invalid JSON response.')
        response, opener = self.response_fixture(b'{"token":"first","token":"second"}')
        with mock.patch.object(m.urllib.request, 'build_opener', return_value=opener):
            with self.assertRaisesRegex(m.Stop, 'Duplicate JSON metadata field'):
                m.fixture_admin_login(m.FIRST_USE_TEST_INPUT)


if __name__=='__main__':unittest.main()
