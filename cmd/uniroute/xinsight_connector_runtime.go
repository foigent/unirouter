package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

const xInsightConnectorRuntimeDiscoveryLimit = 16

type xInsightConnectorRuntimeInstallationSource interface {
	ListRedeemPendingInstallations(
		context.Context,
		int,
	) ([]xinsightconnector.RuntimeInstallationDescriptor, error)
	ListRunnableInstallations(
		context.Context,
		int,
	) ([]xinsightconnector.RuntimeInstallationDescriptor, error)
}

type xInsightConnectorRuntimeRedeemer interface {
	Redeem(context.Context) error
}

type xInsightConnectorRuntimeRedeemerFactory func(
	context.Context,
	xinsightconnector.RuntimeInstallationDescriptor,
) (xInsightConnectorRuntimeRedeemer, error)

type xInsightConnectorRuntimeCounterpartFactory func(
	context.Context,
	xinsightconnector.RuntimeInstallationDescriptor,
) (xinsightconnector.ControlCounterpart, error)

type xInsightConnectorRuntimeAgent interface {
	RunOnce(context.Context) (bool, error)
}

type xInsightConnectorRuntimeAgentFactory func(
	string,
	xinsightconnector.ControlCounterpart,
) (xInsightConnectorRuntimeAgent, error)

type xInsightConnectorRuntimeDependencies struct {
	installations      xInsightConnectorRuntimeInstallationSource
	redeemerFactory    xInsightConnectorRuntimeRedeemerFactory
	counterpartFactory xInsightConnectorRuntimeCounterpartFactory
	agentFactory       xInsightConnectorRuntimeAgentFactory
}

type configuredXInsightReceiverBindingResolver struct {
	config *config.Config
}

func (resolver configuredXInsightReceiverBindingResolver) ResolveXInsightReceiverBinding(
	_ context.Context,
	installationID, deploymentID, bindingSHA256 string,
) (admin.XInsightReceiverBinding, error) {
	if resolver.config == nil {
		return admin.XInsightReceiverBinding{}, xinsightconnector.ErrInvalid
	}
	allowedIP, ok := resolver.config.ResolveXInsightConnectorReceiverBinding(
		installationID, deploymentID, bindingSHA256,
	)
	if !ok {
		return admin.XInsightReceiverBinding{}, xinsightconnector.ErrConflict
	}
	return admin.XInsightReceiverBinding{
		BindingSHA256: bindingSHA256,
		AllowedIP:     allowedIP,
	}, nil
}

var (
	_ xInsightConnectorRuntimeAgent = (*xinsightconnector.Agent)(nil)
)

func newXInsightConnectorTransactionalAgentFactory(
	journal *xinsightconnector.PostgresPhaseJournal,
	executor xinsightconnector.PhaseTxExecutor,
) xInsightConnectorRuntimeAgentFactory {
	return func(
		installationID string,
		counterpart xinsightconnector.ControlCounterpart,
	) (xInsightConnectorRuntimeAgent, error) {
		return xinsightconnector.NewTransactionalAgent(
			installationID, counterpart, journal, executor,
		)
	}
}

func newConfiguredXInsightConnectorRuntimeSupervisor(
	cfg *config.Config,
	dependencies xInsightConnectorRuntimeDependencies,
	logf func(string, ...any),
) (*xInsightConnectorRuntimeSupervisor, error) {
	if cfg == nil {
		return nil, fmt.Errorf("X-Insight Connector runtime configuration is required")
	}
	if !cfg.XInsightConnectorAgentEnabled {
		return newXInsightConnectorRuntimeSupervisor(
			false, 0, nil, nil, nil, nil, logf,
		)
	}
	return newXInsightConnectorRuntimeSupervisor(
		true,
		time.Duration(cfg.XInsightConnectorPollMilliseconds)*time.Millisecond,
		dependencies.installations,
		dependencies.redeemerFactory,
		dependencies.counterpartFactory,
		dependencies.agentFactory,
		logf,
	)
}

