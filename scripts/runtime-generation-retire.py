#!/usr/bin/env python3
"""Retire one exited development/test runtime identity from a completed formal rollout.

Run check, then apply with the same historical rollout input and role. The old
container must have been replaced and removed. Neither lease expiry nor a
successful schema check is process-exit evidence.
"""
import argparse
import contextlib
import fcntl
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("formal_rollout", Path(__file__).with_name("internal-target-rollout.py"))
rollout = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rollout)
spec = importlib.util.spec_from_file_location("compatible_expand", Path(__file__).with_name("compatible-expand.py"))
expand = importlib.util.module_from_spec(spec)
spec.loader.exec_module(expand)


@contextlib.contextmanager
def existing_locks(target):
    # A check creates no files. Match the formal rollout's lock order.
    fds = []
    try:
        for path in (rollout.LIFECYCLE_LOCK, Path(target["ingress_directory"]) / ".ingress.lock"):
            fd = os.open(path, os.O_RDWR | os.O_NOFOLLOW)
            fds.append(fd)
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield
    finally:
        for fd in reversed(fds):
            os.close(fd)


def owner_command(container_id, command, document=None):
    payload = b"" if document is None else rollout.canonical(document)
    result = subprocess.run(["docker", "exec", "-i", container_id, "./uniroute", "runtime-generation", command],
                            input=payload, capture_output=True, timeout=30, env=rollout.ENV)
    rollout.require(result.returncode == 0, "runtime-generation owner refused or commit is unknown; inspect exact row before retry")
    return rollout.load(result.stdout)


def checked_record(evidence, plan, stage):
    rollout.require(evidence.has("start-" + stage + ".json") and
                    evidence.has("done-" + stage + ".json"), "formal rollout stage evidence is missing")
    record = evidence.read("done-" + stage + ".json")
    rollout.require(record["plan_digest"] == plan["plan_digest"] and
                    evidence.read("start-" + stage + ".json")["plan_digest"] == plan["plan_digest"],
                    "rollout stage is not bound to retained Plan")
    return record["result"]


