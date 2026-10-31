#!/usr/bin/env python3
"""Prepare or install an independently pinned UniRouter community release.

Obtain this script, the bootstrap JSON and its SHA-256 through the reviewed,
commit-pinned release instructions. This candidate has no official download URL.
"""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import stat
import subprocess
import sys
import tarfile
import urllib.parse
import urllib.request

SCHEMA = "000129_upstream_observation_restore_namespace"
MAX_BOOTSTRAP = 32 * 1024
MAX_MANIFEST = 128 * 1024
MAX_SIGNATURE = 16 * 1024
MAX_PAYLOAD = 512 * 1024 * 1024
MAX_TAR_PADDING = 10240
ASSETS = {"verifier": ("uniroute-upgrade", 32 * 1024 * 1024),
          "public_keys": ("public-keys.json", 64 * 1024),
          "package": ("release.tar.gz", MAX_PAYLOAD)}
INSTALLER = "payload/deploy/install.py"
UPGRADER = "payload/deploy/upgrade.py"
UPGRADE_ACTIONS = ("check-upgrade", "upgrade", "resume-upgrade", "upgrade-status")
RECEIPT = "prepared.json"


class BootstrapError(Exception):
    """Only static, bootstrap-authored errors are safe to display."""


def require(condition, message):
    if not condition:
        raise BootstrapError(message)


def exact_keys(value, keys):
    require(isinstance(value, dict) and set(value) == set(keys), "Unexpected metadata fields.")


def strict_json(raw):
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, "Duplicate JSON field.")
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=pairs,
                      parse_constant=lambda _: require(False, "Invalid JSON constant."))


def valid_digest(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None


def https_url(value):
    require(isinstance(value, str) and value and not any(ord(c) <= 32 or ord(c) == 127 for c in value),
            "Invalid asset URL.")
    parsed = urllib.parse.urlsplit(value)
    require(parsed.scheme == "https" and parsed.hostname and parsed.username is None
            and parsed.password is None and not parsed.fragment, "Only credential-free HTTPS URLs are accepted.")
    # Accessing port also validates malformed port syntax before any request.
    require(parsed.port in (None, 443), "Only standard HTTPS ports are accepted.")
    return value


class HTTPSRedirects(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        https_url(newurl)
        return super().redirect_request(req, fp, code, msg, headers, newurl)


def open_regular(path, private=False):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1, "A regular, unlinked file is required.")
        if private:
            require(info.st_uid == os.geteuid() and not info.st_mode & 0o022,
                    "Prepared files must belong to the current user and deny other writers.")
        return os.fdopen(fd, "rb")
    except BaseException:
        os.close(fd)
        raise


def read_bounded(path, maximum, private=False):
    with open_regular(path, private) as stream:
        raw = stream.read(maximum + 1)
    require(len(raw) <= maximum, "Metadata exceeds its size limit.")
    return raw


def file_identity(path, private=True):
    with open_regular(path, private) as stream:
        size = os.fstat(stream.fileno()).st_size
        require(size <= MAX_PAYLOAD, "Prepared file exceeds the online entry's size limit.")
        digest = hashlib.sha256()
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return size, digest.hexdigest()


def write_new(path, raw):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())


def protected_directory(path, *, exists):
    require(path.is_absolute() and path == path.resolve(), "Use an absolute path without symlinks.")
    require(os.path.lexists(path) == exists, "Preparation needs a new directory; reuse needs a completed directory.")
    for index, item in enumerate([path, *path.parents]):
        if index == 0 and not exists:
            continue
        info = item.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid in (0, os.geteuid()),
                "Directory ancestry can be replaced by another user.")
        require(not info.st_mode & 0o022 or (info.st_uid == 0 and info.st_mode & stat.S_ISVTX),
                "Directory ancestry allows other writers without root-owned sticky protection.")
        if index == 0:
            require(info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o700,
                    "Reuse requires the same owner and a private 0700 directory; no automatic chown is performed.")


