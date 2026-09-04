#!/usr/bin/env python3
"""Read-only planning / bounded apply for the installed same-schema Nginx carrier.

Never stops a backend, installs the carrier, changes model Slots or claims
mixed-version compatibility. Direct debug ports are outside this contract.
"""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time

MOUNT = "/etc/nginx/runtime-membership"
HOSTS = {"stable": "uniroute-api", "canary": "uniroute-api-canary"}


def digest(data):
    return hashlib.sha256(data).hexdigest()


def membership(state):
    primary = HOSTS["canary"] if state == "canary" else HOSTS["stable"]
    servers = ("    server uniroute-api:8080 weight=9;\n"
               "    server uniroute-api-canary:8080 weight=1;") if state == "both" else f"    server {primary}:8080;"
    return (f"upstream backend_stable {{\n    server {primary}:8080;\n}}\n\n"
            f"upstream backend_canary {{\n{servers}\n}}\n\n"
            "upstream frontend {\n    server uniroute-frontend:80;\n}\n").encode()


def run(args, timeout=15):
    result = subprocess.run(args, capture_output=True, timeout=timeout, check=False)
    if result.returncode:
        # Do not emit arbitrary configuration/environment or credentials.
        raise RuntimeError(f"command failed ({result.returncode}): {args[0]}")
    return result.stdout


def docker_exec(container, *args):
    return run(["docker", "exec", container, *args])


def workers(container):
    script = r"""for p in /proc/[0-9]*; do
test -r "$p/cmdline" || continue
name=$(tr '\000' ' ' < "$p/cmdline")
case "$name" in
  "nginx: worker process"*) awk '{print $1 ":" $22}' "$p/stat" ;;
esac
done"""
    result = docker_exec(container, "sh", "-c", script).decode().splitlines()
    if not result or any(not re.fullmatch(r"[0-9]+:[0-9]+", item) for item in result):
        raise RuntimeError("cannot prove Nginx worker process identities")
    return set(result)


def check_configuration(config):
    values = re.findall(rb"(?m)^\s*worker_shutdown_timeout\s+([^;]+);", config)
    if any(value.strip() not in (b"0", b"0s", b"0ms") for value in values):
        raise RuntimeError("finite worker_shutdown_timeout can interrupt accepted requests")
    if b"non_idempotent" in config:
        raise RuntimeError("non-idempotent proxy retry is outside the continuity contract")
    if re.search(rb"proxy_pass\s+https?://uniroute-api(?:-canary)?[:/;]", config):
        raise RuntimeError("direct backend proxy_pass bypasses managed membership")
    if b"include /etc/nginx/runtime-membership/upstreams.conf;" not in config:
        raise RuntimeError("managed membership include is not installed")


def snapshot(args):
    inspected = json.loads(run(["docker", "inspect", args.container]))
    if len(inspected) != 1 or inspected[0]["Id"] != args.expected_container_id:
        raise RuntimeError("Nginx container identity changed")
    mounted = [item for item in inspected[0]["Mounts"] if item["Destination"] == MOUNT]
    if len(mounted) != 1 or Path(mounted[0]["Source"]).resolve() != args.directory:
        raise RuntimeError("exact directory carrier is not mounted; bootstrap is required")
    path = args.directory / "upstreams.conf"
    if path.is_symlink() or not path.is_file():
        raise RuntimeError("membership must be a regular non-symlink file")
    before = path.read_bytes()
    states = [state for state in ("stable", "canary", "both") if membership(state) == before]
    if len(states) != 1:
        raise RuntimeError("current membership is outside the canonical carrier")
    if docker_exec(args.container, "cat", MOUNT + "/upstreams.conf") != before:
        raise RuntimeError("container-visible membership differs from host bytes")
    configuration = docker_exec(args.container, "nginx", "-T")
    check_configuration(configuration)
    health = {}
    for role, host in HOSTS.items():
        body = docker_exec(args.container, "wget", "-q", "-O", "-", f"http://{host}:8080/health/ready")
        runtime = json.loads(body).get("runtime", {})
        if (runtime.get("state") != "serving" or
                not re.fullmatch(r"[a-f0-9]{64}", runtime.get("schema_ledger_digest", "")) or
                not runtime.get("instance") or runtime.get("drain_timeout_seconds", 0) <= 0):
            raise RuntimeError(f"{role} lacks healthy exact runtime metadata")
        backend = json.loads(run(["docker", "inspect", host]))[0]
        stop_timeout = backend["Config"].get("StopTimeout")
        if not isinstance(stop_timeout, int) or stop_timeout <= runtime["drain_timeout_seconds"]:
            raise RuntimeError(f"{role} Compose stop grace does not exceed application drain")
        health[role] = runtime
    if health["stable"]["schema_ledger_digest"] != health["canary"]["schema_ledger_digest"]:
        raise RuntimeError("different schema generations require the compatibility bridge")
    return {"before": before, "before_state": states[0], "after": membership(args.desired),
            "configuration_sha256": digest(configuration), "workers": workers(args.container), "health": health}


