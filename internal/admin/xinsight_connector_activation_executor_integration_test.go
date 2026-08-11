//go:build integration

package admin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

const (
	cp2bInstallationID = "xci_AAAAAAAAAAAAAAAAAAAAAA"
	cp2bDeploymentID   = "xid_ZZZZZZZZZZZZZZZZZZZZZZ"
	cp2bProjectRef     = "project.second-integration"
	cp2bInvitation     = "xiiv1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	cp2bSource         = "ecs.second-integration.v2"
	cp2bBindingSHA     = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
	cp2bAllowedIP      = "198.51.100.8/32"
)

func TestXInsightConnectorActivationExecutorAtomicLifecycleIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()

	journal, executor, counterpart, projectID, controlToken := setupCP2BActivation(t, pool)
	agent, err := xinsightconnector.NewTransactionalAgent(
		cp2bInstallationID, counterpart, journal, executor,
	)
	require.NoError(t, err)

	prepare := cp2bPrepareOperation()
	counterpart.operation = canonicalCP2BOperation(t, prepare)
	processed, err := agent.RunOnce(ctx)
	require.True(t, processed)
	require.NoError(t, err)
	require.Len(t, counterpart.acks, 1)
	prepareResult := decodeCP2BResult(t, counterpart.acks[0])
	paused, ok := prepareResult.Result.(xinsightconnector.PreparePausedSourceResultV2)
	require.True(t, ok)
	require.Equal(t, cp2bSource, paused.SourceInstance)
	require.Equal(t, fmt.Sprintf("cursor-%d-7", projectID), paused.Cursor)
	require.Equal(t, paused.Cursor, paused.HighWatermark)
	requireCP2BFeedState(t, pool, projectID, "paused", 0, 0, "provisioning")

	rawBearer := auth.ReceiverFeedTokenPrefix + strings.Repeat("ab", 32)
	registration := cp2bReceiverRegistration(t, rawBearer)
	prepareResultSHA, err := prepareResult.CanonicalSHA256()
	require.NoError(t, err)
	register := cp2bRegisterOperation(prepare, prepareResultSHA, registration)
	counterpart.operation = canonicalCP2BOperation(t, register)
	ackLost := errors.New("synthetic X acknowledgement response loss")
	counterpart.ackError = ackLost
	processed, err = agent.RunOnce(ctx)
	require.True(t, processed)
	require.ErrorIs(t, err, ackLost)
	require.Len(t, counterpart.acks, 2)
	firstRegisterAck := bytes.Clone(counterpart.acks[1])
	requireCP2BFeedState(t, pool, projectID, "paused", 0, 1, "provisioning")
	requireCP2BStoredReceiver(t, pool, projectID, registration, rawBearer)
	requireCP2BAuditCount(t, pool, projectID, "data_exchange.xinsight.credential.create", 1)

	processed, err = agent.RunOnce(ctx)
	require.True(t, processed)
	require.NoError(t, err)
	require.Len(t, counterpart.acks, 3)
	require.Equal(t, firstRegisterAck, counterpart.acks[2], "ack loss must replay exact terminal bytes")
	requireCP2BFeedState(t, pool, projectID, "paused", 0, 1, "provisioning")
	requireCP2BAuditCount(t, pool, projectID, "data_exchange.xinsight.credential.create", 1)

	registerResult := decodeCP2BResult(t, counterpart.acks[2])
	registerResultSHA, err := registerResult.CanonicalSHA256()
	require.NoError(t, err)
	resume := cp2bResumeOperation(t, register, registerResult, registerResultSHA)
	resumeSHA, err := resume.CanonicalSHA256()
	require.NoError(t, err)
	acquisition, err := journal.Acquire(ctx, resume, resumeSHA)
	require.NoError(t, err)
	require.NotEmpty(t, acquisition.ClaimToken)

	require.NoError(t, createCP2BResumeAuditFailure(ctx, pool))
	_, _, err = journal.ExecuteClaimedPhase(
		ctx, resume, resumeSHA, acquisition.ClaimToken, executor,
	)
	require.Error(t, err, "the injected audit failure must abort the product transaction")
	requireCP2BFeedState(t, pool, projectID, "paused", 0, 1, "provisioning")
	requireCP2BAuditCount(t, pool, projectID, "data_exchange.xinsight.credential.activate", 0)
	requireCP2BAuditCount(t, pool, projectID, "data_exchange.xinsight.connection.resume", 0)
	requireCP2BRootControlState(t, pool, xinsightconnector.OperationQueued, 1, "")
	var resumeTerminalCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FROM xinsight_connector_phase_operations
