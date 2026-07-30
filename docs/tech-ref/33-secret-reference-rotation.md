# Secret Reference And Rotation Contract

> Contract: `unirouter.secret-reference.v1`
> Scope: PRD7 B4 / P7-F06
> Runtime status: backend-neutral contract and in-memory fixture only

## 1. Verified Current Boundary

The existing supplier credential path stores one AES-GCM ciphertext in
`channel_keys.key_enc`. `state_epoch` invalidates route snapshots after an
administrative update, but it is not a credential-version identity and does
not provide stage, activate, rollback, revoke or stale-worker semantics.

B4 does not reinterpret or migrate that table. It adds an independent contract
for a later external secret backend. Therefore current channel creation,
encrypted-key update, routing selection, provider request construction and
failure handling remain unchanged.

## 2. Contract Shape

`internal/secretref` owns six safe metadata types:

- `Reference`: fixed backend name, operator-owned scope and logical secret
  name;
- `Binding`: exact version plus active generation pinned by a worker;
- `VersionMetadata`: staged/active/retired/revoked lifecycle facts;
- `Snapshot`: current revision, generation, active binding and version list;
- `AuditReceipt`: metadata-only evidence for a successful state transition;
- `Registry`: immutable process-construction allowlist of concrete backends.

The registry is not a plugin loader. A process must be assembled with a fixed
set of typed backend implementations; arbitrary code or a dynamic
high-privilege plugin cannot be registered through an API.

References use printable ASCII and a canonical
`secretref://<backend>/<scope>/<name>` representation. They contain no secret
value, ciphertext, low-entropy material digest, access token or backend
credential.

## 3. Lifecycle

Secret bytes are immutable after a version is staged. Lifecycle metadata may
move according to this state machine:

| Operation | Allowed source | Result | Active generation |
|---|---|---|---|
| `stage` | new version only | `staged` | unchanged |
| `activate` | `staged` or `retired` | target `active`; prior active `retired` | incremented |
| `rollback` | `retired` | target `active`; prior active `retired` | incremented |
| `revoke` | `staged` or `retired` | `revoked` | unchanged |

Every writer supplies the exact expected lifecycle revision. Concurrent or
stale writes fail with `ErrRevisionConflict`; there is no last-writer-wins
mutation. The active version cannot be revoked. Operators must activate a
replacement first, prove it, and then revoke the retired version.

`revoke` means “permanently ineligible through this UniRouter contract.” It
does not imply physical backend destruction. Destruction, recovery windows and
provider-specific disable/delete semantics remain backend/operator policy and
are deliberately outside B4.

## 4. Worker And Outage Semantics

A worker first pins a `Binding`. Staging another version leaves that binding
valid. Activation or rollback increments the active generation, so an older
binding fails with `ErrStaleBinding`.

`WithMaterial` revalidates the exact binding under a read lock and holds that
boundary through a short callback. Activation cannot race between validation
and material injection. The callback is restricted to injecting bytes into an
already bounded provider request; it must not perform network I/O or re-enter
the backend.

The fixture backend copies material for the callback, overwrites that temporary
copy on return and discards callback error and panic text. A callback failure
or panic returns only `ErrConsumerRejected`, preventing secret-derived text from
escaping.

Backend unavailability fails `stage`, `activate`, `rollback`, `revoke`, `pin`,
`WithMaterial`, metadata snapshot and audit reads closed with
`ErrBackendUnavailable`. It never falls back to a stale version and never
changes revision or generation during the outage.

## 5. Metadata-Only Evidence

No exported result contains a plaintext, ciphertext, backend readback handle or
secret-material digest. Snapshot and audit evidence may be serialized; stage
input and callback material may not.

The deterministic fixture
`internal/secretref/testdata/rotation-metadata.golden.json` covers:

1. stage and activate `v1`;
2. stage and activate `v2`;
3. stale `v1` worker rejection;
4. rollback to `v1`;
5. stale `v2` worker rejection;
6. revoke retired `v2`;
7. metadata-only snapshot and six ordered audit receipts.

It intentionally contains no credential value.

## 6. Production Adapter Requirements

A future selected backend adapter must preserve this contract even where the
provider uses different primitives. For example, it may map active generation
to a staging label, alias, enabled version or an adapter-owned CAS pointer. It
must still:

- bind an exact immutable backend version;
- use compare-and-swap or an equivalent conflict guard;
- reject stale worker bindings before material injection;
- fail closed on backend outage;
- separate logical revocation from physical destruction;
- emit only metadata-safe receipts;
- pass the same lifecycle, concurrency, outage and disclosure tests.

The first production backend, its identity/workload credentials, operator,
recovery policy and rotation SLO remain the explicit `P7-D01` User decision.
Until that decision, B4 does not wire `channel_keys`, the Gateway Relay or the
OTLP exporter to this interface.

## 7. Primary Backend Semantics Reviewed

The portable contract uses only the overlap supported by major backend models:

- HashiCorp Vault KV v2 versions, check-and-set and distinct soft-delete versus
  destroy behavior:
  <https://developer.hashicorp.com/vault/docs/secrets/kv/kv-v2>
- AWS Secrets Manager version staging-label movement:
  <https://docs.aws.amazon.com/secretsmanager/latest/apireference/API_UpdateSecretVersionStage.html>
- Google Secret Manager enable/disable/destroy version operations:
  <https://cloud.google.com/secret-manager/docs/reference/rest>
- Azure Key Vault metadata-only version listing:
  <https://learn.microsoft.com/en-us/rest/api/keyvault/secrets/get-secret-versions/get-secret-versions>

These references validate the common abstraction; they do not select or claim
integration with any provider.
