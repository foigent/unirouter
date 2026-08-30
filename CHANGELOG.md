# UniRouter Changelog

All product releases follow Semantic Versioning (`MAJOR.MINOR.PATCH`). Deployment
artifacts additionally retain an immutable runtime version and exact Git commit;
those build identities do not replace the product version.

## [1.2.21] - 2026-08-30

### Documentation

- Recorded the exact Kimi K2.6 Chat Core development/test closeout: deployed
  1.2.20/M100 artifact identity, six independent capability qualifications,
  fixed OpenAI SDK delivery and cache-read evidence, the 18-of-20 bounded-call
  total, zero customer wallet movement, and supported test-access cleanup.
- Removed the duplicated stale Kimi active-batch snapshot so the current-status,
  capability-status and system-fact entrypoints agree.

### Evidence boundary

- This is a documentation and release-metadata correction only. It changes no
  runtime contract, schema, Profile, Probe, Approval, Release, Alias, pricing,
  billing, credential, route, provider traffic or Target deployment. The
  development/test Target remains the exact 1.2.20/M100 artifact recorded in
  the status documents.

## [1.2.20] - 2026-08-30

### Fixed

- Normalized the optional non-stream
  `choices[].message.tool_calls[].index` wire member for the exact Kimi K2.6
  Chat v5 tuple using the same bounded normalizer as public runtime delivery.
  Only a lexical non-negative JSON integer is accepted; streaming indexes and
  every unknown sibling remain unchanged and fail closed under their existing
  contracts. Duplicate JSON members, including escaped-equivalent names, are
  rejected before usage or delivery maps can apply last-wins semantics.
- Published Kimi Function Tools Probe Contract v4 for only tools, function
  tools and automatic tool choice, while retaining the failed v3 Probe as
  immutable historical evidence.

### Evidence boundary

- M100 is a control-data publication plus shared decoder correction. It does
  not change the Execution Profile, Chat protocol, Release, alias, pricing,
  Planner, UI, customer billing or canonical traffic and creates no live Probe
  or Approval evidence by itself.

## [1.2.19] - 2026-08-30

### Fixed

- Kept Kimi capability Probe `contract_error_code` values stable and moved a
  syntax-constrained unknown JSON member identifier into the separately bounded
  `diagnostic_member` evidence field. Provider-controlled member names no
  longer create dynamic error-code identities.
- Added the repository-level model-adaptation learning closeout pointer so an
  independently shipped Delivery Batch cannot rely on long-chat memory for its
  required learning-file inspection and report.

### Evidence boundary

- The diagnostic member remains provider-controlled, bounded to 64 ASCII
  identifier characters and non-authoritative for capability, usage, billing,
  routing or release. The member value and complete raw response body are not
  returned.
- This patch does not change the Kimi Probe acceptance set, migration ceiling,
  Profile, protocol, Probe contract, Release, alias, pricing, routing, billing
  or canonical traffic and creates no live evidence by itself.

## [1.2.18] - 2026-08-30

### Fixed

- Added a bounded, schema-only diagnostic projection for failed Kimi capability
  Probes. It can identify a syntax-constrained unknown member name or the decode
  category, but never persists the member value or complete raw response body.
- Kept the Probe acceptance set and qualification decision unchanged: the
  diagnostic makes an immutable failure actionable and cannot turn it into
  successful evidence.

### Evidence boundary

- Development/test M99 Probe 50 remains immutable, failed and unapproved. This
  patch changes no migration, Profile, protocol, Probe contract, Release,
  alias, pricing, routing, billing, UI or canonical traffic, and creates no
  live or release evidence by itself.

## [1.2.17] - 2026-08-30

### Fixed

- Aligned the exact Kimi K2.6 Function Tools capability Probe with the public
  Chat delivery boundary for bounded numeric provider usage extensions and the
  already modeled nullable response members. Only modeled core, cache and
  reasoning counters remain qualification authority; unknown numeric counters
  are never promoted into usage or billing authority.
- Preserved the immutable v1/v2 acceptance sets, strict tool identity and
  arguments, exact finish semantics, duplicate-member rejection and fail-closed
  handling of every response member outside the existing delivery boundary.

### Evidence boundary

- M99 advances only the exact Kimi K2.6 tools, function-tools and automatic
  tool-choice bindings to Function Tools Probe Contract v3. It does not change
  the Execution Profile, Chat protocol, Release, alias, pricing, Planner, UI or
  canonical traffic, and it creates no live Probe or Approval evidence by
  itself. Failed Probe 49 remains immutable and unapproved.

