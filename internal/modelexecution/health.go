package modelexecution

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executionevidence"
)

type healthAction uint8

const (
	actionRenewLease healthAction = iota + 1
	actionReleaseUndispatched
	actionSuccess
	actionProviderFailure
	actionCredentialAuthFailure
	actionCredentialRateLimited
)

type healthRequest struct {
	evidence     executionevidence.EvidenceV1
	evidenceHash string
	attemptKey   ProviderAttemptKey
	ownerHash    string
	token        string
	action       healthAction
	databaseTime time.Time
}

type breakerSnapshot struct {
	id                   int64
	connectionID         int64
	publicModelID        int64
	legacyChannelID      sql.NullInt64
	adminState           string
	configRevision       int64
	circuitState         string
	circuitRevision      int64
	failCount            int
	degradationThreshold int
	halfOpenUntil        *time.Time
	probeUntil           *time.Time
	probeToken           string
}

type credentialSnapshot struct {
	id            int64
	connectionID  int64
	legacyKeyID   sql.NullInt64
	status        string
	stateRevision int64
	failCount     int
	lastUsedAt    *time.Time
	cooldownUntil *time.Time
}

type breakerNext struct {
	circuitState    string
	circuitRevision int64
	failCount       int
	halfOpenUntil   *time.Time
	probeUntil      *time.Time
	probeToken      string
}

// RenewLease extends a live half-open ownership lease. It never changes a
// breaker revision and a stale or replaced token can never be renewed.
func (c *Coordinator) RenewLease(ctx context.Context, decision Decision) (HealthResult, error) {
	return c.applyHealth(ctx, decision, actionRenewLease)
}

// ReleaseUndispatched terminalizes an owned provider attempt that never
// reached the network. A live half-open lease is cleared opportunistically;
// route drift, administrative disable, or lease expiry cannot resurrect an
// undispatched financial liability.
func (c *Coordinator) ReleaseUndispatched(ctx context.Context, decision Decision) (HealthResult, error) {
	return c.applyHealth(ctx, decision, actionReleaseUndispatched)
}

// MarkSuccess records provider availability and credential use. A successful
// half-open probe heals the breaker; an ordinary healthy response only clears
// accumulated failures.
func (c *Coordinator) MarkSuccess(ctx context.Context, decision Decision) (HealthResult, error) {
	return c.applyHealth(ctx, decision, actionSuccess)
}

// MarkProviderFailure changes only breaker health. Provider availability must
// not invalidate or cool down a credential.
func (c *Coordinator) MarkProviderFailure(ctx context.Context, decision Decision) (HealthResult, error) {
	return c.applyHealth(ctx, decision, actionProviderFailure)
}

// MarkCredentialAuthFailure changes only credential cooldown state. If this
// attempt owns a probe lease, the same transaction releases that lease without
// treating the authentication rejection as provider unavailability.
func (c *Coordinator) MarkCredentialAuthFailure(ctx context.Context, decision Decision) (HealthResult, error) {
	return c.applyHealth(ctx, decision, actionCredentialAuthFailure)
}

// MarkCredentialRateLimited changes only credential cooldown state. Breaker
// fail counts and breaker revisions are deliberately preserved.
func (c *Coordinator) MarkCredentialRateLimited(ctx context.Context, decision Decision) (HealthResult, error) {
	return c.applyHealth(ctx, decision, actionCredentialRateLimited)
}

func (c *Coordinator) applyHealth(ctx context.Context, decision Decision, action healthAction) (HealthResult, error) {
	if c == nil || c.database == nil || c.ledger == nil {
		return HealthResult{}, ErrUnavailable
	}
	request, err := prepareHealthRequest(decision, action)
	if err != nil {
		return HealthResult{}, err
	}
	return runBoundedTransaction(ctx, func(operationCtx context.Context) (HealthResult, transactionRetry, error) {
		return c.applyHealthOnce(operationCtx, request)
	})
}

