# Getting started with UniRouter

[Foigent](https://github.com/foigent) · [Project overview](../../README.md) ·
[简体中文项目入口](../../README.zh-CN.md)

Start with the [Linux amd64 installer guide](install-linux.md) on your server or
ECS. [Preview downloads](https://github.com/foigent/unirouter/releases) are public;
use the exact package and checksums named by the guide. The
[release status](release-readiness.md) distinguishes published artifacts,
installation checks and upgrades on existing servers.

After installation: choose your administrator password, sign in, initialize
your customer, configure a model and check your first request. Installation
does not import a provider key, qualify a model or make a paid model request.

## Install, sign in and initialize

1. Install into a new directory and Compose project using the
   [Linux / ECS guide](install-linux.md). Keep its printed paths and project name.
2. The installer generates an initial private administrator password. To choose
   your own before your onboarding login, use the existing
   [native password command](install-linux.md#choose-your-administrator-password).
   The installer itself does not yet prompt for a password of your choice.
3. Open the console through the SSH tunnel described in that guide and sign in
   as `admin`. Keep your password and browser token private.
4. Open **System & integrations → Deployment & network** and complete
   **First customer initialization**. Its read-only preflight names the exact
   empty-seed objects before you confirm. It creates the customer
   organization separately from the platform administration organization;
   a model and provider credentials are configured in the next step.

Read [data handling](data-handling.md) before enabling capture or outbound
connections. No commercial License import is required in current UniRouter.
For an existing installation, preserve its database and follow its
release-matched upgrade/recovery instructions.

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

Use the [Python first-request example](../../examples/first_request.py) with
Python 3.10 or later and the [repository checkout](#development-and-recovery-references).
It needs an application key allowed to use the selected project and Chat
model. Choose a qualified model that accepts a non-streaming Chat request
with `max_tokens`. Use the gateway HTTPS origin, or keep the install
guide's SSH tunnel open and use its loopback URL. Run from the repository
root in Bash:

```bash
export UR_BASE_URL='http://127.0.0.1:3000'
export UR_MODEL='your-qualified-model-alias'
read -rsp 'UniRouter application key: ' UR_API_KEY
printf '\n'
export UR_API_KEY
python3 -I -B examples/first_request.py
```

The default command only runs preflight. No model request is sent. The
example checks that the same installation explicitly acknowledges the
single-attempt option and accepts the exact request locally. If either
check fails, it stops before Chat. This includes older installations that
ignore the option. For a model profile that supports `thinking.type=disabled`
and requires separate qualification for default reasoning, add
`--disable-thinking` to both commands. The example does not assume which
Provider or profile your alias selects.

If the preflight passes and you intend to make a potentially billable
request, run the following once. It preflights again with the same payload,
then sends at most one Chat request without enabling UniRouter response cache.

```bash
python3 -I -B examples/first_request.py --send
unset UR_API_KEY
```

In the console, open **Logs** in the same organization and project. Use the
**Chat request ID** from the example, not the preflight ID. For a timeout or
invalid response, search by the separately printed **Chat client request ID**
if the server did not return an ID. If your role cannot read these logs, ask
the project administrator to check that ID. A missing row is not proof
that nothing was sent or billed. The example never retries Chat automatically.

In the log detail, check the upstream attempt, usage source, and billing
status or receipt. Upstream token counts report usage; a gateway estimate
can differ. The amount observed or calculated as cost can differ from the
amount actually charged to the customer. In cost-observation mode a zero
customer charge does not mean the provider charged nothing. A successful
HTTP response alone does not prove a settled charge.

For a report, retain version, sanitized error code, request ID and reproduction
conditions; remove real prompts and credentials.

## Optional X-Insight integration

UniRouter can operate independently. Combine it with X-Insight when you want to
associate its execution, usage and economic facts with other authorized sources
for analysis and review. Facts are transferred asynchronously; X-Insight is not
part of the synchronous model request path.

Start with [X-Insight's current installation and first-use instructions](https://github.com/foigent/x-insight#readme),
then create a new authorized connection from the product setup screens. The
[recorded UR 1.15.6 and XI FPV24 acceptance](release-readiness.md#verified-existing-ur-and-xi-installation)
covers connection recovery and scoped reader access on those installations.

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

For the combined walkthrough, follow X-Insight's invitation and reader-access
instructions and the exact version/configuration pair recorded for that result.
Verify a synthetic execution whose original source/request and usage/billing
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

## Development and recovery references

To build or contribute, start with [development prerequisites](../../CONTRIBUTING.md):

```sh
git clone https://github.com/foigent/unirouter.git
cd unirouter
```

The [fresh-install owner](../ops/FRESH-INSTALL.md) documents native
plan/check/apply/resume boundaries for maintainers. It is not an instruction to
reset an existing database. Existing M125 installations need the
[M126 maintenance contract](../ops/COMMERCIAL-LICENSE-RETIREMENT.md); its
historical License steps do not apply to new current-version installations.
The [customer template](../../deploy/customer/README.md) provides the lower-level
verified-package procedure.