## [1.2.16] - 2026-08-30

### Fixed

- Accepted Kimi K2.6's bounded numeric non-stream usage details for prompt
  cache and completion reasoning while preserving top-level cache authority,
  exact token totals and strict rejection of every other unknown usage member.
- Published non-stream Probe Contract v2 for reasoning, tools, function tools,
  automatic tool choice and JSON Object so the live failed reasoning v1 Probe
  remains immutable and stale rather than being reinterpreted.

### Evidence boundary

- M98 advances only the five exact Kimi K2.6 non-streaming Probe contracts. It
  does not change streaming v3, the Execution Profile, Chat protocol, Release,
  alias, pricing, Planner, UI or canonical traffic, and it creates no live
  Probe or Approval evidence by itself.

## [1.2.15] - 2026-08-30

### Fixed

- Accepted Kimi K2.6's bounded choice-level terminal stream usage, including
  numeric cached/reasoning detail counters, while still requiring the standard
  top-level usage frame and identical repeated token totals.
- Published streaming Probe Contract v3 so the live failed v2 Probe remains
  immutable and stale rather than being reinterpreted by the repaired parser.

### Evidence boundary

- M97 advances only the exact Kimi K2.6 streaming Probe contract. It does not
  change the Execution Profile, Chat protocol, Release, alias, pricing,
  routing framework, UI or the other five K2.6 capability contracts, and it
  creates no live Probe or Approval evidence by itself.

## [1.2.14] - 2026-08-29

### Fixed

- Normalized only an empty-string `choices[].delta.reasoning_content` member
  from the exact Kimi K2.6 OpenAI-compatible Chat v5 SSE surface. Runtime
  delivery and the streaming capability Probe now consume the same
  exact-scoped normalizer; non-empty reasoning remains visible, invalid types
  fail closed and unknown siblings remain subject to the existing guardrail.
- Published streaming Probe Contract v2 so the failed live v1 sample remains
  immutable and stale rather than being silently reinterpreted by the new
  software artifact.

### Evidence boundary

- M96 changes only the Kimi K2.6 streaming Probe interpretation contract. It
  does not change the Execution Profile, Chat protocol, Release, alias, pricing
  or the other five K2.6 capability contracts, and it creates no live Probe or
  Approval evidence by itself.

## [1.2.13] - 2026-08-29

### Added

- Added an exact Kimi K2.6 OpenAI-compatible Chat operation identity and six
  typed capability qualification contracts for streaming, provider-default
  reasoning, tools, function tools, automatic tool choice and JSON Object.
- Added strict Kimi probe response validation and cross-contract fixtures that
  show accepted fixtures pass the current Gateway delivery-shape and guard
  parsers. Public UI and fixed-SDK delivery remain separate Target checkpoints.

### Changed

- Scoped immutable Chat operation bindings by exact Execution Profile address,
  preventing the shared Kimi v5 validation contract from implicitly granting
  K2.7 Code onboarding authority during the K2.6 milestone.

### Evidence boundary

- M95 publishes only declared Kimi K2.6 contracts. It does not create live
  Probe or Approval evidence, qualify a route, activate pricing, publish a
  Release, change an alias, or call the provider. K2.7 Code remains outside
  this release's execution authority.

## [1.2.12] - 2026-08-29

### Fixed

- Preserved the backend's `active_workload_sponsorship` diagnosis when an
  administrator attempts to offboard a business Sponsor that still owns an
  active workload authorization. The UI now directs the operator to archive
  the related application or revoke the old authorization; retaining access
  requires a new independently sponsored authorization followed by revocation
  of the old authorization. This replaces the generic delete failure.

### Evidence boundary

- The backend sponsorship invariant and atomic offboarding transaction are
  unchanged. This frontend correction does not reassign a Sponsor, revoke a
  Grant, change routing or create provider traffic.

## [1.2.11] - 2026-08-29

### Fixed

- Required a platform administrator to select an active member of the exact
  customer project as the business Sponsor for a workload-application access
  request; operator identity no longer substitutes for customer authority.
- Exposed the existing grant-bounded `allowed_models` input when issuing an
  application Key. An exact model list narrows the Key while an empty value
  continues to inherit the project's dynamic model scope.

### Evidence boundary

- The backend still revalidates Sponsor, project, Grant and credential scope.
  This UI repair does not promote a model lifecycle, create pricing or aliases,
  widen a Grant, alter routing, or issue provider traffic.

## [1.2.10] - 2026-08-29

