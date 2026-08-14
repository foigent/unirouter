# Project And RBAC Model

> Version: v2.0
> Status: Current implementation contract
> Source of truth: `internal/auth/auth.go`, `internal/admin/rbac_helpers.go`, project/user/member/budget handlers, and `cmd/uniroute/main.go`

## 1. Goal

PRD2 introduces Project / Workspace as the core enterprise operating boundary below organization.

Target hierarchy:

```text
org -> project -> api key -> request
```

Human authorization has two orthogonal axes:

```text
users.organization_role + live project_members.role
```

`users.role` remains a one-release compatibility shadow for old binaries and
accepted v2 sessions. It is not an authorization source in current handlers.
RBAC must enforce organization and project authority in backend handlers and
gateway authorization. Frontend navigation is a convenience layer, not the
security boundary.

## 2. Core Entities

### `projects`

| Field | Purpose |
|-------|---------|
| `id` | Project ID |
| `org_id` | Owning org |
| `name` | Display name |
| `slug` | Stable URL/API slug where needed |
| `status` | `active`, `disabled`, `archived` |
| `description` | Optional description |
| `budget_cents` | Optional whole-cent alert threshold, `0..2147483647` |
| `budget_period` | `none`, `daily`, or `monthly` |
| `created_by` | User who created project |
| `created_at`, `updated_at` | Timestamps |

Default project rule:

- Every existing org receives one default project during PRD2 migration.
- Existing org-scoped API keys should map to the default project unless a more specific project is assigned.

### `project_members`

| Field | Purpose |
|-------|---------|
| `project_id` | Project |
| `user_id` | User |
| `role` | Canonical membership role: `project_admin`, `project_user`, or `project_viewer` |
| `created_at`, `updated_at` | Timestamps |

Rules:

- A project member must also be an active human account in the same organization.
- `users.organization_role` and `project_members.role` are independent. Any
  same-organization account, including an organization administrator or viewer,
  may hold an explicit membership without gaining that role in another project.
- One person may be `project_admin` in project A and `project_viewer` in project
  B. Every project decision reads the live membership for that exact project.
- `organization_role=member` may have zero memberships. Account creation and
  organization-role editing do not implicitly create, replace, or delete project
  memberships.
- Organization roles retain their organization authority but do not
  automatically become project members. Explicit memberships remain when an
  organization role changes.
- Membership mutations serialize on the user/project rows, update the
  conservative `users.role` compatibility shadow where applicable, revoke stale
  sessions/credentials when required, and write audit in the same transaction.
- During one rolling-release window, DB triggers normalize legacy
  `users.role` writes and `developer`/`project_developer`/`viewer` membership
  writes to canonical values. An INSERT that omits `organization_role` is
  identified by a non-persistable sentinel default; an explicit value is not
  inferred. PostgreSQL cannot distinguish a legacy UPDATE of only `role` from
  a client that also redundantly assigns `organization_role` to its unchanged
  old value. For this bounded window, that exact ambiguous UPDATE shape uses
  legacy normalization semantics. Current product services validate and write
  both axes for organization-role changes and never rely on that shape.
  Unknown values still fail closed. Removing the users and project-membership
  compatibility triggers and sentinel default requires a later explicit
  migration after old writers retire.

### API Key Scope

The implemented schema adds `project_id` directly to `api_keys`. Migration
`000009` backfills existing keys to the owning organization's default project;
new project keys carry the selected project in the key row and gateway context.

## 3. Roles

Organization roles (`users.organization_role`):

| Role | Scope | Can Do |
|------|-------|--------|
| `platform_admin` | system | Platform administration and explicit tenant operations |
| `org_admin` | org | Manage organization users, projects, model access, and settings |
| `billing_admin` | org | Wallet, project budgets, usage, pricing reads, and reconciliation |
| `org_viewer` | org | Read ordinary organization/project metadata; no calls or writes |
| `member` | account | No organization-wide authority; project access requires live membership |

Project membership roles (`project_members.role`):

| Role | Scope | Can Do |
|------|-------|--------|
| `project_admin` | one project | Manage non-admin members, project settings, credentials, and model assignment |
| `project_user` | one project | Invoke approved models and manage only their own personal credential |
| `project_viewer` | one project | Read non-secret project metadata and usage; cannot invoke or write |

Compatibility aliases `project_developer`, `developer`, and `viewer` are accepted
only at documented migration/OIDC/API boundaries and normalize to canonical
membership roles. Current responses, UI, and authorization use canonical names.

## 4. Permission Matrix

| Action | platform_admin | org_admin | billing_admin | org_viewer | project_admin | project_user | project_viewer |
|--------|----------------|-----------|---------------|------------|---------------|--------------|----------------|
| Manage provider channels | yes | no | no | no | no | no | no |
| Manage channel keys | yes | no | no | no | no | no | no |
| Manage model registry | yes | yes if licensed | no | no | no | no | no |
| Read approved model catalog | yes | yes if licensed | no | org read if licensed | own project if licensed | own project if licensed | own project if licensed |
| Assign model sets to project | yes | yes | no | no | own project | no | no |
| Create project | yes | yes | no | no | no | no | no |
| Read ordinary project metadata | yes | yes | list only | org read | own project | own project | own project |
| Manage project members | yes | yes | no | no | own project with restrictions | no | no |
| Create project credential | yes | yes | no | no | own project | own personal credential | no |
| Disable project credential | yes | yes | no | no | own project | own active personal credential | no |
| Invoke approved models | yes | yes | no | no | own project | own project | no |
| View project usage | yes | yes | org finance scope | org read | own project | own project | own project read-only |
| Manage wallet adjustment | yes | no | yes if allowed | no | no | no | no |
| View audit logs | yes | org scope | no | no | no | no | no |
| License management | yes | no | no | no | no | no | no |

