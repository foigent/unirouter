//go:build integration

package sourcefacts

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/metering"
)

func TestSourceOutputHoldsFollowOriginalCommitWithoutReplayOrMoney(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, failSource := range []bool{false, true} {
			name := map[bool]string{false: "nonstream", true: "stream"}[stream] + map[bool]string{false: "/capture", true: "/source_failure"}[failSource]
			t.Run(name, func(t *testing.T) {
				pool, source, org, project := setupSourceDB(t)
				source.options.ObservationsEnabled = true
				ctx := t.Context()
				b := billing.NewBilling(pool, nil).WithMode(billing.BillingModeCostObservation)
				var key int64
				require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,name,key_hash,key_prefix,status,allowed_models) VALUES($1,$2,'synthetic-hold',repeat('d',64),'ur-hold','active','synthetic-unpriced') RETURNING id`, org, project).Scan(&key))
				request, attempt := "synthetic-output-hold", "synthetic-hold-attempt"
				frozen, err := b.Freeze(ctx, billing.FreezeRequest{RequestID: request, OrgID: org, ProjectID: project, APIKeyID: key, Model: "synthetic-unpriced", EstimatedTokens: 30, ReservedCompletionTokens: 10, SafetyFactor: "1"})
				require.NoError(t, err)
				require.True(t, frozen.Success)
				require.NoError(t, b.BeginProviderAttempt(ctx, billing.ProviderAttemptStart{RequestID: request, AttemptID: attempt, OrgID: org, APIKeyID: key, ChannelID: 201, ChannelKeyID: 301, RouteSequence: 1, RouteTotal: 1, Endpoint: "/v1/chat/completions", Provider: "synthetic", Channel: "synthetic"}))
				require.NoError(t, b.CompleteProviderAttempt(ctx, billing.ProviderAttemptCompletion{RequestID: request, AttemptID: attempt, OrgID: org, Outcome: billing.ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200}))
				b.SetCommittedObservationSink(source)
				usage := billing.SettleRequest{RequestID: request, AttemptID: attempt, OrgID: org, PromptTokens: 20, UncachedPromptTokens: 20, CompletionTokens: 11, UsageSource: metering.UsageSourceUpstream, UsageAuthority: metering.UsageAuthorityProviderReported, UsagePurpose: metering.UsagePurposeProviderEvidence, UsageFinality: metering.UsageFinalityFinal, UsageContractVersion: metering.UsageContractVersion, CacheUsageStatus: "not_reported"}
				start := billing.StreamDeliveryStart{RequestID: request, AttemptID: attempt, OrgID: org, OwnerID: "synthetic-hold-owner", Generation: 1, LeaseTTL: time.Minute}
				if stream {
					require.NoError(t, b.BeginStreamDelivery(ctx, start))
					fence := start.Fence()
					usage.StreamFence = &fence
					var count int
					require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts WHERE request_id=$1 AND stage='delivery_started'`, request).Scan(&count))
					require.Equal(t, 2, count, "actual stream start produces one request and one attempt observation")
				}
				if failSource {
					_, err = pool.Exec(ctx, `CREATE FUNCTION refuse_hold_source() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.stage IN ('output_held','local_delivery') THEN RAISE EXCEPTION 'synthetic optional hold capture failure'; END IF; RETURN NEW; END$$; CREATE TRIGGER refuse_hold_source BEFORE INSERT ON execution_source_facts FOR EACH ROW EXECUTE FUNCTION refuse_hold_source()`)
					require.NoError(t, err)
				}
				hold := func() error {
					if stream {
						return b.HoldStreamOutput(ctx, usage, 128, 1)
					}
					return b.HoldNonStreamOutput(ctx, usage, billing.OutputHoldReasonGuardrailWithheld)
				}
				require.NoError(t, hold())
				var state string
				var charged int64
				require.NoError(t, pool.QueryRow(ctx, `SELECT status,charged_cost_micros FROM freeze_idempotency WHERE request_id=$1`, request).Scan(&state, &charged))
				require.Equal(t, "settlement_pending", state)
				require.Zero(t, charged)
				var facts int
				require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts WHERE request_id=$1 AND stage='output_held'`, request).Scan(&facts))
				if failSource {
					require.Zero(t, facts)
				} else {
					require.Equal(t, 2, facts)
					var wire []byte
					require.NoError(t, pool.QueryRow(ctx, `SELECT document FROM execution_source_facts WHERE request_id=$1 AND stage='output_held' AND profile=$2`, request, RuntimeProfile).Scan(&wire))
					doc, err := DecodeObservation(wire)
					require.NoError(t, err)
					require.Equal(t, "pending", doc.Runtime.Financial)
					require.Equal(t, "response_received", doc.Runtime.Provider)
					require.Equal(t, map[bool]string{false: "not_started", true: "interrupted"}[stream], doc.Runtime.LocalDelivery)
					_, err = source.DeleteExpired(ctx, time.Now().Add(time.Hour))
					require.NoError(t, err)
				}
				require.NoError(t, hold(), "original idempotent hold remains successful")
				require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts WHERE request_id=$1 AND stage='output_held'`, request).Scan(&facts))
				require.Zero(t, facts, "retry neither recreates retired facts nor repairs missed observations")
			})
		}
	}
}
