# V1 Commercial License And Signed Upgrade Local Evidence

> Date: 2026-07-15
> Baseline: clean `origin/main` at
> `e94a4b184a6f675d76c292602b7c18293ab2ce61`
> Branch: `codex/v1-commercial-license-upgrade`
> Status: Local verified; pull request and required CI pending
> Boundary: Post-PRD6 V1 release gate. PRD6 remains Accepted Scope Complete.

## 1. Decision And Scope

The User requires commercial License enforcement and a signed offline upgrade
bundle before the first customer delivery. This batch implements those two
release gates and a source-free customer Compose profile. It does not create a
production signing key, issue a persistent customer License, activate the
development/test ECS, claim Production evidence, or accept the PRD7 Update
Center/productized automatic-upgrade scope.

## 2. Complete Issue Matrix

| ID | Classification | Impact | Confirmed cause | Priority | Owner | Resolution | Acceptance evidence |
|----|----------------|--------|-----------------|----------|-------|------------|---------------------|
| V1-LICENSE-001 | Confirmed release blocker; not a PRD6 regression | A source-built or copied V1 runtime could admit new model traffic without current signed commercial authority, and the historical License page could be mistaken for enforcement | PRD2 implemented a payload-signature/readback foundation but did not put License validity, deployment binding, features or active-resource limits on admission | P0 / Critical | UniRouter | `unirouter.license.v1`, named Ed25519 trust ring, deployment/time/feature validation on every admission, recovery-only surface, atomic import/audit, startup/refresh projection reconciliation, concurrency-safe DB limits and directly consumable vendor key/issue/verify tooling | Missing/tampered/wrong-key/wrong-deployment/not-yet-valid/expired tests; middleware proves downstream denial and route parity proves the real channel-test path is gated; restored-projection and 16-way migrated-PostgreSQL limit tests; full Go/race/frontend checks |
| V1-UPGRADE-001 | Confirmed release blocker; not a PRD6 regression | An unsigned, tampered, wrong-platform, expired or rollback package could otherwise be presented as an official patch | Existing runtime build metadata and operator runbook did not constitute a signed artifact contract or rollback-resistant installed-release state | P0 / Critical | UniRouter | Deterministic signed archive, bounded canonical manifest, strict Ed25519 trust ring, streaming hash/size/path verification before extraction, fixed provenance binding, OS/arch/sequence/schema preflight and post-health exact runtime record | Determinism and tamper/wrong-key/product/platform/expiry/traversal/symlink/duplicate/undeclared/missing/overflow/rollback/schema tests; migrated-PostgreSQL record/replay tests |
| V1-DELIVERY-001 | Confirmed release blocker; not a new feature | A customer package containing source/build context or mutable dependency tags weakens the commercial and reproducibility boundary | Development Compose builds locally and historical examples allowed tag-based dependency selection | P1 / High | UniRouter | Separate `deploy/customer` Compose with no build/source mount/host migration, mandatory prebuilt application and dependency digest references, external signed config/secrets and local-only frontend bind | Static Compose/YAML assertions locally; real image build/config assertion is routed to required Docker CI |
| V1-TARGET-LICENSE-001 | Target-environment configuration | The currently deployed test ECS will fail closed after this runtime is deployed until an approved trust root, deployment identity and non-production License are installed | Target trust roots, License, migration `000037` and release state intentionally do not exist yet | P0 activation gate / R4 | User decision + target operator | After merge, retain old images and DB backup; configure separately approved public roots/deployment ID; apply `000037`; prove unlicensed denial and licensed success without a paid provider call; record only the healthy exact release | Pending explicit User decision; no target mutation in this batch |
| V1-XI-001 | Misunderstanding / no modification | None for this release gate | License admission and upgrade verification are local UniRouter authority and do not require X-Insight | No action | UniRouter | Keep X-Insight v2 ledger consumer acceptance as its existing separate coordination item | Source/contract boundary review; no cross-project request or data transfer |

## 3. Implemented Commercial License Contract

- The signed envelope covers `schema_version`, `key_id` and the complete
  payload; unknown fields and unbounded/control-character identities fail.
