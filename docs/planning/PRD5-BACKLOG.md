# PRD5 Backlog

> Date: 2026-07-07
> Status: Deferred backlog for capabilities intentionally not implemented in
> PRD4 first scope.
> Source: PRD4 scope confirmation and earlier PRD2/PRD3 deferred boundaries.

## Purpose

This file records important capabilities that remain valuable but are too large,
high-risk, environment-dependent, or strategically premature for PRD4. Listing an
item here prevents PRD4 labels or partial implementation from implying that the
capability is already accepted.

## Deferred Items

| ID | Capability | Why Not PRD4 | Prerequisites To Reopen |
|----|------------|--------------|--------------------------|
| PRD5-SCHED-001 | Full Scheduler V2 | Rewrites gateway dispatch, capacity reservation, model-family queues, priority fairness, and error semantics | Stable PRD4 usage/cost, quota visibility, request trace data, capacity health, and clear rollout/shadow mode |
| PRD5-PLUGIN-001 | Runtime plugin platform | Requires manifest schema, runtime loading, sandboxing, permissions, signing, lifecycle, version compatibility, and secret isolation | Accepted PlungeIn port ADR, stable core events, security model, plugin API compatibility rules |
| PRD5-MARKET-001 | Plugin marketplace / third-party installation | Adds packaging, trust, distribution, update, license, and support obligations | Runtime plugin platform, signing infrastructure, review policy, install/uninstall rollback |
| PRD5-IAM-001 | Logto/IAM rewrite into UniRouter core | Would make UniRouter responsible for a full identity platform, including SAML/SCIM/MFA/social login/security patching | Clear decision to become an identity platform, license review, migration plan, long-term patch process |
| PRD5-DEPLOY-001 | Kubernetes/Helm/Terraform managed HA product | Target-stack dependent and requires production-grade DB/Redis HA, PITR, off-server backup, and failover testing | Chosen target stack, cloud assumptions, secret management, backup encryption, restore/failover drills |
| PRD5-BUDGET-001 | Budget hard-block enforcement | Changes admission, wallet, stream settlement, idempotency, reconciliation, and user-facing error behavior | PRD4 policy model, billing trace, concurrency safety tests, refund/cancel/timeout invariants |
| PRD5-CACHE-001 | Runtime cache / prompt-response plaintext storage | High privacy, tenant-isolation, retention, billing, and correctness risk | Explicit opt-in model, encryption, retention, purge/export/delete, audit-on-read, billing rules |
| PRD5-SDK-001 | Full official SDK suite | Freezes API details too early if typed response schemas are still evolving | Stable OpenAPI response schemas, semver policy, generated SDK tests, compatibility commitments |
| PRD5-ROLE-001 | New persistent roles and service accounts | Requires authority surfaces beyond current roles and project membership | Role taxonomy, permission matrix, route guard tests, service-account credential model, audit model |
| PRD5-OPS-001 | Full maintenance/SLA/SLO incident operations | Needs incident lifecycle, planned maintenance, communication workflow, SLO policy, and notification governance | PRD4 Status Center baseline, operator roles, notification reliability, customer-visible policy |
| PRD5-ONTOLOGY-001 | Ontology-driven governance loop | Requires stable Flow, Trace, Identity, Policy, and Governance entities before ontology can control product behavior | PRD4 trace/event model, identity attribution, policy model, governance evidence model |
| PRD5-SAAS-001 | Public SaaS, reseller, payment, invoice, coupons, campaigns, support CRM | Shifts product direction away from local-deploy enterprise MaaS | Explicit User product-direction change and separate SaaS/commercial PRD |

## PRD4 Relationship

PRD4 may design prerequisites for these items, but must not imply that they are
implemented:

- PRD4 Flow/Event/Trace can feed Scheduler V2 later.
- PRD4 PlungeIn Observe/Act/Enrich ports can prepare plugin boundaries later.
- PRD4 external Logto compatibility can prepare an optional identity deployment
  pattern later.
- PRD4 Status Center can prepare SLA/SLO and full incident operations later.
- PRD4 API key owner usage can prepare budget hard-blocking later.

## Reopen Rule

A PRD5 item can move into active planning only when:

- the User explicitly chooses it as current scope;
- current repo docs/code are re-read before planning;
- prerequisites and blast radius are documented;
- acceptance evidence is defined before implementation starts;
- deferred PRD4 boundaries are not silently rewritten.
