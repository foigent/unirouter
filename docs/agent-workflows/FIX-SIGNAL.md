# FIX-SIGNAL.md - PRD3 Batch 6 API Contract Refresh

**Date:** 2026-07-05
**Status:** Fixed awaiting Canary

PRD3 Batch 5 PRD4 backlog hygiene passed Canary 6/6 in
`docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merged PR #68 at `261787a`.

Codex has completed PRD3 Batch 6 as an API contract cleanup for P3-F04/P3-F11.
This aligns backend route registration, backend `/docs`, frontend `/docs`,
OpenAPI route coverage, and route parity guards at the route/method level.

## Boundaries

- PRD3 remains cleanup/optimization of existing functionality only.
- This batch changes docs, a docs page route index, OpenAPI, and parity tests.
- No backend route, handler, RBAC, frontend navigation, gateway behavior, request
  body, response body, or telemetry behavior changed.
- OpenAPI is refreshed as current route-level coverage. Detailed response schemas
  remain generic where no stable typed contract has been confirmed.
- This does not generate SDKs; SDKs remain PRD4/later after contract maturity.

## Verification Request

Please verify:

1. Backend `/docs` in `cmd/uniroute/main.go`:
   - no longer says `PRD1 API index`;
   - uses `Current API contract index`;
   - includes current route families such as OIDC, Usage Analytics, Internal
     Status, Error Docs, Model Governance, Notifications, License, and
     Sensitive Words.
2. Frontend `/docs` in `frontend/src/pages/ApiDocsPage.tsx`:
   - no longer says `PRD2 当前可用接口索引`;
   - uses `当前 API 合同索引`;
   - renders current post-PRD1 route families.
3. `docs/tech-ref/05-openapi-spec.yaml`:
   - no longer describes itself as a PRD1 snapshot;
   - includes route/method coverage for all registered backend mux routes.
4. `cmd/uniroute/route_parity_test.go` now guards:
   - frontend-client-required routes are registered;
   - OpenAPI covers registered routes;
   - frontend API Docs covers registered route paths;
   - backend docs page does not regress to stale PRD1 wording.
5. Status/evidence docs are updated:
   - `docs/ops/evidence/PRD3-B06-api-contract-refresh-local-20260705.md`;
   - `docs/ops/CURRENT-STATUS.md`;
   - `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`;
   - `docs/issues/OPEN-ISSUES.md`;
   - `docs/tech-ref/README.md`;
   - `docs/DOC-INVENTORY.md`;
   - `docs/CODEBASE-MAP.md`.
6. Run these commands:

```bash
go test ./cmd/uniroute -run 'Test(OpenAPISpecCoversRegisteredRoutes|FrontendDocsPageCoversRegisteredRoutes|BackendDocsPageUsesCurrentContractLanguage|AdminRoutesRegisteredForFrontendClient)' -count=1 -v
npm --prefix frontend test -- --run ApiDocsPage client
go test ./... -count=1
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

## Expected Result

- If all checks pass, update `docs/agent-workflows/VERIFY-RESULT.md` with PRD3
  Batch 6 PASS.
- If any check fails, list the exact file and line-level issue.

## Verification Pointers

- `cmd/uniroute/main.go`
- `cmd/uniroute/route_parity_test.go`
- `docs/tech-ref/05-openapi-spec.yaml`
- `frontend/src/pages/ApiDocsPage.tsx`
- `frontend/src/pages/__tests__/ApiDocsPage.test.tsx`
- `docs/ops/evidence/PRD3-B06-api-contract-refresh-local-20260705.md`
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/issues/OPEN-ISSUES.md`
