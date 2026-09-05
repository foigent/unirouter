#!/usr/bin/env python3
"""Package only an exact tracked main source; CI creates no deployment authority."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess


def main():
    path = os.environ["BUNDLE_PATH"]
    if not re.fullmatch(r"capability-bundles/[a-z0-9][a-z0-9._-]*\.json", path):
        raise ValueError("Bundle must be an exact capability-bundles JSON path")
    source = Path(path)
    if source.is_symlink() or not source.is_file():
        raise ValueError("Bundle source must be a regular file")
    commit = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
    tree = subprocess.check_output(["git", "rev-parse", "HEAD^{tree}"], text=True).strip()
    if os.environ.get("GITHUB_REF") != "refs/heads/main" or commit != os.environ.get("GITHUB_SHA"):
        raise ValueError("CI checkout is not the exact main workflow head")
    wire = subprocess.check_output(["git", "show", f"{commit}:{path}"])
    if wire != source.read_bytes() or not 0 < len(wire) <= 16 << 20:
        raise ValueError("Bundle source differs from tracked commit bytes or exceeds limit")
    digest = hashlib.sha256(wire).hexdigest()
    output = Path("/tmp/unirouter-capability-carrier")
    output.mkdir(mode=0o700, exist_ok=False)
    (output / "bundle.json").write_bytes(wire)
    provenance = {"repository": os.environ["GITHUB_REPOSITORY"], "commit": commit, "tree": tree,
                  "path": path, "bundle_digest": digest, "workflow_run_id": int(os.environ["GITHUB_RUN_ID"]),
                  "run_attempt": int(os.environ["GITHUB_RUN_ATTEMPT"])}
    (output / "provenance.json").write_text(json.dumps(provenance, sort_keys=True, separators=(",", ":")))
    with open(os.environ["GITHUB_OUTPUT"], "a") as stream:
        stream.write(f"digest={digest}\n")


if __name__ == "__main__":
    main()
