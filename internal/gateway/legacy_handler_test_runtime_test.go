package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/uniroute/unirouter/internal/adapters"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/logger"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/routingpolicy"
)

// legacyHandlerTestPlanner keeps historical handler fixtures explicit after
// production Chat and Embeddings stop accepting a missing execution runtime.
// It is compiled only into gateway tests; production has no test flag, field,
// interface assertion, or fallback branch.
type legacyHandlerTestPlanner struct {
	relay *Relay

	mu     sync.Mutex
	routes map[legacyHandlerTestRouteKey]RouteResult
}

type legacyHandlerTestRouteKey struct {
	channelID    int64
	channelKeyID int64
}

func (planner *legacyHandlerTestPlanner) Plan(ctx context.Context, request modelrouting.Request) (modelrouting.Plan, error) {
	if planner == nil || planner.relay == nil || planner.relay.router == nil {
		return modelrouting.Plan{}, fmt.Errorf("legacy handler test planner is unavailable")
	}
	legacyRoutes, err := planner.relay.router.RouteAllWithKey(ctx, request.PublicModel, request.SelectionKey)
	if err != nil {
		return modelrouting.Plan{}, err
	}
	plan := modelrouting.Plan{
		AuthorityMode:        modelrouting.ModeLegacy,
		PublicModel:          request.PublicModel,
		ModelOperationID:     request.ModelOperationID,
		RequiredCapabilities: append([]string(nil), request.RequiredCapabilities...),
		ExecutionProfileID:   modelprofile.UnverifiedProfileID, ExecutionProfileVersion: modelprofile.UnverifiedProfileVersion,
		TokenizerProfileID: modelprofile.TokenizerByteUpperID, TokenizerProfileVersion: modelprofile.TokenizerByteUpperVersion,
		DatabaseTime: time.Now().UTC(),
		Routes:       make([]modelrouting.Route, 0, len(legacyRoutes)),
	}
	profile, ok := modelprofile.Lookup(modelprofile.UnverifiedProfileID, modelprofile.UnverifiedProfileVersion)
	if !ok {
		return modelrouting.Plan{}, fmt.Errorf("unverified execution profile is unavailable")
	}
	plan.ReasoningProfile = profile.ReasoningProfile
	plan.CacheUsageProfile = profile.CacheUsageProfile
	plan.ModelProtocolProfile = profile.ProtocolProfile
	if len(legacyRoutes) > 0 {
		plan.SnapshotHash = legacyRoutes[0].SnapshotVersion
		plan.RuntimeReleaseRef = legacyRoutes[0].RuntimeReleaseRef
		plan.RuntimeStage = legacyRoutes[0].RuntimeStage
		plan.Exclusions = make([]modelrouting.Exclusion, max(legacyRoutes[0].ExcludedCount, 0))
	}
	planner.mu.Lock()
	defer planner.mu.Unlock()
	for index := range legacyRoutes {
		legacy := legacyRoutes[index]
		evidence := executionevidence.EvidenceV1{
			ContractVersion:               executionevidence.ContractVersion,
			AuthorityMode:                 executionevidence.ModeLegacy,
			ExecutionContractSource:       executionevidence.ContractSourceLegacyAdapter,
			ModelOperationID:              request.ModelOperationID,
			LegacyChannelID:               legacy.Channel.ID,
			LegacyChannelKeyID:            legacy.ChannelKey.ID,
			LegacyRoutingRevision:         positiveLegacyTestRevision(legacy.Channel.RoutingEpoch),
			LegacyCircuitRevision:         positiveLegacyTestRevision(legacy.Channel.CircuitEpoch),
			LegacyCredentialStateRevision: positiveLegacyTestRevision(legacy.ChannelKey.StateEpoch),
			RequiresProbe:                 legacy.RequiresProbe,
		}
		if _, normalizeErr := executionevidence.Normalize(evidence); normalizeErr != nil {
			return modelrouting.Plan{}, fmt.Errorf("legacy handler test evidence: %w", normalizeErr)
		}
		plan.Routes = append(plan.Routes, modelrouting.Route{
			ConnectionName:   legacy.Channel.Name,
			Provider:         legacy.Channel.Provider,
			BaseURL:          legacy.Channel.BaseURL,
			AdapterType:      legacy.Channel.AdapterType,
			AdapterConfig:    append([]byte(nil), legacy.Channel.AdapterConfig...),
			UpstreamModel:    legacy.MappedModel,
			MaxContext:       legacy.Channel.MaxContext,
			DefaultMaxOutput: legacy.Channel.DefaultMaxOutput,
			EncryptedSecret:  legacy.ChannelKey.KeyEnc,
			Priority:         legacy.Channel.Priority,
			Weight:           legacy.Channel.RoutingWeight,
			SelectionReason:  legacy.SelectionReason,
			RequiresProbe:    legacy.RequiresProbe,
			Evidence:         evidence,
		})
		planner.routes[legacyHandlerTestRouteKey{channelID: legacy.Channel.ID, channelKeyID: legacy.ChannelKey.ID}] = legacy
	}
	return plan, nil
}

