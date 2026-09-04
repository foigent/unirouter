#!/usr/bin/env python3
"""Behavioral recovery tests; no Docker or Provider calls."""
import importlib.util
from pathlib import Path
from types import SimpleNamespace
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("runtime_ingress", Path(__file__).with_name("runtime-ingress.py"))
ingress = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ingress)


class MembershipTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="unirouter-ingress-test-")
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        self.path = self.directory / "upstreams.conf"
        self.path.write_bytes(ingress.membership("both"))
        self.before = {"before": self.path.read_bytes(), "after": ingress.membership("canary"),
                       "configuration_sha256": "a" * 64, "workers": {"1:100"}}
        self.args = SimpleNamespace(directory=self.directory, container="test",
                                    expected_config_sha256=ingress.digest(self.before["before"]),
                                    expected_nginx_sha256="a" * 64, wait_seconds=10)
        self.calls = []

    def execute(self, container, *args):
        self.calls.append(args)
        if args[0] == "cat":
            return self.path.read_bytes()
        return b""

    def test_canonical_carrier_and_every_backend_upstream(self):
        source = Path(__file__).parent.parent / "nginx/runtime-membership/upstreams.conf"
        self.assertEqual(source.read_bytes(), ingress.membership("both"))
        withdrawn = ingress.membership("canary")
        self.assertNotIn(b"uniroute-api:8080", withdrawn)
        self.assertEqual(withdrawn.count(b"uniroute-api-canary:8080"), 2)

    def test_stale_plan_does_not_write_or_reload(self):
        self.args.expected_config_sha256 = "0" * 64
        with patch.object(ingress, "docker_exec", self.execute), self.assertRaisesRegex(RuntimeError, "predecessor"):
            ingress.apply(self.args, self.before)
        self.assertEqual(self.path.read_bytes(), self.before["before"])
        self.assertEqual(self.calls, [])

    def test_validation_failure_restores_before_without_reload(self):
        def fail(container, *args):
            if args == ("nginx", "-t"):
                raise RuntimeError("invalid")
            return self.execute(container, *args)
        with patch.object(ingress, "docker_exec", fail), self.assertRaisesRegex(RuntimeError, "invalid"):
            ingress.apply(self.args, self.before)
        self.assertEqual(self.path.read_bytes(), self.before["before"])
        self.assertNotIn(("nginx", "-s", "reload"), self.calls)

    def test_ambiguous_reload_keeps_candidate_and_backends(self):
        def fail(container, *args):
            if args == ("nginx", "-s", "reload"):
                raise RuntimeError("unknown reload result")
            return self.execute(container, *args)
        with patch.object(ingress, "docker_exec", fail), self.assertRaisesRegex(RuntimeError, "unknown"):
            ingress.apply(self.args, self.before)
        self.assertEqual(self.path.read_bytes(), self.before["after"])

    def test_old_proxy_with_delayed_body_must_finish_before_success(self):
        identities = [{"1:100", "2:200"}, {"2:200"}]
        with patch.object(ingress, "docker_exec", self.execute), \
                patch.object(ingress, "workers", side_effect=identities) as observed, \
                patch.object(ingress.time, "sleep"):
            self.assertEqual(ingress.apply(self.args, self.before), "old_proxy_workers_exited")
        self.assertEqual(observed.call_count, 2)

    def test_old_proxy_deadline_aborts_without_reverting_or_stopping(self):
        with patch.object(ingress, "docker_exec", self.execute), \
                patch.object(ingress, "workers", return_value={"1:100", "2:200"}), \
                patch.object(ingress.time, "monotonic", side_effect=[0, 0, 0, 20]), \
                patch.object(ingress.time, "sleep"), \
                self.assertRaisesRegex(RuntimeError, "retain old backend"):
            ingress.apply(self.args, self.before)
        self.assertEqual(self.path.read_bytes(), self.before["after"])
        self.assertFalse(any("stop" in call or "kill" in call for call in self.calls))

    def test_configuration_cannot_force_old_streams_closed_or_resend(self):
        base = b"include /etc/nginx/runtime-membership/upstreams.conf;\n"
        ingress.check_configuration(base + b"worker_shutdown_timeout 0;\n")
        for extra in (b"worker_shutdown_timeout 120s;\n",
                      b"proxy_next_upstream error non_idempotent;\n",
                      b"proxy_pass http://uniroute-api:8080;\n"):
            with self.assertRaises(RuntimeError):
                ingress.check_configuration(base + extra)


if __name__ == "__main__":
    unittest.main()
