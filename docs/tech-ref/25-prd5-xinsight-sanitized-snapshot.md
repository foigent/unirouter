# PRD5 X-Insight Sanitized Snapshot

> Version: v0.7
> Date: 2026-07-20
> Status: the bounded operator-mediated sidecar is implemented. Two historical
> controlled v1 exchanges completed; the current producer emits snapshot v2
> with exact micro-CNY fields. UniRouter PR #219 / `5176654` passed required CI;
> a local synthetic compatibility probe also passed, but X-Insight-owned fix,
> publication and consumer acceptance remain a separate verification gate. A
> Linux-only strict internal-real generation path is now defined below; its
> activation, source qualification, artifact generation and cross-project
> binding remain separate User-gated work.
> Related: `docs/PRD-Phase5.md`, `docs/ops/PRD5-ACCEPTANCE-MATRIX.md`,
> `docs/planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md`,
> `docs/planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md`

## Purpose

This document defines the first UniRouter-controlled data handoff to X-Insight:
a sanitized, explicit-request snapshot for Gate A importer development.

The snapshot is not a production exporter. It is a bounded test artifact so
X-Insight can validate import, dedupe, source refs, capture gaps, and read APIs
without touching shared ECS or requiring live read-only credentials.

## 2026-07-20 Strict Internal-Real Path

The historical direct-ID and handoff workflow below remains useful evidence for
the completed controlled Gate A exchanges, but it is not qualifying input for a
new internal-real exchange. For that purpose, approval-manifest mode is the
only strict path and applies these fail-closed rules:

- the manifest contains `1..20` rows and every row is exactly `approved`,
  `required: true`, notes-free, concrete, canonical and unique by both request
  ID and scenario label; no row is ignored or normalized;
- its exact bytes remain bound by `approval_manifest_sha256`, and the manifest
  `source_instance` must already be canonical, must not be an `unknown`-class
  placeholder and must match any CLI override;
- an immutable limit requires `call_logs + request_trace_events + generated
  capture_gaps <= 200`; the producer never truncates, samples or paginates to
  satisfy it;
- one server-confirmed `READ ONLY`, `REPEATABLE READ` transaction performs the
  catalog preflight, source aggregates, both detail reads and every gate;
- the actual `public.call_logs` and `public.request_trace_events` relations must
  contain every snapshot-v2 column with a compatible type before any source-row
  read; `call_logs.usage_confidence` is additionally required to be the actual
  `NUMERIC(4,3)` source contract;
- every selected request has source coverage, all rows belong to one positive
  organization, no selected request spans organizations, and no request has
  more than one call log;
- final request/scenario/row/gap/count/source-ref sets must reconcile exactly;
- source `payload_ref` is absent, payload mode is exactly `none`,
  `metadata_only` or `hash_only`, and every exportable string, reference and
  metadata scalar passes the producer-local field/value policy without silent
  dropping, trimming or truncation; the already strict-validated metadata is
  also passed through `traceutil.SanitizeMetadata` and generation fails unless
  that sanitizer preserves it exactly. Metadata code strings use the current
  receiver-safe character grammar, and integer-like metadata values in the
  receiver-blocked absolute range `[1e9,1e15)` fail closed;
- aggregate gates reject source rows containing any current blocked-class
  marker (customer identifier, email, phone, URL, credential, raw error, raw IP
  or unreviewed free text) before detail publication;
- raw-IP or raw-upstream-error presence blocks strict generation. The two IP
  booleans remain the only permitted artifact proof fields and are both false
  for a qualifying source; this does not relax source qualification; and
- each source event hash is raw lowercase 64-hex. It is a UniRouter producer
  declaration, not a claim that X-Insight can recompute it from sidecar bytes.

This B1 path validates manifest bytes, content and source binding only. It does
not establish manifest-file custody; absolute/no-follow/current-owner/private-
mode/single-link manifest custody remains a future B3/import prerequisite and
is not implemented or claimed here.

Strict output is Linux-only. Before reading the DSN, opening the database or
reading source rows, the producer requires:

- an explicit absolute, lexically normalized, non-stdout destination;
- an already existing, symlink-free parent traversal whose final directory is
  owned by the current user with exact mode `0700`;
