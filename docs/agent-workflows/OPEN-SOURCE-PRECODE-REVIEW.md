# Open-source preparation: independent pre-code review

Object: local proposal against main `59c47b852b7372951c5c49b0e6b23df8c83817b9`, tree `35dea6fce43ff8391d7bb8ef4a9a500efbf329bf`. Reviewer: `/root/opensource_precode_review`, read-only. Scope: retire runtime commercial License enforcement; preserve permissions, financial facts, qualification, history and upgrade signature trust. No publishing or deployment authorization.

Accepted validated_required findings:
- M126 is a maintenance-only schema boundary, not an online compatible expansion. New runtime accepts only the full M126 ledger. Formal migrate-up validates the exact M125 predecessor under the migration owner lock. Fresh install applies M126 through its existing owner and completion verifier. Ordinary startup cannot apply M126.
- Remove five commercial-limit triggers, retain historical settings, entitlements, audits and migrations. Preserve the original M118 budget-coverage proof through M126.
- Remove runtime License construction, refresh, reconciliation and feature gates; update OIDC, /me, readiness, descriptors and frontend. Real authorization, billing, qualification and release validation remain.
- Retain deprecated /api/v1/license shapes: status not_required, no valid/signature/enforceable claim. Import is bounded strict JSON, platform-admin-only, explicit imported=false no-op, with no persistence or license events.
- Ignore retired LICENSE_* configuration. Release upgrade trust remains embedded, mandatory and protected from runtime overrides.
- Verify migration refusal, complete ledgers, inherited writer requirements, fresh install, permissions, UI and signing boundaries.

Validated blocker for public contribution readiness: CI depends on private integration-control N1 and event-connector-spec repositories. They cannot be published, copied, bypassed or exposed to Fork PRs under this authorization. Report separately from local test evidence.

Recovery: stopped/drained old writers and retained pre-M126 backup plus old Artifact are prerequisites. Database lease expiry alone is not process-exit proof. Existing retirement commands retain their environment scope. No generic down owner or production procedure is invented.

## Closing review and blocker repair

One closing read-only review covered the patch retained at
`/private/tmp/unirouter-oss-closing-review-20261010.patch`,
SHA-256 `8ec028d33943bfeb364228e4dbcf7d11aaac8fce71e5cacbb506fe83d1607094`.
This identifies the reviewed snapshot, not the later repaired worktree.

Validated blockers: the old ingress/retirement controllers cannot withdraw
their final receiver; Source Facts and counter-v3 head checks omit M126.
Validated required: replace stale commercial-gate route fixtures, exercise
used-fleet migration/refusal, fresh/resume, retained resource history, restored
budget proof and inherited manifests. Upgrade trust and real production
authority wrappers were confirmed preserved by static review, not by Target tests.

A follow-up pre-code review of the blocker repair accepted explicit empty
membership arrays (never omitted/null), exact-M125 maintenance CLI read/inspect
recovery plus withdrawal-only prepare/complete and original stop-check/apply.
Ordinary runtime admission remains M126-only. Host control must retain exact
Nginx config/worker proof, an independent short-lived Artifact CLI carrier,
actual process/container exit, no unaccounted writers and financial finality.
Scope stays development/test. Lease expiry is not exit proof.

At that checkpoint, automatic approval review rejected a rollout helper edit,
citing service-control scope beyond the temporary PostgreSQL/Redis/npm
authorization. The subsequent explicit approval below resolves local code and
offline tests only. No Target command was run.
Current evidence and exact remaining repairs are in EXECUTION-RECORD.md.

## Authorization and local verification update — 2026-10-10

The user explicitly approved the maintenance-controller code and offline tests
only, resolving the earlier automatic-review rejection within that scope.
Canonical empty membership and the exact M125 predecessor identity are corrected.
Offline ingress/retirement/rollout tests passed (15/9/81), including clean exit,
independent carrier, no-write check and unknown-result refusal/readback. No actual
Docker/Target controller was invoked. The separately approved shared-memory
cleanup was restricted to ID1900545 after a fresh zero-attachment/absent-creator
check. Task-local PG16 acceptance subsequently passed current admission,
retirement/refusal, retained history, fresh/resume and real dump/restore/rebase.
Historical rollback fixtures explicitly install M125; current binary ledger
validation and the unsupported M126 down guard remain unchanged. Final formal
regression and service lifecycle evidence are tracked in EXECUTION-RECORD.md.
This update is the executor's evidence readback, not another independent review.
