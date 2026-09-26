package contentcapture

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrReservationInvalid      = errors.New("invalid content reservation")
	ErrReservationTransaction  = errors.New("content reservation requires an independent connection pool")
	ErrReservationScope        = errors.New("content reservation stream source or recovery epoch mismatch")
	ErrReservationUnconfigured = errors.New("content reservation stream is not configured")
	ErrReservationQuota        = errors.New("content reservation quota exhausted")
	ErrReservationConflict     = errors.New("content representation is already reserved")
)

type ReservationRequest struct {
	OrgID, ProjectID           int64
	ContentRef, SourceFactID   string
	Revision                   int64
	ExpectedPolicyVersion      string
	ExpectedRecoveryEpoch      int64
	Protocol, ObservationPoint string
	MaxSavedBytes              int64
	LeaseOwner                 string
	LeaseTTL                   time.Duration
	// CreateStream explicitly permits the capture owner to initialize this
	// project's stream. Pre-commit failures roll the initialization back.
	CreateStream bool
}

// Reservation is an internal capture binding, not a body receipt or read grant.
// ObjectName is a random placeholder; no corresponding file has been created.
// RecoveryEpoch is the allocation snapshot; the stream remains its authority.
// Effective must stay attached to this capture, even after a later policy PUT.
type Reservation struct {
	OrgID, ProjectID          int64
	ContentRef, SourceFactID  string
	Revision, RecoveryEpoch   int64
	Effective                 PolicyVersion
	EffectiveAt               string
	Binding                   StoreBinding
	ObjectName                string
	ReservedBytes             int64
	LeaseOwner                string
	LeaseExpiresAt, ExpiresAt time.Time
}

// Reserve owns a new top-level pool transaction. There is no caller-Tx API and
// a pgx.Tx supplied as the PolicyStore database is rejected, not used to open a
// savepoint. Callers must finish financial transactions before invoking Reserve;
// the pool type alone cannot prove that they have done so.
//
// This primitive requires internal capture authority from its caller. It does
// not authenticate end users or create policy, scope, keys, ready objects, or
// manifests. It performs no file writes, releases, retries, or reconciliation.
// After success the allocation remains charged until a separate owner reconciles
// it, including when a later file write fails or the lease expires. A commit
// transport error may leave the result unknown; reconcile the exact ContentRef
// before deciding what to do next. This method never retries an ambiguous commit.
func (s *PolicyStore) Reserve(ctx context.Context, files *FileStore, input ReservationRequest) (Reservation, error) {
	var result Reservation
	if err := validateReservationRequest(ctx, input); err != nil {
		return result, err
	}
	if err := s.validateTarget(input.OrgID, input.ProjectID, "ready"); err != nil {
		return result, err
	}
	if s.options.DefaultMode == ModeDisabled || !s.options.FullContentEnabled {
		return result, ErrPolicyInactive
	}
	// This bounded marker read is deliberately outside all database transactions.
	if err := files.verifyBinding(); err != nil {
		return result, err
	}
	binding := files.binding
	pool, ok := s.db.(*pgxpool.Pool)
	if !ok || pool == nil {
		return result, ErrReservationTransaction
	}
	objectName, err := randomStoreName()
	if err != nil {
		return result, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	if input.CreateStream {
		_, err = tx.Exec(ctx, `INSERT INTO execution_content_streams(org_id,project_id,source_instance) VALUES($1,$2,$3) ON CONFLICT(org_id,project_id) DO NOTHING`, input.OrgID, input.ProjectID, s.options.SourceInstance)
		if err != nil {
			return result, err
		}
	}
	// Global lock order: stream -> policy -> object. No file operations below.
	var source string
	var epoch int64
	err = tx.QueryRow(ctx, `SELECT source_instance,recovery_epoch FROM execution_content_streams WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, input.OrgID, input.ProjectID).Scan(&source, &epoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrReservationUnconfigured
	}
	if err != nil {
		return result, err
	}
	if source != s.options.SourceInstance || epoch != input.ExpectedRecoveryEpoch {
		return result, ErrReservationScope
	}
	var row storedPolicyRow
	var reserved, stored int64
	var now time.Time
	err = tx.QueryRow(ctx, `SELECT desired_version,effective_version,desired_config,effective_config,effective_at,application_status,application_reason,reserved_bytes,stored_bytes
FROM execution_content_policies WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, input.OrgID, input.ProjectID).Scan(&row.desiredVersion, &row.effectiveVersion, &row.desiredConfig, &row.effectiveConfig, &row.effectiveAt, &row.applicationStatus, &row.applicationReason, &reserved, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrPolicyInactive
	}
	if err != nil {
		return result, err
	}
	view, err := s.rowView(input.OrgID, input.ProjectID, "ready", row)
	if err != nil {
		return result, err
	}
	config := view.Effective.Config
	if config.Mode != ModeFullContent {
		return result, ErrPolicyInactive
	}
	if err := s.validateSupportedConfig(config); err != nil {
		return result, fmt.Errorf("%w: unsupported effective policy", ErrPolicyInactive)
	}
	if input.ExpectedPolicyVersion != view.Effective.PolicyVersion {
		return result, ErrPolicyVersionConflict
	}
	if !slices.Contains(config.SupportedProtocols, input.Protocol) || !slices.Contains(config.ObservationPoints, input.ObservationPoint) {
		return result, ErrPolicyInactive
	}
	if input.MaxSavedBytes > *config.MaxSavedBytes || input.MaxSavedBytes > s.options.Limits.MaxSavedBytes {
		return result, ErrReservationInvalid
	}
	var exists bool
	err = tx.QueryRow(ctx, `SELECT true FROM execution_content_objects WHERE content_ref=$1 OR (org_id=$2 AND project_id=$3 AND source_fact_id=$4 AND revision=$5) ORDER BY content_ref LIMIT 1 FOR UPDATE`, input.ContentRef, input.OrgID, input.ProjectID, input.SourceFactID, input.Revision).Scan(&exists)
	if err == nil {
		return result, ErrReservationConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	// Expiry removes the ready object, never its immutable manifest. Preserve
	// duplicate-fact protection after reclamation without charging a new slot.
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution_content_manifests WHERE org_id=$1 AND project_id=$2 AND source_fact_id=$3 AND revision=$4)`, input.OrgID, input.ProjectID, input.SourceFactID, input.Revision).Scan(&exists)
	if err != nil {
		return result, err
	}
	if exists {
		return result, ErrReservationConflict
	}
	// Read the server clock after lock waits, so contention does not consume
	// the lease before this allocation has even been established.
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return result, err
	}
	leaseEnd, expires, err := reservationTimes(now, input.LeaseTTL, *config.RetentionSeconds)
	if err != nil {
		return result, err
	}
	if !reservationFitsQuota(reserved, stored, input.MaxSavedBytes, *config.QuotaBytes) {
		return result, ErrReservationQuota
	}
	policyVersion, _ := strconv.ParseInt(view.Effective.PolicyVersion, 10, 64)
	policyConfig, err := encodeStoredPolicyConfig(view.Effective.Config)
	if err != nil {
		return result, err
	}
	policyEffectiveAt, err := time.Parse(time.RFC3339Nano, view.EffectiveAt)
	if err != nil {
		return result, err
	}
	// The subtraction above proves this addition cannot overflow. Updating before
	// insertion also makes insertion/constraint failures exercise atomic rollback.
	tag, err := tx.Exec(ctx, `UPDATE execution_content_policies SET reserved_bytes=reserved_bytes+$3 WHERE org_id=$1 AND project_id=$2 AND effective_version=$4 AND reserved_bytes=$5 AND stored_bytes=$6`, input.OrgID, input.ProjectID, input.MaxSavedBytes, policyVersion, reserved, stored)
	if err != nil {
		return result, err
	}
	if tag.RowsAffected() != 1 {
		return result, ErrPolicyVersionConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO execution_content_objects(content_ref,org_id,project_id,source_fact_id,revision,policy_version,recovery_epoch,storage_identity,storage_binding_token,relative_path,state,reserved_bytes,saved_bytes,expires_at,lease_owner,lease_expires_at,policy_config,policy_effective_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'reserved',$11,0,$12,$13,$14,$15,$16)`, input.ContentRef, input.OrgID, input.ProjectID, input.SourceFactID, input.Revision, policyVersion, epoch, binding.Identity, binding.Token, objectName, input.MaxSavedBytes, expires, input.LeaseOwner, leaseEnd, policyConfig, policyEffectiveAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return result, ErrReservationConflict
		}
		return result, err
	}
	if err := tx.Commit(ctx); err != nil {
		return result, err
	}
	return Reservation{OrgID: input.OrgID, ProjectID: input.ProjectID, ContentRef: input.ContentRef, SourceFactID: input.SourceFactID, Revision: input.Revision, RecoveryEpoch: epoch,
		Effective: view.Effective, EffectiveAt: view.EffectiveAt, Binding: binding, ObjectName: objectName, ReservedBytes: input.MaxSavedBytes,
		LeaseOwner: input.LeaseOwner, LeaseExpiresAt: leaseEnd, ExpiresAt: expires}, nil
}

