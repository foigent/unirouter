package sourcefacts

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"strconv"
	"time"
)

type ContentExpiryResult struct {
	Examined, Reclaimed, Failed int
	ReleasedBytes               int64
}

type expiredContentObject struct {
	ref             string
	org, project    int64
	fact            string
	revision, epoch int64
	receipt         contentcapture.StoredReceipt
	expires         time.Time
	snapshot        string
}

// ReclaimExpiredContent owns bounded, independent transactions. It never
// handles reserved/orphan objects, shortens retention, edits manifests or
// retries ambiguous commits. Files are removed outside all database locks.
func (s *Service) ReclaimExpiredContent(ctx context.Context, files *contentcapture.FileStore, limit int) (ContentExpiryResult, error) {
	var result ContentExpiryResult
	if !s.ContentEnabled() {
		return result, ErrContentJournalDisabled
	}
	pool, ok := s.db.(*pgxpool.Pool)
	if !ok || pool == nil {
		return result, ErrContentJournalTransaction
	}
	if ctx == nil || limit < 1 || limit > 1000 {
		return result, ErrContentPublishBinding
	}
	if err := files.CheckReady(ctx); err != nil {
		return result, err
	}
	rows, err := pool.Query(ctx, `SELECT o.content_ref,o.org_id,o.project_id FROM execution_content_objects o JOIN execution_content_streams s USING(org_id,project_id) WHERE o.state='ready' AND o.expires_at<=clock_timestamp() AND s.source_instance=$1 ORDER BY o.expires_at,o.content_ref LIMIT $2`, s.SourceInstance(), limit)
	if err != nil {
		return result, err
	}
	var candidates []expiredContentObject
	for rows.Next() {
		var object expiredContentObject
		if err = rows.Scan(&object.ref, &object.org, &object.project); err != nil {
			rows.Close()
			return result, err
		}
		candidates = append(candidates, object)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	var firstErr error
	for _, candidate := range candidates {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		result.Examined++
		saved, removed, e := s.reclaimExpiredContentObject(ctx, pool, files, candidate)
		if e != nil {
			result.Failed++
			if firstErr == nil {
				firstErr = e
			}
			continue
		}
		if removed {
			result.Reclaimed++
			result.ReleasedBytes += saved
		}
	}
	return result, firstErr
}

func (s *Service) reclaimExpiredContentObject(ctx context.Context, pool *pgxpool.Pool, files *contentcapture.FileStore, candidate expiredContentObject) (int64, bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(context.Background())
	object, _, found, err := s.lockExpiredContentObject(ctx, tx, candidate)
	if err != nil || !found {
		return 0, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, false, err
	}
	if err = files.RemoveExpired(ctx, object.receipt); err != nil {
		return 0, false, err
	}
	// A crash here leaves the ready row charged. A later ordinary batch repeats
	// the exact identity checks, confirms durable absence, and releases once.
	next, err := pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer next.Rollback(context.Background())
	current, stored, found, err := s.lockExpiredContentObject(ctx, next, candidate)
	if err != nil || !found {
		return 0, false, err
	}
	if current.snapshot != object.snapshot {
		return 0, false, ErrContentPublishBinding
	}
	tag, err := next.Exec(ctx, `DELETE FROM execution_content_objects WHERE content_ref=$1 AND org_id=$2 AND project_id=$3 AND source_fact_id=$4 AND revision=$5 AND state='ready'`, object.ref, object.org, object.project, object.fact, object.revision)
	if err != nil {
		return 0, false, err
	}
	if tag.RowsAffected() != 1 {
		return 0, false, ErrContentPublishBinding
	}
	tag, err = next.Exec(ctx, `UPDATE execution_content_policies SET stored_bytes=stored_bytes-$3 WHERE org_id=$1 AND project_id=$2 AND stored_bytes=$4 AND stored_bytes>=$3`, object.org, object.project, object.receipt.SavedBytes, stored)
	if err != nil {
		return 0, false, err
	}
	if tag.RowsAffected() != 1 {
		return 0, false, ErrContentPublishAccounting
	}
	if err = next.Commit(ctx); err != nil {
		return 0, false, err
	}
	return object.receipt.SavedBytes, true, nil
}

func (s *Service) lockExpiredContentObject(ctx context.Context, tx pgx.Tx, candidate expiredContentObject) (expiredContentObject, int64, bool, error) {
	object := candidate
	var source string
	err := tx.QueryRow(ctx, `SELECT source_instance FROM execution_content_streams WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, object.org, object.project).Scan(&source)
	if errors.Is(err, pgx.ErrNoRows) {
		return object, 0, false, nil
	}
	if err != nil {
		return object, 0, false, err
	}
	if source != s.SourceInstance() {
		return object, 0, false, ErrContentJournalScope
	}
	var stored int64
	err = tx.QueryRow(ctx, `SELECT stored_bytes FROM execution_content_policies WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, object.org, object.project).Scan(&stored)
	if err != nil {
		return object, 0, false, err
	}
	var state string
	// jsonb gives an exact persisted-row comparison between the short phases;
	// use UTC so pool connection timezones cannot change its timestamp spelling.
	if _, err = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); err != nil {
		return object, 0, false, err
	}
	err = tx.QueryRow(ctx, `SELECT source_fact_id,revision,recovery_epoch,storage_identity,storage_binding_token,relative_path,saved_bytes,COALESCE(digest,''),expires_at,state,to_jsonb(o)::text FROM execution_content_objects o WHERE content_ref=$1 AND org_id=$2 AND project_id=$3 FOR UPDATE`, object.ref, object.org, object.project).Scan(&object.fact, &object.revision, &object.epoch, &object.receipt.Binding.Identity, &object.receipt.Binding.Token, &object.receipt.ObjectName, &object.receipt.SavedBytes, &object.receipt.SHA256, &object.expires, &state, &object.snapshot)
	if errors.Is(err, pgx.ErrNoRows) {
		return object, 0, false, nil
	}
	if err != nil {
		return object, 0, false, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return object, 0, false, err
	}
	if state != "ready" || now.Before(object.expires) {
		return object, 0, false, nil
	}
	if object.receipt.SavedBytes < 0 || stored < object.receipt.SavedBytes {
		return object, 0, false, ErrContentPublishAccounting
	}
	var wire []byte
	var hash string
	err = tx.QueryRow(ctx, `SELECT document,source_hash FROM execution_content_manifests WHERE org_id=$1 AND project_id=$2 AND source_fact_id=$3 AND revision=$4`, object.org, object.project, object.fact, object.revision).Scan(&wire, &hash)
	if err != nil {
		return object, 0, false, err
	}
	manifest, err := DecodeContentManifest(wire)
	if err != nil || manifest.Validate() != nil || hash != eventconnector.HashSourceBytes(wire) || manifest.ContentRef != object.ref || manifest.SourceInstance != source || manifest.OrgID != strconv.FormatInt(object.org, 10) || manifest.ProjectID != strconv.FormatInt(object.project, 10) || manifest.SourceFactID != object.fact || manifest.Revision != strconv.FormatInt(object.revision, 10) || manifest.Representation == nil || manifest.Representation.SavedBytes != object.receipt.SavedBytes || manifest.Representation.Digest != object.receipt.SHA256 {
		return object, 0, false, ErrContentPublishBinding
	}
	expiry, e := time.Parse(time.RFC3339Nano, manifest.ExpiresAt)
	if e != nil || !expiry.Equal(object.expires) {
		return object, 0, false, ErrContentPublishBinding
	}

	// recovery_epoch is the allocation snapshot. Old published bodies still
	// expire after a stream recovery; do not require equality to current epoch.
	return object, stored, true, nil
}
