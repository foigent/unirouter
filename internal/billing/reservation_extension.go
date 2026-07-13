package billing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ExtendReservationRequest identifies one monotonic, idempotent output-token
// reservation step. TargetCompletionTokens prevents a caller from silently
// applying the same delta to a different reservation generation.
type ExtendReservationRequest struct {
	RequestID                  string
	OrgID                      int64
	Sequence                   int
	AdditionalCompletionTokens int
	TargetCompletionTokens     int
}

type ExtendReservationResult struct {
	Success                bool
	Sequence               int
	AddedCompletionTokens  int
	TargetCompletionTokens int
	Amount                 int
	NewFrozenAmount        int
	NewBalance             int
	PricingVersion         string
	ErrorCode              string
	ErrorDetail            string
}

// ExtendReservation grows an existing reservation at its immutable output
// price. The receipt is durable for both success and insufficient-funds
// rejection, so a transport retry cannot reserve the same step twice.
func (b *Billing) ExtendReservation(ctx context.Context, request ExtendReservationRequest) (*ExtendReservationResult, error) {
	if b == nil || b.db == nil {
		return nil, fmt.Errorf("billing database is unavailable")
	}
	request.RequestID = strings.TrimSpace(request.RequestID)
	if request.RequestID == "" || len(request.RequestID) > 64 || request.OrgID <= 0 || request.Sequence <= 0 ||
		request.AdditionalCompletionTokens <= 0 || request.TargetCompletionTokens <= 0 ||
		request.AdditionalCompletionTokens > maxPersistedTokenCount || request.TargetCompletionTokens > maxPersistedTokenCount {
		return nil, fmt.Errorf("billing reservation extension is invalid")
	}

	tx, err := b.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin reservation extension: %w", err)
	}
	defer tx.Rollback(ctx)

	var frozenAmount, reservedBalance, reservedGranted, reservedCompletion, currentSequence int
	var balance, grantedBalance int
	var outputPrice, safetyFactor float64
	var pricingVersion, status, pendingReason string
	var payloadReady bool
	err = tx.QueryRow(ctx, `
		SELECT f.frozen_amount, f.reserved_balance_amount, f.reserved_granted_amount,
		       f.reserved_completion_tokens, f.reservation_extension_sequence,
		       f.effective_output_price_cents::float8, f.safety_factor::float8,
		       f.pricing_version, f.status, f.settlement_pending_reason,
		       f.settlement_payload_ready_at IS NOT NULL,
		       w.balance, w.granted_balance
		FROM freeze_idempotency f
		JOIN wallets w ON w.org_id=f.org_id
		WHERE f.org_id=$1 AND f.request_id=$2
		FOR UPDATE OF f, w`, request.OrgID, request.RequestID,
	).Scan(
		&frozenAmount, &reservedBalance, &reservedGranted, &reservedCompletion, &currentSequence,
		&outputPrice, &safetyFactor, &pricingVersion, &status, &pendingReason, &payloadReady, &balance, &grantedBalance,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return &ExtendReservationResult{Success: false, ErrorCode: "not_found"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock reservation extension: %w", err)
	}

	var existing ExtendReservationResult
	var existingStatus string
	err = tx.QueryRow(ctx, `
		SELECT sequence, additional_completion_tokens, target_completion_tokens,
		       reserved_amount, status, error_code
		FROM billing_reservation_extensions
		WHERE org_id=$1 AND request_id=$2 AND sequence=$3`,
		request.OrgID, request.RequestID, request.Sequence,
	).Scan(
		&existing.Sequence, &existing.AddedCompletionTokens, &existing.TargetCompletionTokens,
		&existing.Amount, &existingStatus, &existing.ErrorCode,
	)
	if err == nil {
		if existing.AddedCompletionTokens != request.AdditionalCompletionTokens ||
			existing.TargetCompletionTokens != request.TargetCompletionTokens {
			return nil, fmt.Errorf("billing reservation extension sequence conflicts with its durable receipt")
		}
		existing.Success = existingStatus == "reserved"
		existing.NewFrozenAmount = frozenAmount
		existing.NewBalance = balance
		existing.PricingVersion = pricingVersion
		return &existing, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("read reservation extension receipt: %w", err)
	}
	if request.Sequence != currentSequence+1 || request.TargetCompletionTokens != reservedCompletion+request.AdditionalCompletionTokens {
		return nil, fmt.Errorf("billing reservation extension is not the next monotonic step")
	}
	extendableStreamState := status == "frozen" ||
		(status == "settlement_pending" && (pendingReason == "billable_output" || pendingReason == "delivery_prepared"))
	if !extendableStreamState || payloadReady {
		return &ExtendReservationResult{
			Success: false, Sequence: request.Sequence, ErrorCode: "reservation_not_extendable",
			ErrorDetail: "the reservation no longer accepts output-token extensions",
		}, nil
	}
	if safetyFactor <= 0 {
		return nil, fmt.Errorf("reservation has an invalid safety factor")
	}

	amount := int(math.Ceil(float64(request.AdditionalCompletionTokens) * outputPrice / 1000.0 * safetyFactor))
	if amount < 0 {
		return nil, fmt.Errorf("reservation extension amount overflow")
	}
	_, expiredGrantBalance, err := expireGrantRowsInLockedWallet(ctx, tx, request.OrgID)
	if err != nil {
		return nil, err
	}
	grantedBalance -= minInt(grantedBalance, expiredGrantBalance)
	available := balance + grantedBalance
	if available < amount {
		if _, err := tx.Exec(ctx, `
			INSERT INTO billing_reservation_extensions (
				org_id, request_id, sequence, additional_completion_tokens,
				target_completion_tokens, reserved_amount,
				reserved_balance_amount, reserved_granted_amount,
				pricing_version, status, error_code
			) VALUES ($1,$2,$3,$4,$5,0,0,0,$6,'rejected','insufficient_balance')`,
			request.OrgID, request.RequestID, request.Sequence,
			request.AdditionalCompletionTokens, request.TargetCompletionTokens, pricingVersion,
		); err != nil {
			return nil, fmt.Errorf("record rejected reservation extension: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE freeze_idempotency
			SET reservation_extension_sequence=$3, updated_at=NOW()
			WHERE org_id=$1 AND request_id=$2`, request.OrgID, request.RequestID, request.Sequence); err != nil {
			return nil, fmt.Errorf("advance rejected reservation extension sequence: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit rejected reservation extension: %w", err)
		}
		return &ExtendReservationResult{
			Success: false, Sequence: request.Sequence,
			AddedCompletionTokens:  request.AdditionalCompletionTokens,
			TargetCompletionTokens: request.TargetCompletionTokens,
			NewFrozenAmount:        frozenAmount, NewBalance: balance,
			PricingVersion: pricingVersion, ErrorCode: "insufficient_balance",
			ErrorDetail: fmt.Sprintf("available=%d, required=%d", available, amount),
		}, nil
	}

	reservedGrant := minInt(grantedBalance, amount)
	reservedCash := amount - reservedGrant
	newGranted := grantedBalance - reservedGrant
	newBalance := balance - reservedCash
	allocations, err := reserveGrantRows(ctx, tx, request.OrgID, reservedGrant)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE wallets
		SET balance=$2, granted_balance=$3, frozen=frozen+$4, updated_at=NOW()
		WHERE org_id=$1`, request.OrgID, newBalance, newGranted, amount); err != nil {
		return nil, fmt.Errorf("reserve wallet extension: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE freeze_idempotency
		SET frozen_amount=frozen_amount+$3,
		    reserved_balance_amount=reserved_balance_amount+$4,
		    reserved_granted_amount=reserved_granted_amount+$5,
		    reserved_completion_tokens=$6,
		    reservation_extension_sequence=$7,
		    estimated_tokens=estimated_tokens+$8,
		    updated_at=NOW()
		WHERE org_id=$1 AND request_id=$2`,
		request.OrgID, request.RequestID, amount, reservedCash, reservedGrant,
		request.TargetCompletionTokens, request.Sequence, request.AdditionalCompletionTokens,
	); err != nil {
		return nil, fmt.Errorf("grow billing reservation: %w", err)
	}
	for _, allocation := range allocations {
		if _, err := tx.Exec(ctx, `
			INSERT INTO billing_reservation_grant_allocations (org_id,request_id,grant_id,reserved_amount)
			VALUES ($1,$2,$3,$4)
			ON CONFLICT (org_id,request_id,grant_id)
			DO UPDATE SET reserved_amount=billing_reservation_grant_allocations.reserved_amount+EXCLUDED.reserved_amount,
			              updated_at=NOW()`,
			request.OrgID, request.RequestID, allocation.GrantID, allocation.Amount,
		); err != nil {
			return nil, fmt.Errorf("record grant extension allocation: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO transactions (request_id,org_id,type,status,amount,note,created_at)
		VALUES ($1,$2,'freeze','completed',$3,$4,NOW())`,
		request.RequestID, request.OrgID, amount,
		fmt.Sprintf("Extend freeze for request %s sequence %d", request.RequestID, request.Sequence),
	); err != nil {
		return nil, fmt.Errorf("record reservation extension transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO billing_reservation_extensions (
			org_id, request_id, sequence, additional_completion_tokens,
			target_completion_tokens, reserved_amount,
			reserved_balance_amount, reserved_granted_amount,
			pricing_version, status, error_code
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'reserved','')`,
		request.OrgID, request.RequestID, request.Sequence,
		request.AdditionalCompletionTokens, request.TargetCompletionTokens,
		amount, reservedCash, reservedGrant, pricingVersion,
	); err != nil {
		return nil, fmt.Errorf("record reservation extension receipt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit reservation extension: %w", err)
	}
	return &ExtendReservationResult{
		Success: true, Sequence: request.Sequence,
		AddedCompletionTokens:  request.AdditionalCompletionTokens,
		TargetCompletionTokens: request.TargetCompletionTokens,
		Amount:                 amount, NewFrozenAmount: frozenAmount + amount,
		NewBalance: newBalance, PricingVersion: pricingVersion,
	}, nil
}