WHERE operation_id=$1 AND result_canonical IS NOT NULL`, resume.OperationID).Scan(&resumeTerminalCount))
	require.Zero(t, resumeTerminalCount, "product effects and terminal result must roll back together")
	require.NoError(t, removeCP2BResumeAuditFailure(ctx, pool))

	resumeCanonical, resumeResultSHA, err := journal.ExecuteClaimedPhase(
		ctx, resume, resumeSHA, acquisition.ClaimToken, executor,
	)
	require.NoError(t, err)
	replayedCanonical, replayedSHA, err := journal.ExecuteClaimedPhase(
		ctx, resume, resumeSHA, acquisition.ClaimToken, executor,
	)
	require.NoError(t, err)
	require.Equal(t, resumeCanonical, replayedCanonical)
	require.Equal(t, resumeResultSHA, replayedSHA)
	require.NoError(t, journal.MarkAcknowledged(
		ctx, resume, resumeSHA, resumeResultSHA,
	))
	requireCP2BFeedState(t, pool, projectID, "active", 1, 0, "connected")
	requireCP2BRootControlState(
		t, pool, xinsightconnector.OperationSucceeded, 2, resumeResultSHA,
	)
	requireCP2BAuditCount(t, pool, projectID, "data_exchange.xinsight.credential.activate", 1)
	requireCP2BAuditCount(t, pool, projectID, "data_exchange.xinsight.connection.resume", 1)

	var terminalCount, acknowledgedCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FILTER (WHERE result_canonical IS NOT NULL),
       COUNT(*) FILTER (WHERE acknowledged_at IS NOT NULL)
FROM xinsight_connector_phase_operations
WHERE installation_id=$1`, cp2bInstallationID).Scan(&terminalCount, &acknowledgedCount))
	require.Equal(t, 3, terminalCount)
	require.Equal(t, 3, acknowledgedCount)
	requireCP2BSecretAndTrafficBoundary(
		t, pool, projectID, rawBearer, controlToken, registration.VerifierSHA256,
	)

}

type cp2bSourceAuthority struct{}

func (cp2bSourceAuthority) Enabled() bool          { return true }
func (cp2bSourceAuthority) SourceInstance() string { return cp2bSource }

type cp2bFeedController struct{}

func (cp2bFeedController) Enabled() bool          { return true }
func (cp2bFeedController) SourceInstance() string { return cp2bSource }
func (cp2bFeedController) EncodeSequence(projectID, sequence int64) (string, error) {
	return fmt.Sprintf("cursor-%d-%d", projectID, sequence), nil
}

type cp2bBindingResolver struct{}

func (cp2bBindingResolver) ResolveXInsightReceiverBinding(
	_ context.Context,
	installationID, deploymentID, bindingSHA string,
) (XInsightReceiverBinding, error) {
	if installationID != cp2bInstallationID || deploymentID != cp2bDeploymentID ||
		bindingSHA != cp2bBindingSHA {
		return XInsightReceiverBinding{}, xinsightconnector.ErrConflict
	}
	return XInsightReceiverBinding{BindingSHA256: cp2bBindingSHA, AllowedIP: cp2bAllowedIP}, nil
}

type cp2bCounterpart struct {
	operation []byte
	ackError  error
	acks      [][]byte
}

func (counterpart *cp2bCounterpart) Poll(context.Context) ([]byte, bool, error) {
	if len(counterpart.operation) == 0 {
		return nil, false, nil
	}
	return bytes.Clone(counterpart.operation), true, nil
}

