#!/usr/bin/env python3
"""Consume the fixed real public UR release on one disposable hosted runner.

The checked-in candidate config is deliberately incomplete. Describe/validate
perform no network, Docker, key or output operations. Execution never builds
images, creates signing keys, publishes packages or claims persistent Target.
"""
import argparse
import base64
import contextlib
import datetime as dt
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import uuid

IMMUTABLE_SHA256 = "f2b5060f23d585eeb2dad21ec66c1088e8520a1134b3bcf0ae88cbfc47867002"
SCHEMA = "000127_xinsight_communication_policy"
WORKFLOW = "foigent/unirouter/.github/workflows/community-release-acceptance.yml@refs/heads/main"
RELEASE_KEYS = {"release_sequence", "created_at", "expires_at", "tag", "bootstrap_url",
                "bootstrap_size", "bootstrap_sha256", "package_size", "package_sha256"}


class Stop(Exception):
    """Only static authored labels may be emitted."""


def require(ok, message):
    if not ok:
        raise Stop(message)


def strict_json(raw):
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, "Duplicate JSON metadata field.")
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=pairs)


def sha(path):
    value = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def immutable_digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def load_config(repo):
    path = repo / ".github/release-inputs/community-formal-release.json"
    require(not path.is_symlink() and path.stat().st_size <= 64 * 1024, "Unexpected fixed consumer input.")
    return strict_json(path.read_bytes())


def asset_url(release, name):
    return "https://github.com/foigent/unirouter/releases/download/" + release["tag"] + "/" + name


def validate(config, *, ready=False):
    require(set(config) == {"schema_version", "state", "immutable", "release"} and
            config["schema_version"] == "unirouter.formal-consumer-input.v1" and
            config["state"] in ("incomplete", "ready"), "Invalid consumer config shape.")
    fixed, release = config["immutable"], config["release"]
    require(immutable_digest(fixed) == IMMUTABLE_SHA256, "Fixed product/image/payload/trust identity changed.")
    require(set(release) == RELEASE_KEYS, "Unexpected release decision fields.")
    require(fixed["manifest_schema_version"] == "unirouter.upgrade-manifest.v2" and
            fixed["expires_at"] == release["expires_at"] == "never", "Explicit signed non-expiring policy required.")
    def missing_bindings(v, prefix):
        if v is None: return [prefix]
        if isinstance(v, dict): return [p for k, x in v.items() for p in missing_bindings(x, prefix + "." + k)]
        if isinstance(v, list): return [p for i, x in enumerate(v) for p in missing_bindings(x, prefix + "." + str(i))]
        return []
    missing = [k for k, v in release.items() if v is None] + missing_bindings(fixed, "binding")
    if ready:
        require(config["state"] == "ready" and not missing,
                "Formal release input is incomplete; no output, network or Docker operation was attempted.")
        n = release["release_sequence"]
        require(type(n) is int and fixed["historical_sequence_reserved_through"] < n < 2 ** 63,
                "Invalid admitted release sequence.")
        require(release["tag"] == "v1.14.0-preview." + str(n), "Release tag differs from the admitted preview identity.")
        parse = lambda v: dt.datetime.fromisoformat(v.replace("Z", "+00:00"))
        created = parse(release["created_at"])
        require(created.tzinfo is not None and created.utcoffset() == dt.timedelta(0) and
                created.strftime("%Y-%m-%dT%H:%M:%SZ") == release["created_at"] and
                created <= dt.datetime.now(dt.timezone.utc) + dt.timedelta(minutes=5),
                "Release creation time is not current canonical UTC.")
        require(release["bootstrap_url"] == asset_url(release, "community-bootstrap.json"),
                "Bootstrap URL is outside the exact public preview asset.")
        for k, maximum in (("bootstrap_size", 32 * 1024), ("package_size", 512 * 1024 ** 2)):
            require(type(release[k]) is int and 0 < release[k] <= maximum, "Invalid bounded actual download size.")
        for k in ("bootstrap_sha256", "package_sha256"):
            require(isinstance(release[k], str) and re.fullmatch(r"[a-f0-9]{64}", release[k]),
                    "Actual pinned download SHA256 is missing.")
    return missing


