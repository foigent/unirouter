# Existing-target rollout and recovery convergence

Active delivery: close RC-01 known-abort ingress recovery, preserve RC-02 HTTP/2
diagnosis and RC-03 loaded-backend/cancellation acceptance, and replace the
version-specific development/test rollout wrappers with one bounded repository
tool. Max first-model delivery has finished on unchanged 1.2.65 images; this
platform delivery must not be counted as a Max adaptation rebuild.

Baseline is main `efde443db214b2984c25401c68b2e4b79e67bffc` (1.2.65/M114).
The existing temporary wrapper `/private/tmp/unirouter-contract65-roll-20260907.py`
already composes the formal `runtime-ingress.py` owner, Docker Compose, storage
gate, database backup and readback. Its hardcoded versions, prior images,
publication counts and separate resume programs are the operational debt being
replaced. The retained failed roll65 evidence remains a failed run.

## Scope and authority

Add `scripts/internal-target-rollout.py` as the narrow companion to
`internal-target-build-images.sh`. It operates an already configured single-host
development/test Compose target with stable/canary/backend and one frontend.
It calls the existing ingress writer; it does not write membership, Slot,
Publication, schema, credential or financial tables. It does not install a host,
fetch source, build/pull images, run migrations, delete backups/images or apply a
rollback automatically. The root Docker lifecycle lock is acquired first, then the exact ingress directory lock. Both remain held across the fixed rollout. The tool imports the existing ingress functions under that held directory lock; it does not call a child CLI that reacquires it or add an unchecked already-locked flag. Standalone ingress CLI keeps acquiring the same directory lock. This prevents rejoin between successful withdrawal and stop/create. Child CLI processes inherit held locks, but their exit cannot prove a Docker daemon operation ended. A separate durable command marker precedes each stop/create; an unconfirmed command is never reissued from old/absent state. Resume can close it only by exact post-state readback, otherwise it refuses replay.
The CLI will refuse production and unrecognized topology/configuration deltas.

One explicit input document contains the Target identity (hostname and database
system ID), environment, Compose project/env-file and exact five-file old/new
chains, current/desired source commit/tree/version, exact local component image
tags and digests, formal runtime overlay path, ingress carrier/container ID,
public HTTPS URL and a unique evidence directory/operation ID. Unknown/duplicate
fields, unsafe identities, symlinks and path aliasing fail before write. Input
contains no secret values; `.env` is consumed only by Compose and effective
configuration is hashed in memory rather than printed. Compose subprocesses receive a fixed minimal environment plus exact image references, with no inherited interpolation values. The old rendered service is checked against each actual container: image defaults plus configured environment, mounts, published ports, entrypoint/command, user/workdir, restart/health/logging/resource/security settings and runtime environment. An input development label cannot override an actual production runtime. Before create, recheck local image identity and pass --pull never. No model IDs/counts or
version-specific strings are embedded in the executable.

## Plan, prepare, apply, inspect and resume

`plan` is read-only, including no evidence directory, lock file, database or
Docker mutation. Verify exact clean source trees, existing OCI labels/digests,
actual container IDs/chains, same schema, lease/support and stop grace. Produce
the bounded input/plan identity and before/after effective configuration digests.
The permitted delta is component image/runtime build metadata and the source
root of unchanged migration/monitoring bind mounts. All other environment,
port, volume, TLS and service settings must remain equal. The runtime overlay
update uses its exact prior values with unique occurrences, not date/version
guessing. A build with a new typed implementation may add support; it may not
lose requirements of an active Release. Existing owner join checks remain final.

`prepare` rechecks the same plan, runs the existing storage gate, writes immutable
plan/before/old-new overlay files, a verified pg_dump and an inspectable recovery
reference. These writes create no runtime change. `apply` requires those exact
bytes and fresh predecessor checks before changing the overlay atomically.

Fixed procedure, with immutable per-stage result files:

1. Withdraw canary through a fresh formal ingress Plan and exact change ID;
   wait for old proxy workers, then stop its exact old container and verify
   exit/drain logs. Recreate only that service with local exact images and
   `--no-build --no-deps`; verify new identity, readiness, schema, support and
   chain; rejoin through the formal owner.
