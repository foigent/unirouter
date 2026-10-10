# Getting started

**A reproducible no-commercial-License installation is still a release
prerequisite for this candidate.** Check
[release readiness](release-readiness.md). Development Compose startup alone
does not establish an installed, qualified model-serving deployment.

## Choose a path

- Linux server / ECS: see the [simplified installer preview](install-linux.md).
  Its local implementation is separate from the still-pending public package
  and real Docker acceptance.
- Read/contribute: follow [development prerequisites](../../CONTRIBUTING.md).
  No provider credential or paid request is needed for isolated unit tests.
- Empty database: use the exact artifact's
  [fresh-install owner](../ops/FRESH-INSTALL.md), then
  [first-customer bootstrap](../ops/deployment-runbook.md#102-启动首装登记与读回).
- Existing installation: follow its release-matched upgrade/recovery contract.
  Preserve the database; do not reset it to bypass an installation error.
- Signed release: use [the customer template](../../deploy/customer/README.md)
  with an actual verified artifact. This candidate is not a published release.

Once public:

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

The 1.10.0 candidate requires no commercial License import. Follow the
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
