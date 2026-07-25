package completedfeed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/db"
)

type StoredItem struct {
	Sequence            int64
	RequestID           string
	TerminalStatus      string
	TerminalAvailableAt time.Time
	PublishedAt         time.Time
	Revision            string
	Snapshot            []byte
	SnapshotSHA256      string
}

type ReadWindow struct {
	Items         []StoredItem
	HighWatermark int64
	RetainedAfter int64
	Remaining     int64
}

type Reader interface {
	Read(context.Context, int64, int64, int64, int) (ReadWindow, error)
}

type postgresReader struct {
	db db.DBQuerier
}

func (reader postgresReader) Read(ctx context.Context, orgID, projectID, after int64, limit int) (ReadWindow, error) {
	if reader.db == nil {
		return ReadWindow{}, errors.New("completed-execution feed database is unavailable")
	}
	if orgID <= 0 || projectID <= 0 || after < 0 || limit < 1 || limit > MaxPageItems+1 {
		return ReadWindow{}, errors.New("completed-execution feed read bounds are invalid")
	}
	tx, err := reader.db.Begin(ctx)
	if err != nil {
		return ReadWindow{}, fmt.Errorf("begin completed-execution feed read: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY"); err != nil {
		return ReadWindow{}, fmt.Errorf("set completed-execution feed read snapshot: %w", err)
	}

	var window ReadWindow
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE((
		    SELECT retained_after_id
		    FROM completed_execution_feed_states
		    WHERE org_id = $1 AND project_id = $2
		 ), 0)`,
		orgID, projectID,
	).Scan(&window.RetainedAfter); err != nil {
		return ReadWindow{}, fmt.Errorf("read completed-execution retained floor: %w", err)
	}

	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(id), 0),
		        COUNT(*) FILTER (WHERE id > $3)
		 FROM completed_execution_feed_items
		 WHERE org_id = $1 AND project_id = $2`,
		orgID, projectID, after,
	).Scan(&window.HighWatermark, &window.Remaining); err != nil {
		return ReadWindow{}, fmt.Errorf("read completed-execution high watermark: %w", err)
	}
	if window.HighWatermark < window.RetainedAfter {
		window.HighWatermark = window.RetainedAfter
	}

	rows, err := tx.Query(ctx,
		`SELECT id, request_id, terminal_status, terminal_available_at,
		        created_at, revision, snapshot, snapshot_sha256
		 FROM completed_execution_feed_items
		 WHERE org_id = $1 AND project_id = $2
		   AND id > $3 AND id <= $4
		 ORDER BY id ASC
		 LIMIT $5`,
		orgID, projectID, after, window.HighWatermark, limit,
	)
	if err != nil {
		return ReadWindow{}, fmt.Errorf("query completed-execution feed page: %w", err)
	}
	for rows.Next() {
		var item StoredItem
		if err := rows.Scan(
			&item.Sequence,
			&item.RequestID,
			&item.TerminalStatus,
			&item.TerminalAvailableAt,
			&item.PublishedAt,
			&item.Revision,
			&item.Snapshot,
			&item.SnapshotSHA256,
		); err != nil {
			rows.Close()
			return ReadWindow{}, fmt.Errorf("scan completed-execution feed item: %w", err)
		}
		item.Snapshot = append([]byte(nil), item.Snapshot...)
		window.Items = append(window.Items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ReadWindow{}, fmt.Errorf("iterate completed-execution feed items: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return ReadWindow{}, fmt.Errorf("commit completed-execution feed read: %w", err)
	}
	return window, nil
}

func deleteExpiredBatch(ctx context.Context, database db.DBQuerier, cutoff time.Time, batchSize int) (int64, error) {
	if database == nil || batchSize < 1 || batchSize > 10_000 {
		return 0, errors.New("completed-execution retention bounds are invalid")
	}
	tx, err := database.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin completed-execution retention: %w", err)
	}
	defer tx.Rollback(ctx)

	// Claim exactly one project through the same state row used by
	// materialization. This serializes sequence allocation and retention per
	// project across every service replica.
	var orgID, projectID, retainedAfterID int64
	err = tx.QueryRow(ctx,
		`SELECT state.org_id, state.project_id, state.retained_after_id
		 FROM completed_execution_feed_states AS state
		 JOIN LATERAL (
		    SELECT item.created_at
		    FROM completed_execution_feed_items AS item
		    WHERE item.org_id = state.org_id
		      AND item.project_id = state.project_id
		    ORDER BY item.id ASC
		    LIMIT 1
		 ) AS first_item ON first_item.created_at < $1
		 ORDER BY state.org_id, state.project_id
		 FOR UPDATE OF state SKIP LOCKED
		 LIMIT 1`,
		cutoff.UTC(),
	).Scan(&orgID, &projectID, &retainedAfterID)
	if errors.Is(err, pgx.ErrNoRows) {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return 0, fmt.Errorf("commit empty completed-execution retention: %w", commitErr)
		}
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("claim completed-execution retention project: %w", err)
	}
	if retainedAfterID < 0 {
		return 0, errors.New("completed-execution retained floor is invalid")
	}

	// created_at is assigned only after the project state fence is held. Delete
	// no farther than the first retained publication, even if terminal times
	// are out of order. There is no row-level SKIP LOCKED set from which a
	// hole-bearing MAX(id) could be derived.
	var newFloor, deleted int64
	if err := tx.QueryRow(ctx,
		`WITH ordered AS (
		    SELECT item.id,
		           BOOL_OR(item.created_at >= $3) OVER (
		               ORDER BY item.id ASC
		               ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
		           ) AS retention_barrier
		    FROM completed_execution_feed_items AS item
		    WHERE item.org_id = $1
		      AND item.project_id = $2
		 ), doomed AS (
		    SELECT id
		    FROM ordered
		    WHERE NOT retention_barrier
		    ORDER BY id ASC
		    LIMIT $4
		 ), deleted AS (
		    DELETE FROM completed_execution_feed_items AS item
		    USING doomed
		    WHERE item.id = doomed.id
		    RETURNING item.id
		 )
		 SELECT COALESCE(MAX(id), $5), COUNT(*)
		 FROM deleted`,
		orgID, projectID, cutoff.UTC(), batchSize, retainedAfterID,
	).Scan(&newFloor, &deleted); err != nil {
		return 0, fmt.Errorf("delete completed-execution retention prefix: %w", err)
	}
	if deleted > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE completed_execution_feed_states
			 SET retained_after_id = $3, updated_at = NOW()
			 WHERE org_id = $1 AND project_id = $2`,
			orgID, projectID, newFloor,
		); err != nil {
			return 0, fmt.Errorf("advance completed-execution retention floor: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit completed-execution retention: %w", err)
	}
	return deleted, nil
}

var _ Reader = postgresReader{}
