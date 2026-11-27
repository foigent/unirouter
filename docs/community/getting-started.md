# Getting started with UniRouter

[Foigent](https://github.com/foigent) · [Project overview](../../README.md) ·
[简体中文](getting-started.zh-CN.md)

This walkthrough uses [UniRouter 1.16.0 Preview 26](https://github.com/foigent/unirouter/releases/tag/v1.16.0-preview.26).
Start with the [Linux amd64 installation and verification guide](install-linux.md)
on your server or ECS. Use the exact preview download, checksum and trusted
public-key ring. The [release status](release-readiness.md) tracks signed
downloads, fresh installation and upgrades separately, including older previews.

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
   as `admin`. The console defaults to English, with **简体中文** available in
   the language control. Keep your password and browser token private.
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

1. As an administrator, select the customer organization and intended project.
   In **Model access → Models & endpoints**, configure the provider connection,
   its credential, the model deployment and execution profile. Run the required
   probes, review their evidence and activate the matching model Release.
   **Model access → Call diagnostics** helps distinguish route readiness from
   permission to call it.
2. Use **Access & security → People & roles** to give the intended member
   access to that project. Give the calling identity access to the chosen Chat
   model and set a budget and rate limit for the test. A project viewer can
   inspect project requests but cannot issue or use a project calling credential.
3. Sign in as the authorized project developer. In **Project workspace**,
   select that project, check the enabled model alias and use **Project
   credentials → Create project credential** if you need a key. Restrict it to
   the intended model where the control is available. If your organization
   requires an access grant, obtain the grant through its normal access flow
   before issuing the credential. Keep the full key in a private location;
   it is shown once. Disable any key you created solely for the walkthrough
   when you finish.

Qualification probes and real requests can incur provider charges. Use only
the provider, credential and allowance you intend. In cost-observation mode,
a project budget does not enforce a per-request cap on provider charges.

## Make and inspect a request

If you installed Preview 26 from the signed package, fetch and verify the
single example file in a new private directory on your client. The URL and
SHA-256 should match [Make your first request on the Preview 26 release page](https://github.com/foigent/unirouter/releases/tag/v1.16.0-preview.26#make-your-first-request).
The script is pinned to that release's product source. Use Python 3.10 or
later with Bash and `sha256sum`. A contributor working in the repository can
use [`examples/first_request.py`](../../examples/first_request.py) and replace
`first_request.py` with `examples/first_request.py` in the commands below.

```bash
(
  set -eu
  umask 077
  curl --fail --location --proto '=https' --proto-redir '=https' -o first_request.py \
    https://raw.githubusercontent.com/foigent/unirouter/3e84663f54bd8e57e4637d61bfcdb946a77bc11f/examples/first_request.py
  printf '%s\n' '1bc580acf1038c3a209e7c2398bf1bd5fdcc17468e261c4163529d9b3a00437a  first_request.py' | sha256sum -c -
)
```

Continue only after the download and check pass. Choose a qualified Chat model
that accepts non-streaming requests with `max_tokens`. Set the gateway HTTPS
origin, or keep the install guide's SSH tunnel open and use the loopback URL below.

```bash
export UR_BASE_URL='http://127.0.0.1:3000'
export UR_MODEL='your-qualified-model-alias'
read -rsp 'UniRouter application key: ' UR_API_KEY
printf '\n'
export UR_API_KEY
python3 -I -B first_request.py
```

The default command only runs preflight. No model request is sent. The
example checks that the same installation explicitly acknowledges the
single-attempt option and accepts the exact request locally. If either
check fails, it stops before Chat. This includes older installations that
ignore the option. For a model profile that supports `thinking.type=disabled`
and requires separate qualification for default reasoning, add
`--disable-thinking` to both commands. The example does not assume which
Provider or profile your alias selects.

For a joint walkthrough, check the [authorized XI connection](#optional-x-insight-integration)
before sending. If the preflight passes and you intend to make a potentially
billable request, run the following once. It preflights again with the same payload,
then sends at most one Chat request with one upstream attempt and without
enabling the UniRouter response cache. The example requests up to 32 output
tokens. Clear `UR_API_KEY` from the shell when you finish, including if you
stop after preflight.

```bash
python3 -I -B first_request.py --send
unset UR_API_KEY
```

### Find the request and its settlement

- **Project member:** open **Project workspace**, select the same project and
  paste the full **Chat request ID** into **Find a request**. Keep the `op_…`
  ID as returned even if the field shows `req-…` as a hint. Select **Query**
  to check the requested model, status, token count and recorded
  charge. Read-only project members can use this lookup without a calling
  credential.
- **Administrator:** open **Monitoring & usage → Execution requests** in the
  customer organization. Under **Advanced filters**, use the exact **Operation
  ID (Request ID)** with a time window covering the call, and confirm the
  result belongs to the intended project.
  This administration list is organization-scoped. Open the execution receipt
  to see route attempts and **Cost & settlement** for usage source, settlement
  and the actual customer charge that the ledger recorded.
- **Uncertain outcome:** if the server did not return a Chat ID, give the
  authorized administrator the separately printed **Chat client request ID**
  and time. Use **Client request ID (exact)** in the same advanced filters.
  The **Preflight request ID** identifies only the preflight. If a Chat response
  timed out or was incomplete, check the original attempt before any further
  send. An absent row leaves the result unknown.

Provider-reported tokens and gateway estimates have different sources. The
observed or calculated cost can differ from the customer debit. In
cost-observation mode, the customer can be charged zero while the provider
still bills for the call. Check settlement in the receipt even when the HTTP
request succeeded.

## Optional X-Insight integration

UniRouter can operate independently. Combine it with X-Insight when you want to
associate its execution, usage and economic facts with other authorized sources
for analysis and review. Facts are transferred asynchronously; X-Insight is not
part of the synchronous model request path.

Pair this guide with [X-Insight FPV27](https://github.com/foigent/x-insight/releases/tag/v0.2.0-dev.fpv27)
and its [two-administrator connection and first-fact guide](https://github.com/foigent/x-insight/blob/main/docs/community/getting-started.md#connect-unirouter-and-verify-the-pair).
For a new connection, confirm the exact UR source, project and metadata scope
on both sides. Reuse an existing connection if it already covers that scope.
The [historical UR 1.15.6 and XI FPV24 record](release-readiness.md#verified-existing-ur-and-xi-installation)
covers connection recovery and scoped reader access on those installations.

Once UR has a completed request, share the registered UR source, organization
and project, **Chat request ID** and request time with its time zone privately
with authorized people on both sides. Follow
[Find the same request in X-Insight](https://github.com/foigent/x-insight/blob/main/docs/community/getting-started.md#find-the-same-request-in-x-insight)
with that source, project and time. Match the request ID and compare the
recorded outcome, usage and cost. Preserve the original amount and currency.
An XI administrator can check whether the authorized record has arrived and whether the reader
has the exact source/project grant. If it has yet to appear, check the existing
connection and sync status in XI. The same UR request is sufficient for that
check. Metadata access and body permissions are granted separately.

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

For a failed step, start with the corresponding API error/status, the connection
lifecycle runbook and release-matched [deployment troubleshooting](../ops/deployment-runbook.md#七故障排查).
In a public [Issue](https://github.com/foigent/unirouter/issues), include the
product version, a sanitized error code and the smallest reproduction.
Use synthetic or masked identifiers when needed. Keep production identifiers,
organization and project identities, customer content and credentials out
of public reports. Follow [Security](../../SECURITY.md) for sensitive reports
and [Contributing](../../CONTRIBUTING.md) for patches and DCO.

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
