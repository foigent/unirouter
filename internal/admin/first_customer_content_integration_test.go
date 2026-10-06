//go:build integration

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestFirstCustomerBootstrapRejectsCredentialJournalStateIntegration(t *testing.T) {
	pool, handler, adminID, adminOrgID := seedFirstCustomerRuntimeShell(t)
	token := runtimeBootstrapLogin(t, pool, handler)
	var projectID int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT id FROM projects WHERE org_id=$1 AND slug='default'`, adminOrgID).Scan(&projectID))
	const installationID = "xci_first_customer_credential"
	_, err := pool.Exec(t.Context(), `INSERT INTO xinsight_connector_installations
		(installation_id,org_id,project_id,x_deployment_id,invitation_sha256,event_scope,source_instance,source_binding_sha256,
		desired_state,observed_state,consented_by)
		VALUES($1,$2,$3,'xdep_first_customer',$4,'model.request.completed','first-customer-source',$5,'active','connected',$6)`,
		installationID, adminOrgID, projectID, "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64), adminID)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `INSERT INTO xinsight_connector_credential_operations
		(operation_id,installation_id,org_id,project_id,kind,previous_token_sha256,replacement_token_enc,
		replacement_token_sha256,request_enc,request_sha256)
		VALUES($1,$2,$3,$4,'rotate',$5,'encrypted-replacement',$6,'encrypted-request',$7)`,
		"xcr_"+strings.Repeat("c", 22), installationID, adminOrgID, projectID,
		"sha256:"+strings.Repeat("d", 64), "sha256:"+strings.Repeat("e", 64), "sha256:"+strings.Repeat("f", 64))
	require.NoError(t, err)
	before := runtimeBootstrapState(t, pool)
	tx, err := pool.BeginTx(t.Context(), pgx.TxOptions{AccessMode: pgx.ReadOnly})
	require.NoError(t, err)
	defer tx.Rollback(t.Context())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap/first-customer", nil)
	require.ErrorContains(t, handler.verifyFirstCustomerRuntimeResetCheckpoint(request, tx, adminID, adminOrgID),
		"check runtime empty table xinsight_connector_credential_operations")
	for _, dry := range []string{"false", "true"} {
		response := runtimeBootstrapAuthenticatedRequest(handler, pool, token,
			`{"organization_name":"Must Not Exist","dry_run":`+dry+`}`, "runtime-credential-"+dry)
		require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
		require.Equal(t, before, runtimeBootstrapState(t, pool), "rejection must retain installation and credential history")
	}
	var installationCount, operationCount int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT
		(SELECT COUNT(*) FROM xinsight_connector_installations WHERE installation_id=$1),
		(SELECT COUNT(*) FROM xinsight_connector_credential_operations WHERE installation_id=$1)`, installationID).
		Scan(&installationCount, &operationCount))
	require.EqualValues(t, 1, installationCount)
	require.EqualValues(t, 1, operationCount)
}
