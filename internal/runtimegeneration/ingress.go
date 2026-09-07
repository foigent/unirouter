package runtimegeneration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/releaseexecution"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

type IngressPlan struct {
	ChangeID             string     `json:"change_id"`
	PredecessorID        string     `json:"predecessor_id"`
	ExpectedConfigDigest string     `json:"expected_config_digest"`
	DesiredConfigDigest  string     `json:"desired_config_digest"`
	DesiredConfigBytes   []byte     `json:"desired_config_bytes"`
	Before               []Identity `json:"membership_before"`
	After                []Identity `json:"membership_after"`
}

func (p IngressPlan) validate() error {
	if len(p.ChangeID) == 0 || len(p.ChangeID) > 160 || strings.TrimSpace(p.ChangeID) != p.ChangeID || strings.ContainsAny(p.ChangeID, "\x00\r\n") || !modelprofile.IsDefinitionDigest(p.ExpectedConfigDigest) || !modelprofile.IsDefinitionDigest(p.DesiredConfigDigest) || len(p.DesiredConfigBytes) == 0 || len(p.DesiredConfigBytes) > 1<<20 || fmt.Sprintf("%x", sha256.Sum256(p.DesiredConfigBytes)) != p.DesiredConfigDigest || len(p.After) == 0 || len(p.After) > 64 || len(p.Before) > 64 {
		return fmt.Errorf("invalid ingress transition")
	}
	for _, members := range [][]Identity{p.Before, p.After} {
		previous := ""
		for _, i := range members {
			if err := i.Validate(); err != nil {
				return err
			}
			if i.Instance <= previous {
				return fmt.Errorf("ingress members must be unique and sorted")
			}
			previous = i.Instance
		}
	}
	return nil
}

// PrepareIngress commits every potential receiver before the external reload.
// An ambiguous commit is resolved with ReadIngress; it must never be followed
// by an unproved reload. A pending/ambiguous predecessor blocks another change.
func PrepareIngress(ctx context.Context, pool *pgxpool.Pool, p IngressPlan) (string, error) {
	if err := p.validate(); err != nil {
		return "", err
	}
	return recoverIngressSerialization(ctx, func(ctx context.Context) (string, error) {
		return prepareIngressTransaction(ctx, pool, p)
	})
}

