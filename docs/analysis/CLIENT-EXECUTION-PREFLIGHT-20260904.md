# Client execution preflight and effective parameter projection

Status: implementation design; no runtime or Provider proof.

Base: `f4a0acce2a6e35038cb6eaf3b756c655a0e57438`, tree
`49e212eefc536ff1b27d89e0428c5e29eb5098ff` (remote main read on 2026-09-04).

## Outcome and bounded object

Product direction is usable, governed model execution across clients. The current
workstream still owes fourteen actual OpenClaw tool-task tests, followed by the
separately available Hermes environment. This first implementation object makes
the captured Chat request shape diagnosable before paid dispatch and projects
effective output defaults separately from hard limits. It is not an Agent SDK,
a provider qualification, an Agent-ready state, or a new routing authority.

The previous independent review of this proposal accepted four requirements:
reuse canonical preparation rather than legacy Preview; preserve caller/endpoint
authorization without execution effects; use effective Release/default authority;
and deliver repeat-successor lifecycle separately before M2.7 activation. This
design resolves the first three into code boundaries. The unchanged, locally
reviewed M111 candidate and its review budget are not reopened.

## Request preflight

- Add optional `POST /v1/chat/completions/preflight`, accepting the same Chat
  document, not a new client-specific envelope. It is not required before normal
  SDK calls and is not inserted into every request's network path.
- Bind authentication to the real Chat endpoint using a server-owned wrapper,
  not a client-supplied operation or header. Reuse the current live API-key/Agent
  capability, Grant, IP, license and retained-body/strict-JSON checks. Repeat
  authentication after buffering just as normal Chat does. Do not attach the
  model rate-reservation, idempotency, Freeze or execution middleware.
- Reuse API-key model scope, `modelgov.CheckAccess`, stable/canonical resolution,
  `EvaluateModelAuthorization` and raw-JSON model rewriting. The preflight path
  does not emit an unregistered *call* or model-execution rejection receipt.
  Ordinary access/security telemetry may remain; no prompt is persisted.
- Reuse common ingress output-limit validation, pre-Profile estimation,
  `prepareModelExecutionPlan`, exact history snapshot, Profile normalization,
  invocation requirements, canonical Planner and public output/default limits.
  Preserve the planner's safe exclusion reasons on no-route results. Do not
  expose route names, endpoints, canonical successor names, encrypted secrets,
  function arguments, prompt content or the raw Plan.
- Extract the existing credential-free per-route output injection/context
  calculation and OpenAI-compatible model/body shaping. Normal Chat execution
  and preflight use the same helpers. Adapter config and final outbound token
  bounds are checked before a route is reported locally admissible. Do not
  invoke decrypt, transport, half-open claims or an executor in preflight.
- Scope is canonical OpenAI-compatible Chat. Unsupported runtime/adapter paths
  report unavailable/not checked; they do not fall back to the old Preview.
  Other protocols keep their current execution behavior.
- Return the request name, operation, exact Profile/protocol, snapshot time,
  required capabilities, effective limits and deduplicated safe reason codes.
  A passing result means only that the submitted document has at least one
  locally admissible route in that snapshot. Explicitly list checks not performed:
  dynamic execution quota/reservation, content-policy transformations/compression,
  live credential validity/upstream behavior, response delivery and Agent outcome.
  Input/output body transformations at execution invalidate any broader inference.
  The actual request repeats all normal checks and may legitimately fail later.

## Effective model projection and thin client mapping

- Keep `/v1/models?include=execution_profile` opt-in and the existing status enum.
  `verified` remains static Profile/limit validation, not route qualification.
- For a canonical stable Release, take Profile and public limits from its sealed
  pins after checking model identity, active stage and sealing; do not promote a
  mutable registry override or newest source current pointer. A malformed or
  stale stable Release must not fall back to registry metadata as if executable.
- Add only provenance and request-default fields needed by consumers:
  `source` (`stable_release` or `registry`) and `request_default_output_tokens`.
  Existing fields carry the chosen source's exact Profile and hard ceilings.
  Default is calculated by existing `DefaultRequestOutputLimit`. Registry-only
  projections do not claim active Release defaults. Do not synthesize a mixed
  stable/canary contract; ordinary sync explicitly consumes stable metadata.
- OpenClaw sync requires a valid stable projection for governed defaults. Keep
  `maxTokens` as an output ceiling; write a missing default through supported
  model `params.maxTokens`, bounded by a retained explicit local ceiling. Preserve
  valid existing `params` output overrides and conservative experimental limits;
  reject conflicting output aliases, invalid/nonintegral values and overrides
  outside effective context/output limits before any write.
- Dry-run reports proposed limit/default changes, retained overrides and source
  without dumping other params or secrets. Do not create an ownership ledger or
  an OpenClaw-specific model-value catalog. Metadata from an older server without
  the required source/default is reported, never guessed from its hard maximum.
- Validate the resulting payload through the installed client's actual transport
  with a no-send capture. Headless SDK/CLI execution is a test driver, not a new
  UniRouter execution layer. No default Agent, credential or fallback is changed.

## Required verification

1. Authenticated preflight checks the Chat operation even though its URL differs;
   cross-org, model-scope, retired/canonical-only and denied-endpoint requests
   fail. Retained-body limits/strict duplicate-key rejection remain effective.
2. Default/explicit output fields, conflicting aliases, policy narrowing,
   first/continuation history, actual model rewriting and final outbound context
   bounds agree between preparation and preflight. The fourteen captured shapes
   supply cases, not a permanent second capability matrix.
3. Preflight performs zero Provider transport, decrypt, quota reservation,
   execution claim, Freeze, model/qualification mutation or execution receipt.
   Success and failures never serialize internal Plan/secret/body material.
4. A static verified Profile with absent qualifications is not reported as a
   route; missing/stale manifests, unavailable runtime and half-open candidates
   retain honest snapshot/dispatch boundaries.
5. Projection covers registry-only history, exact stable pins, mutable registry
   drift, wrong-model/stale/unsealed Release and default-versus-ceiling semantics.
   No extra query/work occurs when the opt-in projection is not requested.
6. Sync covers new models, retained experimental ceilings/params, absent server
   defaults, invalid/conflicting overrides, no-write dry-run and actual client
   output-field precedence with bounded-context behavior.

Use focused tests while editing, then one risk-proportionate stable-diff gate and
one bounded closing review. No workflow/security gate is weakened. Release
metadata follows the existing gate; no migration, Profile, Probe or Publication
identity is created by this object. GLM/M111/lifecycle integration is settled
before any cumulative artifact, never improvised on Target.

## Retained downstream work and authority

The authorized work continues through GLM streaming-tool composition, repeatable
stable successor lifecycle, compatible finite delivery batches and fourteen
actual tasks. Repeat-successor must preserve historical receipts and distinguish
already authorized dispatch from stale, merely planned requests. M111 and any
new migration order must remain self-consistent. No past Publication is replayed.

Per-model first-round caps remain CNY 10 and 30 upstream requests, cumulative
including failures, retries and direct controls. Reservations are not spending.
No Computer Use, media, Token Plan, new Agent platform, Pricing redesign or
Production is added. Previously valid evidence is retained. This document does
not create authority beyond the user's existing explicit authorization.
