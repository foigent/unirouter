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

## Signed Linux distribution

Signed Linux amd64 previews, a public verifier and release-matched
installation instructions are available. The
[release status](docs/community/release-readiness.md) records exact package
identities, isolated installation and upgrade results, and open evidence by
version. Continue improving first use, upgrade and recovery.
[Install on Linux](docs/community/install-linux.md) · [Signed previews](https://github.com/foigent/unirouter/releases)

## UniRouter and X-Insight walkthrough

Both [UniRouter](https://github.com/foigent/unirouter) and
[X-Insight](https://github.com/foigent/x-insight) publish source and Linux
installation instructions. The [integration guide](docs/community/getting-started.md#optional-x-insight-integration)
starts from an explicitly authorized connection. The
[architecture notes](docs/community/architecture.md#integration-entry-and-verified-scope)
record tested version pairs and their evidence.

We welcome a repeatable public walkthrough that follows one synthetic
execution from UniRouter usage and billing evidence to authorized X-Insight
reader access, and exercises the denied-access case. Record exact versions,
shared interfaces and observed steps with the result.

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
The public UniRouter and X-Insight sources use Apache-2.0 for project-owned
material. We develop the public products without a community/commercial
edition split. See [licensing and product scope](docs/community/commercial-boundaries.md).

Read the owner's [Why UniRouter](docs/community/why-unirouter.md) essay
([简体中文](docs/community/why-unirouter.zh-CN.md)) for the motivation, design
choices and longer-term research questions behind this direction.
