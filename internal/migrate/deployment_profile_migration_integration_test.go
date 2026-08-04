//go:build integration

package migrate_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestDeploymentProfileMigrationContractAndRollbackGuard(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	rollbackEmptyMigrations(t, pool, []string{"000056_prd8_invocation_optimization.down.sql"})

	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('deployment_profiles','gateway_endpoints')`).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='000055_prd8_deployment_profiles'`).Scan(&count))
	require.Equal(t, 1, count)

	var orgID, userID, profileID, endpointID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type) VALUES ('Deployment migration','active','standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users (org_id,username,password_hash,role,status) VALUES ($1,'deployment-owner','hash','platform_admin','active') RETURNING id`, orgID).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO deployment_profiles (profile_ref,display_name,kind,environment,ingress_mode,dns_mode,outbound_mode,status,created_by,updated_by) VALUES ('dep_migration_poc','Migration POC','ecs_development','poc','bundled_reverse_proxy','ip_address','direct','active',$1,$1) RETURNING id`, userID).Scan(&profileID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO gateway_endpoints (deployment_profile_id,endpoint_ref,display_name,base_url,visibility,tls_mode,certificate_ref,endpoint_profile,status,created_by,updated_by) VALUES ($1,'gwe_migration_poc','Migration Gateway','https://gateway.example.com','public','external_termination','secret://tls/gateway','endpoint.openai-chat.v1','active',$2,$2) RETURNING id`, profileID, userID).Scan(&endpointID))

	_, err := pool.Exec(ctx, `INSERT INTO gateway_endpoints (deployment_profile_id,endpoint_ref,display_name,base_url,visibility,tls_mode,certificate_ref,endpoint_profile,status,created_by,updated_by) VALUES ($1,'gwe_bad_responses','Bad','https://responses.example.com','public','external_termination','','endpoint.unknown.v1','active',$2,$2)`, profileID, userID)
	require.Error(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO gateway_endpoints (deployment_profile_id,endpoint_ref,display_name,base_url,visibility,tls_mode,certificate_ref,endpoint_profile,status,created_by,updated_by) VALUES ($1,'gwe_bad_http','Bad HTTP','http://gateway.example.com','public','disabled','','endpoint.openai-chat.v1','active',$2,$2)`, profileID, userID)
	require.Error(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO gateway_endpoints (deployment_profile_id,endpoint_ref,display_name,base_url,visibility,tls_mode,certificate_ref,endpoint_profile,status,created_by,updated_by) VALUES ($1,'gwe_bad_private_key','Bad key','https://key.example.com','internal','private_ca','-----BEGIN PRIVATE KEY-----','endpoint.openai-chat.v1','active',$2,$2)`, profileID, userID)
	require.Error(t, err)

	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000055_prd8_deployment_profiles.down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "desired-state or endpoint evidence exists")

	_, err = pool.Exec(ctx, `DELETE FROM gateway_endpoints WHERE id=$1`, endpointID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM deployment_profiles WHERE id=$1`, profileID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='000055_prd8_deployment_profiles'`).Scan(&count))
	require.Zero(t, count)
}
