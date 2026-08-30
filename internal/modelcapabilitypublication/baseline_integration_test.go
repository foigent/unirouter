package modelcapabilitypublication_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/migrate"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/testutil"
)

const updateBaselineEnvironment = "UPDATE_CAPABILITY_CONTRACT_BASELINE"

func TestBaselineM100MatchesFreshDatabase(t *testing.T) {
	pool := testutil.SetupIsolatedTestDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	m100Dir, expectedLedger := migrationsThroughM100(t, testutil.MigrationsDir(t))
	require.NoError(t, migrate.Run(ctx, pool, m100Dir))

	baseline, err := publication.ExportBaseline(ctx, pool, expectedLedger)
	require.NoError(t, err)
	require.NoError(t, publication.VerifyRuntimeStaticProjection(ctx, pool, expectedLedger))
	require.Len(t, baseline.Bindings, 76)
	require.Len(t, baseline.Selectors, 51)
	canonical, err := publication.CanonicalBaseline(baseline)
	require.NoError(t, err)
	canonical = append(canonical, '\n')

	baselinePath := filepath.Join("declarations", "baseline-m100.json")
	if os.Getenv(updateBaselineEnvironment) == "1" {
		require.NoError(t, os.WriteFile(baselinePath, canonical, 0o644))
	}
	want, err := os.ReadFile(baselinePath)
	require.NoError(t, err, "run %s=1 go test ./internal/modelcapabilitypublication -run TestBaselineM100MatchesFreshDatabase once after an intentional M100 import change", updateBaselineEnvironment)
	require.Equal(t, want, canonical)

	_, err = pool.Exec(ctx, `DROP TRIGGER model_capability_probe_contract_bindings_immutable
ON model_capability_probe_contract_bindings`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE model_capability_probe_contract_bindings
SET public_path='/publication-drift'
WHERE (operation_id,capability_id,execution_profile_id,execution_profile_version,
       protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version) =
      (SELECT operation_id,capability_id,execution_profile_id,execution_profile_version,
              protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version
         FROM model_capability_probe_contract_bindings ORDER BY operation_id,capability_id LIMIT 1)`)
	require.NoError(t, err)
	require.ErrorContains(t, publication.VerifyRuntimeStaticProjection(ctx, pool, expectedLedger), "static capability-contract projection mismatch")
}

func migrationsThroughM100(t *testing.T, sourceDir string) (string, []string) {
	t.Helper()
	targetDir := t.TempDir()
	entries, err := os.ReadDir(sourceDir)
	require.NoError(t, err)
	var ledger []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") || name > publication.BaselineMigrationM100+".up.sql" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(sourceDir, name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, name), body, 0o600))
		ledger = append(ledger, strings.TrimSuffix(name, ".up.sql"))
	}
	require.NotEmpty(t, ledger)
	require.Equal(t, publication.BaselineMigrationM100, ledger[len(ledger)-1])
	return targetDir, ledger
}
