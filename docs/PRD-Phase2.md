# UniRouter Product Requirements Document - Phase 2

> Version: Phase 2 Draft
> Date: 2026-07-01
> Status: Draft for Review
> Source: `docs/planning/PRD2-DISCOVERY.md`, PRD1 closeout experience, MAAS PRD discussion materials, Davik 2026-06-27 architecture/test references.
> Purpose: formal PRD2 planning baseline. This file supersedes Discovery for PRD2 product scope only after User approval.

---

## 1. Product Positioning

PRD2 evolves UniRouter from a local-deploy MVP into an enterprise internal AI gateway and governance platform.

The product remains local-deploy first. It should not become a public SaaS marketplace in PRD2. Enterprise developers continue to call a pure OpenAI-compatible API. UniRouter absorbs provider differences, cost governance, authorization, model governance, audit, and operational evidence behind that API.

Core product sentence:

> UniRouter PRD2 lets an enterprise safely expose approved AI models to internal projects and developers, with OpenAI-compatible access, project-level governance, provider-adapter extensibility, accurate usage evidence, and auditable administration.

## 2. User Roles

| Role | Scope | Primary Work |
|------|-------|--------------|
| `platform_admin` | deployment | System setup, provider channels, license, global settings |
| `org_admin` | organization | Enterprise org governance, users, projects, model access |
| `billing_admin` | organization | Wallet, project budgets, usage, reconciliation |
| `project_admin` | project | Project members, project keys, allowed models, project budget |
| `developer` | project | Create/use permitted project keys, inspect usage, read model docs |
| `viewer` | org/project | Read-only audit, usage, and configuration access |

PRD2 introduces Developer Console behavior, but uses one frontend application and one login surface. Navigation and permissions separate Admin Console and Developer Console.

## 3. PRD2 Scope Summary

### P0

| ID | Capability | Summary |
|----|------------|---------|
| P2-F01 | Project / Workspace | Add project as core boundary under org |
| P2-F02 | RBAC Expansion | Add project-aware permissions and role-based navigation |
| P2-F03 | Developer Console MVP | Self-service project keys, usage, approved models, request troubleshooting |
| P2-F04 | Model Governance | Registry, approved catalog, model sets, aliases, lifecycle |
| P2-F05 | Provider Adapter Framework | Downstream OpenAI-compatible, upstream adapter abstraction |
| P2-F06 | Token Metering V2 | Normalize usage across providers and reduce estimate variance |
| P2-F07 | Audit Schema Upgrade | Three-level who plus before/after for admin and developer actions |
| P2-F12 | PRD2 Acceptance Discipline | Matrix, tests, RBAC negatives, route parity, mock upstreams |

### P1

| ID | Capability | Summary |
|----|------------|---------|
| P2-F08 | Notifications And Project Budgets | Event system, project budget alerts, webhook routing |
| P2-F09 | HA Readiness | Multi-instance readiness, deployment runbook, smoke evidence |
| P2-F10 | License Foundation | Offline license, expiry, Admin page, audit |
| P2-F11 | Prompt / Response Storage Decision | Design-only unless User explicitly approves implementation |
| P2-F05.1 | Native Adapter Proof | First native adapter, likely Anthropic native |

### P2

| ID | Capability | Summary |
|----|------------|---------|
| P2-F13 | Playground And Docs Integration | Project-key playground, doc_url, error docs, code examples |
| P2-F14 | Advanced Usage Analytics | Drill-down by project/model/key/alias/provider |
| P2-F15 | Cache Research | Exact/semantic cache spike only, not default runtime feature |

## 4. Explicit Non-Goals

PRD2 does not include:

- Reseller / agent channel system.
- Public SaaS dual Portal.
- Payment, invoice, real-name verification, coupon, voucher, campaign.
- Cross-model fallback.
- Full intelligent routing across unrelated models.
- Full official SDK suite across languages.
- Complex semantic cache as a default runtime feature.
- Full support-ticket CRM.
- Marketing automation platform.
- Online license marketplace.
- Mandatory microservice split.

## 5. Functional Requirements

### P2-F01: Project / Workspace

Project becomes the operational boundary below org.

Data hierarchy:

```text
org_id -> project_id -> api_key_id -> request_id
```

Requirements:

- Each org has at least one default project.
- Existing PRD1 org-level resources migrate or map to a default project without breaking existing gateway calls.
- API keys can be scoped to a project.
- Call logs, usage, budgets, and audit records can be filtered by project.
- Project membership is constrained by org membership.
- Project settings include name, status, allowed model sets, budget, and optional description.

Acceptance:

- Existing PRD1 API keys remain usable after migration.
- A new project key can call only models allowed for that project.
- Project usage equals the sum of successful project calls in the selected range.
- A user without project membership cannot read project keys, logs, usage, or settings.

### P2-F02: RBAC Expansion

Requirements:

- Enforce role checks in backend handlers, not only frontend navigation.
- Add project-level roles: `project_admin`, `developer`, `viewer`.
- Add org-level `billing_admin`.
- Preserve `platform_admin`, `org_admin`, and `org_viewer` compatibility where practical.
- Add negative tests for cross-org and cross-project access.
- Role-aware frontend navigation hides inaccessible surfaces but backend remains the source of truth.

