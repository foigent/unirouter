package modelcapabilitypublication

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testdb"
)

const fixturePublicationM101 = "000101_capability_contract_publication_fixture"

func TestKimiK27M101ActualArtifactsGuardedRoundTripIntegration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	migrationDir := testdb.MigrationsDir(t)
	artifactLedger, err := schemaauthority.ExpectedLedger(migrationDir)
	require.NoError(t, err)
	require.Contains(t, artifactLedger, "000101_kimi_k27_chat_core_qualification")
	require.NoError(t, VerifyArtifactPublicationInventory(migrationDir, artifactLedger))
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, BaselineMigrationM100))

	declaration, predecessor, err := CompiledPublication("000101_kimi_k27_chat_core_qualification")
	require.NoError(t, err)
	artifact := ArtifactIdentity{
		Kind: ArtifactKindSource, Commit: strings.Repeat("1", 40), Tree: strings.Repeat("2", 40),
		Version: "k27-m101-integration", PublisherExecutableDigest: "sha256:" + strings.Repeat("3", 64),
	}
	authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, predecessor, declaration)
	plan, err := PlanTarget(ctx, pool, "integration", authority)
	require.NoError(t, err)
	require.Equal(t, ValidationValid, plan.Validation)
	var appendCount, promotionCount int
	for _, item := range plan.BindingItems {
		if item.Action == BindingActionAppend {
			appendCount++
		}
	}
	for _, item := range plan.SelectorItems {
		if item.Action == SelectorActionPromotion {
			promotionCount++
		}
	}
	require.Equal(t, 6, appendCount)
	require.Equal(t, 6, promotionCount)
	require.Empty(t, plan.ProjectionItems, "M100 fixture has no exact K2.7 Deployment")
	hooks, err := HooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks))
	require.NoError(t, VerifyPlanFinal(ctx, pool, plan))

	downHooks, err := DownHooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks))
	var ledgerCount, bindingCount, selectorCount int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=$1`, declaration.PublicationID).Scan(&ledgerCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_probe_contract_bindings WHERE introduced_migration=$1`, declaration.PublicationID).Scan(&bindingCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_current_probe_contracts WHERE execution_profile_id='kimi.k2.7-code.api' AND execution_profile_version='1.3.0'`).Scan(&selectorCount))
	require.Zero(t, ledgerCount)
	require.Zero(t, bindingCount)
	require.Zero(t, selectorCount)

	require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks))
	require.NoError(t, VerifyPlanFinal(ctx, pool, plan))
	_, err = pool.Exec(ctx, `ALTER TABLE billing_provider_attempts RENAME TO billing_provider_attempts_k27_original`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `CREATE TABLE billing_provider_attempts (
route_capability_evidence JSONB NOT NULL, execution_snapshot_hash VARCHAR(69))`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO billing_provider_attempts (route_capability_evidence)
VALUES ('[{"capability_id":"streaming","evidence_ref":"capability_contract:unirouter.capability-probe.kimi-chat-sse.v1@4.0.0"}]'::jsonb)`)
	require.NoError(t, err)
	downHooks, err = DownHooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	err = migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks)
	require.ErrorContains(t, err, "retained provider-attempt capability evidence")
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=$1`, declaration.PublicationID).Scan(&ledgerCount))
	require.Equal(t, int64(1), ledgerCount)
	require.NoError(t, VerifyPlanFinal(ctx, pool, plan))
}

func TestKimiK3M102ActualArtifactsGuardedRoundTripIntegration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	migrationDir := testdb.MigrationsDir(t)
	artifactLedger, err := schemaauthority.ExpectedLedger(migrationDir)
	require.NoError(t, err)
	require.Contains(t, artifactLedger, "000102_kimi_k3_chat_core_qualification")
	require.NoError(t, VerifyArtifactPublicationInventory(migrationDir, artifactLedger))
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, BaselineMigrationM100))

	artifact := ArtifactIdentity{
		Kind: ArtifactKindSource, Commit: strings.Repeat("4", 40), Tree: strings.Repeat("5", 40),
		Version: "k3-m102-integration", PublisherExecutableDigest: "sha256:" + strings.Repeat("6", 64),
	}
	for _, publicationID := range []string{
		"000101_kimi_k27_chat_core_qualification",
		"000102_kimi_k3_chat_core_qualification",
	} {
		declaration, predecessor, err := CompiledPublication(publicationID)
		require.NoError(t, err)
		authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, predecessor, declaration)
		plan, err := PlanTarget(ctx, pool, "integration", authority)
		require.NoError(t, err)
		require.Equal(t, ValidationValid, plan.Validation)
		require.Empty(t, plan.ProjectionItems, "baseline has no exact deployment for %s", publicationID)
		if publicationID == "000102_kimi_k3_chat_core_qualification" {
			var appendCount, promotionCount int
			for _, item := range plan.BindingItems {
				if item.Action == BindingActionAppend {
					appendCount++
				}
			}
			for _, item := range plan.SelectorItems {
				if item.Action == SelectorActionPromotion {
					promotionCount++
				}
			}
			require.Equal(t, 8, appendCount)
			require.Equal(t, 8, promotionCount)
		}
		hooks, err := HooksForPlan("integration", authority, plan)
		require.NoError(t, err)
		require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks))
		require.NoError(t, VerifyPlanFinal(ctx, pool, plan))

		if publicationID != "000102_kimi_k3_chat_core_qualification" {
			continue
		}
		downHooks, err := DownHooksForPlan("integration", authority, plan)
		require.NoError(t, err)
		var orgID, retainedModelID int64
		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type)
VALUES ('k3-m102-retained-profile','active','customer') RETURNING id`).Scan(&orgID))
		require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_registry (
org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,context_length,output_limit,
execution_profile_id,execution_profile_version)
VALUES ($1,'k3-m102-retained-profile','K3 M102 retained Profile','kimi','staging',TRUE,262144,32768,
'kimi.k3.api','1.9.0') RETURNING id`, orgID).Scan(&retainedModelID))

		err = migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks)
		require.ErrorContains(t, err, "retained Profile attachment prevents publication down")
		assertKimiK3PublicationPresent(t, ctx, pool)

		// The generated artifact repeats the guard under the same transaction
		// that holds model_registry, so a stale or bypassed pre-check cannot
		// orphan the append-only Profile identity.
		artifactGuardHooks := downHooks
		artifactGuardHooks.Before = func(context.Context, *pgxpool.Conn) error { return nil }
		err = migrate.RunPublicationDown(ctx, pool, migrationDir, artifactGuardHooks)
		require.ErrorContains(t, err, "retained Profile attachment prevents guarded publication downgrade")
		assertKimiK3PublicationPresent(t, ctx, pool)

		_, err = pool.Exec(ctx, `DELETE FROM model_registry WHERE id=$1`, retainedModelID)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `DELETE FROM orgs WHERE id=$1`, orgID)
		require.NoError(t, err)
		require.NoError(t, migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks))
		var ledgerCount, bindingCount, selectorCount int64
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=$1`, publicationID).Scan(&ledgerCount))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_probe_contract_bindings WHERE introduced_migration=$1`, publicationID).Scan(&bindingCount))
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_current_probe_contracts WHERE execution_profile_id='kimi.k3.api' AND execution_profile_version='1.9.0'`).Scan(&selectorCount))
		require.Zero(t, ledgerCount)
		require.Zero(t, bindingCount)
		require.Zero(t, selectorCount)

		require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks))
		require.NoError(t, VerifyPlanFinal(ctx, pool, plan))
	}
}

