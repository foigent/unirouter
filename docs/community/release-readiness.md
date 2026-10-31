# Open-source release readiness

Status on **2026-10-31: Preview20 is signed and public, and its exact public package passed formal Linux installation acceptance**. English is the primary community language; Chinese entry points and contributions are welcome.

## Current public preview — 1.15.3 / M129

The [English Linux guide](install-linux.md) and [Chinese companion](install-linux.zh-CN.md) bind [v1.15.3-preview.20](https://github.com/foigent/unirouter/releases/tag/v1.15.3-preview.20). Product source is `1a0230b50f799e2f9c64089270d525ec1ac34ca7`, release sequence 20, with 26 signed payloads and `expires_at: never`. The package is 130,052,464 bytes, SHA-256 `d3a6b16abd7561a19ca9b7e35c5e8aaa7fa018a2fb1f221c1a0dc30826c8726b`. Its signed predecessor range is 0–19 with minimum schema M127; existing M127/M128 installations use the [data-preserving upgrade runbook](../ops/COMMUNITY-UPGRADE.md). The original signing trust root is unchanged. The unsigned bootstrap JSON is independently pinned by its hash.

[Formal consumer 41196358700](https://github.com/foigent/unirouter/actions/runs/41196358700), attempt 1, passed all 12 checks on disposable Ubuntu 24.04: exact public signed download and all payloads; six anonymous image pulls and exact app config/layer identities; fresh M129 installation; trust/collision refusals and check purity; same-identity interrupted installation/resume; console/login with `license: not_required`; native chosen-password behavior; read-only first-customer preview, single zero-wallet creation/replay and preserved identities after restart. It made no Provider call, signature or image build.

[Source PR #486](https://github.com/foigent/unirouter/pull/486), [candidate Linux tests](https://github.com/foigent/unirouter/actions/runs/41166284675) and [exact-main protocol validation](https://github.com/foigent/unirouter/actions/runs/41170407453) passed. The candidate Linux cases separately qualify M127/M128→M129 and later M129→M129 ordinary upgrades with paired backups and independent restore, using synthetic data and temporary trust. Accepted [producer 41171563978](https://github.com/foigent/unirouter/actions/runs/41171563978) and [publisher 41173193200](https://github.com/foigent/unirouter/actions/runs/41173193200) bind the two application images and signed publication chain. Four dependency images retain their existing exact signed references. No rebuild, repeated push or second signature was needed for public delivery.

The six public attachments are the complete signed package, Linux verifier, original public ring, independently pinned bootstrap JSON, historical retained-image source/NOTICE companion and checksums. Current source dispositions, notices and required source materials are inside the signed package. Historical materials and original third-party terms remain applicable; publication is not blanket license clearance.

Original-host M128→M129 maintenance, real Provider/UI and ordinary UR→XI acceptance remain separate, pending results. Public-package installation does not establish those environment-specific results. Historical previews and failed candidates retain their original evidence and identities.

## Historical public preview — 1.15.0 / M128 (2026-10-29)

The retained [English Preview17 guide](https://github.com/foigent/unirouter/blob/1a0230b50f799e2f9c64089270d525ec1ac34ca7/docs/community/install-linux.md) and [Chinese companion](https://github.com/foigent/unirouter/blob/1a0230b50f799e2f9c64089270d525ec1ac34ca7/docs/community/install-linux.zh-CN.md) bind [v1.15.0-preview.17](https://github.com/foigent/unirouter/releases/tag/v1.15.0-preview.17). Product source is `4eb32b5514d47aa91610fc66ef561368d6a18900`, release sequence 17, with 26 signed payloads and `expires_at: never`. The package is 130,045,928 bytes, SHA-256 `87f9fbd62b95a8293613f4fe726a2eca0ce723f31d4349a94b3e04a0844e23bf`. The signed predecessor window is 0–16 with minimum schema M127. Existing M127 servers use the [finite upgrade runbook](../ops/COMMUNITY-UPGRADE.md).

The actual holder signature, complete 28-member archive, all 26 payload hashes, native predecessor 0/16/M127 verification and out-of-range/schema rejection passed. The trust root is unchanged. Bootstrap JSON is unsigned and independently pinned by its hash.

[Formal consumer 40739178872](https://github.com/foigent/unirouter/actions/runs/40739178872) passed on a disposable Ubuntu 24.04 runner using these exact public assets: full signed package download/verification; six anonymous digest pulls; exact application configs and ordered layers; M128 initialization; check purity and trust/collision rejection; same-identity interruption/resume; real console/login and `license: not_required`; native password choice; first-customer zero-wallet creation/replay; and retained identities after restart. No model calls, signatures or image builds occurred in that run.

[Source PR #482](https://github.com/foigent/unirouter/pull/482) and [exact-main protocol validation](https://github.com/foigent/unirouter/actions/runs/40699016448) passed. The separate candidate Linux cases covered fresh installation and data-preserving M127→M128 maintenance with paired backup/independent restore. The accepted [producer 40703175851](https://github.com/foigent/unirouter/actions/runs/40703175851) and [publisher 40717610221](https://github.com/foigent/unirouter/actions/runs/40717610221) bind the two existing public image versions. Their actual publication chain is included in signed provenance; no rebuild or repeated image push was needed. Four dependency image references retain the exact signed 1.14.2 strings and digests.

The six attachments are the signed package, Linux verifier, public ring, bootstrap JSON, historical retained-image source/NOTICE companion and checksums. Current dispositions, notices and required source materials are inside the signed package. Original third-party terms and scoped attribution limits remain applicable; this is not blanket license clearance.

Existing-server maintenance, real Provider requests and ordinary UR→XI acceptance have their own environment-specific evidence. Public-package installation alone does not establish those results. Historical previews and failed sequence 14 remain preserved with their original outcomes.

## Historical publication snapshot — 2026-10-25

Status on **2026-10-25: Preview16 is signed, verified and published; formal
installation of its public package and the existing-server upgrade are separate,
pending results**. English is the primary community language; Chinese entry
points and contributions are welcome. `state: ready` means the exact consumer
inputs are complete, not that installation passed.

### Preview16 — 1.14.2 / M127 at publication

The [English Linux guide](install-linux.md) and [Chinese companion](install-linux.zh-CN.md)
bind [`v1.14.2-preview.16`](https://github.com/foigent/unirouter/releases/tag/v1.14.2-preview.16).
The actual signed package is 130,032,075 bytes, SHA-256
`2a7177d08ed74f8cccdaaeca04a69cd1f2706727e8b282614651b7056d8309e7`.
It binds product source `21572d9e73ca901583a8b526c9af7cd0319d59f1`, sequence 16,
M127, 24 payloads and manifest v2 with `expires_at: never`. Its predecessor
range 0–15 includes the actual 1.14.1 / sequence 15 installation. Native public
verification at 0 and 15, out-of-range/schema rejection, the closed 26-member
archive and all 24 payload hashes passed. The public trust root is unchanged.
Bootstrap metadata is unsigned and independently pinned by its hash.

This version fixes first-customer initialization after ordinary logout when
native retention cleanup has removed the expired administrator session. Current
authentication, exact audit shapes, zero-business-data requirements and release
guards remain enforced. It does not recreate historical sessions or delete audit
records. The existing native hidden administrator-password command is retained.

[Source PR478](https://github.com/foigent/unirouter/pull/478) passed
[required CI](https://github.com/foigent/unirouter/actions/runs/40133199823) and
[candidate Linux installation](https://github.com/foigent/unirouter/actions/runs/40133199834).
The latter used synthetic data and temporary signing trust. The accepted
[producer 40141868711](https://github.com/foigent/unirouter/actions/runs/40141868711)
and [publisher 40146914453](https://github.com/foigent/unirouter/actions/runs/40146914453)
bind both current application images. Their manifest/config/layer metadata was
read back anonymously. No rebuild or additional image push occurs here.

The six attachments are the signed package, Linux verifier, public ring,
bootstrap JSON, historical retained-image source/NOTICE companion and checksums.
The current package supplies its 1.14.2 component notices and required source
materials; the unchanged companion retains 1.12/1.13 mappings and shared-material
references. Third-party terms and incomplete attribution limits remain separately
applicable; this publication is not blanket license clearance.

The one real signed-package [formal acceptance](https://github.com/foigent/unirouter/actions/workflows/community-release-acceptance.yml),
the original-host data-preserving upgrade, provider requests and the new ordinary
UR→XI pairing remain pending. Source CI, candidate installation and publication
do not establish those results.

Historical [Preview15](https://github.com/foigent/unirouter/releases/tag/v1.14.1-preview.15)
and [Preview13](https://github.com/foigent/unirouter/releases/tag/v1.14.0-preview.13)
retain their original packages and evidence. Sequence14 remains a valid historical
signature with rejected installation inputs; it is not published, installable or
reused. The [Preview12 consumer](https://github.com/foigent/unirouter/actions/runs/39631286350)
verified its package/application images, then rejected a PostgreSQL image-name
guard before installation. No historical result is replaced by this release.

The accepted direction is to open-source the complete UniRouter (UR) and
X-Insight (XI) products under Apache-2.0, without a community/commercial edition split.
Foigent has no current plan for a commercial product. Third-party license terms
and each project's actual publication status remain separately applicable;
see [licensing and product scope](commercial-boundaries.md).

## Published source and community

- [UniRouter](https://github.com/foigent/unirouter),
  [integration-control](https://github.com/foigent/integration-control) and
  [event-connector-spec](https://github.com/foigent/event-connector-spec) are public
  under Apache-2.0 for project-owned material. Third-party terms remain applicable.
- Event Connector Spec and Integration Control are shared protocol/verification
  components, not additional end-user products. [X-Insight source](https://github.com/foigent/x-insight) is public; the [Foigent overview](https://github.com/foigent) links the
  available product entry points without requiring private-repository access.
- The owner approved publication of the existing Git history and retained records
  after the bounded disclosure review below. No history was rewritten or deleted.
- GitHub private vulnerability reporting is enabled for all three repositories.
  UR's English About/topics and Discussions are enabled; conduct concerns use
  the contact in the [Code of Conduct](../../CODE_OF_CONDUCT.md).
- Three [starter contribution issues](starter-issues.md) are public with bounded
  tasks, code/document entry points, acceptance criteria and newcomer labels.
  [Discussions](https://github.com/foigent/unirouter/discussions) includes Q&A,
  Ideas, Show and tell, and the default maintainer Announcements category.
- Exact licensed dependency sources and accompanying notices were downloaded
  anonymously and compared with the reviewed Git bytes. All five historical N1
  assets matched their original SHA-256 values. The [N1 download page](https://github.com/foigent/integration-control/releases/tag/v0.1.0-candidate.1)
  links the complete accompanying license materials; the assets remain unchanged.

## Verified community bootstrap source — 1.11.0

[PR 448](https://github.com/foigent/unirouter/pull/448) merged the Linux amd64
download/verify/prepare entry, delegating installation and resume to the original
installer. Source `87b0a6dc600436b213a8cf943e3c68f188083c7b`, tree
`247594ff534c886b91b61ccf1dc0bedd139167de`, passed all ten synthetic native
checks in [run 38109442307](https://github.com/foigent/unirouter/actions/runs/38109442307).
[Full CI 38109442312](https://github.com/foigent/unirouter/actions/runs/38109442312)
also passed Change Routing, Backend Tests, Frontend Build, PR4 Browser E2E and
Docker Build. Main merge `7f397f6d2e76b353ee3d3ccb148171fa9298b2ad` preserves
the tested tree. Documentation-only [PR 449](https://github.com/foigent/unirouter/pull/449)
merged as `300592e36c28e8836e37c1a2085401b8701d29d6`, which passed
[exact-main protocol run 38111877810](https://github.com/foigent/unirouter/actions/runs/38111877810).
The tested implementation is unchanged between these source and documentation merges.

The [accepted native result](../ops/COMMUNITY-INSTALLER.md#accepted-native-bootstrap-result--2026-10-11)
binds the report and fixture identities. English installation guidance is primary,
with a Chinese companion. At that historical 1.11.0 checkpoint, official trust
adoption, signed assets and public downloads were pending. This result used local fixture transport,
temporary signing trust and zero provider calls.

## Previously accepted implementation and installation — 1.10.0

The accepted 1.10.0 candidate is `71b5a4ee72d649d08e1137e68edb8b4d866f0987`,
tree `463c5de2405e52cce44423c0f1fa7cfcd6cb6a61`. It was merged through
[PR 442](https://github.com/foigent/unirouter/pull/442) as
`345a614d958eff041460e382e3e25b21556b963b`, with the same tree.

| Evidence | Result and boundary |
| --- | --- |
| Required CI | [38067610362](https://github.com/foigent/unirouter/actions/runs/38067610362): Change Routing, Backend Tests, Frontend Build, Browser E2E and Docker Build passed |
| Exact-main protocol | [38073239078](https://github.com/foigent/unirouter/actions/runs/38073239078) passed at merged main `345a614d` |
| Fresh installation | [38067610346](https://github.com/foigent/unirouter/actions/runs/38067610346): all eight synthetic Linux amd64 checks passed, including interruption/resume, login and no commercial License |
| Existing M125 upgrade and recovery | [38067610388](https://github.com/foigent/unirouter/actions/runs/38067610388): all ten checks passed, including real Docker/NGINX withdrawal, clean stop, M126 preservation checks, admission and restore into a separate PostgreSQL instance |

These native tests used temporary signing trust and synthetic data on disposable
GitHub Ubuntu machines. No existing ECS installation was upgraded, no provider
request was made, and no official package, image or signing identity was published.
See the [installer runbook](../ops/COMMUNITY-INSTALLER.md) and
[maintenance result](../ops/COMMERCIAL-LICENSE-RETIREMENT.md#accepted-native-maintenance-result--2026-10-11).

## Public contributor CI

The reviewed public CI activation removes the private App-token dependency,
retains the exact dependency commits/trees and all five N1 asset checks, and
uses only the read-only GitHub job token. No privileged PR event or reduced
test suite is introduced. Actual cross-repository [PR 443](https://github.com/foigent/unirouter/pull/443)
from `nicholaszming/unirouter` passed all six checks: the five standard jobs in
[CI 38073940176](https://github.com/foigent/unirouter/actions/runs/38073940176)
and [protocol 38073940188](https://github.com/foigent/unirouter/actions/runs/38073940188).
The tested fork head is `ba249cf767d3b07118630417e75ec20d08d663dc`; CI checked
out PR merge `ec59f102beff070991b377d052e7f4fe91087ac2`, with the same tree
`d3b04759106281835bfc0aa3f2611b4c503584c2`. Job logs confirm the public
qualifier/action inputs and read-only token permissions. No private App secret
is referenced by either workflow. Existing required checks and branch protection
remain unchanged. Existing App secret revocation is outside this batch.

## Public two-product walkthrough — pending

The [existing integration entry](getting-started.md#optional-x-insight-integration)
links UniRouter's setup API, credential lifecycle and recorded combinations.
The earlier Target observations retain their specific versions and scope;
UniRouter 1.11.0 installation/protocol CI does not establish a new joint result.
Public installation/invitation/reader guidance and an exact synthetic
execution-to-review acceptance remain necessary for a
newcomer to reproduce the complete path using public sources.

## Remaining public-distribution work

Publish the six exact preview assets and qualify anonymous source/material
downloads, then make the two fully mapped image packages public and verify all
six retained versions. Complete the final consumer/documentation binding PR and
required protected checks, then execute the existing bounded real public-package
consumer once. Until those actual results exist, do not advertise a completed
one-command public installation or a supported production release.

The product has no npm installer; its Linux bootstrap uses Python and an
already-installed Docker Engine/Compose. The adopted public ring, real signature
and fixed source/NOTICE materials need no recreation. Existing ECS changes,
provider calls and the full two-product newcomer walkthrough retain their own
target and evidence boundaries. Older distribution notes below are historical,
not an instruction to repeat the settled key decision or old signing operation.

<details>
<summary>Historical preparation record (superseded by the current status above)</summary>

Status: **private draft [PR 442](https://github.com/foigent/unirouter/pull/442);
not ready for public release**.
Implementation candidate `b7432803a8ae7ef3b946501ae5f3e9ff0a905273`
passed all seven checks: [CI 38039244481](https://github.com/foigent/unirouter/actions/runs/38039244481),
[protocol 38039244482](https://github.com/foigent/unirouter/actions/runs/38039244482)
and [native installer 38039244599](https://github.com/foigent/unirouter/actions/runs/38039244599).
Native acceptance checked out that exact head. Standard CI and protocol used
PR merge commit `a66e19c54955130801de4292b51a356cab1d310c`, with the same
tree `0c52df41786c09bc5c5fe1078e45788f5278c137`.
This is not exact-main or public-release evidence. English-first documentation
candidate `096e15343e2c8fb9348240fa30757cc9c990d78b` also passed all seven
checks: [CI 38044957141](https://github.com/foigent/unirouter/actions/runs/38044957141),
[protocol 38044957124](https://github.com/foigent/unirouter/actions/runs/38044957124)
and [native installer 38044957126](https://github.com/foigent/unirouter/actions/runs/38044957126).
Native acceptance used that exact head; the other workflows used PR merge
commit `19e6fae867d95f843fda7ca5a51349595618989b`. The dependency-carrier
head `6bd3ae192c9e30fc17b68fc3bace5a7fcdc86497` also passed all seven checks:
[CI 38054463214](https://github.com/foigent/unirouter/actions/runs/38054463214),
[protocol 38054463203](https://github.com/foigent/unirouter/actions/runs/38054463203)
and [native installer 38054463238](https://github.com/foigent/unirouter/actions/runs/38054463238).
Its PR merge commit is `b0493da5781d0a050ca6bfdff3d3d7ced23abcd4`; both head
and merge trees are `d96837b958ae73bfdd8dd063e05dd70f663d6ab6`. These
results do not cover the separately prepared native maintenance test.
Main read back on 2026-10-10:
`59c47b852b7372951c5c49b0e6b23df8c83817b9`, version 1.9.8.
This task has not changed private visibility.

## Accepted direction

Apache-2.0 core; retirement of runtime commercial License requirements;
preserved authorization, financial integrity, signatures and history;
independent UR use with optional asynchronous X-Insight integration.
Services and future independent commercial additions may be offered separately.
The owner's [Why UniRouter](why-unirouter.md) essay
([简体中文](why-unirouter.zh-CN.md)) and the fixed-version
[source comparison](gateway-design-comparison.md) are included in the candidate.
English is the primary public documentation/community language; Simplified
Chinese is secondary. Both remain welcome for questions and contributions.

## Publication conditions

| Condition | Remaining work / evidence |
| --- | --- |
| Reader/contributor entry | Candidate bilingual README and design rationale, source comparison, architecture, data guide, contribution map, DCO, reporting and roadmap |
| Runtime License retirement | Local PG16 migration/refusal, retained history and new-runtime admission passed; native maintenance harness/review/offline checks and separate GitHub execution approval are complete, while its actual Docker/NGINX result remains pending |
| Installation/first request | Fresh Linux amd64 installation, interrupted-plan recovery, repeat resume, login and retired-License status passed at `b7432803`; 23 offline cases and signature interoperability also passed. Public assets and affected first-use behavior need their own acceptance; existing ECS evidence retains its recorded versions |
| External Fork CI | Owner approved both dependency repositories under Apache-2.0; licensed private candidates passed local gates. Public access and a real fork run remain pending |
| Copyright/dependencies | Existing Foigent attribution retained; contributor authority and redistribution review need completion |
| Source/history exposure | Review before visibility change; no unapproved deletion of historical material |
| Confidential reporting | Owner selected GitHub private vulnerability reporting and the conduct email in the Code of Conduct; verify the private GitHub form before launch |
| Public artifact/support | No published artifact or support window created |
| GitHub settings | About/topics, Discussions and labels await reviewed publication action |

## Verified publication gaps — 2026-10-10

- **External contributors:** the owner approved publishing both complete
  dependency repositories under Apache-2.0 on 2026-10-10. Private licensed
  candidates and full local verification are prepared below. Active private CI
  still uses its existing read-only App token until those repositories are
  public. A separate activation patch removes that cross-repository dependency;
  it must retain the exact integrity checks and pass from a fork without
  repository secrets before public reproducibility is claimed.
- **Dependency review:** the frontend lockfile declares licenses for all 272
  production dependency entries (254 MIT, 13 ISC, 3 BSD-3-Clause, 1 0BSD and
  1 MIT AND ISC). This is a metadata inventory, not a completed redistribution
  review. Release notices still need to cover the actual Go modules, frontend
  output, six runtime images and their contents at the selected digests. The
  root NOTICE does not substitute for third-party notices. No image digest has
  been selected for the public installer yet.
- **Confidential reporting:** the owner selected GitHub private vulnerability
  reporting and supplied the private conduct contact now recorded in the
  [Code of Conduct](../../CODE_OF_CONDUCT.md). No test email was sent.
  The authenticated GitHub reporting read returned 404 for the still-private
  repository; verify the private form as part of the approved publication
  action before announcing the project. Selection is not availability proof.
- **Source/history:** the saved advertised heads/tags of all three repositories
  were inspected across 12,416 distinct reachable Git blobs for six plaintext
  key/token patterns. The 12 matches were header-only negative-test literals;
  no other matching candidate appeared. Current operational/evidence text
  review covers 192 paths, with 56 containing filesystem, IP or cloud-instance
  references. These include examples and historical development/backup records.
  All nine documented PostgreSQL DSNs point to loopback; password-bearing
  contexts are documented test defaults. This does not clear other credential
  formats, embedded/compressed content, all Actions logs/discussions, other
  contributors' rights or the suitability of internal records for publication.
  No historical material has been removed or rewritten.
- **Actions disclosure check:** 332 complete archives (311 coverage files and
  37 expected JSON entries) matched their metadata digests and passed expected
  shape/six-pattern checks. Three snapshot archives reached their recorded
  retention expiry and return 404; they remain uninspected. Fifteen sampled
  log archives had no pattern matches; one further log could not be completely
  read because its download ended with EOF. This is bounded evidence, not
  clearance of every historical log or credential format. No artifact/log was
  deleted. Exact results are retained in the private review directory.
- **Native installation:** the independent disposable Ubuntu workflow now
  verifies real installation and recovery. See the
  [accepted result](../ops/COMMUNITY-INSTALLER.md#accepted-native-result).
  It uses temporary signing trust, a loopback registry and synthetic data;
  official distribution, existing-database maintenance and provider calls
  remain outside that result.

## Accepted dependency publication scope — 2026-10-10

The owner approved both complete repositories adopting the same Apache-2.0
license as UR. This supersedes the extracted-consumer-kit proposal; no second
kit or reduced test suite is needed. X-Insight is planned for later and is not
included in this batch's edits or publication actions.

| Repository | Licensed private candidate | Preserved identity and validation |
| --- | --- | --- |
| [integration-control PR 1](https://github.com/foigent/integration-control/pull/1) | `52bf197fafef2c55ff7717887b2396fa9df7f877`, tree `d4b23e3903b4173c7f6aa1f7046221ada935e4aa` | 14 additive license/community files; N1 `v0.1.0-candidate.1`, historical source and five assets unchanged; full D1 passed with clean-worktree and official-origin checks |
| [event-connector-spec PR 10](https://github.com/foigent/event-connector-spec/pull/10) | `e01527350ab38d02c2bfdefe090ff72d6e48e749`, tree `b78b6cb590b72c30d8705cfdb9418969a2255c7e` | 13 additive files; protocol 2.0.2, all existing bytes, manifests and tags unchanged; authority/predecessor and action-snapshot kits passed, 83 current cases / 29 classes |

Both add English-first `.github/README.md` landing pages, Chinese navigation,
contribution and reporting instructions, and retained third-party notices.
Their original root READMEs remain unchanged; the protocol README is
release-bound. The committed action snapshot contains the new license files.

UR changes only the N1 qualifier commit/tree and protocol action commit to
these licensed descendants, plus the existing test's exact action expectation.
Runtime `ProductProtocolCommit`, Source fact provenance, product pins, canonical
release digests and N1 release assets remain unchanged. Independent pre-code
review confirmed this distinction; no product/runtime version changes result.

Historical N1 source archives and binaries cannot acquire a LICENSE through a
later commit. Its `docs/LICENSING.md` explicitly grants the owner-approved
license for the historical sources and records all five asset SHA-256 values.
Full Apache-2.0, NOTICE and Go license materials have been exported from the
licensed commit as a separate private review copy. Deliver and verify those
materials alongside public downloads, outside the strict five-file qualifier
input; do not add a sixth asset to the original release or rewrite its hashes.

An ordinary integration-control fork can run tests, race checks, vet, schemas
and fixtures. The complete distribution gate checks the official Git origin
and runs in upstream PR CI. Documentation preserves that distinction instead
of instructing contributors to bypass the origin check.

The public authentication change is prepared separately and remains inactive
while the repositories are private. It removes the App-token mint step and
uses only the job's read-only `github.token` for public checkout/release reads.
It preserves workflow permissions, fixed commits/trees, all five assets and
qualification checks, with no privileged PR event or skipped tests. Bind the
patch to the exact resulting UR commit/tree and recheck it at activation.
Private App credential revocation is a separate cleanup action.

Publication still needs source/history disclosure review, public reporting and
license delivery, anonymous exact-input access, and an actual fork CI run
without private credentials. Local and private CI results do not establish
those external facts. No repository visibility has changed.

## Third-party distribution findings

Private source notice materials now contain 390 unmodified license/notice
files for 42 Go runtime modules, Go 1.25.12 and all 272 frontend production
lockfile entries. Installed frontend versions and every copied file's SHA-256
were verified. The missing root MIT license for `victory-vendor` is supplied
from its exact upstream v36.9.2 source alongside its 13 included vendor licenses.
The manifest SHA-256 is
`a43cd67541c0581684be4de1c8d8d5705bf207637d6837190887f2b75c81a6fc`;
review materials are in
`/private/tmp/unirouter-component-opensource-20261010/source-notice-review/`.
They have not been signed, published or included in release images. Actual
binary/frontend contents, inline attribution, OS/image components and required
source delivery still need verification for the exact public package.

The tested Redis digest
`sha256:858f009f9709ce576febc734aa78b8f6d624b82571f9ddb6bda4377c833b3499`
matched `7.4.11-alpine`. Redis 7.4 uses RSALv2 or SSPLv1, rather than the BSD
license of 7.2 and earlier. See [Redis's license table](https://redis.io/legal/licenses/).
The tested Grafana 11.0.0 source uses
[AGPLv3](https://github.com/grafana/grafana/blob/v11.0.0/LICENSE).
These dependencies do not become Apache-2.0 when UR adopts that license.

Before a public package is signed, select exact dependency versions/digests,
review the intended redistribution and service model, and include the required
licenses, notices and source materials. The installer currently requires all
six services; making monitoring optional or changing the Redis implementation
would be a separate reviewed compatibility change, not a documentation fix.
No dependency has been silently replaced or downgraded.

## Proposed GitHub configuration

Reuse historical ECS evidence for unchanged behavior; do not repeat all model
acceptance solely because the source becomes public. New-user installation and
the 1.10.0 License-retirement boundary require their own bounded checks. See the
[Linux installer scope](../ops/COMMUNITY-INSTALLER.md) and the retained
[Target onboarding evidence](../ops/evidence/DELIVERY-ONBOARDING-177-20260909.md).

These values are prepared for review; none has been applied to GitHub.

- About: `Model execution, policy and accountable usage for enterprise AI.`
- Topics: `ai-gateway`, `llm-gateway`, `model-routing`, `ai-governance`,
  `usage-metering`, `self-hosted`, `openai-compatible`.
- Website: leave unset until a maintained public documentation site exists.
- Discussions: Q&A for usage, Ideas for larger proposals, Show and tell for
  integrations, and maintainer-only Announcements.
- Labels: retain existing labels; add missing `good first issue`, `help wanted`,
  `documentation`, `bug`, and `enhancement`. Mark starter issues only after
  a maintainer provides a bounded problem, entry points and acceptance criteria.
- Security: verify private vulnerability reporting and an accessible contact
  before changing visibility. Keep sensitive reports out of public issues.
- Branch/CI: preserve required checks and existing protection; establish an
  approved public conformance path for fork contributions before publication.

The reviewed publishing batch should identify its exact commits, rights/history
review, confidential channels and activation plan. Publish the reviewed
component repositories before activating UR's public authentication patch;
verify anonymous inputs and real fork CI before announcing contributor
readiness. Changing UR visibility is a separate explicit publication action.

Do not expose secrets to forks, use privileged PR execution, or turn missing
private conformance into a passing check. Additional public checks cannot
silently replace required evidence.

[Execution record](../agent-workflows/EXECUTION-RECORD.md) records actual checks
and unresolved conditions. Local, CI, artifact, target and provider-live
evidence are separate.

Before publication, review exact source/history scope, attribution, contacts,
installation evidence and dependency distribution. Commit, push, PR, public
visibility, release and deployment require their corresponding authorization.

</details>
