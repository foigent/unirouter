//go:build integration

package xinsightconnector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestConnectorRedeemAndFixedOriginTransportAreCrashSafeIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	journal, clock := setupRedeemInstallation(t, pool)
	journal.controlBearer = func() (string, error) { return cp2aControlToken, nil }

	// Crash before send: preparation alone must persist both authorities and
	// the exact request digest while retaining the invitation.
	prepared, err := journal.PrepareRedeem(ctx, cp2aInstallationID)
	require.NoError(t, err)
	require.Equal(t, cp2aControlProject, prepared.ControlProjectID)
	redeemRequest, err := decodeRedeemRequest(prepared.CanonicalRequest)
	require.NoError(t, err)
	require.Equal(t, []string{EventScopeModelCompleted}, redeemRequest.FinalEventScope)
	require.Contains(t, string(prepared.CanonicalRequest), cp2aInvitation)
	require.Contains(t, string(prepared.CanonicalRequest), cp2aControlToken)
	requireRedeemBindingState(t, pool, "redeem_pending", prepared.RequestSHA256, "")

	// Process restart must not call the token generator and must reconstruct
	// byte-identical request bytes from the encrypted durable state.
	restarted, err := NewPostgresPhaseJournal(
		pool, []byte("0123456789abcdef0123456789abcdef"),
	)
	require.NoError(t, err)
	restarted.now = func() time.Time { return *clock }
	restarted.controlBearer = func() (string, error) {
		return "", errors.New("a pending redeem must never regenerate its bearer")
	}
	replayed, err := restarted.PrepareRedeem(ctx, cp2aInstallationID)
	require.NoError(t, err)
	require.Equal(t, prepared.CanonicalRequest, replayed.CanonicalRequest)
	require.Equal(t, prepared.RequestSHA256, replayed.RequestSHA256)
	require.Equal(t, prepared.ControlTokenExpiresAt, replayed.ControlTokenExpiresAt)

	result := cp2aRegistrationResult(t, prepared, cp2aControlProject)
	canonicalResult, err := canonicalizeRegistrationResult(result, Installation{
		InstallationID: prepared.InstallationID,
		XDeploymentID:  prepared.XDeploymentID,
		EventScope:     EventScopeModelCompleted,
	}, cp2aControlProject)
	require.NoError(t, err)
	require.NotEqual(t, result, canonicalResult, "X Encoder framing must not become product authority")
	descriptor := RuntimeInstallationDescriptor{
		InstallationID: cp2aInstallationID,
		XDeploymentID:  "xid_ZZZZZZZZZZZZZZZZZZZZZZ",
	}
	var sentBodies [][]byte
	responseLost := errors.New("synthetic response loss after X commit")
	lostClient := &http.Client{Transport: roundTripConnectorFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, connectorRedeemPath, request.URL.Path)
		require.Empty(t, request.Header.Get("Authorization"))
		require.Empty(t, request.Header.Get("Cookie"))
		body, readErr := io.ReadAll(request.Body)
		require.NoError(t, readErr)
		requestWire, decodeErr := decodeRedeemRequest(body)
		require.NoError(t, decodeErr)
		require.Equal(t, []string{EventScopeModelCompleted}, requestWire.FinalEventScope)
		sentBodies = append(sentBodies, body)
		return nil, responseLost
	})}
	lostCounterpart, err := NewHTTPControlCounterpart(
		"https://control.example.test", descriptor, restarted, lostClient, time.Second,
	)
	require.NoError(t, err)
	require.ErrorIs(t, lostCounterpart.Redeem(ctx), ErrUnavailable)
	requireRedeemBindingState(t, pool, "redeem_pending", prepared.RequestSHA256, "")

	successClient := &http.Client{Transport: roundTripConnectorFunc(func(request *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(request.Body)
		require.NoError(t, readErr)
		sentBodies = append(sentBodies, body)
		return connectorHTTPResponse(request, http.StatusOK, "application/json", result), nil
	})}
	successCounterpart, err := NewHTTPControlCounterpart(
		"https://control.example.test", descriptor, restarted, successClient, time.Second,
	)
	require.NoError(t, err)
	require.NoError(t, successCounterpart.Redeem(ctx))
	require.Len(t, sentBodies, 2)
	require.Equal(t, sentBodies[0], sentBodies[1], "send/response loss must replay exact request bytes")
	require.Equal(t, prepared.CanonicalRequest, sentBodies[1])
	requireRedeemBindingState(t, pool, "control_active", prepared.RequestSHA256, hashBytes(canonicalResult))

	runnable, err := restarted.ListRunnableInstallations(ctx, 16)
	require.NoError(t, err)
	require.Len(t, runnable, 1)
	require.Equal(t, cp2aControlProject, runnable[0].ControlProjectID)

	operation := mustDecodeControlV2Operation(
		t, "testdata/control-v2/valid/01-prepare-paused-source-operation.json",
	)
	operationBytes, err := operation.CanonicalJSON()
	require.NoError(t, err)
	var operationResponse bytes.Buffer
	require.NoError(t, json.NewEncoder(&operationResponse).Encode(operation))
	require.True(t, bytes.HasSuffix(operationResponse.Bytes(), []byte("\n")))
	require.NotEqual(t, operationBytes, operationResponse.Bytes())
	resultV2 := mustDecodeControlV2Result(
		t, "testdata/control-v2/valid/02-prepare-paused-source-result.json",
	)
	resultBytes, err := resultV2.CanonicalJSON()
	require.NoError(t, err)
	requestCount := 0
	controlClient := &http.Client{Transport: roundTripConnectorFunc(func(request *http.Request) (*http.Response, error) {
		requestCount++
		require.Equal(t, "Bearer "+cp2aControlToken, request.Header.Get("Authorization"))
		require.Empty(t, request.Header.Get("Cookie"))
		switch requestCount {
		case 1:
			require.Equal(t, http.MethodGet, request.Method)
			require.Equal(t, connectorOperationsPath, request.URL.Path)
			require.Equal(t, cp2aInstallationID, request.URL.Query().Get("installation_id"))
			return connectorHTTPResponse(request, http.StatusOK, "application/json", operationResponse.Bytes()), nil
		case 2:
			require.Equal(t, http.MethodPost, request.Method)
			require.Equal(t, connectorOperationsPath+"/"+resultV2.OperationID, request.URL.Path)
			body, readErr := io.ReadAll(request.Body)
			require.NoError(t, readErr)
			require.Equal(t, resultBytes, body)
			return connectorHTTPResponse(request, http.StatusOK, "", nil), nil
		default:
			t.Fatalf("unexpected Connector request %d", requestCount)
			return nil, nil
		}
	})}
	activeCounterpart, err := NewHTTPControlCounterpart(
		"https://control.example.test", runnable[0], restarted, controlClient, time.Second,
	)
	require.NoError(t, err)
	polled, found, err := activeCounterpart.Poll(ctx)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, operationBytes, polled)
	require.NoError(t, activeCounterpart.Acknowledge(ctx, resultBytes))
	require.Equal(t, 2, requestCount)

	// Expired authority is visible only as abnormal/fail-closed state: it is
	// neither silently regenerated nor returned to Poll/Ack discovery.
	*clock = clock.Add(connectorControlBearerLifetime + time.Second)
	_, err = restarted.controlToken(ctx, cp2aInstallationID)
	require.ErrorIs(t, err, ErrConflict)
	runnable, err = restarted.ListRunnableInstallations(ctx, 16)
	require.NoError(t, err)
	require.Empty(t, runnable)

	var invitationCipher, controlCipher string
	require.NoError(t, pool.QueryRow(ctx, `
SELECT COALESCE(invitation_token_enc,''),control_token_enc
FROM xinsight_connector_control_bindings WHERE installation_id=$1`,
		cp2aInstallationID,
	).Scan(&invitationCipher, &controlCipher))
	require.Empty(t, invitationCipher)
	require.NotContains(t, controlCipher, cp2aControlToken)
	require.NotContains(t, string(result), cp2aInvitation)
	require.NotContains(t, string(result), cp2aControlToken)
}

