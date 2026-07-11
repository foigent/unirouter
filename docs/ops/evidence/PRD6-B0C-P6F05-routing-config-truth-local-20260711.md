# PRD6 Batch 0C P6-F05 Effective Configuration And Budget Alert Truth Evidence

> Date: 2026-07-11
> Scope: UniRouter typed effective configuration, routing policy and project-scoped budget-alert contract
> Risk: R3 request routing, persisted provider-health and tenant financial-notification behavior
> Evidence level: Local verified; required PR CI pending
> Deployment/target/production claim: none

## 1. Confirmed pre-change divergence

The repository already declared five failover environment/configuration fields:
`MAX_RETRY_COUNT`, `CIRCUIT_BREAKER_THRESHOLD`,
`CIRCUIT_BREAKER_RECOVER_SECONDS`,
`CIRCUIT_BREAKER_HALF_OPEN_PERCENT` and `KEY_COOLDOWN_SECONDS`. Read-only source
reconciliation found no production read of any corresponding `cfg.*` field
after `config.Load`.

Runtime behavior instead used separate literals:

- Chat and Embeddings consumed every compatible candidate; `MAX_RETRY_COUNT`
  had no effect;
- relay call sites supplied channel failure threshold `5` and recovery `300`
  seconds directly;
- both route selectors sampled half-open candidates with a literal `10%`;
- provider 429 used a literal 60-second key cooldown;
- provider 401/403 used a separate unreported 3600-second cooldown;
- provider keys became invalid after a separate literal three failures;
- runtime status, UI and route-decision trace exposed none of the effective
  values;
- the half-open deadline regression only checked SQL text with a mock and did
  not prove a real PostgreSQL before/deadline/after state transition.

The same source audit confirmed a second accepted P6-F05 truth gap:

- seed data declared `budget_alert_threshold_cents`, while the runtime loaded
  the nonexistent `budget_cents` global setting;
- the legacy alert interpreted that value as a total budget, selected
  `wallets ORDER BY org_id LIMIT 1`, and rate-limited across all tenants;
- its successful-webhook path attempted an `audit_logs(action, detail)` insert
  even though the current schema requires organization, user, resource and
  other fields, and it ignored the insert error;
- the already implemented product contract instead uses project-scoped
  `projects.budget_cents` / `projects.budget_period`, tenant/project-scoped
  `notification_events`, and verified `notification_webhooks`.

These are confirmed source/runtime-wiring findings, not evidence that a target
operator attempted an override or suffered an incident.

## 2. Typed effective registry and startup policy

`internal/runtimeconfig` now owns the typed, read-only `effective-config.v1`
registry. It contains the exact `routing-config.v1` driver values plus the one
active `project-budget-alert.v1` schema contract. `internal/routingpolicy` owns
immutable routing defaults, validation and the reusable value type.
`config.Load` maps the environment once, validates it fail-closed, and main
passes the same registry snapshot to the production Router and administrator
runtime status.

| Effective field | Environment | Default | Accepted range |
|-----------------|-------------|---------|----------------|
| provider retries after first dispatch | `MAX_RETRY_COUNT` | 1 | 0..10 |
| channel failure threshold | `CIRCUIT_BREAKER_THRESHOLD` | 5 | 1..100 |
| recovery wait | `CIRCUIT_BREAKER_RECOVER_SECONDS` | 300 s | 1..86400 s |
| half-open candidate percentage | `CIRCUIT_BREAKER_HALF_OPEN_PERCENT` | 10% | 1..100% |
| 429 key cooldown | `KEY_COOLDOWN_SECONDS` | 60 s | 1..604800 s |
| provider 401/403 key cooldown | `PROVIDER_AUTH_COOLDOWN_SECONDS` | 3600 s | 1..604800 s |
| transient failures before key invalidation | `PROVIDER_KEY_FAILURE_THRESHOLD` | 3 | 1..100 |

Unusable candidates rejected before provider dispatch do not consume the retry
budget. Once a provider is dispatched, `MAX_RETRY_COUNT=0/1/2` permits exactly
one/two/three provider calls. A retryable failure records `stop` when the budget
or candidate set is exhausted.

The same immutable policy now drives:

- Chat and Embeddings provider retry budgets;
- persisted channel failure threshold and recovery deadline;
- Route/RouteAll half-open sampling;
- 429 and provider-auth key cooldown deadlines;
- provider-key failure invalidation threshold.

The independent legacy `internal/circuitbreaker` library remains test-only and
is not represented as production routing state. P6-F05 neither activates it nor
claims two simultaneous breaker implementations.

## 3. One active budget-alert contract

The unsafe legacy `AlertService`, global first-wallet worker and Relay wiring
are removed. No global webhook or threshold value is loaded at runtime. The
existing post-settlement `EventService.CheckProjectBudget` path remains the one
active product behavior:

- scope: the authenticated request's organization and project;
- budget/period sources: `projects.budget_cents` and
  `projects.budget_period`;
- occurrence ledger: `notification_events` with
  `event_type=project_budget.threshold`;
- delivery routes: same-tenant/same-project `notification_webhooks` whose scope
  has been verified;