def bootstrap_binding(config, value):
    f, r = config["immutable"], config["release"]
    require(value["release"] == {"version": f["product"]["version"], "commit": f["product"]["commit"],
                "release_sequence": r["release_sequence"], "key_id": f["key_id"], "target_schema": SCHEMA},
            "Public bootstrap release binding differs.")
    expected = {"verifier": ("uniroute-upgrade-linux-amd64", f["verifier"]),
                "public_keys": ("public-keys.json", f["public_ring"]),
                "package": ("unirouter-1.14.0-linux-amd64.tar.gz",
                            {"size": r["package_size"], "sha256": r["package_sha256"]})}
    require(set(value["assets"]) == set(expected), "Bootstrap asset closed set differs.")
    for role, (name, identity) in expected.items():
        require(value["assets"][role] == dict(identity, url=asset_url(r, name)),
                "Public download/trust identity differs.")


def manifest_binding(config, manifest):
    f, r = config["immutable"], config["release"]
    expected = {"schema_version": "unirouter.upgrade-manifest.v2", "product": "unirouter",
                "key_id": f["key_id"], "signature_algorithm": "ed25519",
                "release_sequence": r["release_sequence"], "version": f["product"]["version"],
                "commit": f["product"]["commit"], "created_at": r["created_at"], "expires_at": r["expires_at"],
                "target_os": "linux", "target_arch": "amd64", "min_current_sequence": 0,
                "max_current_sequence": 0, "min_current_schema": SCHEMA, "target_schema": SCHEMA,
                "provenance_sha256": f["provenance_sha256"], "artifacts": f["artifacts"]}
    require(manifest == expected, "Verified real manifest differs from the reviewed24-input contract.")


def native_guard(repo):
    require(os.environ.get("GITHUB_ACTIONS") == "true" and
            os.environ.get("RUNNER_ENVIRONMENT") == "github-hosted" and
            os.environ.get("GITHUB_REPOSITORY") == "foigent/unirouter" and
            os.environ.get("GITHUB_EVENT_NAME") == "workflow_dispatch" and
            os.environ.get("GITHUB_REF") == "refs/heads/main" and
            os.environ.get("GITHUB_WORKFLOW_REF") == WORKFLOW and
            platform.system() == "Linux" and platform.machine() == "x86_64" and os.geteuid() == 0,
            "Requires the exact disposable hosted Linux workflow, not a workstation or Target.")
    commit = os.environ.get("GITHUB_SHA", "")
    require(re.fullmatch(r"[a-f0-9]{40}", commit) and
            Path(os.environ.get("GITHUB_WORKSPACE", "")).resolve() == repo.resolve(), "Wrong checkout identity.")
    env = {k: os.environ[k] for k in ("PATH", "HOME", "LANG", "LC_ALL") if k in os.environ}
    result = subprocess.run(["git", "-c", "safe.directory=" + str(repo), "rev-parse", "HEAD"],
                            cwd=repo, env=env, capture_output=True)
    require(result.returncode == 0 and result.stdout.decode().strip() == commit, "Workflow checkout differs from actual GitHub commit.")
    return {"workflow_commit": commit, "run": os.environ.get("GITHUB_RUN_ID"),
            "attempt": os.environ.get("GITHUB_RUN_ATTEMPT")}


def load_module(path, expected_sha, name):
    require(not path.is_symlink() and sha(path) == expected_sha, "Existing helper source changed.")
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def reference_sources(repo, config):
    fixed = config["immutable"]
    for path, expected in ((repo / ".github/tests/community-installer.py", fixed["native_acceptance_helper_sha256"]),
                           (repo / "deploy/community/bootstrap.py", fixed["bootstrap_script"]["sha256"])):
        require(path.is_file() and not path.is_symlink() and sha(path) == expected,
                "Fixed existing consumer/helper source changed before preparation.")


@contextlib.contextmanager
def clean_environment(env):
    old = dict(os.environ)
    try:
        os.environ.clear()
        os.environ.update(env)
        yield
    finally:
        os.environ.clear()
        os.environ.update(old)


