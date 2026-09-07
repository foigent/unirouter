# Client migration diagnostics — development/test Target

PR409 exact merged main `07761d7371299a4e435ffd5f4b838e77672d6ad9`,
tree `8c7d16849ecc0822934beb704969b31dc2cb175b`, version1.2.71/M114.
CI34162672795 passed Change Routing, Backend, Frontend, PR4 Browser and Docker.

Official SendFile delivered the 13,963-byte exact source delta;
SHA256 `5116bee97a92b3e5b3f386c1233d46520e5f9fb1a2439d6997c24057d59adb01`.
Formal build and rollout succeeded under operation `formal-roll71-07761d73`,
plan digest `7d21900d2674c4a14c92750f7a9252c76840a8c611f2e03e363ee5a2ec547531`.
Target is the existing development/test ECS `i-0jl0x8x34xgaake6li28`.
Post-read `t-wl06wdy98a5m6m8` confirms both backends healthy, schema M114,
39 Releases/17 Slots/13 Publications unchanged, Flash59/Max60 on Slot3 unchanged.
Backend `sha256:b6b76ba7600466655f4de1b4ca1b77dc01a7f6ac012f038ffdda6f90f430585b`;
frontend `sha256:1fb7669eb851794ffa1d86ac6b044456a78cacc0e310d725c09eac1c22804827`.
Rollout evidence covers short requests; previous exact69 continuity evidence
retains its separate qualified-client and loaded-stream boundaries.

## Real request diagnostics

Each Flash/Max actual OpenClaw SDK first request and tool-history continuation
passes no-send preflight with accepted8192/provider8182 and exact protocolv8.
Each legacy-only and same-value dual-field request is locally inadmissible and
returns `parameter_errors`: remove `max_tokens`, use `max_completion_tokens`.
An ordinary minimal Flash request with both fields64 returns HTTP400 and retains
`validation_error.model_execution_profile`, now with the actionable message.
The ordinary rejection Request ID is `op_63f47a3797a8b09dc607b982d1e4445b`.
This implements the selected migration contract; it does not claim aliases are
technically impossible or change existing admission.

Official read `t-wl06wdys92qi0ow` checks all nine Request IDs: zero new Provider
attempts, Freeze records, billing observations and usage finalization records.
Total Provider attempts remain123. Budgets are unchanged. No paid Agent rerun,
new Release, pricing change or Production result is claimed.

Private raw evidence: `/private/tmp/unirouter71-client-acceptance-20260908` and
`diagnostic71-read-result.json` in the existing delivery evidence directory.
The prior real Flash/Max v8 OpenClaw and cancellation results are in
[the completion-contract evidence](QWEN-COMPLETION-CONTRACT-20260908.md).
经验文件无需变化.
