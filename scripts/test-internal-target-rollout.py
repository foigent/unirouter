#!/usr/bin/env python3
"""Bounded rollout authority/recovery regressions; no Docker or paid calls."""
import copy
import contextlib
import datetime
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("rollout", Path(__file__).with_name("internal-target-rollout.py"))
rollout = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rollout)


def fixture():
    image = {"Id": "sha256:" + "a" * 64, "Config": {"Env": ["PATH=/bin"], "Cmd": ["./uniroute"], "WorkingDir": "/app"}}
    service = {"command": None, "environment": {"RUNTIME_ENVIRONMENT": "development", "DB_DSN": "fixture-only"},
               "ports": [{"target": 8080, "published": "8080", "host_ip": "127.0.0.1", "protocol": "tcp"}],
               "volumes": [{"type": "bind", "source": "/release/migrations", "target": "/app/migrations", "read_only": True}],
               "stop_grace_period": "35m", "networks": {"default": None}}
    container = {"Id": "old", "Image": image["Id"], "State": {"Running": False, "ExitCode": 0, "OOMKilled": False},
                 "Config": {"Env": image["Config"]["Env"] + ["RUNTIME_ENVIRONMENT=development", "DB_DSN=fixture-only"],
                            "Cmd": ["./uniroute"], "Entrypoint": None, "User": "", "WorkingDir": "/app", "StopTimeout": 2100},
                 "HostConfig": {"PortBindings": {"8080/tcp": [{"HostIp": "127.0.0.1", "HostPort": "8080"}]},
                                "RestartPolicy": {"Name": "no"}, "LogConfig": {"Type": "json-file", "Config": {}}},
                 "Mounts": [{"Type": "bind", "Source": "/release/migrations", "Destination": "/app/migrations", "RW": False}],
                 "NetworkSettings": {"Networks": {"unirouter_default": {"IPAMConfig": None}}}}
    return service, container, image


class EnvironmentCheckTests(unittest.TestCase):
    def run_check(self, *, client='29.0.1', server='28.0.4', compose=b'2.39.2', container=True,
                  started='2026-09-09T01:02:03.123456789Z', absence=True):
        self.commands = []
        def invoke(args, **kwargs):
            self.commands.append(args)
            self.assertEqual(kwargs['env'], rollout.ENV)
            self.assertNotIn('DOCKER_CONTEXT', kwargs['env'])
            if args[1] == 'version':
                wire = json.dumps({'Client': {'Version': client}, 'Server': {'Version': server}}).encode()
            elif args[1:3] == ['compose', 'version']:
                wire = compose
            elif args[1] == 'ps':
                wire = b'a' * 64 if container else b''
            elif args[1] == 'inspect' and args[2].startswith('ur-environment-check-'):
                self.assertRegex(args[2], r'^ur-environment-check-[a-f0-9]{24}$')
                return SimpleNamespace(returncode=1, stdout=b'[]', stderr=(
                    f'error: no such object: {args[2]}'.encode() if absence else b'daemon unavailable'))
            elif args[1:] == ['inspect', 'a' * 64]:
                wire = json.dumps([{'Id': 'a'*64, 'State': {'Running': True, 'StartedAt': started}}]).encode()
            else:
                self.fail(f'Unexpected or mutating command: {args}')
            return SimpleNamespace(returncode=0, stdout=wire, stderr=b'')
        with patch.object(rollout.subprocess, 'run', side_effect=invoke):
            return rollout.environment_check()

    def test_supported_installed_environment_is_read_only_and_redacted(self):
        result = self.run_check()
        self.assertEqual(result['environment_check'], 'passed')
        self.assertTrue(result['timestamp_observed'])
        self.assertEqual(result['running_timestamp']['container_id'], 'a'*64)
        self.assertNotIn('Config', json.dumps(result))

    def test_fresh_host_reports_missing_timestamp_scope(self):
        self.assertFalse(self.run_check(container=False)['timestamp_observed'])

    def test_unsupported_or_malformed_versions_stop_before_inspection(self):
        for values in ({'client':'27.1.1'}, {'server':'30.0.0'}, {'server':'29.0.0-rc1'},
                       {'server':None}, {'compose':b'2.24.3'}, {'compose':b'2.24.4-rc1'}, {'compose':b'unknown'}):
            with self.subTest(values=values), self.assertRaises(RuntimeError):
                self.run_check(**values)
            self.assertFalse(any(c[1] == 'inspect' for c in self.commands))

    def test_absence_and_timestamp_fail_closed(self):
        for values in ({'absence':False}, {'started':'2026-09-09T01:02:03+24:00'}, {'started':'unknown'}):
            with self.subTest(values=values), self.assertRaises(RuntimeError):
                self.run_check(**values)

    def test_duplicate_version_json_and_wrong_container_cannot_pass(self):
        with patch.object(rollout, 'run', return_value=b'{"Client":{},"Client":{}}'), self.assertRaisesRegex(RuntimeError, 'duplicate'):
            rollout.environment_check()


