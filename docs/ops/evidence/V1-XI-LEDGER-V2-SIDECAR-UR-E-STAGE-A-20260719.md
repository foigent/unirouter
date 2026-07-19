# V1 X-Insight Ledger v2 UR-E Stage A Evidence

**Date:** 2026-07-19 (Asia/Shanghai)

**Batch:** `V1-XI-LEDGER-001 / UR-E Stage A`

**Owner:** Codex, acting as the single UniRouter Stage A execution owner

**Status:** Stage A generation and exclusive transfer complete for the exact
manifest-bound synthetic sidecar scenario; X-Insight Stage A.5 receiver
preflight and all Target work remain pending

## Scope And Claim Boundary

The binding cross-project decision is
[PRD2 UR-E Bounded Synthetic Sidecar Execution](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/decisions/2026-07-19-prd2-ur-e-bounded-synthetic-sidecar-execution.md>),
SHA-256
`c674b04f719b64d9746f2afb4f9d5020d7f49fe96af5d1254a83e7cd911595a1`,
activated at `2026-07-19 12:15:26 CST (+0800)`. The corrected owner response is
[UniRouter PRD2 V2 Sidecar External-Integration Artifact Response](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-19-unirouter-prd2-v2-sidecar-external-integration-artifact-response.md>),
SHA-256
`12857bd7a37436608461d22caf3c9f1fb8dbfac7274f446384349f488dba8cee`.

This run proves only the UniRouter producer and private operator-local transfer
half for one new metadata-only synthetic request. It is not X-Insight
acceptance, Target import, UI/readback, External integration verified,
customer-data, Production or completed-execution live-feed evidence. PRD6
remains closed.

The sanitized shared handoff record is
[2026-07-19 PRD2 UR-E bounded synthetic sidecar handoff](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/2026-07-19-prd2-ur-e-bounded-synthetic-sidecar-handoff.md>),
status `producer_complete_receiver_pending`. It contains identities and
results, not manifest/artifact bytes, DSNs, credentials, secrets or raw
payloads. The corrected producer-half file SHA-256 at baton release is
`17bd360c7972da2d149856b4f743f4d5d24f96f5fc36b5d21c0b04411ea4cd03`;
the whole-file SHA will necessarily change when X-Insight appends its governed
Stage A.5 half.

## Repository And Producer Authority

- Stage A used clean branch `codex/ur-e-stage-a`, created from aligned
  `main`/`origin/main`
  `9a94d54975017de3b6b209d47f561af32c072178`.
- U0 publication authority: PR #222 head
  `85b2614de31d1a44b0f4c03afc2e8f825d8d9f9d`, required CI run
  `29672169049`, squash merge `9a94d54975017de3b6b209d47f561af32c072178`.
- Actual producer code authority: PR #219 head
  `be2d5f5d1cd6d322728b3d7c9aa8a5a347650563`, required CI run
  `29640791281`, merge
  `5176654d7d320b56c1c136f39b3debb8156ece4b`.
- `scripts/prd5_xinsight_snapshot.go` SHA-256 at producer authority:
  `a2f2fc72f8fa8a6047ead34c8956ef81bc7690c33a8bc436695e9e4f10747185`.
- The complete producer-authority-to-generation-head diff contains seven
  paths, all under `docs/**`; the exact inventory is retained in the shared
  handoff.
- Direct producer contract test passed with exact Go 1.25.12:
  `GOTOOLCHAIN=local go test scripts/prd5_xinsight_snapshot.go scripts/prd5_xinsight_snapshot_test.go -count=1`.

The actual Go executable was
`/Users/m4/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.12.darwin-arm64/bin/go`,
reported `go version go1.25.12 darwin/arm64`, and had SHA-256
`8612de418d551a418517845c05cebdcfed49095cd08ef0a4d682bb2a5cf4896c`.
A first migration attempt using the wrong local base toolchain stopped before
schema or business writes. A second pre-write attempt with the exact toolchain
stopped before database connection because required ephemeral runtime
configuration was absent. The database was confirmed empty after both stops;
the retry used explicit synthetic non-secret placeholders that were neither
persisted nor recorded. The official producer later ran exactly once, without
`--overwrite`; its designated DSN variable referenced the task reader and the
fallback `DB_DSN` variable was absent.

## Isolated PostgreSQL 16 Source

Homebrew PostgreSQL `16.14` ran Unix-socket-only at `/private/tmp`, port
`55432`, with `listen_addresses=''`; no TCP listener was enabled. The cluster
data directory is `/opt/homebrew/var/postgresql@16`, system identifier
`7664096976983287589`. The task-owned database is
`unirouter_ur_e_v2_20260719_001`.

The normal CLI migration path applied 41 migration-ledger rows through
`000042_v1_local_login_identity`. Migrations first produced exactly one
bootstrap organization, user, wallet, project and project membership. To
satisfy the binding one-request/no-other-business-rows boundary after an exact
identity check, the Stage A owner deleted those five bootstrap groups. Each
delete affected one row. This narrow task-owned source action does not apply to
an existing, Target or customer database. The schema requires a non-null
organization scalar on `call_logs` but no source `orgs` row or foreign key; the
artifact carries explicit synthetic `org_id=900719001` while the isolated
`orgs` table remains empty.

The final source inventory was:

| Table or state | Count |
| --- | ---: |
| `call_logs` | 1 |
| `request_trace_events` | 0 |
| `schema_migrations` | 41 |
| `settings` | 15 |
| `micro_ledger_runtime_state` | 1 |
| `release_state` | 1 |
| `orgs`, `users`, `wallets`, `projects`, `project_members` | 0 each |
| all other business tables | 0 |

