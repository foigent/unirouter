# PRD8 ORG-2 Enterprise Directory IAM Convergence — Local Evidence

> Work item: `UR-PRD8-ENTERPRISE-ORG-FINANCE-LOCAL`
> Batch: `ORG-2`
> Date: 2026-08-02
> Status: Local verified
> Risk: R3 product-local IAM and tenant-scoped persistence

## Scope

ORG-2 extends the source-neutral directory projection without selecting or
contacting a real HCM or IdP:

- HCM-owned person, employment, assignment and tombstone facts;
- IdP-owned login-account, authentication-state and local-account bindings;
- immutable full/delta projection snapshots with deterministic duplicate-noop
  and conflict behavior;
- durable, tenant-scoped IAM convergence evidence;
- product-local suspension of an already-bound user only when a terminal state
  is proved and the projection has no capture gap; and
- read-only convergence evidence in Organization & Members.

There is no public projection-ingestion endpoint in this batch. A future real
Connector must call the product-local store through a separately reviewed
adapter boundary.

## Main artifacts

- `migrations/000048_prd8_enterprise_directory_people_accounts.{up,down}.sql`
- `internal/orgdirectory/projection_store.go`
- `internal/orgdirectory/projection_store_integration_test.go`
- `internal/admin/enterprise_directory_handlers.go`
- `cmd/uniroute/enterprise_directory_routes.go`
- `frontend/src/pages/MemberLifecyclePage.tsx`
- `frontend/src/pages/OrganizationMembersPage.tsx`

## Verified invariants

1. HCM cannot author account fields and an IdP cannot author people or
   organization-unit fields.
2. A local account binding must remain inside the same organization and cannot
   ambiguously bind multiple external subjects.
3. A proved disabled IdP account or terminated HCM employment may disable the
   existing local user, increment `auth_epoch`, revoke local admin sessions and
   disable applicable personal API keys.
4. Convergence never creates a local user, grants a role, adds project
   membership, restores a prior privilege or treats API-key ownership as proof
   of the initiating person.
5. Reactivation records `review_required`; it never silently re-enables the
   disabled local user or restores revoked access.
6. Full-snapshot omission is distinct from an explicit tombstone. A terminal
   state accompanied by a capture gap records review evidence and cannot mutate
   IAM.
7. The last active platform administrator is protected by an explicit
   transaction lock and fail-closed conflict evidence.
8. Duplicate bytes/revision replay is a no-op; changed bytes under the same
   revision, stale predecessor revisions, cross-tenant bindings and authority
   conflicts are rejected.

## Commands and results

- `go test ./... -count=1`: PASS, including ORG-2 unit and route coverage.
- `REQUIRE_TEST_DATABASE=1 go test -tags=integration ./internal/orgdirectory
  ./internal/enterprisefinance ./internal/admin ./internal/migrate -run
  'Test(ProjectionStore|PostgresStore|EnterpriseDirectory|EnterpriseFinance|EnterpriseRate|EnterpriseAllocation)'
  -count=1`: PASS for all four packages.
- `go test -race ./internal/orgdirectory ./internal/enterprisefinance -count=1`:
  PASS.
- `go vet ./...`: PASS.
- Frontend full regression: 49/49 files and 345/345 tests PASS.

The integration run used the existing local PostgreSQL test service and test
database helpers. It did not contact an external directory or mutate a target
or production database.

## Boundary and non-claims

- No Feishu, DingTalk, Entra/SCIM, AD or HCM target was selected.
- No directory credential, webhook, polling loop, external traffic or
  deployment was created.
- Product-local IAM remains the request-path authority; the external directory
  is never called synchronously during model admission.
- No request, routing, pricing, settlement, Outbox, Feed, Source hash, Cursor or
  Checkpoint behavior changed.
- This is Local evidence, not CI, Target, Production or external-integration
  evidence.
