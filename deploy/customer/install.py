#!/usr/bin/env python3
"""Install a verified UniRouter customer release on a new Linux Docker project.

This source candidate has no embedded download URL, signing key or image tag.
The release producer supplies the signed payload/install.json descriptor.
"""
import argparse
import fcntl
import hashlib
import ipaddress
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import secrets
import shutil
import socket
import stat
import subprocess
import sys
import tarfile
import tempfile
import uuid


SERVICES = ("backend", "frontend", "postgres", "redis", "prometheus", "grafana")
IMAGE_KEYS = {"backend": "UNIROUTER_BACKEND_IMAGE", "frontend": "UNIROUTER_FRONTEND_IMAGE",
              "postgres": "POSTGRES_IMAGE", "redis": "REDIS_IMAGE",
              "prometheus": "PROMETHEUS_IMAGE", "grafana": "GRAFANA_IMAGE"}
VOLUMES = ("postgres_data", "redis_data", "prometheus_data", "grafana_data")
SCHEMA = "000129_upstream_observation_restore_namespace"
LABEL = "io.unirouter.installation"
DESCRIPTOR = "payload/install.json"
PREFIX = "payload/deploy/"
MAX_METADATA = 128 * 1024


class InstallError(Exception):
    pass


def require(condition, message):
    if not condition:
        raise InstallError(message)


def digest(path):
    with path.open("rb") as stream:
        h = hashlib.sha256()
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(block)
        return h.hexdigest()


def immutable_image_reference(value):
    return isinstance(value, str) and re.fullmatch(r"[a-z0-9][a-z0-9._:/-]*@sha256:[a-f0-9]{64}", value) is not None


def strict_json(raw):
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, "Duplicate JSON field")
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=pairs)


def regular(path):
    require(not path.is_symlink() and path.is_file(), "Expected a regular file: " + str(path))
    return path


def safe_child(root, relative):
    p = PurePosixPath(relative)
    require(relative == p.as_posix() and not p.is_absolute() and
            all(x not in (".", "..") for x in p.parts) and "\\" not in relative,
            "Unsafe release path")
    target = root
    for part in p.parts:
        target = target / part
        require(not target.is_symlink(), "Symlinks are not supported in a release directory")
    require(target.resolve().is_relative_to(root.resolve()), "Release path escaped its directory")
    return regular(target)


def private_file(path, content):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(content)
        stream.flush()
        os.fsync(stream.fileno())


def save_state(directory, state):
    fd, name = tempfile.mkstemp(prefix=".state-", dir=directory)
    with os.fdopen(fd, "w") as stream:
        json.dump(state, stream, sort_keys=True)
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(name, directory / "install-state.json")
    fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def clean_environment():
    # Compose interpolation must not inherit a caller's runtime secrets/settings.
    return {key: os.environ[key] for key in ("PATH", "HOME", "LANG", "LC_ALL") if key in os.environ}


def execute(args, *, input=None, label="command"):
    try:
        result = subprocess.run([str(x) for x in args], input=input, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, env=clean_environment(), check=False)
    except OSError as exc:
        raise InstallError(label + ": executable unavailable") from exc
    # Child errors may contain DSNs, Compose environments or secret values.
    require(result.returncode == 0, label + " failed (exit " + str(result.returncode) +
            "); no secret-bearing child output was logged")
    return result.stdout.decode("utf-8", errors="strict").strip()


def fields(text):
    result = {}
    for line in text.splitlines():
        if "=" in line:
            key, value = line.split("=", 1)
            require(key not in result, "Duplicate CLI result field")
            result[key] = value
    return result


