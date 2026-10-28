#!/usr/bin/env python3
"""Offline content reuse tests; no network, credentials or signing operation."""
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("materials", Path(__file__).with_name("community-release-materials.py"))
M = importlib.util.module_from_spec(spec); spec.loader.exec_module(M)


def write(root, path, raw):
    target = root / path; target.parent.mkdir(parents=True, exist_ok=True); target.write_bytes(raw)
    return {"path": path, "size": len(raw), "sha256": hashlib.sha256(raw).hexdigest()}


class MaterialsTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="ur-materials-offline-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.current, self.previous = self.root / "current", self.root / "previous"
        self.current.mkdir(); self.previous.mkdir()
        self.rows = [write(self.current, path, ("synthetic source body " + path).encode()) for path in M.OWNER]
        self.request = {"schema_version": M.SCHEMA, "artifacts": self.rows}
        self.prior = {"schema_version": "unirouter.upgrade-manifest.v2", "artifacts": copy.deepcopy(self.rows)}
        for row in self.rows:
            write(self.previous, row["path"], (self.current / row["path"]).read_bytes())

    def test_warm_sources_reuse_all_stable_groups_and_keep_release_bindings_fresh(self):
        result = M.select_sources(self.request, self.current, self.previous, self.prior)
        groups = {row['group']: row for row in result['groups']}
        self.assertTrue(all(groups[name]['reused'] for name in M.GROUPS if name != 'release'))
        self.assertFalse(groups['release']['reused'])
        for row in result['sources']:
            expected = self.current if M.OWNER[row['path']] == 'release' else self.previous
            self.assertEqual(str(expected / row['path']), row['file_path'])
        self.assertEqual(self.rows, self.request['artifacts'])

    def test_previous_large_archives_need_no_duplicate_current_copy(self):
        for name in ('corresponding_source', 'system_materials'):
            for path in M.GROUPS[name]:
                (self.current / path).unlink()  # exact files created by this fixture
        result = M.select_sources(self.request, self.current, self.previous, self.prior)
        self.assertEqual(3, sum(row['file_path'].startswith(str(self.previous)) and M.OWNER[row['path']] in ('corresponding_source', 'system_materials') for row in result['sources']))
        self.assertFalse(any((self.current / path).exists() for name in ('corresponding_source', 'system_materials') for path in M.GROUPS[name]))

    def test_one_changed_material_invalidates_its_group_only(self):
        changed = M.GROUPS['runtime'][0]
        replacement = write(self.current, changed, b'synthetic successor installer')
        self.request['artifacts'] = [replacement if row['path'] == changed else row for row in self.rows]
        result = M.select_sources(self.request, self.current, self.previous, self.prior)
        self.assertEqual({'runtime': False, 'notices': True, 'corresponding_source': True, 'system_materials': True, 'release': False},
                         {row['group']: row['reused'] for row in result['groups']})
        self.assertEqual(b'synthetic source body ' + changed.encode(), (self.previous / changed).read_bytes())

    def test_media_type_participates_in_group_identity(self):
        self.request = copy.deepcopy(self.request)
        self.request['artifacts'][0]['media_type'] = 'text/x-python'
        result = M.select_sources(self.request, self.current, self.previous, self.prior)
        self.assertFalse(next(row for row in result['groups'] if row['group'] == 'runtime')['reused'])

    def test_corrupt_partial_extra_and_linked_prior_inputs_refuse_without_overwrite(self):
        for kind in ('bytes', 'missing', 'extra', 'symlink'):
            with self.subTest(kind=kind):
                # Separate prior snapshot per negative case, with task-owned files.
                previous = self.root / kind; previous.mkdir()
                for row in self.rows: write(previous, row['path'], (self.current / row['path']).read_bytes())
                target = previous / M.GROUPS['corresponding_source'][0]
                if kind == 'bytes': target.write_bytes(b'changed prior archive')
                elif kind == 'missing': target.unlink()
                elif kind == 'extra': write(previous, 'payload/licenses/unexpected.txt', b'extra')
                else:
                    target.unlink(); target.symlink_to(self.current / M.GROUPS['corresponding_source'][0])
                before = {str(path): path.read_bytes() for path in previous.rglob('*') if path.is_file() and not path.is_symlink()}
                with self.assertRaises(M.Stop): M.select_sources(self.request, self.current, previous, self.prior)
                self.assertEqual(before, {str(path): path.read_bytes() for path in previous.rglob('*') if path.is_file() and not path.is_symlink()})

    def test_stale_current_bytes_cannot_hide_behind_identical_prior_group(self):
        (self.current / M.GROUPS['runtime'][0]).write_bytes(b'unrecorded current source edit')
        with self.assertRaisesRegex(M.Stop, 'bytes_changed'):
            M.select_sources(self.request, self.current, self.previous, self.prior)

    def test_unknown_duplicate_and_partial_group_inputs_refuse(self):
        for kind in ('path', 'duplicate', 'missing', 'fields', 'oversize'):
            with self.subTest(kind=kind):
                request = copy.deepcopy(self.request)
                if kind == 'path': request['artifacts'][0]['path'] = 'payload/../credential'
                elif kind == 'duplicate': request['artifacts'].append(request['artifacts'][0])
                elif kind == 'missing': request['artifacts'].pop(0)
                elif kind == 'fields': request['artifacts'][0]['approved'] = True
                else: request['artifacts'][0]['size'] = M.MAX_BYTES + 1
                with self.assertRaises(M.Stop): M.select_sources(request, self.current)
        with self.assertRaises(M.Stop): M.strict_json(b'{"artifacts":[],"artifacts":[]}')
        with self.assertRaises(M.Stop): M.strict_json(b'[]' + b' ' * M.MAX_METADATA)

    def test_current_release_specific_metadata_always_uses_current_bytes(self):
        path = 'payload/provenance/build-provenance.json'
        replacement = write(self.current, path, b'{"synthetic_version":"next","current_observation":true}')
        self.request['artifacts'] = [replacement if row['path'] == path else row for row in self.rows]
        result = M.select_sources(self.request, self.current, self.previous, self.prior)
        entry = next(row for row in result['sources'] if row['path'] == path)
        self.assertEqual(str(self.current / path), entry['file_path'])
        self.assertNotEqual(entry['sha256'], next(row for row in self.prior['artifacts'] if row['path'] == path)['sha256'])


if __name__ == '__main__': unittest.main()
