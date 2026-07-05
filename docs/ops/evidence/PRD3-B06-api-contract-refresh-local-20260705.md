# PRD3 Batch 6 API Contract Refresh - Local Evidence

Date: 2026-07-05
Owner: Codex
Scope: PRD3 P3-F04/P3-F11 route-level API contract cleanup

## Summary

This batch refreshes the active UniRouter API contract at the route/method level.
It aligns:

- backend route registration in `cmd/uniroute/main.go`;
- backend `/docs` page wording and route index;
- frontend `/docs` page wording and route index;
- `docs/tech-ref/05-openapi-spec.yaml`;
- route parity tests in `cmd/uniroute/route_parity_test.go`.

## Decisions

- The OpenAPI document is the current route-level contract for active routes.
- Response schemas remain generic where no stable typed response contract exists.
- This batch does not generate SDKs, add routes, remove routes, change RBAC,
  change handler behavior, or change frontend navigation.

## Files Changed

- `cmd/uniroute/main.go`
- `cmd/uniroute/route_parity_test.go`
- `docs/tech-ref/05-openapi-spec.yaml`
- `frontend/src/pages/ApiDocsPage.tsx`
- `frontend/src/pages/__tests__/ApiDocsPage.test.tsx`
- `docs/tech-ref/README.md`
- `docs/DOC-INVENTORY.md`
- `docs/CODEBASE-MAP.md`
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/agent-workflows/FIX-SIGNAL.md`

## Local Verification

Commands run:

```bash
go test ./cmd/uniroute -run 'Test(OpenAPISpecCoversRegisteredRoutes|FrontendDocsPageCoversRegisteredRoutes|BackendDocsPageUsesCurrentContractLanguage|AdminRoutesRegisteredForFrontendClient)' -count=1 -v
npm --prefix frontend test -- --run ApiDocsPage client
go test ./... -count=1
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
rg -n "PRD1 API index|PRD2 当前可用接口索引|Current PRD1 API contract snapshot|Current PRD1 API path snapshot|当前 PRD1 API 路径快照" cmd/uniroute/main.go frontend/src/pages/ApiDocsPage.tsx docs/tech-ref/05-openapi-spec.yaml docs/tech-ref/README.md docs/DOC-INVENTORY.md docs/CODEBASE-MAP.md
```

Results:

- Route parity target tests passed.
- Frontend API Docs and API client target tests passed: 2 files / 28 tests.
- Full Go tests passed: `go test ./... -count=1`.
- Full frontend tests passed: 18 files / 97 tests.
- Frontend build passed with the existing Vite large chunk warning.
- `git diff --check` passed.
- Active source/docs stale wording check returned no matches. Stale strings remain
  only in negative regression assertions and Canary verification instructions.
