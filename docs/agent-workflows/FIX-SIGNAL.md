# FIX-SIGNAL.md - PRD4 Batch 1O API Key Input And Security Header Hardening

**Fixer:** main agent
**Date:** file system timestamp is authoritative
**Risk level:** R3 security/API behavior hardening
**Scope:** P4-F12 security, privacy, and redaction guardrails

## Verification Request

Please verify PRD4 Batch 1O: API Key Input And Security Header Hardening.

This batch converts application-code findings from
`docs/ops/evidence/UNIROUTERS-TEST-REPORT-FINAL-20260708.md` into a narrow
security/API hardening slice tracked as `PRD4-SECURITY-001`:

- API key create/update trims names and rejects empty, over-255-character,
  control-containing, markup-like, or SQL-like display names;
- API key create/update rejects negative `project_id` instead of resolving it to
  a default project or touching the database;
- all HTTP responses pass through baseline browser security headers;
- `GET /` no longer exposes hardcoded version data, and unknown GET paths that
  fall through the root pattern return `404 {"error":"not_found"}`.

## Scope To Verify

Product/test files:

- `internal/admin/key_handlers.go`
- `internal/admin/admin_mock_test.go`
- `internal/middleware/security_headers.go`
- `internal/middleware/security_headers_test.go`
- `cmd/uniroute/main.go`
- `cmd/uniroute/main_test.go`

Evidence/status files:

- `docs/ops/evidence/UNIROUTERS-TEST-REPORT-FINAL-20260708.md`
- `docs/agent-workflows/FIX-SIGNAL.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`
- `docs/PROJECT-HANDOFF.md`
- `docs/DOC-INVENTORY.md`
- `docs/README.md`
- `docs/PRD-Phase4.md`
- `docs/CODEBASE-MAP.md`

## Expected Behavior

- API key display names that contain HTML tag characters, quote/semicolon-style
  SQL payload characters, control characters, `--`, or more than 255 characters
  return HTTP 400 on create/update.
- Negative `project_id` returns HTTP 400 on API key create/update.
- Valid API key create/update flows remain unchanged.
- Responses include `X-Content-Type-Options`, `X-Frame-Options`,
  `Referrer-Policy`, `Permissions-Policy`, and a frame/base/form
  `Content-Security-Policy`.
- `GET /` still returns service running status, but no longer returns a
  hardcoded `version`.
- Unknown GET paths such as `/api/v1/orgs` return standard 404 JSON without
  root service/version data.

## Non-Scope

Do not expect this batch to:

- change billing, routing, RBAC, migrations, trace storage, SSO, upstream,
  wallet, provider dispatch, frontend behavior, API key hashing, or secret
  handling;
- solve Go standard-library vulnerability findings by changing the local Go
  toolchain or build image;
- change target-environment Grafana URL, `RUNTIME_ENVIRONMENT`, runtime
  version/commit `ldflags`, Docker healthcheck wiring, or target test database
  cleanup;
- claim broad target-environment `Production verified` status.

## Required Verification Commands

```bash
cd /Users/m4/Desktop/projects/UniRouter

rg -n "normalizeAPIKeyName|SecurityHeaders|TestSecurityHeaders|TestRootHandlerDoesNotExposeHardcodedVersion|PRD4-SECURITY-001|Batch 1O|UNIROUTERS-TEST-REPORT-FINAL-20260708" internal/admin internal/middleware cmd/uniroute docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/PRD-Phase4.md docs/CODEBASE-MAP.md

go test ./internal/admin -run 'Test(CreateAPIKey_RejectsUnsafeNameAndOversizedName|CreateAPIKey_RejectsNegativeProjectID|UpdateAPIKey_RejectsUnsafeName|UpdateAPIKey_RejectsNegativeProjectID)$' -count=1
go test ./internal/middleware -run TestSecurityHeaders -count=1
go test ./cmd/uniroute -run 'TestRootHandler(DoesNotExposeHardcodedVersion|ReturnsStandardNotFoundForUnknownPath)' -count=1
go test ./internal/admin ./internal/middleware ./cmd/uniroute -count=1
go test ./... -count=1

git diff --check
```

## Acceptance Criteria

1. API key create rejects stored HTML-like and SQL-like display-name payloads
   with HTTP 400 before database mutation.
2. API key create rejects names longer than 255 characters with HTTP 400, not
   database-driven HTTP 500.
3. API key create rejects negative `project_id` with HTTP 400.
4. API key update applies the same unsafe-name and negative-`project_id`
   rejection.
5. Baseline security headers are present on wrapped HTTP responses.
6. Root response no longer exposes hardcoded version data.
7. Unknown GET paths that fall through the root pattern return standard 404 JSON
   without hardcoded version data.
8. Focused admin, middleware, and cmd tests pass.
9. Broad Go regression and `git diff --check` pass.
10. Open runtime/build-chain findings from the final test report remain tracked
    separately and are not falsely marked fixed by this application-code batch.