class Release:
    def __init__(self, directory, package, verifier, public_keys, arch, run=execute):
        self.directory = directory
        self.package = regular(package)
        regular(verifier)
        regular(public_keys)
        require(arch in ("amd64", "arm64"), "Only Linux amd64/arm64 release packages are supported")
        self.arch = arch
        # Inspect only bounded metadata; no tar extraction is performed here.
        with tarfile.open(package, "r:gz") as archive:
            first = archive.next()
            require(first is not None and first.name == "manifest.json" and first.isfile()
                    and 0 < first.size <= MAX_METADATA, "Missing bounded release manifest")
            manifest_raw = archive.extractfile(first).read()
            self.manifest = strict_json(manifest_raw)
        require(self.manifest.get("target_schema") == SCHEMA, "This installer requires the current artifact release schema")
        verified = fields(run([verifier, "verify", "--public-keys", public_keys,
                               "--package", package, "--target-os", "linux", "--target-arch", arch,
                               "--current-sequence", "0", "--current-schema", SCHEMA],
                              label="signed release verification"))
        # Verification is for the initial release registration after the formal
        # fresh owner reaches the declared schema; an empty database has no schema yet.
        self.sha = digest(package)
        require(verified.get("verified") == "true" and verified.get("package_sha256") == self.sha,
                "Release verifier did not confirm the selected package")
        require(verified.get("manifest_sha256") == hashlib.sha256(manifest_raw).hexdigest(),
                "Parsed manifest differs from the verified manifest; use the release-matched verifier")
        for key in ("version", "commit", "target_schema", "release_sequence"):
            require(verified.get(key) == str(self.manifest.get(key)), "Verified manifest identity mismatch")
        self.files = {}
        self.artifacts = {}
        for artifact in self.manifest["artifacts"]:
            name = artifact["path"]
            path = safe_child(directory, name)
            require(path.stat().st_size == artifact["size"] and digest(path) == artifact["sha256"],
                    "Prepared release file differs from signed payload: " + name)
            self.files[name] = path
            self.artifacts[name] = artifact
        for name in (DESCRIPTOR, PREFIX + "install.py", PREFIX + "docker-compose.yml",
                     PREFIX + ".env.example", PREFIX + "preflight.sh", PREFIX + "config/prometheus.yml"):
            require(name in self.files, "Signed release is missing " + name)
        require(digest(Path(__file__)) == digest(self.files[PREFIX + "install.py"]),
                "Use the installer shipped in this exact signed release")
        self.deploy = {name[len(PREFIX):]: path for name, path in self.files.items() if name.startswith(PREFIX)}
        require(any(name.startswith("config/grafana/provisioning/") for name in self.deploy) and
                any(name.startswith("config/grafana/dashboards/") for name in self.deploy),
                "Signed monitoring configuration is incomplete")
        declared = {p.resolve() for p in self.deploy.values()}
        for path in (directory / PREFIX).rglob("*"):
            require(not path.is_symlink(), "Unexpected symlink in customer release")
            if path.is_file():
                require(path.resolve() in declared, "Undeclared file in customer release")
        descriptor = strict_json(self.read_declared_metadata(DESCRIPTOR))
        require(set(descriptor) == {"schema_version", "images"} and
                descriptor["schema_version"] == "unirouter.install.v1", "Unsupported install descriptor")
        self.images = descriptor["images"]
        require(isinstance(self.images, dict) and set(self.images) == set(SERVICES), "Six signed service images are required")
        for reference in self.images.values():
            require(immutable_image_reference(reference),
                    "Every signed image must have an immutable digest reference")
        sample = fields("\n".join(line for line in self.read_declared_metadata(PREFIX + ".env.example").decode().splitlines()
                                  if line and not line.startswith("#")))
        require(sample.get("RUNTIME_ENVIRONMENT") == "production" and
                sample.get("UNIROUTER_LISTEN_ADDRESS") == "127.0.0.1" and
                sample.get("MODEL_ROUTE_AUTHORITY") == "legacy" and
                sample.get("MODEL_ROUTE_COMPATIBILITY_ENABLED") == "true",
                "Release does not implement the reviewed private fresh-install contract")
        self.subnet = ipaddress.ip_network(sample["UNIROUTER_INTERNAL_NETWORK_CIDR"])

    def read_declared_metadata(self, name):
        with regular(self.files[name]).open("rb") as stream:
            raw = stream.read(MAX_METADATA + 1)
        expected = self.artifacts[name]
        require(len(raw) <= MAX_METADATA and len(raw) == expected["size"] and
                hashlib.sha256(raw).hexdigest() == expected["sha256"],
                "Release metadata changed after verification: " + name)
        return raw


