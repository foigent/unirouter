//go:build integration

package contentcapture

import (
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScopedPolicyPGAuthenticatedOwnerSharedQuotaAndRevocation(t *testing.T) {
	pool, store, org, project := setupPolicyStorePG(t)
	ctx := t.Context()
	var sharedKey, personalKey, owner int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,name,key_hash,key_prefix,status)
      VALUES($1,$2,'scoped-shared',repeat('a',64),'scoped-shared','active') RETURNING id`, org, project).Scan(&sharedKey))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,role,organization_role,status)
      VALUES($1,'scoped-owner','synthetic','member','member','active') RETURNING id`, org).Scan(&owner))
	_, err := pool.Exec(ctx, `INSERT INTO project_members(project_id,user_id,role) VALUES($1,$2,'project_user')`, project, owner)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,status)
      VALUES($1,$2,$3,'personal','scoped-personal',repeat('b',64),'scoped-personal','active') RETURNING id`, org, project, owner).Scan(&personalKey))
	full := testFullPolicy()
	fullBytes, err := encodeStoredPolicyConfig(full)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO execution_content_enterprise_policies(org_id,desired_version,effective_version,desired_config,effective_config,
      desired_execution_storage_mode,effective_execution_storage_mode,desired_default_project_quota_bytes,effective_default_project_quota_bytes,
      effective_at,application_status,application_reason)
      VALUES($1,2,2,$2,$2,'temporary_allowed','temporary_allowed',1024,1024,clock_timestamp(),'applied','none')`, org, fullBytes)
	require.NoError(t, err)
	shared := CapturePrincipal{APIKeyID: sharedKey, KeyType: "shared"}
	got, err := store.EffectiveScopedForCapture(ctx, shared, org, project, "ready")
	require.NoError(t, err)
	require.Equal(t, PolicyScopeEnterprise, got.Scope)
	require.Equal(t, "2", got.Effective.PolicyVersion)
	require.Equal(t, int64(1024), got.ProjectQuotaBytes)
	require.Equal(t, "content-policy:"+strconv.FormatInt(org, 10)+":enterprise", got.PolicyRef)

	disabled := testMetadataPolicy()
	disabled.Mode = ModeDisabled
	disabledBytes, err := encodeStoredPolicyConfig(disabled)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO execution_content_policies(org_id,project_id,desired_version,effective_version,desired_config,effective_config,
      effective_at,application_status,application_reason,project_quota_bytes,desired_execution_storage_mode,effective_execution_storage_mode)
      VALUES($1,$2,1,1,$3,$3,clock_timestamp(),'applied','none',2048,'strict_no_persist','strict_no_persist')`, org, project, disabledBytes)
	require.NoError(t, err)
	userFull := testFullPolicy()
	userFull.QuotaBytes = nil
	userBytes, err := encodeStoredPolicyConfig(userFull)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO execution_content_user_policies(org_id,project_id,user_id,desired_version,effective_version,desired_config,effective_config,
      desired_execution_storage_mode,effective_execution_storage_mode,effective_at,application_status,application_reason)
      VALUES($1,$2,$3,3,3,$4,$4,'temporary_allowed','temporary_allowed',clock_timestamp(),'applied','none')`, org, project, owner, userBytes)
	require.NoError(t, err)
	got, err = store.EffectiveScopedForCapture(ctx, shared, org, project, "ready")
	require.NoError(t, err)
	require.Equal(t, PolicyScopeProject, got.Scope)
	require.Equal(t, ExecutionStorageStrict, got.ExecutionStorageMode)
	personal := CapturePrincipal{APIKeyID: personalKey, KeyType: "personal", OwnerUserID: owner}
	got, err = store.EffectiveScopedForCapture(ctx, personal, org, project, "ready")
	require.NoError(t, err)
	require.Equal(t, PolicyScopeUser, got.Scope)
	require.Equal(t, int64(2048), *got.Effective.Config.QuotaBytes)
	require.Equal(t, ExecutionStorageTemporary, got.ExecutionStorageMode)
	_, err = pool.Exec(ctx, `DELETE FROM project_members WHERE project_id=$1 AND user_id=$2`, project, owner)
	require.NoError(t, err)
	_, err = store.EffectiveScopedForCapture(ctx, personal, org, project, "ready")
	require.True(t, errors.Is(err, ErrPolicyInactive), "revoked personal membership: %v", err)
}
