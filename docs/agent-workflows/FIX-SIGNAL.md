# FIX-SIGNAL.md - PRD4 Batch 1J Runtime Host Telemetry

**Fixer:** main agent
**Date:** file system timestamp is authoritative
**Risk level:** R2 user-visible Runtime Observability API/UI change
**Scope:** `GET /api/v1/runtime/status` host capacity telemetry and `/status` Runtime Observability UI

## Verification Request

Please verify PRD4 Batch 1J: Runtime Host Telemetry.

This batch moves Runtime Observability beyond the old
`telemetry_source_not_configured` placeholders for CPU, disk, and network:

- backend `GET /api/v1/runtime/status` now emits host CPU, disk, and network
  fields under `capacity_health`;
- frontend `/status` renders CPU, disk, and network as separate capacity cards;
- tests cover deterministic host telemetry without depending on the machine
  running Canary.

## Scope To Verify

Product files:

- `internal/admin/handlers.go`
- `internal/admin/runtime_status_handlers.go`
- `internal/admin/runtime_status_handlers_test.go`
- `frontend/src/pages/InternalStatusPage.tsx`
- `frontend/src/pages/__tests__/InternalStatusPage.test.tsx`

Expected backend behavior:

- `capacity_health.cpu` includes:
  - `status`;
  - `logical_cores`;
  - Linux CPU `usage_ratio` and `sample_millis` when `/proc/stat` is available;
  - `reason=usage_sample_not_supported` when CPU usage sampling is unavailable
    on the host OS.
- `capacity_health.disk` includes:
  - `status=available`;
  - `usage_ratio`;
  - `total_bytes`, `used_bytes`, `free_bytes`;
  - `mount_point="/"`.
- `capacity_health.network` includes:
  - `status`;
  - `interface_count`;
  - `active_interface_count`;
  - Linux `rx_bytes` / `tx_bytes` when `/proc/net/dev` is available;
  - `reason=byte_counters_not_supported` on hosts where byte counters are not
    available from `/proc/net/dev`.
- The handler remains partial-failure tolerant: DB/Redis/traffic/monitoring
  behavior is not changed.
- The new host telemetry provider is test-injectable and does not require
  environment-specific CPU/network state in unit tests.

Expected frontend behavior:

- Runtime Observability no longer has a single generic `主机遥测` card.
- It shows separate `CPU`, `磁盘`, and `网络` cards.
- CPU shows a usage percentage when present, otherwise a status tag and reason.
- Disk shows usage percentage plus used/total bytes and mount point.
- Network shows active/total interfaces plus rx/tx bytes when present.
- Prometheus/Grafana, app memory, DB/Redis, traffic, notification events, RBAC,
  routing, and API paths are unchanged.

## Non-Scope

Do not expect this batch to:

- add node exporter, cloud metadata, Kubernetes node inventory, or cluster
  topology;
- expose private IPs, hostnames, VPC/subnet IDs, cloud account identifiers, or
  security groups;
- add OpenTelemetry ingestion/export;
- create metric alert notification events;
- change Prometheus/Grafana probe behavior;
- change auth, RBAC, billing, routing, trace retention, SSO, upstream, or
  deployment behavior;
- mark any target-environment evidence as `Production verified`.

## Pre-Existing Local Closeout Docs

The worktree may also contain four R0 closeout status-pointer files from the
previous protocol gate:

- `docs/ops/CURRENT-STATUS.md`
- `docs/PROJECT-HANDOFF.md`
- `docs/DOC-INVENTORY.md`
- `docs/README.md`

Those files only change `Protocol Risk-Gated Workflow Update` from awaiting
Canary to PASS 10/10 after PR #93. They are not part of Batch 1J product
acceptance.

## Required Verification Commands

```bash
cd /Users/m4/Desktop/projects/UniRouter

rg -n "runtimeHostTelemetry|runtimeCPUStatus|runtimeDiskStatus|runtimeNetworkStatus|usage_ratio|logical_cores|active_interface_count|rx_bytes|tx_bytes" internal/admin frontend/src/pages/InternalStatusPage.tsx frontend/src/pages/__tests__/InternalStatusPage.test.tsx

go test ./internal/admin -run 'TestRuntimeStatus|TestLinuxCPUUsageRatio' -count=1
go test ./internal/admin -count=1
go test ./cmd/uniroute -count=1
go test ./... -count=1

npm --prefix frontend test -- --run InternalStatusPage client navigation
npm --prefix frontend test -- --run
npm --prefix frontend run build

git diff --check
```

## Acceptance Criteria

1. Runtime status response includes structured CPU, disk, and network telemetry
   fields instead of fixed `telemetry_source_not_configured` placeholders.
2. CPU telemetry is honest across platforms: Linux reports sampled usage when
   `/proc/stat` is available; other hosts expose logical cores and an explicit
   sampling reason instead of fake utilization.
3. Disk telemetry reports root filesystem total/used/free bytes and usage ratio.
4. Network telemetry reports total and active interface counts, with Linux byte
   counters when available.
5. Frontend `/status` renders CPU, disk, and network as separate cards with
   concrete values.
6. Existing Runtime Observability behavior for DB, Redis, app memory, traffic,
   Prometheus, and Grafana remains intact.
7. No secrets, private topology, private IPs, hostnames, cloud identifiers, or
   raw infrastructure inventory are exposed.
8. Focused backend/frontend tests, broad Go/frontend regression, frontend build,
   and `git diff --check` pass.