def replace(path, contents):
    fd, temporary = tempfile.mkstemp(prefix=".membership-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as output:
            os.fchmod(output.fileno(), 0o644)
            output.write(contents)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
        directory_fd = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory_fd)
        finally:
            os.close(directory_fd)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def apply(args, before):
    if digest(before["before"]) != args.expected_config_sha256:
        raise RuntimeError("membership predecessor changed; no mutation")
    if before["configuration_sha256"] != args.expected_nginx_sha256:
        raise RuntimeError("effective Nginx configuration changed; no mutation")
    # Even identical desired bytes need a fresh observed reload: a previous
    # ambiguous apply may still have old workers using an older configuration.
    path = args.directory / "upstreams.conf"
    replace(path, before["after"])
    try:
        if docker_exec(args.container, "cat", MOUNT + "/upstreams.conf") != before["after"]:
            raise RuntimeError("container did not observe new membership bytes")
        docker_exec(args.container, "nginx", "-t")
    except Exception:
        # No reload attempted: safe to restore local pre-state.
        replace(path, before["before"])
        raise
    # Any subsequent error is ambiguous. Retain both backends and new bytes.
    docker_exec(args.container, "nginx", "-s", "reload")
    deadline = time.monotonic() + args.wait_seconds
    new_seen = False
    while time.monotonic() < deadline:
        current = workers(args.container)
        new_seen = new_seen or bool(current - before["workers"])
        if new_seen and not (current & before["workers"]):
            if docker_exec(args.container, "cat", MOUNT + "/upstreams.conf") != before["after"]:
                raise RuntimeError("membership changed during withdrawal")
            return "old_proxy_workers_exited"
        time.sleep(min(1, max(0, deadline - time.monotonic())))
    raise RuntimeError("withdrawal unproven before deadline; retain old backend and stop this rollout")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", required=True, type=Path)
    parser.add_argument("--container", default="uniroute-nginx")
    parser.add_argument("--expected-container-id", required=True)
    parser.add_argument("--desired", required=True, choices=("stable", "canary", "both"))
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--expected-config-sha256")
    parser.add_argument("--expected-nginx-sha256")
    parser.add_argument("--wait-seconds", type=int, default=2100)
    args = parser.parse_args()
    args.directory = args.directory.resolve(strict=True)
    if not re.fullmatch(r"[a-f0-9]{64}", args.expected_container_id):
        parser.error("expected-container-id must be the exact full Docker container ID")
    if not 1 <= args.wait_seconds <= 90000:
        parser.error("wait-seconds must be in 1..90000")
    if args.apply and any(not re.fullmatch(r"[a-f0-9]{64}", value or "") for value in
                          (args.expected_config_sha256, args.expected_nginx_sha256)):
        parser.error("apply requires both predecessor digests from read-only planning")
    if args.apply:
        with (args.directory / ".ingress.lock").open("a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            before = snapshot(args)
            outcome = apply(args, before)
    else:
        before, outcome = snapshot(args), "read_only"
    print(json.dumps({"outcome": outcome, "container_id": args.expected_container_id,
                      "before_state": before["before_state"], "desired": args.desired,
                      "before_sha256": digest(before["before"]), "after_sha256": digest(before["after"]),
                      "nginx_sha256": before["configuration_sha256"], "runtime": before["health"],
                      "backend_stopped": False}, sort_keys=True))


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, ValueError, OSError, subprocess.TimeoutExpired) as error:
        print(f"runtime ingress refused: {error}; no backend stop was performed", file=sys.stderr)
        sys.exit(1)
