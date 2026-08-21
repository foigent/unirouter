package billing

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	ProviderAttemptHealthSuccess              = "success"
	ProviderAttemptHealthProviderFailure      = "provider_failure"
	ProviderAttemptHealthAuthFailure          = "auth_failure"
	ProviderAttemptHealthRateLimited          = "rate_limited"
	ProviderAttemptHealthReleasedUndispatched = "released_undispatched"
)

var ErrProviderAttemptHealthConflict = errors.New("provider attempt health outcome conflicts with the durable record")

type ProviderAttemptHealthFenceRequest struct {
	OrgID             int64
	RequestID         string
	AttemptID         string
	EvidenceHash      string
	DispatchOwnerHash string
	Outcome           string
}

type ProviderAttemptOwnerRequest struct {
	OrgID             int64
	RequestID         string
	AttemptID         string
	EvidenceHash      string
	DispatchOwnerHash string
}

type ProviderAttemptHealthFenceResult uint8

const (
	ProviderAttemptHealthClaimed ProviderAttemptHealthFenceResult = iota + 1
	ProviderAttemptHealthReplay
)

// ValidateProviderAttemptOwnerTx locks and validates a still-streaming
// provider attempt for a lease-only operation. Accepted response headers do
// not terminate a streaming body, so both dispatch_started and
// response_accepted may renew while the terminal health fence remains empty.
func (b *Billing) ValidateProviderAttemptOwnerTx(
	ctx context.Context,
	tx pgx.Tx,
	request ProviderAttemptOwnerRequest,
) error {
	if tx == nil {
		return fmt.Errorf("billing transaction is unavailable")
	}
	req, err := normalizeProviderAttemptOwner(request)
	if err != nil {
		return err
	}
	var state string
	var evidenceHash, ownerHash, healthOutcome sql.NullString
	err = tx.QueryRow(ctx, `
SELECT state,route_evidence_hash,dispatch_owner_hash,health_outcome
  FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3
 FOR UPDATE`, req.OrgID, req.RequestID, req.AttemptID).Scan(
		&state, &evidenceHash, &ownerHash, &healthOutcome,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrProviderAttemptConflict
	}
	if err != nil {
		return fmt.Errorf("lock provider attempt owner: %w", err)
	}
	if !evidenceHash.Valid || evidenceHash.String != req.EvidenceHash ||
		!ownerHash.Valid || ownerHash.String != req.DispatchOwnerHash {
		return ErrProviderAttemptConflict
	}
	if !providerAttemptLeaseState(state) {
		return ErrProviderAttemptConflict
	}
	if healthOutcome.Valid {
		return ErrProviderAttemptHealthConflict
	}
	return nil
}

func providerAttemptLeaseState(state string) bool {
	return state == providerAttemptStateDispatchStarted || state == ProviderAttemptOutcomeResponseAccepted
}

