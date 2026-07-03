# PRD2 Batch 24 OIDC Local Smoke Evidence

> Date: 2026-07-04 CST
> Environment: local current worktree tests with mock OIDC provider
> Scope: Batch 24 OIDC-first SSO implementation

## Objective

Verify the OIDC-first authentication path without requiring a real enterprise IdP:

- OIDC discovery, authorization URL construction, token exchange, JWKS, RS256 `id_token`, issuer/audience/expiry/nonce validation;
- claim mapping for email, username, role, and org id;
- admin callback auto-provisioning, JWT issuance, default project membership for project-scoped roles, and `login.oidc` audit without token leakage;
- public OIDC config endpoint and frontend login-page SSO entry;
- route parity and environment configuration.

## Commands

```bash
go test ./internal/oidc ./internal/admin ./internal/config ./cmd/uniroute -count=1
scripts/prd2-batch24-oidc-smoke.sh
npm --prefix frontend test -- --run LoginPage client navigation
```

## Result

```json
{"status":"PASS","batch":24,"scope":"oidc_mock_provider_and_frontend_entry"}
```

## Coverage

| Check | Result |
|-------|--------|
| `internal/oidc` builds authorization URL from discovery | PASS |
| `internal/oidc` exchanges code and verifies RS256 `id_token` through JWKS | PASS |
| `internal/oidc` rejects nonce mismatch | PASS |
| Admin `GET /api/v1/auth/oidc/config` returns disabled state without config | PASS |
| Admin `GET /api/v1/auth/oidc/start` redirects and sets signed HttpOnly state cookie | PASS |
| Admin callback auto-provisions OIDC user and signs existing UniRouter JWT | PASS |
| Project-scoped OIDC user receives default project membership | PASS |
| `login.oidc` audit detail excludes `id_token` and `access_token` | PASS |
| Frontend LoginPage renders SSO button only when OIDC config is enabled | PASS |
| API client uses `/api/v1/auth/oidc/config` and `/api/v1/auth/oidc/start` | PASS |
| Route parity includes new OIDC auth routes | PASS |

## Boundary

This is local mock-provider evidence, not production IdP acceptance. Real target-environment OIDC acceptance still needs operator-provided issuer/client credentials and a browser login against the approved IdP.
