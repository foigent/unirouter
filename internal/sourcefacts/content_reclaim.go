package sourcefacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/installstate"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

var ErrUnpublishedReclaim = errors.New("offline unpublished content reclamation refused")

// Offline evidence is a required operator assertion, not a lease/epoch test.
// The operator must stop EVERY process sharing the DB/root (including readers,
// capture/media and maintenance) and prevent restarts until this call finishes.
// Database runtime rows are an additional veto, never proof that OS owners exited.
type UnpublishedReclaimRequest struct {
	Target                               installstate.Target
	ExpectedLedger                       []string
	SourceInstance                       string
	OrgID, ProjectID                     int64
	ContentRef                           string
	AllOwnersStopped, RestartsProhibited bool
	OfflineEvidenceSHA256                string
	CheckOnly                            bool
}
type UnpublishedReclaimResult struct {
	Status                string                              `json:"status"`
	ContentRef            string                              `json:"content_ref"`
	RelativePath          string                              `json:"relative_path"`
	ReservedBytes         int64                               `json:"reserved_bytes"`
	ReleasedBytes         int64                               `json:"released_bytes"`
	Files                 contentcapture.UnpublishedFileState `json:"files"`
	OfflineEvidenceSHA256 string                              `json:"offline_evidence_sha256"`
}
type unpublishedObject struct {
	binding                          contentcapture.StoreBinding
	path, fact, immutable, history   string
	revision, epoch, reserved, saved int64
	reclaimed                        *time.Time
}

func validateUnpublishedRequest(r UnpublishedReclaimRequest) error {
	if !r.AllOwnersStopped || !r.RestartsProhibited || !validHash(r.OfflineEvidenceSHA256) || !contentText(r.SourceInstance, 128) || r.OrgID <= 0 || r.ProjectID <= 0 || !contentRefPattern.MatchString(r.ContentRef) || r.Target.Database == "" || r.Target.DatabaseOID <= 0 || r.Target.SystemIdentifier == "" || r.Target.Role == "" || r.Target.Owner == "" || r.Target.Environment == "" {
		return ErrUnpublishedReclaim
	}
	if schemaauthority.VerifyLedger(r.ExpectedLedger, r.ExpectedLedger) != nil || schemaauthority.VerifyRuntimeLedger(r.ExpectedLedger, r.ExpectedLedger) != nil {
		return ErrUnpublishedReclaim
	}
	return nil
}

// ReclaimUnpublishedContent is deliberately disconnected from online maintenance.
// It selects one durable reservation; no scans, automatic publication or history
// changes. On an ambiguous DB commit the caller re-enters with the SAME identity.
func ReclaimUnpublishedContent(ctx context.Context, pool *pgxpool.Pool, files *contentcapture.FileStore, r UnpublishedReclaimRequest) (UnpublishedReclaimResult, error) {
	result := UnpublishedReclaimResult{ContentRef: r.ContentRef, OfflineEvidenceSHA256: r.OfflineEvidenceSHA256}
	if ctx == nil || pool == nil || files == nil {
		return result, ErrUnpublishedReclaim
	}
	if err := validateUnpublishedRequest(r); err != nil {
		return result, err
	}
	if err := files.CheckReady(ctx); err != nil {
		return result, err
	}
	opts := pgx.TxOptions{}
	if r.CheckOnly {
		opts = pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	}
	tx, err := pool.BeginTx(ctx, opts)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	object, err := readUnpublishedObject(ctx, tx, r, !r.CheckOnly)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	result.RelativePath = object.path
	result.ReservedBytes = object.reserved
	result.Files, err = files.InspectUnpublished(ctx, object.binding, object.path, object.reserved)
	if err != nil {
		return result, err
	}
	if object.reclaimed != nil {
		if result.Files.FinalPresent || result.Files.TempPresent {
			return result, ErrUnpublishedReclaim
		}
		result.Status = "already_reclaimed"
		return result, nil
	}
	if r.CheckOnly {
		result.Status = "eligible"
		return result, nil
	}
	if err = files.RemoveUnpublished(ctx, object.binding, object.path, object.reserved); err != nil {
		return result, err
	}
	next, err := pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer next.Rollback(context.Background())
	current, err := readUnpublishedObject(ctx, next, r, true)
	if err != nil {
		return result, err
	}
	if current.immutable != object.immutable || current.history != object.history {
		return result, ErrUnpublishedReclaim
	}
	if current.reclaimed != nil {
		result.Status = "already_reclaimed"
		return result, next.Commit(ctx)
	}
	tag, err := next.Exec(ctx, `UPDATE execution_content_objects SET state='orphan',reclaimed_at=clock_timestamp() WHERE content_ref=$1 AND state='reserved' AND reclaimed_at IS NULL`, r.ContentRef)
	if err != nil {
		return result, err
	}
	if tag.RowsAffected() != 1 {
		return result, ErrUnpublishedReclaim
	}
	tag, err = next.Exec(ctx, `UPDATE execution_content_policies SET reserved_bytes=reserved_bytes-$3 WHERE org_id=$1 AND project_id=$2 AND reserved_bytes>=$3`, r.OrgID, r.ProjectID, object.reserved)
	if err != nil {
		return result, err
	}
	if tag.RowsAffected() != 1 {
		return result, ErrContentPublishAccounting
	}
	if err = next.Commit(ctx); err != nil {
		return result, err
	}
	result.Status = "reclaimed"
	result.ReleasedBytes = object.reserved
	return result, nil
}