class Bootstrap:
    def __init__(self, raw, expected_sha256):
        require(valid_digest(expected_sha256) and hashlib.sha256(raw).hexdigest() == expected_sha256,
                "Bootstrap SHA-256 does not match the independently trusted value.")
        require(0 < len(raw) <= MAX_BOOTSTRAP, "Bootstrap exceeds its size limit.")
        value = strict_json(raw)
        exact_keys(value, ("schema_version", "platform", "release", "assets"))
        require(value["schema_version"] == "unirouter.community-bootstrap.v1", "Unsupported bootstrap schema.")
        exact_keys(value["platform"], ("os", "arch"))
        require(value["platform"] == {"os": "linux", "arch": "amd64"},
                "This online preview supports Linux amd64 only.")
        release = value["release"]
        exact_keys(release, ("version", "commit", "release_sequence", "key_id", "target_schema"))
        require(isinstance(release["version"], str) and re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", release["version"]),
                "Invalid product version.")
        require(isinstance(release["commit"], str) and re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", release["commit"]),
                "A full source commit is required.")
        require(type(release["release_sequence"]) is int and 0 < release["release_sequence"] < 2 ** 63,
                "Invalid release sequence.")
        require(isinstance(release["key_id"], str) and re.fullmatch(r"[A-Za-z0-9._-]{1,64}", release["key_id"]),
                "Invalid signing key identifier.")
        require(release["target_schema"] == SCHEMA, "This entry requires the current artifact fresh-install contract.")
        exact_keys(value["assets"], ASSETS)
        for name, (_, maximum) in ASSETS.items():
            asset = value["assets"][name]
            exact_keys(asset, ("url", "size", "sha256"))
            https_url(asset["url"])
            require(type(asset["size"]) is int and 0 < asset["size"] <= maximum
                    and valid_digest(asset["sha256"]), "Invalid bounded asset identity.")
        self.raw, self.sha, self.value = raw, expected_sha256, value
        self.release, self.assets = release, value["assets"]

    def host_check(self):
        require(platform.system() == "Linux" and platform.machine() in ("x86_64", "amd64"),
                "Use a Linux amd64 server for this preview.")


def download(asset, destination):
    opener = urllib.request.build_opener(HTTPSRedirects())
    request = urllib.request.Request(https_url(asset["url"]), headers={"Accept-Encoding": "identity"})
    with opener.open(request, timeout=30) as response:
        https_url(response.geturl())
        require(response.status == 200 and response.headers.get("Content-Encoding", "identity") == "identity",
                "Asset download did not return an unencoded complete response.")
        length = response.headers.get("Content-Length")
        require(length is None or (length.isdecimal() and int(length) == asset["size"]),
                "Asset response length does not match its pinned size.")
        fd = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "wb") as stream:
            count, digest = 0, hashlib.sha256()
            while True:
                chunk = response.read(min(1024 * 1024, asset["size"] - count + 1))
                if not chunk:
                    break
                count += len(chunk)
                require(count <= asset["size"], "Asset download exceeds its pinned size.")
                digest.update(chunk)
                stream.write(chunk)
            require(count == asset["size"] and digest.hexdigest() == asset["sha256"],
                    "Asset download is truncated or has the wrong SHA-256.")
            stream.flush()
            os.fsync(stream.fileno())


def payload_path(value):
    require(isinstance(value, str) and 0 < len(value) <= 240
            and not any(ord(c) < 32 or ord(c) == 127 for c in value) and "\\" not in value,
            "Unsafe payload path.")
    path = PurePosixPath(value)
    require(not path.is_absolute() and value == path.as_posix() and path.parts[0] == "payload"
            and len(path.parts) > 1 and not any(p in (".", "..") for p in path.parts), "Unsafe payload path.")
    return value


def manifest_files(raw):
    value = strict_json(raw)
    require(isinstance(value, dict) and isinstance(value.get("artifacts"), list)
            and 0 < len(value["artifacts"]) <= 64, "Invalid bounded artifact list.")
    files, total = {}, 0
    for item in value["artifacts"]:
        require(isinstance(item, dict) and {"path", "size", "sha256"} <= set(item)
                and set(item) <= {"path", "size", "sha256", "media_type"}, "Invalid artifact fields.")
        name = payload_path(item["path"])
        require(name not in files and type(item["size"]) is int and 0 <= item["size"] <= MAX_PAYLOAD
                and valid_digest(item["sha256"]), "Invalid or duplicate artifact identity.")
        total += item["size"]
        require(total <= MAX_PAYLOAD, "Payload exceeds the online preparation limit.")
        files[name] = (item["size"], item["sha256"])
    for name in files:
        require(not any(str(parent) in files for parent in PurePosixPath(name).parents),
                "Payload file/directory paths collide.")
    require(INSTALLER in files, "Signed installer is missing.")
    return value, files


