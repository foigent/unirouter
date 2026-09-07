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
        (scripts/check-release-metadata.sh|scripts/test-release-metadata.py)
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
