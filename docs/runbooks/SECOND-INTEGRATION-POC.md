# UniRouter second-integration POC runbook

## Current read-only reset preview

Before the user explicitly announces that the second integration and data reset
may start, the only permitted reset operation is the read-only preview:

```bash
uniroute reset preview \
  --next-source-instance ecs.second-integration-<epoch> \
  --output ./reset-preview.json
```

The command uses a PostgreSQL `REPEATABLE READ READ ONLY` snapshot and only
counts tables and rows. It creates the output with mode `0600` and refuses to
overwrite an existing file. It has no delete SQL, cannot apply a reset, does not
move Cursor/Checkpoint state and does not change the configured Source Instance.
Any newly introduced table that lacks an explicit classification makes
`scope_complete=false`.

The output `plan_sha256` protects the preview scope; it is not a database backup
digest. A future authorized reset must still quiesce traffic, create and verify
a database backup and its SHA-256, review exact delete/bootstrap ordering, bind
a new Source/Cursor epoch and complete post-reset migration/bootstrap/readback.

The latest captured development-Target preview is
`docs/ops/evidence/PRD8-SECOND-INTEGRATION-RESET-PREVIEW-20260805.json`
(artifact SHA-256
`f1c08727870e2094e6aa6f69fd81cf374ea44f08fb5d353a68ef21eaefd3c4da`,
plan SHA-256
`sha256:c491132982eed86adf00b3b28ccfefad640cf482b642013203d38d8782440bf2`).
It covers 88 tables with no unclassified table and proposes
`ecs.second-integration.v2` as the next explicit Source Instance. It is
approval evidence, not reset authority. Re-run it after traffic is quiesced and
stop if any table, count, classification or digest input differs.

## Boundary before the user starts the POC

- Do not clear current control/business data yet. The user will explicitly announce the start.
- Do not change provider credentials, traffic, Cursor/Checkpoint or the X-Insight Reference Connector while X-Insight remains under iteration.
- Deployment secrets, TLS private keys and database credentials are not product data and are not handled by the reset procedure.

## Current development Gateway Endpoint

The current externally verified development endpoint is:

- console: `https://8.130.18.85/`;
- OpenAI-compatible Base URL: `https://8.130.18.85/v1`.

It uses a publicly trusted short-lived IP SAN certificate with automated
renewal. Do not use `8-130-18-85.sslip.io`: Alibaba Cloud's ICP interception
returns 403 for that unfiled shared hostname. The current operator-browser and
protocol checks close the development HTTPS prerequisite only. At POC start,
revalidate the endpoint from the actual second device before creating or using
fresh application credentials. Exact Target evidence is in
`docs/ops/evidence/PRD8-PUBLIC-HTTPS-TARGET-20260803.md`.

## Preflight order

1. Record the exact release commit/tree, image digest, migration ledger and current ECS Deployment Profile.
2. Confirm a public HTTPS Gateway Endpoint from a second device. A declared port 443 is not evidence.
3. Create fresh organization, users, project, upstream channels and provider credentials.
4. Complete model governance/runtime release so each intended channel is `active`; an enabled provider credential does not activate a channel.
5. Register one Workload Application per POC caller and create a bound credential.
6. Select a model in the Effective Access Inspector and require all readiness axes to be ready.
7. Keep Invocation Optimization absent/default-off for the baseline Agent POC. It is not a prerequisite for Gateway compatibility.
8. Run `scripts/poc/check-unirouter-gateway.sh` from the second device.
9. Run exact-version clients in this order: OpenAI SDK, OpenAI Agents SDK Chat model, LangChain, AutoGen, OpenClaw, Tencent WorkBuddy.
10. Validate request logs, usage, charge/finality and audit evidence after each client.
11. Record failures by Client Profile, Endpoint Profile, model, channel and request ID; do not hide them behind a generic “connection failed”.

## Optional Invocation Optimization experiment gate

Do not configure Exact Cache keys or create an optimization policy merely to
make the baseline POC pass. If the Product Owner separately authorizes an
optimization experiment after baseline compatibility is closed:

1. create one bounded project/model policy and advance it through Draft,
   Validated and metadata-only Shadow;
2. read
   `GET /api/v1/invocation-optimization/policies/{ref}/readiness` for that
   organization and exact policy revision;
3. require `canary_admission_ready=true`, zero blockers and the expected
   content-free Shadow counts for every targeted driver; and
4. record a separate activation decision and a separate caller opt-in cohort.

The readiness response has `runtime_mutation=none` and explicitly grants no
activation or traffic authority. A Cache experiment additionally needs an
operator key ceremony, rotation/recovery plan and all-or-none runtime binding.
A Compression experiment does not need Cache keys, but still requires its own
representative Shadow evidence and Product Owner Canary decision.

## Data reset gate

The reset is a separate destructive operation. Before execution, produce a count-only preview covering organization/account/project/application/API-key/channel/provider-credential/model-governance/limit/usage/cost/audit/Feed data and explicitly list preserved deployment secrets and infrastructure. Execute only after the user confirms that exact preview.

## X-Insight dual-track handoff

The second integration uses two separately accepted tracks:

1. The existing completed-execution Feed remains the V1 compatibility path. It keeps its endpoint, DTO, credential,
   order, Cursor and source result semantics. After reset it must use a new explicit Source Instance and X-Insight
   checkpoint epoch; neither side may reuse the first campaign Cursor.
2. Selected second-campaign request IDs use the existing approval-manifest-bound
   `scripts/prd5_xinsight_snapshot.go` path for high-fidelity product validation. This artifact remains metadata-only
   and controlled; it is not a continuous Connector and must not be described as one.

The safe receiver handoff order is: prepare the X-Insight receiver credential; register its public Frozen Registration
Bundle while the source Feed is paused; use the staged credential to obtain the new Source's empty bootstrap
Cursor/high-watermark; activate the X-Insight checkpoint epoch while its worker remains inactive and disabled; then
activate the receiver credential and start the bounded V1 window. If the epoch barrier has been crossed, do not edit
checkpoint rows or attempt an abort—keep the Feed off and either resume the exact credential operation or use the
verified X-Insight database recovery path.

## Pass criteria

- External device uses the configured HTTPS Gateway Endpoint and a newly created application-bound credential.
- At least one non-stream and one stream request complete with stable request identity, settled billing and visible evidence.
- Invalid credential, disabled application, unauthorized model, exhausted limit and unavailable route fail with actionable error identities.
- Restart does not lose application identity, route publication, billing finality or logs.
- No X-Insight availability is present in the synchronous request path.
