#!/usr/bin/env python3
"""Real installer acceptance on a disposable GitHub-hosted Linux runner only.

Uses the shipped installer and signing CLIs. No model calls, customer data,
production keys, registry credentials or public image publication are involved.
Only the allowlisted result JSON may leave the runner; raw logs stay private.
"""
import argparse
import base64
from datetime import datetime, timedelta, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
import uuid

SCHEMA = "000127_xinsight_communication_policy"
DEPENDENCIES = {"postgres": "postgres:16-alpine", "redis": "redis:7-alpine",
                "prometheus": "prom/prometheus:v2.51.0", "grafana": "grafana/grafana:11.0.0"}


class Failed(Exception):
    """Messages contain only harness-authored labels, never child output."""


def require(condition, label):
    if not condition:
        raise Failed(label)


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def files(root):
    return {str(p.relative_to(root)): sha(p) for p in root.rglob("*") if p.is_file()}


def private_write(path, data):
    with path.open("xb") as stream:
        os.chmod(path, 0o600)
        stream.write(data)


def prepare_bootstrap_fixture(module, raw, expected_sha, directory, fetch):
    try:
        anchor = module.Bootstrap(raw, expected_sha)
        module.prepare(anchor, directory, fetch=fetch)
    except module.BootstrapError as exc:
        raise Failed("community bootstrap preparation failed") from exc


