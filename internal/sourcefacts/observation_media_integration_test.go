//go:build integration

package sourcefacts

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/mediajob"
)

func TestSourceMediaOriginalDispatchFailureAndCommittedProjectionIsolation(t *testing.T) {
	pool, s, org, project := setupSourceDB(t)
	s.options.ObservationsEnabled = true
	require.NoError(t, s.CheckReady(t.Context()))
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	var key int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,name,key_hash,key_prefix,status,allowed_models) VALUES($1,$2,'synthetic-media-b',repeat('c',64),'ur-media-b','active','company-image') RETURNING id`, org, project).Scan(&key))
	store, err := mediajob.NewPostgresStore(pool, mediajob.ExecutionModelAuthorizerFunc(func(context.Context, db.DBQuerier, mediajob.ExecutionAuthorization) error { return nil }))
	require.NoError(t, err)
	store.SetSourceMaterializer(s)
	scope := mediajob.Scope{OrgID: org, ProjectID: project}
	create := func(suffix string) mediajob.Job {
		job, reused, err := store.CreateOrGet(ctx, mediajob.CreateRecord{AllowCreate: true, ExpectedLimitSubject: limitpolicy.Subject{OrgID: org, ProjectID: project, APIKeyID: key, Model: "company-image", BaseRPSLimit: 10, BaseRPMLimit: 60}, Job: mediajob.Job{
			ContractVersion: mediajob.ContractVersion, JobID: "mediajob_" + strings.Repeat(suffix, 32), Scope: scope, APIKeyID: key, RequestID: "source-media-b-" + suffix, Operation: mediajob.OperationTextToImage, ProfileID: endpointprotocol.ProfileOpenAIImageGPT, RequestedModel: "company-image", RoutingModel: "company-image", RequestSHA256: "sha256:" + strings.Repeat(suffix, 64), IdempotencyHash: "hmac-sha256:" + strings.Repeat(suffix, 64), RequestCiphertext: "synthetic-ciphertext", Lifecycle: mediajob.Lifecycle{Admission: mediajob.AdmissionAccepted, Queue: mediajob.QueueQueued, Lease: mediajob.LeaseUnleased, Execution: mediajob.ExecutionPending, Outcome: mediajob.OutcomePending, Cancellation: mediajob.CancellationNotRequested, Object: mediajob.ObjectPending, Settlement: mediajob.SettlementNotEligible}, MaxAttempts: 3, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}})
		require.NoError(t, err)
		require.False(t, reused)
		return job
	}
	claim := func(job mediajob.Job) mediajob.Lease {
		claimed, err := store.Claim(ctx, "synthetic-media-b-owner", now.Add(time.Second), time.Minute)
		require.NoError(t, err)
		require.NotNil(t, claimed)
		require.Equal(t, job.JobID, claimed.JobID)
		return mediajob.Lease{JobID: claimed.JobID, Scope: scope, Owner: claimed.LeaseOwner, Fence: claimed.LeaseFence, Job: *claimed}
	}
	dispatch := func(lease mediajob.Lease) error {
		subject, err := store.ExecutionLimitSubject(ctx, lease)
		require.NoError(t, err)
		operation := "synthetic-b-" + lease.Job.RequestID
		_, _, err = store.AttachExecutionReservation(ctx, lease, subject, &limitpolicy.DurableReservation{OperationID: operation}, operation, now.Add(time.Hour), now.Add(2*time.Second))
		return err
	}
	document := func(request, kind string) Observation {
		var wire []byte
		require.NoError(t, pool.QueryRow(ctx, `SELECT document FROM execution_source_facts WHERE request_id=$1 AND stage=$2`, request, kind).Scan(&wire))
		doc, err := DecodeObservation(wire)
		require.NoError(t, err)
		_, err = ObservationEvent(doc, wire)
		require.NoError(t, err)
		return doc
	}
	first := create("a")
	lease := claim(first)
	_, err = pool.Exec(ctx, `CREATE FUNCTION reject_media_b_source() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.stage='media_attempt_registered' THEN RAISE EXCEPTION 'synthetic required media source failure';END IF;RETURN NEW;END$$;CREATE TRIGGER reject_media_b_source BEFORE INSERT ON execution_source_facts FOR EACH ROW EXECUTE FUNCTION reject_media_b_source()`)
	require.NoError(t, err)
	require.ErrorContains(t, dispatch(lease), "synthetic required media source failure")
	unchanged, err := store.Get(ctx, scope, first.JobID)
	require.NoError(t, err)
	require.Zero(t, unchanged.Attempt)
	require.Equal(t, mediajob.ExecutionPending, unchanged.Lifecycle.Execution)
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts WHERE request_id=$1 AND stage IN ('dispatch','media_running','media_attempt_registered')`, first.RequestID).Scan(&count))
	require.Zero(t, count)
	_, err = pool.Exec(ctx, `DROP TRIGGER reject_media_b_source ON execution_source_facts;DROP FUNCTION reject_media_b_source()`)
	require.NoError(t, err)
	require.NoError(t, dispatch(lease))
	registered := document(first.RequestID, "media_attempt_registered")
	require.Equal(t, "1", registered.Attempt.RouteSequence)
	require.Equal(t, "registered", registered.Attempt.Provider)
	require.Equal(t, "admitted", document(first.RequestID, "media_running").Runtime.Admission)
	require.NoError(t, store.MarkProviderSucceeded(ctx, lease, "gpt-image-1", now.Add(3*time.Second)))
	require.Equal(t, "accepted", document(first.RequestID, "media_provider_succeeded").Attempt.Provider)
	_, err = s.DeleteExpired(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.NoError(t, store.MarkProviderSucceeded(ctx, lease, "gpt-image-1", now.Add(4*time.Second)))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts WHERE request_id=$1`, first.RequestID).Scan(&count))
	require.Zero(t, count, "original success replay cannot rebuild retired pages")
	second := create("b")
	lease = claim(second)
	require.NoError(t, dispatch(lease))
	_, err = pool.Exec(ctx, `CREATE FUNCTION reject_optional_media_source() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.stage IN ('media_result','media_terminal') THEN RAISE EXCEPTION 'synthetic optional media source failure';END IF;RETURN NEW;END$$;CREATE TRIGGER reject_optional_media_source BEFORE INSERT ON execution_source_facts FOR EACH ROW EXECUTE FUNCTION reject_optional_media_source()`)
	require.NoError(t, err)
	terminal, err := store.CompleteUnknown(ctx, lease, "synthetic-original-unknown", now.Add(5*time.Second))
	require.NoError(t, err)
	require.True(t, terminal.Terminal())
	require.Equal(t, mediajob.OutcomeUnknown, terminal.Lifecycle.Outcome)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts WHERE request_id=$1 AND stage='terminal'`, second.RequestID).Scan(&count))
	require.Equal(t, 3, count, "original A terminal remains committed")
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts WHERE request_id=$1 AND stage IN ('media_result','media_terminal')`, second.RequestID).Scan(&count))
	require.Zero(t, count)
	_, err = pool.Exec(ctx, `DROP TRIGGER reject_optional_media_source ON execution_source_facts;DROP FUNCTION reject_optional_media_source()`)
	require.NoError(t, err)
	third := create("c")
	lease = claim(third)
	require.NoError(t, dispatch(lease))
	_, err = store.Claim(ctx, "synthetic-expiry", now.Add(2*time.Minute), time.Minute)
	require.NoError(t, err)
	require.Equal(t, "unknown", document(third.RequestID, "media_terminal").Runtime.Provider)
	require.Equal(t, "unknown", document(third.RequestID, "media_result").Attempt.Provider)
	fourth := create("d")
	lease = claim(fourth)
	_, err = store.RequestCancellation(ctx, scope, fourth.JobID, now.Add(time.Second))
	require.NoError(t, err)
	require.ErrorIs(t, dispatch(lease), mediajob.ErrCancelled)
	require.Equal(t, "not_started", document(fourth.RequestID, "media_terminal").Runtime.Provider)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts WHERE request_id=$1 AND stage='media_attempt_registered'`, fourth.RequestID).Scan(&count))
	require.Zero(t, count, "cancellation at dispatch cannot manufacture a provider attempt")
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM completed_execution_feed_items`).Scan(&count))
	require.Zero(t, count, "media must not synthesize text I01")
}
