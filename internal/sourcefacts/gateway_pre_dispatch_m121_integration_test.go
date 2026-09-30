//go:build integration

package sourcefacts

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestGatewayPreDispatchM121ProfileExpansionPG(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	migrations := testutil.MigrationsDir(t)
	require.NoError(t, sourceBudgetFixtureDownIfPresent(t, pool, schemaauthority.XInsightPolicyExpandHead))
	require.NoError(t, sourceBudgetFixtureDownIfPresent(t, pool, schemaauthority.ScopedContentExpandHead))
	down, err := os.ReadFile(filepath.Join(migrations, schemaauthority.PreDispatchExpandHead+".down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	pool, source, org, project := setupSourceDBWithPool(t, pool)
	insertStream := func(profile string) error {
		_, err := pool.Exec(ctx, `INSERT INTO execution_source_streams(source_instance,org_id,project_id,profile) VALUES($1,$2,$3,$4)`, source.SourceInstance(), org, project, profile)
		return err
	}
	require.Error(t, insertStream(GatewayPreDispatchProfile), "M120 must reject v2 before expand")
	_, err = pool.Exec(ctx, `INSERT INTO execution_source_connections(org_id,project_id,source_instance,status,profiles) VALUES($1,$2,$3,'active',ARRAY['unirouter.execution-context.v1']::text[])`, org, project, source.SourceInstance())
	require.NoError(t, err)
	var key int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,key_type,name,key_hash,key_prefix,status,rpm_limit,qps_limit,budget_limit,allowed_ips,allowed_models,policy_labels,purpose,verifier_scheme,registration_operation_id,source_lifecycle_scope)
 VALUES($1,$2,'service','M121 isolated fixture',$3,'ur-xf1-aaaaaaaaaaaaa','feed_active',60,1,0,'127.0.0.1/32','','','xinsight.source.read','receiver_sha256_v1',$4,'legacy') RETURNING id`, org, project, strings.Repeat("a", 64), strings.Repeat("a", 32)).Scan(&key))
	_, err = pool.Exec(ctx, `INSERT INTO execution_source_credentials(api_key_id,org_id,project_id,registration_sha256,profiles,source_lifecycle_scope) VALUES($1,$2,$3,$4,ARRAY['unirouter.execution-context.v1']::text[],'legacy')`, key, org, project, "sha256:"+strings.Repeat("a", 64))
	require.NoError(t, err)
	grant := []string{"unirouter.execution-context.v1", GatewayPreDispatchProfile}
	_, err = pool.Exec(ctx, `UPDATE execution_source_connections SET profiles=$1 WHERE org_id=$2 AND project_id=$3`, grant, org, project)
	require.Error(t, err, "M120 connection must reject v2")
	_, err = pool.Exec(ctx, `UPDATE execution_source_credentials SET profiles=$1 WHERE api_key_id=$2`, grant, key)
	require.Error(t, err, "M120 credential must reject v2")
	up, err := os.ReadFile(filepath.Join(migrations, schemaauthority.PreDispatchExpandHead+".up.sql"))
	require.NoError(t, err)
	hooks := migrate.CompatibleExpandHooks{Version: schemaauthority.PreDispatchExpandHead, ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(up)), Before: func(context.Context, *pgxpool.Conn) error { return nil }, After: func(context.Context, pgx.Tx) error { return nil }}
	for _, variant := range []struct {
		name, digit                      string
		removeHead, removeImplementation bool
	}{
		{name: "old", digit: "b", removeHead: true, removeImplementation: true},
		{name: "missing_head", digit: "c", removeHead: true},
		{name: "missing_implementation", digit: "d", removeImplementation: true},
	} {
		old := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
		if variant.removeHead {
			old.SchemaHeads = slices.DeleteFunc(slices.Clone(old.SchemaHeads), func(head string) bool { return head == schemaauthority.PreDispatchExpandHead })
		}
		if variant.removeImplementation {
			old.Implementations = slices.DeleteFunc(slices.Clone(old.Implementations), func(ref modelprofile.ImplementationReference) bool {
				return ref.Kind == "source_observations" && ref.ID == "unirouter.source-observation.v2"
			})
		}
		oldWire, err := old.CanonicalBytes()
		require.NoError(t, err)
		oldDigest, err := old.Digest()
		require.NoError(t, err)
		artifact := "sha256:" + strings.Repeat(variant.digit, 64)
		_, err = pool.Exec(ctx, `INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes) VALUES($1,$2,$3)`, artifact, oldDigest, oldWire)
		require.NoError(t, err)
		instance := "uri_" + strings.Repeat(variant.digit, 32)
		_, err = pool.Exec(ctx, `INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at)
			VALUES($1,$2,$3,$4,'serving','absent',clock_timestamp()+interval '30 seconds')`, instance, artifact, runtimegeneration.CompatibilityEpoch, schemaauthority.ContentReclaimExpandHead)
		require.NoError(t, err)
		require.ErrorContains(t, migrate.RunCompatibleExpand(ctx, pool, migrations, hooks), "lacks M121 support", variant.name)
		_, err = pool.Exec(ctx, `UPDATE runtime_generations SET state='stopped' WHERE instance_id=$1`, instance)
		require.NoError(t, err)
	}
	require.NoError(t, migrate.RunCompatibleExpand(ctx, pool, migrations, hooks))
	require.NoError(t, source.CheckReady(ctx))
	require.NoError(t, insertStream(GatewayPreDispatchProfile))
	_, err = pool.Exec(ctx, `UPDATE execution_source_connections SET profiles=$1 WHERE org_id=$2 AND project_id=$3`, grant, org, project)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE execution_source_credentials SET profiles=$1 WHERE api_key_id=$2`, grant, key)
	require.NoError(t, err)
	require.Error(t, insertStream("unirouter.runtime-observation.v3"), "future profile must remain denied")
	_, err = pool.Exec(ctx, string(down))
	require.ErrorContains(t, err, "unsafe down refused")
	_, err = pool.Exec(ctx, `UPDATE execution_source_connections SET profiles=ARRAY['unirouter.execution-context.v1']::text[] WHERE org_id=$1 AND project_id=$2`, org, project)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE execution_source_credentials SET profiles=ARRAY['unirouter.execution-context.v1']::text[] WHERE api_key_id=$1`, key)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM execution_source_streams WHERE source_instance=$1 AND org_id=$2 AND project_id=$3 AND profile=$4`, source.SourceInstance(), org, project, GatewayPreDispatchProfile)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(down))
	require.NoError(t, err)
	require.Error(t, insertStream(GatewayPreDispatchProfile), "M120 restored profile bound")
}

