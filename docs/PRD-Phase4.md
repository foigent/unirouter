# UniRouter Product Requirements Document - Phase 4

> Version: Phase 4 Scope Draft
> Date: 2026-07-07
> Status: Active PRD4 implementation; Batch 1N Developer Console Self Usage
> And Model Cost passed Canary 9/9 and Sentinel merge is reflected by
> `6194062` / PR #98. Batch 1O API Key Input And Security Header Hardening is
> fixed awaiting Canary for `P4-F12` / `PRD4-SECURITY-001`.
> Batch 1H real upstream and Batch 1I real SSO evidence harnesses remain below
> `E2E accepted` / `Production verified` until approved target credentials and
> successful target-environment runs exist.
> Source: `docs/planning/PRD4-BACKLOG.md`, `docs/ops/CURRENT-STATUS.md`,
> `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`,
> `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`, current repo/code review, and User
> PRD4 scope decisions on 2026-07-07.
> Purpose: define PRD4 as the commercial observability and control foundation for
> UniRouter as a local-deployable enterprise MaaS gateway.

---

## 1. Product Positioning

PRD4 turns the frozen PRD2 product and completed PRD3 cleanup baseline into a
more commercially operable MaaS control plane.

Core product sentence:

> UniRouter PRD4 adds trustworthy usage/cost visibility, request-flow evidence,
> runtime observability, and target-environment proof for real upstream and real
> SSO, while keeping high-risk runtime plugin, scheduler, IAM rewrite, cache, SDK,
> and full IaC expansion out of the first implementation phase.

PRD4 should make these questions answerable without requiring operators to inspect
raw database rows or multiple monitoring tools first:

- Who used which API key, project, and model?
- How many requests and tokens were consumed?
- How much did that usage cost by model, key, user, project, and time window?
- What path did a request take through UniRouter?
- Which runtime dependency or capacity dimension is currently degraded?
- Which real upstream and real SSO paths are target-environment verified?

## 2. Scope Principle

PRD4 may:

- add API key owner usage and cost attribution;
- add quota terminology and key-level quota foundations for RPM, RPS, TPM, and
  tokens/s visibility;
- add a Flow/Event/Trace foundation built around `request_id` and future
  `trace_id`;
- define PlungeIn-style extension ports that are observation-first and do not
  mutate core billing, auth, routing, or policy decisions in PRD4;
- add a Status Center that separates event status, runtime environment, capacity
  health, and observability integrations;
- display CPU, memory, disk, network, DB, Redis, Prometheus, and Grafana health
  summaries with role-based redaction;
- add target-environment evidence tracks for real upstream adapters and real
  enterprise SSO;
- add password visibility toggles to current password inputs;
- promote PRD5 backlog boundaries for larger deferred capabilities.

PRD4 must not:

- implement a general runtime plugin platform, plugin marketplace, arbitrary code
  loading, plugin sandbox, or third-party install path;
- implement full Scheduler V2 with model-family queues, reservations, GPU batch,
  or priority dispatch;
- rewrite Logto or another identity platform into UniRouter core;
- implement public SaaS, reseller, payment, invoice, coupon, campaign, support
  CRM, or marketplace operations;
- make runtime cache or prompt/response plaintext storage enabled by default;
- turn budget alerts into hard admission blocking without a separate approved
  policy and billing/idempotency safety design;
- claim Production verified status from mock, local-only, or undocumented target
  evidence;
- expose raw host topology, private IPs, VPC/subnet details, security groups,
  cloud account identifiers, full hostnames, prompts, responses, full API keys,
  upstream credentials, or raw IdP tokens in user-visible views.

## 3. PRD4 Scope Summary

### P0

| ID | Workstream | Summary |
|----|------------|---------|
| P4-F01 | PRD4 Scope And Evidence Baseline | Create the formal PRD4 scope, acceptance matrix, PRD5 backlog, and architecture ADRs without runtime code changes |
| P4-F02 | Usage And Cost Attribution | Show each user their API-key-owner usage and cost, including model-level cost breakdown |
| P4-F03 | Quota Vocabulary And Key Quota Foundation | Define RPM/RPS/TPM/tokens-per-second semantics and add key-level quota direction without full scheduler behavior |
| P4-F04 | Flow/Event/Trace Foundation | Add request-flow evidence semantics across gateway, auth, billing, routing, provider, logging, and audit paths |
| P4-F05 | PlungeIn Port Architecture | Define extension ports for observe/act/enrich/decide/mutate and keep PRD4 ports observation-first |

