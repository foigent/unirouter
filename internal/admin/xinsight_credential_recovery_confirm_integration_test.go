//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

type recoveryResolveTransport func(*http.Request) (*http.Response, error)

func (transport recoveryResolveTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestXInsightCredentialRecoveryConfirmPostgresIntegration(t *testing.T) {
	f := newNormalSetupFixture(t)
	ctx := context.Background()
	preview := f.preview(t)
	require.Equal(t, http.StatusAccepted, f.human(t, "create-recovery-confirm", f.put(preview), f.handler.CreateXInsightSetup).Code)
	prepared, err := f.journal.PrepareRedeem(ctx, cp2bInstallationID)
	require.NoError(t, err)
	_, err = f.pool.Exec(ctx, `UPDATE xinsight_connector_control_bindings SET
		invitation_token_enc=NULL,redeem_result_sha256=$2,redeemed_origin=$3,
		created_at=NOW()-INTERVAL '25 hours',control_token_expires_at=NOW()-INTERVAL '1 hour'
		WHERE installation_id=$1`, cp2bInstallationID, cp2bSHA("synthetic-completed-redeem"), f.peer.URL)
	require.NoError(t, err)
	f.handler.SetXInsightCredentialJournal(f.journal)
	var previousSHA, sourceBinding string
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT b.control_token_sha256,i.source_binding_sha256
		FROM xinsight_connector_installations i JOIN xinsight_connector_control_bindings b USING(installation_id)
		WHERE i.installation_id=$1`, cp2bInstallationID).Scan(&previousSHA, &sourceBinding))
	resolution := xinsightconnector.CredentialRecoveryResolution{
		SchemaVersion:  xinsightconnector.CredentialRecoverySchema,
		InstallationID: cp2bInstallationID, DeploymentID: cp2bDeploymentID,
		ProjectID: prepared.ControlProjectID, FinalEventScope: []string{xinsightconnector.EventScopeModelCompleted},
		SourceBindingSHA256: sourceBinding, XInsightOrigin: f.peer.URL, UniRouterOrigin: f.consent.UROrigin,
		RecoveryID: "xrr_" + strings.Repeat("A", 22), PreviousTokenSHA256: previousSHA,
		ExpiresAt: time.Now().UTC().Add(15 * time.Minute).Truncate(time.Microsecond).Format(time.RFC3339Nano),
	}
	require.NoError(t, resolution.Validate(time.Now().UTC()))
	f.handler.xInsightSetup.client.Transport = recoveryResolveTransport(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, f.peer.URL+"/api/v1/connector-control/credential-recoveries/resolve", request.URL.String())
		require.Empty(t, request.Header.Get("Authorization"))
		body, err := json.Marshal(resolution)
		require.NoError(t, err)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	ticket := "xirt_" + strings.Repeat("A", 43)
	input := map[string]any{"recovery_ticket": ticket}
	require.Equal(t, http.StatusOK, f.human(t, "recovery-preview", input, f.handler.PreviewXInsightCredentialRecovery).Code)
	confirm := f.human(t, "recovery-confirm", input, f.handler.ConfirmXInsightCredentialRecovery)
	require.Equal(t, http.StatusAccepted, confirm.Code, confirm.Body.String())
	var accepted struct {
		OperationID string `json:"operation_id"`
		State       string `json:"state"`
	}
	require.NoError(t, json.Unmarshal(confirm.Body.Bytes(), &accepted))
	require.Regexp(t, `^xcr_[A-Za-z0-9_-]{22}$`, accepted.OperationID)
	require.Equal(t, "pending", accepted.State)
	var operations, audits int
	counts := func() (int, int) {
		t.Helper()
		require.NoError(t, f.pool.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM xinsight_connector_credential_operations WHERE installation_id=$1),
			(SELECT count(*) FROM audit_logs WHERE action='data_exchange.xinsight.credential.recovery_confirm' AND resource_id=$1)`, cp2bInstallationID).Scan(&operations, &audits))
		return operations, audits
	}
	operations, audits = counts()
	require.Equal(t, 1, operations)
	require.Equal(t, 1, audits)
	var operationState string
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT state FROM xinsight_connector_credential_operations WHERE operation_id=$1`, accepted.OperationID).Scan(&operationState))
	require.Equal(t, "pending", operationState)
	replay := f.human(t, "recovery-replay", input, f.handler.ConfirmXInsightCredentialRecovery)
	require.Equal(t, http.StatusAccepted, replay.Code, replay.Body.String())
	var replayed struct {
		OperationID string `json:"operation_id"`
	}
	require.NoError(t, json.Unmarshal(replay.Body.Bytes(), &replayed))
	require.Equal(t, accepted.OperationID, replayed.OperationID)
	operations, audits = counts()
	require.Equal(t, 1, operations)
	require.Equal(t, 2, audits, "each accepted human confirmation remains auditable")
	resolution.InstallationID = "xci_" + strings.Repeat("B", 22)
	denied := f.human(t, "recovery-foreign-resolution", input, f.handler.ConfirmXInsightCredentialRecovery)
	require.Equal(t, http.StatusConflict, denied.Code, denied.Body.String())
	beforeOperations, beforeAudits := operations, audits
	operations, audits = counts()
	require.Equal(t, beforeOperations, operations)
	require.Equal(t, beforeAudits, audits)
}
