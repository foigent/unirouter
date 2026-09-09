package sourcefacts

import (
	"context"
	"log"
	"time"
)

// DeleteExpired advances only a contiguous prefix under the publication lock.
// Reservation/job snapshots have a different lifetime and are never deleted here.
func (s *Service) DeleteExpired(ctx context.Context, cutoff time.Time) (int64, error) {
	if !s.Enabled() {
		return 0, nil
	}
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT source_instance,org_id,project_id,profile,high_sequence,retained_after_sequence FROM execution_source_streams
	 WHERE source_instance=$1 AND high_sequence>retained_after_sequence
	 AND EXISTS(SELECT 1 FROM execution_source_facts f WHERE f.source_instance=execution_source_streams.source_instance AND f.org_id=execution_source_streams.org_id AND f.project_id=execution_source_streams.project_id AND f.profile=execution_source_streams.profile AND f.published_at<$2)
	 ORDER BY org_id,project_id,profile LIMIT 100 FOR UPDATE SKIP LOCKED`, s.options.SourceInstance, cutoff)
	if e != nil {
		return 0, e
	}
	type stream struct {
		instance, profile         string
		org, project, high, floor int64
	}
	streams := []stream{}
	for rows.Next() {
		var x stream
		if e = rows.Scan(&x.instance, &x.org, &x.project, &x.profile, &x.high, &x.floor); e != nil {
			break
		}
		streams = append(streams, x)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return 0, e
	}
	var deleted int64
	for _, x := range streams {
		var last int64
		if e = tx.QueryRow(ctx, `SELECT COALESCE(MIN(sequence)-1,$5) FROM execution_source_facts WHERE source_instance=$1 AND org_id=$2 AND project_id=$3 AND profile=$4 AND published_at >= $6`, x.instance, x.org, x.project, x.profile, x.high, cutoff).Scan(&last); e != nil {
			return 0, e
		}
		if last <= x.floor {
			continue
		}
		if last > x.floor+1000 {
			last = x.floor + 1000
		}
		tag, err := tx.Exec(ctx, `DELETE FROM execution_source_facts WHERE source_instance=$1 AND org_id=$2 AND project_id=$3 AND profile=$4 AND sequence<=$5`, x.instance, x.org, x.project, x.profile, last)
		if err != nil {
			return 0, err
		}
		deleted += tag.RowsAffected()
		if _, e = tx.Exec(ctx, `UPDATE execution_source_streams SET retained_after_sequence=$5 WHERE source_instance=$1 AND org_id=$2 AND project_id=$3 AND profile=$4`, x.instance, x.org, x.project, x.profile, last); e != nil {
			return 0, e
		}
	}
	return deleted, tx.Commit(ctx)
}

func (s *Service) RunRetention(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if _, e := s.DeleteExpired(ctx, time.Now().UTC().Add(-RetentionWindow)); e != nil && ctx.Err() == nil {
			log.Printf("[source-facts] retention unavailable: %v", e)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
