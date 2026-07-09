# PRD5 Gate A X-Insight External Integration Closeout

> Date: 2026-07-10
> Status: External integration verified with known UniRouter source gap
> Authorization: `UR-XI-GATE-A-20260710-01`
> Exchange: `uxchg_f7de31e541516893a0d9b31cc0f3892a`

## Sources

- Shared response: `responses/2026-07-10-x-insight-gate-a-import-result.md`
- Process response: `responses/2026-07-10-x-insight-execution-owner-process-response.md`
- Receipt: `evidence/gatea-snapshot-20260710.receipt.json`
- X-Insight import fix: `4a562a6` / PR #9
- X-Insight closeout: `5f0fa49` / PR #10

The shared paths are under `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/`.

## Hash Reconciliation

Local SHA-256 verification matched the response and receipt:

| Artifact | SHA-256 |
|----------|---------|
| Snapshot | `5b1d52b1e8bd9b47f14de74670eb4404ed8862c32cdf323d86766446cdf438e8` |
| Approval manifest | `d2f51fc61fe84dedb8b1d1556c5a4f135b50711346327cf6fbcdd19d60337d68` |
| Receipt | `b4cc1f7eda6745cfdd1c2a038ffea821be60937d747080690b47c22aa117e418` |

Receipt status is `persisted` for source instance `unirouter-dev-gate-a-2026-07-10`.

## Import Result

- In-memory validation: 10 accepted, 0 rejected, 0 quarantined.
- In-memory rerun: 10 duplicate/no-op.
- First PostgreSQL persistence: 10 inserted, 0 duplicate, 0 conflict, 0 rejected.
- Second PostgreSQL persistence: 0 inserted, 10 duplicate/no-op, 0 conflict, 0 rejected.
- Readback: 10 events and 3 persisted capture gaps.

X-Insight initially quarantined two sanitized events because substring matching
misclassified `completion_tokens` as raw completion content. PR #9 fixed the
consumer-side detector while keeping raw prompt, completion, messages,
request/response bodies, credentials, tokens, secrets, and passwords blocked.
Required X-Insight CI passed before merge.

## Process Acknowledgement

X-Insight accepted Codex as the UniRouter execution contact and adopted the
same single execution owner model. Mandatory controls remain repository tests
and CI, explicit execution records, separate evidence levels, high-impact User
authorization, immutable sanitized evidence, and P0/P1 reopen/block behavior.
Optional Canary/Sentinel/Atlas review no longer forms a serial dependency.

## Evidence Meaning

- `successful_non_stream` readback contains 8 normalized events and 1
  `provider_not_returned` gap.
- `model_permission_denied` readback contains 2 factual
  `not_available_from_source` gap events and 2 persisted gaps.
- X-Insight did not invent a denial decision event. The first snapshot remains
  diagnostic source-gap evidence and does not prove Batch 1P/1W target behavior.

## Remaining Boundaries

1. Temporary Gate A test API key disable confirmation remains pending.
2. Batch 1W is CI verified but not target deployed under this evidence.
3. A corrected denial rerun requires an available authorized non-production
   deployment path and a new or amended User authorization naming the operator.
4. No X-Insight ECS target deployment or target-runtime verification is claimed;
   this receipt proves X-Insight persistent import/readback, not X-Insight ECS.
5. No read-only UniRouter DB/view, exporter/outbox, expanded data scope, raw
   payload mode, deployment, credential action, or production claim is included.

## Retention

Receipt reconciliation date is 2026-07-10. The approved temporary snapshot
deletion deadline is 2026-07-17. Sanitized authorization, contract, response,
receipt, and this reconciliation evidence may be retained.
