# VERIFY-RESULT.md - PRD4 Batch 1I Real SSO Evidence Harness

**Date:** 2026-07-07
**Signal:** `docs/agent-workflows/FIX-SIGNAL.md`
**Issue:** `PRD4-SSO-001`
**Matrix Row:** `P4-F09`
**Conclusion: ✅ PASS (11/11)**

## Verification Summary

| # | Acceptance Criteria | Result | Evidence |
|---|---------------------|--------|----------|
| 1 | Batch 1H status reconciled as Canary passed and Sentinel merged | ✅ PASS | `CURRENT-STATUS.md:4` — "Batch 1H passed Canary 11/11, and Sentinel merge is reflected by `d3acfa6` / PR #91" |
| 2 | Batch 1I harness files exist and are indexed from current docs | ✅ PASS | 4 files: `scripts/prd4-real-sso-smoke.sh`, `scripts/prd4_real_sso_smoke.mjs`, `scripts/prd4_real_sso_audit_check.go`, `docs/tech-ref/24-prd4-real-sso-evidence.md`; indexed in `CODEBASE-MAP.md:74-75` |
| 3 | Preflight mode returns safe JSON without IdP credentials | ✅ PASS | `scripts/prd4-real-sso-smoke.sh --preflight` returns `{"status":"SKIP","would_drive_browser":false,"would_call_idp":false,"would_read_database":false}` |
| 4 | Preflight explicitly reports no browser, IdP, or DB interaction | ✅ PASS | Preflight JSON: `"would_drive_browser":false,"would_call_idp":false,"would_read_database":false` |
| 5 | Real mode requires exact browser approval token and required target config | ✅ PASS | `prd4_real_sso_audit_check.go:22` — `prd4RealSSOAllowBrowser = "approved-target-real-sso"`; L52 fails without it; `prd4_real_sso_smoke.mjs:15` — same value; smoke requires 6 env vars |
| 6 | Browser runner validates backend OIDC config and issuer discovery | ✅ PASS | `prd4_real_sso_smoke.mjs` checks `GET /api/v1/auth/oidc/config` and `/.well-known/openid-configuration` before browser login |
| 7 | Browser runner drives UniRouter login to IdP with configurable selectors, verifies JWT via `/me` | ✅ PASS | `prd4_real_sso_smoke.mjs` uses Playwright to click SSO button, submit IdP form with configurable selectors, wait for JWT in `localStorage`, then `GET /api/v1/me` with issued JWT |
| 8 | Read-only audit checker guarded before DB access, verifies OIDC user mapping + `login.oidc` audit | ✅ PASS | `prd4_real_sso_audit_check.go:52` — exits with guard error before DB connection; checks `users.auth_provider='oidc'`, expected issuer/claim mapping, latest `login.oidc` audit row |
| 9 | Evidence includes commit/timestamp/provider/issuer/browser/profile/mapping/audit; rejects secret leakage | ✅ PASS | Evidence outputs: git_commit, timestamp, provider_kind, issuer, browser_completion, /me profile, OIDC mapping metadata, audit row id/source/timestamp/detail keys; excludes: IdP password, OIDC client secret, JWT, id_token, access_token, refresh_token, raw profile, raw OIDC subject |
| 10 | Docs state harness/guardrails only; no E2E accepted or Production verified without approved IdP | ✅ PASS | `24-prd4-real-sso-evidence.md:116` — "Batch 1I verifies the harness and its guardrails. It does not move P4-F09 to E2E accepted"; `OPEN-ISSUES.md:70` — "Do not mark P4-F09 E2E accepted or Production verified until approved real browser login passes" |
| 11 | Syntax/preflight/guard checks, broad Go/frontend tests, build, grep, git diff --check pass | ✅ PASS | Preflight: SKIP safe JSON; `node --check`: clean; `go run audit_check`: guard error as expected; Go 22 packages PASS; frontend focused 46 PASS; full frontend 114 (24 files) PASS; build 11.88s; `git diff --check` clean |

## Detailed Verification

### Preflight Safety (AC3/AC4)
```json
{
  "status": "SKIP",
  "scope": "PRD4 Batch 1I real SSO evidence harness preflight",
  "backend_url": "http://localhost:8080",
  "console_url": null,
  "issuer_url": null,
  "provider_kind": "oidc",
  "git_commit": "d3acfa6cb2320f0b133a006655ee5c735e184ddb",
  "missing": [
    "UNIROUTE_PRD4_SSO_ALLOW_BROWSER",
    "UNIROUTE_PRD4_SSO_CONSOLE_URL or UNIROUTE_SMOKE_FRONTEND_URL",
    "UNIROUTE_PRD4_SSO_ISSUER_URL or OIDC_ISSUER_URL",
    "UNIROUTE_PRD4_SSO_TEST_USERNAME",
    "UNIROUTE_PRD4_SSO_TEST_PASSWORD",
    "UNIROUTE_PRD4_SSO_DB_DSN or DB_DSN"
  ],
  "required_approval_value": "approved-target-real-sso",
  "would_drive_browser": false,
  "would_call_idp": false,
  "would_read_database": false
}
```

### Mutation Guard (AC5/AC8)
```
$ go run ./scripts/prd4_real_sso_audit_check.go
real sso audit check failed: UNIROUTE_PRD4_SSO_ALLOW_BROWSER=approved-target-real-sso is required before reading real SSO audit evidence
exit status 1
```
Fails before any DB connection attempt.

### Node Syntax Check
```
$ node --check scripts/prd4_real_sso_smoke.mjs
(no output = clean)
```

### Evidence Sanitization (AC9)
Evidence writer includes: git_commit, ISO timestamp, provider_kind, issuer_url, browser completion status, /me profile fields, OIDC mapping metadata (issuer/subject/claims), audit row id/source/timestamp/detail_keys.
Evidence writer excludes: IdP password, OIDC client secret, UniRouter JWT, id_token, access_token, refresh_token, raw profile response, raw OIDC subject.

## New Issues Found

None.

## Verdict

**PASS.** All 11 acceptance criteria verified. The harness is ready for use when approved IdP credentials and target environment are supplied. `P4-F09` remains below `E2E accepted` / `Production verified` until real browser login passes.
