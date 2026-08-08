# PRD2 OIDC First SSO

> Version: v0.1
> Scope: Batch 24 OIDC-first authentication integration.

## Goal

PRD2 SSO starts with OIDC as a core authentication path. LDAP/SAML and arbitrary auth plugins remain out of this batch unless scope is expanded later.

OIDC login must reuse UniRouter's existing JWT and RBAC model after successful provider authentication:

1. Browser starts at `GET /api/v1/auth/oidc/start`.
2. UniRouter redirects to the provider authorization endpoint with signed `state` and `nonce`.
3. Provider redirects to `GET /api/v1/auth/oidc/callback`.
4. UniRouter exchanges the code, verifies the RS256 `id_token` through discovery/JWKS, checks issuer, audience, expiry, and nonce, then maps claims to a local user.
5. UniRouter signs the existing admin JWT format and the callback page stores it in browser local storage before redirecting to the console.

The gateway API key path is not changed by OIDC.

## Configuration

OIDC is off by default. Required when enabled:

| Env | Meaning |
|-----|---------|
| `OIDC_ENABLED` | Set `true` to expose the SSO login button and callback flow |
| `OIDC_ISSUER_URL` | Provider issuer URL; discovery is loaded from `/.well-known/openid-configuration` |
| `OIDC_CLIENT_ID` | UniRouter OIDC client id |
| `OIDC_CLIENT_SECRET` | UniRouter OIDC client secret |
| `OIDC_REDIRECT_URL` | Absolute callback URL, usually `https://<host>/api/v1/auth/oidc/callback` |

Optional mapping and provisioning controls:

| Env | Default | Meaning |
|-----|---------|---------|
| `OIDC_PROVIDER_NAME` | `OIDC` | Login button label |
| `OIDC_SCOPES` | `openid,email,profile` | Requested scopes; `openid` is always required |
| `OIDC_ALLOWED_EMAIL_DOMAIN` | empty | Optional email-domain allow-list |
| `OIDC_AUTO_PROVISION` | `false` | If false, only pre-bound users can log in |
| `OIDC_DEFAULT_ORG_ID` | `0` unless set | Required when auto provisioning is enabled |
| `OIDC_DEFAULT_ROLE` | `viewer` | First-login fallback authority input. Organization roles set `users.organization_role`; project roles create `organization_role=member` plus one default-project membership. |
| `OIDC_DEFAULT_PROJECT_SLUG` | `default` | Membership target only when the resolved input is a project role |
| `OIDC_USERNAME_CLAIM` | `preferred_username` | Preferred username claim |
| `OIDC_EMAIL_CLAIM` | `email` | Email claim |
| `OIDC_ROLE_CLAIM` | `uniroute_role` | UniRouter role claim |
| `OIDC_ORG_ID_CLAIM` | `uniroute_org_id` | UniRouter org id claim |

## User Binding

Migration `000015_prd2_oidc_sso` adds these fields to `users`:

- `email`
- `auth_provider`
- `oidc_issuer`
- `oidc_subject`

Existing local users remain `auth_provider='local'`. OIDC users are looked up by `(oidc_issuer, oidc_subject)`.

If auto provisioning is enabled and no binding exists:

1. The configured/default org must exist and be active.
2. Username comes from the username claim, then email, then subject.
3. Authority input comes from `OIDC_ROLE_CLAIM` if present and valid, otherwise `OIDC_DEFAULT_ROLE`.
4. Organization inputs `platform_admin` (`admin` is an accepted legacy alias), `org_admin`, `billing_admin`, `org_viewer`, and `member` set the canonical `users.organization_role`. They do not create or imply any project membership.
5. Project inputs `project_admin`, `project_user`, and `project_viewer` set `users.organization_role=member` and add exactly one canonical membership in the project named by `OIDC_DEFAULT_PROJECT_SLUG`. Compatibility aliases `developer`/`project_developer` map to `project_user`, and `viewer` maps to `project_viewer`; aliases are not persisted in `project_members` after migration `000058`.
6. `users.role` remains a conservative rolling-upgrade shadow and is not an authorization source for the current binary. Project authorization reads the live `project_members` row for the requested project.
7. A `login.oidc` audit row is recorded without `id_token`, `access_token`, client secret, or raw provider response.

Existing OIDC users keep their stored organization role, compatibility shadow,
organization, and project memberships. Role changes should be controlled in
UniRouter unless a future policy explicitly enables continuous claim sync.

## Security Notes

- Only RS256 `id_token` verification is implemented in Batch 24.
- `state` and `nonce` are random and stored in an HMAC-signed, HttpOnly, SameSite=Lax cookie.
- The callback does not put the UniRouter JWT in the URL query string. It returns same-origin HTML that writes the JWT to local storage, then redirects to the console.
- Redirects are restricted to relative console paths; absolute URLs and `/api/*` paths are rejected.
- The password login path remains available unless operators disable it with a future policy.

## Verification

Targeted checks:

```bash
go test ./internal/oidc ./internal/admin ./internal/config ./cmd/uniroute
npm --prefix frontend test -- --run LoginPage client navigation
bash -n scripts/prd2-batch24-oidc-smoke.sh
node --check scripts/prd2_batch24_oidc_smoke.mjs
```

The Batch 24 smoke uses a local mock OIDC provider. Production or customer IdP acceptance still requires operator-provided issuer/client configuration.
