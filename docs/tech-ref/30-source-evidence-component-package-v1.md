# UniRouter Source evidence component package V1

This product-local contract freezes a bounded set of UniRouter Source facts for
a POC campaign. It is not the cross-product campaign manifest and does not give
UniRouter authority over X-Insight acceptance.

## Claims and boundaries

- `unirouter.source-evidence.request-set.v1` freezes 1–20 tenant/project-scoped
  request identities and the fixed 200-fact ceiling.
- `unirouter.poc-source-facts.v1` is complete only for facts visible for that
  frozen set in one read-only, repeatable-read PostgreSQL snapshot.
- `unirouter.source-evidence.source-scope.v1` is the product-local projection
  of the Source instance already authorized by the neutral campaign. It fixes
  `identity_authority=neutral_campaign_authorization` and
  `runtime_observation=not_claimed`; its canonical digest is
  `source_binding_sha256`. Operator-supplied runtime tree, Source configuration
  digest, and reference mode are not accepted or claimed. The retired
  `unirouter.source-evidence.runtime-readback.v1` schema rejects every input.
- The Source facts require an already recorded signed UniRouter release
  identity (positive sequence, package digest, key ID, exact schema and
  canonical lowercase commit). These facts are read directly from an exactly
  matching `release_state` and `release_history` row. They do not create or
  advance release state. Actual Target Source configuration remains a separate
  pre-TRAFFIC operational readback gate.
- Cursor and high-watermark claims are explicitly outside Source facts. They
  can exist only after BOOTSTRAP establishes the required Source state.
- Each `unirouter.execution-evidence.bundle.v2` compatibility member is an
  honest partial projection. UniRouter V1 has no runtime close marker, so every
  member declares `runtime_close_marker_unavailable`, keeps
  `source_profile_complete=false`, and cannot claim `complete`.
  Its required `closed_at` value is the repeatable-read snapshot cutoff, not a
  claim that runtime execution closure was observed.
- The generator reads durable metadata only. It does not call Gateway,
  Provider, Feed, upstream, or X-Insight endpoints and never includes prompts,
  responses, secrets, raw IPs, or raw error text.
- Actor attribution is limited to the stable categories `human`, `agent`,
  `service`, `api_key`, and `unknown`. Durable `actor_ref` and `on_behalf_of`
  values are never selected or copied. Provider-attempt physical key, Grant,
  Credential, channel, and channel-key locators are likewise never exported.
  A complete logical-credential binding contributes only a domain-separated
  SHA-256 correlation of its canonical high-entropy `cred_*` Source reference;
  a legacy physical-key binding has no credential correlation. Incomplete or
  noncanonical logical bindings fail capture rather than emitting a private
  locator.
- Provider-attempt `failure_kind` is an allowlisted machine classification.
  Known routing/provider classes are retained, known downstream-delivery codes
  collapse to `delivery_failure`, and every other durable value collapses to
  `unclassified`. Arbitrary stored text is never copied into the immutable
  package.

## Signed component package

The producer-owned contract ID is
`unirouter.source-evidence.component-package.v1`. The canonical contract-index
body lists every product-owned schema and its content digest. The separate
portable conformance index binds the exact positive and negative fixture bytes
to that producer-contract digest. The producer contract ID is not the signature
domain: signatures use `unirouter.source-evidence.component.v1`.

The deterministic package contains:

1. canonical `manifest.json`;
2. `manifest.sig` using Ed25519 over
   `unirouter.source-evidence.component.v1\0source_evidence\0` plus the canonical
   manifest;
3. the frozen request set;
4. the authorized Source-scope projection;
5. the bounded Source facts;
6. one deterministic partial compatibility member per request.

The package follows the neutral one-way chain `M -> acknowledgements -> A ->
P -> X -> R`. It binds the already verified neutral artifacts only through
`authorization_manifest_sha256`, `authorization_statement_sha256`,
`authorization_signature_sha256`, and `source_binding_sha256`. Neither M nor A
contains P, X, or R, and no User literal or mutable coordination-ledger digest
is copied into a UniRouter artifact. Neutral authorization signature semantics
remain owned by the product-neutral verifier.

