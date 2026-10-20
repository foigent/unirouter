# Linux community installer candidate

Current 1.13.0 delivery requires the M127 maintenance/fresh-install contract. See
[connection communication controls](XINSIGHT-COMMUNICATION.md) for manual defaults, migration and recovery. Earlier M126 candidate notes below remain historical.

Status: the accepted 1.10.0 source candidate `71b5a4ee72d649d08e1137e68edb8b4d866f0987`
passed disposable Linux amd64 installation and was merged. See the
[final native result](#accepted-native-result) and
[public distribution preparation](#public-installer-distribution-preparation--2026-10-11).
Public package distribution remains pending. This work has not published a
package, registry image or npm package, or installed to an existing Target.

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

These Local checks alone did not prove Docker or a downloadable release works.
The separately authorized native batch below supplies actual Linux installation
evidence. Public artifact acceptance remains separate; existing ECS services
remain untouched.

Public download addresses, official release signing/publication, Target service
changes, TLS exposure and provider credentials retain their authorization boundary.
An npm launcher, automatic Docker installation, upgrades and uninstall are
separate follow-ups; the first path uses Python 3 and Docker Compose already
installed on the server.

## Native acceptance batch

The next environment should be a disposable GitHub-hosted Ubuntu runner for
this private candidate, rather than an existing ECS application host. The owner
approved this exact temporary environment, synthetic data, temporary test keys
and service lifecycle on 2026-10-10. Official release publication remains separate.
The existing CI allocation and hard stop remain unchanged. A separate scoped
PR workflow keeps native installation distinguishable from the existing Docker
Build check. Manual dispatch is also available after workflow registration; a
branch-only new manual workflow cannot reliably provide its own first run.

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
original version and environment limits. The separate explicit approval covers this test environment; the earlier local
PostgreSQL/Redis approval was not extended by inference.

The new batch received independent pre-code review from
`/root/installer_precode_review` against `aff3eb4baf497e3c34ce21a07ee6b32928f51c16`.
All eight required points are retained: exact head/tree/version, original signing
interfaces, real digest references, real monitoring files, narrow transport fault
injection, one fixed-subnet installation, authoritative no-op/identity readback
and secret-free bounded execution.

The implementation is `.github/workflows/community-installer.yml` with the
CI-only `.github/tests/community-installer.py`. It runs the actual fresh plan,
then uses a temporary Docker transport wrapper to lose that successful command
response and kill only the installer parent. Normal `resume` runs without the
wrapper. This proves recovery after a completed plan, not a crash within a
migration. No product behavior is patched or simulated. ARM64, existing-database
maintenance, provider setup and official distribution retain separate evidence
requirements.

The single closing review found and resolved a runner ownership issue: helper
CLI builds use `-buildvcs=false` because the harness runs as root against a
runner-owned checkout. The backend release image still receives the exact
commit, tree and version explicitly. No global Git trust setting is changed.
Local syntax, workflow structure, wrapper syntax and host-refusal checks pass.

Native runs [38038209649](https://github.com/foigent/unirouter/actions/runs/38038209649)
and [38038691565](https://github.com/foigent/unirouter/actions/runs/38038691565)
retain the failed `bd4d4b9` and `3a30ddd` candidates respectively. The diagnostic
run reached fresh-apply, release registration and administrator-rotation
checkpoints, then failed frontend health. The frontend was running with no
restarts: readiness probe return codes were `localhost=1`, `127.0.0.1=0`,
`[::1]=1`; the backend, PostgreSQL and Redis were healthy. This establishes a
loopback-address mismatch, not a completed installation or post-install audit
verification. Raw logs and credentials were not uploaded.

The independently reviewed correction changes only the customer frontend probe
to IPv4 loopback; endpoint meaning, listener, timeouts and retries stay intact.
The exact corrected commit must pass the existing
[Community Installer Acceptance workflow](https://github.com/foigent/unirouter/actions/workflows/community-installer.yml)
with a newly generated synthetic package. Its allowlisted report is the
installation result; a previous failed candidate's package is never reused.
The workflow includes original identity, one-rotation, repeated-resume, login
and License-retired assertions. Results apply only to the named commit and
Linux amd64 fixture, not to an official distribution or existing ECS runtime.


## Accepted native result

[Run 38039244599](https://github.com/foigent/unirouter/actions/runs/38039244599)
passed all eight check groups at exact head
`b7432803a8ae7ef3b946501ae5f3e9ff0a905273`, tree
`0c52df41786c09bc5c5fe1078e45788f5278c137`, version 1.10.0.
The runner used Ubuntu 24.04, Linux amd64, Python 3.10.22, Docker 28.0.4
and Compose 2.38.2. Its allowlisted acceptance report contains the six image
digests and synthetic package SHA-256
`3c99ef97de8fb6802064e1cdc7663aada3d501a9104627199ca7e4a4cfbb2981`.

Verified: read-only checks; wrong key and changed payload refusal without
writes; unlabelled volume collision refusal; actual fresh-plan completion
followed by response-loss interruption; normal resume with the same identities;
M126, image/database/release binding and exactly one administrator password
rotation; unchanged completed resume; real NGINX console, login and
`license.status=not_required`.

All credentials, keys and data were temporary synthetic fixtures; there were
zero provider calls. This verifies the interrupted-after-plan fresh path,
not a crash during migration. It does not establish ARM64, an existing M125
database's Docker/NGINX maintenance path, public trust/download bootstrap,
Target, Provider or Production acceptance. Failed runs above retain their own
identities and results.

## Public installer distribution preparation — 2026-10-11

Status: **local preparation and proposal; no official signing or publication**.
The source baseline is main `69d1ef753a2121e5f288b4618bcc4bf1a5d29d48`,
tree `48affd17183df1d70818fa9fe9c57998a3507219`, version 1.10.0/M126.
The repository identity and main were read back from GitHub on 2026-10-11.
The only existing UR release is an unpublished historical 1.1.9/sequence-11
customer draft. It is not the community installer and must not be overwritten,
published, reused as the new release, or used as proof of a public signing key.

### Distribution proposed for the first public package

Use a downloadable Linux amd64 release and public container images, preserving
the six-service installation and the existing production-mode verification.
Native acceptance currently covers amd64; arm64 support needs its own actual
installation evidence before it is advertised for a public package. This does
not withdraw any existing source-level architecture support.

- Publish UR-owned backend/frontend images under `ghcr.io/foigent`, associated
  with `foigent/unirouter`, with exact content digests recorded in the signed
  descriptor. Repository visibility does not imply package visibility. Verify
  a new package's anonymous pull explicitly before advertising installation.
  [GitHub's registry documentation](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)
  supports public anonymous pulls.
- Keep PostgreSQL, Redis, Prometheus and Grafana as upstream-published images at
  reviewed digests. Do not silently replace Redis, downgrade it, make monitoring
  optional or rebuild/rehost third-party images in this distribution batch.
- The first download is an online installation. It must say that network image
  pulls are required. It is not the existing fully offline customer bundle:
  that path retains its six-image-archive delivery requirements.
- Preserve the installer, Compose, fresh-install owner, read-only `check`,
  retained plans, resume, loopback console and no-Provider-call boundaries.
  End-user setup still requires supported Python, Bash and Docker Compose.
  Automatic Docker installation and an npm launcher remain separate work.
- Begin with a clearly labelled public preview/pre-release, pending exact
  package and public-download acceptance. Do not claim a production support
  window from the source CI or the synthetic installer run.

### Package contents and trust bootstrap

The existing `uniroute-upgrade pack` and `verify` remain the signing format and
verification authority; this proposal introduces no replacement cryptography.
The backend must embed the approved public upgrade ring at build time. Only the
public ring enters CI build arguments or an image. Commercial-License admission
stays retired; release signing authenticates a publisher's files and does not
charge users, control their organizations or prevent their own source builds.

The reviewed payload list is:

1. `payload/install.json` with the six exact approved image references;
2. the four installer/template inputs and the four monitoring configuration files;
3. English/Chinese installation guidance and the customer recovery instructions;
4. the exact `scripts/source-budget-restore-rebase.sql` recovery helper;
5. project LICENSE/NOTICE, a third-party index and unmodified notice texts;
6. exact build provenance, actual-image component inventories, applicable source
   delivery information and the release notes.

The staged documentation must be adapted to its delivered paths. Relative
links to pages outside the payload are changed to exact source-commit URLs and
checked against that tree; release-specific entry points must use the final
package identity. The first public guide must explicitly distinguish an amd64
preview artifact from source-level arm64 support.

The signed archive permits at most 64 payload entries. The existing 390 source
notice files therefore belong in a deterministic, indexed companion archive
as one declared payload entry, not 390 extra outer entries. Nested notice
materials are documentation and are never executed by the installer. Their
index must distinguish reviewed source notices from actual image coverage.

The download also needs a release-matched verifier and public ring obtained
**before executing any downloaded installer**. They cannot acquire trust merely
by being included beside the package they verify. A concrete bootstrap proposal
is to publish the verifier hash, public-ring hash/fingerprint and exact release
identity in a reviewed, commit-pinned official repository entry, independently
of the signed package. The authenticated repository/HTTPS channel is that
initial trust root; a second URL on the same GitHub account does not protect
against compromise of that account. A user with an established independently
trusted public ring keeps it; an archive cannot replace it.

The 1.11.0 source candidate implements this download/prepare entry in
`deploy/community/bootstrap.py`. It first authenticates the bootstrap input,
then verify the package with the existing verifier, and only then prepare the
exact declared regular files. It must reject unsafe paths, links, duplicate or
undeclared entries and size/digest mismatches, create a new private destination,
and never overwrites a user's existing directory or executes package code during
preparation. The underlying installer still consumes a prepared directory. The
wrapper adds preparation and delegates installation/check/resume to it; official
bootstrap publication, fresh native acceptance and real public-download evidence
remain pending. There is still no advertised one-command public install.

No final tag, release sequence, creation time, package hash or image digest is assigned
by this proposal. Allocate those only after the current delivery history and
exact candidate are checked, with no overwrite. The historical sequence-11
draft must be accounted for; a fresh-install example's sequence 1 is not an
authorized production allocation. The supported existing-database upgrade
window also cannot be inferred from fresh-install sequence-zero acceptance.

Explicitly non-expiring packages use signed `unirouter.upgrade-manifest.v2` with `expires_at: "never"` (UniRouter 1.14.0+). The standalone verifier and backend must both support v2, including `upgrade record-applied`. Existing finite v1 packages retain their original deadline; replacing or re-signing them cannot resume a plan bound to a different package hash. Every check/install/resume still verifies the original signature, hashes, creation time (with the existing five-minute future tolerance), platform, schema and release sequence. Non-expiring does not authorize rollback, skip trust checks or extend the owner's finite signing-authority window. This source change is not a published package or a verified deployment.

The current preparation selects v2/never explicitly; actual source, images,
sequence, creation time and download identities must be rebound after the new
build. Preserve the frozen 1.13.0 candidate. For a historical expired v1
package, preserve its package, plan and data, then use the supported recovery
procedure; do not change the clock, use `--at` or bypass verification. No new
expired-v1 recovery mechanism is introduced.

### Third-party findings and commercial scope

The source-notice review has 315 components and 390 original notice/license
files (747,514 bytes). All file hashes and sizes were checked again; Go and
frontend lock inputs are unchanged between inventory source `6bd3ae19` and
current main. This validates reuse of those source materials, not clearance of
the final built frontend, Go binary, base images or operating-system packages.

The native fixture currently uses Redis 7.4.11 and Grafana 11.0.0. Redis 7.4.11
provides [RSALv2 or SSPLv1](https://github.com/redis/redis/blob/7.4.11/LICENSE.txt),
not Apache-2.0 or the older Redis BSD license. Grafana OSS uses
[AGPLv3](https://grafana.com/licensing/). These component terms do not become
Apache-2.0 because UR is Apache-2.0. Redis 8's additional license option does
not apply retroactively to the pinned 7.4 fixture. Upstream image pulls do not
by themselves settle the project's license/source-delivery obligations.

For the proposed exact distribution, record whether each image is referenced,
redistributed or modified; retain original notices, tie sources/build recipes
to the actual image contents, and satisfy the applicable source-delivery terms
before publication. Do not treat a Docker tag, license label or top-level
source URL as a complete component inventory. This preparation does not decide
legal compatibility for a future paid hosted product. A Redis/Valkey change or
monitoring split may be considered separately when its product/compatibility
tradeoff is accepted; neither is silently included here.

### Remaining execution and owner decisions

Local work can stage unchanged release files, compile the existing verifier,
validate source notices and prepare exact file receipts without credentials,
Docker or production access. This machine has no Docker executable. Existing
synthetic native results remain valid for their exact code and environment;
local cross-compilation cannot replace actual Linux or public-pull acceptance.

Before a formal backend build, the owner must select the official upgrade
public ring and private-key custody. Prefer a dedicated community-release
upgrade key, separate from historical License keys and every synthetic test
key. Reuse of an existing upgrade key is an alternative only after its purpose,
public identity and custody are confirmed. No secret is to be pasted into chat,
read for discovery, or placed in the repository, ordinary CI artifact or log.
The current release contract calls for approved offline/HSM custody; storing a
private release key in GitHub Actions Secrets is not an already accepted substitute.

After that choice, complete exact image builds/content review, verifier and
bootstrap acceptance, wrong-key/tamper/no-write tests, fresh install/resume/login,
and anonymous downloads/pulls on the supported platform. Public-download
acceptance necessarily follows approved publication to a clearly labelled
preview channel; promote only the same bytes that pass, without rebuilding.
Keep the full required CI and exact-main protocol evidence for changed code.

Creating or reusing signing credentials, allocating/signing immutable release
identities, pushing GHCR images, publishing tags/releases, or changing package
permissions each requires scope covered by the owner's approval. The earlier
source-publication batch excluded these operations. Prepare the exact candidate,
channel, key/public identity, verification and recovery plan before requesting
that approval. ECS/Production, real databases/traffic and Provider calls remain
outside this installer distribution work.

### 1.11.0 bootstrap candidate and validation scope

The local candidate adds `deploy/community/bootstrap.py` and its
[operator/metadata contract](../../deploy/community/README.md). Initial script
and bootstrap hashes must come from independently reviewed, commit-pinned release
instructions. The downloaded verifier/ring/package each have an exact size and
hash. HTTPS redirects cannot downgrade transport or add URL credentials.
There is no official populated bootstrap JSON, key adoption or published asset.

A read-only bounded scan inspects physical USTAR headers and total decompression
before invoking the existing verifier; it rejects implicit PAX/GNU/sparse data,
links, duplicate/unsafe paths and undeclared files. Only the verifier-bound
canonical manifest then authorizes preparation. In particular, its signed key ID
is checked from the bound manifest because the verifier does not emit that field.
Every reuse rechecks assets, signature, expiry and prepared payload bytes.

Preparation and reuse require an owner-private directory with protected ancestry.
A root installation cannot reuse lower-privilege prepared files; no automatic
chown or trust import occurs. The bootstrap's `check` and `resume` need completed
preparation and perform no downloads or preparation writes. `check` retains the
original installer's fresh-install preconditions; it is not a running-system
health check. Existing partial output and original plans are retained on error.

The existing native acceptance harness now prepares its signed fixture through
this entry, then runs all original installation/interruption/identity/login
checks against the resulting payload. It also checks bootstrap read-only reuse
and completed resume. The fixture uses locally supplied bytes with the same
hash and signature checks; it starts no extra download server and exposes no
TLS-bypass CLI flag. The root-owned temporary directory is below `/var/tmp`
because root execution must not reuse runner-user-controlled ancestry. Original
45-minute runner, synthetic-only, no-provider/no-ECS/no-publication boundaries
remain. Workflow path filters and the existing delivery preflight include the
new entry; required CI and exact-main protocol evidence are retained.

Native execution for this changed candidate passed as recorded below. Actual
public HTTPS/anonymous image-pull acceptance remains pending. The accepted
1.10.0 results retain their original scope. Local results and independent review
are recorded in the execution record; version/CHANGELOG metadata advances to
1.11.0 because the installation entry is a new shipped capability. The earlier
69d1ef75 unsigned staged files remain historical preparation and are not a
current installable release.

### Accepted native bootstrap result — 2026-10-11

[Run 38109442307](https://github.com/foigent/unirouter/actions/runs/38109442307)
passed all ten checks for source `87b0a6dc600436b213a8cf943e3c68f188083c7b`,
tree `247594ff534c886b91b61ccf1dc0bedd139167de`, version 1.11.0.
The disposable Linux amd64 runner used Python 3.10.22, Docker 28.0.4 and
Compose 2.38.2. It prepared the signed fixture through the new bootstrap, then
verified the original fresh-install/interruption/recovery path, unchanged
installation identity, M126/image/database bindings, one administrator password
rotation, real NGINX login and `license.status=not_required`. Bootstrap check
had no filesystem/Docker side effects and completed bootstrap resume preserved
the original installation.

The allowlisted report has SHA-256
`da6a5bf0a2c29639ce6db4e72a41ca70a08d10b26712bad5ac978d53a98385ee`.
Its synthetic package SHA-256 is
`5e98715e4f6ebe51483ddf0aa524a0cec931c451590590c80f3ff14fa80ca1a0`,
and synthetic bootstrap SHA-256 is
`b5caeca3dce49a3aa1cbe3c80d10bd882345c07ffe021ecc9188502b48e58ca3`.
The report is retained as the run's `community-installer-87b0a6dc600436b213a8cf943e3c68f188083c7b`
artifact. These hashes identify test evidence, not downloadable official assets.

All signing keys, credentials and database contents were disposable synthetic
fixtures; provider calls were zero. Transport supplied fixture bytes locally,
so this does not prove public HTTPS downloads, anonymous public image pulls,
ARM64, official signing custody, an existing ECS/Production upgrade or a new
maintenance-migration result. The prior 1.10.0 maintenance acceptance retains
its own exact source and result.

### Owner-local encrypted NAS backup preparation

The owner chose a UGREEN NAS and requested local files for manual transfer.
The prepared macOS operator kit is at
`/Users/m4/Desktop/UniRouter-Release-Setup`. It contains tools and instructions,
not a generated official key. The user must run its interactive ceremony after
quitting Codex and disconnecting networking, supplying a separate password
locally. The tool never requests a password through chat or stores it in
arguments, environment variables, a file or Keychain.

The existing keygen writes directly into a verified AES-256 APFS image mounted
under a task-private temporary directory. It never separately saves a plaintext
private key on Desktop or in the repository. After normal detach, it copies the
encrypted image, verifies its hash, opens the copy read-only, compares recovered
keys and closes it. Only then does it publish `Copy-to-NAS/READY.txt`, the public
ring and copy-verification metadata. Existing or partial output is refused;
errors retain encrypted artifacts and attempt normal detach of only this run's
exact images. A failed or uncertain detach is reported, not called complete.

The owner can copy the complete `Copy-to-NAS` folder to the NAS while retaining
`Local-Copy` and an independently stored password. Copy verification and an
actual NAS restore remain pending. The NAS stores encrypted bytes, not an
unlocked key or a signing service. This scoped backup arrangement is not proof
of offline/HSM protection; the kit relies on human-attested offline operation,
and cannot protect an unlocked key on a compromised Mac. Official public-ring
adoption and approved signing custody must still be checked before a formal
build. The existing release requirements and publication permissions are not
silently replaced by this operator kit.

Validation used temporary synthetic credentials only: ten local test methods
with failure/interruption cases, existing keygen tests, and a real macOS image
round trip with wrong-password refusal, matching keys, unchanged ciphertext
and all test images normally detached. Desktop copies match the reviewed kit;
no official private key has yet been created, accessed or adopted. Exact kit
hashes are in its `VERIFICATION.json`; test and review evidence is under
`/private/tmp/unirouter-release-custody-20261011`.

### Independent review and local result

Pre-code review by `/root/distribution_precode_review` bound proposal SHA-256
`e38e91cb2b937057ebf9b472ab37cfdd247fad87d86d1daeb4c34739702ccd7e`
to the source baseline above. No blocker applies to local unsigned preparation.
Required corrections were the expiry/resume boundary and package documentation
links; both are incorporated here. Official trust/custody remains an owner
decision. Actual-image licensing and the public download/prepare implementation
remain required for distribution, not completed by this review.

Local preparation staged 15 payload files. The 390 original source notices
were verified and packed deterministically with their index and scope note;
the companion archive SHA-256 is
`7e0503ffa15e155edf0680fd2915d3c7e09de889bc863100a47c84858ebf6892`.
The current Linux amd64 source dependency graph exactly matches all 42 Go
module notice entries. The existing verifier was compiled with Go 1.25.12 and
`-trimpath -buildvcs=false`, without network downloads or signing material.
Two builds were byte-identical (SHA-256
`d4081b251de6faf2f95664d82657beb7b8a3104249e9e9504542a3e1e00bf942`);
this is local cross-compilation, not native Linux execution or trusted public
distribution. No `release.tar.gz` or pretend `install.json` was produced.

Evidence and staged files are retained locally under
`/private/tmp/unirouter-distribution-prep-20261011/`. They are incomplete,
unsigned preparation, not an installation command or public release. No new
experience-file entry is warranted.