### Fixed

- Made exact reviewed Chat operation probes consume the same immutable
  Execution Profile operation-protocol identity used by onboarding, so a
  DeepSeek V4 Pro deployment can produce the operation evidence required by
  Release authoring without weakening fail-closed routing.
- Replaced the frontend DeepSeek Profile/protocol qualification allowlist with
  a server-owned exact capability-probe contract projection. The frontend now
  renders only presentation contracts it understands, while each capability's
  Probe, Approval and qualification remains independently authoritative.
- Stopped labelling every Release authoring conflict as baseline drift; only
  the server's explicit general authoring-conflict code triggers the neutral
  refresh message, while idempotency and other errors preserve their exact
  reason.

### Evidence boundary

- These changes repair administration reachability and evidence projection;
  they do not create capability evidence, qualify a route, publish a Release,
  change pricing or authorize provider traffic. Target proof remains separate.

## [1.2.9] - 2026-08-29

### Fixed

- Exposed the existing DeepSeek V4 Pro Chat Core capability qualification
  workflow in the deployment details UI, matching the M94 exact Profile
  bindings without changing protocol, Profile or qualification authority.

## [1.2.8] - 2026-08-29

### Added

- Added M94's exact DeepSeek V4 Pro Chat Core qualification bindings for the
  current Profile `1.5.0` and protocol v4 tuple. The nine routing capabilities
  retain independent Probe, Approval and qualification evidence; no generic
  tuple, grouped approval authority or additional surface is introduced.

### Fixed

- Moved reviewed Chat attachment protocol selection out of capability
  qualification and into an immutable, model-profile-owned operation protocol
  identity resolver. Invalid or ambiguous typed declarations now fail closed;
  a legitimate absence of typed authority preserves only the independently
  validated server-owned base identity.
- Reused DeepSeek V4's exact response normalizer and Probe grammars for Pro
  without changing the existing canonical Chat response contract or creating a
  model-identity upgrade for an SDK-only tool-call index.

### Evidence boundary

- The resolver and M94 migration are locally conformant after focused unit and
  PostgreSQL integration tests. DeepSeek V4 Pro remains unreleased and is not
  live verified until the exact artifact is deployed, probed, approved and
  exercised through development/test.

## [1.2.7] - 2026-08-28

### Fixed

- Normalized DeepSeek V4 Flash's optional SDK-only function-tool `index` at the
  exact provider/model/Chat boundary, preserving the existing Profile `1.5.0`,
  protocol v4, Release and Alias. Runtime delivery and function-tool Probe v2
  reuse one normalizer; valid lexical non-negative integer indexes are removed
  from the canonical response while unrelated fields remain fail-closed.
- Added M93's ten immutable Probe v2 bindings for the five existing function-tool
  capabilities under Profile `1.5.0`'s two exact Chat protocol identities. Old
  v1 receipts remain historical but no longer qualify current routing.
- Made the Developer Console surface a terminal SSE error and its request and
  billing status after an HTTP 200 stream, instead of displaying the failed
  stream as an empty successful response.

### Evidence boundary

- This UI contract does not create, qualify, release or route a model by itself.
  Those control-plane records and any supplier calls remain separately verified
  on development/test.
- M93's Probe v2/normalizer correction and the terminal-stream UI correction are
  locally conformant only until their exact artifact is deployed, probed,
  approved and exercised through the development/test UI. They do not reinterpret
  v1 evidence or authorize Production.

## [1.2.6] - 2026-08-28

### Fixed

- Kept reviewed onboarding strict about the exact Execution Profile and upstream
  provider model while allowing the administrator to choose a unique, stable
  public model ID. This lets a new immutable deployment replace an older public
  identity through an audited Alias cutover without treating that caller-facing
  ID as a provider model alias.

### Evidence boundary

- This onboarding contract does not create, qualify, release or route a model by
  itself. Those control-plane records and any supplier calls remain separately
  verified on development/test.

## [1.2.5] - 2026-08-28

### Fixed

- Added immutable DeepSeek V4 Flash and Pro Profile `1.5.0` identities that
  preserve the provider-default thinking mode while rejecting explicit
  `tool_choice` values that the active reasoning surface cannot honor. A
  redundant explicit `auto` is normalized to the provider default; named and
  required choices remain available only when thinking is explicitly disabled.
- Added M92 exact capability-contract bindings for DeepSeek V4 Flash Profile
  `1.5.0`, taught canonical onboarding to materialize every migration-approved
  capability for an exact Profile tuple, and advanced startup schema authority
  to M92. Historical Profile, Release, probe and approval evidence remains
  immutable and version scoped.
