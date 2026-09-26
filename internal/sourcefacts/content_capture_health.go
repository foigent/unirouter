package sourcefacts

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type contentCaptureHealth struct {
	RecoveryEpoch string `json:"recovery_epoch"`
	SourceFactID  string `json:"source_fact_id"`
	ObservedAt    string `json:"observed_at"`
}

// Only a missing or unconfirmed manifest is a manifest-write gap. Successful
// partial/not_captured records describe themselves; they do not erase old gaps.
func (s *ContentCaptureService) recordCaptureHealth(m ContentManifest, epoch int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), s.config.HealthTimeout)
	defer cancel()
	org, _ := strconv.ParseInt(m.OrgID, 10, 64)
	project, _ := strconv.ParseInt(m.ProjectID, 10, 64)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var source string
	var current int64
	err = tx.QueryRow(ctx, `SELECT source_instance,recovery_epoch FROM execution_content_streams WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, org, project).Scan(&source, &current)
	if err != nil {
		return err
	}
	if source != m.SourceInstance || current != epoch {
		return ErrContentEpochChanged
	}
	wire, err := json.Marshal(contentCaptureHealth{RecoveryEpoch: strconv.FormatInt(epoch, 10), SourceFactID: m.SourceFactID, ObservedAt: contentReadTime(time.Now())})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO execution_content_health(org_id,project_id,reason,detail) VALUES($1,$2,'manifest_write_unavailable',$3) ON CONFLICT(org_id,project_id) DO UPDATE SET reason=EXCLUDED.reason,detail=EXCLUDED.detail,updated_at=clock_timestamp()`, org, project, wire)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func contentKnownHealthGap(ctx context.Context, tx pgx.Tx, org, project, epoch int64) (bool, error) {
	var reason string
	var wire []byte
	err := tx.QueryRow(ctx, `SELECT reason,detail FROM execution_content_health WHERE org_id=$1 AND project_id=$2 AND octet_length(detail)<=1024`, org, project).Scan(&reason, &wire)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var health contentCaptureHealth
	if reason != "manifest_write_unavailable" || len(wire) > 1024 || json.Unmarshal(wire, &health) != nil || health.RecoveryEpoch != strconv.FormatInt(epoch, 10) || !contentFactPattern.MatchString(health.SourceFactID) {
		return false, nil
	}
	observed, err := time.Parse("2006-01-02T15:04:05.000000Z", health.ObservedAt)
	if err != nil || observed.IsZero() {
		return false, nil
	}
	return true, nil
}
