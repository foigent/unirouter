# V1 Admin JWT Status And Pilot Catalog — Local Evidence

**Date:** 2026-07-16 (Asia/Shanghai)

**Repository baseline:** clean `main` / `origin/main` at
`0b37f961ef75e2f8ab889cfea66f71e282a53e9f`

**Implementation branch:** `codex/v1-admin-revocation-pilot-catalog`

**Status:** administrator principal-state enforcement, matching frontend
transient-failure behavior and bounded/batch model-list governance are Local
verified. The complete repository local suites also pass; the pull request,
required CI, merge and development-target deployment/configuration are pending.
This file therefore makes no CI or Target verified claim.

## Decision and scope

The User accepted the two recommended first-release boundaries:

1. protect every administrator request with a live principal-state check so an
   already issued JWT is rejected while its signed user, organization or role no
   longer matches an active database principal; and
2. expose and admit only the already Target-verified pilot models,
   `mimo-v2.5-pro` and `DeepSeek-V4-Flash`, during initial customer testing.

This is a Post-PRD6 first-release correction. PRD6 remains
`Completed (Accepted Scope)` and is not reopened. The batch does not add a
session center, token blacklist, vendor catalog discovery, provider adapter,
new model, new price, new credential, paid request, migration, License feature,
PRD7 feature, Production behavior or X-Insight behavior.

## Issue classification

| Issue | Classification | Impact | Cause | Priority / owner | Current fix | Acceptance boundary |
|---|---|---|---|---|---|---|
| `V1-ADMIN-STATUS-001` | confirmed bug/regression | A disabled, deleted or authority-changed administrator could continue using an unexpired JWT until its 24-hour expiry | The previous middleware verified only JWT signature and claims, without reconciling them to current database principal state | High / P1 / UniRouter auth | Bind every protected admin request to the exact signed user, organization and role and require active user plus active organization; fail state lookup closed | Local auth/backend/frontend checks pass; required CI and an already-issued-token target replay remain pending |
| `V1-PILOT-CATALOG-001` | confirmed product-consistency defect plus target configuration | Unverified Kimi, DeepSeek Pro or other price/key entries could appear as available even though only Mimo Pro and DeepSeek Flash have bounded target evidence | `/api/v1/model-catalog`, project model sets, API-key `allowed_models` and `/v1/models` did not share one effective governance filter | High / P1 / UniRouter gateway plus target operator | `/v1/models` now intersects explicit, wildcard and fallback candidates with effective callable project governance in one bounded query; configure the pilot project/key to the two accepted models without deleting internal records | Local unit/race/real-PostgreSQL parity passes; required CI and target API/UI/readback plus zero-dispatch rejection are pending |
| `V1-TARGET-PILOT-001` | target evidence | Two named providers do not prove capacity, detector, enterprise IdP, customer operations, HA or Production readiness | Those scenarios require separate environments and authorization | High / target owners | Keep the pilot model boundary narrow and preserve existing bounded provider evidence | Remains open after this batch |

## Administrator principal-state behavior

`AdminAuth` now receives the database query boundary used by the running
application. After signature and expiry validation, each protected request
requires one current row matching all of the signed claims:

- `users.id == JWT user_id`;
- `users.org_id == JWT org_id`;
- `users.role == JWT role`;
- `users.status == active`; and
- the joined organization status is `active`.

A missing or mismatched principal returns `401 auth_error.invalid_token` before
the protected handler runs. An unavailable principal-state query returns
`503 auth_error.state_unavailable`; the middleware does not fall back to the
old stateless authorization behavior.

This is request-time state enforcement, not permanent or cryptographic token
revocation. The signed JWT is not rewritten or placed on a blacklist. If the
same user is restored to the same organization, role and active state before
the token expires, that old token can become usable again. Password changes and
logout do not independently revoke it, and a request already admitted before a
status change is not interrupted. Per-session revocation remains separate
native-IAM scope.

## Frontend transient-auth behavior

The new database dependency adds a legitimate `503` authentication-state path.
The frontend therefore distinguishes authentication rejection from temporary
state unavailability:

- `401` clears the local token and returns the user to login;
- `503` and other non-401 profile errors retain the existing token;
- the protected route renders an explicit temporary-error state and retry
  action; and
- the error state prevents an automatic retry loop while the service remains
  unavailable.

This does not make a failed profile request successful or continue into an
authorized page without a user role. It preserves the session material only so
the user can retry after a transient control-plane failure.

## Pilot catalog consistency

The accepted V1 pilot identities are exact:

| Public model | Existing target evidence |
|---|---|
| `mimo-v2.5-pro` | bounded non-stream and stream requests through Mimo channel/key 7 |
| `DeepSeek-V4-Flash` | public-to-supplier `deepseek-v4-flash` mapping, two bounded requests and the dated pilot price |

