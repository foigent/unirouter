package middleware

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestP6F40CAffectedKeyPaidAdmissionBlockClearsOnlyAfterResolution(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := t.Context()
	var orgID, apiKeyID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name,status,org_type) VALUES ($1,'active','standard') RETURNING id`,
		fmt.Sprintf("p6f40c-auth-%d", time.Now().UnixNano())).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO api_keys (org_id,name,key_prefix,key_hash,status) VALUES ($1,'blocked-key','ur-test','hash','active') RETURNING id`,
		orgID).Scan(&apiKeyID))
	_, err := pool.Exec(ctx, `INSERT INTO wallets (org_id,balance,granted_balance,frozen,total_spent) VALUES ($1,1000,0,0,0)`, orgID)
	require.NoError(t, err)
	model := fmt.Sprintf("p6f40c-auth-model-%d", time.Now().UnixNano())
	_, err = pool.Exec(ctx, `INSERT INTO model_prices (model_name,provider,input_price_cents,output_price_cents) VALUES ($1,'test',2,10)`, model)
	require.NoError(t, err)
	b := billing.NewBilling(pool, nil)
	requestID := "op_p6f40c_auth"
	freeze, err := b.Freeze(ctx, billing.FreezeRequest{RequestID: requestID, OrgID: orgID, Model: model, EstimatedTokens: 1, SafetyFactor: 1, MinFreezeAmount: 100})
	require.NoError(t, err)
	require.True(t, freeze.Success)
	attempt := billing.ProviderAttemptStart{
		RequestID: requestID, AttemptID: "att_p6f40c_auth", OrgID: orgID, APIKeyID: apiKeyID,
		ChannelID: 1, ChannelKeyID: 1, RouteSequence: 1, RouteTotal: 1,
		Endpoint: "/v1/chat/completions", Provider: "test", Channel: "test",
	}
	require.NoError(t, b.BeginProviderAttempt(ctx, attempt))
	blocked, err := providerCostAdmissionBlocked(ctx, pool, orgID, apiKeyID)
	require.NoError(t, err)
	require.False(t, blocked, "an ordinary in-flight dispatch must not serialize the entire API key")
	require.NoError(t, b.CompleteProviderAttempt(ctx, billing.ProviderAttemptCompletion{
		RequestID: requestID, AttemptID: attempt.AttemptID, OrgID: orgID,
		Outcome: billing.ProviderAttemptOutcomeUnknown, FailureKind: "transport",
	}))
	blocked, err = providerCostAdmissionBlocked(ctx, pool, orgID, apiKeyID)
	require.NoError(t, err)
	require.True(t, blocked)
	_, err = b.ReconcileProviderAttempt(ctx, billing.ProviderReconciliationRequest{
		OrgID: orgID, RequestID: requestID, AttemptID: attempt.AttemptID,
		Action: billing.ProviderReconciliationRefund, ActorUserID: 77,
		Reason: "provider confirmed no customer delivery", EvidenceRef: "case://refund-auth",
	})
	require.NoError(t, err)
	blocked, err = providerCostAdmissionBlocked(ctx, pool, orgID, apiKeyID)
	require.NoError(t, err)
	require.False(t, blocked)
}
