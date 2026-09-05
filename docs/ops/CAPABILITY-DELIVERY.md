# Capability data delivery and compatible activation

This runbook covers the finite M113/M114 development/test bridge and the existing
single-node stable/canary topology. It adds no Production authorization. Current
source supports dynamic GLM streamed-tools and MiniMax M2.7 Probe implementations;
other existing-family runtime contracts remain readable, with qualification and
Agent coverage recorded separately. A source revision, a successful Probe and a
successful Agent answer are different evidence.

## One-time bridge and import

1. Read the actual Target hostname/instance, DB system identifier, full schema
   ledger, current image IDs, exact effective Compose chain, storage and backup
   state. Verify source commit/tree/version and immutable image identities. Keep
   official dependency/checksum policy; a temporary authorized Go mirror is not
   a persistent host or Docker configuration change.
2. Roll both bridge binaries on the existing M113 schema using
   `RUNTIME-CONTINUITY.md`. Set each backend's `RUNTIME_IMAGE_DIGEST` to its actual
   Docker image ID. No startup or ordinary `migrate up` applies M114. Preserve TLS
   and restrict backend debug ports to loopback. Complete each old proxy worker's
   withdrawal before stopping its backend. A slow old worker aborts rollout while
   retaining that backend; it is never forcibly expired to manufacture success.
3. After the storage gate and verified backup, invoke `scripts/compatible-expand.py`
   on the Target with the installed membership directory, exact Nginx container
   ID, expected hostname, DB system identifier/environment, reviewed M114 file
   digest and the two predecessor config digests from ingress planning. Default
   is read-only; `--apply` repeats the same checks under the host ingress lock.
   The existing schema owner rechecks live stable/canary identities and supported
   ledgers under its migration lock. An old proxy generation blocks expansion.
   The host supplies each image's canonical support manifest. The schema owner
   installs these verified instances' initial bounded leases in the same DDL
   transaction; M114 cannot become visible before the matching runtime rows.
   Their ingress state remains absent until the separate explicit import.
   Only the exact additive M114 bytes can execute; no backfill is hidden here.
4. Wait for both readiness responses to report M114, the real image digest and
   a live lease. Import the installed membership with `runtime-ingress.py --apply`
   using the same desired state as the observed state and a new `--change-id`.
   Prepare commits all potential receivers before reload. Complete confirms old
   proxy exit; pending/ambiguous intent retains all receivers and blocks a newer
   ingress command. This command never stops a backend.
5. Create the legacy import through `capability-contract-publication plan-v2`
   then `apply-v2` on the existing authenticated Target operator channel. Input:

   ```json
   {"publication_id":"target-specific-import","kind":"legacy_import",
    "compensates_publication_id":"","bundle_bytes":null,
    "approved_bundle_digest":"","carrier":null,"plan_bytes":null,"plan_digest":""}
   ```

   Plan output contains base64 canonical `plan_bytes` and `plan_digest`; retain
   both, verify Target identity and current legacy head/projection, and include
   them in the identical apply input. Import records the actual supported legacy
   predecessor; it does not replay or manufacture M100-M113 receipts. This bridge
   requires an actual M113 predecessor, not an assumed migration-number maximum.

## Daily candidate publication

- Author a canonical `capability-bundles/<name>.json` using Bundle schema
  `unirouter.capability-bundle.v2`. It contains immutable definitions and exact
  binding/selector deltas only. Data definitions reference installed typed
  implementations, with explicit lineage and a new Profile revision. They cannot
  contain credentials, observations, approvals, Slot changes or customer rates.
- Backend CI validates all tracked Bundle sources. A data-only JSON change does
  not build backend/frontend images or increment product release metadata.
  Dispatch the `Capability Bundle` workflow on the exact reviewed main commit.
  The workflow packages canonical source bytes and commit/tree/run provenance;
  it performs no Target write or Provider call.
- On the existing operator host, `scripts/publish-capability-bundle.py` verifies
  the exact successful official private-repository main workflow, archive digest,
  source Git blob, commit/tree and provenance. Provide exact run/artifact IDs,
  source path/digest, Target/owner image, DB identity/environment, publication ID
  and a new Plan output path. Default creates a read-only Target Plan; `--apply`
  re-verifies the same source and consumes that exact saved Plan.
