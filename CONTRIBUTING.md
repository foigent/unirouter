# Contributing to UniRouter

Documentation, reproducible bug reports, tests, integrations and code
contributions are welcome. [简体中文项目入口](README.zh-CN.md).

Start with the [contribution map](docs/community/contribution-map.md).
The project is maintainer-led: maintainers decide scope, compatibility and
merges; contributors can propose changes through public issues and pull requests.
Use the [Code of Conduct](CODE_OF_CONDUCT.md). Report sensitive issues through
[Security](SECURITY.md), not a public bug report.

## Language

English is the primary language for public documentation, release notes, issue
and pull request templates, and maintainer announcements. Simplified Chinese
is the secondary language. When translations differ, the English document is
the canonical version; report or fix the discrepancy.

Questions, issues and contributions in either language are welcome. Prefer
English for issue and PR titles so the community can find related work. For
substantial decisions discussed in Chinese, include a short English summary
of the problem and accepted outcome; maintainers can help with that summary.
Fluent English is not a contribution requirement.

Use the unsuffixed Markdown path for an English guide and `.zh-CN.md` for its
Simplified Chinese translation, with links between the two when both exist.
GitHub landing pages, templates and new release summaries start in English.
The [documentation index](docs/README.md) is the default reading entry.

Keep the two README entry points aligned when changing product positioning,
installation requirements or contribution guidance. Mark stale translations
clearly and link to the current English source. Historical records retain
their original language; translate active contributor documentation as it is
maintained rather than rewriting historical evidence.

## Before you start

For a small, well-understood correction, a focused PR with its validation is
enough. Discuss substantial behavior, API, migration, routing, financial,
identity or installation changes before implementation. These paths require
the independent pre-code review specified in [AGENTS.md](AGENTS.md).

Explain the user-visible problem and the expected result. A feature request
does not need a complete implementation design. For an issue you want to take,
leave a comment first so maintainers can confirm scope and avoid duplicate work.

## Fork and branch

Once the repository is public, fork it in GitHub, then:

```sh
git clone https://github.com/YOUR-ACCOUNT/unirouter.git
cd unirouter
git remote add upstream https://github.com/foigent/unirouter.git
git switch -c fix/short-description
```

Keep unrelated work separate. Preserve existing worktree changes. AI-assisted
contributions follow the same review and evidence requirements; the submitting
author remains responsible for understanding the change and its tests.

## Development prerequisites

- Go 1.25.12, matching `go.mod` and CI.
- Node.js 24.19.0 and npm 11.17.0, matching `frontend/package.json`.
- PostgreSQL 16 for database-backed tests; Redis 7 where required.
- Docker/Compose for the container build and deployment path.

Do not run database tests against a customer or shared production database.
Tests can create, migrate and remove their own databases. Use a disposable
local test environment and read [the integration runner](scripts/test-backend-integration.sh)
before supplying test connection settings.

### Frontend

```sh
cd frontend
npm ci
npm run type-check
npm test
npm run build
```

### Backend

Start with the package affected by your change:

```sh
go test ./internal/YOUR_PACKAGE -count=1
```

Before submitting a backend change, run the applicable broader checks:

```sh
go vet ./...
go test ./... -count=1
```

These commands alone do not prove database integration passed. DB tests may
skip without their documented environment; report skipped tests. The existing
integration entry point is `scripts/test-backend-integration.sh`; its flags,
required services and scope are documented in that script. Do not use real
provider calls as part of ordinary contributor tests.

**Current external dependency limitation:** parts of the required backend and
protocol CI need private `foigent/integration-control` and
`foigent/event-connector-spec` inputs. Fork PRs cannot currently reproduce
those complete checks. Maintainers must resolve the distribution/access
boundary before declaring contribution CI ready. Do not pass secrets to
untrusted forks, change to privileged PR execution, or treat skipped
conformance as passed. See [release readiness](docs/community/release-readiness.md).

## Version and compatibility

Runtime, API, installation or upgrade changes update `VERSION`,
`CHANGELOG.md`, `frontend/package.json` and `frontend/package-lock.json`
consistently. Pure documentation and test-only changes follow the exemptions
in [engineering governance](docs/governance/README.md).

Run `scripts/check-release-metadata.sh` with the comparison base appropriate
to your branch. Existing `Change Routing`, `Backend Tests`,
`Frontend Build` and `Docker Build` coverage remains authoritative.
A green local check does not establish CI, deployment or production acceptance.

## Submit a pull request

Use a short Conventional Commit-style title such as
`fix(billing): preserve the usage source on retry`.

Describe the problem, resulting behavior, relevant tests and any compatibility
or migration effect. Include the issue if one exists. For UI changes, provide
a relevant screenshot from synthetic or sanitized data. Never include keys,
session cookies, real prompts, customer records or internal deployment details.

Maintain readable commits. Use `gofmt` for Go; follow the existing frontend
style. Tests should cover behavior and regressions, including rejected states
when changing authorization or financial paths.

## Contribution license and DCO

Unless explicitly agreed otherwise before submission, contributions to project
code are submitted under [Apache-2.0](LICENSE). Third-party material must have
a compatible license with its attribution preserved.

By adding a `Signed-off-by` trailer, you certify the
[Developer Certificate of Origin 1.1](https://developercertificate.org/):
you have the right to submit the contribution under its indicated license.
This is a provenance declaration, not a transfer of copyright.

```sh
git commit -s -m "docs: explain the first-request walkthrough"
```

Use an identity and email appropriate for a public, enduring contribution
record. Sign off only your own certification; do not manufacture another
author's sign-off. Maintainers check sign-offs during review; this candidate
does not claim an automated DCO gate is installed.

## Maintainer references

[AGENTS.md](AGENTS.md) and [engineering governance](docs/governance/README.md)
define review, authorization and validation requirements. Internal execution
and historical evidence records are available in the
[documentation index](docs/README.md); a contributor does not need to recreate
an internal campaign history to propose a focused change.

Community support is provided as maintainer capacity permits. A roadmap entry
or discussion is not a delivery date or a support SLA.