func assertKimiK3PublicationPresent(t *testing.T, ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) {
	t.Helper()
	var ledgerCount, bindingCount, selectorCount int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations
WHERE version='000102_kimi_k3_chat_core_qualification'`).Scan(&ledgerCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_probe_contract_bindings
WHERE introduced_migration='000102_kimi_k3_chat_core_qualification'`).Scan(&bindingCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_current_probe_contracts
WHERE execution_profile_id='kimi.k3.api' AND execution_profile_version='1.9.0'`).Scan(&selectorCount))
	require.Equal(t, int64(1), ledgerCount)
	require.Equal(t, int64(8), bindingCount)
	require.Equal(t, int64(8), selectorCount)
}

func TestMiniMaxM3GLM53M103ActualArtifactsGuardedRoundTripIntegration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	migrationDir := testdb.MigrationsDir(t)
	artifactLedger, err := schemaauthority.ExpectedLedger(migrationDir)
	require.NoError(t, err)
	require.Contains(t, artifactLedger, "000103_minimax_m3_glm53_function_tools_qualification")
	require.NoError(t, VerifyArtifactPublicationInventory(migrationDir, artifactLedger))
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, BaselineMigrationM100))

	artifact := ArtifactIdentity{
		Kind: ArtifactKindSource, Commit: strings.Repeat("7", 40), Tree: strings.Repeat("8", 40),
		Version: "minimax-glm-m103-integration", PublisherExecutableDigest: "sha256:" + strings.Repeat("9", 64),
	}
	var m103Plan Plan
	var m103Authority PlanAuthority
	for _, publicationID := range []string{
		"000101_kimi_k27_chat_core_qualification",
		"000102_kimi_k3_chat_core_qualification",
		"000103_minimax_m3_glm53_function_tools_qualification",
	} {
		declaration, predecessor, err := CompiledPublication(publicationID)
		require.NoError(t, err)
		authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, predecessor, declaration)
		plan, err := PlanTarget(ctx, pool, "integration", authority)
		require.NoError(t, err)
		require.Equal(t, ValidationValid, plan.Validation)
		require.Empty(t, plan.ProjectionItems, "baseline has no exact deployment for %s", publicationID)
		if publicationID == "000103_minimax_m3_glm53_function_tools_qualification" {
			var appendCount, promotionCount int
			for _, item := range plan.BindingItems {
				if item.Action == BindingActionAppend {
					appendCount++
				}
			}
			for _, item := range plan.SelectorItems {
				if item.Action == SelectorActionPromotion {
					promotionCount++
				}
			}
			require.Equal(t, 2, appendCount)
			require.Equal(t, 2, promotionCount)
			m103Plan = plan
			m103Authority = authority
		}
		hooks, err := HooksForPlan("integration", authority, plan)
		require.NoError(t, err)
		require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks))
		require.NoError(t, VerifyPlanFinal(ctx, pool, plan))
	}

	var orgID, retainedModelID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type)
VALUES ('glm-m103-retained-profile','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_registry (
org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,context_length,output_limit,
execution_profile_id,execution_profile_version)
VALUES ($1,'glm-m103-retained-profile','GLM M103 retained Profile','glm','staging',TRUE,202752,131072,
'glm.5.3.api','1.8.0') RETURNING id`, orgID).Scan(&retainedModelID))

	downHooks, err := DownHooksForPlan("integration", m103Authority, m103Plan)
	require.NoError(t, err)
	err = migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks)
	require.ErrorContains(t, err, "retained Profile attachment prevents publication down")
	assertMiniMaxGLMPublicationPresent(t, ctx, pool)

	_, err = pool.Exec(ctx, `DELETE FROM model_registry WHERE id=$1`, retainedModelID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM orgs WHERE id=$1`, orgID)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks))
	var ledgerCount, bindingCount, selectorCount int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=$1`, m103Plan.PublicationID).Scan(&ledgerCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_probe_contract_bindings WHERE introduced_migration=$1`, m103Plan.PublicationID).Scan(&bindingCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_current_probe_contracts
WHERE (execution_profile_id='minimax.m3.token-plan' AND execution_profile_version='1.7.0')
   OR (execution_profile_id='glm.5.3.api' AND execution_profile_version='1.8.0')`).Scan(&selectorCount))
	require.Zero(t, ledgerCount)
	require.Zero(t, bindingCount)
	require.Zero(t, selectorCount)

	hooks, err := HooksForPlan("integration", m103Authority, m103Plan)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, m103Plan.ApplyMigrations, hooks))
	require.NoError(t, VerifyPlanFinal(ctx, pool, m103Plan))
}

func assertMiniMaxGLMPublicationPresent(t *testing.T, ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) {
	t.Helper()
	var ledgerCount, bindingCount, selectorCount int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations
WHERE version='000103_minimax_m3_glm53_function_tools_qualification'`).Scan(&ledgerCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_probe_contract_bindings
WHERE introduced_migration='000103_minimax_m3_glm53_function_tools_qualification'`).Scan(&bindingCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_current_probe_contracts
WHERE (execution_profile_id='minimax.m3.token-plan' AND execution_profile_version='1.7.0')
   OR (execution_profile_id='glm.5.3.api' AND execution_profile_version='1.8.0')`).Scan(&selectorCount))
	require.Equal(t, int64(1), ledgerCount)
	require.Equal(t, int64(2), bindingCount)
	require.Equal(t, int64(2), selectorCount)
}

func TestMiniMaxPAYGM104ActualArtifactsGuardedRoundTripPreservesM103Integration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	migrationDir := testdb.MigrationsDir(t)
	artifactLedger, err := schemaauthority.ExpectedLedger(migrationDir)
	require.NoError(t, err)
	require.Contains(t, artifactLedger, "000104_minimax_payg_function_tools_qualification")
	require.NoError(t, VerifyArtifactPublicationInventory(migrationDir, artifactLedger))
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, BaselineMigrationM100))

	artifact := ArtifactIdentity{
		Kind: ArtifactKindSource, Commit: strings.Repeat("a", 40), Tree: strings.Repeat("b", 40),
		Version: "minimax-payg-m104-integration", PublisherExecutableDigest: "sha256:" + strings.Repeat("c", 64),
	}
	var m104Plan Plan
	var m104Authority PlanAuthority
	for _, publicationID := range []string{
		"000101_kimi_k27_chat_core_qualification",
		"000102_kimi_k3_chat_core_qualification",
		"000103_minimax_m3_glm53_function_tools_qualification",
		"000104_minimax_payg_function_tools_qualification",
	} {
		declaration, predecessor, err := CompiledPublication(publicationID)
		require.NoError(t, err)
		authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, predecessor, declaration)
		plan, err := PlanTarget(ctx, pool, "integration", authority)
		require.NoError(t, err)
		require.Equal(t, ValidationValid, plan.Validation)
		require.Empty(t, plan.ProjectionItems)
		if publicationID == "000104_minimax_payg_function_tools_qualification" {
			var appendCount, promotionCount int
			for _, item := range plan.BindingItems {
				if item.Action == BindingActionAppend {
					appendCount++
				}
			}
			for _, item := range plan.SelectorItems {
				if item.Action == SelectorActionPromotion {
					promotionCount++
				}
			}
			require.Equal(t, 3, appendCount)
			require.Equal(t, 3, promotionCount)
			m104Plan = plan
			m104Authority = authority
		}
		hooks, err := HooksForPlan("integration", authority, plan)
		require.NoError(t, err)
		require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks))
		require.NoError(t, VerifyPlanFinal(ctx, pool, plan))
	}

	var orgID, retainedModelID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type)
VALUES ('minimax-m104-retained-profile','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_registry (
org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,context_length,output_limit,
execution_profile_id,execution_profile_version)
VALUES ($1,'minimax-m104-retained-profile','MiniMax M104 retained Profile','minimax','staging',TRUE,204800,65536,
'minimax.m2.7.payg','1.0.0') RETURNING id`, orgID).Scan(&retainedModelID))

	downHooks, err := DownHooksForPlan("integration", m104Authority, m104Plan)
	require.NoError(t, err)
	err = migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks)
	require.ErrorContains(t, err, "retained Profile attachment prevents publication down")
	assertMiniMaxGLMPublicationPresent(t, ctx, pool)

	_, err = pool.Exec(ctx, `DELETE FROM model_registry WHERE id=$1`, retainedModelID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM orgs WHERE id=$1`, orgID)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks))

	var ledgerCount, bindingCount, selectorCount int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=$1`, m104Plan.PublicationID).Scan(&ledgerCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_probe_contract_bindings WHERE introduced_migration=$1`, m104Plan.PublicationID).Scan(&bindingCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_current_probe_contracts
WHERE execution_profile_id IN ('minimax.m3.payg','minimax.m2.7.payg','minimax.m2.7-highspeed.payg')`).Scan(&selectorCount))
	require.Zero(t, ledgerCount)
	require.Zero(t, bindingCount)
	require.Zero(t, selectorCount)
	assertMiniMaxGLMPublicationPresent(t, ctx, pool)

	hooks, err := HooksForPlan("integration", m104Authority, m104Plan)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, m104Plan.ApplyMigrations, hooks))
	require.NoError(t, VerifyPlanFinal(ctx, pool, m104Plan))
}