func positiveLegacyTestRevision(value int64) int64 {
	if value > 0 {
		return value
	}
	return 1
}

func (planner *legacyHandlerTestPlanner) route(route modelrouting.Route) (RouteResult, bool) {
	planner.mu.Lock()
	defer planner.mu.Unlock()
	value, ok := planner.routes[legacyHandlerTestRouteKey{
		channelID: route.Evidence.LegacyChannelID, channelKeyID: route.Evidence.LegacyChannelKeyID,
	}]
	return value, ok
}

type legacyHandlerTestAttempt struct {
	route RouteResult
	stop  func()
}

type legacyHandlerTestCoordinator struct {
	relay   *Relay
	planner *legacyHandlerTestPlanner

	mu       sync.Mutex
	attempts map[string]legacyHandlerTestAttempt
}

func (coordinator *legacyHandlerTestCoordinator) Authorize(
	ctx context.Context,
	plan modelrouting.Plan,
	route modelrouting.Route,
	identity modelexecution.AttemptIdentity,
) (modelexecution.Decision, error) {
	legacy, ok := coordinator.planner.route(route)
	if !ok {
		return modelexecution.Decision{Status: modelexecution.Stale}, nil
	}
	prepared, err := coordinator.relay.router.PrepareRoute(ctx, &legacy)
	if err != nil || !prepared {
		return modelexecution.Decision{Status: modelexecution.Stale}, nil
	}
	evidence := route.Evidence
	if evidence.RequiresProbe {
		evidence.ProbeLeaseTokenHash = legacyHandlerTestTokenHash(legacy.ProbeToken)
	}
	if _, err := executionevidence.NormalizeForDispatch(evidence); err != nil {
		return modelexecution.Decision{Status: modelexecution.Stale}, err
	}
	attemptStart := providerAttemptStartFromRoute(
		ctx, identity.RequestID, identity.OrgID, identity.APIKeyID, adapters.Endpoint(identity.Endpoint), &legacy,
	)
	requestLog := logger.New(logger.Fields{RequestID: identity.RequestID, OrgID: uint(identity.OrgID), Module: "legacy_handler_test_runtime"})
	if err := coordinator.relay.beginProviderAttemptWithRetry(ctx, attemptStart, requestLog); err != nil {
		return modelexecution.Decision{Status: modelexecution.Stale}, err
	}
	stop := coordinator.relay.router.MaintainProbeLease(ctx, &legacy)
	coordinator.mu.Lock()
	coordinator.attempts[identity.AttemptID] = legacyHandlerTestAttempt{route: legacy, stop: stop}
	coordinator.mu.Unlock()
	evidenceHash, err := executionevidence.Digest(evidence)
	if err != nil {
		stop()
		return modelexecution.Decision{Status: modelexecution.Stale}, err
	}
	return modelexecution.Decision{
		Status:          modelexecution.Applied,
		Evidence:        evidence,
		EvidenceHash:    evidenceHash,
		ProbeLeaseToken: legacy.ProbeToken,
		DatabaseTime:    time.Now().UTC(),
		ProviderAttemptKey: modelexecution.ProviderAttemptKey{
			OrgID: identity.OrgID, RequestID: identity.RequestID, AttemptID: identity.AttemptID,
		},
	}, nil
}

