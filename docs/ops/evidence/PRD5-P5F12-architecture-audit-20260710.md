# PRD5 P5-F12 Architecture Audit And Screening

> Date: 2026-07-10
> Repository baseline at start: `935e64e45463c11be49e83d412cf49f1ab77b843`
> Scope: repository-wide architecture, function, technical debt, UI/UX, data,
> deployment, and security audit before continuing PRD5 implementation.
> Execution boundary: no deployment, no real upstream request, no credential
> change, no ECS mutation, no broader export, and no PRD4 reopen.

## Evidence Read

- PRD5/ops first-read set: `docs/PROJECT-HANDOFF.md`,
  `docs/ops/CURRENT-STATUS.md`, `docs/planning/PRD5-CURRENT-GAP-AUDIT-20260710.md`,
  `docs/PRD-Phase5.md`, `docs/ops/PRD5-ACCEPTANCE-MATRIX.md`,
  `docs/planning/PRD5-IMPLEMENTATION-PLAN.md`,
  `docs/planning/PRD5-BACKLOG.md`,
  `docs/planning/PRD5-COMPETITOR-COMPARISON.md`,
  `docs/issues/OPEN-ISSUES.md`, `docs/agent-workflows/AGENT-PROTOCOL.md`,
  `docs/ops/evidence/PRD5-B1W-target-rerun-20260710.md`, and
  `/Users/m4/Desktop/projects/X-Insight/README.md`.
- Supporting source map and current-entry docs:
  `docs/CODEBASE-MAP.md`, `docs/README.md`, `docs/DOC-INVENTORY.md`, and
  `docs/agent-workflows/EXECUTION-RECORD.md`.
- Code paths audited: `cmd/uniroute`, `internal/admin`, `internal/gateway`,
  `internal/config`, `internal/middleware`, `migrations`, `frontend/src`, CI,
  Dockerfiles, and Compose.

## Repository-Wide Audit Findings

### Confirmed Findings

| ID | Priority | Area | Finding | Evidence | Action |
|---|---|---|---|---|---|
| INFRA-SEC-001 | P0 | Deployment/Security | Current development/test ECS exposure is unsafe for any production-like claim. The target evidence says the security group allows all TCP from `0.0.0.0/0`, public DB/Redis/monitoring/app ports, unsolicited inbound reachability, SSH timeout, and mutable rsync deployment state. | `docs/ops/evidence/PRD5-B1W-target-rerun-20260710.md:111-122`; Compose exposes local DB/Redis/app/Prometheus/Grafana host ports at `docker-compose.yml:11-12`, `docker-compose.yml:24-25`, `docker-compose.yml:42-59`, `docker-compose.yml:120-140`; metrics are enabled by default at `internal/config/config.go:226-227` and served at `cmd/uniroute/main.go:197-203`. | Do not mutate ECS in this batch. Use the INFRA-SEC plan below and require explicit target authorization before cutover. |
| PRD5-LOGS-001 | P1 | Function/UI | Logs UI exposed status-class filters (`2xx`, `4xx`, `5xx`) while the audited backend accepted only exact integer status codes, so selecting a class looked active but did not filter. | UI sends status classes at `frontend/src/pages/LogsPage.tsx:521-527`; pre-fix source grep showed `ListLogs` used only integer parsing. This batch fixes status-class SQL ranges at `internal/admin/log_handlers.go:46-61` and adds regression coverage at `internal/admin/admin_mock_test.go:2897-2934`. | Local verified in this batch with `go test ./internal/admin -count=1`. Keep as PRD5 trace/log ergonomics work until CI verifies. |
| CI-FE-001 | P1 | Technical debt/CI | Frontend CI type check was non-enforcing: `npm run type-check` did not exist and the workflow swallowed failure with `|| true`. | Pre-fix command `npm --prefix frontend run type-check` failed with missing script. This batch adds `type-check` at `frontend/package.json:12` and removes the swallow at `.github/workflows/ci.yml:97`. | Local verified in this batch with `npm --prefix frontend run type-check`. Required CI still needs to run on the eventual PR. |
| PRD5-DATAEX-001 | P1 | Product/Data/UI | Data Exchange Control is accepted only as a boundary/design row. No UniRouter UI, API, migration, RBAC, or audit ledger exists yet. | Matrix says P5-F28 is docs/shared-folder coordination only with no UI/API/migration at `docs/ops/PRD5-ACCEPTANCE-MATRIX.md:93`; current gap audit records the same at `docs/planning/PRD5-CURRENT-GAP-AUDIT-20260710.md:67`; PRD5 defines the future surface at `docs/PRD-Phase5.md:699-725`. | Accept a later scoped MVP only after outbound ledger schema, RBAC/audit, retention, and receipt fields are designed. |
| PRD5-TRACE-SEARCH-001 | P1 | Function/UI/Data | Trace/source fields exist and are displayed inside log detail, but request list search is still limited to request/model/provider/upstream error; source-event hash and evidence-ref are not first-class filters. | Structural fields and indexes exist at `migrations/000018_prd5_structural_evidence_fields.up.sql:3-52`; detail API returns structural fields at `internal/admin/log_handlers.go:267-346`; UI renders trace/evidence/hash tags at `frontend/src/pages/LogsPage.tsx:171-178`; list search is limited at `internal/admin/log_handlers.go:119-122`. | Split a safe P1 trace-search batch after status-class fix: exact source hash/evidence ref filters, UI controls, and indexes only if needed. |
| PRD5-XINSIGHT-001 | P1 | Cross-project/Data | UniRouter to local X-Insight integration is proven twice, but X-Insight ECS target runtime, durable import-job ID, deep link, and public adapter surface remain absent. | X-Insight README says current status is M0 plus two controlled loops at `/Users/m4/Desktop/projects/X-Insight/README.md:5-9`, not X-Insight ECS at `README.md:27-31`, and no fabricated `import_job_id` or deep link before durable API at `README.md:143-144`. | UniRouter must not wait on X-Insight for repo-owned work; deep links remain blocked until X-Insight exposes durable IDs/URLs. |
| PRD5-OPS-001 | P2 | Operations/HA | HA/DR and provider capacity remain target-evidence gaps, not local repository feature completion. | P5-F10/F11 remain prerequisite-gated at `docs/ops/PRD5-ACCEPTANCE-MATRIX.md:75-76`; current gap audit separates target evidence and provider load at `docs/planning/PRD5-CURRENT-GAP-AUDIT-20260710.md:84-91`; open provider load issue remains at `docs/issues/OPEN-ISSUES.md`. | Keep separate from PRD5 local implementation. Do not claim production readiness without target evidence. |
| PRD5-PAYLOAD-001 | P2 | Data/Security | Runtime payload evidence supports only `metadata_only` and `disabled`; raw/redacted/hash/reference modes are not implemented and remain approval-gated. | Current audit records raw payload capture off at `docs/planning/PRD5-CURRENT-GAP-AUDIT-20260710.md:47`; PRD5 keeps raw payload and exporter/outbox gated at `docs/PRD-Phase5.md:81`, `docs/PRD-Phase5.md:220`, and `docs/PRD-Phase5.md:859-873`. | Reject plaintext runtime cache for current PRD5. Reopen only with explicit opt-in, encryption, retention, delete/export, and audit-on-read. |

