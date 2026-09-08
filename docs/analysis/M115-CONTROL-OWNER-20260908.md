# M115 control-owner compatibility repair

Status: implementation and sole independent closing review complete;
required CI, real Docker lifecycle and Target acceptance pending.
Base: main 4a273b829f122f0da6b620adfdeb38ddd6de2d49 (1.2.73).

## Observed failure and scope

The development Target is healthy on the exact 1.2.73 Artifact and M115. The
formal M115 owner succeeded once, with verified backup and identical before/after
digests for 13 historical/control/financial table projections. Both HTTP actors
renew on M115, still writing counter v2. No new paid call was made.

The next same-image writer plan fails because runtime_generation_cli.go accepts
only CapabilityExpandHead after already validating the complete compatible
ledger. Thus inspect, read, prepare and complete all reject M115. The old running
CLI cannot coordinate its own replacement. Repeating the command cannot help.
The source-cache refusal preceding this was separately identified: compatible-
expand imports runtime-ingress and creates a pyc file. That exact task-generated
cache was retained in the M115 evidence directory; source is clean again.

Authorized scope remains development/test, existing official build/transfer and
formal rolling owner. No raw database repair, schema down, lost history, admin
change, provider retry, new credential, public port, or new infrastructure.

## Proposed minimum repair

1. Runtime-generation CLI accepts exactly the validated M114 or M115 ledger;
   M113, unknown future heads, gaps and divergent migration bytes still reject.
   Reuse the existing compatible-ledger authority, not a numeric ceiling.
2. A narrowly selectable Artifact CLI carrier in the existing formal rollout
   runs the SAME runtime-generation command and domain writer from the exact
   candidate backend image. It is not a second implementation or database writer.
   The exact source, image digest, complete migration bytes, target, effective
   Compose chain and normal predecessor checks remain required.
3. Proposed carrier mechanics: existing Docker Compose one-off run, --no-deps,
   --pull never, -T, --rm, no service ports or aliases; command is fixed to
   ./uniroute runtime-generation and one of inspect/read/prepare/complete.
   The existing backend service configuration supplies the already authorized
   database connection. Only the four exact runtime Artifact metadata values may
   be projected to the candidate values. No secret command arguments or output.
   The command exits without starting HTTP, workers, recovery, or registration.
   The ordinary runtime-ingress module calls the selected executor for ALL four
   commands; all CAS, locks, typed ingress receipts and ambiguity recovery remain.
4. Carrier selection must be explicit in the immutable rollout input and bound
   into its Plan digest. The default deployed-container executor is unchanged.
   No arbitrary image, shell, command, DSN, container or SQL input is accepted.
   The candidate carrier is available before either backend is stopped, solving
   the old-CLI bootstrap problem. It remains available through exact resume.
5. Plan/inspect through this carrier have no database, ingress or serving-process
   mutations, but start and remove a bounded one-off CLI container. Documentation
   must state that operational side effect; do not call it zero Docker mutations.
   Alternative requiring review: pre-stage one restricted candidate CLI container
   in a separate prepare operation, keeping plan literally container-read-only.
   Prefer the one-off form unless this violates a real accepted contract; avoid a
   permanent operator service or a new orchestration lifecycle for this repair.
6. Disable import bytecode in compatible-expand itself, matching the existing
   rollout tool's no-cache behavior. Preserve the original refused attempts.

## Validation and delivery

- Real DB CLI boundary tests: successful inspect/read and a typed prepare/complete
  on M114 and M115; invalid ledger/head rejects before any ingress mutation.
- Test the caller boundary, not only domain functions: the missed CLI guard is
  the regression. Existing domain CAS/compatibility tests remain applicable.
- Carrier tests: fixed command/selected exact image/config metadata; no ports,
  aliases, dependencies or Provider/worker process; image/target/ledger drift and
  unsupported input reject; safe errors; caller locks preserved; ambiguous command
  never resent without retained receipt inspection. Verify plan data neutrality.
