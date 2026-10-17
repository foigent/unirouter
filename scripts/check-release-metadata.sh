#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || {
  echo "release metadata: run from a Git checkout" >&2
  exit 2
}
cd "${repo_root}"

current_version="$(tr -d '[:space:]' < VERSION)"
if [[ ! "${current_version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "release metadata: VERSION must contain exactly MAJOR.MINOR.PATCH" >&2
  exit 1
fi

frontend_version="$(sed -n 's/^[[:space:]]*"version":[[:space:]]*"\([^"]*\)",[[:space:]]*$/\1/p' frontend/package.json | head -n 1)"
if [[ "${frontend_version}" != "${current_version}" ]]; then
  echo "release metadata: frontend/package.json version ${frontend_version:-<missing>} does not match VERSION ${current_version}" >&2
  exit 1
fi

frontend_lock_version="$(sed -n 's/^[[:space:]]*"version":[[:space:]]*"\([^"]*\)",[[:space:]]*$/\1/p' frontend/package-lock.json | head -n 1)"
if [[ "${frontend_lock_version}" != "${current_version}" ]]; then
  echo "release metadata: frontend/package-lock.json version ${frontend_lock_version:-<missing>} does not match VERSION ${current_version}" >&2
  exit 1
fi

if ! grep -Eq "^## \[${current_version//./\\.}\] - [0-9]{4}-[0-9]{2}-[0-9]{2}$" CHANGELOG.md; then
  echo "release metadata: CHANGELOG.md lacks a dated [${current_version}] release heading" >&2
  exit 1
fi

# Compare declared build toolchains before the current-mode early return.
# This checks source metadata only; CI/build jobs still execute the toolchains.
python3 - <<'PY'
from pathlib import Path
import hashlib
import json
import re
import sys


def fail(path, detail):
    sys.exit(f"release metadata: {path}: {detail}")


def read(path):
    try:
        return Path(path).read_text()
    except OSError as exc:
        fail(path, f"cannot read: {exc}")


def one(path, pattern, label):
    matches = re.findall(pattern, read(path), re.MULTILINE)
    if len(matches) != 1:
        fail(path, f"expected exactly one {label} declaration, found {len(matches)}")
    return matches[0]


minimum = one("go.mod", r"^go[ \t]+([0-9]+\.[0-9]+(?:\.[0-9]+)?)[ \t]*$", "go minimum")
toolchain = one("go.mod", r"^toolchain[ \t]+go([0-9]+\.[0-9]+\.[0-9]+)[ \t]*$", "Go toolchain")


def version_parts(value):
    parts = tuple(map(int, value.split(".")))
    return parts if len(parts) == 3 else (*parts, 0)


if version_parts(minimum) > version_parts(toolchain):
    fail("go.mod", f"go minimum {minimum} exceeds toolchain go{toolchain}")

backend_tag = one("Dockerfile", r"^FROM[ \t]+golang:([^ \t\r\n]+)", "Go builder image")
if backend_tag != f"{toolchain}-alpine":
    fail("Dockerfile", f"Go builder {backend_tag} does not match go.mod toolchain go{toolchain}")

frontend_path = "frontend/package.json"
try:
    node_version = json.loads(read(frontend_path))["engines"]["node"]
except (ValueError, KeyError, TypeError):
    fail(frontend_path, "engines.node must declare an exact Node version")
if not isinstance(node_version, str) or not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", node_version):
    fail(frontend_path, "engines.node must declare an exact Node version")
frontend_tag = one("frontend/Dockerfile", r"^FROM[ \t]+node:([^ \t\r\n]+)", "Node builder image")
node_match = re.fullmatch(r"([0-9]+\.[0-9]+\.[0-9]+)-alpine([0-9]+\.[0-9]+)@sha256:([0-9a-f]{64})", frontend_tag)
if not node_match:
    fail("frontend/Dockerfile", f"Node builder {frontend_tag} must declare an exact Node/Alpine tag and SHA-256 digest")
if node_match.group(1) != node_version:
    fail("frontend/Dockerfile", f"Node builder {node_match.group(1)} does not match frontend/package.json engines.node {node_version}")


def check_workflow(path, action, key, expected):
    content = read(path)
    steps = re.findall(rf"^[ \t]*(?:-[ \t]*)?uses:[ \t]*actions/{action}@", content, re.MULTILINE)
    pins = re.findall(rf"^[ \t]*{key}:[ \t]*['\"]?([^'\" \t\r\n#]+)['\"]?[ \t]*(?:#.*)?$", content, re.MULTILINE)
    if not steps or len(pins) != len(steps):
        fail(path, f"expected one {key} pin per {action} step; found {len(pins)} pins for {len(steps)} steps")
    for pin in pins:
        if pin != expected:
            authority = "go.mod toolchain" if key == "go-version" else "frontend/package.json toolchain authority"
            fail(path, f"{key} {pin} does not match {authority} {expected}")


for workflow in (
    ".github/workflows/ci.yml",
    ".github/workflows/capability-bundle.yml",
    ".github/workflows/foigent-protocol-conformance.yml",
):
    check_workflow(workflow, "setup-go", "go-version", toolchain)
check_workflow(".github/workflows/ci.yml", "setup-node", "node-version-file", frontend_path)
if re.search(r"^[ \t]*node-version:", read(".github/workflows/ci.yml"), re.MULTILINE):
    fail(".github/workflows/ci.yml", "setup-node must use only the frontend/package.json version file")

# The restore helper is a declared customer-package artifact. Keep its
# documented immutable identity tied to the bytes actually shipped.
restore_path = "scripts/source-budget-restore-rebase.sql"
restore_bytes = Path(restore_path).read_bytes()
readme_path = "deploy/customer/README.md"
restore_sha = one(readme_path, r"^- SHA-256: `([0-9a-f]{64})`$", "restore helper SHA-256")
restore_size = one(readme_path, r"^- Bytes: `([0-9]+)`$", "restore helper byte count")
if restore_sha != hashlib.sha256(restore_bytes).hexdigest() or int(restore_size) != len(restore_bytes):
    fail(readme_path, f"declared restore helper identity differs from {restore_path}")
PY

base_sha="${BASE_SHA:-}"
head_sha="${HEAD_SHA:-HEAD}"
if [[ -z "${base_sha}" ]]; then
  printf 'RELEASE_METADATA_OK version=%s mode=current\n' "${current_version}"
  exit 0
fi
if [[ ! "${base_sha}" =~ ^[0-9a-f]{40}$ ]] || ! git cat-file -e "${base_sha}^{commit}" 2>/dev/null; then
  echo "release metadata: BASE_SHA must identify an available 40-character commit" >&2
  exit 2
fi
if [[ "${head_sha}" != "HEAD" && ! "${head_sha}" =~ ^[0-9a-f]{40}$ ]]; then
  echo "release metadata: HEAD_SHA must be HEAD or a 40-character commit" >&2
  exit 2
fi
if ! git cat-file -e "${head_sha}^{commit}" 2>/dev/null; then
  echo "release metadata: HEAD_SHA commit is unavailable" >&2
  exit 2
fi

# This classifies known paths, not the semantic intent of prose or test code.
# Shipped contracts remain subject to the version/review policy in governance.
# Consume the diff through a checked pipeline: process substitution can hide a
# failed git diff and incorrectly turn it into an empty non-release change.
change_flags="$(
  git diff --no-renames --name-only -z "${base_sha}" "${head_sha}" | {
    result=false
    version_changed=false
    changelog_changed=false
    while IFS= read -r -d '' path; do
      case "${path}" in
        (VERSION) version_changed=true ;;
        (CHANGELOG.md) changelog_changed=true ;;
        (docs/agent-workflows/EXECUTION-RECORD.md|docs/ops/evidence/*)
          ;;
        (*_test.go|frontend/src/*.test.ts|frontend/src/*.test.tsx|docs/architecture/*.md|docs/CODEBASE-MAP.md)
          # Go excludes _test.go from normal builds. Frontend test entrypoints
          # were checked separately; type-check, Vitest and Docker CI still run.
          ;;
        (scripts/check-release-metadata.sh|scripts/test-release-metadata.py|scripts/test-backend-integration.sh|scripts/test-backend-integration-runner.py|scripts/summarize-go-test.py)
          # Exact internal verification tools, not all scripts/check-* or test-*.
          ;;
        (*.go|go.mod|go.sum|migrations/*|cmd/*|internal/*|frontend/*|scripts/*|deploy/*|Dockerfile|docker-compose*.yml|docs/tech-ref/05-openapi-spec.yaml|docs/architecture/*)
          result=true
          ;;
      esac
    done
    printf '%s %s %s\n' "${result}" "${version_changed}" "${changelog_changed}"
  }
)"
read -r release_bearing version_changed changelog_changed <<< "${change_flags}"

if [[ "${release_bearing}" != true ]]; then
  printf 'RELEASE_METADATA_OK version=%s mode=non-release-bearing\n' "${current_version}"
  exit 0
fi

if [[ "${version_changed}" != true ]]; then
  echo "release metadata: a release-bearing change must update VERSION" >&2
  exit 1
fi
if [[ "${changelog_changed}" != true ]]; then
  echo "release metadata: a release-bearing change must update CHANGELOG.md" >&2
  exit 1
fi

base_version="$(git show "${base_sha}:VERSION" 2>/dev/null | tr -d '[:space:]' || true)"
if [[ -n "${base_version}" ]]; then
  if [[ ! "${base_version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "release metadata: base VERSION is not valid SemVer" >&2
    exit 1
  fi
  IFS=. read -r current_major current_minor current_patch <<< "${current_version}"
  IFS=. read -r base_major base_minor base_patch <<< "${base_version}"
  if (( current_major < base_major ||
        (current_major == base_major && current_minor < base_minor) ||
        (current_major == base_major && current_minor == base_minor && current_patch <= base_patch) )); then
    echo "release metadata: VERSION ${current_version} must be greater than base ${base_version}" >&2
    exit 1
  fi
fi

printf 'RELEASE_METADATA_OK version=%s mode=release-bearing\n' "${current_version}"