### Hypotheses To Validate Later

| Hypothesis | Why It Is Not Confirmed Yet | Next Evidence Needed |
|---|---|---|
| A richer route/failover panel is worth implementing now | Structural route/provider traces exist, but the exact operator workflow and UI density have not been validated against real failure volume. | Review gateway route/retry event coverage and inspect target failure examples before UI implementation. |
| A cursor exporter/outbox is necessary | Two controlled snapshots prove compatibility, not continuous ingestion need, throughput, latency, or operational ownership. | User decision plus volume, retry, backpressure, dedupe, retention, and owner requirements. |
| OTel/OpenInference bridge should be in PRD5 | Native evidence semantics are still being stabilized, and cross-system observability ownership belongs mostly to X-Insight. | Explicit integration consumer and mapping design after native evidence fields are stable. |
| Data Exchange Control UI is urgent | The product boundary is clear, but operator volume and approval/re-delivery workflows are not yet measured. | User decision that outbound approval/receipt audit needs a first-party UniRouter UI now. |

## P5-F12 Screening Result

Decision values below are for the P5-F12 screening row only. "Accept scoped
slice" does not mean the original full backlog item is implemented or approved.

| Backlog ID | Decision | Product Value | Dependencies | Risks | Priority | Acceptance Evidence |
|---|---|---|---|---|---:|---|
| PRD5-SCHED-001 Full Scheduler V2 | Defer full Scheduler V2; accept scoped route/failover evidence visibility as prerequisite work | Better operator understanding of routing and failover without rewriting dispatch | Current trace fields, route/provider event review, capacity health facts | Dispatch rewrite could break billing, retries, streaming, and provider semantics | P1 for visibility, P3 for full scheduler | Source audit, focused trace tests, UI evidence, no routing semantic change |
| PRD5-PLUGIN-001 Runtime plugin platform | Defer | Future extensibility, but not required for current X-Insight evidence readiness | Manifest, sandbox, signing, secret isolation, API compatibility, PlungeIn ADR | Large attack surface and lifecycle/support burden | P3 | Architecture/security design and compatibility tests before implementation |
| PRD5-MARKET-001 Plugin marketplace | Reject for current PRD5; reopen only after runtime plugins exist | Third-party distribution value is downstream of plugin platform | Runtime plugin platform, signing, review policy, rollback | Trust, license, support, and supply-chain obligations | P3/out of current scope | New product direction and plugin platform acceptance |
| PRD5-IAM-001 Logto/IAM rewrite into UniRouter core | Reject for current PRD5 | UniRouter should remain compatible with external IdP/OIDC rather than become an identity platform | Explicit product-direction change, migration, security patch process | Owning SAML/SCIM/MFA/social-login surface would expand security burden materially | P3/out of current scope | Separate IAM PRD if User changes direction |
| PRD5-DEPLOY-001 Kubernetes/Helm/Terraform managed HA product | Defer full managed product; accept INFRA-SEC remediation design now | Safer target runtime and future enterprise packaging | Chosen stack, secret management, DB/Redis HA, backup encryption, restore/failover drills | Premature IaC can encode wrong topology; target ECS exposure is the immediate risk | P0 for security design, P2 for full IaC | INFRA-SEC evidence plan now; future target cutover evidence under explicit authorization |
| PRD5-INFRA-001 Multi-instance infrastructure observability/topology | Accept limited topology/capacity design; defer collectors/productized cluster inventory | Helps operators reason about exposed services, capacity, and stale telemetry | Deployment topology, RBAC/redaction, collector access model, stale-data rules | Exposing host/IP/topology can leak infrastructure details | P1/P2 | Redacted topology model, API/UI design, target evidence without raw secrets/IP overexposure |
| PRD5-BUDGET-001 Budget hard-block enforcement | Defer implementation; accept design/threat-model only when chosen | Strong cost control | Billing traces, wallet freeze/settle invariants, stream cancel/refund, concurrency tests | Admission mistakes can over-block customers or leak spend via races | P2 | State-machine design, idempotency tests, stream timeout/refund regression suite |
| PRD5-CACHE-001 Runtime cache / prompt-response plaintext storage | Reject plaintext cache for current PRD5; defer only privacy-safe metadata/ref/hash design | Potential latency/cost savings, but not needed for evidence readiness | Explicit opt-in, encryption, tenant isolation, purge/delete/export, audit-on-read | High privacy and correctness risk | P3/out of current scope | Security design and approval before any payload persistence |
| PRD5-SDK-001 Full official SDK suite | Defer | Improves developer adoption after contracts stabilize | Stable typed OpenAPI schemas, semver policy, generated tests | Freezes unstable API response shapes too early | P3 | Typed schema coverage and compatibility CI |
| PRD5-ROLE-001 New persistent roles/service accounts | Defer implementation; accept role/service-account taxonomy design only if Data Exchange Control needs it | Needed for service-owned exports and ops workflows | Permission matrix, route guard tests, service-account credential and audit model | New authority surfaces can bypass current project RBAC if rushed | P2 | Taxonomy ADR, RBAC matrix, route tests, audit-event tests |
| PRD5-OPS-001 Full maintenance/SLA/SLO incident operations | Accept minimal incident/runbook evidence design; defer full SLA product | Helps operate target security and data exchange work | Operator roles, notification reliability, maintenance lifecycle, customer policy | False SLA claims without target reliability evidence | P2 | Incident runbook, evidence template, no customer-facing SLA claim until target evidence |

