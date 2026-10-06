package testutil

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/money"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testdb"
)

var migratedTestDBTemplate struct {
	sync.Mutex
	enabled   bool
	baseDSN   string
	adminPool *pgxpool.Pool
	dbName    string
}

// TestDB_DSN returns the test database DSN from environment or default.
// Uses uniroute_test database to avoid polluting development data.
func TestDB_DSN() string {
	return testdb.DSN()
}

// SetupTestDB connects to uniroute_test, truncates all business tables,
// and returns a connection pool. The pool is closed on test cleanup.
func SetupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := TestDB_DSN()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		testDatabaseUnavailable(t, "cannot connect to test DB: %v (set TEST_DATABASE_URL or create uniroute_test)", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		testDatabaseUnavailable(t, "cannot ping test DB: %v", err)
	}

	// Truncate all business tables in dependency order
	TruncateAll(t, pool)

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

// SetupIsolatedTestDB creates a temporary PostgreSQL database for integration
// tests that must run migrations from a clean schema.
func SetupIsolatedTestDB(t *testing.T) *pgxpool.Pool {
	return testdb.SetupIsolated(t)
}

// SetupMigratedTestDB creates a clean temporary database and applies all repo migrations.
func SetupMigratedTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if migratedTestDBTemplateEnabled() {
		return setupMigratedTestDBFromTemplate(t)
	}
	pool := SetupIsolatedTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, runStrictTestArtifactInstall(ctx, pool, MigrationsDir(t)), "install exact test artifact schema")
	TruncateAll(t, pool)
	return pool
}

// SetupMigratedThroughTestDB installs an exact historical compatible-expand
// prefix in an isolated database, for testing one later migration's backfill.
func SetupMigratedThroughTestDB(t *testing.T, ceiling string) *pgxpool.Pool {
	t.Helper()
	pool := SetupIsolatedTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, runStrictTestArtifactInstallThrough(ctx, pool, MigrationsDir(t), ceiling))
	TruncateAll(t, pool)
	return pool
}

// EnableMigratedTestDBTemplate makes SetupMigratedTestDB migrate and clean one
// process-scoped template database, then clone it for each test. The caller must
// invoke CleanupMigratedTestDBTemplate after the package test run completes.
func EnableMigratedTestDBTemplate() {
	migratedTestDBTemplate.Lock()
	defer migratedTestDBTemplate.Unlock()
	migratedTestDBTemplate.enabled = true
}

// RunWithMigratedTestDBTemplate runs an integration-test package with one
// migrated template database. Every SetupMigratedTestDB call still receives
// an isolated clone, while the package pays the migration cost only once.
func RunWithMigratedTestDBTemplate(m *testing.M) {
	EnableMigratedTestDBTemplate()
	code := m.Run()
	if err := CleanupMigratedTestDBTemplate(); err != nil {
		fmt.Fprintf(os.Stderr, "cleanup migrated test database template: %v\n", err)
		code = 1
	}
	os.Exit(code)
}

// CleanupMigratedTestDBTemplate removes the process-scoped template database.
func CleanupMigratedTestDBTemplate() error {
	migratedTestDBTemplate.Lock()
	adminPool := migratedTestDBTemplate.adminPool
	dbName := migratedTestDBTemplate.dbName
	migratedTestDBTemplate.enabled = false
	migratedTestDBTemplate.baseDSN = ""
	migratedTestDBTemplate.adminPool = nil
	migratedTestDBTemplate.dbName = ""
	migratedTestDBTemplate.Unlock()

	if adminPool == nil || dbName == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := dropTemporaryDatabase(ctx, adminPool, dbName)
	adminPool.Close()
	return err
}

func migratedTestDBTemplateEnabled() bool {
	migratedTestDBTemplate.Lock()
	defer migratedTestDBTemplate.Unlock()
	return migratedTestDBTemplate.enabled
}

