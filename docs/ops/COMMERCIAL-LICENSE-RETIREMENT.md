# Commercial License retirement: 1.10.0 candidate

Status: local candidate. PostgreSQL 16 migration, fresh-install and backup
restore/rebase tests passed; host-controller tests use simulated Docker/NGINX.
Actual Docker/NGINX maintenance acceptance is still required before this becomes
an executable target runbook or an installable release. This document grants no Target or Production
operation authorization.

## Runtime and API contract

The binary does not construct the commercial License runtime, reconcile stored
entitlements, start its refresh loop, or wrap Gateway/admin/OIDC/Agent routes
with commercial gates. Role, tenant/project, credential, rate, budget,
qualification, idempotency, release and upgrade-signature checks remain.

Deprecated endpoints retain platform-admin authorization:
- GET /api/v1/license returns status=not_required; validity, signature and
  enforceability flags are false and no historical payload is returned.
- PUT accepts one bounded strict JSON envelope containing a nonempty document.
  It returns imported=false and status=not_required. It performs no DB read or
  write, entitlement update, import audit or License-warning event.
- /me retains license_access with status=not_required and available feature
  flags. These are not role grants.
- gateway_license readiness is not_applicable without a repair link.
- Deprecated descriptor licensed=true means no commercial restriction. Actual
  availability still requires role, structure and transactional preflight.

LICENSE_* inputs are ignored. A release binary still requires an embedded,
nonempty upgrade verification ring and rejects runtime UPGRADE_PUBLIC_KEYS
overrides. Expected trust readback: license_keys=0, upgrade_keys greater than 0.

Historical signing tools, documents, entitlements, audits and migrations remain
for historical verification. Their presence does not authorize new runtime
commercial enforcement.

## Existing M125 installation

M126 is a maintenance boundary, not a compatible-expand transition.

1. Identify the exact M125 Artifact, complete ledger and environment. Retain
   the database backup, original binary/image, required configuration and
   existing secret custody references; do not copy secrets into evidence.
2. After actual host-controller acceptance and explicit target authorization,
   use the exact M126 Artifact CLI owner to withdraw M125 through explicit empty
   ingress membership, drain workers and stop/retire every writer.
   Resolve pending/ambiguous ingress changes. Retain host/controller evidence
   that processes have exited; expired leases alone do not prove this.
   Existing development/test retirement commands do not authorize Production.
3. With the reviewed new Artifact and migrations, use the existing
   `uniroute migrate up` owner. It checks the entire M125 ledger against the
   artifact while holding the migration session lock. M126 additionally refuses
   non-stopped/non-absent runtime rows and unresolved ingress transitions.
   Ordinary server startup and compatible-expand cannot perform this step.
4. Verify the full M126 ledger and absence of exactly these commercial triggers:
   api_keys_commercial_limit, users_commercial_limit, projects_commercial_limit,
   access_credentials_commercial_limit, provider_connections_commercial_limit.
   Verify retained License/settings/audit history and the original M118 budget
   coverage certificate. Do not recertify historical financial evidence.
5. Start only the new Artifact, verify readiness and permissions, and perform
   the approved synthetic admission and financial checks before opening traffic.
   Real provider calls retain their own explicit scope and allowance.

The development/test controller is `scripts/runtime-generation-retire.py` with
`--maintenance --rollout-input <reviewed-input.json>`. It uses the formal rollout
input (exact host/database/NGINX/source/image identity), requires
`runtime_owner=artifact_cli`, and refuses a rolling `transition`. The explicit
sequence is `plan`, `prepare`, `withdraw`, then `stop` and `remove` for each role,
then `check` and `apply` for each role. Role operations require `--role stable`
or `--role canary`. `prepare` retains a verified dump before runtime changes;
`check` writes no retirement state or evidence. The Artifact carrier is ephemeral
even for reads, so target authorization covers that lifecycle too.

Full withdrawal uses canonical empty backend upstreams and waits for old NGINX
workers through the existing intent/completion protocol. Neither backend is a
replacement or survivor requirement. Only exact cleanly stopped containers can
be removed, without volumes. Both removals and an empty process inventory are
required before retirement. Expired leases, a successful CLI exit or absent
containers alone are insufficient. Unknown Docker command results are not
replayed; prove the exact post-state or investigate. A lost database retirement
response is read back against the retained request and revision.

The controller does not migrate the database or start/rejoin new serving
processes. Those remain separate, authorized operations. After M126 migration,
new ready processes can rejoin from the retained empty membership through the
normal ingress owner; old processes must never be restarted against M126.

The new runtime accepts the complete M126 ledger only. Historical artifacts keep
their original finite bridges and reject M126. Historical release/manifest
identities are not rewritten.

## Fresh installation

Use [FRESH-INSTALL.md](FRESH-INSTALL.md). The same owner applies the full
publication/expansion chain, then M126 as its final atomic completion step.
No License import or commercial signing key is required. Empty-database
ownership, build identity, final coverage verification and first-customer
bootstrap remain mandatory.

## Recovery

Before the M126 transaction commits, failure rolls back that transaction and
the database remains M125; retain the error and investigate before retrying.

