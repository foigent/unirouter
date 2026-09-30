package admin

import (
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/middleware"
)

func (h *Handler) sourceEnrollmentDiscoveryTx(r *http.Request, identity middleware.XInsightMachineIdentity) (pgx.Tx, error) {
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		return nil, err
	}
	var org, anchor int64
	var deployment, source, desired, observed string
	err = tx.QueryRow(r.Context(), `SELECT org_id,project_id,x_deployment_id,source_instance,desired_state,observed_state
 FROM xinsight_connector_installations WHERE installation_id=$1 FOR SHARE`, identity.InstallationID).
		Scan(&org, &anchor, &deployment, &source, &desired, &observed)
	if err != nil || org != identity.OrgID || anchor != identity.AnchorProjectID ||
		deployment != identity.XDeploymentID || source != identity.SourceInstance ||
		desired != "active" || observed == "disconnected" {
		tx.Rollback(r.Context())
		return nil, errCommittingActorAuthorizationChanged
	}
	var keyStatus, purpose string
	err = tx.QueryRow(r.Context(), `SELECT ak.status,ak.purpose FROM api_keys ak
 JOIN xinsight_machine_credentials mc ON mc.api_key_id=ak.id
 WHERE ak.id=$1 AND ak.org_id=$2 AND ak.project_id=$3 AND mc.installation_id=$4
 AND mc.source_instance=$5 AND mc.x_deployment_id=$6 FOR SHARE OF ak`,
		identity.KeyID, identity.OrgID, identity.AnchorProjectID, identity.InstallationID,
		identity.SourceInstance, identity.XDeploymentID).Scan(&keyStatus, &purpose)
	if err != nil || keyStatus != "feed_active" || purpose != sourceEnrollPurpose {
		tx.Rollback(r.Context())
		return nil, errCommittingActorAuthorizationChanged
	}
	return tx, nil
}