func (counterpart *cp2bCounterpart) Acknowledge(_ context.Context, result []byte) error {
	counterpart.acks = append(counterpart.acks, bytes.Clone(result))
	err := counterpart.ackError
	counterpart.ackError = nil
	return err
}

func setupCP2BActivation(
	t *testing.T,
	pool *pgxpool.Pool,
) (*xinsightconnector.PostgresPhaseJournal, *XInsightConnectorActivationExecutor, *cp2bCounterpart, int64, string) {
	t.Helper()
	ctx := context.Background()
	var orgID, projectID, actorID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('connector-cp2b-activation','active','standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status)
VALUES ($1,'connector-cp2b-owner','hash','platform_admin','active') RETURNING id`, orgID).Scan(&actorID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO projects (org_id,name,slug,status,created_by)
VALUES ($1,'Connector CP2B activation','second-integration','active',$2) RETURNING id`,
		orgID, actorID).Scan(&projectID))

	manager, err := xinsightconnector.NewManager(
		pool,
		xinsightconnector.CommitAuthorizerFunc(func(
			context.Context, pgx.Tx, int64, int64, int64, string,
		) error {
			return nil
		}),
		cp2bSourceAuthority{},
	)
	require.NoError(t, err)
	_, _, err = manager.CreateInstallation(ctx, xinsightconnector.Consent{
		InstallationID: cp2bInstallationID, XDeploymentID: cp2bDeploymentID,
		InvitationSHA256: cp2bSHA(cp2bInvitation), OrgID: orgID, ProjectID: projectID,
		EventScope:     xinsightconnector.EventScopeModelCompleted,
		IdempotencyKey: "connect-cp2b-activation", ConsentedByUserID: actorID,
	})
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO completed_execution_feed_states (org_id,project_id,retained_after_id)
VALUES ($1,$2,7)
ON CONFLICT (org_id,project_id) DO UPDATE SET retained_after_id=EXCLUDED.retained_after_id
RETURNING retained_after_id`, orgID, projectID).Scan(new(int64)))

	journal, err := xinsightconnector.NewPostgresPhaseJournal(
		pool, []byte("0123456789abcdef0123456789abcdef"),
	)
	require.NoError(t, err)
	require.NoError(t, journal.StageInvitation(ctx, cp2bInstallationID, cp2bInvitation))
	prepared, err := journal.PrepareRedeem(ctx, cp2bInstallationID)
	require.NoError(t, err)
	require.Equal(t, cp2bProjectRef, prepared.ControlProjectID)
	var redeemRequest struct {
		ControlToken string `json:"control_token"`
	}
	require.NoError(t, json.Unmarshal(prepared.CanonicalRequest, &redeemRequest))
	require.NotEmpty(t, redeemRequest.ControlToken)
	installationBytes, err := json.Marshal(map[string]any{
		"event_scope":     []string{xinsightconnector.EventScopeModelCompleted},
		"installation_id": cp2bInstallationID,
		"project_id":      cp2bProjectRef,
		"x_deployment_id": cp2bDeploymentID,
	})
	require.NoError(t, err)
	operationBytes, err := cp2bPrepareOperation().CanonicalJSON()
	require.NoError(t, err)
	registrationResult, err := json.Marshal(map[string]json.RawMessage{
		"installation": installationBytes,
		"operation":    operationBytes,
	})
	require.NoError(t, err)
	require.NoError(t, journal.FinalizeRedeem(ctx, prepared, registrationResult))

	handler := NewHandlerWithAPIKeyHashSecret(
		pool, "cp2b-jwt-secret", "cp2b-api-key-hmac",
		[]byte("0123456789abcdef01234567"), nil,
	)
	handler.SetCompletedFeedController(cp2bFeedController{})
	executor, err := NewXInsightConnectorActivationExecutor(handler, cp2bBindingResolver{})
	require.NoError(t, err)
	return journal, executor, &cp2bCounterpart{}, projectID, redeemRequest.ControlToken
}

func canonicalCP2BOperation(t *testing.T, operation xinsightconnector.ControlOperationV2) []byte {
	t.Helper()
	canonical, err := operation.CanonicalJSON()
	require.NoError(t, err)
	return canonical
}

func decodeCP2BResult(t *testing.T, canonical []byte) xinsightconnector.ControlResultV2 {
	t.Helper()
	result, err := xinsightconnector.DecodeControlResultV2(canonical)
	require.NoError(t, err)
	return result
}

func cp2bPrepareOperation() xinsightconnector.ControlOperationV2 {
	return xinsightconnector.ControlOperationV2{
		SchemaVersion: xinsightconnector.ControlOperationV2Schema,
		OperationID:   "xco_BBBBBBBBBBBBBBBBBBBBBB", InstallationID: cp2bInstallationID,
		ProjectID: cp2bProjectRef, EventScope: []string{xinsightconnector.EventScopeModelCompleted},
		DesiredState: xinsightconnector.DesiredActive, Kind: xinsightconnector.ControlKindConnect,
		Phase: xinsightconnector.ControlPhasePreparePaused,
		Request: xinsightconnector.PreparePausedSourceRequestV2{
			FeedSchema:          "unirouter.completed-execution.v1",
			CursorKey:           "unirouter.completed-execution.v1",
			RequiredSourceState: xinsightconnector.DesiredPaused,
		},
	}
}

func cp2bRegisterOperation(
	prepare xinsightconnector.ControlOperationV2,
	prepareResultSHA string,
	registration xinsightconnector.ReceiverRegistrationV1,
) xinsightconnector.ControlOperationV2 {
	return xinsightconnector.ControlOperationV2{
		SchemaVersion: xinsightconnector.ControlOperationV2Schema,
		OperationID:   "xco_CCCCCCCCCCCCCCCCCCCCCC", InstallationID: cp2bInstallationID,
		ProjectID: cp2bProjectRef, EventScope: []string{xinsightconnector.EventScopeModelCompleted},
		DesiredState: xinsightconnector.DesiredActive, Kind: xinsightconnector.ControlKindConnect,
		Phase:                  xinsightconnector.ControlPhaseRegisterReceiver,
		PredecessorOperationID: prepare.OperationID, PredecessorResultSHA256: prepareResultSHA,
		Request: xinsightconnector.RegisterReceiverRequestV2{
			SourceInstance: cp2bSource, ConsumerEpoch: 1,
			CheckpointAuthoritySHA256: "sha256:" + strings.Repeat("2", 64),
			ReceiverRegistration:      registration,
			NetworkBinding: xinsightconnector.ReceiverNetworkBindingV2{
				Transport: "https", Route: xinsightconnector.ControlFixedReceiverRoute,
				BindingSHA256: cp2bBindingSHA,
			},
			RequiredSourceState: xinsightconnector.DesiredPaused,
		},
	}
}

func cp2bResumeOperation(
	t *testing.T,
	register xinsightconnector.ControlOperationV2,
	registerResult xinsightconnector.ControlResultV2,
	registerResultSHA string,
) xinsightconnector.ControlOperationV2 {
	t.Helper()
	registrationResult, ok := registerResult.Result.(xinsightconnector.RegisterReceiverResultV2)
	require.True(t, ok)
	return xinsightconnector.ControlOperationV2{
		SchemaVersion: xinsightconnector.ControlOperationV2Schema,
		OperationID:   "xco_DDDDDDDDDDDDDDDDDDDDDD", InstallationID: cp2bInstallationID,
		ProjectID: cp2bProjectRef, EventScope: []string{xinsightconnector.EventScopeModelCompleted},
		DesiredState: xinsightconnector.DesiredActive, Kind: xinsightconnector.ControlKindConnect,
		Phase:                  xinsightconnector.ControlPhaseResumeSource,
		PredecessorOperationID: register.OperationID, PredecessorResultSHA256: registerResultSHA,
		Request: xinsightconnector.ResumeSourceRequestV2{
			SourceInstance:                 cp2bSource,
			ReceiverRegistrationSHA256:     registrationResult.ReceiverRegistrationSHA256,
			NetworkBindingSHA256:           registrationResult.NetworkBindingSHA256,
			PausedAuthReceiptSHA256:        "sha256:" + strings.Repeat("5", 64),
			DormantCredentialReceiptSHA256: "sha256:" + strings.Repeat("6", 64),
			RequiredSourceState:            xinsightconnector.DesiredActive,
			RequiredReceiverState:          xinsightconnector.DesiredActive,
			Readiness:                      "paused_receiver_authenticated",
		},
	}
}

func cp2bReceiverRegistration(t *testing.T, rawBearer string) xinsightconnector.ReceiverRegistrationV1 {
	t.Helper()
	verifier, err := auth.ReceiverFeedVerifierHex(rawBearer)
	require.NoError(t, err)
	prefix, err := auth.ReceiverFeedKeyPrefix(rawBearer)
	require.NoError(t, err)
	return xinsightconnector.ReceiverRegistrationV1{
		SchemaVersion:  auth.ReceiverFeedRegistrationSchema,
		TokenFormat:    auth.ReceiverFeedTokenFormat,
		VerifierScheme: auth.ReceiverFeedVerifierScheme,
		KeyPrefix:      prefix, VerifierSHA256: verifier,
		OperationID: strings.Repeat("7", 32),
	}
}

func cp2bSHA(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func requireCP2BFeedState(
	t *testing.T,
	pool *pgxpool.Pool,
	projectID int64,
	wantConnection string,
	wantActive, wantStaged int,
	wantInstallation string,
) {
	t.Helper()
	ctx := context.Background()
	var connection, installation string
	var active, staged int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT status FROM completed_execution_feed_connections WHERE project_id=$1`, projectID).Scan(&connection))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*) FILTER (WHERE status='feed_active'),
       COUNT(*) FILTER (WHERE status='feed_staged')