- a parent on the specifically qualified local ext4 backend, proven by both
  `fstatfs` and the parent descriptor's `STATX_MNT_ID` joined to the exact
  `ext4` entry in safely parsed `/proc/self/mountinfo`; the shared ext2/ext3/ext4
  superblock magic alone is insufficient, and ext2, ext3, NFS, remote, overlay,
  tmpfs and every other unproven filesystem type are unsupported;
- no existing final entry, whether regular file or symlink;
- a real anonymous `O_TMPFILE` inode with regular/current-owner/`0600`/zero-link
  invariants; and
- a verified `/proc/self/fd/<fd>` route for one atomic no-replace `linkat`
  commit.

All JSON bytes are encoded, written, `fsync`ed, descriptor-read back, length and
SHA-256 checked while the inode has no directory entry. One successful
`linkat` is the publication point and cannot later be downgraded by close or
diagnostic failure. `EEXIST` leaves the winning file or symlink unchanged. The
strict path never creates a named temporary file, writes under the final name,
renames, unlinks, retries the commit, or accepts a `cleanup_blocked` state.
Unsupported operating systems, kernels, filesystems or link routes fail before
DSN lookup and create no output. The initial accepted backend identity is
`linux/ext4/o_tmpfile/procfd-linkat`; no macOS, NFS, remote, overlay, tmpfs or
other-platform/filesystem equivalence is claimed.

The strict command shape is therefore limited to private custody paths whose
literal values are not copied into shared evidence:

```bash
UNIROUTE_PRD5_SNAPSHOT_DSN="<least-privilege read-only DSN supplied out of band>" \
go run scripts/prd5_xinsight_snapshot.go \
  --approval-manifest /approved/private-custody/approval.json \
  --output /approved/private-custody/snapshot.json
```

`--overwrite`, `--handoff-note`, stdout, relative output and direct request-ID
selection are not valid on this strict path. Manifest or artifact bytes, request
IDs, literal private paths, DSNs and field values must not be written to the
shared cooperation directory. The exact direct regression command is:

```bash
go test scripts/prd5_xinsight_snapshot.go scripts/prd5_xinsight_snapshot_test.go -count=1
```

The Linux ext4 anonymous-inode success test must execute, rather than skip, and
record the backend identity in the required `ubuntu-latest` backend check. A code merge alone does not activate
generation: an eligible project-owned non-customer current-schema source, exact
private request set, least-privilege read authorization, custody/expiry rules
and a later explicit binding decision remain prerequisites.

## Tool

The examples in this section describe the historical controlled Gate A surface.
Direct-ID selection, overwrite, handoff notes and shared-folder artifact paths
must not be reused for the strict internal-real path above.

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
  --output output/prd5/xinsight/gate-a-snapshot.json \
  --handoff-note output/prd5/xinsight/gate-a-snapshot-handoff.md
```

Use `--overwrite` only when intentionally replacing a local generated artifact.

After X-Insight requested approved scenario request IDs, Batch 1Q adds a safer
manifest-based selection mode:

```bash
UNIROUTE_PRD5_SNAPSHOT_DSN="<read-only postgres dsn>" \
go run scripts/prd5_xinsight_snapshot.go \
  --approval-manifest approved-request-ids.json \
  --output output/prd5/xinsight/gate-a-snapshot.json \
  --handoff-note output/prd5/xinsight/gate-a-snapshot-handoff.md
