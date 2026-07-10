# PRD5 Batch 1W Target Rerun Evidence

> Date: 2026-07-10
> Environment: existing Alibaba Cloud ECS development/test deployment
> Authorization: `UR-XI-GATE-A-20260710-02`
> Operator: Codex
> Result: Target verified for the authorized denied scenario; X-Insight external persistence/readback verified for the resulting snapshot

## Scope

This run was limited to:

1. deploy the CI-passing UniRouter `main` source containing Batch 1W;
2. execute exactly one synthetic `model_permission_denied` request;
3. verify correlation and pre-billing/pre-provider termination;
4. disable temporary API keys 36 and 37;
5. export and import a metadata-only snapshot for the approved request ID.

It did not authorize a successful paid-provider call, raw prompt/response
capture, broad request discovery, X-Insight ECS deployment, read-only database
access, or exporter/outbox work.

## Deployment Evidence

- Repository target: `e823abc293f996264763c6c1aca19d3d4d6e705f`.
- Batch 1W is contained in target source through `931400a` / PR #126.
- Backend source manifest on the target matched the selected repository target.
- Static Linux/amd64 backend binary SHA-256:
  `8a27eb8042623897d009ec7ac29fb9050cbdde73de1444e3641c43217a3b5157`.
- Deployed image short ID: `sha256:776d046b284d`.
- Stable and canary readiness checks passed after the switch.
- Database schema remained `000018_prd5_structural_evidence_fields`; Batch 1W
  has no migration.
- Rollback tags retained on target:
  `unirouter-backend:rollback-20260710151314` and
  `unirouter-backend-canary:rollback-20260710151314`.

The target could not reach `proxy.golang.org`, so the normal cloud Docker build
could not complete. The deployment used an exact locally built binary from the
verified target source, transferred with one-time authenticated encryption, then
replaced only `/app/uniroute` in the previous backend image. No database,
frontend, or dependency-layer change was introduced.

## Authorized Denial Result

- Request ID: `gatea-model-denied-b1w-283194e67a0c3ff9`.
- Requested model: `mimo-v2.5`.
- Temporary key 37 allowed a different model, so the request exercised the
  Batch 1W API-key model-permission denial path.
- HTTP result: 403.
- Returned `X-Request-ID`: exact match.
- Trace evidence:
  `auth.model_not_allowed|auth|403|permission_denied|auth|rejected|auth_error.model_not_allowed|37`.
- Call-log rows: 0.
- Billing-freeze rows: 0.
- Transaction rows: 0.
- Provider-event rows: 0.
- Temporary API keys 36 and 37: disabled after the run.

This proves target behavior for the denied path only. It does not prove a real
upstream provider success path and incurred no provider request or model cost.

## X-Insight Receipt

- Exchange ID: `uxchg_8deae233291214154815c336c9278983`.
- Approval manifest SHA-256:
  `8cc3d3b54bd8c0220747c0f19cf49cb6f34bcb2a68373a0074ad60dea153c0f4`.
- Snapshot SHA-256:
  `3c0b13241eb369ca096115f420e73d1ae35c3e6ac4b794cb997f56cf09c7339c`.
- Receipt SHA-256:
  `16cbac6030f5f3e08f6df73593a5ef61c5395cde73e816d6e10f7f08ab81ea36`.
- Source counts: one request ID, zero call logs, one trace event, one generated
  capture gap.
- First X-Insight persistence: two inserted, zero conflicts/rejections.
- Idempotent rerun: zero inserted, two duplicate/no-op, zero conflicts.
- Readback: two events and two gaps.
- Receipt status: `persisted` with
  `source_capture_gaps_present` because a correctly rejected request has no call
  log; that absence is represented as `not_available_from_source` rather than a
  fabricated row.

The governed shared artifacts are under:

```text
/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/gatea-b1w-snapshot-20260710.json
/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/gatea-b1w-handoff-note-20260710.md
/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/gatea-b1w-snapshot-20260710.receipt.json
```

## Boundaries And Follow-Up

- Batch 1W target closure is complete for the authorized denial scenario.
- X-Insight external integration is verified for this second controlled
  snapshot, including persistence, idempotency, and readback.
- X-Insight itself is not deployed or runtime-verified on Alibaba Cloud ECS.
- A real MiMo or other provider success-path test remains separately gated by
  ordinary pay-as-you-go credentials, a request/token/cost cap, and explicit
  authorization.
- Read-only DB/view and exporter/outbox are still product/architecture
  decisions; this run does not activate them automatically.
- The first Gate A snapshot deletion deadline remains 2026-07-17. Retention for
  this new snapshot was not specified in `UR-XI-GATE-A-20260710-02` and must be
  decided before deletion.

## Infrastructure Findings

The current ECS is not a suitable shared UniRouter plus X-Insight host:

- the instance is 2 vCPU / 4 GB with a 40 GB root disk, below the current
  UniRouter runbook's 50 GB recommendation and with limited memory headroom;
- its security group permits all TCP ports from `0.0.0.0/0`;
- PostgreSQL, Redis, monitoring, and application ports are bound publicly;
- an unsolicited connection reached a temporary high-port receiver during the
  authorized encrypted artifact transfer;
- direct SSH port 22 timed out from the operator workstation, while Alibaba
  Cloud Assistant remained available;
- `/opt/unirouter` is a mutable rsync deployment tree with local modifications,
  so a direct `git pull` is unsafe.

Security-group changes were not part of this authorization. Remediation should
use a separately authorized security-group cutover that preserves a confirmed
management path, followed by immutable artifact deployment work.

## Process Exception

A staging cleanup command removed two copied `.DS_Store` files with a bulk
`find ... -delete` expression inside a newly created isolated release directory.
No source, runtime, evidence, configuration, or user data was affected. This
violated the workspace rule that deletion must name one exact file at a time;
future cleanup must use explicit single-file paths or be left for the User.
