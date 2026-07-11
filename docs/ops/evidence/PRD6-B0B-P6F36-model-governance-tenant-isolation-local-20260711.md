# PRD6 Batch 0B P6-F36 Model-Governance Tenant Isolation — Local Evidence

> Date: 2026-07-11
> Base: `c08db96167837ade685708c2ab204861a8ecc0ca`
> Branch: `codex/prd6-p6f36-modelgov-tenant`
> Evidence level: local implementation, migrated two-tenant verification,
> independent review and PR #148 implementation-head required CI complete; final
> evidence-head CI, merge and target verification pending; no deployment or
> production verification is claimed

## Confirmed Pre-Fix Findings

The reviewed base exposed eleven organization-level model-governance methods:
three registry methods, one approved-catalog read, three alias methods and four
model-set/item methods. Their production role wrappers required an allowed
administrator or project role, but the handlers did not consistently bind the
requested organization or global record ID to the authenticated actor.

Registry, alias and model-set lists accepted a caller-controlled `org_id`
query. Their create methods accepted a body organization and otherwise fell
back to organization `1`. Registry and alias updates selected and updated only
by a globally unique row ID. Model-set item replacement deleted and inserted
only by the global set ID and was not transactional. A tenant administrator
could therefore read, create or mutate another organization's live model
governance state when an organization or record ID was known.

The approved catalog separately authorized an optional project but still chose
the catalog organization from the independent query. Its assigned-set count
and effective-set `EXISTS` query did not require the project, registry and model
set to share one organization. A malformed historical cross-organization
assignment could therefore affect catalog output even though the P6-F16 live
admission path already ignores that assignment.

These are repository-source findings on the stated base. The standard frontend
normally omitted foreign organizations, but direct API clients could reach the
paths; frontend hiding is not an authorization control. No target data,
production exploit or real provider request was used.

## Repository Change

- organization registry, alias and model-set lists now use the shared actor
  organization resolver: tenant roles can use only the positive organization in
  their signed identity, while a platform administrator may explicitly select
  another positive organization;
- registry, alias and model-set creates distinguish an omitted `org_id` from an
  explicit zero. Omitted values bind to the signed actor organization; zero,
  negative or malformed identifiers return `400`; a positive foreign tenant
  organization returns `403`; there is no organization-1/database-order
  fallback;
- global-ID registry, alias and model-set-item paths accept an optional query
  organization. A tenant's implicit foreign/missing ID returns the same `404`,
  and an explicit positive foreign query returns `403`. A platform
  administrator may select the organization explicitly or let the unique path
  ID resolve the target; the final mutation still includes both row ID and the
  resolved organization predicate;
- registry lifecycle events and registry/alias audit rows use the resolved
  target organization instead of the platform actor's organization;
- project catalog requests derive their canonical organization from the
  authorized project row. An explicit mismatch is rejected, project-scoped
  roles must supply a membership-bearing project, and organization/platform
  administrators retain organization-level catalog reads;
- catalog effective-set queries require the project, registry and model set to
  share one organization. Malformed cross-organization assignments are ignored
  consistently with the already-hardened P6-F16 live admission path;
- model-set parent creation, initial items and target-organization audit are
  committed in one transaction. Item replacement locks an authorized
  organization-scoped parent, performs a scoped delete/insert, writes
  before/after item-count audit evidence and commits atomically;
- all eleven methods now share one production route registrar outside
  `main.go`; tests exercise the actual organization/project-read guards and
  assert that main wires those guards to the correct route families;
- OpenAPI RBAC/scope descriptions, frontend API documentation and client URL
  tests describe the same query/body/path authority contract. The client keeps
  existing tenant calls unchanged and supports an optional platform
  organization on the three global-ID mutations; no new product UI is added.

No migration, credential, target deployment, provider request, external
message, exporter, retained-data scope or X-Insight contract changed.

## Migrated Signed-JWT and PostgreSQL Evidence