def scan_archive(package, destination=None):
    """Read physical USTAR headers before tar readers can consume extensions.

    Used first without writes for resource/structure checks, then after signature
    verification to write declared bytes. TarInfo.frombuf validates standard
    checksums and names; no extract/extractall or archive permissions are used.
    """
    files, seen, metadata = {}, set(), {}
    total = 0
    limit = MAX_PAYLOAD + MAX_MANIFEST + MAX_SIGNATURE + 66 * 1024 + MAX_TAR_PADDING
    with open_regular(package, True) as source, gzip.GzipFile(fileobj=source) as archive:
        def read(count):
            nonlocal total
            require(count >= 0 and total + count <= limit, "Total uncompressed archive exceeds its limit.")
            raw = archive.read(count)
            total += len(raw)
            require(len(raw) == count, "Truncated archive.")
            return raw
        while True:
            header = read(512)
            if header == bytes(512):
                require(read(512) == bytes(512), "Invalid archive end marker.")
                tail = archive.read(MAX_TAR_PADDING + 1)
                require(len(tail) <= MAX_TAR_PADDING and not any(tail), "Unexpected archive trailing data or padding.")
                break
            require(len(seen) < 66 and header[257:263] == b"ustar\0" and header[263:265] == b"00"
                    and header[156:157] in (b"0", b"\0"), "Only bounded physical USTAR regular entries are supported.")
            member = tarfile.TarInfo.frombuf(header, "utf-8", "strict")
            require(not member.linkname and not member.pax_headers and member.size >= 0,
                    "Archive links and extended metadata are not supported.")
            name = member.name
            require(name not in seen, "Duplicate archive entry.")
            if len(seen) == 0:
                require(name == "manifest.json" and 0 < member.size <= MAX_MANIFEST, "Missing bounded manifest.")
            elif len(seen) == 1:
                require(name == "manifest.sig" and 0 < member.size <= MAX_SIGNATURE, "Missing bounded signature.")
            else:
                require(name == payload_path(name) and name in files and member.size == files[name][0],
                        "Undeclared payload or size mismatch.")
            seen.add(name)
            if name in ("manifest.json", "manifest.sig"):
                data = read(member.size)
                metadata[name] = data
                if name == "manifest.json":
                    _, files = manifest_files(data)
                if destination is not None:
                    write_new(destination / name, data)
            else:
                stream = None
                try:
                    if destination is not None:
                        target = destination / name
                        parent = destination
                        for part in PurePosixPath(name).parts[:-1]:
                            parent = parent / part
                            try:
                                parent.mkdir(mode=0o700)
                            except FileExistsError:
                                protected_directory(parent, exists=True)
                        fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
                        stream = os.fdopen(fd, "wb")
                    remaining, digest = member.size, hashlib.sha256()
                    while remaining:
                        data = read(min(1024 * 1024, remaining))
                        digest.update(data)
                        remaining -= len(data)
                        if stream is not None:
                            stream.write(data)
                    require(digest.hexdigest() == files[name][1], "Payload SHA-256 mismatch.")
                    if stream is not None:
                        stream.flush()
                        os.fsync(stream.fileno())
                finally:
                    if stream is not None:
                        stream.close()
            require(not any(read((-member.size) % 512)), "Invalid archive alignment padding.")
    require(seen == {"manifest.json", "manifest.sig", *files}, "Archive is missing declared entries.")
    for name, raw in metadata.items():
        files[name] = (len(raw), hashlib.sha256(raw).hexdigest())
    return metadata["manifest.json"], files


def child_environment():
    return {key: os.environ[key] for key in ("PATH", "HOME", "LANG", "LC_ALL") if key in os.environ}


def verifier_output(args):
    result = subprocess.run([str(a) for a in args], stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            env=child_environment(), check=False)
    require(result.returncode == 0, "Signed release verification failed; child output was not logged.")
    fields = {}
    for line in result.stdout.decode("utf-8", errors="strict").splitlines():
        require("=" in line, "Invalid verifier response.")
        key, value = line.split("=", 1)
        require(key not in fields, "Duplicate verifier response field.")
        fields[key] = value
    return fields