Budget authority is intentionally narrower than project authority. `billing_admin`
may list projects in its JWT organization and use only
`GET/PUT /api/v1/projects/{id}/budget`; it cannot open project detail, manage
members/model sets/keys, or mutate non-budget project fields. A configured
project budget is currently an event threshold, not a hard request-admission
cap.

Every `no` cell must have at least one negative test when the feature is implemented.

## 5. Authorization Rules

Backend checks must answer:

1. Who is the actor?
2. Which org is being accessed?
3. Which project is being accessed, if any?
4. Does the actor have the required role at the required scope?
5. Is the resource inside that org/project boundary?

Recommended helper shape:

```go
RequireOrgRole(ctx, orgID, roles...)
RequireProjectRole(ctx, projectID, roles...)
RequireSystemRole(ctx, roles...)
```

Avoid relying on client-supplied org/project alone. Load resource ownership from DB for update/delete/read-by-id paths.

Implemented member constraints:

- `platform_admin` and the owning `org_admin` may manage eligible project roles.
- `project_admin` must have an active `project_admin` membership in the target project.
- `project_admin` cannot create, promote, downgrade, overwrite, or remove another
  `project_admin`, and cannot alter its own membership. Platform/org admins keep
  that authority.
- A target user from another organization is rejected.
- Membership add/update/removal and its audit entry commit in the same transaction.

Implemented APIs:

- `GET /api/v1/projects/{id}/members` returns paginated members plus a bounded, server-filtered list of eligible same-organization users.
- `PUT /api/v1/projects/{id}/members/{user_id}` explicitly creates or updates a
  canonical membership independently of the target account's organization role.
- `DELETE /api/v1/projects/{id}/members/{user_id}` removes that one membership;
  a `member` account may remain active with zero memberships.
- `GET/PUT /api/v1/projects/{id}/budget` is the field-limited, tenant-bound budget surface.
- `POST /api/v1/users` and `PUT /api/v1/users/{id}` manage account identity and
  organization role only. Project memberships are maintained at the project
  endpoint and are never inferred from the compatibility shadow.

OIDC applies the same split: organization-role defaults create only the account
axis; canonical project roles (and accepted aliases) create
`organization_role=member` plus one membership in the configured default
project. OIDC never converts an organization role into implicit project access.

## 6. Gateway Authorization

Gateway API key validation carries project context:

1. Validate API key prefix/hash.
2. Check key status.
3. Load `org_id` and `project_id`.
4. Check IP allowlist.
5. Check requested model against key allowed models and project allowed model sets.
6. Put `org_id`, `project_id`, and `api_key_id` into request context.

Call logs and transactions should include project_id where applicable.

## 7. Model Access

Model access combines:

- key allowed models
- project allowed model sets
- model alias lifecycle
- model registry status

Rule:

```text
allowed = key_allows(requested_model_or_alias)
       AND project_allows(requested_model_or_alias)
       AND model_lifecycle_allows_call
```

Retired models must not be callable even if old keys or project sets reference them.

All model-set list/assignment routes require an enforceable commercial License;
model governance is included as a core management module rather than a separate
signed feature item. `project_admin` may list model sets only after live
membership authorization and may read/replace `/projects/{id}/model-sets` only
for that project. Its organization role and compatibility shadow do not grant
project authority. It cannot create model sets or read/replace
`/model-sets/{id}/items`; those remain platform/org governance actions.
`project_user` and `project_viewer` may read the sets assigned to their own
project but cannot replace them.

## 8. Audit Requirements

Audit records should include:

- actor_user_id
- actor_role
- org_id
- project_id if applicable
- action
- resource_type
- resource_id
- before
- after
- ip
- user_agent
- request_id
- source

Source values:

- `admin_console`
- `developer_console`
- `gateway_api`
- `system`

## 9. Implemented Migration Shape

Implemented sequence:

1. Add `projects` table.
2. Add `project_members` table.
3. Add nullable `project_id` to `api_keys`, `call_logs`, and future usage tables where needed.
4. Backfill one default project per org.
5. Backfill existing `api_keys.project_id` to the org default project.
6. Backfill historical `call_logs` from their API key first, then the organization default project; backfill historical transactions and audit rows to the organization default project.
7. Add indexes for `(org_id, project_id, created_at)` on high-volume tables.
8. Update gateway context to carry project_id.
9. Enforce new RBAC in admin/developer handlers.
10. Migration `000058` adds `users.organization_role`, canonicalizes
    `project_members.role`, creates conservative `users.role` and legacy-write
    compatibility guards, and fails closed when a down migration would collapse
    an orthogonal or heterogeneous state.

Historical data rule: migration `000009` uses only the linked API key project or
the owning organization's explicit `default` project; later handlers do not
reassign that stored project provenance.

## 10. Test Requirements

Minimum tests:

- Existing key remains valid after default project migration.
- Project user can create/call with their personal project credential.
- Project user cannot read another project.
- Project admin cannot manage another project.
- One account can be project admin in A and project viewer in B without the
  compatibility shadow granting B writes.
- An organization role plus an explicit project membership remains orthogonal.
- Billing admin cannot edit channel.
- Retired model blocked even if project set still contains it.
- Audit writes project_id for project key creation.
- Route parity covers Developer Console API routes.

## 11. Resolved Current Behavior

- An active `project_user` with live membership can create a personal project
  credential and disable only an active credential it owns in that project.
- Project budgets are alert thresholds. Reaching a daily/monthly threshold emits a deduplicated `project_budget.threshold` event; it does not reject gateway traffic.
- Historical request rows keep their migrated provenance; handlers do not fabricate a project outside the implemented migration/backfill rules.
