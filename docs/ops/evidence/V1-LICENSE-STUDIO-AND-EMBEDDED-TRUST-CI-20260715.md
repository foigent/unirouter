# V1 License Studio And Embedded Release Trust — CI Evidence

**Date:** 2026-07-15 (Asia/Shanghai)

**Scope:** Post-PRD6 V1 commercial-release hardening; PRD6 remains Completed
(Accepted Scope) and is not reopened

**PR:** #205

**Code-bearing head:** `cd05055afe25cedff77830230d2788d8f06c96b3`

**Workflow run:** `29359673125`

**Evidence level:** Repository and exact-code-head CI verified

## Outcome

PR #205's exact code-bearing head passed all four routed required checks:

| Check | Result | Material coverage |
|-------|--------|-------------------|
| Change Routing | PASS | Classified the code, Docker, packaging and documentation changes for the full required path |
| Backend Tests | PASS | Vet, full race-enabled Go suite, admin integration gates, V1 pricing/micro-ledger, commercial License/signed-upgrade and all later migration/runtime contract gates |
| Frontend Build | PASS | Type-check, 31-file/164-test Vitest suite and production build |
| Docker Build | PASS | Built the formal backend image with separate CI-only embedded License/upgrade public rings, verified Docker contexts and verified the resulting image |

The dedicated `Run V1 commercial license and signed upgrade integration tests`
step passed. The Docker build also proves that the formal release path can
build with explicit embedded public rings. The CI ring values are public test
material only; no private signing key or customer License entered GitHub
Actions.

## Closure And Remaining Decisions

- `V1-LIC-TRUST-001`, `V1-UPG-TRUST-001` and `V1-LIC-UX-001` are closed at
  repository/CI level on the code-bearing head above.
- PR #205 remains open and merge is not authorized by this evidence.
- No production/customer key, License, deployment identity, migration, image,
  ECS service or database state was created or changed.
- The first production key ceremony, protected release-build public rings,
  Apple Developer ID signing/notarization and any target activation remain
  separate decisions and operational evidence.

This CI result does not claim production, real-customer, notarized-app or ECS
verification, and it does not accept PRD7 Update Center/automatic-upgrade scope.