func validateReservationRequest(ctx context.Context, r ReservationRequest) error {
	if ctx == nil {
		return ErrReservationInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	version, err := strconv.ParseInt(r.ExpectedPolicyVersion, 10, 64)
	if err != nil || version <= 0 || strconv.FormatInt(version, 10) != r.ExpectedPolicyVersion || r.OrgID <= 0 || r.ProjectID <= 0 || r.Revision <= 0 || r.ExpectedRecoveryEpoch <= 0 || r.MaxSavedBytes <= 0 || r.MaxSavedBytes > maxPolicyInteger ||
		!strings.HasPrefix(r.ContentRef, "uc1_") || !validStoreName(strings.TrimPrefix(r.ContentRef, "uc1_")) ||
		!strings.HasPrefix(r.SourceFactID, "uf1_") || !validStoreName(strings.TrimPrefix(r.SourceFactID, "uf1_")) ||
		!policyText(r.LeaseOwner) || len(r.LeaseOwner) > 128 || r.LeaseTTL < time.Microsecond || r.Protocol == "" || r.ObservationPoint == "" {
		return ErrReservationInvalid
	}
	return nil
}

func reservationFitsQuota(reserved, stored, amount, quota int64) bool {
	return reserved >= 0 && stored >= 0 && amount > 0 && quota > 0 && reserved <= quota && stored <= quota-reserved && amount <= quota-reserved-stored
}

func reservationTimes(now time.Time, ttl time.Duration, retentionSeconds int64) (time.Time, time.Time, error) {
	if now.IsZero() || retentionSeconds <= 0 || retentionSeconds > math.MaxInt64/int64(time.Second) || ttl < time.Microsecond || ttl > time.Duration(retentionSeconds)*time.Second {
		return time.Time{}, time.Time{}, ErrReservationInvalid
	}
	// Match PostgreSQL timestamp precision without extending the requested lease.
	now = now.UTC().Truncate(time.Microsecond)
	lease := now.Add(ttl.Truncate(time.Microsecond))
	expires := now.Add(time.Duration(retentionSeconds) * time.Second)
	if expires.Year() > 9999 || !lease.After(now) || lease.After(expires) {
		return time.Time{}, time.Time{}, ErrReservationInvalid
	}
	return lease, expires, nil
}
