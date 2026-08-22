package migrate

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSealedModelExecutionContractsMigrationPinsReleaseAuthorityAndGuardsDown(t *testing.T) {
	upBody, err := os.ReadFile("../../migrations/000082_sealed_model_execution_contracts.up.sql")
	require.NoError(t, err)
	up := string(upBody)
	require.Contains(t, up, "LOCK TABLE runtime_change_releases, model_registry, schema_migrations")
	require.Contains(t, up, "ADD COLUMN execution_profile_id VARCHAR(100)")
	require.Contains(t, up, "ADD COLUMN public_context_limit INTEGER")
	require.Contains(t, up, "release.public_model_id=model.id")
	require.Contains(t, up, "runtime_change_releases_execution_contract_check")
	require.Contains(t, up, "FOR SHARE;")
	require.Contains(t, up, "canonical release execution contract must equal the locked public model profile")
	require.Contains(t, up, "BEFORE INSERT OR UPDATE ON runtime_change_releases")
	require.Contains(t, up, "runtime_change_releases_execution_contract_immutable")
	require.Contains(t, up, "sealed model execution contract is immutable")
	require.Less(t, strings.Index(up, "LOCK TABLE"), strings.Index(up, "ALTER TABLE runtime_change_releases"))
	require.Less(t, strings.Index(up, "UPDATE runtime_change_releases"), strings.Index(up, "runtime_change_releases_execution_contract_check"))

	downBody, err := os.ReadFile("../../migrations/000082_sealed_model_execution_contracts.down.sql")
	require.NoError(t, err)
	down := string(downBody)
	require.Contains(t, down, "version > '000082_sealed_model_execution_contracts'")
	require.Contains(t, down, "durable release pins would be lost")
	require.Contains(t, down, "release.execution_profile_id IS DISTINCT FROM model.execution_profile_id")
	require.Less(t, strings.Index(down, "durable release pins would be lost"), strings.Index(down, "DROP COLUMN execution_profile_id"))
	require.Less(t, strings.Index(down, "DROP COLUMN execution_profile_id"), strings.Index(down, "WHERE version='000082_sealed_model_execution_contracts'"))
}
