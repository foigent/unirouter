package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/executionprojection"
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestPrepareTraceMaterializationRejectsProviderEventWithoutExactAttemptID(t *testing.T) {
	relay := &Relay{db: &testutil.MockDB{}}
	_, err := relay.prepareTraceMaterialization(context.Background(), traceOutboxPayload{
		OrgID: 1, RequestID: "precontract-provider-event", EventType: "provider.dispatch_started",
		Stage: "provider", SourceEventHash: strings.Repeat("a", 64),
	})
	if !errors.Is(err, executionprojection.ErrExpectedAttemptMissing) {
		t.Fatalf("incomplete provider event must fail closed, got %v", err)
	}
}

func TestPrepareTraceMaterializationPersistsPreDispatchCorrelationWithoutAttemptLookup(t *testing.T) {
	db := &testutil.MockDB{
		ExecutionProjectionFn: func(context.Context, executionprojection.ResolveInput) (executionprojection.Projection, error) {
			t.Fatal("pre-dispatch trace must not query the provider-attempt ledger")
			return executionprojection.Projection{}, nil
		},
	}
	relay := &Relay{db: db}
	events := []string{
		"provider.key_decrypt_failed",
		"provider.adapter_unknown",
		"provider.request_build_failed",
		"routing.probe_claim_failed",
		"routing.route_selected",
		"billing.provider_attempt.begin_failed",
	}
	for _, eventType := range events {
		t.Run(eventType, func(t *testing.T) {
			attemptID := "att_pre_dispatch"
			prepared, err := relay.prepareTraceMaterialization(context.Background(), traceOutboxPayload{
				OrgID: 1, RequestID: "pre-dispatch", AttemptID: attemptID,
				ProviderAttemptStage: string(providerAttemptStagePreDispatch),
				EventType:            eventType, SourceEventHash: strings.Repeat("c", 64),
			})
			if err != nil {
				t.Fatal(err)
			}
			if prepared.AttemptID != attemptID {
				t.Fatalf("attempt correlation ID = %q, want %q", prepared.AttemptID, attemptID)
			}
			if prepared.RouteProjection.Source != executionprojection.SourceNoProviderAttempt ||
				prepared.RouteProjection.Compact != nil || prepared.RouteProjection.LegacyChannelID != 0 {
				t.Fatalf("pre-dispatch event manufactured provider evidence: %+v", prepared.RouteProjection)
			}
		})
	}
}

func TestPrepareTraceMaterializationRejectsDurableProviderEventsWithoutReceipt(t *testing.T) {
	db := &testutil.MockDB{
		ExecutionProjectionFn: func(context.Context, executionprojection.ResolveInput) (executionprojection.Projection, error) {
			return executionprojection.Projection{}, executionprojection.ErrExpectedAttemptMissing
		},
	}
	relay := &Relay{db: db}
	for _, eventType := range []string{"provider.dispatch_started", "provider.response_accepted"} {
		t.Run(eventType, func(t *testing.T) {
			_, err := relay.prepareTraceMaterialization(context.Background(), traceOutboxPayload{
				OrgID: 1, RequestID: "missing-receipt", AttemptID: "att_missing_receipt",
				ProviderAttemptStage: string(providerAttemptStageDurable),
				EventType:            eventType, SourceEventHash: strings.Repeat("d", 64),
			})
			if !errors.Is(err, executionprojection.ErrExpectedAttemptMissing) {
				t.Fatalf("durable provider event must fail closed without its receipt, got %v", err)
			}
		})
	}
}

func TestPrepareTraceMaterializationUsesOnlyExplicitNoAttemptSources(t *testing.T) {
	relay := &Relay{db: &testutil.MockDB{}}
	for _, test := range []struct {
		name, eventType, stage string
		source                 executionprojection.Source
	}{
		{name: "response cache", eventType: "response_cache.hit", stage: "cache", source: executionprojection.SourceResponseCache},
		{name: "local terminal", eventType: "auth.model_not_allowed", stage: "auth", source: executionprojection.SourceNoProviderAttempt},
	} {
		t.Run(test.name, func(t *testing.T) {
			prepared, err := relay.prepareTraceMaterialization(context.Background(), traceOutboxPayload{
				OrgID: 1, RequestID: "explicit-no-attempt", EventType: test.eventType,
				Stage: test.stage, SourceEventHash: strings.Repeat("b", 64),
			})
			if err != nil {
				t.Fatal(err)
			}
			if prepared.RouteProjection.Source != test.source {
				t.Fatalf("source = %q, want %q", prepared.RouteProjection.Source, test.source)
			}
			if prepared.AttemptID != "" || prepared.RouteProjection.Compact != nil || prepared.RouteProjection.LegacyChannelID != 0 {
				t.Fatalf("no-attempt source manufactured provider identity: %+v", prepared.RouteProjection)
			}
		})
	}
}

