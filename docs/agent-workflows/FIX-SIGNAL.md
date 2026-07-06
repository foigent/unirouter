# FIX-SIGNAL.md - PRD3 Batch 12 Dependency Vulnerability Maintenance

**Date:** 2026-07-06
**Status:** Fixed awaiting Canary

PRD3 Batch 11 status reconciliation and gap plan passed Canary 6/6 in
`docs/agent-workflows/VERIFY-RESULT.md`; Sentinel merged PR #75 at `132ca79`;
User reported Canary and Sentinel completed.

Codex has completed PRD3 Batch 12 as a narrow SEC-004 dependency vulnerability
maintenance batch. This updates vulnerable Go and frontend dependencies, clears
current reachable vulnerability findings, and records the Go 1.25 toolchain
boundary required by the fixed `pgx` version.

## Boundaries

- PRD3 remains cleanup/optimization of existing functionality only.
- This batch changes dependency manifests/locks and docs/status/evidence only.
- No product capability, API route, backend handler, frontend behavior, RBAC
  rule, database migration, deployment config, or archive file changed.
- No files were deleted.
- This does not implement SEC-005, P3-F09, P3-F10, P3-F11, P3-F14 final
  cleanup, or any PRD4 feature.
- `github.com/jackc/pgx/v5@v5.9.2` requires Go 1.25, so `go.mod` now declares
  `go 1.25.0`; Canary should verify this is explicitly documented rather than
  treated as an accidental drift.

## Verification Request

Please verify:

1. Dependency manifests reflect the intended minimal security maintenance:
   - `go.mod` declares `go 1.25.0`;
   - `github.com/jackc/pgx/v5` is v5.9.2;
   - `github.com/golang-jwt/jwt/v5` is v5.2.2;
   - `github.com/redis/go-redis/v9` is v9.6.3;
   - `frontend/package.json` declares `vite` as `^6.4.3`;
   - `frontend/package-lock.json` resolves Vite 6.4.3 and esbuild 0.25.12.
2. Security scans are clean for the targeted findings:
   - `govulncheck` reports 0 reachable vulnerabilities;
   - `npm audit` reports 0 vulnerabilities.
3. Regression commands pass:
   - `go test ./...`;
   - `npm --prefix frontend test -- --run`;
   - `npm --prefix frontend run build`.
4. Status docs are self-consistent:
   - `docs/ops/CURRENT-STATUS.md` shows Batch 12 as current `Fixed awaiting Canary`;
   - `docs/ops/PRD3-ACCEPTANCE-MATRIX.md` marks Batch 11 accepted and SEC-004 fixed awaiting Canary;
   - `docs/issues/OPEN-ISSUES.md` marks `SEC-004` as `Fixed awaiting Canary` and `PRD3-EVIDENCE-001` as `Canary passed`.
5. Evidence exists at
   `docs/ops/evidence/PRD3-B12-dependency-vulnerability-maintenance-local-20260706.md`
   and states the Go 1.25 boundary, validation commands, and non-feature
   boundaries.
6. Run these commands:

```bash
rg -n "github.com/jackc/pgx/v5 v5.9.2|github.com/golang-jwt/jwt/v5 v5.2.2|github.com/redis/go-redis/v9 v9.6.3|go 1.25.0" go.mod
rg -n '"vite": "\\^6.4.3"|node_modules/vite"|node_modules/esbuild"' frontend/package.json frontend/package-lock.json
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
npm --prefix frontend audit --audit-level=moderate
go test ./...
npm --prefix frontend test -- --run
npm --prefix frontend run build
rg -n "PRD3 Batch 12 Dependency Vulnerability|SEC-004 \\| Critical \\| Fixed awaiting Canary|PRD3-EVIDENCE-001 \\| Medium \\| Canary passed|go 1.25" docs/ops/CURRENT-STATUS.md docs/ops/PRD3-ACCEPTANCE-MATRIX.md docs/issues/OPEN-ISSUES.md docs/ops/evidence/PRD3-B12-dependency-vulnerability-maintenance-local-20260706.md
git diff --check
```

## Expected Result

- Dependency/version greps return matches for the upgraded versions.
- `govulncheck` reports 0 reachable vulnerabilities.
- `npm audit` reports 0 vulnerabilities.
- Go and frontend test/build commands pass.
- Status/evidence grep returns matches in the expected docs.
- `git diff --check` passes.
- If all checks pass, update `docs/agent-workflows/VERIFY-RESULT.md` with PRD3
  Batch 12 PASS.
- If any check fails, list the exact file and line-level issue.

## Verification Pointers

- `go.mod`
- `go.sum`
- `frontend/package.json`
- `frontend/package-lock.json`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD3-ACCEPTANCE-MATRIX.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/ops/evidence/PRD3-B12-dependency-vulnerability-maintenance-local-20260706.md`
- `docs/agent-workflows/FIX-SIGNAL.md`
