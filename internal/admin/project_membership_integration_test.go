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
		require.NoError(t, pool.QueryRow(ctx,
			`INSERT INTO users (org_id, username, password_hash, role, status)
			 VALUES ($1, $2, 'hash', $3, 'active') RETURNING id`,
			orgID, username, role,
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
	billingA := insertUser(orgA, "v1-project-billing-a", auth.RoleBillingAdmin)
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

	t.Run("local project role creation is atomic and tenant bound", func(t *testing.T) {
		rec := request(http.MethodPost, "/users",
			fmt.Sprintf(`{"org_id":%d,"username":"v1-created-viewer","password":"secret-password-123","role":"viewer","project_ids":[%d]}`, orgA, projectA1),
			orgAdminA, orgA, auth.RoleOrgAdmin)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		userID := responseID(t, rec.Body.Bytes())
		var membershipRole string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT role FROM project_members WHERE project_id = $1 AND user_id = $2`, projectA1, userID,
		).Scan(&membershipRole))
		require.Equal(t, projectMemberViewer, membershipRole)

		rec = request(http.MethodPost, "/users",
			fmt.Sprintf(`{"org_id":%d,"username":"v1-cross-org-viewer","password":"secret-password-123","role":"viewer","project_ids":[%d]}`, orgA, projectB),
			orgAdminA, orgA, auth.RoleOrgAdmin)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		var crossOrgUsers int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE org_id=$1 AND username='v1-cross-org-viewer'`, orgA).Scan(&crossOrgUsers))
		require.Zero(t, crossOrgUsers)
	})

	t.Run("active project role cannot be enabled or left without a project", func(t *testing.T) {
		rec := request(http.MethodPut, fmt.Sprintf("/users/%d", developerA), `{"project_ids":[]}`, orgAdminA, orgA, auth.RoleOrgAdmin)
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

		rec = request(http.MethodPut, fmt.Sprintf("/users/%d", developerA), fmt.Sprintf(`{"project_ids":[%d]}`, projectA1), orgAdminA, orgA, auth.RoleOrgAdmin)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		rec = request(http.MethodPut, fmt.Sprintf("/users/%d", developerA), `{"status":"disabled","project_ids":[]}`, orgAdminA, orgA, auth.RoleOrgAdmin)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		rec = request(http.MethodPut, fmt.Sprintf("/users/%d", developerA), `{"status":"active"}`, orgAdminA, orgA, auth.RoleOrgAdmin)
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
		rec = request(http.MethodPut, fmt.Sprintf("/users/%d", developerA), fmt.Sprintf(`{"status":"active","project_ids":[%d]}`, projectA2), orgAdminA, orgA, auth.RoleOrgAdmin)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})

	t.Run("project admin cannot escalate or cross tenant", func(t *testing.T) {
		serveMember := func(userID int64, body string) *httptest.ResponseRecorder {
			req := withTestActor(httptest.NewRequest(http.MethodPut, "/members", bytes.NewBufferString(body)), projectAdminA, orgA, auth.RoleProjectAdmin)
			req.SetPathValue("id", fmt.Sprintf("%d", projectA1))
			req.SetPathValue("user_id", fmt.Sprintf("%d", userID))
			rec := httptest.NewRecorder()
			h.UpsertProjectMember(rec, req)
			return rec
		}
		require.Equal(t, http.StatusForbidden, serveMember(projectAdminTarget, `{"role":"project_admin"}`).Code)
		require.Equal(t, http.StatusForbidden, serveMember(foreignDeveloper, `{"role":"project_developer"}`).Code)
		require.Equal(t, http.StatusOK, serveMember(developerA, `{"role":"project_developer"}`).Code)
	})

	t.Run("concurrent removals preserve one project for an active user", func(t *testing.T) {
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
		require.Equal(t, 1, got[http.StatusOK], got)
		require.Equal(t, 1, got[http.StatusConflict], got)
		var memberships int
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM project_members WHERE user_id=$1`, concurrentUser).Scan(&memberships))
		require.Equal(t, 1, memberships)
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
