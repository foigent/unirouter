//go:build integration

package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCommercialRetirementCLIWithdrawalIntegration(t *testing.T) {
	// Retain the named gate and all refusal/withdrawal assertions for the current adjacent maintenance pair.
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.CommercialLicenseRetirementHead)
	ctx := t.Context()
	artifact, err := schemaauthority.ExpectedLedger(testutil.MigrationsDir(t))
	oldArtifact := artifact[:len(artifact)-1]
	require.NoError(t, err)
	old := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("a", 32), Artifact: "sha256:" + strings.Repeat("b", 64)}
	_, _, err = runtimegeneration.Renew(ctx, pool, old, runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads()), oldArtifact, false)
	require.NoError(t, err)
	hash := func(w []byte) string { return fmt.Sprintf("%x", sha256.Sum256(w)) }
	wire := []byte("synthetic old receiving config")
	initial := runtimegeneration.IngressPlan{ChangeID: "old-ingress", ExpectedConfigDigest: hash(wire), DesiredConfigDigest: hash(wire), DesiredConfigBytes: wire, Before: []runtimegeneration.Identity{}, After: []runtimegeneration.Identity{old}}
	_, err = runtimegeneration.PrepareIngress(ctx, pool, initial)
	require.NoError(t, err)
	_, err = runtimegeneration.CompleteIngress(ctx, pool, initial.ChangeID, initial.DesiredConfigDigest, true, json.RawMessage(`{"fixture":true}`))
	require.NoError(t, err)
	cfg := &config.Config{DB_DSN: pool.Config().ConnString(), DBMaxConnections: 3, MigrationsDir: testutil.MigrationsDir(t), RuntimeEnvironment: "test"}
	before, err := invokeGenerationCLI(t, cfg, "inspect", nil)
	require.NoError(t, err)
	empty := []byte("synthetic no receivers")
	withdrawal := runtimegeneration.IngressPlan{ChangeID: "withdraw-all", PredecessorID: initial.ChangeID, ExpectedConfigDigest: initial.DesiredConfigDigest, DesiredConfigDigest: hash(empty), DesiredConfigBytes: empty, Before: initial.After, After: []runtimegeneration.Identity{}}
	for _, denied := range []runtimegeneration.IngressPlan{initial, func() runtimegeneration.IngressPlan { p := withdrawal; p.After = nil; return p }()} {
		_, err = invokeGenerationCLI(t, cfg, "prepare", denied)
		require.ErrorContains(t, err, "outside this artifact")
	}
	cfg.RuntimeEnvironment = "production"
	_, err = invokeGenerationCLI(t, cfg, "prepare", withdrawal)
	require.ErrorContains(t, err, "outside this artifact")
	cfg.RuntimeEnvironment = "test"
	after, err := invokeGenerationCLI(t, cfg, "inspect", nil)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after), "denied actions must not mutate generations or ingress")
	result, err := invokeGenerationCLI(t, cfg, "prepare", withdrawal)
	require.NoError(t, err)
	require.Contains(t, string(result), `"pending"`)
	retained, err := invokeGenerationCLI(t, cfg, "read", map[string]string{"change_id": withdrawal.ChangeID})
	require.NoError(t, err)
	require.Contains(t, string(retained), `"membership_after":[]`)
	completion := map[string]any{"change_id": withdrawal.ChangeID, "observed_digest": withdrawal.DesiredConfigDigest, "old_workers_gone": false, "observation": map[string]bool{"synthetic": true}}
	result, err = invokeGenerationCLI(t, cfg, "complete", completion)
	require.NoError(t, err)
	require.Contains(t, string(result), `"ambiguous"`)
	completion["old_workers_gone"] = true
	result, err = invokeGenerationCLI(t, cfg, "complete", completion)
	require.NoError(t, err)
	require.Contains(t, string(result), `"applied"`)
	var state, head string
	require.NoError(t, pool.QueryRow(ctx, `SELECT ingress_state FROM runtime_generations WHERE instance_id=$1`, old.Instance).Scan(&state))
	require.Equal(t, "absent", state)
	require.NoError(t, pool.QueryRow(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&head))
	require.Equal(t, schemaauthority.CommercialLicenseRetirementHead, head, "maintenance controller never applies migration")
}
