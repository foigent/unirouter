package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/money"
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
	AmountMicros           int64
	CalculatedCostMicros   int64
	NewFrozenMicros        int64
	NewBalanceMicros       int64
	PricingVersion         string
	BillingMode            BillingMode
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
	var mode BillingMode
	var frozenAmount, reservedBalance, reservedGranted int64
	var reservedCompletion, currentSequence int
	var balance, grantedBalance, outputPrice int64
	var safetyFactor, pricingVersion, status, pendingReason string
	var payloadReady bool
	err = tx.QueryRow(ctx, `
		SELECT f.billing_mode, f.frozen_amount_micros, f.reserved_balance_micros, f.reserved_granted_micros,
		       f.reserved_completion_tokens, f.reservation_extension_sequence,
		       f.effective_output_price_micros_per_million, f.safety_factor::text,
		       f.pricing_version, f.status, f.settlement_pending_reason,
		       f.settlement_payload_ready_at IS NOT NULL
		FROM freeze_idempotency f
		WHERE f.org_id=$1 AND f.request_id=$2
		FOR UPDATE OF f`, request.OrgID, request.RequestID,
	).Scan(
		&mode,
		&frozenAmount, &reservedBalance, &reservedGranted, &reservedCompletion, &currentSequence,
		&outputPrice, &safetyFactor, &pricingVersion, &status, &pendingReason, &payloadReady,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return &ExtendReservationResult{Success: false, ErrorCode: "not_found"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock reservation extension: %w", err)
	}
	if err := validateReservationBillingMode(mode); err != nil {
		return nil, err
	}
	if mode == BillingModePrepaidEnforced {
		if err := tx.QueryRow(ctx,
			`SELECT balance_micros, granted_balance_micros
			 FROM wallets WHERE org_id=$1 FOR UPDATE`, request.OrgID,
		).Scan(&balance, &grantedBalance); err != nil {
			return nil, fmt.Errorf("lock reservation extension wallet: %w", err)
		}
	}

	var existing ExtendReservationResult
	var existingStatus string
	err = tx.QueryRow(ctx, `
		SELECT sequence, additional_completion_tokens, target_completion_tokens,
		       reserved_micros, calculated_cost_micros, status, error_code
		FROM billing_reservation_extensions
		WHERE org_id=$1 AND request_id=$2 AND sequence=$3`,
		request.OrgID, request.RequestID, request.Sequence,
	).Scan(
		&existing.Sequence, &existing.AddedCompletionTokens, &existing.TargetCompletionTokens,
		&existing.AmountMicros, &existing.CalculatedCostMicros, &existingStatus, &existing.ErrorCode,
	)
	if err == nil {
		if existing.AddedCompletionTokens != request.AdditionalCompletionTokens ||
			existing.TargetCompletionTokens != request.TargetCompletionTokens {
			return nil, fmt.Errorf("billing reservation extension sequence conflicts with its durable receipt")
		}
		existing.Success = existingStatus == "reserved" || existingStatus == "observed"
		if existing.CalculatedCostMicros == 0 && existing.AmountMicros > 0 {
			// Pre-migration prepaid receipts used reserved_micros as their
			// calculated admission estimate.
			existing.CalculatedCostMicros = existing.AmountMicros
		}
		existing.NewFrozenMicros = frozenAmount
		existing.NewBalanceMicros = balance
		existing.PricingVersion = pricingVersion
		existing.BillingMode = mode
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
	amount, err := money.EstimateHold(int64(request.AdditionalCompletionTokens), outputPrice, safetyFactor, 0)
	if err != nil {
		return nil, fmt.Errorf("calculate reservation extension: %w", err)
	}
	if mode == BillingModeCostObservation {
		if _, err := tx.Exec(ctx, `
			UPDATE freeze_idempotency
			SET reserved_completion_tokens=$3,
			    reservation_extension_sequence=$4,
			    estimated_tokens=estimated_tokens+$5,
			    updated_at=NOW()
			WHERE org_id=$1 AND request_id=$2 AND billing_mode='cost_observation'`,
			request.OrgID, request.RequestID, request.TargetCompletionTokens,
			request.Sequence, request.AdditionalCompletionTokens,
		); err != nil {
			return nil, fmt.Errorf("grow observation reservation: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO billing_reservation_extensions (
				org_id, request_id, sequence, additional_completion_tokens,
				target_completion_tokens, reserved_micros,
				reserved_balance_micros, reserved_granted_micros,
				calculated_cost_micros, pricing_version, status, error_code
			) VALUES ($1,$2,$3,$4,$5,0,0,0,$6,$7,'observed','')`,
			request.OrgID, request.RequestID, request.Sequence,
			request.AdditionalCompletionTokens, request.TargetCompletionTokens,
			amount, pricingVersion,
		); err != nil {
			return nil, fmt.Errorf("record observation reservation extension: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit observation reservation extension: %w", err)
		}
		return &ExtendReservationResult{
			Success: true, Sequence: request.Sequence,
			AddedCompletionTokens:  request.AdditionalCompletionTokens,
			TargetCompletionTokens: request.TargetCompletionTokens,
			AmountMicros:           0, CalculatedCostMicros: amount,
			NewFrozenMicros: frozenAmount, NewBalanceMicros: 0,
			PricingVersion: pricingVersion, BillingMode: mode,
		}, nil
	}
	_, expiredGrantBalance, err := expireGrantRowsInLockedWallet(ctx, tx, request.OrgID)
	if err != nil {
		return nil, err
	}
	grantedBalance -= minInt64(grantedBalance, expiredGrantBalance)
	available, err := money.Add(balance, grantedBalance)
	if err != nil {
		return nil, fmt.Errorf("calculate reservation extension availability: %w", err)
	}
	if available < amount {
		if _, err := tx.Exec(ctx, `
			INSERT INTO billing_reservation_extensions (
				org_id, request_id, sequence, additional_completion_tokens,
				target_completion_tokens, reserved_micros,
				reserved_balance_micros, reserved_granted_micros,
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
			CalculatedCostMicros:   amount,
			NewFrozenMicros:        frozenAmount, NewBalanceMicros: balance,
			PricingVersion: pricingVersion, ErrorCode: "insufficient_balance",
			BillingMode: mode,
			ErrorDetail: fmt.Sprintf("available=%d, required=%d", available, amount),
		}, nil
	}

	reservedGrant := minInt64(grantedBalance, amount)
	reservedCash := amount - reservedGrant
	newGranted := grantedBalance - reservedGrant
	newBalance := balance - reservedCash
	newFrozen, err := money.Add(frozenAmount, amount)
	if err != nil {
		return nil, fmt.Errorf("calculate reservation extension receipt: %w", err)
	}
	allocations, err := reserveGrantRows(ctx, tx, request.OrgID, reservedGrant)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE wallets
		SET balance_micros=$2, granted_balance_micros=$3, frozen_micros=frozen_micros+$4, updated_at=NOW()
		WHERE org_id=$1`, request.OrgID, newBalance, newGranted, amount); err != nil {
		return nil, fmt.Errorf("reserve wallet extension: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE freeze_idempotency
		SET frozen_amount_micros=frozen_amount_micros+$3,
		    reserved_balance_micros=reserved_balance_micros+$4,
		    reserved_granted_micros=reserved_granted_micros+$5,
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
			INSERT INTO billing_reservation_grant_allocations (org_id,request_id,grant_id,reserved_micros)
			VALUES ($1,$2,$3,$4)
			ON CONFLICT (org_id,request_id,grant_id)
			DO UPDATE SET reserved_micros=billing_reservation_grant_allocations.reserved_micros+EXCLUDED.reserved_micros,
			              updated_at=NOW()`,
			request.OrgID, request.RequestID, allocation.GrantID, allocation.Micros,
		); err != nil {
			return nil, fmt.Errorf("record grant extension allocation: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO transactions (request_id,org_id,type,status,amount_micros,note,created_at)
		VALUES ($1,$2,'freeze','completed',$3,$4,NOW())`,
		request.RequestID, request.OrgID, amount,
		fmt.Sprintf("Extend freeze for request %s sequence %d", request.RequestID, request.Sequence),
	); err != nil {
		return nil, fmt.Errorf("record reservation extension transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO billing_reservation_extensions (
			org_id, request_id, sequence, additional_completion_tokens,
		target_completion_tokens, reserved_micros,
		reserved_balance_micros, reserved_granted_micros,
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
		AmountMicros:           amount, CalculatedCostMicros: amount, NewFrozenMicros: newFrozen,
		NewBalanceMicros: newBalance, PricingVersion: pricingVersion,
		BillingMode: mode,
	}, nil
}
