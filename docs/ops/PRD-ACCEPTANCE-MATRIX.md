# Phase 1 PRD Acceptance Matrix

> Purpose: track acceptance against `docs/PRD-Phase1.md`.
> Status values: `Not started`, `Implemented not accepted`, `Unit tested`, `E2E accepted`, `Production verified`, `Out of scope`.
> Last verified: 2026-07-01 local workspace.

## Core Flow

| PRD Item | Status | Current Evidence | Gap To Acceptance |
|----------|--------|------------------|-------------------|
| Change base URL and call DeepSeek/Qwen/GLM/GPT/Claude/Gemini | Implemented not accepted | Gateway/provider routing code exists; tests pass | Current real-provider matrix needed |
| Create API key, call API, return response and `_uniroute.cost` | Implemented not accepted | API key and relay/billing paths exist | Current E2E evidence needed |
| Top up, call, deduct balance, reconcile | Implemented not accepted | Wallet and billing code exists | Current balance-before/after and reconciliation evidence needed |
| Insufficient balance returns 402 | Unit tested | Billing and relay tests pass | Current E2E evidence needed |
| Disabled key returns 401 | Unit tested | Auth/key tests pass | Current E2E evidence needed |
| Streaming request returns fully and settles correctly | Implemented not accepted | SSE flusher path fixed historically; gateway tests pass | Current real streaming evidence needed |
| Interrupted streaming settles by received/estimated tokens | Implemented not accepted | Stream accounting code exists | Controlled disconnect acceptance needed |

## Admin Capability

| PRD Item | Status | Current Evidence | Gap To Acceptance |
|----------|--------|------------------|-------------------|
| Admin can create/edit/disable channels | Implemented not accepted | Admin routes and frontend page exist | Current manual/API CRUD evidence needed |
| Admin can create/disable API keys | Implemented not accepted | Routes and frontend page exist | Current manual/API CRUD evidence needed |
| Admin can view billing and call logs | Implemented not accepted | Logs, wallet, reconciliation pages exist; local current-source smoke verified `/api/v1/settings` and `/api/v1/dashboard/stats` | Current full admin API/browser evidence needed |
| Admin can top up and adjust wallet | Implemented not accepted | Routes and frontend page exist | Current manual/API evidence needed |
| Docker Compose one-command deployment succeeds | Implemented not accepted | Compose files and runbook exist | Current clean-machine or clean-volume deployment evidence needed |

## Frontend UI

| PRD Item | Status | Current Evidence | Gap To Acceptance |
|----------|--------|------------------|-------------------|
| Admin frontend has a coherent navigation and layout shell | Implemented not accepted | `frontend/src/App.tsx` defines grouped sidebar and protected layout; UI source audit passed Canary non-visual review | Current browser/manual UX evidence needed |
| Admin pages use consistent table/form/modal patterns | Implemented not accepted | Pages exist under `frontend/src/pages`; `docs/ui/FRONTEND-UI-ARCHITECTURE-AUDIT.md` documents reuse gaps and PRD2 architecture | Further component refactor can move to PRD2 unless user wants it in PRD1 |
| Core admin workflows are usable from browser | Implemented not accepted | Frontend build and limited page tests pass | Browser/manual validation needed |
| UI has consistent visual hierarchy, spacing, empty/error/loading states | Implemented not accepted | Semi UI is used; source audit completed and non-visual fixes passed Canary | Browser/manual validation still needed |
| Frontend API client matches backend routes | Unit tested | `ApiError` added; `/me/password` backend route added; `route_parity_test.go` guards frontend admin routes | Canary verification needed for latest password/route-parity round |

## Stability

| PRD Item | Status | Current Evidence | Gap To Acceptance |
|----------|--------|------------------|-------------------|
| 100 concurrent requests do not create negative wallet balance | Implemented not accepted | Wallet constraints and billing tests exist | k6/wrk concurrency acceptance needed |
| Circuit breaker switches to backup channel | Implemented not accepted | Circuit breaker and relay tests pass | Current failover simulation needed |
| Sensitive-word request returns 400 | Implemented not accepted | DFA filter and routes exist | Current gateway E2E evidence needed |
| Same `X-Request-Id` does not double charge | Implemented not accepted | Idempotency table and billing paths exist | Current repeated-request E2E evidence needed |
| Admission over concurrency limit returns 429 | Unit tested | Admission middleware tests pass | Current load/concurrency acceptance needed |

## Observability

| PRD Item | Status | Current Evidence | Gap To Acceptance |
|----------|--------|------------------|-------------------|
| Each request log includes request_id/org_id/channel_id | Implemented not accepted | `call_logs` schema and handlers exist | Current E2E log row evidence needed |
| Prometheus `/metrics` returns metrics | E2E accepted | Temporary current-source backend on port 18080 returned Prometheus metric output | Reconfirm in deployed environment |
| Grafana dashboard shows request rate, latency, error rate | Implemented not accepted | Grafana dashboard file exists | Current running Grafana screenshot/manual evidence needed |

## Acceptance Rule

An item should not move to `E2E accepted` unless the evidence includes:

1. Exact command or manual path.
2. Environment used.
3. Expected result.
4. Actual result.
5. Timestamp or commit/worktree reference.
