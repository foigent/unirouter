# Roadmap

UniRouter focuses on model execution, policy enforcement, usage, customer
billing and inspectable execution receipts. X-Insight independently handles
cross-source evidence and investigation. [Product boundaries](docs/community/architecture.md)
explain that split.

This is a direction and participation guide. Items are not release promises,
delivery dates or claims that a deployment has been verified.

## Completed source publication and verification

- UniRouter and its required protocol sources are public under Apache-2.0 for
  project-owned material, with third-party terms and notices preserved.
- Runtime commercial License requirements were retired in the accepted source;
  access control, tenant isolation, budgets, financial and audit integrity,
  signatures and data protection remain.
- Disposable Linux amd64 fresh-install, interruption/resume and M125 maintenance
  upgrade/restore checks passed. The 1.11.0 bootstrap passed all ten native checks.
- Public fork CI passed using fixed public dependency sources and a read-only
  job token. Required tests and branch protections were retained.
- English-first contribution guidance, Chinese entry points, Discussions,
  starter issues and private vulnerability reporting are available. The bounded
  source/history disclosure review is recorded.

See [release readiness](docs/community/release-readiness.md) for the exact
versions, runs and evidence boundaries. These results do not establish that an
existing ECS or Production installation has been upgraded.

## Remaining signed-distribution work

- Complete the owner's signing-key custody and recovery testing.
- Prepare and publish the official signed package, verifier, public verification
  ring and exact image identities with their applicable notices.
- Verify public downloads, anonymous image pulls and the documented first-request
  path for that release, including usage and receipts.

Official signed downloads are not available yet. Source publication and the
completed synthetic installation checks are recorded separately from this work.

## Public UniRouter and X-Insight walkthrough — pending

X-Insight source publication is preparing. When both product sources and their
formal installation/connection instructions are public, reproduce one bounded
synthetic chain: UniRouter records execution and usage/billing facts, an
explicitly authorized connection transfers those facts asynchronously, and an
ordinary authorized X-Insight reader finds the same execution and original
source with its supporting evidence. Verify the denied-access boundary as well.

Record the exact product versions, shared pins, configuration and executed steps;
keep gaps and failures explicit. This joint result cannot be inferred from two
separate product tests. Reuse the [existing integration entry](docs/community/getting-started.md#optional-x-insight-integration)
and the products' existing contracts.

## Contributions we welcome

- Clearer installation and configuration guidance.
- Reproducible reports and regression tests for supported operations.
- Provider/profile compatibility improvements with explicit evidence.
- Easier inspection of routing, usage provenance, billing state and receipts.
- Accessibility, localization and focused administration-console improvements.

Use the [contribution map](docs/community/contribution-map.md) to find the
relevant code and tests. Discuss interface, financial, routing and migration
changes before coding.

## Longer-term direction

Make model execution a reliable source of facts for enterprise AI operations.
Support asynchronous evidence consumers through explicit interfaces, preserve
source ownership, and distinguish observed facts from estimates and hypotheses.
The direction is to open-source the complete UniRouter and X-Insight products
under Apache-2.0, without a community/commercial edition split. No commercial
product is currently planned. See [licensing and product scope](docs/community/commercial-boundaries.md).

Read the owner's [Why UniRouter](docs/community/why-unirouter.md) essay
([简体中文](docs/community/why-unirouter.zh-CN.md)) for the motivation, design
choices and longer-term research questions behind this direction.