func (coordinator *legacyHandlerTestCoordinator) RenewLease(context.Context, modelexecution.Decision) (modelexecution.HealthResult, error) {
	return modelexecution.HealthResult{Status: modelexecution.Applied, DatabaseTime: time.Now().UTC()}, nil
}

func (coordinator *legacyHandlerTestCoordinator) ReleaseUndispatched(_ context.Context, decision modelexecution.Decision) (modelexecution.HealthResult, error) {
	coordinator.finishAttempt(decision)
	return modelexecution.HealthResult{Status: modelexecution.Applied, DatabaseTime: time.Now().UTC()}, nil
}

func (coordinator *legacyHandlerTestCoordinator) MarkSuccess(ctx context.Context, decision modelexecution.Decision) (modelexecution.HealthResult, error) {
	return coordinator.applyHealth(ctx, decision, executionHealthSuccess)
}

func (coordinator *legacyHandlerTestCoordinator) MarkProviderFailure(ctx context.Context, decision modelexecution.Decision) (modelexecution.HealthResult, error) {
	return coordinator.applyHealth(ctx, decision, executionHealthProviderFailure)
}

func (coordinator *legacyHandlerTestCoordinator) MarkCredentialAuthFailure(ctx context.Context, decision modelexecution.Decision) (modelexecution.HealthResult, error) {
	return coordinator.applyHealth(ctx, decision, executionHealthCredentialAuthFailure)
}

func (coordinator *legacyHandlerTestCoordinator) MarkCredentialRateLimited(ctx context.Context, decision modelexecution.Decision) (modelexecution.HealthResult, error) {
	return coordinator.applyHealth(ctx, decision, executionHealthCredentialRateLimited)
}

func (coordinator *legacyHandlerTestCoordinator) applyHealth(
	ctx context.Context,
	decision modelexecution.Decision,
	action executionHealthAction,
) (modelexecution.HealthResult, error) {
	attempt, ok := coordinator.finishAttempt(decision)
	if !ok {
		return modelexecution.HealthResult{Status: modelexecution.Stale}, nil
	}
	var err error
	switch action {
	case executionHealthSuccess:
		err = coordinator.relay.router.MarkRouteSuccess(ctx, &attempt.route)
	case executionHealthProviderFailure:
		err = coordinator.relay.router.MarkRouteFailure(ctx, attempt.route)
	case executionHealthCredentialAuthFailure:
		err = coordinator.relay.router.MarkRouteKeyAuthFailure(ctx, attempt.route)
	case executionHealthCredentialRateLimited:
		err = coordinator.relay.router.MarkRouteKeyRateLimited(ctx, attempt.route)
	default:
		err = fmt.Errorf("legacy handler test health action is unsupported")
	}
	if err != nil {
		return modelexecution.HealthResult{Status: modelexecution.Stale}, err
	}
	return modelexecution.HealthResult{Status: modelexecution.Applied, DatabaseTime: time.Now().UTC()}, nil
}

func (coordinator *legacyHandlerTestCoordinator) finishAttempt(decision modelexecution.Decision) (legacyHandlerTestAttempt, bool) {
	attemptID := decision.ProviderAttemptKey.AttemptID
	coordinator.mu.Lock()
	attempt, ok := coordinator.attempts[attemptID]
	if ok {
		delete(coordinator.attempts, attemptID)
	}
	coordinator.mu.Unlock()
	if ok && attempt.stop != nil {
		attempt.stop()
	}
	return attempt, ok
}

func legacyHandlerTestTokenHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func installLegacyHandlerTestRuntime(relay *Relay) {
	if relay == nil {
		panic("legacy handler test runtime requires relay")
	}
	if relay.router == nil {
		relay.router = NewRouter(relay.db)
	}
	planner := &legacyHandlerTestPlanner{relay: relay, routes: make(map[legacyHandlerTestRouteKey]RouteResult)}
	coordinator := &legacyHandlerTestCoordinator{
		relay: relay, planner: planner, attempts: make(map[string]legacyHandlerTestAttempt),
	}
	policy := routingpolicy.Default()
	if relay.router != nil {
		policy = relay.router.Policy()
	}
	if err := relay.SetModelExecutionRuntime(modelrouting.ModeLegacy, planner, coordinator, policy); err != nil {
		panic(fmt.Sprintf("install legacy handler test runtime: %v", err))
	}
}