func TestGLMToggleM105ActualArtifactsGuardedRoundTripPreservesM104Integration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	migrationDir := testdb.MigrationsDir(t)
	artifactLedger, err := schemaauthority.ExpectedLedger(migrationDir)
	require.NoError(t, err)
	require.Equal(t, schemaauthority.LatestSupportedMigration, artifactLedger[len(artifactLedger)-1])
	require.NoError(t, VerifyArtifactPublicationInventory(migrationDir, artifactLedger))
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, BaselineMigrationM100))

	artifact := ArtifactIdentity{
		Kind: ArtifactKindSource, Commit: strings.Repeat("d", 40), Tree: strings.Repeat("e", 40),
		Version: "glm-toggle-m105-integration", PublisherExecutableDigest: "sha256:" + strings.Repeat("f", 64),
	}
	var m105Plan Plan
	var m105Authority PlanAuthority
	for _, publicationID := range []string{
		"000101_kimi_k27_chat_core_qualification",
		"000102_kimi_k3_chat_core_qualification",
		"000103_minimax_m3_glm53_function_tools_qualification",
		"000104_minimax_payg_function_tools_qualification",
		"000105_glm_toggle_function_tools_qualification",
	} {
		declaration, predecessor, err := CompiledPublication(publicationID)
		require.NoError(t, err)
		authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, predecessor, declaration)
		plan, err := PlanTarget(ctx, pool, "integration", authority)
		require.NoError(t, err)
		require.Equal(t, ValidationValid, plan.Validation)
		require.Empty(t, plan.ProjectionItems)
		if publicationID == "000105_glm_toggle_function_tools_qualification" {
			var appendCount, promotionCount int
			for _, item := range plan.BindingItems {
				if item.Action == BindingActionAppend {
					appendCount++
				}
			}
			for _, item := range plan.SelectorItems {
				if item.Action == SelectorActionPromotion {
					promotionCount++
				}
			}
			require.Equal(t, 5, appendCount)
			require.Equal(t, 5, promotionCount)
			m105Plan = plan
			m105Authority = authority
		}
		hooks, err := HooksForPlan("integration", authority, plan)
		require.NoError(t, err)
		require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks))
		require.NoError(t, VerifyPlanFinal(ctx, pool, plan))
	}

	var orgID, retainedModelID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type)
VALUES ('glm-m105-retained-profile','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_registry (
org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,context_length,output_limit,
execution_profile_id,execution_profile_version)
VALUES ($1,'glm-m105-retained-profile','GLM M105 retained Profile','glm','staging',TRUE,200000,65536,
'glm.5.1.api','1.0.0') RETURNING id`, orgID).Scan(&retainedModelID))

	downHooks, err := DownHooksForPlan("integration", m105Authority, m105Plan)
	require.NoError(t, err)
	err = migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks)
	require.ErrorContains(t, err, "retained Profile attachment prevents publication down")
	assertMiniMaxGLMPublicationPresent(t, ctx, pool)

	_, err = pool.Exec(ctx, `DELETE FROM model_registry WHERE id=$1`, retainedModelID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM orgs WHERE id=$1`, orgID)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks))

	var ledgerCount, bindingCount, selectorCount int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=$1`, m105Plan.PublicationID).Scan(&ledgerCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_probe_contract_bindings WHERE introduced_migration=$1`, m105Plan.PublicationID).Scan(&bindingCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_current_probe_contracts
WHERE execution_profile_id IN ('glm.5.2.api','glm.5.1.api','glm.5-turbo.api','glm.5.api','glm.5v-turbo.api')
  AND probe_contract_id='unirouter.capability-probe.glm-toggle-chat-function-tools.v1'`).Scan(&selectorCount))
	require.Zero(t, ledgerCount)
	require.Zero(t, bindingCount)
	require.Zero(t, selectorCount)
	assertMiniMaxGLMPublicationPresent(t, ctx, pool)

	hooks, err := HooksForPlan("integration", m105Authority, m105Plan)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, m105Plan.ApplyMigrations, hooks))
	require.NoError(t, VerifyPlanFinal(ctx, pool, m105Plan))

	_, err = pool.Exec(ctx, `ALTER TABLE billing_provider_attempts RENAME TO billing_provider_attempts_m105_original`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `CREATE TABLE billing_provider_attempts (
route_capability_evidence JSONB NOT NULL, execution_snapshot_hash VARCHAR(69))`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO billing_provider_attempts (route_capability_evidence)
VALUES ('[{"capability_id":"function_tools","evidence_ref":"capability_contract:unirouter.capability-probe.glm-toggle-chat-function-tools.v1@1.0.0"}]'::jsonb)`)
	require.NoError(t, err)
	downHooks, err = DownHooksForPlan("integration", m105Authority, m105Plan)
	require.NoError(t, err)
	err = migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks)
	require.ErrorContains(t, err, "retained provider-attempt capability evidence")
	require.NoError(t, VerifyPlanFinal(ctx, pool, m105Plan))
}

