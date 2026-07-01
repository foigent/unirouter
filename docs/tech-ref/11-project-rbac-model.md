# Project And RBAC Model

> Version: v0.1
> Phase: PRD2
> Status: Draft for Review
> Related PRD: `docs/PRD-Phase2.md` P2-F01, P2-F02, P2-F07

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
| `budget_cents` | Optional project budget |
| `budget_period` | `monthly` or future values |
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
| `role` | `project_admin`, `developer`, `viewer` |
| `created_at`, `updated_at` | Timestamps |

Rule:

- A project member must also be an org member.

### API Key Scope

Preferred incremental option:

- Add nullable `project_id` to `api_keys`.
- During migration, set existing keys to default project.

Alternative:

- Add `project_api_keys` table if the current `api_keys` shape becomes too constrained.

Initial recommendation: add `project_id` to `api_keys` for minimal churn.

## 3. Roles

| Role | Scope | Can Do |
|------|-------|--------|
| `platform_admin` | system | All admin actions |
| `org_admin` | org | Manage org users, projects, model access, org settings |
| `billing_admin` | org | Wallet, project budgets, usage, reconciliation |
| `project_admin` | project | Manage project members, keys, allowed models, budget |
| `developer` | project | Use models, manage permitted project keys, view usage |
| `viewer` | org/project | Read-only usage/config where granted |

Compatibility:

- Existing `org_viewer` can map to `viewer` at org scope.
- Existing `org_admin` remains org-level admin.

## 4. Permission Matrix

| Action | platform_admin | org_admin | billing_admin | project_admin | developer | viewer |
|--------|----------------|-----------|---------------|---------------|-----------|--------|
| Manage provider channels | yes | no | no | no | no | no |
| Manage channel keys | yes | no | no | no | no | no |
| Manage model registry | yes | yes if allowed | no | no | no | read approved only |
| Assign model sets to project | yes | yes | no | yes for own project if allowed | no | no |
| Create project | yes | yes | no | no | no | no |
| Manage project members | yes | yes | no | own project | no | no |
| Create project API key | yes | yes | no | own project | own project if allowed | no |
| Disable project API key | yes | yes | no | own project | own key if allowed | no |
| View project usage | yes | yes | yes | own project | own project | own project read-only |
| Manage wallet adjustment | yes | no | yes if allowed | no | no | no |
| View audit logs | yes | org scope | billing-related | own project | own actions/project subset | read-only if granted |
| License management | yes | no | no | no | no | no |

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

## 6. Gateway Authorization

Gateway API key validation should add project context:

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

## 9. Migration Plan

Draft migration sequence:

1. Add `projects` table.
2. Add `project_members` table.
3. Add nullable `project_id` to `api_keys`, `call_logs`, and future usage tables where needed.
4. Backfill one default project per org.
5. Backfill existing `api_keys.project_id` to the org default project.
6. For historical `call_logs`, either backfill default project where reliable or leave nullable with documented historical behavior.
7. Add indexes for `(org_id, project_id, created_at)` on high-volume tables.
8. Update gateway context to carry project_id.
9. Enforce new RBAC in admin/developer handlers.

Historical data rule:

- Do not invent project granularity for old logs beyond default project backfill.
- Mark historical project_id provenance if needed in implementation.

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

## 11. Open Decisions

- Whether `developer` can create keys by default or requires project_admin grant.
- Whether project budgets are enforced hard or alert-only in first PRD2 implementation.
- Whether historical call logs should show default project or "pre-project".
