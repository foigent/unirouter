package completedfeed

import (
	"context"
	"errors"
	"fmt"

	"github.com/uniroute/unirouter/internal/db"
)

const (
	ConnectionStatusActive = "active"
	ConnectionStatusPaused = "paused"
)

var ErrConnectionMissing = errors.New("completed-execution feed connection is not configured")

func (service *Service) Enabled() bool {
	return service != nil && service.enabled && service.codec != nil
}

func (service *Service) SourceInstance() string {
	if service == nil {
		return ""
	}
	return service.sourceInstance
}

func (service *Service) EncodeSequence(projectID, sequence int64) (string, error) {
	if service == nil || service.codec == nil {
		return "", errors.New("completed-execution cursor codec is unavailable")
	}
	return service.codec.Encode(projectID, sequence)
}

type accessRecorder interface {
	RecordAuthenticatedPull(context.Context, int64, int64, int64) (string, error)
	Acknowledge(context.Context, int64, int64, int64, int64) error
}

type postgresAccessRecorder struct {
	db db.DBQuerier
}

func (recorder postgresAccessRecorder) RecordAuthenticatedPull(
	ctx context.Context,
	orgID, projectID, keyID int64,
) (string, error) {
	if recorder.db == nil || orgID <= 0 || projectID <= 0 || keyID <= 0 {
		return "", ErrConnectionMissing
	}
	var status string
	err := recorder.db.QueryRow(ctx,
		`UPDATE completed_execution_feed_connections AS connection
		 SET last_authenticated_pull_at = NOW(),
		     last_authenticated_key_id = $3,
		     updated_at = NOW()
		 FROM api_keys AS key
		 WHERE connection.org_id = $1
		   AND connection.project_id = $2
		   AND key.id = $3
		   AND key.org_id = connection.org_id
		   AND key.project_id = connection.project_id
		   AND key.purpose = 'xinsight.feed.read'
		   AND key.status IN ('feed_active', 'feed_staged')
		 RETURNING connection.status`,
		orgID, projectID, keyID,
	).Scan(&status)
	if err != nil {
		return "", fmt.Errorf("record completed-execution feed authenticated pull: %w", err)
	}
	return status, nil
}

func (recorder postgresAccessRecorder) Acknowledge(
	ctx context.Context,
	orgID, projectID, keyID, sequence int64,
) error {
	if recorder.db == nil || orgID <= 0 || projectID <= 0 || keyID <= 0 || sequence < 0 {
		return ErrConnectionMissing
	}
	tag, err := recorder.db.Exec(ctx,
		`UPDATE completed_execution_feed_connections AS connection
		 SET acknowledged_sequence = GREATEST(connection.acknowledged_sequence, $4),
		     updated_at = NOW()
		 FROM api_keys AS key
		 WHERE connection.org_id = $1
		   AND connection.project_id = $2
		   AND key.id = $3
		   AND key.org_id = connection.org_id
		   AND key.project_id = connection.project_id
		   AND key.purpose = 'xinsight.feed.read'
		   AND key.status IN ('feed_active', 'feed_staged')
		   AND (key.status = 'feed_active' OR connection.status = 'paused')`,
		orgID, projectID, keyID, sequence,
	)
	if err != nil {
		return fmt.Errorf("record completed-execution receiver acknowledgement: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrConnectionMissing
	}
	return nil
}
