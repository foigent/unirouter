# PRD8 Second Integration — Pre-Reset Gate Evidence

> Work item: `UR-PRD8-SECOND-INTEGRATION-PRE-RESET`
> Evidence date: 2026-08-05 (Asia/Shanghai)
> Environment: existing Alibaba Cloud UniRouter development/test ECS
> Status: `PRE_RESET_READY_WAITING_FOR_EXPLICIT_USER_START`
> Runtime mutation: none
> Data reset authorization: not granted

## Outcome

All safe work immediately before the second-integration data-reset boundary is
closed. The current Target release, public HTTPS entrypoint, reset inventory
coverage and zero-network Agent compatibility artifacts have been rechecked.
The next action is the destructive reset transaction itself, so execution
stops here until the Product Owner explicitly starts it and confirms the exact
preview below.

Invocation Optimization remains absent/default-off. Exact Cache keys, policy
revisions, Compression Shadow/Canary and a caller cohort are not prerequisites
for the baseline Agent POC and were not created. No Provider credential, model
request, Cursor/Checkpoint, Feed, X-Insight or runtime configuration changed.

## Exact Repository And Target Baseline

- Product `main` at the start of this checkpoint: commit/tree
  `fcfd6d3860fa6e95c4174fef7f5c3ee9832dca94` /
  `6206ee0fff9f32a84922637ae33678568ffb2bfc` with a clean worktree.
- Current deployed code release: commit
  `0def6b436b8b92bd68bf666c6ecc59072f6251ec`; its staged Target checkout is
  clean and reads tree `aacb40c932c668e802aeb31feeda43d4abede0b4`.
- Stable backend:
  `sha256:0de92e4ab456a8bbe995d0e7a2d5771dfa0b4746ca499d25f3f43df9e2fcafcb`,
  healthy, restart count `0`.
- Canary backend:
  `sha256:4cf81996321276151bbf0213268f6e6339b5729fff6508a8fbd5930d45f26fa2`,
  healthy, restart count `0`.
- Frontend:
  `sha256:3ee48db89c972d6f9d10bdbf00694836a443d4e43ea38b29e115f1f77d477e78`,
  running, restart count `0`.
- `http://8.130.18.85/` returns `308` to HTTPS;
  `https://8.130.18.85/` returns `200` with certificate verification result
  `0`; `/health/ready` returns `200` with PostgreSQL and Redis `ok`.
- Root filesystem remains at 84% used with approximately 6.3 GB available.
  No prune or cleanup ran.

## Exact Read-Only Reset Preview

The current backend binary ran:

```text
uniroute reset preview \
  --next-source-instance ecs.second-integration.v2 \
  --output -
```

The command opens a PostgreSQL `REPEATABLE READ READ ONLY` snapshot. Its code
contains no reset apply path. The exact captured artifact is
`PRD8-SECOND-INTEGRATION-RESET-PREVIEW-20260805.json`:

| Fact | Exact result |
|---|---|
| contract | `unirouter.reset-preview.v1` |
| generated at | `2026-08-04T18:03:09.146883Z` |
| current Source Instance | `unirouter-dev-149a8220bf1e` |
| proposed next Source Instance | `ecs.second-integration.v2` |
| mode | `dry_run_read_only` |
| action authorized / apply supported / mutation performed | `false / false / false` |
| scope complete | `true` |
| tables / all rows | `88 / 1623` |
| reset-candidate tables / rows | `81 / 1545` |
| protected tables / rows | `6 / 23` |
| schema-ledger tables / rows | `1 / 55` |
| review-required tables | `0` |
| plan SHA-256 | `sha256:c491132982eed86adf00b3b28ccfefad640cf482b642013203d38d8782440bf2` |
| artifact SHA-256 | `f1c08727870e2094e6aa6f69fd81cf374ea44f08fb5d353a68ef21eaefd3c4da` |

An independent `jq` reconstruction with `plan_sha256` set to the empty string
reproduced the embedded plan digest exactly. A second read-only snapshot at
`2026-08-04T18:04:07.711288Z` returned the same summary and byte-equivalent
ordered 88-table count/classification array. Only generation time and its
derived plan digest changed.

### Non-zero reset-candidate rows

