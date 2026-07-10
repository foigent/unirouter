# PRD5 Batch 1Z Target Deployment And Infrastructure Security Evidence

**Date:** 2026-07-10

**Owner:** Codex

**Environment:** Alibaba Cloud shared development/test ECS

**Status:** Target verified for the evidence explicitly listed below

## Scope

This evidence closes the authorized target operations for PRD5 Batch 1Y and
`INFRA-SEC-001`:

- merge and CI verification of the UniRouter-owned PRD5 implementation;
- target backup, release build, migration `000019`, stable/canary deployment,
  health checks, and target browser checks;
- a target-discovered build-network fix and stable/canary runtime configuration
  parity fix;
- least-privilege ECS security-group cutover with rollback retained;
- temporary QA account and credential cleanup.

It does not claim production readiness, migration down execution, real paid
provider E2E, real enterprise SSO, X-Insight ECS verification, or completion of
X-Insight-owned runtime contracts.

## Repository And CI

| Change | Merge | Required CI |
|---|---|---|
| UniRouter-owned PRD5 controls | `88a7354` / PR #131 | Backend Tests, Frontend Build, Docker Build passed |
| Configurable target Go proxy | `5b4a5e7` / PR #132 | Backend Tests, Frontend Build, Docker Build passed |
| Stable/canary runtime parity | `95d1da9` / PR #133 | Backend Tests, Frontend Build, Docker Build passed |

The final deployed backend commit is
`95d1da9f6a169311de6a9f58bba4a4ad1efe138c`.

## Backup And Release Controls

- A custom-format PostgreSQL backup was written before migration under the
  target `backups/` directory.
- Backup size: 133,167 bytes.
- Backup SHA-256:
  `35c8c530db8bd296ad583c76b3177a2c4b6b91e7e35986f14f0cec86da4726ee`.
- `pg_restore -l` succeeded before migration.
- Existing stable, canary, and frontend image IDs were retained under explicit
  rollback tags. The prior security group remains available but is not attached.
- Releases were extracted into commit-addressed directories rather than
  overwriting the mutable `/opt/unirouter` working tree.
- The existing `.env` and `.env.production` were copied with mode `0600`; no
  credential value was changed or recorded in repository evidence.

The initial target Docker build could not reach `proxy.golang.org`. PR #132 adds
a configurable `GOPROXY` build argument while preserving the official proxy as
the default. The target used `https://goproxy.cn,direct`; `go mod download`
completed in 2.7 to 3.2 seconds and existing `go.sum` verification remained on.

## Migration And Deployment

- Pre-deploy migration status showed `000001` through `000018` applied.
- `000019_prd5_data_exchange_control.up.sql` applied successfully in 73 ms.
- Readback showed these tables:
  - `integration_exchange_items`
  - `integration_exchanges`
  - `integration_exchange_members`
  - `integration_exchange_deliveries`
  - `integration_exchange_receipts`
- Stable and canary both returned
  `{"status":"ok","db":"ok","redis":"ok"}` after deployment.
- Stable and canary run from the final commit-addressed release directory.
- Direct canary runtime API readback reported:
  - environment `local`;
  - deployment mode `docker-compose`;
  - deployment role `canary`;
  - version `prd5-b1y`;
  - commit `95d1da9f6a169311de6a9f58bba4a4ad1efe138c`;
  - topology scope `current_process`, visibility `redacted`, network exposure
    `not_observed`, and multi-instance state `not_configured`.

PR #133 was required because Nginx routes 10% of `/api/` requests to canary and
target browser evidence showed canary defaults could return `unspecified` and
empty monitoring URLs. The final canary receives the stable runtime, evidence,
monitoring, login-rate-limit, retention, license, and OIDC configuration set,
while retaining `SKIP_MIGRATION=true` and the explicit `canary` role.

## Target Browser Evidence

The target public HTTP entrypoint was checked in the in-app browser after the
deployment:

- unauthenticated access redirected to `/login` and rendered the login form;
- a temporary platform-admin account logged in successfully;
- platform-admin navigation included Data Exchange;
- `/data-exchange` rendered approval queue and exchange ledger tabs, safety
  labels, and an empty target ledger without console errors;
- `/logs` rendered exact Trace ID, upstream request ID, Evidence Ref, and Source
  Event Hash filters against the target dataset;
- `/status` rendered the redacted service/database/Redis topology and healthy
  target dependencies;
- a 390 x 844 viewport rendered Data Exchange without page-level horizontal
  overflow; the table used contained horizontal scrolling;
- the final public entrypoint still rendered `/login` with no browser console
  errors after the infrastructure cutover.

The route/failover summary was not opened for a concrete target request. P5-F35
therefore remains CI verified rather than Target verified.

## INFRA-SEC-001 Cutover

Pre-cutover target state confirmed one security group with public TCP 1-65535,
publicly bound application/database/cache/monitoring ports, SSH, and Cloud
Assistant. Public/private IPs, management source, instance ID, and security-group
IDs are intentionally omitted here.

The replacement group contains:

- public TCP 80 and 443 allow rules;
- TCP 22 limited to the approved operator source `/32`;
- an explicit lower-priority TCP 1-65535 drop rule;
- no public allow rule for frontend debug, backend stable/canary, PostgreSQL,
  Redis, Prometheus, Grafana, or RDP ports.

Cutover evidence:

- the instance readback contains only the replacement group;
- a new SSH session succeeded after cutover;
- Cloud Assistant remained online and Session Manager capable;
- public HTTP returned 200 and `/health/ready` returned the accepted DB/Redis
  readiness JSON through Nginx;
- external HTTP probes received no response from 3000, 3001, 8080, 8081, or
  9090;
- external Redis PING and PostgreSQL SSLRequest probes received no protocol
  response from 6379 or 5432;
- from inside the target, application readiness passed, Prometheus readiness
  returned 200, and Grafana reported database `ok`;
- the old group remains unattached and retained as the rollback object.

Pure `nc -z` results were rejected as evidence after the operator workstation
route was shown to traverse a `utun` transparent proxy. Final acceptance uses
protocol-response checks plus Alibaba Cloud control-plane readback.

## Cleanup

- Two temporary platform-admin QA users created for browser/runtime checks were
  deleted through the supported admin API.
- Browser logout cleared the interactive temporary session before deletion.
- Temporary credential/response files were deleted by exact path.
- Database readback showed zero matching temporary QA users.
- No real provider request, expanded export, X-Insight import, raw payload, or
  production data mutation occurred.

## Remaining Boundaries

- Migration `000019` down was not executed against the target database. The
  pre-migration backup, down SQL, and rollback image tags are retained.
- Docker Compose still publishes several internal services on host interfaces;
  the ECS security group now blocks external protocol access. Loopback-only or
  un-published host bindings remain a defense-in-depth follow-up, not evidence
  that the public exposure is still open.
- HTTPS/TLS service behavior on 443 was not verified; the current accepted
  public application evidence is HTTP 80 in a development/test environment.
- Full Scheduler V2, runtime plugin marketplace, managed HA/IaC, full SDK, IAM
  rewrite, budget hard-block runtime, service-account runtime, and customer SLA
  operations remain deferred or separately scoped per P5-F12.
- X-Insight must still answer the shared runtime-contract information request.