2. Perform the symmetric stable procedure with a healthy surviving canary.
3. Replace frontend once, verify its served index, perform the existing formal
   ingress refresh for Docker DNS and verify external HTTPS index equality.
4. Verify protected schema/Publication/Release/Slot projection digests and exact
   component identities. Keep bounded unauthenticated HTTP/1.1 and HTTP/2 request
   observations; any failed sample is a failed acceptance gate, even if healthy
   readback succeeds. Do not hide failures by replacing the evidence file or
   rerunning the observer until green.

`inspect` never mutates. It compares journal stages with current container IDs,
image labels, overlay bytes and exact ingress receipts and reports the next
eligible fixed stage or a specific drift/ambiguity. `resume` uses that same
inspection and operation ID. It may complete a previously pending ingress via
the existing owner's exact recovery. It must never recreate a already-proven
new container or stop an unknown one. A lost result after Docker stop/create
is classified by actual container identity/image/configuration and readiness;
if those facts cannot uniquely establish the stage, refuse before mutation.
The completed-stage prefix binds the exact current overlay and last applied or uniquely pending ingress head, receipt, carrier bytes and receiver set. A completed operation requires the new overlay and final applied membership; historical head membership is insufficient. No arbitrary command graph, job queue, generic retry system or new DB ledger.

A command-start marker is written before a side effect; a result marker follows
verified readback. The journal describes this one host operation and never
supersedes the database ingress receipt or actual Docker state. Evidence writes
are exclusive, durable and protected from path/symlink substitution. A resumed
operation appends a new attempt record; it preserves every failed observation. An observer window-start marker precedes mutation and each sample is durably appended; a matching end marker proves coverage. Missing/corrupt/truncated records or a missing end marker mean an observation gap. Resume may finish deployment but cannot restore that run to passing continuity. Reports separate deployment completion from observer acceptance and from the independent paid stream/cancellation acceptance.
Failure diagnostics expose only controlled stage, change ID, exit status and
known SQLSTATE; raw environment/SQL/provider text must not enter reports.

## Known serialization abort

The existing RC-01 pre-code review applies: Prepare/Complete run the entire
database transaction again only for PostgreSQL `40001`, with the same input/ID,
a five-second context bound within the existing thirty-second CLI call, and a
context-aware pause. Every try rereads predecessor, membership, lease and support.
An ambiguous commit/connection error, external reload or Provider request never
enters this recovery. Heartbeats remain outside the long compatibility lock.
Tests reproduce actual heartbeat commits after the owner's Repeatable Read
snapshot, including rejection if recovered state becomes ineligible.

## Validation and finite retirement

Offline tool tests use faithful Docker/owner/configuration observations to cover
read-only Plan/inspect, unexpected delta, stale identity, every stop/create crash
boundary, pending/applied ingress recovery, failed drain, unsupported generation,
tampered evidence and an earlier failed observer retained through resume.
No fake Target test substitutes for actual rollout. Existing Go race/integration
tests cover the shared owner; one independent closing review follows this exact
design plus the earlier RC-01/02/03 review and actual final diff.

Target acceptance captures a real ongoing stream on the exact generation to be
withdrawn, starts withdrawal while that stream remains active, observes worker
wait and stream/usage/settlement completion, then confirms that backend stopped.
Test cancellation separately with accurate partial-delivery/ambiguous-charge
semantics and no second Provider dispatch. HTTP/2 diagnosis uses unauthenticated
uniquely tagged requests, actual ALPN/stream/GOAWAY/RST evidence and corresponding
server logs. A failed curl16 remains a failure until its cause and handling are
established; no retry policy or protocol downgrade masks it.

After this fixed procedure passes, subsequent development/test rollouts use the
repository tool and parameter document. The old version-specific wrappers become
historical evidence only; do not delete them or old backups. No supported Artifact,
history reader, legacy capability writer or rollback promise retires in this slice.
Multi-host orchestration, HA frontend/database and automatic business rollback
are outside this single-host contract.

Pre-code review 2026-09-07: OR-01 (actual container/config environment and no-pull), OR-02 (continuous shared ingress lock), OR-03 (durable observation gaps) accepted and incorporated before tool implementation. RC-01/02/03 prior adjudications remain unchanged. One closing review will bind both objects and the final diff.
