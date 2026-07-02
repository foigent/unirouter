# FIX-SIGNAL.md — PRD2 Batch 21 HA And Deployment Validation Tooling

**修复者：** Codex
**日期：** 2026-07-02
**修复轮次：** PRD2 Batch 21

---

## 前置条件

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| Batch 20 | Canary PASS / Sentinel merged | `VERIFY-RESULT.md` reports PASS 7/7; `main` includes PR #53 at `6b1f5cc` |
| Batch 18 | Open / deferred | User explicitly deferred real-upstream provider acceptance |
| Production HA | Not claimed | This round makes local/staging validation runnable; production HA still requires target evidence |
| Docker runtime | Required for full HA smoke | Codex shell has no `docker` command, so runtime compose smoke was not executed locally |

---

## Fix #1: Canary overlay is runnable without TLS certificates

**Files:**

- `docker-compose.canary.yml`
- `nginx/nginx.http-canary.conf`
- `docs/ops/deployment-runbook.md`

**Original issue:**

The canary runbook referenced a two-instance Docker Compose flow, but the default Nginx mount used the TLS config. In local/staging environments without certificates, Nginx would fail or require manual config edits before smoke testing.

**Current code:**

- `docker-compose.canary.yml` mounts `nginx/nginx.http-canary.conf` by default.
- The new HTTP-only config proxies:
  - `/v1/` and `/api/` through weighted stable/canary upstreams.
  - `/health/` and `/metrics` directly to stable.
  - frontend routes to the frontend container.
- TLS production config remains available via `nginx/nginx.conf`.

**Verification method:**

```bash
ruby -e 'require "yaml"; YAML.load_file("docker-compose.canary.yml"); YAML.load_file("docker-compose.yml"); puts "yaml ok"'

JWT_SECRET=01234567890123456789012345678901 \
API_KEY_HASH_SECRET=abcdefabcdefabcdefabcdefabcdef12 \
ENCRYPTION_KEY=01234567890123456789012345678901 \
GRAFANA_ADMIN_PASSWORD=local-grafana-pass \
docker compose -f docker-compose.yml -f docker-compose.canary.yml config
```

Expected:

- YAML parse passes.
- Docker Compose config resolves successfully in a Docker-enabled environment.
- No TLS certificate is required for the default canary overlay.

---

## Fix #2: Canary direct smoke port matches runbook and HA script

**Files:**

- `docker-compose.canary.yml`
- `docs/tech-ref/15-ha-readiness.md`
- `docs/ops/deployment-runbook.md`

**Original issue:**

The HA smoke and runbook used `UNIROUTE_CANARY_URL=http://localhost:8081`, but `backend-canary` did not expose a direct host port.

**Current code:**

- `backend-canary` now maps `8081:8080`.
- Docs list:
  - stable direct URL: `http://localhost:8080`
  - canary direct URL: `http://localhost:8081`
  - proxy URL: `http://localhost`

**Verification method:**

```bash
JWT_SECRET=01234567890123456789012345678901 \
API_KEY_HASH_SECRET=abcdefabcdefabcdefabcdefabcdef12 \
ENCRYPTION_KEY=01234567890123456789012345678901 \
GRAFANA_ADMIN_PASSWORD=local-grafana-pass \
docker compose -f docker-compose.yml -f docker-compose.canary.yml up -d --build

UNIROUTE_STABLE_URL=http://localhost:8080 \
UNIROUTE_CANARY_URL=http://localhost:8081 \
UNIROUTE_PROXY_URL=http://localhost \
scripts/prd2-ha-smoke.sh
```

Expected:

- stable and canary direct health checks pass.
- proxy health check passes through Nginx to stable.
- If Docker is unavailable, Canary should report this as an environment SKIP rather than a product PASS.

---

## Fix #3: HA smoke records gateway, DB idempotency, proxy, and optional rate-limit evidence

**Files:**

- `scripts/prd2-ha-smoke.sh`
- `docs/tech-ref/15-ha-readiness.md`
- `docs/ops/deployment-runbook.md`

**Original issue:**

The existing HA smoke checked readiness only and had a basic optional gateway call. It did not expose proxy verification, live checks, DB evidence for duplicate request ids, or a way to exercise shared Redis rate-limit state.

**Current code:**

- Always checks `/health/live` and `/health/ready` for stable and canary.
- Checks proxy live/ready when `UNIROUTE_PROXY_URL` is set.
- Optional gateway smoke calls stable/canary with the same `X-Request-ID`; proxy gets a separate request id.
- If `UNIROUTE_HA_DB_DSN` or `DB_DSN` is set, the script checks that the shared request id did not create duplicate `freeze_idempotency` or freeze `transactions` rows.
- Optional shared rate-limit smoke alternates stable/canary requests and expects HTTP 429 when a low-limit key is used.

**Verification method:**

```bash
bash -n scripts/prd2-ha-smoke.sh

UNIROUTE_STABLE_URL=http://localhost:8080 \
UNIROUTE_CANARY_URL=http://localhost:8081 \
UNIROUTE_PROXY_URL=http://localhost \
UNIROUTE_RUN_GATEWAY_SMOKE=1 \
UNIROUTE_API_KEY=sk-... \
UNIROUTE_MODEL=gpt-4o \
UNIROUTE_HA_DB_DSN='postgres://uniroute:...@localhost:5432/uniroute?sslmode=disable' \
scripts/prd2-ha-smoke.sh
```

Optional low-limit key check:

```bash
UNIROUTE_RUN_GATEWAY_SMOKE=1 \
UNIROUTE_RUN_RATE_LIMIT_SMOKE=1 \
UNIROUTE_RATE_LIMIT_REQUESTS=4 \
UNIROUTE_API_KEY=sk-low-limit \
scripts/prd2-ha-smoke.sh
```

Expected:

- Script syntax passes.
- Health checks pass for stable/canary/proxy in a running two-instance environment.
- Gateway smoke returns accepted HTTP status `200`, `409`, or `429`.
- DB evidence reports `freeze_rows<=1` and `freeze_tx_rows<=1` for the shared request id.
- Rate-limit smoke observes HTTP 429 only when a deliberately low-limit key is used.

---

## Fix #4: Guarded backup/restore rehearsal script

**Files:**

- `scripts/prd2-backup-restore-rehearsal.sh`
- `docs/ops/deployment-runbook.md`
- `docs/tech-ref/15-ha-readiness.md`

**Original issue:**

PRD2 HA validation requires backup/restore rehearsal evidence, but the repo did not provide a safe, repeatable command that refuses accidental production restores.

**Current code:**

- Requires `UNIROUTE_BACKUP_SOURCE_DSN`.
- Requires `UNIROUTE_RESTORE_REHEARSAL_DSN` pointing to a separate empty non-production database.
- Requires `UNIROUTE_BACKUP_RESTORE_CONFIRM=non-production-restore-target`.
- Refuses identical source/restore DSNs.
- Runs `pg_dump`, restores into the rehearsal target, and validates `schema_migrations` plus core tables.
- Does not drop or truncate the target database.

**Verification method:**

```bash
bash -n scripts/prd2-backup-restore-rehearsal.sh

UNIROUTE_BACKUP_SOURCE_DSN='postgres://uniroute:...@source:5432/uniroute?sslmode=disable' \
UNIROUTE_RESTORE_REHEARSAL_DSN='postgres://uniroute:...@localhost:5432/uniroute_restore_rehearsal?sslmode=disable' \
UNIROUTE_BACKUP_RESTORE_CONFIRM=non-production-restore-target \
scripts/prd2-backup-restore-rehearsal.sh
```

Expected:

- Script syntax passes.
- In a prepared non-production restore target, output ends with:

```text
PASS backup/restore rehearsal schema_versions=<n> core_tables=6 dump_file=/tmp/...
```

---

## Fix #5: Status docs avoid HA overclaim

**Files:**

- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`
- `docs/ops/PRD2-COMPLETION-PLAN.md`
- `docs/issues/OPEN-ISSUES.md`

**Original issue:**

Batch 21 should not mark production HA complete merely because scripts and docs exist. Batch 20 also needed to be reflected as Canary/Sentinel complete after PR #53.

**Current docs:**

- Batch 20 is marked Canary PASS / Sentinel merged.
- P2-F07 audit rows are advanced to E2E accepted after Canary PASS.
- P2-F09 HA remains `Fixed locally awaiting Canary/runtime verification`.
- `PRD2-HA-001` remains open/in progress until Docker/staging/proxy/backup restore and eventual production target evidence exist.

**Verification method:**

```bash
rg -n "Batch 21|P2-F09|PRD2-HA-001|Fixed locally awaiting Canary/runtime verification|6b1f5cc|Batch 20" \
  docs/ops/CURRENT-STATUS.md \
  docs/ops/PRD2-ACCEPTANCE-MATRIX.md \
  docs/ops/PRD2-COMPLETION-PLAN.md \
  docs/issues/OPEN-ISSUES.md
```

Expected:

- Batch 21 is not marked E2E accepted.
- Production HA is not marked complete.
- Batch 20 is recorded as Canary PASS / Sentinel merged.

---

## Local Verification Already Run By Codex

```bash
bash -n scripts/prd2-ha-smoke.sh
bash -n scripts/prd2-backup-restore-rehearsal.sh
ruby -e 'require "yaml"; YAML.load_file("docker-compose.canary.yml"); YAML.load_file("docker-compose.yml"); puts "yaml ok"'
go test ./...
git diff --check
```

Observed limitation:

```text
docker compose config -> zsh:1: command not found: docker
command -v nginx -> not found
```

Codex therefore did not claim compose startup, Nginx runtime syntax, actual two-instance smoke, rollback, or restore rehearsal as completed.

---

## Files Changed

- `docker-compose.canary.yml`
- `nginx/nginx.http-canary.conf`
- `scripts/prd2-ha-smoke.sh`
- `scripts/prd2-backup-restore-rehearsal.sh`
- `docs/ops/deployment-runbook.md`
- `docs/tech-ref/15-ha-readiness.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`
- `docs/ops/PRD2-COMPLETION-PLAN.md`
- `docs/issues/OPEN-ISSUES.md`

---

## Requested Canary Verdict

Please verify:

1. Static checks pass.
2. Docker Compose can resolve and start the stable/canary/proxy stack in a Docker-enabled environment.
3. `scripts/prd2-ha-smoke.sh` passes read-only health/proxy checks.
4. Optional gateway + DB evidence works when a safe test API key and database DSN are available.
5. Optional low-limit rate-limit smoke either passes or is explicitly skipped with reason.
6. Backup/restore rehearsal either passes against a prepared empty non-production DB or is explicitly skipped with environment reason.
7. Status docs do not overclaim production HA.

Conclusion should state whether Batch 21 can move to Sentinel, and whether any runtime-only items must be handed to Atlas.