func TestMiniMaxOfficialCNPAYGM106ActualArtifactsGuardedRoundTripPreservesM105Integration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	migrationDir := testdb.MigrationsDir(t)
	artifactLedger, err := schemaauthority.ExpectedLedger(migrationDir)
	require.NoError(t, err)
	require.Equal(t, schemaauthority.LatestSupportedMigration, artifactLedger[len(artifactLedger)-1])
	require.NoError(t, VerifyArtifactPublicationInventory(migrationDir, artifactLedger))
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, BaselineMigrationM100))

	artifact := ArtifactIdentity{
		Kind: ArtifactKindSource, Commit: strings.Repeat("1", 40), Tree: strings.Repeat("2", 40),
		Version: "minimax-official-cn-payg-m106-integration", PublisherExecutableDigest: "sha256:" + strings.Repeat("3", 64),
	}
	var m106Plan Plan
	var m106Authority PlanAuthority
	for _, publicationID := range []string{
		"000101_kimi_k27_chat_core_qualification",
		"000102_kimi_k3_chat_core_qualification",
		"000103_minimax_m3_glm53_function_tools_qualification",
		"000104_minimax_payg_function_tools_qualification",
		"000105_glm_toggle_function_tools_qualification",
		"000106_minimax_official_cn_payg_endpoint_qualification",
	} {
		declaration, predecessor, err := CompiledPublication(publicationID)
		require.NoError(t, err)
		authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, predecessor, declaration)
		plan, err := PlanTarget(ctx, pool, "integration", authority)
		require.NoError(t, err)
		require.Equal(t, ValidationValid, plan.Validation)
		require.Empty(t, plan.ProjectionItems)
		if publicationID == "000106_minimax_official_cn_payg_endpoint_qualification" {
			var appendCount, promotionCount int
			for _, item := range plan.BindingItems {
				if item.Action == BindingActionAppend {
					appendCount++
				}
			}
			for _, item := range plan.SelectorItems {
				if item.Action == SelectorActionPromotion {
					promotionCount++
				}
			}
			require.Equal(t, 3, appendCount)
			require.Equal(t, 3, promotionCount)
			m106Plan = plan
			m106Authority = authority
		}
		hooks, err := HooksForPlan("integration", authority, plan)
		require.NoError(t, err)
		require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks))
		require.NoError(t, VerifyPlanFinal(ctx, pool, plan))
	}

	var orgID, retainedModelID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type)
VALUES ('minimax-m106-retained-profile','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_registry (
org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,context_length,output_limit,
execution_profile_id,execution_profile_version)
VALUES ($1,'minimax-m106-retained-profile','MiniMax M106 retained Profile','minimax','staging',TRUE,204800,65536,
'minimax.m2.7.payg','1.1.0') RETURNING id`, orgID).Scan(&retainedModelID))

	downHooks, err := DownHooksForPlan("integration", m106Authority, m106Plan)
	require.NoError(t, err)
	err = migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks)
	require.ErrorContains(t, err, "retained Profile attachment prevents publication down")
	require.NoError(t, VerifyPlanFinal(ctx, pool, m106Plan))

	_, err = pool.Exec(ctx, `DELETE FROM model_registry WHERE id=$1`, retainedModelID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM orgs WHERE id=$1`, orgID)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks))

	var ledgerCount, bindingCount, selectorCount int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=$1`, m106Plan.PublicationID).Scan(&ledgerCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_probe_contract_bindings WHERE introduced_migration=$1`, m106Plan.PublicationID).Scan(&bindingCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_current_probe_contracts
WHERE execution_profile_id IN ('minimax.m3.payg','minimax.m2.7.payg','minimax.m2.7-highspeed.payg')
  AND execution_profile_version='1.1.0'`).Scan(&selectorCount))
	require.Zero(t, ledgerCount)
	require.Zero(t, bindingCount)
	require.Zero(t, selectorCount)
	var m105Count int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations
WHERE version='000105_glm_toggle_function_tools_qualification'`).Scan(&m105Count))
	require.EqualValues(t, 1, m105Count)

	hooks, err := HooksForPlan("integration", m106Authority, m106Plan)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, m106Plan.ApplyMigrations, hooks))
	require.NoError(t, VerifyPlanFinal(ctx, pool, m106Plan))

	_, err = pool.Exec(ctx, `ALTER TABLE billing_provider_attempts RENAME TO billing_provider_attempts_m106_original`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `CREATE TABLE billing_provider_attempts (
route_capability_evidence JSONB NOT NULL, execution_snapshot_hash VARCHAR(69))`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO billing_provider_attempts (route_capability_evidence)
VALUES ('[{"capability_id":"function_tools","evidence_ref":"capability_contract:unirouter.capability-probe.minimax-payg-m2-chat-function-tools.v1@1.0.0"}]'::jsonb)`)
	require.NoError(t, err)
	downHooks, err = DownHooksForPlan("integration", m106Authority, m106Plan)
	require.NoError(t, err)
	err = migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks)
	require.ErrorContains(t, err, "retained provider-attempt capability evidence")
	require.NoError(t, VerifyPlanFinal(ctx, pool, m106Plan))
}

func TestStrictPublicationAppliesExactPlanAndRecoveryIsReadOnlyIntegration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	migrationDir, artifactLedger := fixturePublicationArtifact(t, testdb.MigrationsDir(t))
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, BaselineMigrationM100))
	deploymentID := seedPublicationDeployment(t, ctx, pool)

	baseline, err := CompiledBaseline()
	require.NoError(t, err)
	declaration := fixtureDeclaration()
	artifact := ArtifactIdentity{
		Kind: ArtifactKindSource, Commit: strings.Repeat("1", 40), Tree: strings.Repeat("2", 40), Version: "fixture",
		PublisherExecutableDigest: "sha256:" + strings.Repeat("3", 64),
	}
	authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, baseline, declaration)
	plan, err := PlanTarget(ctx, pool, "integration", authority)
	require.NoError(t, err)
	require.Equal(t, ValidationValid, plan.Validation)
	require.Len(t, plan.ProjectionItems, 1)
	require.Equal(t, ProjectionActionInsert, plan.ProjectionItems[0].Action)
	require.Equal(t, deploymentID, plan.ProjectionItems[0].Final.DeploymentID)
	assertPublicationAbsent(t, ctx, pool)
	tamperedAuthority := authority
	tamperedAuthority.Declaration.BindingAppends = append([]Binding(nil), authority.Declaration.BindingAppends...)
	tamperedAuthority.Declaration.BindingAppends[0].PublicPath = "/tampered"
	require.ErrorContains(t, ValidatePlanAuthority(tamperedAuthority, plan), "compiled declaration")

	// Ordinary migration execution must fail before applying a declaration-owned
	// migration or writing its ledger row.
	err = migrate.Run(ctx, pool, migrationDir)
	require.ErrorContains(t, err, "requires explicit publication or install context")
	assertPublicationAbsent(t, ctx, pool)

	hooks, err := HooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks))

	var ledgerCount, bindingCount, selectorCount, projectionCount int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=$1`, fixturePublicationM101).Scan(&ledgerCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_probe_contract_bindings WHERE introduced_migration=$1`, fixturePublicationM101).Scan(&bindingCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_current_probe_contracts WHERE capability_id='fixture.publication.v1'`).Scan(&selectorCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_deployment_capabilities WHERE deployment_id=$1 AND capability_id='fixture.publication.v1' AND evidence_ref='capability_contract:unirouter.capability-probe.fixture.v1@1.0.0'`, deploymentID).Scan(&projectionCount))
	require.Equal(t, int64(1), ledgerCount)
	require.Equal(t, int64(1), bindingCount)
	require.Equal(t, int64(1), selectorCount)
	require.Equal(t, int64(1), projectionCount)

	var guardCount int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM pg_trigger WHERE tgname='model_deployment_capabilities_receipt_projection_guard' AND NOT tgisinternal`).Scan(&guardCount))
	require.Equal(t, int64(1), guardCount)

	result, err := PostCommitRecoveryCheck(ctx, pool, "integration", &plan, artifactLedger, &authority)
	require.NoError(t, err)
	require.Equal(t, RecoveryCheckResult{
		LedgerState:        LedgerStateExactPublicationPresent,
		LedgerRelation:     LedgerRelationExactPlanPostState,
		StaticVerification: StaticVerificationForPlan,
		DynamicReadback:    DynamicReadbackUnreconstructable,
	}, result)
	wrongTarget := plan
	wrongTarget.Target.DatabaseName = "other-database"
	wrongTarget, err = FinalizePlan(wrongTarget)
	require.NoError(t, err)
	_, err = PostCommitRecoveryCheck(ctx, pool, "integration", &wrongTarget, artifactLedger, &authority)
	require.ErrorContains(t, err, "does not target this database")
	_, err = HooksForPlan("other-environment", authority, plan)
	require.ErrorContains(t, err, "does not match trusted runtime environment")

	const laterOrdinaryMigration = "000102_fixture_ordinary_migration"
	_, err = pool.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, laterOrdinaryMigration)
	require.NoError(t, err)
	advancedLedger := append(append([]string(nil), artifactLedger...), laterOrdinaryMigration)
	advanced, err := PostCommitRecoveryCheck(ctx, pool, "integration", &plan, advancedLedger, &authority)
	require.NoError(t, err)
	require.Equal(t, LedgerRelationAdvancedCompatible, advanced.LedgerRelation)
	require.Equal(t, StaticVerificationCurrent, advanced.StaticVerification)
	require.Equal(t, DynamicReadbackUnreconstructable, advanced.DynamicReadback)
	_, err = pool.Exec(ctx, `DELETE FROM schema_migrations WHERE version=$1`, laterOrdinaryMigration)
	require.NoError(t, err)

	downHooks, err := DownHooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks))
	assertPublicationAbsent(t, ctx, pool)
	missing, missingErr := PostCommitRecoveryCheck(ctx, pool, "integration", &plan, artifactLedger, &authority)
	require.Error(t, missingErr)
	require.Equal(t, LedgerStateMissing, missing.LedgerState)
	require.Equal(t, LedgerRelationUnexpected, missing.LedgerRelation)
	require.Equal(t, DynamicReadbackUnreconstructable, missing.DynamicReadback)
	withoutPlan, err := PostCommitRecoveryCheck(ctx, pool, "integration", nil, artifactLedger, nil)
	require.NoError(t, err)
	require.Equal(t, RecoveryCheckResult{
		LedgerState:        LedgerStateUnexpected,
		LedgerRelation:     LedgerRelationUnexpected,
		StaticVerification: StaticVerificationPlanUnavailable,
		DynamicReadback:    DynamicReadbackUnreconstructable,
	}, withoutPlan)
	var remainingProjection int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_deployment_capabilities
