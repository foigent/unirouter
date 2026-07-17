# PRD4 Real SSO Evidence Harness

> Scope: PRD4 Batch 1I / `P4-F09`
> Status: Harness for approved target-environment SSO evidence. It does not
> create real SSO evidence unless approved IdP credentials, test user access, and
> target environment access are supplied.

## Purpose

PRD2 added OIDC-first SSO and verified it with a mock provider. PRD4 needs a
repeatable target-environment evidence path for Logto or another approved
enterprise OIDC provider.

This harness verifies the existing UniRouter OIDC path against a real IdP. It
does not rewrite Logto into UniRouter core and does not add a general IAM
platform.

## Files

| File | Purpose |
|------|---------|
| `scripts/prd4-real-sso-smoke.sh` | Shell wrapper and `--preflight` entrypoint |
| `scripts/prd4_real_sso_smoke.mjs` | Browser login runner and sanitized evidence writer |
| `scripts/prd4_real_sso_audit_check.go` | Read-only DB check for OIDC user mapping and `login.oidc` audit evidence |

## Safety Model

Preflight mode is safe by default:

```bash
scripts/prd4-real-sso-smoke.sh --preflight
```

Preflight returns JSON with `status: SKIP` when required target configuration is
missing. It does not drive a browser, does not call the IdP, and does not read
the database.

Real SSO mode requires explicit opt-in:

```bash
UNIROUTE_PRD4_SSO_ALLOW_BROWSER=approved-target-real-sso \
UNIROUTE_SMOKE_BACKEND_URL=https://unirouter.example.com \
UNIROUTE_PRD4_SSO_CONSOLE_URL=https://unirouter.example.com \
UNIROUTE_PRD4_SSO_ISSUER_URL=https://logto.example.com/oidc \
UNIROUTE_PRD4_SSO_TEST_USERNAME='test-user@example.com' \
UNIROUTE_PRD4_SSO_TEST_PASSWORD='...' \
UNIROUTE_PRD4_SSO_DB_DSN='postgres://...' \
scripts/prd4-real-sso-smoke.sh
```

Real mode:

- checks `GET /api/v1/auth/oidc/config` and requires OIDC to be enabled;
- checks issuer discovery at `/.well-known/openid-configuration`;
- derives the approved credential destination from the discovered
  `authorization_endpoint` and rejects non-HTTPS or non-exact browser origins
  before each username/password fill;
- blocks unapproved main-frame redirects during credential entry; federated or
  multi-origin login requires each additional HTTPS origin to be explicitly
  configured, while subframe and static-resource loading remains unaffected;
- opens the UniRouter console login page with Playwright;
- clicks the configured SSO login button;
- fills the IdP username and password fields;
- waits for UniRouter's callback flow to store the admin JWT in browser
  `localStorage`;
- calls `GET /api/v1/me` with the issued JWT;
- performs a read-only DB check for:
  - `users.auth_provider = 'oidc'`;
  - expected issuer/role/org/email/username when configured;
  - a latest `audit_logs.action = 'login.oidc'` row;
  - no `id_token`, `access_token`, `refresh_token`, client secret, or test
    password in audit detail;
- writes sanitized JSON evidence to `docs/ops/evidence/` by default.

## Required Environment

| Variable | Required | Notes |
|----------|----------|-------|
| `UNIROUTE_PRD4_SSO_ALLOW_BROWSER` | Yes for real mode | Must equal `approved-target-real-sso` |
| `UNIROUTE_SMOKE_BACKEND_URL` or `BACKEND_URL` | Recommended | Defaults to `http://localhost:8080` |
| `UNIROUTE_PRD4_SSO_CONSOLE_URL` or `UNIROUTE_SMOKE_FRONTEND_URL` | Yes for real mode | Console login origin; target deployments should normally serve API and console same-origin for OIDC callback localStorage |
| `UNIROUTE_PRD4_SSO_ISSUER_URL` or `OIDC_ISSUER_URL` | Yes for real mode | Logto or enterprise OIDC issuer URL |
| `UNIROUTE_PRD4_SSO_TEST_USERNAME` | Yes for real mode | Target IdP test user |
| `UNIROUTE_PRD4_SSO_TEST_PASSWORD` | Yes for real mode | Not written to evidence |
| `UNIROUTE_PRD4_SSO_DB_DSN` or `DB_DSN` | Yes for real mode | Read-only evidence check for user mapping and audit row |
| `UNIROUTE_PRD4_SSO_PROVIDER_KIND` | Optional | Defaults to `oidc`; use `logto` for Logto target evidence labeling |
| `UNIROUTE_PRD4_SSO_HEADLESS` | Optional | Defaults to headless browser mode; set `false` for manual observation |
| `UNIROUTE_PRD4_SSO_USERNAME_SELECTOR` | Optional | Comma-separated selector override for IdP username field |
| `UNIROUTE_PRD4_SSO_AFTER_USERNAME_SELECTOR` | Optional | Selector for IdP next/continue button when the IdP has a two-step login |
| `UNIROUTE_PRD4_SSO_PASSWORD_SELECTOR` | Optional | Comma-separated selector override for IdP password field |
| `UNIROUTE_PRD4_SSO_SUBMIT_SELECTOR` | Optional | Selector override for IdP submit button |
| `UNIROUTE_PRD4_SSO_ADDITIONAL_CREDENTIAL_ORIGINS` | Optional | Comma-separated additional exact HTTPS origins approved to receive test credentials; paths, queries, fragments, HTTP, suffix matches, and implicit wildcarding are rejected |
| `UNIROUTE_PRD4_SSO_EXPECTED_ROLE` | Optional | Verifies `/me` and DB role mapping |
| `UNIROUTE_PRD4_SSO_EXPECTED_ORG_ID` | Optional | Verifies `/me` and DB org mapping |
| `UNIROUTE_PRD4_SSO_EXPECTED_USERNAME` | Optional | Verifies `/me` and DB username mapping |
| `UNIROUTE_PRD4_SSO_EXPECTED_EMAIL` | Optional | Verifies DB email mapping |
| `UNIROUTE_PRD4_SSO_EVIDENCE_PATH` | Optional | Overrides default evidence JSON path |

## Evidence Contract

Successful real SSO evidence records:

- target backend URL, console URL, issuer URL, provider kind, provider name, and
  current git commit;
- discovery endpoint hosts, not raw response bodies;
- browser login completion and final origin;
- `/me` user id, username, role, org id, org name, and status;
- OIDC DB mapping metadata with hashed subject;
- latest `login.oidc` audit row id/source/timestamp and audit detail keys;
- evidence path.

Evidence must not include:

- IdP password;
- OIDC client secret;
- UniRouter JWT;
- `id_token`, `access_token`, or `refresh_token`;
- full provider profile response;
- raw OIDC subject.

## Acceptance Boundary

Batch 1I verifies the harness and its guardrails. It does not move `P4-F09` to
`E2E accepted` or `Production verified` without a real browser login against an
approved target IdP.

Logto evidence is accepted as an external OIDC target when the issuer/client and
test user are approved. Rewriting Logto or another IAM product into UniRouter
core remains deferred to PRD5.
