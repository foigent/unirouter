package xinsightconnector

import (
	"context"
	"database/sql"
	"fmt"
)

// ReadCompletedRedeemOriginTx is a current read inside the caller's existing
// product transaction. It never promotes historical originless installations.
func ReadCompletedRedeemOriginTx(ctx context.Context, tx PhaseProductTx, installationID, deploymentID string) (string, error) {
	if tx == nil || !validExactExternalIdentity(installationID) || !validExactDeploymentIdentity(deploymentID) {
		return "", ErrInvalid
	}
	var expanded bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version='000124_xinsight_receiver_adoption')`).Scan(&expanded); err != nil {
		return "", fmt.Errorf("%w: read origin schema qualification", ErrUnavailable)
	}
	if !expanded {
		return "", ErrConflict
	}
	var origin sql.NullString
	err := tx.QueryRow(ctx, `SELECT b.redeemed_origin
FROM xinsight_connector_installations i JOIN xinsight_connector_control_bindings b
 ON b.installation_id=i.installation_id AND b.org_id=i.org_id AND b.project_id=i.project_id
WHERE i.installation_id=$1 AND i.x_deployment_id=$2 AND i.desired_state='active'
 AND i.observed_state<>'disconnected' AND b.invitation_token_enc IS NULL
 AND b.control_token_enc IS NOT NULL AND b.control_token_sha256 IS NOT NULL
 AND b.control_project_id IS NOT NULL AND b.control_token_expires_at>clock_timestamp()
 AND b.redeem_request_sha256 IS NOT NULL AND b.redeem_result_sha256 IS NOT NULL`,
		installationID, deploymentID).Scan(&origin)
	if err != nil || !origin.Valid || !validFrozenHTTPSOrigin(origin.String) {
		return "", ErrConflict
	}
	return origin.String, nil
}
