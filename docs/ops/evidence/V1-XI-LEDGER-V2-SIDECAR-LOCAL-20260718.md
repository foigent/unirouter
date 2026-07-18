# V1 X-Insight Ledger v2 Sidecar Local Evidence

**Date:** 2026-07-18 (Asia/Shanghai)

**Issue:** `V1-XI-LEDGER-001`

**Owner:** Codex

**Status:** UniRouter-side Local verified with synthetic, metadata-only data;
UniRouter PR/required CI and X-Insight-owned consumer acceptance pending

## Frozen boundaries

- PRD6 remains `Completed (Accepted Scope)`; this batch does not reopen it.
- The accepted joint runtime remains the operator-mediated
  `xinsight.import-receipt.v1` sidecar.
- No completed-execution read model, endpoint, cursor/replay, service token,
  continuous polling, p95 freshness gate, Production access or customer data
  was added or used.
- UniRouter's code-bearing release remains
  `cdbaa972db9b318b4412dd2aebce118c934debac`; docs-only current main was
  `72a0a05e64085a53c4832c6d91af59a598406e5c` at branch creation.
- The current target's micro-CNY runtime evidence does not prove that the
  build-ignored operator producer ran on that target.

## Compatibility findings and corrections

1. The producer already emitted snapshot v2, but the UniRouter exchange form
   still defaulted to snapshot v1. Receipt registration requires the ledger and
   artifact schema versions to match. The form now defaults to v2, with a
   frontend regression.
2. The producer and its tests use `//go:build ignore`, so ordinary
   `go test ./...` did not exercise the contract. Required CI is now configured
   to run the script test directly. The regression covers the actual SQL field,
   row mapper, exact v2 money tuple, absence of `cost_cents` and fail-closed
   rejection of negative micro-CNY.
3. The technical reference described the producer as if no controlled
   artifact existed and did not document v2 money. It now distinguishes merged
   operator source, historical v1 exchanges, local v2 generation and the lack
   of a packaged/deployed producer component.
4. The unchanged `xinsight.import-receipt.v1` envelope accepts a v2 artifact;
   a UniRouter backend regression covers that exact combination.

## Exact synthetic artifact

The actual UniRouter producer ran against an isolated local PostgreSQL database
containing one synthetic call-log row:

| Field | Value |
| --- | --- |
| request ID | `req-xi-v2-micro-7-synthetic` |
| schema | `unirouter.prd5.xinsight.snapshot.v2` |
| `cost_micros` | JSON string `"7"` |
| `currency` | `CNY` |
| `amount_scale` | `1000000` |
| `money_schema` | `micro-cny.v1` |
| artifact bytes | 3611 |
| artifact mode | `0600` |
| SHA-256 | `93bc4c8448f1f838668f9dab10cbba070f0df550665b16d73bb494066fe8d6a1` |

Safety flags were false for raw prompts, raw responses and API keys. The
artifact contained one call log, no trace row and one generated
`not_available_from_source` capture gap. At observation time the exact bytes
existed as the untracked X-Insight worktree fixture
`fixtures/unirouter_snapshot/micro_money_v2_generated.json`; this task makes no
X-Insight commit or publication claim.

After adding the query/mapper regression and producer-side negative-money
guard, the actual producer was run again against the same isolated synthetic
database. The final temporary artifact was mode `0600`, 3763 bytes, SHA-256
`94c8db5610037e87c18e196aecdf40ce27888e9375c14f139beb8c7953dfa4a5`,
and retained the exact v2 tuple with no `cost_cents` or raw-content flags.

## Non-modifying X-Insight worktree observation

The local probe left X-Insight's current uncommitted worktree unmodified and
wrote import/readback state only to task-owned isolated PostgreSQL databases.
This task does not own, publish or accept that consumer. The observed patch:

- strictly accepts v1 cents or the complete v2 four-field money tuple, never a
  mixed or partial tuple;
- retains `cost_micros` as text through JSON validation, JSONB persistence,
  API DTO and UI `BigInt` formatting;