Acceptance:

- `developer` can manage permitted project keys but cannot manage channels, provider keys, wallet adjustments, license, or global settings.
- `billing_admin` can view wallet/usage/budget but cannot edit provider channels or project members.
- `project_admin` can manage project members and project keys but cannot access other projects.
- Cross-org and cross-project API access returns 403.

### P2-F03: Developer Console MVP

Developer Console is a role-aware surface inside the existing frontend application.

P0 pages:

- Project overview.
- Project API keys.
- Approved model catalog.
- Project usage.
- Request troubleshooting by request_id.
- API docs entry.

Requirements:

- Developer Console uses current login and JWT session.
- Developer default landing shows current project, allowed model aliases, recent usage, and key status.
- Key creation returns plaintext once.
- Request troubleshooting never exposes provider keys or restricted admin data.
- Developer-facing model list defaults to approved models, not full registry.

Acceptance:

- A developer can create a project key and call `/v1/chat/completions`.
- A developer can see only usage for projects they can access.
- A developer cannot open Admin Console pages by URL.
- Request troubleshooting by request_id shows normalized status, model, alias, cost, token counts, and request metadata.

### P2-F04: Model Governance

UniRouter should provide enterprise model governance, not a public model marketplace.

Concepts:

| Concept | Description |
|---------|-------------|
| Model Registry | Admin-only full internal model inventory |
| Approved Model Catalog | Developer-facing approved model list |
| Model Set | Curated group of models for a scenario |
| Model Alias | Stable enterprise-facing name mapped to provider model(s) |
| Model Lifecycle | `staging -> ga -> deprecated -> retired` |

Requirements:

- Admins can create and edit model registry entries.
- Registry entries include context length, output limit, capability tags, provider family, data residency, pricing reference, and lifecycle.
- Approved catalog shows only `ga` and explicitly allowed `deprecated` models.
- Deprecated models include migration recommendation.
- Retired models cannot be called through aliases or project allowlists.
- Project allowed models are assigned through model sets and/or explicit aliases.

Acceptance:

- Developer catalog shows approved models only.
- A project cannot call a model outside its allowed set.
- Deprecated model displays replacement guidance.
- Retired model returns 404 or model-not-available error even if an old key requests it.

### P2-F05: Provider Adapter Framework

Downstream client protocol remains OpenAI-compatible.

Upstream provider differences must be handled by adapters.

P0 adapter types:

- `openai_compatible`: current relay semantics, using base_url, Authorization Bearer, model_mapping, and OpenAI-compatible request/response.

P1 proof adapter:

- First native adapter selected by implementation planning. Default candidate: `anthropic_native`.

Requirements:

- Add adapter interface and registry.
- Add `channels.adapter_type`.
- Convert current relay behavior into adapter semantics without changing downstream API.
- Normalize upstream response into common usage, error, latency, model, and metadata structures.
- Billing, logs, metrics, and circuit breaker consume normalized adapter output.
- Adapter must support non-stream and stream paths where provider supports streaming.
- Channel test uses the configured adapter.

Acceptance:

- Existing OpenAI-compatible providers keep working through `openai_compatible`.
- Adapter mock upstream proves request conversion, response conversion, usage parsing, streaming final metadata, and error mapping.
- A provider-specific 401 marks/cools the key according to existing F12 classification.
- A provider-specific 5xx contributes to channel failure and circuit behavior.

### P2-F06: Token Metering V2

Requirements:

- Normalize usage from provider-native and OpenAI-compatible responses.
- Record `usage_source` with values such as `upstream`, `estimated`, `adapter_estimated`, `counter_v2`.
- Track prompt, completion, total tokens, and source confidence where useful.
- Surface estimated-vs-upstream variance in reconciliation.
- Keep PRD1 billing safety invariant: no negative wallet balance.

Acceptance:

- Normal successful upstream usage is recorded as `upstream`.
- Missing stream usage uses deterministic fallback and records non-upstream usage source.
- Reconciliation can filter estimated and upstream records separately.
- Estimated usage above configured ratio triggers dashboard or notification signal.

### P2-F07: Audit Schema Upgrade

Requirements:

- Audit records include actor, org, project when applicable, action, resource type/id, before, after, ip, user_agent, request_id, source.
- Source values include `admin_console`, `developer_console`, `gateway_api`, and `system`.
- Critical admin actions record before/after values.
- Audit list supports filtering by org, project, actor, action, resource, time range, and source.

Acceptance:

- Project key creation records audit with actor and project.
- Model lifecycle change records before/after and actor.
- Wallet adjustment or license change records before/after and privileged actor.
- Developer cannot read global audit logs.

### P2-F08: Notifications And Project Budgets

Requirements:

- Add project budget configuration.
- Emit events for budget threshold, key disabled/revoked, model deprecated/retired, provider/channel failure, license warning, and high estimated-usage ratio.
- Support at least admin-visible notification records and webhook routing.
- Email or IM delivery can be staged by priority, but event persistence should be shared.

