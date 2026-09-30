//go:build integration

package contentcapture

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScopedManagementPGVersionedOverrideAndPersonalException(t *testing.T) {
	pool, store, org, project := setupPolicyStorePG(t)
	ctx := t.Context()
	application := PolicyApplication{StorageStatus: "ready", ApplyNow: true, Now: time.Now().UTC()}
	tx := policyStorePGTx(t, pool, org, project)
	beforeEnterprise, err := store.EnterpriseManagementTx(ctx, tx, org, "ready")
	require.NoError(t, err)
	require.False(t, beforeEnterprise.Enterprise.Explicit)
	require.Equal(t, store.options.SupportedProtocols, beforeEnterprise.PolicyCapabilities.SupportedProtocols)
	require.Equal(t, store.options.ObservationPoints, beforeEnterprise.PolicyCapabilities.ObservationPoints)
	beforeEnterprise.PolicyCapabilities.SupportedProtocols[0] = "changed"
	require.NotEqual(t, "changed", store.options.SupportedProtocols[0], "read capabilities must not alias store options")
	require.NoError(t, tx.Commit(ctx))
	quota := int64(1024)
	putEnterprise := ScopedManagementPut{SchemaVersion: ScopedPolicyManagementSchema,
		ExpectedPolicyRef: beforeEnterprise.Enterprise.PolicyRef, ExpectedVersion: "0",
		ExpectedVector: beforeEnterprise.VersionVector, Desired: testFullPolicy(),
		ExecutionStorageMode: ExecutionStorageTemporary, ProjectQuotaBytes: &quota}
	unsupported := putEnterprise
	unsupported.Desired.ObservationPoints = []string{"client_response"}
	_, _, err = store.validateScopedDesired(unsupported, PolicyScopeEnterprise, quota, application)
	require.ErrorContains(t, err, "observation point is unsupported by this deployment")
	tx = policyStorePGTx(t, pool, org, project)
	enterprise, err := store.PutEnterpriseTx(ctx, tx, org, putEnterprise, application)
	require.NoError(t, err)
	require.Equal(t, "1", enterprise.Enterprise.Desired.PolicyVersion)
	require.NoError(t, tx.Commit(ctx))
	tx = policyStorePGTx(t, pool, org, project)
	beforeProject, err := store.ScopedManagementTx(ctx, tx, org, project, 0, "ready")
	require.NoError(t, err)
	require.Equal(t, PolicyScopeEnterprise, beforeProject.Effective.Scope)
	require.False(t, beforeProject.Project.Explicit)
	require.Equal(t, store.options.SupportedProtocols, beforeProject.PolicyCapabilities.SupportedProtocols)
	require.Equal(t, store.options.ObservationPoints, beforeProject.PolicyCapabilities.ObservationPoints)
	require.NoError(t, tx.Commit(ctx))
	disabled := testMetadataPolicy()
	disabled.Mode = ModeDisabled
	projectQuota := int64(2048)
	putProject := ScopedManagementPut{SchemaVersion: ScopedPolicyManagementSchema,
		ExpectedPolicyRef: beforeProject.Project.PolicyRef, ExpectedVersion: "0",
		ExpectedVector: beforeProject.VersionVector, Desired: disabled,
		ExecutionStorageMode: ExecutionStorageStrict, ProjectQuotaBytes: &projectQuota}
	tx = policyStorePGTx(t, pool, org, project)
	projectView, err := store.PutScopedTx(ctx, tx, PolicyScopeProject, org, project, 0, putProject, application)
	require.NoError(t, err)
	require.Equal(t, PolicyScopeProject, projectView.Effective.Scope)
	require.Equal(t, ExecutionStorageStrict, projectView.Effective.ExecutionStorageMode)
	require.NoError(t, tx.Commit(ctx))
	tx = policyStorePGTx(t, pool, org, project)
	_, err = store.PutScopedTx(ctx, tx, PolicyScopeProject, org, project, 0, putProject, application)
	require.ErrorIs(t, err, ErrPolicyVersionConflict)
	require.NoError(t, tx.Rollback(ctx))

	var userID int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,role,organization_role,status)
      VALUES($1,'scoped-management-user','synthetic','member','member','active') RETURNING id`, org).Scan(&userID))
	_, err = pool.Exec(ctx, `INSERT INTO project_members(project_id,user_id,role) VALUES($1,$2,'project_user')`, project, userID)
	require.NoError(t, err)
	tx = policyStorePGTx(t, pool, org, project)
	beforeUser, err := store.ScopedManagementTx(ctx, tx, org, project, userID, "ready")
	require.NoError(t, err)
	require.False(t, beforeUser.User.Explicit)
	require.Equal(t, store.options.SupportedProtocols, beforeUser.PolicyCapabilities.SupportedProtocols)
	require.NoError(t, tx.Commit(ctx))
	userFull := testFullPolicy()
	userFull.QuotaBytes = nil
	putUser := ScopedManagementPut{SchemaVersion: ScopedPolicyManagementSchema,
		ExpectedPolicyRef: beforeUser.User.PolicyRef, ExpectedVersion: "0",
		ExpectedVector: beforeUser.VersionVector, Desired: userFull,
		ExecutionStorageMode: ExecutionStorageTemporary}
	tx = policyStorePGTx(t, pool, org, project)
	userView, err := store.PutScopedTx(ctx, tx, PolicyScopeUser, org, project, userID, putUser, application)
	require.NoError(t, err)
	require.Equal(t, PolicyScopeUser, userView.Effective.Scope)
	require.Equal(t, projectQuota, *userView.Effective.Effective.Config.QuotaBytes)
	require.Equal(t, ExecutionStorageTemporary, userView.Effective.ExecutionStorageMode)
	require.NoError(t, tx.Commit(ctx))
	pendingConfig, err := json.Marshal(testMetadataPolicy())
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE execution_content_policies
      SET desired_version=2,desired_config=$3,desired_execution_storage_mode='temporary_allowed',
          application_status='pending',application_reason='awaiting_activation'
      WHERE org_id=$1 AND project_id=$2`, org, project, pendingConfig)
	require.NoError(t, err)
	tx = policyStorePGTx(t, pool, org, project)
	pending, err := store.ScopedManagementTx(ctx, tx, org, project, 0, "ready")
	require.NoError(t, err)
	require.Equal(t, "2", pending.Project.Desired.PolicyVersion)
	require.Equal(t, "1", pending.Project.Effective.PolicyVersion)
	require.Equal(t, ModeDisabled, pending.Effective.Effective.Config.Mode)
	require.Equal(t, ExecutionStorageStrict, pending.Effective.ExecutionStorageMode)
	require.NoError(t, tx.Commit(ctx))
	_, err = pool.Exec(ctx, `DELETE FROM project_members WHERE project_id=$1 AND user_id=$2`, project, userID)
	require.NoError(t, err)
	tx = policyStorePGTx(t, pool, org, project)
	_, err = store.ScopedManagementTx(ctx, tx, org, project, userID, "ready")
	require.True(t, errors.Is(err, ErrPolicyInactive))
	require.NoError(t, tx.Rollback(ctx))
}
