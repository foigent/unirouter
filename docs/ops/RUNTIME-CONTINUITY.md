# Same-schema runtime continuity

This first slice covers planned changes through the managed Nginx ingress.
It does not cover direct backend debug ports, infrastructure faults, schema
changes, online Capability Publication or arbitrary third-party proxy configs.
An aborted rollout that retains a slow old connection is not a completed rollout.

## Bootstrap

The source canary overlay adds a read-only directory mount at
`/etc/nginx/runtime-membership`; both HTTP and TLS route templates include its
`upstreams.conf`. Keep the directory outside an immutable release checkout when
using the membership writer. Copy the canonical `both` file into a dedicated
operator directory and mount that exact directory with the deployment overlay.
Keep the route template itself immutable. Do not atomically replace a singly
bind-mounted file and assume the container sees new bytes.

Installing the directory mount on an existing proxy can require recreating
Nginx. Use the existing authorized maintenance/bootstrap procedure; this first
installation does not establish uninterrupted continuity. Likewise both backend
processes must first run the version exposing `runtime` in `/health/ready`.
Never change or overwrite an existing Target TLS overlay without checking its
exact effective Compose chain and retaining its pre-state.

Nginx `worker_shutdown_timeout` must be unset/zero. Slow request uploads have no
proven total ingress deadline: the controller times out and retains the old
backend instead of forcibly expiring old workers. Do not enable
`proxy_next_upstream ... non_idempotent` to hide failed publication requests.
Restrict direct debug ports before claiming that ingress membership is the
complete set of possible recipients in a later compatibility activation.

## Plan and change membership

Capture the exact current Nginx container ID with `docker inspect`, then run:

```bash
python3 scripts/runtime-ingress.py \
  --directory /absolute/operator/runtime-membership \
  --expected-container-id FULL_CONTAINER_ID \
  --desired stable
```

Default is read-only: it checks the directory mount, container-visible bytes,
effective Nginx configuration, both backend readiness/schema identities,
Compose stop grace, and old worker PID/start identities. It emits the exact
membership and effective-config predecessor digests. `stable` withdraws canary;
`canary` withdraws stable from both request and health/metrics upstreams; `both`
restores the existing 9:1 backend weights and stable health upstream.

Apply the exact plan with the same arguments plus:

```bash
  --apply \
  --expected-config-sha256 BEFORE_SHA256 \
  --expected-nginx-sha256 NGINX_SHA256
```

The helper locks its exact directory, checks fresh state, atomically writes and
fsyncs membership, reads it inside Nginx, validates syntax and gracefully
reloads. It returns `old_proxy_workers_exited` only after seeing new workers and
the exit of every observed old worker. A syntax failure before reload restores
the original file. Any reload/observation ambiguity retains both backends and
the observed candidate file; it never blindly rolls back traffic or stops a
backend. Reapplying unchanged bytes still performs a freshly observed reload,
so a previously ambiguous attempt cannot be mistaken for withdrawal proof.

Only after this successful withdrawal may the operator use the already verified
exact Compose chain to stop/recreate the withdrawn backend with the exact image.
The survivor must have adequate capacity. Verify the new backend readiness and
same schema before planning `both`. Repeat for the other replica only after the
first is healthy. Preserve image and source identity and restore only compatible
artifacts. The helper deliberately does not derive or execute a Compose command
from an untrusted filename or guessed project.

## Existing development/test rollout and recovery

For an already bootstrapped development/test Target with both components on one
exact source and the formal five-file backend/frontend Compose chain, use
`scripts/internal-target-rollout.py` from the **new exact clean source**. This
bounded Artifact-only procedure replaces version-specific rollout/resume wrappers.
The original `runtime-ingress.py` remains the sole ingress writer and retains its
standalone membership operation. Its lock is held throughout backend stop/create;
child commands inherit the held descriptors if the controller exits unexpectedly.

Build and verify local images through `internal-target-build-images.sh` first.
The rollout tool does not fetch/pull/build, migrate, activate models, clean disk,
bootstrap Nginx or handle a frontend-only mixed-source change. It requires:

- `format`: `unirouter.internal-target-rollout.v1`; unique `operation_id`.
- `target`: exact `hostname`, `database_system_id`, `environment` (development or
  test), `project`, `env_file`, `overlay`, `tls_overlay`, `ingress_directory`,
  `nginx_container_id`, and credential-free HTTPS `public_url`.
- `source`: absolute `directory`, full `commit`, `tree`, and `version`.
- `images`: `backend` and `frontend`, each with exact local `tag` and `digest`.
- `evidence_directory`: a new absolute directory under an existing private parent.

The first three Compose files are `docker-compose.yml`,
`docker-compose.canary.yml`, and `deploy/internal-target-images.override.yml`
under the source; the final two are the explicit TLS and formal runtime overlays.
The existing Nginx container and its distinct retained Compose chain stay intact.
The tool compares actual container configuration with rendered Compose, independent
of labels; it ignores inherited host interpolation. The only permitted changes
are image/runtime metadata and source paths for unchanged schema/monitoring mounts.
Environment values are compared in memory and never printed into the Plan.

With a reviewed non-secret input file, run each action using the same file:

