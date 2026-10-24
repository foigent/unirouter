#!/usr/bin/env python3
"""Offline regression for the pre-Preview13 template mistakenly reused in14."""
import contextlib
import copy
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('descriptor', ROOT / 'community-install-descriptor.py')
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class DescriptorTests(unittest.TestCase):
    def setUp(self):
        self.images = {name: repo + '@sha256:' + str(n) * 64 for n, (name, repo) in enumerate([
            ('backend', 'ghcr.io/foigent/unirouter-backend'), ('frontend', 'ghcr.io/foigent/unirouter-frontend'),
            ('postgres', 'postgres'), ('redis', 'redis'), ('prometheus', 'prom/prometheus'), ('grafana', 'grafana/grafana')], 1)}
        self.previous = {'schema_version': m.SCHEMA, 'images': dict(self.images)}
        self.apps = {x: self.images[x].split('@')[0] + '@sha256:' + 'a' * 64 for x in m.APPS}
        self.expected = dict(self.images, **self.apps)
        self.observations = {'images': [{'role': role, 'Os': 'linux', 'Architecture': 'amd64',
            'Id': 'sha256:' + 'b' * 64, 'RepoDigests': [ref]} for role, ref in self.expected.items()]}
        self.publisher = {'images': [{'role': role, 'repository': ref.split('@')[0],
            'registry_digest_reference': ref} for role, ref in self.apps.items()]}

    def test_native_predecessor_and_new_app_images_select_exact_six(self):
        self.assertEqual(m.select_descriptor(self.previous, self.apps, self.observations),
                         {'schema_version': m.SCHEMA, 'images': self.expected})

    def test_original_build_template_is_rejected_before_output(self):
        bad = copy.deepcopy(self.previous)
        for role in m.ROLES - m.APPS:
            ref = bad['images'][role]
            bad['images'][role] = 'docker.io/' + ('library/' if '/' not in ref.split('@')[0] else '') + ref
        with self.assertRaises(m.DescriptorError):
            m.select_descriptor(bad, self.apps, self.observations)

    def test_repository_or_digest_change_is_not_normalized_away(self):
        for role in m.ROLES:
            for ref in ('elsewhere.invalid/image@sha256:' + 'c' * 64, self.expected[role].split('@')[0] + '@sha256:' + 'c' * 64):
                bad = copy.deepcopy(self.observations)
                next(x for x in bad['images'] if x['role'] == role)['RepoDigests'] = [ref]
                with self.subTest(role=role, ref=ref), self.assertRaises(m.DescriptorError):
                    m.select_descriptor(self.previous, self.apps, bad)

    def test_missing_duplicated_or_unknown_role_refuses(self):
        for rows in (self.observations['images'][:-1], self.observations['images'][:-1] + self.observations['images'][:1],
                     self.observations['images'][:-1] + [dict(self.observations['images'][-1], role='other')]):
            with self.assertRaises(m.DescriptorError):
                m.select_descriptor(self.previous, self.apps, {'images': rows})

    def test_ambiguous_digest_or_wrong_native_platform_refuses(self):
        for delta in ({'RepoDigests': [self.expected['backend']] * 2}, {'Os': 'windows'},
                      {'Architecture': 'arm64'}, {'Id': 'not-an-image'}):
            bad = copy.deepcopy(self.observations)
            bad['images'][0].update(delta)
            with self.assertRaises(m.DescriptorError):
                m.select_descriptor(self.previous, self.apps, bad)

    def test_publisher_cannot_substitute_other_app_repository(self):
        bad = copy.deepcopy(self.publisher)
        bad['images'][0]['repository'] = 'ghcr.io/other/backend'
        with self.assertRaises(m.DescriptorError):
            m.publisher_apps(bad)

    def test_check_is_pure_output_exclusive_and_hashes_required(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            argv = []
            for name, value in [('previous', self.previous), ('publisher', self.publisher), ('observations', self.observations)]:
                path = root / (name + '.json')
                path.write_bytes(m.wire(value))
                argv += ['--' + name, str(path), '--' + name + '-sha256', hashlib.sha256(path.read_bytes()).hexdigest()]
            out = root / 'install.json'
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(m.main(argv + ['--out', str(out)]), 0)
            before = {x.name: (x.stat().st_mtime_ns, x.read_bytes()) for x in root.iterdir()}
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(m.main(argv + ['--check-descriptor', str(out)]), 0)
            self.assertEqual(before, {x.name: (x.stat().st_mtime_ns, x.read_bytes()) for x in root.iterdir()})
            with contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(m.main(argv + ['--out', str(out)]), 1)
                bad = list(argv)
                bad[3] = '0' * 64
                self.assertEqual(m.main(bad + ['--check-descriptor', str(out)]), 1)
            self.assertEqual(before, {x.name: (x.stat().st_mtime_ns, x.read_bytes()) for x in root.iterdir()})


if __name__ == '__main__':
    unittest.main(verbosity=2)
