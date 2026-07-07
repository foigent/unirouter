# FIX-SIGNAL.md - PRD4 Batch 1I Real SSO Evidence Harness

**Date:** 2026-07-07
**Status:** Fixed awaiting Canary
**Issue:** `PRD4-SSO-001`
**Matrix Row:** `P4-F09`

## Verification Request

Please verify PRD4 Batch 1I: Real SSO Evidence Harness.

This batch adds a guarded evidence path for PRD4 real SSO target-environment
proof against Logto or another approved enterprise OIDC provider. It does not
claim real SSO E2E acceptance in the current workspace, because IdP credentials,
test-user access, and approved target environment access are not present.

## Scope To Verify

Harness files:

- `scripts/prd4-real-sso-smoke.sh`
- `scripts/prd4_real_sso_smoke.mjs`
- `scripts/prd4_real_sso_audit_check.go`
- `docs/tech-ref/24-prd4-real-sso-evidence.md`

Expected behavior:

- `scripts/prd4-real-sso-smoke.sh --preflight` is safe without target
  credentials.
- Preflight reports `status: SKIP` when required target configuration is
  missing.
- Preflight does not drive a browser, call an IdP, or read the database.
- Real mode is blocked unless
  `UNIROUTE_PRD4_SSO_ALLOW_BROWSER=approved-target-real-sso`.
- Real mode requires:
  - console URL;
  - backend URL;
  - Logto or enterprise OIDC issuer URL;
  - IdP test username and password;
  - DB DSN for read-only user/audit evidence;
  - optional expected role/org/email/username mapping assertions.
- Real mode checks backend `GET /api/v1/auth/oidc/config`.
- Real mode checks issuer discovery at `/.well-known/openid-configuration`.
- Real mode uses Playwright to click the UniRouter SSO login button, submit the
  IdP login form, and wait for a UniRouter JWT in browser `localStorage`.
- Real mode verifies `GET /api/v1/me` with the issued JWT.
- Real mode performs a guarded read-only DB check for `users.auth_provider =
  'oidc'`, expected issuer/claim mapping, and a latest `login.oidc` audit row.
- Evidence includes provider kind/name, issuer, commit, browser completion,
  `/me` profile, OIDC mapping metadata, audit row id/source/timestamp, and audit
  detail keys.
- Evidence must not include IdP password, OIDC client secret, UniRouter JWT,
  `id_token`, `access_token`, `refresh_token`, raw profile response, or raw OIDC
  subject.

Docs/status:

- `P4-F08` / `PRD4-UPSTREAM-001` is marked Canary passed after Batch 1H PASS
  11/11 and Sentinel merge `d3acfa6` / PR #91.
- `P4-F09` / `PRD4-SSO-001` is marked fixed awaiting Canary for harness
  verification only.
- `P4-F09` must not be marked `E2E accepted` or `Production verified` until a
  real browser login passes against an approved Logto or enterprise OIDC target.

## Non-Scope

Do not expect this batch to implement:

- Real SSO browser evidence without approved IdP credentials.
- Any browser automation, IdP calls, or DB reads during preflight.
- Logto or IAM rewrite into UniRouter core.
- SAML, LDAP, SCIM, MFA policy, social login, or identity lifecycle management.
- OIDC claim continuous-sync policy changes.
- New auth/RBAC/session semantics.
- Runtime plugin loading, manifest parsing, sandboxing, marketplace, third-party
  installation, or PlungeIn Mutate behavior.

## Required Verification Commands

```bash
rg -n "prd4-real-sso-smoke|prd4_real_sso_smoke|prd4_real_sso_audit_check|approved-target-real-sso|PRD4 Batch 1I|P4-F09|PRD4-SSO-001|Logto|real SSO" scripts docs/tech-ref/24-prd4-real-sso-evidence.md docs/agent-workflows/FIX-SIGNAL.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/PRD-Phase4.md docs/PROJECT-HANDOFF.md docs/DOC-INVENTORY.md docs/README.md docs/tech-ref/README.md docs/CODEBASE-MAP.md
scripts/prd4-real-sso-smoke.sh --preflight
node --check scripts/prd4_real_sso_smoke.mjs
go run ./scripts/prd4_real_sso_audit_check.go
go test ./internal/oidc ./internal/admin ./internal/config ./cmd/uniroute -count=1
go test ./... -count=1
npm --prefix frontend test -- --run LoginPage client navigation
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

The `go run ./scripts/prd4_real_sso_audit_check.go` command is expected to fail
before reading DB in this workspace unless
`UNIROUTE_PRD4_SSO_ALLOW_BROWSER=approved-target-real-sso` is set.

## Acceptance Criteria

1. Batch 1H status is reconciled as Canary passed and Sentinel merged in current
   PRD4 docs.
2. The Batch 1I harness files exist and are indexed from current docs.
3. Preflight mode returns safe JSON and does not require IdP credentials.
4. Preflight mode explicitly reports that it will not drive a browser, call an
   IdP, or read DB.
5. Real mode requires the exact browser approval token and required target
   configuration.
6. The browser runner validates backend OIDC public config and issuer discovery.
7. The browser runner can drive UniRouter login to the external IdP using
   configurable selectors, then verify a UniRouter JWT through `/me`.
8. The read-only audit checker is guarded before DB access and verifies OIDC user
   mapping plus `login.oidc` audit evidence.
9. Evidence output includes commit/timestamp/provider/issuer/browser/profile/
   mapping/audit metadata and rejects secret/token leakage.
10. Docs clearly state that Batch 1I verifies harness and guardrails only; no
    real SSO E2E acceptance or Production verified status is claimed without
    approved target-IdP evidence.
11. Focused syntax/preflight/guard checks, broad Go/frontend tests, frontend
    build, docs grep, and `git diff --check` pass.