func TestGatewayPreDispatchCaptureReplayPG(t *testing.T) {
	pool, source, org, project := setupSourceDB(t)
	source.options.ObservationsEnabled = true
	snapshot := testSnapshot()
	snapshot.Core.OrgID = executioncontext.ID(org)
	snapshot.Core.ProjectID = executioncontext.ID(project)
	snapshot.Core.Operation = "openai.chat_completions.v1"
	snapshot.Core.RequestID = "m121-replay"
	require.NoError(t, source.CaptureAuthenticated(t.Context(), snapshot))
	require.NoError(t, source.CaptureGatewayNoRoute(t.Context(), snapshot))
	var firstWire []byte
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT document FROM execution_source_facts WHERE profile=$1 AND request_id=$2`, GatewayPreDispatchProfile, snapshot.Core.RequestID).Scan(&firstWire))
	require.NoError(t, source.CaptureGatewayNoRoute(t.Context(), snapshot))
	var count int
	var secondWire []byte
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts WHERE profile=$1 AND request_id=$2`, GatewayPreDispatchProfile, snapshot.Core.RequestID).Scan(&count))
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT document FROM execution_source_facts WHERE profile=$1 AND request_id=$2`, GatewayPreDispatchProfile, snapshot.Core.RequestID).Scan(&secondWire))
	require.Equal(t, 1, count)
	require.Equal(t, firstWire, secondWire)
	snapshot.Core.Operation = "openai.embeddings.v1"
	require.Error(t, source.CaptureGatewayNoRoute(t.Context(), snapshot))
	snapshot.Core.Operation = "openai.chat_completions.v1"
	snapshot.Core.RequestID = "m121-concurrent"
	require.NoError(t, source.CaptureAuthenticated(t.Context(), snapshot))
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			results <- source.CaptureGatewayNoRoute(t.Context(), snapshot)
		}()
	}
	close(start)
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts WHERE profile=$1 AND request_id=$2`, GatewayPreDispatchProfile, snapshot.Core.RequestID).Scan(&count))
	require.Equal(t, 1, count)
}
