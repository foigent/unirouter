package admin

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/middleware"
)

type machineDiscoveryGrant struct {
	id, revision, projectID, userID int64
	kind, coverage                  string
	read, write                     bool
}

func (h *Handler) machineDiscoveryTx(r *http.Request, identity middleware.XInsightMachineIdentity) (pgx.Tx, []machineDiscoveryGrant, error) {
	tx, err := h.db.Begin(r.Context())
	if err != nil {
		return nil, nil, err
	}
	var desired, observed, source, deployment string
	var anchor int64
	err = tx.QueryRow(r.Context(), `SELECT desired_state,observed_state,source_instance,x_deployment_id,project_id
 FROM xinsight_connector_installations WHERE installation_id=$1 AND org_id=$2 FOR SHARE`,
		identity.InstallationID, identity.OrgID).Scan(&desired, &observed, &source, &deployment, &anchor)
	if err != nil || desired != "active" || observed == "disconnected" || source != identity.SourceInstance ||
		deployment != identity.XDeploymentID || anchor != identity.AnchorProjectID {
		tx.Rollback(r.Context())
		return nil, nil, errCommittingActorAuthorizationChanged
	}
	var status, purpose string
	err = tx.QueryRow(r.Context(), `SELECT ak.status,ak.purpose FROM api_keys ak
 JOIN xinsight_machine_credentials mc ON mc.api_key_id=ak.id
 WHERE ak.id=$1 AND ak.org_id=$2 AND ak.project_id=$3 AND mc.installation_id=$4
 AND mc.source_instance=$5 AND mc.x_deployment_id=$6 FOR SHARE OF ak`,
		identity.KeyID, identity.OrgID, identity.AnchorProjectID, identity.InstallationID,
		identity.SourceInstance, identity.XDeploymentID).Scan(&status, &purpose)
	if err != nil || status != "feed_active" || purpose != policyManagePurpose {
		tx.Rollback(r.Context())
		return nil, nil, errCommittingActorAuthorizationChanged
	}
	rows, err := tx.Query(r.Context(), `SELECT d.id,d.revision,d.target_kind,d.coverage,
 COALESCE(d.exact_project_id,0),COALESCE(d.exact_user_id,0),d.can_read,d.can_write
 FROM xinsight_policy_delegations d JOIN users u ON u.id=d.grantor_user_id
 WHERE d.installation_id=$1 AND d.org_id=$2 AND d.state='active' AND d.can_read
 AND (d.expires_at IS NULL OR d.expires_at>now()) AND u.status='active'
 AND (u.organization_role='platform_admin' OR (u.org_id=$2 AND u.organization_role='org_admin')
      OR (d.coverage='exact' AND d.exact_project_id IS NOT NULL
       AND EXISTS(SELECT 1 FROM project_members pm WHERE pm.project_id=d.exact_project_id
                  AND pm.user_id=u.id AND pm.role='admin')))
 AND (d.exact_project_id IS NULL OR EXISTS(SELECT 1 FROM projects p
      WHERE p.id=d.exact_project_id AND p.org_id=$2 AND p.status='active'))
 AND (d.exact_user_id IS NULL OR EXISTS(SELECT 1 FROM users target JOIN project_members pm
      ON pm.user_id=target.id AND pm.project_id=d.exact_project_id
      WHERE target.id=d.exact_user_id AND target.org_id=$2 AND target.status='active'))
 ORDER BY d.id`, identity.InstallationID, identity.OrgID)
	if err != nil {
		tx.Rollback(r.Context())
		return nil, nil, err
	}
	grants := []machineDiscoveryGrant{}
	for rows.Next() {
		var g machineDiscoveryGrant
		if err = rows.Scan(&g.id, &g.revision, &g.kind, &g.coverage, &g.projectID, &g.userID, &g.read, &g.write); err != nil {
			break
		}
		grants = append(grants, g)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		tx.Rollback(r.Context())
		return nil, nil, err
	}
	return tx, grants, nil
}

func discoveryCursor(r *http.Request) (int64, bool) {
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return 0, true
	}
	return positiveIDOrZero(raw)
}

