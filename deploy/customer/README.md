# Customer Runtime Template

Explicitly non-expiring packages use signed `unirouter.upgrade-manifest.v2` with `expires_at: "never"` (UniRouter 1.14.0+). The standalone verifier and backend must both support v2, including `upgrade record-applied`. Existing finite v1 packages retain their original deadline; replacing or re-signing them cannot resume a plan bound to a different package hash. Every check/install/resume still verifies the original signature, hashes, creation time (with the existing five-minute future tolerance), platform, schema and release sequence. Non-expiring does not authorize rollback, skip trust checks or extend the owner's finite signing-authority window. This source change is not a published package or a verified deployment.

Current 1.13.0 delivery requires the M127 maintenance/fresh-install contract. See
[connection communication controls](../../docs/ops/XINSIGHT-COMMUNICATION.md) for manual defaults, migration and recovery. Earlier M126 candidate notes below remain historical.

This directory is an input to the signed customer release, not a source-build
deployment. The shipped copy must be declared by path, size and SHA-256 in the
signed upgrade manifest together with prebuilt application and dependency
image artifacts (or explicitly approved preloaded dependency digests),
monitoring configuration, release notes and SBOM/provenance files.
Every runtime image variable is mandatory and must use a digest identity; this
template has no mutable image-tag fallback. `preflight.sh` verifies the effective
release-secret floor, rendered shape and digest syntax. Only verification of the
signed release manifest proves that those digests are vendor-approved.

The Compose file contains no `build:` entry, source mount, builder cache or
host migration mount. The backend image owns its exact migrations, immutable
build version/commit and the embedded upgrade public trust root. Runtime
environment variables cannot replace that root. Commercial licenses and
LICENSE_* settings are not required by the 1.10.0 candidate.

For the 1.10.0 source candidate, schema installation uses the same backend
image's explicit `fresh-install` owner through M126 before the runtime starts.
The [Linux installer preview](../../docs/community/install-linux.md) automates
the private installation steps below. Local database fresh/resume/restore
checks passed; actual Docker installation and the public release remain pending.
This template is not evidence of a signed release or successful target run.

`MODEL_ROUTE_AUTHORITY` is a process-level cutover gate. The binary and the
Compose fallback default to `canonical`, with
`MODEL_ROUTE_COMPATIBILITY_ENABLED=false`. The formal fresh-install
`.env.example` deliberately overrides that fallback with `legacy` plus an
explicit compatibility `true`: a newly migrated database has only the partner
platform shell, not the root customer or canonical route inventory that the
canonical startup gate must prove. Canonical therefore fails before HTTP bind
until bootstrap and authoring are complete; there is no bootstrap exception in
the database truth or startup gate.

The initial legacy process is a full runtime, not a bootstrap-only listener.
Deployment orchestration must keep customer Gateway traffic blocked while the
platform administrator creates the sole customer root and canonical
deployment/release/slot. The separate compatibility switch is strict:
`legacy` and `verify` require an explicit `true`, while `canonical` rejects
`true`. For either non-legacy mode the binary performs a read-only PostgreSQL
authority check after migrations and single-customer baseline resolution but
before Redis, background workers or HTTP bind. The check covers the sole root
customer, every configured live legacy model and current stable/canary slot,
sealed release manifests, operations, connections, deployments, credential
bindings and their latest revision-bound probes. There is no automatic
fallback among modes, and changing the environment variable alone is not an
approved cutover.

Prepare verify authority and inspect the exact customer explicitly while the
current process is still in the approved maintenance window:

```bash
./uniroute model-route-authority apply --customer-org-id <customer-org-id>
./uniroute model-route-authority check --customer-org-id <customer-org-id>
```

