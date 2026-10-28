package migrate_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
	"slices"
	"strings"
	"testing"
)

func TestCommercialRetirementMaintenanceIntegration(t *testing.T) {
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.XInsightCredentialExpandHead)
	ctx := context.Background()
	dir := testutil.PreCommunicationMigrationsDir(t)
	before, err := runtimegeneration.ReadBudgetCoverage(ctx, pool)
	require.NoError(t, err)
	var triggerCount int
	count := func() int {
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM pg_trigger WHERE tgname IN
  ('api_keys_commercial_limit','users_commercial_limit','projects_commercial_limit','access_credentials_commercial_limit','provider_connections_commercial_limit')`).Scan(&triggerCount))
		return triggerCount
	}
	require.Equal(t, 5, count())
	_, err = pool.Exec(ctx, `INSERT INTO settings(key,value) VALUES('license.offline','{"historical":true}')`)
	require.NoError(t, err)
	var historical string
	require.NoError(t, pool.QueryRow(ctx, `SELECT value::text FROM settings WHERE key='license.offline'`).Scan(&historical))
	require.ErrorContains(t, migrate.RunRuntime(ctx, pool, dir), "explicit maintenance")
	require.Equal(t, 5, count())
	require.NoError(t, migrate.RunExisting(ctx, pool, dir))
	require.Equal(t, 0, count())
	var retained, head string
	require.NoError(t, pool.QueryRow(ctx, `SELECT value::text FROM settings WHERE key='license.offline'`).Scan(&retained))
	require.Equal(t, historical, retained)
	require.NoError(t, pool.QueryRow(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&head))
	require.Equal(t, schemaauthority.CommercialLicenseRetirementHead, head)
	after, err := runtimegeneration.ReadBudgetCoverage(ctx, pool)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.NoError(t, migrate.RunExisting(ctx, pool, dir))
	require.NoError(t, migrate.RunRuntime(ctx, pool, dir))
}

func TestCommercialRetirementReceivingFleetMustWithdrawAndRetire(t *testing.T) {
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.XInsightCredentialExpandHead)
	ctx := t.Context()
	artifact, err := schemaauthority.ExpectedLedger(testutil.MigrationsDir(t))
	require.NoError(t, err)
	artifact = artifact[:slices.Index(artifact, schemaauthority.CommercialLicenseRetirementHead)+1] // Exact historical M126 artifact.
	oldLedger := artifact[:len(artifact)-1]
	manifest := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
	old := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("a", 32), Artifact: "sha256:" + strings.Repeat("a", 64)}
	_, head, err := runtimegeneration.Renew(ctx, pool, old, manifest, oldLedger, false)
	require.NoError(t, err)
	require.Equal(t, schemaauthority.XInsightCredentialExpandHead, head)
	hash := func(w []byte) string { return fmt.Sprintf("%x", sha256.Sum256(w)) }
	initialBytes := []byte("synthetic old receiver")
	initial := runtimegeneration.IngressPlan{ChangeID: "m126-fixture-initial", ExpectedConfigDigest: hash(initialBytes), DesiredConfigDigest: hash(initialBytes), DesiredConfigBytes: initialBytes, Before: []runtimegeneration.Identity{}, After: []runtimegeneration.Identity{old}}
	_, err = runtimegeneration.PrepareIngress(ctx, pool, initial)
	require.NoError(t, err)
	_, err = runtimegeneration.CompleteIngress(ctx, pool, initial.ChangeID, initial.DesiredConfigDigest, true, json.RawMessage(`{"synthetic_proxy_observation":true}`))
	require.NoError(t, err)
	require.ErrorContains(t, migrate.RunExisting(ctx, pool, testutil.PreCommunicationMigrationsDir(t)), "stopped, retired writers")
	emptyBytes := []byte("synthetic explicit empty membership")
	withdrawal := runtimegeneration.IngressPlan{ChangeID: "m126-fixture-withdraw", PredecessorID: initial.ChangeID, ExpectedConfigDigest: initial.DesiredConfigDigest, DesiredConfigDigest: hash(emptyBytes), DesiredConfigBytes: emptyBytes, Before: []runtimegeneration.Identity{old}, After: []runtimegeneration.Identity{}}
	omitted := withdrawal
	omitted.After = nil
	_, err = runtimegeneration.PrepareIngress(ctx, pool, omitted)
	require.Error(t, err, "missing or null membership is not an explicit full withdrawal")
	_, err = runtimegeneration.PrepareIngress(ctx, pool, withdrawal)
	require.NoError(t, err)
	retained, status, err := runtimegeneration.ReadIngress(ctx, pool, withdrawal.ChangeID)
	require.NoError(t, err)
	require.Equal(t, "pending", status)
	require.Equal(t, withdrawal, retained, "unknown prepare response recovers the exact retained empty array")
	status, err = runtimegeneration.CompleteIngress(ctx, pool, withdrawal.ChangeID, withdrawal.DesiredConfigDigest, false, json.RawMessage(`{"synthetic_old_worker_still_alive":true}`))
	require.NoError(t, err)
	require.Equal(t, "ambiguous", status)
	require.Error(t, migrate.RunExisting(ctx, pool, testutil.PreCommunicationMigrationsDir(t)))
	status, err = runtimegeneration.CompleteIngress(ctx, pool, withdrawal.ChangeID, withdrawal.DesiredConfigDigest, true, json.RawMessage(`{"synthetic_old_workers_exited":true}`))
	require.NoError(t, err)
	require.Equal(t, "applied", status)
	require.Error(t, migrate.RunExisting(ctx, pool, testutil.PreCommunicationMigrationsDir(t)), "withdrawal alone is not process retirement")

	// Database-owner fixture only: the separate host controller must prove real
	// process/container exit. This synthetic expiry is not Target exit evidence.
	_, err = pool.Exec(ctx, "UPDATE runtime_generations SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE instance_id=$1", old.Instance)
	require.NoError(t, err)
	var revision int64
	var systemID string
	require.NoError(t, pool.QueryRow(ctx, "SELECT revision FROM runtime_generations WHERE instance_id=$1", old.Instance).Scan(&revision))
	require.NoError(t, pool.QueryRow(ctx, "SELECT system_identifier::text FROM pg_control_system()").Scan(&systemID))
	manifestDigest, err := manifest.Digest()
	require.NoError(t, err)
	input := runtimegeneration.RetirementInput{InstanceID: old.Instance, ArtifactDigest: old.Artifact, ManifestDigest: manifestDigest, CompatibilityEpoch: runtimegeneration.CompatibilityEpoch, ExpectedSchemaHead: schemaauthority.XInsightCredentialExpandHead, ExpectedRevision: revision, ExpectedIngressHead: withdrawal.ChangeID, ExpectedEnvironment: "test", ExpectedDatabaseSystemID: systemID, HostProofSHA256: "sha256:" + strings.Repeat("f", 64)}
	_, err = runtimegeneration.CheckRetirement(ctx, pool, input)
	require.NoError(t, err)
	var checkedRevision int64
	require.NoError(t, pool.QueryRow(ctx, "SELECT revision FROM runtime_generations WHERE instance_id=$1", old.Instance).Scan(&checkedRevision))
	require.Equal(t, revision, checkedRevision, "check performs no mutation")
	_, err = runtimegeneration.ApplyRetirement(ctx, pool, input)
	require.NoError(t, err)
	require.NoError(t, migrate.RunExisting(ctx, pool, testutil.PreCommunicationMigrationsDir(t)))
	require.Error(t, schemaauthority.VerifyRuntimeLedger(artifact, oldLedger))
	current := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("b", 32), Artifact: "sha256:" + strings.Repeat("b", 64)}
	_, head, err = runtimegeneration.Renew(ctx, pool, current, manifest, artifact, false)
	require.NoError(t, err)
	require.Equal(t, schemaauthority.CommercialLicenseRetirementHead, head)
	newBytes := []byte("synthetic new receiver")
	rejoin := runtimegeneration.IngressPlan{ChangeID: "m126-fixture-rejoin", PredecessorID: withdrawal.ChangeID, ExpectedConfigDigest: withdrawal.DesiredConfigDigest, DesiredConfigDigest: hash(newBytes), DesiredConfigBytes: newBytes, Before: []runtimegeneration.Identity{}, After: []runtimegeneration.Identity{current}}
	_, err = runtimegeneration.PrepareIngress(ctx, pool, rejoin)
	require.NoError(t, err)
	status, err = runtimegeneration.CompleteIngress(ctx, pool, rejoin.ChangeID, rejoin.DesiredConfigDigest, true, json.RawMessage(`{"synthetic_new_receiver":true}`))
	require.NoError(t, err)
	require.Equal(t, "applied", status)
}

func TestCommercialRetirementKeepsHistoryAndRemovesResourceLimits(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprintf("expired=%t", expired), func(t *testing.T) {
			pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.XInsightCredentialExpandHead)
			ctx := t.Context()
			_, err := pool.Exec(ctx, `INSERT INTO commercial_license_entitlements(singleton,license_id,deployment_id,key_id,expires_at,features,active_api_keys,active_users,active_projects,active_channels)
    VALUES(TRUE,'historical','synthetic-test','historical-key',clock_timestamp()+CASE WHEN $1 THEN interval '-1 day' ELSE interval '1 day' END,'[]',1,1,1,1)`, expired)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `INSERT INTO audit_logs(org_id,user_id,action,resource,resource_id,detail) VALUES(1,1,'license_import','license','historical','{"synthetic":true}')`)
			require.NoError(t, err)
			var entitlement, audit string
			require.NoError(t, pool.QueryRow(ctx, "SELECT to_jsonb(e)::text FROM commercial_license_entitlements e").Scan(&entitlement))
			require.NoError(t, pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(a) ORDER BY id)::text FROM audit_logs a").Scan(&audit))
			insert := func(name string) error {
				_, err := pool.Exec(ctx, `INSERT INTO provider_connections(name,provider,base_url) VALUES($1,'custom','https://synthetic.invalid')`, name)
				return err
			}
			if !expired {
				require.NoError(t, insert("before-first"))
			}
			require.ErrorContains(t, insert("before-blocked"), "commercial License")
			require.NoError(t, migrate.RunExisting(ctx, pool, testutil.PreCommunicationMigrationsDir(t)))
			require.NoError(t, insert("after-first"))
			require.NoError(t, insert("after-second"))
			_, err = pool.Exec(ctx, `INSERT INTO provider_connections(name,provider,base_url,config_revision) VALUES('invalid','custom','https://synthetic.invalid',0)`)
			require.ErrorContains(t, err, "provider_connections_identity_check", "business identity constraints remain enforced")
			var afterEntitlement, afterAudit string
			require.NoError(t, pool.QueryRow(ctx, "SELECT to_jsonb(e)::text FROM commercial_license_entitlements e").Scan(&afterEntitlement))
			require.NoError(t, pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(a) ORDER BY id)::text FROM audit_logs a").Scan(&afterAudit))
			require.Equal(t, entitlement, afterEntitlement)
			require.Equal(t, audit, afterAudit)
		})
	}
}
