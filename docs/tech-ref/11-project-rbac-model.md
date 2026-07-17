# Project And RBAC Model

> Version: v1.0
> Status: Current implementation contract
> Source of truth: `internal/auth/auth.go`, `internal/admin/rbac_helpers.go`, project/user/member/budget handlers, and `cmd/uniroute/main.go`

## 1. Goal

PRD2 introduces Project / Workspace as the core enterprise operating boundary below organization.

Target hierarchy:

```text
org -> project -> api key -> request
```

RBAC must enforce this hierarchy in backend handlers and gateway authorization. Frontend navigation is a convenience layer, not the security boundary.

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
| `role` | Persisted membership role: `project_admin`, `project_developer`, or `project_viewer` |
| `created_at`, `updated_at` | Timestamps |

Rules:

- A project member must also be a user in the same organization.
- For project-scoped accounts, user role and membership role are one-to-one: `project_admin -> project_admin`, `developer -> project_developer`, `viewer -> project_viewer`.
- An active project-scoped user must belong to at least one project. A disabled project-scoped user may temporarily have none.
- `project_ids` is invalid for organization-scoped roles. Changing to an organization-scoped role removes existing project memberships.
- User role/status updates and membership mutations use one per-user transaction lock so concurrent requests cannot remove the last project from an active project user.
- Migration `000009` retained default-project membership rows for pre-existing platform/org administrators; those organization-scoped roles authorize through their account scope, and new member writes accept only the three project-scoped account roles.

### API Key Scope

The implemented schema adds `project_id` directly to `api_keys`. Migration
`000009` backfills existing keys to the owning organization's default project;
new project keys carry the selected project in the key row and gateway context.

## 3. Roles

| Role | Scope | Can Do |
|------|-------|--------|
| `platform_admin` | system | All admin actions |
| `org_admin` | org | Manage org users, projects, model access, org settings |
| `billing_admin` | org | Wallet, project budgets, usage, reconciliation |
| `project_admin` | project | Manage project members, keys, allowed models, budget |
| `developer` | project | Use models, manage permitted project keys, view usage |
| `viewer` | project | Read-only project usage/config where granted |

Compatibility:

- Existing `org_viewer` remains a distinct organization-scoped read-only role and does not inherit project `viewer` membership or APIs.
- Existing `org_admin` remains org-level admin.

## 4. Permission Matrix

| Action | platform_admin | org_admin | billing_admin | project_admin | developer | viewer |
|--------|----------------|-----------|---------------|---------------|-----------|--------|
| Manage provider channels | yes | no | no | no | no | no |
| Manage channel keys | yes | no | no | no | no | no |
| Manage model registry | yes | yes if licensed | no | no | no | no |
| Read approved model catalog | yes | yes if licensed | no | own project if licensed | own project if licensed | own project if licensed |
| Assign model sets to project | yes | yes | no | yes for own project if allowed | no | no |
| Create project | yes | yes | no | no | no | no |
| Manage project members | yes | yes | no | own project | no | no |
| Create project API key | yes | yes | no | own project | own project if allowed | no |
| Disable project API key | yes | yes | no | own project | own key if allowed | no |
| View project usage | yes | yes | yes | own project | own project | own project read-only |
| Manage wallet adjustment | yes | no | yes if allowed | no | no | no |
| View audit logs | yes | org scope | no | no | no | no |
| License management | yes | no | no | no | no | no |

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
- `project_admin` cannot grant or remove another `project_admin`, and cannot alter its own membership.
- A target user from another organization is rejected.
- Membership add/update/removal and its audit entry commit in the same transaction.

Implemented APIs:

- `GET /api/v1/projects/{id}/members` returns paginated members plus a bounded, server-filtered list of eligible same-organization users.
- `PUT /api/v1/projects/{id}/members/{user_id}` adds or aligns a membership with the target user's account role.
- `DELETE /api/v1/projects/{id}/members/{user_id}` removes a membership while preserving the active-user invariant.
- `GET/PUT /api/v1/projects/{id}/budget` is the field-limited, tenant-bound budget surface.
- `POST /api/v1/users` and `PUT /api/v1/users/{id}` accept `project_ids` and update the user/membership state atomically.

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

All model-set list/assignment routes require the commercial
`model_governance` feature. `project_admin` may list model sets in its JWT
organization only to obtain assignment candidates, and may read/replace
`/projects/{id}/model-sets` only for a project where both its active account
role and membership role are `project_admin`. It cannot create model sets or
read/replace `/model-sets/{id}/items`; those remain platform/org governance
actions. `developer` and `viewer` may read the sets assigned to their own
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

Historical data rule: migration `000009` uses only the linked API key project or
the owning organization's explicit `default` project; later handlers do not
reassign that stored project provenance.

## 10. Test Requirements

Minimum tests:

- Existing key remains valid after default project migration.
- Developer can create/call with project key.
- Developer cannot read another project.
- Project admin cannot manage another project.
- Billing admin cannot edit channel.
- Retired model blocked even if project set still contains it.
- Audit writes project_id for project key creation.
- Route parity covers Developer Console API routes.

## 11. Resolved V1 Behavior

- An active `developer` with a matching project membership can create a project key and disable only an active key it owns in that project.
- Project budgets are alert thresholds. Reaching a daily/monthly threshold emits a deduplicated `project_budget.threshold` event; it does not reject gateway traffic.
- Historical request rows keep their migrated provenance; handlers do not fabricate a project outside the implemented migration/backfill rules.
