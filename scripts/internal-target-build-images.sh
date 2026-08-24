#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  RUNTIME_VERSION=VERSION RUNTIME_COMMIT=FULL_COMMIT \
    scripts/internal-target-build-images.sh

Optional environment:
  GOPROXY                         Go module proxy chain passed to the backend
                                  build. Defaults to the official proxy.
  UNIROUTER_BACKEND_IMAGE        Exact local backend target tag. Defaults to
                                 unirouter-release/backend:<12-char commit>.
  UNIROUTER_FRONTEND_IMAGE       Exact local frontend target tag. Defaults to
                                 unirouter-release/frontend:<12-char commit>.
  UNIROUTER_FRONTEND_REUSE_FROM  Reuse an earlier helper-built frontend image
                                 only when Git proves frontend/ is unchanged.

This internal development/test helper builds each component at most once for a
clean, exact Git commit. Existing target tags are reused only when their OCI
version, revision and UniRouter component labels match. It never overwrites an
existing tag and never deletes images, containers, caches or volumes.
EOF
}

if [[ $# -gt 0 ]]; then
  case "$1" in
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "internal target image build: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
fi

for command_name in docker git flock; do
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "internal target image build: ${command_name} is required" >&2
    exit 2
  fi
done
if ! docker info >/dev/null 2>&1; then
  echo "internal target image build: Docker daemon is unavailable" >&2
  exit 2
fi

runtime_version="${RUNTIME_VERSION:-}"
runtime_commit="${RUNTIME_COMMIT:-}"
go_proxy="${GOPROXY:-https://proxy.golang.org,direct}"
if [[ ! "${runtime_version}" =~ ^[A-Za-z0-9._+-]+$ ]] || (( ${#runtime_version} > 128 )); then
  echo "internal target image build: RUNTIME_VERSION must be a safe immutable value of at most 128 bytes" >&2
  exit 2
fi
if [[ ! "${runtime_commit}" =~ ^[0-9a-f]{40}$ && ! "${runtime_commit}" =~ ^[0-9a-f]{64}$ ]]; then
  echo "internal target image build: RUNTIME_COMMIT must be a complete lowercase 40- or 64-character commit" >&2
  exit 2
fi
if [[ -z "${go_proxy}" || "${go_proxy}" =~ [[:space:]] ]] || (( ${#go_proxy} > 512 )); then
  echo "internal target image build: GOPROXY must be one non-empty proxy chain without whitespace" >&2
  exit 2
fi

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || {
  echo "internal target image build: run from a Git checkout" >&2
  exit 2
}
cd "${repo_root}"
product_version="$(tr -d '[:space:]' < VERSION)"
if [[ ! "${product_version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "internal target image build: VERSION must contain one MAJOR.MINOR.PATCH value" >&2
  exit 2
fi
head_commit="$(git rev-parse HEAD)"
if [[ "${head_commit}" != "${runtime_commit}" ]]; then
  echo "internal target image build: RUNTIME_COMMIT does not equal checkout HEAD" >&2
  exit 2
fi
if [[ -n "$(git status --porcelain=v1 --untracked-files=normal)" ]]; then
  echo "internal target image build: checkout is not clean; refuse an unrepeatable image" >&2
  exit 2
fi

exec 9>/run/lock/unirouter-docker-lifecycle.lock
if ! flock -n 9; then
  echo "internal target image build: another build or maintenance run holds the lifecycle lock" >&2
  exit 1
fi

release_tag="${runtime_commit:0:12}"
backend_image="${UNIROUTER_BACKEND_IMAGE:-unirouter-release/backend:${release_tag}}"
frontend_image="${UNIROUTER_FRONTEND_IMAGE:-unirouter-release/frontend:${release_tag}}"
frontend_reuse_from="${UNIROUTER_FRONTEND_REUSE_FROM:-}"

validate_image_ref() {
  local name="$1" image_ref="$2"
  if [[ -z "${image_ref}" || "${image_ref}" =~ [[:space:]] || "${image_ref}" == *@* ]]; then
    echo "internal target image build: ${name} must be one non-digest local image tag" >&2
    exit 2
  fi
}

validate_image_ref UNIROUTER_BACKEND_IMAGE "${backend_image}"
validate_image_ref UNIROUTER_FRONTEND_IMAGE "${frontend_image}"
if [[ -n "${frontend_reuse_from}" ]]; then
  validate_image_ref UNIROUTER_FRONTEND_REUSE_FROM "${frontend_reuse_from}"
fi

label_value() {
  local image_ref="$1" label_name="$2"
  docker image inspect --format "{{ index .Config.Labels \"${label_name}\" }}" "${image_ref}"
}

validate_exact_target() {
  local component="$1" image_ref="$2"
  local actual_component actual_revision actual_version
  actual_component="$(label_value "${image_ref}" io.unirouter.component)"
  actual_revision="$(label_value "${image_ref}" org.opencontainers.image.revision)"
  actual_version="$(label_value "${image_ref}" org.opencontainers.image.version)"
  if [[ "${actual_component}" != "${component}" ||
        "${actual_revision}" != "${runtime_commit}" ||
        "${actual_version}" != "${runtime_version}" ]]; then
    echo "internal target image build: existing immutable tag has mismatched metadata: ${image_ref}" >&2
    exit 1
  fi
}

backend_needs_build=true
if docker image inspect "${backend_image}" >/dev/null 2>&1; then
  validate_exact_target backend "${backend_image}"
  backend_needs_build=false
  echo "REUSE_EXISTING component=backend image=${backend_image}"
fi

frontend_needs_build=true
selected_frontend_image="${frontend_image}"
if [[ -n "${frontend_reuse_from}" ]]; then
  if ! docker image inspect "${frontend_reuse_from}" >/dev/null 2>&1; then
    echo "internal target image build: frontend reuse source does not exist: ${frontend_reuse_from}" >&2
    exit 1
  fi
  source_component="$(label_value "${frontend_reuse_from}" io.unirouter.component)"
  source_revision="$(label_value "${frontend_reuse_from}" org.opencontainers.image.revision)"
  if [[ "${source_component}" != "frontend" || ! "${source_revision}" =~ ^[0-9a-f]{40}$ ]]; then
    echo "internal target image build: frontend reuse source lacks helper-generated provenance" >&2
    exit 1
  fi
  if ! git cat-file -e "${source_revision}^{commit}" 2>/dev/null; then
    echo "internal target image build: frontend reuse source commit is absent from this checkout" >&2
    exit 1
  fi
  if ! git merge-base --is-ancestor "${source_revision}" "${runtime_commit}"; then
    echo "internal target image build: frontend reuse source is not an ancestor of RUNTIME_COMMIT" >&2
    exit 1
  fi
  if ! git diff --quiet "${source_revision}" "${runtime_commit}" -- frontend; then
    echo "internal target image build: frontend changed; reuse is not allowed" >&2
    exit 1
  fi
  selected_frontend_image="${frontend_reuse_from}"
  frontend_needs_build=false
  echo "REUSE_UNCHANGED component=frontend image=${selected_frontend_image} source_revision=${source_revision}"
elif docker image inspect "${frontend_image}" >/dev/null 2>&1; then
  validate_exact_target frontend "${frontend_image}"
  frontend_needs_build=false
  echo "REUSE_EXISTING component=frontend image=${frontend_image}"
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ "${backend_needs_build}" == true || "${frontend_needs_build}" == true ]]; then
  "${script_dir}/docker-storage-preflight.sh" \
    --path /var/lib/docker \
    --minimum-free-gib 8 \
    --maximum-use-percent 80
fi

if [[ "${backend_needs_build}" == true ]]; then
  docker build \
    --label "io.unirouter.component=backend" \
    --label "org.opencontainers.image.revision=${runtime_commit}" \
    --label "org.opencontainers.image.version=${runtime_version}" \
    --build-arg "RUNTIME_VERSION=${runtime_version}" \
    --build-arg "RUNTIME_COMMIT=${runtime_commit}" \
    --build-arg "GOPROXY=${go_proxy}" \
    --build-arg UNIROUTER_RELEASE_TRUST_MODE=development \
    --tag "${backend_image}" .
  validate_exact_target backend "${backend_image}"
  echo "BUILT_ONCE component=backend image=${backend_image}"
fi

if [[ "${frontend_needs_build}" == true ]]; then
  docker build \
    --label "io.unirouter.component=frontend" \
    --label "org.opencontainers.image.revision=${runtime_commit}" \
    --label "org.opencontainers.image.version=${runtime_version}" \
    --build-arg VITE_DEPLOY_ENV=development \
    --build-arg "VITE_APP_VERSION=${product_version}" \
    --tag "${frontend_image}" ./frontend
  validate_exact_target frontend "${frontend_image}"
  echo "BUILT_ONCE component=frontend image=${frontend_image}"
fi

backend_image_id="$(docker image inspect --format '{{.Id}}' "${backend_image}")"
frontend_image_id="$(docker image inspect --format '{{.Id}}' "${selected_frontend_image}")"
printf 'SELECTED UNIROUTER_BACKEND_IMAGE=%s IMAGE_ID=%s\n' "${backend_image}" "${backend_image_id}"
printf 'SELECTED UNIROUTER_FRONTEND_IMAGE=%s IMAGE_ID=%s\n' "${selected_frontend_image}" "${frontend_image_id}"
