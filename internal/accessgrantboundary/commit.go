package accessgrantboundary

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/principallock"
)

// ResolveForCommit serializes a durable admission with every supported
// principal, Workload, Grant, logical-credential, and secret-version
// transition. The first query is non-locking and reveals only the exact
// caller-supplied org/project lane; all authoritative interpretation happens
// after the shared principal order is established and the binding rows are
// locked. Callers must keep the transaction open through their durable write.
func ResolveForCommit(ctx context.Context, tx pgx.Tx, keyID, orgID, projectID int64) (Snapshot, error) {
	if keyID <= 0 || orgID <= 0 || projectID <= 0 {
		return Snapshot{}, ErrInactive
	}
	var ownerUserID, workloadID, grantID, credentialID sql.NullInt64
	var grantSubjectUserID, grantSubjectWorkloadID, sponsorUserID sql.NullInt64
	var credentialRef string
	err := tx.QueryRow(ctx, `
		SELECT ak.owner_user_id,ak.workload_application_id,ak.access_grant_id,
		       ak.access_credential_id,COALESCE(ak.credential_ref,''),
		       ag.subject_user_id,ag.subject_workload_application_id,ag.sponsor_user_id
		FROM api_keys ak
		LEFT JOIN access_grants ag
		  ON ag.id=ak.access_grant_id AND ag.org_id=ak.org_id AND ag.project_id=ak.project_id
		WHERE ak.id=$1 AND ak.org_id=$2 AND ak.project_id=$3`, keyID, orgID, projectID).Scan(
		&ownerUserID, &workloadID, &grantID, &credentialID, &credentialRef,
		&grantSubjectUserID, &grantSubjectWorkloadID, &sponsorUserID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Snapshot{}, ErrInactive
		}
		return Snapshot{}, err
	}
	if grantID.Valid != credentialID.Valid || credentialID.Valid != (credentialRef != "") {
		return Snapshot{}, ErrInactive
	}
	if err := principallock.LockUsers(ctx, tx,
		nullableID(ownerUserID), nullableID(grantSubjectUserID), nullableID(sponsorUserID)); err != nil {
		return Snapshot{}, err
	}
	if err := principallock.LockWorkloads(ctx, tx,
		nullableID(workloadID), nullableID(grantSubjectWorkloadID)); err != nil {
		return Snapshot{}, err
	}
	if err := lockCommitScopeRows(ctx, tx, keyID, orgID, projectID, workloadID, grantID, credentialID, credentialRef); err != nil {
		return Snapshot{}, err
	}
	snapshot, err := Resolve(ctx, tx, keyID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Snapshot{}, ErrInactive
		}
		return Snapshot{}, err
	}
	if snapshot.APIKey.OrgID != orgID || snapshot.APIKey.ProjectID == nil || *snapshot.APIKey.ProjectID != projectID ||
		!sameSnapshotID(snapshot.APIKey.AccessGrantID, grantID) ||
		!sameSnapshotID(snapshot.APIKey.AccessCredentialID, credentialID) || snapshot.APIKey.CredentialRef != credentialRef {
		return Snapshot{}, ErrInactive
	}
	return snapshot, nil
}

func lockCommitScopeRows(
	ctx context.Context,
	tx pgx.Tx,
	keyID, orgID, projectID int64,
	workloadID, grantID, credentialID sql.NullInt64,
	credentialRef string,
) error {
	var lockedID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM orgs WHERE id=$1 FOR SHARE`, orgID).Scan(&lockedID); err != nil {
		return commitLockError(err)
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM projects WHERE id=$1 AND org_id=$2 FOR SHARE`, projectID, orgID).Scan(&lockedID); err != nil {
		return commitLockError(err)
	}
	if workloadID.Valid {
		if err := tx.QueryRow(ctx, `
			SELECT id FROM workload_applications
			WHERE id=$1 AND org_id=$2 AND project_id=$3 FOR SHARE`,
			workloadID.Int64, orgID, projectID).Scan(&lockedID); err != nil {
			return commitLockError(err)
		}
	}
	if grantID.Valid {
		if err := tx.QueryRow(ctx, `
			SELECT id FROM access_grants
			WHERE id=$1 AND org_id=$2 AND project_id=$3 FOR SHARE`,
			grantID.Int64, orgID, projectID).Scan(&lockedID); err != nil {
			return commitLockError(err)
		}
		if err := tx.QueryRow(ctx, `
			SELECT id FROM access_credentials
			WHERE id=$1 AND access_grant_id=$2 AND org_id=$3 AND project_id=$4
			  AND credential_ref=$5 FOR SHARE`,
			credentialID.Int64, grantID.Int64, orgID, projectID, credentialRef).Scan(&lockedID); err != nil {
			return commitLockError(err)
		}
	}
	if err := tx.QueryRow(ctx, `
		SELECT id FROM api_keys
		WHERE id=$1 AND org_id=$2 AND project_id=$3
		  AND access_grant_id IS NOT DISTINCT FROM $4::bigint
		  AND access_credential_id IS NOT DISTINCT FROM $5::bigint
		  AND COALESCE(credential_ref,'')=$6
		FOR SHARE`, keyID, orgID, projectID, nullableSQLID(grantID), nullableSQLID(credentialID), credentialRef).Scan(&lockedID); err != nil {
		return commitLockError(err)
	}
	return nil
}

func commitLockError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInactive
	}
	return err
}

func nullableID(value sql.NullInt64) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}

func nullableSQLID(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

func sameSnapshotID(pointer *int64, value sql.NullInt64) bool {
	if pointer == nil {
		return !value.Valid
	}
	return value.Valid && *pointer == value.Int64
}
