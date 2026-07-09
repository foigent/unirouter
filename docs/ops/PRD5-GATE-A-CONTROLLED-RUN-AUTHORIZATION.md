# PRD5 Gate A Controlled Run Authorization

> Date: 2026-07-10
> Status: proposed authorization path; real controlled run not yet authorized
> Audience: User, UniRouter owner/operator, X-Insight coordination reviewers

## Plain-Language Decision

For a request that already exists, approval continues to use its exact
`request_id`.

For a new request created only for Gate A, the User should not have to search
logs for an opaque ID. Instead, the User approves the run boundary before any
request is sent. An authorized UniRouter operator then creates only those test
requests, gives each one a unique `X-Request-ID`, verifies the same ID is
returned, and seals those IDs into the approval manifest.

Approving the controlled run authorizes the test requests and the sanitized
metadata handoff. It does not authorize production traffic, broad log search,
database credentials, raw prompt/response export, read-only DB/view access,
shared ECS staging, or an exporter/outbox.

## Required Approval Fields

| Field | Meaning | Current Gate A Value |
| --- | --- | --- |
| Scenario set | Which test outcomes may be created | Required: `successful_non_stream`, `model_permission_denied`; other scenarios remain unavailable unless separately approved |
| Data classes | What may leave UniRouter | Matching sanitized `call_logs`, `request_trace_events`, generated `capture_gaps`, approval manifest, handoff note, and X-Insight receipt |
| Source instance | Which non-production source may be used | Proposed: `unirouter-dev-gate-a-2026-07-10`; operator must confirm the actual environment before execution |
| Safety profile | What must never be exported | No credentials, DSN, tokens, raw prompt/response, raw IP, or raw upstream error text |
| Retention | How long temporary shared artifacts remain | Pending User decision; proposed default is delete the temporary Gate A snapshot seven days after receipt reconciliation, while retaining the sanitized contract and receipt evidence |
| Authorized operator | Who may use the environment and API keys | Pending User assignment; must be a UniRouter owner/operator with approved dev/test access |

The proposed seven-day artifact retention favors data minimization because the
snapshot is a temporary integration test input, not either product's system of
record. A longer period makes troubleshooting easier but increases the time an
otherwise unnecessary copy remains in the shared folder.

## Operator Preconditions

The operator must confirm all of the following before sending requests:

- the target is an approved UniRouter dev/test instance, not production;
- Batch 1P model-permission evidence is deployed on that instance;
- one API key/model pair can complete a non-stream request;
- one restricted API key rejects the selected model through
  `auth_error.model_not_allowed`;
- API keys and DSNs remain in environment variables or an approved secret
  store and are never written to this repo or the shared folder;
- the temporary requests contain only synthetic, non-sensitive content.

## Narrow Run Procedure

UniRouter already accepts `X-Request-ID` on gateway requests, returns it in the
response header, and uses it as the gateway request identifier. The operator
therefore creates a unique, scenario-marked ID before each request instead of
running any broad discovery query.

Command shape, with all credentials supplied out of band:

```bash
run_suffix="$(openssl rand -hex 8)"
success_request_id="gatea-success-${run_suffix}"
denied_request_id="gatea-model-denied-${run_suffix}"

curl -sS -o /dev/null -D /tmp/gatea-success.headers \
  -H "Authorization: Bearer ${UNIROUTE_GATE_A_SUCCESS_API_KEY}" \
  -H "Content-Type: application/json" \
  -H "X-Request-ID: ${success_request_id}" \
  "${UNIROUTE_GATE_A_BASE_URL}/v1/chat/completions" \
  -d "{\"model\":\"${UNIROUTE_GATE_A_SUCCESS_MODEL}\",\"messages\":[{\"role\":\"user\",\"content\":\"Reply with OK.\"}],\"stream\":false}"

curl -sS -o /dev/null -D /tmp/gatea-model-denied.headers \
  -H "Authorization: Bearer ${UNIROUTE_GATE_A_DENY_API_KEY}" \
  -H "Content-Type: application/json" \
  -H "X-Request-ID: ${denied_request_id}" \
  "${UNIROUTE_GATE_A_BASE_URL}/v1/chat/completions" \
  -d "{\"model\":\"${UNIROUTE_GATE_A_DENIED_MODEL}\",\"messages\":[{\"role\":\"user\",\"content\":\"Reply with OK.\"}],\"stream\":false}"
```

The operator verifies only status and returned `X-Request-ID`; response bodies
are discarded. Expected outcomes:

| Scenario | Expected HTTP Result | Required ID Check |
| --- | --- | --- |
| `successful_non_stream` | `2xx` | returned `X-Request-ID` equals `success_request_id` |
| `model_permission_denied` | `403` | returned `X-Request-ID` equals `denied_request_id` |

After verification, the operator records only the two IDs, scenario labels,
environment label, operator identity, and execution time in the shared
authorization worksheet. Header temp files should be deleted individually
after the IDs are recorded.

## Seal And Handoff

Only the returned IDs from this authorized run may be marked `approved` in the
JSON approval manifest. The snapshot tool then:

- exports only those IDs;
- writes a new immutable `exchange_id` into the snapshot;
- writes `approval_manifest_sha256` from the exact manifest bytes;
- preserves manifest mode in the generated handoff command;
- excludes raw prompts, raw responses, credentials, raw IPs, and raw upstream
  error text.

X-Insight returns an `xinsight.import-receipt.v1` sidecar. UniRouter reconciles
the exchange using `(exchange_id, artifact.sha256)` and must not interpret
`persisted` as proof that every event is complete or fully observable.

## Approval Sentence

The User can authorize the run without editing JSON or running commands:

```text
批准在 <dev/test 环境> 由 <操作人> 执行 successful_non_stream 和
model_permission_denied 两个 Gate A 合成测试；仅允许 metadata-only 的
call_logs、request_trace_events 和 capture_gaps；临时快照在 X-Insight
回执对账完成 7 天后删除；禁止导出凭证、DSN、raw prompt/response、raw IP
和 raw upstream error。
```

Until the environment, operator, and retention are approved, this document is
an executable runbook but not authorization to create real requests.
