//go:build integration

package xinsightconnector

import (
	"context"
	"database/sql"
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
	cp2aControlToken   = "xib_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
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
	agent, err := NewAgent(cp2aInstallationID, counterpart, journal, executor)
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
VALUES ($1,'Connector CP2A agent','connector-cp2a-agent','active',$2) RETURNING id`,
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
	require.NoError(t, journal.StageInvitation(ctx, cp2aInstallationID, cp2aInvitation))
	require.NoError(t, journal.StageInvitation(ctx, cp2aInstallationID, cp2aInvitation))
	require.NoError(t, journal.PromoteControlToken(
		ctx, cp2aInstallationID, cp2aInvitation, cp2aControlToken, cp2aControlProject,
	))
	require.ErrorIs(t, journal.PromoteControlToken(
		ctx, cp2aInstallationID, cp2aInvitation, cp2aControlToken, "project.not-authorized",
	), ErrConflict)
	require.NoError(t, journal.PromoteControlToken(
		ctx, cp2aInstallationID, cp2aInvitation, cp2aControlToken, cp2aControlProject,
	))
	return journal, &clock
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
