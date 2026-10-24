#!/usr/bin/env python3
"""Offline checks of the actual native fixture and shipped bootstrap contract."""
import ast
import hashlib
import importlib.util
import json
from pathlib import Path
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]

def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

fixture = load('installer_fixture', ROOT / '.github/tests/community-installer.py')
bootstrap = load('actual_bootstrap', ROOT / 'deploy/community/bootstrap.py')

class InstallerFixtureContract(unittest.TestCase):
    def test_workflow_covers_selector_and_shared_first_use_inputs(self):
        # A change to an imported selector/assertion must exercise the real
        # native fixture, even when its main driver is otherwise unchanged.
        text = (ROOT / '.github/workflows/community-installer.yml').read_text()
        paths = text.split('    paths:\n', 1)[1].split('  workflow_dispatch:', 1)[0]
        for path in ('.github/tests/community-install-descriptor.py',
                     '.github/tests/test-community-install-descriptor.py',
                     '.github/tests/community-release-acceptance.py'):
            self.assertIn('      - ' + path + '\n', paths)

    def raw(self, schema):
        return json.dumps({'schema_version': 'unirouter.community-bootstrap.v1',
            'platform': {'os': 'linux', 'arch': 'amd64'},
            'release': {'version': '1.13.0', 'commit': 'a' * 40, 'release_sequence': 1,
                        'key_id': 'installer-native-test', 'target_schema': schema},
            'assets': {key: {'url': 'https://example.invalid/' + key, 'size': 1,
                            'sha256': 'b' * 64} for key in bootstrap.ASSETS}}).encode()

    def test_fixture_matches_actual_latest_migration_and_both_install_owners(self):
        head = sorted((ROOT / 'migrations').glob('*.up.sql'))[-1].name.removesuffix('.up.sql')
        self.assertEqual(fixture.SCHEMA, head)
        self.assertEqual(fixture.SCHEMA, bootstrap.SCHEMA)
        tree = ast.parse((ROOT / 'deploy/customer/install.py').read_text())
        schema = next(ast.literal_eval(n.value) for n in tree.body if isinstance(n, ast.Assign)
                      and any(isinstance(target, ast.Name) and target.id == 'SCHEMA' for target in n.targets))
        self.assertEqual(schema, fixture.SCHEMA)

    def test_current_fixture_is_parsed_by_real_bootstrap_before_prepare(self):
        raw = self.raw(fixture.SCHEMA)
        fetch = object()
        with patch.object(bootstrap, 'prepare') as prepare:
            fixture.prepare_bootstrap_fixture(bootstrap, raw, hashlib.sha256(raw).hexdigest(), Path('unused'), fetch)
        anchor = prepare.call_args.args[0]
        self.assertIsInstance(anchor, bootstrap.Bootstrap)
        self.assertEqual(anchor.release['target_schema'], fixture.SCHEMA)
        self.assertIs(prepare.call_args.kwargs['fetch'], fetch)

    def test_old_fixture_is_a_sanitized_failure_before_prepare(self):
        raw = self.raw('000126_commercial_license_retirement')
        with patch.object(bootstrap, 'prepare') as prepare:
            with self.assertRaisesRegex(fixture.Failed, '^community bootstrap preparation failed$'):
                fixture.prepare_bootstrap_fixture(bootstrap, raw, hashlib.sha256(raw).hexdigest(), Path('unused'), object())
        prepare.assert_not_called()

    def test_prepare_error_is_also_normalized_without_child_output(self):
        raw = self.raw(fixture.SCHEMA)
        with patch.object(bootstrap, 'prepare', side_effect=bootstrap.BootstrapError('untrusted child output')):
            with self.assertRaisesRegex(fixture.Failed, '^community bootstrap preparation failed$'):
                fixture.prepare_bootstrap_fixture(bootstrap, raw, hashlib.sha256(raw).hexdigest(), Path('unused'), object())

if __name__ == '__main__':
    unittest.main(verbosity=2)
