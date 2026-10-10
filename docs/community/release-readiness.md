# Open-source release readiness

Status: **private draft [PR 442](https://github.com/foigent/unirouter/pull/442);
not ready for public release**.
Candidate `c5958c57106e7073e74653652b38dc549f482e10` passed all six checks in
[CI 38032678754](https://github.com/foigent/unirouter/actions/runs/38032678754)
and [protocol 38032678752](https://github.com/foigent/unirouter/actions/runs/38032678752).
This is the prior implementation baseline. Newer candidate checks are visible
in [PR 442](https://github.com/foigent/unirouter/pull/442/checks); CI evidence
does not substitute for native installer or public-release acceptance.
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
| Installation/first request | Existing ECS model/onboarding evidence is retained for its recorded versions; Local fresh/resume and real restore/rebase passed. Linux installer code, 23 offline cases and real signature interoperability passed; the native workflow records exact-commit Docker installation results, while public assets and the affected first-use path need their own acceptance |
| External Fork CI | Private integration-control and event-connector-spec inputs block complete reproducibility; distribution/access strategy unresolved |
| Copyright/dependencies | Existing Foigent attribution retained; contributor authority and redistribution review need completion |
| Source/history exposure | Review before visibility change; no unapproved deletion of historical material |
| Confidential reporting | Working security and conduct channels must be confirmed before launch |
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
- **Confidential reporting:** the authenticated GitHub private-reporting read
  returned 404 for the still-private repository. This does not establish that
  reporting will work after publication; confirm an accessible private channel
  before launch. Do not publish an unverified contact address.
- **Source/history:** user-authored public-facing essays are reviewed, but that
  does not clear the entire repository history, internal operational records
  or other contributors' rights for disclosure. A content-pattern triage of 189
  tracked Markdown files under `docs/ops` and `docs/agent-workflows` found 41
  files with local filesystem references and 3 with private-IPv4-shaped values.
  These may include examples; they are review candidates, not confirmed secret
  leaks. Preserve existing material while reviewing the exact intended
  publication scope.
- **Native installation:** this host has no Docker, Podman or Colima command.
  The existing Docker Build check builds a release-mode image and validates
  Compose, but does not execute the community installer. The authorized bounded
  acceptance batch is in [COMMUNITY-INSTALLER.md](../ops/COMMUNITY-INSTALLER.md#native-acceptance-batch).

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
