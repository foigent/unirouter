# PRD6 Batch 0B P6-F17 Operation And Idempotency Identity — Local And CI Evidence

> Date: 2026-07-11
> Status: CI verified and merged by PR #149 as `c9402fa`; post-merge main CI passed
> Scope: repository-only P6-F17 implementation and verification
> Authorization boundary: no deployment, target mutation, real/paid provider
> request, credential change, response-body persistence, exporter, external
> integration, or data-scope expansion occurred

## Outcome

P6-F17 is implemented and CI verified for the accepted Scheme A boundary:

- every HTTP request receives a cryptographically random, caller-unoverrideable
  server operation ID exposed through response `X-Request-ID` and retained in
  legacy `request_id` storage/API fields;
- caller correlation is separate as `X-UniRouter-Client-Request-ID`; inbound
  `X-Request-ID` remains only a compatibility alias and never becomes the
  server or financial identity;
- Chat and Embeddings accept `Idempotency-Key` with a 24-hour metadata-only
  receipt scoped by `(org_id, api_key_id, keyed key digest)` and bound to a
  keyed canonical request fingerprint;
- the raw idempotency key, prompt and model response are not stored; a repeated
  completed request returns `409` plus the original operation ID and status,
  not the model answer;
- trace/span, trusted workflow/Agent identity, route-attempt identity and
  provider request identity remain distinct while one server operation ID
  continues through gateway, billing, call log and audit evidence;
- financial reservation lookup and primary-key uniqueness are tenant scoped;
  the same external-looking value cannot cross an organization boundary;
- the Logs API/UI can filter and display server operation, client correlation,
  trace, workflow, Agent, attempt and upstream identities. An earlier failed
  attempt can resolve its operation through tenant/project-scoped trace lookup.

Source anchors:

- `internal/middleware/request_identity.go:16-187`;
- `internal/middleware/idempotency.go:23-416`;
- `cmd/uniroute/main.go:215-226,479-486`;
- `migrations/000021_prd6_operation_idempotency_identity.up.sql:1-118`;
- `internal/billing/billing.go:142-343`;
- `internal/gateway/relay.go:395-519,599-620,951-980,1038-1092`;
- `internal/gateway/trace.go:30-87,122-221`;
- `internal/admin/log_handlers.go:24-230,251-430`;
- `frontend/src/pages/LogsPage.tsx` and
  `docs/tech-ref/05-openapi-spec.yaml`.

## Scheme A Request Semantics

| Situation | Result | Provider/billing dispatch |
|-----------|--------|---------------------------|
| No `Idempotency-Key` | Existing request behavior; a new server operation ID is still generated | Normal |
| First valid key/payload | Atomically claims `in_progress` then dispatches | Exactly one claim winner |
| Same key and canonical payload while running | `409 idempotency_error.request_in_progress`, original operation ID, `Retry-After: 1` | No second dispatch |
| Same key and canonical payload after 2xx completion | `409 idempotency_error.already_completed` metadata receipt | No second dispatch and no response replay |
| Same key and canonical payload after non-2xx/panic | `409 idempotency_error.original_failed` metadata receipt | No second dispatch |
| Same key with changed method/URI/payload | `409 idempotency_error.key_reused_with_different_request` | No dispatch |
| Claim store unavailable or required scope/secret missing | Fails closed with `500` before provider dispatch | No dispatch |
| Receipt expiry | The key may be atomically claimed as a new operation | Normal after the 24-hour window |

JSON object key order is canonicalized; business-semantic equivalence is not
invented. This avoids semantic-cache behavior inside financial idempotency.

## Acceptance Evidence

