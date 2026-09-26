//go:build integration

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// These rows belong only to the test's isolated database and existing default
// project. The health case includes its required stream; no FK is bypassed.
func seedFirstCustomerContentState(t *testing.T, pool *pgxpool.Pool, org, project int64, kind string) {
	t.Helper()
	if kind == "policy" {
		config := []byte(`{"mode":"metadata_only","supported_protocols":["openai.chat"],"observation_points":["client_request"],"allowed_purposes":[]}`)
		_, err := pool.Exec(t.Context(), `INSERT INTO execution_content_policies(org_id,project_id,desired_version,effective_version,desired_config,effective_config,effective_at,application_status,application_reason) VALUES($1,$2,1,1,$3,$3,clock_timestamp(),'applied','none')`, org, project, config)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, err := pool.Exec(context.Background(), `DELETE FROM execution_content_policies WHERE org_id=$1 AND project_id=$2`, org, project)
			require.NoError(t, err)
		})
		return
	}
	_, err := pool.Exec(t.Context(), `INSERT INTO execution_content_streams(org_id,project_id,source_instance) VALUES($1,$2,'synthetic-bootstrap-content')`, org, project)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), `DELETE FROM execution_content_streams WHERE org_id=$1 AND project_id=$2`, org, project)
		require.NoError(t, err)
	})
	if kind == "health" {
		_, err := pool.Exec(t.Context(), `INSERT INTO execution_content_health(org_id,project_id,reason,detail) VALUES($1,$2,'synthetic_capture_gap',$3)`, org, project, []byte(`{}`))
		require.NoError(t, err)
		t.Cleanup(func() {
			_, err := pool.Exec(context.Background(), `DELETE FROM execution_content_health WHERE org_id=$1 AND project_id=$2`, org, project)
			require.NoError(t, err)
		})
	}
}

func TestFirstCustomerBootstrapRejectsContentStateIntegration(t *testing.T) {
	for _, kind := range []string{"stream", "policy", "health"} {
		t.Run(kind, func(t *testing.T) {
			pool, handler, adminID, adminOrgID := seedFirstCustomerRuntimeShell(t)
			token := runtimeBootstrapLogin(t, pool, handler)
			check := func() error {
				tx, err := pool.BeginTx(t.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
				require.NoError(t, err)
				defer tx.Rollback(t.Context())
				return handler.verifyFirstCustomerRuntimeResetCheckpoint(httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap/first-customer", nil), tx, adminID, adminOrgID)
			}
			require.NoError(t, check(), "the clean checkpoint must be accepted before injecting content")
			var projectID int64
			require.NoError(t, pool.QueryRow(t.Context(), `SELECT id FROM projects WHERE org_id=$1 AND slug='default'`, adminOrgID).Scan(&projectID))
			seedFirstCustomerContentState(t, pool, adminOrgID, projectID, kind)
			before := runtimeBootstrapState(t, pool)
			require.ErrorContains(t, check(), "check runtime empty table execution_content_")
			for _, dry := range []string{"false", "true"} {
				response := runtimeBootstrapAuthenticatedRequest(handler, pool, token, `{"organization_name":"Must Not Exist","dry_run":`+dry+`}`, "runtime-content-"+kind+"-"+dry)
				require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
				require.Equal(t, before, runtimeBootstrapState(t, pool), "rejection must preserve content, business state and identity sequences")
			}
		})
	}
}
