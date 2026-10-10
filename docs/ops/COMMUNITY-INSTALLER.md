# Linux community installer candidate

Status: local implementation and offline checks complete; actual Linux/Docker
acceptance and publication pending. No public package, registry image,
npm package or target installation is created by this work.

The owner selected Linux servers, including ECS, for the first simplified
installation. Existing ECS onboarding/model evidence remains valid for its
recorded versions. This work automates a new user's installation; it does not
reopen completed provider acceptance.

## Reviewed boundary

Independent pre-code review: `/root/installer_precode_review`, read-only,
2026-10-10, against the 1.10.0 working candidate on baseline
`59c47b852b7372951c5c49b0e6b23df8c83817b9`.

The installer wraps the existing signed customer release and Compose template.
It must verify the bundle and every prepared customer file before using them,
including a signed descriptor for the six approved image references. The
separately trusted upgrade verifier and public ring remain the bootstrap root.
It never extracts an unchecked tar archive, builds source, installs Docker,
changes the host firewall or uses development mode to bypass release evidence.

`check` performs read-only prerequisite/identity checks without generating
secrets or starting services. `install` creates one private directory and a new
Compose project, generates independent secrets once, starts PostgreSQL/Redis,
then uses the exact backend's fresh-install owner and retained plans. Initial
release recording and the administrator password rotation finish before HTTP
is started. Success means a private setup console, not an already-qualified
public model Gateway. No login, provider credential, Probe or model call is
performed automatically.

Resume retains the same package, config, Docker daemon, project, database and
plans. The DB's existing owner is the installation authority. Release recording
is reconciled through `upgrade status`; only an exact match is accepted.
Password rotation is not blindly retried: the existing CLI appends an audit
on each call, while first-customer bootstrap requires exactly one rotation.
An uncertain result stops before HTTP and requires operator investigation.
No audit deletion, new password-authority table or relaxed checkpoint is added.

First-install collision checks cover existing project containers/volumes/
networks by label and exact intended name, loopback port use and the template's
fixed subnet. Resume requires original ownership even for exact-name matches.
Do not run concurrent installations in different directories with the same
Compose project name; that mode is unsupported. Ambient Compose
and Docker variables cannot redirect the installation. Installer progress is
local orchestration evidence and does not replace any database ledger.

## Release producer input

The [English guide](../community/install-linux.md) and
[Chinese guide](../community/install-linux.zh-CN.md) describe the user flow.
The repository source template is not a prepared release. A producer must
provide a reviewed directory with `release.tar.gz` plus its prepared payloads.
No unverified extraction is performed by this installer.

The existing `uniroute-upgrade pack --artifact payload/path=/source/path`
interface declares every input's path, size and SHA-256. Include:

- `payload/install.json`;
- `payload/deploy/install.py`, `docker-compose.yml`, `.env.example`,
  `preflight.sh` and every required monitoring file under `config/`;
- the other required release materials, image/provenance and notices under
  the existing customer release contract.

`payload/install.json` has exactly two fields: `schema_version` equal to
`unirouter.install.v1`, and `images`. The latter maps exactly `backend`,
`frontend`, `postgres`, `redis`, `prometheus`, and `grafana` to canonical
registry references ending in `@sha256:<64 lowercase hexadecimal digits>`.
Use the repository spelling returned by Docker `RepoDigests`; tag aliases
and undeclared environment substitutions are not installation input.
Choose references available for the signed target architecture. No example
digest in this repository denotes an approved public release.

The bundle must admit current sequence zero and post-fresh M126, with target
schema `000126_commercial_license_retirement`. This is verification for initial
release registration after the formal fresh owner completes, not an assertion
that the empty database already contains M126. The embedded backend trust
root must accept the actual package and its version/commit/platform/schema.
The independent verifier now returns `manifest_sha256`, computed from the
exact canonical manifest accepted by the signature verifier. The installer
requires this binding for the entire artifact list, not only version/commit.
Previously bootstrapped verifiers without that field need the reviewed update.

Do not sign or publish until image builds, candidate content, metadata and
native acceptance are ready. This task creates no production key, immutable
release identity, registry artifact or public download address.

## Delivery and validation

Closing independent review checked the 14-file receipt
`/private/tmp/ur-community-installer-closing-review-20261010.json`, SHA-256
`ca1b6c5dd021f3180e39fcc16919aca0c901c343d150401607c66565883d9e54`.
The reviewer reported four `validated_blocker` findings. The executor verified
and corrected all four in this batch:

1. Unlabelled/differently labelled exact-name resources are now rejected before
   service startup. Compose can otherwise reuse such volumes; see its
   [official implementation](https://github.com/docker/compose/blob/v2.38.2/pkg/compose/create.go#L1441-L1487).
2. If the original fresh owner created its evidence directory before the DB
   claim, the wrapper uses `resume` even while the DB is still unclaimed.
   No evidence is removed. If no directory exists and the DB is unclaimed,
   `apply` remains appropriate: the original CLI cannot claim before creating
   that directory. An active DB with missing evidence is refused by its owner.
3. Effective Compose JSON must bind all six service images to the signed
   descriptor, the backend's runtime declaration to the inspected image ID,
   and named volumes/network to this private project. The JSON stays in memory.
   Service commands use the documented `--pull never` policy
   ([run](https://docs.docker.com/reference/cli/docker/compose/run/),
   [up](https://docs.docker.com/reference/cli/docker/compose/up/)).
4. Bounded descriptor and environment-template bytes are checked against their
   signed size/digest at the exact read used for parsing. An initial earlier
   file check alone cannot authorize reread bytes.

23 offline cases passed after these fixes:
`/private/tmp/ur-community-installer-offline-4.log`. Full race tests for the
standalone upgrade CLI and bundle package also passed after the fixes,
including real Go/Python signed-input interoperability with synthetic keys:
`/private/tmp/ur-community-installer-signatures-final.log`. Release-policy
19 cases, current metadata, CLI help, local documentation links and whitespace
checks passed. No additional independent review round is claimed.

These checks do not prove Docker or a downloadable release works. Actual Linux acceptance
requires a reviewed signed release, real images and a separately authorized
disposable host. Existing running ECS services remain untouched.

Public download addresses, release signing/publication, real Docker acceptance,
TLS exposure and provider credentials retain their own authorization boundary.
An npm launcher, automatic Docker installation, upgrades and uninstall are
separate follow-ups; the first path uses Python 3 and Docker Compose already
installed on the server.

## Proposed native acceptance batch

The next environment should be a disposable GitHub-hosted Ubuntu runner for
this private candidate, rather than an existing ECS application host. This is
a proposed test batch, not a workflow already dispatched or an approved release.
The existing CI allocation and hard stop remain unchanged. A separate manual
job keeps native installation distinguishable from the existing Docker Build
check and avoids running the full install on every documentation edit.

Before execution, bind the batch to the exact candidate commit and runner
platform, inspect available Docker/Compose/Python versions and storage, and
prepare the reviewed package using the existing pack/verifier interfaces.
Use only a fresh test signing key and public ring, embedded in this test
backend's release-mode build. Production keys, repository secrets and private
registry credentials are unnecessary. Test signatures do not establish the
identity of a future official release. Record the exact six pulled/built image
digests; backend and frontend registry round-tripping, if needed for RepoDigests,
stays inside a loopback registry on the same disposable runner. No registry
publication or public release is part of this batch.

Acceptance must exercise the actual packaged installer:

1. `check` leaves files, database and Docker resources unchanged; reject a
   modified payload or wrong ring before starting services.
2. A fresh install reaches M126 through the existing fresh owner, records the
   exact test release, rotates the administrator password once and starts the
   loopback console without a commercial License. Check actual image IDs,
   backend and NGINX health, login and the License-retired response.
3. An interrupted install resumes with the original project, secrets, database
   identity and retained evidence. Repeating a completed install/resume does
   not append another password-rotation audit. An ambiguous rotation remains
   a refusal requiring investigation, not a blind retry.
4. Existing exact-name or labelled Docker resources are refused; changing
   signed inputs or resuming against the wrong daemon/project/database is
   refused. Compare before/after resource and database state.
5. Retain redacted commit/image/version/schema/check results and failure logs.
   Do not upload generated secrets, private keys, full Compose output or raw
   customer/provider content. Dispose only of this job's synthetic resources
   through the runner lifecycle.

This batch validates installation and console access. It makes no provider
call, publishes no port beyond loopback, changes no ECS/firewall configuration,
and does not replace the separate M125-to-M126 maintenance acceptance or
published-download/trust-bootstrap checks. Existing provider evidence keeps its
original version and environment limits. New test infrastructure and execution
must have corresponding authorization before dispatch; the earlier local
PostgreSQL/Redis approval alone does not cover this Docker environment.
