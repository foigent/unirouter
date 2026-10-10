# Open-source release readiness

Status: **local preparation; not ready for public release**.
Main read back on 2026-10-10:
`59c47b852b7372951c5c49b0e6b23df8c83817b9`, version 1.9.8.
This task has not changed private visibility.

## Accepted direction

Apache-2.0 core; retirement of runtime commercial License requirements;
preserved authorization, financial integrity, signatures and history;
independent UR use with optional asynchronous X-Insight integration.
Services and future independent commercial additions may be offered separately.
The owner will write Why UniRouter later; it is not a launch gate.
English is the primary public documentation/community language; Simplified
Chinese is secondary. Both remain welcome for questions and contributions.

## Publication conditions

| Condition | Remaining work / evidence |
| --- | --- |
| Reader/contributor entry | Local candidate bilingual README, architecture, data guide, contribution map, DCO, reporting and roadmap |
| Runtime License retirement | Local PG16 migration/refusal, retained history and new-runtime admission passed; actual Docker/NGINX maintenance acceptance remains pending |
| Installation/first request | Existing ECS model/onboarding evidence is retained for its recorded versions; Local fresh/resume and real restore/rebase passed. Linux installer code, 23 offline cases and real signature interoperability passed; real Docker installation, public assets and the affected first-use path still need exact-artifact acceptance |
| External Fork CI | Private integration-control and event-connector-spec inputs block complete reproducibility; distribution/access strategy unresolved |
| Copyright/dependencies | Existing Foigent attribution retained; contributor authority and redistribution review need completion |
| Source/history exposure | Review before visibility change; no unapproved deletion of historical material |
| Confidential reporting | Working security and conduct channels must be confirmed before launch |
| Public artifact/support | No published artifact or support window created |
| GitHub settings | About/topics, Discussions and labels await reviewed publication action |

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
