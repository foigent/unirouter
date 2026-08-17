//go:build integration

package runtimechange

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestListPageDistinguishesCurrentStableFromRetainedActiveRecords(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	var orgID, userID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO orgs (name,status,org_type)
		VALUES ('runtime-list-page','active','customer') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,role,status,organization_role)
		VALUES ($1,'runtime-list-page-owner','test-hash','org_admin','active','org_admin') RETURNING id`, orgID).Scan(&userID))

	insertRelease := func(ref, stage string) int64 {
		t.Helper()
		var id int64
		var validatedBy, activatedBy *int64
		var validatedAt, activatedAt, rolledBackAt *time.Time
		now := time.Now()
		if stage == "active" || stage == "rolled_back" {
			validatedBy = &userID
			validatedAt = &now
		}
		if stage == "active" {
			activatedBy = &userID
			activatedAt = &now
		}
		if stage == "rolled_back" {
			rolledBackAt = &now
		}
		query := `
			INSERT INTO runtime_change_releases (
			  release_ref,model_name,candidate_version,candidate_hash,baseline_hash,
			  candidate,provider_pack_pins,simulation_report,overall_risk,stage,version,
			  reason,created_by,validated_by,activated_by,validated_at,activated_at,rolled_back_at)
			VALUES (
			  $1,'runtime-list-model','unirouter.runtime-change-candidate.v1',
			  'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
			  'sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
			  '{"contract_version":"unirouter.runtime-change-candidate.v1","model_name":"runtime-list-model","overrides":[]}',
			  '[]','{}','unchanged',$2,1,'runtime list fixture',$3,$4,$5,$6,$7,$8)
			RETURNING id`
		require.NoError(t, pool.QueryRow(ctx, query, ref, stage, userID, validatedBy, activatedBy, validatedAt, activatedAt, rolledBackAt).Scan(&id))
		return id
	}

	currentID := insertRelease("rcr_runtime_list_current", "active")
	insertRelease("rcr_runtime_list_retained", "active")
	insertRelease("rcr_runtime_list_finished", "rolled_back")
	_, err := pool.Exec(ctx, `
		INSERT INTO runtime_change_slots (model_name,stable_release_id,slot_version)
		VALUES ('runtime-list-model',$1,3)`, currentID)
	require.NoError(t, err)

	manager := &Manager{db: pool}
	page, err := manager.ListPage(ctx, "runtime-list-model", 2, 1)
	require.NoError(t, err)
	require.Equal(t, 3, page.Total)
	require.Equal(t, 2, page.Summary.ActiveRecords)
	require.Equal(t, 1, page.Summary.CurrentStable)
	require.Equal(t, 1, page.Summary.RetainedActive)
	require.Equal(t, 1, page.Summary.History)
	require.Len(t, page.Data, 1)
	require.Equal(t, 2, page.Page)
	require.Equal(t, 1, page.PageSize)
}