| Domain | Exact current rows |
|---|---|
| identity and access | `admin_sessions=1`, `api_keys=56`, `grants=5`, `users=20` |
| organization/project | `orgs=1`, `projects=1`, `project_members=6` |
| Provider/routing/model | `channels=3`, `channel_keys=3`, `model_registry=11`, `model_prices=11`, `model_sets=1`, `model_set_items=2`, `project_model_sets=1`, `guardrail_policies=1` |
| request/usage/billing | `call_logs=60`, `transactions=286`, `billing_provider_attempts=19`, `billing_reservation_grant_allocations=14`, `freeze_idempotency=60`, `usage_finalization_journal=32`, `wallets=1` |
| evidence/delivery | `audit_logs=461`, `request_trace_events=415`, `request_delivery_receipts=27`, `runtime_terminal_hooks=19`, `notification_events=22` |
| Reference Feed and stream state | `completed_execution_feed_connections=1`, `completed_execution_feed_states=1`, `stream_delivery_checkpoints=2`, `stream_delivery_leases=1` |
| runtime ledger | `micro_ledger_runtime_state=1` |

All remaining reset-candidate tables are present with zero rows and remain in
the exact JSON artifact. In particular, Workload Applications, Invocation
Optimization policies/revisions/receipts, Cache receipts, internal Outbox,
enterprise directory and enterprise finance tables are all zero.

### Preserved scope

The future reset plan protects:

- `commercial_license_entitlements=1`;
- `deployment_profiles=0` and `gateway_endpoints=0`;
- append-only `release_history=5` and `release_state=1`;
- deployment/security `settings=16`; and
- immutable `schema_migrations=55` as the separate schema ledger.

Deployment secrets, database credentials, TLS private keys, certificate
renewal state, release images and host infrastructure are outside product-data
reset scope and were not read into the artifact.

## Compatibility Readiness Before The Gate

The source-controlled compatibility bundle remains fail-closed:

- OpenAI SDK Chat Completions is repository/protocol verified;
- OpenAI Agents SDK must use `OpenAIChatCompletionsModel` because the default
  Responses path remains unsupported;
- LangChain, AutoGen, OpenClaw and Tencent WorkBuddy remain
  `protocol_compatible` until exact versions run from the second device; and
- Feishu Bot/Aily remain isolated-adapter work requiring a real tenant, not
  direct Gateway compatibility claims.

The configuration JSON and Python examples parse, contain no embedded
credential, and the POC shell preflight passes syntax validation. This is
zero-network artifact evidence, not a substitute for the later exact-client
matrix.

## Verification Results

```text
go test ./internal/resetplan ./cmd/uniroute <focused reset tests>
  PASS

go test ./internal/clientcompat ./internal/workloadapp \
  ./internal/deploymentprofile ./internal/resetplan ./cmd/uniroute \
  <focused pre-reset and compatibility tests>
  PASS

bash -n scripts/poc/check-unirouter-gateway.sh
jq empty <compatibility JSON artifacts>
Python AST parse <all compatibility Python examples>
  PASS

public HTTP redirect / HTTPS root / HTTPS readiness
  308 / 200 / 200; TLS verify result 0

two ECS reset previews
  same ordered table counts and classifications; mutation_performed=false
```

## Explicit Stop Boundary

The next step is not another autonomous repository task. It is one bounded,
destructive reset transaction and therefore requires a new explicit Product
Owner start instruction against this exact preview. Only after that instruction
may the operator:

1. quiesce model and Reference Connector traffic;
2. create and verify a fresh PostgreSQL backup and SHA-256;
3. re-run the preview and stop on any count, table or digest drift;
4. review the exact delete/bootstrap order;
5. bind the new Source/Cursor epoch without reusing the current identity;
6. handle the empty/default-off response-cache scope as specified by the plan;
7. execute the product-data reset and fresh bootstrap; and
8. prove post-reset migrations, preserved infrastructure and zero stale
   identity/credential/Feed/Checkpoint reuse before creating new POC data.

No item in that sequence was executed in this checkpoint. Fresh organization,
users, project, channels, Provider credentials, Workload Applications and
second-device traffic all occur after the reset gate, not before it.
