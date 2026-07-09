# PRD5 Gate A Request ID Approval Runbook

> Date: 2026-07-10
> Status: PRD5 Batch 1R Canary/Sentinel complete (`c15cd7d` / PR #121);
> existing-request approval path remains active
> Audience: User, UniRouter owner, Codex, X-Insight coordination reviewers
> Scope: human approval process for the first X-Insight Gate A controlled
> sanitized snapshot

## Plain-Language Summary

A `request_id` is the receipt number for one UniRouter model request.

Approving a `request_id` does not approve raw prompts, raw model responses,
credentials, DSNs, API keys, raw IP addresses, or raw upstream error text. It
only allows UniRouter to export the sanitized evidence for that one receipt:

- the matching `call_logs` metadata;
- the matching `request_trace_events` metadata;
- generated `capture_gaps` explaining missing evidence, if any.

The approval manifest is a whitelist. If a request is not on the whitelist with
`approval_status=approved`, the snapshot tool must not export it.

This exact-ID process remains the path for requests that already exist. Newly
generated Gate A test requests use prior scenario/data/environment/operator/
retention authorization from
`docs/ops/PRD5-GATE-A-CONTROLLED-RUN-AUTHORIZATION.md`, then seal only the IDs
returned by that narrow run into the same approval manifest.

## What The User Is Approving

When the User approves one row, the approval means:

- this exact request is safe enough for the first X-Insight Gate A import test;
- the request came from the named controlled source instance;
- the request represents the scenario label in the row;
- UniRouter may export metadata-only evidence for this one request to the shared
  evidence handoff path.

The approval does not mean:

- X-Insight receives database access;
- X-Insight receives production credentials;
- raw prompt or response payloads may be copied;
- UniRouter may discover or export other requests;
- exporter/outbox or read-only DB/view work is approved.

## Approval Status Meaning

| Status | Meaning | Exported? | Use When |
|--------|---------|-----------|----------|
| `approved` | This exact request ID is approved for the Gate A sanitized snapshot | Yes | The row has a real request ID, the scenario is understood, and the source environment is acceptable |
| `pending` | Not approved yet | No | The approver needs more context or the request ID is not filled |
| `unavailable` | This scenario will not be provided in this Gate A round | No | The scenario cannot be safely produced or does not exist in the controlled source |

## How A Non-Technical Approval Works

1. A UniRouter owner runs or selects controlled test requests in a safe dev/test
   environment.
2. The owner finds each `request_id` from one of the existing UniRouter surfaces:
   Dashboard recent requests, Logs Explorer filter/detail, Developer Console
   Playground result, Developer Console Request Lookup, or the gateway response
   `_uniroute.request_id` field.
3. The owner fills the shared approval worksheet:
   `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-10-unirouter-gate-a-request-id-approval-worksheet.md`
4. The User reviews the worksheet. The User only needs to decide whether each
   row is `approved`, `pending`, or `unavailable`.
5. Codex or a UniRouter owner converts approved rows into the JSON manifest used
   by `scripts/prd5_xinsight_snapshot.go --approval-manifest`.
6. The snapshot tool exports only approved rows and rejects placeholder IDs such
   as `TBD`, `n/a`, or `unavailable`.

For controlled run authorization `UR-XI-GATE-A-20260710-01`, the User already
approved the scenario/data/environment/operator/retention boundary before the
IDs exist. Atlas therefore may complete steps 1-4 only for the two newly
generated authorized requests and mark their exact returned IDs `approved`
after verifying the expected HTTP status, matching response `X-Request-ID`,
source label, and non-production environment. The User does not need to review
opaque IDs a second time. This delegation does not apply to existing requests,
other scenarios, another environment, or another operator.

Batch 1V execution exception: Atlas sealed the denied request ID even though
the response did not return `X-Request-ID`. The immutable manifest/snapshot are
preserved for X-Insight diagnostic gap import, but this does not satisfy the
delegated correlation check and must not be used as approval precedent. A
post-fix denied-scenario rerun requires a new or amended User authorization.

## Required Gate A Scenarios

X-Insight requested a small scenario set for the first combined sanitized
snapshot:

| Scenario | Priority | Approval Guidance |
|----------|----------|-------------------|
| `successful_non_stream` | Required | Prefer a simple controlled test prompt with a successful non-stream response |
| `model_permission_denied` | Required | Use the existing UniRouter chat model-permission denial path after Batch 1P is deployed in the source environment |
| `provider_or_routing_failure` | Preferred | Mark `unavailable` if no safe controlled failure exists |
| `quota_or_budget_denial` | Preferred | Mark `unavailable` if no safe controlled denial exists |
| `streaming_request` | Optional | Include only if a safe controlled streaming request is available |
| `missing_trace_or_call_log_gap` | Optional | May remain import-side generated gap evidence instead of a real request |

## Why This Is Safer Than The Alternatives

| Option | Advantage | Cost / Risk | Current Decision |
|--------|-----------|-------------|------------------|
| Approval manifest | Small, auditable, no credentials to X-Insight, exports only named requests | Manual step; needs a UniRouter owner to find IDs | Use now for Gate A |
| Auto-discover request IDs | Faster for engineers | Turns Gate A into broad data discovery and can accidentally export unrelated requests | Do not use |
| Read-only DB/view | Better for repeated X-Insight queries | Requires credential handoff, access limits, bounded queries, and local import proof first | Later, only after Gate A import passes and User approves |
| Exporter/outbox | Scalable for production integration | Larger product surface: cursor, retry, redaction, retention, backpressure, failure isolation | Later, only after Gate A shows concrete gaps |

The current choice is intentionally conservative because Gate A is the first
real cross-project evidence exchange. It should prove the shape, import, dedupe,
source references, capture gaps, and readback before UniRouter grants broader
data access.

## Example User Approval Sentence

The User can approve without editing JSON:

```text
批准 successful_non_stream=req-xxx 和 model_permission_denied=req-yyy；
provider_or_routing_failure 本轮 unavailable；
quota_or_budget_denial 先 pending；
不要导出 raw prompt/response、凭证、DSN、raw IP 或 raw upstream error。
```

Codex or a UniRouter owner can then translate that sentence into the manifest
format, and the tool will enforce the boundary.
