# Architecture and product boundaries

UniRouter is the model-execution and economic-fact foundation for enterprise
AI. Applications and agents use its gateway; operators use its console to
manage access, model deployment, usage and billing.

## Follow one request

1. Authenticate the caller and resolve organization/project access and policies.
2. Select an eligible model deployment and provider connection, apply its exact
   execution profile, and dispatch the supported operation.
3. Record usage provenance and settle under the applicable billing policy.
4. Inspect the request, decisions, failures and receipt. Missing evidence
   remains an explicit gap.
5. Optionally export authorized facts asynchronously for further investigation.

The implementation and [API contract](../tech-ref/05-openapi-spec.yaml)
define exact ordering, failure behavior and authorization.

## UniRouter and X-Insight

| Responsibility | Owner |
| --- | --- |
| Model admission, routing, budgets and model credentials | UniRouter |
| Customer ledger, settlement and captured supplier-cost evidence | UniRouter |
| Raw supplier billing records | The supplier |
| Request-local receipt and operational inspector | UniRouter |
| Cross-source evidence association, investigation and findings | X-Insight |
| Business outcomes and external tool effects | Their original systems |

X-Insight is optional and independent. It consumes authorized facts
asynchronously; it does not sit in the synchronous model request path or replace
UR's execution decisions and ledger. Its license, releases and integration
availability are separate from this repository's open-source preparation.

The long-term direction is to make AI work understandable through inspectable
facts. It does not imply all connectors, automatic remediation or business
outcome verification are already available.

## Deployment and code

Source Compose contains a Go backend, React/Nginx frontend, PostgreSQL 16,
Redis 7, Prometheus and Grafana. Model requests leave the deployment for the
selected provider; optional exports follow their own configuration.

- `cmd/uniroute/`: server, installation and lifecycle commands.
- `internal/admin/`, `frontend/src/`: administration API and console.
- `internal/gateway/`, `internal/modelgov/`: execution and governance.
- `internal/billing/`, `internal/metering/`: billing and usage provenance.
- `internal/sourcefacts/`, `internal/completedfeed/`,
  `internal/xinsightconnector/`: facts and controlled exchange.
- `migrations/`: historical database transitions.
- `internal/releasetrust/`, `internal/upgradebundle/`: signed release trust.

See the [contribution map](contribution-map.md) and
[accepted product boundary](../PRD-Phase8.md). Historical evidence remains bound
to its recorded version and environment.
