//go:build integration

package sourcefacts

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/mediajob"
)

type failingMediaSource struct {
	service *Service
	fail    bool
}

func (m *failingMediaSource) MaterializeMediaStageTx(ctx context.Context, tx pgx.Tx, job mediajob.Job, stage string, snap *executioncontext.Snapshot) error {
	if stage == "terminal" && m.fail {
		return errors.New("synthetic publication failure")
	}
	return m.service.MaterializeMediaStageTx(ctx, tx, job, stage, snap)
}

func TestMediaRealWritersPublishTerminalWithoutInventingI01(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	var key int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,name,key_hash,key_prefix,status,allowed_models) VALUES($1,$2,'synthetic-media',repeat('a',64),'ur-media-test','active','company-image') RETURNING id`, org, project).Scan(&key))
	store, e := mediajob.NewPostgresStore(pool, mediajob.ExecutionModelAuthorizerFunc(func(context.Context, db.DBQuerier, mediajob.ExecutionAuthorization) error { return nil }))
	require.NoError(t, e)
	fault := &failingMediaSource{service: s}
	store.SetSourceMaterializer(fault)
	scope := mediajob.Scope{OrgID: org, ProjectID: project}
	create := func(suffix string) mediajob.Job {
		r := mediajob.CreateRecord{AllowCreate: true, ExpectedLimitSubject: limitpolicy.Subject{OrgID: org, ProjectID: project, APIKeyID: key, Model: "company-image", BaseRPSLimit: 10, BaseRPMLimit: 60}, Job: mediajob.Job{
			ContractVersion: mediajob.ContractVersion, JobID: "mediajob_" + strings.Repeat(suffix, 32), Scope: scope, APIKeyID: key, RequestID: "media-source-" + suffix, Operation: mediajob.OperationTextToImage, ProfileID: endpointprotocol.ProfileOpenAIImageGPT, RequestedModel: "company-image", RoutingModel: "company-image", RequestSHA256: "sha256:" + strings.Repeat(suffix, 64), IdempotencyHash: "hmac-sha256:" + strings.Repeat(suffix, 64), RequestCiphertext: "synthetic-ciphertext", Lifecycle: mediajob.Lifecycle{Admission: mediajob.AdmissionAccepted, Queue: mediajob.QueueQueued, Lease: mediajob.LeaseUnleased, Execution: mediajob.ExecutionPending, Outcome: mediajob.OutcomePending, Cancellation: mediajob.CancellationNotRequested, Object: mediajob.ObjectPending, Settlement: mediajob.SettlementNotEligible}, MaxAttempts: 3, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}}
		job, reused, e := store.CreateOrGet(ctx, r)
		require.NoError(t, e)
		require.False(t, reused)
		_, reused, e = store.CreateOrGet(ctx, r)
		require.NoError(t, e)
		require.True(t, reused)
		return job
	}
	claim := func(job mediajob.Job) mediajob.Lease {
		claimed, e := store.Claim(ctx, "source-media-worker", now.Add(time.Second), time.Minute)
		require.NoError(t, e)
		require.NotNil(t, claimed)
		require.Equal(t, job.JobID, claimed.JobID)
		lease := mediajob.Lease{JobID: claimed.JobID, Scope: scope, Owner: claimed.LeaseOwner, Fence: claimed.LeaseFence, Job: *claimed}
		subject, e := store.ExecutionLimitSubject(ctx, lease)
		require.NoError(t, e)
		operation := "source-media-" + job.RequestID
		_, reused, e := store.AttachExecutionReservation(ctx, lease, subject, &limitpolicy.DurableReservation{OperationID: operation}, operation, now.Add(time.Hour), now.Add(2*time.Second))
		require.NoError(t, e)
		require.False(t, reused)
		return lease
	}
	assertTerminal := func(job mediajob.Job, hasCharge bool) {
		var wire []byte
		require.NoError(t, pool.QueryRow(ctx, `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=$2 AND stage='terminal'`, job.RequestID, CostProfile).Scan(&wire))
		doc, e := Decode(wire)
		require.NoError(t, e)
		_, e = Event(doc, wire)
		require.NoError(t, e)
		require.Equal(t, "async_media_charge_fact", doc.Cost.Reference.Source)
		require.Equal(t, "not_applicable", doc.Cost.Reference.PublicationStatus)
		require.Empty(t, doc.Cost.Reference.SnapshotSHA256)
		require.Empty(t, doc.Cost.Reference.CalculatedAmountPath)
		if hasCharge {
			require.Equal(t, job.ChargeFact.FactID, doc.Cost.Reference.ChargeFactRef)
			require.Equal(t, "pricing_pending_media_rate_card", doc.Cost.MediaAmountState)
		} else {
			require.Empty(t, doc.Cost.Reference.ChargeFactRef)
			require.Equal(t, "not_eligible", doc.Cost.MediaAmountState)
		}
	}
	queued := create("a")
	cancelled, e := store.RequestCancellation(ctx, scope, queued.JobID, now.Add(time.Second))
	require.NoError(t, e)
	assertTerminal(cancelled.Job, false)
	_, e = s.DeleteExpired(ctx, time.Now().Add(time.Hour))
	require.NoError(t, e)
	_, e = store.RequestCancellation(ctx, scope, queued.JobID, now.Add(2*time.Second))
	require.NoError(t, e)
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts WHERE request_id=$1`, queued.RequestID).Scan(&count))
	require.Zero(t, count, "repeat cancellation cannot rematerialize retired terminal pages")
	failed := create("b")
	lease := claim(failed)
	fault.fail = true
	_, e = store.CompleteFailure(ctx, lease, "synthetic-failure", now.Add(3*time.Second))
	require.ErrorContains(t, e, "synthetic publication failure")
	unchanged, e := store.Get(ctx, scope, failed.JobID)
	require.NoError(t, e)
	require.False(t, unchanged.Terminal())
	fault.fail = false
	failed, e = store.CompleteFailure(ctx, lease, "synthetic-failure", now.Add(3*time.Second))
	require.NoError(t, e)
	assertTerminal(failed, false)
	unknown := create("c")
	lease = claim(unknown)
	_, e = store.Claim(ctx, "expiry-worker", now.Add(2*time.Minute), time.Minute)
	require.NoError(t, e)
	unknown, e = store.Get(ctx, scope, unknown.JobID)
	require.NoError(t, e)
	require.Equal(t, "unknown", unknown.Lifecycle.Outcome)
	assertTerminal(unknown, false)
	success := create("d")
	lease = claim(success)
	require.NoError(t, store.MarkProviderSucceeded(ctx, lease, "gpt-image-1", now.Add(3*time.Second)))
	outcome := endpointprotocol.Outcome{ContractVersion: endpointprotocol.ContractVersion, ProfileID: endpointprotocol.ProfileOpenAIImageGPT, ProviderState: "completed", MappedModel: "gpt-image-1", OutputCount: 1, Units: []endpointprotocol.UnitObservation{{Class: endpointprotocol.UnitInputTokens, Quantity: "3", Authority: "provider_reported", Finality: "final", ChargeState: endpointprotocol.ChargeObservedOnly}, {Class: endpointprotocol.UnitInputTextTokens, Quantity: "2", Authority: "provider_reported", Finality: "final", ChargeState: endpointprotocol.ChargeObservedOnly}, {Class: endpointprotocol.UnitInputImageTokens, Quantity: "1", Authority: "provider_reported", Finality: "final", ChargeState: endpointprotocol.ChargeObservedOnly}, {Class: endpointprotocol.UnitOutputImageTokens, Quantity: "4", Authority: "provider_reported", Finality: "final", ChargeState: endpointprotocol.ChargeObservedOnly}}}
	success, e = store.CompleteSuccess(ctx, lease, mediajob.PersistedResult{ObjectKey: "synthetic-object", ReceiptKey: "synthetic-receipt", ContentSHA256: "sha256:" + strings.Repeat("d", 64), MediaType: "image/png", SizeBytes: 128, PersistedAt: now.Add(4 * time.Second), MappedModel: "gpt-image-1", Outcome: outcome}, mediajob.DefaultRetention, now.Add(5*time.Second))
	require.NoError(t, e)
	assertTerminal(success, true)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM completed_execution_feed_items`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts WHERE request_id=$1`, success.RequestID).Scan(&count))
	require.Equal(t, 7, count, "store emits two admission, two first-dispatch and three terminal fragments")

	// Capture may expire while a dispatched job is unresolved. Its original
	// admission seed must still produce a self-contained terminal later.
	late := create("e")
	lease = claim(late)
	var originalSeed []byte
	require.NoError(t, pool.QueryRow(ctx, `SELECT source_context FROM async_media_jobs WHERE job_id=$1`, late.JobID).Scan(&originalSeed))
	_, e = s.DeleteExpired(ctx, time.Now().Add(time.Hour))
	require.NoError(t, e)
	_, e = store.Claim(ctx, "late-expiry-worker", now.Add(8*24*time.Hour), time.Minute)
	require.NoError(t, e)
	late, e = store.Get(ctx, scope, late.JobID)
	require.NoError(t, e)
	require.Equal(t, mediajob.OutcomeUnknown, late.Lifecycle.Outcome)
	assertTerminal(late, false)
	var retainedSeed []byte
	require.NoError(t, pool.QueryRow(ctx, `SELECT source_context FROM async_media_jobs WHERE job_id=$1`, late.JobID).Scan(&retainedSeed))
	require.Equal(t, originalSeed, retainedSeed)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts WHERE request_id=$1`, late.RequestID).Scan(&count))
	require.Equal(t, 3, count)
	var lateWire []byte
	require.NoError(t, pool.QueryRow(ctx, `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=$2`, late.RequestID, ContextProfile).Scan(&lateWire))
	lateDoc, e := Decode(lateWire)
	require.NoError(t, e)
	require.NotNil(t, lateDoc.Core)
	require.Equal(t, executioncontext.Time(late.CreatedAt), lateDoc.Core.CapturedAt)

	// Use the original store's dispatch-time reauthorization after admission;
	// a revoked ordinary key must not gain a dispatch snapshot or attempt.
	queuedAfter := create("f")
	claimed, e := store.Claim(ctx, "revocation-worker", now.Add(time.Second), time.Minute)
	require.NoError(t, e)
	require.NotNil(t, claimed)
	require.Equal(t, queuedAfter.JobID, claimed.JobID)
	lease = mediajob.Lease{JobID: claimed.JobID, Scope: scope, Owner: claimed.LeaseOwner, Fence: claimed.LeaseFence, Job: *claimed}
	subject, e := store.ExecutionLimitSubject(ctx, lease)
	require.NoError(t, e)
	_, e = pool.Exec(ctx, `UPDATE api_keys SET status='disabled',auth_epoch=auth_epoch+1 WHERE id=$1`, key)
	require.NoError(t, e)
	_, _, e = store.AttachExecutionReservation(ctx, lease, subject, &limitpolicy.DurableReservation{OperationID: "source-media-revoked"}, "source-media-revoked", now.Add(time.Hour), now.Add(2*time.Second))
	require.ErrorIs(t, e, mediajob.ErrForbidden)
	var dispatchSeed []byte
	var attempts int
	require.NoError(t, pool.QueryRow(ctx, `SELECT attempt,source_dispatch_context FROM async_media_jobs WHERE job_id=$1`, queuedAfter.JobID).Scan(&attempts, &dispatchSeed))
	require.Zero(t, attempts)
	require.Empty(t, dispatchSeed)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts WHERE request_id=$1`, queuedAfter.RequestID).Scan(&count))
	require.Equal(t, 2, count, "only the historical admitted identity remains")
}
