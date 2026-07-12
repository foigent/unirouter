package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/terminalhook"
)

func newRuntimeTerminalHooks(
	database db.DBQuerier,
	outboxService *outbox.Service,
	agentRuntime *agentcap.Service,
	policyRuntime *limitpolicy.Service,
) (*terminalhook.Service, error) {
	service, err := terminalhook.New(database, outboxService)
	if err != nil {
		return nil, err
	}
	if err := service.RegisterExecutor(terminalhook.HookAgentAncestor, func(
		ctx context.Context,
		payload json.RawMessage,
		terminal string,
		actualTokens int64,
		actualCostCents int64,
	) error {
		var reservation agentcap.InvocationReservation
		if err := json.Unmarshal(payload, &reservation); err != nil {
			return fmt.Errorf("decode Agent ancestor reservation: %w", err)
		}
		switch terminal {
		case terminalhook.TerminalSettled:
			return agentRuntime.FinalizeInvocation(ctx, &reservation, actualTokens, actualCostCents)
		case terminalhook.TerminalCancelled:
			return agentRuntime.CancelInvocation(ctx, &reservation)
		default:
			return fmt.Errorf("unsupported Agent terminal state %q", terminal)
		}
	}); err != nil {
		return nil, err
	}
	if err := service.RegisterExecutor(terminalhook.HookLimitPolicy, func(
		ctx context.Context,
		payload json.RawMessage,
		terminal string,
		actualTokens int64,
		_ int64,
	) error {
		var snapshot limitpolicy.DurableReservation
		if err := json.Unmarshal(payload, &snapshot); err != nil {
			return fmt.Errorf("decode distributed-policy reservation: %w", err)
		}
		reservation, err := limitpolicy.RestoreDurableReservation(&snapshot)
		if err != nil {
			return err
		}
		switch terminal {
		case terminalhook.TerminalSettled:
			return policyRuntime.Finalize(ctx, reservation, actualTokens)
		case terminalhook.TerminalCancelled:
			return policyRuntime.Cancel(ctx, reservation)
		default:
			return fmt.Errorf("unsupported distributed-policy terminal state %q", terminal)
		}
	}); err != nil {
		return nil, err
	}
	return service, nil
}