- Updated the Admin capability workflow and OpenAPI projection for Profile
  `1.5.0`; clean rollback is supported only before any Profile `1.5.0`
  attachment or qualification evidence exists. A retained Flash/Pro model
  attachment, probe or approval prevents destructive downgrade.

### Evidence boundary

- Local request, migration, bootstrap and UI tests do not transfer the existing
  Target Profile `1.4.0` live evidence to `1.5.0`. Deployment, new exact probes,
  routing approvals, ordinary supplier calls and any public-model traffic switch
  remain separately bounded and must be verified on development/test.

## [1.2.4] - 2026-08-28

### Added

- Added exact DeepSeek V4 Flash Chat qualification contracts for reasoning,
  reasoning effort, fixed function tools, auto/named/required tool choice and
  JSON Object. Migrations M90/M91 add only declared support projections;
  immutable typed probe and approval receipts remain required before routing.
- Added one guided Admin capability workflow for the new exact contracts and
  bounded Developer Console scenarios for reasoning, fixed no-side-effect tools
  and JSON Object. Arbitrary provider payloads and private reasoning text are
  not retained by the qualification evidence.

### Fixed

- Preserved terminal usage and finish reason in the streaming Developer Console
  result, and prevented `thinking=disabled` from being sent together with
  `reasoning_effort`.
- Advanced the binary schema authority to M91, extended current-schema rollback
  fixtures, made M90/M91 downgrade fail closed on stable identity drift, and
  updated fresh-customer bootstrap to validate all 20 exact probe bindings and
  18 current contracts.
- Updated the OpenAPI capability qualification contract and negative tests so
  schema, bootstrap, UI and runtime consumers agree on the same finite set.

### Evidence boundary

- Local typed, migration, bootstrap, rollback and UI evidence does not by itself
  prove provider-live qualification, routing approval, customer billing,
  Release or canonical promotion. Paid tests remain separately bounded.

## [1.2.3] - 2026-08-24

### Added

- Added exact versioned onboarding Profiles and official Provider presets for
  the retained catalog: 30 upstream model entries and 33 API-surface identities,
  with exact upstream IDs, explicit operations, provider limits, request defaults,
  bounded protocol subsets and separately recorded documentation evidence.
- Added dated CNY or USD official-price candidates. Flat, reviewable schedules
  can create provider-list drafts; context-, tier-, cache-TTL-, storage- or
  tool-dependent prices remain visible as `manual_review_required` instead of
  being flattened into an inaccurate rate.
- Added migration `000084_official_rate_source_currency_drafts`, allowing USD
  only on official review-source evidence while retaining CNY-only request
  pricing approval, activation, wallet and settlement contracts.
- Added M85 Plan-to-Freeze/attempt snapshot fences, M86 coexisting exact public
  Responses/Messages v1/v2 bindings and the bounded M87 Messages function-tool
  binding. Historical Profile and sealed Release identities remain addressable;
  referenced evidence prevents destructive rollback. These schema changes
  require an explicitly approved non-rolling deployment, not a live promotion.
- Added the bounded M88 capability-qualification authority: append-only typed
  probe and approval receipts, deterministic preferred approval for new Plans
  and cutover, and exact bound-receipt revalidation for in-flight Plans. This
  release exposes only DeepSeek V4 Flash Chat streaming qualification. The
  migration and canonical deployment creator materialize its exact
  `enabled/declared` support projection only when the compiled and database
  contracts agree; they never call the provider or create live, approval,
  route, Release or pricing authority, and never overwrite an existing support
  decision.

### Fixed

- Separated documented provider maximum output from the default output used
  when a request omits an explicit limit, including the DeepSeek V4 384K
  maximum and the reviewed defaults for Kimi, MiniMax and the new catalog.
- Rejected mixed-currency official drafts and guarded M84 rollback when retained
  non-CNY evidence would be lost; no implicit FX conversion was introduced.
- Derived invocation capability requirements from the exact typed Profile in
  the same route snapshot, rather than requiring every supported capability.
  Normalized capability evidence through one timestamp/hash authority and kept
  historical strict public-v1 execution isolated from newer capabilities.
- Rejected public output-limit violations before credentials/Freeze, serialized
  M85 rollback against evidence writers and migration discovery, and corrected
  onboarding/status claims to distinguish local subsets from full adaptation.
