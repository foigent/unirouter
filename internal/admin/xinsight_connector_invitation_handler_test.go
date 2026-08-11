package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

func TestInstallXInsightConnectorReturnsSecretFreeIdempotentState(t *testing.T) {
	intake := &recordingXInsightConnectorInvitationIntake{
		result: xinsightconnector.InvitationIntakeResult{
			Installation: xinsightconnector.Installation{
				SchemaVersion:  xinsightconnector.InstallationSchemaVersion,
				InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA",
				XDeploymentID:  "xid_AAAAAAAAAAAAAAAAAAAAAA",
				EventScope:     xinsightconnector.EventScopeModelCompleted,
				SourceInstance: "source.connector.intake",
				DesiredState:   xinsightconnector.DesiredActive,
				ObservedState:  xinsightconnector.ObservedProvisioning,
				ConsentedAt:    time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			Operation: xinsightconnector.Operation{
				SchemaVersion:  xinsightconnector.OperationSchemaVersion,
				OperationRef:   "xcop_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA",
				Action:         xinsightconnector.ActionConnect,
				DesiredState:   xinsightconnector.DesiredActive,
				State:          xinsightconnector.OperationQueued,
			},
		},
	}
	handler := &Handler{
		db:                      &testutil.MockDB{QueryRowFn: projectOrgRow(t, 7)},
		xInsightConnectorIntake: intake,
	}
	request := newXInsightConnectorInvitationRequest(t, "valid/01-frozen-invitation.json")
	request = request.WithContext(context.WithValue(request.Context(), middleware.CtxUserID, int64(13)))
	response := httptest.NewRecorder()

	handler.InstallXInsightConnector(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.Equal(t, 1, intake.calls)
	require.Equal(t, int64(7), intake.input.OrgID)
	require.Equal(t, int64(11), intake.input.ProjectID)
	require.Equal(t, int64(13), intake.input.ConsentedByUserID)
	require.Equal(t, "xii_AAAAAAAAAAAAAAAAAAAAAA", intake.input.Invitation.InvitationID)
	body := response.Body.String()
	require.Contains(t, body, `"connector_product_status":"connecting"`)
	require.Contains(t, body, `"installation_id":"xci_AAAAAAAAAAAAAAAAAAAAAA"`)
	require.NotContains(t, body, "xiiv1_")
	require.NotContains(t, body, "install_url")
	require.NotContains(t, body, "invitation_sha256")
}

func TestInstallXInsightConnectorRejectsExtendedInvitationBeforeDatabaseAccess(t *testing.T) {
	intake := &recordingXInsightConnectorInvitationIntake{}
	handler := &Handler{
		db: &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
			t.Fatal("malformed invitation reached the database")
			return &testutil.MockRow{}
		}},
		xInsightConnectorIntake: intake,
	}
	request := newXInsightConnectorInvitationRequest(t, "invalid/01-unknown-field.json")
	response := httptest.NewRecorder()

	handler.InstallXInsightConnector(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Zero(t, intake.calls)
	require.Contains(t, response.Body.String(), "Invalid JSON")
}

func TestInstallXInsightConnectorFailureMapping(t *testing.T) {
	for _, testCase := range []struct {
		name string
		err  error
		want int
	}{
		{name: "invalid", err: xinsightconnector.ErrInvalid, want: http.StatusBadRequest},
		{name: "unauthorized", err: xinsightconnector.ErrUnauthorized, want: http.StatusForbidden},
		{name: "conflict", err: xinsightconnector.ErrConflict, want: http.StatusConflict},
		{name: "unavailable", err: errors.New("database unavailable"), want: http.StatusServiceUnavailable},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			intake := &recordingXInsightConnectorInvitationIntake{err: testCase.err}
			handler := &Handler{
				db:                      &testutil.MockDB{QueryRowFn: projectOrgRow(t, 7)},
				xInsightConnectorIntake: intake,
			}
			request := newXInsightConnectorInvitationRequest(t, "valid/01-frozen-invitation.json")
			request = request.WithContext(context.WithValue(request.Context(), middleware.CtxUserID, int64(13)))
			response := httptest.NewRecorder()

			handler.InstallXInsightConnector(response, request)

			require.Equal(t, testCase.want, response.Code)
			require.Equal(t, 1, intake.calls)
			require.NotContains(t, response.Body.String(), "xiiv1_")
		})
	}
}

type recordingXInsightConnectorInvitationIntake struct {
	input  xinsightconnector.InvitationIntakeInput
	result xinsightconnector.InvitationIntakeResult
	err    error
	calls  int
}

func (intake *recordingXInsightConnectorInvitationIntake) Accept(
	_ context.Context,
	input xinsightconnector.InvitationIntakeInput,
) (xinsightconnector.InvitationIntakeResult, error) {
	intake.calls++
	intake.input = input
	return intake.result, intake.err
}

func projectOrgRow(t *testing.T, orgID int64) func(context.Context, string, ...any) pgx.Row {
	t.Helper()
	return func(_ context.Context, query string, _ ...any) pgx.Row {
		require.Contains(t, query, "SELECT org_id FROM projects")
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			*dest[0].(*int64) = orgID
			return nil
		}}
	}
}

func newXInsightConnectorInvitationRequest(t *testing.T, relativeFixture string) *http.Request {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(
		"..", "xinsightconnector", "testdata", "invitations", relativeFixture,
	))
	require.NoError(t, err)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/data-exchange/xinsight/connections/11/install",
		strings.NewReader(`{"invitation":`+string(raw)+`}`),
	)
	request.SetPathValue("project_id", "11")
	return request
}
