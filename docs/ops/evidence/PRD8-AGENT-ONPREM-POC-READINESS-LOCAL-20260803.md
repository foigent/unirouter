# PRD8 Agent And On-Prem POC Readiness — Local Evidence

> Work item: `UR-PRD8-AGENT-ONPREM-POC-READINESS`
> Evidence date: 2026-08-03 (Asia/Shanghai)
> Baseline commit/tree: `cedd11e23cf2e88a9d7941b2764e25bca1e6dde2` / `b0424da04ae0931dcfd4cb08711a0717ee3d7b73`
> Branch: `codex/agent-onprem-poc-readiness`
> Status: `LOCAL_VERIFIED`; CI, release and revised Target evidence are separate

## Outcome

The repository-local implementation now provides the long-term product
foundation needed before the second integration:

- a governed `WorkloadApplication` identity, same-tenant/same-project API-key
  binding, lifecycle enforcement and an always-visible Effective Access
  Inspector;
- one backend readiness composition across client, endpoint, application,
  credential, model authorization, limit, model governance and eligible
  Provider route facts;
- versioned deployment and Gateway Endpoint desired state without storing TLS
  private keys or applying runtime changes from the UI;
- truthful Integration Manager entries for the X-Insight Reference Connector,
  Feishu bot/Aily adapters and future enterprise-system adapters;
- zero-network client contracts and examples for the OpenAI SDK, OpenAI Agents
  SDK Chat path, LangChain, AutoGen, OpenClaw and Tencent WorkBuddy;
- a read-only, fail-closed reset preview that cannot delete data, overwrite an
  existing plan file or reuse the current Source Instance; and
- an enforced production-build budget of 450 KiB per generated JavaScript
  asset.

No X-Insight investigation, Tool Gateway execution, MCP session/OAuth, real
directory/ERP integration or `/v1/responses` runtime claim was introduced.

## Product And Compatibility Boundaries

Migrations `000054` and `000055` are additive. Existing API keys remain
`legacy_unbound`; binding is optional for existing callers. An archived
application rejects future use and atomically disables active bound
credentials, while request records, billing finality, audit evidence, Outbox,
Source Snapshot bytes/hash, Feed data and Cursor/Checkpoint identity remain
untouched.

The UI does not infer execution readiness from a Provider credential alone. A
credential may be enabled while its routing channel is disabled, staged,
cooling, model-incompatible or otherwise ineligible. Only the backend-composed
intersection can report an executable route.

`GatewayEndpoint` is non-secret desired state. Public entries require HTTPS;
certificate fields accept references only and reject embedded PEM material.
Organization administrators can read the endpoint URL required for client
configuration, but certificate references and operator evidence are redacted.
Only a platform administrator can mutate deployment desired state.

## Verification

The following commands passed on the final code state represented by this
evidence:

```text
cd frontend && npm test -- --run
  54/54 files, 359/359 tests PASS

cd frontend && npm run build
  TypeScript PASS
  Vite production build PASS (4,318 modules, 78 JavaScript assets)
  largest asset DashboardPage: 376,583 bytes
  enforced limit: 460,800 bytes

go test -p 1 ./... -count=1
  PASS for all packages

go vet ./...
  PASS

go test -tags=integration -race ./internal/admin ./internal/migrate ./cmd/uniroute \
  -run '^(TestWorkloadApplication.*|TestDeploymentNetwork.*|TestWorkloadApplicationMigration.*|TestDeploymentProfileMigration.*|TestResetPreview.*)$' \
  -count=1
  PASS for all three packages

go test ./cmd/uniroute \
  -run '^(TestAdminRoutesRegisteredForFrontendClient|TestOpenAPISpecCoversRegisteredRoutes|TestFrontendDocsPageCoversRegisteredRoutes|TestOpenAPI30AvoidsUnsupportedReusableOperationReferences|TestResetCLIExposesPreviewOnlyAndValidatesBeforeDatabaseAccess)$' \
  -count=1
  PASS

go test ./internal/clientcompat ./internal/workloadapp \
  ./internal/deploymentprofile ./internal/resetplan -count=1
  PASS

bash -n scripts/poc/check-unirouter-gateway.sh
jq empty <all three compatibility JSON artifacts>
Python AST parse <all compatibility Python examples>
  PASS
```

The first unconstrained `go test ./... -count=1` run is intentionally not
reported as a pass. Package-level PostgreSQL tests saturated the local test
server and several otherwise successful tests exceeded the 20-second temporary
database teardown deadline. There were no assertion failures. The authoritative
package-serialized rerun above completed successfully, as did the focused race
suite.

Docker Compose parsing was not executed on this Mac because no local `docker`
binary is installed. Required CI and the existing ECS Docker host must both
validate the exact release configuration before Target promotion.

## Frontend Loading Evidence

The normal build completed without the historical large-chunk warning. A
separate manifest build was used only to measure generated static import
closures; it did not change source files.

| Initial path | JS assets | JS gzip | CSS assets | CSS gzip |
|---|---:|---:|---:|---:|
| Login | 19 | 359.95 kB | 10 | 56.82 kB |
| Platform overview | 25 | 349.22 kB | 11 | 38.22 kB |
| Organization & Members default account view | 33 | 443.13 kB | 18 | 69.44 kB |

These are generated static dependency closures, not measured browser request
counts. Cache, HTTP version, preload and later interaction can change actual
network behavior.

## Read-Only ECS Preflight

No target mutation occurred during this Local evidence pass. Read-only checks
found the existing stable/canary release healthy and preserved:

- public `http://8.130.18.85/health/ready`: HTTP 200;
- host ports 80, 443, 8080 and 8081 are bound, but the current Nginx
  configuration listens only on port 80;
- a direct external TLS connection to port 443 times out, so public HTTPS is
  **not** currently verified or usable;
- both `8-130-18-85.sslip.io` and `8.130.18.85.sslip.io` resolve to the ECS IP,
  which proves only DNS feasibility, not certificate or ingress readiness; and
- the root filesystem is 81% used with approximately 7.5 GB available.

Therefore the next release must not register a public Gateway Endpoint as
`observed_status=ready` until a real certificate, Nginx TLS listener, cloud
ingress rule and second-device verification all succeed.

## Remaining Gates

- Commit, required CI, merge, exact release checkout, backup, migrations
  `000054`–`000055`, non-clearing ECS deployment and authenticated browser
  verification remain to be executed and recorded separately.
- Opening cloud TCP/443 and choosing the temporary development hostname and
  certificate method are infrastructure decisions if they are not already
  available to the release operator.
- Current business/control data, Provider channels/credentials, Feed state and
  Cursor/Checkpoint must remain unchanged until the Product Owner explicitly
  starts the second integration reset.
- ORG-3 and FIN-4 still wait for a real customer directory and ERP/GL target.