`apply` is accepted only when `MODEL_ROUTE_AUTHORITY=verify` and every current
assignment is still v1; it calls the formal idempotent legacy reconciliation
and compatibility seal paths. Once any v2 release is current, apply fails
before projection writes and the operator uses read-only `check`. It does not
change `MODEL_ROUTE_AUTHORITY`, publish a v2 release or move a slot. In
`canonical`, use `check` only: apply is rejected before database access so
legacy state can never overwrite canonical pause, revoke or configuration.
Fresh installs author and activate an initial canonical v2 release under the
traffic-blocked legacy process, then run a one-off canonical `check`; they do
not use legacy reconciliation `apply` to manufacture a v1 assignment.
`check` is one Repeatable Read, read-only transaction and does not advance
sequences. Before starting verify/canonical, separately drain every old
binary that can still write legacy model state. A non-legacy process rejects
legacy channel, channel-key, v1 runtime-change and model-endpoint lifecycle
writes with stable HTTP 410 while retaining their read surfaces. Canonical
model-deployment creation/probing remains the supported write path.

The asynchronous Image V1 worker still resolves legacy channels and keys, so
`ASYNC_MEDIA_ENABLED=true` is rejected with canonical authority. It may run
only in an explicitly authorized legacy/verify compatibility process until a
canonical media operation and receipt contract exists.

Before first start:

1. verify the complete signed release with the separately bootstrapped
   `uniroute-upgrade` verifier and vendor-published public-key ring;
2. copy `.env.example` to `.env`, retain its explicit one-time fresh-install
   `legacy` plus compatibility `true`, replace every placeholder (including the
   `JWT_SECRET`, `API_KEY_HASH_SECRET` and deployment-wide notification Webhook
   signing secret with independent values of at least 32 bytes, and
   `ENCRYPTION_KEY` with exactly 32 bytes), keep the file out of
   tickets/evidence, and retain the exact signed image references. Keep the
   dedicated internal network as an IPv4 `/24`; its fixed frontend proxy IP
   must be a usable non-gateway address and its exact `/32` must remain in
   `TRUSTED_PROXY_CIDRS`. Keep `XINSIGHT_CONNECTOR_AGENT_ENABLED=false` until
   the product-owned installation exists. Enabling it requires the completed
   feed, one fixed HTTPS control origin, bounded poll/request timeouts and at
   least one exact-case installation/deployment/binding-digest to `/32` or
   `/128` map. That map is public authority only: control tokens and the
   X-owned receiver bearer must never enter Compose or `.env`;
3. include the signed `config/` tree beside this Compose file;
4. load or pull only the digest-pinned image identities declared by the signed
   release (including PostgreSQL, Redis, Prometheus and Grafana);
5. run `bash ./preflight.sh ./.env`; it must reject weak, incorrectly sized or
   placeholder runtime values, an invalid fixed proxy/network relationship, a
   non-loopback frontend bind, or a trusted-proxy list missing the frontend
   `/32`; it must also render Compose, find no `build:` block and accept exactly
   six digest-pinned images. This is a local configuration check, not a
   substitute for step 1's signed-manifest verification;
6. make the exact independently verified release package available at an
   approved, readable absolute host path;
