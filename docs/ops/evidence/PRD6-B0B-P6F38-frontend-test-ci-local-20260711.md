# PRD6 Batch 0B P6-F38 Frontend Tests In Required CI — Local Evidence

> Date: 2026-07-11
> Base: `7173c5967a42c112fd0b219938a0901c1a0b9633`
> Evidence level: CI verified by PR #143

## Confirmed Gap

The repository already had 25 Vitest files and 126 tests, but the required
`Frontend Build` job ran dependency install, type-check and production build
only. A PR could therefore merge after breaking an existing rendered behavior
test.

The first exact-diff validation also exposed why the omission matters: running
the frontend suite concurrently with the backend race suite caused several
Semi UI interaction tests to exceed Vitest's default 5-second per-test timeout.
A later frontend-only full run narrowed the remaining failures to the same
timeout boundary, while the Data Exchange test passed in 1.6 seconds when run
alone. No frontend product source had changed.

This is recorded as a test-infrastructure defect, not hidden as a product
failure and not dismissed without rerun evidence.

## Change

- `.github/workflows/ci.yml` now runs `npm test -- --run` between frontend
  type-check and build, so the existing suite becomes part of the required
  `Frontend Build` job.
- `frontend/vite.config.ts` sets a bounded 10-second interaction timeout. Test
  assertions, file selection and failure behavior are unchanged; this only
  accommodates the measured cost of parallel jsdom/Semi UI interaction tests.

## Acceptance

Local verification now proves:

1. `npm --prefix frontend run type-check` passes;
2. all 25 files / 126 tests pass with no skipped file;
3. `npm --prefix frontend run build` passes;
4. backend race/vet and `git diff --check` remain green.

Final exact-diff commands:

```text
go test -race ./... -count=1
go vet ./...
npm --prefix frontend run type-check
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Result: backend race/vet PASS; frontend 25 files / 126 tests PASS; type-check
and production build PASS. Existing React/Semi warnings, the Go macOS linker
warning and the large frontend chunk warning remain non-failing. The pre-change
5-second failures and isolated reruns are retained above rather than rewritten
as a first-pass green result.

PR #143 run `29133309062` passed Backend Tests, Frontend Build and Docker Build
for implementation head `132b019`. The required Frontend Build log explicitly
shows the `Frontend tests` step and reports `25 passed (25)` / `126 passed
(126)` before the production build. Independent review confirmed that the
10-second value changes only the per-test waiting bound and does not skip tests,
weaken assertions or convert failures to success.

No target deployment, browser credential, external request or data-scope
change is needed for this engineering gate.