- allocates a distinct stable v2 adapter manifest ID
  `22222222-2222-4222-8222-222222222223` while preserving the historical v1
  ID, preventing same-source-row v1/v2 idempotency conflicts and mapper
  misattribution;
- makes the exported event builder validate malformed snapshots instead of
  panicking.

Independent review also found an unresolved X-Insight-owned compatibility bug:
after a v2 import, a later valid v1 replay can downgrade the mutable source
registry to v1 even though v2 events remain persisted. Existing smoke coverage
only checks that the source list is non-empty. This does not corrupt the exact
money event readback proven below, but it prevents consumer acceptance and
External-integration closure until X-Insight fixes and verifies monotonic
version behavior (or adopts a non-mutable version model).

The actual X-Insight migration and importer CLIs ran against a fresh isolated
local database:

| Check | First run | Identical replay |
| --- | ---: | ---: |
| accepted in-memory | 2 | 2 |
| in-memory rerun duplicate/no-op | 2 | 2 |
| persisted inserts | 2 | 0 |
| persisted duplicate/no-op | 0 | 2 |
| persistence conflicts | 0 | 0 |
| readback events | 2 | 2 |
| readback capture gaps | 1 | 1 |

The two sanitized receipt files used the unchanged
`xinsight.import-receipt.v1` envelope. The first receipt SHA-256 was
`999805eee109abbe2f0a297c1ce8e24c3521dd385f7a442acec6ccea96167281`;
the replay receipt SHA-256 was
`865ae3d436ac624752a4a02e8bb626db191a92a3813145f2e2b5764f46a2f128`.
Both were mode `0600`. The warnings correctly recorded the synthetic
artifact's missing approval-manifest hash and its explicit source capture gap.

Direct SQL returned:

```text
source_system_id=11111111-1111-1111-1111-111111111111
adapter_manifest_id=22222222-2222-4222-8222-222222222223
source_version=unirouter.prd5.xinsight.snapshot.v2
cost_micros=7
currency=CNY
amount_scale=1000000
money_schema=micro-cny.v1
jsonb_typeof(cost_micros)=string
cost_cents_present=false
event_count=2
capture_gap_count=1
```

The actual loopback server returned one searchable execution with two evidence
events and one gap. Its event-detail API returned
`"cost_micros":"7"`, the three companion fields, the v2 source version and
v2 adapter manifest; `raw_source_excerpt` was absent. A real Chromium
desktop/narrow E2E run passed 4/4. A separate Playwright CLI run against the
actual PostgreSQL-backed server displayed:

```text
精确成本（micro-CNY）  CNY 0.000007 (7 micro-CNY)
币种                  CNY
金额缩放因子          1,000,000
金额语义版本          micro-cny.v1
```

## Local verification

- UniRouter full Go race suite: pass.
- UniRouter `go vet ./...` and `go mod verify`: pass.
- UniRouter producer query/mapper/negative-boundary direct test: pass.
- UniRouter actual post-patch producer run against the isolated synthetic
  PostgreSQL database: pass.
- UniRouter Data Exchange backend tests: pass.
- UniRouter frontend type-check: pass; 41 files / 278 tests pass; production
  build pass. The focused Data Exchange file passed 5/5.
- X-Insight PostgreSQL-backed first-write, identical replay, search/API and
  real-browser checks: observed pass against its uncommitted worktree.
- X-Insight existing test suites also passed in that worktree, but they did not
  cover the later-v1 source-registry downgrade found by independent review.
- UniRouter required CI: pending at this checkpoint.
- UniRouter `git diff --check`: pass at this checkpoint.

## Classification

| Layer | Result |
| --- | --- |
| Implemented | UniRouter worktree patch only; publication pending |
| Local verified | Yes for UniRouter producer/ledger behavior; synthetic cross-repository compatibility probe observed |
| CI verified | Pending for the UniRouter patch |
| Target verified | No for this v2 producer/consumer pairing |
| External integration | Not closed; X-Insight-owned fix, publication and acceptance pending |
| Production verified | No |

No Target or Production mutation occurred. Temporary local databases, snapshot
and receipt files are retained only until final evidence/CI identities are
recorded, then must be removed by exact path/name with absence checks.
