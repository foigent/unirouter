#!/usr/bin/env python3
"""Real M125→M126→M127 maintenance acceptance on one disposable GitHub runner.

Existing Artifact, migration, ingress and restore owners perform all mutations.
Only authored assertions and immutable identities leave the synthetic fixture.
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
import secrets
import shutil
import socket
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

sys.dont_write_bytecode = True
OLD_COMMIT = "59c47b852b7372951c5c49b0e6b23df8c83817b9"
OLD_HEAD = "000125_xinsight_control_credential_lifecycle"
RETIREMENT_COMMIT = "55983609c9a4f37072c4eeb2427364841bc1228b"
RETIREMENT_HEAD = "000126_commercial_license_retirement"
NEW_HEAD = "000127_xinsight_communication_policy"
TRIGGERS = {name + "_commercial_limit" for name in
            ("api_keys", "users", "projects", "access_credentials", "provider_connections")}


class Refused(Exception):
    """Harness-authored, non-secret messages only."""


def require(condition, message):
    if not condition:
        raise Refused(message)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def private_write(path, value):
    with path.open("xb") as file:
        os.chmod(path, 0o600)
        file.write(value.encode() if isinstance(value, str) else value)


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


class Acceptance:
    def __init__(self, checkout, root, expected_commit):
        self.root, self.repo, self.phase = root, checkout, "preflight"
        self.logs = root / "logs"
        self.logs.mkdir(mode=0o700)
        self.counter = 0
        self.env = {k: os.environ[k] for k in ("PATH", "HOME", "LANG", "LC_ALL") if k in os.environ}
        self.env["DOCKER_HOST"] = "unix:///var/run/docker.sock"
        self.result = {"schema": "unirouter.maintenance-native-acceptance.v1",
                       "status": "running", "checks": [], "provider_calls": 0,
                       "synthetic_signing_only": True, "platform": "linux/amd64"}
        require(re.fullmatch(r"[a-f0-9]{40}", expected_commit), "invalid candidate identity")
        git = ["git", "-c", "safe.directory=" + str(checkout)]
        require(self.run("read exact checkout", git + ["rev-parse", "HEAD"]).strip() == expected_commit,
                "checkout differs from expected commit")
        self.sources = {}
        for name, commit in (("old", OLD_COMMIT), ("new", RETIREMENT_COMMIT),
                             ("successor", expected_commit)):
            source = root / (name + "-source")
            self.run("clone exact " + name + " source", git + ["clone", "--shared", "--no-checkout", checkout, source])
            self.run("checkout exact " + name + " source", ["git", "-C", source, "checkout", "--detach", commit])
            tree = self.run("read " + name + " tree", ["git", "-C", source, "rev-parse", "HEAD^{tree}"]).strip()
            self.sources[name] = {"directory": str(source), "commit": commit, "tree": tree,
                                  "version": (source / "VERSION").read_text().strip()}
        self.repo = Path(self.sources["new"]["directory"])
        self.control = module("maintenance_owner", self.repo / "scripts/runtime-generation-retire.py")
        self.rollout = self.control.rollout
        self.ingress = module("fixture_ingress", self.repo / "scripts/runtime-ingress.py")
        self.result["sources"] = {k: {f: v[f] for f in ("commit", "tree", "version")} for k, v in self.sources.items()}
        self.project = "ur-maintenance-native"
        self.images = {}
        self.image_ids = {}
        self.env_file = root / "fixture.env"
        self.tls_overlay = root / "tls.yml"
        self.old_overlay, self.new_overlay = root / "old-runtime.yml", root / "new-runtime.yml"
        self.membership = root / "runtime-membership"
        self.membership.mkdir(mode=0o700)
        self.fresh = root / "fresh-evidence"
        self.fresh.mkdir(mode=0o700)
        self.evidence = root / "maintenance-evidence"
        self.input = root / "maintenance.json"
        self.old_head, self.new_head = OLD_HEAD, RETIREMENT_HEAD
        self.stage = "m125-to-m126"
        self.restore_database = "ur-maintenance-restore-postgres"
        self.restore_backend = "ur-maintenance-restore-backend"
        self.successor_overlay = root / "successor-runtime.yml"
        self.result["transitions"] = []

    def run(self, label, command, *, data=None, env=None, ok=True, timeout=1800):
        self.phase = label
        print("Maintenance acceptance: " + label, flush=True)
        log = self.logs / (str(self.counter) + ".log")
        self.counter += 1
        with log.open("xb") as file:
            os.chmod(log, 0o600)
            result = subprocess.run([str(x) for x in command], input=data, stdout=file,
                                    stderr=subprocess.STDOUT, cwd=self.repo,
                                    env=env or self.env, timeout=timeout)
        output = log.read_text(errors="replace")
        if ok:
            if result.returncode != 0:
                prefixes = ("Failed to load config:", "Failed to initialize HTTP runtime:",
                            "Failed to load release trust:", "Command failed:",
                            "Schema authority startup gate failed:", "Model route authority startup gate failed:")
                self.result["command_failure"] = {"exit_code": result.returncode,
                    "fatal_categories": [prefix[:-1] for prefix in prefixes if prefix in output]}
            require(result.returncode == 0, label + " failed")
            return output
        return result.returncode, output

    def passed(self, label):
        self.result["checks"].append(label)
        print("PASS: " + label, flush=True)

    def inspect(self, name):
        return json.loads(self.run("inspect fixture identity", ["docker", "inspect", name]))[0]

    def prepare_runner_storage(self):
        # These preinstalled packages belong to this disposable job, not a
        # customer host. Use their existing owner; never delete SDK directories.
        require(os.environ.get("GITHUB_ACTIONS") == "true"
                and os.environ.get("RUNNER_ENVIRONMENT") == "github-hosted"
                and platform.system() == "Linux" and platform.machine() == "x86_64"
                and os.geteuid() == 0, "storage preparation requires the disposable runner")
        self.phase = "prepare disposable runner storage"
        sdk = Path("/usr/local/lib/android/sdk")
        manager = sdk / "cmdline-tools/latest/bin/sdkmanager"
        require(manager.is_file() and os.access(manager, os.X_OK)
                and manager.resolve().is_relative_to(sdk),
                "preinstalled Android package manager unavailable")
        packages = []
        for version in ("27.3.13750724", "29.0.14206865", "30.0.16248370"):
            directory = sdk / "ndk" / version
            if not directory.exists():
                continue
            require(directory.is_dir() and directory.resolve() == directory,
                    "unexpected NDK package path")
            properties = (directory / "source.properties").read_text()
            require(re.search(r"^Pkg.Revision\s*=\s*" + re.escape(version) + r"\s*$",
                              properties, re.MULTILINE), "NDK package identity differs")
            packages.append("ndk;" + version)
        before = shutil.disk_usage("/var/lib/docker")
        self.result["runner_storage_preparation"] = {
            "uninstalled_packages": [], "free_bytes_before": before.free,
            "total_bytes": before.total}
        if packages:
            self.run("uninstall unused disposable-runner NDK packages",
                     [manager, "--sdk_root=" + str(sdk), "--uninstall", *packages])
            require(all(not (sdk / "ndk" / package.split(";")[1]).exists()
                        for package in packages), "NDK uninstall did not complete")
            self.result["runner_storage_preparation"]["uninstalled_packages"] = packages
        self.result["runner_storage_preparation"]["free_bytes_after"] = shutil.disk_usage("/var/lib/docker").free

    def preflight(self):
        self.result["tools"] = self.rollout.environment_check()
        self.prepare_runner_storage()
        code, output = self.run("formal storage preflight before builds", [self.repo / "scripts/docker-storage-preflight.sh",
                 "--path", "/var/lib/docker", "--minimum-free-gib", "8", "--maximum-use-percent", "80"], ok=False)
        storage = {"exit_code": code}
        values = re.search(r"used_percent=(\d+) available_gib=([0-9.]+) minimum_free_gib=(\d+) maximum_use_percent=(\d+)", output)
        if values:
            storage.update(used_percent=int(values[1]), available_gib=float(values[2]),
                           minimum_free_gib=int(values[3]), maximum_use_percent=int(values[4]))
        for reason, message in (("missing_path", "path does not exist:"),
                                ("usage_threshold", "filesystem use exceeds the approved threshold"),
                                ("free_space_threshold", "available space is below the approved threshold")):
            if message in output:
                storage["refusal"] = reason
        self.result["storage"] = storage
        require(code == 0, "formal storage preflight before builds failed")
        require(self.run("refuse existing fixture project", ["docker", "ps", "-aq", "--filter",
                    "label=com.docker.compose.project=" + self.project]).strip() == "", "fixture project exists")
        for name in ("uniroute-api", "uniroute-api-canary", "uniroute-frontend", "uniroute-nginx", "uniroute-postgres", "uniroute-redis", "ur-maintenance-restore-postgres", "ur-maintenance-restore-backend",
                     "ur-maintenance-restore-postgres-m126", "ur-maintenance-restore-backend-m126"):
            require(self.rollout.inspect(name, missing_ok=True) is None, "fixture container name exists")
        for port in (8080, 8081, 13443):
            with socket.socket() as probe:
                probe.bind(("127.0.0.1", port))
        self.passed("formal host, storage and fixture prerequisites")

    def build(self):
        usage = self.run("verify initially empty disposable build cache",
                         ["docker", "system", "df", "--format", '{{if eq .Type "Build Cache"}}{{.TotalCount}}{{end}}'])
        require(usage.strip() == "0", "runner contains preexisting build cache")
        self.result["initial_build_cache_count"] = 0
        keygen = self.root / "uniroute-license"
        self.run("build existing key and License tool", ["go", "build", "-buildvcs=false", "-o", keygen, "./cmd/uniroute-license"])
        rings = {}
        for name in ("commercial", "upgrade"):
            public, private = self.root / (name + ".public.json"), self.root / (name + ".private")
            self.run("create synthetic " + name + " key", [keygen, "keygen", "--key-id", "maintenance-" + name,
                     "--private-out", private, "--public-out", public])
            rings[name] = base64.urlsafe_b64encode(public.read_bytes()).decode().rstrip("=")
        self.keygen = keygen
        for name in ("old", "new", "successor"):
            identity = self.sources[name]
            tag = "unirouter-maintenance/backend:" + name
            args = ["docker", "build", "-t", tag, "--label", "org.opencontainers.image.revision=" + identity["commit"],
                    "--label", "org.opencontainers.image.version=" + identity["version"], "--label", "io.unirouter.component=backend"]
            build_args = {"RUNTIME_VERSION": identity["version"], "RUNTIME_COMMIT": identity["commit"],
                          "RUNTIME_TREE": identity["tree"], "UNIROUTER_RELEASE_TRUST_MODE": "release",
                          "UNIROUTER_UPGRADE_TRUST_BUNDLE_B64": rings["upgrade"]}
            if name == "old":
                build_args["UNIROUTER_LICENSE_TRUST_BUNDLE_B64"] = rings["commercial"]
            for key, value in build_args.items():
                args += ["--build-arg", key + "=" + value]
            self.run("build exact " + name + " release backend", args + [identity["directory"]])
            self.images[name] = tag
        identity = self.sources["successor"]
        self.images["frontend"] = "unirouter-maintenance/frontend:new"
        self.run("build exact release frontend", ["docker", "build", "-t", self.images["frontend"],
                 "--label", "org.opencontainers.image.revision=" + identity["commit"],
                 "--label", "org.opencontainers.image.version=" + identity["version"],
                 "--label", "io.unirouter.component=frontend", "--build-arg", "VITE_DEPLOY_ENV=production",
                 "--build-arg", "VITE_APP_VERSION=" + identity["version"], Path(identity["directory"]) / "frontend"])
        for name, tag in (("postgres", "postgres:16-alpine"), ("redis", "redis:7-alpine"), ("nginx", "nginx:1.30.3-alpine")):
            self.run("pull fixture " + name, ["docker", "pull", tag])
            self.images[name] = tag
        self.result["images"] = {}
        for name, tag in self.images.items():
            row = json.loads(self.run("bind fixture image", ["docker", "image", "inspect", tag]))[0]
            require(row["Os"] == "linux" and row["Architecture"] == "amd64", "fixture image platform differs")
            self.image_ids[name] = row["Id"]
            self.result["images"][name] = {"id": row["Id"], "repo_digests": row.get("RepoDigests", [])}
        # The initial cache was empty on this job's exclusive disposable VM.
        # Reclaim only build cache produced above; keep the tagged final images.
        self.run("reclaim this job's unused build cache", ["docker", "builder", "prune", "--all", "--force"])
        for name, tag in self.images.items():
            require(self.inspect(tag)["Id"] == self.image_ids[name], "cache cleanup changed a fixture image")
        self.passed("exact old and new release-mode artifacts built with synthetic trust")

    def configuration(self):
        self.password, self.admin_password = secrets.token_hex(24), secrets.token_hex(24)
        self.settings = {"POSTGRES_PASSWORD": self.password, "JWT_SECRET": secrets.token_hex(32),
                         "ENCRYPTION_KEY": secrets.token_hex(16), "API_KEY_HASH_SECRET": secrets.token_hex(32),
                         "GRAFANA_ADMIN_PASSWORD": secrets.token_hex(24),
                         "UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET": secrets.token_hex(32),
                         "RUNTIME_ENVIRONMENT": "test", "RUNTIME_DEPLOYMENT_MODE": "docker-compose",
                         "MODEL_ROUTE_AUTHORITY": "legacy", "MODEL_ROUTE_COMPATIBILITY_ENABLED": "true"}
        private_write(self.env_file, "".join(k + "=" + v + "\n" for k, v in self.settings.items()))
        self.cert = self.root / "loopback.crt"
        self.run("create loopback-only TLS fixture", ["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
                 "-keyout", self.root / "loopback.key", "-out", self.cert, "-days", "1", "-subj", "/CN=127.0.0.1",
                 "-addext", "subjectAltName=IP:127.0.0.1"])
        os.chmod(self.root / "loopback.key", 0o600)
        self.ssl_context = ssl.create_default_context(cafile=str(self.cert))
        self.origin = "https://127.0.0.1:13443"
        self.membership.joinpath("upstreams.conf").write_bytes(self.ingress.membership("both"))
        mounts = [str(self.repo / "nginx/nginx.conf") + ":/etc/nginx/conf.d/default.conf:ro",
                  str(self.membership) + ":/etc/nginx/runtime-membership:ro",
                  str(self.cert) + ":/etc/nginx/ssl/fullchain.pem:ro",
                  str(self.root / "loopback.key") + ":/etc/nginx/ssl/privkey.pem:ro"]
        private_write(self.tls_overlay, "services:\n  nginx:\n    ports: !override ['127.0.0.1:13443:443']\n    volumes: !override " + json.dumps(mounts) + "\n")
        for name, overlay in (("old", self.old_overlay), ("new", self.new_overlay),
                              ("successor", self.successor_overlay)):
            identity = self.sources[name]
            common = {"DB_DSN": "postgres://uniroute:" + self.password + "@postgres:5432/uniroute?sslmode=disable",
                      "SKIP_MIGRATION": "true", "RUNTIME_ENVIRONMENT": "test", "RUNTIME_VERSION": identity["version"],
                      "RUNTIME_COMMIT": identity["commit"], "RUNTIME_TREE": identity["tree"],
                      "RUNTIME_IMAGE_DIGEST": self.image_ids[name], "LICENSE_DEPLOYMENT_ID": "maintenance-native",
                      "XINSIGHT_CONNECTOR_AGENT_ENABLED": "false", "COMPLETED_EXECUTION_FEED_ENABLED": "false",
                      "SOURCE_FACTS_ENABLED": "false", "SOURCE_OBSERVATIONS_ENABLED": "false", "SOURCE_BUDGET_SNAPSHOTS_ENABLED": "false",
                      "SOURCE_CONTENT_ENABLED": "false", "ASYNC_MEDIA_ENABLED": "false"}
            wire = "services:\n  postgres:\n    ports: !override []\n    environment:\n      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}\n  redis:\n    ports: !override []\n  prometheus:\n    ports: !override []\n  grafana:\n    ports: !override []\n  frontend:\n    ports: !override []\n"
            for service, port in (("backend", 8080), ("backend-canary", 8081)):
                wire += "  " + service + ":\n    restart: 'no'\n    ports: !override ['127.0.0.1:" + str(port) + ":8080']\n    environment: " + json.dumps(common) + "\n"
                wire += "    volumes:\n      - " + json.dumps(str(self.fresh) + ":/evidence") + "\n"
            private_write(overlay, wire)

    def compose(self, name):
        source = Path(self.sources[name]["directory"])
        files = [source / "docker-compose.yml", source / "docker-compose.canary.yml",
                 source / "deploy/internal-target-images.override.yml", self.tls_overlay,
                 self.old_overlay if name == "old" else self.new_overlay]
        command = ["docker", "compose", "--project-name", self.project, "--env-file", self.env_file]
        for path in files:
            command += ["-f", path]
        return command

    def compose_env(self, name):
        return dict(self.env, UNIROUTER_BACKEND_IMAGE=self.images[name], UNIROUTER_FRONTEND_IMAGE=self.images["frontend"])

    def backend(self, name, args, *, label, overrides=None, data=None, ok=True):
        command = self.compose(name) + ["run", "--rm", "--no-deps", "--pull", "never", "-T"]
        for key, value in (overrides or {}).items():
            command += ["-e", key + "=" + value]
        return self.run(label, command + ["backend", "./uniroute", *args], env=self.compose_env(name), data=data, ok=ok)

    def sql(self, query, *, restored=False):
        name = self.restore_database if restored else "uniroute-postgres"
        return self.run("read fixture database facts", ["docker", "exec", name, "psql", "-X", "-At", "-U", "uniroute", "-d", "uniroute",
                        "-v", "ON_ERROR_STOP=1", "-c", query]).strip()

    def table(self, name, *, restored=False, heartbeat=False, where=""):
        value = "to_jsonb(t)"
        if heartbeat:
            value += " - 'lease_expires_at' - 'revision' - 'updated_at'"
        return json.loads(self.sql("SELECT COALESCE(jsonb_agg(row ORDER BY row::text),'[]'::jsonb) FROM (SELECT " + value + " AS row FROM " + name + " t " + where + ") s", restored=restored))

    def facts(self, *, restored=False, heartbeat=False):
        names = ("schema_migrations", "runtime_change_releases", "runtime_change_slots", "capability_publications",
                 "runtime_ingress_changes", "commercial_license_entitlements", "settings", "audit_logs", "execution_source_budget_coverage")
        facts = {name: self.table(name, restored=restored) for name in names}
        facts["runtime_generations"] = self.table("runtime_generations", restored=restored, heartbeat=heartbeat)
        return facts

    def request(self, path, *, data=None, token=None, method=None):
        headers = {"Content-Type": "application/json"}
        if token:
            headers["Authorization"] = "Bearer " + token
        request = urllib.request.Request(self.origin + path, data=json.dumps(data).encode() if data is not None else None,
                                         headers=headers, method=method)
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPSHandler(context=self.ssl_context))
        with opener.open(request, timeout=20) as response:
            require(response.status == 200, "fixture HTTP response failed")
            return json.loads(response.read())

    def wait_ready(self, *, restored=False):
        names = (self.restore_backend,) if restored else ("uniroute-api", "uniroute-api-canary")
        for name in names:
            deadline = time.monotonic() + 120
            while True:
                code, output = self.run("wait for exact backend readiness", ["docker", "exec", name, "wget", "-qO-", "-T", "3", "http://127.0.0.1:8080/health/ready"], ok=False, timeout=10)
                if code == 0:
                    value = json.loads(output)
                    require(value["runtime"]["state"] == "serving", "backend is not serving")
                    break
                require(time.monotonic() < deadline, "backend readiness deadline")
                time.sleep(1)

    def join(self, change_id):
        args = [sys.executable, "-B", self.repo / "scripts/runtime-ingress.py", "--directory", self.membership,
                "--expected-container-id", self.nginx_id, "--desired", "both", "--change-id", change_id]
        planned = json.loads(self.run("plan actual ingress", args))
        before = self.ingress.workers("uniroute-nginx")
        result = json.loads(self.run("apply actual ingress", args + ["--apply", "--expected-config-sha256", planned["before_sha256"],
                                "--expected-nginx-sha256", planned["nginx_sha256"]]))
        require(result["outcome"] == "old_proxy_workers_exited" and not before.intersection(self.ingress.workers("uniroute-nginx")),
                "old NGINX worker exit unproved")

    def establish_m125(self):
        config = json.loads(self.run("validate old Compose input", self.compose("old") + ["config", "--format", "json"], env=self.compose_env("old")))
        for service in config["services"].values():
            for port in service.get("ports", []):
                require(port.get("host_ip") == "127.0.0.1", "fixture exposes a non-loopback port")
        for name in ("old", "new"):
            self.backend(name, ["runtime-generation", "support"], label="validate " + name + " release configuration before database startup")
        self.passed("old and new release configuration admitted before database startup")
        self.run("start empty fixture databases", self.compose("old") + ["up", "-d", "--wait", "--pull", "never", "--no-build", "postgres", "redis"], env=self.compose_env("old"))
        self.backend("old", ["fresh-install", "plan", "--out", "/evidence/install.json"], label="plan actual M125 fresh owner")
        self.backend("old", ["fresh-install", "apply", "--plan", "/evidence/install.json", "--evidence-dir", "/evidence/publications"], label="apply actual M125 fresh owner")
        self.backend("old", ["admin", "set-password", "--username", "admin", "--org-id", "1", "--password-stdin"],
                     label="rotate fixture administrator once", data=(self.admin_password + "\n").encode())
        self.run("start original M125 receivers and NGINX", self.compose("old") + ["up", "-d", "--no-deps", "--pull", "never", "--no-build", "backend", "backend-canary", "frontend", "nginx"], env=self.compose_env("old"))
        self.wait_ready()
        self.nginx_id = self.inspect("uniroute-nginx")["Id"]
        self.join("maintenance-native-initial-both")
        require(self.sql("SELECT count(*) FROM audit_logs WHERE action='login'") == "0"
                and self.sql("SELECT count(*) FROM internal_outbox_events WHERE payload->>'action'='login'") == "0",
                "fixture already has login audit activity")
        login = self.request("/api/v1/auth/login", data={"username": "admin", "password": self.admin_password})
        token = login["token"]
        user_id = login["user_id"]
        require(type(user_id) is int and user_id > 0, "login returned an invalid user identity")
        # Login intentionally uses a background audit context, so it has no
        # HTTP request ID. This fresh fixture has exactly one login producer.
        deadline = time.monotonic() + 60
        while True:
            settled = self.sql("SELECT (SELECT count(*) FROM audit_logs WHERE action='login' AND resource='user' "
                               "AND user_id=" + str(user_id) + " AND resource_id='" + str(user_id) + "') = 1 "
                               "AND NOT EXISTS (SELECT 1 FROM internal_outbox_events WHERE payload->>'action'='login')")
            if settled == "t":
                break
            require(time.monotonic() < deadline, "initial login audit materialization deadline")
            time.sleep(1)
        self.passed("initial login audit materialized and its outbox completed before backup baseline")
        now = datetime.now(timezone.utc)
        stamp = lambda value: value.isoformat(timespec="seconds").replace("+00:00", "Z")
        payload = {"license_id": "maintenance-native-history", "customer": "Synthetic maintenance fixture", "deployment_id": "maintenance-native",
                   "tier": "enterprise", "issued_at": stamp(now - timedelta(minutes=2)), "not_before": stamp(now - timedelta(minutes=1)),
                   "expires_at": stamp(now + timedelta(hours=4)), "features": ["gateway"]}
        payload_path, document = self.root / "license-payload.json", self.root / "license-document.json"
        private_write(payload_path, json.dumps(payload))
        self.run("issue synthetic historical License", [self.keygen, "issue", "--key-id", "maintenance-commercial", "--private-key", self.root / "commercial.private",
                 "--payload", payload_path, "--out", document])
        status = self.request("/api/v1/license", data={"license": json.loads(document.read_text())}, token=token, method="PUT")
        require(status.get("enforceable") is True and status.get("valid") is True, "old License import was not accepted")
        original = self.facts(heartbeat=True)
        require(original["commercial_license_entitlements"] and any(x["key"] == "license.offline" for x in original["settings"])
                and any(x.get("action") == "license.update" for x in original["audit_logs"]), "nonempty historical License evidence missing")
        self.old_ledger = sorted(p.name[:-7] for p in (Path(self.sources["old"]["directory"]) / "migrations").glob("*.up.sql"))
        require(self.old_ledger[-1] == OLD_HEAD and json.loads(self.sql("SELECT jsonb_agg(version ORDER BY version) FROM schema_migrations")) == self.old_ledger, "complete M125 ledger differs")
        require(self.trigger_names() == TRIGGERS, "M125 commercial trigger fixture differs")
        self.passed("real M125 fleet, applied ingress and nonempty License history")

    def trigger_names(self):
        return set(json.loads(self.sql("SELECT COALESCE(jsonb_agg(tgname ORDER BY tgname),'[]'::jsonb) FROM pg_trigger WHERE NOT tgisinternal AND tgname LIKE '%_commercial_limit'")))

    def maintenance(self, action, role=None, *, ok=True):
        args = [sys.executable, "-B", self.repo / "scripts/runtime-generation-retire.py", action, "--maintenance", "--rollout-input", self.input]
        if role:
            args += ["--role", role]
        result = self.run("maintenance " + action + (" " + role if role else ""), args, ok=ok, timeout=2200)
        return json.loads(result) if ok else result

    def retire_and_migrate(self):
        spec = {"format": "unirouter.internal-target-rollout.v1", "operation_id": "maintenance-native-" + self.stage,
                "target": {"hostname": socket.gethostname(), "database_system_id": self.sql("SELECT system_identifier FROM pg_control_system()"),
                           "environment": "test", "project": self.project, "env_file": str(self.env_file), "overlay": str(self.old_overlay),
                           "tls_overlay": str(self.tls_overlay), "ingress_directory": str(self.membership), "nginx_container_id": self.nginx_id, "public_url": self.origin},
                "source": self.sources["new"], "images": {"backend": {"tag": self.images["new"], "digest": self.image_ids["new"]},
                                                          "frontend": {"tag": self.images["frontend"], "digest": self.image_ids["frontend"]}},
                "evidence_directory": str(self.evidence), "runtime_owner": "artifact_cli"}
        private_write(self.input, json.dumps(spec))
        with self.rollout.operation_locks(spec["target"]):
            pass
        before = self.facts(heartbeat=True)
        code, output = self.backend("new", [], label="refuse ordinary automatic " + self.new_head + " migration", overrides={"SKIP_MIGRATION": "false"}, ok=False)
        ordinary_denial = ("M126 requires explicit maintenance migration" if self.new_head == RETIREMENT_HEAD
                           else "retired-license schema must have its complete artifact ledger")
        require(code != 0 and ordinary_denial in output and self.facts(heartbeat=True) == before, "ordinary migration refusal is not proved")
        code, output = self.backend("new", ["migrate", "up"], label="refuse maintenance while old writers remain", ok=False)
        unsafe_denial = ("M126 requires stopped, retired writers and resolved ingress changes"
                         if self.new_head == RETIREMENT_HEAD else "M127 requires stopped retired writers")
        require(code != 0 and unsafe_denial in output and self.facts(heartbeat=True) == before,
                "unsafe-writer migration refusal is not proved")
        before_files = self.file_hashes(self.membership)
        resources = self.resource_ids()
        self.plan = self.maintenance("plan")
        require(self.facts(heartbeat=True) == before and self.file_hashes(self.membership) == before_files and self.resource_ids() == resources
                and not self.evidence.exists(), "maintenance plan changed retained authority")
        self.maintenance("prepare")
        self.backup_facts = self.facts(heartbeat=True)
        require(self.backup_facts == before and (self.evidence / "before.dump").stat().st_size > 0, "maintenance preparation changed protected authority")
        self.result["backup_sha256"] = digest((self.evidence / "before.dump").read_bytes())
        receivers = {name: self.inspect(name)["Id"] for name in ("uniroute-api", "uniroute-api-canary")}
        for action in ("stop", "remove", "check"):
            code, output = self.maintenance(action, "stable", ok=False)
            require(code != 0 and "full withdrawal is not applied" in output
                    and self.facts(heartbeat=True) == before and self.resource_ids() == resources,
                    action + " before withdrawal was not safely refused")
            for name, identity in receivers.items():
                current = self.inspect(name)
                require(current["Id"] == identity and current["State"]["Running"], "early refusal changed a receiver")
            # stop/remove may retain intent, but must not issue a Docker command.
            require(not (self.evidence / ("command-" + action + "-stable.json")).exists(),
                    "early refusal crossed the container command boundary")
        self.passed("unsafe migration and pre-withdrawal stop/remove/retirement refused; plan is read-only and backup retained")
        old_workers = self.ingress.workers("uniroute-nginx")
        self.maintenance("withdraw")
        require(not old_workers.intersection(self.ingress.workers("uniroute-nginx")) and (self.membership / "upstreams.conf").read_bytes() == self.ingress.membership("none"), "actual full withdrawal is unproved")
        for role in ("stable", "canary"):
            self.maintenance("stop", role)
            self.maintenance("remove", role)
        deadline = time.monotonic() + 60
        while self.sql("SELECT count(*) FROM runtime_generations WHERE lease_expires_at > clock_timestamp()") != "0":
            require(time.monotonic() < deadline, "old lease expiry deadline")
            time.sleep(1)
        self.control.expand.verify_recovery_process_inventory({})
        for role in ("stable", "canary"):
            before = self.facts()
            files = self.file_hashes(self.evidence)
            result = self.maintenance("check", role)
            require(result["status"] == "eligible" and self.facts() == before and self.file_hashes(self.evidence) == files, "retirement check mutated authority or evidence")
            with self.control.existing_locks(spec["target"]):
                operation = self.control.MaintenanceRetirement(self.rollout.document(self.input))
                stale = operation.retirement_document(role)
                row = next(x for x in before["runtime_generations"] if x["instance_id"] == stale["instance_id"])
                require(row["state"] != "stopped" and stale["expected_revision"] > 1,
                        "stale revision test lacks an eligible positive predecessor")
                # The real proof is eligible; change only its legal revision.
                stale["expected_revision"] -= 1
                code, output = self.backend("new", ["runtime-generation", "stop-check"],
                                            label="refuse stale retirement revision " + role, data=json.dumps(stale).encode(), ok=False)
            require(code != 0 and "historical generation predecessor changed or is still active" in output
                    and self.facts() == before and self.file_hashes(self.evidence) == files,
                    "stale revision alone did not refuse without mutation")
            self.maintenance("apply", role)
        stopped = self.facts()
        expected_generations = 2 if self.new_head == RETIREMENT_HEAD else 4
        require(len(self.backup_facts["runtime_generations"]) == expected_generations, "unexpected predecessor generation count")
        require(len(stopped["runtime_generations"]) == expected_generations and all(x["state"] == "stopped" and x["ingress_state"] == "absent" for x in stopped["runtime_generations"]), "old generation retirement differs")
        self.passed("real NGINX withdrawal, clean process exit, exact removal and read-only retirement checks")
        self.backend("new", ["migrate", "up"], label="apply explicit " + self.new_head + " maintenance migration")
        migrated = self.facts()
        self.migrated_coverage = migrated["execution_source_budget_coverage"]
        require([x["version"] for x in sorted(migrated["schema_migrations"], key=lambda x: x["version"])] == self.old_ledger + [self.new_head], "complete target ledger differs")
        require(not self.trigger_names(), "commercial triggers remain")
        for name in stopped:
            if name != "schema_migrations":
                require(migrated[name] == stopped[name], "migration changed preserved " + name)
        code, output = self.backend("old", [], label="refuse old Artifact on " + self.new_head, ok=False)
        require(code != 0 and "Schema authority startup gate failed:" in output and self.facts() == migrated, "old Artifact schema refusal is not proved")
        self.passed(self.new_head + " preserves history and financial certificate; old Artifact refuses admission")
        self.run("start new exact " + self.new_head + " receivers", self.compose("new") + ["up", "-d", "--no-deps", "--pull", "never", "--no-build", "backend", "backend-canary"], env=self.compose_env("new"))
        self.wait_ready()
        self.join("maintenance-native-" + self.stage + "-new-both")
        login_count = int(self.sql("SELECT count(*) FROM audit_logs WHERE action='login'"))
        token = self.request("/api/v1/auth/login", data={"username": "admin", "password": self.admin_password})["token"]
        require(self.request("/api/v1/license", token=token).get("status") == "not_required", "new runtime still requires commercial License")
        deadline = time.monotonic() + 60
        while self.sql("SELECT (SELECT count(*) FROM audit_logs WHERE action='login')=" + str(login_count + 1)
                      + " AND NOT EXISTS(SELECT 1 FROM internal_outbox_events WHERE payload->>'action'='login')") != "t":
            require(time.monotonic() < deadline, "new login audit materialization deadline")
            time.sleep(1)
        if self.new_head == NEW_HEAD:
            require(self.sql("SELECT count(*) FROM xinsight_communication_policies") == "0"
                    and self.sql("SELECT count(*) FROM xinsight_communication_runs") == "0",
                    "empty connection fixture acquired communication intent or receipts")
        self.passed("new " + self.new_head + " fleet rejoins actual NGINX and permits administrator login without License")

    def restore(self):
        name = self.restore_database
        pg_env = self.root / ("restore-postgres-" + self.stage + ".env")
        private_write(pg_env, "POSTGRES_USER=uniroute\nPOSTGRES_DB=uniroute\nPOSTGRES_PASSWORD=" + self.password + "\n")
        network = self.inspect("uniroute-postgres")["NetworkSettings"]["Networks"]
        require(len(network) == 1, "fixture network is ambiguous")
        network = next(iter(network))
        self.run("start separate empty restore database", ["docker", "run", "-d", "--name", name, "--network", network,
                 "--label", "io.unirouter.fixture=" + self.project, "--env-file", pg_env, self.image_ids["postgres"]])
        deadline = time.monotonic() + 60
        while True:
            code, _ = self.run("wait for restore database", ["docker", "exec", name, "pg_isready", "-h", "127.0.0.1", "-p", "5432", "-U", "uniroute", "-d", "uniroute"], ok=False, timeout=10)
            if code == 0:
                break
            require(time.monotonic() < deadline, "restore database readiness deadline")
            time.sleep(1)
        # The official image's initialization server accepts Unix sockets before
        # POSTGRES_DB exists, then exits. Only the final server opens TCP.
        require(self.sql("SELECT current_database()='uniroute' AND current_user='uniroute' "
                         "AND NOT EXISTS (SELECT 1 FROM pg_class WHERE relnamespace='public'::regnamespace)",
                         restored=True) == "t", "restore target is not the expected empty database")
        require(self.sql("SELECT system_identifier FROM pg_control_system()", restored=True)
                != self.sql("SELECT system_identifier FROM pg_control_system()"),
                "restore target shares the source database system")
        self.result["restore_target"] = "distinct_empty_database_after_tcp_readiness"
        self.run("restore actual pre-" + self.new_head + " backup", ["docker", "exec", "-i", name, "pg_restore", "--exit-on-error", "-U", "uniroute", "-d", "uniroute"], data=(self.evidence / "before.dump").read_bytes())
        restored = self.facts(restored=True, heartbeat=True)
        require(restored == self.backup_facts, "restored predecessor authoritative facts differ from backup")
        source = Path(self.sources["old"]["directory"])
        old_coverage = restored["execution_source_budget_coverage"]
        old_streams = self.table("execution_content_streams", restored=True)
        self.result["restored_content_stream_count"] = len(old_streams)
        self.run("apply original stopped-restore rebase owner", ["docker", "exec", "-i", name, "psql", "-X", "-U", "uniroute", "-d", "uniroute", "-v", "ON_ERROR_STOP=1"], data=(source / "scripts/source-budget-restore-rebase.sql").read_bytes())
        coverage = self.table("execution_source_budget_coverage", restored=True)
        require(len(coverage) == len(old_coverage) + 1, "restore did not append one coverage successor")
        prior = next(x for x in old_coverage if x["ended_at"] is None)
        successor = next(x for x in coverage if x["ended_at"] is None)
        require(successor["reason"] == "restore_rebase" and successor["predecessor_epoch"] == prior["coverage_epoch"], "restore coverage lineage differs")
        for field in ("schema_head", "writer_contract", "migration_sha256", "schema_ledger_sha256", "support_manifest_sha256", "support_manifest_bytes"):
            require(successor[field] == prior[field], "restore changed certified support proof")
        after_streams = {(x["org_id"], x["project_id"]): x["recovery_epoch"] for x in self.table("execution_content_streams", restored=True)}
        require(all(after_streams[(x["org_id"], x["project_id"])] != x["recovery_epoch"] for x in old_streams), "content restore epoch was reused")
        config = json.loads(self.run("read original restore runtime input", self.compose("old") + ["config", "--format", "json"], env=self.compose_env("old")))
        env = config["services"]["backend"]["environment"]
        env["DB_DSN"] = "postgres://uniroute:" + self.password + "@" + name + ":5432/uniroute?sslmode=disable"
        env["RUNTIME_DEPLOYMENT_ROLE"] = "stable"
        env_file = self.root / ("restore-backend-" + self.stage + ".env")
        require(all("\n" not in str(x) for x in env.values()), "restore environment contains newlines")
        private_write(env_file, "".join(k + "=" + str(v) + "\n" for k, v in env.items()))
        self.run("start original Artifact only on restored database", ["docker", "run", "-d", "--name", self.restore_backend,
                 "--network", network, "--restart", "no", "--stop-timeout", "2100", "--env-file", env_file,
                 "--label", "io.unirouter.fixture=" + self.project, self.image_ids["old"]])
        self.wait_ready(restored=True)
        require(self.sql("SELECT max(version) FROM schema_migrations", restored=True) == self.old_head, "restored original Artifact changed schema")
        require(self.table("execution_source_budget_coverage") == self.migrated_coverage, "restore fixture affected the migrated financial certificate")
        self.passed("real backup restores separately through original rebase owner and admits the original Artifact")
        self.result["transitions"].append({"original_schema": self.old_head, "migrated_schema": self.new_head,
            "backup_sha256": self.result["backup_sha256"], "restore_database": self.restore_database,
            "sources": {k: {f: self.sources[k][f] for f in ("commit", "tree", "version")} for k in ("old", "new")},
            "status": "passed"})

    def successor_transition(self):
        # The first transition and independent M125 restore remain complete facts.
        # Stop only that synthetic restored backend before the same host's next
        # process-inventory proof; retain both databases and all evidence.
        require(self.inspect(self.restore_backend)["Image"] == self.image_ids["old"], "restored backend identity changed")
        self.run("stop this fixture's validated restored predecessor", ["docker", "stop", self.restore_backend])
        require(not self.inspect(self.restore_backend)["State"]["Running"], "restored backend still running")
        self.sources["old"], self.sources["new"] = self.sources["new"], self.sources["successor"]
        self.images["old"], self.images["new"] = self.images["new"], self.images["successor"]
        self.image_ids["old"], self.image_ids["new"] = self.image_ids["new"], self.image_ids["successor"]
        self.old_overlay, self.new_overlay = self.new_overlay, self.successor_overlay
        self.repo = Path(self.sources["new"]["directory"])
        self.control = module("successor_maintenance_owner", self.repo / "scripts/runtime-generation-retire.py")
        self.rollout = self.control.rollout
        self.ingress = module("successor_fixture_ingress", self.repo / "scripts/runtime-ingress.py")
        self.old_head, self.new_head = RETIREMENT_HEAD, NEW_HEAD
        self.stage = "m126-to-m127"
        self.evidence = self.root / "communication-maintenance-evidence"
        self.input = self.root / "communication-maintenance.json"
        self.restore_database = "ur-maintenance-restore-postgres-m126"
        self.restore_backend = "ur-maintenance-restore-backend-m126"
        self.old_ledger = sorted(p.name[:-7] for p in (Path(self.sources["old"]["directory"]) / "migrations").glob("*.up.sql"))
        require(self.old_ledger[-1] == RETIREMENT_HEAD and not self.trigger_names(), "second transition lacks exact retired-license predecessor")
        require(self.sql("SELECT max(version) FROM schema_migrations") == RETIREMENT_HEAD, "second transition database differs")
        for name in ("old", "new"):
            self.backend(name, ["runtime-generation", "support"], label="validate second transition " + name + " artifact")
        self.retire_and_migrate()
        self.restore()

    def file_hashes(self, root):
        return {str(p.relative_to(root)): digest(p.read_bytes()) for p in root.rglob("*") if p.is_file()}

    def resource_ids(self):
        return {kind: sorted(self.run("read fixture resource identities", ["docker", *args]).splitlines()) for kind, args in (
            ("containers", ["ps", "-aq", "--no-trunc"]), ("volumes", ["volume", "ls", "-q"]), ("networks", ["network", "ls", "-q", "--no-trunc"]))}

    def execute(self):
        self.preflight()
        self.build()
        self.configuration()
        self.establish_m125()
        self.retire_and_migrate()
        self.restore()
        self.successor_transition()
        self.result.update(status="passed", original_schema=OLD_HEAD, migrated_schema=NEW_HEAD)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--expected-commit", required=True)
    parser.add_argument("--report", type=Path, required=True)
    args = parser.parse_args()
    require(os.environ.get("GITHUB_ACTIONS") == "true" and os.environ.get("RUNNER_ENVIRONMENT") == "github-hosted"
            and platform.system() == "Linux" and platform.machine() == "x86_64" and os.geteuid() == 0,
            "requires a disposable GitHub-hosted Linux amd64 runner as root")
    require(sys.version_info >= (3, 10), "Python 3.10 or newer required")
    checkout = Path(__file__).resolve().parents[2]
    root = Path(tempfile.mkdtemp(prefix="ur-maintenance-native-", dir=os.environ["RUNNER_TEMP"]))
    os.chmod(root, 0o700)
    fixture = None
    result = {"schema": "unirouter.maintenance-native-acceptance.v1", "status": "failed", "phase": "fixture initialization"}
    try:
        fixture = Acceptance(checkout, root, args.expected_commit)
        fixture.execute()
        result = fixture.result
    except (Refused, RuntimeError, OSError, ValueError, KeyError, subprocess.TimeoutExpired, urllib.error.URLError) as error:
        if fixture is not None:
            result = dict(fixture.result, status="failed", phase=fixture.phase)
        # Arbitrary owner, database and subprocess error strings remain private.
        result["error"] = str(error) if isinstance(error, Refused) else type(error).__name__
    args.report.write_text(json.dumps(result, indent=2) + "\n")
    os.chmod(args.report, 0o644)
    print(json.dumps(result), flush=True)
    return 0 if result["status"] == "passed" else 1


if __name__ == "__main__":
    sys.exit(main())