func setupMigratedTestDBFromTemplate(t *testing.T) *pgxpool.Pool {
	t.Helper()
	baseDSN := TestDB_DSN()
	adminPool, templateName := ensureMigratedTestDBTemplate(t, baseDSN)
	_, cloneDSN, cloneName, err := tempDatabaseDSNs(baseDSN)
	require.NoError(t, err, "build cloned test database DSN")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = adminPool.Exec(ctx, `CREATE DATABASE `+quoteIdent(cloneName)+` TEMPLATE `+quoteIdent(templateName))
	if err != nil {
		testDatabaseUnavailable(t, "cannot clone migrated test database %s: %v", cloneName, err)
	}

	pool, err := pgxpool.New(ctx, cloneDSN)
	if err != nil {
		_ = dropTemporaryDatabase(ctx, adminPool, cloneName)
		testDatabaseUnavailable(t, "cannot connect to cloned test database %s: %v", cloneName, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		_ = dropTemporaryDatabase(ctx, adminPool, cloneName)
		testDatabaseUnavailable(t, "cannot ping cloned test database %s: %v", cloneName, err)
	}

	t.Cleanup(func() {
		pool.Close()
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		if err := dropTemporaryDatabase(dropCtx, adminPool, cloneName); err != nil {
			t.Errorf("drop cloned test database %s: %v", cloneName, err)
		}
	})
	return pool
}

func ensureMigratedTestDBTemplate(t *testing.T, baseDSN string) (*pgxpool.Pool, string) {
	t.Helper()
	migratedTestDBTemplate.Lock()
	defer migratedTestDBTemplate.Unlock()
	if migratedTestDBTemplate.adminPool != nil {
		require.Equal(t, migratedTestDBTemplate.baseDSN, baseDSN, "migrated test database template DSN changed during one test process")
		return migratedTestDBTemplate.adminPool, migratedTestDBTemplate.dbName
	}

	adminDSN, templateDSN, templateName, err := tempDatabaseDSNs(baseDSN)
	require.NoError(t, err, "build migrated template database DSN")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	adminPool, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		testDatabaseUnavailable(t, "cannot connect to PostgreSQL admin database for migrated template: %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		testDatabaseUnavailable(t, "cannot ping PostgreSQL admin database for migrated template: %v", err)
	}
	if _, err := adminPool.Exec(ctx, `CREATE DATABASE `+quoteIdent(templateName)); err != nil {
		adminPool.Close()
		testDatabaseUnavailable(t, "cannot create migrated template database %s: %v", templateName, err)
	}

	templatePool, err := pgxpool.New(ctx, templateDSN)
	if err != nil {
		_ = dropTemporaryDatabase(ctx, adminPool, templateName)
		adminPool.Close()
		testDatabaseUnavailable(t, "cannot connect to migrated template database %s: %v", templateName, err)
	}
	if err := templatePool.Ping(ctx); err != nil {
		templatePool.Close()
		_ = dropTemporaryDatabase(ctx, adminPool, templateName)
		adminPool.Close()
		testDatabaseUnavailable(t, "cannot ping migrated template database %s: %v", templateName, err)
	}
	if err := runStrictTestArtifactInstall(ctx, templatePool, MigrationsDir(t)); err != nil {
		templatePool.Close()
		_ = dropTemporaryDatabase(ctx, adminPool, templateName)
		adminPool.Close()
		require.NoError(t, err, "run migrations in template test database")
	}
	TruncateAll(t, templatePool)
	templatePool.Close()

	migratedTestDBTemplate.baseDSN = baseDSN
	migratedTestDBTemplate.adminPool = adminPool
	migratedTestDBTemplate.dbName = templateName
	return adminPool, templateName
}

// MigrationsDir returns the repository migrations directory from the testutil package location.
func MigrationsDir(t *testing.T) string {
	return testdb.MigrationsDir(t)
}

func tempDatabaseDSNs(baseDSN string) (adminDSN, tempDSN, dbName string, err error) {
	return testdb.TempDatabaseDSNs(baseDSN)
}

func quoteIdent(value string) string {
	return testdb.QuoteIdent(value)
}

func dropTemporaryDatabase(ctx context.Context, adminPool *pgxpool.Pool, dbName string) error {
	return testdb.Drop(ctx, adminPool, dbName)
}

func testDatabaseUnavailable(t *testing.T, format string, args ...any) {
	testdb.Unavailable(t, format, args...)
}

func runStrictTestArtifactInstall(ctx context.Context, pool *pgxpool.Pool, migrationsDir string) error {
	return runStrictTestArtifactInstallThrough(ctx, pool, migrationsDir, schemaauthority.LatestSupportedMigration)
}

func runStrictTestArtifactInstallThrough(ctx context.Context, pool *pgxpool.Pool, migrationsDir, ceiling string) error {
	artifactLedger, err := schemaauthority.ExpectedLedger(migrationsDir)
	if err != nil {
		return err
	}
	if err := publication.VerifyArtifactPublicationInventory(migrationsDir, artifactLedger); err != nil {
		return err
	}
	if err := migrate.RunThrough(ctx, pool, migrationsDir, publication.BaselineMigrationM100); err != nil {
		return err
	}
	declarations, err := publication.CompiledDeclarations()
	if err != nil {
		return err
	}
	if len(declarations) == 0 {
		if err := migrate.Run(ctx, pool, migrationsDir); err != nil {
			return err
		}
		return publication.VerifyRuntimeStaticProjection(ctx, pool, artifactLedger)
	}
	files, err := publication.ReadArtifactMigrationFiles(migrationsDir, artifactLedger)
	if err != nil {
		return err
	}
	artifact := publication.ArtifactIdentity{
		Kind: publication.ArtifactKindSource, Commit: strings.Repeat("1", 40), Tree: strings.Repeat("2", 40),
		Version: "integration-test", PublisherExecutableDigest: "sha256:" + strings.Repeat("3", 64),
	}
	for _, compiled := range declarations {
		declaration, predecessor, err := publication.CompiledPublication(compiled.PublicationID)
		if err != nil {
			return err
		}
		if err := publication.VerifyGeneratedMigrationFiles(migrationsDir, predecessor, declaration); err != nil {
			return err
		}
		downDigest, verifyDigest, err := publication.ReadPublicationAuxiliaryDigests(migrationsDir, declaration.PublicationID)
		if err != nil {
			return err
		}
		authority := publication.PlanAuthority{
			Artifact: artifact, ArtifactMigrationLedger: artifactLedger, ArtifactMigrationFiles: files,
			PublicationDownSHA256: downDigest, PublicationVerifySHA256: verifyDigest,
			Predecessor: predecessor, Declaration: declaration,
		}
		plan, err := publication.PlanTarget(ctx, pool, "integration-test", authority)
		if err != nil {
			return err
		}
		if plan.Validation != publication.ValidationValid {
			return fmt.Errorf("test artifact publication %s conflicts with clean install: %v", declaration.PublicationID, plan.Conflicts)
		}
		hooks, err := publication.HooksForPlan("integration-test", authority, plan)
		if err != nil {
			return err
		}
		if err := migrate.RunPublication(ctx, pool, migrationsDir, plan.ApplyMigrations, hooks); err != nil {
			return err
		}
	}
	// This test-only caller owns an isolated database with no serving fleet.
	// Exercise the same explicit expand owner; ordinary startup cannot apply it.
	for _, expandVersion := range []string{schemaauthority.CapabilityExpandHead, schemaauthority.StreamCounterExpandHead, schemaauthority.SourceFactsExpandHead, schemaauthority.ObservationExpandHead, schemaauthority.BudgetSnapshotExpandHead, schemaauthority.ContentCaptureExpandHead, schemaauthority.ContentReclaimExpandHead, schemaauthority.PreDispatchExpandHead, schemaauthority.ScopedContentExpandHead, schemaauthority.XInsightPolicyExpandHead, schemaauthority.XInsightAdoptionExpandHead, schemaauthority.XInsightCredentialExpandHead} {
		if expandVersion > ceiling {
			break
		}
		present := false
		for _, version := range artifactLedger {
			if version == expandVersion {
				present = true
			}
		}
		if !present {
			continue
		}
		wire, err := os.ReadFile(filepath.Join(migrationsDir, expandVersion+".up.sql"))
		if err != nil {
			return err
		}
		err = migrate.RunCompatibleExpand(ctx, pool, migrationsDir, migrate.CompatibleExpandHooks{
			Version: expandVersion, ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(wire)),
			After: func(ctx context.Context, tx pgx.Tx) error {
				if expandVersion != schemaauthority.BudgetSnapshotExpandHead {
					return nil
				}
				// Explicit synthetic support proof for this empty, task-owned DB.
				// Do not import runtimegeneration: its own tests use this helper.
				proof := []byte(`{"compatibility_epoch":"unirouter.runtime-compatibility.v1","implementations":[{"id":"unirouter.gateway-financial-journal.v2","kind":"financial_journal"},{"id":"unirouter.budget-snapshot.v1","kind":"source_budgets"}],"schema_heads":["000118_source_budget_snapshots"],"schema_version":"unirouter.runtime-support.v1"}`)
				_, err := tx.Exec(ctx, `UPDATE execution_source_budget_coverage SET state='certified',certified_from=clock_timestamp(),
 schema_ledger_sha256=(SELECT encode(sha256(convert_to(string_agg(version,E'\n' ORDER BY version),'UTF8')),'hex') FROM schema_migrations),
                    migration_sha256=$1,support_manifest_sha256=$2,support_manifest_bytes=$3 WHERE state='pending' AND ended_at IS NULL`,
					fmt.Sprintf("sha256:%x", sha256.Sum256(wire)), fmt.Sprintf("%x", sha256.Sum256(proof)), proof)
				return err
			},
			Before: func(ctx context.Context, conn *pgxpool.Conn) error {
				var name string
				if err := conn.QueryRow(ctx, `SELECT current_database()`).Scan(&name); err != nil {
					return err
				}
				if !strings.HasPrefix(name, "uniroute_it_") {
					return fmt.Errorf("explicit test expand requires a task-owned isolated database")
				}
				return nil
			},
		})
		if err != nil {
			return err
		}
	}
	if ceiling != schemaauthority.LatestSupportedMigration {
		var actual string
		if err := pool.QueryRow(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&actual); err != nil {
			return err
		}
		if actual != ceiling {
			return fmt.Errorf("test artifact prefix got %s, want %s", actual, ceiling)
		}
		return nil
	}
	// Apply any trailing ordinary migrations. A missed declaration-owned
	// migration still fails closed in migrate.Run instead of being bypassed.
	if err := migrate.Run(ctx, pool, migrationsDir); err != nil {
		return err
	}
	return publication.VerifyRuntimeStaticProjection(ctx, pool, artifactLedger)
}