class Acceptance:
    def __init__(self, repo, root, expected_commit):
        self.repo, self.root = repo, root
        self.logs = root / "logs"
        self.logs.mkdir(mode=0o700)
        self.result = {"schema": "unirouter.community-installer-acceptance.v1",
                       "status": "running", "platform": "linux/amd64", "checks": [],
                       "synthetic_signing_only": True, "provider_calls": 0}
        self.phase = "prerequisites"
        self.docker = shutil.which("docker")
        require(self.docker is not None, "Docker unavailable")
        self.docker = str(Path(self.docker).resolve())
        self.env = {k: os.environ[k] for k in ("PATH", "HOME", "LANG", "LC_ALL") if k in os.environ}
        git = ["git", "-c", "safe.directory=" + str(repo)]
        self.commit = self.run("read commit", git + ["rev-parse", "HEAD"]).strip()
        self.tree = self.run("read tree", git + ["rev-parse", "HEAD^{tree}"]).strip()
        require(self.commit == expected_commit and re.fullmatch(r"[0-9a-f]{40}", self.commit), "checkout commit mismatch")
        self.version = (repo / "VERSION").read_text().strip()
        require(re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", self.version), "invalid version")
        self.result.update(commit=self.commit, tree=self.tree, version=self.version)
        self.release = root / "release"
        self.release.mkdir(mode=0o700)
        self.instances = root / "instances"
        self.instances.mkdir(mode=0o700)
        self.images = {}
        self.image_ids = {}
        self.suffix = uuid.uuid4().hex[:10]

    def run(self, label, args, *, data=None, env=None, ok=True, timeout=1800):
        self.phase = label
        print("Native installer: " + label, flush=True)
        name = str(len(list(self.logs.iterdir()))) + ".log"
        with (self.logs / name).open("xb") as log:
            os.chmod(self.logs / name, 0o600)
            result = subprocess.run([str(a) for a in args], cwd=self.repo,
                                    env=env or self.env, input=data, stdout=log,
                                    stderr=subprocess.STDOUT, timeout=timeout)
        output = (self.logs / name).read_text(errors="replace")
        if result.returncode != 0 and hasattr(self, "release") and str(self.release / "payload/deploy/install.py") in [str(a) for a in args]:
            # install.py guarantees this line contains only its authored error
            # or a parser/system exception class, never secret-bearing child output.
            stopped = [line for line in output.splitlines() if line.startswith("Installation stopped: ")]
            if stopped:
                self.result["installer_stop"] = stopped[-1]
        if ok:
            require(result.returncode == 0, label + " failed")
        return output if ok else (result.returncode, output)

    def passed(self, label):
        self.result["checks"].append(label)
        print("PASS: " + label, flush=True)

    def inventory(self):
        result = {}
        for kind, args in (("containers", ["ps", "-aq", "--no-trunc"]),
                           ("volumes", ["volume", "ls", "-q"]),
                           ("networks", ["network", "ls", "-q", "--no-trunc"])):
            result[kind] = sorted(self.run("read " + kind, [self.docker, *args]).splitlines())
        return result

    def preflight(self):
        info = json.loads(self.run("Docker preflight", [self.docker, "info", "--format", "{{json .}} "]))
        require(info.get("OSType") == "linux" and info.get("ID"), "Linux Docker identity unavailable")
        require(not any("rootless" in v for v in info.get("SecurityOptions", [])), "rootful Docker required")
        free = shutil.disk_usage(self.root).free
        require(free >= 8 * 1024 ** 3, "less than 8 GiB free before builds")
        self.result["free_bytes_before_build"] = free
        self.result["docker_version"] = self.run("Docker versions", [self.docker, "version", "--format", "{{.Client.Version}} / {{.Server.Version}}"]).strip()
        self.result["compose_version"] = self.run("Compose version", [self.docker, "compose", "version", "--short"]).strip()
        self.result["python_version"] = platform.python_version()
        self.passed("native prerequisites")

    def request(self, url, data=None, token=None):
        headers = {"Content-Type": "application/json"}
        if token:
            headers["Authorization"] = "Bearer " + token
        request = urllib.request.Request(url, data=data, headers=headers)
        # Loopback requests must not inherit an ambient HTTP proxy.
        with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request, timeout=20) as response:
            require(response.status == 200, "unexpected HTTP status")
            return response.read()

    def prepare(self):
        self.verifier, self.keygen = self.root / "uniroute-upgrade", self.root / "uniroute-license"
        self.run("build existing verifier", ["go", "build", "-buildvcs=false", "-o", self.verifier, "./cmd/uniroute-upgrade"])
        self.run("build existing test key generator", ["go", "build", "-buildvcs=false", "-o", self.keygen, "./cmd/uniroute-license"])
        self.ring, private = self.root / "public-ring.json", self.root / "test.private"
        self.run("generate ephemeral signing key", [self.keygen, "keygen", "--key-id", "installer-native-test",
                 "--private-out", private, "--public-out", self.ring])
        self.other_ring = self.root / "other-public-ring.json"
        self.run("generate wrong-key fixture", [self.keygen, "keygen", "--key-id", "installer-native-test",
                 "--private-out", self.root / "other-test.private", "--public-out", self.other_ring])
        public = base64.urlsafe_b64encode(self.ring.read_bytes()).decode().rstrip("=")
        self.registry = "ur-native-registry-" + self.suffix
        self.run("start loopback-only test registry", [self.docker, "run", "-d", "--name", self.registry,
                 "-p", "127.0.0.1:15000:5000", "registry:2"])
        for _ in range(30):
            try:
                self.request("http://127.0.0.1:15000/v2/")
                break
            except (urllib.error.URLError, TimeoutError):
                time.sleep(1)
        else:
            raise Failed("loopback registry unavailable")
        backend = "localhost:15000/unirouter/backend:test"
        frontend = "localhost:15000/unirouter/frontend:test"
        self.run("build exact release-mode backend", [self.docker, "build", "--build-arg", "RUNTIME_VERSION=" + self.version,
                 "--build-arg", "RUNTIME_COMMIT=" + self.commit, "--build-arg", "RUNTIME_TREE=" + self.tree,
                 "--build-arg", "UNIROUTER_RELEASE_TRUST_MODE=release", "--build-arg",
                 "UNIROUTER_UPGRADE_TRUST_BUNDLE_B64=" + public, "-t", backend, "."])
        self.run("build exact frontend", [self.docker, "build", "--build-arg", "VITE_DEPLOY_ENV=production",
                 "--build-arg", "VITE_APP_VERSION=" + self.version, "-t", frontend, "frontend"])
        for service, reference in {"backend": backend, "frontend": frontend, **DEPENDENCIES}.items():
            if service in ("backend", "frontend"):
                require(reference.startswith("localhost:15000/"), "external registry push refused")
                self.run("round-trip test " + service + " through loopback registry", [self.docker, "push", reference])
            self.run("pull " + service, [self.docker, "pull", reference])
            image = json.loads(self.run("inspect " + service, [self.docker, "image", "inspect", reference]))[0]
            require(image["Os"] == "linux" and image["Architecture"] == "amd64", "image platform mismatch")
            expected_repo = reference.rsplit(":", 1)[0]
            digests = [v for v in image.get("RepoDigests", []) if v.split("@", 1)[0] == expected_repo]
            require(len(digests) == 1, "ambiguous image digest for " + service)
            self.images[service] = digests[0]
            self.image_ids[service] = image["Id"]
        deploy = self.release / "payload/deploy"
        deploy.mkdir(parents=True)
        for name in ("install.py", "docker-compose.yml", ".env.example", "preflight.sh"):
            shutil.copyfile(self.repo / "deploy/customer" / name, deploy / name)
        (deploy / "config").mkdir()
        shutil.copyfile(self.repo / "infra/prometheus/prometheus.yml", deploy / "config/prometheus.yml")
        shutil.copytree(self.repo / "infra/grafana", deploy / "config/grafana")
        (self.release / "payload/install.json").write_text(json.dumps({"schema_version": "unirouter.install.v1", "images": self.images}))
        now = datetime.now(timezone.utc)
        stamp = lambda value: value.isoformat(timespec="seconds").replace("+00:00", "Z")
        args = [self.verifier, "pack", "--key-id", "installer-native-test", "--private-key", private,
                "--release-sequence", "1", "--version", self.version, "--commit", self.commit,
                "--created-at", stamp(now - timedelta(minutes=1)), "--expires-at", stamp(now + timedelta(hours=2)),
                "--target-os", "linux", "--target-arch", "amd64", "--min-current-sequence", "0",
                "--max-current-sequence", "0", "--min-current-schema", SCHEMA, "--target-schema", SCHEMA,
                "--out", self.release / "release.tar.gz"]
        for path in sorted((self.release / "payload").rglob("*")):
            if path.is_file():
                args.extend(["--artifact", str(path.relative_to(self.release)) + "=" + str(path)])
        self.run("pack synthetic signed release", args)
        self.result["images"] = self.images
        self.result["test_package_sha256"] = sha(self.release / "release.tar.gz")
        self.passed("exact signed test release prepared")
        # Real preparation/verification; fixture transport is local and never
        # exposed as a CLI TLS bypass. Public HTTPS acceptance remains separate.
        sources = {"verifier": self.verifier, "public_keys": self.ring,
                   "package": self.release / "release.tar.gz"}
        bootstrap_value = {
            "schema_version": "unirouter.community-bootstrap.v1",
            "platform": {"os": "linux", "arch": "amd64"},
            "release": {"version": self.version, "commit": self.commit,
                        "release_sequence": 1, "key_id": "installer-native-test", "target_schema": SCHEMA},
            "assets": {key: {"url": "https://example.invalid/" + key,
                             "size": path.stat().st_size, "sha256": sha(path)}
                       for key, path in sources.items()}}
        self.bootstrap_file = self.root / "bootstrap.json"
        private_write(self.bootstrap_file, json.dumps(bootstrap_value).encode())
        self.bootstrap_sha = sha(self.bootstrap_file)
        spec = importlib.util.spec_from_file_location("community_bootstrap", self.repo / "deploy/community/bootstrap.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        def fixture_fetch(asset, target):
            private_write(target, sources[asset["url"].rsplit("/", 1)[-1]].read_bytes())
        prepared = self.root / "prepared-release"
        prepare_bootstrap_fixture(module, self.bootstrap_file.read_bytes(), self.bootstrap_sha, prepared, fixture_fetch)
        self.release = prepared
        self.result["bootstrap_sha256"] = self.bootstrap_sha
        self.result["bootstrap_transport"] = "local fixture; no public-download claim"
        self.passed("pinned synthetic bootstrap prepares the exact signed installer")

    def bootstrap_command(self, action, directory, project):
        return [sys.executable, "-I", "-B", self.repo / "deploy/community/bootstrap.py", action,
                "--bootstrap", self.bootstrap_file, "--bootstrap-sha256", self.bootstrap_sha,
                "--directory", self.release, "--install-directory", directory,
                "--name", project, "--port", "13000"]

    def command(self, action, directory, project, *, release=None, ring=None):
        return [sys.executable, "-B", self.release / "payload/deploy/install.py", action,
                "--release", release or self.release, "--verifier", self.verifier,
                "--public-keys", ring or self.ring, "--directory", directory, "--name", project, "--port", "13000"]

    def negative_checks(self, directory, project):
        before = self.inventory(), files(self.release), files(self.instances)
        self.run("read-only bootstrap check", self.bootstrap_command("check", directory, project))
        require(before == (self.inventory(), files(self.release), files(self.instances)), "bootstrap check changed state")
        self.run("read-only installer check", self.command("check", directory, project))
        require(before == (self.inventory(), files(self.release), files(self.instances)), "check changed state")
        self.passed("check has no filesystem or Docker side effects")
        code, _ = self.run("reject wrong signing key", self.command("check", directory, project, ring=self.other_ring), ok=False)
        require(code != 0, "wrong signing key accepted")
        corrupted = self.root / "corrupted-release"
        shutil.copytree(self.release, corrupted)
        with (corrupted / "payload/install.json").open("a") as stream:
            stream.write(" ")
        code, _ = self.run("reject modified signed payload", self.command("check", directory, project, release=corrupted), ok=False)
        require(code != 0, "modified payload accepted")
        require(before == (self.inventory(), files(self.release), files(self.instances)), "negative signature check changed state")
        self.passed("wrong key and modified payload refused without writes")
        volume = project + "_postgres_data"
        self.run("create synthetic unlabelled collision", [self.docker, "volume", "create", volume])
        collision = self.run("read collision identity", [self.docker, "volume", "inspect", volume])
        code, output = self.run("reject exact-name volume collision", self.command("install", directory, project), ok=False)
        require(code != 0 and "Compose project already exists" in output, "volume collision was not rejected")
        require(not directory.exists(), "collision created installation files")
        require(collision == self.run("verify collision retained", [self.docker, "volume", "inspect", volume]), "collision volume changed")
        self.run("remove exact empty test collision", [self.docker, "volume", "rm", volume])
        require(before == (self.inventory(), files(self.release), files(self.instances)), "collision check changed resources")
        self.passed("unlabelled volume collision refused before installation")

    def compose(self, directory, project):
        runtime = directory / "runtime"
        return [self.docker, "compose", "--project-name", project, "--project-directory", runtime,
                "--env-file", runtime / ".env", "-f", runtime / "docker-compose.yml",
                "-f", runtime / "installation.json", "-f", runtime / "runtime-image.json"]

    def sql(self, directory, project, query):
        return self.run("read synthetic database facts", self.compose(directory, project) +
                        ["exec", "-T", "postgres", "psql", "-X", "-U", "uniroute", "-d", "uniroute",
                         "-v", "ON_ERROR_STOP=1", "-Atc", query]).strip()

    def target(self, directory, project):
        return json.loads(self.sql(directory, project, "SELECT jsonb_build_object('system_identifier',"
            "(SELECT system_identifier::text FROM pg_control_system()),'database',current_database(),"
            "'database_oid',(SELECT oid::bigint FROM pg_database WHERE datname=current_database()),"
            "'owner',current_user,'role',current_user)::text"))

    def facts(self, directory, project):
        state = json.loads((directory / "install-state.json").read_text())
        plan = json.loads((directory / "evidence/install.json").read_text())
        require(state["image_ids"] == self.image_ids, "installed images differ from producer inspection")
        actual = self.target(directory, project)
        require(all(plan["target"][k] == v for k, v in actual.items()), "database identity differs from original plan")
        require(self.sql(directory, project, "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1") == SCHEMA, "schema is not M127")
        comment = self.sql(directory, project, "SELECT obj_description('public.schema_migrations'::regclass)")
        record = json.loads(comment.split("\n", 1)[1])
        require(record["state"] == "completed" and record["plan"] == plan, "fresh owner is not completed for the original plan")
        rotation = self.sql(directory, project, "SELECT count(*) FROM audit_logs WHERE action='user.password_reset' AND user_id=1 AND org_id=1")
        require(rotation == "1", "administrator password rotation count is not one")
        release = json.loads(self.sql(directory, project, "SELECT jsonb_build_object('sequence',release_sequence,'version',version,'commit',commit,'package',package_sha256,'schema',target_schema)::text FROM release_state"))
        require(release == {"sequence": 1, "version": self.version, "commit": self.commit,
                            "package": self.result["test_package_sha256"], "schema": SCHEMA}, "initial release registration mismatch")
        ids = self.run("read installed containers", self.compose(directory, project) + ["ps", "-aq"]).splitlines()
        containers = json.loads(self.run("inspect installed containers", [self.docker, "inspect", *ids]))
        require(len(containers) == 6, "six services were not installed")
        for container in containers:
            name = container["Config"]["Labels"]["com.docker.compose.service"]
            require(container["Image"] == state["image_ids"][name], "running image mismatch")
            require(container["State"]["Running"], "installed service is not running")
            for binds in (container["HostConfig"].get("PortBindings") or {}).values():
                require(name == "frontend" and all(b["HostIp"] == "127.0.0.1" and b["HostPort"] == "13000" for b in binds), "non-loopback or unexpected published service")
            if name in ("backend", "frontend", "postgres", "redis"):
                require(container["State"].get("Health", {}).get("Status") == "healthy", "service health failed")
        return {"target": actual, "plan_sha256": sha(directory / "evidence/install.json"),
                "record": record, "release": release, "rotation": rotation,
                "files": state["files"], "installation_id": state["installation_id"], "images": state["image_ids"]}

    def interrupt_after_plan(self, directory, project):
        # A test-only transport wrapper delegates the actual Docker command.
        # The installer process and the database owner are never monkeypatched.
        wrapper_dir = self.root / "fault-bin"
        wrapper_dir.mkdir(mode=0o700)
        marker = self.root / "plan-response-lost"
        expected = self.compose(directory, project)[1:] + ["run", "--rm", "--no-deps", "--pull", "never", "-T",
            "--volume", str(directory / "evidence") + ":/evidence",
            "--volume", str(directory / "release.tar.gz") + ":/install/release.tar.gz:ro"]
        # install.py supplies an explicit daemon host before compose.
        expected = [str(v) for v in ["--host", "unix:///var/run/docker.sock", *expected]]
        config = {"real": self.docker, "prefix": expected, "marker": str(marker),
                  "plan": str(directory / "evidence/install.json"), "installer": str(self.release / "payload/deploy/install.py"),
                  "image": self.image_ids["backend"]}
        wrapper = wrapper_dir / "docker"
        code = '''#!/usr/bin/env python3
import json, os, signal, subprocess, sys
from pathlib import Path
C = CONFIG
args = sys.argv[1:]
prefix = C["prefix"]
tail = ["backend", "./uniroute", "fresh-install", "plan", "--out", "/evidence/install.json"]
match = (args[:len(prefix)] == prefix and len(args) == len(prefix) + 2 + len(tail)
         and args[len(prefix)] == "--env" and args[len(prefix)+1] == "RUNTIME_IMAGE_DIGEST=" + C["image"]
         and args[-len(tail):] == tail and not Path(C["marker"]).exists())
if not match:
    os.execv(C["real"], [C["real"], *args])
parent = os.getppid()
parent_args = Path(f"/proc/{parent}/cmdline").read_bytes().split(b"\\0")
if C["installer"].encode() not in parent_args:
    sys.exit(91)
result = subprocess.run([C["real"], *args])
if result.returncode:
    sys.exit(result.returncode)
plan = json.loads(Path(C["plan"]).read_text())
if not plan.get("installation_id") or not plan.get("digest"):
    sys.exit(92)
fd = os.open(C["marker"], os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
os.write(fd, b"real-plan-completed-response-lost\\n")
os.fsync(fd)
os.close(fd)
fd = os.open(str(Path(C["marker"]).parent), os.O_RDONLY | os.O_DIRECTORY)
os.fsync(fd)
os.close(fd)
if os.getppid() != parent or Path(f"/proc/{parent}/cmdline").read_bytes().split(b"\\0") != parent_args:
    sys.exit(93)
os.kill(parent, signal.SIGKILL)
'''.replace("CONFIG", repr(config))
        private_write(wrapper, code.encode())
        wrapper.chmod(0o700)
        env = dict(self.env, PATH=str(wrapper_dir) + os.pathsep + self.env["PATH"])
        code, _ = self.run("interrupt after actual fresh plan completion", self.command("install", directory, project), env=env, ok=False)
        require(code == -signal.SIGKILL and marker.is_file(), "expected transport interruption did not occur")
        state = json.loads((directory / "install-state.json").read_text())
        require(state["pending"] == "fresh-plan", "unexpected interruption checkpoint")
        plan = json.loads((directory / "evidence/install.json").read_text())
        original = sha(directory / "evidence/install.json"), state["files"], self.target(directory, project)
        require(all(plan["target"][k] == v for k, v in original[2].items()), "interrupted plan DB identity mismatch")
        require(self.sql(directory, project, "SELECT to_regclass('public.schema_migrations') IS NULL") == "t", "database claimed before planned fault")
        live_ids = self.run("check installation processes after fault", [self.docker, "ps", "-q", "--filter",
            "label=com.docker.compose.project=" + project]).splitlines()
        live = json.loads(self.run("read running carrier labels", [self.docker, "inspect", *live_ids])) if live_ids else []
        require(not any(str(c["Config"]["Labels"].get("com.docker.compose.oneoff", "")).lower() == "true"
                        for c in live), "one-off carrier still running after fault")
        self.run("resume actual interrupted installation", self.command("resume", directory, project))
        current = json.loads((directory / "install-state.json").read_text())
        require(original == (sha(directory / "evidence/install.json"), current["files"], self.target(directory, project)), "resume changed original plan, secrets or database")
        self.passed("real plan completion followed by lost response resumes with original identity")

    def installed_checks(self, directory, project):
        before = self.facts(directory, project)
        self.run("repeat completed resume", self.command("resume", directory, project))
        require(before == self.facts(directory, project), "completed resume changed installation authority")
        self.run("repeat completed bootstrap resume", self.bootstrap_command("resume", directory, project))
        require(before == self.facts(directory, project), "bootstrap resume changed installation authority")
        self.passed("bootstrap resume preserves the completed original installation")
        inventory = self.inventory()
        code, _ = self.run("reject wrong project on resume", self.command("resume", directory, project + "-wrong"), ok=False)
        require(code != 0, "wrong project resume accepted")
        code, _ = self.run("reject wrong daemon on resume", self.command("resume", directory, project) + ["--docker-socket", "/tmp/not-the-fixture-docker.sock"], ok=False)
        require(code != 0 and inventory == self.inventory(), "identity refusal changed resources")
        self.passed("M127, image, DB, release and one-rotation identities survive repeat resume")
        self.phase = "private console and authentication"
        self.request("http://127.0.0.1:13000/health/ready")
        page = self.request("http://127.0.0.1:13000/")
        require(b'<div id="root"' in page, "frontend application document unavailable")
        password = (directory / "admin-password").read_text().strip()
        response = json.loads(self.request("http://127.0.0.1:13000/api/v1/auth/login",
                             data=json.dumps({"username": "admin", "password": password}).encode()))
        require(response.get("organization_role") == "platform_admin" and response.get("token"), "fresh administrator login failed")
        status = json.loads(self.request("http://127.0.0.1:13000/api/v1/license", token=response["token"]))
        require(status.get("status") == "not_required", "commercial License still required")
        self.passed("real NGINX console, administrator login and license not_required")

    def failure_facts(self):
        """Read only this fixture; emit authored labels and scalar states only."""
        if not hasattr(self, "project"):
            return
        previous_phase = self.phase
        try:
            path = self.directory / "install-state.json"
            if path.is_file():
                state = json.loads(path.read_text())
                allowed = {"fresh-plan", "fresh-apply", "record-release", "rotate-administrator", "start-console", "start-databases"}
                self.result["installation_checkpoint"] = {
                    "done": sorted(set(state.get("done", [])) & allowed),
                    "pending": state.get("pending") if state.get("pending") in allowed else None}
            ids = self.run("read failed fixture container identities", [self.docker, "ps", "-aq", "--filter",
                "label=com.docker.compose.project=" + self.project]).splitlines()
            if not ids:
                return
            containers = json.loads(self.run("inspect failed fixture states", [self.docker, "inspect", *ids]))
            facts = {}
            # Match only source-authored constant fatal prefixes. Dynamic log
            # arguments, health output, container env and commands never leave.
            source = (self.repo / "cmd/uniroute/main.go").read_text()
            fatal_prefixes = re.findall(r'log\.Fatalf\("([^"%]+)', source)
            health_markers = ("Connection refused", "bad address", "403 Forbidden", "503 Service Unavailable")
            for container in containers:
                name = container["Config"]["Labels"].get("com.docker.compose.service")
                if name not in self.images:
                    continue
                state = container["State"]
                health = state.get("Health", {})
                item = {"running": bool(state.get("Running")), "exit_code": state.get("ExitCode"),
                        "oom_killed": bool(state.get("OOMKilled")), "restart_count": container.get("RestartCount"),
                        "health": health.get("Status") if health.get("Status") in ("starting", "healthy", "unhealthy") else None,
                        "health_exit_codes": [v.get("ExitCode") for v in health.get("Log", [])],
                        "health_markers": sorted({marker for v in health.get("Log", []) for marker in health_markers if marker in v.get("Output", "")})}
                if name in ("backend", "frontend"):
                    _, logs = self.run("classify failed " + name + " logs", [self.docker, "logs", "--tail", "100", container["Id"]], ok=False)
                    item["fatal_prefixes"] = sorted({prefix for prefix in fatal_prefixes if prefix in logs})
                    if state.get("Running"):
                        port = "8080" if name == "backend" else "80"
                        item["health_probe_exit_codes"] = {}
                        for host in ("localhost", "127.0.0.1", "[::1]"):
                            code, output = self.run("diagnose " + name + " loopback health", [self.docker, "exec", container["Id"],
                                "wget", "-qO-", "-T", "5", "http://" + host + ":" + port + "/health/ready"], ok=False, timeout=15)
                            item["health_probe_exit_codes"][host] = code
                facts[name] = item
            self.result["failed_container_facts"] = facts
        except (Failed, OSError, ValueError, KeyError, subprocess.TimeoutExpired):
            self.result["failure_diagnostics_incomplete"] = True
        finally:
            self.phase = previous_phase

    def execute(self):
        self.preflight()
        self.prepare()
        project = "ur-native-" + self.suffix
        directory = self.instances / "interrupted"
        self.project, self.directory = project, directory
        self.negative_checks(directory, project)
        self.interrupt_after_plan(directory, project)
        self.installed_checks(directory, project)
        self.result["status"] = "passed"
        self.result.pop("installer_stop", None)
        # All resources belong to this disposable runner; no broad prune/cleanup.
        # The runner lifecycle disposes private keys, logs, images and test DB.


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--expected-commit", required=True)
    parser.add_argument("--report", type=Path, required=True)
    args = parser.parse_args()
    require(os.environ.get("GITHUB_ACTIONS") == "true" and
            os.environ.get("RUNNER_ENVIRONMENT") == "github-hosted" and
            platform.system() == "Linux" and platform.machine() == "x86_64" and os.geteuid() == 0,
            "requires a disposable GitHub-hosted Linux amd64 runner as root")
    require(sys.version_info >= (3, 10), "Python 3.10+ required")
    repo = Path(__file__).resolve().parents[2]
    # Root executes prepared files; do not put them below runner-owned ancestry.
    # The disposable runner lifecycle still owns all fixture resources.
    root = Path(tempfile.mkdtemp(prefix="ur-community-native-", dir="/var/tmp")).resolve()
    os.chmod(root, 0o700)
    fixture = Acceptance(repo, root, args.expected_commit)
    try:
        fixture.execute()
    except (Failed, OSError, ValueError, KeyError, subprocess.TimeoutExpired, urllib.error.URLError) as exc:
        fixture.result.update(status="failed", phase=fixture.phase,
                              error=str(exc) if isinstance(exc, Failed) else type(exc).__name__)
        fixture.failure_facts()
    finally:
        # Explicitly allowlisted facts only. No keys, raw logs, DSNs, env or JWT.
        args.report.write_text(json.dumps(fixture.result, indent=2) + "\n")
        os.chmod(args.report, 0o644)
    print(json.dumps(fixture.result), flush=True)
    return 0 if fixture.result["status"] == "passed" else 1


if __name__ == "__main__":
    sys.exit(main())
