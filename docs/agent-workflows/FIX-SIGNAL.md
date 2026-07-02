# FIX-SIGNAL.md — PRD2 Batch 20 Audit Completion And Hardening

**修复者：** Codex
**日期：** 2026-07-02
**修复轮次：** PRD2 Batch 20

---

## 前置条件

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| Batch 18 | Open / deferred | User explicitly deferred real-upstream provider acceptance |
| Production webhook evidence | Not claimed | This round does not change Batch 19 production webhook boundary |
| Unregistered models | Still callable | User direction was to preserve PRD1 compatibility and add visibility, not hard-block |
| Frontend warnings | Existing non-blocking warnings may appear | React Router future flag, Semi `findDOMNode`, and Vite large chunk warnings are pre-existing |

---

## Fix #1: Audit Logs frontend filters and API client parameters

**Files:**

- `frontend/src/api/client.ts`
- `frontend/src/pages/AuditLogsPage.tsx`
- `frontend/src/pages/__tests__/AuditLogsPage.test.tsx`
- `frontend/src/api/__tests__/client.test.ts`

**Original issue:**

P2-F07 had backend filtering support, but the Audit Logs UI/client did not expose filters for org, project, actor, action, resource, time range, or source.

**Current code:**

- `api.getAuditLogs` accepts filter parameters and serializes them to `/api/v1/audit-logs`.
- Audit Logs page exposes filters for `org_id`, `project_id`, `actor_id`, `action`, `resource`, `resource_id`, `source`, `start_time`, and `end_time`.
- Audit table renders org/project/source columns.

**Verification method:**

```bash
npm --prefix frontend test -- --run AuditLogsPage client
npm --prefix frontend test -- --run
npm --prefix frontend run build
```

Expected:

- `AuditLogsPage.test.tsx` verifies filter controls and default API call.
- `client.test.ts` verifies the audit filter query string.
- Full frontend tests pass; build passes with only the existing large chunk warning.

---

## Fix #2: Channel create/update/delete audit before/after coverage

**Files:**

- `internal/admin/channel_handlers.go`
- `internal/admin/channel_audit_test.go`

**Original issue:**

`channel.create/update/delete` audit rows were thin. They did not consistently expose structured before/after evidence.

**Current code:**

- `channel.create` records an `after_state` snapshot.
- `channel.update` records changed fields plus `before_state` and `after_state` snapshots when available.
- `channel.delete` records a `before_state` snapshot.
- Snapshots include channel metadata but not channel provider keys.

**Verification method:**

```bash
go test ./internal/admin -run 'Test(CreateChannel_AuditIncludesAfterState|UpdateChannel_AuditIncludesBeforeAfterState|DeleteChannel_AuditIncludesBeforeState)' -count=1
```

Expected:

- Create audit includes `after_state.name` and normalized adapter type.
- Update audit includes before and after names.
- Delete audit includes before state and no `sk-` key material.

---

## Fix #3: `channel.test` audit event without secret leakage

**Files:**

- `internal/admin/test_handlers.go`
- `internal/admin/channel_audit_test.go`

**Original issue:**

Testing a channel did not produce an audit event, and adding one must avoid leaking provider keys, prompt text, response bodies, or generated curl examples.

**Current code:**

- `TestChannel` records `channel.test` for no-key, decrypt, adapter, transport, success, and upstream-failure outcomes.
- Audit `after_state` includes channel id, model, adapter type, success/status/status_code/latency/tokens/error summary where applicable.
- Audit excludes provider key, prompt, upstream response body, and curl example.

**Verification method:**

```bash
go test ./internal/admin -run 'TestTestChannel_WritesAuditWithoutSecret|TestTestChannel_Upstream401IsFailureAndUsesSelectedModel|TestTestChannel_AnthropicNativeUsesMessagesEndpoint' -count=1
```

Expected:

- `channel.test` audit row is written for an upstream 401.
- Audit JSON includes failure metadata but not `bad-upstream-key`, `sk-`, or `curl`.
- Existing channel test behavior still passes for OpenAI-compatible and Anthropic-native adapters.

---

## Fix #4: Migration hardening for historical audit project backfill and rollback fidelity

**Files:**

- `migrations/000009_prd2_project_foundation.up.sql`
- `migrations/000012_prd2_token_metering_v2.down.sql`
- `internal/migrate/prd2_project_migration_integration_test.go`

**Original issue:**

- 000009 backfilled project ids for API keys, call logs, and transactions, but not historical audit rows.
- 000012 normalized `usage_source` values on up migration but did not reverse those values on down migration.

**Current code:**

- 000009 maps historical org-scoped audit rows to each org's default project when `audit_logs.project_id` is null.
- 000012 down maps `upstream -> api` and `counter_v2 -> stream_estimate` before dropping V2 columns.
- Integration tests verify both behaviors.

**Verification method:**

```bash
go test -tags=integration ./internal/migrate -run 'TestMigration000009BackfillsDefaultProjectForPRD1Data_Integration|TestMigration000012DownRestoresLegacyUsageSourceValues_Integration' -count=1 -v
```

Expected:

- 000009 test verifies `audit_logs.project_id` equals the default project id.
- 000012 test verifies up migration canonicalizes values and down migration restores legacy values.

---

## Fix #5: Unregistered model calls stay compatible but become auditable

**Files:**