- Kept public stream response, terminal-health and delivery ordering consistent
  with the durable attempt ledger; preserved actual rejected/transport status
  and prevented checkpoint estimates from charging provider-final-only contracts.
  Updated M86 attempt/call-log/trace constraints to consume exact public bindings.
- Required explicit GPT surface selection for ambiguous governance aliases;
  populated new public limits from executable maxima without changing existing
  lower limits or mistaking request defaults for provider maxima.
- Kept fresh-install bootstrap compatible with the exact M86/M87 protocol seed
  additions while retaining dirty-state, provenance, schema and Release guards.
- Prevented mutable capability projection rows from authorizing M88-typed
  routes without a current receipt-aware qualification. Equivalent newer
  approvals do not invalidate an already frozen Plan; stable contract drift
  still fails closed, while transient credential cooldown remains a separate
  route-health gate. M88 rollback removes only its untouched declaration
  marker; retained probe, approval or execution evidence blocks destructive
  rollback, and a down/up replay restores only the exact supported tuple.

### Evidence boundary

- B1 retains only the explicitly approved bounded implementation batch; it is
  not completion of every model's provider-documented capabilities. Local
  conformance is capability-specific. Provider-live, canary and release evidence
  remain separate; no paid traffic, deployment or canonical switch is authorized
  by this changelog. USD prices remain review-only drafts, and nonstandard tiers
  cannot debit prepaid customer wallets.

## [1.2.2] - 2026-08-24

### Fixed

- Removed all React `act(...)` and Semi Form validation-race warnings from the
  frontend unit suite by waiting for observable state, real user interactions,
  and file-owned Semi portal cleanup.
- Kept the custom provider template selected after an execution-Profile catalog
  failure by binding initialization to the SideSheet open transition and
  preserving unrelated fields when applying partial Semi Form updates.
- Normalized ANSI control sequences before warning fingerprinting so local and
  CI runs classify the same diagnostics and stack continuations.

### Added

- Added a Vitest stderr fingerprint ratchet: an unknown diagnostic or an
  increase in an approved fingerprint now fails the test command even when all
  assertions pass. Reporter output remains visible.
- Kept expected validation-boundary and explicit failure-path diagnostics in
  exact, independently capped fingerprints so one warning family cannot
  silently replace another.

### Compatibility boundary

- The exact React 18 / Semi UI 2.100.0 `findDOMNode` waiver remains visible and
  capped at ten occurrences. UniRouter-owned calls remain forbidden and React
  19 remains blocked until that third-party warning reaches zero.

## [1.2.1] - 2026-08-24

### Changed

- Enabled the reviewed React Router v7 transition and relative-splat behavior
  in production and shared test routers before the eventual router upgrade.
- Added a React compatibility guard that retains Strict Mode, rejects
  UniRouter-owned `findDOMNode` calls, and blocks React 19 while the exact Semi
  UI dependency waiver remains active.

### Fixed

- Covered absolute deep links, query/hash restoration and catch-all replace
  redirects under the future router behavior, while making affected async
  component tests wait for observable UI state instead of render counts.

### Compatibility boundary

- UniRouter source has no `findDOMNode` call. The visible React 18 warning from
  Semi UI 2.100.0 remains an exact, non-suppressed temporary waiver and must be
  removed before React 19; this release does not claim the upstream warning is
  fixed.

## [1.2.0] - 2026-08-24

### Added

- Reviewed provider/model onboarding templates that compose provider presets
  with an exact versioned Model Execution Profile.
- Safe defaults and constrained choices for public model identity, upstream
  model identity, context/output limits, and operation.
- A visible product version in the administration shell plus a CI guard that
  keeps `VERSION`, frontend package metadata, and this changelog aligned.

### Changed

- Renamed the primary model onboarding UI from the implementation-oriented
  “Canonical Deployment” wording to the user-facing “模型接入”; canonical
  deployment and operation identities remain visible as advanced details.
- Canonical model creation now pins the selected reviewed execution profile;
  omitted profile selection remains explicitly unverified rather than inferred.
- Updated the system fact map and codebase map for the current M83/Profile
  onboarding boundary.

### Fixed

- Kept model-onboarding SideSheets and nested page action groups inside narrow
  mobile viewports without changing their desktop widths.

### Verification scope

- Local backend/frontend contract tests, frontend type/build gates, and a
  bounded browser UI audit are required before this release entry is marked
  externally delivered.

> This is the first repository-governed product SemVer entry. It advances the
> existing deployed `1.1.62-*` runtime line without rewriting that historical
> build identity.
