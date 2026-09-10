//go:build integration

package sourcefacts

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/mediajob"
)

func TestSourceRuntimeCurrentUsesOriginalStreamFencesAndDurableResults(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	b := billing.NewBilling(pool, nil).WithMode(billing.BillingModeCostObservation)
	ctx := t.Context()
	var key int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,name,key_hash,key_prefix,status,allowed_models) VALUES($1,$2,'synthetic-runtime',repeat('b',64),'ur-runtime','active','synthetic-unpriced') RETURNING id`, org, project).Scan(&key))
	scope := access{org: org, project: project, keyID: 1, generation: 1}
	freeze := func(request string) {
		r, err := b.Freeze(ctx, billing.FreezeRequest{RequestID: request, OrgID: org, ProjectID: project, APIKeyID: key, Model: "synthetic-unpriced", EstimatedTokens: 16, SafetyFactor: "1"})
		require.NoError(t, err)
		require.True(t, r.Success)
	}
	stream := func(request string) billing.StreamDeliveryStart {
		freeze(request)
		attempt := request + "-attempt"
		require.NoError(t, b.BeginProviderAttempt(ctx, billing.ProviderAttemptStart{RequestID: request, AttemptID: attempt, OrgID: org, APIKeyID: key, ChannelID: 201, ChannelKeyID: 301, RouteSequence: 1, RouteTotal: 1, Endpoint: "/v1/chat/completions", Provider: "synthetic", Channel: "synthetic"}))
		require.NoError(t, b.CompleteProviderAttempt(ctx, billing.ProviderAttemptCompletion{RequestID: request, AttemptID: attempt, OrgID: org, Outcome: billing.ProviderAttemptOutcomeResponseAccepted, HTTPStatus: 200}))
		start := billing.StreamDeliveryStart{RequestID: request, AttemptID: attempt, OwnerID: "synthetic-current-owner", OrgID: org, Generation: 1, LeaseTTL: time.Minute}
		require.NoError(t, b.BeginStreamDelivery(ctx, start))
		return start
	}
	read := func(request string, cursor currentRuntimeCursor, cutoff time.Time, scope access) []CurrentRuntimeEntry {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx)
		cursor.RequestFilter = request
		entries, err := s.readCurrentRuntimeTx(ctx, tx, scope, cursor, cutoff, 50)
		require.NoError(t, err)
		require.NoError(t, tx.Commit(ctx))
		return entries
	}
	active := stream("active-cross-day")
	_, err := pool.Exec(ctx, `UPDATE freeze_idempotency SET created_at=NOW()-INTERVAL '2 days' WHERE org_id=$1 AND request_id=$2`, org, active.RequestID)
	require.NoError(t, err)
	cp, err := b.CheckpointStreamDelivery(ctx, billing.StreamDeliveryCheckpoint{RequestID: active.RequestID, AttemptID: active.AttemptID, OwnerID: active.OwnerID, OrgID: org, Generation: 1, DeliveredBytes: 16, DeliveredChunks: 1, PromptTokens: 2, CompletionTokens: 1, PolicySetVersion: "synthetic-v1", LeaseTTL: time.Minute})
	require.NoError(t, err)
	require.Equal(t, 1, cp.Sequence)
	completed := stream("completed-left-lease")
	fence := completed.Fence()
	require.NoError(t, b.RecordDelivery(ctx, billing.DeliveryEvent{RequestID: completed.RequestID, AttemptID: completed.AttemptID, OrgID: org, Mode: billing.DeliveryModeStream, Outcome: billing.DeliveryOutcomeCompleted, DeliveredBytes: 16, DeliveredChunks: 1, HTTPStatus: 200, StreamFence: &fence}))
	var state string
	require.NoError(t, pool.QueryRow(ctx, `SELECT state FROM stream_delivery_leases WHERE org_id=$1 AND request_id=$2`, org, completed.RequestID).Scan(&state))
	require.Equal(t, "active", state, "original receipt does not retire the lease")
	mismatch := stream("generation-mismatch")
	_, err = pool.Exec(ctx, `UPDATE stream_delivery_leases SET generation=generation+1 WHERE org_id=$1 AND request_id=$2`, org, mismatch.RequestID)
	require.NoError(t, err)
	expired := stream("expired-not-recovered")
	_, err = pool.Exec(ctx, `UPDATE stream_delivery_leases SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE org_id=$1 AND request_id=$2`, org, expired.RequestID)
	require.NoError(t, err)
	terminal := stream("terminal-lease")
	require.NoError(t, b.FinishStreamDelivery(ctx, terminal, "interrupted", "synthetic-terminal"))
	freeze("ordinary-sync")
	// A live routing probe protects a breaker transition. It does not provide
	// an execution lease or prove that an authenticated request is still running.
	_, err = pool.Exec(ctx, `INSERT INTO channels(id,name,provider,base_url,models,circuit_state,half_open_until,half_open_probe_until,half_open_probe_token)
	 VALUES(201,'synthetic-live-probe','synthetic','https://provider.invalid','synthetic-unpriced','half_open',NOW()-INTERVAL '1 second',NOW()+INTERVAL '1 minute','synthetic-probe-owner')`)
	require.NoError(t, err)
	freeze("probe-only-sync")
	require.NoError(t, b.BeginProviderAttempt(ctx, billing.ProviderAttemptStart{RequestID: "probe-only-sync", AttemptID: "probe-only-attempt", OrgID: org, APIKeyID: key, ChannelID: 201, ChannelKeyID: 301, RouteSequence: 1, RouteTotal: 1, Endpoint: "/v1/chat/completions", Provider: "synthetic", Channel: "synthetic-live-probe"}))
	now := time.Now()
	entries := read("", currentRuntimeCursor{}, now, scope)
	require.Len(t, entries, 1)
	require.Equal(t, active.RequestID, entries[0].RequestID)
	require.Equal(t, "lease_valid_execution", entries[0].Freshness)
	require.NotEmpty(t, entries[0].CheckpointRef)
	require.NotEmpty(t, entries[0].ValidUntil)
	require.NotContains(t, entries[0].EvidenceRef, active.OwnerID)
	// Every page is a current observation. A cursor after the only live item must
	// not switch to last_known merely because its live row precedes the cursor.
	after := currentRuntimeCursor{AfterRequest: active.RequestID, AfterFamily: "stream", AfterSubject: active.AttemptID}
	require.Empty(t, read(active.RequestID, after, now, scope))
	require.Empty(t, read("", currentRuntimeCursor{}, now, access{org: org, project: project + 999}))
	for _, request := range []string{completed.RequestID, mismatch.RequestID, expired.RequestID, terminal.RequestID, "ordinary-sync", "probe-only-sync"} {
		items := read(request, currentRuntimeCursor{}, now, scope)
		require.Len(t, items, 1, request)
		require.Equal(t, "last_known", items[0].Freshness, request)
		require.Empty(t, items[0].ValidUntil, request)
	}
	require.Empty(t, read("missing-request", currentRuntimeCursor{}, now, scope))
	// No worker heartbeat or cleanup is needed for expiry to stop asserting liveness.
	require.Empty(t, read("", currentRuntimeCursor{}, now.Add(2*time.Minute), scope))
}

func TestSourceRuntimeCurrentDistinguishesOriginalMediaClaimAndRunning(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	var key int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,name,key_hash,key_prefix,status,allowed_models) VALUES($1,$2,'synthetic-current',repeat('a',64),'ur-runtime-test','active','company-image') RETURNING id`, org, project).Scan(&key))
	store, err := mediajob.NewPostgresStore(pool, mediajob.ExecutionModelAuthorizerFunc(func(context.Context, db.DBQuerier, mediajob.ExecutionAuthorization) error { return nil }))
	require.NoError(t, err)
	scope := mediajob.Scope{OrgID: org, ProjectID: project}
	create := func(suffix string) mediajob.Job {
		job, reused, err := store.CreateOrGet(ctx, mediajob.CreateRecord{AllowCreate: true, ExpectedLimitSubject: limitpolicy.Subject{OrgID: org, ProjectID: project, APIKeyID: key, Model: "company-image", BaseRPSLimit: 10, BaseRPMLimit: 60}, Job: mediajob.Job{
			ContractVersion: mediajob.ContractVersion, JobID: "mediajob_" + strings.Repeat(suffix, 32), Scope: scope, APIKeyID: key, RequestID: "media-current-" + suffix, Operation: mediajob.OperationTextToImage, ProfileID: endpointprotocol.ProfileOpenAIImageGPT, RequestedModel: "company-image", RoutingModel: "company-image", RequestSHA256: "sha256:" + strings.Repeat(suffix, 64), IdempotencyHash: "hmac-sha256:" + strings.Repeat(suffix, 64), RequestCiphertext: "synthetic-ciphertext",
			Lifecycle: mediajob.Lifecycle{Admission: mediajob.AdmissionAccepted, Queue: mediajob.QueueQueued, Lease: mediajob.LeaseUnleased, Execution: mediajob.ExecutionPending, Outcome: mediajob.OutcomePending, Cancellation: mediajob.CancellationNotRequested, Object: mediajob.ObjectPending, Settlement: mediajob.SettlementNotEligible}, MaxAttempts: 3, NextAttemptAt: now.Add(-24 * time.Hour), CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now.Add(-24 * time.Hour)}})
		require.NoError(t, err)
		require.False(t, reused)
		return job
	}
	read := func(cutoff time.Time) []CurrentRuntimeEntry {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx)
		items, err := s.readCurrentRuntimeTx(ctx, tx, access{org: org, project: project}, currentRuntimeCursor{}, cutoff, 50)
		require.NoError(t, err)
		return items
	}
	first := create("a")
	require.Empty(t, read(now))
	claimed, err := store.Claim(ctx, "synthetic-media-current-owner", now, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, first.JobID, claimed.JobID)
	items := read(now)
	require.Len(t, items, 1)
	require.Equal(t, "leased_pending", items[0].Freshness)
	lease := mediajob.Lease{JobID: claimed.JobID, Scope: scope, Owner: claimed.LeaseOwner, Fence: claimed.LeaseFence, Job: *claimed}
	subject, err := store.ExecutionLimitSubject(ctx, lease)
	require.NoError(t, err)
	operation := "synthetic-media-runtime-" + first.RequestID
	_, reused, err := store.AttachExecutionReservation(ctx, lease, subject, &limitpolicy.DurableReservation{OperationID: operation}, operation, now.Add(time.Hour), now.Add(time.Second))
	require.NoError(t, err)
	require.False(t, reused)
	items = read(now.Add(2 * time.Second))
	require.Len(t, items, 1)
	require.Equal(t, "lease_valid_execution", items[0].Freshness)
	require.Equal(t, "running", items[0].OriginalState)
	require.Empty(t, read(now.Add(2*time.Minute)))
	require.NoError(t, store.MarkProviderSucceeded(ctx, lease, "gpt-image-1", now.Add(3*time.Second)))
	require.Empty(t, read(now.Add(4*time.Second)), "succeeded provider is not currently running even before object persistence")
}