class CounterTransitionTests(unittest.TestCase):
    def test_named_transition_is_finite_and_strict(self):
        rollout.validate_transition(None)
        rollout.validate_transition({"kind":"stream_counter_expand_carrier", "up_sha256":"a"*64, "down_sha256":"b"*64})
        rollout.validate_transition({"kind":"source_facts_expand_carrier", "up_sha256":"a"*64, "down_sha256":"b"*64})
        rollout.validate_transition({"kind":"source_observations_expand_carrier", "up_sha256":"a"*64, "down_sha256":"b"*64})
        for before, after in ((rollout.COUNTER_V2,rollout.COUNTER_V3),(rollout.COUNTER_V3,rollout.COUNTER_V2)):
            rollout.validate_transition({"kind":"stream_counter_writer","from":before,"to":after})
        for value in ({"kind":"any_schema"}, {"kind":"stream_counter_expand_carrier","up_sha256":"a"*64},
                      {"kind":"stream_counter_writer","from":rollout.COUNTER_V2,"to":"future"},
                      {"kind":"stream_counter_writer","from":rollout.COUNTER_V2,"to":rollout.COUNTER_V2},
                      {"kind":"stream_counter_writer","from":rollout.COUNTER_V2,"to":rollout.COUNTER_V3,"skip_checks":True}):
            with self.subTest(value=value), self.assertRaises(RuntimeError):
                rollout.validate_transition(value)

    def test_writer_overlay_changes_one_shared_key_only(self):
        before=b"x-runtime: &runtime\n  RUNTIME_IMAGE_DIGEST: sha256:abc\n  RUNTIME_ENVIRONMENT: development\n"
        change={"kind":"stream_counter_writer","from":rollout.COUNTER_V2,"to":rollout.COUNTER_V3}
        expected=before.replace(b"  RUNTIME_ENVIRONMENT:", b"  STREAM_COUNTER_VERSION: unirouter.stream-counter.v3\n  RUNTIME_ENVIRONMENT:")
        self.assertEqual(expected,rollout.writer_overlay(before,change))
        self.assertEqual(expected.replace(b"stream-counter.v3", b"stream-counter.v2"),
                         rollout.writer_overlay(expected,dict(change,**{"from":rollout.COUNTER_V3,"to":rollout.COUNTER_V2})))
        for invalid in (before+before, before.replace(b"RUNTIME_IMAGE_DIGEST",b"OTHER_KEY"),
                        expected.replace(b"stream-counter.v3",b"future")):
            with self.assertRaises(RuntimeError): rollout.writer_overlay(invalid,change)

    def test_pending_expand_rejects_existing_migration_rewrite(self):
        transition={"kind":"stream_counter_expand_carrier","up_sha256":"a"*64,"down_sha256":"b"*64}
        old=["000113_glm_stream_function_tools_qualification",rollout.CAPABILITY_HEAD]
        tree="\n".join("migrations/"+name+".up.sql" for name in old).encode()
        for changes in (b"M\tmigrations/000114_capability_delivery_expand.up.sql\n", b"A\tmigrations/000116_other.up.sql\n", b""):
            with patch.object(rollout,"run",side_effect=[tree,b"oldcommit",changes]):
                with self.assertRaisesRegex(RuntimeError,"only add"):
                    rollout.validate_pending_counter_carrier(Path("/old"),Path("/new"),transition,old)
        with patch.object(rollout,"run",return_value=tree), self.assertRaisesRegex(RuntimeError,"complete exact"):
            rollout.validate_pending_counter_carrier(Path("/old"),Path("/new"),transition,old[:1])

    def test_pending_expand_verifies_both_exact_bytes(self):
        old=["000113_glm_stream_function_tools_qualification", rollout.CAPABILITY_HEAD]
        tree="\n".join("migrations/"+name+".up.sql" for name in old).encode()
        changes="\n".join("A\tmigrations/"+rollout.COUNTER_HEAD+"."+direction+".sql" for direction in ("up","down")).encode()
        with tempfile.TemporaryDirectory() as directory:
            new=Path(directory).resolve(); (new/"migrations").mkdir()
            transition={"kind":"stream_counter_expand_carrier"}
            for direction in ("up","down"):
                wire=(direction+" exact fixture").encode()
                (new/"migrations"/(rollout.COUNTER_HEAD+"."+direction+".sql")).write_bytes(wire)
                transition[direction+"_sha256"]=rollout.digest(wire)
            with patch.object(rollout,"run",side_effect=[tree,b"oldcommit",changes]):
                rollout.validate_pending_counter_carrier(Path("/old"),new,transition,old)
            (new/"migrations"/(rollout.COUNTER_HEAD+".down.sql")).write_text("unreviewed replacement")
            with patch.object(rollout,"run",side_effect=[tree,b"oldcommit",changes]), self.assertRaisesRegex(RuntimeError,"bytes differ"):
                rollout.validate_pending_counter_carrier(Path("/old"),new,transition,old)


    def test_source_expand_requires_exact_m115_ledger_and_reviewed_pair(self):
        old=["000113_glm_stream_function_tools_qualification", rollout.CAPABILITY_HEAD, rollout.COUNTER_HEAD]
        tree="\n".join("migrations/"+name+".up.sql" for name in old).encode()
        changes="\n".join("A\tmigrations/"+rollout.SOURCE_HEAD+"."+direction+".sql" for direction in ("up","down")).encode()
        with tempfile.TemporaryDirectory() as directory:
            new=Path(directory).resolve(); (new/"migrations").mkdir()
            transition={"kind":"source_facts_expand_carrier"}
            for direction in ("up","down"):
                wire=(direction+" exact M116 fixture").encode()
                (new/"migrations"/(rollout.SOURCE_HEAD+"."+direction+".sql")).write_bytes(wire)
                transition[direction+"_sha256"]=rollout.digest(wire)
            with patch.object(rollout,"run",side_effect=[tree,b"oldcommit",changes]):
                rollout.validate_pending_counter_carrier(Path("/old"),new,transition,old)
            with patch.object(rollout,"run",return_value=tree), self.assertRaisesRegex(RuntimeError,"predecessor"):
                rollout.validate_pending_counter_carrier(Path("/old"),new,transition,old[:-1])
            with patch.object(rollout,"run",side_effect=[tree,b"oldcommit",changes+b"\nM\tmigrations/000001_init_schema.up.sql"]), self.assertRaisesRegex(RuntimeError,"migration pair"):
                rollout.validate_pending_counter_carrier(Path("/old"),new,transition,old)
            (new/"migrations"/(rollout.SOURCE_HEAD+".down.sql")).write_text("changed")
            with patch.object(rollout,"run",side_effect=[tree,b"oldcommit",changes]), self.assertRaisesRegex(RuntimeError,"bytes differ"):
                rollout.validate_pending_counter_carrier(Path("/old"),new,transition,old)


    def test_observation_expand_requires_exact_m116_and_preserves_older_bytes(self):
        old=["000113_glm_stream_function_tools_qualification", rollout.CAPABILITY_HEAD, rollout.COUNTER_HEAD, rollout.SOURCE_HEAD]
        tree="\n".join("migrations/"+name+".up.sql" for name in old).encode()
        changes="\n".join("A\tmigrations/"+rollout.OBSERVATION_HEAD+"."+direction+".sql" for direction in ("up","down")).encode()
        with tempfile.TemporaryDirectory() as directory:
            new=Path(directory).resolve(); (new/"migrations").mkdir()
            transition={"kind":"source_observations_expand_carrier"}
            for direction in ("up","down"):
                wire=(direction+" exact M117 fixture").encode()
                (new/"migrations"/(rollout.OBSERVATION_HEAD+"."+direction+".sql")).write_bytes(wire)
                transition[direction+"_sha256"]=rollout.digest(wire)
            with patch.object(rollout,"run",side_effect=[tree,b"oldcommit",changes]):
                rollout.validate_pending_counter_carrier(Path("/old"),new,transition,old)
            with patch.object(rollout,"run",return_value=tree), self.assertRaisesRegex(RuntimeError,"predecessor"):
                rollout.validate_pending_counter_carrier(Path("/old"),new,transition,old[:-1])
            with patch.object(rollout,"run",side_effect=[tree,b"oldcommit",changes+b"\nM\tmigrations/"+rollout.SOURCE_HEAD.encode()+b".up.sql"]), self.assertRaisesRegex(RuntimeError,"migration pair"):
                rollout.validate_pending_counter_carrier(Path("/old"),new,transition,old)
            (new/"migrations"/(rollout.OBSERVATION_HEAD+".down.sql")).write_text("unreviewed replacement")
            with patch.object(rollout,"run",side_effect=[tree,b"oldcommit",changes]), self.assertRaisesRegex(RuntimeError,"bytes differ"):
                rollout.validate_pending_counter_carrier(Path("/old"),new,transition,old)


    def test_budget_expand_requires_exact_m117_and_preserves_older_bytes(self):
        old=["000113_glm_stream_function_tools_qualification", rollout.CAPABILITY_HEAD, rollout.COUNTER_HEAD, rollout.SOURCE_HEAD, rollout.OBSERVATION_HEAD]
        tree="\n".join("migrations/"+name+".up.sql" for name in old).encode()
        changes="\n".join("A\tmigrations/"+rollout.BUDGET_HEAD+"."+direction+".sql" for direction in ("up","down")).encode()
        with tempfile.TemporaryDirectory() as directory:
            new=Path(directory).resolve(); (new/"migrations").mkdir()
            transition={"kind":"source_budgets_expand_carrier"}
            for direction in ("up","down"):
                wire=(direction+" exact M118 fixture").encode()
                (new/"migrations"/(rollout.BUDGET_HEAD+"."+direction+".sql")).write_bytes(wire)
                transition[direction+"_sha256"]=rollout.digest(wire)
            with patch.object(rollout,"run",side_effect=[tree,b"oldcommit",changes]):
                rollout.validate_pending_counter_carrier(Path("/old"),new,transition,old)
            with patch.object(rollout,"run",return_value=tree), self.assertRaisesRegex(RuntimeError,"predecessor"):
                rollout.validate_pending_counter_carrier(Path("/old"),new,transition,old[:-1])
            with patch.object(rollout,"run",side_effect=[tree,b"oldcommit",changes+b"\nM\tmigrations/"+rollout.SOURCE_HEAD.encode()+b".up.sql"]), self.assertRaisesRegex(RuntimeError,"migration pair"):
                rollout.validate_pending_counter_carrier(Path("/old"),new,transition,old)
            (new/"migrations"/(rollout.BUDGET_HEAD+".down.sql")).write_text("unreviewed replacement")
            with patch.object(rollout,"run",side_effect=[tree,b"oldcommit",changes]), self.assertRaisesRegex(RuntimeError,"bytes differ"):
                rollout.validate_pending_counter_carrier(Path("/old"),new,transition,old)


