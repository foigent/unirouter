# Customer Runtime Template

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
build version/commit and vendor License/upgrade public trust roots. Runtime
environment variables cannot replace those embedded roots. A stable deployment
identity remains mandatory configuration.

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
7. with customer Gateway traffic still blocked, start the full legacy
   compatibility stack and copy that same package into the healthy backend,
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
   Provider/Feed credential, application key, policy or traffic. Import the
   customer's customer-bound V1 License through the local
   platform-admin recovery surface, then create the single reviewed customer
   project with `slug=default` (the canonical restart reuses this project and
   must not consume a second active-project License seat), provider
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
- SHA-256: `e09d8a3897c40c8efc24d81fdba50bab7af179c07c3e8b3ae018305d88a3e6c7`
- Bytes: `2084`

After every supported SQL restore, while all database readers/writers are still
stopped, run this helper with `psql -v ON_ERROR_STOP=1 -f` before restart. It retains
the former coverage epoch and records the database restore boundary; it does not
change money, credentials, source identity or A/B cursors. Any helper failure
stops restoration validation. The backend then verifies the exact retained schema
and writer proof before enabling D. Config preflight alone is not readiness.
Pre-D backups without M118 keep their original recovery path; D cannot be enabled
until normal M118 expansion and a fresh certification have completed.
