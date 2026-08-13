#!/usr/bin/env bash
set -euo pipefail

if ! date -d '1970-01-01T00:00:00Z' +%s >/dev/null 2>&1; then
  echo "image retention report test: GNU date is required" >&2
  exit 2
fi

repo_root="$(git rev-parse --show-toplevel)"
test_dir="$(mktemp -d "${TMPDIR:-/tmp}/unirouter-image-report-test.XXXXXX")"
cleanup() {
  rm -rf -- "${test_dir}"
}
trap cleanup EXIT

mkdir -p "${test_dir}/bin"
cat >"${test_dir}/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

case "$1 ${2:-}" in
  "info ")
    exit 0
    ;;
  "container ls")
    if [[ "${3:-}" == "-q" ]]; then
      printf 'running-container\n'
    elif [[ "${3:-}" == "-aq" ]]; then
      printf 'running-container\nstopped-container\n'
    else
      exit 2
    fi
    ;;
  "container inspect")
    case "${5:-}" in
      running-container) printf 'sha256:running\n' ;;
      stopped-container) printf 'sha256:stopped\n' ;;
      *) exit 2 ;;
    esac
    ;;
  "image ls")
    printf '%s\n' \
      sha256:running \
      sha256:stopped \
      sha256:keep \
      sha256:candidate \
      sha256:unmanaged \
      sha256:young \
      sha256:dangling
    ;;
  "image inspect")
    if [[ "${3:-}" == "--format" ]]; then
      [[ "${5:-}" == "unirouter-release/backend:keep" ]] || exit 2
      printf 'sha256:keep\n'
    else
      cat "${UNIROUTER_IMAGE_REPORT_FIXTURE}/images.json"
    fi
    ;;
  *)
    echo "unexpected fake Docker call: $*" >&2
    exit 2
    ;;
esac
EOF
chmod 0755 "${test_dir}/bin/docker"

cat >"${test_dir}/images.json" <<'EOF'
[
  {"Id":"sha256:running","Created":"2000-01-01T00:00:00Z","Size":1048576,"RepoTags":["unirouter-release/backend:running"],"Config":{"Labels":{"io.unirouter.component":"backend"}}},
  {"Id":"sha256:stopped","Created":"2000-01-01T00:00:00Z","Size":1048576,"RepoTags":["unirouter-release/backend:stopped"],"Config":{"Labels":{"io.unirouter.component":"backend"}}},
  {"Id":"sha256:keep","Created":"2000-01-01T00:00:00Z","Size":1048576,"RepoTags":["unirouter-release/backend:keep"],"Config":{"Labels":{"io.unirouter.component":"backend"}}},
  {"Id":"sha256:candidate","Created":"2000-01-01T00:00:00Z","Size":2097152,"RepoTags":["unirouter-release/backend:candidate"],"Config":{"Labels":{"io.unirouter.component":"backend"}}},
  {"Id":"sha256:unmanaged","Created":"2000-01-01T00:00:00Z","Size":1048576,"RepoTags":["postgres:17"],"Config":{"Labels":{}}},
  {"Id":"sha256:young","Created":"2099-01-01T00:00:00Z","Size":1048576,"RepoTags":["unirouter-release/frontend:young"],"Config":{"Labels":{"io.unirouter.component":"frontend"}}},
  {"Id":"sha256:dangling","Created":"2000-01-01T00:00:00Z","Size":3145728,"RepoTags":null,"Config":{"Labels":{"io.unirouter.component":"frontend"}}}
]
EOF
printf 'unirouter-release/backend:keep\n' >"${test_dir}/keep"

PATH="${test_dir}/bin:${PATH}" \
UNIROUTER_IMAGE_REPORT_FIXTURE="${test_dir}" \
  "${repo_root}/scripts/docker-image-retention-report.sh" \
    --keep-file "${test_dir}/keep" \
    --min-age-days 14 >"${test_dir}/report.tsv"

grep -Eq '^protected-running-container[[:space:]].*sha256:running' "${test_dir}/report.tsv"
grep -Eq '^protected-stopped-container[[:space:]].*sha256:stopped' "${test_dir}/report.tsv"
grep -Eq '^protected-keep[[:space:]].*sha256:keep' "${test_dir}/report.tsv"
grep -Eq '^candidate[[:space:]].*sha256:candidate' "${test_dir}/report.tsv"
grep -Eq '^candidate[[:space:]].*sha256:dangling[[:space:]]+-$' "${test_dir}/report.tsv"
grep -Eq '^unmanaged[[:space:]].*sha256:unmanaged' "${test_dir}/report.tsv"
grep -Eq '^young[[:space:]].*sha256:young' "${test_dir}/report.tsv"
grep -F $'SUMMARY\trunning_protected=1\tstopped_protected=1\tkeep_protected=1\tyoung=1\tunmanaged=1\tcandidates=2\tcandidate_virtual_mib=5.0' "${test_dir}/report.tsv"
grep -F 'READ_ONLY: no image was removed or untagged' "${test_dir}/report.tsv"

echo "docker_image_retention_report_test=pass"
