# FIX-SIGNAL.md - PRD4 Batch 1F PlungeIn Port Architecture

**Date:** 2026-07-07
**Status:** Fixed awaiting Canary
**Issue:** `PRD4-PLUNGEIN-001`
**Matrix Row:** `P4-F05`

## Verification Request

Please verify PRD4 Batch 1F: PlungeIn Port Architecture.

This batch is documentation-only. It accepts the PRD4 PlungeIn ADR scope and
adds a concrete port contract for Observe, Act, limited Enrich, advisory Decide,
and forbidden Mutate behavior. It does not implement runtime plugin loading,
manifest handling, sandboxing, marketplace distribution, third-party
installation, or mutation of core auth/billing/routing/wallet/settlement/provider
dispatch.

## Scope To Verify

Docs/architecture:

- `docs/governance/adr/ADR-0001-prd4-plungein-ports.md` is accepted for PRD4
  Batch 1F scope and links to the normative port contract.
- `docs/tech-ref/22-prd4-plungein-port-contract.md` defines:
  - Observe as read-only request/trace/status/usage/runtime/monitoring access.
  - Act as guarded notifications, webhooks, deep links, and evidence exports.
  - Enrich as limited, non-authoritative labels/classifications/ontology hints.
  - Decide as advisory-only recommendations in PRD4.
  - Mutate as forbidden/deferred for auth, authorization, billing, wallet,
    settlement, routing, provider dispatch, request/response bodies, schema
    ownership, and secrets.
- PRD5 backlog boundaries remain explicit for the full runtime plugin platform,
  plugin marketplace, sandbox, signing, lifecycle, permissions, and third-party
  installation.
- Documentation indexes route readers to the new port contract.

Status:

- Records User-reported Batch 1E Canary/Sentinel completion after `a1acdb6` /
  PR #88.
- Marks `P4-F03` / `PRD4-QUOTA-001` Canary passed.
- Marks `P4-F05` / `PRD4-PLUNGEIN-001` fixed awaiting Canary.
- Keeps `PRD4-PLUGIN-001` open because full runtime plugin platform remains
  unimplemented and deferred to PRD5.

## Non-Scope

Do not expect this batch to implement:

- Any Go, frontend, migration, API route, OpenAPI, deployment, Docker, or runtime
  behavior change.
- Runtime plugin loading, dynamic code loading, package installation, plugin
  manifest parsing, plugin signing, sandboxing, marketplace distribution, or
  third-party install/uninstall.
- Mutating hooks for authentication, authorization, billing, wallet, settlement,
  routing, provider dispatch, retry behavior, or request/response bodies.
- Prompt/response plaintext storage or new secret exposure.
- Scheduler V2, budget hard-blocking, real upstream evidence, or real SSO
  evidence.

## Required Verification Commands

```bash
rg -n "PlungeIn|Observe|Act|Enrich|Decide|Mutate|PRD4 Batch 1F|PRD4-PLUNGEIN-001|P4-F05" docs/tech-ref/22-prd4-plungein-port-contract.md docs/governance/adr/ADR-0001-prd4-plungein-ports.md docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/tech-ref/README.md
rg -n "runtime plugin platform|manifest|dynamic loading|sandbox|marketplace|third-party|Mutate|auth|billing|routing|wallet|settlement|provider dispatch" docs/tech-ref/22-prd4-plungein-port-contract.md docs/governance/adr/ADR-0001-prd4-plungein-ports.md docs/planning/PRD5-BACKLOG.md docs/issues/OPEN-ISSUES.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md
git status --short
git diff --check
```

## Acceptance Criteria

1. ADR 0001 is accepted for PRD4 Batch 1F scope and points to the technical port
   contract.
2. The port contract clearly distinguishes Observe, Act, Enrich, Decide, and
   Mutate.
3. PRD4 allowed behavior remains observation-first; Act is guarded; Enrich is
   non-authoritative; Decide is advisory only.
4. Mutating core auth, authorization, billing, wallet, settlement, routing,
   provider dispatch, request/response bodies, schema ownership, or secrets is
   explicitly forbidden/deferred.
5. Full runtime plugin platform, marketplace, sandbox, manifest, signing,
   lifecycle, permissions, and third-party installation remain deferred to PRD5.
6. Docs consistently show Batch 1E as Canary/Sentinel complete per User report
   and Batch 1F as fixed awaiting Canary.
7. No runtime code, migration, API route, OpenAPI, deployment config, frontend,
   or Docker files changed in Batch 1F.
8. Documentation grep and `git diff --check` pass.
