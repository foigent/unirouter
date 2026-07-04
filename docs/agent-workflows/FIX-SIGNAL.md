# FIX-SIGNAL.md — PRD3 Batch 3 Follow-up: Empty Search Coverage And Evidence Routing

**日期：** 2026-07-05
**状态：** Fixed awaiting Canary

PRD3 Batch 3 passed Canary 5/5 in `docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merged PR #65 at `8cb71f6`; User reported Canary, Sentinel, and Atlas completed. Canary left one non-blocking suggestion: add explicit test coverage for empty topbar search routing to `/logs`.

Codex also reviewed Canary's comprehensive test report and moved it out of historical archive because it contains current deployment/security evidence and active findings.

## Boundaries

- This follow-up does not change application runtime behavior.
- This follow-up does not fix dependency vulnerabilities, CORS, login rate limiting, or metrics exposure.
- Security findings from the comprehensive report are registered as open issues for a dedicated later batch.
- `docs/testing/archive/COMPREHENSIVE-TEST-PLAN-20260705.md` remains historical planning material; the executed report is promoted as evidence.

## Verification Request

Please verify:

1. `frontend/src/__tests__/App.test.tsx` includes an explicit empty-search test:
   - render App shell;
   - press Enter in the topbar search input without a value;
   - assert location becomes `/logs`.
2. `docs/ops/evidence/COMPREHENSIVE-TEST-REPORT-20260705.md` exists and the old report path under `docs/testing/archive/` is no longer used for the report.
3. `docs/issues/OPEN-ISSUES.md` tracks the comprehensive report risks without reusing existing issue IDs:
   - dependency vulnerability reproduction as `SEC-004`;
   - runtime exposure policy/hardening as `SEC-005`.
4. Status docs reflect the correct lifecycle:
   - PRD3 Batch 3 is Canary/Sentinel/Atlas complete;
   - this empty-search/evidence-routing follow-up is fixed awaiting Canary;
   - security risks are open, not silently treated as accepted.
5. Run these commands:

```bash
npm --prefix frontend test -- --run App
npm --prefix frontend test -- --run
npm --prefix frontend run build
go test ./... -count=1
git diff --check
```

Optional reproduction commands for the registered security issues:

```bash
npm --prefix frontend audit --audit-level=moderate --json
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

The optional audit commands are expected to return non-zero until `SEC-004` is fixed; that should not fail this follow-up verification.

## Expected Result

- If all required checks pass, update `docs/agent-workflows/VERIFY-RESULT.md` with this follow-up PASS.
- If any required check fails, list the exact file and line-level issue.

## Verification Pointers

- `frontend/src/__tests__/App.test.tsx`
- `docs/ops/evidence/COMPREHENSIVE-TEST-REPORT-20260705.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
- `docs/DOC-INVENTORY.md`
- `docs/README.md`