WHERE deployment_id=$1 AND capability_id='fixture.publication.v1'`, deploymentID).Scan(&remainingProjection))
	require.Zero(t, remainingProjection)
}

func TestStrictPublicationRejectsPostPlanProjectionDriftAtomicallyIntegration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	migrationDir, artifactLedger := fixturePublicationArtifact(t, testdb.MigrationsDir(t))
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, BaselineMigrationM100))
	deploymentID := seedPublicationDeployment(t, ctx, pool)
	baseline, err := CompiledBaseline()
	require.NoError(t, err)
	declaration := fixtureDeclaration()
	artifact := ArtifactIdentity{Kind: ArtifactKindSource, Commit: strings.Repeat("1", 40), Tree: strings.Repeat("2", 40), Version: "fixture",
		PublisherExecutableDigest: "sha256:" + strings.Repeat("3", 64)}
	authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, baseline, declaration)
	plan, err := PlanTarget(ctx, pool, "integration", authority)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO model_deployment_capabilities (
deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,revision,support_revision)
VALUES ($1,'openai.chat_completions.v1','fixture.publication.v1','enabled','declared','fixture:drift',1,1)`, deploymentID)
	require.NoError(t, err)
	hooks, err := HooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	err = migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks)
	require.Error(t, err)
	assertPublicationAbsent(t, ctx, pool)
}

func TestStrictPublicationRejectsIncompleteUpHooksBeforeWriteIntegration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	migrationDir, artifactLedger := fixturePublicationArtifact(t, testdb.MigrationsDir(t))
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, BaselineMigrationM100))
	seedPublicationDeployment(t, ctx, pool)
	baseline, err := CompiledBaseline()
	require.NoError(t, err)
	declaration := fixtureDeclaration()
	artifact := ArtifactIdentity{Kind: ArtifactKindSource, Commit: strings.Repeat("1", 40), Tree: strings.Repeat("2", 40), Version: "fixture",
		PublisherExecutableDigest: "sha256:" + strings.Repeat("3", 64)}
	authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, baseline, declaration)
	plan, err := PlanTarget(ctx, pool, "integration", authority)
	require.NoError(t, err)
	hooks, err := HooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	hooks.ApplyProjection = nil
	err = migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks)
	require.ErrorContains(t, err, "requires complete Plan validation")
	assertPublicationAbsent(t, ctx, pool)
}

