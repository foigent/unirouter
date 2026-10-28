#!/usr/bin/env python3
"""Real installer acceptance on a disposable GitHub-hosted Linux runner only.

Uses the shipped installer and signing CLIs. No model calls, customer data,
production keys, registry credentials or public image publication are involved.
Only the allowlisted result JSON may leave the runner; raw logs stay private.
"""
import argparse
import ast
import base64
import copy
from collections import namedtuple
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
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
import uuid

RETAINED_COMMIT = "d92abe0a5bd4b997893ba6043a213d1a98108883"
RETAINED_SCHEMA = "000127_xinsight_communication_policy"
CURRENT_SCHEMA = "000128_upstream_release_observation"
SourceContract = namedtuple("SourceContract", "schema ledger supported_heads")
# Match the accepted installation descriptor, not build-stage pull labels or floating tags.
DEPENDENCIES = {
    "postgres": "postgres@sha256:1a66d744c1b459e13b05a8fca341da84cb63383e99ce262210efee5a319d4551",
    "redis": "redis@sha256:ca0acbb137c1dc3339c8b147a58fd6f42775d4599327b50e7b116c23de501af2",
    "prometheus": "prom/prometheus@sha256:3305cd2e483c13364c94ae4905368eb425c9fa804f8e523ee47d8bf4de9d87a2",
    "grafana": "grafana/grafana@sha256:a80bc3848cf5d4b2958ea25dbeb36fa9442ef4be8c73fe4bff11340307c32919",
}


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