// InstallExactTestArtifact installs the repository's complete test artifact
// through the same explicit declaration-publication path required by Strict
// V1. It intentionally does not truncate migration seed data.
func InstallExactTestArtifact(ctx context.Context, pool *pgxpool.Pool, migrationsDir string) error {
	return runStrictTestArtifactInstall(ctx, pool, migrationsDir)
}

// RewindCapabilityPublicationsForLegacyMigrationTest removes known trailing
// ordinary test migrations and declaration-owned test publications in exact
// reverse schema order when a historical rollback fixture must establish the
// M100 schema ceiling. Every guarded down migration runs only when that exact
// ledger entry is present; production recovery continues to require the
// supported migration or Publication V1 owner path.
// RewindEmptyBudgetSnapshotsForLegacyTest must run before a legacy fixture
// creates projects or other D history. It uses the guarded product down.
func RewindEmptyBudgetSnapshotsForLegacyTest(ctx context.Context, pool *pgxpool.Pool, migrationsDir string) error {
	for _, version := range []string{schemaauthority.XInsightCredentialExpandHead, schemaauthority.XInsightAdoptionExpandHead, schemaauthority.XInsightPolicyExpandHead, schemaauthority.ScopedContentExpandHead, schemaauthority.PreDispatchExpandHead, schemaauthority.ContentReclaimExpandHead, schemaauthority.ContentCaptureExpandHead, schemaauthority.BudgetSnapshotExpandHead} {
		var applied bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&applied); err != nil {
			return err
		}
		if !applied {
			continue
		}
		wire, err := os.ReadFile(filepath.Join(migrationsDir, version+".down.sql"))
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(wire)); err != nil {
			return err
		}
	}
	return nil
}