// Only active projects covered by a still-authorized grant are returned.
// project_id is the pagination key; each project nests all eligible grants.
func (h *Handler) sourceEnrollmentProjects(w http.ResponseWriter, r *http.Request, exactProject int64) {
	w.Header().Set("Cache-Control", "no-store")
	identity, ok := middleware.XInsightMachineFromContext(r.Context())
	if !ok || identity.Purpose != sourceEnrollPurpose {
		writeJSON(w, 403, map[string]any{"error": "Enrollment discovery denied"})
		return
	}
	cursor, ok := discoveryCursor(r)
	if !ok || exactProject > 0 && cursor != 0 {
		writeJSON(w, 400, map[string]any{"error": "Invalid cursor"})
		return
	}
	tx, err := h.sourceEnrollmentDiscoveryTx(r, identity)
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "Enrollment discovery denied"})
		return
	}
	defer tx.Rollback(r.Context())
	rows, err := tx.Query(r.Context(), `SELECT p.id,p.name FROM projects p WHERE p.org_id=$1 AND p.status='active'
 AND p.id>$2 AND ($3::bigint=0 OR p.id=$3) AND EXISTS(
  SELECT 1 FROM xinsight_source_enrollments e JOIN users u ON u.id=e.grantor_user_id
  WHERE e.installation_id=$4 AND e.org_id=$1 AND e.state='active'
   AND (e.expires_at IS NULL OR e.expires_at>now())
   AND (e.coverage='all_current_future' OR e.exact_project_id=p.id) AND u.status='active'
   AND NOT EXISTS(SELECT 1 FROM xinsight_source_enrollment_exclusions x
    WHERE x.enrollment_id=e.id AND x.project_id=p.id AND x.excluded)
   AND (u.organization_role='platform_admin' OR (u.org_id=$1 AND u.organization_role='org_admin')
    OR (e.coverage='exact' AND u.org_id=$1 AND EXISTS(
      SELECT 1 FROM project_members pm WHERE pm.project_id=p.id AND pm.user_id=u.id AND pm.role='admin'))))
 ORDER BY p.id LIMIT 101`, identity.OrgID, cursor, exactProject, identity.InstallationID)
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Enrollment discovery failed"})
		return
	}
	projects := []map[string]any{}
	ids := []int64{}
	next := ""
	for rows.Next() {
		var id int64
		var name string
		if err = rows.Scan(&id, &name); err != nil {
			break
		}
		if len(projects) == 100 {
			next = strconv.FormatInt(ids[len(ids)-1], 10)
			break
		}
		ids = append(ids, id)
		projects = append(projects, map[string]any{"project_id": strconv.FormatInt(id, 10), "display_name": name,
			"enrollments": []map[string]any{}})
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "Enrollment discovery failed"})
		return
	}
	if exactProject > 0 && len(projects) == 0 {
		writeJSON(w, 404, map[string]any{"error": "Project not found"})
		return
	}
	if len(ids) > 0 {
		grantRows, queryErr := tx.Query(r.Context(), `SELECT p.id,e.id,e.revision,e.profiles,e.include_content,
 CASE WHEN cardinality(e.profiles)=0 THEN 'not_requested'
  WHEN f.api_key_id IS NULL AND existing.project_id IS NOT NULL THEN 'requires_migration'
  WHEN f.api_key_id IS NULL THEN 'pending'
  WHEN f.state='active' AND fak.status='feed_active' THEN 'active' ELSE 'revoked' END,
 CASE WHEN NOT e.include_content THEN 'not_requested'
  WHEN c.api_key_id IS NULL AND existing_content.project_id IS NOT NULL THEN 'requires_migration'
  WHEN c.api_key_id IS NULL THEN 'pending'
  WHEN c.state='active' AND cak.status='feed_active' THEN 'active' ELSE 'revoked' END
 FROM projects p JOIN xinsight_source_enrollments e ON e.org_id=p.org_id
 JOIN users u ON u.id=e.grantor_user_id
 LEFT JOIN xinsight_source_enrollment_bindings f ON f.enrollment_id=e.id AND f.project_id=p.id AND f.kind='facts'
 LEFT JOIN xinsight_source_enrollment_bindings c ON c.enrollment_id=e.id AND c.project_id=p.id AND c.kind='content'
 LEFT JOIN api_keys fak ON fak.id=f.api_key_id
 LEFT JOIN api_keys cak ON cak.id=c.api_key_id
 LEFT JOIN execution_source_connections existing ON existing.org_id=p.org_id AND existing.project_id=p.id
 LEFT JOIN execution_source_content_scopes existing_content ON existing_content.org_id=p.org_id AND existing_content.project_id=p.id
 WHERE p.id=ANY($1::bigint[]) AND e.installation_id=$2 AND e.state='active'
 AND (e.expires_at IS NULL OR e.expires_at>now()) AND (e.coverage='all_current_future' OR e.exact_project_id=p.id)
 AND NOT EXISTS(SELECT 1 FROM xinsight_source_enrollment_exclusions x
  WHERE x.enrollment_id=e.id AND x.project_id=p.id AND x.excluded)
 AND u.status='active' AND (u.organization_role='platform_admin' OR (u.org_id=p.org_id AND u.organization_role='org_admin')
  OR (e.coverage='exact' AND u.org_id=p.org_id AND EXISTS(
    SELECT 1 FROM project_members pm WHERE pm.project_id=p.id AND pm.user_id=u.id AND pm.role='admin')))
 ORDER BY p.id,e.id`, ids, identity.InstallationID)
		if queryErr != nil {
			writeJSON(w, 503, map[string]any{"error": "Enrollment discovery failed"})
			return
		}
		byID := make(map[int64]int, len(ids))
		for i, id := range ids {
			byID[id] = i
		}
		for grantRows.Next() {
			var projectID, enrollmentID, revision int64
			var profiles []string
			var content bool
			var factsStatus, contentStatus string
			if queryErr = grantRows.Scan(&projectID, &enrollmentID, &revision, &profiles, &content, &factsStatus, &contentStatus); queryErr != nil {
				break
			}
			entry := map[string]any{"enrollment_id": "xise_" + strconv.FormatInt(enrollmentID, 10),
				"revision": strconv.FormatInt(revision, 10), "profiles": profiles, "include_content": content,
				"facts_status": factsStatus, "content_status": contentStatus}
			index := byID[projectID]
			projects[index]["enrollments"] = append(projects[index]["enrollments"].([]map[string]any), entry)
		}
		if queryErr == nil {
			queryErr = grantRows.Err()
		}
		grantRows.Close()
		if queryErr != nil {
			writeJSON(w, 503, map[string]any{"error": "Enrollment discovery failed"})
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeJSON(w, 503, map[string]any{"error": "Enrollment discovery failed"})
		return
	}
	if exactProject > 0 {
		writeJSON(w, 200, projects[0])
		return
	}
	writeJSON(w, 200, map[string]any{"schema_version": "unirouter.xinsight.source-enrollment-projects.v1",
		"installation_id": identity.InstallationID, "source_instance": identity.SourceInstance,
		"org_id": strconv.FormatInt(identity.OrgID, 10), "projects": projects, "next_cursor": next})
}

func (h *Handler) ListXInsightMachineEnrollmentProjects(w http.ResponseWriter, r *http.Request) {
	h.sourceEnrollmentProjects(w, r, 0)
}

func (h *Handler) GetXInsightMachineEnrollmentProject(w http.ResponseWriter, r *http.Request) {
	projectID, ok := parsePositivePathID(w, r, "project_id", "Invalid project ID")
	if !ok {
		return
	}
	h.sourceEnrollmentProjects(w, r, projectID)
}
