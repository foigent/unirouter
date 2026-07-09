# PRD5 Batch 1W Pre-Relay Model Permission Evidence Parity

> Date: 2026-07-10
> Risk: R3 live authorization and evidence path
> Status: CI verified at `931400a` / PR #126; target deployment/rerun pending

## Trigger

Gate A authorization `UR-XI-GATE-A-20260710-01` produced an immutable sanitized snapshot. The successful scenario correlated correctly, but the model-permission denial returned HTTP 403 without a response `X-Request-ID` and without the expected source trace.

## Root Cause

`prepareGatewayModelRequest` repeated the API-key `allowed_models` check after model-governance resolution and returned before `Relay.HandleChatCompletion`. Direct Relay tests therefore passed while the live wrapper bypassed Relay's correlation and decision-evidence behavior.

## Change

- Remove the duplicate wrapper-owned API-key denial.
- Preserve wrapper-owned project model-governance resolution and body rewriting.
- Let Relay own the shared Chat/Embeddings API-key model restriction.
- Add migrated-DB full-handler coverage for the denied path.

## Acceptance Assertions

The integration test proves that a project-allowed but API-key-denied alias:

1. returns HTTP 403;
2. returns the same `X-Request-ID` supplied by the caller;
3. writes `auth.model_not_allowed` at stage `auth` with status 403;
4. writes `capture_gap=permission_denied`;
5. writes `decision_kind=auth`, `decision_result=rejected`, and `decision_reason=auth_error.model_not_allowed`;
6. writes zero call logs and zero billing freezes.

## Verification Commands

```bash
go test ./cmd/uniroute ./internal/gateway -count=1
go test -tags=integration ./cmd/uniroute -run TestPRD2P0DeveloperProjectKeyGatewayE2E_Integration -count=1 -v
go test ./... -count=1
git diff --check
```

The execution owner must update this evidence if any command fails after rebasing onto `origin/main`. GitHub CI and any target rerun are separate evidence layers.

Current execution result before repository integration:

- focused `cmd/uniroute` and `internal/gateway`: PASS;
- migrated-DB integration test through migration `000018`: PASS;
- full Go test suite: PASS;
- diff whitespace/error check: PASS.

## Repository Integration

- PR: #126
- merged commit: `931400a`
- `Backend Tests`: PASS
- `Frontend Build`: PASS
- `Docker Build`: PASS

This promotes Batch 1W to `CI verified`. It does not prove deployment or close
the target denied-scenario gap.

## Target Evidence Boundary

- The Batch 1V snapshot is not modified or regenerated.
- Its artifact SHA-256 remains `5b1d52b1e8bd9b47f14de74670eb4404ed8862c32cdf323d86766446cdf438e8`.
- The existing denied capture gaps remain factual pre-fix diagnostic evidence.
- At this local-evidence checkpoint, X-Insight persistent import/receipt had not
  yet been received. The later external state is superseded by
  `docs/ops/evidence/PRD5-GATE-A-XINSIGHT-external-integration-20260710.md`.
- A target rerun requires a new or amended authorization and an available non-production deployment path.

## Rollback

There is no migration or data mutation. Revert the Batch 1W code through a protected PR if regression evidence appears. No target deployment or production claim is part of this local evidence.
