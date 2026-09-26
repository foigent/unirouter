# Delivery/onboarding 1.2.77 — development Target and GUI verified

Cutoff: 2026-09-09 04:10 China time. This is a bounded delivery/onboarding task before the user's
separate X-Insight integration and complete test campaign. It does not close that
future work or repeat historical Agent/model acceptance.

## Plan, value and acceptance

| Priority | Outcome/value | Acceptance | Current evidence |
|---|---|---|---|
| P0 | Detect tool incompatibility before expensive builds | Formal build runs the same local-daemon environment check before image selection/build; unsupported Python/Docker/Compose fails closed | Local/CI and actual Target pre-build pass |
| P0 | Make evidence understandable and approvals explicit | Latest Probe, its exact Approval and effective qualification are distinct; one selected existing fact approved, refresh does not send | Local/CI and real Target GUI pass |
| P1 | Continue configuration without repeated credentials | Saved model links to its exact Deployment/Chat operation; duplicates remain separate | Local/CI and real Target GUI pass |
| P1 | Produce usable client configuration | Stable model, gateway URL and key placeholder; invalid provider name blocks sync export | Local/CI and real Target GUI pass |
| P1 | Frontload CI without weakening coverage | Cheap compatibility checks run before expensive jobs; unknown classification remains conservative; all applicable required checks pass | All five checks and exact-main protocol pass |
| P0 | Freeze a verified integration baseline | Exact reviewed main, immutable images, formal plan/backup/rollout/inspect, representative GUI and precise retained evidence | Formal rollout/inspect and representative GUI pass |

The implementation changes no schema, parser, model identity, Release/Slot
semantics, pricing or metering contract. It adds no model/Provider, fallback,
Production change, credential, X-Insight integration or old-writer retirement.
Original paid-call histories remain; the additional shared30-call/CNY5 pool is unused.

## Source, review and required checks

PR417:https://github.com/foigent/unirouter/pull/417

- Base44a6a158f28f05fb447b183bc529ea7499c02c56.
- Head67632ab0f10a4b4f99ed7f365ff2869c0870238d;26 paths,+610/-44.
- Mergecd9f354b8a104a6f0677345a5694b19c000ab015; parentsbase/head above.
- Exact head/merge tree7725ffa81b6f6897f5ef6ff843f308c05d44fd63.
- Independent pre-code and closing review completed; required findings resolved.
  [Design](../../analysis/DELIVERY-ONBOARDING-CLOSEOUT-20260909.md),
  [review](../../analysis/DELIVERY-ONBOARDING-CLOSEOUT-20260909-REVIEW.md).
- Local65 rollout,13 ingress,6 wrapper and14 CI-routing tests pass; UI workspace20
  and page32 pass; real-DB ordering tests and reviewer race/integration pass.
  Frontend types/API152 checks and production budget pass. An initial combined
  Go command referenced nonexistent internal/apicontract; the corrected actual
  cmd/uniroute API checks passed. It is not reported as a product-test failure.
- PR CI34264407072: Change Routing23s, Backend31m25s, PR4 Browser4m18s,
  Frontend7m24s, Docker2m14s all successful; no rerun. Frontend88 files/739 tests.
  Exact-main protocol34268228598 successful atcd9f354b.
- CI actual host Python3.10.21/Docker28.0.4/Compose2.38.2; candidate carrier
  lifecycle/response-loss cleanup remained tested after build.

## Immutable Artifact and actual Target host check

Native source bundle47307 bytes,
SHA256b8825f641cde962e6e0e49eaedcb73d939588cac3256e4bccb82564d86a2751f.
Transferred through the authorized official Aliyun Workbench upload. The upload
extension denied file access; native chooser completed the same transfer without
changing extension permissions. Existing passwordless Workbench login was used;
no administrator credential or security configuration changed.

GUI upload produced0644; the first wrapper rejected before importing source or
building an image. Exact owner/size/digest were checked, and only that uploaded
file's mode tightened to0600. The successful invocation was separately retained.

Target:cn-wulanchabu/i-0jl0x8x34xgaake6li28/8.130.18.85,
hostnameiZ0jl0x8x34xgaake6li28Z; development/test only.
Source:/opt/unirouter/releases/cd9f354b8a104a6f0677345a5694b19c000ab015.
Actual mandatory host check passed before building on Python3.10.12,
Docker29.6.0 client/server,Compose5.2.0; an actual container's nanosecond
StartedAt was parsed. The authorized Aliyun Go proxy was temporary to the build;
checksum validation was retained. No persistent mirror/daemon change.