func TestConnectorRedeemRejectsTamperAndIdentityDriftIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	journal, _ := setupRedeemInstallation(t, pool)
	journal.controlBearer = func() (string, error) { return cp2aControlToken, nil }
	prepared, err := journal.PrepareRedeem(ctx, cp2aInstallationID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
UPDATE xinsight_connector_control_bindings
SET redeem_request_sha256='sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
WHERE installation_id=$1`, cp2aInstallationID)
	require.NoError(t, err)
	_, err = journal.PrepareRedeem(ctx, cp2aInstallationID)
	require.ErrorIs(t, err, ErrConflict)
	_, err = pool.Exec(ctx, `
UPDATE xinsight_connector_control_bindings SET redeem_request_sha256=$2
WHERE installation_id=$1`, cp2aInstallationID, prepared.RequestSHA256)
	require.NoError(t, err)

	drifted := cp2aRegistrationResult(t, prepared, "project.other")
	require.ErrorIs(t, journal.FinalizeRedeem(ctx, prepared, drifted), ErrConflict)
	requireRedeemBindingState(t, pool, "redeem_pending", prepared.RequestSHA256, "")
	legacyIdentity := bytes.ReplaceAll(
		cp2aRegistrationResult(t, prepared, cp2aControlProject),
		[]byte(`"deployment_id"`), []byte(`"x_deployment_id"`),
	)
	require.ErrorIs(t, journal.FinalizeRedeem(ctx, prepared, legacyIdentity), ErrConflict)
	scalarScope := bytes.ReplaceAll(
		cp2aRegistrationResult(t, prepared, cp2aControlProject),
		[]byte(`"final_event_scope":["`+EventScopeModelCompleted+`"]`),
		[]byte(`"final_event_scope":"`+EventScopeModelCompleted+`"`),
	)
	require.Error(t, journal.FinalizeRedeem(ctx, prepared, scalarScope))

	invalidPrepared := prepared
	invalidPrepared.CanonicalRequest = append(bytes.Clone(prepared.CanonicalRequest), ' ')
	require.ErrorIs(t, journal.FinalizeRedeem(
		ctx, invalidPrepared, cp2aRegistrationResult(t, prepared, cp2aControlProject),
	), ErrInvalid)
}

func setupRedeemInstallation(
	t *testing.T,
	pool *pgxpool.Pool,
) (*PostgresPhaseJournal, *time.Time) {
	t.Helper()
	ctx := context.Background()
	var orgID, projectID, actorID int64
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO orgs (name,status,org_type)
VALUES ('connector-cp2c-redeem','active','standard') RETURNING id`).Scan(&orgID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO users (org_id,username,password_hash,role,status)
VALUES ($1,'connector-cp2c-owner','hash','platform_admin','active') RETURNING id`, orgID).Scan(&actorID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO projects (org_id,name,slug,status,created_by)
VALUES ($1,'Connector CP2C redeem','second-integration','active',$2) RETURNING id`,
		orgID, actorID).Scan(&projectID))
	manager, err := NewManager(
		pool,
		CommitAuthorizerFunc(func(context.Context, pgx.Tx, int64, int64, int64, string) error { return nil }),
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
		EventScope: EventScopeModelCompleted, IdempotencyKey: "connect-cp2c-redeem",
		ConsentedByUserID: actorID,
	})
	require.NoError(t, err)
	journal, err := NewPostgresPhaseJournal(
		pool, []byte("0123456789abcdef0123456789abcdef"),
	)
	require.NoError(t, err)
	journal.now = func() time.Time { return clock }
	require.NoError(t, journal.StageInvitation(ctx, cp2aInstallationID, cp2aInvitation))
	return journal, &clock
}

