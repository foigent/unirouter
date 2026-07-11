# PRD6 Batch 0B P6-F37 Audit-Log Tenant Isolation — Local Evidence

> Date: 2026-07-11
> Base: `7173c5967a42c112fd0b219938a0901c1a0b9633`
> Evidence level: CI verified by PR #143; deployment is not claimed

## Confirmed Pre-Fix Finding

`GET /api/v1/audit-logs` allowed `platform_admin`, `org_admin` and
`billing_admin`, but the handler only applied caller-supplied filters. An
organization-scoped role could omit `org_id` and read every organization's
audit rows, or explicitly request another positive organization ID. Audit
details may contain configuration snapshots and integration destinations, so
this was a direct cross-tenant confidentiality defect rather than a naming-only
UI concern.

This finding is independent of P6-F33. PR #142 secured notification/Webhook
reads and writes but did not claim that every administrator endpoint was tenant
safe.

## Repository Change

- an authenticated non-platform audit reader is always constrained to the
  organization carried by its signed JWT;
- an explicit foreign `org_id` is rejected with `403` and cannot replace the
  actor constraint;
- an explicit `project_id` must belong to the actor organization; a foreign or
  nonexistent project has the same `403` result;
- a non-platform token with organization `0` fails closed;
- `platform_admin` retains explicit cross-tenant query authority;
- direct unauthenticated handler unit fixtures retain their legacy test seam,
  while the production route remains behind `AdminAuth` and role middleware.

No target environment, credential, provider request, external message, data
export or X-Insight contract changed.

## Red/Green Regression

The migrated PostgreSQL test
`TestP6F37AuditLogTenantIsolationIntegration` seeds two organizations with
distinct audit rows, including a victim-only destination marker. It verifies:

1. organization and billing administrators receive only their JWT
   organization's row when no organization filter is supplied;
2. neither response contains the other tenant's marker;
3. explicit foreign organization and project filters return `403`;
4. same-organization org/project/actor filters continue to work;
5. a platform administrator may explicitly inspect the second organization;
6. a missing non-platform organization identity returns `403`.

Before the production predicate was added, the red run stopped on the first
unfiltered assertion after returning both tenant rows. Source review separately
confirms that an explicit positive foreign organization filter was accepted and
queried with `200`; the green regression now exercises both paths.

Focused command:

```text
go test ./internal/admin -run 'TestP6F37AuditLogTenantIsolationIntegration|TestListAuditLogs' -count=1 -v
```

Result: PASS, including all five authenticated integration subtests and the
existing handler unit tests.

## Remaining Boundaries

This patch does not yet redesign the Audit Logs information architecture. Event
categories, actor/object name snapshots, friendly labels, date-range controls,
request/operation/attempt correlation and sensitive-value redaction are
separate P1 operability work recorded by the product/architecture UX audit.

It also does not remediate other confirmed administrator surfaces. Wallet,
grant, transaction, reconciliation and unfiltered usage-log tenant boundaries
must remain open P0 work until their own two-tenant, no-side-effect evidence is
green. Consequently this evidence must not be used to claim that the entire
administrator plane is production-safe.

Full local verification passed on the final code behavior:

```text
go test -race ./... -count=1
go vet ./...
npm --prefix frontend run type-check
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

The backend race/vet checks passed. After P6-F38 addressed the measured
frontend interaction-timeout instability without changing assertions, all 25
files / 126 tests, type-check and the production build passed. Required PR
run `29133309062` then passed Backend Tests, Frontend Build and Docker Build for
implementation head `132b019`. Independent review reproduced the pre-fix leak
on `7173c59` and found no P6-F37 bypass candidate. This is `CI verified`, not
Target or Production verified.
