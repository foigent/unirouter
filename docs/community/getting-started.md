# Getting started with UniRouter

[Foigent](https://github.com/foigent) · [Project overview](../../README.md) ·
[简体中文项目入口](../../README.zh-CN.md)

**Unreleased candidate:** fresh installation without a commercial License,
interrupted-plan recovery and console login passed on a disposable Linux amd64
runner. Public release packages are still pending. See
[release readiness](release-readiness.md) for the exact tested commit and scope.
Installation does not configure or qualify a model for you; the remaining
steps below prepare model access and verify your first request.

## Choose a path

- Linux server / ECS: see the [simplified installer preview](install-linux.md).
  Exact native acceptance is recorded separately from the still-pending
  public package; the guide links to both scope and results.
- Read/contribute: follow [development prerequisites](../../CONTRIBUTING.md).
  No provider credential or paid request is needed for isolated unit tests.
- Empty database: use the exact artifact's
  [fresh-install owner](../ops/FRESH-INSTALL.md), then
  [first-customer bootstrap](../ops/deployment-runbook.md#102-启动首装登记与读回).
- Existing installation: follow its release-matched upgrade/recovery contract.
  Preserve the database; do not reset it to bypass an installation error.
- Signed release: use [the customer template](../../deploy/customer/README.md)
  with an actual verified artifact. This candidate is not a published release.

Clone the public source:

```sh
git clone https://github.com/foigent/unirouter.git
cd unirouter
```

Read [data handling](data-handling.md) before configuring services. Keep secrets
in the supported local environment channel and out of issues/logs.

## Install and initialize

The binary, migration ledger, embedded version/commit/tree and inspected image
digest must agree. A default `local`/`unknown` build is not a substitute.

The formal owner separates `fresh-install plan`, `check`, `apply` and
`resume`. Its [runbook](../ops/FRESH-INSTALL.md) defines evidence storage,
database roles and recovery. Ordinary startup cannot bypass an empty database
or an active installation.

Complete administrator password rotation/login and the first-customer
bootstrap using the supported control surface. Any legacy bootstrap state
specified by the runbook remains isolated from client traffic.

Accepted UniRouter source since 1.10.0 requires no commercial License import;
the 1.11.0 bootstrap retains M126. Follow the
[M126 contract](../ops/COMMERCIAL-LICENSE-RETIREMENT.md) when upgrading M125;
older License instructions apply only to the older Artifact. Full installation
acceptance is tracked in [release readiness](release-readiness.md).

## Prepare model access

1. Create the customer/project context and provider connection.
2. Bind the chosen provider credential.
3. Configure the deployment, exact execution profile and execution limits.
4. Run required operation probes and inspect their evidence.
5. Author and activate the matching canonical Release.
6. Grant application access to the project/model and configure its budget.

Qualification probes and real requests can incur provider charges. Use only
the provider, credential and allowance you intend. Do not substitute synthetic
success for real qualification or bypass access/Release checks.

## Make and inspect a request

Use a model identifier eligible in your deployment. Supply `UR_BASE_URL`,
`UR_API_KEY` and `UR_MODEL` through your approved local environment:

```python
import json
import os
import urllib.request

request = urllib.request.Request(
    os.environ["UR_BASE_URL"].rstrip("/") + "/v1/chat/completions",
    data=json.dumps({
        "model": os.environ["UR_MODEL"],
        "messages": [{"role": "user", "content": "Reply with a brief greeting."}],
        "stream": False,
    }).encode(),
    headers={
        "Authorization": "Bearer " + os.environ["UR_API_KEY"],
        "Content-Type": "application/json",
    },
    method="POST",
)
with urllib.request.urlopen(request, timeout=60) as response:
    print("Request ID:", response.headers.get("X-Request-ID"))
    print(response.read().decode())
```

Use the identifier to inspect routing, final status, usage provenance and
billing/receipt records. A response alone does not prove billing finality or
business-task completion. Keep missing provider evidence explicit.

For a report, retain version, sanitized error code, request ID and reproduction
conditions; remove real prompts and credentials.

## Optional X-Insight integration

UniRouter can operate independently. Combine it with X-Insight when you want to
associate its execution, usage and economic facts with other authorized sources
for analysis and review. Facts are transferred asynchronously; X-Insight is not
part of the synchronous model request path.

X-Insight's source is **preparing for publication**. The
[Foigent overview](https://github.com/foigent) will link available product entry
points. A newcomer cannot yet reproduce the complete two-product walkthrough
from public source alone. UniRouter's completed installation checks and shared
protocol checks do not establish that joint result.

Use the existing sources for the relevant step:

- [UniRouter API contract](../tech-ref/05-openapi-spec.yaml): the organization-scoped
  `xinsight/setup-previews` and `xinsight/setups` operations define the invitation,
  preview and consent path. Administrator setup does not grant every reader
  access to the resulting facts.
- [Connection-credential lifecycle](../ops/XINSIGHT-CONTROL-CREDENTIAL-LIFECYCLE.md):
  rotation and recovery for an already established connection, with its original
  identity and authorization. It does not create a new connection.
- [Configuration and data boundaries](data-handling.md) and
  [.env.example](../../.env.example): feed and connector enablement are explicit;
  capture, content access and model-call permission have separate scopes.
- [Architecture and recorded combinations](architecture.md#integration-entry-and-verified-scope):
  pinned shared interfaces, dated Target observations and unperformed joint work.

The public walkthrough still needs X-Insight's installation/invitation and
reader-access documentation, an exact supported version/configuration pair,
and a synthetic execution whose original source/request and usage/billing
facts can be found by an ordinary authorized reader. Record the denied-access
case as well. Preserve original identifiers, amounts, units and provenance;
report unexecuted steps and gaps explicitly. These are the acceptance goals,
not new installation or authorization commands.

For a failed step, start with the corresponding API error/status, the connection
lifecycle runbook and release-matched [deployment troubleshooting](../ops/deployment-runbook.md#七故障排查).
Include sanitized versions, source/request or operation identifiers and the
exact failing step in [Issues](https://github.com/foigent/unirouter/issues);
follow [Security](../../SECURITY.md) for sensitive reports and
[Contributing](../../CONTRIBUTING.md) for patches and DCO.
