# V1 X-Insight Ledger v2 Sidecar Local Evidence

**Date:** 2026-07-18 (Asia/Shanghai)

**Issue:** `V1-XI-LEDGER-001`

**Owner:** Codex

**Status:** UniRouter implemented and required CI verified with synthetic,
metadata-only data; the historical X-Insight consumer defect is published and
named-Target fixture proof is execution-owner verified; fresh External
integration remains pending joint activation and execution

## Frozen boundaries

- PRD6 remains `Completed (Accepted Scope)`; this batch does not reopen it.
- The accepted joint runtime remains the operator-mediated
  `xinsight.import-receipt.v1` sidecar.
- The User accepted the shared
  [2026-07-18 completed-execution live-feed deferral](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/decisions/2026-07-18-prd2-completed-execution-live-feed-deferral.md>).
  This defers the whole slice from the first formal version; it does not accept
  the proposed feed contract or waive any unimplemented feed gate.
- No completed-execution read model, endpoint, cursor/replay, service token,
  continuous polling, p95 freshness gate, Production access or customer data
  was added or used.
- The later UniRouter
  [UR-E U0 response](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/2026-07-19-unirouter-prd2-v2-sidecar-external-integration-artifact-response.md>)
  accepts one new bounded synthetic v2 proof. Its
  [joint decision candidate](</Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/decisions/2026-07-19-prd2-ur-e-bounded-synthetic-sidecar-execution.md>)
  remains proposed/non-binding; no manifest, artifact, transfer or handoff
  evidence exists from U0.
- UniRouter PR #219 exact head
  `be2d5f5d1cd6d322728b3d7c9aa8a5a347650563` passed required CI run
  `29640791281` and squash-merged as code-bearing commit
  `5176654d7d320b56c1c136f39b3debb8156ece4b`.
- The current target's micro-CNY runtime evidence does not prove that the
  build-ignored operator producer ran on that target. PR #219 was not deployed;
  development-target authority remains PR #217 / `cdbaa972`.

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

## X-Insight dirty-worktree observation (non-acceptance)

The local probe used the then-current uncommitted X-Insight worktree, which
contained user-existing and exploratory changes, and wrote import/readback
state to task-owned isolated PostgreSQL databases. After the ownership
correction, this closeout made no further X-Insight worktree writes and did not
stage, commit, push, reset or clean that repository. It does not own, publish
or accept that consumer. The observed patch:

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

Independent review also found an X-Insight-owned compatibility bug at this
historical checkpoint: after a v2 import, a later valid v1 replay could
downgrade the mutable source registry to v1 even though v2 events remained
persisted. Existing smoke coverage then checked only that the source list was
non-empty. This did not corrupt the exact money event readback proven below,
but it prevented consumer acceptance at that time. X-Insight later fixed and
published monotonic source-version behavior in PR #52 / merge `0462e698`; the
subsequent evidence boundary is recorded below.

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
- X-Insight existing test suites also passed in that worktree, but at that
  checkpoint they did not cover the later-v1 source-registry downgrade found
  by independent review; PR #52 later added the missing correction/regression.
- UniRouter required CI run `29640791281`: Change Routing, Backend Tests,
  Frontend Build and Docker Build all pass. The direct producer contract step
  passed inside Backend Tests.
- UniRouter `git diff --check`: pass at this checkpoint.

## Subsequent X-Insight-Owned Publication And Target Boundary

This UniRouter evidence preserves the historical 2026-07-18 probe. Later
read-only reconciliation verified the following X-Insight-owned publication
chain without modifying the X-Insight repository:

- PR #52 merged as `0462e698775488d86e1263debf64990239464340`;
  PR CI `29645951982` and exact-main CI `29646094307` succeeded. The merged
  consumer preserves monotonic source versions and separate v1/v2 mapper
  identities.
- PR #54 merged as `0ca502f99181173e3542592ae5a018a5bfc43e0c`;
  exact-main CI `29648815418` and owner-supervised workflow `29648976737`
  succeeded for the rc.6 release identity.
