//go:build integration

package xinsightconnector

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestCompletedRedeemOriginRequiresActualTransportAndRejectsHistoricalPromotionIntegration(t *testing.T) {
	for _, historical := range []bool{false, true} {
		t.Run(map[bool]string{false: "actual-origin", true: "historical-originless"}[historical], func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			ctx := context.Background()
			journal, _ := setupRedeemInstallation(t, pool)
			prepared, err := journal.PrepareRedeem(ctx, cp2aInstallationID)
			require.NoError(t, err)
			result := cp2aRegistrationResult(t, prepared, cp2aControlProject)
			readOrigin := func(deployment string) (string, error) {
				tx, err := pool.Begin(ctx)
				require.NoError(t, err)
				defer tx.Rollback(ctx)
				return ReadCompletedRedeemOriginTx(ctx, tx, cp2aInstallationID, deployment)
			}
			_, err = readOrigin(prepared.XDeploymentID)
			require.ErrorIs(t, err, ErrConflict)
			origin := "https://control.example.test"
			if historical {
				require.NoError(t, journal.FinalizeRedeem(ctx, prepared, result))
				require.ErrorIs(t, journal.FinalizeRedeemFromOrigin(ctx, prepared, result, origin), ErrConflict)
				_, err = pool.Exec(ctx, `UPDATE xinsight_connector_control_bindings SET redeemed_origin=$2 WHERE installation_id=$1`, cp2aInstallationID, origin)
				require.ErrorContains(t, err, "cannot upgrade historical authority")
				_, err = readOrigin(prepared.XDeploymentID)
				require.ErrorIs(t, err, ErrConflict)
				return
			}
			require.NoError(t, journal.FinalizeRedeemFromOrigin(ctx, prepared, result, origin))
			require.NoError(t, journal.FinalizeRedeemFromOrigin(ctx, prepared, result, origin))
			actual, err := readOrigin(prepared.XDeploymentID)
			require.NoError(t, err)
			require.Equal(t, origin, actual)
			require.ErrorIs(t, journal.FinalizeRedeemFromOrigin(ctx, prepared, result, "https://other.example"), ErrConflict)
			_, err = readOrigin("xid_AAAAAAAAAAAAAAAAAAAAAA")
			require.ErrorIs(t, err, ErrConflict)
			_, err = pool.Exec(ctx, `UPDATE xinsight_connector_installations SET desired_state='paused' WHERE installation_id=$1`, cp2aInstallationID)
			require.NoError(t, err)
			_, err = readOrigin(prepared.XDeploymentID)
			require.ErrorIs(t, err, ErrConflict)
		})
	}
}
