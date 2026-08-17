//go:build integration

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestProjectMemberUpsertTargetVisibilityIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	insertOrg := func(name string) int64 {
		t.Helper()
		var orgID int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO orgs (name,status,org_type) VALUES ($1,'active','standard') RETURNING id`, name,
		).Scan(&orgID))
		return orgID
	}
	insertProject := func(orgID int64) int64 {
		t.Helper()
		var projectID int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO projects (org_id,name,slug,status)
			 VALUES ($1,'Target Visibility','target-visibility','active') RETURNING id`, orgID,
		).Scan(&projectID))
		return projectID
	}
	insertUser := func(orgID int64, username, organizationRole, compatibilityRole, status string) int64 {
		t.Helper()
		var userID int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
			 VALUES ($1,$2,'hash',$3,$4,$5) RETURNING id`,
			orgID, username, organizationRole, compatibilityRole, status,
		).Scan(&userID))
		return userID
	}

	orgA := insertOrg("target-visibility-a")
	orgB := insertOrg("target-visibility-b")
	projectID := insertProject(orgA)
	projectAdmin := insertUser(orgA, "target-visibility-project-admin", auth.RoleMember, auth.RoleProjectAdmin, "active")
	orgAdmin := insertUser(orgA, "target-visibility-org-admin", auth.RoleOrgAdmin, auth.RoleOrgAdmin, "active")
	platformAdmin := insertUser(orgB, "target-visibility-platform-admin", auth.RolePlatformAdmin, auth.RolePlatformAdmin, "active")
	inactiveNonmember := insertUser(orgA, "target-visibility-inactive-nonmember", auth.RoleMember, auth.RoleMember, "inactive")
	inactiveExistingMember := insertUser(orgA, "target-visibility-inactive-member", auth.RoleMember, auth.RoleDeveloper, "inactive")
	foreignUser := insertUser(orgB, "target-visibility-foreign", auth.RoleMember, auth.RoleMember, "active")

	_, err := pool.Exec(ctx,
		`INSERT INTO project_members (project_id,user_id,role) VALUES
		 ($1,$2,'project_admin'),($1,$3,'project_user')`,
		projectID, projectAdmin, inactiveExistingMember,
	)
	require.NoError(t, err)

	handler := NewHandler(pool, "target-visibility-secret", []byte("0123456789abcdef01234567"), nil)
	serveUpsert := func(actorID, targetUserID int64) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPut, "/projects/members", bytes.NewBufferString(`{"role":"project_user"}`))
		request.Header.Set("Content-Type", "application/json")
		request = withIntegrationCommittingActor(t, pool, request, actorID)
		request.SetPathValue("id", fmt.Sprint(projectID))
		request.SetPathValue("user_id", fmt.Sprint(targetUserID))
		response := httptest.NewRecorder()
		handler.UpsertProjectMember(response, request)
		return response
	}
	readCounts := func() (memberships, audits, outbox int) {
		t.Helper()
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM project_members`).Scan(&memberships))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs`).Scan(&audits))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM internal_outbox_events`).Scan(&outbox))
		return memberships, audits, outbox
	}

	beforeMemberships, beforeAudits, beforeOutbox := readCounts()
	absent := serveUpsert(projectAdmin, foreignUser+1_000_000)
	foreign := serveUpsert(projectAdmin, foreignUser)
	inactiveHidden := serveUpsert(projectAdmin, inactiveNonmember)
	for _, response := range []*httptest.ResponseRecorder{absent, foreign, inactiveHidden} {
		require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
		require.Equal(t, []byte("{\"error\":\"User not found\"}\n"), response.Body.Bytes())
	}
	require.Equal(t, absent.Body.Bytes(), foreign.Body.Bytes())
	require.Equal(t, absent.Body.Bytes(), inactiveHidden.Body.Bytes())
	afterMemberships, afterAudits, afterOutbox := readCounts()
	require.Equal(t, beforeMemberships, afterMemberships, "target probes must not change project memberships")
	require.Equal(t, beforeAudits, afterAudits, "target probes must not append audit rows")
	require.Equal(t, beforeOutbox, afterOutbox, "target probes must not enqueue outbox rows")

	inactiveMemberResponse := serveUpsert(projectAdmin, inactiveExistingMember)
	require.Equal(t, http.StatusConflict, inactiveMemberResponse.Code, inactiveMemberResponse.Body.String())
	require.JSONEq(t, `{"error":"Only active accounts can receive project membership"}`, inactiveMemberResponse.Body.String())
	var retainedMembershipRole string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, inactiveExistingMember,
	).Scan(&retainedMembershipRole))
	require.Equal(t, auth.ProjectRoleUser, retainedMembershipRole)

	for _, actorID := range []int64{orgAdmin, platformAdmin} {
		response := serveUpsert(actorID, inactiveNonmember)
		require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
		require.JSONEq(t, `{"error":"Only active accounts can receive project membership"}`, response.Body.String())
	}
	finalMemberships, finalAudits, finalOutbox := readCounts()
	require.Equal(t, beforeMemberships, finalMemberships)
	require.Equal(t, beforeAudits, finalAudits)
	require.Equal(t, beforeOutbox, finalOutbox)

	listRequest := httptest.NewRequest(http.MethodGet, "/projects/members", nil)
	listRequest = withIntegrationCommittingActor(t, pool, listRequest, projectAdmin)
	listRequest.SetPathValue("id", fmt.Sprint(projectID))
	listResponse := httptest.NewRecorder()
	handler.ListProjectMembers(listResponse, listRequest)
	require.Equal(t, http.StatusOK, listResponse.Code, listResponse.Body.String())
	var listPayload struct {
		EligibleUsers []struct {
			UserID int64  `json:"user_id"`
			Status string `json:"status"`
		} `json:"eligible_users"`
	}
	require.NoError(t, json.Unmarshal(listResponse.Body.Bytes(), &listPayload))
	for _, candidate := range listPayload.EligibleUsers {
		require.Equal(t, "active", candidate.Status)
		require.NotEqual(t, inactiveNonmember, candidate.UserID)
		require.NotEqual(t, inactiveExistingMember, candidate.UserID)
	}
}
