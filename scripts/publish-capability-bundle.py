#!/usr/bin/env python3
"""Verify official private-repository CI bytes, then use the single Target owner.

The existing authenticated operator/SSH boundary approves exact source bytes.
No GitHub credential is copied to the Target; no HTTP JSON upload is provided.
The default action only verifies and plans. --apply consumes the exact saved Plan.
"""
import argparse
import base64
import hashlib
import io
import json
from pathlib import Path
import re
import shlex
import subprocess
import zipfile

REPOSITORY = "foigent/unirouter"
WORKFLOW = ".github/workflows/capability-bundle.yml"
MAXIMUM = 16 << 20


def execute(arguments, payload=None, timeout=60):
    result = subprocess.run(arguments, input=payload, capture_output=True, timeout=timeout, check=False)
    if result.returncode:
        raise RuntimeError(f"{arguments[0]} failed; no credential-bearing diagnostics are emitted")
    return result.stdout


def api(path, raw=False):
    wire = execute(["gh", "api", "--hostname", "github.com", f"repos/{REPOSITORY}/{path}"])
    if len(wire) > (MAXIMUM + (4 << 20)) * 2:
        raise ValueError("GitHub response exceeds bounded carrier size")
    return wire if raw else json.loads(wire)


def verify_source(run, artifact, commit, path, expected_digest):
    if not re.fullmatch(r"[a-f0-9]{40}", commit) or not re.fullmatch(r"[a-f0-9]{64}", expected_digest):
        raise ValueError("exact commit and Bundle digest are required")
    if not re.fullmatch(r"capability-bundles/[a-z0-9][a-z0-9._-]*\.json", path) or run <= 0 or artifact <= 0:
        raise ValueError("exact CI run, artifact and tracked Bundle path are required")
    execution = api(f"actions/runs/{run}")
    repository_id = execution.get("repository", {}).get("id")
    if (execution.get("id") != run or execution.get("status") != "completed" or execution.get("conclusion") != "success"
            or execution.get("event") != "workflow_dispatch" or execution.get("head_branch") != "main"
            or execution.get("head_sha") != commit or execution.get("path") != WORKFLOW
            or execution.get("repository", {}).get("full_name") != REPOSITORY
            or not repository_id or execution.get("head_repository", {}).get("id") != repository_id):
        raise ValueError("CI run is not the exact successful trusted main workflow")
    item = api(f"actions/artifacts/{artifact}")
    origin = item.get("workflow_run", {})
    if (item.get("id") != artifact or item.get("expired") is not False
            or item.get("name") != f"capability-bundle-{expected_digest}"
            or not 0 < item.get("size_in_bytes", 0) <= MAXIMUM + (1 << 20)
            or origin.get("id") != run or origin.get("head_sha") != commit
            or origin.get("repository_id") != repository_id or origin.get("head_repository_id") != repository_id):
        raise ValueError("CI artifact identity differs from the approved source")
    archive = api(f"actions/artifacts/{artifact}/zip", raw=True)
    archive_digest = "sha256:" + hashlib.sha256(archive).hexdigest()
    if len(archive) != item["size_in_bytes"] or archive_digest != item.get("digest"):
        raise ValueError("CI archive digest/size mismatch")
    with zipfile.ZipFile(io.BytesIO(archive)) as package:
        names = package.namelist()
        if sorted(names) != ["bundle.json", "provenance.json"] or any(package.getinfo(name).file_size > MAXIMUM for name in names):
            raise ValueError("unexpected or oversized CI artifact members")
        wire = package.read("bundle.json")
        provenance = json.loads(package.read("provenance.json"))
    source = api(f"contents/{path}?ref={commit}")
    if source.get("type") != "file" or not re.fullmatch(r"[a-f0-9]{40}", source.get("sha", "")):
        raise ValueError("exact tracked source is unavailable")
    blob = api(f"git/blobs/{source['sha']}")
    if blob.get("encoding") != "base64" or not 0 < blob.get("size", 0) <= MAXIMUM:
        raise ValueError("exact source blob encoding/size is invalid")
    source_bytes = base64.b64decode(blob["content"].replace("\n", ""), validate=True)
    tree = api(f"git/commits/{commit}")["tree"]["sha"]
    expected = {"repository": REPOSITORY, "commit": commit, "tree": tree, "path": path,
                "bundle_digest": expected_digest, "workflow_run_id": run, "run_attempt": execution["run_attempt"]}
    if not 0 < len(wire) <= MAXIMUM or wire != source_bytes or hashlib.sha256(wire).hexdigest() != expected_digest or provenance != expected:
        raise ValueError("Bundle bytes/provenance differ from exact tracked CI source")
    carrier = {"repository": REPOSITORY, "commit": commit, "tree": tree, "path": path,
               "workflow_run_id": run, "artifact_id": artifact}
    return wire, carrier, archive_digest


