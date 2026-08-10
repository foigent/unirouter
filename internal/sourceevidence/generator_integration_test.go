//go:build integration

package sourceevidence

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/testutil"
)

const (
	testReleaseVersion = "1.1.8-source-evidence-test"
	testReleaseCommit  = "4907ff0001fc0b47f749a42bbe89a344ee56b529"
	testSourceInstance = "unirouter-source-evidence-integration"
)

func TestGenerateSourceFactsIsBoundedPayloadNeutralAndDatabaseReadOnly(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	orgID, projectID := seedEvidenceProject(t, ctx, pool, "source-evidence-primary")
	otherOrgID, otherProjectID := seedEvidenceProject(t, ctx, pool, "source-evidence-foreign")
	requestID := "req-poc-chat-001"
	createdAt := time.Date(2026, 8, 10, 12, 0, 30, 0, time.UTC)
	seedCallLog(t, ctx, pool, orgID, projectID, requestID, "approved-model", createdAt)
	seedCallLog(t, ctx, pool, otherOrgID, otherProjectID, requestID, "foreign-model", createdAt)
	_, err := pool.Exec(ctx, `
		INSERT INTO request_trace_events (
		    request_id,org_id,project_id,event_type,stage,severity,
		    model,requested_model,provider,channel,status_code,latency_ms,
		    trace_id,span_id,attempt_id,actor_type,actor_ref,on_behalf_of,
		    payload_capture_mode,metadata,created_at
		) VALUES (
		    $1,$2,$3,'route_selected','routing','info',
		    'approved-model','requested-model','provider-a','channel-a',200,8,
		    'trace-001','span-001','attempt-001','api_key','user:source-evidence-person@example.com','api_key:987654',
		    'metadata_only',$4::jsonb,$5
		)`, requestID, orgID, projectID, `{"diagnostic":"raw trace value must not escape","route_sequence":1}`, createdAt)
	require.NoError(t, err)

	_, snapshotBody, snapshotSHA, err := completedfeed.BuildSnapshot(completedfeed.SnapshotInput{
		RequestID:            requestID,
		OrgID:                orgID,
		ProjectID:            projectID,
		TerminalStatus:       completedfeed.TerminalSettled,
		TerminalAvailableAt:  createdAt.Add(time.Second),
		Model:                "approved-model",
		PromptTokens:         4,
		UncachedPromptTokens: 4,
		CompletionTokens:     2,
		UsageSource:          "upstream",
		UsageAuthority:       "provider_reported",
		UsagePurpose:         "customer_charge",
		UsageFinality:        "final",
		UsageContractVersion: "unirouter.usage.v1",
		CacheUsageStatus:     "not_reported",
		CalculatedCostMicros: 1200,
		ChargedCostMicros:    1200,
		PricingVersion:       "pricing-v1",
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO completed_execution_feed_items (
		    event_key,org_id,project_id,request_id,terminal_status,
		    terminal_available_at,revision,snapshot,snapshot_sha256,created_at
		) VALUES ($1,$2,$3,$4,'settled',$5,'1',$6,$7,$5)`,
		"terminal:"+requestID, orgID, projectID, requestID, createdAt.Add(time.Second), []byte(snapshotBody), snapshotSHA)
	require.NoError(t, err)

	requestSet, protocolPin := evidenceInputs(t, ctx, pool, orgID, projectID, requestID)
	before := databaseFingerprint(t, ctx, pool)
	prepared, err := PrepareComponentInputs(ctx, pool, requestSet, protocolPin)
	require.NoError(t, err)
	afterPrepare := databaseFingerprint(t, ctx, pool)
	require.Equal(t, before, afterPrepare, "read-only component preparation changed durable database state")
	require.Equal(t, SourceScopeForRequestSet(requestSet), prepared.SourceScope)
	require.Equal(t, testReleaseCommit, prepared.InstalledRelease.CanonicalCommit)
	require.Equal(t, zeroReceiptEffects(), prepared.ScopeReceipt.Effects)
	require.True(t, prepared.ScopeReceipt.DatabaseReadOnly)
	require.False(t, prepared.ScopeReceipt.TransactionIDAssigned)
	require.Equal(t, "repeatable read", prepared.ScopeReceipt.DatabaseIsolation)

	generation, err := Generate(ctx, pool, requestSet, GenerateOptions{
		ProtocolPin: protocolPin,
		Now:         func() time.Time { return createdAt.Add(2 * time.Second) },
	})
	require.NoError(t, err)
	after := databaseFingerprint(t, ctx, pool)
	require.Equal(t, before, after, "read-only generator changed durable database state")
	protocol, err := ParseProtocolPin(protocolPin)
	require.NoError(t, err)
	require.NoError(t, ValidateGenerationAgainstPrepared(generation, requestSet, protocol, prepared.InstalledRelease, prepared.ScopeReceipt))
	driftedRelease := prepared.InstalledRelease
	driftedRelease.ReleaseSequence++
	require.ErrorContains(t, ValidateGenerationAgainstPrepared(generation, requestSet, protocol, driftedRelease, prepared.ScopeReceipt), "does not match")

	facts := generation.SourceFacts
	require.Equal(t, SourceFactCounts{
		RequestedRequests:  1,
		CallLogs:           1,
		TraceEvents:        1,
		ProviderAttempts:   0,
		CompletedFeedItems: 1,
		CaptureGaps:        1,
		TotalFacts:         4,
	}, facts.Counts)
	require.True(t, facts.Snapshot.ReadOnly)
	require.Equal(t, "repeatable read", facts.Snapshot.Isolation)
	require.False(t, facts.Snapshot.TransactionIDAssigned)
	require.Equal(t, "complete", facts.Completeness.State)
	require.Equal(t, "partial", facts.Completeness.ExecutionEvidenceV2Projection)
	require.False(t, facts.Completeness.RuntimeCloseMarkerAvailable)
	require.Equal(t, SourceSafety{DatabaseMutation: "none"}, facts.Safety)
	require.Equal(t, SourceIdentityAuthority, facts.Source.IdentityAuthority)
	require.Equal(t, SourceRuntimeObservation, facts.Source.RuntimeObservation)
	require.Equal(t, SourceScopeForRequestSet(requestSet), generation.SourceScope)
	require.Equal(t, testReleaseCommit, facts.Release.Commit)
	require.Len(t, generation.CompatibilityMembers, 1)
	require.Equal(t, "partial", generation.CompatibilityMembers[0].Completeness.State)
	require.Contains(t, generation.CompatibilityMembers[0].CaptureGaps, CompatibilityGap{
		Code:   CompatibilityGapRuntimeCloseMarker,
		Stage:  "source",
		Reason: "runtime close marker is unavailable; closed_at is the snapshot cutoff, not a runtime closure claim; expected hashes are limited to persisted facts visible in the frozen repeatable-read snapshot",
	})
	require.Equal(t, "approved-model", facts.Requests[0].CallLog.Model)
	require.NotEqual(t, "foreign-model", facts.Requests[0].CallLog.Model)
	require.Equal(t, "api_key", facts.Requests[0].CallLog.ActorType)
	require.Equal(t, "api_key", facts.Requests[0].TraceEvents[0].ActorType)
	require.Equal(t, "api_key", generation.CompatibilityMembers[0].CallLogs[0].ActorType)
	require.Empty(t, generation.CompatibilityMembers[0].CallLogs[0].ActorRef)
	require.Equal(t, "api_key", generation.CompatibilityMembers[0].TraceEvents[0].ActorType)
	require.Empty(t, generation.CompatibilityMembers[0].TraceEvents[0].ActorRef)
	require.Empty(t, generation.CompatibilityMembers[0].TraceEvents[0].OnBehalfOf)
	require.NotNil(t, facts.FeedRange.MinSequenceID)
	require.Equal(t, facts.FeedRange.MinSequenceID, facts.FeedRange.MaxSequenceID)
	require.Equal(t, "not_part_of_source_facts", facts.FeedRange.CursorClaim)
	require.Equal(t, "not_part_of_source_facts", facts.FeedRange.HighWatermarkClaim)

	encoded, err := CanonicalJSON(facts)
	require.NoError(t, err)
	for _, prohibited := range []string{
		"raw provider error must not escape",
		"203.0.113.99",
		"raw trace value must not escape",
		"user:source-evidence-person@example.com",
		"api_key:987654",
		"version-42",
	} {
		require.NotContains(t, string(encoded), prohibited)
	}
	require.True(t, facts.Requests[0].CallLog.UpstreamErrorSeen)
	require.True(t, facts.Requests[0].CallLog.IPAddressSeen)

	uppercaseCommit := strings.ToUpper(testReleaseCommit)
	_, err = pool.Exec(ctx, `UPDATE release_state SET commit=$1 WHERE singleton=TRUE`, uppercaseCommit)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE release_history SET commit=$1 WHERE release_sequence=8`, uppercaseCommit)
	require.NoError(t, err)
	_, err = Generate(ctx, pool, requestSet, GenerateOptions{ProtocolPin: protocolPin})
	require.ErrorContains(t, err, "canonical lowercase Git identity")
	_, err = pool.Exec(ctx, `UPDATE release_state SET commit=$1 WHERE singleton=TRUE`, testReleaseCommit)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE release_history SET commit=$1 WHERE release_sequence=8`, testReleaseCommit)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE release_state SET manifest='{"drift":true}'::jsonb WHERE singleton=TRUE`)
	require.NoError(t, err)
	_, err = Generate(ctx, pool, requestSet, GenerateOptions{ProtocolPin: protocolPin})
	require.ErrorContains(t, err, "exact release state/history binding")
}

