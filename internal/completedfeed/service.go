package completedfeed

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/db"
)

const (
	RetentionWindow       = 7 * 24 * time.Hour
	retentionInterval     = time.Hour
	retentionBatchSize    = 1000
	retentionMaxBatches   = 10
	maxConcurrentFeedRead = 8
)

type Service struct {
	db             db.DBQuerier
	reader         Reader
	codec          *CursorCodec
	sourceInstance string
	enabled        bool
	now            func() time.Time
	readSlots      chan struct{}
	access         accessRecorder
}

func NewService(database db.DBQuerier, cursorSecret, sourceInstance string, enabled bool) (*Service, error) {
	service := &Service{
		db:             database,
		reader:         postgresReader{db: database},
		sourceInstance: sourceInstance,
		enabled:        enabled,
		now:            time.Now,
		readSlots:      make(chan struct{}, maxConcurrentFeedRead),
		access:         postgresAccessRecorder{db: database},
	}
	if !enabled {
		return service, nil
	}
	if database == nil {
		return nil, errors.New("completed-execution feed database is required")
	}
	codec, err := NewCursorCodec(cursorSecret, sourceInstance)
	if err != nil {
		return nil, err
	}
	service.codec = codec
	return service, nil
}

func (service *Service) MaterializeTerminalTx(ctx context.Context, tx pgx.Tx, record billing.TerminalRecord) error {
	if service == nil || !service.enabled {
		return nil
	}
	if tx == nil {
		return errors.New("completed-execution materialization transaction is required")
	}
	// V1 is project-scoped. Legacy terminal events without a project remain in
	// the authoritative billing journal but are outside this activation-forward
	// integration contract.
	if record.ProjectID <= 0 {
		feedMaterialized.WithLabelValues(record.Status, "not_project_scoped").Inc()
		return nil
	}
	_, snapshot, snapshotHash, err := BuildSnapshot(SnapshotInput{
		RequestID:            record.RequestID,
		OrgID:                record.OrgID,
		ProjectID:            record.ProjectID,
		TerminalStatus:       record.Status,
		TerminalAvailableAt:  record.OccurredAt,
		Model:                record.Model,
		PromptTokens:         record.PromptTokens,
		UncachedPromptTokens: record.UncachedPromptTokens,
		CacheReadTokens:      record.CacheReadTokens,
		CacheCreationTokens:  record.CacheCreationTokens,
		CompletionTokens:     record.CompletionTokens,
		UsageSource:          record.UsageSource,
		UsageAuthority:       record.UsageAuthority,
		UsagePurpose:         record.UsagePurpose,
		UsageFinality:        record.UsageFinality,
		UsageContractVersion: record.UsageContractVersion,
		UsageEstimateVersion: record.UsageEstimateVersion,
		CacheUsageSchema:     record.CacheUsageSchema,
		CacheUsageStatus:     record.CacheUsageStatus,
		CalculatedCostMicros: record.CalculatedCostMicros,
		ChargedCostMicros:    record.ChargedCostMicros,
		PricingVersion:       record.PricingVersion,
	})
	if err != nil {
		feedMaterialized.WithLabelValues(record.Status, "rejected").Inc()
		return fmt.Errorf("build completed-execution snapshot: %w", err)
	}

	// BIGSERIAL allocation order alone is not commit order. Lock the durable
	// project state before allocating the item sequence so a reader can never
	// advance past a lower, still-uncommitted item from the same project.
	if _, err := tx.Exec(ctx,
		`INSERT INTO completed_execution_feed_states (
		    org_id, project_id, retained_after_id, updated_at
		 ) VALUES ($1, $2, 0, NOW())
		 ON CONFLICT (org_id, project_id) DO NOTHING`,
		record.OrgID, record.ProjectID,
	); err != nil {
		return fmt.Errorf("initialize completed-execution feed state: %w", err)
	}
	var retainedAfterID int64
	if err := tx.QueryRow(ctx,
		`SELECT retained_after_id
		 FROM completed_execution_feed_states
		 WHERE org_id = $1 AND project_id = $2
		 FOR UPDATE`,
		record.OrgID, record.ProjectID,
	).Scan(&retainedAfterID); err != nil {
		return fmt.Errorf("lock completed-execution project sequence: %w", err)
	}
	if retainedAfterID < 0 {
		return errors.New("completed-execution retained floor is invalid")
	}

	tag, err := tx.Exec(ctx,
		`INSERT INTO completed_execution_feed_items (
		    event_key, org_id, project_id, request_id, terminal_status,
		    terminal_available_at, revision, snapshot, snapshot_sha256, created_at
		 ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, clock_timestamp())
		 ON CONFLICT (event_key) DO NOTHING`,
		record.EventKey,
		record.OrgID,
		record.ProjectID,
		record.RequestID,
		record.Status,
		record.OccurredAt.UTC(),
		SnapshotRevision,
		[]byte(snapshot),
		snapshotHash,
	)
	if err != nil {
		feedMaterialized.WithLabelValues(record.Status, "failed").Inc()
		return fmt.Errorf("insert completed-execution feed item: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var existingHash string
		if err := tx.QueryRow(ctx,
			`SELECT snapshot_sha256
			 FROM completed_execution_feed_items
			 WHERE event_key = $1`,
			record.EventKey,
		).Scan(&existingHash); err != nil {
			return fmt.Errorf("read replayed completed-execution feed item: %w", err)
		}
		if existingHash != snapshotHash {
			feedMaterialized.WithLabelValues(record.Status, "conflict").Inc()
			return errors.New("completed-execution feed replay content conflicts with retained item")
		}
		feedMaterialized.WithLabelValues(record.Status, "duplicate").Inc()
		return nil
	}
	feedMaterialized.WithLabelValues(record.Status, "created").Inc()
	return nil
}

func (service *Service) RunRetention(ctx context.Context) {
	if service == nil || !service.enabled {
		return
	}
	service.runRetentionOnce(ctx)
	ticker := time.NewTicker(retentionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			service.runRetentionOnce(ctx)
		}
	}
}

func (service *Service) runRetentionOnce(ctx context.Context) {
	for batch := 0; batch < retentionMaxBatches; batch++ {
		deleted, err := deleteExpiredBatch(
			ctx,
			service.db,
			service.now().UTC().Add(-RetentionWindow),
			retentionBatchSize,
		)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("[completed-execution-feed] retention error: %v", err)
			}
			return
		}
		if deleted > 0 {
			feedRetentionDeleted.Add(float64(deleted))
		}
		if deleted == 0 {
			return
		}
	}
}

var _ billing.TerminalMaterializer = (*Service)(nil)
