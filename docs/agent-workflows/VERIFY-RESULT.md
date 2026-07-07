# VERIFY-RESULT.md - PRD4 Batch 1B Usage And Cost Attribution

**Date:** 2026-07-07
**Signal:** `docs/agent-workflows/FIX-SIGNAL.md`
**Issue:** `PRD4-USAGE-001`
**Matrix Row:** `P4-F02`
**Conclusion: ✅ PASS (10/10)**

## Verification Summary

| # | Acceptance Criteria | Result | Evidence |
|---|---------------------|--------|----------|
| 1 | Migration adds owner/type columns and can roll back | ✅ PASS | `migrations/000016_prd4_key_owner_usage.up.sql` adds `owner_user_id` + `key_type` to both `api_keys` and `call_logs` with CHECK constraints and indexes; `.down.sql` drops all in reverse order |
| 2 | Gateway request logging writes immutable owner/type snapshots into `call_logs` | ✅ PASS | `internal/gateway/relay.go:848-858` reads `apiKey.OwnerUserID` and `apiKey.KeyType` at request time and inserts them into `call_logs` — immutable snapshot, not mutable key state |
| 3 | API key create/list/get/update expose owner/type and enforce conservative owner rules | ✅ PASS | `internal/admin/key_handlers.go` handles `owner_user_id`/`key_type` in create (L179-209), list (L51-86), get (L471-495), update (L226-393); personal keys require `owner_user_id`; shared/service keys do not carry user owner |
| 4 | Deleting a user disables their personal API keys before hard-delete | ✅ PASS | `internal/admin/user_handlers.go:162-163` — `UPDATE api_keys SET status='disabled' WHERE owner_user_id=$1 AND key_type='personal'` runs before `DELETE FROM users` in the same transaction |
| 5 | `scope=my` lets project-scoped user query own usage without project filter | ✅ PASS | `internal/admin/prd2_product_handlers.go:131-139` — `scope=my` adds `owner_user_id = $currentUserID` condition; L166-168 allows project-scoped roles to skip `project_id` requirement when `myScope` is true |
| 6 | Project-scoped users cannot query another user's owner usage | ✅ PASS | `prd2_product_handlers.go:184-188` — if `myScope && ownerUserID != currentUserID` → error "owner_user_id conflicts with scope"; if `IsProjectScopedRole && ownerUserID != currentUserID` → "Forbidden" |
| 7 | Usage analytics supports owner/key-type grouping and filters without SQL injection | ✅ PASS | `prd2_product_handlers.go:35-37` defines `group_by=owner` → `COALESCE(owner_user_id, 0)::text` and `group_by=key_type` → `COALESCE(NULLIF(key_type,''),'unknown')`; all filters use parameterized queries (`$%d` placeholders) |
| 8 | Frontend serializes and renders new filters without breaking existing tests | ✅ PASS | `frontend/src/api/client.ts:151-161` serializes `owner_user_id`, `key_type`, `scope`; `UsageAnalyticsPage.tsx:58-73` manages state and passes to API; all 22 frontend test files pass (108 tests) |
| 9 | Full Go and frontend tests pass; frontend build passes | ✅ PASS | `go test ./...` — all 22 packages PASS; `npm --prefix frontend test -- --run` — 108/108 PASS; `npm --prefix frontend run build` — built in 9.54s |
| 10 | Docs consistently show Batch 1B as awaiting Canary, not accepted | ✅ PASS | All 7 doc files show "Fixed awaiting Canary" status; OPEN-ISSUES.md, CURRENT-STATUS.md, PRD4-ACCEPTANCE-MATRIX.md, PROJECT-HANDOFF.md, DOC-INVENTORY.md, README.md, FIX-SIGNAL.md all consistent |

## Detailed Verification

### Migration (AC1)
- Up migration: adds `owner_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL` and `key_type VARCHAR(20) NOT NULL DEFAULT 'shared'` to `api_keys`; adds `owner_user_id BIGINT` and `key_type VARCHAR(20) NOT NULL DEFAULT 'unknown'` to `call_logs`; backfills `call_logs.key_type` from `api_keys`; creates 4 indexes
- Down migration: drops indexes, constraints, columns in reverse order — properly paired
- CHECK constraints: `api_keys.key_type IN ('shared','personal','service')`, `call_logs.key_type IN ('unknown','shared','personal','service')`

### Gateway Snapshot (AC2)
- `relay.go:848-852`: reads `apiKey.OwnerUserID` and `apiKey.KeyType` from the authenticated API key struct
- `relay.go:858`: INSERT INTO call_logs includes `owner_user_id` and `key_type` columns — snapshot at request time

### API Key CRUD (AC3)
- Create: validates personal keys require `owner_user_id`, validates owner by org/project membership
- List: joins `users` table to return `owner_username` alongside `owner_user_id`/`key_type`
- Get: same join pattern as list
- Update: validates `owner_user_id`/`key_type` changes, checks org/project membership for personal keys

### User Deletion (AC4)
- `user_handlers.go:160-163`: within a transaction, first disables personal keys, then hard-deletes user
- The `ON DELETE SET NULL` foreign key on `api_keys.owner_user_id` provides additional safety for any edge cases

### Scope=my (AC5)
- When `scope=my`, the handler adds `owner_user_id = $currentUserID` to the query conditions
- Project-scoped users can use `scope=my` without specifying `project_id` (L166-168: `if !myScope` check)

### Cross-User Isolation (AC6)
- Two independent checks: `myScope && ownerUserID != currentUserID` and `IsProjectScopedRole && ownerUserID != currentUserID`
- Both return appropriate errors before the query executes

### SQL Injection Safety (AC7)
- All filters use parameterized `$%d` placeholders via `fmt.Sprintf`
- `group_by` values are mapped through a switch statement (L28-37), never interpolated into SQL directly
- `key_type` validated against `validAPIKeyType()` function

### Frontend (AC8)
- `client.ts:151-161`: adds `owner_user_id`, `key_type`, `scope` to query params
- `UsageAnalyticsPage.tsx:58-73`: state management for all new filters
- `UsageAnalyticsPage.tsx:95`: triggers reload on filter changes
- All existing analytics tests pass unchanged

### Tests (AC9)
- Go: 22 packages, 0 failures
- Frontend: 22 test files, 108 tests, 0 failures
- Build: success (9.54s)
- `git diff --check`: clean (no whitespace errors)

### Docs (AC10)
- Verified in 7 doc files: all show "Fixed awaiting Canary" for Batch 1B

## New Issues Found

None.

## Verdict

**PASS.** All 10 acceptance criteria verified with code evidence. No new issues found. Batch 1B is ready for Sentinel to merge.
