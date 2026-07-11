# PRD6 Batch 1 P6-F19A Agent Capability Kernel — Local Evidence

> Date: 2026-07-12
> Scope: repository/local evidence only
> Status: CI verified prerequisite slice; parent P6-F19 later closed by P6-F19B
> Parent row: P6-F19 CI verified / closed through PR #166 and main run `29171807693`

## Accepted contract exercised

- A child Agent is known only after explicit supervisor exchange and parent
  delegation; ordinary gateway traffic never creates an Agent identity.
- Capability tokens use `unirouter.agent-capability.v1`, the `urc_` prefix, a
  strict issuer/audience/algorithm/key ID, and a maximum five-minute TTL.
  The v1 HMAC key is domain-separated from `API_KEY_HASH_SECRET`; no credential
  value is logged, returned through runtime status or added to documentation.
- The server recommends refresh near minute three with plus/minus 15 seconds of
  stable jitter. Concurrent refreshes converge on one non-persisted bearer
  token; Redis retains only a non-secret issuance grant and the superseded JTI.
- Normal expiry rejects a new admission. An already authenticated/admitted
  request does not depend on later token refresh.
- Workflow lifetime, depth, direct children and aggregate descendant limits are
  independent from token TTL. Redis Lua registration is atomic; keys use one
  workflow hash tag for Redis Cluster slot compatibility.
- Delegated model, endpoint, cost, token, RPM, RPS, concurrency, depth and
  topology ceilings may only stay equal or decrease.
- Explicit workflow revocation is checked through shared Redis on every new
  capability admission. Root API-key, organization, project, owner and project-
  membership authority is re-read through a bounded 15-second per-instance LRU
  cache with singleflight suppression. Expired cache refresh failure and Redis
  failure both deny capability admission.
- A capability grants UniRouter model-gateway authority only. It grants no Tool,
  IAM, database, write or export authority.

## Repository implementation evidence

- `internal/agentcap/` owns claims, signing, bounded authority cache, Redis state,
  exchange, delegation, refresh, revocation and unit/integration tests.
- `internal/capapi/` owns strict, size-bounded HTTP control endpoints and
  non-cacheable token responses.
- gateway authentication accepts root API keys or `urc_` capabilities while
  retaining the root API-key ID for aggregate legacy rate/billing boundaries.
- model and endpoint scopes are enforced before dispatch. `/v1/models` returns
  the capability's model subset; wildcard authority returns the real catalog,
  never a literal `*` model.
- signed capability workflow/Agent/actor identity is injected into the existing
  request evidence context and cannot be replaced by the optional trusted
  evidence-header side channel.
- runtime status and the frontend Internal Status page expose the non-secret
  lifecycle, outage, revocation and ownership boundary. Backend docs, frontend
  API docs and OpenAPI describe all four control endpoints.

## Local verification

Passed:

```text
go vet ./...
go test -race ./internal/agentcap ./internal/capapi ./internal/middleware ./internal/gateway ./internal/runtimeconfig ./internal/admin ./cmd/uniroute -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/agentcap -count=1
npm test -- --run                         # 27 files / 137 tests
npm run type-check
npm run build
ruby YAML.load_file(OpenAPI)              # parsed successfully
git diff --check
```

High-risk fixtures include:

- 101 concurrent unique child registrations under a 100-child/descendant root:
  exactly 100 succeed and one is atomically rejected;
- 20 concurrent refreshes: all receive the identical new capability, and the
  prior token is superseded;
- cross-service shared-Redis revocation readback;
- Redis outage fail-closed behavior;
- real migrated PostgreSQL owner/project membership and owner-disable authority
  refresh; and
- conflicting trusted evidence headers cannot override signed Agent identity.

## Pull request and required CI

- implementation head: `df80a710d9ea5e44cb72a1b83d619566a63fda89`;
- PR #164 exact-head required run: `29170347267`;
- required results: Backend Tests, Frontend Build and Docker Build all passed;
- squash merge: `43b90f85b0d4c655bc833a42829a8b458c581d35`;
- post-merge `main` run: `29170507588`, with Backend Tests, Frontend Build and
  Docker Build all passed; and
- historical branch `codex/prd6-agent-capability-kernel` was restored at the
  exact implementation head after merge.

The post-merge run emitted non-blocking GitHub Actions Node 20 deprecation and
Go cache-restore `tar` annotations. The Backend job, including the new migrated
Agent capability authority integration step, concluded `success`.

## Explicit non-claims and remaining work

- This slice alone did **not** close P6-F19. The later P6-F19B implementation
  connects shared runtime reservation/settlement and closes the parent row;
  see `docs/ops/evidence/PRD6-B1-P6F19B-agent-ancestor-budget-local-20260712.md`.
- P6-F07 must provide the Redis-backed RPM/RPS/TPM/concurrency policy and consume
  capability ceilings; P6-F21 must provide shared workload-weighted admission.
- Capability-authenticated model calls carry durable workflow/Agent evidence in
  the existing logs and trace path. A separate durable control-operation
  issuance/delegation/revocation audit/outbox is not claimed here and remains
  aligned with P6-F20 rather than being silently invented in this slice.
- Native admin JWT and native API-key revocation are not rewritten by this
  slice. The new 15-second bound applies to the Agent capability root-authority
  path; `P6-AUTH-001` remains open for the broader native-auth surface.
- Dedicated multi-key signing-secret rotation remains part of the separately
  deferred secret-version/rotation capability; this slice provides a strict v1
  `kid`, not a claim that P6-F12 is implemented.
- No target deployment, paid/real upstream request, credential mutation,
  additional data access, exporter/outbox enablement or X-Insight change is
  performed or claimed.
