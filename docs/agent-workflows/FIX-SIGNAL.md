# FIX-SIGNAL.md - PRD4 Batch 1B Usage And Cost Attribution

**Date:** 2026-07-07
**Status:** Fixed awaiting Canary
**Issue:** `PRD4-USAGE-001`
**Matrix Row:** `P4-F02`

## Verification Request

Please verify PRD4 Batch 1B: Usage And Cost Attribution foundation.

This batch implements API key owner/type attribution and exposes it through
usage analytics so each authenticated user can request their own model-level
usage/cost view while authorized roles retain broader filtered views.

## Scope To Verify

Backend/data:

- Adds migration `000016_prd4_key_owner_usage` with `api_keys.owner_user_id`,
  `api_keys.key_type`, `call_logs.owner_user_id`, `call_logs.key_type`, checks,
  and query indexes.
- Extends gateway API-key resolution to load owner/type into the request API key.
- Records owner/type snapshots into `call_logs` at request time instead of
  relying on mutable current key ownership.
- Extends API key create/list/get/update with `owner_user_id` and `key_type`.
- Defaults admin-created keys to `shared`; project-scoped user-created keys
  default to `personal` owned by the current user.
- Validates personal owners by org and project membership; shared/service keys
  do not carry a user owner.
- Disables a deleted user's personal API keys before hard-deleting the user, so
  personal keys do not remain active without an owner.
- Extends `/api/v1/usage/analytics` with:
  - `scope=my`
  - `owner_user_id`
  - `key_type`
  - `group_by=owner`
  - `group_by=key_type`

Frontend:

- Adds usage analytics filter controls for `scope`, owner user ID, and key type.
- Adds grouping options for Key Owner and key type.
- Updates API client query serialization for the new filters.

Docs/status:

- Marks PRD4 Batch 1B as fixed awaiting Canary, not accepted.
- Keeps PRD2 frozen and PRD3 complete.

## Non-Scope

Do not expect this batch to implement:

- Full Trace/Flow/Event request path UI.
- OpenTelemetry or external observability ingestion.
- Runtime Status Center CPU/memory/network panels.
- RPM/TPM scheduler changes, hard quota enforcement, or Scheduler V2.
- Logto/IAM rewrite or real SSO target-environment evidence.
- Real upstream provider target-environment evidence.
- Prompt/response plaintext storage.
- PRD5 runtime plugin marketplace.

## Required Verification Commands

```bash
rg -n "owner_user_id|key_type|scope=my|group_by=owner|group_by=key_type" migrations internal frontend/src
rg -n "PRD4-USAGE-001|P4-F02|Batch 1B|Usage And Cost Attribution" docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md
go test ./internal/admin ./internal/middleware ./internal/gateway
go test ./...
npm --prefix frontend test -- --run src/api/__tests__/client.test.ts src/pages/__tests__/UsageAnalyticsPage.test.tsx
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

## Acceptance Criteria

1. Migration adds owner/type columns and can roll back with the paired down migration.
2. Gateway request logging writes immutable owner/type snapshots into `call_logs`.
3. API key create/list/get/update expose owner/type and enforce conservative owner rules.
4. Deleting a user disables their personal API keys before the user row is hard-deleted.
5. `scope=my` lets a project-scoped user query their own usage without requiring a project filter.
6. Project-scoped users cannot query another user's owner usage.
7. Usage analytics supports owner/key-type grouping and filters without SQL injection.
8. Frontend serializes and renders the new filters without breaking existing analytics tests.
9. Full Go and frontend tests pass; frontend build passes.
10. Docs consistently show Batch 1B as awaiting Canary, not accepted.

## Notes For Canary

Historical `call_logs` may have `owner_user_id=NULL`; this is intentional. The
batch avoids fabricating historical owner attribution. Existing historical logs
can still be analyzed by project/key/model, while new gateway requests carry the
owner/type snapshot from the authenticated API key.