func TestDurableContextProjectionEliminatesRepeatedAttemptQueriesAndLegacyReplayStaysExact(t *testing.T) {
	resolverCalls := 0
	db := &testutil.MockDB{
		ExecutionProjectionFn: func(_ context.Context, input executionprojection.ResolveInput) (executionprojection.Projection, error) {
			resolverCalls++
			if !input.AttemptRequired || input.AttemptID != "att_hot_path" {
				t.Fatalf("unexpected exact replay lookup: %+v", input)
			}
			return executionprojection.Projection{
				Source:          executionprojection.SourcePrecontractProviderAttempt,
				LegacyChannelID: 7,
			}, nil
		},
	}
	relay := &Relay{db: db}
	ctx := context.WithValue(context.Background(), middleware.CtxOrgID, int64(1))
	ctx = context.WithValue(ctx, middleware.CtxRequestID, "req_hot_path")
	ctx = contextWithPreDispatchProviderAttempt(ctx, "att_hot_path")
	ctx = contextWithDurableLegacyProviderAttempt(ctx, 1, "req_hot_path", 7)

	for index, eventType := range []string{
		"provider.response_accepted", "usage.provider_missing", "logging.call_log.recorded",
	} {
		prepared, err := relay.prepareTraceMaterialization(ctx, traceOutboxPayload{
			OrgID: 1, RequestID: "req_hot_path", AttemptID: "att_hot_path", ChannelID: 7,
			ProviderAttemptStage: string(providerAttemptStageDurable), EventType: eventType,
			SourceEventHash: strings.Repeat(string(rune('a'+index)), 64),
		})
		if err != nil {
			t.Fatal(err)
		}
		if prepared.RouteProjection.Source != executionprojection.SourcePrecontractProviderAttempt ||
			prepared.RouteProjection.LegacyChannelID != 7 {
			t.Fatalf("trace lost context projection: %+v", prepared.RouteProjection)
		}
	}
	callLog, err := relay.prepareCallLogMaterialization(ctx, callLogOutboxPayload{
		OrgID: 1, RequestID: "req_hot_path", AttemptID: "att_hot_path", ChannelID: 7,
		UsageSource: metering.UsageSourceUpstream, SourceEventHash: strings.Repeat("d", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	if callLog.RouteProjection.Source != executionprojection.SourcePrecontractProviderAttempt || resolverCalls != 0 {
		t.Fatalf("hot path projection=%+v resolver_calls=%d, want context projection and zero queries", callLog.RouteProjection, resolverCalls)
	}

	// An old, unprepared outbox envelope has no trusted in-process projection;
	// it must still resolve the exact immutable attempt rather than downgrade.
	replayed, err := relay.prepareTraceMaterialization(context.Background(), traceOutboxPayload{
		OrgID: 1, RequestID: "req_hot_path", AttemptID: "att_hot_path", ChannelID: 7,
		ProviderAttemptStage: string(providerAttemptStageDurable), EventType: "provider.response_accepted",
		SourceEventHash: strings.Repeat("e", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.RouteProjection.Source != executionprojection.SourcePrecontractProviderAttempt || resolverCalls != 1 {
		t.Fatalf("legacy replay projection=%+v resolver_calls=%d, want one exact lookup", replayed.RouteProjection, resolverCalls)
	}
}

func TestContextWithCoordinatorExecutionProjectionBindsCanonicalAndVerifyIdentity(t *testing.T) {
	base := context.WithValue(context.Background(), middleware.CtxOrgID, int64(11))
	base = context.WithValue(base, middleware.CtxRequestID, "req_coordinator_projection")
	for _, mode := range []executionevidence.AuthorityMode{
		executionevidence.ModeCanonical,
		executionevidence.ModeVerify,
	} {
		t.Run(string(mode), func(t *testing.T) {
			compact := executionevidence.CompactProjection{
				ContractVersion: executionevidence.ContractVersion,
				AuthorityMode:   mode, EvidenceHash: "sha256:" + strings.Repeat("a", 64),
				PublicModelID: 1, DeploymentID: 2, ConnectionID: 3, CredentialBindingID: 4,
				ModelOperationID: "chat.completions.v1", RuntimeReleaseID: 5,
				SnapshotHash: "mrs1_" + strings.Repeat("b", 64),
			}
			if mode == executionevidence.ModeVerify {
				compact.LegacyChannelID = 7
			}
			ctx, err := ContextWithCoordinatorExecutionProjection(
				base, 11, "req_coordinator_projection", "att_coordinator_projection", compact,
			)
			if err != nil {
				t.Fatal(err)
			}
			projection, ok := executionProjectionFromContext(
				ctx, 11, "req_coordinator_projection", "att_coordinator_projection",
			)
			if !ok || projection.Compact == nil || *projection.Compact != compact ||
				providerAttemptStageFromContext(ctx) != providerAttemptStageDurable {
				t.Fatalf("coordinator compact projection was not preserved: %+v", projection)
			}
		})
	}

	compact := executionevidence.CompactProjection{
		ContractVersion: executionevidence.ContractVersion,
		AuthorityMode:   executionevidence.ModeCanonical,
		EvidenceHash:    "sha256:" + strings.Repeat("c", 64),
		PublicModelID:   1, DeploymentID: 2, ConnectionID: 3, CredentialBindingID: 4,
		ModelOperationID: "chat.completions.v1", RuntimeReleaseID: 5,
		SnapshotHash: "mrs1_" + strings.Repeat("d", 64),
	}
	if _, err := ContextWithCoordinatorExecutionProjection(
		base, 12, "req_coordinator_projection", "att_coordinator_projection", compact,
	); err == nil {
		t.Fatal("cross-organization context projection must be rejected")
	}
}

func TestTraceMaterializationChannelProjection(t *testing.T) {
	for _, kind := range []string{"no_provider_attempt", "response_cache", "precontract_provider_attempt", "legacy", "verify", "canonical"} {
		t.Run(kind, func(t *testing.T) {
			projection := executionprojection.Projection{Source: executionprojection.Source(kind)}
			var expected any
			switch kind {
			case "precontract_provider_attempt":
				projection.LegacyChannelID = 7
				expected = int64(7)
			case "legacy", "verify", "canonical":
				compact := &executionevidence.CompactProjection{ContractVersion: executionevidence.ContractVersion, AuthorityMode: executionevidence.AuthorityMode(kind), EvidenceHash: "sha256:" + strings.Repeat("a", 64), ModelOperationID: "openai.chat_completions.v1"}
				if kind != "canonical" {
					compact.LegacyChannelID = 7
					expected = int64(7)
				}
				if kind != "legacy" {
					compact.PublicModelID = 1
					compact.DeploymentID = 2
					compact.ConnectionID = 3
					compact.CredentialBindingID = 4
					compact.RuntimeReleaseID = 5
					compact.SnapshotHash = "mrs1_" + strings.Repeat("b", 64)
				}
				projection = executionprojection.Projection{Source: executionprojection.SourceProviderAttempt, Compact: compact}
			}
			var actual []any
			relay := &Relay{db: &testutil.MockDB{ExecFn: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
				actual = args
				return pgconn.CommandTag{}, nil
			}}}
			err := relay.materializeTrace(t.Context(), traceOutboxPayload{RequestID: "trace-projection", OrgID: 1, AttemptID: "att_correlation", Provider: "routing-provider", Channel: "routing-channel", ChannelID: 7, RouteProjection: projection, Metadata: []byte("{}"), SourceEventHash: strings.Repeat("c", 64)}, "test-event")
			require.NoError(t, err)
			require.Len(t, actual, 57)
			require.Equal(t, expected, actual[56], "trace execution channel must follow the projection")
			require.Equal(t, "routing-provider", actual[13])
			require.Equal(t, "routing-channel", actual[14])
			require.Equal(t, "att_correlation", actual[36])
			if kind == "no_provider_attempt" || kind == "response_cache" {
				shared, err := projectionValues(projection, 7)
				require.NoError(t, err)
				require.EqualValues(t, 7, shared.LegacyChannelID, "shared call-log behavior is unchanged")
			}
			if kind == "canonical" {
				require.Equal(t, int64(2), actual[46])
				require.Equal(t, string(executionprojection.SourceProviderAttempt), actual[41])
			}
		})
	}
}
