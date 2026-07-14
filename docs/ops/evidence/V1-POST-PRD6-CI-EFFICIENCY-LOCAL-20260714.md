# V1 Post-PRD6 CI Efficiency Local Evidence — 2026-07-14

## Classification

- Issue: `V1-CI-001`
- Category: confirmed engineering/operational regression
- Severity: High
- Evidence level: Local verified; required CI pending
- Baseline: `origin/main` at `f90e7eb79ae23d48c99b882e2d7a3bb568b46c5a`
- Branch: `codex/v1-ci-efficiency`
- PRD6 boundary: unchanged; PRD6 remains Completed (Accepted Scope)

## Verified pre-change facts

1. `.github/workflows/ci.yml` ran the same Backend Tests, Frontend Build and
   Docker Build jobs for both a pull request and the resulting push to `main`.
2. `main` branch protection is strict and requires those exact three GitHub
   Actions checks. A pull request therefore has to be tested against the latest
   base before it can merge.
3. Recent history contains paired full runs, including PR #194 run
   `29276000599` followed by `main` run `29276900239`, even though that change
   was documentation-only.
4. The repository held 35 caches totaling `10,450,365,643` bytes. Thirty-four
   `setup-go` caches totaled `10,378,971,894` bytes; the remaining npm cache was
   `71,393,749` bytes.
5. Every Go cache used the same
   `setup-go-Linux-x64-ubuntu24-go-1.25.12-8948...` key, but PR-created entries
   were scoped to distinct `refs/pull/<number>/merge` refs. They cannot be
   restored by `main` or sibling pull requests.
6. `go.mod` declares `toolchain go1.25.12`, while the workflow requested the
   floating minor version `1.25` and enabled setup-go's combined module/build
   cache.
7. Frontend CI ran `npm run type-check` and then `npm run build`; the latter
   invokes `tsc` again before Vite and therefore duplicated type checking.

## Implemented contract

1. Full required CI runs on `pull_request` only. A manual `workflow_dispatch`
   remains available and deliberately routes to the full suite. The duplicate
   automatic post-merge `main` run is removed.
2. Workflow-level concurrency is keyed by workflow plus PR number/ref and
   cancels superseded runs for the same PR.
3. A repository-owned change router classifies the complete PR diff:
   - documentation and Markdown-only changes request no expensive job;
   - Go, migration, `cmd` or `internal` changes request Backend plus Docker;
   - `frontend` changes request Frontend plus Docker;
   - every unknown or workflow/configuration path fails safe to the complete
     Backend, Frontend and Docker suite.
4. The three branch-protection job names are preserved. They are skipped only
   through job-level conditions after successful routing. If routing fails,
   all three jobs are forced to start and fail; a routing failure cannot turn
   into a green bypass.
5. Go is fixed to `1.25.12`, `GOTOOLCHAIN=local` prevents an implicit toolchain
   download, and setup-go's automatic merge-ref cache is disabled. Pull-request
   jobs use restore-only access to a controlled shared key; only a trusted
   manual dispatch on `main` may save that key after a full successful run.
   Pull requests therefore cannot create another per-merge-ref Go cache.
6. Frontend CI keeps the explicit type-check and runs Vite directly afterward,
   avoiding the second `tsc` pass.
7. Bounded timeouts are set for routing, Backend, Frontend and Docker jobs.

## Explicit cache cleanup

Only two individually identified caches were removed:

1. cache `5670690671`, scoped to merged PR #165;
2. cache `5671017473`, scoped to merged PR #166.

Each cache was deleted with a separate command. No wildcard, key-wide or bulk
deletion was used. Readback after both deletions reported 33 caches totaling
`9,865,023,859` bytes (`9.865 GB`), including 32 Go caches. No current PR,
default-branch npm cache or repository file was deleted.

## Local verification

- `actionlint` v1.7.7 plus Ruby YAML/expected job-key checks: PASS.
- The actual router script classified historical PR #194 as docs-only,
  PR #192 as Frontend plus Docker, PR #196 as the complete suite and a manual
  dispatch as the complete suite: PASS.
- Invalid base/head SHA handling failed closed before producing route outputs:
  PASS.
- `npm exec -- vite build` from `frontend/`: PASS, with the existing large
  chunk warning.
- `git diff --check`: PASS.

Required GitHub CI must still validate GitHub expression semantics, full test
execution and Docker build on this branch before merge.

## Rollback and non-goals

- Rollback is a reviewed revert of the workflow change. No branch-protection
  context was renamed or removed.
- This batch does not weaken Backend, Frontend or Docker required-check
  semantics, alter application behavior, deploy a target, change credentials,
  mutate a database or reopen PRD6.
- Before the first trusted `main` cache seed, capacity must be read back and at
  most one additional explicitly identified closed-PR cache removed if needed.
  No key-wide or bulk cleanup is permitted.
