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
