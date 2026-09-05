//go:build integration

package runtimechange_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/modelgov"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/runtimechange"
	"github.com/uniroute/unirouter/internal/testutil"
)

// These are synthetic local qualification fixtures, not Provider receipts.
func preciseSuccessorFixture(t *testing.T, pool *pgxpool.Pool, base canonicalDeploymentFixture, name, version string) canonicalDeploymentFixture {
	t.Helper()
	profile, ok := modelprofile.Lookup("minimax.m2.7.payg", version)
	require.True(t, ok)
	f := base
	f.ModelName = name
	protocol, found, err := modelprofile.ResolveOperationProtocolIdentity(profile.ID, version, f.OperationID)
	require.NoError(t, err)
	require.True(t, found)
	f.ProtocolProfileID, f.ProtocolVersion = protocol.ProtocolProfileID, protocol.ProtocolProfileVersion
	f.UpstreamModel = "MiniMax-M2.7"
	f.ConnectionRevision, f.DeploymentRevision, f.OperationRevision, f.CredentialRevision = 1, 1, 1, 1
	ctx := context.Background()
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_registry
 (org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,context_length,output_limit,execution_profile_id,execution_profile_version)
 VALUES ($1,$2,'MiniMax M2.7','minimax','staging',FALSE,128000,8192,$3,$4) RETURNING id`, f.OrgID, name, profile.ID, version).Scan(&f.PublicModelID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO provider_connections
 (name,provider,base_url,adapter_type,adapter_config,admin_state)
 VALUES ($1,'minimax','https://api.minimax.cn/v1','openai_compatible','{}','enabled') RETURNING id`, "connection-"+name).Scan(&f.ConnectionID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_deployments
 (customer_org_id,public_model_id,connection_id,upstream_model,admin_state,priority,routing_weight,max_context,default_max_output)
 VALUES ($1,$2,$3,$4,'enabled',100,100,128000,8192) RETURNING id`, f.OrgID, f.PublicModelID, f.ConnectionID, f.UpstreamModel).Scan(&f.DeploymentID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO provider_credential_bindings
 (connection_id,key_enc,status) VALUES ($1,'fixture','active') RETURNING id`, f.ConnectionID).Scan(&f.CredentialID))
	_, err = pool.Exec(ctx, `INSERT INTO credential_binding_deployments (binding_id,deployment_id,connection_id) VALUES ($1,$2,$3)`, f.CredentialID, f.DeploymentID, f.ConnectionID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO model_deployment_operations
 (deployment_id,operation_id,support_state,evidence_state,evidence_ref,verified_at,revision,protocol_profile_id,protocol_profile_version)
 VALUES ($1,$2,'enabled','live_probed','synthetic local fixture',NOW(),1,$3,$4)`, f.DeploymentID, f.OperationID, f.ProtocolProfileID, f.ProtocolVersion)
	require.NoError(t, err)
	insertSuccessfulProbe(t, pool, f, "repeat-"+name)
	return f
}

func repeatTestCommand(stable string, old, next canonicalDeploymentFixture, sealed runtimechange.DeploymentRelease, token string) runtimechange.ModelSuccessorCutoverCommand {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte("key:"+token)))
	fingerprint := fmt.Sprintf("hmac-sha256:%x", sha256.Sum256([]byte("request:"+token)))
	return runtimechange.ModelSuccessorCutoverCommand{
		Mutation: runtimechange.ModelSuccessorCutoverMutation{
			ReleaseRef: sealed.ReleaseRef, ExpectedReleaseVersion: sealed.Version,
			StableRequestModel: stable, PredecessorPublicModelID: old.PublicModelID, PredecessorModelName: old.ModelName,
			ExpectedPredecessorLifecycle: "ga", ExpectedPredecessorApprovedCatalog: true,
			SuccessorPublicModelID: next.PublicModelID, SuccessorModelName: next.ModelName,
			ExpectedSuccessorLifecycle: "staging", ExpectedModelSets: []runtimechange.ModelSuccessorExpectedModelSet{},
		}, CustomerOrgID: next.OrgID, ActorUserID: next.UserID, ActorOrgID: next.OrgID,
		IdempotencyKeyHash: key, RequestFingerprint: fingerprint, ServerOperationID: "op_" + token,
		Audit: runtimechange.DeploymentLifecycleAudit{RequestID: "op_" + token, Source: "admin_console"},
	}
}

func repeatAuthority(t *testing.T, pool *pgxpool.Pool, command runtimechange.ModelSuccessorCutoverCommand) runtimechange.ModelSuccessorCutoverCommand {
	t.Helper()
	command.Mutation.PredecessorAuthorityMode = runtimechange.ModelSuccessorPredecessorPrimaryAlias
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT alias.id,slot.slot_version,release.release_ref
 FROM model_aliases alias JOIN model_registry registry ON registry.org_id=alias.org_id AND registry.model_name=alias.target_model
 JOIN runtime_change_slots slot ON slot.public_model_id=registry.id
 JOIN runtime_change_releases release ON release.id=slot.stable_release_id
 WHERE alias.org_id=$1 AND alias.alias=$2`, command.CustomerOrgID, command.Mutation.StableRequestModel).Scan(
		&command.Mutation.ExpectedPrimaryAliasID, &command.Mutation.ExpectedPredecessorSlotVersion, &command.Mutation.ExpectedPredecessorReleaseRef))
	return command
}

