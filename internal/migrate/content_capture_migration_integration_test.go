//go:build integration

package migrate_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/testutil"
)

func contentMigrationHooks(t *testing.T) migrate.CompatibleExpandHooks {
	t.Helper()
	wire, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), schemaauthority.ContentCaptureExpandHead+".up.sql"))
	require.NoError(t, err)
	return migrate.CompatibleExpandHooks{Version: schemaauthority.ContentCaptureExpandHead, ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(wire)), Before: func(context.Context, *pgxpool.Conn) error { return nil }, After: func(context.Context, pgx.Tx) error { return nil }}
}

func contentMigrationDown(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	wire, err := os.ReadFile(filepath.Join(dir, schemaauthority.ContentCaptureExpandHead+".down.sql"))
	if err != nil {
		return err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	_, err = conn.Exec(ctx, string(wire))
	if err != nil {
		_, rollbackErr := conn.Exec(context.Background(), "ROLLBACK")
		return errors.Join(err, rollbackErr)
	}
	return nil
}

func contentMigrationLedger(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	var ledger []string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT array_agg(version::text ORDER BY version) FROM schema_migrations`).Scan(&ledger))
	return ledger
}

// Reuse the strict same-source artifact installer, including its Publication
// lifecycle and original M118 certification. The real empty M119 down produces
// the exact M118 ledger/schema predecessor. This is a synthetic DB fixture, not
// execution of a historical M118 binary, host process, or deployed artifact.
func contentMigrationM118(t *testing.T) (*pgxpool.Pool, string, []string) {
	t.Helper()
	pool := testutil.SetupMigratedTestDB(t)
	dir := testutil.MigrationsDir(t)
	artifact, err := schemaauthority.ExpectedLedger(dir)
	require.NoError(t, err)
	require.Equal(t, schemaauthority.PreDispatchExpandHead, artifact[len(artifact)-1])
	preDispatchDown, err := os.ReadFile(filepath.Join(dir, schemaauthority.PreDispatchExpandHead+".down.sql"))
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), string(preDispatchDown))
	require.NoError(t, err)
	artifact = slices.Clone(artifact[:len(artifact)-1])
	require.Equal(t, schemaauthority.ContentReclaimExpandHead, artifact[len(artifact)-1])
	require.NoError(t, reclaimMigrationDown(t.Context(), pool, dir))
	artifact = slices.Clone(artifact[:len(artifact)-1])
	require.Equal(t, schemaauthority.ContentCaptureExpandHead, artifact[len(artifact)-1])
	require.NoError(t, contentMigrationDown(t.Context(), pool, dir))
	predecessor := slices.Clone(artifact[:len(artifact)-1])
	require.Equal(t, schemaauthority.BudgetSnapshotExpandHead, predecessor[len(predecessor)-1])
	require.Equal(t, predecessor, contentMigrationLedger(t, pool))
	return pool, dir, artifact
}

func TestContentCaptureMigrationRestoresBudgetCommitVerifierOnDown(t *testing.T) {
	pool, dir, _ := contentMigrationM118(t)
	definition := func() string {
		var value string
		require.NoError(t, pool.QueryRow(t.Context(), `SELECT pg_get_functiondef('verify_source_budget_coverage_commit()'::regprocedure)`).Scan(&value))
		return value
	}
	original := definition()
	require.NotContains(t, original, schemaauthority.ContentCaptureExpandHead)
	require.NoError(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, contentMigrationHooks(t)))
	require.Contains(t, definition(), schemaauthority.ContentCaptureExpandHead)
	require.NoError(t, contentMigrationDown(t.Context(), pool, dir))
	require.Equal(t, original, definition())
}

func contentMigrationProject(t *testing.T, pool *pgxpool.Pool) (int64, int64) {
	t.Helper()
	org := testutil.NewTestOrg(t, pool)
	var project int64
	require.NoError(t, pool.QueryRow(t.Context(), `INSERT INTO projects(org_id,name,slug,status,budget_period) VALUES($1,'Content migration','content-migration','active','none') RETURNING id`, org).Scan(&project))
	return org, project
}

func contentMigrationRegistration(org, project int64, label string, profiles []string) sourcefacts.Registration {
	verifier := sha256.Sum256([]byte("synthetic-content-migration-verifier:" + label))
	operation := sha256.Sum256([]byte("synthetic-content-migration-operation:" + label))
	return sourcefacts.Registration{SchemaVersion: sourcefacts.RegistrationVersion, TokenFormat: auth.ReceiverFeedTokenFormat, VerifierScheme: auth.ReceiverFeedVerifierScheme, KeyPrefix: "ur-xf1-" + hex.EncodeToString(verifier[:])[:13], VerifierSHA256: hex.EncodeToString(verifier[:]), OperationID: hex.EncodeToString(operation[:16]), SourceInstance: "synthetic-content-migration", OrgID: strconv.FormatInt(org, 10), ProjectID: strconv.FormatInt(project, 10), Profiles: profiles}
}

// Match the existing receiver/source migration tests' synthetic row fixtures;
// no real credential, caller permission, Provider attempt, or billing state is
// created. Registration digests use the production registration serializer.
func contentMigrationKey(t *testing.T, pool *pgxpool.Pool, org, project int64, registration sourcefacts.Registration, status, scope string) int64 {
	t.Helper()
	verifier, err := auth.DecodeReceiverFeedVerifier(registration.VerifierSHA256)
	require.NoError(t, err)
	hash := auth.HashReceiverFeedVerifier(verifier, "synthetic-content-migration-local-only")
	columns, values := "", ""
	args := []any{org, project, hash, registration.KeyPrefix, status, registration.VerifierScheme, registration.OperationID}
	if scope != "" {
		columns = ",source_lifecycle_scope"
		values = ",$8"
		args = append(args, scope)
	}
	var id int64
	require.NoError(t, pool.QueryRow(t.Context(), `INSERT INTO api_keys(org_id,project_id,key_type,name,key_hash,key_prefix,status,allowed_ips,allowed_models,purpose,verifier_scheme,registration_operation_id`+columns+`) VALUES($1,$2,'service','Synthetic migration key',$3,$4,$5,'127.0.0.1/32','','xinsight.source.read',$6,$7`+values+`) RETURNING id`, args...).Scan(&id))
	return id
}

func contentMigrationLegacy(t *testing.T, pool *pgxpool.Pool) (int64, int64, []int64) {
	t.Helper()
	org, project := contentMigrationProject(t, pool)
	_, err := pool.Exec(t.Context(), `INSERT INTO execution_source_connections(org_id,project_id,source_instance,status,scope_generation,profiles) VALUES($1,$2,'synthetic-content-migration','active',7,$3)`, org, project, []string{sourcefacts.ContextProfile})
	require.NoError(t, err)
	var ids []int64
	for _, status := range []string{"feed_active", "feed_staged", "feed_revoked", "disabled"} {
		registration := contentMigrationRegistration(org, project, status, []string{sourcefacts.ContextProfile})
		digest, err := registration.Hash()
		require.NoError(t, err)
		id := contentMigrationKey(t, pool, org, project, registration, status, "")
		ids = append(ids, id)
		_, err = pool.Exec(t.Context(), `INSERT INTO execution_source_credentials(api_key_id,org_id,project_id,registration_sha256,profiles) VALUES($1,$2,$3,$4,$5)`, id, org, project, digest, registration.Profiles)
		require.NoError(t, err)
	}
	return org, project, ids
}

func contentMigrationLegacyRows(t *testing.T, pool *pgxpool.Pool, org, project int64) []string {
	t.Helper()
	result := make([]string, 3)
	queries := []string{
		`SELECT (to_jsonb(c)-'content_safety_version')::text FROM execution_source_connections c WHERE org_id=$1 AND project_id=$2`,
		`SELECT jsonb_agg(to_jsonb(k)-'source_lifecycle_scope' ORDER BY id)::text FROM api_keys k WHERE org_id=$1 AND project_id=$2 AND COALESCE(to_jsonb(k)->>'source_lifecycle_scope','legacy')='legacy'`,
		`SELECT jsonb_agg(to_jsonb(c)-'source_lifecycle_scope' ORDER BY api_key_id)::text FROM execution_source_credentials c WHERE org_id=$1 AND project_id=$2 AND COALESCE(to_jsonb(c)->>'source_lifecycle_scope','legacy')='legacy'`,
	}
	for i, q := range queries {
		require.NoError(t, pool.QueryRow(t.Context(), q, org, project).Scan(&result[i]))
	}
	return result
}

func contentMigrationPresent(t *testing.T, pool *pgxpool.Pool, want bool) {
	t.Helper()
	var table, ledger bool
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT to_regclass('execution_content_objects') IS NOT NULL,EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, schemaauthority.ContentCaptureExpandHead).Scan(&table, &ledger))
	require.Equal(t, want, table)
	require.Equal(t, want, ledger)
}

func contentMigrationAuthTriggerState(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var state string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT COALESCE((SELECT tgenabled::text FROM pg_trigger WHERE tgrelid='public.api_keys'::regclass AND tgname='trg_api_keys_auth_epoch_monotonic'),'missing')`).Scan(&state))
	return state
}

