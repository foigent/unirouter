package admin

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestV1ProjectMembershipAndBudgetIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	testutil.TruncateAll(t, pool)
	var orgA, orgB int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type) VALUES ('v1-project-org-a', 'active', 'standard') RETURNING id`,
	).Scan(&orgA))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name, status, org_type) VALUES ('v1-project-org-b', 'active', 'standard') RETURNING id`,
	).Scan(&orgB))

	insertUser := func(orgID int64, username, role string) int64 {
		t.Helper()
		var userID int64
		organizationRole, ok := auth.NormalizeOrganizationRole(role)
		require.True(t, ok)
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO users (org_id, username, password_hash, organization_role, role, status)
			 VALUES ($1, $2, 'hash', $3, $4, 'active') RETURNING id`,
			orgID, username, organizationRole, role,
		).Scan(&userID))
		return userID
	}
	insertProject := func(orgID int64, name, slug string) int64 {
		t.Helper()
		var projectID int64
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO projects (org_id, name, slug, status, budget_cents, budget_period)
			 VALUES ($1, $2, $3, 'active', 0, 'monthly') RETURNING id`,
			orgID, name, slug,
		).Scan(&projectID))
		return projectID
	}

	orgAdminA := insertUser(orgA, "v1-project-org-admin-a", auth.RoleOrgAdmin)
	orgAdminWithoutMembership := insertUser(orgA, "v1-project-org-admin-no-membership", auth.RoleOrgAdmin)
	billingA := insertUser(orgA, "v1-project-billing-a", auth.RoleBillingAdmin)
	orgViewerA := insertUser(orgA, "v1-project-org-viewer-a", auth.RoleOrgViewer)
	projectAdminA := insertUser(orgA, "v1-project-admin-a", auth.RoleProjectAdmin)
	projectAdminTarget := insertUser(orgA, "v1-project-admin-target", auth.RoleProjectAdmin)
	developerA := insertUser(orgA, "v1-project-developer-a", auth.RoleDeveloper)
	foreignDeveloper := insertUser(orgB, "v1-project-developer-b", auth.RoleDeveloper)
	projectA1 := insertProject(orgA, "V1 Project A1", "v1-project-a1")
	projectA2 := insertProject(orgA, "V1 Project A2", "v1-project-a2")
	projectB := insertProject(orgB, "V1 Project B", "v1-project-b")
	_, err := pool.Exec(ctx,
		`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'project_admin')`,
		projectA1, projectAdminA,
	)
	require.NoError(t, err)
	h := NewHandler(pool, "v1-project-membership-secret", []byte("0123456789abcdef01234567"), nil)

	request := func(method, target, body string, actorID, actorOrgID int64, role string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = withTestActor(req, actorID, actorOrgID, role)
		rec := httptest.NewRecorder()
		switch {
		case method == http.MethodPost && target == "/users":
			h.CreateUser(rec, req)
		case method == http.MethodPut && len(target) > len("/users/") && target[:len("/users/")] == "/users/":
			var id int64
			_, _ = fmt.Sscanf(target, "/users/%d", &id)
			req.SetPathValue("id", fmt.Sprintf("%d", id))
			h.UpdateUser(rec, req)
		}
		return rec
	}

	t.Run("local account creation defaults to member and separates project membership", func(t *testing.T) {
		rec := request(http.MethodPost, "/users",
			fmt.Sprintf(`{"org_id":%d,"username":"v1-created-member","password":"secret-password-123"}`, orgA),
			orgAdminA, orgA, auth.RoleOrgAdmin)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		userID := responseID(t, rec.Body.Bytes())
		var organizationRole, compatibilityRole string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT organization_role,role FROM users WHERE id=$1`, userID,
		).Scan(&organizationRole, &compatibilityRole))
		require.Equal(t, auth.RoleMember, organizationRole)
		require.Equal(t, auth.RoleMember, compatibilityRole)
		var membershipCount int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM project_members WHERE user_id=$1`, userID).Scan(&membershipCount))
		require.Zero(t, membershipCount)

		rec = request(http.MethodPost, "/users",
			fmt.Sprintf(`{"org_id":%d,"username":"v1-cross-org-viewer","password":"secret-password-123","role":"viewer","project_ids":[%d]}`, orgA, projectB),
			orgAdminA, orgA, auth.RoleOrgAdmin)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "project_membership_separate")
		var crossOrgUsers int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE org_id=$1 AND username='v1-cross-org-viewer'`, orgA).Scan(&crossOrgUsers))
		require.Zero(t, crossOrgUsers)
	})

	t.Run("account updates cannot rewrite the project axis", func(t *testing.T) {
		rec := request(http.MethodPut, fmt.Sprintf("/users/%d", developerA), `{"project_ids":[]}`, orgAdminA, orgA, auth.RoleOrgAdmin)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "project_membership_separate")
	})

	t.Run("organization roles may receive explicit non-propagating memberships", func(t *testing.T) {
		upsert := func(userID, projectID int64, role string) *httptest.ResponseRecorder {
			req := withTestActor(httptest.NewRequest(http.MethodPut, "/members", bytes.NewBufferString(fmt.Sprintf(`{"role":%q}`, role))), orgAdminA, orgA, auth.RoleOrgAdmin)
			req.SetPathValue("id", fmt.Sprintf("%d", projectID))
			req.SetPathValue("user_id", fmt.Sprintf("%d", userID))
			rec := httptest.NewRecorder()
			h.UpsertProjectMember(rec, req)
			return rec
		}
		require.Equal(t, http.StatusOK, upsert(orgAdminA, projectA1, auth.ProjectRoleUser).Code)
		require.Equal(t, http.StatusOK, upsert(orgViewerA, projectA1, auth.ProjectRoleUser).Code)
		require.Equal(t, http.StatusOK, upsert(billingA, projectA1, auth.ProjectRoleViewer).Code)

		for _, userID := range []int64{orgAdminA, orgViewerA, billingA} {
			var organizationRole, compatibilityRole string
			require.NoError(t, pool.QueryRow(ctx, `SELECT organization_role,role FROM users WHERE id=$1`, userID).Scan(&organizationRole, &compatibilityRole))
			require.Equal(t, organizationRole, compatibilityRole, "organization-role compatibility shadow changed for user %d", userID)
			var otherProjectMemberships int
			require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM project_members WHERE user_id=$1 AND project_id=$2`, userID, projectA2).Scan(&otherProjectMemberships))
			require.Zero(t, otherProjectMemberships)
		}

		orgViewerRequest := withTestActor(httptest.NewRequest(http.MethodGet, "/projects", nil), orgViewerA, orgA, auth.RoleOrgViewer)
		require.True(t, h.canReadProject(orgViewerRequest.Context(), projectA1))
		require.True(t, h.canCreateProjectKey(orgViewerRequest.Context(), projectA1))
		require.True(t, h.canReadProject(orgViewerRequest.Context(), projectA2), "organization viewers read ordinary project metadata without membership")
		require.False(t, h.canCreateProjectKey(orgViewerRequest.Context(), projectA2), "explicit project capability must not propagate")
		require.False(t, h.canManageProjectMembers(orgViewerRequest.Context(), projectA1))

		billingRequest := withTestActor(httptest.NewRequest(http.MethodGet, "/projects", nil), billingA, orgA, auth.RoleBillingAdmin)
		require.True(t, h.canReadProject(billingRequest.Context(), projectA1))
		require.False(t, h.canWriteProject(billingRequest.Context(), projectA1))
		require.False(t, h.canCreateProjectKey(billingRequest.Context(), projectA1))

		orgAdminRequest := withTestActor(httptest.NewRequest(http.MethodGet, "/projects", nil), orgAdminWithoutMembership, orgA, auth.RoleOrgAdmin)
		require.True(t, h.canWriteProject(orgAdminRequest.Context(), projectA1), "existing org-admin authority must not require automatic membership")
		var orgAdminMemberships int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM project_members WHERE user_id=$1`, orgAdminWithoutMembership).Scan(&orgAdminMemberships))
		require.Zero(t, orgAdminMemberships)
	})

	t.Run("compatibility shadow cannot leak a stronger role across projects", func(t *testing.T) {
		heterogeneousMember := insertUser(orgA, "v1-heterogeneous-member", auth.RoleMember)
		upsert := func(projectID int64, role string) {
			req := withTestActor(httptest.NewRequest(http.MethodPut, "/members", bytes.NewBufferString(fmt.Sprintf(`{"role":%q}`, role))), orgAdminA, orgA, auth.RoleOrgAdmin)
			req.SetPathValue("id", fmt.Sprintf("%d", projectID))
			req.SetPathValue("user_id", fmt.Sprintf("%d", heterogeneousMember))
			rec := httptest.NewRecorder()
			h.UpsertProjectMember(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		}
		upsert(projectA1, auth.ProjectRoleAdmin)
		var authEpochBeforeViewer int64
		require.NoError(t, pool.QueryRow(ctx, `SELECT auth_epoch FROM users WHERE id=$1`, heterogeneousMember).Scan(&authEpochBeforeViewer))
		_, err := pool.Exec(ctx,
			`INSERT INTO admin_sessions (user_id, sid_hash, issued_at, expires_at)
			 VALUES ($1, decode(repeat('ab', 32), 'hex'), NOW(), NOW() + INTERVAL '1 hour')`,
			heterogeneousMember,
		)
		require.NoError(t, err)
		upsert(projectA2, auth.ProjectRoleViewer)

		var compatibilityRole string
		var authEpochAfterViewer int64
		require.NoError(t, pool.QueryRow(ctx, `SELECT role,auth_epoch FROM users WHERE id=$1`, heterogeneousMember).Scan(&compatibilityRole, &authEpochAfterViewer))
		require.Equal(t, auth.RoleViewer, compatibilityRole, "rolling-upgrade shadow must be no stronger than every live membership")
		require.Equal(t, authEpochBeforeViewer+1, authEpochAfterViewer)
		var activeSessions int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM admin_sessions WHERE user_id=$1 AND revoked_at IS NULL AND expires_at > NOW()`,
			heterogeneousMember,
		).Scan(&activeSessions))
		require.Zero(t, activeSessions, "a weaker compatibility shadow must revoke previously issued sessions")
		actor := withTestActor(httptest.NewRequest(http.MethodGet, "/projects", nil), heterogeneousMember, orgA, compatibilityRole)
		require.True(t, h.canWriteProject(actor.Context(), projectA1))
		require.False(t, h.canWriteProject(actor.Context(), projectA2), "project_admin compatibility shadow leaked write authority into project_viewer membership")
		require.True(t, h.canReadProject(actor.Context(), projectA2))
		require.False(t, h.canCreateProjectKey(actor.Context(), projectA2), "project_viewer membership gained calling authority from the compatibility shadow")
	})

	t.Run("project admin cannot escalate or cross tenant", func(t *testing.T) {
		orgAdminUpsert := withTestActor(
			httptest.NewRequest(http.MethodPut, "/members", bytes.NewBufferString(`{"role":"project_admin"}`)),
			orgAdminA, orgA, auth.RoleOrgAdmin,
		)
		orgAdminUpsert.SetPathValue("id", fmt.Sprintf("%d", projectA1))
		orgAdminUpsert.SetPathValue("user_id", fmt.Sprintf("%d", projectAdminTarget))
		orgAdminRec := httptest.NewRecorder()
		h.UpsertProjectMember(orgAdminRec, orgAdminUpsert)
		require.Equal(t, http.StatusOK, orgAdminRec.Code, orgAdminRec.Body.String())

		serveMember := func(userID int64, body string) *httptest.ResponseRecorder {
			req := withTestActor(httptest.NewRequest(http.MethodPut, "/members", bytes.NewBufferString(body)), projectAdminA, orgA, auth.RoleProjectAdmin)
			req.SetPathValue("id", fmt.Sprintf("%d", projectA1))
			req.SetPathValue("user_id", fmt.Sprintf("%d", userID))
			rec := httptest.NewRecorder()
			h.UpsertProjectMember(rec, req)
			return rec
		}
		require.Equal(t, http.StatusForbidden, serveMember(projectAdminTarget, `{"role":"project_admin"}`).Code)
		start := make(chan struct{})
		results := make(chan int, 2)
		var wg sync.WaitGroup
		for _, nextRole := range []string{auth.ProjectRoleUser, auth.ProjectRoleViewer} {
			wg.Add(1)
			go func(nextRole string) {
				defer wg.Done()
				<-start
				results <- serveMember(projectAdminTarget, fmt.Sprintf(`{"role":%q}`, nextRole)).Code
			}(nextRole)
		}
		close(start)
		wg.Wait()
		close(results)
		for code := range results {
			require.Equal(t, http.StatusForbidden, code)
		}
		var protectedRole string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT role FROM project_members WHERE project_id=$1 AND user_id=$2`, projectA1, projectAdminTarget,
		).Scan(&protectedRole))
		require.Equal(t, auth.ProjectRoleAdmin, protectedRole)
		require.Equal(t, http.StatusForbidden, serveMember(foreignDeveloper, `{"role":"project_developer"}`).Code)
		require.Equal(t, http.StatusOK, serveMember(developerA, `{"role":"project_developer"}`).Code)
	})

	t.Run("concurrent removals permit a least-privilege member with zero projects", func(t *testing.T) {
		concurrentUser := insertUser(orgA, "v1-concurrent-viewer", auth.RoleViewer)
		_, err := pool.Exec(ctx,
			`INSERT INTO project_members (project_id, user_id, role)
			 VALUES ($1, $3, 'project_viewer'), ($2, $3, 'project_viewer')`,
			projectA1, projectA2, concurrentUser,
		)
		require.NoError(t, err)
		start := make(chan struct{})
		codes := make(chan int, 2)
		var wg sync.WaitGroup
		for _, projectID := range []int64{projectA1, projectA2} {
			wg.Add(1)
			go func(projectID int64) {
				defer wg.Done()
				<-start
				req := withTestActor(httptest.NewRequest(http.MethodDelete, "/members", nil), orgAdminA, orgA, auth.RoleOrgAdmin)
				req.SetPathValue("id", fmt.Sprintf("%d", projectID))
				req.SetPathValue("user_id", fmt.Sprintf("%d", concurrentUser))
				rec := httptest.NewRecorder()
				h.DeleteProjectMember(rec, req)
				codes <- rec.Code
			}(projectID)
		}
		close(start)
		wg.Wait()
		close(codes)
		got := map[int]int{}
		for code := range codes {
			got[code]++
		}
		require.Equal(t, 2, got[http.StatusOK], got)
		var memberships int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM project_members WHERE user_id=$1`, concurrentUser).Scan(&memberships))
		require.Zero(t, memberships)
		var organizationRole, compatibilityRole string
		require.NoError(t, pool.QueryRow(ctx, `SELECT organization_role,role FROM users WHERE id=$1`, concurrentUser).Scan(&organizationRole, &compatibilityRole))
		require.Equal(t, auth.RoleMember, organizationRole)
		require.Equal(t, auth.RoleMember, compatibilityRole)
	})

	t.Run("committed membership revoke denies an update that passed preflight", func(t *testing.T) {
		_, err := pool.Exec(ctx,
			`INSERT INTO project_members (project_id, user_id, role)
			 VALUES ($1, $2, 'project_admin')
			 ON CONFLICT (project_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
			projectA2, projectAdminA,
		)
		require.NoError(t, err)

		blocker, err := pool.Begin(ctx)
		require.NoError(t, err)
		blockerReleased := false
		t.Cleanup(func() {
			if !blockerReleased {
				_ = blocker.Rollback(context.Background())
			}
		})
		var blockerPID int
		require.NoError(t, blocker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID))
		var lockedProjectID int64
		require.NoError(t, blocker.QueryRow(ctx,
			`SELECT id FROM projects WHERE id = $1 FOR UPDATE`, projectA1,
		).Scan(&lockedProjectID))

		updateResult := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			req := withTestActor(
				httptest.NewRequest(http.MethodPut, "/projects", bytes.NewBufferString(`{"name":"must-not-commit"}`)),
				projectAdminA, orgA, auth.RoleProjectAdmin,
			)
			req.SetPathValue("id", fmt.Sprintf("%d", projectA1))
			rec := httptest.NewRecorder()
			h.UpdateProject(rec, req)
			updateResult <- rec
		}()

		deadline := time.Now().Add(5 * time.Second)
		for {
			var updateBlocked bool
			require.NoError(t, pool.QueryRow(ctx, `
				SELECT EXISTS (
				    SELECT 1 FROM pg_stat_activity
				    WHERE datname = current_database()
				      AND $1 = ANY(pg_blocking_pids(pid))
				)`, blockerPID).Scan(&updateBlocked))
			if updateBlocked {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("project update did not reach the transaction row lock after membership preflight")
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Commit the authority change while the update is still between its
		// preflight and transaction-level check. A direct write is deliberate:
		// the normal audited revoke also references the project in audit_logs and
		// would wait behind the test-only project-row blocker.
		tag, err := pool.Exec(ctx,
			`DELETE FROM project_members WHERE project_id = $1 AND user_id = $2`,
			projectA1, projectAdminA,
		)
		require.NoError(t, err)
		require.EqualValues(t, 1, tag.RowsAffected())

		require.NoError(t, blocker.Rollback(ctx))
		blockerReleased = true
		select {
		case rec := <-updateResult:
			require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		case <-time.After(5 * time.Second):
			t.Fatal("project update did not finish after releasing the project row lock")
		}

		var projectName string
		require.NoError(t, pool.QueryRow(ctx, `SELECT name FROM projects WHERE id = $1`, projectA1).Scan(&projectName))
		require.Equal(t, "V1 Project A1", projectName)
	})

	t.Run("billing update is field limited tenant bound audited and rollback safe", func(t *testing.T) {
		updateBudget := func(projectID int64, body string) *httptest.ResponseRecorder {
			req := withTestActor(httptest.NewRequest(http.MethodPut, "/budget", bytes.NewBufferString(body)), billingA, orgA, auth.RoleBillingAdmin)
			req.SetPathValue("id", fmt.Sprintf("%d", projectID))
			rec := httptest.NewRecorder()
			h.UpdateProjectBudget(rec, req)
			return rec
		}
		require.Equal(t, http.StatusOK, updateBudget(projectA1, `{"budget_cents":1234,"budget_period":"daily"}`).Code)
		require.Equal(t, http.StatusForbidden, updateBudget(projectB, `{"budget_cents":777}`).Code)
		require.Equal(t, http.StatusBadRequest, updateBudget(projectA1, `{"name":"not-allowed"}`).Code)
		require.Equal(t, http.StatusBadRequest, updateBudget(projectA1, `{"budget_cents":2147483648}`).Code)
		var budget int64
		var period string
		require.NoError(t, pool.QueryRow(ctx, `SELECT budget_cents, budget_period FROM projects WHERE id=$1`, projectA1).Scan(&budget, &period))
		require.Equal(t, int64(1234), budget)
		require.Equal(t, "daily", period)
		var audits int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE action='project.budget.update' AND project_id=$1`, projectA1).Scan(&audits))
		require.Equal(t, 1, audits)

		_, err := pool.Exec(ctx, `
			CREATE FUNCTION v1_reject_project_budget_audit() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN
			  IF NEW.action = 'project.budget.update' THEN RAISE EXCEPTION 'reject budget audit'; END IF;
			  RETURN NEW;
			END $$;
			CREATE TRIGGER v1_reject_project_budget_audit_trigger
			BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION v1_reject_project_budget_audit()`)
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, updateBudget(projectA1, `{"budget_cents":9999}`).Code)
		require.NoError(t, pool.QueryRow(ctx, `SELECT budget_cents FROM projects WHERE id=$1`, projectA1).Scan(&budget))
		require.Equal(t, int64(1234), budget)
	})
}
