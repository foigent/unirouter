# PRD2 Completion Plan

> Purpose: track the remaining work required before PRD2 can be honestly closed as fully implemented and usable.
> Scope decision date: 2026-07-02.

## Completion Standard

PRD2 is not complete when implementation batches merely pass local or unit tests. PRD2 can close only after:

1. Every PRD2 P0/P1/P2 item in `docs/PRD-Phase2.md` is either implemented and accepted, or explicitly recorded as a user-approved later-phase item.
2. User-confirmed P2 items are included: P2-F13 Playground/docs integration, P2-F14 Advanced Usage Analytics, and P2-F15 Cache Research.
3. Acceptance evidence follows `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`: exact command or manual path, environment, expected result, actual result, and timestamp or commit/worktree reference.
4. Browser-dependent product workflows are verified against a real backend, not only component tests or mocked browser data.
5. Provider-dependent workflows have real upstream evidence where credentials are available, and any credential-blocked gap is clearly marked.
6. `docs/issues/OPEN-ISSUES.md` has no PRD2 blocker or unclassified PRD2 open item left.
7. The final round completes the Codex -> Canary -> Sentinel -> Atlas protocol in `docs/agent-workflows/AGENT-PROTOCOL.md`.

## Confirmed Product Decisions

| Decision | Current Direction | Tracking |
|----------|-------------------|----------|
| P2-F13/F14/F15 | All are mandatory for complete PRD2 scope | Matrix P2 rows remain `Planned` until implemented/accepted |
| Cache | Complete PRD2 scope means a cache research spike, not default runtime cache | P2-F15 |
| SSO | OIDC first is mandatory, but later in the remaining plan | `PRD2-AUTH-001` |
| Prompt/response storage | Default no plaintext storage; optional storage needs formal design and user approval before implementation | P2-F11 |
| Developer key creation | `developer` can create and manage permitted project keys by default | P2-F03/P2-F01 |
| Project budget | PRD2 first release remains alert-only; hard-blocking budget enforcement is a later item | `PRD2-BUDGET-001` |
| HA validation | Staging/docker-compose two-instance validation first; production HA remains not fully implemented until target evidence exists | `PRD2-HA-001` |
| Claude via OpenRouter | Valid for `openai_compatible` Claude E2E, not a substitute for direct `anthropic_native` protocol E2E | P2-F05/native adapter proof |

## Why Cache Is Research-Only In PRD2

`docs/PRD-Phase2.md` defines P2-F15 as a research spike and says runtime cache must not be enabled by default. This is intentional:

- Prompt and response caching intersects with P2-F11 storage policy, privacy, retention, encryption, and read audit.
- Semantic cache can leak information across org/project/key/model boundaries unless isolation rules are designed and verified.
- Cached responses can become stale or incorrect, which changes auditability and enterprise responsibility.
- Billing becomes ambiguous unless cache-hit pricing, provider-cost evidence, and usage records are defined.
- Streaming, tool calls, vision payloads, system prompts, temperature, and model alias changes make cache correctness non-trivial.

PRD2 completion therefore requires a research note with risks, constraints, and an optional future architecture. Runtime cache implementation needs a separate user approval.

## Why Prompt/Response Storage Defaults To Off

UniRouter routes potentially sensitive enterprise prompts and responses. Storing plaintext by default would turn the gateway into a sensitive-content repository. The PRD2-safe policy is:

- no plaintext prompt/response storage by default;
- optional org/project-level enablement only after user approval;
- encrypted storage if enabled;
- retention controls;
- RBAC on every read;
- audit event for every read;
- redaction guidance before exposing content to support or admin users.

Until this policy is approved, request troubleshooting must use metadata only: request id, project, key prefix, model/alias, status, tokens, cost, latency, provider family, channel display name where allowed, normalized error, and docs link.

## Remaining Execution Batches

### Batch 16: Real Backend Browser And Developer Console E2E

Goal: prove the P0 developer path works against a real backend.

Required work:

- Add real-backend browser smoke for login, project selector, project overview, project API keys, approved models, usage, request lookup, and docs entry.
- Verify a developer can create a project key, copy plaintext once, call `/v1/chat/completions`, and then see the resulting project usage/log evidence.
- Verify direct Admin Console URLs are blocked for developer users.
- Verify request lookup does not expose provider keys, full API keys, prompt plaintext, response plaintext, or other-project data.
- Verify cross-project negative cases through API and browser paths where practical.

Target rows:

- P2-F01 Project / Workspace frontend flow and usage/log drill-down.
- P2-F02 real-browser RBAC navigation.
- P2-F03 Developer Console MVP.
- Request troubleshooting restricted-data behavior.

Status update on 2026-07-02:

- Codex implemented the Batch 16 real-backend browser smoke and supporting seed command:
  - `scripts/prd2-developer-console-smoke.sh`
  - `scripts/prd2_developer_console_seed.go`
  - `scripts/prd2_developer_console_smoke.mjs`
- Local environment used for evidence: current-code backend on `http://localhost:18080`, existing Vite frontend on `http://localhost:3000`, local PostgreSQL/Redis, Playwright Chromium, and a script-owned mock OpenAI-compatible upstream.
- Local command passed:

```bash
set -a; source .env.production; set +a; \
UNIROUTE_SMOKE_BACKEND_URL='http://localhost:18080' \
UNIROUTE_SMOKE_FRONTEND_URL='http://localhost:3000' \
UNIROUTE_SMOKE_SKIP_BROWSER_INSTALL=true \
scripts/prd2-developer-console-smoke.sh
```

- Actual result: `status=PASS`, `request_id=batch16-browser-1782957796339`, `model_alias=company-chat`, `upstream_calls=1`, screenshot `output/playwright/prd2-batch16-developer-console.png`.
- Defects found and fixed during this batch:
  - Developer Console crashed on array `capability_tags`/`aliases` from the model catalog; fixed by accepting array or CSV values.
  - Request Lookup failed on successful logs with NULL `upstream_error`; fixed by making `GetLogDetail` use `COALESCE(upstream_error, '')` and adding `TestGetLogDetail_NullSafeUpstreamError`.
- Current protocol state: fixed locally by Codex and awaiting Canary verification.

### Batch 17: Model Governance And Usage/Metering Completion

Goal: make model governance and usage evidence usable through the product UI.

Required work:

- Verify Model Registry create/edit/list and Model Set assignment against a real backend.
- Verify Approved Catalog hides unapproved models with fixtures visible in Developer Console.
- Verify deprecated migration recommendation is visible in the developer catalog.
- Verify retired models remain blocked through gateway.
- Add usage-source filtering to frontend reconciliation/usage surfaces where missing.
- Move estimated-vs-upstream variance from page-scoped evidence toward a full-query aggregate where required by product UX.
- Wire high estimated-usage ratio into notification/dashboard evidence.

Target rows:

- P2-F04 Model Governance.
- P2-F06 Token Metering V2.
- P2-F12 acceptance discipline for matrix upgrades.

### Batch 18: Provider Adapter Real Upstream Acceptance

Goal: separate OpenRouter Claude E2E from direct Anthropic native protocol E2E.

Required work:

- Batch 18A: configure Claude through OpenRouter as `openai_compatible` and run non-stream plus stream gateway E2E.
- Batch 18B: configure direct Anthropic credentials for `anthropic_native` and run non-stream plus stream protocol E2E when credentials are available.
- Verify normalized usage, error, metadata, stream final `_uniroute`, and missing-usage fallback behavior.
- Verify provider-specific 401/429/5xx classification with real or controlled upstream behavior where practical.

Acceptance boundary:

- OpenRouter Claude validates real Claude model access through UniRouter's OpenAI-compatible adapter path.
- OpenRouter does not replace direct Anthropic Messages API validation for `anthropic_native`, because OpenRouter exposes a normalized OpenAI-compatible API.

Target rows:

- P2-F05 Provider Adapter Framework.
- Native adapter proof.
- P2-F06 real upstream usage evidence.

### Batch 19: Notification/Event And Project Budget Completion

Goal: make notification events visible, retryable, and production-verifiable.

Required work:

- Add frontend notification center for events, delivery state, and webhook routes.
- Add background retry worker for failed webhook deliveries.
- Wire remaining emitters: key disabled/revoked, model deprecated/retired, provider/channel failure, license warning, and high estimated-usage ratio.
- Add project budget browser/API E2E.
- Add period-boundary integration test for daily/monthly budgets.
- Produce production or production-like webhook delivery evidence.

Target rows:

- P2-F08 Notification/event foundation.
- Project budget alerts.
- License warning event dependency for P2-F10.

### Batch 20: Audit Completion And PRD2 Hardening Issues

Goal: close known PRD2 audit/migration/governance hardening gaps.

Required work:

- Add frontend audit filters for org, project, actor, action, resource, time range, and source.
- Add real JWT/API E2E for audit filters and before/after responses.
- Add richer channel create/update/delete before/after audit details.
- Add `channel.test` audit event for success/failure without leaking provider keys.
- Decide and implement/document historical audit `project_id` behavior.
- Decide rollback fidelity for migration 000012 `usage_source` normalization.
- Keep PRD1 compatibility for unregistered models while adding admin/developer warning, audit, or notification signal unless user later chooses strict blocking.

Target rows/issues:

- P2-F07 Audit schema upgrade.
- `PRD2-MIG-001`.
- `PRD2-MIG-002`.
- `PRD2-GOV-001`.
- `PRD2-AUDIT-001`.

