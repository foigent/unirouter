# Existing-model delivery and OpenClaw evidence — 2026-09-07

## Scope and outcome

All 14 configured exact models completed real UniRouter → OpenClaw streamed
function-tool loops. The matrix records immutable request identities, execution
references, historical report hashes, client timings and cumulative budget
upper bounds. Protocol completion is 14/14; answer quality is 2 passed, 9 partial,
3 failed. No answer-quality failure was repaired by weakening Gateway semantics.
GLM-5V-Turbo coverage is text only. M2.7 used its retained bounded task; the direct
samples and Gateway runs do not constitute a controlled performance comparison.

Current development/test source is merge `00e30b8a521f5b6edfcd870174c2080c66d80179`,
tree `a03a684f294fa3985f2b0d2e88cab9a8b62e8362`, 1.2.62/M114. Backend image is
`sha256:5cf6fce011e55556e6e9d6d0aa080c0ac35b6b79dcc91153092a3306f868fab2`, frontend
image `sha256:99b806f2ee8fadf7ff9f0b60381992f79b7ccd094d67250687de06c834b4c086`.
Earlier successful model runs remain bound to their earlier images. The final
read-only Target invocation is `t-wl06wa77v2owb28` on the existing development ECS.

## Final Highspeed path

The independent CI Bundle `03e6aceb7a46fcf53d3b7448a3eee8b0ce418443f0fff80dc1bed3e436ba342b`
was published as `development-minimax-highspeed-cache-20260906-00e30b8a`, without
changing the running images or M114 and without changing old Release/Slot rows.
This corrective batch did require prerequisite typed Probe/platform support in
1.2.62; it is not proof that the entire corrective batch required no rebuild.

Deployment39 uses `minimax.m2.7-highspeed.payg@1.5.0`, definition digest
`72843e1d56264bf28eb23ce996e445b933e3950c273cac37248a2943992dcbb3`, Connection13 and
credential binding13. Operation Probe37 and capability Probes228–234 succeeded;
Approvals205–211 were individually recorded. Authoring receipt37 sealed Release56
with snapshot `46b121b94174cdb64a8bbb5f0c3f4c7a4944cf012be2dea23930879565860f86`.
Lifecycle receipts52/53 changed Slot5 → 6 → 7, retaining Release54 as history.
No dated PublicModel was created for this revision.

Actual SDK preflight `op_a24313f520e21efec43467721263d2d9` accepted the real request
with zero Provider sends. The final Agent task used a bounded 32768-token output
override, two HTTP200 model turns, three successful `read` executions, tool result
history and final `stop`; no fallback. Requests
`op_ff4069dba0ecd12fba8386eb72597138` and
`op_fa84b4e192cfc3f6a80a3a4191050cab` each have one ProviderAttempt, Release56,
Deployment39, Slot7, completed delivery and exactly one final provider-reported
settlement observation (264/265). Total usage is 26031 tokens; client time is
47.429 seconds. The answer correctly identifies the metadata gate but incorrectly
claims the two documentation paths hit the CI catch-all instead of `docs/*`.
Its final recommendation meets the 220-word bound; the longer full answer is
reported separately from that requirement. Verdict: partial answer quality.

The user added at most 20 calls and CNY5 for Highspeed. The temporary guard bound
that authorization to the prior 23/CNY6.43896 state, yielding stricter cumulative
limits 43/CNY11.43896. It preserves the complete historical refinement prefix,
unknown usage reservations and counts. Eight qualification requests plus two
Agent requests used 10 added calls and CNY0.91420 exposure. Final cumulative
upper bounds are 33 calls/CNY7.35316. Other models retain 30/CNY10. Supplier fees
are conservative exposure bounds, not invoices; zero client-estimated cost or
zero customer charge is not evidence of zero supplier cost.

## Continuity evidence and remaining boundary

The earlier GLM-5.3 long-SSE rollout and explicit cancellation evidence remain
bound to `t-wl06w72go1yg740` and `t-wl06w72qzjebsao`. They recorded one Provider
attempt and settlement per request, with cancellation classified separately.

Roll62 invocation `t-wl06w9gw5thd9fk` still has a **failed** continuity assertion:
35 short samples succeeded and one HTTP2 sample returned HTTP000/curl16. All
backend/frontend replacements later read back healthy, but the observer did not
retain that failed sample's stderr. Correlation with old Nginx worker shutdown
is a diagnostic lead, not a confirmed cause. No automatic request retry, TLS
weakening or HTTP2 disablement was introduced.

A separate controlled test, `t-wl06wa67obkbif4`, held an accepted HTTP2 public
asset request after a 1024-byte receive-window SETTINGS acknowledgment. During
one formal same-membership graceful reload, it observed NO_ERROR GOAWAY with
lastStreamID1, received all 301858 bytes with baseline SHA
`2d3d0a8bb0a4e03794aff7231eb7571a99c4f63fd22b43327d4f595f884225ce`, and saw a clean
close without native session errors. An idle session received lastStreamID0
GOAWAY; a distinct fresh session returned the expected unauthenticated401.
All 120 concurrent short probes succeeded, old proxy workers exited, and images,
Compose chains, schema, Release/Slot and the nine-entry Publication ledger stayed
unchanged. No Provider call or backend restart occurred in this diagnostic.
Its five local cases cover success, bad final GOAWAY, early completion, baseline
deadline cancellation and native session errors.

This proves bounded accepted HTTP2 handoff alongside the earlier real SSE
execution evidence. It does not erase the failed rollout, prove every arrival
race harmless, or justify claiming the entire B/F continuity program closed.

## Preserved boundaries

- Independent Bundle publication, materialization and existing data revisions
  have Target evidence. DeepSeek Flash/Pro retained same-image end-to-end data
  revision evidence; platform repairs and new typed wire support remain Artifact
  changes. No claim of no rebuild for every batch or a brand-new product model.
- Default consistency is per request. The two Highspeed rounds used the same
  Release in this run, without implying a general Agent-session affinity contract.
- Original public model identities, M100–M113, receipts, rollback history and
  the supported legacy writer code are retained under the existing writer epoch.
  No H retirement. Qwen and Hermes are deferred by explicit user scope.
- Backend/frontend Compose configuration uses the formal five-file chain. The
  retained Nginx historical chain has not been recreated as a cleanup exercise.
- OpenClaw's official default and existing Provider credentials remain intact;
  its configured UniRouter models use ordinary application access and metadata.
- No Production, native media/Computer Use, Token Plan, Pricing lifecycle, new
  infrastructure or administrative account/session change.

## Learning closeout

经验文件无需变化. Existing MAR-003/MAR-007 cover the exact Probe/runtime contract
and cross-delivery enforcement. No new lesson is admitted for the unresolved
curl16 incident: the original diagnostic data is absent, so a reusable cause or
product correction has not been established. The temporary budget extension and
controlled HTTP2 harness do not create a permanent model-adaptation policy.