### P1

| ID | Workstream | Summary |
|----|------------|---------|
| P4-F06 | Status Center And Runtime Observability | Add a unified internal Status Center for event status, runtime environment, capacity health, and observability integrations |
| P4-F07 | External Monitoring Integration | Integrate with existing customer Prometheus/Grafana setups through summaries, health checks, configuration, and deep links |
| P4-F08 | Real Upstream Evidence Track | Run target-environment E2E for OpenRouter-compatible and direct Anthropic-native adapter paths when credentials are provided |
| P4-F09 | Real SSO Evidence Track | Run target-environment browser login evidence against Logto or another approved enterprise OIDC provider |
| P4-F10 | Auth UX Hardening | Add visible/hidden password controls for login, password change, and admin password entry surfaces |

### P2

| ID | Workstream | Summary |
|----|------------|---------|
| P4-F11 | Production Evidence Promotion | Promote license, webhook delivery, and selected operational evidence only when approved target environments exist |
| P4-F12 | Security, Privacy, And Redaction Guardrails | Apply RBAC, redaction, retention, and no-prompt/no-secret rules to usage, trace, and runtime observability |
| P4-F13 | PRD5 Backlog Consolidation | Move scheduler, plugin marketplace, IAM rewrite, full IaC, runtime cache, full SDK, new roles, and public SaaS scope to PRD5 |

## 4. Functional Requirements

### P4-F01: PRD4 Scope And Evidence Baseline

Requirements:

- Create `docs/PRD-Phase4.md` as the formal PRD4 scope contract.
- Create `docs/ops/PRD4-ACCEPTANCE-MATRIX.md` with planned rows, evidence
  requirements, and target-environment boundaries.
- Create `docs/planning/PRD5-BACKLOG.md` for explicitly deferred capabilities.
- Create ADRs for PRD4 PlungeIn ports, usage attribution, and runtime
  observability.
- Update doc entrypoints so future agents do not treat the old PRD4 candidate
  backlog as the only planning surface.

Acceptance:

- The PRD4 formal scope, PRD4 matrix, PRD5 backlog, and ADRs exist and are linked
  from current docs.
- The docs state that no runtime code, migrations, frontend behavior, API routes,
  or deployment behavior changed in the scope-baseline batch.
- The PRD5 backlog includes the major deferred items and explains why they are not
  PRD4 first-batch work.

### P4-F02: Usage And Cost Attribution

Requirements:

- Define per-user usage as API key owner usage for PRD4.
- Associate API keys with an owner user and preserve ownership at request time so
  historical usage does not move when key ownership changes.
- Distinguish personal keys, project shared keys, and service keys.
- Show the current user their own request count, input/output/total tokens,
  cost, model breakdown, project breakdown, API key breakdown, latency, error
  rate, and `usage_source`.
- Let authorized project/org/platform roles view broader project/org usage
  according to existing RBAC.
- Keep prompt and response plaintext out of usage views.

Acceptance:

- A user with personal keys can see only their own key-owner usage unless a higher
  role grants broader scope.
- Model-level cost totals reconcile to the user's total cost for the selected
  time window.
- Shared/service keys are labeled so they are not falsely presented as personal
  consumption.
- Historical usage remains tied to the request-time owner snapshot.

### P4-F03: Quota Vocabulary And Key Quota Foundation

Requirements:

- Use RPM for requests per minute and RPS for requests per second.
- Use TPM for tokens per minute and explicit `tokens/s` for token throughput.
- Avoid using TPS in product UI because it is ambiguous between transactions per
  second and tokens per second.
- Preserve current QPS/RPM compatibility while documenting product terms.
- Define key-level RPM/RPS/TPM/budget visibility before implementing full
  scheduler behavior.
- Track quota changes with operator, reason, before/after snapshot, and
  idempotency key when implementation begins.

Acceptance:

- User-facing docs and UI labels consistently use RPM, RPS, TPM, and tokens/s.
- Quota usage and limit values are visible by key and time window.
- Full Scheduler V2 behavior is not implied by PRD4 quota foundation work.