func prepareIngressTransaction(ctx context.Context, pool *pgxpool.Pool, p IngressPlan) (string, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if err := schemaauthority.LockExecutionCompatibility(ctx, tx); err != nil {
		return "", err
	}
	if old, status, err := ReadIngress(ctx, tx, p.ChangeID); err == nil {
		a, _ := json.Marshal(p)
		b, _ := json.Marshal(old)
		if !bytes.Equal(a, b) {
			return "", fmt.Errorf("ingress change ID belongs to another plan")
		}
		return status, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	var head, status, headDigest string
	err = tx.QueryRow(ctx, `SELECT c.change_id,c.status,c.desired_config_digest FROM runtime_ingress_changes c WHERE NOT EXISTS(SELECT 1 FROM runtime_ingress_changes n WHERE n.predecessor_id=c.change_id)`).Scan(&head, &status, &headDigest)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if head != p.PredecessorID || (head != "" && (status != "applied" || headDigest != p.ExpectedConfigDigest)) {
		return "", fmt.Errorf("ingress predecessor is not the exact applied head")
	}
	rows, err := tx.Query(ctx, `SELECT instance_id,artifact_digest FROM runtime_generations WHERE ingress_state<>'absent' ORDER BY instance_id`)
	if err != nil {
		return "", err
	}
	current := []Identity{}
	for rows.Next() {
		var i Identity
		if err := rows.Scan(&i.Instance, &i.Artifact); err != nil {
			rows.Close()
			return "", err
		}
		current = append(current, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	if !slices.Equal(current, p.Before) {
		return "", fmt.Errorf("ingress membership changed")
	}
	// The initial empty ledger imports the externally observed existing proxy
	// membership via the same after set. It does not pretend it was absent from
	// Nginx before the bootstrap controller's exact readback.
	for _, i := range p.After {
		if err := requireGenerationJoin(ctx, tx, i); err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, `UPDATE runtime_generations SET ingress_state='pending_join',revision=revision+1,updated_at=clock_timestamp() WHERE instance_id=$1`, i.Instance); err != nil {
			return "", err
		}
	}
	for _, i := range p.Before {
		if !slices.Contains(p.After, i) {
			if _, err := tx.Exec(ctx, `UPDATE runtime_generations SET ingress_state='withdrawing',revision=revision+1,updated_at=clock_timestamp() WHERE instance_id=$1`, i.Instance); err != nil {
				return "", err
			}
		}
	}
	before, _ := json.Marshal(p.Before)
	after, _ := json.Marshal(p.After)
	_, err = tx.Exec(ctx, `INSERT INTO runtime_ingress_changes(change_id,predecessor_id,expected_config_digest,desired_config_digest,desired_config_bytes,membership_before,membership_after,status) VALUES($1,NULLIF($2,''),$3,$4,$5,$6,$7,'pending')`, p.ChangeID, p.PredecessorID, p.ExpectedConfigDigest, p.DesiredConfigDigest, p.DesiredConfigBytes, before, after)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("ingress prepare commit ambiguous: %w", err)
	}
	return "pending", nil
}

type ingressReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func ReadIngress(ctx context.Context, q ingressReader, id string) (IngressPlan, string, error) {
	var p IngressPlan
	var status string
	var before, after []byte
	err := q.QueryRow(ctx, `SELECT change_id,COALESCE(predecessor_id,''),expected_config_digest,desired_config_digest,desired_config_bytes,membership_before,membership_after,status FROM runtime_ingress_changes WHERE change_id=$1`, id).Scan(&p.ChangeID, &p.PredecessorID, &p.ExpectedConfigDigest, &p.DesiredConfigDigest, &p.DesiredConfigBytes, &before, &after, &status)
	if err != nil {
		return p, status, err
	}
	if err := json.Unmarshal(before, &p.Before); err != nil {
		return p, status, err
	}
	if err := json.Unmarshal(after, &p.After); err != nil {
		return p, status, err
	}
	return p, status, p.validate()
}

func requireGenerationJoin(ctx context.Context, tx pgx.Tx, i Identity) error {
	var artifact, head, state string
	var live bool
	var wire []byte
	var digest string
	err := tx.QueryRow(ctx, `SELECT g.artifact_digest,g.schema_head,g.state,g.lease_expires_at>clock_timestamp(),m.canonical_bytes,m.manifest_digest FROM runtime_generations g JOIN runtime_support_manifests m USING(artifact_digest) WHERE g.instance_id=$1 FOR UPDATE OF g`, i.Instance).Scan(&artifact, &head, &state, &live, &wire, &digest)
	if err != nil {
		return err
	}
	if artifact != i.Artifact || state != "serving" || !live {
		return fmt.Errorf("joining generation is not a live exact instance")
	}
	m, err := VerifyManifest(wire, digest)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT r.id FROM runtime_change_slots s JOIN runtime_change_releases r ON r.id IN(s.stable_release_id,s.canary_release_id) WHERE r.execution_snapshot_digest IS NOT NULL ORDER BY r.id`)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		s, err := releaseexecution.ReadSnapshot(ctx, tx, id)
		if err != nil {
			return err
		}
		if s == nil {
			return fmt.Errorf("active snapshot disappeared")
		}
		if err := m.Require(head, s.Requirements); err != nil {
			return err
		}
	}
	return nil
}

// CompleteIngress is called only by the same controller after exact config
// readback and disappearance of every pre-reload proxy worker. An ambiguous
// reload retains both member sets; it never grants permission to stop backends.
func CompleteIngress(ctx context.Context, pool *pgxpool.Pool, id, observedDigest string, oldWorkersGone bool, observation json.RawMessage) (string, error) {
	if len(observation) == 0 || len(observation) > 64<<10 || !json.Valid(observation) {
		return "", fmt.Errorf("bounded ingress observation required")
	}
	return recoverIngressSerialization(ctx, func(ctx context.Context) (string, error) {
		return completeIngressTransaction(ctx, pool, id, observedDigest, oldWorkersGone, observation)
	})
}

func completeIngressTransaction(ctx context.Context, pool *pgxpool.Pool, id, observedDigest string, oldWorkersGone bool, observation json.RawMessage) (string, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if err := schemaauthority.LockExecutionCompatibility(ctx, tx); err != nil {
		return "", err
	}
	p, status, err := ReadIngress(ctx, tx, id)
	if err != nil {
		return "", err
	}
	if status == "applied" {
		if observedDigest != p.DesiredConfigDigest || !oldWorkersGone {
			return "", fmt.Errorf("applied ingress readback mismatch")
		}
		return status, nil
	}
	if status != "pending" && status != "ambiguous" {
		return "", fmt.Errorf("ingress transition cannot complete")
	}
	status = "ambiguous"
	if observedDigest == p.DesiredConfigDigest && oldWorkersGone {
		for _, i := range p.After {
			if err := requireGenerationJoin(ctx, tx, i); err != nil {
				return "", err
			}
			if _, err := tx.Exec(ctx, `UPDATE runtime_generations SET ingress_state='receiving',revision=revision+1,updated_at=clock_timestamp() WHERE instance_id=$1`, i.Instance); err != nil {
				return "", err
			}
		}
		for _, i := range p.Before {
			if !slices.Contains(p.After, i) {
				if _, err := tx.Exec(ctx, `UPDATE runtime_generations SET ingress_state='absent',revision=revision+1,updated_at=clock_timestamp() WHERE instance_id=$1`, i.Instance); err != nil {
					return "", err
				}
			}
		}
		status = "applied"
	}
	_, err = tx.Exec(ctx, `UPDATE runtime_ingress_changes SET status=$2,observation=$3,completed_at=CASE WHEN $2='applied' THEN clock_timestamp() ELSE NULL END WHERE change_id=$1`, id, status, observation)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("ingress completion commit ambiguous: %w", err)
	}
	return status, nil
}

// Heartbeats may commit after this owner's Repeatable Read snapshot. A 40001
// explicitly aborts the transaction, including when PostgreSQL rejects COMMIT;
// only that known outcome permits restarting the entire database operation.
// The five-second budget fits inside the ingress controller's 30-second owner
// command deadline. No external reload or Provider operation is in this closure.
// Connection errors, cancellation and unknown commits return without replay.
func recoverIngressSerialization(ctx context.Context, operation func(context.Context) (string, error)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		status, err := operation(ctx)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "40001" {
			return status, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", fmt.Errorf("ingress serialization recovery deadline: %w", ctx.Err())
		case <-timer.C:
		}
	}
}