| Component | Exact tag | Image ID |
|---|---|---|
| Backend | unirouter-release/backend:cd9f354b8a10 | sha256:65b71f325e86f54f9bbfb41971f222e8abc2c9bf01904fe090834a44e17a5669 |
| Frontend | unirouter-release/frontend:cd9f354b8a10 | sha256:2311c37d04613655d31c17688b6ba2d9b1fd22f40efb0bdb79db8850df7b6cbe |

Build evidence:/opt/unirouter/deployment-evidence/20260909-artifact77.
Source/tree/version/component labels verified. One actual build per component.
Official build invocationt-wl06wh5504y9nnk succeeded. Artifact success is not Target rollout success.

## Resolved storage gate and bounded recovery

Post-build storage:81% used,10.74GiB available; existing gate maximum80%,minimum8GiB
refused. Pre-build80%/11.60GiB had passed. This refusal occurred before any
rollout action; the old healthy1.2.76/M115/counterv3 service stayed in place until
the user-approved capacity remedy below passed.

The user explicitly approved one exact Go compiler-cache deletion:
`ed6ven59z7bpdd4pvn3u0xo3s`, created2026-09-07T17:34:05.962594051Z,
typeexec.cachemount,reclaimable=true,shared=false. The existing lifecycle lock
and fresh identity checks protected the native Docker ID/type-scoped prune.
Official invocationt-wl06wh7to8mk64g succeeded once:581885952 bytes reclaimed,
81%→80%,11.29GiB free. The record is absent and image/container/volume identity
inventory is unchanged (SHA25620c4a2e88fda930e912810613aeaec9516c0cadf75790d5cb572839be474f136).
No image,container,volume,database,backup,release or evidence was deleted; no
retention policy or storage threshold changed. Subsequent builds may recompile
packages. This capacity remedy is complete; rollout acceptance follows below.

Diagnostic failures remain bounded and read-only: a broad du timed out; an initial
Buildx Size parser assumed bytes but this host returns human units; bare private
filter was rejected; du until filters still included recent records and were not
accepted as retention proof. Full inventory shows33 reclaimable/nonshared records,
all used within26hours. No repeated broad scan, lower retention sweep or prune.
These are diagnostic-tool limitations, not new product compatibility claims.

## Formal rollout and final runtime

Plan, verified backup/prepare, apply and inspect all completed through the existing
formal owner, with no new deployment implementation or on-server source patch.

- Operation: `formal-roll77-cd9f354b`.
- Evidence: `/opt/unirouter/deployment-evidence/20260909-rollout77`.
- Plan digest: `d864a95aa8f0bf1217e103ee1428145ecedeba33a9a5d4daeb05d936f4d4d3d6`.
- Backup: 1,905,423 bytes; verified with pg_restore list; digest
  `0369b604a69a9c07967e165eca8170a1f55a2a40f89b5520d33f09486dfd592b`.
- Plan/prepare invocations: `t-wl06wh7w8hj2f40` / `t-wl06wh7yglrto1s`.
- Apply/inspect: `t-wl06wh8276uvhfk` / `t-wl06wh86xouv9xc`; both report complete.
- Final stable container: `47223017e9f9a2b7cf981da40e8fc4d372bc12e715de0e39dd0a4d4211d4af6a`.
- Final canary container: `07b1702dd49023e541c2afee6273ac9f42f272f6f24b15909c05192554d979c2`.
- Final frontend container: `883f9e645312379bfc414227f32faa91215e9faf52375d0b156b01f172704007`.
- All use the exact 1.2.77 images above. Backend health/readiness passed; both
  retain `unirouter.stream-counter.v3`. Schema remains M115.
- Final storage gate passed at 80% used / 11.28 GiB available.

The formal observer recorded 48 passing unauthenticated short requests from
04:01:35 to 04:02:14 China time: 24 HTTP/1.1 and 24 HTTP/2, all expected structured
401 responses, zero recorded failures. The qualified client was curl 8.19.0 in the
unchanged ingress container/image. This is `passed_short_requests_only`, not a
new long-stream, full Agent, all-client or positive-charge settlement proof.
All 11 formal stages completed. Schema, Publication, Release and Slot protected
projection digests matched their frozen plan values through final inspection.

