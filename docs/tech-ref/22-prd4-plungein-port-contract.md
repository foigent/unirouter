# PRD4 PlungeIn Port Contract

> Version: v0.1  
> Scope: PRD4 Batch 1F architecture contract only. No runtime plugin loading,
> marketplace, sandbox, or third-party installation is implemented by this
> document.

## Product Intent

PlungeIn is UniRouter's architecture pattern for connecting the long-term loop:
Flow (MaaS), Trace (observation), Identity, Policy, Govern, and Ontology.

In PRD4, PlungeIn is not an external dependency or plugin framework. It is a
set of named port boundaries that let UniRouter integrate with customer
monitoring, governance, identity, and evidence systems without tightly coupling
core gateway correctness to those systems.

## Port Classes

| Port | PRD4 Status | Allowed Inputs | Allowed Outputs | Hard Boundary |
|------|-------------|----------------|-----------------|---------------|
| Observe | Allowed | Request trace metadata, call logs, audit metadata, runtime status, metrics summaries, notification events | Read-only views, exports, evidence summaries, external monitoring summaries | Must not change auth, billing, routing, wallet, settlement, provider dispatch, or request payloads |
| Act | Allowed with guardrails | Event/status changes, operator actions, evidence export requests | Notifications, webhooks, deep links, export jobs, operator-visible evidence | Must be idempotent where retried and must not be part of the synchronous correctness path |
| Enrich | Limited and non-authoritative | Existing trace/log/status records, model/project/user/key metadata | Labels, tags, classifications, ontology hints, risk annotations | Must not override source-of-truth fields or become an access-control, billing, or routing decision |
| Decide | Advisory only in PRD4 | Policy context, quota/usage summaries, trace/runtime summaries | Recommendations such as allow, deny, throttle, route, review, or escalate | Recommendations are not enforced unless a later approved policy phase implements enforcement |
| Mutate | Deferred to PRD5 or later | Not available as a PRD4 PlungeIn runtime port | Not available as a PRD4 PlungeIn runtime port | No PRD4 port may mutate auth, authorization, billing, wallet, settlement, routing, provider dispatch, schema ownership, or secrets |

## PRD4 Allowed Examples

Observe examples:

- Read `request_trace_events` and call-log metadata to explain a request path.
- Read runtime status, DB/Redis health, traffic summaries, and monitoring
  configuration state.
- Read usage and cost summaries through existing RBAC-controlled APIs.
- Read Prometheus/Grafana integration state when configured.

Act examples:

- Send notification/webhook events that already pass through the notification
  event foundation.
- Open configured Grafana/Prometheus deep links.
- Generate evidence files or export summaries when an operator explicitly
  invokes them.

Limited Enrich examples:

- Attach non-authoritative labels such as `suspected_retry_loop`,
  `provider_rate_limited`, or `needs_operator_review`.
- Add ontology hints that help future governance search, provided they do not
  change the original request, cost, route, or access result.

Decide examples:

- Produce advisory policy recommendations for future review.
- Produce quota or capacity recommendations for operators.
- Record "would deny" or "would throttle" shadow-mode results only as evidence.

## PRD4 Forbidden Examples

PRD4 PlungeIn work must not implement:

- dynamic code loading, package installation, plugin manifests, plugin signing,
  sandboxing, or marketplace distribution;
- arbitrary third-party plugins running inside the gateway process;
- hooks that change authentication, authorization, key validation, wallet
  balance, billing settlement, channel selection, provider dispatch, retry
  behavior, or request/response bodies;
- prompt/response plaintext capture or storage;
- raw API keys, upstream credentials, IdP tokens, private topology, cloud account
  identifiers, or unredacted secrets in evidence outputs.

## Data And Security Contract

All PRD4 PlungeIn ports inherit the following rules:

- RBAC must be evaluated by UniRouter core before data is exposed.
- Prompt and response plaintext remain disabled unless a later opt-in product
  scope is approved.
- Evidence must use redacted identifiers or scoped metadata where full values are
  not required.
- Missing or redacted data must be represented explicitly instead of silently
  fabricating values.
- Core source-of-truth tables remain owned by their existing domain code.
- Future code-level ports must be typed, explicit, registered in core code, and
  covered by tests; this document does not authorize dynamic runtime loading.

## Relationship To Current PRD4 Work

| PRD4 Workstream | PlungeIn Port Fit | Notes |
|-----------------|-------------------|-------|
| Usage and cost attribution | Observe | Reads key-owner usage/cost summaries through existing RBAC |
| Quota vocabulary | Observe / advisory Decide | Displays quota terms and can later support recommendations; no enforcement in Batch 1E/1F |
| Flow/Event/Trace | Observe | Request path evidence is the primary Observe substrate |
| Runtime Observability | Observe / Act | Runtime summaries are Observe; deep links and notifications are guarded Act |
| External monitoring | Observe / Act | Monitoring state is Observe; external dashboard links are guarded Act |
| Real upstream evidence | Observe / Act | Evidence collection observes provider behavior and may export evidence |
| Real SSO evidence | Observe / Act | Evidence collection observes identity flow and audit behavior |
| Security/redaction guardrails | Applies to all ports | Defines what each port may reveal |

## PRD5 Reopen Boundary

Moving from PRD4 PlungeIn ports to a runtime plugin platform requires a separate
PRD5 decision covering:

- plugin manifest schema;
- package signing and trust;
- sandbox or isolation model;
- permission model;
- lifecycle, install, disable, uninstall, and rollback behavior;
- version compatibility and API deprecation policy;
- secret isolation;
- plugin audit events;
- operator support and incident response.

Until those prerequisites are accepted, PRD4 PlungeIn remains an architecture
and static-integration boundary, not a runtime plugin product.
