#!/usr/bin/env python3
"""Fixed existing-target Compose rollout; uses the sole runtime-ingress writer.

No image build/pull, schema apply, model activation, automatic rollback or cleanup.
Plan/inspect are read-only. Operational completion and continuity are separate.
"""
import argparse
import base64
import contextlib
import copy
import datetime
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import socket
import stat
import subprocess
import sys
import threading
import time
from types import SimpleNamespace
from urllib.parse import urlsplit

sys.dont_write_bytecode = True  # Read-only commands must not create import caches.
ENV = {"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
       "DOCKER_HOST": "unix:///var/run/docker.sock", "PYTHONDONTWRITEBYTECODE": "1", "GIT_OPTIONAL_LOCKS": "0"}
LIFECYCLE_LOCK = Path("/run/lock/unirouter-docker-lifecycle.lock")
HELD_LOCK_FDS = []
SERVICES = {"stable": "backend", "canary": "backend-canary", "frontend": "frontend"}
CONTAINERS = {"stable": "uniroute-api", "canary": "uniroute-api-canary", "frontend": "uniroute-frontend"}
STAGES = ("withdraw-canary", "stop-canary", "create-canary", "rejoin-canary",
          "withdraw-stable", "stop-stable", "create-stable", "rejoin-stable",
          "create-frontend", "refresh-frontend", "verify")


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()


def digest(wire):
    return hashlib.sha256(wire).hexdigest()


