# License Foundation

> Version: v0.1
> Scope: PRD2 Batch 15 local-deploy offline license foundation.

## Goals

- Support an offline license document imported by a platform admin.
- Verify license signature before saving it.
- Expose license status and metadata in Admin Console.
- Audit license changes with before/after summaries.
- Warn when a license is expiring without blocking existing gateway requests.

## Non-goals

- No online activation.
- No hardware fingerprinting.
- No marketplace or reseller workflow.
- No gateway request blocking in this foundation batch.

## License Document

Stored setting key: `license.offline`.

Expected JSON:

```json
{
  "payload": {
    "customer": "Acme Corp",
    "deployment_id": "prod-cn-1",
    "tier": "enterprise",
    "issued_at": "2026-07-01T00:00:00Z",
    "expires_at": "2026-12-31T00:00:00Z",
    "features": ["projects", "native_adapters"],
    "limits": {
      "projects": 20
    }
  },
  "signature": "base64-ed25519-signature"
}
```

The signature is Ed25519 over the canonical JSON encoding of `payload`.

## Runtime Config

| Variable | Required | Purpose |
|----------|----------|---------|
| `LICENSE_PUBLIC_KEY` | Required before importing a license | Base64 or hex encoded 32-byte Ed25519 public key |
| `LICENSE_EXPIRY_WARNING_DAYS` | Optional, default `30` | Warning window before expiry |

If `LICENSE_PUBLIC_KEY` is missing, the service still starts and `GET /api/v1/license` can return missing/invalid status, but `PUT /api/v1/license` rejects imports with a clear error.

## API

| Method | Path | Role | Behavior |
|--------|------|------|----------|
| `GET` | `/api/v1/license` | `platform_admin` / `admin` | Returns current license status and metadata |
| `PUT` | `/api/v1/license` | `platform_admin` / `admin` | Validates and stores an offline license document |

License management is intentionally platform-admin only. `org_admin`, billing roles, and project roles cannot access `/license`.

## Status Values

| Status | Meaning |
|--------|---------|
| `missing` | No license document is stored |
| `active` | Signature is valid and expiry is outside the warning window |
| `expiring` | Signature is valid and expiry is inside the warning window |
| `expired` | Signature is valid but expiry is in the past |
| `invalid` | License JSON, required fields, public key, signature, or expiry parsing failed |

## Audit

`PUT /api/v1/license` records `license.update` with before/after summary fields:

- status
- valid/signature_valid/warning
- customer
- deployment_id
- tier
- expires_at
- features/limits

The raw signature is not written into audit summaries.
