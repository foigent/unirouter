package sourcefacts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/contentcapture"
)

var (
	ErrContentPublishBinding    = errors.New("content publication does not match its persistent reservation")
	ErrContentPublishLease      = errors.New("content publication lease or retention expired")
	ErrContentPublishAccounting = errors.New("content publication accounting is inconsistent")
)

// PublishReservedContent requires internal capture authority from its caller.
// It verifies the durable file before opening its own top-level transaction, then
// atomically publishes ready state, accounting, sequence and immutable manifest.
// No file I/O occurs while DB locks are held. The caller must have finished any
// financial transaction; there is intentionally no exported caller-Tx variant.
//
// File failures and rolled-back publication leave the exact reserved name for
// explicit reconciliation. Commit errors are ambiguous and are never retried.
// Changing the current capture policy does not invalidate an allocation made
// under its persisted original snapshot. Recovery epoch changes do invalidate it.
func (s *Service) PublishReservedContent(ctx context.Context, files *contentcapture.FileStore, reservation contentcapture.Reservation, receipt contentcapture.StoredReceipt, manifest ContentManifest, limits contentcapture.StoreLimits) (ContentAppendResult, error) {
	var result ContentAppendResult
	if !s.ContentEnabled() {
		return result, ErrContentJournalDisabled
	}
	pool, ok := s.db.(*pgxpool.Pool)
	if !ok || pool == nil {
		return result, ErrContentJournalTransaction
	}
	if ctx == nil {
		return result, ErrContentPublishBinding
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if manifest.SourceInstance != s.SourceInstance() {
		return result, ErrContentJournalScope
	}
	wire, err := EncodeContentManifest(manifest)
	if err != nil {
		return result, err
	}
	if err = validateContentPublication(reservation, receipt, manifest); err != nil {
		return result, err
	}
	verified, err := files.OpenVerified(ctx, receipt, limits)
	if err != nil {
		return result, err
	}
	if err = verified.Close(); err != nil {
		return result, err
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	// All capture writers use stream -> policy -> object lock order.
	var source string
	var epoch int64
	err = tx.QueryRow(ctx, `SELECT source_instance,recovery_epoch FROM execution_content_streams WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, reservation.OrgID, reservation.ProjectID).Scan(&source, &epoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrContentJournalUnconfigured
	}
	if err != nil {
		return result, err
	}
	if source != manifest.SourceInstance {
		return result, ErrContentJournalScope
	}
	if epoch != reservation.RecoveryEpoch {
		return result, ErrContentEpochChanged
	}
	var reserved, stored int64
	err = tx.QueryRow(ctx, `SELECT reserved_bytes,stored_bytes FROM execution_content_policies WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, reservation.OrgID, reservation.ProjectID).Scan(&reserved, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrContentPublishBinding
	}
	if err != nil {
		return result, err
	}
	var row contentPublicationRow
	err = tx.QueryRow(ctx, `SELECT policy_version,policy_config,policy_effective_at,recovery_epoch,storage_identity,storage_binding_token,relative_path,state,reserved_bytes,saved_bytes,digest,expires_at,lease_owner,lease_expires_at
FROM execution_content_objects WHERE content_ref=$1 AND org_id=$2 AND project_id=$3 AND source_fact_id=$4 AND revision=$5 FOR UPDATE`, reservation.ContentRef, reservation.OrgID, reservation.ProjectID, reservation.SourceFactID, reservation.Revision).Scan(&row.policyVersion, &row.policyConfig, &row.policyEffectiveAt, &row.epoch, &row.binding.Identity, &row.binding.Token, &row.name, &row.state, &row.reserved, &row.saved, &row.digest, &row.expires, &row.leaseOwner, &row.leaseExpires)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrContentPublishBinding
	}
	if err != nil {
		return result, err
	}
	if err = row.validate(reservation, receipt); err != nil {
		return result, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return result, err
	}
	if !now.Before(row.expires) {
		return result, ErrContentPublishLease
	}
	var hasManifest bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution_content_manifests WHERE org_id=$1 AND project_id=$2 AND source_fact_id=$3 AND revision=$4)`, reservation.OrgID, reservation.ProjectID, reservation.SourceFactID, reservation.Revision).Scan(&hasManifest)
	if err != nil {
		return result, err
	}
	switch row.state {
	case "ready":
		// A replay cannot manufacture a missing manifest or charge the bytes again.
		if !hasManifest {
			return result, ErrContentPublishBinding
		}
	case "reserved":
		if hasManifest {
			return result, ErrContentPublishBinding
		}
		if !now.Before(row.leaseExpires) {
			return result, ErrContentPublishLease
		}
		newReserved, newStored, countErr := contentPublishedAccounting(reserved, stored, row.reserved, receipt.SavedBytes)
		if countErr != nil {
			return result, countErr
		}
		tag, updateErr := tx.Exec(ctx, `UPDATE execution_content_policies SET reserved_bytes=$3,stored_bytes=$4 WHERE org_id=$1 AND project_id=$2 AND reserved_bytes=$5 AND stored_bytes=$6`, reservation.OrgID, reservation.ProjectID, newReserved, newStored, reserved, stored)
		if updateErr != nil {
			return result, updateErr
		}
		if tag.RowsAffected() != 1 {
			return result, ErrContentPublishAccounting
		}
		// Keep the object's original reserved_bytes as immutable allocation evidence;
		// only the project counter relinquishes that reservation on this transition.
		tag, updateErr = tx.Exec(ctx, `UPDATE execution_content_objects SET state='ready',saved_bytes=$2,digest=$3 WHERE content_ref=$1 AND state='reserved'`, reservation.ContentRef, receipt.SavedBytes, receipt.SHA256)
		if updateErr != nil {
			return result, updateErr
		}
		if tag.RowsAffected() != 1 {
			return result, ErrContentPublishBinding
		}
	default:
		return result, ErrContentPublishBinding
	}
	result, err = s.appendContentManifestTx(ctx, tx, manifest, wire, epoch)
	if err != nil {
		return ContentAppendResult{}, err
	}
	if result.Replayed != (row.state == "ready") {
		return ContentAppendResult{}, ErrContentPublishBinding
	}
	if err = tx.Commit(ctx); err != nil {
		return ContentAppendResult{}, fmt.Errorf("content publication commit unconfirmed: %w", err)
	}
	return result, nil
}

func validateContentPublication(r contentcapture.Reservation, receipt contentcapture.StoredReceipt, m ContentManifest) error {
	if r.OrgID <= 0 || r.ProjectID <= 0 || r.Revision <= 0 || r.RecoveryEpoch <= 0 || r.ReservedBytes <= 0 || r.LeaseOwner == "" || r.LeaseExpiresAt.IsZero() || r.ExpiresAt.IsZero() || r.LeaseExpiresAt.After(r.ExpiresAt) ||
		m.OrgID != strconv.FormatInt(r.OrgID, 10) || m.ProjectID != strconv.FormatInt(r.ProjectID, 10) || m.SourceFactID != r.SourceFactID || m.Revision != strconv.FormatInt(r.Revision, 10) || m.ContentRef != r.ContentRef ||
		receipt.Binding != r.Binding || receipt.ObjectName != r.ObjectName || receipt.SavedBytes < 0 || receipt.SavedBytes > r.ReservedBytes || m.Representation == nil || m.Representation.SavedBytes != receipt.SavedBytes || m.Representation.Digest != receipt.SHA256 {
		return ErrContentPublishBinding
	}
	c := r.Effective.Config
	if c.Validate() != nil || c.Mode != contentcapture.ModeFullContent || r.ReservedBytes > *c.MaxSavedBytes ||
		!slices.Contains(c.SupportedProtocols, m.Protocol) || !slices.Contains(c.ObservationPoints, m.ObservationPoint) ||
		m.Capture.Mode != c.Mode || m.Capture.PolicyRef != fmt.Sprintf("content-policy:%d:%d", r.OrgID, r.ProjectID) || m.Capture.PolicyVersion != r.Effective.PolicyVersion || m.Capture.PolicyEffectiveAt != r.EffectiveAt {
		return ErrContentPublishBinding
	}
	expires, err := time.Parse(time.RFC3339Nano, m.ExpiresAt)
	if err != nil || !expires.Equal(r.ExpiresAt) {
		return ErrContentPublishBinding
	}
	return nil
}

type contentPublicationRow struct {
	policyVersion, epoch int64
	policyConfig         []byte
	policyEffectiveAt    time.Time
	binding              contentcapture.StoreBinding
	name, state          string
	reserved, saved      int64
	digest               *string
	expires              time.Time
	leaseOwner           string
	leaseExpires         time.Time
}

func (p contentPublicationRow) validate(r contentcapture.Reservation, receipt contentcapture.StoredReceipt) error {
	// The original policy is DB evidence, not whatever current effective policy
	// or an in-memory reservation happens to claim after a later policy change.
	if len(p.policyConfig) > 16384 {
		return ErrContentPublishBinding
	}
	c, err := contentcapture.DecodePolicyConfig(p.policyConfig)
	if err != nil {
		return ErrContentPublishBinding
	}
	actual, _ := json.Marshal(c)
	claimed, err := json.Marshal(r.Effective.Config)
	effective, timeErr := time.Parse(time.RFC3339Nano, r.EffectiveAt)
	if err != nil || timeErr != nil || !bytes.Equal(actual, claimed) || p.policyVersion <= 0 || strconv.FormatInt(p.policyVersion, 10) != r.Effective.PolicyVersion || !p.policyEffectiveAt.Equal(effective) ||
		p.epoch != r.RecoveryEpoch || p.binding != r.Binding || p.name != r.ObjectName || p.reserved != r.ReservedBytes || !p.expires.Equal(r.ExpiresAt) || p.leaseOwner != r.LeaseOwner || !p.leaseExpires.Equal(r.LeaseExpiresAt) {
		return ErrContentPublishBinding
	}
	if p.state == "ready" {
		if p.saved != receipt.SavedBytes || p.digest == nil || *p.digest != receipt.SHA256 {
			return ErrContentPublishBinding
		}
	} else if p.state != "reserved" || p.saved != 0 || p.digest != nil {
		return ErrContentPublishBinding
	}
	return nil
}

func contentPublishedAccounting(reserved, stored, allocation, saved int64) (int64, int64, error) {
	if reserved < 0 || stored < 0 || allocation <= 0 || allocation > reserved || saved < 0 || saved > allocation || reserved > math.MaxInt64-stored {
		return 0, 0, ErrContentPublishAccounting
	}
	// saved <= allocation <= reserved proves stored+saved cannot overflow and
	// the total occupied capacity can only decrease or remain unchanged.
	return reserved - allocation, stored + saved, nil
}