Batch 1E implementation note: the first slice standardizes user-facing
RPM/RPS/TPM/tokens/s vocabulary, adds preferred `rps_limit` and structured key
`quota` response data, and documents that existing `qps_limit` storage remains a
compatibility boundary. Scheduler V2, key-level TPM enforcement, budget
hard-blocking, quota audit reason/idempotency fields, and schema migration remain
follow-up work. User reported Canary/Sentinel completed after `a1acdb6` / PR
#88.

### P4-F04: Flow/Event/Trace Foundation

Requirements:

- Treat `request_id` as the minimum correlation key across call logs, billing,
  audit, reconciliation, and request troubleshooting.
- Define future `trace_id`/span semantics without requiring OpenTelemetry in the
  first PRD4 batch.
- Record high-level request stages: gateway received, auth checked, budget
  prechecked, billing frozen, model resolved, channel selected, provider called,
  usage parsed, billing settled or cancelled, call log persisted, audit written,
  and response returned.
- Capture timing fields such as auth, route, upstream, TTFT, settle, and log write
  lag where implementation safely supports them.
- Do not store prompts, responses, full API keys, upstream secrets, or raw tokens
  in trace data.

Acceptance:

- A request can be investigated from one ID across gateway, billing, logs, and
  audit metadata.
- Project-scoped users do not see restricted provider credentials, channel keys,
  hidden topology, prompt plaintext, or response plaintext.
- Missing trace data is represented as unavailable or partial, not fabricated.

### P4-F05: PlungeIn Port Architecture

Requirements:

- Define PlungeIn as a UniRouter architecture pattern, not as a dependency on an
  external framework.
- Split extension points into five ports: Observe, Act, Enrich, Decide, and
  Mutate.
- PRD4 may implement or document Observe, Act, and limited Enrich ports.
- PRD4 must not expose Mutate ports for changing auth, billing, routing, wallet,
  or settlement decisions.
- Decide ports may be advisory in PRD4, but enforcement belongs to a later
  explicit policy phase.

Acceptance:

- ADR documentation defines which ports are allowed in PRD4 and which are PRD5.
- Trace/observability work can plug into the Observe port without becoming a
  general plugin runtime.
- Core billing/auth/routing correctness remains owned by UniRouter core code.

Batch 1F implementation note: the accepted ADR now points to
`docs/tech-ref/22-prd4-plungein-port-contract.md`, which defines concrete
Observe, Act, limited Enrich, advisory Decide, and forbidden Mutate boundaries.
This is documentation-only and does not add runtime plugin loading, manifests,
sandboxing, marketplace installation, or third-party code execution.

### P4-F06: Status Center And Runtime Observability

Requirements:

- Keep current event-derived Internal Status semantics intact.
- Add a broader Status Center information architecture:
  - Event Status;
  - Runtime Environment;
  - Capacity Health;
  - Observability Integrations;
  - Maintenance Windows as future or disabled unless implemented.
- Show CPU, memory, disk, network, DB, Redis, request throughput, token
  throughput, latency, error rate, and dependency health as summaries.
- Define freshness, missing-data, stale-data, and degraded-data behavior.
- Restrict raw infrastructure details to platform/ops roles.

Acceptance:

- Operators can see a single summary view without opening Prometheus, Grafana,
  database tooling, and raw logs first.
- Prometheus or Grafana being unavailable does not break the main app; the status
  shows unknown or stale.
- Raw host topology and cloud identifiers are hidden unless the role and workflow
  explicitly require them.

Implementation notes:

- Batch 1D adds the separate Runtime Observability surface, app memory,
  DB/Redis health, recent traffic summary, and Prometheus/Grafana configuration
  state.
- Batch 1G adds active Prometheus/Grafana health checks.
- Batch 1J adds host CPU/disk/network summaries without exposing raw topology,
  private IPs, hostnames, or cloud identifiers.
- Cluster inventory, external collector ownership, OpenTelemetry, metric-alert
  routing, and embedded dashboards remain follow-up work.

### P4-F07: External Monitoring Integration

Requirements:

- Support customers who already have Prometheus, Grafana, or another monitoring
  platform.