`TestP6F36ModelGovernanceTenantIsolationIntegration` creates an empty
PostgreSQL database, applies all migrations and invokes the eleven methods
through signed JWT authentication and the production role middleware. It
proves:

1. registry, alias and model-set lists default to the JWT organization, reject
   a positive foreign tenant query, reject an explicit zero and fail an
   organization-zero tenant identity closed; a platform administrator can
   explicitly read the other organization;
2. registry, alias and model-set creates derive an omitted organization from
   the JWT, reject foreign and explicit-zero bodies without a row/audit side
   effect, and attribute platform-created foreign rows and audits to the target
   organization;
3. project catalog uses the project's canonical organization, rejects a
   project/query mismatch, denies a foreign project and a project role without
   `project_id`, and ignores a deliberately inserted Project-A-to-Org-B set
   assignment;
4. the unchanged runtime `modelgov.CheckAccess` resolves an own-organization
   alias and denies a foreign model even when the malformed cross-organization
   assignment exists;
5. tenant registry and alias updates succeed only for own rows, return
   non-enumerating `404` for foreign IDs, return `403` for explicit foreign
   organization queries and leave denied rows, audits and lifecycle events
   unchanged;
6. platform registry/alias mutations work both with an explicit target
   organization and with the global ID alone, and their audit/event rows use
   the target organization;
7. model-set item reads and replacements enforce the same own/foreign/platform
   matrix, preserve denied foreign policies and write target-organization audit
   evidence;
8. a PostgreSQL trigger that rejects selected audit inserts proves failed
   model-set creation leaves neither parent nor orphan items, and failed item
   replacement leaves the complete prior policy unchanged.

The focused matrix passes repeatedly and under the race detector. Existing
unit tests also require authenticated platform context for direct-handler
success paths, preventing the old unauthenticated organization fallback from
remaining as test-only behavior.

## Local Verification

The current local diff passed:

```text
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
npm --prefix frontend test -- --reporter=dot
npm --prefix frontend run type-check
npm --prefix frontend run build
ruby -e 'require "yaml"; YAML.load_file("docs/tech-ref/05-openapi-spec.yaml"); puts "openapi yaml: ok"'
git diff --check
```

Results: every backend package, the race detector and vet pass; the frontend
suite passes 27 files / 136 tests; type-check and production build pass. The
build retains the existing large-chunk warning, and macOS race linking retains
the existing non-failing `LC_DYSYMTAB` warning. Independent review found no
remaining blocker, high, medium or functional low issue after correcting API
403/404 wording, adding production registrar/guard parity coverage and fixing
the normalized `org_viewer` test expectation. PR #148 implementation head
`d69c0414d5543167cc4b89de50eaafef74ede5c0` passed Backend Tests (1m36s),
Frontend Build (3m14s) and Docker Build (1m03s) in required CI run
`29140520863`. Backend logs explicitly show the P6-F36 migrated integration,
production registrar/guard and main-registration tests passing; frontend logs
show 27 files / 136 tests and a successful production build. Final
evidence-head CI and merge evidence remain pending.

## Explicit Remaining Boundaries

- P6-F16's project assignment and live same-organization admission safeguards
  remain unchanged and are not reopened by this organization-administration
  patch.
- Registry and alias business writes still use the existing best-effort audit
  mechanism after the write. This batch corrects target organization
  attribution and denied-call side effects but does not claim the broader
  P6-F17 operation/readback/atomic-audit redesign is complete.
- This batch does not add RLS, composite foreign keys, alias-to-registry foreign
  keys or duplicated organization columns on model-set items. Existing data
  must be audited before any such defense-in-depth migration is separately
  designed.
- The gateway continues to allow an unregistered model for compatibility while
  recording its existing audit, and `/v1/models` still uses API-key allowed
  models or global pricing rather than the organization registry. Changing
  either product rule requires a separate decision and is not inferred here.
- Alias rename cascade, dangling alias policy, a platform organization selector
  UI and target malformed-assignment cleanup are not silently added to P6-F36.
- No target or production behavior is claimed from local tests or future PR CI.
