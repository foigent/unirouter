# PRD5 X-Insight Sanitized Snapshot

> Version: v0.1
> Date: 2026-07-08
> Status: Batch 1A/1B fixed awaiting Canary
> Related: `docs/PRD-Phase5.md`, `docs/ops/PRD5-ACCEPTANCE-MATRIX.md`,
> `docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md`

## Purpose

This document defines the first UniRouter-controlled data handoff to X-Insight:
a sanitized, explicit-request snapshot for Gate A importer development.

The snapshot is not a production exporter. It is a bounded test artifact so
X-Insight can validate import, dedupe, source refs, capture gaps, and read APIs
without touching shared ECS or requiring live read-only credentials.

## Tool

```bash
go run scripts/prd5_xinsight_snapshot.go -help
```

The tool reads Postgres through a DSN stored in an environment variable. Do not
put DSNs, credentials, tokens, or passwords in docs, shell history examples, or
the shared folder.

Example command shape:

```bash
UNIROUTE_PRD5_SNAPSHOT_DSN="<read-only postgres dsn>" \
go run scripts/prd5_xinsight_snapshot.go \
  --source-instance local-controlled \
  --request-id req-success-001 \
  --request-id req-stream-001 \
  --scenario success=req-success-001 \
  --scenario stream=req-stream-001 \
  --output output/prd5/xinsight/gate-a-snapshot.json
```

Use `--overwrite` only when intentionally replacing a local generated artifact.

## Selection Rule

The tool requires explicit `--request-id` or `--request-ids` values.

Broad table snapshots are intentionally unsupported in Batch 1A because X-Insight
Gate A only needs controlled request scenarios:

- one successful model request;
- one routing or provider failure if available;
- one billing/quota/budget-related event if available;
- one streaming request if available;
- one request with usage/cost call-log data.

## Included Sources

Batch 1A exports only:

- `call_logs`
- `request_trace_events`

The output preserves:

- row IDs;
- `request_id`;
- org/project/API-key/owner/key-type context;
- structural PRD5 evidence fields when present;
- event ordering through `created_at` and `id`;
- usage, cost, status, latency, streaming, usage source, and confidence fields;
- sanitized trace metadata;
- generated source refs;
- generated source event hashes;
- capture gaps for selected request IDs with missing rows.

## Safety Behavior

The snapshot does not export:

- raw prompts;
- raw responses;
- API key plaintext;
- upstream credentials;
- DSNs;
- tokens;
- raw IP addresses;
- raw `upstream_error` text;
- customer payloads.

Trace metadata is re-sanitized with `internal/traceutil.SanitizeMetadata` even
though UniRouter already sanitizes trace metadata on write and read.

Raw IP addresses are reduced to `ip_address_was_present`.

Raw upstream errors are reduced to `upstream_error_present`.

## Output Contract

Top-level fields:

- `schema_version`: `unirouter.prd5.xinsight.snapshot.v1`
- `generated_at`
- `source`: `unirouter`
- `source_instance`
- `request_ids`
- `scenarios`
- `safety`
- `counts`
- `capture_gaps`
- `call_logs`
- `request_trace_events`
- `notes`

Source ref shape:

```text
unirouter:<source_instance>:<table>:<id>
```

Examples:

```text
unirouter:local-controlled:call_logs:98765
unirouter:local-controlled:request_trace_events:12345
```

Capture gap values follow the X-Insight-aligned vocabulary where possible. Batch
1A generated gaps use `not_available_from_source` with a reason string instead
of inventing missing evidence.

## Handoff Rule

Generated snapshots should stay out of Git unless the User explicitly approves a
fully synthetic fixture.

Recommended shared-folder handoff path for generated, sanitized artifacts:

```text
/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/
```

If a snapshot is placed there, add a short Markdown note with:

- generation date;
- source instance label;
- selected request IDs or scenario labels;
- command shape without DSN;
- confirmation that raw prompt/response, credentials, raw IPs, and raw upstream
  errors are absent.

## Gate Conditions For Later Read-Only Access

Read-only DB credentials or read-only views should only be considered after:

- X-Insight imports the sanitized snapshot locally;
- X-Insight proves duplicate re-run behavior;
- X-Insight exposes imported events/gaps through minimal read APIs;
- GitHub CI is green before any shared ECS update;
- the User approves the credential handoff process.

Prefer read-only views over direct whole-table privileges.

Queries must use bounded filters/cursors and avoid broad unbounded scans on the
shared development/test database.