func TestGenerateProviderAttemptCostIsLogicalTenantBoundedAndPackageVerifiable(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	orgID, projectID := seedEvidenceProject(t, ctx, pool, "source-evidence-provider-attempt")
	foreignOrgID, foreignProjectID := seedEvidenceProject(t, ctx, pool, "source-evidence-provider-attempt-foreign")
	requestID := "req-poc-provider-attempt-001"
	createdAt := time.Date(2026, 8, 10, 12, 10, 0, 0, time.UTC)
	inScope := seedLogicalProviderAttempt(t, ctx, pool, orgID, projectID, requestID, "primary", createdAt)
	foreign := seedLogicalProviderAttempt(t, ctx, pool, foreignOrgID, foreignProjectID, requestID, "foreign", createdAt)

	requestSet, protocolPin := evidenceInputs(t, ctx, pool, orgID, projectID, requestID)
	before := databaseFingerprint(t, ctx, pool)
	generation, err := Generate(ctx, pool, requestSet, GenerateOptions{
		ProtocolPin: protocolPin,
		Now:         func() time.Time { return createdAt.Add(2 * time.Second) },
	})
	require.NoError(t, err)
	require.Equal(t, before, databaseFingerprint(t, ctx, pool), "provider-attempt evidence capture changed durable database state")

	require.Equal(t, SourceFactCounts{
		RequestedRequests: 1,
		ProviderAttempts:  1,
		CaptureGaps:       3,
		TotalFacts:        4,
	}, generation.SourceFacts.Counts)
	require.Len(t, generation.SourceFacts.Requests, 1)
	require.Len(t, generation.SourceFacts.Requests[0].ProviderAttempts, 1)
	attempt := generation.SourceFacts.Requests[0].ProviderAttempts[0]
	require.Equal(t, inScope.AttemptID, attempt.AttemptID)
	require.NotEqual(t, foreign.AttemptID, attempt.AttemptID)
	require.Equal(t, ProviderAttemptAccessBindingLogical, attempt.AccessBindingType)
	require.Equal(t, mustLogicalCredentialCorrelation(t, inScope.CredentialRef), attempt.LogicalCredentialCorrelationSHA256)
	require.Equal(t, "/v1/chat/completions", attempt.Endpoint)
	require.Equal(t, "provider-a", attempt.Provider)
	require.Equal(t, "settled", attempt.State)
	require.Equal(t, "known_usage", attempt.OutcomeCertainty)
	require.Equal(t, "unclassified", attempt.FailureKind)
	require.False(t, attempt.RetryAuthorized)
	require.NotNil(t, attempt.HTTPStatus)
	require.EqualValues(t, 200, *attempt.HTTPStatus)
	require.NotNil(t, attempt.CalculatedCostMicros)
	require.Equal(t, "1234", *attempt.CalculatedCostMicros)
	require.NotNil(t, attempt.ChargedCostMicros)
	require.Equal(t, "1200", *attempt.ChargedCostMicros)
	require.Equal(t, canonicalTime(createdAt), attempt.StartedAt)
	require.Equal(t, canonicalTime(createdAt.Add(500*time.Millisecond)), attempt.ResponseReceivedAt)
	require.Equal(t, canonicalTime(createdAt.Add(time.Second)), attempt.FinancialResolvedAt)

	attemptBody, err := CanonicalJSON(attempt)
	require.NoError(t, err)
	var attemptObject map[string]any
	require.NoError(t, json.Unmarshal(attemptBody, &attemptObject))
	for _, prohibitedField := range []string{
		"api_key_id", "access_credential_id", "credential_ref", "access_grant_id", "channel_id", "channel_key_id",
	} {
		require.NotContains(t, attemptObject, prohibitedField)
	}

	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x5a}, ed25519.SeedSize))
	publicKeys := map[string]ed25519.PublicKey{"source-evidence-provider-attempt-test": privateKey.Public().(ed25519.PublicKey)}
	generatedAt, err := time.Parse(time.RFC3339Nano, generation.SourceFacts.GeneratedAt)
	require.NoError(t, err)
	var packageBody bytes.Buffer
	_, err = Pack(&packageBody, PackageOptions{
		SigningKeyRef: "source-evidence-provider-attempt-test",
		PublicKeys:    publicKeys,
		PrivateKey:    privateKey,
		CreatedAt:     generatedAt.Add(time.Second),
		RequestSet:    requestSet,
		Authorization: validNeutralAuthorization(t, generation.SourceScope),
		Generation:    generation,
	})
	require.NoError(t, err)
	verified, err := Verify(bytes.NewReader(packageBody.Bytes()), publicKeys)
	require.NoError(t, err)
	require.Equal(t, generation.SourceFacts, verified.SourceFacts)
	verifiedJSON, err := CanonicalJSON(verified)
	require.NoError(t, err)
	for _, prohibited := range []string{
		inScope.CredentialRef,
		foreign.CredentialRef,
		inScope.UpstreamRequestID,
		foreign.UpstreamRequestID,
		inScope.EncryptedChannelKey,
		foreign.EncryptedChannelKey,
		"user:source-evidence-person@example.com",
		"api_key:987654",
		"private_person_example_com",
	} {
		require.NotContains(t, string(verifiedJSON), prohibited)
	}
}

