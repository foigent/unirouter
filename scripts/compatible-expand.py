#!/usr/bin/env python3
"""Exact M113/M114 dev/test bridge owner; does not roll containers or publish data.

Run on the verified Target after storage/backup and both bridge rollouts. The
installed ingress lock excludes the ordinary controller for the whole check/apply.
"""
import argparse
import fcntl
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import subprocess

spec = importlib.util.spec_from_file_location("runtime_ingress", Path(__file__).with_name("runtime-ingress.py"))
ingress = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ingress)
HEADS = ["000113_glm_stream_function_tools_qualification", "000114_capability_delivery_expand"]


def verify_bridge(runtime, backend, manifest_wire):
    manifest = json.loads(manifest_wire)
    canonical = json.dumps(manifest, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()
    if (canonical != manifest_wire.strip() or hashlib.sha256(canonical).hexdigest() != runtime.get("support_manifest_digest")
            or manifest.get("schema_heads") != HEADS or runtime.get("supported_schema_heads") != HEADS
            or runtime.get("compatibility_epoch") != "unirouter.runtime-compatibility.v1"
            or runtime.get("artifact_digest") != backend["Image"]):
        raise RuntimeError("running image/support manifest does not match the exact bridge")
    for bindings in (backend["HostConfig"].get("PortBindings") or {}).values():
        if any(b.get("HostIp") not in ("127.0.0.1", "::1") for b in (bindings or [])):
            raise RuntimeError("public backend port bypasses the verified ingress")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", required=True, type=Path)
    parser.add_argument("--container", default="uniroute-nginx")
    parser.add_argument("--expected-container-id", required=True)
    parser.add_argument("--expected-hostname", required=True)
    parser.add_argument("--expected-system-identifier", required=True)
    parser.add_argument("--environment", required=True, choices=("development", "test", "integration"))
    parser.add_argument("--migration-digest", required=True)
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
    with (args.directory / ".ingress.lock").open("a") as lock:
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
        for role, host in ingress.HOSTS.items():
            backend = json.loads(ingress.run(["docker", "inspect", host]))[0]
            manifest = ingress.docker_exec(host, "./uniroute", "runtime-generation", "support")
            verify_bridge(before["health"][role], backend, manifest)
            fleet[host] = before["health"][role]
            manifests[host] = json.loads(manifest)
        document = {"environment": args.environment, "system_identifier": args.expected_system_identifier,
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
