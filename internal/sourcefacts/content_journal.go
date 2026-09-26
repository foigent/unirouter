package sourcefacts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/eventconnector"
)

var (
	ErrContentJournalDisabled     = errors.New("content journal is disabled")
	ErrContentJournalUnconfigured = errors.New("content stream is not configured")
	ErrContentJournalScope        = errors.New("content journal scope mismatch")
	ErrContentJournalRevision     = errors.New("content manifest revision conflict")
	ErrContentJournalCorrupt      = errors.New("stored content manifest is corrupt")
	ErrContentObjectNotReady      = errors.New("content representation has no matching ready object")
	ErrContentJournalTransaction  = errors.New("content journal requires an independent connection pool")
)

type ContentAppendResult struct {
	Sequence      int64
	RecoveryEpoch int64
	SourceSHA256  string
	Replayed      bool
}

// AppendContentManifest owns a new, short transaction on the configured pool.
// There is deliberately no Tx variant: callers must finish financial transactions
// before invoking it. A pgx.Tx supplied through New's generic query interface is
// rejected, rather than using its Begin method to create a financial savepoint.
//
// The capture owner freezes policy and epoch when capture starts, establishes the
// stream explicitly, and durably writes the object before marking its DB row ready.
// This method creates neither capture policy nor read authority, and performs no
// file or provider I/O. A replay describes immutable history, not current body
// availability: expiration or loss after publication cannot rewrite that history.
func (s *Service) AppendContentManifest(ctx context.Context, m ContentManifest, expectedRecoveryEpoch int64) (ContentAppendResult, error) {
	var result ContentAppendResult
	if !s.ContentEnabled() {
		return result, ErrContentJournalDisabled
	}
	if m.SourceInstance != s.options.SourceInstance || expectedRecoveryEpoch <= 0 {
		return result, ErrContentJournalScope
	}
	wire, err := EncodeContentManifest(m)
	if err != nil {
		return result, err
	}
	pool, ok := s.db.(*pgxpool.Pool)
	if !ok || pool == nil {
		return result, ErrContentJournalTransaction
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.Background())
	result, err = s.appendContentManifestTx(ctx, tx, m, wire, expectedRecoveryEpoch)
	if err != nil {
		return ContentAppendResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ContentAppendResult{}, fmt.Errorf("content manifest commit unconfirmed: %w", err)
	}
	return result, nil
}

// appendContentManifestTx is private to the content journal/publisher. wire must
// be the result of EncodeContentManifest(m). Its callers own independent top-level
// transactions; it neither exposes a financial-transaction API nor commits.
func (s *Service) appendContentManifestTx(ctx context.Context, tx pgx.Tx, m ContentManifest, wire []byte, expectedRecoveryEpoch int64) (ContentAppendResult, error) {
	var result ContentAppendResult
	org, _ := strconv.ParseInt(m.OrgID, 10, 64)
	project, _ := strconv.ParseInt(m.ProjectID, 10, 64)
	revision, _ := strconv.ParseInt(m.Revision, 10, 64)
	hash := eventconnector.HashSourceBytes(wire)
	var source string
	var high, epoch int64
	err := tx.QueryRow(ctx, `SELECT source_instance,high_sequence,recovery_epoch FROM execution_content_streams WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, org, project).Scan(&source, &high, &epoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrContentJournalUnconfigured
	}
	if err != nil {
		return result, err
	}
	if source != m.SourceInstance {
		return result, ErrContentJournalScope
	}
	if epoch != expectedRecoveryEpoch {
		return result, ErrContentEpochChanged
	}
	// All appenders serialize before looking up a revision, so a replay racing an
	// initial append sees the committed original without consuming another number.
	var retained []byte
	var retainedHash string
	var retainedSequence int64
	err = tx.QueryRow(ctx, `SELECT sequence,document,source_hash FROM execution_content_manifests WHERE org_id=$1 AND project_id=$2 AND source_fact_id=$3 AND revision=$4`, org, project, m.SourceFactID, revision).Scan(&retainedSequence, &retained, &retainedHash)
	if err == nil {
		if retainedHash != eventconnector.HashSourceBytes(retained) || retainedSequence <= 0 || retainedSequence > high {
			return result, ErrContentJournalCorrupt
		}
		if retainedHash != hash || !bytes.Equal(retained, wire) {
			return result, ErrContentJournalRevision
		}
		return ContentAppendResult{Sequence: retainedSequence, RecoveryEpoch: epoch, SourceSHA256: hash, Replayed: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	var previousRevision int64
	err = tx.QueryRow(ctx, `SELECT revision,document,source_hash FROM execution_content_manifests WHERE org_id=$1 AND project_id=$2 AND source_fact_id=$3 ORDER BY revision DESC LIMIT 1`, org, project, m.SourceFactID).Scan(&previousRevision, &retained, &retainedHash)
	if errors.Is(err, pgx.ErrNoRows) {
		if revision != 1 {
			return result, ErrContentJournalRevision
		}
	} else if err != nil {
		return result, err
	} else {
		previous, decodeErr := DecodeContentManifest(retained)
		if decodeErr != nil || retainedHash != eventconnector.HashSourceBytes(retained) || previous.Revision != strconv.FormatInt(previousRevision, 10) {
			return result, ErrContentJournalCorrupt
		}
		if err := ValidateContentPredecessor(m, previous); err != nil {
			return result, fmt.Errorf("%w: %v", ErrContentJournalRevision, err)
		}
	}
	if m.Representation != nil {
		var saved int64
		var digest string
		var expires time.Time
		err = tx.QueryRow(ctx, `SELECT saved_bytes,digest,expires_at FROM execution_content_objects
WHERE content_ref=$1 AND org_id=$2 AND project_id=$3 AND source_fact_id=$4 AND revision=$5 AND state='ready' FOR SHARE`, m.ContentRef, org, project, m.SourceFactID, revision).Scan(&saved, &digest, &expires)
		if errors.Is(err, pgx.ErrNoRows) {
			return result, ErrContentObjectNotReady
		}
		if err != nil {
			return result, err
		}
		declaredExpires, _ := time.Parse(time.RFC3339Nano, m.ExpiresAt)
		if saved != m.Representation.SavedBytes || digest != m.Representation.Digest || !expires.Equal(declaredExpires) {
			return result, ErrContentObjectNotReady
		}
	}
	if high < 0 || high == math.MaxInt64 {
		return result, errors.New("content sequence exhausted or invalid")
	}
	sequence := high + 1
	if _, err = tx.Exec(ctx, `UPDATE execution_content_streams SET high_sequence=$3 WHERE org_id=$1 AND project_id=$2`, org, project, sequence); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO execution_content_manifests(org_id,project_id,sequence,source_fact_id,event_id,revision,request_id,document,source_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, org, project, sequence, m.SourceFactID, m.EventID, revision, m.RequestID, wire, hash); err != nil {
		return result, err
	}
	return ContentAppendResult{Sequence: sequence, RecoveryEpoch: epoch, SourceSHA256: hash}, nil
}
