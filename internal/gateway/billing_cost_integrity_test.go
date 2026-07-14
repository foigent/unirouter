package gateway

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/traceutil"
)

func TestP6F40FundingPendingSettlementIsNotCancelled(t *testing.T) {
	cancelCalls := 0
	settleCalls := 0
	b := &mockBilling{
		settleFn: func(context.Context, billing.SettleRequest) (*billing.SettleResult, error) {
			settleCalls++
			return &billing.SettleResult{
				ActualCostMicros:    500,
				UncoveredCostMicros: 400,
				Success:             false,
				ErrorCode:           "insufficient_topup",
			}, nil
		},
		cancelFn: func(context.Context, string, int64) error {
			cancelCalls++
			return nil
		},
	}
	relay := newTestRelay(nil, b)

	outcome, err := relay.settleBillingWithRetry(billing.SettleRequest{
		RequestID: "op_p6f40_gateway_pending",
		OrgID:     7,
	}, logger.New(logger.Fields{}))

	require.ErrorIs(t, err, billing.ErrSettlementFundingPending)
	require.NotNil(t, outcome.Result)
	require.Equal(t, "insufficient_topup", outcome.Result.ErrorCode)
	require.Equal(t, 1, settleCalls, "funding policy failures are not transient retries")
	require.Zero(t, cancelCalls, "post-dispatch funding pending must remain non-cancellable")
}

func TestP6F40SettlementEvidenceIncludesPrepaidTopUp(t *testing.T) {
	usage := adapters.Usage{PromptTokens: 5, UncachedPromptTokens: 5, Source: metering.UsageSourceUpstream}
	result := &billing.SettleResult{ActualCostMicros: 500, ChargedCostMicros: 500, TopUpAmountMicros: 400, Success: true}

	cost := settlementCostExtension(usage, result, result.ActualCostMicros, result.ChargedCostMicros)
	require.Equal(t, "500", cost["actual_cost_micros"])
	require.Equal(t, "500", cost["charged_cost_micros"])
	require.Equal(t, "400", cost["prepaid_topup_micros"])
	require.Equal(t, false, cost["cache_hit"])

	metadata := traceutil.SanitizeMetadata(settlementTraceMetadata(usage, result, result.ActualCostMicros, result.ChargedCostMicros))
	require.Equal(t, "400", metadata["prepaid_topup_micros"])
}
