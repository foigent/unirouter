# V1 Commercial License And Signed Offline Upgrade Contract

**Status:** User-approved V1 release gate; local implementation verified, pull request and required CI pending

**Owner:** UniRouter

**Risk:** R3 for authorization and signing; R4 for any target activation

**Boundary:** This is Post-PRD6 V1 release work. It does not reopen PRD6
Accepted Scope Complete.

## 1. Release Gate

UniRouter V1 is not ready for customer delivery until both contracts below are
implemented and verified:

1. a signed commercial license actively controls new model traffic and signed
   feature/resource entitlements; and
2. an offline upgrade bundle is rejected before execution when its signature,
   manifest, payload hash/size, product, platform, release sequence or schema
   compatibility cannot be proved.

Repository privacy, JavaScript minification, a stripped Go binary and source
obfuscation are not license enforcement. A customer with source code or host
root can ultimately patch a local binary, so the supported delivery model is a
signed final artifact without source or builder cache, backed by commercial
terms and a controlled update/support channel.

## 2. Commercial License V1

### 2.1 Signed document

The enforceable document uses an explicit schema and signing-key identifier:

```json
{
  "schema_version": "unirouter.license.v1",
  "key_id": "license-2026-a",
  "payload": {
    "license_id": "lic_customer_release_unique_id",
    "customer": "Customer name",
    "deployment_id": "customer-approved-stable-deployment-id",
    "tier": "enterprise",
    "issued_at": "2026-07-14T00:00:00Z",
    "not_before": "2026-07-14T00:00:00Z",
    "expires_at": "2027-07-14T00:00:00Z",
    "features": ["*"],
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

The signature covers the schema version, key ID and complete payload. Unknown
fields are rejected rather than silently removed from the signed contract.
Signing private keys never enter the application image, customer bundle,
repository evidence or runtime environment. Runtime receives only a public-key
ring so one key can be rotated without accepting an unsigned trust change.

### 2.2 Enforced status

New model traffic is admitted only when all of the following are true:

- the document uses the supported commercial schema;
- `key_id` selects a configured trusted public key;
- the Ed25519 signature is valid;
- `not_before <= now < expires_at`;
- `deployment_id` exactly matches the configured stable deployment identity;
- the requested feature is present, or the signed feature list contains `*`;
- every applicable signed resource limit remains available.

Missing, invalid, not-yet-valid, mismatched or expired licenses fail closed for
new `/v1` traffic and Agent-capability control requests. `expiring` remains
valid and emits warnings. A request admitted before expiry is allowed to reach
its existing financial/stream terminal contract; no request is interrupted
mid-provider merely because the wall clock crossed expiry.

### 2.3 Recovery surface

License failure does not make the deployment unrecoverable. The following
remain available:

- liveness/readiness and metrics under their existing exposure policy;
- local admin login and platform-admin License read/import;
- migration, backup, version, runtime diagnostics and audit readback; and
- shutdown and ordinary dependency recovery.

OIDC requires a currently enforceable License with the signed `oidc` feature;
it is not the License-failure recovery path. A local platform administrator
must remain available for recovery, and ordinary model traffic never bypasses
the License.
License import is atomic with its audit record and verified resource-limit
projection. Startup reconciles a restored valid document under the same
resource locks; recurring replica refresh accepts it only while that projection
still matches. Expiry is evaluated against current time on every admission.

### 2.4 Resource-limit semantics

Only explicitly signed positive limits are enforced. An absent limit means the
license does not constrain that resource; zero is rejected in an issued
commercial document rather than ambiguously meaning unlimited. Counts use
active resources and must be concurrency-safe. Disabling a resource releases
capacity; re-enabling consumes it again.

### 2.5 Explicit non-claims

- Offline licensing cannot prove that a customer did not clone an authorized
  database for legitimate disaster recovery or patch a binary as host root.
- Hardware fingerprinting is not a V1 dependency.
- No runtime environment switch disables enforcement in the customer release.
- Legacy PRD2 payload-only signatures remain readable for historical evidence
  but are not enforceable commercial licenses.

## 3. Signed Offline Upgrade Bundle V1

### 3.1 Bundle contents

One deterministic archive contains only regular files:

```text
manifest.json
manifest.sig
payload/<declared artifact files>
```

The signed manifest includes:

- schema and product identifiers;
- signing `key_id` and signature algorithm;
- monotonically increasing release sequence;
- release version and exact source commit;
- creation and metadata-expiry timestamps;
- target OS and architecture;
- minimum/maximum accepted current release sequence;
- minimum current schema and target schema;
- every payload path, byte size and SHA-256 digest; and
- optional build-provenance digest bound to the declared
  `payload/provenance/build-provenance.json` artifact, without requiring
  network access during customer verification.

### 3.2 Verification and preflight

Verification occurs before extraction or execution and rejects:

- unknown schema/product/key/signature algorithm;
- invalid signature, expired metadata or untrusted key;
- duplicate, undeclared, missing or non-regular archive entries;
- absolute paths, `..`, symlinks, devices or other path traversal forms;
- payload size/hash mismatch or configured archive bounds;
- wrong OS/architecture;
- release-sequence rollback or an unsupported jump;
- current/target database-schema incompatibility; and
- a post-apply record when the running binary's immutable version/commit or
  database schema disagrees with the signed manifest.

Verification is read-only. Apply remains a separate explicit operator action
after backup, financial-state drain, migration preflight and human approval.
V1 does not execute an arbitrary uploaded file from the Admin Console.

### 3.3 Rollback and key rotation

The installed release sequence and accepted package digest are persisted only
after the exact new runtime is healthy. Database migrations that have written
new authoritative evidence may declare roll-forward-only recovery. Runtime
image rollback must never silently pair an old writer with an incompatible new
schema.

Upgrade-signing trust is separate from License-signing trust. Both use named
public-key rings; production private keys are separately custodied. Formal
release builds embed both public rings into the signed backend binary and
reject runtime `LICENSE_PUBLIC_KEY`, `LICENSE_PUBLIC_KEYS`, or
`UPGRADE_PUBLIC_KEYS` overrides. Source-development builds retain explicit
test rings, but are not customer-release eligible. A later threshold/TUF
repository may replace this single-offline-bundle profile, but V1 does not
claim full TUF conformance.

The design follows the relevant TUF principles of signed target metadata,
hash/size verification and rollback/freeze resistance, and SLSA's separation
between artifact identity and build provenance:

- https://theupdateframework.github.io/specification/draft/
- https://slsa.dev/spec/v1.2/provenance

## 4. Acceptance Evidence

### 4.1 License

- valid, missing, tampered, wrong-key, wrong-deployment, not-yet-valid,
  expiring and expired fixtures;
- new Chat, Embeddings, Models and Agent-capability requests fail before API-key
  lookup, billing reservation or provider dispatch when unlicensed;
- admin recovery/import remains available;
- hot import/renewal activates every replica within the bounded refresh window;
- expiry is recognized without requiring a refresh or restart;
- resource-limit boundary and concurrent create/re-enable tests;
- no private key, raw signature or full license document leaks to audit/logs;
- full Go/frontend/build regression and required CI.

### 4.2 Upgrade bundle

- deterministic pack and successful offline verify;
- single-byte payload and manifest tamper rejection;
- wrong key, wrong product, wrong platform, expired metadata, duplicate entry,
  path traversal, symlink, undeclared file, size overflow and missing-file
  rejection;
- release rollback and schema incompatibility rejection;
- no extraction/execution before full verification;
- exact package digest, release sequence and runtime metadata readback;
- required CI builds/verifies a test bundle without a production signing key.

## 5. Target Activation Boundary

The existing ECS is development/test, but deploying a new image with embedded
License/upgrade trust roots, changing its deployment identity, stored License,
release sequence or runtime admission is an R4 action. Local and CI completion
do not authorize creation of a persistent production signing key or customer
License. Before target activation, retain the old images and database backup,
import a separately approved non-production License, prove the unlicensed
denial and licensed success without a paid provider call, then remove any
ephemeral private signing material from the target workflow.
