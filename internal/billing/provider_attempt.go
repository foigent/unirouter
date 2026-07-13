package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	ProviderAttemptOutcomeRejected         = "rejected"
	ProviderAttemptOutcomeResponseAccepted = "response_accepted"
	ProviderAttemptOutcomeUnknown          = "outcome_unknown"
	ProviderAttemptOutcomeResponseUnusable = "response_unusable"
)

const (
	providerAttemptStateDispatchStarted    = "dispatch_started"
	providerAttemptStateFundingPending     = "funding_pending"
	providerAttemptStateSettled            = "settled"
	providerAttemptCertaintyPending        = "pending"
	providerAttemptCertaintyRejected       = "known_rejected"
	providerAttemptCertaintyUnknown        = "unknown"
	providerAttemptCertaintyKnownUsage     = "known_usage"
	pendingReasonAmbiguousAttempt          = "ambiguous_attempt"
	pendingReasonResponseUnusable          = "provider_response_unusable"
	pendingReasonProviderAttemptUnresolved = "provider_attempt_unresolved"
)

var ErrProviderAttemptConflict = errors.New("provider attempt evidence conflicts with the durable record")
var ErrProviderAttemptUnresolved = errors.New("provider attempt has unresolved financial liability")

// ProviderAttemptStart is the bounded identity envelope persisted before a
// network call that may create provider liability. It deliberately excludes
// credentials, prompts, response bodies, and full provider URLs.
type ProviderAttemptStart struct {
	RequestID     string
	AttemptID     string
	OrgID         int64
	APIKeyID      int64
	ChannelID     int64
	ChannelKeyID  int64
	RouteSequence int
	RouteTotal    int
	Endpoint      string
	Provider      string
	Channel       string
}

// ProviderAttemptCompletion records what UniRouter knows after dispatch. A
// retry is financially authorized only for a durable rejected response.
type ProviderAttemptCompletion struct {
	RequestID         string
	AttemptID         string
	OrgID             int64
	Outcome           string
	RetryAuthorized   bool
	HTTPStatus        int
	FailureKind       string
	UpstreamRequestID string
}

type providerAttemptRecord struct {
	APIKeyID              int64
	ChannelID             int64
	ChannelKeyID          int64
	RouteSequence         int
	RouteTotal            int
	Endpoint              string
	Provider              string
	Channel               string
	State                 string
	OutcomeCertainty      string
	RetryAuthorized       bool
	HTTPStatus            sql.NullInt32
	FailureKind           sql.NullString
	UpstreamRequestID     sql.NullString
	CalculatedCost        sql.NullInt64
	ChargedCost           sql.NullInt64
	TerminalTransactionID sql.NullInt64
}

func normalizeProviderAttemptStart(req ProviderAttemptStart) (ProviderAttemptStart, error) {
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.AttemptID = strings.TrimSpace(req.AttemptID)
	req.Endpoint = strings.TrimSpace(req.Endpoint)
	req.Provider = strings.TrimSpace(req.Provider)
	req.Channel = strings.TrimSpace(req.Channel)
	if req.RequestID == "" || len(req.RequestID) > 64 || req.AttemptID == "" || len(req.AttemptID) > 64 {
		return req, fmt.Errorf("provider attempt requires bounded request_id and attempt_id")
	}
	if req.OrgID <= 0 || req.APIKeyID <= 0 || req.ChannelID <= 0 || req.ChannelKeyID <= 0 {
		return req, fmt.Errorf("provider attempt requires positive org, API key, channel, and channel-key identities")
	}
	if req.RouteSequence <= 0 || req.RouteTotal <= 0 || req.RouteSequence > req.RouteTotal {
		return req, fmt.Errorf("provider attempt route sequence is invalid")
	}
	if req.Endpoint == "" || len(req.Endpoint) > 64 || req.Provider == "" || len(req.Provider) > 50 || req.Channel == "" || len(req.Channel) > 255 {
		return req, fmt.Errorf("provider attempt routing metadata is invalid")
	}
	return req, nil
}

