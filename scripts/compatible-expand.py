#!/usr/bin/env python3
"""Finite M113–M123 dev/test bridge owner; does not roll containers or publish data.

Run on the verified Target after storage/backup and both bridge rollouts. The
installed ingress lock excludes the ordinary controller for the whole check/apply.
"""
import argparse
import contextlib
import fcntl
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import subprocess
import sys

sys.dont_write_bytecode = True

spec = importlib.util.spec_from_file_location("runtime_ingress", Path(__file__).with_name("runtime-ingress.py"))
ingress = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ingress)
HEADS = ["000113_glm_stream_function_tools_qualification", "000114_capability_delivery_expand", "000115_stream_counter_version", "000116_execution_source_facts", "000117_execution_observations", "000118_source_budget_snapshots", "000119_execution_content_capture", "000120_unpublished_content_reclamation", "000121_gateway_pre_dispatch_terminal", "000122_scoped_content_policy", "000123_xinsight_policy_delegation"]
COUNTERS = [{"kind": "stream_counter", "id": "unirouter.stream-counter.v2"}, {"kind": "stream_counter", "id": "unirouter.stream-counter.v3"}]
SOURCE_OBSERVATIONS = {"kind": "source_observations", "id": "unirouter.source-observation.v1"}
SOURCE_DISPATCH = {"kind": "source_dispatch", "id": "unirouter.source-dispatch-outbox.v1"}
SOURCE_FACTS = {"kind": "source_facts", "id": "unirouter.source-fact.v1"}
BUDGET_REQUIREMENTS = [{"kind":"source_budgets","id":"unirouter.budget-snapshot.v1"},{"kind":"financial_journal","id":"unirouter.gateway-financial-journal.v2"}]
CONTENT_LIFECYCLE = {"kind": "source_content", "id": "unirouter.source-content-lifecycle.v1"}
PRE_DISPATCH_OBSERVATIONS = {"kind": "source_observations", "id": "unirouter.source-observation.v2"}
SCOPED_CONTENT = [{"kind": "source_content", "id": "unirouter.content-policy.v2"}, {"kind": "media_request", "id": "unirouter.media-request-expiry.v1"}]
XINSIGHT_POLICY = [{"kind": "xinsight_policy", "id": "unirouter.xinsight.policy-write.v1"}, {"kind": "xinsight_source_enrollment", "id": "unirouter.xinsight.source-enrollment.v1"}]