func executeRepeatMigration(t *testing.T, pool *pgxpool.Pool, direction string) error {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "000112_repeat_model_successor_cutover."+direction+".sql"))
	require.NoError(t, err)
	conn, err := pool.Acquire(context.Background())
	require.NoError(t, err)
	defer conn.Release()
	_, err = conn.Exec(context.Background(), string(body))
	if err != nil {
		_, _ = conn.Exec(context.Background(), "ROLLBACK")
	}
	if err == nil && direction == "up" {
		_, err = conn.Exec(context.Background(), `INSERT INTO schema_migrations(version) VALUES ('000112_repeat_model_successor_cutover')`)
	}
	return err
}

func TestRepeatedModelSuccessorContinuousHistoryIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	// This round-trip exercises M112 against its supported schema ceiling.
	// Remove later, unattached test artifacts through their guarded downs in
	// reverse order. M114's guard must remain authoritative above M113/M112.
	for _, migration := range []string{"000114_capability_delivery_expand", "000113_glm_stream_function_tools_qualification"} {
		body, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), migration+".down.sql"))
		require.NoError(t, err)
		_, err = pool.Exec(ctx, string(body))
		require.NoError(t, err, migration)
	}
	manager, err := runtimechange.NewManager(&recordingBeginTxPool{Pool: pool}, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default())
	require.NoError(t, err)
	a := createCanonicalDeploymentFixture(t, pool, "repeat-a", "active", 100, 100)
	b := preciseSuccessorFixture(t, pool, a, "repeat-b", "1.2.0")
	makeSoleInitialCustomer(t, pool, a.OrgID)
	bRelease := sealInitialDeploymentRoutes(t, pool, manager, b, []runtimechange.DeploymentRoute{deploymentRoute(b)}, "rcr_repeat-b")
	first := repeatTestCommand(a.ModelName, a, b, bRelease, "repeat_first")
	firstReceipt, err := manager.ApplyModelSuccessorCutover(ctx, first)
	require.NoError(t, err)

	// Upgrade/down must work from an already completed historical A→B, not just
	// an empty database. Down is refused after a durable repeated transition.
	require.NoError(t, executeRepeatMigration(t, pool, "down"))
	require.NoError(t, executeRepeatMigration(t, pool, "up"))
	_, err = pool.Exec(ctx, `UPDATE model_aliases SET target_model='unapproved' WHERE org_id=$1 AND alias=$2`, a.OrgID, a.ModelName)
	require.ErrorContains(t, err, "primary model alias is immutable")

	var stableSet, canonicalSet int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_sets(org_id,name,slug,status) VALUES($1,'stable','stable','active') RETURNING id`, a.OrgID).Scan(&stableSet))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_sets(org_id,name,slug,status) VALUES($1,'canonical','canonical','active') RETURNING id`, a.OrgID).Scan(&canonicalSet))
	_, err = pool.Exec(ctx, `INSERT INTO model_set_items(model_set_id,item_type,item_name) VALUES($1,'alias',$2),($3,'model',$4)`, stableSet, a.ModelName, canonicalSet, b.ModelName)
	require.NoError(t, err)
	c := preciseSuccessorFixture(t, pool, a, "repeat-c", "1.3.0")
	cRelease := sealInitialDeploymentRoutes(t, pool, manager, c, []runtimechange.DeploymentRoute{deploymentRoute(c)}, "rcr_repeat-c")
	var targetSlots int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM runtime_change_slots WHERE public_model_id=$1`, c.PublicModelID).Scan(&targetSlots))
	require.Zero(t, targetSlots, "sealed candidate is not a public execution entry")
	second := repeatAuthority(t, pool, repeatTestCommand(a.ModelName, b, c, cRelease, "repeat_second"))
	second.Mutation.ExpectedModelSets = []runtimechange.ModelSuccessorExpectedModelSet{{ModelSetID: canonicalSet}}
	for _, mutate := range []func(*runtimechange.ModelSuccessorCutoverCommand){
		func(c *runtimechange.ModelSuccessorCutoverCommand) { c.Mutation.ExpectedPrimaryAliasID++ },
		func(c *runtimechange.ModelSuccessorCutoverCommand) { c.Mutation.ExpectedPredecessorSlotVersion++ },
		func(c *runtimechange.ModelSuccessorCutoverCommand) {
			c.Mutation.ExpectedPredecessorReleaseRef = "rcr_missing"
		},
		func(c *runtimechange.ModelSuccessorCutoverCommand) { c.CustomerOrgID++ },
		func(c *runtimechange.ModelSuccessorCutoverCommand) { c.Mutation.ExpectedReleaseVersion++ },
		func(c *runtimechange.ModelSuccessorCutoverCommand) {
			c.Mutation.ExpectedModelSets = []runtimechange.ModelSuccessorExpectedModelSet{}
		},
	} {
		invalid := second
		mutate(&invalid)
		_, err = manager.ApplyModelSuccessorCutover(ctx, invalid)
		require.Error(t, err)
		var target string
		require.NoError(t, pool.QueryRow(ctx, `SELECT target_model FROM model_aliases WHERE id=$1`, second.Mutation.ExpectedPrimaryAliasID).Scan(&target))
		require.Equal(t, b.ModelName, target, "rejected command must leave old authority unchanged")
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM runtime_change_slots WHERE public_model_id=$1`, c.PublicModelID).Scan(&targetSlots))
		require.Zero(t, targetSlots)
	}
	secondReceipt, err := manager.ApplyModelSuccessorCutover(ctx, second)
	require.NoError(t, err)
	require.Equal(t, runtimechange.RepeatedModelSuccessorCutoverReceiptContractVersion, secondReceipt.ContractVersion)
	require.Equal(t, second.Mutation.ExpectedPrimaryAliasID, secondReceipt.PredecessorAuthoritySnapshot["primary_alias_id"])

	d := preciseSuccessorFixture(t, pool, a, "repeat-d", "1.3.0")
	dRelease := sealInitialDeploymentRoutes(t, pool, manager, d, []runtimechange.DeploymentRoute{deploymentRoute(d)}, "rcr_repeat-d")
	third := repeatAuthority(t, pool, repeatTestCommand(a.ModelName, c, d, dRelease, "repeat_third"))
	_, err = manager.ApplyModelSuccessorCutover(ctx, third)
	require.NoError(t, err)
	for _, command := range []runtimechange.ModelSuccessorCutoverCommand{first, second} {
		command.ServerOperationID += "_replay"
		replayed, replayErr := manager.ApplyModelSuccessorCutover(ctx, command)
		require.NoError(t, replayErr)
		require.True(t, replayed.Lifecycle.IdempotencyReplay)
		if command.Mutation.PredecessorPublicModelID == a.PublicModelID {
			require.Equal(t, firstReceipt.Lifecycle.ReceiptID, replayed.Lifecycle.ReceiptID)
		}
	}
	var current string
	require.NoError(t, pool.QueryRow(ctx, `SELECT target_model FROM model_aliases WHERE id=$1`, third.Mutation.ExpectedPrimaryAliasID).Scan(&current))
	require.Equal(t, d.ModelName, current)
	recoveryManager, err := runtimechange.NewManager(&successorClaimErrorPool{Pool: pool, code: "40001"}, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default())
	require.NoError(t, err)
	recovered, err := recoveryManager.ApplyModelSuccessorCutover(ctx, second)
	require.NoError(t, err)
	require.Equal(t, secondReceipt.Lifecycle.ReceiptID, recovered.Lifecycle.ReceiptID)
	require.NoError(t, pool.QueryRow(ctx, `SELECT target_model FROM model_aliases WHERE id=$1`, third.Mutation.ExpectedPrimaryAliasID).Scan(&current))
	require.Equal(t, d.ModelName, current, "uncertain-claim recovery reads history without retargeting")
	allowed, err := modelgov.CheckAccess(ctx, pool, a.OrgID, 0, a.ModelName)
	require.NoError(t, err)
	require.Equal(t, d.ModelName, allowed.ResolvedModel)
	for _, direct := range []string{b.ModelName, c.ModelName, d.ModelName} {
		_, err = modelgov.CheckAccess(ctx, pool, a.OrgID, 0, direct)
		require.Error(t, err)
	}
	identities, err := modelgov.ResolveCallableModelIdentities(ctx, pool, a.OrgID, 0, []string{a.ModelName, b.ModelName, c.ModelName, d.ModelName})
	require.NoError(t, err)
	require.Len(t, identities, 1)
	require.Equal(t, a.ModelName, identities[0].RequestModel)
	var aliasItems int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_set_items WHERE model_set_id=ANY($1::bigint[]) AND item_type='alias' AND item_name=$2`, []int64{stableSet, canonicalSet}, a.ModelName).Scan(&aliasItems))
	require.Equal(t, 2, aliasItems)
	require.ErrorContains(t, executeRepeatMigration(t, pool, "down"), "durable primary successor evidence")

	// Competing next targets share the same before-state; only one can commit.
	e := preciseSuccessorFixture(t, pool, a, "repeat-e", "1.3.0")
	f := preciseSuccessorFixture(t, pool, a, "repeat-f", "1.3.0")
	eRelease := sealInitialDeploymentRoutes(t, pool, manager, e, []runtimechange.DeploymentRoute{deploymentRoute(e)}, "rcr_repeat-e")
	fRelease := sealInitialDeploymentRoutes(t, pool, manager, f, []runtimechange.DeploymentRoute{deploymentRoute(f)}, "rcr_repeat-f")
	commands := []runtimechange.ModelSuccessorCutoverCommand{
		repeatAuthority(t, pool, repeatTestCommand(a.ModelName, d, e, eRelease, "repeat_e")),
		repeatAuthority(t, pool, repeatTestCommand(a.ModelName, d, f, fRelease, "repeat_f")),
	}
	results := make(chan error, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for _, command := range commands {
		workers.Add(1)
		go func(command runtimechange.ModelSuccessorCutoverCommand) {
			defer workers.Done()
			<-start
			_, err := manager.ApplyModelSuccessorCutover(ctx, command)
			results <- err
		}(command)
	}
	close(start)
	workers.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	require.Equal(t, 1, success)
}

func TestRepeatedSuccessorRejectsUnverifiedCustomUpstreamLineageIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	manager, err := runtimechange.NewManager(&recordingBeginTxPool{Pool: pool}, unusedDeploymentSnapshotBuilder{}, routingpolicy.Default())
	require.NoError(t, err)
	a := createCanonicalDeploymentFixture(t, pool, "custom-repeat-a", "active", 100, 100)
	makeSoleInitialCustomer(t, pool, a.OrgID)
	makeTarget := func(name string) canonicalDeploymentFixture {
		f := a
		f.ModelName = name
		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_registry
 (org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,context_length,output_limit)
 VALUES ($1,$2,$2,'custom','staging',FALSE,128000,8192) RETURNING id`, a.OrgID, name).Scan(&f.PublicModelID))
		return createAdditionalDeploymentFixture(t, pool, f, name, "active", 100, 100)
	}
	b, c := makeTarget("custom-b"), makeTarget("custom-c")
	require.NotEqual(t, b.UpstreamModel, c.UpstreamModel)
	bRelease := sealInitialDeploymentRoutes(t, pool, manager, b, []runtimechange.DeploymentRoute{deploymentRoute(b)}, "rcr_custom-b")
	_, err = manager.ApplyModelSuccessorCutover(ctx, repeatTestCommand(a.ModelName, a, b, bRelease, "custom_first"))
	require.NoError(t, err, "historical first-cutover contract remains unchanged")
	cRelease := sealInitialDeploymentRoutes(t, pool, manager, c, []runtimechange.DeploymentRoute{deploymentRoute(c)}, "rcr_custom-c")
	command := repeatAuthority(t, pool, repeatTestCommand(a.ModelName, b, c, cRelease, "custom_repeat"))
	_, err = manager.ApplyModelSuccessorCutover(ctx, command)
	require.ErrorContains(t, err, "same verified model product")
	var target string
	require.NoError(t, pool.QueryRow(ctx, `SELECT target_model FROM model_aliases WHERE id=$1`, command.Mutation.ExpectedPrimaryAliasID).Scan(&target))
	require.Equal(t, b.ModelName, target)
}
