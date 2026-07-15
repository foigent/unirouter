# V1 Mimo Routing And Upstream Status — Target Evidence

**Date:** 2026-07-15 (Asia/Shanghai)

**Target:** existing Alibaba Cloud development/test ECS in `cn-wulanchabu`;
this is not a Production claim

**Result:** Mimo `mimo-v2.5-pro` real non-stream and stream routing is Target
verified for bounded single requests. The missing `call_logs.upstream_status_code`
regression is fixed by PR #210 and Target verified on the exact merged backend.
The approved test upgrade trust rotation, signed sequence `3` package and formal
release deployment also passed. DeepSeek, Kimi, capacity, external detector,
enterprise IdP, customer operations, Production and X-Insight remain separate.

PRD6 remains `Completed (Accepted Scope)`. This Post-PRD6 evidence does not
reopen a PRD6 row or accept PRD7 scope.

## Authorization and safety boundary

The User explicitly approved:

1. rotating the development target to a persistent **test** upgrade-signing
   key and deploying the exact PR #210 merge as a formally signed release; and
2. exactly one additional real Mimo non-stream request, with an additional
   UniRouter-ledger cap of CNY 0.35 and immediate cleanup of temporary access.

No DeepSeek, Kimi, detector, IdP, X-Insight or Production request was authorized
or sent. No provider secret, raw access key, password, JWT, prompt body or model
response is stored in this evidence.

## Confirmed regression and repository closure

Two earlier authorized Mimo requests on the prior backend completed successfully:

| Request | Mode | HTTP | Provider attempt | Usage | Charge | `call_logs.upstream_status_code` |
|---|---|---:|---|---|---:|---:|
| `op_04096b7660220f4e9bbd2656d1fbca77` | non-stream | 200 | `settled`, `known_usage`, HTTP 200 | 256 input / 8 output / 264 total | 272000 micro-CNY | `NULL` |
| `op_109052f1db2d2e47859c3444aa7df8bf` | stream | 200 | `settled`, `known_usage`, HTTP 200 | 256 input / 8 output / 264 total | 272000 micro-CNY | `NULL` |

The provider-attempt ledger was authoritative and correct, but both the normal
durable-outbox materializer and its synchronous fallback omitted the matching
attempt HTTP status when inserting `call_logs`. This was a confirmed evidence
regression, not a failed provider request and not a UI-only defect.

PR #210 closes both insert paths by looking up the exact
`(org_id, request_id, attempt_id)` attempt and persisting its bounded
`http_status`. Migrated integration coverage asserts the durable outbox and
synchronous fallback independently.

| Item | Exact evidence | Result |
|---|---|---|
| PR | #210, `fix(gateway): persist upstream status evidence` | Squash-merged |
| Exact PR head | `5f8cd6a4aedb74354dea9f33223d4aca95e3f2e9` | Required CI authority |
| Required CI | run `29419821405` | Change Routing, Backend Tests and Docker Build passed; Frontend Build was correctly skipped by change routing |
| Merge | `ae9ce2c927460e993dd00c6d81c8736cc5a015a2` | Backend release authority |

Before promotion, the current tree also passed full Go tests, vet and build;
the exact-head required Backend job ran the migrated PostgreSQL suite and the
commercial-License/signed-upgrade gate.

## Test upgrade key rotation

The shipped License Studio was opened first. The automated Mac UI controller
could not attach to the native XPC folder chooser, so that attempt was cancelled
without creating a file. This is an automation limitation, not evidence that a
human cannot use the double-click application. The controlled fallback used the
prebuilt vendor `uniroute-license keygen` program, which uses the same Ed25519
key format and exclusive no-overwrite semantics.

Persistent test-only files are outside the repository:

```text
~/Documents/UniRouter-Test-Keys/2026-07-15-ae9-upgrade/
  test-upgrade-20260715-b.private
  test-upgrade-20260715-b.public-keys.json
```

- key ID: `test-upgrade-20260715-b`;
- private file mode/size: `0600`, 89 bytes;
- public-ring file mode/size: `0644`, 75 bytes;
- raw public-key SHA-256 fingerprint:
  `0b0f720051321fa52ee080c04d87172410408dd00cfec63db3a82a9c024ef1d9`;
- public-ring file SHA-256:
  `050cd89b70b22ad4870d5559bda6acf3f9ed1ec7f8753e51e5154ab0b49d0ce4`.

Only the public ring was uploaded to the ECS. The private key was not copied to
the target, repository, image, database or logs. The existing test License root
`test-license-20260715-a` was unchanged. This approved test rotation replaces
the prior test upgrade root for the new formal backend; it is not a production
key-rotation ceremony or customer trust policy.

## Formal release and signed package

The exact merge was built in formal `release` trust mode with one embedded
License key and one embedded upgrade key:

- backend image:
  `sha256:fca7fe8fdabfcbfd32198c8bb4ba054e1b465f8342f671af39dba76b54b822d0`;
- build invocation: `t-wl06r058dnxgoao`;
- runtime version/commit: `1.0.0` /
  `ae9ce2c927460e993dd00c6d81c8736cc5a015a2`;
- target: `linux/amd64`;
- migration: `000037_v1_commercial_release_control`.

The sequence `3` archive is retained at:

```text
/opt/unirouter/incoming/unirouter-1.0.0-ecs-ae9-seq3.tar.gz
```

Its exact facts are:

- size: 7,962 bytes;
- SHA-256:
  `ac7413476b08e54561a8e243d5a0bc32916f8312443d62f82025613413ff3746`;
- signing key: `test-upgrade-20260715-b`;
- current-state window: sequence `2` only;
- created: `2026-07-15T14:31:29Z`;
- expires: `2026-07-22T14:31:29Z`;
- provenance SHA-256:
  `ca30b33ee13e18017258817c04c7eeb4d47a476451f83c5da0939e181c2dd5bb`.