UniRouter pins the immutable N1 release by source/release commit and tree,
release-manifest digest, contract/fixture aggregates, and per-platform binary
digest and size. A workstation path or tag is never authority. The local
adapter snapshots the exact verified binary, invokes it without a shell or
network access, and requires N1 to validate canonical M, both common
acknowledgement envelopes, A, the detached signature and the neutral trust set
before the database capture starts. The N1 whole verifier does not parse the
X-Insight product verification receipt or claim that X runtime state is true;
those remain X-owned facts.

UniRouter separately verifies its own product material before emitting its
neutral acknowledgement:

- `unirouter.release-state-identity.v1` comes from exact durable
  `release_state`/`release_history` readback;
- `unirouter.source-evidence-scope-receipt.v1` binds the canonical request set,
  Source scope, installed release and protocol pin from one read-only
  repeatable-read transaction;
- `unirouter.source-evidence.component-verifier-release.v1` binds the exact
  product verifier binary and producer-contract/N1 pin;
- `unirouter.source-evidence.component-verification-receipt.v1` records the
  product checks and their operation-local zero-effect evidence;
- `foigent.integration-campaign-component-ack.v1` is then checked by N1 against
  M. Its neutral envelope does not replace the product receipt.

The trust set is canonicalized as
`unirouter.source-evidence.trust-set.v1`, has purpose `source_evidence`, domain
`unirouter.source-evidence.component.v1`, and
accepts only `source-evidence-*` operational `signing_key_ref` values. Every
entry also carries the derived cryptographic identity
`signer_public_key_id=ed25519:sha256:<raw-public-key-digest>` and the manifest,
signature, and trust set must agree on both identities and the purpose-and-domain-specific
`trust_set_sha256`. Upgrade trust and key references are not valid
Source-evidence authority, even though the implementation reuses the same
Ed25519 primitives.

The offline verifier checks the package size and paths, canonical manifest and
signature envelopes, dedicated trust set, signature domain, artifact hashes
and sizes, request-set/Source-scope/Source-facts bindings, and exact
deterministic partial projections. It reconstructs the canonical gzip+USTAR
container and requires byte equality, rejecting truncated checksums,
concatenated gzip members, bytes after the tar end, trailing bytes, duplicate
or unsafe paths, and noncanonical archive metadata. A content-addressed
package SHA-256 identifies the resulting bytes.

## Tooling

`uniroute-source-evidence` exposes five operations:

- `validate-request-set` validates a frozen request set and prints its schema,
  semantic digest, canonical Source-scope binding, and request count.
- `prepare` reads the installed release once in a read-only repeatable-read
  transaction and emits canonical installed-release, Source-scope, and scope
  receipts. Cross-domain inputs must use exact product canonical JSON so N1's
  exact-byte hashes and the package's semantic identities cannot diverge.
- `acknowledge` verifies the pinned N1 release and canonical M, then repeats
  the product read-only preparation and requires byte-exact installed-release,
  Source-scope, and scope-receipt equality before it emits the product
  verification receipt and neutral acknowledgement through create-only private
  publication.
- `capture` first revalidates the exact product receipt/acknowledgement and the
  complete M/acks/A/signature/trust chain through the pinned N1 binary. It then
  re-reads the installed release, fails on post-authorization drift, performs
  the bounded database snapshot, builds and independently verifies the package
  in memory, and publishes it through the strict private-output path. Signing
  keys and Target trust provisioning remain later operational inputs.
- `verify` is offline and requires only the package and dedicated public key
  ring. It reports package-signature verification and the authorization hashes
  present in the signed manifest; it does not claim to have re-run N1 unless
  the caller separately supplies the neutral artifacts to the accepted capture
  lifecycle.

The committed conformance material contains a product-compatible canonical M,
both acknowledgements, A/signature/trust, all product receipts, one signed
package, the expected verification projection, and bounded wrong-purpose,
wrong-key, wrong-binding, member tamper/missing/extra/duplicate/path-unsafe,
and trailing-container negatives. Its test-only keys were generated
ephemerally; no private key or seed is retained. Neutral N1's own synthetic
product fixture bodies are never used as UniRouter authority.

This first batch does not add a reset journal, database migration, runtime
close marker, neutral joint-manifest tool, UI, worker, deployment, or traffic
path. The local N1 executable path is a focused-test/runtime input only;
content-addressed durable N1 distribution remains a hard dependency before the
full repository gate, PR, or release can be proposed.