class SchemaBridgeTransitionTests(unittest.TestCase):
    def test_m121_bridge_values_are_present_in_both_compose_service_environments(self):
        # Read the shipped Compose service maps, not a duplicate hand-written
        # environment fixture. The fixed indentation is their current format.
        root = Path(__file__).resolve().parent.parent

        def service_environment(filename, service):
            lines = (root / filename).read_text().splitlines()
            start = lines.index(f"  {service}:") + 1
            end = next((i for i in range(start, len(lines))
                        if re.fullmatch(r"  [a-z][a-z0-9-]*:", lines[i])), len(lines))
            section = lines[start:end]
            self.assertEqual(1, section.count("    environment:"))
            env_start = section.index("    environment:") + 1
            values = {}
            for line in section[env_start:]:
                if line and not line.lstrip().startswith("#") and not line.startswith("      "):
                    break
                match = re.fullmatch(r"      ([A-Z][A-Z0-9_]*):\s*(.*?)\s*", line)
                if match:
                    self.assertNotIn(match[1], values)
                    values[match[1]] = match[2]
            return values

        stable = service_environment("docker-compose.yml", "backend")
        canary = service_environment("docker-compose.canary.yml", "backend-canary")
        bridged = (*rollout.SOURCE_DEFAULTS, "COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE",
                   "ASYNC_MEDIA_ENABLED", "ASYNC_MEDIA_REQUEST_TTL_HOURS")
        for key in bridged:
            with self.subTest(key=key):
                self.assertIn(key, stable)
                self.assertEqual(stable[key], canary.get(key))
        self.assertEqual("${ASYNC_MEDIA_REQUEST_TTL_HOURS:-24}",
                         stable["ASYNC_MEDIA_REQUEST_TTL_HOURS"])
        self.assertEqual('"false"', canary["XINSIGHT_CONNECTOR_AGENT_ENABLED"])

    def test_every_exact_m115_to_m122_suffix_and_predecessor(self):
        for index, head in enumerate(rollout.BRIDGE_HEADS[:-1]):
            with self.subTest(head=head), tempfile.TemporaryDirectory() as directory:
                new=Path(directory).resolve(); (new/"migrations").mkdir()
                old=["000113_glm_stream_function_tools_qualification", rollout.CAPABILITY_HEAD,
                     *rollout.BRIDGE_HEADS[:index+1]]
                suffix=rollout.BRIDGE_HEADS[index+1:]
                transition={"kind":"schema_bridge_expand_carrier","from_head":head,"migration_pairs":[]}
                changes=[]
                for version in suffix:
                    pair={"version":version}
                    for direction in ("up","down"):
                        wire=(version+" "+direction+" exact reviewed fixture").encode()
                        (new/"migrations"/(version+"."+direction+".sql")).write_bytes(wire)
                        pair[direction+"_sha256"]=rollout.digest(wire)
                        changes.append("A\tmigrations/"+version+"."+direction+".sql")
                    transition["migration_pairs"].append(pair)
                tree="\n".join("migrations/"+name+".up.sql" for name in old).encode()
                delta="\n".join(changes).encode()
                rollout.validate_transition(transition)
                for invalid in (dict(transition,migration_pairs=transition["migration_pairs"][:-1]),
                                dict(transition,migration_pairs=[*transition["migration_pairs"],transition["migration_pairs"][-1]]),
                                dict(transition,migration_pairs=[dict(transition["migration_pairs"][0],version="000124_unapproved"),*transition["migration_pairs"][1:]])):
                    with self.assertRaises(RuntimeError):
                        rollout.validate_transition(invalid)
                with patch.object(rollout,"run",side_effect=[tree,b"oldcommit",delta]):
                    rollout.validate_pending_counter_carrier(Path("/old"),new,transition,old)
                with patch.object(rollout,"run",return_value=tree), self.assertRaisesRegex(RuntimeError,"predecessor"):
                    rollout.validate_pending_counter_carrier(Path("/old"),new,transition,old[:-1])
                with patch.object(rollout,"run",side_effect=[tree,b"oldcommit",delta+b"\nM\tmigrations/000001_init_schema.up.sql"]), self.assertRaisesRegex(RuntimeError,"migration pairs"):
                    rollout.validate_pending_counter_carrier(Path("/old"),new,transition,old)
                if len(transition["migration_pairs"]) > 1:
                    reversed_pairs=dict(transition,migration_pairs=list(reversed(transition["migration_pairs"])))
                    with self.assertRaises(RuntimeError):
                        rollout.validate_transition(reversed_pairs)
                last=transition["migration_pairs"][-1]
                (new/"migrations"/(last["version"]+".down.sql")).write_text("changed")
                with patch.object(rollout,"run",side_effect=[tree,b"oldcommit",delta]), self.assertRaisesRegex(RuntimeError,"bytes differ"):
                    rollout.validate_pending_counter_carrier(Path("/old"),new,transition,old)

    def test_schema_bridge_shape_rejects_missing_reordered_and_extra_pairs(self):
        pairs=[{"version":name,"up_sha256":"a"*64,"down_sha256":"b"*64} for name in rollout.BRIDGE_HEADS[1:]]
        valid={"kind":"schema_bridge_expand_carrier","from_head":rollout.COUNTER_HEAD,"migration_pairs":pairs}
        rollout.validate_transition(valid)
        for invalid in (dict(valid,migration_pairs=pairs[:-1]),
                        dict(valid,migration_pairs=[pairs[1],pairs[0],*pairs[2:]]),
                        dict(valid,migration_pairs=[*pairs,pairs[-1]]),
                        dict(valid,migration_pairs=[dict(pairs[0],up_sha256="not-a-digest"),*pairs[1:]]),
                        dict(valid,from_head="000114_capability_delivery_expand"),
                        dict(valid,allow_unknown=True)):
            with self.subTest(invalid=invalid), self.assertRaises(RuntimeError):
                rollout.validate_transition(invalid)

    def test_compose_defaults_only_add_absent_disabled_values(self):
        expected={"RUNTIME_VERSION":"1.2.77", "SOURCE_FACTS_ENABLED":"false"}
        actual=dict(expected)
        rollout.add_disabled_source_defaults(expected,actual)
        self.assertEqual({"RUNTIME_VERSION":"1.2.77",**rollout.SOURCE_DEFAULTS},expected)
        for key,default in rollout.SOURCE_DEFAULTS.items():
            enabled="true" if default=="false" else "/secret"
            with self.subTest(key=key):
                previous=dict(expected)
                with self.assertRaisesRegex(RuntimeError,"disabled source"):
                    rollout.add_disabled_source_defaults(dict(expected,**{key:enabled}),actual)
                with self.assertRaisesRegex(RuntimeError,"disabled source"):
                    rollout.add_disabled_source_defaults(expected,dict(actual,**{key:enabled}))
                self.assertEqual(previous,expected)

    def test_m123_carrier_keeps_live_capture_and_only_adds_inert_peer_default(self):
        original = {**rollout.SOURCE_DEFAULTS, "SOURCE_FACTS_ENABLED": "true",
                    "SOURCE_OBSERVATIONS_ENABLED": "true", "SOURCE_CONTENT_ENABLED": "true",
                    "SOURCE_CONTENT_FULL_ENABLED": "true", "SOURCE_CONTENT_STORAGE_ROOT": "/data/content",
                    "SOURCE_CONTENT_STORAGE_IDENTITY": "original-store",
                    "SOURCE_CONTENT_STORAGE_TOKEN_FILE": "/run/secrets/original.token",
                    "COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE": "original-source"}
        future = dict(original, XINSIGHT_CONNECTOR_STATIC_PEER_JSON="")
        expected = dict(original)
        before = copy.deepcopy((original, future))
        rollout.admit_inert_static_peer_default(expected, original, future, [])
        fingerprint = rollout.preserve_m123_source_config(expected, original, future, [])
        self.assertEqual("true", expected["SOURCE_CONTENT_FULL_ENABLED"])
        self.assertEqual(tuple(original[key] for key in (*rollout.SOURCE_DEFAULTS,
                         "COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE")), fingerprint)
        self.assertEqual(before, (original, future))
        with self.assertRaisesRegex(RuntimeError, "introduce static peer authority"):
            rollout.admit_inert_static_peer_default(dict(original), original,
                dict(future, XINSIGHT_CONNECTOR_STATIC_PEER_JSON='{"enabled":true}'), [])
        for key, value in (("SOURCE_FACTS_ENABLED", "false"),
                           ("SOURCE_CONTENT_STORAGE_IDENTITY", "other-store"),
                           ("COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE", "other-source")):
            with self.subTest(key=key), self.assertRaisesRegex(RuntimeError, "M123 source"):
                rollout.preserve_m123_source_config(expected, original, dict(future, **{key:value}), [])

    def test_m121_carrier_preserves_live_source_and_only_adds_disabled_media_ttl(self):
        original = {"SOURCE_FACTS_ENABLED":"true", "SOURCE_OBSERVATIONS_ENABLED":"true",
                    "SOURCE_BUDGET_SNAPSHOTS_ENABLED":"false", "SOURCE_CONTENT_ENABLED":"true",
                    "SOURCE_CONTENT_FULL_ENABLED":"true", "SOURCE_CONTENT_STORAGE_ROOT":"/data/content",
                    "SOURCE_CONTENT_STORAGE_IDENTITY":"existing-store",
                    "SOURCE_CONTENT_STORAGE_TOKEN_FILE":"/run/secrets/content.token",
                    "COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE":"existing-producer",
                    "ASYNC_MEDIA_ENABLED":"false"}
        actual = dict(original)
        future = dict(original, ASYNC_MEDIA_REQUEST_TTL_HOURS="24")
        expected = dict(original)
        fingerprint = rollout.preserve_m121_source_config(expected, actual, future, ["PATH=/bin"])
        self.assertEqual("24", expected["ASYNC_MEDIA_REQUEST_TTL_HOURS"])
        self.assertEqual("true", expected["SOURCE_CONTENT_FULL_ENABLED"])
        self.assertEqual(tuple(original[key] for key in (*rollout.SOURCE_DEFAULTS,
                         "COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE")), fingerprint)
        for changed, value in (("SOURCE_FACTS_ENABLED", "false"),
                               ("SOURCE_CONTENT_STORAGE_IDENTITY", "another-store"),
                               ("COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE", "another-producer")):
            with self.subTest(changed=changed), self.assertRaisesRegex(RuntimeError, "source identity or capture"):
                rollout.preserve_m121_source_config(dict(original), actual, dict(future, **{changed:value}), [])
        with self.assertRaisesRegex(RuntimeError, "media already disabled"):
            rollout.preserve_m121_source_config(dict(original), dict(actual, ASYNC_MEDIA_ENABLED="true"), future, [])
        with self.assertRaisesRegex(RuntimeError, "TTL default"):
            rollout.preserve_m121_source_config(dict(original), actual,
                                                 dict(future, ASYNC_MEDIA_REQUEST_TTL_HOURS="48"), [])
        with self.assertRaisesRegex(RuntimeError, "TTL default"):
            rollout.preserve_m121_source_config(dict(original, ASYNC_MEDIA_REQUEST_TTL_HOURS="24"),
                                                 dict(actual, ASYNC_MEDIA_REQUEST_TTL_HOURS="24"), future, [])
        with self.assertRaisesRegex(RuntimeError, "producer identity"):
            empty = dict(original, COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE="")
            rollout.preserve_m121_source_config(empty, empty, dict(future, COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE=""), [])


