# Delivery compatibility and onboarding closeout

Pre-code review object. Base main44a6a158f28f05fb447b183bc529ea7499c02c56,
tree3c6473b98651978ea1609dcfb741ccc3f5a541ad. Runtime remains1.2.76/M115.
The user authorized this bounded long task on2026-09-09, with X-Insight
integration and a later complete test campaign explicitly separate.

## Outcome and acceptance

1. Reject supported-host tool incompatibility before expensive image builds.
   Reuse the formal build/rollout tools; retain exact-image carrier tests afterward.
2. Reuse saved connection/Key, select models and expose exact next actions.
   Show latest observation separately from approved evidence and active Release.
3. Validate an explicit per-capability Approval in the real UI using existing
   successful Probe259–264; no new paid Probe is needed. Verify refresh and client
   configuration without exporting credentials or changing client defaults.
4. Move cheap checks before expensive CI work, retaining current required checks
   and conservative classification. Test add/delete/rename and failure routing.
5. Complete Local, one independent pre-code review, at most one closing review,
   required CI/exact-main checks, exact Artifact, development Target rollout and
   representative GUI. Freeze the resulting baseline for the next user task.

No schema, model/Provider expansion, parser, pricing, metering, Release pinning,
fallback, Production, X-Insight integration, new credential or old-writer retirement.
Keep current paid-call histories and the unused shared additional30/CNY5 pool.
Coalesce stable edits into one planned1.2.77 delivery; failures remain explicit,
and a promised build count must not conceal necessary corrective artifacts.

## A. Read-only environment check in the existing rollout owner

Add `environment-check` to `scripts/internal-target-rollout.py`, without `--input`.
Other actions retain their required exact input, locks and current behavior.
The check reuses `run`, duplicate-rejecting JSON, exact missing-object `inspect`
and `docker_started_at`. It never creates a container, runs a DB command, pulls
an image, acquires a persistent operation lease, or writes ingress/configuration.

- Require Python at least3.10; report its actual version. Execute the existing
  finite timestamp parser against valid nanosecond and invalid-offset samples.
- Read Docker client/server versions through structured output and report them;
  this delivery's supported Docker major set is28/29. Unknown majors fail closed
  pending explicit support, not inferred from a matching error string.
- Check the local daemon and Compose plugin. Use the fixed local-daemon ENV
  already owned by the rollout tool; inherited alternate Docker endpoints cannot
  redirect this check.
- Generate an unpredictable, bounded `ur-environment-check-<hex>` name and use
  exact `inspect(..., missing_ok=True)`. Unexpected presence/error rejects; do
  not delete an object whose name happens to match.
- Read IDs of running containers in Compose project `unirouter`, inspect at
  most one exact ID and validate its StartedAt with the same parser. If there
  is no such container (fresh install/CI), report timestamp_not_observed and use
  the parser samples; this is not a real carrier-lifetime proof. A listed object
  disappearing during inspection rejects safely; no automatic retry is required.
- Return only safe versions, selected ID/timestamp and success/proof scope.
  Never emit full inspect output, environment variables, DSNs or credentials.

The existing `internal-target-build-images.sh` invokes this command after exact
source validation and lifecycle lock, before image selection/build. It must not
allow a caller-supplied bypass. Both normal and frontend-only paths are checked;
frontend-only still makes no backend image selection or runtime compatibility
claim. Preserve immutable tag, storage and provenance gates.

This check tests installed host behavior, not the as-yet-unbuilt candidate CLI.
`test-artifact-owner-docker.py` remains the post-build real carrier/response-loss
cleanup test. No second preflight receipt registry or transport framework.

## B. Cheap CI checks without weakening required coverage

Keep current Change Routing classifications and all required check names.
For changes with docker=true, Change Routing uses existing official Python3.10
setup and runs the rollout/ingress/build-wrapper unit suites plus the read-only
environment check before downstream jobs. Its five-minute budget is unchanged.

