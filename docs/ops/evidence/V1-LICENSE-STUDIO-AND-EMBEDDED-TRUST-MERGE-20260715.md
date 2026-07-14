# V1 License Studio And Embedded Release Trust — Merge Evidence

**Date:** 2026-07-15 (Asia/Shanghai)

**Scope:** Post-PRD6 V1 commercial-release hardening; PRD6 remains Completed
(Accepted Scope) and is not reopened

**PR:** #205

**Final PR head:** `f1fdee12499aa399e63f205a00040d772425e7df`

**Squash merge:** `5c1313521870731b0164d4eecfbbb06d7a7f0704`

**Merged at:** 2026-07-15 03:41:56 Asia/Shanghai

**Evidence level:** Merged and CI verified; target and production not verified

## Merge Readback

1. User explicitly approved merging PR #205.
2. GitHub reported the PR as `MERGED` with squash commit
   `5c1313521870731b0164d4eecfbbb06d7a7f0704`.
3. Local `main`, `origin/main` and the GitHub merge commit all resolved to that
   exact SHA immediately after merge.
4. Final PR head `f1fdee12499aa399e63f205a00040d772425e7df`
   passed Change Routing, Backend Tests, Frontend Build and Docker Build in run
   `29360683242`; GitHub reported merge state `CLEAN` before merge.
5. A commit-scoped Actions readback returned no automatic `push` run for the
   merge commit. This is the intended CI-efficiency policy: the already-tested
   PR is not followed by a duplicate automatic full main run.

## Preserved Boundaries

- No production/customer private key, public trust ring or License was
  generated.
- No ECS image, deployment identity, License, migration, release state,
  database or service was changed.
- Production key custody, protected release-build public rings, Apple
  Developer ID signing/notarization and target activation remain separate
  decisions and evidence levels.
- This merge does not accept PRD7 Update Center/automatic-upgrade scope and
  does not promote repository/CI evidence to Target or Production verified.