func readUnpublishedObject(ctx context.Context, tx pgx.Tx, r UnpublishedReclaimRequest, lock bool) (unpublishedObject, error) {
	var o unpublishedObject
	target, err := installstate.ReadTarget(ctx, tx, r.Target.Environment)
	if err != nil {
		return o, err
	}
	if target != r.Target {
		return o, ErrUnpublishedReclaim
	}
	var ledger []string
	if err = tx.QueryRow(ctx, `SELECT array_agg(version::text ORDER BY version) FROM schema_migrations`).Scan(&ledger); err != nil {
		return o, err
	}
	if schemaauthority.VerifyLedger(r.ExpectedLedger, ledger) != nil {
		return o, ErrUnpublishedReclaim
	}
	var owners bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runtime_generations WHERE state<>'stopped')`).Scan(&owners); err != nil {
		return o, err
	}
	if owners {
		return o, ErrUnpublishedReclaim
	}
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	var source string
	var retained int64
	if err = tx.QueryRow(ctx, `SELECT source_instance,retained_after_sequence FROM execution_content_streams WHERE org_id=$1 AND project_id=$2`+suffix, r.OrgID, r.ProjectID).Scan(&source, &retained); err != nil {
		return o, err
	}
	// Missing retained revisions cannot prove that an earlier publication never existed.
	if source != r.SourceInstance || retained != 0 {
		return o, ErrUnpublishedReclaim
	}
	var charged int64
	if err = tx.QueryRow(ctx, `SELECT reserved_bytes FROM execution_content_policies WHERE org_id=$1 AND project_id=$2`+suffix, r.OrgID, r.ProjectID).Scan(&charged); err != nil {
		return o, err
	}
	if _, err = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); err != nil {
		return o, err
	}
	var state string
	var digest *string
	var config []byte
	var version int64
	err = tx.QueryRow(ctx, `SELECT source_fact_id,revision,recovery_epoch,storage_identity,storage_binding_token,relative_path,reserved_bytes,saved_bytes,state,digest,reclaimed_at,policy_config,policy_version,(to_jsonb(o)-'state'-'reclaimed_at')::text FROM execution_content_objects o WHERE content_ref=$1 AND org_id=$2 AND project_id=$3`+suffix, r.ContentRef, r.OrgID, r.ProjectID).Scan(&o.fact, &o.revision, &o.epoch, &o.binding.Identity, &o.binding.Token, &o.path, &o.reserved, &o.saved, &state, &digest, &o.reclaimed, &config, &version, &o.immutable)
	if err != nil {
		return o, err
	}
	if !contentFactPattern.MatchString(o.fact) || o.revision <= 0 || o.epoch <= 0 || o.reserved <= 0 || o.saved != 0 || digest != nil || version <= 0 {
		return o, ErrUnpublishedReclaim
	}
	if (o.reclaimed == nil && state != "reserved") || (o.reclaimed != nil && state != "orphan") {
		return o, ErrUnpublishedReclaim
	}
	policy, err := contentcapture.DecodePolicyConfig(config)
	if err != nil || policy.Validate() != nil {
		return o, ErrUnpublishedReclaim
	}
	var duplicates int64
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM execution_content_objects WHERE storage_identity=$1 AND relative_path=$2`, o.binding.Identity, o.path).Scan(&duplicates); err != nil {
		return o, err
	}
	if duplicates != 1 {
		return o, ErrUnpublishedReclaim
	}
	var accounting bool
	if err = tx.QueryRow(ctx, `SELECT $3::numeric=COALESCE(sum(reserved_bytes),0) FROM execution_content_objects WHERE org_id=$1 AND project_id=$2 AND state IN ('reserved','orphan') AND reclaimed_at IS NULL`, r.OrgID, r.ProjectID, charged).Scan(&accounting); err != nil {
		return o, err
	}
	if !accounting || (o.reclaimed == nil && charged < o.reserved) {
		return o, ErrContentPublishAccounting
	}
	// A ref copied into another fact/scope is contradictory evidence, not a safe
	// orphan. This exact reference predicate does not enumerate filesystem names.
	var referenced bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution_content_manifests WHERE convert_from(document,'UTF8')::jsonb->>'content_ref'=$1)`, r.ContentRef).Scan(&referenced); err != nil {
		return o, err
	}
	if referenced {
		return o, ErrUnpublishedReclaim
	}
	rows, err := tx.Query(ctx, `SELECT revision,sequence,event_id,request_id,document,source_hash FROM execution_content_manifests WHERE org_id=$1 AND project_id=$2 AND source_fact_id=$3 ORDER BY revision`, r.OrgID, r.ProjectID, o.fact)
	if err != nil {
		return o, err
	}
	defer rows.Close()
	var previous *ContentManifest
	var lastSequence int64
	hash := sha256.New()
	for rows.Next() {
		var revision, sequence int64
		var event, request, sourceHash string
		var wire []byte
		if err = rows.Scan(&revision, &sequence, &event, &request, &wire, &sourceHash); err != nil {
			return o, err
		}
		m, e := unpublishedManifest(wire, sourceHash, r, o.fact, revision, previous)
		if e != nil || sequence <= lastSequence || event != m.EventID || request != m.RequestID {
			return o, ErrUnpublishedReclaim
		}
		fmt.Fprintf(hash, "%d:%s\n", sequence, sourceHash)
		previous = &m
		lastSequence = sequence
	}
	if err = rows.Err(); err != nil {
		return o, err
	}
	o.history = hex.EncodeToString(hash.Sum(nil))
	return o, nil
}

func unpublishedManifest(wire []byte, hash string, r UnpublishedReclaimRequest, fact string, revision int64, previous *ContentManifest) (ContentManifest, error) {
	m, err := DecodeContentManifest(wire)
	if err != nil || hash != eventconnector.HashSourceBytes(wire) || m.SourceInstance != r.SourceInstance || m.OrgID != strconv.FormatInt(r.OrgID, 10) || m.ProjectID != strconv.FormatInt(r.ProjectID, 10) || m.SourceFactID != fact || m.Revision != strconv.FormatInt(revision, 10) || m.ContentRef != "" || m.Representation != nil {
		return m, ErrUnpublishedReclaim
	}
	if previous == nil {
		if revision != 1 {
			return m, ErrUnpublishedReclaim
		}
	} else if ValidateContentPredecessor(m, *previous) != nil {
		return m, ErrUnpublishedReclaim
	}
	return m, nil
}
