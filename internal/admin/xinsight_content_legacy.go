package admin

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Legacy runtimes remain usable on their supported pre-M119 ledgers. These
// helpers query the content table only when that expansion actually exists.
func hasContentScopeTx(ctx context.Context, tx pgx.Tx) (bool, error) {
	var present bool
	err := tx.QueryRow(ctx, `SELECT to_regclass('execution_source_content_scopes') IS NOT NULL`).Scan(&present)
	return present, err
}

// Caller holds the shared parent row lock, serializing legacy registration
// against every C operation, including C operations that do not create keys.
func contentOperationExistsTx(ctx context.Context, tx pgx.Tx, org, project int64, operation string) (bool, error) {
	present, err := hasContentScopeTx(ctx, tx)
	if err != nil || !present {
		return false, err
	}
	var exists bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution_source_content_operations WHERE org_id=$1 AND project_id=$2 AND operation_id=$3)`, org, project, operation).Scan(&exists)
	return exists, err
}

func contentSourceActiveTx(ctx context.Context, tx pgx.Tx, org, project int64) (bool, error) {
	present, err := hasContentScopeTx(ctx, tx)
	if err != nil || !present {
		return false, err
	}
	var active bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution_source_content_scopes cs
	 JOIN execution_source_content_bindings b ON b.org_id=cs.org_id AND b.project_id=cs.project_id
	 JOIN api_keys ak ON ak.id=b.api_key_id
	 WHERE cs.org_id=$1 AND cs.project_id=$2 AND cs.status='active'
	 AND ak.purpose='xinsight.source.read' AND ak.source_lifecycle_scope='content' AND ak.status='feed_active')`, org, project).Scan(&active)
	return active, err
}

// Called only by the original global revoke, after the parent row lock and
// global key revocation. Local legacy profile changes must never invoke it.
func revokeContentScopeTx(ctx context.Context, tx pgx.Tx, org, project int64) error {
	present, err := hasContentScopeTx(ctx, tx)
	if err != nil || !present {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE execution_source_content_scopes
	 SET status='revoked',state_version=state_version+1,access_generation=access_generation+1,updated_at=clock_timestamp()
	 WHERE org_id=$1 AND project_id=$2`, org, project)
	return err
}