- A Docker-backed local one-off CLI smoke must prove effective config/entrypoint
  and lifetime behavior, plus a formal 73/M115 predecessor to corrected candidate
  plan. Fake subprocess tests alone are insufficient for the bootstrap path.
- One 1.2.74 patch, required review/checks/CI/exact-main Artifact. Formal rollout
  73/M115/v2 to 74/M115/v2 using the exact candidate CLI carrier, then same-image
  74 v2-to-v3 through the ordinary formal transition. Model Releases unchanged.
- Then the one bounded Flash cancellation and concentrated shared-Probe UI. Do
  not claim the counter batch remained on one backend Artifact: this platform
  correction requires an additional build/rollout. Provider/usage/quality proof
  stays separate; unchanged previous normal OpenClaw evidence is reused.

## Review request

Independently validate the root cause and the least sufficient safe bootstrap.
Specifically assess whether the one-off carrier weakens a real plan/no-side-effect
contract, introduces competing writers, leaks connection configuration, or leaves
unbounded processes on timeout. Prefer a smaller existing supported path if found.
No implementation, Target writes, paid calls or new product scope by the reviewer.

## Independent pre-code ruling and adopted lifecycle

Reviewer `/root/stream_checkpoint_precode_review` independently validated the
M114-only guard as a blocker and the same-CLI carrier as required. No new product
decision or parallel writer is needed. The reviewed object was SHA256
`b0e2984f0f10df977a8031edb7c43fdb07b7d3db6849ce3f655ec8ab42625183`
plus the lifecycle clarification below. This section records that clarification;
it does not claim implementation evidence.

- Carrier mode takes the existing lifecycle then ingress locks even for
  plan/inspect. Default plan/inspect retain their existing read-only behavior.
- Use the five real configuration files, including the stage-validated installed
  overlay. Override only the four non-secret Artifact identity environment keys.
  The CLI's typed JSON has exclusive stdin; no YAML is sent on that stream.
- Each command starts a named one-off container with fixed `sleep 90`, --rm,
  --no-deps, --pull never, no ports or service aliases. A known exact ID, image,
  effective configuration and remaining lifetime must be verified BEFORE any
  database command. Existing name or unknown create outcome refuses takeover.
- Execute only `./uniroute runtime-generation inspect|read|prepare|complete` in
  that exact ID. The Go owner has a 20-second context deadline; the host call is
  bounded at 30 seconds and may start only with enough remaining container TTL.
- In finally, stop only that verified helper ID with explicit five-second grace,
  never the backend's 35-minute setting. Verify stopped/absent; --rm handles this
  task-created container. Its fixed lifetime also bounds a lost host controller.
  Unknown command result remains requires_inspection; do not infer no commit or
  resend. Existing typed ingress receipts/CAS are still the recovery authority.
- Prove real M114/M115 and negative CLI paths, caller locking, zero paid/HTTP/
  worker startup, safe errors, and actual Docker lifecycle before Target rollout.
- One-off CLI has an existing runbook precedent; it does not authorize using the
  old maintenance migration stop-both sequence for this repair.

## Closing review and Local evidence

The same independent reviewer inspected exact patch SHA256
`691b423e7b78cc260f6cc6cfdaabd3c9757675530092638a918ebe7ddbfd2b6c`
(14 paths) and found no remaining code blocker or new product decision. The
pre-code requirements above are implemented. Required real Docker lifecycle
verification runs in the existing Docker CI job before Target deployment; the
Mac has no Docker daemon. Its synthetic read fixture is not an installation
or complete migration claim.

Independent Local reruns pass: rollout 58 tests, ingress 13, metadata 12, and
the new migrated CLI integration/race tests (7.675s, no skip). Author validation
also passes the existing consolidated CLI selector plus the new cases, affected
runtimegeneration/schemaauthority race suites, vet, and release metadata checks.
An exploratory full tagged cmd suite finds two old PRD2 503 failures; the
unchanged exact 1.2.73 base reproduces both. They remain deferred and no existing
required CI selector was removed. No Target74, counter-v3 writer, or paid-call
success is claimed by these Local results. 经验文件无需变化.