func TestContentCaptureMigrationM118LegacyPreservationAndEmptyRoundTrip(t *testing.T) {
	pool, dir, artifact := contentMigrationM118(t)
	org, project, ids := contentMigrationLegacy(t, pool)
	// Non-source keys are not part of the backfill and must remain byte-for-byte
	// unchanged as well. This is a synthetic row, never a usable credential.
	_, err := pool.Exec(t.Context(), `INSERT INTO api_keys(org_id,project_id,key_type,name,key_hash,key_prefix,status,allowed_ips,allowed_models) VALUES($1,$2,'service','Synthetic ordinary key',$3,'synthetic-ordinary','active','','')`, org, project, strings.Repeat("d", 64))
	require.NoError(t, err)
	before := contentMigrationLegacyRows(t, pool, org, project)
	var originalBudgetProof string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT to_jsonb(c)::text FROM execution_source_budget_coverage c`).Scan(&originalBudgetProof))
	require.NoError(t, migrate.RunRuntime(t.Context(), pool, dir))
	contentMigrationPresent(t, pool, false)
	require.ErrorContains(t, migrate.Run(t.Context(), pool, dir), "requires explicit fleet preflight")
	for iteration := 0; iteration < 2; iteration++ {
		require.NoError(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, contentMigrationHooks(t)))
		contentMigrationPresent(t, pool, true)
		require.Equal(t, artifact, contentMigrationLedger(t, pool))
		require.Equal(t, before, contentMigrationLegacyRows(t, pool, org, project))
		var keyScopes, credentialScopes []string
		require.NoError(t, pool.QueryRow(t.Context(), `SELECT array_agg(source_lifecycle_scope ORDER BY id) FROM api_keys WHERE id=ANY($1::bigint[])`, ids).Scan(&keyScopes))
		require.Equal(t, []string{"legacy", "legacy", "legacy", "legacy"}, keyScopes)
		require.NoError(t, pool.QueryRow(t.Context(), `SELECT array_agg(source_lifecycle_scope ORDER BY api_key_id) FROM execution_source_credentials WHERE api_key_id=ANY($1::bigint[])`, ids).Scan(&credentialScopes))
		require.Equal(t, keyScopes, credentialScopes)
		var budgetProof string
		require.NoError(t, pool.QueryRow(t.Context(), `SELECT to_jsonb(c)::text FROM execution_source_budget_coverage c`).Scan(&budgetProof))
		require.Equal(t, originalBudgetProof, budgetProof, "M119 must preserve the original M118 certification bytes/fields")
		if iteration == 0 {
			require.NoError(t, contentMigrationDown(t.Context(), pool, dir))
			contentMigrationPresent(t, pool, false)
			require.Equal(t, artifact[:len(artifact)-1], contentMigrationLedger(t, pool))
			require.Equal(t, before, contentMigrationLegacyRows(t, pool, org, project))
		}
	}
	require.Equal(t, "O", contentMigrationAuthTriggerState(t, pool))
	var beforeEpoch, afterEpoch int64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT auth_epoch FROM api_keys WHERE id=$1`, ids[0]).Scan(&beforeEpoch))
	require.NoError(t, pool.QueryRow(t.Context(), `UPDATE api_keys SET allowed_ips='192.0.2.1/32' WHERE id=$1 RETURNING auth_epoch`, ids[0]).Scan(&afterEpoch))
	require.Equal(t, beforeEpoch+1, afterEpoch, "ordinary authority updates must still invalidate the previous epoch")
}