func TestGenerateUsesOneRepeatableReadSnapshot(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	orgID, projectID := seedEvidenceProject(t, ctx, pool, "source-evidence-snapshot")
	requestID := "req-repeatable-read-001"
	requestSet, protocolPin := evidenceInputs(t, ctx, pool, orgID, projectID, requestID)

	reached := make(chan struct{})
	proceed := make(chan struct{})
	starter := &blockingTxStarter{pool: pool, reached: reached, proceed: proceed}
	type generationResult struct {
		generation Generation
		err        error
	}
	result := make(chan generationResult, 1)
	go func() {
		generation, err := Generate(ctx, starter, requestSet, GenerateOptions{
			ProtocolPin: protocolPin,
			Now:         func() time.Time { return time.Date(2026, 8, 10, 12, 3, 0, 0, time.UTC) },
		})
		result <- generationResult{generation: generation, err: err}
	}()

	select {
	case <-reached:
	case <-ctx.Done():
		require.FailNow(t, "generator did not reach bounded call-log query")
	}
	seedCallLog(t, ctx, pool, orgID, projectID, requestID, "late-model", time.Date(2026, 8, 10, 12, 2, 0, 0, time.UTC))
	close(proceed)
	generated := <-result
	require.NoError(t, generated.err)
	require.Nil(t, generated.generation.SourceFacts.Requests[0].CallLog, "late committed row escaped repeatable-read snapshot")
	require.Equal(t, 4, generated.generation.SourceFacts.Counts.CaptureGaps)
	require.Equal(t, 4, generated.generation.SourceFacts.Counts.TotalFacts)

	var durableRows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM call_logs WHERE org_id=$1 AND request_id=$2`, orgID, requestID).Scan(&durableRows))
	require.Equal(t, 1, durableRows, "test control row was not durably committed")
}

type blockingTxStarter struct {
	pool    *pgxpool.Pool
	reached chan struct{}
	proceed chan struct{}
}

func (starter *blockingTxStarter) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	tx, err := starter.pool.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &blockingTx{Tx: tx, reached: starter.reached, proceed: starter.proceed}, nil
}

type blockingTx struct {
	pgx.Tx
	reached chan struct{}
	proceed chan struct{}
	once    sync.Once
}

func (tx *blockingTx) Query(ctx context.Context, query string, arguments ...any) (pgx.Rows, error) {
	if strings.Contains(query, "FROM call_logs") {
		tx.once.Do(func() { close(tx.reached) })
		select {
		case <-tx.proceed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return tx.Tx.Query(ctx, query, arguments...)
}

func evidenceInputs(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID int64, requestID string) (RequestSet, []byte) {
	t.Helper()
	var currentSchema string
	require.NoError(t, pool.QueryRow(ctx, `SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&currentSchema))
	_, err := pool.Exec(ctx, `
		UPDATE release_state
		SET release_sequence=8, version=$1, commit=$2,
		    package_sha256=$3, trusted_key_id='source-evidence-integration-release',
		    target_schema=$4, manifest='{}'::jsonb,
		    updated_at='2026-08-10T11:59:00Z'
		WHERE singleton=TRUE`, testReleaseVersion, testReleaseCommit, strings.Repeat("7", 64), currentSchema)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO release_history (
		    release_sequence,version,commit,package_sha256,trusted_key_id,
		    target_schema,manifest,recorded_by,recorded_at
		) VALUES (8,$1,$2,$3,'source-evidence-integration-release',$4,'{}'::jsonb,'integration-test','2026-08-10T11:59:00Z')`,
		testReleaseVersion, testReleaseCommit, strings.Repeat("7", 64), currentSchema)
	require.NoError(t, err)
	requestSet := RequestSet{
		SchemaVersion:  RequestSetSchemaVersion,
		CampaignID:     "joint-poc-source-evidence-integration",
		SourceInstance: testSourceInstance,
		FrozenAt:       "2026-08-10T12:00:00Z",
		Requests: []RequestSelection{{
			ScenarioID: "chat-success",
			RequestID:  requestID,
			OrgID:      orgID,
			ProjectID:  projectID,
		}},
		Limits: RequestSetLimits{MaxRequests: MaxRequests, MaxFacts: MaxFacts},
	}
	require.NoError(t, ValidateRequestSet(requestSet))
	protocolPin, err := os.ReadFile(filepath.Join(repositoryRoot(t), "contracts", "foigent", "event-connector-protocol.pin.json"))
	require.NoError(t, err)
	return requestSet, protocolPin
}

func seedEvidenceProject(t *testing.T, ctx context.Context, pool *pgxpool.Pool, slug string) (int64, int64) {
	t.Helper()
	var orgID, projectID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO orgs (name,status,org_type) VALUES ($1,'active','standard') RETURNING id`, slug,
	).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO projects (org_id,name,slug,status,budget_period) VALUES ($1,$2,$2,'active','none') RETURNING id`, orgID, slug,
	).Scan(&projectID))
	return orgID, projectID
}

type seededLogicalProviderAttempt struct {
	AttemptID           string
	CredentialRef       string
	UpstreamRequestID   string
	EncryptedChannelKey string
}

func seedLogicalProviderAttempt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID int64, requestID, suffix string, startedAt time.Time) seededLogicalProviderAttempt {
	t.Helper()
	var adminID, subjectID, accessRequestID, grantID, credentialID, apiKeyID, channelID, channelKeyID int64
	referenceSuffix := fmt.Sprintf("%s_%d", suffix, orgID)
	credentialRef := "cred_source_evidence_" + referenceSuffix
	attemptID := "att_source_evidence_" + referenceSuffix
	upstreamRequestID := "raw-upstream-private-" + referenceSuffix
	encryptedChannelKey := "encrypted-provider-secret-" + referenceSuffix

	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,$2,'hash','org_admin','org_admin','active') RETURNING id`,
		orgID, "source-evidence-admin-"+referenceSuffix).Scan(&adminID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO users (org_id,username,password_hash,organization_role,role,status)
		VALUES ($1,$2,'hash','member','developer','active') RETURNING id`,
		orgID, "source-evidence-subject-"+referenceSuffix).Scan(&subjectID))
	_, err := pool.Exec(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'project_user')`, projectID, subjectID)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO access_requests (
		  request_ref,client_request_id,semantic_hash,org_id,project_id,subject_type,
		  subject_user_id,request_kind,requested_project_role,requested_scope_mode,
		  requested_models,requested_endpoints,environment,submission_mode,status,
		  decision_mode,policy_reason_codes,decision_reason_codes,policy_version,
		  submitted_by,decided_by,decided_at
		) VALUES (
		  $1,$2,repeat('a',64),$3,$4,'user',$5,'human_personal','project_user','explicit',
		  '["approved-model"]','["/v1/chat/completions"]','personal','direct_grant','approved',
		  'direct',ARRAY['project_default_scope'],ARRAY['direct_grant_authorized_nonproduction'],
		  'unirouter.enterprise-access-policy.v1',$6,$6,$7
		) RETURNING id`,
		"req_source_evidence_"+referenceSuffix, "source-evidence-"+referenceSuffix,
		orgID, projectID, subjectID, adminID, startedAt).Scan(&accessRequestID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO access_grants (
		  grant_ref,access_request_id,org_id,project_id,subject_type,subject_user_id,
		  project_role_snapshot,scope_mode,allowed_models,allowed_endpoints,environment,
		  status,valid_from,decision_mode,policy_reason_codes,decision_reason_codes,
		  policy_version,semantic_hash,created_by,updated_by
		) VALUES (
		  $1,$2,$3,$4,'user',$5,'project_user','explicit','["approved-model"]',
		  '["/v1/chat/completions"]','personal','active',$6,'direct',
		  ARRAY['project_default_scope'],ARRAY['direct_grant_authorized_nonproduction'],
		  'unirouter.enterprise-access-policy.v1',repeat('a',64),$7,$7
		) RETURNING id`,
		"grant_source_evidence_"+referenceSuffix, accessRequestID, orgID, projectID, subjectID, startedAt, adminID).Scan(&grantID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO access_credentials (
		  credential_ref,access_grant_id,org_id,project_id,name,allowed_ips,
		  allowed_models,allowed_endpoints,rpm_limit,rps_limit,created_by
		) VALUES ($1,$2,$3,$4,$5,'','approved-model','/v1/chat/completions',60,10,$6)
		RETURNING id`, credentialRef, grantID, orgID, projectID, "source evidence "+referenceSuffix, subjectID).Scan(&credentialID))
	keyDigest := sha256.Sum256([]byte("source-evidence-key-" + referenceSuffix))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO api_keys (
		  org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,status,
		  allowed_ips,allowed_models,allowed_endpoints,rpm_limit,qps_limit,purpose,
		  access_grant_id,access_credential_id,credential_ref
		) VALUES ($1,$2,$3,'personal',$4,$5,$6,'active','','approved-model',
		  '/v1/chat/completions',60,10,'',$7,$8,$9) RETURNING id`,
		orgID, projectID, subjectID, "source evidence "+referenceSuffix, hex.EncodeToString(keyDigest[:]),
		fmt.Sprintf("ur-se-%d", orgID), grantID, credentialID, credentialRef).Scan(&apiKeyID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO channels (name,provider,base_url,models,status)
		VALUES ($1,'provider-a','https://provider.invalid','approved-model','staged') RETURNING id`,
		"private-channel-locator-"+referenceSuffix).Scan(&channelID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO channel_keys (channel_id,key_enc,status) VALUES ($1,$2,'disabled') RETURNING id`,
		channelID, encryptedChannelKey).Scan(&channelKeyID))
	_, err = pool.Exec(ctx, `
		INSERT INTO freeze_idempotency (
		  request_id,org_id,project_id,status,pricing_version,
		  api_key_id,access_credential_id,credential_ref,access_grant_id
		) VALUES ($1,$2,$3,'settled','source-evidence-test',$4,$5,$6,$7)`,
		requestID, orgID, projectID, apiKeyID, credentialID, credentialRef, grantID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO billing_provider_attempts (
		  org_id,request_id,attempt_id,api_key_id,channel_id,channel_key_id,
		  route_sequence,route_total,endpoint,provider,channel_name,state,outcome_certainty,
		  retry_authorized,http_status,failure_kind,upstream_request_id,calculated_cost_micros,
		  charged_cost_micros,started_at,response_received_at,financial_resolved_at,updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,1,1,'/v1/chat/completions','provider-a',$7,
		  'settled','known_usage',FALSE,200,'private_person_example_com',$8,1234,1200,$9,$10,$11,$11)`,
		orgID, requestID, attemptID, apiKeyID, channelID, channelKeyID,
		"private-channel-locator-"+referenceSuffix, upstreamRequestID, startedAt,
		startedAt.Add(500*time.Millisecond), startedAt.Add(time.Second))
	require.NoError(t, err)

	return seededLogicalProviderAttempt{
		AttemptID: attemptID, CredentialRef: credentialRef,
		UpstreamRequestID: upstreamRequestID, EncryptedChannelKey: encryptedChannelKey,
	}
}

func seedCallLog(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, projectID int64, requestID, model string, createdAt time.Time) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO call_logs (
		    request_id,org_id,project_id,model,requested_model,provider,channel,
		    prompt_tokens,uncached_prompt_tokens,completion_tokens,total_tokens,
		    cost_micros,latency_ms,status_code,upstream_status_code,upstream_error,
		    usage_source,usage_authority,usage_purpose,usage_finality,usage_contract_version,
		    trace_id,attempt_id,actor_type,actor_ref,payload_capture_mode,ip_address,created_at
		) VALUES (
		    $1,$2,$3,$4,'requested-model','provider-a','channel-a',
		    4,4,2,6,1200,12,200,200,'raw provider error must not escape',
		    'upstream','provider_reported','customer_charge','final','unirouter.usage.v1',
		    'trace-001','attempt-001','api_key','user:source-evidence-person@example.com','metadata_only','203.0.113.99',$5
		)`, requestID, orgID, projectID, model, createdAt)
	require.NoError(t, err)
}

