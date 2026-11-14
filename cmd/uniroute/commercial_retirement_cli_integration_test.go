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
	dir := testutil.MigrationsDir(t)
	artifact, err := schemaauthority.ExpectedLedger(dir)
	require.NoError(t, err)
	for _, head := range []string{schemaauthority.UpstreamObservationRestoreHead, schemaauthority.UpgradeAdmissionRecoveryHead} {
		t.Run(head, func(t *testing.T) {
			testRetirementCLIWithdrawal(t, dir, artifact, head)
		})
	}
}

func testRetirementCLIWithdrawal(t *testing.T, dir string, artifact []string, head string) {
	t.Helper()
	pool := testutil.SetupMigratedThroughTestDB(t, head)
	ctx := t.Context()
	ledger, err := readRuntimeSchemaLedger(ctx, pool)
	require.NoError(t, err)
	require.NotEmpty(t, ledger)
	require.Equal(t, head, ledger[len(ledger)-1])
	require.Equal(t, artifact[:len(ledger)], ledger)
	old := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("a", 32), Artifact: "sha256:" + strings.Repeat("b", 64)}
	_, _, err = runtimegeneration.Renew(ctx, pool, old, runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads()), ledger, false)
	require.NoError(t, err)
	hash := func(w []byte) string { return fmt.Sprintf("%x", sha256.Sum256(w)) }
	wire := []byte("synthetic old receiving config")
	initial := runtimegeneration.IngressPlan{ChangeID: "old-ingress", ExpectedConfigDigest: hash(wire), DesiredConfigDigest: hash(wire), DesiredConfigBytes: wire, Before: []runtimegeneration.Identity{}, After: []runtimegeneration.Identity{old}}
	_, err = runtimegeneration.PrepareIngress(ctx, pool, initial)
	require.NoError(t, err)
	_, err = runtimegeneration.CompleteIngress(ctx, pool, initial.ChangeID, initial.DesiredConfigDigest, true, json.RawMessage(`{"fixture":true}`))
	require.NoError(t, err)
	cfg := &config.Config{DB_DSN: pool.Config().ConnString(), DBMaxConnections: 3, MigrationsDir: dir, RuntimeEnvironment: "test"}
	before, err := invokeGenerationCLI(t, cfg, "inspect", nil)
	require.NoError(t, err)
	empty := []byte("synthetic no receivers")
	withdrawal := runtimegeneration.IngressPlan{ChangeID: "withdraw-all", PredecessorID: initial.ChangeID, ExpectedConfigDigest: initial.DesiredConfigDigest, DesiredConfigDigest: hash(empty), DesiredConfigBytes: empty, Before: initial.After, After: []runtimegeneration.Identity{}}
	omittedMembership := withdrawal
	omittedMembership.After = nil
	completion := map[string]any{"change_id": withdrawal.ChangeID, "observed_digest": withdrawal.DesiredConfigDigest, "old_workers_gone": false, "observation": map[string]bool{"synthetic": true}}
	if head == schemaauthority.UpstreamObservationRestoreHead {
		// Exercise a real empty withdrawal and an existing change ID so the
		// refusals reach the current CLI schema boundary.
		for _, denied := range []runtimegeneration.IngressPlan{initial, omittedMembership, withdrawal} {
			_, err = invokeGenerationCLI(t, cfg, "prepare", denied)
			require.ErrorContains(t, err, "outside this artifact")
		}
		read := map[string]string{"change_id": initial.ChangeID}
		_, err = invokeGenerationCLI(t, cfg, "read", read)
		require.ErrorContains(t, err, "outside this artifact")
		complete := map[string]any{"change_id": initial.ChangeID, "observed_digest": initial.DesiredConfigDigest, "old_workers_gone": true, "observation": map[string]bool{"synthetic": true}}
		_, err = invokeGenerationCLI(t, cfg, "complete", complete)
		require.ErrorContains(t, err, "outside this artifact")
		cfg.RuntimeEnvironment = "production"
		_, err = invokeGenerationCLI(t, cfg, "inspect", nil)
		require.ErrorContains(t, err, "outside this artifact")
		_, err = invokeGenerationCLI(t, cfg, "prepare", withdrawal)
		require.ErrorContains(t, err, "outside this artifact")
		cfg.RuntimeEnvironment = "test"
		after, err := invokeGenerationCLI(t, cfg, "inspect", nil)
		require.NoError(t, err)
		require.JSONEq(t, string(before), string(after), "denied actions must not mutate generations or ingress")
		var count int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM runtime_ingress_changes`).Scan(&count))
		require.Equal(t, 1, count)
		actual, err := readRuntimeSchemaLedger(ctx, pool)
		require.NoError(t, err)
		require.Equal(t, ledger, actual)
		return
	}
	_, err = invokeGenerationCLI(t, cfg, "prepare", omittedMembership)
	require.ErrorContains(t, err, "invalid ingress transition")
	after, err := invokeGenerationCLI(t, cfg, "inspect", nil)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after), "invalid withdrawal must preserve generations and ingress")
	result, err := invokeGenerationCLI(t, cfg, "prepare", withdrawal)
	require.NoError(t, err)
	require.Contains(t, string(result), `"pending"`)
	retained, err := invokeGenerationCLI(t, cfg, "read", map[string]string{"change_id": withdrawal.ChangeID})
	require.NoError(t, err)
	require.Contains(t, string(retained), `"membership_after":[]`)
	result, err = invokeGenerationCLI(t, cfg, "complete", completion)
	require.NoError(t, err)
	require.Contains(t, string(result), `"ambiguous"`)
	completion["old_workers_gone"] = true
	result, err = invokeGenerationCLI(t, cfg, "complete", completion)
	require.NoError(t, err)
	require.Contains(t, string(result), `"applied"`)
	var state string
	require.NoError(t, pool.QueryRow(ctx, `SELECT ingress_state FROM runtime_generations WHERE instance_id=$1`, old.Instance).Scan(&state))
	require.Equal(t, "absent", state)
	actual, err := readRuntimeSchemaLedger(ctx, pool)
	require.NoError(t, err)
	require.Equal(t, ledger, actual, "withdrawal never applies migration")
}
