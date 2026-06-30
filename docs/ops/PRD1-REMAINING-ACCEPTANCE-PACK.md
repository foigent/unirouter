# PRD1 Remaining Acceptance Pack

Status: Codex local closeout evidence and external acceptance queue
Last updated: 2026-07-01

## Purpose

This file separates PRD1 items that Codex verified locally from items that require a capability not available in this workspace, such as Docker, real upstream providers, browser/manual visual validation, or cloud deployment.

## Local Evidence Completed By Codex

Environment:

- Workspace: `/Users/m4/Desktop/projects/UniRouter`
- Local backend source verified with `go run ./cmd/uniroute` on port `18080`
- Existing local services detected: PostgreSQL on `127.0.0.1:5432`, Redis on `127.0.0.1:6379`
- Docker: unavailable in this workspace (`docker: command not found`)

Commands completed:

```bash
go test ./...
cd frontend && npm test -- --run
cd frontend && npm run build
go test ./cmd/uniroute -run TestAdminRoutesRegisteredForFrontendClient -v
```

Results:

- Go tests: PASS
- Frontend tests: PASS, 36 tests
- Frontend build: PASS, with existing Vite large chunk warning
- Route parity guard: PASS
- Temporary current-source backend health: PASS
- Temporary current-source `/metrics`: PASS, returned Prometheus metric output
- Temporary current-source admin login: PASS
- Temporary current-source `/api/v1/me`: PASS
- Temporary current-source `/api/v1/settings`: PASS
- Temporary current-source `/api/v1/dashboard/stats`: PASS
- Temporary current-source `/api/v1/me/password` unauthenticated behavior: PASS, returned 401 through `AdminAuth`

## Code Fixes Completed

### PRD1-ENG-001: `/me/password` contract mismatch

Completed:

- Added backend handler `ChangePassword`
- Registered `PUT /api/v1/me/password`
- Added bcrypt old-password verification
- Added bcrypt new-password hashing
- Added authenticated current-user scoping through `AdminAuth` context
- Added frontend confirm-password equality check
- Added unit tests for success, wrong old password, and unauthenticated requests
- Added API client test for `changePassword`

Files:

- `internal/admin/auth_handlers.go`
- `cmd/uniroute/main.go`
- `internal/admin/admin_mock_test.go`
- `frontend/src/pages/SettingsPage.tsx`
- `frontend/src/api/__tests__/client.test.ts`

### PRD1-ENG-002: frontend/backend route parity

Completed:

- Added `cmd/uniroute/route_parity_test.go`
- Guard covers current frontend admin API routes, including `/api/v1/me/password`

### DOC-005: OpenAPI Admin spec drift

Completed:

- Identified that the previous `docs/tech-ref/05-openapi-spec.yaml` contained older `/api/v1/admin/*` routes
- Rewrote `docs/tech-ref/05-openapi-spec.yaml` as a current PRD1 API path snapshot
- Verified the YAML parses and contains no `/api/v1/admin/*` paths
- Updated `CODEBASE-MAP.md`, `DOC-INVENTORY.md`, and `docs/tech-ref/README.md`

Remaining:

- Expand schema detail before using generated clients in PRD2

## External Acceptance Queue

These items are not honestly closable from this workspace.

| ID | Required Capability | Owner Stage | Blocking Reason | Evidence Needed |
|----|---------------------|-------------|-----------------|-----------------|
| EXT-001 | Docker Compose clean deployment | Sentinel/Atlas | `docker` is unavailable locally | `docker compose up --build` health output, backend/frontend/PG/Redis/Prometheus/Grafana status |
| EXT-002 | Real upstream provider E2E | Sentinel/Atlas or configured test environment | Requires valid provider keys and configured channels | Non-stream and stream `/v1/chat/completions` responses with `_uniroute`, call log rows, wallet before/after |
| EXT-003 | Interrupted streaming settlement | Capable runtime tester | Requires controlled client disconnect against real stream | Call log with estimated usage and wallet settlement evidence |
| EXT-004 | 100 concurrent billing requests | Capable runtime tester | Requires load tool and controlled test wallet | No negative balance, idempotency/freeze rows, latency/error summary |
| EXT-005 | Failover and circuit breaker | Capable runtime tester | Requires controlled upstream failures | Primary channel fail counts, backup channel selected, circuit state/cooldown evidence |
| EXT-006 | Browser/manual frontend validation | Human or browser-capable agent | Canary/OpenClaw has no visual capability | Walkthrough notes for Dashboard, Channels, Keys, Wallet, Logs, Settings |
| EXT-007 | Grafana dashboard visual verification | Atlas or browser-capable operator | Requires deployed Grafana | Dashboard import/provisioning evidence and panel screenshot/manual notes |
| EXT-008 | Full typed OpenAPI schemas | Codex/Sentinel before typed-client work | Current spec is path-accurate but intentionally uses generic schemas for many admin resources | Detailed schemas matching handler payloads |

## Recommended Handoff

1. Canary should verify the code/docs in this pack without modifying files.
2. If Canary passes, Sentinel can include local test/build evidence in the PR.
3. Sentinel should run CI and, if available, Docker Compose validation.
4. Atlas should run cloud deployment and complete EXT-001, EXT-002, EXT-007.
5. Browser/manual UI validation remains separate from Canary.
