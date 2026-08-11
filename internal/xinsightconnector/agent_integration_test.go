//go:build integration

package xinsightconnector

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

const (
	cp2aInstallationID = "xci_AAAAAAAAAAAAAAAAAAAAAA"
	cp2aControlProject = "project.second-integration"
	cp2aInvitation     = "xiiv1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	cp2aControlToken   = "xib_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
)

func TestPostgresPhaseJournalRunsDurableFakeLifecycleWithoutSecretPersistence(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	journal, clock := setupCP2APhaseJournal(t, pool)
	journal.token = phaseTokenSequence(t,
		"11111111111111111111111111111111",
		"22222222222222222222222222222222",
		"33333333333333333333333333333333",
	)

	ackLost := errors.New("synthetic acknowledgement response loss")
	operations := agentTestThreePhaseOperations(t)
	counterpart := &agentTestCounterpart{
		operations: [][]byte{operations[0], operations[0], operations[1], operations[2]},
		ackErrors:  []error{ackLost, nil, nil, nil},
	}
	executor := newAgentTestExecutor(t)
	agent, err := NewTransactionalAgent(cp2aInstallationID, counterpart, journal, executor)
	require.NoError(t, err)

	for attempt := 0; attempt < 4; attempt++ {
		processed, runErr := agent.RunOnce(context.Background())
		require.True(t, processed)
		if attempt == 0 {
			require.ErrorIs(t, runErr, ackLost)
			continue
		}
		require.NoError(t, runErr)
		*clock = clock.Add(time.Second)
	}

	require.Equal(t, map[string]int{
		ControlPhasePreparePaused: 1, ControlPhaseRegisterReceiver: 1, ControlPhaseResumeSource: 1,
	}, executor.calls, "acknowledgement loss must replay the terminal result, not the phase action")
	require.Equal(t, DesiredActive, executor.sourceState)
	require.Equal(t, DesiredActive, executor.receiverState)
	require.Len(t, counterpart.acks, 4)
	require.Equal(t, counterpart.acks[0], counterpart.acks[1])

	ctx := context.Background()
	var operationCount, acknowledgedCount int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COUNT(*),COUNT(*) FILTER (WHERE acknowledged_at IS NOT NULL)
FROM xinsight_connector_phase_operations
WHERE installation_id=$1`, cp2aInstallationID).Scan(&operationCount, &acknowledgedCount))
	require.Equal(t, 3, operationCount)
	require.Equal(t, 3, acknowledgedCount)

	rows, err := pool.Query(ctx, `
SELECT request_canonical,result_canonical
FROM xinsight_connector_phase_operations
WHERE installation_id=$1
ORDER BY created_at,operation_id`, cp2aInstallationID)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var request, result []byte
		require.NoError(t, rows.Scan(&request, &result))
		for _, durable := range [][]byte{request, result} {
			require.NotContains(t, string(durable), cp2aInvitation)
			require.NotContains(t, string(durable), cp2aControlToken)
			assertAgentWireHasNoReceiverSecret(t, durable)
		}
	}
	require.NoError(t, rows.Err())

	var invitationCipher, controlCipher, controlSHA, controlProject sql.NullString
	require.NoError(t, pool.QueryRow(ctx, `
SELECT invitation_token_enc,control_token_enc,control_token_sha256,control_project_id
FROM xinsight_connector_control_bindings
WHERE installation_id=$1`, cp2aInstallationID).Scan(
		&invitationCipher, &controlCipher, &controlSHA, &controlProject,
	))
	require.False(t, invitationCipher.Valid)
	require.True(t, controlCipher.Valid)
	require.NotEqual(t, cp2aControlToken, controlCipher.String)
	require.NotContains(t, controlCipher.String, cp2aControlToken)
	require.Equal(t, hashBytes([]byte(cp2aControlToken)), controlSHA.String)
	require.Equal(t, cp2aControlProject, controlProject.String)
	opened, err := journal.controlToken(ctx, cp2aInstallationID)
	require.NoError(t, err)
	require.Equal(t, cp2aControlToken, opened)
	_, err = pool.Exec(ctx, `
UPDATE xinsight_connector_control_bindings
SET control_token_sha256='sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
WHERE installation_id=$1`, cp2aInstallationID)
	require.NoError(t, err)
	_, err = journal.controlToken(ctx, cp2aInstallationID)
	require.ErrorIs(t, err, ErrConflict, "ciphertext and durable token identity must remain installation-bound")
}

type phaseTxExecutorFunc func(
	context.Context,
	PhaseProductTx,
	PhaseExecutionInput,
) (ControlResultV2, error)

func (execute phaseTxExecutorFunc) ExecutePhaseTx(
	ctx context.Context,
	tx PhaseProductTx,
	input PhaseExecutionInput,
) (ControlResultV2, error) {
	return execute(ctx, tx, input)
}

func TestPostgresPhaseJournalExecutesProductEffectAndTerminalResultAtomically(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	journal, _ := setupCP2APhaseJournal(t, pool)
	journal.token = phaseTokenSequence(t, "44444444444444444444444444444444")
	operation := mustDecodeControlV2Operation(t,
		"testdata/control-v2/valid/01-prepare-paused-source-operation.json")
	requestSHA, err := operation.CanonicalSHA256()
	require.NoError(t, err)
	claim, err := journal.Acquire(context.Background(), operation, requestSHA)
	require.NoError(t, err)

	syntheticFailure := errors.New("synthetic product effect failure")
	_, _, err = journal.ExecuteClaimedPhase(
		context.Background(), operation, requestSHA, claim.ClaimToken,
		phaseTxExecutorFunc(func(
			ctx context.Context,
			tx PhaseProductTx,
			input PhaseExecutionInput,
		) (ControlResultV2, error) {
			tag, updateErr := tx.Exec(ctx, `
UPDATE xinsight_connector_installations
SET last_error_code='must_rollback',last_error_at=$2,updated_at=$2
WHERE installation_id=$1`, input.Installation.InstallationID, input.ObservedAt)
			if updateErr != nil || tag.RowsAffected() != 1 {
				return ControlResultV2{}, fmt.Errorf("write synthetic product effect: %w", updateErr)
			}
			return ControlResultV2{}, syntheticFailure
		}),
	)
	require.ErrorIs(t, err, syntheticFailure)

	var lastErrorCode sql.NullString
	var resultSHA, durableClaim sql.NullString
	require.NoError(t, pool.QueryRow(context.Background(), `
SELECT i.last_error_code,p.result_sha256,p.claim_token
FROM xinsight_connector_installations i
JOIN xinsight_connector_phase_operations p USING (installation_id)
WHERE p.operation_id=$1`, operation.OperationID).Scan(
		&lastErrorCode, &resultSHA, &durableClaim,
	))
	require.False(t, lastErrorCode.Valid, "failed executor product writes must roll back")
	require.False(t, resultSHA.Valid, "failed executor must not publish a terminal result")
	require.Equal(t, claim.ClaimToken, durableClaim.String)

	expectedResult := mustDecodeControlV2Result(t,
		"testdata/control-v2/valid/02-prepare-paused-source-result.json")
	canonicalResult, resultDigest, err := journal.ExecuteClaimedPhase(
		context.Background(), operation, requestSHA, claim.ClaimToken,
		phaseTxExecutorFunc(func(
			ctx context.Context,
			tx PhaseProductTx,
			input PhaseExecutionInput,
		) (ControlResultV2, error) {
			tag, updateErr := tx.Exec(ctx, `
UPDATE xinsight_connector_installations
SET last_error_code='transaction_committed',last_error_at=$2,updated_at=$2
WHERE installation_id=$1`, input.Installation.InstallationID, input.ObservedAt)
			if updateErr != nil || tag.RowsAffected() != 1 {
				return ControlResultV2{}, fmt.Errorf("write committed product effect: %w", updateErr)
			}
			return expectedResult, nil
		}),
	)
	require.NoError(t, err)
	require.Equal(t, hashBytes(canonicalResult), resultDigest)

	var committedError, committedResultSHA sql.NullString
	require.NoError(t, pool.QueryRow(context.Background(), `
SELECT i.last_error_code,p.result_sha256
FROM xinsight_connector_installations i
JOIN xinsight_connector_phase_operations p USING (installation_id)
WHERE p.operation_id=$1`, operation.OperationID).Scan(
		&committedError, &committedResultSHA,
	))
	require.Equal(t, "transaction_committed", committedError.String)
	require.Equal(t, resultDigest, committedResultSHA.String)

	replayExecutions := 0
	replayed, replaySHA, err := journal.ExecuteClaimedPhase(
		context.Background(), operation, requestSHA, claim.ClaimToken,
		phaseTxExecutorFunc(func(
			context.Context, PhaseProductTx, PhaseExecutionInput,
		) (ControlResultV2, error) {
			replayExecutions++
			return ControlResultV2{}, errors.New("terminal replay must not execute")
		}),
	)
	require.NoError(t, err)
	require.Zero(t, replayExecutions)
	require.Equal(t, canonicalResult, replayed)
	require.Equal(t, resultDigest, replaySHA)
}

func TestPostgresPhaseJournalFencesConcurrentAndStaleClaims(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	journal, clock := setupCP2APhaseJournal(t, pool)
	journal.claimTTL = time.Minute
	journal.token = phaseTokenSequence(t,
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	)
	operation := mustDecodeControlV2Operation(t,
		"testdata/control-v2/valid/01-prepare-paused-source-operation.json")
	requestSHA, err := operation.CanonicalSHA256()
	require.NoError(t, err)

	_, err = pool.Exec(context.Background(), `
UPDATE xinsight_connector_control_bindings
SET control_token_sha256='sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
WHERE installation_id=$1`, cp2aInstallationID)
	require.NoError(t, err)
	_, err = journal.Acquire(context.Background(), operation, requestSHA)
	require.ErrorIs(t, err, ErrConflict, "phase execution must verify the installation-bound control secret")
	_, err = pool.Exec(context.Background(), `
UPDATE xinsight_connector_control_bindings
SET control_token_sha256=$2
WHERE installation_id=$1`, cp2aInstallationID, hashBytes([]byte(cp2aControlToken)))
	require.NoError(t, err)

	wrongProject := operation
	wrongProject.ProjectID = "project.not-authorized"
	wrongProjectSHA, err := wrongProject.CanonicalSHA256()
	require.NoError(t, err)
	_, err = journal.Acquire(context.Background(), wrongProject, wrongProjectSHA)
	require.ErrorIs(t, err, ErrConflict)

	first, err := journal.Acquire(context.Background(), operation, requestSHA)
	require.NoError(t, err)
	require.NotEmpty(t, first.ClaimToken)
	_, err = journal.Acquire(context.Background(), operation, requestSHA)
	require.ErrorIs(t, err, ErrConflict, "an unexpired durable claim must be exclusive")

	*clock = clock.Add(time.Minute + time.Microsecond)
	recovered, err := journal.Acquire(context.Background(), operation, requestSHA)
	require.NoError(t, err)
	require.NotEqual(t, first.ClaimToken, recovered.ClaimToken)

	result := mustDecodeControlV2Result(t,
		"testdata/control-v2/valid/02-prepare-paused-source-result.json")
	canonicalResult, err := result.CanonicalJSON()
	require.NoError(t, err)
	resultSHA := hashBytes(canonicalResult)
	err = journal.Complete(context.Background(), operation, requestSHA, first.ClaimToken, canonicalResult, resultSHA)
	require.ErrorIs(t, err, ErrConflict, "an expired claim must not complete after a newer fence")
	_, err = pool.Exec(context.Background(), `
UPDATE xinsight_connector_control_bindings
SET control_token_sha256='sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
WHERE installation_id=$1`, cp2aInstallationID)
	require.NoError(t, err)
	err = journal.Complete(context.Background(), operation, requestSHA, recovered.ClaimToken, canonicalResult, resultSHA)
	require.ErrorIs(t, err, ErrConflict, "completion must reverify the installation-bound control secret")
	_, err = pool.Exec(context.Background(), `
UPDATE xinsight_connector_control_bindings
SET control_token_sha256=$2
WHERE installation_id=$1`, cp2aInstallationID, hashBytes([]byte(cp2aControlToken)))
	require.NoError(t, err)
	err = journal.Complete(context.Background(), operation, requestSHA, recovered.ClaimToken, canonicalResult, resultSHA)
	require.NoError(t, err)

	_, err = pool.Exec(context.Background(), `
UPDATE xinsight_connector_phase_operations
SET result_sha256='sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
WHERE operation_id=$1`, operation.OperationID)
	require.NoError(t, err)
	_, err = journal.Acquire(context.Background(), operation, requestSHA)
	require.ErrorIs(t, err, ErrUnavailable, "durable result corruption must fail before external acknowledgement")
}

func TestPostgresPhaseJournalPreservesTerminalButFencesPredecessorAfterControlFenceDrift(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	journal, _ := setupCP2APhaseJournal(t, pool)
	journal.token = phaseTokenSequence(t,
		"cccccccccccccccccccccccccccccccc",
		"dddddddddddddddddddddddddddddddd",
	)
	prepare := mustDecodeControlV2Operation(t,
		"testdata/control-v2/valid/01-prepare-paused-source-operation.json")
	prepareSHA, err := prepare.CanonicalSHA256()
	require.NoError(t, err)
	claim, err := journal.Acquire(context.Background(), prepare, prepareSHA)
	require.NoError(t, err)
	prepareResult := mustDecodeControlV2Result(t,
		"testdata/control-v2/valid/02-prepare-paused-source-result.json")
	canonicalResult, err := prepareResult.CanonicalJSON()
	require.NoError(t, err)
	resultSHA := hashBytes(canonicalResult)
	require.NoError(t, journal.Complete(
		context.Background(), prepare, prepareSHA, claim.ClaimToken, canonicalResult, resultSHA,
	))

	_, err = pool.Exec(context.Background(), `
UPDATE xinsight_connector_installations
SET control_fence=control_fence+1
WHERE installation_id=$1`, cp2aInstallationID)
	require.NoError(t, err)
	replayed, err := journal.Acquire(context.Background(), prepare, prepareSHA)
	require.NoError(t, err)
	require.Equal(t, canonicalResult, replayed.TerminalCanonicalResult,
		"a committed result remains immutable and replayable after local authority advances")
	require.NoError(t, journal.MarkAcknowledged(
		context.Background(), prepare, prepareSHA, resultSHA,
	))

	register := mustDecodeControlV2Operation(t,
		"testdata/control-v2/valid/03-register-receiver-operation.json")
	registerSHA, err := register.CanonicalSHA256()
	require.NoError(t, err)
	_, err = journal.Acquire(context.Background(), register, registerSHA)
	require.Equal(t, "predecessor_phase_conflict", ControlV2ErrorCode(err),
		"a current phase must not consume an old-fence predecessor")
	var registerCount int
	require.NoError(t, pool.QueryRow(context.Background(), `
SELECT COUNT(*) FROM xinsight_connector_phase_operations WHERE operation_id=$1`,
		register.OperationID,
	).Scan(&registerCount))
	require.Zero(t, registerCount)

	restartedPrepare := prepare
	restartedPrepare.OperationID = "xco_EEEEEEEEEEEEEEEEEEEEEE"
	restartedSHA, err := restartedPrepare.CanonicalSHA256()
	require.NoError(t, err)
	restarted, err := journal.Acquire(context.Background(), restartedPrepare, restartedSHA)
	require.NoError(t, err, "a new exact operation may restart phase 1 under the new control fence")
	require.NotEmpty(t, restarted.ClaimToken)
}

func setupCP2APhaseJournal(
	t *testing.T,
	pool *pgxpool.Pool,
) (*PostgresPhaseJournal, *time.Time) {
	t.Helper()
	ctx := context.Background()
	var orgID, projectID, actorID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('connector-cp2a-agent','active','standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status)
VALUES ($1,'connector-cp2a-agent','hash','platform_admin','active') RETURNING id`, orgID).Scan(&actorID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO projects (org_id,name,slug,status,created_by)
VALUES ($1,'Connector CP2A agent','second-integration','active',$2) RETURNING id`,
		orgID, actorID).Scan(&projectID))

	manager, err := NewManager(
		pool,
		&recordingAuthorizer{},
		sourceAuthorityFixture{enabled: true, instance: "ecs.second-integration.v2"},
	)
	require.NoError(t, err)
	clock := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return clock }
	_, _, err = manager.CreateInstallation(ctx, Consent{
		InstallationID:   cp2aInstallationID,
		XDeploymentID:    "xid_ZZZZZZZZZZZZZZZZZZZZZZ",
		InvitationSHA256: hashBytes([]byte(cp2aInvitation)),
		OrgID:            orgID, ProjectID: projectID,
		EventScope: EventScopeModelCompleted, IdempotencyKey: "connect-cp2a-agent",
		ConsentedByUserID: actorID,
	})
	require.NoError(t, err)

	journal, err := NewPostgresPhaseJournal(pool, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	journal.now = func() time.Time { return clock }
	journal.controlBearer = func() (string, error) { return cp2aControlToken, nil }
	require.NoError(t, journal.StageInvitation(ctx, cp2aInstallationID, cp2aInvitation))
	require.NoError(t, journal.StageInvitation(ctx, cp2aInstallationID, cp2aInvitation))
	prepared, err := journal.PrepareRedeem(ctx, cp2aInstallationID)
	require.NoError(t, err)
	require.Equal(t, cp2aControlProject, prepared.ControlProjectID)
	result := cp2aRegistrationResult(t, prepared, cp2aControlProject)
	drifted := cp2aRegistrationResult(t, prepared, "project.not-authorized")
	require.ErrorIs(t, journal.FinalizeRedeem(ctx, prepared, drifted), ErrConflict)
	require.NoError(t, journal.FinalizeRedeem(ctx, prepared, result))
	require.NoError(t, journal.FinalizeRedeem(ctx, prepared, result))
	return journal, &clock
}

func cp2aRegistrationResult(
	t *testing.T,
	prepared PreparedInvitationRedeem,
	projectID string,
) []byte {
	t.Helper()
	operation := mustDecodeControlV2Operation(
		t, "testdata/control-v2/valid/01-prepare-paused-source-operation.json",
	)
	operation.ProjectID = projectID
	operationBytes, err := operation.CanonicalJSON()
	require.NoError(t, err)
	installation := struct {
		InstallationID  string   `json:"installation_id"`
		DeploymentID    string   `json:"deployment_id"`
		ProjectID       string   `json:"project_id"`
		FinalEventScope []string `json:"final_event_scope"`
	}{
		InstallationID: prepared.InstallationID, DeploymentID: prepared.XDeploymentID,
		ProjectID: projectID, FinalEventScope: []string{EventScopeModelCompleted},
	}
	var encoded bytes.Buffer
	require.NoError(t, json.NewEncoder(&encoded).Encode(struct {
		Installation any             `json:"installation"`
		Operation    json.RawMessage `json:"operation"`
	}{Installation: installation, Operation: operationBytes}))
	require.True(t, bytes.HasSuffix(encoded.Bytes(), []byte("\n")))
	return encoded.Bytes()
}

func phaseTokenSequence(t *testing.T, values ...string) func() (string, error) {
	t.Helper()
	index := 0
	return func() (string, error) {
		if index >= len(values) {
			return "", fmt.Errorf("phase token fixture exhausted")
		}
		value := values[index]
		index++
		return value, nil
	}
}
