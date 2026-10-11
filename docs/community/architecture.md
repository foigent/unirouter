# Architecture and product boundaries

UniRouter (UR) is the model-execution and economic-fact foundation for enterprise
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
UR's execution decisions and ledger. The accepted direction is to open-source
both complete products under Apache-2.0. Their publication, releases and
integration availability are tracked independently; see
[licensing and product scope](commercial-boundaries.md).

Use UniRouter independently to control and inspect model execution. Use
X-Insight independently to associate evidence from its supported sources and
review findings; combine the products when UniRouter facts are useful to that
investigation. X-Insight source publication is preparing; public materials are
linked from the [Foigent overview](https://github.com/foigent).

[Event Connector Spec](https://github.com/foigent/event-connector-spec) supplies
shared protocol contracts. [Integration Control](https://github.com/foigent/integration-control)
supplies neutral offline verification for conformance and evidence packages.
These shared foundations support the products; users follow the product
installation and connection paths.
The [existing protocol binding](../tech-ref/29-foigent-event-connector-protocol-pin.md)
distinguishes CI authority 2.0.2 from Reference runtime binding 1.1.0 and Connector
1.0.0. The neutral verifier's exact release remains in the
[Integration Control pin](../../contracts/source-evidence/v1/unirouter.integration-control.n1-pin.v1.json).
These identities have separate responsibilities; updating another repository's
main does not update a supported product connection.

The long-term direction is to make AI work understandable through inspectable
facts. It does not imply all connectors, automatic remediation or business
outcome verification are already available.

## Integration entry and verified scope

[Getting started](getting-started.md#optional-x-insight-integration) points to the
existing setup API, connection-credential lifecycle and configuration sources.
The lifecycle runbook applies to an already established connection; it is not a
new-install procedure. X-Insight's own installation, invitation and reader-access
instructions must accompany the public joint walkthrough when its source is
published.

The current UniRouter 1.11.0 installation and protocol results are recorded in
[release readiness](release-readiness.md). They use synthetic installation data
and separate conformance checks; they do not qualify a new UniRouter/X-Insight
pair. The earlier [2026-10-07 Target record](../ops/CURRENT-STATUS.md) binds
UniRouter 1.9.3/M125 with X-Insight FPV16/schema45 for original-connection recovery,
authorized source-stream reception and the original reviewer's scoped content
readback. It made no new model request and retains those limits; it does not
establish a public installation-to-execution-to-review walkthrough or Production
acceptance for the newer source.

Preserve the original source and request identifiers, units, currencies and
recorded provenance across that boundary. Model execution permission and
X-Insight reader permission are separate scoped authorizations. Provider usage,
estimates, supplier-cost evidence and customer billing retain their respective
meanings and finality; missing facts remain explicit. An imported association
does not rewrite the producing system's authority or grant general content
access. See [data handling](data-handling.md) and the version-scoped
[source contract](../tech-ref/55-source-facts-v1.md) for the original distinctions;
the [source fact schema](../../contracts/source-facts/v1/source-fact.schema.json)
defines the corresponding current field representation.

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
