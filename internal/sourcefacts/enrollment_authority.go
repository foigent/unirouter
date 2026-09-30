package sourcefacts

import (
	"context"
	"github.com/jackc/pgx/v5"
)

const AutoEnrollmentPolicyLabel = "xinsight_auto_enrollment"

// The marker is assigned only to M123-created A/C credentials. Older manual
// credentials never query M123 tables, preserving the predecessor bridge.
// Each read obtains a fresh database snapshot; revocation is effective on the
// next read rather than waiting for key cleanup or connector polling.
func enrollmentReadAllowedTx(ctx context.Context, tx pgx.Tx, keyID, orgID, projectID int64, instance, kind, label string) (bool, error) {
	if label != AutoEnrollmentPolicyLabel {
		return true, nil
	}
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
 SELECT 1 FROM xinsight_source_enrollment_bindings b
 JOIN xinsight_source_enrollments e ON e.id=b.enrollment_id AND e.org_id=b.org_id
 JOIN xinsight_connector_installations i ON i.installation_id=e.installation_id
 JOIN projects anchor ON anchor.id=i.project_id AND anchor.org_id=i.org_id
 JOIN users u ON u.id=e.grantor_user_id
 JOIN orgs o ON o.id=e.org_id
 JOIN projects p ON p.id=b.project_id AND p.org_id=e.org_id
 WHERE b.api_key_id=$1 AND b.org_id=$2 AND b.project_id=$3 AND b.kind=$4 AND b.state='active'
 AND b.source_instance=$5 AND e.state='active' AND (e.expires_at IS NULL OR e.expires_at>now())
 AND (e.coverage='all_current_future' OR e.exact_project_id=b.project_id)
 AND NOT EXISTS(SELECT 1 FROM xinsight_source_enrollment_exclusions x
  WHERE x.enrollment_id=e.id AND x.project_id=b.project_id AND x.excluded)
 AND i.org_id=e.org_id AND i.project_id=e.anchor_project_id
 AND i.desired_state='active' AND i.observed_state<>'disconnected' AND i.source_instance=$5
 AND anchor.status='active'
 AND o.status='active' AND p.status='active' AND u.status='active'
 AND (u.organization_role='platform_admin' OR (u.org_id=e.org_id AND u.organization_role='org_admin')
  OR (e.coverage='exact' AND u.org_id=e.org_id AND EXISTS(
   SELECT 1 FROM project_members pm WHERE pm.project_id=b.project_id AND pm.user_id=u.id AND pm.role='admin')))
 )`, keyID, orgID, projectID, kind, instance).Scan(&allowed)
	return allowed, err
}