def remote(args, command, payload=None):
    return execute(["ssh", "-o", "BatchMode=yes", args.target, shlex.join(command)], payload=payload, timeout=120)


def check_target(args):
    actual_host = remote(args, ["hostname"]).decode().strip()
    actual_image = remote(args, ["docker", "inspect", "--format", "{{.Image}}", args.owner_container]).decode().strip()
    if actual_host != args.expected_hostname or actual_image != args.expected_owner_image:
        raise ValueError("Target hostname or executing Artifact changed")


def owner(args, action, envelope):
    check_target(args)
    wire = remote(args, ["docker", "exec", "-i", args.owner_container, "./uniroute", "capability-contract-publication", action],
                  json.dumps(envelope, separators=(",", ":")).encode())
    return json.loads(wire)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run", required=True, type=int)
    parser.add_argument("--artifact", required=True, type=int)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--path", required=True)
    parser.add_argument("--digest", required=True)
    parser.add_argument("--publication", required=True)
    parser.add_argument("--target", required=True)
    parser.add_argument("--expected-hostname", required=True)
    parser.add_argument("--expected-system-identifier", required=True)
    parser.add_argument("--expected-environment", required=True, choices=("development", "test", "integration"))
    parser.add_argument("--owner-container", required=True, choices=("uniroute-api", "uniroute-api-canary"))
    parser.add_argument("--expected-owner-image", required=True)
    parser.add_argument("--plan", required=True, type=Path)
    parser.add_argument("--apply", action="store_true")
    parser.add_argument("--compensates", default="")
    args = parser.parse_args()
    if not re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9._-]*", args.target) or not re.fullmatch(r"sha256:[a-f0-9]{64}", args.expected_owner_image):
        raise ValueError("exact configured SSH target and image digest are required")
    wire, carrier, archive_digest = verify_source(args.run, args.artifact, args.commit, args.path, args.digest)
    envelope = {"publication_id": args.publication, "kind": "compensation" if args.compensates else "delta",
                "compensates_publication_id": args.compensates, "bundle_bytes": base64.b64encode(wire).decode(),
                "approved_bundle_digest": args.digest, "carrier": carrier, "plan_bytes": None, "plan_digest": ""}
    if args.apply:
        saved = json.loads(args.plan.read_bytes())
        if saved["source"] != envelope or saved["archive_digest"] != archive_digest:
            raise ValueError("saved Plan belongs to different verified CI bytes")
        plan_result = saved["plan"]
    else:
        if args.plan.exists():
            raise ValueError("Plan output already exists; never overwrite an immutable candidate")
        plan_result = owner(args, "plan-v2", envelope)
    plan_bytes = base64.b64decode(plan_result["plan_bytes"], validate=True)
    if hashlib.sha256(plan_bytes).hexdigest() != plan_result["plan_digest"]:
        raise ValueError("Target Plan bytes do not match their digest")
    target = json.loads(plan_bytes)["target_identity"]
    if target["system_identifier"] != args.expected_system_identifier or target["environment"] != args.expected_environment:
        raise ValueError("Plan database/environment differs from authorized Target")
    if not args.apply:
        with args.plan.open("xb") as output:
            import os
            os.fchmod(output.fileno(), 0o600)
            output.write(json.dumps({"source": envelope, "archive_digest": archive_digest, "plan": plan_result}, separators=(",", ":")).encode())
        print(json.dumps({"status": "verified_ci_plan", "plan_digest": plan_result["plan_digest"], "bundle_digest": args.digest}))
        return
    envelope.update(plan_result)
    print(json.dumps(owner(args, "apply-v2", envelope), sort_keys=True))


if __name__ == "__main__":
    main()