func prepareHealthRequest(decision Decision, action healthAction) (healthRequest, error) {
	if decision.Status != Applied || action < actionRenewLease || action > actionCredentialRateLimited {
		return healthRequest{}, fmt.Errorf("%w: health event requires an applied dispatch decision", ErrInvalid)
	}
	var evidence executionevidence.EvidenceV1
	var digest string
	var err error
	if decision.PublicEvidence != nil {
		if !reflect.DeepEqual(decision.Evidence, executionevidence.EvidenceV1{}) {
			return healthRequest{}, fmt.Errorf("%w: dispatch evidence versions are mutually exclusive", ErrInvalid)
		}
		public, normalizeErr := executionevidence.NormalizeV2ForDispatch(*decision.PublicEvidence)
		if normalizeErr != nil {
			return healthRequest{}, fmt.Errorf("%w: public dispatch evidence: %v", ErrInvalid, normalizeErr)
		}
		digest, err = executionevidence.DigestV2(public)
		if err == nil {
			evidence, err = public.RouteEvidenceV1()
		}
	} else {
		evidence, err = executionevidence.NormalizeForDispatch(decision.Evidence)
		if err == nil {
			digest, err = executionevidence.Digest(evidence)
		}
	}
	if err != nil || subtle.ConstantTimeCompare([]byte(digest), []byte(decision.EvidenceHash)) != 1 {
		return healthRequest{}, fmt.Errorf("%w: immutable dispatch evidence digest", ErrInvalid)
	}
	if evidence.RequiresProbe {
		if decision.ProbeLeaseToken == "" || len(decision.ProbeLeaseToken) > 80 ||
			subtle.ConstantTimeCompare([]byte(probeTokenHash(decision.ProbeLeaseToken)), []byte(evidence.ProbeLeaseTokenHash)) != 1 {
			return healthRequest{}, fmt.Errorf("%w: probe lease token", ErrInvalid)
		}
	} else if decision.ProbeLeaseToken != "" || evidence.ProbeLeaseTokenHash != "" {
		return healthRequest{}, fmt.Errorf("%w: healthy dispatch cannot carry a probe lease", ErrInvalid)
	}
	if action == actionRenewLease && !evidence.RequiresProbe {
		return healthRequest{}, fmt.Errorf("%w: lease renewal requires a half-open dispatch", ErrInvalid)
	}
	key := decision.ProviderAttemptKey
	if key.OrgID <= 0 || key.RequestID == "" || len(key.RequestID) > 64 ||
		key.AttemptID == "" || len(key.AttemptID) > 64 ||
		strings.ContainsAny(key.RequestID+key.AttemptID, "\x00\r\n") ||
		!validEphemeralToken(decision.DispatchOwnerToken) {
		return healthRequest{}, fmt.Errorf("%w: provider-attempt health owner", ErrInvalid)
	}
	return healthRequest{
		evidence: evidence, evidenceHash: digest, attemptKey: key,
		ownerHash: probeTokenHash(decision.DispatchOwnerToken),
		token:     decision.ProbeLeaseToken, action: action,
	}, nil
}

func (c *Coordinator) applyHealthOnce(ctx context.Context, request healthRequest) (HealthResult, transactionRetry, error) {
	tx, err := c.database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadWrite})
	if err != nil {
		return HealthResult{}, transactionRetryFor(err), fmt.Errorf("%w: begin health transition: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)

	if outcome := healthFenceOutcome(request.action); outcome != "" {
		fence, fenceErr := c.ledger.FenceProviderAttemptHealthTx(ctx, tx, billing.ProviderAttemptHealthFenceRequest{
			OrgID: request.attemptKey.OrgID, RequestID: request.attemptKey.RequestID,
			AttemptID: request.attemptKey.AttemptID, EvidenceHash: request.evidenceHash,
			DispatchOwnerHash: request.ownerHash, Outcome: outcome,
		})
		if errors.Is(fenceErr, billing.ErrProviderAttemptConflict) ||
			errors.Is(fenceErr, billing.ErrProviderAttemptHealthConflict) {
			return HealthResult{Status: Stale}, retryNone, nil
		}
		if fenceErr != nil {
			return HealthResult{}, transactionRetryFor(fenceErr), wrapDatabaseError("claim provider-attempt health fence", fenceErr)
		}
		if fence == billing.ProviderAttemptHealthReplay {
			var replayNow time.Time
			if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&replayNow); err != nil {
				return HealthResult{}, transactionRetryFor(err), wrapDatabaseError("read health replay clock", err)
			}
			// The exact attempt fence proves that the original health transaction
			// committed. No projection write is repeated in this transaction, so
			// the deferred rollback safely releases its validation lock.
			return HealthResult{Status: Applied, DatabaseTime: replayNow.UTC()}, retryNone, nil
		}
	} else {
		ownerErr := c.ledger.ValidateProviderAttemptOwnerTx(ctx, tx, billing.ProviderAttemptOwnerRequest{
			OrgID: request.attemptKey.OrgID, RequestID: request.attemptKey.RequestID,
			AttemptID: request.attemptKey.AttemptID, EvidenceHash: request.evidenceHash,
			DispatchOwnerHash: request.ownerHash,
		})
		if errors.Is(ownerErr, billing.ErrProviderAttemptConflict) ||
			errors.Is(ownerErr, billing.ErrProviderAttemptHealthConflict) {
			return HealthResult{Status: Stale}, retryNone, nil
		}
		if ownerErr != nil {
			return HealthResult{}, transactionRetryFor(ownerErr), wrapDatabaseError("validate provider-attempt lease owner", ownerErr)
		}
	}

	status, dbNow, err := c.applyModeHealth(ctx, tx, request)
	if err != nil {
		return HealthResult{}, transactionRetryFor(err), wrapDatabaseError("apply health transition", err)
	}
	if status != Applied {
		if dbNow.IsZero() {
			if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&dbNow); err != nil {
				return HealthResult{}, transactionRetryFor(err), wrapDatabaseError("read rejected health clock", err)
			}
			dbNow = dbNow.UTC()
		}
		return HealthResult{Status: status, DatabaseTime: dbNow}, retryNone, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return HealthResult{}, retryCommitAmbiguity, wrapDatabaseError("commit health transition", err)
	}
	return HealthResult{Status: Applied, DatabaseTime: dbNow}, retryNone, nil
}