7. with customer Gateway traffic still blocked, start only PostgreSQL and Redis
   and wait for their existing health checks. On a fresh database, use the
   exact same verified backend image as a one-off installer. Provide a private,
   persistent evidence parent and the image digest verified in step 1; this
   digest is public identity, not a secret. The installer creates its own new
   `publications` directory and never creates a separate serving generation:

   ```bash
   : "${UNIROUTER_INSTALL_EVIDENCE:?Set the approved absolute persistent evidence parent}"
   : "${UNIROUTER_INSTALL_IMAGE_DIGEST:?Set the exact inspected sha256 image digest}"
   test -d "$UNIROUTER_INSTALL_EVIDENCE"
   compose=(docker compose --env-file ./.env -f ./docker-compose.yml)
   "${compose[@]}" up -d postgres redis
   install=("${compose[@]}" run --rm --no-deps \
     --volume "$UNIROUTER_INSTALL_EVIDENCE:/evidence" \
     --env "RUNTIME_IMAGE_DIGEST=$UNIROUTER_INSTALL_IMAGE_DIGEST" backend ./uniroute fresh-install)
   "${install[@]}" plan --out /evidence/install.json
   "${install[@]}" check --plan /evidence/install.json
   "${install[@]}" apply --plan /evidence/install.json --evidence-dir /evidence/publications
   # After an interruption, retain every Plan and use the SAME image and DB:
   # "${install[@]}" resume --plan /evidence/install.json --evidence-dir /evidence/publications
   ```

   `plan/check` do not change the database. An incomplete installation refuses
   ordinary startup and writers. Never replace an original referenced Plan;
   a missing or damaged reference must be restored from the original evidence.
   The existing `install-baseline` command is for nonempty historical restore
   continuation, not fresh installation. Completed DB restores follow the
   existing stopped-writer restore/rebase procedure, preserving their historical
   installation COMMENT; do not rerun fresh certification after restore.

   After the owner reports completed M126 and the retained readback is verified,
   start the full legacy compatibility stack and copy the same package into the healthy backend,
   re-verify it with the running release binary and record the initial
   installed release:

   ```bash
   : "${UNIROUTER_SIGNED_PACKAGE:?Set the absolute path to the exact verified release package}"
   : "${UNIROUTER_RELEASE_OPERATOR:?Set the approved operator or deployment identity}"
   test -r "${UNIROUTER_SIGNED_PACKAGE}"

   compose=(docker compose --env-file ./.env -f ./docker-compose.yml)
   "${compose[@]}" up -d
   "${compose[@]}" exec backend wget -qO- http://localhost:8080/health/ready
   "${compose[@]}" cp "${UNIROUTER_SIGNED_PACKAGE}" backend:/tmp/unirouter-initial-release.tar.gz
   "${compose[@]}" exec backend ./uniroute upgrade record-applied \
     --package /tmp/unirouter-initial-release.tar.gz \
     --recorded-by "${UNIROUTER_RELEASE_OPERATOR}"
   "${compose[@]}" exec backend ./uniroute upgrade status
   "${compose[@]}" exec backend rm -f /tmp/unirouter-initial-release.tar.gz
   ```

   `record-applied` must report the package's non-zero sequence, exact runtime
   version/commit and target schema. Retain the approved host copy and the
   sanitized readback evidence;
8. rotate the seeded administrator from an approved, readable secret file.
   The file must contain exactly one password with at most one trailing LF or
   CRLF and no additional line or bytes. `-T` keeps the automation path
   non-interactive, and no password value enters process arguments or an
   environment variable:

   ```bash
   : "${UNIROUTER_ADMIN_PASSWORD_FILE:?Set the approved administrator password file}"
   test -r "$UNIROUTER_ADMIN_PASSWORD_FILE"
   docker compose --env-file ./.env -f ./docker-compose.yml exec -T backend \
     ./uniroute admin set-password \
     --username admin \
     --org-id 1 \
     --password-stdin < "$UNIROUTER_ADMIN_PASSWORD_FILE"
   ```

   An operator at an approved interactive TTY may instead omit
   `--password-stdin`; the CLI prompts twice without echo. It rejects the old
   value-bearing `--password` before database access;
9. log in once as the rotated local administrator, then use
   `POST /api/v1/bootstrap/first-customer` through the authenticated
   platform-admin surface to create exactly one root customer. The endpoint
   accepts either the exact strict post-reset shell or the exact current-
   migration fresh seed. In this formal `production` flow, the fresh checkpoint
   also requires the unique non-zero `release_state` and matching
   `release_history` written in step 7, with the current target schema. The one
   logical login audit may still be a durable pending Outbox event or may already
   be materialized in `audit_logs`; a linked materialized event is not counted
   twice.

   After every invariant matches, the same `SERIALIZABLE` transaction deletes
   only the exact seeded partner zero wallet, `Default Project`, and optional
   default-admin `project_admin` membership before creating the active customer
   and its zero wallet. Any extra row, field drift, missing/mismatched release
   evidence, or concurrent change returns `409` and rolls back all normalization
   and creation. The endpoint does not create a customer user, project,
   Provider/Feed credential, application key, policy or traffic. Create the
   single reviewed customer project with `slug=default` (the canonical restart
   reuses this project), provider
   connection/credential, canonical model deployment and operation evidence,
   sealed v2 release and initial stable slot. Admit no customer Gateway traffic
   during this phase; only separately authorized bootstrap, authoring and
   provider-probe actions are permitted;