// RewindEmptySourceFactsForLegacyTest restores the real M115 predecessor using
// guarded product downs without erasing data-bearing source evidence.
func RewindEmptySourceFactsForLegacyTest(ctx context.Context, pool *pgxpool.Pool, migrationsDir string) error {
	for _, version := range []string{schemaauthority.XInsightCredentialExpandHead, schemaauthority.XInsightAdoptionExpandHead, schemaauthority.XInsightPolicyExpandHead, schemaauthority.ScopedContentExpandHead, schemaauthority.PreDispatchExpandHead, schemaauthority.ContentReclaimExpandHead, schemaauthority.ContentCaptureExpandHead, schemaauthority.BudgetSnapshotExpandHead, schemaauthority.ObservationExpandHead, schemaauthority.SourceFactsExpandHead} {
		var applied bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&applied); err != nil {
			return err
		} else if !applied {
			continue
		}
		wire, err := os.ReadFile(filepath.Join(migrationsDir, version+".down.sql"))
		if err != nil {
			return err
		}
		if _, err = pool.Exec(ctx, string(wire)); err != nil {
			return err
		}
	}
	return nil
}

func RewindCapabilityPublicationsForLegacyMigrationTest(ctx context.Context, pool *pgxpool.Pool, migrationsDir string) error {
	for _, rollback := range []struct {
		migrationID string
		kind        string
	}{
		{migrationID: "000125_xinsight_control_credential_lifecycle", kind: "unused X-Insight control credential lifecycle expand test artifact"},
		{migrationID: "000124_xinsight_receiver_adoption", kind: "unused X-Insight receiver adoption expand test artifact"},
		{migrationID: "000123_xinsight_policy_delegation", kind: "unused X-Insight machine delegation expand test artifact"},
		{migrationID: "000122_scoped_content_policy", kind: "unused scoped content expand test artifact"},
		{migrationID: "000121_gateway_pre_dispatch_terminal", kind: "unused pre-dispatch terminal expand test artifact"},
		{migrationID: "000120_unpublished_content_reclamation", kind: "unused content reclamation expand test artifact"},
		{migrationID: "000119_execution_content_capture", kind: "empty content capture expand test artifact"},
		{migrationID: "000118_source_budget_snapshots", kind: "unused budget snapshot expand test artifact"},
		{migrationID: "000117_execution_observations", kind: "empty source observations expand test artifact"},
		{migrationID: "000116_execution_source_facts", kind: "empty source facts expand test artifact"},
		{migrationID: "000115_stream_counter_version", kind: "empty stream counter expand test artifact"},
		{migrationID: "000114_capability_delivery_expand", kind: "empty compatible expand test artifact"},
		{migrationID: "000113_glm_stream_function_tools_qualification", kind: "capability publication test artifact"},
		{migrationID: "000112_repeat_model_successor_cutover", kind: "repeat model successor lifecycle test artifact"},
		{migrationID: "000111_minimax_m27_stream_function_tools_qualification", kind: "capability publication test artifact"},
		{migrationID: "000110_openai_chat_runtime_capability_qualification", kind: "capability publication test artifact"},
		{migrationID: "000109_staging_model_successor_cutover", kind: "trailing legacy-test migration"},
		{migrationID: "000108_minimax_cache_threshold_qualification", kind: "capability publication test artifact"},
		{migrationID: "000107_stable_model_successor_cutover", kind: "trailing legacy-test migration"},
		{migrationID: "000106_minimax_official_cn_payg_endpoint_qualification", kind: "capability publication test artifact"},
		{migrationID: "000105_glm_toggle_function_tools_qualification", kind: "capability publication test artifact"},
		{migrationID: "000104_minimax_payg_function_tools_qualification", kind: "capability publication test artifact"},
		{migrationID: "000103_minimax_m3_glm53_function_tools_qualification", kind: "capability publication test artifact"},
		{migrationID: "000102_kimi_k3_chat_core_qualification", kind: "capability publication test artifact"},
		{migrationID: "000101_kimi_k27_chat_core_qualification", kind: "capability publication test artifact"},
	} {
		migrationID := rollback.migrationID
		var applied bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, migrationID,
		).Scan(&applied); err != nil {
			return fmt.Errorf("query %s %s: %w", rollback.kind, migrationID, err)
		}
		if !applied {
			continue
		}
		down, err := os.ReadFile(filepath.Join(migrationsDir, migrationID+".down.sql"))
		if err != nil {
			return fmt.Errorf("read %s rollback %s: %w", rollback.kind, migrationID, err)
		}
		if _, err := pool.Exec(ctx, string(down)); err != nil {
			return fmt.Errorf("rewind %s %s: %w", rollback.kind, migrationID, err)
		}
	}
	return nil
}

