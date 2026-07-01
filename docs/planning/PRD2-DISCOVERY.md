# UniRouter PRD2 Discovery

> Status: direction confirmed by User; not yet the formal PRD.
> Purpose: freeze PRD2 product direction, scope boundaries, and first planning decisions before writing `docs/PRD-Phase2.md`.

## Source Materials

- `docs/PRD-Phase1.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD-ACCEPTANCE-MATRIX.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/agent-workflows/AGENT-PROTOCOL.md`
- `/Users/m4/Desktop/projects/MAAS PRD讨论/MAAS产品需求文档PRD-design by Claude.pdf`
- `/Users/m4/Desktop/projects/MAAS PRD讨论/2026-06-27Davik/`

## Product Direction

PRD2 should evolve UniRouter from a local-deploy MVP into an enterprise internal AI gateway and governance platform.

The long-term optimal direction is not to copy a public SaaS "model marketplace" or dual-portal commercial platform. UniRouter should keep its strongest product boundary:

1. Enterprise local deployment first.
2. Downstream client API remains OpenAI-compatible.
3. Upstream provider differences are hidden behind UniRouter.
4. Enterprise admins govern models, projects, keys, budgets, usage, audit, and provider capacity.
5. Enterprise developers get a controlled self-service console instead of broad platform-admin access.

## Confirmed Decisions

### D1: Product Shape

PRD2 remains a local-deploy enterprise product.

There should be one frontend application and one login surface in PRD2, with role-based navigation separating:

- Admin Console: channels, provider keys, model governance, wallet, pricing, audit, settings, system operations.
- Developer Console: projects, project keys, approved model catalog, usage, docs, playground, request troubleshooting.

PRD2 should not introduce public SaaS dual-domain portals.

### D2: Project As Core Boundary

Project / Workspace should become a PRD2 core data boundary.

PRD1 is mostly organized around `org_id` and API keys. PRD2 should extend this to:

```text
org_id -> project_id -> api_key_id -> request_id
```

Project should control:

- Project members and roles.
- Project-scoped API keys.
- Project allowed model sets.
- Project budget and budget alerts.
- Project usage and cost reports.
- Project audit history.

Migration principle: existing PRD1 org-level data should be attached to a default project where needed, without breaking existing gateway calls.

### D3: Auth And RBAC

PRD2 must extend authorization, but should not rewrite the whole auth system first.

P0:

- RBAC expansion.
- Project-level authorization checks.
- Role-aware Admin Console / Developer Console navigation.
- 2FA for privileged users.

Recommended role model:

| Role | Scope | Purpose |
|------|-------|---------|
| `platform_admin` | system | Full deployment administration |
| `org_admin` | org | Enterprise administrator |
| `billing_admin` | org | Wallet, budget, billing, reconciliation |
| `project_admin` | project | Project members, keys, budget, model access |
| `developer` | project | Use models, manage own permitted keys, inspect usage |
| `viewer` | org/project | Read-only access |

P1:

- SSO / LDAP / OIDC integration.
- External identity group mapping to UniRouter roles.

PRD2 should reserve schema and interfaces for SSO/LDAP even if the first implementation ships after RBAC.

### D4: Provider Adapter Framework

Downstream remains pure OpenAI-compatible API.

PRD2 should add an upstream Provider Adapter Framework so UniRouter can support both:

- `openai_compatible`: current HTTP relay behavior, suitable for OpenAI-compatible upstreams.
- Native adapters: provider-specific request, response, streaming, usage, auth, and error mapping.

The architecture should be:

```text
Developer OpenAI SDK
  -> UniRouter /v1/chat/completions
  -> billing / auth / audit / routing
  -> provider adapter
  -> upstream native or OpenAI-compatible provider
```

P0:

- Introduce adapter interface and registry.
- Refactor current relay path into `openai_compatible` adapter semantics.
- Add `channels.adapter_type`.
- Normalize adapter output into common usage, error, latency, model, and metadata structures.
- Keep billing, logs, and circuit breaker dependent on normalized adapter result, not provider-specific payloads.

P1:

- Implement one native adapter as proof of framework. Candidate priority:
  1. `anthropic_native`, because streaming usage and event format differ materially.
  2. `gemini_native` or the first native provider required by a real enterprise customer.

Do not make downstream customers use Anthropic/Gemini/Baidu/Tongyi/Wenxin SDKs directly.

### D5: Model Governance Instead Of Model Marketplace

PRD2 should not show developers a long public marketplace-style list of hundreds of models.

Use two layers:

- Full Model Registry: admin-only source of truth for all available upstream models.
- Approved Model Catalog: developer-facing catalog containing only enterprise-approved models.

Recommended PRD2 model governance concepts:

| Concept | Purpose |
|---------|---------|
| Model Registry | Full internal model inventory |
| Approved Models | Models approved for enterprise use |
| Model Sets | Curated groups such as "customer-service-prod", "rag-embedding", "low-cost-test" |
| Project Allowed Models | Model sets assigned to projects |
| Model Alias | Stable enterprise names such as `company-chat-fast` or `company-embedding` |
| Model Lifecycle | `staging -> ga -> deprecated -> retired` |
| Migration Recommendation | Deprecated model points to replacement model or alias |

Developer default view should show recommended model sets and aliases first. Full search can exist for advanced users, but should not be the default product experience.

### D6: Prompt And Response Storage

PRD2 P0 should not store prompt or response plaintext by default.

P0 can store:

- request_id
- org_id
- project_id
- api_key_id
- model / alias / provider / channel
- token counts
- cost
- latency
- status and normalized error
- prompt hash and response hash if useful
- metadata needed for audit and troubleshooting

The next planning version must explicitly discuss whether optional prompt/response storage is needed, and under what controls:

- disabled by default
- admin opt-in
- retention window
- redaction
- encryption
- role-restricted access
- audit trail for every read

### D7: Token Metering V2

PRD2 should improve token metering and provider usage normalization.

Rationale: enterprise acceptance depends on billing and reconciliation credibility. PRD1 estimated token logic is acceptable for MVP, but PRD2 should reduce variance and make provider usage differences visible.

Candidate scope:

- Provider usage normalizer.
- Stream usage parser per adapter.
- Token counter strategy per model family where provider usage is missing.
- Difference report between estimated and upstream usage.
- Reconciliation metrics and alerts when estimated usage exceeds threshold.

### D8: High Availability Scope

PRD2 should improve deployment robustness without immediately turning UniRouter into a distributed platform.

P0:

- Make stateless multi-instance gateway operation explicit and testable.
- Ensure DB/Redis state owns shared mutable state.
- Add multi-instance smoke tests where practical.
- Document reverse proxy / load balancer deployment.

P1:

- HA deployment runbook for PostgreSQL, Redis, backup, restore, and failover.
- Capacity and chaos test evidence.

Defer full platform-level HA stack such as mandatory Redis Sentinel, PG read replicas, PgBouncer, or separate auth-service unless customer deployment requires it.

### D9: License

PRD2 should include a local-deploy license foundation, but keep it simple.

P1 candidate:

- Offline license file.
- Expiration and grace-period handling.
- License info page in Admin Console.
- Audit logs for license changes.
- Deployment ID / customer ID metadata.

Defer complex hardware fingerprinting, online activation, or a license marketplace.

### D10: Acceptance Discipline

PRD2 must start with acceptance design, not add it after implementation.

Every PRD2 feature should have:

- PRD item and explicit non-goals.
- Acceptance matrix row.
- Unit/integration tests.
- RBAC positive and negative tests where relevant.
- API route parity guard for frontend/API changes.
- Adapter mock upstream tests where provider behavior is involved.
- k6 or other capacity evidence for performance-sensitive items.
- Chaos or failure-mode evidence for billing, streaming, provider routing, DB/Redis, and high-availability claims.
- Codex to Canary handoff through `docs/agent-workflows/FIX-SIGNAL.md` when implementation or current-state docs are changed for verification.

## PRD2 Candidate Scope

### P0 Candidate Scope

P0 should be the minimum coherent PRD2 product surface:

1. Project / Workspace data model and migration to default project.
2. RBAC expansion for Admin Console and Developer Console.
3. Developer Console within the existing frontend application.
4. Project-scoped API keys and project-scoped usage.
5. Approved Model Catalog, Model Sets, Model Aliases, and model lifecycle.
6. Provider Adapter Framework with `openai_compatible` adapter semantics.
7. Token usage normalization foundation.
8. Audit schema upgrade with `actor_user_id + org_id + project_id + action + resource + before/after + ip + user_agent + request_id + source`.
9. PRD2 acceptance matrix, adapter mock tests, RBAC negative tests, route parity checks.

### P1 Candidate Scope

1. 2FA enforcement for privileged roles if not completed in P0.
2. SSO / LDAP / OIDC integration.
3. One native provider adapter.
4. Notification center and event system.
5. Project budget alerts and webhook routing.
6. License foundation.
7. HA deployment runbook and multi-instance smoke.
8. Optional prompt/response storage decision and design, if approved after discussion.

### P2 Candidate Scope

1. Playground integrated with approved model catalog and project keys.
2. Error-code documentation with `doc_url`.
3. Internal status page / incident history / maintenance windows.
4. Advanced model usage analytics and drill-down.
5. Exact or semantic cache research spike, not production default.
6. Additional native provider adapters based on customer demand.

## Explicitly Deferred

The following are not PRD2 development targets:

- Reseller /代理商体系.
- Public SaaS dual Portal.
- Payment, invoice, real-name verification, coupon, voucher, campaign.
- Cross-model fallback.
- Full intelligent routing across models.
- Full official SDK suite across languages.
- Complex semantic cache as a default runtime feature.
- Full support-ticket CRM.
- Marketing automation platform.
- Online license marketplace.
- Mandatory multi-service architecture split.

