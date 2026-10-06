//go:build integration

package xinsightconnector

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/testutil"
)

func setupCredentialLifecycle(t *testing.T) (*PostgresPhaseJournal, *time.Time) {
	t.Helper()
	pool := testutil.SetupMigratedTestDB(t)
	ctx := context.Background()
	journal, clock := setupRedeemInstallation(t, pool)
	journal.controlBearer = func() (string, error) { return cp2aControlToken, nil }
	prepared, err := journal.PrepareRedeem(ctx, cp2aInstallationID)
	require.NoError(t, err)
	require.NoError(t, journal.FinalizeRedeemFromOrigin(ctx, prepared, cp2aRegistrationResult(t, prepared, cp2aControlProject), "https://control.example.test"))
	var fence int64
	require.NoError(t, pool.QueryRow(ctx, `SELECT control_fence FROM xinsight_connector_installations WHERE installation_id=$1`, cp2aInstallationID).Scan(&fence))
	consent, err := json.Marshal(struct {
		ControlFence int64                  `json:"control_fence"`
		XIConsent    credentialSetupBinding `json:"xi_consent"`
	}{fence, credentialSetupBinding{XIOrigin: "https://control.example.test", UROrigin: "https://ur.example.test", InstallationID: cp2aInstallationID, DeploymentID: prepared.XDeploymentID}})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE xinsight_connector_installations SET setup_request_id='00000000-0000-4000-8000-000000000125',
setup_consent_canonical=$2,setup_consent_sha256=$3,setup_state='ready',setup_progress='{}'::jsonb WHERE installation_id=$1`, cp2aInstallationID, consent, hashBytes(consent))
	require.NoError(t, err)
	// The current token remains valid, but enters the six-hour rotation window.
	*clock = prepared.ControlTokenExpiresAt.Add(-5 * time.Hour)
	return journal, clock
}

func TestControlCredentialLifecyclePostgresIntegration(t *testing.T) {
	ctx := context.Background()
	journal, clock := setupCredentialLifecycle(t)
	journal.controlBearer = func() (string, error) { return "xib_B" + cp2aControlToken[5:], nil }
	due, err := journal.ListCredentialDueInstallations(ctx, 8)
	require.NoError(t, err)
	require.Len(t, due, 1)
	rotation, err := journal.PrepareRotate(ctx, cp2aInstallationID)
	require.NoError(t, err)
	require.Equal(t, "rotate", rotation.request.Kind)
	require.Equal(t, "https://control.example.test", rotation.request.XInsightOrigin)
	journal.controlBearer = func() (string, error) { return "", errors.New("must replay durable replacement") }
	replayed, err := journal.PrepareRotate(ctx, cp2aInstallationID)
	require.NoError(t, err)
	require.Equal(t, rotation.canonical, replayed.canonical)
	require.Equal(t, rotation.replacement, replayed.replacement)
	status, err := journal.CredentialStatus(ctx, cp2aInstallationID)
	require.NoError(t, err)
	require.Equal(t, "rotation_pending", status.State)
	require.Equal(t, rotation.request.OperationID, status.PendingOperationID)

	receipt := CredentialReceipt{SchemaVersion: CredentialReceiptSchema, InstallationID: rotation.request.InstallationID,
		DeploymentID: rotation.request.DeploymentID, ProjectID: rotation.request.ProjectID,
		FinalEventScope: rotation.request.FinalEventScope, SourceBindingSHA256: rotation.request.SourceBindingSHA256,
		XInsightOrigin: rotation.request.XInsightOrigin, UniRouterOrigin: rotation.request.UniRouterOrigin,
		OperationID: rotation.request.OperationID, RequestSHA256: rotation.requestSHA256,
		ControlIdentityID: "xsi_AAAAAAAAAAAAAAAAAAAAAA", PreviousTokenSHA256: rotation.request.PreviousTokenSHA256,
		ReplacementTokenSHA256: hashBytes([]byte(rotation.replacement)), ReplacementExpiresAt: clock.Add(24 * time.Hour).UTC().Format(time.RFC3339Nano), State: "committed"}
	require.NoError(t, journal.RecordCredentialReceipt(ctx, rotation, receipt))
	require.NoError(t, journal.PromoteCredential(ctx, cp2aInstallationID, rotation.request.OperationID))
	status, err = journal.CredentialStatus(ctx, cp2aInstallationID)
	require.NoError(t, err)
	require.Equal(t, "healthy", status.State)
	require.Equal(t, hashBytes([]byte(rotation.replacement)), status.CurrentTokenSHA256)
	require.Empty(t, status.PendingOperationID)

	// An origin changed in runtime configuration cannot receive either old or
	// replacement secrets. The transport must remain untouched.
	wrongOrigin, err := NewHTTPControlCounterpart("https://other.example.test", due[0], journal,
		&http.Client{Transport: roundTripConnectorFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("credential request escaped the frozen-origin guard")
			return nil, nil
		})}, time.Second)
	require.NoError(t, err)
	*clock = status.CurrentExpiresAt.Add(-5 * time.Hour)
	journal.controlBearer = func() (string, error) { return "xib_C" + cp2aControlToken[5:], nil }
	require.ErrorIs(t, wrongOrigin.RunCredential(ctx), ErrConflict)
}

func TestControlCredentialExpiredPendingRecoveryPostgresIntegration(t *testing.T) {
	for _, caseA := range []bool{true, false} {
		name := "expired-prepared-old-current"
		if caseA {
			name = "xi-committed-ur-unpromoted"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			journal, clock := setupCredentialLifecycle(t)
			journal.controlBearer = func() (string, error) { return "xib_B" + cp2aControlToken[5:], nil }
			rotation, err := journal.PrepareRotate(ctx, cp2aInstallationID)
			require.NoError(t, err)
			receiptState := "prepared"
			if caseA {
				receiptState = "committed"
			}
			require.NoError(t, journal.RecordCredentialReceipt(ctx, rotation,
				credentialLifecycleReceipt(rotation, clock.Add(24*time.Hour), receiptState)))
			// The stored XI receipt's actual deadline has passed. In case A,
			// XI committed the replacement but UR never promoted it; in case B
			// XI prepared it while the old current was still valid.
			*clock = clock.Add(30 * time.Hour)
			status, err := journal.CredentialStatus(ctx, cp2aInstallationID)
			require.NoError(t, err)
			require.Equal(t, "expired_recovery_required", status.State)
			journal, err = NewPostgresPhaseJournal(journal.db, []byte("0123456789abcdef0123456789abcdef"))
			require.NoError(t, err)
			journal.now = func() time.Time { return *clock }
			previousSHA := rotation.request.PreviousTokenSHA256
			supersededID, supersededSHA := rotation.request.OperationID, hashBytes([]byte(rotation.replacement))
			if caseA {
				previousSHA, supersededID, supersededSHA = supersededSHA, "", ""
			}
			resolution := CredentialRecoveryResolution{SchemaVersion: CredentialRecoverySchema,
				InstallationID: rotation.request.InstallationID, DeploymentID: rotation.request.DeploymentID,
				ProjectID: rotation.request.ProjectID, FinalEventScope: rotation.request.FinalEventScope,
				SourceBindingSHA256: rotation.request.SourceBindingSHA256,
				XInsightOrigin:      rotation.request.XInsightOrigin, UniRouterOrigin: rotation.request.UniRouterOrigin,
				RecoveryID: "xrr_AAAAAAAAAAAAAAAAAAAAAA", PreviousTokenSHA256: previousSHA,
				SupersededOperationID: supersededID, SupersededReplacementSHA256: supersededSHA,
				ExpiresAt: clock.Add(15 * time.Minute).UTC().Format(time.RFC3339Nano)}
			journal.controlBearer = func() (string, error) { return "xib_C" + cp2aControlToken[5:], nil }
			tx, err := journal.db.Begin(ctx)
			require.NoError(t, err)
			opID, err := journal.PrepareRecoveryTx(ctx, tx, cp2aInstallationID, "xirt_"+"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", resolution)
			require.NoError(t, err)
			require.NoError(t, tx.Commit(ctx))
			require.NotEqual(t, rotation.request.OperationID, opID)
			var oldState string
			require.NoError(t, journal.db.QueryRow(ctx, `SELECT state FROM xinsight_connector_credential_operations WHERE operation_id=$1`, rotation.request.OperationID).Scan(&oldState))
			require.Equal(t, "superseded", oldState)
			pending, err := journal.PendingCredential(ctx, cp2aInstallationID)
			require.NoError(t, err)
			require.Equal(t, "recover", pending.request.Kind)
			require.Equal(t, previousSHA, pending.request.PreviousTokenSHA256)
			if caseA {
				require.Equal(t, rotation.replacement, *pending.request.PreviousToken)
			} else {
				require.Equal(t, cp2aControlToken, *pending.request.PreviousToken)
			}
			// A lost recovery response is replayed from the same encrypted bytes.
			journal.controlBearer = func() (string, error) { return "", errors.New("no new secret on retry") }
			replayed, err := journal.PrepareRotate(ctx, cp2aInstallationID)
			require.NoError(t, err)
			require.Equal(t, pending.canonical, replayed.canonical)
			require.NoError(t, journal.RecordCredentialReceipt(ctx, pending, credentialLifecycleReceipt(pending, clock.Add(24*time.Hour), "committed")))
			require.NoError(t, journal.PromoteCredential(ctx, cp2aInstallationID, opID))
			status, err = journal.CredentialStatus(ctx, cp2aInstallationID)
			require.NoError(t, err)
			require.Equal(t, "healthy", status.State)
			require.Equal(t, hashBytes([]byte(pending.replacement)), status.CurrentTokenSHA256)
			// Revocation is terminal for this writer.
			_, err = journal.db.Exec(ctx, `UPDATE xinsight_connector_installations SET desired_state='disconnected',observed_state='disconnected' WHERE installation_id=$1`, cp2aInstallationID)
			require.NoError(t, err)
			tx, err = journal.db.Begin(ctx)
			require.NoError(t, err)
			_, err = journal.PrepareRecoveryTx(ctx, tx, cp2aInstallationID, "xirt_"+"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", resolution)
			require.ErrorIs(t, err, ErrConflict)
			require.NoError(t, tx.Rollback(ctx))
		})
	}
}

func TestControlCredentialPendingExpiryDiagnosisPostgresIntegration(t *testing.T) {
	ctx := context.Background()
	journal, clock := setupCredentialLifecycle(t)
	journal.controlBearer = func() (string, error) { return "xib_B" + cp2aControlToken[5:], nil }
	rotation, err := journal.PrepareRotate(ctx, cp2aInstallationID)
	require.NoError(t, err)
	before := *clock
	// The old current has expired, but without a peer receipt the durable
	// request may have succeeded. Keep its exact replay and report uncertainty.
	*clock = before.Add(6 * time.Hour)
	status, err := journal.CredentialStatus(ctx, cp2aInstallationID)
	require.NoError(t, err)
	require.Equal(t, "rotation_pending", status.State)
	require.Equal(t, rotation.request.OperationID, status.PendingOperationID)
	*clock = before
	expiry := clock.Add(time.Hour)
	require.NoError(t, journal.RecordCredentialReceipt(ctx, rotation, credentialLifecycleReceipt(rotation, expiry, "prepared")))
	*clock = expiry.Add(time.Second)
	status, err = journal.CredentialStatus(ctx, cp2aInstallationID)
	require.NoError(t, err)
	require.Equal(t, "expired_recovery_required", status.State)
	require.Equal(t, "pending", status.PendingState)
	// A committed-but-unpromoted replacement with the same known expiry
	// requires recovery as well; a later promotion cannot use it.
	require.NoError(t, journal.RecordCredentialReceipt(ctx, rotation, credentialLifecycleReceipt(rotation, expiry, "committed")))
	status, err = journal.CredentialStatus(ctx, cp2aInstallationID)
	require.NoError(t, err)
	require.Equal(t, "expired_recovery_required", status.State)
	require.Equal(t, "committed", status.PendingState)
}

func credentialLifecycleReceipt(op preparedCredentialOperation, expiry time.Time, state string) CredentialReceipt {
	r := op.request
	return CredentialReceipt{SchemaVersion: CredentialReceiptSchema, InstallationID: r.InstallationID,
		DeploymentID: r.DeploymentID, ProjectID: r.ProjectID, FinalEventScope: r.FinalEventScope,
		SourceBindingSHA256: r.SourceBindingSHA256, XInsightOrigin: r.XInsightOrigin, UniRouterOrigin: r.UniRouterOrigin,
		OperationID: r.OperationID, RequestSHA256: op.requestSHA256, ControlIdentityID: "xsi_AAAAAAAAAAAAAAAAAAAAAA",
		PreviousTokenSHA256: r.PreviousTokenSHA256, ReplacementTokenSHA256: hashBytes([]byte(op.replacement)),
		ReplacementExpiresAt: expiry.UTC().Format(time.RFC3339Nano), State: state}
}