def generated_environment(sample, images, port):
    values = {IMAGE_KEYS[key]: value for key, value in images.items()}
    for key in ("POSTGRES_PASSWORD", "REDIS_PASSWORD", "JWT_SECRET", "API_KEY_HASH_SECRET",
                "UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET", "GRAFANA_ADMIN_PASSWORD"):
        values[key] = secrets.token_hex(32)
    values["ENCRYPTION_KEY"] = secrets.token_hex(16)  # Exactly 32 ASCII bytes.
    values["DB_DSN"] = "postgres://uniroute:" + values["POSTGRES_PASSWORD"] + "@postgres:5432/uniroute?sslmode=disable"
    values["POSTGRES_USER"] = values["POSTGRES_DB"] = "uniroute"
    values["UNIROUTER_HTTP_PORT"] = str(port)
    result = []
    used = set()
    for line in sample.splitlines():
        if line and not line.startswith("#") and "=" in line:
            key, value = line.split("=", 1)
            if key in values:
                value = values[key]
                used.add(key)
            require(not any(marker in value for marker in ("REPLACE", "CHANGE_ME", "CHANGEME")),
                    "Unresolved release configuration field: " + key)
            line = key + "=" + value
        result.append(line)
    require(used == set(values), "Release environment template is missing installer fields")
    return ("\n".join(result) + "\n").encode()