- configuration audit: existing project and notification-webhook administration
  actions remain in the management audit ledger; automatic alert occurrences
  remain notification events rather than malformed synthetic administrator
  actions.

Historical setting rows are not deleted. `GET /api/v1/settings` marks
`budget_alert_webhook_url`, `budget_alert_threshold_cents` and `budget_cents`
as `retired=true` and `effective=false`, with replacement APIs. Generic writes
to those keys return 409 before a transaction. This preserves historical data
while preventing an operator from believing an ignored value is active.

## 4. Safe observability

Every `routing.route_selected` trace now carries safe scalar values for
`effective_config_version`, `routing_config_version`, retry count, circuit threshold/recovery/probe,
rate/auth cooldown and provider-key threshold. Metadata sanitization explicitly
allows only those named fields; no environment dump, URL, key or credential is
recorded.

`GET /api/v1/runtime/status` returns the same values under
`effective_configuration` with `version=effective-config.v1`. Its nested
`routing_policy` reports `version=routing-config.v1` and
`source=startup_environment`; `budget_alerts` reports the project sources,
event/delivery ledgers and explicitly states that legacy global settings are
inactive. Internal Status displays separate “有效路由策略” and
“有效预算告警合同” surfaces in operator-readable units. The OpenAPI description
records this contract.

This is startup configuration truth, not dynamic configuration. Changing an
environment value requires an ordinary controlled restart; cross-replica
dynamic rollout/version consensus belongs to accepted Routing Policy V1 and is
not silently added here.

## 5. Acceptance evidence

### Config and retry matrix

- defaults are stable and validate;
- explicit environment overrides `4/7/90/25/45/1800/6` survive Load and appear
  unchanged in the effective policy;
- an out-of-range retry value fails startup validation;
- with three provider candidates, retry budgets `0`, `1` and `2` produce exact
  call counts `[1,0,0]`, `[1,1,0]` and `[1,1,1]` respectively;
- the same route trace contains the selected version and all override values;
- configured channel threshold/recovery, rate/auth cooldown and key threshold
  have focused unit evidence.

The focused routing/config/admin/trace race matrix passed ten repetitions.

### Real PostgreSQL state transitions

`TestRoutingPolicyPostgresStateTransitions` creates an isolated current-schema
database and proves:

1. one failure remains healthy below configured threshold 2;
2. the second failure persists `half_open` with a real one-second future
   recovery deadline;
3. RouteAll excludes the channel before that PostgreSQL deadline;
4. without manually changing the row, RouteAll admits it after the actual
   deadline when the configured probe percentage is 100%;
5. success resets failure count/state/deadline;
6. configured 2-second 429 and 3-second auth cooldown deadlines are persisted
   and exclude the key;
7. three key failures keep it active under configured threshold 4, and the
   fourth persists `invalid`.

The race test passed twice locally in 4.663 seconds. Required Backend CI now has
a separate fail-closed gateway integration step, using
`REQUIRE_TEST_DATABASE=1`, so unavailable PostgreSQL cannot create a skipped
green result.

### Project budget tenant scope

`TestEventService_CheckProjectBudgetTenantScope_Integration` creates an
isolated current-schema database with two organizations and two projects. The
first organization's project has the larger spend, but only the second
organization/project pair is evaluated. A foreign organization/project pair
produces no event; the valid pair persists exactly its own `org_id`,
`project_id`, 1,200-cent spend and 100% threshold in `notification_events`.
No legacy `budget_alert*` audit row is created. The race test passed twice
locally; required Backend CI runs it fail-closed with
`REQUIRE_TEST_DATABASE=1`.

## 6. Full local verification

```text
go vet ./...
go test -race ./... -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/admin -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/gateway \
  -run TestRoutingPolicyPostgresStateTransitions -count=1
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/billing \
  -run TestEventService_CheckProjectBudgetTenantScope_Integration -count=1
```

Result: PASS for all Go packages, the 48-case isolated admin integration suite,
the routing-policy PostgreSQL state-transition test and the two-tenant budget
alert contract test. Admin integration passed in 31.107 seconds, routing
integration in 2.625 seconds and budget integration in 1.681 seconds. The known
non-failing macOS `LC_DYSYMTAB` linker warning remains; no race was reported.

```text
npm run type-check
npm test -- --run
npm run build
```

Result: PASS for type-check, 27 files / 137 tests and production build,
including the effective-policy UI regression. Existing React/Semi and bundle
size warnings remain non-failing.

The OpenAPI YAML parses and `git diff --check` passes. Docker is not installed
in the local shell; required CI remains the Docker evidence source.

## 7. Acceptance and boundary

P6-F05 is Local verified. It can advance to CI verified / closed only after the
exact PR head passes Backend Tests (including all three fail-closed integration
steps), Frontend Build and Docker Build, is merged, and post-merge main CI
passes.

No deployment, live config change, credential operation, provider call, dynamic
policy service, migration, historical-setting deletion, target or production
claim is made. Provider HTTP
timeouts, stream/server timeouts, billing settlement retry and P6-F06 pricing
correctness remain separate contracts.
