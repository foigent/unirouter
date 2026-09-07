# Platform model onboarding and Qwen acceptance

Status: exact pre-code design, 2026-09-07. Base main
`3332a86929ac870fa34e0f7ad1a66ed7c2a80e31`, tree
`250b0410dbb4178f8ca2d01e75e2237a28c99b7b`.

## Mission and ordered acceptance

Make a platform connection reusable for many models, while preserving the
existing Publication, Profile, Deployment, Probe, Release and request authorities.
The first vertical acceptance is Qwen3.7-Flash Chat through UniRouter/OpenClaw.
Next validate the available intersection of the already supported DeepSeek,
Kimi, MiniMax and GLM models on Bailian, starting with one representative per
family. Then complete one explicitly supported native Messages tool loop.
Gemini, Hermes, Token Plan, media, Production and old-writer retirement are out.
The new interface must help users finish this path; it must not merely add
another list of provider-region-protocol presets.

This document freezes the first implementation slice: platform selection,
connection-scoped model discovery and batch configuration. The Qwen qualification
and Messages implementation diffs require exact typed-contract supplements before
their code is written. Those are independent engineering reviews within existing
authorization, not repeated user approval or a second architecture project.

## Verified starting state

- Existing family evidence is 14 real streamed tool loops with quality recorded
  separately. This is not evidence for their Bailian surfaces.
- Live Target read `t-wl06waldudx9o8w` confirms authorized ECS, DB system identity,
  1.2.62/M114, nine capability publications, both serving leases and four running
  containers. Backend/frontend use the formal five-file Compose chain. Nginx
  retains its historical nine-file chain. Storage free is 10,308,923,392 bytes.
- Official Go/Docker connection attempts still time out; the authorized temporary
  Aliyun Go mirror sumdb endpoint returns 200. No host configuration was changed.
- User-created Deployment40 uses PublicModel3159 `qwen3.7-flash`, compiled Profile
  `qwen.3.7-flash.api@1.7.0`, Connection14 / binding14, both revision1 and enabled.
  The connection is the Beijing workspace HTTPS Chat endpoint; the Operation is
  generic `openai.chat_completions@1.0.0`. No Probe, Release or Slot exists for it.
- Both logged-in QianwenAI and Bailian pages identify the same workspace. This
  proves the account context, not interchangeability of every key or model.
- `ModelEndpointCreateSheet` already reuses a connection and binding through
  the canonical create owner. It currently selects one model and flattens
  platform/surface choices. QianwenAI has no named entry.
- `provider_connections` owns a protocol/base URL; credentials are scoped to
  that connection. A model has an exact Deployment and Operation. This slice
  does not reinterpret a connection as a protocol-independent account entity.
- Data Profile loading for authoring currently assumes a compiled bootstrap
  Profile. Qwen is already compiled. No brand-new-model/no-rebuild claim follows.

## User flow and authority

1. Choose a service platform (QianwenAI, Alibaba Bailian, DeepSeek, Kimi, etc.).
   Platform is presentation metadata over finite existing presets, not runtime
   routing identity. OpenAI Chat and Responses are children of OpenAI; Bailian
   shared/workspace endpoints are children of Bailian. Do not infer the selected
   child from an arbitrary hostname or silently switch an existing connection.
2. Choose an existing saved connection, or configure one new connection. Region,
   workspace and API interface are separate fields. An official API Host may
   safely fill the exact supported URL/region/workspace; unsupported region or
   path reports a concrete unsupported state. Do not invent dozens of spaces or
   require cloud root/RAM access to enumerate them.
3. Select models in a searchable, filterable list. Known contract entries show
   exact upstream ID, contract availability and configured state. Discovery is an
   optional explicit refresh from the selected connection. Manually entered IDs
   remain possible but are visibly unverified and never gain tools booleans or
   qualification from the upstream model list.
4. Preview each selection as create, exact reuse, or blocked/conflict. Show a
   batch summary, including that configuration does not activate traffic.
5. Submit selected configurations through the existing canonical create command.
   Show each actual result; offer the existing next action (qualification,
   Release or project test). A failed model does not hide successful models.

The existing single-model form becomes the detailed configuration step and
advanced fallback. It is not retained as a competing default entry. Top-level
platform grouping is derived from one preset definition, with tests that each
existing preset occurs exactly once. QianwenAI uses the officially displayed
shared DashScope endpoints; Bailian recommends the workspace endpoint. Both
retain provider `dashscope` at the execution boundary and recognizable user labels.
Existing connection names, endpoint identity and historical configuration remain
unchanged. Existing credentials are reused by binding ID, never read into forms.

## Discovery API: connection-scoped, read-only metadata

Add an authenticated administrative endpoint under the existing model deployment
API, anchored to an owned Deployment and its Connection and credential binding.
Request accepts a binding ID, expected connection/binding revisions, bounded page
and query. It never accepts an arbitrary outbound URL or a raw secret. The server
loads and checks organization scope, active connection, active binding and exact
membership using the same store boundaries as the canonical Operation Probe.

