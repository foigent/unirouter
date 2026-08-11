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

Before first start:

1. verify the complete signed release with the separately bootstrapped
   `uniroute-upgrade` verifier and vendor-published public-key ring;
2. copy `.env.example` to `.env`, replace every placeholder (including the
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
7. start the stack and, before admitting customer traffic, copy that same
   package into the healthy backend, re-verify it with the running release
   binary and record the initial installed release:

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
   value-bearing `--password` before database access. After rotation, import
   the customer-bound V1 License through the local platform-admin recovery
   surface.

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