- PR #55 merged as `6b1ea9002a7a8769473d3ceb4b180bf44d6ee6c4` and records
  the historical `93bc4c...` fixture on the named non-production Target.
  Those Target details are X-Insight execution-owner evidence, not independent
  acceptance by UniRouter and not a fresh producer-to-consumer handoff.

Accordingly, the historical consumer defect and publication gap are closed.
The historical fixture pairing is Target execution-owner verified only.
External integration remains open until a genuinely new manifest-bound
artifact completes the separately governed UR-E generation, transfer,
first-write, identical replay and bounded readback proof. U0 itself performs
none of those operations.

## UR-XI-01 through UR-XI-10 fact matrix

The request defines UR-XI-02 through UR-XI-10 around the proposed live feed.
The accepted sidecar result must not be used as a substitute for those gates.

| ID | Current fact | Result |
| --- | --- | --- |
| UR-XI-01 | Current docs-only head is `f96fb039`; code-bearing PR #219 merge is `5176654`, with required CI run `29640791281` green | Satisfied |
| UR-XI-02 | The User accepted a deferral decision linked from the UniRouter truth chain; the feed contract was not accepted and this task did not modify the X-Insight truth chain | Closed for current scope by deferral; not satisfied as a feed-enable gate |
| UR-XI-03 | The accepted sidecar preserves UniRouter independence; proposed feed failure/slow-path isolation has no executable evidence | Not satisfied; explicitly deferred |
| UR-XI-04 | No universal completed-execution terminal ledger covers all required success/failure/stream fixtures | Not satisfied; explicitly deferred |
| UR-XI-05 | The synthetic sidecar proved no raw content, but no feed endpoint/cross-organization negative conformance exists | Not satisfied; explicitly deferred |
| UR-XI-06 | No feed cursor, revision, replay expiry, reset or restart contract is implemented | Not satisfied; explicitly deferred |
| UR-XI-07 | No X-Insight-scoped service authentication exists | Not satisfied; explicitly deferred |
| UR-XI-08 | No live-feed limit, 8 MiB, overload or replay-retention behavior exists | Not satisfied; explicitly deferred |
| UR-XI-09 | The synthetic sidecar reached search/API/UI, but no live completion-to-search run with restart/outage recovery exists | Not satisfied; explicitly deferred |
| UR-XI-10 | No terminal-to-search freshness distribution or p95 live-feed measurement exists | Not satisfied; explicitly deferred |

The accepted decision removes UR-XI-03 through UR-XI-10 from the current first
formal version; it does not mark them passed, waived or Production-ready. A new
explicit product decision must reopen the whole minimum slice before either
project implements live transport.

## Classification

| Layer | Result |
| --- | --- |
| Implemented | Yes in UniRouter main merge `5176654d7d320b56c1c136f39b3debb8156ece4b` |
| Local verified | Yes for UniRouter producer/ledger behavior; synthetic cross-repository compatibility probe observed |
| CI verified | Yes for UniRouter, required CI run `29640791281` |
| Target verified | Historical `93bc4c...` fixture pairing is X-Insight execution-owner verified on the named non-production Target; independent acceptance is not claimed |
| External integration | Not closed for a fresh producer handoff; UR-E is accepted by UniRouter but remains proposed/non-binding pending atomic activation and execution |
| Production verified | No |

## Cleanup

No Target or Production mutation occurred. After recording the artifact and CI
identities, the following task-owned state was removed one item at a time and
verified absent:

- `/private/tmp/unirouter-xi-v2-019f747b.json`;
- `/private/tmp/unirouter-xi-v2-postpatch-019f747b.json`;
- `/private/tmp/xinsight-xi-v2-receipt-019f747b.json`;
- `/private/tmp/xinsight-xi-v2-receipt-clean-019f747b.json`;
- `/private/tmp/xinsight-xi-v2-receipt-replay-019f747b.json`;
- PostgreSQL databases `unirouter_xi_v2_019f747b`,
  `xinsight_xi_v2_019f747b` and `xinsight_xi_v2_cli_019f747b`.

No temporary role or credential was created. No customer data or X-Insight
worktree file was deleted during cleanup. The deleted temporary files/databases
are not recoverable except from an independent backup; the sanitized hashes
and durable evidence above remain in this repository.
