package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/guardrail"
)

// startGuardrailRuntime keeps high-risk safety initialization and lifecycle
// outside main assembly. Startup requires a readable policy source and valid
// platform scope; tenant scopes publish or retain their snapshots independently.
func startGuardrailRuntime(pool *pgxpool.Pool, sensitive *filter.DFAFilter, fingerprintSecret string, options guardrail.Options) (*guardrail.Service, context.CancelFunc, error) {
	service, err := guardrail.NewService(pool, sensitive, []byte(fingerprintSecret), options)
	if err != nil {
		return nil, nil, err
	}
	initialCtx, cancelInitial := context.WithTimeout(context.Background(), 10*time.Second)
	err = service.Refresh(initialCtx)
	cancelInitial()
	if err != nil {
		return nil, nil, fmt.Errorf("load initial policy snapshot: %w", err)
	}
	refreshCtx, stop := context.WithCancel(context.Background())
	go service.RunRefreshLoop(refreshCtx)
	return service, stop, nil
}