func (h *Handler) GetXInsightMachinePolicyScopes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	identity, ok := middleware.XInsightMachineFromContext(r.Context())
	if !ok || identity.Purpose != policyManagePurpose {
		writeJSON(w, 403, map[string]any{"error": "Machine policy discovery denied"})
		return
	}
	cursor, ok := discoveryCursor(r)
	if !ok {
		writeJSON(w, 400, map[string]any{"error": "Invalid cursor"})
		return
	}
	tx, grants, err := h.machineDiscoveryTx(r, identity)
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "Machine policy discovery denied"})
		return
	}
	defer tx.Rollback(r.Context())
	visible := []map[string]any{}
	anyProject := false
	for _, g := range grants {
		actions := []string{"read"}
		if g.write {
			actions = append(actions, "write")
		}
		visible = append(visible, map[string]any{"grant_id": "xipg_" + strconv.FormatInt(g.id, 10),
			"revision": strconv.FormatInt(g.revision, 10), "target_kind": g.kind,
			"coverage": g.coverage, "project_id": idOrEmpty(g.projectID), "user_id": idOrEmpty(g.userID), "actions": actions})
		if g.kind != "enterprise_default" {
			anyProject = true
		}
	}
	projects := []map[string]string{}
	next := ""
	if anyProject {
		rows, queryErr := tx.Query(r.Context(), `SELECT p.id,p.name FROM projects p
 WHERE p.org_id=$1 AND p.status='active' AND p.id>$2 AND EXISTS(
   SELECT 1 FROM xinsight_policy_delegations d JOIN users u ON u.id=d.grantor_user_id
   WHERE d.installation_id=$3 AND d.org_id=$1 AND d.state='active' AND d.can_read
    AND d.target_kind IN ('project_policy','user_exception')
    AND (d.expires_at IS NULL OR d.expires_at>now()) AND u.status='active'
    AND (d.coverage='all_current_future' OR d.exact_project_id=p.id)
    AND (u.organization_role='platform_admin' OR (u.org_id=$1 AND u.organization_role='org_admin')
       OR (d.coverage='exact' AND EXISTS(SELECT 1 FROM project_members pm
           WHERE pm.project_id=p.id AND pm.user_id=u.id AND pm.role='admin'))))
 ORDER BY p.id LIMIT 101`, identity.OrgID, cursor, identity.InstallationID)
		if queryErr != nil {
			writeJSON(w, 503, map[string]any{"error": "Project discovery failed"})
			return
		}
		for rows.Next() {
			var id int64
			var name string
			if queryErr = rows.Scan(&id, &name); queryErr != nil {
				break
			}
			if len(projects) == 100 {
				next = projects[len(projects)-1]["project_id"]
				break
			}
			projects = append(projects, map[string]string{"project_id": strconv.FormatInt(id, 10), "display_name": name})
		}
		if queryErr == nil {
			queryErr = rows.Err()
		}
		rows.Close()
		if queryErr != nil {
			writeJSON(w, 503, map[string]any{"error": "Project discovery failed"})
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeJSON(w, 503, map[string]any{"error": "Project discovery failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"schema_version": "unirouter.xinsight.policy-scopes.v1", "installation_id": identity.InstallationID,
		"source_instance": identity.SourceInstance, "org_id": strconv.FormatInt(identity.OrgID, 10), "grants": visible,
		"projects": projects, "next_cursor": next, "observed_at": time.Now().UTC().Format(time.RFC3339)})
}

func idOrEmpty(id int64) string {
	if id <= 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

func (h *Handler) GetXInsightMachinePolicyUsers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	identity, ok := middleware.XInsightMachineFromContext(r.Context())
	projectID, projectOK := parsePositivePathID(w, r, "project_id", "Invalid project ID")
	if !projectOK {
		return
	}
	if !ok || identity.Purpose != policyManagePurpose {
		writeJSON(w, 403, map[string]any{"error": "Machine policy discovery denied"})
		return
	}
	cursor, ok := discoveryCursor(r)
	if !ok {
		writeJSON(w, 400, map[string]any{"error": "Invalid cursor"})
		return
	}
	tx, grants, err := h.machineDiscoveryTx(r, identity)
	if err != nil {
		writeJSON(w, 403, map[string]any{"error": "Machine policy discovery denied"})
		return
	}
	defer tx.Rollback(r.Context())
	var orgID int64
	var status string
	err = tx.QueryRow(r.Context(), `SELECT org_id,status FROM projects WHERE id=$1 FOR SHARE`, projectID).Scan(&orgID, &status)
	if err != nil || orgID != identity.OrgID || status != "active" {
		writeJSON(w, 404, map[string]any{"error": "Project not found"})
		return
	}
	allowed := false
	broad := false
	exactUsers := []int64{}
	for _, g := range grants {
		if g.kind == "user_exception" && (g.coverage == "all_current_future" || g.projectID == projectID) {
			allowed = true
			if g.coverage == "all_current_future" {
				broad = true
			} else {
				exactUsers = append(exactUsers, g.userID)
			}
		}
	}
	if !allowed {
		writeJSON(w, 404, map[string]any{"error": "Project not found"})
		return
	}
	rows, err := tx.Query(r.Context(), `SELECT u.id,u.username FROM users u JOIN project_members pm ON pm.user_id=u.id
 WHERE pm.project_id=$1 AND u.org_id=$2 AND u.status='active' AND u.id>$3
 AND ($4::boolean OR u.id=ANY($5::bigint[])) ORDER BY u.id LIMIT 101`,
		projectID, identity.OrgID, cursor, broad, exactUsers)
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "User discovery failed"})
		return
	}
	users := []map[string]string{}
	next := ""
	for rows.Next() {
		var id int64
		var name string
		if err = rows.Scan(&id, &name); err != nil {
			break
		}
		if len(users) == 100 {
			next = users[len(users)-1]["user_id"]
			break
		}
		users = append(users, map[string]string{"user_id": strconv.FormatInt(id, 10), "display_name": name})
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "User discovery failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"schema_version": "unirouter.xinsight.policy-users.v1", "project_id": strconv.FormatInt(projectID, 10),
		"users": users, "next_cursor": next})
}