10. prove the exact target authority from the still-running backend with a
    one-off canonical, read-only process environment (replace the customer ID):

    ```bash
    : "${UNIROUTER_CUSTOMER_ORG_ID:?Set the created root customer organization ID}"
    compose=(docker compose --env-file ./.env -f ./docker-compose.yml)
    "${compose[@]}" exec \
      -e MODEL_ROUTE_AUTHORITY=canonical \
      -e MODEL_ROUTE_COMPATIBILITY_ENABLED=false \
      -e ASYNC_MEDIA_ENABLED=false \
      backend ./uniroute model-route-authority check \
      --customer-org-id "${UNIROUTER_CUSTOMER_ORG_ID}"
    ```

    Require `ready: true`, retain the secret-free report, and confirm that no
    other legacy writer process exists. Stop the backend, change `.env` to
    `MODEL_ROUTE_AUTHORITY=canonical` and
    `MODEL_ROUTE_COMPATIBILITY_ENABLED=false`, rerun
    `bash ./preflight.sh ./.env`, recreate the backend and require its readiness
    check to pass. Only that canonical restart may admit customer Gateway
    traffic. A failed check or restart leaves traffic blocked and must not be
    bypassed by returning to the sample values.

The formal customer HTTP bind remains `127.0.0.1:3000`; expose it through an
approved TLS reverse proxy. PostgreSQL, Redis, Prometheus and Grafana have no
host port in this template.

An application-binary rollback to a release that predates purpose-scoped API
keys must not start until X-Insight polling is stopped, the exact
`xinsight.feed.read` key is terminally revoked through the dedicated
X-Insight connection control, and the
current backend reports:

```bash
compose=(docker compose --env-file ./.env -f ./docker-compose.yml)
"${compose[@]}" exec backend ./uniroute upgrade preflight-legacy-rollback
```

`rollback_preflight=pass` and `active_purpose_keys=0` are required before the
image switch. Serving and staged feed keys use internal `feed_active` and
`feed_staged` statuses that old binaries reject, so omission remains
fail-closed; the preflight is still the supported operational order. Binary
rollback is supported after key revocation. Migration `000043` schema down
additionally requires removal of
purpose credentials and retained feed state and is not part of a routine
binary rollback.

The backend ignores `X-Forwarded-For` and `X-Real-IP` unless its direct peer is
in `TRUSTED_PROXY_CIDRS`. It then walks `X-Forwarded-For` from right to left and
uses the first untrusted hop. The template gives frontend Nginx a deterministic
internal address and trusts that exact `/32`; the default gateway `/32` covers
an approved TLS proxy running on the same host. If the TLS proxy is elsewhere,
replace or extend the gateway entry with only the actual proxy hop CIDR as seen
by frontend Nginx. The TLS proxy must overwrite an inbound client-supplied
`X-Forwarded-For` or append its verified network peer; it must not pass the
header through unchanged. Do not use `0.0.0.0/0` or `::/0` as trusted proxy
ranges. Login throttling and API-key IP allowlists share this resolver.

## Optional source observations and budget snapshots

M118 currently has no supported successful `reset` lifecycle. Budget definition,
binding, coverage history and the original journal are preserved; the old reset
scope is incomplete: the CLI can write a read-only diagnostic preview, but that
preview cannot be loaded as an executable plan and apply is refused. Disabling D
capture does not remove this restriction. Historical A/M116 reset evidence
does not establish support for this version. Do not clear metadata or alter the
migration ledger to make reset proceed.

`SOURCE_OBSERVATIONS_ENABLED` and `SOURCE_BUDGET_SNAPSHOTS_ENABLED` default to
`false` and each requires `SOURCE_FACTS_ENABLED=true`. B requires the reviewed
M117 expansion; D requires M118 and original financial-journal writer support.
Every non-stopped generation, including recovery-capable generations, must
advertise the finite schema and writer contracts before expansion. Connection
and credential profiles are explicit; upgrade never expands old credentials.

D snapshots are report-only. Gateway known subtotals exclude calculated prices,
reservations and legacy call-log baselines. Enterprise budgets require an explicit
same-project binding; no organization totals are exposed. M118 initially covers
only the interval beginning at certification. A missing interval prevents a
comparable remaining amount, even when the current journal subtotal is zero.

A customer package supporting D must carry the exact helper below as a declared
artifact with its size and SHA-256 in the signed upgrade manifest, alongside this
restoration instruction. This source template does not itself sign or release it.