- No GitHub credential leaves the operator host. The authenticated deployment
  operator is the existing trust boundary, not an unrestricted administrator JSON
  upload. A bare claimed digest or a manually constructed carrier is not an
  approved delivery path. Canonical bytes remain in the append-only DB ledger
  even after the CI download artifact expires. Expiry does not erase past proof;
  a new publication needs a currently verifiable approved source.
- The one materializer holds schema/static and execution-compatibility locks,
  compares the exact predecessor, recalculates the full static DB projection,
  applies only the delta, verifies the resulting digest and commits one ledger
  entry. Heartbeats, Deployment state, Probe observations, approvals and usage
  are outside this digest. Candidate Deployment changes remain their own normal
  authoring transaction, not a side effect of static Publication.

## Qualification and activation

Use the existing model endpoint form to select the stable model and published
execution revision. It creates a separate candidate Deployment on the existing
Connection and credential without changing the active operation or model name.
Run the actual operation/capability Probes within the cumulative per-model budget;
record original observations and issue each necessary approval through its owner.
Release authoring selects the exact definition, explicit limits, deployments and
approval IDs. It seals immutable references/digests, not copies of raw evidence.

Existing lifecycle actions control Slot activation/canary/rollback. Every possible
receiver, including pending joins and withdrawing old proxy generations, must
support the Release requirements. A joining generation checks currently active
Releases under the same domain lock. Digests identify support objects; set
inclusion establishes compatibility. Rejoining an incompatible old image fails.
Publication itself never activates a Release or changes existing Release semantics.

A request retains one definition through preflight, normalization, planner,
dispatch and response mechanics. Coordinator authorization commit freezes the
attempt identity before Provider I/O. Current permission, budget, credential,
connection and emergency safety state can reject execution without selecting a
new contract. ProviderAttempt identifies the runtime instance/Artifact on M114;
existing billing Freeze fixes the independent pricing identity. An Agent's later
HTTP round may select a newer Release: this is per-request consistency.

## Recovery and rollback

- Unknown Publication commit: use `recover-v2` with the exact retained Plan on a
  fresh read-only connection. It returns committed/already-committed evidence or
  remains unconfirmed; it never automatically repeats a write or Provider call.
  A new Plan cannot overwrite the previous publication ID. Correct committed
  static state with a new compensating Publication linked to the prior receipt.
- Unknown ingress reload/complete: keep all backends. Read the exact typed intent,
  then resume the same change ID and retained bytes. Recovery reload observes all
  remaining old workers before marking removed members absent. Do not infer
  withdrawal from Compose health, config-on-disk alone or a missing heartbeat.
- Slot rollback changes only subsequent, not already-authorized requests.
  Binary rollback requires the full current schema and active Release support.
  A 1.2.52 binary cannot serve M114. M114 down refuses retained definitions,
  Publications, generation/attempt identities or new Release evidence; it is not
  a history-erasing operational rollback procedure.
- One dispatch owner's runtime identity remains fixed through transaction
  ambiguity. A request authorized under M113 keeps its historical empty runtime
  field if M114 commits before its same-owner recovery; it is never retrofitted
  or treated as authority for a second Provider dispatch.
- Lease renewal failure cannot extend the last confirmed bounded lease; expiry or
  explicit incompatibility fences new admission. Unchanged heartbeats do not wait
  for the Publication/activation lock and cannot grant ingress membership.
  Accepted handlers finish or follow
  the declared request/drain cancellation deadline, with terminal dependencies
  retained until their local responsibility closes or is durably transferred.
  Do not wait for a global outbox to become empty, mask uncertainty with retries,
  or call an interrupted deadline-exceeded stream an uninterrupted rollout.

## Acceptance evidence

Separate Local, CI, Artifact, Target, Provider and Agent results. Required product
proof includes unchanged image IDs across two real data batches, dynamic Probe
and fresh-process startup, old Release invariance, incompatible join/activation
rejection, long SSE plus short-request rollout, one Provider dispatch/terminal
settlement per accepted attempt, and actual OpenClaw tool loops. Track answer
quality independently from protocol delivery. Existing exact models retain their
historical cumulative limit of 30 upstream requests and CNY10 each. Until those
proofs exist, do not claim full architecture/14-model closure or retire historical
compatibility readers.