def source_contract(repo):
    """Read exact source owners; a filename maximum alone grants no authority."""
    heads = []
    for name in ("deploy/customer/install.py", "deploy/community/bootstrap.py"):
        tree = ast.parse((repo / name).read_bytes())
        assignments = [node.value for node in tree.body if isinstance(node, ast.Assign)
            and any(isinstance(target, ast.Name) and target.id == "SCHEMA" for target in node.targets)]
        require(len(assignments) == 1, "ambiguous shipped schema metadata")
        try:
            heads.append(ast.literal_eval(assignments[0]))
        except (ValueError, TypeError):
            raise Failed("shipped schema metadata must be literal") from None
    require(heads[0] == heads[1] and heads[0] in (RETAINED_SCHEMA, CURRENT_SCHEMA),
            "shipped installer/bootstrap schema disagreement or unsupported fixture source")
    text = (repo / "internal/schemaauthority/version.go").read_text()
    constants = dict(re.findall(r'^const ([A-Za-z]+) = "([a-z0-9_]+)"$', text, re.M))
    latest = re.findall(r'^const LatestSupportedMigration = ([A-Za-z]+)$', text, re.M)
    supported = re.findall(r'return \[\]string\{([^}]+)\}', text)
    require(len(latest) == len(supported) == 1 and constants.get(latest[0]) == heads[0],
            "native source schema authority disagrees with shipped installers")
    names = [name.strip() for name in supported[0].split(",") if name.strip()]
    require(names and len(names) == len(set(names)) and all(name in constants for name in names),
            "ambiguous native supported-schema declaration")
    ledger = tuple(sorted(path.name.removesuffix(".up.sql") for path in (repo / "migrations").glob("*.up.sql")))
    native = tuple(sorted(constants[name] for name in names))
    require(ledger and ledger[-1] == heads[0] and set(native).issubset(ledger)
            and heads[0] in native, "source migration ledger disagrees with native schema authority")
    return SourceContract(heads[0], ledger, native)


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
        self.source_contract = source_contract(repo)
        self.schema = self.source_contract.schema
        self.release_sequence = 2 if self.schema == CURRENT_SCHEMA else 1
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
        error_path = self.logs / (name + ".stderr")
        with (self.logs / name).open("xb") as log, error_path.open("xb") as errors:
            os.chmod(self.logs / name, 0o600)
            os.chmod(error_path, 0o600)
            result = subprocess.run([str(a) for a in args], cwd=self.repo,
                                    env=env or self.env, input=data, stdout=log,
                                    stderr=errors, timeout=timeout)
        stdout = (self.logs / name).read_text(errors="replace")
        output = stdout + error_path.read_text(errors="replace")
        if result.returncode != 0 and hasattr(self, "release") and str(self.release / "payload/deploy/install.py") in [str(a) for a in args]:
            # install.py guarantees this line contains only its authored error
            # or a parser/system exception class, never secret-bearing child output.
            stopped = [line for line in output.splitlines() if line.startswith("Installation stopped: ")]
            if stopped:
                self.result["installer_stop"] = stopped[-1]
        if ok:
            require(result.returncode == 0, label + " failed")
        # Native CLI JSON belongs to stdout; startup diagnostics remain private
        # on stderr. Failure classification still sees both original streams.
        return stdout if ok else (result.returncode, output)

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
        self.prepare_release(private)

    def prepare_release(self, private):
        """One package/image path for the two exact source identities and trust."""
        public = base64.urlsafe_b64encode(self.ring.read_bytes()).decode().rstrip("=")
        backend = "localhost:15000/unirouter/backend:" + self.commit
        frontend = "localhost:15000/unirouter/frontend:" + self.commit
        self.run("build exact release-mode backend", [self.docker, "build", "--build-arg", "RUNTIME_VERSION=" + self.version,
                 "--build-arg", "RUNTIME_COMMIT=" + self.commit, "--build-arg", "RUNTIME_TREE=" + self.tree,
                 "--build-arg", "UNIROUTER_RELEASE_TRUST_MODE=release", "--build-arg",
                 "UNIROUTER_UPGRADE_TRUST_BUNDLE_B64=" + public, "-t", backend, "."])
        self.run("build exact frontend", [self.docker, "build", "--build-arg", "VITE_DEPLOY_ENV=production",
                 "--build-arg", "VITE_APP_VERSION=" + self.version, "-t", frontend, "frontend"])
        native_observations = []
        for service, reference in {"backend": backend, "frontend": frontend, **DEPENDENCIES}.items():
            if service in ("backend", "frontend"):
                require(reference.startswith("localhost:15000/"), "external registry push refused")
                self.run("round-trip test " + service + " through loopback registry", [self.docker, "push", reference])
            self.run("pull " + service, [self.docker, "pull", reference])
            image = json.loads(self.run("inspect " + service, [self.docker, "image", "inspect", reference]))[0]
            require(image["Os"] == "linux" and image["Architecture"] == "amd64", "image platform mismatch")
            expected_repo = reference.split("@", 1)[0] if "@" in reference else reference.rsplit(":", 1)[0]
            digests = [v for v in image.get("RepoDigests", []) if v.split("@", 1)[0] == expected_repo]
            require(len(digests) == 1, "ambiguous image digest for " + service)
            self.images[service] = digests[0]
            self.image_ids[service] = image["Id"]
            native_observations.append(dict(role=service, **{k:image[k] for k in
                ("Id", "Os", "Architecture", "RepoDigests")}))
        deploy = self.release / "payload/deploy"
        deploy.mkdir(parents=True)
        names = ["install.py", "docker-compose.yml", ".env.example", "preflight.sh"]
        if self.schema == CURRENT_SCHEMA:
            names += ["upgrade.py", "recovery.py"]
        for name in names:
            shutil.copyfile(self.repo / "deploy/customer" / name, deploy / name)
        if self.schema == RETAINED_SCHEMA:
            # A retained operator can have historical content. Configure that
            # synthetic fixture before its first native start, so no stale
            # writer is manufactured by changing mounts on a live container.
            body = self.root / "historical-body"
            body.mkdir(mode=0o700)
            (body / "objects").mkdir(mode=0o700)
            self.run("initialize retained synthetic content through native owner", [self.docker, "run", "--rm",
                "--network", "none", "--pull", "never", "--volume", str(body) + ":/fixture-body",
                self.images["backend"], "./uniroute", "content-store", "init", "--root", "/fixture-body/objects",
                "--identity", "community-linux-history", "--token-file", "/fixture-body/binding.token"])
            sample = (deploy / ".env.example").read_text()
            for key, value in {"SOURCE_CONTENT_STORAGE_ROOT": "/fixture-body/objects",
                "SOURCE_CONTENT_STORAGE_IDENTITY": "community-linux-history",
                "SOURCE_CONTENT_STORAGE_TOKEN_FILE": "/fixture-body/binding.token"}.items():
                sample, count = re.subn(r"^" + key + r"=.*$", key + "=" + value, sample, flags=re.M)
                require(count == 1, "retained fixture content configuration field missing")
            (deploy / ".env.example").write_text(sample)
            compose = (deploy / "docker-compose.yml").read_text()
            compose, count = re.subn(r"^  backend:\n", lambda match:
                match.group() + "    volumes:\n      - " + json.dumps(str(body) + ":/fixture-body") + "\n", compose, flags=re.M)
            require(count == 1, "retained fixture backend declaration differs")
            (deploy / "docker-compose.yml").write_text(compose)
            self.result["historical_fixture_config"] = "native body binding; signed synthetic operator configuration before first start"
        if self.schema == CURRENT_SCHEMA:
            scripts = self.release / "payload/scripts"
            scripts.mkdir()
            shutil.copyfile(self.repo / "scripts/source-budget-restore-rebase.sql",
                            scripts / "source-budget-restore-rebase.sql")
        (deploy / "config").mkdir()
        shutil.copyfile(self.repo / "infra/prometheus/prometheus.yml", deploy / "config/prometheus.yml")
        shutil.copytree(self.repo / "infra/grafana", deploy / "config/grafana")
        # Exercise the same finite selector used before owner freeze. Upstream
        # expectations retain the accepted native names; test apps use only the
        # existing loopback registry and its ephemeral embedded public trust.
        spec = importlib.util.spec_from_file_location("native_install_descriptor",
            self.repo / ".github/tests/community-install-descriptor.py")
        selector = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(selector)
        prior = {"schema_version": "unirouter.install.v1", "images": dict(self.images, **DEPENDENCIES)}
        selected = selector.select_descriptor(prior, {k:self.images[k] for k in ("backend", "frontend")},
            {"images":native_observations})
        require(selected["images"] == self.images, "native selected descriptor differs from inspected images")
        (self.release / "payload/install.json").write_bytes(selector.wire(selected))
        self.result["descriptor_selection"] = "pinned upstream names and native exact RepoDigests"
        now = datetime.now(timezone.utc)
        stamp = lambda value: value.isoformat(timespec="seconds").replace("+00:00", "Z")
        payload = sorted(path for path in (self.release / "payload").rglob("*") if path.is_file())
        expected = self.release / "expected-artifacts.json"
        private_write(expected, json.dumps([{"path": str(path.relative_to(self.release)),
            "size": path.stat().st_size, "sha256": sha(path)} for path in payload]).encode())
        args = [self.verifier, "pack", "--expected-artifacts", expected,
                "--key-id", "installer-native-test", "--private-key", private,
                "--release-sequence", str(self.release_sequence), "--version", self.version, "--commit", self.commit,
                "--created-at", stamp(now - timedelta(minutes=1)), "--no-expiry",
                "--target-os", "linux", "--target-arch", "amd64", "--min-current-sequence", "0",
                "--max-current-sequence", str(self.release_sequence - 1),
                "--min-current-schema", RETAINED_SCHEMA, "--target-schema", self.schema,
                "--out", self.release / "release.tar.gz"]
        for path in payload:
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
                        "release_sequence": self.release_sequence, "key_id": "installer-native-test", "target_schema": self.schema},
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

    def sql(self, directory, project, query, database="uniroute"):
        return self.run("read synthetic database facts", self.compose(directory, project) +
                        ["exec", "-T", "postgres", "psql", "-X", "-qAt", "-U", "uniroute", "-d", database,
                         "-v", "ON_ERROR_STOP=1"], data=query.encode()).strip()

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
        ledger = json.loads(self.sql(directory, project, "SELECT jsonb_agg(version ORDER BY version) FROM schema_migrations"))
        require(tuple(ledger) == self.source_contract.ledger, "complete installed schema ledger differs from exact source")
        support = json.loads(self.run("read actual installed native schema support", self.compose(directory, project) +
            ["exec", "-T", "backend", "./uniroute", "runtime-generation", "support"]))
        require(tuple(sorted(support.get("schema_heads", []))) == self.source_contract.supported_heads,
                "actual native schema support differs from exact source")
        comment = self.sql(directory, project, "SELECT obj_description('public.schema_migrations'::regclass)")
        record = json.loads(comment.split("\n", 1)[1])
        require(record["state"] == "completed" and record["plan"] == plan, "fresh owner is not completed for the original plan")
        rotation = self.sql(directory, project, "SELECT count(*) FROM audit_logs WHERE action='user.password_reset' AND user_id=1 AND org_id=1")
        require(rotation == "1", "administrator password rotation count is not one")
        release = json.loads(self.sql(directory, project, "SELECT jsonb_build_object('sequence',release_sequence,'version',version,'commit',commit,'package',package_sha256,'schema',target_schema)::text FROM release_state"))
        require(release == {"sequence": self.release_sequence, "version": self.version, "commit": self.commit,
                            "package": self.result["test_package_sha256"], "schema": self.schema}, "initial release registration mismatch")
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
        self.passed("exact schema, image, DB, release and one-rotation identities survive repeat resume")
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

    def native_first_use_checks(self, directory, project, *, restart=True):
        # These pure assertions are shared with the later real-public-package
        # consumer. This synthetic fixture never consumes its pending config.
        spec = importlib.util.spec_from_file_location("first_use_assertions",
            self.repo / ".github/tests/community-release-acceptance.py")
        first = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(first)
        try:
            snapshot = lambda: first.strict_json(self.sql(directory, project, first.FIRST_CUSTOMER_STATE_SQL))
            retained = lambda: (sha(directory / "admin-password"), sha(directory / "install-state.json"),
                sha(directory / "evidence/install.json"), files(directory / "runtime"))
            self.phase = "native caller-selected synthetic password"
            before, original_files = snapshot(), retained()
            generated = (directory / "admin-password").read_text().strip()
            self.run("choose public synthetic password through native stdin", self.compose(directory, project) +
                ["exec", "-T", "backend", "./uniroute", "admin", "set-password", "--username", "admin",
                 "--org-id", "1", "--password-stdin"], data=(first.FIRST_USE_TEST_INPUT + "\n").encode())
            first.password_choice_delta(before, snapshot())
            require(retained() == original_files, "password choice changed original installer files")
            first.fixture_admin_login(generated, expected_status=401)
            self.session_refusals(directory, project, first.FIRST_USE_TEST_INPUT)
            login = first.fixture_admin_login(first.FIRST_USE_TEST_INPUT)
            self.result["caller_selected_password"] = {"initial_installer_rotations":1, "final_rotations":2,
                "auth_epoch_advanced_once":True, "generated_old_input_refused":True,
                "original_installer_files_unchanged":True}
            self.passed("native password choice and first login preserve original installer state")
            token, intent = login["token"], uuid.uuid4().hex
            before = snapshot()
            self.phase = "native authenticated first-customer read-only preview"
            preview = first.first_customer_request(token, intent, dry_run=True, expected_status=200)
            first.first_customer_preview(preview, before, expected_rotations=2)
            require(first.immutable_digest(before) == first.immutable_digest(snapshot()), "first-customer preview changed state")
            self.phase = "native first-customer commit and known-success replay"
            created = first.first_customer_request(token, intent, dry_run=False, expected_status=201)
            customer_id = first.first_customer_result(created, idempotent=False)
            after = snapshot()
            first.first_customer_transition(before, after, customer_id, intent)
            replay = first.first_customer_request(token, intent, dry_run=False, expected_status=200)
            first.first_customer_result(replay, idempotent=True, customer_id=customer_id)
            require(first.immutable_digest(after) == first.immutable_digest(snapshot()), "known-success replay duplicated state")
            if restart:
                self.run("stop only the native synthetic backend", self.compose(directory, project) + ["stop","-t","30","backend"])
                self.run("restart synthetic backend with bounded native health wait", self.compose(directory, project) +
                    ["up","-d","--no-deps","--wait","--wait-timeout","120","--pull","never","--no-build","backend"])
                self.request("http://127.0.0.1:13000/health/ready")
                first.fixture_admin_login(first.FIRST_USE_TEST_INPUT)
            restarted = snapshot()
            first.first_customer_transition(before, restarted, customer_id, intent, restarted=restart)
            self.result["first_customer"] = {"organization_id":customer_id,
                "platform_org_id":before["admin"]["org_id"], "default_project_id":restarted["projects"][0]["id"] if restart else None,
                "preview_read_only":True, "same_intent_replay":True, "chosen_admin_password_usable":True,
                "protected_release_and_rotation_preserved":True, "provider_calls":0}
            self.passed("native first-customer preview, commit and same-intent replay preserve protected state" +
                        (" through restart" if restart else " before maintenance"))
        except first.Stop as exc:
            raise Failed(str(exc)) from None

    def session_refusals(self, directory, project, password):
        """Exercise normal logout and server-expired sessions before bootstrap."""
        def login():
            return json.loads(self.request("http://127.0.0.1:13000/api/v1/auth/login",
                data=json.dumps({"username": "admin", "password": password}).encode()))["token"]
        def refused(token):
            try:
                self.request("http://127.0.0.1:13000/api/v1/bootstrap/first-customer",
                    data=b'{"organization_name":"Synthetic refused customer","dry_run":true}', token=token)
            except urllib.error.HTTPError as error:
                code = error.code
                error.close()
                require(code == 401, "invalid session did not return unauthorized")
            else:
                raise Failed("invalid session was accepted for first-customer preview")
        token = login()
        self.request("http://127.0.0.1:13000/api/v1/auth/logout", data=b"{}", token=token)
        refused(token)
        token = login()
        # Only the latest synthetic administrator session, with a valid
        # earlier issue/expiry pair. No real JWT/secret is placed in SQL/argv.
        self.sql(directory, project, "UPDATE admin_sessions SET issued_at=clock_timestamp()-interval '2 hours',"
            "expires_at=clock_timestamp()-interval '1 hour' WHERE id=(SELECT max(id) FROM admin_sessions "
            "WHERE user_id=(SELECT id FROM users WHERE username='admin'))")
        refused(token)
        self.sql(directory, project, "DELETE FROM admin_sessions WHERE id=(SELECT max(id) FROM admin_sessions "
            "WHERE user_id=(SELECT id FROM users WHERE username='admin')) AND expires_at<clock_timestamp()")
        refused(token)
        self.result["session_refusals"] = {"logged_out": True, "server_expired": True, "expired_fixture_session_removed": True}
        self.passed("normal logged-out, server-expired and cleaned sessions cannot bootstrap a customer")

    def retained_release(self):
        """Build the retained predecessor once, using this fixture's same trust."""
        source = self.root / "retained-source"
        git = ["git", "-c", "safe.directory=" + str(self.repo)]
        self.run("clone retained M127 source", git + ["clone", "--shared", "--no-checkout", self.repo, source])
        self.run("select retained M127 source", ["git", "-C", source, "checkout", "--detach", RETAINED_COMMIT])
        root = self.root / "retained-fixture"
        root.mkdir(mode=0o700)
        old = Acceptance(source, root, RETAINED_COMMIT)
        require(old.version == "1.14.2" and old.schema == RETAINED_SCHEMA, "retained source identity changed")
        old.verifier, old.keygen, old.ring, old.other_ring = self.verifier, self.keygen, self.ring, self.other_ring
        require(shutil.disk_usage(root).free >= 8 << 30, "insufficient space before retained fixture builds")
        old.prepare_release(self.root / "test.private")
        self.result["retained_source"] = {"commit": old.commit, "tree": old.tree, "version": old.version,
            "schema": old.schema, "package_sha256": old.result["test_package_sha256"]}
        return old

    def historical_data(self, old, directory, project):
        """Nonempty synthetic money/history and native-bound body, never a model call."""
        customer = old.result["first_customer"]
        org = customer["organization_id"]
        require(type(org) is int and org > 1,
                "retained first-customer identity is missing")
        project_id = int(old.sql(directory, project, "INSERT INTO projects(org_id,name,slug,status,budget_period) "
            "VALUES(" + str(org) + ",'Synthetic retained default','default','active','none') RETURNING id"))
        body = old.root / "historical-body"
        token = (body / "binding.token").read_text().strip()
        require(re.fullmatch(r"[a-f0-9]{64}", token) is not None, "native content binding missing")
        raw = b"Synthetic retained customer body; no provider or customer data.\n"
        name = hashlib.sha256(raw).hexdigest()
        if not (body / "objects" / name).exists():
            private_write(body / "objects" / name, raw)
        require(sha(body / "objects" / name) == name, "retained synthetic body changed")
        old.sql(directory, project, """BEGIN;
UPDATE wallets SET balance_micros=1234567 WHERE org_id={org};
INSERT INTO transactions(request_id,org_id,project_id,type,status,amount_micros,balance_after_micros,note)
VALUES('synthetic-linux-history',{org},{project_id},'topup','completed',1234567,1234567,'Synthetic preservation fixture');
INSERT INTO execution_content_streams(org_id,project_id,source_instance)
VALUES({org},{project_id},'community-linux-history');
INSERT INTO execution_content_objects(content_ref,org_id,project_id,source_fact_id,revision,storage_identity,
storage_binding_token,policy_version,policy_config,policy_effective_at,recovery_epoch,relative_path,state,
reserved_bytes,saved_bytes,digest,expires_at,lease_owner,lease_expires_at)
VALUES('uc1_{name}',{org},{project_id},'synthetic-linux-history',1,'community-linux-history','{token}',1,
convert_to('{{}}','UTF8'),clock_timestamp(),1,'{name}','ready',0,{size},'sha256:{name}',
clock_timestamp()+interval '1 day','synthetic-linux-history',clock_timestamp()); COMMIT;""".format(
            org=org, project_id=project_id, name=name, token=token, size=len(raw)))
        # The retained deployment may have operator configuration. Freeze it
        # before maintenance; the upgrader must preserve these exact settings.
        rendered = json.loads(old.run("read retained runtime for body mount", old.compose(directory, project) +
                                      ["config", "--format", "json"]))
        return body, name, rendered

    def named_edge(self, old, directory, project, rendered):
        """The supported named adapter; reuse the existing TLS/NGINX inputs."""
        edge = directory / "edge"
        edge.mkdir(mode=0o700)
        backend = copy.deepcopy(rendered["services"]["backend"])
        backend.pop("container_name", None)
        backend["environment"]["RUNTIME_DEPLOYMENT_ROLE"] = "canary"
        backend["networks"] = {"default": {}}
        overrides = {"services": {"backend": {}, "backend-canary": backend}}
        private_write(edge / "runtime-overrides.json", json.dumps(overrides).encode())
        self.run("create loopback-only edge TLS fixture", ["openssl", "req", "-x509", "-newkey", "rsa:2048",
            "-nodes", "-keyout", edge / "loopback.key", "-out", edge / "loopback.crt", "-days", "1",
            "-subj", "/CN=127.0.0.1", "-addext", "subjectAltName=IP:127.0.0.1"])
        (edge / "loopback.key").chmod(0o600)
        # Keep the product's actual location and proxy behavior. Only its
        # frontend DNS and the surrounding standalone main config differ.
        membership = (self.repo / "nginx/runtime-membership/upstreams.conf").read_text()
        for source, destination in (("uniroute-api-canary", project + "-backend-canary-1"),
                                    ("uniroute-api", project + "-backend-1"),
                                    ("uniroute-frontend", project + "-frontend-1")):
            membership = membership.replace(source + ":", destination + ":")
        private_write(edge / "upstreams.conf", membership.encode())
        raw = b"events {}\nhttp {\n" + (self.repo / "nginx/nginx.conf").read_bytes() + b"\n}\n"
        private_write(edge / "nginx.conf", raw)
        image = "nginx:1.30.3-alpine"
        self.run("pull existing named edge fixture image", [self.docker, "pull", image])
        mounts = [str(edge / name) + ":" + target + ":ro" for name, target in (
            ("nginx.conf", "/etc/nginx/nginx.conf"), ("upstreams.conf", "/etc/nginx/runtime-membership/upstreams.conf"),
            ("loopback.crt", "/etc/nginx/ssl/fullchain.pem"), ("loopback.key", "/etc/nginx/ssl/privkey.pem"))]
        private_write(edge / "compose.edge.json", json.dumps({"services": {"nginx": {
            "image": image, "container_name": "uniroute-nginx", "restart": "no", "volumes": mounts,
            "ports": ["127.0.0.1:13443:443"], "networks": ["default"]}},
            "networks": {"default": {"external": True, "name": rendered["networks"]["default"]["name"]}}}).encode())
        stable = old.run("retain original stable process identity", [self.docker, "inspect", project + "-backend-1"])
        stable = json.loads(stable)[0]
        old.run("start retained stable and canary", old.compose(directory, project) +
            ["-f", edge / "runtime-overrides.json", "up", "-d", "--wait", "--no-deps", "--pull", "never", "--no-build", "backend", "backend-canary"])
        actual = json.loads(old.run("verify original stable process was not replaced", [self.docker, "inspect", project + "-backend-1"]))[0]
        require(actual["Id"] == stable["Id"] and actual["State"]["Pid"] == stable["State"]["Pid"],
                "named fixture unexpectedly replaced the original stable writer")
        self.run("start retained named edge", [self.docker, "compose", "--project-name", project + "-edge",
            "--project-directory", edge, "--env-file", "/dev/null", "-f", edge / "compose.edge.json", "up", "-d", "--wait", "--pull", "never", "--no-build"])
        self.edge_ready(edge)
        return edge

    def edge_ready(self, edge):
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
            urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=str(edge / "loopback.crt"))))
        with opener.open("https://127.0.0.1:13443/health/ready", timeout=20) as response:
            require(response.status == 200 and json.loads(response.read(16384))["status"] == "ok",
                    "retained TLS edge is not ready")

    def upgrade_command(self, action, directory, edge=None, *, history=None):
        command = [sys.executable, "-I", "-B", self.release / "payload/deploy/upgrade.py", action,
            "--directory", directory, "--release", self.release, "--verifier", self.verifier,
            "--public-keys", self.ring, "--source-package", directory / "release.tar.gz"]
        if edge:
            command += ["--layout", "ur_edge_pair", "--edge-directory", edge]
        if history:
            command += ["--historical-exits", history]
        return command

    def recovery_module(self):
        spec = importlib.util.spec_from_file_location("linux_recovery", self.release / "payload/deploy/recovery.py")
        recovery = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(recovery)
        return recovery

    def upgrade_fingerprint(self, directory, project, database="uniroute", *, running=False, projections=None):
        recovery = self.recovery_module()
        projections = dict(projections or {})
        if running:
            projections["runtime_generations"] = ("to_jsonb(t)-'lease_expires_at'-'revision'-'updated_at'", "")
        return recovery.table_fingerprints(lambda db, query: self.sql(directory, project, query, db),
                                           database, projections=projections)

    def protected_upgrade_rows(self, directory, project, source_sequence):
        # Reuse the existing full-row hash algorithm, independently of the
        # upgrader's receipt. User/password/epoch bytes are hashed inside PG;
        # no password hash or session token is returned or logged here.
        projections = {"audit_logs": ("to_jsonb(t)", " WHERE action IN ('user.password_reset','bootstrap.first_customer')"),
                       "release_history": ("to_jsonb(t)", " WHERE release_sequence<=" + str(source_sequence))}
        rows = self.upgrade_fingerprint(directory, project, projections=projections)
        return {table: rows[table] for table in ("users", "orgs", "projects", "wallets", "transactions",
            "project_members", "audit_logs", "release_history", "execution_content_objects", "execution_content_streams")}

    def protected_upgrade_files(self, directory, body, edge):
        roots = [directory / "runtime", directory / "admin-password", directory / "release.tar.gz",
                 directory / "evidence/install.json", body]
        if edge:
            roots.append(edge)
        inventory = self.recovery_module().private_inventory(roots)
        # The one native image override is deliberately new. Original bytes,
        # ownership, permissions, directories and links must remain identical.
        inventory.pop(str(directory / "runtime/upgrade-image.json").lstrip("/"), None)
        return inventory

    def wait_stopped_leases(self, directory):
        """Resume only the native, no-pending-operation lease wait boundary."""
        state = json.loads((directory / "install-state.json").read_bytes())
        witness = state["upgrade"]
        require(witness["pending"] is None and "retire-writers" not in witness["done"] and
            all("stop-" + role in witness["done"] for role in witness["writer_instances"]),
            "lease wait is not at the proved physical-stop boundary")
        for role in witness["writer_instances"]:
            proof = witness["receipts"]["stop-" + role]
            row = json.loads(self.run("read exact stopped lease owner", [self.docker, "inspect", proof["container_id"]]))[0]
            require(not row["State"]["Running"] and row["State"]["Pid"] == 0 and proof["old_processes_gone"],
                    "lease wait has an unproved live source process")
        delay = float(self.sql(directory, state["project"], "SELECT greatest(0,coalesce(max(extract(epoch FROM "
            "lease_expires_at-clock_timestamp())),0)) FROM runtime_generations WHERE state<>'stopped'"))
        require(0 <= delay <= 45, "unexpected native lease expiry; preserve fixture")
        if delay:
            time.sleep(delay + 0.05)

    def run_native_upgrade(self, directory, edge=None, history=None):
        code, output = self.run("run actual shipped upgrade", self.upgrade_command("upgrade", directory, edge, history=history), ok=False)
        if code:
            require("Upgrade stopped: A stopped writer's original native lease is still live; wait and read back before retiring" in output,
                    "native upgrade failed outside its known lease wait")
            self.wait_stopped_leases(directory)
            self.run("resume after proved native lease expiry", self.upgrade_command("resume-upgrade", directory, edge, history=history))

    def interrupt_upgrade(self, directory, edge, history):
        # Use the shipped owner's existing test hook. No migration, process,
        # reconciliation or SQL effect is replaced by a fake implementation.
        hook = self.root / "cold-pair-checkpoint.py"
        if not hook.exists():
            private_write(hook, b'''import importlib.util,sys
from pathlib import Path
program,directory,release,verifier,ring,edge,history=map(Path,sys.argv[1:])
spec=importlib.util.spec_from_file_location("shipped_upgrade",program)
owner=importlib.util.module_from_spec(spec); spec.loader.exec_module(owner)
operation=owner.Upgrade(directory,release,verifier,ring,directory/"release.tar.gz","ur_edge_pair",edge,historical_exits=history)
class Interrupted(Exception): pass
def checkpoint(point):
    if point=="effect:cold-pair": raise Interrupted()
operation.test_checkpoint=checkpoint
try: operation.execute(resume="upgrade" in operation.state)
except Interrupted: sys.exit(73)
except owner.install.InstallError as error:
    if str(error)=="A stopped writer's original native lease is still live; wait and read back before retiring": sys.exit(74)
    raise
raise SystemExit("expected cold-pair interruption was not reached")
''')
        code, _ = self.run("interrupt real upgrade after independently restored cold pair", [sys.executable, "-I", "-B",
            hook, self.release / "payload/deploy/upgrade.py", directory, self.release, self.verifier, self.ring, edge, history], ok=False)
        if code == 74:
            self.wait_stopped_leases(directory)
            code, _ = self.run("continue known lease boundary to real cold-pair interruption", [sys.executable, "-I", "-B",
                hook, self.release / "payload/deploy/upgrade.py", directory, self.release, self.verifier, self.ring, edge, history], ok=False)
        state = json.loads((directory / "install-state.json").read_bytes())
        witness = state["upgrade"]
        require(code == 73 and witness["pending"] == "cold-pair", "real cold-pair checkpoint was not reached")
        evidence = directory / "upgrades" / witness["operation_id"] / "backup"
        receipt = evidence / "receipt.json"
        retained = evidence / "receipt.retained-for-unknown-test.json"
        original = sha(receipt)
        receipt.rename(retained)
        before = files(directory), self.inventory(), self.upgrade_fingerprint(directory, state["project"])
        code, output = self.run("refuse unknown cold-pair outcome without repeating it",
            self.upgrade_command("resume-upgrade", directory, edge, history=history), ok=False)
        require(code != 0 and "Upgrade result is unconfirmed at cold-pair" in output,
                "unknown cold-pair outcome was not refused")
        require(before == (files(directory), self.inventory(), self.upgrade_fingerprint(directory, state["project"])),
                "unknown outcome refusal mutated source or retained evidence")
        require(sha(retained) == original and not receipt.exists(), "unknown outcome evidence changed")
        retained.rename(receipt)  # Recover the exact task-owned receipt, never fabricate or overwrite it.
        self.run("resume real independently proved cold pair", self.upgrade_command("resume-upgrade", directory, edge, history=history))

    def historical_observations(self, old, directory, project, edge):
        """Real native stop/restart or replacement, before the upgrade claim."""
        spec = importlib.util.spec_from_file_location("shipped_history_contract", self.release / "payload/deploy/upgrade.py")
        owner = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(owner)
        folder = directory / "evidence/historical-observations"
        folder.mkdir(mode=0o700)
        def reference(name, value, pointer=""):
            path = folder / name
            private_write(path, value if isinstance(value, bytes) else owner.canonical(value))
            return {"path": str(path), "sha256": sha(path), "pointer": pointer}
        def inspect(identifier):
            fmt = '{"Id":{{json .Id}},"Image":{{json .Image}},"State":{{json .State}}}'
            return json.loads(self.run("read original history resource identity", [self.docker, "inspect", "--format", fmt, identifier]))
        container = inspect(project + "-backend-1")
        ready = json.loads(self.run("read original history runtime identity", [self.docker, "exec", container["Id"],
            "wget", "-qO-", "http://127.0.0.1:8080/health/ready"]))["runtime"]
        require(ready["artifact_digest"] == container["Image"] and re.fullmatch(r"uri_[a-f0-9]{32}", ready["instance"]),
                "history runtime differs from the actual original resource")
        installation = json.loads((directory / "install-state.json").read_bytes())
        before = {"installation_id": installation["installation_id"], "instance_id": ready["instance"],
                  "container_id": container["Id"], "image": container["Image"]}
        before_ref = reference("before.json", before)
        record = {"kind": "native_replacement" if edge else "process_exit", "service": "backend",
            "before": {key: dict(before_ref, pointer="/" + key) for key in before}, "program": None}
        compose = old.compose(directory, project)
        if edge:
            compose += ["-f", edge / "runtime-overrides.json"]
        if edge:
            command = compose + ["up", "-d", "--wait", "--force-recreate", "--no-deps", "--pull", "never", "--no-build", "backend"]
        else:
            pids = self.run("retain original native history process inventory", [self.docker, "top", container["Id"], "-eo", "pid"]).splitlines()[1:]
            processes = {row.strip(): owner.Upgrade._process_start(int(row.strip())) for row in pids}
            require(processes and all(value is not None for value in processes.values()), "original process observation missing")
            record["processes"] = reference("processes.json", processes)
            command = compose + ["stop", "backend"]
        code, output = self.run("perform exact native source history operation", command, ok=False)
        require(code == 0, "native source history operation failed")
        record["command"] = reference("history.process.json", {"returncode": code, "argv": [str(value) for value in command]})
        if edge:
            record["output"] = reference("history.stderr", output.encode(), None)
            successor = inspect(project + "-backend-1")
            new_ready = json.loads(self.run("read actual replacement successor runtime", [self.docker, "exec", successor["Id"],
                "wget", "-qO-", "http://127.0.0.1:8080/health/ready"]))["runtime"]
            successor_ref = reference("successor.json", dict(before, instance_id=new_ready["instance"],
                container_id=successor["Id"], image=successor["Image"]))
            record["successor"] = {key: dict(successor_ref, pointer="/" + key) for key in before}
            require(successor["Id"] != container["Id"] and new_ready["instance"] != ready["instance"],
                    "native replacement did not create a distinct resource/runtime")
        else:
            after = inspect(container["Id"])
            require(not after["State"]["Running"] and after["State"]["Pid"] == 0 and after["State"]["ExitCode"] == 0
                and not after["State"]["OOMKilled"] and all(owner.Upgrade._process_start(int(pid)) != start for pid, start in processes.items()),
                "actual native history clean stop is unproved")
            record["after"] = reference("after.json", after)
            self.run("restart exact retained source after proved process exit", compose +
                ["up", "-d", "--wait", "--no-deps", "--pull", "never", "--no-build", "backend"])
        original = json.loads(self.sql(directory, project, "SELECT to_jsonb(g)||jsonb_build_object('manifest_digest',m.manifest_digest) "
            "FROM runtime_generations g JOIN runtime_support_manifests m USING(artifact_digest) WHERE instance_id='" + ready["instance"] + "'"))
        delay = float(self.sql(directory, project, "SELECT greatest(0,extract(epoch FROM lease_expires_at-clock_timestamp())) "
            "FROM runtime_generations WHERE instance_id='" + ready["instance"] + "'"))
        require(0 <= delay <= 45, "unexpected original history lease bound")
        if delay:
            time.sleep(delay + 0.05)
        record["generation"] = {key: original[key] for key in owner._GENERATION_FIELDS}
        target = self.target(directory, project)
        release = json.loads(self.sql(directory, project, "SELECT to_jsonb(s) FROM release_state s WHERE singleton"))
        document = {"format": "unirouter.historical-exits.v1", "installation_id": installation["installation_id"], "project": project,
            "database_system_id": target["system_identifier"], "database": target["database"], "database_oid": target["database_oid"],
            "source_sequence": release["release_sequence"], "source_package_sha256": release["package_sha256"], "records": [record]}
        path = folder / "historical-exits.json"
        private_write(path, owner.canonical(document))
        return path

    def qualify_upgrade(self, old, layout):
        project = "ur-upgrade-" + layout + "-" + self.suffix
        directory = old.instances / layout
        old.project, old.directory = project, directory
        old.run("install exact retained M127 predecessor", old.command("install", directory, project))
        old.installed_checks(directory, project)
        old.native_first_use_checks(directory, project, restart=False)
        body, name, rendered = self.historical_data(old, directory, project)
        edge = self.named_edge(old, directory, project, rendered) if layout == "edge" else None
        history = self.historical_observations(old, directory, project, edge)
        # Let the native audit worker finish its bounded synthetic login work
        # before a no-write comparison; heartbeat timing alone is projected out.
        deadline = time.monotonic() + 60
        while old.sql(directory, project, "SELECT count(*) FROM internal_outbox_events WHERE payload->>'action'='login'") != "0":
            require(time.monotonic() < deadline, "retained login audit did not materialize")
            time.sleep(1)
        protected_rows = self.protected_upgrade_rows(directory, project, old.release_sequence)
        protected_files = self.protected_upgrade_files(directory, body, edge)
        require(all(protected_rows[table]["count"] > 0 for table in
            ("users", "orgs", "projects", "wallets", "transactions", "audit_logs", "release_history", "execution_content_objects")),
            "source preservation projection lacks nonempty account/business/financial/body facts")
        before = files(directory), self.inventory(), self.upgrade_fingerprint(directory, project, running=True)
        code, output = self.run("refuse original history without its observations", self.upgrade_command("check-upgrade", directory, edge), ok=False)
        require(code != 0 and "unaccounted or missing writer" in output, "history without evidence was admitted")
        checked = json.loads(self.run("check actual M127 to M128 without effects", self.upgrade_command("check-upgrade", directory, edge, history=history)))
        status = json.loads(self.run("read actual not-started upgrade status", self.upgrade_command("upgrade-status", directory, edge, history=history)))
        require(checked["writes"] == 0 and checked["writers"] == (2 if edge else 1) and status["state"] == "not_started",
                "native finite upgrade preflight differs")
        require(before == (files(directory), self.inventory(), self.upgrade_fingerprint(directory, project, running=True)),
                "read-only upgrade check/status changed source state")
        require(self.protected_upgrade_files(directory, body, edge) == protected_files,
                "read-only upgrade check/status changed protected body or configuration")
        if edge:
            self.interrupt_upgrade(directory, edge, history)
        else:
            self.run_native_upgrade(directory, history=history)
        self.run("read back completed native upgrade resume", self.upgrade_command("resume-upgrade", directory, edge, history=history))
        state = json.loads((directory / "install-state.json").read_bytes())
        witness = state["upgrade"]
        require(witness["state"] == "completed" and witness["pending"] is None, "finite upgrade remains incomplete")
        require(self.protected_upgrade_rows(directory, project, old.release_sequence) == protected_rows and
            self.protected_upgrade_files(directory, body, edge) == protected_files,
            "independent original account/financial/audit/configuration projection changed")
        require(tuple(json.loads(self.sql(directory, project, "SELECT jsonb_agg(version ORDER BY version) FROM schema_migrations")))
                == self.source_contract.ledger, "upgraded complete ledger differs from candidate")
        backup = directory / "upgrades" / witness["operation_id"] / "backup"
        pair = json.loads((backup / "receipt.json").read_bytes())
        require(pair["verification_database"] != "uniroute"
            and self.upgrade_fingerprint(directory, project, pair["verification_database"]) == pair["restored_tables"],
            "physically separate restored database differs from original receipt")
        require(all(pair["tables"][table]["count"] > 0 for table in
            ("orgs", "projects", "users", "wallets", "transactions", "audit_logs", "release_history", "execution_content_objects")),
            "preservation fixture lacks nonempty business/financial/account/body history")
        require(sha(body / "objects" / name) == name and sha(backup / "restore-files" / str(body / "objects" / name).lstrip("/")) == name,
                "restored nonempty body bytes differ")
        roles = ("backend", "backend-canary") if edge else ("backend",)
        require(all(witness["receipts"]["stop-" + role]["clean_exit"] and
                    witness["receipts"]["stop-" + role]["old_processes_gone"] for role in (*roles, "frontend", *(("edge",) if edge else ()))),
                "original writer/receiver process exits are unproved")
        self.request("http://127.0.0.1:13000/health/ready")
        login = json.loads(self.request("http://127.0.0.1:13000/api/v1/auth/login", data=json.dumps(
            {"username": "admin", "password": "replacement-password-456"}).encode()))
        require(login.get("organization_role") == "platform_admin", "retained chosen password does not work after upgrade")
        require(self.sql(directory, project, "SELECT count(*) FROM transactions WHERE request_id='synthetic-linux-history'") == "1"
            and self.sql(directory, project, "SELECT count(*) FROM audit_logs WHERE action='bootstrap.first_customer'") == "1"
            and self.sql(directory, project, "SELECT count(*) FROM release_history") == "2", "retained business or release history differs")
        require(self.protected_upgrade_rows(directory, project, old.release_sequence) == protected_rows and
            self.protected_upgrade_files(directory, body, edge) == protected_files,
            "post-upgrade login changed original protected facts")
        if edge:
            self.edge_ready(edge)
        self.result.setdefault("finite_upgrades", []).append({"layout": "ur_edge_pair" if edge else "community_single",
            "source_schema": old.schema, "target_schema": self.schema, "writers": len(roles),
            "full_table_count": len(pair["tables"]), "nonempty_history_preserved": True,
            "independent_original_row_and_configuration_equality": True,
            "independent_database_and_body_restore": True, "read_only_no_effects": True,
            "physical_exits_proved": True, "completed_resume": True, "known_checkpoint_resume": bool(edge),
            "historical_kind": "native_replacement" if edge else "process_exit", "history_without_evidence_refused": True,
            "unknown_outcome_refused": bool(edge), "provider_calls": 0})
        self.passed("real " + layout + " M127 to M128 upgrade, paired restore and preserved first use")
        # Serialize cases, releasing only this exact fixture's receivers/DBs.
        if edge:
            self.run("stop exact completed edge fixture", [self.docker, "compose", "--project-name", project + "-edge",
                "--project-directory", edge, "--env-file", "/dev/null", "-f", edge / "compose.edge.json", "stop"])
        compose = old.compose(directory, project)
        if edge:
            compose += ["-f", edge / "runtime-overrides.json"]
        self.run("stop exact completed upgraded fixture", compose + ["-f", directory / "runtime/upgrade-image.json", "stop"])

    def execute(self):
        self.preflight()
        self.prepare()
        project = "ur-native-" + self.suffix
        directory = self.instances / "interrupted"
        self.project, self.directory = project, directory
        self.negative_checks(directory, project)
        self.interrupt_after_plan(directory, project)
        self.installed_checks(directory, project)
        self.native_first_use_checks(directory, project)
        self.run("stop exact completed fresh fixture", self.compose(directory, project) + ["stop"])
        require(self.schema == CURRENT_SCHEMA, "current Linux qualification requires the M128 candidate")
        old = self.retained_release()
        for layout in ("single", "edge"):
            self.qualify_upgrade(old, layout)
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