class ArtifactOwnerTests(unittest.TestCase):
    def test_docker_timestamp_fraction_and_timezone(self):
        for wire, microsecond, offset in (
            ("2026-09-08T12:05:50Z", 0, 0),
            ("2026-09-08T12:05:50.4Z", 400000, 0),
            ("2026-09-08T12:05:50.449Z", 449000, 0),
            ("2026-09-08T12:05:50.449763+00:00", 449763, 0),
            ("2026-09-08T12:05:50.449763833Z", 449763, 0),
            ("2026-09-08T12:05:50.449763833+02:30", 449763, 150),
            ("2026-09-08T12:05:50.449763833-02:30", 449763, -150),
        ):
            with self.subTest(wire=wire):
                expected = datetime.datetime(2026, 9, 8, 12, 5, 50, microsecond,
                    datetime.timezone(datetime.timedelta(minutes=offset)))
                self.assertEqual(expected, rollout.docker_started_at(wire))

    def test_docker_timestamp_rejects_invalid_or_ambiguous_input(self):
        for wire in (None, "", "2026-09-08T12:05:50", "2026-09-08T12:05:50.Z",
                     "2026-09-08T12:05:50.4497638330Z", "2026-09-08T12:05:50Z extra",
                     "2026-09-08T12:05:50+00:60", "2026-09-08T12:05:50+24:00",
                     "2026-09-08T12:05:50-00:60", "2026-09-08T12:05:50-24:00",
                     "2026-02-30T12:05:50Z", "2026-09-08T25:05:50Z"):
            with self.subTest(wire=wire), self.assertRaises((RuntimeError, ValueError)):
                rollout.docker_started_at(wire)

    def carrier(self):
        service, container, image = fixture()
        metadata = {"RUNTIME_VERSION":"1.2.74", "RUNTIME_COMMIT":"a"*40,
                    "RUNTIME_TREE":"b"*40, "RUNTIME_IMAGE_DIGEST":image["Id"]}
        container["Id"] = "c"*64
        container["State"].update(Running=True, StartedAt=datetime.datetime.now(datetime.timezone.utc).isoformat())
        container["Config"]["Cmd"] = ["sleep", "90"]
        container["Config"]["Env"] += [k+"="+v for k,v in metadata.items()]
        container["HostConfig"].update(PortBindings={}, AutoRemove=True)
        return service, container, image, metadata

    def test_exact_configuration_lifetime_and_no_serving_alias(self):
        service, container, image, metadata = self.carrier()
        rollout.verify_owner_carrier(service,container,image,"unirouter","development",metadata)
        for change in (
            lambda c:c["Config"].update(Cmd=["./uniroute"]),
            lambda c:c["HostConfig"].update(AutoRemove=False),
            lambda c:c["HostConfig"].update(PortBindings={"8080/tcp":[{"HostIp":"127.0.0.1","HostPort":"8080"}]}),
            lambda c:c["State"].update(StartedAt=(datetime.datetime.now(datetime.timezone.utc)-datetime.timedelta(seconds=46)).isoformat()),
            lambda c:c["State"].update(StartedAt=(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(seconds=2)).isoformat()),
            lambda c:c["NetworkSettings"]["Networks"]["unirouter_default"].update(Aliases=["backend"]),
            lambda c:c["Config"]["Env"].append("UNREVIEWED=must-not-be-printed"),
        ):
            bad=copy.deepcopy(container);change(bad)
            with self.assertRaises(RuntimeError) as error:
                rollout.verify_owner_carrier(service,bad,image,"unirouter","development",metadata)
            self.assertNotIn("must-not-be-printed",str(error.exception))

    def test_cleanup_is_exact_bounded_and_confirms_exit(self):
        _,c,_,_=self.carrier()
        with patch.object(rollout,"inspect",side_effect=[c,None]),patch.object(rollout.subprocess,"run",return_value=SimpleNamespace(returncode=0)) as execute:
            rollout.stop_owner_carrier(c["Id"])
        self.assertEqual(["docker","stop","--time","5",c["Id"]],execute.call_args.args[0])
        self.assertEqual(10,execute.call_args.kwargs["timeout"])
        with patch.object(rollout,"inspect",side_effect=[c,c]),patch.object(rollout.subprocess,"run",return_value=SimpleNamespace(returncode=0)),self.assertRaisesRegex(RuntimeError,"exit remains unconfirmed"):
            rollout.stop_owner_carrier(c["Id"])

    def test_carrier_refuses_without_original_operation_locks(self):
        owner=object.__new__(rollout.Rollout)
        for cmd in ("inspect","read","prepare","complete"):
            with self.assertRaisesRegex(RuntimeError,"requires operation locks"):
                owner.artifact_owner_command("ignored",cmd)
        with self.assertRaisesRegex(RuntimeError,"unsupported"):
            owner.artifact_owner_command("ignored","migrate")

    def owner(self):
        service,c,image,metadata=self.carrier()
        o=object.__new__(rollout.Rollout)
        o.spec={"operation_id":"fixture","images":{"backend":{"tag":"fixture:exact","digest":image["Id"]}}}
        o.target={"overlay":"/fixture/runtime.yml","project":"unirouter","environment":"development"}
        o.source={"version":metadata["RUNTIME_VERSION"],"commit":metadata["RUNTIME_COMMIT"],"tree":metadata["RUNTIME_TREE"]}
        o.new_source=Path("/fixture/source")
        o.identity=lambda:None;o.sources=lambda:None;o.images=lambda:{"backend":image}
        o.evidence=SimpleNamespace(has=lambda name:False)
        o.chain=lambda path:["/one","/two","/three","/four","/fixture/runtime.yml"]
        o.compose=lambda chain:["docker","compose"]
        o.config=lambda chain,tags:{"services":{"backend":service}}
        o.compose_env=lambda tags:rollout.ENV
        label=rollout.digest(rollout.canonical(o.spec))
        c["Name"]="/unirouter-owner-"+label[:24]
        c["Config"]["Labels"]={"io.unirouter.control-owner":label}
        return o,c

    def test_exact_command_payload_and_cleanup_for_success_failure_and_timeout(self):
        for outcome in ("success","failed","lost"):
            o,c=self.owner()
            result=SimpleNamespace(returncode=0 if outcome=="success" else 1,stdout=b'{"head":"fixture"}',stderr=b"secret-dsn-must-not-leak")
            effect=subprocess.TimeoutExpired(["docker","exec",c["Id"]],30) if outcome=="lost" else None
            with patch.object(rollout,"HELD_LOCK_FDS",[123]),patch.object(rollout,"inspect",side_effect=[None,c]),patch.object(rollout,"run",return_value=c["Id"].encode()) as create,patch.object(rollout.subprocess,"run",return_value=result,side_effect=effect) as execute,patch.object(rollout,"stop_owner_carrier") as stop:
                if outcome=="success": self.assertEqual({"head":"fixture"},o.artifact_owner_command("old","read",{"change_id":"exact"}))
                else:
                    with self.assertRaises((RuntimeError,subprocess.TimeoutExpired)) as e:o.artifact_owner_command("old","read",{"change_id":"exact"})
                    self.assertNotIn("secret-dsn",str(e.exception))
                stop.assert_called_once_with(c["Id"])
                self.assertEqual(["docker","exec","-i",c["Id"],"./uniroute","runtime-generation","read"],execute.call_args.args[0])
                self.assertEqual({"change_id":"exact"},json.loads(execute.call_args.kwargs["input"]))
                self.assertEqual((123,),execute.call_args.kwargs["pass_fds"])
                argv=create.call_args.args[0]
                self.assertEqual(["backend","sleep","90"],argv[-3:])
                self.assertIn("--no-deps",argv);self.assertIn("--rm",argv)
                self.assertEqual("never",argv[argv.index("--pull")+1])
                self.assertNotIn("--service-ports",argv);self.assertNotIn("--use-aliases",argv)
                self.assertFalse(any("DB_DSN" in value for value in argv))
                self.assertEqual(4,argv.count("-e"))

    def test_unknown_creation_and_existing_name_never_send_or_take_over(self):
        for exists in (True,False):
            o,c=self.owner()
            with patch.object(rollout,"HELD_LOCK_FDS",[123]),patch.object(rollout,"inspect",return_value=c if exists else None),patch.object(rollout,"run",side_effect=subprocess.TimeoutExpired(["docker","compose"],30)) as create,patch.object(rollout.subprocess,"run") as execute,patch.object(rollout,"stop_owner_carrier") as stop:
                with self.assertRaisesRegex(RuntimeError,"already exists" if exists else "creation unconfirmed"):o.artifact_owner_command("old","inspect")
                execute.assert_not_called();stop.assert_not_called()
                if exists:create.assert_not_called()

    def test_only_explicit_carrier_read_commands_take_lifecycle_locks(self):
        for action in ("plan","inspect"):
            for carrier in (False,True):
                document={"target":{}}
                if carrier:document["runtime_owner"]="artifact_cli"
                owner=SimpleNamespace(plan=lambda:({},None,None),progress=lambda:({},{}))
                with patch.object(sys,"argv",["rollout",action,"--input","/fixture"]),patch.object(rollout,"document",return_value=document),patch.object(rollout,"Rollout",return_value=owner),patch.object(rollout,"operation_locks",return_value=contextlib.nullcontext()) as lock,patch.dict(os.environ,{},clear=True),patch("builtins.print"):
                    self.assertEqual(0,rollout.main())
                    self.assertEqual(1 if carrier else 0,lock.call_count)


class CounterBridgeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec=importlib.util.spec_from_file_location("counter_expand_test",Path(__file__).with_name("compatible-expand.py"))
        cls.expand=importlib.util.module_from_spec(spec); spec.loader.exec_module(cls.expand)

    def test_missing_counter_support_or_public_port_rejects_expand(self):
        e=self.expand
        manifest={"schema_heads":e.HEADS,"implementations":e.COUNTERS}
        wire=json.dumps(manifest,sort_keys=True,separators=(",",":")).encode()
        backend={"Image":"sha256:"+"a"*64,"HostConfig":{"PortBindings":{"8080/tcp":[{"HostIp":"127.0.0.1"}]}}}
        runtime={"artifact_digest":backend["Image"],"supported_schema_heads":e.HEADS,"compatibility_epoch":"unirouter.runtime-compatibility.v1","support_manifest_digest":rollout.digest(wire)}
        e.verify_bridge(runtime,backend,wire,e.HEADS[2])
        for refs in (e.COUNTERS[:1],e.COUNTERS[1:]):
            bad=json.dumps(dict(manifest,implementations=refs),sort_keys=True,separators=(",",":")).encode()
            with self.assertRaises(RuntimeError): e.verify_bridge(dict(runtime,support_manifest_digest=rollout.digest(bad)),backend,bad,e.HEADS[2])
        backend["HostConfig"]["PortBindings"]["8080/tcp"][0]["HostIp"]="0.0.0.0"
        with self.assertRaises(RuntimeError): e.verify_bridge(runtime,backend,wire,e.HEADS[2])

    def test_m124_requires_receiver_adoption_and_retains_prior_contracts(self):
        e=self.expand
        refs=e.COUNTERS+[e.SOURCE_FACTS,e.SOURCE_OBSERVATIONS,e.SOURCE_DISPATCH]+e.BUDGET_REQUIREMENTS+[e.CONTENT_LIFECYCLE,e.PRE_DISPATCH_OBSERVATIONS]+e.SCOPED_CONTENT+e.XINSIGHT_POLICY+e.XINSIGHT_ADOPTION
        manifest={"schema_heads":e.HEADS,"implementations":refs}
        wire=json.dumps(manifest,sort_keys=True,separators=(",",":")).encode()
        backend={"Image":"sha256:"+"a"*64,"HostConfig":{"PortBindings":{}}}
        runtime={"artifact_digest":backend["Image"],"supported_schema_heads":e.HEADS,"compatibility_epoch":"unirouter.runtime-compatibility.v1","support_manifest_digest":rollout.digest(wire)}
        e.verify_bridge(runtime,backend,wire,e.HEADS[-1])
        for missing in e.XINSIGHT_ADOPTION+e.XINSIGHT_POLICY+e.BUDGET_REQUIREMENTS:
            bad=json.dumps(dict(manifest,implementations=[r for r in refs if r!=missing]),sort_keys=True,separators=(",",":")).encode()
            with self.assertRaises(RuntimeError):e.verify_bridge(dict(runtime,support_manifest_digest=rollout.digest(bad)),backend,bad,e.HEADS[-1])

    def test_actual_process_inventory_includes_absent_or_expired_actors(self):
        e=self.expand
        backends={"stable":{"Id":"a"*64,"State":{"Pid":10}},"canary":{"Id":"b"*64,"State":{"Pid":20}}}
        def membership(path,*args,**kwargs):
            return "0::/system.slice/docker-"+{"10":"a"*64,"20":"b"*64,"30":"c"*64}[path.parent.name]+".scope"
        with patch.object(e.ingress,"run",return_value=b"10 uniroute\n20 uniroute\n90 postgres\n"),patch.object(Path,"read_text",membership):
            e.verify_recovery_process_inventory(backends)
        for process in (b"10 uniroute\n20 uniroute\n30 uniroute\n",b"10 uniroute\n"):
            with patch.object(e.ingress,"run",return_value=process),patch.object(Path,"read_text",membership),self.assertRaises(RuntimeError):
                e.verify_recovery_process_inventory(backends)
        with patch.object(e.ingress,"run",return_value=b"10 uniroute\n20 uniroute\n"),patch.object(Path,"read_text",side_effect=FileNotFoundError),self.assertRaisesRegex(RuntimeError,"changed"):
            e.verify_recovery_process_inventory(backends)