// TruncateAll truncates all business tables in dependency order.
// Skips schema_migrations and settings (base config).
func TruncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	// Security-control state is reset explicitly for isolated tests. Keep the
	// release_state singleton itself because runtime code relies on its presence.
	for _, statement := range []string{
		"DELETE FROM release_history",
		"DELETE FROM commercial_license_entitlements",
		"UPDATE release_state SET release_sequence = 0, version = '', commit = '', package_sha256 = NULL, trusted_key_id = '', target_schema = '', manifest = NULL, updated_at = NOW() WHERE singleton = TRUE",
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Logf("release-control reset warning for %q: %v", statement, err)
		}
	}

	tables := []string{
		"model_successor_cutover_receipt_model_sets",
		"model_successor_cutover_receipts",
		"xinsight_connector_phase_operations",
		"xinsight_connector_control_bindings",
		"xinsight_connector_operations",
		"xinsight_connector_installations",
		"async_media_limit_reservations",
		"api_keys",
		"access_operations",
		"access_credentials",
		"access_grant_revisions",
		"access_grants",
		"access_requests",
		"workload_application_client_profiles",
		"response_cache_commercial_receipts",
		"response_cache_delivery_receipts",
		"invocation_optimization_receipts",
		"invocation_optimization_policy_revisions",
		"invocation_optimization_policies",
		"model_deployment_lifecycle_receipt_qualifications",
		"model_deployment_lifecycle_receipt_releases",
		"model_deployment_lifecycle_receipts",
		"model_deployment_release_authoring_receipts",
		"model_operation_public_execution_bindings",
		"model_capability_routing_approvals",
		"model_capability_probes",
		"runtime_route_revision_items",
		"runtime_change_events",
		"runtime_change_slots",
		"runtime_change_releases",
		"model_deployment_create_receipts",
		"model_call_authorizations",
		"async_media_charge_facts",
		"async_media_jobs",
		"enterprise_finance_adjustment_journal_versions",
		"enterprise_finance_allocation_results",
		"enterprise_finance_allocation_runs",
		"enterprise_finance_pricing_bindings",
		"enterprise_finance_pricing_snapshots",
		"enterprise_finance_rate_source_evidence",
		"enterprise_finance_rate_components",
		"enterprise_finance_rate_card_versions",
		"enterprise_finance_budget_versions",
		"enterprise_finance_cost_center_versions",
		"enterprise_finance_period_events",
		"enterprise_finance_fiscal_periods",
		"enterprise_directory_iam_actions",
		"enterprise_directory_account_snapshots",
		"enterprise_directory_person_snapshots",
		"enterprise_directory_unit_snapshots",
		"enterprise_directory_snapshots",
		"enterprise_directory_sources",
		"guardrail_policies",
		"distributed_limit_policies",
		"provider_cost_reconciliations",
		"billing_provider_attempts",
		"billing_observation_receipts",
		"internal_outbox_events",
		"usage_finalization_journal",
		"completed_execution_feed_items",
		"completed_execution_feed_states",
		"project_budget_usage",
		"runtime_terminal_hooks",
		"notification_deliveries",
		"notification_webhooks",
		"notification_events",
		"request_idempotency",
		"call_logs",
		"model_operation_production_approvals",
		"model_deployment_probes",
		"model_deployment_capabilities",
		"model_deployment_operations",
		"credential_binding_deployments",
		"provider_credential_bindings",
		"model_deployments",
		"provider_connections",
		"freeze_idempotency",
		"transactions",
		"audit_logs",
		"grants",
		"wallets",
		"project_model_sets",
		"model_set_items",
		"model_aliases",
		"model_sets",
		"model_registry",
		"project_members",
		"channel_model_probes",
		"channel_keys",
		"org_model_prices",
		"model_prices",
		"users",
		"projects",
		"orgs",
	}

	rows, err := pool.Query(ctx, `
		SELECT tablename
		FROM pg_catalog.pg_tables
		WHERE schemaname = 'public' AND tablename = ANY($1::text[])`, tables)
	if err != nil {
		t.Logf("truncate table discovery warning: %v", err)
		return
	}
	existing := make(map[string]struct{}, len(tables))
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			rows.Close()
			t.Logf("truncate table discovery scan warning: %v", err)
			return
		}
		existing[table] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Logf("truncate table discovery warning: %v", err)
		return
	}
	rows.Close()

	quotedTables := make([]string, 0, len(existing))
	for _, table := range tables {
		if _, ok := existing[table]; ok {
			quotedTables = append(quotedTables, quoteIdent(table))
		}
	}
	if len(quotedTables) == 0 {
		return
	}
	testResetTriggers := []struct {
		table string
		name  string
	}{
		{table: "model_successor_cutover_receipt_model_sets", name: "model_successor_cutover_receipt_model_sets_truncate_guard"},
		{table: "model_successor_cutover_receipts", name: "model_successor_cutover_receipts_truncate_guard"},
		{table: "runtime_change_events", name: "runtime_change_events_append_only"},
		{table: "model_operation_production_approvals", name: "model_operation_production_approvals_append_only"},
		{table: "model_operation_public_execution_bindings", name: "model_operation_public_execution_bindings_append_only"},
		{table: "model_capability_routing_approvals", name: "model_capability_routing_approvals_append_only"},
		{table: "model_capability_probes", name: "model_capability_probes_append_only"},
		{table: "model_deployment_probes", name: "model_deployment_probes_truncate_guard"},
		{table: "model_deployment_lifecycle_receipt_qualifications", name: "model_deployment_lifecycle_receipt_qualifications_truncate_guard"},
		{table: "model_deployment_lifecycle_receipt_releases", name: "model_deployment_lifecycle_receipt_releases_truncate_guard"},
		{table: "model_deployment_lifecycle_receipts", name: "model_deployment_lifecycle_receipts_truncate_guard"},
		{table: "model_deployment_release_authoring_receipts", name: "model_deployment_release_authoring_receipts_truncate_guard"},
	}
	disabled := make([]struct {
		table string
		name  string
	}, 0, len(testResetTriggers))
	for _, trigger := range testResetTriggers {
		if _, tableExists := existing[trigger.table]; !tableExists {
			continue
		}
		var triggerExists bool
		if err := pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM pg_catalog.pg_trigger trigger_row
  JOIN pg_catalog.pg_class table_row ON table_row.oid=trigger_row.tgrelid
  JOIN pg_catalog.pg_namespace namespace_row ON namespace_row.oid=table_row.relnamespace
 WHERE namespace_row.nspname='public' AND table_row.relname=$1
   AND trigger_row.tgname=$2 AND NOT trigger_row.tgisinternal
)`, trigger.table, trigger.name).Scan(&triggerExists); err != nil || !triggerExists {
			continue
		}
		statement := "ALTER TABLE " + quoteIdent(trigger.table) + " DISABLE TRIGGER " + quoteIdent(trigger.name)
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Logf("test truncate trigger disable warning for %s.%s: %v", trigger.table, trigger.name, err)
			continue
		}
		disabled = append(disabled, trigger)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE TABLE "+strings.Join(quotedTables, ", ")+" CASCADE"); err != nil {
		t.Logf("truncate warning: %v", err)
	}
	for index := len(disabled) - 1; index >= 0; index-- {
		trigger := disabled[index]
		statement := "ALTER TABLE " + quoteIdent(trigger.table) + " ENABLE TRIGGER " + quoteIdent(trigger.name)
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("restore test truncate trigger %s.%s: %v", trigger.table, trigger.name, err)
		}
	}
}

// NewTestOrg creates a unique test org and returns its ID. The caller's test-DB
// setup owns isolation and cleanup; repeating TruncateAll here would scan and
// clear the entire schema a second time for every fixture.
func NewTestOrg(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	ctx := context.Background()

	name := fmt.Sprintf("test-org-%s-%d", t.Name(), time.Now().UnixNano())
	var orgID int64
	insert := func() error {
		return pool.QueryRow(ctx,
			`INSERT INTO orgs (name, status, org_type) VALUES ($1, 'active', 'standard') RETURNING id`,
			name,
		).Scan(&orgID)
	}
	err := insert()
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" && postgresError.ConstraintName == "orgs_pkey" {
		// Some historical migration fixtures insert the platform organization
		// with an explicit ID without advancing the serial sequence. Repair only
		// that stale-sequence case instead of clearing every business table.
		_, syncErr := pool.Exec(ctx, `
			SELECT setval(
				pg_get_serial_sequence('orgs', 'id'),
				GREATEST(COALESCE((SELECT MAX(id) FROM orgs), 0) + 1, 1),
				FALSE
			)`)
		require.NoError(t, syncErr, "synchronize test organization sequence")
		err = insert()
	}
	require.NoError(t, err, "create test org")

	return orgID
}

// NewTestWalletCents creates a wallet from an explicitly cents-denominated test fixture.
func NewTestWalletCents(t *testing.T, pool *pgxpool.Pool, orgID int64, balanceCents int64) {
	t.Helper()
	ctx := context.Background()
	balanceMicros, err := money.CentsToMicros(balanceCents)
	require.NoError(t, err, "convert test wallet cents")

	_, err = pool.Exec(ctx,
		`INSERT INTO wallets (org_id, balance_micros, granted_balance_micros, frozen_micros, total_spent_micros)
		 VALUES ($1, $2, 0, 0, 0)`,
		orgID, balanceMicros,
	)
	require.NoError(t, err, "create test wallet")
}

// NewTestGrantCents creates a grant from an explicitly cents-denominated test fixture.
func NewTestGrantCents(t *testing.T, pool *pgxpool.Pool, orgID int64, amountCents int64, expiresAt *time.Time) {
	t.Helper()
	ctx := context.Background()
	amountMicros, err := money.CentsToMicros(amountCents)
	require.NoError(t, err, "convert test grant cents")

	_, err = pool.Exec(ctx,
		`INSERT INTO grants (org_id, amount_micros, balance_micros, status, expires_at)
		 VALUES ($1, $2, $2, 'active', $3)`,
		orgID, amountMicros, expiresAt,
	)
	require.NoError(t, err, "create test grant")
}

// NewTestModelPrice creates a model price entry.
func NewTestModelPrice(t *testing.T, pool *pgxpool.Pool, model string, inputPrice, outputPrice float64) {
	t.Helper()
	ctx := context.Background()

	_, err := pool.Exec(ctx,
		`INSERT INTO model_prices (model_name, input_price_cents, output_price_cents)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (model_name) DO UPDATE SET input_price_cents = $2, output_price_cents = $3`,
		model, inputPrice, outputPrice,
	)
	require.NoError(t, err, "create test model price")
}