The no-password reader
`unirouter_ur_e_reader_20260719_001` has login, connection limit 1,
`default_transaction_read_only=on`, a 30-second statement timeout and SELECT
only on `call_logs` and `request_trace_events`. It has no superuser, createdb,
createrole, inherit, replication or bypass-RLS attribute. An actual reader
session proved read-only mode, permitted source-table SELECT and denied source-
table writes. The database's default PUBLIC temporary-object permission was
not separately revoked; the producer executed only its fixed SELECT path and
the before/after inventory proved no source or business-table mutation. This
is retained as a hardening observation, not represented as a stricter role
boundary than was actually exercised.

The one synthetic row used request ID
`req-ur-e-v2-synthetic-20260719-001`, source instance
`unirouter-ur-e-20260719-001` and source time
`2026-07-19T04:57:16.877007Z`, after activation. Its exact monetary evidence is
`cost_micros="314159"`, `currency=CNY`, `amount_scale=1000000`,
`money_schema=micro-cny.v1`; payload mode is `metadata_only`. It has no trace
row. The one-time PostgreSQL server was stopped cleanly after final read-only
inventory; cluster, database, role and log remain retained.

## Manifest, Artifact And Scenario Result

| Evidence | Exact value |
| --- | --- |
| Manifest sender | `/private/tmp/unirouter-ur-e-20260719-001.approval.json`; `0600`; 412 bytes; SHA-256 `b1c096df2e709520cbf7b2a7b5b94989b2fcacad295ea42a3deede46d1aff1f6` |
| Artifact sender | `/private/tmp/unirouter-ur-e-20260719-001.snapshot.json`; `0600`; 3985 bytes; SHA-256 `412c9b641cb6b6451a8b66f3ff808ef29750a7f3bedafdef93a473b66f641e7e` |
| Schema | `unirouter.prd5.xinsight.snapshot.v2` |
| Exchange ID | `uxchg_3a8559f2405c1351f16964235408fb89` |
| Generated time | `2026-07-19T05:01:28.276835Z` |
| Scenario | `usage_nonzero_cost_missing_trace_gap` |
| Actual inventory | requested IDs 1; call logs 1; trace events 0; capture gaps 1 |

The artifact embeds the exact manifest SHA. Its SHA differs from the historical
`93bc4c...` and `94c8db...` artifacts. Contract checks proved the exact
schema/request/source/scenario, exact non-negative micro-CNY tuple, absence of
`cost_cents`, and one generated `not_available_from_source` gap. Safety flags
are false for raw prompts, raw responses and API keys. The record has no
project/API-key/owner/payload reference, raw IP or raw upstream error. The
post-generation database inventory was unchanged.

## Exclusive Transfer And Receiver Equality

The only transfer mechanism was the corrected `ur-e-exclusive-copy-v1` code
block, SHA-256
`934a2036aa6fbccc95033f560d27eecf9f435d11da3f782fbf261e36a8811a74`,
run with exact Python 3.14.5 at
`/opt/homebrew/Cellar/python@3.14/3.14.5/Frameworks/Python.framework/Versions/3.14/bin/python3.14`.
The interpreter SHA-256 was
`2477b47fa3ae65b9574eb18a15edb364e96948eaa1875ad3f1c80d780efc9c12`.
All four identities were rechecked immediately before use.

The receiver paths were absent. The manifest was copied first and the artifact
second, once each, using atomic exclusive create. There was no retry,
overwrite, rename, substitution or cleanup path.

| Receiver | Result |
| --- | --- |
| `/private/tmp/xinsight-ur-e-20260719-001.approval.json` | separate inode; `0600`; 412 bytes; SHA-256 `b1c096df2e709520cbf7b2a7b5b94989b2fcacad295ea42a3deede46d1aff1f6`; external `stat`, SHA and bytewise `cmp` passed |
| `/private/tmp/xinsight-ur-e-20260719-001.snapshot.json` | separate inode; `0600`; 3985 bytes; SHA-256 `412c9b641cb6b6451a8b66f3ff808ef29750a7f3bedafdef93a473b66f641e7e`; external `stat`, SHA, bytewise `cmp` and receiver-structure checks passed |

These are non-Target operator-local paths. Stage A performed no Target access
or write.

## Retention And Next Gate

The exact cluster, database, reader role, PostgreSQL log, two sender files and
two receiver files are retained with review due by
`2026-07-20 23:59:59 CST (+0800)`. The custodian is the Codex execution owner
under User control. This is a review time, not automatic deletion authority.
No file, database, role, cluster, directory or downstream evidence was deleted
at Stage A closeout.

The shared handoff writer baton is released with status
`producer_complete_receiver_pending`. X-Insight must now append the Stage A.5
receiver half only after independently recomputing receiver hashes and
performing the binding decision's read-only Target/release/database/path and
source-qualified pre-existence checks. No Target byte placement or import is
authorized until that half says `stage_b_gate_passed`. Any mismatch must set
`stopped`; this artifact may not be replaced or regenerated under the same
decision.

## Verification Classification

| Layer | Result |
| --- | --- |
| Implemented | Existing producer authority is merged at `5176654d...` |
| Local | Stage A owner verified for the exact isolated synthetic generation and exclusive transfer described above |
| CI | Producer authority required CI `29640791281` passed; U0 publication CI `29672169049` passed |
| Target | Not exercised by Stage A |
| External integration | Not yet verified; X-Insight Stage A.5 and Stage B remain pending |
| Production | Not verified and not in scope |

All Stage A runtime assertions in this record are execution-owner evidence,
not independent runtime acceptance.
