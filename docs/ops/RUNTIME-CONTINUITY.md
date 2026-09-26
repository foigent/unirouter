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
bounded Artifact procedure, with the finite counter transitions below, replaces version-specific rollout/resume wrappers.
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
are image/runtime metadata and source paths for unchanged schema/monitoring mounts,
except for the explicitly selected counter transitions below.
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

After a completed backend rollout, retire each replaced **process instance ID**
before expanding beyond an old image's supported schema head. The installed
1.5.2 or later owner provides the narrow `runtime-generation stop-check` and
`stop-apply` commands through the formal host wrapper:

```bash
python3 scripts/runtime-generation-retire.py check --rollout-input /absolute/rollout-input.json --role canary
python3 scripts/runtime-generation-retire.py apply --rollout-input /absolute/rollout-input.json --role canary
python3 scripts/runtime-generation-retire.py check --rollout-input /absolute/rollout-input.json --role stable
python3 scripts/runtime-generation-retire.py apply --rollout-input /absolute/rollout-input.json --role stable
```

Use the input and evidence directory from the exact rollout that replaced the
instance. The wrapper acquires the existing lifecycle and ingress locks, checks
the retained Plan, withdrawal/stop/replacement/rejoin/verify receipts, the old
container's absence, current host process inventory and applied ingress history.
The owner independently checks the exact Target, immutable historical manifest,
row revision, expired lease, absent ingress and every associated attempt's
financial terminal/reservation closure. `check` writes no DB or evidence state;
`apply` repeats these predicates and changes only state/revision/updated_at.
An unknown commit requires exact row readback before another action. Missing
historical exit evidence leaves that identity unretired; lease expiry alone is
insufficient. The old image and manifest remain available as recovery artifacts;
only the exited process instance is permanently stopped. This command does not
run automatically on lease expiry or during Docker stop, and it never changes
schema, ingress, traffic or container state.
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


## Versioned stream counter: finite M115 bridge

The1.2.73 HTTP bridge supports M115, but its runtime-generation CLI incorrectly
required M114. Use the corrected1.2.74 owner before any post-M115 ingress change.
For this forward recovery, the ordinary rollout input may explicitly add
`"runtime_owner":"artifact_cli"`. Keep the default v2 writer for the Artifact
rollout, then use the ordinary same-image writer transition. Do not combine these
actions or downgrade M115 to escape the old control guard.

The selected carrier uses the exact candidate backend image and the same
runtime-generation inspect/read/prepare/complete owner. It takes the existing
lifecycle/ingress locks, including for plan/inspect. Those commands remain
data-neutral but create/remove a private, bounded CLI container in this mode;
default plan/inspect remain fully read-only. The five installed configuration
files supply connection settings without copying/printing credentials. Only four
Artifact metadata fields are overridden; typed JSON is the CLI's sole stdin.

Each helper starts `sleep 90` with no service ports, aliases, dependency startup
or image pulls. Before a database command, its exact ID, image, effective config
and remaining lifetime are verified. The CLI has a20-second context deadline;
the host uses30seconds and an explicit five-second helper stop, followed by exit
confirmation. No existing helper name is adopted. On an unknown creation/command
result, preserve the failed invocation, inspect its exact container and the
existing ingress receipt, and continue only through the formal inspection/resume
path. A missing container never proves that an ingress commit did not occur.
No persistent operator, second writer, schema change or Provider call is added.

`counter_v3` is a marked estimate of successfully delivered extracted text, not a
supplier tokenizer or a bill cap. Cumulative integer rounding removes dependence
on valid text frame partitioning. Input estimation and current rates are unchanged.
A stream lease fixes its estimator version; its checkpoints inherit that identity.
Both recovery owners use retained totals and the immutable pin, never current
writer settings or re-estimated historical text. Provider-final usage and output
holds retain their existing authority. Retain the v2 implementation while historical
leases/receipts and supported rollback require it.

The bridge Artifact supports the complete named M113/M114/M115 ledger matrix and
both counter/recovery implementations. `STREAM_COUNTER_VERSION` defaults to
`unirouter.stream-counter.v2`; the only other value is
`unirouter.stream-counter.v3`, which requires M115 before serving. A bridge on
M113 cannot claim background financial recovery until M114 registration succeeds.
After lease loss no new recovery claim is allowed; already-owned work is fenced.

For an authorized development/test Target already on M114:

1. Build and verify one exact bridge Artifact with both implementations. Add
   `transition: {"kind":"stream_counter_expand_carrier","up_sha256":"<exact 64-hex>","down_sha256":"<exact 64-hex>"}`
   to the normal rollout input. The owner requires the complete real M114 ledger,
   identical historical migration bytes and precisely the two reviewed M115 files.
   Run its normal plan/prepare/apply sequence; it does not execute DDL. Keep v2.
2. Confirm both receivers use that bridge and all old backend processes stopped.
   Use `compatible-expand.py` with `--target-version 000115_stream_counter_version`
   and the existing exact hostname, database ID, ingress/config/container and
   `--migration-digest sha256:<up-file-sha256>` flags. Without `--apply` it checks
   only. The same Docker lifecycle and ingress locks protect host process inventory;
   every actual `uniroute` process must belong to the two exact bridge containers.
   Unknown/unreadable actors and old proxy workers reject the change. Database
   checks cover all ingress-associated or unexpired recovery actors under the
   compatibility lock. Expired rows alone are not proof that a process stopped.
3. After the verified backup/storage gate and successful check, run that exact
   command with `--apply`. The existing schema owner atomically commits M115 and
   its ledger; it does not repeat the M114 generation bootstrap. On timeout, read
   the complete ledger and immutable result before any retry. Release, Slot,
   capability projection and existing checkpoint values must be unchanged.
   M115 takes its control/financial DDL locks without waiting; SQLSTATE `55P03`
   means the migration yielded to an existing owner and rolled back. Read back
   the unchanged schema before another explicit attempt after contention ends.
   Do not retry automatically or stop in-flight settlement to force the expand.
   A successful expand still briefly holds exclusive locks; no zero-wait promise
   follows from this safety gate.
4. Re-read both healthy bridge actors on M115. Use a new ordinary rollout input
   with identical source/images and
   `transition: {"kind":"stream_counter_writer","from":"unirouter.stream-counter.v2","to":"unirouter.stream-counter.v3"}`.
   Only the shared writer environment key may change. Run normal
   plan/prepare/apply, including storage, backup, explicit withdrawal, old proxy
   wait, drain and qualified-client observer. Existing in-flight v2 remains v2.

Configuration rollback uses the same Artifact and reverse writer transition. It
only changes new requests; that bridge continues recovering retained v3 leases.
Do not roll back to 1.2.72 or an Artifact lacking M115/v3 recovery. Guarded M115
`down` is maintenance-only and refuses potential live actors, future schema heads
or any v3 lease/usage history; it never deletes or rewrites that history. Ordinary
rollout cannot apply schema down. Preserve original failed observer/recovery
records and use the existing inspect/resume commands, not an alternate writer.

Report Artifact rollout, schema expansion, writer activation and bounded real
cancellation separately. A successful short-request observer does not re-prove
loaded-stream continuity or arbitrary client support. Paid tests use remaining
cumulative model budgets; local injected crash/commit ambiguity tests are distinct
from supplier evidence.