func TestStrictPublicationRejectsIncompleteDownHooksBeforeWriteIntegration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	migrationDir, artifactLedger := fixturePublicationArtifact(t, testdb.MigrationsDir(t))
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, BaselineMigrationM100))
	seedPublicationDeployment(t, ctx, pool)
	baseline, err := CompiledBaseline()
	require.NoError(t, err)
	declaration := fixtureDeclaration()
	artifact := ArtifactIdentity{Kind: ArtifactKindSource, Commit: strings.Repeat("1", 40), Tree: strings.Repeat("2", 40), Version: "fixture",
		PublisherExecutableDigest: "sha256:" + strings.Repeat("3", 64)}
	authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, baseline, declaration)
	plan, err := PlanTarget(ctx, pool, "integration", authority)
	require.NoError(t, err)
	hooks, err := HooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks))
	downHooks, err := DownHooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	downHooks.RevertProjection = nil
	err = migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks)
	require.ErrorContains(t, err, "requires complete Plan validation")
	var ledgerCount int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=$1`, fixturePublicationM101).Scan(&ledgerCount))
	require.Equal(t, int64(1), ledgerCount)
	require.NoError(t, VerifyPlanFinal(ctx, pool, plan))
}

func TestStrictPublicationUpdatesOnlyDeclaredProjectionLifecycleIntegration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	migrationDir, artifactLedger := fixturePublicationArtifact(t, testdb.MigrationsDir(t))
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, BaselineMigrationM100))
	declaredDeploymentID := seedPublicationDeployment(t, ctx, pool)
	var liveConnectionID, liveDeploymentID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO provider_connections (
name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
SELECT name || '-live',provider,base_url,adapter_type,adapter_config,admin_state,config_revision
  FROM provider_connections
 WHERE id=(SELECT connection_id FROM model_deployments WHERE id=$1)
RETURNING id`, declaredDeploymentID).Scan(&liveConnectionID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_deployments (
customer_org_id,public_model_id,connection_id,upstream_model,admin_state,config_revision,max_context,default_max_output)
SELECT customer_org_id,public_model_id,$2,upstream_model,admin_state,config_revision,max_context,default_max_output
  FROM model_deployments WHERE id=$1 RETURNING id`, declaredDeploymentID, liveConnectionID).Scan(&liveDeploymentID))
	_, err := pool.Exec(ctx, `INSERT INTO model_deployment_operations (
deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.chat_completions.v1','enabled','declared','',1,'protocol.deepseek-openai-chat.v4','1.0.0')`, liveDeploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `ALTER TABLE model_deployment_capabilities DISABLE TRIGGER USER`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO model_deployment_capabilities (
deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,revision,support_revision)
VALUES
($1,'openai.chat_completions.v1','fixture.publication.v1','enabled','declared','',1,1),
($2,'openai.chat_completions.v1','fixture.publication.v1','enabled','production_approved','capability_approval:999',4,1)`,
		declaredDeploymentID, liveDeploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `ALTER TABLE model_deployment_capabilities ENABLE TRIGGER USER`)
	require.NoError(t, err)

	baseline, err := CompiledBaseline()
	require.NoError(t, err)
	declaration := fixtureDeclaration()
	artifact := ArtifactIdentity{Kind: ArtifactKindSource, Commit: strings.Repeat("1", 40), Tree: strings.Repeat("2", 40), Version: "fixture",
		PublisherExecutableDigest: "sha256:" + strings.Repeat("3", 64)}
	authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, baseline, declaration)
	plan, err := PlanTarget(ctx, pool, "integration", authority)
	require.NoError(t, err)
	require.Equal(t, ValidationValid, plan.Validation)
	require.Len(t, plan.ProjectionItems, 1)
	require.Equal(t, declaredDeploymentID, plan.ProjectionItems[0].Final.DeploymentID)
	hooks, err := HooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks))

	var declaredState, declaredRef, liveState, liveRef string
	var declaredRevision, liveRevision int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT evidence_state,evidence_ref,revision
FROM model_deployment_capabilities WHERE deployment_id=$1 AND capability_id='fixture.publication.v1'`,
		declaredDeploymentID).Scan(&declaredState, &declaredRef, &declaredRevision))
	require.NoError(t, pool.QueryRow(ctx, `SELECT evidence_state,evidence_ref,revision
FROM model_deployment_capabilities WHERE deployment_id=$1 AND capability_id='fixture.publication.v1'`,
		liveDeploymentID).Scan(&liveState, &liveRef, &liveRevision))
	require.Equal(t, "declared", declaredState)
	require.Equal(t, "capability_contract:unirouter.capability-probe.fixture.v1@1.0.0", declaredRef)
	require.Equal(t, int64(2), declaredRevision)
	require.Equal(t, "production_approved", liveState)
	require.Equal(t, "capability_approval:999", liveRef)
	require.Equal(t, int64(4), liveRevision)
}

func TestStrictPublicationRequiresVerifierBeforeAnyWriteIntegration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	migrationDir, artifactLedger := fixturePublicationArtifact(t, testdb.MigrationsDir(t))
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, BaselineMigrationM100))
	seedPublicationDeployment(t, ctx, pool)
	baseline, err := CompiledBaseline()
	require.NoError(t, err)
	declaration := fixtureDeclaration()
	artifact := ArtifactIdentity{Kind: ArtifactKindSource, Commit: strings.Repeat("1", 40), Tree: strings.Repeat("2", 40), Version: "fixture",
		PublisherExecutableDigest: "sha256:" + strings.Repeat("3", 64)}
	authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, baseline, declaration)
	plan, err := PlanTarget(ctx, pool, "integration", authority)
	require.NoError(t, err)
	hooks, err := HooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(migrationDir, fixturePublicationM101+".verify.sql")))

	err = migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks)
	require.ErrorContains(t, err, "preflight committed publication verifier")
	assertPublicationAbsent(t, ctx, pool)
}

func TestStrictPublicationRejectsMigrationByteDriftBeforeAnyWriteIntegration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	migrationDir, artifactLedger := fixturePublicationArtifact(t, testdb.MigrationsDir(t))
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, BaselineMigrationM100))
	seedPublicationDeployment(t, ctx, pool)
	baseline, err := CompiledBaseline()
	require.NoError(t, err)
	declaration := fixtureDeclaration()
	artifact := ArtifactIdentity{Kind: ArtifactKindSource, Commit: strings.Repeat("1", 40), Tree: strings.Repeat("2", 40), Version: "fixture",
		PublisherExecutableDigest: "sha256:" + strings.Repeat("3", 64)}
	authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, baseline, declaration)
	plan, err := PlanTarget(ctx, pool, "integration", authority)
	require.NoError(t, err)
	hooks, err := HooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	originalBefore := hooks.Before
	hooks.Before = func(ctx context.Context, conn *pgxpool.Conn) error {
		if err := originalBefore(ctx, conn); err != nil {
			return err
		}
		path := filepath.Join(migrationDir, fixturePublicationM101+".up.sql")
		wire, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(path, append(wire, []byte("\n-- drift after preflight\n")...), 0o600)
	}

	err = migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks)
	require.ErrorContains(t, err, "bytes changed after Plan")
	assertPublicationAbsent(t, ctx, pool)
}

func TestRunThroughRejectsNonPrefixLedgerBeforeMigrationWriteIntegration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	_, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (
version VARCHAR(64) PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ('000002_non_prefix_fixture')`)
	require.NoError(t, err)

	err = migrate.RunThrough(ctx, pool, testdb.MigrationsDir(t), BaselineMigrationM100)
	require.ErrorContains(t, err, "not the exact artifact prefix")
	var orgsTable *string
	require.NoError(t, pool.QueryRow(ctx, `SELECT to_regclass('public.orgs')::text`).Scan(&orgsTable))
	require.Nil(t, orgsTable)
	var ledgerCount int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&ledgerCount))
	require.Equal(t, int64(1), ledgerCount)
}

func TestGuardedDownRejectsForgedProjectionPlanIntegration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	migrationDir, artifactLedger := fixturePublicationArtifact(t, testdb.MigrationsDir(t))
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, BaselineMigrationM100))
	deploymentID := seedPublicationDeployment(t, ctx, pool)
	baseline, err := CompiledBaseline()
	require.NoError(t, err)
	declaration := fixtureDeclaration()
	artifact := ArtifactIdentity{Kind: ArtifactKindSource, Commit: strings.Repeat("1", 40), Tree: strings.Repeat("2", 40), Version: "fixture",
		PublisherExecutableDigest: "sha256:" + strings.Repeat("3", 64)}
	authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, baseline, declaration)
	plan, err := PlanTarget(ctx, pool, "integration", authority)
	require.NoError(t, err)
	hooks, err := HooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks))

	_, err = pool.Exec(ctx, `ALTER TABLE model_deployment_capabilities DISABLE TRIGGER USER`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO model_deployment_capabilities (
deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,revision,support_revision)
VALUES ($1,'openai.chat_completions.v1','fixture.unrelated','enabled','declared','fixture:unrelated',1,1)`, deploymentID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `ALTER TABLE model_deployment_capabilities ENABLE TRIGGER USER`)
	require.NoError(t, err)
	forged := plan
	forged.ProjectionItems = append(forged.ProjectionItems, ProjectionPlanItem{
		Action: ProjectionActionInsert, ExpectedAffectedRows: 1,
		Final: DeploymentProjection{DeploymentID: deploymentID, OperationID: "openai.chat_completions.v1",
			CapabilityID: "fixture.unrelated", SupportState: "enabled", EvidenceState: "declared",
			EvidenceRef: "fixture:unrelated", Revision: 1, SupportRevision: 1},
	})
	forged, err = FinalizePlan(forged)
	require.NoError(t, err)
	_, err = DownHooksForPlan("integration", authority, forged)
	require.ErrorContains(t, err, "not authorized by an exact declaration effect")
	var rowCount, ledgerCount int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_deployment_capabilities
