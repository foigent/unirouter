#!/usr/bin/env python3
"""Strict finite stage records. Data identity is never dispatch approval.

Trusted exact workflow checkout precedes this offline validator. Product
checkout and stage credentials/download/build/write follow validation. Inputs
travel through environment/files; they are never interpolated into shell code.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import sys
import io
import tarfile

SCHEMA = "unirouter.community-stage.v1"
RESULT_SCHEMA = "unirouter.community-stage-result.v1"
REPOSITORY = "foigent/unirouter"
REPOSITORY_ID = 1281228065
ACTOR_ID = 252312344
MAX_RAW = 32 * 1024
MAX_ENCODED = ((MAX_RAW + 2) // 3) * 4
MAX_DISPATCH = 65535
MAX_INPUTS = 25
WORKFLOWS = {"producer": "community-byte-candidate.yml", "publisher": "publish-accepted-community-images.yml", "consumer": "community-release-acceptance.yml"}
HEADS = {"000127_xinsight_communication_policy", "000128_upstream_release_observation"}
RING = {"key_id": "ur-community-upgrade-20261011-a", "bytes": 83,
        "sha256": "96c91635460ddfb2a14aa1ea97d8445951730e9eb04a33cbdf4320141e61d63d",
        "decoded_key_sha256": "765f38f8a4a4f38bf7e94aa3236c28f74dccac56431ab3a1d035a880f6086116"}
BASE_REPOS = {"backend-builder": "docker.io/library/golang", "backend-runtime": "docker.io/library/alpine",
              "frontend-builder": "docker.io/library/node", "frontend-runtime": "docker.io/library/nginx",
              "postgres": "docker.io/library/postgres", "redis": "docker.io/library/redis",
              "prometheus": "docker.io/prom/prometheus", "grafana": "docker.io/grafana/grafana"}
SOURCE_FILES = {"Dockerfile", "frontend/Dockerfile", "go.mod", "go.sum", "frontend/package.json", "frontend/package-lock.json",
                "frontend/scripts/check-toolchain.mjs", ".dockerignore", "frontend/.dockerignore",
                "migrations/000126_commercial_license_retirement.up.sql", "migrations/000127_xinsight_communication_policy.up.sql"}


class Stop(Exception):
    pass


def need(ok, label):
    if not ok:
        raise Stop(label)


def exact(value, keys):
    need(type(value) is dict and set(value) == set(keys), "stage_fields")


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def text(value, pattern, maximum):
    need(type(value) is str and len(value) <= maximum and re.fullmatch(pattern, value) is not None, "stage_text")


def digest(value):
    text(value, r"[a-f0-9]{64}", 64)


def image_digest(value):
    text(value, r"sha256:[a-f0-9]{64}", 71)


def integer(value, low=1, high=2**63-1):
    need(type(value) is int and low <= value <= high, "stage_integer")


def strict_json(raw, maximum=MAX_RAW):
    need(type(raw) is bytes and 0 < len(raw) <= maximum, "stage_raw_bound")
    def pairs(items):
        result = {}
        for key, value in items:
            need(key not in result, "stage_duplicate_field")
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=pairs, parse_constant=lambda _: need(False, "stage_nonfinite"))


def product(value):
    exact(value, ("repository", "commit", "tree", "version", "schema"))
    need(value["repository"] == REPOSITORY and value["schema"] in HEADS, "stage_product_scope")
    text(value["commit"], r"[a-f0-9]{40}", 40); text(value["tree"], r"[a-f0-9]{40}", 40)
    text(value["version"], r"[0-9]+\.[0-9]+\.[0-9]+", 32)


def file_fact(value, maximum=512*1024*1024):
    exact(value, ("bytes", "sha256")); integer(value["bytes"], 0, maximum); digest(value["sha256"])


def predecessor(value, stage):
    if stage == "producer":
        need(value is None, "stage_producer_predecessor")
        return
    exact(value, ("stage", "record_sha256", "result_sha256", "run_id", "program_commit"))
    need(value["stage"] == ("producer" if stage == "publisher" else "publisher"), "stage_predecessor_order")
    digest(value["record_sha256"]); digest(value["result_sha256"]); integer(value["run_id"])
    text(value["program_commit"], r"[a-f0-9]{40}", 40)


def base_image(value, role):
    exact(value, ("input_reference", "index_digest", "pull_reference", "config_id", "platform", "rootfs_diff_ids", "source_annotation", "version_annotation", "version_clues"))
    prefix = BASE_REPOS[role]
    text(value["input_reference"], re.escape(prefix) + r":[A-Za-z0-9._@:+-]+", 256)
    text(value["pull_reference"], re.escape(prefix) + r"@sha256:[a-f0-9]{64}", 256)
    image_digest(value["index_digest"]); image_digest(value["config_id"])
    need(value["platform"] == {"os": "linux", "architecture": "amd64", "variant": None}, "stage_base_platform")
    need(type(value["rootfs_diff_ids"]) is list and 0 < len(value["rootfs_diff_ids"]) <= 32, "stage_base_layers")
    for layer in value["rootfs_diff_ids"]: image_digest(layer)
    if value["source_annotation"] is not None:
        text(value["source_annotation"], r"https://github\.com/[A-Za-z0-9._/#:+-]+", 1024)
    if value["version_annotation"] is not None:
        text(value["version_annotation"], r"[A-Za-z0-9._+-]+", 128)
    allowed = {"backend-builder": {"GOLANG_VERSION"}, "frontend-builder": {"NODE_VERSION"},
               "frontend-runtime": {"NGINX_VERSION"}, "postgres": {"PG_MAJOR", "PG_VERSION"}, "redis": {"REDIS_VERSION"}}.get(role, set())
    need(type(value["version_clues"]) is dict and set(value["version_clues"]) == allowed, "stage_base_clues")
    for clue in value["version_clues"].values(): text(clue, r"[A-Za-z0-9._+-]+", 128)


def producer_inputs(value, subject):
    exact(value, ("source_inventory", "images", "notices"))
    inventory = value["source_inventory"]
    exact(inventory, ("tracked_files", "tracked_regular_bytes", "files"))
    integer(inventory["tracked_files"], 1, 100000); integer(inventory["tracked_regular_bytes"], 1, 512*1024*1024)
    expected = set(SOURCE_FILES)
    if subject["schema"] == "000128_upstream_release_observation":
        expected.add("migrations/000128_upstream_release_observation.up.sql")
    exact(inventory["files"], expected)
    for row in inventory["files"].values(): file_fact(row, 4*1024*1024)
    exact(value["images"], BASE_REPOS)
    for role, row in value["images"].items(): base_image(row, role)
    exact(value["notices"], ("bytes", "sha256", "source"))
    file_fact({key: value["notices"][key] for key in ("bytes", "sha256")}, 1024*1024)
    exact(value["notices"]["source"], ("commit", "tree"))
    for item in value["notices"]["source"].values(): text(item, r"[a-f0-9]{40}", 40)


def publication_image(value, role):
    exact(value, ("role", "package_name", "repository", "asset_id", "archive_name", "archive_bytes", "archive_sha256", "config_id", "diff_ids"))
    need(value["role"] == role and value["package_name"] == "unirouter-" + role
         and value["repository"] == "ghcr.io/foigent/unirouter-" + role
         and value["archive_name"] == role + "-image.tar.gz", "stage_image_scope")
    integer(value["asset_id"]); integer(value["archive_bytes"], 1, 512*1024*1024); digest(value["archive_sha256"]); image_digest(value["config_id"])
    need(type(value["diff_ids"]) is list and 0 < len(value["diff_ids"]) <= 32, "stage_image_layers")
    for layer in value["diff_ids"]: image_digest(layer)


def publisher_inputs(value, subject, parent):
    exact(value, ("draft_id", "tag", "images", "evidence"))
    integer(value["draft_id"])
    need(value["tag"] == "source-" + subject["commit"] + "-run-" + str(parent["run_id"]) + "-linux-amd64", "stage_tag")
    exact(value["images"], ("backend", "frontend"))
    for role, row in value["images"].items(): publication_image(row, role)
    need(value["images"]["backend"]["asset_id"] != value["images"]["frontend"]["asset_id"], "stage_image_asset_collision")
    exact(value["evidence"], ("name", "semantic_sha256"))
    need(value["evidence"] == {"name": "candidate-evidence.tar.gz", "semantic_sha256": parent["result_sha256"]}, "stage_evidence_binding")


def consumer_inputs(value, subject, parent):
    # Deep full-package/bootstrap validation remains the consumer owner's job,
    # performed before its first network/Docker effect. This common parser binds
    # its exact closed config envelope to product and adjacent-stage identity.
    exact(value, ("config",))
    config = value["config"]
    exact(config, ("schema_version", "state", "immutable", "release"))
    need(config["schema_version"] == "unirouter.formal-consumer-input.v1" and config["state"] == "ready", "stage_consumer_config")
    fixed = config["immutable"]
    exact(fixed, ("product", "key_id", "public_ring", "raw_public_fingerprint", "bootstrap_script", "verifier", "native_acceptance_helper_sha256", "images", "app_image_identities", "artifacts", "provenance_sha256", "accepted_assembly_run", "accepted_draft_id", "actual_private_publisher_run", "min_current_sequence", "max_current_sequence", "min_current_schema", "historical_sequence_reserved_through", "manifest_schema_version", "expires_at", "stage_chain"))
    exact(config["release"], ("release_sequence", "created_at", "expires_at", "tag", "bootstrap_url", "bootstrap_size", "bootstrap_sha256", "package_size", "package_sha256"))
    need(fixed["key_id"] == RING["key_id"] and fixed["public_ring"] == {"size": RING["bytes"], "sha256": RING["sha256"]}
         and fixed["raw_public_fingerprint"] == RING["decoded_key_sha256"]
         and fixed["manifest_schema_version"] == "unirouter.upgrade-manifest.v2"
         and fixed["expires_at"] == config["release"]["expires_at"] == "never", "stage_consumer_trust")
    need(type(fixed) is dict and fixed.get("product") == {key: subject[key] for key in ("commit", "tree", "version", "schema")}, "stage_consumer_product")
    chain = fixed.get("stage_chain")
    exact(chain, ("producer_record_sha256", "producer_result_sha256", "publisher_record_sha256", "publisher_result_sha256"))
    for item in chain.values(): digest(item)
    need(chain["publisher_record_sha256"] == parent["record_sha256"] and chain["publisher_result_sha256"] == parent["result_sha256"], "stage_consumer_predecessor")


def validate_record(value, stage=None):
    exact(value, ("schema_version", "stage", "product", "predecessor", "inputs"))
    need(value["schema_version"] == SCHEMA and value["stage"] in WORKFLOWS
         and (stage is None or value["stage"] == stage), "stage_scope")
    product(value["product"]); predecessor(value["predecessor"], value["stage"])
    if value["stage"] == "producer": producer_inputs(value["inputs"], value["product"])
    elif value["stage"] == "publisher": publisher_inputs(value["inputs"], value["product"], value["predecessor"])
    else: consumer_inputs(value["inputs"], value["product"], value["predecessor"])
    return value


def decode_record(encoded, expected_sha256, stage=None):
    text(encoded, r"[A-Za-z0-9_-]+", MAX_ENCODED); digest(expected_sha256)
    raw = base64.b64decode(encoded + "=" * (-len(encoded) % 4), altchars=b"-_", validate=True)
    need(0 < len(raw) <= MAX_RAW and base64.urlsafe_b64encode(raw).decode().rstrip("=") == encoded, "stage_encoded_bound")
    need(sha(raw) == expected_sha256, "stage_record_digest")
    return raw, validate_record(strict_json(raw), stage)


def dispatch_bound(inputs):
    need(type(inputs) is dict and 0 < len(inputs) <= MAX_INPUTS and all(type(key) is str and type(value) is str for key, value in inputs.items()), "stage_dispatch_properties")
    need(len(json.dumps(inputs, separators=(",", ":"), ensure_ascii=False)) <= MAX_DISPATCH, "stage_dispatch_total_bound")


def from_dispatch(inputs, stage):
    dispatch_bound(inputs)
    exact(inputs, ("stage_record_b64", "stage_record_sha256", "expected_workflow_commit", "admission_sha256"))
    text(inputs["expected_workflow_commit"], r"[a-f0-9]{40}", 40); digest(inputs["admission_sha256"])
    return decode_record(inputs["stage_record_b64"], inputs["stage_record_sha256"], stage)


def check_context(env, event, stage, inputs):
    from_dispatch(inputs, stage)
    expected = inputs["expected_workflow_commit"]
    need(env.get("GITHUB_REPOSITORY") == REPOSITORY and env.get("GITHUB_REPOSITORY_ID") == str(REPOSITORY_ID)
         and env.get("GITHUB_REF") == "refs/heads/main" and env.get("GITHUB_EVENT_NAME") == "workflow_dispatch"
         and env.get("GITHUB_SHA") == env.get("GITHUB_WORKFLOW_SHA") == expected
         and env.get("GITHUB_RUN_ATTEMPT") == "1" and env.get("GITHUB_ACTOR_ID") == str(ACTOR_ID)
         and env.get("GITHUB_WORKFLOW_REF") == REPOSITORY + "/.github/workflows/" + WORKFLOWS[stage] + "@refs/heads/main"
         and env.get("RUNNER_ENVIRONMENT") == "github-hosted" and env.get("RUNNER_OS") == "Linux" and env.get("RUNNER_ARCH") == "X64", "stage_runtime_context")
    need(event.get("repository", {}).get("id") == REPOSITORY_ID and event.get("repository", {}).get("full_name") == REPOSITORY
         and event.get("sender", {}).get("id") == ACTOR_ID and event.get("inputs") == inputs, "stage_event_binding")


def encode(value):
    raw = canonical(validate_record(value))
    need(len(raw) <= MAX_RAW, "stage_raw_bound")
    return {"stage_record_b64": base64.urlsafe_b64encode(raw).decode().rstrip("="), "stage_record_sha256": sha(raw)}


PUBLIC_CONTEXT = ("GITHUB_REPOSITORY", "GITHUB_REPOSITORY_ID", "GITHUB_REF", "GITHUB_EVENT_NAME", "GITHUB_SHA", "GITHUB_WORKFLOW_SHA", "GITHUB_RUN_ATTEMPT", "GITHUB_ACTOR_ID", "GITHUB_WORKFLOW_REF", "RUNNER_ENVIRONMENT", "RUNNER_OS", "RUNNER_ARCH")
INPUT_ENV = {"stage_record_b64": "STAGE_RECORD_B64", "stage_record_sha256": "STAGE_RECORD_SHA256", "expected_workflow_commit": "EXPECTED_WORKFLOW_COMMIT", "admission_sha256": "ADMISSION_SHA256"}


def read_dispatch(stage, environment=None):
    env = os.environ if environment is None else environment
    inputs = {key: env.get(name, "") for key, name in INPUT_ENV.items()}
    raw, value = from_dispatch(inputs, stage)
    with Path(env["GITHUB_EVENT_PATH"]).open("rb") as stream:
        event = strict_json(stream.read(2*1024*1024+1), 2*1024*1024)
    check_context({name: env.get(name, "") for name in PUBLIC_CONTEXT}, event, stage, inputs)
    return raw, value, inputs


CHAIN_PATHS = ("stage/producer-record.json", "stage/producer-result.json", "stage/publisher-candidate.json")
CLOSED_MANIFEST = "closed-file-manifest.json"
SEMANTIC_EXCLUSIONS = frozenset((*CHAIN_PATHS, CLOSED_MANIFEST))
MAX_EVIDENCE = 32 * 1024 * 1024


def member_name(name):
    from pathlib import PurePosixPath
    text(name, r"[A-Za-z0-9._/+-]+", 256)
    pure = PurePosixPath(name)
    need(not pure.is_absolute() and name == pure.as_posix() and all(part not in (".", "..") for part in pure.parts), "stage_member_name")
    need(not name.startswith("stage/") or name in CHAIN_PATHS, "stage_unknown_chain_member")


def byte_fact(raw):
    return {"bytes": len(raw), "sha256": sha(raw)}


def producer_result(value):
    exact(value, ("schema_version", "stage", "status", "product", "record_sha256", "program_commit", "run_id", "draft_id", "images", "verifier", "members"))
    need(value["schema_version"] == RESULT_SCHEMA and value["stage"] == "producer"
         and value["status"] == "unsigned_candidate_pending_clearance", "stage_producer_result")
    product(value["product"]); digest(value["record_sha256"]); text(value["program_commit"], r"[a-f0-9]{40}", 40)
    integer(value["run_id"]); integer(value["draft_id"])
    exact(value["images"], ("backend", "frontend"))
    for role, image in value["images"].items(): publication_image(image, role)
    exact(value["verifier"], ("asset_id", "bytes", "sha256")); integer(value["verifier"]["asset_id"])
    file_fact({key: value["verifier"][key] for key in ("bytes", "sha256")}, 32*1024*1024)
    members = value["members"]
    need(type(members) is dict and 0 < len(members) <= 8192, "stage_source_members")
    for name, fact in members.items():
        member_name(name); need(name not in SEMANTIC_EXCLUSIONS, "stage_semantic_self_reference"); file_fact(fact, 28*1024*1024)
    need(sum(fact["bytes"] for fact in members.values()) <= 28*1024*1024, "stage_source_bytes")
    return value


def next_publisher(raw_record, result):
    original = validate_record(strict_json(raw_record), "producer")
    producer_result(result)
    need(result["record_sha256"] == sha(raw_record) and result["product"] == original["product"], "stage_result_input_binding")
    parent = {"stage": "producer", "record_sha256": sha(raw_record), "result_sha256": sha(canonical(result)),
              "run_id": result["run_id"], "program_commit": result["program_commit"]}
    value = {"schema_version": SCHEMA, "stage": "publisher", "product": original["product"], "predecessor": parent,
             "inputs": {"draft_id": result["draft_id"], "tag": "source-" + original["product"]["commit"] + "-run-" + str(result["run_id"]) + "-linux-amd64",
                        "images": result["images"], "evidence": {"name": "candidate-evidence.tar.gz", "semantic_sha256": parent["result_sha256"]}}}
    validate_record(value, "publisher"); need(len(canonical(value)) <= MAX_RAW, "stage_next_record_bound")
    return value


def producer_chain(raw_record, program_commit, run_id, draft_id, images, verifier, source_members):
    original = validate_record(strict_json(raw_record), "producer")
    need(type(source_members) is dict, "stage_source_members")
    for name in source_members:
        member_name(name)
        need(name not in SEMANTIC_EXCLUSIONS, "stage_semantic_self_reference")
    result = {"schema_version": RESULT_SCHEMA, "stage": "producer", "status": "unsigned_candidate_pending_clearance",
              "product": original["product"], "record_sha256": sha(raw_record), "program_commit": program_commit,
              "run_id": run_id, "draft_id": draft_id, "images": images, "verifier": verifier,
              "members": {name: byte_fact(body) for name, body in source_members.items()}}
    producer_result(result)
    following = next_publisher(raw_record, result)
    return dict(zip(CHAIN_PATHS, (raw_record, canonical(result), canonical(following))))


def verify_producer_chain(files, publisher_record):
    validate_record(publisher_record, "publisher")
    need(type(files) is dict and set(CHAIN_PATHS) | {CLOSED_MANIFEST} <= set(files), "stage_chain_missing")
    for name, raw in files.items():
        member_name(name); need(type(raw) is bytes, "stage_member_bytes")
    closed = strict_json(files[CLOSED_MANIFEST], MAX_EVIDENCE)
    exact(closed, ("files", "bytes", "scope"))
    need(type(closed["files"]) is dict and type(closed["scope"]) is str, "stage_closed_manifest")
    ordinary = {name: byte_fact(raw) for name, raw in files.items() if name != CLOSED_MANIFEST}
    need(closed["files"] == ordinary and closed["bytes"] == sum(row["bytes"] for row in ordinary.values()), "stage_closed_membership")
    raw_record, raw_result, raw_next = (files[name] for name in CHAIN_PATHS)
    original = validate_record(strict_json(raw_record), "producer")
    result = producer_result(strict_json(raw_result, MAX_EVIDENCE))
    actual = validate_record(strict_json(raw_next), "publisher")
    need(raw_result == canonical(result) and actual == publisher_record and raw_next == canonical(actual), "stage_next_record_binding")
    parent = actual["predecessor"]
    need(parent["record_sha256"] == sha(raw_record) == result["record_sha256"]
         and parent["result_sha256"] == sha(raw_result) and parent["run_id"] == result["run_id"]
         and parent["program_commit"] == result["program_commit"] and actual["product"] == original["product"] == result["product"], "stage_predecessor_binding")
    members = {name: byte_fact(raw) for name, raw in files.items() if name not in SEMANTIC_EXCLUSIONS}
    need(result["members"] == members and next_publisher(raw_record, result) == actual, "stage_semantic_membership")
    return result


def evidence_files(path):
    need(path.is_file() and not path.is_symlink() and path.stat().st_size <= MAX_EVIDENCE, "stage_evidence_file")
    files = {}; total = 0
    with tarfile.open(path, "r:gz") as archive:
        for member in archive:
            member_name(member.name)
            need(member.isfile() and member.name not in files and len(files) < 8192
                 and 0 <= member.size <= MAX_EVIDENCE, "stage_evidence_member")
            total += member.size; need(total <= MAX_EVIDENCE, "stage_evidence_total")
            raw = archive.extractfile(member).read(member.size+1)
            need(len(raw) == member.size, "stage_evidence_truncated")
            files[member.name] = raw
    return files


def publisher_result(raw_record, program_commit, run_id, images):
    record = validate_record(strict_json(raw_record), 'publisher')
    value = {'schema_version': RESULT_SCHEMA, 'stage': 'publisher',
             'status': 'authenticated_image_identity_verified_only', 'product': record['product'],
             'record_sha256': sha(raw_record), 'program_commit': program_commit, 'run_id': run_id,
             'predecessor': record['predecessor'], 'draft_id': record['inputs']['draft_id'], 'images': images}
    return verify_publisher_result(raw_record, value)


def verify_publisher_result(raw_record, value):
    record = validate_record(strict_json(raw_record), 'publisher')
    exact(value, ('schema_version', 'stage', 'status', 'product', 'record_sha256', 'program_commit',
                  'run_id', 'predecessor', 'draft_id', 'images'))
    need(value['schema_version'] == RESULT_SCHEMA and value['stage'] == 'publisher'
         and value['status'] == 'authenticated_image_identity_verified_only', 'stage_publisher_result')
    need(value['record_sha256'] == sha(raw_record) and value['product'] == record['product']
         and value['predecessor'] == record['predecessor'] and value['draft_id'] == record['inputs']['draft_id'],
         'stage_publisher_result_binding')
    text(value['program_commit'], r'[a-f0-9]{40}', 40); integer(value['run_id'])
    need(type(value['images']) is list and len(value['images']) == 2, 'stage_published_images')
    seen = set()
    for row in value['images']:
        exact(row, ('role', 'repository', 'tag', 'registry_digest_reference', 'config_id', 'diff_ids',
                    'native_archive_sha256', 'asset_id', 'operation'))
        role = row['role']; need(role in ('backend', 'frontend') and role not in seen, 'stage_published_role'); seen.add(role)
        original = record['inputs']['images'][role]
        need(all(row[key] == original[key] for key in ('repository', 'config_id', 'diff_ids', 'asset_id'))
             and row['native_archive_sha256'] == original['archive_sha256']
             and row['tag'] == record['inputs']['tag'], 'stage_published_content')
        text(row['registry_digest_reference'], re.escape(original['repository']) + r'@sha256:[a-f0-9]{64}', 160)
        need(row['operation'] in ('new_public_push_of_exact_accepted_image',
                                 'reused_existing_exact_public_image_no_push'), 'stage_published_operation')
    return value


def publication_bindings(raw_record, raw_result):
    """Ordinary existing signed provenance data; never a dispatch or clearance."""
    record = validate_record(strict_json(raw_record), 'publisher')
    result = verify_publisher_result(raw_record, strict_json(raw_result))
    return {'producer_record_sha256': record['predecessor']['record_sha256'],
            'producer_result_sha256': record['predecessor']['result_sha256'],
            'publisher_record_sha256': sha(raw_record), 'publisher_result_sha256': sha(raw_result),
            'publisher_record_b64': base64.urlsafe_b64encode(raw_record).decode().rstrip('='),
            'publisher_result_b64': base64.urlsafe_b64encode(raw_result).decode().rstrip('=')}


def verify_publication_bindings(value, config, consumer_record):
    exact(value, ('producer_record_sha256', 'producer_result_sha256', 'publisher_record_sha256',
                  'publisher_result_sha256', 'publisher_record_b64', 'publisher_result_b64'))
    raw_record, record = decode_record(value['publisher_record_b64'], value['publisher_record_sha256'], 'publisher')
    encoded = value['publisher_result_b64']; text(encoded, r'[A-Za-z0-9_-]+', MAX_ENCODED)
    raw_result = base64.b64decode(encoded+'='*(-len(encoded)%4), altchars=b'-_', validate=True)
    need(base64.urlsafe_b64encode(raw_result).decode().rstrip('=') == encoded
         and sha(raw_result) == value['publisher_result_sha256'], 'stage_publisher_result_digest')
    result = verify_publisher_result(raw_record, strict_json(raw_result))
    validate_record(consumer_record, 'consumer')
    need(consumer_record['inputs']['config'] == config
         and consumer_record['product'] == record['product']
         and consumer_record['predecessor'] == {
             'stage': 'publisher', 'record_sha256': sha(raw_record), 'result_sha256': sha(raw_result),
             'run_id': result['run_id'], 'program_commit': result['program_commit']},
         'stage_consumer_full_predecessor')
    fixed = config['immutable']; chain = fixed['stage_chain']
    need(all(value[key] == chain[key] for key in chain)
         and value['producer_record_sha256'] == record['predecessor']['record_sha256']
         and value['producer_result_sha256'] == record['predecessor']['result_sha256']
         and fixed['product'] == {key: record['product'][key] for key in ('commit','tree','version','schema')}
         and fixed['accepted_assembly_run'] == record['predecessor']['run_id']
         and fixed['accepted_draft_id'] == record['inputs']['draft_id']
         and fixed['actual_private_publisher_run'] == result['run_id'], 'stage_signed_provenance_binding')
    expected = {row['role']: row for row in result['images']}
    need(type(fixed['app_image_identities']) is list and len(fixed['app_image_identities']) == 2, 'stage_consumer_app_images')
    need({row['role']: row for row in fixed['app_image_identities']} == expected
         and all(fixed['images'][role] == expected[role]['registry_digest_reference'] for role in expected),
         'stage_consumer_published_images')
    return record, result


def next_consumer(raw_record, raw_result, config):
    """Fill only actual predecessor-owned fields into an existing full consumer config."""
    import copy
    record = validate_record(strict_json(raw_record), 'publisher')
    result = verify_publisher_result(raw_record, strict_json(raw_result))
    bindings = publication_bindings(raw_record, raw_result)
    config = copy.deepcopy(config); fixed = config['immutable']
    fixed['stage_chain'] = {key: bindings[key] for key in bindings if key.endswith('_sha256')}
    fixed.update(accepted_assembly_run=record['predecessor']['run_id'], accepted_draft_id=record['inputs']['draft_id'],
                 actual_private_publisher_run=result['run_id'], app_image_identities=result['images'])
    fixed['images'].update({row['role']: row['registry_digest_reference'] for row in result['images']})
    value = {'schema_version': SCHEMA, 'stage': 'consumer', 'product': record['product'],
             'predecessor': {'stage': 'publisher', 'record_sha256': sha(raw_record), 'result_sha256': sha(raw_result),
                             'run_id': result['run_id'], 'program_commit': result['program_commit']}, 'inputs': {'config': config}}
    validate_record(value, 'consumer'); verify_publication_bindings(bindings, config, value)
    need(len(canonical(value)) <= MAX_RAW, 'stage_next_record_bound')
    return value


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("validate", "describe", "publisher-candidate", "publication-bindings", "consumer-candidate"))
    parser.add_argument("--stage", choices=tuple(WORKFLOWS))
    parser.add_argument("--record", type=Path)
    parser.add_argument("--sha256")
    parser.add_argument("--dispatch", action="store_true")
    parser.add_argument("--github-output", action="store_true", help="write only validated standard step output fields")
    parser.add_argument("--evidence", type=Path)
    parser.add_argument("--publisher-record", type=Path)
    parser.add_argument("--publisher-result", type=Path)
    parser.add_argument("--config", type=Path)
    parser.add_argument("--out", type=Path, help="new private file; existing outputs are never replaced")
    args = parser.parse_args(argv)
    try:
        if args.action in ("publisher-candidate", "publication-bindings", "consumer-candidate"):
            need(not args.dispatch and not args.github_output and args.out is not None, "stage_offline_handoff_arguments")
            if args.action == "publisher-candidate":
                need(args.evidence is not None, "stage_parent_evidence_required")
                files=evidence_files(args.evidence)
                following=validate_record(strict_json(files[CHAIN_PATHS[2]]), "publisher")
                verify_producer_chain(files, following); output=files[CHAIN_PATHS[2]]
            else:
                need(args.publisher_record is not None and args.publisher_result is not None, "stage_publisher_bytes_required")
                with args.publisher_record.open("rb") as stream: raw_record=stream.read(MAX_RAW+1)
                with args.publisher_result.open("rb") as stream: raw_result=stream.read(MAX_RAW+1)
                # A native publisher receipt may be passed whole; actual stage facts
                # are selected and canonicalized without copying individual hashes.
                receipt=strict_json(raw_result)
                if type(receipt) is dict and "stage_result" in receipt: raw_result=canonical(receipt["stage_result"])
                if args.action == "publication-bindings":
                    output=canonical(publication_bindings(raw_record,raw_result))
                else:
                    need(args.config is not None, "stage_consumer_config_required")
                    with args.config.open("rb") as stream: config=strict_json(stream.read(MAX_RAW+1))
                    output=canonical(next_consumer(raw_record,raw_result,config))
            fd=os.open(args.out,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
            with os.fdopen(fd,"wb") as stream: stream.write(output);stream.flush();os.fsync(stream.fileno())
            print(json.dumps({"output_bytes":len(output),"sha256":sha(output),"approval":"not_asserted"},sort_keys=True))
            return 0
        need(args.stage in WORKFLOWS, "stage_required")
        if args.dispatch:
            raw, value, inputs = read_dispatch(args.stage)
        else:
            need(args.record is not None and args.sha256 is not None, "stage_record_required")
            with args.record.open("rb") as stream: raw = stream.read(MAX_RAW+1)
            digest(args.sha256); need(sha(raw) == args.sha256, "stage_record_digest")
            value = validate_record(strict_json(raw), args.stage)
        if args.github_output:
            need(args.dispatch, "stage_output_context")
            fd = os.open(os.environ["GITHUB_OUTPUT"], os.O_WRONLY | os.O_APPEND | os.O_NOFOLLOW)
            with os.fdopen(fd, "w") as stream:
                stream.write("product_commit=" + value["product"]["commit"] + "\nrecord_sha256=" + sha(raw) + "\n")
        print(json.dumps({"stage": args.stage, "record_sha256": sha(raw), "product_commit": value["product"]["commit"], "product_version": value["product"]["version"], "approval": "not_asserted"}, sort_keys=True))
        return 0
    except (Stop, OSError, ValueError, TypeError, KeyError, RecursionError):
        print("Community stage record refused before stage effects.", file=sys.stderr)
        return 1


if __name__ == "__main__": sys.exit(main())