These can be placed in later roadmap planning after PRD2 validates enterprise governance, developer self-service, adapter extensibility, and billing credibility.

## PRD1 / PRD2 Document Governance

PRD1 and PRD2 must be managed as separate product phases.

### PRD1 Baseline

PRD1 should be treated as frozen historical baseline after closeout.

- `docs/PRD-Phase1.md` remains the PRD1 requirement source and should not receive new PRD2 requirements.
- PRD1 acceptance evidence should remain available, but should be clearly marked as PRD1 evidence.
- PRD1 items explicitly deferred by User should be marked as `Deferred to PRD2` or `Deferred to later roadmap`, not left as ambiguous PRD1 open bugs.
- PRD1 issue IDs should not be reused for PRD2.

If a future cleanup archives PRD1 issue details, do not bulk-delete files. Move or edit files deliberately according to the repository rules and User instructions.

### PRD2 Mainline

PRD2 should get its own formal documents:

- `docs/PRD-Phase2.md`
- `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`
- `docs/tech-ref/10-provider-adapter-framework.md`
- `docs/tech-ref/11-project-rbac-model.md`
- `docs/design/PRD2-DEVELOPER-CONSOLE-SPEC.md`

`docs/planning/PRD2-DISCOVERY.md` remains the planning and decision background. It is not a substitute for the formal PRD.

### Current Status Entry Point

`docs/ops/CURRENT-STATUS.md` should remain the single current-state entry point, but should explicitly distinguish:

- PRD1: closed / frozen / archived status.
- PRD2: planning / in-progress status.
- Active acceptance matrix for the current phase.
- Historical acceptance matrix for frozen phases.

### Issue Naming

PRD2 issue IDs should include the phase and area, for example:

- `PRD2-AUTH-001`
- `PRD2-PROJECT-001`
- `PRD2-ADAPTER-001`
- `PRD2-MODEL-001`
- `PRD2-BILLING-001`

`docs/issues/OPEN-ISSUES.md` can remain the active issue queue, but phase must be visible in IDs and titles.

## Sentinel GitHub Guidance

Sentinel should keep GitHub work small, phase-labeled, and evidence-backed.

### Pull Request Separation

Planning, acceptance, and implementation should be split when possible:

1. PRD2 planning PR: Discovery, formal PRD, acceptance matrix, and planning references.
2. PRD2 technical-spec PRs: adapter framework spec, Project/RBAC spec, Developer Console spec.
3. PRD2 implementation PRs: one coherent feature area per PR, such as Project/RBAC, Provider Adapter Framework, or Model Governance.

Do not mix PRD1 closeout fixes, PRD2 PRD writing, and PRD2 implementation code in one pull request unless User explicitly asks for that tradeoff.

### Branch Naming

Recommended branch names:

- `codex/prd2-discovery`
- `codex/prd2-prd`
- `codex/prd2-project-rbac`
- `codex/prd2-provider-adapter`
- `codex/prd2-model-governance`

### Pull Request Description Requirements

Each PRD2 pull request should state:

- Phase: PRD2.
- Scope and non-goals.
- Acceptance matrix rows affected.
- Canary verification result, if applicable.
- Schema migration impact.
- Backward compatibility impact.
- Rollback plan.

### Sentinel Merge Gate

Sentinel should advance GitHub integration only when:

- `docs/agent-workflows/FIX-SIGNAL.md` and `docs/agent-workflows/VERIFY-RESULT.md` refer to the same verification round, when the PR includes implementation or current-state changes requiring Canary verification.
- Canary result is PASS, or remaining items are explicitly accepted by User as non-blocking.
- `docs/issues/OPEN-ISSUES.md` has no blocker for the PR scope.
- The relevant PRD2 acceptance matrix rows are updated with status or evidence expectations.
- Schema changes include migration, compatibility, and rollback notes.

## Open Questions For Formal PRD

These are intentionally left open for `docs/PRD-Phase2.md` drafting:

1. Should prompt/response plaintext storage ever be supported, and under what controls?
2. Which native provider adapter is the first PRD2 proof point?
3. Should 2FA be P0 for all users or only privileged roles?
4. What is the exact Project migration rule for existing PRD1 API keys and call logs?
5. Should model aliases be mandatory for developer-facing models or optional?
6. What is the minimum Developer Console surface for the first PRD2 release?
7. Which PRD1 acceptance leftovers are formally closed, deferred, or converted into PRD2 acceptance rows?

## Next Deliverables

Recommended next documents:

1. `docs/PRD-Phase2.md`: formal product requirements.
2. `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`: evidence-backed acceptance matrix.
3. `docs/tech-ref/10-provider-adapter-framework.md`: adapter architecture and interface.
4. `docs/tech-ref/11-project-rbac-model.md`: Project/RBAC data and permission model.
5. `docs/design/PRD2-DEVELOPER-CONSOLE-SPEC.md`: Developer Console IA and UI scope.
