# Counter v3 development Target acceptance — 2026-09-08

Cutoff: 22:03 China time. This closes the bounded counter-v3 Target/cancellation
slice, not the remaining concentrated qualification GUI or the whole product
workstream. No Production, model activation, price change or history rewrite.

## Source, CI and Artifact

- PR414 head `4265c48624b7e94f6af94a351a81299c38876d12` merged as
  `6a89eef6be75fc1a767e0258fa6c84109cef7924`; parents are exact75
  `03de41d65ca828c02c1790ff42f4afd8f21bf1d3` and that head. Tree
  `5591d3f3287f7763f8da89e96b63904715251eaf` matches the tested candidate.
- Version 1.2.76; CI34228828651 passed all five checks, Backend30m46s.
  Docker job102080000016 ran60 rollout and13 ingress tests and the actual
  carrier on Python3.10.21/Docker28.0.4. M115 inspect and lost-response cleanup
  passed, with zero ingress writes and Provider calls. Exact-main protocol
  run34232452659 passed.
- First76 CI34225310227 failed isolated PostgreSQL fixture setup before carrier
  execution. The fixture now waits for the final TCP server rather than the
  official image's temporary initialization socket. Original failure remains
  `/private/tmp/unirouter76-docker-failed.log`. Product assertions were not reduced.
- Official SendFile `f-wl06wg9hsoo28ld` delivered18385 bytes, root0600/no overwrite,
  SHA256 `54cd7fa9709349ff1c83c80af212092d456742537b2dd040742f2b73a2cb1623`.
  Native Git bundle prerequisite is exact75; runtime predecessor remained73.
- Build `t-wl06wg9n1phg5q8` succeeded once using the authorized temporary Aliyun
  Go proxy with checksum verification. Backend image
  `sha256:3714ea67f910b0dc77042bf3985ca7142565b05d0a23efb73e54a4961ff9fb91`;
  frontend `sha256:39a26a1588e56cbab24178c158904e35cfc40b55774b68df1dfe9bfe496af39f`.

## Target and two separate changes

Exact development instance `i-0jl0x8x34xgaake6li28`, cn-wulanchabu,8.130.18.85;
DB system7655821080247336993. Root60GiB is expanded and mounted. Pre-build
storage78percent/free12.44GiB passed the unchanged80percent/8GiB gate.

M115 was already applied with verified backup and protected-history comparisons.
This delivery added no schema migration or capability Publication. It used the
formal five-file Compose chain and existing runtime-ingress owner.

| Action | Plan digest | Apply | Result |
|---|---|---|---|
| 73→76, M115/v2 retained | `ec261cf8e49b7baf0014278fe8b4f73909a6c7cfc6582b47095cdc562d9be649` | `t-wl06wgaatjrj18g` | Complete;282 short samples,141 HTTP/1.1+141 HTTP/2,all pass |
| Same-image76 v2→v3 configuration | `a1e15e6d34cf97c7447f3e4497719e734f5f6fbf2e02910b8ad96a0210e89fe6` | `t-wl06wgb7lv3a800` | Complete;32 short samples,16 HTTP/1.1+16 HTTP/2,all pass |

Both plans and fresh backups passed before apply. Final stable container
`b3ec5d06aa86e3c199e428b0375288d13e34b0207c830ee48bac792f66f069d4` and canary
`82b3a67cd299c32fda422f440362a837c28b277c7db99f0b1a7b5e4b257ad943` run the
same exact76 backend/M115/counter-v3 and passed readiness. Protected schema,
39 Releases,17 Slots and13 Publications remained unchanged. FlashRelease59 and
Slot3 retain snapshot `75b94aa6b080dee5b85d99324f0b4490721d59c68a5bfc9e17c935be2934aa99`.

These are short-request observations from the pinned ingress-container client,
not a new loaded-long-stream or Agent task and not all-client zero downtime.
Earlier loaded-stream and normal Agent evidence retains its original scope.
The overall counter batch needed additional74/75/76 repair Artifacts; it is not
an end-to-end single-Artifact success. Only the final76 writer transition is
proved same-image. Failed74/75 Artifacts and plan records remain retained.

## One real cancellation and its financial boundary

Fresh Target proof `t-wl06wgbd2r4nq4g` confirmed both76/M115/v3 and the unchanged
FlashRelease59/Slot3. Zero-Provider preflight
`op_3cfe07e257299ce3357953639fc28abb` accepted8192 and projected8182 to the
provider under the unchanged Qwen v8 completion contract.

Request `op_459c35c4d5f0b879979aafb7e7b21383`, at13:56:01 UTC:

- HTTP200; client canceled after4 chunks/1021 bytes. Server delivery receipt
  records interrupted,6 chunks/1526 bytes,`downstream_stream_write_failed`.
- Exactly one recorded ProviderAttempt
  `att_c0e4e2ea759ec26bfb044867ae94e4b3`, route1/1, no retry authorization/fallback,
  deployment43,Release59,Slot3 and exact76 runtime generation.
- Lease pins `unirouter.stream-counter.v3`. Final checkpoint, Freeze and journal
  agree on estimated input1154/output10,source`counter_v3` and estimate-versionv3.
- One settled Freeze, one settled journal event and terminal receipt284. The
  additional `usage_prepared` journal event is preparation, not another settlement.
- Receipt authority is `gateway_counter`, finality`estimated`. No provider-final
  usage claim. Billing mode is `cost_observation`; calculated/charged amount0 is
  not proof of zero supplier cost or a positive customer-debit test.
- Historical v2 request `op_22779cdfb7645dfd10f3e3d80bcba983` remains v2,
  input1154/output29,receipt283. These two requests delivered different partial
  text; their counts are not an estimator savings/comparison experiment. Partition
  invariance and interrupted recovery/replay are established by the existing
  Local/CI arithmetic, relay and real-DB tests, not inferred from this difference.

Readbacks `t-wl06wgbiez3d3wg` and `t-wl06wgbvh1uyqrk` confirm the result;
ProviderAttempt total123→124, control-plane counts unchanged. Evidence and
assertions are under `/private/tmp/unirouter76-counter-acceptance-20260908/`.
Remote formal evidence is under `/opt/unirouter/deployment-evidence/20260908-artifact76`,
`20260908-rollout76` and `20260908-writer76-v3`.

## Remaining work and retained boundaries

Flash cumulative29/30, conservative reservedCNY4.92284/5; Max18/30,CNY4.689972/5.
These are budget reservations, not invoices. One exact shared Flash Probe can
still fit: one call/CNY0.03367, giving30/30 and reservedCNY4.95651. It has not run.

The connected Chrome UniRouter tab still redirects protected `/channels` to
`/login`. User console login is needed for the concentrated GUI acceptance:
one binding choice, server-derived one physical request/six facts, explicit
paid action, per-item results and refresh without resending. Do not create another
Release or repeat successful Agent calls just to exercise the new panel.

No new model, Hermes, admin credential/session-setting change or old-writer
retirement. New v3 writing works; historical v2 read/recovery compatibility remains.
The legacy reader is still required, not declared retired. 经验文件无需变化;
the Target/tool fixes are covered by existing compatibility/identity lessons.
