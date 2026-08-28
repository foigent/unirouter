//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestModelOperationHTTPSOriginGoSQLGoldenVectorsIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	vectors := []struct {
		raw  string
		want string
		ok   bool
	}{
		{raw: "https://API.Example.com", want: "https://api.example.com", ok: true},
		{raw: " https://api.example.com:8443/ ", want: "https://api.example.com:8443", ok: true},
		{raw: "https://127.0.0.1:443", want: "https://127.0.0.1:443", ok: true},
		{raw: "https://[2001:db8::1]:443", want: "https://[2001:db8::1]:443", ok: true},
		{raw: "https://[2001:0DB8:0:0:0:0:0:1]:443", want: "https://[2001:db8::1]:443", ok: true},
		{raw: "\thttps://api.example.com\r\n"},
		{raw: "\rhttps://api.example.com\n"},
		{raw: "http://api.example.com"},
		{raw: "https://api.example.com/v1"},
		{raw: "https://user@api.example.com"},
		{raw: "https://api.example.com?x=1"},
		{raw: "https://api.example.com#x"},
		{raw: "https://api.example.com:0"},
		{raw: "https://api.example.com:70000"},
		{raw: "https://api.example.com:"},
		{raw: "https://[2001:db8::1]:"},
		{raw: "https://api .example.com"},
		{raw: "https://bad_label.example.com"},
		{raw: "https://999.999.999.999"},
		{raw: "https://[not-ip]"},
		{raw: "https://[127.0.0.1]"},
		{raw: "https://[::ffff:192.0.2.1]"},
		{raw: "https://[::192.0.2.1]"},
		{raw: "https://[2001:db8::1]/v1"},
		{raw: "\u00a0https://api.example.com\u00a0"},
	}
	for _, vector := range vectors {
		t.Run(vector.raw, func(t *testing.T) {
			var sqlValue *string
			require.NoError(t, pool.QueryRow(ctx, `SELECT model_operation_https_origin($1)`, vector.raw).Scan(&sqlValue))
			goValue, goErr := modelqualification.NormalizeHTTPSOrigin(vector.raw)
			if !vector.ok {
				require.Error(t, goErr, "Go accepted an explicitly rejected origin")
				require.Nil(t, sqlValue, "SQL accepted an explicitly rejected origin")
				return
			}
			require.NoError(t, goErr)
			require.NotNil(t, sqlValue, "SQL rejected an origin accepted by Go")
			require.Equal(t, vector.want, goValue)
			require.Equal(t, vector.want, *sqlValue)
		})
	}
}

func TestModelOperationProductionQualificationDatabaseGuardsIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
CREATE TEMP TABLE m75_route_shape
(LIKE runtime_route_revision_items INCLUDING DEFAULTS INCLUDING CONSTRAINTS)`)
	require.NoError(t, err)
	insertShape := func(publicPath, executor any) error {
		_, insertErr := pool.Exec(ctx, `
INSERT INTO m75_route_shape (
  release_id,ordinal,deployment_id,operation_id,expected_connection_revision,
  expected_deployment_revision,admission,source_candidate_version,
  public_path,executor_contract_version)