FROM api_keys WHERE project_id=$1 AND purpose='xinsight.feed.read'`, projectID).Scan(&active, &staged))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT observed_state FROM xinsight_connector_installations WHERE installation_id=$1`,
		cp2bInstallationID).Scan(&installation))
	require.Equal(t, wantConnection, connection)
	require.Equal(t, wantActive, active)
	require.Equal(t, wantStaged, staged)
	require.Equal(t, wantInstallation, installation)
}

func requireCP2BStoredReceiver(
	t *testing.T,
	pool *pgxpool.Pool,
	projectID int64,
	registration xinsightconnector.ReceiverRegistrationV1,
	rawBearer string,
) {
	t.Helper()
	var status, keyHash, prefix, scheme, operationID, allowedIP string
	require.NoError(t, pool.QueryRow(context.Background(), `
SELECT status,key_hash,key_prefix,verifier_scheme,registration_operation_id,allowed_ips
FROM api_keys
WHERE project_id=$1 AND purpose='xinsight.feed.read'`, projectID).Scan(
		&status, &keyHash, &prefix, &scheme, &operationID, &allowedIP,
	))
	decodedVerifier, err := auth.DecodeReceiverFeedVerifier(registration.VerifierSHA256)
	require.NoError(t, err)
	require.Equal(t, middleware.APIKeyStatusFeedStaged, status)
	require.Equal(t, auth.HashReceiverFeedVerifier(decodedVerifier, "cp2b-api-key-hmac"), keyHash)
	require.NotEqual(t, registration.VerifierSHA256, keyHash)
	require.NotContains(t, keyHash, rawBearer)
	require.Equal(t, registration.KeyPrefix, prefix)
	require.Equal(t, registration.VerifierScheme, scheme)
	require.Equal(t, registration.OperationID, operationID)
	require.Equal(t, cp2bAllowedIP, allowedIP)
}