def unique_pairs(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate JSON field")
        result[key] = value
    return result


def load(wire):
    return json.loads(wire, object_pairs_hook=unique_pairs)


def regular(path, directory=False):
    path = Path(path)
    require(path.is_absolute() and path == path.resolve(), "absolute non-aliased path required")
    for part in [path, *path.parents]:
        require(not part.is_symlink(), "symlink path refused")
    require(path.is_dir() if directory else path.is_file(), "required path is absent or wrong type")
    return path


def run(args, timeout=30, data=None, env=None):
    result = subprocess.run(args, input=data, capture_output=True, timeout=timeout, env=env or ENV, pass_fds=tuple(HELD_LOCK_FDS))
    require(result.returncode == 0, f"{Path(args[0]).name} failed with exit {result.returncode}")
    return result.stdout


def inspect(name, missing_ok=False):
    result = subprocess.run(["docker", "inspect", name], capture_output=True, timeout=30, env=ENV, pass_fds=tuple(HELD_LOCK_FDS))
    if missing_ok and result.returncode == 1 and result.stdout.strip() == b"[]":
        messages = {f"Error: No such object: {name}".encode(), f"Error response from daemon: No such container: {name}".encode()}
        if result.stderr.strip() in messages:
            return None
    require(result.returncode == 0, "Docker inspection failed; absence is unproven")
    rows = load(result.stdout)
    require(len(rows) == 1, "Docker identity is ambiguous")
    return rows[0]


def environment(values):
    return dict(item.split("=", 1) for item in values or [])


def check_source(source):
    directory = str(regular(source["directory"], True))
    for ref, field in (("HEAD", "commit"), ("HEAD^{tree}", "tree")):
        require(run(["git", "-C", directory, "rev-parse", ref]).decode().strip() == source[field], "source " + field + " changed")
    require(not run(["git", "-C", directory, "status", "--porcelain"]), "source is dirty")
    require(regular(Path(directory) / "VERSION").read_text().strip() == source["version"], "source version differs")


def seconds(value):
    if isinstance(value, (int, float)):
        return float(value)
    pieces = re.findall(r"([0-9]+(?:\.[0-9]+)?)(ns|us|ms|s|m|h)", value or "")
    require(pieces and "".join(n + u for n, u in pieces) == value, "unsupported duration")
    return sum(float(n) * {"ns": 1e-9, "us": 1e-6, "ms": .001, "s": 1, "m": 60, "h": 3600}[u] for n, u in pieces)


def check_service(service, container, image, project, runtime_environment=None):
    """Compare effective Compose with actual runtime without disclosing values."""
    config, host = container["Config"], container["HostConfig"]
    expected_env = environment(image["Config"].get("Env"))
    expected_env.update({key: str(value) for key, value in service.get("environment", {}).items() if value is not None})
    actual_env = environment(config.get("Env"))
    require(actual_env == expected_env, "actual container environment differs from effective configuration")
    if runtime_environment is not None:
        require(runtime_environment in ("development", "test") and
                actual_env.get("RUNTIME_ENVIRONMENT") == runtime_environment,
                "actual runtime is outside the authorized development/test environment")
    require(container["Image"] == image["Id"], "actual image differs from selected local image")
    for key, field in (("command", "Cmd"), ("entrypoint", "Entrypoint"), ("user", "User"), ("working_dir", "WorkingDir")):
        expected = service.get(key)
        if expected is None:
            expected = image["Config"].get(field)
        if expected is None and field in ("User", "WorkingDir"):
            expected = ""
        require(config.get(field) == expected, f"actual {key} differs from effective configuration")
    ports = {}
    for port in service.get("ports", []):
        key = f"{port['target']}/{port.get('protocol', 'tcp')}"
        ports.setdefault(key, []).append({"HostIp": port.get("host_ip", ""), "HostPort": str(port["published"])})
    require((host.get("PortBindings") or {}) == ports, "actual published ports differ")
    mounts = []
    for volume in service.get("volumes", []):
        require(volume["type"] == "bind", "this rollout supports explicit bind mounts only")
        mounts.append(("bind", volume["source"], volume["target"], not volume.get("read_only", False)))
    observed = [(v["Type"], v["Source"], v["Destination"], v["RW"]) for v in container.get("Mounts", [])]
    require(sorted(mounts) == sorted(observed), "actual mounts differ")
    for key, field, default in (("privileged", "Privileged", False), ("read_only", "ReadonlyRootfs", False),
                                ("init", "Init", None), ("cap_add", "CapAdd", None), ("cap_drop", "CapDrop", None),
                                ("security_opt", "SecurityOpt", None), ("devices", "Devices", []),
                                ("pids_limit", "PidsLimit", None), ("cpu_shares", "CpuShares", 0),
                                ("cpuset", "CpusetCpus", "")):
        actual = host.get(field)
        expected = service.get(key)
        require((default if actual is None else actual) == (default if expected is None else expected), f"actual {key} differs")
    for key, field in (("mem_limit", "Memory"), ("mem_reservation", "MemoryReservation"), ("memswap_limit", "MemorySwap")):
        require(host.get(field, 0) == service.get(key, 0), f"actual {key} differs")
    require(host.get("NanoCpus", 0) == int(float(service.get("cpus", 0)) * 1e9), "actual CPU limit differs")
    require(not host.get("AutoRemove", False), "automatic container removal is unsupported")
    require(host["RestartPolicy"]["Name"] == service.get("restart", "no"), "actual restart policy differs")
    logging = service.get("logging", {})
    require(host["LogConfig"] == {"Type": logging.get("driver", "json-file"), "Config": logging.get("options", {})}, "actual logging differs")
    health = copy.deepcopy(image["Config"].get("Healthcheck", {}))
    for key, value in service.get("healthcheck", {}).items():
        field = {"test": "Test", "interval": "Interval", "timeout": "Timeout", "start_period": "StartPeriod",
                 "start_interval": "StartInterval", "retries": "Retries", "disable": "Disable"}.get(key)
        require(field is not None and key != "disable", "unsupported health setting")
        health[field] = round(seconds(value) * 1e9) if key in ("interval", "timeout", "start_period", "start_interval") else value
    require(config.get("Healthcheck", {}) == health, "actual healthcheck differs")
    if "stop_grace_period" in service:
        require(config.get("StopTimeout") == seconds(service["stop_grace_period"]), "actual stop grace differs")
    networks = service.get("networks", {"default": None})
    require(set(container["NetworkSettings"]["Networks"]) == {project + "_" + n for n in networks}, "actual networks differ")
    for name, options in networks.items():
        actual = container["NetworkSettings"]["Networks"][project + "_" + name]
        require((actual.get("IPAMConfig") or {}).get("IPv4Address") == (options or {}).get("ipv4_address"), "actual static network assignment differs")


def document(path):
    value = load(regular(path).read_bytes())
    require(set(value) == {"format", "operation_id", "target", "source", "images", "evidence_directory"}, "unknown rollout input fields")
    require(value["format"] == "unirouter.internal-target-rollout.v1", "unsupported rollout input")
    require(re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9._-]{0,100}", value["operation_id"]), "invalid operation identity")
    target, source = value["target"], value["source"]
    require(set(target) == {"hostname", "database_system_id", "environment", "project", "env_file", "overlay", "tls_overlay", "ingress_directory", "nginx_container_id", "public_url"}, "unknown Target fields")
    require(target["environment"] in ("development", "test") and re.fullmatch(r"[a-z0-9_-]+", target["project"]), "unsupported Target environment/project")
    require(set(source) == {"directory", "commit", "tree", "version"}, "unknown source fields")
    require(all(re.fullmatch(r"[a-f0-9]{40}", source[k]) for k in ("commit", "tree")) and re.fullmatch(r"\d+\.\d+\.\d+", source["version"]), "invalid source identity")
    require(re.fullmatch(r"[a-f0-9]{64}", target["nginx_container_id"]), "exact Nginx container required")
    require(re.fullmatch(r"[0-9]+", target["database_system_id"]), "exact database identity required")
    url = urlsplit(target["public_url"])
    require(url.scheme == "https" and url.hostname and not url.username and not url.password and not url.query and not url.fragment and url.path in ("", "/"), "one credential-free HTTPS origin required")
    require(set(value["images"]) == {"backend", "frontend"}, "both component images required")
    for image in value["images"].values():
        require(set(image) == {"tag", "digest"} and re.fullmatch(r"[a-zA-Z0-9./:_-]+", image["tag"]) and re.fullmatch(r"sha256:[a-f0-9]{64}", image["digest"]), "invalid immutable local image")
    for key in ("env_file", "overlay", "tls_overlay"):
        regular(target[key])
    for key in ("ingress_directory",):
        regular(target[key], True)
    regular(source["directory"], True)
    evidence = Path(value["evidence_directory"])
    regular(evidence.parent, True)
    require(evidence.is_absolute() and evidence == evidence.resolve() and not evidence.is_symlink(), "invalid evidence path")
    require(not evidence.exists() or evidence.is_dir(), "evidence path is not a directory")
    require(all(evidence != Path(p) and evidence not in Path(p).parents and Path(p) not in evidence.parents
                for p in (target["ingress_directory"], source["directory"])), "evidence overlaps managed source/carrier")
    return value


class Evidence:
    def __init__(self, directory):
        self.root = Path(directory)

    def write(self, name, value):
        wire = value if isinstance(value, bytes) else canonical(value)
        fd = os.open(self.root / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        try:
            with os.fdopen(fd, "wb") as file:
                file.write(wire)
                file.flush()
                os.fsync(file.fileno())
            fd = os.open(self.root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
            try:
                os.fsync(fd)
            finally:
                os.close(fd)
        except BaseException:
            raise

    def read(self, name):
        return load(regular(self.root / name).read_bytes())

    def has(self, name):
        p = self.root / name
        require(not p.is_symlink(), "evidence symlink refused")
        return p.is_file()


class Rollout:
    def __init__(self, spec):
        self.spec, self.target, self.source = spec, spec["target"], spec["source"]
        self.evidence = Evidence(spec["evidence_directory"])
        self.new_source = Path(self.source["directory"])
        module = importlib.util.spec_from_file_location("runtime_ingress", Path(__file__).with_name("runtime-ingress.py"))
        self.ingress = importlib.util.module_from_spec(module)
        module.loader.exec_module(self.ingress)
        self.ingress.LOCK_FDS = HELD_LOCK_FDS

    def sql(self, query):
        return run(["docker", "exec", "uniroute-postgres", "psql", "-X", "-U", "uniroute", "-d", "uniroute", "-At", "-v", "ON_ERROR_STOP=1", "-c", query]).decode().strip()

    def identity(self):
        require(socket.gethostname() == self.target["hostname"], "Target hostname changed")
        require(self.sql("SELECT system_identifier FROM pg_control_system()") == self.target["database_system_id"], "Target database changed")
        require(inspect("uniroute-nginx")["Id"] == self.target["nginx_container_id"], "Nginx generation changed")

    def chain(self, source, overlay=None):
        return [str(source / "docker-compose.yml"), str(source / "docker-compose.canary.yml"),
                str(source / "deploy/internal-target-images.override.yml"), self.target["tls_overlay"], overlay or self.target["overlay"]]

    def compose(self, chain):
        args = ["docker", "compose", "--project-name", self.target["project"], "--env-file", self.target["env_file"]]
        for path in chain:
            args += ["-f", path]
        return args

    def images(self):
        result = {}
        for kind, desired in self.spec["images"].items():
            image = load(run(["docker", "image", "inspect", desired["tag"]]))[0]
            labels = image["Config"]["Labels"]
            require(image["Id"] == desired["digest"] and labels.get("org.opencontainers.image.revision") == self.source["commit"] and
                    labels.get("org.opencontainers.image.version") == self.source["version"] and labels.get("io.unirouter.component") == kind,
                    "immutable local component image changed or absent")
            result[kind] = image
        return result

    def compose_env(self, tags):
        return dict(ENV, UNIROUTER_BACKEND_IMAGE=tags["backend"], UNIROUTER_FRONTEND_IMAGE=tags["frontend"])

    def config(self, chain, tags, overlay=None):
        return load(run(self.compose(chain) + ["config", "--format", "json"], data=overlay, env=self.compose_env(tags)))

    def pins(self):
        return {name: digest(self.sql("SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY " + key + "),'[]'::jsonb) FROM " + table + " t").encode())
                for name, table, key in (("schema", "schema_migrations", "version"), ("releases", "runtime_change_releases", "id"),
                                         ("slots", "runtime_change_slots", "public_model_id"), ("publications", "capability_publications", "sequence"))}

    def ingress_args(self, desired, change_id=None):
        return SimpleNamespace(directory=Path(self.target["ingress_directory"]), container="uniroute-nginx",
                               expected_container_id=self.target["nginx_container_id"], desired=desired,
                               change_id=change_id, wait_seconds=2100)

    def ingress_static_digest(self):
        membership = regular(Path(self.target["ingress_directory"]) / "upstreams.conf").read_bytes()
        require(membership in [self.ingress.membership(s) for s in ("both", "stable", "canary")], "unknown installed membership")
        config = self.ingress.docker_exec("uniroute-nginx", "nginx", "-T")
        self.ingress.check_configuration(config)
        require(config.count(membership) == 1, "managed membership occurrence is ambiguous")
        return digest(config.replace(membership, b"MANAGED_MEMBERSHIP"))

    def sources(self, plan=None):
        check_source(self.source)
        # The reviewed implementation and its existing ingress owner come from
        # this exact source, including on resume. A copied wrapper is not an
        # alternate executable carrier.
        require(Path(__file__).resolve() == self.new_source / "scripts/internal-target-rollout.py", "rollout tool is outside the exact source")
        if plan is not None:
            check_source(plan["old_source_identity"])

    def plan(self):
        self.identity()
        self.images()  # Absence is rejected before storage, backup or stop.
        self.sources()
        containers = {role: inspect(name) for role, name in CONTAINERS.items()}
        old_chain = containers["stable"]["Config"]["Labels"]["com.docker.compose.project.config_files"].split(",")
        old_source = regular(Path(old_chain[0]).parent, True)
        require(old_chain == self.chain(old_source), "existing Compose chain is outside the five-file contract")
        old_runtime = environment(containers["stable"]["Config"]["Env"])
        old_identity = {"directory": str(old_source), "commit": old_runtime["RUNTIME_COMMIT"],
                        "tree": old_runtime["RUNTIME_TREE"], "version": old_runtime["RUNTIME_VERSION"]}
        check_source(old_identity)
        old_tags = {"backend": containers["stable"]["Config"]["Image"], "frontend": containers["frontend"]["Config"]["Image"]}
        old_config = self.config(old_chain, old_tags)
        for role, container in containers.items():
            require(container["State"]["Running"] and container["Config"]["Labels"]["com.docker.compose.project.config_files"].split(",") == old_chain, "running component chain differs")
            image = load(run(["docker", "image", "inspect", container["Image"]]))[0]
            labels = image["Config"].get("Labels", {})
            require(labels.get("org.opencontainers.image.revision") == old_identity["commit"] and
                    labels.get("org.opencontainers.image.version") == old_identity["version"] and
                    labels.get("io.unirouter.component") == ("frontend" if role == "frontend" else "backend"),
                    "old component Artifact differs from its source")
            if role != "frontend":
                actual_runtime = environment(container["Config"]["Env"])
                require(all(actual_runtime.get(key) == old_runtime[key] for key in ("RUNTIME_VERSION", "RUNTIME_COMMIT", "RUNTIME_TREE", "RUNTIME_IMAGE_DIGEST")) and
                        actual_runtime["RUNTIME_IMAGE_DIGEST"] == image["Id"], "old runtime metadata differs from actual Artifact")
            check_service(old_config["services"][SERVICES[role]], container, image, self.target["project"], None if role == "frontend" else self.target["environment"])
        old_overlay = regular(self.target["overlay"]).read_bytes()
        overlay = old_overlay
        replacements = {"RUNTIME_VERSION": self.source["version"], "RUNTIME_COMMIT": self.source["commit"],
                        "RUNTIME_TREE": self.source["tree"], "RUNTIME_IMAGE_DIGEST": self.spec["images"]["backend"]["digest"]}
        for key, value in replacements.items():
            before = old_runtime[key].encode()
            require(before and overlay.count(before) == 1, "runtime metadata occurrence is ambiguous")
            overlay = overlay.replace(before, value.encode())
        new_tags = {k: v["tag"] for k, v in self.spec["images"].items()}
        new_config = self.config(self.chain(self.new_source, "-"), new_tags, overlay)
        expected = copy.deepcopy(old_config)
        for role, service in SERVICES.items():
            expected["services"][service]["image"] = new_tags["frontend" if role == "frontend" else "backend"]
            if role != "frontend":
                expected["services"][service]["environment"].update(replacements)
        for service, target, relative in (("backend", "/app/migrations", "migrations"), ("backend-canary", "/app/migrations", "migrations"),
                                          ("prometheus", "/etc/prometheus/prometheus.yml", "infra/prometheus/prometheus.yml"),
                                          ("grafana", "/etc/grafana/provisioning", "infra/grafana/provisioning"),
                                          ("grafana", "/var/lib/grafana/dashboards", "infra/grafana/dashboards")):
            require(run(["git", "-C", str(old_source), "rev-parse", "HEAD:" + relative]) == run(["git", "-C", str(self.new_source), "rev-parse", "HEAD:" + relative]), "bound schema/monitoring source differs")
            if service in expected["services"]:
                matches = [v for v in expected["services"][service].get("volumes", []) if v["target"] == target]
                require(len(matches) == 1 and matches[0]["source"] == str(old_source / relative), "unexpected source mount")
                matches[0]["source"] = str(self.new_source / relative)
        require(new_config == expected, "effective configuration delta exceeds Artifact-only scope")
        before = self.ingress.snapshot(self.ingress_args("both"))
        require(before["before_state"] == "both", "initial rollout requires both healthy receivers")
        ingress_state = self.ingress.owner_command("uniroute-api", "inspect")
        require(ingress_state["head_status"] == "applied", "initial ingress head is not applied")
        plan = {"format": "unirouter.internal-target-plan.v1", "input_digest": digest(canonical(self.spec)),
                "containers": {role: {"id": c["Id"], "image": c["Image"]} for role, c in containers.items()},
                "old_chain": old_chain, "old_tags": old_tags, "old_source": str(old_source), "old_source_identity": old_identity,
                "old_config_digest": digest(canonical(old_config)), "new_config_digest": digest(canonical(new_config)),
                "old_overlay_digest": digest(old_overlay), "new_overlay_digest": digest(overlay),
                "pins": self.pins(), "nginx_digest": before["configuration_sha256"],
                "nginx_static_digest": self.ingress_static_digest(), "initial_ingress_head": ingress_state["head"],
                "old_instances": {role: h["instance"] for role, h in before["health"].items()}}
        plan["plan_digest"] = digest(canonical(plan))
        return plan, old_overlay, overlay

    def storage(self):
        run([str(self.new_source / "scripts/docker-storage-preflight.sh"), "--path", "/var/lib/docker",
             "--minimum-free-gib", "8", "--maximum-use-percent", "80"])

    def prepare(self):
        require(not self.evidence.root.exists(), "existing operation evidence; inspect/resume it")
        plan, old_overlay, overlay = self.plan()
        self.storage()
        self.evidence.root.mkdir(mode=0o700)
        for name, value in (("input.json", self.spec), ("plan.json", plan),
                            ("old.runtime.yml", old_overlay), ("new.runtime.yml", overlay)):
            self.evidence.write(name, value)
        backup = self.evidence.root / "before.dump"
        with backup.open("xb") as file:
            os.fchmod(file.fileno(), 0o600)
            result = subprocess.run(["docker", "exec", "uniroute-postgres", "pg_dump", "-U", "uniroute", "-Fc", "uniroute"],
                                    stdout=file, stderr=subprocess.DEVNULL, timeout=180, env=ENV)
            require(result.returncode == 0, "database backup failed; no runtime mutation")
            file.flush()
            os.fsync(file.fileno())
        with backup.open("rb") as file:
            result = subprocess.run(["docker", "exec", "-i", "uniroute-postgres", "pg_restore", "--list"],
                                    stdin=file, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=60, env=ENV)
            require(result.returncode == 0, "backup verification failed; no runtime mutation")
        self.evidence.write("prepared.json", {"plan_digest": plan["plan_digest"], "backup_digest": digest(backup.read_bytes())})
        return {"deployment": "prepared", "runtime_changed": False, "plan_digest": plan["plan_digest"]}

    def retained_plan(self):
        require(self.evidence.read("input.json") == self.spec, "operation input changed")
        plan = self.evidence.read("plan.json")
        checked = dict(plan)
        expected_digest = checked.pop("plan_digest")
        require(digest(canonical(checked)) == expected_digest and plan["input_digest"] == digest(canonical(self.spec)), "retained plan digest differs")
        prepared = self.evidence.read("prepared.json")
        require(prepared["plan_digest"] == expected_digest, "preparation does not bind this plan")
        for name, field in (("old.runtime.yml", "old_overlay_digest"), ("new.runtime.yml", "new_overlay_digest")):
            require(digest(regular(self.evidence.root / name).read_bytes()) == plan[field], "retained runtime bytes changed")
        return plan

    def config_for(self, plan, new):
        if new:
            chain = self.chain(self.new_source, str(self.evidence.root / "new.runtime.yml"))
            tags = {k: v["tag"] for k, v in self.spec["images"].items()}
        else:
            chain = self.chain(Path(plan["old_source"]), str(self.evidence.root / "old.runtime.yml"))
            tags = plan["old_tags"]
        rendered = self.config(chain, tags)
        require(digest(canonical(rendered)) == plan["new_config_digest" if new else "old_config_digest"], "prepared effective configuration drifted")
        return rendered

    def progress(self):
        self.identity()
        plan = self.retained_plan()
        self.sources(plan)
        self.images()
        for name in ["apply-started"] + [prefix + stage for stage in STAGES for prefix in ("start-", "done-", "command-")]:
            if self.evidence.has(name + ".json"):
                require(self.evidence.read(name + ".json")["plan_digest"] == plan["plan_digest"], "operation evidence belongs to a different plan")
        completed = []
        for stage in STAGES:
            if not self.evidence.has("done-" + stage + ".json"):
                break
            require(self.evidence.has("start-" + stage + ".json"), "stage result has no start record")
            completed.append(stage)
        require(not any(self.evidence.has(prefix + s + ".json") for s in STAGES[len(completed) + 1:] for prefix in ("start-", "done-", "command-")), "stage evidence is out of order")
        require(self.pins() == plan["pins"], "schema/Publication/Release/Slot changed; fresh scoped plan required")
        current_overlay = digest(regular(self.target["overlay"]).read_bytes())
        allowed_overlays = {plan["new_overlay_digest"]} if self.evidence.has("start-" + STAGES[0] + ".json") else {plan["old_overlay_digest"], plan["new_overlay_digest"]}
        if not self.evidence.has("apply-started.json"):
            require(not completed and not self.evidence.has("start-" + STAGES[0] + ".json"), "stage evidence predates apply")
            allowed_overlays = {plan["old_overlay_digest"]}
        require(current_overlay in allowed_overlays, "runtime overlay differs from current stage")
        require(self.ingress_static_digest() == plan["nginx_static_digest"], "unmanaged ingress configuration changed")
        self.check_ingress_stage(plan, completed)
        old_config, new_config = self.config_for(plan, False), self.config_for(plan, True)
        actual = {}
        for role, name in CONTAINERS.items():
            c = inspect(name, missing_ok=True)
            if c is None:
                self.require_interrupted_create(role, plan)
                actual[role] = {"absent_after_create_start": True}
                continue
            old = c["Id"] == plan["containers"][role]["id"]
            desired = self.spec["images"]["frontend" if role == "frontend" else "backend"]["digest"]
            if old:
                require(c["Image"] == plan["containers"][role]["image"], "old component image changed")
                config, chain = old_config, plan["old_chain"]
            else:
                require(self.evidence.has("start-create-" + role + ".json") and c["Image"] == desired, "unrecognized replacement container")
                config, chain = new_config, self.chain(self.new_source)
            require(c["Config"]["Labels"]["com.docker.compose.project.config_files"].split(",") == chain, "component source chain drifted")
            image = load(run(["docker", "image", "inspect", c["Image"]]))[0]
            check_service(config["services"][SERVICES[role]], c, image, self.target["project"], None if role == "frontend" else self.target["environment"])
            if self.evidence.has("done-create-" + role + ".json"):
                result = self.evidence.read("done-create-" + role + ".json")["result"]
                require(result["container_id"] == c["Id"], "verified replacement container changed")
                require(c["State"]["Running"], "verified replacement container stopped externally")
                if role != "frontend":
                    require(self.ready(role, desired)["instance"] == result["instance"], "verified replacement process changed")
            elif old and self.evidence.has("done-stop-" + role + ".json"):
                require(not c["State"]["Running"], "stopped old container was restarted externally")
            actual[role] = {"id": c["Id"], "old": old, "running": c["State"]["Running"], "image": c["Image"]}
        return plan, {"deployment": "complete" if len(completed) == len(STAGES) else "incomplete",
                      "next_stage": STAGES[len(completed)] if len(completed) < len(STAGES) else None,
                      "components": actual, "observer_acceptance": observation_acceptance(self.evidence)}

    def check_ingress_stage(self, plan, completed):
        def is_switch(stage):
            return stage.startswith(("withdraw-", "rejoin-", "refresh-"))

        def expected(stage):
            desired = "stable" if stage == "withdraw-canary" else "canary" if stage == "withdraw-stable" else "both"
            members = {r: {"instance": plan["old_instances"][r], "artifact_digest": plan["containers"][r]["image"]} for r in ("stable", "canary")}
            for earlier in STAGES[:STAGES.index(stage)] if stage else ():
                if earlier in ("create-canary", "create-stable"):
                    role = earlier.removeprefix("create-")
                    record = self.evidence.read("done-" + earlier + ".json")["result"]
                    members[role] = {"instance": record["instance"], "artifact_digest": record["image"]}
            selected = sorted((members[r] for r in (("stable", "canary") if desired == "both" else (desired,))), key=lambda m: m["instance"])
            return self.ingress.membership(desired), selected

        last = next((s for s in reversed(completed) if is_switch(s)), None)
        next_stage = STAGES[len(completed)] if len(completed) < len(STAGES) else None
        pending = next_stage if next_stage and is_switch(next_stage) and self.evidence.has("start-" + next_stage + ".json") else None
        owner = self.owner()
        state = self.ingress.owner_command(owner, "inspect")
        last_id = self.change_id(last) if last else plan["initial_ingress_head"]
        recovering = pending is not None and state["head"] == self.change_id(pending)
        require(recovering or state["head"] == last_id, "ingress head differs from current stage")
        require(state["head_status"] in (("pending", "ambiguous", "applied") if recovering else ("applied",)), "ingress status differs from current stage")
        retained = self.ingress.owner_command(owner, "read", {"change_id": state["head"]})
        receipt = retained["plan"]
        require(retained["status"] == state["head_status"] and receipt["change_id"] == state["head"], "ingress receipt identity differs")
        stage = pending if recovering else last
        after, members = expected(stage)
        require(receipt["desired_config_digest"] == digest(after) and base64.b64decode(receipt["desired_config_bytes"], validate=True) == after and
                receipt["membership_after"] == members, "ingress receipt differs from exact stage membership")
        installed = regular(Path(self.target["ingress_directory"]) / "upstreams.conf").read_bytes()
        require(self.ingress.docker_exec("uniroute-nginx", "cat", self.ingress.MOUNT + "/upstreams.conf") == installed, "proxy-visible membership drifted")
        eligible_bytes, eligible_members = {after}, members
        if stage:
            earlier = STAGES[:STAGES.index(stage)]
            predecessor = next((s for s in reversed(earlier) if is_switch(s)), None)
            before, previous_members = expected(predecessor)
            previous_id = self.change_id(predecessor) if predecessor else plan["initial_ingress_head"]
            require(receipt["predecessor_id"] == previous_id and receipt["expected_config_digest"] == digest(before) and
                    receipt["membership_before"] == previous_members, "ingress stage predecessor differs")
            if retained["status"] != "applied":
                eligible_bytes.add(before)
                eligible_members = sorted({m["instance"]: m for m in members + previous_members}.values(), key=lambda m: m["instance"])
        require(installed in eligible_bytes, "installed membership differs from current ingress stage")
        active = sorted(({"instance": g["instance"], "artifact_digest": g["artifact_digest"]} for g in state["generations"] if g["ingress_state"] != "absent"), key=lambda m: m["instance"])
        require(active == eligible_members, "database receivers differ from current ingress stage")

    def require_interrupted_create(self, role, plan):
        require(self.evidence.has("start-create-" + role + ".json") and self.evidence.has("command-create-" + role + ".json") and
                not self.evidence.has("done-create-" + role + ".json"), "missing container is outside an interrupted create")
        require(role == "frontend" or self.evidence.has("done-stop-" + role + ".json"), "old backend stop was not verified")
        old = inspect(plan["containers"][role]["id"], missing_ok=True)
        require(old is None or not old["State"]["Running"], "old component is still running under another name")

    def owner(self):
        for name in ("uniroute-api", "uniroute-api-canary"):
            try:
                if inspect(name)["State"]["Running"]:
                    return name
            except RuntimeError:
                pass
        raise RuntimeError("no running database owner remains")

    def change_id(self, stage):
        return self.spec["operation_id"] + "-" + stage

    def prove_withdrawal(self, role):
        stage = "withdraw-" + role
        record = self.evidence.read("done-" + stage + ".json")
        state = self.ingress.owner_command(self.owner(), "inspect")
        require(state["head"] == self.change_id(stage) and state["head_status"] == "applied", "withdrawal is no longer the applied head")
        saved = self.ingress.owner_command(self.owner(), "read", {"change_id": self.change_id(stage)})
        require(saved["status"] == "applied" and saved["plan"]["desired_config_digest"] == record["result"]["after_sha256"], "withdrawal receipt differs")
        require(digest(regular(Path(self.target["ingress_directory"]) / "upstreams.conf").read_bytes()) == saved["plan"]["desired_config_digest"], "actual membership changed after withdrawal")
        removed = self.retained_plan()["old_instances"][role]
        require(all(m["instance"] != removed for m in saved["plan"]["membership_after"]), "old generation still receives traffic")

    def switch(self, stage, desired):
        args = self.ingress_args(desired, self.change_id(stage))
        before = self.ingress.snapshot(args)
        args.expected_config_sha256 = digest(before["before"])
        args.expected_nginx_sha256 = before["configuration_sha256"]
        outcome = self.ingress.coordinated_apply(args, before)
        require(outcome in ("old_proxy_workers_exited", "already_applied"), "withdrawal remains unproven")
        return {"outcome": outcome, "change_id": args.change_id, "after_sha256": digest(before["after"])}

    def stop(self, role, plan):
        self.prove_withdrawal(role)
        survivor = "stable" if role == "canary" else "canary"
        self.ready(survivor, None)
        old = inspect(CONTAINERS[role])
        require(old["Id"] == plan["containers"][role]["id"], "refuse stopping an unrecognized generation")
        grace = old["Config"].get("StopTimeout")
        require(isinstance(grace, int) and 0 < grace <= 90000, "missing bounded Compose stop grace")
        if old["State"]["Running"]:
            self.begin_command("stop-" + role, plan)
            run(["docker", "stop", "--time", str(grace), old["Id"]], timeout=grace + 40)
        stopped = inspect(old["Id"])
        started = self.evidence.read("start-stop-" + role + ".json")["at"]
        result = subprocess.run(["docker", "logs", "--since", started, old["Id"]], capture_output=True, timeout=15, env=ENV)
        require(result.returncode == 0, "shutdown logs unavailable; retain stopped container")
        logs = (result.stdout + result.stderr).decode(errors="replace")
        events = [line for line in logs.splitlines() if line.endswith("Shutting down server...") or line.endswith("Server exited")]
        incomplete = any(text in logs for text in ("HTTP runtime drain incomplete:", "did not stop before shutdown", "did not flush before shutdown"))
        require(not stopped["State"]["Running"] and stopped["State"]["ExitCode"] == 0 and not stopped["State"]["OOMKilled"] and
                not incomplete and any(line.endswith("Server exited") for line in events), "clean drain/stop is unproven")
        return {"container_id": old["Id"], "exit": 0, "shutdown_events": events, "incomplete": False}

    def ready(self, role, expected_image):
        port = "8080" if role == "stable" else "8081"
        runtime = load(run(["curl", "--noproxy", "*", "-fsS", "--max-time", "3", "http://127.0.0.1:" + port + "/health/ready"], timeout=5))["runtime"]
        require(runtime["state"] == "serving" and runtime["lease_remaining_seconds"] > 5, "surviving generation is not ready")
        if expected_image:
            require(runtime["artifact_digest"] == expected_image, "runtime Artifact differs from desired image")
        return runtime

    def create(self, role, plan):
        if role != "frontend":
            self.prove_withdrawal(role)
        current = inspect(CONTAINERS[role], missing_ok=True)
        kind = "frontend" if role == "frontend" else "backend"
        desired = self.spec["images"][kind]["digest"]
        images = self.images()  # Immediately before the only create command.
        config = self.config_for(plan, True)
        if current is None:
            self.require_interrupted_create(role, plan)
        if current is None or current["Id"] == plan["containers"][role]["id"]:
            require(current is None or role == "frontend" or not current["State"]["Running"], "old backend has not stopped")
            tags = {k: v["tag"] for k, v in self.spec["images"].items()}
            self.begin_command("create-" + role, plan)
            run(self.compose(self.chain(self.new_source)) + ["up", "-d", "--no-deps", "--no-build", "--pull", "never", "--force-recreate", SERVICES[role]],
                timeout=120, env=self.compose_env(tags))
        # A lost create response is read back; an already desired container is
        # never recreated. Full runtime configuration must match this plan.
        deadline = time.monotonic() + 120
        while True:
            current = inspect(CONTAINERS[role])
            require(current["Id"] != plan["containers"][role]["id"] and current["Image"] == desired, "replacement identity is ambiguous")
            check_service(config["services"][SERVICES[role]], current, images[kind], self.target["project"], None if role == "frontend" else self.target["environment"])
            try:
                if role == "frontend":
                    wire = run(["docker", "exec", current["Id"], "wget", "-T", "3", "-qO-", "http://127.0.0.1/"], timeout=5)
                    require(b"<!doctype html>" in wire.lower(), "frontend index is not ready")
                    return {"container_id": current["Id"], "image": desired, "index_sha256": digest(wire)}
                runtime = self.ready(role, desired)
                require(runtime["instance"] != plan["old_instances"][role], "new process reused old instance")
                return {"container_id": current["Id"], "image": desired, "instance": runtime["instance"]}
            except (RuntimeError, subprocess.TimeoutExpired):
                require(time.monotonic() < deadline, "new generation readiness deadline; keep surviving ingress")
                time.sleep(1)

    def begin_command(self, stage, plan):
        # A killed CLI does not cancel the Docker daemon's operation. Old or
        # absent state alone cannot prove an earlier invocation finished. Only
        # exact post-state readback may close a lost stop/create response.
        name = "command-" + stage + ".json"
        require(not self.evidence.has(name), "Docker command remains unconfirmed; refuse replay and inspect daemon/post-state")
        self.evidence.write(name, {"at": utcnow(), "plan_digest": plan["plan_digest"]})

    def verify(self, plan):
        require(self.pins() == plan["pins"], "protected publication/execution pins changed")
        desired = self.spec["images"]["backend"]["digest"]
        for role in ("stable", "canary"):
            self.ready(role, desired)
        inside = run(["docker", "exec", "uniroute-frontend", "wget", "-T", "3", "-qO-", "http://127.0.0.1/"])
        entry = run(["curl", "--noproxy", "*", "-fsS", "--max-time", "10", self.target["public_url"].rstrip("/") + "/"])
        require(entry == inside and b"<!doctype html>" in entry.lower(), "public frontend differs from desired container")
        return {"entry_sha256": digest(entry), "pins_unchanged": True}

    def apply(self, resume):
        plan, progress = self.progress()
        if progress["deployment"] == "complete":
            return progress
        if not resume:
            require(not self.evidence.has("apply-started.json"), "apply already began; inspect/resume exact operation")
            fresh, _, _ = self.plan()
            require(fresh == plan, "prepared pre-state changed")
            require(digest(regular(self.evidence.root / "before.dump").read_bytes()) == self.evidence.read("prepared.json")["backup_digest"], "backup bytes changed")
        else:
            require(self.evidence.has("apply-started.json"), "operation has not started; use apply")
        self.storage()
        observer = Observer(self.evidence, self.target["public_url"], self.target["nginx_container_id"])
        observer.start()
        try:
            if not self.evidence.has("apply-started.json"):
                self.evidence.write("apply-started.json", {"at": utcnow(), "plan_digest": plan["plan_digest"]})
            overlay = regular(self.target["overlay"])
            if digest(overlay.read_bytes()) == plan["old_overlay_digest"]:
                self.ingress.replace(overlay, regular(self.evidence.root / "new.runtime.yml").read_bytes(), stat.S_IMODE(overlay.stat().st_mode))
            require(digest(overlay.read_bytes()) == plan["new_overlay_digest"], "runtime overlay changed before operation")
            for stage in STAGES:
                if self.evidence.has("done-" + stage + ".json"):
                    continue
                if not self.evidence.has("start-" + stage + ".json"):
                    self.evidence.write("start-" + stage + ".json", {"at": utcnow(), "plan_digest": plan["plan_digest"]})
                if stage.startswith("withdraw-"):
                    result = self.switch(stage, "stable" if stage.endswith("canary") else "canary")
                elif stage.startswith("rejoin-") or stage == "refresh-frontend":
                    result = self.switch(stage, "both")
                elif stage.startswith("stop-"):
                    result = self.stop(stage.removeprefix("stop-"), plan)
                elif stage.startswith("create-"):
                    result = self.create(stage.removeprefix("create-"), plan)
                else:
                    result = self.verify(plan)
                self.evidence.write("done-" + stage + ".json", {"plan_digest": plan["plan_digest"], "at": utcnow(), "result": result})
        except BaseException as error:
            observer.failure(type(error).__name__)
            raise
        finally:
            observer.stop()
        return self.progress()[1]


def utcnow():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


OBSERVER_CONTRACT = "unirouter.ingress-observer.v2"
OBSERVER_BODY_LIMIT = 64 * 1024
LEGACY_SAMPLE_FIELDS = {"at", "completed_at", "protocol", "exit", "result", "ok"}
OBSERVER_SAMPLE_FIELDS = LEGACY_SAMPLE_FIELDS | {"client_digest", "body_bytes", "body_sha256", "json_error", "num_connects"}


def qualified_observer_client(client):
    require(set(client) == {"container_id", "image_digest", "curl_version", "vantage"}, "invalid observer client")
    require(re.fullmatch(r"[0-9a-f]{64}", client["container_id"]) and
            re.fullmatch(r"sha256:[0-9a-f]{64}", client["image_digest"]), "invalid observer client identity")
    require(client["vantage"] == "pinned_ingress_container_public_https", "unknown observer vantage")
    version = client["curl_version"]
    match = re.match(r"curl ([0-9]+)\.([0-9]+)\.([0-9]+) ", version)
    require(match and tuple(map(int, match.groups())) >= (8, 19, 0), "observer requires qualified curl 8.19 or newer")
    features = next((line.split()[1:] for line in version.splitlines() if line.startswith("Features:")), [])
    protocols = next((line.split()[1:] for line in version.splitlines() if line.startswith("Protocols:")), [])
    require("HTTP2" in features and "https" in protocols, "observer client lacks HTTPS/HTTP2")


def observer_sample_ok(row):
    return (type(row["exit"]) is int and row["exit"] == 0 and
            row["result"] == ("401 2" if row["protocol"] == "--http2" else "401 1.1") and
            type(row["body_bytes"]) is int and 0 < row["body_bytes"] <= OBSERVER_BODY_LIMIT and
            row["json_error"] is True and type(row["num_connects"]) is int and row["num_connects"] >= 1)


class Observer:
    def __init__(self, evidence, origin, container_id):
        self.evidence, self.origin = evidence, origin.rstrip("/")
        require(re.fullmatch(r"[0-9a-f]{64}", container_id), "observer requires exact ingress container")
        parsed = urlsplit(self.origin)
        require(parsed.scheme == "https" and parsed.hostname and not parsed.username and not parsed.password and
                parsed.path == "" and not parsed.query and not parsed.fragment, "observer requires credential-free HTTPS origin")
        self.container_id, self.client = container_id, None
        index = 1
        while evidence.has(f"observer-{index}-start.json"):
            index += 1
        self.name = f"observer-{index}"
        self.stop_event, self.first = threading.Event(), threading.Event()
        self.count, self.failed, self.error = 0, False, None
        self.thread = threading.Thread(target=self.observe)

    def read_client(self):
        container = inspect(self.container_id)
        require(container["Id"] == self.container_id and container["State"]["Running"], "observer ingress identity unavailable")
        version = run(["docker", "exec", self.container_id, "curl", "-q", "-V"], timeout=7).decode().strip()
        client = {"container_id": self.container_id, "image_digest": container["Image"],
                  "curl_version": version, "vantage": "pinned_ingress_container_public_https"}
        qualified_observer_client(client)
        return client

    def start(self):
        # Runs before overlay replacement, ingress mutation or container stop.
        # No fallback to an unqualified host client or new tool installation.
        self.client = self.read_client()
        self.evidence.write(self.name + "-start.json", {"at": utcnow(), "contract_version": OBSERVER_CONTRACT,
                                                       "origin": self.origin, "client": self.client})
        self.thread.start()
        if not self.first.wait(15) or self.error or self.failed:
            self.stop()
            raise RuntimeError("observer did not establish initial coverage")

    def sample(self, protocol):
        require(protocol in ("--http1.1", "--http2"), "unknown observer protocol")
        started = utcnow()
        wire_protocol = "--http2-prior-knowledge" if protocol == "--http2" else protocol
        result = subprocess.run(["docker", "exec", self.container_id, "curl", "-q", "--noproxy", "*", wire_protocol,
                                 "--retry", "0", "-sS", "--max-time", "5", "--max-filesize", str(OBSERVER_BODY_LIMIT),
                                 "-w", "\n%{http_code} %{http_version} %{num_connects}", "-X", "POST",
                                 "-H", "Content-Type: application/json", "--data", "{}",
                                 self.origin + "/v1/chat/completions"], capture_output=True, timeout=7, env=ENV)
        body, separator, summary = result.stdout.rpartition(b"\n")
        parts = summary.decode(errors="replace").split() if separator else []
        value = " ".join(parts[:2]) if len(parts) == 3 else "invalid_metadata"
        connects = int(parts[2]) if len(parts) == 3 and parts[2].isdigit() and len(parts[2]) <= 9 else 0
        json_error = False
        if 0 < len(body) <= OBSERVER_BODY_LIMIT:
            try:
                parsed = load(body)
                json_error = isinstance(parsed, dict) and isinstance(parsed.get("error"), dict) and bool(parsed["error"])
            except (RuntimeError, ValueError, UnicodeError):
                pass
        row = {"at": started, "completed_at": utcnow(), "protocol": protocol, "exit": result.returncode,
               "result": value, "client_digest": digest(canonical(self.client)), "body_bytes": len(body),
               "body_sha256": digest(body), "json_error": json_error, "num_connects": connects}
        row["ok"] = observer_sample_ok(row)
        return row

    def observe(self):
        path = self.evidence.root / (self.name + "-samples.jsonl")
        try:
            with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "wb") as file:
                while not self.stop_event.is_set():
                    for protocol in ("--http1.1", "--http2"):
                        row = self.sample(protocol)
                        file.write(canonical(row) + b"\n")
                        file.flush()
                        os.fsync(file.fileno())
                        self.count += 1
                        self.failed = self.failed or not row["ok"]
                    self.first.set()
                    self.stop_event.wait(1)
        except BaseException as error:
            self.error = type(error).__name__
            self.first.set()

    def failure(self, kind):
        self.evidence.write(self.name + "-operation-failed.json", {"at": utcnow(), "error_class": kind})

    def stop(self):
        self.stop_event.set()
        self.thread.join(16)
        require(not self.thread.is_alive(), "observer shutdown incomplete; coverage gap retained")
        try:
            require(self.read_client() == self.client, "observer client changed during rollout")
        except (RuntimeError, ValueError, OSError, KeyError, TypeError, subprocess.TimeoutExpired) as error:
            self.error = type(error).__name__
        self.evidence.write(self.name + "-end.json", {"at": utcnow(), "samples": self.count, "failed": self.failed, "error": self.error})


def observation_acceptance(evidence):
    windows, failed, gap = 0, False, False
    for start in sorted(evidence.root.glob("observer-*-start.json")):
        windows += 1
        name = start.name.removesuffix("-start.json")
        try:
            beginning = evidence.read(start.name)
            # Only the exact old shape is legacy. Removing a new version field
            # or mixing new fields cannot downgrade a V2 receipt to old rules.
            legacy = set(beginning) == {"at"}
            if not legacy:
                require(set(beginning) == {"at", "contract_version", "origin", "client"} and
                        beginning["contract_version"] == OBSERVER_CONTRACT, "unknown observer contract")
                qualified_observer_client(beginning["client"])
            end = evidence.read(name + "-end.json")
            require(set(end) == {"at", "samples", "failed", "error"}, "invalid observer end")
            wire = regular(evidence.root / (name + "-samples.jsonl")).read_bytes()
            require(wire.endswith(b"\n"), "truncated sample record")
            samples = [load(line) for line in wire.splitlines()]
            require(type(end["samples"]) is int and len(samples) == end["samples"] and len(samples) >= 2 and
                    type(end["failed"]) is bool and not end["error"], "observer sample gap")
            if not legacy:
                require(len(samples) % 2 == 0, "incomplete protocol pair")
            for index, row in enumerate(samples):
                if not legacy:
                    require(row["protocol"] == ("--http1.1", "--http2")[index % 2], "protocol coverage gap")
                require(set(row) == (LEGACY_SAMPLE_FIELDS if legacy else OBSERVER_SAMPLE_FIELDS), "invalid sample record")
                require(row["protocol"] in ("--http1.1", "--http2"), "unrecognized sampled protocol")
                if legacy:
                    actual_ok = row["exit"] == 0 and row["result"] == ("401 2" if row["protocol"] == "--http2" else "401 1.1")
                else:
                    require(row["client_digest"] == digest(canonical(beginning["client"])) and
                            re.fullmatch(r"[0-9a-f]{64}", row["body_sha256"]), "invalid observer proof identity")
                    actual_ok = observer_sample_ok(row)
                require(row["ok"] is actual_ok, "sample verdict does not match the recorded response")
            failed = failed or end["failed"] or any(not row["ok"] for row in samples) or evidence.has(name + "-operation-failed.json")
        except (RuntimeError, ValueError, OSError, KeyError, TypeError):
            gap = True
    if not windows:
        return "not_started"
    return "unproven_gap" if gap else "failed" if failed else "passed_short_requests_only"


@contextlib.contextmanager
def operation_locks(target):
    # Same order for every rollout; ingress CLI independently takes directory
    # lock. Import/call its owner while held, with no bypass flag or child lock.
    locks = []
    try:
        for path in (LIFECYCLE_LOCK, Path(target["ingress_directory"]) / ".ingress.lock"):
            require(not path.is_symlink(), "lock symlink refused")
            fd = os.open(path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
            locks.append(fd)
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        HELD_LOCK_FDS.extend(locks)
        yield
    finally:
        HELD_LOCK_FDS.clear()
        for fd in reversed(locks):
            os.close(fd)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("plan", "prepare", "apply", "inspect", "resume"))
    parser.add_argument("--input", required=True, type=Path)
    args = parser.parse_args()
    spec = document(args.input)
    # No host interpolation or alternate Docker daemon reaches either helper.
    os.environ.clear()
    os.environ.update(ENV)
    rollout = Rollout(spec)
    if args.action == "plan":
        result = rollout.plan()[0]
    elif args.action == "inspect":
        result = rollout.progress()[1]
    else:
        with operation_locks(spec["target"]):
            result = rollout.prepare() if args.action == "prepare" else rollout.apply(args.action == "resume")
    print(json.dumps(result, sort_keys=True))
    if result.get("deployment") == "complete" and result.get("observer_acceptance") != "passed_short_requests_only":
        return 3  # Deployment recovered; failed/missing acceptance is preserved.
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (RuntimeError, ValueError, OSError, KeyError, TypeError, subprocess.TimeoutExpired) as error:
        print(json.dumps({"deployment": "requires_inspection", "error_class": type(error).__name__, "error": str(error)}), file=sys.stderr)
        sys.exit(1)
