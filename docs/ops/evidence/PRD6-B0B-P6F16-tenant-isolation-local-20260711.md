# PRD6 Batch 0B P6-F16 Tenant Isolation Local Evidence

> Date: 2026-07-11
> Baseline: `main@62f161e2866a3a4c699abb9a647feef48bded1b2`
> Branch: `codex/prd6-b0b-tenant-isolation`
> Evidence level: local verified; PR and required CI pending
> Scope: key, user, project, and project-owned model-set assignment/runtime
> boundary only

## 1. Patch Contract

### Security invariant

An authenticated non-platform actor may list, read, create, update, disable, or
delete only objects owned by the organization in the signed admin JWT. A
`platform_admin` or normalized legacy `admin` is the only explicit cross-tenant
exception in this batch.

Project-scoped authorization additionally requires the actor's database user,
membership row, project, and JWT organization to agree. A project may reference
only an active model set from the same organization, and malformed historical
cross-organization assignments must not influence live model admission.

### Attacker preconditions

- a valid `org_admin`, `project_admin`, developer, viewer, or equivalent signed
  admin token;
- a foreign global object ID, foreign `org_id`, malformed membership/assignment,
  or a stale organization claim;
- no database, credential, target, or deployment access is assumed.

### Expected denial and legitimate behavior

- explicit foreign-object key/user/project read/create/update/disable/delete
  returns `403` before mutation; unqualified lists return `200` with actor-org
  rows only;
- assigning a foreign or nonexistent model set returns `400` and preserves the
  previous assignment atomically;
- non-platform tokens with `org_id = 0` fail closed;
- same-tenant operations continue to succeed;
- platform administrators retain the documented cross-tenant exception;
- denied key operations create no audit row, notification event, or data change.

### Explicit non-scope

This patch does not claim global administrator-surface isolation. Finance,
notification, pricing, deployment settings, global sensitive words, and
organization-level model-governance endpoints have separately confirmed gaps in
`PRD6-B0B-admin-tenant-surface-audit-20260711.md`. PostgreSQL RLS/composite
foreign keys and removal of direct-handler test compatibility are separate
defense-in-depth work.

## 2. Regression Before Fix

The initial P6-F16 regression suite was run before the handler repair:

```text
go test ./internal/admin -run '^TestP6F16' -count=1
```

It failed eight expected paths: missing API-key and user organization predicates,
foreign project creation, organization-admin platform-role creation, foreign key
update, foreign user deletion, foreign project read, and project membership not
bound to actor organization.

Independent review then added additional bypass regressions before their
corresponding fixes. On the partially remediated worktree, the same command
failed these additional cases:

```text
project model-set replacement accepted a foreign set and returned 200
malformed cross-org membership exposed the foreign project
runtime model governance consumed a cross-org project/model-set assignment
project membership SQL did not bind the database user organization
user update SQL did not atomically exclude platform roles
```

The independent model-governance regression also failed before the execution
query was repaired:

```text
go test ./internal/modelgov -run '^TestCheckAccess_ProjectModelSetsBindProjectAndSetOrganization$' -count=1
```

The old query selected `project_model_sets` by `project_id` alone and did not
join the project and model-set organizations.

A later atomicity regression injects a failure after delete and one successful
insert, then verifies rollback without commit. It validates the transaction
behavior; it is not presented as a separately observed pre-fix failure.

## 3. Implemented Closure

- list/read/write key operations bind the actor organization at authorization
  and final SQL mutation sinks;
- user lists and mutations bind actor/target organization; organization admins
  cannot create, promote, modify, or delete platform roles;
- the final user update predicate excludes `admin` and `platform_admin`, closing
  the read/update role race;
- user deletion locks and resolves the target organization before disabling
  personal keys and deleting the user;
- project list/create/read/update authorization binds actor organization;
- project membership joins `users` and `projects` and requires both to match the
  signed actor organization;
- project model-set replacement validates all IDs before change, locks the
  project, deletes/inserts in one transaction, and rejects foreign sets without
  partial mutation;
- live model governance joins project and model-set organization so malformed
  cross-tenant assignments are ignored.

## 4. Authenticated Integration Matrix

`internal/admin/tenant_isolation_integration_test.go` creates a temporary
PostgreSQL database, applies every present repository migration through latest
`000019`, seeds two
tenants, generates signed JWTs, and calls handlers through `AdminAuth` plus the
same role gates used by the production routes.

Covered behavior:

- key list/read/create/update/disable: same tenant, foreign tenant, platform
  exception, and no audit/notification side effect on denial;
- user list/create/update/delete: same tenant, foreign tenant, platform-role
  escalation denial, protected platform target, and platform exception;
- project list/create/read/update plus model-set list/replace: same tenant,
  foreign tenant, and platform exception;
- normalized legacy `admin` exception and non-platform `org_id = 0` fail closed;
- malformed cross-organization `project_members` and `project_model_sets` rows;
- foreign model-set rejection without deleting the existing valid assignment.

## 5. Local Verification

Verified so far on this branch:

```text
go test ./internal/admin -run '^TestP6F16' -count=1 -v                 PASS
go test ./internal/admin -count=1                                     PASS
go test ./internal/modelgov -count=1                                  PASS
go test -race ./... -count=1                                          PASS
go vet ./...                                                           PASS
npm --prefix frontend run type-check                                  PASS
npm --prefix frontend test -- --run                                   PASS, 25 files / 125 tests
npm --prefix frontend run build                                       PASS
git diff --check                                                       PASS
```

The P6-F16 verbose run showed the migrated PostgreSQL test and all twelve
authenticated subtests as `PASS`; it did not skip. The first frontend test run
was intentionally concurrent with race/vet/build and one pre-existing Data
Exchange UI test exceeded its five-second timeout. An immediate standalone rerun
passed all 25 files / 125 tests. Existing React Router, Semi UI,
`findDOMNode`, macOS `LC_DYSYMTAB`, and large-chunk warnings remain unchanged.

Required PR CI remains a promotion gate and will be recorded only for the exact
pushed head. CI logs must show the migrated P6-F16 test as `PASS`, not a database
permission/connectivity `SKIP`.

The legacy `-tags=integration` admin suite uses a shared non-migrated database
schema and currently emits missing-table/column failures independent of this
acceptance harness. P6-F16 therefore uses the untagged isolated migrated test
that required CI actually executes. This does not promote the legacy harness to
passing evidence.

## 6. Residual Risk And Evidence Boundary

- No deployment, target request, credential change, paid provider call, data
  export, or external-project mutation occurred.
- Local verification is not CI, target, or production verification.
- Direct handler calls with no authenticated role retain legacy unit-test
  compatibility; production routes remain protected by `AdminAuth` and
  `RequireRoles`. A route-parity/fail-closed hardening item remains separate.
- Database RLS and composite organization foreign keys are defense in depth,
  not claimed by this source patch.
- P6-F32 through P6-F36 remain open inputs and prevent any claim that UniRouter's
  full administrator surface is tenant-isolated.