## Real GUI acceptance and no paid resend

The authenticated development console visibly reports 1.2.77. Existing connection
14, credential binding 14 and Deployment 43 were reused; no Key was read or rewritten.

1. Latest Probe 259–264 each showed successful verification awaiting its own
   approval. Effective Approvals 224–229 separately pointed to Probe 247–252.
2. Only Function Tools Probe 259 was selected. The user-visible reason limited
   the action to development/test GUI acceptance, reused observation and no Release switch.
3. One product API action created Approval **236**, source_kind
   `admin.model_capability_routing_approval.v1`, source_ref `capability_probe:259`,
   at 04:04:48 China time. It binds Deployment 43, connection 14,
   `openai.chat_completions.v1`, `function_tools`, Profile `qwen.3.7-flash.api@1.9.0`
   and Probe contract `unirouter.capability-probe.qwen-chat-stream-tools.v2@1.0.0`.
4. The UI showed “此记录已批准”, exact Approval 236 / Probe 259, and disabled repeated
   selection of that record. The other five effective approvals stayed unchanged.
5. “刷新逐项状态” preserved the result without another Probe or approval. The paid
   verification confirmation remained unchecked; no Provider request was sent.
6. `Bad Provider` produced a precise validation message and hid the sync command.
   Restoring `unirouter` restored the command. The generated client configuration
   retained `https://8.130.18.85/v1`, stable `qwen3.7-flash` and a Key placeholder.
   No global OpenClaw default or actual credential was modified.
7. “接入模型 → 向已有连接添加模型 → 阿里云百炼连接 #14” showed distinct links for
   configured Flash Deployments 41, 43 and 40. Clicking “查看模型 #43” returned to
   exactly Deployment 43 / Chat operation, with Approval 236 visible. No model,
   connection or credential was created; no platform discovery or paid Probe was repeated.

| Authority | Before GUI | After approval, refresh and continuation |
|---|---:|---:|
| Capability Probes | 264 | 264 |
| Capability Approvals | 235 | 236 |
| Releases | 39 | 39 |
| Slots | 17 | 17 |
| Capability Publications | 13 | 13 |
| Billing ProviderAttempts | 124 | 124 |

Flash Release 59 / Slot version 3 is unchanged. Execution snapshot remains
`75b94aa6b080dee5b85d99324f0b4490721d59c68a5bfc9e17c935be2934aa99`;
route hash remains `sha256:546b5c0f7763a530f200d172ecca20a59defa554450897e356b93edebad9cd1f`.
Original Approval 224 is retained; the other five still pin 248–252. All six latest
facts continue to share source `op_0d2775e36a1708fdae770862e15f2c2f`.

Readbacks: before `t-wl06wh8752e2qdc`; final `t-wl06wh8p4a6y8zk`;
rollout summary `t-wl06wh8l2ui9z40`. One intermediate readback had a whitespace
error in its final SELECT after returning the correct counts/pins. It was kept as
failed and the remaining read-only assertion corrected; no product operation was replayed.
Browser locator failures likewise occurred before submission; only one Approval was sent.

## Closure and next-task boundary

The bounded delivery/onboarding task is complete through Local, independent review,
CI, exact Artifact, development Target and real GUI. There were zero additional paid
model calls in 1.2.77 acceptance. Original pools remain Flash 30/30 with CNY 4.95651
reserved and Max 18/30 with CNY 4.689972 reserved; the extra shared 30-call/CNY 5
pool remains unused. These are experiment reservations, not supplier invoices.

Historical model/Agent protocol and answer-quality results retain their original
scope. Historical long-stream/cancellation and estimated settlement evidence is
not replaced or enlarged by this UI acceptance. No Production, old-writer
retirement or X-Insight integration is claimed.

The next user-defined X-Insight integration/full-test task can start from runtime
**1.2.77 / M115 / counter v3 / commit cd9f354b**, with exact images and model pins
above. This evidence-only documentation change does not require another backend
build or Target rollout. Confirm integration scope and use the existing supported
interfaces in that separate task; no integration credentials or traffic were created here.

MAR-008 was revised with the implemented pre-build environment check. No additional
lesson or permanent gate is introduced for the bounded diagnostic/locator errors;
existing exact-identity, failure classification and readback constraints cover them.
No lesson was retired. No further change to the experience file is necessary.