func TestContentCaptureMigrationBackfillFailureRestoresAuthTrigger(t *testing.T) {
	pool, dir, artifact := contentMigrationM118(t)
	org, project, _ := contentMigrationLegacy(t, pool)
	before := contentMigrationLegacyRows(t, pool, org, project)
	_, err := pool.Exec(t.Context(), `CREATE FUNCTION synthetic_fail_content_backfill() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic content backfill failure'; END $$;
CREATE TRIGGER zzz_synthetic_fail_content_backfill BEFORE UPDATE ON api_keys FOR EACH ROW EXECUTE FUNCTION synthetic_fail_content_backfill()`)
	require.NoError(t, err)
	require.ErrorContains(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, contentMigrationHooks(t)), "synthetic content backfill failure")
	contentMigrationPresent(t, pool, false)
	require.Equal(t, artifact[:len(artifact)-1], contentMigrationLedger(t, pool))
	require.Equal(t, before, contentMigrationLegacyRows(t, pool, org, project))
	require.Equal(t, "O", contentMigrationAuthTriggerState(t, pool))
	var lifecycleColumn bool
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='api_keys' AND column_name='source_lifecycle_scope')`).Scan(&lifecycleColumn))
	require.False(t, lifecycleColumn)
	_, err = pool.Exec(t.Context(), `DROP TRIGGER zzz_synthetic_fail_content_backfill ON api_keys; DROP FUNCTION synthetic_fail_content_backfill()`)
	require.NoError(t, err)
	require.NoError(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, contentMigrationHooks(t)))
	require.Equal(t, before, contentMigrationLegacyRows(t, pool, org, project))
}

func TestContentCaptureMigrationRejectsUnexpectedAuthTriggerState(t *testing.T) {
	for _, mode := range []string{"DISABLE", "ENABLE REPLICA", "ENABLE ALWAYS", "DROP"} {
		t.Run(mode, func(t *testing.T) {
			pool, dir, artifact := contentMigrationM118(t)
			org, project, _ := contentMigrationLegacy(t, pool)
			before := contentMigrationLegacyRows(t, pool, org, project)
			sql := "ALTER TABLE api_keys " + mode + " TRIGGER trg_api_keys_auth_epoch_monotonic"
			if mode == "DROP" {
				sql = "DROP TRIGGER trg_api_keys_auth_epoch_monotonic ON api_keys"
			}
			_, err := pool.Exec(t.Context(), sql)
			require.NoError(t, err)
			state := contentMigrationAuthTriggerState(t, pool)
			require.ErrorContains(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, contentMigrationHooks(t)), "content migration requires the original enabled API key auth epoch trigger")
			contentMigrationPresent(t, pool, false)
			require.Equal(t, artifact[:len(artifact)-1], contentMigrationLedger(t, pool))
			require.Equal(t, before, contentMigrationLegacyRows(t, pool, org, project))
			require.Equal(t, state, contentMigrationAuthTriggerState(t, pool), "rejected migration must not repair or change the original trigger mode")
		})
	}
}

func TestContentCaptureMigrationBootstrapFailureIsAtomic(t *testing.T) {
	pool, dir, artifact := contentMigrationM118(t)
	org, project, _ := contentMigrationLegacy(t, pool)
	before := contentMigrationLegacyRows(t, pool, org, project)
	hooks := contentMigrationHooks(t)
	hooks.After = func(context.Context, pgx.Tx) error { return errors.New("synthetic M119 bootstrap failure") }
	require.ErrorContains(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, hooks), "synthetic M119 bootstrap failure")
	contentMigrationPresent(t, pool, false)
	require.Equal(t, artifact[:len(artifact)-1], contentMigrationLedger(t, pool))
	require.Equal(t, before, contentMigrationLegacyRows(t, pool, org, project))
	require.Equal(t, "O", contentMigrationAuthTriggerState(t, pool))
	require.NoError(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, contentMigrationHooks(t)))
}

func TestContentCaptureMigrationRejectsUnsupportedGenerationPredicates(t *testing.T) {
	for _, missing := range []string{"head", "implementation", "typed_kind"} {
		for _, draining := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/draining=%t", missing, draining), func(t *testing.T) {
				pool, dir, artifact := contentMigrationM118(t)
				manifest := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
				if missing == "head" {
					manifest.SchemaHeads = slices.DeleteFunc(manifest.SchemaHeads, func(h string) bool { return h == schemaauthority.ContentCaptureExpandHead })
				} else {
					for i := range manifest.Implementations {
						if manifest.Implementations[i].ID == runtimegeneration.SourceContentLifecycleContract {
							if missing == "typed_kind" {
								manifest.Implementations[i].Kind = "source_facts"
							} else {
								manifest.Implementations = slices.Delete(manifest.Implementations, i, i+1)
							}
							break
						}
					}
				}
				identity := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("a", 32), Artifact: "sha256:" + strings.Repeat("b", 64)}
				_, head, err := runtimegeneration.Renew(t.Context(), pool, identity, manifest, artifact, draining)
				require.NoError(t, err)
				require.Equal(t, schemaauthority.BudgetSnapshotExpandHead, head)
				// Even an expired recovery member is not a stopped runtime.
				_, err = pool.Exec(t.Context(), `UPDATE runtime_generations SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE instance_id=$1`, identity.Instance)
				require.NoError(t, err)
				require.ErrorContains(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, contentMigrationHooks(t)), "all serving and recovery generations")
				contentMigrationPresent(t, pool, false)
				require.Equal(t, artifact[:len(artifact)-1], contentMigrationLedger(t, pool))
				// Test-owned generation has no ingress membership or live process. This is
				// a database predicate test, not evidence that an old binary was stopped.
				_, err = pool.Exec(t.Context(), `UPDATE runtime_generations SET state='stopped' WHERE instance_id=$1`, identity.Instance)
				require.NoError(t, err)
				require.NoError(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, contentMigrationHooks(t)))
				// Exercise the installed SQL trigger directly, rather than only the Go
				// Renew precheck: the immutable incompatible manifest cannot become live.
				_, err = pool.Exec(t.Context(), `UPDATE runtime_generations SET state='draining' WHERE instance_id=$1`, identity.Instance)
				require.ErrorContains(t, err, "runtime requires the content lifecycle implementation")
				var state string
				require.NoError(t, pool.QueryRow(t.Context(), `SELECT state FROM runtime_generations WHERE instance_id=$1`, identity.Instance).Scan(&state))
				require.Equal(t, "stopped", state)
				contentMigrationPresent(t, pool, true)
			})
		}
	}
}

func contentMigrationSQLState(t *testing.T, err error, code string) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, code, pgErr.Code)
}

func TestContentCaptureMigrationScopeConstraintsAndSafetyTrigger(t *testing.T) {
	pool, dir, _ := contentMigrationM118(t)
	org, project, legacyIDs := contentMigrationLegacy(t, pool)
	require.NoError(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, contentMigrationHooks(t)))
	before := contentMigrationLegacyRows(t, pool, org, project)
	registration := contentMigrationRegistration(org, project, "content", []string{sourcefacts.ContentProfile})
	id := contentMigrationKey(t, pool, org, project, registration, "feed_active", "content")
	digest, err := sourcefacts.HashContentRegistration(registration)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `INSERT INTO execution_source_credentials(api_key_id,org_id,project_id,registration_sha256,profiles,source_lifecycle_scope) VALUES($1,$2,$3,$4,$5,'content')`, id, org, project, digest, []string{sourcefacts.ContextProfile})
	contentMigrationSQLState(t, err, "23514")
	_, err = pool.Exec(t.Context(), `INSERT INTO execution_source_credentials(api_key_id,org_id,project_id,registration_sha256,profiles,source_lifecycle_scope) VALUES($1,$2,$3,$4,$5,'content')`, id, org, project, digest, registration.Profiles)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `INSERT INTO api_keys(org_id,project_id,key_type,name,key_hash,key_prefix,status,allowed_ips,allowed_models,purpose,verifier_scheme,registration_operation_id,source_lifecycle_scope) SELECT org_id,project_id,key_type,'Second content active',$2,'ur-xf1-fffffffffffff',status,allowed_ips,allowed_models,purpose,verifier_scheme,'ffffffffffffffffffffffffffffffff','content' FROM api_keys WHERE id=$1`, id, strings.Repeat("f", 64))
	contentMigrationSQLState(t, err, "23505")
	require.Equal(t, before, contentMigrationLegacyRows(t, pool, org, project), "C active coexists with the exact unchanged legacy active/staged rows")
	_, err = pool.Exec(t.Context(), `INSERT INTO execution_content_streams(org_id,project_id,source_instance) VALUES($1,$2,'synthetic-content-migration')`, org, project)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `INSERT INTO execution_source_content_scopes(org_id,project_id) VALUES($1,$2)`, org, project)
	require.NoError(t, err)
	bindingSQL := `INSERT INTO execution_source_content_bindings(api_key_id,org_id,project_id,coverage_after_sequence,coverage_time,allowed_purposes) VALUES($1,$2,$3,0,clock_timestamp(),$4)`
	_, err = pool.Exec(t.Context(), bindingSQL, legacyIDs[0], org, project, []string{"human_review"})
	contentMigrationSQLState(t, err, "23503")
	_, err = pool.Exec(t.Context(), bindingSQL, id, org, project, []string{"machine_training"})
	contentMigrationSQLState(t, err, "23514")
	_, err = pool.Exec(t.Context(), bindingSQL, id, org, project, []string{"human_review"})
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `UPDATE execution_source_content_bindings SET coverage_after_sequence=1 WHERE api_key_id=$1`, id)
	require.ErrorContains(t, err, "execution source facts are immutable")
	var generation int64
	require.NoError(t, pool.QueryRow(t.Context(), `UPDATE execution_source_connections SET status='paused',content_safety_version=999 WHERE org_id=$1 AND project_id=$2 RETURNING content_safety_version`, org, project).Scan(&generation))
	require.Equal(t, int64(2), generation)
	require.NoError(t, pool.QueryRow(t.Context(), `UPDATE execution_source_connections SET content_safety_version=999 WHERE org_id=$1 AND project_id=$2 RETURNING content_safety_version`, org, project).Scan(&generation))
	require.Equal(t, int64(2), generation)
	require.NoError(t, pool.QueryRow(t.Context(), `UPDATE execution_source_connections SET status='active' WHERE org_id=$1 AND project_id=$2 RETURNING content_safety_version`, org, project).Scan(&generation))
	require.Equal(t, int64(3), generation)
	require.ErrorContains(t, contentMigrationDown(t.Context(), pool, dir), "unsafe down refused")
	contentMigrationPresent(t, pool, true)
}

func TestContentCaptureMigrationDownRefusesRetainedDataAndPreservesLedger(t *testing.T) {
	for _, kind := range []string{"stream", "policy", "history"} {
		t.Run(kind, func(t *testing.T) {
			pool, dir, artifact := contentMigrationM118(t)
			require.NoError(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, contentMigrationHooks(t)))
			org, project := contentMigrationProject(t, pool)
			if kind == "policy" {
				config := []byte(`{"mode":"metadata_only","supported_protocols":["openai.chat"],"observation_points":["client_request"],"allowed_purposes":[]}`)
				_, err := pool.Exec(t.Context(), `INSERT INTO execution_content_policies(org_id,project_id,desired_version,effective_version,desired_config,effective_config,effective_at,application_status,application_reason) VALUES($1,$2,1,1,$3,$3,clock_timestamp(),'applied','none')`, org, project, config)
				require.NoError(t, err)
			} else {
				_, err := pool.Exec(t.Context(), `INSERT INTO execution_content_streams(org_id,project_id,source_instance) VALUES($1,$2,'synthetic-content-migration')`, org, project)
				require.NoError(t, err)
				if kind == "history" {
					service, err := sourcefacts.New(pool, sourcefacts.Options{Enabled: true, ContentEnabled: true, SourceInstance: "synthetic-content-migration", CursorSecret: "synthetic-migration-content-journal-secret"})
					require.NoError(t, err)
					stamp := "2026-09-15T00:00:00.000000Z"
					manifest := sourcefacts.ContentManifest{SchemaVersion: sourcefacts.ContentManifestVersion, SourceInstance: "synthetic-content-migration", OrgID: strconv.FormatInt(org, 10), ProjectID: strconv.FormatInt(project, 10), RequestID: "synthetic-migration-history", Profile: sourcefacts.ContentProfile, Revision: "1", OccurredAt: stamp, RecordedAt: stamp, Protocol: "openai.chat", ObservationPoint: "client_request", ContentItemID: "body-0", Capture: sourcefacts.ContentCapture{Mode: "metadata_only", PolicyRef: "synthetic-migration-policy", PolicyVersion: "1", PolicyEffectiveAt: stamp, Completeness: "not_captured", Reason: "policy_metadata_only"}}
					manifest.SourceFactID = manifest.FactID()
					manifest.EventID = manifest.EventIDForRevision()
					_, err = service.AppendContentManifest(t.Context(), manifest, 1)
					require.NoError(t, err)
					_, err = pool.Exec(t.Context(), `UPDATE execution_content_manifests SET document=document WHERE org_id=$1 AND project_id=$2`, org, project)
					require.ErrorContains(t, err, "execution source facts are immutable", "retained canonical history must be immutable")
				}
			}
			snapshot := func() string {
				var data string
				require.NoError(t, pool.QueryRow(t.Context(), `SELECT jsonb_build_object('streams',(SELECT jsonb_agg(to_jsonb(s)) FROM execution_content_streams s),'policies',(SELECT jsonb_agg(to_jsonb(p)) FROM execution_content_policies p),'manifests',(SELECT jsonb_agg(to_jsonb(m)) FROM execution_content_manifests m))::text`).Scan(&data))
				return data
			}
			before := snapshot()
			require.ErrorContains(t, contentMigrationDown(t.Context(), pool, dir), "unsafe down refused")
			require.Equal(t, before, snapshot())
			require.Equal(t, artifact, contentMigrationLedger(t, pool))
			contentMigrationPresent(t, pool, true)
		})
	}
}

func TestContentCaptureMigrationAllowsCompatibleGenerationButDownRequiresStop(t *testing.T) {
	pool, dir, artifact := contentMigrationM118(t)
	identity := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("c", 32), Artifact: "sha256:" + strings.Repeat("d", 64)}
	manifest := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
	_, head, err := runtimegeneration.Renew(t.Context(), pool, identity, manifest, artifact, true)
	require.NoError(t, err)
	require.Equal(t, schemaauthority.BudgetSnapshotExpandHead, head)
	require.NoError(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, contentMigrationHooks(t)))
	_, head, err = runtimegeneration.Renew(t.Context(), pool, identity, manifest, artifact, true)
	require.NoError(t, err)
	require.Equal(t, schemaauthority.ContentCaptureExpandHead, head)
	require.ErrorContains(t, contentMigrationDown(t.Context(), pool, dir), "unsafe down refused")
	require.Equal(t, artifact, contentMigrationLedger(t, pool))
	// The synthetic member has no actual process or ingress membership.
	_, err = pool.Exec(t.Context(), `UPDATE runtime_generations SET state='stopped' WHERE instance_id=$1`, identity.Instance)
	require.NoError(t, err)
	require.NoError(t, contentMigrationDown(t.Context(), pool, dir))
	require.Equal(t, artifact[:len(artifact)-1], contentMigrationLedger(t, pool))
}