func newProductionXInsightConnectorRuntimeSupervisor(
	cfg *config.Config,
	database db.DBQuerier,
	encryptionRoot []byte,
	adminHandler *admin.Handler,
	logf func(string, ...any),
) (*xInsightConnectorRuntimeSupervisor, error) {
	if cfg == nil {
		return nil, fmt.Errorf("X-Insight Connector runtime configuration is required")
	}
	if !cfg.XInsightConnectorAgentEnabled {
		return newConfiguredXInsightConnectorRuntimeSupervisor(
			cfg, xInsightConnectorRuntimeDependencies{}, logf,
		)
	}
	if database == nil || adminHandler == nil {
		return nil, fmt.Errorf("X-Insight Connector runtime product dependencies are required")
	}
	journal, err := xinsightconnector.NewPostgresPhaseJournal(database, encryptionRoot)
	if err != nil {
		return nil, err
	}
	executor, err := admin.NewXInsightConnectorActivationExecutor(
		adminHandler, configuredXInsightReceiverBindingResolver{config: cfg},
	)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{}
	requestTimeout := time.Duration(cfg.XInsightConnectorRequestTimeoutMilliseconds) * time.Millisecond
	newCounterpart := func(
		_ context.Context,
		descriptor xinsightconnector.RuntimeInstallationDescriptor,
	) (*xinsightconnector.HTTPControlCounterpart, error) {
		return xinsightconnector.NewHTTPControlCounterpart(
			cfg.XInsightConnectorControlOrigin,
			descriptor,
			journal,
			httpClient,
			requestTimeout,
		)
	}
	dependencies := xInsightConnectorRuntimeDependencies{
		installations: journal,
		redeemerFactory: func(
			ctx context.Context,
			descriptor xinsightconnector.RuntimeInstallationDescriptor,
		) (xInsightConnectorRuntimeRedeemer, error) {
			return newCounterpart(ctx, descriptor)
		},
		counterpartFactory: func(
			ctx context.Context,
			descriptor xinsightconnector.RuntimeInstallationDescriptor,
		) (xinsightconnector.ControlCounterpart, error) {
			return newCounterpart(ctx, descriptor)
		},
		agentFactory: newXInsightConnectorTransactionalAgentFactory(journal, executor),
	}
	return newConfiguredXInsightConnectorRuntimeSupervisor(cfg, dependencies, logf)
}

// xInsightConnectorRuntimeSupervisor is deliberately a one-lane, same-process
// loop. Every cycle snapshots bounded pending and active batches, redeems
// staged invitations, then executes at most one Poll/ack per pre-existing
// active installation. A fresh redeem cannot cross into Poll in the same cycle.
type xInsightConnectorRuntimeSupervisor struct {
	enabled            bool
	pollInterval       time.Duration
	installations      xInsightConnectorRuntimeInstallationSource
	redeemerFactory    xInsightConnectorRuntimeRedeemerFactory
	counterpartFactory xInsightConnectorRuntimeCounterpartFactory
	agentFactory       xInsightConnectorRuntimeAgentFactory
	logf               func(string, ...any)
}

func newXInsightConnectorRuntimeSupervisor(
	enabled bool,
	pollInterval time.Duration,
	installations xInsightConnectorRuntimeInstallationSource,
	redeemerFactory xInsightConnectorRuntimeRedeemerFactory,
	counterpartFactory xInsightConnectorRuntimeCounterpartFactory,
	agentFactory xInsightConnectorRuntimeAgentFactory,
	logf func(string, ...any),
) (*xInsightConnectorRuntimeSupervisor, error) {
	if !enabled {
		return &xInsightConnectorRuntimeSupervisor{enabled: false}, nil
	}
	if pollInterval <= 0 || installations == nil || redeemerFactory == nil || counterpartFactory == nil || agentFactory == nil {
		return nil, fmt.Errorf("X-Insight Connector runtime dependencies are required")
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &xInsightConnectorRuntimeSupervisor{
		enabled:            true,
		pollInterval:       pollInterval,
		installations:      installations,
		redeemerFactory:    redeemerFactory,
		counterpartFactory: counterpartFactory,
		agentFactory:       agentFactory,
		logf:               logf,
	}, nil
}

func startXInsightConnectorRuntime(
	ctx context.Context,
	supervisor *xInsightConnectorRuntimeSupervisor,
) <-chan struct{} {
	done := make(chan struct{})
	if supervisor == nil || !supervisor.enabled {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		supervisor.run(ctx)
	}()
	return done
}

func (supervisor *xInsightConnectorRuntimeSupervisor) run(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := supervisor.runCycle(ctx); err != nil && ctx.Err() == nil {
				// Connector control availability is product-local health, not a
				// dependency of gateway or global readiness. Keep logs free of
				// tokens, request bodies, receiver registration and installation IDs.
				supervisor.logf(
					"X-Insight Connector control cycle failed; retrying after bounded poll interval",
				)
			}
			timer.Reset(supervisor.pollInterval)
		}
	}
}

