//go:build integration

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

// Exercise the public CLI boundary, not only the domain owner used by HTTP.
func invokeGenerationCLI(t *testing.T, cfg *config.Config, action string, input any) (json.RawMessage, error) {
	t.Helper()
	wire, err := json.Marshal(input)
	require.NoError(t, err)
	in, err := os.CreateTemp(t.TempDir(), "stdin")
	require.NoError(t, err)
	defer in.Close()
	_, err = in.Write(wire)
	require.NoError(t, err)
	_, err = in.Seek(0, 0)
	require.NoError(t, err)
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	require.NoError(t, err)
	defer out.Close()
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = in, out
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	err = runRuntimeGenerationCLI(context.Background(), []string{action}, cfg)
	output, readErr := os.ReadFile(out.Name())
	require.NoError(t, readErr)
	return output, err
}

func TestRuntimeGenerationCLIExpandedLedgerLifecycleIntegration(t *testing.T) {
	for _, head := range schemaauthority.SupportedSchemaHeads() {
		t.Run(head, func(t *testing.T) {
			pool := testutil.SetupMigratedThroughTestDB(t, head)
			dir := testutil.MigrationsDir(t)
			var actualHead string
			require.NoError(t, pool.QueryRow(t.Context(), `SELECT max(version) FROM schema_migrations`).Scan(&actualHead))
			require.Equal(t, head, actualHead, "each CLI case must run on its named actual schema")
			cfg := &config.Config{DB_DSN: pool.Config().ConnString(), DBMaxConnections: 3, MigrationsDir: dir}
			if head != schemaauthority.LatestSupportedMigration {
				cfg.RuntimeEnvironment = "test"
				_, err := invokeGenerationCLI(t, cfg, "inspect", nil)
				if head == schemaauthority.XInsightCommunicationHead {
					require.NoError(t, err, "only exact M127 predecessor maintenance reads remain available")
				} else {
					require.ErrorContains(t, err, "outside this artifact")
				}
				var afterHead string
				require.NoError(t, pool.QueryRow(t.Context(), `SELECT max(version) FROM schema_migrations`).Scan(&afterHead))
				require.Equal(t, head, afterHead, "control read/refusal must not migrate")
				return
			}
			ledger, err := schemaauthority.ExpectedLedger(dir)
			require.NoError(t, err)
			member := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("a", 32), Artifact: "sha256:" + strings.Repeat("b", 64)}
			_, _, err = runtimegeneration.Renew(t.Context(), pool, member, runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads()), ledger, false)
			require.NoError(t, err)
			before, err := invokeGenerationCLI(t, cfg, "inspect", nil)
			require.NoError(t, err, "the real CLI must remain usable after additive expansion")
			again, err := invokeGenerationCLI(t, cfg, "inspect", nil)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(again))
			wire := []byte("CLI lifecycle fixture")
			plan := runtimegeneration.IngressPlan{ChangeID: "cli-after-expand", ExpectedConfigDigest: strings.Repeat("0", 64), DesiredConfigBytes: wire, DesiredConfigDigest: fmt.Sprintf("%x", sha256.Sum256(wire)), Before: []runtimegeneration.Identity{}, After: []runtimegeneration.Identity{member}}
			result, err := invokeGenerationCLI(t, cfg, "prepare", plan)
			require.NoError(t, err)
			require.JSONEq(t, `{"change_id":"cli-after-expand","status":"pending"}`, string(result))
			result, err = invokeGenerationCLI(t, cfg, "read", map[string]string{"change_id": plan.ChangeID})
			require.NoError(t, err)
			require.Contains(t, string(result), `"pending"`)
			result, err = invokeGenerationCLI(t, cfg, "complete", map[string]any{"change_id": plan.ChangeID, "observed_digest": plan.DesiredConfigDigest, "old_workers_gone": true, "observation": map[string]bool{"fixture": true}})
			require.NoError(t, err)
			require.JSONEq(t, `{"change_id":"cli-after-expand","status":"applied"}`, string(result))
			_, err = invokeGenerationCLI(t, cfg, "complete", map[string]any{"change_id": plan.ChangeID, "observed_digest": plan.DesiredConfigDigest, "old_workers_gone": true, "observation": map[string]bool{"fixture": true}})
			require.NoError(t, err)
			var count int
			require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM runtime_ingress_changes`).Scan(&count))
			require.Equal(t, 1, count)
		})
	}
}

func TestRuntimeGenerationCLIRejectsUnapprovedLedgersIntegration(t *testing.T) {
	for _, kind := range []string{"legacy", "gap", "future"} {
		t.Run(kind, func(t *testing.T) {
			head := schemaauthority.XInsightAdoptionExpandHead
			if kind == "legacy" {
				head = schemaauthority.LegacyCapabilityHead
			}
			pool := testutil.SetupMigratedThroughTestDB(t, head)
			var query string
			switch kind {
			case "legacy":
				var actualHead string
				require.NoError(t, pool.QueryRow(t.Context(), `SELECT max(version) FROM schema_migrations`).Scan(&actualHead))
				require.Equal(t, schemaauthority.LegacyCapabilityHead, actualHead)
			case "gap":
				query = `DELETE FROM schema_migrations WHERE version='000114_capability_delivery_expand'`
			case "future":
				query = `INSERT INTO schema_migrations(version) VALUES ('000125_unapproved_future')`
			}
			if query != "" {
				_, err := pool.Exec(t.Context(), query)
				require.NoError(t, err)
			}
			cfg := &config.Config{DB_DSN: pool.Config().ConnString(), DBMaxConnections: 3, MigrationsDir: testutil.MigrationsDir(t)}
			for _, action := range []string{"inspect", "read", "prepare", "complete"} {
				_, err := invokeGenerationCLI(t, cfg, action, map[string]any{})
				require.Error(t, err)
				if kind == "legacy" {
					require.ErrorContains(t, err, "outside this artifact")
				}
			}
			if kind == "legacy" {
				var absent bool
				require.NoError(t, pool.QueryRow(t.Context(), `SELECT to_regclass('public.runtime_ingress_changes') IS NULL`).Scan(&absent))
				require.True(t, absent, "legacy rejection must not create the expanded schema")
			} else {
				var count int
				require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM runtime_ingress_changes`).Scan(&count))
				require.Zero(t, count)
			}
		})
	}
}

func TestRuntimeGenerationCLIHonorsCommandDeadlineIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	defer tx.Rollback(context.Background())
	_, err = tx.Exec(t.Context(), `LOCK TABLE runtime_ingress_changes IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	cfg := &config.Config{DB_DSN: pool.Config().ConnString(), DBMaxConnections: 3, MigrationsDir: testutil.MigrationsDir(t)}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = runRuntimeGenerationCLI(ctx, []string{"inspect"}, cfg)
	require.Error(t, err)
	require.Less(t, time.Since(started), 2*time.Second)
	require.NoError(t, tx.Rollback(t.Context()), "a timed-out observer must not abort the competing owner")
}
