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

## C-F implementation refinements within the reviewed object

- Candidate Deployment identity required one additional immutable discriminator:
  `profile_definition_digest`. The old unique Connection/model/upstream tuple
  would force an operation revision update and invalidate an existing sealed
  Release. New revisions use separate Deployments with the same Connection,
  stable product and Credential; old identities cannot be retrofitted. This is
  not a second model or execution revision entity.
- Capability derivation receives the actual immutable Definition with the Profile.
  Reconstructing a temporary plan from only an ID/version would send new data
  revisions back through compiled Lookup. The same value travels into response
  mechanics; existing exact Protocol references select normalizers, while old
  compiled tuple behavior stays addressable.
- Runtime joins, Release/Slot changes and Publication first acquire the existing
  release-before-slot table locks, then the compatibility advisory lock. A utility
  lock before the first business read avoids an RR snapshot taken while waiting
  on an earlier join. Schema/static owner locks remain outside that boundary.
- Unchanged runtime heartbeats renew only the exact immutable identity, manifest,
  state and schema under a bounded lease; they do not acquire the global execution
  lock or grant ingress membership. This prevents static projection work from
  starving healthy instances. Registration/state changes retain the domain lock.
  Transient renewal failure cannot extend the last confirmed deadline; explicit
  incompatibility latches admission closed until a successful revalidation.
- Bundle trust uses the existing authenticated deployment operator and SSH
  channel: official private-repository successful main CI run, archive digest,
  exact source commit/tree/blob and provenance are verified on the operator host.
  Target receives bounded bytes and the approved identity. No GitHub credential
  is copied, no admin HTTP upload exists, and the same materializer writes the
  ledger. This does not claim a remotely signed Bundle management platform.
- M114 expansion uses the installed host ingress lock and exact Docker/support
  observations, excludes old shutting-down proxy workers, and repeats live
  instance checks inside the existing schema owner. It supports only the full
  known M113/M114 ledgers. Older Targets must reach their actual supported legacy
  predecessor through the established owner; no historical receipt is invented.
- ProviderAttempt adds only the server-owned runtime instance reference. Its
  immutable generation identifies the Artifact. Existing Freeze/pricing_version
  remains the customer price authority; no new PricingSnapshot owner is added.
- Publication owns static definitions/bindings/selectors only. Dynamic candidate
  Deployment writes remain separate exact authoring transactions, so publication
  cannot revise an active operation or reinterpret an existing Release. Legacy
  compiled selector addresses are frozen; data revisions require new addresses.
- The exact code diff and regression results must receive the one closing review
  before this source candidate is represented as merge-ready. Current local
  results do not establish Target/Provider/OpenClaw closure or old-writer retirement.

## C-F closing review and adjudicated fixes

The same independent reviewer completed the one closing read-only review of
tree `bc9acbca8b74578a2980081a2e0bec529cb7264f` against base
`1b11a4ec831d228ce0f13587cdcdb087f8345b93`; its 134-path binary diff SHA256 is
`f19c0c19f502945bbdcced8872be92f55c994736a42362b0eab56eed861dc030`.
No additional product authority was requested or granted.

- `validated_blocker`, accepted and fixed: an expanded schema could become
  visible before the next process refresh registered its generation. The schema
  owner's mandatory transactional After hook now installs verified bootstrap
  leases before the DDL commit. It consumes the host-observed canonical support
  manifests; ingress membership is still absent until the separate controller
  import. A failed bootstrap rolls back the whole expansion.
- `validated_blocker`, accepted and fixed: recomputing runtime identity on each
  transaction retry could invalidate an M113 committed attempt after M114
  arrived. Each ephemeral Authorize owner now fixes its runtime field at the
  first successful schema read and preserves it during same-owner recovery.
  Strict existing receipt comparison remains; historical rows are not changed.
- `rejected`: the pre-code dynamic qualification/compiled startup gap persists.
  The reviewer verified DB binding resolution and capability-head startup.
- `rejected`: the pre-code current-selector reinterpretation gap persists.
  The reviewer verified raw immutable pins in planning/dispatch and candidate
  Deployment isolation. Existing current safety checks remain authoritative.

Main-agent verification of the fixes passed actual PostgreSQL race regressions:
`TestCapabilityExpandPublishesVerifiedLeasesAtomically`, the failed-bootstrap
rollback branch of `TestCapabilityExpandRequiresExplicitBridgeAndPreservesLegacy`,
and `TestAuthorizeOwnerSurvivesCommitAmbiguityAcrossCompatibleExpandIntegration`.
The latter commits an M113 attempt, applies real M114 before returning the lost
ACK, recovers the same owner without changing its row, then authorizes the next
request immediately without calling Renew. A second owner remains rejected.
The existing first-customer corruption fixture now disables both immutable
binding triggers only inside its rolled-back isolated transaction; the actual
writer protections are unchanged. Its targeted regression passed.

Final affected adapter/Gateway/routing/qualification/execution/Profile/schema/
tokenizer/billing/Release package race suites passed. The complete admin
integration lane found only that deliberate-injection fixture issue, whose
targeted repair passed. These are Local facts, not CI or external proof.
