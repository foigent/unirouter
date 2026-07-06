# ADR 0002: PRD4 API Key Owner Usage Attribution

> Status: Proposed
> Date: 2026-07-07
> Owners: User, Codex
> Related: `docs/PRD-Phase4.md`, `docs/ops/PRD4-ACCEPTANCE-MATRIX.md`

## Context

The User wants each user to see their own usage and cost, including the amount
spent on different models. Current UniRouter analytics already aggregate by
project, key, model, alias, provider, channel, status, and usage source, but the
current request logs are not a reliable per-login-user fact source unless usage is
attributed through API key ownership or an explicit end-user field.

Shared API keys make exact human attribution impossible unless UniRouter captures
a trusted end-user identity on every request.

## Decision

PRD4 defines "my usage" as API key owner usage.

Implementation planning should:

- add or use a stable API key owner concept;
- snapshot request-time owner identity into request usage evidence when
  implementation begins;
- classify API keys as personal, project shared, or service keys;
- show shared/service key usage as shared/service usage, not as exact personal
  consumption;
- keep model-level cost breakdown available for the current user, project admin,
  org admin, billing admin, and platform admin according to scope;
- leave terminal end-user attribution to a later optional protocol field.

## Consequences

Benefits:

- Gives users a useful and auditable "my usage" view.
- Avoids false precision for shared API keys.
- Fits the current code direction because call logs already carry API key and
  cost fields.

Tradeoffs:

- A user can still share a personal key outside the system; UniRouter can only
  attribute usage to the key owner.
- Terminal end-user attribution requires a separate privacy and API-contract
  decision.
- Historical usage needs owner snapshotting to avoid changing when key ownership
  changes.

## Alternatives Considered

- Project-member proportional attribution: rejected because it fabricates usage
  shares without request-level evidence.
- End-user header attribution as PRD4 default: rejected because it adds API
  contract, privacy, hashing, and trust decisions that can follow later.
- Organization-only usage: rejected because it does not satisfy the User's
  per-user requirement.

## Verification

- Seed at least two users, two projects, multiple API keys, and multiple models.
- Verify each user sees only their own key-owner usage unless role scope grants
  broader visibility.
- Verify model-level cost sums reconcile to the user's total selected-window
  cost.
- Verify shared/service keys are labeled and not treated as exact personal usage.

## Rollback Or Exit Criteria

If a later enterprise requirement needs terminal end-user accounting, this ADR can
be extended with a separate end-user identity field and privacy model. Existing
API key owner usage should remain as a stable fallback attribution mode.
