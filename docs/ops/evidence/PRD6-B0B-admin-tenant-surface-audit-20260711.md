# PRD6 Batch 0B Administrator Tenant-Surface Audit

> Date: 2026-07-11
> Audited baseline: `main@62f161e2866a3a4c699abb9a647feef48bded1b2`
> Evidence level: source confirmed, read-only audit; no exploit, target, or
> production verification
> Scope: administrator routes outside the exact P6-F16 key/user/project and
> project-model-set boundary

## 1. Executive Finding

P6-F16 is not the full administrator tenant boundary. The route table grants
several organization or billing roles by role alone, while affected handlers
trust request `org_id`, use a global object ID, or select the first/global row.
The resulting paths must be separate P0/P1 work rather than hidden inside the
P6-F16 completion claim.

## 2. Confirmed Findings

### P6-F32 — Finance Tenant Boundary (P0)

`billing_admin` routes are role-gated at `cmd/uniroute/main.go:231-233`, but:

- `internal/admin/wallet_handlers.go:13-16` reads every wallet and returns the
  first row;
- `internal/admin/wallet_handlers.go:78-80` and `174-193` top up or adjust the
  first wallet instead of the actor organization;
- `internal/admin/wallet_handlers.go:235-293` lists and exports all tenant
  transactions;
- `internal/admin/grant_handlers.go:10-21` lists all grants, while `48-107`
  accepts an arbitrary request-body organization and changes its wallet;
- `internal/admin/model_handlers.go:40-90` and `170-190` expose cross-tenant
  reconciliation data.

Impact: a valid tenant billing administrator can read or modify another
tenant's financial state. No production exploitation was attempted.

### P6-F33 — Notification Tenant Boundary (P0)

- `internal/admin/notification_handlers.go:444-461` trusts query `org_id` for
  event, delivery, and webhook reads;
- `internal/admin/notification_handlers.go:254-297` accepts a foreign body
  organization when creating a webhook;
- `internal/admin/notification_handlers.go:315-395` updates by global webhook
  ID, while `486-491` grants organization/billing roles without target-org
  comparison;
- migration `000014` has independent organization and project foreign keys but
  no same-organization constraint;
- `internal/billing/notifications.go:285-300` and `337-366` route an event and
  payload by the webhook's stored organization and URL.

Impact: an organization administrator can register an attacker-controlled URL
for a victim organization and receive later victim events, or redirect an
existing victim webhook. This is a source-reachable exfiltration chain; it was
not executed.

### P6-F34 — Pricing Authority Boundary (P0)

- `internal/admin/model_handlers.go:307-406` lets an `org_admin` mutate or
  delete the global base-price table;
- `internal/admin/model_handlers.go:418-530` trusts arbitrary organization IDs
  or global row IDs for organization multipliers;
- `internal/billing/billing.go:82-98` consumes both values in billing.

Impact: one tenant administrator can change platform-wide base pricing or a
victim tenant's multiplier. The correct authority for global and organization
pricing is a product decision recorded below.

### P6-F35 — Global Control-Plane Authority (P0)

- `internal/admin/settings_handlers.go:9-50` exposes and mutates global settings
  for `org_admin`, with no key allowlist or reserved-key gate;
- migration `000001` stores deployment settings without an organization ID;
- the platform-only license handler validates signed license writes, but the
  same `license.offline` setting can be overwritten through generic settings;
- `internal/admin/sensitive_handlers.go:26-169` lets an organization admin
  alter the one global sensitive-word list consumed by all tenants.

Impact: tenant roles can change deployment-wide controls, bypass the dedicated
license write path, disclose configured values, or create a platform-wide
request-denial condition.

### P6-F36 — Model Governance Ownership (P1, security prerequisite)

On the audited baseline, `orgIDFromRequest` trusts query `org_id`; registry,
alias, and model-set creates accept body organization; updates and item replace
use global IDs. `ListApprovedModelCatalog` also permits a project-scoped role to
omit project ID and request a foreign organization. Relevant source is
`internal/admin/model_governance_handlers.go:21-155`, `256-272`, `316-338`,
`410-542`, `555-718`, and `804-815` on the audited commit.

Impact: cross-tenant catalog disclosure and mutation of lifecycle, alias, or
model-set data consumed by live model admission. P6-F16 protects only project
assignment and runtime consumption from malformed cross-org assignments; it
does not close these organization-level endpoints.

## 3. Hypotheses And Unknowns

- No production incident, active exploitation, tenant count, or affected-row
  count was verified.
- It is unknown whether any current deployment intentionally grants tenant
  administrators platform-wide pricing, settings, or sensitive-word authority.
- The exact event payload sensitivity and real webhook reachability depend on
  deployment configuration; the source chain is sufficient to require closure.
- PostgreSQL RLS/composite foreign keys may be valuable defense in depth, but
  the first fix must constrain application authorization and final SQL sinks.

## 4. Proposed Ownership And Order

| Row | Default owner and rule | Suggested order | Scope state |
|-----|------------------------|-----------------|-------------|
| P6-F33 | UniRouter; non-platform actors can read/write only their JWT org and same-org project | first, because it closes an event-exfiltration sink | New confirmed input; not silently accepted by D6-01 |
| P6-F32 | UniRouter; billing admin is tenant-scoped, platform admin is the explicit global exception | second | New confirmed input; not silently accepted by D6-01 |
| P6-F34 | UniRouter billing authority; global base price is platform-only, org multiplier policy needs decision | third after authority decision | New confirmed input; decision required |
| P6-F35 | UniRouter deployment control plane; current global settings/sensitive words default to platform-only | fourth after authority decision | New confirmed input; decision required |
| P6-F36 | UniRouter; organization-level model governance is actor-org scoped, platform admin is the exception | fifth, before broader Agent/model rollout | New confirmed input; not silently accepted by D6-01 |

## 5. Minimum Product Decision

Confirm or revise this default authority split:

1. global base prices, deployment settings, license material, and the current
   global sensitive-word list are `platform_admin` mutation surfaces;
2. an `org_admin` may manage only its own organization's model governance and
   notification webhooks;
3. a `billing_admin` may manage only its own wallet/grants/reconciliation;
4. organization multiplier mutation is either own-organization self-service or
   platform/billing-authority only; this is the only unresolved role choice.

No decision is needed to preserve the already accepted P6-F16 implementation.
