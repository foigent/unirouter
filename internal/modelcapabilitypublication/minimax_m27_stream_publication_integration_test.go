//go:build integration

package modelcapabilitypublication

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testdb"
)

func TestMiniMaxM27M111ActualArtifactsGuardedRoundTripIntegration(t *testing.T) {
	pool := testdb.SetupIsolated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	dir := testdb.MigrationsDir(t)
	ledger, err := schemaauthority.ExpectedLedger(dir)
	require.NoError(t, err)
	require.NoError(t, VerifyArtifactPublicationInventory(dir, ledger))
	require.NoError(t, migrate.RunThrough(ctx, pool, dir, BaselineMigrationM100))
	artifact := ArtifactIdentity{Kind: ArtifactKindSource, Commit: strings.Repeat("1", 40), Tree: strings.Repeat("2", 40), Version: "m111-integration", PublisherExecutableDigest: "sha256:" + strings.Repeat("3", 64)}
	apply := func(id string) Plan {
		t.Helper()
		d, predecessor, err := CompiledPublication(id)
		require.NoError(t, err)
		authority := fixturePlanAuthority(t, dir, ledger, artifact, predecessor, d)
		p, err := PlanTarget(ctx, pool, "integration", authority)
		require.NoError(t, err)
		require.Equal(t, ValidationValid, p.Validation)
		hooks, err := HooksForPlan("integration", authority, p)
		require.NoError(t, err)
		require.NoError(t, migrate.RunPublication(ctx, pool, dir, p.ApplyMigrations, hooks))
		require.NoError(t, VerifyPlanFinal(ctx, pool, p))
		return p
	}
	for _, id := range []string{
		"000101_kimi_k27_chat_core_qualification", "000102_kimi_k3_chat_core_qualification",
		"000103_minimax_m3_glm53_function_tools_qualification", "000104_minimax_payg_function_tools_qualification",
		"000105_glm_toggle_function_tools_qualification", "000106_minimax_official_cn_payg_endpoint_qualification",
	} {
		apply(id)
	}
	require.NoError(t, migrate.RunThrough(ctx, pool, dir, "000107_stable_model_successor_cutover"))
	apply("000108_minimax_cache_threshold_qualification")
	require.NoError(t, migrate.RunThrough(ctx, pool, dir, "000109_staging_model_successor_cutover"))
	d, predecessor, err := CompiledPublication("000111_minimax_m27_stream_function_tools_qualification")
	require.NoError(t, err)
	authority := fixturePlanAuthority(t, dir, ledger, artifact, predecessor, d)
	pre, err := PlanTarget(ctx, pool, "integration", authority)
	require.NoError(t, err)
	require.Equal(t, ValidationConflict, pre.Validation, "M110 predecessor is mandatory")
	m110 := apply("000110_openai_chat_runtime_capability_qualification")
	history := func() string {
		t.Helper()
		var snapshot string
		require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(b) ORDER BY to_jsonb(b)::text)::text FROM model_capability_probe_contract_bindings b WHERE introduced_migration <> '000111_minimax_m27_stream_function_tools_qualification'`).Scan(&snapshot))
		return snapshot
	}
	before := history()
	p, err := PlanTarget(ctx, pool, "integration", authority)
	require.NoError(t, err)
	require.Equal(t, ValidationValid, p.Validation)
	appended := 0
	for _, item := range p.BindingItems {
		if item.Action == BindingActionAppend {
			appended++
		}
	}
	require.Equal(t, 7, appended)
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations WHERE version=$1`, p.PublicationID).Scan(&count))
	require.Zero(t, count, "planning is read only")
	p = apply(p.PublicationID)
	require.Equal(t, before, history(), "all historical binding rows remain exact")
	down, err := DownHooksForPlan("integration", authority, p)
	require.NoError(t, err)
	require.NoError(t, migrate.RunPublicationDown(ctx, pool, dir, down))
	require.NoError(t, VerifyPlanFinal(ctx, pool, m110))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM model_capability_probe_contract_bindings WHERE introduced_migration=$1`, p.PublicationID).Scan(&count))
	require.Zero(t, count)
	p = apply(p.PublicationID)
	var orgID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES ('m111-fixture','active','customer') RETURNING id`).Scan(&orgID))
	_, err = pool.Exec(ctx, `INSERT INTO model_registry(org_id,model_name,display_name,provider_family,lifecycle,approved_catalog,context_length,output_limit,execution_profile_id,execution_profile_version)
VALUES ($1,'m111-fixture','M2.7 fixture','minimax','staging',FALSE,204800,65536,'minimax.m2.7.payg','1.3.0')`, orgID)
	require.NoError(t, err)
	down, err = DownHooksForPlan("integration", authority, p)
	require.NoError(t, err)
	require.ErrorContains(t, migrate.RunPublicationDown(ctx, pool, dir, down), "retained Profile attachment prevents publication down")
	require.NoError(t, VerifyPlanFinal(ctx, pool, p), "rejected down preserves candidate and attachment")
}