Local verification with the new public ring passed. The old test ring rejected
the new signature, and current sequence `3` rejected replay because it is
outside the signed `2..2` window. Pre-cutover target verification against
sequence `2` and schema `000037` passed in invocation `t-wl06r05ncoynkzk`.

## Deployment incident, rollback and final cutover

The first ae9 deployment used the repository source Compose path by mistake.
That binary correctly reported `development`, `license_keys=0` and
`upgrade_keys=0`; the stored License was not enforceable and `/v1/models`
failed closed with HTTP 403. Release state remained at sequence `2`. Invocation
`t-wl06r03hfe9v7cw` immediately restored the prior formal a0a backend, active
License and normal 401 authentication boundary. No migration or data rollback
was needed.

The first formal canary cutover later reached a healthy application but its
operator assertion used `docker logs | grep -q` under `pipefail`. Early grep
exit caused producer `SIGPIPE`/141, so the script conservatively rolled canary
back. Readback invocation `t-wl06r05rh5yvshs` proved the old canary healthy.
The corrected check captured output before matching; it changed no application
code or acceptance criterion.

Final promotion passed:

- canary: `t-wl06r05t4b8c074`;
- stable: `t-wl06r05w15ietq8`;
- release record: `t-wl06r05yag7fym8`.

Stable and canary now both use the exact formal image above, exact ae9 working
directory, environment `development`, roles `stable` / `canary`, healthy Docker
state and restart count `0`. Startup readback reports:

```text
Release trust mode: release license_keys=1 upgrade_keys=1
Commercial license status: active enforceable=true
```

The frontend and Nginx intentionally remain on the unchanged a0a frontend
release; PR #210 has no frontend change. Database release state and history
contain sequence `3` exactly once with the exact commit, package hash, new test
key and schema. Re-verification rejects replay.

## Bounded real Mimo verification

Preflight proved the Mimo channel/key active and healthy, model scope exactly
`mimo-v2.5-pro`, no cooldown, no unresolved provider attempt and no outbox
backlog. The target base-price source rows are 100/200 CNY cents per thousand
tokens, exposed as CNY 1000/2000 per million tokens, with no organization
multiplier. These are target customer-ledger prices, not a supplier invoice.

Exactly one additional non-stream request was sent with `max_tokens=1` and a
short synthetic prompt. Result:

| Evidence | Exact result |
|---|---|
| Request | `op_1dc206219bcbafc45d4b9df2b66915c7` |
| Gateway/provider | HTTP 200 / Mimo channel 7, key 7, route 1 of 1 |
| Usage | 257 input / 1 output / 258 total; `provider_reported`, `final` |
| `call_logs` | `status_code=200`, `upstream_status_code=200`, non-stream |
| Provider attempt | `settled`, `known_usage`, `http_status=200` |
| Charge | 259000 micro-CNY = CNY 0.259, below the approved CNY 0.35 additional ledger cap |
| Wallet | balance 8246000 -> 7987000; total spent 2374000 -> 2633000; frozen 0 |
| Cleanup | temporary API key #40 disabled; QA user #13 disabled; request body/header temporary files removed individually |

All three bounded Mimo calls therefore account for 803000 micro-CNY in the
UniRouter customer ledger. This does not independently reconcile the supplier
invoice or prove capacity. The earlier temporary key #39, later key #40,
QA user #13 and developer user #14 all read back disabled.

## Final independent readback

Invocation `t-wl06r06zk3rlfcw` independently verified:

- public, stable and canary `/health/ready`: HTTP 200 with PostgreSQL/Redis OK;
- public, stable and canary unauthenticated `/v1/models`: HTTP 401, proving the
  request reaches authentication rather than a License failure;
- stable/canary formal image, exact ae9 commit, healthy, restart count `0`;
- formal release trust `1/1`, active enforceable test License, no panic/fatal/
  invalid-License startup signal;
- migration `000037`, release state sequence `3`, history sequences `1..3`;
- new call log upstream status 200 and exact settled attempt/ledger conservation;
- zero unresolved provider attempts and zero outbox rows;
- temporary actors disabled; root filesystem 56% used.

## Issue disposition

| Issue | Classification | Status / next owner |
|---|---|---|
| `V1-UPSTREAM-STATUS-001` | confirmed bug/regression | **Target verified / closed** by PR #210 and the ae9 Mimo request |
| `V1-TARGET-ROUTING-001` | target environment configuration | **Mimo `mimo-v2.5-pro` verified; remainder open.** DeepSeek/Kimi and other catalog identifiers still require explicit target review; no bulk normalization |
| `V1-TARGET-PILOT-001` | target/Production evidence | Mimo bounded non-stream/stream semantics are verified; capacity, detector, IdP, customer backup/restore/upgrade and Production remain open |
| `V1-XI-LEDGER-001` | X-Insight coordination | unchanged; UniRouter-only target evidence is not consumer acceptance |

## Preserved non-claims

1. No DeepSeek or Kimi request was sent and their target model scopes were not
   edited.
2. No capacity/load, failover-under-load, detector, enterprise IdP, customer
   restore or Production/TLS/HA test was run.
3. No supplier invoice/balance API was reconciled; the exact amounts above are
   the UniRouter customer ledger.
4. The test License, test keys and development ECS do not substitute for
   production key custody, customer issuance or notarized distribution.
5. X-Insight v2 consumer acceptance remains external coordination work.
6. The earlier no-paid-call evidence remains historically correct at its own
   checkpoint; this document supersedes that boundary only for the explicitly
   authorized Mimo calls.
