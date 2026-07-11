# PRD6 Batch 0B P6-F35 Global-Control Authority — Local Evidence

> Date: 2026-07-11
> Base: `517ce45381429781f7e81e492bdd9f38093c21ba`
> Branch: `codex/prd6-p6f35-global-controls`
> Evidence level: local implementation, independent review, PR #147 final-head
> required CI, merge and post-merge main CI verified; target verification
> pending; no deployment or production verification is claimed

## Confirmed Pre-Fix Findings

The reviewed base mounted deployment-wide settings and all four sensitive-word
routes behind the organization-administrator guard. The settings table has no
organization scope, and the gateway consumes one in-process sensitive-word
filter for every tenant. An organization administrator could therefore read
or change global controls that affected other organizations.

The generic settings writer accepted arbitrary keys. It could write
`license.*` without the dedicated signature verification and could write
`sensitive_words` without the dedicated filter reload path. The settings list
also returned raw stored values, including the offline-license document,
configured webhook URLs and the sensitive-word list.

Settings and sensitive-word writes had no durable audit row. The dedicated
license write committed the business value before a best-effort audit, so an
audit failure could leave an unaudited control change. Sensitive-word add and
delete were unlocked read-modify-write operations: concurrent updates could be
lost, and a database or JSON error could be interpreted as an empty policy.

The filter was initially empty while the first database load ran
asynchronously. Later reloads had no policy version, so a delayed old snapshot
could overwrite a newer handler update. `WordCount` also read shared state
without the filter's read lock.

These are repository-source findings on the stated base. No real webhook,
provider, target deployment or target exploit was invoked; target reachability
is not inferred from the source path.

## Repository Change

- one production registrar now puts all eight settings, license and
  sensitive-word routes behind the platform-administrator guard; every handler
  repeats the platform-authority check and fails closed before parsing a tenant
  request;
- generic settings permanently reserve `sensitive_words` and the full
  `license.*` namespace for their dedicated endpoints. Tenant callers receive
  `403` before the platform-only conflict response is considered;
- the generic settings list redacts dedicated values plus the Feishu,
  DingTalk and budget-alert webhook values. License status remains available
  from its dedicated endpoint without returning the signature;
- generic settings, offline-license imports and sensitive-word mutations now
  take a PostgreSQL transaction-scoped advisory lock, read the prior state,
  write the business state and insert the audit row in one transaction. Audit
  or commit failure returns an error before local post-commit effects;
- audits record hashes, byte counts, word counts and the policy version instead
  of raw license signatures, webhook URLs or words. Valid identifiers longer
  than the audit schema's 64-character resource field use a SHA-256 resource ID
  while retaining the non-secret setting key or deployment identifier in
  detail;
- audit IP normalization stores a canonical host-only IPv4/IPv6 value, removes
  IPv6 zones and avoids exceeding the existing 45-character column;
- sensitive-word add/delete operations serialize across processes, normalize
  and deterministically sort the policy, reject missing or `null` `words`, and
  never treat a storage or JSON failure as an empty policy;
- each sensitive-word write returns a database-backed `updated_at` that is
  strictly newer than the prior value. The DFA accepts only a strictly newer
  version under its write lock, preventing a delayed handler or polling
  snapshot from rolling back a newer policy;
- startup synchronously loads and validates the deployment policy before the
  relay or HTTP server starts. A missing row, invalid JSON or zero version is a
  startup error instead of an empty-policy fail-open window;
- the existing one-minute reload loop remains the convergence mechanism for
  other running instances and preserves the last-known-good policy on read or
  decode failure;
- backend HTML docs, frontend API docs, OpenAPI RBAC/request schemas and route
  parity tests now describe the same platform-global boundary. This batch does
  not add a global-settings management screen.

No schema migration, target deployment, credential, provider request, external
message, exporter, retained-data scope or X-Insight contract changed.

## Migrated Signed-JWT and PostgreSQL Evidence

`TestP6F35GlobalControlAuthorityIntegration` creates an empty PostgreSQL
database, applies all migrations and invokes the control routes through signed
JWT authentication and the production role middleware. It proves:

1. organization, billing, project, developer, viewer and unknown roles receive
   `403` for all reads and writes before invalid request bodies are parsed;
2. denied calls leave settings, license, sensitive words, audit rows,
   notification events and the live filter snapshot unchanged;