func normalizeProviderAttemptCompletion(req ProviderAttemptCompletion) (ProviderAttemptCompletion, string, error) {
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.AttemptID = strings.TrimSpace(req.AttemptID)
	req.Outcome = strings.TrimSpace(req.Outcome)
	req.FailureKind = strings.TrimSpace(req.FailureKind)
	req.UpstreamRequestID = strings.TrimSpace(req.UpstreamRequestID)
	if req.RequestID == "" || len(req.RequestID) > 64 || req.AttemptID == "" || len(req.AttemptID) > 64 || req.OrgID <= 0 {
		return req, "", fmt.Errorf("provider attempt completion requires bounded identities")
	}
	if req.HTTPStatus < 0 || req.HTTPStatus > 599 || (req.HTTPStatus > 0 && req.HTTPStatus < 100) {
		return req, "", fmt.Errorf("provider attempt completion has invalid HTTP status")
	}
	if len(req.FailureKind) > 40 || len(req.UpstreamRequestID) > 128 {
		return req, "", fmt.Errorf("provider attempt completion metadata is too long")
	}
	var certainty string
	switch req.Outcome {
	case ProviderAttemptOutcomeRejected:
		certainty = providerAttemptCertaintyRejected
		if req.HTTPStatus == 0 {
			return req, "", fmt.Errorf("rejected provider attempt requires an HTTP status")
		}
	case ProviderAttemptOutcomeResponseAccepted:
		certainty = providerAttemptCertaintyPending
		if req.HTTPStatus < 200 || req.HTTPStatus > 299 || req.RetryAuthorized {
			return req, "", fmt.Errorf("accepted provider response cannot authorize retry")
		}
	case ProviderAttemptOutcomeUnknown:
		certainty = providerAttemptCertaintyUnknown
		if req.HTTPStatus != 0 || req.RetryAuthorized {
			return req, "", fmt.Errorf("ambiguous provider outcome cannot authorize retry or claim a status")
		}
	case ProviderAttemptOutcomeResponseUnusable:
		certainty = providerAttemptCertaintyUnknown
		if req.HTTPStatus < 200 || req.HTTPStatus > 299 || req.RetryAuthorized {
			return req, "", fmt.Errorf("unusable provider response cannot authorize retry")
		}
	default:
		return req, "", fmt.Errorf("unsupported provider attempt outcome %q", req.Outcome)
	}
	return req, certainty, nil
}

