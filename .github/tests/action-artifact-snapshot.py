"""Freeze the complete allowlisted input to Actions' bounded ZIP uploader."""
import argparse
import os
import re
import stat
import tempfile
from pathlib import Path

MIB = 1024 * 1024
KINDS = {
    "coverage": (8 * MIB, "coverage.out"),
    "browser": (3 * MIB, "ur-customer-overview-browser"),
    "installer": (5 * MIB // 4, "ur-community-native-report.json"),
    "maintenance": (5 * MIB // 4, "ur-maintenance-native-report.json"),
}
BROWSER_PATH = re.compile(
    r"overview/(?:database-invariants\.json|sql-api-dom\.json|[a-zA-Z0-9_-]+\.png|"
    r"navigation/(?:result\.json|[a-zA-Z0-9_-]+\.png))|"
    r"community-first-use/(?:browser-result\.json|browser-process\.log|"
    r"resolved-rate-quote\.json|sql-authority\.json|request-refusal\.png)"
)


class ArtifactError(Exception):
    pass


def directory_error(error):
    raise error


def inventory(source, kind):
    try:
        mode = source.lstat().st_mode
    except FileNotFoundError:
        if kind == "coverage":
            raise ArtifactError("Missing required coverage file.") from None
        return {}
    if kind != "browser":
        if not stat.S_ISREG(mode):
            raise ArtifactError("Expected a regular report file.")
        return {source.name: source.lstat()}
    if not stat.S_ISDIR(mode):
        raise ArtifactError("Expected an ordinary browser evidence directory.")
    reports = {}
    for root, directories, names in os.walk(source, onerror=directory_error, followlinks=False):
        for name in directories:
            directory = Path(root) / name
            if directory.relative_to(source).as_posix() not in (
                "overview", "overview/navigation", "community-first-use"
            ) or not stat.S_ISDIR(directory.lstat().st_mode):
                raise ArtifactError("Unexpected browser evidence directory.")
        for name in names:
            file = Path(root) / name
            relative = file.relative_to(source).as_posix()
            metadata = file.lstat()
            if (len(relative) > 256 or not BROWSER_PATH.fullmatch(relative)
                    or not stat.S_ISREG(metadata.st_mode) or len(reports) >= 256):
                raise ArtifactError("Unexpected browser evidence file.")
            reports[relative] = metadata
    return reports


def signature(metadata):
    return (metadata.st_dev, metadata.st_ino, metadata.st_mode,
            metadata.st_size, metadata.st_mtime_ns, metadata.st_ctime_ns)


def zip_upper_bound(reports):
    # Raw deflate overhead is well below 12.5% plus the per-entry allowance.
    # Account for both filename copies and allow 4 KiB per entry for headers,
    # descriptors and extras, plus 32 KiB for archive-level structures.
    return 32 * 1024 + sum(
        item.st_size + (item.st_size + 7) // 8 + 2 * len(name.encode("ascii")) + 4096
        for name, item in reports.items()
    )


def freeze(source, kind, scratch):
    cap, expected_name = KINDS[kind]
    if source.name != expected_name:
        raise ArtifactError("Unexpected artifact source.")
    reports = inventory(source, kind)
    if not reports:
        return None
    upper = zip_upper_bound(reports)
    if upper > cap:
        raise ArtifactError(f"Full artifact ZIP bound {upper} exceeds {cap} bytes.")
    stage = Path(tempfile.mkdtemp(prefix="ur-artifact-", dir=scratch))
    for name, metadata in sorted(reports.items()):
        original = source / name if kind == "browser" else source
        frozen = stage / name
        frozen.parent.mkdir(parents=True, exist_ok=True)
        with os.fdopen(os.open(original, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK), "rb") as body:
            if signature(os.fstat(body.fileno())) != signature(metadata):
                raise ArtifactError("Artifact file changed before snapshot.")
            with frozen.open("xb") as destination:
                remaining = metadata.st_size
                while remaining:
                    chunk = body.read(min(remaining, MIB))
                    if not chunk:
                        raise ArtifactError("Artifact file was truncated during snapshot.")
                    destination.write(chunk)
                    remaining -= len(chunk)
                if body.read(1):
                    raise ArtifactError("Artifact file grew during snapshot.")
            if signature(os.fstat(body.fileno())) != signature(metadata):
                raise ArtifactError("Artifact file changed during snapshot.")
        if frozen.stat().st_size != metadata.st_size:
            raise ArtifactError("Snapshot file size differs.")
        frozen.chmod(0o444)
    if {k: signature(v) for k, v in inventory(source, kind).items()} != {
        k: signature(v) for k, v in reports.items()
    }:
        raise ArtifactError("Artifact inventory changed during snapshot.")
    for root, _, _ in os.walk(stage, topdown=False):
        Path(root).chmod(0o555)
    print(f"Frozen {len(reports)} complete files, ZIP upper bound {upper}/{cap} bytes.")
    return stage if kind == "browser" else stage / expected_name


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("kind", choices=KINDS)
    parser.add_argument("source", type=Path)
    args = parser.parse_args()
    try:
        snapshot = freeze(args.source, args.kind, Path(os.environ["RUNNER_TEMP"]))
        if snapshot is not None:
            with Path(os.environ["GITHUB_OUTPUT"]).open("a") as outputs:
                outputs.write(f"path={snapshot}\n")
    except ArtifactError as exc:
        raise SystemExit(str(exc)) from None
    except OSError:
        raise SystemExit("Could not freeze the complete artifact.") from None


if __name__ == "__main__":
    main()
