# V1 Security Remediation — Local Verification — 2026-07-17

## Evidence level and boundary

- Batch: `V1-SECURITY-REMEDIATION-001`
- Codex Security scan: `124093ab-e945-47e9-8fa3-2eb688dc31dc`
- Repository baseline: `92ba78878782114a82411697597f5e58e10602a8`
- Working branch: `codex/v1-user-task-ia-correctness`
- Evidence level: `Local verified`
- Scope relationship: independent Post-PRD6 amendment over the intentional
  `V1-UX-IA-001` worktree; PRD6 remains `Completed (Accepted Scope)`.

This checkpoint binds the final uncommitted local tree before publication. It
does not claim a commit, pull request, GitHub CI, merge, image build, ECS
migration/deployment, target-browser result, external integration, supplier
bill reconciliation, or Production verification. Those require separate exact
commit and environment evidence.

## Closed findings and adjacent release corrections

The scan-local remediation ledger records all 39 canonical findings as
`fixed_verified`, with exploit/malicious regressions, legitimate controls and
owning-package verification. The principal closure areas are:

- versioned, server-revocable administrator sessions and password-change
  invalidation (`000038`);
- tenant-isolated Guardrail snapshots, bounded quotas, fair scheduling and
  metadata-only advisory-gap evidence (`000039`);
- immutable provider-attempt binding, quarantine of ambiguous history and
  owner/generation stream fencing (`000040` / `000041`);
- workflow-scoped Agent capability supersession and fail-closed distributed
  workload admission;
- atomic authorization/audit mutations, login limiter fail-closed behavior,
  usage/delivery integrity, content-safety parsing, backup/secret transport and
  other source-to-sink fixes listed in the scan ledger.

Two User-approved release-adjacent corrections are not counted as new
canonical scan findings:

1. `000042` makes local login IDs deployment-global, case-insensitive and
   outer-Unicode-whitespace normalized. Local and OIDC identities may share a
   display name in one organization without OIDC shadowing password login;
   OIDC remains issuer/subject bound. Concurrent first OIDC logins converge,
   while different external identities with the same display name retain the
   deterministic suffix behavior. The guarded down path refuses activated
   cross-provider overlaps atomically.
2. The management CLI rejects value-bearing `--password`, positional password
   values and secret-like parser echoes before database access. It accepts only
   a twice-confirmed hidden TTY prompt or bounded one-line
   `--password-stdin`, has no password environment fallback, rejects invalid
   UTF-8 and applies the same 12-code-point / 72-UTF-8-byte policy as the API
   and UI.

The frontend now distinguishes the typed `local_login_id_conflict` from
commercial License/resource-limit `409` responses. Its password form uses the
same Unicode-code-point and UTF-8-byte boundaries as the backend.

## Final local verification

The following final-tree gates passed:

```text
GOCACHE=/private/tmp/unirouter-go-build-final \
  go test -race -coverprofile=/private/tmp/unirouter-v1-security-final-postreview-20260717.out ./...
```

- PASS for every package; no race reported.
- Relevant coverage included `internal/admin` 69.0%, `internal/auth` 88.8%,
  `internal/billing` 66.5%, `internal/gateway` 75.8%,
  `internal/middleware` 84.0% and `internal/migrate` 75.5%.

```text
REQUIRE_TEST_DATABASE=1 GOCACHE=/private/tmp/unirouter-go-build-final \
  go test -tags=integration -race ./...
```

- PASS for every tagged package against local PostgreSQL/Redis.
- This includes migrations through `000042`, administrator session/CLI paths,
  local/OIDC namespace and concurrency, delivery-attempt/stream fencing,
  personal-key authority revocation, tenant isolation and valid controls.

```text
GOCACHE=/private/tmp/unirouter-go-build-final go vet ./...
git diff --check
go mod verify
```

- PASS; Go modules reported `all modules verified`.

```text
cd frontend
npm run type-check
npm test -- --run
npm run build
```

- PASS: 41 test files / 277 tests.
- Production Vite build transformed 4,300 modules successfully.
- Existing React Router/Semi UI test warnings and the existing large-chunk
  advisory remain non-failing; they are not represented as zero-warning proof.

Additional final checks passed:

- YAML parsing for `.github/workflows/ci.yml`, both source Compose files and
  `deploy/customer/docker-compose.yml`;
- shell syntax plus the backup security regression harness;
- 13 SSO credential-origin guard tests;
- Python syntax compilation for the Compose deployment helper;
- focused identity/CLI unit and real-PostgreSQL race suites, including the
  complete Go `unicode.IsSpace` trim set, same-organization local/OIDC overlap,
  concurrent OIDC provisioning, global local-ID conflicts, invalid UTF-8,
  audit rollback and session invalidation;
- final frontend contract review caught and corrected the remaining self-service
  password form drift: user creation and password change now share the same
  Unicode-code-point and UTF-8-byte validators, with distinct policy versus
  current-credential failure messages and 72/73-byte regression coverage.

The first focused run inside the filesystem/network sandbox could not bind an
`httptest` IPv6 loopback port (`operation not permitted`). The identical test
was immediately rerun with local-loopback permission and passed. This is an
environment restriction, not a code failure. macOS emitted known
`LC_DYSYMTAB` linker warnings; all affected commands exited successfully and
reported no race.

An exploratory `go mod tidy -diff` was not used as a gate: this mixed
repository root includes non-Go artifact directories and frontend dependency
content that `tidy` attempted to interpret as import paths, while its default
cache path was also sandbox-denied. It changed no repository file.
`go mod verify`, compilation, vet and the complete test graph passed.

## Publication and target gates

The CI workflow is prepared to:

- run only on pull requests or explicit manual dispatch, avoiding a duplicate
  automatic full run after merge;
- route docs-only changes away from expensive jobs and fail unknown paths safe;
- cancel superseded PR jobs;
- pin Go `1.25.12`, use the versioned shared cache key, and save a missing cache
  only from a trusted manual `main` dispatch;
- run precise tagged security migration, CLI, delivery/stream and personal-key
  authority regressions in addition to the full untagged race suite.

Local Docker/Compose is unavailable, so the Docker/context, formal release
image, embedded trust-root, source-free customer Compose and Nginx runtime
checks remain required GitHub CI gates. After CI and merge, the development ECS
requires one stopped-traffic `000037` to `000042` migration window, preflight,
backup, exact digest readback and authenticated role/browser verification.
Any nonzero migration preflight condition is a stop condition; the migration
must not auto-rename, delete or guess identity/financial records.
