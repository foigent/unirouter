# FIX-SIGNAL.md — PRD3 Batch 1 Verification Request

**日期：** 2026-07-05
**状态：** Fixed awaiting Canary

PRD2 Batch 28 passed Canary 6/6 in `docs/agent-workflows/VERIFY-RESULT.md`; User reported Canary/Sentinel/Atlas completed. PRD2 remains frozen for new feature work.

PRD3 Planning Baseline passed Canary 6/6 in `docs/agent-workflows/VERIFY-RESULT.md`; User reported Sentinel completed on 2026-07-05. Codex has completed PRD3 Batch 1 for role/navigation/default landing and User Management role workflow alignment.

## Boundaries

- Batch 18 real upstream provider evidence remains credential-gated later work.
- P2-F10 License remains E2E accepted, not Production verified.
- PRD3 is cleanup/optimization of existing functionality only.
- Runtime plugin platform, plugin marketplace, enterprise IaC/deployment expansion, project budget hard-block, runtime cache, maintenance scheduling/SLA/SLO, and new persistent roles requiring new authority surfaces are PRD4/later items unless User explicitly reopens scope.
- Batch 1 changes frontend role semantics only; backend authorization, database, gateway, billing, and route wiring are unchanged.

## Verification Request

Please verify:

1. `frontend/src/navigation.tsx` aligns frontend role navigation with backend route groups:
   - `org_admin` cannot navigate to `/channels`, `/channel-keys`, `/test`, `/wallet`, `/reconciliation`, `/grants`, or `/license`;
   - `org_admin` can still open org/admin-supported pages such as `/keys`, `/logs`, `/usage-analytics`, `/notifications`, `/users`, `/audit-logs`, `/status`, `/settings`, `/model-governance`, `/model-prices`, and `/org-model-prices`;
   - `org_viewer` and unknown roles default to `/access`, not `/docs`.
2. `frontend/src/pages/AccessStatePage.tsx` exists and is routed at `/access` in `frontend/src/main.tsx`.
3. `frontend/src/App.tsx` only shows the notification topbar button when `/notifications` is allowed for the current role.
4. `frontend/src/pages/UsersPage.tsx` exposes canonical non-legacy roles from `USER_MANAGEMENT_ROLE_OPTIONS` and explains that project roles still require project membership.
5. Evidence and status docs are updated:
   - `docs/ops/evidence/PRD3-B01-role-navigation-local-20260705.md`
   - `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
   - `docs/issues/OPEN-ISSUES.md`
   - `docs/ops/CURRENT-STATUS.md`
6. Run these commands:

```bash
npm --prefix frontend test -- --run navigation LoginPage
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

## Expected Result

- If all checks pass, update `docs/agent-workflows/VERIFY-RESULT.md` with PRD3 Batch 1 PASS.
- If any check fails, list the exact file and line-level issue.

## Verification Pointers

- `docs/agent-workflows/VERIFY-RESULT.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/evidence/PRD3-B01-role-navigation-local-20260705.md`
