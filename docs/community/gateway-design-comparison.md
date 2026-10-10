# Gateway design comparison

[Why UniRouter](why-unirouter.md) · [设计理念](why-unirouter.zh-CN.md)

This review supports the project's design rationale. It compares selected
implementation paths and official documentation, rather than ranking entire
products. A mechanism visible in source is not proof of deployment reliability,
performance superiority or exclusive functionality.

## Review baseline

Reviewed on **2026-10-10**. The source snapshots are:

| Project | Inspected source |
| --- | --- |
| UniRouter | [c5958c5](https://github.com/foigent/unirouter/tree/c5958c57106e7073e74653652b38dc549f482e10), 1.10.0 open-source candidate |
| LiteLLM | [15154a4](https://github.com/BerriAI/litellm/tree/15154a40a83fe2fdf6ca9e8f5b3ba438056a5bd9), upstream development snapshot |
| New API | [c6741c3](https://github.com/QuantumNous/new-api/tree/c6741c36a9a1a729554ad94ee99f21602a214a23), upstream development snapshot |

These are not three equivalent released deployments. UniRouter's candidate
was inspected separately from the older documentation-authoring checkout.
Official web documentation can change independently of these commits. No
comparative benchmark, crash experiment, provider call or live deployment
verification was performed in this review. Runtime tests were not rerun.
UniRouter separately passed its [CI][ur-ci] and [protocol checks][ur-protocol]
on the listed commit; those results are not comparative runtime evidence.
Enterprise editions and optional extensions were not exhaustively evaluated.
Current publication status is tracked in [release readiness](release-readiness.md).

## Shared capabilities

Calling LiteLLM or New API a transparent forwarding layer would be inaccurate.
LiteLLM documents routing, retries, budget enforcement, spend tracking and
guardrail integrations. New API implements channel configuration, quota
accounting, wallet/subscription funding and a billing lifecycle. Both retain
request records. Self-hosting, API compatibility, cost visibility and local
records are therefore insufficient claims of differentiation.

Sources: LiteLLM's [request architecture][ll-architecture],
[budget controls][ll-budgets] and [guardrails][ll-guardrails]; New API's
[channels][na-channel], [billing session][na-session] and [request logs][na-log].

## Where the inspected designs differ

### Execution configuration and admission

LiteLLM organizes deployments into model groups and provides routing, retry and
fallback policies. New API's channel model includes provider type, endpoint,
models, mappings, priority, weights and test state. These are substantial
configuration and routing mechanisms. See the
[LiteLLM architecture][ll-architecture] and [New API channel model][na-channel].

UniRouter's canonical path adds a particular execution contract: it validates
the selected release and sealed route manifest, checks applicable qualification
and profile bindings, then commits the route claim and provider-attempt record
before returning dispatch authority. An ambiguous transaction commit has an
owned-record readback path. See [routing eligibility][ur-routing] and the
[execution coordinator][ur-coordinator].

**Potential value:** operators can investigate which approved configuration
authorized an attempt, including after configuration changes. **Tradeoff:**
qualification and release preparation add work, and dispatch depends on a
database transaction. This review does not establish that competitors cannot
implement comparable controls, or that this path covers arbitrary provider
operations.

### Reservation, settlement and recovery

LiteLLM already reserves estimated cost before supported calls, reconciles it
afterward and handles cancellation. Its implementation uses spend counters,
reservation leases and database-based reseeding. Optional fail-closed behavior
and operation/pricing coverage matter. Spend persistence is described as
asynchronous in its architecture. It is incorrect to infer that asynchronous
logging means no pre-call budget protection. Sources:
[reservation implementation][ll-reservation], [budget documentation][ll-budgets]
and [request architecture][ll-architecture].

New API's `BillingSession` handles pre-consumption, settlement and refunds.
Funding and token quota adjustments are separate steps, with explicit handling
for partial completion. Wallet funding has an atomic reservation; subscription
refunds have request-ID-based idempotency. The session also uses in-process
state and asynchronous refund work. This is a description of the reviewed
path, not a finding that all its accounting lacks transactions or persistence.
Sources: [billing session][na-session] and [funding implementations][na-funding].

UniRouter persists reservation identity, execution binding, billing mode and
pricing basis. Its settlement writes the financial result and an outgoing
journal event in the same PostgreSQL transaction; the application wires this
outbox into billing. Pending settlements have a leased recovery path.
Sources: [billing lifecycle][ur-billing], [outbox][ur-outbox],
[application wiring][ur-main] and [recovery][ur-recovery].

**Potential value:** a durable basis for explaining charges and recovering
unfinished financial work. **Tradeoff:** more persistent state, recovery logic
and operational responsibility. These source differences do not prove lower
failure rates, universal exactly-once execution or supplier-invoice accuracy.

### Meaning and export of records

LiteLLM's spend schema includes request, deployment, organization, session and
metadata fields, with configurable prompt/response retention. New API records
request and upstream IDs, channel, tokens and quota; its text billing path also
passes pricing ratios and other calculation details into consumption logs.
Neither should be described as retaining only an opaque final number.
Sources: [LiteLLM schema][ll-schema] and [log construction][ll-logs];
[New API logs][na-log] and [text accounting][na-text].

UniRouter explicitly distinguishes usage authority, purpose and finality, and
calculated amounts from charged amounts. Its source-fact contract separates
context, identity and cost basis, with scoped identities, revisions,
predecessors, correction reasons and collection status. Financial references
point back to UR-owned facts. These contracts give an asynchronous consumer
such as X-Insight more explicit semantics to preserve. Sources:
[usage definitions][ur-metering], [billing][ur-billing] and
[source-fact contract][ur-facts].

**Potential value:** investigations can preserve the difference between a
provider report, an estimate, a charge and missing evidence. **Tradeoff:** richer
contracts require consumer support and version maintenance. The existence of
an export or an X-Insight connector is not itself a unique advantage, nor proof
that cross-system business intent can already be reconstructed.

## Limits that must remain visible

- **Cost observation is the example default.** `cost_observation` and
  `prepaid_enforced` are supported billing modes; `budget_enforced` is reserved
  but explicitly rejected. This concerns that billing-mode contract, not the
  absence of every other quota or budget control. See [configuration][ur-env]
  and [mode validation][ur-mode].
- **Controlled capture is not comprehensive data-loss prevention.** UR supports
  disabled, metadata-only and explicitly enabled full-content policies for
  supported paths. Full-content policy requires finite retention and byte
  limits, with `human_review` as its allowed purpose. The inspected policy
  rejects a redaction reference. Body access is separately authorized and
  rechecked. LiteLLM also offers guardrails and configurable content retention;
  content governance must not be presented as UR-only. See
  [UR capture policy][ur-policy], [body authority][ur-body],
  [LiteLLM guardrails][ll-guardrails] and [retention control][ll-logs].
- **Delivery readiness is separate from source capability.** The inspected
  UR candidate's [release-readiness record][ur-readiness] still lists installation,
  public artifact and external-fork reproducibility work. Historical Target
  evidence applies to its recorded versions and environments. This review does
  not certify current production status or compare adoption and ecosystem size.

## A defensible product claim

UniRouter is designed to help enterprises inspect the execution basis, usage
source and financial outcome of model calls. Its qualification, durable
accounting and source-fact mechanisms support that direction. Their practical
advantage depends on whether a deployment needs those guarantees enough to
accept their operational cost.

Performance, recovery effectiveness, onboarding effort and investigation
quality still need matched scenarios and runtime evidence before they become
comparative superiority claims. Automatic business ontology and workflow
optimization remain research directions.

[ll-architecture]: https://docs.litellm.ai/docs/proxy/architecture
[ll-budgets]: https://docs.litellm.ai/docs/proxy/users
[ll-guardrails]: https://docs.litellm.ai/docs/proxy/guardrails/quick_start
[ll-reservation]: https://github.com/BerriAI/litellm/blob/15154a40a83fe2fdf6ca9e8f5b3ba438056a5bd9/litellm/proxy/spend_tracking/budget_reservation.py
[ll-schema]: https://github.com/BerriAI/litellm/blob/15154a40a83fe2fdf6ca9e8f5b3ba438056a5bd9/litellm/proxy/schema.prisma
[ll-logs]: https://github.com/BerriAI/litellm/blob/15154a40a83fe2fdf6ca9e8f5b3ba438056a5bd9/litellm/proxy/spend_tracking/spend_tracking_utils.py
[na-channel]: https://github.com/QuantumNous/new-api/blob/c6741c36a9a1a729554ad94ee99f21602a214a23/model/channel.go
[na-session]: https://github.com/QuantumNous/new-api/blob/c6741c36a9a1a729554ad94ee99f21602a214a23/service/billing_session.go
[na-funding]: https://github.com/QuantumNous/new-api/blob/c6741c36a9a1a729554ad94ee99f21602a214a23/service/funding_source.go
[na-log]: https://github.com/QuantumNous/new-api/blob/c6741c36a9a1a729554ad94ee99f21602a214a23/model/log.go
[na-text]: https://github.com/QuantumNous/new-api/blob/c6741c36a9a1a729554ad94ee99f21602a214a23/service/text_quota.go
[ur-routing]: https://github.com/foigent/unirouter/blob/c5958c57106e7073e74653652b38dc549f482e10/internal/modelrouting/evaluate.go
[ur-coordinator]: https://github.com/foigent/unirouter/blob/c5958c57106e7073e74653652b38dc549f482e10/internal/modelexecution/coordinator.go
[ur-billing]: https://github.com/foigent/unirouter/blob/c5958c57106e7073e74653652b38dc549f482e10/internal/billing/billing.go
[ur-outbox]: https://github.com/foigent/unirouter/blob/c5958c57106e7073e74653652b38dc549f482e10/internal/billing/outbox.go
[ur-main]: https://github.com/foigent/unirouter/blob/c5958c57106e7073e74653652b38dc549f482e10/cmd/uniroute/main.go
[ur-recovery]: https://github.com/foigent/unirouter/blob/c5958c57106e7073e74653652b38dc549f482e10/internal/billing/settlement_recovery.go
[ur-metering]: https://github.com/foigent/unirouter/blob/c5958c57106e7073e74653652b38dc549f482e10/internal/metering/metering.go
[ur-facts]: https://github.com/foigent/unirouter/blob/c5958c57106e7073e74653652b38dc549f482e10/internal/sourcefacts/contract.go
[ur-env]: https://github.com/foigent/unirouter/blob/c5958c57106e7073e74653652b38dc549f482e10/.env.example
[ur-mode]: https://github.com/foigent/unirouter/blob/c5958c57106e7073e74653652b38dc549f482e10/internal/billing/mode.go
[ur-policy]: https://github.com/foigent/unirouter/blob/c5958c57106e7073e74653652b38dc549f482e10/internal/contentcapture/policy.go
[ur-body]: https://github.com/foigent/unirouter/blob/c5958c57106e7073e74653652b38dc549f482e10/internal/sourcefacts/content_body_authority.go
[ur-readiness]: https://github.com/foigent/unirouter/blob/c5958c57106e7073e74653652b38dc549f482e10/docs/community/release-readiness.md

[ur-ci]: https://github.com/foigent/unirouter/actions/runs/38032678754
[ur-protocol]: https://github.com/foigent/unirouter/actions/runs/38032678752
