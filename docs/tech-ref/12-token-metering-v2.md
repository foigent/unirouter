# Token Metering V2

> Version: v0.1
> Scope: PRD2 batch 5 backend foundation.

## Goals

Token Metering V2 makes usage evidence explicit enough for enterprise reconciliation without changing PRD1 wallet safety behavior.

## Usage Source Values

Canonical values written to `call_logs.usage_source`:

| Value | Meaning | Confidence |
|-------|---------|------------|
| `upstream` | Provider or adapter returned usage tokens | 1.00 |
| `counter_v2` | Stream fallback counted output deltas and estimated request input | 0.65 |
| `adapter_estimated` | Adapter-specific deterministic estimate | 0.75 |
| `estimated` | Generic local estimate | 0.50 |
| `no_route` | Request failed before upstream route | 0.00 |
| `upstream_failed` | All upstream attempts failed | 0.00 |

Legacy values are normalized on write and migration:

| Legacy | Canonical |
|--------|-----------|
| `api` | `upstream` |
| `stream_estimate` | `counter_v2` |

## Log Fields

`call_logs` stores:

- `estimated_tokens`: pre-freeze estimate used for admission/billing safety.
- `prompt_tokens`, `completion_tokens`, `total_tokens`: final metered usage.
- `usage_source`: canonical source value.
- `usage_confidence`: confidence score derived from `usage_source`.

Billing still settles on final metered usage and must preserve the invariant that wallet balance does not go negative.

## Reconciliation Contract

`GET /api/v1/reconciliation` supports:

- existing `model` and `status` filters.
- `usage_source` filter, including legacy aliases such as `api` and `stream_estimate`.

Each record includes:

- `estimated_tokens`
- `actual_tokens`
- `delta`
- `variance_tokens`
- `variance_ratio`
- `usage_source`
- `source_confidence`

The response includes a `summary` block for the current page:

- `estimated_tokens`
- `upstream_tokens`
- `estimated_ratio`
- `estimated_ratio_alert_threshold`
- `estimated_usage_alert`
- `estimated_ratio_alert_threshold_setting`
- `estimated_ratio_scope`

The default alert threshold is `0.2` and can be configured through `settings` key `token_metering.estimated_ratio_alert_threshold`.

## Current Limits

- The current summary is page-scoped, not full-query aggregated.
- No notification delivery is emitted in batch 5.
- Frontend filtering by usage source is not implemented in batch 5.
- Native provider adapter-specific estimates remain future work.
