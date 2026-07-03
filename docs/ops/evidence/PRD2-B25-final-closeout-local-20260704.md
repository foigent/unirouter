# PRD2 Batch 25 Final Closeout Local Evidence

Date: 2026-07-04
Executor: Codex main agent
Environment: local macOS workspace `/Users/m4/Desktop/projects/UniRouter`

## Scope

Batch 25 is a documentation and status closeout round. It does not change application code.

Verified changes:

- P2-F11 prompt/response storage policy exists and is indexed.
- PRD2 acceptance matrix has no `Planned`, `Implemented not accepted`, or `Unit tested` rows.
- External boundaries remain explicit:
  - Batch 18 real upstream provider evidence needs OpenRouter and direct Anthropic credentials.
  - P2-F10 License is E2E accepted, not Production verified.
  - Project budget hard-block enforcement is Deferred.
  - Production HA/DR remains later target evidence.
  - Real enterprise IdP browser login remains target-environment evidence.

## Commands

```bash
test -f docs/tech-ref/19-prompt-response-storage-policy.md
rg -n "disabled by default|audit-on-read|explicit future approval|metadata-only" docs/tech-ref/19-prompt-response-storage-policy.md
rg -n "19-prompt-response-storage-policy|Prompt And Response Storage Policy" docs/tech-ref/README.md docs/DOC-INVENTORY.md
rg -n '\| (Planned|Implemented not accepted|Unit tested) \|' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
git diff --check
go test ./...
npm --prefix frontend test -- --run
npm --prefix frontend run build
```

Results:

- P2-F11 policy check: PASS.
- Index check: PASS.
- Matrix non-final status grep: PASS by returning no matches.
- `git diff --check`: PASS.
- `go test ./...`: PASS.
- `npm --prefix frontend test -- --run`: PASS, 15 files / 85 tests.
- `npm --prefix frontend run build`: PASS with existing Vite large chunk warning.

## Browser Smoke

The already-running backend on `localhost:8080` was not the current Batch 24/25 route set: `/api/v1/error-docs` returned a health-style response, so it was not used as acceptance evidence.

Codex started current source on a temporary local port:

```bash
set -a; source .env.production; set +a; LISTEN_PORT=18084 go run ./cmd/uniroute
```

Readiness:

```json
{"status":"ok","db":"ok","redis":"ok"}
```

Browser smoke command:

```bash
set -a; source .env.production; set +a; \
UNIROUTE_SMOKE_BACKEND_URL=http://localhost:18084 \
UNIROUTE_SMOKE_FRONTEND_URL=http://localhost:3000 \
UNIROUTE_SMOKE_SKIP_BROWSER_INSTALL=true \
scripts/prd2-batch23-product-smoke.sh
```

Result:

```json
{
  "status": "PASS",
  "batch": 23,
  "backend_url": "http://localhost:18084",
  "frontend_url": "http://localhost:3000",
  "project_id": 35,
  "model_alias": "batch23-company-chat",
  "provider": "batch23-mock",
  "upstream_calls": 1,
  "internal_status": "warning",
  "prompt_retention": "not_stored_by_default",
  "response_retention": "not_stored_by_default",
  "screenshot": "/Users/m4/Desktop/projects/unirouter/output/playwright/prd2-batch23-product.png"
}
```

This is local current-code browser smoke evidence, not production or customer-environment evidence.
