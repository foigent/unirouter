# PRD5 Deferred Control Designs

> Date: 2026-07-10
> Status: Design complete; runtime implementation remains deferred
> Scope: budget hard blocking, service accounts, and incident/SLO operations

## Decision Boundary

P5-F12 accepted design work for these controls but did not authorize runtime
authority changes. This document completes the repository-owned design and test
contract. It does not change admission, wallet settlement, credentials, roles,
notifications, maintenance state, or customer-facing SLA claims.

## Budget Hard Block

### State machine

| State | Entry fact | Allowed transition | Required evidence |
|---|---|---|---|
| `open` | no active hard limit or remaining budget is positive | `reserved`, `blocked` | policy version, observed balance, request ID |
| `reserved` | an idempotent worst-case reservation exists | `settled`, `released`, `expired` | reservation ID, amount, owner, TTL |
| `blocked` | admission atomically proved the limit would be exceeded | `open` after policy/balance change | stable error code, policy ID, observed amount |
| `settled` | final usage replaced the reservation | terminal | usage source, final amount, settlement ID |
| `released` | request failed/cancelled before settlement | terminal | failure/cancel reason and release ID |
| `expired` | reconciler released an abandoned reservation | terminal | lease expiry and reconciliation audit |

Required invariants:

- reservation and admission must be one database transaction or one equivalent
  compare-and-set operation;
- `(org_id, project_id, request_id)` is the idempotency key;
- stream timeout, client cancellation, provider failure, retry exhaustion, and
  duplicate settlement cannot leave spend reserved twice;
- a hard block returns a stable machine error and emits an audit/trace decision;
- soft notification thresholds remain separate from hard admission state.

Runtime implementation remains deferred until the User explicitly chooses
budget scope (organization, project, key, or combinations) and overshoot policy.

Acceptance evidence for a future implementation: concurrent admission tests,
duplicate request tests, stream cancel/timeout tests, retry/failover tests,
settlement/release reconciliation, and target database fault-injection evidence.

## Service Accounts

### Taxonomy

| Principal | Purpose | Credential | Minimum permissions |
|---|---|---|---|
| human platform admin | approve and reconcile cross-system exchange | current admin session | Data Exchange Control read/write |
| future exchange service account | register a generated artifact or delivery from an approved offline job | dedicated rotatable credential | exchange ledger write only; no user/admin CRUD |
| future receipt service account | register a validated downstream receipt | dedicated rotatable credential | receipt create only; no artifact access |

Required invariants:

- service accounts are non-interactive principals with explicit owner, purpose,
  expiry, rotation, disable, last-used, and audit fields;
- credentials are shown once, stored hashed, scoped to an organization, and
  denied all routes not explicitly granted;
- Data Exchange Control remains `platform_admin` only until this model is
  implemented and tested;
- no service-account credential is shared through the cooperation folder.

Runtime implementation remains deferred because the current Data Exchange MVP
does not run an exporter/outbox or receive X-Insight callbacks.

Acceptance evidence for a future implementation: permission matrix tests for
every route, credential rotation/disable tests, cross-org denial, audit actor
identity, expiry behavior, and secret-leak scanning.

## Incident And SLO Operations

### Minimal operating model

| Phase | Required owner action | Evidence |
|---|---|---|
| detect | acknowledge a notification/runtime degradation | incident ID, source event IDs, first observed time |
| contain | stop unsafe exchange/deployment activity without changing evidence | action, actor, reason, affected scope |
| recover | execute an approved runbook and verify health/readback | commands without secrets, checks, timestamps |
| close | record impact, residual gaps, and follow-up issue IDs | close time, owner, evidence links |

SLO evidence may report measured availability/latency/error data for a named
window, but UniRouter must not publish a customer SLA from local tests or a
single target smoke run. Maintenance scheduling, customer communication, alert
escalation, and error-budget policy remain separate runtime/product work.

Acceptance evidence for a future implementation: incident lifecycle API/UI,
RBAC, notification delivery/retry proof, maintenance state transitions,
immutable audit, and a target observation window long enough to support the
stated SLO.

## Current Completion

The designs, invariants, dependencies, risks, and acceptance evidence are now
explicit. No runtime row is promoted beyond `deferred` by this document.