class Installer:
    def __init__(self, release, directory, project, port, docker_socket, run=execute):
        require(re.fullmatch(r"[a-z][a-z0-9_-]{0,47}", project), "Invalid installation name")
        require(1024 <= port <= 65535, "Choose a port from 1024 to 65535")
        require(docker_socket.is_absolute() and "," not in str(docker_socket), "Use an absolute local Docker socket")
        require(directory.is_absolute() and not directory.is_symlink(), "Use an absolute non-symlink installation directory")
        for path in (directory, release.package, release.directory):
            require(not any(c in str(path) for c in (",", ":", "\n", "\r")), "Unsupported installation path characters")
        self.release, self.directory, self.project, self.port = release, directory, project, port
        self.docker_socket, self.run = docker_socket, run
        self.docker = ["docker", "--host", "unix://" + str(docker_socket)]
        runtime = directory / "runtime"
        self.compose = self.docker + ["compose", "--project-name", project, "--project-directory", str(runtime),
            "--env-file", str(runtime / ".env"), "-f", str(runtime / "docker-compose.yml"),
            "-f", str(runtime / "installation.json"), "-f", str(runtime / "runtime-image.json")]
        self.state = None

    def command(self, args, label, input=None):
        return self.run(args, label=label, input=input)

    def inventory(self):
        result = []
        for kind, command in (("container", ["ps", "-aq"]), ("volume", ["volume", "ls", "-q"]),
                              ("network", ["network", "ls", "-q"])):
            ids = self.command(self.docker + command + ["--filter", "label=com.docker.compose.project=" + self.project],
                               "inspect installation resources").splitlines()
            # Compose can reuse an existing named volume even without its
            # project label. Inspect exact allocated names as well as labels.
            listing = (["container", "ls", "-a", "--format", "{{.Names}}"] if kind == "container"
                       else [kind, "ls", "--format", "{{.Name}}"])
            names = self.command(self.docker + listing, "inspect allocated " + kind + " names").splitlines()
            expected = ({self.project + "-" + name + "-1" for name in SERVICES} if kind == "container"
                        else {self.project + "_" + name for name in
                              (VOLUMES if kind == "volume" else ("default",))})
            ids = sorted(set(ids) | (set(names) & expected))
            for identifier in ids:
                data = strict_json(self.command(self.docker + [kind, "inspect", identifier], "inspect resource identity"))[0]
                labels = (data.get("Config", {}).get("Labels") if kind == "container" else data.get("Labels")) or {}
                if self.state is not None and kind == "container":
                    service = labels.get("com.docker.compose.service")
                    require(service in SERVICES and data.get("Image") == self.state["image_ids"].get(service),
                            "Existing container does not match the retained service image")
                    require(not (str(labels.get("com.docker.compose.oneoff", "")).lower() == "true" and
                                 data.get("State", {}).get("Running")),
                            "An earlier one-off installer container is still running; inspect it before resuming")
                result.append((kind, data.get("Id", data.get("Name", identifier)), labels))
        return result

    def check(self, resume=False):
        require(platform.system() == "Linux", "The first installer supports Linux servers only")
        require(os.geteuid() == 0, "Run the Linux installer with sudo to retain private container-written evidence")
        self.command(self.docker + ["compose", "version"], "Docker Compose v2 prerequisite")
        info = strict_json(self.command(self.docker + ["info", "--format", "{{json .}}"], "local Docker daemon prerequisite"))
        require(info.get("OSType") == "linux" and info.get("ID"), "A Linux Docker daemon with stable identity is required")
        require(not any("rootless" in option for option in info.get("SecurityOptions", [])),
                "This installer requires a rootful local Docker daemon")
        self.daemon = info["ID"]
        resources = self.inventory()
        if resume:
            require(self.state["daemon"] == self.daemon, "The selected Docker daemon changed")
            for _, _, labels in resources:
                require(labels.get(LABEL) == self.state["installation_id"] and
                        labels.get("com.docker.compose.project") == self.project,
                        "Existing project belongs to another installation")
        else:
            require(not self.directory.exists(), "Installation directory already exists; use resume for this installer's original directory")
            require(not resources, "The Compose project already exists; choose another installation name")
        own_networks = {identifier for kind, identifier, _ in resources if kind == "network"}
        network_ids = self.command(self.docker + ["network", "ls", "-q", "--no-trunc"], "inspect network allocation").splitlines()
        for identifier in network_ids:
            if identifier in own_networks:
                continue
            networks = strict_json(self.command(self.docker + ["network", "inspect", identifier], "inspect network subnet"))
            for allocation in networks[0].get("IPAM", {}).get("Config", []) or []:
                if allocation.get("Subnet"):
                    require(not self.release.subnet.overlaps(ipaddress.ip_network(allocation["Subnet"])),
                            "The signed release subnet overlaps an existing Docker network")
        if not resume:
            with socket.socket() as listener:
                try:
                    listener.bind(("127.0.0.1", self.port))
                except OSError as exc:
                    raise InstallError("The selected loopback port is already in use") from exc
        return info

    def prepare(self):
        required = self.release.package.stat().st_size + sum(p.stat().st_size for p in self.release.deploy.values())
        require(self.directory.parent.is_dir(), "Installation parent directory does not exist")
        require(shutil.disk_usage(self.directory.parent).free > required + 1024 * 1024,
                "Not enough free space for the selected release and installation files")
        self.directory.mkdir(mode=0o700)
        runtime = self.directory / "runtime"
        runtime.mkdir(mode=0o700)
        (self.directory / "evidence").mkdir(mode=0o700)
        for name, source in self.release.deploy.items():
            target = runtime / name
            target.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
            private_file(target, source.read_bytes())
            expected = self.release.artifacts[PREFIX + name]
            require(target.stat().st_size == expected["size"] and digest(target) == expected["sha256"],
                    "Signed customer file changed while copying")
            if name.startswith("config/"):
                os.chmod(target, 0o644)
                parent = target.parent
                while parent != runtime:
                    os.chmod(parent, 0o755)
                    parent = parent.parent
        # Retain the exact bundle for registration; do not use mutable source paths later.
        target = self.directory / "release.tar.gz"
        with self.release.package.open("rb") as source, target.open("xb") as output:
            os.chmod(target, 0o600)
            shutil.copyfileobj(source, output)
        require(digest(target) == self.release.sha, "Release changed while copying")
        private_file(runtime / ".env", generated_environment((runtime / ".env.example").read_text(), self.release.images, self.port))
        private_file(self.directory / "admin-password", ("Ur1!" + secrets.token_urlsafe(30) + "\n").encode())
        installation_id = str(uuid.uuid4())
        labels = {LABEL: installation_id}
        overlay = {"services": {name: {"labels": labels} for name in SERVICES},
                   "volumes": {name: {"labels": labels} for name in VOLUMES},
                   "networks": {"default": {"labels": labels}}}
        private_file(runtime / "installation.json", json.dumps(overlay).encode())
        bindings = {str(p.relative_to(self.directory)): digest(p) for p in runtime.rglob("*") if p.is_file()}
        bindings["admin-password"] = digest(self.directory / "admin-password")
        self.state = {"format": "unirouter.install-state.v1", "directory": str(self.directory),
                      "project": self.project, "port": self.port, "daemon": self.daemon,
                      "docker_socket": str(self.docker_socket), "package_sha256": self.release.sha,
                      "installation_id": installation_id, "files": bindings, "done": [], "pending": None,
                      "image_ids": {}}
        save_state(self.directory, self.state)

    def load_state(self):
        mode = self.directory.stat()
        require(mode.st_uid == os.geteuid() and stat.S_IMODE(mode.st_mode) == 0o700,
                "Original installation directory must be private and owned by the installer user")
        self.state = strict_json(regular(self.directory / "install-state.json").read_bytes())
        require("upgrade" not in self.state,
                "This installation has an upgrade witness; use resume-upgrade or upgrade-status, not fresh-install resume")
        expected = {"format": "unirouter.install-state.v1", "directory": str(self.directory),
                    "project": self.project, "port": self.port, "docker_socket": str(self.docker_socket),
                    "package_sha256": self.release.sha}
        require(all(self.state.get(k) == v for k, v in expected.items()), "Resume identity differs from the original installation")
        require(digest(regular(self.directory / "release.tar.gz")) == self.release.sha, "Retained release package changed")
        for name, sha in self.state["files"].items():
            file = safe_child(self.directory, name)
            mode = 0o644 if name.startswith("runtime/config/") else 0o600
            require(digest(file) == sha and stat.S_IMODE(file.stat().st_mode) == mode,
                    "Retained configuration/secret changed; do not regenerate it to resume")

    def step(self, name, args, *, input=None):
        self.state["pending"] = name
        save_state(self.directory, self.state)
        print("Installing: " + name, flush=True)
        output = self.command(args, name, input)
        if name not in self.state["done"]:
            self.state["done"].append(name)
        self.state["pending"] = None
        save_state(self.directory, self.state)
        return output

    def backend(self, *args):
        return self.compose + ["run", "--rm", "--no-deps", "--pull", "never", "-T",
            "--volume", str(self.directory / "evidence") + ":/evidence",
            "--volume", str(self.directory / "release.tar.gz") + ":/install/release.tar.gz:ro",
            "--env", "RUNTIME_IMAGE_DIGEST=" + self.state["image_ids"]["backend"],
            "backend", "./uniroute", *args]

    def check_rendered_compose(self):
        # This JSON contains secrets. Inspect in memory; never print/store it.
        rendered = strict_json(self.command(self.compose + ["config", "--format", "json"],
                                            "verify effective Compose identities"))
        services = rendered.get("services", {})
        require(set(services) == set(SERVICES), "Effective Compose service inventory differs from the signed descriptor")
        for name, reference in self.release.images.items():
            require(services[name].get("image") == reference and "build" not in services[name],
                    "Effective Compose image differs from the signed descriptor: " + name)
        require(services["backend"].get("environment", {}).get("RUNTIME_IMAGE_DIGEST") == self.state["image_ids"]["backend"],
                "Effective backend image declaration differs from the inspected image")
        for section, names in (("volumes", VOLUMES), ("networks", ("default",))):
            declared = rendered.get(section, {})
            require(set(declared) == set(names), "Effective Compose resource inventory differs")
            for name in names:
                require(declared[name].get("name") == self.project + "_" + name and
                        not declared[name].get("external"), "Effective Compose resource names differ from the private project")

    def release_record_matches(self, status):
        expected = {"release_sequence": str(self.release.manifest["release_sequence"]),
                    "version": self.release.manifest["version"], "commit": self.release.manifest["commit"],
                    "package_sha256": self.release.sha, "recorded_target_schema": SCHEMA, "current_schema": SCHEMA}
        return all(status.get(k) == v for k, v in expected.items())

    def install(self):
        pending = self.state["pending"]
        require(pending != "rotate-administrator", "Administrator rotation has an uncertain result. HTTP remains blocked. Retain this directory and request operator readback; do not rotate again or delete audits.")
        self.command(["env", "DOCKER_HOST=unix://" + str(self.docker_socket), "bash",
                      self.directory / "runtime/preflight.sh", self.directory / "runtime/.env"], "customer configuration preflight")
        for service in SERVICES:
            reference = self.release.images[service]
            if "pull-" + service not in self.state["done"]:
                self.step("pull-" + service, self.docker + ["pull", reference])
            inspected = strict_json(self.command(self.docker + ["image", "inspect", reference], "verify selected image"))[0]
            require(inspected.get("Os") == "linux" and inspected.get("Architecture") == self.release.arch and
                    reference in (inspected.get("RepoDigests") or []) and
                    re.fullmatch(r"sha256:[a-f0-9]{64}", inspected.get("Id", "")), "Selected image identity/platform mismatch")
            old = self.state["image_ids"].get(service)
            require(old is None or old == inspected["Id"], "The selected image changed during resume")
            self.state["image_ids"][service] = inspected["Id"]
        save_state(self.directory, self.state)
        image_file = self.directory / "runtime/runtime-image.json"
        image_config = json.dumps({"services": {"backend": {"environment": {
            "RUNTIME_IMAGE_DIGEST": self.state["image_ids"]["backend"]}}}}, sort_keys=True).encode()
        if image_file.exists():
            require(regular(image_file).read_bytes() == image_config, "Retained runtime image declaration changed")
        else:
            private_file(image_file, image_config)
        self.state["files"]["runtime/runtime-image.json"] = digest(image_file)
        save_state(self.directory, self.state)
        self.check_rendered_compose()
        if "start-console" in self.state["done"]:
            print("Installation already completed; no services or credentials were changed.")
            return
        self.step("start-databases", self.compose + ["up", "-d", "--wait", "--pull", "never", "--no-build", "postgres", "redis"])
        plan = self.directory / "evidence/install.json"
        if not plan.exists():
            require("fresh-plan" not in self.state["done"], "Original installation plan is missing; restore its evidence")
            self.step("fresh-plan", self.backend("fresh-install", "plan", "--out", "/evidence/install.json"))
        checked = self.command(self.backend("fresh-install", "check", "--plan", "/evidence/install.json"), "check original fresh plan")
        require("fresh_install_check=PASS" in checked, "Fresh owner did not confirm the original plan")
        if "state=completed" not in checked:
            evidence = self.directory / "evidence/publications"
            # The original CLI creates evidence before claiming the database.
            # Resume that retained directory even when DB state is unclaimed.
            verb = "resume" if "state=active" in checked or evidence.exists() or evidence.is_symlink() else "apply"
            require("state=active" in checked or "state=unclaimed" in checked, "Unexpected fresh owner state")
            self.step("fresh-apply", self.backend("fresh-install", verb, "--plan", "/evidence/install.json",
                                                "--evidence-dir", "/evidence/publications"))
        checked = self.command(self.backend("fresh-install", "check", "--plan", "/evidence/install.json"), "verify completed fresh owner")
        require("state=completed" in checked, "Fresh installation has not completed")
        status = fields(self.command(self.backend("upgrade", "status"), "read initial release registration"))
        if not self.release_record_matches(status):
            require(status.get("release_sequence") == "0" and status.get("current_schema") == SCHEMA,
                    "Existing installed release does not match this package")
            self.step("record-release", self.backend("upgrade", "record-applied", "--package", "/install/release.tar.gz",
                                                   "--recorded-by", "community-installer"))
            status = fields(self.command(self.backend("upgrade", "status"), "verify initial release registration"))
            require(self.release_record_matches(status), "Initial release registration did not match")
        if "rotate-administrator" not in self.state["done"]:
            self.step("rotate-administrator", self.backend("admin", "set-password", "--username", "admin",
                      "--org-id", "1", "--password-stdin"), input=(self.directory / "admin-password").read_bytes())
        self.step("start-console", self.compose + ["up", "-d", "--wait", "--pull", "never", "--no-build"])
        print("Private setup console: http://127.0.0.1:" + str(self.port))
        print("Username: admin; password file: " + str(self.directory / "admin-password"))
        print("For ECS, use an SSH tunnel to this loopback port. Configure the customer and model in the console.")
        print("No provider call or public Gateway activation was performed.")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("check", "install", "resume"))
    parser.add_argument("--release", type=Path, default=Path(__file__).resolve().parents[2], help="prepared signed release directory")
    parser.add_argument("--verifier", type=Path, required=True, help="independently trusted uniroute-upgrade executable")
    parser.add_argument("--public-keys", type=Path, required=True, help="independently trusted public-key ring")
    parser.add_argument("--directory", type=Path, default=Path("/opt/unirouter"))
    parser.add_argument("--name", default="unirouter")
    parser.add_argument("--port", type=int, default=3000)
    parser.add_argument("--docker-socket", type=Path, default=Path("/var/run/docker.sock"))
    args = parser.parse_args(argv)
    arch = {"x86_64": "amd64", "aarch64": "arm64", "arm64": "arm64"}.get(platform.machine())
    try:
        require(args.directory.is_absolute() and args.directory == args.directory.resolve(), "Installation directory must be an absolute real path")
        release = Release(args.release.resolve(), args.release.resolve() / "release.tar.gz", args.verifier.resolve(), args.public_keys.resolve(), arch)
        installer = Installer(release, args.directory, args.name, args.port, args.docker_socket)
        if args.action == "check":
            installer.check()
            print("Checks passed. No services, configuration or credentials were created.")
            return 0
        if args.action == "install":
            installer.check()
            installer.prepare()
        else:
            installer.load_state()
        lock_fd = os.open(args.directory / ".install.lock", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        with os.fdopen(lock_fd, "a+b") as lock:
            require(stat.S_ISREG(os.fstat(lock.fileno()).st_mode), "Installation lock must be a regular file")
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            installer.load_state()
            installer.check(resume=True)
            installer.install()
        return 0
    except (InstallError, OSError, ValueError, KeyError, TypeError, tarfile.TarError) as exc:
        # Only installer-authored errors are safe to display; parser/system
        # exceptions can include untrusted input or configuration fragments.
        print("Installation stopped: " + (str(exc) if isinstance(exc, InstallError) else type(exc).__name__), file=sys.stderr)
        print("Keep the original installation directory and plans. No automatic deletion or reset was attempted.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