WHERE deployment_id=$1 AND capability_id='fixture.unrelated'`, deploymentID).Scan(&rowCount))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=$1`, fixturePublicationM101).Scan(&ledgerCount))
	require.Equal(t, int64(1), rowCount)
	require.Equal(t, int64(1), ledgerCount)
}

func TestGuardedDownRejectsForgedUpdatePredecessorBeforeWriteIntegration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	declaration := fixtureUpdateDeclaration()
	migrationDir, artifactLedger := fixturePublicationArtifactForDeclaration(t, testdb.MigrationsDir(t), declaration)
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, BaselineMigrationM100))
	deploymentID := seedPublicationDeployment(t, ctx, pool)
	_, err := pool.Exec(ctx, `ALTER TABLE model_deployment_capabilities DISABLE TRIGGER USER`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO model_deployment_capabilities (
deployment_id,operation_id,capability_id,support_state,evidence_state,evidence_ref,revision,support_revision)
VALUES ($1,'openai.chat_completions.v1','streaming','enabled','declared',$2,4,2)`, deploymentID,
		declaration.DeclaredProjectionEffects[0].ExpectedContractRef)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `ALTER TABLE model_deployment_capabilities ENABLE TRIGGER USER`)
	require.NoError(t, err)

	baseline, err := CompiledBaseline()
	require.NoError(t, err)
	artifact := ArtifactIdentity{Kind: ArtifactKindSource, Commit: strings.Repeat("1", 40), Tree: strings.Repeat("2", 40), Version: "fixture",
		PublisherExecutableDigest: "sha256:" + strings.Repeat("3", 64)}
	authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, baseline, declaration)
	plan, err := PlanTarget(ctx, pool, "integration", authority)
	require.NoError(t, err)
	require.Equal(t, ProjectionActionUpdate, plan.ProjectionItems[0].Action)
	hooks, err := HooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks))

	forged := plan
	forged.ProjectionItems = append([]ProjectionPlanItem(nil), plan.ProjectionItems...)
	forgedExpected := *forged.ProjectionItems[0].Expected
	forgedExpected.EvidenceRef = "capability_contract:forged-predecessor@9.9.9"
	forged.ProjectionItems[0].Expected = &forgedExpected
	forged, err = FinalizePlan(forged)
	require.NoError(t, err)
	_, err = DownHooksForPlan("integration", authority, forged)
	require.ErrorContains(t, err, "not authorized by an exact declaration effect")
	require.NoError(t, VerifyPlanFinal(ctx, pool, plan))
	var ledgerCount int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=$1`, fixturePublicationM101).Scan(&ledgerCount))
	require.Equal(t, int64(1), ledgerCount)
}

func TestStrictPublicationDownRefusesRetainedProviderAttemptEvidenceIntegration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	migrationDir, artifactLedger := fixturePublicationArtifact(t, testdb.MigrationsDir(t))
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, BaselineMigrationM100))
	seedPublicationDeployment(t, ctx, pool)
	baseline, err := CompiledBaseline()
	require.NoError(t, err)
	declaration := fixtureDeclaration()
	artifact := ArtifactIdentity{Kind: ArtifactKindSource, Commit: strings.Repeat("1", 40), Tree: strings.Repeat("2", 40), Version: "fixture",
		PublisherExecutableDigest: "sha256:" + strings.Repeat("3", 64)}
	authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, baseline, declaration)
	plan, err := PlanTarget(ctx, pool, "integration", authority)
	require.NoError(t, err)
	hooks, err := HooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks))

	// The isolated database uses a minimal surrogate after publication so this
	// test exercises the exact provider-attempt evidence JSON predicate without creating
	// unrelated billing/customer fixtures.
	_, err = pool.Exec(ctx, `ALTER TABLE billing_provider_attempts RENAME TO billing_provider_attempts_fixture_original`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `CREATE TABLE billing_provider_attempts (
route_capability_evidence JSONB NOT NULL, execution_snapshot_hash VARCHAR(69))`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO billing_provider_attempts (route_capability_evidence)
VALUES ('[{"capability_id":"fixture.publication.v1","evidence_ref":"capability_contract:unirouter.capability-probe.fixture.v1@1.0.0"}]'::jsonb)`)
	require.NoError(t, err)

	downHooks, err := DownHooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	err = migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks)
	require.ErrorContains(t, err, "retained provider-attempt capability evidence")
	var ledgerCount int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=$1`, fixturePublicationM101).Scan(&ledgerCount))
	require.Equal(t, int64(1), ledgerCount)
}

