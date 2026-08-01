# Async Multimodal Threat Model

> Scope: P7-D03 pre-gate design evidence
> Implementation claim: none
> Required before runtime: product decisions, security review and target proof

## Assets And Trust Boundaries

Future media execution would cross at least six independently authorized
boundaries:

1. client/control-plane admission;
2. tenant-scoped metadata and object authorization;
3. object store and content-processing boundary;
4. queue, lease and worker ownership;
5. provider request/callback boundary;
6. billing, terminal evidence, retention and deletion.

Provider credentials, raw prompts/media, object locators, generated outputs,
tenant identity, job state, provider usage and customer debt have different
owners and must not be collapsed into one high-privilege worker or plugin.

## Input And Actor Assumptions

- Client-supplied prompts, media, object references, content metadata,
  cancellation requests and idempotency keys are attacker-controlled until
  authenticated, tenant-authorized and validated.
- Provider bodies and callbacks are external/untrusted even when they arrive
  from an expected network; authenticity, job binding and replay protection
  must be proven separately.
- Provider/store selection, retention, billing rules, regional policy and
  trust roots are operator-controlled configuration requiring versioned audit
  and rollback.
- Schemas, fixtures and adapter code are developer-controlled supply-chain
  inputs. Their repository presence is not runtime authorization.
- The current B6 image profile, strict JSON/canonical hash utilities and
  metadata-only storage policy are useful controls, but none implements a
  media job runtime or closes P7-D03.

## Required Threat Classes

| Threat class | Failure mode | Mandatory runtime control before promotion |
|--------------|--------------|--------------------------------------------|
| `cross_tenant_object_reference` | A valid-looking object reference is read or attached from another org/project | Authorize every create/read against immutable org/project ownership; bind job and object tenant scope; deny on missing ownership |
| `mutable_object_substitution` | Content behind a reference changes after admission | Immutable object version plus content digest verification at every trust-boundary handoff |
| `signed_url_or_locator_leakage` | Bucket keys or signed URLs leak through API, UI, logs, receipts or callbacks | Opaque external references; worker-only short-lived locators; structured metadata-only logs and redaction |
| `raw_media_or_prompt_persistence` | Sensitive content is retained implicitly by jobs, traces, caches or analytics | Default no plaintext/raw persistence; explicit opt-in purpose, encryption, RBAC, audit-on-read, export/purge controls and approved retention |
| `lease_split_brain_and_duplicate_dispatch` | Two workers dispatch the same billable job | Durable CAS state, fencing token, idempotent provider strategy, bounded lease renewal and duplicate outcome reconciliation |
| `cancel_complete_race_and_double_charge` | Cancellation and provider completion race, causing lost output, double charge or false refund | Orthogonal execution/cancellation/settlement facts; preserve provider outcome; one terminal financial authority; never infer refund from cancel intent |
| `malformed_media_and_decompression_bomb` | Crafted media exhausts memory/CPU or exploits parser/transcoder | MIME sniffing, byte/pixel/duration limits, sandboxed decoders, bounded transforms, malware policy and fail-closed validation |
| `provider_callback_spoofing` | Forged/replayed callback advances state or settlement | Fixed callback registry, signature or mTLS verification, timestamp/nonce replay defense, provider/job binding and idempotent state transition |
| `retention_purge_failure` | Deleted/expired content remains accessible or derived copies survive | Versioned retention policy, tombstone and purge workflow, deletion retries, audit evidence, legal-hold separation and backup/derivative coverage |

The B13 fixtures enumerate all nine identifiers exactly. This proves threat
coverage in the contract, not that any mitigation is implemented.

## Additional Design Requirements

- Provider egress must be fixed-registry and tenant/policy authorized; media
  references must never become arbitrary SSRF targets.
- Safety/moderation outcomes and provider policy errors are not billing or
  delivery finality.
- Preview thumbnails, derivatives and transcodes inherit tenant, retention and
  deletion policy; they are not untracked convenience copies.
- Customer-visible status must distinguish queued, leased, running, provider
  outcome, cancellation intent, output availability and financial settlement.
- Observability must use bounded metadata. Raw content, locators, credentials
  and provider response bodies are excluded from logs, metrics and shared
  events by default.
- A worker compromise must not grant UniRouter IAM, routing or global billing
  mutation. Worker capabilities must be short-lived, tenant/job scoped and
  attenuation-only.

## P7-D03 Exit Evidence

Before runtime implementation, the product decision must name:

- provider and exact operations/capabilities;
- object store, region, encryption and ownership model;
- retention defaults, deletion SLA, legal hold and customer controls;
- charge trigger, cancellation race, provider-liability and refund rules.

The technical follow-up must then add versioned state transitions, fencing and
replay tests, content-validation limits, callback authentication, retention
and purge tests, billing/finality fixtures, operational rollback and
representative target evidence. B13 alone authorizes none of those actions.

## Severity Calibration

- **Critical:** cross-tenant raw media access, provider credential compromise,
  arbitrary customer-debt creation, or worker authority that can mutate global
  IAM/routing/billing.
- **High:** forged callbacks, duplicate billable dispatch, mutable-object
  substitution, failed deletion that leaves sensitive media accessible, or
  sandbox escape from media processing.
- **Medium:** bounded tenant-local denial of service, metadata/locator leakage
  without raw content, or cancellation/status inconsistencies that do not
  alter financial authority.
- **Low:** fixture/report inconsistencies that cannot reach a registered
  runtime and expose no secret, tenant data, authority or financial effect.

Severity depends on reachability. B13 artifacts have no registered runtime,
network, credentials, persistence or billing path, so they are design evidence
rather than proof of an exploitable production surface.