func (supervisor *xInsightConnectorRuntimeSupervisor) runCycle(ctx context.Context) error {
	if supervisor == nil || !supervisor.enabled {
		return nil
	}
	// Snapshot runnable first, then pending. A binding finalized by this cycle
	// (or concurrently between the two reads) cannot enter this frozen runnable
	// set and therefore cannot Poll until a later durable discovery pass.
	runnable, runnableErr := supervisor.installations.ListRunnableInstallations(
		ctx, xInsightConnectorRuntimeDiscoveryLimit,
	)
	pending, pendingErr := supervisor.installations.ListRedeemPendingInstallations(
		ctx, xInsightConnectorRuntimeDiscoveryLimit,
	)
	if len(runnable) > xInsightConnectorRuntimeDiscoveryLimit {
		return fmt.Errorf("runnable installation source exceeded bounded limit")
	}
	if len(pending) > xInsightConnectorRuntimeDiscoveryLimit {
		return fmt.Errorf("redeem-pending installation source exceeded bounded limit")
	}
	cycleErrors := make([]error, 0, 4)
	if runnableErr != nil {
		cycleErrors = append(cycleErrors, fmt.Errorf("discover runnable installation: %w", runnableErr))
		runnable = nil
	}
	if pendingErr != nil {
		cycleErrors = append(cycleErrors, fmt.Errorf("discover redeem-pending installation: %w", pendingErr))
		pending = nil
	}

	redeemFailures := 0
	for _, descriptor := range pending {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		redeemer, err := supervisor.redeemerFactory(ctx, descriptor)
		if err == nil && redeemer == nil {
			err = fmt.Errorf("redeem client is unavailable")
		}
		if err == nil {
			err = redeemer.Redeem(ctx)
		}
		if err != nil {
			redeemFailures++
		}
	}
	if redeemFailures != 0 {
		cycleErrors = append(cycleErrors, fmt.Errorf(
			"invitation redeem failed for %d of %d pending installations",
			redeemFailures, len(pending),
		))
	}

	operationFailures := 0
	for _, descriptor := range runnable {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := supervisor.runInstallation(ctx, descriptor); err != nil {
			operationFailures++
		}
	}
	if operationFailures != 0 {
		cycleErrors = append(cycleErrors, fmt.Errorf(
			"control operation failed for %d of %d runnable installations",
			operationFailures, len(runnable),
		))
	}
	return errors.Join(cycleErrors...)
}

func (supervisor *xInsightConnectorRuntimeSupervisor) runInstallation(
	ctx context.Context,
	descriptor xinsightconnector.RuntimeInstallationDescriptor,
) error {
	counterpart, err := supervisor.counterpartFactory(ctx, descriptor)
	if err != nil {
		return fmt.Errorf("initialize control counterpart: %w", err)
	}
	if counterpart == nil {
		return fmt.Errorf("initialize control counterpart: unavailable")
	}
	agent, err := supervisor.agentFactory(descriptor.InstallationID, counterpart)
	if err != nil {
		return fmt.Errorf("initialize transactional agent: %w", err)
	}
	if agent == nil {
		return fmt.Errorf("initialize transactional agent: unavailable")
	}
	if _, err := agent.RunOnce(ctx); err != nil {
		return fmt.Errorf("process control operation: %w", err)
	}
	return nil
}