Expose whether this particular early setup/check failed through step outcomes.
Only that explicit failure suppresses expensive dependent jobs, while Change
Routing remains FAILED and blocks merge. Unknown/missing classification outputs
retain the existing conservative full-check behavior; success never substitutes
for Backend/Frontend/PR4/Docker coverage. No continue-on-error. The real candidate
carrier test stays in Docker after image build. Pure data Backend splitting is
not presumed safe by this change.

Extend the existing actual-workflow routing tests for data bundles, OpenAPI,
frontend, deployment config, unknown files, deletes and cross-surface renames,
plus explicit-preflight-failure versus unknown-failure dispatch. Official context
reference: https://docs.github.com/en/actions/reference/workflows-and-actions/contexts

## C. Onboarding presentation and explicit Approval

Use existing fields `latest_probe`, `preferred_approval_id`,
`preferred_capability_probe_id`, `qualified` and current runtime membership.
Do not derive authority from row order or overwrite backend state.

- Separate columns for latest verification (ID, result/currentness, HTTP) and
  effective approval (Approval ID + its Probe ID, or unavailable/unapproved).
- Allow explicit selection of a successful current latest Probe when it is not
  the preferred approved Probe, including a model that already has older valid
  approval. Call the SAME per-capability approval API with that exact ID and a
  human-visible reason. No auto-approval or Release/Slot change. Already approved
  latest facts are not selectable; failed/stale/missing facts remain disabled.
- Bind a selection to both capability ID and Probe ID, so a refresh/replacement
  cannot silently approve a different observation. Clear invalid selections;
  before each command verify the frozen selection and actor/target generation.
  On an ambiguous/partial approval response stop and require refresh; do not
  dispatch remaining approvals as though the result were known.
- State that a newer observation does not update an active Release. Show a
  concise verification/approval next step in the workspace. The existing detail
  and Release workspace remain the inspection and activation owners.
- In batch selection, already configured models link to their exact saved
  deployments and status instead of being a disabled dead end. Preserve multiple
  deployments; never guess the active one from its name. Existing creation,
  idempotency, permission and actor fencing remain unchanged.
- Keep stable-name/client configuration output and dry-run OpenClaw sync. Align
  the provider-name form with the existing CLI's lowercase64-character grammar
  and show invalid input clearly; no new client SDK or credentials.

## Verification and recovery

Offline tests cover environment success/failure, no writes/build on failed
preflight, malformed versions/JSON/timestamps, exact absence vs daemon errors,
and workflow dispatch coverage. UI tests cover old approval/new Probe, a
failed/stale latest Probe alongside old approval, selection replacement, partial
approval, actor/target change, no automatic paid calls, existing-model continuation
and client export parity. Existing affected suites/build budgets remain required.

Before Target change, re-read exact instance, images, schema, storage and formal
Compose/ingress state. Run the new environment check before the formal build;
after exact Artifact verification use the existing rollout plan/backup/apply/
inspection owner. Preserve failed invocations, no blind replay or image overwrite.
Verify a single selected existing Flash capability Approval through the GUI,
unchanged other approval references and Release59/Slot3, refresh-no-send, saved
connection/batch continuation and safe client configuration. This UI-only/model
contract-preserving change does not require repeating all Agent/model tests.

## Accepted independent-review corrections

Review object above was SHA256
`96e121d74beae31a19f03bdedc446d62d604c4ffc332b81437da9120f3c68ce4`.
Independent reviewer Parfit identified three required corrections; the owner
accepted all before implementation:

- Build wrapper clears Docker context/TLS/API and remote builder selectors and
  fixes the same local socket as the environment check. Checking a different
  daemon from the one building images cannot satisfy preflight.
- Compose requires a stable version at least2.24.4, because the existing official
  overlay uses !override. Presence/version logging alone was insufficient.
- Latest Probe gains a read-only exact `approval_id` projection from the immutable
  Approval table. Explicit zero proves absence; a missing field from an older
  backend is unknown and cannot enable approval. Preferred order is not evidence
  of whether a different Probe already has an Approval. No schema/writer change.

CI outcome/failure and unchanged Release/writer boundaries were validated; no
new product decision. X-Insight and full integration campaign remain separate.