// BeginProviderAttempt durably opens the financial attempt before dispatch.
// A second attempt cannot start while an earlier attempt is unresolved.
func (b *Billing) BeginProviderAttempt(ctx context.Context, request ProviderAttemptStart) error {
	if b == nil || b.db == nil {
		return fmt.Errorf("billing database is unavailable")
	}
	req, err := normalizeProviderAttemptStart(request)
	if err != nil {
		return err
	}
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin provider attempt: %w", err)
	}
	defer tx.Rollback(ctx)

	record, err := reservationForUpdate(ctx, tx, req.RequestID, req.OrgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("begin provider attempt: reservation not found")
		}
		return fmt.Errorf("lock provider attempt reservation: %w", err)
	}
	if record.Status != "frozen" {
		return fmt.Errorf("begin provider attempt: reservation is %s", record.Status)
	}

	existing, err := providerAttemptForUpdate(ctx, tx, req.OrgID, req.RequestID, req.AttemptID)
	if err == nil {
		if !providerAttemptStartMatches(existing, req) || existing.State != providerAttemptStateDispatchStarted {
			return ErrProviderAttemptConflict
		}
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lock provider attempt: %w", err)
	}

	var unresolved bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (
		    SELECT 1 FROM billing_provider_attempts
		    WHERE org_id = $1 AND request_id = $2
		      AND state IN (
		          'dispatch_started', 'response_accepted', 'outcome_unknown',
		          'response_unusable', 'funding_pending'
		      )
		)`, req.OrgID, req.RequestID,
	).Scan(&unresolved); err != nil {
		return fmt.Errorf("check unresolved provider attempt: %w", err)
	}
	if unresolved {
		return ErrProviderAttemptUnresolved
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO billing_provider_attempts (
		    org_id, request_id, attempt_id, api_key_id, channel_id, channel_key_id,
		    route_sequence, route_total, endpoint, provider, channel_name
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		req.OrgID, req.RequestID, req.AttemptID, req.APIKeyID, req.ChannelID, req.ChannelKeyID,
		req.RouteSequence, req.RouteTotal, req.Endpoint, req.Provider, req.Channel,
	); err != nil {
		return fmt.Errorf("insert provider attempt: %w", err)
	}
	return tx.Commit(ctx)
}

// CompleteProviderAttempt advances only monotonic evidence transitions. An
// ambiguous dispatch or unusable 2xx response atomically moves the reservation
// into a non-cancellable provider-cost hold.
func (b *Billing) CompleteProviderAttempt(ctx context.Context, request ProviderAttemptCompletion) error {
	if b == nil || b.db == nil {
		return fmt.Errorf("billing database is unavailable")
	}
	req, certainty, err := normalizeProviderAttemptCompletion(request)
	if err != nil {
		return err
	}
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("complete provider attempt: %w", err)
	}
	defer tx.Rollback(ctx)

	reservation, err := reservationForUpdate(ctx, tx, req.RequestID, req.OrgID)
	if err != nil {
		return fmt.Errorf("lock provider attempt reservation: %w", err)
	}
	attempt, err := providerAttemptForUpdate(ctx, tx, req.OrgID, req.RequestID, req.AttemptID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("complete provider attempt: attempt not found")
		}
		return fmt.Errorf("lock provider attempt: %w", err)
	}

	if providerAttemptCompletionMatches(attempt, req) {
		return tx.Commit(ctx)
	}
	allowed := attempt.State == providerAttemptStateDispatchStarted ||
		(attempt.State == ProviderAttemptOutcomeResponseAccepted && req.Outcome == ProviderAttemptOutcomeResponseUnusable)
	if !allowed {
		return ErrProviderAttemptConflict
	}
	if reservation.Status != "frozen" && reservation.Status != "settlement_pending" {
		return fmt.Errorf("complete provider attempt: reservation is %s", reservation.Status)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE billing_provider_attempts
		 SET state = $4::varchar(32),
		     outcome_certainty = $5::varchar(24),
		     retry_authorized = $6,
		     http_status = NULLIF($7, 0),
		     failure_kind = NULLIF($8::text, ''),
		     upstream_request_id = NULLIF($9::text, ''),
		     response_received_at = CASE WHEN $7 > 0 THEN COALESCE(response_received_at, NOW()) ELSE response_received_at END,
		     financial_resolved_at = CASE WHEN $4::text = 'rejected' THEN COALESCE(financial_resolved_at, NOW()) ELSE NULL END,
		     updated_at = NOW()
		 WHERE org_id = $1 AND request_id = $2 AND attempt_id = $3`,
		req.OrgID, req.RequestID, req.AttemptID, req.Outcome, certainty,
		req.RetryAuthorized, req.HTTPStatus, req.FailureKind, req.UpstreamRequestID,
	); err != nil {
		return fmt.Errorf("update provider attempt: %w", err)
	}

	if req.Outcome == ProviderAttemptOutcomeUnknown || req.Outcome == ProviderAttemptOutcomeResponseUnusable {
		reason := pendingReasonAmbiguousAttempt
		if req.Outcome == ProviderAttemptOutcomeResponseUnusable {
			reason = pendingReasonResponseUnusable
		}
		if _, err := tx.Exec(ctx,
			`UPDATE freeze_idempotency
			 SET status = 'settlement_pending',
			     provider_cost_pending_at = COALESCE(provider_cost_pending_at, NOW()),
			     settlement_pending_reason = $3,
			     settlement_claimed_until = NULL,
			     settlement_next_attempt_at = NULL,
			     updated_at = NOW()
			 WHERE org_id = $1 AND request_id = $2
			   AND status IN ('frozen', 'settlement_pending')`,
			req.OrgID, req.RequestID, reason,
		); err != nil {
			return fmt.Errorf("hold unresolved provider attempt: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func providerAttemptForUpdate(ctx context.Context, tx pgx.Tx, orgID int64, requestID, attemptID string) (providerAttemptRecord, error) {
	var record providerAttemptRecord
	err := tx.QueryRow(ctx,
		`SELECT api_key_id, channel_id, channel_key_id, route_sequence, route_total,
		        endpoint, provider, channel_name, state, outcome_certainty, retry_authorized,
		        http_status, failure_kind, upstream_request_id, calculated_cost, charged_cost,
		        terminal_transaction_id
		 FROM billing_provider_attempts
		 WHERE org_id = $1 AND request_id = $2 AND attempt_id = $3
		 FOR UPDATE`, orgID, requestID, attemptID,
	).Scan(
		&record.APIKeyID, &record.ChannelID, &record.ChannelKeyID, &record.RouteSequence, &record.RouteTotal,
		&record.Endpoint, &record.Provider, &record.Channel, &record.State, &record.OutcomeCertainty,
		&record.RetryAuthorized, &record.HTTPStatus, &record.FailureKind,
		&record.UpstreamRequestID, &record.CalculatedCost, &record.ChargedCost,
		&record.TerminalTransactionID,
	)
	return record, err
}

func providerAttemptStartMatches(record providerAttemptRecord, req ProviderAttemptStart) bool {
	return record.APIKeyID == req.APIKeyID && record.ChannelID == req.ChannelID && record.ChannelKeyID == req.ChannelKeyID &&
		record.RouteSequence == req.RouteSequence && record.RouteTotal == req.RouteTotal &&
		record.Endpoint == req.Endpoint && record.Provider == req.Provider && record.Channel == req.Channel
}

func providerAttemptCompletionMatches(record providerAttemptRecord, req ProviderAttemptCompletion) bool {
	return record.State == req.Outcome && record.RetryAuthorized == req.RetryAuthorized &&
		nullableInt32Equals(record.HTTPStatus, req.HTTPStatus) &&
		nullableStringEquals(record.FailureKind, req.FailureKind) &&
		nullableStringEquals(record.UpstreamRequestID, req.UpstreamRequestID)
}

func nullableInt32Equals(value sql.NullInt32, expected int) bool {
	return value.Valid == (expected != 0) && (!value.Valid || int(value.Int32) == expected)
}

func nullableStringEquals(value sql.NullString, expected string) bool {
	return value.Valid == (expected != "") && (!value.Valid || value.String == expected)
}

func markProviderAttemptFundingPendingTx(ctx context.Context, tx pgx.Tx, req SettleRequest, calculatedCost int) error {
	return updateProviderAttemptFinancialStateTx(
		ctx, tx, req, providerAttemptStateFundingPending, calculatedCost, 0, 0,
	)
}

func markProviderAttemptSettledTx(ctx context.Context, tx pgx.Tx, req SettleRequest, calculatedCost, chargedCost int, transactionID int64) error {
	return updateProviderAttemptFinancialStateTx(
		ctx, tx, req, providerAttemptStateSettled, calculatedCost, chargedCost, transactionID,
	)
}

func updateProviderAttemptFinancialStateTx(
	ctx context.Context,
	tx pgx.Tx,
	req SettleRequest,
	state string,
	calculatedCost int,
	chargedCost int,
	transactionID int64,
) error {
	result, err := tx.Exec(ctx,
		`UPDATE billing_provider_attempts
		 SET state = $4::varchar(32),
		     outcome_certainty = $5::varchar(24),
		     retry_authorized = FALSE,
		     calculated_cost = $6,
		     charged_cost = $7,
		     terminal_transaction_id = NULLIF($8::bigint, 0),
		     financial_resolved_at = CASE WHEN $4::text = 'settled' THEN COALESCE(financial_resolved_at, NOW()) ELSE NULL END,
		     updated_at = NOW()
		 WHERE org_id = $1 AND request_id = $2
		   AND ($3::text = '' OR attempt_id = $3::text)
		   AND state IN (
		       'dispatch_started', 'response_accepted', 'outcome_unknown',
		       'response_unusable', 'funding_pending'
		   )`,
		req.OrgID, req.RequestID, req.AttemptID, state, providerAttemptCertaintyKnownUsage,
		calculatedCost, chargedCost, transactionID,
	)
	if err != nil {
		return fmt.Errorf("mark provider attempt %s: %w", state, err)
	}
	rows := result.RowsAffected()
	if req.AttemptID != "" && rows != 1 {
		return fmt.Errorf("mark provider attempt %s: %w", state, ErrProviderAttemptConflict)
	}
	if req.AttemptID == "" && rows > 1 {
		return fmt.Errorf("mark provider attempt %s: multiple unresolved attempts", state)
	}
	return nil
}

// holdUnresolvedProviderAttemptTx prevents generic cleanup paths from
// refunding a request after a possible provider-side charge. The reservation
// lock is already held, so the attempt check and hold transition are atomic.
func holdUnresolvedProviderAttemptTx(ctx context.Context, tx pgx.Tx, requestID string, orgID int64) (bool, error) {
	var state string
	err := tx.QueryRow(ctx,
		`SELECT state
		 FROM billing_provider_attempts
		 WHERE org_id = $1 AND request_id = $2
		   AND state IN (
		       'dispatch_started', 'response_accepted', 'outcome_unknown',
		       'response_unusable', 'funding_pending'
		   )
		 ORDER BY CASE state
		     WHEN 'outcome_unknown' THEN 1
		     WHEN 'response_unusable' THEN 2
		     ELSE 3
		 END, started_at
		 LIMIT 1
		 FOR UPDATE`, orgID, requestID,
	).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock unresolved provider attempt: %w", err)
	}
	reason := pendingReasonProviderAttemptUnresolved
	if state == ProviderAttemptOutcomeUnknown || state == providerAttemptStateDispatchStarted {
		reason = pendingReasonAmbiguousAttempt
	} else if state == ProviderAttemptOutcomeResponseUnusable {
		reason = pendingReasonResponseUnusable
	}
	result, err := tx.Exec(ctx,
		`UPDATE freeze_idempotency
		 SET status = 'settlement_pending',
		     provider_cost_pending_at = COALESCE(provider_cost_pending_at, NOW()),
		     settlement_pending_reason = $3,
		     settlement_claimed_until = NULL,
		     settlement_next_attempt_at = NULL,
		     updated_at = NOW()
		 WHERE org_id = $1 AND request_id = $2 AND status = 'frozen'`,
		orgID, requestID, reason,
	)
	if err != nil {
		return false, fmt.Errorf("persist provider-attempt hold: %w", err)
	}
	if result.RowsAffected() != 1 {
		return false, fmt.Errorf("persist provider-attempt hold: reservation state changed")
	}
	return true, nil
}
