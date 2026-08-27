# UniRouter System Fact Map

> Status: repository current-state map for implementation impact analysis; local source facts are not Target deployment evidence.
> Scope: the supported on-premises topology, where one deployment serves one customer while deployment operators remain a separate identity concern.
> Authority: code and migrations override this map when they disagree; any product change that alters an entity, authority, or consumer must update this file in the same batch.

## 1. Product boundary

UniRouter is a modular monolith with one public gateway and one administration API. It is not a collection of independently deployable services. PostgreSQL owns durable control-plane and evidence state; Redis owns short-lived admission, routing-health, cache, and coordination state.

The local candidate's executable PostgreSQL compatibility ceiling is `000088`.
M85–M87 batch inclusion is tracked in
[B1 scope](../tech-ref/44-model-adaptation-required-baseline-v1.md#5-b1-finite-integration-batch--2026-08-26);
M88 is the separately bounded local capability-qualification candidate described
in the current status document. This source ceiling alone does not prove a
deployment. The separately authorized
[B2 Target checkpoint](../tech-ref/42-model-execution-profile-capability-status.md#b2-target-deployment-and-bounded-zero-call-acceptance--2026-08-26)
records the 86-entry M87 ledger after a non-rolling maintenance window on
2026-08-26. The subsequent
[bootstrap repair checkpoint](../tech-ref/42-model-execution-profile-capability-status.md#first-customer-runtime-reset-repair--localci-and-target-preview-2026-08-26)
records the earlier deployed `e888c05f9d5d70f4809bcd5362373b8fa8f8ecf4`
with the same schema and a successful read-only operator preview. The later
[approved initialization](../tech-ref/42-model-execution-profile-capability-status.md#first-customer-initialization--target-verified-2026-08-26)
created independent customer 2, its zero CNY wallet and startup-owned Default
Project 86; admin/platform organization 1 and login evidence remain separate.
A later readback confirms one staging DeepSeek Flash deployment and credential
binding under customer 2. Following explicit approval of a 3-call/CNY-1 batch,
exact probe 1 succeeded at 23:02:34 CST on 2026-08-26 (HTTP 200, 298 ms), advancing
only the Chat operation to `live_probed` revision 2. At that checkpoint there was
no Release, runtime assignment or gateway-client call. This
single connectivity result does not prove model capabilities, usage or pricing.
The [no-redirect transport repair](../tech-ref/42-model-execution-profile-capability-status.md#no-redirect-transport-repair--deployment-and-zero-call-acceptance-2026-08-27)
was superseded by UI-workbench candidate
`1f30b1cd4c8d614f177fa0d890f162d4c0605af7`, then by the live-found capability
correction `e47af1faacd8ca96403c25b4cf2c47ec155bd980`, and finally by bounded M88
artifact `08d0ef8e5cee7e1bff515d5751e7a9bdd452f16f`. Its automatic CI
33050163111 and protocol-conformance CI 33050163136 passed before a non-rolling
maintenance deployment. Stable, canary and frontend now use the exact 08d
artifacts; canonical model-route authority remains enabled with compatibility
disabled, and that M88 deployment performed no canonical cutover. The database
has the exact 87-entry M88 ledger.
Following explicit authorization to create a test account and test through the
UI, user 38 was created as customer 2's ordinary member / project 86 user and
successfully logged in. UI authoring/first activation created Release 22,
active@3 / slot@1, containing only deployment 1 / Chat. Model 677 is now approved
in the test customer catalog with lifecycle GA; this is not capability evidence
or personal acceptance. The ordinary-user UI first clicked the exact bounded
non-stream Flash request and recorded a 503/no-route call with no provider
attempt, Freeze, transaction, token or wallet mutation. The live-found fix
preserves an explicitly disabled thinking control in the provider payload but
does not make reasoning a per-invocation route requirement. After deployment,
the UI clicked the same request once: HTTP 200, exact expected text, 16 prompt +
8 completion tokens and provider-reported final usage. At that non-stream
checkpoint, the original probe plus this successful UI attempt were the only
two supplier calls. One unused one-time key was inadvertently exposed to an
automation trace and immediately disabled;
all nine short-lived test keys are now disabled, including key 81 used for the
successful request. No credential is retained here. The [UI checkpoint](../tech-ref/42-model-execution-profile-capability-status.md#first-ordinary-user-ui-request--pre-dispatch-blocker-and-bounded-correction)
records the exact request and evidence boundary.
Project discovery now reads existing v2 normalized route items with
exact tenant/public-model identity and stable/active/allow membership, retaining
an explicit historical v1 branch; discovery is not gateway admission authority.
The repair also gives the Chat debugger explicit optional request bounds and
accurate non-stream response/error/duration evidence; no schema, pricing or
execution Profile changed. Call, provider-attempt and cost-observation Freeze
bind the same canonical deployment/connection/Release/Plan snapshot. One
append-only zero-charge observation receipt exists, with no customer transaction,
supplier-cost reconciliation or wallet movement. Zero customer charge does not
prove zero supplier cost. Customer-side manual acceptance remains deferred.
The product owner later increased the cumulative test ceiling to 20 calls/CNY 5,
including probe 1; this is spending authority, not evidence of further calls or
a change to caller-identity, capability or Release requirements.
The M88 Admin workflow subsequently issued exactly one typed streaming probe.
The provider returned HTTP 200, but the deployed validator rejected its stream
contract and recorded only a generic sanitized error. No capability approval,
qualification or ordinary streaming request followed; this is supplier call
3/20, with no wallet or transaction mutation. A local stabilization candidate
now accepts SSE-standard CRLF/LF/CR endings and records a bounded typed partial
observation—sanitized model, terminal and usage fields when parsed, plus byte,
event and delta counters—with an enumerated failure category and no provider
response content. It remains
local evidence until its own gate/artifact/Target checks; it does not reinterpret
the historical failed receipt or authorize another paid probe.
The exact Flash non-stream text slice is `canary_live_verified`. That evidence
does not imply direct parity, other capabilities, billing, supplier-cost
authority or `released`. After running
migrations or honoring `SKIP_MIGRATION`, every backend reads the complete,
ordered migration ledger and requires it to equal the binary artifact ledger
before customer-baseline initialization or HTTP bind; missing, extra, future,
duplicate, reordered and empty ledgers all fail closed. Schema-ceiling changes
are therefore non-rolling: an M84 process and an M87 process cannot be ready
against one shared database. Operators drain and stop the old fleet, let one
migration owner advance the database to the approved artifact's exact ledger,
and only then start matching
stable/canary. A secondary may use `SKIP_MIGRATION=true` only after the shared
database already has that exact ledger. M85 adds nullable immutable Plan hash
bindings on Freeze/attempt without backfilling historical rows. M86 preserves
v1 and v2 public execution tuples in exact binding views; M87 appends the
Messages tools v3 tuple. None rewrites a sealed Release or promotes live traffic.
M78 adds atomic
sealed-release authoring provenance and the explicit v1-to-v2 lifecycle bridge;
M79 moves the commercial `active_channels` count authority to enabled canonical
provider connections. M80 gives cost-observation provider reconciliation an
append-only zero-charge terminal receipt instead of manufacturing a financial
transaction; its terminal transaction and observation receipt identities are
mutually exclusive and database-guarded. M81 records the exact versioned model
execution profile selected by the customer: tokenizer, reasoning, cache-usage and
protocol identities are repository-reviewed evidence, while context/output values
remain customer policy bounded by provider maxima. Historical rows are deliberately
`unverified.chat@1.0.0`; no migration infers authority from a model name. Component
identities have executable contracts rather than provider-name branches. In
particular, `kimi.k3.api@1.5.0` validates the provider-owned
`reasoning_effort=low|high|max` contract without injecting the documented `max`
default, preserves reasoning on assistant tool-call history, and authorizes Kimi's
top-level `usage.cached_tokens` only through the selected cache-usage profile.
Kimi K2.6/K3 Partial Mode, K3 Dynamic Tools, bounded inline media and built-in
web search now have typed request contracts; only their exact executable
profile versions may admit those shapes. M83 extends Freeze, finalization,
observation and call-log evidence with exact non-token Rate Card rationals and
provider-evidenced meter quantities. The first executable unit is a bounded
Kimi `$web_search` call; a missing price, excess quantity or untyped response
fails closed. M82 pins that reviewed
profile identity plus the public context/output limits into each canonical v2
Release. Canonical Planner resolves them from the selected stable/canary Release
and commits them to the snapshot hash, so a registry edit only supplies a future
Release and cannot mutate live execution. Exact release/lifecycle inspection
surfaces expose the pins without changing the v1 authoring-receipt body. Billing
Freeze remains the independent price-version authority. M83 guarded down rejects
later migrations, durable non-token meter evidence or versioned component source
evidence that M82 cannot represent. M82 guarded down rejects
later migrations and any durable Release pin that would be lost under M81's
mutable registry lookup. M79 takes the shared model-execution migration advisory
lock, then the provider-connection/channel/schema relation locks, then the
`active_channels` resource advisory lock, and only then the entitlement-table
lock; this ordering matches both activation and License renewal without a lock
cycle. Its guarded down is permitted only after later migrations are gone and
every enabled canonical connection has one exact active legacy projection whose
count still fits the signed limit. The down restores only the legacy count
trigger/function and retains canonical tables, credentials and history. Neither
migration reinterprets the M77 Responses/Messages public-execution chain, and
legacy tables remain available only for history and explicit compatibility
operation.

M84 widens only official rate-source evidence currency to one original CNY or
USD value so foreign list prices can be retained as review-only drafts. It does
not widen the CNY request-pricing approval, activation, wallet or settlement
contracts. Mixed-currency source drafts fail closed, and M84 down refuses to
discard any retained non-CNY source evidence.

The supported customer topology is:

- one deployment;
- one customer context;
- multiple customer projects, applications, users, and API keys;
- multiple upstream provider endpoints for the same public model;
- separate deployment-operator identities and customer identities.

`org_id` remains an internal isolation and integrity key. It is not a normal operator-selectable tenant dimension in the single-customer product.

New installations should keep deployment-operator and customer identities in separate organization records. For an upgraded local installation with exactly one active root organization, the customer-context resolver may adopt that sole root as the compatibility customer context even when its historical `org_type` is `partner`. This does not make the deployment-operator role a project member, and customer-facing UI must use the resolved customer context rather than expose a historical technical name such as `Platform Admin`.

That compatibility resolution is not canonical-startup readiness: the current
route-authority checker separately requires one explicit active root `customer`
and assigned runtime inventory. The [DeepSeek preflight](../tech-ref/42-model-execution-profile-capability-status.md#deepseek-zero-call-preparation--blocked-preflight-2026-08-26)
records this distinction on the partner-only clean Target. The subsequently
approved bootstrap repair adds an exact runtime-reset predecessor and a
write-free `dry_run` projection in the existing API, exposed only in the
deployment-operator workspace. It preserves admin/platform identity and login
evidence, normalizes only an explicitly authorized automatic empty project and
zero wallet, and creates a separate customer; it does not alter startup/probe
gates. After explicit exact-object approval, Target initialization succeeded;
the subsequent canonical check removed the customer-root blocker but still
rejects empty runtime inventory. This is not supplier-live or billing evidence.

## 2. User workspaces

| Workspace | Primary actor | Owns | Does not own |
|---|---|---|---|
| Deployment operations | deployment operator | runtime, License, provider endpoints, upstream credentials, release/routing operations, global safeguards | customer project membership or application credentials |
| Customer administration | customer administrator | people, project membership, model visibility, application access, budgets, usage | host/runtime lifecycle or upstream secret custody |
| Project workspace | project member | project-scoped applications, API keys, permitted model use, usage visibility | provider endpoints, prices, or deployment policy |

The UI may combine these workspaces in one shell, but every page must state which authority it uses. A deployment operator must not silently become a customer project member.

API guidance follows the same ownership boundary. Ordinary project members use the project workspace as the primary API quickstart and can open `/docs#error-<code>` for a gateway error. Project administrators may open the project API reference; organization and deployment management identities see only the control-plane contracts within their own authority. The frontend documentation catalog is a presentation projection, not an authorization boundary: backend route gates remain authoritative, and every new API area is hidden until its audience is explicitly declared.

## 3. Model and routing truth

```mermaid
flowchart LR
  T["Reviewed provider preset\nprotocol + official base URL"] --> O["Model onboarding composition\nsafe editable draft"]
  H["Immutable Profile catalog\nall historical ID@version addresses"] --> O
  E["Current-for-surface index\nnew attachment authority"] --> O
  X["Compiled public operation contract\noperation + path + endpoint protocol@version"] --> O
  R["Model registry\npublic model identity"] --> A["Alias and model set\ncustomer visibility"]
  C["Provider endpoint\nprotocol, base URL, upstream model mapping"] --> K["Upstream credential"]
  O -. "create with exact profile pin" .-> R
  O -. "create connection/deployment/operation" .-> C
  K --> P["Live probe\ncredential + model + routing epoch"]
  C --> P
  X --> P
  R --> L["M78 sealed release\nserver baseline + selected operations"]
  H -. "exact historical pin remains resolvable" .-> L
  P --> L
  L --> S["Runtime slot\nstable or canary"]
  S --> G["Gateway route plan"]
  X --> G
  H -. "selected Profile typed capability identities" .-> G
  A --> G
```

| Concept | Durable authority | Important projections/consumers |
|---|---|---|
| Model adaptation requirement | current `unirouter.model-adaptation.required.v1` file is a Draft exact-matrix candidate; no immutable approved Baseline has been issued | product-scope candidate only; it is never loaded by Gateway and never copies Progress, evidence, Release or pricing state |
| Model Execution Profile lifecycle | immutable repository product-identity manifest owns every addressable `profile_id@version`, including provider, region, requested upstream model, surface, operation and request-default provenance; append-only exact-address metadata may additionally declare upstream model family and `pinned|floating` version policy, while historical unmodeled addresses remain empty rather than being backfilled; a locally conformant public operation may pin an exact endpoint-protocol ID/version and compose typed capability sub-contracts; the independent current-for-surface index stores only one attachable version pointer | onboarding and exact surface alias resolution use the current pointer; Planner and sealed Release reads resolve immutable historical addresses. Provider-reported model identity belongs to probe or immutable attempt evidence and never rewrites the requested alias. `Profile.Capabilities` is the supported executable set and `AlwaysRequiredCapabilities` is its narrowly validated subset. The exact typed invocation derives one normalized Plan capability list with origin sets; optional supported capabilities are not automatically required. The current pointer cannot backfill historical fields, and no mutable booleans or database lifecycle state are introduced |
| Onboarding composition | repository-reviewed provider preset plus an exact Model Execution Profile; the backend validates the profile pair, operation and effective limits and persists the exact profile pin. For the exact M88 DeepSeek Flash Chat streaming contract, migration recognizes both the current v4 protocol tuple and the historical generic-Chat tuple already sealed in pre-M88 Releases; canonical create refines new attachments to v4. Either path may materialize an `enabled/declared` capability projection only after compiled and database support contracts agree. A separate dated official-price snapshot may be sent to the existing evidence-backed Rate Card draft importer | “模型接入” form defaults and constrained choices only; a declaration is not a live probe, routing approval, Release, active pricing, credential or traffic authority. The historical binding is not generic Chat inference. The declaration writer performs no provider call and never overwrites an existing support decision. Pricing composition creates only a separately reviewable draft. Omission stays `unverified.chat@1.0.0` only when the alias is not ambiguous across current surfaces; an explicit unverified pair remains the deliberate escape hatch |
| Processing tier | provider/surface-specific typed policy composed by the exact operation Profile; requested wire value, normalized effective tier, runtime timeout and provider response evidence are separate fields. A surface with no documented observed-tier channel records that evidence as unavailable rather than fabricating a missing header source or inferred tier | admission capability derivation, pre-Freeze billing-mode guard, request-specific deadline, provider payload and trace evidence. It never derives a customer price from a supplier wire value |
| Public model identity | model registry and aliases | model catalog, project model sets, readiness, `/v1/models` |
| Provider connection | canonical `provider_connections` plus deployment operation/profile and public-to-upstream mapping | canonical deployment UI, operation probe, Planner candidates, License `active_channels` usage |
| Credential | canonical `provider_credential_bindings` plus exact deployment link; plaintext is accepted once and encrypted | operation probe and Coordinator credential selection |
| Legacy endpoint history | `channels`, `channel_keys` and `channel_model_probes` | read-only migration/audit compatibility and explicit legacy/verify planning; never canonical readiness or current Dashboard authority |
| Canonical operation probe evidence | append-only `model_deployment_probes`, bound to exact deployment, operation, protocol profile, connection/deployment/credential revisions and credential-deployment link epoch | canonical route admission and operation evidence; never capability inference |
| Capability routing qualification | M88 append-only typed capability probe and approval receipts plus `model_capability_approval_qualifications`; the receipt is the approval source, while receipt plus the current exact deployment/operation/Profile/protocol/probe-contract tuple is the current qualification authority | new Plan and cutover use `model_capability_execution_qualifications` to select one deterministic preferred approval. Pre-dispatch validates the approval identity already frozen in the Plan; it never requires that receipt to remain the newest. The mutable capability row is UI/management projection only |
| Responses/Messages execution support | immutable M75 binding plus compiled `modelqualification` tuple: operation, public path, protocol profile/version and executor contract | probe availability and structural production qualification; it cannot by itself authorize public dispatch |
| Responses/Messages production qualification | append-only platform approval of one current exact live probe; `model_operation_execution_qualifications` is the only mutable current structural qualification truth | canonical read DTO and M77 public-binding creation; qualification alone is never online/traffic proof |
| Public execution support extension | immutable M77 extension references the exact M75 five-field support tuple and adds delivery, charge and idempotency contract versions | the sole exact eight-field public executor registry lookup |
| Per-item public execution binding | immutable M77 binding freezes customer, public model, deployment, connection, operation/profile, M75 approval structural lineage, approved revisions, origin, release ID/ordinal and exact eight-field support identity | required selected manifest item join in qualification, Planner snapshot and Coordinator revalidation; old sealed M75 items never auto-upgrade |
| Public execution evidence | `executionevidence.EvidenceV2` plus the V2 compact projection and exact provider-attempt columns | Planner snapshot hash, owner-fenced billing attempt, terminal health, call log and trace; V1 digest semantics remain unchanged |
| Release authoring | M78 `model_deployment_release_authoring_receipts`, its exact audit/events and the receipt-linked sealed v2 release/items/bindings | release workspace collection, exact replay and provenance; the command ends at `validated` and never assigns traffic |
| Release history | runtime releases plus append-only operational history; M77 historical sealed v2 rows may have no M78 authoring receipt | release queue, stable-parent lineage, rollback evidence, audit |
| Release lifecycle | M76/M78 immutable lifecycle receipts, frozen qualification/release images and `runtime_change_slots` CAS | seven server-described actions; five canonical actions plus `cutover_legacy` and no-License `rollback_legacy` |
| Current traffic authority | sealed runtime stable/canary slots and assignments | canonical Planner, readiness and release workspace |
| Connection operational state | canonical connection state/revision, credential/link state and latest own operation probe | Planner/Coordinator eligibility, readiness and Dashboard current health |
| Per-request route attribution | immutable execution-evidence projection including `route_deployment_id`, `route_connection_id`, `route_model_operation_id`, credential binding and runtime release | Dashboard canonical health and recent requests; `route_channel_id` remains legacy history only |

Rules:

1. A public model can have many provider endpoints, including the official provider and aggregators.
2. Provider identity, adapter protocol, public model ID, and upstream model ID are different fields.
3. A successful probe is evidence for one endpoint, one upstream credential, one model mapping, and one routing epoch. It is not a global provider assertion.
4. Historical releases remain visible as audit history, but only the current slot is traffic authority.
5. Readiness is a read-only explanation of existing authorities. It must never introduce another status or require users to re-enter identifiers already known by the selected model/project/application.
6. Endpoint display names are mutable and may repeat. No request, health, or cost projection may infer endpoint identity from `channels.name`; legacy rows without provable route identity remain unattributed.
7. A canonical live probe addresses exactly `deployment_id + operation_id + credential_binding_id`. All four supported operations use distinct bounded requests. Responses and Messages additionally resolve their exact endpoint-protocol profile and execute its real network request/response contract; a local conformance fixture is never live evidence. A successful response advances only that operation while tools, vision and every other capability remain unchanged unless separately exercised and evidenced.
8. “模型接入” is the primary product wording. `Canonical Deployment` remains the internal durable identity because routing, release, probe and audit records require `deployment_id + operation_id`; it is not the user task name.
9. A reviewed onboarding template may prefill and constrain provider, model aliases, context/output limits and operation. It must pin the exact Profile version, keep connection naming editable, and never mark the result live-verified or released. Reusing an existing public model treats supplied Profile fields only as exact assertions.
10. A non-standard normalized processing tier must pass its typed provider/surface contract and the current customer-charge boundary before credential decryption and Freeze. Until a tier-aware `CustomerChargeQuote` exists, explicit Flex/Priority is cost-observation-only.
11. Requested, effective and provider-observed tiers are distinct. Missing or unknown provider evidence stays a capture gap; Priority-to-Standard is accepted only from the typed provider response header and never inferred from latency or status.
12. A provider model alias that is current on more than one API surface is ambiguous by design. New attachment must select the exact surface-specific Profile and compatible operation; surface-agnostic reviewed matching fails closed. Retiring a mixed legacy Profile ID removes only new-attachment eligibility—historical lookup, sealed Release pins and immutable evidence are not rewritten.
13. A Model Execution Profile may claim local conformance for the strict public Responses/Messages baseline only by pinning the exact compiled endpoint-protocol ID/version for that operation. A richer public request grammar must be a typed, versioned capability sub-contract on a new exact Profile address. Historical Profile pins retain their former grammar. Canonical onboarding and Planner both compare the endpoint-protocol pin with the deployment operation; a mismatch fails before public qualification or dispatch. This repository-level conformance does not replace the live probe, M75/M77 approval chain, customer price authority or Release.
14. Current capability authority: select the exact Profile/Release identity, use its typed operation contract to derive one normalized, sorted and deduplicated Plan capability list with per-invocation origin (`explicit_request` or `provider_default`) plus any truly unavoidable `profile_always_required` entries, then select route evidence inside the same repeatable-read snapshot. Closed invocation constraints such as declared/selected function names and an exact `tool_choice=none` no-call rule live inside that same capability entry and snapshot hash; `none` reuses the supported `function_tools` entry rather than inventing another capability set. A route receives only a deep-copied projection, not another authority. `effective_required` must be a subset of Profile-supported capabilities. Route selection, the M85 Freeze receipt and the provider-attempt Receipt bind the same Plan snapshot hash; none independently re-normalizes the capability list. Historical empty bindings remain addressable only for pre-M85 compatibility and do not authorize a new evidenced attempt.
15. For an M88-typed capability, Planner and cutover select the same deterministic preferred qualified approval for the exact deployment, operation, Execution Profile, protocol Profile and probe contract. The Plan freezes that approval in the existing capability `evidence_ref` and evidence hash. Pre-dispatch then validates that exact bound receipt against the same stable tuple; a later equivalent receipt does not stale an in-flight Plan. Stable tuple drift or an invalid receipt fails closed. Credential cooldown/recovery remains an operational availability axis and does not revoke a generic streaming approval; a future credential-scoped capability must declare that narrower scope in its own typed probe contract.

For Responses and Messages, M75 and M77 form this control chain:

```mermaid
flowchart LR
  P["Exact endpoint-protocol live probe"] --> A["Platform approval\nappend-only receipt"]
  A --> Q["Current qualification view\nM75 sole truth"]
  Q --> B["Per-release-item M77 binding\nexact eight-field support"]
  B --> M["Sealed M77 manifest item\nand EvidenceV2 Planner snapshot"]
  C["Current credential + link\nlatest own live probe"] --> R["Attempt-first Coordinator\nsame-transaction revalidation"]
  M --> R
  R --> X["Strict public operation executor\nnon-stream + exact Responses text SSE"]
```

The approval receipt freezes the exact probe credential identity and link epoch
and revalidates their currentness when the receipt is appended. Later credential
rotation, revoke, unlink, or cooldown remains a separate operational binding
axis: it requires a current selectable binding and latest own probe at planning
and Coordinator revalidation, but does not rewrite the historical human
approval, M77 structural binding or qualification lineage. Structural authority
still fails closed on customer/release item, connection, deployment, operation,
profile, immutable support, origin or approved-revision drift.

M78 adds the authoring and compatibility-transition control chain without
replacing any M77 public-executor authority above:

```mermaid
flowchart LR
  O["GET release collection\ntenant snapshot"] --> B["Server-owned baseline\nand eligible operation choices"]
  B --> A["POST atomic authoring\nstrict JSON + Idempotency-Key"]
  A --> R["Durable author receipt\nsealed validated v2 release"]
  R --> I["GET release detail\nslot + parent + action descriptors"]
  I --> C["Five canonical actions\nor exact legacy bridge actions"]
  V1["Sealed active v1 parent\nraw slot public_model_id = NULL"] --> C
  C --> T["Lifecycle receipt + audit + events\nrelease pair and slot CAS"]
  T --> S["Current stable slot\ntraffic authority"]
```

- The collection read is the only UI authoring preflight. It returns each
  tenant-owned public model, the exact server-derived `initial`, `stable` or
  `legacy_cutover` baseline, the raw-slot projection after ownership proof,
  eligible deployment/operation selections and existing actionable v2 release
  summaries. Ambiguous identity, a live canary, a retired public model or no
  eligible operation remains visible with an unavailable reason and no usable
  baseline; the client must not invent revisions, hashes, parent IDs, profiles,
  bindings or a release reference.
- `POST /api/v1/model-deployment-releases` is the sole M78 v2 authoring writer.
  One repeatable-read transaction claims a durable receipt, locks the exact
  baseline and selected authority, creates the release/items/public bindings,
  appends create and validate events plus audit, seals the manifest and returns
  a `validated` release. It does not activate, start a canary or move a slot.
  A new commit returns 201 and `Location`; a later exact winner replay returns
  200 with the same `Location`, `X-UniRouter-Idempotency-Replayed: true` and the
  original server operation ID. An ACK-recovery inside the same operation is
  not labelled as a later replay.
- The lifecycle detail exposes either the normal five actions or, for an exact
  sealed v1 stable parent, only `cutover_legacy` and `rollback_legacy`; the UI
  follows these server descriptors. `assigned_public_model_id` is absent/null
  for the raw v1 slot while the logical `public_model_id` remains the owned
  public model. Every write revalidates versions and authority under locks.
- `cutover_legacy` changes a stable slot from raw v1 identity
  (`public_model_id IS NULL`) to canonical v2 identity only when the sealed v2
  target preserves the complete established Gateway operation set—Chat
  Completions and Embeddings—and the two currently executable plans have the
  same secret-free hash. It atomically advances both releases and the slot and
  freezes the two plan hashes in the immutable lifecycle receipt.
- `rollback_legacy` is the inverse emergency contraction. It is available to
  organization write roles without a Gateway License and deliberately ignores
  target v2 route health, qualification and parity; exact target/slot lineage
  plus the retained sealed v1 parent's current executable Chat+Embeddings
  authority still must pass. It atomically restores the raw v1 slot identity
  and retains the immutable evidence chain.
- Collection/detail reads allow `platform_admin`, `org_admin` and `org_viewer`
  without a Gateway License. Authoring, normal expansion and legacy cutover
  allow only `platform_admin`/`org_admin` and require the Gateway feature.
  Canary cancellation and legacy rollback keep the same write RBAC but no
  License gate. Authentication and RBAC run before License admission.
- All authoring and lifecycle request bodies are one strict JSON object no
  larger than 16 KiB. Unknown/duplicate members and trailing values fail before
  durable receipt access. A 409 means the client must refresh server truth; no
  UI action silently retries against a changed baseline.

### Current live Gateway execution boundary

- The binary, ordinary Compose and runtime config default
  `MODEL_ROUTE_AUTHORITY` to exactly `canonical` with
  `MODEL_ROUTE_COMPATIBILITY_ENABLED=false`. There is no automatic fallback.
  `legacy` and `verify` require compatibility to be explicitly enabled. The
  formal customer fresh-install package starts once in `legacy + compatibility`
  only because an empty installation does not yet have a customer root or a
  sealed current slot; it must remain traffic-isolated while the customer and
  first canonical release are authored, then pass the read-only authority check,
  switch to `canonical + false`, and restart before serving traffic.
- For `verify` and `canonical`, process startup performs
  `unirouter.model-route-authority-check.v1` from one Repeatable Read, read-only
  PostgreSQL snapshot before Redis, background workers or HTTP bind. Its
  inventory is the union of live configured legacy model names and every
  current stable/canary slot. A sole root customer, registry/slot identity,
  sealed manifest, operation/profile, connection/deployment revisions,
  credential binding/link and latest own probe must agree. Operational disable,
  cooldown and breaker state may make a route temporarily unroutable; one
  healthy route never hides a structurally stale sibling in the same manifest.
  Because v1 exposes `legacy.any.v1`, verify additionally proves both published
  project-default paid envelopes (Chat Completions and Embeddings) against every
  selectable adapter; a chat-only adapter blocks startup.
- `uniroute model-route-authority apply --customer-org-id <id>` is the only
  explicit compatibility preparation command and is accepted only in `verify`.
  It reuses the idempotent legacy reconcile and seals every current v1 stable
  and canary compatibility manifest before running the same read-only check. It
  does not promote, rollback, switch mode, publish a v2 release or move a slot.
  Reconciliation and v2 lifecycle take release/slot authority locks in one
  fixed order; any current v2 stable/canary makes apply fail before projection
  writes, including from a verify process during a rolling cutover.
  `legacy`/`canonical` apply is rejected before database access; canonical uses
  read-only `check`, so legacy projection cannot overwrite its state. Canonical
  readiness accepts a sealed parentless native v2 assignment; only verify
  requires legacy/canonical parity and a v1 compatibility parent.
- Exactly 19 legacy channel/key, v1 runtime-change and model-endpoint lifecycle
  writes are permanently retired in every authority mode: four channel writes,
  four channel-key writes, three model-endpoint lifecycle writes and eight
  runtime-change writes. Their routes authenticate and enforce platform RBAC,
  then return `410 legacy_model_mutation_retired` before License admission,
  body parsing, secret access or any retirement-handler/business-data query;
  authentication and customer/RBAC context may still read their own security
  state. No compatibility flag re-enables the retired handler or its SQL writer.
  Historical reads remain available, while canonical model-deployment creation,
  probing, release authoring and descriptor-driven lifecycle remain writable. Two
  authenticated organization-admin safety contractions remain available even
  without a valid Gateway License: connection pause changes only the connection
  axis (verify mirrors only legacy channel status), while credential revoke is
  terminal and scrubs canonical and verify-side legacy ciphertext in one
  audited transaction. Each revalidates the sole root customer and active
  authoritative organization role in that same transaction. Fleet drain is
  still a deployment responsibility because the database has no live-process
  registry or writer lease.
- Production startup always injects `modelrouting.Planner` and
  `modelexecution.Coordinator` into Chat Completions and Embeddings, including
  in explicit compatibility modes. Their handlers require that complete runtime
  before guardrail, cache, secret resolution, Freeze or provider I/O and contain
  no legacy Router/attempt fallback. Canonical execution also bypasses the
  legacy response cache. The old Gateway `Router` remains only for explicit
  compatibility and historical consumers; canonical startup rejects async media
  until that separate operation has canonical execution authority.
- In explicit `legacy` mode, Planner reads the existing channel/key and v1
  runtime-slot authorities. The canonical provider-connection, deployment,
  credential-binding and operation rows are a migration/proof projection, not
  the live read authority. `verify` compares the two projections atomically;
  `canonical` selects only the sealed v2 projection.
- Planner fixes one operation- and capability-specific route snapshot; Gateway
  locally builds usable candidates before billing Freeze. After Freeze,
  only an `Applied` Coordinator decision may dispatch provider HTTP; that same
  transaction owns the immutable provider-attempt evidence and health fence.
- Before Freeze, Chat conservatively measures the complete supported envelope
  (including tool/function/structured controls), while Embeddings accepts only
  bounded string or integer token-ID shapes. Typed normalization rejects unknown
  semantics before Freeze and network. Public context and each candidate's `MaxContext`
  are hard admission evidence. `DefaultMaxOutput` is not a hard maximum:
  omitted canonical/verify output is reserved and injected from the exact
  Profile's request default, bounded by its public limit and supported wire
  contract; the legacy adapter
  contract uses its versioned 1000-token compatibility ceiling. The legacy
  probe profile never authorizes a wire field. The deliberate pre-catalog
  legacy case has no public registry limit and retains the channel `MaxContext`
  as its context bound.
  Commit ambiguity is recovered only with the same ephemeral owner. If local
  processing fails before network dispatch, the owned attempt becomes the
  financially resolved `released_undispatched` terminal even when its route was
  subsequently disabled, revised, or lost a half-open lease.
- `/v1/responses` and `/v1/messages` are registered only for the exact M77
  non-stream text contract. The Gateway runtime fence requires an installed
  public registry and narrow Coordinator; the pre-secret plan fence requires
  the selected item, EvidenceV2 base, profile and all three public contract
  versions to match exactly. Ordinary Coordinator `Authorize` continues to
  reject public evidence; only narrow `AuthorizePublic` can execute the exact
  M77 plan and it revalidates release item, binding, current credential/link and
  latest own probe after opening the owner-fenced attempt and before network.
- The public executor bypasses legacy Router/adapters/response-cache and never
  uses V1 evidence or the old provider-attempt API. It prepares and input-guards
  the provider-independent document, strictly re-prepares any transformation,
  builds all candidates locally, freezes once, dispatches only an `Applied`
  authorization, strictly parses a whitelisted customer body, completes the
  attempt and terminal health, then records delivery before settlement. Guarded
  output retains only canonical usage as provider-liability evidence and is
  never delivered. Retry-authorized known rejection may fall back only after
  durable completion and terminal health; non-retryable or exhausted rejection
  cancels. Transport ambiguity or unusable 2xx remains settlement-pending
  without fallback or cancellation.
- M75 `production_qualified` alone still must not be presented as “online” or
  Production traffic evidence. Online execution additionally requires the
  selected active M77 release item, exact eight-field binding, current
  credential/probe authority, funding, provider outcome, health, guard and
  delivery/settlement terminals for that request.
- The customer-package preflight validates the selected mode and compatibility
  pair but is deliberately not a readiness proof. `verify` preparation is
  explicit and bounded to current v1 assignments; `canonical` is read-only
  `check` only. In both non-legacy modes the backend database startup gate is the
  authority that proves readiness before any listener or worker starts. Draining
  older binaries remains a deployment responsibility; the database intentionally
  retains legacy read evidence and does not pretend to be a live-process lease.

Canonical administrative projections use the same identities as execution:

- `unirouter.model-readiness.v2` derives an exact model operation from the
  selected public endpoint. Connection mode requires `deployment_id` and
  reports `deployment_id + model_operation_id`; caller mode explains the
  canonical Planner/current application projection rather than invoking a
  legacy Router preview.
- Dashboard current health and current-window call facts join canonical
  `route_deployment_id + route_model_operation_id` to the canonical application
  projection. `route_channel_id` is retained and labelled only as historical
  attribution; it cannot create a current health row.
- `GET /api/v1/model-endpoints` is the
  `unirouter.model-endpoints.canonical-compat.v3` selector projection. Its
  legacy-shaped `channel_id` and `routing_epoch` fields are fixed at zero while
  `deployment_id`, `connection_id` and `model_operation_id` carry current
  identity; it reads no legacy current authority and performs no Router preview.
- Canonical cost/readiness responses explicitly report
  `response_cache_runtime=canonical_bypass`. Historical request cost and cache
  facts remain immutable evidence; bypassing future canonical cache reads and
  writes does not rewrite them.

## 4. Access truth

```mermaid
flowchart LR
  U["Customer user"] --> M["Project membership"]
  M --> PK["Personal project credential"]
  M --> AP["Workload application"]
  AP --> Q["Access request and grant"]
  Q --> D["Logical application credential"]
  MS["Project model set"] --> E["Effective callable scope"]
  X["Optional advanced model restriction"] --> E
  D --> E
  E --> G["Gateway admission"]
```

| Question | Authority |
|---|---|
| Can the user administer a project? | customer role plus project membership role |
| Which models can the project see? | project model-set binding and approved catalog |
| Which models/endpoints can a personal project key call? | key scope and ownership, intersected with project visibility and membership |
| Which models/endpoints can a workload application key call? | access grant and logical credential scope, intersected with project visibility |
| Is a special per-user/model restriction present? | model authorization policy; this narrows access and is not the normal grant path |
| Can a deployment operator call models? | only when separately represented as a customer/project principal; operator status alone is insufficient |

The normal human/developer flow is: create user -> add user to project -> the project user creates a personal project credential in the project workspace. The workload flow is: project administrator creates an Application -> Request -> Grant -> logical credential. The advanced model-authorization surface must not be presented as the base way to grant access.

## 5. Pricing, billing, and evidence truth

### 5.1 Current implemented facts

```mermaid
flowchart LR
  LP["Legacy model price"] --> B["Billing Freeze / settle"]
  RC["Current RequestPriceQuote\nCNY supplier-layer Rate Card selected by model_ref"] --> B
  U["Provider usage fields"] --> M["Meter classification\ninput, cache read, cache creation, output"]
  B --> T["Settle or cost-observation billing mode"]
  M --> T
  T --> J["Customer usage journal, receipt, call log, outbox"]
  J --> R["Reconciliation and reports"]
  PC["providercogs offline MemoryStore"] -. "append-only import/replay only" .-> R
```

| Concept | Authority | Boundary |
|---|---|---|
| Current request price | configured pricing mode: legacy model price or `RequestPriceQuote` | `RequestPriceQuote` is CNY and immutable after selection, but currently resolves supplier-layer Rate Cards primarily by `model_ref`; it is not yet the approved `CustomerChargeQuote` contract |
| Peak/off-peak price | complete UTC daily bands in the active Rate Card | every minute must select exactly one band |
| Cache-hit charging | provider usage fields such as cached input tokens | UniRouter does not infer a cache hit from latency or prompt text |
| Prepaid billing | wallet freeze and settlement records | blocks when required funds/admission are unavailable |
| Exact public final-token charge | immutable attempt `route_charge_contract_version` plus provider-reported final usage | checkpoint estimates cannot become public customer charges; missing final usage retains pending liability, including process-loss recovery |
| `BillingModeCostObservation` | customer-CNY calculated-cost receipt/journal | does not charge the wallet; its current numeric storage is not native-currency supplier-cost evidence and must not be presented as such |
| Supplier-cost estimate | any UniRouter-local `usage × rate` calculation, whether before or after the provider response | an estimate is defined by local calculation authority, not chronology |
| Supplier-cost observation | supplier-reported cost, bill or reconciliation evidence | the current reusable semantics are offline in `providercogs`; its persistence is only `MemoryStore`, so it is not a production runtime supplier-cost system |
| Supplier-cost quote | a provider-supplied, citable quote | absent unless the provider actually supplies an authoritative quote; a UniRouter calculation is never renamed as a quote |
| Supplier reconciliation | imported/manual provider evidence and offline reconciliation records | `supplier_reported` and `unirouter_estimated` are authorities; `provisional`, `final` and `invoice_reconciled` are finality/evidence states, not authorities |
| Foreign supplier list price | original-currency review draft | no FX, native-USD runtime cost claim or CNY wallet charge is created by M84 source evidence |

### 5.2 Known current conflicts

- The Rate Card type separates supplier and customer economic layers, but the
  current request resolver selects `supplier_contracted` or `provider_list` and
  feeds that quote into Billing. Source code therefore has an architecture
  conflict; this map does not claim that the current Target has actually charged
  a customer incorrectly.
- Current cost-observation receipts can store a numeric calculated zero when a
  price is unavailable. That compatibility storage is not authoritative proof
  of a free price; a future typed projection must expose price status and omit
  the quote when unavailable.
- No implemented runtime contract yet represents the approved replayable
  `CustomerChargeQuote`, native-currency `SupplierCostObservation`, or their
  separation. The offline `providercogs` terminology and authority/finality
  invariants should be reused without making Gateway depend on its offline
  types or implementing invoice ingestion in this milestone.

### 5.3 Approved target architecture (not current implementation)

```mermaid
flowchart LR
  PA["Public CNY customer pricing authority"] --> CQ["CustomerChargeQuote\nroute-neutral + normalized-tier-aware"]
  CQ --> PS["One immutable pricing snapshot"]
  PS --> WA["Wallet authorization upper bound"]
  US["Normalized provider usage"] --> ST["Customer settlement or zero-charge observation"]
  WA --> ST
  SC["Supplier native-currency rate evidence"] --> SE["SupplierCostEstimate\nUniRouter-local calculation"]
  SO["Supplier-reported bill / reconciliation"] --> OB["SupplierCostObservation"]
  SE -. "separate authority" .-> REP["Margin and reconciliation reporting"]
  OB -. "separate authority" .-> REP
  ST --> REP
```

The approved V1 target freezes these invariants:

- `CustomerChargeQuote` is public-CNY, route-neutral and normalized-tier-aware.
  It depends on public product/model, operation, normalized requested tier,
  server-resolved customer plan and effective period. Provider, connection,
  route, attempt and supplier currency never select or mutate it.
- Customer plan is resolved by the server from authenticated organization,
  project, API-key and contract identity. A client-supplied `customer_plan` is
  rejected rather than used as a pricing input.
- A quote Receipt retains `customer_charge_quote_id`, quote contract version,
  pricing Profile ID/version, selector schema version, normalized selector
  dimensions, selector hash, normalized meter/rate components, server quote
  time, effective period, customer-plan resolver identity/version, `currency=CNY`,
  estimated/reserved quantities and `maximum_authorized_customer_cost`. A hash
  proves stability but is never the only explainable price identity.
- Once wallet Freeze authorizes the quote, failover, retry and crossing a price
  period reuse that exact snapshot. Final customer charge cannot exceed the
  authorized maximum without a new explicit authorization; supplier cost never
  increases the customer quote.
- Customer amounts remain distinct:
  `quoted_customer_cost`, `wallet_authorized_cost` and
  `charged_customer_cost`. In cost-observation mode the latter two are zero;
  `quoted_customer_cost` is present only when an approved quote exists. An
  unavailable price is absent, never an authoritative zero-price quote.
- A pricing snapshot/observation and a wallet hold are different events even if
  a compatibility API is still named `Freeze`. Cost-observation may create an
  immutable pricing snapshot but never a hold, balance change or customer
  financial transaction.
- Standard tier may use an approved customer price. Until a separate settlement
  rule exists, explicit flex/priority fails the prepaid path with
  `billing_contract_unavailable` before secret resolution, wallet authorization
  or provider dispatch; only separately authorized bounded zero-charge supplier-
  cost evidence is allowed.

## 6. Operational and integration truth

| Surface | Observed truth | Desired/registered truth |
|---|---|---|
| Runtime status | current process, PostgreSQL/Redis probes, traffic and host snapshot | none |
| Deployment and network | runtime status can show the actually running instance | manually registered deployment/entry records describe intended or auxiliary topology |
| License | verified license claims and feature gates | no page-local duplicate feature status |
| Integrations | connector-specific live state only after the connector is configured | available integrations and planned integrations must be visually distinct |

A normal “not configured” integration is not a global page error. Connector failures belong inside that connector’s detail surface.

## 7. Write-authority and impact matrix

Use this table before changing any listed concept.

| Change | Required writers/checks | Required consumers to verify |
|---|---|---|
| Add provider connection/deployment | canonical deployment create receipt, explicit public/deployment limits, credential binding, operation identity and audit. The ordinary UI requires all four limits to be positive: public `context_length`/`output_limit` and deployment `max_context`/`default_max_output`; the API may retain zero only as non-authorable staging state | model endpoints, release authoring options, operation probe, readiness and License usage |
| Pause/revoke canonical authority | connection safety contraction or terminal credential-binding revoke, exact revision/role proof and audit | Planner/Coordinator selection, endpoint detail, readiness and Dashboard |
| Probe canonical model operation | exact deployment/operation/credential binding, server-side secret resolution, bounded adapter request/response, append-only evidence and atomic audit | operation evidence, canonical route admission, endpoint detail; no capability projection |
| Change public model identity | registry uniqueness/canonicalization, aliases, release conflict checks | catalog, model sets, releases, `/v1/models`, pricing |
| Publish or roll back a deployment | atomic M78 author receipt followed by a server-described lifecycle receipt, release/item/event/audit proof and slot CAS | Planner/Coordinator, release workspace, Dashboard and readiness |
| Add customer user | canonical customer scope | people, project membership, access requests, audit |
| Grant project access | membership transaction and role validation | project workspace, applications, effective access, audit |
| Issue/rotate application key | access grant and scope snapshot | gateway admission, credential list, usage attribution |
| Enable a person to call models | customer user plus project membership; the person creates the one-time personal secret | project workspace, personal key list, gateway ownership and usage attribution |
| Change price | authority mode, Rate Card maker-checker and immutable schedule | freeze/settle, readiness, cost preview, reports |
| Change License | one verified license authority plus resource advisory locks; `active_channels` counts enabled canonical connections from M79 | navigation, route gates, page availability, runtime status and canonical connection activation |
| Change deployment record | manual registry only | deployment/network page; never infer live process mutation |
| Change API audience or error documentation | backend route authorization and error registry | project quickstart, direct `/docs#error-*` links, project-admin reference, management reference, navigation |

## 8. Golden journeys

These are cross-layer acceptance contracts, not mock-page demonstrations.

1. **J1 - First usable model:** create a canonical connection/deployment with positive public `context_length`/`output_limit` and deployment `max_context`/`default_max_output` -> bind a credential -> probe the exact operation -> atomically author a sealed release -> execute `activate_initial` -> canonical Planner selects it -> a real gateway request succeeds -> route evidence, call log and cost evidence appear. API-created zero-limit rows remain non-authorable staging records and cannot substitute for this journey.
2. **J2 - Second deployment for the same public model:** add an aggregator deployment with a different upstream model ID -> probe -> author an exact child release -> start canary -> promote or roll back without duplicating the public model.
3. **J3 - Credential safety:** bind and prove a replacement credential against the exact deployment/operation before a future rotation surface changes selection; terminal revoke and connection pause fail closed immediately and existing immutable evidence remains attributable.
4. **J4 - Customer user access:** create customer user -> add to project -> issue a personal project credential, or create a workload Application/Grant credential -> permitted model succeeds and unpermitted model fails clearly.
5. **J5 - Price authority:** draft complete cache-aware/peak schedule -> independent review -> activate -> calls in representative bands freeze and settle with the selected four meter prices.
6. **J6 - License and integration presentation:** licensed pages load; unavailable features explain the exact missing claim; unconfigured connectors remain local neutral states.
7. **J7 - Deployment operations:** actual runtime is shown from observation; optional registered topology is clearly separate; no form implies it deployed or reconfigured the running service.

## 9. Confirmed debt register

### Closed in the current model-endpoint lifecycle batch

- Active upstream credentials can be rotated without pausing traffic: the replacement is proved against the exact mapped upstream model before an atomic swap, and a failed probe leaves the old credential and route unchanged.
- Customer-facing people and model-endpoint surfaces use the canonical customer context instead of displaying the deployment-operator organization as the customer.
- Model onboarding preserves the selected endpoint context when opening credential management; it no longer drops the operator into an ambiguous global list.
- The ordinary access path is now user -> project membership -> personal project credential, or a project-managed workload application credential. Per-user/model authorization is explicitly an advanced narrowing control.
- Rate Card authoring supports multiple complete UTC daily bands with four explicit cache-aware meter prices; the legacy price form is labelled as a flat compatibility authority.
- Connector state is loaded only inside the selected connector detail; an unconfigured connector is no longer promoted to a global integration-page error.
- Deployment/network separates observed runtime health from manually registered topology, and no registration form claims to deploy or reconfigure the running service.
- Runtime release history has server pagination and current/history separation; revision counters remain internal CAS evidence rather than a user-facing model version.
- This fact map is the canonical entity/authority/consumer impact map, and the codebase map now tracks the current migration ceiling.
- The required real-backend browser journey uses only canonical deployment, exact operation probe, atomic release authoring and receipt-backed first activation; retired channel/runtime-change writers are never mounted by the test server.
- Dashboard current health and Readiness now consume canonical deployment+operation authority instead of reinterpreting channel, credential and v1 release rows. A credential for one deployment cannot make another operation look callable, and each repair link carries the exact deployment/operation identity.
- Gateway call evidence stores the immutable selected deployment, connection, credential, operation and release identity. Dashboard current-window metrics join deployment+operation instead of display names or legacy channel identity; `route_channel_id` remains labelled historical evidence only.
- Application identities are generated by the server in the ordinary create flow and shown only as advanced audit evidence. Access-review rows resolve the person or Application display name on the server instead of requiring administrators to interpret database IDs.
- Shared tables and the Application access panel show an explicit loading state until their authoritative requests complete; an in-flight read is no longer presented as an empty credential, Request, or Grant set.

### Closed in the maintenance-debt batch

- The retired `ChannelsPage` compatibility implementation and its isolated test were deleted; `/channels` continues to resolve only to the supported Model Endpoints workspace.
- Model Endpoints is now a page orchestrator with separate create/detail surfaces and presentation helpers. Its API contract has a typed domain module instead of page-local response assertions.
- Model endpoint reads now live in one canonical projection file, while endpoint lifecycle writes remain in their handler. Model-governance registry mutations, catalog reads, and shared scope/scanning helpers are separate review and test seams.
- Dashboard did not retain a second legacy channel/release SQL authority: current health consumes the canonical deployment/operation application projection. Readiness route eligibility calls the narrow canonical Planner, while access, pricing and policy decisions come from their owning services or bounded read models.
- GitHub-maintained JavaScript actions used by active workflows run on the current Node 24 action generations. Compose no longer declares the obsolete top-level `version` field.
- Commit-addressed release worktrees now have a read-only retention report and an explicit, revalidated apply path. Only old, detached, clean, canonical worktrees absent from running-container and keep-file protection can become candidates; no timer or automatic deletion is installed.
- Development Target backend builds keep the truthful new runtime commit while reusing persistent Go module and build caches. Reusing an old backend image under a new release identity remains forbidden.
- PostgreSQL-backed tests run only in the integration gate and reuse a migrated template per package. The sensitive-word database contract no longer makes the ordinary Go unit gate create and migrate a temporary database; its source-order assertion remains in the unit gate.
- Readiness no longer owns copied pricing, grant/key, registry or model-set admission SQL inside the HTTP handler. Pricing and wallet evidence comes from the billing service used by `Freeze`; credentials are rechecked through the gateway's live access-grant boundary; model authorization alternatives share one bounded policy read; governance and explanatory reads live behind the `modelreadiness` read model.
- Dashboard current/previous window metrics are each read from one organization-scoped PostgreSQL snapshot instead of ten separate aggregate queries. Current route availability comes from the canonical deployment/operation application projection; the observation aggregates join only canonical route-deployment/operation evidence and never infer current state from `route_channel_id`.
- Provider channels/credentials, model endpoints, runtime routing/releases, Dashboard, enterprise finance and access/Readiness use named typed frontend API modules. The remaining aggregate compatibility calls are explicitly counted by a non-increasing build gate, so a new feature cannot silently add another `any` response seam. The unused legacy channel editor and its duplicate status helpers were removed.
- API documentation is audience-specific: ordinary members receive one project quickstart and common or deep-linked errors, project administrators receive a project-scoped reference, and management identities receive an authority-filtered control-plane reference. Undeclared API areas fail closed in the UI catalog, while direct error links remain compatible and backend authorization remains the only security authority.

### Current-batch blockers and release gates

- This map does not make an unbounded "no P0" claim. Non-legacy startup now
  uses the process-level database proof above rather than relying on a later
  per-request 503. A production cutover still requires reconciled route data,
  sealed manifests, a drained legacy-writer fleet and evidence from the exact
  release artifact.
- Release verification must also demonstrate that an Applied decision released
  before provider dispatch is terminal and cannot remain an unresolved billing
  liability, and must bind the deployed artifact to one unambiguous commit.
  These are cross-authority correctness gates, not page-local UI symptoms.
- The ordinary binary and source Compose default is already
  `canonical + compatibility=false`. A formal fresh customer package may run
  `legacy + true` only as a traffic-isolated bootstrap process; customer root,
  first canonical release/slot and the read-only authority check must complete
  before a restart in `canonical + false`. Final artifact/Target evidence for
  that exact transition remains a release gate and does not turn bootstrap mode
  into the customer serving default.

### P1 - recurring product inconsistency

- Readiness composes model, access, pricing and policy authorities from their owners and remains a read-only explanation rather than a mutable status. Its deployment/operation route axis, Dashboard canonical health and model-endpoints v3 projection are locked by shared Planner/projection tests; every future authority change must retain that cross-surface gate.
- Finance and access still contain advanced audit/workflow surfaces. The changed pages now explain and separate them, but a future information-architecture batch may move them out of the ordinary task path; that is not required for the current golden journeys.
- The canonical-customer projection fix is covered on the changed people and endpoint paths. Remaining customer-domain pages stay under the existing access/readiness browser journey and must keep that gate when their scope logic changes.

### P2 - maintainability

- The Model Endpoints workspace and its two backend domains now have meaningful orchestration/read/write seams. Other large files should be split only when a product change needs an independent authority or test seam, not to chase line-count targets.
- The API client has a strict shared transport and named contracts for provider channels/credentials, Model Endpoints, runtime routing/releases, Dashboard, enterprise finance and access. The remaining aggregate compatibility surface is still hand-written and weakly typed, but its exact call ceiling is ratcheted by the build. OpenAPI is route-complete while many response bodies remain generic JSON, so generation must follow authoritative response schemas rather than manufacture `unknown` wrappers.
- Readiness authority duplication is closed for the current model-call path. Its HTTP handler still shapes product explanations, while durable reads and admission decisions remain owned by billing, model governance, access-grant boundary, model authorization, limit policy and gateway routing services.
- A frontend-only release still produces a backend binary bound to the same release commit by design. Persistent BuildKit Go caches reduce the rebuild cost without creating mixed or misleading release identity; independently versioned components would require a separate release-manifest contract and is not being implied here.

Debt is closed only when the affected golden journey and projection checks pass. Moving code into more files without reducing authority duplication does not close architectural debt.

## 10. Change completion checklist

For changes to these flows:

1. identify the durable authority and all writers;
2. verify each projection consumer in the impact matrix;
3. add a negative test for stale revision, wrong scope, or missing evidence as applicable;
4. run the relevant real-backend golden journey;
5. distinguish Local, CI, Target, external-provider, and production evidence;
6. update this map if an entity, authority, or consumer relationship changed.
