#!/usr/bin/env python3
"""Offline checks for exact two-stage fixture selection; never invokes Docker."""
import importlib.util
from pathlib import Path
from types import SimpleNamespace
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('maintenance_native', Path(__file__).with_name('maintenance-native.py'))
native = importlib.util.module_from_spec(spec)
spec.loader.exec_module(native)


class TransitionSelection(unittest.TestCase):
    def test_all_three_sources_are_exact_and_original_evidence_is_retained(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            head = 'a' * 40
            checked_out = {}
            commands = []
            def run(self, label, command, **kwargs):
                commands.append((label, [str(x) for x in command]))
                if label == 'read exact checkout':
                    return head
                if label.startswith('clone exact '):
                    path = Path(command[-1]); path.mkdir()
                    (path / 'VERSION').write_text('synthetic\n')
                if label.startswith('checkout exact '):
                    checked_out[Path(command[2]).name] = command[-1]
                if label.startswith('read ') and label.endswith(' tree'):
                    return 'b' * 40
                return ''
            owner = SimpleNamespace(rollout=object())
            with patch.object(native.Acceptance, 'run', run), patch.object(native, 'module', return_value=owner):
                work = root / 'fixture'; work.mkdir()
                fixture = native.Acceptance(root, work, head)
            self.assertEqual(checked_out, {'old-source': native.OLD_COMMIT,
                'new-source': native.RETIREMENT_COMMIT, 'successor-source': head})
            self.assertEqual(fixture.old_head, native.OLD_HEAD)
            self.assertEqual(fixture.new_head, native.RETIREMENT_HEAD)
            self.assertEqual(fixture.result['sources']['successor']['commit'], head)
            self.assertEqual(fixture.result['transitions'], [])
            self.assertTrue(all(command[0] == 'git' for _, command in commands))

    def successor_fixture(self, root, *, wrong_image=False):
        fixture = object.__new__(native.Acceptance)
        fixture.root = root
        fixture.sources = {name: {'directory': str(root / name), 'commit': str(i) * 40,
                                 'tree': str(i) * 40, 'version': 'synthetic'}
                           for i, name in enumerate(('old', 'new', 'successor'), 1)}
        migration_dir = root / 'new' / 'migrations'; migration_dir.mkdir(parents=True)
        for head in (native.OLD_HEAD, native.RETIREMENT_HEAD):
            (migration_dir / (head + '.up.sql')).write_text('-- fixture\n')
        fixture.images = {name: name + '-tag' for name in fixture.sources}
        fixture.image_ids = {name: name + '-image' for name in fixture.sources}
        fixture.old_overlay, fixture.new_overlay, fixture.successor_overlay = ('old.yml', 'new.yml', 'successor.yml')
        fixture.restore_backend = 'ur-maintenance-restore-backend'
        running = [True]; calls = []
        fixture.inspect = lambda name: {'Image': 'wrong-image' if wrong_image else 'old-image', 'State': {'Running': running[0]}}
        def run(label, command):
            calls.append((label, command)); running[0] = False; return ''
        fixture.run = run
        fixture.trigger_names = lambda: set()
        fixture.sql = lambda query: native.RETIREMENT_HEAD
        fixture.backend = lambda name, args, **kwargs: calls.append(('admit', name, args))
        fixture.retire_and_migrate = lambda: calls.append(('retire', fixture.sources['old']['commit'],
                                                          fixture.sources['new']['commit'], fixture.old_head, fixture.new_head))
        fixture.restore = lambda: calls.append(('restore', fixture.restore_database, fixture.restore_backend))
        return fixture, calls

    def test_second_transition_uses_m126_artifact_and_a_distinct_restore_database(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture, calls = self.successor_fixture(Path(directory))
            with patch.object(native, 'module', return_value=SimpleNamespace(rollout=object())):
                fixture.successor_transition()
            self.assertEqual(calls[0][1], ['docker', 'stop', 'ur-maintenance-restore-backend'])
            self.assertIn(('retire', '2' * 40, '3' * 40, native.RETIREMENT_HEAD, native.NEW_HEAD), calls)
            self.assertIn(('restore', 'ur-maintenance-restore-postgres-m126', 'ur-maintenance-restore-backend-m126'), calls)
            self.assertEqual(fixture.images['old'], 'new-tag')
            self.assertEqual(fixture.images['new'], 'successor-tag')
            self.assertEqual(fixture.old_overlay, 'new.yml')
            self.assertEqual(fixture.new_overlay, 'successor.yml')
            self.assertEqual(fixture.evidence.name, 'communication-maintenance-evidence')
            self.assertEqual(fixture.input.name, 'communication-maintenance.json')
            self.assertEqual(fixture.old_ledger, [native.OLD_HEAD, native.RETIREMENT_HEAD])

    def test_wrong_restored_container_refuses_before_stop_or_next_stage(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture, calls = self.successor_fixture(Path(directory), wrong_image=True)
            with self.assertRaisesRegex(native.Refused, 'identity changed'):
                fixture.successor_transition()
            self.assertEqual(calls, [])

    def test_driver_never_admits_host_execution_outside_disposable_runner(self):
        with patch.object(native.sys, 'argv', ['maintenance-native.py', '--expected-commit', 'a' * 40,
                                             '--report', '/uncreated-report']), patch.dict(native.os.environ, {}, clear=True), \
             patch.object(native.subprocess, 'run', side_effect=AssertionError('must not execute')):
            with self.assertRaises(native.Refused):
                native.main()


if __name__ == '__main__':
    unittest.main(verbosity=2)