def verify_bridge(runtime, backend, manifest_wire, target_version=HEADS[1]):
    manifest = json.loads(manifest_wire)
    canonical = json.dumps(manifest, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()
    if (canonical != manifest_wire.strip() or hashlib.sha256(canonical).hexdigest() != runtime.get("support_manifest_digest")
            or manifest.get("schema_heads") not in [HEADS[:end] for end in range(2, len(HEADS) + 1)] or runtime.get("supported_schema_heads") != manifest.get("schema_heads")
            or target_version not in manifest.get("schema_heads", [])
            or (target_version in HEADS[2:] and any(ref not in manifest.get("implementations", []) for ref in COUNTERS))
            or (target_version in HEADS[3:] and SOURCE_FACTS not in manifest.get("implementations", []))
            or (target_version in HEADS[4:] and any(ref not in manifest.get("implementations", []) for ref in [SOURCE_OBSERVATIONS, SOURCE_DISPATCH]))
            or (target_version in HEADS[5:] and any(ref not in manifest.get("implementations", []) for ref in BUDGET_REQUIREMENTS))
            or (target_version in HEADS[6:] and CONTENT_LIFECYCLE not in manifest.get("implementations", []))
            or (target_version in HEADS[8:] and PRE_DISPATCH_OBSERVATIONS not in manifest.get("implementations", []))
            or (target_version in HEADS[9:] and any(ref not in manifest.get("implementations", []) for ref in SCOPED_CONTENT))
            or (target_version in HEADS[10:] and any(ref not in manifest.get("implementations", []) for ref in XINSIGHT_POLICY))
            or runtime.get("compatibility_epoch") != "unirouter.runtime-compatibility.v1"
            or runtime.get("artifact_digest") != backend["Image"]):
        raise RuntimeError("running image/support manifest does not match the exact bridge")
    for bindings in (backend["HostConfig"].get("PortBindings") or {}).values():
        if any(b.get("HostIp") not in ("127.0.0.1", "::1") for b in (bindings or [])):
            raise RuntimeError("public backend port bypasses the verified ingress")


def verify_recovery_process_inventory(backends):
    """Single-host proof complements DB leases, including expired/absent actors.

    The lifecycle lock prevents another formal rollout starting while inspecting.
    Never treat a missing/unknown host process as a supported recovery binary.
    """
    ids = {value["Id"] for value in backends.values()}
    pids = {str(value["State"]["Pid"]) for value in backends.values()}
    observed = set()
    for line in ingress.run(["ps", "-eo", "pid=,comm="]).decode().splitlines():
        pieces = line.split(None, 1)
        if len(pieces) != 2 or Path(pieces[1]).name != "uniroute":
            continue
        pid = pieces[0]
        cgroup = Path("/proc") / pid / "cgroup"
        try:
            membership = cgroup.read_text()
        except OSError as error:
            raise RuntimeError("runtime process inventory changed during preflight") from error
        if not any(container in membership for container in ids):
            raise RuntimeError("unaccounted host/container recovery process")
        observed.add(pid)
    if not pids.issubset(observed):
        raise RuntimeError("both exact backend process identities must be observable")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", required=True, type=Path)
    parser.add_argument("--container", default="uniroute-nginx")
    parser.add_argument("--expected-container-id", required=True)
    parser.add_argument("--expected-hostname", required=True)
    parser.add_argument("--expected-system-identifier", required=True)
    parser.add_argument("--environment", required=True, choices=("development", "test", "integration"))
    parser.add_argument("--migration-digest", required=True)
    parser.add_argument("--target-version", choices=HEADS[1:], default=HEADS[1])
    parser.add_argument("--expected-config-sha256", required=True)
    parser.add_argument("--expected-nginx-sha256", required=True)
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    args.directory = args.directory.resolve(strict=True)
    args.desired = "both"  # Only used to build a read-only snapshot; never applied.
    for value in (args.expected_container_id, args.expected_config_sha256, args.expected_nginx_sha256):
        if not re.fullmatch(r"[a-f0-9]{64}", value):
            raise ValueError("exact Docker/config digests are required")
    if not re.fullmatch(r"sha256:[a-f0-9]{64}", args.migration_digest):
        raise ValueError("exact reviewed migration digest is required")
    if ingress.run(["hostname"]).decode().strip() != args.expected_hostname:
        raise RuntimeError("Target hostname changed")
    with contextlib.ExitStack() as stack:
        lifecycle = stack.enter_context(Path("/run/lock/unirouter-docker-lifecycle.lock").open("a"))
        fcntl.flock(lifecycle, fcntl.LOCK_EX | fcntl.LOCK_NB)
        lock = stack.enter_context((args.directory / ".ingress.lock").open("a"))
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        before = ingress.snapshot(args)
        if (ingress.digest(before["before"]) != args.expected_config_sha256
                or before["configuration_sha256"] != args.expected_nginx_sha256):
            raise RuntimeError("installed ingress differs from the reviewed predecessor")
        # An old worker may still address a removed generation. Do not infer its
        # membership from the newest on-disk config; complete withdrawal first.
        process_names = ingress.docker_exec(args.container, "sh", "-c", "for p in /proc/[0-9]*/cmdline; do tr '\\000' ' ' < \"$p\" 2>/dev/null; echo; done")
        if b"nginx: worker process is shutting down" in process_names:
            raise RuntimeError("old proxy generation still exists; finish its withdrawal before expansion")
        fleet = {}
        manifests = {}
        backends = {}
        for role, host in ingress.HOSTS.items():
            backend = json.loads(ingress.run(["docker", "inspect", host]))[0]
            manifest = ingress.docker_exec(host, "./uniroute", "runtime-generation", "support")
            verify_bridge(before["health"][role], backend, manifest, args.target_version)
            backends[host] = backend
            fleet[host] = before["health"][role]
            manifests[host] = json.loads(manifest)
        if args.target_version in HEADS[2:]:
            verify_recovery_process_inventory(backends)
        document = {"target_version": args.target_version, "environment": args.environment, "system_identifier": args.expected_system_identifier,
                    "migration_digest": args.migration_digest, "fleet": fleet, "support_manifests": manifests}
        action = "apply" if args.apply else "check"
        result = subprocess.run(["docker", "exec", "-i", ingress.HOSTS["stable"], "./uniroute", "compatible-expand", action],
                                input=json.dumps(document, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode(), capture_output=True, timeout=120, check=False)
        if result.returncode:
            raise RuntimeError("schema owner refused or result is unknown; read the exact ledger before recovery")
        print(json.dumps({"owner": json.loads(result.stdout), "nginx_container": args.expected_container_id,
                          "ingress_config_digest": args.expected_config_sha256, "instances": fleet}, sort_keys=True))


if __name__ == "__main__":
    main()
