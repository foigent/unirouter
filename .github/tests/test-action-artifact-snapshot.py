"""Offline proof that every Actions upload class preserves and bounds its input."""
import importlib.util
import os
import subprocess
import sys
import tempfile
import unittest
import zipfile
from pathlib import Path
from unittest import mock

HELPER = Path(__file__).with_name("action-artifact-snapshot.py")
spec = importlib.util.spec_from_file_location("action_artifact_snapshot", HELPER)
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class SnapshotTests(unittest.TestCase):
    def test_complete_success_and_failure_files_fit_the_bound(self):
        for kind, (limit, name) in m.KINDS.items():
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as temp:
                scratch = Path(temp)
                source = scratch / name
                expected = {"community-first-use/browser-process.log": b"failed test\n",
                            "overview/navigation/result.json": b"{}"} if kind == "browser" else {name: os.urandom(1024)}
                for path, contents in expected.items():
                    file = source / path if kind == "browser" else source
                    file.parent.mkdir(parents=True, exist_ok=True)
                    file.write_bytes(contents)
                frozen = m.freeze(source, kind, scratch)
                archive = scratch / "test.zip"
                with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as out:
                    for path in expected:
                        original = source / path if kind == "browser" else source
                        original.write_bytes(b"changed after freeze")
                        out.write(frozen / path if kind == "browser" else frozen, path)
                with zipfile.ZipFile(archive) as staged:
                    self.assertIsNone(staged.testzip())
                    self.assertEqual({name: staged.read(name) for name in staged.namelist()}, expected)
                self.assertLess(archive.stat().st_size, limit)

    def test_every_oversized_class_refuses_before_exposing_upload_path(self):
        for kind, (limit, name) in m.KINDS.items():
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as temp:
                scratch = Path(temp)
                source = scratch / name
                file = source / "overview/oversize.png" if kind == "browser" else source
                file.parent.mkdir(parents=True, exist_ok=True)
                with file.open("wb") as body:
                    body.truncate(limit)
                outputs = scratch / "out"
                outputs.touch()
                proc = subprocess.run([sys.executable, "-I", "-B", HELPER, kind, source],
                    env=dict(os.environ, RUNNER_TEMP=temp, GITHUB_OUTPUT=str(outputs)), capture_output=True)
                self.assertNotEqual(proc.returncode, 0)
                self.assertIn(b"ZIP bound", proc.stderr)
                self.assertEqual(outputs.read_bytes(), b"")
                self.assertFalse(list(scratch.glob("ur-artifact-*")))

    def test_absent_optional_and_bad_browser_files(self):
        with tempfile.TemporaryDirectory() as temp:
            scratch = Path(temp)
            source = scratch / m.KINDS["browser"][1]
            self.assertIsNone(m.freeze(source, "browser", scratch))
            self.assertIsNone(m.freeze(scratch / m.KINDS["installer"][1], "installer", scratch))
            with self.assertRaisesRegex(m.ArtifactError, "Missing required"):
                m.freeze(scratch / m.KINDS["coverage"][1], "coverage", scratch)
            folder = source / "overview"
            folder.mkdir(parents=True)
            path = folder / "hidden.key"
            path.write_bytes(b"synthetic forbidden entry")
            with self.assertRaisesRegex(m.ArtifactError, "Unexpected browser evidence file"):
                m.freeze(source, "browser", scratch)
            path.rename(scratch / "synthetic-target")
            (folder / "eligible.png").symlink_to(scratch / "synthetic-target")
            with self.assertRaisesRegex(m.ArtifactError, "Unexpected browser evidence file"):
                m.freeze(source, "browser", scratch)

    def test_inventory_change_fails_closed(self):
        with tempfile.TemporaryDirectory() as temp:
            scratch = Path(temp)
            source = scratch / "coverage.out"
            source.write_bytes(b"coverage")
            original = m.inventory(source, "coverage")
            with mock.patch.object(m, "inventory", side_effect=(original, {})):
                with self.assertRaisesRegex(m.ArtifactError, "inventory changed"):
                    m.freeze(source, "coverage", scratch)


if __name__ == "__main__":
    unittest.main()
