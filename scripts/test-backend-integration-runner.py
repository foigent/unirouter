#!/usr/bin/env python3
"""Offline orchestration checks; the fake Go process never connects to services."""
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

REPO = Path(__file__).resolve().parent.parent
FAKE_GO = '''#!/usr/bin/env python3
import json, os, sys
args = sys.argv[1:]
if args == ["env", "CGO_ENABLED"]:
    print(os.environ.get("FAKE_CGO", "1"))
elif args == ["list", "-m"]:
    sys.exit(int(os.environ.get("FAKE_TOOLCHAIN_EXIT", "0")))
elif args == ["env", "CC"]:
    print(os.environ.get("FAKE_CC", "true"))
elif args == ["version"]:
    print("offline fake go for runner regression")
elif args[0] == "test":
    with open(os.environ["FAKE_CALLS"], "a") as out:
        out.write(json.dumps(args) + "\\n")
    package = next(a for a in args if a.startswith("./internal/"))
    failed = package == os.environ.get("FAKE_FAIL_PACKAGE")
    action = "fail" if failed else os.environ.get("FAKE_ACTION", "pass")
    print(json.dumps({"Action":"start", "Package":package}))
    print(json.dumps({"Action":action, "Package":package, "Test":"TestSynthetic"}))
    print(json.dumps({"Action":"fail" if failed else "pass", "Package":package}))
    sys.exit(9 if failed else 0)
else:
    raise RuntimeError("unexpected fake Go invocation")
'''


class RunnerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="ur-runner-offline-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        fake = self.root / "go"
        fake.write_text(FAKE_GO)
        fake.chmod(0o700)
        self.calls = self.root / "calls.jsonl"
        self.env = dict(os.environ, PATH=str(self.root) + os.pathsep + os.environ["PATH"],
                        TEST_DATABASE_URL="synthetic-unused", REDIS_ADDR="synthetic-unused",
                        REDIS_DB="0", REQUIRE_TEST_DATABASE="1", FAKE_CALLS=str(self.calls))

    def run_runner(self, *args):
        return subprocess.run(["bash", "scripts/test-backend-integration.sh", *args],
                              cwd=REPO, env=self.env, capture_output=True, text=True)

    def read_calls(self):
        return [json.loads(line) for line in self.calls.read_text().splitlines()] if self.calls.exists() else []

    def test_ci_and_logged_runs_use_same_five_sequential_groups(self):
        workflow = (REPO / ".github/workflows/ci.yml").read_text()
        self.assertEqual(workflow.count("run: bash scripts/test-backend-integration.sh"), 1)
        result = self.run_runner()
        self.assertEqual(result.returncode, 0, result.stderr)
        before = self.read_calls()
        self.assertEqual(len(before), 5)
        self.assertEqual([sum(arg.startswith("./") for arg in call) for call in before], [20, 3, 1, 1, 1])
        self.assertIn("./internal/runtimegeneration", before[0])
        self.assertEqual([arg for arg in before[1] if arg.startswith("./")],
                         ["./internal/sourcefacts", "./internal/mediajob", "./internal/contentcapture"])
        pattern = before[0][before[0].index("-run") + 1]
        self.assertIn("TestContentControl.*Integration", pattern)
        self.assertIn("TestContentPolicy.*Integration", pattern)
        self.assertIn("TestContentCaptureMigration.*", pattern)
        self.assertIn("TestHistoricalM115RetirementPermitsOnlyProvedSuffix", pattern)
        self.assertIn("TestRetirementRacesFastAndSlowRenewWithoutReusingStoppedIdentity", pattern)
        for name in (
            "TestAsyncMediaImageMigrationContractAndRollbackGuard",
            "TestReceiverFeedCredentialMigrationContractAndDownRefusal",
            "TestXInsightFeedControlMigrationEnforcesStagedAndRollbackLifecycle",
            "TestM115ToM123CompatibleExpandPreservesExistingKey",
            "TestSourceCompatibleExpandRetainsOnlySupportedDormantGenerations",
            "TestScopedContentMigrationPreservesOldPolicyAndRefusesNewHistoryRollback",
            "TestResponseCachePolicySwitchReleasesRealRedisReservationIntegration",
            "TestXInsightMachinePolicyDelegationCASAndReplayIntegration",
            "TestXInsightAuditHumanSystemServiceProjectionIntegration",
            "TestXInsightAuditM123HistoricalActorBackfillIntegration",
            "TestXInsightFutureSourceEnrollmentACReadAndRevokeIntegration",
            "TestXInsightBindExistingFactsAndContentIntegration",
            "TestScopedPolicyHTTPEnterpriseAndProjectCASIntegration",
        ):
            with self.subTest(name=name):
                self.assertIsNotNone(re.fullmatch(pattern, name))
        self.assertEqual([call[call.index("-run") + 1] for call in before[2:]], [
            "^TestAgentCapabilityPostgresAuthorityAndRevocationIntegration$",
            "^TestP6F07PostgresRedisMultiInstanceIntegration$",
            "^TestP6F21RedisMultiInstanceLeaseAndFaultIntegration$"])
        self.calls.unlink()
        result = self.run_runner("--log-dir", str(self.root / "logs"))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual([[arg for arg in call if arg != "-json"] for call in self.read_calls()], before)
        self.assertEqual(len(list((self.root / "logs").glob("*.summary.json"))), 5)

    def test_readonly_preflight_does_not_run_tests_or_create_logs(self):
        logs = self.root / "logs"
        result = self.run_runner("--check", "--log-dir", str(logs))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.read_calls(), [])
        self.assertFalse(logs.exists())

    def test_missing_race_dependencies_fail_before_execution(self):
        for key, value in (("FAKE_CGO", "0"), ("FAKE_CC", "/missing/synthetic-cc")):
            with self.subTest(key=key):
                self.env[key] = value
                self.assertEqual(self.run_runner("--check").returncode, 2)
                self.assertEqual(self.read_calls(), [])
                del self.env[key]

    def test_unavailable_python_fails_before_tests_or_log_creation(self):
        broken = self.root / "broken-python"
        broken.write_text('#!/bin/sh\nexit 7\n')
        broken.chmod(0o700)
        logs = self.root / "logs"
        for python in (str(self.root / "missing-python"), str(broken)):
            for check in ([], ["--check"]):
                with self.subTest(python=python, check=check):
                    self.env["PYTHON"] = python
                    result = self.run_runner(*check, "--log-dir", str(logs))
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(self.read_calls(), [])
                    self.assertFalse(logs.exists())

    def test_fresh_check_reuses_python_preflight_without_running_tests(self):
        self.env["PYTHON"] = str(self.root / "missing-python")
        logs = self.root / "fresh-logs"
        result = subprocess.run(["bash", "scripts/test-fresh-install.sh", "--check", str(logs)],
                                cwd=REPO, env=self.env, capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.read_calls(), [])
        self.assertFalse(logs.exists())

    def test_incompatible_toolchain_fails_before_execution(self):
        self.env["FAKE_TOOLCHAIN_EXIT"] = "1"
        self.assertEqual(self.run_runner("--check").returncode, 1)
        self.assertEqual(self.read_calls(), [])

    def test_failure_propagates_and_remaining_groups_are_not_run(self):
        self.env["FAKE_FAIL_PACKAGE"] = "./internal/sourcefacts"
        logs = self.root / "logs"
        result = self.run_runner("--log-dir", str(logs))
        self.assertEqual(result.returncode, 9, result.stderr)
        self.assertEqual(len(self.read_calls()), 2)
        self.assertFalse((logs / "agentcap.jsonl").exists())
        summary = json.loads((logs / "source-media.summary.json").read_text())
        self.assertEqual(summary["status"], "FAIL")
        self.assertEqual(summary["exit_code"], 9)

    def test_existing_evidence_directory_is_not_overwritten(self):
        logs = self.root / "logs"
        logs.mkdir()
        sentinel = logs / "retained"
        sentinel.write_text("original")
        self.assertNotEqual(self.run_runner("--log-dir", str(logs)).returncode, 0)
        self.assertEqual(self.read_calls(), [])
        self.assertEqual(sentinel.read_text(), "original")

    def test_all_skipped_is_not_reported_as_execution_pass(self):
        self.env["FAKE_ACTION"] = "skip"
        result = self.run_runner("--log-dir", str(self.root / "logs"))
        self.assertEqual(result.returncode, 1)
        self.assertEqual(len(self.read_calls()), 1)


if __name__ == "__main__":
    unittest.main()
