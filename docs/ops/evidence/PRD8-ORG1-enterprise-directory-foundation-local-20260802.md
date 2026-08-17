# PRD8 ORG-1 Enterprise Directory Foundation — Local Evidence

> Work item: `UR-PRD8-ENTERPRISE-ORG-FINANCE-LOCAL`
> Batch: `ORG-1`
> Date: 2026-08-02
> Status: Local verified
> Risk: R3 database and tenant-scoped admin read surface

## Scope

ORG-1 adds a source-neutral local enterprise-directory projection foundation:

- metadata-only directory-source registration with no credential columns;
- independent connection, freshness, reconciliation and coverage health;
- immutable full/delta snapshot identities and exact predecessor revision;
- materialized, effective-dated organization trees with explicit tombstones;
- duplicate/no-op replay and fail-closed stale/conflicting revision handling;
- strict parent existence and cycle checks;
- organization-scoped read APIs for source health and local tree projection;
- a UI read surface that shows real local projection state or an honest
  unconfigured boundary, never synthetic people or departments.

The implementation performs no HCM/IdP call, stores no credential and is not on
the model-request path.

## Main artifacts

- `migrations/000047_prd8_enterprise_directory_foundation.{up,down}.sql`
- `internal/orgdirectory/contract.go`
- `internal/orgdirectory/identity.go`
- `internal/orgdirectory/store.go`
- `internal/admin/enterprise_directory_handlers.go`
- `cmd/uniroute/enterprise_directory_routes.go`
- `frontend/src/pages/OrganizationMembersPage.tsx`

## Verified invariants

1. `source_ref` is tenant-scoped and authority cannot change from HCM to IdP.
2. Delta snapshots require an exact previous revision; identical replay is a
   no-op and same-revision changed bytes are a conflict.
3. Conflicts remain visible in reconciliation health and prevent new privilege
   grant through the DOMAIN-0 fail-closed predicate.
4. Organization units are materialized under one immutable snapshot; missing
   parents and hierarchy cycles are rejected.
5. Platform administrators must explicitly select an organization; organization
   administrators cannot read another organization through `org_id`.
6. Migration inspection found zero credential/secret/password/token/key
   columns across all `enterprise_directory_%` tables.
7. Down migration refuses to destroy directory data or cross a later migration.

## Commands and results

- `go test ./internal/orgdirectory ./cmd/uniroute` with isolated `GOCACHE`:
  PASS.
- `go test ./internal/admin` with isolated `GOCACHE` and local loopback
  permission: PASS.
- `go test -tags=integration ./internal/orgdirectory -run TestPostgresStore
  -count=1 -v` with required local PostgreSQL: PASS, 2/2 tests.
- `go test -tags=integration ./internal/migrate -run
  TestEnterpriseDirectoryFoundationMigrationAndRollbackGuard -count=1 -v`
  with required local PostgreSQL: PASS, 1/1 test.
- `go test -tags=integration ./internal/admin -run
  TestEnterpriseDirectoryHandlersFailClosedAcrossOrganizationsIntegration -count=1 -v`
  with required local PostgreSQL: PASS, 1/1 test.
- `go vet ./internal/orgdirectory ./internal/admin ./cmd/uniroute`: PASS.
- `npm test -- src/pages/__tests__/OrganizationMembersPage.test.tsx`: PASS,
  5/5 tests.
- `npm run type-check`: PASS.

The first sandboxed test attempts could not write the user Go cache or open a
loopback socket. They did not reach the relevant code. Re-runs used a bounded
`/private/tmp` Go cache and the existing local PostgreSQL test service; those
are the results recorded above.

## Boundary and non-claims

- No real directory, SCIM, Feishu, DingTalk, Entra, AD or HCM target selected.
- No credential, external traffic, deployment or target database mutation.
- No local IAM mutation yet; that belongs to ORG-2.
- No request, routing, billing, Outbox, Feed, Source hash or Cursor change.
- This is Local evidence, not CI, Target, Production or external integration.
