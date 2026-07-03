# PRD2 Batch 23 Product Local Smoke Evidence

> Date: 2026-07-04 00:44 CST
> Environment: local current-code backend + local Vite frontend + local PostgreSQL/Redis
> Scope: Batch 23 P2 product features: Playground, error docs, internal status, usage analytics, and cache research boundary

## Objective

Verify the Batch 23 product surface against a real backend and browser before Canary:

- Developer Console Playground uses a project API key and approved model alias;
- gateway auth errors and relay errors expose `doc_url`;
- usage analytics drill-down and CSV export work with org/project/provider filters;
- internal status page derives state and incident history from notification events;
- API docs render the error-code registry;
- prompt/response content is not persisted by the Batch 23 product surfaces by default.

## Environment

| Item | Value |
|------|-------|
| Backend | current worktree `go run ./cmd/uniroute` on `http://localhost:18083` |
| Frontend | existing Vite frontend on `http://localhost:3000` |
| Database | local PostgreSQL from `.env.production` / running UniRouter environment |
| Redis | local `localhost:6379` |
| Screenshot | `output/playwright/prd2-batch23-product.png` |

The already-running `http://localhost:8080` backend was checked first, but it returned the old pre-Batch-23 response for `/api/v1/error-docs`. A current-code backend was therefore started on `18083` to avoid mixing stale runtime state with Batch 23 verification.

## Commands

Backend startup:

```bash
set -a
source .env.production
set +a
LISTEN_PORT=18083 go run ./cmd/uniroute
```

Smoke:

```bash
set -a
source .env.production
set +a
UNIROUTE_SMOKE_BACKEND_URL=http://localhost:18083 \
UNIROUTE_SMOKE_FRONTEND_URL=http://localhost:3000 \
scripts/prd2-batch23-product-smoke.sh
```

## Result

```json
{
  "status": "PASS",
  "batch": 23,
  "backend_url": "http://localhost:18083",
  "frontend_url": "http://localhost:3000",
  "org_id": 43,
  "project_id": 35,
  "model_alias": "batch23-company-chat",
  "provider": "batch23-mock",
  "upstream_calls": 1,
  "analytics_summary": {
    "avg_latency_ms": 35,
    "cost_cents": 7,
    "csv_export_scope": "filtered_query",
    "estimated_ratio": 0.2597402597402597,
    "estimated_tokens": 20,
    "failed_requests": 1,
    "prompt_retention": "not_stored_by_default",
    "response_retention": "not_stored_by_default",
    "success_requests": 3,
    "total_requests": 4,
    "total_tokens": 77,
    "upstream_tokens": 57
  },
  "internal_status": "warning",
  "screenshot": "/Users/m4/Desktop/projects/unirouter/output/playwright/prd2-batch23-product.png"
}
```

## Coverage

| Check | Result |
|-------|--------|
| `/api/v1/error-docs` exposes gateway/auth/model/billing/provider error docs | PASS |
| Invalid gateway project key returns `auth_error.invalid_api_key` with `doc_url` | PASS |
| Usage analytics groups by `usage_source` and includes upstream/counter_v2/upstream_failed rows | PASS |
| Usage analytics summary includes estimated/upstream token split and `estimated_ratio` | PASS |
| Usage analytics CSV export returns filtered-query CSV content | PASS |
| Internal status API returns `warning` from the seeded open warning incident | PASS |
| Developer Console Playground creates a project key and calls a mock OpenAI-compatible upstream | PASS |
| Playground renders response content, request metadata, and `17 tokens` | PASS |
| Playground invalid-key path renders a link to error docs | PASS |
| Admin `/usage-analytics`, `/status`, and `/docs` browser routes render against the real backend | PASS |

## Boundary

This is local current-code evidence, not Canary, Sentinel, Atlas, or production verification. Batch 23 rows should remain `Implemented not accepted` until Canary verifies the code/docs/smoke and Sentinel completes the merge path. The smoke uses a local mock upstream and does not replace Batch 18 real-provider acceptance.

The smoke seed is idempotent, while each Playground run creates a real additional call log. Re-running the smoke against the same local fixture can therefore increase `total_requests`, `total_tokens`, `cost_cents`, and lower/raise `estimated_ratio`; the required checks assert the seeded usage-source groups and positive estimated/upstream split rather than a fixed total.