def verify_release(bootstrap, directory, verify=verifier_output):
    for name, (filename, _) in ASSETS.items():
        asset = bootstrap.assets[name]
        require(file_identity(directory / filename) == (asset["size"], asset["sha256"]),
                "Prepared asset differs from its independently pinned identity.")
    manifest_raw, files = scan_archive(directory / "release.tar.gz")
    verified = verify([directory / "uniroute-upgrade", "verify", "--public-keys", directory / "public-keys.json",
                       "--package", directory / "release.tar.gz", "--target-os", "linux", "--target-arch", "amd64",
                       "--current-sequence", "0", "--current-schema", SCHEMA,
                       "--max-payload-bytes", str(MAX_PAYLOAD)])
    manifest_sha = hashlib.sha256(manifest_raw).hexdigest()
    require(verified.get("verified") == "true" and verified.get("package_sha256") == bootstrap.assets["package"]["sha256"]
            and verified.get("manifest_sha256") == manifest_sha, "Verifier did not bind the exact package and manifest.")
    # key_id is not a verifier output field: read it only from the bound manifest.
    manifest, _ = manifest_files(manifest_raw)
    for key, value in bootstrap.release.items():
        require(manifest.get(key) == value, "Signed release differs from the trusted bootstrap identity.")
        if key != "key_id":
            require(verified.get(key) == str(value), "Verifier release identity mismatch.")
    require(manifest.get("target_os") == "linux" and manifest.get("target_arch") == "amd64"
            and verified.get("target") == "linux/amd64", "Signed platform mismatch.")
    require(file_identity(directory / "release.tar.gz") ==
            (bootstrap.assets["package"]["size"], bootstrap.assets["package"]["sha256"]),
            "Package changed during verification.")
    receipt = {"schema_version": "unirouter.community-prepared.v1", "bootstrap_sha256": bootstrap.sha,
               "package_sha256": bootstrap.assets["package"]["sha256"], "manifest_sha256": manifest_sha}
    return files, receipt


def validate_payload(directory, files, receipt=None):
    allowed = set(files) | {filename for filename, _ in ASSETS.values()} | {"bootstrap.json", RECEIPT}
    allowed_dirs = {str(parent) for name in allowed for parent in PurePosixPath(name).parents if str(parent) != "."}
    for parent, directories, filenames in os.walk(directory, followlinks=False):
        for name in directories:
            item = Path(parent) / name
            require(not item.is_symlink() and str(item.relative_to(directory)) in allowed_dirs,
                    "Unexpected directory in prepared release.")
            protected_directory(item, exists=True)
        for name in filenames:
            item = Path(parent) / name
            require(str(item.relative_to(directory)) in allowed, "Undeclared file in prepared release.")
            with open_regular(item, True):
                pass
    for name, identity in files.items():
        require(file_identity(directory / name) == identity, "Prepared payload changed after verification.")
    if receipt is not None:
        require(strict_json(read_bounded(directory / RECEIPT, MAX_BOOTSTRAP, True)) == receipt,
                "Preparation did not finish or its completion receipt differs.")


def prepare(bootstrap, directory, fetch=download, verify=verifier_output):
    bootstrap.host_check()
    protected_directory(directory, exists=False)
    directory.mkdir(mode=0o700)
    write_new(directory / "bootstrap.json", bootstrap.raw)
    for name, (filename, _) in ASSETS.items():
        fetch(bootstrap.assets[name], directory / filename)
        require(file_identity(directory / filename) == (bootstrap.assets[name]["size"], bootstrap.assets[name]["sha256"]),
                "Downloaded asset does not match its pinned identity.")
    os.chmod(directory / "uniroute-upgrade", 0o700)
    files, receipt = verify_release(bootstrap, directory, verify)
    extracted_manifest, extracted_files = scan_archive(directory / "release.tar.gz", destination=directory)
    require(extracted_files == files and hashlib.sha256(extracted_manifest).hexdigest() == receipt["manifest_sha256"],
            "Archive changed during preparation.")
    require(file_identity(directory / "release.tar.gz") ==
            (bootstrap.assets["package"]["size"], bootstrap.assets["package"]["sha256"]), "Package changed during preparation.")
    validate_payload(directory, files)
    write_new(directory / RECEIPT, (json.dumps(receipt, sort_keys=True) + "\n").encode())
    return receipt


