#!/usr/bin/env python3
"""Offline regressions for misleading or incomplete Go test evidence."""
import importlib.util
import hashlib
import json
from pathlib import Path
import tempfile
import subprocess
import sys
import unittest

spec = importlib.util.spec_from_file_location("summary", Path(__file__).with_name("summarize-go-test.py"))
summary = importlib.util.module_from_spec(spec)
spec.loader.exec_module(summary)


class SummaryTests(unittest.TestCase):
    def test_cli_binds_each_runner_and_rejects_missing_or_wrong_identity(self):
        repo = Path(__file__).resolve().parent.parent
        with tempfile.TemporaryDirectory() as root:
            log = Path(root) / "go.jsonl"
            log.write_text('\n'.join(json.dumps(e) for e in [
                {"Action": "pass", "Package": "a", "Test": "TestSynthetic"},
                {"Action": "pass", "Package": "a"}]))
            target = log.with_suffix(".summary.json")
            command = [sys.executable, "-B", str(repo / "scripts/summarize-go-test.py"),
                       str(log), "--exit-code", "0", "--group", "fresh-install"]
            for flags in ([], ["--runner", "missing.sh"],
                          ["--runner", "scripts/test-backend-integration.sh"]):
                with self.subTest(flags=flags):
                    result = subprocess.run(command + flags, capture_output=True, text=True)
                    self.assertEqual(result.returncode, 2, result.stderr)
                    self.assertFalse(target.exists())
            for runner, groups in summary.RUNNER_GROUPS.items():
                for group in groups:
                    with self.subTest(runner=runner, group=group):
                        result = subprocess.run(command[:-1] + [group, "--runner", runner],
                                                capture_output=True, text=True)
                        self.assertEqual(result.returncode, 0, result.stderr)
                        source = json.loads(target.read_text())["source"]
                        self.assertEqual(source["runner_path"], runner)
                        self.assertEqual(source["runner_sha256"], hashlib.sha256((repo / runner).read_bytes()).hexdigest())
                        self.assertEqual(source["summarizer_sha256"], hashlib.sha256((repo / "scripts/summarize-go-test.py").read_bytes()).hexdigest())
                        target.unlink()

    def run_log(self, events, code=0):
        with tempfile.TemporaryDirectory() as root:
            path = Path(root) / "go.jsonl"
            path.write_text("\n".join(json.dumps(event) for event in events))
            return summary.summarize(path, code)

    def test_package_count_is_distinct_from_test_and_subtest_count(self):
        result = self.run_log([
            {"Action": "pass", "Package": "a", "Test": name}
            for name in ("TestOne", "TestOne/subtest")
        ] + [{"Action": "pass", "Package": "a"}])
        self.assertEqual(result["packages"]["pass"], 1)
        self.assertEqual(result["tests_including_subtests"]["pass"], 2)
        self.assertEqual(result["status"], "PASS")

    def test_failures_keep_line_locations_without_printing_payloads(self):
        result = self.run_log([
            {"Action": "fail", "Package": "a", "Test": "TestOne"},
            {"Action": "fail", "Package": "a"}], 1)
        self.assertEqual(result["status"], "FAIL")
        self.assertEqual(result["failure_locations"][0]["line"], 1)

    def test_zero_selected_tests_and_all_skipped_remain_visible(self):
        result = self.run_log([{"Action": "pass", "Package": "a"}])
        self.assertEqual(result["status"], "FAIL")
        result = self.run_log([{"Action": "skip", "Package": "a", "Test": "TestOne"},
                               {"Action": "pass", "Package": "a"}])
        self.assertEqual(result["tests_including_subtests"]["skip"], 1)
        self.assertEqual(result["status"], "FAIL")

    def test_incomplete_or_nonzero_execution_cannot_become_a_pass(self):
        events = [{"Action": "start", "Package": "a"},
                  {"Action": "pass", "Package": "a", "Test": "TestOne"}]
        self.assertEqual(self.run_log(events)["status"], "FAIL")
        events.append({"Action": "pass", "Package": "a"})
        self.assertEqual(self.run_log(events, 2)["status"], "FAIL")


if __name__ == "__main__":
    unittest.main()