3. platform reads redact reserved and secret generic-setting values while the
   dedicated license response omits the signature;
4. the generic endpoint cannot bypass the dedicated license or sensitive-word
   validation and reload paths;
5. successful platform mutations produce actor/organization/request-correlated
   audit rows without raw control material;
6. valid 100-character setting keys and long license deployment IDs remain
   writable and auditable despite the existing 64-character resource-ID field;
7. a PostgreSQL trigger that forces audit insertion failure rolls back the
   setting, license and sensitive-word mutations and leaves the live filter
   unchanged;
8. two independent handlers and filters sharing PostgreSQL can add words
   concurrently without losing either value. The two audit rows form a
   before/after hash chain with strictly increasing database policy versions,
   and controlled reload converges both filters;
9. legacy `admin` normalization retains platform compatibility, while missing
   and invalid bearer tokens return `401`.

Unit tests additionally prove all three commit-error paths return `500`, the
sensitive-word filter is not changed before a successful commit, and a license
warning event is not emitted after a commit error. Filter tests prove copied
input storage, locked `WordCount`, and rejection of stale and equal policy
versions. Startup/reload tests prove strict initial load and last-known-good,
strictly-newer reload behavior.

## Local Verification

The final local diff passed:

```text
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
npm --prefix frontend test -- --reporter=dot
npm --prefix frontend run type-check
npm --prefix frontend run build
ruby -e 'require "yaml"; YAML.load_file("docs/tech-ref/05-openapi-spec.yaml"); puts "openapi yaml: ok"'
git diff --check
```

Results: every backend package, the race detector and vet pass; the frontend
suite passes 27 files / 135 tests; type-check and production build pass. The
build retains the existing large-chunk warning, and the macOS race link step
retains the existing non-failing `LC_DYSYMTAB` warning. Independent security
and concurrency review found no remaining blocker, high, medium or functional
low finding after fixes for startup fail-open, stale reload, missing/null
policy requests, long audit identifiers and IPv6 audit addresses.

PR #147 final head `fb77083d0f04f5cdaa20f38e763c93b51b89f4d1`
passed required CI in run `29139408226`: Backend Tests passed in 1m9s,
Frontend Build passed in 3m14s with 27 files / 135 tests, and Docker Build
passed in 1m4s. The Backend log explicitly shows the P6-F35 migrated-database,
production-route and sensitive-word reload tests PASS.

PR #147 was squash-merged as `c08db96167837ade685708c2ab204861a8ecc0ca`.
Post-merge main run `29139534691` passed Backend Tests in 1m38s, Frontend Build
in 3m10s and Docker Build in 1m1s. The repository's automatic branch deletion
was reversed and the historical branch was restored at the exact final head.
Neither CI nor merge promotes target or production status.

## Explicit Remaining Boundaries

- Existing old binaries do not know the new route authority, audit transaction
  or advisory lock. A mixed-version target rollout is unsafe. Under accepted
  D6-09, target execution remains after PRD6 repository closeout; when
  authorized, the control plane must drain/remove all old replicas before only
  new binaries expose these endpoints.
- Other instances converge through the existing approximately one-minute
  database poll rather than an immediate push/ack protocol. A durable
  guardrail-policy lifecycle is later accepted P6-F10 work, not silently added
  to P6-F35.
- A transport failure while committing can leave the caller unable to know
  whether PostgreSQL committed. Durable operation IDs, idempotent write/readback
  and end-to-end request correlation remain accepted P6-F17 work.
- Production admin authentication still does not create and propagate an
  authoritative `request_id`; tests inject one to verify storage. P6-F35 does
  not claim the broader P6-F17 correlation contract is complete.
- The settings surface is still an untyped key/value registry. The confirmed
  budget-alert key mismatch, alert-audit/schema mismatch and first-wallet
  aggregation semantics are configuration-truth inputs for accepted P6-F05 and
  P6-F17, not a reason to broaden this authority batch.
- This batch adds no global-settings UI and makes no claim about target or
  production behavior.

Follow-up status: P6-F05 now locally reconciles those configuration-truth
inputs with `effective-config.v1`, retires the ambiguous global keys and removes
the first-wallet runtime path. Required PR and post-merge CI remain pending;
this follow-up does not retroactively broaden or change P6-F35 evidence.
