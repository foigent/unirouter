# UniRouter documentation

[Foigent](https://github.com/foigent) · [简体中文](README.zh-CN.md) · [Project overview](../README.md)

English is the primary language for public guides and project updates.
Start with the path below that matches what you want to do. Historical
engineering records retain their original language and evidence boundaries.

## Understand UniRouter

| Question | Guide |
| --- | --- |
| Why build it, and what are the tradeoffs? | [Why UniRouter](community/why-unirouter.md) · [简体中文](community/why-unirouter.zh-CN.md) |
| How does it work, and where does X-Insight fit? | [Architecture](community/architecture.md) |
| How do selected gateway implementations differ? | [Source-based design comparison](community/gateway-design-comparison.md) |
| What is recorded, retained or sent elsewhere? | [Data handling](community/data-handling.md) |
| Understand the open-source product scope and license terms | [Licensing and product scope](community/commercial-boundaries.md) |
| What is being worked on? | [Roadmap](../ROADMAP.md) |

## Install and make a request

1. Read the [Linux/ECS installer preview](community/install-linux.md)
   ([简体中文](community/install-linux.zh-CN.md)) and its prerequisites.
2. Follow [Getting started](community/getting-started.md) to configure model
   access, make a request and inspect usage and the execution receipt.
3. Consult the [API contract](tech-ref/05-openapi-spec.yaml) and
   [client examples](../examples/client-compatibility/README.md) for integration.
4. To combine execution with analysis and review, follow the
   [X-Insight integration entry](community/getting-started.md#optional-x-insight-integration).
   X-Insight source publication and the public joint walkthrough are pending.

This is an unreleased candidate. Native installation has passed on a disposable
Linux amd64 runner with synthetic data; a public download is still pending.
See [release readiness](community/release-readiness.md) for the exact evidence
and remaining publication work. Existing deployments must use their
release-matched migration and recovery procedures.

## Contribute

- [Contributing](../CONTRIBUTING.md): development setup, validation, language
  policy and the Developer Certificate of Origin.
- [Contribution map](community/contribution-map.md): find the relevant code
  and documentation for a focused change.
- [Starter contribution issues](community/starter-issues.md): three published
  tasks with entry points and acceptance criteria; check each issue's availability.
- [Security reporting](../SECURITY.md) and [Code of Conduct](../CODE_OF_CONDUCT.md).
- [Changelog](../CHANGELOG.md): the current candidate entry is in English;
  older release records retain their original language.

English and Simplified Chinese questions and contributions are welcome.
Maintainers help summarize substantial Chinese discussions in English.

## Engineering and operations

The references below include English and Chinese technical documents. Their
accepted contracts and recorded versions matter; an index entry does not
establish that a deployment is current or authorize a new operation.

| Task | Reference |
| --- | --- |
| Follow contribution review and compatibility requirements | [Project rules](../AGENTS.md) and [engineering governance](governance/README.md) |
| Find implementation paths | [Codebase map](CODEBASE-MAP.md) and [technical reference index](tech-ref/README.md) |
| Understand product responsibilities | [PRD Phase 8](PRD-Phase8.md) and [boundary matrix](ops/PRD8-BOUNDARY-MATRIX.md) |
| Inspect installation and recovery contracts | [Community installer](ops/COMMUNITY-INSTALLER.md), [fresh installation](ops/FRESH-INSTALL.md), [License retirement](ops/COMMERCIAL-LICENSE-RETIREMENT.md) |
| Operate a release | [Deployment runbook](ops/deployment-runbook.md) and [signed customer release template](../deploy/customer/README.md) |
| Check recorded status and active work | [Current status](ops/CURRENT-STATUS.md), [execution record](agent-workflows/EXECUTION-RECORD.md) and [open issues](issues/OPEN-ISSUES.md) |
| Distinguish accepted, draft and historical material | [Document inventory](DOC-INVENTORY.md) |

The [Chinese engineering and historical index](README.zh-CN.md) preserves
the full earlier navigation, including design, testing and phase evidence.
Those records are useful for a specific investigation; you do not need to
read the project history before reporting a bug or making a small contribution.
