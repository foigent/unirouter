//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/orgdirectory"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestEnterpriseDirectoryHandlersFailClosedAcrossOrganizations(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	store, err := orgdirectory.NewPostgresStore(pool)
	require.NoError(t, err)

	var orgA, orgB int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name, status, org_type)
		VALUES ('directory-handler-a','active','standard') RETURNING id`).Scan(&orgA))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name, status, org_type)
		VALUES ('directory-handler-b','active','standard') RETURNING id`).Scan(&orgB))
	sourceA, err := store.RegisterSource(ctx, orgdirectory.SourceRegistration{
		OrgID: orgA, SourceRef: "hcm-a", Authority: orgdirectory.AuthorityHCM,
		Connection: orgdirectory.ConnectionUnconfigured,
	})
	require.NoError(t, err)
	sourceB, err := store.RegisterSource(ctx, orgdirectory.SourceRegistration{
		OrgID: orgB, SourceRef: "hcm-b", Authority: orgdirectory.AuthorityHCM,
		Connection: orgdirectory.ConnectionUnconfigured,
	})
	require.NoError(t, err)
	insertIAMAction := func(orgID, sourceID int64, digest, subject string) {
		t.Helper()
		var snapshotID int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO enterprise_directory_snapshots (
			  snapshot_ref,org_id,source_id,kind,source_revision,observed_at
			) VALUES ($1,$2,$3,'full',$4,NOW()) RETURNING id`,
			digest, orgID, sourceID, "revision-"+subject).Scan(&snapshotID))
		_, insertErr := pool.Exec(ctx, `
			INSERT INTO enterprise_directory_iam_actions (
			  snapshot_id,org_id,source_id,subject_kind,subject_ref,action,reason,result
			) VALUES ($1,$2,$3,'person',$4,'no_change','tenant_scope_fixture','noop')`,
			snapshotID, orgID, sourceID, subject)
		require.NoError(t, insertErr)
	}
	insertIAMAction(orgA, sourceA, "sha256:"+strings.Repeat("a", 64), "person-a")
	insertIAMAction(orgB, sourceB, "sha256:"+strings.Repeat("b", 64), "person-b")

	handler := NewHandler(pool, "directory-handler-secret", nil, nil)
	serve := func(target string, actorOrgID int64, role string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		requestContext := context.WithValue(request.Context(), middleware.CtxOrgID, actorOrgID)
		requestContext = context.WithValue(requestContext, middleware.CtxUserRole, role)
		request = request.WithContext(requestContext)
		response := httptest.NewRecorder()
		handler.ListEnterpriseDirectorySources(response, request)
		return response
	}

	forbidden := serve("/api/v1/enterprise-directory/sources?org_id="+strconv.FormatInt(orgB, 10), orgA, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusForbidden, forbidden.Code)

	allowed := serve("/api/v1/enterprise-directory/sources", orgA, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusOK, allowed.Code)
	var payload struct {
		OrgID int64 `json:"org_id"`
		Total int   `json:"total"`
		Data  []struct {
			Observation orgdirectory.SourceObservation `json:"observation"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(allowed.Body.Bytes(), &payload))
	require.Equal(t, orgA, payload.OrgID)
	require.Equal(t, 1, payload.Total)
	require.Equal(t, "hcm-a", payload.Data[0].Observation.SourceRef)

	platform := serve("/api/v1/enterprise-directory/sources?org_id="+strconv.FormatInt(orgB, 10), orgA, auth.RolePlatformAdmin)
	require.Equal(t, http.StatusOK, platform.Code)
	require.NotContains(t, platform.Body.String(), "hcm-a")
	require.Contains(t, platform.Body.String(), "hcm-b")

	serveActions := func(target string, actorOrgID int64, role string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		requestContext := context.WithValue(request.Context(), middleware.CtxOrgID, actorOrgID)
		requestContext = context.WithValue(requestContext, middleware.CtxUserRole, role)
		request = request.WithContext(requestContext)
		response := httptest.NewRecorder()
		handler.ListEnterpriseDirectoryIAMActions(response, request)
		return response
	}
	forbiddenActions := serveActions("/api/v1/enterprise-directory/iam-actions?org_id="+strconv.FormatInt(orgB, 10), orgA, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusForbidden, forbiddenActions.Code)
	orgActions := serveActions("/api/v1/enterprise-directory/iam-actions", orgA, auth.RoleOrgAdmin)
	require.Equal(t, http.StatusOK, orgActions.Code)
	require.Contains(t, orgActions.Body.String(), "person-a")
	require.NotContains(t, orgActions.Body.String(), "person-b")
	platformActions := serveActions("/api/v1/enterprise-directory/iam-actions?org_id="+strconv.FormatInt(orgB, 10), orgA, auth.RolePlatformAdmin)
	require.Equal(t, http.StatusOK, platformActions.Code)
	require.Contains(t, platformActions.Body.String(), "person-b")
	require.NotContains(t, platformActions.Body.String(), "person-a")
}
