# PRD4 Quota Vocabulary And Key Quota Foundation

> Version: v0.1  
> Scope: PRD4 Batch 1E vocabulary and visibility foundation only.

## Canonical Terms

| Term | Meaning | Current Implementation |
|------|---------|------------------------|
| RPM | Requests per minute | Existing per-key Redis sliding 60-second window uses `api_keys.rpm_limit` |
| RPS | Requests per second | Existing per-key Redis 1-second window still stores the value in legacy `api_keys.qps_limit`; API responses also expose `rps_limit` |
| TPM | Tokens per minute | Channel-level `channels.tpm_limit` exists, but key-level TPM is not configured or enforced in Batch 1E |
| tokens/s | Token throughput per second | Observability vocabulary only; no scheduler behavior in Batch 1E |

Do not use `TPS` in product UI or operator-facing PRD4 docs. It is ambiguous
between transactions per second and tokens per second.

## API Compatibility

`qps_limit` remains a backward-compatible request/response field. New clients
should prefer `rps_limit`; when both fields are present they must match.

API key list/detail/create responses include:

- `rpm_limit`
- `rps_limit`
- legacy `qps_limit`
- `quota.rpm_limit`
- `quota.rps_limit`
- `quota.qps_compat_limit`
- `quota.tpm_limit = null`
- `quota.tpm_source = "not_configured"`

Batch 1E does not add a migration because the existing `qps_limit` column is
the compatibility storage for RPS.

## Non-Scope

Batch 1E does not implement:

- Scheduler V2.
- Key-level TPM enforcement.
- Token reservation or capacity-aware dispatch.
- Budget hard-block behavior.
- New quota audit reason or idempotency key fields.

Future quota enforcement work should add explicit before/after quota snapshots,
operator reason, and idempotency key when the persistent quota model changes.