class RuntimeConfigurationTests(unittest.TestCase):
    def test_image_defaults_and_compose_null_are_checked_against_actual_runtime(self):
        rollout.check_service(*fixture(), "unirouter", "development")

    def test_environment_drift_is_rejected_without_disclosing_values(self):
        service, container, image = fixture()
        service["environment"]["DB_DSN"] = "must-not-appear-in-diagnostic"
        with self.assertRaisesRegex(RuntimeError, "environment differs") as error:
            rollout.check_service(service, container, image, "unirouter", "development")
        self.assertNotIn("must-not-appear", str(error.exception))

    def test_document_development_does_not_allow_actual_production(self):
        service, container, image = fixture()
        service["environment"]["RUNTIME_ENVIRONMENT"] = "production"
        container["Config"]["Env"][1] = "RUNTIME_ENVIRONMENT=production"
        with self.assertRaisesRegex(RuntimeError, "outside.*environment"):
            rollout.check_service(service, container, image, "unirouter", "development")

    def test_ports_mounts_and_runtime_settings_are_not_label_only_checks(self):
        for changed in ("ports", "mount", "privilege", "health"):
            with self.subTest(changed=changed):
                service, container, image = fixture()
                if changed == "ports":
                    container["HostConfig"]["PortBindings"]["8080/tcp"][0]["HostIp"] = "0.0.0.0"
                elif changed == "mount":
                    container["Mounts"][0]["RW"] = True
                elif changed == "privilege":
                    container["HostConfig"]["Privileged"] = True
                else:
                    container["Config"]["Healthcheck"] = {"Test": ["NONE"]}
                with self.assertRaises(RuntimeError):
                    rollout.check_service(service, container, image, "unirouter", "development")

    def test_host_interpolation_does_not_reach_compose(self):
        owner = object.__new__(rollout.Rollout)
        with patch.dict(os.environ, {"JWT_SECRET": "host-override", "RUNTIME_ENVIRONMENT": "production", "COMPOSE_FILE": "wrong", "DOCKER_HOST": "tcp://wrong"}):
            env = owner.compose_env({"backend": "local:backend", "frontend": "local:frontend"})
            self.assertNotIn("JWT_SECRET", env)
            self.assertNotIn("COMPOSE_FILE", env)
            self.assertNotIn("RUNTIME_ENVIRONMENT", env)
            self.assertEqual(env["DOCKER_HOST"], "unix:///var/run/docker.sock")

    def test_missing_image_refuses_before_any_stop_or_preparation(self):
        owner = object.__new__(rollout.Rollout)
        with patch.object(owner, "identity"), patch.object(owner, "images", side_effect=RuntimeError("missing local image")), \
                patch.object(rollout, "run") as command, patch.object(rollout, "inspect") as inspect, \
                self.assertRaisesRegex(RuntimeError, "missing local image"):
            owner.plan()
        command.assert_not_called()
        inspect.assert_not_called()


class SourceAuthorityTests(unittest.TestCase):
    def test_source_identity_and_dirty_state_are_checked_read_only(self):
        with tempfile.TemporaryDirectory(prefix="unirouter-source-test-") as directory:
            root = Path(directory).resolve()
            (root / "VERSION").write_text("1.2.3\n")
            source = {"directory": str(root), "commit": "a" * 40, "tree": "b" * 40, "version": "1.2.3"}
            for outputs, message in (([b"wrong"], "commit changed"),
                                     ([b"a" * 40, b"wrong"], "tree changed"),
                                     ([b"a" * 40, b"b" * 40, b" M source"], "dirty"),
                                     ([b"a" * 40, b"b" * 40, b""], None)):
                with patch.object(rollout, "run", side_effect=outputs) as command:
                    if message:
                        with self.assertRaisesRegex(RuntimeError, message):
                            rollout.check_source(source)
                    else:
                        rollout.check_source(source)
                    for call in command.call_args_list:
                        self.assertEqual(call.args[0][:3], ["git", "-C", str(root)])
                        self.assertIn(call.args[0][3], ("rev-parse", "status"))
            self.assertEqual(list(root.iterdir()), [root / "VERSION"])

    def test_copied_tool_cannot_control_another_exact_source(self):
        owner = object.__new__(rollout.Rollout)
        owner.source = {}
        owner.new_source = Path("/another-source")
        with patch.object(rollout, "check_source"), self.assertRaisesRegex(RuntimeError, "tool is outside"):
            owner.sources()

    def test_inspection_refuses_changed_source_before_reading_or_mutating_components(self):
        owner = object.__new__(rollout.Rollout)
        with patch.object(owner, "identity"), patch.object(owner, "retained_plan", return_value={}), \
                patch.object(owner, "sources", side_effect=RuntimeError("source commit changed")), \
                patch.object(rollout, "inspect") as inspect, patch.object(owner, "images") as images, \
                self.assertRaisesRegex(RuntimeError, "source commit changed"):
            owner.progress()
        images.assert_not_called()
        inspect.assert_not_called()


class RecoveryBoundaryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="unirouter-rollout-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.evidence = rollout.Evidence(self.root)
        self.service, self.container, self.image = fixture()
        self.owner = object.__new__(rollout.Rollout)
        self.owner.spec = {"images": {"backend": {"tag": "local:next", "digest": self.image["Id"]},
                                      "frontend": {"tag": "local:next-ui", "digest": self.image["Id"]}}}
        self.owner.target = {"project": "unirouter", "environment": "development"}
        self.owner.evidence = self.evidence
        self.owner.new_source = Path("/new-source")
        self.plan = {"plan_digest": "exact-plan", "containers": {"canary": {"id": "old"}}, "old_instances": {"canary": "old-instance"}}

    def test_failed_withdrawal_cannot_stop_backend(self):
        with patch.object(self.owner, "prove_withdrawal", side_effect=RuntimeError("unproven")), \
                patch.object(rollout, "run") as command, self.assertRaisesRegex(RuntimeError, "unproven"):
            self.owner.stop("canary", self.plan)
        command.assert_not_called()

    def test_lost_stop_result_checks_existing_exit_and_drain_without_second_stop(self):
        self.evidence.write("start-stop-canary.json", {"at": "2026-09-07T00:00:00Z"})
        stopped = SimpleNamespace(returncode=0, stdout=b"Server exited\n", stderr=b"")
        with patch.object(self.owner, "prove_withdrawal"), patch.object(self.owner, "ready"), \
                patch.object(rollout, "inspect", return_value=self.container), \
                patch.object(rollout.subprocess, "run", return_value=stopped) as command:
            result = self.owner.stop("canary", self.plan)
        self.assertEqual(result["container_id"], "old")
        self.assertEqual(command.call_count, 1)
        self.assertEqual(command.call_args.args[0][:2], ["docker", "logs"])

    def test_stopped_container_with_incomplete_drain_is_not_a_success(self):
        self.evidence.write("start-stop-canary.json", {"at": "2026-09-07T00:00:00Z"})
        stopped = SimpleNamespace(returncode=0, stdout=b"HTTP runtime drain incomplete: deadline\nServer exited\n", stderr=b"")
        with patch.object(self.owner, "prove_withdrawal"), patch.object(self.owner, "ready"), \
                patch.object(rollout, "inspect", return_value=self.container), \
                patch.object(rollout.subprocess, "run", return_value=stopped), self.assertRaisesRegex(RuntimeError, "drain/stop is unproven"):
            self.owner.stop("canary", self.plan)

    def test_unconfirmed_docker_stop_is_not_resent_while_daemon_may_still_run(self):
        self.container["State"]["Running"] = True
        self.evidence.write("command-stop-canary.json", {"plan_digest": "exact-plan"})
        with patch.object(self.owner, "prove_withdrawal"), patch.object(self.owner, "ready"), \
                patch.object(rollout, "inspect", return_value=self.container), patch.object(rollout, "run") as command, \
                self.assertRaisesRegex(RuntimeError, "remains unconfirmed"):
            self.owner.stop("canary", self.plan)
        command.assert_not_called()

    def test_unconfirmed_compose_create_is_not_resent_from_old_or_absent_state(self):
        self.evidence.write("command-create-canary.json", {"plan_digest": "exact-plan"})
        for observed in (self.container, None):
            with self.subTest(observed=observed is not None), patch.object(self.owner, "prove_withdrawal"), \
                    patch.object(rollout, "inspect", return_value=observed), patch.object(self.owner, "require_interrupted_create"), \
                    patch.object(self.owner, "images", return_value={"backend": self.image}), \
                    patch.object(self.owner, "config_for", return_value={"services": {"backend-canary": self.service}}), \
                    patch.object(rollout, "run") as command, self.assertRaisesRegex(RuntimeError, "remains unconfirmed"):
                self.owner.create("canary", self.plan)
            command.assert_not_called()

    def test_lost_create_result_reads_desired_container_without_recreation(self):
        self.container["Id"] = "new"
        with patch.object(self.owner, "prove_withdrawal"), patch.object(rollout, "inspect", return_value=self.container), \
                patch.object(self.owner, "images", return_value={"backend": self.image}), \
                patch.object(self.owner, "config_for", return_value={"services": {"backend-canary": self.service}}), \
                patch.object(self.owner, "ready", return_value={"instance": "new-instance"}), patch.object(rollout, "run") as command:
            self.assertEqual(self.owner.create("canary", self.plan)["container_id"], "new")
        command.assert_not_called()

    def test_create_rechecks_images_and_explicitly_disables_pull(self):
        newer = copy.deepcopy(self.container)
        newer["Id"] = "new"
        with patch.object(self.owner, "prove_withdrawal"), patch.object(rollout, "inspect", side_effect=[self.container, newer]), \
                patch.object(self.owner, "images", return_value={"backend": self.image}) as image_check, \
                patch.object(self.owner, "config_for", return_value={"services": {"backend-canary": self.service}}), \
                patch.object(self.owner, "ready", return_value={"instance": "new-instance"}), \
                patch.object(self.owner, "chain", return_value=[]), patch.object(self.owner, "compose", return_value=["docker", "compose"]), \
                patch.object(rollout, "run") as command:
            self.owner.create("canary", self.plan)
        image_check.assert_called_once()
        args = command.call_args.args[0]
        self.assertIn("--no-build", args)
        self.assertEqual(args[args.index("--pull") + 1], "never")
        self.assertEqual(args[-1], "backend-canary")

    def test_old_backend_still_running_cannot_be_recreated(self):
        self.container["State"]["Running"] = True
        with patch.object(self.owner, "prove_withdrawal"), patch.object(rollout, "inspect", return_value=self.container), \
                patch.object(self.owner, "images", return_value={"backend": self.image}), \
                patch.object(self.owner, "config_for", return_value={"services": {"backend-canary": self.service}}), \
                patch.object(rollout, "run") as command, self.assertRaisesRegex(RuntimeError, "has not stopped"):
            self.owner.create("canary", self.plan)
        command.assert_not_called()

    def test_exact_evidence_is_exclusive_and_symlink_rejected(self):
        self.evidence.write("one.json", {"value": 1})
        with self.assertRaises(FileExistsError):
            self.evidence.write("one.json", {"value": 2})
        (self.root / "alias.json").symlink_to(self.root / "one.json")
        with self.assertRaises(RuntimeError):
            self.evidence.read("alias.json")
        self.assertEqual(self.evidence.read("one.json"), {"value": 1})

    def test_interrupted_create_requires_prior_stop_and_no_running_old_container(self):
        self.evidence.write("start-create-canary.json", {})
        self.evidence.write("command-create-canary.json", {})
        with patch.object(rollout, "inspect", return_value=None), self.assertRaisesRegex(RuntimeError, "stop was not verified"):
            self.owner.require_interrupted_create("canary", self.plan)
        self.evidence.write("done-stop-canary.json", {})
        with patch.object(rollout, "inspect", return_value=None):
            self.owner.require_interrupted_create("canary", self.plan)
        self.container["State"]["Running"] = True
        with patch.object(rollout, "inspect", return_value=self.container), self.assertRaisesRegex(RuntimeError, "another name"):
            self.owner.require_interrupted_create("canary", self.plan)

    def test_missing_container_is_not_inferred_from_docker_failure(self):
        for failed in (SimpleNamespace(returncode=1, stdout=b"[]", stderr=b"Cannot connect to Docker daemon"),
                       SimpleNamespace(returncode=1, stdout=b"[]", stderr=b"permission denied"),
                       SimpleNamespace(returncode=1, stdout=b"", stderr=b"Error: No such object: exact"),
                       SimpleNamespace(returncode=1, stdout=b"", stderr=b"error: no such object: exact"),
                       SimpleNamespace(returncode=2, stdout=b"[]", stderr=b"error: no such object: exact"),
                       SimpleNamespace(returncode=1, stdout=b"[]", stderr=b"error: no such object: another"),
                       SimpleNamespace(returncode=1, stdout=b"[]", stderr=b"error: no such object: EXACT"),
                       SimpleNamespace(returncode=1, stdout=b"[]", stderr=b"error: no such object: exact\npermission denied")):
            with patch.object(rollout.subprocess, "run", return_value=failed), self.assertRaisesRegex(RuntimeError, "absence is unproven"):
                rollout.inspect("exact", missing_ok=True)
        for message in (b"Error: No such object: exact", b"Error response from daemon: No such container: exact",
                        b"error: no such object: exact\n"):
            with patch.object(rollout.subprocess, "run", return_value=SimpleNamespace(returncode=1, stdout=b"[]\n", stderr=message)):
                self.assertIsNone(rollout.inspect("exact", missing_ok=True))
                with self.assertRaisesRegex(RuntimeError, "absence is unproven"):
                    rollout.inspect("exact")

    def test_child_mutation_inherits_real_locks_if_parent_exits(self):
        with patch.object(rollout, "LIFECYCLE_LOCK", self.root / "lifecycle.lock"):
            with rollout.operation_locks({"ingress_directory": str(self.root)}):
                with patch.object(rollout.subprocess, "run", return_value=SimpleNamespace(returncode=0, stdout=b"ok")) as child:
                    rollout.run(["docker", "stop", "exact-container"])
                self.assertEqual(len(child.call_args.kwargs["pass_fds"]), 2)
                for fd in child.call_args.kwargs["pass_fds"]:
                    self.assertGreater(os.fstat(fd).st_ino, 0)
        self.assertEqual(rollout.HELD_LOCK_FDS, [])

    def test_formal_ingress_cannot_rejoin_between_withdrawal_and_stop(self):
        target = {"ingress_directory": str(self.root)}
        with patch.object(rollout, "LIFECYCLE_LOCK", self.root / "lifecycle.lock"):
            with rollout.operation_locks(target):
                args = [sys.executable, str(Path(__file__).with_name("runtime-ingress.py")), "--directory", str(self.root),
                        "--expected-container-id", "a" * 64, "--desired", "both", "--apply", "--expected-config-sha256", "b" * 64,
                        "--expected-nginx-sha256", "c" * 64]
                result = subprocess.run(args, capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("Resource temporarily unavailable", result.stderr)
            # Process/operation exit releases both locks; recovery can reacquire.
            with rollout.operation_locks(target):
                pass

    def test_real_child_keeps_lock_after_parent_closes_descriptors(self):
        target = {"ingress_directory": str(self.root)}
        with patch.object(rollout, "LIFECYCLE_LOCK", self.root / "lifecycle.lock"):
            with rollout.operation_locks(target):
                child = subprocess.Popen([sys.executable, "-c", "import sys; print('ready', flush=True); sys.stdin.readline()"],
                                         stdin=subprocess.PIPE, stdout=subprocess.PIPE, pass_fds=tuple(rollout.HELD_LOCK_FDS))
                self.assertEqual(child.stdout.readline(), b"ready\n")
            try:
                with self.assertRaises(BlockingIOError):
                    with rollout.operation_locks(target):
                        pass
            finally:
                child.communicate(b"finish\n", timeout=10)
            with rollout.operation_locks(target):
                pass

    def test_prepared_plan_and_overlay_bytes_cannot_be_substituted(self):
        self.owner.spec = {"test": "input"}
        self.evidence.write("input.json", self.owner.spec)
        old, new = b"old runtime", b"new runtime"
        plan = {"input_digest": rollout.digest(rollout.canonical(self.owner.spec)),
                "old_overlay_digest": rollout.digest(old), "new_overlay_digest": rollout.digest(new)}
        plan["plan_digest"] = rollout.digest(rollout.canonical(plan))
        self.evidence.write("plan.json", plan)
        self.evidence.write("prepared.json", {"plan_digest": plan["plan_digest"]})
        self.evidence.write("old.runtime.yml", old)
        self.evidence.write("new.runtime.yml", new)
        self.assertEqual(self.owner.retained_plan(), plan)
        (self.root / "new.runtime.yml").write_bytes(b"replacement")
        with self.assertRaisesRegex(RuntimeError, "bytes changed"):
            self.owner.retained_plan()
        (self.root / "new.runtime.yml").write_bytes(new)
        plan["input_digest"] = "wrong"
        (self.root / "plan.json").write_bytes(rollout.canonical(plan))
        with self.assertRaisesRegex(RuntimeError, "digest differs"):
            self.owner.retained_plan()

    def test_resume_rejects_configuration_drift_before_create(self):
        with patch.object(self.owner, "prove_withdrawal"), patch.object(rollout, "inspect", return_value=self.container), \
                patch.object(self.owner, "images", return_value={"backend": self.image}), \
                patch.object(self.owner, "config_for", side_effect=RuntimeError("configuration drifted")), \
                patch.object(rollout, "run") as command, self.assertRaisesRegex(RuntimeError, "drifted"):
            self.owner.create("canary", self.plan)
        command.assert_not_called()


class CompletedStateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="unirouter-complete-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.owner = rollout.Rollout({"target": {"project": "unirouter", "environment": "development", "overlay": str(self.root / "runtime.yml"),
                                                  "ingress_directory": str(self.root)}, "source": {"directory": "/new"},
                                      "evidence_directory": str(self.root), "operation_id": "operation", "images": {}})
        self.owner.target["tls_overlay"] = "/tls.yml"
        self.evidence = self.owner.evidence
        self.service, _, self.image = fixture()
        self.owner.spec["images"] = {k: {"tag": "local:new", "digest": self.image["Id"]} for k in ("backend", "frontend")}
        self.plan = {"plan_digest": "exact", "pins": {"schema": "same"}, "old_overlay_digest": rollout.digest(b"old"),
                     "new_overlay_digest": rollout.digest(b"new"), "nginx_static_digest": "static", "initial_ingress_head": "initial",
                     "containers": {r: {"id": "old-" + r, "image": self.image["Id"]} for r in rollout.SERVICES},
                     "old_instances": {r: "old-" + r for r in ("stable", "canary")}}
        self.config = {"services": {v: copy.deepcopy(self.service) for v in rollout.SERVICES.values()}}
        self.containers = {}
        for role, name in rollout.CONTAINERS.items():
            _, c, _ = fixture()
            c["Id"], c["State"]["Running"] = "new-" + role, True
            c["Config"]["Labels"] = {"com.docker.compose.project.config_files": ",".join(self.owner.chain(self.owner.new_source))}
            self.containers[name] = c
        self.evidence.write("apply-started.json", {"plan_digest": "exact"})
        for stage in rollout.STAGES:
            role = stage.removeprefix("create-")
            result = {"container_id": "new-" + role, "instance": "new-" + role, "image": self.image["Id"]} if stage.startswith("create-") else {}
            self.evidence.write("start-" + stage + ".json", {"plan_digest": "exact"})
            self.evidence.write("done-" + stage + ".json", {"plan_digest": "exact", "result": result})
        (self.root / "runtime.yml").write_bytes(b"new")
        self.wire = self.owner.ingress.membership("both")
        (self.root / "upstreams.conf").write_bytes(self.wire)
        self.members = sorted([{"instance": "new-" + r, "artifact_digest": self.image["Id"]} for r in ("stable", "canary")], key=lambda m: m["instance"])
        self.state = {"head": "operation-refresh-frontend", "head_status": "applied",
                      "generations": [dict(m, ingress_state="receiving") for m in self.members]}
        self.receipt = {"status": "applied", "plan": {"change_id": self.state["head"], "predecessor_id": "operation-rejoin-stable",
                        "desired_config_digest": rollout.digest(self.wire), "desired_config_bytes": rollout.base64.b64encode(self.wire).decode(),
                        "expected_config_digest": rollout.digest(self.wire), "membership_after": self.members, "membership_before": self.members}}
        patches = [patch.object(self.owner, "identity"), patch.object(self.owner, "retained_plan", return_value=self.plan),
                   patch.object(self.owner, "sources"), patch.object(self.owner, "images"),
                   patch.object(self.owner, "pins", return_value=self.plan["pins"]),
                   patch.object(self.owner, "ingress_static_digest", return_value="static"), patch.object(self.owner, "owner", return_value="exact"),
                   patch.object(self.owner, "config_for", return_value=self.config),
                   patch.object(self.owner, "ready", side_effect=lambda role, image: {"instance": "new-" + role}),
                   patch.object(self.owner.ingress, "owner_command", side_effect=lambda owner, action, *args: self.state if action == "inspect" else self.receipt),
                   patch.object(self.owner.ingress, "docker_exec", side_effect=lambda *args: (self.root / "upstreams.conf").read_bytes()),
                   patch.object(rollout, "inspect", side_effect=lambda name, **kw: self.containers[name]),
                   patch.object(rollout, "run", return_value=rollout.canonical([self.image]))]
        for p in patches:
            p.start()
            self.addCleanup(p.stop)

    def test_complete_inspection_checks_current_authority_without_writes(self):
        before = {p.name: p.read_bytes() for p in self.root.iterdir()}
        self.assertEqual(self.owner.progress()[1]["deployment"], "complete")
        self.assertEqual({p.name: p.read_bytes() for p in self.root.iterdir()}, before)

    def test_old_overlay_cannot_hide_behind_completed_stages(self):
        (self.root / "runtime.yml").write_bytes(b"old")
        with self.assertRaisesRegex(RuntimeError, "overlay differs"):
            self.owner.progress()

    def test_historical_or_pending_ingress_cannot_hide_behind_completed_stages(self):
        for head, status in (("initial", "applied"), ("operation-rejoin-stable", "applied"), ("operation-refresh-frontend", "pending")):
            self.state.update(head=head, head_status=status)
            with self.subTest(head=head, status=status), self.assertRaisesRegex(RuntimeError, "ingress (head|status) differs"):
                self.owner.progress()

    def test_applied_receipt_does_not_replace_actual_carrier_or_database_membership(self):
        (self.root / "upstreams.conf").write_bytes(self.owner.ingress.membership("stable"))
        with self.assertRaisesRegex(RuntimeError, "installed membership differs"):
            self.owner.progress()
        (self.root / "upstreams.conf").write_bytes(self.wire)
        self.state["generations"] = self.state["generations"][:1]
        with self.assertRaisesRegex(RuntimeError, "database receivers differ"):
            self.owner.progress()

    def test_changed_receipt_and_restarted_process_refuse_complete(self):
        self.receipt["plan"]["predecessor_id"] = "unrelated"
        with self.assertRaisesRegex(RuntimeError, "predecessor differs"):
            self.owner.progress()
        self.receipt["plan"]["predecessor_id"] = "operation-rejoin-stable"
        with patch.object(self.owner, "ready", return_value={"instance": "unplanned-restart"}), self.assertRaisesRegex(RuntimeError, "process changed"):
            self.owner.progress()

    def test_only_current_pending_transition_allows_its_two_carrier_states(self):
        before_members = sorted([{"instance": "old-stable", "artifact_digest": self.image["Id"]},
                                 {"instance": "new-canary", "artifact_digest": self.image["Id"]}], key=lambda m: m["instance"])
        after_members = [m for m in before_members if m["instance"] == "new-canary"]
        after = self.owner.ingress.membership("canary")
        self.state.update(head="operation-withdraw-stable", head_status="pending", generations=[dict(m, ingress_state="receiving") for m in before_members])
        self.receipt = {"status": "pending", "plan": {"change_id": self.state["head"], "predecessor_id": "operation-rejoin-canary",
                        "desired_config_digest": rollout.digest(after), "desired_config_bytes": rollout.base64.b64encode(after).decode(),
                        "expected_config_digest": rollout.digest(self.wire), "membership_after": after_members, "membership_before": before_members}}
        completed = list(rollout.STAGES[:rollout.STAGES.index("withdraw-stable")])
        for wire in (self.wire, after):
            (self.root / "upstreams.conf").write_bytes(wire)
            self.owner.check_ingress_stage(self.plan, completed)
        self.state.update(head_status="applied", generations=[dict(m, ingress_state="receiving") for m in after_members])
        self.receipt["status"] = "applied"
        self.owner.check_ingress_stage(self.plan, completed)
        (self.root / "upstreams.conf").write_bytes(self.wire)
        with self.assertRaisesRegex(RuntimeError, "installed membership differs"):
            self.owner.check_ingress_stage(self.plan, completed)


class ObserverEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="unirouter-observer-test-")
        self.addCleanup(self.temp.cleanup)
        self.evidence = rollout.Evidence(Path(self.temp.name).resolve())

    def window(self, number, failed=False, end=True):
        name = f"observer-{number}"
        self.evidence.write(name + "-start.json", {"at": "start"})
        rows = [{"at": "start", "completed_at": "end", "protocol": protocol, "exit": 16 if failed else 0,
                 "result": "000 2" if failed else "401 2" if protocol == "--http2" else "401 1.1", "ok": not failed}
                for protocol in ("--http1.1", "--http2")]
        self.evidence.write(name + "-samples.jsonl", b"".join(rollout.canonical(r) + b"\n" for r in rows))
        if end:
            self.evidence.write(name + "-end.json", {"at": "end", "samples": 2, "failed": failed, "error": None})

    def test_clean_observation_only_proves_short_requests(self):
        self.window(1)
        self.assertEqual(rollout.observation_acceptance(self.evidence), "passed_short_requests_only")

    def test_crash_without_end_cannot_be_repaired_by_green_resume(self):
        self.window(1, end=False)
        self.window(2)
        self.assertEqual(rollout.observation_acceptance(self.evidence), "unproven_gap")

    def test_prior_failed_sample_survives_green_resume(self):
        self.window(1, failed=True)
        self.window(2)
        self.assertEqual(rollout.observation_acceptance(self.evidence), "failed")

    def test_crash_before_any_sample_is_an_observation_gap(self):
        self.evidence.write("observer-1-start.json", {"at": "start"})
        self.assertEqual(rollout.observation_acceptance(self.evidence), "unproven_gap")

    def test_truncated_sample_or_lost_end_remains_unproven(self):
        self.window(1)
        path = self.evidence.root / "observer-1-samples.jsonl"
        path.write_bytes(path.read_bytes()[:-2])
        self.assertEqual(rollout.observation_acceptance(self.evidence), "unproven_gap")

    def test_successful_samples_do_not_erase_failed_deployment_attempt(self):
        self.window(1)
        self.evidence.write("observer-1-operation-failed.json", {"error_class": "RuntimeError"})
        self.window(2)
        self.assertEqual(rollout.observation_acceptance(self.evidence), "failed")


