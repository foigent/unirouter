# ADR 0001: PRD4 PlungeIn Ports

> Status: Accepted for PRD4 Batch 1F scope
> Date: 2026-07-07
> Owners: User, Codex
> Related: `docs/PRD-Phase4.md`, `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`,
> `docs/planning/PRD5-BACKLOG.md`,
> `docs/tech-ref/22-prd4-plungein-port-contract.md`

## Context

The User wants a long-term closed loop across Flow (MaaS), Trace
(observation), Identity, Policy, Govern, and Ontology. The same loop should stay
compatible with 2B deployments where customers already have their own identity,
monitoring, audit, or governance systems.

The current UniRouter code already has a strong `request_id` thread across logs,
billing, audit, and gateway behavior, but it does not have a runtime plugin
platform, sandbox, manifest, signing, or stable plugin API.

## Decision

PRD4 defines PlungeIn as a UniRouter architecture pattern, not an external
framework dependency.

PlungeIn ports are split into five classes:

| Port | Purpose | PRD4 Status |
|------|---------|-------------|
| Observe | Read request events, trace metadata, metrics, and status | Allowed |
| Act | Trigger notifications, webhooks, exports, or deep links | Allowed with guardrails |
| Enrich | Add labels, tags, ontology metadata, or classifications | Limited and non-authoritative |
| Decide | Produce policy advice such as allow/deny/throttle recommendations | Design-only or advisory in PRD4 |
| Mutate | Change auth, billing, routing, wallet, settlement, or provider dispatch | Deferred to PRD5 or later |

PRD4 implementation must remain observation-first. Core correctness paths for
authentication, authorization, billing, wallet, routing, settlement, and provider
dispatch remain owned by UniRouter core.

The normative PRD4 port contract is
`docs/tech-ref/22-prd4-plungein-port-contract.md`. That document defines allowed
Observe/Act/limited-Enrich behavior, advisory-only Decide behavior, forbidden
Mutate behavior, and the PRD5 reopen prerequisites for a runtime plugin platform.

## Consequences

Benefits:

- Gives the User's long-term closed-loop architecture a concrete entry point.
- Lets UniRouter integrate with customer monitoring/governance systems without
  becoming tightly coupled to one vendor.
- Avoids turning PRD4 into a high-risk arbitrary plugin runtime.

Tradeoffs:

- PRD4 integrations can observe and export more than they can control.
- Policy enforcement and route mutation require later explicit design.
- Some advanced customer automation will wait for PRD5.

## Alternatives Considered

- Full runtime plugin platform in PRD4: rejected because sandboxing, permission
  boundaries, signing, lifecycle, and secret safety are not ready.
- Hard-coded one-off integrations only: rejected because it does not support the
  User's long-term pluggable architecture.
- Direct OpenTelemetry-only dependency: rejected as the product architecture
  source of truth because OpenTelemetry can be an implementation/integration, but
  UniRouter still needs its own product concepts and access rules.

## Verification

- PRD4 docs define the port classes and PRD4/PRD5 boundary.
- `docs/tech-ref/22-prd4-plungein-port-contract.md` defines concrete allowed
  and forbidden examples for each port class.
- PRD4 trace/status work uses Observe/Act/limited Enrich semantics.
- No PRD4 code exposes arbitrary plugin mutation of auth, billing, routing,
  wallet, settlement, or provider dispatch.

## Rollback Or Exit Criteria

This ADR can be superseded if the User explicitly chooses a full plugin platform
as active scope and approves a security model covering manifest, sandbox,
permissions, signing, lifecycle, compatibility, secrets, and audit.
