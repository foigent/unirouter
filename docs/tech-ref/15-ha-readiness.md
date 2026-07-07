# HA Readiness

> Version: v0.1
> Scope: PRD2 batch 8 backend and operations foundation.

## Goals

PRD2 HA readiness means UniRouter can run more than one gateway/admin instance against the same PostgreSQL and Redis services without corrupting billing, idempotency, rate-limit, routing, or audit state.

This is not a mandatory full HA platform stack. Redis Sentinel, PostgreSQL replicas, PgBouncer, Kubernetes probes, and managed load balancers remain deployment choices.

## Instance Model

Each UniRouter application instance owns:

- HTTP listener.
- in-process admission controller.
- in-process sensitive-word DFA cache.
- in-flight streaming response state.

Shared mutable state must live in PostgreSQL or Redis:

| State | Owner | Notes |
|-------|-------|-------|
| orgs/users/projects/api keys | PostgreSQL | canonical auth and RBAC state |
| billing wallet, transactions, grants, freeze idempotency | PostgreSQL | transactionally updated |
| request logs, audit logs, notification events | PostgreSQL | canonical evidence |
| channel circuit/key cooldown state | PostgreSQL | shared by routers |
| per-key RPM/RPS windows | Redis | shared rate-limit counters; existing storage keeps the legacy `qps_limit` name |
| sensitive words | PostgreSQL + periodic in-process cache refresh | DB is canonical; cache converges on each instance |

The admission controller is intentionally per-process. Capacity planning should multiply per-instance admission limits by instance count or configure the load balancer accordingly.

## Migration Ownership

Only one stable instance should run migrations for a shared database.

Canary or secondary instances must set:

```text
SKIP_MIGRATION=true
```

This prevents two instances from attempting schema changes at the same time.

## Readiness Contract

Each instance must pass:

```bash
curl -fsS "$BASE_URL/health/ready"
```

Expected body:

```json
{"status":"ok","db":"ok","redis":"ok"}
```

This proves the instance can reach the shared PostgreSQL and Redis endpoints. It does not prove upstream provider reachability.

## Two-Instance Smoke

Local/staging Docker Compose can start a canary instance with:

```bash
docker compose -f docker-compose.yml -f docker-compose.canary.yml up -d
```

The overlay exposes:

- stable backend direct URL: `http://localhost:8080`
- canary backend direct URL: `http://localhost:8081`
- HTTP proxy/load-balancer URL: `http://localhost`

Use:

```bash
UNIROUTE_STABLE_URL=http://localhost:8080 \
UNIROUTE_CANARY_URL=http://localhost:8081 \
scripts/prd2-ha-smoke.sh
```

Optional gateway smoke:

```bash
UNIROUTE_STABLE_URL=http://localhost:8080 \
UNIROUTE_CANARY_URL=http://localhost:8081 \
UNIROUTE_PROXY_URL=http://localhost \
UNIROUTE_RUN_GATEWAY_SMOKE=1 \
UNIROUTE_API_KEY=sk-... \
UNIROUTE_MODEL=gpt-4o \
UNIROUTE_HA_DB_DSN='postgres://uniroute:...@localhost:5432/uniroute?sslmode=disable' \
scripts/prd2-ha-smoke.sh
```

The default smoke is read-only health verification. Gateway smoke sends real requests only when explicitly enabled. When `UNIROUTE_HA_DB_DSN` is set, the script also checks that a shared request id does not create duplicate freeze idempotency or freeze transaction rows.

Shared rate-limit evidence requires a dedicated low-limit test key:

```bash
UNIROUTE_RUN_GATEWAY_SMOKE=1 \
UNIROUTE_RUN_RATE_LIMIT_SMOKE=1 \
UNIROUTE_RATE_LIMIT_REQUESTS=4 \
UNIROUTE_API_KEY=sk-low-limit \
scripts/prd2-ha-smoke.sh
```

## Wallet And Idempotency Safety

The billing freeze path uses `freeze_idempotency.request_id` as the shared idempotency key.

Batch 8 hardens the duplicate race:

- two instances may receive the same request id at the same time.
- PostgreSQL primary key enforces one canonical freeze row.
- a losing duplicate insert rolls back its wallet update and reads the winning freeze row.
- callers receive the existing freeze amount instead of surfacing a unique-key race.

Integration evidence lives in:

```bash
go test -tags integration ./internal/billing -run TestFreeze_ConcurrentDuplicateRequestAcrossInstances_Integration -v
```

The test uses two `Billing` instances sharing the same PostgreSQL pool to model stable/canary application instances.

## Rollback

For a failed canary or secondary instance:

1. Remove the instance from the load balancer or set its weight to zero.
2. Stop the secondary instance.
3. Keep stable instance serving traffic.
4. Confirm stable readiness:

```bash
curl -fsS "$STABLE_URL/health/ready"
```

## Backup And Restore Rehearsal

Run restore rehearsal only against an empty non-production database:

```bash
UNIROUTE_BACKUP_SOURCE_DSN='postgres://uniroute:...@source:5432/uniroute?sslmode=disable' \
UNIROUTE_RESTORE_REHEARSAL_DSN='postgres://uniroute:...@localhost:5432/uniroute_restore_rehearsal?sslmode=disable' \
UNIROUTE_BACKUP_RESTORE_CONFIRM=non-production-restore-target \
scripts/prd2-backup-restore-rehearsal.sh
```

The script runs `pg_dump`, restores into the rehearsal target, and verifies migration history plus core tables. It does not drop or truncate the target database.

2026-07-03 Atlas evidence:

- Report: `docs/ops/evidence/PRD2-B21-backup-recovery-drill-20260703.md`
- Environment: Alibaba Cloud ECS dev/test, PostgreSQL 16.14 in Docker.
- Result: PASS for backup creation, custom-format dump integrity, isolated drill restore, migration/table/core row/FK/index/query checks, drill cleanup, and WAL archiving.
- Not covered: backup encryption, NAS/off-server sync, cron backup automation, failure alerts, PITR recovery, remote restore, app-level recovery verification, Patroni/local DB HA, and app DB failover reconnect behavior.

For a failed schema-bearing release:

1. Stop new canary traffic before rollback.
2. Restore PostgreSQL from the pre-upgrade backup if schema changes were applied and cannot be safely rolled back.
3. Restore Redis only when the incident involves Redis state loss or corruption.
4. Restart stable instance and verify `/health/ready`.

## Current Limits

- No mandatory PostgreSQL replica or Redis Sentinel topology in PRD2 batch 8.
- No global cross-instance admission semaphore.
- Sensitive-word cache convergence is periodic, not push-based.
- Provider upstream failures are covered by routing/circuit logic, not by HA readiness smoke.
