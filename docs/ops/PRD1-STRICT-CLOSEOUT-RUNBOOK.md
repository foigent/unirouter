# PRD1 Strict Closeout Runbook

> Purpose: final execution gate before PRD1 can be closed. PRD1 is not closed while any item in `PRD-ACCEPTANCE-MATRIX.md` remains `Implemented not accepted` without an explicit user-approved deferral.
> Last updated: 2026-07-01.

## Closeout Rule

PRD1 can close only when:

1. `docs/ops/PRD-ACCEPTANCE-MATRIX.md` contains only `E2E accepted`, `Production verified`, `Unit tested` for unit-scope items, or explicitly approved `Out of scope`.
2. `docs/issues/OPEN-ISSUES.md` has no `High` or `Medium` issue in `Open`, `In progress`, or `Fixed awaiting Canary`.
3. `docs/agent-workflows/VERIFY-RESULT.md` passes the latest Codex `FIX-SIGNAL.md`.
4. Sentinel CI/merge and Atlas deployment evidence are recorded.
5. User visual acceptance is recorded for PRD1 UI because OpenClaw Canary cannot judge visuals.

## Safety Classes

| Class | Meaning | Examples | Can Codex Run Without Extra Approval |
|-------|---------|----------|--------------------------------------|
| S0 | No auth and no data mutation | health, metrics, frontend root, unauthenticated 401 checks | Yes |
| S1 | Low-impact auth/read check | admin login, admin GET APIs, default-admin rejection check | Yes, but record audit side effects |
| S2 | Controlled write in test org/account | create channel/API key, top up, adjust wallet, sensitive words | Only with explicit test account/data scope |
| S3 | Real provider paid calls | non-stream/stream chat completions through UniRouter | Only with explicit budget and provider/key scope |
| S4 | Stress/fault tests | 100 concurrent, interrupted stream, failover, circuit breaker | Only with explicit test window and rollback plan |

## Mandatory Execution Order

1. Deploy the latest build that includes default admin password protection and `/health/ready` PRD response shape.
2. Run S0/S1 cloud smoke:

```bash
scripts/prd1-cloud-smoke.sh
```

If an admin token or safe admin credential is available, also run:

```bash
RUN_ADMIN_READ_SMOKE=1 ADMIN_TOKEN='<jwt>' scripts/prd1-cloud-smoke.sh
# or, knowing login writes one audit row:
RUN_ADMIN_READ_SMOKE=1 ADMIN_USERNAME='<admin>' ADMIN_PASSWORD='<password>' scripts/prd1-cloud-smoke.sh
```

3. Confirm the public default admin password is rejected. A 200 response for `admin/admin123456` is a PRD1 security blocker.
4. Run controlled S2 admin mutation tests in a dedicated test org/key/channel only.
5. Run S3 real provider E2E through UniRouter, not directly against providers.
6. Run S4 load, interrupted streaming, and failover scenarios in a safe test window.
7. Update `PRD-ACCEPTANCE-MATRIX.md`, `CURRENT-STATUS.md`, `OPEN-ISSUES.md`, and `VERIFY-RESULT.md` references with exact evidence.

## Required Evidence By Area

| Area | Required Evidence |
|------|-------------------|
| Gateway non-stream | request command, provider/channel/model, 200 response, `_uniroute.cost`, call log row |
| Gateway stream | SSE headers/chunks, `[DONE]`, final `_uniroute`, call log row with `is_stream=true` |
| Interrupted stream | disconnect method, log row, usage source, wallet settlement |
| Billing | wallet before/after, freeze/settle transactions, no negative balance |
| Idempotency | repeated `X-Request-Id`, same/deduped settlement, `freeze_idempotency` row |
| Admin writes | create/edit/disable channel and key, wallet top-up/adjust, audit log rows |
| Sensitive words | configured word, 400 rejection, normal request still passes |
| Failover/circuit | primary failure evidence, backup selection, circuit state/cooldown evidence |
| Load | k6/wrk command, request count, p95/p99, error rate, wallet invariant |
| Observability | `/metrics`, Prometheus healthy, Grafana dashboard/panel screenshot or manual notes |
| UI | cloud browser walkthrough for Dashboard, Channels, Keys, Wallet, Logs, Settings, Docs |

## Resolved PRD1 Blocking Finding

On 2026-07-01 Codex confirmed Alibaba Cloud accepted `admin/admin123456` and returned a JWT. After redeploy, `scripts/prd1-cloud-smoke.sh` passed and the same default login returned 403.

Code-level remediation recorded at PRD1 closeout:

- Web login rejects the public default admin password unless `UNIROUTE_ALLOW_DEFAULT_ADMIN_LOGIN=true`.
- At that historical point, the recovery commands accepted a value-bearing password flag. V1 has retired that interface because it exposed secrets in process arguments. This historical statement is not an executable current instruction.

Current supported deployment remediation uses a hidden interactive prompt:

```bash
docker compose exec backend ./uniroute admin set-password \
  --username admin \
  --org-id 1
```

For non-interactive operation, follow the current deployment runbook's controlled
secret-file plus `docker compose exec -T ... --password-stdin` procedure. Do not
put a password in argv or an environment variable. Do not set the password back
to `admin123456`; the CLI refuses it by default.

Current cloud evidence:

- `GET http://8.130.18.85:8080/health/ready`: `{"status":"ok","db":"ok","redis":"ok"}`
- `GET http://8.130.18.85:3000/health/ready`: `{"status":"ok","db":"ok","redis":"ok"}`
- `POST http://8.130.18.85:3000/api/v1/auth/login` with `admin/admin123456`: 403