func requireCP2BAuditCount(
	t *testing.T,
	pool *pgxpool.Pool,
	projectID int64,
	action string,
	want int,
) {
	t.Helper()
	var count int
	require.NoError(t, pool.QueryRow(context.Background(), `
SELECT COUNT(*) FROM audit_logs WHERE project_id=$1 AND action=$2`, projectID, action).Scan(&count))
	require.Equal(t, want, count)
}

func requireCP2BRootControlState(
	t *testing.T,
	pool *pgxpool.Pool,
	wantOperationState string,
	wantRevision int64,
	wantResultSHA string,
) {
	t.Helper()
	var operationState string
	var revision int64
	var resultCode, resultSHA *string
	require.NoError(t, pool.QueryRow(context.Background(), `
SELECT operation.state,installation.state_revision,
       operation.result_code,operation.result_sha256
FROM xinsight_connector_operations AS operation
JOIN xinsight_connector_installations AS installation
  ON installation.installation_id=operation.installation_id
WHERE operation.installation_id=$1 AND operation.action='connect'`,
		cp2bInstallationID,
	).Scan(&operationState, &revision, &resultCode, &resultSHA))
	require.Equal(t, wantOperationState, operationState)
	require.Equal(t, wantRevision, revision)
	if wantResultSHA == "" {
		require.Nil(t, resultCode)
		require.Nil(t, resultSHA)
		return
	}
	require.NotNil(t, resultCode)
	require.Equal(t, "connector_activation_complete", *resultCode)
	require.NotNil(t, resultSHA)
	require.Equal(t, wantResultSHA, *resultSHA)
}