func TestStrictPublicationDownRefusesRetainedFreezeSnapshotIntegration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	migrationDir, artifactLedger := fixturePublicationArtifact(t, testdb.MigrationsDir(t))
	require.NoError(t, migrate.RunThrough(ctx, pool, migrationDir, BaselineMigrationM100))
	deploymentID := seedPublicationDeployment(t, ctx, pool)
	baseline, err := CompiledBaseline()
	require.NoError(t, err)
	declaration := fixtureDeclaration()
	artifact := ArtifactIdentity{Kind: ArtifactKindSource, Commit: strings.Repeat("1", 40), Tree: strings.Repeat("2", 40), Version: "fixture",
		PublisherExecutableDigest: "sha256:" + strings.Repeat("3", 64)}
	authority := fixturePlanAuthority(t, migrationDir, artifactLedger, artifact, baseline, declaration)
	plan, err := PlanTarget(ctx, pool, "integration", authority)
	require.NoError(t, err)
	hooks, err := HooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublication(ctx, pool, migrationDir, plan.ApplyMigrations, hooks))

	_, err = pool.Exec(ctx, `INSERT INTO freeze_idempotency (
request_id,org_id,status,pricing_version,execution_snapshot_hash)
SELECT 'publication-freeze-fixture', customer_org_id, 'frozen', 'fixture',
       'mrs1_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
  FROM model_deployments WHERE id=$1`, deploymentID)
	require.NoError(t, err)
	downHooks, err := DownHooksForPlan("integration", authority, plan)
	require.NoError(t, err)
	err = migrate.RunPublicationDown(ctx, pool, migrationDir, downHooks)
	require.ErrorContains(t, err, "retained execution Freeze evidence")
	var ledgerCount int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=$1`, fixturePublicationM101).Scan(&ledgerCount))
	require.Equal(t, int64(1), ledgerCount)
}

func fixturePublicationArtifact(t *testing.T, source string) (string, []string) {
	return fixturePublicationArtifactForDeclaration(t, source, fixtureDeclaration())
}

func fixturePublicationArtifactForDeclaration(t *testing.T, source string, declaration Declaration) (string, []string) {
	t.Helper()
	target := t.TempDir()
	entries, err := os.ReadDir(source)
	require.NoError(t, err)
	var ledger []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") || entry.Name() > BaselineMigrationM100+".up.sql" {
			continue
		}
		wire, err := os.ReadFile(filepath.Join(source, entry.Name()))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(target, entry.Name()), wire, 0o600))
		ledger = append(ledger, strings.TrimSuffix(entry.Name(), ".up.sql"))
	}
	baseline, err := CompiledBaseline()
	require.NoError(t, err)
	generated, err := GenerateMigration(baseline, declaration)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(target, fixturePublicationM101+".up.sql"), generated.UpSQL, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(target, fixturePublicationM101+".down.sql"), generated.DownSQL, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(target, fixturePublicationM101+".verify.sql"), generated.VerifySQL, 0o600))
	ledger = append(ledger, fixturePublicationM101)
	return target, ledger
}

func fixturePlanAuthority(t *testing.T, migrationDir string, ledger []string, artifact ArtifactIdentity,
	predecessor Baseline, declaration Declaration,
) PlanAuthority {
	t.Helper()
	files, err := ReadArtifactMigrationFiles(migrationDir, ledger)
	require.NoError(t, err)
	down, verify, err := ReadPublicationAuxiliaryDigests(migrationDir, declaration.PublicationID)
	require.NoError(t, err)
	return PlanAuthority{
		Artifact: artifact, ArtifactMigrationLedger: append([]string(nil), ledger...), ArtifactMigrationFiles: files,
		PublicationDownSHA256: down, PublicationVerifySHA256: verify,
		Predecessor: predecessor, Declaration: declaration,
	}
}

func fixtureDeclaration() Declaration {
	return Declaration{
		SchemaVersion: DeclarationSchemaV1, PublicationID: fixturePublicationM101,
		ExpectedStaticPredecessor: BaselineMigrationM100, ExpectedSchemaPredecessor: BaselineMigrationM100,
		BindingAppends: []Binding{{
			OperationID: "openai.chat_completions.v1", CapabilityID: "fixture.publication.v1",
			ExecutionProfileID: "deepseek.v4-pro.api", ExecutionProfileVersion: "1.5.0",
			ProtocolProfileID: "protocol.deepseek-openai-chat.v4", ProtocolProfileVersion: "1.0.0",
			ProbeContractID: "unirouter.capability-probe.fixture.v1", ProbeContractVersion: "1.0.0",
			ExpectedProvider: "deepseek", ExpectedUpstreamModel: "deepseek-v4-pro",
			PublicPath: "/v1/chat/completions", IntroducedMigration: fixturePublicationM101,
		}},
		SelectorTransitions: []SelectorTransition{{
			OperationID: "openai.chat_completions.v1", CapabilityID: "fixture.publication.v1",
			ExecutionProfileID: "deepseek.v4-pro.api", ExecutionProfileVersion: "1.5.0",
			ProtocolProfileID: "protocol.deepseek-openai-chat.v4", ProtocolProfileVersion: "1.0.0",
			Final: ContractIdentity{ProbeContractID: "unirouter.capability-probe.fixture.v1", ProbeContractVersion: "1.0.0"},
		}},
		DeclaredProjectionEffects: []ProjectionEffect{{
			OperationID: "openai.chat_completions.v1", CapabilityID: "fixture.publication.v1",
			ExecutionProfileID: "deepseek.v4-pro.api", ExecutionProfileVersion: "1.5.0",
			ProtocolProfileID: "protocol.deepseek-openai-chat.v4", ProtocolProfileVersion: "1.0.0",
			ExpectedProvider: "deepseek", ExpectedUpstreamModel: "deepseek-v4-pro",
			FinalContractRef: "capability_contract:unirouter.capability-probe.fixture.v1@1.0.0", AllowInsert: true,
		}},
	}
}

func fixtureUpdateDeclaration() Declaration {
	const contractID = "unirouter.capability-probe.deepseek-chat-sse.v1"
	return Declaration{
		SchemaVersion: DeclarationSchemaV1, PublicationID: fixturePublicationM101,
		ExpectedStaticPredecessor: BaselineMigrationM100, ExpectedSchemaPredecessor: BaselineMigrationM100,
		BindingAppends: []Binding{{
			OperationID: "openai.chat_completions.v1", CapabilityID: "streaming",
			ExecutionProfileID: "deepseek.v4-pro.api", ExecutionProfileVersion: "1.5.0",
			ProtocolProfileID: "protocol.deepseek-openai-chat.v4", ProtocolProfileVersion: "1.0.0",
			ProbeContractID: contractID, ProbeContractVersion: "3.0.0",
			ExpectedProvider: "deepseek", ExpectedUpstreamModel: "deepseek-v4-pro",
			PublicPath: "/v1/chat/completions", IntroducedMigration: fixturePublicationM101,
		}},
		SelectorTransitions: []SelectorTransition{{
			OperationID: "openai.chat_completions.v1", CapabilityID: "streaming",
			ExecutionProfileID: "deepseek.v4-pro.api", ExecutionProfileVersion: "1.5.0",
			ProtocolProfileID: "protocol.deepseek-openai-chat.v4", ProtocolProfileVersion: "1.0.0",
			ExpectedPredecessor: &ContractIdentity{ProbeContractID: contractID, ProbeContractVersion: "2.0.0"},
			Final:               ContractIdentity{ProbeContractID: contractID, ProbeContractVersion: "3.0.0"},
		}},
		DeclaredProjectionEffects: []ProjectionEffect{{
			OperationID: "openai.chat_completions.v1", CapabilityID: "streaming",
			ExecutionProfileID: "deepseek.v4-pro.api", ExecutionProfileVersion: "1.5.0",
			ProtocolProfileID: "protocol.deepseek-openai-chat.v4", ProtocolProfileVersion: "1.0.0",
			ExpectedProvider: "deepseek", ExpectedUpstreamModel: "deepseek-v4-pro",
			ExpectedContractRef: "capability_contract:" + contractID + "@2.0.0",
			FinalContractRef:    "capability_contract:" + contractID + "@3.0.0",
		}},
	}
}

func seedPublicationDeployment(t *testing.T, ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}) int64 {
	t.Helper()
	var orgID, modelID, connectionID, deploymentID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs (name,status,org_type) VALUES ('publication-fixture','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_registry (
org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,context_length,output_limit,
execution_profile_id,execution_profile_version)
VALUES ($1,'publication-fixture','publication-fixture','deepseek','ga',TRUE,1048576,32768,
'deepseek.v4-pro.api','1.5.0') RETURNING id`, orgID).Scan(&modelID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO provider_connections (
name,provider,base_url,adapter_type,adapter_config,admin_state,config_revision)
VALUES ('publication-fixture','deepseek','https://api.deepseek.com','openai_compatible','{}','enabled',1) RETURNING id`).Scan(&connectionID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO model_deployments (
customer_org_id,public_model_id,connection_id,upstream_model,admin_state,config_revision,max_context,default_max_output)
VALUES ($1,$2,$3,'deepseek-v4-pro','enabled',1,1048576,32768) RETURNING id`, orgID, modelID, connectionID).Scan(&deploymentID))
	_, err := pool.Exec(ctx, `INSERT INTO model_deployment_operations (
deployment_id,operation_id,support_state,evidence_state,evidence_ref,revision,protocol_profile_id,protocol_profile_version)
VALUES ($1,'openai.chat_completions.v1','enabled','declared','',1,'protocol.deepseek-openai-chat.v4','1.0.0')`, deploymentID)
	require.NoError(t, err)
	return deploymentID
}

func assertPublicationAbsent(t *testing.T, ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) {
	t.Helper()
	var ledger, binding, selector int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=$1`, fixturePublicationM101).Scan(&ledger))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_probe_contract_bindings WHERE introduced_migration=$1`, fixturePublicationM101).Scan(&binding))
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM model_capability_current_probe_contracts WHERE capability_id='fixture.publication.v1'`).Scan(&selector))
	require.Zero(t, ledger)
	require.Zero(t, binding)
	require.Zero(t, selector)
}
