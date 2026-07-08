# PRD4 Batch 1S Open Issue Boundary Closeout Evidence

> Date: 2026-07-08
> Scope: documentation/status-boundary only
> Risk level: R2 because issue status and PRD4/PRD5 scope boundaries changed
> Status: fixed awaiting Canary

## Purpose

Batch 1S closes the remaining PRD4 issue-boundary ambiguity after Batch 1R:

- `PRD4-PLUGIN-001` was still marked `Open` even though PRD4 intentionally
  implements only the PlungeIn port contract, not runtime plugin loading or a
  marketplace.
- `PRD4-DEPLOY-001` was still marked `Open` even though full
  Kubernetes/Helm/Terraform managed HA productization is outside PRD4.
- `PRD4-INFRA-001` was still marked `Open` even though PRD4 already accepted
  single-node Runtime Observability, active Prometheus/Grafana checks, and host
  CPU/disk/network summaries; the remaining work is multi-instance inventory,
  topology, collector integration, and metric-alert routing.
- `PRD4-DEPLOY-002` remains open because target test database cleanup requires
  Atlas target-environment confirmation and must not be performed by Codex from
  local repository context.

## Changes

| Area | Change |
|------|--------|
| `docs/issues/OPEN-ISSUES.md` | Adds `Deferred to PRD5` as an explicit issue status value and maps `PRD4-PLUGIN-001`, `PRD4-DEPLOY-001`, and `PRD4-INFRA-001` to PRD5 |
| `docs/planning/PRD5-BACKLOG.md` | Adds `PRD5-INFRA-001` for multi-instance infrastructure observability and topology |
| `docs/ops/PRD4-ACCEPTANCE-MATRIX.md` | Records Batch 1S as fixed awaiting Canary and adds a Batch 1S verification section |
| `docs/PRD-Phase4.md` | Clarifies that multi-instance infrastructure inventory/topology/collectors/alerts are not PRD4 implementation targets |
| Status/index docs | Route the active verification signal to Batch 1S while preserving Batch 1R as the latest completed verification |

## Boundary

Batch 1S does not change:

- Go source;
- frontend source;
- migrations;
- API routes;
- runtime behavior;
- deployment scripts;
- evidence harness code;
- target infrastructure;
- database contents;
- any `Production verified` claim.

## Expected Current PRD4 Open Set

After Batch 1S, the only PRD4 row that should remain truly `Open` in
`docs/issues/OPEN-ISSUES.md` is:

- `PRD4-DEPLOY-002`: target test database cleanup remains pending Atlas.

The following are no longer active PRD4 implementation gaps:

- `PRD4-PLUGIN-001`: deferred to `PRD5-PLUGIN-001` and `PRD5-MARKET-001`;
- `PRD4-DEPLOY-001`: deferred to `PRD5-DEPLOY-001`;
- `PRD4-INFRA-001`: deferred to `PRD5-INFRA-001`.

## Local Verification

Codex should run before handoff:

```bash
rg -n "PRD4-PLUGIN-001|PRD4-DEPLOY-001|PRD4-INFRA-001|PRD4-DEPLOY-002|PRD5-PLUGIN-001|PRD5-MARKET-001|PRD5-DEPLOY-001|PRD5-INFRA-001|Batch 1S|PRD4-B1S-open-issue-boundary-closeout-20260708" docs/issues/OPEN-ISSUES.md docs/planning/PRD5-BACKLOG.md docs/ops/evidence/PRD4-B1S-open-issue-boundary-closeout-20260708.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/PRD-Phase4.md docs/agent-workflows/FIX-SIGNAL.md
awk -F'|' '/^\| P4-F[0-9]+ / {gsub(/^ +| +$/, "", $4); count[$4]++; total++} END {print "total=" total; for (s in count) print s "=" count[s]}' docs/ops/PRD4-ACCEPTANCE-MATRIX.md
git diff --check
```

Expected matrix count remains:

```text
total=13
Canary passed=11
Deferred to PRD5=2
```