### Batch 21: HA And Deployment Validation

Goal: prove two-instance readiness in a safe environment and record production gaps honestly.

Required work:

- Run staging/docker-compose two-instance smoke against shared PostgreSQL/Redis.
- Verify health, gateway call, idempotency, wallet safety, rate-limit/cooldown shared state, and rollback steps.
- Verify load-balancer/proxy routing evidence where the target environment provides it.
- Run backup/restore rehearsal in a safe non-production environment.
- Preserve `.env`/`.env.production` and record post-deploy API key smoke output.

Target rows/issues:

- P2-F09 HA readiness.
- `PRD2-HA-001`.
- `PRD2-DEPLOY-001` archive readiness after operator confirmation.

### Batch 22: License Production Acceptance

Goal: move License Foundation from unit-tested foundation to real-backend/production accepted.

Required work:

- Provide or generate a test offline license and matching `LICENSE_PUBLIC_KEY`.
- Run real-backend browser smoke for `/license`.
- Verify missing, invalid, active, expiring, and expired statuses where practical.
- Verify production or approved staging license import/status evidence.
- Verify `license.update` audit summary and no raw signature leakage.
- Verify license warning event after Batch 19 emitter support exists.

Target rows:

- P2-F10 License foundation.
- Notification dependency for license warning.

### Batch 23: P2 Product Features

Goal: implement the user-confirmed mandatory P2 product scope.

Required work:

- P2-F13 Playground integrated with approved catalog:
  - project-scoped keys only;
  - no shared platform key exposure;
  - approved model aliases only;
  - stream output, request id, cost, tokens, latency, and copyable code;
  - no prompt/response history by default.
- Error docs with `doc_url`:
  - normalized error schema update;
  - docs pages for common gateway/RBAC/model/billing/provider errors;
  - API/browser tests.
- Internal status page / incident history:
  - status schema/API/UI;
  - incident list and timeline;
  - access control.
- P2-F14 Advanced Usage Analytics:
  - drill-down by org, project, key, model, alias, provider, channel, status, and usage_source;
  - CSV export respecting role scope;
  - estimated usage ratio visibility.
- P2-F15 Cache Research:
  - exact cache and semantic cache comparison;
  - privacy, billing, audit, correctness, tenant isolation, and invalidation analysis;
  - future opt-in architecture recommendation;
  - no runtime cache enabled by default.

Target rows:

- P2-F13.
- Error docs with `doc_url`.
- Internal status page / incident history.
- P2-F14.
- P2-F15.

### Batch 24: OIDC First Authentication Integration

Goal: implement the user-confirmed SSO path after core PRD2 product completion.

Required work:

- Add OIDC provider configuration.
- Implement login initiation and callback.
- Map OIDC identity claims to UniRouter users/orgs/roles.
- Define first-login provisioning rules.
- Add RBAC mapping tests and negative tests.
- Add operator docs for provider setup.

Boundary:

- OIDC is first.
- LDAP/SAML are not first unless user later expands scope.

Target issue:

- `PRD2-AUTH-001`.

### Batch 25: Final PRD2 Closeout

Goal: close the phase only after all preceding batches are accepted.

Required work:

- Reconcile every matrix row to `E2E accepted`, `Production verified`, `Deferred`, or `Out of scope`.
- Ensure no PRD2 `Open` or `Fixed awaiting Canary` issues remain without explicit user decision.
- Run global backend/frontend tests and browser smoke.
- Run final Canary verification.
- Sentinel merges final closeout PR.
- Atlas deploys and records final target-environment evidence.

## External Inputs Needed Later

| Input | Needed For | Can Proceed Without It? |
|-------|------------|--------------------------|
| OpenRouter API key and Claude model slug | Batch 18A | Yes, but only mock/OpenAI-compatible local evidence can be recorded |
| Direct Anthropic API key | Batch 18B | No, direct native E2E remains open without it |
| Test offline license and public key | Batch 22 | Can generate local/staging sample, but production evidence needs target key/license |
| Webhook receiver endpoint | Batch 19 | Can use local receiver first; production evidence needs target endpoint |
| Staging/two-instance target | Batch 21 | Local docker-compose can start first |
| OIDC provider details | Batch 24 | Can design first, implementation needs provider metadata/client credentials |

## Agent Loop

Every implementation batch follows:

1. Codex implements and updates `docs/agent-workflows/FIX-SIGNAL.md`.
2. Canary reads `FIX-SIGNAL.md`, verifies independently, and overwrites `docs/agent-workflows/VERIFY-RESULT.md`.
3. If Canary fails, Codex fixes and repeats.
4. If Canary passes, Sentinel handles PR/CI/merge.
5. Atlas deploys and verifies target-environment evidence where the batch needs deployment.