func requireRedeemBindingState(
	t *testing.T,
	pool *pgxpool.Pool,
	wantState, requestSHA, resultSHA string,
) {
	t.Helper()
	var invitationPresent, controlPresent bool
	var storedRequest, storedResult string
	require.NoError(t, pool.QueryRow(context.Background(), `
SELECT invitation_token_enc IS NOT NULL,control_token_enc IS NOT NULL,
       COALESCE(redeem_request_sha256,''),COALESCE(redeem_result_sha256,'')
FROM xinsight_connector_control_bindings WHERE installation_id=$1`,
		cp2aInstallationID,
	).Scan(&invitationPresent, &controlPresent, &storedRequest, &storedResult))
	switch wantState {
	case "redeem_pending":
		require.True(t, invitationPresent)
		require.True(t, controlPresent)
	case "control_active":
		require.False(t, invitationPresent)
		require.True(t, controlPresent)
	default:
		t.Fatalf("unsupported state %q", wantState)
	}
	require.Equal(t, requestSHA, storedRequest)
	require.Equal(t, resultSHA, storedResult)
}

type roundTripConnectorFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripConnectorFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func connectorHTTPResponse(
	request *http.Request,
	status int,
	contentType string,
	body []byte,
) *http.Response {
	header := make(http.Header)
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Request:    request,
	}
}
