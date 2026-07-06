# PRD3 Batch 14 Admin Helper Boundary Local Evidence

Date: 2026-07-06

## Scope

PRD3 Batch 14 is a P3-F09 backend module boundary cleanup batch. It keeps the
existing `internal/admin` package and public route/API contracts intact, but
separates cross-domain helper concerns from the core handler wiring.

Inputs:

- User reported PRD3 Batch 13 Canary/Sentinel/Atlas completed.
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md` tracks P3-F09 as backend module
  boundary cleanup for `internal/admin`.
- The existing package already has domain handler files, but `handlers.go` also
  contained event emitters, HTTP JSON/pagination helpers, and model-list string
  normalization.

## Code Changes

Helper boundaries introduced:

- `internal/admin/event_helpers.go`
  - `emitAPIKeyDisabledEvent`
  - `emitModelLifecycleEvent`
  - `emitLicenseWarningEvent`
- `internal/admin/http_helpers.go`
  - `parsePagination`
  - `paginationOffset`
  - `writeJSON`
  - central `defaultPageSize` / `maxPageSize`
- `internal/admin/model_helpers.go`
  - `joinStrings`
  - `normalizeModels`

Call sites updated to use the shared pagination helper instead of local
duplicate parsing:

- `internal/admin/log_handlers.go`
- `internal/admin/user_handlers.go`
- `internal/admin/grant_handlers.go`
- `internal/admin/wallet_handlers.go`
- `internal/admin/channel_key_handlers.go`
- `internal/admin/model_handlers.go`
- `internal/admin/model_governance_handlers.go`
- `internal/admin/notification_handlers.go`
- `internal/admin/project_handlers.go`
- `internal/admin/audit_handlers.go`

Test coverage:

- Added `TestParsePagination` in `internal/admin/admin_test.go` for default,
  valid, invalid page, oversized page size, invalid page size, and offset
  behavior.

Docs:

- `docs/CODEBASE-MAP.md` now records the `internal/admin` helper file boundary.

## Boundaries

- No new package boundary was introduced in this batch.
- No route registration changed.
- No JSON response field, status code, SQL query, RBAC rule, billing behavior,
  audit behavior, notification event semantics, or database migration changed.
- This is not PRD4 plugin/runtime architecture work.
- P3-F10 frontend architecture cleanup, P3-F11 broader parity guards, and P3-F14
  final evidence hygiene remain follow-up work.

## Local Validation

Commands run by Codex:

```bash
go test ./internal/admin -count=1
go test ./cmd/uniroute -run 'TestAdminRoutesRegisteredForFrontendClient|TestOpenAPISpecCoversRegisteredRoutes|TestFrontendDocsPageCoversRegisteredRoutes'
go test ./...
rg -n "offset := \\(page - 1\\) \\* pageSize|page, _ := strconv.Atoi\\(r.URL.Query\\(\\)\\.Get\\(\\\"page\\\"\\)|pageSize, _ := strconv.Atoi\\(r.URL.Query\\(\\)\\.Get\\(\\\"page_size\\\"\\)" internal/admin
git diff --check
```

Observed local result:

- `go test ./internal/admin -count=1` passes.
- Route/OpenAPI/frontend-doc parity tests pass.
- Full Go tests pass.
- Duplicate admin pagination grep only returns the centralized parser in
  `internal/admin/http_helpers.go`.
- `git diff --check` passes.