## Three Buckets Of Remaining Work

> Batch 1Y update: the accepted UniRouter-owned Data Exchange Control MVP,
> structural trace search, route/failover visibility, redacted current-process
> topology, and design-only budget/service-account/incident contracts below are
> now Local verified in
> `docs/ops/evidence/PRD5-B1Y-unirouter-owned-completion-local-20260710.md`.
> X-Insight and target-evidence buckets remain open.

### UniRouter-Owned Unimplemented Or Partial Work

- Data Exchange Control MVP: outbound approval/export ledger, artifact
  retention/delete, re-delivery audit, import receipt reconciliation, RBAC, and
  audit events. Evidence: `docs/ops/PRD5-ACCEPTANCE-MATRIX.md:93`.
- Trace/search ergonomics: source-event hash, evidence-ref, upstream request ID,
  and possibly trace ID list filters. Evidence: `internal/admin/log_handlers.go:119-122`.
- Route/failover visibility: expose existing route/provider decision evidence
  more clearly after event coverage audit.
- Optional topology/capacity design and redaction model for PRD5-INFRA-001.
- Design-only budget hard-block, service-account, and incident/SLA models.
- No raw payload/cache implementation without explicit approval.

### X-Insight Collaboration Not Implemented

- X-Insight ECS target runtime verification. Evidence:
  `/Users/m4/Desktop/projects/X-Insight/README.md:27-31`.
