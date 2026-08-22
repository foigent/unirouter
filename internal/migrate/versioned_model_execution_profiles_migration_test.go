package migrate

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionedModelExecutionProfilesMigrationFreezesIdentityAndGuardedDown(t *testing.T) {
	upBody, err := os.ReadFile("../../migrations/000081_versioned_model_execution_profiles.up.sql")
	require.NoError(t, err)
	up := string(upBody)
	require.Contains(t, up, "LOCK TABLE model_registry, schema_migrations")
	require.Contains(t, up, "execution_profile_id VARCHAR(100) NOT NULL DEFAULT 'unverified.chat'")
	require.Contains(t, up, "execution_profile_version VARCHAR(40) NOT NULL DEFAULT '1.0.0'")
	require.Contains(t, up, "model_registry_execution_profile_identity_check")
	require.NotContains(t, strings.ToLower(up), "update model_registry")

	downBody, err := os.ReadFile("../../migrations/000081_versioned_model_execution_profiles.down.sql")
	require.NoError(t, err)
	down := string(downBody)
	require.Contains(t, down, "version > '000081_versioned_model_execution_profiles'")
	require.Contains(t, down, "execution_profile_id <> 'unverified.chat'")
	require.Contains(t, down, "durable profile selections exist")
	require.Less(t, strings.Index(down, "durable profile selections exist"), strings.Index(down, "DROP COLUMN execution_profile_version"))
	require.Less(t, strings.Index(down, "DROP COLUMN execution_profile_id"), strings.Index(down, "WHERE version='000081_versioned_model_execution_profiles'"))
}
