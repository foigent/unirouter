"""Synthetic stage fixture inputs. Never an actual dispatch/approval record."""
import ast
import copy
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]

def producer(version="1.15.0", commit="a" * 40, *, schema="000128_upstream_release_observation"):
    for node in ast.parse((ROOT / ".github/tests/community-image-candidate.py").read_text()).body:
        if isinstance(node, ast.Assign) and any(isinstance(target, ast.Name) and target.id == "FIXED" for target in node.targets):
            legacy = json.loads(ast.literal_eval(node.value.args[0])); break
    subject = {"repository": "foigent/unirouter", "commit": commit, "tree": "b" * 40, "version": version, "schema": schema}
    files = copy.deepcopy(legacy["source_files"])
    files["migrations/000128_upstream_release_observation.up.sql"] = {"bytes": 100, "sha256": "c" * 64}
    if schema == "000129_upstream_observation_restore_namespace":
        files["migrations/000129_upstream_observation_restore_namespace.up.sql"] = {"bytes": 100, "sha256": "e" * 64}
    return {"schema_version": "unirouter.community-stage.v1", "stage": "producer", "product": subject, "predecessor": None,
            "inputs": {"source_inventory": {"tracked_files": 3000, "tracked_regular_bytes": 50000000, "files": files},
                       "images": legacy["images"], "notices": {"bytes": 335927, "sha256": "d" * 64,
                            "source": {"commit": legacy["source"]["commit"], "tree": legacy["source"]["tree"]}}}}


def publisher(subject=None, images=None):
    subject = copy.deepcopy(subject or producer()["product"])
    if images is None:
        images = {role: {"role": role, "package_name": "unirouter-" + role, "repository": "ghcr.io/foigent/unirouter-" + role,
                         "asset_id": index, "archive_name": role + "-image.tar.gz", "archive_bytes": 32,
                         "archive_sha256": str(index) * 64, "config_id": "sha256:" + str(index + 2) * 64,
                         "diff_ids": ["sha256:" + str(index + 4) * 64]} for index, role in enumerate(("backend", "frontend"), 1)}
    parent = {"stage": "producer", "record_sha256": "e" * 64, "result_sha256": "f" * 64, "run_id": 12345, "program_commit": "c" * 40}
    return {"schema_version": "unirouter.community-stage.v1", "stage": "publisher", "product": subject, "predecessor": parent,
            "inputs": {"draft_id": 456, "tag": "source-" + subject["commit"] + "-run-12345-linux-amd64", "images": images,
                       "evidence": {"name": "candidate-evidence.tar.gz", "semantic_sha256": parent["result_sha256"]}}}
