# Kimi K2.7 Code Chat Core development/test closeout

Date: 2026-08-31
Environment: development/test
Evidence boundary: Target and external-integration evidence; not Production or
canonical-release evidence.

## Exact artifact and database state

- Main merge: `76763701461e1ccfdb80ec8e32d7c3009c9208f8`
- Git tree: `3a84f73250ae344cc180c43572f6da18185aa738`
- Product version: `1.2.23`
- Schema ledger ceiling: `000101_kimi_k27_chat_core_qualification`
- Backend image digest:
  `sha256:54398f8d1c520097de7481109d069d91375fc43764b15a57eee5375093977ed8`
- Frontend image ID:
  `sha256:6710df150a0c22597ca3beec11a670c4dd3260d35204ea1345c1880d57943f28`
- Publication V1 plan digest:
  `da98b72d71a46265f2db5d818bca568c3e59408c65b726a1b5ff3de07cafb3bc`
- Pre-publication backup:
  `/opt/unirouter/backups/20260831-1.2.23-kimi-k27-m101/postgres-before.dump`
  with SHA-256
  `c3e04714986ca12263a9b79622040d62ec906220c7dc8e4118cc0dbc9fb9b0e3`.

PR #357 passed Change Routing, Backend, Frontend, Browser E2E and Docker in CI
run `33337204059`. Stable, canary, frontend, PostgreSQL and Redis were healthy
after the non-rolling M101 publication/deployment sequence. A fresh browser tab
served `V1.2.23`; an older tab that still showed `V1.2.20` was stale client
state, not the deployed frontend artifact.

## Exact model, operation and qualification

The administrator UI created the later attachment after M101 publication,
proving that a new exact Deployment obtains its declared capability projection
from the server-owned current bindings rather than a frontend Profile allowlist:

- public model ID: `806`, public name `kimi-k2.7-code`
- Execution Profile: `kimi.k2.7-code.api@1.3.0`
- protocol: `protocol.kimi-openai-chat.v5@1.0.0`
- operation: `openai.chat_completions.v1`
- provider connection / credential binding: `10 / 10`
- Deployment: `5`

The operation Probe was successful as Probe `5`. Six capability Probes were
then individually approved for the exact tuple:

| Capability | Probe | Approval |
| --- | ---: | ---: |
| function tools | 56 | 48 |
| JSON Object | 57 | 49 |
| reasoning | 58 | 50 |
| streaming | 59 | 51 |
| automatic tool choice | 60 | 52 |
| tools | 61 | 53 |

Each capability row is `production_approved` and each receipt-aware
qualification resolves `qualified=true`. Release `26`, ref
`rcr_deployment_20260830t223323_9486e7148a98c397555176fa`, binds the exact
public model, Deployment and operation for the development/test stable route.
No Alias, canonical route, pricing activation or Production release changed.

## Bounded fixed-SDK evidence

The closeout used fixed `openai-python@3.5.0`, `max_retries=0` and the exact
public model. The complete K2.7 batch consumed 17 of the approved 20 supplier
calls: seven operation/capability Probes and ten public SDK upstream attempts.
No automatic provider retry occurred.

Call logs `403` through `412` all bind the exact Deployment `5`, Release `26`,
public model `806`, connection/binding `10/10` and Chat Completions operation.
They cover non-stream text, streaming, function tools, typed multi-turn tool
history, JSON Object and a two-request cache workflow. Nine public SDK attempts
completed successfully. One deliberately small streaming output limit of 64
was terminated by the gateway as `stream_error.output_limit_exceeded` after an
upstream HTTP 200; the evidence-driven 512-token streaming request then
completed successfully without retrying the failed request. Call log `412`
reported `750` cache-read tokens for the repeated `750`-token prompt.

Billing attempt rows are settled with known provider usage, except that the
bounded stream termination remains correctly marked partial/estimated at the
gateway counter. K2.7 has no active price, SupplierCostObservation or provider
invoice authority, so CNY 5 remains an authorization ceiling rather than an
observed supplier-cost claim. The project wallet balance, grant, frozen amount,
spend and transaction count remained zero; no CustomerCharge was created.

## Temporary access cleanup

The temporary workload Application
`app_generated_c1b0ea43f6e93fa1c4351020` used Grant `15` with
`scope_mode=project_default`. The Grant was not K2.7-only. Its sole application
credential `35` narrowed `allowed_models` to `kimi-k2.7-code`.

After the SDK closeout, the credential was explicitly revoked and the
Application was archived through the administrator UI. Database readback then
confirmed:

- Application `7`: `archived`, revision `2`;
- Grant `15`: `revoked`, revision `2`;
- credential `35`: `revoked`, `allowed_models=kimi-k2.7-code`.

The task-owned temporary SDK environment and plaintext one-time Key file under
`/private/tmp/unirouter-k27-sdk-kcxU77` were deleted after revocation. Immutable
access, audit, Probe, Approval, execution and billing evidence was retained.

## Explicit exclusions

This closeout proves only the approved K2.7 Code OpenAI-compatible Chat Core
subset. It does not authorize or claim Dynamic Tools, built-in search, media,
highspeed or subscription identities, required/named/parallel tool choice,
function-tool streaming, active pricing, customer billing, canonical traffic,
Production, K3, MiniMax or another provider.
