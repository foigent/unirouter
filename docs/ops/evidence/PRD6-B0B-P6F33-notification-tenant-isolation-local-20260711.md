# PRD6 Batch 0B P6-F33 Notification Tenant Isolation — Local Evidence

> Date: 2026-07-11
> Base: `d3cf16f2169aeef1899c5e55b3b04a40d0a5c6a7`
> Evidence level: Local verified; required PR CI and deployment are not claimed

## Scope

P6-F33 applies the accepted D6-08 authority boundary to notification events,
deliveries and Webhook sinks:

- authenticated non-platform actors are always constrained to the organization
  in their JWT;
- a supplied `org_id` cannot widen that scope;
- a project-scoped Webhook must belong to the same organization and a project
  administrator must hold write membership for that project;
- `platform_admin` retains explicit cross-tenant authority;
- a missing organization identity fails closed;
- rejected requests create no Webhook, audit row or external delivery.
- migration `000020` retains but quarantines every pre-fix Webhook until an
  authorized tenant/platform actor explicitly confirms its stored scope and
  destination; unverified routes are excluded from both new delivery and retry.

This repository-only patch does not send a real provider request, change a
target environment, expand retained data, or alter the PRD5/X-Insight contract.

## Red Evidence

Before the handler repair, the migrated PostgreSQL/JWT integration test
`TestP6F33NotificationTenantIsolationIntegration` failed with observable
cross-tenant behavior:

- organization and billing administrators received `200` for another
  organization's notification events;
- an organization administrator received `201` when creating a foreign sink;
- an organization administrator received `200` when updating a foreign sink;
- a non-platform token with organization `0` received an unscoped `200` list;
- the denied-sink test instead created the sink with `201`.

The fixture failed first for an unrelated `project_members.id` assumption; that
fixture defect was corrected, then the authorization failures above were
reproduced before production code changed.

Independent review then found that future-write enforcement alone would leave
a pre-fix hostile row deliverable. That release blocker was reproduced with a
seeded historical row and closed with the non-destructive quarantine below.

## Implementation

- `notificationProjectScopedConditions` now rejects foreign explicit
  organizations and always injects the JWT organization for constrained actors.
- Webhook create/update authorization now checks both target organization and
  project-to-organization ownership.
- Update uses the already-authorized organization in its final SQL predicate.
- Delivery reads only resolve a Webhook display name when the event and Webhook
  organizations agree.
- The shared RBAC helper now exposes an explicit project/organization ownership
  predicate while preserving existing actor-scoped behavior.
- migration `000020` disables and marks all existing routes
  `scope_verified=false`; it preserves the former enabled intent for review.
  New authorized creates explicitly start at verification/write generation 1.
- a database safe-writer trigger rejects inserts and updates from pre-P6-F33
  binaries; every new-code edit increments the generation, re-quarantines the
  route and requires a fresh confirmation.
- the new audited `POST /notification-webhooks/{id}/verify-scope` action and
  Notification Center state/button require explicit review before an old route
  can deliver again. Confirmation is bound to the displayed `updated_at`
  snapshot, uses a row lock, and commits state plus audit atomically.
- delivery and retry queries require `scope_verified=true`; no route is deleted
  or silently rewritten.
- pre-migration failed deliveries keep their former due time as evidence but
  have active scheduling cleared and remain excluded from automatic retry even
  after route verification.
- non-platform update/verify lookup includes the JWT organization, so foreign
  and nonexistent IDs both return `404` instead of exposing object existence.
- the down migration disables all routes, installs an `enabled=false` lockdown
  plus an unconditional table read-only trigger, and atomically removes its
  migration-ledger row; down→up rebuilding is tested. It deliberately allows
  neither delivery nor legacy configuration writes on a vulnerable binary.

The database trigger protects mixed-version writes and delivery. It cannot add
tenant predicates to an already-running old binary's read handlers. Therefore
deployment requires a non-rolling notification-control-plane cutover: block or
drain every pre-P6-F33 admin endpoint before allowing notification reads on the
new release. That target action was not performed here.

## Green Evidence

Command:

```text
go test ./internal/migrate ./internal/admin ./internal/billing ./cmd/uniroute -run 'TestP6F33|TestListNotification|TestCreateNotificationWebhook|TestEventService|TestAdminRoutesRegisteredForFrontendClient|TestOpenAPISpecCoversRegisteredRoutes|TestFrontendDocsPageCoversRegisteredRoutes' -count=1 -v
```

Result: PASS. The migrated integration matrix covers:

1. foreign event/delivery/Webhook lists denied for organization and billing
   administrators;
2. same-tenant reads and the explicit platform exception;
3. foreign-organization and project/organization-mismatch creates denied with
   unchanged Webhook/audit counts;
4. project administrators may create in their member project but not another
   same-org project; new authorized routes start verified;
5. foreign update is indistinguishable from a missing ID; an authorized edit is
   allowed but becomes quarantined until a new explicit confirmation;
6. a seeded pre-fix hostile route is retained but excluded from new delivery
   and retry, cannot be verified by a foreign actor, rolls back if audit fails,
   and accepts a new event only after its own tenant confirms the exact visible
   snapshot; its old failed delivery remains non-replayable;
7. organization `0` fail-closed behavior;
8. a newly denied local receiver receives zero victim-event calls;
9. platform cross-tenant update remains allowed while mismatched project/org
   creation remains denied.

The migration integration additionally proves pre-up enabled/disabled intent
capture, pre-up failed-delivery quarantine, old-writer insert/update rejection,
safe down-mode rejection of re-enable, destination mutation, disabled-row
creation and deletion, migration-ledger removal, and down→up reconstruction.

Full local regression also passed:

```text
go test -race ./... -count=1
go vet ./...
npm --prefix frontend run type-check
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Final exact-diff result: frontend 25 files / 126 tests PASS. Backend race, vet,
route/OpenAPI/docs parity and `git diff --check` also PASS. The build retained the existing
large-chunk warning and Go race linking retained the existing macOS
`LC_DYSYMTAB` warning; neither command failed. Required PR checks and exact-head
evidence remain promotion gates; this file does not pre-claim them.

Independent final review found no remaining direct old-writer, verification,
down/roll-forward or retry bypass. It confirmed two separate promotion guards:

- old read handlers and already-buffered/in-flight Webhook sends cannot be
  repaired retroactively by a migration, so target cutover/drain and readback
  remain required before any production claim;
- `ListAuditLogs` still lacks its own actor-organization predicate. That is a
  separate confirmed P0 row/PR; P6-F33 does not claim the whole administrator
  surface is tenant safe.