- Do not duplicate full Grafana dashboards in the UniRouter UI.
- Provide summary health, configured datasource state, scrape freshness, and
  links to external dashboards where configured.
- Keep metrics authentication and trusted proxy policy explicit.

Acceptance:

- A configured external monitoring platform is visible as connected, stale,
  misconfigured, or unavailable.
- Operators have deep links to their own dashboards when configured.
- Metrics exposure and monitoring API access follow RBAC and network policy.

Batch 1G implementation note: the first slice keeps UniRouter as the status
center, not a monitoring replacement. `GET /api/v1/runtime/status` actively
checks configured Prometheus and Grafana base URLs, reports Prometheus readiness
and `up{job="unirouter"}` scrape state, reports Grafana `/api/health`, and keeps
the configured URLs as external deep links. It does not embed dashboards, add
new metrics authentication, validate Grafana datasources, or implement a
third-party monitoring connector framework.

### P4-F08: Real Upstream Evidence Track

Requirements:

- Run target-environment E2E for OpenRouter-compatible and direct
  Anthropic-native adapter paths after approved credentials are provided.
- Cover non-stream and stream flows where provider support allows.
- Verify request success, error classification, usage source, billing settlement,
  request log, and no secret leakage.
- Keep provider cost risk explicit and controlled.

Acceptance:

- Evidence records target environment, commit, config shape, provider/model slug,
  test timestamp, expected result, actual result, and rollback/cleanup.
- OpenRouter-compatible evidence does not substitute for direct Anthropic-native
  evidence.
- Failed provider auth or rate limits are classified and documented rather than
  hidden.

Batch 1H implementation note: the first slice adds guarded target-evidence
tooling, not a real-provider acceptance claim. The harness includes
`scripts/prd4-real-upstream-smoke.sh --preflight`,
`scripts/prd4_real_upstream_seed.go`,
`scripts/prd4_real_upstream_smoke.mjs`, and
`docs/tech-ref/23-prd4-real-upstream-evidence.md`. Preflight mode does not
mutate the database or call providers. Real mode requires approved OpenRouter
and direct Anthropic credentials/model slugs, matching backend secrets, and
`UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION=approved-target-real-upstream`. `P4-F08`
does not move to `E2E accepted` or `Production verified` until both adapter
paths pass real-provider non-stream/stream checks in an approved target
environment.

### P4-F09: Real SSO Evidence Track

Requirements:

- Use Logto or another approved enterprise OIDC provider as target-environment
  SSO evidence.
- Verify discovery/JWKS, signed state/nonce, callback, claim mapping, user
  provisioning, role/org behavior, audit event, and browser login.
- Keep Logto as an external or optional deployment component, not a rewritten
  UniRouter core module in PRD4.

Acceptance:

- Browser login passes against the approved real IdP.
- Claim mapping behavior is documented and tested for allowed and denied users.
- No IdP token, client secret, password, or raw profile secret appears in logs,
  audit, frontend state, screenshots, or evidence files.

Batch 1I implementation note: the first slice adds guarded target-evidence
tooling, not a real-IdP acceptance claim. The harness includes
`scripts/prd4-real-sso-smoke.sh --preflight`,
`scripts/prd4_real_sso_smoke.mjs`,
`scripts/prd4_real_sso_audit_check.go`, and
`docs/tech-ref/24-prd4-real-sso-evidence.md`. Preflight mode does not drive a
browser, call the IdP, or read the database. Real mode requires approved Logto
or enterprise OIDC issuer/test-user access, console/backend URLs, DB DSN, and
`UNIROUTE_PRD4_SSO_ALLOW_BROWSER=approved-target-real-sso`. `P4-F09` does not
move to `E2E accepted` or `Production verified` until browser login,
claim-mapping, and `login.oidc` audit evidence pass in an approved target
environment.

### P4-F10: Auth UX Hardening

Requirements:

- Add visible/hidden password controls to login, password change, and admin user
  password entry surfaces.
- Password fields default to hidden.
- Visibility state is local to the input and is not persisted.
- Controls must be keyboard-accessible and screen-reader labeled.

Acceptance:

- Tests cover hidden-by-default and toggle behavior.
- No password value is logged or copied into evidence.
- The feature does not change authentication semantics.

### P4-F11: Production Evidence Promotion