The implementation does not hard-code those two names as a global product
catalog. The OpenAI-compatible `/v1/models` response now applies the same
effective callable project governance used by request admission to explicit
key model lists, wildcard keys and `model_prices` fallback candidates.
`FilterCallableModels` normalizes and deduplicates all candidates and resolves
active aliases, lifecycle and project model-set rules in one database query,
preserving candidate order. A missing production API-key context fails closed
instead of falling back to the global price list. No-project keys still retain
legacy unregistered-model compatibility but no longer list retired models.

`approved_catalog` intentionally remains a presentation flag, not a call-
admission authority. The pilot project model set and API-key scope are what make
the selected two-model target boundary enforceable; target configuration and
readback remain pending.

The later target operation must use reversible supported configuration so the
pilot project model set and pilot key expose only the two accepted identities.
Other model-registry and price records remain retained with their existing
lifecycle. They must not be deleted or mislabeled as supplier-deprecated/
retired merely to hide them from the pilot.

## Local verification completed

The auth/frontend slice has passed the following local checks on the shared
branch:

- `go test ./internal/middleware ./cmd/uniroute -count=1`;
- disabled-user already-issued-token integration coverage, including a race
  run;
- `go test -tags=integration ./internal/admin -count=1`;
- `go test -race ./internal/middleware ./cmd/uniroute -count=1`;
- both focused frontend auth-store regressions; and
- all 169 frontend tests, the production frontend build and `git diff --check`.

The model-list slice also passed:

- `go test ./internal/modelgov ./internal/gateway -run 'Test(FilterCallableModels|HandleModels_)' -count=1`;
- `REQUIRE_TEST_DATABASE=1 go test -tags=integration ./internal/modelgov -run TestFilterCallableModels_MatchesCheckAccessIntegration -count=1 -v` against a real migrated PostgreSQL database;
- `go test ./internal/modelgov ./internal/gateway -count=1`;
- `go test ./cmd/uniroute -count=1`;
- `go test -race ./internal/modelgov ./internal/gateway -count=1`;
- `go vet ./internal/modelgov ./internal/gateway`; and
- `git diff --check -- internal/modelgov internal/gateway`.

Coverage proves 128 candidates still use one governance query; production
context does not re-query API-key authority; project model sets intersect both
explicit and fallback candidates; aliases, retired/unregistered compatibility,
empty/duplicate values and database errors match per-request `CheckAccess`
semantics; and a query error returns no partial model list.

The focused regression proves an already issued token succeeds while the user
is active, returns 401 after disablement, and becomes accepted again if the
same principal state is restored. Unit coverage also proves missing/mismatched
state returns 401 and database uncertainty returns 503 without invoking the
next handler.

## Full local verification

The branch additionally passed:

- `go test -race ./... -count=1`;
- `go vet ./...`;
- `REQUIRE_TEST_DATABASE=1 go test -tags=integration ./internal/middleware ./internal/modelgov ./internal/admin ./cmd/uniroute -count=1`;
- `npm test -- --run` in `frontend` (34 files, 169 tests); and
- `npm run build` in `frontend`.

Local Docker is unavailable on this workstation; the required Docker Build job
remains the authoritative image-build gate.

## Pending before publication

1. Confirm the final diff contains no unrelated feature or schema change.
2. Open a pull request and require Change Routing, Backend Tests, Frontend Build
   and Docker Build as selected by the actual backend/frontend diff.
3. Do not label this batch `CI verified` before the exact PR head passes.

## Pending target acceptance

After merge, the separately authorized development/test ECS verification must
record exact image/commit continuity and prove:

1. preflight stable/canary readiness, zero restarts, migration `000037`, active
   test License, release trust and signed sequence `3`, then prove the new
   formally signed release records monotonic sequence `4` after cutover;
2. one temporary active administrator's issued token reaches a protected API,
   then the same token returns 401 after the account is disabled;
3. temporary passwords/JWTs are never printed or persisted and the actor ends
   disabled;
4. `/api/v1/model-catalog`, Developer Console and a pilot key's `/v1/models`
   expose exactly `mimo-v2.5-pro` and `DeepSeek-V4-Flash`;
5. an excluded Kimi/DeepSeek Pro request is rejected before dispatch with zero
   provider attempt, freeze, settlement and paid supplier call; and
6. exact prior catalog/model-set/key values are retained as rollback evidence.

## Preserved non-claims

1. No PR, required CI, merge, image build or ECS deployment has completed at
   this local evidence checkpoint.
2. No target user, JWT, model registry, model set, API key, route, price,
   channel or provider credential has changed in this local stage.
3. The existing ae9 development-target release and all earlier evidence remain
   the target authority until a later deployment/readback succeeds.
4. Kimi and DeepSeek Pro are excluded from the selected V1 pilot; they are not
   verified, broken, retired or accepted by this decision.
5. Capacity, external detector, enterprise IdP, customer backup/restore/upgrade,
   Production/TLS/HA, durable markup and X-Insight v2 acceptance remain open.
