# PRD4 Real Upstream Evidence Harness

> Scope: PRD4 Batch 1H / `P4-F08`
> Status: Harness for approved target-environment evidence. It does not create
> real upstream evidence unless approved provider credentials and target
> environment access are supplied.

## Purpose

PRD4 requires real upstream proof for both adapter paths:

- OpenRouter-compatible through `openai_compatible`.
- Direct Anthropic Messages API through `anthropic_native`.

Mock upstreams and local unit tests do not satisfy this evidence requirement.
The harness in this document creates a repeatable, guarded path for running
small real-provider gateway requests and collecting sanitized evidence.

## Files

| File | Purpose |
|------|---------|
| `scripts/prd4-real-upstream-smoke.sh` | Shell wrapper and `--preflight` entrypoint |
| `scripts/prd4_real_upstream_seed.go` | Guarded DB fixture seed for org/project/model/channel/channel-key/API-key |
| `scripts/prd4_real_upstream_smoke.mjs` | Gateway runner and sanitized evidence writer |

## Safety Model

The harness has two modes.

Preflight mode is safe by default:

```bash
scripts/prd4-real-upstream-smoke.sh --preflight
```

Preflight returns JSON with `status: SKIP` when credentials or secrets are
missing. It does not mutate the database and does not call any provider.

Real-provider mode requires explicit opt-in:

```bash
UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION=approved-target-real-upstream \
UNIROUTE_SMOKE_BACKEND_URL=http://localhost:8080 \
UNIROUTE_SMOKE_DB_DSN='postgres://...' \
ENCRYPTION_KEY='32-byte-backend-encryption-key' \
API_KEY_HASH_SECRET='backend-api-key-hash-secret' \
OPENROUTER_API_KEY='...' \
OPENROUTER_MODEL='provider/model-slug' \
ANTHROPIC_API_KEY='...' \
ANTHROPIC_MODEL='anthropic-model-slug' \
scripts/prd4-real-upstream-smoke.sh
```

Real-provider mode:

- checks `GET /health/ready` before seeding;
- upserts a dedicated PRD4 evidence org, project, wallet, model registry rows,
  model set, channels, channel keys, and one service API key;
- sends four bounded gateway requests:
  - OpenRouter non-stream;
  - OpenRouter stream;
  - Anthropic-native non-stream;
  - Anthropic-native stream;
- uses `max_tokens` clamped to `1..64` with default `16`;
- verifies call logs and request trace events after each request;
- writes sanitized JSON evidence to `docs/ops/evidence/` by default.

## Required Environment

| Variable | Required | Notes |
|----------|----------|-------|
| `UNIROUTE_PRD4_UPSTREAM_ALLOW_MUTATION` | Yes for real mode | Must equal `approved-target-real-upstream` |
| `UNIROUTE_SMOKE_BACKEND_URL` | Recommended | Defaults to `http://localhost:8080` |
| `UNIROUTE_SMOKE_DB_DSN` or `DB_DSN` | Yes for real mode | Used by the seed step |
| `ENCRYPTION_KEY` or `UNIROUTE_SMOKE_ENCRYPTION_KEY` | Yes for real mode | Must match the running backend encryption key |
| `API_KEY_HASH_SECRET`, `UNIROUTE_SMOKE_API_KEY_HASH_SECRET`, or `JWT_SECRET` | Yes for real mode | Must match backend API key hashing behavior |
| `OPENROUTER_API_KEY` | Yes for real mode | Not written to evidence |
| `OPENROUTER_MODEL` | Yes for real mode | Exact model slug to test |
| `OPENROUTER_BASE_URL` | Optional | Defaults to `https://openrouter.ai/api/v1`; override per target policy |
| `ANTHROPIC_API_KEY` | Yes for real mode | Not written to evidence |
| `ANTHROPIC_MODEL` | Yes for real mode | Exact direct Anthropic model slug to test |
| `ANTHROPIC_BASE_URL` | Optional | Defaults to `https://api.anthropic.com`; override per target policy |
| `UNIROUTE_PRD4_UPSTREAM_MAX_TOKENS` | Optional | Default `16`, clamped to `1..64` |
| `UNIROUTE_PRD4_UPSTREAM_EVIDENCE_PATH` | Optional | Overrides the default evidence JSON path |

## Evidence Contract

Successful real-provider evidence records:

- environment URL and timestamp;
- current git commit from `git rev-parse HEAD`;
- provider, model slug, adapter type, and channel name;
- gateway request id for each mode;
- HTTP status;
- call log id, status code, usage source, token totals, cost, latency, and TTFT;
- trace event names, including `provider.response_accepted`,
  `billing.settle.succeeded`, and `logging.call_log.recorded`;
- evidence path.

Evidence must not include:

- `OPENROUTER_API_KEY`;
- `ANTHROPIC_API_KEY`;
- generated UniRouter raw API key;
- prompt or full response body;
- upstream raw error body beyond sanitized diagnostics.

## Acceptance Boundary

Batch 1H verifies the harness and its guardrails. It does not move `P4-F08` to
`E2E accepted` or `Production verified` without a real-provider run in an
approved target environment.

OpenRouter-compatible success does not substitute for direct Anthropic-native
success. Both paths must pass before the real-upstream evidence track can be
accepted.