Only a reviewed discovery implementation may dispatch. Initial implementation:
DashScope Beijing workspace `GET /api/v1/models` on the exact validated HTTPS
workspace host already saved in the connection. Qianwen shared hosts may display
the static reviewed catalog and explain that workspace discovery needs its exact
host; no guessed endpoint or account API fallback. No automatic redirects, URL
userinfo, custom port, query-bearing saved base URL, private-network host or
credential forwarding to another origin. Use the existing bounded HTTP transport
policy, finite response bytes/time and strict JSON decoding. Failures return
stable reason codes with sanitized diagnostics, never upstream raw body/header.

Result is a bounded list of metadata: exact upstream ID, label, author/provider,
inference provider, documented capabilities, available contract matches, observed
time, and pagination. Preserve case and namespaced upstream IDs. Response models
do not authorize execution and do not overwrite Profile, price, Release or Slot.
Unknown metadata is ignored only in this discovery projection; no raw JSON is
persisted as executable configuration. Keep requested page/query stable in the UI,
cancel obsolete reads and never merge pages from different connection revisions.
Upstream price fields are not automatically imported into customer pricing.

Official reference: https://help.aliyun.com/zh/model-studio/list-models.
The API explicitly separates model author and inference provider. Its metadata
availability does not establish an account's paid model access or Agent success.

## Batch configuration without a second writer

Reuse `/model-deployments` and its normalized fingerprint, existing typed create
receipt, transaction, encrypted credential handling and commit readback. No
separate batch database, work queue, generic orchestration engine or raw SQL
configuration writer is added.

The UI builds one exact create payload per selected model. All use one selected
existing connection and binding. For a new connection, the first selected model
creates it once and returns its IDs; subsequent selected models reference those
IDs. On an ambiguous first result, stop dependent items, recover the exact create
with its retained idempotency key, and do not create a second connection/key.

Each item retains its immutable payload and idempotency key for the current
submission. Resume retries only failed/unconfirmed configuration commands using
that same payload/key. Credential plaintext is never persisted in browser storage
or evidence. A page reload re-reads server configuration facts; it does not replay
a paid Probe or submit new credentials automatically. The existing server create
owner validates scope/product/limits/identity and returns actual conflict/reuse;
client preview is advisory and cannot bypass this validation.

For this first slice, execution contract matches are existing exact Profile
choices, including published revisions for an existing PublicModel. Match by the
full provider, API surface, endpoint class and exact upstream ID. A model lacking
a matching contract is marked as such and excluded from reviewed bulk submission;
the explicit unverified advanced form remains available. No name-based parser or
fallback inference is introduced.

Configuration submission never runs Probe, Approval, Release activation or
Provider inference. Those continue as explicit existing typed actions. The user
has authorized this development/test validation; per-item UI confirmations may
express concrete paid work, but operator automation must count each dispatch.

## Qualification and native follow-on constraints

Qwen's compiled tools contract is not a qualification binding. First author an
immutable data revision and exact typed Probe composition, preserving the staging
bootstrap and all existing rows. Do not expand current selectors or legacy Probe
implementations in place. Prefer shared SSE/tool mechanics and exact Qwen policy.
Preserve reasoning, tool history, terminal usage and DONE boundaries. Keep the
request output and thinking bounds finite for both qualification and Agent work.

Messages requires an exact endpoint implementation for `/apps/anthropic/v1/messages`
and native streamed tool lifecycle. Current origin-only M75 qualification and
Chat-only data Definition cannot be weakened to make it fit. Inspect the entire
authoring/qualification/Release/dispatch path and freeze a bounded supplement
before those changes. A concrete unsupported path is not completion, and is not
permission to route Messages through Chat or to reuse Claude evidence.

Official reference: https://help.aliyun.com/zh/model-studio/anthropic-api-messages.
Its model list and thinking/max_tokens rules differ by exact model. Messages and
Chat for one submodel share the experiment budget, not separate free allowances.

## Cost and operational controls

The user's 2026-09-07 authorization is this new onboarding batch's maximum of
30 upstream attempts and CNY5 per exact submodel, across the Qianwen/Bailian
interfaces tested in this batch. Keep previous four-family direct-provider
experiments in their original records; do not erase, reset or spend against them.
Within this batch, key changes, protocols, aliases, failures, retries and direct
diagnostics do not reset a model counter. Reserve a conservative price/token bound
before every paid call, record actual Provider usage separately, and stop before
either cap. Unknown outcome keeps the reservation and is never automatically
redispatched. Metadata discovery is recorded separately from paid inference.

Use synthetic fixtures for protocol negatives; run only required exact live
qualification and Agent rounds. If the supported request/Probe maximum cannot fit
the remaining CNY5, finish independent local work and report the exact gap.
Do not reduce product advertised limits merely to fit a test budget.

Only authorized development/test ECS may receive reviewed exact-main source and
official CI bundles. Reuse formal build, Publication and ingress/Drain runbooks.
Before a write, bind fresh Target, current images/ledgers/storage and expected
predecessor. Temporary Aliyun Go proxy keeps checksum verification. No persistent
host/proxy changes, unknown mirrors, admin account changes or global fallback.

## Tests and release gates

No implementation tests have run yet; this is a pre-code object. Required checks:

