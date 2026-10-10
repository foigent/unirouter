# Open-source release readiness

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
follow-up below requires its own CI results.
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
| Runtime License retirement | Local PG16 migration/refusal, retained history and new-runtime admission passed; actual Docker/NGINX maintenance acceptance remains pending |
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
- **Source/history:** user-authored public-facing essays are reviewed, but that
  does not clear the entire repository history, internal operational records
  or other contributors' rights for disclosure. A content-pattern triage of 189
  tracked Markdown files under `docs/ops` and `docs/agent-workflows` found 41
  files with local filesystem references and 3 with private-IPv4-shaped values.
  These may include examples; they are review candidates, not confirmed secret
  leaks. Preserve existing material while reviewing the exact intended
  publication scope.
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

The source inventory found 42 third-party modules in the Linux amd64
`cmd/uniroute` dependency graph, each with root license/notice files. Together
with the frontend metadata above, this is an input to release notices, not a
complete inventory of the built frontend, Go standard library or image contents.

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