Acceptance:

- Project budget threshold can be configured and audited.
- Crossing threshold creates an event and sends configured webhook where enabled.
- Event retry/failure state is visible to admin.

### P2-F09: HA Readiness

Requirements:

- Document multi-instance gateway assumptions.
- Ensure shared mutable state is DB/Redis backed.
- Add smoke procedure for two gateway instances behind a proxy where practical.
- Document backup and restore expectations for PRD2.
- Keep advanced HA stack optional unless deployment requires it.

Acceptance:

- Two instances can serve gateway and admin health checks using shared DB/Redis.
- Repeated requests through two instances do not violate idempotency or wallet safety.
- Deployment runbook states rollback and health verification steps.

### P2-F10: License Foundation

Requirements:

- Support offline license file.
- License includes customer/deployment metadata, expiry, product tier, and signature.
- Admin Console exposes license status.
- Expiry warning creates event/audit record.
- License change is audited.

Acceptance:

- Invalid license is rejected with clear admin-facing error.
- Expiring license produces warning without breaking existing requests before grace rules apply.
- License metadata is visible to platform_admin.

### P2-F11: Prompt / Response Storage Decision

P0 does not store prompt or response plaintext by default.

The formal design must answer:

- Whether optional plaintext storage is needed.
- Whether storage is disabled by default.
- Retention windows.
- Redaction and encryption rules.
- Access control and audit for every read.
- Customer-visible controls.

No implementation should start until User confirms the storage policy.

### P2-F12: Acceptance Discipline

Requirements:

- Create `docs/ops/PRD2-ACCEPTANCE-MATRIX.md` before implementation.
- Every PRD2 feature has acceptance rows before coding.
- Provider behavior requires mock upstream tests.
- RBAC behavior requires positive and negative tests.
- Frontend/backend route changes require route parity tests or equivalent contract check.
- Canary verifies implementation or current-state document changes through `FIX-SIGNAL.md`.

Acceptance:

- PRD2 acceptance matrix exists and lists P0/P1/P2 rows.
- Formal PRD2 references the matrix and PRD2 technical specs.
- `CURRENT-STATUS.md` distinguishes PRD1 frozen/closed state from PRD2 planning/in-progress state.

### P2-F13: Playground And Docs Integration

P2 scope.

Requirements:

- Playground uses project-scoped key context.
- No shared platform key is exposed.
- Model choices come from approved model catalog.
- Outputs include request_id, cost, token counts, latency, and copyable code.
- Error responses link to docs through `doc_url` where implemented.

### P2-F14: Advanced Usage Analytics

P2 scope.

Requirements:

- Usage drill-down by org, project, key, model, alias, provider, channel, status, and usage_source.
- CSV export should respect role scope.
- Estimated usage ratio is visible.

### P2-F15: Cache Research

P2 research spike only.

Requirements:

- Compare exact cache and semantic cache risks.
- Do not enable runtime cache by default.
- Evaluate privacy, billing, audit, and correctness risk before production implementation.

## 6. Data Model Impact

Expected new or changed entities:

- `projects`
- `project_members`
- `project_api_keys` or `api_keys.project_id`
- `model_registry`
- `model_aliases`
- `model_sets`
- `model_set_items`
- `project_model_sets`
- `provider_adapters` or `channels.adapter_type`
- `usage_metering_details`
- audit schema additions
- license metadata table or settings-backed license record
- notification/events tables

Compatibility rule:

- Existing PRD1 gateway API remains `/v1/*`.
- Existing org-level keys must keep working until migration behavior is explicitly changed.
- Any schema migration must include rollback and data backfill notes.

## 7. Phase Governance

PRD1 and PRD2 are separate product phases.

- PRD1 is frozen after closeout and should not receive PRD2 requirements.
- PRD2 gets its own acceptance matrix and technical references.
- Current state remains centralized in `docs/ops/CURRENT-STATUS.md`.
- PRD2 issue IDs use phase prefixes such as `PRD2-AUTH-001` and `PRD2-ADAPTER-001`.

## 8. First Development Sequence

Recommended implementation order after PRD2 approval:

1. Create PRD2 acceptance matrix and technical specs.
2. Add Project/RBAC schema and default project migration.
3. Enforce project-aware authorization and route parity tests.
4. Add model registry, model aliases, and project model sets.
5. Introduce provider adapter interface and `openai_compatible` adapter.
6. Add token metering normalization foundation.
7. Add Developer Console MVP surfaces.
8. Add audit schema upgrade.
9. Add notifications/project budgets and license foundation.

## 9. Formal Approval Gates

PRD2 planning is ready for implementation only after:

1. User accepts this PRD.
2. `docs/ops/PRD2-ACCEPTANCE-MATRIX.md` exists.
3. Provider Adapter Framework spec exists.
4. Project/RBAC spec exists.
5. Developer Console spec exists.
6. `CURRENT-STATUS.md` names PRD2 as active planning or implementation phase.