Requirements:

- Treat production or approved-staging evidence as separate from local E2E.
- Candidate tracks include license import/status, webhook delivery, real upstream,
  real SSO, and selected HA/DR smoke evidence.
- Each evidence track needs explicit environment, credentials owner, cost/risk
  note, command/manual path, expected result, actual result, and rollback.

Acceptance:

- A matrix row only moves to Production verified when target-environment evidence
  exists.
- Local-only or mock-only smoke remains E2E accepted or lower.

### P4-F12: Security, Privacy, And Redaction Guardrails

Requirements:

- Define a redaction policy for usage, trace, runtime observability, SSO, upstream,
  and evidence outputs.
- Keep prompt/response plaintext disabled unless a later opt-in product scope is
  approved.
- Hide full API keys, upstream credentials, tokens, private topology, raw IdP
  secrets, and cloud account identifiers.
- Define trace/event retention and sampling before storing high-volume traces.

Acceptance:

- Tests or evidence prove restricted roles cannot access restricted trace,
  provider, topology, or secret fields.
- Evidence files do not contain secrets.
- Missing or redacted fields are represented intentionally, not as empty bugs.

Implementation notes:

- Batch 1L passed Canary 8/8 and Sentinel merge is reflected by `4426efd` / PR
  #96 for the first runtime monitoring redaction slice: runtime status display
  URLs for Prometheus/Grafana strip URL userinfo, query strings, and fragments
  while probes continue to use raw configured URLs.

### P4-F13: PRD5 Backlog Consolidation

Requirements:

- Maintain `docs/planning/PRD5-BACKLOG.md` as the explicit backlog for deferred
  capabilities.
- Record why each major item is deferred and what prerequisite would make it
  eligible.

Acceptance:

- PRD4 acceptance matrix links to the PRD5 backlog.
- Deferred items are not silently implemented or implied by PRD4 UI labels.

## 5. Recommended Batch Plan

| Batch | Scope | Runtime Code? | Notes |
|-------|-------|---------------|-------|
| PRD4 Batch 0 | Formal PRD4 scope, PRD4 matrix, PRD5 backlog, ADRs, docs routing | No | Requires Canary because it is acceptance-sensitive documentation |
| PRD4 Batch 1 | API key owner usage/cost MVP, model-level cost, password visibility toggles | Yes | Lowest-risk product value after scope baseline |
| PRD4 Batch 2 | Flow/Event/Trace foundation and Observe port | Yes | Start with metadata-only request path evidence |
| PRD4 Batch 3 | Status Center runtime observability summary and external monitoring integration | Yes | Keep event status separate from runtime/capacity health |
| PRD4 Batch 4 | Real upstream evidence track | Mostly evidence/scripts | Requires approved provider credentials |
| PRD4 Batch 5 | Real SSO evidence track | Mostly evidence/scripts | Requires approved Logto or enterprise OIDC target |
| PRD4 Batch 6 | Production evidence promotion and PRD4 closeout | Docs/evidence | Promote only verified target-environment rows |

## 6. Dependencies And Open Decisions

- Approved real upstream credentials and model slugs are required for P4-F08.
- Approved Logto or enterprise OIDC issuer/client credentials are required for
  P4-F09.
- The first implementation batch should confirm whether shared/service API keys
  are shown under a separate key type rather than a personal user total.
- Batch 1K passed Canary 10/10 for the first trace retention/cleanup slice;
  high-volume production storage still needs partitioning, archival, and
  capacity policy.
- Status Center implementation must choose telemetry sources: app runtime,
  Prometheus, node exporter, cloud API, deployment manifest, or operator metadata.

## 7. Explicit PRD5 Boundary

The following are not PRD4 implementation targets unless the User explicitly
reopens scope:

- full Scheduler V2;
- runtime plugin platform and marketplace;
- Logto/IAM rewrite into UniRouter core;
- full Kubernetes/Helm/Terraform managed-HA product;
- project budget hard-block enforcement;
- runtime cache or prompt/response plaintext storage;
- full SDK suite;
- new persistent role system;
- public SaaS, reseller, payment, invoice, campaign, marketplace, and support CRM.

Detailed PRD5 candidates live in `docs/planning/PRD5-BACKLOG.md`.
