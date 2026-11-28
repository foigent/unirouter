//go:build integration

package sourcefacts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/mediajob"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestSourceMediaOriginalDispatchFailureAndCommittedProjectionIsolation(t *testing.T) {
	pool, s, org, project := setupSourceDBWithPool(t, testutil.SetupMigratedThroughTestDB(t, schemaauthority.OpenAIResponsesToolsHead))
	ctx := t.Context()
	dir := testutil.MigrationsDir(t)
	s.options.ObservationsEnabled = true
	legacyProfiles := DefaultEnrollmentProfiles(EnabledProfiles(s))
	require.Len(t, legacyProfiles, 8)
	_, err := pool.Exec(ctx, `INSERT INTO execution_source_connections(org_id,project_id,source_instance,status,profiles) VALUES($1,$2,$3,'active',$4)`, org, project, s.SourceInstance(), legacyProfiles)
	require.NoError(t, err)
	receiver := contentBodyPGFixture{pool: pool, source: s, org: org, project: project}
	oldKey, oldBearer := insertContentBodyPGKey(t, &receiver, "a", "legacy", "feed_active", 0)
	_, err = pool.Exec(ctx, `INSERT INTO execution_source_streams(source_instance,org_id,project_id,profile) VALUES($1,$2,$3,$4)`, s.SourceInstance(), org, project, MediaChargeProfile)
	require.ErrorContains(t, err, "execution_source_streams_profile_check")
	manifest := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
	wire, err := manifest.CanonicalBytes()
	require.NoError(t, err)
	manifestDigest, err := manifest.Digest()
	require.NoError(t, err)
	artifact, instance := "sha256:"+strings.Repeat("b", 64), "uri_"+strings.Repeat("b", 32)
	_, err = pool.Exec(ctx, `INSERT INTO runtime_support_manifests(artifact_digest,manifest_digest,canonical_bytes) VALUES($1,$2,$3)`, artifact, manifestDigest, wire)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO runtime_generations(instance_id,artifact_digest,compatibility_epoch,schema_head,state,ingress_state,lease_expires_at)
		VALUES($1,$2,$3,$4,'serving','receiving',clock_timestamp()+interval '1 hour')`, instance, artifact, runtimegeneration.CompatibilityEpoch, schemaauthority.OpenAIResponsesToolsHead)
	require.NoError(t, err)
	require.ErrorContains(t, migrate.RunExistingThrough(ctx, pool, dir, schemaauthority.MediaChargeSourceHead), "M133 requires stopped writers")
	_, err = pool.Exec(ctx, `UPDATE runtime_generations SET state='stopped',ingress_state='absent',revision=revision+1 WHERE instance_id=$1`, instance)
	require.NoError(t, err)
	require.NoError(t, migrate.RunExistingThrough(ctx, pool, dir, schemaauthority.MediaChargeSourceHead))
	require.NoError(t, s.CheckReady(ctx))
	var migratedProfiles []string
	require.NoError(t, pool.QueryRow(ctx, `SELECT profiles FROM execution_source_connections WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&migratedProfiles))
	require.Equal(t, legacyProfiles, migratedProfiles)
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
	extendedProfiles := append(slices.Clone(legacyProfiles), MediaChargeProfile)
	slices.Sort(extendedProfiles)
	require.True(t, s.ProfilesEnabled(extendedProfiles))
	const route = "/api/v1/integrations/xinsight/source/v2/"
	read := func(bearer, path, profile, cursor string, status int) []byte {
		t.Helper()
		query := url.Values{}
		if profile != "" {
			query.Set("profile", profile)
		}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		req := httptest.NewRequest(http.MethodGet, route+path+"?"+query.Encode(), nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Authorization", "Bearer "+bearer)
		w := httptest.NewRecorder()
		s.ObservationHandler(middleware.ClientIPResolver{}).ServeHTTP(w, req)
		require.Equal(t, status, w.Code, "%s", w.Body.Bytes())
		return w.Body.Bytes()
	}
	read(oldBearer, "events", MediaChargeProfile, "", http.StatusForbidden)
	bearer := "ur-xf1-" + strings.Repeat("b", 64)
	hash, prefix, err := auth.HashReceiverFeedCredential(bearer, s.options.CursorSecret)
	require.NoError(t, err)
	verifier, err := auth.ReceiverFeedVerifierHex(bearer)
	require.NoError(t, err)
	reg := Registration{SchemaVersion: RegistrationVersion, TokenFormat: auth.ReceiverFeedTokenFormat,
		VerifierScheme: auth.ReceiverFeedVerifierScheme, KeyPrefix: prefix, VerifierSHA256: verifier,
		OperationID: strings.Repeat("b", 32), SourceInstance: s.SourceInstance(),
		OrgID: strconv.FormatInt(org, 10), ProjectID: strconv.FormatInt(project, 10), Profiles: extendedProfiles}
	digest, err := reg.Hash()
	require.NoError(t, err)
	var receiverKey int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,key_type,name,key_hash,key_prefix,status,rpm_limit,qps_limit,budget_limit,allowed_ips,allowed_models,policy_labels,purpose,verifier_scheme,registration_operation_id,source_lifecycle_scope)
	 VALUES($1,$2,'service','Synthetic media audit',$3,$4,'feed_staged',60,1,0,'127.0.0.1/32','','',$5,$6,$7,'legacy') RETURNING id`,
		org, project, hash, prefix, Purpose, reg.VerifierScheme, reg.OperationID).Scan(&receiverKey))
	_, err = pool.Exec(ctx, `INSERT INTO execution_source_credentials(api_key_id,org_id,project_id,registration_sha256,profiles,source_lifecycle_scope) VALUES($1,$2,$3,$4,$5,'legacy')`, receiverKey, org, project, digest, extendedProfiles)
	require.NoError(t, err)
	read(bearer, "events", MediaChargeProfile, "", http.StatusForbidden)
	_, err = pool.Exec(ctx, `UPDATE execution_source_connections SET profiles=$3 WHERE org_id=$1 AND project_id=$2`, org, project, extendedProfiles)
	require.NoError(t, err)
	read(oldBearer, "events", MediaChargeProfile, "", http.StatusForbidden)
	_, err = pool.Exec(ctx, `UPDATE api_keys SET status='feed_revoked' WHERE id=$1`, oldKey)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE api_keys SET status='feed_active' WHERE id=$1`, receiverKey)
	require.NoError(t, err)
	read(oldBearer, "events", MediaChargeProfile, "", http.StatusUnauthorized)
	capabilities := read(bearer, "capabilities", "", "", http.StatusOK)
	var capBody struct {
		Profiles []string `json:"profiles"`
	}
	require.NoError(t, json.Unmarshal(capabilities, &capBody))
	require.Contains(t, capBody.Profiles, MediaChargeProfile)
	require.Contains(t, capBody.Profiles, RuntimeProfile)
	type page struct {
		NextCursor string `json:"next_cursor"`
		Events     []struct {
			Payload json.RawMessage `json:"payload"`
		} `json:"events"`
	}
	parsePage := func(wire []byte) page {
		t.Helper()
		var p page
		require.NoError(t, json.Unmarshal(wire, &p))
		require.NotEmpty(t, p.NextCursor)
		return p
	}
	success := create("e")
	lease = claim(success)
	require.NoError(t, dispatch(lease))
	require.NoError(t, store.MarkProviderSucceeded(ctx, lease, "gpt-image-2.6-flare", now.Add(3*time.Second)))
	runtimeBootstrap := read(bearer, "events", RuntimeProfile, "", http.StatusOK)
	chargeBootstrap := read(bearer, "events", MediaChargeProfile, "", http.StatusOK)
	runtimeCursor, chargeCursor := parsePage(runtimeBootstrap).NextCursor, parsePage(chargeBootstrap).NextCursor
	outcome := endpointprotocol.Outcome{ContractVersion: endpointprotocol.ContractVersion, ProfileID: endpointprotocol.ProfileOpenAIImageGPT, ProviderState: "completed", MappedModel: "gpt-image-2.6-flare", OutputCount: 1}
	for _, unit := range []struct {
		class    endpointprotocol.UnitClass
		quantity string
	}{
		{endpointprotocol.UnitInputTokens, "50"}, {endpointprotocol.UnitInputTextTokens, "10"},
		{endpointprotocol.UnitInputImageTokens, "40"}, {endpointprotocol.UnitOutputImageTokens, "50"},
	} {
		outcome.Units = append(outcome.Units, endpointprotocol.UnitObservation{
			Class: unit.class, Quantity: unit.quantity, Authority: "provider_reported",
			Finality: "final", ChargeState: endpointprotocol.ChargeObservedOnly,
		})
	}
	result := mediajob.PersistedResult{ObjectKey: "synthetic-object", ReceiptKey: "synthetic-receipt",
		ContentSHA256: "sha256:" + strings.Repeat("d", 64), MediaType: "image/png", SizeBytes: 128,
		PersistedAt: now.Add(4 * time.Second), MappedModel: outcome.MappedModel, Outcome: outcome}
	_, err = pool.Exec(ctx, `CREATE FUNCTION reject_media_charge_source() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.stage='media_charge' THEN RAISE EXCEPTION 'synthetic media charge transaction failure'; END IF; RETURN NEW; END$$; CREATE TRIGGER reject_media_charge_source BEFORE INSERT ON execution_source_facts FOR EACH ROW EXECUTE FUNCTION reject_media_charge_source()`)
	require.NoError(t, err)
	_, err = store.CompleteSuccess(ctx, lease, result, mediajob.DefaultRetention, now.Add(5*time.Second))
	require.ErrorContains(t, err, "synthetic media charge transaction failure")
	unchanged, err = store.Get(ctx, scope, success.JobID)
	require.NoError(t, err)
	require.False(t, unchanged.Terminal())
	require.Nil(t, unchanged.ChargeFact)
	require.Empty(t, parsePage(read(bearer, "events", MediaChargeProfile, chargeCursor, http.StatusOK)).Events)
	require.Empty(t, parsePage(read(bearer, "events", RuntimeProfile, runtimeCursor, http.StatusOK)).Events)
	_, err = pool.Exec(ctx, `DROP TRIGGER reject_media_charge_source ON execution_source_facts; DROP FUNCTION reject_media_charge_source()`)
	require.NoError(t, err)
	success, err = store.CompleteSuccess(ctx, lease, result, mediajob.DefaultRetention, now.Add(5*time.Second))
	require.NoError(t, err)
	require.NotNil(t, success.ChargeFact)
	_, err = store.CompleteSuccess(ctx, lease, result, mediajob.DefaultRetention, now.Add(5*time.Second))
	require.ErrorIs(t, err, mediajob.ErrLeaseLost)
	read(bearer, "events", MediaChargeProfile, runtimeCursor, http.StatusBadRequest)
	runtimeWire := read(bearer, "events", RuntimeProfile, runtimeCursor, http.StatusOK)
	chargeWire := read(bearer, "events", MediaChargeProfile, chargeCursor, http.StatusOK)
	runtimePage, chargePage := parsePage(runtimeWire), parsePage(chargeWire)
	require.Len(t, runtimePage.Events, 1)
	require.Len(t, chargePage.Events, 1)
	var term, charge Observation
	require.NoError(t, json.Unmarshal(runtimePage.Events[0].Payload, &term))
	require.NoError(t, json.Unmarshal(chargePage.Events[0].Payload, &charge))
	require.NoError(t, term.Validate())
	require.NoError(t, charge.Validate())
	require.Equal(t, "media_terminal", term.Kind)
	require.Equal(t, mediaChargeKind, charge.Kind)
	require.Equal(t, success.RequestID, term.RequestID)
	require.Equal(t, term.RequestID, charge.RequestID)
	require.Equal(t, term.SourceInstance, charge.SourceInstance)
	require.Equal(t, term.OrgID, charge.OrgID)
	require.Equal(t, term.ProjectID, charge.ProjectID)
	require.Equal(t, term.SubjectID, charge.SubjectID)
	require.Equal(t, term.OccurredAt, charge.OccurredAt)
	require.Equal(t, success.ChargeFact.FactID, charge.MediaCharge.ChargeFactID)
	require.Equal(t, success.ChargeFact.Units, charge.MediaCharge.Units)
	require.Equal(t, mediajob.ChargeAmountPricingPending, charge.MediaCharge.AmountState)
	require.Nil(t, charge.MediaCharge.CustomerCharge)
	require.Empty(t, parsePage(read(bearer, "events", MediaChargeProfile, chargePage.NextCursor, http.StatusOK)).Events)
	require.Empty(t, parsePage(read(bearer, "events", RuntimeProfile, runtimePage.NextCursor, http.StatusOK)).Events)
	if dir := os.Getenv("UR_MEDIA_HTTP_EVIDENCE_DIR"); dir != "" {
		expected, err := json.MarshalIndent(map[string]any{
			"OrgID": term.OrgID, "ProjectID": term.ProjectID, "Instance": term.SourceInstance,
			"RequestID": term.RequestID, "JobID": term.SubjectID, "TerminalCursor": runtimeCursor,
			"MediaCursor": chargeCursor, "AmountState": charge.MediaCharge.AmountState, "Units": charge.MediaCharge.Units,
		}, "", "  ")
		require.NoError(t, err)
		for name, wire := range map[string][]byte{
			"runtime-capabilities.json": capabilities, "media-capabilities.json": capabilities,
			"runtime-bootstrap.json": runtimeBootstrap, "media-bootstrap.json": chargeBootstrap,
			"runtime-page.json": runtimeWire, "media-page.json": chargeWire, "expectations.json": expected,
		} {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), wire, 0600))
		}
	}
	down, err := os.ReadFile(filepath.Join(dir, schemaauthority.MediaChargeSourceHead+".down.sql"))
	require.NoError(t, err)
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, downErr := tx.Exec(ctx, strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(down)), "BEGIN;"), "COMMIT;"))
	require.NoError(t, tx.Rollback(ctx))
	require.ErrorContains(t, downErr, "rollback would remove live or retained media charge facts or access")
	t.Logf("authenticated v2 pages matched request %s, job %s, four provider-reported units and pricing pending", charge.RequestID, charge.SubjectID)
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM completed_execution_feed_items`).Scan(&count))
	require.Zero(t, count, "media must not synthesize text I01")
}