VALUES (1,1,1,'openai.chat_completions.v1',1,1,'allow','fixture',$1,$2)`, publicPath, executor)
		return insertErr
	}
	require.NoError(t, insertShape(nil, nil))
	_, err = pool.Exec(ctx, `TRUNCATE m75_route_shape`)
	require.NoError(t, err)
	require.ErrorContains(t, insertShape("/v1/responses", nil), "runtime_route_revision_items_execution_binding_shape_check")
	require.ErrorContains(t, insertShape(nil, modelqualification.ExecutorContractVersion), "runtime_route_revision_items_execution_binding_shape_check")

	var originalSupportCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_operation_execution_support_bindings`).Scan(&originalSupportCount))
	for _, mutation := range []struct {
		statement string
		message   string
	}{
		{statement: `INSERT INTO model_operation_execution_support_bindings (
	           operation_id,public_path,protocol_profile_id,protocol_profile_version,
	           executor_contract_version,introduced_migration)
	         VALUES ('future.operation.v1','/v1/future','future.profile','1.0.0','future.executor','000076_future')`, message: "migration-owned and immutable"},
		{statement: `UPDATE model_operation_execution_support_bindings SET public_path='/v1/changed' WHERE operation_id='openai.responses.v1'`, message: "migration-owned and immutable"},
		{statement: `DELETE FROM model_operation_execution_support_bindings WHERE operation_id='openai.responses.v1'`, message: "migration-owned and immutable"},
		// PostgreSQL may reject parent TRUNCATE at the FK boundary before the
		// table trigger runs. Either rejection preserves the migration-owned
		// rows; the exact trigger message is not part of the SQL contract.
		{statement: `TRUNCATE model_operation_execution_support_bindings`},
	} {
		_, guardErr := pool.Exec(ctx, mutation.statement)
		if mutation.message == "" {
			require.Error(t, guardErr)
		} else {
			require.ErrorContains(t, guardErr, mutation.message)
		}
	}
	var supportCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_operation_execution_support_bindings`).Scan(&supportCount))
	require.Equal(t, originalSupportCount, supportCount, "rejected writes must preserve every appended exact tuple")
	var historicalSupportCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_operation_execution_support_bindings
WHERE introduced_migration='000075_model_operation_production_qualification'`).Scan(&historicalSupportCount))
	require.Equal(t, 2, historicalSupportCount, "M75's two historical identities remain present")
}

func TestModelOperationProductionQualificationDownRejectsFrozenEvidenceIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	execMigrationDown(t, ctx, pool, "000076_model_deployment_lifecycle_receipts.down.sql")
	var orgID, publicModelID, connectionID, deploymentID, bindingID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('m75-down-evidence-customer','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_registry (org_id,model_name,display_name,provider_family,lifecycle)
VALUES ($1,'m75-down-evidence-model','M75 down evidence','openai','staging') RETURNING id`, orgID).Scan(&publicModelID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_connections (name,provider,base_url,adapter_type,adapter_config,admin_state)
VALUES ('M75 down evidence','openai','https://m75-down.example','openai_compatible','{}','enabled')
RETURNING id`).Scan(&connectionID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO model_deployments (
  customer_org_id,public_model_id,connection_id,upstream_model,admin_state)
VALUES ($1,$2,$3,'m75-down-upstream','enabled') RETURNING id`, orgID, publicModelID, connectionID).Scan(&deploymentID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO provider_credential_bindings (connection_id,key_enc,status)
VALUES ($1,'fixture-encrypted-secret','active') RETURNING id`, connectionID).Scan(&bindingID))
	_, err := pool.Exec(ctx, `
INSERT INTO model_deployment_operations (
  deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,
  protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.responses.v1','enabled','declared','',1,
        'unirouter.openai.responses.text.v1','1.0.0')`, deploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO model_deployment_probes (
  deployment_id,connection_id,operation_id,probe_profile_id,probe_profile_version,
  upstream_model,connection_revision,deployment_revision,operation_revision,
  public_path,executor_contract_version,https_origin,
  credential_binding_id,credential_state_revision,success,status_code,
  source_kind,source_ref,source_fingerprint)
VALUES ($1,$2,'openai.responses.v1','unirouter.openai.responses.text.v1','1.0.0',
        'm75-down-upstream',1,1,1,'/v1/responses',
        'unirouter.endpoint-profile-nonstream-executor.v1','https://m75-down.example',
        $3,1,FALSE,503,'admin.endpointprotocol_live_probe.v1','m75-down-evidence',
        'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')`,
		deploymentID, connectionID, bindingID)
	require.NoError(t, err)

	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000075_model_operation_production_qualification.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "frozen network probe evidence exists")
	var applied, evidencePresent bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM schema_migrations
               WHERE version='000075_model_operation_production_qualification'),
       EXISTS(SELECT 1 FROM model_deployment_probes
               WHERE source_ref='m75-down-evidence')`).Scan(&applied, &evidencePresent))
	require.True(t, applied)
	require.True(t, evidencePresent)
}

func TestModelOperationProductionQualificationDownRejectsFutureM76Integration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ('000076_future_contract')`)
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000075_model_operation_production_qualification.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "later migrations must be reversed first")
	var m75Present, m76Present bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version='000075_model_operation_production_qualification'),
       EXISTS(SELECT 1 FROM schema_migrations WHERE version='000076_future_contract')`).Scan(&m75Present, &m76Present))
	require.True(t, m75Present)
	require.True(t, m76Present)
}
