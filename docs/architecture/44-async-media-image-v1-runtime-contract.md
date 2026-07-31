# Async Media Image V1 Runtime Contract

> Status: repository-local implementation contract
>
> Product decision: P7-D03 confirmed by the User on 2026-07-31
>
> Scope: UniRouter-owned text-to-image job plane only

## Decision

Image V1 uses the existing
`unirouter.openai.images.gpt-image.v1` Endpoint Profile as its provider-wire
evidence baseline. Outputs are stored in a tenant-scoped S3-compatible object
store and expire seven days after durable persistence. Video remains disabled
until it has an independent Endpoint Profile and conformance evidence.

The runtime is an isolated job plane. It does not turn Connector SDKs,
Endpoint Profiles, Tool Gateway, MCP Gateway, or the completed-execution Feed
into a shared high-privilege plugin runtime. Existing Chat, Embeddings,
customer-ledger, Outbox, completed Feed, and Cursor behavior remain unchanged.

## Public Contract

The gateway-authenticated contract is:

- `POST /v1/media/jobs` creates one text-to-image job and requires
  `Idempotency-Key`;
- `GET /v1/media/jobs/{id}` returns metadata-only state;
- `POST /v1/media/jobs/{id}/cancel` requests cancellation;
- `GET /v1/media/jobs/{id}/content` proxies an authorized, unexpired output
  without disclosing a bucket, object key, or signed URL.

The console reads the same tenant facts through JWT-authenticated
`/api/v1/media-jobs` routes. Project-scoped roles must select and be authorized
for one project. The desktop inspector is always visible; selecting a row only
changes its content.

Runtime readiness and new-job admission are separate controls. Operators may
pause admission while leaving the object store, recovery worker, expiry
enforcement, and purge worker active. Admission cannot be enabled while the
runtime is disabled. Both controls default to false; enabling the runtime alone
does not implicitly admit new paid work.

Admission is capped at 10 nonterminal jobs per project and 100 per
organization. The atomic store checks an exact idempotent replay before the
admission switch and capacity limits, so pausing or filling capacity cannot
hide an already accepted identity; only genuinely new work is rejected.

Requests support exactly one image, no streaming, no provider-hosted URL, and
the strict GPT Image request fields already frozen by the Endpoint Profile.
Unknown fields, duplicate JSON keys, nulls, multiple images, streaming, and
oversized prompts fail before persistence or dispatch. Provider HTTP redirects
are not part of the profile and are never followed, so an upstream cannot move
the credential-bearing request to a second location. The shared Image V1
profile owns one 32 MiB decoded-image ceiling and the corresponding bounded
Base64 JSON-envelope ceiling; Provider parsing and object persistence use
those same constants.

## Durable Identity

The server owns `job_id` and `request_id`. Client idempotency material is never
stored in clear text. The durable identity is HMAC-bound to:

1. organization;
2. project;
3. API-key identity;
4. the client `Idempotency-Key`.

Idempotency and object-key HMAC inputs use distinct V1 domain labels even
though both derive from the deployment's API-key hashing authority.
For every unexpired job, `API_KEY_HASH_SECRET`, the S3 bucket, and the
normalized S3 prefix are therefore an immutable identity set. Operators must
not rotate or rename any member in place while jobs remain; a future rotation
requires an explicit locator migration and compatibility procedure.
The normalized request has a separate SHA-256. Reusing one idempotency identity
with different normalized bytes is a conflict; the existing job is not
mutated. The prompt is encrypted with the existing application encryption
boundary while queued or retryable and is erased from the application row at
terminal state. Prompt text, output bytes, provider credentials, storage
locators, and signed URLs never enter API responses, logs, charge facts, or
Outbox payloads.

## Orthogonal State Axes

One overloaded status string is not authoritative. Each job records:

| Axis | V1 states |
|---|---|
| Admission | `accepted` |
| Queue | `queued`, `leased`, `terminal` |
| Lease | `unleased`, `leased`, `released` |
| Execution | `pending`, `running`, `terminal` |
| Provider outcome | `pending`, `succeeded`, `failed`, `not_started`, `unknown` |
| Cancellation | `not_requested`, `requested`, `accepted`, `too_late`, `indeterminate` |
| Object | `pending`, `persisted`, `purge_pending`, `purged`, `not_created` |
| Settlement | `not_eligible`, `charge_fact_recorded` |

Leases carry a monotonically increasing fence. A stale worker cannot complete,
retry, fail, or settle a reclaimed job. Expired leases are reclaimable. A
valid runtime configuration reserves at least sixty seconds beyond the
provider timeout for the durable success fence and output handoff. A
cancellation accepted before dispatch produces no provider call. Cancellation
that races a terminal provider success becomes `too_late`; it does not rewrite
success and does not imply a refund. A cancellation first requested after any
other known terminal fact is also `too_late`; an earlier accepted cancellation
remains idempotently `accepted`.

A transport failure after dispatch, or an HTTP success whose output/usage cannot
pass the frozen Endpoint Profile, is `provider_outcome=unknown`; it is never
silently reclassified as a provider failure or automatically retried. A
cancellation against that fact is `indeterminate`. A worker lease that expires
after dispatch but before a durable terminal provider fact is recorded has the
same ambiguity: it becomes terminal `unknown` and is never redispatched.
Provider implementations must explicitly classify a failure as definitive and
retryable; an unclassified implementation error fails closed as terminal
`unknown`.

## Output Persistence and Recovery

Object keys are server-derived from the tenant and job identities and are never
accepted from a caller. The S3 adapter writes one immutable image object and
one bounded recovery receipt. A retry first checks that receipt, so a process
crash after object persistence can finish the database transition without a
second provider request. Terminal delivery state, object metadata, and the
customer charge fact are committed together in one database transaction.

The worker fences validated provider success in PostgreSQL before it begins
object persistence. From that point onward, retries are recovery-only and can
never call the model provider again. If the success-fence response is lost, the
worker may continue with its in-memory output only after a read-back proves the
same live lease, fence, mapped model, and recorded provider success. Recovery
requires strict JSON, the full receipt to match the image's bounded metadata,
and the four usage classes to preserve their exact input-token partition. If
durable output cannot be recovered within the bounded attempts, the terminal
record preserves
`provider_outcome=succeeded`, `object=not_created`, and
`settlement=not_eligible`; UniRouter absorbs the provider-side uncertainty and
does not invent a customer charge.

The seven-day retention clock starts only after the image is durably stored.
After `expires_at`, content reads fail closed even if a transient S3 deletion
failure leaves physical bytes pending. Purge retries use a separate lease and
cannot make an expired object readable again. A production bucket must also
carry an independently reviewed lifecycle policy as defense in depth; this
repository-local implementation does not claim that target policy is present.

## Billing Finality

A customer charge fact may be created only when both conditions are true:

1. the Endpoint Profile validated a terminal provider success and authoritative
   provider usage;
2. the output and recovery receipt were durably persisted.

The fact preserves the distinct provider unit classes. The current text-token
price table cannot losslessly price GPT Image input-text, input-image, and
output-image units. Image V1 therefore records an immutable
`pricing_pending_media_rate_card` fact but does not debit a wallet or invent an
amount. A later media rate card may consume that fact; it may not backdate a
charge by reinterpreting text prices.

## Compatibility and Non-goals

- no change to existing request records, Chat/Embeddings paths, routing
  semantics, customer ledger rows, completed-execution DTOs, Outbox identities,
  Feed pages, credentials, or Cursor behavior;
- no image edit, variation, multiple image, streaming, callback, public object
  URL, video, audio, provenance-signing, refund automation, marketplace, or
  generic code execution;
- no provider or object-store traffic is evidence of this repository-local
  implementation; target credentials, deployment, paid traffic, database
  migration execution, and production proof require separate authorization.
