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

    def test_overlay_replacement_preserves_explicit_private_mode(self):
        self.path.chmod(0o600)
        ingress.replace(self.path, b"private runtime", 0o600)
        self.assertEqual(self.path.read_bytes(), b"private runtime")
        self.assertEqual(self.path.stat().st_mode & 0o777, 0o600)

    def test_owner_diagnostic_is_closed_sqlstate_and_preserves_held_locks(self):
        response = SimpleNamespace(returncode=1, stderr=b"secret=value; detail SQLSTATE 40001\nprivate query", stdout=b"")
        with self.path.open("rb") as lock, patch.object(ingress, "LOCK_FDS", [lock.fileno()]), \
                patch.object(ingress.subprocess, "run", return_value=response) as command:
            with self.assertRaises(RuntimeError) as error:
                ingress.owner_command("exact", "prepare", {"change_id": "same"})
            self.assertIn("SQLSTATE 40001", str(error.exception))
            self.assertNotIn("secret", str(error.exception))
            self.assertNotIn("private", str(error.exception))
            self.assertEqual(command.call_args.kwargs["pass_fds"], (lock.fileno(),))


class CoordinatedMembershipTests(unittest.TestCase):
    def setUp(self):
        self.health={role:{"instance":"uri_"+letter*32,"artifact_digest":"sha256:"+letter*64,"lease_required":True} for role,letter in (("stable","a"),("canary","b"))}
        self.before={"before":ingress.membership("both"),"after":ingress.membership("canary"),"before_state":"both","configuration_sha256":"c"*64,"workers":{"10:100"},"health":self.health}
        self.args=SimpleNamespace(change_id="withdraw",desired="canary",expected_config_sha256=ingress.digest(self.before["before"]),expected_nginx_sha256="c"*64,expected_container_id="d"*64,container="nginx")
        self.events=[]
        self.plan=None

    def owner(self, container, action, document=None):
        self.events.append(action)
        if action=="inspect":
            return {"head":"bootstrap","head_status":"applied","generations":[dict(i,ingress_state="receiving") for i in ingress.state_members("both",self.health)]}
        if action=="prepare":
            self.plan=document
            return {"status":"pending"}
        if action=="complete":
            self.completion=document
            return {"status":"applied" if document["old_workers_gone"] else "ambiguous"}
        raise AssertionError(action)

    def test_intent_precedes_reload_and_completion_waits_for_old_workers(self):
        def reload(*args):
            self.events.append("reload_and_wait")
            self.assertEqual(self.plan["membership_before"],ingress.state_members("both",self.health))
            self.assertEqual(self.plan["membership_after"],ingress.state_members("canary",self.health))
            return "old_proxy_workers_exited"
        with patch.object(ingress,"owner_command",self.owner),patch.object(ingress,"apply",reload):
            ingress.coordinated_apply(self.args,self.before)
        self.assertEqual(self.events,["inspect","prepare","reload_and_wait","complete"])
        self.assertTrue(self.completion["old_workers_gone"])

    def test_ambiguous_reload_never_confirms_withdrawal(self):
        with patch.object(ingress,"owner_command",self.owner),patch.object(ingress,"apply",side_effect=RuntimeError("unknown reload")),patch.object(ingress,"docker_exec",return_value=self.before["after"]),self.assertRaisesRegex(RuntimeError,"unknown reload"):
            ingress.coordinated_apply(self.args,self.before)
        self.assertFalse(self.completion["old_workers_gone"])

    def test_failed_prepare_does_not_touch_nginx(self):
        def refuse(container,action,document=None):
            if action=="prepare":raise RuntimeError("incompatible receiver")
            return self.owner(container,action,document)
        with patch.object(ingress,"owner_command",refuse),patch.object(ingress,"apply") as reload,self.assertRaisesRegex(RuntimeError,"incompatible"):
            ingress.coordinated_apply(self.args,self.before)
        reload.assert_not_called()

    def test_full_maintenance_withdrawal_keeps_the_worker_and_database_protocol(self):
        import base64
        self.args.desired = "none"
        self.args.maintenance = True
        self.before["after"] = ingress.membership("none")
        self.before["health"] = {}  # Recovery works after both receivers exit.
        def owner(container, action, document=None):
            if action == "read":
                return {"status": "applied", "plan": {
                    "membership_after": ingress.state_members("both", self.health),
                    "desired_config_digest": ingress.digest(self.before["before"]),
                    "desired_config_bytes": base64.b64encode(self.before["before"]).decode()}}
            return self.owner(container, action, document)
        def reload(*args):
            self.assertEqual(self.plan["membership_after"], [])
            self.assertEqual(len(self.plan["membership_before"]), 2)
            self.events.append("reload_and_wait")
            return "old_proxy_workers_exited"
        with patch.object(ingress, "owner_command", owner), patch.object(ingress, "apply", reload):
            ingress.coordinated_apply(self.args, self.before)
        self.assertTrue(self.completion["old_workers_gone"])
        self.assertEqual(self.events, ["inspect", "prepare", "reload_and_wait", "complete"])
        self.assertEqual(self.before["after"].count(b"127.0.0.1:1 down"), 2)
        self.assertNotIn(b"uniroute-api", self.before["after"])

    def test_full_withdrawal_rejects_a_drifted_predecessor_without_reload(self):
        self.args.desired = "none"
        self.args.maintenance = True
        self.before["after"] = ingress.membership("none")
        self.before["health"] = {}
        def owner(container, action, document=None):
            if action == "read":
                return {"status": "applied", "plan": {"membership_after": [], "desired_config_digest": "wrong"}}
            return self.owner(container, action, document)
        with patch.object(ingress, "owner_command", owner), patch.object(ingress, "apply") as reload:
            with self.assertRaisesRegex(RuntimeError, "predecessor differs"):
                ingress.coordinated_apply(self.args, self.before)
        reload.assert_not_called()

    def test_import_cannot_silently_change_observed_membership(self):
        with patch.object(ingress,"owner_command",return_value={"head":"","generations":[]}),patch.object(ingress,"apply") as reload,self.assertRaisesRegex(RuntimeError,"initial ingress import"):
            ingress.coordinated_apply(self.args,self.before)
        reload.assert_not_called()


if __name__ == "__main__":
    unittest.main()