After commit, no generic SQL down owner is supported. Recovery requires the
verified pre-M126 backup plus its original compatible Artifact under the
existing approved recovery procedure. Restoring it loses subsequent writes
unless the operator has separately reviewed how to preserve them. Do not
remove the migration row, replay historical trigger SQL, edit entitlement data
or start an old binary against M126 as an improvised rollback.

[Release readiness](../community/release-readiness.md) tracks missing acceptance.

## Prepared disposable maintenance acceptance — 2026-10-10

The local candidate `.github/tests/maintenance-native.py` and scoped workflow
`.github/workflows/maintenance-native.yml` exercise the original M125 artifact
`59c47b852b7372951c5c49b0e6b23df8c83817b9` against the exact candidate head/tree.
The proposed environment is one disposable GitHub-hosted Ubuntu 24.04 amd64
runner, with a 45-minute job limit and the existing account budget/hard stop.
It creates synthetic signing keys, loopback TLS and test-only databases; no
repository secrets, ECS, existing databases, provider calls or official
publication are involved. The owner explicitly approved committing and running
this bounded disposable-maintenance batch on 2026-10-10. Actual native
acceptance remains pending and must bind the resulting head and run ID.

The harness uses shipped owners to initialize M125, import a synthetic License,
retain a pre-M126 backup, withdraw both receivers, prove clean process exit,
remove exact stopped containers, retire the two generations and migrate M126.
It checks full ledger/history/financial-certificate preservation, old-artifact
schema refusal, new-artifact admission and login through NGINX. Recovery uses
an independent empty PostgreSQL instance, the original restore/rebase owner
and the old artifact; it never down-migrates or overwrites the migrated fixture.

Negative checks cover unsafe startup/migration, stop/remove/retirement before
withdrawal and a stale revision while the genuine predecessor remains eligible.
Plan and check compare protected database/evidence/resource facts. Initial
login audit and its matching outbox must settle before the backup baseline;
normal active-runtime heartbeat fields remain explicitly excluded only while
old processes are live. Content-stream counts are reported, not invented.
Only the allowlisted JSON report is uploaded, for seven days. Raw logs, dumps,
private keys, passwords and tokens remain inside the temporary runner.

Independent pre-code review and the single closing review by
`/root/installer_precode_review` are complete. All three required closing
findings were corrected. Python 3.10 grammar, help, non-runner refusal before
fixture creation, workflow identities/permissions/upload scope and whitespace
checks passed. Exact local receipts are in
`/private/tmp/unirouter-component-opensource-20261010/maintenance-offline-checks.json`
and `maintenance-closing-review.md`. Native acceptance has not yet passed.

The earlier [fresh-installer acceptance](COMMUNITY-INSTALLER.md) explicitly
excludes this maintenance path. This batch uses the separate explicit owner
approval above, rather than extending the earlier scope by inference. Bind
any result to the actual head, images and run ID. Existing required checks,
merge/publication restrictions and target-specific deployment approval remain.

### Initial native preflight result

Run [38058875535](https://github.com/foigent/unirouter/actions/runs/38058875535)
at `bb9b26befa414bfe53390503b105b40727596b24` stopped at the formal storage
preflight before builds, keys or database startup. Python 3.10.22, Docker
client/server 28.0.4 and Compose 2.38.2 passed their prerequisites. The original
report did not retain storage measurements, so it cannot identify the reason
or establish maintenance acceptance. A test-only diagnostic follow-up retains
numeric capacity/usage and fixed refusal categories while preserving the
8-GiB-free/80-percent-used checks. No arbitrary command output is uploaded.


Run [38064314648](https://github.com/foigent/unirouter/actions/runs/38064314648)
at `5e2d354310e4e54e96227728f96cd402089d8ecb` retained the exact refusal:
12.6 GiB available, 83 percent used, exceeding the unchanged 80-percent limit.
It also stopped before builds, keys or database startup. The disposable runner
image was `ubuntu24/20261004.327`. Its [official inventory](https://github.com/actions/runner-images/blob/ubuntu24/20261004.327/images/ubuntu/Ubuntu2404-Readme.md)
includes three Android NDK packages unused by this server-only rehearsal.
The test harness prepares only this newly provisioned GitHub VM by asking the
preinstalled `sdkmanager` to uninstall `ndk;27.3.13750724`,
`ndk;29.0.14206865` and `ndk;30.0.16248370`, when present and identity-verified.
Unknown packages, user worktrees and other hosts are untouched. No directory
removal command, tool installation or global configuration change is used.
The report retains package IDs and numeric capacity before/after preparation;
the same formal 8-GiB/80-percent storage checks still decide admission.

Run [38064998204](https://github.com/foigent/unirouter/actions/runs/38064998204)
at `66d96420b8de27a191b3fe3f94ea9ee32784798b` refused before uninstall/builds:
the preinstalled SDK manager was not discoverable through `PATH`. The runner's
[exact image build script](https://github.com/actions/runner-images/blob/ubuntu24/20261004.327/images/ubuntu/scripts/build/install-android-sdk.sh)
installs it at `/usr/local/lib/android/sdk/cmdline-tools/latest/bin/sdkmanager`.
The harness now checks and invokes that exact executable within the SDK root,
without changing `PATH`, installing a tool or expanding the package allowlist.