// FenceProviderAttemptHealthTx claims the one terminal health outcome owned by
// an immutable provider-attempt receipt. The caller owns the transaction and
// must apply the corresponding health projection before commit. Exact replay
// is reported without touching either the receipt or the health projection.
func (b *Billing) FenceProviderAttemptHealthTx(
	ctx context.Context,
	tx pgx.Tx,
	request ProviderAttemptHealthFenceRequest,
) (ProviderAttemptHealthFenceResult, error) {
	if tx == nil {
		return 0, fmt.Errorf("billing transaction is unavailable")
	}
	req, err := normalizeProviderAttemptHealthFence(request)
	if err != nil {
		return 0, err
	}

	var state string
	var evidenceHash, ownerHash sql.NullString
	var currentOutcome, failureKind sql.NullString
	err = tx.QueryRow(ctx, `
SELECT state,route_evidence_hash,dispatch_owner_hash,health_outcome,failure_kind
  FROM billing_provider_attempts
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3
 FOR UPDATE`, req.OrgID, req.RequestID, req.AttemptID).Scan(
		&state, &evidenceHash, &ownerHash, &currentOutcome, &failureKind,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrProviderAttemptConflict
	}
	if err != nil {
		return 0, fmt.Errorf("lock provider attempt health fence: %w", err)
	}
	if !evidenceHash.Valid || evidenceHash.String != req.EvidenceHash ||
		!ownerHash.Valid || ownerHash.String != req.DispatchOwnerHash {
		return 0, ErrProviderAttemptConflict
	}
	if currentOutcome.Valid {
		if currentOutcome.String == req.Outcome {
			return ProviderAttemptHealthReplay, nil
		}
		return 0, ErrProviderAttemptHealthConflict
	}
	if !providerAttemptMayClaimHealth(state, failureKind, req.Outcome) {
		return 0, ErrProviderAttemptHealthConflict
	}

	var tag pgconn.CommandTag
	if req.Outcome == ProviderAttemptHealthReleasedUndispatched {
		tag, err = tx.Exec(ctx, `
UPDATE billing_provider_attempts
   SET state=$4,outcome_certainty=$5,retry_authorized=FALSE,
       financial_resolved_at=clock_timestamp(),
       health_outcome=$6,health_applied_at=clock_timestamp(),updated_at=clock_timestamp()
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3
   AND state='dispatch_started'
   AND route_evidence_hash=$7 AND dispatch_owner_hash=$8
   AND health_outcome IS NULL`,
			req.OrgID, req.RequestID, req.AttemptID,
			ProviderAttemptStateReleasedUndispatched, providerAttemptCertaintyNotDispatched,
			req.Outcome, req.EvidenceHash, req.DispatchOwnerHash,
		)
	} else {
		tag, err = tx.Exec(ctx, `
UPDATE billing_provider_attempts
   SET health_outcome=$4,health_applied_at=clock_timestamp(),updated_at=clock_timestamp()
 WHERE org_id=$1 AND request_id=$2 AND attempt_id=$3
   AND route_evidence_hash=$5 AND dispatch_owner_hash=$6
   AND health_outcome IS NULL`,
			req.OrgID, req.RequestID, req.AttemptID, req.Outcome,
			req.EvidenceHash, req.DispatchOwnerHash,
		)
	}
	if err != nil {
		return 0, fmt.Errorf("claim provider attempt health fence: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return 0, ErrProviderAttemptHealthConflict
	}
	return ProviderAttemptHealthClaimed, nil
}

func providerAttemptMayClaimHealth(state string, failureKind sql.NullString, outcome string) bool {
	switch outcome {
	case ProviderAttemptHealthReleasedUndispatched:
		return state == providerAttemptStateDispatchStarted && !failureKind.Valid
	case ProviderAttemptHealthSuccess:
		return state == ProviderAttemptOutcomeResponseAccepted && !failureKind.Valid
	case ProviderAttemptHealthProviderFailure:
		if state == ProviderAttemptOutcomeResponseAccepted {
			return !failureKind.Valid
		}
		return (state == ProviderAttemptOutcomeRejected || state == ProviderAttemptOutcomeUnknown ||
			state == ProviderAttemptOutcomeResponseUnusable) &&
			failureKind.Valid && failureKind.String == "channel_failure"
	case ProviderAttemptHealthAuthFailure:
		return state == ProviderAttemptOutcomeRejected && failureKind.Valid && failureKind.String == "auth"
	case ProviderAttemptHealthRateLimited:
		return state == ProviderAttemptOutcomeRejected && failureKind.Valid && failureKind.String == "rate_limit"
	default:
		return false
	}
}

func normalizeProviderAttemptHealthFence(
	request ProviderAttemptHealthFenceRequest,
) (ProviderAttemptHealthFenceRequest, error) {
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.AttemptID = strings.TrimSpace(request.AttemptID)
	request.EvidenceHash = strings.TrimSpace(request.EvidenceHash)
	request.DispatchOwnerHash = strings.TrimSpace(request.DispatchOwnerHash)
	request.Outcome = strings.TrimSpace(request.Outcome)
	if request.OrgID <= 0 || request.RequestID == "" || len(request.RequestID) > 64 ||
		request.AttemptID == "" || len(request.AttemptID) > 64 ||
		!validProviderAttemptDigest(request.EvidenceHash) ||
		!validProviderAttemptDigest(request.DispatchOwnerHash) {
		return request, fmt.Errorf("provider attempt health fence requires bounded identities and digests")
	}
	switch request.Outcome {
	case ProviderAttemptHealthSuccess,
		ProviderAttemptHealthProviderFailure,
		ProviderAttemptHealthAuthFailure,
		ProviderAttemptHealthRateLimited,
		ProviderAttemptHealthReleasedUndispatched:
	default:
		return request, fmt.Errorf("unsupported provider attempt health outcome %q", request.Outcome)
	}
	return request, nil
}

func normalizeProviderAttemptOwner(request ProviderAttemptOwnerRequest) (ProviderAttemptOwnerRequest, error) {
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.AttemptID = strings.TrimSpace(request.AttemptID)
	request.EvidenceHash = strings.TrimSpace(request.EvidenceHash)
	request.DispatchOwnerHash = strings.TrimSpace(request.DispatchOwnerHash)
	if request.OrgID <= 0 || request.RequestID == "" || len(request.RequestID) > 64 ||
		request.AttemptID == "" || len(request.AttemptID) > 64 ||
		!validProviderAttemptDigest(request.EvidenceHash) ||
		!validProviderAttemptDigest(request.DispatchOwnerHash) {
		return request, fmt.Errorf("provider attempt owner requires bounded identities and digests")
	}
	return request, nil
}

func validProviderAttemptDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value[len("sha256:"):])
	return err == nil && len(decoded) == 32
}
