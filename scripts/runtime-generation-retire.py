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
        rollout.HELD_LOCK_FDS.extend(fds)
        yield
    finally:
        rollout.HELD_LOCK_FDS.clear()
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



def maintenance_schema_predecessor(artifact, actual):
    allowed = (("000125_xinsight_control_credential_lifecycle", "000126_commercial_license_retirement"),
               ("000126_commercial_license_retirement", "000127_xinsight_communication_policy"))
    rollout.require(tuple(artifact[-2:]) in allowed,
                    "maintenance requires an exact supported M125/M126 or M126/M127 artifact")
    rollout.require(actual == artifact[:-1], "maintenance requires the complete exact predecessor ledger")


class MaintenanceRetirement(rollout.Rollout):
    """Exact M125/M126 predecessor withdrawal using its bounded successor CLI.

    This owner does not apply schema or start replacement serving processes.
    Each mutation is explicit and retains evidence for unknown-result recovery.
    """
    maintenance_retirement = True

    def __init__(self, spec):
        rollout.require(spec.get("runtime_owner") == "artifact_cli" and not spec.get("transition"),
                        "maintenance requires artifact_cli without a rolling transition")
        super().__init__(spec)

    def owner(self):
        return ""  # Independent carrier ignores a serving-container receiver.

    def retained_plan(self):
        rollout.require(self.evidence.read("input.json") == self.spec, "maintenance input changed")
        plan = self.evidence.read("plan.json")
        checked = dict(plan)
        expected = checked.pop("plan_digest")
        rollout.require(rollout.digest(rollout.canonical(checked)) == expected and
                        plan["input_digest"] == rollout.digest(rollout.canonical(self.spec)),
                        "maintenance plan digest differs")
        rollout.require(self.evidence.read("prepared.json")["plan_digest"] == expected,
                        "maintenance backup is not confirmed")
        rollout.require(rollout.digest(rollout.regular(self.target["overlay"]).read_bytes()) ==
                        plan["old_overlay_digest"], "maintenance overlay changed")
        return plan

    def plan(self):
        self.identity()
        self.sources()
        self.images()
        artifact = sorted(p.name[:-7] for p in (self.new_source / "migrations").glob("*.up.sql"))
        actual = rollout.load(self.sql("SELECT jsonb_agg(version ORDER BY version) FROM schema_migrations"))
        maintenance_schema_predecessor(artifact, actual)
        before = self.ingress.snapshot(self.ingress_args("both"))
        rollout.require(before["before_state"] == "both", "maintenance starts from both healthy receivers")
        current = self.ingress.owner_command(self.owner(), "inspect")
        rollout.require(current["head_status"] == "applied", "maintenance predecessor must be applied")
        members = self.ingress.state_members("both", before["health"])
        latest = self.ingress.owner_command(self.owner(), "read", {"change_id": current["head"]})
        rollout.require(latest["status"] == "applied" and latest["plan"]["membership_after"] == members and
                        latest["plan"]["desired_config_digest"] == rollout.digest(before["before"]),
                        "maintenance predecessor differs from actual receivers")
        containers = {role: rollout.inspect(rollout.CONTAINERS[role]) for role in ("stable", "canary")}
        expand.verify_recovery_process_inventory(containers)
        for role, container in containers.items():
            labels = container["Config"].get("Labels", {})
            rollout.require(labels.get("com.docker.compose.project") == self.target["project"] and
                            labels.get("com.docker.compose.service") == rollout.SERVICES[role],
                            "maintenance backend is outside the exact project/service")
            rollout.require(rollout.environment(container["Config"]["Env"]).get("RUNTIME_ENVIRONMENT") ==
                            self.target["environment"], "maintenance backend environment differs")
        live = sorted(({"instance": row["instance"], "artifact_digest": row["artifact_digest"]}
                       for row in current["generations"] if row["state"] != "stopped"),
                      key=lambda row: row["instance"])
        rollout.require(live == members, "retire historical generations before full maintenance")
        overlay_digest = rollout.digest(rollout.regular(self.target["overlay"]).read_bytes())
        plan = {"format": "unirouter.commercial-retirement-plan.v1",
                "input_digest": rollout.digest(rollout.canonical(self.spec)),
                "old_overlay_digest": overlay_digest, "new_overlay_digest": overlay_digest,
                "pins": self.pins(), "nginx_static_digest": self.ingress_static_digest(),
                "initial_ingress_head": current["head"],
                "containers": {role: {"id": c["Id"], "image": c["Image"]} for role, c in containers.items()},
                "old_instances": {role: h["instance"] for role, h in before["health"].items()}}
        plan["plan_digest"] = rollout.digest(rollout.canonical(plan))
        return plan

    def prepare(self):
        rollout.require(not self.evidence.root.exists(), "existing maintenance evidence; inspect it")
        plan = self.plan()
        self.storage()
        self.evidence.root.mkdir(mode=0o700)
        self.evidence.write("input.json", self.spec)
        self.evidence.write("plan.json", plan)
        backup = self.evidence.root / "before.dump"
        with backup.open("xb") as output:
            os.fchmod(output.fileno(), 0o600)
            result = subprocess.run(["docker", "exec", "uniroute-postgres", "pg_dump",
                                     "-U", "uniroute", "-Fc", "uniroute"],
                                    stdout=output, stderr=subprocess.DEVNULL, timeout=180, env=rollout.ENV)
            rollout.require(result.returncode == 0, "backup failed; no runtime mutation")
            output.flush()
            os.fsync(output.fileno())
        with backup.open("rb") as source:
            result = subprocess.run(["docker", "exec", "-i", "uniroute-postgres", "pg_restore", "--list"],
                                    stdin=source, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                    timeout=60, env=rollout.ENV)
            rollout.require(result.returncode == 0, "backup verification failed; no runtime mutation")
        self.evidence.write("prepared.json", {"plan_digest": plan["plan_digest"],
                                            "backup_digest": rollout.digest(backup.read_bytes())})
        return {"status": "prepared", "plan_digest": plan["plan_digest"]}

    def prove_withdrawal(self, _role=None):
        self.identity()
        plan = self.retained_plan()
        rollout.require(self.pins() == plan["pins"], "schema/publication/execution pins changed")
        rollout.require(self.ingress_static_digest() == plan["nginx_static_digest"],
                        "unmanaged proxy configuration changed")
        current = self.ingress.owner_command(self.owner(), "inspect")
        rollout.require(current["head"] == self.change_id("withdraw-all") and
                        current["head_status"] == "applied", "full withdrawal is not applied")
        latest = self.ingress.owner_command(self.owner(), "read", {"change_id": current["head"]})
        installed = rollout.regular(Path(self.target["ingress_directory"]) / "upstreams.conf").read_bytes()
        rollout.require(latest["status"] == "applied" and latest["plan"]["membership_after"] == [] and
                        latest["plan"]["desired_config_digest"] == rollout.digest(installed) and
                        installed == self.ingress.membership("none") and
                        self.ingress.docker_exec("uniroute-nginx", "cat", self.ingress.MOUNT + "/upstreams.conf") == installed,
                        "exact empty installed membership is unproved")
        rollout.require(all(row["ingress_state"] == "absent" for row in current["generations"]),
                        "a generation still receives traffic")
        return current

    def stage(self, action, role=None):
        self.identity()
        plan = self.retained_plan()
        rollout.require(self.pins() == plan["pins"], "maintenance protected pins changed")
        name = "withdraw-all" if action == "withdraw" else action + "-" + role
        if self.evidence.has("done-" + name + ".json"):
            if action == "withdraw":
                self.prove_withdrawal()
            else:
                old = rollout.inspect(plan["containers"][role]["id"], missing_ok=True)
                if action == "stop" and old is None:
                    removed = checked_record(self.evidence, plan, "remove-" + role)
                    rollout.require(removed == {"container_id": plan["containers"][role]["id"], "removed": True},
                                    "stopped container disappeared without exact removal evidence")
                else:
                    rollout.require(old is None if action == "remove" else
                                    old is not None and not old["State"]["Running"],
                                    "completed maintenance stage drifted")
            return checked_record(self.evidence, plan, name)
        if not self.evidence.has("start-" + name + ".json"):
            self.evidence.write("start-" + name + ".json",
                                {"at": rollout.utcnow(), "plan_digest": plan["plan_digest"]})
        rollout.require(self.evidence.read("start-" + name + ".json")["plan_digest"] == plan["plan_digest"],
                        "maintenance stage predecessor differs from retained Plan")
        if action == "withdraw":
            rollout.require(self.ingress_static_digest() == plan["nginx_static_digest"], "proxy configuration changed")
            args = self.ingress_args("none", self.change_id("withdraw-all"))
            args.maintenance = True
            before = self.ingress.snapshot(args)
            args.expected_config_sha256 = rollout.digest(before["before"])
            args.expected_nginx_sha256 = before["configuration_sha256"]
            outcome = self.ingress.coordinated_apply(args, before)
            self.prove_withdrawal()
            result = {"outcome": outcome, "change_id": args.change_id,
                      "after_sha256": rollout.digest(before["after"])}
        elif action == "stop":
            result = self.stop(role, plan)
        elif action == "remove":
            self.prove_withdrawal(role)
            stopped = checked_record(self.evidence, plan, "stop-" + role)
            old_id = plan["containers"][role]["id"]
            rollout.require(stopped["container_id"] == old_id and stopped["exit"] == 0 and
                            not stopped["incomplete"] and
                            any(event.endswith("Server exited") for event in stopped["shutdown_events"]),
                            "clean exit must precede removal")
            old = rollout.inspect(old_id, missing_ok=True)
            if old is not None:
                rollout.require(not old["State"]["Running"] and old["State"]["ExitCode"] == 0 and
                                not old["State"]["OOMKilled"], "refuse removing a live or failed container")
                self.begin_command(name, plan)
                rollout.run(["docker", "rm", old_id])  # Exact stopped container; never volumes.
            rollout.require(rollout.inspect(old_id, missing_ok=True) is None, "container removal unconfirmed")
            result = {"container_id": old_id, "removed": True}
        else:
            raise RuntimeError("unsupported maintenance stage")
        self.evidence.write("done-" + name + ".json", {"plan_digest": plan["plan_digest"], "result": result})
        return result

    def retirement_document(self, role):
        current = self.prove_withdrawal(role)
        plan = self.retained_plan()
        withdrawal = checked_record(self.evidence, plan, "withdraw-all")
        rollout.require(withdrawal["change_id"] == self.change_id("withdraw-all") and
                        withdrawal["outcome"] in ("old_proxy_workers_exited", "already_applied") and
                        withdrawal["after_sha256"] == rollout.digest(self.ingress.membership("none")),
                        "full withdrawal receipt is unproved")
        for name in ("stable", "canary"):
            removed = checked_record(self.evidence, plan, "remove-" + name)
            stopped = checked_record(self.evidence, plan, "stop-" + name)
            old_id = plan["containers"][name]["id"]
            rollout.require(removed == {"container_id": old_id, "removed": True} and
                            stopped["container_id"] == old_id and stopped["exit"] == 0 and
                            not stopped["incomplete"] and
                            any(event.endswith("Server exited") for event in stopped["shutdown_events"]),
                            "exact clean exit and removal receipts are unproved")
            rollout.require(rollout.inspect(plan["containers"][name]["id"], missing_ok=True) is None,
                            "old container still exists")
        expand.verify_recovery_process_inventory({})
        instance = plan["old_instances"][role]
        rows = [row for row in current["generations"] if row["instance"] == instance]
        rollout.require(len(rows) == 1, "historical runtime identity missing")
        row = rows[0]
        rollout.require(row["artifact_digest"] == plan["containers"][role]["image"] and
                        row["ingress_state"] == "absent" and not row["lease_live"],
                        "historical runtime identity active or changed")
        proof = {"plan_digest": plan["plan_digest"], "role": role, "old_instance": instance,
                 "old_container": plan["containers"][role], "current_ingress_head": current["head"],
                 "withdrawal": checked_record(self.evidence, plan, "withdraw-all"),
                 "stop": checked_record(self.evidence, plan, "stop-" + role),
                 "removal": checked_record(self.evidence, plan, "remove-" + role)}
        return {"instance_id": instance, "artifact_digest": row["artifact_digest"],
                "manifest_digest": row["support_manifest_digest"], "compatibility_epoch": row["compatibility_epoch"],
                "expected_schema_head": row["schema_head"], "expected_revision": row["revision"],
                "expected_ingress_head": current["head"], "expected_environment": self.target["environment"],
                "expected_database_system_id": self.target["database_system_id"],
                "host_proof_sha256": "sha256:" + rollout.digest(rollout.canonical(proof))}

    def retire(self, action, role):
        document = self.retirement_document(role)
        current = self.ingress.owner_command(self.owner(), "inspect")
        row = next(row for row in current["generations"] if row["instance"] == document["instance_id"])
        # An unknown stop commit is read back through its exact retained request.
        name = "retirement-" + role + ".json"
        if row["state"] == "stopped":
            rollout.require(self.evidence.has(name), "stopped row has no retained request")
            expected = dict(self.evidence.read(name))
            expected["expected_revision"] += 1
            rollout.require(document == expected, "stopped row differs from retained request")
            return {"status": "stopped", "instance_id": row["instance"], "readback": True}
        if self.evidence.has(name):
            rollout.require(self.evidence.read(name) == document, "retirement predecessor changed")
        elif action == "apply":
            self.evidence.write(name, document)
        result = self.ingress.owner_command(self.owner(), "stop-" + action, document)
        rollout.require(result["instance_id"] == document["instance_id"] and
                        result["host_proof_sha256"] == document["host_proof_sha256"] and
                        result["status"] == ("eligible" if action == "check" else "stopped"),
                        "retirement response differs from exact proof")
        return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("plan", "prepare", "withdraw", "stop", "remove", "check", "apply"))
    parser.add_argument("--maintenance", action="store_true", help="Exact predecessor full withdrawal via the M126 or M127 Artifact CLI")
    parser.add_argument("--rollout-input", required=True, type=Path)
    parser.add_argument("--role", choices=("stable", "canary"))
    args = parser.parse_args()
    rollout.require(args.role is not None or (args.maintenance and args.action in ("plan", "prepare", "withdraw")),
                    "this action requires an exact role")
    value = rollout.document(args.rollout_input)
    operation = MaintenanceRetirement(value) if args.maintenance else rollout.Rollout(value)
    os.environ.clear()
    os.environ.update(rollout.ENV)
    if args.maintenance:
        # Read/check uses existing locks; carrier lifecycle is explicit even for reads.
        locks = rollout.operation_locks if args.action == "prepare" else existing_locks
        with locks(operation.target):
            if args.action == "plan":
                result = operation.plan()
            elif args.action == "prepare":
                result = operation.prepare()
            elif args.action in ("withdraw", "stop", "remove"):
                result = operation.stage(args.action, args.role)
            else:
                result = operation.retire(args.action, args.role)
            print(json.dumps(result, sort_keys=True))
        return
    rollout.require(args.action in ("check", "apply"), "rolling retirement supports only check/apply")
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