def check_prepared(bootstrap, directory, verify=verifier_output):
    bootstrap.host_check()
    protected_directory(directory, exists=True)
    require(read_bounded(directory / "bootstrap.json", MAX_BOOTSTRAP, True) == bootstrap.raw,
            "Retained bootstrap differs from the trusted input.")
    require((directory / RECEIPT).is_file(), "Partial preparation cannot be resumed or installed.")
    files, receipt = verify_release(bootstrap, directory, verify)
    validate_payload(directory, files, receipt)
    return receipt


def delegate(action, directory, installation, name, port, docker_socket):
    args = [sys.executable, "-I", "-B", str(directory / INSTALLER), action,
            "--release", str(directory), "--verifier", str(directory / "uniroute-upgrade"),
            "--public-keys", str(directory / "public-keys.json"), "--directory", str(installation),
            "--name", name, "--port", str(port), "--docker-socket", str(docker_socket)]
    # Preserve the existing installer's sanitized progress and errors.
    return subprocess.run(args, env=child_environment(), check=False).returncode


def delegate_upgrade(action, directory, installation, source_package, layout, edge_directory, historical_exits=None):
    require(action in UPGRADE_ACTIONS, "Unsupported finite upgrade action.")
    require((directory / UPGRADER).is_file(), "This signed release has no community upgrade entry.")
    args = [sys.executable, "-I", "-B", str(directory / UPGRADER), action,
            "--release", str(directory), "--verifier", str(directory / "uniroute-upgrade"),
            "--public-keys", str(directory / "public-keys.json"), "--directory", str(installation),
            "--layout", layout]
    if source_package is not None:
        args += ["--source-package", str(source_package)]
    if edge_directory is not None:
        args += ["--edge-directory", str(edge_directory)]
    if historical_exits is not None:
        args += ["--historical-exits", str(historical_exits)]
    return subprocess.run(args, env=child_environment(), check=False).returncode


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("prepare", "check", "install", "resume", *UPGRADE_ACTIONS))
    parser.add_argument("--bootstrap", type=Path, required=True, help="independently obtained, pinned bootstrap JSON")
    parser.add_argument("--bootstrap-sha256", required=True, help="SHA-256 from the independent trusted release instructions")
    parser.add_argument("--directory", type=Path, required=True, help="new private prepared-release directory; retain for resume")
    parser.add_argument("--install-directory", type=Path, default=Path("/opt/unirouter"))
    parser.add_argument("--name", default="unirouter")
    parser.add_argument("--port", type=int, default=3000)
    parser.add_argument("--docker-socket", type=Path, default=Path("/var/run/docker.sock"))
    parser.add_argument("--source-package", type=Path, help="retained exact currently installed signed full package")
    parser.add_argument("--layout", choices=("community_single", "ur_edge_pair"), default="community_single")
    parser.add_argument("--edge-directory", type=Path, help="original named UR edge configuration directory")
    parser.add_argument("--historical-exits", type=Path, help="private original observations for known historical M127 exits")
    args = parser.parse_args(argv)
    os.umask(0o077)
    try:
        require(sys.version_info >= (3, 10), "Python 3.10 or later is required.")
        require(args.action == "prepare" or os.geteuid() == 0, "Run prepare and installation as the same root user.")
        bootstrap = Bootstrap(read_bounded(args.bootstrap, MAX_BOOTSTRAP), args.bootstrap_sha256)
        if args.action == "prepare" or (args.action == "install" and not os.path.lexists(args.directory)):
            prepare(bootstrap, args.directory)
        else:
            check_prepared(bootstrap, args.directory)
        if args.action == "prepare":
            print("Release prepared and verified. No installer, services or credentials were started.")
            return 0
        if args.action in UPGRADE_ACTIONS:
            return delegate_upgrade(args.action, args.directory, args.install_directory, args.source_package, args.layout, args.edge_directory, args.historical_exits)
        return delegate(args.action, args.directory, args.install_directory, args.name, args.port, args.docker_socket)
    except (Exception, KeyboardInterrupt) as exc:
        print("Community bootstrap stopped: " + (str(exc) if isinstance(exc, BootstrapError) else type(exc).__name__), file=sys.stderr)
        print("Keep existing files and original installation plans. No automatic deletion or reset was attempted.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
