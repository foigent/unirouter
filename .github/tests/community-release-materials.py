#!/usr/bin/env python3
"""Select exact existing release material bytes for ordinary full-pack Sources.

This offline helper reads one current preparation and optionally one explicit
previous preparation. It creates no cache, downloads nothing and grants no
license, inclusion, signing or publication authority. Current product/image
obligations and all release-specific provenance remain caller-owned checks.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import stat
import sys

SCHEMA = "unirouter.release-material-inputs.v1"
RESULT = "unirouter.release-material-selection.v1"
MAX_METADATA = 128 * 1024
MAX_FILES = 64
MAX_BYTES = 512 * 1024 * 1024
GROUPS = {
    "runtime": (
        "payload/deploy/install.py", "payload/deploy/upgrade.py", "payload/deploy/recovery.py",
        "payload/deploy/docker-compose.yml", "payload/deploy/.env.example", "payload/deploy/preflight.sh",
        "payload/deploy/config/prometheus.yml",
        "payload/deploy/config/grafana/dashboards/unirouter-overview.json",
        "payload/deploy/config/grafana/provisioning/dashboards/dashboards.yml",
        "payload/deploy/config/grafana/provisioning/datasources/datasource.yml",
        "payload/scripts/source-budget-restore-rebase.sql"),
    "notices": ("payload/licenses/LICENSE", "payload/licenses/NOTICE", "payload/licenses/THIRD-PARTY-NOTICES.txt",
                "payload/licenses/component-notices.tar.gz"),
    "corresponding_source": ("payload/licenses/sources/gcc-15.2.0.tar.xz",),
    "system_materials": ("payload/licenses/system-component-materials.tar.gz",
                         "payload/licenses/system-component-notices-supplement.tar.gz"),
    "release": ("payload/install.json", "payload/docs/RECOVERY.md", "payload/docs/UPSTREAM-SOURCES.md",
                "payload/docs/install-linux.md", "payload/docs/install-linux.zh-CN.md",
                "payload/licenses/source-disposition.json", "payload/licenses/frontend-disposition.json",
                "payload/provenance/build-provenance.json"),
}
OWNER = {path: group for group, paths in GROUPS.items() for path in paths}


class Stop(Exception):
    pass


def need(ok, label):
    if not ok:
        raise Stop(label)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()


def strict_json(raw):
    need(len(raw) <= MAX_METADATA, "material_metadata_bound")
    def pairs(items):
        result = {}
        for key, value in items:
            need(key not in result, "material_duplicate_field")
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=pairs,
                      parse_constant=lambda _: need(False, "material_nonfinite_number"))


def metadata(path):
    with regular(path).open("rb") as stream:
        return strict_json(stream.read(MAX_METADATA + 1))


def regular(path):
    info = path.lstat()
    need(stat.S_ISREG(info.st_mode) and not path.is_symlink(), "material_regular_file_required")
    return path


def child(root, name):
    pure = PurePosixPath(name)
    need(name in OWNER and pure.as_posix() == name and not pure.is_absolute()
         and all(part not in (".", "..") for part in pure.parts), "material_path_scope")
    path = root
    for part in pure.parts:
        path = path / part
        need(not path.is_symlink(), "material_symlink_refused")
    return path


def normalize(rows, *, current):
    need(type(rows) is list and 0 < len(rows) <= MAX_FILES, "material_inventory_bound")
    result = {}; total = 0
    for row in rows:
        need(type(row) is dict and {"path", "size", "sha256"} <= set(row)
             and set(row) <= {"path", "size", "sha256", "media_type"}, "material_artifact_fields")
        path, size, digest = row["path"], row["size"], row["sha256"]
        need(type(path) is str and path in OWNER and path not in result, "material_path_scope")
        need(type(size) is int and 0 <= size <= MAX_BYTES, "material_file_bound")
        need(type(digest) is str and len(digest) == 64 and all(c in "0123456789abcdef" for c in digest), "material_digest")
        media = row.get("media_type", "")
        need(type(media) is str and len(media) <= 128 and all(32 < ord(c) < 127 for c in media), "material_media_type")
        result[path] = {"path": path, "size": size, "sha256": digest, "media_type": media}
        total += size
    need(total <= MAX_BYTES, "material_total_bound")
    if current:
        for name, paths in GROUPS.items():
            selected = set(paths) & set(result)
            if selected and name != "release":
                need(selected == set(paths), "material_group_incomplete")
    return result


def group_digest(files):
    value = {"schema_version": SCHEMA, "files": sorted(files, key=lambda row: row["path"])}
    return hashlib.sha256(canonical(value)).hexdigest()


def root_path(path):
    need(path.is_absolute() and path == path.resolve(strict=True) and not path.is_symlink()
         and path.is_dir(), "material_root_identity")
    return path


def membership(root, inventory, *, previous):
    # Validate the closed package namespace, without traversing unrelated files
    # in a retained release/owner directory. A current warm preparation may omit
    # large files supplied by an exact previous group; undeclared extras refuse.
    payload = root / "payload"
    actual = set()
    directories = {parent.as_posix() for name in inventory for parent in PurePosixPath(name).parents if parent.as_posix() != "."}
    if payload.exists():
        need(not payload.is_symlink(), "material_symlink_refused")
        for path in payload.rglob("*"):
            need(not path.is_symlink(), "material_symlink_refused")
            if path.is_file():
                actual.add(path.relative_to(root).as_posix())
            else:
                need(path.is_dir() and path.relative_to(root).as_posix() in directories, "material_directory_scope")
            need(len(actual) <= MAX_FILES, "material_membership_bound")
    need(actual <= set(inventory), "material_undeclared_member")
    if previous:
        need(actual == set(inventory), "material_previous_incomplete")


def verify_file(path, expected):
    regular(path)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    digest = hashlib.sha256(); size = 0
    with os.fdopen(fd, "rb") as stream:
        info = os.fstat(stream.fileno())
        need(stat.S_ISREG(info.st_mode), "material_regular_file_required")
        while block := stream.read(1024 * 1024):
            size += len(block)
            need(size <= expected["size"], "material_bytes_changed")
            digest.update(block)
        final = os.fstat(stream.fileno())
    observed = path.lstat()
    identity = lambda item: (item.st_dev, item.st_ino, item.st_size, item.st_mtime_ns, item.st_ctime_ns)
    need(identity(info) == identity(final) == identity(observed)
         and size == expected["size"] and digest.hexdigest() == expected["sha256"], "material_bytes_changed")


def select_sources(request, current_root, previous_root=None, previous_manifest=None):
    need(type(request) is dict and set(request) == {"schema_version", "artifacts"}
         and request["schema_version"] == SCHEMA, "material_request_shape")
    wanted = normalize(request["artifacts"], current=True)
    current_root = root_path(current_root)
    membership(current_root, wanted, previous=False)
    need((previous_root is None) == (previous_manifest is None), "material_previous_pair_required")
    prior = {}
    if previous_root is not None:
        previous_root = root_path(previous_root)
        # Use the ordinary signed manifest's artifact inventory. This helper
        # does not validate its signature or promote it to release authority.
        need(type(previous_manifest) is dict and "artifacts" in previous_manifest, "material_previous_manifest")
        prior = normalize(previous_manifest["artifacts"], current=False)
        membership(previous_root, prior, previous=True)
    sources = []; groups = []
    for name in GROUPS:
        files = sorted((row for path, row in wanted.items() if OWNER[path] == name), key=lambda row: row["path"])
        if not files:
            continue
        digest = group_digest(files)
        old = [row for path, row in prior.items() if OWNER[path] == name]
        reused = name != "release" and previous_root is not None and group_digest(old) == digest
        for row in files:
            current_path = child(current_root, row["path"])
            # Present current inputs must still match their requested identity;
            # a stale source inventory cannot hide drift behind a warm hit.
            if current_path.exists():
                verify_file(current_path, row)
            path = child(previous_root, row["path"]) if reused else current_path
            verify_file(path, row)
            sources.append(dict(row, file_path=str(path)))
        groups.append({"group": name, "sha256": digest, "reused": reused,
                       "files": [row["path"] for row in files], "bytes": sum(row["size"] for row in files)})
    return {"schema_version": RESULT, "groups": groups, "sources": sorted(sources, key=lambda row: row["path"]),
            "artifacts": sorted(wanted.values(), key=lambda row: row["path"])}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inventory", type=Path, required=True)
    parser.add_argument("--current", type=Path, required=True)
    parser.add_argument("--previous", type=Path)
    parser.add_argument("--previous-manifest", type=Path)
    parser.add_argument("--artifact-inventory-out", type=Path, help="new private ordinary Artifact array for native pack --expected-artifacts")
    args = parser.parse_args(argv)
    try:
        result = select_sources(metadata(args.inventory), args.current, args.previous,
                                metadata(args.previous_manifest) if args.previous_manifest else None)
        if args.artifact_inventory_out is not None:
            fd = os.open(args.artifact_inventory_out, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            with os.fdopen(fd, "wb") as stream:
                stream.write(canonical(result["artifacts"])); stream.flush(); os.fsync(stream.fileno())
        print(canonical(result).decode())
        return 0
    except (Stop, OSError, ValueError, TypeError, KeyError):
        print("Release material selection refused; preserve original inputs.", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
