# Contribution map

Choose a narrow problem and its expected observable result. These entry points
are not already assigned or approved issues.

| Area | Entry | Useful first contribution |
| --- | --- | --- |
| Onboarding | READMEs and `docs/community/` | Reproduce a step and clarify a missing prerequisite |
| Console | `frontend/src/pages/`, `navigation.tsx` | Accessible labels or a focused state/rendering regression |
| API examples | `docs/tech-ref/05-openapi-spec.yaml` | A verified synthetic example |
| Providers | `internal/gateway/`, `capability-bundles/` | A protocol edge case with bounded fixtures |
| Model governance | `internal/modelgov/`, `internal/admin/model_deployment*` | Explain a qualification failure or test an accepted contract |
| Usage/billing | `internal/metering/`, `internal/billing/` | Preserve actual/estimated provenance in a regression |
| Evidence | `internal/sourcefacts/`, `internal/completedfeed/` | Missing-data/replay regressions under the existing contract |
| X-Insight | `internal/xinsightconnector/`, `contracts/` | Product-local compatibility reproduction |
| Installation | `internal/migrate/`, `deploy/` | Reproduce an issue while preserving identity and trust checks |

Use nearby package tests and frontend `__tests__`. Report skipped integration
tests. Routing, financial, identity, migration and release changes require the
pre-code review in [AGENTS.md](../../AGENTS.md); contributions can begin with
a reproduction before implementation.

A good beginner issue identifies the symptom, entry file, expected behavior,
verification and a maintainer able to review it. Apply `good first issue`
or `help wanted` only after confirming those details.

Start with the [three published contribution tasks](starter-issues.md), or browse
[open beginner issues](https://github.com/foigent/unirouter/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22).

Follow [CONTRIBUTING](../../CONTRIBUTING.md) for development, Fork/PR and DCO.
