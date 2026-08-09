// Package accesscredential owns the lifecycle fan-out from one logical access
// credential to all of its secret versions. It deliberately excludes
// purpose-scoped Feed credentials, which have an independent lifecycle.
package accesscredential

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ContainmentResult describes only product-state rows changed by a permanent
// containment operation. Logical credentials and secret versions are counted
// separately so callers can write accurate audit evidence.
type ContainmentResult struct {
	CredentialFamilies int64
	SecretVersions     int64
	LegacyKeys         int64
}

// RevokeForGrant permanently revokes every logical credential issued under a
// revoked grant and disables every secret version in the same transaction.
func RevokeForGrant(ctx context.Context, tx pgx.Tx, grantID, actorUserID int64, at time.Time) (ContainmentResult, error) {
	return revoke(ctx, tx, `ac.access_grant_id=$1`, []any{grantID}, "FALSE", nil, actorUserID, at)
}

// RevokeFamily terminates one logical credential and every secret version in
// its immutable family. The family ID and Grant ID are both required so a
// caller cannot use a version locator to cross an authorization boundary.
func RevokeFamily(ctx context.Context, tx pgx.Tx, familyID, grantID, actorUserID int64, at time.Time) (ContainmentResult, error) {
	return revoke(ctx, tx, `ac.id=$1 AND ac.access_grant_id=$2`, []any{familyID, grantID}, "FALSE", nil, actorUserID, at)
}

// RevokeForUser permanently contains all personal access for a deleted user.
// A zero projectID selects every project in the organization.
func RevokeForUser(ctx context.Context, tx pgx.Tx, orgID, projectID, userID, actorUserID int64, at time.Time) (ContainmentResult, error) {
	familyPredicate := `ag.org_id=$1 AND ag.subject_type='user' AND ag.subject_user_id=$2`
	familyArgs := []any{orgID, userID}
	legacyPredicate := `ak.org_id=$1 AND ak.owner_user_id=$2 AND ak.key_type='personal'
		AND ak.access_credential_id IS NULL AND COALESCE(ak.purpose,'')=''`
	legacyArgs := []any{orgID, userID}
	if projectID > 0 {
		familyPredicate += ` AND ag.project_id=$3`
		familyArgs = append(familyArgs, projectID)
		legacyPredicate += ` AND ak.project_id=$3`
		legacyArgs = append(legacyArgs, projectID)
	}
	return revoke(ctx, tx, familyPredicate, familyArgs, legacyPredicate, legacyArgs, actorUserID, at)
}

// RevokeForWorkload permanently contains all service access for an archived
// workload identity. Existing legacy-unbound service keys are disabled but are
// never reclassified as logical credentials.
func RevokeForWorkload(ctx context.Context, tx pgx.Tx, orgID, projectID, workloadID, actorUserID int64, at time.Time) (ContainmentResult, error) {
	return revoke(ctx, tx,
		`ag.org_id=$1 AND ag.project_id=$2 AND ag.subject_type='workload_application'
		 AND ag.subject_workload_application_id=$3`, []any{orgID, projectID, workloadID},
		`ak.org_id=$1 AND ak.project_id=$2 AND ak.workload_application_id=$3
		 AND ak.access_credential_id IS NULL AND COALESCE(ak.purpose,'')=''`, []any{orgID, projectID, workloadID}, actorUserID, at)
}

func revoke(
	ctx context.Context,
	tx pgx.Tx,
	familyPredicate string,
	familyArgs []any,
	legacyPredicate string,
	legacyArgs []any,
	actorUserID int64,
	at time.Time,
) (ContainmentResult, error) {
	rows, err := tx.Query(ctx, fmt.Sprintf(`
		SELECT ac.id
		FROM access_credentials ac
		JOIN access_grants ag
		  ON ag.id=ac.access_grant_id AND ag.org_id=ac.org_id AND ag.project_id=ac.project_id
		WHERE %s
		ORDER BY ac.id
		FOR UPDATE OF ac`, familyPredicate), familyArgs...)
	if err != nil {
		return ContainmentResult{}, fmt.Errorf("lock access credential families: %w", err)
	}
	familyIDs := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return ContainmentResult{}, fmt.Errorf("scan access credential family: %w", err)
		}
		familyIDs = append(familyIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ContainmentResult{}, fmt.Errorf("iterate access credential families: %w", err)
	}
	rows.Close()

	result := ContainmentResult{}
	if len(familyIDs) > 0 {
		versionTag, err := tx.Exec(ctx, `
			UPDATE api_keys
			SET status='disabled',auth_epoch=auth_epoch+1,updated_at=$2
			WHERE access_credential_id=ANY($1::bigint[]) AND status<>'disabled'`, familyIDs, at)
		if err != nil {
			return ContainmentResult{}, fmt.Errorf("disable access credential versions: %w", err)
		}
		result.SecretVersions = versionTag.RowsAffected()

		familyTag, err := tx.Exec(ctx, `
			UPDATE access_credentials
			SET status='revoked',revoked_by=$2,revoked_at=COALESCE(revoked_at,$3),updated_at=$3
			WHERE id=ANY($1::bigint[]) AND status<>'revoked'`, familyIDs, actorUserID, at)
		if err != nil {
			return ContainmentResult{}, fmt.Errorf("revoke access credential families: %w", err)
		}
		result.CredentialFamilies = familyTag.RowsAffected()
	}
	if legacyPredicate != "FALSE" {
		legacyTag, err := tx.Exec(ctx, fmt.Sprintf(`
			UPDATE api_keys ak
			SET status='disabled',auth_epoch=auth_epoch+1,updated_at=$%d
			WHERE %s AND ak.status<>'disabled'`, len(legacyArgs)+1, legacyPredicate), append(legacyArgs, at)...)
		if err != nil {
			return ContainmentResult{}, fmt.Errorf("disable legacy access keys: %w", err)
		}
		result.LegacyKeys = legacyTag.RowsAffected()
	}
	return result, nil
}
