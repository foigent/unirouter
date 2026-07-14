# Commercial License Enforcement

> Version: v1.0
> Scope: Post-PRD6 V1 commercial runtime. The PRD2 foundation remains
> historical evidence, but its non-blocking behavior is superseded here.

## V1 Contract

- Only a strict, signed `unirouter.license.v1` document is commercially
  enforceable.
- The signature covers `schema_version`, `key_id`, and the complete payload.
- The License is bound to `LICENSE_DEPLOYMENT_ID` and checked against current
  wall-clock time on every new model admission.
- Missing, invalid, not-yet-valid, deployment-mismatched, expired, or
  feature-incomplete authority rejects new model and Agent-capability traffic
  before API-key lookup, billing reservation, or provider dispatch.
- Local login, License read/import, health, migration, backup, diagnostics and
  audit recovery remain available.

This V1 contract does not add online activation, hardware fingerprinting, a
License marketplace, or a runtime switch that disables commercial admission.
Host root can patch a local binary; commercial delivery therefore uses final
artifacts without source or builder cache, signed releases, controlled key
custody, and contract/support controls rather than an obfuscation claim.

## Signed Document

Stored setting key: `license.offline`.

Expected JSON:

```json
{
  "schema_version": "unirouter.license.v1",
  "key_id": "license-2026-a",
  "payload": {
    "license_id": "lic_acme_prod_1",
    "customer": "Acme Corp",
    "deployment_id": "prod-cn-1",
    "tier": "enterprise",
    "issued_at": "2026-07-14T00:00:00Z",
    "not_before": "2026-07-14T00:00:00Z",
    "expires_at": "2027-07-14T00:00:00Z",
    "features": ["gateway", "model_governance"],
    "limits": {
      "active_api_keys": 100,
      "active_users": 100,
      "active_projects": 20,
      "active_channels": 20
    }
  },
  "signature": "base64-ed25519-signature"
}
```

Unknown document fields, unknown feature/limit names, duplicate features,
non-positive limits, oversized identities/metadata, and trailing JSON are
rejected. The request body is capped at 256 KiB. Private signing keys must
never enter the repository, runtime image, customer package, logs, database
projection, or evidence.

## Runtime Config

| Variable | Required | Purpose |
|----------|----------|---------|
| `LICENSE_PUBLIC_KEYS` | Required for V1 commercial operation | Strict JSON key-ID to Ed25519 public-key ring |
| `LICENSE_DEPLOYMENT_ID` | Required for V1 commercial operation | Stable deployment identity that must exactly match the signed payload |
| `LICENSE_EXPIRY_WARNING_DAYS` | Optional, default `30` | Warning window before expiry |
| `LICENSE_REFRESH_SECONDS` | Optional, default `30` | Maximum database-to-replica convergence interval after import |
| `LICENSE_PUBLIC_KEY` | Legacy only | PRD2 payload-only verification key; never makes a document commercially enforceable |

The service may start without commercial authority so a platform administrator
can recover it, but new model traffic remains fail-closed. Malformed configured
keys fail startup validation. Stable and canary replicas must use the same V1
key ring and deployment identity.

## API

| Method | Path | Role | Behavior |
|--------|------|------|----------|
| `GET` | `/api/v1/license` | `platform_admin` / legacy `admin` | Returns verified status and payload metadata, never the signature |
| `PUT` | `/api/v1/license` | `platform_admin` / legacy `admin` | Strictly verifies, checks current inventory, atomically stores License + entitlement projection + audit |

License management is intentionally platform-admin only. `org_admin`, billing roles, and project roles cannot access `/license`.

## Status Values

| Status | Meaning |
|--------|---------|
| `missing` | No license document is stored |
| `active` | V1 signature, time and deployment checks pass outside the warning window |
| `expiring` | V1 checks pass inside the warning window; still enforceable |
| `expired` | Signature is valid but expiry is in the past |
| `not_yet_valid` | Current time precedes signed `not_before` |
| `deployment_mismatch` | Signed deployment does not match this runtime |
| `invalid` | Schema, key, JSON, signature, entitlement, or configuration verification failed |

`valid` alone is not sufficient: only `enforceable=true` authorizes commercial
runtime. Legacy PRD2 documents can be read for evidence when their legacy key
is present, but always return `enforceable=false`.

## Feature And Resource Enforcement

Supported feature keys are `gateway`, `agent_capabilities`, `oidc`,
`guardrails`, `notifications`, `model_governance`, `data_exchange`, and `*`.
`gateway` protects Chat, Embeddings, Models and the Admin channel test that
makes a real upstream request; Agent capability endpoints require both
`gateway` and `agent_capabilities`. The credential-free routing preview remains
available for diagnosis, while feature-specific control-plane routes are gated
separately.

Existing Guardrail and model-governance enforcement is not turned off when a
feature is absent or a License expires: doing so would weaken safety. The
feature controls management/use of the licensed surface while existing
deny/safety policy continues to apply.

Migration `000037_v1_commercial_release_control` stores only the verified
entitlement projection and enforces positive active API-key, user, project and
channel limits with per-resource PostgreSQL advisory locks. Concurrent create
or inactive-to-active changes cannot race over capacity. Disable/delete
remains possible. Importing a lower renewal is rejected when current active
inventory already exceeds it.

After migrations, startup takes the global License lock, verifies the stored
document and reconstructs this derived projection before making the runtime
enforceable. Periodic replica refresh does not blindly trust `settings`: it
requires the projection identity, expiry and every signed/absent limit to match.
A mismatch fails model traffic closed and requires a normal audited re-import;
this prevents a partial database restore from turning a signed limit into
unlimited capacity.

## Audit

`PUT /api/v1/license` records `license.update` with before/after summary fields:

- status
- valid/signature_valid/warning
- customer
- deployment_id
- tier
- expires_at
- features/limits

The raw signature and complete signed document are not returned by GET and are
not written into audit summaries or entitlement projections.

## Offline Issuer Tooling

Build the vendor-side tool outside the customer runtime image:

```bash
go build -o ./bin/uniroute-license ./cmd/uniroute-license
./bin/uniroute-license keygen \
  --key-id license-2026-a \
  --private-out /approved-offline-custody/license-2026-a.private \
  --public-out ./license-public-keys.json
./bin/uniroute-license issue \
  --key-id license-2026-a \
  --private-key /approved-offline-custody/license-2026-a.private \
  --payload ./customer-license-payload.json \
  --out ./customer-license.json
./bin/uniroute-license verify \
  --public-keys ./license-public-keys.json \
  --deployment-id customer-approved-deployment-id \
  --license ./customer-license.json
```

`--public-out` is already a directly consumable single-key JSON ring. Add a
new key through a separately reviewed rotation step; do not hand-edit a private
key into this file.

Output files are created with no-overwrite semantics; private keys use mode
`0600`. Key generation in this repository is tooling, not a key-custody
decision. Production/customer key creation and storage require separately
approved custody and rotation procedures.

## Historical PRD2 Evidence

Batch 22 smoke and
`docs/ops/evidence/PRD2-B22-license-local-smoke-20260703.md` remain valid for
the historical PRD2 foundation only. They generate payload-only documents and
must not be used as V1 commercial activation evidence. Target trust-root,
deployment-ID and License activation remain R4 operations requiring explicit
authorization.