- A configured public-key ring is mandatory and bounded. Private keys are not
  loaded by the runtime or written by tests into the repository.
- New model and Agent admission requires a valid time window, exact deployment
  identity and signed feature. The real channel-test upstream call uses the
  same gateway feature gate; routing preview stays available because it makes
  no provider request and exposes no credential.
- Local login, License read/import, health and diagnostics remain available for
  recovery. Legacy PRD2 evidence can be read only when explicitly configured
  and is never an enforceable commercial License.
- Active API-key, user, project and channel limits are projected into a
  singleton entitlement row and enforced by migration `000037` triggers under
  transaction/advisory locking. Disabling releases capacity; expiry blocks a
  later active insert; down migration refuses while entitlement/history exists.
- Startup reconstructs the derived row from a verified restored document under
  the same locks. Periodic refresh requires its identity, expiry and signed or
  absent limits to match; a mismatch clears runtime authority instead of
  interpreting the missing projection as unlimited.

## 4. Implemented Signed Upgrade Contract

- `uniroute-upgrade pack` creates a deterministic gzip/tar archive containing
  `manifest.json`, `manifest.sig` and only declared regular payload files.
- Verification performs bounded/canonical parsing, verifies the Ed25519
  signature, then trusts product/platform/time/sequence/schema fields. It
  streams every payload digest and size without extracting or executing it.
- The optional provenance digest is accepted only when it exactly binds the
  declared `payload/provenance/build-provenance.json` artifact. This is
  inspired by TUF target-metadata and rollback/freeze principles and SLSA
  provenance separation; V1 does not claim full TUF or SLSA conformance.
- `uniroute upgrade verify` reads the actual database release state/schema.
  `record-applied` succeeds only after the running binary's immutable
  version/commit and current schema exactly match the signed manifest, and it
  rejects replay/rollback under a serializable transaction and advisory lock.
- V1 deliberately has no Admin upload/execute endpoint and no automatic apply.
  Backup, drain, image import, migration, health readback and rollback remain
  explicit operator steps in the runbook.

References:

- https://theupdateframework.github.io/specification/draft/
- https://slsa.dev/spec/v1.2/provenance

## 5. Local Verification

The following checks passed on the branch:

```text
go test ./internal/admin ./internal/licensing ./internal/signing \
  ./internal/upgradebundle ./cmd/uniroute ./cmd/uniroute-license \
  ./cmd/uniroute-upgrade -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race \
  ./internal/migrate ./cmd/uniroute \
  -run 'Test(V1CommercialReleaseControlMigrationAndConcurrentResourceLimit|UpgradeRecordAppliedPinsRuntimeDigestSequenceAndSchema)'
REQUIRE_TEST_DATABASE=1 go test -tags=integration -race ./internal/admin \
  -run TestReconcileStoredCommercialLicenseRestoresDerivedProjectionIntegration
npm test -- --run                 # frontend: 31 files / 164 tests
npm run type-check
npm run build
```

The targeted migrated-PostgreSQL gates proved restored-projection
reconstruction and mismatch fail-closed, exactly one winner with 16 concurrent
active-user inserts at capacity, capacity release/re-enable denial, expiry
denial, guarded down refusal, exact release record and replay rejection. Their
generated temporary database count returned to zero. CI/OpenAPI/customer
Compose YAML parsed locally, and `git diff --check` passed.

This Mac has no Docker executable, so local evidence does not claim a customer
image build or Compose runtime. Required Docker CI must validate the exact
prebuilt image references and no-build customer profile before merge.

## 6. Remaining Gates And Non-Claims

1. Open a pull request and pass Change Routing, Backend Tests, Frontend Build
   and Docker Build on the exact head.
2. Merge only after review/CI approval.
3. Treat test-ECS activation as a separate R4 decision: backup, trust roots,
   deployment identity, non-production License, migration `000037`, exact
   image cutover, denial/success proof and release-state readback.
4. Do not create or store a production/customer private signing key in this
   repository, CI, ECS or evidence.
5. Do not claim Production, real-provider/detector, capacity/HA, TLS, customer
   usability, N-1 recovery, automatic apply/rollback, Update Center, full TUF,
   full SLSA or X-Insight acceptance from this local evidence.
