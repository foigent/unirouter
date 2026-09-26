#!/usr/bin/env bash
# Additive fresh-owner checks; the five existing regression groups are retained.
set -euo pipefail
cd "$(dirname "$0")/.."
check_only=false
if [[ ${1:-} == --check ]]; then check_only=true; shift; fi
if (($# != 1)); then
  echo 'usage: scripts/test-fresh-install.sh [--check] NEW_LOG_DIRECTORY' >&2
  exit 2
fi
log_dir=$1
# Reuse the accepted offline toolchain/CGO/compiler preflight.
bash scripts/test-backend-integration.sh --check --log-dir "$log_dir"
: "${UR_FRESH_TEST_BINARY:?set the exact native backend executable from the inspected candidate image}"
: "${UR_FRESH_TEST_IMAGE_DIGEST:?set the actual inspected candidate image digest}"
[[ -f $UR_FRESH_TEST_BINARY && -x $UR_FRESH_TEST_BINARY ]] || { echo 'native backend executable is unavailable' >&2; exit 2; }
[[ $UR_FRESH_TEST_IMAGE_DIGEST =~ ^sha256:[a-f0-9]{64}$ ]] || { echo 'invalid inspected image identity' >&2; exit 2; }
for tool in pg_dump psql "${PYTHON:-python3}"; do command -v "$tool" >/dev/null; done
pg_dump --version
psql --version
if [[ $check_only == true ]]; then
  echo 'fresh_configuration_preflight=PASS database_state_and_resource_admission=operator_required'
  exit 0
fi
mkdir "$log_dir"
log_dir=$(cd "$log_dir" && pwd)
export UR_FRESH_EVIDENCE_PARENT=$log_dir
export REQUIRE_FRESH_NATIVE=1
status=0
go test -json -tags integration -race -count=1 ./internal/installstate ./cmd/uniroute -run '^TestFresh' > "$log_dir/fresh.jsonl" 2>&1 || status=$?
summary_status=0
"${PYTHON:-python3}" -B scripts/summarize-go-test.py "$log_dir/fresh.jsonl" --exit-code "$status" --group fresh-install --runner scripts/test-fresh-install.sh || summary_status=$?
if ((status != 0)); then exit "$status"; fi
exit "$summary_status"
