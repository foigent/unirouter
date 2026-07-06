# FIX-SIGNAL.md - PRD4 Batch 0 Scope Baseline

**Date:** 2026-07-07
**Status:** Fixed awaiting Canary

## Context

User approved the PRD4 scope direction after discussion:

- PRD4 should focus on API key owner usage/cost, quota vocabulary,
  Flow/Event/Trace foundation, PlungeIn observation-first ports, Status Center
  runtime observability, external monitoring integration, real upstream evidence,
  real SSO evidence, auth UX hardening, production evidence promotion, and PRD5
  backlog consolidation.
- Full Scheduler V2, runtime plugin marketplace, Logto/IAM rewrite, managed-HA
  IaC product, budget hard-block enforcement, runtime cache, full SDK, new
  persistent roles, and public SaaS/commercial growth features should be deferred
  to PRD5 unless User reopens scope.

This batch is documentation-only. It must not change runtime code, API routes,
migrations, frontend behavior, deployment config, or accepted PRD2/PRD3 status.

## Files Changed

- `docs/PRD-Phase4.md`
- `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`
- `docs/planning/PRD5-BACKLOG.md`
- `docs/governance/adr/ADR-0001-prd4-plungein-ports.md`
- `docs/governance/adr/ADR-0002-prd4-api-key-owner-usage.md`
- `docs/governance/adr/ADR-0003-prd4-status-center-observability.md`
- routing/status updates in docs entrypoints and issue/status files

## Required Verification

1. PRD4 formal scope exists and is routed from current docs:

   ```bash
   rg -n "P4-F01|P4-F13|PRD5|PlungeIn|API key owner|Status Center|Production verified" docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/planning/PRD5-BACKLOG.md docs/governance/adr
   rg -n "docs/PRD-Phase4.md|PRD4-ACCEPTANCE-MATRIX|PRD5-BACKLOG" docs/README.md docs/DOC-INVENTORY.md docs/PROJECT-HANDOFF.md docs/ops/CURRENT-STATUS.md
   ```

2. PRD5 deferrals are explicit:

   ```bash
   rg -n "Full Scheduler V2|runtime plugin platform|Logto/IAM rewrite|Budget hard-block|Runtime cache|Full SDK|Public SaaS" docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/planning/PRD5-BACKLOG.md
   ```

3. PRD2 and PRD3 accepted states are not reopened or promoted:

   ```bash
   rg -n "PRD2 is frozen|PRD3 is complete|Production verified|target-environment" docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PRD-Phase4.md
   ```

4. Docs-only boundary holds:

   ```bash
   git diff --stat
   git diff --name-only
   git diff --check
   ```

   Expected changed paths are documentation paths only. No Go, TypeScript,
   migration, infra runtime config, or generated artifact changes are expected.

## Expected Result

Canary should mark PRD4 Batch 0 PASS only if:

- the new PRD4 scope docs are internally consistent;
- PRD5 deferrals are clear;
- no runtime implementation is claimed or changed;
- PRD2 remains frozen and PRD3 remains complete;
- `git diff --check` passes.