func healthFenceOutcome(action healthAction) string {
	switch action {
	case actionReleaseUndispatched:
		return billing.ProviderAttemptHealthReleasedUndispatched
	case actionSuccess:
		return billing.ProviderAttemptHealthSuccess
	case actionProviderFailure:
		return billing.ProviderAttemptHealthProviderFailure
	case actionCredentialAuthFailure:
		return billing.ProviderAttemptHealthAuthFailure
	case actionCredentialRateLimited:
		return billing.ProviderAttemptHealthRateLimited
	default:
		return ""
	}
}

func (c *Coordinator) applyModeHealth(
	ctx context.Context,
	tx pgx.Tx,
	request healthRequest,
) (Status, time.Time, error) {
	if request.action == actionReleaseUndispatched {
		dbNow, err := releaseUndispatchedProjection(ctx, tx, request)
		return Applied, dbNow, err
	}
	var canonicalBreakers []breakerSnapshot
	var canonicalCredential *credentialSnapshot
	var legacyState *legacyHealthState
	var status Status
	var err error

	if request.evidence.AuthorityMode != executionevidence.ModeLegacy {
		status, err = lockCanonicalConnection(ctx, tx, request.evidence)
		if err != nil || status != Applied {
			return status, time.Time{}, err
		}
		canonicalBreakers, status, err = lockCanonicalBreakers(ctx, tx, request)
		if err != nil || status != Applied {
			return status, time.Time{}, err
		}
		if actionNeedsCredential(request.action) {
			canonicalCredential, status, err = lockCanonicalCredential(ctx, tx, request.evidence)
			if err != nil || status != Applied {
				return status, time.Time{}, err
			}
		}
	}

	if request.evidence.AuthorityMode != executionevidence.ModeCanonical {
		var canonicalBreaker *breakerSnapshot
		if request.evidence.AuthorityMode == executionevidence.ModeVerify {
			canonicalBreaker = &canonicalBreakers[0]
		}
		var locked legacyHealthState
		locked, status, err = lockLegacyHealth(
			ctx, tx, request, canonicalBreaker, canonicalCredential,
		)
		if err != nil || status != Applied {
			return status, time.Time{}, err
		}
		legacyState = &locked
	}

	var dbNow time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&dbNow); err != nil {
		return "", time.Time{}, err
	}
	dbNow = dbNow.UTC()
	request.databaseTime = dbNow

	if request.evidence.AuthorityMode != executionevidence.ModeLegacy {
		status, err = c.applyCanonicalHealth(ctx, tx, request, dbNow, canonicalBreakers, canonicalCredential)
		if err != nil || status != Applied {
			return status, dbNow, err
		}
	}
	if legacyState != nil {
		status, err = c.applyLockedLegacyHealth(ctx, tx, request, dbNow, *legacyState)
		if err != nil || status != Applied {
			return status, dbNow, err
		}
	}
	return Applied, dbNow, nil
}

func actionNeedsCredential(action healthAction) bool {
	return action == actionSuccess || action == actionCredentialAuthFailure || action == actionCredentialRateLimited
}

func actionChangesBreaker(action healthAction) bool {
	return action == actionSuccess || action == actionProviderFailure
}