func databaseFingerprint(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	tables := []string{
		"call_logs",
		"request_trace_events",
		"billing_provider_attempts",
		"completed_execution_feed_items",
		"completed_execution_feed_states",
		"audit_logs",
		"internal_outbox_events",
		"transactions",
		"wallets",
		"access_requests",
		"access_grants",
		"access_credentials",
		"release_history",
	}
	state := make(map[string]any, len(tables)+1)
	for _, table := range tables {
		var count int64
		require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count))
		state[table] = count
	}
	var releaseJSON string
	require.NoError(t, pool.QueryRow(ctx, `SELECT row_to_json(release_state)::text FROM release_state WHERE singleton=TRUE`).Scan(&releaseJSON))
	state["release_state"] = releaseJSON
	body, err := json.Marshal(state)
	require.NoError(t, err)
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func TestGeneratedContractCountsRemainHardBounded(t *testing.T) {
	requests := make([]RequestFacts, MaxRequests)
	for index := range requests {
		requests[index] = RequestFacts{
			ScenarioID:       fmt.Sprintf("scenario-%02d", index),
			RequestID:        fmt.Sprintf("request-%02d", index),
			OrgID:            1,
			ProjectID:        1,
			TraceEvents:      make([]TraceFact, 0),
			ProviderAttempts: make([]ProviderAttemptFact, 0),
			CaptureGaps:      make([]SourceCaptureGap, 11),
		}
	}
	_, _, err := summarizeFacts(requests)
	require.ErrorContains(t, err, "exceeds")
}