- Repository/package path: `scripts/source-budget-restore-rebase.sql`
- SHA-256: `deb0f30c040dd7faa28e177209b2ea7e718bd9eb0ee0686d373b1b39bcec262c`
- Bytes: `12818`

After every supported SQL restore, while all database readers/writers are still
stopped, run this helper with `psql -v ON_ERROR_STOP=1 -f` before restart. It retains
the former D coverage epoch and records the database restore boundary. With the
exact M119, M120, M121, M122, M123, M124, M125 or M126 schema it also changes each C recovery epoch in the same transaction,
invalidating old C cursors and in-flight grants. It does not change money,
credentials, source identity, A/B cursors, C registration, manifests or object
allocation epochs. Any helper failure
stops restoration validation. The backend then verifies the exact retained schema
and writer proof before enabling D. Config preflight alone is not readiness.
Pre-D backups without M118 keep their original recovery path; D cannot be enabled
until normal M118 expansion and a fresh certification have completed.

SQL-only and paired SQL/body restores both require this rebase. Stop every
reader and capture/media/recovery writer that uses the database or body root.
For a paired backup, preserve the original body marker and files during the
same stopped window and verify their digests; do not initialize a new binding.
New authorization may read verified historical bodies. Missing files remain
missing and damaged/binding-mismatched files remain corrupt without rewriting
historical complete manifests. No store configured means source_unavailable.
The helper does not clean files or release quota. C epochs use fresh positive
63-bit random values, not ordered counters; random collisions are extremely
unlikely, and a collision with the current epoch rejects the transaction.

### Content storage initialization

The local `content-store` command runs before service configuration loading; it
requires no database, JWT, provider or license configuration. It never enables
capture, changes policy, creates directories, changes permissions or starts a
service. Supply existing private directories (normally mode 0700), an empty body
root, and a new token-file path. Every path component must be free of symlinks.
Identity is at most 128 UTF-8 bytes, without surrounding whitespace or controls.

```sh
uniroute content-store init --root /data/uniroute/content --identity customer-content --token-file /run/secrets/content-binding
uniroute content-store check --root /data/uniroute/content --identity customer-content --token-file /run/secrets/content-binding
```

`init` exclusively creates the marker and mode-0600 token file, syncing both to
disk. `check` only reads; it makes no probe files or permission changes. Configure
`SOURCE_CONTENT_STORAGE_ROOT`, `SOURCE_CONTENT_STORAGE_IDENTITY` and
`SOURCE_CONTENT_STORAGE_TOKEN_FILE` with these same values. Mount both paths at
the corresponding container paths and keep their permissions private. Only the
separate full-content deployment gate and effective project policy enable body
capture. Ordinary startup verifies the binding and never initializes it.

If initialization reports a partial failure, preserve the marker and any token
file. To recover from a valid existing marker, explicitly export to a **new**
token-file path, then check that path and update the configuration:

```sh
uniroute content-store export-token --root /data/uniroute/content --identity customer-content --token-file /run/secrets/content-binding-recovered
uniroute content-store check --root /data/uniroute/content --identity customer-content --token-file /run/secrets/content-binding-recovered
```

Export preserves the original binding; it never rotates the marker or overwrites
an existing empty, partial or complete token file. It rejects a malformed or
noncanonical marker. Before creating the token, export syncs the verified marker
and its directory to complete any previously unconfirmed initialization.
Output includes action, status and paths, never token bytes.
Preserve both binding files with private permissions in the deployment backup.

Published body expiry uses each object's original retention deadline. With an
existing bound store, the runtime checks at most 1,000 expired ready objects per
minute (30 seconds per batch), including when capture policy is metadata-only or
disabled. It removes only the exact final regular file, durably confirms absence,
then atomically removes its object row and releases stored bytes. Historical
manifests and digests remain; expired reads continue to return 410. Reserved and
orphan objects, temporary files, directories and the binding marker are retained.
Failures keep unreleased accounting for a later verified batch. These scheduling
bounds are not a capacity or cleanup-throughput guarantee. Shutdown cancels and
waits for maintenance before capture shutdown and closing the storage root.