```

`--approval-manifest` is mutually exclusive with direct `--request-id`,
`--request-ids`, and `--scenario` flags. Manifest mode sets
`safety.selection_mode` to `approved_request_id_manifest`.

Each generated snapshot includes a source-created `exchange_id`. Manifest mode
also includes `approval_manifest_sha256`, calculated from the exact manifest
file bytes. Re-delivering the same unchanged artifact keeps its embedded
exchange ID; regenerating creates a new one. X-Insight calculates the artifact
SHA-256 after receipt and reconciles `(exchange_id, artifact.sha256)`.

Manifest mode requires a non-empty `source_instance`. An explicit
`--source-instance` must match the manifest value exactly, otherwise the tool
fails before reading a DSN. This prevents the hashed approval context from being
reused against a different source environment.

Approval manifest schema:

```json
{
  "schema_version": "unirouter.prd5.xinsight.approval.v1",
  "source_instance": "unirouter-dev-gate-a-2026-07-10",
  "requests": [
    {
      "scenario_label": "successful_non_stream",
      "request_id": "req-controlled-001",
      "approval_status": "approved",
      "required": true,
      "notes": "User/UniRouter owner approved this metadata-only export."
    },
    {
      "scenario_label": "provider_or_routing_failure",
      "request_id": "TBD",
      "approval_status": "pending",
      "required": false,
      "notes": "Not exported until a concrete request_id is approved."
    },
    {
      "scenario_label": "missing_trace_or_call_log_gap",
      "request_id": "",
      "approval_status": "unavailable",
      "required": false,
      "notes": "Not fabricated for Gate A."
    }
  ]
}
```

Only rows with `approval_status: "approved"` and a concrete request ID are
exported. Rows marked `pending` or `unavailable` remain non-exported.

The optional `--handoff-note` path writes a Markdown note after the snapshot JSON
is written successfully. It records generated date, source instance, schema
version, selected request IDs, scenario labels, counts, safety confirmation,
snapshot output path, generated capture gaps, and command shape without the DSN.

`--handoff-note -` is rejected so JSON and Markdown output cannot be interleaved
on stdout.

When manifest mode is used, the generated handoff command keeps
`--approval-manifest`; it does not convert the approved selection to direct
request flags.

## Selection Rule

The tool requires explicit `--request-id` / `--request-ids` values or an
`--approval-manifest`.

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
- usage, exact `cost_micros`, `currency=CNY`, `amount_scale=1000000`,
  `money_schema=micro-cny.v1`, status, latency, streaming, usage source, and
  confidence fields;
- sanitized trace metadata;
- generated source refs;
- generated source event hashes;
- capture gaps for selected request IDs with missing rows, including candidate
  capture-gap categories where UniRouter cannot assert one definitive cause.

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

- `schema_version`: `unirouter.prd5.xinsight.snapshot.v2`
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

Each `call_logs` row in snapshot v2 has one and only one money contract:

- `cost_micros` is required JSON string text matching `0|[1-9][0-9]*`;
- `currency` is required and exactly `CNY`;
- `amount_scale` is the required JSON integer `1000000`;
- `money_schema` is required and exactly `micro-cny.v1`;
- `cost_cents` is prohibited and must not be inferred as a fallback.

The operator producer reads PostgreSQL `BIGINT` through
`cost_micros::text`, attaches the three fixed companion fields and fails closed
before writing an artifact if the value is not canonical non-negative integer
text.

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

Batch 1N adds an optional `candidate_capture_gaps` array to generated
`capture_gaps` rows:

- missing `call_logs` rows use
  `candidate_capture_gaps: ["not_available_from_source"]`;
- missing `request_trace_events` rows with a matching `call_logs` row use
  `candidate_capture_gaps: ["sampled_out", "retention_expired",
  "not_instrumented"]`;
- missing `request_trace_events` rows without a matching `call_logs` row use
  `candidate_capture_gaps: ["not_available_from_source"]`.

The primary `capture_gap` remains `not_available_from_source` for generated
missing-row gaps. Candidate gaps are hints for X-Insight import and review; they
are not definitive UniRouter runtime claims.

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
- top-level counts and safety confirmation;
- generated capture gaps and candidate categories;
- snapshot output path;
- command shape without DSN;
- confirmation that raw prompt/response, credentials, raw IPs, and raw upstream
  errors are absent.

Prefer generating this note with:

```bash
--handoff-note "/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/<snapshot-name>-handoff.md"
```

Do not paste DSNs, credentials, tokens, raw prompts, raw responses, raw IP
addresses, or raw upstream errors into the handoff note.

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

Historical Batch 1O/1Q work established the approval gate before any controlled
artifact existed. Later Gate A runs generated and exchanged two bounded v1
artifacts under explicit authorization. The current producer contract is v2;
new ledger registrations and receipts must preserve the exact artifact schema
instead of defaulting to the historical v1 value.

The producer is an operator script with `//go:build ignore`; it is merged source
and can be run from an authenticated checkout, but it is not copied into the
runtime Docker image. A deployed micro-CNY database/runtime therefore does not
by itself prove that the producer script executed on that target.
