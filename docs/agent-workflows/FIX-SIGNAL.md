# FIX-SIGNAL.md - PRD3 Batch 14 Admin Helper Boundary Cleanup

**Date:** 2026-07-06
**Status:** Fixed awaiting Canary

PRD3 Batch 13 runtime exposure hardening passed Canary 7/7 in
`docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merged PR #77 at `e8053f3`;
User reported Canary/Sentinel/Atlas completed.

Codex has completed PRD3 Batch 14 as a narrow P3-F09 backend helper-boundary
cleanup. This keeps `internal/admin` as one package and does not change external
route/API behavior, but it separates shared helper concerns from core handler
wiring and consolidates duplicate pagination parsing.

## Boundaries

- PRD3 remains cleanup/optimization of existing functionality only.
- No new API route, response field, status code, RBAC rule, SQL query,
  migration, package boundary, service, UI, or product capability was added.
- No billing, audit, notification, login, gateway, CORS, metrics, or deployment
  runtime behavior changed.
- This batch does not implement P3-F10 frontend architecture cleanup, P3-F11
  broader parity guards, P3-F14 final evidence closeout, or any PRD4 feature.
- No files were deleted.

## Verification Request

Please verify:

1. `internal/admin/handlers.go` now only contains handler construction/service
   wiring plus `GetMe`; cross-domain helpers have moved out.
2. Helper files have clear package-local responsibilities:
   - `internal/admin/event_helpers.go` contains admin notification event
     emitters;
   - `internal/admin/http_helpers.go` contains `writeJSON`, `parsePagination`,
     `paginationOffset`, and pagination constants;
   - `internal/admin/model_helpers.go` contains model/string normalization
     helpers.
3. Duplicate pagination parsing has been consolidated:
   - list handlers use `parsePagination(r)` and `paginationOffset(page,
     pageSize)`;
   - grep for direct list-handler `page` / `page_size` `strconv.Atoi` patterns
     should only match the centralized parser.
4. Regression coverage exists:
   - `internal/admin/admin_test.go` includes `TestParsePagination`.
5. Docs/status are self-consistent:
   - `docs/ops/CURRENT-STATUS.md` shows Batch 14 as the current verification
     request and Batch 13 complete per User report;
   - `docs/ops/PRD3-ACCEPTANCE-MATRIX.md` marks P3-F09 as `Fixed awaiting
     Canary`;
   - `docs/issues/OPEN-ISSUES.md` records Batch 14 under `PRD3-ARCH-001`;
   - `docs/CODEBASE-MAP.md` documents the new `internal/admin` helper files.
6. Evidence exists at
   `docs/ops/evidence/PRD3-B14-admin-helper-boundary-local-20260706.md`.
7. Run these commands:

```bash
go test ./internal/admin -count=1
go test ./cmd/uniroute -run 'TestAdminRoutesRegisteredForFrontendClient|TestOpenAPISpecCoversRegisteredRoutes|TestFrontendDocsPageCoversRegisteredRoutes'
go test ./...
rg -n "offset := \\(page - 1\\) \\* pageSize|page, _ := strconv.Atoi\\(r.URL.Query\\(\\)\\.Get\\(\\\"page\\\"\\)|pageSize, _ := strconv.Atoi\\(r.URL.Query\\(\\)\\.Get\\(\\\"page_size\\\"\\)" internal/admin
rg -n "PRD3 Batch 14 Admin Helper Boundary|P3-F09 \\| Backend Module Boundary Cleanup \\| Fixed awaiting Canary|PRD3-B14-admin-helper-boundary|event_helpers.go|http_helpers.go|model_helpers.go" docs/ops/CURRENT-STATUS.md docs/ops/PRD3-ACCEPTANCE-MATRIX.md docs/issues/OPEN-ISSUES.md docs/CODEBASE-MAP.md docs/ops/evidence/PRD3-B14-admin-helper-boundary-local-20260706.md
git diff --check
```

## Expected Result

- Admin targeted tests pass.
- Route/OpenAPI/frontend-doc parity tests pass.
- Full Go tests pass.
- Duplicate pagination grep only returns the centralized parser in
  `internal/admin/http_helpers.go`.
- Status/evidence grep returns Batch 14, P3-F09, evidence, and codebase-map
  matches.
- `git diff --check` passes.
- If all checks pass, update `docs/agent-workflows/VERIFY-RESULT.md` with PRD3
  Batch 14 PASS.
- If any check fails, list the exact file and line-level issue.

## Verification Pointers

- `internal/admin/handlers.go`
- `internal/admin/event_helpers.go`
- `internal/admin/http_helpers.go`
- `internal/admin/model_helpers.go`
- `internal/admin/admin_test.go`
- `docs/CODEBASE-MAP.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/evidence/PRD3-B14-admin-helper-boundary-local-20260706.md`
- `docs/agent-workflows/FIX-SIGNAL.md`