- Frontend: grouped platforms preserve all existing presets, safe host fill,
  existing binding reuse, single/bulk create, partial failure, ambiguous first
  create, same-key retry, reload reconciliation, no automatic paid call, read-only
  role, source labels, inaccessible/unknown model and stale discovery response.
- Discovery: real handler authorization with foreign organization/deployment/
  binding denied; disabled/revised connection or binding denied; exact URL/path
  and authorization placement; redirect/private/custom host fail closed; bounded
  malformed/oversized response and secret-safe errors; metadata never mutates
  model/qualification/release/price tables.
- Existing canonical create idempotency and typed binding integration regressions.
- API contract/OpenAPI, TypeScript, frontend tests, targeted Go race/integration,
  release metadata and required CI. Use real PostgreSQL where scope/receipts matter.
- Target: create/reuse through visible UI, one connection for multiple model
  configurations, exact readback, no unexpected Release/Slot change. Code/UI
  success alone does not close Qwen/Agent acceptance.

One independent pre-code adversarial review applies to this exact slice. One
closing review, if needed, is bound to this design, adjudicated findings, final
diff and new tests. Do not turn unrelated reviewer suggestions into new scope.

### Pre-code review adjudication

Independent reviewer `onboarding_precode_review` reviewed the original
`d2df99a1ef03d3e745d8d0962813f9eb8734a27c32b7590695696a4d15f34192` object.
All three validated_required findings are accepted with these exact corrections:

- Before the first Deployment exists, a new connection uses the reviewed static
  catalog only. Online discovery is disabled with an explanation. First successful
  creation opens the same connection's discovery and additional-model step; no
  placeholder Deployment or separate credential writer is introduced.
- Fresh canonical create returns the exact selected credential binding ID from
  its transaction. Existing-binding submissions already fix that ID in the
  request. Historical/replayed new-credential results cannot infer the original
  binding from current bindings. Such a result stops dependent batch items and
  requires an explicit existing-binding selection before continuing. This is a
  normal configuration choice; no first/latest/only-eligible heuristic is used.
- A submission is resumable for at most one hour from its local start, within the
  existing 24-hour server receipt window. Expired, time-regressed or restored
  state never replays a raw-secret creation. Re-read current configuration and
  select explicit connection/binding IDs for a new intent. Actor changes clear
  in-memory intents and prevent completion from continuing another actor's batch.

Tests cover first failure, lost response with changed bindings, expired intent,
page/actor transition and explicit binding continuation. No new schema or receipt
lifetime change is required. The existing 27 frontend preset/model-option tests
passed after the review object was submitted; they are baseline evidence only.

## Retirement and completion boundaries

Replace only the flat default picker and repetitive single-model workflow.
Preserve legacy receipt/history readers and the finite M113/M114 bridge; no H
writer retirement. Existing stored connections, active Releases and old rollback
artifacts remain consumers of their exact historical contracts. The advanced
single-model command is a supported primitive, not a second runtime authority.

Completion reports UI behavior, exact build/Target identity, discovered versus
configured versus qualified models, per-model attempts/exposure, Agent protocol
and answer quality separately. Shared Qianwen/Bailian branding does not imply
two independent upstream providers, and an existing compiled Qwen model does not
prove a brand-new arbitrary model can enter without a backend build.

### Implementation candidate 1.2.63 / M114

The single entry now offers saved-connection bulk selection or first-connection
creation. A fresh Chat create continues into the same connection's bulk step
using only the returned exact binding ID. A replay without that field requires
an explicit binding choice. Preview blocks visible name conflicts; the existing
canonical transaction remains the final identity and scope validator. Bulk
results distinguish saved, sending, unconfirmed and not-yet-submitted items.

Discovery is an administrator-only GET, with the same License/role guards as
configuration writes because it uses a credential. It uses the existing bounded,
non-redirecting Probe HTTP transport, but creates no Probe or execution receipt.
The search field uses the supplier's documented fuzzy `name` parameter. Paging
retains that search and connection revision; obsolete responses are suppressed.
Known static contracts remain available without online discovery. Published data
revisions retain the existing exact-product detailed authoring path; bulk model
creation does not invent bootstrap support for unknown supplier IDs.

Local validation covers actor/time-window ambiguity, first-create binding versus
replay, discovery ownership/revision/revocation, response projection, supplier
metadata exclusion, canonical create persistence, preview name conflicts and no
automatic Probe/Release. The affected Go packages passed race checks; the first
API parity run found missing Qwen enum/route docs, which were corrected and then
passed. Isolated PostgreSQL tests now also carry Qwen's data Definition through
its actual reasoning-budget Probe handler and persisted safe request summary.
Frontend focused checks passed 61 tests and the production build/bundle budget.
Full frontend checks, independent closing review, CI and Target validation are
separate final gates; none is inferred from these local results.

The existing lessons MAR-001, MAR-007, MAR-009 and MAR-010 already cover the
observed composition, evidence and publication boundaries. Qwen-specific null
fields and the discovery UI are exact implementation details, not a new general
lesson. No active lessons added, merged or retired: 经验文件无需变化.