def make_acceptance(repo, root, config, context):
    fixed = config["immutable"]
    base = load_module(repo / ".github/tests/community-installer.py",
                       fixed["native_acceptance_helper_sha256"], "accepted_native_installer")
    bootstrap = load_module(repo / "deploy/community/bootstrap.py", fixed["bootstrap_script"]["sha256"],
                            "accepted_product_bootstrap")

    class Formal(base.Acceptance):
        def __init__(self):
            self.repo, self.root, self.config = repo, root, config
            self.logs = root / "logs"
            self.logs.mkdir(mode=0o700)
            self.instances = root / "instances"
            self.instances.mkdir(mode=0o700)
            home = root / "anonymous-home"
            home.mkdir(mode=0o700)
            docker_config = home / ".docker"
            docker_config.mkdir(mode=0o700)
            base.private_write(docker_config / "config.json", b'{"auths":{"ghcr.io":{}}}\n')
            self.env = {k: os.environ[k] for k in ("PATH", "LANG", "LC_ALL") if k in os.environ}
            self.env.update(HOME=str(home), DOCKER_CONFIG=str(docker_config))
            self.docker = shutil.which("docker")
            require(self.docker is not None, "Docker is unavailable.")
            self.docker = str(Path(self.docker).resolve())
            self.release = root / "prepared-release"  # Remains absent until the shipped bootstrap creates it.
            self.verifier, self.ring = self.release / "uniroute-upgrade", self.release / "public-keys.json"
            self.bootstrap_file, self.bootstrap_script = root / "bootstrap.json", root / "bootstrap.py"
            self.bootstrap_sha = config["release"]["bootstrap_sha256"]
            self.commit, self.tree, self.version = [fixed["product"][k] for k in ("commit", "tree", "version")]
            self.release_sequence = config["release"]["release_sequence"]
            self.suffix = uuid.uuid4().hex[:10]
            self.images, self.image_ids = fixed["images"], {}
            self.phase = "native prerequisites"
            self.result = {"schema": "unirouter.formal-community-consumer.v1", "status": "running", "checks": [],
                           "platform": "linux/amd64", "source": self.commit, "tree": self.tree, "version": self.version,
                           "release_sequence": self.release_sequence, "provider_calls": 0, "builds": 0,
                           "signatures_generated": 0, "persistent_Target_acceptance": False, **context}

        def bootstrap_command(self, action, directory, project):
            return [sys.executable, "-I", "-B", self.bootstrap_script, action, "--bootstrap", self.bootstrap_file,
                    "--bootstrap-sha256", self.bootstrap_sha, "--directory", self.release,
                    "--install-directory", directory, "--name", project, "--port", "13000"]

        def prepare(self):
            r = config["release"]
            bootstrap.download({"url": r["bootstrap_url"], "size": r["bootstrap_size"],
                                "sha256": r["bootstrap_sha256"]}, self.bootstrap_file)
            anchor = bootstrap.Bootstrap(self.bootstrap_file.read_bytes(), r["bootstrap_sha256"])
            bootstrap_binding(config, anchor.value)
            bootstrap.download(fixed["bootstrap_script"], self.bootstrap_script)
            self.run("prepare real owner-signed public release", self.bootstrap_command("prepare", root / "unused-install", "unused"))
            with tarfile.open(self.release / "release.tar.gz", "r:gz") as archive:
                first = archive.next()
                require(first is not None and first.isfile() and first.name == "manifest.json" and first.size <= 128 * 1024,
                        "Verified signed manifest is unavailable.")
                manifest_binding(config, strict_json(archive.extractfile(first).read()))
            descriptor = strict_json((self.release / "payload/install.json").read_bytes())
            require(descriptor == {"schema_version": "unirouter.install.v1", "images": self.images},
                    "Real signed descriptor differs from actual accepted digest references.")
            self.other_ring = root / "wrong-public-ring.json"
            base.private_write(self.other_ring, json.dumps({fixed["key_id"]: base64.b64encode(bytes(32)).decode()}).encode())
            self.result.update(real_owner_signed_public_package_verified=True,
                               package_sha256=r["package_sha256"], bootstrap_sha256=r["bootstrap_sha256"])
            self.passed("actual public signed24-input package and independent trust bindings verified")
            expected = {a["role"]: a for a in fixed["app_image_identities"]}
            for service, ref in self.images.items():
                self.run("anonymous pull " + service, [self.docker, "pull", ref])
                values = json.loads(self.run("inspect exact image " + service, [self.docker, "image", "inspect", ref]))
                require(len(values) == 1, "Ambiguous anonymous image.")
                image = values[0]
                require(image["Os"] == "linux" and image["Architecture"] == "amd64" and
                        ref in image.get("RepoDigests", []), "Anonymous image digest/platform differs.")
                if service in expected:
                    require(image["Id"] == expected[service]["config_id"] and
                            image["RootFS"]["Layers"] == expected[service]["diff_ids"],
                            "Anonymous app config or ordered layer identities differ.")
                self.image_ids[service] = image["Id"]
            self.passed("six anonymous digest pulls and both app configs/ordered18layers verified")

        def negative_checks(self, directory, project):
            before = self.inventory(), base.files(self.release), base.files(self.instances)
            self.run("read-only real bootstrap check", self.bootstrap_command("check", directory, project))
            self.run("read-only real installer check", self.command("check", directory, project))
            require(before == (self.inventory(), base.files(self.release), base.files(self.instances)), "Read-only check changed resources.")
            code, _ = self.run("reject wrong public key", self.command("check", directory, project, ring=self.other_ring), ok=False)
            require(code != 0, "Wrong public key was accepted.")
            descriptor = self.release / "payload/install.json"
            original = descriptor.read_bytes()
            try:
                with descriptor.open("ab") as stream:
                    stream.write(b" ")
                tampered = self.inventory(), base.files(self.release), base.files(self.instances)
                code, _ = self.run("reject modified signed descriptor", self.command("check", directory, project), ok=False)
                require(code != 0 and tampered == (self.inventory(), base.files(self.release), base.files(self.instances)),
                        "Tampered descriptor check did not refuse without writes.")
            finally:
                descriptor.write_bytes(original)
            require(before == (self.inventory(), base.files(self.release), base.files(self.instances)), "Negative trust checks changed resources.")
            volume = project + "_postgres_data"
            self.run("create exact empty synthetic collision", [self.docker, "volume", "create", volume])
            collision = self.run("read exact collision", [self.docker, "volume", "inspect", volume])
            code, _ = self.run("reject exact-name collision", self.command("install", directory, project), ok=False)
            require(code != 0 and not directory.exists() and collision ==
                    self.run("verify collision unchanged", [self.docker, "volume", "inspect", volume]), "Conflict refusal failed.")
            self.run("remove this exact empty test collision", [self.docker, "volume", "rm", volume])
            require(before == (self.inventory(), base.files(self.release), base.files(self.instances)), "Conflict check changed resources.")
            self.passed("check purity, wrong key, tampered signed descriptor and exact collision refusal")

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
            require(release == {"sequence": self.release_sequence, "version": self.version, "commit": self.commit,
                                "package": self.config["release"]["package_sha256"], "schema": SCHEMA}, "initial release registration mismatch")
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

        def execute(self):
            self.preflight()
            self.prepare()
            self.project = "ur-formal-" + self.suffix
            self.directory = self.instances / "interrupted"
            self.negative_checks(self.directory, self.project)
            self.interrupt_after_plan(self.directory, self.project)
            self.installed_checks(self.directory, self.project)
            self.result["status"] = "passed"
            self.result.pop("installer_stop", None)

    return Formal()


