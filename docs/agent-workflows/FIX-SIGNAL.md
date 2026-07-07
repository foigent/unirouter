# FIX-SIGNAL.md - PRD4 Batch 1E Quota Vocabulary And Key Quota Foundation

**Date:** 2026-07-07
**Status:** Fixed awaiting Canary
**Issue:** `PRD4-QUOTA-001`
**Matrix Row:** `P4-F03`

## Verification Request

Please verify PRD4 Batch 1E: Quota Vocabulary And Key Quota Foundation.

This batch standardizes the human-facing quota vocabulary around RPM, RPS, TPM,
and tokens/s while preserving the existing `qps_limit` storage/API compatibility
boundary. It adds key-level quota visibility in API key responses and frontend
tables/forms without enabling Scheduler V2, token reservation, TPM enforcement,
or budget hard-blocking.

## Scope To Verify

Backend/API:

- `GET /api/v1/keys` and `GET /api/v1/keys/{id}` continue returning legacy
  `qps_limit`, and also expose preferred `rps_limit`.
- Key responses include a structured `quota` object with `rpm_limit`,
  `rps_limit`, `qps_compat_limit`, `tpm_limit`, `tokens_per_second`,
  `budget_limit_cents`, `tpm_source`, and a compatibility note.
- `POST /api/v1/keys` accepts preferred `rps_limit` and legacy `qps_limit`.
  Conflicting values are rejected instead of silently choosing one field.
- `PUT /api/v1/keys/{id}` accepts preferred `rps_limit` and legacy `qps_limit`.
  Conflicting or non-positive update values are rejected.
- No migration is added; the existing `api_keys.qps_limit` column remains the
  compatibility storage for per-key RPS.

Frontend:

- Admin API Keys and Developer Console key surfaces display `RPM / RPS / TPM`.
- Key create/edit forms use `RPS 限速`, not user-facing `QPS 限速`.
- The frontend sends `rps_limit` for new key creation while remaining able to
  render legacy records that only contain `qps_limit`.
- TPM is shown as `未配置` in this batch; no UI implies token quota enforcement.

Docs/status:

- Adds `docs/tech-ref/21-prd4-quota-vocabulary.md`.
- Updates docs indexes and frontend/deployment terminology from user-facing
  QPS to RPS where appropriate.
- Records User-reported Batch 1D Canary/Sentinel completion after `349ac75` /
  PR #87.
- Marks `P4-F03` / `PRD4-QUOTA-001` fixed awaiting Canary.

## Non-Scope

Do not expect this batch to implement:

- Scheduler V2, token reservation, capacity-aware dispatch, or tokens/s
  admission.
- Key-level TPM enforcement or new TPM persistence.
- Budget hard-blocking or new billing admission behavior.
- Quota change audit reason/idempotency fields.
- New API key schema migration or removal/rename of `qps_limit`.
- OpenTelemetry, trace retention cleanup, or runtime plugin/PlungeIn execution.
- Real upstream or real SSO target-environment evidence.

## Required Verification Commands

```bash
rg -n "rps_limit|qps_compat_limit|apiKeyQuotaPayload|createRPSLimit|updateRPSLimit|keyQuotaText|RPM / RPS / TPM|RPS 限速" internal frontend/src docs/tech-ref/21-prd4-quota-vocabulary.md
rg -n "DEFAULT_KEY_QPS|QPS|qps_limit|TPS" .env.example docker-compose.yml docs/ops/deployment-runbook.md docs/tech-ref docs/PRD-Phase4.md frontend/src
rg -n "PRD4-QUOTA-001|P4-F03|Batch 1E|Quota Vocabulary|rps_limit|PRD4-RUNTIME-001|Batch 1D" docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/CODEBASE-MAP.md docs/PRD-Phase4.md
go test ./internal/admin -run 'Test(CreateRPSLimit|UpdateRPSLimit|APIKeyQuotaPayload)' -count=1
go test ./internal/admin ./cmd/uniroute -count=1
go test ./...
npm --prefix frontend test -- src/utils/__tests__/quota.test.ts src/pages/__tests__/KeysPage.test.tsx src/pages/__tests__/DeveloperConsolePage.test.tsx
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

## Acceptance Criteria

1. User-facing quota labels use RPM, RPS, TPM, and tokens/s; ambiguous TPS is not
   introduced in product UI.
2. Legacy `qps_limit` compatibility remains intact for existing API clients and
   DB schema.
3. Preferred `rps_limit` is accepted, serialized, and used by new frontend
   key-create flows.
4. Conflicting `rps_limit`/`qps_limit` values fail explicitly.
5. Key quota visibility shows TPM as not configured instead of implying
   enforcement.
6. No Scheduler V2, TPM enforcement, budget hard-block, or schema migration is
   introduced.
7. Backend focused tests, frontend focused tests, full Go/frontend tests,
   frontend build, terminology grep, and `git diff --check` pass.
8. Docs consistently show Batch 1D as Canary/Sentinel complete per User report
   and Batch 1E as fixed awaiting Canary.