def retirement_document(operation, role):
    operation.identity()
    plan = operation.retained_plan()
    evidence = operation.evidence
    old_instance = plan["old_instances"][role]
    old_container = plan["containers"][role]
    withdrawal = checked_record(evidence, plan, "withdraw-" + role)
    stopped = checked_record(evidence, plan, "stop-" + role)
    created = checked_record(evidence, plan, "create-" + role)
    rejoined = checked_record(evidence, plan, "rejoin-" + role)
    checked_record(evidence, plan, "verify")
    rollout.require(withdrawal["change_id"] == operation.change_id("withdraw-" + role) and
                    withdrawal["outcome"] in ("old_proxy_workers_exited", "already_applied"),
                    "old ingress withdrawal is unproved")
    rollout.require(stopped == {"container_id": old_container["id"], "exit": 0,
                                "shutdown_events": stopped["shutdown_events"], "incomplete": False} and
                    any(event.endswith("Server exited") for event in stopped["shutdown_events"]),
                    "old process clean exit is unproved")
    rollout.require(created["container_id"] != old_container["id"] and
                    created["instance"] != old_instance and
                    rejoined["change_id"] == operation.change_id("rejoin-" + role),
                    "old process replacement is unproved")
    rollout.require(rollout.inspect(old_container["id"], missing_ok=True) is None,
                    "old container still exists and could be restarted")
    backends = {}
    for name in ("uniroute-api", "uniroute-api-canary"):
        backend = rollout.inspect(name)
        rollout.require(backend["State"]["Running"] and backend["Id"] != old_container["id"],
                        "current backend process identity is unproved")
        backends[name] = backend
    expand.verify_recovery_process_inventory(backends)
    current = owner_command(backends["uniroute-api"]["Id"], "inspect")
    rollout.require(current["head_status"] == "applied", "current ingress head is not applied")
    generations = current["generations"]
    rows = [row for row in generations if row["instance"] == old_instance]
    rollout.require(len(rows) == 1, "old runtime identity is absent or duplicated")
    row = rows[0]
    rollout.require(row["artifact_digest"] == old_container["image"] and row["state"] != "stopped" and
                    row["ingress_state"] == "absent" and not row["lease_live"],
                    "old runtime identity is active or differs from retained container")
    members = []
    for name, backend in backends.items():
        role_name = "stable" if name == "uniroute-api" else "canary"
        runtime = operation.ready(role_name, backend["Image"])
        rollout.require(runtime["artifact_digest"] == backend["Image"] and
                        any(row["instance"] == runtime["instance"] and row["ingress_state"] == "receiving"
                            for row in generations), "current process/ingress identity differs")
        members.append({"instance": runtime["instance"], "artifact_digest": backend["Image"]})
    members.sort(key=lambda member: member["instance"])
    rollout.require(sorted(({"instance": row["instance"], "artifact_digest": row["artifact_digest"]}
                            for row in generations if row["ingress_state"] != "absent"),
                           key=lambda member: member["instance"]) == members,
                    "database receiver membership differs from current processes")
    latest = owner_command(backends["uniroute-api"]["Id"], "read", {"change_id": current["head"]})
    installed = rollout.regular(Path(operation.target["ingress_directory"]) / "upstreams.conf").read_bytes()
    rollout.require(latest["status"] == "applied" and latest["plan"]["membership_after"] == members and
                    latest["plan"]["desired_config_digest"] == rollout.digest(installed) and
                    operation.ingress.docker_exec("uniroute-nginx", "cat", operation.ingress.MOUNT + "/upstreams.conf") == installed,
                    "installed ingress differs from the exact applied current head")
    receipt = owner_command(backends["uniroute-api"]["Id"], "read", {"change_id": withdrawal["change_id"]})
    rollout.require(receipt["status"] == "applied" and
                    receipt["plan"]["desired_config_digest"] == withdrawal["after_sha256"] and
                    all(member["instance"] != old_instance for member in receipt["plan"]["membership_after"]),
                    "historical withdrawal receipt does not exclude old identity")
    proof = {"plan_digest": plan["plan_digest"], "role": role, "old_instance": old_instance,
             "old_container": old_container, "withdrawal": withdrawal, "stop": stopped,
             "replacement": created, "rejoin": rejoined, "current_ingress_head": current["head"]}
    target = operation.target
    return {"instance_id": old_instance, "artifact_digest": row["artifact_digest"],
            "manifest_digest": row["support_manifest_digest"],
            "compatibility_epoch": row["compatibility_epoch"], "expected_schema_head": row["schema_head"],
            "expected_revision": row["revision"], "expected_ingress_head": current["head"],
            "expected_environment": target["environment"],
            "expected_database_system_id": target["database_system_id"],
            "host_proof_sha256": "sha256:" + rollout.digest(rollout.canonical(proof))}, backends["uniroute-api"]["Id"]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("check", "apply"))
    parser.add_argument("--rollout-input", required=True, type=Path)
    parser.add_argument("--role", required=True, choices=("stable", "canary"))
    args = parser.parse_args()
    operation = rollout.Rollout(rollout.document(args.rollout_input))
    os.environ.clear()
    os.environ.update(rollout.ENV)
    with existing_locks(operation.target):
        document, owner = retirement_document(operation, args.role)
        result = owner_command(owner, "stop-" + ("check" if args.action == "check" else "apply"), document)
        rollout.require(result["instance_id"] == document["instance_id"] and
                        result["host_proof_sha256"] == document["host_proof_sha256"] and
                        result["status"] == ("eligible" if args.action == "check" else "stopped"),
                        "retirement owner response differs from exact host proof")
        print(json.dumps(result, sort_keys=True))


if __name__ == "__main__":
    main()