def execute(config, repo):
    validate(config, ready=True)
    context = native_guard(repo)  # All invalid inputs/context stop before creating any destination.
    reference_sources(repo, config)
    os.umask(0o077)
    root = Path(tempfile.mkdtemp(prefix="ur-formal-39370754179-", dir="/var/tmp")).resolve()
    root.chmod(0o700)
    fixture = make_acceptance(repo, root, config, context)
    try:
        with clean_environment(fixture.env):
            fixture.execute()
    except BaseException:
        fixture.result.update(status="failed", phase=fixture.phase, error="consumer_check_failed_redacted")
        with clean_environment(fixture.env):
            fixture.failure_facts()
    # The runner lifecycle owns these exact temporary resources. No broad cleanup,
    # second manager, artifact/cache upload or persistent Target claim.
    report = json.dumps(fixture.result, separators=(",", ":"))
    require(len(report.encode()) <= 32 * 1024, "Allowlisted report exceeded its complete bound.")
    print(report, flush=True)
    return 0 if fixture.result["status"] == "passed" else 1


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("describe", "validate", "execute"))
    args = parser.parse_args(argv)
    try:
        repo = Path(__file__).resolve().parents[2]
        config = load_config(repo)
        missing = validate(config, ready=args.action != "describe")
        if args.action == "describe":
            print(json.dumps({"source": config["immutable"]["product"]["commit"], "payload_entries": 24,
                              "ready": config["state"] == "ready" and not missing,
                              "missing_decisions": missing, "network_Docker_or_output_operations": 0}))
            return 0
        if args.action == "validate":
            print("Actual public input shape accepted; no network, Docker or destination operation.")
            return 0
        return execute(config, repo)
    except BaseException as error:
        print("Formal consumer stopped: " + (str(error) if isinstance(error, Stop) else type(error).__name__), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