- Durable import-job ID and stable deep-link URL. Evidence:
  `/Users/m4/Desktop/projects/X-Insight/README.md:143-144`.
- Stable public adapter surfaces for LiteLLM/NewAPI/one-api beyond the current
  dedicated UniRouter adapter.
- Ongoing ingestion/read-only DB/exporter/outbox contract and retention
  reconciliation after the second snapshot.

### Target-Environment Evidence Only

- P5-F10 real upstream E2E with approved OpenRouter/direct Anthropic credentials.
- P5-F11 real enterprise OIDC/SSO E2E with approved IdP/test user/browser flow.
- First snapshot deletion due 2026-07-17 and second snapshot retention decision.
- INFRA-SEC-001 remediation cutover and verification.
- Provider load/capacity evidence and HA/DR drills.
- Target browser checks for frontend behavior after deployment.

## P0-P3 Batches

| Priority | Batch | Scope | Safe In Repo Now | Exit Evidence |
|---|---|---|---|---|
| P0 | PRD5 Batch 1X-a INFRA-SEC plan | Remediation design and evidence checklist for current ECS exposure | Yes, docs only | Plan linked from `OPEN-ISSUES.md`; no ECS mutation |
| P1 | PRD5 Batch 1X-b Logs/CI hygiene | Status-class log filtering and enforcing frontend type-check | Done in this batch | Full local verification passed; PR CI remains required |
| P1 | PRD5 Batch 1Y Trace Search Ergonomics | Add source/evidence/ref exact lookup and UI controls | Local verified | Backend tests, LogsPage tests, build, no schema change |
| P1 | PRD5 Batch 1Y Data Exchange Control MVP | Schema/API/UI/RBAC/audit plus receipt ledger | Local verified | Migration and repo tests pass locally; target apply/browser pending |
| P2 | PRD5 Batch 1Y Infrastructure topology and ops evidence | Redacted current-process topology plus incident evidence design | Local verified | Backend/frontend tests; target topology evidence remains separate |
| P2 | PRD5 Batch 1Y Budget/service-account/SLA designs | State machines and permission models | Design complete | No runtime authority change |
| P3 | PRD5 Later | Plugin platform, marketplace, full SDK, full Scheduler V2, plaintext cache | No current implementation | Separate User-approved PRD and security review |

## INFRA-SEC-001 Remediation Plan And Evidence Design

This is a plan only. It must not be executed without explicit target
authorization.

1. Capture current target state: ECS instance ID, ENIs, current security group
   ID, bound services, management path, and rollback owner. Redact public IPs,
   private IPs, and credentials in repo evidence.
2. Confirm at least two management paths before changing ingress: Alibaba Cloud
   Cloud Assistant plus SSH via approved fixed source, VPN, or bastion. Do not
   edit the in-use group until recovery is proven.
3. Clone the security group and build least-privilege rules:
   - public ingress only for approved app entrypoints such as 80/443 or the
     actual reverse-proxy port;
   - SSH only from the approved management source or bastion;
   - PostgreSQL, Redis, Prometheus, Grafana, and internal app admin ports not
     public;
   - outbound rules documented and minimized if possible.
4. Attach the cloned group in a controlled window with rollback to the original
   group ready.
5. Verify from outside: public app endpoint works, DB/Redis/Prometheus/Grafana
   ports are not reachable publicly, `/health/ready` works through the intended
   path, and metrics remain disabled or CIDR-restricted.
6. Verify from inside: app can reach PostgreSQL and Redis, Prometheus/Grafana
   probes match intended deployment, logs show no denied-management lockout.
7. Record evidence: sanitized rule diff, command/manual steps, pre/post port
   checks, app health, rollback plan, and cleanup note.

## Local Work Completed In This Batch

- Fixed log status-class filtering in `internal/admin/log_handlers.go:46-61`.
- Added regression coverage in `internal/admin/admin_mock_test.go:2897-2934`.
- Added frontend `type-check` script in `frontend/package.json:12`.
- Made CI type checking enforce failure in `.github/workflows/ci.yml:97`.

Verification passed:

```bash
go test ./internal/admin -count=1
go test ./... -count=1
go vet ./...
npm --prefix frontend run type-check
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Frontend test output still includes existing React Router future-flag, Semi Form,
and `findDOMNode` warnings; the build still reports the existing large chunk
warning. No command failed. PR CI is still required before any CI verified claim.