class PinnedObserverTests(unittest.TestCase):
    CLIENT = {"container_id": "d" * 64, "image_digest": "sha256:" + "e" * 64,
              "curl_version": "curl 8.19.0 (fixture) libcurl/8.19.0\nProtocols: http https\nFeatures: HTTP2 SSL",
              "vantage": "pinned_ingress_container_public_https"}

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="unirouter-pinned-observer-")
        self.addCleanup(self.temp.cleanup)
        self.evidence = rollout.Evidence(Path(self.temp.name).resolve())
        self.observer = rollout.Observer(self.evidence, "https://8.130.18.85", self.CLIENT["container_id"])
        self.observer.client = dict(self.CLIENT)

    def sample(self, body=b'{"error":{"type":"authentication_error"}}', metadata=b"401 2 1", exit=0, protocol="--http2"):
        response = SimpleNamespace(returncode=exit, stdout=body + b"\n" + metadata, stderr=b"sensitive error not retained")
        with patch.object(rollout.subprocess, "run", return_value=response) as command:
            row = self.observer.sample(protocol)
        return row, command

    def write_window(self, row=None):
        self.evidence.write("observer-1-start.json", {"at": "start", "contract_version": rollout.OBSERVER_CONTRACT,
                                                     "origin": self.observer.origin, "client": self.CLIENT})
        h2 = row or self.sample()[0]
        h1 = self.sample(metadata=b"401 1.1 1", protocol="--http1.1")[0]
        self.evidence.write("observer-1-samples.jsonl", b"".join(rollout.canonical(r) + b"\n" for r in [h1, h2]))
        self.evidence.write("observer-1-end.json", {"at": "end", "samples": 2, "failed": not h2["ok"], "error": None})

    def test_existing_exact_container_and_config_free_credential_free_command(self):
        row, command = self.sample(metadata=b"401 2 2")
        argv = command.call_args.args[0]
        self.assertEqual(argv[:5], ["docker", "exec", self.CLIENT["container_id"], "curl", "-q"])
        self.assertIn("--http2-prior-knowledge", argv)
        self.assertEqual(argv[argv.index("--retry") + 1], "0")
        self.assertEqual(argv[argv.index("--noproxy") + 1], "*")
        self.assertEqual(argv[argv.index("--max-filesize") + 1], "65536")
        self.assertEqual(argv[-1], "https://8.130.18.85/v1/chat/completions")
        self.assertEqual(argv[argv.index("-H") + 1], "Content-Type: application/json")
        self.assertFalse(any(v in argv for v in ("-k", "-L", "--netrc", "--retry-all-errors", "-K", "--config")))
        self.assertNotIn("shell", command.call_args.kwargs)
        self.assertEqual(row["num_connects"], 2)
        self.assertTrue(row["ok"])
        self.assertNotIn("sensitive", str(row))
        self.assertEqual(row["client_digest"], rollout.digest(rollout.canonical(self.CLIENT)))

    def test_failures_are_not_reinterpreted_as_success(self):
        for kwargs in ({"metadata": b"401 2 1", "exit": 92}, {"metadata": b"000 0 1", "exit": 16},
                       {"metadata": b"401 1.1 1"}, {"metadata": b"503 2 1"},
                       {"body": b'{"error":{"message":"truncated"}'}, {"body": b'{"error":{}}'},
                       {"body": b'{"choices":[]}'}, {"body": b'{"error":{},"error":{"type":"x"}}'},
                       {"metadata": b"401 2 0"}, {"metadata": b"401 2 x"}, {"metadata": b"401 2"},
                       {"body": b'{"error":{"message":"' + b"x" * 65536 + b'"}}'}):
            with self.subTest(kwargs={k: str(v)[:70] for k, v in kwargs.items()}):
                self.assertFalse(self.sample(**kwargs)[0]["ok"])

    def test_client_preflight_refuses_old_or_unsupported_versions(self):
        for version in ("curl 7.81.0 (fixture)\nProtocols: https\nFeatures: HTTP2", "not curl",
                        "curl 8.19.0 (fixture)\nProtocols: https\nFeatures: SSL",
                        "curl 8.19.0 (fixture)\nProtocols: http\nFeatures: HTTP2"):
            with self.subTest(version=version), self.assertRaises(RuntimeError):
                rollout.qualified_observer_client(dict(self.CLIENT, curl_version=version))

    def test_unavailable_container_refuses_before_thread_or_receipt(self):
        with patch.object(rollout, "inspect", return_value={"Id": self.CLIENT["container_id"], "State": {"Running": False}}), \
                patch.object(self.observer.thread, "start") as start, self.assertRaises(RuntimeError):
            self.observer.start()
        start.assert_not_called()
        self.assertFalse(self.evidence.has("observer-1-start.json"))

    def test_named_container_or_url_credentials_are_never_accepted(self):
        for origin, identity in (("https://8.130.18.85", "uniroute-nginx"), ("http://8.130.18.85", "d" * 64),
                                 ("https://secret@8.130.18.85", "d" * 64), ("https://8.130.18.85/path", "d" * 64)):
            with self.subTest(origin=origin), self.assertRaises(RuntimeError):
                rollout.Observer(self.evidence, origin, identity)

    def test_client_image_drift_makes_end_unproven(self):
        self.write_window()
        # Use a fresh actual window name; do not overwrite immutable fixtures.
        self.observer.name = "observer-2"
        with patch.object(self.observer.thread, "join"), patch.object(self.observer.thread, "is_alive", return_value=False), \
                patch.object(self.observer, "read_client", return_value=dict(self.CLIENT, image_digest="sha256:" + "f" * 64)):
            self.observer.stop()
        self.assertEqual(self.evidence.read("observer-2-end.json")["error"], "RuntimeError")

    def test_new_proof_is_strict_but_keeps_legacy_failure(self):
        self.write_window()
        self.assertEqual(rollout.observation_acceptance(self.evidence), "passed_short_requests_only")
        self.evidence.write("observer-2-start.json", {"at": "legacy"})
        bad = {"at": "start", "completed_at": "end", "protocol": "--http2", "exit": 16, "result": "000 0", "ok": False}
        self.evidence.write("observer-2-samples.jsonl", rollout.canonical(bad) + b"\n" + rollout.canonical(bad) + b"\n")
        self.evidence.write("observer-2-end.json", {"at": "end", "samples": 2, "failed": True, "error": None})
        self.assertEqual(rollout.observation_acceptance(self.evidence), "failed")

    def test_new_window_requires_both_protocols_in_order(self):
        self.write_window()
        path = self.evidence.root / "observer-1-samples.jsonl"
        rows = [rollout.load(line) for line in path.read_bytes().splitlines()]
        path.write_bytes(rollout.canonical(rows[1]) + b"\n" + rollout.canonical(rows[1]) + b"\n")
        self.assertEqual(rollout.observation_acceptance(self.evidence), "unproven_gap")

    def test_missing_or_unknown_version_cannot_downgrade_new_proof(self):
        for mutation in ("remove_version", "unknown_version", "legacy_start", "wrong_client", "false_verdict"):
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory(prefix="unirouter-proof-negative-") as directory:
                old = self.evidence
                self.evidence = rollout.Evidence(Path(directory).resolve())
                self.write_window()
                if mutation in ("remove_version", "unknown_version", "legacy_start"):
                    path = self.evidence.root / "observer-1-start.json"
                    value = rollout.load(path.read_bytes())
                    if mutation == "remove_version":
                        del value["contract_version"]
                    elif mutation == "unknown_version":
                        value["contract_version"] = "future"
                    else:
                        value = {"at": value["at"]}
                    path.write_bytes(rollout.canonical(value))
                else:
                    path = self.evidence.root / "observer-1-samples.jsonl"
                    rows = [rollout.load(line) for line in path.read_bytes().splitlines()]
                    if mutation == "wrong_client":
                        rows[0]["client_digest"] = "0" * 64
                    else:
                        rows[0]["exit"] = 92
                    path.write_bytes(b"".join(rollout.canonical(r) + b"\n" for r in rows))
                self.assertEqual(rollout.observation_acceptance(self.evidence), "unproven_gap")
                self.evidence = old


class ArtifactOwnerReadFixtureTests(unittest.TestCase):
    def load_carrier(self):
        path=Path(__file__).with_name("test-artifact-owner-docker.py")
        spec=importlib.util.spec_from_file_location("carrier_read_fixture",path)
        module=importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def test_exact_current_artifact_and_bridge_are_reported_separately(self):
        migrations=Path(__file__).resolve().parent.parent/"migrations"
        bridge,current=self.load_carrier().carrier_read_ledgers(migrations)
        self.assertEqual(bridge[0],"bridge")
        self.assertEqual(current[0],"current")
        self.assertEqual(bridge[1][-1],rollout.XINSIGHT_POLICY_HEAD)
        self.assertEqual(current[1][-1],rollout.XINSIGHT_ADOPTION_HEAD)
        self.assertEqual(current[1],sorted(p.name[:-7] for p in migrations.glob("*.up.sql")))
        self.assertEqual(bridge[1],current[1][:-1])

    def test_unknown_future_head_cannot_keep_a_stale_success_label(self):
        import tempfile
        with tempfile.TemporaryDirectory(prefix="ur-carrier-ledger-") as root:
            for name in ["000120_unpublished_content_reclamation","000121_gateway_pre_dispatch_terminal","000122_unknown"]:
                (Path(root)/(name+".up.sql")).write_text("")
            with self.assertRaisesRegex(RuntimeError,"exact reviewed artifact heads"):
                self.load_carrier().carrier_read_ledgers(Path(root))

    def test_historical_m117_m118_read_fixture_remains_supported(self):
        with tempfile.TemporaryDirectory(prefix="ur-carrier-historical-") as root:
            for name in ["000117_execution_observations", "000118_source_budget_snapshots"]:
                (Path(root)/(name+".up.sql")).write_text("")
            bridge,current=self.load_carrier().carrier_read_ledgers(Path(root), historical=True)
            self.assertEqual(bridge[1][-1], rollout.OBSERVATION_HEAD)
            self.assertEqual(current[1][-1], rollout.BUDGET_HEAD)

    def test_carrier_logging_and_grace_match_actual_daemon_configuration(self):
        backend=self.load_carrier().carrier_compose("synthetic-image",Path("/release"))["services"]["backend"]
        service,container,image=fixture()
        service["logging"]=backend["logging"]
        service["stop_grace_period"]=backend["stop_grace_period"]
        container["HostConfig"]["LogConfig"]={"Type":"local","Config":{"max-size":"10m","max-file":"3"}}
        rollout.check_service(service,container,image,"unirouter")
        # Reproduce both setup regressions against the unchanged product checker.
        container["Config"]["StopTimeout"]=40
        with self.assertRaisesRegex(RuntimeError,"stop grace"):
            rollout.check_service(service,container,image,"unirouter")
        container["Config"]["StopTimeout"]=2100
        del service["logging"]
        with self.assertRaisesRegex(RuntimeError,"logging"):
            rollout.check_service(service,container,image,"unirouter")


if __name__ == "__main__":
    unittest.main()