| Acceptance requirement | Evidence and result |
|------------------------|---------------------|
| 100 descendants share one trace without charge-identity collision | `TestP6F17HundredChildOperationsShareTraceWithoutIdentityCollision` writes 100 distinct `op_` identities with one trace/workflow and distinct Agent identities: PASS |
| Each of 100 child operations settles at most once | `TestP6F17HundredDescendantOperationsSettleAtMostOnceEach` creates 100 real PostgreSQL reservations, settles each once, rejects a second settle, and reads back 100 terminal rows/transactions with zero frozen balance: PASS |
| Same payload replay is safe | middleware unit test and migrated gateway E2E return metadata-only `409`, preserve the original operation ID, retain one provider call and one financial operation: PASS |
| Changed payload is rejected | `TestIdempotencyGuardChangedPayloadReturnsMismatch`: PASS |
| Concurrent duplicate cannot dispatch twice | in-memory overlap test plus PostgreSQL 100-way atomic claim with exactly one winner: PASS |
| Cross-tenant/key collisions do not interfere | PostgreSQL store namespace test, two-organization financial operation test and existing finance JWT matrix: PASS |
| Concurrent admin financial correlation has one effect | Real PostgreSQL top-up and grant pairs with the same tenant/client ID produce one mutation/ledger row and a deterministic duplicate response; each HTTP attempt keeps a distinct server operation ID: PASS |
| Retry/failover attempts are distinct and truthful | two distinct `att_` events keep one operation; response reports `fallback`, attempt count and final attempt; a mixed HTTP-error then network-error regression proves the all-fail call log uses only the final route/attempt facts and does not retain an earlier status/upstream ID: PASS |
| Migration up/down/roll-forward | isolated migration test applies `000021`, permits cross-org correlation reuse, executes the safe partial down, retains hardened tenant uniqueness/width, removes the ledger row, and reapplies forward: PASS |
| Operator readback | Real PostgreSQL Audit insert plus operation/client filters, List/detail/trace API and Logs/Audit UI tests expose/filter separate identities without widening tenant/project access: PASS |
| Raw key/answer non-persistence | storage accepts only HMAC digests and status metadata; E2E receipt contains no `choices`; migration contains no prompt/response column: PASS |

## Verification Commands

The following completed successfully on the current branch:

```text
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go test -tags=integration ./internal/middleware ./internal/billing ./cmd/uniroute -count=1
go test -tags=integration ./internal/admin -run 'TestP6F32FinanceAndUsageTenantIsolationIntegration' -count=1 -v
go test -tags=integration ./cmd/uniroute -run 'TestPRD2P0DeveloperProjectKeyGatewayE2E_Integration' -count=1 -v
cd frontend && npm test -- --run
cd frontend && npm run type-check
cd frontend && npm run build
Ruby YAML parse of docs/tech-ref/05-openapi-spec.yaml
git diff --check
```

Observed results:

- normal and race Go suites: PASS; the known non-failing macOS race-linker
  `LC_DYSYMTAB` warning remains;
- migrated integration paths: PASS, including migration `000021`, one-provider-
  call replay proof and the two-tenant finance/usage matrix;
- frontend: 27 files / 136 tests PASS, type-check PASS, production build PASS;
  the existing large JavaScript chunk warning remains;
- OpenAPI parse and diff whitespace check: PASS;
- local Docker/nginx executables were unavailable, so Docker image and nginx
  configuration validation remain required CI evidence.

## Safety And Rollout Boundary

Migration `000021` changes financial uniqueness from global `request_id` to
`(org_id, request_id)`. Old binaries still query by request ID alone and are
therefore not safe beside new writers after a legitimate cross-tenant duplicate
exists. Any later target activation must:

1. stop/drain all old gateway and control-plane writers;
2. apply the migration;
3. start only the new binary and run same-tenant/cross-tenant smoke readback;
4. roll back only through an explicitly reviewed restore/cutover plan.

The supplied `.down.sql` is a safe structural exercise, not authorization to
restart an old binary. It deliberately retains the composite financial key,
tenant-scoped grant index and widened client-correlation column because
restoring the old global constraints could either fail on legitimate data or
reintroduce the critical collision. New binaries must also be drained before
running the down script because it removes columns they query.

## Explicit Remaining Boundaries

- P6-F18 remains open. This batch does not claim pool-safe Freeze pricing,
  concurrent Settle/Cancel terminal CAS, `settlement_pending`, process-kill
  recovery, price/multiplier pinning or post-output financial durability.
- If receipt finalization cannot reach PostgreSQL, the row remains safely
  `in_progress` until expiry rather than risk a duplicate provider call. Durable
  finalization/replay belongs to accepted P6-F20.
- Trusted workflow/Agent headers are accepted only through the existing
  server-to-server evidence trust boundary. Browser CORS does not allow the
  evidence-auth or trusted Agent headers.
- Scheme A is not response caching. It stores no model answer and is unrelated
  to the future independently deployable Cache Service recorded in
  `docs/planning/PRD7-CACHE-SERVICE-CANDIDATE.md`.
- No target, production, multi-instance capacity or SaaS claim follows from
  this result. PR #149 implementation head
  `c1db6142bb910e47843dc2282c2ff7e36477d26d` passed all required jobs in run
  `29152968280`, was squash-merged as
  `c9402fad323e8656493ff1bb29367a5dcf118331`, and post-merge main run
  `29153106336` passed Backend Tests, Frontend Build and Docker Build.
