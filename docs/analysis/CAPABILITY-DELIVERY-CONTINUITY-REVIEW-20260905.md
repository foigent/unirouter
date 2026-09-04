# Pre-code review adjudication

Reviewed object: `CAPABILITY-DELIVERY-CONTINUITY-DESIGN-20260905.md`, SHA256
`fe0e751872b62e9b687d56432a77325db26584e3f0486d4241d4219fcba17b2c`.
Independent reviewer: `/root/precode_review`. Original bytes remain unchanged.
No production code existed when review completed. This record is the controlling
addendum, not a second design authority.

## User scope update

The user explicitly authorized the temporary Aliyun official Go mirror for
this development/test project. Checksum validation remains mandatory. The user
deferred Bailian/Qwen and Hermes: current acceptance is the existing DeepSeek,
Kimi, MiniMax and GLM series under the new architecture plus the OpenClaw goal.
Data-only delivery is demonstrated with exact new data revisions of existing
series without a backend rebuild. A new-provider/model demonstration remains
future evidence and is not claimed here. Every exact submodel retains its
cumulative maximum of 30 upstream calls and CNY10, including previous runs.

## Findings and decisions

1. `validated_required`, accepted: Nginx currently mounts one file. Install a
   directory-mounted config carrier at an explicit bootstrap boundary; verify
   container-visible config digest before every steady-state reload. Converting
   the old mount can require proxy recreation and is not continuity proof.
   Process all upstream references, including health/metrics.
2. `validated_required`, accepted: slow request uploads have no proven ingress
   total-lifetime bound. Do not forcibly expire old Nginx workers to manufacture
   withdrawal. Keep worker shutdown timeout unlimited in the supported carrier;
   the controller has a finite wait and aborts rollout while retaining the old
   backend if workers remain. Cover delayed request-body forwarding. Direct
   debug backend ports are excluded from ingress continuity and must be fenced
   before later fleet-activation claims; readiness does not fence those ports.
3. `validated_required`, accepted for C/E/G: qualification binding resolution
   and application attachment must consume exact DB definitions in the new
   epoch; compiled registry validates implementation support only. Startup in
   that epoch validates the capability ledger/projection, not embedded M113
   equality. Same-image Probe and fresh-process restart are explicit tests.
4. `validated_required`, accepted for D: the legacy qualification SQL view itself
   joins mutable Profile/selector authority. New Release qualification must
   verify immutable original Approval/Probe/binding plus pinned decision version
   and all original ownership/identity constraints, with separate current safety
   checks. Legacy view semantics remain unchanged. Pair selector-drift survival
   with real-disable rejection at dispatch.
5. `rejected`, accepted: B need not wait for C-F implementation details or the
   previously pending mirror decision. No further product approval or repeated
   pre-code review is necessary for this reviewed scope.

## B implementation details fixed before code

- Existing HTTP/stream Provider contracts remain unchanged. The outer accepted
  handler lifetime is finite: the greater of configured stream maximum and the
  existing 1800-second typed reasoning ceiling, plus 120 seconds of total
  admission/finalization allowance. Existing shorter Provider limits still hold.
- Application drain is at least that lifetime plus 120 seconds for cancellation,
  durable finalization and dependency exit, and never below configured
  SHUTDOWN_TIMEOUT_SECONDS. Startup reports the effective values; Compose must
  have more stop grace than that effective deadline. Defaults use 35 minutes
  (2100 seconds), compared with default effective drain 2040 seconds.
- Middleware does not wrap or buffer ResponseWriter and preserves SSE/flush
  capabilities. Admission/count change is mutex-atomic; fencing does not cancel
  already accepted requests. Per-request deadline/cancel is propagated.
- HTTP shutdown timeout closes connections and cancels remaining request
  contexts, then allows the remaining bounded cleanup grace. A nonreturning
  handler is reported as an incomplete drain, not success. The normal signal
  path never uses log.Fatal to bypass cleanup.
- Settlement recovery/outbox get explicit done waits after HTTP completion;
  their durable leases support recovery on bounded cancellation. No global
  outbox-empty condition and no new financial recovery state machine.
- Controller steady-state operations are confined to an already installed
  supported directory carrier and exact existing Compose identity. Bootstrap
  is separate; arbitrary replacement of user TLS/configuration is forbidden.

Baseline evidence: config and streampolicy tests passed using existing local
dependencies. Isolated PostgreSQL16 is running on a task-only Unix socket. No
Provider request or Target mutation has occurred.
