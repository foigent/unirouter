# UniRouter — Context Checkpoint Handoff

## Project Overview
**UniRouter** is an AI API gateway/proxy that routes OpenAI-compatible API requests to multiple upstream channels (providers), handles billing/freezing/settling, and provides an admin dashboard + API management UI.

**Stack:** Go backend (net/http, pgx, redis), React/Vite/TypeScript frontend, PostgreSQL, Redis, Docker Compose.

---

## Work Completed This Session

### Backend — New API Endpoints (`internal/admin/handlers.go`)
- `PUT /api/v1/channels/{id}` — dynamic partial update with auto-generated SET clauses
- `DELETE /api/v1/channels/{id}` — delete by ID
- `PUT /api/v1/keys/{id}` — dynamic partial update for API keys
- `GET /api/v1/wallet/transactions` — paginated transaction list (`page`, `page_size` query params)
- `GET /api/v1/logs` — paginated call logs (replaced old `ListCallLogs`)
- `GET /api/v1/dashboard/stats` — now returns `recent_logs` (last 10) + `model_prices` in addition to aggregate stats
- `GET /api/v1/channel-keys` — list channel_keys with channel name join
- `POST /api/v1/channel-keys` — create with AES-256-GCM encrypted key storage
- `internal/billing/alert.go` — new `AlertService` with `CheckAndNotify()` that POSTs webhook on low balance, rate-limited to once/hour via audit_logs table
- `GET /api/v1/reconciliation` — token reconciliation with pagination and filtering (NEW)

### Frontend — Pages Fixed (`frontend/src/`)
- `DashboardPage` — now calls `getDashboardStats()`, renders `recent_logs` table + `model_prices` table
- `ChannelsPage` — full CRUD: table view, edit modal (name/provider/base_url/models/priority), status toggle (active/disabled/degraded), delete confirmation
- `KeysPage` — edit modal for name/allowed_ips/allowed_models/status/rpm_limit/qps_limit
- `WalletPage` — uses `getWalletTransactions()` with pagination
- `ReconciliationPage` — token reconciliation with model/status filtering (NEW)
- `TestPage` — channel testing with single/batch test and result history (NEW)

### Deployment (`docker-compose.yml`, `frontend/`)
- `frontend/nginx.conf` — SPA fallback (`try_files /index.html`), proxy `/api/` `/v1/` `/health/` to `backend:8080`, static asset caching (30d)
- `frontend/Dockerfile` — multi-stage: `node:20-alpine` build → `nginx:1.27-alpine` serve
- `docker-compose.yml` — services renamed: `uniroute` → `backend`, added `frontend` on port 3000:80

### Route Registration (`cmd/uniroute/main.go`)
- All new endpoints registered under `adminAuth` middleware
- Uses Go 1.22+ `mux.Handle("METHOD /path", ...)` pattern

---

## Key Decisions & Constraints

1. **No browser/screenshots for UI verification** — user explicitly forbids `view_image`, playwright, screenshots. Only source code reading for UI validation.
2. **Code-first delivery** — user prefers minimal diffs, surgical changes, no unnecessary rewrites.
3. **Single-org simplified model** — wallet/transactions queries assume single org (first wallet), acceptable for current scope.
4. **Channel key encryption** — uses AES-256-GCM encryption via `auth.EncryptKey()`/`auth.DecryptKey()`. The `encKey` comes from `ENCRYPTION_KEY` env var.
5. **Dynamic SQL updates** — `UpdateChannel` and `UpdateAPIKey` use pointer fields (`*string`, `*int`) to detect which fields were sent, building SET clauses dynamically.
6. **Budget alert rate-limiting** — checks `audit_logs` for `budget_alert` action within last hour before sending webhook.
7. **Pagination helper** — `parsePagination(r)` extracts `page` and `page_size` from query params with defaults (page=1, pageSize=20).
8. **Reconciliation** — derives from `call_logs` table, showing estimated vs actual tokens with delta calculation.

---

## Current State

- ✅ `go build ./...` passes clean
- ✅ All 10 test suites pass (`admin`, `auth`, `billing`, `circuitbreaker`, `filter`, `gateway`, `middleware`, `models`, `tokenutil`)
- ✅ Frontend TypeScript compiles (Vite build)
- ✅ All 14 frontend pages implemented
- ✅ All 17 admin API endpoints implemented
- ✅ Budget alerts wired up in main.go
- ✅ Docker Compose configuration complete

---

## What Remains (Next Steps)

1. **Add `estimated_tokens` column to `call_logs`** — for proper reconciliation with pre-flight estimation
2. **Frontend code splitting** — dynamic imports for pages to reduce bundle size
3. **Encryption already uses AES-256-GCM — verify key rotation strategy for production
4. **WebSocket dashboard** — real-time updates for dashboard stats
5. **Export functionality** — CSV/JSON export for logs and transactions

---

## Critical File Paths

| File | Role |
|------|------|
| `cmd/uniroute/main.go` | Entry point, route registration |
| `internal/admin/handlers.go` | All admin API handlers (~500 lines) |
| `internal/billing/alert.go` | Budget alert webhook service |
| `internal/billing/billing.go` | Freeze/settle/cancel state machine |
| `internal/gateway/relay.go` | Core request relay + billing integration |
| `frontend/src/pages/` | All admin dashboard pages |
| `frontend/src/services/api.ts` | API client functions |
| `frontend/nginx.conf` | Production nginx config |
| `frontend/Dockerfile` | Multi-stage frontend build |
| `docker-compose.yml` | Full stack orchestration |
| `migrations/000001_init_schema.up.sql` | Database schema |

---

## User Preferences (Critical)

- **Code-only delivery** — when user says "只输出代码", provide code without explanation
- **Surgical diffs** — "只改必要部分", "不要重写文件"
- **No fabrication** — verify claims from real sources; don't invent data
- **Continuous progress** — don't go quiet between tool calls; report briefly
- **Exhaust tools before asking** — use your own capabilities before asking user for help
- **Thorough understanding first** — for new tasks, understand the full project before implementing