- `cmd/uniroute/gateway_handlers.go`
- `cmd/uniroute/prd2_p0_e2e_integration_test.go`

**Original issue:**

Unregistered models remain callable by design for PRD1 compatibility, but there was no visibility signal for typo or unmanaged model calls.

**Current code:**

- When model governance returns an empty lifecycle, the gateway preserves the request path.
- The gateway writes a non-blocking `model.unregistered.call` audit row with `source=gateway_api`, request id, project id, API key id, key prefix, and model name.
- No API key hash or provider key is stored.

**Verification method:**

```bash
go test -tags=integration ./cmd/uniroute -run 'TestGatewayUnregisteredModelAllowedButAudited_Integration' -count=1 -v
```

Expected:

- Gateway request for `unregistered-chat` returns HTTP 200.
- Audit row `model.unregistered.call` is present with source `gateway_api` and project id.

---

## Fix #6: Real JWT/API audit filter E2E

**Files:**

- `cmd/uniroute/prd2_p0_e2e_integration_test.go`
- `internal/admin/audit_handlers.go`

**Original issue:**

Audit filtering needed a real JWT/API E2E path that proves filters and before/after response fields work through auth middleware, not only handler mocks.

**Current code:**

- `TestPRD2AuditFiltersJWTBeforeAfterE2E_Integration` seeds a migrated DB audit row with `before_state` and `after_state`.
- The test calls `GET /api/v1/audit-logs` through `AdminAuth` and role middleware using an org-admin JWT.
- Filters include `org_id`, `project_id`, `actor_id`, `action`, `resource`, `resource_id`, and `source`.

**Verification method:**

```bash
go test -tags=integration ./cmd/uniroute -run 'TestPRD2AuditFiltersJWTBeforeAfterE2E_Integration' -count=1 -v
```

Expected:

- Response is 200.
- Exactly one row is returned.
- Row contains project id, actor id, source, and `before`/`after` JSON.

---

## Fix #7: Status docs record Batch 20 local completion without overclaim

**Files:**

- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD2-COMPLETION-PLAN.md`
- `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`
- `docs/issues/OPEN-ISSUES.md`

**Original issue:**

Batch 20 work needed to be reflected in the active status sources after local implementation.

**Current state:**

- `CURRENT-STATUS.md` marks Batch 20 fixed locally awaiting Canary.
- `PRD2-COMPLETION-PLAN.md` records the Batch 20 local work and commands.
- `PRD2-ACCEPTANCE-MATRIX.md` keeps P2-F07 at `Implemented not accepted` until Canary verifies.
- `PRD2-MIG-001`, `PRD2-MIG-002`, `PRD2-GOV-001`, and `PRD2-AUDIT-001` are `Fixed awaiting Canary`.

**Verification method:**

```bash
rg -n "Batch 20|PRD2-MIG-001|PRD2-MIG-002|PRD2-GOV-001|PRD2-AUDIT-001|Implemented not accepted" \
  docs/ops/CURRENT-STATUS.md \
  docs/ops/PRD2-COMPLETION-PLAN.md \
  docs/ops/PRD2-ACCEPTANCE-MATRIX.md \
  docs/issues/OPEN-ISSUES.md
```

Expected:

- Batch 20 is not marked Canary passed.
- P2-F07 is not overclaimed as E2E accepted.
- Four hardening issues are fixed awaiting Canary.

---

## Global Verification Commands

```bash
cd /Users/m4/Desktop/projects/UniRouter
go test ./internal/admin -run 'Test(CreateChannel_AuditIncludesAfterState|UpdateChannel_AuditIncludesBeforeAfterState|DeleteChannel_AuditIncludesBeforeState|TestChannel_WritesAuditWithoutSecret|ListAuditLogs_Filters)' -count=1
go test ./cmd/uniroute -run Test -count=1
go test -tags=integration ./internal/migrate -run 'TestMigration000009BackfillsDefaultProjectForPRD1Data_Integration|TestMigration000012DownRestoresLegacyUsageSourceValues_Integration' -count=1 -v
go test -tags=integration ./cmd/uniroute -run 'TestPRD2AuditFiltersJWTBeforeAfterE2E_Integration|TestGatewayUnregisteredModelAllowedButAudited_Integration' -count=1 -v
go test ./...
go test -tags=integration ./cmd/uniroute ./internal/migrate -count=1
npm --prefix frontend test -- --run AuditLogsPage client
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Codex local notes:

- All listed commands passed on 2026-07-02.
- `npm --prefix frontend test -- --run` still prints existing React Router/Semi warnings.
- `npm --prefix frontend run build` still prints the existing Vite large chunk warning.

---

## Expected Canary Output Format

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

1. PASS/FAIL for Fix #1 Audit Logs frontend filters/client parameters.
2. PASS/FAIL for Fix #2 channel create/update/delete audit before/after.
3. PASS/FAIL for Fix #3 `channel.test` audit without secret leakage.
4. PASS/FAIL for Fix #4 migration hardening.
5. PASS/FAIL for Fix #5 unregistered model audit compatibility.
6. PASS/FAIL for Fix #6 real JWT/API audit filter E2E.
7. PASS/FAIL for Fix #7 status docs discipline.
8. Command result table for every listed command.
9. New findings, if any.
10. Conclusion stating whether Batch 20 can move to Sentinel.