func createCP2BResumeAuditFailure(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `
CREATE FUNCTION cp2b_reject_resume_audit() RETURNS trigger AS $$
BEGIN
  IF NEW.action='data_exchange.xinsight.connection.resume' THEN
    RAISE EXCEPTION 'synthetic CP2B resume audit failure';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql`); err != nil {
		return err
	}
	_, err := pool.Exec(ctx, `
CREATE TRIGGER cp2b_reject_resume_audit
BEFORE INSERT ON audit_logs
FOR EACH ROW EXECUTE FUNCTION cp2b_reject_resume_audit()`)
	return err
}

func removeCP2BResumeAuditFailure(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `DROP TRIGGER cp2b_reject_resume_audit ON audit_logs`); err != nil {
		return err
	}
	_, err := pool.Exec(ctx, `DROP FUNCTION cp2b_reject_resume_audit()`)
	return err
}

func requireCP2BSecretAndTrafficBoundary(
	t *testing.T,
	pool *pgxpool.Pool,
	projectID int64,
	rawBearer, controlToken, publicVerifier string,
) {
	t.Helper()
	ctx := context.Background()
	var phaseText, auditText, keyText string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COALESCE(string_agg(
  convert_from(request_canonical,'UTF8') ||
  COALESCE(convert_from(result_canonical,'UTF8'),''), ' '
), '')
FROM xinsight_connector_phase_operations
WHERE installation_id=$1`, cp2bInstallationID).Scan(&phaseText))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COALESCE(string_agg(detail::text,' '),'')
FROM audit_logs WHERE project_id=$1`, projectID).Scan(&auditText))
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COALESCE(string_agg(
  COALESCE(key_hash,'') || ' ' || COALESCE(key_prefix,'') || ' ' ||
  COALESCE(allowed_ips,'') || ' ' || COALESCE(registration_operation_id,''), ' '
), '')
FROM api_keys WHERE project_id=$1`, projectID).Scan(&keyText))
	for _, durable := range []string{phaseText, auditText, keyText} {
		require.NotContains(t, durable, rawBearer)
		require.NotContains(t, durable, cp2bInvitation)
		require.NotContains(t, durable, controlToken)
	}
	require.Contains(t, phaseText, publicVerifier, "public registration authority belongs in the request")
	require.NotContains(t, auditText, publicVerifier)
	require.NotContains(t, keyText, publicVerifier)

	var businessRows int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT
  (SELECT COUNT(*) FROM call_logs) +
  (SELECT COUNT(*) FROM request_trace_events) +
  (SELECT COUNT(*) FROM billing_provider_attempts) +
  (SELECT COUNT(*) FROM completed_execution_feed_items) +
  (SELECT COUNT(*) FROM internal_outbox_events) +
  (SELECT COUNT(*) FROM transactions) +
  (SELECT COUNT(*) FROM async_media_jobs)`).Scan(&businessRows))
	require.Zero(t, businessRows)
}
