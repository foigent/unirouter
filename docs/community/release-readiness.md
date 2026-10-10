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
This is not exact-main or public-release evidence. Later documentation updates
have their own checks in [PR 442](https://github.com/foigent/unirouter/pull/442/checks).
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
| External Fork CI | Private integration-control and event-connector-spec inputs block complete reproducibility; distribution/access strategy unresolved |
| Copyright/dependencies | Existing Foigent attribution retained; contributor authority and redistribution review need completion |
| Source/history exposure | Review before visibility change; no unapproved deletion of historical material |
| Confidential reporting | Owner selected GitHub private vulnerability reporting and the conduct email in the Code of Conduct; verify the private GitHub form before launch |
| Public artifact/support | No published artifact or support window created |
| GitHub settings | About/topics, Discussions and labels await reviewed publication action |

## Verified publication gaps — 2026-10-10

- **External contributors:** both `foigent/integration-control` and
  `foigent/event-connector-spec` are private. Backend CI creates an App token
  from repository secrets to obtain the pinned Neutral qualifier and release;
  protocol CI uses a pinned action from the other private repository. Passing
  inside this private repository does not prove a fork can reproduce them.
  Prepare an openly redistributable conformance kit with the same pinned
  contracts, fixtures and integrity checks, or approve publication of the
  necessary dependency repositories after their own rights review. No private
  repository has been copied, relicensed or published here.
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

## Dependency publication boundary

The following is a preparation proposal, not permission to publish other
repositories or a claim that a new kit has been assembled.

| Required input | Current identity and consumer | Public preparation needed |
| --- | --- | --- |
| Neutral N1 release and qualifier | `integration-control` qualifier `db10730d64a9302887ad59c53921e98c757927e7`; release `v0.1.0-candidate.1`; Backend Tests | Rights-reviewed source archive, both declared platform binaries, manifest, checksums and qualifier scripts, retaining their exact integrity checks |
| Source-Neutral protocol consumer kit | `event-connector-spec` action `ad8fe7a38c9d7335657630baa8670dd07556b98c`; protocol 2.0.2; Protocol Pin | Action/entry scripts, v1 compatibility kit, schemas/vectors, implementation and release manifests, Go implementation with vendored dependencies, TypeScript implementation, validators and expected reports |

The protocol action executes Go and TypeScript conformance and verifies the UR
product pin. Publishing only a schema or replacing the action with a local
success marker would omit required behavior. The N1 verifier also validates
declared assets beyond the single Linux executable used by UR.

The recommended distribution is a reviewed public consumer kit containing the
complete required dependency set. It does not require the X-Insight application
repository. Its exact contents, rights, retained notices and publication target
must be accepted before changing CI. If a new public artifact has a new
identity, update the consumer binding through review; do not pretend a reduced
copy is the original private Git tree. Acceptance must run from a fork without
repository secrets while retaining all current conformance coverage.

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

The reviewed publishing batch should identify its exact commit, rights/history
review, confidential channels and fork CI results. Visibility is the last
publication action, after these conditions are met.

Do not expose secrets to forks, use privileged PR execution, or turn missing
private conformance into a passing check. Additional public checks cannot
silently replace required evidence.

[Execution record](../agent-workflows/EXECUTION-RECORD.md) records actual checks
and unresolved conditions. Local, CI, artifact, target and provider-live
evidence are separate.

Before publication, review exact source/history scope, attribution, contacts,
installation evidence and dependency distribution. Commit, push, PR, public
visibility, release and deployment require their corresponding authorization.
