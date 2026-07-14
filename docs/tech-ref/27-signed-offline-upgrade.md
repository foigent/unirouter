# Signed Offline Upgrade Bundle

> Version: v1.1
> Scope: Post-PRD6 V1 customer-local, operator-approved release verification.

## Security Boundary

The V1 bundle proves artifact identity before an operator extracts or executes
anything. It does not silently update a deployment and it does not accept an
arbitrary Admin Console upload. Apply remains a separate backup, maintenance,
migration, image switch and health-check procedure.

Upgrade signing uses a public-key ring independent from License signing. In a
formal release, both rings are compiled into the backend binary:

```text
UNIROUTER_RELEASE_TRUST_MODE=release
UNIROUTER_UPGRADE_TRUST_BUNDLE_B64=<base64url-public-ring-json>
```

The release binary rejects a runtime `UPGRADE_PUBLIC_KEYS` override. Source
development may still use that variable explicitly. Only public keys enter the
customer runtime; a trusted release private key must stay in separately
approved offline/HSM custody. License Studio generates this key separately from
the License key and displays the public build argument without exposing the
private key to the browser.

## Bundle Contract

The deterministic gzip/tar archive contains only:

```text
manifest.json
manifest.sig
payload/<declared regular files>
```

The signed manifest fixes product/schema, key ID, algorithm, version, exact
source commit, OS/architecture, creation/expiry time, monotonic release
sequence and accepted current-sequence window, current/target database schema,
optional provenance SHA-256, and every artifact path/size/SHA-256. When
`provenance_sha256` is present it must equal the declared digest of
`payload/provenance/build-provenance.json`; a free-floating provenance digest
is rejected.

Verification is streaming and bounded (8 GiB payload by default, at most 64
payload files). It rejects untrusted keys/signatures, tampering, wrong product
or platform, expired/future metadata, rollback/replay, incompatible schema,
duplicate/undeclared/missing entries, path traversal, absolute paths,
backslashes, symlinks/devices/non-regular files, hash/size mismatch, and
trailing data or another gzip stream. Verification does not extract files.

## Vendor Pack And Independent Verify

Use a release key pair separate from the License key pair. The existing keygen
creates generic Ed25519 material; using a distinct path and key ID preserves
the trust separation.

```bash
go build -o ./bin/uniroute-license ./cmd/uniroute-license
go build -o ./bin/uniroute-upgrade ./cmd/uniroute-upgrade

./bin/uniroute-license keygen \
  --key-id upgrade-2026-a \
  --private-out /approved-offline-custody/upgrade-2026-a.private \
  --public-out ./upgrade-public-keys.json

./bin/uniroute-upgrade pack \
  --key-id upgrade-2026-a \
  --private-key /approved-offline-custody/upgrade-2026-a.private \
  --release-sequence 1 \
  --version 1.0.0 \
  --commit <exact-40-character-commit> \
  --created-at 2026-07-15T00:00:00Z \
  --expires-at 2026-07-22T00:00:00Z \
  --target-os linux \
  --target-arch amd64 \
  --min-current-sequence 0 \
  --max-current-sequence 0 \
  --min-current-schema 000036_v1_micro_precision_ledger \
  --target-schema 000037_v1_commercial_release_control \
  --provenance-sha256 <sha256-of-build-provenance> \
  --artifact payload/images/unirouter-backend.oci.tar=/release/unirouter-backend.oci.tar \
  --artifact payload/images/unirouter-frontend.oci.tar=/release/unirouter-frontend.oci.tar \
  --artifact payload/images/postgres.oci.tar=/release/postgres.oci.tar \
  --artifact payload/images/redis.oci.tar=/release/redis.oci.tar \
  --artifact payload/images/prometheus.oci.tar=/release/prometheus.oci.tar \
  --artifact payload/images/grafana.oci.tar=/release/grafana.oci.tar \
  --artifact payload/deploy/docker-compose.yml=deploy/customer/docker-compose.yml \
  --artifact payload/deploy/.env.example=deploy/customer/.env.example \
  --artifact payload/deploy/config/prometheus.yml=infra/prometheus/prometheus.yml \
  --artifact payload/deploy/config/grafana/provisioning/datasources/datasource.yml=infra/grafana/provisioning/datasources/datasource.yml \
  --artifact payload/deploy/config/grafana/provisioning/dashboards/dashboards.yml=infra/grafana/provisioning/dashboards/dashboards.yml \
  --artifact payload/deploy/config/grafana/dashboards/unirouter-overview.json=infra/grafana/dashboards/unirouter-overview.json \
  --artifact payload/release-notes.md=/release/release-notes.md \
  --artifact payload/sbom/backend.spdx.json=/release/backend.spdx.json \
  --artifact payload/provenance/build-provenance.json=/release/build-provenance.json \
  --artifact payload/migrations/000037.up.sql=migrations/000037_v1_commercial_release_control.up.sql \
  --out ./unirouter-1.0.0-linux-amd64.tar.gz

./bin/uniroute-upgrade verify \
  --public-keys ./upgrade-public-keys.json \
  --package ./unirouter-1.0.0-linux-amd64.tar.gz \
  --target-os linux \
  --target-arch amd64 \
  --current-sequence 0 \
  --current-schema 000036_v1_micro_precision_ledger
```

The generated `upgrade-public-keys.json` is the directly consumable
single-key ring. Upgrade and License key IDs/files must remain separate.

Outputs use no-overwrite semantics. Repeating a pack command cannot silently
replace an existing bundle.

The signed release note records every OCI image digest used by the customer
Compose file. A fully offline delivery includes all six image archives shown
above; an already-installed dependency may be omitted only when the release
note explicitly names its approved preloaded digest.

## Customer Runtime Procedure

The running binary reads the actual database sequence/schema and its configured
upgrade public-key ring:

```bash
./uniroute upgrade status
./uniroute upgrade verify --package /approved-read-only/unirouter-1.0.0-linux-amd64.tar.gz
```

After the exact verified artifacts have been applied through the approved
maintenance procedure, migrations have reached the signed target, and the new
runtime is healthy, run from the new binary:

```bash
./uniroute upgrade record-applied \
  --package /approved-read-only/unirouter-1.0.0-linux-amd64.tar.gz \
  --recorded-by <operator-or-deployment-identity>
```

`record-applied` re-verifies the complete package under a serializable
transaction and release-state advisory lock. It refuses unless the running
version/commit and current database schema exactly match the signed manifest,
then atomically records package SHA-256, trusted key, sequence, manifest and
operator in `release_state`/`release_history`. A replay or rollback package is
subsequently rejected.

Migration `000037_v1_commercial_release_control` refuses down migration after
signed release history exists. Evidence-bearing or incompatible migrations are
roll-forward recovery, not an automatic image rollback claim.

V1 uses TUF-inspired signed target metadata, hash/size checks and rollback/
freeze resistance, plus an optional SLSA provenance digest. It does not claim
full TUF repository or SLSA build-level conformance.
