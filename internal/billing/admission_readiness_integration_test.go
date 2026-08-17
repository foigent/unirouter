//go:build integration

package billing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdmissionReadinessUsesFreezePricingAndSpendableBalanceWithoutMutation(t *testing.T) {
	pool, orgID, model := newReservationFixture(t, 10, 100)
	ctx := t.Context()
	_, err := pool.Exec(ctx, `UPDATE wallets SET frozen_micros=$1 WHERE org_id=$2`, testMicros(500), orgID)
	require.NoError(t, err)
	var expiredGrantID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO grants (org_id,amount_micros,balance_micros,status,expires_at,reason,created_by)
		VALUES ($1,$2,$2,'active',NOW()-INTERVAL '1 second','readiness-expired',0)
		RETURNING id`, orgID, testMicros(80)).Scan(&expiredGrantID))

	result, err := NewBilling(pool, nil).InspectAdmissionReadiness(ctx, orgID, model)
	require.NoError(t, err)
	require.Equal(t, BillingModePrepaidEnforced, result.BillingMode)
	require.Equal(t, RequestPricingAuthorityLegacy, result.PricingAuthority)
	require.True(t, result.PriceConfigured)
	require.Regexp(t, `^pv3_[a-f0-9]{64}$`, result.PricingVersion)
	require.True(t, result.WalletAdmissionRequired)
	require.True(t, result.WalletExists)
	require.Equal(t, testMicros(30), result.SpendableBalanceMicros,
		"frozen is already deducted and must not be subtracted twice; expired grant credit is excluded")

	var walletGranted, walletFrozen, grantBalance int64
	var grantStatus string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT granted_balance_micros,frozen_micros FROM wallets WHERE org_id=$1`, orgID).
		Scan(&walletGranted, &walletFrozen))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT balance_micros,status FROM grants WHERE id=$1`, expiredGrantID).
		Scan(&grantBalance, &grantStatus))
	require.Equal(t, testMicros(100), walletGranted)
	require.Equal(t, testMicros(500), walletFrozen)
	require.Equal(t, testMicros(80), grantBalance)
	require.Equal(t, "active", grantStatus, "readiness must not perform lazy grant expiry")
}

func TestAdmissionReadinessCostObservationDoesNotRequireWallet(t *testing.T) {
	pool, orgID, _ := newReservationFixture(t, 0, 0)
	result, err := NewBilling(pool, nil).
		WithMode(BillingModeCostObservation).
		InspectAdmissionReadiness(t.Context(), orgID, "unpriced-readiness-model")
	require.NoError(t, err)
	require.False(t, result.PriceConfigured)
	require.Regexp(t, `^uo1_[a-f0-9]{64}$`, result.PricingVersion)
	require.False(t, result.WalletAdmissionRequired)
	require.False(t, result.WalletExists)
	require.Zero(t, result.SpendableBalanceMicros)
}