```bash
python3 scripts/internal-target-rollout.py plan --input /absolute/rollout-input.json
python3 scripts/internal-target-rollout.py prepare --input /absolute/rollout-input.json
python3 scripts/internal-target-rollout.py apply --input /absolute/rollout-input.json
```

`plan` and `inspect` are read-only. `prepare` records exclusive exact Plan and
private overlay bytes plus a verified database backup, without changing runtime.
If `apply` fails, preserve its output and all evidence, then use:

```bash
python3 scripts/internal-target-rollout.py inspect --input /absolute/rollout-input.json
python3 scripts/internal-target-rollout.py resume --input /absolute/rollout-input.json
```

Resume verifies actual container/image/source/configuration, protected ledger
projections and the existing ingress receipt. It keeps the original operation ID
and never recreates an already verified replacement. A different ingress head,
changed execution state or uncertain stop/create prevents further mutation. Docker
may continue a daemon operation after its CLI times out: an existing command
marker with old/absent state is unconfirmed and is never automatically reissued.
Readback may close an exact finished post-state; it cannot infer that no command
was sent merely because the old container still runs.
A preparation failure has no runtime side effect; retain that incomplete candidate
and use a new operation identity after correcting the prerequisite.

Result fields distinguish `deployment` from `observer_acceptance`. Exit3 means
component delivery completed while a failed/missing observation still prevents
acceptance. Every unauthenticated HTTP1/HTTP2 sample and observation window is
retained through resume; an interrupted window cannot become green on recovery.
`passed_short_requests_only` proves only sampled short requests. Real loaded
backend withdrawal/drain/stop, cancellation, Provider attempts and settlement
remain separate acceptance requirements; the tool does not claim them by health.

New observer windows use `unirouter.ingress-observer.v2`. The already pinned
Nginx container must be running and contain curl 8.19 or newer with HTTPS/HTTP2
support; the qualified development image contains 8.19.0. The tool checks this
before changing runtime state and fails if the client is unavailable. It does
not install/pull a client or fall back to the host version. Initial dual-protocol
samples must also pass before mutation, and client identity is rechecked at exit.

Each fixed, unauthenticated POST runs with curlrc disabled (`-q` first), TLS
verification, no redirects and `--retry 0`; HTTP2 uses strict prior knowledge.
The existing client's standards-defined reconnect for an unprocessed HTTP2 stream
is recorded by connection count. No application or Provider retry is added.
The vantage is the ingress container's network namespace reaching the public
HTTPS URL; it is not an external Internet measurement. A 401 JSON error response
must be complete and bounded to 64 KiB. Client/image identity, body bytes/digest,
protocol and exit code are retained; raw response text and environment values
are not. Nonzero exit, status000, downgrade and incomplete/invalid response fail.

Only the exact historical unversioned receipt shape uses the legacy reader. New
records with missing/unknown versions or mixed fields cannot downgrade to it.
The [client diagnosis and review](../analysis/INGRESS-OBSERVER-CLIENT-20260908.md)
keep old curl failures distinct from this new observer contract.

See the [exact recovery design](../analysis/INTERNAL-TARGET-ROLLOUT-RECOVERY-20260907.md).
Old version-specific wrappers and failed runs remain historical evidence, not
supported authoring paths after this procedure's Target acceptance. No legacy
capability writer, released Artifact, rollback promise or history reader retires.

## Deadlines and shutdown evidence

Readiness reports process `instance`, serving/draining `state`, active handler
count, `schema_ledger_digest`, `request_lifetime_seconds` and
`drain_timeout_seconds`. The schema digest identifies the exact verified ordered
ledger, not runtime implementation compatibility. Readiness alone never removes
Nginx traffic.

The accepted HTTP lifetime covers the maximum configured stream or implemented
typed provider timeout plus 120 seconds for admission/finalization. Effective
drain adds 120 seconds for cancellation and dependency exit and is at least
`SHUTDOWN_TIMEOUT_SECONDS`. Defaults are 1920 and 2040 seconds respectively;
Compose stop grace defaults to 35 minutes. Longer stream configurations require
longer Compose grace; the helper refuses insufficient grace before mutation.

SIGTERM/SIGINT fences new business handlers and makes readiness fail while
accepted handlers retain their contexts. Settlement recovery, Connector and
outbox dependencies stay alive until handlers finish. At the declared deadline,
remaining contexts/connections are canceled and the bounded cleanup period is
used. Logs report incomplete drain and active responsibility; this is not a
successful continuity result. Existing durable settlement/attempt/outbox leases
remain the recovery authority. Never wait for the global outbox to be empty or
automatically resend an ambiguous Provider call.

## Verification boundary

`go test -race ./cmd/uniroute -run '^TestHTTPRuntime'` exercises real loopback
HTTP/SSE flush, admission races, finalization waiting and explicit cancellation.
`PYTHONDONTWRITEBYTECODE=1 python3 scripts/test-runtime-ingress.py` exercises
stale plans, all-upstream withdrawal, validation failure, reload ambiguity,
old-worker delays and unsafe proxy settings. These local tests do not substitute
for a real Target Nginx rollout or Provider/usage/settlement evidence.
